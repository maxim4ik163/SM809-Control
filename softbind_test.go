package main

import (
	"encoding/hex"
	"strings"
	"testing"
)

// Carrier layout of the default SM809 profile. The expected packets were
// computed independently from docs/PROTOCOL.md (cmd 1, sub 2 / 18, button
// order 11,12,13,14,15,9,10,0,1 | 2,3,4,5,8,6,7, pad FF to 30 bytes, checksum
// (6 + Σ) & FF inserted at index 3), seq 0.
func TestFwOutput(t *testing.T) {
	cases := []struct {
		code [3]int
		kind string
		want string // combo or mouse button; "" = not carried
	}{
		{[3]int{112, 8, 7}, SoftKeys, "Win+D"},
		{[3]int{112, 1, 6}, SoftKeys, "Ctrl+C"},
		{[3]int{112, 5, 0}, SoftKeys, "Ctrl+Alt"}, // modifiers only
		{[3]int{112, 0, 31}, SoftKeys, "2"},
		{[3]int{112, 0, 88}, SoftKeys, "NumEnter"},
		{[3]int{112, 0, 104}, SoftKeys, "F13"},
		{[3]int{112, 0, 229}, SoftKeys, "Shift"},
		{[3]int{112, 0, 250}, "", ""}, // unknown usage
		{[3]int{128, 148, 1}, SoftKeys, "MyComputer"},
		{[3]int{128, 233, 0}, SoftKeys, "VolumeUp"},
		{[3]int{128, 131, 1}, SoftKeys, "MediaPlayer"},
		{[3]int{128, 1, 2}, "", ""},
		{[3]int{16, 16, 0}, SoftMouse, "forward"},
		{[3]int{16, 8, 0}, SoftMouse, "back"},
		{[3]int{16, 2, 0}, SoftMouse, "right"},
		{[3]int{16, 1, 0}, "", ""}, // the left click is never carried
		{[3]int{96, 0, 0}, SoftDisable, ""},
		{[3]int{96, 4, 0}, "", ""},  // lighting toggle
		{[3]int{64, 2, 0}, "", ""},  // DPI +
		{[3]int{80, 3, 0}, "", ""},  // DPI lock
		{[3]int{48, 50, 3}, "", ""}, // rapid fire
	}
	for _, c := range cases {
		o, ok := fwOutput(ButtonAssign{Kind: "x", Code: c.code})
		if ok != (c.kind != "") {
			t.Errorf("%v: carried = %v", c.code, ok)
			continue
		}
		if !ok {
			continue
		}
		got := o.Button
		if o.Kind == SoftKeys {
			got = o.Combo.String()
		}
		if o.Kind != c.kind || got != c.want {
			t.Errorf("%v: got %s %q, want %s %q", c.code, o.Kind, got, c.kind, c.want)
		}
	}
	if _, ok := fwOutput(ButtonAssign{Kind: "macro", Code: [3]int{112, 0, 4}}); ok {
		t.Error("a macro must stay on the mouse")
	}
}

// Every key the Buttons tab offers (web/keys.js KEY_GROUPS) must translate.
func TestHIDUsageCoverage(t *testing.T) {
	var usages []int
	for u := 4; u <= 57; u++ {
		if u != 50 {
			usages = append(usages, u)
		}
	}
	for u := 58; u <= 101; u++ {
		usages = append(usages, u)
	}
	for u := 104; u <= 115; u++ {
		usages = append(usages, u)
	}
	for u := 224; u <= 231; u++ {
		usages = append(usages, u)
	}
	for _, u := range usages {
		if _, ok := fwOutput(ButtonAssign{Code: [3]int{112, 0, u}}); !ok {
			t.Errorf("usage %d does not translate", u)
		}
	}
	for c, n := range consumerName {
		if _, err := parseCombo(n); err != nil {
			t.Errorf("consumer %#x → %q: %v", c, n, err)
		}
	}
}

