package kernel

// Plan 3 Task 21 — Risk limits + force-flat kill switch.
//
// Hard server-side guard rails enforced BEFORE expensive prompt assembly.
// The AI prompt itself only contains soft hints ("don't risk more than 1%"),
// which the model may ignore. These limits are non-negotiable.
//
// Design goals:
//   - Plain primitives in CheckPreTrade so the function is trivially testable
//     without constructing a full *Context / *Decision (the plan's illustrative
//     types do not exactly match the real ones).
//   - Decoupled from concrete broker packages (ninjatrader, etc.) so
//     that the engine can call ForceFlat via a tiny interface.
//   - Loaded from env once at startup via LoadRiskLimitsFromConfig().

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"vl/config"
	"vl/logger"
	"vl/store"
)

// RiskLimits caps the risk a strategy can take before the engine intervenes.
// Zero values disable the corresponding check (so partial config is safe).
type RiskLimits struct {
	MaxDailyLossUSD     float64 // hard stop for the trading day (USD, positive number)
	MaxConcurrentTrades int     // open position cap (count)
	MaxNotionalUSD      float64 // sum of (entry price * quantity) cap across open positions
}

// RiskLimitDecision is the qualitative outcome of a pre-trade check.
// Callers may use the typed err for logging and the decision for action.
type RiskLimitDecision int

const (
	RiskAllow      RiskLimitDecision = iota // no constraints hit
	RiskBlockEntry                          // block this entry, hold existing positions
	RiskForceFlat                           // close everything (daily-loss tripped)
)

// CheckPreTrade evaluates the proposed trade against all enabled limits.
// Inputs are plain primitives so this can be exercised in a unit test
// without a full engine.Context.
//
//	totalPnL          — account.TotalPnL (negative = losing)
//	openPositions     — len(ctx.Positions)
//	requestedNotional — abs(decision.PositionSizeUSD) for the proposed entry
//	existingNotional  — sum of MarkPrice*Quantity over existing positions
//
// Semantics:
//   - PnL strictly LESS THAN -MaxDailyLossUSD trips the daily-loss limit.
//     PnL == -MaxDailyLossUSD does NOT trip (boundary is permissive, matches
//     the plan's `< -r.MaxDailyLossUSD` source). PnL == limit slightly past
//     trips immediately.
//   - openPositions >= cap trips (cap-equals-trip).
//   - existing + requested > cap trips (strict exceed).
//   - When MaxDailyLossUSD == 0 the daily-loss check is disabled; same for
//     MaxConcurrentTrades and MaxNotionalUSD (so partial config is safe).
func (r *RiskLimits) CheckPreTrade(totalPnL float64, openPositions int, requestedNotional float64, existingNotional float64) error {
	if r == nil {
		return nil
	}
	if r.MaxDailyLossUSD > 0 && totalPnL < -r.MaxDailyLossUSD {
		return fmt.Errorf("risk: daily loss limit hit (pnl=%.2f, limit=-%.2f)", totalPnL, r.MaxDailyLossUSD)
	}
	if r.MaxConcurrentTrades > 0 && openPositions >= r.MaxConcurrentTrades {
		return fmt.Errorf("risk: concurrent trade cap reached (open=%d, cap=%d)", openPositions, r.MaxConcurrentTrades)
	}
	if r.MaxNotionalUSD > 0 {
		total := existingNotional + requestedNotional
		if total > r.MaxNotionalUSD {
			return fmt.Errorf("risk: notional cap exceeded (existing=%.2f + requested=%.2f > cap=%.2f)", existingNotional, requestedNotional, r.MaxNotionalUSD)
		}
	}
	return nil
}

// Classify returns the qualitative decision class without re-running checks.
// daily-loss trip → ForceFlat (close everything).
// other trips     → BlockEntry (hold existing, refuse new).
// nil err         → Allow.
func (r *RiskLimits) Classify(totalPnL float64, openPositions int, requestedNotional float64, existingNotional float64) (RiskLimitDecision, error) {
	if r == nil {
		return RiskAllow, nil
	}
	if r.MaxDailyLossUSD > 0 && totalPnL < -r.MaxDailyLossUSD {
		return RiskForceFlat, fmt.Errorf("risk: daily loss limit hit (pnl=%.2f, limit=-%.2f)", totalPnL, r.MaxDailyLossUSD)
	}
	if err := r.CheckPreTrade(totalPnL, openPositions, requestedNotional, existingNotional); err != nil {
		return RiskBlockEntry, err
	}
	return RiskAllow, nil
}

