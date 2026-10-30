package trader

import (
	"fmt"
	"strings"
	"time"
	"vl/discipline"
	"vl/kernel"
	"vl/logger"
	"vl/market"
	"vl/store"
	"vl/telemetry"
)

// saveEquitySnapshot saves equity snapshot independently (for drawing profit curve, decoupled from AI decision)
func (at *AutoTrader) saveEquitySnapshot(ctx *kernel.Context) {
	if at.store == nil || ctx == nil {
		return
	}

	snapshot := &store.EquitySnapshot{
		TraderID:      at.id,
		Account:       at.currentAccountName(), // ITEM 2 per-account attribution
		Timestamp:     time.Now().UTC(),
		TotalEquity:   ctx.Account.TotalEquity,
		Balance:       ctx.Account.TotalEquity - ctx.Account.UnrealizedPnL,
		UnrealizedPnL: ctx.Account.UnrealizedPnL,
		PositionCount: ctx.Account.PositionCount,
		MarginUsedPct: ctx.Account.MarginUsedPct,
	}

	if err := at.store.Equity().Save(snapshot); err != nil {
		logger.Infof("⚠️ Failed to save equity snapshot: %v", err)
	}
}

// saveDecision saves AI decision log to database (only records AI input/output, for debugging)
func (at *AutoTrader) saveDecision(record *store.DecisionRecord) error {
	if at.store == nil {
		return nil
	}

	// PEP MODE: the pep entry saves its decision row from the event-loop goroutine
	// while the cycle saves its own; the counter is bumped under cycleMu.
	at.cycleMu.Lock()
	at.cycleNumber++
	cycleNo := at.cycleNumber
	at.cycleMu.Unlock()
	record.CycleNumber = cycleNo
	record.TraderID = at.id
	record.Account = at.currentAccountName() // ITEM 2 per-account attribution

	// W5.3 (weekly-bias wave) — draw-alignment tag on EVERY decision row:
	// toward_draw | away | neutral. Stored on decision_records.draw_align
	// (additive AutoMigrate column).
	record.DrawAlign = at.weeklyDrawAlignTag(record)

	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}

	if err := at.store.Decision().LogDecision(record); err != nil {
		// P1-D (audit 2026-09-26): a lost decision row is never INFO-only — WARN
		// auto-promotes to the WARN+ DB sink (journald prunes INFO at the 2G cap),
		// and RecordError makes it queryable on /api/risk/errors. Every discard
		// site rides this path, so none can drop the failure silently.
		telemetry.RecordError(at.id, "decision_save_failed", err.Error(), telemetry.CostDecisionLost)
		logger.Warnf("[trader_id=%s] ⚠️ Failed to save decision record (account=%s plan_id=%s plan_version=%d op=save_decision): %v",
			record.TraderID, record.Account, record.PlanID, record.PlanVersion, err)
		return err
	}

	logger.Infof("📝 Decision record saved: trader=%s, cycle=%d", at.id, cycleNo)
	return nil
}

// GetStatus gets system status (for API)
// aiCallCount reads the AI cycle counter under statusMu, for readers that
// run off the decision loop's goroutine (the watcher, GetStatus).
func (at *AutoTrader) aiCallCount() int {
	at.statusMu.Lock()
	defer at.statusMu.Unlock()
	return at.callCount
}