func TestSoftSlots(t *testing.T) {
	seen := map[uint32]bool{}
	for i := 0; i < softSlots; i++ {
		if softSlotIndex(softSlotName(i)) != i {
			t.Fatal(i)
		}
		if i == lockedSlot {
			continue
		}
		if carrierSlotOfVK(carrierVK(i)) != i || seen[carrierVK(i)] {
			t.Fatal("carrier VK of button", i+1)
		}
		seen[carrierVK(i)] = true
	}
	for _, s := range []string{"b0", "b17", "b01", "x1", "b", ""} {
		if softSlotIndex(s) != -1 {
			t.Errorf("%q accepted", s)
		}
	}
	if carrierSlotOfVK(0x7B) != -1 || carrierSlotOfVK(0x88) != -1 || carrierSlotOfVK(0) != -1 {
		t.Error("F12 / VK 0x88 are not carrier keys")
	}
	want := map[int]uint32{0: 0x7C, 10: 0x86, 12: 0x87, 13: 0x1C, 14: 0x1D, 15: 0xC1}
	for i, vk := range want {
		if carrierVK(i) != vk {
			t.Errorf("button %d: VK %#x, want %#x", i+1, carrierVK(i), vk)
		}
	}
	k, ok := keyByVK[0x7C]
	if !ok || k.Name != "F13" {
		t.Error("VK_F13")
	}
}

func softSettings(hw [softSlots]bool) Settings {
	return Settings{ButtonMode: ModeSoftware, HWButtons: hw}
}

// Carrier layout of the default SM809 profile. The expected packets were
// computed independently from docs/PROTOCOL.md (cmd 1, sub 2 / 18, button
// order 11,12,13,14,15,9,10,0,1 | 2,3,4,5,8,6,7, pad FF to 30 bytes, checksum
// (6 + Σ) & FF inserted at index 3), seq 0.
func TestCarrierPackets(t *testing.T) {
	p := newProfile("t", 0)
	hw := defaultHW(p)
	for i := 0; i < softSlots; i++ {
		if hw[i] != (i == 9 || i >= lockedSlot) { // rapid fire and buttons 12..16 start as hardware buttons
			t.Fatalf("defaultHW[%d] = %v", i, hw[i])
		}
	}
	set := softSettings(hw)
	m := carriedMask(set)
	c := deviceProfile(p, set)
	var codes [numButtons]KeyCode
	for i, b := range c.Buttons {
		codes[i] = KeyCode{byte(b.Code[0]), byte(b.Code[1]), byte(b.Code[2])}
	}
	pk := EncodeKeys(codes, func() byte { return 0 })
	exp := []string{
		"0100029a100100100200100400400200400300301002700072700068700069",
		"0100121e70006a70006b70006c70006d70007070006e70006fffffffffffff",
	}
	for i := range exp {
		if got := hex.EncodeToString(pk[i]); got != exp[i] {
			t.Errorf("packet %d:\n got %s\nwant %s", i, got, exp[i])
		}
	}
	if p.Buttons[0].Code != [3]int{112, 8, 7} {
		t.Fatal("deviceProfile changed its argument")
	}
	if carrierInMemory(c) != m || carrierInMemory(p) != ([softSlots]bool{}) {
		t.Fatal("carrierInMemory")
	}
	for i := 11; i < 16; i++ {
		if c.Buttons[i] != p.Buttons[i] {
			t.Fatalf("button %d changed", i+1)
		}
	}
	// hardware mode and a mode not chosen yet: no carrier
	for _, mode := range []string{"", ModeHardware} {
		if deviceProfile(p, Settings{ButtonMode: mode, HWButtons: hw}) != p || carriedMask(Settings{ButtonMode: mode}) != ([softSlots]bool{}) {
			t.Fatalf("mode %q writes a carrier", mode)
		}
	}
}