// LoadRiskLimitsFromConfig pulls the four limits from config.Get().
// Called once at startup or each cycle (cheap — just struct copy).
func LoadRiskLimitsFromConfig() RiskLimits {
	c := config.Get()
	return RiskLimits{
		MaxDailyLossUSD:     c.RiskMaxDailyLossUSD,
		MaxConcurrentTrades: c.RiskMaxConcurrentTrades,
		MaxNotionalUSD:      c.RiskMaxNotionalUSD,
	}
}

// ============================================================================
// Daily PnL reset
// ============================================================================
//
// The MaxDailyLossUSD limit is evaluated against ctx.Account.TotalPnL, which
// is a session-cumulative figure tracked by the trader. A true "daily" reset
// requires either:
//   (a) the trader to reset its TotalPnL at the CME session boundary
//       (Sunday 18:00 ET); or
//   (b) the engine to remember the start-of-day equity and compute
//       (current_equity - day_open_equity) on each cycle.
//
// For Plan 3 v1 we ship a SIMPLE day-boundary tracker here so the API
// endpoint and tests have something to call. It DOES NOT yet hook into
// CME session boundaries (deferred to a follow-up alongside T22 drift).
//
// Manual reset path: ResetDailyPnLAt(now) — the GLOBAL reset (every trader).
// r22 RISK-GATES: the operator's force-flat endpoint no longer calls it; it
// clears only the trader acted on (ClearDailyForceFlat via MaybeForceFlat).
//
// Automatic check-and-reset path: MaybeResetDaily(now) — called at the
// top of each decision cycle. If the wall-clock date has changed since
// last reset, the tracker zeroes itself. This is a coarse approximation
// of session boundaries (UTC date rollover, not 18:00 ET) and is
// adequate for SIM-mode paper trading; live trading should switch to
// CME-aware reset by checking IsCMEOpen + transition detection.

var (
	dailyResetMu       sync.Mutex
	lastDailyResetDate string // YYYY-MM-DD in UTC
)

// ResetDailyPnL marks "now" as the new day-start. Cheap, idempotent.
// Operators call this from the force-flat API endpoint or at the
// CME Sunday 18:00 ET open.
// Clock seam (class 60): the entry point owns the wall clock and does nothing
// else; the rule lives in the …At body so a test can state its own hour.
// ---------------------------------------------------------------------------
// DAILY FORCE-FLAT TRIP STATE (wiring wave 2026-09-05)
//
// DailyGuardrails.Check() has always returned RiskForceFlat on a daily-loss
// trip, and its ONLY production caller discarded the value:
//
//	} else if _, gErr := g.Check(); gErr != nil {   // engine_analysis.go
//
// so the trip skipped the decision cycle and nothing else. The ARM path never
// consulted the guardrail at all (entry_gate.go contained neither "daily" nor
// "guardrail"), which meant a RESTING ARM FILLED STRAIGHT THROUGH A TRIPPED
// DAILY LOSS LIMIT. The limit was believed and did not hold.
//
// The caller now publishes the trip here and EntryGate reads it, so both order
// paths — arm seam and decision path — refuse new entries once it is set.
//
// SCOPE, deliberately: this blocks NEW ENTRIES only. Open positions are NOT
// closed. Closing them stays operator-initiated via POST /api/risk/force-flat,
// per the ForceFlatSignaler contract documented in engine_analysis.go — that
// policy is unchanged by this wave.
//
// Cleared by the CME session-day reset (ResetDailyPnLAt), so a trip lasts the
// session-day and lifts with the daily window.
//
// r22 RISK-GATES — THE TRIP IS A LATCH STAMPED WITH ITS CME SESSION-DAY.
// The roll used to be the only thing that lifted it, and the roll
// (MaybeResetDaily) runs only inside an AI decision cycle — a mentor-only
// trader never runs one, so its trip had nothing to lift it but a restart or an
// operator. A trip now carries the session-day it was set on and reads as
// lifted the moment the clock is on a LATER session-day (17:00 CT), whoever
// asks. Until then it holds, whatever the P&L does afterwards.
var (
	forceFlatMu     sync.RWMutex
	forceFlatReason = map[string]forceFlatTrip{}
)

