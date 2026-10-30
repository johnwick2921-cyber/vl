package trader

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vl/kernel"
	"vl/kernel/pep"
	"vl/store"
	"vl/telemetry"
)

// ── PEP MODE, TRADER SIDE (owner order 2026-10-29) ──────────────────────────
//
// pep is a trading mode SEPARATE from mentor mode: its own evaluator
// (kernel/pep), its own settings (the root "pep" block), its own durable
// one-trade-per-day record (store.PepDayStore), and no mentor rule applies to
// it. A trader is exactly ONE of {AI/planner, mentor, pep}.
//
// THE SHAPE OF THE WIRING
//
//	final 1m bar ──► pepEventPassAt / pepTick ──► kernel/pep.Evaluate (pure)
//	                                                    │ one Intent
//	                                                    ▼
//	                          pepSendEntry: wire R:R + slippage check on the
//	                          ROUNDED prices ──► PepSourced kernel.Decision
//	                                                    │
//	                                                    ▼
//	        executeDecisionWithRecordAt ─► admitChain (pep branch, entry_admission.go)
//	                                    ─► durable claim (pepClaimBeforeSend)
//	                                    ─► openEntryWithRecord ─► OpenWithBracket
//	                                       ONE market order with its native SL/TP
//
// NEVER two resting stops: a whipsaw cannot fill both, and the one-contract-per
// -account guard and the entry latch stay exactly as they are.
//
// The mentor's entry route (Origin=mentor armed rows) is NOT reused: every
// mentor fill is registered into the mentor exit drive, which moves stops (BE,
// 1:1 floor, candle trail, X8 trap-dodge) and would violate "no trailing".
//
// NO PACKAGE-LEVEL STATE: everything below lives on the AutoTrader (pepRuntime)
// or in the database, so two pep traders (MNQ + ES) never share anything.

// pepRuntime is one trader's pep working memory. It holds nothing that must
// survive a restart: the one-trade-per-day record is the database's.
type pepRuntime struct {
	// mu serialises an evaluation with its entry send: the event loop and the
	// 2-minute cycle both call pepEvalOnce.
	mu sync.Mutex

	// pending is the entry currently between claim and send (set by pepSendEntry
	// for the duration of the synchronous executeDecisionWithRecordAt call).
	pending *pepPending

	// lastBlock is the (code|detail-class) of the last COUNTED block, so a
	// refusal that repeats every bar is counted once per change, not per bar.
	lastBlock string
	// flatAttempts counts flat attempts while a position stays open past the flat
	// time (an alert after pepFlatAlertAttempts). Atomic: the monitor beat resets
	// it outside pep.mu.
	flatAttempts atomic.Int32

	// truth is the newest evaluation, for /api/traders/:id/pep-truth.
	truthMu sync.Mutex
	truth   pepTruthState

	// conflictOnce makes the load-time conflict alert fire once per trader object.
	conflictOnce sync.Once
}

// pepPending is one entry's identity between decision and wire.
type pepPending struct {
	intent   pep.Intent
	signalID string
	day      string
	// now is the instant the entry was judged at (the clock the durable rows are
	// stamped with, so a window measured from them is on one clock).
	now time.Time
	row *store.PepDayDB
	// sending is true once the claim was advanced to "sending" (the wire is
	// about to be reached): from then on the day's slot is consumed whatever
	// happens next.
	sending bool
}

// pepSaved reports the saved master switch (OFF by default).
func (at *AutoTrader) pepSaved() bool {
	return at != nil && at.config.StrategyConfig != nil && at.config.StrategyConfig.PepEnabled()
}

// pepModeConflict names the illegal mode combination of this trader's strategy,
// nil when there is none.
func (at *AutoTrader) pepModeConflict() *store.ModeConflictError {
	if at == nil || at.config.StrategyConfig == nil {
		return nil
	}
	return at.config.StrategyConfig.TradingModeConflict()
}

// pepEnabled is THE pep mode predicate (mirrors mentorEnabled): the saved switch
// is ON and the strategy is not in a mode conflict. A conflicted strategy is
// not pep (nothing pep acts) and, because Run refuses to start it, not anything
// else either - see pepStartRefusal.
func (at *AutoTrader) pepEnabled() bool {
	return at.pepSaved() && at.pepModeConflict() == nil
}

// modeOwnsExits reports that a RULE MODE (mentor or pep) owns this trader's
// exits and entries: the AI-era stop movers (auto-breakeven, trailing),
// the thesis-death exit and the drawdown emergency close must never touch a
// position of such a trader. mentor -> true is byte-identical to the mentorEnabled()
// checks it replaces; pep -> true is the new half.
func (at *AutoTrader) modeOwnsExits() bool {
	return at.mentorEnabled() || at.pepEnabled()
}

