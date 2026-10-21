package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
)

// FIX-READ-FACTS-PLAN-ID (DS-103) at the PRODUCTION write site
// (runPlannerReadCoreObserved → AppendPlan → BindPlanToReadFact).
//
// A planner read writes its facts row BEFORE the AI call (persistReadFacts) and
// returns that row's id; the plan row lands after and is bound BY THAT ID. These
// tests drive the write site with the id a read would have returned.

// TestReadFactsBoundToPlanAtWriteSite — a read with a facts row id binds that
// exact row to the plan it wrote. RED: drop the BindPlanToReadFact call (or the
// readFactID threading) and the facts row stays plan_id="" / version=0.
func TestReadFactsBoundToPlanAtWriteSite(t *testing.T) {
	at := plannerTestTrader(t) // id "t1"
	const date, session = "2026-10-08", "NY"

	// Simulate the read's facts row (what persistReadFacts writes) and capture
	// the id its insert returned.
	row := &store.PlannerReadFact{TraderID: at.id, TradeDate: date, Session: session, PromptHash: "aihash", VoidLevels: "[]"}
	if err := at.store.PlannerReadFacts().SaveReadFact(row); err != nil {
		t.Fatalf("seed read-facts row: %v", err)
	}
	if row.ID == 0 {
		t.Fatalf("seed row got id 0 — insert did not return an id")
	}

	now := time.Now()
	ver, lc, err := at.runPlannerReadCoreObserved(
		func() time.Time { return now }, func() time.Time { return now },
		nil, session, date, "", "model", "hash", "", "aihash", "", "FULLPROMPT",
		kernel.PlanFacts{ReadAt: now}, nil, nil, nil, true, row.ID,
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

// TestPlanWriteWithoutReadFactLeavesFailedReadUnbound — a plan written by a path
// that has NO facts id (readFactID == 0) must bind NOTHING, even when an older
// failed read's facts row is the newest unbound row for the session.
//
// RED (the CTO's fold): the old "newest unbound row for (trader,date,session)"
// heuristic WOULD bind the failed read's row here. The exact-id bind leaves it
// empty, which is correct — a failed read did not produce this plan.
func TestPlanWriteWithoutReadFactLeavesFailedReadUnbound(t *testing.T) {
	at := plannerTestTrader(t) // id "t1"
	const date, session = "2026-10-08", "NY"

	// A FAILED read: its facts row exists, but no plan was written for it.
	failed := &store.PlannerReadFact{TraderID: at.id, TradeDate: date, Session: session, PromptHash: "failed", VoidLevels: "[]"}
	if err := at.store.PlannerReadFacts().SaveReadFact(failed); err != nil {
		t.Fatalf("seed failed read: %v", err)
	}
	failedID := failed.ID

	// A plan lands from a path with NO facts id of its own (readFactID == 0).
	now := time.Now()
	ver, lc, err := at.runPlannerReadCoreObserved(
		func() time.Time { return now }, func() time.Time { return now },
		nil, session, date, "", "model", "hash", "", "other-config", "", "FULLPROMPT",
		kernel.PlanFacts{ReadAt: now}, nil, nil, nil, true, 0,
		func(string) (string, error) { return validTraderPlanJSON, nil })
	if err != nil || lc != "active" || ver != 1 {
		t.Fatalf("write failed: ver=%d lc=%q err=%v", ver, lc, err)
	}

	// The failed read's row must still be empty (it is the only read-facts row).
	got, err := at.store.PlannerReadFacts().LatestReadFact()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.ID != failedID {
		t.Fatalf("unexpected latest row id %d, want %d", got.ID, failedID)
	}
	if got.PlanID != "" || got.Version != 0 {
		t.Fatalf("failed read's row bound to a plan it did not produce: %q v%d (RED — the newest-row heuristic bound it)", got.PlanID, got.Version)
	}
}
