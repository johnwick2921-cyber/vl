package kernel

import (
	"fmt"
	"strings"
)

// CLASS 38 (2026-09-01) — THE PROMPT/VALIDATOR CONTRACT.
//
// The defect class: the prompt offers or names something the validator
// rejects. reject_retest (class 34), the bare "2x5m" confirm token, and the
// unqualified "legs" schema field are all instances. The model is not the
// defect; the contract is.
//
// This registry is the machine-checkable half of that contract. Every
// validator branch that restricts a field BY CONDITION is listed here with the
// sentence(s) the rendered prompt must carry. ValidatePromptContracts asserts
// the sentences are present — "validator forbids X" without "prompt says X is
// forbidden" fails the build (table test) and shouts at boot.
//
// Add a row whenever a condition-keyed restriction is added to the validator.
// The list is deliberately literal: it is a contract, not a heuristic.

// PromptContract is one validator restriction and the prompt text that states
// it to the author.
type PromptContract struct {
	// Rule is the human name of the restriction.
	Rule string
	// Site is the validator branch that enforces it (file:line at authoring).
	Site string
	// MustAppear are fragments the rendered prompt must contain. ALL must be
	// present — a restriction stated only half-way is how row 80 happened.
	MustAppear []string
	// Gate is a phrase the rendered prompt must contain for this row to be
	// enforced (used for knob-gated contract sentences such as the
	// write-time feasibility clause). Empty = always enforced.
	Gate string
	// Unless (W-EXEC-TRUTH W3, 2026-09-23) is a phrase whose presence SUSPENDS
	// this row: the legacy-policy rows whose sentences the market_in_zone
	// prompt replaces are suspended by EntryPolicyPromptMarker, and the ENTRY
	// POLICY row (Gate = the same marker) states the replacement. Empty =
	// never suspended.
	Unless string
}

