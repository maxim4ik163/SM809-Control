//go:build windows

package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webFS embed.FS

type API struct {
	eng   *Engine
	store *Store
	token string
	url   string

	subMu sync.Mutex
	subs  map[chan struct{}]bool

	uiCall func(func()) // run on the UI thread (OSD etc.)

	readMu   sync.Mutex
	lastRead *mouseReadResult
}

type mouseReadResult struct {
	Profile     *Profile `json:"profile"`
	Macros      []*Macro `json:"macros"`
	Debounce    int      `json:"debounce"`
	Compared    string   `json:"compared"`    // name of the profile it was compared with
	Differences []string `json:"differences"` // command groups that differ from it
}

func newAPI(eng *Engine, st *Store, uiCall func(func())) (*API, error) {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	a := &API{eng: eng, store: st, token: hex.EncodeToString(b), subs: map[chan struct{}]bool{}, uiCall: uiCall}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	a.url = fmt.Sprintf("http://%s/?t=%s", ln.Addr().String(), a.token)
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	files := http.FileServer(http.FS(sub))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(w, r)
	}))
	mux.HandleFunc("/api/", a.dispatch)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return a, nil
}

// notify wakes all SSE subscribers.
func (a *API) notify() {
	a.subMu.Lock()
	for c := range a.subs {
		select {
		case c <- struct{}{}:
		default:
		}
	}
	a.subMu.Unlock()
}

type apiError struct{ Error string `json:"error"` }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *API) dispatch(w http.ResponseWriter, r *http.Request) {
	tok := r.Header.Get("X-Token")
	if tok == "" {
		tok = r.URL.Query().Get("t")
	}
	if tok != a.token {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	if path == "events" {
		a.events(w, r)
		return
	}
	var body json.RawMessage
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&body); err != nil {
			w.WriteHeader(400)
			writeJSON(w, apiError{T("неверный запрос: ", "bad request: ") + err.Error()})
			return
		}
	}
	res, err := a.handle(path, body)
	if err != nil {
		w.WriteHeader(400)
		writeJSON(w, apiError{err.Error()})
		return
	}
	writeJSON(w, res)
	a.notify()
}

