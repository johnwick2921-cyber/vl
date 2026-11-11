package mentor

import (
	"strings"
	"testing"
	"time"
)

// G7 (oct round 2) — Oct D4.2 @ 36:45-40:20: "entry ít nhất cái điểm entry ...
// cao hơn cái đáy trước đó" [R39], "nên cho nó cao hơn tầm 15, 15 điểm" [R41],
// and the Sun 01 Nov example: an entry 6.50 pts above the old low "em cũng
// không gọi nó là cái Potential High Low" [R42]. PHLMinAbovePriorPts (knob
// phl_min_above_old_extreme_pts) refuses a PHL whose entry sits closer than N
// pts to the old low it stands on (mirror PLH); 0 = off (the pre-round-2
// behaviour; the DEFAULT is 0 — the mentor calls 15 experience, not law,
// R39). Real stored 1m bars of Sun 01 Nov 20:30-21:05 CT.
func TestG7EntryMustStandOffTheOldLow(t *testing.T) {
	bars := m413Load(t, "testdata/round2_mnq_1m_20261101_2000ct.csv")
	at := func(h, m int) int { return phl1At(t, bars, 2026, time.November, 1, h, m) }
	old := bars[at(20, 36)].Low // 28,759.75, the old low of the example
	if old != 28759.75 {
		t.Fatalf("fixture drift: old low %.2f", old)
	}
	oldHigh := Level{Price: 28800, Role: OldHigh}
	cases := []struct {
		name   string
		h, m   int
		prior  float64
		wantOK bool
		dist   float64
	}{
		// 20:56 G O 28,764.75 H 28,770.00 L 28,764.75: entry 28,771.00 = old low + 11.25.
		{"entry 11.25 above the old low", 20, 56, old, false, 11.25},
		// 20:57 G H 28,777.00 L 28,768.00: entry 28,778.00 = old low + 18.25.
		{"entry 18.25 above the old low", 20, 57, old, true, 18.25},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ref := bars[at(c.h, c.m)]
			tr := Touch{Outcome: TouchReject, RefBar: ref, ApproachedFrom: SideLong}
			cfg := DefaultConfig()
			cfg.Enabled = true
			cfg.PHLMinAbovePriorPts = 15
			in, ok, why := PHLPLHR2Levels(tr, oldHigh, 0, 100, c.prior, nil, cfg)
			if ok != c.wantOK {
				t.Fatalf("15-pt knob: ok=%v (%s) intent=%+v, want ok=%v at +%.2f over the old low", ok, why, in, c.wantOK, c.dist)
			}
			if !ok && !strings.HasPrefix(why, "entry only") {
				t.Fatalf("the refusal must be the named min-above-old-low one, got %q", why)
			}
			if !ok && phlRefusalKey(why) != "phl_min_above_old_extreme" {
				t.Fatalf("refusal key %q, want phl_min_above_old_extreme", phlRefusalKey(why))
			}
			off := cfg
			off.PHLMinAbovePriorPts = 0
			if _, ok0, why0 := PHLPLHR2Levels(tr, oldHigh, 0, 100, c.prior, nil, off); !ok0 {
				t.Fatalf("knob 0 must restore today's behaviour, refused: %s", why0)
			}
		})
	}
	// Mirror: a short stands PHLMinAbovePriorPts under the old HIGH.
	hi := 28800.0
	ref := bars[at(20, 55)] // L 28,759.00 → entry 28,758.00; 28,758 is 42 under 28,800: allowed
	tr := Touch{Outcome: TouchReject, RefBar: ref, ApproachedFrom: SideShort}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PHLMinAbovePriorPts = 15
	if _, ok, why := PHLPLHR2Levels(tr, Level{Price: 28700, Role: OldLow}, 0, 100, hi, nil, cfg); !ok && strings.HasPrefix(why, "entry only") {
		t.Fatalf("a short 42 pts under the old high must not trip the 15-pt rule: %s", why)
	}
	nearHi := hi - 28758 // 42
	if nearHi < 15 {
		t.Fatal("fixture arithmetic")
	}
	if _, ok, why := PHLPLHR2Levels(tr, Level{Price: 28700, Role: OldLow}, 0, 100, 28765.0, nil, cfg); ok || !strings.HasPrefix(why, "entry only") {
		t.Fatalf("a short 7 pts under the old high must be refused 'entry only', ok=%v why=%q", ok, why)
	}
}

// TestG7DefaultIsOff — the default reproduces today's behaviour: the 11.25-pt
// entry passes without the knob.
func TestG7DefaultIsOff(t *testing.T) {
	if DefaultConfig().PHLMinAbovePriorPts != 0 {
		t.Fatalf("default PHLMinAbovePriorPts = %v, want 0 (off)", DefaultConfig().PHLMinAbovePriorPts)
	}
}
