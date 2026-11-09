package main

// Command encoders for the SM809 (Elan VID 04F3 / Beken BK3633 firmware).
//
// This is a faithful port of the vendor's own web driver
// (driver.yuandaxin-tech.com/HYX/V1000_SM809_3311, files index-CQl4Vnyy.js and
// DeviceView-C4JAQhLH.js). Every packet produced here must be byte-for-byte
// identical to what that driver sends, see protocol_test.go.
//
// Transport: HID feature report 0x06. Payload (without the report ID) is
//   [cmd, seq, sub, checksum, data...]
// where checksum = (6 + sum(all other payload bytes)) & 0xFF, inserted at
// index 3 after padding. The firmware ignores a command whose seq equals the
// previous one, so retries resend the identical packet (safe), and every new
// command takes a new seq. Success is read back via GET_FEATURE 0x06: byte[1]==1.

import (
	"errors"
	"fmt"
	"sort"
)

const (
	cmdKeys       = 1
	cmdDPI        = 2
	cmdDPIColors  = 3
	cmdPollRate   = 4
	cmdLOD        = 5
	cmdLighting   = 6
	cmdPower      = 7
	cmdSensor     = 8
	cmdDebounce   = 9  // not used by the vendor tools
	cmdZones      = 10 // byte the stock firmware stores but never reads; used by the zones firmware patch
	cmdReset      = 15
	cmdMacroData  = 16
	cmdMacroErase = 17

	numButtons   = 16
	maxDPILevels = 6       // SM809 web config: dpiMaxLevels = 6 (firmware has room for 8)
	macroMaxData = 15 * 25 // 15 packets x 25 bytes, firmware buffer is 400 bytes
	macroMaxActs = macroMaxData / 3
	maxDelayMs   = 4095
)

// setCheckSum mirrors the web driver's x(): sum starts at 6 (report ID).
func setCheckSum(u []byte) []byte {
	s := 6
	for _, b := range u {
		s += int(b)
	}
	out := make([]byte, 0, len(u)+1)
	out = append(out, u[:3]...)
	out = append(out, byte(s&0xFF))
	out = append(out, u[3:]...)
	return out
}

func pad(u []byte, n int, v byte) []byte {
	for len(u) < n {
		u = append(u, v)
	}
	return u
}

// ---------- buttons ----------

// KeyCode is the 3-byte action assigned to a button.
type KeyCode [3]byte

var unassigned = KeyCode{0xFF, 0xFF, 0xFF}

// dualPacketOrder: button indexes (0-based) in the order the web driver sends
// them when the mouse has more than 9 buttons (fu()).
var dualPacketOrder = [16]int{11, 12, 13, 14, 15, 9, 10, 0, 1, 2, 3, 4, 5, 8, 6, 7}

// EncodeKeys builds the two key-assignment packets for 16 buttons.
// codes[i] is the action for button id i+1; zero value means "not configured".
func EncodeKeys(codes [numButtons]KeyCode, seq func() byte) [][]byte {
	get := func(i int) []byte {
		c := codes[i]
		if c == (KeyCode{}) {
			return unassigned[:]
		}
		return c[:]
	}
	h := []byte{cmdKeys, seq(), 2}
	for fe := 0; fe < 9; fe++ {
		h = append(h, get(dualPacketOrder[fe])...)
	}
	p1 := setCheckSum(pad(h, 30, 0xFF))
	y := []byte{cmdKeys, seq(), 18}
	for fe := 9; fe < 16; fe++ {
		y = append(y, get(dualPacketOrder[fe])...)
	}
	p2 := setCheckSum(pad(y, 30, 0xFF))
	return [][]byte{p1, p2}
}

// MacroButtonCode is the key code that binds a macro to a button.
// execType (web driver labels): 1 = run while the button is held,
// 2 = run N times, 3 = run until any key is pressed.
func MacroButtonCode(buttonID, execType, repeat int) KeyCode {
	h, n := 1, 0
	switch execType {
	case 2:
		h = 2
		n = repeat
		if n < 1 {
			n = 1
		}
	case 3:
		h = 4
	}
	slot := buttonID & 15
	if buttonID == 16 {
		slot = 12
	}
	return KeyCode{144, byte(h<<4 | slot), byte(n)}
}

// ---------- DPI ----------

type SensorInfo struct {
	Name    string
	Step    int // DPI per firmware unit (encoding divisor)
	MaxDPI  int
	Default []int
}

// Sensor table from the web driver, with MaxDPI additionally clamped to the
// size of the firmware's own lookup tables where we know them (the firmware
// indexes its table with the value we send, so going past it would read junk).
var sensors = map[int]SensorInfo{
	0x11: {"PAW3311", 100, 24000, []int{1200, 1600, 2400, 3200, 6400, 16000}}, // fw table: 240 entries
	0x95: {"PAW3395", 50, 26000, []int{400, 800, 1600, 3200, 6400, 26000}},
	0x50: {"PAW3950", 50, 30000, []int{400, 800, 1600, 3200, 6400, 30000}},
	0x20: {"PAW3220", 100, 8000, []int{400, 800, 1600, 3200, 6400, 8000}},
	0x12: {"PAW3212", 100, 4800, []int{400, 800, 1600, 2400, 3200, 4800}},
	0x09: {"PMW3809", 100, 6000, []int{400, 800, 1600, 2400, 3200, 6000}},
	0x06: {"S205", 100, 12800, []int{400, 800, 1600, 3200, 6400, 12800}},
	0x70: {"OM70", 100, 30000, []int{400, 800, 1600, 3200, 6400, 30000}},
	0x25: {"0x25", 100, 12800, []int{400, 800, 1600, 3200, 6400, 12800}}, // fw table: 128 entries
}

