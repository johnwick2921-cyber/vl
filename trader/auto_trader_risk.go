package trader

import (
	"fmt"
	"strings"
	"time"
	"vl/kernel"
	"vl/logger"
	"vl/market"
)

// futuresMaxNotionalLeverage mirrors kernel.futuresMaxNotionalLeverage: the
// notional sanity ceiling for CME futures (max notional = equity × this).
// Kept in lockstep with the risk gate so the executor cap matches what the
// gate already accepted.
const futuresMaxNotionalLeverage = 20.0

// startDrawdownMonitor starts drawdown monitoring
func (at *AutoTrader) startDrawdownMonitor() {
	at.monitorWg.Add(1)
	at.goNetted("drawdown-monitor", func() {
		defer at.monitorWg.Done()

		ticker := time.NewTicker(1 * time.Minute) // Check every minute
		defer ticker.Stop()

		// Capture the per-Run stop signal once (the ctx lives for exactly one
		// Run; a restarted trader creates a fresh one).
		at.stopMonitorMu.Lock()
		stopDone := at.stopMonitorCtx.Done()
		at.stopMonitorMu.Unlock()

		logger.Info("📊 Started position drawdown monitoring (check every minute)")

		for {
			select {
			case <-ticker.C:
				at.runBeatSafely("drawdown monitor", func() { at.monitorTick(time.Now()) })
			case <-stopDone:
				logger.Info("⏹ Stopped position drawdown monitoring")
				return
			}
		}
	})
}

// monitorTick is one wall-clock monitor beat (every minute, independent of the
// decision cycle). The feed watch lives HERE, not in runCycle (in-position
// silence fix 2026-08-19): under bar-close cadence a dead feed stops the
// cycles themselves, and skip-while-open silenced them while holding — so the
// decision cycle could never report the outage that mattered most.
func (at *AutoTrader) monitorTick(now time.Time) {
	at.checkFeedDown(now)
	at.checkPositionDrawdown()
	// TRADE EXCURSION LOGGING (wave 1A, 2026-09-02) — E2. This tick is the
	// ~1m cadence the excursion path is recomputed on while a position lives.
	// The whole path is rebuilt from stored bars each time, so a missed tick,
	// a restart or a late bar costs nothing. Telemetry: it can WARN, never
	// panic, and never blocks the guards above it (class 23).
	at.excursionOnBarTick()
	// D5 (2026-09-07) — IS THE OPEN POSITION ACTUALLY PROTECTED?
	//
	// Deliberately HERE and not in maybeManageArmedOrders, which returns early
	// unless day_plan is enabled. A position is unprotected regardless of
	// day_plan, and a safety check a config flag can silence is not one. This
	// beat is wall-clock and runs while a position is held — which is exactly
	// the window position 592 spent naked with nothing looking.
	at.reconcileProtectionAt(now, "monitor")
	// W-EXEC-TRUTH W0 (f) — withdraw resting ENTRIES while a hold asks for it
	// or a loss limit has tripped. Here, not in the armed pass: that pass does
	// not run in exactly the windows an update or a halt use.
	at.withdrawEntriesIfDue(now)
}

// positionPnLPct computes a position's leveraged P&L percent. Side is normalized:
// NT8 GetPositions/positionMap emits UPPERCASE "LONG"/"SHORT" (upperSideStr), so a
// case-sensitive == "long" ran the SHORT formula for every NT8 long — inverting the
// sign so a winning long produced negative pct and the drawdown-close condition
// (>5% profit) could never fire on a long. Compare lowercase so either casing works.
func positionPnLPct(side string, entryPrice, markPrice float64, leverage int) float64 {
	if strings.ToLower(side) == "long" {
		return ((markPrice - entryPrice) / entryPrice) * float64(leverage) * 100
	}
	return ((entryPrice - markPrice) / entryPrice) * float64(leverage) * 100
}

