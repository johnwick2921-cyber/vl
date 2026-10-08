package kernel

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"vl/logger"
	"vl/market"
	"vl/telemetry"
)

// B4 — stale-data ENTRY block. A NEW entry requires the freshest intraday bar to
// be recent (≤ 2× its interval old); a frozen / stale feed → refuse the entry. Only
// open_long/open_short are affected — open-position management and exits are never
// blocked (a stale feed must not strand an open position).

// THE AGE CONTRACT (stale-bar dispatch, 2026-08-19).
//
// A timeframe's feed is stale only when the bar that SHOULD exist for the
// current period is missing or old:
//
//	expected_open = floor(now/period) * period
//	stale iff newest_bar.open < expected_open − period − grace
//
// i.e. the newest bar is neither the current FORMING bar nor the immediately
// previous one, with `grace` seconds of delivery slack (env STALE_BAR_GRACE_S,
// default 15 — config, no literal on the path).
//
// WHY THE OLD MATH HAD TO GO: `age = now − newest_stamp > 2×interval` was
// calibrated in the era when NT8's period-END stamps were mislabelled as opens
// (17fcb640, 2026-08-13). The open-stamp correction (e9084e38) silently removed
// one full period of slack: the newest bar's stamp moved from its end to its
// open, so the same bar read one period older. Combined with the evaluation
// clock being POST-AI-CALL while the bars are a CYCLE-START snapshot, a legal
// slow call (now up to 300s) could make a perfectly live 1m feed read >120s
// old and silently neutralize an entry to wait. Zero live fires so far — only
// because the drought suppressed entry decisions — but the trap was armed.
//
// Two separations fix it for good:
//   - the FEED question is measured at SNAPSHOT time (ctx.SnapshotMs): "was the
//     feed alive when this data was assembled". Call latency is not staleness —
//     the supersession guard (staleBarDiscard, shift-invariant, era-safe) owns
//     the call-spans-a-bar-close case.
//   - each timeframe is judged against ITS OWN period; nothing is shared.
const DefaultStaleBarGraceSeconds = 15

// staleBarGraceMs resolves the delivery-slack grace (env STALE_BAR_GRACE_S).
func staleBarGraceMs() int64 {
	if v := os.Getenv("STALE_BAR_GRACE_S"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return int64(n) * 1000
		}
	}
	return DefaultStaleBarGraceSeconds * 1000
}

// tfIntervalMs returns the bar interval in ms, 0 unknown. B1 (T3): delegates
// to THE one table (kernel/timeframes.go) — the 1m/5m/15m-only private copy
// made every other legal primary TF invisible to forming labels + B4.
func tfIntervalMs(tf string) int64 {
	if ms, ok := TFDurationMs(tf); ok {
		return ms
	}
	return 0
}

// newestBarMs returns the timestamp (ms) of the newest kline for tf, 0 if none.
func newestBarMs(md *market.Data, tf string) int64 {
	if md == nil || md.TimeframeData == nil {
		return 0
	}
	td := md.TimeframeData[tf]
	if td == nil || len(td.Klines) == 0 {
		return 0
	}
	return td.Klines[len(td.Klines)-1].Time
}

// barIsStale implements the age contract for ONE timeframe: the bar that should
// exist for the current period is missing/old. Unknown inputs → fail-open.
func barIsStale(newestBarMs, intervalMs, nowMs int64) bool {
	if newestBarMs <= 0 || intervalMs <= 0 {
		return false
	}
	expectedOpen := (nowMs / intervalMs) * intervalMs
	return newestBarMs < expectedOpen-intervalMs-staleBarGraceMs()
}

// StaleEntryGateFeed is the MENTOR stale-data block's ONE definition (release
// #11): the SAME barIsStale formula B4 applies on the AI path — the
// expected-open − interval − grace rule, ~75 s for the 1m — plus the SAME
// CME-open/halt awareness. While CME is closed (the daily 16:00–17:00 break,
// weekends, holidays) no bars arrive, so the formula must not fire there or it
// would false-refuse every halt. The planner path already inherits this
// awareness (it only runs during an open session); the mentor tick runs around
// the clock and needs it explicit. Unknown bar → fail-open (not stale).
func StaleEntryGateFeed(newestBarOpenMs int64, now time.Time) bool {
	if !IsCMEOpen(now) {
		return false
	}
	return barIsStale(newestBarOpenMs, 60_000, now.UnixMilli())
}

