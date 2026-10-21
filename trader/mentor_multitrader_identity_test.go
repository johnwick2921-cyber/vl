package trader

import (
	"path/filepath"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/store"
)

// ── REL18-A MULTI-TRADER IDENTITY (P0) ──────────────────────────────────────
//
// The owner now runs TWO mentor traders in one bot ("hoang" and "12345"), both
// MNQ, both mentor mode, the SAME strategy — one AddOn link, one process, one
// ledger, one live-arm registry. These tests pin that an ArmID minted by B can
// never resolve to (or act on) A's entry, and vice-versa. Production call sites:
// mentorArmIntent (authoring), mentorLiveArmFor / mentorUnregisterLiveArm /
// mentorCancelArm / setMentorExitMode (the registry + exit-mode consumers).

// multiTraderPair builds two mentor AutoTraders sharing ONE store (the
// production two-trader shape: one process, one ledger, one package-global
// registry), with DISTINCT per-trader epochs.
func multiTraderPair(t *testing.T) (atA, atB *AutoTrader, st *store.Store) {
	t.Helper()
	resetMentorLiveArms(t)
	st, err := store.New(filepath.Join(t.TempDir(), "multi.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	mk := func(id string, epoch int64) *AutoTrader {
		at := &AutoTrader{id: id, store: st, exchange: "ninjatrader"}
		at.config.Exchange = "ninjatrader"
		at.config.NinjaTraderSymbol = "MNQ"
		at.config.StrategyConfig = &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}}
		at.mentorArmEpoch = epoch
		return at
	}
	return mk("A", 1000), mk("B", 2000), st
}

func multiISBIntent() mentor.Intent {
	return mentor.Intent{Action: mentor.PlaceStopEntry, ArmID: "isb-1", Setup: "ISB",
		Side: mentor.SideLong, Price: 30000, Stop: 29990, Target: 30020,
		ExpiryMs: time.Now().UnixMilli() + 60_000}
}

// TestMultiTraderSameArmIDDistinctLedgerRows (P0 #2): two traders authoring the
// SAME ArmID on the SAME bar must mint TWO ledger rows (trader A and B), each
// with its own size/split, and each trader's registry resolves ITS OWN row.
// Mutants: revert PlanID to "mentor" → B's upsert overwrites A's row (one row);
// make the epoch shared → the two rows can collide on scenario.
func TestMultiTraderSameArmIDDistinctLedgerRows(t *testing.T) {
	atA, atB, st := multiTraderPair(t)
	now := time.Now().UnixMilli()
	atA.mentorArmIntent(multiISBIntent(), mentorSizeChoice{Contracts: 2, Tier: "base"}, now, now, "B", 0)
	atB.mentorArmIntent(multiISBIntent(), mentorSizeChoice{Contracts: 4, Tier: "confluence"}, now, now, "C", 0)

	var rows []store.ArmedOrderDB
	if err := st.GormDB().Where("origin = ?", store.ArmOriginMentor).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("two traders authoring isb-1 must produce TWO ledger rows, got %d", len(rows))
	}
	var a, b *store.ArmedOrderDB
	for i := range rows {
		switch rows[i].TraderID {
		case "A":
			a = &rows[i]
		case "B":
			b = &rows[i]
		}
	}
	if a == nil || b == nil {
		t.Fatalf("both traders' rows must exist, got rows %+v", rows)
	}
	if a.Contracts == nil || *a.Contracts != 2 {
		t.Fatalf("A contracts = %v, want 2 (B must not overwrite A's size)", a.Contracts)
	}
	if b.Contracts == nil || *b.Contracts != 4 {
		t.Fatalf("B contracts = %v, want 4", b.Contracts)
	}
	if a.Scenario == b.Scenario {
		t.Fatalf("A and B must author distinct scenarios, both %q", a.Scenario)
	}
	if a.PlanID == b.PlanID {
		t.Fatalf("A and B must author distinct plan ids (trader in the key), both %q", a.PlanID)
	}

	la, oka := atA.mentorLiveArmFor("isb-1")
	lb, okb := atB.mentorLiveArmFor("isb-1")
	if !oka || !okb {
		t.Fatalf("both registries must resolve isb-1: A ok=%v B ok=%v", oka, okb)
	}
	if la.RowID == lb.RowID {
		t.Fatalf("A and B must resolve DIFFERENT rows, both %d", la.RowID)
	}
	if la.RowID != a.ID || lb.RowID != b.ID {
		t.Fatalf("registry resolves the wrong row: A %d/%d, B %d/%d", la.RowID, a.ID, lb.RowID, b.ID)
	}
}

