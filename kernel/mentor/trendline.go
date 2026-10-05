package mentor

import (
	"fmt"
	"time"

	"vl/kernel"
	"vl/market"
)

// Trendline — X9 (slide 27) + D2.3 + DAY-3 row 27. A trendline joins two
// same-role swing extremes: a low and a HIGHER low (support, SideLong), or a
// high and a LOWER high (resistance, SideShort). It must slope — never
// horizontal [D2.3 p1 @06:44–07:18]. Two points = EXISTS; a 3rd touch makes
// it VALID (a location) [DAY-3 p2 @03:45–04:07]. A break confirmed by a 5m
// CLOSE discards the line ("ĐỢI KHUNG 5 PHÚT ĐÓNG" [X9 @06:25]); a break is
// NOT an entry. The line dies at day end [D4.1 p2 @01:17–01:30] — the
// today-only swing filter below is its intraday death besides the 5m break.
type Trendline struct {
	Side     Side
	P0Idx    int     // earlier swing extreme (lowest low / highest high), confirmed
	P0Px     float64 // bars[P0Idx] swing price
	P0T      int64   // bars[P0Idx].OpenTime
	P1Idx    int     // later same-role swing forming the slope
	P1Px     float64
	P1T      int64 // bars[P1Idx].OpenTime
	FormedAt int   // P1Idx — the line is drawn when P1 closes
	ValidAt  int   // bar index of the 3rd touch (0 = not yet a location)
	Dead     bool  // a closed 5m candle closed through the line
}

// trendlineTouchBandPts is the wick-touch tolerance for a diagonal line (the
// course's literal touch [D2-40] on a sloping line needs a small band).
const trendlineTouchBandPts = 0.5

// priceAt returns the line's value at a bar open time.
func (t Trendline) priceAt(tm int64) float64 {
	return t.P0Px + (t.P1Px-t.P0Px)/float64(t.P1T-t.P0T)*float64(tm-t.P0T)
}

// touch reports a wick touch of the line at the candle's open time.
func (t Trendline) touch(c market.Kline) bool {
	p := t.priceAt(c.OpenTime)
	return c.High >= p-trendlineTouchBandPts && c.Low <= p+trendlineTouchBandPts
}

// brokenBy5m reports a 5m candle whose CLOSE is through the line (the break
// that discards it): support broken by a close BELOW, resistance by a close
// ABOVE, evaluated at the bucket's open time.
func (t Trendline) brokenBy5m(b market.Kline) bool {
	p := t.priceAt(b.OpenTime)
	if t.Side == SideLong {
		return b.Close < p
	}
	return b.Close > p
}

