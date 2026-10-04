package trader

import (
	"fmt"
	"math"
	"vl/kernel"
	"vl/logger"
	"vl/market"
	"vl/store"
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
	"time"
)

// maxFuturesContracts caps the per-order contract count for CME futures
// (SIM-conservative). Tune per account size; 10 MNQ ≈ $600k notional ≈ $22k
// intraday margin, comfortably within a $50k SIM account.
// P0 follow-up 2026-08-17: an UNSET per-order cap used to fall back to 10 — five
// times the researched value, i.e. "never configured" was the most permissive
// setting. The fallback is now the researched 2. An explicit
// max_contracts_per_order still wins (ResolveMaxContracts), so this changes the
// default, never a choice.
const maxFuturesContracts = 2.0

// futuresOrderQuantity converts a decision's notional (position_size_usd) into
// a clamped contract count for CME futures: contracts = notional / (price ×
// pointValue), rounded, floored at 1, capped at maxFuturesContracts. Bypasses
// the crypto notional/leverage margin model (futures margin is per-contract).
func futuresOrderQuantity(symbol string, notionalUSD, price float64, maxContracts int) float64 {
	pv := market.FuturesPointValue(symbol)
	if pv <= 0 || price <= 0 {
		return 1 // safe default: 1 contract
	}
	contracts := math.Round(notionalUSD / (price * pv))
	if contracts < 1 {
		contracts = 1
	}
	// Chunk 3 — max-contracts clamp (was a silent hardcoded 10). maxContracts<=0
	// → no clamp (the guardrail is disabled by master/toggle). Log when clamping.
	if maxContracts > 0 && contracts > float64(maxContracts) {
		logger.Infof("  ⚠️ [RISK CONTROL] %s order %.0f contracts exceeds max %d — clamping to %d", symbol, contracts, maxContracts, maxContracts)
		contracts = float64(maxContracts)
	}
	return contracts
}

// resolveMaxContracts returns the futures max-contracts clamp for this trader's
// strategy (per-strategy value, else the 2-contract venue default; 6.6 comment-truth fix). Hardening D3
// (audit F2): ALWAYS ON — the guardrails master switch no longer disables it.
func (at *AutoTrader) resolveMaxContracts() int {
	// MENTOR P3 — the 0B size clamp does NOT apply to a mentor-mode trader:
	// mentor sizes come from the mentor table (up to mentor_max_contracts).
	// AI mode keeps the 2-contract default untouched.
	if mx, ok := at.mentorMaxContracts(); ok {
		return mx
	}
	if at.config.StrategyConfig == nil {
		return int(maxFuturesContracts)
	}
	rc := at.config.StrategyConfig.RiskControl
	return kernel.ResolveMaxContracts(rc.MaxContractsPerOrder, int(maxFuturesContracts))
}

// hlBool resolves a three-state *bool toggle (nil → default).
func hlBool(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// holdLockSuppressesClose reports whether the "Hold discipline" hold-lock should
// suppress this AI-initiated close: the per-strategy toggle is ON AND an OPEN
// position exists for the decision's symbol/side. When it returns true it has
// already logged the suppression loudly and marked the record as a deliberate
// no-op (Success, no error). The trade then rides to the stop/target the AI set —
// a real OCO bracket resting at the exchange (VLTraderTCPClient.SubmitBracketOnEntryFill),
// so the position stays protected. Emergency Flat (handler_risk.go) and the
// drawdown monitor call the trader directly and never reach this path, so human
// and safety closes always go through. Default OFF ⇒ behavior byte-identical.
func (at *AutoTrader) holdLockSuppressesClose(d *kernel.Decision, rec *store.DecisionAction) bool {
	if d.Action != "close_long" && d.Action != "close_short" {
		return false
	}
	if at.store == nil || at.config.StrategyConfig == nil {
		return false
	}
	if !hlBool(at.config.StrategyConfig.RiskControl.HoldDisciplineEnabled, false) {
		return false
	}
	side := "LONG"
	if d.Action == "close_short" {
		side = "SHORT"
	}
	openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, market.Normalize(d.Symbol), side)
	if err != nil || openPos == nil {
		return false // flat for this side → nothing to hold; allow the (no-op) close
	}
	// Best-effort current price for the log line (non-fatal if unavailable).
	px := 0.0
	if md, e := market.GetWithExchange(d.Symbol, at.exchange); e == nil {
		px = md.CurrentPrice
	}
	at.logWarnf("🔒 HOLD-LOCK: AI %s suppressed for %s (price %.2f, entry %.2f) — trade rides to stop/target.",
		d.Action, d.Symbol, px, openPos.EntryPrice)
	rec.Price = px
	rec.Success = true // deliberate no-op, not a failure
	return true
}

