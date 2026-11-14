//go:build ignore

package main

import (
	"testing"
	"time"
)

func TestBatteryForecast(t *testing.T) {
	h := &BatHistory{path: t.TempDir() + "/b.json"}
	t0 := time.Unix(1_700_000_000, 0)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	// connected from minute a to b, reports every minute, pct(m) as reported
	run := func(a, b int, pct func(m int) int) {
		for m := a; m <= b; m++ {
			h.add(pct(m), 0, "dongle", at(m))
		}
	}
	near := func(v, want, tol float64) bool { return v >= want-tol && v <= want+tol }
	// previous session: 100 → 60 over 4 days of use (10%/day = 0.42%/h), then charge to full
	run(0, 96*60, func(m int) int { return 100 - m/144 })
	h.add(60, 1, "wired", at(97*60))
	h.add(100, 2, "wired", at(99*60))
	f := h.forecast(at(99 * 60))
	if f.State != "full" || f.LastFull == 0 {
		t.Fatalf("full %+v", f)
	}
	// unplugged at 100; only 1% dropped so far → uses history
	run(100*60, 102*60, func(m int) int { return 100 - (m-100*60)/120 })
	f = h.forecast(at(102 * 60))
	if f.Basis != "history" || !near(f.RatePerHour, 10.0/24, 0.02) || f.Hours < 230 || f.Hours > 245 {
		t.Fatalf("history %+v", f)
	}
	// current session: 100 → 90 in 12 h of use → 0.83%/h, 90% left → 108 h
	run(102*60+1, 112*60, func(m int) int { return 100 - (m-100*60)/72 })
	f = h.forecast(at(112 * 60))
	if f.Basis != "current" || !near(f.RatePerHour, 10.0/12, 0.02) || f.Hours < 105 || f.Hours > 110 {
		t.Fatalf("current %+v", f)
	}
	// PC off for 60 h (no samples), then 2 h of use at 80%: 20% in 14 h of use
	// → 1.43%/h, 80% left → 56 h. Calendar time (74 h) would give ~300 h.
	run(172*60, 174*60, func(int) int { return 80 })
	f = h.forecast(at(174 * 60))
	if f.Basis != "current" || !near(f.RatePerHour, 20.0/14, 0.03) || f.Hours < 54 || f.Hours > 58 {
		t.Fatalf("after a break %+v", f)
	}
	// the mouse is off now: the time since the last sample does not count
	if g := h.forecast(at(190 * 60)); !near(g.RatePerHour, f.RatePerHour, 1e-9) {
		t.Fatalf("disconnected now %+v vs %+v", g, f)
	}
	// connected 96+12+2 = 110 h in 174 h of history → 15.2 h a day
	if !near(f.UsePerDay, 110.0/174*24, 0.1) || !near(f.Days, f.Hours/f.UsePerDay, 1e-9) {
		t.Fatalf("use per day %+v", f)
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

func TestBatterySmoother(t *testing.T) {
	var b batSmoother
	t0 := time.Unix(1_700_000_000, 0)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	if b.feed(90, 0, at(0)) != 90 {
		t.Fatal("first value is taken")
	}
	// jitter down and back: stays 90
	for _, x := range []struct{ s, p int }{{10, 80}, {15, 90}, {40, 80}, {80, 90}} {
		if got := b.feed(x.p, 0, at(x.s)); got != 90 {
			t.Fatalf("jitter at %ds: %d", x.s, got)
		}
	}
	// 80 held for 3 minutes: taken
	b.feed(80, 0, at(100))
	if b.feed(80, 0, at(279)) != 90 || b.feed(80, 0, at(280)) != 80 {
		t.Fatal("drop after 3 min")
	}
	// back up on battery: only after 10 minutes
	b.feed(90, 0, at(300))
	if b.feed(90, 0, at(2099)) != 80 || b.feed(90, 0, at(2100)) != 90 {
		t.Fatal("rise after 30 min")
	}
	// charge state change and big jumps are taken at once
	if b.feed(90, 1, at(2101)) != 90 || b.feed(100, 2, at(2102)) != 100 {
		t.Fatal("charge change")
	}
	if b.feed(70, 0, at(2103)) != 70 || b.feed(40, 0, at(2104)) != 40 {
		t.Fatal("jump of 20+")
	}
	// while charging: up after 3 minutes, down only after 30 minutes
	b.feed(40, 1, at(2200))
	b.feed(50, 1, at(2210))
	if b.feed(50, 1, at(2390)) != 50 {
		t.Fatal("charging up")
	}
	b.feed(40, 1, at(2400))
	if b.feed(40, 1, at(3000)) != 50 {
		t.Fatal("charging down too early")
	}
}

func TestSmoothHistory(t *testing.T) {
	// the user's data around the 90 → 80 boundary (2026-11-04)
	raw := []BatSample{
		{T: 1793632792, Pct: 90}, {T: 1793634593, Pct: 90},
		{T: 1793636372, Pct: 80}, {T: 1793636377, Pct: 90}, {T: 1793636458, Pct: 80}, {T: 1793636460, Pct: 90},
		{T: 1793636559, Pct: 80}, {T: 1793636583, Pct: 90}, {T: 1793639193, Pct: 80}, {T: 1793639198, Pct: 90},
		{T: 1793639289, Pct: 80}, {T: 1793639538, Pct: 90}, {T: 1793639561, Pct: 80}, {T: 1793639565, Pct: 90},
		{T: 1793639594, Pct: 80}, {T: 1793641394, Pct: 80},
	}
	out := smoothBat(raw)
	for i := 1; i < len(out); i++ {
		if out[i].Pct > out[i-1].Pct {
			t.Fatalf("rise in smoothed data: %+v", out)
		}
	}
	if out[len(out)-1].Pct != 80 {
		t.Fatalf("end %+v", out)
	}
	// idempotent
	again := smoothBat(out)
	if len(again) != len(out) {
		t.Fatalf("not idempotent: %+v\n→ %+v", out, again)
	}
}

func TestActiveSecs(t *testing.T) {
	s := []BatSample{{T: 0}, {T: 1800}, {T: 3600}, {T: 3600 + 7200}, {T: 3600 + 7200 + 600}}
	cases := []struct{ from, to, want int64 }{
		{0, 3600, 3600},                      // two 30-min heartbeats
		{0, 3600 + 7200, 3600},               // the 2-h gap does not count
		{900, 11400, 2700 + 600},             // partial overlap
		{0, 11400 + 1200, 3600 + 600 + 1200}, // still connected 20 min after the last sample
		{0, 11400 + 3600, 3600 + 600},        // 1 h after the last sample: disconnected
	}
	for _, c := range cases {
		if got := activeSecs(s, c.from, c.to); got != float64(c.want) {
			t.Errorf("activeSecs(%d, %d) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
	if usePerDay(s, 11400) != 0 {
		t.Error("usePerDay with less than 3 days of history")
	}
}
