//go:build windows

package main

// Software bindings runtime: a low-level keyboard hook catches the carrier
// keys F13..F23 sent by the mouse, swallows them and performs the bound
// action with SendInput. The hook runs on its own locked OS thread with its
// own message loop; its callback only decides and queues, the work happens
// in a worker goroutine. The same thread also watches the foreground window.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	pSetWindowsHookExW         = user32.NewProc("SetWindowsHookExW")
	pCallNextHookEx            = user32.NewProc("CallNextHookEx")
	pUnhookWindowsHookEx       = user32.NewProc("UnhookWindowsHookEx")
	pSendInput                 = user32.NewProc("SendInput")
	pMapVirtualKeyW            = user32.NewProc("MapVirtualKeyW")
	pSetWinEventHook           = user32.NewProc("SetWinEventHook")
	pGetForegroundWindow       = user32.NewProc("GetForegroundWindow")
	pGetWindowThreadProcessId  = user32.NewProc("GetWindowThreadProcessId")
	pPostThreadMessageW        = user32.NewProc("PostThreadMessageW")
	pOpenProcess               = kernel32.NewProc("OpenProcess")
	pQueryFullProcessImageName = kernel32.NewProc("QueryFullProcessImageNameW")
	pGetCurrentThreadId        = kernel32.NewProc("GetCurrentThreadId")
)

const (
	WH_KEYBOARD_LL = 13
	LLKHF_EXTENDED = 0x01
	LLKHF_INJECTED = 0x10
	LLKHF_UP       = 0x80

	INPUT_MOUSE           = 0
	INPUT_KEYBOARD        = 1
	KEYEVENTF_EXTENDEDKEY = 0x0001
	KEYEVENTF_KEYUP       = 0x0002

	EVENT_SYSTEM_FOREGROUND = 0x0003
	WINEVENT_OUTOFCONTEXT   = 0x0000

	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000

	msgSoftUpdate = WM_APP + 20 // (re)install or remove the hook
	msgCapCancel  = WM_APP + 21
)

// injectedMark goes into dwExtraInfo of every event we inject.
const injectedMark = 0x534D3839 // "SM89"

