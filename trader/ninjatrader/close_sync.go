package ninjatrader

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vl/discipline"
	"vl/logger"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/safe"
	"vl/store"
	"vl/store/sqlitedriver"
	"vl/telemetry"
)

// StartCloseSync consumes position_close frames from the TCP bridge and records
// each close into trader_positions (real exit price + futures realized PnL),
// then clears the in-memory fill so GetPositions reports flat.
//
// NT closes positions broker-side via the OCO bracket (SL/TP); the bot never
// issues a close_* order and — unlike every crypto broker — NT has no
// order-sync. So this is the ONLY path that transitions an open NT position to
// CLOSED, which is what populates the dashboard's position history.
func (t *TCPTrader) StartCloseSync(traderID, exchangeID, exchangeType string, st *store.Store) {
	if st == nil {
		return
	}
	// A2 (G1) — record the owning trader id so outbound order frames can stamp it.
	t.mu.Lock()
	t.traderID = traderID
	t.st = st // Entry rejection receipts need the ledger before any placement.
	t.mu.Unlock()
	pb := store.NewPositionBuilder(st.Position())
	t.closeSyncOnce.Do(func() {
		done := t.observerLifetime()
		// W117 F5 — subscribe BEFORE the goroutines: a delayed old goroutine must
		// never subscribe after the replacement and steal its account stream.
		closes := t.server.SubscribeClosesFor(t.symbol, t.boundAccount)
		rejects := t.server.SubscribeRejectsFor(t.symbol, t.boundAccount)
		instruments := t.server.SubscribeInstrumentInfoFor(t.symbol, t.boundAccount)
		safe.GoNet("nt8-close-sync", "traderID", func() {
			defer close(done) // drain all queued receipts before stopping reconcile
			for p := range closes {
				// W117 F2 — the ordered worker owns this frame's durable close; the
				// advisory copy only marks it (never records it twice), and the
				// consumer still drains its channel and closes done after the skip.
				if p.OrderedOwned {
					t.MarkCloseConfirmed(p.Symbol, p.PositionSide)
					continue
				}
				t.recordClose(traderID, exchangeID, exchangeType, st, pb, p)
				// Fast, account-correct flat signal for reconcile-before-open: a
				// position_close arrived for this trader's bound account (frame path
				// beats the 30s positions-snapshot heartbeat on a non-active account).
				t.MarkCloseConfirmed(p.Symbol, p.PositionSide)
			}
		})
		// Rejected exit/flatten watcher. The SIM/broker refused a close (e.g. "no
		// market data" with the feed down). The position is STILL OPEN in NT8 — do
		// NOT record a close; raise a loud alarm. Because decision-driven closes no
		// longer mark the DB CLOSED off the mark (see auto_trader_decision.go), the
		// position simply stays open: the next decision cycle re-issues the close (a
		// natural bounded retry) and the periodic reconcile keeps the DB anchored to
		// NT8 truth, so the orphan can't be netted onto by the next entry.
		safe.GoNet("nt8-reject-watch", "traderID", func() {
			for r := range rejects { // P5.4 router-fed (per-symbol)
				logger.Warnf("🚨 NT close REJECTED: %s %s — STILL OPEN in NT8, NOT recording closed (reason: %q, account: %s). Will retry on next decision cycle / reconnect.",
					r.Symbol, r.PositionSide, r.Reason, r.Account)
			}
		})
		// Instrument-info watcher (Phase 4): NT8 reports the RESOLVED instrument's
		// real specs. Cross-check the hardcoded tables — they are CME-correct, so a
		// divergence is a drift signal worth surfacing. For a parked/unknown symbol
		// (no table entry) NT8 is the only source. The tables stay authoritative for
		// the math; this is defense-in-depth + drift detection.
		safe.GoNet("nt8-instrument-watch", "traderID", func() {
			for in := range instruments { // P5.4 router-fed (per-symbol)
				tablePV := market.FuturesPointValue(in.Symbol)
				tableTick := market.FuturesTickSize(in.Symbol)
				switch {
				case tablePV <= 0:
					logger.Infof("📐 NT8 instrument_info %s (%s): point_value=%.4g tick=%.6g — no table entry (parked/unknown); NT8 is the source.",
						in.Symbol, in.Contract, in.PointValue, in.TickSize)
				case in.PointValue != tablePV || (tableTick > 0 && in.TickSize != tableTick):
					logger.Warnf("🚨 instrument spec DRIFT %s (%s): NT8 point_value=%.4g tick=%.6g vs table point_value=%.4g tick=%.6g — verify the table.",
						in.Symbol, in.Contract, in.PointValue, in.TickSize, tablePV, tableTick)
				default:
					logger.Infof("📐 NT8 instrument_info %s (%s): point_value=%.4g tick=%.6g — matches table ✓",
						in.Symbol, in.Contract, in.PointValue, in.TickSize)
				}
			}
		})
		logger.Infof("🔄 NinjaTrader close-sync started (records SL/TP + manual exits; alarms on rejected flattens; cross-checks NT8 instrument specs)")
	})
}

