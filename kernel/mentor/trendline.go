package mentor

import (
	"fmt"
	"math"
	"time"

	"vl/kernel"
	"vl/market"
)

// Trendline — X9 (slide 27) + D2.3 + DAY-3 row 27. A trendline joins two
// same-role swing extremes: a low and a HIGHER low (support, SideLong), or a
// high and a LOWER high (resistance, SideShort). It must slope — never
// horizontal [D2.3 p1 @06:44–07:18]. Two points = EXISTS; a 3rd touch makes
// it VALID (a location) [DAY-3 p2 @03:45–04:07] — and that 3rd touch is a
// RETURN: price must LEAVE the line and come BACK to it ("nó phải VỀ nó test
// lần thứ 3" [D3.2 p2 @03:45]; see trendlineScan). A break confirmed by a 5m
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
	Dead     bool  // a closed 5m candle CLOSED through the line (brokenBy5m)
}

// trendlineTouchBandPts is the wick-touch tolerance for a diagonal line (the
// course's literal touch [D2-40] on a sloping line needs a small band).
const trendlineTouchBandPts = 0.5

// priceAt returns the line's value at an instant (epoch ms) — a bar's open
// time for a touch, a 5m candle's close time for the break.
func (t Trendline) priceAt(tm int64) float64 {
	return t.P0Px + (t.P1Px-t.P0Px)/float64(t.P1T-t.P0T)*float64(tm-t.P0T)
}

// touch reports a wick touch of the line at the candle's open time.
func (t Trendline) touch(c market.Kline) bool {
	p := t.priceAt(c.OpenTime)
	return c.High >= p-trendlineTouchBandPts && c.Low <= p+trendlineTouchBandPts
}

// brokenBy5m reports a CLOSED 5m candle whose CLOSE is through the line (the
// break that discards it): support broken by a close BELOW the line, resistance
// by a close ABOVE it. The course's test is the close: "Em hỏi là bây giờ nến
// nó đụng level mà nó ĐÓNG DƯỚI level — đó đụng TRENDLINE nó thể hiện việc gì?
// … Nó GÃY level… Ngay đây nó chỉ gãy thôi chứ không phải đảo chiều… không thể
// nào nó vừa đóng dưới level là nó đảo chiều được" [D2.3 p1 @14:36–15:17];
// "đợi khung 5 phút đóng" [X9 @06:25] picks the 5m. X9's stricter "đóng dưới
// NGUYÊN CÂY" (the whole candle) is an older source the Sep-26 course
// supersedes — not adopted (CTO fold on #520, 2026-10-14).
//
// The line value is read at the candle's CLOSE time — the instant the close
// is judged. The pre-10-14 read took it at the bucket's OPEN, five minutes of
// slope early: on a rising support line that under-read the line, so a close
// already below it at the close was not counted.
func (t Trendline) brokenBy5m(b market.Kline) bool {
	p := t.priceAt(b.CloseTime)
	if t.Side == SideLong {
		return b.Close < p
	}
	return b.Close > p
}

// TrendlinesBuild is the pure, deterministic trendline scan. The points are
// STRUCTURAL swings (CTO ruling 23:10:31Z, item 12): a point must be a
// CONFIRMED two-sided swing — swings3's left 3-bar fractal plus the right-side
// confirmation (swingConfirmed). Raw one-sided 3-bar fractals are pullback
// noise and never pair a line.
//
// Release #22 (owner ruling R4, course D4.1): EVERY consecutive pair of
// today's qualifying same-role swings draws a line — at least 5 bars apart,
// sloped, and NOT crossed by price between its own anchors (see
// trendlineCrossed). A drawn line stays on the chart until a 5m close breaks
// it (Dead) or the session ends: "Extend right; never erase during the
// session; DELETE at end of day" [D4.1 p2 @01:17]; "nó BREAK QUA rồi thì
// MÌNH BỎ" [X9 @00:54–01:00]. The old rule kept ONE line per side — the
// newest pair replaced the older line, so 135 of 151 lines on 10-13 vanished
// without a break. Consecutive pairs never change once both swings exist
// (swings only append at the right edge), so a line, once drawn, is rebuilt
// identically on every later tick and after a restart. Only today's swings
// pair (the session-end death). The line becomes a location on the 3rd touch
// (a return, trendlineScan) and is discarded by a 5m close through.
func TrendlinesBuild(bars []market.Kline, now time.Time) []Trendline {
	lines, _ := trendlinesBuild(bars, now)
	return lines
}

