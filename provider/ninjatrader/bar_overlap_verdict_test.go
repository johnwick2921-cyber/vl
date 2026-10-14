package ninjatrader

import "testing"

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
