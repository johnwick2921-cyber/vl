package store

import (
	"strings"
	"testing"
)

func TestArmTerminalContract(t *testing.T) {
	check := func(state string, terminal bool) {
		t.Helper()
		if IsTerminalArmState(state) != terminal {
			t.Fatalf("%q terminal: want %v", state, terminal)
		}
	}
	check("filled", true)
	check("cancelled", true)
	check("canceled", true)
	check("rejected", true)
	check("expired", true)
	check("superseded", true)
	check("shadowed", true)
	check("armed", false)
	check("place_pending", false)
	check("working", false)
	check("cancel_pending", false)
	check("unknown-future-state", false)
	check("", false)
}

func TestArmStateSQLAndGoAgreeAtStoreCallSites(t *testing.T) {
	db := newArmedTestDB(t)
	ledger := NewArmedOrderStore(db)
	states := append(ArmStateNames(), "", "future_state")
	for _, raw := range states {
		for _, state := range []string{raw, "\t\n\u00a0" + strings.ToUpper(raw) + "\u2003\r", strings.ReplaceAll(raw, "i", "İ")} {
			row := &ArmedOrderDB{TraderID: "scope", PlanID: "p", Scenario: "fixture", State: state, BootID: "previous"}
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
			var terminal bool
			if err := db.Raw("SELECT "+TerminalArmStateSQL()+" FROM armed_orders WHERE id = ?", row.ID).Scan(&terminal).Error; err != nil {
				t.Fatal(err)
			}
			if terminal != IsTerminalArmState(state) {
				t.Fatalf("SQL/Go disagree for %q", state)
			}
			live, err := ledger.ListNonTerminal("scope")
			if err != nil {
				t.Fatal(err)
			}
			preboot, err := ledger.ListPreBoot("scope", "next")
			if err != nil {
				t.Fatal(err)
			}
			for reader, rows := range [][]ArmedOrderDB{live, preboot} {
				wantPresent := !terminal
				if reader == 1 && strings.ToLower(strings.TrimSpace(state)) == StateCancelPending {
					wantPresent = false // settlement owns this row, never the sweep
				}
				found := false
				for _, r := range rows {
					if r.ID == row.ID {
						found = true
					}
				}
				if found != wantPresent {
					t.Fatalf("store reader %d disagrees with its predicate: row %d state=%q", reader, row.ID, state)
				}
			}
			if err := db.Delete(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	var terminal bool
	if err := db.Raw("SELECT " + TerminalArmStateSQL() + " FROM (SELECT NULL AS state)").Scan(&terminal).Error; err != nil {
		t.Fatal(err)
	}
	if terminal {
		t.Fatal("NULL cannot hide possible exposure")
	}
	rows, err := ledger.ListNonTerminal("different-trader")
	if err != nil || len(rows) != 0 {
		t.Fatalf("trader scope leaked: %v %v", rows, err)
	}
}
