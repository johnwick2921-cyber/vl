// Settings integrity (D6) — resolvers that report WHERE the value came from.
//
// The UI renders "saved → resolved · source". That line is only honest if the
// source is produced by the same function production resolves with; a second
// copy in the API layer would drift the moment either side changed, and the
// page would then narrate a rule the engine does not follow.
//
// So the rule lives here once, and the shipped entry points (resolvedMinRR,
// HTFVetoEnabled, PlanModeFor) delegate to these. Canon 28: one resolver,
// called where the value enters.

package store

import (
	"os"
	"strconv"
	"strings"
)

// Where a resolved value came from. These strings are rendered to the operator,
// so they say what happened, not which branch ran.
const (
	SourceSaved           = "saved value"
	SourceSchemaDefault   = "schema default"
	SourceShippedDefault  = "shipped default"
	SourceStrategyValue   = "strategy value"
	SourceSessionOverride = "session override"
	// SourceEnv prefixes a value read from a process environment variable; the
	// variable's name follows ("env BREAKER_HALT_N").
	SourceEnv = "env"
)

// OriginLetter maps a resolver source onto the boot-line evidence tag (W1):
// [O] the owner set it (saved / strategy / session), [E] the process env set
// it, [I] the shipped default. ONE mapping, so two boot lines cannot tag the
// same source two ways. An unrecognised source reads [?] — never a guess.
func OriginLetter(source string) string {
	switch {
	case strings.HasPrefix(source, SourceSaved),
		strings.HasPrefix(source, SourceStrategyValue),
		strings.HasPrefix(source, SourceSessionOverride):
		return "[O]"
	case strings.HasPrefix(source, SourceEnv+" "):
		return "[E]"
	case strings.HasPrefix(source, SourceShippedDefault),
		strings.HasPrefix(source, SourceSchemaDefault):
		return "[I]"
	}
	return "[?]"
}

// BreakerHaltDefault is the consecutive-loss halt's shipped N — vet-06's [I]
// proposal (8 of the last 10 losing). It never fires on the retained tape (max
// run 7, ids 585-591); trader/session_risk.go carries the measurement.
const BreakerHaltDefault = 8

// BreakerHaltMentorDefault is the consecutive-loss halt's shipped N for a
// MENTOR-MODE strategy (risk_control.mentor_mode=true). [Oct D4.1 @ 14:40]
// OctD4.1-R28 / R19: "thua 3 lenh lien tuc, lenh thu 4 thu 1 lenh nua, neu
// khong duoc thi tat may nghi" — 3 losses, one more try on the 4th, then shut
// the machine off: 4 consecutive losses halt the 5th entry. AI-mode strategies
// keep BreakerHaltDefault (8).
const BreakerHaltMentorDefault = 4

// BreakerHaltEnv is the process-wide override consulted only when the strategy
// saved no value.
const BreakerHaltEnv = "BREAKER_HALT_N"

// ReplanCapDefault is the shipped re-plan cap when neither the session nor the
// strategy saved one.
const ReplanCapDefault = 2

// ResolveBreakerHalt resolves the consecutive-loss halt N and where it came
// from (W1, settings truth). Precedence: the SAVED value — INCLUDING 0, which
// is OFF — → env BREAKER_HALT_N (valid ≥0; 0 = OFF) → 4 for a mentor_mode strategy
// [Oct D4.1 @ 14:40] → the shipped default 8.
// An invalid env value falls to the default AND SAYS SO in the source, never a
// silent 8. Absent in the strategy means INHERIT, never OFF: the breaker is a
// safety default, so a missing key must not read as "off".
func ResolveBreakerHalt(cfg *StrategyConfig) (int, string) {
	return ResolveBreakerHaltEnv(cfg, os.Getenv)
}

// ResolveBreakerHaltEnv is ResolveBreakerHalt with the environment injected —
// the migration report evaluates a fixed environment, and tests pin one.
func ResolveBreakerHaltEnv(cfg *StrategyConfig, getenv func(string) string) (int, string) {
	if cfg != nil && cfg.RiskControl.ConsecutiveLossHalt != nil {
		if n := *cfg.RiskControl.ConsecutiveLossHalt; n >= 0 {
			return n, SourceSaved
		}
		// A negative saved value is not a threshold. Fall through to what an
		// absent key would resolve to, and name why.
		n, src := breakerHaltFromEnv(cfg, getenv)
		return n, src + " (saved value is negative)"
	}
	return breakerHaltFromEnv(cfg, getenv)
}

func breakerHaltFromEnv(cfg *StrategyConfig, getenv func(string) string) (int, string) {
	def := BreakerHaltDefault
	if cfg != nil && cfg.RiskControl.MentorMode {
		def = BreakerHaltMentorDefault // [Oct D4.1 @ 14:40]
	}
	n, state := BreakerHaltEnvState(getenv)
	switch state {
	case EnvValid:
		return n, SourceEnv + " " + BreakerHaltEnv
	case EnvInvalid:
		return def, SourceShippedDefault + " (env " + BreakerHaltEnv + " invalid)"
	}
	return def, SourceShippedDefault
}

