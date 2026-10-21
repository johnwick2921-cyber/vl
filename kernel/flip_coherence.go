package kernel

import (
	"fmt"
	"math"
	"strings"

	"vl/market"
)

// R22 F3 — FLIP-LINE COHERENCE (2026-10-14).
//
// Nothing related a scenario's ENTRY to its own plan's flip line. Class 148
// judged the flip line against PRICE; nothing judged it against the plan's own
// arms. Since 09-24, 79 of 231 armed scenarios (34%) had their entry on or past
// the flip line (48 strictly past, 31 exactly on it); trade #649 (v2 S2) was a
// SHORT entered at 30843.50 above the plan's "5m close above 30828.75 → long"
// line, and the plan went dormant 7 minutes after the fill.
//
// The relation: an ARMED scenario whose direction is the OPPOSITE of the flip's
// flip_to, with an entry STRICTLY BEYOND the flip price on the flip side (a short
// entered above an "above → long" line; a long entered below a "below → short"
// line), can only fill where the plan is about to flip against it. Such an arm
// is written DISABLED (arm_disabled_reason FlipIncoherentClass). An entry
// exactly AT the line is coherent: the fill is the test of the line, and a
// close beyond it is the invalidation.

// FlipCoherenceSentence is the planner-prompt sentence for the relation.
const FlipCoherenceSentence = "FLIP COHERENCE: an armed scenario must not enter PAST your own flip line against the flip direction — a short entered ABOVE a 'close above → long' line, or a long entered BELOW a 'close below → short' line, is written with its arm DISABLED (arm_disabled_reason flip_incoherent), because it can only fill where your plan is about to flip against it; an entry exactly AT the line is allowed. "

// FlipIncoherentClass is the arm_disabled_reason / counter class.
const FlipIncoherentClass = "flip_incoherent"

// FlipIncoherence is one armed scenario whose entry sits past its own plan's
// flip line, against the flip direction.
type FlipIncoherence struct {
	Scenario  string
	Direction string
	Entry     float64
	FlipPrice float64
	FlipSide  string
	FlipTo    string
}

// String is the verbose reason (log / plan view detail).
func (f FlipIncoherence) String() string {
	return fmt.Sprintf("%s %s entry %.2f sits %s the plan's own flip line %.2f (close %s → bias %s): a fill there means the plan is about to flip against the trade",
		f.Scenario, f.Direction, f.Entry, f.FlipSide, f.FlipPrice, f.FlipSide, f.FlipTo)
}

// FlipIncoherentScenarios returns every enabled arm whose entry (or any leg
// entry) is strictly past the plan's structured flip line, against the flip
// direction. No structured flip, an unknown side, or a non long/short scenario
// → nothing (the relation is undefined, never guessed).
func FlipIncoherentScenarios(d *PlanDoc) []FlipIncoherence {
	if d == nil || d.FlipStructured == nil || d.FlipStructured.Price <= 0 {
		return nil
	}
	f := d.FlipStructured
	side := strings.ToLower(strings.TrimSpace(f.Side))
	if side != "above" && side != "below" {
		return nil
	}
	to := strings.ToLower(strings.TrimSpace(f.FlipTo))
	if to == "" { // the flip's natural direction: a close above flips long, below flips short
		to = map[string]string{"above": "long", "below": "short"}[side]
	}
	var out []FlipIncoherence
	for _, sc := range d.Scenarios {
		if sc.Arm == nil || !sc.Arm.Enabled {
			continue
		}
		dir := strings.ToLower(strings.TrimSpace(sc.Direction))
		if (dir != "long" && dir != "short") || dir == to {
			continue
		}
		entries := []float64{sc.Arm.Entry}
		for _, l := range sc.Arm.Legs {
			entries = append(entries, l.Entry)
		}
		// N6 (rescan 10-14, planner-touch-fills): a market_in_zone limit goes on
		// the wire at the zone's tick-rounded FAR edge (long → zone high rounded
		// down, short → zone low rounded up), not at the authored entry. #650's
		// authored 30798.92 sat ON the flip line "below 30798.92", but the wire
		// limit was 30798.75 — strictly past it.
		if e, ok := zoneWireEdge(sc, dir); ok {
			entries = append(entries, e)
		}
		for _, e := range entries {
			if e <= 0 {
				continue
			}
			if (side == "above" && e > f.Price) || (side == "below" && e < f.Price) {
				out = append(out, FlipIncoherence{Scenario: sc.ID, Direction: dir, Entry: e, FlipPrice: f.Price, FlipSide: side, FlipTo: to})
				break
			}
		}
	}
	return out
}

// DisableFlipIncoherentArms writes each incoherent arm disabled with the class
// as its reason. Returns the issues it applied (those whose arm was enabled).
func DisableFlipIncoherentArms(d *PlanDoc, issues []FlipIncoherence) []FlipIncoherence {
	if d == nil || len(issues) == 0 {
		return nil
	}
	by := make(map[string]bool, len(issues))
	for _, is := range issues {
		by[is.Scenario] = true
	}
	var applied []FlipIncoherence
	for i := range d.Scenarios {
		sc := &d.Scenarios[i]
		if sc.Arm == nil || !sc.Arm.Enabled || !by[sc.ID] {
			continue
		}
		sc.Arm.Enabled = false
		sc.Arm.DisabledReason = FlipIncoherentClass
		for _, is := range issues {
			if is.Scenario == sc.ID {
				applied = append(applied, is)
				break
			}
		}
	}
	return applied
}

// zoneWireEdge is the price a market_in_zone arm's limit is SENT at: the far
// edge of economics.entry_zone rounded inward to the instrument tick (the
// composer's own rounding, trader/zone_placement.go). ok=false for any other
// policy or a missing zone.
func zoneWireEdge(sc PlanScenario, dir string) (float64, bool) {
	inZone := EffectiveArmPolicy(sc.Arm, nil) == EntryPolicyMarketInZone
	for i := range sc.Arm.Legs {
		if EffectiveArmPolicy(sc.Arm, &sc.Arm.Legs[i]) == EntryPolicyMarketInZone {
			inZone = true
		}
	}
	if !inZone || sc.Economics == nil || len(sc.Economics.EntryZone) != 2 {
		return 0, false
	}
	lo, hi := math.Min(sc.Economics.EntryZone[0], sc.Economics.EntryZone[1]), math.Max(sc.Economics.EntryZone[0], sc.Economics.EntryZone[1])
	if lo <= 0 {
		return 0, false
	}
	tick := market.FuturesTickSize("MNQ")
	if tick <= 0 {
		tick = 0.25
	}
	const eps = 1e-9
	switch dir {
	case "long":
		return math.Floor(hi/tick+eps) * tick, true
	case "short":
		return math.Ceil(lo/tick-eps) * tick, true
	}
	return 0, false
}
