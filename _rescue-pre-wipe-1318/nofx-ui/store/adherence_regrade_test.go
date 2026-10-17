package store

import (
	"path/filepath"
	"testing"
)

// ADHERENCE REGRADE MIGRATION (owner ruling 2026-09-03, flag-guarded).
//
// Four rows carry FULL lineage and still hold the off-plan D they were graded
// with before the lineage arrived: 575, 584, 586, 591 — plan_matched=1,
// plan_band=armed_fill. A cited+matched close grades base A, and two penalties
// reach only C, so D is impossible from correct grading.
//
// THREE rows look similar and must NOT be touched. Backfilling all seven would
// regrade a test-seam row and promote two closes that earned their D:
//
//	530  cited_scenario_id 'off-plan' (the literal sentinel), matched=0  → D is right
//	572  source e7_farside_test, cited 'TEST-E7', plan_version 0         → not a trade
//	582  matched=0 → base C ("direction mismatched") − 1 penalty         → D is right
//
// The migration CLEARS the grade rather than writing one. It does not invent a
// verdict: the W5 analytics regrade the row with the lineage now in hand.

func regradeFixture(t *testing.T) *Store {
	t.Helper()
	st, err := New(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	mk := func(source, scenario, band string, ver int, matched bool, grade string) int64 {
		p := &TraderPosition{
			TraderID: "t", Symbol: "MNQ", Side: "SHORT", Quantity: 1,
			EntryPrice: 29285, EntryTime: 1, Status: "OPEN", Source: source,
			CreatedAt: 1, UpdatedAt: 1,
		}
		if err := st.Position().Create(p); err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := st.Position().ClosePosition(p.ID, 29115, "", -140, 0, "stop"); err != nil {
			t.Fatalf("close: %v", err)
		}
		if err := st.Position().SetPlanLinkFull(p.ID, ver, scenario, matched, band, "pid", "2026-09-03", "NY"); err != nil {
			t.Fatalf("link: %v", err)
		}
		if err := st.Position().SetAdherence(p.ID, grade); err != nil {
			t.Fatalf("grade: %v", err)
		}
		return p.ID
	}

	// the four real ones
	mk("reconcile", "S2", "armed_fill", 3, true, "D")
	mk("reconcile", "S2", "armed_fill", 6, true, "D")
	mk("reconcile", "S3", "armed_fill", 5, true, "D")
	mk("reconcile", "S1", "armed_fill", 2, true, "D")
	// the three that must be left alone
	mk("system", "off-plan", "", 2, false, "D")                  // 530 shape
	mk("e7_farside_test", "TEST-E7", "armed_fill", 0, true, "D") // 572 shape
	mk("armed_entry", "S2", "", 3, false, "D")                   // 582 shape
	// and a correctly-graded A, to prove the predicate is not "all closed rows"
	mk("reconcile", "S1", "armed_fill", 4, true, "A")
	return st
}

func TestAdherenceRegradeTouchesOnlyTheImpossibleDs(t *testing.T) {
	st := regradeFixture(t)

	ids, err := st.Position().StuckAdherenceRows()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(ids) != 4 {
		t.Fatalf("scan found %d rows, want exactly the 4 impossible Ds — got %v", len(ids), ids)
	}

	n, err := st.Position().RegradeStuckAdherence()
	if err != nil {
		t.Fatalf("regrade: %v", err)
	}
	if n != 4 {
		t.Fatalf("regraded %d, want 4", n)
	}

	closed, err := st.Position().GetClosedPositions("t", 20)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	cleared, keptD, keptA := 0, 0, 0
	for _, p := range closed {
		switch p.AdherenceGrade {
		case "":
			cleared++
		case "D":
			keptD++
		case "A":
			keptA++
		}
	}
	if cleared != 4 {
		t.Errorf("cleared %d, want 4", cleared)
	}
	if keptD != 3 {
		t.Errorf("kept %d Ds, want 3 (off-plan sentinel, test seam, direction mismatch)", keptD)
	}
	if keptA != 1 {
		t.Errorf("kept %d As, want 1 — the predicate must not sweep correctly-graded rows", keptA)
	}

	// IDEMPOTENT: a second run finds nothing, because the rows no longer hold D.
	n2, err := st.Position().RegradeStuckAdherence()
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second run regraded %d, want 0 — the migration must be idempotent", n2)
	}
}