func (at *AutoTrader) GetStatus() map[string]interface{} {
	aiProvider := "DeepSeek"
	if at.config.UseQwen {
		aiProvider = "Qwen"
	}

	at.isRunningMutex.RLock()
	isRunning := at.isRunning
	at.isRunningMutex.RUnlock()
	at.statusMu.Lock()
	statusCallCount, statusLastReset := at.callCount, at.lastResetTime
	at.statusMu.Unlock()

	result := map[string]interface{}{
		"trader_id":       at.id,
		"trader_name":     at.name,
		"ai_model":        at.aiModel,
		"exchange":        at.exchange,
		"is_running":      isRunning,
		"start_time":      at.startTime.Format(time.RFC3339),
		"runtime_minutes": int(time.Since(at.startTime).Minutes()),
		"call_count":      statusCallCount,
		"initial_balance": at.initialBalance,
		"scan_interval":   at.config.ScanInterval.String(),
		// P2 (ledger-close 2026-08-19): stop_until now reports the REAL owner
		// pause (auto_trader_pause.go). The legacy at.stopUntil field is dormant
		// (never assigned) so the unpaused rendering is byte-identical.
		"stop_until":      pauseStatusString(at),
		"last_reset_time": statusLastReset.Format(time.RFC3339),
		"ai_provider":     aiProvider,
		// v1 audit §5.6 (E5): the running binary's revision, so a bug report
		// can be checked against what is actually deployed without a shell.
		"revision": kernel.RunningRevision(),
	}

	// P3 (ledger-close 2026-08-19) — roll picture for the dashboard (3.6):
	// resolved contract, expiry, window start, days remaining.
	if at.exchange == "ninjatrader" {
		result["roll"] = at.RollStatus(time.Now())
	}

	// Add strategy info
	if at.config.StrategyConfig != nil {
		result["strategy_type"] = at.config.StrategyConfig.StrategyType
		if at.config.StrategyConfig.GridConfig != nil {
			result["grid_symbol"] = at.config.StrategyConfig.GridConfig.Symbol
		}
	}

	return result
}

// GetAccountInfo gets account information (for API)
func (at *AutoTrader) GetAccountInfo() (map[string]interface{}, error) {
	balance, err := at.trader.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	// Get account fields
	totalWalletBalance := 0.0
	totalUnrealizedProfit := 0.0
	availableBalance := 0.0
	totalEquity := 0.0

	if wallet, ok := balance["totalWalletBalance"].(float64); ok {
		totalWalletBalance = wallet
	}
	if unrealized, ok := balance["totalUnrealizedProfit"].(float64); ok {
		totalUnrealizedProfit = unrealized
	}
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Use totalEquity directly if provided by trader (more accurate)
	if eq, ok := balance["totalEquity"].(float64); ok && eq > 0 {
		totalEquity = eq
	} else {
		// Fallback: Total Equity = Wallet balance + Unrealized profit
		totalEquity = totalWalletBalance + totalUnrealizedProfit
	}

	// Get positions to calculate total margin
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	totalMarginUsed := 0.0
	for _, pos := range positions {
		// Comma-ok asserts: NT futures positions may omit some Binance-style
		// keys; a hard assert would panic /api/account (Plan 4.11 fix).
		markPrice, _ := pos["markPrice"].(float64)
		quantity, _ := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity
		}

		leverage := 10
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}
		marginUsed := (quantity * markPrice) / float64(leverage)
		totalMarginUsed += marginUsed
	}

	totalPnL := totalEquity - at.initialBalance
	// R22 PNL-TRUTH (RISK-PNL P1-2): every account figure under its OWN name.
	// equity − initial is the account's P&L since the configured start; it is
	// unknown (absent) without an initial balance.
	accountVsInitial := totalPnL
	totalPnLSource := "equity_minus_initial"
	var nt8Session *float64
	totalPnLPct := 0.0
	if at.initialBalance > 0 {
		totalPnLPct = (totalPnL / at.initialBalance) * 100
	} else {
		logger.Infof("⚠️ Initial Balance abnormal: %.2f, cannot calculate P&L percentage", at.initialBalance)
	}

	// Issue 2B — NT8 multi-account: the bot's single global InitialBalance is NOT
	// each NT sub-account's real starting equity, so (equity - InitialBalance)
	// yields a fake P&L% (e.g. -30% on a 70000 SIM account vs a 100000 baseline).
	// When the broker reports its own realized+unrealized P&L (NinjaTrader
	// account_balance frame, flagged brokerNativePnL), use that as the displayed
	// per-account P&L and derive the baseline as equity - pnl (this account's
	// effective starting equity), so each account shows its OWN real P&L.
	if broker, _ := balance["brokerNativePnL"].(bool); broker {
		realized, _ := balance["totalRealizedProfit"].(float64)
		unrealized, _ := balance["totalUnrealizedProfit"].(float64)
		totalPnL = realized + unrealized
		// NT8 RESETS RealizedProfitLoss every session: this is the NT8
		// SESSION figure (10-14: +74.00 = #648 + #651), not a total.
		sessionPnL := totalPnL
		nt8Session = &sessionPnL
		totalPnLSource = "nt8_session"
		if base := totalEquity - totalPnL; base > 0 {
			totalPnLPct = (totalPnL / base) * 100
		} else {
			totalPnLPct = 0
		}
	}

	marginUsedPct := 0.0
	if totalEquity > 0 {
		marginUsedPct = (totalMarginUsed / totalEquity) * 100
	}

	out := map[string]interface{}{
		// Core fields
		"total_equity":      totalEquity,           // Account equity = wallet + unrealized
		"wallet_balance":    totalWalletBalance,    // Wallet balance (excluding unrealized P&L)
		"unrealized_profit": totalUnrealizedProfit, // Unrealized P&L (official value from exchange API)
		"available_balance": availableBalance,      // Available balance

		// P&L statistics. N6 (release #26): total_pnl is NOT a uniform "total" —
		// on NT8 it is the broker's SESSION figure (realized+unrealized since the
		// session reset), on crypto it is equity − initial. The truth is carried
		// beside it in total_pnl_source; readers must consult it.
		"total_pnl":       totalPnL,          // NT8: session realized+unrealized; crypto: equity − initial (see total_pnl_source)
		"total_pnl_pct":   totalPnLPct,       // Total P&L percentage
		"initial_balance": at.initialBalance, // Initial balance
		"daily_pnl":       at.dailyPnL,       // Daily P&L

		// Position information
		"position_count":  len(positions),  // Position count
		"margin_used":     totalMarginUsed, // Margin used
		"margin_used_pct": marginUsedPct,   // Margin usage rate

		// R22: what total_pnl HOLDS — "nt8_session" (NT8's session-reset
		// realized+unrealized) or "equity_minus_initial".
		"total_pnl_source": totalPnLSource,
	}
	if nt8Session != nil {
		out["nt8_session_pnl"] = *nt8Session // ACCOUNT figure, as NT8 reports it
	}
	if at.initialBalance > 0 {
		out["account_pnl_vs_initial"] = accountVsInitial // ACCOUNT figure: equity − initial_balance
		out["account_pnl_vs_initial_pct"] = accountVsInitial / at.initialBalance * 100
	}
	return out, nil
}

