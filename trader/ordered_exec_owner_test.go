package trader

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// REL18-C O2 — a second LIVE owner for the same (symbol, account) makes the
// start FAIL: installNTOrderedExecutions returns the owner conflict, marks this
// trader not-running, and raises a P0 naming both traders. It must NEVER fall
// back to the legacy advisory path (two traders on one account double-apply
// fills).
func TestOrderedOwnerConflictFailsStart(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "o2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// The DB says this trader was running; the refusal must flip it back.
	if err := st.Trader().Create(&store.Trader{
		ID: "t2", UserID: "u", Name: "trader-2", AIModelID: "m", ExchangeID: "ex",
		StrategyID: "s", Account: "Sim101", IsRunning: true,
	}); err != nil {
		t.Fatal(err)
	}

	srv := ntwire.NewTCPServer(nil)
	// A live owner already holds (MNQ, Sim101).
	if _, err := srv.RegisterOrderedExecutionsFor("MNQ", "Sim101", ntwire.OrderedExecutionHandlers{TraderID: "t1"}); err != nil {
		t.Fatalf("pre-register owner: %v", err)
	}

	nt := ntTrader.NewTCPTrader(srv, "MNQ", "Sim101")
	at := &AutoTrader{
		id: "t2", name: "trader-2", exchange: "ninjatrader", exchangeID: "ex",
		trader: nt, store: st, userID: "u",
		config: AutoTraderConfig{
			NinjaTraderSymbol: "MNQ",
			StrategyConfig:    &store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}},
		},
	}

	err = at.installNTOrderedExecutions(nt)
	if err == nil || !errors.Is(err, ntwire.ErrOrderedOwnerExists) {
		t.Fatalf("install must fail with ErrOrderedOwnerExists, got %v", err)
	}

	// Marked not-running in the DB.
	row, _ := st.Trader().GetByID("t2")
	if row == nil || row.IsRunning {
		t.Fatalf("t2 must be marked not-running after the refused install, got %+v", row)
	}

	// The P0 names both traders.
	alerts, _ := st.Alert().List("t2", 10)
	found := false
	for _, a := range alerts {
		if a.Kind == "ordered_owner_conflict" {
			found = true
			if !strings.Contains(a.Body, "t2") || !strings.Contains(a.Body, "t1") {
				t.Fatalf("P0 must name both traders, got: %s", a.Body)
			}
		}
	}
	if !found {
		t.Fatalf("no ordered_owner_conflict P0 alert written; alerts=%d", len(alerts))
	}
}
