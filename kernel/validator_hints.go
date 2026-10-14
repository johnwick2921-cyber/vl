package kernel

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// CLASS 34 (owner ruling 2026-08-31) — validator hints must name only legal
// conditions. Tonight both ASIA chains failed: the breakdown-void reject said
// "author a reject/retest play instead", the model authored condition
// "reject_retest", and parse/schema rejected it — the model complied with the
// hint and was punished for it. A hint is an instruction; instructions must be
// checkable. This registry pins every hint's condition tokens against the enum
// and the default shadow map (the table test IS the guard; the boot line
// re-runs it at startup).

// BreakdownReclaimedHint is the remediation phrase for a void breakdown (a
// close came back across the breakdown level).
const BreakdownReclaimedHint = "author a `reject` play instead (do NOT combine condition names; `reject_retest` is not a valid condition)"

// BreakdownDisplacementHint is the remediation phrase for a sub-BD_MIN_DISP_ATR
// waterfall authoring.
const BreakdownDisplacementHint = "author the conditional thesis as an `acceptance` play instead — confirm 1x5m_close beyond the level (side below for a short, above for a long), arm wait_confirm:true — the machine sends nothing until that close prints (do NOT combine condition names; `reject_retest` is not a valid condition)"

// ArmLegsSplitContract is the arm-legs condition contract fragment
// (sweep_reclaim only splits).
const ArmLegsSplitContract = "(the split entry is the sweep_reclaim contract; other conditions arm single)"

// RepairBreakdownLaw is the BREAKDOWN-CONTINUE law excerpt for repair prompts.
const RepairBreakdownLaw = "BREAKDOWN-CONTINUE LAW: the breakdown is void once a close comes back across the breakdown level — author a `reject` play instead of breakdown_continue (do NOT combine condition names; `reject_retest` is not a valid condition)."

// RepairArmSplitLaw is the ARM-SPLIT law excerpt for repair prompts.
const RepairArmSplitLaw = "ARM-SPLIT LAW: a scenario with a split contract arm needs EXACTLY 2 legs — leg 1 is authored AT the sweep ref (wait_confirm false) with confirm=touch at the sweep ref; leg 2 is 1m_mss (1x5m_close accepted as the leg-2 alternative). The machine holds BOTH legs until the whole confirm (the sweep touch AND confirm2) is MET, so leg 1 rests as a retest limit after the reclaim. Only sweep_reclaim conditions arm split; every other condition arms a SINGLE leg."

// RepairEntryConfirmLaw is the ENTRY-LAW CONFIRM excerpt for repair prompts.
const RepairEntryConfirmLaw = "ENTRY-LAW CONFIRM LAW: breakdown_continue takes 1 confirming close + displacement >= BD_MIN_DISP_ATR x ATR5m OR stop-entry (E7); 2x5m_close is legal ONLY there. confirm2 mirrors confirm1 unless the law above allows it."

// RepairConfirmVocabLaw (REPAIR-PARSE, 2026-09-02) is the excerpt for a
// rejected confirm/confirm2 RULE TOKEN. It is the single most common repair
// defect: 10 of 18 repair rejections in the 2026-09-01 journals were a confirm
// rule token that does not exist in that field's enum ("2x5m", "displacement")
// or a token illegal for that condition. Before this the repair prompt routed
// those to a generic excerpt about level labels and targets — the model was
// told it was wrong and never told the legal words. Class 38's rule holds: the
// confirm enum and the death/flip enum are SEPARATE vocabularies and are named
// as such, never as one list of bare tokens.
const RepairConfirmVocabLaw = "CONFIRM-RULE VOCABULARY: the `confirm.rule` and `confirm2.rule` fields take EXACTLY one of touch | 1x5m_close | 2x5m_close | 1m_mss | time_hold. The death/flip `rule` field is a DIFFERENT vocabulary (2x5m | 5m_close) — a token from it is INVALID in confirm/confirm2, and words like `displacement` are not tokens in either. Re-spell the rejected token as one of the five confirm values that matches the play."

// RepairFlipDirectionLaw (W-FLIP-DIRECTION, 2026-09-17) is the excerpt for a
// flip whose side points the wrong way for the bias it flips from. LONDON v3
// (2026-09-17) shipped bias short + flip{below → long}; the number matched the
// prose and nothing asked the direction. The model is told the law it is
// judged by, in the words the validator uses.
const RepairFlipDirectionLaw = "FLIP DIRECTION: flip side must oppose the bias: short bias flips long on a close ABOVE; long bias flips short on a close BELOW. `flip.side` is the side of the line price must CLOSE on for the bias to reverse — a short bias with `flip.side: below` can never flip on a rally. Fix the side (or the flip_to), never the death object."

