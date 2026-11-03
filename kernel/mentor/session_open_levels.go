package mentor

import (
	"fmt"
	"sort"

	"vl/kernel"
	"vl/market"
)

// m466 P2 (release #61) — the session OPEN's levels.
//
// G10 D3.1-R26 (R24): the opening candle's wick IS the high/low.
//
//	"đây là mới lúc open… cái râu này nó sẽ là cái đỉnh" — 29,136.00, Sun
//	13 Sep 17:00 [D3.1 p1 @11:58–12:22]; "anh chị lúc nào cũng phải lấy cái
//	low này làm chuẩn" [@11:32–11:48].
//
// The bot's old highs/lows are 5m/15m fractal pivots (kernel.SwingPointLevels):
// the 17:00 candle after a weekend has a HIGHER Friday left neighbour
// (29,394 on the lesson's tape), so the fractal never makes it a pivot and
// the open wick the mentor draws was missing. sessionOpenPivots adds the
// first 5m candle of the current CME session (17:00–17:05 CT, after the daily
// halt or a weekend) as one high pivot and one low pivot, confirmed at that
// candle's close, with no left-neighbour test. They join the candidates in
// swingOldExtremeLevels; the unbroken test and the nearest selection there
// decide whether either is THE old high / old low.
//
// G11 D3.1-R20 (R15, R16, R22, R23): the gap edge is a level.
//
//	"Cái cạnh dưới của cái gap, lúc nào nó cũng sẽ là resistance… Level là
//	resistance" [D3.1 p1 @09:41–09:51]; the gap is marked from Friday's last
//	close 29,391.25 to the Sunday first candle's high ~29,136–29,140
//	[@07:48–08:13]. A half-filled gap is still valid (R22), a fill may take
//	days (R16), and "em không đánh theo gap" (R23) — there is no gap-fill
//	entry, the edge is only a level.
//
// gapEdgeLevels: a session whose first 5m candle opened with empty space to the
// prior session's last close — gap DOWN: the first 5m High < the last close;
// gap UP: the first 5m Low > the last close — has its NEAR edge in the G10
// open pivot and its FAR edge (the prior last close) as a KindGapEdge level: a
// location, a target and an obstacle. Only a gap of at least KeyLevelPrunePts
// (20) makes the far edge — D5.3-R24's prune keeps the more recent of two lines
// closer than that, and the more recent is the open wick. The edge stands until
// a later closed 1m candle trades THROUGH it (a High above a gap-down edge, a
// Low below a gap-up edge) inside the evaluator's bar slice.
//
// A session boundary that is also a CONTRACT change (the quarterly roll — the
// stored tape mixes "MNQ 09-26" and "MNQ 12-26" around it) makes no gap edge:
// the jump is the roll spread, not empty space in one market.
//
// Both share the knob SessionOpenLevels "on" (default) | "off" (neither — the
// pre-#61 level set, byte-identical).

// sessionOpenLevelsOn reports whether G10/G11 are enabled ("" reads as on).
func sessionOpenLevelsOn(cfg Config) bool {
	return cfg.SessionOpenLevels != SessionOpenLevelsOff
}

const sessionOpenCandleMs = 5 * 60_000

// sessionOpenCandle is one session's first 5m candle read from the 1m tape.
type sessionOpenCandle struct {
	openMs    int64 // the session's 17:00 CT open (== the 5m candle's open)
	closeMs   int64 // the 5m candle's close (exclusive end: openMs + 5m)
	high, low float64
	firstIdx  int // index of the session's first 1m bar
}

// firstSessionCandleAt reads the first 5m candle of the session whose first 1m
// bar is bars[i]. ok=false when that bar does not open inside the session's
// first five minutes (no 17:00 candle on the tape — fail closed: no evidence,
// no line), or when the 5m candle has not closed by now.
func firstSessionCandleAt(bars []market.Kline, i int, now int64) (sessionOpenCandle, bool) {
	openMs, _ := sessionBounds(bars[i].OpenTime, ctime())
	if bars[i].OpenTime < openMs || bars[i].OpenTime >= openMs+sessionOpenCandleMs {
		return sessionOpenCandle{}, false
	}
	c := sessionOpenCandle{openMs: openMs, closeMs: openMs + sessionOpenCandleMs, high: bars[i].High, low: bars[i].Low, firstIdx: i}
	if now < c.closeMs {
		return sessionOpenCandle{}, false // the open candle is still forming
	}
	for j := i + 1; j < len(bars) && bars[j].OpenTime < c.closeMs; j++ {
		if bars[j].High > c.high {
			c.high = bars[j].High
		}
		if bars[j].Low < c.low {
			c.low = bars[j].Low
		}
	}
	return c, true
}

