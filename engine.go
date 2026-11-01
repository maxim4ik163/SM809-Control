//go:build windows

package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// DevStatus is the decoded status report of one HID path.
type DevStatus struct {
	Path     string    `json:"-"`
	PID      uint16    `json:"pid"`
	Mode     string    `json:"mode"` // wired, dongle, bt1, bt2
	Link     bool      `json:"link"`
	Level    int       `json:"level"`
	Rate     int       `json:"rate"`
	Battery  int       `json:"battery"`
	Charge   int       `json:"charge"`
	Light    int       `json:"light"`
	MaxRate  int       `json:"maxRate"`
	Sensor   int       `json:"sensor"`
	Updated  time.Time `json:"updated"`
	HasState bool      `json:"hasState"`
}

type stackEntry struct {
	Exe       string `json:"exe"`
	ProfileID string `json:"profileId"`
	OSD       bool   `json:"osd"`
}

type Engine struct {
	store *Store

	mu       sync.Mutex
	devs     map[string]*DevStatus
	stack    []stackEntry
	active   string // profile currently on the mouse (as far as we know)
	busy     bool
	progress string
	lastErr  string
	lastMsg  string
	pending  *applyJob
	seq      int

	jobs chan *applyJob
	bat  *BatHistory // may be nil

	// callbacks into the UI thread
	onChange func()           // state changed (tray tooltip, SSE)
	onOSD    func(osdContent) // show popup
	onNotify func(title, text string, warn bool)
}

type applyJob struct {
	profileID string
	force     bool
	reason    string
	osd       bool
	reset     bool
	done      chan error
}

func newEngine(st *Store) *Engine {
	e := &Engine{store: st, devs: map[string]*DevStatus{}, jobs: make(chan *applyJob, 16)}
	e.seq = st.state.lastSeq()
	go e.worker()
	return e
}

// ---------------------------------------------------------------------------
// status

func (e *Engine) modeFor(path string, pid uint16) string {
	switch pid {
	case pidWired:
		return "wired"
	case pidDongle:
		return "dongle"
	case pidBLE:
		s := &e.store.data.Settings
		for i, p := range s.BTPaths {
			if strings.EqualFold(p, path) {
				return fmt.Sprintf("bt%d", i+1)
			}
		}
		s.BTPaths = append(s.BTPaths, path)
		_ = e.store.saveData()
		return fmt.Sprintf("bt%d", len(s.BTPaths))
	}
	return fmt.Sprintf("pid%04x", pid)
}

func (e *Engine) handleReport(r statusReport) {
	d := r.Data
	if len(d) < 10 {
		return
	}
	e.mu.Lock()
	st := e.store
	st.mu.Lock()
	prev, had := e.devs[r.Path]
	if !had {
		prev = &DevStatus{}
	}
	ns := &DevStatus{
		Path: r.Path, PID: r.PID, Mode: e.modeFor(r.Path, r.PID),
		Link: d[1] != 0, Level: int(d[2]), Rate: int(d[3]), Battery: int(d[4]), Charge: int(d[5]),
		Light: int(d[6]), MaxRate: int(d[8]), Sensor: int(d[9]), Updated: time.Now(), HasState: true,
	}
	if ns.Battery > 100 {
		ns.Battery = 100
	}
	if ns.Charge == 2 {
		ns.Battery = 100
	}
	if !ns.Link {
		ns.Level, ns.Battery, ns.Charge, ns.Light = prev.Level, prev.Battery, prev.Charge, prev.Light
	}
	e.devs[r.Path] = ns
	logf("статус %s: % X", ns.Mode, d[:12])

	// one-time: seed the default profile from what the mouse reports, so that
	// the very first full sync does not silently change the user's settings
	if ns.Link && !st.state.Synced && !st.state.Seeded {
		if p := st.profile(st.data.Settings.DefaultProfile); p != nil {
			if validRate(ns.Rate) {
				p.PollRate = ns.Rate
			}
			if ns.Light >= 0 && ns.Light < len(lightingModes) {
				p.Lighting = ns.Light
			}
			if ns.Sensor != 0 {
				si := sensorFor(ns.Sensor)
				for i := range p.DPILevels {
					if p.DPILevels[i] > si.MaxDPI {
						p.DPILevels[i] = si.MaxDPI
					}
				}
			}
			if ns.Level < p.DPICount {
				p.DPIStart = ns.Level
			}
			st.state.Seeded = true
			_ = st.saveData()
			_ = st.saveState()
		}
	}
	settings := st.data.Settings
	activeP := st.profile(e.active)
	if activeP == nil {
		activeP = st.profile(settings.BaseProfile)
	}
	var dpiPopup *osdContent
	if settings.OSDDPI && prev.HasState && prev.Link && ns.Link && prev.Level != ns.Level && activeP != nil {
		dpiPopup = e.dpiOSD(activeP, ns.Level, settings.Popup)
	}
	pend := e.pending
	if ns.Link && pend != nil {
		e.pending = nil
	}
	st.mu.Unlock()
	e.mu.Unlock()

	if dpiPopup != nil && e.onOSD != nil {
		e.onOSD(*dpiPopup)
	}
	e.batteryNotify(prev, ns)
	if ns.Link && e.bat != nil {
		e.bat.add(ns.Battery, ns.Charge, ns.Mode, time.Now())
	}
	if ns.Link && pend != nil {
		e.jobs <- pend
	}
	e.changed()
}

