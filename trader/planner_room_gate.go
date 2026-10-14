package trader

import (
	"fmt"
	"math"

	"vl/kernel"
	"vl/market"
	"vl/store"
	"vl/telemetry"
)

// ── Release #25 PK-1 (STRICT-12345 F-2) — THE ROOM GATE ─────────────────────
//
// Every strict-planner loser of 10-14 stalled at its FIRST obstacle: the room
// to it (r_to_obstacle) was 0.25–0.60 R while the arm target sat at 2.04–3.82 R
// (649 0.50, 650 0.60, 652 0.55, 653 0.25, 654 0.42). The planner already
// writes the obstacle (economics.first_obstacle, refused at write unless it is
// the nearest seated level on the path — kernel/scenario_write_truth.go) and
// the machine recomputes the ratio (kernel.EconomicsFor) — but a sub-1R first
// obstacle was WARN only ("fact only; target policy unchanged").
//
// Knob day_plan.planner_room_gate (store.ResolvePlannerRoomGate), DEFAULT OFF:
//   - off             → nothing here runs (the doc is untouched, no issue);
//   - refuse          → an arm whose r_to_obstacle < planner_room_min_r is a
//                       write-time issue: attempts 1..N-1 carry the repair
//                       hint, the last attempt writes the arm disabled with
//                       reason room_gate (the existing feasibility machinery);
//   - tp1_at_obstacle → the arm's target (leg 1's on a split) is MOVED to the
//                       first obstacle; the existing R:R floor then judges the
//                       new geometry like any other arm (so with
//                       min_risk_reward_ratio 2 such an arm is refused "rr").
// An obstacle the economics cannot state (no first_obstacle / no risk) is a
// room_gate refusal in both ON modes — ON means "prove the room".
//
// Every refusal / move is logged (WARN) and counted twice: the per-trader
// gate-block table (GET /api/risk/gate-blocks) and a recorded system counter
// planner_knob:<gate>:<trader>:<date>:<session> (counters record, never infer).

// RoomGateClass is the refusal class (arm_disabled_reason + counter key).
const RoomGateClass = "room_gate"

// RoomGateTP1Class counts a target moved to the first obstacle.
const RoomGateTP1Class = "room_gate_tp1"

// roomVerdict is the pure room judgement for ONE scenario.
type roomVerdict struct {
	Known    bool    // the economics state an obstacle and a nonzero risk
	R        float64 // r_to_obstacle (machine-computed)
	Obstacle float64 // the first obstacle price
	Label    string  // the obstacle's level label
	Entry    float64
	Risk     float64 // |entry − stop|
	Short    bool    // r < min
}

// roomVerdictFor reads r_to_obstacle through kernel.EconomicsFor — the SAME
// computation the parser's economics check and the scenario desk line use
// (canon 53), from the arm's authored entry and stop.
func roomVerdictFor(sc kernel.PlanScenario, minR float64) roomVerdict {
	v := kernel.EconomicsFor(sc)
	out := roomVerdict{}
	if v.ObstacleR == nil || v.Geometry == nil || sc.Economics == nil || sc.Economics.FirstObstacle == nil || sc.Economics.FirstObstacle.Price == nil {
		return out
	}
	out.Known = true
	out.R = *v.ObstacleR
	out.Obstacle = *sc.Economics.FirstObstacle.Price
	out.Label = sc.Economics.FirstObstacle.Level
	out.Entry = v.Geometry.Entry
	out.Risk = math.Abs(v.Geometry.Entry - v.Geometry.Stop)
	out.Short = out.R+1e-9 < minR
	return out
}

// roomGateText is the refusal / hint text (pure — fixture-pinned).
func roomGateText(sc kernel.PlanScenario, v roomVerdict, minR float64) string {
	if !v.Known {
		return fmt.Sprintf("room gate: %s states no first obstacle with a nonzero risk — r_to_obstacle UNKNOWN, refused (planner_room_gate on)", sc.ID)
	}
	return fmt.Sprintf("room gate: r_to_obstacle %.2f < planner_room_min_r %.2f (first obstacle %s %.2f is %.2f pts from entry %.2f, risk %.2f pts) — the trade stalls at that level before it pays 1R; author an entry with room, or a stop/target where the first obstacle sits ≥ %.2fR away",
		v.R, minR, v.Label, v.Obstacle, math.Abs(v.Obstacle-v.Entry), v.Entry, v.Risk, minR)
}

