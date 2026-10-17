package kernel

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// P3.3 — the day-plan document (the schema-strict JSON the planner AI emits).
// The card renders it; the executor cites its scenarios. Reasoning-fields-FIRST
// (reasoning before the answer fields) per the contract.

// PlanBias is the plan's directional bias + explicit flip condition.
type PlanBias struct {
	Direction     string `json:"direction"`      // long | short | neutral
	Conviction    string `json:"conviction"`     // high | medium | low
	FlipCondition string `json:"flip_condition"` // e.g. "flips short on 2x5m < 30148"
}

// PlanLevel is one graded reference level with an instruction verb.
type PlanLevel struct {
	ID            *string  `json:"id"`
	Symbol        *string  `json:"symbol"`
	Kind          *string  `json:"kind"`
	Lo            *float64 `json:"lo"`
	Hi            *float64 `json:"hi"`
	OriginDate    *string  `json:"origin_date"`
	TF            *string  `json:"tf"`
	FormedAtMs    *int64   `json:"formed_at_ms"`
	FormedCloseMs *int64   `json:"formed_close_ms"`
	LookbackBars  *int     `json:"lookback_bars"`
	Names         []string `json:"names,omitempty"`
	SourceIDs     []string `json:"source_ids,omitempty"` // exact merged members, captured by the existing merge
	Price         float64  `json:"price"`
	Label         string   `json:"label"`       // provenance chip: PDH, ONH, nPOC·Tue, RN, EQH…
	Grade         string   `json:"grade"`       // A | B | C (MODEL-written)
	Instruction   string   `json:"instruction"` // instruction verb, e.g. "fade", "reclaim-long"
	// MachineGrade is the deterministic detector-side grade (type × freshness ×
	// confluence × HTF — levels_score.go) stamped at plan write by matching the
	// plan level back to the Go-ranked candidate table. Empty when no match
	// (owner levels grade A by design; unseated detector levels have none).
	// Master-audit finding 8.4: the card used to show ONLY the model's grade.
	MachineGrade string `json:"machine_grade,omitempty"`
}

// PlanScenario is one if/then play in the formal grammar.
// PlanConfirm (C1/F3, fail-register wave 2026-08-20) — the STRUCTURED
// confirmation the owner asked for ("2x5m vs 15m close — I need to totally
// understand"). The prose trigger/invalid REMAIN AI-judged; this object is
// machine-computed into an advisory prompt line + card chip (MET / NOT MET)
// using the same acceptance machinery as plan death — the AI stays the final
// judge (no new hard gate; the suppression class stays dead), but it reasons
// from machine truth instead of re-deriving closes itself.
type PlanConfirm struct {
	Rule     string  `json:"rule"`      // touch | 1x5m_close | 2x5m_close | 1m_mss | time_hold (E1: 15m dead)
	RefPrice float64 `json:"ref_price"` // the price the closes are counted against
	Side     string  `json:"side"`      // above | below
	// HoldMin (W2 A5, 2026-09-23) — time_hold ONLY: the minutes of completed
	// 1m closes the prose states. Absent = the ACCEPT_HOLD_MIN authoring
	// default (ResolveConfirm names it); never inferred from prose at read.
	HoldMin *int `json:"hold_min,omitempty"`
}

type PlanScenario struct {
	LevelID *string `json:"level_id"` // NULL on legacy; refused at write when it does not resolve or names another price (W2 A3).
	// W2 A3 — a two-anchor setup (sweep + reclaim) names each leg's level.
	// Additive: absent on legacy rows; checked only at the write site.
	SweepLevelID   *string `json:"sweep_level_id,omitempty"`
	ReclaimLevelID *string `json:"reclaim_level_id,omitempty"`
	// Absent on legacy records: never inferred or required during stored reads.
	Economics *ScenarioEconomics `json:"economics,omitempty"`
	ID        string             `json:"id"`        // S1, S2, S3
	Trigger   string             `json:"trigger"`   // the setup description
	Condition string             `json:"condition"` // reclaim|hold|sweep_reclaim|reject|acceptance|breakout_retest|fvg_entry|breakdown_continue|breakup_continue
	Direction string             `json:"direction"` // long | short
	// S3 (2026-09-16): relations are VALIDATOR-COMPUTED — never model-authored.
	// relation_d / relation_4h are stamped from the structure table's D and 4h
	// trend vs this scenario's direction (with-trend | counter-trend | range).
	// A model-supplied value in either field is moved to relation_claimed at
	// stamp time and overwritten — the model's own claim is kept, never trusted.
	RelationD       string    `json:"relation_d,omitempty"`
	Relation4h      string    `json:"relation_4h,omitempty"`
	RelationClaimed string    `json:"relation_claimed,omitempty"`
	TargetChain     []float64 `json:"target_chain"` // ordered targets
	Invalid         string    `json:"invalid"`      // invalidation
	// Confirm (C1) — REQUIRED after the grace window; see PlanConfirm.
	Confirm *PlanConfirm `json:"confirm,omitempty"`
	Quality string       `json:"quality"` // A+ | A | B
	// Fvg (FVG ENTRY MODEL, 2026-08-26) — machine-verified gap-entry play:
	// condition=="fvg_entry" REQUIRES this object. Every field is re-verified
	// from stored bars at write time (ValidateFvgEntryScenarios) — the model
	// declares, the math verifies.
	Fvg *PlanFvgEntry `json:"fvg,omitempty"`
	// Breakdown (WATERFALL-CLASS, 2026-08-28) — machine-verified momentum-follow
	// play: condition=="breakdown_continue"|"breakup_continue" REQUIRES this
	// object (ValidateBreakdownContinueScenarios re-checks the displacement from
	// bars at write time).
	Breakdown *PlanBreakdownContinue `json:"breakdown,omitempty"`
	// Confirm2 (F2 two-leg confirm rendering, 2026-08-28) — the optional SECOND
	// trigger leg (e.g. the retest-fail leg of a two-leg setup). A partial
	// (leg 1 MET, leg 2 not) never renders as a bare "MET".
	Confirm2 *PlanConfirm `json:"confirm2,omitempty"`
	// G5 (regime wave 2026-08-21) — true when the trigger level was CONSUMED at
	// write/re-align time: quality is capped at C and the card badges it
	// "level consumed". Advisory — never a gate.
	Consumed bool `json:"consumed,omitempty"`
	// A2 (planner-contract wave 2026-08-26) — optional setup-chain link: the
	// id of the scenario this play FOLLOWS (e.g. an fvg_entry SHOULD declare
	// the sweep_reclaim that swept the origin pool). Validator WARNs (never
	// fails) when an fvg_entry lacks a sweep precursor at a non-A/B origin.
	ChainAfter string `json:"chain_after,omitempty"`
	// Arm (Wave 2 armed orders, 2026-08-27) — the AI's AUTHORIZATION to arm
	// this scenario as a resting order with exact deterministic prices. The
	// LLM chooses WHAT to arm; Go manages WHEN it fills (advisory law holds).
	// Only armable conditions may carry an enabled Arm. The set is
	// ArmableCondition() in armed.go plus chained sweep_reclaim — do not
	// restate it here. This comment used to name "breakout_retest" as armable
	// and it never was, which is part of why long plans could not arm: they
	// lean on retest plays. One source, quoted nowhere else (owner ruling
	// 2026-09-04 (b)).
	// Arm (Wave 2 armed orders, 2026-08-27) — the AI's AUTHORIZATION to arm
	// this scenario as a resting order with exact deterministic prices. The
	// LLM chooses WHAT to arm; Go manages WHEN it fills (advisory law holds).
	Arm *PlanArmSpec `json:"arm,omitempty"`
	// W-EXEC-TRUTH W5 — a MACHINE-authored scenario (the Picture HTF source,
	// ScenarioSourcePicture). Absent on every planner-authored scenario: the
	// planner write path refuses both fields, a P<n> id is legal only with
	// them, and they reach a plan only through a machine overlay or a machine
	// plan (plan_machine.go). A planner scenario re-marshals byte-identically.
	Source  string             `json:"source,omitempty"`
	Machine *PlanMachineSource `json:"machine,omitempty"`
}

// PlanArmSpec is the machine-manageable arming contract for one scenario.
// Entry is the resting LIMIT price; Stop/Target form the bracket. Long:
// stop < entry < target. Short: target < entry < stop.
// E4 (entry-mechanics 2026-08-30): a sweep_reclaim arm may carry Legs — the
// SPLIT entry (two child orders, shared plan lineage, independent OCO
// brackets). When Legs is present the top-level Entry/Stop/Target mirror
// Legs[0] (legacy readers stay coherent).
type PlanArmSpec struct {
	Enabled     bool    `json:"enabled"`                // the arming authorization itself
	Entry       float64 `json:"entry"`                  // resting limit price
	Stop        float64 `json:"stop"`                   // bracket stop
	Target      float64 `json:"target"`                 // bracket target
	WaitConfirm bool    `json:"wait_confirm,omitempty"` // chain-arm: rest until the scenario's confirm{} is MET (sweep_reclaim retrace fast path, autopsy-response wave)
	// Legs (E4): the split entry. Exactly 2 when present, sweep_reclaim only.
	// Leg 0 = the touch leg (wait_confirm false, resting AT the sweep ref);
	// Leg 1 = the momentum leg (wait_confirm true, chains on 1m_mss or
	// 1x5m_close). Either leg's stop-out cancels the sibling's unfilled order.
	Legs []PlanArmLeg `json:"legs,omitempty"`
	// DisabledReason (W-WRITE-TIME-FEASIBILITY, 2026-09-18) — set by the write
	// site when the scenario's arm would have been refused by the gate-at-arm
	// chain and the repair budget is spent: the arm is written disabled with
	// the reason instead of a silent WARN. NULL on legacy rows and on every
	// path where the arm was never judged.
	DisabledReason string `json:"arm_disabled_reason,omitempty"`
	// Policy (W3) — the entry policy: "market_in_zone" | "planned_order".
	// Absent = LEGACY (today's arm kinds, byte-identical). A newly authored
	// plan is stamped with day_plan.entry_policy_default at parse; a stored
	// doc is never stamped. A leg's own Policy overrides this one.
	Policy string `json:"policy,omitempty"`
}