// sessionStartIdx reports whether bars[i] is the first bar of a CME session in
// the slice: the bar before it belongs to an EARLIER session (the daily halt or
// a weekend lies between). The slice's first bar is never one — the prior
// session is unknown.
func sessionStartIdx(bars []market.Kline, i int) bool {
	if i <= 0 || i >= len(bars) {
		return false
	}
	cur, _ := sessionBounds(bars[i].OpenTime, ctime())
	prev, _ := sessionBounds(bars[i-1].OpenTime, ctime())
	return cur != prev
}

// currentSessionOpen finds the first 5m candle of the session the newest bar
// belongs to.
func currentSessionOpen(bars []market.Kline, now int64) (sessionOpenCandle, bool) {
	if len(bars) == 0 {
		return sessionOpenCandle{}, false
	}
	openMs, _ := sessionBounds(bars[len(bars)-1].OpenTime, ctime())
	i := sort.Search(len(bars), func(i int) bool { return bars[i].OpenTime >= openMs })
	if i >= len(bars) {
		return sessionOpenCandle{}, false
	}
	return firstSessionCandleAt(bars, i, now)
}

// sessionOpenPivots — G10: the current session's open candle as one swing HIGH
// and one swing LOW candidate (kernel.DetectedLevel, the shape
// swingOldExtremeLevels reads), confirmed at the open candle's close. nil when
// the knob is off or no 17:00 candle is on the tape.
func sessionOpenPivots(bars []market.Kline, cfg Config, now int64) []kernel.DetectedLevel {
	if !sessionOpenLevelsOn(cfg) {
		return nil
	}
	c, ok := currentSessionOpen(bars, now)
	if !ok {
		return nil
	}
	formed := c.closeMs
	hiMs, loMs := formed, formed
	return []kernel.DetectedLevel{
		{Kind: kernel.KindSWGH, Price: c.high, Lo: c.high, Hi: c.high, TF: "5m", Label: "session open high", FormedAtMs: c.openMs, FormedCloseMs: &hiMs},
		{Kind: kernel.KindSWGL, Price: c.low, Lo: c.low, Hi: c.low, TF: "5m", Label: "session open low", FormedAtMs: c.openMs, FormedCloseMs: &loMs},
	}
}

// gapEdgeLevels — G11: the unfilled far edges of every session-open gap in the
// bar slice (a fill may take days, R16). The tape for the fill test is the
// closed 1m candles BEFORE the newest one — the same convention as
// oldExtremeBroken, so the candle being judged (a touch of the edge) is
// measured against the edge that stood before it.
func gapEdgeLevels(bars []market.Kline, cfg Config, now int64) []Level {
	if !sessionOpenLevelsOn(cfg) || len(bars) < 2 {
		return nil
	}
	tape := bars[:len(bars)-1]
	var out []Level
	for i := 1; i < len(bars); i++ {
		if !sessionStartIdx(bars, i) {
			continue
		}
		c, ok := firstSessionCandleAt(bars, i, now)
		if !ok {
			continue
		}
		if bars[i-1].Contract != "" && bars[i].Contract != "" && bars[i-1].Contract != bars[i].Contract {
			continue // a contract roll between the sessions: the price jump is the roll spread, not a gap (fail closed)
		}
		lastClose := bars[i-1].Close
		var size float64
		gapDown := c.high < lastClose
		switch {
		case gapDown:
			size = lastClose - c.high
		case c.low > lastClose:
			size = c.low - lastClose
		default:
			continue // no empty space between the sessions
		}
		if size < cfg.KeyLevelPrunePts {
			continue // D5.3-R24: closer than the prune distance → the newer line (the open wick) stands alone
		}
		if gapFilled(tape, c.firstIdx, lastClose, gapDown) {
			continue
		}
		out = append(out, Level{
			Key:    fmt.Sprintf("%s:%.2f@%d", KindGapEdge, lastClose, c.openMs),
			Kind:   KindGapEdge,
			Price:  lastClose,
			AtTime: bars[i-1].OpenTime,
		})
	}
	return out
}

// gapFilled reports a closed 1m candle of the session or later that traded
// THROUGH the far edge — above it for a gap down, below it for a gap up. A
// touch AT the edge is not a fill (it is the touch the level exists for).
func gapFilled(tape []market.Kline, from int, edge float64, gapDown bool) bool {
	for j := from; j < len(tape); j++ {
		if gapDown && tape[j].High > edge || !gapDown && tape[j].Low < edge {
			return true
		}
	}
	return false
}