// forceFlatTrip is one latched daily-loss trip: why, and the CME session-day
// (CMESessionDayKey) it was set on.
type forceFlatTrip struct {
	reason string
	day    string
}

// SetDailyForceFlat records that traderID tripped its daily loss limit.
func SetDailyForceFlat(traderID, reason string) {
	SetDailyForceFlatAt(traderID, reason, time.Now())
}

// SetDailyForceFlatAt is SetDailyForceFlat on the caller's clock: the latch is
// stamped with the CME session-day of now.
func SetDailyForceFlatAt(traderID, reason string, now time.Time) {
	day := CMESessionDayKey(now)
	forceFlatMu.Lock()
	defer forceFlatMu.Unlock()
	if prev, already := forceFlatReason[traderID]; !already || prev.day != day {
		logger.Warnf("🔴 daily force-flat ARMED for trader %s: %s — NEW ENTRIES BLOCKED on every order path until the CME session-day %s ends (17:00 CT)", traderID, reason, day)
	}
	forceFlatReason[traderID] = forceFlatTrip{reason: reason, day: day}
}

// DailyForceFlatReason returns the trip reason for traderID, or "" when the
// daily loss limit has not tripped. "" is the fail-open answer: no evidence of
// a trip is never a refusal.
func DailyForceFlatReason(traderID string) string {
	return DailyForceFlatReasonAt(traderID, time.Now())
}

// DailyForceFlatReasonAt is DailyForceFlatReason on the caller's clock. A trip
// stamped with an EARLIER session-day than now's has lifted (the 17:00 CT
// roll) and reads "".
func DailyForceFlatReasonAt(traderID string, now time.Time) string {
	forceFlatMu.RLock()
	defer forceFlatMu.RUnlock()
	trip, ok := forceFlatReason[traderID]
	if !ok {
		return ""
	}
	if CMESessionDayKey(now) > trip.day { // "2006-01-02" keys order as dates
		return ""
	}
	return trip.reason
}

// ClearDailyForceFlat lifts the trip for ONE trader (operator resume: the
// Emergency Flat on that trader). Every other trader's trip stands.
func ClearDailyForceFlat(traderID string) {
	forceFlatMu.Lock()
	defer forceFlatMu.Unlock()
	if _, ok := forceFlatReason[traderID]; ok {
		logger.Infof("daily force-flat cleared for trader %s only (operator reset) — every other trader's trip stands", traderID)
	}
	delete(forceFlatReason, traderID)
}

// clearAllDailyForceFlat lifts every trip; called by the manual global reset.
func clearAllDailyForceFlat() {
	forceFlatMu.Lock()
	defer forceFlatMu.Unlock()
	if n := len(forceFlatReason); n > 0 {
		logger.Infof("daily force-flat cleared for %d trader(s) by the daily window reset", n)
	}
	forceFlatReason = map[string]forceFlatTrip{}
}

// clearDailyForceFlatNotOn lifts every trip NOT stamped with session-day today
// — the roll's clear. A trip set after the roll (today's) stands: the roll
// runs at the first AI cycle after 17:00 CT, which can come after a fresh trip.
func clearDailyForceFlatNotOn(today string) {
	forceFlatMu.Lock()
	defer forceFlatMu.Unlock()
	n := 0
	for id, trip := range forceFlatReason {
		if trip.day != today {
			delete(forceFlatReason, id)
			n++
		}
	}
	if n > 0 {
		logger.Infof("daily force-flat cleared for %d trader(s) by the session-day roll to %s", n, today)
	}
}

// ResetDailyPnLAt is the manual GLOBAL window reset: it lifts EVERY trader's
// trip. r22 RISK-GATES (T4): its wall-clock wrapper ResetDailyPnL was removed —
// its only production caller was MaybeForceFlat (the Emergency Flat), which now
// clears the ONE trader acted on (ClearDailyForceFlat).
func ResetDailyPnLAt(now time.Time) {
	clearAllDailyForceFlat()
	dailyResetMu.Lock()
	defer dailyResetMu.Unlock()
	lastDailyResetDate = CMESessionDayKey(now)
	logger.Infof("Plan 3 T21: daily window manually reset to CME session-day %s", lastDailyResetDate)
}

