package trader

import (
	"testing"
	"time"

	"vl/store"
)

// m226 — the protection P0s are the only signal for a missing, partial or
// stuck stop, so they must (1) reach the desk even with the day plan OFF
// (every mentor trader) and (2) fire again for a NEW episode instead of
// being swallowed by the forever-dedupe on (trader_id, event_id).

// MUTANT: drop the four kinds from isOpsAlertKind → RED (0 rows with the
// day plan off).
func TestM226ProtectionAlertsReachDeskWithoutDayPlan(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{})
	at.id = "m226-noplan"
	at.exchange = "ninjatrader"
	at.trader = &MockTrader{}
	now := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	at.actOnProtection(protectionVerdict{Action: protectionOK, HaveQty: 1, PendingStuck: true, Why: "stuck"},
		"MNQ", "LONG", 1, "sigA", "t", 0, now)
	alerts, err := st.Alert().List(at.id, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range alerts {
		if a.Kind == "protection_pending_stuck" {
			found = true
		}
	}
	if !found {
		t.Fatalf("protection_pending_stuck must reach the desk with the day plan OFF, got %d alert rows", len(alerts))
	}
	for _, k := range []string{"unprotected_position", "protection_partial", "protection_pending_stuck", "protection_over_covered"} {
		if !isOpsAlertKind(k) {
			t.Fatalf("%s must be an ops alert kind", k)
		}
	}
}

// MUTANT: drop the signal / session-day from the stuck event id → RED (the
// second episode, next session day, is deduped into the first).
func TestM226StuckAlertFiresAgainForANewEpisode(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{})
	at.id = "m226-episode"
	at.exchange = "ninjatrader"
	at.trader = &MockTrader{}
	v := protectionVerdict{Action: protectionOK, HaveQty: 1, PendingStuck: true, Why: "stuck"}
	day1 := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	at.actOnProtection(v, "MNQ", "LONG", 1, "sigA", "t", 0, day1)
	at.actOnProtection(v, "MNQ", "LONG", 1, "sigA", "t", 0, day1.Add(time.Minute)) // same episode: deduped
	at.actOnProtection(v, "MNQ", "LONG", 1, "sigB", "t", 0, day1.Add(24*time.Hour))
	alerts, err := st.Alert().List(at.id, 50)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range alerts {
		if a.Kind == "protection_pending_stuck" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("one stuck P0 per episode: want 2 (episode 1 deduped across passes, episode 2 new), got %d", n)
	}
}