// consecutiveLossHalted reports whether new entries are blocked by the D1
// consecutive-loss halt: N consecutive LOSING closed trades in the current CME
// session-day (resets on a win/break-even close or a new session). N resolves
// through store.ResolveBreakerHalt (W1): a saved 0 = OFF, an absent knob
// inherits env BREAKER_HALT_N else 8. It is a per-strategy circuit breaker,
// NOT gated by the guardrails master switch.
// Fail-OPEN on a query error — never block a trade because the DB hiccuped.
//
// W-EXEC-TRUTH W0: it takes the caller's clock (admitEntry passes it; the
// wall-clock wrapper had no production caller left and was removed).
func (at *AutoTrader) consecutiveLossHaltedAt(now time.Time) (string, bool) {
	if at.store == nil || at.config.StrategyConfig == nil {
		return "", false
	}
	// D2 (2026-09-09) — ONE RESOLUTION, BOTH PATHS. This read the knob directly
	// and treated 0 as OFF; the ARM path resolves N through breakerHaltN, which
	// falls back to the [I] default when the owner has not set a value. Two
	// resolutions of one threshold is how the two paths come to disagree about
	// whether the desk is halted (A24: never a second copy).
	n := breakerHaltN(at.config.StrategyConfig)
	if n <= 0 {
		return "", false // OFF: a saved 0, or BREAKER_HALT_N=0 with no saved value
	}
	sinceMs := kernel.CMESessionDayStart(now).UnixMilli()
	losses, err := at.store.Position().CountConsecutiveLossesSince(at.id, sinceMs)
	if err != nil {
		at.logWarnf("consecutive-loss halt: count query failed (%v) — allowing entry (fail-open)", err)
		return "", false
	}
	if losses >= n {
		return fmt.Sprintf("%d consecutive losing trades this session (limit %d)", losses, n), true
	}
	return "", false
}

// executeDecisionWithRecord executes AI decision and records detailed information
func (at *AutoTrader) executeDecisionWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	return at.executeDecisionWithRecordAt(decision, actionRecord, time.Now())
}

// executeDecisionWithRecordAt is executeDecisionWithRecord on the caller's
// clock (W3): `now` is the instant the admission chain judges, and the strict
// nudge's armed pass judges the SAME instant.
func (at *AutoTrader) executeDecisionWithRecordAt(decision *kernel.Decision, actionRecord *store.DecisionAction, now time.Time) error {
	// W5.2 (weekly-bias wave) — SHADOW counter-trend annotation for entries.
	// Log/counters ONLY: this call can never block, resize or re-grade the trade
	// (the real gates below are untouched — W5.4 THE LAW).
	if decision.Action == "open_long" || decision.Action == "open_short" {
		at.applyWeeklyDecisionShadow(decision)
	}

	// MENTOR P3 — AI-entries-off for a mentor-mode trader. Mentor-sourced
	// decisions (MentorSourced) pass; every other open is refused and counted.
	// Closes, flattens and the safety paths never reach this refusal.
	if decision.Action == "open_long" || decision.Action == "open_short" {
		if refusal := at.mentorSuppressAIEntry(decision); refusal != "" {
			at.logWarnf("🧑‍🏫 %s (trader %s)", refusal, at.id)
			actionRecord.Success = false
			actionRecord.Error = refusal
			return nil
		}
	}

	// Feed-down gate (NinjaTrader, TRACK A), CLOSE half: the SIM cannot fill
	// without market data, so a flatten issued while the feed is down is
	// rejected ("no market data") — the upstream condition behind the
	// phantom-close mess. The position simply waits (the close path retries on
	// the next cycle / reconnect). Default-ALLOW until a feed_status frame
	// arrives. The ENTRY half runs FIRST inside admitEntry, below.
	switch decision.Action {
	case "close_long", "close_short":
		if down, status := at.ninjaFeedDown(); down {
			at.logWarnf("⛔ feed-gate: %s %s skipped — NT8 price feed not Connected (status=%q); SIM would reject 'no market data'. Will act when the feed returns.", decision.Action, decision.Symbol, status)
			telemetry.IncGateBlock(at.id, "feed_down")
			// W16/R3 — stamp the refusal like every sibling gate.
			actionRecord.Success = false
			actionRecord.Error = fmt.Sprintf("feed_down: NT8 price feed not Connected (status=%q)", status)
			return nil
		}
	}

	// W-EXEC-TRUTH W0 (a) — THE ONE ADMISSION GATE. The chain that ran inline
	// here (feed → dead-man → freeze → boot integrity → owner pause →
	// maintenance → contract roll → breaker → last entry → session → plan mode
	// → approval → EntryGate) now lives in entry_admission.go, in the SAME
	// pinned order with the SAME strings, and the armed path and Picture ask it
	// too. Closes are never admitted — only NEW entries.
	switch decision.Action {
	case "open_long", "open_short":
		if refusal, refused := at.admitEntry(admitIntent{
			Path: admitDecision, Symbol: decision.Symbol, Action: decision.Action,
			Now: now, Decision: decision, Record: actionRecord,
		}); refused {
			actionRecord.Success = false
			actionRecord.Error = refusal
			// W3 D13 — THE STRICT NUDGE. A decision refused ONLY for not being
			// on the arm path, that cites a matched scenario whose doc arm is a
			// market_in_zone arm, runs ONE armed pass for that scenario and the
			// record carries the executor's verdict. The decision itself stays
			// refused (Success false) — never a Path flip.
			if v, ok := at.strictNudgeAt(decision, refusal, now); ok {
				actionRecord.Error = refusal + " · 🚦 " + v
			}
			return nil
		}
	}

	// P3.5 — ADVISORY: record the executor's plan citation for entries (cited/
	// matched/off-plan match-rate via B6). Never gates — plan restricts, never
	// compels; hard gates already ran above.
	at.recordPlanCitation(decision)

	switch decision.Action {
	case "open_long":
		return at.executeOpenLongWithRecord(decision, actionRecord)
	case "open_short":
		return at.executeOpenShortWithRecord(decision, actionRecord)
	case "close_long":
		return at.executeCloseLongWithRecord(decision, actionRecord)
	case "close_short":
		return at.executeCloseShortWithRecord(decision, actionRecord)
	case "hold", "wait":
		// No execution needed, just record
		return nil
	default:
		return fmt.Errorf("unknown action: %s", decision.Action)
	}
}

