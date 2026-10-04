package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
)

// TestMentorAdmitChainAIEntriesOff — P0 fix/mentor-ai-off: with mentor_mode ON
// every NON-mentor entry is refused at the ONE admission chain (admitEntry),
// on every producer path — the AI decision, the agent-chat door, the armed
// planner pass and Picture. Mentor-sourced decisions pass the mentor gate.
// The reason names the mode; the gate-block counter is the chain's.
func TestMentorAdmitChainAIEntriesOff(t *testing.T) {
	withMaintenanceDir(t)
	at := mkPlanTrader(nil)
	at.config.StrategyConfig.RiskControl.MentorMode = true
	at.id = "mentor-ai-off"
	nyMidday := time.Date(2026, 9, 15, 16, 0, 0, 0, time.UTC) // 11:00 CT Tuesday

	aiOpen := &kernel.Decision{Action: "open_long", Symbol: "MNQ"}
	cases := []struct {
		name string
		in   admitIntent
	}{
		{"decision path", admitIntent{Path: admitDecision, Symbol: "MNQ", Action: "open_long", Now: nyMidday, Decision: aiOpen}},
		{"agent door", admitIntent{Path: admitAgent, Symbol: "MNQ", Action: "open_short", Now: nyMidday, Decision: &kernel.Decision{Action: "open_short", Symbol: "MNQ"}}},
		{"arm path", admitIntent{Path: admitArm, Symbol: "MNQ", Action: "open_long", Now: nyMidday, Key: "P1|S1|0"}},
		{"picture path", admitIntent{Path: admitPicture, Symbol: "MNQ", Action: "open_short", Now: nyMidday, Key: "pic-1"}},
	}
	for _, c := range cases {
		reason, refused := at.admitEntry(c.in)
		if !refused || !strings.Contains(reason, "mentor_mode: AI entries are OFF") {
			t.Fatalf("%s: a non-mentor open must be refused at the ONE admission chain with the named reason, got refused=%v %q", c.name, refused, reason)
		}
	}

	// The mentor's own synthetic decision carries MentorSourced — the mentor
	// gate must NOT refuse it (a downstream gate may still judge it).
	mentorOpen := &kernel.Decision{Action: "open_long", Symbol: "MNQ"}
	mentorOpen.MentorSourced = true
	reason, refused := at.admitEntry(admitIntent{Path: admitDecision, Symbol: "MNQ", Action: "open_long", Now: nyMidday, Decision: mentorOpen})
	if refused && strings.Contains(reason, "mentor_mode") {
		t.Fatalf("a mentor-sourced open must pass the mentor gate, got %q", reason)
	}

	// Closes never reach the admission chain (entry-only) — the agent door
	// returns them un-admitted by construction.
	if reason, refused := at.AdmitManualEntryAt("MNQ", "close_long", nyMidday); refused {
		t.Fatalf("a close is never admitted, got %q", reason)
	}
}

// TestMentorAdmitMentorArmPasses — P0 bundle conflict fix (CTO 20:14:30Z):
// the mentor injector routes every mentor entry as an armed-ledger row, so
// the gate must pass an arm-path intent flagged MentorArm. A planner arm
// (MentorArm false) stays refused; the agent door stays refused; mentor_mode
// OFF stays byte-identical.
func TestMentorAdmitMentorArmPasses(t *testing.T) {
	withMaintenanceDir(t)
	at := mkPlanTrader(nil)
	at.config.StrategyConfig.RiskControl.MentorMode = true
	at.id = "mentor-ai-off-arm"
	nyMidday := time.Date(2026, 9, 15, 16, 0, 0, 0, time.UTC)

	// A mentor-authored armed row passes the mentor gate (a downstream gate
	// may still judge it) even though it carries no Decision.
	reason, refused := at.admitEntry(admitIntent{Path: admitArm, Symbol: "MNQ", Action: "open_long", Now: nyMidday, Key: "mentor|S1|0", MentorArm: true})
	if refused && strings.Contains(reason, "mentor_mode") {
		t.Fatalf("a MentorArm arm-path open must pass the mentor gate, got %q", reason)
	}
	// The same row from the planner (MentorArm false) is refused with the
	// named reason.
	reason, refused = at.admitEntry(admitIntent{Path: admitArm, Symbol: "MNQ", Action: "open_long", Now: nyMidday, Key: "P1|S1|0"})
	if !refused || !strings.Contains(reason, "mentor_mode: AI entries are OFF") {
		t.Fatalf("a planner arm must be refused with the named reason, got refused=%v %q", refused, reason)
	}
	// The agent door stays refused.
	reason, refused = at.admitEntry(admitIntent{Path: admitAgent, Symbol: "MNQ", Action: "open_short", Now: nyMidday, Decision: &kernel.Decision{Action: "open_short", Symbol: "MNQ"}})
	if !refused || !strings.Contains(reason, "mentor_mode: AI entries are OFF") {
		t.Fatalf("the agent door must be refused with the named reason, got refused=%v %q", refused, reason)
	}
	// A MentorArm flag on any non-arm path changes nothing — only the arm
	// path reads it.
	reason, refused = at.admitEntry(admitIntent{Path: admitDecision, Symbol: "MNQ", Action: "open_long", Now: nyMidday, Decision: &kernel.Decision{Action: "open_long", Symbol: "MNQ"}, MentorArm: true})
	if !refused || !strings.Contains(reason, "mentor_mode: AI entries are OFF") {
		t.Fatalf("MentorArm must not open the decision path, got refused=%v %q", refused, reason)
	}
}