// PlanArmLeg is one child order of a split arm.
type PlanArmLeg struct {
	Entry       float64 `json:"entry"`                  // resting limit / stop-entry trigger price
	Stop        float64 `json:"stop"`                   // bracket stop
	Target      float64 `json:"target"`                 // bracket target
	Size        int     `json:"size,omitempty"`         // contracts; 1 when absent
	WaitConfirm bool    `json:"wait_confirm,omitempty"` // leg chains on its confirm rule before placement
	Rule        string  `json:"rule,omitempty"`         // the confirm rule the leg chains on (1m_mss | 1x5m_close)
	Kind        string  `json:"kind,omitempty"`         // limit (default) | stop_entry (E7)
	Policy      string  `json:"policy,omitempty"`       // W3: overrides PlanArmSpec.Policy for this leg
}

// ArmSpecValid checks the arming contract of one scenario. ok=false with a
// reason when the contract is malformed or the condition is not armable.
func ArmSpecValid(sc PlanScenario) error {
	if sc.Arm == nil || !sc.Arm.Enabled {
		return nil // not armed — nothing to validate
	}
	// W-EXEC-TRUTH W3 (2026-09-23) — an arm carrying an entry policy (on the arm
	// or on any leg) is judged by the policy branch. An arm with no policy
	// anywhere is LEGACY and runs the code below byte-for-byte as before (R4).
	if armHasPolicy(sc.Arm) {
		return armSpecValidPolicy(sc)
	}
	// Autopsy-response wave (2026-08-27): sweep_reclaim becomes armable ONLY
	// as a CHAINED arm (wait_confirm) — the arm rests until the scenario's own
	// confirm{} is machine-MET, then the retrace entry goes live.
	// E4 (2026-08-30): the SPLIT entry replaces the single chain — the legacy
	// wait_confirm requirement applies to single arms only.
	if strings.EqualFold(strings.TrimSpace(sc.Condition), "sweep_reclaim") {
		if len(sc.Arm.Legs) == 0 && !sc.Arm.WaitConfirm {
			return fmt.Errorf("sweep_reclaim arm on %s requires wait_confirm:true (the retrace arm must chain on its confirm)", sc.ID)
		}
		if sc.Confirm == nil {
			return fmt.Errorf("sweep_reclaim arm on %s requires a confirm{} object to chain on", sc.ID)
		}
	} else if IsBreakdownCondition(sc.Condition) {
		// Waterfall-class arms (2026-08-28): the resting limit sits AT the broken
		// level and chains on confirm leg 1 — exactly the pullback-that-fails
		// entry the play describes. Immediate-mode entries cannot rest.
		if sc.Breakdown == nil {
			return fmt.Errorf("%s arm requires the breakdown{} facts object", sc.ID)
		}
		if !strings.EqualFold(strings.TrimSpace(sc.Breakdown.EntryMode), "pullback") {
			return fmt.Errorf("%s arm requires entry_mode=pullback (immediate-mode entries are AI-path only)", sc.ID)
		}
		if !sc.Arm.WaitConfirm {
			return fmt.Errorf("%s arm requires wait_confirm:true (it chains on confirm leg 1 before resting at the level)", sc.ID)
		}
		if sc.Confirm == nil {
			return fmt.Errorf("%s arm requires a confirm{} (leg 1) to chain on", sc.ID)
		}
	} else if !ArmableCondition(sc.Condition) {
		return fmt.Errorf("arm enabled on non-armable condition %q (fvg_entry | reject | breakdown_continue | breakup_continue; sweep_reclaim via wait_confirm; breakout_retest is a normal AI play and never arms — GAR-F4)", sc.Condition)
	}
	if err := armSplitLock(sc); err != nil {
		return err
	}
	return armPricesValid(sc)
}

// armSplitLock is the E4 split-leg contract, extracted UNCHANGED (W3) so the
// legacy path and the policy path refuse a malformed split with the IDENTICAL
// error strings.
func armSplitLock(sc PlanScenario) error {
	a := sc.Arm
	// E4 (2026-08-30) — split-entry legs. sweep_reclaim ONLY for now; exactly
	// two; leg 0 = the touch leg resting AT the sweep ref (no chain), leg 1 =
	// the momentum leg chained on 1m_mss (1x5m_close accepted).
	if len(a.Legs) > 0 {
		if !strings.EqualFold(strings.TrimSpace(sc.Condition), "sweep_reclaim") {
			return fmt.Errorf("arm legs on %s — arm_legs_sweep_reclaim_only %s", sc.Condition, ArmLegsSplitContract)
		}
		if len(a.Legs) != 2 {
			return fmt.Errorf("arm on %s needs EXACTLY 2 legs (split contract), got %d", sc.ID, len(a.Legs))
		}
		if sc.Confirm == nil || sc.Confirm.Rule != "touch" {
			return fmt.Errorf("arm on %s split requires confirm=touch at the sweep ref (leg 1 rests AT the level)", sc.ID)
		}
		if sc.Confirm2 == nil {
			return fmt.Errorf("arm on %s split requires confirm2 (the leg-2 chain: 1m_mss or 1x5m_close)", sc.ID)
		}
		l0, l1 := a.Legs[0], a.Legs[1]
		if l0.WaitConfirm {
			return fmt.Errorf("arm on %s leg 1 must rest at the sweep ref (wait_confirm false) — it fills ON the touch", sc.ID)
		}
		if !l1.WaitConfirm {
			return fmt.Errorf("arm on %s leg 2 must chain (wait_confirm true) on its confirm rule", sc.ID)
		}
		if l1.Rule != "1m_mss" && l1.Rule != "1x5m_close" {
			return fmt.Errorf("arm on %s leg 2 rule %q — sweep_leg2_requires_mss_or_1x5m", sc.ID, l1.Rule)
		}
		if l1.Rule != sc.Confirm2.Rule {
			return fmt.Errorf("arm on %s leg 2 rule %q must match confirm2.rule %q (the chain is the machine confirm)", sc.ID, l1.Rule, sc.Confirm2.Rule)
		}
		if l0.Kind != "" && l0.Kind != "limit" {
			return fmt.Errorf("arm on %s leg 1 must be a limit (touch leg), got kind %q", sc.ID, l0.Kind)
		}
		if l1.Kind != "" && l1.Kind != "limit" && l1.Kind != "stop_entry" {
			return fmt.Errorf("arm on %s leg 2 kind %q invalid (limit|stop_entry)", sc.ID, l1.Kind)
		}
		// The top-level fields mirror leg 1 for legacy readers.
		if a.Entry != l0.Entry || a.Stop != l0.Stop || a.Target != l0.Target {
			return fmt.Errorf("arm on %s top-level entry/stop/target must equal leg 1's (legacy readers read the top-level)", sc.ID)
		}
	}
	return nil
}

// armPricesValid is the bracket-shape tail of ArmSpecValid (extracted
// UNCHANGED, W3): exact positive prices and the long/short ordering of the
// arm and every leg.
func armPricesValid(sc PlanScenario) error {
	a := sc.Arm
	if a.Entry <= 0 || a.Stop <= 0 || a.Target <= 0 {
		return fmt.Errorf("arm on %s needs exact entry/stop/target > 0 (got %.2f/%.2f/%.2f)", sc.ID, a.Entry, a.Stop, a.Target)
	}
	dir := strings.ToLower(strings.TrimSpace(sc.Direction))
	if dir == "long" {
		if !(a.Stop < a.Entry && a.Entry < a.Target) {
			return fmt.Errorf("arm on %s long: stop %.2f < entry %.2f < target %.2f required", sc.ID, a.Stop, a.Entry, a.Target)
		}
		for i, l := range a.Legs {
			if !(l.Stop < l.Entry && l.Entry < l.Target) {
				return fmt.Errorf("arm on %s long leg %d: stop %.2f < entry %.2f < target %.2f required", sc.ID, i+1, l.Stop, l.Entry, l.Target)
			}
		}
	} else if dir == "short" {
		if !(a.Target < a.Entry && a.Entry < a.Stop) {
			return fmt.Errorf("arm on %s short: target %.2f < entry %.2f < stop %.2f required", sc.ID, a.Target, a.Entry, a.Stop)
		}
		for i, l := range a.Legs {
			if !(l.Target < l.Entry && l.Entry < l.Stop) {
				return fmt.Errorf("arm on %s short leg %d: target %.2f < entry %.2f < stop %.2f required", sc.ID, i+1, l.Target, l.Entry, l.Stop)
			}
		}
	}
	return nil
}

func validateArmSpecs(d *PlanDoc) error {
	if d == nil {
		return nil
	}
	for _, sc := range d.Scenarios {
		if err := ArmSpecValid(sc); err != nil {
			return err
		}
	}
	return nil
}

// PlanFvgEntry is the machine-verifiable schema of an fvg_entry scenario.
// ce is COMPUTED (midpoint) — a declared ce is re-checked, never trusted.
type PlanFvgEntry struct {
	Lo              float64 `json:"fvg_lo"`
	Hi              float64 `json:"fvg_hi"`
	CE              float64 `json:"ce"`
	EntryMode       string  `json:"entry_mode"`       // edge | ce (ce for gaps > FVG_CE_WIDTH_PTS)
	DisplacementATR float64 `json:"displacement_atr"` // impulse body in 5m ATR multiples (0 = let the validator compute)
	OriginLevel     string  `json:"origin_level"`     // the Tier-1/seated anchor the displacement left
	Direction       string  `json:"direction"`        // long | short (must equal scenario direction)
}

