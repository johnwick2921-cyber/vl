package trader

import (
	"strings"
	"time"

	"vl/kernel"
	"vl/store"
)

// W9 — DAY-PLAN CONFIG READERS. The audit found six DayPlanConfig fields that the
// FE persists but NO production code reads: plan_mode, proximity_filter_atr,
// scenario_cap, sessions_enabled, approval_required, evening_digest (+ per-session
// overrides). These resolvers read them (per-session override wins over the
// strategy-level value) and default to the shipped values so behavior is
// byte-identical until the owner changes something.

func (at *AutoTrader) dayPlanCfg() *store.DayPlanConfig {
	if at.config.StrategyConfig == nil {
		return nil
	}
	return at.config.StrategyConfig.DayPlan
}

// sessionOverride returns this session's minimal override block, or nil.
func (at *AutoTrader) sessionOverride(session string) *store.DayPlanSessionOverride {
	dp := at.dayPlanCfg()
	if dp == nil {
		return nil
	}
	for i := range dp.Sessions {
		if strings.EqualFold(dp.Sessions[i].Session, session) {
			return &dp.Sessions[i]
		}
	}
	return nil
}

// planModeFor resolves the plan-restriction mode for a session: per-session override
// → strategy PlanMode → "advisory". advisory (default) never gates.
func (at *AutoTrader) planModeFor(session string) string {
	return at.dayPlanCfg().PlanModeFor(session)
}

// replanCapFor resolves the per-session re-read cap (default 2); a per-session
// override wins (0 = no re-plan after death).
func (at *AutoTrader) replanCapFor(session string) int {
	return at.dayPlanCfg().ReplanCapFor(session)
}

// acceptanceRuleFor resolves the per-session acceptance rule (W15.B — the
// override was persisted and rendered but read by NOTHING; every consumer went
// straight to the strategy-level field). One resolver, shared with the kernel
// prompt path and the API card renderer so all three narrate the same rulebook.
func (at *AutoTrader) acceptanceRuleFor(session string) string {
	return at.dayPlanCfg().AcceptanceRuleFor(session)
}

// activeSessionName returns the currently-active session's name, or "" when we
// are outside every window (night/interim). "" resolves to strategy-level
// settings, which is the correct fallback for an off-hours evaluation.
func (at *AutoTrader) activeSessionName(now time.Time) string {
	if s, ok := at.sessionRegistry(now).ActiveSession(now); ok {
		return s.Name
	}
	return ""
}

// sessionEnabledForStrategy gates which sessions THIS trader runs (on top of the
// registry Enabled flag): the SessionsEnabled subset (default [NY]). A per-session
// Enable override wins over the subset.
func (at *AutoTrader) sessionEnabledForStrategy(session string) bool {
	if ov := at.sessionOverride(session); ov != nil && ov.Enable != nil {
		return *ov.Enable
	}
	dp := at.dayPlanCfg()
	if dp == nil || len(dp.SessionsEnabled) == 0 {
		return strings.EqualFold(session, kernel.SessionNY) // default [NY]
	}
	for _, s := range dp.SessionsEnabled {
		if strings.EqualFold(strings.TrimSpace(s), session) {
			return true
		}
	}
	return false
}

// sessionRunnable resolves whether THIS strategy runs a session, combining the two
// enable layers the spec defines, with the inherit/override model the accordion
// chips already show:
//
//	EXPLICIT per-session override (sessions[].enable) → authoritative. 🔸override
//	otherwise → inherit: the admin registry's Enabled AND the sessions_enabled
//	                     subset (default [NY]).                            ⚪inherit
//
// Before this, the read scheduler ANDed the registry flag in unconditionally, so a
// strategy-level "turn ASIA on" could never take effect — the hardcoded
// DefaultSessionRegistry (ASIA/LONDON false) vetoed it forever. That is what made
// the session toggle dead on arrival. The registry still owns the CLOCK (window /
// read / flat / killzones) and still supplies the default; an explicit owner choice
// now wins for that strategy.
//
// Returns (runnable, why) — why is a short reason for the gate log when false.
func (at *AutoTrader) sessionRunnable(s *kernel.SessionDef) (bool, string) {
	if s == nil {
		return false, "no session"
	}
	if ov := at.sessionOverride(s.Name); ov != nil && ov.Enable != nil {
		if *ov.Enable {
			return true, ""
		}
		return false, s.Name + " switched off for this strategy"
	}
	if !s.Enabled {
		return false, s.Name + " not enabled in the session registry"
	}
	if !at.sessionEnabledForStrategy(s.Name) {
		return false, s.Name + " not in this strategy's sessions_enabled"
	}
	return true, ""
}

// SessionRunnable is the EXPORTED form of sessionRunnable for the API layer
// (P1/H8-residuals): the card/edit/ask/apply handlers must resolve enablement
// through the SAME resolver the bot gates use, never the raw registry flag.
func (at *AutoTrader) SessionRunnable(s *kernel.SessionDef) (bool, string) {
	return at.sessionRunnable(s)
}