// staleEntryFeed reports whether the freshest intraday feed (1m preferred, else
// 5m) is stale enough to block NEW entries, each timeframe judged against ITS
// OWN period. No 1m/5m data → fail-open (not stale).
func staleEntryFeed(md *market.Data, nowMs int64) (tf string, ageMs, limitMs int64, stale bool) {
	for _, cand := range []string{"1m", "5m"} {
		bMs := newestBarMs(md, cand)
		if bMs <= 0 {
			continue
		}
		iv := tfIntervalMs(cand)
		// limit = how old newest_open may be before the contract trips.
		expectedOpen := (nowMs / iv) * iv
		limit := (nowMs - expectedOpen) + iv + staleBarGraceMs()
		return cand, nowMs - bMs, limit, barIsStale(bMs, iv, nowMs)
	}
	// B2 (T6): no 1m/5m subscribed — the gate used to be BLIND here (fail-open
	// with {15m,1h}-only selections). Fallback: the freshest bar of ANY
	// subscribed TF against the unified flat-state feed policy.
	if age, anyTF, ok := FreshestSubscribedAge(md, nowMs); ok {
		limit := LoadFeedPolicy().FlatAlertMs
		return anyTF, age, limit, age > limit
	}
	return "", 0, 0, false
}

// applyStaleDataBlock (B4) neutralizes an open decision to `wait` when the freshest
// 1m/5m bar is stale (feed frozen). Only open_long/open_short are affected — exits
// and open-position management are never blocked.
func applyStaleDataBlock(fd *FullDecision, ctx *Context, nowMs int64) {
	if fd == nil || ctx == nil {
		return
	}
	// Measure the FEED at snapshot-build time, not after the AI call: a legal
	// slow call must never turn a live-at-snapshot feed into "stale".
	//
	// P8 (ledger-close 2026-08-19, the #48 leftover): SnapshotMs is now ALWAYS
	// stamped at data assembly (P7 — GetFullDecisionWithStrategy re-stamps it
	// next to the market fetch), so an absent value means a context that never
	// went through assembly. Evaluating those on the post-call wall clock was
	// the hidden-clock defect class: WARN + fail-open instead — entries pass
	// unassessed rather than being judged on a clock nobody chose. The nowMs
	// parameter stays for the post-verdict diagnostics line only.
	if ctx.SnapshotMs <= 0 {
		logger.Warnf("⚠️ B4 stale-data gate: ctx.SnapshotMs absent (context skipped data assembly) — fail-open, entries pass unassessed (trader=%s)", ctx.TraderID)
		return
	}
	evalMs := ctx.SnapshotMs
	for i := range fd.Decisions {
		d := &fd.Decisions[i]
		if d.Action != "open_long" && d.Action != "open_short" {
			continue
		}
		md := ctx.MarketDataMap[d.Symbol]
		if tf, age, limit, stale := staleEntryFeed(md, evalMs); stale {
			// 2.4 — every discard self-diagnoses: live-feed age + clock drift +
			// a verdict hint, so "feed or clock or math" is one glance.
			feedAge, drift := liveFeedDiagnostics(d.Symbol, nowMs)
			hint := "math"
			switch {
			// Clock wins first: a skewed local clock inflates feedAge by the
			// same skew, so drift past C2 tolerance explains BOTH readings.
			case absI64(drift) > C2ToleranceMs():
				hint = "clock"
			case feedAge < 0 || feedAge > limit:
				hint = "feed" // the LIVE cache is also old → the wire really gapped
			}
			logger.Warnf("⛔ stale_bar_discarded tf=%s age=%ds thresh=%ds drift_ms=%+d feed_last_bar_age=%ds verdict_hint=%s — %s %s → WAIT (exits unaffected)",
				tf, age/1000, limit/1000, drift, feedAge/1000, hint, d.Symbol, d.Action)
			reason := fmt.Sprintf("freshest %s bar %dms old (>%dms) hint=%s", tf, age, limit, hint)
			d.RefusalReason = "stale-data: " + reason
			d.Action = "wait"
			telemetry.IncGateBlock(ctx.TraderID, "stale_data")
			telemetry.RecordError(ctx.TraderID, "entry_refused", "stale-data: "+reason, telemetry.CostTradeLost)
		}
	}
}

// liveFeedDiagnostics reads the LIVE bar provider (not the snapshot) for the
// freshest 1m bar age and the local-vs-feed drift, for the self-diagnosing
// discard line. Futures-only; crypto (no provider) returns (-1, 0).
func liveFeedDiagnostics(symbol string, nowMs int64) (feedAgeMs, driftMs int64) {
	if market.FuturesBarsProvider == nil {
		return -1, 0
	}
	bars := market.FuturesBarsProvider(symbol, "1m", 2)
	if len(bars) == 0 {
		return -1, 0
	}
	newest := bars[len(bars)-1].OpenTime
	// Open-stamp contract: newest+interval is NT8's own stamp of the bar.
	return nowMs - newest, clockDriftMs(nowMs, newest, 60_000)
}
