package trader

import (
	"strings"

	"fmt"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// Phase 3B — TRAILING PROFIT (final-bundle 2026-08-19). Owner-requested Risk
// Control function: mechanical, deterministic, ZERO AI. Ships DEFAULT OFF; the
// owner enables it in Studio (ai_config.risk_control.trailing_*).
//
// Engine (runs on the 60s wall-clock monitor, additive beat beside breakeven):
//   arm per trailing_arm (after_breakeven default | after_trigger_points |
//   immediate) → while armed, trail = best_price_since_entry − mult×ATR (long)
//   / + mult×ATR (short) with ATR(period, 5m) computed live → the stop RATCHETS
//   through the proven #52 move_stop path.
// Rails: R-A ratchet-only (never widens, never moves backward — the wire layer's
// B1 stop-widen ban backstops this), R-B never below entry once breakeven fired.
// Idempotent: emits only when strictly better than the last emitted level.

const (
	defaultTrailingATRMult   = 2.0
	defaultTrailingATRPeriod = 14
	trailATRTimeframe        = "5m"

	TrailArmAfterBreakeven = "after_breakeven"
	TrailArmAfterPoints    = "after_trigger_points"
	TrailArmImmediate      = "immediate"
)

type trailState struct {
	Armed       bool
	BestPrice   float64 // best price since entry (max for LONG, min for SHORT)
	LastEmitted float64 // last stop level actually sent (0 = none yet)
}

// trailingConfig resolves the strategy's trailing settings with spec defaults.
func trailingConfig(rc store.RiskControlConfig) (enabled bool, mult float64, period int, arm string, armPts float64) {
	enabled = hlBool(rc.TrailingEnabled, false)
	mult = rc.TrailingATRMult
	if mult <= 0 {
		mult = defaultTrailingATRMult
	}
	period = rc.TrailingATRPeriod
	if period <= 0 {
		period = defaultTrailingATRPeriod
	}
	arm = rc.TrailingArm
	switch arm {
	case TrailArmAfterPoints, TrailArmImmediate:
	default:
		arm = TrailArmAfterBreakeven
	}
	armPts = rc.TrailingArmPoints
	return
}

// trailArmed is the pure arming decision.
func trailArmed(arm string, beFired bool, pnlPts, armPts float64) bool {
	switch arm {
	case TrailArmImmediate:
		return true
	case TrailArmAfterPoints:
		return armPts > 0 && pnlPts >= armPts
	default: // after_breakeven
		return beFired
	}
}

// trailTarget computes the raw trail level from the best price and ATR. Units:
// price POINTS at every layer — ATR is in points, mult is dimensionless.
func trailTarget(side string, best, atr, mult float64) float64 {
	if strings.ToLower(side) == "long" {
		return best - mult*atr
	}
	return best + mult*atr
}

// trailDecide applies the ratchet + BE-floor rails and returns whether to emit
// and the final level. Pure — the whole 3B.4 test matrix runs on this.
//   - LONG: stop only moves UP; SHORT: only DOWN (R-A).
//   - After breakeven fired, the level never crosses back through entry (R-B):
//     a trail worse than the BE floor loses to it (no emit — BE already rests).
//   - Emits only when strictly better than lastEmitted (idempotence).
func trailDecide(side string, target, lastEmitted, entry float64, beFired bool) (bool, float64) {
	long := strings.ToLower(side) == "long"
	// R-B — the BE floor: once breakeven fired the working stop is at least
	// entry; a trail on the wrong side of it is not an improvement.
	if beFired {
		if long && target <= entry {
			return false, 0
		}
		if !long && target >= entry {
			return false, 0
		}
	}
	// R-A — ratchet-only vs the last emitted level.
	if lastEmitted > 0 {
		if long && target <= lastEmitted {
			return false, 0
		}
		if !long && target >= lastEmitted {
			return false, 0
		}
	}
	return true, target
}

// maybeTrailStop runs one trailing beat for an open position (called from
// checkPositionDrawdown on the 60s monitor, beside maybeMoveStopToBreakeven).
func (at *AutoTrader) maybeTrailStop(symbol, side string, entryPrice, markPrice float64) {
	if at.exchange != "ninjatrader" || at.config.StrategyConfig == nil {
		return
	}
	// FIX-AUTOBE-OFF-IN-MENTOR (owner ruling 2026-10-09): trailing is an AI-mode
	// stop mover. With mentor mode ON, mentor trades follow ONLY the mentor's
	// exits — the AI-era trail must not move a mentor-owned stop.
	if at.mentorEnabled() {
		return
	}
	enabled, mult, period, arm, armPts := trailingConfig(at.config.StrategyConfig.RiskControl)
	if !enabled {
		return
	}
	key := symbol + "_" + strings.ToUpper(side)

	at.trailMu.Lock()
	if at.trailStates == nil {
		at.trailStates = make(map[string]*trailState)
	}
	ts, ok := at.trailStates[key]
	if !ok {
		ts = &trailState{BestPrice: markPrice}
		at.trailStates[key] = ts
	}
	long := strings.ToLower(side) == "long"
	if long && markPrice > ts.BestPrice {
		ts.BestPrice = markPrice
	}
	if !long && (ts.BestPrice == 0 || markPrice < ts.BestPrice) {
		ts.BestPrice = markPrice
	}
	best := ts.BestPrice
	lastEmitted := ts.LastEmitted
	wasArmed := ts.Armed
	at.trailMu.Unlock()

	at.breakevenMu.Lock()
	beFired := at.breakevenDone[key]
	at.breakevenMu.Unlock()

	pnlPts := markPrice - entryPrice
	if !long {
		pnlPts = entryPrice - markPrice
	}
	if !trailArmed(arm, beFired, pnlPts, armPts) {
		return
	}
	if !wasArmed {
		at.trailMu.Lock()
		ts.Armed = true
		at.trailMu.Unlock()
		at.logWarnf("📈 trailing_armed mode=%s — %s %s: trail = best ∓ %.1f×ATR%d(%s), ratchet-only via move_stop.",
			arm, symbol, side, mult, period, trailATRTimeframe)
	}

	if market.FuturesBarsProvider == nil {
		return
	}
	bars := market.FuturesBarsProvider(symbol, trailATRTimeframe, period+1)
	atr := market.ExportCalculateATR(bars, period)
	if atr <= 0 {
		return // no verifiable ATR → no move this beat (fail-safe)
	}
	target := trailTarget(side, best, atr, mult)
	emit, level := trailDecide(side, target, lastEmitted, entryPrice, beFired)
	if !emit {
		return
	}
	// 0B — SUSPENDED: the ratchet computed a new level; the broker is never
	// resolved and nothing is sent.
	if at.exitMechSuspendedRefuse("atr-trail", fmt.Sprintf("%s %s ratchet → stop %.2f", symbol, side, level)) {
		return
	}
	ntTCP, ok2 := at.trader.(*ntTrader.TCPTrader)
	if !ok2 {
		return
	}
	// The #52-proven amendment path: tick-rounds, refuses widens (B1), preserves
	// the OCO target. Method name says breakeven; it moves a stop to any price.
	if err := moveStopWire(ntTCP, side, level); err != nil {
		at.logWarnf("⚠️ trailing: move_stop send failed for %s %s → %.2f: %v (will retry next beat)", symbol, side, level, err)
		return
	}
	at.trailMu.Lock()
	old := ts.LastEmitted
	ts.LastEmitted = level
	at.trailMu.Unlock()
	// WARN — Phase 1 honest-logs path (log_events + dashboard).
	at.logWarnf("📈 trailing_moved old=%.2f new=%.2f best=%.2f atr=%.2f mult=%.1f — %s %s stop ratcheted.",
		old, level, best, atr, mult, symbol, side)
}

// pruneTrailStates clears trail memory for positions no longer open (re-arms
// fresh for the next trade). Called beside pruneBreakevenDone.
func (at *AutoTrader) pruneTrailStates(openKeys map[string]bool) {
	at.trailMu.Lock()
	defer at.trailMu.Unlock()
	for k := range at.trailStates {
		if !openKeys[k] {
			delete(at.trailStates, k)
		}
	}
}

// currentTrailLevel reports the last emitted trail level for a position key
// ("symbol_SIDE"), 0 when none. Read by the watcher for its prompt context.
func (at *AutoTrader) currentTrailLevel(key string) float64 {
	at.trailMu.Lock()
	defer at.trailMu.Unlock()
	if ts, ok := at.trailStates[key]; ok {
		return ts.LastEmitted
	}
	return 0
}
