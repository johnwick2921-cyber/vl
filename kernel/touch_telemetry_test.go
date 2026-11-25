package kernel

import (
	"math"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// touchBars builds ascending 1m bars; each bar is [open,high,low,close,vol].
func touchBars(start time.Time, rows [][5]float64) []market.Kline {
	out := make([]market.Kline, len(rows))
	for i, r := range rows {
		t := start.Add(time.Duration(i) * time.Minute)
		out[i] = market.Kline{
			OpenTime: t.UnixMilli(), Open: r[0], High: r[1], Low: r[2], Close: r[3], Volume: r[4],
			CloseTime: t.Add(time.Minute).UnixMilli() - 1,
		}
	}
	return out
}

func touchLevels(prices ...float64) []ScoredLevel {
	out := make([]ScoredLevel, 0, len(prices))
	for i, p := range prices {
		out = append(out, ScoredLevel{
			DetectedLevel: DetectedLevel{Kind: KindPDH, Price: p, Label: "L" + string(rune('A'+i))},
			Grade:         "A", Fresh: "fresh", Distance: p - 100,
		})
	}
	return out
}

// runTouch feeds TouchUpdate once per minute (the live cycle cadence) and
// accumulates closed episodes.
func runTouch(uid, symbol string, bars []market.Kline, lv []ScoredLevel, atr float64, minutes int) []TouchEpisode {
	start := time.UnixMilli(bars[0].OpenTime)
	var out []TouchEpisode
	for m := 1; m <= minutes; m++ {
		now := start.Add(time.Duration(m) * time.Minute)
		out = append(out, TouchUpdate(uid, symbol, bars, lv, atr, now)...)
	}
	return out
}

// TestTouchPenetrationEpisodeScoped locks the live-caught bug: a PRE-episode
// bar 83pts beyond the level must NOT count as penetration (the 14:50:54 live
// line said "through 83pt" while the episode was 4pts-wide).
func TestTouchPenetrationEpisodeScoped(t *testing.T) {
	uid := "t-epscoped"
	start := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	bars := touchBars(start, [][5]float64{
		{180, 181, 179, 180, 5}, // pre-episode, FAR above the level
		{179, 180, 178, 179, 5},
		{99, 100.5, 98.5, 100, 8}, // touch from below (dist 0)
		{100, 100.8, 99.6, 100.2, 8},
		{104.6, 106, 104.6, 105, 5}, // leaves band
	})
	lv := touchLevels(100)
	closed := runTouch(uid, "MNQ", bars, lv, 2.0, 5)
	if len(closed) != 1 {
		t.Fatalf("want 1 closed episode, got %d", len(closed))
	}
	if closed[0].PenetrationPts > 6.0 {
		t.Fatalf("penetration = %.2f — pre-episode bars leaked in (want ≤ 6)", closed[0].PenetrationPts)
	}
}

// TestTouchEpisodeOpenClose (T5) — an episode opens when price comes within the
// band, stays ONE episode across consecutive in-band bars (dedup), and closes
// when price leaves the band.
func TestTouchEpisodeOpenClose(t *testing.T) {
	uid := "t-open-close"
	start := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	bars := touchBars(start, [][5]float64{
		{100, 101, 99, 100, 10},   // far
		{100, 101, 99, 100, 10},   // far
		{100, 102, 100, 101, 12},  // approaches (dist 1 <= 4) → OPEN
		{101, 102.5, 101, 102, 9}, // still in band → same episode
		{102, 104, 102, 103.5, 8}, // still near
		{105, 106, 104.5, 105, 7}, // leaves band (close 105 > level+4) → closes
	})
	lv := touchLevels(100)
	closed := runTouch(uid, "MNQ", bars, lv, 2.0, 3)
	if len(closed) != 0 {
		t.Fatalf("cycle 3 must not close: %+v", closed)
	}
	if got := len(ActiveTouchEpisodes(uid, "MNQ", 101)); got != 1 {
		t.Fatalf("active episodes = %d, want exactly 1 (dedup: one episode, not one per bar)", got)
	}
	ep := ActiveTouchEpisodes(uid, "MNQ", 101)[0]
	if ep.Number != 1 {
		t.Fatalf("touch number = %d, want 1 (1st touch)", ep.Number)
	}
	closed = runTouch(uid, "MNQ", bars, lv, 2.0, 6)
	if len(closed) != 1 {
		t.Fatalf("must close exactly 1 episode, got %d", len(closed))
	}
	if closed[0].BarsIn < 2 {
		t.Fatalf("episode bars = %d, want >= 2", closed[0].BarsIn)
	}
	if got := len(ActiveTouchEpisodes(uid, "MNQ", 104)); got != 0 {
		t.Fatalf("episode must be closed, %d active", got)
	}
}

// TestTouchOrphanedEpisodeClosed — bug-hunt #21: a level that drops out of the
// seated set (or a session-day roll) left its episode active forever, rendering
// as a ghost TOUCH line. TouchUpdate now closes and returns it. Mutant (drop the
// orphan sweep) → RED.
func TestTouchOrphanedEpisodeClosed(t *testing.T) {
	uid := "t-orphan"
	start := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	bars := touchBars(start, [][5]float64{
		{100, 101, 99, 100, 10},  // far
		{100, 101, 99, 100, 10},  // far
		{100, 102, 100, 101, 12}, // approaches → OPEN (dist 1 <= band)
	})
	lv := touchLevels(100)
	runTouch(uid, "MNQ", bars, lv, 2.0, 3)
	if got := len(ActiveTouchEpisodes(uid, "MNQ", 100)); got != 1 {
		t.Fatalf("fixture: want 1 active episode, got %d", got)
	}
	closed := TouchUpdate(uid, "MNQ", bars, nil, 2.0, start.Add(4*time.Minute))
	if len(closed) != 1 {
		t.Fatalf("orphaned episode must close when its level leaves the seated set, got %d", len(closed))
	}
	if got := len(ActiveTouchEpisodes(uid, "MNQ", 100)); got != 0 {
		t.Fatalf("orphaned episode must not stay active, got %d", got)
	}
}

// TestTouchPenetrationMath (T5) — wick vs body penetration and close-side math
// on a golden fixture: price probes through the level on a wick, closes back
// below → wick-through, 1m reject.
func TestTouchPenetrationMath(t *testing.T) {
	uid := "t-pen"
	start := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	// Level at 100, approached from below.
	bars := touchBars(start, [][5]float64{
		{98, 99, 97, 98.5, 10},
		{98.5, 100.2, 98.4, 99.8, 10},  // within band → OPEN; wick 0.2 through
		{99.8, 102.0, 99.7, 99.9, 14},  // wick-through 2.0, close BACK below → reject
		{99.9, 100.3, 99.6, 100.1, 12}, // wick 0.3, close THROUGH → accept
		{100.1, 103, 100.0, 102.5, 16}, // close through → accept
		{105, 106, 104.5, 105, 10},     // leaves band → close episode
	})
	lv := touchLevels(100)
	closed := runTouch(uid, "MNQ", bars, lv, 2.0, 6)
	if len(closed) != 1 {
		t.Fatalf("want 1 closed episode, got %d", len(closed))
	}
	ep := closed[0]
	if math.Abs(ep.PenetrationPts-6.0) > 0.01 {
		t.Fatalf("penetration = %.2f, want 6.0 (closing-bar high 106 − level 100)", ep.PenetrationPts)
	}
	if math.Abs(ep.WickPenPts-6.0) > 0.01 {
		t.Fatalf("wick pen = %.2f, want 6.0", ep.WickPenPts)
	}
	if math.Abs(ep.BodyPenPts-5.0) > 0.01 {
		t.Fatalf("body pen = %.2f, want 5.0 (close 105 through)", ep.BodyPenPts)
	}
	if ep.Close1m != "accept" {
		t.Fatalf("last 1m close = %q, want accept (closed through at 102.5)", ep.Close1m)
	}
	if ep.Shape != "acceptance" {
		t.Fatalf("shape = %q, want acceptance (closes through)", ep.Shape)
	}
}

// A14 (mega-research 2026-08-26) — the volRatio lookback branch was dead code:
// the baseline averaged ALL pre-bars. Now only the last `lookback` pre-bars
// count. A distant volume burst must NOT damp a quiet recent baseline.
func TestTouchVolRatioLookbackTruncates(t *testing.T) {
	start := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	var ring []market.Kline
	add := func(n int, vol float64) {
		for i := 0; i < n; i++ {
			ms := start.Add(time.Duration(len(ring)) * time.Minute).UnixMilli()
			ring = append(ring, market.Kline{OpenTime: ms, CloseTime: ms + 59_999, Open: 100, High: 101, Low: 99, Close: 100, Volume: vol})
		}
	}
	add(5, 1000) // distant burst — must be dropped by the 20-bar truncation
	add(20, 1)   // the actual recent baseline
	add(1, 30)   // the episode bar
	openAt := ring[len(ring)-1].OpenTime
	got := volRatio(ring, openAt, 20)
	// Old (dead-branch) behavior: baseline ≈ (5000+20)/25 ≈ 201 → ratio ≈ 0.15.
	if got < 15 {
		t.Fatalf("vol ratio = %.2f, want ≥15 (lookback truncation must exclude the distant burst)", got)
	}
	// Boundary: lookback=3 uses exactly the last 3 pre-bars.
	if v := volRatio(ring, openAt, 3); v != 30.0 {
		t.Fatalf("lookback=3 baseline must be the last 3 pre-bars (vol 1 each): got %.2f", v)
	}
}

// TestTouchVolRatioAndApproach (T5) — volume ratio against the pre-episode
// average and approach speed in ATR multiples.
func TestTouchVolRatioAndApproach(t *testing.T) {
	uid := "t-vol"
	start := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	bars := touchBars(start, [][5]float64{
		{94, 95, 93, 94.5, 5}, // pre (beyond band)
		{94, 95.5, 93.5, 95, 5},
		{95, 96, 94, 95.5, 5},
		{95, 97, 94.5, 96.5, 6},     // approach (dist 3 ≤ 4 → OPEN at min 4)
		{97, 100.5, 96.8, 99.8, 40}, // touch: vol 40 vs avg ~5 → ~8×
		{99.8, 101, 99, 100.5, 30},
		{104.6, 106, 104.6, 105, 5}, // whole range beyond the band → close
	})
	lv := touchLevels(100)
	closed := runTouch(uid, "MNQ", bars, lv, 2.0, 7)
	if len(closed) != 1 {
		t.Fatalf("want 1 closed episode, got %d", len(closed))
	}
	ep := closed[0]
	if ep.VolRatio < 3.0 {
		t.Fatalf("vol ratio = %.2f, want >= 3 (spike)", ep.VolRatio)
	}
	if ep.ApproachATR <= 0 {
		t.Fatalf("approach ATR = %.2f, want > 0", ep.ApproachATR)
	}
}

// TestTouchPromptRenderAndCard (T5) — the TOUCH line renders the spec shape;
// the card state machine answers approaching/touching/rejected/accepted.
func TestTouchPromptRenderAndCard(t *testing.T) {
	uid := "t-render"
	start := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	bars := touchBars(start, [][5]float64{
		{98, 99, 97, 98.5, 10},
		{98.5, 100.2, 98.4, 99.8, 10}, // OPEN
		{99.8, 101, 99.7, 100.6, 10},  // accept building
	})
	lv := touchLevels(100)
	runTouch(uid, "MNQ", bars, lv, 2.0, 2)
	line := RenderTouchLines(uid, "MNQ", 99.8, 2)
	for _, want := range []string{"TOUCH:", "1st touch", "through", "shape: forming"} {
		if !strings.Contains(line, want) {
			t.Fatalf("touch line %q missing %q", line, want)
		}
	}
	// Card states: touching while the episode is open.
	now := start.Add(2 * time.Minute)
	if st := TouchStateForCard(uid, "MNQ", "LA", 100, 99.8, now.UnixMilli()); st != "touching" {
		t.Fatalf("card state = %q, want touching", st)
	}
	// Approaching: a level 6pts away (within 2×band=8).
	if st := TouchStateForCard(uid, "MNQ", "LB", 105, 99.8, now.UnixMilli()); st != "approaching" {
		t.Fatalf("card state = %q, want approaching", st)
	}
	// Far: nothing.
	if st := TouchStateForCard(uid, "MNQ", "LC", 130, 99.8, now.UnixMilli()); st != "" {
		t.Fatalf("card state = %q, want empty", st)
	}
	// Close the episode as a rejection and check the rejected chip.
	bars2 := touchBars(start, [][5]float64{
		{98, 99, 97, 98.5, 10},
		{98.5, 100.2, 98.4, 99.8, 10},
		{99.8, 101, 99.7, 100.6, 10},
		{101, 102, 100.5, 101.5, 10}, // through then out
		{104.6, 106, 104.6, 105, 10}, // whole range beyond the band
	})
	TouchUpdate(uid, "MNQ", bars2, lv, 2.0, start.Add(5*time.Minute))
	// Rejection fixture: price probes and closes back below.
	uid2 := "t-reject"
	bars3 := touchBars(start, [][5]float64{
		{98, 99, 97, 98.5, 10},
		{98.5, 101.0, 98.4, 99.9, 10}, // wick through 1.0, close back below
		{99.9, 100.2, 99.4, 99.8, 10},
		{99.8, 100.1, 99.5, 99.9, 10},
		{99.9, 100.3, 99.6, 99.7, 10},
	})
	lv2 := touchLevels(100)
	closed := TouchUpdate(uid2, "MNQ", bars3, lv2, 2.0, start.Add(4*time.Minute))
	if len(closed) != 0 {
		t.Fatalf("still open (band re-entry) — got %d closed", len(closed))
	}
	// After the episode max-bars it closes; force via bars past the band.
	bars4 := append(bars3, market.Kline{
		OpenTime: start.Add(5 * time.Minute).UnixMilli(), Open: 104.6, High: 106, Low: 104.6, Close: 105, Volume: 5,
		CloseTime: start.Add(6*time.Minute).UnixMilli() - 1,
	})
	closed = TouchUpdate(uid2, "MNQ", bars4, lv2, 2.0, start.Add(6*time.Minute))
	if len(closed) == 1 && closed[0].Shape == "rejection" {
		if st := TouchStateForCard(uid2, "MNQ", "LA", 100, 103, start.Add(6*time.Minute).UnixMilli()); st != "rejected" {
			t.Fatalf("card state after rejection = %q, want rejected", st)
		}
	}
}
