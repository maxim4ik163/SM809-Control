package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Battery history and a simple remaining-time forecast.
//
// A sample is stored when the percentage or the charge state changes, plus a
// heartbeat every 30 minutes so that flat periods are visible on the chart.
// Only reports from a connected mouse are recorded.
//
// Right after the link comes up (PC start, dongle plugged in, mouse woken up)
// the firmware reports nonsense for a few seconds: first 0 %, then a value
// about 10 % too high, and the real value ~12 s later (battery.json of
// 2026-11-05: 0 → 80 → 70, 0 → 60 → 50, 0 → 40 → 30). Battery values are
// therefore ignored for batSettle after link-up (batSettler); the last known
// value is shown meanwhile. sanitizeBat removes such glitches from old data.
//
// The firmware reports the charge in 10 % steps, and near a step boundary it
// jumps back and forth for up to an hour (80 → 90 → 80 … every few seconds:
// the voltage sags under load and recovers at rest). batSmoother keeps a
// stable value: a step in the expected direction (down on battery, up while
// charging) is taken once it has held for batDropHold, a step back only after
// batRiseHold, a jump of batJump or more and any change of the charge state at
// once. The same filter is applied once to the stored history (smoothBat).

type BatSample struct {
	T      int64  `json:"t"` // unix seconds
	Pct    int    `json:"p"`
	Charge int    `json:"c"` // 0 battery, 1 charging, 2 full
	Mode   string `json:"m,omitempty"`
}

// Discharge is measured against the time the mouse was connected (see
// activeSecs), not calendar time: with the PC off at night or over a weekend
// the mouse sleeps and hardly drains, and counting those hours made the
// forecast jump several times up after every break. So Hours is hours of use;
// Days converts it to calendar days with the average use per day.
type BatForecast struct {
	State       string  `json:"state"`       // discharging, charging, full, unknown
	Hours       float64 `json:"hours"`       // discharging: hours of use left; charging: hours to full; 0 = unknown
	RatePerHour float64 `json:"ratePerHour"` // % per hour of use (discharge) or per hour (charge)
	UsePerDay   float64 `json:"usePerDay"`   // hours connected per day over the last batUseWindow; 0 = too little history
	Days        float64 `json:"days"`        // discharging: calendar days left at that routine; 0 = unknown
	Basis       string  `json:"basis"`       // current, history, none
	Since       int64   `json:"since"`       // start of the current discharge/charge session
	LastFull    int64   `json:"lastFull"`    // last time the mouse was fully charged / unplugged at 100
}

type BatHistory struct {
	mu       sync.Mutex
	path     string
	Samples  []BatSample `json:"samples"`
	Smoothed int         `json:"smoothed,omitempty"` // 1 = old data passed through smoothBat
}

const (
	batKeep      = 60 * 24 * time.Hour
	batHeartbeat = 30 * time.Minute
	batMinDrop   = 3      // % needed before a rate is trusted
	batMinSpan   = 3600.0 // seconds
	batSettle    = 15 * time.Second
	batDropHold  = 3 * time.Minute  // step in the expected direction
	batRiseHold  = 30 * time.Minute // step against it (up on battery, down while charging)
	batJump      = 20               // % taken at once (charged elsewhere, …)
	batGap       = 35 * time.Minute // longer without samples = mouse was not connected
	batUseWindow = 14 * 24 * time.Hour
	batUseMin    = 3 * 24 * time.Hour // history needed before UsePerDay is trusted
)

// batSmoother turns the jittery 10 % steps of the firmware into a stable value.
type batSmoother struct {
	ok      bool
	pct     int
	charge  int
	hasCand bool
	cand    int
	since   time.Time
	at      time.Time // since when the stable value holds (start of the held step)
}

// seed sets the stable value without waiting (e.g. from the history).
func (b *batSmoother) seed(pct, charge int) {
	*b = batSmoother{ok: true, pct: pct, charge: charge}
}

func (b *batSmoother) take(pct, charge int, at time.Time) {
	b.seed(pct, charge)
	b.at = at
}

