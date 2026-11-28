package trader

import (
	"os"
	"strings"
	"testing"

	"vl/market"
	"vl/store"
)

// m420b — banner / alert hygiene (release #50).

// B2 — "close" and "fill" are informational, not P0. Pinned at the two
// production emit sites (a source pin: driving a full fill/close needs the whole
// broker stack, and the kind/level pair is a literal at the call).
func TestM420bCloseAndFillAlertsAreP1AtTheEmitSites(t *testing.T) {
	for file, kind := range map[string]string{
		"auto_trader_clock.go":    "close",
		"auto_trader_decision.go": "fill",
	} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		if strings.Contains(src, `emitAlert("P0", "`+kind+`"`) {
			t.Fatalf("%s still emits %q as P0 (informational alerts must be P1)", file, kind)
		}
		if !strings.Contains(src, `emitAlert("P1", "`+kind+`"`) {
			t.Fatalf("%s does not emit %q as P1", file, kind)
		}
	}
}

// B3 — block -> stuck alert -> resume -> the alert is acked, through the
// production driver (driveDeadManWatchdog's wdResumed edge).
func TestM420bDeadManStuckAlertAckedOnResume(t *testing.T) {
	at, s, broker, conn := deadManLoopback(t)
	at.config.StrategyConfig.DayPlan = &store.DayPlanConfig{PlanEnabled: true}
	at.driveDeadManWatchdog()
	reconnectUnseen(t, s, broker, conn)
	if ev := at.driveDeadManWatchdog(); ev != wdReconnected {
		t.Fatalf("precondition: want reconnect, got %d", ev)
	}
	at.emitAlert("P0", "dead_man_reconcile_stuck", "deadman-stuck-"+at.id+"-1", "stuck", "b")
	if n, _ := at.store.Alert().UnackedCount(at.id); n != 1 {
		t.Fatalf("precondition: one unacked stuck alert, got %d", n)
	}
	if ev := at.driveDeadManWatchdog(); ev != wdResumed {
		t.Fatalf("precondition: want resume, got %d", ev)
	}
	if n, _ := at.store.Alert().UnackedCount(at.id); n != 0 {
		t.Fatalf("the stuck alert must be acked when the watchdog resumes, %d still unacked", n)
	}
}

// B3 — restart-safe: a fresh run whose watchdog is not blocked clears a stale
// banner a dead process left (the same helper Run calls under that guard).
func TestM420bDeadManStuckAckedAtStartWhenNotBlocked(t *testing.T) {
	at, st := deadManEscalationTrader(t)
	at.emitAlert("P0", "dead_man_reconcile_stuck", "deadman-stuck-"+at.id+"-9", "stuck", "b")
	b, err := os.ReadFile("auto_trader.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "if !at.deadMan.entriesBlocked() {\n\t\tat.ackDeadManStuck()") {
		t.Fatal("Run must ack stale stuck alerts when the watchdog is not blocked")
	}
	if at.deadMan.entriesBlocked() {
		t.Fatal("fixture: zero-value watchdog must not be blocked")
	}
	at.ackDeadManStuck()
	if n, _ := st.Alert().UnackedCount(at.id); n != 0 {
		t.Fatalf("stale stuck alert must be acked, got %d unacked", n)
	}
	// A blocked watchdog keeps its (new) alert: Run's guard skips the ack.
}

// B5 — closed-market time is never counted: at 17:05 CT Monday (5 min after the
// reopen) a newest bar from 15:59 is 66 min old by the raw stamp, but the gap is
// clamped to the open span (17:00), so nothing fires; at 17:11 it does, the text
// names the clamped start, and the dedupe key is session-day + clamped start.
func TestM420bFeedDownAgeClampsToTheOpenSpan(t *testing.T) {
	at, st := feedTrader(t)
	last := ctTimeAt(t, "2026-08-17", "15:59")
	withProvider(t, []market.Kline{{OpenTime: last.UnixMilli()}})

	at.checkFeedDown(ctTimeAt(t, "2026-08-17", "17:05"))
	if got := feedAlerts(t, st, "feed-down"); got != 0 {
		t.Fatalf("closed-market time must not count: a 5-min-old open span fired %d alerts", got)
	}
	at.checkFeedDown(ctTimeAt(t, "2026-08-17", "17:11"))
	at.checkFeedDown(ctTimeAt(t, "2026-08-17", "17:20")) // same gap: deduped
	alerts, _ := st.Alert().List("t1", 50)
	var fd []*store.AlertDB
	for _, a := range alerts {
		if a.Kind == "feed-down" {
			fd = append(fd, a)
		}
	}
	if len(fd) != 1 {
		t.Fatalf("want exactly one feed-down alert, got %d", len(fd))
	}
	if want := "feeddown:2026-08-17:17:00 CT"; fd[0].EventID != want {
		t.Fatalf("dedupe key = %q, want %q", fd[0].EventID, want)
	}
	if !strings.Contains(fd[0].Body, "No bar frames since 17:00 CT (11m0s) while CME is open.") {
		t.Fatalf("body must name the clamped start and age, got %q", fd[0].Body)
	}
}

