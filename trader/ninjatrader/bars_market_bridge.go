package ninjatrader

import (
	"time"

	"vl/kernel"
	"vl/logger"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
)

// wireFuturesBarsProvider connects the live NT8 BarCache to the market
// package's futures kline path (Stage 3). Called once when the TCP server
// starts. The legacy crypto path is unaffected — market.GetWithTimeframes
// only calls this hook for CME futures symbols (IsCMEFuturesSymbol).
//
// The cache is keyed by the NT8 instrument symbol the AddOn subscribed to
// (e.g. "MNQ"); the kernel passes the same canonical symbol, so the lookup
// is direct with no remapping.
func wireFuturesBarsProvider(server *ntwire.TCPServer) {
	// BARS HORIZON (2026-09-09) — the EMPTY arm is graced from here, because
	// wiring the bridge IS the boot instant this leaf can know (A28).
	armBarHorizonGrace(time.Now())
	market.FuturesBarsProvider = func(symbol, timeframe string, count int) []market.Kline {
		if server == nil {
			return nil
		}
		// `now` is taken HERE, at the entry point, and handed down (A28).
		return barsFromCache(server.BarCache(), symbol, timeframe, count, time.Now())
	}
	// P0 feed freshness — expose the broker mode (realtime|delayed|"" unknown)
	// so the kernel's entry gate can distinguish explicit delayed from inferred
	// stale. "" is UNKNOWN, never realtime.
	market.FuturesFeedMode = func() string {
		if server == nil {
			return ""
		}
		return server.FeedMode()
	}
	// Release #22 OPS-SAFETY: the stale gate's MONOTONIC receipt leg reads the
	// same cache, and the clock-step guard samples wall vs monotonic every
	// second from here on (one sampler per process).
	kernel.FeedLastLiveReceipt = func(symbol, timeframe string) (time.Time, bool) {
		if server == nil || server.BarCache() == nil {
			return time.Time{}, false
		}
		return server.BarCache().LastLiveReceipt(symbol, timeframe)
	}
	kernel.StartClockStepSampler()
	// m445: from here the bot reads LIVE NT8 bars, so the data-integrity gate
	// applies (bar_integrity.go). Last, after the provider is installed.
	integrityArmed.Store(true)
}

// barsFromCache is the production path, extracted from the closure above so a
// pin drives IT rather than a copy of it (class 86).
//
// THE RETURN VALUE IS BYTE-IDENTICAL to the pre-wave closure: nil on an empty
// cache, the tail when the ring holds MORE than asked, and whatever it has when
// it holds LESS. Only the OBSERVATION is new — this wave adds no gate, refuses
// nothing and degrades nothing (A10/A24).
//
// Before this, a short read was indistinguishable from a short market at every
// one of the 57 consumer call sites. Now the bridge says which it was, once, at
// the choke point, instead of 57 places each having to remember to ask.
func barsFromCache(cache *ntwire.BarCache, symbol, timeframe string, count int, now time.Time) []market.Kline {
	if cache == nil {
		return nil
	}
	bars := cache.Get(symbol, timeframe)
	if count > 0 && len(bars) > count {
		bars = bars[len(bars)-count:]
	}
	out := []market.Kline(nil)
	if len(bars) > 0 {
		out = barsToKlines(bars, timeframe)
	}
	// AFTER the slice is final, so the horizon describes what was SERVED.
	h := kernel.HorizonOf(out, timeframe, count, now)
	if h.Short() || h.Holed() || h.Empty() {
		barHorizonWarn(now, h, symbol, timeframe, resolveBarHorizonCaller(), cache.MaxBars())
	}
	return out
}

// barsToKlines adapts the NT8 wire Bar shape to market.Kline. NT8 bars carry
// no quote-volume / trade-count / taker-split, so those Kline fields stay
// zero — the indicator engine reads only OHLCV. CloseTime is derived from
// OpenTime + the timeframe duration. A10 (T7) comment-truth: the NEWEST bar is
// usually FORMING — the AddOn re-emits the same-open bar as it builds and the
// cache replaces it in place; CloseTime here is the bar's SCHEDULED close, not
// proof it closed. Consumers that need closed bars filter CloseTime < now
// (latestClosedPrimaryBarMs, closedBars, acceptance buckets).
func barsToKlines(bars []ntwire.Bar, timeframe string) []market.Kline {
	durMs := timeframeDurationMs(timeframe)
	out := make([]market.Kline, len(bars))
	for i, b := range bars {
		out[i] = market.Kline{
			OpenTime:  b.T,
			Open:      b.O,
			High:      b.H,
			Low:       b.L,
			Close:     b.C,
			Volume:    b.V,
			CloseTime: b.T + durMs - 1,
			Final:     b.Final,
			EmittedAt: b.EmittedAt,
		}
	}
	return out
}

// timeframeDurationMs maps a timeframe string to its millisecond span. Falls
// back to 60_000 (1m) for unrecognized values. Covers the coded TF
// vocabulary (store/strategy.go::normalizeTimeframe).
func timeframeDurationMs(timeframe string) int64 {
	// B1 (T3): delegate to THE one table. The old unknown→60_000 default made
	// the bridge fabricate CloseTimes for unmapped TFs (a forming bar could
	// count as closed, corrupting the bar-close gate + supersession watermark).
	// Unknown now returns 0 and the caller logs loudly; the PRIMARY timeframe
	// is boot-validated upstream so this is a defensive edge only.
	if ms, ok := kernel.TFDurationMs(timeframe); ok {
		return ms
	}
	logger.Warnf("🧪 bars bridge: UNMAPPED timeframe %q — bars passed through without a derived CloseTime (add it to kernel/timeframes.go)", timeframe)
	return 0
}
