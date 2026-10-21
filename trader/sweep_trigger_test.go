package trader

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// R22 F1 (owner R4: sweep setups wait for their trigger) at the PRODUCTION
// call site maybeManageArmedOrdersAt — the pass that authorizes the armed row
// the placement loop sends to NT8. A sweep setup must have NO authorized row
// before its sweep + reclaim close exist.
//
// sweepTape: price sits ABOVE the long reclaim ref 100 from birth (closes 101,
// lows 100.5) — the #649 mirror: every 5m close is on the reclaimed side, and
// the ref is never traded.
func sweepReclaimTrader(t *testing.T, waitConfirm bool) (*AutoTrader, *store.Store, *[]market.Kline, time.Time) {
	t.Helper()
	at, st, plan, tape, base := confirmationTrader(t)
	sc := &plan.Doc.Scenarios[0]
	sc.Condition, sc.Fvg = "reclaim", nil
	sc.Trigger = "sweep of 100.00 PDH then a 5m close back above reclaims it"
	sc.Confirm = &kernel.PlanConfirm{Rule: "1x5m_close", RefPrice: 100, Side: "above"}
	sc.Arm.WaitConfirm = waitConfirm
	for i := range *tape {
		(*tape)[i].Low = 100.5
	}
	return at, st, tape, base
}

func TestSweepSetupNoArmBeforeSweep649Shape(t *testing.T) {
	at, st, tape, base := sweepReclaimTrader(t, true) // #649 authored wait_confirm=true
	logBuf := captureTraderLog(t)
	at.maybeManageArmedOrdersAt(nil, base.Add(5*time.Minute))
	rows, err := st.ArmedOrders().ListNonTerminal(at.id)
	if err != nil || len(rows) != 0 {
		t.Fatalf("a reclaim whose ref was never swept must authorize NO arm (live #649 armed here): rows=%d err=%v", len(rows), err)
	}
	if !strings.Contains(logBuf.String(), "HELD — a sweep setup waits for its trigger") {
		t.Fatalf("the hold must be logged:\n%s", logBuf.String())
	}
	// 10:05 the sweep (low 99.75 through 100), 10:05–10:10 closes back above.
	for i := 0; i < 5; i++ {
		o := base.Add(time.Duration(5+i) * time.Minute).UnixMilli()
		lo := 100.5
		if i == 0 {
			lo = 99.75
		}
		*tape = append(*tape, market.Kline{OpenTime: o, CloseTime: o + 59999, Open: 101, High: 102, Low: lo, Close: 101})
	}
	at.maybeManageArmedOrdersAt(nil, base.Add(10*time.Minute))
	rows, err = st.ArmedOrders().ListNonTerminal(at.id)
	if err != nil || len(rows) != 1 {
		t.Fatalf("after the sweep + a 5m close back above, the same call site arms: rows=%d err=%v\nlog:\n%s", len(rows), err, logBuf.String())
	}
}

// A sweep setup the plan authored WITHOUT wait_confirm is held all the same —
// before R22 the gate never looked at its confirm and the order rested at once.
func TestSweepSetupHeldWithoutAuthoredWaitConfirm(t *testing.T) {
	at, st, _, base := sweepReclaimTrader(t, false)
	logBuf := captureTraderLog(t)
	at.maybeManageArmedOrdersAt(nil, base.Add(5*time.Minute))
	rows, err := st.ArmedOrders().ListNonTerminal(at.id)
	if err != nil || len(rows) != 0 {
		t.Fatalf("a sweep setup without wait_confirm must still wait for its trigger: rows=%d err=%v", len(rows), err)
	}
	if !strings.Contains(logBuf.String(), "HELD — a sweep setup waits for its trigger") {
		t.Fatalf("the hold must be logged:\n%s", logBuf.String())
	}
	raw, _ := st.GetSystemConfig("sweep_waits_for_trigger:" + at.id + ":2026-09-08:NY")
	if strings.TrimSpace(raw) != "1" {
		t.Fatalf("the hold is counted once per scenario-version: counter=%q", raw)
	}
	at.maybeManageArmedOrdersAt(nil, base.Add(5*time.Minute+30*time.Second))
	if raw, _ := st.GetSystemConfig("sweep_waits_for_trigger:" + at.id + ":2026-09-08:NY"); strings.TrimSpace(raw) != "1" {
		t.Fatalf("a repeat hold of the same leg is not re-counted: counter=%q", raw)
	}
}