// RepairFlipSideOfPriceLaw (W-FLIP-LINE-SIDE-OF-PRICE, 2026-09-17) is the
// excerpt for a flip or death line that already sits beyond price on its own
// side. ASIA v2 (2026-09-17 22:52 CT) shipped bias short + flip{29747.50 above
// → long} with price 29764: the direction matched the bias and nothing asked
// where price was. The touch gate fires a line only from the near side after
// birth, so that flip could never fire. The model is told the law in the
// validator's words.
const RepairFlipSideOfPriceLaw = "LINE SIDE OF PRICE: a flip line must sit on the far side of price at authoring — `flip.side: above` needs the line ABOVE the current price, `flip.side: below` needs it BELOW. The machine fires a line only after price touches it from the near side and then closes beyond it, so a line already beyond price on its own side can never be touched from the near side and never fires; the death line obeys the same law (a death line already crossed is a plan born dead). Move the line to the far side of the current price (keep the side the bias requires); never flip the bias to satisfy it."

// RepairInvalidationGrammarLaw (W-EXEC-TRUTH W2 A1, 2026-09-23) is the excerpt
// for the combined grammar refusal. Row 455 authored "Any 5m close above
// 31075.75 invalidates the fade; stand aside for the breakout." — accepted as
// UNKNOWN before this wave. The model is told the grammar in the forms the
// validator parses; the sentence is prose, so only 2x5m is a scanned token.
const RepairInvalidationGrammarLaw = "INVALIDATION GRAMMAR: every scenario's `invalid` is machine-checked at write and must be EXACTLY one of `5m close above <price>` | `5m close below <price>` | `2x5m close above <price>` | `2x5m close below <price>` — one plain number and nothing else (no `any`, `back`, `then`, no second clause, no other timeframe). A sentence outside the grammar is REFUSED, never accepted. Rewrite each quoted line into one of the four forms, keeping its price and side; change nothing else."

// HintRuleField names WHICH enum a hint's rule tokens are drawn from. The same
// spelling can be legal in one field and illegal in another: "2x5m" is a legal
// death/flip rule (conditionRules) and an ILLEGAL confirm rule (confirmRules).
// Class 38 rows 78 → 79: a confirm-field instruction named the death/flip
// spelling, the model copied it into confirm2.rule, and the schema rejected it.
type HintRuleField string

const (
	// HintFieldNone — the hint names no rule token at all; any token found in
	// its text is a defect by construction.
	HintFieldNone HintRuleField = ""
	// HintFieldConfirmRule — confirm{}/confirm2{}.rule and arm leg rules:
	// touch | 1x5m_close | 2x5m_close | 1m_mss | time_hold.
	HintFieldConfirmRule HintRuleField = "confirm.rule"
	// HintFieldConditionRule — death{}/flip{}.rule: 2x5m | 5m_close.
	HintFieldConditionRule HintRuleField = "death/flip.rule"
	// HintFieldRelation — relation_d / relation_4h values:
	// with-trend | counter-trend | range (S3, 2026-09-16).
	HintFieldRelation HintRuleField = "relation"
	// HintFieldInvalidGrammar — scenario.invalid prose (W2 A1, 2026-09-23):
	// the only scanned token its grammar names is 2x5m ("5m close" is prose).
	HintFieldInvalidGrammar HintRuleField = "scenario.invalid"
)

// invalidGrammarTokens are the rule-shaped tokens the invalid grammar may name.
var invalidGrammarTokens = map[string]bool{"2x5m": true}

// ValidatorHint pairs a validator message/hint site with the enum tokens its
// text names: condition names (class 34) and rule tokens (class 38).
type ValidatorHint struct {
	Site       string
	Text       string
	Conditions []string      // every LEGAL condition name the text mentions
	RuleField  HintRuleField // which rule enum this hint's tokens belong to
}

