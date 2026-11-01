//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func report(link bool, level, rate, light int) statusReport {
	d := make([]byte, 32)
	d[0] = 4
	if link {
		d[1] = 1
	}
	d[2], d[3], d[4], d[5], d[6], d[8], d[9] = byte(level), byte(rate), 80, 0, byte(light), 4, 0x11
	return statusReport{Path: `\\?\hid#vid_04f3&pid_026f&mi_01&col02#x`, PID: pidDongle, Data: d}
}

func takeSent() [][]byte {
	sentMu.Lock()
	defer sentMu.Unlock()
	s := sent
	sent = nil
	return s
}
func cmds(p [][]byte) string {
	var b []string
	for _, x := range p {
		b = append(b, string(rune('0'+x[0])))
	}
	return strings.Join(b, ",")
}

func setup(t *testing.T) (*Store, *Engine) {
	st := loadStore(t.TempDir())
	devices = []hidDevice{{Path: `\\?\hid#vid_04f3&pid_026f&mi_01&col03#x`, PID: pidDongle, UsagePage: 0x0A, FeatLen: 32}}
	e := newEngine(st)
	sent = nil
	return st, e
}

func waitIdle(e *Engine) {
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < 200; i++ {
		e.mu.Lock()
		b := e.busy
		e.mu.Unlock()
		if !b && len(e.jobs) == 0 {
			time.Sleep(30 * time.Millisecond)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestFlow(t *testing.T) {
	st, e := setup(t)
	def := st.data.Settings.DefaultProfile
	e.handleReport(report(true, 2, 1, 9))
	p := st.profile(def)
	if p.Lighting != 9 || p.DPIStart != 2 || !st.state.Seeded {
		t.Fatalf("seed: %+v", p)
	}
	st.data.Settings.AutoSwitch = true
	// rule before sync → ignored
	st.data.Rules = []Rule{{Exe: "game.exe", ProfileID: def, Enabled: true}}
	procs = map[string]int{"game.exe": 1}
	e.onProcesses(procs)
	waitIdle(e)
	if len(takeSent()) != 0 {
		t.Fatal("rule applied before sync")
	}
	procs = map[string]int{}
	e.onProcesses(procs)

	if err := e.Apply(def, false, "t", false, true); err != nil {
		t.Fatal(err)
	}
	s := takeSent()
	if cmds(s) != "1,1,2,2,3,4,5,7,8,6" {
		t.Fatalf("full sync cmds %s", cmds(s))
	}
	// seq strictly increasing, never 0
	for i, pk := range s {
		if pk[1] == 0 || (i > 0 && pk[1] != s[i-1][1]+1) {
			t.Fatalf("seq %v", pk[1])
		}
	}
	if !st.state.Synced {
		t.Fatal("not synced")
	}
	e.Apply(def, false, "t", false, true)
	if n := len(takeSent()); n != 0 {
		t.Fatalf("second apply sent %d", n)
	}
	p.Lighting = 3
	e.Apply(def, false, "t", false, true)
	s = takeSent()
	if cmds(s) != "6" || s[0][5] != 3 {
		t.Fatalf("light diff %s % x", cmds(s), s)
	}

	// game profile with other DPI
	g := newProfile("Game", 0x11)
	g.Lighting = 3
	g.DPILevels[0] = 800
	g.DPIStart = p.DPIStart
	g.Buttons = p.Buttons
	g.PollRate = p.PollRate
	st.data.Profiles = append(st.data.Profiles, g)
	st.data.Rules = []Rule{{Exe: "game.exe", ProfileID: g.ID, Enabled: true, OSD: true}}
	st.data.Settings.OSDProfile = true
	var osdN int
	e.onOSD = func(osdContent) { osdN++ }
	procs = map[string]int{"game.exe": 1}
	e.onProcesses(procs)
	waitIdle(e)
	s = takeSent()
	if cmds(s) != "2,2" {
		t.Fatalf("game switch %s", cmds(s))
	}
	if osdN != 1 {
		t.Fatalf("osd %d", osdN)
	}
	// still running: nothing
	e.onProcesses(procs)
	waitIdle(e)
	if len(takeSent()) != 0 {
		t.Fatal("re-applied while running")
	}
	// feature switched off while the game runs → back to base, rules ignored
	st.data.Settings.AutoSwitch = false
	e.onProcesses(procs)
	waitIdle(e)
	if cmds(takeSent()) != "2,2" || len(e.stack) != 0 {
		t.Fatal("autoswitch off did not revert")
	}
	e.onProcesses(procs)
	waitIdle(e)
	if len(takeSent()) != 0 {
		t.Fatal("rule applied while autoswitch off")
	}
	st.data.Settings.AutoSwitch = true
	e.onProcesses(procs)
	waitIdle(e)
	if cmds(takeSent()) != "2,2" {
		t.Fatal("re-enable")
	}
	// exit → back
	procs = map[string]int{}
	e.onProcesses(procs)
	waitIdle(e)
	if cmds(takeSent()) != "2,2" {
		t.Fatal("revert")
	}

	// macro on button 1
	m := &Macro{ID: "m1", Name: "x", ExecuteType: 2, RepeatCount: 3, Events: []MacroEvent{{Type: "keydown", Code: 4}, {Type: "keyup", Code: 4, Delay: 20, Index: 1}}}
	st.data.Macros = []*Macro{m}
	p.Buttons[0] = ButtonAssign{Kind: "macro", MacroID: "m1"}
	e.Apply(def, false, "t", false, true)
	s = takeSent()
	if cmds(s) != "1,1,?,@" { // 17 erase, 16 data
		t.Logf("macro cmds %v", cmds(s))
	}
	if len(s) != 4 || s[2][0] != 17 || s[3][0] != 16 || len(s[3]) != 32 {
		t.Fatalf("macro packets %d", len(s))
	}
	// editing the macro → keys resent
	m.Events[1].Delay = 50
	e.Apply(def, false, "t", false, true)
	if n := len(takeSent()); n != 4 {
		t.Fatalf("macro edit resend %d", n)
	}

	// link lost → pending → link back → applied
	e.handleReport(report(false, 0, 0, 0))
	p.Lighting = 5
	e.Apply(def, false, "t", false, true)
	if len(takeSent()) != 0 || e.pending == nil {
		t.Fatal("pending not set")
	}
	e.handleReport(report(true, 2, 1, 5))
	waitIdle(e)
	if cmds(takeSent()) != "6" {
		t.Fatal("pending not run")
	}

	// failure keeps signature unsent
	ackFail = true
	p.Lighting = 1
	e.Apply(def, false, "t", false, true)
	ackFail = false
	if d := e.diff(p); len(d) != 1 || d[0] != "light" {
		t.Fatalf("diff after fail %v", d)
	}
	// factory reset → unsynced
	e.FactoryReset()
	waitIdle(e)
	if st.state.Synced || cmds(takeSent()) != "?" {
		t.Fatal("reset")
	}
}

func post(t *testing.T, a *API, path string, body any) (int, map[string]any) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", strings.Split(a.url, "/?")[0]+"/api/"+path, bytes.NewReader(b))
	req.Header.Set("X-Token", a.token)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var m map[string]any
	json.NewDecoder(r.Body).Decode(&m)
	return r.StatusCode, m
}

func TestAPI(t *testing.T) {
	st, e := setup(t)
	a, err := newAPI(e, st, func(f func()) { f() })
	if err != nil {
		t.Fatal(err)
	}
	// token required
	r, _ := http.Get(strings.Split(a.url, "/?")[0] + "/api/all")
	if r.StatusCode != 403 {
		t.Fatal("no token check")
	}
	r, _ = http.Get(a.url)
	if r.StatusCode != 200 {
		t.Fatal("index")
	}
	p := *st.profile(st.data.Settings.DefaultProfile)
	p.Buttons[11] = ButtonAssign{Kind: "mouse", Code: [3]int{16, 2, 0}}
	if c, m := post(t, a, "profile/save", p); c != 400 {
		t.Fatalf("left click rule: %v", m)
	}
	p = *st.profile(st.data.Settings.DefaultProfile)
	p.DPILevels[0] = 150
	if c, _ := post(t, a, "profile/save", p); c != 400 {
		t.Fatal("dpi range")
	}
	c, m := post(t, a, "macro/save", Macro{Name: "a", ExecuteType: 1, Events: []MacroEvent{{Type: "keydown", Code: 4}, {Type: "keyup", Code: 4}}})
	if c != 200 {
		t.Fatal(m)
	}
	mid := m["id"].(string)
	p = *st.profile(st.data.Settings.DefaultProfile)
	p.Buttons[0] = ButtonAssign{Kind: "macro", MacroID: mid}
	if c, m := post(t, a, "profile/save", p); c != 200 {
		t.Fatal(m)
	}
	if c, _ := post(t, a, "macro/delete", map[string]string{"id": mid}); c != 400 {
		t.Fatal("deleted used macro")
	}
	if c, _ := post(t, a, "macro/save", Macro{Name: "b", Events: []MacroEvent{{Type: "keydown", Code: 200}}}); c != 400 {
		t.Fatal("bad key accepted")
	}
	c, m = post(t, a, "profile/create", map[string]string{"name": "G", "copyFrom": p.ID})
	if c != 200 {
		t.Fatal(m)
	}
	gid := m["id"].(string)
	if c, m := post(t, a, "rules", []Rule{{Exe: "Game", ProfileID: gid, Enabled: true}, {Exe: "game.exe", ProfileID: gid}}); c != 400 {
		t.Fatalf("dup rules %v", m)
	}
	if c, m := post(t, a, "rules", []Rule{{Exe: "Game", ProfileID: gid, Enabled: true}}); c != 200 || st.data.Rules[0].Exe != "game.exe" {
		t.Fatalf("rules %v", m)
	}
	if c, _ := post(t, a, "profile/delete", map[string]string{"id": st.data.Settings.DefaultProfile}); c != 400 {
		t.Fatal("deleted default")
	}
	if c, _ := post(t, a, "profile/delete", map[string]string{"id": gid}); c != 200 || len(st.data.Rules) != 0 {
		t.Fatal("delete cascade")
	}
	// settings keep profile ids
	s := st.data.Settings
	s.DefaultProfile = "zzz"
	s.ScanSeconds = 0
	if c, _ := post(t, a, "settings", map[string]any{"settings": s}); c != 200 || st.data.Settings.DefaultProfile == "zzz" || st.data.Settings.ScanSeconds != 1 {
		t.Fatal("settings")
	}
	// import remaps macro ids
	exp := map[string]any{"profiles": []*Profile{st.profile(p.ID)}, "macros": st.data.Macros}
	if c, m := post(t, a, "import", exp); c != 200 {
		t.Fatal(m)
	}
	last := st.data.Profiles[len(st.data.Profiles)-1]
	if last.Buttons[0].MacroID == mid || st.macro(last.Buttons[0].MacroID) == nil {
		t.Fatal("import macro remap")
	}
	_ = a.stateView()

	// full backup → restore on a "new PC"
	st.state.FlashWrites = 1234
	e.bat = &BatHistory{path: t.TempDir() + "/b.json"}
	e.bat.add(80, 0, "dongle", time.Now())
	r, _ = http.Get(strings.Split(a.url, "/?")[0] + "/api/backup?t=" + a.token)
	var bk map[string]any
	json.NewDecoder(r.Body).Decode(&bk)
	r.Body.Close()
	st2, e2 := setup(t)
	e2.bat = &BatHistory{path: t.TempDir() + "/b.json"}
	a2, _ := newAPI(e2, st2, func(f func()) { f() })
	if c, m := post(t, a2, "restore", map[string]any{"format": "x"}); c != 400 {
		t.Fatalf("bad backup accepted %v", m)
	}
	if c, m := post(t, a2, "restore", bk); c != 200 {
		t.Fatalf("restore %v", m)
	}
	if st2.state.FlashWrites != 1234 || len(st2.data.Profiles) != len(st.data.Profiles) || len(e2.bat.Samples) != 1 {
		t.Fatalf("restore content %d %d %d", st2.state.FlashWrites, len(st2.data.Profiles), len(e2.bat.Samples))
	}
}

func TestDebounce(t *testing.T) {
	st, e := setup(t)
	def := st.data.Settings.DefaultProfile
	e.handleReport(report(true, 1, 1, 9))
	e.Apply(def, false, "t", false, true)
	takeSent()
	st.data.Settings.DebounceSet, st.data.Settings.Debounce = true, 5
	e.Apply(def, false, "t", false, true)
	s := takeSent()
	if len(s) != 1 || s[0][0] != 9 || s[0][4] != 5 || s[0][2] != 1 {
		t.Fatalf("debounce packet % X", s)
	}
	e.Apply(def, false, "t", false, true)
	if len(takeSent()) != 0 {
		t.Fatal("debounce resent")
	}
	st.data.Settings.DebounceSet = false
	e.Apply(def, false, "t", false, true)
	if len(takeSent()) != 0 {
		t.Fatal("sent while not managed")
	}
}

// Button modes: switching writes only the keys command, software actions are
// never written, the left click is guarded, secrets are sealed.
func TestButtonModes(t *testing.T) {
	st, e := setup(t)
	a, err := newAPI(e, st, func(f func()) { f() })
	if err != nil {
		t.Fatal(err)
	}
	protectSecret = func(s string) (string, error) { return "enc:" + s, nil }
	defer func() { protectSecret = nil }()
	var carrierSeen [][softSlots]bool
	onCarrierChange = func(m [softSlots]bool) { carrierSeen = append(carrierSeen, m) }
	defer func() { onCarrierChange = nil }()
	def := st.data.Settings.DefaultProfile
	if st.data.Settings.ButtonMode != "" || !st.data.Settings.KeyRepeat {
		t.Fatal("fresh settings")
	}
	e.handleReport(report(true, 1, 1, 9))
	e.Apply(def, false, "t", false, true)
	takeSent()

	// a pending profile change (lighting) must not be written by the mode switch
	st.profile(def).Lighting = 3
	if c, m := post(t, a, "buttons/mode", map[string]string{"mode": "software"}); c != 200 || m["ok"] != true {
		t.Fatalf("software: %d %v", c, m)
	}
	s := takeSent()
	if cmds(s) != "1,1" || s[0][4+21] != 112 || s[0][4+23] != 104 { // button 1 (8th entry of the first packet) = F13
		t.Fatalf("mode switch sent %s % X", cmds(s), s)
	}
	want := carriedMask(st.data.Settings)
	if st.data.Settings.HWButtons != defaultHW(st.profile(def)) || st.state.Carrier != want || len(carrierSeen) == 0 || carrierSeen[len(carrierSeen)-1] != want {
		t.Fatalf("hw %v carrier %v", st.data.Settings.HWButtons, st.state.Carrier)
	}
	if st.profile(def).Soft[0] != (SoftBinding{Kind: SoftKeys, Keys: "Win+D"}) {
		t.Fatalf("soft actions not filled: %+v", st.profile(def).Soft[0])
	}
	if d := e.diff(st.profile(def)); len(d) != 1 || d[0] != "light" {
		t.Fatalf("pending after the switch: %v", d)
	}
	st.profile(def).Lighting = 9

	// software actions and hardware actions of software buttons: no write
	p := *st.profile(def)
	p.Soft[3] = SoftBinding{Kind: SoftText, Text: "s3cret", Secret: true, After: "Enter"}
	p.Buttons[3] = ButtonAssign{Kind: "mouse", Code: [3]int{16, 8, 0}}
	if c, m := post(t, a, "profile/save", p); c != 200 {
		t.Fatal(m)
	}
	if b := st.profile(def).Soft[3]; b.Text != "" || b.Enc != "enc:s3cret" {
		t.Fatalf("secret stored as %+v", b)
	}
	_, all := post(t, a, "all", nil)
	for _, pr := range all["data"].(map[string]any)["profiles"].([]any) {
		sb := pr.(map[string]any)["soft"].([]any)[3].(map[string]any)
		if _, has := sb["text"]; has {
			t.Fatalf("secret text sent to the UI: %v", sb)
		}
	}
	if len(e.diff(st.profile(def))) != 0 {
		t.Fatal("software change pending a write")
	}
	// unknown macro, broken action
	q := *st.profile(def)
	q.Soft[1] = SoftBinding{Kind: SoftMacro, MacroID: "nope"}
	if c, _ := post(t, a, "profile/save", q); c != 400 {
		t.Fatal("unknown macro accepted")
	}
	q.Soft[1] = SoftBinding{Kind: SoftKeys, Keys: "Nope"}
	if c, _ := post(t, a, "profile/save", q); c != 400 {
		t.Fatal("broken combo accepted")
	}
	// left click only on a software button → refused
	q = *st.profile(def)
	q.Buttons[2] = ButtonAssign{Kind: "mouse", Code: [3]int{16, 1, 0}}
	q.Buttons[11] = ButtonAssign{Kind: "mouse", Code: [3]int{16, 2, 0}}
	if c, _ := post(t, a, "profile/save", q); c != 400 {
		t.Fatal("profile without a hardware left click accepted")
	}
	// the settings form does not change the mode
	set := st.data.Settings
	set.ButtonMode, set.HWButtons = ModeHardware, [softSlots]bool{true}
	if c, _ := post(t, a, "settings", map[string]any{"settings": set}); c != 200 || !softMode(st.data.Settings) || st.data.Settings.HWButtons == set.HWButtons {
		t.Fatal("settings save changed the button mode")
	}
	if c, m := post(t, a, "state", nil); c != 200 || m["soft"].(map[string]any)["enabled"] != true {
		t.Fatalf("state: %v", m["soft"])
	}

	// button 2 becomes a hardware button: keys written
	hw := st.data.Settings.HWButtons
	hw[1] = true
	if c, m := post(t, a, "buttons/hw", map[string]any{"hw": hw}); c != 200 || m["ok"] != true {
		t.Fatal(m)
	}
	want = carriedMask(st.data.Settings)
	if cmds(takeSent()) != "1,1" || st.state.Carrier != want || want[1] {
		t.Fatal("hw change")
	}
	// button 16 (DPI−) as a software button: browser-stop consumer key; the
	// left click (button 12) can never become one
	hw[15], hw[lockedSlot] = false, false
	if c, m := post(t, a, "buttons/hw", map[string]any{"hw": hw}); c != 200 || m["ok"] != true {
		t.Fatal(m)
	}
	s16 := takeSent()
	if !st.data.Settings.HWButtons[lockedSlot] || st.data.Settings.HWButtons[15] || len(s16) != 2 || !strings.Contains(fmt.Sprintf("% x", s16[0]), "80 26 02") {
		t.Fatalf("button 16: hw %v, sent % x", st.data.Settings.HWButtons, s16)
	}
	if !st.state.Carrier[15] || st.state.Carrier[lockedSlot] {
		t.Fatal("carrier state for buttons 12/16")
	}
	hw[15] = true
	post(t, a, "buttons/hw", map[string]any{"hw": hw})
	takeSent()
	// back to software: its action is still there
	hw[1] = false
	post(t, a, "buttons/hw", map[string]any{"hw": hw})
	takeSent()
	if st.profile(def).Soft[1].Keys != "2" {
		t.Fatal("software action lost")
	}

	// exit with "restore": hardware layout, then the carrier again
	if err := e.ApplyPlain(def, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if s := takeSent(); cmds(s) != "1,1" || st.state.Carrier != ([softSlots]bool{}) {
		t.Fatalf("plain apply sent %s", cmds(s))
	}
	e.ApplyKeys(def, "start", true)
	if cmds(takeSent()) != "1,1" || st.state.Carrier != carriedMask(st.data.Settings) {
		t.Fatal("carrier not rewritten")
	}

	// hardware mode: normal layout, software actions kept
	if c, m := post(t, a, "buttons/mode", map[string]string{"mode": "hardware"}); c != 200 || m["ok"] != true {
		t.Fatal(m)
	}
	if cmds(takeSent()) != "1,1" || st.state.Carrier != ([softSlots]bool{}) || st.profile(def).Soft[3].Enc == "" {
		t.Fatal("hardware mode")
	}
	if c, _ := post(t, a, "buttons/mode", map[string]string{"mode": "x"}); c != 400 {
		t.Fatal("bad mode accepted")
	}
	// mouse offline: the mode is kept, the write waits for the mouse
	e.handleReport(report(false, 1, 1, 9))
	if c, m := post(t, a, "buttons/mode", map[string]string{"mode": "software"}); c != 200 || m["ok"] != false || !softMode(st.data.Settings) {
		t.Fatalf("offline: %v", m)
	}
	e.handleReport(report(true, 1, 1, 9))
	waitIdle(e)
	if cmds(takeSent()) != "1,1" || st.state.Carrier != carriedMask(st.data.Settings) {
		t.Fatal("pending carrier write")
	}
	if !st.data.Settings.HWButtons[9] || st.data.Settings.HWButtons[1] {
		t.Fatal("the hardware/software split must survive mode changes")
	}

	// a macro used by a software button cannot be deleted
	c, m := post(t, a, "macro/save", Macro{Name: "a", ExecuteType: 1, Events: []MacroEvent{{Type: "keydown", Code: 4}, {Type: "keyup", Code: 4}}})
	if c != 200 {
		t.Fatal(m)
	}
	q = *st.profile(def)
	q.Soft[4] = SoftBinding{Kind: SoftMacro, MacroID: m["id"].(string)}
	if c, m := post(t, a, "profile/save", q); c != 200 {
		t.Fatal(m)
	}
	if c, _ := post(t, a, "macro/delete", map[string]string{"id": m["id"].(string)}); c != 400 {
		t.Fatal("macro used by a software button deleted")
	}
	// first choice "hardware" writes nothing
	st2, e2 := setup(t)
	a2, _ := newAPI(e2, st2, func(f func()) { f() })
	e2.handleReport(report(true, 1, 1, 9))
	e2.Apply(st2.data.Settings.DefaultProfile, false, "t", false, true)
	takeSent()
	if c, _ := post(t, a2, "buttons/mode", map[string]string{"mode": "hardware"}); c != 200 || len(takeSent()) != 0 || st2.data.Settings.ButtonMode != ModeHardware {
		t.Fatal("first choice hardware")
	}
}

// 1.x data (version 1): key repeat on, mode not chosen.
func TestLoad1x(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"version":1,"profiles":[{"id":"p1","name":"A","buttons":[],"dpiCount":1,"dpiLevels":[800],"pollRate":1,"lod":1,"sleepMin":5}],"settings":{"defaultProfile":"p1","baseProfile":"p1"}}`), 0644)
	st := loadStore(dir)
	if st.data.Version != dataVersion || !st.data.Settings.KeyRepeat || st.data.Settings.ButtonMode != "" || st.data.Soft != nil {
		t.Fatalf("%+v", st.data.Settings)
	}
	for i := 0; i < softSlots; i++ {
		if st.data.Settings.HWButtons[i] != (i >= lockedSlot) {
			t.Fatalf("hw %v", st.data.Settings.HWButtons)
		}
	}
}