// PromptContracts is the C5 enumeration: every condition-keyed restriction in
// ValidatePlanDoc / ArmSpecValid / ValidateEntryLaw.
func PromptContracts() []PromptContract {
	return []PromptContract{
		// CLASS 45 (2026-09-02) — the two feed-forward facts. They are
		// CONDITIONAL sections (rendered only when there is something to say),
		// so the contract asserts the ORDER text that always ships with them.
		{
			// CHOP COLLAPSE (owner ruling 2026-09-03): levels void BOTH ways carry
			// no direction, so the eighteen-line list measured live at 00:00:56
			// CT renders as ONE aggregated CHOP line naming the alternative
			// ("prefer touch/fade plays there"); one-sided voids keep their own
			// line with side and reclaim time. MustAppear stays the ORDER text
			// that ALWAYS ships — the chop line itself is conditional, and a
			// fragment that cannot always appear would make this guard unusable.
			Rule:       "void breakdown levels are named in the facts, not discovered at write (both-way voids collapse into one CHOP line that names the alternative)",
			Site:       "kernel/class45_feeds_forward.go ComputeVoidBreakdownLevels → BreakdownContinueState; RenderVoidBreakdownLevels (chop collapse)",
			MustAppear: []string{"if a breakdown level is listed as VOID above, author a different condition there"},
		},
		{
			Rule:       "the gap-side order names a DIRECTION, never a condition",
			Site:       "kernel/plan_doc.go hasDirection + gapDownDirectionMessage",
			MustAppear: []string{"MUST include a SHORT-direction scenario", "ANY legal condition"},
		},
		{
			Rule:       "arm{} legal only on armable conditions",
			Site:       "plan_doc.go ArmSpecValid (arm enabled on non-armable condition)",
			MustAppear: []string{"legal ONLY on " + ArmableConditionsPipe()},
			Unless:     EntryPolicyPromptMarker, // W3: the ENTRY POLICY row states the market_in_zone set
		},
		{
			Rule:       "legs[] only on sweep_reclaim (arm_legs_sweep_reclaim_only)",
			Site:       "plan_doc.go ArmSpecValid (arm legs on %s)",
			MustAppear: []string{"ONLY if condition is sweep_reclaim", "legs[] are the sweep_reclaim SPLIT contract"},
		},
		{
			Rule:       "legs[] must be EXACTLY 2 when present",
			Site:       "plan_doc.go ArmSpecValid (needs EXACTLY 2 legs)",
			MustAppear: []string{"EXACTLY 2 legs"},
		},
		{
			Rule:       "every non-sweep condition arms SINGLE (no legs)",
			Site:       "plan_doc.go ArmSpecValid (arm legs on %s — other conditions arm single)",
			MustAppear: []string{"must arm SINGLE", "no legs"},
		},
		{
			Rule:       "split requires confirm=touch at the sweep ref",
			Site:       "plan_doc.go ArmSpecValid (split requires confirm=touch)",
			MustAppear: []string{"confirm=touch at the sweep ref"},
		},
		{
			Rule:       "split leg 1 rests (wait_confirm false), leg 2 chains (wait_confirm true)",
			Site:       "plan_doc.go ArmSpecValid (leg 1 must rest / leg 2 must chain)",
			MustAppear: []string{"leg 1 rests there (wait_confirm false)", "leg 2 chains (wait_confirm true)"},
		},
		{
			Rule:       "split leg 2 rule ∈ {1m_mss, 1x5m_close} and equals confirm2.rule",
			Site:       "plan_doc.go ArmSpecValid (sweep_leg2_requires_mss_or_1x5m / must match confirm2.rule)",
			MustAppear: []string{"confirm2 = 1m_mss or 1x5m_close", "EQUAL to confirm2.rule"},
		},
		{
			Rule:       "split top-level entry/stop/target mirror leg 1",
			Site:       "plan_doc.go ArmSpecValid (top-level must equal leg 1's)",
			MustAppear: []string{"top-level entry/stop/target mirror leg 1"},
		},
		{
			// W3 (2026-09-23): LEGACY policy only — under market_in_zone a
			// waterfall arm takes pullback OR immediate (armSpecValidPolicy) and
			// the ENTRY POLICY row below states it; this row is suspended there.
			Rule:       "breakdown/breakup arm requires breakdown{} with entry_mode=pullback (legacy policy)",
			Site:       "plan_doc.go ArmSpecValid (arm requires entry_mode=pullback)",
			MustAppear: []string{"entry_mode=pullback", "entry_mode=immediate is AI-path ONLY"},
			Unless:     EntryPolicyPromptMarker,
		},
		{
			Rule:       "sweep_reclaim single arm requires wait_confirm:true",
			Site:       "plan_doc.go ArmSpecValid (sweep_reclaim arm requires wait_confirm:true)",
			MustAppear: []string{"wait_confirm:true"},
		},
		{
			Rule:       "fvg{} REQUIRED iff condition==fvg_entry",
			Site:       "plan_doc.go ValidatePlanDoc (fvg required)",
			MustAppear: []string{`fvg{} REQUIRED iff condition=="fvg_entry"`},
		},
		{
			Rule:       "breakdown{} REQUIRED iff waterfall-class condition",
			Site:       "plan_doc.go ValidatePlanDoc (breakdown required)",
			MustAppear: []string{"breakdown{} REQUIRED iff waterfall-class"},
		},
		{
			Rule:       "fades (reject|fvg_entry) are touch-only (fade_requires_touch)",
			Site:       "entry_law.go ValidateEntryLaw (fade_requires_touch)",
			MustAppear: []string{"touch ONLY (fade_requires_touch)"},
		},
		{
			Rule:       "2x5m_close legal ONLY on breakdown_continue|breakup_continue (2x5m_reserved)",
			Site:       "entry_law.go ValidateEntryLaw (2x5m_reserved)",
			MustAppear: []string{"2x5m_close is legal ONLY here"},
		},
		{
			Rule:       "armed fade needs a structure stop ≥2 ticks beyond the level",
			Site:       "entry_law.go ValidateEntryLaw (structure stop required)",
			MustAppear: []string{"structure stop ≥2 ticks beyond the level"},
		},
		{
			// W3 (2026-09-23): LEGACY policy only — under market_in_zone
			// breakout_retest arms (ArmableConditionFor) and stays SHADOW by
			// default (D11); the ENTRY POLICY row states it.
			Rule:       "breakout_retest never arms (GAR-F4) (legacy policy)",
			Site:       "armed.go ArmableCondition (breakout_retest excluded)",
			MustAppear: []string{"breakout_retest stays a normal AI play"},
			Unless:     EntryPolicyPromptMarker,
		},
		{
			// W-EXEC-TRUTH W3 (2026-09-23) — THE ENTRY POLICY. Rendered only when
			// the resolved day_plan.entry_policy_default is market_in_zone (the
			// shipped default); every fragment is a restriction the write site
			// enforces: the policy branch of ArmSpecValid (every condition arms,
			// pullback|immediate, wait_confirm for a non-touch confirm), the zone
			// verdict (kernel.ArmZoneVerdict via the trader write hook) and the
			// armable hold floor.
			Rule:       "market_in_zone: a limit at the far edge of entry_zone; every condition arms; a non-touch confirm chains; zone contains the entry, sits on the permitted side, ≤ zone_max_pts, bracket outside; waterfall pullback|immediate; armed time_hold ≥ min_hold_min",
			Site:       "kernel/entry_policy.go armSpecValidPolicy + ArmZoneVerdict (trader/write_time_feasibility.go writeTimeZoneVerdicts) + ValidateArmableHoldFloor",
			MustAppear: []string{EntryPolicyPromptMarker, "a LIMIT at the FAR edge of your economics.entry_zone", "legal ONLY on " + armableUnderPolicyPipe(EntryPolicyMarketInZone), "must carry wait_confirm:true", "the zone must contain arm.entry", "lie on the permitted side of the confirm ref", "leave the stop and the target OUTSIDE it", "entry_mode=pullback or entry_mode=immediate", "an armed time_hold holds at least"},
			Gate:       EntryPolicyPromptMarker,
		},
		{
			Rule:       "death/flip.rule is a SEPARATE enum from confirm.rule",
			Site:       "plan_doc.go conditionRules vs confirmRules",
			MustAppear: []string{"death/flip rules use their OWN vocabulary"},
		},
		{
			// W-FLIP-DIRECTION (2026-09-17) — LONDON v3 shipped bias short +
			// flip{below → long}; the validator checked the number, not the
			// direction. Now it rejects, and the prompt states the law.
			Rule:       "flip side must oppose the bias (short bias flips long on a close ABOVE; long bias flips short on a close BELOW)",
			Site:       "plan_doc.go FlipDirectionContradiction",
			MustAppear: []string{"flip side must oppose the bias: short bias flips long on a close ABOVE; long bias flips short on a close BELOW"},
		},
		{
			// W-FLIP-LINE-SIDE-OF-PRICE (2026-09-17) — ASIA v2 shipped bias
			// short + flip{29747.50 above → long} with price at 29764: the line
			// sat BELOW price with side "above". The touch gate fires a line
			// only from the near side after birth, so the plan could never flip.
			// The write site rejects it (when the authoring price is known) and
			// the prompt states the law; the death line obeys the same law.
			Rule:       "flip/death lines sit on the far side of price at authoring (side above → line ABOVE price; side below → line BELOW price)",
			Site:       "plan_doc.go FlipLineBeyondPrice / DeathLineBeyondPrice (ValidatePlanDocWithFactsMachine)",
			MustAppear: []string{"a flip line must sit on the far side of price", "the death line obeys the same law"},
		},
		{
			// S3 (2026-09-16) — the structure relation contract: the validator
			// stamps relation_d / relation_4h; the model never writes them and
			// a counter-trend scenario is flagged, never blocked.
			Rule:       "relation_d / relation_4h are validator-stamped — the model never writes them; counter-trend is a flag, not a block",
			Site:       "kernel/structure_relation.go StampScenarioRelations → ValidatePlanDocWithFactsMachine",
			MustAppear: []string{"the validator stamps relation_d / relation_4h itself, the model never writes them"},
		},
		{
			// W-WRITE-TIME-FEASIBILITY (2026-09-18) — the write site runs the
			// executor's gate-at-arm predicates; a scenario that would be refused
			// at arm is repair-hinted, and after the last repair attempt it is
			// written with arm.enabled=false + arm_disabled_reason. The prompt
			// fragment renders only when the knob is ON (default ON per owner
			// ruling "fix all").
			Rule:       "an arm the gate-at-arm chain would refuse is repaired first, then written arm.enabled=false with arm_disabled_reason",
			Site:       "trader/auto_trader_planner.go write-time feasibility check → armGateVerdictFor / composeArmStop geometry / decideStopEntry",
			MustAppear: []string{"written with arm.enabled=false", "arm_disabled_reason", "stop-entry trigger already through price"},
			Gate:       "WRITE-TIME FEASIBILITY",
		},
		{
			// W-EXEC-TRUTH W2 A5 (2026-09-23) — the stored duration. A time_hold
			// whose prose states minutes must store them as confirm.hold_min
			// (row 452 S2 "3 minutes" was counted as the 10-minute default), and
			// hold_min is legal on time_hold only.
			Rule:       "time_hold prose minutes must be stored as confirm.hold_min (time_hold only); the machine counts the stored rule exactly",
			Site:       "kernel/confirm_resolver.go ValidateConfirmHoldProse (parsePlanDocument newAuthoring) + validateConfirmHoldMin (ValidatePlanDocWithCaps)",
			MustAppear: []string{`"hold_min": <n>`, "hold_min is time_hold ONLY — the minutes of 1m closes your prose states", "2x5m_close waits for TWO completed 5m closes"},
		},
		{
			// W-EXEC-TRUTH W2 A1 (2026-09-23) — scenario.invalid outside the
			// grammar was accepted as UNKNOWN (row 455: all four sentences).
			// It is now a write-time refusal, so the prompt states the grammar
			// and one placeholder example, verbatim from the kernel.
			Rule:       "scenario.invalid must be exactly one of the 5m / 2x5m close above|below <price> forms — anything else is refused at write",
			Site:       "trader/plan_liveness.go validateAuthoredScenariosAt → kernel.EvaluateBornCheck (authoredCloseRule grammar, AuthoredUnknownGrammar)",
			MustAppear: []string{`"invalid" GRAMMAR (machine-checked at write; anything else is REFUSED, never accepted as UNKNOWN)`, `"5m close above <price>" | "5m close below <price>" | "2x5m close above <price>" | "2x5m close below <price>"`, `Example: "invalid": "`},
		},
		// W-EXEC-TRUTH W2 A3 (2026-09-23) — identity ≠ price is a write-time
		// refusal (correction; unconditional), including an id not in the map.
		{
			Rule:       "a level_id names the map level at the traded price; an id at another price or not in the map is refused at write",
			Site:       "kernel/scenario_write_truth.go scenarioIdentityWriteIssues ← CheckScenarioWriteTruth (trader write loop + shadowVerdictFor)",
			MustAppear: []string{"IDENTITY = PRICE (refused at write)", "an id not in the map (invented or altered), is REFUSED"},
		},
		{
			Rule:       "a two-anchor setup names two different map ids (sweep_level_id / reclaim_level_id), each at its own leg",
			Site:       "kernel/scenario_write_truth.go scenarioIdentityWriteIssues (anchor_reuse / anchor_unrelated)",
			MustAppear: []string{"sweep_level_id and reclaim_level_id: two DIFFERENT map ids"},
		},
		// W-EXEC-TRUTH W2 A4 (2026-09-23) — the obstacle-chain contract.
		{
			Rule:       "target_chain sorted outward; first_obstacle = nearest seated level on the path; every seated path level listed in path_levels with a role",
			Site:       "kernel/scenario_write_truth.go obstacleChainWriteIssues ← CheckScenarioWriteTruth",
			MustAppear: []string{"target_chain is sorted outward from entry in the trade direction", "first_obstacle is the NEAREST seated map level strictly between entry and the arm target", "a seated level missing from the path is REFUSED by name"},
		},
		{
			Rule:       "reduce on a single-contract arm is refused as infeasible",
			Site:       "kernel/scenario_write_truth.go obstacleChainWriteIssues (reduce_qty1, ArmQuantityFor)",
			MustAppear: []string{"reduce on a single-contract arm", "is REFUSED at write as infeasible"},
		},
	}
}