// Env states for a numeric process override.
const (
	EnvUnset   = "unset"
	EnvValid   = "valid"
	EnvInvalid = "invalid"
)

// BreakerHaltEnvState reads BREAKER_HALT_N once: unset, valid (an integer ≥0)
// or invalid. The same test envInt applied before W1 (Atoi, ≥0), so the
// pre-W1 and post-W1 readings of one environment agree.
func BreakerHaltEnvState(getenv func(string) string) (int, string) {
	if getenv == nil {
		getenv = os.Getenv
	}
	v := getenv(BreakerHaltEnv)
	if v == "" {
		return 0, EnvUnset
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, EnvInvalid
	}
	return n, EnvValid
}

// ResolveReplanCap resolves the per-session re-plan cap and its source (W1):
// session override (≥0) → strategy value (≥0) → the shipped default 2. An
// explicit 0 is honoured at BOTH levels — "no re-plan after death" — where the
// strategy level used to read 0 as "unset → 2". session "" resolves the
// strategy level (outside every session window).
func ResolveReplanCap(c *DayPlanConfig, session string) (int, string) {
	n, source := ReplanCapDefault, SourceShippedDefault
	if c != nil && c.ReplanCap != nil && *c.ReplanCap >= 0 {
		n, source = *c.ReplanCap, SourceStrategyValue
	}
	if ov := c.SessionOverride(session); ov != nil && ov.ReplanCap != nil && *ov.ReplanCap >= 0 {
		n, source = *ov.ReplanCap, SourceSessionOverride
	}
	return n, source
}

// ResolveMinRiskReward is the single R:R floor — the arm seam and the decision
// path both land here. A saved value above zero wins; absent means the schema's
// own safe default, never a second opinion from an env var.
func ResolveMinRiskReward(cfg *StrategyConfig) (float64, string) {
	if cfg != nil && cfg.RiskControl.MinRiskRewardRatio > 0 {
		return cfg.RiskControl.MinRiskRewardRatio, SourceSaved
	}
	return SafeDefaultMinRiskReward, SourceSchemaDefault
}

// ResolveHTFVeto reports the higher-timeframe veto and why. Absent → ON: the
// veto is a safety default, so a missing block must not read as "off".
func ResolveHTFVeto(cfg *StrategyConfig) (bool, string) {
	if cfg == nil || cfg.Regime == nil || cfg.Regime.HTFVeto == nil {
		return true, SourceShippedDefault
	}
	return *cfg.Regime.HTFVeto, SourceSaved
}

// ResolvePlanMode resolves the plan-restriction mode for a session:
// per-session override → strategy-level → "advisory". advisory never gates.
func ResolvePlanMode(c *DayPlanConfig, session string) (string, string) {
	mode, source := "advisory", SourceShippedDefault
	if c != nil && strings.TrimSpace(c.PlanMode) != "" {
		mode, source = c.PlanMode, SourceStrategyValue
	}
	if ov := c.SessionOverride(session); ov != nil && ov.PlanMode != nil && strings.TrimSpace(*ov.PlanMode) != "" {
		mode, source = *ov.PlanMode, SourceSessionOverride
	}
	return strings.ToLower(strings.TrimSpace(mode)), source
}

// ResolveOneSetup (dispatch 102) resolves the two one-setup knobs from THE
// BOUND STRATEGY, with their sources: enabled defaults ON [O] (nil, never a
// plain false), min grade defaults B [O]. A malformed grade falls back to B
// and says so — a boot line that prints a grade the predicate does not use is
// worse than none (class 45/49).
func ResolveOneSetup(cfg *StrategyConfig) (enabled bool, minGrade, enabledSource, gradeSource string) {
	enabled, enabledSource = true, SourceShippedDefault
	minGrade, gradeSource = "B", SourceShippedDefault
	if cfg == nil || cfg.DayPlan == nil {
		return
	}
	if cfg.DayPlan.OneSetupEnabled != nil {
		enabled, enabledSource = *cfg.DayPlan.OneSetupEnabled, SourceSaved
	}
	switch g := strings.ToUpper(strings.TrimSpace(cfg.DayPlan.OneSetupMinGrade)); g {
	case "A+", "A", "B", "C":
		minGrade, gradeSource = g, SourceSaved
	case "":
	default:
		gradeSource = SourceShippedDefault + " (saved value " + g + " is not a grade)"
	}
	return
}

// ResolveStructureMap resolves day_plan.structure_map (S1, 2026-09-16): the
// STRUCTURE table is OFF unless the strategy saved true. Every S1 knob
// defaults OFF — nothing changes the live plan until S4 measures it.
func ResolveStructureMap(cfg *StrategyConfig) (enabled bool, source string) {
	if cfg == nil || cfg.DayPlan == nil || cfg.DayPlan.StructureMap == nil {
		return false, SourceShippedDefault
	}
	return *cfg.DayPlan.StructureMap, SourceSaved
}