func (t *TCPTrader) recordClose(
	traderID, exchangeID, exchangeType string,
	st *store.Store,
	pb *store.PositionBuilder,
	p ntwire.PositionClosePayload,
) {
	side, action := "LONG", "close_long"
	if strings.EqualFold(p.PositionSide, "short") {
		side, action = "SHORT", "close_short"
	}

	// The open record is keyed by the canonical bot symbol (root, e.g. "MNQ"),
	// not the resolved front-month contract — prefer t.symbol.
	symbol := t.symbol
	if symbol == "" {
		symbol = p.Symbol
	}
	qty := float64(p.Quantity)
	if qty <= 0 {
		qty = 1
	}

	exitMs := time.Now().UTC().UnixMilli()
	if p.ExitTime != "" {
		if ts, err := time.Parse(time.RFC3339, p.ExitTime); err == nil {
			exitMs = ts.UTC().UnixMilli()
		}
	}

	// OWNER-ROUTING: a position_close routes by SYMBOL to ONE trader's close-sync,
	// which may NOT own the open row when multiple traders share a symbol — recording
	// against the RECEIVER's trader_id then missed and the priced close was lost
	// (reconcile later wrote exit=entry pnl=0). Find the trader that actually OWNS the
	// open (account, symbol, side) row across ALL traders and record against IT.
	// Single-trader: owner == this trader → byte-identical.
	owner, oerr := st.Position().GetOpenPositionByAccountSymbol(p.Account, symbol, side)
	if oerr != nil {
		logger.Warnf("ninjatrader/tcp: recordClose owner lookup failed (%s %s acct=%q): %v", symbol, side, p.Account, oerr)
	}
	if owner == nil {
		// PRICED close with NO matching open row anywhere. The old code logged a FALSE
		// "📕 pnl" here (ProcessTrade skipped silently) and reconcile later wrote
		// exit=entry pnl=0. Instead: alarm loudly and PARK the price so reconcile's
		// orphan-close consumes it (priced-frame fallback) rather than fabricate a 0.
		logger.Warnf("⚠️ ninjatrader/tcp: priced close DROPPED — no matching open row (trader=%s acct=%q sym=%s side=%s exit=%.2f). Parked for reconcile fallback.",
			traderID, p.Account, symbol, side, p.ExitPrice)
		putPricedClose(p.Account, symbol, side, p.ExitPrice, qty, exitMs)
		t.mu.Lock()
		t.hasFill = false
		t.mu.Unlock()
		return
	}

	// Realized P&L with the futures point value (PositionBuilder's fallback omits it),
	// computed from the OWNING row's entry — and the OWNING ROW's QUANTITY.
	//
	// P0 pnl-record-integrity (2026-08-20): a MANUAL flatten in NT8 emits ONE
	// position_close frame for the account's WHOLE flattened size. Position
	// #526 proved it live: the bot held 1 lot short, the owner's manual
	// activity flattened 21 contracts, the frame said qty=21 avg=29660.96, and
	// this code recorded −$1,458 on a 1-lot row whose true loss was −$69.43.
	// The frame's qty belongs to the NT8 POSITION EVENT; only the row's own
	// size may ever be attributed to the row.
	attributedQty := owner.Quantity
	if attributedQty <= 0 {
		attributedQty = 1
	}
	if qty > attributedQty {
		logger.Warnf("⚖️ pnl-attribution: position_close frame carries qty=%.0f but the owning row holds %.2f — attributing the ROW's size only (the excess is foreign/manual activity on the same account; exit price %.2f is the frame's average).",
			qty, attributedQty, p.ExitPrice)
	}
	pv := market.FuturesPointValue(symbol)
	if pv <= 0 {
		pv = 1
	}
	realizedPnL := 0.0
	if owner.EntryPrice > 0 {
		if side == "LONG" {
			realizedPnL = (p.ExitPrice - owner.EntryPrice) * attributedQty * pv
		} else {
			realizedPnL = (owner.EntryPrice - p.ExitPrice) * attributedQty * pv
		}
	}

	// D3 — the broker's own cause travels WITH the close instead of being
	// logged and thrown away. p.ExitReason is the same value line 204 below
	// uses to arm the re-entry cooldown.
	if err := pb.ProcessTradeWithExitReason(owner.TraderID, exchangeID, exchangeType, symbol, side, action,
		attributedQty, p.ExitPrice, 0, realizedPnL, exitMs, p.SignalID, p.ExitReason); err != nil {
		logger.Warnf("ninjatrader/tcp: record close failed (%s %s): %v", symbol, side, err)
	} else {
		// 4.2 — exit-fill persistence (NT8 SIM lineage): entries record fills in
		// trader_fills (executeDecisionWithRecord poll path), exits NEVER did — NT
		// closes return early there and wait for THIS frame. Write the tick-exact
		// exit fill now, keyed deterministically on the owning position row so a
		// retransmitted position_close frame can never double-count (CreateFill
		// dedupes on exchange_trade_id).
		if fill := buildExitFill(owner, exchangeID, exchangeType, symbol, side,
			p.ExitPrice, attributedQty, realizedPnL, exitMs, p.SignalID); fill != nil {
			if err := st.Order().CreateFill(fill); err != nil {
				logger.Warnf("ninjatrader/tcp: exit fill record failed (%s %s): %v", symbol, side, err)
			} else {
				logger.Infof("📊 exit fill recorded: %s %s qty=%.2f @ %.2f (tick-exact, pnl %.2f)",
					symbol, side, attributedQty, p.ExitPrice, realizedPnL)
			}
		}
		// Phase 4 (final-bundle): notify the owning trader — one post-exit rescan.
		if OnPositionClosed != nil {
			OnPositionClosed(owner.TraderID, owner.ID)
		}
		// T7 (2026-08-27) — the close path stamps pnl_corrected on the
		// row immediately (same recompute the readers COALESCE to), so the
		// column is non-NULL on every NEW close. The Δ≥$0.50 class-killer
		// WARN lives inside the stamp.
		st.StampPnlCorrectedOnClose(owner.ID, realizedPnL, realizedPnL)
		// WARN (honest-logs 2026-08-19): a position close with realized P&L is
		// owner-visible truth — must reach the log_events sink + dashboard even
		// under journald frame-flood suppression.
		logger.Warnf("📕 NT position closed: %s %s qty=%.2f exit=%.2f reason=%s pnl=%.2f (owner=%s)",
			symbol, side, attributedQty, p.ExitPrice, p.ExitReason, realizedPnL, owner.TraderID)
	}

	// B7 — re-entry cooldown: a STOP-LOSS exit (NT8 reason "sl") arms the
	// same-direction re-entry cooldown for the OWNING trader on this symbol, measured
	// from the SL fill price. Target/manual exits do NOT arm it. The kernel entry
	// gate (applyReentryCooldown) enforces it; 0 = OFF disables it there.
	if strings.EqualFold(p.ExitReason, "sl") {
		discipline.NoteStopLossExit(owner.TraderID, symbol, side, p.ExitPrice, exitMs)
		logger.Infof("⏳ re-entry cooldown armed: %s %s stop=%.2f (owner=%s)", symbol, side, p.ExitPrice, owner.TraderID)
	}

	// Mark flat so GetPositions stops reporting the now-closed position.
	t.mu.Lock()
	t.hasFill = false
	t.mu.Unlock()
}

