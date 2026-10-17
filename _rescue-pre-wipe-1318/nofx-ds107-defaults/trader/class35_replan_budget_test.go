package trader

import (
	"testing"
	"time"

	"nofx/store"
)

// CLASS 35 (2026-09-01) — counters RECORD events; they do not infer them.
//
// These fixtures drive the two real spending paths (death re-plan, owner
// re-read) and the free ones (scheduled read, level_event / structure_mss
// wakes, owner_reset) through the SAME read core production uses, then read
// the budget back the way the gate, the card and the executor prompt do.

func class35Trader(t *testing.T, cap int) (*AutoTrader, *store.Store) {
	t.Helper()
	// The canned plan predates the confirm{} contract; its grace window (3
	// reads) is not what these fixtures test — keep it open for the chain.
	t.Setenv("CONFIRM_GRACE_SESSIONS", "100")
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: store.IntPtr(cap)}})
	at.mcpClient = &planClient{} // schema-valid plan → every read lands an ACTIVE row
	return at, st
}

func seedV1(t *testing.T, st *store.Store, tradeDate, session string) {
	t.Helper()
	if _, err := st.Plan().AppendPlan(&store.PlanDB{
		PlanID: store.MakePlanID(tradeDate, session), StrategyID: "trader-1",
		TradeDate: tradeDate, Session: session, TriggerReason: session + "_scheduled_read", Lifecycle: "active", Doc: "{}",
	}); err != nil {
		t.Fatal(err)
	}
}

func latestRow(t *testing.T, st *store.Store, tradeDate, session string) *store.PlanDB {
	t.Helper()
	row, err := st.Plan().GetLatestPlanForTraderSession(tradeDate, session, "trader-1")
	if err != nil || row == nil {
		t.Fatalf("latest row: %v %+v", err, row)
	}
	return row
}

// A death re-plan DOES decrement; four deaths exhaust cap 4; the fifth
// fail-closes into the NO-TRADE marker without spending.
func TestClass35DeathReplanSpendsAndFifthFailsClosed(t *testing.T) {
	at, st := class35Trader(t, 4)
	const tradeDate, sess = "2026-09-01", "NY"
	seedV1(t, st, tradeDate, sess)
	killer := "all 3 levels touched and accepted through (last: PDH 29200.00)"
	for i := 1; i <= 5; i++ {
		latest := latestRow(t, st, tradeDate, sess)
		budget := store.GetReplanBudget(st, "trader-1", tradeDate, sess, 4)
		allowed := at.deathReplanAllowed(sess, tradeDate, latest, killer, budget)
		if i <= 4 {
			if !allowed {
				t.Fatalf("death %d must be allowed, budget %+v", i, budget)
			}
			at.runDeathReplan(sess, tradeDate, latest, killer)
			fresh := latestRow(t, st, tradeDate, sess)
			if fresh.Version != latest.Version+1 || fresh.Lifecycle != "active" {
				t.Fatalf("death %d: expected a fresh ACTIVE v%d, got %+v", i, latest.Version+1, fresh)
			}
			if fresh.TriggerReason != store.TriggerDeathReplan {
				t.Fatalf("death %d: the re-plan row must be labelled %q (it used to land as %s_scheduled_read), got %q", i, store.TriggerDeathReplan, sess, fresh.TriggerReason)
			}
			if b := store.GetReplanBudget(st, "trader-1", tradeDate, sess, 4); b.Used != i || b.Left() != 4-i {
				t.Fatalf("after death %d: budget %+v", i, b)
			}
			continue
		}
		if allowed {
			t.Fatalf("the fifth death must fail-closed, budget %+v", budget)
		}
		fresh := latestRow(t, st, tradeDate, sess)
		if fresh.Lifecycle != "no_trade" || fresh.TriggerReason != "replans_exhausted" {
			t.Fatalf("fifth death must write the NO-TRADE marker, got %+v", fresh)
		}
		if b := store.GetReplanBudget(st, "trader-1", tradeDate, sess, 4); b.Used != 4 {
			t.Fatalf("the marker must not spend: %+v", b)
		}
	}
}

