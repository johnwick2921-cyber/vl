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

// m149 (a) — a dead 125 s bar plus a −5.9 s step (below the 6 s freeze) must
// still block via bar_age: raising the freeze to 6 s must not also reopen a
// genuinely stale feed.
func TestM149Dead125sBarWithMinus5p9sStepStillBlocksViaBarAge(t *testing.T) {
	// Land the stepped wall on a clean minute boundary so the B4 formula has
	// no phase slack: wall0 = 08:59:05.9, a −5.9 s step lands at 08:59:00. A
	// bar 125 s behind that (08:56:55) is stale via bar_age.
	wall0 := ctOpen().Add(-54*time.Second - 100*time.Millisecond) // 08:59:05.9
	clk := NewSteppedClock(wall0)
	defer ClockSampleForTest(clk.Sample)()
	ObserveClock()
	clk.Step(-5900 * time.Millisecond) // wall now reads 08:59:00.0
	if v := ClockStepSnapshot(); v.FreezeSteps != 0 || v.EntryHoldActive {
		t.Fatalf("a −5.9 s step is below the 6 s freeze and away from the boundary, must not hold: %+v", v)
	}
	wallNs, _ := clk.Sample()
	now := time.Unix(0, wallNs)
	barOpen := now.UnixMilli() - 125_000
	stale, why := EntryFeedStale("MNQ", barOpen, now)
	if !stale || !strings.Contains(why, "bar_age") {
		t.Fatalf("a 125 s-old bar must still block via bar_age, got stale=%v why=%q", stale, why)
	}
}

// m149 (b) — 12 × −4.9 s steps (each below the 6 s freeze, never a hold) with
// a 136 s-old monotonic receipt must block via receipt_age: the receipt leg is
// the thing that catches a dead feed the wall rule still reads fresh.
func TestM149TwelveMinus4p9sStepsWith136sOldReceiptBlocksViaReceiptAge(t *testing.T) {
	clk := NewSteppedClock(ctOpen())
	defer ClockSampleForTest(clk.Sample)()
	recv := time.Now().Add(-136 * time.Second) // carries the monotonic reading
	FeedLastLiveReceipt = func(string, string) (time.Time, bool) { return recv, true }
	defer func() { FeedLastLiveReceipt = nil }()
	ObserveClock()
	for i := 0; i < 12; i++ {
		clk.Advance(time.Second)
		clk.Step(-4900 * time.Millisecond)
		ObserveClock() // sample each step so they cannot fold into one big jump
	}
	if v := ClockStepSnapshot(); v.FreezeSteps != 0 || v.EntryHoldActive {
		t.Fatalf("12 × −4.9 s steps are each below the freeze and away from the boundary, must not hold: %+v", v)
	}
	wallNs, _ := clk.Sample()
	now := time.Unix(0, wallNs)
	barOpen := (now.UnixMilli() / 60_000) * 60_000 // fresh on the stepped wall
	stale, why := EntryFeedStale("MNQ", barOpen, now)
	if !stale || !strings.Contains(why, "receipt_age") {
		t.Fatalf("a 136 s-old monotonic receipt must block via receipt_age, got stale=%v why=%q", stale, why)
	}
}

// m149 (c) — the freeze boundary pair: 5999 ms is below the 6 s freeze and is
// counted only; 6000 ms is at the freeze and holds.
func TestM149ClockStepFreezeBoundary5999vs6000(t *testing.T) {
	clk := NewSteppedClock(ctOpen())
	restore := ClockSampleForTest(clk.Sample)
	ObserveClock()
	clk.Advance(time.Second)
	clk.Step(-5999 * time.Millisecond)
	if held, why := EntryFeedStale("MNQ", 0, ctOpen()); held {
		restore()
		t.Fatalf("a −5999 ms step is below the 6000 ms freeze, got held: %q", why)
	}
	if v := ClockStepSnapshot(); v.StepsBackward != 1 || v.FreezeSteps != 0 {
		restore()
		t.Fatalf("−5999 ms must be counted, not frozen: %+v", v)
	}
	restore()

	clk = NewSteppedClock(ctOpen())
	defer ClockSampleForTest(clk.Sample)()
	ObserveClock()
	clk.Advance(time.Second)
	clk.Step(-6000 * time.Millisecond)
	if held, why := EntryFeedStale("MNQ", 0, ctOpen()); !held || !strings.Contains(why, "clock_step") {
		t.Fatalf("a −6000 ms step must hold NEW entries, got held=%v why=%q", held, why)
	}
}

