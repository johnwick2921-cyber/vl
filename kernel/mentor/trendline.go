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
// line (the lowest confirmed swing low + the nearest LATER higher low) and
// one resistance line (the highest confirmed swing high + the nearest LATER
// lower high). Only today's swings survive. The line is drawn when P1 closes
// and extended right; it becomes a location on the 3rd touch and is
// discarded by a 5m close through.
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
		extreme := -1
		for i, s := range seq {
			if s.kind != role || !swingConfirmed(bars, s) {
				continue
			}
			if extreme < 0 {
				extreme = i
				continue
			}
			if role == kernel.KindSWGL && s.price < seq[extreme].price {
				extreme = i
			}
			if role == kernel.KindSWGH && s.price > seq[extreme].price {
				extreme = i
			}
		}
		if extreme < 0 {
			continue
		}
		nearest := -1
		for i, s := range seq {
			if i == extreme || s.kind != role || s.idx <= seq[extreme].idx {
				continue
			}
			// the later swing must form the slope: a HIGHER low (support)
			// or a LOWER high (resistance). Equal = horizontal = refused.
			if role == kernel.KindSWGL && s.price <= seq[extreme].price {
				continue
			}
			if role == kernel.KindSWGH && s.price >= seq[extreme].price {
				continue
			}
			if nearest < 0 || s.idx < seq[nearest].idx {
				nearest = i
			}
		}
		if nearest < 0 {
			continue
		}
		side := SideLong
		if role == kernel.KindSWGH {
			side = SideShort
		}
		tl := Trendline{
			Side:     side,
			P0Idx:    seq[extreme].idx,
			P0Px:     seq[extreme].price,
			P0T:      bars[seq[extreme].idx].OpenTime,
			P1Idx:    seq[nearest].idx,
			P1Px:     seq[nearest].price,
			P1T:      bars[seq[nearest].idx].OpenTime,
			FormedAt: seq[nearest].idx,
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
// is the line's value at the current bar (a moving line — the touch loop's
// freshTouch resets the classification as the line drifts). A trendline is a
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
		if nearBoxEdge(boxes, price) {
			continue // box beats trendline
		}
		out = append(out, Level{Key: tl.key(), Kind: KindTrendline, Price: price, AtTime: cur.OpenTime})
	}
	return out
}

// nearBoxEdge reports whether a price sits within one trendline touch band
// of a box edge — the SAME spot. BOX BEATS TRENDLINE [DAY-3 row 27 rec
// @04:38] only where the two coincide; a trendline sloping away from the box
// is a separate location.
func nearBoxEdge(boxes []Box, price float64) bool {
	for _, b := range boxes {
		if abs(price-b.Top) <= trendlineTouchBandPts || abs(price-b.Bottom) <= trendlineTouchBandPts {
			return true
		}
	}
	return false
}