// PlanDoc is the full plan (stored as the plans.doc JSON).
type PlanDoc struct {
	Zones *LevelZoneMap `json:"zone_map,omitempty"` // frozen display snapshot, not an execution input
	// Frozen machine map actually shown at authoring; never model-authored.
	IdentityLevels []PlanLevel    `json:"identity_levels"`
	Reasoning      string         `json:"reasoning"` // reasoning FIRST
	Bias           PlanBias       `json:"bias"`
	Levels         []PlanLevel    `json:"levels"`
	Scenarios      []PlanScenario `json:"scenarios"`
	NoTrade        []string       `json:"no_trade"`
	DeathCondition string         `json:"death_condition"`
	DayType        string         `json:"day_type,omitempty"`

	// P0.3 (2026-08-19) — MACHINE-EVALUABLE death/flip. The prose fields above
	// stay (card display + back-compat); these structured fields are what Go
	// evaluates every cycle. Empty → the old all-levels-consumed fallback
	// remains for legacy stored plans.
	DeathStructured *PlanCondition `json:"death,omitempty"`
	FlipStructured  *PlanCondition `json:"flip,omitempty"`
	// PriceAtWrite (W-FLIP-LINE-SIDE-OF-PRICE, 2026-09-17) — the authoring
	// price the write-site validator judged the death/flip lines against
	// (facts.Price: the last CLOSED 1m close the read was authored on). Stamped
	// at write when known, 0/absent otherwise. The read path uses it to name a
	// stored line that sits on the wrong side of price WITHOUT inventing a
	// price; older rows fall back to the tape's close at their created_at.
	PriceAtWrite float64 `json:"price_at_write,omitempty"`

	// CLASS 39 (owner ruling 2026-09-01) — every normalize-don't-reject event
	// applied to this doc at validation: legs dropped from a non-sweep arm.
	// Kept IN the stored doc so the write site can WARN, the E8 row can be
	// stamped, and forensics can compare what the model authored with what
	// the machine kept.
	ArmNormalizations []ArmNormalization `json:"arm_normalizations,omitempty"`

	// CLASS 50b (live-bias replay ruling 53498adb, 2026-09-02) — the dual
	// label stamped at write: "bias: AI <x> · tree <y> · regime <z>". A LABEL,
	// not a direction — no MUST attaches to either leg.
	BiasLabel string `json:"bias_label,omitempty"`

	// S1 (2026-09-16) — the STRUCTURE table the read saw (D/4h/1h, bias only,
	// never an entry). ABSENT when the knob is off or nothing was computed —
	// never an empty object (canon: no fabricated values). Machine-stamped at
	// write, never model-authored. Field names are the S3/S5 contract.
	Structure *StructureMap `json:"structure,omitempty"`

	// NoTradeWindows (owner ruling 2026-09-02) — the MACHINE's structured
	// no-trade constraints for this plan's session, written at plan time from
	// enforcing sources only: the shared window definitions for first-N and
	// lunch, and the entry gate's own resolved red-news windows for T1. The
	// model's `no_trade` prose stays above for legacy readers but renders as
	// NOTES, never as a constraint — an ASIA card was showing an NY lunch
	// window because the model wrote it, beside a T1 blackout fourteen hours
	// after it had elapsed.
	//
	// Stored windows are absolute CT minutes; whether any of them still
	// constrains anything is decided at READ time (EvaluateNoTradeWindows),
	// never here.
	NoTradeWindows []NoTradeWindow `json:"no_trade_windows,omitempty"`
}

// PlanCondition is a checkable predicate: price closes beyond `Price` on the
// rule timeframe (`Rule`: "2x5m" | "5m_close"; E1 — the 15m variant is dead),
// on `Side` ("below" | "above"). `FlipTo` names the direction the bias flips
// to when the flip condition fires ("" for death).
type PlanCondition struct {
	Price  float64 `json:"price"`
	Side   string  `json:"side"` // below | above
	Rule   string  `json:"rule"` // 2x5m | 5m_close (15m dead — E1)
	FlipTo string  `json:"flip_to,omitempty"`
}

// conditionRules / conditionSides are the enums PlanCondition validates against.
// E1 (entry-mechanics 2026-08-30): the 15m condition variant is DEAD — removed
// from the enum chokepoint; new authorship is rejected (condition_rule_15m_removed).
var (
	conditionRules = map[string]bool{"2x5m": true, "5m_close": true}
	conditionSides = map[string]bool{"below": true, "above": true}
)

var (
	biasDirections  = map[string]bool{"long": true, "short": true, "neutral": true}
	biasConvictions = map[string]bool{"high": true, "medium": true, "low": true}
	levelGrades     = map[string]bool{"A": true, "B": true, "C": true}
	scenarioConds   = map[string]bool{"reclaim": true, "hold": true, "sweep_reclaim": true, "reject": true, "acceptance": true, "breakout_retest": true, "fvg_entry": true, "breakdown_continue": true, "breakup_continue": true}
	scenarioDirs    = map[string]bool{"long": true, "short": true}
	// C is ACCEPTED: it is the G5 machine-demoted state (trigger level consumed
	// at write/re-align time), never a model-written grade. The write path runs
	// demoteConsumedScenarios BEFORE validation, so rejecting C made every
	// consumed-trigger plan fail-closed (London/ASIA 2026-08-23/24).
	scenarioQualities = map[string]bool{"A+": true, "A": true, "B": true, "C": true}
)

// ScenarioSchemaLedger (PRE-SUNDAY F5) — one boot line listing the full
// scenario-condition vocabulary (sorted) so a shipped schema change is visible
// in the boot block without opening a prompt.
func ScenarioSchemaLedger() string {
	conds := make([]string, 0, len(scenarioConds))
	for c := range scenarioConds {
		conds = append(conds, c)
	}
	sort.Strings(conds)
	return fmt.Sprintf("scenario schema: %d conditions [%s]", len(conds), strings.Join(conds, ", "))
}

// ConfirmRuleLedger (ENTRY-MECHANICS E1, 2026-08-30) — one boot line listing
// the confirm-rule vocabulary (sorted) so the 5-rule enum (15m dead) is
// visible in the boot block.
func ConfirmRuleLedger() string {
	rules := make([]string, 0, len(confirmRules))
	for r := range confirmRules {
		rules = append(rules, r)
	}
	sort.Strings(rules)
	return fmt.Sprintf("confirm rules: %d [%s]", len(rules), strings.Join(rules, ", "))
}

const (
	planMaxLevels    = 8 // shipped default; the owner's max_levels (3–12) may raise it
	planMaxScenarios = 3 // shipped default; the owner's scenario_cap (1–5) may raise it

	// PlanHardMaxLevels / PlanHardMaxScenarios are the HARD CEILINGS no plan may
	// ever exceed — the UI range's top (max_levels 3–12, scenario_cap 1–5). The
	// resolved config can raise the shipped default up to these, never past.
	PlanHardMaxLevels    = 12
	PlanHardMaxScenarios = 5
)

// resolvePlanCaps turns resolved config values into effective caps: ≤0 → shipped
// defaults; above the hard ceilings → clamped DOWN to them (a bad config value
// can never widen the schema past what the UI offers).
func resolvePlanCaps(maxLevels, maxScenarios int) (maxL, maxS int) {
	maxL = planMaxLevels
	if maxLevels > 0 {
		maxL = maxLevels
	}
	if maxL > PlanHardMaxLevels {
		maxL = PlanHardMaxLevels
	}
	maxS = planMaxScenarios
	if maxScenarios > 0 {
		maxS = maxScenarios
	}
	if maxS > PlanHardMaxScenarios {
		maxS = PlanHardMaxScenarios
	}
	return maxL, maxS
}

// CollapsePlanLevels merges plan levels closer than tol points into ONE entry
// (the P0.4 cluster rule, applied to the model's own output — 2026-08-24 ASIA
// v2 fail-closed because the model wrote two levels 2.13 pts apart and the
// duplicate-seat validation burned all retries). The survivor is the higher
// grade; ties keep the first. Consumed ORs across the merge. Pure.
func CollapsePlanLevels(levels []PlanLevel, tol float64) ([]PlanLevel, int) {
	if len(levels) < 2 || tol <= 0 {
		return levels, 0
	}
	out := make([]PlanLevel, 0, len(levels))
	merged := 0
	for _, l := range levels {
		hit := -1
		for i, o := range out {
			if math.Abs(o.Price-l.Price) <= tol {
				hit = i
				break
			}
		}
		if hit < 0 {
			out = append(out, l)
			continue
		}
		merged++
		old := out[hit]
		if planGradeRank(l.Grade) > planGradeRank(old.Grade) {
			out[hit] = l
		}
	}
	return out, merged
}

// planGradeRank ranks a level grade A > B > C (unknown → 0).
func planGradeRank(g string) int {
	switch strings.ToUpper(strings.TrimSpace(g)) {
	case "A":
		return 3
	case "B":
		return 2
	case "C":
		return 1
	}
	return 0
}

// ParsePlanDoc extracts the JSON object from raw model output (tolerating
// surrounding prose / code fences), unmarshals it, and validates it against the
// schema at the SHIPPED caps (8 levels / 3 scenarios). Any failure → error, which
// the planner treats as a retryable/fail-closed event.
func ParsePlanDoc(raw string) (*PlanDoc, error) {
	return parsePlanDocument(raw, 0, 0, false, AuthoringOpts{})
}

// ParsePlanDocCapped is ParsePlanDoc with the RESOLVED config caps (max_levels,
// scenario_cap). H4/H5: the owner's raised caps (9–12 levels, 4–5 scenarios) must
// pass validation instead of making every read fail-closed against the hardcoded
// 8/3.
func ParsePlanDocCapped(raw string, maxLevels, maxScenarios int) (*PlanDoc, error) {
	return parsePlanDocument(raw, maxLevels, maxScenarios, true, AuthoringOpts{})
}

// ParsePlanDocCappedWithMinRR is ParsePlanDocCapped plus the resolved R:R floor
// for new-authoring economics (R4, owner ruling 2026-09-15). An over-floor
// misstated r_to_arm_target is auto-corrected to the computed value; minRR <= 0
// keeps the strict contradiction refusal.
func ParsePlanDocCappedWithMinRR(raw string, maxLevels, maxScenarios int, minRR float64) (*PlanDoc, error) {
	return parsePlanDocument(raw, maxLevels, maxScenarios, true, AuthoringOpts{MinRR: minRR})
}