// reconcileFlattenTimeout / PollInterval bound the flatten-first await in
// reconcileBeforeOpenNT — the auto-flatten polls NT8 net until flat, then opens.
const (
	// Heartbeat-aware backstop: the primary confirmation is the fill-confirmed
	// position_close FRAME (arrives ~instantly), so this timeout is only hit when no
	// frame comes — in which case we must give the 30s all-account positions
	// heartbeat a chance before refusing. 35s > the 30s heartbeat (was 6s, which
	// starved a non-active account whose snapshot only refreshes every 30s).
	reconcileFlattenTimeout      = 35 * time.Second
	reconcileFlattenPollInterval = 500 * time.Millisecond
)

// ntHeldPosition returns the side ("long"/"short") NT8 currently holds for symbol,
// or "" if flat / unreadable. Reads the NT8 positions snapshot via GetPositions.
func (at *AutoTrader) ntHeldPosition(symbol string) (string, error) {
	positions, err := at.trader.GetPositions()
	if err != nil {
		// CTO 2026-09-25 addendum 2 — an unreadable book is UNKNOWN, never
		// flat. Returning "" alone made reconcile read the error as "NT8
		// flat → proceed" — the exact unknown-as-flat reading F4 exists to
		// kill. Callers must refuse (or keep waiting) on the error.
		at.logWarnf("⚠️ positions read failed — reconcile REFUSES as unknown (never flat), reason: %v", err)
		telemetry.RecordError(at.id, "positions_read_failed", err.Error(), telemetry.CostNone)
		return "", err
	}
	for _, pos := range positions {
		if pos["symbol"] != symbol {
			continue
		}
		// W-EXEC-TRUTH W0 (canon 28): a held position is any NON-ZERO amount —
		// NT8 signs a SHORT negative, and the old `amt > 0` read every held
		// short as flat, so an entry netted onto it. The side is read through
		// the one canonicalizer (NT8 emits UPPERCASE; the earlier casing fix
		// lowered it only on the long branch).
		amt, _ := pos["positionAmt"].(float64)
		if amt != 0 {
			if side := brokerPositionSide(pos); side != "" {
				return side, nil
			}
		}
	}
	return "", nil
}

// reconcileBeforeOpenNT (TRACK B): NinjaTrader-only defense-in-depth run before an
// entry. The bot is about to OPEN, so it believes it is flat; if NT8 still holds a
// position for this symbol (an orphan), flatten-first AWAITING its own fill, then
// allow the open. If the flatten cannot be confirmed flat (timeout / feed drop),
// REFUSE the open — never compound onto an unreconciled net (the id=46 harm).
// No-op for non-NT traders or when NT8 is flat.
//
// STEP-A note: the open path already refuses to open on a SAME-side held position,
// and 0118ca77 (no phantom close → the bot knows it is still in a position) + the
// Track-A feed gate prevent the orphan at the source. This adds AUTO-RECOVERY
// (flatten + proceed instead of refusing every cycle) and widens to EITHER side.
// It does NOT cure a stale snapshot (the actual id=46 bypass) — that is Track A +
// 0118ca77; this acts only on a snapshot that positively reports a held position.
func (at *AutoTrader) reconcileBeforeOpenNT(symbol, intendedSide string) error {
	_, err := at.reconcileBeforeOpenNTReport(symbol, intendedSide)
	return err
}

