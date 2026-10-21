package trader

import (
	"strings"
	"testing"

	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

func bePtr(b bool) *bool { return &b }

func TestBreakevenTrigger(t *testing.T) {
	on, off := bePtr(true), bePtr(false)
	cases := []struct {
		name      string
		rc        store.RiskControlConfig
		side      string
		entry     float64
		mark      float64
		wantFire  bool
		wantPtsGE float64 // sanity: pts should be >= this
	}{
		{"disabled (nil) never fires", store.RiskControlConfig{}, "long", 30352, 30450, false, 0},
		{"disabled (false) never fires", store.RiskControlConfig{BreakevenEnabled: off}, "long", 30352, 30450, false, 0},
		{"long +98 >= default 50 → fire", store.RiskControlConfig{BreakevenEnabled: on}, "long", 30352, 30450, true, 90},
		{"long +40 < default 50 → no", store.RiskControlConfig{BreakevenEnabled: on}, "long", 30352, 30392, false, 0},
		{"long exactly +50 → fire", store.RiskControlConfig{BreakevenEnabled: on}, "long", 30352, 30402, true, 50},
		{"short +60 >= 50 → fire (mirror)", store.RiskControlConfig{BreakevenEnabled: on}, "short", 30400, 30340, true, 60},
		{"short losing → no", store.RiskControlConfig{BreakevenEnabled: on}, "short", 30400, 30460, false, 0},
		{"custom trigger 20, long +25 → fire", store.RiskControlConfig{BreakevenEnabled: on, BreakevenTriggerPoints: 20}, "long", 30352, 30377, true, 25},
		{"custom trigger 100, long +25 → no", store.RiskControlConfig{BreakevenEnabled: on, BreakevenTriggerPoints: 100}, "long", 30352, 30377, false, 0},
		// UPPERCASE side — this is what the real caller passes (NT8 positionMap →
		// upperSideStr → "LONG"/"SHORT"). These rows fail against the pre-fix
		// case-sensitive == "long" comparison and guard the casing regression.
		{"UPPER long +98 >= 50 → fire", store.RiskControlConfig{BreakevenEnabled: on}, "LONG", 30352, 30450, true, 90},
		{"UPPER long losing → no (not inverted)", store.RiskControlConfig{BreakevenEnabled: on}, "LONG", 30352, 30300, false, 0},
		{"UPPER short +60 >= 50 → fire", store.RiskControlConfig{BreakevenEnabled: on}, "SHORT", 30400, 30340, true, 60},
		{"UPPER short losing → no", store.RiskControlConfig{BreakevenEnabled: on}, "SHORT", 30400, 30460, false, 0},
		{"UPPER long exactly +50 → fire (boundary)", store.RiskControlConfig{BreakevenEnabled: on}, "LONG", 30352, 30402, true, 50},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fire, pts := breakevenTrigger(c.rc, c.side, c.entry, c.mark)
			if fire != c.wantFire {
				t.Fatalf("fire=%v want %v (pts=%.1f)", fire, c.wantFire, pts)
			}
			if fire && pts < c.wantPtsGE {
				t.Fatalf("pts=%.1f want >= %.1f", pts, c.wantPtsGE)
			}
		})
	}
}

// TestBreakevenTrigger_NT8UppercaseNotInverted reproduces the exact casing the
// real production caller passes. checkPositionDrawdown reads pos["side"] straight
// from NT8's GetPositions/positionMap, which emits UPPERCASE "LONG"/"SHORT"
// (upperSideStr). The pre-fix breakevenTrigger compared side == "long" (lowercase),
// so every NT8 long fell into the short branch pts = entry-mark — inverted: a
// WINNING long produced negative pts and never armed, while a LOSING long produced
// positive pts and would have moved the stop to entry on a loser (instant stop-out).
// This test asserts the post-fix, correct direction for the caller's real casing.
func TestBreakevenTrigger_NT8UppercaseNotInverted(t *testing.T) {
	on := bePtr(true)
	rc := store.RiskControlConfig{BreakevenEnabled: on} // default trigger 50

	// A winning long (+62.5) MUST arm — the case the bug silently broke.
	if fire, pts := breakevenTrigger(rc, "LONG", 29129, 29191.5); !fire || pts <= 0 {
		t.Fatalf("winning LONG: fire=%v pts=%.1f — want fire=true, pts>0 (pre-fix bug: never fired on a winner)", fire, pts)
	}
	// A losing long (-62.5) MUST NOT arm — pre-fix it would have (moving the stop to entry on a loser).
	if fire, pts := breakevenTrigger(rc, "LONG", 29129, 29066.5); fire {
		t.Fatalf("losing LONG: fire=%v pts=%.1f — want fire=false (pre-fix bug: fired on a loser)", fire, pts)
	}
	// Mirror for shorts: a winning short (+62.5) MUST arm.
	if fire, pts := breakevenTrigger(rc, "SHORT", 29515.75, 29453.25); !fire || pts <= 0 {
		t.Fatalf("winning SHORT: fire=%v pts=%.1f — want fire=true, pts>0", fire, pts)
	}
	// A losing short (-62.5) MUST NOT arm.
	if fire, _ := breakevenTrigger(rc, "SHORT", 29515.75, 29578.25); fire {
		t.Fatalf("losing SHORT: want fire=false")
	}
}

