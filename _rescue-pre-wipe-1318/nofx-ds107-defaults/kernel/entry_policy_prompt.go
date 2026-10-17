package kernel

import (
	"fmt"
	"sort"
	"strings"

	"nofx/store"
)

// W-EXEC-TRUTH W3 (h) — the planner prompt follows the RESOLVED
// day_plan.entry_policy_default.
//
//	""               → the SHIPPED default (market_in_zone, R4): the zero
//	                   PlannerInput renders what production renders when the
//	                   strategy saved nothing.
//	market_in_zone   → the ENTRY POLICY sentence; the two sentences the policy
//	                   makes false ("a RESTING ORDER IS THE ONLY WAY INTO THE
//	                   MARKET", "entry_mode=immediate is AI-path ONLY") are gone
//	                   and their siblings say the new truth.
//	planned_order    → the legacy text plus one planned_order sentence (the
//	                   policy only ever stamps where legacy already arms).
//	legacy           → the pre-W3 prompt BYTE-IDENTICAL (the explicit off).
//
// Every segment below returns the legacy text VERBATIM for any policy other
// than market_in_zone, so the legacy golden is a byte-identity proof.

// entryPolicyPromptInput is the resolved policy + its two numeric knobs.
type entryPolicyPromptInput struct {
	Policy     string // resolved: market_in_zone | planned_order | legacy
	ZoneMaxPts float64
	MinHoldMin int
}

// resolvePromptEntryPolicy maps the PlannerInput fields onto the rendered
// policy: an empty policy is the shipped default; zero knobs are their
// shipped defaults (store.*Default — one source with the resolvers).
func resolvePromptEntryPolicy(policy string, zoneMax float64, minHold int) entryPolicyPromptInput {
	p := strings.ToLower(strings.TrimSpace(policy))
	if p == "" {
		p = store.EntryPolicyDefaultShipped
	}
	if zoneMax <= 0 {
		zoneMax = store.ZoneMaxPtsDefault
	}
	if minHold <= 0 {
		minHold = store.MinHoldMinDefault
	}
	return entryPolicyPromptInput{Policy: p, ZoneMaxPts: zoneMax, MinHoldMin: minHold}
}

func (e entryPolicyPromptInput) miz() bool { return e.Policy == EntryPolicyMarketInZone }

// EntryPolicyPromptMarker is the phrase that opens the market_in_zone ENTRY
// POLICY sentence. The class-38 ENTRY POLICY row is gated on it, and the
// legacy rows it replaces are suspended by it (PromptContract.Unless).
const EntryPolicyPromptMarker = "ENTRY POLICY (market_in_zone"

// entryPolicySentence is the ONE sentence describing the resolved policy.
func (e entryPolicyPromptInput) entryPolicySentence() string {
	switch e.Policy {
	case EntryPolicyMarketInZone:
		return EntryPolicyPromptMarker + " — the machine stamps it on every arm you author): the entry is a LIMIT at the FAR edge of your economics.entry_zone (a long buys at the zone HIGH, a short sells at the zone LOW), so it fills at once inside the zone and never beyond it; every condition is armable, and a non-touch confirm (1x5m_close, 2x5m_close, 1m_mss, time_hold) must carry wait_confirm:true; the zone must contain arm.entry, lie on the permitted side of the confirm ref (a touch zone contains the ref, any other zone lies wholly on confirm.side of it), be at most " +
			fmt.Sprintf("%g", e.ZoneMaxPts) + " points wide (day_plan.zone_max_pts), and leave the stop and the target OUTSIDE it; R:R is judged at the far edge and the stop distance at the near edge; an armed time_hold holds at least " +
			fmt.Sprintf("%d", e.MinHoldMin) + " minutes (day_plan.min_hold_min); a breakdown/breakup arm takes entry_mode=pullback or entry_mode=immediate; write \"policy\": \"planned_order\" on a reject / fvg_entry / sweep_reclaim leg-1 arm to rest at your exact entry instead. "
	case EntryPolicyPlannedOrder:
		return "ENTRY POLICY (planned_order — the machine stamps it on reject, fvg_entry and single sweep_reclaim arms): the arm rests as a LIMIT at your EXACT entry; every other condition keeps the rules below unchanged. "
	}
	return ""
}

// armLegalClause is the schema line's arm{} legality clause.
func (e entryPolicyPromptInput) armLegalClause() string {
	if e.miz() {
		return fmt.Sprintf("arm{} is OPTIONAL and legal ONLY on %s (every condition under the ENTRY POLICY below; a non-touch confirm arms with wait_confirm:true)", armableUnderPolicyPipe(EntryPolicyMarketInZone))
	}
	return fmt.Sprintf("arm{} is OPTIONAL and legal ONLY on %s (sweep_reclaim arms only via wait_confirm; %s NEVER arm)", ArmableConditionsPipe(), NonArmableConditionsPipe())
}

