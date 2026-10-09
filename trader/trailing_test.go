package trader

import (
	"testing"

	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// 3B.4 — LONG and SHORT ratchet math (units: price points at every layer).
func TestTrailTargetMath(t *testing.T) {
	// LONG: best 29700, ATR 40, mult 2.0 → trail 29620 (80 pts below best —
	// a ticks mix-up would put it 20 pts away, 4× early; assert the points math).
	if got := trailTarget("LONG", 29700, 40, 2.0); got != 29620 {
		t.Errorf("long trail = %.2f, want 29620 (2.0×ATR40 = 80 POINTS below best)", got)
	}
	if got := trailTarget("SHORT", 29500, 40, 2.0); got != 29580 {
		t.Errorf("short trail = %.2f, want 29580 (80 points above best)", got)
	}
}

// 3B.4 — ratchet-only: pullbacks never move the stop backward.
func TestTrailDecideRatchet(t *testing.T) {
	// LONG advancing: 29620 emitted; deeper target 29650 emits; pullback target
	// 29600 (< last emitted) holds.
	if emit, lvl := trailDecide("LONG", 29620, 0, 29600, false); !emit || lvl != 29620 {
		t.Fatalf("first long emit failed (%v %.2f)", emit, lvl)
	}
	if emit, _ := trailDecide("LONG", 29650, 29620, 29600, false); !emit {
		t.Fatal("improving long trail must emit")
	}
	if emit, _ := trailDecide("LONG", 29600, 29620, 29600, false); emit {
		t.Fatal("pullback: long trail below last emitted must HOLD (never backward)")
	}
	// SHORT mirror.
	if emit, _ := trailDecide("SHORT", 29580, 0, 29650, false); !emit {
		t.Fatal("first short emit failed")
	}
	if emit, _ := trailDecide("SHORT", 29550, 29580, 29650, false); !emit {
		t.Fatal("improving short trail must emit")
	}
	if emit, _ := trailDecide("SHORT", 29590, 29580, 29650, false); emit {
		t.Fatal("pullback: short trail above last emitted must HOLD")
	}
}

// 3B.4 — trail worse than the BE floor → BE wins, no emit.
func TestTrailDecideBEFloor(t *testing.T) {
	// LONG entry 29650, BE fired (stop at entry). Trail target 29640 < entry →
	// BE wins, nothing sent.
	if emit, _ := trailDecide("LONG", 29640, 0, 29650, true); emit {
		t.Fatal("long trail below the BE floor must not emit")
	}
	if emit, lvl := trailDecide("LONG", 29660, 0, 29650, true); !emit || lvl != 29660 {
		t.Fatal("long trail above the BE floor must emit")
	}
	// SHORT entry 29650, BE fired. Trail 29655 > entry → BE wins.
	if emit, _ := trailDecide("SHORT", 29655, 0, 29650, true); emit {
		t.Fatal("short trail above the BE floor must not emit")
	}
	if emit, _ := trailDecide("SHORT", 29640, 0, 29650, true); !emit {
		t.Fatal("short trail below the BE floor must emit")
	}
}

// 3B.4 — idempotence: the same level twice → one emit.
func TestTrailDecideIdempotent(t *testing.T) {
	if emit, _ := trailDecide("LONG", 29620, 29620, 29600, false); emit {
		t.Fatal("same level as last emitted must not re-emit")
	}
}

// 3B.4 — arming modes honored.
func TestTrailArming(t *testing.T) {
	if !trailArmed(TrailArmImmediate, false, -10, 0) {
		t.Error("immediate must arm at open")
	}
	if trailArmed(TrailArmAfterBreakeven, false, 100, 0) {
		t.Error("after_breakeven must NOT arm before BE fires (even deep in profit)")
	}
	if !trailArmed(TrailArmAfterBreakeven, true, 1, 0) {
		t.Error("after_breakeven must arm once BE fired")
	}
	if trailArmed(TrailArmAfterPoints, false, 40, 50) {
		t.Error("after_trigger_points must not arm below the threshold")
	}
	if !trailArmed(TrailArmAfterPoints, false, 50, 50) {
		t.Error("after_trigger_points must arm at the threshold")
	}
	if trailArmed(TrailArmAfterPoints, false, 100, 0) {
		t.Error("after_trigger_points with no points set must never arm")
	}
}

// 3B.4 — config resolution: defaults + disabled → zero trail execution.
func TestTrailingConfigDefaultsAndDisabled(t *testing.T) {
	en, mult, period, arm, _ := trailingConfig(store.RiskControlConfig{})
	if en {
		t.Fatal("trailing must default OFF")
	}
	if mult != 2.0 || period != 14 || arm != TrailArmAfterBreakeven {
		t.Errorf("defaults = %.1f/%d/%s, want 2.0/14/after_breakeven", mult, period, arm)
	}
	en, mult, period, arm, pts := trailingConfig(store.RiskControlConfig{
		TrailingEnabled: bp(true), TrailingATRMult: 1.5, TrailingATRPeriod: 20,
		TrailingArm: TrailArmAfterPoints, TrailingArmPoints: 30,
	})
	if !en || mult != 1.5 || period != 20 || arm != TrailArmAfterPoints || pts != 30 {
		t.Errorf("explicit config not honored: %v %.1f %d %s %.0f", en, mult, period, arm, pts)
	}
	// Disabled → maybeTrailStop returns before touching any state.
	at := &AutoTrader{exchange: "ninjatrader"}
	at.config.StrategyConfig = &store.StrategyConfig{}
	at.maybeTrailStop("MNQ", "LONG", 29600, 29700)
	if len(at.trailStates) != 0 {
		t.Fatal("disabled trailing must execute zero trail code (no state created)")
	}
}

// 3B round-trip: the five Studio fields survive the ai_config codec both ways.
func TestTrailingFieldsSurviveStrategyCodec(t *testing.T) {
	cfg := &store.StrategyConfig{}
	cfg.RiskControl.TrailingEnabled = bp(true)
	cfg.RiskControl.TrailingATRMult = 2.5
	cfg.RiskControl.TrailingATRPeriod = 21
	cfg.RiskControl.TrailingArm = TrailArmAfterPoints
	cfg.RiskControl.TrailingArmPoints = 35
	blob, err := cfg.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var back store.StrategyConfig
	if err := back.UnmarshalJSON(blob); err != nil {
		t.Fatal(err)
	}
	rc := back.RiskControl
	if !hlBool(rc.TrailingEnabled, false) || rc.TrailingATRMult != 2.5 || rc.TrailingATRPeriod != 21 ||
		rc.TrailingArm != TrailArmAfterPoints || rc.TrailingArmPoints != 35 {
		t.Errorf("trailing fields lost in the ai_config codec round-trip: %+v", rc)
	}
}

// trailCallSiteTrader builds the SAME armed trailing config for both sides of
// the mentor-mode gate: trailing ENABLED + arm=immediate so the mentor-OFF twin
// provably moves the stop through moveStopWire.
func trailCallSiteTrader(mentorOn bool) *AutoTrader {
	return &AutoTrader{
		id:       "trail-gate",
		exchange: "ninjatrader",
		trader:   &ntTrader.TCPTrader{},
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{
			RiskControl: store.RiskControlConfig{
				MentorMode:       mentorOn,
				TrailingEnabled:  bp(true),
				TrailingArm:      TrailArmImmediate,
				TrailingATRMult:  2.0,
				TrailingATRPeriod: 14,
			},
		}},
	}
}