type KBDLLHOOKSTRUCT struct {
	VkCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// INPUT for SendInput (x64: 4-byte type, 4 padding, 32-byte union).
type INPUT struct {
	Type  uint32
	_     uint32
	Union [32]byte
}

type KEYBDINPUT struct {
	Vk        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type MOUSEINPUT struct {
	Dx, Dy    int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

func keyInput(k KEYBDINPUT) INPUT {
	in := INPUT{Type: INPUT_KEYBOARD}
	*(*KEYBDINPUT)(unsafe.Pointer(&in.Union[0])) = k
	return in
}

func mouseInput(m MOUSEINPUT) INPUT {
	in := INPUT{Type: INPUT_MOUSE}
	*(*MOUSEINPUT)(unsafe.Pointer(&in.Union[0])) = m
	return in
}

func sendInputs(in []INPUT) {
	if len(in) == 0 {
		return
	}
	pSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
}

// ---------------------------------------------------------------------------
// Output

func comboInputs(c Combo, up bool) []INPUT {
	var in []INPUT
	n := len(c.Keys)
	for i := 0; i < n; i++ {
		k := c.Keys[i]
		if up {
			k = c.Keys[n-1-i] // release in reverse order
		}
		scan, _, _ := pMapVirtualKeyW.Call(uintptr(k.VK), 0) // MAPVK_VK_TO_VSC
		var fl uint32
		if k.Ext {
			fl |= KEYEVENTF_EXTENDEDKEY
		}
		if up {
			fl |= KEYEVENTF_KEYUP
		}
		in = append(in, keyInput(KEYBDINPUT{Vk: k.VK, Scan: uint16(scan), Flags: fl, ExtraInfo: injectedMark}))
	}
	return in
}

func pressCombo(c Combo)   { sendInputs(comboInputs(c, false)) }
func releaseCombo(c Combo) { sendInputs(comboInputs(c, true)) }

// mouse button → down flags, up flags, mouseData (XBUTTON1 = back, XBUTTON2 = forward)
var mouseOut = map[string][3]uint32{
	"left":    {0x0002, 0x0004, 0},
	"right":   {0x0008, 0x0010, 0},
	"middle":  {0x0020, 0x0040, 0},
	"back":    {0x0080, 0x0100, 1},
	"forward": {0x0080, 0x0100, 2},
}

func sendMouseButton(name string, up bool) {
	m, ok := mouseOut[name]
	if !ok {
		return
	}
	fl := m[0]
	if up {
		fl = m[1]
	}
	sendInputs([]INPUT{mouseInput(MOUSEINPUT{MouseData: m[2], Flags: fl, ExtraInfo: injectedMark})})
}

// repeater sends repeated key-down events for the last key of a combination
// while the button is held, like a keyboard's typematic repeat (Windows does
// not repeat injected keys, and the carrier key's own repeats are swallowed).
type repeater struct {
	mu   sync.Mutex
	stop chan struct{}
}

func (r *repeater) start(c Combo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stop != nil {
		close(r.stop)
	}
	stop := make(chan struct{})
	r.stop = stop
	last := Combo{Keys: c.Keys[len(c.Keys)-1:]}
	go func() {
		select {
		case <-stop:
			return
		case <-time.After(500 * time.Millisecond):
		}
		t := time.NewTicker(33 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				pressCombo(last)
			}
		}
	}()
}

func (r *repeater) halt() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stop != nil {
		close(r.stop)
		r.stop = nil
	}
}

// ---------------------------------------------------------------------------
// Runtime

type softEvent struct {
	slot int
	down bool
}

type softHeld struct {
	out SoftOut
	rep *repeater
	run *macroRun // a macro started by this press
}

type softRuntime struct {
	store  *Store
	eng    *Engine
	onOSD  func(osdContent)
	notify func() // wake the settings UI (live state); set after the API starts

	// hook thread
	threadID  uint32
	kbdHook   uintptr
	kcb       uintptr
	mask      atomic.Uint32 // carrier buttons (bit i = button i+1)
	swallowed [softSlots]bool
	alive     atomic.Bool
	events    chan softEvent

	// combination capture (state on the hook thread)
	capActive  atomic.Bool
	capResult  chan string
	capMods    [4]bool
	capKey     bool
	capBlocked map[uint32]bool

	mu      sync.Mutex
	ownExe  string
	fgExe   string
	profile string // active soft profile id
	held    map[int]*softHeld
	macros  map[int]*macroRun // macros started per button (still running or finished)
	last    string
	lastErr string
	errAt   time.Time

	untilAny atomic.Int32 // running "until any key" macros (read by the hook)
	ensured  bool         // carrier checked after the first link this session
}

func newSoftRuntime(st *Store, e *Engine, onOSD func(osdContent)) *softRuntime {
	exe, _ := os.Executable()
	r := &softRuntime{store: st, eng: e, onOSD: onOSD, events: make(chan softEvent, 64),
		capBlocked: map[uint32]bool{}, held: map[int]*softHeld{}, macros: map[int]*macroRun{}, ownExe: normalizeExe(filepath.Base(exe))}
	st.mu.Lock()
	r.mask.Store(maskBits(st.state.Carrier))
	st.mu.Unlock()
	onCarrierChange = func(m [softSlots]bool) {
		r.mask.Store(maskBits(m))
		r.post(msgSoftUpdate)
	}
	return r
}

func maskBits(m [softSlots]bool) uint32 {
	var b uint32
	for i, on := range m {
		if on {
			b |= 1 << i
		}
	}
	return b
}

func (r *softRuntime) post(msg uint32) {
	if r.threadID != 0 {
		pPostThreadMessageW.Call(uintptr(r.threadID), uintptr(msg), 0, 0)
	}
}

// run starts the hook thread, the worker and the foreground fallback timer.
func (r *softRuntime) run() {
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		tid, _, _ := pGetCurrentThreadId.Call()
		r.threadID = uint32(tid)
		r.kcb = syscall.NewCallback(func(code int, wp, lp uintptr) uintptr { return r.kbdProc(code, wp, lp) })
		watchForeground(r.setForeground)
		r.updateHook()
		close(ready)
		var m MSG
		for {
			res, _, _ := pGetMessageW.Call(ptr(&m), 0, 0, 0)
			if int32(res) <= 0 {
				break
			}
			if m.Hwnd == 0 {
				switch m.Message {
				case msgSoftUpdate:
					r.updateHook()
					continue
				case msgCapCancel:
					r.capFinish("")
					continue
				}
			}
			pTranslateMessage.Call(ptr(&m))
			pDispatchMessageW.Call(ptr(&m))
		}
		if r.kbdHook != 0 {
			pUnhookWindowsHookEx.Call(r.kbdHook)
			r.kbdHook = 0
		}
		r.alive.Store(false)
	}()
	<-ready
	go func() {
		for ev := range r.events {
			r.handle(ev)
		}
	}()
	go func() {
		for {
			time.Sleep(2 * time.Second)
			r.setForeground(foregroundExe()) // fallback for missed WinEvents
			r.ensureCarrier()
		}
	}()
}

