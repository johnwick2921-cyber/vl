package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
	"vl/telemetry"
)

// TestMentorEvalOnceCatchesUpMissedClosedBars — MENTOR-NY item 7 (rescan
// P1-N1) at the production call site. 10-14 08:32–08:52: the event path missed
// FINALs while the bar ingest was saturated, and the next tick evaluated only
// the newest bar — the funnel fell 6 bars behind the clock and the skipped bars
// were never an ISB/touch/ORB candle. Here the ORB escape candle (08:32, the
// only 1m body wholly outside the 08:30–08:31 box) is one of three bars the
// event path missed; the next tick is the 08:35 bar (inside the box). Without
// the catch-up the escape is never read. With it, all three missed bars are
// ticked in order (counted), the escape is read on 08:32, and the funnel counts
// 4 bars for that tick.
// MUTANT: mentorCatchUp returns nothing → the escape is never read → RED.
func TestMentorEvalOnceCatchesUpMissedClosedBars(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	old := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = old })
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline { return nil }

	loc := kernel.CTLocation()
	day := time.Date(2026, time.October, 1, 0, 0, 0, 0, loc).UnixMilli()
	bar := func(h, m int, o, hi, lo, c float64) market.Kline {
		ot := day + int64(h*60+m)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: hi, Low: lo, Close: c, Final: true}
	}
	var bars []market.Kline
	for m := 6*60 + 30; m < 8*60+30; m++ {
		bars = append(bars, bar(m/60, m%60, 100, 101, 99, 100))
	}
	bars = append(bars,
		bar(8, 30, 100, 101, 99, 100),
		bar(8, 31, 100, 101, 99.5, 100),
		bar(8, 32, 101.5, 105.5, 101.25, 105), // the ORB escape candle (body wholly above the box)
		bar(8, 33, 100.5, 101, 99.5, 100.2),   // back inside
		bar(8, 34, 100.2, 100.9, 99.6, 100.4), // inside
		bar(8, 35, 100.4, 100.8, 99.7, 100.1), // inside — the next tick the event path delivers
	)
	i0831 := len(bars) - 5 // index of the 08:31 bar
	for i := 2; i <= i0831+1; i++ {
		at.mentorEvalOnce(bars[:i])
	}
	if at.mentorEval.State.ORB.Escaped != "" {
		t.Fatalf("precondition: no escape before 08:32, got %q", at.mentorEval.State.ORB.Escaped)
	}
	at.mentorFunnel.mu.Lock()
	barsBefore := at.mentorFunnel.bars
	at.mentorFunnel.mu.Unlock()

	// The event path missed 08:32, 08:33 and 08:34; the next tick is 08:35.
	if !at.mentorEvalOnce(bars) {
		t.Fatal("the 08:35 tick must run")
	}
	if got := at.mentorEval.State.ORB.Escaped; got != mentor.SideLong {
		t.Fatalf("the missed 08:32 escape candle was never evaluated: ORB escape %q, want long", got)
	}
	if got := at.mentorCountSnapshot()["mentor_bars_caught_up"]; got != 3 {
		t.Fatalf("mentor_bars_caught_up = %d, want 3 (08:32, 08:33, 08:34)", got)
	}
	at.mentorFunnel.mu.Lock()
	got := at.mentorFunnel.bars - barsBefore
	at.mentorFunnel.mu.Unlock()
	if got != 4 {
		t.Fatalf("the funnel must count the 3 caught-up bars + the newest = 4, got %d", got)
	}
	// A repeat of the same newest bar is still a no-op (the watermark holds).
	if at.mentorEvalOnce(bars) {
		t.Fatal("a second tick of the same newest bar must be a no-op")
	}
}

// TestMentorCatchUpEntriesAreRefusedAsStale — MENTOR-NY item 7: an ENTRY a
// caught-up bar emits rides the same dispatch with the NEWEST bar's close, so
// N10 refuses it (stale RefBarMs) — a missed bar never becomes a late order.
// The intent is injected at mentorPlaceIntent with the caught-up bar's close as
// its RefBarMs and the newest bar's close as barCloseMs, exactly as
// mentorEvalOnce dispatches a caught-up intent.
func TestMentorCatchUpEntriesAreRefusedAsStale(t *testing.T) {
	telemetry.ResetSetupStallsForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	var placed []mentor.Intent
	mentorPlaceRecorderForTest = func(in mentor.Intent, _ int) { placed = append(placed, in) }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })
	missedClose := int64(1_790_000_000_000 + 59_999)
	newestClose := missedClose + 3*60_000
	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, Setup: "ISB", Side: mentor.SideLong, Price: 100, Stop: 95, Target: 110, RefBarMs: missedClose, ArmID: "isb-1"}
	at.mentorPlaceIntent(in, mentorSizeChoice{Contracts: 1}, newestClose, newestClose)
	if len(placed) != 0 {
		t.Fatalf("a caught-up bar's entry must never place: %+v", placed)
	}
	if got := at.mentorCountSnapshot()["stale_intent"]; got != 1 {
		t.Fatalf("the caught-up entry must be refused as stale_intent (N10), got %d", got)
	}
	if got := stallLastRefusal(at.id, "isb"); !strings.Contains(got, "stale_intent") {
		t.Fatalf("the stall alarm must record stale_intent as the last refusal (item 9), got %q", got)
	}
}

// stallLastRefusal reads the stall alarm's recorded last refusal for a kind.
func stallLastRefusal(traderID, kind string) string {
	_, tbl := telemetry.SetupStallSnapshot()
	return tbl[traderID][kind].LastRefusal
}