// TrendlinesBuild is the pure, deterministic trendline scan: one support
// line and one resistance line. The points are STRUCTURAL swings (CTO ruling
// 23:10:31Z, item 12): a point must be a CONFIRMED two-sided swing —
// swings3's left 3-bar fractal plus the right-side confirmation
// (swingConfirmed). Raw one-sided 3-bar fractals are pullback noise and never
// pair a line. The pairing is the two MOST RECENT qualifying same-role swings
// (ONE live line per side: the newest pair replaces any older line), at least
// 5 bars apart. Only today's swings survive. The line is drawn when P1 closes
// and extended right; it becomes a location on the 3rd touch and is discarded
// by a 5m close through.
func TrendlinesBuild(bars []market.Kline, now time.Time) []Trendline {
	seq := swings3(bars)
	today := tradingDayKey(now.In(ctime()))
	var seqToday []swingPairAt
	for _, s := range seq {
		if tradingDayKey(time.UnixMilli(bars[s.idx].OpenTime).In(ctime())) == today {
			seqToday = append(seqToday, s)
		}
	}
	seq = seqToday
	var out []Trendline
	for _, role := range []kernel.LevelKind{kernel.KindSWGL, kernel.KindSWGH} {
		// STRUCTURAL swings only: confirmed two-sided (left fractal + right
		// confirmation). The D2-28 lesson — fractals pair pullback noise.
		var pts []swingPairAt
		for _, s := range seq {
			if s.kind == role && swingConfirmed(bars, s) {
				pts = append(pts, s)
			}
		}
		if len(pts) < 2 {
			continue
		}
		// the two MOST RECENT qualifying swings — the newest pair.
		p0, p1 := pts[len(pts)-2], pts[len(pts)-1]
		if p1.idx-p0.idx < 5 {
			continue // at least 5 bars apart
		}
		side := SideLong
		if role == kernel.KindSWGH {
			side = SideShort
		}
		// must slope: support = a HIGHER low; resistance = a LOWER high.
		// Equal (horizontal) is refused [D2.3 p1 @06:44–07:18].
		if role == kernel.KindSWGL && p1.price <= p0.price {
			continue
		}
		if role == kernel.KindSWGH && p1.price >= p0.price {
			continue
		}
		tl := Trendline{
			Side:     side,
			P0Idx:    p0.idx,
			P0Px:     p0.price,
			P0T:      bars[p0.idx].OpenTime,
			P1Idx:    p1.idx,
			P1Px:     p1.price,
			P1T:      bars[p1.idx].OpenTime,
			FormedAt: p1.idx,
		}
		if tl.P1T <= tl.P0T {
			continue
		}
		tl.ValidAt, tl.Dead = trendlineScan(bars, tl, now)
		out = append(out, tl)
	}
	return out
}

// trendlineScan walks the closed bars after formation: the first wick touch
// is the 3rd touch (ValidAt); any closed 5m candle that closes through the
// line marks it dead.
func trendlineScan(bars []market.Kline, tl Trendline, now time.Time) (validAt int, dead bool) {
	nowMs := now.UnixMilli()
	b5 := barsTF(bars, 5)
	for _, b := range b5 {
		if b.CloseTime >= nowMs {
			continue // still forming
		}
		if b.OpenTime <= tl.P1T {
			continue // before the line was drawn
		}
		if tl.brokenBy5m(b) {
			return 0, true
		}
	}
	for i := tl.FormedAt + 1; i < len(bars); i++ {
		c := bars[i]
		if c.CloseTime == 0 || c.CloseTime >= nowMs {
			continue
		}
		if tl.touch(c) {
			return i, false
		}
	}
	return 0, false
}

// key returns the stable level key (state joins on it).
func (t Trendline) key() string {
	return fmt.Sprintf("%s:%s:%d:%d", KindTrendline, t.Side, t.P0Idx, t.P1Idx)
}

// TrendlineLevels exports each VALID, live trendline as a Level whose Price
// is the line's value at the current bar (a moving line — the touch loop
// clears its classification only on a visit departure, never on drift alone,
// item 22). A trendline is a
// LOCATION only (never a target): after the 3rd touch it enters the set; a
// 5m close through removes it. BOX BEATS TRENDLINE [DAY-3 row 27 rec
// @04:38]: a trendline whose current price sits within ±2 pts of a box edge
// is suppressed — the box wins.
func TrendlineLevels(trendlines []Trendline, boxes []Box, bars []market.Kline) []Level {
	if len(trendlines) == 0 || len(bars) == 0 {
		return nil
	}
	cur := bars[len(bars)-1]
	var out []Level
	for _, tl := range trendlines {
		if tl.ValidAt == 0 || tl.Dead {
			continue
		}
		price := tl.priceAt(cur.OpenTime)
		// BOX BEATS TRENDLINE (CTO ruling 23:10:31Z): the band is the BOX
		// ITSELF — a trendline touch inside a live box's [Bottom, Top] is not
		// a trendline location; the box governs the interior.
		if InsideAnyBox(boxes, price) {
			continue
		}
		out = append(out, Level{Key: tl.key(), Kind: KindTrendline, Price: price, AtTime: cur.OpenTime})
	}
	return out
}