// m149 (d) — an EMPTY 1m cache (newest bar = 0) with a wired receipt provider
// that reports no live frame since boot must fail CLOSED (stale), not hit the
// empty-cache fail-open.
func TestM149EmptyCacheWithWiredProviderNoFrameBlocks(t *testing.T) {
	defer ClockSampleForTest(NewSteppedClock(ctOpen()).Sample)()
	FeedLastLiveReceipt = func(string, string) (time.Time, bool) { return time.Time{}, false }
	defer func() { FeedLastLiveReceipt = nil }()
	stale, why := FeedStaleAt("MNQ", 0, ctOpen())
	if !stale || !strings.Contains(why, "since boot") {
		t.Fatalf("an empty 1m cache with a wired provider reporting no live frame must block (fail closed), got stale=%v why=%q", stale, why)
	}
}

// m149 (session boundary) — a 3 s step is below the 6 s freeze, but inside the
// 15:59–16:01 / 16:59–17:01 CT boundary it can flip IsCMEOpen, so it must
// still hold NEW entries. The same 3 s step away from the boundary must not.
func TestM149SessionBoundaryStepBelowFreezeStillHolds(t *testing.T) {
	boundary := time.Date(2026, 9, 23, 16, 0, 30, 0, CTLocation())
	clk := NewSteppedClock(boundary)
	defer ClockSampleForTest(clk.Sample)()
	ObserveClock()
	clk.Advance(time.Second)
	clk.Step(-3000 * time.Millisecond)
	if held, why := EntryFeedStale("MNQ", 0, boundary); !held || !strings.Contains(why, "clock_step") {
		t.Fatalf("a 3 s step inside the 15:59–16:01 boundary must hold NEW entries, got held=%v why=%q", held, why)
	}

	clk2 := NewSteppedClock(ctOpen())
	restore := ClockSampleForTest(clk2.Sample)
	ObserveClock()
	clk2.Advance(time.Second)
	clk2.Step(-3000 * time.Millisecond)
	if held, why := EntryFeedStale("MNQ", 0, ctOpen()); held {
		restore()
		t.Fatalf("a 3 s step away from the boundary must not hold, got %q", why)
	}
	restore()
}

// m149 (session boundary unit pin) — at 15:59:30 CT a 1.0 s step is below the
// 2 s boundary threshold and must NOT hold; a 2.5 s step is at/above it and
// must hold. This pin goes RED if the boundary threshold is compared against
// nanoseconds instead of milliseconds.
func TestM149SessionBoundary1sStepNotHeld2p5sStepHeld(t *testing.T) {
	boundary := time.Date(2026, 9, 23, 15, 59, 30, 0, CTLocation())

	clk := NewSteppedClock(boundary)
	restore := ClockSampleForTest(clk.Sample)
	ObserveClock()
	clk.Step(-1000 * time.Millisecond)
	if held, why := EntryFeedStale("MNQ", 0, boundary); held {
		restore()
		t.Fatalf("a 1.0 s step inside the boundary is below the 2 s boundary threshold, must not hold: %q", why)
	}
	if v := ClockStepSnapshot(); v.FreezeSteps != 0 {
		restore()
		t.Fatalf("a 1.0 s boundary step must not freeze: %+v", v)
	}
	restore()

	clk = NewSteppedClock(boundary)
	defer ClockSampleForTest(clk.Sample)()
	ObserveClock()
	clk.Step(-2500 * time.Millisecond)
	if held, why := EntryFeedStale("MNQ", 0, boundary); !held || !strings.Contains(why, "clock_step") {
		t.Fatalf("a 2.5 s step inside the boundary must hold NEW entries, got held=%v why=%q", held, why)
	}
}