// ruleTokenScan finds rule-shaped tokens in hint prose. \b…\b means a longer
// token is never seen as its own prefix: "2x5m_close" does NOT match \b2x5m\b
// because "_" is a word character, so only genuinely bare spellings trip the
// guard. Ordered longest-first so the leftmost-first alternation cannot split a
// legal token. "touch" is included: it is a legal confirm rule, so it only ever
// fails inside a death/flip hint, which is exactly the cross-field defect.
var ruleTokenScan = regexp.MustCompile(`\b(2x5m_close|1x5m_close|15m_close|time_hold|1m_mss|5m_close|5m-close|2x_5m|1x15m|2x5m|1x5m|5mclose|touch|15m|2x)\b`)

// relationShapeScan finds any *-trend token in hint prose (S3): a hint naming a
// relation value must only name the enum (with-trend | counter-trend | range) —
// an invented "against-trend" spelling is the same disease as reject_retest.
var relationShapeScan = regexp.MustCompile(`\b([a-z]+-trend)\b`)

// legalRuleTokens returns the enum a field's tokens must come from.
func legalRuleTokens(field HintRuleField) map[string]bool {
	switch field {
	case HintFieldConfirmRule:
		return confirmRules
	case HintFieldConditionRule:
		return conditionRules
	case HintFieldInvalidGrammar:
		return invalidGrammarTokens
	}
	return nil
}

// validateHintTokens is the CLASS 38 guard: every rule-shaped token in a hint's
// text must be a member of that hint's own field enum. A hint is an
// instruction; an instruction naming a token its field cannot hold punishes the
// model for complying (rows 78 → 79).
func validateHintTokens(h ValidatorHint) error {
	if h.RuleField == HintFieldRelation {
		for _, tok := range relationShapeScan.FindAllString(h.Text, -1) {
			if !relationValues[strings.ToLower(tok)] {
				return fmt.Errorf("validator hint %q names %q, which is not a legal %s (enum: with-trend | counter-trend | range) — a hint must never name a token its own field rejects", h.Site, tok, h.RuleField)
			}
		}
		return nil
	}
	found := ruleTokenScan.FindAllString(h.Text, -1)
	if len(found) == 0 {
		return nil
	}
	legal := legalRuleTokens(h.RuleField)
	if legal == nil {
		return fmt.Errorf("validator hint %q names rule token(s) %v but declares no rule field (RuleField is unset) — every token must be checkable against an enum", h.Site, dedupeTokens(found))
	}
	for _, tok := range found {
		if !legal[tok] {
			return fmt.Errorf("validator hint %q names %q, which is not a legal %s (enum: %s) — a hint must never name a token its own field rejects", h.Site, tok, h.RuleField, sortedTokens(legal))
		}
	}
	return nil
}