// feed takes a reported value and returns the stable one.
func (b *batSmoother) feed(pct, charge int, now time.Time) int {
	if !b.ok || charge != b.charge {
		b.take(pct, charge, now)
		return pct
	}
	d := pct - b.pct
	if d == 0 {
		b.hasCand = false
		return b.pct
	}
	if d >= batJump || d <= -batJump {
		b.take(pct, charge, now)
		return pct
	}
	hold := batDropHold
	if (charge == 0) == (d > 0) {
		hold = batRiseHold
	}
	if !b.hasCand || b.cand != pct {
		b.hasCand, b.cand, b.since = true, pct, now
	}
	if now.Sub(b.since) >= hold {
		b.take(pct, charge, b.since)
	}
	return b.pct
}

// appendSample adds a sample unless it repeats the last one within the
// heartbeat, or the clock went back.
func appendSample(s []BatSample, x BatSample) []BatSample {
	if n := len(s); n > 0 {
		l := s[n-1]
		if l.Pct == x.Pct && l.Charge == x.Charge && x.T-l.T < int64(batHeartbeat/time.Second) {
			return s
		}
		if x.T < l.T {
			return s
		}
	}
	return append(s, x)
}

// smoothBat replays the history through batSmoother. Each sample's value is
// assumed to have held until the next sample, unless the gap is so long that
// the mouse was not connected in between. An accepted step is dated by when it
// started to hold, so applying smoothBat again changes nothing.
func smoothBat(s []BatSample) []BatSample {
	var b batSmoother
	out := make([]BatSample, 0, len(s))
	put := func(x BatSample, raw int, t int64) {
		x.T = t
		x.Pct = b.feed(raw, x.Charge, time.Unix(t, 0))
		if n := len(out); n > 0 && (out[n-1].Pct != x.Pct || out[n-1].Charge != x.Charge) {
			if at := b.at.Unix(); at < x.T {
				for len(out) > 0 && out[len(out)-1].T > at {
					out = out[:len(out)-1] // old value repeated after the step began
				}
				y := x
				y.T = at
				out = appendSample(out, y)
			}
		}
		out = appendSample(out, x) // dropped as a repeat unless it is a heartbeat
	}
	for i, x := range s {
		put(x, x.Pct, x.T)
		if i+1 < len(s) {
			next := s[i+1].T
			if next > x.T && next-x.T <= int64(batGap/time.Second) {
				put(x, x.Pct, next)
			}
		}
	}
	return out
}

// batSettler remembers when each HID path got its link, to ignore the battery
// readings of the first batSettle after that.
type batSettler struct {
	up map[string]time.Time
}

// settling reports whether battery values from this report must be ignored.
// wasLinked = the previous report of the same path had a link.
func (g *batSettler) settling(path string, link, wasLinked bool, now time.Time) bool {
	if g.up == nil {
		g.up = map[string]time.Time{}
	}
	if !link {
		delete(g.up, path)
		return false
	}
	t, ok := g.up[path]
	if !wasLinked || !ok {
		g.up[path] = now
		t = now
	}
	return now.Sub(t) < batSettle
}

func (g *batSettler) forget(path string) {
	delete(g.up, path)
}

// sanitizeBat drops start-up glitches recorded by older versions: a 0 %
// sample (not charging) followed within a minute by a non-zero one, and the
// too-high value right after it when the real one follows within 30 s.
func sanitizeBat(s []BatSample) []BatSample {
	out := make([]BatSample, 0, len(s))
	for i := 0; i < len(s); i++ {
		x := s[i]
		if x.Pct == 0 && x.Charge == 0 && i+1 < len(s) && s[i+1].Pct > 0 && s[i+1].T-x.T <= 60 {
			if i+2 < len(s) && s[i+2].Charge == s[i+1].Charge && s[i+2].T-s[i+1].T <= 30 {
				i++ // skip the too-high one as well
			}
			continue
		}
		out = append(out, x)
	}
	return out
}