func (a *API) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no stream", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	c := make(chan struct{}, 1)
	a.subMu.Lock()
	a.subs[c] = true
	a.subMu.Unlock()
	defer func() {
		a.subMu.Lock()
		delete(a.subs, c)
		a.subMu.Unlock()
	}()
	send := func() bool {
		b, _ := json.Marshal(a.stateView())
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	if !send() {
		return
	}
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-c:
			time.Sleep(50 * time.Millisecond) // coalesce bursts
			if !send() {
				return
			}
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

type pollOption struct {
	Label string `json:"label"`
	Value int    `json:"value"`
}

// pollOptions mirrors the web driver's list for the reported max rate.
func pollOptions(maxRate int) []pollOption {
	b := []pollOption{{"125 Hz", 8}, {"250 Hz", 4}, {"500 Hz", 2}, {"1000 Hz", 1}}
	switch maxRate {
	case 8:
		return append(b, pollOption{"2000 Hz", 17}, pollOption{"4000 Hz", 18}, pollOption{"8000 Hz", 20})
	case 4:
		return append(b, pollOption{"2000 Hz", 17}, pollOption{"4000 Hz", 18})
	case 2:
		return append(b, pollOption{"2000 Hz", 17})
	case 49:
		return []pollOption{{"125 Hz", 8}, {"250 Hz", 4}}
	}
	return b
}

type stateView struct {
	EngineSnapshot
	PollOptions []pollOption `json:"pollOptions"`
	Pending     []string     `json:"pendingChanges"` // differences of the current profile vs mouse
	Current     string       `json:"current"`        // profile that is (or should be) on the mouse
}

func (a *API) stateView() stateView {
	s := a.eng.Snapshot()
	maxRate := 1
	if s.Primary != nil {
		maxRate = s.Primary.MaxRate
	}
	v := stateView{EngineSnapshot: s, PollOptions: pollOptions(maxRate)}
	cur := s.Base
	if n := len(s.Stack); n > 0 {
		cur = s.Stack[n-1].ProfileID
	}
	v.Current = cur
	a.store.mu.Lock()
	p := a.store.profile(cur)
	var pc Profile
	if p != nil {
		pc = *p
	}
	a.store.mu.Unlock()
	if p != nil {
		v.Pending = a.eng.diff(&pc)
	}
	return v
}

type allView struct {
	Data      *Data     `json:"data"`
	State     stateView `json:"state"`
	Lighting  []string  `json:"lighting"`
	Autostart bool      `json:"autostart"`
	Version   string    `json:"version"`
}

func (a *API) handle(path string, body json.RawMessage) (any, error) {
	st := a.store
	dec := func(v any) error {
		if len(body) == 0 {
			return errors.New(T("пустой запрос", "empty request"))
		}
		return json.Unmarshal(body, v)
	}
	switch path {
	case "all":
		st.mu.Lock()
		b, _ := json.Marshal(st.data) // deep copy
		st.mu.Unlock()
		var d Data
		_ = json.Unmarshal(b, &d)
		return allView{Data: &d, State: a.stateView(), Lighting: lightingModes, Autostart: autostartEnabled(), Version: appVersion}, nil

	case "state":
		return a.stateView(), nil

	case "profile/save":
		var p Profile
		if err := dec(&p); err != nil {
			return nil, err
		}
		if err := validateProfile(&p, a.eng.sensorType()); err != nil {
			return nil, err
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		for _, b := range p.Buttons {
			if b.Kind == "macro" && st.macro(b.MacroID) == nil {
				return nil, errors.New(T("назначен несуществующий макрос", "a deleted macro is assigned"))
			}
		}
		if cur := st.profile(p.ID); cur != nil {
			*cur = p
		} else {
			if p.ID == "" {
				p.ID = newID("p")
			}
			st.data.Profiles = append(st.data.Profiles, &p)
		}
		return map[string]string{"id": p.ID}, st.saveData()

	case "profile/create":
		var req struct {
			Name     string `json:"name"`
			CopyFrom string `json:"copyFrom"`
		}
		if err := dec(&req); err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.Name) == "" {
			return nil, errors.New(T("введите имя профиля", "enter a profile name"))
		}
		sensor := a.eng.sensorType()
		st.mu.Lock()
		defer st.mu.Unlock()
		var p *Profile
		if src := st.profile(req.CopyFrom); src != nil {
			c := *src
			p = &c
			p.ID = newID("p")
			p.Name = req.Name
		} else {
			p = newProfile(req.Name, sensor)
		}
		st.data.Profiles = append(st.data.Profiles, p)
		return map[string]string{"id": p.ID}, st.saveData()

	case "profile/delete":
		var req struct{ ID string }
		if err := dec(&req); err != nil {
			return nil, err
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		if req.ID == st.data.Settings.DefaultProfile {
			return nil, errors.New(T("основной профиль удалить нельзя — сначала назначьте основным другой", "the main profile cannot be deleted — make another profile the main one first"))
		}
		var keep []*Profile
		for _, p := range st.data.Profiles {
			if p.ID != req.ID {
				keep = append(keep, p)
			}
		}
		st.data.Profiles = keep
		var rules []Rule
		for _, r := range st.data.Rules {
			if r.ProfileID != req.ID {
				rules = append(rules, r)
			}
		}
		st.data.Rules = rules
		if st.data.Settings.BaseProfile == req.ID {
			st.data.Settings.BaseProfile = st.data.Settings.DefaultProfile
		}
		return map[string]bool{"ok": true}, st.saveData()

	case "profile/default":
		var req struct{ ID string }
		if err := dec(&req); err != nil {
			return nil, err
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.profile(req.ID) == nil {
			return nil, errors.New(T("профиль не найден", "profile not found"))
		}
		st.data.Settings.DefaultProfile = req.ID
		return map[string]bool{"ok": true}, st.saveData()

	case "profile/apply":
		var req struct {
			ID       string `json:"id"`
			Force    bool   `json:"force"`
			MakeBase bool   `json:"makeBase"`
		}
		if err := dec(&req); err != nil {
			return nil, err
		}
		st.mu.Lock()
		if st.profile(req.ID) == nil {
			st.mu.Unlock()
			return nil, errors.New(T("профиль не найден", "profile not found"))
		}
		if req.MakeBase {
			st.data.Settings.BaseProfile = req.ID
			_ = st.saveData()
		}
		st.mu.Unlock()
		go func() { _ = a.eng.Apply(req.ID, req.Force, "вручную", false, true); a.notify() }()
		return map[string]bool{"queued": true}, nil

	case "macro/save":
		var m Macro
		if err := dec(&m); err != nil {
			return nil, err
		}
		if strings.TrimSpace(m.Name) == "" {
			return nil, errors.New(T("введите имя макроса", "enter a macro name"))
		}
		for i := range m.Events {
			m.Events[i].Index = i
		}
		if err := ValidateMacro(m); err != nil {
			return nil, err
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		if cur := st.macro(m.ID); cur != nil {
			*cur = m
		} else {
			if m.ID == "" {
				m.ID = newID("m")
			}
			st.data.Macros = append(st.data.Macros, &m)
		}
		return map[string]string{"id": m.ID}, st.saveData()

	case "macro/delete":
		var req struct{ ID string }
		if err := dec(&req); err != nil {
			return nil, err
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		for _, p := range st.data.Profiles {
			for i, b := range p.Buttons {
				if b.Kind == "macro" && b.MacroID == req.ID {
					return nil, fmt.Errorf(T("макрос назначен на кнопку %d в профиле «%s»", "the macro is assigned to button %d in profile “%s”"), i+1, p.Name)
				}
			}
		}
		var keep []*Macro
		for _, m := range st.data.Macros {
			if m.ID != req.ID {
				keep = append(keep, m)
			}
		}
		st.data.Macros = keep
		return map[string]bool{"ok": true}, st.saveData()

	case "rules":
		var rules []Rule
		if err := dec(&rules); err != nil {
			return nil, err
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		seen := map[string]bool{}
		var clean []Rule
		for _, r := range rules {
			r.Exe = lower(strings.TrimSpace(r.Exe))
			if r.Exe == "" {
				continue
			}
			if !strings.HasSuffix(r.Exe, ".exe") {
				r.Exe += ".exe"
			}
			if seen[r.Exe] {
				return nil, fmt.Errorf(T("%s указан дважды", "%s is listed twice"), r.Exe)
			}
			if st.profile(r.ProfileID) == nil {
				return nil, fmt.Errorf(T("%s: профиль не выбран", "%s: no profile selected"), r.Exe)
			}
			seen[r.Exe] = true
			clean = append(clean, r)
		}
		st.data.Rules = clean
		return map[string]bool{"ok": true}, st.saveData()

	case "settings":
		var req struct {
			Settings  Settings `json:"settings"`
			Autostart *bool    `json:"autostart"`
		}
		if err := dec(&req); err != nil {
			return nil, err
		}
		st.mu.Lock()
		s := req.Settings
		old := st.data.Settings
		s.DefaultProfile, s.BaseProfile, s.BTPaths = old.DefaultProfile, old.BaseProfile, old.BTPaths
		if s.Language != "ru" && s.Language != "en" {
			s.Language = old.Language
		}
		setLang(s.Language)
		if s.ScanSeconds < 1 {
			s.ScanSeconds = 1
		}
		st.data.Settings = s
		logEnabled = s.Debug
		err := st.saveData()
		st.mu.Unlock()
		if req.Autostart != nil {
			setAutostart(*req.Autostart)
		}
		return map[string]bool{"ok": true}, err

	case "processes":
		m := runningProcesses()
		var names []string
		for n := range m {
			if strings.HasSuffix(n, ".exe") && !systemProcess[n] {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		return names, nil

	case "osd/test":
		var req struct {
			Kind  string     `json:"kind"`
			Style PopupStyle `json:"style"`
		}
		if err := dec(&req); err != nil {
			return nil, err
		}
		snap := a.eng.Snapshot()
		st.mu.Lock()
		p := st.profile(snap.Base)
		var c *osdContent
		if p != nil && req.Kind == "dpi" {
			lvl := p.DPIStart
			if snap.Primary != nil && snap.Primary.Link {
				lvl = snap.Primary.Level
			}
			c = a.eng.dpiOSD(p, lvl, req.Style)
		} else if p != nil {
			c = &osdContent{Caption: T("Профиль", "Profile"), Value: p.Name, Accent: parseHex(req.Style.AccentColor, 0x4CAF50), Style: req.Style}
		}
		st.mu.Unlock()
		if c != nil {
			cc := *c
			a.uiCall(func() { osdShow(cc) })
		}
		return map[string]bool{"ok": true}, nil

	case "factory-reset":
		a.eng.FactoryReset()
		return map[string]bool{"queued": true}, nil

	case "battery":
		if a.eng.bat == nil {
			return []BatSample{}, nil
		}
		out := a.eng.bat.since(time.Now().Add(-batKeep).Unix())
		if out == nil {
			out = []BatSample{}
		}
		return out, nil

	case "export":
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.data, nil

	case "mouse/read":
		mem, err := readMouseMemory()
		if err != nil {
			return nil, err
		}
		p, macros, err := decodeMouse(mem, a.eng.sensorType())
		if err != nil {
			return nil, err
		}
		res := &mouseReadResult{Profile: p, Macros: macros, Debounce: mem.Debounce, Differences: []string{}}
		mo := func(id string) *Macro {
			for _, m := range macros {
				if m.ID == id {
					return m
				}
			}
			return nil
		}
		sensor := a.eng.sensorType()
		mouseSig := map[string]string{}
		for _, c := range buildCommands(p, mo, sensor) {
			mouseSig[c.key] = signature(c)
		}
		cur := a.stateView().Current
		st.mu.Lock()
		if cp := st.profile(cur); cp != nil {
			res.Compared = cp.Name
			for _, c := range buildCommands(cp, st.macro, sensor) {
				if mouseSig[c.key] != signature(c) {
					res.Differences = append(res.Differences, c.key)
				}
			}
		}
		// the mouse is now known exactly: "what was sent" = what is there
		st.state.Sent = mouseSig
		st.state.Synced = true
		st.state.Seeded = true
		_ = st.saveState()
		st.mu.Unlock()
		a.readMu.Lock()
		a.lastRead = res
		a.readMu.Unlock()
		a.eng.changed()
		return res, nil

	case "mouse/import":
		var req struct {
			Name string `json:"name"`
		}
		if err := dec(&req); err != nil {
			return nil, err
		}
		a.readMu.Lock()
		r := a.lastRead
		a.readMu.Unlock()
		if r == nil {
			return nil, errors.New(T("сначала прочитайте настройки из мыши", "read the settings from the mouse first"))
		}
		b, _ := json.Marshal(r)
		var cp mouseReadResult
		_ = json.Unmarshal(b, &cp)
		p := cp.Profile
		p.ID = newID("p")
		if strings.TrimSpace(req.Name) != "" {
			p.Name = strings.TrimSpace(req.Name)
		}
		st.mu.Lock()
		st.data.Macros = append(st.data.Macros, cp.Macros...)
		st.data.Profiles = append(st.data.Profiles, p)
		st.data.Settings.BaseProfile = p.ID // it is what the mouse holds now
		err := st.saveData()
		st.mu.Unlock()
		a.eng.mu.Lock()
		if len(a.eng.stack) == 0 {
			a.eng.active = p.ID
		}
		a.eng.mu.Unlock()
		a.eng.changed()
		return map[string]string{"id": p.ID}, err

	case "flash-dump":
		dir := filepath.Join(st.dir, "flashdump-"+time.Now().Format("20060102-150405"))
		out, err := dumpFlash(dir)
		if err != nil {
			return nil, err
		}
		a.uiCall(func() { shellOpen(out) })
		return map[string]string{"dir": out}, nil

	case "backup":
		return a.backup(), nil

	case "restore":
		var b fullBackup
		if err := dec(&b); err != nil {
			return nil, err
		}
		return a.restore(&b)

	case "import":
		var d Data
		if err := dec(&d); err != nil {
			return nil, err
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		idMap := map[string]string{}
		for _, m := range d.Macros {
			n := *m
			idMap[m.ID] = newID("m")
			n.ID = idMap[m.ID]
			st.data.Macros = append(st.data.Macros, &n)
		}
		for _, p := range d.Profiles {
			n := *p
			n.ID = newID("p")
			n.Name = p.Name + T(" (импорт)", " (imported)")
			for i := range n.Buttons {
				if n.Buttons[i].Kind == "macro" {
					n.Buttons[i].MacroID = idMap[n.Buttons[i].MacroID]
				}
			}
			st.data.Profiles = append(st.data.Profiles, &n)
		}
		return map[string]int{"profiles": len(d.Profiles), "macros": len(d.Macros)}, st.saveData()
	}
	return nil, fmt.Errorf(T("неизвестный запрос %s", "unknown request %s"), path)
}

var systemProcess = map[string]bool{
	"svchost.exe": true, "csrss.exe": true, "wininit.exe": true, "winlogon.exe": true, "services.exe": true,
	"lsass.exe": true, "smss.exe": true, "dwm.exe": true, "conhost.exe": true, "fontdrvhost.exe": true,
	"runtimebroker.exe": true, "sihost.exe": true, "taskhostw.exe": true, "ctfmon.exe": true,
	"searchindexer.exe": true, "spoolsv.exe": true, "dllhost.exe": true, "wmiprvse.exe": true,
	"audiodg.exe": true, "msedgewebview2.exe": true, "system": true, "registry": true,
	"memory compression": true, "secure system": true, "sm809control.exe": true,
}

// ---------------------------------------------------------------------------
// Full backup: everything needed to move the app to another PC.

const backupFormat = "sm809control-backup"

type fullBackup struct {
	Format  string       `json:"format"`
	Version int          `json:"version"`
	App     string       `json:"app"`
	Created time.Time    `json:"created"`
	Data    *Data        `json:"data"`
	Device  *DeviceState `json:"device"`
	Battery []BatSample  `json:"battery"`
}

func (a *API) backup() fullBackup {
	st := a.store
	st.mu.Lock()
	b, _ := json.Marshal(struct {
		D *Data
		S *DeviceState
	}{st.data, st.state})
	st.mu.Unlock()
	var cp struct {
		D *Data
		S *DeviceState
	}
	_ = json.Unmarshal(b, &cp)
	fb := fullBackup{Format: backupFormat, Version: 1, App: appVersion, Created: time.Now(), Data: cp.D, Device: cp.S, Battery: []BatSample{}}
	if a.eng.bat != nil {
		if s := a.eng.bat.since(0); s != nil {
			fb.Battery = s
		}
	}
	return fb
}

func (a *API) restore(b *fullBackup) (any, error) {
	if b.Format != backupFormat || b.Data == nil || len(b.Data.Profiles) == 0 {
		return nil, errors.New(T("это не резервная копия SM809 Control", "this is not an SM809 Control backup"))
	}
	st := a.store
	// keep a copy of what is being replaced
	cur := a.backup()
	_ = writeJSONAtomic(filepath.Join(st.dir, "before-restore-"+time.Now().Format("20060102-150405")+".json"), cur)

	st.mu.Lock()
	old := st.data.Settings
	st.data = b.Data
	// PC-specific: Bluetooth HID paths and the UI language of this PC
	st.data.Settings.BTPaths = old.BTPaths
	if l := st.data.Settings.Language; l != "ru" && l != "en" {
		st.data.Settings.Language = old.Language
	}
	if b.Device != nil {
		seq := st.state.Seq // the sequence counter continues from this PC
		st.state = b.Device
		if st.state.Sent == nil {
			st.state.Sent = map[string]string{}
		}
		st.state.Seq = seq
	}
	st.fixup()
	setLang(st.data.Settings.Language)
	logEnabled = st.data.Settings.Debug
	err1 := st.saveData()
	err2 := st.saveState()
	st.mu.Unlock()
	if a.eng.bat != nil && b.Battery != nil {
		a.eng.bat.replace(b.Battery)
	}
	a.eng.mu.Lock()
	a.eng.stack = nil
	a.eng.active = ""
	a.eng.mu.Unlock()
	a.eng.changed()
	if err1 != nil {
		return nil, err1
	}
	return map[string]int{"profiles": len(b.Data.Profiles), "macros": len(b.Data.Macros), "battery": len(b.Battery)}, err2
}
