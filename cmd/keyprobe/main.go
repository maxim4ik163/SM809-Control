//go:build windows

// keyprobe: checks the assumptions of SM809 Control's software bindings.
//
// Give one or more SM809 buttons the keys F13..F24 (SM809 Control → Buttons →
// Key) or the media keys used for buttons 14..16 (browser Search, Favorites,
// Stop), then run this program. It logs every such key seen by
//   - a low-level keyboard hook (what SM809 Control will use), and
//   - Raw Input (which device sent it),
//
// with the virtual-key code, the scan code and how auto-repeat behaves while a
// button is held. Run it over the cable, the 2.4G dongle and Bluetooth.
//
// Phase 1 only watches. Phase 2 blocks F13..F24 in the hook for 20 seconds
// and checks that Raw Input (and so every program) no longer sees them.
// Nothing is written to the mouse.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unsafe"
)

func init() { runtime.LockOSThread() } // hook, window and loop on one thread

const (
	WM_INPUT = 0x00FF
	WM_TIMER = 0x0113

	WH_KEYBOARD_LL = 13

	LLKHF_EXTENDED = 0x01
	LLKHF_INJECTED = 0x10
	LLKHF_UP       = 0x80

	RIDEV_REMOVE    = 0x00000001
	RIDEV_INPUTSINK = 0x00000100
	RID_INPUT       = 0x10000003

	vkF13 = 0x7C
	vkF24 = 0x87
)

type event struct {
	t      time.Duration
	src    string // HOOK or RAW
	vk     uint32 // HOOK: virtual key; RAW: VK from the raw report
	scan   uint32
	ext    bool
	up     bool
	inj    bool
	dev    string // RAW only
	repeat bool   // HOOK: a key-down while the key was already down
}

var (
	out      io.Writer = os.Stdout
	start              = time.Now()
	events   []event
	hwnd     uintptr
	until    time.Time
	blocking bool
	down     = map[uint32]bool{} // hook: keys currently down
)

func logf(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) }

// carrier keys of SM809 Control: F13..F24 and three browser keys
var browserKeys = map[uint32]string{0xA9: "BrowserStop", 0xAA: "BrowserSearch", 0xAB: "BrowserFavorites"}

func isF(vk uint32) bool { return (vk >= vkF13 && vk <= vkF24) || browserKeys[vk] != "" }

func fname(vk uint32) string {
	if vk >= vkF13 && vk <= vkF24 {
		return fmt.Sprintf("F%d", 13+vk-vkF13)
	}
	if n := browserKeys[vk]; n != "" {
		return n
	}
	return fmt.Sprintf("VK %02X", vk)
}

func (e event) String() string {
	s := fmt.Sprintf("%9.1f ms  %-4s %-6s scan %02X", ms(e.t), e.src, fname(e.vk), e.scan)
	if e.ext {
		s += " E0"
	}
	if e.up {
		s += "  up  "
	} else {
		s += "  down"
	}
	if e.repeat {
		s += " (repeat)"
	}
	if e.inj {
		s += " injected"
	}
	if e.dev != "" {
		s += "  " + shortDev(e.dev)
		if isSM809(e.dev) {
			s += " [SM809]"
		}
	}
	return s
}

func record(e event) {
	events = append(events, e)
	logf("%s", e)
}

func kbdHook(code int, wp, lp uintptr) uintptr {
	if code == 0 {
		k := *(**kbdLL)(unsafe.Pointer(&lp))
		if isF(k.VkCode) {
			up := k.Flags&LLKHF_UP != 0
			e := event{t: time.Since(start), src: "HOOK", vk: k.VkCode, scan: k.ScanCode,
				ext: k.Flags&LLKHF_EXTENDED != 0, up: up, inj: k.Flags&LLKHF_INJECTED != 0}
			if !up {
				e.repeat = down[k.VkCode]
				down[k.VkCode] = true
			} else {
				delete(down, k.VkCode)
			}
			record(e)
			if blocking && !e.inj {
				return 1
			}
		}
	}
	r, _, _ := pCallNextHookEx.Call(0, uintptr(code), wp, lp)
	return r
}

