package kernel

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// ── release #22 OPS-SAFETY — the stale gate on the monotonic clock ──────────

// ctOpen is a CME-open instant (Wed 2026-09-23 09:00 CT).
func ctOpen() time.Time { return time.Date(2026, 9, 23, 9, 0, 0, 0, CTLocation()) }

// The 10-14 shape: wall −125.0 s, then +124.4 s 4.9 s later. Both steps are
// seen, counted by direction, and each one holds NEW entries for the hold
// window of MONOTONIC time — the gate fails CLOSED, never open.
func TestClockStepBackwardHoldsEntriesThenClearsOnMonotonicTime(t *testing.T) {
	clk := NewSteppedClock(ctOpen())
	defer ClockSampleForTest(clk.Sample)()

	ObserveClock() // baseline sample
	if held, _ := EntryFeedStale("MNQ", 0, ctOpen()); held {
		t.Fatalf("no step yet: the entry gate must be open")
	}
	clk.Advance(time.Second)
	clk.Step(-125 * time.Second)
	held, why := EntryFeedStale("MNQ", 0, ctOpen())
	if !held || !strings.Contains(why, "clock_step") {
		t.Fatalf("a −125 s wall step must hold NEW entries (fail closed), got held=%v why=%q", held, why)
	}
	clk.Advance(4900 * time.Millisecond)
	clk.Step(124400 * time.Millisecond)
	ObserveClock()
	v := ClockStepSnapshot()
	if v.StepsBackward != 1 || v.StepsForward != 1 || v.FreezeSteps != 2 {
		t.Fatalf("want 1 backward + 1 forward step, both freezing; got %+v", v)
	}
	if v.LargestBackMs > -125000 || v.LargestFwdMs < 124000 {
		t.Fatalf("largest steps not recorded: %+v", v)
	}
	if !v.EntryHoldActive {
		t.Fatalf("health must show the hold active right after the step pair: %+v", v)
	}
	// 119 s of real time after the LAST step: still held.
	clk.Advance(119 * time.Second)
	if held, _ := EntryFeedStale("MNQ", 0, ctOpen()); !held {
		t.Fatalf("the hold must last %ds of monotonic time after the last step", LoadClockStepConfig().HoldS)
	}
	clk.Advance(2 * time.Second)
	if held, why := EntryFeedStale("MNQ", 0, ctOpen()); held {
		t.Fatalf("the hold must clear after the window, got %q", why)
	}
}

// A sub-threshold step (tonight's median −0.26 s) is logged and counted but
// does not freeze entries.
func TestClockStepBelowFreezeThresholdIsCountedNotHeld(t *testing.T) {
	clk := NewSteppedClock(ctOpen())
	defer ClockSampleForTest(clk.Sample)()
	ObserveClock()
	clk.Advance(time.Second)
	clk.Step(-260 * time.Millisecond)
	if held, why := EntryFeedStale("MNQ", 0, ctOpen()); held {
		t.Fatalf("a 260 ms step is below the freeze threshold, got held: %q", why)
	}
	if v := ClockStepSnapshot(); v.StepsBackward != 1 || v.FreezeSteps != 0 {
		t.Fatalf("the small step must be counted, not frozen: %+v", v)
	}
}

// THE RECEIPT LEG: a feed whose last live frame arrived 200 s ago (monotonic)
// is stale even when the wall clock has stepped back 125 s and the bar
// therefore reads only 75 s old on the wall rule.
func TestFeedStaleAtReceiptLegCatchesADeadFeedTheWallRuleMisses(t *testing.T) {
	defer ClockSampleForTest(NewSteppedClock(ctOpen()).Sample)()
	real := ctOpen()
	wall := real.Add(-125 * time.Second)
	barOpen := real.Add(-200 * time.Second).UnixMilli()
	if StaleEntryGateFeed(barOpen, wall) {
		t.Fatalf("precondition: the wall rule alone reads this dead feed as FRESH under the step (the defect)")
	}
	recv := time.Now().Add(-200 * time.Second) // carries the monotonic reading
	FeedLastLiveReceipt = func(string, string) (time.Time, bool) { return recv, true }
	defer func() { FeedLastLiveReceipt = nil }()
	stale, why := FeedStaleAt("MNQ", barOpen, wall)
	if !stale || !strings.Contains(why, "receipt_age") {
		t.Fatalf("the monotonic receipt leg must refuse a 200 s-dead feed, got stale=%v why=%q", stale, why)
	}
	// A live feed (frame 2 s ago) passes the receipt leg.
	recv = time.Now().Add(-2 * time.Second)
	if stale, why := FeedStaleAt("MNQ", real.Add(-30*time.Second).UnixMilli(), real); stale {
		t.Fatalf("a live feed must pass, got %q", why)
	}
	// A wired provider with no live frame since boot: stale (fail-closed).
	FeedLastLiveReceipt = func(string, string) (time.Time, bool) { return time.Time{}, false }
	if stale, why := FeedStaleAt("MNQ", real.Add(-30*time.Second).UnixMilli(), real); !stale || !strings.Contains(why, "since boot") {
		t.Fatalf("no live frame since boot must read stale, got stale=%v why=%q", stale, why)
	}
}