// checkPositionDrawdown checks position drawdown situation
func (at *AutoTrader) checkPositionDrawdown() {
	if at.trader == nil {
		return // no broker bound (shutdown race / test harness)
	}
	// Get current positions
	positions, err := at.trader.GetPositions()
	if err != nil {
		logger.Infof("❌ Drawdown monitoring: failed to get positions: %v", err)
		return
	}

	openKeys := make(map[string]bool)
	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity // Short position quantity is negative, convert to positive
		}

		// Guard: skip if entry price is zero (prevents division by zero panic)
		if entryPrice <= 0 {
			logger.Warnf("⚠️ Drawdown monitoring: %s %s has zero entry price, skipping", symbol, side)
			continue
		}

		// Auto-breakeven: once far enough in profit, move the stop to entry (once
		// per position; opt-in, default OFF). Runs alongside the drawdown check.
		openKeys[symbol+"_"+side] = true
		at.maybeMoveStopToBreakeven(symbol, side, entryPrice, markPrice)
		// Phase 3B — trailing profit (opt-in, default OFF): additive beat on the
		// same monitor, after breakeven so the BE floor is visible to the rails.
		at.maybeTrailStop(symbol, side, entryPrice, markPrice)

		// Calculate current P&L percentage
		leverage := 10 // Default value
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}

		currentPnLPct := positionPnLPct(side, entryPrice, markPrice, leverage)

		// Construct unique position identifier (distinguish long/short)
		posKey := symbol + "_" + side

		// Get historical peak profit for this position
		at.peakPnLCacheMutex.RLock()
		peakPnLPct, exists := at.peakPnLCache[posKey]
		at.peakPnLCacheMutex.RUnlock()

		if !exists {
			// If no historical peak record, use current P&L as initial value
			peakPnLPct = currentPnLPct
			at.UpdatePeakPnL(symbol, side, currentPnLPct)
		} else {
			// Update peak cache
			at.UpdatePeakPnL(symbol, side, currentPnLPct)
		}

		// Calculate drawdown (magnitude of decline from peak)
		var drawdownPct float64
		if peakPnLPct > 0 && currentPnLPct < peakPnLPct {
			drawdownPct = ((peakPnLPct - currentPnLPct) / peakPnLPct) * 100
		}

		// Check close position condition: profit > 5% and drawdown >= 40%
		if currentPnLPct > 5.0 && drawdownPct >= 40.0 {
			// FIX-EMERGENCY-CLOSE-OFF-IN-MENTOR (owner ruling 2026-10-09):
			// with mentor mode ON, a mentor trade follows ONLY the mentor's
			// exits — the AI-era drawdown emergency close must not close a
			// mentor-owned position. Log ONCE per position + a visible counter.
			if at.mentorEnabled() {
				at.drawdownMentorBlockedOnce(symbol, side, currentPnLPct, peakPnLPct, drawdownPct)
				continue
			}
			logger.Infof("🚨 Drawdown close position condition triggered: %s %s | Current profit: %.2f%% | Peak profit: %.2f%% | Drawdown: %.2f%%",
				symbol, side, currentPnLPct, peakPnLPct, drawdownPct)

			// Execute close position
			if err := at.emergencyClosePosition(symbol, side); err != nil {
				logger.Infof("❌ Drawdown close position failed (%s %s): %v", symbol, side, err)
			} else {
				logger.Infof("✅ Drawdown close position succeeded: %s %s", symbol, side)
				// Clear cache for this position after closing
				at.ClearPeakPnLCache(symbol, side)
			}
		} else if currentPnLPct > 5.0 {
			// Record situations close to close position condition (for debugging)
			logger.Infof("📊 Drawdown monitoring: %s %s | Profit: %.2f%% | Peak: %.2f%% | Drawdown: %.2f%%",
				symbol, side, currentPnLPct, peakPnLPct, drawdownPct)
		}
	}
	// Re-arm breakeven for any position that has since gone flat.
	at.pruneBreakevenDone(openKeys)
	at.pruneTrailStates(openKeys)
}

// emergencyClosePosition emergency close position function
func (at *AutoTrader) emergencyClosePosition(symbol, side string) error {
	// Normalize: NT8 GetPositions emits UPPERCASE side; a lowercase-only switch hit
	// default → "unknown position direction" and the drawdown safety-close could not
	// execute. Compare lowercase (the sole caller passes pos["side"] from GetPositions).
	switch strings.ToLower(side) {
	case "long":
		order, err := at.trader.CloseLong(symbol, 0) // 0 = close all
		if err != nil {
			return err
		}
		logger.Infof("✅ Emergency close long position succeeded, order ID: %v", order["orderId"])
	case "short":
		order, err := at.trader.CloseShort(symbol, 0) // 0 = close all
		if err != nil {
			return err
		}
		logger.Infof("✅ Emergency close short position succeeded, order ID: %v", order["orderId"])
	default:
		return fmt.Errorf("unknown position direction: %s", side)
	}

	return nil
}

// GetPeakPnLCache gets peak profit cache
func (at *AutoTrader) GetPeakPnLCache() map[string]float64 {
	at.peakPnLCacheMutex.RLock()
	defer at.peakPnLCacheMutex.RUnlock()

	// Return a copy of the cache
	cache := make(map[string]float64)
	for k, v := range at.peakPnLCache {
		cache[k] = v
	}
	return cache
}

