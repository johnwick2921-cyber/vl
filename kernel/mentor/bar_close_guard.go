package mentor

import (
	"time"

	"vl/market"
)

// EX-13 (11-16 full re-read) — the bar-close guard, kernel half.
//
// October reads a candle only after it closes: "quan trọng là việc đóng và mở
// nến. Em không quan tâm việc mở nến. Em quan tâm việc đóng nến" [Oct D3.2
// @34:20-34:33]; "Ngồi đợi cây nến này đóng xong kéo lên" [Oct D4.2
// @30:48-30:56].
//
// The AddOn's bars_historical batch stamps EVERY replay bar final=true
// (ninjascript/VLBarsSubscriptionManager.cs EmitHistorical, "historical bars are
// closed by definition") — NT8's in-progress last bar included — and then moves
// LastFinalizedTimeUtcMs onto it, so OnBarsUpdate never re-sends that bar and
// the boundary finalisation skips it: its real close never reaches Go. On 11-16
// the boot batch's 19:12 bar was emitted at 19:12:45 (close 19:13:00); the cache
// and the store kept that snapshot (v=5140, 615 contracts short of its 3m/5m
// parents), and at 19:14 it was the MOTHER of the armed long ISB.
//
// The frame's own emission clock is the proof Go has: Final=true with EmittedAt
// before the bar's close instant is a forming snapshot labelled final. A bar with
// no emission stamp (store / backfill rows, an AddOn older than emitted_at) is
// judged by Final alone, as before — nothing to compare, never a fabricated
// verdict.

// BarNotTrulyClosed reports whether b is a final-labelled forming snapshot: the
// AddOn marked it Final, but the frame carrying it was emitted before the bar's
// close instant.
func BarNotTrulyClosed(b market.Kline) bool {
	return b.Final && b.EmittedAt > 0 && b.EmittedAt < BarCloseInstant(b)
}

// guardFrameMs is the 5m frame the course draws its boxes and ISB box on [Oct
// D4.3 @58:00-58:33] — the largest frame an entry's own pattern candles are read
// on; the 1m mother + inside pair [Oct D4.3 @52:03-52:49] lies inside it.
const guardFrameMs = int64(5 * time.Minute / time.Millisecond)

// snapshotInEntryCandles returns the newest final-labelled snapshot among the 1m
// bars an entry on this tick reads as its pattern candles: the bars of the newest
// two 5m candles up to the current bar (the 5m mother + inside pair, and the
// current partial 5m), which always holds the 1m mother + inside pair (cur and
// the bar before it). ok=false when none is a snapshot.
func snapshotInEntryCandles(bars []market.Kline) (snap market.Kline, ok bool) {
	n := len(bars)
	if n == 0 {
		return market.Kline{}, false
	}
	cur := bars[n-1]
	from := (BarCloseInstant(cur)/guardFrameMs)*guardFrameMs - 2*guardFrameMs
	for i := n - 1; i >= 0; i-- {
		b := bars[i]
		if b.OpenTime < from && i < n-2 {
			break
		}
		if BarNotTrulyClosed(b) {
			return b, true
		}
	}
	return market.Kline{}, false
}

// barCloseGuardFilter refuses every entry intent (bar_not_truly_closed) while a
// final-labelled snapshot is among this tick's entry candles. Cancels, extends
// and state changes flow untouched; an arm an entry registered this tick is
// un-registered by Tick's single exit pass (unregisterDroppedArms). The refusal
// is noted once per tick, naming the bar and its emission stamp.
func (e *Evaluator) barCloseGuardFilter(out []Intent, bars []market.Kline) []Intent {
	snap, ok := snapshotInEntryCandles(bars)
	if !ok {
		return out
	}
	kept := out[:0:0]
	refused := 0
	for _, in := range out {
		if in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry {
			e.refuse("bar_not_truly_closed")
			refused++
			continue
		}
		kept = append(kept, in)
	}
	if refused > 0 {
		at := time.UnixMilli(snap.OpenTime).In(ctime())
		e.note("entry refused x%d: the %s 1m bar is not truly closed — it was labelled final but its frame was emitted %s, before its %s close (a forming snapshot; its real close never arrived) [EX-13; Oct D3.2 @34:20-34:33]",
			refused, at.Format("01-02 15:04 CT"), time.UnixMilli(snap.EmittedAt).In(ctime()).Format("15:04:05.000 CT"),
			time.UnixMilli(BarCloseInstant(snap)).In(ctime()).Format("15:04:05 CT"))
	}
	return kept
}
