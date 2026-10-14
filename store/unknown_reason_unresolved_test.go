package store

import (
	"path/filepath"
	"testing"
)

// RT-3 (release #24) — A BOT CLOSE WITH AN UNKNOWN P&L REASON IS UNRESOLVED,
// NOT INVISIBLE. BotRealizedPnL and MentorDayActivity used to drop
// reconcile_flat / unresolved rows in their WHERE, so a class-27 `unresolved`
// bot close today (exit unknown — possibly a full stop) neither appeared in
// the count nor reached the callers' fail-closed handling. ManualExcludedSince
// already counted them; the bot side now does the same. The test seam stays
// out: a synthetic row is not a trade that happened.
//
// MUTANT: restore the NOT IN (reconcile_flat, unresolved, …) filter in either
// WHERE → Unresolved reads 0 → RED.
func TestUnknownReasonBotClosesCountAsUnresolved(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "rt3.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ps := st.Position()
	base := int64(1_756_700_000_000)

	pnlRow(t, ps, 1, "u1", "Sim101", base+1_000, -40, fp(-40), CloseReasonStop)      // a known bot loss
	pnlRow(t, ps, 2, "u1", "Sim101", base+2_000, -500, nil, CloseReasonUnresolved)   // class-27: exit unknown
	pnlRow(t, ps, 3, "u1", "Sim101", base+3_000, 0, fp(0), CloseReasonReconcileFlat) // placeholder — its 0 is not a P&L
	pnlRow(t, ps, 4, "u1", "Sim101", base+4_000, -9, fp(-9), CloseReasonTestSeam)    // test seam: not a trade
	pnlRow(t, ps, 5, "u1", "Sim101", base-1_000, -77, nil, CloseReasonUnresolved)    // before the session-day: out of scope

	b, err := ps.BotRealizedPnL("u1", base)
	if err != nil {
		t.Fatal(err)
	}
	if b.Total != -40 || b.Resolved != 1 || b.Unresolved != 2 {
		t.Fatalf("BotRealizedPnL must sum the known -40 over 1 and COUNT the unresolved + reconcile_flat closes as Unresolved=2 (test seam out), got %+v", b)
	}
	// Account-scoped read: same rows.
	if b2, err := ps.BotRealizedPnL("u1", base, "Sim101"); err != nil || b2.Unresolved != 2 {
		t.Fatalf("scoped read: want Unresolved 2, got %+v err %v", b2, err)
	}

	act, err := ps.MentorDayActivity("u1", base)
	if err != nil {
		t.Fatal(err)
	}
	if act.DayNetPnl != -40 || !act.ClosedInLoss || act.ClosedInProfit || act.Unresolved != 2 {
		t.Fatalf("MentorDayActivity must count the unknown-reason closes as Unresolved=2 (net -40 from the known loss), got %+v", act)
	}

	// A day with only the test-seam row stays clean (nothing unresolved).
	pnlRow(t, ps, 6, "u2", "Sim101", base+1_000, -9, fp(-9), CloseReasonTestSeam)
	if b3, err := ps.BotRealizedPnL("u2", base); err != nil || b3.Unresolved != 0 || b3.Resolved != 0 || b3.Total != 0 {
		t.Fatalf("a test-seam row is not a trade: want 0/0/0, got %+v err %v", b3, err)
	}
	if a3, err := ps.MentorDayActivity("u2", base); err != nil || a3.Unresolved != 0 {
		t.Fatalf("a test-seam row is not a trade: want Unresolved 0, got %+v err %v", a3, err)
	}
}
