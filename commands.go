package main

// Command building shared by the engine and the memory decoder.

import (
	"encoding/hex"
	"strings"
)

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

// zones: the mouse runs the patched firmware with switchable lighting zones.
func buildCommands(p *Profile, macroOf func(string) *Macro, sensor int, zones bool) []devCommand {
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
	if zones {
		// slot 8 is never a DPI level (max 6); the patched firmware shows it on the logo
		colors[logoSlot] = 0
		if !p.LogoOff {
			colors[logoSlot] = parseColor(p.LogoColor, 0xFFFFFF)
		}
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
	if zones {
		v := zonesValue(p)
		cmds = append(cmds, zonesCommand(v))
	}
	return cmds
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func parseHex(s string, def uint32) uint32 { return parseColor(s, def) }

func parseColor(s string, def uint32) uint32 {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return def
	}
	var v uint32
	for _, c := range s {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v |= uint32(c - '0')
		case c >= 'a' && c <= 'f':
			v |= uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v |= uint32(c-'A') + 10
		default:
			return def
		}
	}
	return v
}

const (
	debounceFactory = 2
	debounceMax     = 20
)

// deviceCommands are mouse-wide settings that do not belong to a profile.
func deviceCommands(s Settings) []devCommand {
	if !s.DebounceSet {
		return nil
	}
	v := s.Debounce
	if v < 0 || v > debounceMax {
		v = debounceFactory
	}
	return []devCommand{{"debounce", 1, func(seq func() byte) [][]byte { return [][]byte{EncodeDebounce(v, seq)} }}}
}

func debounceSignature(v int) string {
	return signature(devCommand{"debounce", 1, func(seq func() byte) [][]byte { return [][]byte{EncodeDebounce(v, seq)} }})
}

// plausibleStatus: whether a report (ID at [0]) looks like the status layout
// of input report 4 — used for feature report 5, which the vendor tools poll
// as a "health" report and whose layout is not documented.
func plausibleStatus(d []byte) bool {
	if len(d) < 10 {
		return false
	}
	link, level, rate, battery, charge, sensor := d[1], d[2], d[3], d[4], d[5], d[9]
	if link > 1 || level > 7 || battery > 100 || charge > 2 {
		return false
	}
	if !validRate(int(rate)) {
		return false
	}
	_, known := sensors[int(sensor)]
	return known
}

// ---------- lighting zones (patched firmware) ----------

const (
	logoSlot     = 7 // DPI colour slot used for the logo colour
	zoneSideOff  = 1
	zoneWheelOff = 2
	zoneLogoFix  = 4
)

func zonesValue(p *Profile) int {
	v := zoneLogoFix
	if p.SideOff {
		v |= zoneSideOff
	}
	if p.WheelOff {
		v |= zoneWheelOff
	}
	return v
}

func zonesCommand(v int) devCommand {
	return devCommand{"zones", 1, func(seq func() byte) [][]byte { return [][]byte{EncodeZones(v, seq)} }}
}

func zonesSignature(v int) string { return signature(zonesCommand(v)) }
