package telemetry

import (
	"strings"
	"testing"
)

// TestGateBlocks_TripTwoGatesShowsExactlyThem is the B6 acceptance: after tripping
// exactly two gates for a trader, the snapshot shows those two with the right
// counts and NOTHING else.
func TestGateBlocks_TripTwoGatesShowsExactlyThem(t *testing.T) {
	resetGateBlocksForTest()
	defer resetGateBlocksForTest()

	RolloverGateBlocks(1_000) // adopt a session-day

	// Trip gate A twice, gate B once — for trader "T1".
	IncGateBlock("T1", "feed_down")
	IncGateBlock("T1", "feed_down")
	IncGateBlock("T1", "consecutive_loss")

	day, table := GateBlockSnapshot()
	if day != 1_000 {
		t.Fatalf("session day = %d, want 1000", day)
	}
	t1 := table["T1"]
	if t1 == nil {
		t.Fatal("no counters for T1")
	}
	if len(table) != 1 {
		t.Fatalf("expected exactly one trader bucket, got %d: %v", len(table), table)
	}
	if len(t1) != 2 {
		t.Fatalf("expected exactly two gates for T1, got %d: %v", len(t1), t1)
	}
	if t1["feed_down"] != 2 {
		t.Errorf("feed_down = %d, want 2", t1["feed_down"])
	}
	if t1["consecutive_loss"] != 1 {
		t.Errorf("consecutive_loss = %d, want 1", t1["consecutive_loss"])
	}
	// A gate that was never tripped must be absent (zero), not present.
	if _, present := t1["stale_data"]; present {
		t.Error("stale_data was never tripped but appears in the table")
	}

	// Summary lists exactly the two tripped gates, highest count first.
	sum := GateBlockDailySummary()
	if !strings.Contains(sum, "feed_down=2") || !strings.Contains(sum, "consecutive_loss=1") {
		t.Errorf("summary missing expected gates: %q", sum)
	}
	if strings.Contains(sum, "stale_data") {
		t.Errorf("summary must not mention an untripped gate: %q", sum)
	}
	if i, j := strings.Index(sum, "feed_down"), strings.Index(sum, "consecutive_loss"); i < 0 || j < 0 || i > j {
		t.Errorf("summary must order by count desc (feed_down before consecutive_loss): %q", sum)
	}
}

// TestIncTraderEventDoesNotCountAsGateBlock is the P2-3a (release #26) anchor:
// an EVENT (arm authored, night transition) must never read as a refusal in the
// per-session gate-block table. If a call site regresses to IncGateBlock, this
// test's snapshot/summary asserts fail.
func TestIncTraderEventDoesNotCountAsGateBlock(t *testing.T) {
	resetGateBlocksForTest()
	defer resetGateBlocksForTest()
	RolloverGateBlocks(1_000)

	IncTraderEvent("T1", "arm_authored")
	IncTraderEvent("T1", "night_transition")
	IncGateBlock("T1", "feed_down") // a REAL refusal

	_, table := GateBlockSnapshot()
	if _, present := table["T1"]["arm_authored"]; present {
		t.Error("arm_authored must not appear in the gate-block table")
	}
	if _, present := table["T1"]["night_transition"]; present {
		t.Error("night_transition must not appear in the gate-block table")
	}
	if table["T1"]["feed_down"] != 1 {
		t.Errorf("feed_down = %d, want 1 (a real refusal must still count)", table["T1"]["feed_down"])
	}
	sum := GateBlockDailySummary()
	if strings.Contains(sum, "arm_authored") || strings.Contains(sum, "night_transition") {
		t.Errorf("summary must not mention events as blocks: %q", sum)
	}
}

// TestGateBlocks_SummaryGroupsPerTrader is the P2-3b (release #26) anchor: the
// daily summary names which trader each gate blocked, instead of an aggregate
// that loses the trader dimension.
func TestGateBlocks_SummaryGroupsPerTrader(t *testing.T) {
	resetGateBlocksForTest()
	defer resetGateBlocksForTest()
	RolloverGateBlocks(1_000)

	IncGateBlock("T1", "feed_down")
	IncGateBlock("T2", "feed_down")
	IncGateBlock("", "b3_order_dedup")

	sum := GateBlockDailySummary()
	if !strings.Contains(sum, "T1{feed_down=1}") {
		t.Errorf("summary missing T1 group: %q", sum)
	}
	if !strings.Contains(sum, "T2{feed_down=1}") {
		t.Errorf("summary missing T2 group: %q", sum)
	}
	if !strings.Contains(sum, "(process){b3_order_dedup=1}") {
		t.Errorf("summary missing process-wide group: %q", sum)
	}
}

// TestGateBlocks_ProcessWideBucket: the empty-trader key holds shared-chokepoint
// gates (B3) separately from any real trader's counters.
func TestGateBlocks_ProcessWideBucket(t *testing.T) {
	resetGateBlocksForTest()
	defer resetGateBlocksForTest()
	RolloverGateBlocks(1)

	IncGateBlock("", "b3_order_dedup")
	IncGateBlock("T1", "feed_down")

	_, table := GateBlockSnapshot()
	if table[""]["b3_order_dedup"] != 1 {
		t.Errorf(`process-wide bucket b3_order_dedup = %d, want 1`, table[""]["b3_order_dedup"])
	}
	if table["T1"]["feed_down"] != 1 {
		t.Errorf("T1 feed_down = %d, want 1", table["T1"]["feed_down"])
	}
}

// TestGateBlocks_RolloverClearsAndSummarizes: a session-day change returns the
// ending day's summary and resets the table; the first-ever rollover is silent.
func TestGateBlocks_RolloverClearsAndSummarizes(t *testing.T) {
	resetGateBlocksForTest()
	defer resetGateBlocksForTest()

	// First rollover adopts the day silently (no prior data).
	if s := RolloverGateBlocks(100); s != "" {
		t.Errorf("first rollover must be silent, got %q", s)
	}
	IncGateBlock("T1", "price_sanity")
	// Same-day rollover → no summary, counts intact.
	if s := RolloverGateBlocks(100); s != "" {
		t.Errorf("same-day rollover must return empty, got %q", s)
	}
	if _, table := GateBlockSnapshot(); table["T1"]["price_sanity"] != 1 {
		t.Fatal("same-day rollover must not clear counts")
	}
	// New session-day → returns the ending day's summary and clears the table.
	sum := RolloverGateBlocks(200)
	if !strings.Contains(sum, "price_sanity=1") {
		t.Errorf("rollover summary missing ended-day data: %q", sum)
	}
	day, table := GateBlockSnapshot()
	if day != 200 {
		t.Errorf("session day = %d, want 200", day)
	}
	if len(table) != 0 {
		t.Errorf("table must be cleared after rollover, got %v", table)
	}
	// Empty table summarizes cleanly.
	if s := GateBlockDailySummary(); !strings.Contains(s, "no gate blocks") {
		t.Errorf("empty summary = %q, want the no-blocks sentinel", s)
	}
}
