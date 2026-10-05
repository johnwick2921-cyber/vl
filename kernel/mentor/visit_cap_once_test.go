package mentor

import (
	"testing"

	"vl/market"
)

// ── B23 visit-cap refusal ONCE per visit (CTO 01:19Z, DS-105) ──────────────
// The bug: the PHL/PLH reject path re-reads a capped rejected level every tick
// while it sits in TouchReject, so the per-tick e.refuse inflated
// level_visit_cap (12,375 refusals for a handful of capped levels) and hid
// real refusals in the funnel line. The refusal is correct; the counting is
// noise.
//
// The fix: a per-level "refused this visit" mark (State.VisitCapRefused) makes
// the refusal fire once per visit. The touch loop clears the mark on visit
// departure, so a NEW visit re-arms it.
// ───────────────────────────────────────────────────────────────────────────

// TestVisitCapRefusalOncePerVisit — a capped rejected level held in TouchReject
// for 30 consecutive ticks raises level_visit_cap EXACTLY once.
func TestVisitCapRefusalOncePerVisit(t *testing.T) {
	e := triggerRetestFixture()
	bars := []market.Kline{
		rthBars(0, 101, 101.5, 100.9, 101.2), // prev close above 100: support
		rthBars(1, 101, 101.2, 100, 100.5),   // touches 100, closes back above → reject
	}
	e.Tick(bars, bars[1].CloseTime+1)
	key := string(KindTriggerRetest)
	if e.State.Visits[key] != 1 {
		t.Fatalf("precondition: one reject = one visit; visits=%d", e.State.Visits[key])
	}
	// Pre-load the visit count past the cap (LevelMaxVisits default 3) so the
	// level is "capped".
	e.State.Visits[key] = 4

	// 30 ticks: price keeps touching the line and closing back on the approach
	// side (reject, same visit — never a departure). The cap refusal must
	// fire ONCE, not 30 times.
	for i := 0; i < 30; i++ {
		line := 99.0 - 1.0*float64(i)
		e.State.Trigger.Price = line
		bars = append(bars, rthBars(2+i, line+1.5, line+2, line-0.5, line+0.5))
		e.Tick(bars, bars[len(bars)-1].CloseTime+1)
	}
	if got := e.State.Refusals["level_visit_cap"]; got != 1 {
		t.Fatalf("level_visit_cap=%d after 30 capped reject ticks, want 1 (once per visit)", got)
	}
}

// TestVisitCapRefusalRearmsAfterDeparture — a departure clears the mark and the
// NEXT capped visit raises level_visit_cap again (once more), so the counter is
// per visit, not per day.
func TestVisitCapRefusalRearmsAfterDeparture(t *testing.T) {
	e := triggerRetestFixture()
	bars := []market.Kline{
		rthBars(0, 101, 101.5, 100.9, 101.2),
		rthBars(1, 101, 101.2, 100, 100.5), // reject → visit 1
	}
	e.Tick(bars, bars[1].CloseTime+1)
	key := string(KindTriggerRetest)
	e.State.Visits[key] = 4 // capped

	// One capped tick → the refusal fires once.
	bars = append(bars, rthBars(2, 100.5, 101, 99.5, 100.5))
	e.Tick(bars, bars[2].CloseTime+1)
	if got := e.State.Refusals["level_visit_cap"]; got != 1 {
		t.Fatalf("first capped visit: level_visit_cap=%d, want 1", got)
	}

	// Departure: a candle entirely below the band ends the visit.
	bars = append(bars, rthBars(3, 95, 95.5, 93, 94))
	e.Tick(bars, bars[3].CloseTime+1)

	// Return from below (resistance now) and reject again → a new visit, still
	// capped → the refusal fires once more.
	bars = append(bars, rthBars(4, 94, 100.5, 93.5, 99.5))
	e.Tick(bars, bars[4].CloseTime+1)
	if got := e.State.Refusals["level_visit_cap"]; got != 2 {
		t.Fatalf("second capped visit: level_visit_cap=%d, want 2 (once per visit)", got)
	}
}