func TestDeviceProfileSignature(t *testing.T) {
	p := newProfile("t", 0)
	set := softSettings(defaultHW(p))
	sig := func(s Settings) map[string]string {
		m := map[string]string{}
		for _, c := range buildCommands(deviceProfile(p, s), func(string) *Macro { return nil }, 0x11) {
			m[c.key] = signature(c)
		}
		return m
	}
	a, b := sig(Settings{ButtonMode: ModeHardware}), sig(set)
	for k := range a {
		if (a[k] != b[k]) != (k == "keys") {
			t.Errorf("%s: software mode should change only the keys command", k)
		}
	}
	// hardware action of a software button, or a software action: no write
	p.Buttons[3] = ButtonAssign{Kind: "mouse", Code: [3]int{16, 8, 0}}
	p.Soft[3] = SoftBinding{Kind: SoftText, Text: "x"}
	if sig(set)["keys"] != b["keys"] {
		t.Error("software button change needs no write")
	}
	// a hardware button: written
	p.Buttons[9] = ButtonAssign{Kind: "fire", Code: [3]int{48, 50, 3}}
	if sig(set)["keys"] == b["keys"] {
		t.Error("hardware button change must be written")
	}
	// left-click guard
	q := newProfile("q", 0)
	q.Buttons[2] = ButtonAssign{Kind: "mouse", Code: [3]int{16, 1, 0}}
	q.Buttons[11] = ButtonAssign{Kind: "mouse", Code: [3]int{16, 2, 0}}
	if !deviceHasLeft(q, Settings{ButtonMode: ModeHardware}) || deviceHasLeft(q, softSettings([softSlots]bool{})) {
		t.Fatal("deviceHasLeft")
	}
	if defaultHW(q)[2] != true {
		t.Fatal("a left click must default to a hardware button")
	}
}

func TestSoftFromFirmware(t *testing.T) {
	p := newProfile("t", 0)
	p.Buttons[4] = ButtonAssign{Kind: "macro", Code: [3]int{144, 0, 0}, MacroID: "m1"}
	p.Buttons[5] = ButtonAssign{Kind: "dpi", Code: [3]int{64, 2, 0}}
	hw := defaultHW(p)
	if hw[4] || !hw[5] || !hw[9] || hw[0] {
		t.Fatalf("defaultHW %v", hw)
	}
	fillSoft(p, hw)
	want := map[int]SoftBinding{
		0:  {Kind: SoftKeys, Keys: "Win+D"},
		1:  {Kind: SoftKeys, Keys: "2"},
		3:  {Kind: SoftMouse, Button: "forward"},
		4:  {Kind: SoftMacro, MacroID: "m1"},
		5:  {},
		9:  {},
		10: {Kind: SoftKeys, Keys: "MyComputer"},
	}
	for i, w := range want {
		if p.Soft[i] != w {
			t.Errorf("button %d: %+v, want %+v", i+1, p.Soft[i], w)
		}
	}
	// fillSoft keeps what is already set
	p.Soft[1] = SoftBinding{Kind: SoftRun, Path: "x"}
	fillSoft(p, [softSlots]bool{})
	if p.Soft[1].Kind != SoftRun {
		t.Fatal("fillSoft overwrote an action")
	}
	// profile copies do not share actions
	c := *p
	c.Soft[1].Path = "y"
	if p.Soft[1].Path != "x" {
		t.Fatal("Soft must be a value")
	}
}

