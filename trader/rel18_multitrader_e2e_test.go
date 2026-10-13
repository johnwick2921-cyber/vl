package trader

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── REL18-MULTI — two mentor traders in one bot (A identity · B wiring · C
// account rails) — the A+B+C harness at the MERGED head. One fake TCP server,
// two AutoTraders on SimAccount1 / Sim101, same strategy, same bars. This is
// the integration pin the per-PR tests cannot see: the three fixes run TOGETHER.
//
// Production call sites exercised: mentorArmIntent (authoring), mentorLiveArmFor
// / mentorUnregisterLiveArm / mentorCancelArm (A registry), mentorAddGate (B
// per-trader open-side source), mentorCount / mentorCountSnapshot (B per-trader
// counters), installNTOrderedExecutions (C ordered-owner rail).

// rel18TwoTraderPair builds the production two-trader shape: one fake TCP
// server, two bound TCP traders (SimAccount1 / Sim101), two mentor AutoTraders
// on the SAME store (one process, one ledger), same MNQ symbol and strategy.
func rel18TwoTraderPair(t *testing.T) (at1, at2 *AutoTrader, st *store.Store) {
	t.Helper()
	resetMentorLiveArms(t)
	st, err := store.New(filepath.Join(t.TempDir(), "rel18.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := ntwire.NewTCPServer(nil)
	cfg := func() AutoTraderConfig {
		return AutoTraderConfig{
			NinjaTraderSymbol: "MNQ",
			StrategyConfig:    &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}},
		}
	}
	mk := func(id, account string) *AutoTrader {
		nt := ntTrader.NewTCPTrader(srv, "MNQ", account)
		at := &AutoTrader{
			id: id, name: id, userID: "u", store: st,
			exchange: "ninjatrader", exchangeID: "ex",
			trader: nt,
			config: cfg(),
		}
		at.config.Exchange = "ninjatrader"
		at.mentorArmEpoch = 1000 // distinct-but-stable epochs; the harness asserts identity, not time
		return at
	}
	at1 = mk("hoang", "SimAccount1")
	at2 = mk("12345", "Sim101")
	at2.mentorArmEpoch = 2000
	return at1, at2, st
}

func rel18ISBIntent() mentor.Intent {
	return mentor.Intent{Action: mentor.PlaceStopEntry, ArmID: "isb-1", Setup: "ISB",
		Side: mentor.SideLong, Price: 30000, Stop: 29990, Target: 30020,
		ExpiryMs: time.Now().UnixMilli() + 60_000}
}

// TestRel18TwoTradersSameBarDistinctRowsAndAccounts — the whole P0: two traders
// author the SAME ArmID on the SAME bar; each mints its OWN ledger row (trader
// in the plan id), its own size, and each owns a DISTINCT (symbol, account)
// ordered-execution slot. Mutant: revert PlanID to the shared "mentor" → one
// row overwrites the other (RED).
func TestRel18TwoTradersSameBarDistinctRowsAndAccounts(t *testing.T) {
	at1, at2, st := rel18TwoTraderPair(t)
	now := time.Now().UnixMilli()
	at1.mentorArmIntent(rel18ISBIntent(), mentorSizeChoice{Contracts: 2, Tier: "base"}, now, now, "B", 0)
	at2.mentorArmIntent(rel18ISBIntent(), mentorSizeChoice{Contracts: 5, Tier: "confluence"}, now, now, "C", 0)

	var rows []store.ArmedOrderDB
	if err := st.GormDB().Where("origin = ?", store.ArmOriginMentor).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("two traders on the same bar must mint TWO ledger rows, got %d", len(rows))
	}
	byTrader := map[string]*store.ArmedOrderDB{}
	for i := range rows {
		byTrader[rows[i].TraderID] = &rows[i]
	}
	if byTrader["hoang"] == nil || byTrader["12345"] == nil {
		t.Fatalf("both traders' rows must exist, got %+v", rows)
	}
	if *byTrader["hoang"].Contracts != 2 || *byTrader["12345"].Contracts != 5 {
		t.Fatalf("sizes crossed: hoang=%v want 2, 12345=%v want 5", *byTrader["hoang"].Contracts, *byTrader["12345"].Contracts)
	}
	if byTrader["hoang"].PlanID == byTrader["12345"].PlanID {
		t.Fatalf("plan ids must carry the trader, both %q", byTrader["hoang"].PlanID)
	}

	// A: each registry resolves its OWN row.
	l1, ok1 := at1.mentorLiveArmFor("isb-1")
	l2, ok2 := at2.mentorLiveArmFor("isb-1")
	if !ok1 || !ok2 || l1.RowID == l2.RowID {
		t.Fatalf("registries must resolve distinct rows: ok1=%v ok2=%v row1=%d row2=%d", ok1, ok2, l1.RowID, l2.RowID)
	}

	// C: each trader owns a DISTINCT (symbol, account) ordered-execution slot.
	if err := at1.installNTOrderedExecutions(at1.armedTrader()); err != nil {
		t.Fatalf("hoang install on SimAccount1 must succeed: %v", err)
	}
	if err := at2.installNTOrderedExecutions(at2.armedTrader()); err != nil {
		t.Fatalf("12345 install on Sim101 must succeed: %v", err)
	}
	// A THIRD trader on the SAME (symbol, account) as hoang is refused (O2).
	at3 := &AutoTrader{
		id: "intruder", name: "intruder", userID: "u", store: st,
		exchange: "ninjatrader", exchangeID: "ex",
		trader: ntTrader.NewTCPTrader(at1.armedTrader().GetServer(), "MNQ", "SimAccount1"),
		config: at1.config,
	}
	if err := at3.installNTOrderedExecutions(at3.armedTrader()); !errors.Is(err, ntwire.ErrOrderedOwnerExists) {
		t.Fatalf("a second trader on hoang's (MNQ, SimAccount1) must be refused with ErrOrderedOwnerExists, got %v", err)
	}
}