// UpdatePeakPnL updates peak profit cache
func (at *AutoTrader) UpdatePeakPnL(symbol, side string, currentPnLPct float64) {
	at.peakPnLCacheMutex.Lock()
	defer at.peakPnLCacheMutex.Unlock()

	posKey := symbol + "_" + side
	if peak, exists := at.peakPnLCache[posKey]; exists {
		// Update peak (if long, take larger value; if short, currentPnLPct is negative, also compare)
		if currentPnLPct > peak {
			at.peakPnLCache[posKey] = currentPnLPct
		}
	} else {
		// First time recording
		at.peakPnLCache[posKey] = currentPnLPct
	}
}

// ClearPeakPnLCache clears peak cache for specified position
func (at *AutoTrader) ClearPeakPnLCache(symbol, side string) {
	at.peakPnLCacheMutex.Lock()
	defer at.peakPnLCacheMutex.Unlock()

	posKey := symbol + "_" + side
	delete(at.peakPnLCache, posKey)
}

// ============================================================================
// Risk Control Helpers
// ============================================================================

// enforcePositionValueRatio checks and enforces position value ratio limits (CODE ENFORCED)
// Returns the adjusted position size (capped if necessary) and whether the position was capped
// positionSizeUSD: the original position size in USD
// equity: the account equity
// symbol: the trading symbol
func (at *AutoTrader) enforcePositionValueRatio(positionSizeUSD float64, equity float64, symbol string) (float64, bool) {
	if at.config.StrategyConfig == nil {
		return positionSizeUSD, false
	}

	riskControl := at.config.StrategyConfig.RiskControl

	// Get the appropriate position value ratio limit
	var maxPositionValueRatio float64
	switch {
	case market.IsCMEFuturesSymbol(symbol):
		// CME futures: the notional ceiling multiplier (max notional = equity ×
		// this). Hardening D3 (audit F2): ALWAYS ON — master-independent venue
		// safety. Per-strategy value overrides, else the equity×20 venue default.
		// Never 0; the guard below is belt-and-suspenders.
		maxPositionValueRatio = kernel.ResolveNotionalLeverage(riskControl.MaxNotionalLeverage, futuresMaxNotionalLeverage)
		if maxPositionValueRatio <= 0 {
			maxPositionValueRatio = futuresMaxNotionalLeverage
		}
	default:
		maxPositionValueRatio = riskControl.AltcoinMaxPositionValueRatio
		if maxPositionValueRatio <= 0 {
			maxPositionValueRatio = 1.0 // Default: 1x for altcoins
		}
	}

	// Calculate max allowed position value = equity × ratio
	maxPositionValue := equity * maxPositionValueRatio

	// Check if position size exceeds limit
	if positionSizeUSD > maxPositionValue {
		logger.Infof("  ⚠️ [RISK CONTROL] Position %.2f USD exceeds limit (equity %.2f × %.1fx = %.2f USD max for %s), capping",
			positionSizeUSD, equity, maxPositionValueRatio, maxPositionValue, symbol)
		return maxPositionValue, true
	}

	return positionSizeUSD, false
}

// enforceMinPositionSize checks minimum position size (CODE ENFORCED)
func (at *AutoTrader) enforceMinPositionSize(positionSizeUSD float64) error {
	if at.config.StrategyConfig == nil {
		return nil
	}

	minSize := at.config.StrategyConfig.RiskControl.MinPositionSize
	if minSize <= 0 {
		minSize = 12 // Default: 12 USD
	}

	if positionSizeUSD < minSize {
		return fmt.Errorf("❌ [RISK CONTROL] Position %.2f USD below minimum (%.2f USD)", positionSizeUSD, minSize)
	}
	return nil
}

// enforceMaxPositions checks maximum positions count (CODE ENFORCED)
func (at *AutoTrader) enforceMaxPositions(currentPositionCount int) error {
	if at.config.StrategyConfig == nil {
		return nil
	}

	maxPositions := at.config.StrategyConfig.RiskControl.MaxPositions
	if maxPositions <= 0 {
		maxPositions = 3 // Default: 3 positions
	}

	if currentPositionCount >= maxPositions {
		return fmt.Errorf("❌ [RISK CONTROL] Already at max positions (%d/%d)", currentPositionCount, maxPositions)
	}
	return nil
}

// getSideFromAction converts order action to side (BUY/SELL)
func getSideFromAction(action string) string {
	switch action {
	case "open_long", "close_short":
		return "BUY"
	case "open_short", "close_long":
		return "SELL"
	default:
		return "BUY"
	}
}
