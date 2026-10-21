package kernel

import (
	"strings"
	"testing"
	"time"
)

// m134 — wall steps below ~6 s must never trip the stale-data block; a real
// stale feed still blocks. Two pins:
//
//  1. a −5143 ms wall step with a FRESH bar → no block (the step is below the
//     new 6 s freeze threshold and the fresh bar reads fresh);
//  2. a 120 s-old bar → block (the bar-age leg still refuses a genuinely stale
//     feed).

func TestM134WallStepBelow6sWithFreshBarDoesNotBlock(t *testing.T) {
	clk := NewSteppedClock(ctOpen())
	defer ClockSampleForTest(clk.Sample)()
	ObserveClock() // baseline
	clk.Advance(time.Second)
	clk.Step(-5143 * time.Millisecond)

	// The fresh 1m bar is the current minute's open.
	barOpen := (ctOpen().UnixMilli() / 60_000) * 60_000
	wallNs, _ := clk.Sample()
	now := time.Unix(0, wallNs) // the stepped wall reading (5.1 s behind)
	if held, why := EntryFeedStale("MNQ", barOpen, now); held {
		t.Fatalf("a −5143 ms step with a fresh bar must not block, got %q", why)
	}
}

func TestM134StaleBarStillBlocks(t *testing.T) {
	// No step: a 120 s-old bar must still read stale.
	now := ctOpen()
	barOpen := now.UnixMilli() - 120_000
	stale, why := EntryFeedStale("MNQ", barOpen, now)
	if !stale {
		t.Fatalf("a 120 s-old bar must still block, got %q", why)
	}
	if !strings.Contains(why, "bar_age") {
		t.Fatalf("the refusal must name the bar-age leg, got %q", why)
	}
}