// reconcileBeforeOpenNTReport is reconcileBeforeOpenNT that also reports
// whether it SUBMITTED an orphan flatten (W1b FOLD-2 repair): true from the
// moment CloseLong/CloseShort was called, whatever followed — so the chat door
// never tells a refusal that came after a flatten as "nothing was sent".
func (at *AutoTrader) reconcileBeforeOpenNTReport(symbol, intendedSide string) (flattenSent bool, err error) {
	if at.exchange != "ninjatrader" {
		return false, nil
	}
	// W117 F4 (CTO addendum 2) — an UNBOUND NT trader has no book to read and
	// cannot flatten; reconcile would refuse with a misleading "positions
	// unknown". Skip so the broker's own binding refusal names the cause (the
	// entry still refuses at the broker — fail-closed either way).
	if ntTCP, ok := at.trader.(*ntTrader.TCPTrader); ok && !ntTCP.IsBound() {
		return false, nil
	}
	// Never flatten into a dead feed (Track A also gates upstream; be defensive).
	if down, status := at.ninjaFeedDown(); down {
		return false, fmt.Errorf("reconcile-before-open: NT8 feed not Connected (%s) — refusing open", status)
	}
	held, hErr := at.ntHeldPosition(symbol)
	if hErr != nil {
		return false, fmt.Errorf("reconcile-before-open: %w — refusing open (an unreadable book is not an empty book)", hErr)
	}
	if held == "" {
		return false, nil // NT8 flat → proceed
	}
	// W-EXEC-TRUTH W0 (c): a position a LEDGER row explains is another
	// producer's (an armed fill, a Picture fill, one not yet materialized in
	// trader_positions) — never an orphan. Flattening it destroyed that
	// producer's trade and its bracket; the AI entry is refused instead, named.
	if owner, owned := at.ledgerExplainsPosition(symbol, held, time.Now()); owned {
		at.logWarnf("⛔ reconcile-before-open: NT8 holds a %s %s that the ledger explains (%s) — refusing the %s open; the position is NOT flattened.", held, symbol, owner, intendedSide)
		return false, fmt.Errorf("%w: %s", errPositionOwned, owner)
	}
	at.logWarnf("🚨 reconcile-before-open: NT8 holds a %s %s before an intended %s open — flattening first (awaiting fill) to avoid compounding onto an orphan.", held, symbol, intendedSide)
	// Timestamp BEFORE the flatten so we only accept a close that our flatten caused.
	t0 := time.Now().UnixMilli()
	var ferr error
	flattenSent = true // the flatten is submitted below, whatever follows
	if held == "long" {
		_, ferr = at.trader.CloseLong(symbol, 0)
	} else {
		_, ferr = at.trader.CloseShort(symbol, 0)
	}
	if ferr != nil {
		return true, fmt.Errorf("reconcile-before-open: flatten submit failed: %w", ferr)
	}
	// AWAIT its own fill (NOT fire-and-forget — the trap 0118ca77 fixed). Prefer the
	// FILL-CONFIRMED close FRAME (position_close), which arrives ~instantly for the
	// bound account even when it is NOT the streamed/active account — so a non-active
	// trader no longer waits on the 30s positions-snapshot heartbeat. The snapshot
	// (ntHeldPosition) remains a fallback, and the timeout is heartbeat-aware.
	ntTCP, _ := at.trader.(*ntTrader.TCPTrader)
	deadline := time.Now().Add(reconcileFlattenTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(reconcileFlattenPollInterval)
		if down, _ := at.ninjaFeedDown(); down {
			return true, fmt.Errorf("reconcile-before-open: feed dropped during flatten — refusing open")
		}
		// Frame path (fast, account-correct): our flatten's close was fill-confirmed.
		if ntTCP != nil && ntTCP.CloseConfirmedSince(symbol, held, t0) {
			at.logInfof("✅ reconcile-before-open: %s flatten fill-confirmed via position_close frame — proceeding to open.", symbol)
			return true, nil
		}
		// Snapshot fallback (covers a manual/external flatten with no close frame).
		// An UNKNOWN read here is NOT flat: keep waiting (the deadline
		// refusal fires on timeout — never declare flat on an error).
		if heldNow, hErr := at.ntHeldPosition(symbol); hErr == nil && heldNow == "" {
			at.logInfof("✅ reconcile-before-open: %s flattened + confirmed flat (snapshot) — proceeding to open.", symbol)
			return true, nil
		}
	}
	return true, fmt.Errorf("reconcile-before-open: flatten not confirmed flat within %s — refusing open (never compound)", reconcileFlattenTimeout)
}