func (e *Engine) dpiOSD(p *Profile, level int, style PopupStyle) *osdContent {
	val := fmt.Sprintf(T("Уровень %d", "Level %d"), level+1)
	if level >= 0 && level < 8 && p.DPILevels[level] > 0 {
		val = fmt.Sprintf("%d", p.DPILevels[level])
	}
	accent := parseHex(style.AccentColor, 0x4CAF50)
	if style.UseDPIColors && level >= 0 && level < 8 {
		accent = parseHex(p.DPIColors[level], accent)
	}
	n := p.DPICount
	if level+1 > n {
		n = level + 1
	}
	return &osdContent{Caption: "DPI", Value: val, Dots: n, Active: level, Accent: accent, Style: style}
}

var (
	lowNotified, critNotified bool
)

func (e *Engine) batteryNotify(prev, ns *DevStatus) {
	if e.onNotify == nil || !ns.Link {
		return
	}
	s := e.store.data.Settings
	charging := ns.Charge != 0
	if charging || (s.LowPercent > 0 && ns.Battery > s.LowPercent+5) {
		lowNotified = false
	}
	if charging || (s.CriticalPercent > 0 && ns.Battery > s.CriticalPercent+5) {
		critNotified = false
	}
	if !charging {
		if s.CriticalPercent > 0 && ns.Battery <= s.CriticalPercent && !critNotified {
			e.onNotify(T("SM809: очень низкий заряд", "SM809: battery critical"), fmt.Sprintf(T("Заряд мыши %d%%. Подключите зарядку.", "Mouse battery %d%%. Connect the charger."), ns.Battery)+e.leftText(), true)
			critNotified, lowNotified = true, true
		} else if s.LowPercent > 0 && ns.Battery <= s.LowPercent && !lowNotified {
			e.onNotify(T("SM809: низкий заряд", "SM809: battery low"), fmt.Sprintf(T("Заряд мыши %d%%.", "Mouse battery %d%%."), ns.Battery)+e.leftText(), true)
			lowNotified = true
		}
	}
	if s.NotifyFull && prev.HasState && prev.Link && prev.Charge == 1 && ns.Charge == 2 {
		e.onNotify("SM809", T("Мышь заряжена", "Mouse is fully charged"), false)
	}
	if s.NotifyLink && prev.HasState && prev.Link != ns.Link {
		if ns.Link {
			e.onNotify("SM809", T("Мышь снова на связи", "Mouse reconnected"), false)
		} else {
			e.onNotify("SM809", T("Мышь не на связи", "Mouse disconnected"), false)
		}
	}
}

func (e *Engine) deviceGone(path string) {
	e.mu.Lock()
	delete(e.devs, path)
	e.mu.Unlock()
	e.changed()
}

