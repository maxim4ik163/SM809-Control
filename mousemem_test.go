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
	for _, c := range buildCommands(p, mo, 0x11) {
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