// updateHook installs the keyboard hook while it has something to do
// (carrier keys on the mouse, or a combination capture) and removes it
// otherwise, so the system's keyboard input is not routed through us for
// nothing. Hook thread only.
func (r *softRuntime) updateHook() {
	want := r.mask.Load() != 0 || r.capActive.Load() || len(r.capBlocked) > 0 || r.anySwallowed()
	switch {
	case want && r.kbdHook == 0:
		h, _, err := pSetWindowsHookExW.Call(WH_KEYBOARD_LL, r.kcb, hinst, 0)
		if h == 0 {
			logf("keyboard hook: %v", err)
			return
		}
		r.kbdHook = h
		r.alive.Store(true)
		logf("хук клавиатуры установлен")
	case !want && r.kbdHook != 0:
		pUnhookWindowsHookEx.Call(r.kbdHook)
		r.kbdHook = 0
		r.alive.Store(false)
		logf("хук клавиатуры снят")
	}
}

func (r *softRuntime) anySwallowed() bool {
	for _, s := range r.swallowed {
		if s {
			return true
		}
	}
	return false
}

func (r *softRuntime) kbdProc(code int, wp, lp uintptr) uintptr {
	if code == 0 {
		k := *(**KBDLLHOOKSTRUCT)(unsafe.Pointer(&lp)) // lParam points to the hook struct
		if k.Flags&LLKHF_INJECTED == 0 && r.decide(k) {
			return 1
		}
	}
	res, _, _ := pCallNextHookEx.Call(0, uintptr(code), wp, lp)
	return res
}

// decide returns true to swallow the key. Hook thread only.
func (r *softRuntime) decide(k *KBDLLHOOKSTRUCT) bool {
	up := k.Flags&LLKHF_UP != 0
	if r.capActive.Load() || r.capBlocked[k.VkCode] {
		return r.capKeyEvent(k, up)
	}
	i := carrierSlotOfVK(k.VkCode)
	if i < 0 {
		if !up && r.untilAny.Load() > 0 {
			r.queue(softEvent{slot: -1, down: true}) // stops "until any key" macros
		}
		return false
	}
	if up {
		if !r.swallowed[i] {
			return false
		}
		r.swallowed[i] = false
		r.queue(softEvent{slot: i, down: false})
		if r.mask.Load() == 0 && !r.anySwallowed() {
			r.post(msgSoftUpdate) // the hook was kept only for this release
		}
		return true
	}
	if r.swallowed[i] {
		return true // auto-repeat of a held carrier key
	}
	if r.mask.Load()&(1<<i) == 0 {
		return false // not a carrier key now: let F13.. through
	}
	r.swallowed[i] = true
	r.queue(softEvent{slot: i, down: true})
	return true
}

func (r *softRuntime) queue(ev softEvent) {
	select {
	case r.events <- ev:
	default:
	}
}