// MaybeResetDaily checks for a CME session-day rollover (17:00 CT boundary)
// since the last reset and resets the daily window when it has. Returns true if
// a reset fired. Strategy Studio P1 fix: the reset key is derived from the
// PASSED `now` (via CMESessionDayKey), not time.Now() — so the window is
// deterministic + testable and rolls on the real session boundary, not UTC
// midnight. (Previously ResetDailyPnL stamped time.Now() while the check used
// the passed `now`, so they never agreed.)
func MaybeResetDaily(now time.Time) bool {
	today := CMESessionDayKey(now)
	dailyResetMu.Lock()
	defer dailyResetMu.Unlock()
	if lastDailyResetDate == "" || lastDailyResetDate != today {
		lastDailyResetDate = today
		// D4(c) (2026-09-09, dispatch 104) — THE ROLL LIFTS THE TRIP.
		//
		// This rolled the DATE and left forceFlatReason untouched, so a daily
		// force-flat never cleared automatically at all: clearAllDailyForceFlat
		// had exactly one caller, ResetDailyPnLAt, reachable only from the
		// operator's force-flat endpoint. A tripped desk stayed blocked across
		// the roll, the next session and the one after, until a human reset it
		// or the process restarted.
		//
		// vet-06 called this "clears only at the first AI cycle after the roll";
		// the truth was that it did not clear on any cycle. The comment above
		// forceFlatReason has promised since #91 that a trip "lifts with the
		// daily window" — this is the line that makes that true. It has never
		// been seen because the guardrails master is OFF and the trip has never
		// fired: a latent halt waiting for the day the owner turns it on.
		//
		// The unlock is deliberate: clearAllDailyForceFlat takes forceFlatMu,
		// and holding dailyResetMu across it is a lock-order the manual path
		// (ResetDailyPnLAt: clear FIRST, then take dailyResetMu) does not use.
		// One order, both paths.
		dailyResetMu.Unlock()
		clearDailyForceFlatNotOn(today)
		dailyResetMu.Lock()
		logger.Infof("Plan 3 T21 / Strategy Studio: daily window reset to CME session-day %s (force-flat trips lifted)", today)
		return true
	}
	return false
}

// ============================================================================
// Strategy Studio Phase 1 — per-strategy daily guardrails (evolves the gate).
// ============================================================================

// DailyGuardrails carries the RESOLVED per-strategy daily guardrail config (the
// caller resolves per-strategy value → env fallback + the master/per-guardrail
// toggles) plus the live daily-window state. Pure primitives so it is trivially
// unit-testable, like CheckPreTrade.
type DailyGuardrails struct {
	MasterEnabled bool // master switch; false → ALL guardrails bypassed (caller logs)

	DailyRealizedPnL float64 // realized P&L on the CME session-day (negative = net loss)
	TradesToday      int     // entries taken on the CME session-day

	// r22 RISK-GATES — the LOSS leg's P&L when the caller resolved it: the
	// bot's own session-day P&L, realized + open, manual trades excluded.
	// Known=false → the loss leg reads DailyRealizedPnL (the pre-r22 input).
	DailyLossPnL      float64
	DailyLossPnLKnown bool

	DailyLossEnabled      bool
	DailyLossLimitUSD     float64 // positive USD; loss ≥ this trips (force-flat class)
	DailyProfitEnabled    bool
	DailyProfitTargetUSD  float64 // positive USD; profit ≥ this trips (block-entry)
	MaxDailyTradesEnabled bool
	MaxDailyTrades        int

	// 6.3 (final-bundle 2026-08-19) — blackout + consistency join the soft
	// audit. The caller precomputes the window/breach facts (CheckSoft stays
	// pure); CONFIGURED means the owner set the values, regardless of toggles.
	BlackoutConfigured   bool // start+end CT both set
	InBlackoutNow        bool // precomputed InBlackoutWindow(now, …)
	ConsistencyMaxDayPct float64
	TotalRealizedPnL     float64
}

// StrategyGuardrailPosture is the CONFIG half of the daily guardrail gate,
// resolved once (W1 (g), settings truth): per-strategy value → env fallback for
// the daily-loss limit, and every toggle → its shipped default when unset. The
// engine's gate (engine_analysis.go) builds DailyGuardrails from it and the
// Settings page reports it, so the page can never narrate a default the gate
// does not apply.
type StrategyGuardrailPosture struct {
	MasterEnabled         bool
	DailyLossEnabled      bool
	DailyLossLimitUSD     float64
	DailyProfitEnabled    bool
	DailyProfitTargetUSD  float64
	MaxDailyTradesEnabled bool
	MaxDailyTrades        int
	BlackoutEnabled       bool
	ConsistencyEnabled    bool
}

