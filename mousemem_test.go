package main

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// testref/flash holds a real dump of a mouse together with what the app had
// written to it. Decoding the dump and encoding it again must give exactly
// the packets that were sent.
func TestDecodeRealDump(t *testing.T) {
	cfg, err := os.ReadFile("testref/flash/config_7C000.bin")
	if err != nil {
		t.Fatal(err)
	}
	msec, _ := os.ReadFile("testref/flash/macros_7D000.bin")
	var st struct {
		Profile *Profile
		Device  *DeviceState
	}
	b, _ := os.ReadFile("testref/flash/app_state.json")
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	mem := &MouseMemory{Config: cfg[:cfgLen], Macros: map[int][]byte{}}
	for btn := 1; btn <= 16; btn++ {
		a := macroSlotAddr(btn)
		if a >= 0x7D000 {
			o := a - 0x7D000
			mem.Macros[btn] = msec[o : o+macroSlotLen]
		}
	}
	p, macros, err := decodeMouse(mem, 0x11)
	if err != nil {
		t.Fatal(err)
	}
	if len(macros) != 1 || len(macros[0].Events) != 28 {
		t.Fatalf("macros %+v", macros)
	}
	if mem.Debounce != 2 {
		t.Fatalf("debounce %d", mem.Debounce)
	}
	mo := func(id string) *Macro {
		for _, m := range macros {
			if m.ID == id {
				return m
			}
		}
		return nil
	}
	for _, c := range buildCommands(p, mo, 0x11, false) {
		if got, want := signature(c), st.Device.Sent[c.key]; got != want {
			t.Errorf("%s:\n got %s\nwant %s", c.key, short(got), short(want))
		}
	}
	if p.DPICount != 4 || p.DPILevels[3] != 3600 || p.Lighting != 9 || p.SleepMin != 5 || !p.MoveLightOff || p.PollRate != 2 || p.LOD != 1 {
		t.Fatalf("profile %+v", p)
	}
	_ = hex.EncodeToString
}

func short(s string) string { return strings.ReplaceAll(s, "|", "\n     ") }

// Zones (patched firmware): the zones byte and the logo colour in DPI colour
// slot 8 survive a write → read round trip, and the packets are as expected.
func TestZones(t *testing.T) {
	cfg, err := os.ReadFile("testref/flash/config_7C000.bin")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		side, wheel, logoOff bool
		color                string
		raw                  byte
		slot                 [3]byte
	}{
		{false, false, false, "#12ab34", 4, [3]byte{0x12, 0xab, 0x34}},
		{true, false, false, "#ffffff", 5, [3]byte{0xff, 0xff, 0xff}},
		{false, true, true, "#ffffff", 6, [3]byte{0, 0, 0}},
		{true, true, true, "#ffffff", 7, [3]byte{0, 0, 0}},
	} {
		p := &Profile{DPIColors: defaultDPIColors, SideOff: tc.side, WheelOff: tc.wheel, LogoOff: tc.logoOff, LogoColor: tc.color}
		var zones, colors []byte
		for _, c := range buildCommands(p, func(string) *Macro { return nil }, 0x11, true) {
			switch c.key {
			case "zones":
				zones = c.build(func() byte { return 0 })[0]
			case "dpiColors":
				colors = c.build(func() byte { return 0 })[0]
			}
		}
		if zones == nil || zones[0] != 10 || zones[2] != 1 || zones[4] != tc.raw {
			t.Fatalf("zones packet % X", zones)
		}
		if got := [3]byte{colors[4+3*7], colors[5+3*7], colors[6+3*7]}; got != tc.slot {
			t.Fatalf("slot 8 % X want % X (packet % X)", got, tc.slot, colors)
		}
		// what the mouse would store
		c := append([]byte(nil), cfg[:cfgLen]...)
		c[cfgZones] = tc.raw
		copy(c[cfgColors+3*logoSlot:], tc.slot[:])
		mem := &MouseMemory{Config: c, Macros: map[int][]byte{}}
		for btn := 1; btn <= 16; btn++ {
			mem.Macros[btn] = make([]byte, macroSlotLen)
		}
		q, _, err := decodeMouse(mem, 0x11)
		if err != nil {
			t.Fatal(err)
		}
		if q.SideOff != tc.side || q.WheelOff != tc.wheel || q.LogoOff != tc.logoOff || q.LogoColor != tc.color || mem.Zones != int(tc.raw) {
			t.Fatalf("decoded %v %v %v %s %d", q.SideOff, q.WheelOff, q.LogoOff, q.LogoColor, mem.Zones)
		}
		if zonesSignature(mem.Zones) != signature(zonesCommand(zonesValue(q))) {
			t.Fatal("zones signature mismatch")
		}
	}
}
