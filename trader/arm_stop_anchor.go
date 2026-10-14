package trader

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// ── 0B (2026-09-02) — STOP ANCHORED TO SEATED STRUCTURE ──────────────────────
//
// Historical rule below; superseded for reject fades by frozen structural
// context. Other entry plays retain this construction and its telemetry.
//
// Week-in-review evidence: 15 of 27 losers printed stopped-too-tight, and on
// the five biggest losers 0 of 5 stops sat ON a seated level while 2 of 5 sat
// in dead zones 40+ points away. A wider stop in a dead zone is still a stop in
// a dead zone: width alone is not the fix, ANCHORING is.
//
// The rule (owner ruling): stop = BEYOND the nearest seated level on the risk
// side + tick clearance, then floored at MIN_SL_ATR_MULT×ATR5m — WHICHEVER IS
// WIDER WINS. The authored stop is a third floor: this composition never
// TIGHTENS what the planner wrote, it only widens. When no seated level sits
// within a sane range on the risk side, the arm is `stop_unanchored` and the
// ATR floor governs — a level is never invented.

// ArmStopAnchorMaxATRDefault bounds "nearest seated level" in ATR5m units: a
// level further than this on the risk side is a DEAD ZONE, not an anchor
// (2 of the 5 biggest losers stopped 40+ points from anything). 3.0×ATR5m at a
// ~16-point ATR is ~48 points — beyond that, anchoring would widen the stop
// past any sane R:R and the ATR floor is the honest answer.
//
// CHOSEN DEFAULT, NOT AN OWNER RULING: flagged in the 0B report for a ruling.
// Env ARM_STOP_ANCHOR_MAX_ATR overrides; 0 disables anchoring entirely.
const ArmStopAnchorMaxATRDefault = 3.0

// armStopAnchorMaxATR resolves the dead-zone bound (env ARM_STOP_ANCHOR_MAX_ATR).
func armStopAnchorMaxATR() float64 {
	if v := os.Getenv("ARM_STOP_ANCHOR_MAX_ATR"); v != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && f >= 0 {
			return f
		}
	}
	return ArmStopAnchorMaxATRDefault
}

// StopComposition is one arm's stop decision, fully explained: every field the
// per-arm log line prints, so the choice can be audited from the journal alone.
type StopComposition struct {
	Geometry     *store.StructuralGeometryRecord
	Stop         float64 // the chosen stop
	Authored     float64 // what the planner wrote
	AnchorPrice  float64 // the seated level the stop sits beyond (0 = none)
	AnchorLabel  string  // that level's provenance chip
	AnchorStop   float64 // anchor ± clearance (0 = none)
	ATRFloorStop float64 // entry ∓ mult×ATR5m (0 = no ATR)
	Bound        string  // anchor | atr_floor | authored — which one won
	Unanchored   bool    // no seated level within the dead-zone bound
}

// composeArmStop is the pure stop composition (fixture-tested).
//
//	side      long|short
//	entry     the arm's entry price
//	authored  the planner's stop (never tightened)
//	atr5m     ATR(14) on 5m; ≤0 → the ATR leg is skipped (fail-open)
//	tick      instrument tick size
//	levels    the plan's seated levels
//	mult      MIN_SL_ATR_MULT (resolved)
//	clearTicks the level-clearance leg (MinSLTickClearance)
//	maxAnchorATR the dead-zone bound in ATR units; ≤0 disables anchoring
func composeArmStop(side string, entry, authored, atr5m, tick float64, levels []kernel.PlanLevel, mult float64, clearTicks int, maxAnchorATR float64, structural ...armStructuralContext) StopComposition {
	// Reject-fade production callers supply frozen structural context. Other
	// entry plays and historical benchmark fixtures retain the legacy branch.
	if len(structural) == 1 {
		in := structural[0]
		r := ComposeLevelFadeGeometryWith(in.Doc, in.Scenario, in.Leg, in.Policy, atr5m, tick, in.PointValue, in.GeometryRefIDs)
		c := StopComposition{Authored: authored, Bound: r.StopSource, Geometry: &r, Unanchored: r.StopSource == "atr_fallback"}
		if r.Stop != nil {
			c.Stop = *r.Stop
		}
		return c
	}
	c := StopComposition{Stop: authored, Authored: authored, Bound: "authored"}
	long := strings.EqualFold(strings.TrimSpace(side), "long")
	if entry <= 0 || authored <= 0 {
		return c // no usable geometry — leave the authored stop untouched
	}
	if tick <= 0 {
		tick = 0.25
	}
	clearance := float64(clearTicks) * tick

	// ATR floor leg.
	if atr5m > 0 && mult > 0 {
		if long {
			c.ATRFloorStop = entry - mult*atr5m
		} else {
			c.ATRFloorStop = entry + mult*atr5m
		}
	}

	// Anchor leg: the NEAREST seated level on the RISK side (below entry for a
	// long, above for a short), within the dead-zone bound.
	if maxAnchorATR > 0 {
		bound := math.MaxFloat64
		if atr5m > 0 {
			bound = maxAnchorATR * atr5m
		}
		best, bestDist, found := 0.0, math.MaxFloat64, false
		for _, l := range levels {
			if l.Price <= 0 {
				continue
			}
			var dist float64
			if long {
				if l.Price >= entry {
					continue // not on the risk side
				}
				dist = entry - l.Price
			} else {
				if l.Price <= entry {
					continue
				}
				dist = l.Price - entry
			}
			if dist > bound {
				continue // dead zone
			}
			if dist < bestDist {
				best, bestDist, found = l.Price, dist, true
				c.AnchorLabel = l.Label
			}
		}
		if found {
			c.AnchorPrice = best
			if long {
				c.AnchorStop = best - clearance
			} else {
				c.AnchorStop = best + clearance
			}
		} else {
			c.Unanchored = true
			c.AnchorLabel = ""
		}
	} else {
		c.Unanchored = true
	}

	// WIDEST WINS. For a long a wider stop is LOWER; for a short, HIGHER.
	pick := func(cand float64, name string) {
		if cand <= 0 {
			return
		}
		if (long && cand < c.Stop) || (!long && cand > c.Stop) {
			c.Stop, c.Bound = cand, name
		}
	}
	pick(c.AnchorStop, "anchor")
	pick(c.ATRFloorStop, "atr_floor")
	return c
}

