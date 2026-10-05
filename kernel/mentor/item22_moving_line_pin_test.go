package mentor

import (
	"testing"

	"vl/market"
)

// ── item 22 FIX NOW (CTO 00:19Z, DS-105 replay) Tick pins ─────────────────
// The bug: a MOVING line (ema34 / ema34_htf / trigger_retest) cleared its
// ISB-only state on DRIFT. An EMA drifts every tick, so the line re-touched
// and re-invalidated every tick (level_invalid 40→446/day, cancel_arm
// 137→541/day, level_visit_cap 293→14,875 refusals — two zero-entry days).
//
// The fix: a moving line resets ONLY when a visit ENDS (price departs the
// touch band) and a NEW visit starts. Never on drift alone. A wrong-way close
// inside the same visit stays invalid, with ONE LevelInvalid + ONE CancelArm.
// ───────────────────────────────────────────────────────────────────────────

// countActions tallies LevelInvalid / CancelArm intents from one tick.
func countActions(out []Intent) (levelInvalid, cancelArm int) {
	for _, in := range out {
		switch in.Action {
		case LevelInvalid:
			levelInvalid++
		case CancelArm:
			cancelArm++
		}
	}
	return
}

// PIN 1 — an EMA drifting for 30 ticks while price stays on the wrong side
// emits EXACTLY one level_invalid (and one cancel_arm). The line re-prices
// every tick (a moving average follows price), but drift alone must never
// re-classify the touch: the wrong-way candle stays the reference candle.
func TestItem22DriftDoesNotReInvalidate(t *testing.T) {
	e := triggerRetestFixture()
	// support line at 100, approached from above.
	bars := []market.Kline{
		rthBars(0, 101, 101.5, 100.9, 101.2),
		rthBars(1, 101, 101.2, 98, 98.5), // touches 100, closes below → wrong way
	}
	li, ca := countActions(e.Tick(bars, bars[1].CloseTime+1))
	// No order rests at the retest line, so the wrong-way close cancels
	// nothing (CTO replay-noise fix): 1 level_invalid, 0 id-less cancels.
	if li != 1 || ca != 0 {
		t.Fatalf("first wrong-way close: level_invalid=%d cancel_arm=%d, want 1 and 0", li, ca)
	}
	if !e.State.ISBOnly[string(KindTriggerRetest)] {
		t.Fatalf("precondition: trigger retest must be ISB-only; state=%v", e.State.ISBOnly)
	}

	// 30 ticks: the line drifts DOWN 2 pts/tick (well past the 0.01 drift
	// threshold the bug keyed on), each candle still touches the line and
	// closes below it — price stays on the wrong side, the visit never ends.
	for i := 0; i < 30; i++ {
		line := 98.0 - 2.0*float64(i) // 98, 96, 94, … below each prev close
		e.State.Trigger.Price = line
		// prev close = previous candle close = (line+2) - 1 = line+1 → above
		// the line (support approach); candle closes line-1 → wrong side.
		bars = append(bars, rthBars(2+i, line+1, line+1.5, line-2, line-1))
		li2, ca2 := countActions(e.Tick(bars, bars[len(bars)-1].CloseTime+1))
		if li2 != 0 || ca2 != 0 {
			t.Fatalf("tick %d: drift re-invalidated (level_invalid=%d cancel_arm=%d), want 0/0 — drift alone must not reset", i, li2, ca2)
		}
	}
	// the invalid state held the whole time.
	if !e.State.ISBOnly[string(KindTriggerRetest)] {
		t.Fatalf("ISB-only must survive drift; state=%v", e.State.ISBOnly)
	}
}

// PIN 2 — depart + return re-arms: a departure clears the invalidity and the
// NEXT wrong-way close emits a NEW level_invalid (one per visit, not one for
// the line's whole life).
func TestItem22DepartAndReturnRearms(t *testing.T) {
	e := triggerRetestFixture()
	bars := []market.Kline{
		rthBars(0, 101, 101.5, 100.9, 101.2),
		rthBars(1, 101, 101.2, 98, 98.5), // wrong way → ISB-only
	}
	li, ca := countActions(e.Tick(bars, bars[1].CloseTime+1))
	// No order rests at the retest line, so the wrong-way close cancels
	// nothing (CTO replay-noise fix): 1 level_invalid, 0 id-less cancels.
	if li != 1 || ca != 0 {
		t.Fatalf("first wrong-way close: level_invalid=%d cancel_arm=%d, want 1 and 0", li, ca)
	}

	// Departure: a candle entirely below the band ends the visit.
	bars = append(bars, rthBars(2, 95, 95.5, 93, 94))
	li2, ca2 := countActions(e.Tick(bars, bars[2].CloseTime+1))
	if li2 != 0 || ca2 != 0 {
		t.Fatalf("departure must not emit: level_invalid=%d cancel_arm=%d", li2, ca2)
	}
	if e.State.ISBOnly[string(KindTriggerRetest)] {
		t.Fatalf("departure must clear ISB-only; state=%v", e.State.ISBOnly)
	}

	// Return: price comes back from below (line is a resistance now) and
	// closes through → a new wrong-way visit → re-armed.
	bars = append(bars, rthBars(3, 94, 101.5, 93.5, 101))
	li3, ca3 := countActions(e.Tick(bars, bars[3].CloseTime+1))
	if li3 != 1 || ca3 != 0 {
		t.Fatalf("return wrong-way close must re-arm: level_invalid=%d cancel_arm=%d, want 1 and 0 (nothing rests)", li3, ca3)
	}
	if !e.State.ISBOnly[string(KindTriggerRetest)] {
		t.Fatalf("return wrong-way close must re-set ISB-only; state=%v", e.State.ISBOnly)
	}
}

// PIN 3 — the visit counter increments ONCE per visit, not per tick. A reject
// classification stays the reference until the visit ends; a drifting line
// must not re-classify reject on every tick (the bug inflated level_visit_cap
// 293→14,875).
func TestItem22VisitCounterOncePerVisit(t *testing.T) {
	e := triggerRetestFixture()
	bars := []market.Kline{
		rthBars(0, 101, 101.5, 100.9, 101.2),
		rthBars(1, 101, 101.2, 100, 100.5), // touches 100, closes back above → reject
	}
	e.Tick(bars, bars[1].CloseTime+1)
	if got := e.State.Visits[string(KindTriggerRetest)]; got != 1 {
		t.Fatalf("precondition: one reject = one visit; visits=%d, want 1", got)
	}

	// 30 ticks of drift: the line re-prices down each tick, price keeps
	// closing back on the approach side (above) — the visit never ends.
	for i := 0; i < 30; i++ {
		line := 99.0 - 1.0*float64(i) // 99, 98, … below each prev close
		e.State.Trigger.Price = line
		// prev close = (line+1) + 0.5 = line+1.5 → above (support); close
		// line+0.5 → back above the line (reject, same visit).
		bars = append(bars, rthBars(2+i, line+1.5, line+2, line-0.5, line+0.5))
		e.Tick(bars, bars[len(bars)-1].CloseTime+1)
	}
	if got := e.State.Visits[string(KindTriggerRetest)]; got != 1 {
		t.Fatalf("visits=%d after 30 drift ticks, want 1 — drift must not re-classify reject", got)
	}
}