// sensorFor returns the sensor description; unknown types fall back to a
// conservative limit instead of the web driver's 3311 default.
func sensorFor(t int) SensorInfo {
	if s, ok := sensors[t]; ok {
		return s
	}
	return SensorInfo{fmt.Sprintf("0x%02X", t), 100, 12000, []int{400, 800, 1600, 3200, 6400, 12000}}
}

// EncodeDPI mirrors X(): sub 2 = X axis, 18 = Y axis.
func EncodeDPI(axis byte, levels []int, count, current int, sensorType int, seq func() byte) []byte {
	si := sensorFor(sensorType)
	o := []byte{cmdDPI, seq(), axis, byte(current)}
	mask := 0
	for i := 0; i < count && i < 8; i++ {
		mask |= 1 << i
	}
	o = append(o, byte(mask))
	for i := 0; i < 8; i++ {
		de := 0
		if i < count {
			de = 1600
			if i < len(levels) && levels[i] != 0 {
				de = levels[i]
			}
		}
		pe := de/si.Step - 1 // de=0 -> -1 -> 0xFFFF, exactly like the web driver
		o = append(o, byte(pe&0xFF), byte((pe>>8)&0xFF))
	}
	return setCheckSum(pad(o, 30, 0xFF))
}

// EncodeDPIColors mirrors Q(): 8 colours, RGB each.
func EncodeDPIColors(colors [8]uint32, seq func() byte) []byte {
	o := []byte{cmdDPIColors, seq(), 1}
	for _, c := range colors {
		o = append(o, byte(c>>16), byte(c>>8), byte(c))
	}
	return setCheckSum(pad(o, 30, 0xFF))
}

// ---------- performance / power / lighting ----------

func EncodePollRate(code int, seq func() byte) []byte {
	return setCheckSum(pad([]byte{cmdPollRate, seq(), 1, byte(code)}, 30, 0))
}

func EncodeLOD(mm int, seq func() byte) []byte {
	return setCheckSum(pad([]byte{cmdLOD, seq(), 1, byte(mm)}, 30, 0xFF))
}

func EncodePower(sleepMin int, moveWakeup, moveLighting bool, seq func() byte) []byte {
	return setCheckSum(pad([]byte{cmdPower, seq(), 1, byte(sleepMin), b2u(moveWakeup), b2u(moveLighting)}, 30, 0xFF))
}

// EncodeDebounce sets the key debounce threshold (scan ticks). The firmware
// accepts command 9 but no vendor tool sends it: config[0x78] = value.
func EncodeDebounce(v int, seq func() byte) []byte {
	return setCheckSum(pad([]byte{cmdDebounce, seq(), 1, byte(v)}, 30, 0xFF))
}

// EncodeZones sets the lighting-zones byte (config 0x7B). Stock firmware
// stores it and ignores it; the patched firmware (SM809_Mouse_IAP_zones)
// reads bit0 = side buttons off, bit1 = wheel off, bit2 = logo shows the
// fixed colour from DPI colour slot 8 (otherwise the stock colour cycle).
func EncodeZones(v int, seq func() byte) []byte {
	return setCheckSum(pad([]byte{cmdZones, seq(), 1, byte(v)}, 30, 0xFF))
}

func EncodeSensor(angleSnap, motionSync, ripple bool, seq func() byte) []byte {
	return setCheckSum(pad([]byte{cmdSensor, seq(), 1, b2u(angleSnap), b2u(motionSync), b2u(ripple)}, 30, 0xFF))
}

// EncodeLighting mirrors w() for the SM809 scheme, where no mode has
// brightness/speed/colour controls: [255, mode, 0, direction].
func EncodeLighting(mode int, seq func() byte) []byte {
	return setCheckSum(pad([]byte{cmdLighting, seq(), 1, 255, byte(mode), 0, 0}, 30, 0xFF))
}

func EncodeReset(seq func() byte) []byte {
	return setCheckSum(pad([]byte{cmdReset, seq(), 1, 255}, 30, 0xFF))
}

func b2u(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// ---------- macros ----------

// MacroEvent mirrors the web driver's macro event objects.
// Delay is the delay *before* this event (the recorder stores it that way);
// key events take the delay of the following event as their post-delay.
type MacroEvent struct {
	Type      string `json:"type"` // keydown, keyup, delay, cursorMove
	Code      int    `json:"code"` // key code as produced by the web driver's N() table, 240..244 = mouse buttons
	Delay     int    `json:"delay"`
	Direction string `json:"direction,omitempty"` // left/right/up/down for cursorMove
	Value     int    `json:"value,omitempty"`
	Index     int    `json:"index"`
}

type Macro struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	ExecuteType int          `json:"executeType"` // 1 while held, 2 N times, 3 until any key
	RepeatCount int          `json:"repeatCount"`
	Events      []MacroEvent `json:"events"`
}

