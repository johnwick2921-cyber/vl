package kernel

import (
	"sync/atomic"
	"time"

	"nofx/logger"
	"nofx/market"
	"nofx/telemetry"
)

// C2 — CLOCK-DRIFT ENTRY GUARD.
//
// The bot timestamps every signal with the LOCAL clock, and the NT8 AddOn rejects
// signals whose timestamp is more than ~60s from its own (Tradovate) clock. So if
// the local system clock drifts more than a minute from real time, entries get
// silently rejected or mis-timed. NTP isn't guaranteed on this box, so we detect
// drift from the only external time reference we already have: the freshest feed
// bar's labeled timestamp.
//
// A bar is labeled at its OPEN, so under a correct clock and a live feed the bar
// that just closed is labeled ~one interval ago; comparing the local clock to the
// bar's approximate CLOSE (open + interval) yields ~0. A large positive drift means
// the clock is AHEAD (or the feed is lagging); a large negative drift means the
// clock is BEHIND (the feed labeled a bar in the future — impossible unless the
// local clock is wrong, which B4's staleness check cannot catch). Either way,
// entries are blocked; exits / open-position management are never touched.

const clockDriftToleranceMs = 60_000 // >60s local-vs-feed skew blocks NEW entries

// clockDriftMs approximates the local-vs-feed clock skew: nowMs minus the freshest
// bar's close (open label + one interval). ~0 under a correct clock + live feed.
func clockDriftMs(nowMs, freshestBarMs, intervalMs int64) int64 {
	return nowMs - (freshestBarMs + intervalMs)
}

// freshestIntradayBar returns the newest 1m (else 5m) bar time and its interval,
// (0,0) if neither is present.
func freshestIntradayBar(md *market.Data) (barMs, intervalMs int64) {
	for _, tf := range []string{"1m", "5m"} {
		if b := newestBarMs(md, tf); b > 0 {
			return b, tfIntervalMs(tf)
		}
	}
	return 0, 0
}

func absI64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// applyClockDriftBlock (C2) detects local-vs-feed clock skew. Since 2026-08-18
// outgoing NT8 signals are stamped with the FEED clock (trader/ninjatrader
// tcp_trader.go feedNowUTC), so a skewed local clock can no longer age a signal
// into NT8's 60s stale rejection. The guard no longer converts entries to wait —
// it logs the skew as a warning so the operator still sees when NTP needs
// attention. Only open_long/open_short are examined; no 1m/5m data → nothing
// to compare (never blocks).
func applyClockDriftBlock(fd *FullDecision, ctx *Context, nowMs int64) {
	if fd == nil || ctx == nil {
		return
	}
	for i := range fd.Decisions {
		d := &fd.Decisions[i]
		if d.Action != "open_long" && d.Action != "open_short" {
			continue
		}
		barMs, ivMs := freshestIntradayBar(ctx.MarketDataMap[d.Symbol])
		if barMs <= 0 || ivMs <= 0 {
			continue // fail-open
		}
		drift := clockDriftMs(nowMs, barMs, ivMs)
		if absI64(drift) <= clockDriftToleranceMs {
			continue
		}
		dir := "AHEAD of"
		if drift < 0 {
			dir = "BEHIND"
		}
		logger.Warnf("⚠️ clock-drift DETECTED (no entry block): local clock is %s the feed by %ds (>%ds) — signals are feed-stamped so entries proceed; fix the host clock / NTP. Exits unaffected.",
			dir, absI64(drift)/1000, clockDriftToleranceMs/1000)
		telemetry.IncClockSkewObserved(ctx.TraderID)
	}
}

// ── F6 CLOCK-HOLD (2026-08-30) — escalation layer ─────────────────────────────
//
// Root cause of the 2026-08-30 regression: the OS-level fix (chrony) was
// HANDCUFFED — chronyd-starter.sh detected WSL2 as a container and appended
// `-x` ("Disabled control of system clock"), so `makestep 1 -1` never stepped;
// the cron belt-and-suspenders called `hwclock`, which does not exist in this
// WSL2 rootfs. The machine therefore protects ITSELF at the tolerance breach:
//
//   • below CLOCK_WARN_MS (30s): nothing changes.
//   • warn band (30-60s):   log-only (P1.3, unchanged) + news windows widened
//                           by the measured drift so red-news protection
//                           survives a skewed clock.
//   • tolerance breach (>60s): DEFER NEW PLAN AUTHORING (no plan written, no
//                           budget consumed — never author on a clock known
//                           broken) + news windows widened by the drift.
//
// Exits / armed-order management / open-position management are never touched
// (same blast-radius contract as C2). Fail-open on no measurement, exactly like
// C2 ("no data → nothing to compare, never blocks").

// clockDriftStore keeps the last measured local-vs-feed drift (ms, sign
// preserved), written by every clock-health read (boot + session-roll) and by
// on-demand FeedClockDriftMs measurements. 0/have=false = never measured.
var clockDriftStore atomic.Int64
var clockDriftHave atomic.Bool

// RecordClockDrift persists a drift measurement for F6 consumers
// (authoring gate, news-window widening).
func RecordClockDrift(driftMs int64, have bool) {
	clockDriftHave.Store(have)
	if have {
		clockDriftStore.Store(driftMs)
	}
}

// LastClockDrift returns the most recent persisted measurement, or
// (0, false) when nothing was measured yet.
func LastClockDrift() (driftMs int64, ok bool) {
	if !clockDriftHave.Load() {
		return 0, false
	}
	return clockDriftStore.Load(), true
}

// FeedClockDriftMs measures the drift RIGHT NOW from the freshest 1m bar of
// symbol (the same channel LogClockHealth uses: local clock vs the bar's
// approximate close). (0, false) when the feed is unavailable — fail-open.
func FeedClockDriftMs(symbol string) (int64, bool) {
	if market.FuturesBarsProvider == nil {
		return 0, false
	}
	bars := market.FuturesBarsProvider(symbol, "1m", 3)
	if len(bars) == 0 {
		return 0, false
	}
	last := bars[len(bars)-1]
	drift := time.Now().UnixMilli() - (last.OpenTime + 60_000)
	RecordClockDrift(drift, true)
	return drift, true
}

// ClockHoldDecision is the pure F6 decision: (deferAuthoring, widenMs).
//
//   - no measurement        → (false, 0)   fail-open (C2 contract)
//   - |drift| <  warn       → (false, 0)
//   - warn ≤ |drift| ≤ tol  → (false, |drift|)  author, windows widened
//   - |drift| > tol, drift < 0 → (true, |drift|)  authoring deferred
//   - |drift| > tol, drift > 0 → (false, |drift|) author, windows widened
//
// The asymmetry is deliberate. NEGATIVE drift means the feed labeled bars in
// the FUTURE — impossible unless the local clock is broken (the 2026-08-30
// incident class: -41s, NTPSynchronized=no). POSITIVE drift is ambiguous: it
// is also exactly what a CLOSED market's old bars look like (the P0B 16:55
// Sunday read must fire with 10-min-old bars). C2 already covers the
// positive-skew entry risk via feed-stamped signals, so authoring is never
// deferred on the ambiguous sign; the news windows still widen so a mis-timed
// red event cannot slip through the blackout.
func ClockHoldDecision(driftMs int64, have bool, warnMs, toleranceMs int64) (deferAuthoring bool, widenMs int64) {
	if !have {
		return false, 0
	}
	abs := absI64(driftMs)
	if abs >= warnMs {
		widenMs = abs
	}
	if driftMs < 0 && abs > toleranceMs {
		return true, abs
	}
	return false, widenMs
}