// openEntryMarketRead is the open path's market read — market.GetWithExchange
// in production. A package var only so a test can drive a non-CME venue's open
// offline (that read is a network call); nothing reassigns it at run time.
var openEntryMarketRead = market.GetWithExchange

// manualOpen marks an open as the agent-chat door's (W1b FOLD-2): the entry
// runs the AI decision's own execute path with the OWNER's quantity. The path
// records whether a broker write was reached, so the door can tell a refusal
// before any send from the broker's own failure (E9's typed errors).
type manualOpen struct {
	// Quantity is the owner's requested quantity (contracts on CME), sent as
	// typed: the door already refused one that breaks the cap — never clamped.
	Quantity float64
	// brokerCalled is set immediately before the entry's first broker write
	// (the CME bracket set, or the open itself).
	brokerCalled bool
	// flattenSent is set when reconcile-before-open SUBMITTED an orphan
	// flatten before this entry (W1b FOLD-2 repair): a later refusal or
	// failure of the entry is then never told as "nothing was sent".
	flattenSent bool
	// order is what the broker returned for the open.
	order map[string]interface{}
}

// bracketCarryingEntrySender is a broker whose market entry carries ITS OWN
// bracket into the send (W1b FOLD-3: *ntTrader.TCPTrader.OpenWithBracket) —
// the shared (symbol, side) SL/TP maps learn it only when the entry may be on
// the wire. Not part of the 19-method Trader interface.
type bracketCarryingEntrySender interface {
	OpenWithBracket(symbol, side string, quantity, stop, target float64) (map[string]interface{}, error)
}

var _ bracketCarryingEntrySender = (*ntTrader.TCPTrader)(nil)

// executeOpenLongWithRecord executes open long position and records detailed information
func (at *AutoTrader) executeOpenLongWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	return at.executeOpenLong(decision, actionRecord, nil)
}

// executeOpenLong is the long open's ONE execute path (W1b FOLD-2): an AI
// decision (manual == nil) and an agent-chat entry (manual != nil) alike.
func (at *AutoTrader) executeOpenLong(decision *kernel.Decision, actionRecord *store.DecisionAction, manual *manualOpen) error {
	logger.Infof("  📈 Open long: %s", decision.Symbol)

	// TRACK B — reconcile NT8 net before opening; flatten an orphan first or refuse.
	flattened, err := at.reconcileBeforeOpenNTReport(decision.Symbol, "long")
	if manual != nil {
		manual.flattenSent = flattened // FOLD-2 repair: the door tells a flatten that went out
	}
	if err != nil {
		return at.reconcileRefusal(err, actionRecord)
	}
	return at.openEntryWithRecord(decision, actionRecord, "long", manual)
}

// executeOpenShortWithRecord executes open short position and records detailed information
func (at *AutoTrader) executeOpenShortWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	return at.executeOpenShort(decision, actionRecord, nil)
}

// executeOpenShort is the short open's ONE execute path (W1b FOLD-2): an AI
// decision (manual == nil) and an agent-chat entry (manual != nil) alike.
func (at *AutoTrader) executeOpenShort(decision *kernel.Decision, actionRecord *store.DecisionAction, manual *manualOpen) error {
	logger.Infof("  📉 Open short: %s", decision.Symbol)

	// TRACK B — reconcile NT8 net before opening; flatten an orphan first or refuse.
	flattened, err := at.reconcileBeforeOpenNTReport(decision.Symbol, "short")
	if manual != nil {
		manual.flattenSent = flattened // FOLD-2 repair: the door tells a flatten that went out
	}
	if err != nil {
		return at.reconcileRefusal(err, actionRecord)
	}
	return at.openEntryWithRecord(decision, actionRecord, "short", manual)
}

