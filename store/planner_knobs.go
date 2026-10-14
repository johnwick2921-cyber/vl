package store

import (
	"fmt"
	"strconv"
	"strings"
)

// ── Release #25 lane planner-knobs (owner "do all now", 2026-10-14) ─────────
//
// The STRICT-12345 report (0 W / 11 L for planner market_in_zone fills since
// 09-23) ranked the causes; the ones the owner had not ruled on ship as
// switches, ALL DEFAULT OFF (COMMON-25 case b — planner numbers the owner has
// not given). ONE resolver per knob, called where the value enters (canon 28);
// the boot line, the Studio effective rows and every consumer read these.
// Absent = OFF = today's behaviour byte-identical.

// Room gate modes (F-2).
const (
	RoomGateOff           = "off"
	RoomGateRefuse        = "refuse"
	RoomGateTP1AtObstacle = "tp1_at_obstacle"
)

// RoomMinRDefault is planner_room_min_r when unset — the report's suggested
// 1.0 (used only when the room gate is on).
const RoomMinRDefault = 1.0

// Thesis-death exit modes (F-3).
const (
	ThesisDeathOff              = "off"
	ThesisDeathInvalidationOnly = "invalidation_only"
	ThesisDeathAll              = "all"
)

// ResolvePlannerRoomGate resolves day_plan.planner_room_gate: off (absent /
// empty) | refuse | tp1_at_obstacle. A value outside the vocabulary resolves
// OFF and the source names it — a typo never changes trading.
func ResolvePlannerRoomGate(c *DayPlanConfig) (string, string) {
	if c == nil || strings.TrimSpace(c.PlannerRoomGate) == "" {
		return RoomGateOff, SourceShippedDefault
	}
	v := strings.ToLower(strings.TrimSpace(c.PlannerRoomGate))
	switch v {
	case RoomGateOff, RoomGateRefuse, RoomGateTP1AtObstacle:
		return v, SourceSaved
	}
	return RoomGateOff, SourceShippedDefault + " (saved value " + strconv.Quote(c.PlannerRoomGate) + " is not off|refuse|tp1_at_obstacle — OFF)"
}

// ResolvePlannerRoomMinR resolves day_plan.planner_room_min_r: a saved value
// in (0, 10], else 1.0. Read only when the room gate is on.
func ResolvePlannerRoomMinR(c *DayPlanConfig) (float64, string) {
	if c == nil || c.PlannerRoomMinR == nil {
		return RoomMinRDefault, SourceShippedDefault
	}
	if v := *c.PlannerRoomMinR; v > 0 && v <= 10 {
		return v, SourceSaved
	}
	return RoomMinRDefault, SourceShippedDefault + " (saved value " + strconv.FormatFloat(*c.PlannerRoomMinR, 'g', -1, 64) + " is outside (0, 10])"
}

// ResolvePlannerExitOnThesisDeath resolves day_plan.planner_exit_on_thesis_death:
// off (absent / empty) | invalidation_only | all. Outside the vocabulary → OFF.
func ResolvePlannerExitOnThesisDeath(c *DayPlanConfig) (string, string) {
	if c == nil || strings.TrimSpace(c.PlannerExitOnThesisDeath) == "" {
		return ThesisDeathOff, SourceShippedDefault
	}
	v := strings.ToLower(strings.TrimSpace(c.PlannerExitOnThesisDeath))
	switch v {
	case ThesisDeathOff, ThesisDeathInvalidationOnly, ThesisDeathAll:
		return v, SourceSaved
	}
	return ThesisDeathOff, SourceShippedDefault + " (saved value " + strconv.Quote(c.PlannerExitOnThesisDeath) + " is not off|invalidation_only|all — OFF)"
}

// ResolvePlannerNoChaseEnforce resolves day_plan.planner_no_chase_enforce:
// nil = OFF (WARN-first, today), an explicit value as saved.
func ResolvePlannerNoChaseEnforce(c *DayPlanConfig) (bool, string) {
	if c == nil || c.PlannerNoChaseEnforce == nil {
		return false, SourceShippedDefault
	}
	return *c.PlannerNoChaseEnforce, SourceSaved
}

