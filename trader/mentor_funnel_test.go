package trader

import (
	"strings"
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// ── N12 MENTOR FUNNEL pins ─────────────────────────────────────────────────

// funnelAT builds a minimal mentor-mode AutoTrader with a live evaluator so the
// funnel can read the kernel refusal ledgers.
func funnelAT(t *testing.T) *AutoTrader {
	t.Helper()
	at := &AutoTrader{
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}}},
	}
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	t.Cleanup(ResetMentorCountersForTest)
	return at
}

// Pin: a tick with an ORB refusal shows orb_not_drawn:1 in the funnel line.
func TestMentorFunnelLineShowsOrbRefusal(t *testing.T) {
	line := mentorFunnelLine(1, 2, map[string]int{"orb_not_drawn": 1}, map[string]int{}, map[string]int{}, 0, 0, 0)
	if !strings.Contains(line, "orb_not_drawn:1") {
		t.Fatalf("funnel line missing orb_not_drawn:1: %q", line)
	}
}

// Pin: the kernel merge reads BOTH refusal ledgers (State.Refusals + Limits).
// Mutant: skip the Limits merge → this goes RED.
func TestMentorKernelRefusalsMergesBothLedgers(t *testing.T) {
	at := funnelAT(t)
	at.mentorEval.State.Refusals = map[string]int{"orb_not_drawn": 1}
	at.mentorEval.State.Limits.Refusals = map[string]int{"loss_box_blocked": 2}
	got := mentorKernelRefusals(at)
	if got["orb_not_drawn"] != 1 || got["loss_box_blocked"] != 2 {
		t.Fatalf("kernel refusals = %v, want orb_not_drawn:1 + loss_box_blocked:2", got)
	}
}

// Pin (FU-1 R2): a kernel fill counter (record_fill_from_row) renders under
// "kernel fills", never under "kernel refusals".
func TestMentorFunnelSeparatesKernelFills(t *testing.T) {
	at := funnelAT(t)
	at.mentorEval.State.Limits.Counters = map[string]int{"record_fill_from_row": 1}
	if got := mentorKernelRefusals(at); got["record_fill_from_row"] != 0 {
		t.Fatalf("a fill counter must not render as a kernel refusal: %v", got)
	}
	if fills := mentorKernelFills(at); fills["record_fill_from_row"] != 1 {
		t.Fatalf("a fill counter must render under kernel fills: %v", fills)
	}
	line := mentorFunnelLine(1, 2, map[string]int{}, map[string]int{"record_fill_from_row": 1}, map[string]int{}, 0, 0, 0)
	if !strings.Contains(line, "kernel fills {record_fill_from_row:1}") {
		t.Fatalf("funnel line missing the kernel fills segment: %q", line)
	}
	if strings.Contains(line, "kernel refusals {record_fill_from_row:1}") {
		t.Fatalf("a fill counter must not render under kernel refusals: %q", line)
	}
}

// Pin: trader refusals are the refusal-family counters, never stage/tier/OK.
func TestMentorTraderRefusalsFilters(t *testing.T) {
	ResetMentorCountersForTest()
	mentorCount("refused_no_max")
	mentorCount("refused_no_max")
	mentorCount("placement_held")
	mentorCount("armed_base")
	mentorCount("tier_base")
	got := mentorTraderRefusals()
	if got["refused_no_max"] != 2 {
		t.Fatalf("refused_no_max = %d, want 2", got["refused_no_max"])
	}
	if got["placement_held"] != 1 {
		t.Fatalf("placement_held = %d, want 1", got["placement_held"])
	}
	if got["armed_base"] != 0 || got["tier_base"] != 0 {
		t.Fatalf("stage/tier counters must NOT be refusals: %v", got)
	}
}

// Pin: the funnel stage bumps increment (a placed arm increments placed).
func TestMentorFunnelStageBumps(t *testing.T) {
	f := &mentorFunnel{}
	f.bumpAuthored()
	f.bumpPlaced()
	f.bumpPlaced()
	f.bumpFilled()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.authored != 1 || f.placed != 2 || f.filled != 1 {
		t.Fatalf("authored=%d placed=%d filled=%d, want 1/2/1", f.authored, f.placed, f.filled)
	}
}

// Pin: mentorFunnelTick records one closed bar + the intents and emits (lastFp set).
func TestMentorFunnelTickRecordsBarsIntents(t *testing.T) {
	at := funnelAT(t)
	at.mentorEval.State.Refusals = map[string]int{"orb_not_drawn": 1}
	at.mentorFunnelTick(1, 3)
	f := &at.mentorFunnel
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.bars != 1 || f.intents != 3 || f.lastFp == "" {
		t.Fatalf("bars=%d intents=%d lastFp=%q, want 1/3/non-empty", f.bars, f.intents, f.lastFp)
	}
	if !strings.Contains(f.lastFp, "orb_not_drawn:1") {
		t.Fatalf("fingerprint missing orb_not_drawn:1: %q", f.lastFp)
	}
}
