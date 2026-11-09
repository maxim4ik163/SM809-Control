package main

// Decoding of the mouse's own settings flash (read with the IAP report 0xBF,
// see flashdump.go). Layout found by reading the firmware and confirmed with
// a dump of a mouse whose settings were known.
//
// Config sector 0x7C000 = RAM copy of the settings struct (0x81 bytes):
//
//	0x00..0x04  header (not interpreted)
//	0x05..0x34  16 buttons × 3 bytes, in the order of the keys command packets
//	            (button indices 11,12,13,14,15,9,10,0,1, 2,3,4,5,8,6,7)
//	0x35..0x37  unused 17th entry
//	0x38        current DPI level        (cmd 2)
//	0x39        enabled-levels bit mask  (cmd 2)
//	0x3A..0x49  8 × DPI X (lo,hi; value = (code+1)×step)
//	0x4A..0x59  8 × DPI Y
//	0x5A..0x71  8 × DPI colour R,G,B      (cmd 3)
//	0x72        polling-rate code         (cmd 4)
//	0x73        LOD                       (cmd 5)
//	0x74..0x76  angle snap, motion sync, ripple (cmd 8)
//	0x77        sleep minutes             (cmd 7)
//	0x78        key debounce (scan ticks) (cmd 9 — not used by vendor tools)
//	0x7A        sensor type
//	0x7B        lighting zones (cmd 10; ignored by stock firmware, read by the zones patch)
//	0x7C..0x7E  lighting mode, brightness/speed, direction (cmd 6)
//	0x7F        wake on movement          (cmd 7)
//	0x80        lighting off while moving (cmd 7)
//
// Macro data: slot index i = (button slot − 1) where the slot is the button
// number (16 → 12); i < 8 → 0x7D000 + i×0x200, else 0x7B000 + (i−8)×0x200.
// Data is the event stream of EncodeMacroEvents, padded with zeros.

import (
	"errors"
	"fmt"
)

const (
	cfgAddr   = 0x7C000
	cfgLen    = 0x81
	cfgKeys   = 0x05
	cfgDPICur = 0x38
	cfgDPIMsk = 0x39
	cfgDPIX   = 0x3A
	cfgColors = 0x5A
	cfgPoll   = 0x72
	cfgLOD    = 0x73
	cfgSensor = 0x74
	cfgSleep  = 0x77
	cfgDebnc  = 0x78
	cfgSType  = 0x7A
	cfgZones  = 0x7B
	cfgLight  = 0x7C
	cfgWake   = 0x7F
	cfgLOff   = 0x80

	macroSlotLen = 0x200
)

func macroSlotAddr(button int) uint32 {
	slot := button
	if slot == 16 {
		slot = 12
	}
	i := uint32(slot - 1)
	if i < 8 {
		return 0x7D000 + i*macroSlotLen
	}
	return 0x7B000 + (i-8)*macroSlotLen
}

// MouseMemory is what was read from the mouse.
type MouseMemory struct {
	Config   []byte         // cfgLen bytes from cfgAddr
	Macros   map[int][]byte // button number (1-based) → raw slot
	Debounce int            `json:"debounce"`
	Zones    int            // raw zones byte (config 0x7B)
	ZonesFW  bool           // the zones firmware patch is present
}

// decodeMacroEvents is the inverse of EncodeMacroEvents. Key events carry
// the delay of the following event; delay and cursor events carry their own.
func decodeMacroEvents(raw []byte) ([]MacroEvent, error) {
	var out []MacroEvent
	next := -1 // delay that the previous key event announced for this event
	for i := 0; i+3 <= len(raw); i += 3 {
		b0, b1, b2 := raw[i], raw[i+1], raw[i+2]
		if b0 == 0 && b1 == 0 && b2 == 0 {
			break // padding
		}
		if len(out) >= macroMaxActs {
			return nil, errors.New("macro too long")
		}
		e := MacroEvent{Index: len(out)}
		hi := int(b0 & 0x0F)
		switch {
		case b0 == 0x40 || b0 == 0x41 || b0 == 0x50 || b0 == 0x51:
			e.Type = "cursorMove"
			e.Delay = int(b1)
			v := int(b2)
			switch b0 {
			case 0x40:
				e.Direction, e.Value = "left", (256-v)&255
			case 0x41:
				e.Direction, e.Value = "right", v
			case 0x50:
				e.Direction, e.Value = "up", (256-v)&255
			case 0x51:
				e.Direction, e.Value = "down", v
			}
			next = -1
		case b0&0x70 == 0:
			e.Type = "delay"
			e.Delay = hi<<8 | int(b1)
			if b2 != 0 || b0&0x80 != 0 {
				return nil, fmt.Errorf("bad delay event % X", raw[i:i+3])
			}
			next = -1
		default:
			e.Type = "keydown"
			if b0&0x80 != 0 {
				e.Type = "keyup"
			}
			if next >= 0 {
				e.Delay = next
			}
			bit := func(v byte) (int, bool) {
				for k := 0; k < 8; k++ {
					if v == 1<<k {
						return k, true
					}
				}
				return 0, false
			}
			switch b0 & 0x70 {
			case 0x30:
				e.Code = int(b2)
			case 0x20:
				k, ok := bit(b2)
				if !ok {
					return nil, fmt.Errorf("bad modifier % X", raw[i:i+3])
				}
				e.Code = 224 + k
			case 0x10:
				k, ok := bit(b2)
				if !ok || k > 4 {
					return nil, fmt.Errorf("bad mouse button % X", raw[i:i+3])
				}
				e.Code = 240 + k
			default:
				return nil, fmt.Errorf("unsupported macro event % X", raw[i:i+3])
			}
			next = hi<<8 | int(b1)
		}
		// a delay/cursor event right after a key event: its own delay and the
		// one announced by the key event are the same value by construction
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, errors.New("empty macro")
	}
	return out, nil
}