func dedupeTokens(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, t := range in {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func sortedTokens(set map[string]bool) string {
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return strings.Join(out, "|")
}

// ValidatorHints is the class-34 registry. Every hint shipped in a validator
// message or a repair excerpt MUST be listed here; the guard test asserts each
// token is in the enum and is not shadowed by default.
func ValidatorHints() []ValidatorHint {
	out := []ValidatorHint{
		{Site: "breakdown_continue.go reclaimed", Text: BreakdownReclaimedHint, Conditions: []string{"reject"}},
		{Site: "breakdown_continue.go displacement", Text: BreakdownDisplacementHint, Conditions: []string{"acceptance"}, RuleField: HintFieldConfirmRule},
		{Site: "plan_doc.go arm-legs contract", Text: ArmLegsSplitContract, Conditions: []string{"sweep_reclaim"}},
		{Site: "planner_repair.go breakdown law", Text: RepairBreakdownLaw, Conditions: []string{"reject", "breakdown_continue"}},
		{Site: "planner_repair.go arm-split law", Text: RepairArmSplitLaw, Conditions: []string{"sweep_reclaim"}, RuleField: HintFieldConfirmRule},
		{Site: "planner_repair.go entry-law confirm", Text: RepairEntryConfirmLaw, Conditions: []string{"breakdown_continue"}, RuleField: HintFieldConfirmRule},
		// S3 (2026-09-16) — the structure relation vocabulary: the prompt names
		// the relation enum, and the guard checks every *-trend token against it.
		{Site: "structure_relation.go counter-trend hint", Text: CounterTrendRelationHint, RuleField: HintFieldRelation},
		{Site: "planner_repair.go structure relation law", Text: RepairStructureRelationLaw, RuleField: HintFieldRelation},
		// W-FLIP-DIRECTION (2026-09-17) — names no rule token; guarded so a
		// later edit that adds one is checked against the death/flip enum.
		{Site: "planner_repair.go flip direction law", Text: RepairFlipDirectionLaw, RuleField: HintFieldConditionRule},
		// W-FLIP-LINE-SIDE-OF-PRICE (2026-09-17) — same guard, same reason.
		{Site: "planner_repair.go flip side-of-price law", Text: RepairFlipSideOfPriceLaw, RuleField: HintFieldConditionRule},
		// W-EXEC-TRUTH W2 A5 (2026-09-23) — the stored-duration refusal and its
		// repair excerpt name confirm-field tokens only (time_hold).
		{Site: "confirm_resolver.go hold_min prose", Text: ConfirmHoldMinHint, RuleField: HintFieldConfirmRule},
		{Site: "planner_repair.go hold_min law", Text: RepairHoldMinLaw, RuleField: HintFieldConfirmRule},
		// W2 A1 (2026-09-23) — the invalidation grammar law; field-scoped to
		// scenario.invalid so a later edit naming a confirm token fails here.
		{Site: "planner_repair.go invalidation grammar law", Text: RepairInvalidationGrammarLaw, RuleField: HintFieldInvalidGrammar},
		// W-EXEC-TRUTH W2 A3/A4 — the identity law names the two-anchor
		// condition and speaks of confirm legs; neither law names a rule token,
		// and a later edit that adds one is checked against the confirm enum.
		{Site: "planner_repair.go identity=price law", Text: RepairIdentityPriceLaw, Conditions: []string{"sweep_reclaim"}, RuleField: HintFieldConfirmRule},
		{Site: "planner_repair.go obstacle-chain law", Text: RepairObstacleChainLaw, RuleField: HintFieldConfirmRule},
		// W-EXEC-TRUTH W3 (2026-09-23) — the zone law names confirm-rule tokens
		// (touch, time_hold, 1m_mss); the hold-floor law names time_hold and
		// the two conditions it arms (acceptance, hold — both live by default).
		{Site: "entry_policy.go entry zone law", Text: RepairEntryZoneLaw, RuleField: HintFieldConfirmRule},
		{Site: "entry_policy.go armable hold floor law", Text: RepairArmableHoldFloorLaw, Conditions: []string{"acceptance", "hold"}, RuleField: HintFieldConfirmRule},
	}
	// CLASS 38 — the entry law Style strings are quoted VERBATIM into the
	// rejection the model reads ("… not allowed for %s — entry law: %s"), so
	// they ARE hints and must be guarded. Row 78 was born in this table while
	// the class-34 guard (conditions only) stayed green.
	for _, cond := range KnownConditions() {
		law, ok := EntryLawFor(cond)
		if !ok {
			continue
		}
		out = append(out, ValidatorHint{
			Site:      "entry_law.go Style:" + cond,
			Text:      law.Style,
			RuleField: HintFieldConfirmRule,
		})
	}
	return out
}

// ValidateValidatorHints is the class-34 guard: every condition token a hint
// names must exist in the enum and must not be shadowed by default. The table
// test is the hard build gate; the boot line re-runs this at startup.
func ValidateValidatorHints() error {
	known := make(map[string]bool, len(KnownConditions()))
	for _, c := range KnownConditions() {
		known[c] = true
	}
	resolved := ResolvedConditionStatuses(nil, nil, "") // defaults — the static contract
	for _, h := range ValidatorHints() {
		for _, c := range h.Conditions {
			if !known[c] {
				return fmt.Errorf("validator hint %q names unknown condition %q (enum: %v)", h.Site, c, KnownConditions())
			}
			if resolved[c] == ConditionShadow {
				return fmt.Errorf("validator hint %q names shadowed condition %q", h.Site, c)
			}
		}
		// CLASS 38 — the rule-token half of the same law.
		if err := validateHintTokens(h); err != nil {
			return err
		}
	}
	return nil
}

// ResolvedLiveConditions returns the sorted list of conditions that resolve
// LIVE for the given base/session maps + env — the vocabulary appended to the
// planner reject block (class 34, fix 5).
func ResolvedLiveConditions(base, session map[string]string, env string) []string {
	resolved := ResolvedConditionStatuses(base, session, env)
	out := make([]string, 0, len(resolved))
	for c, st := range resolved {
		if st != ConditionShadow {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

// LiveConditionsLine renders the "Valid conditions: [...]" reject-block suffix.
func LiveConditionsLine(live []string) string {
	if len(live) == 0 {
		return ""
	}
	return fmt.Sprintf("\nValid conditions: [%s] (use exactly ONE token from this list; do NOT combine condition names).", strings.Join(live, ", "))
}
