package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
)

// FIX-READ-FACTS-PLAN-ID (DS-103) at the PRODUCTION write site
// (runPlannerReadCoreObserved → AppendPlan → BindPlanToLatestReadFact).
//
// A planner read writes its facts row BEFORE the AI call (persistReadFacts);
// the plan row lands after. This test seeds the facts row exactly as a read
// would, drives the write site, and asserts the facts row ends up carrying the
// written plan's plan_id + version.
//
// RED: remove the BindPlanToLatestReadFact call in runPlannerReadCoreObserved
// and this test fails — the facts row stays plan_id="" / version=0 while the
// plan row is written, i.e. the exact unbound-provenance defect this fixes.
func TestReadFactsBoundToPlanAtWriteSite(t *testing.T) {
	at := plannerTestTrader(t) // id "t1"
	const date, session = "2026-10-08", "NY"

	// Simulate the read's facts row (what persistReadFacts writes): bound to
	// nothing yet.
	if err := at.store.PlannerReadFacts().SaveReadFact(&store.PlannerReadFact{
		TraderID: at.id, TradeDate: date, Session: session, PromptHash: "aihash", VoidLevels: "[]",
	}); err != nil {
		t.Fatalf("seed read-facts row: %v", err)
	}
	before, err := at.store.PlannerReadFacts().LatestReadFact()
	if err != nil {
		t.Fatalf("read back seeded row: %v", err)
	}
	if before.PlanID != "" || before.Version != 0 {
		t.Fatalf("precondition broken: seeded row already bound (%q v%d)", before.PlanID, before.Version)
	}

	now := time.Now()
	ver, lc, err := at.runPlannerReadCoreObserved(
		func() time.Time { return now }, func() time.Time { return now },
		nil, session, date, "", "model", "hash", "", "aihash", "", "FULLPROMPT",
		kernel.PlanFacts{ReadAt: now}, nil, nil, nil, true,
		func(string) (string, error) { return validTraderPlanJSON, nil })
	if err != nil || lc != "active" || ver != 1 {
		t.Fatalf("write failed: ver=%d lc=%q err=%v", ver, lc, err)
	}

	plan, err := at.store.Plan().GetLatestPlanForSession(date, session)
	if err != nil || plan == nil {
		t.Fatalf("written plan missing: %v", err)
	}

	after, err := at.store.PlannerReadFacts().LatestReadFact()
	if err != nil {
		t.Fatalf("read back bound row: %v", err)
	}
	if after.PlanID != plan.PlanID || after.Version != plan.Version {
		t.Fatalf("facts row not bound to the plan it produced: facts=%q v%d, plan=%q v%d",
			after.PlanID, after.Version, plan.PlanID, plan.Version)
	}
	if after.PlanID == "" {
		t.Fatalf("facts row plan_id still empty after write — the bind never ran (RED)")
	}
}
