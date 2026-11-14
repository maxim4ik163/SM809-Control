package main

// Input debugging for test builds (debugInput = true): logs software-button
// presses, holds, the keyboard hook's view of modifier and carrier keys
// (real or injected, ours or not) and every change of the Windows state of
// the mouse buttons and modifiers. Never logs ordinary keys (no typed text).

import (
	"fmt"
	"strings"
	"time"
)

const debugInput = true

var dbgCh = make(chan string, 512)

// dbg queues a line for the log without blocking (safe in the hook callback).
func dbg(format string, a ...any) {
	if !debugInput {
		return
	}
	s := time.Now().Format("15:04:05.000") + " " + fmt.Sprintf(format, a...)
	select {
	case dbgCh <- s:
	default:
	}
}

var dbgVKName = map[uint32]string{
	0x10: "Shift", 0x11: "Ctrl", 0x12: "Alt", 0xA0: "LShift", 0xA1: "RShift", 0xA2: "LCtrl", 0xA3: "RCtrl",
	0xA4: "LAlt", 0xA5: "RAlt", 0x5B: "LWin", 0x5C: "RWin", 0x0D: "Enter", 0x1B: "Esc",
}

// dbgHookKey: one keyboard event as the hook sees it (hook thread).
func dbgHookKey(k *KBDLLHOOKSTRUCT, swallowed bool) {
	if !debugInput {
		return
	}
	name, ok := dbgVKName[k.VkCode]
	if !ok {
		if carrierSlotOf(k.VkCode, k.ScanCode, k.Flags&LLKHF_EXTENDED != 0) < 0 {
			return // ordinary key: never logged
		}
		name = "носитель"
	}
	dir := "down"
	if k.Flags&LLKHF_UP != 0 {
		dir = "up"
	}
	src := "реальная"
	if k.Flags&LLKHF_INJECTED != 0 {
		src = "имитация"
		if k.ExtraInfo == injectedMark {
			src = "имитация SM809"
		}
	}
	act := ""
	if swallowed {
		act = " — перехвачена"
	}
	dbg("хук: %s %s vk=%02X scan=%02X flags=%02X (%s)%s", name, dir, k.VkCode, k.ScanCode, k.Flags, src, act)
}

var dbgStateVKs = []struct {
	vk   uint16
	name string
}{
	{0x01, "ЛКМ"}, {0x02, "ПКМ"}, {0x04, "СКМ"}, {0xA0, "LShift"}, {0xA1, "RShift"}, {0xA2, "LCtrl"}, {0xA3, "RCtrl"},
	{0xA4, "LAlt"}, {0xA5, "RAlt"}, {0x5B, "LWin"}, {0x5C, "RWin"},
}

// dbgState: what Windows reports down now, e.g. "ЛКМ+LShift" or "—".
func dbgState() string {
	var d []string
	for _, x := range dbgStateVKs {
		if s, _, _ := pGetAsyncKeyState.Call(uintptr(x.vk)); s&0x8000 != 0 {
			d = append(d, x.name)
		}
	}
	if len(d) == 0 {
		return "—"
	}
	return strings.Join(d, "+")
}

// dbgRun writes queued lines and logs every change of the state (20 times a second).
func dbgRun() {
	if !debugInput {
		return
	}
	logf("отладка ввода включена (тестовая сборка)")
	t := time.NewTicker(50 * time.Millisecond)
	last := ""
	for {
		select {
		case s := <-dbgCh:
			logf("[ввод] %s", s)
		case <-t.C:
			if st := dbgState(); st != last {
				logf("[ввод] нажато в Windows: %s", st)
				last = st
			}
		}
	}
}