// openEntryWithRecord is the open after reconcile, one body for both sides
// (W1b FOLD-2 — it was two copies): max positions, the same-side check,
// sizing, the bracket, the send, and the order record.
//
// manual != nil is the agent-chat door: its quantity is the owner's (the AI's
// notional sizing never re-sizes it), its position never takes the AI
// decision's plan citation, a failed bracket is typed (CME: nothing sent;
// other venues: *ManualEntryUnprotected), and it never writes the two
// cycle-goroutine maps (entryTheses, positionFirstSeenTime) — the door runs
// on the chat's goroutine, and those maps have no lock.
func (at *AutoTrader) openEntryWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction, side string, manual *manualOpen) error {
	upper, action, open := "LONG", "open_long", at.trader.OpenLong
	if side == "short" {
		upper, action, open = "SHORT", "open_short", at.trader.OpenShort
	}

	// ⚠️ Get current positions for multiple checks
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}

	// [CODE ENFORCED] Check max positions limit
	if err := at.enforceMaxPositions(len(positions)); err != nil {
		return err
	}

	// Check if there's already a position in the same symbol and direction
	for _, pos := range positions {
		if pos["symbol"] == decision.Symbol && brokerPositionSide(pos) == side {
			return fmt.Errorf("❌ %s already has %s position, close it first", decision.Symbol, side)
		}
	}

	// Get current price
	marketData, err := openEntryMarketRead(decision.Symbol, at.exchange)
	if err != nil {
		return err
	}

	// Calculate order quantity.
	var quantity float64
	if manual != nil {
		// The owner's quantity, as typed (the door judged it against the
		// max-contracts cap and whole contracts before this path ran).
		quantity = manual.Quantity
	} else {
		// Get balance (needed for multiple checks)
		balance, err := at.trader.GetBalance()
		if err != nil {
			return fmt.Errorf("failed to get account balance: %w", err)
		}
		availableBalance := 0.0
		if avail, ok := balance["availableBalance"].(float64); ok {
			availableBalance = avail
		}

		// Get equity for position value ratio check
		equity := 0.0
		if eq, ok := balance["totalEquity"].(float64); ok && eq > 0 {
			equity = eq
		} else if eq, ok := balance["totalWalletBalance"].(float64); ok && eq > 0 {
			equity = eq
		} else {
			equity = availableBalance // Fallback to available balance
		}

		// [CODE ENFORCED] Position Value Ratio Check: position_value <= equity × ratio
		adjustedPositionSize, wasCapped := at.enforcePositionValueRatio(decision.PositionSizeUSD, equity, decision.Symbol)
		if wasCapped {
			decision.PositionSizeUSD = adjustedPositionSize
		}

		if market.IsCMEFuturesSymbol(decision.Symbol) {
			// CME futures: size in contracts (notional / (price × point value)),
			// clamped. Skip the crypto notional/leverage margin model — futures
			// margin is per-contract, not notional/leverage.
			quantity = futuresOrderQuantity(decision.Symbol, decision.PositionSizeUSD, marketData.CurrentPrice, at.resolveMaxContracts())
		} else {
			// ⚠️ Auto-adjust position size if insufficient margin (crypto)
			// Formula: totalRequired = positionSize/leverage + positionSize*0.001 + positionSize/leverage*0.01
			//        = positionSize * (1.01/leverage + 0.001)
			marginFactor := 1.01/float64(decision.Leverage) + 0.001
			maxAffordablePositionSize := availableBalance / marginFactor

			actualPositionSize := decision.PositionSizeUSD
			if actualPositionSize > maxAffordablePositionSize {
				// Use 98% of max to leave buffer for price fluctuation
				adjustedSize := maxAffordablePositionSize * 0.98
				logger.Infof("  ⚠️ Position size %.2f exceeds max affordable %.2f, auto-reducing to %.2f",
					actualPositionSize, maxAffordablePositionSize, adjustedSize)
				actualPositionSize = adjustedSize
				decision.PositionSizeUSD = actualPositionSize
			}

			// [CODE ENFORCED] Minimum position size check
			if err := at.enforceMinPositionSize(decision.PositionSizeUSD); err != nil {
				return err
			}

			// Calculate quantity with adjusted position size
			quantity = actualPositionSize / marketData.CurrentPrice
		}
	}
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice

	// Set margin mode
	if err := at.trader.SetMarginMode(decision.Symbol, at.config.IsCrossMargin); err != nil {
		logger.Infof("  ⚠️ Failed to set margin mode: %v", err)
		// Continue execution, doesn't affect trading
	}

	// W1b FOLD-3 — the NT8 broker carries the entry's OWN bracket into the
	// send (OpenWithBracket): nothing is written to the shared (symbol, side)
	// SL/TP maps before it, so a refused or provably-unsent entry never leaves
	// its stop where MoveStopToBreakeven's widen ban reads the live one.
	carrier, carries := at.trader.(bracketCarryingEntrySender)
	carries = carries && market.IsCMEFuturesSymbol(decision.Symbol)

	// CME futures (NT8) require SL/TP set BEFORE the entry — the AddOn places
	// the market entry + protective OCO bracket atomically from the signal,
	// which carries SL/TP. (Crypto sets them after the fill, below.) Without
	// this, placeEntry errors "SetStopLoss and SetTakeProfit must be called
	// before long". A chat entry is never sent without its own bracket: a
	// failed set sends nothing (W1b E9). A bracket-carrying broker needs no
	// set here (FOLD-3, above).
	//
	// W1b FOLD-10 — the set and the open are ONE section per AutoTrader: the
	// maps are keyed (symbol, side), and the chat door (HTTP goroutine) and
	// the AI decision (cycle goroutine) both run this path; a foreign set
	// between this entry's set and its send sent it on the other's bracket.
	endSend := at.lockEntrySend()
	defer endSend()
	if market.IsCMEFuturesSymbol(decision.Symbol) && !carries {
		if manual != nil {
			manual.brokerCalled = true
		}
		if err := at.trader.SetStopLoss(decision.Symbol, upper, quantity, decision.StopLoss); err != nil {
			if manual != nil {
				return fmt.Errorf("manual entry NOT sent: its own bracket could not be set (set stop %.2f: %v) — never sent on another decision's bracket (fail-closed)", decision.StopLoss, err)
			}
			at.logErrorf("🚨 pre-entry bracket STOP set FAILED for %s — %v (entry proceeds without the protective stop)", upper, err)
			telemetry.RecordError(at.id, "bracket_set_failed", "pre-entry SetStopLoss: "+err.Error(), telemetry.CostTradeLost)
		}
		if err := at.trader.SetTakeProfit(decision.Symbol, upper, quantity, decision.TakeProfit); err != nil {
			if manual != nil {
				return fmt.Errorf("manual entry NOT sent: its own bracket could not be set (set target %.2f: %v) — never sent on another decision's bracket (fail-closed)", decision.TakeProfit, err)
			}
			at.logErrorf("🚨 pre-entry bracket TARGET set FAILED for %s — %v (entry proceeds without the protective target)", upper, err)
			telemetry.RecordError(at.id, "bracket_set_failed", "pre-entry SetTakeProfit: "+err.Error(), telemetry.CostTradeLost)
		}
	}

	// Open position
	if manual != nil {
		manual.brokerCalled = true
	}
	var order map[string]interface{}
	// MENTOR P3 — a mentor-sourced decision rests a STOP-ENTRY at the
	// evaluator's trigger price. Every gate above (admission, one-contract,
	// sizing) has already run; only the wire hop differs from the AI's market
	// entry. A broker without PlaceStopEntry refuses fail-closed (no trade).
	if decision.MentorSourced && decision.EntryPrice > 0 {
		sp, ok := at.trader.(mentorStopEntryPlacer)
		if !ok {
			mentorCount("refused_no_stop_entry_frame")
			telemetry.IncGateBlock(at.id, "mentor_no_stop_entry_frame")
			return fmt.Errorf("mentor stop-entry: the broker carries no PlaceStopEntry frame — refusing rather than market-filling a stop order")
		}
		order, err = sp.PlaceStopEntry(decision.Symbol, side, quantity,
			decision.EntryPrice, decision.StopLoss, decision.TakeProfit)
		if err == nil {
			at.logInfof("🧑‍🏫 mentor stop-entry resting: %s %s %.0f @ %.2f (stop %.2f, target %.2f)",
				decision.Symbol, side, quantity, decision.EntryPrice, decision.StopLoss, decision.TakeProfit)
			mentorCount("stop_entry_placed")
		}
	} else if carries {
		order, err = carrier.OpenWithBracket(decision.Symbol, side, quantity, decision.StopLoss, decision.TakeProfit)
	} else {
		order, err = open(decision.Symbol, quantity, decision.Leverage)
	}
	endSend() // FOLD-10: the send has returned; the confirmation poll runs outside the section
	if err != nil {
		return err
	}
	if manual != nil {
		manual.order = order
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	logger.Infof("  ✓ Position opened successfully, order ID: %v, quantity: %.4f", order["orderId"], quantity)

	// Record order to database and poll for confirmation
	if manual == nil {
		at.captureEntryThesis(decision, upper, marketData.CurrentPrice) // Phase 3: the watcher's anchor
	}
	var chat *chatOpenBracket // W1b FOLD-2 repair: the chat's own bracket → its excursion row
	if manual != nil {
		chat = &chatOpenBracket{stop: decision.StopLoss, target: decision.TakeProfit}
	}
	at.recordAndConfirmOrderAs(order, decision.Symbol, action, quantity, marketData.CurrentPrice, decision.Leverage, 0, decision.Confidence, chat)

	// Record position opening time
	if manual == nil {
		posKey := decision.Symbol + "_" + side
		at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()
	}

	// Set stop loss and take profit — inside the entry-send section too
	// (FOLD-10): on a map-keyed broker this write would otherwise land between
	// ANOTHER entry's set and its send.
	endBracket := at.lockEntrySend()
	defer endBracket()
	var bracketErr error
	if err := at.trader.SetStopLoss(decision.Symbol, upper, quantity, decision.StopLoss); err != nil {
		logger.Infof("  ⚠ Failed to set stop loss: %v", err)
		bracketErr = fmt.Errorf("set stop %.2f: %w", decision.StopLoss, err)
	}
	if err := at.trader.SetTakeProfit(decision.Symbol, upper, quantity, decision.TakeProfit); err != nil {
		logger.Infof("  ⚠ Failed to set take profit: %v", err)
		if bracketErr == nil {
			bracketErr = fmt.Errorf("set target %.2f: %w", decision.TakeProfit, err)
		}
	}
	endBracket()
	// A chat entry on a venue that opens first and sets after (not CME: its
	// bracket rode the signal) is a LIVE position with no bracket if the set
	// failed — told as OPENED and UNPROTECTED, never as a failure.
	if manual != nil && bracketErr != nil && !market.IsCMEFuturesSymbol(decision.Symbol) {
		at.logErrorf("🚨 manual entry %s %s OPENED but its own bracket failed to set — %v (position unprotected)", decision.Symbol, upper, bracketErr)
		return &ManualEntryUnprotected{Symbol: decision.Symbol, Side: upper, Order: order, Err: bracketErr}
	}

	return nil
}