// handle performs a button press or release (worker goroutine).
func (r *softRuntime) handle(ev softEvent) {
	defer r.wake() // after the unlock below (deferred calls run in reverse)
	r.mu.Lock()
	defer r.mu.Unlock()
	if ev.slot < 0 { // a keyboard key
		r.stopUntilAny(-1)
		return
	}
	if !ev.down {
		h := r.held[ev.slot]
		if h == nil {
			return
		}
		delete(r.held, ev.slot)
		if h.rep != nil {
			h.rep.halt()
		}
		if h.run != nil && h.run.mode == 1 {
			h.run.halt() // "while held"
		}
		switch h.out.Kind {
		case SoftKeys:
			releaseCombo(h.out.Combo)
		case SoftMouse:
			sendMouseButton(h.out.Button, true)
		}
		return
	}
	if r.held[ev.slot] != nil {
		return
	}
	// any button press stops "until any key" macros; pressing their own
	// button again only stops them (a toggle)
	if r.stopUntilAny(ev.slot) {
		r.held[ev.slot] = &softHeld{out: SoftOut{Kind: SoftDisable}}
		return
	}
	if run := r.macros[ev.slot]; run != nil && run.running() {
		r.held[ev.slot] = &softHeld{out: SoftOut{Kind: SoftDisable}} // "N times" still running
		return
	}
	onMouse := r.eng.OnMouse()
	st := r.store
	st.mu.Lock()
	sd := st.data.Soft
	sp := activeSoftProfile(sd, r.fgExe)
	out, ok := softOutput(sd, sp, st.profile(onMouse), ev.slot, st.macro)
	repeat := sd.KeyRepeat
	st.mu.Unlock()
	r.last = softSlotName(ev.slot)
	if !ok {
		logf("кнопка %d: действие только в мыши — нужна запись профиля", ev.slot+1)
		return
	}
	h := &softHeld{out: out}
	switch out.Kind {
	case SoftKeys:
		pressCombo(out.Combo)
		if repeat && out.Combo.repeatable() {
			h.rep = &repeater{}
			h.rep.start(out.Combo)
		}
	case SoftMouse:
		sendMouseButton(out.Button, false)
	case SoftMacro:
		run := &macroRun{slot: ev.slot, mode: out.Macro.ExecuteType, stop: make(chan struct{}), done: make(chan struct{})}
		if run.mode < 1 || run.mode > 3 {
			run.mode = 2
		}
		if run.mode == 3 {
			r.untilAny.Add(1)
			go func() { <-run.done; r.untilAny.Add(-1) }()
		}
		r.macros[ev.slot] = run
		h.run = run
		go playMacro(out.Macro, run)
	case SoftText:
		go r.typeText(out.B)
	case SoftRun:
		go r.runProgram(out.B)
	}
	r.held[ev.slot] = h
	if logEnabled {
		what := out.Kind
		switch out.Kind {
		case SoftKeys:
			what = out.Combo.String()
		case SoftMouse:
			what = "mouse " + out.Button
		case SoftMacro:
			what = "macro " + out.Macro.Name
		case SoftRun:
			what = "run " + out.B.Path
		}
		logf("кнопка %d → %s (%s)", ev.slot+1, what, r.fgExe)
	}
}

// stopUntilAny stops running "until any key" macros; true if one of them was
// started by slot. r.mu held.
func (r *softRuntime) stopUntilAny(slot int) bool {
	own := false
	for s, run := range r.macros {
		if run.mode == 3 && run.running() {
			run.halt()
			if s == slot {
				own = true
			}
		}
	}
	return own
}

func (r *softRuntime) setError(s string) {
	r.mu.Lock()
	r.lastErr, r.errAt = s, time.Now()
	r.mu.Unlock()
	r.wake()
}

// releaseAll lets go of everything (exit).
func (r *softRuntime) releaseAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, run := range r.macros {
		run.halt()
	}
	for slot, h := range r.held {
		if h.rep != nil {
			h.rep.halt()
		}
		switch h.out.Kind {
		case SoftKeys:
			releaseCombo(h.out.Combo)
		case SoftMouse:
			sendMouseButton(h.out.Button, true)
		}
		delete(r.held, slot)
	}
}

