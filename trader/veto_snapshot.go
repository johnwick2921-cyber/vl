package trader

import (
	"time"

	"vl/kernel"
	"vl/market"
)

// ── THE HTF VETO'S ONE SNAPSHOT BUILDER (2026-10-16, planner HTF veto snapshot) ──
//
// kernel.HTFVetoVerdict judges a structure snapshot. Under HTF_VETO_MODE=cross
// it needs snap["1h"] AND snap["4h"]; a missing key FAILS OPEN (loudly) for
// that TF. That fail-open exists for ONE case: the detector has no bars. It
// was never meant for a caller that simply did not pass the data.
//
// The write-time feasibility mirror of the arm chain did exactly that: it
// called armGateVerdictFor with a literal nil snapshot
// (write_time_feasibility.go, the non-zone leg and the zone far/near edges), so
// every planner write logged "🛡️ HTF veto (cross) SKIPPED — no 1h+4h structure
// snapshot this cycle" (136× on 10-15, every one beside a "🎯 zone at write"
// line) and published counter-trend arms the executor then refused at arm
// (10-15 10:11:55 NY v4 S1 long written → 10:32:59 "arm REFUSED NY S1 leg 1:
// HTF veto … long vs 1h TRENDING_DOWN + 4h TRENDING_DOWN").
//
// armVetoSnapshot is the snapshot every arm-chain caller hands the veto: the 1m
// cache (kernel.AISVPBarInterval × kernel.AISVPBarCount) aggregated to
// 5m/15m/1h, plus the NATIVE 4h series (kernel.StructureSnapshotForVeto, F-4)
// — the same inputs the decision loop (auto_trader_loop.go), the live-bar
// event pass, the strict nudge and the mentor pass build.
//
// FRESHNESS: it is built at verdict time from the live bar cache and never
// cached across verdicts, so it is exactly as fresh as the cache the arm pass
// reads — no second staleness bound exists to drift.
//
// It is never nil. It is EMPTY only when the bar cache is genuinely
// unavailable (no provider, or no bars) — the one case the veto still fails
// open, with its WARN.
func (at *AutoTrader) armVetoSnapshot(now time.Time) map[string]kernel.StructureState {
	var bars []market.Kline
	if market.FuturesBarsProvider != nil {
		bars = market.FuturesBarsProvider(at.futuresSymbol(), kernel.AISVPBarInterval, kernel.AISVPBarCount)
	}
	return kernel.StructureSnapshotForVeto(at.futuresSymbol(), bars, now.UnixMilli())
}

// lazyArmVetoSnapshot memoizes armVetoSnapshot(traderNow()) for ONE verdict
// pass over a plan doc: built on first use (a doc with no enabled arm reads no
// bars) and shared by every leg the pass judges, so the far and near edges of
// a zone are judged on the same snapshot.
func (at *AutoTrader) lazyArmVetoSnapshot() func() map[string]kernel.StructureState {
	var snap map[string]kernel.StructureState
	return func() map[string]kernel.StructureState {
		if snap == nil {
			snap = at.armVetoSnapshot(traderNow())
		}
		return snap
	}
}