// TestMultiTraderCancelAndUnregisterAreTraderScoped (P0 #1/#3): B's unregister
// prunes B's entry only, and a registry entry under B's key that carries A's
// identity (the cross-trader collision the TraderID verify defends against) is
// REFUSED — B's cancel never acts on A's resting entry. Mutant: remove the
// TraderID check in mentorLiveArmFor → the poisoned entry resolves and this
// turns RED.
func TestMultiTraderCancelAndUnregisterAreTraderScoped(t *testing.T) {
	atA, atB, st := multiTraderPair(t)
	now := time.Now().UnixMilli()
	atA.mentorArmIntent(multiISBIntent(), mentorSizeChoice{Contracts: 2, Tier: "base"}, now, now, "B", 0)
	atB.mentorArmIntent(multiISBIntent(), mentorSizeChoice{Contracts: 4, Tier: "confluence"}, now, now, "C", 0)

	la, _ := atA.mentorLiveArmFor("isb-1")

	// B's unregister prunes B's entry only — A's survives.
	atB.mentorUnregisterLiveArm("isb-1")
	if _, ok := atA.mentorLiveArmFor("isb-1"); !ok {
		t.Fatal("B's unregister must leave A's entry in the registry")
	}
	if _, ok := atB.mentorLiveArmFor("isb-1"); ok {
		t.Fatal("B's unregister must prune B's own entry")
	}

	// Poison: an entry under B's key carrying A's TraderID (a mis-keyed entry).
	// The TraderID verify must refuse it, so B's cancel never acts on A's row.
	mentorLiveMu.Lock()
	mentorLiveArms[mentorArmKey("B", "isb-1")] = mentorLiveArm{TraderID: "A", ArmID: "isb-1", RowID: la.RowID, Side: "long", Entry: 30000, Scenario: "isb-1-1000"}
	mentorLiveMu.Unlock()
	if _, ok := atB.mentorLiveArmFor("isb-1"); ok {
		t.Fatal("a registry entry whose TraderID is not the caller's must NOT resolve (cross-trader refusal)")
	}
	if atB.mentorCancelArm(mentor.Intent{Action: mentor.CancelArm, ArmID: "isb-1", Reason: "test"}) {
		t.Fatal("B's cancel must refuse a cross-trader entry — never act on it")
	}
	var aRow store.ArmedOrderDB
	if err := st.GormDB().First(&aRow, la.RowID).Error; err != nil {
		t.Fatal(err)
	}
	if aRow.State != store.StateArmed {
		t.Fatalf("B's cancel must NEVER cancel A's resting entry — A state %q, want armed", aRow.State)
	}
}

// TestMultiTraderExitModeIndependent (P2 #8): the exit branch is keyed by the
// trader's OWN scenario (its own epoch). A reload of B (a new B epoch) must
// never break A's exit-mode lookup — and vice-versa.
func TestMultiTraderExitModeIndependent(t *testing.T) {
	atA, atB, _ := multiTraderPair(t)
	now := time.Now().UnixMilli()
	atA.mentorArmIntent(multiISBIntent(), mentorSizeChoice{Contracts: 2, Tier: "base"}, now, now, "B", 0)
	atB.mentorArmIntent(multiISBIntent(), mentorSizeChoice{Contracts: 4, Tier: "confluence"}, now, now, "C", 0)

	aScenario := atA.mentorScenarioFor("isb-1")
	bScenario := atB.mentorScenarioFor("isb-1")
	atA.setMentorExitMode(aScenario, "B")
	atB.setMentorExitMode(bScenario, "C")

	if got := atA.mentorExitMode(aScenario); got != "B" {
		t.Fatalf("A's exit branch = %q, want B", got)
	}
	if got := atB.mentorExitMode(bScenario); got != "C" {
		t.Fatalf("B's exit branch = %q, want C", got)
	}

	// B reloads: a new epoch. A's scenario must be untouched.
	atB.mentorArmEpoch = 3000
	atB.mentorArmIntent(multiISBIntent(), mentorSizeChoice{Contracts: 4, Tier: "confluence"}, now, now, "swing", 0)
	atB.setMentorExitMode(atB.mentorScenarioFor("isb-1"), "swing")

	if got := atA.mentorExitMode(aScenario); got != "B" {
		t.Fatalf("B's reload must NOT break A's exit-mode lookup — A got %q, want B", got)
	}
	if got := atB.mentorExitMode(bScenario); got != "C" {
		t.Fatalf("the OLD B scenario must keep its branch — got %q, want C", got)
	}
	if got := atB.mentorExitMode(atB.mentorScenarioFor("isb-1")); got != "swing" {
		t.Fatalf("the reloaded B scenario must hold its new branch — got %q, want swing", got)
	}
}