// exitOwnerWords is the mode-aware wording of the "not applied" log lines the
// four AI-era movers print. mentor keeps its exact historical text.
func (at *AutoTrader) exitOwnerWords() (mode, owner string) {
	if at.pepEnabled() && !at.mentorEnabled() {
		return "pep mode", "pep's fixed stop and target own"
	}
	return "mentor mode", "the mentor's exits own"
}

// pepResolved is the strategy's pep block as the bot RUNS it (the ONE resolver).
func (at *AutoTrader) pepResolved() store.PepResolved {
	if at == nil {
		return store.ResolvePep(nil)
	}
	return store.ResolvePep(at.config.StrategyConfig)
}

// pepConfig bridges the resolved block into the evaluator's Config.
func (at *AutoTrader) pepConfig() pep.Config {
	return pep.FromSettings(at.pepResolved())
}

// pepMaxContracts is the pep ceiling for resolveMaxContracts: the 0B size clamp
// (maxFuturesContracts = 2) does NOT apply to a pep trader - it would silently
// turn the owner's 6 into 2. The ceiling is the resolved pep contract count
// (which the resolver itself clamps to the hard ceiling of 10), and the entry
// path then REFUSES any sizing that is not exactly that number.
func (at *AutoTrader) pepMaxContracts() (int, bool) {
	if !at.pepEnabled() {
		return 0, false
	}
	return at.pepResolved().Contracts, true
}

// pepSkipsAIDecision: with pep ON the AI's entries are refused at the admission
// chain anyway, so the LLM call would spend tokens on a decision that cannot
// place. OFF -> false, byte-identical.
func (at *AutoTrader) pepSkipsAIDecision() bool { return at.pepEnabled() }

// pepSuppressAIClose is the close half of the AI-off scope: while pep is ON an
// AI close decision is refused - pep's fixed stop, target and 15:00 flat own
// every pep position. Every position of a pep trader is a pep position BY
// CONSTRUCTION (pepAdmitRefusal refuses every other entry on every path), so
// ownership needs no per-position flag that a restart could lose.
func (at *AutoTrader) pepSuppressAIClose(d *kernel.Decision) string {
	if !at.pepEnabled() || d == nil || d.PepSourced {
		return ""
	}
	telemetry.IncGateBlock(at.id, "pep_ai_closes_off")
	return "pep_mode: AI closes are OFF on a pep position - pep's fixed stop and target and its own flat time own the exit"
}

// pepStartRefusal is the load-time mutual-exclusion check, called by Run before
// anything starts. A strategy that turns pep ON together with mentor mode or the
// day plan is REFUSED: one P0 alert (deduped per trader), one gate-block count,
// one ERROR line, the trader marked not running, and Run returns the named
// error. Never silent, never "mentor wins".
func (at *AutoTrader) pepStartRefusal() error {
	c := at.pepModeConflict()
	if c == nil || !at.pepSaved() {
		return nil
	}
	telemetry.IncGateBlock(at.id, c.Code)
	at.pep.conflictOnce.Do(func() {
		at.logErrorf("🚫 PEP MODE CONFLICT: %s - trader %q (%s) is NOT started.", c.Message, at.name, at.id)
		at.emitAlert("P0", "pep_mode_conflict", "pep-mode-conflict:"+at.id,
			"Pep mode conflict - trader not started",
			fmt.Sprintf("Trader %q (%s): %s It was NOT started and will not trade until the strategy is fixed.", at.name, at.id, c.Message))
	})
	// EVERY refusal writes the not-running bit, not only the first: the start
	// handler writes is_running=true BEFORE it spawns Run, so a second Start that
	// is refused again must put the bit back (rereview m461 P3, SEPARATION lens).
	if at.store != nil {
		if err := at.store.Trader().UpdateStatus(at.userID, at.id, false); err != nil {
			at.logErrorf("❌ pep conflict refused the start but the not-running DB write FAILED (%v) - trader %q may still read running", err, at.id)
		}
	}
	return fmt.Errorf("%s: %s", c.Code, c.Message)
}

// pepDayNow is the CT calendar day of an instant ("" when America/Chicago is
// unavailable - every caller fails closed on "").
func pepDayNow(now time.Time) string {
	loc := kernel.CTLocation()
	if loc == nil {
		return ""
	}
	return now.In(loc).Format("2006-01-02")
}

// pepSignalID is the identity of one signal: the same signal re-seen on a later
// tick maps to the same id (the durable claim is the arbiter, this is the label).
func pepSignalID(in pep.Intent) string {
	return strings.Join([]string{string(in.Style), in.Day, string(in.Side), fmt.Sprint(in.SignalClose)}, "|")
}