func clampDelay(d int) int {
	if d < 0 {
		return 0
	}
	if d > maxDelayMs {
		return maxDelayMs
	}
	return d
}

var bitTab = [8]byte{1, 2, 4, 8, 16, 32, 64, 128}

// EncodeMacroEvents is a literal port of R().
func EncodeMacroEvents(m Macro) []byte {
	ev := append([]MacroEvent(nil), m.Events...)
	sort.SliceStable(ev, func(a, b int) bool { return ev[a].Index < ev[b].Index })
	var d []byte
	for i, o := range ev {
		switch o.Type {
		case "delay":
			w := clampDelay(o.Delay)
			d = append(d, byte((w>>8)&15), byte(w&255), 0)
		case "cursorMove":
			v := o.Value
			if v == 0 {
				v = 1
			}
			k := clampDelay(o.Delay)
			le, ne := 112, 0
			switch o.Direction {
			case "left":
				le, ne = 64, (0-v)&255
			case "right":
				le, ne = 65, v
			case "up":
				le, ne = 80, (0-v)&255
			case "down":
				le, ne = 81, v
			}
			d = append(d, byte(le), byte(k&255), byte(ne))
		case "keydown", "keyup":
			k := o.Code
			le := 0
			if i < len(ev)-1 {
				le = clampDelay(ev[i+1].Delay)
			}
			z := (le >> 8) & 15
			ne, ie := 0, k
			switch {
			case k < 145:
				ne = 48
			case k < 232:
				ne, ie = 32, int(bitTab[k&7])
			case k >= 240 && k <= 244:
				ne, ie = 16, int(bitTab[k&7])
			case k == 245:
				ne = 64
			case k == 246:
				ne = 80
			case k == 247:
				ne = 96
			}
			if o.Type == "keyup" {
				z |= 128
			}
			z |= ne
			d = append(d, byte(z), byte(le&255), byte(ie))
		}
	}
	return d
}

// ValidateMacro rejects anything the firmware could mis-handle.
func ValidateMacro(m Macro) error {
	if len(m.Events) == 0 {
		return errors.New(T("макрос пустой", "the macro is empty"))
	}
	n := 0
	for _, e := range m.Events {
		switch e.Type {
		case "keydown", "keyup":
			n++
			k := e.Code
			ok := (k > 0 && k < 145) || (k >= 224 && k <= 231) || (k >= 240 && k <= 244)
			if !ok {
				return fmt.Errorf(T("клавиша с кодом %d не поддерживается в макросах", "key code %d is not supported in macros"), k)
			}
		case "delay", "cursorMove":
			n++
		default:
			return fmt.Errorf(T("неизвестный тип события %q", "unknown event type %q"), e.Type)
		}
		if e.Delay < 0 || e.Delay > maxDelayMs {
			return fmt.Errorf(T("задержка %d мс вне диапазона 0..%d", "delay %d ms is out of range 0..%d"), e.Delay, maxDelayMs)
		}
	}
	if n > macroMaxActs {
		return fmt.Errorf(T("слишком много действий: %d (максимум %d)", "too many actions: %d (max %d)"), n, macroMaxActs)
	}
	if len(EncodeMacroEvents(m)) > macroMaxData {
		return errors.New(T("макрос не помещается в память мыши", "the macro does not fit into the mouse memory"))
	}
	if m.ExecuteType == 2 && (m.RepeatCount < 1 || m.RepeatCount > 255) {
		return errors.New(T("число повторов должно быть от 1 до 255", "repeat count must be 1..255"))
	}
	return nil
}

// EncodeMacroUpload mirrors the per-button part of T(): data packets for one macro.
func EncodeMacroUpload(buttonID int, m Macro, seq func() byte) [][]byte {
	data := EncodeMacroEvents(m)
	ne := len(data)
	ie := ne / 25
	if ne%25 != 0 {
		ie++
	}
	if ie == 0 {
		ie = 1
	}
	slot := buttonID
	if slot == 16 {
		slot = 12
	}
	var pkts [][]byte
	ce := ne
	for y := 0; y < ie; y++ {
		oe := []byte{cmdMacroData, seq(), byte((y<<4)&240 | ie&15)}
		ae := 0
		if ce <= 25 {
			ae = ce
		} else {
			ae = 25
			ce -= 25
		}
		oe = append(oe, byte(1+ae), byte(slot))
		ge := y * 25
		for g := 0; g < ae; g++ {
			if ge+g < len(data) {
				oe = append(oe, data[ge+g])
			} else {
				oe = append(oe, 0)
			}
		}
		oe = pad(oe, 31, 0)
		pkts = append(pkts, setCheckSum(oe))
	}
	return pkts
}

func EncodeMacroErase(seq func() byte) []byte {
	return setCheckSum(pad([]byte{cmdMacroErase, seq(), 1, 255}, 30, 0))
}