func (r *softRuntime) stop() {
	r.releaseAll()
	if r.threadID != 0 {
		pPostThreadMessageW.Call(uintptr(r.threadID), 0x0012, 0, 0) // WM_QUIT
	}
}

// ensureCarrier: once per session, after the mouse is first seen on the link,
// make sure the carrier layout is on it (e.g. after "restore on exit"). Only
// the key layout is written, and only if it differs.
func (r *softRuntime) ensureCarrier() {
	r.mu.Lock()
	done := r.ensured
	r.mu.Unlock()
	if done {
		return
	}
	snap := r.eng.Snapshot()
	if snap.Primary == nil || !snap.Primary.Link || snap.Busy {
		return
	}
	r.mu.Lock()
	r.ensured = true
	r.mu.Unlock()
	onMouse := r.eng.OnMouse()
	st := r.store
	st.mu.Lock()
	soft, synced, cur := st.data.Settings.SoftBindings, st.state.Synced, st.state.Carrier
	var want [softSlots]bool
	if p := st.profile(onMouse); p != nil && soft {
		want = carriedMask(p)
	}
	st.mu.Unlock()
	if synced && soft && cur != want {
		logf("программные кнопки: раскладка в мыши не совпадает — записываю клавиши")
		_ = r.eng.ApplyKeys(onMouse, "программные кнопки", false)
	}
}

// ---------------------------------------------------------------------------
// Foreground window → soft profile

func (r *softRuntime) setForeground(exe string) {
	if exe == "" || exe == r.ownExe || exe == "msedgewebview2.exe" {
		return // our own settings window keeps the current profile
	}
	r.mu.Lock()
	if exe == r.fgExe {
		r.mu.Unlock()
		return
	}
	r.fgExe = exe
	r.mu.Unlock()
	r.changed()
}

// changed recomputes the active soft profile (popup if it changed).
func (r *softRuntime) changed() {
	st := r.store
	r.mu.Lock()
	exe, prevID := r.fgExe, r.profile
	r.mu.Unlock()
	st.mu.Lock()
	sd := st.data.Soft
	var prev *SoftProfile
	for _, p := range sd.Profiles {
		if p.ID == prevID {
			prev = p
		}
	}
	p := activeSoftProfile(sd, exe)
	enabled, osdOn, style := st.data.Settings.SoftBindings, st.data.Settings.OSDProfile, st.data.Settings.Popup
	var id, name string
	show := false
	if p != nil {
		id, name, show = p.ID, p.Name, p.OSD
	}
	if prev != nil && prev.OSD {
		show = true
	}
	st.mu.Unlock()
	if id == prevID {
		return
	}
	r.mu.Lock()
	r.profile = id
	r.mu.Unlock()
	logf("активное окно %s → кнопки «%s»", exe, name)
	if enabled && osdOn && show && r.onOSD != nil {
		if name == "" {
			name = T("Общие", "Global")
		}
		r.onOSD(osdContent{Caption: T("Кнопки", "Buttons"), Value: name, Accent: parseHex(style.AccentColor, 0x4CAF50), Style: style})
	}
	r.wake()
}

func (r *softRuntime) wake() {
	if r.notify != nil {
		r.notify()
	}
}

func (r *softRuntime) status() SoftStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := SoftStatus{Hook: r.alive.Load(), Exe: r.fgExe, Profile: r.profile, Last: r.last, Held: []string{}}
	if r.lastErr != "" && time.Since(r.errAt) < time.Minute {
		s.Error = r.lastErr
	}
	for slot := range r.held {
		s.Held = append(s.Held, softSlotName(slot))
	}
	return s
}

var fgCallback uintptr

func exeOfWindow(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	var pid uint32
	pGetWindowThreadProcessId.Call(hwnd, ptr(&pid))
	if pid == 0 {
		return ""
	}
	h, _, _ := pOpenProcess.Call(PROCESS_QUERY_LIMITED_INFORMATION, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer pCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if res, _, _ := pQueryFullProcessImageName.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), ptr(&n)); res == 0 {
		return ""
	}
	return normalizeExe(syscall.UTF16ToString(buf[:n]))
}

