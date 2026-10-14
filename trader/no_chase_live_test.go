package trader

import (
	"strings"
	"testing"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// R22 P2 (MATH-PLANNER §4) — no-chase on the ARM path measured |authored entry
// − authored confirm ref|: a constant of the plan, 0.00×ATR for every
// reject-at-level arm (v5 S1: 30815 vs 30815), so it could never see a chase
// (9,069 nochase_arm_ok vs 18 would_refuse). Production call site:
// entryGateForArm (the arm pass's gate). Price has run 60 pts past the cited
// level: the live measurement must fire; with no live price it must abstain.
func TestNoChaseArmMeasuresFromTheLivePrice(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	plan, sc := e12ArmFixture()
	sc.Condition = "reject"
	sc.Confirm = &kernel.PlanConfirm{Rule: "touch", RefPrice: 100, Side: "below"}
	leg := kernel.PlanArmLeg{Entry: 100, Stop: 60, Target: 230} // R:R 3.25, stop 40 ≥ 1.5×ATR 20
	old := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = old })
	market.FuturesBarsProvider = func(string, string, int) []market.Kline {
		return []market.Kline{{Close: 158}, {Close: 160}} // live price 60 pts past the level
	}
	logBuf := captureTraderLog(t)
	if reason, refused := at.entryGateForArm(plan, sc, leg, "long", "long", 20, e12PassClock); refused {
		t.Fatalf("fixture must pass every refusing leg (no-chase is WARN-first): %s", reason)
	}
	if n, _ := store.SystemCounter(st, NoChaseCounterKey("arm", "dist_fired")); n != 1 {
		t.Fatalf("live price 60 pts (3.00×ATR) from the cited level must fire the dist half: dist_fired=%d\nlog:\n%s", n, logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "dist=60.0pts/3.00×ATR from live price") {
		t.Fatalf("the WARN must say the distance was taken from the live price:\n%s", logBuf.String())
	}
	// No live price → nothing measured, counted apart, never "ok".
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return nil }
	at.entryGateForArm(plan, sc, leg, "long", "long", 20, e12PassClock)
	if n, _ := store.SystemCounter(st, NoChaseCounterKey("arm", "no_live_price")); n != 1 {
		t.Fatalf("no live price must be counted no_live_price: %d", n)
	}
	if n, _ := store.SystemCounter(st, NoChaseCounterKey("arm", "ok")); n != 0 {
		t.Fatalf("an unmeasured pass is never counted ok: %d", n)
	}
}

// The DECISION path is unchanged: it still measures from its entry.
func TestNoChaseDecisionPathStillMeasuresFromEntry(t *testing.T) {
	v := EvaluateNoChase(NoChaseInputs{Entry: 160, CitedLevel: 100, ATR5m: 20, MinSLMult: 1.5, LivePx: 100})
	if !v.Applicable || v.DistPts != 60 || v.MeasuredFrom != "entry" {
		t.Fatalf("decision path (LiveRequired false) measures from the entry: %+v", v)
	}
}