// TestTrailBlockedInMentorMode (FIX-AUTOBE-OFF-IN-MENTOR, owner ruling
// 2026-10-09): with mentor mode ON the AI-era trail must NOT move a mentor-owned
// stop. RED (named): delete the mentorEnabled check in maybeTrailStop and
// moveStopWire fires.
func TestTrailBlockedInMentorMode(t *testing.T) {
	t.Setenv("EXIT_MECHS_SUSPENDED", "0") // seam open: removing the mentor gate must make this MOVE (the RED)
	restore := a3WireBars()
	defer restore()

	at := trailCallSiteTrader(true)
	oldWire := moveStopWire
	moved := false
	moveStopWire = func(nt *ntTrader.TCPTrader, side string, newStop float64) error {
		moved = true
		return nil
	}
	t.Cleanup(func() { moveStopWire = oldWire })

	at.maybeTrailStop("MNQ", "LONG", 29600, 29700)
	if moved {
		t.Fatal("mentor mode ON: the AI-era trail must NOT move the stop")
	}
	if len(at.trailStates) != 0 {
		t.Fatalf("mentor mode ON: the trail must create no state, got %d", len(at.trailStates))
	}
}

// TestTrailFiresWhenMentorModeOff — mentor OFF keeps the trail byte-identical:
// the SAME armed config moves the stop through moveStopWire (so the blocked
// test above cannot pass on an empty trail path).
func TestTrailFiresWhenMentorModeOff(t *testing.T) {
	t.Setenv("EXIT_MECHS_SUSPENDED", "0")
	restore := a3WireBars()
	defer restore()

	at := trailCallSiteTrader(false)
	oldWire := moveStopWire
	var movedTo []float64
	moveStopWire = func(nt *ntTrader.TCPTrader, side string, newStop float64) error {
		movedTo = append(movedTo, newStop)
		return nil
	}
	t.Cleanup(func() { moveStopWire = oldWire })

	at.maybeTrailStop("MNQ", "LONG", 29600, 29700)
	if len(movedTo) != 1 {
		t.Fatalf("mentor OFF: the armed trail must move the stop once; got %v", movedTo)
	}
	// LONG trail = best − mult×ATR, strictly below best (29700) and above 0.
	if movedTo[0] <= 0 || movedTo[0] >= 29700 {
		t.Fatalf("mentor OFF: trail level %.2f out of range (0, 29700)", movedTo[0])
	}
}