func TestSoftOutputAndRules(t *testing.T) {
	d := defaultData()
	g := newProfile("Game", 0)
	d.Profiles = append(d.Profiles, g)
	d.Rules = []Rule{{Exe: "off.exe", ProfileID: g.ID}, {Exe: "game.exe", ProfileID: g.ID, Enabled: true, OSD: true}}
	if id, r := ruleProfileFor(d, `C:\G\GAME.EXE`); id != g.ID || r == nil || !r.OSD {
		t.Fatal("rule")
	}
	if id, _ := ruleProfileFor(d, "off.exe"); id != "" {
		t.Fatal("disabled rule used")
	}
	g.Soft[0] = SoftBinding{Kind: SoftKeys, Keys: "Ctrl+Z"}
	g.Soft[1] = SoftBinding{Kind: SoftMouse, Button: "middle"}
	g.Soft[2] = SoftBinding{Kind: SoftMacro, MacroID: "m1"}
	g.Soft[3] = SoftBinding{Kind: SoftDisable}
	m := &Macro{ID: "m1", Name: "M", ExecuteType: 3, Events: []MacroEvent{{Type: "keydown", Code: 4}, {Type: "keyup", Code: 4}}}
	mo := func(id string) *Macro {
		if id == "m1" {
			return m
		}
		return nil
	}
	if o, ok := softOutput(g, 0, mo); !ok || o.Combo.String() != "Ctrl+Z" {
		t.Fatal("keys")
	}
	if o, ok := softOutput(g, 1, mo); !ok || o.Button != "middle" {
		t.Fatal("mouse")
	}
	if o, ok := softOutput(g, 2, mo); !ok || o.Macro == m || o.Macro.Name != "M" {
		t.Fatal("macro output must be a copy")
	}
	if _, ok := softOutput(g, 2, func(string) *Macro { return nil }); ok {
		t.Fatal("deleted macro played")
	}
	if o, ok := softOutput(g, 3, mo); !ok || o.Kind != SoftDisable {
		t.Fatal("disable")
	}
	if _, ok := softOutput(g, 4, mo); ok {
		t.Fatal("unset action does something")
	}
	if softMacroUse(d, "m1") == "" || softMacroUse(d, "m2") != "" {
		t.Fatal("softMacroUse")
	}
}

func TestValidateSoftBinding(t *testing.T) {
	ok := func(b SoftBinding) SoftBinding {
		t.Helper()
		if err := validateSoftBinding(0, &b); err != nil {
			t.Fatalf("%+v: %v", b, err)
		}
		return b
	}
	bad := func(b SoftBinding) {
		t.Helper()
		if err := validateSoftBinding(0, &b); err == nil {
			t.Fatalf("%+v accepted", b)
		}
	}
	b := ok(SoftBinding{Kind: SoftText, Text: "привет", After: "enter", CharDelay: 900, Keys: "X", Path: "y", Force: true})
	if b.Method != TextUnicode || b.After != "Enter" || b.CharDelay != maxSoftCharDelay || b.Keys != "" || b.Path != "" || b.Force {
		t.Fatalf("text not normalised: %+v", b)
	}
	if ok(SoftBinding{Kind: SoftText, Text: "x", Method: TextKeys, Layout: "4090409"}).Layout != "4090409" {
		t.Fatal("layout dropped")
	}
	if ok(SoftBinding{Kind: SoftText, Text: "x", Layout: "409"}).Layout != "" {
		t.Fatal("layout kept for Unicode input")
	}
	if ok(SoftBinding{Kind: SoftKeys, Keys: "z+ctrl"}).Keys != "Ctrl+Z" {
		t.Fatal("combo")
	}
	ok(SoftBinding{})
	ok(SoftBinding{Kind: SoftText, Enc: "dpapi:xx", Secret: true})
	bad(SoftBinding{Kind: SoftText})
	bad(SoftBinding{Kind: SoftText, Text: "x", Method: "paste"})
	bad(SoftBinding{Kind: SoftText, Text: "x", Method: TextKeys, Layout: "zz"})
	bad(SoftBinding{Kind: SoftText, Text: "x", After: "Nope"})
	bad(SoftBinding{Kind: SoftText, Text: strings.Repeat("a", maxSoftText+1)})
	bad(SoftBinding{Kind: SoftText, Enc: "dpapi:xx"}) // secret switched off without a new text
	bad(SoftBinding{Kind: SoftMacro})
	bad(SoftBinding{Kind: SoftKeys, Keys: "Ctrl+Nope"})
	bad(SoftBinding{Kind: SoftMouse, Button: "side"})
	bad(SoftBinding{Kind: "default"})
	bad(SoftBinding{Kind: SoftRun, Path: "  "})
	if p := ok(SoftBinding{Kind: SoftRun, Path: ` "C:\Tools\a.exe" `}).Path; p != `C:\Tools\a.exe` {
		t.Fatalf("path %q", p)
	}

	p := newProfile("t", 0)
	p.Soft[1] = SoftBinding{Kind: SoftText, Text: "pw", Secret: true}
	old := protectSecret
	defer func() { protectSecret = old }()
	protectSecret = nil
	if sealSecrets(p) == nil {
		t.Fatal("sealed without encryption")
	}
	protectSecret = func(s string) (string, error) { return "enc:" + s, nil }
	if err := sealSecrets(p); err != nil || p.Soft[1].Text != "" || p.Soft[1].Enc != "enc:pw" {
		t.Fatalf("seal: %v %+v", err, p.Soft[1])
	}
}