func loadBatHistory(dir string) *BatHistory {
	h := &BatHistory{path: filepath.Join(dir, "battery.json")}
	if b, err := os.ReadFile(h.path); err == nil {
		_ = json.Unmarshal(b, h)
		if n := len(h.Samples); n > 0 {
			h.Samples = sanitizeBat(h.Samples)
			changed := len(h.Samples) != n
			if h.Smoothed < 1 {
				h.Samples = smoothBat(h.Samples)
				changed = true
			}
			if changed {
				h.Smoothed = 1
				_ = writeJSONAtomic(h.path, h)
			}
		}
	}
	return h
}

// last returns the most recent sample.
func (h *BatHistory) last() (BatSample, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if n := len(h.Samples); n > 0 {
		return h.Samples[n-1], true
	}
	return BatSample{}, false
}

func (h *BatHistory) add(pct, charge int, mode string, now time.Time) {
	if pct < 0 || pct > 100 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	n := len(h.Samples)
	h.Samples = appendSample(h.Samples, BatSample{T: now.Unix(), Pct: pct, Charge: charge, Mode: mode})
	if len(h.Samples) == n {
		return
	}
	h.Smoothed = 1
	cut := now.Add(-batKeep).Unix()
	i := 0
	for i < len(h.Samples) && h.Samples[i].T < cut {
		i++
	}
	h.Samples = h.Samples[i:]
	_ = writeJSONAtomic(h.path, h)
}

func (h *BatHistory) since(t int64) []BatSample {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []BatSample
	for _, s := range h.Samples {
		if s.T >= t {
			out = append(out, s)
		}
	}
	return out
}

func (h *BatHistory) forecast(now time.Time) BatForecast {
	h.mu.Lock()
	s := append([]BatSample(nil), h.Samples...)
	h.mu.Unlock()
	return batForecast(s, now)
}

// session returns the index where the trailing run of samples with the same
// "charging" flag as the last one starts.
func sessionStart(s []BatSample) int {
	n := len(s)
	charging := s[n-1].Charge != 0
	i := n - 1
	for i > 0 && (s[i-1].Charge != 0) == charging {
		i--
	}
	return i
}

// activeSecs returns how long the mouse was connected between from and to.
// While connected a sample is stored at least every batHeartbeat, so the time
// between two samples counts unless the gap exceeds batGap; after the last
// sample the time up to `to` counts only within batGap (still connected).
func activeSecs(s []BatSample, from, to int64) float64 {
	gap := int64(batGap / time.Second)
	over := func(a, b int64) int64 {
		a, b = max(a, from), min(b, to)
		return max(b-a, 0)
	}
	var tot int64
	for i := 0; i+1 < len(s); i++ {
		if d := s[i+1].T - s[i].T; d > 0 && d <= gap {
			tot += over(s[i].T, s[i+1].T)
		}
	}
	if n := len(s); n > 0 && to-s[n-1].T <= gap {
		tot += over(s[n-1].T, to)
	}
	return float64(tot)
}

// usePerDay: average hours the mouse was connected per calendar day over the
// last batUseWindow; 0 while the history is shorter than batUseMin.
func usePerDay(s []BatSample, now int64) float64 {
	if len(s) == 0 {
		return 0
	}
	from := max(now-int64(batUseWindow/time.Second), s[0].T)
	cal := float64(now - from)
	if cal < batUseMin.Seconds() {
		return 0
	}
	return activeSecs(s, from, now) / cal * 24
}