// TestAutoBEBlockedInMentorMode (FIX-AUTOBE-OFF-IN-MENTOR, owner ruling
// 2026-10-09): with mentor mode ON the AI-era auto-BE must NOT move a
// mentor-owned stop. RED (named): delete the mentorEnabled check in
// maybeMoveStopToBreakeven and moveStopWire fires.
func TestAutoBEBlockedInMentorMode(t *testing.T) {
	t.Setenv("EXIT_MECHS_SUSPENDED", "0") // seam open: removing the mentor gate must make this MOVE (the RED)
	on := bePtr(true)
	at := &AutoTrader{
		id:       "autobe-mentor",
		exchange: "ninjatrader",
		trader:   &ntTrader.TCPTrader{},
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{
			RiskControl: store.RiskControlConfig{
				MentorMode:            true,
				BreakevenEnabled:      on,
				BreakevenTriggerPoints: 40,
			},
		}},
	}
	oldWire := moveStopWire
	moved := false
	moveStopWire = func(nt *ntTrader.TCPTrader, side string, newStop float64) error {
		moved = true
		return nil
	}
	t.Cleanup(func() { moveStopWire = oldWire })
	resetAutoBEMentorBlockedCountForTest()

	get := warnPlusCapture(t)

	// +45 pts ≥ 40 → the trigger WOULD fire; mentor mode ON blocks the wire.
	at.maybeMoveStopToBreakeven("MNQ", "LONG", 31023.50, 31068.50)
	if moved {
		t.Fatal("mentor mode ON: the auto-BE must NOT move the stop")
	}
	if c := AutoBEMentorBlockedCount(); c != 1 {
		t.Fatalf("auto-BE mentor-blocked counter = %d, want 1", c)
	}
	if !hasLine(get(), "auto-breakeven: not applied — mentor mode ON") {
		t.Fatalf("expected the one 'not applied' WARN line, got %v", get())
	}

	// 2nd tick on the SAME position: no new log line, no re-count.
	at.maybeMoveStopToBreakeven("MNQ", "LONG", 31023.50, 31070.00)
	if c := AutoBEMentorBlockedCount(); c != 1 {
		t.Fatalf("2nd tick re-counted: %d, want still 1", c)
	}
	n := 0
	for _, l := range get() {
		if strings.Contains(l, "auto-breakeven: not applied — mentor mode ON") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("2nd tick logged a new line: %d lines, want 1 (lines=%v)", n, get())
	}
}

// TestAutoBEFiresWhenMentorModeOff — mentor OFF keeps the auto-BE byte-identical:
// +45 pts ≥ 40 moves the stop to entry exactly as before.
func TestAutoBEFiresWhenMentorModeOff(t *testing.T) {
	t.Setenv("EXIT_MECHS_SUSPENDED", "0") // the 0B seam must be open for the wire move
	on := bePtr(true)
	at := &AutoTrader{
		id:       "autobe-ai",
		exchange: "ninjatrader",
		trader:   &ntTrader.TCPTrader{},
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{
			RiskControl: store.RiskControlConfig{
				MentorMode:            false,
				BreakevenEnabled:      on,
				BreakevenTriggerPoints: 40,
			},
		}},
	}
	oldWire := moveStopWire
	var movedTo []float64
	moveStopWire = func(nt *ntTrader.TCPTrader, side string, newStop float64) error {
		movedTo = append(movedTo, newStop)
		return nil
	}
	t.Cleanup(func() { moveStopWire = oldWire })
	resetAutoBEMentorBlockedCountForTest()

	at.maybeMoveStopToBreakeven("MNQ", "LONG", 31023.50, 31068.50) // +45 ≥ 40
	if len(movedTo) != 1 || movedTo[0] != 31023.50 {
		t.Fatalf("mentor OFF: auto-BE must move the stop to entry 31023.50; got %v", movedTo)
	}
	if c := AutoBEMentorBlockedCount(); c != 0 {
		t.Fatalf("mentor OFF: the mentor-blocked counter must stay 0, got %d", c)
	}
}