// proximityFilterATR is the level activation half-width in daily-range-proxy
// multiples (day-trade lock). Valid 0.1–3.0; anything else → the default 1.5.
// S1 (mega-research 2026-08-26) — the 0.5 lower clamp made sane values
// unreachable when the proxy runs ~350pt (0.5×350 = ±175pt); 0.1 allows the
// owner's 0.3 retune (±~105pt).
func (at *AutoTrader) proximityFilterATR() float64 {
	if dp := at.dayPlanCfg(); dp != nil {
		// GAR-F2 (2026-08-28): ONE shared clamp (0.1–3.0) with the engine
		// prompt path — a 0.3 owner retune must behave identically on both.
		return kernel.ResolveProximityK(dp.ProximityFilterATR)
	}
	return kernel.ActivationWindowK // 1.5
}

// confluenceBandPts is the confluence cluster half-width in POINTS
// (FIX-LEVEL-GRADE, 2026-10-14). ≤0 (unset) → the scorer's default 0.10 ×
// daily-range proxy (byte-identical today). A positive value is passed through
// verbatim to the scorer's confBand.
func (at *AutoTrader) confluenceBandPts() float64 {
	if dp := at.dayPlanCfg(); dp != nil {
		return dp.ConfluenceBandPts
	}
	return 0
}

// scenarioCap is the max scenarios kept from a planner read (1–5). FOLDED
// (W-KNOB-PRUNE): the constant 3 unless a stored value exists — one seam.
func (at *AutoTrader) scenarioCap() int {
	return at.dayPlanCfg().ScenarioCapResolved()
}

// approvalRequired reports whether entries must be owner-approved before firing.
func (at *AutoTrader) approvalRequired() bool {
	dp := at.dayPlanCfg()
	return dp != nil && dp.ApprovalRequired
}

// eveningDigestEnabled gates the end-of-day roll-up digest. FOLDED
// (W-KNOB-PRUNE): constant OFF unless a stored true (every live strategy
// stores true). The caller (maybeWriteDigests) is already gated on the day
// plan being enabled, so a nil block never reaches this.
func (at *AutoTrader) eveningDigestEnabled() bool {
	return at.dayPlanCfg().EveningDigestOn()
}

// ApprovalKey is the system_config key granting entries for one trader + CME
// session-day (set by POST /api/plan/approve; consumed by the entry gate).
func ApprovalKey(traderID, sessionDayKey string) string {
	return "dayplan_approval:" + traderID + ":" + sessionDayKey
}

// approvalGranted reports whether the owner has approved entries for the current
// CME session-day.
func (at *AutoTrader) approvalGranted(now time.Time) bool {
	if at.store == nil {
		return false
	}
	v, _ := at.store.GetSystemConfig(ApprovalKey(at.id, kernel.CMESessionDayKey(now)))
	return v == "granted"
}

// planModeBlocked applies the plan-restriction mode to a NEW entry. advisory (or
// no plan mode) never blocks. direction: refuse entries against the plan's bias.
// strict: refuse entries that don't cite a matched scenario. In direction/strict
// with NO active plan, nothing is authorized → block (plan restricts).
//
// W-EXEC-TRUTH W0: it takes the caller's clock (admitEntry passes it; the
// wall-clock wrapper had no production caller left and was removed).
func (at *AutoTrader) planModeBlockedAt(d *kernel.Decision, now time.Time) (string, bool) {
	if d == nil {
		return "", false
	}
	session := ""
	if s, ok := at.sessionRegistry(now).ActiveSession(now); ok {
		session = s.Name
	}
	mode := at.planModeFor(session)
	if mode == "" || mode == "advisory" {
		return "", false
	}
	if !kernel.HasTraderPlanProvider(at.id) {
		return "", false // provider not installed (e.g. tests) → don't block
	}
	ap := kernel.ActivePlanFor(at.id, at.futuresSymbol())
	if ap == nil {
		return "no active plan (" + mode + " mode restricts to the plan)", true
	}
	switch mode {
	case "direction":
		bias := strings.ToLower(strings.TrimSpace(ap.Doc.Bias.Direction))
		if bias == "long" && d.Action == "open_short" {
			return "short entry against a long plan bias", true
		}
		if bias == "short" && d.Action == "open_long" {
			return "long entry against a short plan bias", true
		}
		return "", false
	case "strict":
		res := kernel.ClassifyCitation(d.Action, d.CitedScenario, ap.Doc)
		if !res.Matched {
			return "no matched scenario cited (strict mode)", true
		}
		return "", false
	}
	return "", false
}

// RealignCap returns this strategy's auto re-align ceiling (W13). Exported because
// the API layer enforces the cap at the /api/plan/realign entry point. FOLDED
// (W-KNOB-PRUNE): store.DefaultRealignCap (5) unless a stored value exists.
func (at *AutoTrader) RealignCap() int {
	return at.dayPlanCfg().RealignCapResolved()
}

// DayPlanOn reports whether the day-plan feature is enabled for this trader —
// the API uses it to skip re-align entirely on non-day-plan traders.
func (at *AutoTrader) DayPlanOn() bool { return at.dayPlanEnabled() }

// RunnableSessions returns the session names THIS strategy actually runs, in
// registry order. Exported so the plan card's session tabs reflect the owner's
// real configuration instead of a hardcoded frontend constant — and so they
// resolve it through the SAME sessionRunnable the gates use, which is the whole
// point of having one resolver.
func (at *AutoTrader) RunnableSessions() []string {
	reg := at.sessionRegistry(time.Now())
	out := make([]string, 0, len(reg.Sessions))
	for i := range reg.Sessions {
		if ok, _ := at.sessionRunnable(&reg.Sessions[i]); ok {
			out = append(out, reg.Sessions[i].Name)
		}
	}
	return out
}