// TestRel18CancelIsTraderScoped — a cancel by one trader never touches the
// other's resting entry. This is the P0 #1 shape driven through the production
// cancel path (not a poisoned registry entry).
func TestRel18CancelIsTraderScoped(t *testing.T) {
	at1, at2, st := rel18TwoTraderPair(t)
	now := time.Now().UnixMilli()
	at1.mentorArmIntent(rel18ISBIntent(), mentorSizeChoice{Contracts: 2, Tier: "base"}, now, now, "B", 0)
	at2.mentorArmIntent(rel18ISBIntent(), mentorSizeChoice{Contracts: 5, Tier: "confluence"}, now, now, "C", 0)

	// hoang cancels its isb-1 — only hoang's row may go terminal.
	if !at1.mentorCancelArm(mentor.Intent{Action: mentor.CancelArm, ArmID: "isb-1", Reason: "test cancel"}) {
		t.Fatal("hoang must be able to cancel its own arm")
	}
	var rows []store.ArmedOrderDB
	if err := st.GormDB().Where("origin = ?", store.ArmOriginMentor).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		if rows[i].TraderID == "hoang" && rows[i].State == store.StateArmed {
			t.Fatalf("hoang's row must not remain armed after its cancel, got %q", rows[i].State)
		}
		if rows[i].TraderID == "12345" && rows[i].State != store.StateArmed {
			t.Fatalf("12345's resting entry must be untouched by hoang's cancel, got %q", rows[i].State)
		}
	}
}

// TestRel18PerTraderGatesAndCounters — B: the never-add gate reads ITS OWN
// open-side source, and the counters are per trader (no summing across traders).
// at1 sees a long open; at2 is flat.
func TestRel18PerTraderGatesAndCounters(t *testing.T) {
	at1, at2, _ := rel18TwoTraderPair(t)
	at1.mentorOpenSideSource = func() string { return "long" }
	at2.mentorOpenSideSource = func() string { return "" }

	in := rel18ISBIntent() // long

	// at1: a long is already open → the never-add gate refuses the long intent.
	if refused, why := at1.mentorAddGate(in); !refused || !strings.Contains(why, "already open") {
		t.Fatalf("hoang with an open long must refuse a long add, got refused=%v why=%q", refused, why)
	}
	// at2: flat → the same intent is NOT refused.
	if refused, _ := at2.mentorAddGate(in); refused {
		t.Fatal("12345 is flat — its never-add gate must not refuse")
	}

	// Counters are per trader: a count on at1 never lands on at2's snapshot.
	at1.mentorCount("e2e_probe")
	if got := at1.mentorCountSnapshot()["e2e_probe"]; got != 1 {
		t.Fatalf("at1 must count its own probe, got %d", got)
	}
	if got := at2.mentorCountSnapshot()["e2e_probe"]; got != 0 {
		t.Fatalf("at2 must NOT see at1's counter, got %d", got)
	}
}
