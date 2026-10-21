package kernel

// R22 TIME-WEEKLY (2026-10-14) — dATR FROM COMPLETE CME SESSIONS ONLY.
//
// THE DEFECT (planner math review P1, MATH-INDICATORS F-4). DailyRangeProxy
// averaged every non-developing session-day found in the 2,000-bar 1m ring.
// 2,000 bars ≈ 1.45 sessions, so the OLDEST day in the ring is nearly always
// cut off at the front and was averaged as if complete. dATR therefore slid
// through every session and jumped at 17:00: 418 → 247 on 10-13, and 179.62 at
// 01:44 CT on 10-14 against a true two-session mean of ~380 (−53%). Every
// planner band scales with it: the seat band, the confluence band, the zone
// size multiplier, the validator's target band, the one-setup band, the
// round-number spacing, and the prompt's own dATR.
//
// THE RULE. dATR is the mean H−L of the last DATRCompleteSessions COMPLETE
// CME sessions before the developing one. Complete ranges come from the
// native daily bars (the cache holds months of them); when no daily source
// answers, the intraday slice is used with every front-truncated day dropped.
// DATRCompleteSessions = 2 is the span the 2,000-bar slice always averaged
// (≤ two non-developing days) — the same horizon, with the truncation removed.

import (
	"time"

	"vl/market"
)

// DATRCompleteSessions is how many complete sessions dATR averages.
const DATRCompleteSessions = 2

// datrDailyFetch is how many native daily bars DailyRangeProxyFor reads —
// enough to find DATRCompleteSessions complete sessions across a holiday week.
const datrDailyFetch = 10

// sessionStartSlack tolerates a quiet first minute: a day whose first bar
// opens within this of its 17:00 CT session start is held whole.
const sessionStartSlack = 5 * time.Minute

// sessionFrontTruncated reports whether a session-day whose first bar in the
// slice opens at firstMs was cut off at the front by the slice window.
func sessionFrontTruncated(firstMs int64) bool {
	start := CMESessionDayStart(time.UnixMilli(firstMs))
	return time.UnixMilli(firstMs).Sub(start) > sessionStartSlack
}

// meanOfLastSessions averages the last DATRCompleteSessions ranges (oldest →
// newest input).
func meanOfLastSessions(ranges []float64) (float64, bool) {
	if len(ranges) == 0 {
		return 0, false
	}
	if len(ranges) > DATRCompleteSessions {
		ranges = ranges[len(ranges)-DATRCompleteSessions:]
	}
	var sum float64
	for _, r := range ranges {
		sum += r
	}
	return sum / float64(len(ranges)), true
}

// completeSessionRangesFromDaily returns the H−L of every COMPLETE session in
// the native daily bars, oldest → newest: the session's trading-date 16:00 CT
// close is past and it is not the developing session at `now`.
func completeSessionRangesFromDaily(daily []market.Kline, now time.Time) []float64 {
	nowKey := CMESessionDayKey(now)
	var out []float64
	for _, d := range SessionDaysFromNativeDaily(daily) {
		if d.CloseTime >= now.UnixMilli() || CMESessionDayKey(time.UnixMilli(d.OpenTime)) == nowKey {
			continue
		}
		if d.High > d.Low {
			out = append(out, d.High-d.Low)
		}
	}
	return out
}

// DailyRangeProxyFor is dATR for a symbol: the mean range of the last
// DATRCompleteSessions complete CME sessions, read from the native daily bars;
// the intraday slice (DailyRangeProxy, front-truncated days dropped) when no
// daily source answers. 0 when neither has a complete session — the caller's
// existing fallback applies.
func DailyRangeProxyFor(symbol string, bars []market.Kline, now time.Time) float64 {
	if m, ok := meanOfLastSessions(completeSessionRangesFromDaily(DailyBarsFor(symbol, datrDailyFetch), now)); ok {
		return m
	}
	return DailyRangeProxy(bars, now)
}