// ResolveStrategyGuardrails resolves the posture from the strategy's risk
// control block. envDailyLossUSD is RISK_MAX_DAILY_LOSS_USD as loaded at boot
// (LoadRiskLimitsFromConfig().MaxDailyLossUSD).
func ResolveStrategyGuardrails(rc store.RiskControlConfig, envDailyLossUSD float64) StrategyGuardrailPosture {
	return StrategyGuardrailPosture{
		// r22 RISK-GATES (owner R4 + R5, CTO fold 2026-10-14): ABSENT = OFF.
		// Before r22 an absent master / daily-loss toggle read ON with the env
		// $500 — decorative then (nothing flattened). Since r22 a breach
		// refuses, cancels, FLATTENS and latches, so an inherited ON would be
		// new behaviour nobody ordered. Only the ON/OFF default flips; the $
		// value keeps its inherit (Studio value → env fallback). Stored values
		// are untouched.
		MasterEnabled:         boolOrDefault(rc.GuardrailsEnabled, false),
		DailyLossEnabled:      boolOrDefault(rc.DailyLossEnabled, false),
		DailyLossLimitUSD:     firstPositive(rc.DailyLossLimitUSD, envDailyLossUSD),
		DailyProfitEnabled:    boolOrDefault(rc.DailyProfitEnabled, false),
		DailyProfitTargetUSD:  rc.DailyProfitTargetUSD,
		MaxDailyTradesEnabled: boolOrDefault(rc.MaxDailyTradesEnabled, false),
		MaxDailyTrades:        rc.MaxDailyTrades,
		BlackoutEnabled:       boolOrDefault(rc.BlackoutEnabled, false),
		ConsistencyEnabled:    boolOrDefault(rc.ConsistencyEnabled, false),
	}
}

// CheckSoft evaluates every CONFIGURED limit (value > 0) regardless of toggles
// and returns the "would have tripped" reasons. P0-cleanup (2026-08-19): the
// guardrails master stays OFF by the owner's dated decision, but the owner must
// SEE what the cage would have caught. It never blocks anything.
func (g DailyGuardrails) CheckSoft() []string {
	var hits []string
	if g.DailyLossLimitUSD > 0 && g.lossPnL() <= -g.DailyLossLimitUSD {
		hits = append(hits, fmt.Sprintf("daily loss would trip (%s today=%.2f, limit=-%.2f)", g.lossPnLLabel(), g.lossPnL(), g.DailyLossLimitUSD))
	}
	if g.DailyProfitTargetUSD > 0 && g.DailyRealizedPnL >= g.DailyProfitTargetUSD {
		hits = append(hits, fmt.Sprintf("daily profit target would trip (realized today=%.2f, target=%.2f)", g.DailyRealizedPnL, g.DailyProfitTargetUSD))
	}
	if g.MaxDailyTrades > 0 && g.TradesToday >= g.MaxDailyTrades {
		hits = append(hits, fmt.Sprintf("max daily trades would trip (today=%d, max=%d)", g.TradesToday, g.MaxDailyTrades))
	}
	// 6.3 — the two checks that used to die SILENTLY under master OFF (PR #54:
	// 69 live would-trip lines were trio-only; blackout/consistency never spoke).
	if g.BlackoutConfigured && g.InBlackoutNow {
		hits = append(hits, "blackout window would trip (inside the configured CT window)")
	}
	if g.ConsistencyMaxDayPct > 0 && ConsistencyBreached(g.DailyRealizedPnL, g.TotalRealizedPnL, g.ConsistencyMaxDayPct) {
		hits = append(hits, fmt.Sprintf("consistency rule would trip (today=%.2f vs %.0f%% of total=%.2f)", g.DailyRealizedPnL, g.ConsistencyMaxDayPct, g.TotalRealizedPnL))
	}
	return hits
}