// armSingleClause is the tail of ARM SPLIT vs ARM SINGLE (the single-arm
// condition list + the waterfall entry_mode).
func (e entryPolicyPromptInput) armSingleClause() string {
	if e.miz() {
		var single []string
		for _, c := range KnownConditions() {
			if c != "sweep_reclaim" && ArmableConditionFor(c, EntryPolicyMarketInZone) {
				single = append(single, c)
			}
		}
		return "EVERY other condition — " + strings.Join(single, ", ") + " — must arm SINGLE: arm{} and no legs, with wait_confirm:true whenever its confirm is not a touch. A breakdown/breakup arm additionally needs breakdown{} with entry_mode=pullback or entry_mode=immediate. "
	}
	return "EVERY other condition — breakdown_continue, breakup_continue, reject, fvg_entry — must arm SINGLE: arm{} with wait_confirm:true and no legs. A breakdown/breakup arm additionally needs breakdown{} with entry_mode=pullback. "
}

// armsFollowBias is ARMS FOLLOW THE BIAS — under market_in_zone without the
// deleted sentence (the entry is a limit inside the zone, not a resting order
// at a fixed price), preceded by the ENTRY POLICY sentence.
func (e entryPolicyPromptInput) armsFollowBias() string {
	const rest = "Every scenario in the plan's bias direction that has a concrete trigger price MUST carry an arm. A long plan with no long arm is invalid; a short plan with no short arm is invalid, for the same reason and in the same words. If you cannot arm your own bias direction, say so in reasoning and state a NEUTRAL bias rather than arguing for a direction you have left no way to take. "
	if e.miz() {
		return e.entryPolicySentence() + "ARMS FOLLOW THE BIAS: " + rest
	}
	return e.entryPolicySentence() + "ARMS FOLLOW THE BIAS: with the decision path closed, a RESTING ORDER IS THE ONLY WAY INTO THE MARKET — a scenario with no arm cannot trade, however well argued. " + rest
}

// entryTypeSentence is ENTRY TYPE FOLLOWS THE CONDITION.
func (e entryPolicyPromptInput) entryTypeSentence() string {
	if e.miz() {
		return "ENTRY TYPE under market_in_zone: EVERY arm is a LIMIT at the far edge of its entry_zone — never author kind stop_entry (the machine derives the type and REFUSES a contradiction); a planned_order arm keeps its condition's type (reject→limit, fvg_entry→limit, sweep_reclaim→limit). "
	}
	return "ENTRY TYPE FOLLOWS THE CONDITION (the machine derives it and REFUSES a contradiction): a play that rests AT a price is a limit (reject→limit, fvg_entry→limit, sweep_reclaim→limit); a play that is only valid once price travels BEYOND its trigger is a stop entry (reclaim→stop_entry — a BUY STOP above the reclaim level for a long, a SELL STOP below it for a short). A waterfall (breakup_continue→limit / breakdown_continue→limit) rests as a PULLBACK limit AT the broken level and chains on confirm leg 1 — do not author a stop entry for it. "
}

// The four clauses of the "Every armed scenario …" sentence that the policy
// changes. Each returns its legacy text verbatim off the policy.
func (e entryPolicyPromptInput) breakoutRetestClause() string {
	if e.miz() {
		return "(every condition arms under the ENTRY POLICY; a SHADOWED condition — see the list above — is recorded, never placed)"
	}
	return "(breakout_retest stays a normal AI play: the machine never arms it — GAR-F4)"
}

func (e entryPolicyPromptInput) neverArmClause() string {
	if e.miz() {
		return "NEVER arm a non-touch confirm (acceptance, hold, reclaim, a waterfall, a raw sweep) WITHOUT the wait_confirm chain."
	}
	return "NEVER arm acceptance or a raw sweep WITHOUT the wait_confirm chain."
}

func (e entryPolicyPromptInput) placementClause() string {
	if e.miz() {
		return "The system places a market_in_zone limit once price is inside or beyond its zone (short of the zone it waits), cancels a limit that rests longer than day_plan.zone_rest_max_min, and cancels on veto/dormant/session-end."
	}
	return "The system places arms within a tick band, manages them tick-level, and cancels on veto/dormant/session-end."
}

func (e entryPolicyPromptInput) omitArmClause() string {
	if e.miz() {
		return "If your setup cannot meet BOTH, OMIT arm{} — an unarmed scenario reaches the market only through the AI path, which plan_mode=strict closes."
	}
	return "If your setup cannot meet BOTH, OMIT arm{} and let the AI path take it."
}

func (e entryPolicyPromptInput) waterfallArmsClause() string {
	if e.miz() {
		return "WATERFALL ARMS (F1): a breakdown_continue / breakup_continue at quality A or B SHOULD carry arm{} with wait_confirm:true — entry_mode=pullback puts the entry_zone AT the broken level (the pullback-that-fails fills it), entry_mode=immediate puts it at the confirming close (the limit at its far edge fills on the close). "
	}
	return "WATERFALL ARMS (F1): a breakdown_continue / breakup_continue at quality A or B SHOULD carry arm{} with wait_confirm:true + entry_mode=pullback — the resting limit sits AT the broken level and chains on confirm leg 1, so the pullback-that-fails FILLS it (immediate-mode waterfall plays stay on the AI path). "
}