// AuthoringOpts (W-EXEC-TRUTH W3, 2026-09-23) are the RESOLVED knobs a NEW
// authoring parse applies. The zero value is exactly ParsePlanDocCapped: no R:R
// auto-correct floor, no policy stamp, no hold floor.
type AuthoringOpts struct {
	// MinRR — the resolved R:R floor (see ParsePlanDocCappedWithMinRR).
	MinRR float64
	// EntryPolicyDefault — day_plan.entry_policy_default resolved. market_in_zone
	// | planned_order is STAMPED on every arm it is legal for, BEFORE the
	// validator runs (R4); "" or legacy stamps nothing.
	EntryPolicyDefault string
	// MinHoldMin — day_plan.min_hold_min resolved; ≤0 = no floor.
	MinHoldMin int
}

// ParsePlanDocForAuthoring is the new-authoring parse with the resolved W3
// knobs: the default entry policy is stamped (StampEntryPolicyDefault) before
// ValidatePlanDocWithCaps, and the armable hold floor runs beside the A5 prose
// check. A stored reader never calls this — a stored doc is never stamped.
func ParsePlanDocForAuthoring(raw string, maxLevels, maxScenarios int, opts AuthoringOpts) (*PlanDoc, error) {
	return parsePlanDocument(raw, maxLevels, maxScenarios, true, opts)
}

// The boolean is a trusted call-site boundary, never a JSON version switch.
func parsePlanDocument(raw string, maxLevels, maxScenarios int, newAuthoring bool, opts AuthoringOpts) (*PlanDoc, error) {
	js := extractJSONObject(raw)
	if js == "" {
		return nil, fmt.Errorf("no JSON object found in planner output")
	}
	var doc PlanDoc
	if err := json.Unmarshal([]byte(js), &doc); err != nil {
		return nil, fmt.Errorf("plan JSON unmarshal: %w", err)
	}
	// W3 R4: the default policy is stamped BEFORE the validator, so the
	// validator judges the arm the executor will run (ArmSpecValid's legacy
	// branch refuses acceptance/hold/breakout_retest arms that the policy
	// branch accepts).
	if newAuthoring {
		// W5 D18 — only the machine writes a machine scenario. A planner
		// output carrying source/machine is refused before anything else, so
		// the model can never author (or forge) Picture evidence.
		if err := RefuseModelAuthoredMachineFields(&doc); err != nil {
			return nil, err
		}
		StampEntryPolicyDefault(&doc, opts.EntryPolicyDefault)
	}
	if err := ValidatePlanDocWithCaps(&doc, maxLevels, maxScenarios); err != nil {
		return nil, err
	}
	if newAuthoring && scenarioEconomicsRequired {
		if err := validateNewScenarioEconomics(&doc, opts.MinRR); err != nil {
			return nil, err
		}
	}
	// W2 A5: a time_hold's stated minutes must be STORED (new authoring only).
	if newAuthoring {
		if err := ValidateConfirmHoldProse(&doc); err != nil {
			return nil, err
		}
		// W3 D10: the armable hold floor (CTO ruling 1790181002671).
		if err := ValidateArmableHoldFloor(&doc, opts.MinHoldMin); err != nil {
			return nil, err
		}
	}
	return &doc, nil
}

// ValidatePlanDoc enforces the schema-strict rules at the SHIPPED caps (levels
// ≤8, scenarios 1–3).
func ValidatePlanDoc(d *PlanDoc) error {
	return ValidatePlanDocWithCaps(d, 0, 0)
}

// NormalizePlanDocRules (F1b, LONDON-FORENSICS 2026-08-28) canonicalizes every
// confirm/flip/death rule spelling the model has been observed producing. The
// audit (journal, 2026-08-23→28) found flip.rule "2x5m_close" (15 rejects) and
// confirm.rule "5m_close" (2 rejects); the wider alias set mirrors the
// scenario-facts vocabulary so future spellings normalize too. Unknown
// spellings pass through unchanged — validation still rejects them honestly.
func NormalizePlanDocRules(d *PlanDoc) {
	if d == nil {
		return
	}
	for i := range d.Scenarios {
		if d.Scenarios[i].Confirm != nil {
			d.Scenarios[i].Confirm.Rule = NormalizeConfirmRule(d.Scenarios[i].Confirm.Rule)
		}
	}
	if d.FlipStructured != nil {
		d.FlipStructured.Rule = NormalizeConditionRule(d.FlipStructured.Rule)
	}
	if d.DeathStructured != nil {
		d.DeathStructured.Rule = NormalizeConditionRule(d.DeathStructured.Rule)
	}
	// H3 (hygiene 2026-09-03) — bias spelling. NOTE the canonical set is
	// long|short|neutral (biasDirections), NOT bull/bear: bull/bear is the
	// WEEKLY doc's vocabulary (kernel/weekly_prompt.go), and aliasing a day-plan
	// bias to "bear" would MANUFACTURE the reject this is meant to prevent.
	// Precautionary: a store grep found ZERO bias-spelling rejects to date, so
	// the table is kept to the directional vocabulary only — no semantic guesses.
	d.Bias.Direction = NormalizeBiasDirection(d.Bias.Direction)
	// CLASS 39 — legs on a non-sweep condition collapse to the single arm.
	normalizeArmLegs(d)
}

// NormalizeBiasDirection canonicalizes a day-plan bias direction to the set the
// validator accepts: long | short | neutral. Unknown spellings pass through
// unchanged so validation still rejects them honestly (class 11's law).
func NormalizeBiasDirection(dir string) string {
	switch strings.ToLower(strings.TrimSpace(dir)) {
	case "bearish", "bear", "down", "downside", "sell":
		return "short"
	case "bullish", "bull", "up", "upside", "buy":
		return "long"
	}
	// DELIBERATELY NOT ALIASED: "flat" / "sideways" / "range" → neutral. Those
	// are SEMANTIC judgements, not spellings of a known token, and "neutral" is
	// already writable directly. "sideways" also serves as the invalid-direction
	// sentinel in two existing fixtures (plan_doc_test, w4_overlay_executor_test)
	// — aliasing it would have silently weakened both. An alias table fixes
	// SPELLING; it must never decide meaning.
	return dir
}

// NormalizeConfirmRule canonicalizes a confirm{} rule spelling.
// E1 (2026-08-30): 15m spellings are NOT normalized — they pass through and
// the validator rejects them with confirm_rule_15m_removed (dead variant).
// Canonical: touch | 1x5m_close | 2x5m_close | 1m_mss | time_hold.
func NormalizeConfirmRule(rule string) string {
	switch strings.TrimSpace(rule) {
	case "5m_close", "5m-close", "5mclose", "1x5m":
		return "1x5m_close"
	case "2x5m", "2x_5m":
		return "2x5m_close"
	case "mss", "1m-mss", "1mmss", "mss_1m":
		return "1m_mss"
	case "hold_time", "timehold", "time-hold":
		return "time_hold"
	}
	return rule
}

// NormalizeConditionRule canonicalizes a death/flip structured rule spelling.
// E1 (2026-08-30): 15m spellings are NOT normalized — rejected at the chokepoint.
// Canonical: 2x5m | 5m_close.
func NormalizeConditionRule(rule string) string {
	switch strings.TrimSpace(rule) {
	case "2x5m_close", "2x_5m", "2x5":
		return "2x5m"
	case "1x5m_close", "1x5m", "5m-close", "5mclose":
		return "5m_close"
	}
	return rule
}

// ArmFeasibilityWarnings (F4, LONDON-FORENSICS 2026-08-28) reports arms that
// the gate-at-arm chain would refuse EVERY cycle: R:R below the arm minimum or
// a stop tighter than minSLMult × ATR5m. Advisory only — the write succeeds
// (the executor's hard gate is the enforcement); the planner learns instead of
// burning 120 refusal lines a night.
func ArmFeasibilityWarnings(d *PlanDoc, atr5m, minRR, minSLMult float64) []string {
	if d == nil || atr5m <= 0 {
		return nil
	}
	var out []string
	for _, sc := range d.Scenarios {
		a := sc.Arm
		if a == nil || !a.Enabled {
			continue
		}
		dist := a.Entry - a.Stop
		if strings.EqualFold(sc.Direction, "short") {
			dist = a.Stop - a.Entry
		}
		rr := 0.0
		if dist > 0 && a.Entry > 0 {
			if strings.EqualFold(sc.Direction, "short") {
				rr = (a.Entry - a.Target) / dist
			} else {
				rr = (a.Target - a.Entry) / dist
			}
		}
		if minRR > 0 && rr+1e-9 < minRR {
			out = append(out, fmt.Sprintf("%s arm R:R %.2f below min_risk_reward_ratio %.2f (Studio) — the gate-at-arm chain will refuse it every cycle (target/stop infeasible)", sc.ID, rr, minRR))
		}
		if minSLMult > 0 && dist+1e-9 < minSLMult*atr5m {
			out = append(out, fmt.Sprintf("%s arm stop %.2f too close (%.2f < %.2f = %.1f×ATR5m) — min-SL gate will refuse it", sc.ID, a.Stop, dist, minSLMult*atr5m, minSLMult))
		}
	}
	return out
}