// GetPositions gets position list (for API)
func (at *AutoTrader) GetPositions() ([]map[string]interface{}, error) {
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	var result []map[string]interface{}
	// Non-nil empty slice: the /api/positions route documents an ARRAY ([]), and
	// some clients (and the dashboard's defensive .map paths) mishandle a JSON
	// null when flat. Encode the flat state as [] — never null.
	if len(positions) == 0 {
		return []map[string]interface{}{}, nil
	}
	for _, pos := range positions {
		// Comma-ok asserts: NT futures positions may omit some Binance-style
		// keys (e.g. liquidationPrice); a hard assert would panic the API
		// positions repackaging (Plan 4.11 fix).
		symbol, _ := pos["symbol"].(string)
		side, _ := pos["side"].(string)
		entryPrice, _ := pos["entryPrice"].(float64)
		markPrice, _ := pos["markPrice"].(float64)
		quantity, _ := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity
		}
		unrealizedPnl, _ := pos["unRealizedProfit"].(float64)
		liquidationPrice, _ := pos["liquidationPrice"].(float64)

		leverage := 10
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}

		// Calculate margin used
		marginUsed := (quantity * markPrice) / float64(leverage)

		// Calculate P&L percentage (based on margin)
		pnlPct := calculatePnLPercentage(unrealizedPnl, marginUsed)

		row := map[string]interface{}{
			"symbol":             symbol,
			"side":               side,
			"entry_price":        entryPrice,
			"mark_price":         markPrice,
			"quantity":           quantity,
			"leverage":           leverage,
			"unrealized_pnl":     unrealizedPnl,
			"unrealized_pnl_pct": pnlPct,
			"liquidation_price":  liquidationPrice,
			"margin_used":        marginUsed,
		}
		// m421a C1 — pass the broker's UI-only keys through when PRESENT (the
		// fixed key list above used to drop them, so the chart never drew the
		// open position's SL/TP lines): the working bracket, the contract point
		// value and the notional value. Absent upstream stays absent.
		for _, k := range []string{"bracket", "point_value", "position_value"} {
			if v, ok := pos[k]; ok && v != nil {
				row[k] = v
			}
		}
		// entry_time (ms) is the position's REAL open time from the trader_positions
		// row; absent when the store has no OPEN row (never fabricated from
		// "first seen", which a restart resets).
		if at.store != nil {
			if dbPos, derr := at.store.Position().GetOpenPositionBySymbol(at.id, symbol, side); derr == nil && dbPos != nil && dbPos.EntryTime > 0 {
				row["entry_time"] = dbPos.EntryTime
			}
		}
		result = append(result, row)
	}

	return result, nil
}

