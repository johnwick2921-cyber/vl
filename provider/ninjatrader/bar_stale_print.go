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
// stalePrintGuardWindow after a (re)connect/hello/bar-rebuild, a live bar whose
// HIGH, LOW or CLOSE sits an impossible distance from BOTH its own open AND the
// previous bar's close — max(stalePrintMinTicks ticks, stalePrintPct of price) —
// is quarantined: the bar is relabelled MIXED (values untouched, A24) so no
// reader takes it. The window is the point: inside it the AddOn is re-emitting
// its loaded series, not live ticks, so a print that moved a full 2% from its
// own open on a 1m–30m bar is a replay of a stale series, not a trade. Outside the window the
// live feed is trusted exactly as before. A real fast market (a 1% move over
// several minutes) never trips it: each minute's extremes stay near its own
// open, and a gap open stays near the open it opens from.

const (
	// stalePrintGuardWindow is how long after a (re)connect/hello/bar-rebuild
	// trigger the guard is armed. The 10-14 restart was at 19:59 and the first
	// stale print carried the 20:00 minute — up to a full minute later (the
	// tcp_server accept/hello lines live in journald, not the file log, so the
	// exact accept→print gap could not be read back; 60 s covers it). A real
	// fast market cannot move a full 2% inside one minute of a reconnect,
	// when the feed is still settling, so the wider window costs nothing.
	stalePrintGuardWindow = 60 * time.Second
	// stalePrintMinTicks is the K-tick floor of the impossible-move bound (40
	// ticks = 10 pts on MNQ/ES at 0.25). It exists so a very low-priced symbol
	// still has a bound when the percentage leg would be tiny.
	stalePrintMinTicks = 40
	// stalePrintPct is the P% leg of the impossible-move bound: 2% of the bar's
	// own open (release #27, 2026-10-15). It was 0.5% in release #26, which is
	// inside the NORMAL range of real bars — measured on the last 3,000 MNQ bars
	// per timeframe, the largest extreme that was far from BOTH its own open and
	// the previous close was 0.64% on 1m (the 10-14 07:30 CPI minute), 0.75% on
	// 5m, 0.93% on 15m and 1.43% on 30m. 2% clears every real bar there and still
	// catches the 10-14 stale print (30753.75 vs 29931.25 = 2.75%).
	stalePrintPct = 0.02
)

// stalePrintJudgedTF lists the ONLY timeframes the guard judges (release #27).
// On 1h and above a real bar's extreme routinely sits 2–4% from its own open
// and the previous close (measured max: 1h 2.98%, 2h 2.75%, 4h 2.98%, 1d
// 3.88%), so no fixed bound separates a stale print from a real bar there. In
// release #26 the 0.5% bound applied to every timeframe and, in the reconnect
// window after the 10-15 boot, hid the REAL forming 1d and 3d bars of MNQ and
// ES (5,876 counted quarantines). An unknown timeframe is never judged.
var stalePrintJudgedTF = map[string]bool{"1m": true, "2m": true, "3m": true, "5m": true, "15m": true, "30m": true}

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

// ── HTF CROSS-CHECK (release #28, 2026-10-15) ────────────────────────────
//
// stalePrintJudgedTF leaves 1h and above unjudged: no fixed bound separates a
// stale print from a real higher-timeframe bar (real 1h–1d extremes reach 3–4%).
// But the 10-14 stale print ALSO landed on the 1h and 2h bars (high 30754 vs a
// real ~29955). An HTF bar cannot have an extreme its own 1m bars never traded:
// they are cut from the same ticks. So, inside the same reconnect window, an
// HTF live bar is judged AGAINST ITS OWN CLEAN 1m BARS — never against a fixed
// percentage of its own range — and only when those 1m bars are COMPLETE for
// the bar's period so far:
//   - the 1m ring reaches back to the HTF bar's open (a 3d/1w bar is longer
//     than the ring, so it is never judged);
//   - no 1m gap > stalePrintHTFMaxGapMs inside the period, except the CME daily
//     break (16:00–17:00 CT) and the weekend;
//   - the newest clean 1m bar is within stalePrintHTFMaxGapMs of now.
// Mixed 1m bars (scale-break or stale-print) are excluded from the reference.
// A high/low/close beyond the clean 1m extremes (or the newest clean 1m close)
// by more than max(40 ticks, 2%) is quarantined. Any doubt → not judged.

// stalePrintHTFTF lists the higher timeframes the cross-check judges.
// Measured on the last ~400 stored bars per TF (MNQ + ES): a real 1h/2h/4h bar's
// extreme exceeds its own clean 1m extremes by at most 0.365% (NT8 cuts both
// from the same ticks), far below the 2% bound. Daily bars are NOT judged: NT8
// stamps them at the calendar day, not the 17:00 CT session open, so their own
// minutes cannot be matched reliably (measured excess up to 1.67%, too close to
// 2%). 6h/8h/12h alignment was not measured, so they are not judged either. The
// 10-14 stale print reached the 1m–2h bars only.
var stalePrintHTFTF = map[string]bool{"1h": true, "2h": true, "4h": true}

// stalePrintHTFMaxGapMs is the largest 1m gap the cross-check accepts inside a
// period (a hole means the 1m reference may miss a real extreme).
const stalePrintHTFMaxGapMs = 3 * 60_000