// caller logs the bypass). Each guardrail is evaluated only when its toggle is
// ON AND its value is configured (>0); a tripped guardrail returns a non-nil err
// with a clear reason. Daily-loss → ForceFlat; profit-target / max-trades →
// BlockEntry. NB: this controls ONLY the prop-firm guardrails — it can never
// affect the SIM-only / live-account block (that is enforced separately in the
// broker layer and is untoggleable).
func (g DailyGuardrails) Check() (RiskLimitDecision, error) {
	if !g.MasterEnabled {
		return RiskAllow, nil
	}
	if g.DailyLossEnabled && g.DailyLossLimitUSD > 0 && g.lossPnL() <= -g.DailyLossLimitUSD {
		return RiskForceFlat, fmt.Errorf("daily loss limit hit (%s today=%.2f, limit=-%.2f)", g.lossPnLLabel(), g.lossPnL(), g.DailyLossLimitUSD)
	}
	if g.DailyProfitEnabled && g.DailyProfitTargetUSD > 0 && g.DailyRealizedPnL >= g.DailyProfitTargetUSD {
		return RiskBlockEntry, fmt.Errorf("daily profit target reached (realized today=%.2f, target=%.2f)", g.DailyRealizedPnL, g.DailyProfitTargetUSD)
	}
	if g.MaxDailyTradesEnabled && g.MaxDailyTrades > 0 && g.TradesToday >= g.MaxDailyTrades {
		return RiskBlockEntry, fmt.Errorf("max daily trades reached (today=%d, max=%d)", g.TradesToday, g.MaxDailyTrades)
	}
	return RiskAllow, nil
}

// lossPnL is the P&L the LOSS leg judges: the caller-resolved bot P&L
// (realized + open, manual excluded) when known, else the realized input.
func (g DailyGuardrails) lossPnL() float64 {
	if g.DailyLossPnLKnown {
		return g.DailyLossPnL
	}
	return g.DailyRealizedPnL
}

// lossPnLLabel names which number lossPnL is, for the refusal text.
func (g DailyGuardrails) lossPnLLabel() string {
	if g.DailyLossPnLKnown {
		return "bot P&L realized+open, manual excluded"
	}
	return "realized"
}

// boolOrDefault resolves a three-state *bool toggle: nil → the default value.
// Used so a guardrail's per-strategy "enabled" can be on/off/unset; unset
// inherits the guardrail's safe default (daily-loss ON, new guardrails OFF).
func boolOrDefault(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// firstPositive returns the first > 0 value — used for per-strategy value with
// an env fallback: firstPositive(perStrategyValue, envValue).
func firstPositive(vals ...float64) float64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

// ResolveMaxContracts returns the per-order contract clamp for a FUTURES order.
// Hardening D3 (audit F2): this SIZE cap is ALWAYS ON for futures — it is venue
// safety, NOT a prop-firm rule, so the guardrails master switch and the
// max-contracts toggle govern ONLY daily limits/blackout, never this clamp.
// Per-strategy value overrides (>0); else the venue default (2 contracts —
// the researched fallback; 6.6: the old '10-contract' text was a comment lie).
// NEVER returns 0 — a futures order can never be left unclamped.
func ResolveMaxContracts(perStrategy, def int) int {
	n, _ := ResolveMaxContractsWithSource(perStrategy, def)
	return n
}

// ResolveMaxContractsWithSource is ResolveMaxContracts with WHERE the value came
// from (W1 (g), settings truth). The rule lives here once and the value-only
// entry point delegates, so the Settings page narrates the SAME resolution the
// order path performs: a saved value (>0), else the venue default, then the
// Stage-A ceiling — named by its env var when that is what bound, else as the
// clamp.
func ResolveMaxContractsWithSource(perStrategy, def int) (int, string) {
	n, src := def, store.SourceShippedDefault
	if perStrategy > 0 {
		n, src = perStrategy, store.SourceSaved
	}
	if capN, capSrc := StageAContractCapWithSource(); n > capN {
		if capSrc == StageAContractCapEnvSource {
			return capN, capSrc
		}
		return capN, "clamp (kernel.ClampStageAContracts)"
	}
	return n, src
}

// StageAContractCap (0B, owner ruling 2026-09-02) is the survival-first size
// ceiling: ONE contract until n≥30 closed trades with a positive lower-CI
// expectancy. Kelly and optimal-f are undefined without an edge estimate, so
// size does not move on intuition. It also holds the line under 0B's stop
// floor: 1.0→1.5×ATR5m raises dollar risk per trade by ~50% at constant size,
// which is exactly why size stays at 1.
//
// Before 0B the resolvers disagreed in production: arm-leg capacity resolved to
// 1 (splitLegCapacity, unset max_contracts_per_order) while order sizing
// resolved to 2 (this function's `def` = maxFuturesContracts) — the boot line
// said capacity=1 while a market entry could size 2.
//
// Env STAGE_A_CONTRACT_CAP raises the ceiling for Stage B; 0 or unset = 1.
const StageAContractCapDefault = 1

// StageAContractCap resolves the Stage-A ceiling (env STAGE_A_CONTRACT_CAP).
func StageAContractCap() int {
	n, _ := StageAContractCapWithSource()
	return n
}

// StageAContractCapEnvSource names the env origin of the Stage-A ceiling.
const StageAContractCapEnvSource = "env STAGE_A_CONTRACT_CAP"

// StageAContractCapWithSource is StageAContractCap with its origin: the env
// value when it parses to a positive int, else the code constant.
func StageAContractCapWithSource() (int, string) {
	if v := os.Getenv("STAGE_A_CONTRACT_CAP"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n, StageAContractCapEnvSource
		}
	}
	return StageAContractCapDefault, "code constant (kernel.StageAContractCapDefault)"
}

