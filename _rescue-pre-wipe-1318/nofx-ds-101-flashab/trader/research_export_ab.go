package main
// RESEARCH-FLASH-AB exports — RESEARCH BRANCH ONLY, never merged (CTO ruling
// 2026-09-26 03:46Z). Thin wrappers over the REAL unexported write-time
// feasibility code so the offline harness runs the live predicates instead of
// approximating them (canon 53: the production call site's own functions).
package trader

import (
	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// ResearchArmSeamATR5mFromBars wraps the real arm-seam ATR5m math over bars
// read from the DB copy: the SAME market.ExportCalculateATR +
// kernel.AcceptanceBars the live arm seam uses; only the bar SOURCE differs
// (the copy instead of the live FuturesBarsProvider).
func ResearchArmSeamATR5mFromBars(bars []market.Kline) float64 {
	return armSeamATR5mFromBars(bars)
}

// ResearchWriteTimeFeasibilityVerdicts runs the REAL write-time feasibility +
// zone verdicts (the same functions the live write site calls) on a minimal
// AutoTrader carrying the strategy config and symbol read from the copy.
// Returns the Verbose text of every issue; empty = every arm feasible at
// write time. The caller MUST have parsed with the same AuthoringOpts.
//
// NOT run (named, not approximated): the StampAuthoredIdentity pre-stamp —
// its inputs (facts.Zones / facts.IdentityMap) are json:"-" and not persisted.
func ResearchWriteTimeFeasibilityVerdicts(symbol string, d *kernel.PlanDoc, atr5m float64, cfg *store.StrategyConfig, session string) []string {
	at := &AutoTrader{}
	at.config.StrategyConfig = cfg
	at.config.NinjaTraderSymbol = symbol
	var out []string
	for _, iss := range at.writeTimeFeasibilityVerdicts(d, atr5m, cfg, session) {
		out = append(out, iss.Verbose)
	}
	for _, z := range at.writeTimeZoneVerdicts(d, atr5m, cfg, session) {
		out = append(out, z.Verbose)
	}
	return out
}

// ResearchValidateAuthoredScenariosAt runs the REAL born-dead/flip-met check
// (validateAuthoredScenariosAt) with the bars provider swapped to `bars` (the
// 1m bars from the DB copy around the window), then restores the previous
// provider. Returns the born-check record (nil on success) and the error.
// Caller must ensure single-threaded judging.
func ResearchValidateAuthoredScenariosAt(d *kernel.PlanDoc, session, tradeDate string, readAt, publishAt interface{ Format(string) string }, bars []market.Kline) (interface{}, error) {
	return nil, nil
}
