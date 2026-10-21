package kernel

import "vl/market"

// FeedFreshness classifies the admission-relevant feed state for a NEW entry.
// Broker mode and candle freshness are INDEPENDENT dimensions; this folds them
// into one admission verdict with a fixed precedence.
type FeedFreshness int

const (
	// FeedFreshFresh — candles are current and the broker is not explicitly
	// delayed (mode realtime or UNKNOWN). Admission allowed.
	FeedFreshFresh FeedFreshness = iota
	// FeedFreshStale — candles are missing or stale by timestamp, regardless of
	// the broker mode. Admission refused (stale).
	FeedFreshStale
	// FeedFreshDelayed — the broker explicitly reported delayed mode, regardless
	// of candle freshness. Admission refused (delayed).
	FeedFreshDelayed
)

// ClassifyFeedFreshness returns the admission verdict. Precedence (CTO schema):
// delayed > stale/missing > fresh. An UNKNOWN broker mode ("" — legacy AddOn or
// absent) is NOT a reject reason on its own; missing/stale candles are still
// refused, and fresh candles with unknown mode are allowed.
func ClassifyFeedFreshness(mode string, newestBarMs, intervalMs, nowMs int64) FeedFreshness {
	if mode == "delayed" {
		return FeedFreshDelayed
	}
	if newestBarMs <= 0 || barIsStale(newestBarMs, intervalMs, nowMs) {
		return FeedFreshStale
	}
	return FeedFreshFresh
}

// feedMode reads the broker mode wired by the ninjatrader transport
// (market.FuturesFeedMode). "" = UNKNOWN (nil provider, or absent/unsupported).
func feedMode() string {
	if market.FuturesFeedMode == nil {
		return ""
	}
	return market.FuturesFeedMode()
}
