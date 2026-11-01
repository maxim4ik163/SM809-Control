package main

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// The reference packets in testref/ref.json were produced by running the
// vendor web driver's own JavaScript against a fake device (the generator is
// not included because it needs the vendor's code). Every encoder must
// reproduce them byte for byte.

func loadRef(t *testing.T) map[string]json.RawMessage {
	b, err := os.ReadFile("testref/ref.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func refPackets(t *testing.T, m map[string]json.RawMessage, name string) [][]byte {
	var raw [][]int
	if err := json.Unmarshal(m[name], &raw); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	out := make([][]byte, len(raw))
	for i, p := range raw {
		for _, v := range p {
			out[i] = append(out[i], byte(v))
		}
	}
	return out
}

// seqGen reproduces the web driver's counter: starts at 1, pre-increments.
type seqGen struct{ v int }

func (s *seqGen) next() byte { s.v = (s.v + 1) % 256; return byte(s.v) }

func cmp(t *testing.T, name string, got, want [][]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d packets, want %d", name, len(got), len(want))
	}
	for i := range want {
		if fmt.Sprint(got[i]) != fmt.Sprint(want[i]) {
			t.Errorf("%s packet %d:\n got %v\nwant %v", name, i, got[i], want[i])
		}
	}
}

func TestMatchesWebDriver(t *testing.T) {
	ref := loadRef(t)
	s := &seqGen{v: 1}
	seq := s.next

	// ---- macro on button 5 (repeat 3 times) ----
	key := map[string]int{"a": 4, "Shift": 225, "F5": 62, "LMB": 240, "Enter": 40}
	ev := func(i int, typ, k string, d int) MacroEvent {
		return MacroEvent{Index: i, Type: typ, Code: key[k], Delay: d}
	}
	m1 := Macro{Name: "m1", ExecuteType: 2, RepeatCount: 3, Events: []MacroEvent{
		ev(0, "keydown", "a", 0), ev(1, "keyup", "a", 35), ev(2, "delay", "", 1200),
		ev(3, "keydown", "Shift", 20), ev(4, "keydown", "F5", 5000), ev(5, "keyup", "F5", 10),
		ev(6, "keyup", "Shift", 0), ev(7, "keydown", "LMB", 7), ev(8, "keyup", "LMB", 300),
		{Index: 9, Type: "cursorMove", Delay: 15, Direction: "left", Value: 5},
		{Index: 10, Type: "cursorMove", Delay: 260, Direction: "down", Value: 3},
	}}
	got := [][]byte{EncodeMacroErase(seq)}
	got = append(got, EncodeMacroUpload(5, m1, seq)...)
	cmp(t, "macro_btn5", got, refPackets(t, ref, "macro_btn5"))

	long := Macro{Name: "long", ExecuteType: 1}
	for i := 0; i < 70; i++ {
		typ := "keydown"
		if i%2 == 1 {
			typ = "keyup"
		}
		long.Events = append(long.Events, MacroEvent{Index: i, Type: typ, Code: 40, Delay: i * 13 % 4000})
	}
	got = [][]byte{EncodeMacroErase(seq)}
	got = append(got, EncodeMacroUpload(16, long, seq)...)
	cmp(t, "macro_btn16_long", got, refPackets(t, ref, "macro_btn16_long"))

	// The UI's factory-reset button (DeviceView) pads with 0xFF; the HID module's
	// unused reset() pads with 0x00. We send the UI variant; same cmd/seq/checksum rules.
	{
		want := refPackets(t, ref, "reset")[0]
		got := EncodeReset(seq)
		w := append([]byte{}, want[:3]...)
		w = append(w, 255)
		for len(w) < 30 {
			w = append(w, 255)
		}
		w = setCheckSum(w)
		if fmt.Sprint(got) != fmt.Sprint(w) || got[1] != want[1] {
			t.Errorf("reset: got %v want %v", got, w)
		}
	}

	// ---- DPI: X, Y, colours ----
	colors := [8]uint32{0xea5e56, 0x0066ff, 0x32aa70, 0xf59c25, 0x00dde4, 0xb500ec, 0xff6b6b, 0x4ecdc4}
	for _, c := range []struct {
		sensor, cnt, cur int
		lv               []int
	}{
		{17, 6, 2, []int{400, 800, 1600, 3200, 6400, 24000}},
		{149, 4, 3, []int{400, 850, 1650, 26000}},
		{17, 3, 0, []int{1200, 1600, 2400}},
	} {
		name := fmt.Sprintf("dpi_%d_%d_%d", c.sensor, c.cnt, c.cur)
		got := [][]byte{
			EncodeDPI(2, c.lv, c.cnt, c.cur, c.sensor, seq),
			EncodeDPI(18, c.lv, c.cnt, c.cur, c.sensor, seq),
			EncodeDPIColors(colors, seq),
		}
		cmp(t, name, got, refPackets(t, ref, name))
	}

	// ---- 16 buttons ----
	var codes [numButtons]KeyCode
	src := [][]byte{{16, 1, 0}, {16, 2, 0}, {16, 4, 0}, {16, 16, 0}, {16, 8, 0}, {64, 1, 0}, {112, 8, 7}, {112, 0, 31}, {48, 16, 2}, {128, 148, 1}, {96, 4, 0}, {16, 1, 0}, {16, 2, 0}, nil, {64, 2, 0}, {144, 0x2c, 0}}
	for i, c := range src {
		if c != nil {
			codes[i] = KeyCode{c[0], c[1], c[2]}
		}
	}
	cmp(t, "keys16", EncodeKeys(codes, seq), refPackets(t, ref, "keys16"))

	cmp(t, "rate", [][]byte{EncodePollRate(4, seq)}, refPackets(t, ref, "rate"))
	cmp(t, "lod", [][]byte{EncodeLOD(1, seq)}, refPackets(t, ref, "lod"))
	cmp(t, "power", [][]byte{EncodePower(7, true, false, seq)}, refPackets(t, ref, "power"))
	cmp(t, "sensor", [][]byte{EncodeSensor(false, true, true, seq)}, refPackets(t, ref, "sensor"))
	cmp(t, "light9", [][]byte{EncodeLighting(9, seq)}, refPackets(t, ref, "light9"))

	var fin int
	_ = json.Unmarshal(ref["finalSeq"], &fin)
	if s.v != fin {
		t.Errorf("sequence counter %d, web driver ended at %d", s.v, fin)
	}
}

