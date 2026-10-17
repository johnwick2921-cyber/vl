package ninjatrader

import (
	"encoding/json"
	"strings"
	"testing"

	ntwire "vl/provider/ninjatrader"
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

// TestPlaceEntry_NonSIMBoundAccountStillRefuses pins the SIM-only law AFTER
// UPDATER-FLAT-LIVE-OK: a FLAT non-SIM connection may pass the installation
// census now, but its account is STILL untradeable — the order path's refusal
// is unchanged, and the exported gate predicate agrees with it.
func TestPlaceEntry_NonSIMBoundAccountStillRefuses(t *testing.T) {
	s := ntwire.NewTCPServer(nil)
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Live123", IsSim: false}}, "Live123")

	tr := NewTCPTrader(s, "MNQ", "Live123")
	if tr.IsAccountTradeable("Live123") {
		t.Fatal("IsAccountTradeable must be false for a non-SIM account, whatever the census says")
	}
	if _, err := tr.OpenWithBracket("MNQ", "long", 1, 0, 0); err == nil || !strings.Contains(err.Error(), "not tradeable") {
		t.Fatalf("a NON-SIM bound account must refuse the entry after the census ruling: err=%v", err)
	}
}

// TestPlaceEntry_NT8ClosedStillRefuses pins F35's live-only law at the ORDER
// path: with NT8 closed (an empty live list) the entry is refused even though
// the gate can vouch for the account from the PERSISTED frame. The trader never
// reads the store, so the gate's fallback can never leak into a placed order.
func TestPlaceEntry_NT8ClosedStillRefuses(t *testing.T) {
	s := ntwire.NewTCPServer(nil) // no live accounts — NT8 closed
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	if tr.IsAccountTradeable("Sim101") {
		t.Fatal("IsAccountTradeable must be false on an empty live list")
	}
	if _, err := tr.OpenWithBracket("MNQ", "long", 1, 0, 0); err == nil || !strings.Contains(err.Error(), "not tradeable") {
		t.Fatalf("an entry with NT8 closed must refuse at the order path: err=%v", err)
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
	// semantics): PositionsFor("") is !ok; absent fill evidence is UNKNOWN
	// (W117 F4 — never fabricated as flat).
	unbound := NewTCPTrader(s, "MNQ")
	upos, err := unbound.GetPositions()
	if err == nil {
		t.Fatal("GetPositions unbound must report unknown rather than flat")
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

	// W117-F F7: a bound account with NO snapshot REFUSES — another account's
	// equity is never a substitute for no answer (risk sizing must not use it).
	trNoSnap := NewTCPTrader(s, "MNQ", "SimNoSnapshot")
	if _, err := trNoSnap.GetBalance(); err == nil {
		t.Fatal("bound account without a snapshot must refuse, not fall back to the shared current account")
	}
}