// 1.5 beta data: global bindings → every profile, program profiles → new
// profiles + rules, the set of software buttons kept as on the mouse.
func TestMigrateBeta(t *testing.T) {
	d := defaultData()
	base := d.Profiles[0]
	other := newProfile("Other", 0)
	d.Profiles = append(d.Profiles, other)
	d.Rules = []Rule{{Exe: "taken.exe", ProfileID: other.ID, Enabled: true}}
	d.Settings.SoftBindings = true
	d.Soft = &SoftData{
		Global: map[string]SoftBinding{
			"b4": {Kind: SoftKeys, Keys: "Ctrl+Win+Right", Force: true},
			"b2": {Kind: softLegacyDefault},
			"b3": {Kind: SoftKeys, Keys: "Broken"},
		},
		Profiles: []*SoftProfile{{Name: "CAD", Exes: []string{"acad.exe", "taken.exe"}, Enabled: true, OSD: true,
			Bindings: map[string]SoftBinding{"b5": {Kind: SoftMouse, Button: "middle"}, "b4": {Kind: softLegacyDefault}}}},
		KeyRepeat: true, RestoreOnExit: true,
	}
	var carrier [softSlots]bool
	for i := range carrier {
		carrier[i] = i != 9
	}
	migrateBeta(d, carrier, base)
	if d.Soft != nil || d.Settings.SoftBindings || d.Settings.ButtonMode != ModeSoftware || !d.Settings.KeyRepeat || !d.Settings.RestoreOnExit {
		t.Fatalf("settings %+v", d.Settings)
	}
	if d.Settings.HWButtons != defaultHW(base) {
		t.Fatalf("hw %v", d.Settings.HWButtons)
	}
	for _, p := range []*Profile{base, other} {
		if p.Soft[3] != (SoftBinding{Kind: SoftKeys, Keys: "Ctrl+Win+Right"}) || p.Soft[1] != (SoftBinding{Kind: SoftKeys, Keys: "2"}) || p.Soft[2] != (SoftBinding{Kind: SoftKeys, Keys: "1"}) {
			t.Fatalf("%s: %+v", p.Name, p.Soft)
		}
	}
	if len(d.Profiles) != 3 {
		t.Fatal("program profile not created")
	}
	cad := d.Profiles[2]
	if cad.Name != "CAD" || cad.Soft[4] != (SoftBinding{Kind: SoftMouse, Button: "middle"}) || cad.Soft[3] != (SoftBinding{Kind: SoftMouse, Button: "forward"}) || cad.ID == base.ID {
		t.Fatalf("cad %+v", cad.Soft)
	}
	if len(d.Rules) != 2 || d.Rules[1] != (Rule{Exe: "acad.exe", ProfileID: cad.ID, OSD: true, Enabled: true}) {
		t.Fatalf("rules %+v", d.Rules)
	}
	// carrier never written: the default split
	d2 := defaultData()
	d2.Settings.SoftBindings = true
	d2.Soft = &SoftData{}
	migrateBeta(d2, [softSlots]bool{}, d2.Profiles[0])
	if d2.Settings.HWButtons != defaultHW(d2.Profiles[0]) {
		t.Fatal("default split")
	}
	// beta with the mode off: stays undecided (asked at start)
	d3 := defaultData()
	d3.Soft = &SoftData{}
	migrateBeta(d3, carrier, d3.Profiles[0])
	if d3.Settings.ButtonMode != "" {
		t.Fatal("mode chosen for the user")
	}
}

