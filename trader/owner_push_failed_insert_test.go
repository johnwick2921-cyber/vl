package trader

import (
	"strings"
	"testing"
	"time"
)

// m471 L3-2 — a pushed P0 reaches the owner even when its alert row INSERT
// fails. emitAlert pushed only on `err == nil && created`; the untracked-position
// P0 is raised exactly when the same SQLite file just refused a Position write,
// and daily-loss / dead_man / ai-failing / panics are one-shot latches that are
// never re-emitted, so a failed INSERT lost them for good. Drives the
// production emitAlert -> maybeOwnerPush -> process-default channel; the INSERT
// is made to fail with a trigger (standing in for "database is locked" / I/O).
//
// MUTANT (shown RED): gate the push on `err == nil && created` again -> "P0
// untracked-position was NOT pushed (and not held)" -> RED.
func TestP0IsPushedEvenWhenItsAlertRowInsertFails(t *testing.T) {
	net, p, _ := m469Push(t, time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC))
	at, st := m469Trader(t, "m471-l32-trader")

	// control: with a healthy DB the P0 is pushed
	at.emitAlert("P0", "untracked-position", "untracked:MNQ:LONG:1",
		"UNTRACKED LONG MNQ @ 25000.00 — fill recorded at the broker, not in the DB",
		"trader frozen for new entries · reconcile from NT8, then clear the freeze")
	p.Flush()
	if n := len(net.sent()); n != 1 {
		t.Fatalf("control: healthy DB should push 1, got %d", n)
	}

	if err := st.GormDB().Exec(`CREATE TRIGGER m471_l32_fail BEFORE INSERT ON day_plan_alerts BEGIN SELECT RAISE(ABORT, 'database is locked'); END;`).Error; err != nil {
		t.Fatal(err)
	}
	at.emitAlert("P0", "untracked-position", "untracked:MNQ:SHORT:2",
		"UNTRACKED SHORT MNQ @ 25010.00 — fill recorded at the broker, not in the DB",
		"trader frozen for new entries · reconcile from NT8, then clear the freeze")
	at.emitAlert("P0", "daily-loss", "daily-loss:2026-11-06", "🔴 Daily loss limit hit", "loss -500.00 <= limit -500.00")
	// a repeat of the same event while the DB is still broken is ONE message
	at.emitAlert("P0", "daily-loss", "daily-loss:2026-11-06", "🔴 Daily loss limit hit", "loss -500.00 <= limit -500.00")
	// a P1 whose row failed is not pushed by this path
	at.emitAlert("P1", "fill", "fill:9", "Filled", "x")
	p.Flush()

	got := net.sent()
	if c := countContaining(got, "UNTRACKED SHORT"); c != 1 {
		t.Errorf("P0 untracked-position was NOT pushed (and not held) because its alert row INSERT failed: %d message(s), held=%d", c, p.Held())
	}
	if c := countContaining(got, "Daily loss limit hit"); c != 1 {
		t.Errorf("P0 daily-loss after a failed INSERT: want exactly 1 message (the owner-push key dedupes the repeat), got %d", c)
	}
	for _, s := range got {
		if strings.Contains(s, "Filled") {
			t.Errorf("a P1 with a failed row must not be pushed by the P0 path: %q", s)
		}
	}
	if len(m469Rows(t, st, at.id, "daily-loss")) != 0 {
		t.Fatal("the trigger did not make the INSERT fail - the test proves nothing")
	}
}