// TestMentorAdmitChainOffByteIdentical — mentor_mode OFF: the chain must not
// refuse any open with the mentor reason (the AI path is byte-identical).
func TestMentorAdmitChainOffByteIdentical(t *testing.T) {
	withMaintenanceDir(t)
	at := mkPlanTrader(nil)
	at.id = "mentor-ai-off-off"
	nyMidday := time.Date(2026, 9, 15, 16, 0, 0, 0, time.UTC)

	for _, c := range []admitIntent{
		{Path: admitDecision, Symbol: "MNQ", Action: "open_long", Now: nyMidday, Decision: &kernel.Decision{Action: "open_long", Symbol: "MNQ"}},
		{Path: admitAgent, Symbol: "MNQ", Action: "open_short", Now: nyMidday, Decision: &kernel.Decision{Action: "open_short", Symbol: "MNQ"}},
		{Path: admitArm, Symbol: "MNQ", Action: "open_long", Now: nyMidday, Key: "P1|S1|0"},
		{Path: admitArm, Symbol: "MNQ", Action: "open_long", Now: nyMidday, Key: "mentor|S1|0", MentorArm: true},
	} {
		if reason, refused := at.admitEntry(c); refused && strings.Contains(reason, "mentor_mode") {
			t.Fatalf("mentor_mode OFF: the chain must never refuse with the mentor reason, got %q", reason)
		}
	}
}

// TestMentorSkipsAIDecisionSwitch — the AI decision cycle's LLM skip follows
// the mode switch exactly: ON → skip, OFF → call (byte-identical).
func TestMentorSkipsAIDecisionSwitch(t *testing.T) {
	on := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	if !on.mentorSkipsAIDecision() {
		t.Fatal("mentor_mode ON must skip the AI decision call")
	}
	off := mentoredTrader(t, store.RiskControlConfig{})
	if off.mentorSkipsAIDecision() {
		t.Fatal("mentor_mode OFF must never skip the AI decision call")
	}
}

// TestMentorAICloseRefusedOnMentorOwnedPosition — CTO 15:17:23Z (D2.1 @02:47):
// the AI's close decisions must not act on a mentor-owned position. The
// ownership flag follows the mentor-sourced open and the flat sweep.
func TestMentorAICloseRefusedOnMentorOwnedPosition(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.positionMentorOwned = map[string]bool{"MNQ_long": true}

	// An AI close of the mentor-owned position is refused with the named reason.
	if r := at.mentorSuppressAIClose(&kernel.Decision{Action: "close_long", Symbol: "MNQ"}); !strings.Contains(r, "mentor_mode: AI closes are OFF") {
		t.Fatalf("an AI close of a mentor-owned position must be refused, got %q", r)
	}
	// The mentor's own close (MentorSourced) passes.
	mentorClose := &kernel.Decision{Action: "close_long", Symbol: "MNQ"}
	mentorClose.MentorSourced = true
	if r := at.mentorSuppressAIClose(mentorClose); r != "" {
		t.Fatalf("a mentor-sourced close must pass, got %q", r)
	}
	// A close of a position the mentor does not own is untouched.
	if r := at.mentorSuppressAIClose(&kernel.Decision{Action: "close_short", Symbol: "MNQ"}); r != "" {
		t.Fatalf("a close of a non-mentor-owned side must pass, got %q", r)
	}
	// Mentor mode OFF: byte-identical, never refused.
	off := mentoredTrader(t, store.RiskControlConfig{})
	off.positionMentorOwned = map[string]bool{"MNQ_long": true}
	if r := off.mentorSuppressAIClose(&kernel.Decision{Action: "close_long", Symbol: "MNQ"}); r != "" {
		t.Fatalf("mentor_mode OFF must never suppress a close, got %q", r)
	}
}
