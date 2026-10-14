package kernel

import "time"

// EnsureMarketData (hotfix/watcher-eyes U1, 2026-08-19) — gives callers OUTSIDE
// GetFullDecisionWithStrategy the same market truth the decision path gets.
//
// The anatomy census (PR #56, FAIL U1) proved the in-position watcher was
// MARKET-BLIND as shipped in #55: ctx.MarketDataMap and ctx.SnapshotMs are
// populated only inside GetFullDecisionWithStrategy, and runWatchCycle consumed
// the loop-built context — so the observer prompt carried no bars, no forming
// labels, no Snapshot clock, and price/PnL read 0.
//
// This helper reproduces the decision path's P7 single-instant contract:
// stamp SnapshotMs at assembly, fetch the strategy's timeframes, and arm the
// engine's prompt-snapshot so formingBarLine renders. Idempotent: an already-
// populated map is reused (SnapshotMs still restamped to NOW — the caller is
// about to read fresh state, and a stale stamp is the hidden-clock defect
// class P8 exists to kill).
func EnsureMarketData(ctx *Context, engine *StrategyEngine) error {
	if ctx == nil {
		return nil
	}
	snapshotNow := time.Now()
	ctx.SnapshotMs = snapshotNow.UnixMilli()
	ctx.SnapshotAt = snapshotNow
	if engine != nil {
		engine.SetPromptSnapshotMs(ctx.SnapshotMs)
	}
	if len(ctx.MarketDataMap) == 0 {
		return fetchMarketDataWithStrategy(ctx, engine)
	}
	return nil
}
