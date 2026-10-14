package kernel

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// A3 — MIN-SL VALIDATION constants + resolvers (2026-08-26).
//
// Research grounding (week ledger, pnl_corrected): 15 of 27 losers were
// STOPPED-TOO-TIGHT — they printed MFE ≥ 0.5×SL before the stop-out (e.g.
// 08-25 02:09 MFE +58.0 → −62.0); the 5 biggest losers all stopped 5–44 pts
// from ANY seated level. The fix: a minimum stop width anchored to the real
// 5m ATR plus level clearance, refusing tight stops before they can execute.

// MinSLATRMultDefault is the shipped minimum stop width in 5m ATR(14) units.
//
// 0B (owner ruling 2026-09-02): 1.0 → 1.5. The 1.0 was [C] code-canon with no
// citation (knob census). Round-7 research tests the day-trade range at
// 1.5–2.5×ATR and finds stop-out rates above 60% on noise alone below 1.0×;
// our own tape has 6 of 8 losers printing MAE beyond the stop and 15 of 27
// losers stopped-too-tight. 1.5 is the bottom of the researched range, not the
// middle — the deliberate first step from an uncited number to a cited one.
//
// This value is read by THREE gates: the arm-time gate
// (trader/armed_executor.go), the AI-entry decision gate
// (kernel/engine_position.go) and the planner's authoring WARNING
// (trader/auto_trader_planner.go, ArmFeasibilityWarnings). Raising it tightens
// all three at once — intended: the same floor everywhere is the point.
//
// Env MIN_SL_ATR_MULT overrides; 0 disables the gate entirely.
const MinSLATRMultDefault = 1.5

// MinSLTickClearance is the level-clearance leg: a cited-scenario stop must
// sit at least this many ticks BEYOND the anchor level/zone far edge. Stops
// parked exactly at a level get run by the sweep; 2 ticks of clearance puts
// the stop on the far side of the resting orders.
const MinSLTickClearance = 2

// MinSLATRMult resolves the min-SL ATR multiplier (env MIN_SL_ATR_MULT,
// default 1.5; 0 = gate off).
func MinSLATRMult() float64 {
	if v := os.Getenv("MIN_SL_ATR_MULT"); v != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && f >= 0 {
			return f
		}
	}
	return MinSLATRMultDefault
}

// MinSLVerdict is the pure ATR-leg verdict: (blocked, refusal message).
// dist = |entry − SL|. mult ≤ 0 (off) / atr ≤ 0 (no ATR) / dist ≤ 0 (no entry
// reference) all FAIL OPEN — the gate never blocks on missing inputs.
func MinSLVerdict(action string, dist, atr, mult float64) (bool, string) {
	if action != "open_long" && action != "open_short" {
		return false, ""
	}
	if mult <= 0 || atr <= 0 || dist <= 0 {
		return false, ""
	}
	if dist < mult*atr {
		return true, fmt.Sprintf("sl_too_tight: %.1f < %.1f×ATR (%.1f) — widen or skip", dist, mult, atr)
	}
	return false, ""
}

// MinSLAnchorFor resolves the cited scenario's anchor level price for the
// clearance leg. Fail-open: no citation / no active plan / unevaluable anchor
// → ok=false (the ATR leg still applies).
func MinSLAnchorFor(ctx *Context, d *Decision) (float64, bool) {
	if ctx == nil || d == nil {
		return 0, false
	}
	c := strings.ToUpper(strings.TrimSpace(d.CitedScenario))
	if c == "" || c == "OFF-PLAN" {
		return 0, false
	}
	ap := ActivePlanFor(ctx.TraderID, d.Symbol)
	if ap == nil {
		return 0, false
	}
	for _, s := range ap.Doc.Scenarios {
		if !strings.EqualFold(s.ID, c) {
			continue
		}
		if anchor, ok := ScenarioAnchor(s, ap.Doc.Levels); ok {
			return anchor, true
		}
		return 0, false
	}
	return 0, false
}