// ValidatePlanDocWithCaps enforces the schema-strict rules: required fields, enum
// values, and counts at the RESOLVED caps (clamped to the 12/5 hard ceilings).
// ≤0 → shipped defaults, so default callers are byte-identical to before.
func ValidatePlanDocWithCaps(d *PlanDoc, maxLevels, maxScenarios int) error {
	maxL, maxS := resolvePlanCaps(maxLevels, maxScenarios)
	if d == nil {
		return fmt.Errorf("nil plan")
	}
	// F1b (LONDON-FORENSICS 2026-08-28) — ALIAS COMPLETION: the model has been
	// rejected 15× this week for flip.rule "2x5m_close" and 2× for
	// confirm.rule "5m_close". Normalize every observed spelling to the
	// canonical enum BEFORE validation so a truncation-adjacent spelling never
	// burns a planner retry again.
	NormalizePlanDocRules(d)
	if strings.TrimSpace(d.Reasoning) == "" {
		return fmt.Errorf("reasoning is required (reasoning-first)")
	}
	if !biasDirections[d.Bias.Direction] {
		return fmt.Errorf("bias.direction %q invalid (long|short|neutral)", d.Bias.Direction)
	}
	if d.Bias.Conviction != "" && !biasConvictions[d.Bias.Conviction] {
		return fmt.Errorf("bias.conviction %q invalid (high|medium|low)", d.Bias.Conviction)
	}
	if strings.TrimSpace(d.DeathCondition) == "" {
		return fmt.Errorf("death_condition is required")
	}
	if len(d.Levels) > maxL {
		return fmt.Errorf("too many levels: %d (max %d)", len(d.Levels), maxL)
	}
	for i, l := range d.Levels {
		if !levelGrades[l.Grade] {
			return fmt.Errorf("level[%d].grade %q invalid (A|B|C)", i, l.Grade)
		}
		// P5.1 hardening — a non-positive price is never a real level (armors both
		// the write path and read-time plan_final re-validation, all overlay
		// origins). Positive-but-implausible prices are caught by LevelPriceViolation.
		if l.Price <= 0 {
			return fmt.Errorf("level[%d].price %v invalid (must be > 0)", i, l.Price)
		}
	}
	if len(d.Scenarios) < 1 || len(d.Scenarios) > maxS {
		return fmt.Errorf("scenarios count %d invalid (1..%d)", len(d.Scenarios), maxS)
	}
	for i, s := range d.Scenarios {
		if strings.TrimSpace(s.ID) == "" {
			return fmt.Errorf("scenario[%d].id is required", i)
		}
		// A5 (F11, fail-register wave): the id format is a contract now — the
		// cite rule, the status map, the chips and adherence all key on it.
		if s.Source == "" && s.Machine == nil {
			if !scenarioIDRe.MatchString(strings.TrimSpace(s.ID)) {
				return fmt.Errorf("scenario[%d].id %q invalid (format: S1..S99)", i, s.ID)
			}
		} else if err := machineScenarioIdentity(s); err != nil {
			// W5 — a machine scenario: a known source, its machine record, and
			// a P<n> id (never the planner's S namespace).
			return fmt.Errorf("scenario[%d]: %w", i, err)
		}
		if !scenarioConds[s.Condition] {
			return fmt.Errorf("scenario[%d].condition %q invalid", i, s.Condition)
		}
		if !scenarioDirs[s.Direction] {
			return fmt.Errorf("scenario[%d].direction %q invalid (long|short)", i, s.Direction)
		}
		if !scenarioQualities[s.Quality] {
			return fmt.Errorf("scenario[%d].quality %q invalid (A+|A|B|C — C is the G5 machine-demoted 'level consumed' state)", i, s.Quality)
		}
		for j, t := range s.TargetChain {
			if t <= 0 {
				return fmt.Errorf("scenario[%d].target_chain[%d] %v invalid (must be > 0)", i, j, t)
			}
		}
		// C1 (F3): when authored, the structured confirmation must be coherent
		// AND its number must appear in the prose trigger/invalid (the A3
		// object↔prose contract). Absence is judged at the WRITE SITE (grace
		// window), not here.
		if s.Confirm != nil {
			if confirmRuleMentions15m(s.Confirm.Rule) {
				return fmt.Errorf("scenario[%d].confirm.rule %q — confirm_rule_15m_removed (the 15m confirm variant is dead; use touch|1x5m_close|2x5m_close|1m_mss|time_hold)", i, s.Confirm.Rule)
			}
			if !confirmRules[s.Confirm.Rule] {
				return fmt.Errorf("scenario[%d].confirm.rule %q invalid (touch|1x5m_close|2x5m_close|1m_mss|time_hold)", i, s.Confirm.Rule)
			}
			if s.Confirm.Side != "above" && s.Confirm.Side != "below" {
				return fmt.Errorf("scenario[%d].confirm.side %q invalid (above|below)", i, s.Confirm.Side)
			}
			if s.Confirm.RefPrice <= 0 {
				return fmt.Errorf("scenario[%d].confirm.ref_price %v invalid", i, s.Confirm.RefPrice)
			}
			if !numberNearInText(s.Trigger+" "+s.Invalid, s.Confirm.RefPrice, 2.0) {
				return fmt.Errorf("scenario[%d].confirm.ref_price %.2f does not match any number in the trigger/invalid prose (object and prose must agree)", i, s.Confirm.RefPrice)
			}
			if err := validateConfirmHoldMin(i, "confirm", s.Confirm); err != nil {
				return err
			}
		}
		if s.Confirm2 != nil {
			if confirmRuleMentions15m(s.Confirm2.Rule) {
				return fmt.Errorf("scenario[%d].confirm2.rule %q — confirm_rule_15m_removed (the 15m confirm variant is dead; use touch|1x5m_close|2x5m_close|1m_mss|time_hold)", i, s.Confirm2.Rule)
			}
			if !confirmRules[s.Confirm2.Rule] {
				return fmt.Errorf("scenario[%d].confirm2.rule %q invalid (touch|1x5m_close|2x5m_close|1m_mss|time_hold)", i, s.Confirm2.Rule)
			}
			if s.Confirm2.Side != "above" && s.Confirm2.Side != "below" {
				return fmt.Errorf("scenario[%d].confirm2.side %q invalid (above|below)", i, s.Confirm2.Side)
			}
			if s.Confirm2.RefPrice <= 0 {
				return fmt.Errorf("scenario[%d].confirm2.ref_price %v invalid", i, s.Confirm2.RefPrice)
			}
			if err := validateConfirmHoldMin(i, "confirm2", s.Confirm2); err != nil {
				return err
			}
		}
	}
	// P0.3 (2026-08-19) — structured conditions validate when PRESENT (legacy
	// stored plans without them still pass — the all-levels-consumed fallback
	// governs those).
	for _, cond := range []struct {
		name string
		c    *PlanCondition
	}{{"death", d.DeathStructured}, {"flip", d.FlipStructured}} {
		if cond.c == nil {
			continue
		}
		if cond.c.Price <= 0 {
			return fmt.Errorf("%s.price %v invalid (must be > 0)", cond.name, cond.c.Price)
		}
		if confirmRuleMentions15m(cond.c.Rule) {
			return fmt.Errorf("%s.rule %q — condition_rule_15m_removed (the 15m condition variant is dead; use 2x5m|5m_close)", cond.name, cond.c.Rule)
		}
		if !conditionSides[cond.c.Side] {
			return fmt.Errorf("%s.side %q invalid (below|above)", cond.name, cond.c.Side)
		}
		if !conditionRules[cond.c.Rule] {
			return fmt.Errorf("%s.rule %q invalid (2x5m|5m_close)", cond.name, cond.c.Rule)
		}
		if cond.name == "flip" && cond.c.FlipTo != "" && !biasDirections[cond.c.FlipTo] {
			return fmt.Errorf("flip.flip_to %q invalid (long|short)", cond.c.FlipTo)
		}
	}
	// A3 (F5, fail-register wave): the prompt has always CLAIMED "death/flip
	// objects must match the prose lines" — nothing checked it. Now the
	// validator does: a structured price must appear (±2pts) among the numbers
	// in its prose twin, else the planner retries with a named error.
	if d.DeathStructured != nil && strings.TrimSpace(d.DeathCondition) != "" {
		if !numberNearInText(d.DeathCondition, d.DeathStructured.Price, 2.0) {
			return fmt.Errorf("death{price %.2f} does not match any number in death_condition prose %q (object and prose must agree)", d.DeathStructured.Price, d.DeathCondition)
		}
	}
	if d.FlipStructured != nil && strings.TrimSpace(d.Bias.FlipCondition) != "" {
		if !numberNearInText(d.Bias.FlipCondition, d.FlipStructured.Price, 2.0) {
			return fmt.Errorf("flip{price %.2f} does not match any number in bias.flip_condition prose %q", d.FlipStructured.Price, d.Bias.FlipCondition)
		}
	}
	// W-FLIP-DIRECTION (2026-09-17): the number was checked, the DIRECTION was
	// not. LONDON v3 shipped bias short + flip{below → long}: a short bias can
	// only flip long on a close ABOVE the line, so the overnight rally could
	// never flip it. Death is NOT judged here (separate question).
	if err := FlipDirectionContradiction(d.Bias.Direction, d.FlipStructured); err != nil {
		return err
	}
	// Wave 2 armed orders (2026-08-27) — the arm authorization must be coherent:
	// only armable conditions, exact prices, sane long/short ordering.
	if err := validateArmSpecs(d); err != nil {
		return err
	}
	// E2 (entry-mechanics 2026-08-30) — the per-condition ENTRY LAW: one table,
	// enum-keyed, condition → allowed confirm rules + required entry style.
	if err := ValidateEntryLaw(d); err != nil {
		return err
	}
	return nil
}

// FlipDirectionContradiction (W-FLIP-DIRECTION, 2026-09-17) is the one place
// the flip's SIDE is judged against the bias it flips from: a short bias flips
// to long only on a close ABOVE the line; a long bias flips to short only on a
// close BELOW it. An empty flip_to is read as the opposite of the bias. Returns
// nil when there is no structured flip, the bias is not long/short, or the
// flip_to is not the opposite of the bias (that is not this rule's question).
// Shared by the write-site validator (reject) and the read-path sanity pass
// (WARN for plans already in the store), so both speak one sentence.
func FlipDirectionContradiction(biasDir string, flip *PlanCondition) error {
	if flip == nil {
		return nil
	}
	bias := NormalizeBiasDirection(biasDir)
	var opposite, wantSide string
	switch bias {
	case "short":
		opposite, wantSide = "long", "above"
	case "long":
		opposite, wantSide = "short", "below"
	default:
		return nil
	}
	flipTo := strings.ToLower(strings.TrimSpace(flip.FlipTo))
	if flipTo == "" {
		flipTo = opposite
	}
	if flipTo != opposite {
		return nil
	}
	if flip.Side == wantSide {
		return nil
	}
	return fmt.Errorf("flip{%s %.2f → %s} contradicts bias %s: a %s bias flips to %s only on a close %s the line", flip.Side, flip.Price, flipTo, bias, bias, flipTo, wantSide)
}