// ValidatePromptContracts asserts every enumerated restriction is stated in the
// rendered prompt. Returns the first restriction that is not.
func ValidatePromptContracts(prompt string) error {
	for _, c := range PromptContracts() {
		if c.Gate != "" && !strings.Contains(prompt, c.Gate) {
			continue // knob-gated row; the sentence was not rendered
		}
		if c.Unless != "" && strings.Contains(prompt, c.Unless) {
			continue // W3: a legacy row the rendered policy replaces
		}
		for _, frag := range c.MustAppear {
			if !strings.Contains(prompt, frag) {
				return fmt.Errorf("restriction %q (enforced at %s) is NOT stated in the prompt — missing fragment %q", c.Rule, c.Site, frag)
			}
		}
	}
	return nil
}

// PromptContractBootLine renders the F8 boot line. The prompt is rendered from
// the pure output-contract block (no market data needed), so the guard runs at
// every boot exactly as the table test runs it in CI.
func PromptContractBootLine() string {
	n := len(PromptContracts())
	// 0/0 → resolvePlanCaps supplies the RESOLVED caps (A11). The contract
	// sentences are static text, so the cap values never change the verdict.
	// W3: every entry policy's rendering is judged — the shipped default
	// (market_in_zone), planned_order and legacy — so a row can never be
	// "stated" only under a policy the bot is not running.
	for _, p := range []string{EntryPolicyMarketInZone, EntryPolicyPlannedOrder, EntryPolicyDefaultLegacy} {
		if err := ValidatePromptContracts(plannerOutputContractFor(0, 0, true, true, true, resolvePromptEntryPolicy(p, 0, 0))); err != nil {
			return fmt.Sprintf("📜 prompt/validator contract: BROKEN — %v [entry policy %s] (class 38 guard)", err, p)
		}
	}
	return fmt.Sprintf("📜 prompt/validator contract: %d restrictions, all stated in prompt (class 38 guard)", n)
}