// executeCloseLongWithRecord executes close long position and records detailed information
func (at *AutoTrader) executeCloseLongWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  🔄 Close long: %s", decision.Symbol)

	// Get current price
	marketData, err := market.GetWithExchange(decision.Symbol, at.exchange)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// Normalize symbol for database lookup
	normalizedSymbol := market.Normalize(decision.Symbol)

	// Get entry price and quantity - prioritize local database for accurate quantity
	var entryPrice float64
	var quantity float64

	// First try to get from local database (more accurate for quantity)
	if at.store != nil {
		if openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, normalizedSymbol, "LONG"); err == nil && openPos != nil {
			quantity = openPos.Quantity
			entryPrice = openPos.EntryPrice
			logger.Infof("  📊 Using local position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
		}
	}

	// Fallback to exchange API if local data not found
	if quantity == 0 {
		positions, err := at.trader.GetPositions()
		if err == nil {
			for _, pos := range positions {
				if pos["symbol"] == decision.Symbol && brokerPositionSide(pos) == "long" {
					if ep, ok := pos["entryPrice"].(float64); ok {
						entryPrice = ep
					}
					if amt, ok := pos["positionAmt"].(float64); ok && amt > 0 {
						quantity = amt
					}
					break
				}
			}
		}
		logger.Infof("  📊 Using exchange position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
	}

	// Close position
	order, err := at.trader.CloseLong(decision.Symbol, 0) // 0 = close all
	if err != nil {
		return err
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "close_long", quantity, marketData.CurrentPrice, 0, entryPrice, 0)

	logger.Infof("  ✓ Position closed successfully")
	return nil
}