// armStopCompositionLine is the per-arm log line (D2): chosen stop, the anchor
// it sits beyond, the ATR floor, and which leg bound. Pure, fixture-tested.
//
// R22 P2 (PLANNER.md F9): a WIDENED stop now prints by how much — points past
// the authored stop and the one-contract dollars (pointValue; ≤0 or absent →
// "$ n/a"). The floor itself is unchanged (the owner's open question).
func armStopCompositionLine(session, scenario string, leg int, side string, c StopComposition, atr5m, mult float64, pointValue ...float64) string {
	anchor := "none (stop_unanchored)"
	if !c.Unanchored && c.AnchorPrice > 0 {
		label := c.AnchorLabel
		if strings.TrimSpace(label) == "" {
			label = "unlabelled"
		}
		anchor = fmt.Sprintf("%s %.2f → beyond %.2f", label, c.AnchorPrice, c.AnchorStop)
	}
	floor := "n/a (no ATR)"
	if c.ATRFloorStop > 0 {
		floor = fmt.Sprintf("%.2f (%.1f×ATR5m %.2f)", c.ATRFloorStop, mult, atr5m)
	}
	moved := ""
	if math.Abs(c.Stop-c.Authored) > 1e-9 {
		pts := math.Abs(c.Stop - c.Authored)
		usd := "$ n/a"
		if len(pointValue) > 0 && pointValue[0] > 0 {
			usd = fmt.Sprintf("$%.2f/contract", pts*pointValue[0])
		}
		moved = fmt.Sprintf(" (authored %.2f WIDENED +%.2f pts = %s past the authored stop)", c.Authored, pts, usd)
	}
	return fmt.Sprintf("🛑 arm stop %s %s leg %d %s: stop %.2f%s · anchor %s · atr_floor %s · bound=%s",
		session, scenario, leg, side, c.Stop, moved, anchor, floor, c.Bound)
}

// logArmStopComposition (R22 P2, PLANNER.md F9) logs the composition line with
// the widening in points and one-contract dollars, and RECORDS each widened
// arm-spec ONCE (plan, version, scenario, leg) in
// arm_stop_widened:<trader>:<date>:<session> — the composition line itself
// re-logs whenever the composed stop moves; the counter counts arms, not ATR
// ticks. #649: authored 30873.00 → 30882.78 = +9.78 pts = $19.55/contract.
func (at *AutoTrader) logArmStopComposition(plan *kernel.ActivePlan, sc kernel.PlanScenario, li int, c StopComposition, atr5m float64) {
	line := armStopCompositionLine(plan.Session, sc.ID, li+1, sc.Direction, c, atr5m, kernel.MinSLATRMult(), market.FuturesPointValue(at.futuresSymbol()))
	if math.Abs(c.Stop-c.Authored) > 1e-9 && c.Authored > 0 && at.store != nil {
		key := plan.PlanID + ":" + strconv.Itoa(plan.Version) + ":" + sc.ID + ":leg" + strconv.Itoa(li+1) + ":widened"
		if armRefusalChanged(&at.armStopCompLast, key, "1") {
			ckey := fmt.Sprintf("arm_stop_widened:%s:%s:%s", at.id, kernel.PlanTradeDateFor(plan), plan.Session)
			if n, err := store.IncSystemCounter(at.store, ckey); err != nil {
				at.logWarnf("🛑 arm-stop-widened counter write failed: %v", err)
			} else {
				line += fmt.Sprintf(" · widened arms this session: %d (recorded)", n)
			}
		}
	}
	at.logInfof("%s", line)
}