// m168 (1) — the 15:59–16:01 / 16:59–17:01 windows are pinned exhaustively:
// inside → a 3 s step holds, a 1 s step does not; outside → a 3 s step does not;
// the inclusive edges hold; and a FORWARD step holds like a backward one.
func TestM168SessionBoundaryWindowPins(t *testing.T) {
	at := func(h, m, s int) time.Time { return time.Date(2026, 9, 23, h, m, s, 0, CTLocation()) }
	cases := []struct {
		name   string
		wall0  time.Time
		stepMs int64
		held   bool
	}{
		{"16:59:30 -3s holds", at(16, 59, 30), -3000, true},
		{"16:59:30 -1s not held", at(16, 59, 30), -1000, false},
		{"17:00:30 -3s holds", at(17, 0, 30), -3000, true},
		{"17:00:30 -1s not held", at(17, 0, 30), -1000, false},
		{"15:58:30 -3s not held", at(15, 58, 30), -3000, false},
		{"16:01:30 -3s not held", at(16, 1, 30), -3000, false},
		{"edge 15:59:00 +3s holds", at(15, 58, 57), 3000, true}, // lands exactly 15:59:00
		{"edge 17:01:00 +3s holds", at(17, 0, 57), 3000, true},  // lands exactly 17:01:00
		{"forward 16:59:30 +3s holds", at(16, 59, 30), 3000, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clk := NewSteppedClock(c.wall0)
			defer ClockSampleForTest(clk.Sample)()
			ObserveClock()
			clk.Step(time.Duration(c.stepMs) * time.Millisecond)
			held, why := EntryFeedStale("MNQ", 0, c.wall0)
			if held != c.held {
				t.Fatalf("step %+dms landing at %s: held=%v (%q), want %v", c.stepMs, clkSampleWall(t, clk), held, why, c.held)
			}
		})
	}
}

// clkSampleWall returns the wall reading of a SteppedClock formatted HH:MM:SS.
func clkSampleWall(t *testing.T, c *SteppedClock) string {
	t.Helper()
	wallNs, _ := c.Sample()
	return time.Unix(0, wallNs).In(CTLocation()).Format("15:04:05")
}

// m168 (2) — a shortened session (early close) is a boundary too: on
// 2026-11-27 (Day after Thanksgiving, close 12:15 CT) a 3 s step landing at
// 12:14:59 is inside the close ±60 s window and must hold.
func TestM168SessionBoundaryEarlyClose(t *testing.T) {
	wall0 := time.Date(2026, 11, 27, 12, 14, 56, 0, CTLocation()) // +3 s → 12:14:59
	clk := NewSteppedClock(wall0)
	defer ClockSampleForTest(clk.Sample)()
	ObserveClock()
	clk.Step(3000 * time.Millisecond)
	if held, why := EntryFeedStale("MNQ", 0, wall0); !held || !strings.Contains(why, "clock_step") {
		t.Fatalf("a 3 s step landing at 12:14:59 on the 12:15 early-close day must hold, got held=%v why=%q", held, why)
	}

	// Control: 12:13:30, outside the close ±60 s window → a 3 s step must NOT hold.
	wall0b := time.Date(2026, 11, 27, 12, 13, 30, 0, CTLocation())
	clk2 := NewSteppedClock(wall0b)
	restore := ClockSampleForTest(clk2.Sample)
	ObserveClock()
	clk2.Step(-3000 * time.Millisecond)
	if held, why := EntryFeedStale("MNQ", 0, wall0b); held {
		restore()
		t.Fatalf("a 3 s step at 12:13:30 on the early-close day is outside the window, must not hold: %q", why)
	}
	restore()
}