// executeCloseShortWithRecord executes close short position and records detailed information
func (at *AutoTrader) executeCloseShortWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  🔄 Close short: %s", decision.Symbol)

	// Get current price
	marketData, err := market.GetWithExchange(decision.Symbol, at.exchange)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// Normalize symbol for database lookup
	normalizedSymbol := market.Normalize(decision.Symbol)

	// Get entry price and quantity - prioritize local database for accurate quantity
	var entryPrice float64
	var quantity float64

	// First try to get from local database (more accurate for quantity)
	if at.store != nil {
		if openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, normalizedSymbol, "SHORT"); err == nil && openPos != nil {
			quantity = openPos.Quantity
			entryPrice = openPos.EntryPrice
			logger.Infof("  📊 Using local position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
		}
	}

	// Fallback to exchange API if local data not found
	if quantity == 0 {
		positions, err := at.trader.GetPositions()
		if err == nil {
			for _, pos := range positions {
				if pos["symbol"] == decision.Symbol && brokerPositionSide(pos) == "short" {
					if ep, ok := pos["entryPrice"].(float64); ok {
						entryPrice = ep
					}
					if amt, ok := pos["positionAmt"].(float64); ok {
						quantity = -amt // positionAmt is negative for short
					}
					break
				}
			}
		}
		logger.Infof("  📊 Using exchange position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
	}

	// Close position
	order, err := at.trader.CloseShort(decision.Symbol, 0) // 0 = close all
	if err != nil {
		return err
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "close_short", quantity, marketData.CurrentPrice, 0, entryPrice, 0)

	logger.Infof("  ✓ Position closed successfully")
	return nil
}

// isBootIntegrityGatedAction documents (and lets tests assert) which actions the
// P1 boot-integrity refusal blocks: NEW ENTRIES ONLY. Closes, holds and waits are
// never gated — a refused process must still be able to manage an open position
// down to flat.
func isBootIntegrityGatedAction(action string) bool {
	return action == "open_long" || action == "open_short"
}