// ResolvePlannerMomentumIntoZoneMaxATR resolves
// day_plan.planner_momentum_into_zone_max_atr: a saved value in (0, 10], else
// 0 = OFF. A negative or absurd value is named in the source and reads OFF.
func ResolvePlannerMomentumIntoZoneMaxATR(c *DayPlanConfig) (float64, string) {
	if c == nil || c.PlannerMomentumIntoZoneMaxATR == 0 {
		return 0, SourceShippedDefault
	}
	if v := c.PlannerMomentumIntoZoneMaxATR; v > 0 && v <= 10 {
		return v, SourceSaved
	}
	return 0, SourceShippedDefault + " (saved value " + strconv.FormatFloat(c.PlannerMomentumIntoZoneMaxATR, 'g', -1, 64) + " is outside (0, 10] — OFF)"
}

// ResolvePlannerATRFloorCap resolves day_plan.planner_atr_floor_cap_at_invalidation:
// nil = OFF (the floor widens freely, today), an explicit value as saved.
func ResolvePlannerATRFloorCap(c *DayPlanConfig) (bool, string) {
	if c == nil || c.PlannerATRFloorCapAtInvalidation == nil {
		return false, SourceShippedDefault
	}
	return *c.PlannerATRFloorCapAtInvalidation, SourceSaved
}

// ResolveBreakevenTriggerR resolves risk_control.breakeven_trigger_r: a saved
// value in (0, 10] = the R trigger, else 0 = OFF = the points rule (today).
func ResolveBreakevenTriggerR(rc RiskControlConfig) (float64, string) {
	if rc.BreakevenTriggerR == 0 {
		return 0, SourceShippedDefault
	}
	if v := rc.BreakevenTriggerR; v > 0 && v <= 10 {
		return v, SourceSaved
	}
	return 0, SourceShippedDefault + " (saved value " + strconv.FormatFloat(rc.BreakevenTriggerR, 'g', -1, 64) + " is outside (0, 10] — OFF, points rule)"
}

// PlannerKnobsBootLine is the per-trader load line for the seven switches:
// every value READ from the resolver its consumer calls (L7 — never a
// literal), each tagged with its origin letter.
func PlannerKnobsBootLine(cfg *StrategyConfig) string {
	if cfg == nil {
		return "🧱 planner loss-fix switches: n/a (no strategy config)"
	}
	dp := cfg.DayPlan
	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	room, roomSrc := ResolvePlannerRoomGate(dp)
	minR, minRSrc := ResolvePlannerRoomMinR(dp)
	minRNote := ""
	if room == RoomGateOff {
		minRNote = " unused"
	}
	death, deathSrc := ResolvePlannerExitOnThesisDeath(dp)
	nc, ncSrc := ResolvePlannerNoChaseEnforce(dp)
	mom, momSrc := ResolvePlannerMomentumIntoZoneMaxATR(dp)
	momText := "off"
	if mom > 0 {
		momText = strconv.FormatFloat(mom, 'g', -1, 64) + "×ATR5m/15m"
	}
	capOn, capSrc := ResolvePlannerATRFloorCap(dp)
	ber, berSrc := ResolveBreakevenTriggerR(cfg.RiskControl)
	berText := "off (points rule)"
	if ber > 0 {
		berText = strconv.FormatFloat(ber, 'g', -1, 64) + "R"
	}
	return fmt.Sprintf("🧱 planner loss-fix switches: room_gate=%s%s min_r=%.2f%s%s · exit_on_thesis_death=%s%s · no_chase_enforce=%s%s · momentum_into_zone_max_atr=%s%s · atr_floor_cap_at_invalidation=%s%s · breakeven_trigger_r=%s%s",
		room, OriginLetter(roomSrc), minR, OriginLetter(minRSrc), minRNote,
		death, OriginLetter(deathSrc),
		onOff(nc), OriginLetter(ncSrc),
		momText, OriginLetter(momSrc),
		onOff(capOn), OriginLetter(capSrc),
		berText, OriginLetter(berSrc))
}