// Every button but the left click as a software button: buttons 13..16 get
// F24 and the International keys; button 12 keeps the left click. Expected
// packets computed independently (same rules as TestCarrierPackets).
func TestCarrierAllButtons(t *testing.T) {
	p := newProfile("t", 0)
	set := softSettings([softSlots]bool{})
	m := carriedMask(set)
	if m[lockedSlot] {
		t.Fatal("the left click must never be carried")
	}
	c := deviceProfile(p, set)
	if !deviceHasLeft(p, set) {
		t.Fatal("left click lost")
	}
	var codes [numButtons]KeyCode
	for i, b := range c.Buttons {
		codes[i] = KeyCode{byte(b.Code[0]), byte(b.Code[1]), byte(b.Code[2])}
	}
	pk := EncodeKeys(codes, func() byte { return 0 })
	exp := []string{
		"0100025d10010070007370008a70008b700087700071700072700068700069",
		"0100121e70006a70006b70006c70006d70007070006e70006fffffffffffff",
	}
	for i := range exp {
		if got := hex.EncodeToString(pk[i]); got != exp[i] {
			t.Errorf("packet %d:\n got %s\nwant %s", i, got, exp[i])
		}
	}
	if carrierInMemory(c) != m {
		t.Fatal("carrierInMemory")
	}
	// a macro whose bytes look like a carrier is not one
	q := newProfile("q", 0)
	q.Buttons[13] = ButtonAssign{Kind: "macro", Code: [3]int{128, 0x21, 0x02}}
	if carrierInMemory(q)[13] {
		t.Fatal("macro taken for a carrier")
	}
}

func TestHookEventKind(t *testing.T) {
	if hookEventKind(0x7C, false, 0) != hookKeyboard || hookEventKind(0x41, false, 0) != hookKeyboard {
		t.Fatal("real key presses")
	}
	if hookEventKind(0xAA, true, 0) != hookIgnore || hookEventKind(0x7C, true, injectedMark) != hookIgnore {
		t.Fatal("injected input must be ignored")
	}
}

// Carrier keys are matched by virtual key, or by scan code when a layout
// reports another virtual key for the International keys.
func TestCarrierSlotOf(t *testing.T) {
	cases := []struct {
		vk, scan uint32
		ext      bool
		want     int
	}{
		{0x7C, 0x64, false, 0},
		{0x86, 0x6E, false, 10},
		{0x87, 0x76, false, 12},
		{0x1C, 0x79, false, 13},
		{0x1D, 0x7B, false, 14},
		{0xC1, 0x73, false, 15},
		{0xFF, 0x73, false, 15}, // Ro under a layout with another VK
		{0xFF, 0x79, false, 13}, // seen on the user's PC: Henkan arrives as VK 0xFF (probe 3)
		{0xEB, 0x7B, false, 14}, // and Muhenkan as VK 0xEB
		{0xFF, 0x00, false, -1}, // VK 0xFF alone (unmapped keys) is never a carrier
		{0x00, 0x79, false, 13},
		{0x41, 0x1E, false, -1},
		{0x2E, 0x53, true, -1}, // Delete (extended) is not a carrier
		{0x00, 0x73, true, -1}, // extended scan 73: not ours
		{0xAA, 0, false, -1},   // the browser keys of 2.1.0/2.1.1 are not carriers any more
	}
	for _, c := range cases {
		if got := carrierSlotOf(c.vk, c.scan, c.ext); got != c.want {
			t.Errorf("vk %#x scan %#x ext %v: %d, want %d", c.vk, c.scan, c.ext, got, c.want)
		}
	}
	// carrier codes are distinct keyboard usages the button catalog never uses
	seen := map[[3]int]bool{}
	for i, c := range carriers {
		if i == lockedSlot {
			continue
		}
		if c.Code[0] != 112 || seen[c.Code] {
			t.Fatalf("button %d: %v", i+1, c.Code)
		}
		seen[c.Code] = true
	}
}