// decodeRaw reads one WM_INPUT keyboard report.
func decodeRaw(lp uintptr) (event, bool) {
	var size uint32
	pGetRawInputData.Call(lp, RID_INPUT, 0, ptr(&size), 24)
	if size == 0 || size > 1024 {
		return event{}, false
	}
	buf := make([]byte, size)
	if r, _, _ := pGetRawInputData.Call(lp, RID_INPUT, uintptr(unsafe.Pointer(&buf[0])), ptr(&size), 24); int32(r) <= 0 {
		return event{}, false
	}
	if *(*uint32)(unsafe.Pointer(&buf[0])) != 1 { // keyboard only
		return event{}, false
	}
	// RAWINPUTHEADER (24 bytes on x64), then RAWKEYBOARD:
	// MakeCode u16, Flags u16, Reserved u16, VKey u16, Message u32, ExtraInformation u32
	mk := *(*uint16)(unsafe.Pointer(&buf[24]))
	fl := *(*uint16)(unsafe.Pointer(&buf[26]))
	vk := *(*uint16)(unsafe.Pointer(&buf[30]))
	if !isF(uint32(vk)) {
		return event{}, false
	}
	return event{t: time.Since(start), src: "RAW", vk: uint32(vk), scan: uint32(mk), ext: fl&2 != 0, up: fl&1 != 0,
		dev: rawDeviceName(*(*uintptr)(unsafe.Pointer(&buf[8])))}, true
}

func wndProc(h, m, wp, lp uintptr) uintptr {
	switch m {
	case WM_INPUT:
		if e, ok := decodeRaw(lp); ok {
			record(e)
		}
	case WM_TIMER:
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(h, m, wp, lp)
	return r
}

var kbdCB = syscallCallback(kbdHook)

func setupWindow() error {
	hinst, _, _ := pGetModuleHandleW.Call(0)
	cls := u16("SM809KeyProbe")
	wc := wndClassEx{LpfnWndProc: syscallCallback(wndProc), HInstance: hinst, LpszClassName: cls}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassExW.Call(ptr(&wc)); r == 0 {
		return err
	}
	h, _, err := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), 0, 0, 0, 0, 0, 0, ^uintptr(2), 0, hinst, 0) // HWND_MESSAGE
	if h == 0 {
		return err
	}
	hwnd = h
	return nil
}

func registerRaw(on bool) {
	flags, target := uint32(RIDEV_INPUTSINK), hwnd
	if !on {
		flags, target = RIDEV_REMOVE, 0
	}
	rid := [1]rawInputDevice{{0x01, 0x06, flags, target}}
	pRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&rid[0])), 1, unsafe.Sizeof(rid[0]))
}

