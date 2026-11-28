package ninjatrader

import (
	"testing"
	"time"
)

// ── GAP-BACKFILL (2026-10-14) — a replay landing on traded minutes is judged
// on those minutes ──────────────────────────────────────────────────────────
//
// The ring holds live 03:30–03:43 and 07:48–07:52 (the feed came back at
// 07:48). The gap re-request's replay covers 03:30–07:52. Before this wave the
// first live bar after it was compared with the newest HISTORICAL bar left in
// the ring — 07:47, the hole's last minute, five minutes back — and the check
// SKIPPED as "not adjacent" for good, so the replay-hold never released the
// refill to the store (live 10-14 07:50:20: "scale check SKIPPED … replay
// rows held from the store").

const ovT0 = int64(1_791_970_200_000) // a minute boundary; only spacing matters

func ovLive(c float64, mins ...int64) []Bar {
	out := make([]Bar, 0, len(mins))
	for _, m := range mins {
		out = append(out, Bar{T: ovT0 + (m+1)*60_000, O: c, H: c + 1, L: c - 1, C: c, V: 5}) // wire close-stamp
	}
	return out
}

func ovRange(from, to int64) []int64 {
	var out []int64
	for m := from; m <= to; m++ {
		out = append(out, m)
	}
	return out
}

func ovRing(t *testing.T) *BarCache {
	t.Helper()
	c := NewBarCache(0)
	live := append(ovRange(0, 13), ovRange(258, 262)...) // 03:30–03:43 and 07:48–07:52
	for _, m := range live {
		c.Upsert("MNQ", "1m", ovLive(30700, m))
	}
	return c
}

func TestReplayOverlappingLiveMinutesIsJudgedOnThem(t *testing.T) {
	c := ovRing(t)
	c.armOverlapVerdict("MNQ", time.Now().Add(gapReplayJudgeWindow)) // the gap re-request went out
	c.SeedHistorical("MNQ", "1m", ovLive(30700.25, ovRange(0, 262)...))
	checked, off := c.SeedVerdict("MNQ", "1m")
	if !checked || off {
		t.Fatalf("an on-scale replay overlapping 19 live minutes must be judged on them: checked=%v offScale=%v", checked, off)
	}
	if a, d := c.OverlapVerdictCounts(); a != 1 || d != 0 {
		t.Fatalf("overlap verdicts agreed=%d disagreed=%d, want 1/0", a, d)
	}
	if n := len(c.Get("MNQ", "1m")); n != 263 {
		t.Fatalf("the ring must hold the refilled window: %d bars, want 263", n)
	}
	// and the next live bar does not re-judge it against a stale reference
	c.Upsert("MNQ", "1m", ovLive(30700, 263))
	if checked, off := c.SeedVerdict("MNQ", "1m"); !checked || off {
		t.Fatalf("the verdict must stand after the next live bar: checked=%v offScale=%v", checked, off)
	}
}

func TestOffScaleOverlapChangesNothing(t *testing.T) {
	c := ovRing(t)
	c.armOverlapVerdict("MNQ", time.Now().Add(gapReplayJudgeWindow))
	c.SeedHistorical("MNQ", "1m", ovLive(30410, ovRange(0, 262)...)) // ~290 pts below: the 09-10 finding
	checked, off := c.SeedVerdict("MNQ", "1m")
	if checked || off {
		t.Fatalf("a disagreeing overlap adds no verdict — the key stays armed exactly as before: checked=%v offScale=%v", checked, off)
	}
	if a, d := c.OverlapVerdictCounts(); a != 0 || d != 1 {
		t.Fatalf("overlap verdicts agreed=%d disagreed=%d, want 0/1", a, d)
	}
	for _, b := range c.Get("MNQ", "1m") {
		if (b.T <= ovT0+13*60_000 || b.T >= ovT0+258*60_000) && b.C != 30700 {
			t.Fatalf("a live bar was overwritten at %d: %.2f", b.T, b.C)
		}
	}
}

func TestReplayIntoAnEmptyRingIsLeftToTheFirstLiveBar(t *testing.T) {
	c := NewBarCache(0)
	c.SeedHistorical("MNQ", "1m", ovLive(30700, ovRange(0, 10)...))
	if checked, _ := c.SeedVerdict("MNQ", "1m"); checked {
		t.Fatal("a boot replay (no live overlap) must wait for the first live bar, as before")
	}
	if a, d := c.OverlapVerdictCounts(); a != 0 || d != 0 {
		t.Fatalf("no overlap, no verdict: agreed=%d disagreed=%d", a, d)
	}
}

