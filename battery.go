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

type BatSample struct {
	T      int64  `json:"t"` // unix seconds
	Pct    int    `json:"p"`
	Charge int    `json:"c"` // 0 battery, 1 charging, 2 full
	Mode   string `json:"m,omitempty"`
}

type BatForecast struct {
	State      string  `json:"state"`      // discharging, charging, full, unknown
	Hours      float64 `json:"hours"`      // hours left (to empty or to full); 0 = unknown
	RatePerDay float64 `json:"ratePerDay"` // % per day (discharge) or per hour*24 (charge)
	Basis      string  `json:"basis"`      // current, history, none
	Since      int64   `json:"since"`      // start of the current discharge/charge session
	LastFull   int64   `json:"lastFull"`   // last time the mouse was fully charged / unplugged at 100
}

type BatHistory struct {
	mu      sync.Mutex
	path    string
	Samples []BatSample `json:"samples"`
}

const (
	batKeep      = 60 * 24 * time.Hour
	batHeartbeat = 30 * time.Minute
	batMinDrop   = 3      // % needed before a rate is trusted
	batMinSpan   = 3600.0 // seconds
	batSettle    = 15 * time.Second
)

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
			if len(h.Samples) != n {
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
	t := now.Unix()
	if n := len(h.Samples); n > 0 {
		l := h.Samples[n-1]
		if l.Pct == pct && l.Charge == charge && t-l.T < int64(batHeartbeat/time.Second) {
			return
		}
		if t < l.T { // clock went back
			return
		}
	}
	h.Samples = append(h.Samples, BatSample{T: t, Pct: pct, Charge: charge, Mode: mode})
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
	nowS := float64(now.Unix())

	switch last.Charge {
	case 2:
		f.State = "full"
		return f
	case 1:
		f.State = "charging"
		first := s[st]
		rise := float64(last.Pct - first.Pct)
		span := float64(last.T - first.T)
		if rise >= batMinDrop && span >= 600 {
			perH := rise / (span / 3600)
			f.RatePerDay = perH * 24
			f.Hours = float64(100-last.Pct) / perH
			f.Basis = "current"
		}
		return f
	}

	f.State = "discharging"
	// current session: from the highest point after unplugging to now
	first := s[st]
	for _, x := range s[st:] {
		if x.Pct > first.Pct {
			first = x
		}
	}
	drop := float64(first.Pct - last.Pct)
	span := nowS - float64(first.T)
	if drop >= batMinDrop && span >= batMinSpan {
		perDay := drop / (span / 86400)
		f.RatePerDay = perDay
		f.Hours = float64(last.Pct) / perDay * 24
		f.Basis = "current"
		return f
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
			d, sp := float64(hi.Pct-lo.Pct), float64(lo.T-hi.T)
			if d >= batMinDrop && sp >= batMinSpan {
				totDrop += d
				totSpan += sp
			}
		}
		i = j + 1
	}
	if totDrop > 0 && totSpan > 0 {
		perDay := totDrop / (totSpan / 86400)
		f.RatePerDay = perDay
		f.Hours = float64(last.Pct) / perDay * 24
		f.Basis = "history"
	}
	return f
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
	h.Samples = sanitizeBat(s)
	_ = writeJSONAtomic(h.path, h)
}
