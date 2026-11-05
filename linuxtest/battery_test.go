//go:build ignore

package main

import (
	"testing"
	"time"
)

func TestBatteryForecast(t *testing.T) {
	h := &BatHistory{path: t.TempDir() + "/b.json"}
	t0 := time.Unix(1_700_000_000, 0)
	// previous session: 100 → 60 over 4 days, then charge to full
	for i := 0; i <= 40; i++ {
		h.add(100-i, 0, "dongle", t0.Add(time.Duration(i)*144*time.Minute))
	}
	h.add(60, 1, "wired", t0.Add(97*time.Hour))
	h.add(100, 2, "wired", t0.Add(99*time.Hour))
	f := h.forecast(t0.Add(99 * time.Hour))
	if f.State != "full" || f.LastFull == 0 {
		t.Fatalf("full %+v", f)
	}
	// unplugged at 100; only 1% dropped so far → uses history (10%/day)
	h.add(100, 0, "dongle", t0.Add(100*time.Hour))
	h.add(99, 0, "dongle", t0.Add(102*time.Hour))
	f = h.forecast(t0.Add(102 * time.Hour))
	if f.Basis != "history" || f.RatePerDay < 9.5 || f.RatePerDay > 10.5 || f.Hours < 230 || f.Hours > 245 {
		t.Fatalf("history %+v", f)
	}
	// current session: 100 → 90 in 12 h → 20%/day, 90% left → 108 h
	h.add(90, 0, "dongle", t0.Add(112*time.Hour))
	f = h.forecast(t0.Add(112 * time.Hour))
	if f.Basis != "current" || f.RatePerDay < 19.5 || f.RatePerDay > 20.5 || f.Hours < 105 || f.Hours > 110 {
		t.Fatalf("current %+v", f)
	}
	// charging 20% per hour from 40
	h.add(40, 1, "wired", t0.Add(200*time.Hour))
	h.add(60, 1, "wired", t0.Add(201*time.Hour))
	f = h.forecast(t0.Add(201 * time.Hour))
	if f.State != "charging" || f.Hours < 1.9 || f.Hours > 2.1 {
		t.Fatalf("charging %+v", f)
	}
	// heartbeat dedupe
	n := len(h.Samples)
	h.add(60, 1, "wired", t0.Add(201*time.Hour+time.Minute))
	if len(h.Samples) != n {
		t.Fatal("dup sample stored")
	}
	if loadBatHistory(t.TempDir()).Samples != nil {
		t.Fatal("empty load")
	}
}

func TestBatterySettle(t *testing.T) {
	var g batSettler
	t0 := time.Unix(1_700_000_000, 0)
	p := "dongle"
	if g.settling(p, false, false, t0) {
		t.Fatal("no link must not settle")
	}
	if !g.settling(p, true, false, t0) || !g.settling(p, true, true, t0.Add(14*time.Second)) {
		t.Fatal("must settle right after link-up")
	}
	if g.settling(p, true, true, t0.Add(16*time.Second)) {
		t.Fatal("settled after batSettle")
	}
	// link lost and back: settle again
	g.settling(p, false, true, t0.Add(60*time.Second))
	if !g.settling(p, true, false, t0.Add(61*time.Second)) {
		t.Fatal("must settle after reconnect")
	}
	// unknown path with link (path re-enumerated): treated as link-up
	g.forget(p)
	if !g.settling(p, true, true, t0.Add(100*time.Second)) {
		t.Fatal("forgotten path must settle")
	}
}

func TestSanitizeBattery(t *testing.T) {
	// from the user's battery.json (2026-11-05)
	in := []BatSample{
		{T: 1793656633, Pct: 70},
		{T: 1793710898, Pct: 0}, {T: 1793710899, Pct: 80}, {T: 1793710910, Pct: 70},
		{T: 1793739288, Pct: 50},
		{T: 1793780311, Pct: 0}, {T: 1793780311, Pct: 60}, {T: 1793780323, Pct: 50},
		{T: 1793800000, Pct: 0},            // real 0 (nothing follows soon): kept
		{T: 1793900000, Pct: 0, Charge: 1}, // charging from empty: kept
		{T: 1793900010, Pct: 5, Charge: 1},
	}
	out := sanitizeBat(in)
	want := []int{70, 70, 50, 50, 0, 0, 5}
	if len(out) != len(want) {
		t.Fatalf("got %+v", out)
	}
	for i, w := range want {
		if out[i].Pct != w {
			t.Fatalf("sample %d: got %+v", i, out)
		}
	}
	if out[1].T != 1793710910 || out[3].T != 1793780323 {
		t.Fatalf("wrong samples kept %+v", out)
	}
}