// W117 a3 — bounded busy retry on the worker, in receive order. Five total
// tries (the first call + ntExitBusyRetries retries); the backoff sleeps sum to
// 1.55s, so the worker's own retry schedule stays inside ~3s wall. Each
// attempt's BEGIN IMMEDIATE busy wait is capped by the store's busy_timeout
// (5000ms live); a holder that outlives it parks the exit instead of dropping.
const ntExitBusyRetries = 4

func ntExitBusyBackoff(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 50 * time.Millisecond
	case 2:
		return 100 * time.Millisecond
	case 3:
		return 200 * time.Millisecond
	default:
		return 400 * time.Millisecond
	}
}

// ── W117 F3 / slice-A R6 — durable exit receipts, apply-or-park ON THE WORKER ──
//
// recordCloseOrdered is the ordered worker's close consumer: the same evidence
// recordClose applies, but through store.ApplyNT8Exit — one transaction that
// reduces the exact owned residual, writes the exit fill (deduped by receipt
// id), flips the receipt, and closes + stamps pnl_corrected when the residual
// hits zero. A valid exit that beats its later cumulative entry update is
// RETAINED as a pending receipt — never dropped, never a hard error — and
// retried (RetryPendingNT8Exits) once the row catches up.

func (t *TCPTrader) recordCloseOrdered(
	traderID, exchangeID, exchangeType string,
	st *store.Store,
	p ntwire.PositionClosePayload,
) {
	if st == nil || st.Position() == nil {
		return
	}
	side := "LONG"
	if strings.EqualFold(p.PositionSide, "short") {
		side = "SHORT"
	}
	symbol := t.symbol
	if symbol == "" {
		symbol = p.Symbol
	}
	qty := float64(p.Quantity)
	if qty <= 0 {
		qty = 1
	}
	exitMs := time.Now().UTC().UnixMilli()
	if p.ExitTime != "" {
		if ts, err := time.Parse(time.RFC3339, p.ExitTime); err == nil {
			exitMs = ts.UTC().UnixMilli()
		}
	}
	reason := strings.ToLower(strings.TrimSpace(p.ExitReason))
	leg := "exit"
	if reason == "sl" || reason == "tp" {
		leg = reason
	}
	// Receipt identity: stable for idempotent replay of the SAME frame, and
	// distinct for a partial vs a final close of the same bracket leg (qty +
	// exit time are part of the identity when no broker exit-order id exists).
	// A split bracket's two legs share the signal id, reason, qty and (RFC3339)
	// second, so the bracket leg joins the identity — appended ONLY when set, so
	// every single-bracket key stays byte-identical to the pre-split receipts.
	parts := []any{p.Account, symbol, side, p.SignalID, leg, p.Seq, qty, exitMs}
	if p.Leg > 0 {
		parts = append(parts, p.Leg)
	}
	identity, _ := json.Marshal(parts)
	key := fmt.Sprintf("nt8-exit-v2-%x", sha256.Sum256(identity))
	receipt := store.NT8ExitReceipt{
		ID: key, Account: p.Account, Symbol: symbol, Side: side, SignalID: p.SignalID,
		TraderID: traderID, ExchangeID: exchangeID, ExchangeType: exchangeType,
		Reason: reason, Quantity: qty, Price: p.ExitPrice,
		PointValue: market.FuturesPointValue(symbol), ExitMs: exitMs, ReceivedMs: time.Now().UnixMilli(),
	}
	if receipt.PointValue <= 0 {
		receipt.PointValue = 1
	}

	// Earlier parked exits for this account retry FIRST: by the time a later
	// close arrives, the rows the earlier exits were waiting on may exist.
	t.RetryPendingNT8Exits(st)

	// W117 a3 — SQLite lock contention must never lose a close. BEGIN
	// IMMEDIATE already moves the lock wait to transaction start (the busy
	// handler applies there); when the holder outlives busy_timeout the worker
	// retries BOUNDED, in receive order, then parks on final failure.
	result, err := st.Position().ApplyNT8Exit(receipt)
	if err != nil && sqlitedriver.IsBusy(err) {
		for attempt := 1; attempt <= ntExitBusyRetries && sqlitedriver.IsBusy(err); attempt++ {
			time.Sleep(ntExitBusyBackoff(attempt))
			result, err = st.Position().ApplyNT8Exit(receipt)
		}
	}
	if err != nil {
		if !sqlitedriver.IsBusy(err) {
			logger.Warnf("NT8 exit receipt refused/uncommitted account=%s signal=%s qty=%.0f: %v", p.Account, p.SignalID, qty, err)
			return
		}
		// FINAL FAILURE — never drop. The two legacy contracts run first (the
		// broker's real price is parked for reconcile's orphan close and the
		// flat signal is dropped, exactly like legacy recordClose), THEN the
		// receipt is persisted in its own small write so
		// RetryPendingNT8Exits applies it once the lock storm passes.
		putPricedClose(p.Account, symbol, side, p.ExitPrice, qty, exitMs)
		t.mu.Lock()
		t.hasFill = false
		t.mu.Unlock()
		telemetry.IncNT8ExitBusyPark()
		if perr := st.Position().SavePendingExit(receipt); perr != nil {
			telemetry.IncNT8ExitBusyParkPersistFailure()
			logger.Errorf("❌ NT8 exit PARKED after busy retries but the receipt persist FAILED account=%s signal=%s qty=%.0f exit=%.2f: %v — the 2-minute priced park is the only net until the next frame",
				p.Account, p.SignalID, qty, p.ExitPrice, perr)
			return
		}
		logger.Errorf("❌ NT8 exit PARKED after busy retries exhausted account=%s signal=%s qty=%.0f exit=%.2f — priced + receipt persisted (applied=false); RetryPendingNT8Exits will apply it",
			p.Account, p.SignalID, qty, p.ExitPrice)
		return
	}
	if result.Pending {
		// The receipt is RETAINED (the store parked it) — the exit is never
		// dropped; it applies when the cumulative entry update lands. AND the
		// two legacy contracts are kept (F-A, class 40): the broker's price is
		// parked for reconcile's orphan close (a no-row close or a manual
		// flatten like #526's qty=21-over-1-lot would otherwise close at
		// exit=entry pnl=0), and the flat signal is dropped exactly like
		// legacy recordClose.
		putPricedClose(p.Account, symbol, side, p.ExitPrice, qty, exitMs)
		t.mu.Lock()
		t.hasFill = false
		t.mu.Unlock()
		logger.Warnf("NT8 exit receipt PENDING owned entry evidence account=%s signal=%s qty=%.0f exit=%.2f (retained + price parked for reconcile; will apply when the row catches up)",
			p.Account, p.SignalID, qty, p.ExitPrice)
		return
	}
	owner := result.Position
	if OnPositionClosed != nil {
		OnPositionClosed(owner.TraderID, owner.ID)
	}
	if strings.EqualFold(p.ExitReason, "sl") {
		discipline.NoteStopLossExit(owner.TraderID, symbol, side, p.ExitPrice, exitMs)
		logger.Infof("⏳ re-entry cooldown armed: %s %s stop=%.2f (owner=%s)", symbol, side, p.ExitPrice, owner.TraderID)
	}
	if result.Closed {
		logger.Warnf("📕 NT position closed: %s %s qty=%.2f exit=%.2f reason=%s pnl=%.2f (owner=%s row=%d, durable receipt)",
			symbol, side, qty, p.ExitPrice, p.ExitReason, result.RealizedPnL, owner.TraderID, owner.ID)
	} else {
		logger.Infof("📊 NT partial exit recorded: row=%d actual_qty=%.0f residual=%.0f price=%.2f pnl=%.2f (still OPEN)",
			owner.ID, result.Quantity, owner.Quantity, receipt.Price, result.RealizedPnL)
	}
	// A received close for this trader's bound account is the flat signal.
	t.mu.Lock()
	t.hasFill = false
	t.mu.Unlock()
}