// countPlannerKnob records one refusal/action of a release-#25 switch: the
// in-memory gate-block table and a recorded system counter.
func (at *AutoTrader) countPlannerKnob(gate, tradeDate, session string) {
	telemetry.IncGateBlock(at.id, gate)
	if at.store == nil {
		return
	}
	key := fmt.Sprintf("planner_knob:%s:%s:%s:%s", gate, at.id, tradeDate, session)
	if _, err := store.IncSystemCounter(at.store, key); err != nil {
		at.logWarnf("🧱 %s counter write failed: %v", gate, err)
	}
}

// roomGateAtWrite runs the room gate over every enabled arm of a doc at the
// plan write site, BEFORE the feasibility verdicts (so a tp1_at_obstacle move
// is judged by the existing R:R floor). OFF → nil and the doc is untouched.
func (at *AutoTrader) roomGateAtWrite(d *kernel.PlanDoc, tradeDate, session string) []writeTimeFeasibilityIssue {
	mode, _ := store.ResolvePlannerRoomGate(at.dayPlanCfg())
	if mode == store.RoomGateOff || d == nil {
		return nil
	}
	minR, _ := store.ResolvePlannerRoomMinR(at.dayPlanCfg())
	tick := market.FuturesTickSize(at.futuresSymbol())
	var out []writeTimeFeasibilityIssue
	for i := range d.Scenarios {
		sc := &d.Scenarios[i]
		if sc.Arm == nil || !sc.Arm.Enabled {
			continue
		}
		v := roomVerdictFor(*sc, minR)
		if v.Known && !v.Short {
			continue
		}
		if mode == store.RoomGateTP1AtObstacle && v.Known {
			from := moveArmTargetToObstacle(sc, v.Obstacle, tick)
			at.logWarnf("🧱 room gate: %s %s TP1 moved %.2f → first obstacle %s %.2f (r_to_obstacle %.2f < %.2f) — the R:R floor judges the new target",
				session, sc.ID, from, v.Label, v.Obstacle, v.R, minR)
			at.countPlannerKnob(RoomGateTP1Class, tradeDate, session)
			continue
		}
		text := roomGateText(*sc, v, minR)
		at.logWarnf("🧱 room gate REFUSED %s %s: %s", session, sc.ID, text)
		at.countPlannerKnob(RoomGateClass, tradeDate, session)
		out = append(out, writeTimeFeasibilityIssue{Scenario: sc.ID, Class: RoomGateClass, Kind: "room", Cond: sc.Condition, Verbose: text})
	}
	return out
}

// moveArmTargetToObstacle moves the arm's first target (leg 1 on a split, the
// single target otherwise) onto the first obstacle, tick-rounded, and keeps
// the stated r_to_arm_target equal to the new geometry. Returns the old target.
func moveArmTargetToObstacle(sc *kernel.PlanScenario, obstacle, tick float64) float64 {
	px := obstacle
	if tick > 0 {
		px = math.Round(obstacle/tick) * tick
	}
	if len(sc.Arm.Legs) > 0 {
		from := sc.Arm.Legs[0].Target
		sc.Arm.Legs[0].Target = px
		return from
	}
	from := sc.Arm.Target
	sc.Arm.Target = px
	if sc.Economics != nil {
		if r := kernel.EconomicsFor(*sc).ArmR; r != nil {
			v := *r
			sc.Economics.RToArmTarget = &v
		}
	}
	return from
}

// firstVerdict returns the first non-empty refusal verdict (release #25: the
// PK-4 cap verdict ahead of the existing arm gate chain).
func firstVerdict(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// noteATRFloorCapAtWrite logs + counts one PK-4 refusal judged at plan write
// (the arm path records its own through the arm-refusal counter).
func (at *AutoTrader) noteATRFloorCapAtWrite(session, scenario, why string) {
	at.logWarnf("🛑 arm REFUSED at write %s %s: %s", session, scenario, why)
	telemetry.IncGateBlock(at.id, ATRFloorBeyondInvalidationClass)
}