// phase installs the hook, runs the message loop for secs seconds and
// removes the hook again.
func phase(secs int, block bool) []event {
	events, blocking = nil, block
	down = map[uint32]bool{}
	hinst, _, _ := pGetModuleHandleW.Call(0)
	hk, _, err := pSetWindowsHookExW.Call(WH_KEYBOARD_LL, kbdCB, hinst, 0)
	if hk == 0 {
		logf("SetWindowsHookEx: %v", err)
		return nil
	}
	registerRaw(true)
	pSetTimer.Call(hwnd, 1, 100, 0)
	until = time.Now().Add(time.Duration(secs) * time.Second)
	var m msg
	for time.Now().Before(until) {
		if r, _, _ := pGetMessageW.Call(ptr(&m), 0, 0, 0); int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(ptr(&m))
		pDispatchMessageW.Call(ptr(&m))
	}
	registerRaw(false)
	pUnhookWindowsHookEx.Call(hk)
	blocking = false
	return events
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

type keyStat struct {
	hookDown, hookUp, repeats, rawDown, rawUp, injected int
	scans, devs                                         map[string]bool
	firstRepeat                                         []time.Duration // delay of the first repeat after the press
	repeatGap                                           []time.Duration // gaps between repeats
	holds                                               []time.Duration
}

func summary(ev []event, title string) {
	logf("\n--- %s ---", title)
	st := map[uint32]*keyStat{}
	pressT := map[uint32]time.Duration{}
	lastDown := map[uint32]time.Duration{}
	for _, e := range ev {
		s := st[e.vk]
		if s == nil {
			s = &keyStat{scans: map[string]bool{}, devs: map[string]bool{}}
			st[e.vk] = s
		}
		sc := fmt.Sprintf("%02X", e.scan)
		if e.ext {
			sc += " E0"
		}
		s.scans[sc] = true
		if e.src == "RAW" {
			s.devs[shortDev(e.dev)] = true
			if e.up {
				s.rawUp++
			} else {
				s.rawDown++
			}
			continue
		}
		if e.inj {
			s.injected++
			continue
		}
		switch {
		case e.up:
			s.hookUp++
			if t, ok := pressT[e.vk]; ok {
				s.holds = append(s.holds, e.t-t)
				delete(pressT, e.vk)
			}
		case e.repeat:
			s.repeats++
			if lastDown[e.vk] == pressT[e.vk] {
				s.firstRepeat = append(s.firstRepeat, e.t-pressT[e.vk])
			} else {
				s.repeatGap = append(s.repeatGap, e.t-lastDown[e.vk])
			}
			lastDown[e.vk] = e.t
		default:
			s.hookDown++
			pressT[e.vk], lastDown[e.vk] = e.t, e.t
		}
	}
	if len(st) == 0 {
		logf("no F13..F24 keys seen")
		return
	}
	var vks []int
	for vk := range st {
		vks = append(vks, int(vk))
	}
	sort.Ints(vks)
	for _, v := range vks {
		s := st[uint32(v)]
		logf("%s (VK %02X): hook %d down / %d up, %d repeats; raw %d down / %d up; injected %d",
			fname(uint32(v)), v, s.hookDown, s.hookUp, s.repeats, s.rawDown, s.rawUp, s.injected)
		logf("    scan codes: %s; devices: %s", keys(s.scans), keys(s.devs))
		if len(s.holds) > 0 {
			logf("    holds: %s", durs(s.holds))
		}
		if len(s.firstRepeat) > 0 {
			logf("    first repeat after: %s; repeat gaps: %s", durs(s.firstRepeat), durRange(s.repeatGap))
		}
	}
}

func keys(m map[string]bool) string {
	var s []string
	for k := range m {
		s = append(s, k)
	}
	sort.Strings(s)
	if len(s) == 0 {
		return "-"
	}
	return strings.Join(s, ", ")
}

func durs(d []time.Duration) string {
	var s []string
	for i, v := range d {
		if i == 12 {
			s = append(s, "…")
			break
		}
		s = append(s, fmt.Sprintf("%.0f", ms(v)))
	}
	return strings.Join(s, " ") + " ms"
}

func durRange(d []time.Duration) string {
	if len(d) == 0 {
		return "-"
	}
	lo, hi := d[0], d[0]
	for _, v := range d {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return fmt.Sprintf("%.0f..%.0f ms (%d)", ms(lo), ms(hi), len(d))
}

func waitEnter(prompt string) {
	fmt.Print(prompt)
	flushConsoleInput()
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func main() {
	disableQuickEdit()
	exe, _ := os.Executable()
	logPath := filepath.Join(filepath.Dir(exe), "keyprobe-"+time.Now().Format("2006-01-02-150405")+".log")
	if f, err := os.Create(logPath); err == nil {
		defer f.Close()
		out = io.MultiWriter(os.Stdout, f)
	}
	if err := setupWindow(); err != nil {
		logf("window: %v", err)
		return
	}
	logf("SM809 key probe. Log: %s", logPath)
	logf("Assign F13..F23 to some buttons in SM809 Control first (Buttons → Key).")
	logf("")
	logf("Phase 1 (45 s, watch only): press each F-key button a few times quickly,")
	logf("then HOLD one for about 3 seconds (auto-repeat).")
	waitEnter("Press Enter to start phase 1… ")
	ev := phase(45, false)
	summary(ev, "phase 1: watch")

	logf("\nPhase 2 (20 s): F13..F24 are BLOCKED in the hook. Press the same buttons again;")
	logf("RAW lines should NOT appear any more (nothing else sees the key).")
	waitEnter("Press Enter to start phase 2… ")
	ev = phase(20, true)
	summary(ev, "phase 2: blocked")
	raw := 0
	for _, e := range ev {
		if e.src == "RAW" {
			raw++
		}
	}
	if raw == 0 {
		logf("result: blocked keys did not reach Raw Input")
	} else {
		logf("result: %d raw events got through while blocked!", raw)
	}
	waitEnter("\nDone. Send the log file. Press Enter to exit… ")
}
