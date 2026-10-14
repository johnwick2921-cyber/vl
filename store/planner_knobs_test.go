package store

import (
	"strings"
	"testing"
)

// Release #25 planner-knobs — the seven switches resolve OFF when absent, a
// saved value reads [O], a value outside the vocabulary reads OFF and names
// itself, and the boot line READS every resolver (never a literal).

func TestPlannerKnobsAbsentIsOff(t *testing.T) {
	for _, dp := range []*DayPlanConfig{nil, {}} {
		if v, src := ResolvePlannerRoomGate(dp); v != RoomGateOff || src != SourceShippedDefault {
			t.Fatalf("room gate absent → off [I], got %q %q", v, src)
		}
		if v, _ := ResolvePlannerRoomMinR(dp); v != RoomMinRDefault {
			t.Fatalf("min_r absent → 1.0, got %v", v)
		}
		if v, _ := ResolvePlannerExitOnThesisDeath(dp); v != ThesisDeathOff {
			t.Fatalf("thesis death absent → off, got %q", v)
		}
		if v, _ := ResolvePlannerNoChaseEnforce(dp); v {
			t.Fatal("no-chase enforce absent → off")
		}
		if v, _ := ResolvePlannerMomentumIntoZoneMaxATR(dp); v != 0 {
			t.Fatalf("momentum absent → 0 (off), got %v", v)
		}
		if v, _ := ResolvePlannerATRFloorCap(dp); v {
			t.Fatal("ATR floor cap absent → off")
		}
	}
	if v, _ := ResolveBreakevenTriggerR(RiskControlConfig{}); v != 0 {
		t.Fatalf("breakeven_trigger_r absent → 0 (points rule), got %v", v)
	}
	line := PlannerKnobsBootLine(&StrategyConfig{})
	want := "🧱 planner loss-fix switches: room_gate=off[I] min_r=1.00[I] unused · exit_on_thesis_death=off[I] · no_chase_enforce=off[I] · momentum_into_zone_max_atr=off[I] · atr_floor_cap_at_invalidation=off[I] · breakeven_trigger_r=off (points rule)[I]"
	if line != want {
		t.Fatalf("absent boot line:\n got %q\nwant %q", line, want)
	}
	if !strings.Contains(PlannerKnobsBootLine(nil), "n/a") {
		t.Fatal("no config → n/a, never a fabricated OFF")
	}
}

func TestPlannerKnobsSavedAndInvalid(t *testing.T) {
	on := true
	minR, mom := 1.5, 3.0
	cfg := &StrategyConfig{DayPlan: &DayPlanConfig{
		PlannerRoomGate: "Refuse", PlannerRoomMinR: &minR, PlannerExitOnThesisDeath: "all",
		PlannerNoChaseEnforce: &on, PlannerMomentumIntoZoneMaxATR: mom, PlannerATRFloorCapAtInvalidation: &on,
	}}
	cfg.RiskControl.BreakevenTriggerR = 1
	line := PlannerKnobsBootLine(cfg)
	want := "🧱 planner loss-fix switches: room_gate=refuse[O] min_r=1.50[O] · exit_on_thesis_death=all[O] · no_chase_enforce=on[O] · momentum_into_zone_max_atr=3×ATR5m/15m[O] · atr_floor_cap_at_invalidation=on[O] · breakeven_trigger_r=1R[O]"
	if line != want {
		t.Fatalf("saved boot line:\n got %q\nwant %q", line, want)
	}
	bad := &DayPlanConfig{PlannerRoomGate: "yes", PlannerExitOnThesisDeath: "sometimes", PlannerMomentumIntoZoneMaxATR: -2}
	if v, src := ResolvePlannerRoomGate(bad); v != RoomGateOff || !strings.Contains(src, `"yes" is not off|refuse|tp1_at_obstacle`) {
		t.Fatalf("a typo never changes trading: %q %q", v, src)
	}
	if v, src := ResolvePlannerExitOnThesisDeath(bad); v != ThesisDeathOff || !strings.Contains(src, `"sometimes"`) {
		t.Fatalf("a typo never changes trading: %q %q", v, src)
	}
	if v, _ := ResolvePlannerMomentumIntoZoneMaxATR(bad); v != 0 {
		t.Fatalf("a negative momentum ceiling reads OFF, got %v", v)
	}
	for _, k := range []string{"planner_room_gate", "planner_room_min_r", "planner_exit_on_thesis_death", "planner_no_chase_enforce",
		"planner_momentum_into_zone_max_atr", "planner_atr_floor_cap_at_invalidation", "breakeven_trigger_r"} {
		if e, ok := knobRegistry[k]; !ok || e.Status != KnobLive || len(e.Consumers) == 0 {
			t.Fatalf("knob %s must be registered live with its consumer: %+v", k, e)
		}
	}
}