// FlipLineBeyondPrice (W-FLIP-LINE-SIDE-OF-PRICE, 2026-09-17) is the sibling of
// FlipDirectionContradiction: the flip's side is judged against the AUTHORING
// PRICE. A flip with side "above" must have its line ABOVE price; side "below"
// must have it BELOW. The P1c touch gate (PlanConditionFiredSince) only fires
// a line price touches after the plan is born and then closes beyond on the
// stated side — a line already beyond price on its own side can never be
// touched from the near side, so it never fires and the plan cannot flip by
// construction (ASIA v2 2026-09-17 22:52 CT: flip{29747.50 above → long} with
// price 29764). Returns nil when there is no structured flip, the flip has no
// price, the side is not above/below, or PRICE IS UNKNOWN (<= 0) — the rule
// never invents a price. Shared by the write site (reject) and the read path
// (once-per-version WARN) so both speak one sentence.
func FlipLineBeyondPrice(flip *PlanCondition, price float64) error {
	if flip == nil {
		return nil
	}
	head := fmt.Sprintf("flip{%s %.2f", flip.Side, flip.Price)
	if to := strings.ToLower(strings.TrimSpace(flip.FlipTo)); to != "" {
		head += " → " + to
	}
	head += "}"
	return lineBeyondPrice(head, flip, price, "a flip line must sit on the far side of price (it can never be touched from the near side)")
}

// DeathLineBeyondPrice is FlipLineBeyondPrice for the death object: a death
// line already crossed at authoring is a plan born dead. This judges the READ
// price only; since W-EXEC-TRUTH W2 D5 the born check (EvaluateBornCheck, via
// validateAuthoredScenariosAt) also judges death{}/flip{} on every 5m group
// closed between the read clock and publication.
func DeathLineBeyondPrice(death *PlanCondition, price float64) error {
	if death == nil {
		return nil
	}
	head := fmt.Sprintf("death{%s %.2f}", death.Side, death.Price)
	return lineBeyondPrice(head, death, price, "a death line must sit on the far side of price (the plan would be born dead)")
}

// lineBeyondPrice is the one predicate both line rules share. Its rejection
// words ("far side of price") are what planner_repair.go routes on.
func lineBeyondPrice(head string, c *PlanCondition, price float64, law string) error {
	if c.Price <= 0 || price <= 0 {
		return nil
	}
	var where string
	switch c.Side {
	case "above":
		switch {
		case c.Price < price:
			where = "below"
		case c.Price == price:
			where = "at"
		default:
			return nil
		}
	case "below":
		switch {
		case c.Price > price:
			where = "above"
		case c.Price == price:
			where = "at"
		default:
			return nil
		}
	default:
		return nil
	}
	return fmt.Errorf("%s is already %s price %.2f at authoring: %s", head, where, price, law)
}

// FlipToDirection parses the flip direction out of a killer line
// ("flip-condition: ... → bias long") — "long"/"short", "" otherwise. Used by
// the write site to enforce that a flip-triggered re-plan honors the flip.
//
// W-FLIP-REREAD BLOCKER 1 (2026-09-17): ONLY the arrow form is read — the
// word after the LAST "→ bias " (or ASCII "-> bias "). The first version
// looked for the substring "bias long" anywhere in the string, before
// "bias short", and the structure_flip prior line ("PRIOR PLAN v3 bias long —
// its flip condition fired → bias is now expected short … flip-condition: …
// → bias short") echoes the OLD bias first: for long→short it answered
// "long", so the write site demanded the stale bias, rejected every correct
// short plan three times and would have accepted a wrong long one. The
// killer is always the LAST thing in that line, so the last arrow is the
// flip's destination. A "→ bias the other side" killer (empty flip_to) and a
// string with no arrow at all answer "" — nothing is mandated.
func FlipToDirection(killer string) string {
	k := strings.ToLower(killer)
	const arrowU, arrowA = "→ bias ", "-> bias "
	i, n := strings.LastIndex(k, arrowU), len(arrowU)
	if j := strings.LastIndex(k, arrowA); j > i {
		i, n = j, len(arrowA)
	}
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(k[i+n:])
	if sp := strings.IndexAny(rest, " \t\n\r.,;:)]"); sp >= 0 {
		rest = rest[:sp]
	}
	switch rest {
	case "long", "short":
		return rest
	}
	return ""
}

// structuralLabels are the detector-anchored label prefixes the model may NOT
// re-invent: a plan level whose price matches a machine-table row must carry
// the table's label, or the plan ships a phantom anchor (LONDON v1: "PDH
// 29297.75" when the true prior-day high was 29290.5).
var structuralLabels = map[string]bool{
	"PDH": true, "PDL": true, "PDC": true,
	"RTH-H": true, "RTH-L": true,
	"ONH": true, "ONL": true,
	"AS-H": true, "AS-L": true,
	"LDN-H": true, "LDN-L": true,
	"PWH": true, "PWL": true, "PMH": true, "PML": true,
	"EQH": true, "EQL": true,
}

// structuralPrefix returns the leading structural token of a label (e.g.
// "PDL", "EQH·4h" → "EQH"), "" when the label isn't structural. Only the "·"
// separator is split — RTH-H/AS-H/LDN-H are exact structural labels.
//
// W3's merged names (owner ruling 2026-09-11): a merged reference renders as
// EVERY member's label joined by " · " ("RTH-H · EQL·4h · EQH·15m",
// MapCandidate.NamesLine), and the model copies that line. The PRIMARY
// component is the first one, so the split token is trimmed before the
// lookup — "RTH-H " is "RTH-H". Before this, every merged label read as a
// re-invented anchor and cost a repair round on every read.
func structuralPrefix(label string) string {
	l := strings.TrimSpace(label)
	if i := strings.Index(l, "·"); i > 0 {
		l = strings.TrimSpace(l[:i])
	}
	if structuralLabels[l] {
		return l
	}
	return ""
}

// MislabeledStructuralLevels (P0.4-H, 2026-08-25) reports plan levels whose
// rounded price matches a machine-table row but whose label is a DIFFERENT
// structural anchor than the table's. Rounded-price keying mirrors the
// machine-grade stamp. Empty = clean. Pure.
func MislabeledStructuralLevels(d *PlanDoc, machineLabels map[float64]string) []string {
	if d == nil || len(machineLabels) == 0 {
		return nil
	}
	var out []string
	for _, l := range d.Levels {
		k := math.Round(l.Price*100) / 100
		ml, ok := machineLabels[k]
		if !ok {
			continue // not a machine-table price — free label
		}
		mp := structuralPrefix(ml)
		lp := structuralPrefix(l.Label)
		// Flag when EITHER side is a structural anchor and they disagree:
		// the LONDON v1 phantom was the model writing "PDH" (structural) over
		// a machine row whose label is a ZONE (non-structural). A structural
		// label may never be re-invented for a table price.
		if (mp != "" || lp != "") && mp != lp {
			out = append(out, fmt.Sprintf("%.2f labeled %q but the machine table says %q", l.Price, l.Label, ml))
		}
	}
	return out
}

type PlanFacts struct {
	Zones       *LevelZoneMap  `json:"-"` // presentation only
	IdentityMap []MapCandidate `json:"-"` // frozen seated map; read by the W2 A3/A4 write-time checks (nil = UNKNOWN → skipped)
	CapacityCut []MapCandidate `json:"-"` // W2 A4 — pool references the seat race dropped; accepted as a first obstacle, never required
	Price       float64        // reference price at read time
	DATR        float64        // daily ATR proxy
	PDH         float64        // prior day high (0 = unknown → gap rules skipped)
	PDL         float64        // prior day low (0 = unknown → gap rules skipped)
	PDC         float64        // prior day close (CLASS 50b — the bias-label tree leg)
	Regime      RegimeBlock    // CLASS 50b — the bias-label regime leg (read-time copy)
	Structure   *StructureMap  `json:"-"` // S1 — stamped onto the doc at write when non-nil
	// ReadAt (W-EXEC-TRUTH W2 A2) — the read clock the prompt was assembled at.
	// The write site re-judges every 5m group closed between it and publication.
	// Zero (legacy facts-less callers) = latest-window check, read clock n/a.
	// json:"-": FactsSnapshotJSON marshals PlanFacts and must not change shape.
	ReadAt time.Time `json:"-"`
}

// ValidatePlanDocWithFacts = schema rules + facts rules:
//   - 0 levels on a side of price → HARD FAIL (one-sided maps are the
//     2026-08-18 pathology — a 110-point breakdown with zero downside levels;
//     owner ruling 2026-08-31: the per-side COUNT concept is deleted — only
//     the 0-side guard survives);
//   - price BELOW PDL (gap-down) → a continuation SHORT scenario is mandatory;
//     price ABOVE PDH (gap-up) → a continuation LONG scenario is mandatory;
//   - no two levels within the cluster tolerance (duplicate seats);
//   - every scenario target must sit within the proximity band of price.
//
// Legacy signature: machine map nil. The write site uses
// ValidatePlanDocWithFactsMachine — see that doc.
func ValidatePlanDocWithFacts(d *PlanDoc, facts PlanFacts, maxLevels, maxScenarios int) error {
	return ValidatePlanDocWithFactsMachine(d, facts, nil, maxLevels, maxScenarios)
}