// recordAndConfirmOrder polls order status for actual fill data and records position
// action: open_long, open_short, close_long, close_short
// entryPrice: entry price when closing (0 when opening)
func (at *AutoTrader) recordAndConfirmOrder(orderResult map[string]interface{}, symbol, action string, quantity float64, price float64, leverage int, entryPrice float64, confidence int) {
	at.recordAndConfirmOrderAs(orderResult, symbol, action, quantity, price, leverage, entryPrice, confidence, nil)
}

// chatOpenBracket is an agent-chat open's OWN authored stop and target (W1b
// FOLD-2 repair), known at the door and carried to its position's excursion
// row. nil = an AI open, whose levels are resolved later from its decision.
type chatOpenBracket struct{ stop, target float64 }

// recordAndConfirmOrderAs is recordAndConfirmOrder for an open that may be the
// agent-chat door's (W1b FOLD-2): manual != nil records and confirms it
// exactly as an AI open, except its position never takes the AI decision's
// pending plan citation and its excursion row carries the chat's own bracket
// (recordPositionChangeAs).
func (at *AutoTrader) recordAndConfirmOrderAs(orderResult map[string]interface{}, symbol, action string, quantity float64, price float64, leverage int, entryPrice float64, confidence int, manual *chatOpenBracket) {
	if at.store == nil {
		return
	}

	// Get order ID (supports multiple types)
	var orderID string
	switch v := orderResult["orderId"].(type) {
	case int64:
		orderID = fmt.Sprintf("%d", v)
	case float64:
		orderID = fmt.Sprintf("%.0f", v)
	case string:
		orderID = v
	default:
		orderID = fmt.Sprintf("%v", v)
	}
	// W-EXEC-TRUTH W0 (d) — CLASS 160. An NT8 entry returns no "orderId"; it
	// returns the SIGNAL id it was sent under, and that is the only identity a
	// fill or a reject can be matched to. Keyed by it, the order row no longer
	// collapses onto one "<nil>" row, and the confirmation below can ask about
	// THIS entry instead of whatever entry last wrote the shared slot.
	ntSignalKeyed := at.exchange == "ninjatrader" && (action == "open_long" || action == "open_short")
	if ntSignalKeyed {
		sid, _ := orderResult["signal_id"].(string)
		orderID = strings.TrimSpace(sid)
	}

	if orderID == "" || orderID == "0" {
		logger.Infof("  ⚠️ Order ID is empty, skipping record")
		return
	}

	// Determine positionSide
	var positionSide string
	switch action {
	case "open_long", "close_long":
		positionSide = "LONG"
	case "open_short", "close_short":
		positionSide = "SHORT"
	}

	var actualPrice = price
	var actualQty = quantity
	var fee float64

	// NinjaTrader has no order-sync; an NT8 position transitions to CLOSED ONLY via
	// the position_close fill frame (trader/ninjatrader close_sync.recordClose —
	// fill-confirmed and futures-point-value-correct), exactly as SL/TP exits do.
	// A decision-driven close must NOT mark the DB CLOSED off the mark price before
	// the NT8 exit actually fills: a rejected flatten (e.g. data feed down → SIM
	// "no market data") would phantom-close a position NT8 still holds, and the next
	// entry would net onto the orphan (the id=45→id=46 net-2 bug). So defer NT8
	// closes to the fill frame. OPENS still record below (they create the position
	// row); only closes defer. Crypto closes are handled by their own OrderSync above.
	if at.exchange == "ninjatrader" && (action == "close_long" || action == "close_short") {
		logger.Infof("  📝 NT close submitted (%s %s) — awaiting NT8 position_close fill confirmation (close-sync; not marking CLOSED off the mark)", symbol, action)
		return
	}

	orderRecord := at.createOrderRecord(orderID, symbol, action, positionSide, quantity, price, leverage)
	if err := at.store.Order().CreateOrder(orderRecord); err != nil {
		logger.Infof("  ⚠️ Failed to record order: %v", err)
	} else {
		logger.Infof("  📝 Order recorded: %s [%s] %s", orderID, action, symbol)
	}

	// W-EXEC-TRUTH W0 (d) — CLASS 160: an NT8 open becomes a position ONLY on a
	// received fill for ITS OWN signal. A reject for that signal settles the
	// order row REJECTED; no evidence within the poll leaves the order row
	// unresolved (NEW) and records NOTHING — never a position at the mark. A
	// fill that lands later is materialized by the position reconciler from
	// NT8's own position (the untracked path), not fabricated here.
	if ntSignalKeyed {
		ev, ok := at.trader.(ntSignalEvidence)
		if !ok {
			logger.Warnf("  ⚠️ %s %s submitted (signal %s) — this broker exposes no per-signal fill evidence; recorded as SUBMITTED, UNCONFIRMED (no position)", action, symbol, orderID)
			return
		}
		filled := false
		time.Sleep(500 * time.Millisecond)
		for i := 0; i < 5 && !filled; i++ {
			if reason, rejected := ev.RecentRejectFor(orderID); rejected {
				if strings.TrimSpace(reason) == "" {
					reason = store.PlacementReasonUnavailable
				}
				if err := at.store.Order().UpdateOrderStatus(orderRecord.ID, "REJECTED", 0, 0, 0); err != nil {
					logger.Infof("  ⚠️ Failed to update order status: %v", err)
				}
				logger.Warnf("  ⛔ %s %s REJECTED by NT8 (signal %s, reason %q) — no position recorded", action, symbol, orderID, reason)
				return
			}
			if px, qty, got := ev.RecentFillFor(orderID); got {
				actualPrice = px
				if qty > 0 {
					actualQty = qty
				}
				filled = true
				logger.Infof("  ✅ Order filled (signal %s): avgPrice=%.6f, qty=%.6f", orderID, actualPrice, actualQty)
				if err := at.store.Order().UpdateOrderStatus(orderRecord.ID, "FILLED", actualQty, actualPrice, fee); err != nil {
					logger.Infof("  ⚠️ Failed to update order status: %v", err)
				}
				at.recordOrderFill(orderRecord.ID, orderID, symbol, action, actualPrice, actualQty, fee)
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if !filled {
			logger.Warnf("  ⚠️ %s %s SUBMITTED, UNCONFIRMED — no fill for signal %s within the confirmation window; the order row stays unresolved and NO position is recorded (a later fill is materialized from NT8's position by the reconciler)", action, symbol, orderID)
			return
		}
	}

	// Wait for order to be filled and get actual fill data
	if !ntSignalKeyed {
		time.Sleep(500 * time.Millisecond)
	}
	for i := 0; i < 5 && !ntSignalKeyed; i++ {
		status, err := at.trader.GetOrderStatus(symbol, orderID)
		if err == nil {
			statusStr, _ := status["status"].(string)
			if statusStr == "FILLED" {
				// Get actual fill price
				if avgPrice, ok := status["avgPrice"].(float64); ok && avgPrice > 0 {
					actualPrice = avgPrice
				}
				// Get actual executed quantity
				if execQty, ok := status["executedQty"].(float64); ok && execQty > 0 {
					actualQty = execQty
				}
				// Get commission/fee
				if commission, ok := status["commission"].(float64); ok {
					fee = commission
				}
				logger.Infof("  ✅ Order filled: avgPrice=%.6f, qty=%.6f, fee=%.6f", actualPrice, actualQty, fee)

				// Update order status to FILLED
				if err := at.store.Order().UpdateOrderStatus(orderRecord.ID, "FILLED", actualQty, actualPrice, fee); err != nil {
					logger.Infof("  ⚠️ Failed to update order status: %v", err)
				}

				// Record fill details
				at.recordOrderFill(orderRecord.ID, orderID, symbol, action, actualPrice, actualQty, fee)
				break
			} else if statusStr == "CANCELED" || statusStr == "EXPIRED" || statusStr == "REJECTED" {
				logger.Infof("  ⚠️ Order %s, skipping position record", statusStr)
				// Update order status
				if err := at.store.Order().UpdateOrderStatus(orderRecord.ID, statusStr, 0, 0, 0); err != nil {
					logger.Infof("  ⚠️ Failed to update order status: %v", err)
				}
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Normalize symbol for position record consistency
	normalizedSymbolForPosition := market.Normalize(symbol)

	logger.Infof("  📝 Recording position (ID: %s, action: %s, price: %.6f, qty: %.6f, fee: %.4f)",
		orderID, action, actualPrice, actualQty, fee)

	// Record position change with actual fill data (use normalized symbol)
	at.recordPositionChangeAs(orderID, normalizedSymbolForPosition, positionSide, action, actualQty, actualPrice, leverage, entryPrice, fee, confidence, manual)

	// Send anonymous trade statistics for experience improvement (async, non-blocking)
	// This helps us understand overall product usage across all deployments
	telemetry.TrackTrade(telemetry.TradeEvent{
		Exchange:  at.exchange,
		TradeType: action,
		Symbol:    symbol,
		AmountUSD: actualPrice * actualQty,
		Leverage:  leverage,
		UserID:    at.userID,
		TraderID:  at.id,
	})
}

// recordPositionChange records position change (create record on open, update record on close)
func (at *AutoTrader) recordPositionChange(orderID, symbol, side, action string, quantity, price float64, leverage int, entryPrice float64, fee float64, confidence int) {
	at.recordPositionChangeAs(orderID, symbol, side, action, quantity, price, leverage, entryPrice, fee, confidence, nil)
}

// recordPositionChangeAs is recordPositionChange; manual != nil = an
// agent-chat open (W1b FOLD-2), whose position never consumes lastCitation —
// the citation is the AI decision's, written and read on the cycle goroutine —
// and whose excursion row opens with the chat's OWN stop and target, never
// resolved later from a nearby AI decision (FOLD-2 repair, L7).
func (at *AutoTrader) recordPositionChangeAs(orderID, symbol, side, action string, quantity, price float64, leverage int, entryPrice float64, fee float64, confidence int, manual *chatOpenBracket) {
	if at.store == nil {
		return
	}

	switch action {
	case "open_long", "open_short":
		// Open position: create new position record
		nowMs := time.Now().UTC().UnixMilli()
		pos := &store.TraderPosition{
			TraderID:        at.id,
			Account:         at.currentAccountName(), // ITEM 2 per-account attribution
			ExchangeID:      at.exchangeID,           // Exchange account UUID
			ExchangeType:    at.exchange,
			Symbol:          symbol,
			Side:            side, // LONG or SHORT
			Quantity:        quantity,
			EntryPrice:      price,
			EntryOrderID:    orderID,
			EntryTime:       nowMs,
			Leverage:        leverage,
			Status:          "OPEN",
			EntryConfidence: &confidence, // P2.4 — the AI's entry confidence (R22: nil on paths without one)
			CreatedAt:       nowMs,
			UpdatedAt:       nowMs,
		}
		if err := at.store.Position().Create(pos); err != nil {
			// W16/R5 — THE WORST CASE, previously the quietest.
			//
			// The order already FILLED at the broker. Failing to write the row means
			// a REAL live position exists that this process does not know about:
			// it is absent from the position list, the reconcile's belief side, the
			// daily-loss and trade-count accounting, EOD-flat's work list (which
			// sources from GetOpenPositions), and MAE/MFE + adherence. Every
			// downstream number is now wrong and the position will not be flattened
			// by us. This used to log at INFO and continue — and the P0 fill alert
			// lived in the else-branch, so the one failure mode that most needs an
			// alert emitted none.
			//
			// Now: loud log + a P0 the owner sees + FREEZE the trader. The freeze is
			// the same A4 mechanism the reconcile divergence uses; it blocks NEW
			// entries only (closes / stop-moves / reconcile are never blocked, see
			// auto_trader_orders.go:163-174), so the owner can still be brought flat.
			// It persists until explicitly cleared via /api/risk/clear-freeze, which
			// is the "flag the UI shows until resolved".
			reason := fmt.Sprintf("untracked live position: %s %s @ %.2f filled but the DB write failed (%v)", side, symbol, price, err)
			logger.Errorf("🚨 UNTRACKED LIVE POSITION — %s. This process cannot see, size, close or account for it. Trader FROZEN for new entries; reconcile from NT8 and clear via /api/risk/clear-freeze.", reason)
			discipline.FreezeTrader(at.id, reason, nowMs)
			at.emitAlert("P0", "untracked-position",
				fmt.Sprintf("untracked:%s:%s:%d", symbol, side, nowMs),
				fmt.Sprintf("UNTRACKED %s %s @ %.2f — fill recorded at the broker, not in the DB", side, symbol, price),
				"trader frozen for new entries · reconcile from NT8, then clear the freeze")
		} else {
			// WARN (honest-logs 2026-08-19): a confirmed open is owner-visible
			// truth — survives journald flood via the log_events sink.
			logger.Warnf("📗 Position OPENED [%s] %s %s qty=%.2f @ %.4f", at.id[:8], symbol, side, quantity, price)
			// P5.5 — stamp the plan link captured in recordPlanCitation onto this
			// open (day_plan-gated → dormant for crypto). Consumed once.
			// S3 — full link: plan_id/date/session from the ACTIVE plan at
			// decision time (never reconstructed later).
			if manual == nil && at.dayPlanEnabled() && at.lastCitation.valid {
				_ = at.store.Position().SetPlanLinkFull(pos.ID, at.lastCitation.planVersion, at.lastCitation.scenarioID, at.lastCitation.matched, at.lastCitation.band, at.lastCitation.planID, at.lastCitation.tradeDate, at.lastCitation.session)
				at.lastCitation.valid = false
			}
			// E1 (wave 1A, 2026-09-02) — the excursion row's entry half. The AI
			// execution path does not carry the stop and target down to the
			// position row, so they are opened UNKNOWN (NULL) and resolved on
			// the next tick from the decision that opened this fill. Nothing
			// here is guessed and nothing here can block the fill.
			//
			// W1b FOLD-2 repair (L7) — an agent-chat open is NOT that path: its
			// own stop and target are known here, and a row opened UNKNOWN
			// would be resolved on the next tick from whatever AI decision sits
			// nearest the fill (StopTargetNear) — another trade's bracket. The
			// door refuses an entry without both levels; if one were ever
			// missing, no row is opened rather than one that would inherit.
			switch {
			case manual == nil:
				at.excursionOnOpen(pos, 0, 0, plannerATR5m(at.futuresSymbol()))
			case manual.stop > 0 && manual.target > 0:
				at.excursionOnOpen(pos, manual.stop, manual.target, plannerATR5m(at.futuresSymbol()))
			default:
				at.logWarnf("📐 excursion NOT opened for chat position pos=%d %s %s: its own stop/target unknown (stop=%.2f target=%.2f) — never resolved from an AI decision", pos.ID, symbol, side, manual.stop, manual.target)
			}
			// W6 — fill alert (P1 informational: m420b/B2).
			at.emitAlert("P1", "fill", fmt.Sprintf("fill:%d", pos.ID),
				fmt.Sprintf("Filled %s %s @ %.2f", side, symbol, price), "")
		}

	case "close_long", "close_short":
		// Close position using PositionBuilder for consistent handling
		// PositionBuilder will handle both cases:
		// 1. If open position exists: close it properly
		// 2. If no open position (e.g., table cleared): create a closed position record
		posBuilder := store.NewPositionBuilder(at.store.Position())
		if err := posBuilder.ProcessTrade(
			at.id, at.exchangeID, at.exchange,
			symbol, side, action,
			quantity, price, fee, 0, // realizedPnL will be calculated
			time.Now().UTC().UnixMilli(), orderID,
		); err != nil {
			logger.Infof("  ⚠️ Failed to process close position: %v", err)
		} else {
			logger.Infof("  ✅ Position closed [%s] %s %s @ %.4f", at.id[:8], symbol, side, price)
			// P2.4 — compute + store MAE/MFE over the hold (gated futures + day_plan).
			at.recordExcursionForClosedSymbol(market.Normalize(symbol))
		}
	}
}

// createOrderRecord creates an order record struct from order details
func (at *AutoTrader) createOrderRecord(orderID, symbol, action, positionSide string, quantity, price float64, leverage int) *store.TraderOrder {
	// Determine order type (market for auto trader)
	orderType := "MARKET"

	// Determine side (BUY/SELL)
	var side string
	switch action {
	case "open_long", "close_short":
		side = "BUY"
	case "open_short", "close_long":
		side = "SELL"
	}

	// Use action as orderAction directly (keep lowercase format)
	orderAction := action

	// Determine if it's a reduce only order
	reduceOnly := (action == "close_long" || action == "close_short")

	// Normalize symbol for consistency
	normalizedSymbol := market.Normalize(symbol)

	return &store.TraderOrder{
		TraderID:        at.id,
		ExchangeID:      at.exchangeID,
		ExchangeType:    at.exchange,
		ExchangeOrderID: orderID,
		Symbol:          normalizedSymbol,
		Side:            side,
		PositionSide:    positionSide,
		Type:            orderType,
		TimeInForce:     "GTC",
		Quantity:        quantity,
		Price:           price,
		Status:          "NEW",
		FilledQuantity:  0,
		AvgFillPrice:    0,
		Commission:      0,
		CommissionAsset: "USDT",
		Leverage:        leverage,
		ReduceOnly:      reduceOnly,
		ClosePosition:   reduceOnly,
		OrderAction:     orderAction,
		CreatedAt:       time.Now().UTC().UnixMilli(),
		UpdatedAt:       time.Now().UTC().UnixMilli(),
	}
}

// recordOrderFill records order fill/trade details
func (at *AutoTrader) recordOrderFill(orderRecordID int64, exchangeOrderID, symbol, action string, price, quantity, fee float64) {
	if at.store == nil {
		return
	}

	// Determine side (BUY/SELL)
	var side string
	switch action {
	case "open_long", "close_short":
		side = "BUY"
	case "open_short", "close_long":
		side = "SELL"
	}

	// Generate a simple trade ID (exchange doesn't always provide one)
	tradeID := fmt.Sprintf("%s-%d", exchangeOrderID, time.Now().UnixNano())

	// Normalize symbol for consistency
	normalizedSymbol := market.Normalize(symbol)

	fill := &store.TraderFill{
		TraderID:        at.id,
		ExchangeID:      at.exchangeID,
		ExchangeType:    at.exchange,
		OrderID:         orderRecordID,
		ExchangeOrderID: exchangeOrderID,
		ExchangeTradeID: tradeID,
		Symbol:          normalizedSymbol,
		Side:            side,
		Price:           price,
		Quantity:        quantity,
		QuoteQuantity:   price * quantity,
		Commission:      fee,
		CommissionAsset: "USDT",
		RealizedPnL:     0,     // Will be calculated for close orders
		IsMaker:         false, // Market orders are usually taker
		CreatedAt:       time.Now().UTC().UnixMilli(),
	}

	// Calculate realized PnL for close orders
	if action == "close_long" || action == "close_short" {
		// Try to get the entry price from the open position
		var positionSide string
		if action == "close_long" {
			positionSide = "LONG"
		} else {
			positionSide = "SHORT"
		}

		if openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, symbol, positionSide); err == nil && openPos != nil {
			if positionSide == "LONG" {
				fill.RealizedPnL = (price - openPos.EntryPrice) * quantity
			} else {
				fill.RealizedPnL = (openPos.EntryPrice - price) * quantity
			}
		}
	}

	if err := at.store.Order().CreateFill(fill); err != nil {
		logger.Infof("  ⚠️ Failed to record fill: %v", err)
	} else {
		logger.Infof("  📋 Fill recorded: %.4f @ %.6f, fee: %.4f", quantity, price, fee)
	}
}

// GetOpenOrders returns open orders (pending SL/TP) from exchange
func (at *AutoTrader) GetOpenOrders(symbol string) ([]OpenOrder, error) {
	return at.trader.GetOpenOrders(symbol)
}

// ntSignalEvidence is the per-signal entry evidence an NT8 broker exposes
// (W-EXEC-TRUTH W0 (d), CLASS 160): a received fill or a received reject for
// the exact signal an entry was sent under.
type ntSignalEvidence interface {
	RecentFillFor(signalID string) (price, quantity float64, ok bool)
	RecentRejectFor(signalID string) (reason string, ok bool)
}
