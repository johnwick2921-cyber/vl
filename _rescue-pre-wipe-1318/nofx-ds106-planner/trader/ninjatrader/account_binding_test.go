package ninjatrader

import (
	"encoding/json"
	"strings"
	"testing"

	ntwire "nofx/provider/ninjatrader"
)

// TestSignalAccountWire locks the P5.4 wire rule: an UNBOUND trader's signal
// JSON carries NO "account" key (byte-identical legacy wire → the AddOn uses
// its active account); a BOUND trader's signal carries it (activating the
// AddOn's Phase-3 per-order routing).
func TestSignalAccountWire(t *testing.T) {
	legacy, _ := json.Marshal(ntwire.SignalPayload{Symbol: "MNQ", Side: "long", SignalID: "x"})
	if strings.Contains(string(legacy), "account") {
		t.Fatalf("unbound signal must omit the account key (legacy wire); got %s", legacy)
	}
	bound, _ := json.Marshal(ntwire.SignalPayload{Symbol: "ES", Account: "SimAccount1", Side: "long", SignalID: "y"})
	if !strings.Contains(string(bound), `"account":"SimAccount1"`) {
		t.Fatalf("bound signal must carry the account; got %s", bound)
	}
}

// TestNewTCPTrader_AccountBinding locks the constructor binding: no account →
// unbound (legacy); the variadic account binds (trimmed).
func TestNewTCPTrader_AccountBinding(t *testing.T) {
	s := ntwire.NewTCPServer(nil)
	if tr := NewTCPTrader(s, "MNQ"); tr.boundAccount != "" {
		t.Fatalf("no-account constructor must stay unbound; got %q", tr.boundAccount)
	}
	if tr := NewTCPTrader(s, "ES", " SimAccount1 "); tr.boundAccount != "SimAccount1" {
		t.Fatalf("bound constructor must trim+bind; got %q", tr.boundAccount)
	}
}

// TestPlaceEntry_UnboundRefuses locks the multi-account safety rail: an UNBOUND
// trader (empty account) must REFUSE to place an entry — it must NOT fall back to
// the shared active account (which could be another trader's account). A BOUND
// trader passes the empty-account gate (and only then fails on a different gate),
// proving the non-empty path is unchanged.
func TestPlaceEntry_UnboundRefuses(t *testing.T) {
	s := ntwire.NewTCPServer(nil)

	// UNBOUND → hard refuse, before any signal frame is built.
	unbound := NewTCPTrader(s, "MNQ")
	if _, err := unbound.OpenLong("MNQ", 1, 1); err == nil || !strings.Contains(err.Error(), "no bound account") {
		t.Fatalf("unbound OpenLong must refuse with 'no bound account'; got err=%v", err)
	}
	if _, err := unbound.OpenShort("MNQ", 1, 1); err == nil || !strings.Contains(err.Error(), "no bound account") {
		t.Fatalf("unbound OpenShort must refuse with 'no bound account'; got err=%v", err)
	}

	// BOUND → passes the empty-account gate; it then fails on a DIFFERENT gate
	// (account not in the mock's list / no SL-TP), never on 'no bound account'.
	bound := NewTCPTrader(s, "MNQ", "Sim101")
	if _, err := bound.OpenLong("MNQ", 1, 1); err == nil || strings.Contains(err.Error(), "no bound account") {
		t.Fatalf("bound OpenLong must clear the empty-account gate; got err=%v", err)
	}
}

// TestGetPositions_ReadsBoundAccountNotCurrent locks the decoupling: a trader reads
// its OWN bound account's positions, NOT the shared connection "current" account
// (which the dashboard can switch for display). Without this, viewing another
// account would make a running trader read the wrong positions (see itself flat →
// double-open / mis-reconcile).
func TestGetPositions_ReadsBoundAccountNotCurrent(t *testing.T) {
	s := ntwire.NewTCPServer(nil)
	// Two accounts hold DIFFERENT positions; the shared "current" points at the OTHER one.
	s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "long", Quantity: 2, AvgPrice: 30550.25}})
	s.SeedPositionsForTest("SimAccountX", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "short", Quantity: 5, AvgPrice: 29000.00}})
	s.SetCurrentAccountForTest("SimAccountX")

	// A trader BOUND to Sim101 must return Sim101's position (entry 30550.25), NOT
	// the shared current account's (SimAccountX @ 29000).
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	pos, err := tr.GetPositions()
	if err != nil {
		t.Fatalf("GetPositions: %v", err)
	}
	if len(pos) != 1 {
		t.Fatalf("want 1 position (Sim101), got %d — likely read the shared current account", len(pos))
	}
	if entry, _ := pos[0]["entryPrice"].(float64); entry != 30550.25 {
		t.Fatalf("want Sim101 entry 30550.25, got %v — read the WRONG (current) account", entry)
	}

	// Empty-bound trader must NOT borrow the current account (ef550df7 refuse
	// semantics): PositionsFor("") is !ok → fill-derived cache (empty here).
	unbound := NewTCPTrader(s, "MNQ")
	upos, err := unbound.GetPositions()
	if err != nil {
		t.Fatalf("GetPositions unbound: %v", err)
	}
	if len(upos) != 0 {
		t.Fatalf("unbound trader must not read the shared current account's positions; got %d", len(upos))
	}
}

// TestGetBalance_ReadsBoundAccountNotCurrent locks the GetBalance decouple (the
// twin of GetPositions above, previously missed): a trader's balance/equity must
// come from its OWN bound account, NOT the shared streamed "current" account.
// Without this a second trader on another sub-account displayed — and RISK-SIZED
// off — the wrong account's equity.
func TestGetBalance_ReadsBoundAccountNotCurrent(t *testing.T) {
	s := ntwire.NewTCPServer(nil)
	// Two accounts with DIFFERENT equity; the shared "current" points at the OTHER.
	s.SeedAccountBalanceForTest("Sim101", ntwire.AccountBalancePayload{
		Account: "Sim101", NetLiquidation: 70000, CashValue: 70000, BuyingPower: 70000,
	})
	s.SeedAccountBalanceForTest("SimAccountX", ntwire.AccountBalancePayload{
		Account: "SimAccountX", NetLiquidation: 12345, CashValue: 12345, BuyingPower: 12345,
	})
	s.SetCurrentAccountForTest("SimAccountX")

	// BOUND to Sim101 → must report Sim101's equity (70000) and tag the account,
	// NOT the shared current (SimAccountX @ 12345).
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	bal, err := tr.GetBalance()
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if eq, _ := bal["totalEquity"].(float64); eq != 70000 {
		t.Fatalf("want Sim101 equity 70000, got %v — read the WRONG (current) account", eq)
	}
	if acct, _ := bal["account"].(string); acct != "Sim101" {
		t.Fatalf("balance must be tagged with the bound account Sim101; got %q", acct)
	}

	// Graceful fallback: a bound account with NO snapshot yet falls back to the
	// streamed current (never zero/no-data) so we don't regress before the frame
	// arrives — mirrors today's behavior, just correctly labeled.
	trNoSnap := NewTCPTrader(s, "MNQ", "SimNoSnapshot")
	balF, err := trNoSnap.GetBalance()
	if err != nil {
		t.Fatalf("GetBalance fallback: %v", err)
	}
	if acct, _ := balF["account"].(string); acct != "SimAccountX" {
		t.Fatalf("bound account without a snapshot must fall back to current (SimAccountX); got %q", acct)
	}
}