// waterfallEntryModes is the entry_mode half of WATERFALL PLAY.
// n is the BD_MIN_CLOSES authoring default, rendered by the caller
// (planner_prompt.go — the one prompt site allowed to read it, confirm
// resolver source-scan pin).
func (e entryPolicyPromptInput) waterfallEntryModes(n string) string {
	if e.miz() {
		return "entry_mode=immediate ARMS under the ENTRY POLICY: the entry_zone sits at the CONFIRMING close (BD_MIN_CLOSES, default " + n + ") and the limit at its far edge is placed once leg 1 is MET, through the FULL gate chain — CHOOSE immediate for no-retest waterfalls (one-sided delivery, displacement EXPANDING, price running away from the level): SL beyond the pullback extreme, target at the next liquidity pool. entry_mode=pullback: the entry_zone sits AT the broken level (chains on leg 1 — the pullback-that-fails FILLS it) — CHOOSE pullback when a retest is likely. "
	}
	return "entry_mode=immediate is AI-path ONLY (no arm; the machine rejects immediate arms): the market entry fires on the CONFIRMING close (BD_MIN_CLOSES, default " + n + ") and runs the FULL gate chain (min-SL ≥ 1.0×ATR5m, R:R ≥ min_risk_reward_ratio, min-conf, HTF veto) — CHOOSE immediate for no-retest waterfalls (one-sided delivery, displacement EXPANDING, price running away from the level): SL beyond the pullback extreme, target at the next liquidity pool. entry_mode=pullback is the ARM path (resting limit AT the broken level, chains on leg 1 — the pullback-that-fails FILLS it) — CHOOSE pullback when a retest is likely. "
}

// ArmableConditionsLineFor is ArmableConditionsLine under an entry policy:
// under market_in_zone every known condition is armable (a limit) — nothing is
// called "not armable"; shadowed conditions are still named. Any other policy
// renders ArmableConditionsLine exactly.
func ArmableConditionsLineFor(statuses map[string]string, policy string) string {
	if policy != EntryPolicyMarketInZone {
		return ArmableConditionsLine(statuses)
	}
	var live, shadowed []string
	for _, c := range KnownConditions() {
		if !ArmableConditionFor(c, policy) {
			continue
		}
		if statuses[strings.ToLower(strings.TrimSpace(c))] == ConditionShadow {
			shadowed = append(shadowed, c)
			continue
		}
		live = append(live, fmt.Sprintf("%s→%s", c, ArmKindForPolicy(c, policy)))
	}
	sort.Strings(live)
	sort.Strings(shadowed)
	parts := []string{"ARMABLE + LIVE under market_in_zone (every condition arms as a limit inside its entry_zone): " + strings.Join(live, " · ")}
	if len(shadowed) > 0 {
		parts = append(parts, "armable but SHADOWED — do not arm these: "+strings.Join(shadowed, ", "))
	}
	return strings.Join(parts, ". ") + "."
}

// BiasArmWarningFor is BiasArmWarning under the plan's entry policy: under
// market_in_zone an unarmed in-bias acceptance/hold/breakout_retest is
// "armable but carries no arm", never "un-armable". Any other policy is
// BiasArmWarning exactly.
func BiasArmWarningFor(d *PlanDoc, statuses map[string]string, policy string) string {
	if policy != EntryPolicyMarketInZone {
		return BiasArmWarning(d, statuses)
	}
	if d == nil {
		return ""
	}
	bias := strings.ToLower(strings.TrimSpace(d.Bias.Direction))
	if bias != "long" && bias != "short" {
		return ""
	}
	var blockers []string
	armedInBias, armedOther := 0, 0
	for _, sc := range d.Scenarios {
		inBias := strings.EqualFold(strings.TrimSpace(sc.Direction), bias)
		armed := sc.Arm != nil && sc.Arm.Enabled
		switch {
		case inBias && armed:
			armedInBias++
		case !inBias && armed:
			armedOther++
		case inBias:
			cond := strings.ToLower(strings.TrimSpace(sc.Condition))
			switch {
			case !ArmableConditionFor(cond, policy):
				blockers = append(blockers, fmt.Sprintf("%s %s is un-armable", sc.ID, cond))
			case statuses[cond] == ConditionShadow:
				blockers = append(blockers, fmt.Sprintf("%s %s is shadowed", sc.ID, cond))
			default:
				blockers = append(blockers, fmt.Sprintf("%s %s is armable but carries no arm", sc.ID, cond))
			}
		}
	}
	if armedInBias > 0 {
		return ""
	}
	msg := fmt.Sprintf("⚠ bias=%s but no %s scenario carries an arm", bias, bias)
	if armedOther > 0 {
		msg += fmt.Sprintf(" (%d arm(s) authored on the other side)", armedOther)
	}
	if len(blockers) > 0 {
		msg += " — " + strings.Join(blockers, "; ")
	} else {
		msg += " — no scenario in the bias direction at all"
	}
	return msg + ". " + BiasCoherentArmsHint
}