var chicagoLoc = func() *time.Location {
	if l, err := time.LoadLocation("America/Chicago"); err == nil {
		return l
	}
	return time.FixedZone("CT", -5*3600)
}()

// cmeClosedGap reports whether a 1m gap from prevT to nextT (open stamps, ms)
// is the CME daily break (16:00→17:00 CT) or the weekend, i.e. no trading.
func cmeClosedGap(prevT, nextT int64) bool {
	a := time.UnixMilli(prevT + 60_000).In(chicagoLoc) // first missing minute
	b := time.UnixMilli(nextT).In(chicagoLoc)          // first minute back
	if a.Hour() == 16 && b.Hour() == 17 && b.Sub(a) <= 61*time.Minute {
		return true // daily break
	}
	if a.Weekday() == time.Friday && a.Hour() >= 16 && b.Weekday() == time.Sunday && b.Hour() == 17 {
		return true // weekend
	}
	return false
}

// quarantineStalePrintHTF judges an HTF live bar against its own clean 1m bars
// (caller holds c.mu). Returns the (possibly relabelled) bar and whether it was
// quarantined. Inside the reconnect window only; any incomplete reference →
// not judged.
func (c *BarCache) quarantineStalePrintHTF(symbol, timeframe string, b Bar) (Bar, bool) {
	if c == nil || !stalePrintHTFTF[timeframe] || b.Source != BarSourceLive || b.O == 0 {
		return b, false
	}
	recon := c.lastReconnectUnixNano.Load()
	if recon == 0 || time.Now().UnixNano()-recon > int64(stalePrintGuardWindow) {
		return b, false
	}
	one := c.bars[barKey(symbol, "1m")]
	if len(one) == 0 || one[0].T > b.T {
		return b, false // the 1m ring does not reach back to the HTF bar's open
	}
	end := b.T + timeframeMs(timeframe)
	var ref []Bar
	for _, m := range one {
		if m.T >= b.T && m.T < end && m.Source != BarSourceMixed && m.H != 0 && m.L != 0 {
			ref = append(ref, m)
		}
	}
	if len(ref) == 0 {
		return b, false
	}
	nowMs := time.Now().UnixMilli()
	if ref[0].T-b.T > stalePrintHTFMaxGapMs && !cmeClosedGap(b.T-60_000, ref[0].T) {
		return b, false // the period's first minutes are missing
	}
	for i := 1; i < len(ref); i++ {
		if ref[i].T-ref[i-1].T > stalePrintHTFMaxGapMs && !cmeClosedGap(ref[i-1].T, ref[i].T) {
			return b, false // a 1m hole inside the period: the reference may miss a real extreme
		}
	}
	last := ref[len(ref)-1]
	if end > nowMs {
		if nowMs-last.T > stalePrintHTFMaxGapMs+60_000 {
			return b, false // the 1m feed lags the forming HTF bar: not comparable yet
		}
	} else if end-last.T > stalePrintHTFMaxGapMs+60_000 && !cmeClosedGap(last.T, end) {
		return b, false // a closed HTF bar whose last minutes are missing from the 1m ring
	}
	hi, lo := ref[0].H, ref[0].L
	for _, m := range ref[1:] {
		if m.H > hi {
			hi = m.H
		}
		if m.L < lo {
			lo = m.L
		}
	}
	bound := stalePrintBound(symbol, b.O)
	beyond := (b.H != 0 && b.H-hi > bound) || (b.L != 0 && lo-b.L > bound)
	if !beyond && b.C != 0 && last.C != 0 && end > nowMs && math.Abs(b.C-last.C) > bound {
		beyond = true // a forming HTF close far from the newest clean 1m close
	}
	if !beyond {
		return b, false
	}
	b.Source = BarSourceMixed
	stalePrintQuarantined.Add(1)
	return b, true
}

// quarantineStalePrint relabels b MIXED when it is an impossible live print
// inside the reconnect window. prev is the newest ring bar strictly BEFORE b
// (nil = no neighbour, nothing to judge against). Returns the (possibly
// relabelled) bar and whether it was quarantined.
func (c *BarCache) quarantineStalePrint(symbol, timeframe string, b Bar, prev *Bar) (Bar, bool) {
	if c == nil || b.Source != BarSourceLive || b.C == 0 || b.O == 0 {
		return b, false
	}
	if !stalePrintJudgedTF[timeframe] {
		return b, false // 1h and above (and unknown TFs) are never judged — see stalePrintJudgedTF
	}
	recon := c.lastReconnectUnixNano.Load()
	if recon == 0 || time.Now().UnixNano()-recon > int64(stalePrintGuardWindow) {
		return b, false
	}
	if prev == nil || prev.C == 0 {
		return b, false // no neighbour: nothing to judge the close against
	}
	bound := stalePrintBound(symbol, b.O)
	// Any extreme — the HIGH, the LOW or the CLOSE — that is impossible against
	// BOTH the bar's own open and the previous close quarantines the bar. The
	// close alone is not enough: live ticks after a stale print return the close
	// to the real price while the 820-pt high wick survives (the F2 shape). A
	// normal bar's extremes stay near its own open; a gap open's extremes stay
	// near the open it gapped from. Only a stale reloaded series has an extreme
	// far from both.
	impossible := func(x float64) bool {
		return x != 0 && math.Abs(x-b.O) > bound && math.Abs(x-prev.C) > bound
	}
	if !impossible(b.H) && !impossible(b.L) && !impossible(b.C) {
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