// A replay the gap backfill did NOT ask for — a reconnect replay, the
// scale-break re-request, or one that lands after the window — keeps the
// first-live-bar rule untouched (scale_check_adjacency_test.go).
func TestUnaskedReplayKeepsTheFirstLiveBarRule(t *testing.T) {
	for _, arm := range []bool{false, true} {
		c := ovRing(t)
		if arm {
			c.armOverlapVerdict("MNQ", time.Now().Add(-time.Second)) // window already closed
		}
		c.SeedHistorical("MNQ", "1m", ovLive(30700.25, ovRange(0, 262)...))
		if checked, _ := c.SeedVerdict("MNQ", "1m"); checked {
			t.Fatalf("arm=%v: an unasked replay must not be judged by overlap", arm)
		}
		if a, d := c.OverlapVerdictCounts(); a != 0 || d != 0 {
			t.Fatalf("arm=%v: no overlap verdict outside the window, got %d/%d", arm, a, d)
		}
	}
	// the window is per symbol: ES's re-request does not judge MNQ
	c := ovRing(t)
	c.armOverlapVerdict("ES", time.Now().Add(gapReplayJudgeWindow))
	c.SeedHistorical("MNQ", "1m", ovLive(30700.25, ovRange(0, 262)...))
	if checked, _ := c.SeedVerdict("MNQ", "1m"); checked {
		t.Fatal("another symbol's window must not judge MNQ")
	}
}

// ── #23 review P3-3 — the same-minute tolerance is a few ticks, not 0.5% ──
// The old bound (ScaleMismatchPct, 0.5% ≈ 154 pts on MNQ at 30800) agreed
// with a replay 40 points off its own live minutes. Same instrument, same
// minute: max(8 ticks, 0.05% of price).
func TestOverlapToleranceIsAFewTicksNotHalfAPercent(t *testing.T) {
	cases := []struct {
		name      string
		symbol    string
		live, rep float64
		agree     bool
	}{
		{"MNQ 40 pts off (inside the old 0.5%)", "MNQ", 30700, 30740, false},
		{"MNQ 12 pts off (inside 0.05% ≈ 15.35)", "MNQ", 30700, 30712, true},
		{"ES 5 pts off (0.05% of 6800 = 3.4)", "ES", 6800, 6805, false},
		{"ES 3 pts off", "ES", 6800, 6803, true},
		{"ES 2 pts off (8 ticks)", "ES", 6800, 6802, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewBarCache(0)
			for _, m := range append(ovRange(0, 13), ovRange(258, 262)...) {
				c.Upsert(tc.symbol, "1m", ovLive(tc.live, m))
			}
			c.armOverlapVerdict(tc.symbol, time.Now().Add(gapReplayJudgeWindow))
			c.SeedHistorical(tc.symbol, "1m", ovLive(tc.rep, ovRange(0, 262)...))
			a, d := c.OverlapVerdictCounts()
			if tc.agree && (a != 1 || d != 0) {
				t.Fatalf("%s: want agreed, got agreed=%d disagreed=%d", tc.name, a, d)
			}
			if !tc.agree && (a != 0 || d != 1) {
				t.Fatalf("%s: want DISAGREED, got agreed=%d disagreed=%d", tc.name, a, d)
			}
		})
	}
}

// m438 (#53) — the disagreeing-replay count is kept PER SYMBOL: an ES replay that
// disagrees with ES live minutes is counted on ES only.
// MUTANT (shown RED): count into the MNQ bucket / drop the per-symbol map ->
// OverlapDisagreedFor("ES") reads 0 or MNQ reads 1 -> RED.
func TestOverlapDisagreedIsCountedPerSymbol(t *testing.T) {
	c := NewBarCache(0)
	for _, m := range ovRange(0, 13) {
		c.Upsert("ES", "1m", ovLive(6000, m))
	}
	c.armOverlapVerdict("ES", time.Now().Add(gapReplayJudgeWindow))
	c.SeedHistorical("ES", "1m", ovLive(5700, ovRange(0, 13)...))
	if _, d := c.OverlapVerdictCounts(); d != 1 {
		t.Fatalf("cache-wide disagreed = %d, want 1", d)
	}
	if got := c.OverlapDisagreedFor("es"); got != 1 { // case-insensitive
		t.Fatalf("OverlapDisagreedFor(ES) = %d, want 1", got)
	}
	if got := c.OverlapDisagreedFor("MNQ"); got != 0 {
		t.Fatalf("OverlapDisagreedFor(MNQ) = %d, want 0", got)
	}
	if got := (*BarCache)(nil).OverlapDisagreedFor("ES"); got != 0 {
		t.Fatalf("nil cache = %d", got)
	}
}