// R22 F8 — the adherence grade at its PRODUCTION call site
// (recordClosedTradeAnalyticsAt): a filled sweep setup whose sweep never
// happened before the entry must not grade "A (followed the plan)" (#649 did).
func gradeSweepFill(t *testing.T, sweepFirst bool) (got, withoutTrigger string, log string) {
	t.Helper()
	at, st, tape, base := sweepReclaimTrader(t, true)
	logBuf := captureTraderLog(t)
	// Coverage from plan birth (base−1m) through the entry; lows 100.5 never
	// trade the ref 100 unless sweepFirst puts a 99.75 low at 10:05.
	pre := base.Add(-time.Minute).UnixMilli()
	*tape = append([]market.Kline{{OpenTime: pre, CloseTime: pre + 59999, Open: 101, High: 102, Low: 100.5, Close: 101}}, (*tape)...)
	for i := 5; i < 12; i++ {
		o := base.Add(time.Duration(i) * time.Minute).UnixMilli()
		lo := 100.5
		if sweepFirst && i == 5 {
			lo = 99.75
		}
		*tape = append(*tape, market.Kline{OpenTime: o, CloseTime: o + 59999, Open: 101, High: 102, Low: lo, Close: 101})
	}
	plan := kernel.ActivePlanFor(at.id, "MNQ")
	// The grader reads the STORED row (as the live close path does): persist
	// the reclaim doc as its own version, born at the same instant.
	blob, _ := json.Marshal(plan.Doc)
	ver, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: plan.PlanID, TradeDate: "2026-09-08", Session: "NY", StrategyID: at.id, Lifecycle: "active", Doc: string(blob), CreatedAt: base.Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	entry := base.Add(11 * time.Minute)
	pos := &store.TraderPosition{TraderID: at.id, Symbol: "MNQ", Side: "LONG", EntryPrice: 101, EntryQuantity: 1,
		EntryTime: entry.UnixMilli(), Source: "armed_entry",
		PlanID: plan.PlanID, PlanVersion: ver, CitedScenarioID: "S1", PlanMatched: true}
	if err := st.Position().Create(pos); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Position().ClosePosition(pos.ID, 99, "", -4, 0, "sl"); err != nil {
		t.Fatal(err)
	}
	byID := func() *store.TraderPosition {
		rows, err := st.Position().GetClosedPositions(at.id, 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.ID == pos.ID {
				return r
			}
		}
		t.Fatalf("closed position %d not found", pos.ID)
		return nil
	}
	at.recordClosedTradeAnalyticsAt(entry.Add(5*time.Minute), byID())
	again := byID()
	inKZ, inNoTrade := kernel.SessionWindowFacts(at.sessionRegistry(entry), entry)
	withoutTrigger, _ = kernel.GradeAdherence(kernel.AdherenceInput{Cited: true, Matched: true, InNoTrade: inNoTrade, InKillzone: inKZ})
	return again.AdherenceGrade, withoutTrigger, logBuf.String()
}

func TestAdherenceGradeSweepNeverHappenedIsNotFollowedPlan(t *testing.T) {
	got, legacy, log := gradeSweepFill(t, false)
	if got == "" || got == legacy {
		t.Fatalf("an entry before its sweep must grade BELOW the trigger-blind grade %q, got %q\nlog:\n%s", legacy, got, log)
	}
	if got == "A" || !strings.Contains(log, "entered BEFORE its cited scenario's trigger") || !strings.Contains(log, "trigger=missed") {
		t.Fatalf("grade %q must not be A and the miss must be logged:\n%s", got, log)
	}
}

func TestAdherenceGradeSweepThenEntryKeepsItsGrade(t *testing.T) {
	got, legacy, log := gradeSweepFill(t, true)
	if got != legacy || !strings.Contains(log, "trigger=met") {
		t.Fatalf("a sweep + reclaim close before the entry keeps the trigger-blind grade %q, got %q\nlog:\n%s", legacy, got, log)
	}
}
