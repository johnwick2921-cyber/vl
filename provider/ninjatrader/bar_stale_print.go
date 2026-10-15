package ninjatrader

import (
	"math"
	"sync/atomic"
	"time"

	"vl/market"
)

// ── STALE-PRINT GUARD (WAVE 26, 2026-10-15) ────────────────────────────────
//
// 10-14 20:00 CT: NT8 restarted (AddOn build 2026-10-14-a) and the first LIVE
// print carried a price from about a day earlier — MNQ 1m o 29931.25 / h
// 30754.0 / l 29927.0 / c 30753.75 while the real close was ~29954; ES 1m
// 19:59/20:00 c 7845.25 while the real close was ~7744. It was stored as
// `live`, so no replay could overwrite it (the store's upsert rule protects
// live rows), it inflated ATR (+81 pts on one planner stop), drew an 820-pt
// wick, and polluted the mentor's overnight range. The scale-break guard SAW a
// mismatch and trusted the LIVE side — which was the stale one.
//
// ROOT CAUSE [A]: the AddOn's BarsRequest rebuild on (re)subscribe re-emits the
// tail of its RELOADED series as forming bar_update frames
// (VLBarsSubscriptionManager.cs OnBarsUpdate — the dedup guard is reset with
// LastEmittedTimeUtcMs=0 on a fresh request, so the reloaded bars are not
// filtered). The stale last bar's high/close (30754.0/30753.75) is merged into
// the forming 20:00 minute, whose open (29931.25) and low (29927.0) already
// carried real live ticks. The scale-break guard (detectScaleMismatch) only
// fires ONCE per key and only on the FIRST live bar after a historical seed,
// and it trusts the live side; for MNQ 1m and every higher timeframe there was
// no adjacent historical reference left to compare, so the print was accepted
// as `live`. ES 1m was caught (labelled `mixed`) because its historical seed
// was still adjacent.
//
// THIS GUARD closes the hole on the Go side. For the first
// stalePrintGuardWindow after a (re)connect/hello, a live bar whose CLOSE sits
// an impossible distance from BOTH its own open AND the previous bar's close —
// max(stalePrintMinTicks ticks, stalePrintPct of price) — is quarantined: the
// bar is relabelled MIXED (values untouched, A24) so no reader takes it. The
// window is the point: inside it the AddOn is re-emitting its loaded series,
// not live ticks, so a close that moved a full 0.5% from its own open is a
// replay of a stale series, not a trade. Outside the window the live feed is
// trusted exactly as before. A real fast market (a 1% move over several
// minutes) never trips it: each minute's close stays near its own open, and a
// gap open stays near the close it opens from.

const (
	// stalePrintGuardWindow is how long after a (re)connect/hello the guard is
	// armed. The AddOn's BarsRequest rebuild + first bar_update land within
	// seconds of the accept; 30 s covers it with headroom without touching a
	// normal reconnect's later, real ticks.
	stalePrintGuardWindow = 30 * time.Second
	// stalePrintMinTicks is the K-tick floor of the impossible-move bound (40
	// ticks = 10 pts on MNQ/ES at 0.25). It exists so a very low-priced symbol
	// still has a bound when the percentage leg would be tiny.
	stalePrintMinTicks = 40
	// stalePrintPct is the P% leg of the impossible-move bound (0.5% of the
	// bar's own open, the same fraction ScaleMismatchPct uses). A 1m close
	// 0.5% from its own open inside the reconnect window is not a trade.
	stalePrintPct = 0.005
)

// stalePrintQuarantined counts live bars the guard relabelled MIXED since boot.
var stalePrintQuarantined atomic.Int64

// StalePrintQuarantined reports the lifetime count of quarantined stale prints.
func StalePrintQuarantined() int64 { return stalePrintQuarantined.Load() }

// NoteReconnect arms the stale-print guard. Called by the TCP server's accept
// loop at the same instant it records the attempted-replay reconnect, so the
// guard window covers the AddOn's hello + BarsRequest rebuild + first
// bar_update frames that follow it.
func (c *BarCache) NoteReconnect(now time.Time) {
	if c == nil {
		return
	}
	c.lastReconnectUnixNano.Store(now.UnixNano())
}

// stalePrintBound is the impossible-move bound in price points: the larger of
// the tick floor and the percentage leg, read from the bar's own open.
func stalePrintBound(symbol string, open float64) float64 {
	tick := market.FuturesTickSize(symbol)
	if tick < 0 {
		tick = 0
	}
	return math.Max(stalePrintMinTicks*tick, stalePrintPct*math.Abs(open))
}

// quarantineStalePrint relabels b MIXED when it is an impossible live print
// inside the reconnect window. prev is the newest ring bar strictly BEFORE b
// (nil = no neighbour, nothing to judge against). Returns the (possibly
// relabelled) bar and whether it was quarantined.
func (c *BarCache) quarantineStalePrint(symbol string, b Bar, prev *Bar) (Bar, bool) {
	if c == nil || b.Source != BarSourceLive || b.C == 0 || b.O == 0 {
		return b, false
	}
	recon := c.lastReconnectUnixNano.Load()
	if recon == 0 || time.Now().UnixNano()-recon > int64(stalePrintGuardWindow) {
		return b, false
	}
	if prev == nil || prev.C == 0 {
		return b, false // no neighbour: nothing to judge the close against
	}
	bound := stalePrintBound(symbol, b.O)
	// The close must be impossible against BOTH the bar's own open and the
	// previous close. A normal bar closes near its own open; a gap open closes
	// near the close it opened from. Only a stale reloaded series closes far
	// from both.
	if math.Abs(b.C-b.O) <= bound {
		return b, false
	}
	if math.Abs(b.C-prev.C) <= bound {
		return b, false
	}
	b.Source = BarSourceMixed
	stalePrintQuarantined.Add(1)
	return b, true
}

// prevBarBefore returns the newest ring bar strictly before t, or nil. It is
// the "neighbouring minute" the stale-print guard judges a live close against.
func prevBarBefore(existing []Bar, t int64) *Bar {
	for i := len(existing) - 1; i >= 0; i-- {
		if existing[i].T < t {
			return &existing[i]
		}
	}
	return nil
}