// ClampStageAContracts applies the Stage-A ceiling to any resolved size.
func ClampStageAContracts(n int) int {
	if cap := StageAContractCap(); n > cap {
		return cap
	}
	return n
}

// ResolveConcurrentCap returns the open-position cap for the pre-prompt gate and
// which source governed it. Hardening D4 (audit F3): the PER-STRATEGY max_positions
// is authoritative; the env RISK_MAX_CONCURRENT_TRADES is only the fallback when
// the strategy did not set one (perStrategyMaxPositions <= 0). Previously the gate
// always used the env value (default 2), so a configured max_positions of 3 was
// unreachable — the effective cap silently disagreed with the UI.
func ResolveConcurrentCap(perStrategyMaxPositions, envFallback int) (limit int, source string) {
	if perStrategyMaxPositions > 0 {
		return perStrategyMaxPositions, "strategy max_positions"
	}
	return envFallback, "env RISK_MAX_CONCURRENT_TRADES"
}

// ResolveNotionalLeverage returns the futures notional-ceiling multiplier
// (max notional = equity × this). Hardening D3 (audit F2): ALWAYS ON for futures,
// independent of the guardrails master switch/toggle (which govern only daily
// limits/blackout). Per-strategy value overrides (>0); else the venue default
// (equity×20). NEVER returns 0 — a futures order always has a notional ceiling.
func ResolveNotionalLeverage(perStrategy, def float64) float64 {
	v, _ := ResolveNotionalLeverageWithSource(perStrategy, def)
	return v
}

// ResolveNotionalLeverageWithSource is ResolveNotionalLeverage with its origin
// (W1 (g)): a saved value (>0) or the venue default.
func ResolveNotionalLeverageWithSource(perStrategy, def float64) (float64, string) {
	if perStrategy > 0 {
		return perStrategy, store.SourceSaved
	}
	return def, store.SourceShippedDefault
}

// ConsistencyBreached (Chunk 5) reports whether today's realized profit exceeds
// the configured share (pct%) of all-time total realized profit — the prop-firm
// consistency rule ("no single day > X% of total"). SEMANTICS (documented): it
// triggers ONLY once there is prior-day profit (totalProfit − todayProfit > 0),
// so a fresh/single-day account never self-locks on its first profitable day; a
// non-profitable day (todayProfit ≤ 0), a non-positive total, or pct ≤ 0 never
// breach. When breached, the caller blocks new entries for the session-day so
// today's profit stops growing past the allowed share.
func ConsistencyBreached(todayProfit, totalProfit, pct float64) bool {
	if pct <= 0 || todayProfit <= 0 || totalProfit <= 0 {
		return false
	}
	if totalProfit-todayProfit <= 0 {
		return false // first profitable day — nothing to be consistent against yet
	}
	return todayProfit >= (pct/100.0)*totalProfit
}

// SoftGuardrailFunc is installed by the trader layer: a guardrail that WOULD
// have tripped (but is disabled — master OFF or toggle OFF) is announced to the
// owner's alert feed. Never blocks. P0-cleanup (2026-08-19).
var SoftGuardrailFunc func(trader, what string)