// ValidatePlanDocWithFactsMachine is the write-site validator. `machine` is the
// prompt-visible universe keyed by rounded price (seated table incl.
// owner-sticky levels + HTF-section rows merged); len==0 means the universe
// was empty/unknown.
//
// Side-count ruling history (owner ruling 2026-08-31): the per-side COUNT
// concept is DELETED from the system — no quota, no WARN, no thin_side note.
// The only survivors are the two data-earned hard fails:
//   - plan carries 0 on a side → HARD FAIL (the original one-sided-map
//     pathology — a plan nobody can trade on that side).
//   - machine map EMPTY → HARD FAIL (true safety floor — never write on an
//     empty/unknown map).
//
// All other rules (schema, duplicates, gap continuation, reachable targets,
// targets-in-band) are unchanged.
func ValidatePlanDocWithFactsMachine(d *PlanDoc, facts PlanFacts, machine map[float64]string, maxLevels, maxScenarios int) error {
	if err := ValidatePlanDocWithCaps(d, maxLevels, maxScenarios); err != nil {
		return err
	}
	// S3 (2026-09-16) — the relation fields are VALIDATOR-COMPUTED: stamp
	// relation_d / relation_4h from the structure table before any other check.
	// The model's own claim is preserved in relation_claimed, never trusted.
	StampScenarioRelations(d)
	if facts.Price <= 0 {
		return nil // no facts → schema-only (legacy callers/tests)
	}
	// W-FLIP-LINE-SIDE-OF-PRICE (2026-09-17): the flip's side was judged
	// against the BIAS (CLASS 140); nothing judged it against PRICE. ASIA v2
	// (22:52 CT) shipped bias short + flip{29747.50 above → long} with price at
	// 29764 — the line was BELOW price with side "above". The P1c touch gate
	// only fires a line price touches from the near side after birth, so a line
	// already beyond price on its own side can never fire: the plan cannot flip
	// by construction. The death line obeys the same law (a death line already
	// crossed is a plan born dead — the scenario born-dead check reads only
	// scenario.invalid prose and never the death object). Skipped above when
	// the authoring price is unknown: never invent a price.
	if err := FlipLineBeyondPrice(d.FlipStructured, facts.Price); err != nil {
		return err
	}
	if err := DeathLineBeyondPrice(d.DeathStructured, facts.Price); err != nil {
		return err
	}
	// P0.4 — duplicate-level rejection (the planner copied an EQ family 4×).
	for i := 0; i < len(d.Levels); i++ {
		for j := i + 1; j < len(d.Levels); j++ {
			if math.Abs(d.Levels[i].Price-d.Levels[j].Price) <= LevelClusterTicks*0.25 {
				return fmt.Errorf("levels[%d] and [%d] are %.2f apart — duplicates within the cluster tolerance; collapse them into one entry",
					i, j, math.Abs(d.Levels[i].Price-d.Levels[j].Price))
			}
		}
	}
	// P0.1-relax — both-side minimum (see doc above).
	below, above := 0, 0
	for _, l := range d.Levels {
		switch {
		case l.Price < facts.Price:
			below++
		case l.Price > facts.Price:
			above++
		}
	}
	if machine != nil && len(machine) == 0 {
		// Empty machine map = the true safety floor: never write a plan the
		// system cannot vouch for. (The write site always passes the
		// prompt-visible map, so this is the fail-closed escape hatch.)
		return fmt.Errorf("machine level map is EMPTY at price %.2f — refusing to validate a plan against no universe (never stale, never uncalibrated)", facts.Price)
	}
	// OWNER RULING 2026-08-31 — the per-side COUNT concept is deleted. The
	// only surviving side guard is the data-earned 0-on-a-side hard fail
	// (the 2026-08-18 one-sided-map pathology). No quota, no WARN, no note.
	if below == 0 {
		return fmt.Errorf("0 levels below price %.2f — a plan must map both directions (the 2026-08-18 one-sided-map pathology)", facts.Price)
	}
	if above == 0 {
		return fmt.Errorf("0 levels above price %.2f — a plan must map both directions (the 2026-08-18 one-sided-map pathology)", facts.Price)
	}
	// P0.2 — continuation scenario on a gap out of the prior range.
	if facts.PDL > 0 && facts.Price < facts.PDL && !hasDirection(d.Scenarios, "short") {
		return fmt.Errorf("price %.2f is BELOW PDL %.2f (gap-down) — %s", facts.Price, facts.PDL, gapDownDirectionMessage)
	}
	if facts.PDH > 0 && facts.Price > facts.PDH && !hasDirection(d.Scenarios, "long") {
		return fmt.Errorf("price %.2f is ABOVE PDH %.2f (gap-up) — %s", facts.Price, facts.PDH, gapUpDirectionMessage)
	}
	// P0.2-c — the continuation scenario must be REACHABLE from here: a gap-down
	// short whose trigger needs a rally back above price (the 2026-08-18 S3
	// pathology: "rally into 29853/29919" while price sat at 29687) is not a
	// continuation play, it is a re-entry into the old range. The trigger's
	// nearest numeric level must sit AT or beyond price in the gap direction.
	if facts.PDL > 0 && facts.Price < facts.PDL {
		if !continuationReachable(d.Scenarios, "short", facts.Price) {
			return fmt.Errorf("gap-down at %.2f (< PDL %.2f): the short scenario's trigger must reference a level ≤ current price (breakdown/retest), not a rally back above", facts.Price, facts.PDL)
		}
	}
	if facts.PDH > 0 && facts.Price > facts.PDH {
		if !continuationReachable(d.Scenarios, "long", facts.Price) {
			return fmt.Errorf("gap-up at %.2f (> PDH %.2f): the long scenario's trigger must reference a level ≥ current price (breakout/retest), not a dip back below", facts.Price, facts.PDH)
		}
	}
	// P0.2b — targets must be reachable: inside the proximity band.
	band := 1.5 * facts.DATR
	if band <= 0 {
		band = 0.012 * facts.Price // warm-up fallback
	}
	for i, s := range d.Scenarios {
		for _, t := range s.TargetChain {
			if math.Abs(t-facts.Price) > band {
				return fmt.Errorf("scenario[%d] target %.2f is %.0f pts from price %.2f — outside the %.0f-pt proximity band (unreachable target)", i, t, math.Abs(t-facts.Price), facts.Price, band)
			}
		}
	}
	return nil
}

func hasDirection(scenarios []PlanScenario, dir string) bool {
	for _, s := range scenarios {
		if s.Direction == dir {
			return true
		}
	}
	return false
}

// triggerNumbers extracts the numeric levels (≥3 digits, >100 — filters clock
// times like "08:35") mentioned in a trigger's prose.
func triggerNumbers(trigger string) []float64 {
	var out []float64
	for _, m := range reTriggerNumber.FindAllString(trigger, -1) {
		// A4-consistent (fail-register wave): any positive decimal counts — the
		// old v>100 floor made sub-1000-priced instruments unminable. Callers
		// that need price-magnitude filtering do it themselves
		// (continuationReachable keeps its beyond-price comparison).
		if v, err := strconv.ParseFloat(m, 64); err == nil && v > 0 {
			out = append(out, v)
		}
	}
	return out
}

var reTriggerNumber = regexp.MustCompile(`\d+(?:\.\d+)?`)

// continuationReachable reports whether at least one scenario in `dir` has a
// trigger whose nearest numeric level sits AT or beyond price in that direction
// (a play reachable without crossing the whole map).
func continuationReachable(scenarios []PlanScenario, dir string, price float64) bool {
	for _, s := range scenarios {
		if s.Direction != dir {
			continue
		}
		for _, n := range triggerNumbers(s.Trigger) {
			// Price-magnitude band (A4-consistent widening moved the >100
			// filter out of the miner): only numbers in the same magnitude as
			// price count as price references — "2x5m"-style vocabulary
			// digits ("2", "5", "15") can never satisfy reachability.
			if n < price*0.5 || n > price*1.5 {
				continue
			}
			if dir == "short" && n <= price {
				return true
			}
			if dir == "long" && n >= price {
				return true
			}
		}
	}
	return false
}

// extractJSONObject returns the substring from the first '{' to the matching
// last '}' (brace-balanced), or "" if none.
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// NoTradePlanDoc builds the FAIL-CLOSED no-trade plan: a valid plan with a
// neutral bias and a single "no-trade" scenario, so a read failure still writes a
// concrete NO-TRADE plan row (never a stale plan, never nothing).
func NoTradePlanDoc(reason string) *PlanDoc {
	return &PlanDoc{
		Reasoning:      "FAIL-CLOSED: " + reason + " — no valid plan produced; sitting out this session.",
		Bias:           PlanBias{Direction: "neutral", Conviction: "low", FlipCondition: "n/a"},
		Levels:         nil,
		Scenarios:      []PlanScenario{{ID: "S0", Trigger: "none", Condition: "hold", Direction: "long", Invalid: "n/a", Quality: "B"}},
		NoTrade:        []string{"ENTIRE SESSION — planner fail-closed"},
		DeathCondition: "already dead (fail-closed)",
		DayType:        "no-trade",
	}
}

// NoTradePlanDocWithLevels is the P7 form: a NO-TRADE plan that still CARRIES THE
// MAP. Levels are market FACTS; the plan is an opinion about them — a no-trade
// decision must never erase the map. The caller passes the current
// detector/scorer output; when genuinely unavailable, the doc says so explicitly
// (an empty levels section must never render without a reason).
func NoTradePlanDocWithLevels(reason string, levels []PlanLevel) *PlanDoc {
	doc := NoTradePlanDoc(reason)
	if len(levels) == 0 {
		doc.NoTrade = append(doc.NoTrade, "detector data unavailable — no level map could be assembled")
		return doc
	}
	// Level-truth wave (2026-08-27) — the NO-TRADE doc is MACHINE-AUTHORED:
	// every level's grade IS the machine grade. Stamp it explicitly so no-trade
	// plans stop contributing unstamped rows to the card (256/795 regression).
	for i := range levels {
		if levels[i].MachineGrade == "" {
			levels[i].MachineGrade = levels[i].Grade
		}
	}
	doc.Levels = levels
	return doc
}