// An owner re-read DOES decrement.
func TestClass35OwnerRereadSpends(t *testing.T) {
	at, st := class35Trader(t, 4)
	const tradeDate, sess = "2026-09-01", "NY"
	seedV1(t, st, tradeDate, sess)
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, chicagoLoc()) // NY window, CME open
	gate, err := at.ForceReread(now)
	if err != nil || !gate.Allowed {
		t.Fatalf("ForceReread: %v %+v", err, gate)
	}
	if gate.ReplansLeft != 4 {
		t.Fatalf("the gate must have seen the full budget before spending, got %+v", gate)
	}
	fresh := latestRow(t, st, tradeDate, sess)
	if fresh.Version != 2 || fresh.TriggerReason != store.TriggerOwnerReread {
		t.Fatalf("expected v2 owner_reread, got %+v", fresh)
	}
	if b := store.GetReplanBudget(st, "trader-1", tradeDate, sess, 4); b.Used != 1 || b.Left() != 3 {
		t.Fatalf("one re-read must spend one: %+v", b)
	}
	// The gate reports the recorded number on the next ask.
	if g := at.CanForceReread(now); g.ReplansLeft != 3 || !g.Allowed {
		t.Fatalf("CanForceReread after one spend: %+v", g)
	}
}

// level_event, structure_mss, the session's scheduled read and owner_reset do
// NOT decrement (fast-market is a reasoning MODE of the wake reads — same
// trigger class, same answer).
func TestClass35FreeReadsDoNotSpend(t *testing.T) {
	at, st := class35Trader(t, 4)
	const tradeDate, sess = "2026-09-01", "NY"
	seedV1(t, st, tradeDate, sess)
	for _, tc := range []struct {
		trigger    string
		failClosed bool
		wantLabel  string
	}{
		{"", true, sess + "_scheduled_read"},
		{"level_event", false, "level_event"},
		{"structure_mss", false, "structure_mss"},
		{"owner_reset", true, "owner_reset"},
	} {
		before := latestRow(t, st, tradeDate, sess)
		if !at.runPlannerReadWithTriggerClaimedCtx(sess, tradeDate, tc.trigger, "", nil, tc.failClosed) {
			t.Fatalf("%q: read did not run", tc.trigger)
		}
		fresh := latestRow(t, st, tradeDate, sess)
		if fresh.Version != before.Version+1 || fresh.TriggerReason != tc.wantLabel {
			t.Fatalf("%q: expected v%d %s, got %+v", tc.trigger, before.Version+1, tc.wantLabel, fresh)
		}
		if b := store.GetReplanBudget(st, "trader-1", tradeDate, sess, 4); b.Used != 0 || b.Left() != 4 {
			t.Fatalf("%q must be free, got %+v", tc.trigger, b)
		}
	}
	if store.TriggerSpendsReplan("level_event") || store.TriggerSpendsReplan("structure_mss") {
		t.Fatal("fast-market reads ride level_event/structure_mss — those classes must be free")
	}
}

// A death re-plan whose read is REFUSED (no planner client → nothing written)
// spends nothing: "no plan row, no budget consumed" still holds.
func TestClass35RefusedDeathReplanDoesNotSpend(t *testing.T) {
	at, st := class35Trader(t, 4)
	at.mcpClient = nil
	const tradeDate, sess = "2026-09-01", "NY"
	seedV1(t, st, tradeDate, sess)
	latest := latestRow(t, st, tradeDate, sess)
	at.runDeathReplan(sess, tradeDate, latest, "all levels consumed")
	if fresh := latestRow(t, st, tradeDate, sess); fresh.Version != 1 {
		t.Fatalf("a refused read must write nothing, got %+v", fresh)
	}
	if b := store.GetReplanBudget(st, "trader-1", tradeDate, sess, 4); b.Used != 0 {
		t.Fatalf("a refused read must not spend: %+v", b)
	}
}