// RetryPendingNT8Exits re-applies every parked (pending) exit receipt for this
// trader's bound account. ApplyNT8Exit is idempotent; a receipt whose row still
// is not there stays parked. Called by the worker's close handler and after a
// cumulative entry update lands (the order handler), so an exit that arrived
// BEFORE its entry applies as soon as the entry does — the R6 contract.
func (t *TCPTrader) RetryPendingNT8Exits(st *store.Store) {
	if st == nil || st.Position() == nil {
		return
	}
	receipts, err := st.Position().PendingNT8Exits(t.boundAccount)
	if err != nil {
		logger.Warnf("NT8 pending exit read failed: %v", err)
		return
	}
	for _, receipt := range receipts {
		result, err := st.Position().ApplyNT8Exit(receipt)
		if err != nil {
			logger.Warnf("NT8 pending exit unresolved signal=%s: %v", receipt.SignalID, err)
			continue
		}
		if result.Pending {
			continue
		}
		owner := result.Position
		if OnPositionClosed != nil {
			OnPositionClosed(owner.TraderID, owner.ID)
		}
		if result.Closed {
			logger.Warnf("📕 NT parked exit APPLIED after the entry caught up: %s %s qty=%.0f exit=%.2f pnl=%.2f (owner=%s row=%d)",
				receipt.Symbol, receipt.Side, receipt.Quantity, receipt.Price, result.RealizedPnL, owner.TraderID, owner.ID)
		}
	}
}

