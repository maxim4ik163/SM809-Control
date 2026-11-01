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