// ── W-EXEC-TRUTH W3 (2026-09-23) — the entry-policy knobs ───────────────────
//
// The policy tokens are spelled here AND in kernel/entry_policy.go (store
// cannot import kernel); kernel's TestStoreEntryPolicyTokensMatchKernel pins
// the two spellings together.
const (
	EntryPolicyMarketInZone = "market_in_zone"
	EntryPolicyPlannedOrder = "planned_order"
	EntryPolicyLegacy       = "legacy"
	// EntryPolicyDefaultShipped is R4: market_in_zone is the default for newly
	// authored plans.
	EntryPolicyDefaultShipped = EntryPolicyMarketInZone
	ZoneMaxPtsDefault         = 10.0
	ZoneRestMaxMinDefault     = 30
	MinHoldMinDefault         = 3
)

// EntryPolicyDefaultValues is the legal value set of day_plan.entry_policy_default.
func EntryPolicyDefaultValues() []string {
	return []string{EntryPolicyMarketInZone, EntryPolicyPlannedOrder, EntryPolicyLegacy}
}

// ResolveEntryPolicyDefault resolves day_plan.entry_policy_default: the saved
// value when it is one of market_in_zone | planned_order | legacy, else the
// shipped default market_in_zone — and an unrecognised saved value is NAMED
// in the source instead of being silently read as the default.
func ResolveEntryPolicyDefault(c *DayPlanConfig) (string, string) {
	if c == nil || strings.TrimSpace(c.EntryPolicyDefault) == "" {
		return EntryPolicyDefaultShipped, SourceShippedDefault
	}
	v := strings.ToLower(strings.TrimSpace(c.EntryPolicyDefault))
	for _, ok := range EntryPolicyDefaultValues() {
		if v == ok {
			return v, SourceSaved
		}
	}
	return EntryPolicyDefaultShipped, SourceShippedDefault + " (saved value " + strconv.Quote(c.EntryPolicyDefault) + " is not a policy)"
}

// ResolveZoneMaxPts resolves day_plan.zone_max_pts (points): a saved positive
// value, else 10. A saved value ≤ 0 is named in the source.
func ResolveZoneMaxPts(c *DayPlanConfig) (float64, string) {
	if c == nil || c.ZoneMaxPts == nil {
		return ZoneMaxPtsDefault, SourceShippedDefault
	}
	if *c.ZoneMaxPts > 0 {
		return *c.ZoneMaxPts, SourceSaved
	}
	return ZoneMaxPtsDefault, SourceShippedDefault + " (saved value " + strconv.FormatFloat(*c.ZoneMaxPts, 'g', -1, 64) + " is not positive)"
}

// ResolveZoneRestMaxMin resolves day_plan.zone_rest_max_min (minutes): a
// saved positive value, else 30. The executor's rest cap reads THIS resolver.
func ResolveZoneRestMaxMin(c *DayPlanConfig) (int, string) {
	return resolvePositiveInt(c, func(c *DayPlanConfig) *int { return c.ZoneRestMaxMin }, ZoneRestMaxMinDefault)
}

// ResolveZonePlaceWithinPts resolves day_plan.zone_place_within_pts (points):
// nil = 25 (the armed placement band — ON), a saved positive value, else 0
// (OFF = legacy, place at any distance). WAVE PLANNER B1 reads THIS resolver.
func ResolveZonePlaceWithinPts(c *DayPlanConfig) (float64, string) {
	if c == nil || c.ZonePlaceWithinPts == nil {
		return 25, SourceShippedDefault + " (the armed placement band)"
	}
	if *c.ZonePlaceWithinPts > 0 {
		return *c.ZonePlaceWithinPts, SourceSaved
	}
	return 0, SourceShippedDefault + " (saved value " + strconv.FormatFloat(*c.ZonePlaceWithinPts, 'g', -1, 64) + " is not positive — OFF, legacy)"
}

// ResolveMinHoldMin resolves day_plan.min_hold_min (minutes): a saved positive
// value, else 3 — the floor on an armed market_in_zone time_hold.
func ResolveMinHoldMin(c *DayPlanConfig) (int, string) {
	return resolvePositiveInt(c, func(c *DayPlanConfig) *int { return c.MinHoldMin }, MinHoldMinDefault)
}

func resolvePositiveInt(c *DayPlanConfig, get func(*DayPlanConfig) *int, def int) (int, string) {
	if c == nil || get(c) == nil {
		return def, SourceShippedDefault
	}
	if v := *get(c); v > 0 {
		return v, SourceSaved
	}
	return def, SourceShippedDefault + " (saved value " + strconv.Itoa(*get(c)) + " is not positive)"
}