// OnPositionClosed (Phase 4, final-bundle 2026-08-19) is the package-level
// close-event hook: the trader layer registers a dispatcher so a confirmed
// position close (close_sync priced close, reconcile priced/flat close)
// triggers exactly one post-exit rescan for the OWNING trader. Nil = no-op.
var OnPositionClosed func(traderID string, positionID int64)

// buildExitFill constructs the deterministic trader_fills row for an NT8 exit
// (4.2). The exchange_trade_id is keyed on the owning position row id — one
// close, one row, no double-count even if the position_close frame retransmits
// (CreateFill dedupes). Side uses the fill convention (close_long = SELL,
// close_short = BUY), matching recordOrderFill. Nil only when the owner row is
// absent (caller already dropped those closes).
func buildExitFill(owner *store.TraderPosition, exchangeID, exchangeType, symbol, side string,
	exitPrice, qty, pnl float64, exitMs int64, signalID string) *store.TraderFill {
	if owner == nil {
		return nil
	}
	fillSide := "BUY"
	if side == "LONG" {
		fillSide = "SELL"
	}
	return &store.TraderFill{
		TraderID:        owner.TraderID,
		ExchangeID:      exchangeID,
		ExchangeType:    exchangeType,
		OrderID:         0,
		ExchangeOrderID: signalID,
		ExchangeTradeID: fmt.Sprintf("nt8-exit-%d", owner.ID),
		Symbol:          market.Normalize(symbol),
		Side:            fillSide,
		Price:           exitPrice,
		Quantity:        qty,
		QuoteQuantity:   exitPrice * qty,
		Commission:      0,
		CommissionAsset: "USD",
		RealizedPnL:     pnl,
		IsMaker:         false,
		CreatedAt:       exitMs,
	}
}

// observerLifetime (W117 F5) returns the shared observer-done channel,
// initialized under mu. The close consumer closes it AFTER draining its queued
// receipts, which retires the reconcile worker — same-account replacement
// drains; a foreign account or a socket disconnect does not.
func (t *TCPTrader) observerLifetime() chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.observerDone == nil {
		t.observerDone = make(chan struct{})
	}
	return t.observerDone
}