// decodeMouse turns the raw memory into a profile (and macros for buttons
// that have one). sensorType comes from the status report (falls back to the
// value stored in the config).
func decodeMouse(mem *MouseMemory, sensorType int) (*Profile, []*Macro, error) {
	c := mem.Config
	if len(c) < cfgLen {
		return nil, nil, errors.New("config too short")
	}
	blank := true
	for _, b := range c[cfgKeys:cfgLen] {
		if b != 0xFF {
			blank = false
			break
		}
	}
	if blank {
		return nil, nil, errors.New(T("в памяти мыши нет сохранённых настроек (заводское состояние)", "the mouse memory holds no saved settings (factory state)"))
	}
	if sensorType == 0 {
		sensorType = int(c[cfgSType])
	}
	si := sensorFor(sensorType)
	p := &Profile{Name: T("Из мыши", "From the mouse")}

	// buttons
	for k, idx := range dualPacketOrder {
		o := cfgKeys + 3*k
		code := [3]int{int(c[o]), int(c[o+1]), int(c[o+2])}
		p.Buttons[idx] = ButtonAssign{Kind: kindOfCode(code[0]), Code: code}
	}
	// DPI
	mask := c[cfgDPIMsk]
	for i := 0; i < 8; i++ {
		lo, hi := int(c[cfgDPIX+2*i]), int(c[cfgDPIX+2*i+1])
		if lo == 0xFF && hi == 0xFF {
			p.DPILevels[i] = si.Default[min(i, len(si.Default)-1)]
			continue
		}
		p.DPILevels[i] = (hi<<8 | lo + 1) * si.Step
		if p.DPILevels[i] > si.MaxDPI {
			p.DPILevels[i] = si.MaxDPI
		}
	}
	for i := 0; i < 8; i++ {
		if mask&(1<<i) != 0 {
			p.DPICount = i + 1
		}
	}
	if p.DPICount < 1 {
		p.DPICount = 1
	}
	if p.DPICount > maxDPILevels {
		p.DPICount = maxDPILevels
	}
	p.DPIStart = int(c[cfgDPICur])
	if p.DPIStart >= p.DPICount {
		p.DPIStart = 0
	}
	for i := 0; i < 8; i++ {
		o := cfgColors + 3*i
		p.DPIColors[i] = fmt.Sprintf("#%02x%02x%02x", c[o], c[o+1], c[o+2])
	}
	// performance
	p.PollRate = int(c[cfgPoll])
	if !validRate(p.PollRate) {
		p.PollRate = 1
	}
	p.LOD = int(c[cfgLOD])
	if p.LOD != 1 && p.LOD != 2 {
		p.LOD = 2
	}
	p.AngleSnap, p.MotionSync, p.Ripple = c[cfgSensor] == 1, c[cfgSensor+1] == 1, c[cfgSensor+2] == 1
	p.SleepMin = int(c[cfgSleep])
	if p.SleepMin < 1 || p.SleepMin > 20 {
		p.SleepMin = 20
	}
	p.MoveWakeup, p.MoveLightOff = c[cfgWake] == 1, c[cfgLOff] == 1
	p.Lighting = int(c[cfgLight])
	if p.Lighting < 0 || p.Lighting >= len(lightingModes) {
		p.Lighting = 2
	}
	mem.Debounce = int(c[cfgDebnc])
	// lighting zones (patched firmware); the logo colour lives in DPI colour slot 8
	z := int(c[cfgZones])
	mem.Zones = z
	p.SideOff, p.WheelOff = z&zoneSideOff != 0, z&zoneWheelOff != 0
	lc := cfgColors + 3*logoSlot
	if c[lc]|c[lc+1]|c[lc+2] == 0 {
		p.LogoOff, p.LogoColor = true, defaultLogoColor
	} else {
		p.LogoColor = p.DPIColors[logoSlot]
	}

	// macros
	var macros []*Macro
	for i := range p.Buttons {
		b := &p.Buttons[i]
		if b.Code[0] != 144 {
			continue
		}
		raw := mem.Macros[i+1]
		evs, err := decodeMacroEvents(raw)
		if err != nil {
			// keep the button usable rather than failing the whole read
			b.Kind, b.Code = "special", [3]int{96, 0, 0}
			continue
		}
		exec, rep := 1, 0
		switch b.Code[1] >> 4 {
		case 2:
			exec, rep = 2, b.Code[2]
		case 4:
			exec = 3
		}
		m := &Macro{ID: newID("m"), Name: fmt.Sprintf(T("Из мыши, кнопка %d", "From the mouse, button %d"), i+1),
			ExecuteType: exec, RepeatCount: rep, Events: evs}
		if exec != 2 {
			m.RepeatCount = 1
		}
		macros = append(macros, m)
		b.Kind, b.MacroID = "macro", m.ID
	}
	if !hasLeftClick(p) {
		return nil, nil, errors.New(T("прочитанные данные не похожи на настройки (нет левой кнопки)", "the data does not look like settings (no left click)"))
	}
	return p, macros, nil
}

func hasLeftClick(p *Profile) bool {
	for _, b := range p.Buttons {
		if b.Kind != "macro" && b.Code == [3]int{16, 1, 0} {
			return true
		}
	}
	return false
}

func kindOfCode(c int) string {
	switch c {
	case 16:
		return "mouse"
	case 48:
		return "fire"
	case 64:
		return "dpi"
	case 80:
		return "dpilock"
	case 96:
		return "special"
	case 112:
		return "key"
	case 128:
		return "media"
	case 144:
		return "macro"
	}
	return "special"
}
