package telemetry

import "testing"

// The stalled-setup alarm's threshold + latch live in SetupStallAlertDue; the
// rollover clears the tally (and the latch) on the 17:00 CT session-day
// boundary. These pin the pure counter behaviour the trader's alert layer reads.

func TestSetupStallAlertDueThresholdAndLatch(t *testing.T) {
	ResetSetupStallsForTest()

	IncSetupStallEmitted("t1", "lvl")
	IncSetupStallEmitted("t1", "lvl")
	if due := SetupStallAlertDue("t1"); len(due) != 0 {
		t.Fatalf("2 emitted must not alert, got %+v", due)
	}

	IncSetupStallEmitted("t1", "lvl")
	due := SetupStallAlertDue("t1")
	if len(due) != 1 || due[0].Kind != "lvl" || due[0].Emitted != 3 || due[0].Placed != 0 {
		t.Fatalf("want one lvl alert {3 emitted, 0 placed}, got %+v", due)
	}
	// A silent drop (no refusal ever recorded) renders as the honest fallback.
	if due[0].LastRefusal != "" {
		t.Fatalf("no refusal recorded must leave LastRefusal empty, got %q", due[0].LastRefusal)
	}
	if due := SetupStallAlertDue("t1"); len(due) != 0 {
		t.Fatalf("the alert must fire once per kind per day, got %+v", due)
	}
}

func TestSetupStallLastRefusalRecorded(t *testing.T) {
	ResetSetupStallsForTest()

	for i := 0; i < 3; i++ {
		IncSetupStallEmitted("t1", "lvl")
	}
	SetupStallRecordRefusal("t1", "lvl", "bad geometry: missing stop/target — refuse fail-closed")
	due := SetupStallAlertDue("t1")
	if len(due) != 1 || due[0].LastRefusal != "bad geometry: missing stop/target — refuse fail-closed" {
		t.Fatalf("the last refusal must ride the alert, got %+v", due)
	}
}

func TestSetupStallPlacedSuppressesAlert(t *testing.T) {
	ResetSetupStallsForTest()

	for i := 0; i < 3; i++ {
		IncSetupStallEmitted("t1", "box")
	}
	IncSetupStallPlaced("t1", "box")
	if due := SetupStallAlertDue("t1"); len(due) != 0 {
		t.Fatalf("a kind that reached the ledger must not alert, got %+v", due)
	}
}

func TestSetupStallRolloverResets(t *testing.T) {
	ResetSetupStallsForTest()

	RolloverSetupStalls(100)
	for i := 0; i < 3; i++ {
		IncSetupStallEmitted("t1", "lvl")
	}
	if due := SetupStallAlertDue("t1"); len(due) != 1 {
		t.Fatalf("day 100: 3 emitted, 0 placed must alert, got %+v", due)
	}

	// 17:00 CT rollover clears the tally AND the latch: a fresh day starts cold.
	RolloverSetupStalls(200)
	if due := SetupStallAlertDue("t1"); len(due) != 0 {
		t.Fatalf("rollover must clear the tally, got %+v", due)
	}
	_, table := SetupStallSnapshot()
	if len(table) != 0 {
		t.Fatalf("rollover must leave an empty table, got %+v", table)
	}
}

func TestSetupStallCountersNeverMixAcrossTraders(t *testing.T) {
	ResetSetupStallsForTest()

	for i := 0; i < 3; i++ {
		IncSetupStallEmitted("tA", "lvl")
	}
	IncSetupStallEmitted("tB", "lvl")

	if due := SetupStallAlertDue("tA"); len(due) != 1 {
		t.Fatalf("tA must alert, got %+v", due)
	}
	if due := SetupStallAlertDue("tB"); len(due) != 0 {
		t.Fatalf("tB has only 1 emit and must not alert, got %+v", due)
	}

	_, table := SetupStallSnapshot()
	if table["tA"]["lvl"].Emitted != 3 || table["tB"]["lvl"].Emitted != 1 {
		t.Fatalf("per-trader tallies must not mix, got %+v", table)
	}
}

// m442 #9: the alarm arithmetic is (Emitted - Policy) >= 3, not Emitted >= 3.
// 5 emitted of which 3 were the owner's own rules = 2 real -> NO alert; 5
// emitted of which 2 were policy = 3 real -> alert. MUTANT (shown RED): test
// `c.Emitted >= 3` (ignore Policy) -> the first case alerts -> RED.
func TestSetupStallPolicyRefusalsAreNotCountedTowardTheThreshold(t *testing.T) {
	ResetSetupStallsForTest()
	for i := 0; i < 5; i++ {
		IncSetupStallEmitted("t1", "isb")
	}
	for i := 0; i < 3; i++ {
		SetupStallRecordPolicyRefusal("t1", "isb", "window: outside the trading window")
	}
	if due := SetupStallAlertDue("t1"); len(due) != 0 {
		t.Fatalf("5 emitted, 3 refused by the owner's rules = 2 real: no alert, got %+v", due)
	}

	ResetSetupStallsForTest()
	for i := 0; i < 5; i++ {
		IncSetupStallEmitted("t1", "isb")
	}
	for i := 0; i < 2; i++ {
		SetupStallRecordPolicyRefusal("t1", "isb", "window: outside the trading window")
	}
	due := SetupStallAlertDue("t1")
	if len(due) != 1 || due[0].Emitted != 5 || due[0].Policy != 2 {
		t.Fatalf("5 emitted, 2 policy = 3 real: want one alert {5 emitted, 2 policy}, got %+v", due)
	}
}