func foregroundExe() string {
	w, _, _ := pGetForegroundWindow.Call()
	return exeOfWindow(w)
}

// watchForeground must be called on a thread with a message loop.
func watchForeground(on func(exe string)) {
	fgCallback = syscall.NewCallback(func(hook, event, hwnd, idObject, idChild, thread, t uintptr) uintptr {
		go on(exeOfWindow(hwnd)) // never block the hook thread
		return 0
	})
	pSetWinEventHook.Call(EVENT_SYSTEM_FOREGROUND, EVENT_SYSTEM_FOREGROUND, 0, fgCallback, 0, 0, WINEVENT_OUTOFCONTEXT)
}

// ---------------------------------------------------------------------------
// Key combination capture for the settings UI
//
// The settings window cannot see combinations such as Ctrl+Win+Right:
// Windows acts on them before any window does. While capturing, the hook
// takes every key press for itself, builds the combination and keeps blocking
// the pressed keys until they are released, so no key is left half-pressed.

func modIndex(vk uint32) int {
	switch vk {
	case 0x11, 0xA2, 0xA3:
		return 0
	case 0x10, 0xA0, 0xA1:
		return 1
	case 0x12, 0xA4, 0xA5:
		return 2
	case 0x5B, 0x5C:
		return 3
	}
	return -1
}

func (r *softRuntime) capCombo(key string) string {
	var parts []string
	for i, on := range r.capMods {
		if on {
			parts = append(parts, modOrder[i])
		}
	}
	if key != "" {
		parts = append(parts, key)
	}
	c, err := parseCombo(strings.Join(parts, "+"))
	if err != nil {
		return ""
	}
	return c.String()
}

func (r *softRuntime) capFinish(combo string) {
	if r.capActive.Swap(false) {
		select {
		case r.capResult <- combo:
		default:
		}
	}
	r.updateHook()
}

// capKeyEvent: hook thread, capture active or a blocked key being released.
func (r *softRuntime) capKeyEvent(k *KBDLLHOOKSTRUCT, up bool) bool {
	vk := k.VkCode
	if up {
		if !r.capBlocked[vk] {
			return false
		}
		delete(r.capBlocked, vk)
		// releasing a modifier with no other key: the combination is the modifiers
		if r.capActive.Load() && !r.capKey && modIndex(vk) >= 0 {
			r.capFinish(r.capCombo(""))
		} else if !r.capActive.Load() {
			r.updateHook()
		}
		return true
	}
	if !r.capActive.Load() {
		return true // still held from the capture
	}
	r.capBlocked[vk] = true
	if i := modIndex(vk); i >= 0 {
		r.capMods[i] = true
		return true
	}
	if vk == 0x1B && r.capMods == [4]bool{} { // Esc alone cancels
		r.capFinish("")
		return true
	}
	name := ""
	if vk == 0x0D && k.Flags&LLKHF_EXTENDED != 0 {
		name = "NumEnter"
	} else if kd, ok := keyByVK[uint16(vk)]; ok && !kd.Mod {
		name = kd.Name
	}
	if name != "" {
		r.capKey = true
		r.capFinish(r.capCombo(name))
	}
	return true
}

// capture waits for the user to press a combination (up to timeout).
// Returns "" if cancelled or timed out.
func (r *softRuntime) capture(timeout time.Duration) (string, error) {
	if r.threadID == 0 {
		return "", errors.New(T("хук клавиатуры не работает", "the keyboard hook is not running"))
	}
	if r.capActive.Load() {
		return "", errors.New(T("запись уже идёт", "already recording"))
	}
	// the hook thread touches these only while capActive is set
	ch := make(chan string, 1)
	r.capResult = ch
	r.capMods, r.capKey = [4]bool{}, false
	if !r.capActive.CompareAndSwap(false, true) {
		return "", errors.New(T("запись уже идёт", "already recording"))
	}
	r.post(msgSoftUpdate) // installs the hook if needed
	select {
	case s := <-ch:
		return s, nil
	case <-time.After(timeout):
		r.post(msgCapCancel)
		return "", nil
	}
}