// primary returns the most relevant connected device status.
func (e *Engine) primary() *DevStatus {
	order := map[string]int{"wired": 0, "dongle": 1}
	var best *DevStatus
	bestScore := 1 << 30
	for _, d := range e.devs {
		score := 10
		if o, ok := order[d.Mode]; ok {
			score = o
		}
		if !d.Link {
			score += 100
		}
		if score < bestScore {
			best, bestScore = d, score
		}
	}
	return best
}

func (e *Engine) sensorType() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if p := e.primary(); p != nil && p.Sensor != 0 {
		return p.Sensor
	}
	return 0x11
}

func (e *Engine) changed() {
	if e.onChange != nil {
		e.onChange()
	}
}

// ---------------------------------------------------------------------------
// command building

type devCommand struct {
	key    string
	writes int // flash sectors rewritten (for the wear counter)
	build  func(seq func() byte) [][]byte
}

func signature(c devCommand) string {
	zero := func() byte { return 0 }
	var parts []string
	for _, p := range c.build(zero) {
		parts = append(parts, hex.EncodeToString(p))
	}
	return strings.Join(parts, "|")
}

func validRate(r int) bool {
	switch r {
	case 8, 4, 2, 1, 17, 18, 20:
		return true
	}
	return false
}

func buildCommands(p *Profile, macroOf func(string) *Macro, sensor int) []devCommand {
	var codes [numButtons]KeyCode
	type mUp struct {
		btn int
		m   Macro
	}
	var macros []mUp
	for i, b := range p.Buttons {
		if b.Kind == "macro" {
			if m := macroOf(b.MacroID); m != nil {
				codes[i] = MacroButtonCode(i+1, m.ExecuteType, m.RepeatCount)
				macros = append(macros, mUp{i + 1, *m})
				continue
			}
		}
		codes[i] = KeyCode{byte(b.Code[0]), byte(b.Code[1]), byte(b.Code[2])}
	}
	levels := append([]int(nil), p.DPILevels[:]...)
	var colors [8]uint32
	for i, c := range p.DPIColors {
		colors[i] = parseColor(c, 0xEA5E56)
	}
	cmds := []devCommand{
		{"keys", 2 + 2*b2i(len(macros) > 0), func(seq func() byte) [][]byte {
			pk := EncodeKeys(codes, seq)
			if len(macros) > 0 { // web driver: erase, then every assigned macro
				pk = append(pk, EncodeMacroErase(seq))
				for _, mu := range macros {
					pk = append(pk, EncodeMacroUpload(mu.btn, mu.m, seq)...)
				}
			}
			return pk
		}},
		{"dpi", 2, func(seq func() byte) [][]byte {
			return [][]byte{
				EncodeDPI(2, levels, p.DPICount, p.DPIStart, sensor, seq),
				EncodeDPI(18, levels, p.DPICount, p.DPIStart, sensor, seq),
			}
		}},
		{"dpiColors", 1, func(seq func() byte) [][]byte { return [][]byte{EncodeDPIColors(colors, seq)} }},
		{"poll", 1, func(seq func() byte) [][]byte { return [][]byte{EncodePollRate(p.PollRate, seq)} }},
		{"lod", 1, func(seq func() byte) [][]byte { return [][]byte{EncodeLOD(p.LOD, seq)} }},
		{"power", 1, func(seq func() byte) [][]byte {
			return [][]byte{EncodePower(p.SleepMin, p.MoveWakeup, p.MoveLightOff, seq)}
		}},
		{"sensor", 1, func(seq func() byte) [][]byte {
			return [][]byte{EncodeSensor(p.AngleSnap, p.MotionSync, p.Ripple, seq)}
		}},
		{"light", 1, func(seq func() byte) [][]byte { return [][]byte{EncodeLighting(p.Lighting, seq)} }},
	}
	return cmds
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Pending changes of a profile relative to what is on the mouse.
func (e *Engine) diff(p *Profile) []string {
	sensor := e.sensorType()
	st := e.store
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []string
	for _, c := range buildCommands(p, st.macro, sensor) {
		if !st.state.Synced || st.state.Sent[c.key] != signature(c) {
			out = append(out, c.key)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// apply

func (e *Engine) nextSeq() byte {
	e.seq = e.seq%255 + 1 // 1..255 like the web driver (never 0)
	return byte(e.seq)
}

func (e *Engine) worker() {
	for j := range e.jobs {
		err := e.run(j)
		if j.done != nil {
			j.done <- err
		}
	}
}

// Apply queues a profile; returns when done (or immediately if wait=false).
func (e *Engine) Apply(profileID string, force bool, reason string, osd bool, wait bool) error {
	j := &applyJob{profileID: profileID, force: force, reason: reason, osd: osd}
	if wait {
		j.done = make(chan error, 1)
	}
	e.jobs <- j
	if wait {
		return <-j.done
	}
	return nil
}

func (e *Engine) pickChannel() (*cmdChannel, *DevStatus, error) {
	e.mu.Lock()
	prim := e.primary()
	e.mu.Unlock()
	if prim == nil {
		return nil, nil, errors.New(T("мышь не найдена", "mouse not found"))
	}
	if !prim.Link {
		return nil, prim, errors.New(T("мышь не на связи", "mouse is not connected"))
	}
	// the command collection sits on the same physical device (same PID) as the status one
	for _, d := range enumerateHID(false) {
		if d.PID == prim.PID && isCommandCollection(d) && samePhysical(d.Path, prim.Path) {
			ch, err := openCmdChannel(d)
			return ch, prim, err
		}
	}
	for _, d := range enumerateHID(false) {
		if d.PID == prim.PID && isCommandCollection(d) {
			ch, err := openCmdChannel(d)
			return ch, prim, err
		}
	}
	return nil, prim, errors.New(T("не найден канал команд мыши", "mouse command interface not found"))
}

// samePhysical compares HID interface paths up to the collection suffix
// (…&col01#…, …&col02#…): collections of one device share the prefix.
func samePhysical(a, b string) bool {
	cut := func(s string) string {
		s = strings.ToLower(s)
		if i := strings.Index(s, "&col"); i >= 0 {
			return s[:i]
		}
		return s
	}
	return cut(a) == cut(b)
}

func (e *Engine) setBusy(b bool, msg string) {
	e.mu.Lock()
	e.busy = b
	e.progress = msg
	e.mu.Unlock()
	e.changed()
}

// FactoryReset sends the reset command through the worker (keeps seq ordered).
func (e *Engine) FactoryReset() { e.jobs <- &applyJob{reset: true} }

func (e *Engine) runReset() error {
	ch, _, err := e.pickChannel()
	if err != nil {
		e.fail(err)
		return err
	}
	defer ch.close()
	e.setBusy(true, T("Сброс мыши", "Resetting the mouse"))
	defer e.setBusy(false, "")
	if err := ch.send(EncodeReset(e.nextSeq)); err != nil {
		e.persistSeq()
		e.fail(err)
		return err
	}
	st := e.store
	st.mu.Lock()
	st.state.Synced = false
	st.state.Sent = map[string]string{}
	st.state.FlashWrites += 3
	st.state.Seq = e.seq
	_ = st.saveState()
	st.mu.Unlock()
	e.mu.Lock()
	e.active = ""
	e.lastErr = ""
	e.lastMsg = T("Мышь сброшена к заводским настройкам. Запишите профиль целиком, чтобы вернуть свои настройки.", "The mouse was reset to factory settings. Write a profile in full to restore your settings.")
	e.mu.Unlock()
	e.changed()
	return nil
}

func (e *Engine) run(j *applyJob) error {
	if j.reset {
		return e.runReset()
	}
	st := e.store
	st.mu.Lock()
	p := st.profile(j.profileID)
	if p == nil {
		st.mu.Unlock()
		return errors.New(T("профиль не найден", "profile not found"))
	}
	pc := *p
	synced := st.state.Synced
	st.mu.Unlock()

	sensor := e.sensorType()
	cmds := buildCommands(&pc, func(id string) *Macro {
		st.mu.Lock()
		defer st.mu.Unlock()
		if m := st.macro(id); m != nil {
			c := *m
			return &c
		}
		return nil
	}, sensor)

	var todo []devCommand
	st.mu.Lock()
	for _, c := range cmds {
		if j.force || !synced || st.state.Sent[c.key] != signature(c) {
			todo = append(todo, c)
		}
	}
	st.mu.Unlock()

	showOSD := func() {
		if j.osd && e.onOSD != nil {
			st.mu.Lock()
			style := st.data.Settings.Popup
			st.mu.Unlock()
			e.onOSD(osdContent{Caption: T("Профиль", "Profile"), Value: pc.Name, Accent: parseHex(style.AccentColor, 0x4CAF50), Style: style})
		}
	}

	if len(todo) == 0 {
		e.mu.Lock()
		e.active = pc.ID
		e.lastMsg = fmt.Sprintf(T("«%s»: изменений нет, мышь не трогали", "“%s”: nothing changed, the mouse was not written"), pc.Name)
		e.mu.Unlock()
		logf("применение %q (%s): без изменений", pc.Name, j.reason)
		showOSD()
		e.changed()
		return nil
	}

	ch, prim, err := e.pickChannel()
	if err != nil {
		if prim != nil && !prim.Link {
			e.mu.Lock()
			e.pending = &applyJob{profileID: j.profileID, force: j.force, reason: j.reason, osd: j.osd}
			e.lastMsg = fmt.Sprintf(T("«%s» будет применён, когда мышь появится на связи", "“%s” will be applied when the mouse reconnects"), pc.Name)
			e.mu.Unlock()
			e.changed()
		}
		e.fail(err)
		return err
	}
	defer ch.close()

	var keys []string
	for _, c := range todo {
		keys = append(keys, c.key)
	}
	logf("применение %q (%s), команды: %s", pc.Name, j.reason, strings.Join(keys, ","))
	e.setBusy(true, T("Запись в мышь: ", "Writing to the mouse: ")+pc.Name)
	defer e.setBusy(false, "")

	for _, c := range todo {
		pkts := c.build(e.nextSeq)
		for i, pk := range pkts {
			if err := ch.send(pk); err != nil {
				e.persistSeq()
				err = fmt.Errorf("%s: %v", c.key, err)
				e.fail(err)
				return err
			}
			if c.key == "keys" && i >= 2 {
				time.Sleep(120 * time.Millisecond) // macro packets, as in the web driver
			}
		}
		if c.key == "dpi" {
			time.Sleep(200 * time.Millisecond)
		}
		st.mu.Lock()
		st.state.Sent[c.key] = signature(c)
		st.state.FlashWrites += c.writes
		st.mu.Unlock()
	}
	st.mu.Lock()
	if len(todo) == len(cmds) {
		st.state.Synced = true
	}
	st.state.LastProfile = pc.ID
	st.state.LastApplied = time.Now()
	st.state.Seq = e.seq
	_ = st.saveState()
	st.mu.Unlock()

	e.mu.Lock()
	e.active = pc.ID
	e.lastErr = ""
	e.lastMsg = fmt.Sprintf(T("«%s» записан в мышь (%s)", "“%s” written to the mouse (%s)"), pc.Name, strings.Join(keys, ", "))
	e.mu.Unlock()
	showOSD()
	e.changed()
	return nil
}

func (e *Engine) persistSeq() {
	e.store.mu.Lock()
	e.store.state.Seq = e.seq
	_ = e.store.saveState()
	e.store.mu.Unlock()
}

func (e *Engine) fail(err error) {
	logf("ошибка: %v", err)
	e.mu.Lock()
	e.lastErr = err.Error()
	e.mu.Unlock()
	e.changed()
}

// ---------------------------------------------------------------------------
// process rules (switch only on exe start / exit)

func (e *Engine) onProcesses(running map[string]int) {
	st := e.store
	st.mu.Lock()
	rules := append([]Rule(nil), st.data.Rules...)
	base := st.data.Settings.BaseProfile
	osdOn := st.data.Settings.OSDProfile
	auto := st.data.Settings.AutoSwitch
	synced := st.state.Synced
	valid := map[string]bool{}
	for _, p := range st.data.Profiles {
		valid[p.ID] = true
	}
	st.mu.Unlock()

	e.mu.Lock()
	before := e.topProfileLocked(base)
	if !auto {
		rules = nil
		e.stack = nil // feature turned off → back to the manually selected profile
	}
	// drop exited
	var keep []stackEntry
	for _, s := range e.stack {
		if running[s.Exe] > 0 {
			keep = append(keep, s)
		}
	}
	e.stack = keep
	// add newly started (in rule order for determinism)
	inStack := map[string]bool{}
	for _, s := range e.stack {
		inStack[s.Exe] = true
	}
	var started []Rule
	for _, r := range rules {
		if r.Enabled && r.Exe != "" && running[r.Exe] > 0 && !inStack[r.Exe] && valid[r.ProfileID] {
			started = append(started, r)
			inStack[r.Exe] = true
		}
	}
	for _, r := range started {
		e.stack = append(e.stack, stackEntry{Exe: r.Exe, ProfileID: r.ProfileID, OSD: r.OSD})
	}
	after := e.topProfileLocked(base)
	osdWanted := osdOn
	if len(started) > 0 {
		osdWanted = osdOn && started[len(started)-1].OSD
	} else if len(e.stack) > 0 {
		osdWanted = osdOn && e.stack[len(e.stack)-1].OSD
	}
	e.mu.Unlock()

	if len(started) > 0 || after != before {
		e.changed()
	}
	if after != before {
		if !synced {
			logf("правило сработало, но мышь ещё ни разу не синхронизирована — пропуск")
			return
		}
		reason := "правило"
		if len(started) > 0 {
			reason = "запущен " + started[len(started)-1].Exe
		} else {
			reason = "закрыта игра"
		}
		_ = e.Apply(after, false, reason, osdWanted, false)
	}
}

func (e *Engine) topProfileLocked(base string) string {
	if n := len(e.stack); n > 0 {
		return e.stack[n-1].ProfileID
	}
	return base
}

// ---------------------------------------------------------------------------
// snapshot for UI / tray

type EngineSnapshot struct {
	Devices     []*DevStatus `json:"devices"`
	Primary     *DevStatus   `json:"primary"`
	Active      string       `json:"active"`
	Base        string       `json:"base"`
	Stack       []stackEntry `json:"stack"`
	Busy        bool         `json:"busy"`
	Progress    string       `json:"progress"`
	LastError   string       `json:"lastError"`
	LastMessage string       `json:"lastMessage"`
	Synced      bool         `json:"synced"`
	FlashWrites int          `json:"flashWrites"`
	Sensor      SensorInfo   `json:"sensor"`
	SensorType  int          `json:"sensorType"`
	Pending     string       `json:"pending"`
	Battery     BatForecast  `json:"battery"`
}

func (e *Engine) Snapshot() EngineSnapshot {
	e.mu.Lock()
	var s EngineSnapshot
	for _, d := range e.devs {
		c := *d
		s.Devices = append(s.Devices, &c)
	}
	sort.Slice(s.Devices, func(a, b int) bool { return s.Devices[a].Mode < s.Devices[b].Mode })
	if p := e.primary(); p != nil {
		c := *p
		s.Primary = &c
		s.SensorType = p.Sensor
	}
	s.Active = e.active
	s.Stack = append([]stackEntry(nil), e.stack...)
	s.Busy, s.Progress, s.LastError, s.LastMessage = e.busy, e.progress, e.lastErr, e.lastMsg
	if e.pending != nil {
		s.Pending = e.pending.profileID
	}
	e.mu.Unlock()
	e.store.mu.Lock()
	s.Base = e.store.data.Settings.BaseProfile
	s.Synced = e.store.state.Synced
	s.FlashWrites = e.store.state.FlashWrites
	e.store.mu.Unlock()
	if s.SensorType == 0 {
		s.SensorType = 0x11
	}
	s.Sensor = sensorFor(s.SensorType)
	if e.bat != nil {
		s.Battery = e.bat.forecast(time.Now())
	} else {
		s.Battery = BatForecast{State: "unknown", Basis: "none"}
	}
	return s
}

// leftText: " Хватит примерно на 1 д 3 ч." when a forecast is available.
func (e *Engine) leftText() string {
	if e.bat == nil {
		return ""
	}
	f := e.bat.forecast(time.Now())
	if f.State != "discharging" || f.Hours <= 0 {
		return ""
	}
	return T(" Хватит примерно на ", " Roughly ") + fmtHours(f.Hours) + T(".", " left.")
}