// trendlinesBuild is TrendlinesBuild plus the keys of the pairs it REFUSED
// because price crossed the line between its anchors (Tick counts each once
// per trading day — a drawing refusal is never silent).
func trendlinesBuild(bars []market.Kline, now time.Time) (out []Trendline, crossed []string) {
	all := swings3(bars)
	today := tradingDayKey(now.In(ctime()))
	var seq []swingPairAt
	for _, s := range all {
		if tradingDayKey(time.UnixMilli(bars[s.idx].OpenTime).In(ctime())) == today {
			seq = append(seq, s)
		}
	}
	b5 := barsTF(bars, 5)
	for _, role := range []kernel.LevelKind{kernel.KindSWGL, kernel.KindSWGH} {
		// STRUCTURAL swings only: confirmed two-sided (left fractal + right
		// confirmation). The D2-28 lesson — fractals pair pullback noise.
		var pts []swingPairAt
		for _, s := range seq {
			if s.kind == role && swingConfirmed(bars, s) {
				pts = append(pts, s)
			}
		}
		side := SideLong
		if role == kernel.KindSWGH {
			side = SideShort
		}
		for j := 1; j < len(pts); j++ {
			p0, p1 := pts[j-1], pts[j]
			if p1.idx-p0.idx < 5 {
				continue // at least 5 bars apart
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
			if trendlineCrossed(bars, tl) {
				crossed = append(crossed, tl.key())
				continue
			}
			tl.ValidAt, tl.Dead = trendlineScanTF(bars, b5, all, tl, now)
			out = append(out, tl)
		}
	}
	return out, crossed
}

// trendlineCrossed reports a line that price CROSSED between its own anchors:
// a 1m candle strictly between P0 and P1 whose wick sits beyond the line by
// more than the line's own touch band — a LOW below a support line, a HIGH
// above a resistance line. Such a line does not join the real swing points;
// it is drawn through the candles: "TRENDLINE chứ không phải CẦU TUỘT… CHỨ
// KHÔNG PHẢI LÀ MÌNH THÍCH VẼ ĐÂU MÌNH VẼ" [X9 @01:22–02:10]; "nó BREAK QUA
// rồi thì MÌNH BỎ" [X9 @00:54–01:00]; "try drawing the trendline — if it
// doesn't look right, it isn't one … if it isn't clean, use the box"
// [D4.1 p1 @24:02; p2 @00:17]. 40 of 151 lines on 10-13 had such a candle.
func trendlineCrossed(bars []market.Kline, tl Trendline) bool {
	for i := tl.P0Idx + 1; i < tl.P1Idx && i < len(bars); i++ {
		p := tl.priceAt(bars[i].OpenTime)
		if tl.Side == SideLong && bars[i].Low < p-trendlineTouchBandPts {
			return true
		}
		if tl.Side == SideShort && bars[i].High > p+trendlineTouchBandPts {
			return true
		}
	}
	return false
}

// trendlineScanTF walks the closed bars after formation: any closed 5m candle
// that closes through the line (brokenBy5m) marks it dead, and the 3rd touch (ValidAt) is
// a RETURN — "nó phải VỀ nó test lần thứ 3 thì mới valid" [D3.2 p2 @03:45];
// "Nó chưa VỀ test một lần nào" [D3.2 p2 @00:52–01:44]; "Nếu như nó VỀ đây, nó
// test cái trendline một lần rồi…" [D2.3 p1 @02:34–03:16]. "Về" = price comes
// BACK. The bars right after P1 — P1's own confirmation bar and whatever still
// sits on the line — are the swing being built, not a return (the CTO probe,
// 10-13: 55 of 120 lines went VALID on the bar right after P1, median +2).
//
// So a touch counts only AFTER price has LEFT the line: a CONFIRMED
// opposite-role swing on the line's far side, beyond the touch band (see
// trendlineLeaveIdx). The touch scan starts on the bar after that swing, which
// is always after P1's confirmation bar (P1Idx+1). Structural — no distance
// number; the same "separated by a real swing of the other role" the box
// re-read asks of a box partner [CTO-REREAD-1004 DAY-3 row 12].
//
// b5 is barsTF(bars, 5), built once by the caller (every line of a tick reads
// the same 5m buckets).
func trendlineScanTF(bars, b5 []market.Kline, swings []swingPairAt, tl Trendline, now time.Time) (validAt int, dead bool) {
	nowMs := now.UnixMilli()
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
	leave := trendlineLeaveIdx(bars, swings, tl, nowMs)
	if leave < 0 {
		return 0, false // price never left the line — no return, not a location
	}
	for i := leave + 1; i < len(bars); i++ {
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

// trendlineLeaveIdx returns the bar index of the FIRST confirmed opposite-role
// swing after P1 whose extreme is OFF the line on its far side — a swing HIGH
// above a support line (SideLong), a swing LOW below a resistance line
// (SideShort), by more than the line's own touch band (an extreme still inside
// the band never left the line). Confirmed = swingConfirmed's right side, with
// the confirming bar CLOSED before now. -1 = price has not left the line yet.
func trendlineLeaveIdx(bars []market.Kline, swings []swingPairAt, tl Trendline, nowMs int64) int {
	for _, s := range swings {
		if s.idx <= tl.P1Idx || s.idx+1 >= len(bars) {
			continue
		}
		if bars[s.idx+1].CloseTime == 0 || bars[s.idx+1].CloseTime >= nowMs {
			continue // the confirming bar has not closed yet
		}
		if !swingConfirmed(bars, s) {
			continue
		}
		p := tl.priceAt(bars[s.idx].OpenTime)
		if tl.Side == SideLong && s.kind == kernel.KindSWGH && s.price > p+trendlineTouchBandPts {
			return s.idx
		}
		if tl.Side == SideShort && s.kind == kernel.KindSWGL && s.price < p-trendlineTouchBandPts {
			return s.idx
		}
	}
	return -1
}

// key returns the stable level key (state joins on it). UR-FIX U2: keyed on
// the swing OpenTimes (P0T/P1T), NOT the bar indices — the live window slides
// (the provider returns the last 2000 bars), so P0Idx/P1Idx shift by 1 each
// new 1m bar and a key that embedded them changed every tick, so per-level
// state (Visits, LevelArms, ISBOnly, VisitCapRefused) never carried over.
func (t Trendline) key() string {
	return fmt.Sprintf("%s:%s:%d:%d", KindTrendline, t.Side, t.P0T, t.P1T)
}

// trendlineLevelPreferPts is the band within which a respected level beats a
// fresh trendline (D2.3 p1 @02:19–03:45: "chọn cái mà nó đã respect nhiều lần …
// đừng chọn cái nó mới tạo"). The mentor gives no number; 2 pts matches the old
// "±2 pts of a box edge" note.
const trendlineLevelPreferPts = 2.0

// trendlineBeatenByLevel reports whether a respected LEVEL (a key level or an
// old high/low) sits at/near the trendline's current price — the level wins and
// the trendline is refused [D2.3 p1 @02:19–03:45]. FORMING lines never enter
// here: they stay non-locations (the 3rd-touch return rule is unchanged).
func trendlineBeatenByLevel(respected []Level, price float64) bool {
	for _, lv := range respected {
		switch lv.Kind {
		case KindKeyLevel, KindOldExtreme:
			if math.Abs(lv.Price-price) <= trendlineLevelPreferPts {
				return true
			}
		}
	}
	return false
}

// TrendlineLevels exports each VALID, live trendline as a Level whose Price
// is the line's value at the current bar (a moving line — the touch loop
// clears its classification only on a visit departure, never on drift alone,
// item 22). A trendline is a
// LOCATION only (never a target): after the 3rd touch it enters the set; a
// 5m close through removes it. BOX BEATS TRENDLINE [DAY-3 row 27 rec
// @04:38]: a trendline whose current price sits within a live box is
// suppressed — the box wins. A RESPECTED LEVEL (key level / old extreme) at the
// same price also beats the trendline [D2.3 p1 @02:19–03:45]; its key is
// returned in refused so the caller counts tl_first_test_level_preferred.
func TrendlineLevels(trendlines []Trendline, boxes []Box, respected []Level, bars []market.Kline) (levels []Level, refused []string) {
	if len(trendlines) == 0 || len(bars) == 0 {
		return nil, nil
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
		if trendlineBeatenByLevel(respected, price) {
			refused = append(refused, tl.key())
			continue
		}
		out = append(out, Level{Key: tl.key(), Kind: KindTrendline, Price: price, AtTime: cur.OpenTime})
	}
	return out, refused
}