func batForecast(s []BatSample, now time.Time) BatForecast {
	f := BatForecast{State: "unknown", Basis: "none"}
	if len(s) == 0 {
		return f
	}
	for i := len(s) - 1; i >= 0; i-- {
		if s[i].Charge == 2 || (i > 0 && s[i-1].Charge != 0 && s[i].Charge == 0 && s[i-1].Pct >= 95) {
			f.LastFull = s[i].T
			break
		}
	}
	last := s[len(s)-1]
	st := sessionStart(s)
	f.Since = s[st].T
	nowS := now.Unix()

	switch last.Charge {
	case 2:
		f.State = "full"
		return f
	case 1:
		// charging also goes on with the PC off (USB charger), so calendar time
		f.State = "charging"
		first := s[st]
		rise := float64(last.Pct - first.Pct)
		span := float64(last.T - first.T)
		if rise >= batMinDrop && span >= 600 {
			perH := rise / (span / 3600)
			f.RatePerHour = perH
			f.Hours = float64(100-last.Pct) / perH
			f.Basis = "current"
		}
		return f
	}

	f.State = "discharging"
	f.UsePerDay = usePerDay(s, nowS)
	done := func(perH float64, basis string) BatForecast {
		f.RatePerHour = perH
		f.Hours = float64(last.Pct) / perH
		f.Basis = basis
		if f.UsePerDay > 0 {
			f.Days = f.Hours / f.UsePerDay
		}
		return f
	}
	// current session: from the highest point after unplugging to now; the
	// whole drop counts (a step that shows up after a break was mostly used up
	// before it), the time only while connected
	first := s[st]
	for _, x := range s[st:] {
		if x.Pct > first.Pct {
			first = x
		}
	}
	drop := float64(first.Pct - last.Pct)
	span := activeSecs(s, first.T, nowS)
	if drop >= batMinDrop && span >= batMinSpan {
		return done(drop/(span/3600), "current")
	}

	// otherwise: average over earlier discharge sessions
	var totDrop, totSpan float64
	i := 0
	for i < st {
		j := i
		for j+1 < st && (s[j+1].Charge != 0) == (s[i].Charge != 0) {
			j++
		}
		if s[i].Charge == 0 {
			hi, lo := s[i], s[j]
			for _, x := range s[i : j+1] {
				if x.Pct > hi.Pct {
					hi = x
				}
			}
			d, sp := float64(hi.Pct-lo.Pct), activeSecs(s[i:j+1], hi.T, lo.T)
			if d >= batMinDrop && sp >= batMinSpan {
				totDrop += d
				totSpan += sp
			}
		}
		i = j + 1
	}
	if totDrop > 0 && totSpan > 0 {
		return done(totDrop/(totSpan/3600), "history")
	}
	return f
}

// fmtUse: hours of use, "64 ч", "40 мин" (never days: 48 h of use is not two days).
func fmtUse(h float64) string {
	if h >= 1 {
		return fmt.Sprintf(T("%d ч", "%dh"), int(h+0.5))
	}
	return fmt.Sprintf(T("%d мин", "%d min"), int(h*60+0.5))
}

// fmtDays: "8 дней", "2 дня", "1 день".
func fmtDays(d float64) string {
	n := max(int(d+0.5), 1)
	if !langRU.Load() {
		if n == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", n)
	}
	w := "дней"
	if n%100 < 11 || n%100 > 14 {
		switch n % 10 {
		case 1:
			w = "день"
		case 2, 3, 4:
			w = "дня"
		}
	}
	return fmt.Sprintf("%d %s", n, w)
}

// leftLine: "≈ 64 ч работы (~8 дней)" for a discharge forecast, "" if none.
func (f BatForecast) leftLine() string {
	if f.State != "discharging" || f.Hours <= 0 {
		return ""
	}
	t := "≈ " + fmtUse(f.Hours) + T(" работы", " of use")
	if f.Days > 0 {
		t += " (~" + fmtDays(f.Days) + ")"
	}
	return t
}

// fmtHours: "2 д 5 ч", "7 ч", "40 мин".
func fmtHours(h float64) string {
	switch {
	case h >= 48:
		d := int(h / 24)
		return fmt.Sprintf(T("%d д %d ч", "%dd %dh"), d, int(h)-d*24)
	case h >= 1:
		return fmt.Sprintf(T("%d ч", "%dh"), int(h+0.5))
	}
	return fmt.Sprintf(T("%d мин", "%d min"), int(h*60+0.5))
}

// replace swaps the whole history (restore from a backup).
func (h *BatHistory) replace(s []BatSample) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Samples = smoothBat(sanitizeBat(s))
	h.Smoothed = 1
	_ = writeJSONAtomic(h.path, h)
}
