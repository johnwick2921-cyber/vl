package store

import (
	"path/filepath"
	"testing"
)

// B1 (DS-104): the done-after-win seam reads the day's CLOSED trades with the
// strict corrected column. A NULL pnl_corrected row is UNRESOLVED — surfaced,
// never coerced — so the caller can fail closed.

func TestMentorDayActivitySessionDayNetAndClosedProfit(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "mda.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ps := st.Position()
	base := int64(1_756_700_000_000) // a fixed epoch; the CME-day window is passed in

	// One profitable close (+40) and one loss (−10) → net +30, won.
	pnlRow(t, ps, 1, "m1", "Sim101", base+1_000, 40, fp(40), "sync")
	pnlRow(t, ps, 2, "m1", "Sim101", base+2_000, -10, fp(-10), "sync")
	act, err := ps.MentorDayActivity("m1", base)
	if err != nil {
		t.Fatal(err)
	}
	if act.DayNetPnl != 30 || !act.ClosedInProfit || act.Unresolved != 0 {
		t.Fatalf("winning day: got %+v, want net=30 won=true unresolved=0", act)
	}

	// Only losses → net negative, no win.
	pnlRow(t, ps, 3, "m2", "Sim101", base+3_000, -5, fp(-5), "sync")
	pnlRow(t, ps, 4, "m2", "Sim101", base+4_000, -15, fp(-15), "sync")
	act2, err := ps.MentorDayActivity("m2", base)
	if err != nil {
		t.Fatal(err)
	}
	if act2.DayNetPnl != -20 || act2.ClosedInProfit || act2.Unresolved != 0 {
		t.Fatalf("losing day: got %+v, want net=-20 won=false unresolved=0", act2)
	}

	// A NULL pnl_corrected closed row is UNRESOLVED — excluded from the sum and
	// surfaced for the fail-closed caller (the raw realized_pnl is never read).
	pnlRow(t, ps, 5, "m3", "Sim101", base+5_000, 100, nil, "sync")
	act3, err := ps.MentorDayActivity("m3", base)
	if err != nil {
		t.Fatal(err)
	}
	if act3.Unresolved != 1 || act3.DayNetPnl != 0 || act3.ClosedInProfit {
		t.Fatalf("NULL-pnl day: got %+v, want unresolved=1 net=0 won=false", act3)
	}

	// The session-day window is respected: a close BEFORE sinceMs is excluded.
	act4, err := ps.MentorDayActivity("m1", base+1_500)
	if err != nil {
		t.Fatal(err)
	}
	if act4.DayNetPnl != -10 || act4.ClosedInProfit {
		t.Fatalf("window: got %+v, want net=-10 won=false (only the loss after sinceMs)", act4)
	}

	// Account scoping (production passes the bound account): a row on another
	// account is excluded from a scoped read, included in an unscoped read.
	pnlRow(t, ps, 6, "m4", "Sim102", base+6_000, 50, fp(50), "sync")
	scoped, err := ps.MentorDayActivity("m4", base, "Sim101")
	if err != nil {
		t.Fatal(err)
	}
	if scoped.Unresolved != 0 || scoped.DayNetPnl != 0 || scoped.ClosedInProfit {
		t.Fatalf("account scope: got %+v, want empty (the +50 is on Sim102)", scoped)
	}
	unscoped, err := ps.MentorDayActivity("m4", base)
	if err != nil {
		t.Fatal(err)
	}
	if unscoped.DayNetPnl != 50 || !unscoped.ClosedInProfit {
		t.Fatalf("unscoped: got %+v, want net=50 won=true", unscoped)
	}
}