// B4 on the AI path (applyStaleDataBlock, the production evaluator): a stepped
// SnapshotMs no longer hides a dead feed — the receipt leg measures at the
// snapshot's MONOTONIC instant.
func TestB4ReceiptLegRefusesUnderABackwardWallStep(t *testing.T) {
	defer ClockSampleForTest(NewSteppedClock(ctOpen()).Sample)()
	snapAt := time.Now()
	wallMs := snapAt.UnixMilli() - 125_000 // the wall clock reads 125 s behind
	barOpen := snapAt.UnixMilli() - 200_000
	md := &market.Data{TimeframeData: map[string]*market.TimeframeSeriesData{
		"1m": {Klines: []market.KlineBar{{Time: barOpen, Close: 30000}}},
	}}
	ctx := &Context{TraderID: "t1", SnapshotMs: wallMs, SnapshotAt: snapAt, MarketDataMap: map[string]*market.Data{"MNQ": md}}
	FeedLastLiveReceipt = func(string, string) (time.Time, bool) { return snapAt.Add(-200 * time.Second), true }
	defer func() { FeedLastLiveReceipt = nil }()
	fd := &FullDecision{Decisions: []Decision{{Action: "open_long", Symbol: "MNQ"}, {Action: "close_long", Symbol: "MNQ"}}}
	applyStaleDataBlock(fd, ctx, wallMs)
	if fd.Decisions[0].Action != "wait" || !strings.Contains(fd.Decisions[0].RefusalReason, "receipt_age") {
		t.Fatalf("B4 must refuse the entry on the receipt leg, got %q (%q)", fd.Decisions[0].Action, fd.Decisions[0].RefusalReason)
	}
	if fd.Decisions[1].Action != "close_long" {
		t.Fatalf("exits are never gated, got %q", fd.Decisions[1].Action)
	}
}

// The only production callers of the wall-only rule are inside kernel: every
// gate outside it must use EntryFeedStale / FeedStaleAt (a census, so a new
// call site cannot quietly reintroduce the fail-open).
func TestWallOnlyStaleRuleHasNoCallerOutsideKernel(t *testing.T) {
	offenders := grepGoOutside(t, "..", "kernel.StaleEntryGateFeed(")
	if len(offenders) > 0 {
		t.Fatalf("kernel.StaleEntryGateFeed is the WALL-ONLY leg and fails open on a backward clock step; use kernel.EntryFeedStale (entries) or kernel.FeedStaleAt (prices): %v", offenders)
	}
}

// grepGoOutside lists non-test .go files outside kernel/ under root that
// contain needle (comments included — a doc line naming it is fine to fix).
func grepGoOutside(t *testing.T, root, needle string) []string {
	t.Helper()
	var hits []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "web", "node_modules", ".claude", ".understand-anything", "kernel", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr == nil && strings.Contains(string(b), needle) {
			hits = append(hits, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return hits
}

// m134 — the freeze default is 6000 ms (was 2000, before that 1000). The
// 10-14 WSL jitter (1.0–1.2 s) and the 10-21 00:44–04:00 NTP corrections
// (2–5 s) are all below 6 s, so they are logged and counted, never held; a
// 7 s step still holds NEW entries for the (unchanged) 120 s. The receipt leg
// never reads the wall clock and is not touched. MUTANT:
// defaultClockStepFreezeMs back to 1000 → the 1.2 s case RED.
func TestClockStepDefaultFreezeIs6sWSLJitterIsCountedNotHeld(t *testing.T) {
	t.Setenv("CLOCK_STEP_FREEZE_MS", "")
	t.Setenv("CLOCK_STEP_HOLD_S", "")
	// +1192 ms — the larger of the two 10-14 RTH steps: counted, NOT held.
	clk := NewSteppedClock(ctOpen())
	restore := ClockSampleForTest(clk.Sample)
	ObserveClock()
	clk.Advance(time.Second)
	clk.Step(1192 * time.Millisecond)
	if held, why := EntryFeedStale("MNQ", 0, ctOpen()); held {
		restore()
		t.Fatalf("a +1.192 s WSL jitter step must be counted, not held: %q", why)
	}
	if v := ClockStepSnapshot(); v.StepsForward != 1 || v.FreezeSteps != 0 {
		restore()
		t.Fatalf("the 1.2 s step must be counted (fwd 1) and not frozen: %+v", v)
	}
	restore()

	// A step just above the new 6 s freeze threshold (m134): still holds NEW
	// entries for the 120 s window. The 10-21 00:44–04:00 NTP steps (2–5 s)
	// now sit BELOW this and no longer refuse mentor arms.
	clk = NewSteppedClock(ctOpen())
	defer ClockSampleForTest(clk.Sample)()
	ObserveClock()
	clk.Advance(time.Second)
	clk.Step(7000 * time.Millisecond)
	if held, why := EntryFeedStale("MNQ", 0, ctOpen()); !held || !strings.Contains(why, "clock_step") {
		t.Fatalf("a +7 s step must still hold NEW entries, got held=%v why=%q", held, why)
	}
	clk.Advance(119 * time.Second)
	if held, _ := EntryFeedStale("MNQ", 0, ctOpen()); !held {
		t.Fatal("the 7 s step must hold for the full 120 s")
	}
	clk.Advance(2 * time.Second)
	if held, why := EntryFeedStale("MNQ", 0, ctOpen()); held {
		t.Fatalf("the hold must clear after 120 s, got %q", why)
	}
	if cfg := LoadClockStepConfig(); cfg.FreezeMs != 6000 || cfg.HoldS != 120 {
		t.Fatalf("defaults = freeze %dms hold %ds, want 6000 ms / 120 s", cfg.FreezeMs, cfg.HoldS)
	}
}