func TestMacroButtonCode(t *testing.T) {
	// web driver zL(): code = [144, H<<4 | id&15, count]; id 16 uses slot 12
	if c := MacroButtonCode(5, 2, 3); c != (KeyCode{144, 0x25, 3}) {
		t.Error(c)
	}
	if c := MacroButtonCode(16, 1, 9); c != (KeyCode{144, 0x1c, 0}) {
		t.Error(c)
	}
	if c := MacroButtonCode(3, 3, 0); c != (KeyCode{144, 0x43, 0}) {
		t.Error(c)
	}
}

func TestPlausibleStatus(t *testing.T) {
	// a real report 4 from the dongle: link, level 3, 500 Hz, 80 %, on battery, light 9, max 1, PAW3311
	ok := []byte{4, 1, 3, 2, 80, 0, 9, 0, 1, 0x11}
	if !plausibleStatus(ok) {
		t.Fatal("real status rejected")
	}
	offline := []byte{4, 0, 3, 2, 80, 0, 9, 0, 1, 0x11}
	if !plausibleStatus(offline) {
		t.Fatal("offline status rejected")
	}
	for i, bad := range [][]byte{
		{5, 1, 3, 2, 80, 0, 9},              // short
		{5, 7, 3, 2, 80, 0, 9, 0, 1, 0x11},  // link flag
		{5, 1, 3, 3, 80, 0, 9, 0, 1, 0x11},  // polling code 3
		{5, 1, 3, 2, 180, 0, 9, 0, 1, 0x11}, // battery
		{5, 1, 3, 2, 80, 0, 9, 0, 1, 0x77},  // unknown sensor
		{5, 0, 0, 0, 0, 0, 0, 0, 0, 0},      // zeros
		{5, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
	} {
		if plausibleStatus(bad) {
			t.Errorf("case %d accepted", i)
		}
	}
}