// StampMachineGrades (level-truth wave, 2026-08-27) stamps doc levels from a
// rounded-price → grade map (the write site's machineGrades). Returns how many
// rows it stamped. Pure — the write site and the golden regression test share
// this exact stamping so the test IS the write path.
//
// T2 root-cause (forensics hygiene 2026-08-28): the model carries 3dp prices
// (e.g. 29541.125, tick-fraction levels) TRUNCATED to 2dp into the doc
// (29541.12), while the map keys round half-up (29541.13) — exact-key lookup
// missed the (HTF)-carried rows (2/12 unstamped). A ±0.011 tolerance fallback
// covers the truncation class; real levels sit ≥0.25 apart, so it can never
// collide.
func StampMachineGrades(doc *PlanDoc, grades map[float64]string) int {
	if doc == nil || len(grades) == 0 {
		return 0
	}
	n := 0
	for i := range doc.Levels {
		if doc.Levels[i].MachineGrade != "" {
			continue
		}
		p := doc.Levels[i].Price
		if g, ok := grades[math.Round(p*100)/100]; ok && g != "" {
			doc.Levels[i].MachineGrade = g
			n++
			continue
		}
		for k, g := range grades {
			if g != "" && math.Abs(k-p) <= 0.011 {
				doc.Levels[i].MachineGrade = g
				n++
				break
			}
		}
	}
	return n
}

// CarryMachineGrades (level-truth wave, 2026-08-27) stamps doc levels from the
// PREVIOUS version's levels (rounded-price → grade, strongest wins on
// collisions). Returns how many rows it carried. Pure — shared with
// AutoTrader.carryMachineGrades. Same T2 truncation tolerance as
// StampMachineGrades (3dp source prices carried into the doc at 2dp).
func CarryMachineGrades(doc *PlanDoc, prior []PlanLevel) int {
	if doc == nil || len(prior) == 0 {
		return 0
	}
	carry := map[float64]string{}
	for _, l := range prior {
		if l.Price <= 0 {
			continue
		}
		g := l.MachineGrade
		if g == "" {
			g = l.Grade
		}
		if g == "" {
			continue
		}
		k := math.Round(l.Price*100) / 100
		if old, ok := carry[k]; !ok || GradeRank(g) > GradeRank(old) {
			carry[k] = g
		}
	}
	if len(carry) == 0 {
		return 0
	}
	n := 0
	for i := range doc.Levels {
		if doc.Levels[i].MachineGrade != "" {
			continue
		}
		p := doc.Levels[i].Price
		if g, ok := carry[math.Round(p*100)/100]; ok {
			doc.Levels[i].MachineGrade = g
			n++
			continue
		}
		for k, g := range carry {
			if math.Abs(k-p) <= 0.011 {
				doc.Levels[i].MachineGrade = g
				n++
				break
			}
		}
	}
	return n
}

// scenarioIDRe — A5 (F11): "S1".."S99", the convention everything keys on.
// S0 is reserved for the Go-authored fail-closed NO-TRADE stub plan.
var scenarioIDRe = regexp.MustCompile(`^S\d{1,2}$`)

// numberNearInText — A3 (F5): does any number token in the prose sit within
// tol points of want? Reuses the trigger-mining tokenizer.
func numberNearInText(text string, want, tol float64) bool {
	for _, v := range triggerNumbers(text) {
		if v >= want-tol && v <= want+tol {
			return true
		}
	}
	return false
}

// confirmRules — C1 vocabulary. 1x5m_close maps to the A2-fixed "5m-close"
// acceptance rule; 2x5m_close → "2x5m". E1 (entry-mechanics 2026-08-30):
// 15m_close REMOVED (dead variant — rejected with confirm_rule_15m_removed);
// 1m_mss (E5) and time_hold (E6) added as machine primitives.
var confirmRules = map[string]bool{"touch": true, "1x5m_close": true, "2x5m_close": true, "1m_mss": true, "time_hold": true}

// confirmRuleMentions15m reports whether a rule spelling is (or normalizes
// toward) the dead 15m variant — E1's named rejection key.
func confirmRuleMentions15m(rule string) bool {
	switch strings.ToLower(strings.TrimSpace(rule)) {
	case "15m", "15m-close", "15mclose", "15m_close", "1x15m", "1x15m_close":
		return true
	}
	return false
}

// ArmNormalization (CLASS 39, owner ruling 2026-09-01) records ONE
// normalize-don't-reject event: on a non-sweep_reclaim condition the authored
// arm carried a legs array, the array was dropped, and the remaining single
// (top-level) arm validated. DroppedLegs is exactly what the model authored;
// Kept* is exactly what the machine kept — nothing is synthesized.
type ArmNormalization struct {
	Scenario    string       `json:"scenario"`
	Condition   string       `json:"condition"`
	DroppedLegs []PlanArmLeg `json:"dropped_legs"`
	KeptEntry   float64      `json:"kept_entry"`
	KeptStop    float64      `json:"kept_stop"`
	KeptTarget  float64      `json:"kept_target"`
	KeptRule    string       `json:"kept_rule"` // the scenario's confirm rule the single arm chains on ("" when none)
	KeptWait    bool         `json:"kept_wait_confirm"`
}

// normalizeArmLegs — CLASS 39, Section B of the owner ruling, applied
// literally:
//
//  1. on a NON-sweep_reclaim condition with ANY legs array (one leg or many):
//     DROP the array;
//  2. RE-RUN the full arm validation (ArmSpecValid) on what remains — the
//     top-level entry / stop / target / wait_confirm;
//  3. VALID → keep the single arm and record the normalization (the write
//     site emits the ⚖ WARN, the E8 row is stamped, the counter is bumped);
//  4. STILL INVALID → leave the scenario UNCHANGED, so validateArmSpecs emits
//     the ORIGINAL reason to the model's retry. No second normalization pass.
//
// NEVER synthesizes a leg. NEVER touches sweep_reclaim — one leg there stays a
// reject (rows 69-S2 / 85-S1); a legal two-leg split passes through untouched.
// Runs from NormalizePlanDocRules, i.e. before validateArmSpecs on every
// parse/validate path. Idempotent: a stored doc whose legs are already gone
// is a no-op and its recorded normalizations are preserved.
func normalizeArmLegs(d *PlanDoc) {
	if d == nil {
		return
	}
	for i := range d.Scenarios {
		sc := &d.Scenarios[i]
		if sc.Arm == nil || len(sc.Arm.Legs) == 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(sc.Condition), "sweep_reclaim") {
			continue // D7 — the split contract is never normalized
		}
		trial := *sc
		single := *sc.Arm
		single.Legs = nil
		trial.Arm = &single
		if err := ArmSpecValid(trial); err != nil {
			continue // step 4 — unchanged; the original reason surfaces downstream
		}
		rec := ArmNormalization{
			Scenario: sc.ID, Condition: sc.Condition,
			DroppedLegs: append([]PlanArmLeg(nil), sc.Arm.Legs...), // a COPY of what was authored — never a new leg
			KeptEntry:   sc.Arm.Entry, KeptStop: sc.Arm.Stop, KeptTarget: sc.Arm.Target, KeptWait: sc.Arm.WaitConfirm,
		}
		if sc.Confirm != nil {
			rec.KeptRule = sc.Confirm.Rule
		}
		sc.Arm.Legs = nil // step 1, in place — the top-level arm IS the single arm
		d.ArmNormalizations = append(d.ArmNormalizations, rec)
	}
}

// ArmNormalizationWarn renders the class-39 write-site WARN (D2): the
// condition, the scenario, every dropped leg, and the single arm that was kept.
// Pure, so the fixture pins the wording the journal will carry.
func ArmNormalizationWarn(n ArmNormalization) string {
	parts := make([]string, 0, len(n.DroppedLegs))
	for i, l := range n.DroppedLegs {
		r := l.Rule
		if r == "" {
			r = "-"
		}
		parts = append(parts, fmt.Sprintf("#%d entry=%.2f stop=%.2f target=%.2f rule=%s", i+1, l.Entry, l.Stop, l.Target, r))
	}
	kr := n.KeptRule
	if kr == "" {
		kr = "-"
	}
	return fmt.Sprintf("⚖ arm normalized (class 39): %s %s — dropped legs[%d] (%s); single arm kept entry=%.2f stop=%.2f target=%.2f rule=%s wait_confirm=%t",
		n.Condition, n.Scenario, len(n.DroppedLegs), strings.Join(parts, "; "), n.KeptEntry, n.KeptStop, n.KeptTarget, kr, n.KeptWait)
}

// ArmNormalizationFor returns the recorded class-39 normalization for a
// scenario id, or nil — the E8 stamp lookup at arm time.
func ArmNormalizationFor(d *PlanDoc, scenarioID string) *ArmNormalization {
	if d == nil {
		return nil
	}
	for i := range d.ArmNormalizations {
		if d.ArmNormalizations[i].Scenario == scenarioID {
			return &d.ArmNormalizations[i]
		}
	}
	return nil
}

// DroppedLegsJSON renders a normalization's dropped legs for the E8 column
// (dropped_legs). "" when there is nothing to record.
func DroppedLegsJSON(n *ArmNormalization) string {
	if n == nil || len(n.DroppedLegs) == 0 {
		return ""
	}
	b, err := json.Marshal(n.DroppedLegs)
	if err != nil {
		return ""
	}
	return string(b)
}

// CLASS 45 (2026-09-02) — the RULE tests direction only (hasDirection), but the
// MESSAGE used to name conditions ("continuation/breakdown short"), which is
// part of what taught the model to reach for breakdown_continue at a void
// level. Message and rule now use the same words.
const (
	gapDownDirectionMessage = "the plan MUST include a SHORT-direction scenario (ANY legal condition)"
	gapUpDirectionMessage   = "the plan MUST include a LONG-direction scenario (ANY legal condition)"
)
