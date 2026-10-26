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
// lần thứ 3" [D3.2 p2 @03:45]; see trendlineScan) and a REJECTION: the candle's
// extreme comes to the line and it closes on the line's own side (m423). A
// break is a 5m candle WHOLLY beyond the line, body and wicks ("đóng dưới NGUYÊN
// CÂY" [X9 @06:28–06:45], owner ruling 2026-10-25); a close beyond the line
// alone is not a break; a break is NOT an entry. The line dies at day end [D4.1 p2 @01:17–01:30] — the
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
	carried  bool  // m407: latched across an intraday frame switch — already past its first test (never re-suppressed by m311)
	TFMin    int   // 0 = 1m trendline; 60/240 = PERSISTENT HTF line [D2.3 p1 @04:23–05:41]
}

// trendlineTouchBandPts is the wick-touch tolerance for a diagonal line (the
// course's literal touch [D2-40] on a sloping line needs a small band).
const trendlineTouchBandPts = 0.5

// priceAt returns the line's value at an instant (epoch ms) — a bar's open
// time for a touch, a 5m candle's close time for the break.
func (t Trendline) priceAt(tm int64) float64 {
	return t.P0Px + (t.P1Px-t.P0Px)/float64(t.P1T-t.P0T)*float64(tm-t.P0T)
}

// span returns the line's lowest and highest value over a candle's life (its
// open time to its close time). The instant of the candle's high or low is not
// on the wire, so every wick test below reads the whole interval: "beyond" means
// beyond at EVERY instant of the candle, "within the band" means within it at
// some instant.
func (t Trendline) span(c market.Kline) (lo, hi float64) {
	a := t.priceAt(c.OpenTime)
	b := a
	if c.CloseTime > c.OpenTime {
		b = t.priceAt(c.CloseTime)
	}
	return math.Min(a, b), math.Max(a, b)
}

// wickBeyond reports a candle whose extreme sits BEYOND the line by more than
// the touch band: a LOW below a support line, a HIGH above a resistance line.
func (t Trendline) wickBeyond(c market.Kline) bool {
	lo, hi := t.span(c)
	if t.Side == SideLong {
		return c.Low < lo-trendlineTouchBandPts
	}
	return c.High > hi+trendlineTouchBandPts
}

// touch reports a REJECTION of the line (m423, X9): the candle's extreme comes
// within the 0.5-pt band of the line and its CLOSE is on the line's own side —
// a low at a support line closing at or above it, a high at a resistance line
// closing at or below it. A candle whose wick goes BEYOND the band is not a
// touch (it went through the line, it did not reject it), and neither is one
// that closes through. The line is read over the candle's life, not only at its
// open (the extreme's own time is not known).
func (t Trendline) touch(c market.Kline) bool {
	lo, hi := t.span(c)
	closeLine := t.priceAt(c.CloseTime)
	if c.CloseTime <= c.OpenTime {
		closeLine = t.priceAt(c.OpenTime)
	}
	if t.Side == SideLong {
		return c.Low >= lo-trendlineTouchBandPts && c.Low <= hi+trendlineTouchBandPts && c.Close >= closeLine
	}
	return c.High <= hi+trendlineTouchBandPts && c.High >= lo-trendlineTouchBandPts && c.Close <= closeLine
}

// brokenBy5m reports a CLOSED 5m candle WHOLLY beyond the line — body AND
// wicks: the whole candle below a support line, the whole candle above a
// resistance line, judged over the candle's WHOLE LIFE (m427): the line is read at
// the candle's open time AND its close time, and the candle must be beyond BOTH —
// a support line is broken only when High < min(line@open, line@close), a
// resistance line only when Low > max(line@open, line@close). On a sloped line a
// candle that traded on the line's own side earlier in its five minutes is NOT
// wholly beyond it (a rising support 100 -> 105 with High 104 is not broken).
// Owner ruling 2026-10-25: "Đóng trên rồi đóng dưới, đóng dưới NGUYÊN CÂY luôn…
// Không đợi khung 15 phút, đợi khung 5 phút thôi" [X9 @06:28–06:45]. A close
// beyond the line with a wick still back at it is NOT a break.
// m428 (owner ruling R1, Q3 2026-10-25): the persistent 1H/4H lines (TFMin != 0)
// break the SAME way — a closed 5m candle wholly beyond the line; their old
// HTF-close break is gone.
func (t Trendline) brokenBy5m(b market.Kline) bool {
	pc := t.priceAt(b.CloseTime)
	po := t.priceAt(b.OpenTime)
	if b.CloseTime <= b.OpenTime {
		po = pc
	}
	if t.Side == SideLong {
		return b.High < math.Min(po, pc)
	}
	return b.Low > math.Max(po, pc)
}

// breakScanFrom is the open time a 5m candle must reach to be judged for the
// line's break: after the later anchor P1 for an intraday line; for a persistent
// HTF line after the whole HTF candle of P1 (the swing being built is not a
// break). m428 R1.
func (t Trendline) breakScanFrom() int64 {
	if t.TFMin != 0 {
		return t.P1T + int64(t.TFMin)*60_000
	}
	return t.P1T + 1
}

// flatNearWindow is how many closed candles of the traded frame measure the day.
const flatNearWindow = 20

// flatNearScale multiplies the near distance. Always 1 in production; the
// package's TestMain sets it to 0 so the long-standing synthetic tapes (candles a
// few points tall, swings a point or two apart) keep their premise, and the m428
// R2 pins switch it back on with withFlatNear.
var flatNearScale = 1.0

// flatNearPts is the "near" distance of owner ruling R2 (m428, Q2): HALF the
// average high-low range of the last 20 closed candles of the traded frame
// ending at frame index idx. It depends on the day — a quiet day ~4-5 pts, a
// run day 10-20 pts ("some day 10-20 points is normal near in run day"). It
// reads history up to idx only, so a pair's verdict never changes after it forms.
func flatNearPts(fb []market.Kline, idx int) float64 {
	if idx >= len(fb) {
		idx = len(fb) - 1
	}
	lo := idx - flatNearWindow + 1
	if lo < 0 {
		lo = 0
	}
	sum, n := 0.0, 0
	for i := lo; i <= idx; i++ {
		sum += fb[i].High - fb[i].Low
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n) / 2 * flatNearScale
}

// flatNearAtTime is flatNearPts for a persistent HTF line, whose anchors are HTF
// candles: the traded-frame (2m) candles that closed at or before tMs. 0 when
// the 1m window no longer reaches back that far (only an exact tie is flat).
func flatNearAtTime(bars []market.Kline, tMs int64) float64 {
	return flatNearFromB2(barsTF(bars, 2), tMs)
}

// flatNearFromB2 is flatNearAtTime over a prebuilt barsTF(bars, 2) (m434: the
// HTF build asks for every pair, and re-bucketing the whole window per pair was
// the cost). closedBucketsTF(bars, 2, tMs) is exactly b2 with its last bucket
// dropped when that bucket closes at or after tMs — reproduced here verbatim.
func flatNearFromB2(b2 []market.Kline, tMs int64) float64 {
	fr := b2
	if len(fr) > 0 && fr[len(fr)-1].CloseTime >= tMs {
		fr = fr[:len(fr)-1]
	}
	if len(fr) == 0 {
		return 0
	}
	return flatNearPts(fr, len(fr)-1)
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
// trendlineCrossed). A drawn line stays on the
// chart until a 5m candle wholly beyond it breaks it (Dead) or the session ends: "Extend right; never erase during the
// session; DELETE at end of day" [D4.1 p2 @01:17]; "nó BREAK QUA rồi thì
// MÌNH BỎ" [X9 @00:54–01:00]. The old rule kept ONE line per side — the
// newest pair replaced the older line, so 135 of 151 lines on 10-13 vanished
// without a break. Consecutive pairs never change once both swings exist
// (swings only append at the right edge), so a line, once drawn, is rebuilt
// identically on every later tick and after a restart. Only today's swings
// pair (the session-end death). The line becomes a location on the 3rd touch
// (a return, trendlineScan) and is discarded by a 5m candle wholly beyond it.
func TrendlinesBuild(bars []market.Kline, now time.Time) []Trendline {
	// This public entry is the 1m construction (display/test callers). The
	// evaluator's Tick and SeedTrendlines build through trendlinesBuildExec, which
	// reads the 2m frame after the RTH open +30 min (m407: X9 @03:35–03:49 "Thường
	// em sẽ vẽ trendline khung 2 phút").
	lines, _ := trendlinesBuild(bars, now)
	return lines
}

// trendlinesBuild is TrendlinesBuild plus the keys of the pairs it REFUSED
// because price crossed the line between its anchors (Tick counts each once
// per trading day — a drawing refusal is never silent).
func trendlinesBuild(bars []market.Kline, now time.Time) (out []Trendline, crossed []string) {
	return trendlinesBuildFrame(bars, bars, false, now)
}

// trendlinesBuildExec is the INTRADAY trendline build on the EXECUTION frame
// (m407). The mentor draws his trendlines on the 2-minute chart and trades the
// 1m only in the first 30 minutes after the 08:30 CT open:
//   - "Em lúc nào cũng prefer trade khung 2 phút … Trong dòng 30 phút đầu giờ
//     em vẫn sẽ trade khung 1 phút … sau 30 phút em sẽ chuyển qua khung 2 phút
//     em trade" [Routine trước khi trade p1 @00:00:34–00:01:14];
//   - "Thường em sẽ vẽ trendline KHUNG 2 PHÚT — nó sẽ CLEAR HƠN, tại vì MÌNH
//     TRADE khung 2 phút và khung 1 phút." [Cách vẽ trendline p1, R5, X9
//     @03:35–03:49].
//
// With the knob Exec2mAfter30m ON and the ONE schedule predicate
// exec2mReads2m(now) true (the caller passes exec2m), the swings, the slope
// rule, the not-crossed rule and the 3rd-touch return are read on CLOSED 2m
// candles; the 5m-close break and the trading-day filter are unchanged. During
// 08:30 <= t < 09:00 CT (and with the knob OFF) it is the 1m build,
// byte-identical to trendlinesBuild. The "at least 5 bars apart" spacing stays
// in BARS of the build frame — the course gives no number (open mentor
// question), none is invented. The 1H/4H builders are untouched.
func trendlinesBuildExec(bars []market.Kline, now time.Time, exec2m bool) (out []Trendline, crossed []string) {
	return trendlinesBuildExecB5(bars, barsTF(bars, 5), now, exec2m)
}

// trendlinesBuildExecB5 is trendlinesBuildExec over a prebuilt barsTF(bars, 5)
// (m434: the evaluator buckets the 5m once per Tick and shares it).
func trendlinesBuildExecB5(bars, b5 []market.Kline, now time.Time, exec2m bool) (out []Trendline, crossed []string) {
	fb, coarse := execBuildFrame(bars, now, exec2m)
	return trendlinesBuildFrameB5(bars, b5, fb, coarse, now)
}

// execBuildFrame is the traded build frame: the closed 2m buckets when exec2m
// holds (and at least 5 exist), else the 1m bars themselves. The trendline build
// and the m428 flat-pair levels read the SAME frame.
func execBuildFrame(bars []market.Kline, now time.Time, exec2m bool) (fb []market.Kline, coarse bool) {
	if exec2m {
		if frame := closedBucketsTF(bars, 2, now.UnixMilli()); len(frame) >= 5 {
			return frame, true
		}
	}
	return bars, false
}

// dropDeadTrendlines removes the lines a closed 5m candle has broken (m428 R3,
// owner 2026-10-25: "trendline gãy → xoá"): a broken line is DELETED — not drawn,
// not a level, not carried. The builders still mark Dead so the change log
// (trackTrendlines) and the carry latch can retire the line; the display and the
// level set never see it. Unbroken lines are untouched (they stay all session).
func dropDeadTrendlines(lines []Trendline) []Trendline {
	out := lines[:0:0]
	for _, tl := range lines {
		if !tl.Dead {
			out = append(out, tl)
		}
	}
	return out
}

// trendlinesBuildFrame is the construction over `fb`, the build frame: the 1m
// bars themselves, or (coarse) their closed 2m buckets. `bars` (1m) feed the 5m
// break. On a coarse frame P0Idx/P1Idx/FormedAt are indices into the FRAME and
// ValidAt is re-expressed as the 1m index of the last minute of the touching
// candle, so ValidAt == lastIdx still means "the 3rd touch JUST closed".
func trendlinesBuildFrame(bars, fb []market.Kline, coarse bool, now time.Time) (out []Trendline, crossed []string) {
	return trendlinesBuildFrameB5(bars, barsTF(bars, 5), fb, coarse, now)
}

// trendlinesBuildFrameB5 is trendlinesBuildFrame over a prebuilt b5 = barsTF(bars, 5).
func trendlinesBuildFrameB5(bars, b5, fb []market.Kline, coarse bool, now time.Time) (out []Trendline, crossed []string) {
	all := swings3(fb)
	today := tradingDayKey(now.In(ctime()))
	var seq []swingPairAt
	for _, s := range all {
		if tradingDayKey(time.UnixMilli(fb[s.idx].OpenTime).In(ctime())) == today {
			seq = append(seq, s)
		}
	}
	dayStart := len(fb)
	for i, b := range fb {
		if tradingDayKey(time.UnixMilli(b.OpenTime).In(ctime())) == today {
			dayStart = i
			break
		}
	}
	for _, role := range []kernel.LevelKind{kernel.KindSWGL, kernel.KindSWGH} {
		// STRUCTURAL swings only: confirmed two-sided (left fractal + right
		// confirmation). The D2-28 lesson — fractals pair pullback noise.
		var pts []swingPairAt
		for _, s := range seq {
			if s.kind == role && swingConfirmed(fb, s) {
				pts = append(pts, s)
			}
		}
		side := SideLong
		if role == kernel.KindSWGH {
			side = SideShort
		}
		var liveNA *Trendline // the ONE non-adjacent line of this side that still stands (m428 R5)
		st := newStandingMemo(fb, dayStart)
		for j := 1; j < len(pts); j++ {
			p1 := pts[j]
			// The adjacent pair (release #22) plus — m428 R5, owner "missing best
			// lines" redone with a TIGHT cap — at most ONE non-adjacent line per
			// side at a time: the most significant extreme (swingStanding) that
			// joins p1 with a real angle, uncrossed. While the side's earlier
			// non-adjacent line still stands (no closed 5m candle wholly beyond it
			// by p1) no second one is offered, so the side never fans.
			cands := []swingPairAt{pts[j-1]}
			naIdx := -1
			naFree := liveNA == nil || naBrokenBy(*liveNA, b5, fb[p1.idx].OpenTime)
			if naFree {
				liveNA = nil
			}
			if best, ok := bestNonAdjacentPartner(fb, pts[:j-1], p1, role, dayStart, st); ok {
				// A new non-adjacent line is offered when the side has none standing,
				// or when it CONTINUES the standing one from its end anchor (the
				// trend's next leg replaces the previous leg — still one at a time), or when
				// its partner is a MORE significant extreme than the standing line's (the
				// better line replaces the lesser one) — but only while the standing line
				// is still FORMING at that instant: a valid line (a location in use) is only
				// ever broken.
				if naFree || !validAsOf(fb, b5, all, *liveNA, fb[p1.idx].CloseTime+1) && (fb[best.idx].OpenTime == liveNA.P1T || st.of(best) > swingStanding(fb, swingPairAt{kind: role, price: liveNA.P0Px, idx: liveNA.P0Idx}, dayStart)) {
					cands = append(cands, best)
					naIdx = len(cands) - 1
				}
			}
			for ci, p0 := range cands {
				tl, ok := newSlopedLine(fb, role, side, p0, p1)
				if !ok {
					continue
				}
				if trendlineCrossed(fb, tl) {
					crossed = append(crossed, tl.key())
					continue
				}
				tl.ValidAt, tl.Dead = trendlineScanTF(fb, b5, all, tl, now)
				if coarse && tl.ValidAt > 0 {
					tl.ValidAt = oneMinIdxAtClose(bars, fb[tl.ValidAt].CloseTime)
				}
				if ci == naIdx {
					if liveNA != nil {
						out = dropLineKey(out, liveNA.key()) // the continued leg replaces the previous one
					}
					c := tl
					liveNA = &c
				}
				out = append(out, tl)
			}
		}
	}
	return out, crossed
}

// newSlopedLine builds the line for a same-role swing pair, or ok=false when
// the pair is not a trendline: at least 5 bars apart, ANGLED (m428 R2: the two
// anchors more than the day's "near" distance apart — a (nearly) flat pair is a
// support/resistance level, see flatSRLevels), sloped the right way (support = a
// HIGHER low, resistance = a LOWER high [D2.3 p1 @06:44–07:18]), and P1 after P0
// in time.
func newSlopedLine(fb []market.Kline, role kernel.LevelKind, side Side, p0, p1 swingPairAt) (Trendline, bool) {
	if p1.idx-p0.idx < 5 {
		return Trendline{}, false // at least 5 bars apart
	}
	if math.Abs(p1.price-p0.price) <= flatNearPts(fb, p1.idx) {
		return Trendline{}, false // m428 R2: a flat pair is a support/resistance, never a trendline
	}
	if role == kernel.KindSWGL && p1.price <= p0.price {
		return Trendline{}, false
	}
	if role == kernel.KindSWGH && p1.price >= p0.price {
		return Trendline{}, false
	}
	tl := Trendline{
		Side:     side,
		P0Idx:    p0.idx,
		P0Px:     p0.price,
		P0T:      fb[p0.idx].OpenTime,
		P1Idx:    p1.idx,
		P1Px:     p1.price,
		P1T:      fb[p1.idx].OpenTime,
		FormedAt: p1.idx,
	}
	if tl.P1T <= tl.P0T {
		return Trendline{}, false
	}
	return tl, true
}

// validAsOf reports whether the line was already a location (its 3rd touch, a
// return, had closed) at instant atMs — the same scan the build runs, read at
// that instant, so the answer never depends on when the build runs.
func validAsOf(fb, b5 []market.Kline, swings []swingPairAt, tl Trendline, atMs int64) bool {
	va, dead := trendlineScanTF(fb, b5, swings, tl, time.UnixMilli(atMs))
	return va > 0 && !dead
}

// dropLineKey removes the line with the given key from the list.
func dropLineKey(lines []Trendline, key string) []Trendline {
	for i, tl := range lines {
		if tl.key() == key {
			return append(lines[:i:i], lines[i+1:]...)
		}
	}
	return lines
}

// naBrokenBy reports whether a closed 5m candle wholly beyond the line had
// closed by instant atMs (the line stood until then).
func naBrokenBy(tl Trendline, b5 []market.Kline, atMs int64) bool {
	for _, b := range b5 {
		if b.CloseTime > atMs {
			break
		}
		if b.OpenTime >= tl.breakScanFrom() && tl.brokenBy5m(b) {
			return true
		}
	}
	return false
}

// swingStanding is how many frame bars (back to the trading day's first bar)
// the swing stood as the extreme before it: the bars between it and the nearest
// earlier bar that went as far or further. It reads only history before the
// swing, so it never changes once the swing exists. The significance of an
// anchor (m423 T2, redone m428 R5).
func swingStanding(fb []market.Kline, s swingPairAt, dayStart int) int {
	n := 0
	for k := s.idx - 1; k >= dayStart && k >= 0; k-- {
		if s.kind == kernel.KindSWGL && fb[k].Low <= s.price {
			break
		}
		if s.kind == kernel.KindSWGH && fb[k].High >= s.price {
			break
		}
		n++
	}
	return n
}

// bestNonAdjacentPartner picks, for the later anchor p1, the ONE earlier
// non-adjacent same-role swing (`earlier` excludes the immediately preceding
// swing, which the adjacent pair already covers) whose line is a real
// trendline — angled, 5+ bars, not crossed by any candle between its anchors —
// and that is the most significant extreme (swingStanding; the earlier one on a
// tie). Reads history up to p1 only, so a drawn line is rebuilt identically on
// every later tick. Deterministic; no slope or length number is added.
func bestNonAdjacentPartner(fb []market.Kline, earlier []swingPairAt, p1 swingPairAt, role kernel.LevelKind, dayStart int, st *standingMemo) (swingPairAt, bool) {
	side := SideLong
	if role == kernel.KindSWGH {
		side = SideShort
	}
	// m434: the pick is the valid partner with the HIGHEST standing, the
	// earliest on a tie. Standing is memoised and compared FIRST, so the O(bars)
	// crossed scan runs only for a candidate that could still take the lead
	// (strictly more standing than the best valid one so far) — the same pick as
	// scoring every candidate.
	var best swingPairAt
	bestScore, found := -1, false
	for _, p0 := range earlier {
		sc := st.of(p0)
		if sc <= bestScore {
			continue
		}
		tl, ok := newSlopedLine(fb, role, side, p0, p1)
		if !ok || trendlineCrossed(fb, tl) {
			continue
		}
		best, bestScore, found = p0, sc, true
	}
	return best, found
}

// standingMemo caches swingStanding per swing of ONE role within ONE build: a
// swing's standing reads only history before it (never changes), but the
// partner pick asks for every earlier swing again at every later anchor.
type standingMemo struct {
	fb       []market.Kline
	dayStart int
	v        []int // by swing idx; 0 = not yet computed, else standing+1
}

func newStandingMemo(fb []market.Kline, dayStart int) *standingMemo {
	return &standingMemo{fb: fb, dayStart: dayStart, v: make([]int, len(fb))}
}

func (m *standingMemo) of(s swingPairAt) int {
	if s.idx < 0 || s.idx >= len(m.v) {
		return swingStanding(m.fb, s, m.dayStart)
	}
	if n := m.v[s.idx]; n != 0 {
		return n - 1
	}
	n := swingStanding(m.fb, s, m.dayStart)
	m.v[s.idx] = n + 1
	return n
}

// oneMinIdxAtClose returns the index of the last 1m bar closing at or before
// closeMs — the last minute of a coarser candle. Never 0 (0 means "not valid").
func oneMinIdxAtClose(bars []market.Kline, closeMs int64) int {
	idx := 1
	for i, b := range bars {
		if b.CloseTime <= closeMs {
			idx = i
		}
	}
	return idx
}

// trendlinesBuildTF builds PERSISTENT higher-timeframe swing-pair trendlines
// [D2.3 p1 @04:23–05:41]. Same construction as the 1m lines, except: the swing
// pairs are found on the bucketed HTF bars (barsTF), there is NO today filter
// (the lines survive the session end), and the break is read on the HTF close
// (trendlineScanTF walks the HTF bars as both the bars and the break candles).
func trendlinesBuildTF(bars []market.Kline, tfMin int, now time.Time) []Trendline {
	return trendlinesBuildTFFrames(bars, barsTF(bars, tfMin), barsTF(bars, 5), tfMin, now)
}

// trendlinesBuildTFFrames is trendlinesBuildTF over the prebuilt buckets
// btf = barsTF(bars, tfMin) and b5 = barsTF(bars, 5) (m434: the evaluator
// buckets each frame once per Tick; b5 is the m428 R1 5m break scan).
func trendlinesBuildTFFrames(bars, btf, b5 []market.Kline, tfMin int, now time.Time) []Trendline {
	if len(btf) < 5 {
		return nil
	}
	all := swings3(btf)
	var b2 []market.Kline // barsTF(bars, 2), built at most once, only if a pair reaches the flat test
	var out []Trendline
	for _, role := range []kernel.LevelKind{kernel.KindSWGL, kernel.KindSWGH} {
		var pts []swingPairAt
		for _, s := range all {
			if s.kind == role && swingConfirmed(btf, s) {
				pts = append(pts, s)
			}
		}
		side := SideLong
		if role == kernel.KindSWGH {
			side = SideShort
		}
		for j := 1; j < len(pts); j++ {
			p0, p1 := pts[j-1], pts[j]
			if p1.idx-p0.idx < 2 {
				continue // same-role swings are ≥2 HTF bars apart; refuse anything tighter
			}
			// must slope: support = a HIGHER low; resistance = a LOWER high.
			if role == kernel.KindSWGL && p1.price <= p0.price {
				continue
			}
			if role == kernel.KindSWGH && p1.price >= p0.price {
				continue
			}
			// m428 R2: a (nearly) flat HTF pair is no trendline either; "near" is
			// read on the traded frame at the later anchor's time.
			if b2 == nil {
				b2 = barsTF(bars, 2)
			}
			if math.Abs(p1.price-p0.price) <= flatNearFromB2(b2, btf[p1.idx].OpenTime) {
				continue
			}
			tl := Trendline{
				Side:     side,
				P0Idx:    p0.idx,
				P0Px:     p0.price,
				P0T:      btf[p0.idx].OpenTime,
				P1Idx:    p1.idx,
				P1Px:     p1.price,
				P1T:      btf[p1.idx].OpenTime,
				FormedAt: p1.idx,
				TFMin:    tfMin,
			}
			if tl.P1T <= tl.P0T {
				continue
			}
			if trendlineCrossed(btf, tl) {
				continue
			}
			tl.ValidAt, tl.Dead = trendlineScanTF(btf, b5, all, tl, now)
			out = append(out, tl)
		}
	}
	return out
}

// trendlineHTFBroken reports whether a persisted HTF line is now broken — a
// closed 5m candle WHOLLY beyond it (m428 R1, owner Q3 2026-10-25: the same
// break as the intraday line) since the line was drawn. The scan is purely by
// TIME: every closed 5m candle after the line's P1 candle is tested. A line in
// evaluator state was unbroken on the previous tick, so this only needs to see
// the candles the current window can supply.
func trendlineHTFBroken(tl Trendline, b5 []market.Kline, now time.Time) bool {
	nowMs := now.UnixMilli()
	from := tl.breakScanFrom()
	for _, b := range b5 {
		if b.CloseTime >= nowMs || b.OpenTime < from {
			continue // still forming, or before the line was drawn
		}
		if tl.brokenBy5m(b) {
			return true
		}
	}
	return false
}

// trendlineHTFTouch returns a non-zero value once price has LEFT a persisted
// HTF line and come BACK to touch it — the 3rd touch that makes it a location
// [D3.2 p2 @03:45] — read on the line's own timeframe, purely by TIME. A line
// that is already valid never calls this (validity is latched in state).
func trendlineHTFTouch(tl Trendline, btf []market.Kline, now time.Time) int {
	nowMs := now.UnixMilli()
	all := swings3(btf)
	leave := -1
	for _, s := range all {
		if btf[s.idx].OpenTime <= tl.P1T || s.idx+1 >= len(btf) {
			continue
		}
		if btf[s.idx+1].CloseTime == 0 || btf[s.idx+1].CloseTime >= nowMs {
			continue // the confirming bar has not closed yet
		}
		if !swingConfirmed(btf, s) {
			continue
		}
		p := tl.priceAt(btf[s.idx].OpenTime)
		if tl.Side == SideLong && s.kind == kernel.KindSWGH && s.price > p+trendlineTouchBandPts {
			leave = s.idx
			break
		}
		if tl.Side == SideShort && s.kind == kernel.KindSWGL && s.price < p-trendlineTouchBandPts {
			leave = s.idx
			break
		}
	}
	if leave < 0 {
		return 0 // price never left the line — no return, not a location
	}
	for i := leave + 1; i < len(btf); i++ {
		c := btf[i]
		if c.CloseTime == 0 || c.CloseTime >= nowMs {
			continue
		}
		if tl.touch(c) {
			return i
		}
	}
	return 0
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
	// m434: the hot loop of the build (every candidate pair of every Tick runs
	// it). Same arithmetic as tl.wickBeyond(bars[i]) — priceAt's slope is hoisted
	// out of the loop (priceAt = P0Px + (P1Px-P0Px)/dT*dt is evaluated
	// slope-first, so the hoisted value is the identical float) and the
	// math.Min/Max of span becomes a compare. m434TickOutputIsByteIdentical and
	// TestM434CrossedMatchesWickBeyond pin it to the unhoisted form.
	slope := (tl.P1Px - tl.P0Px) / float64(tl.P1T-tl.P0T)
	long := tl.Side == SideLong
	for i := tl.P0Idx + 1; i < tl.P1Idx && i < len(bars); i++ {
		c := &bars[i]
		a := tl.P0Px + slope*float64(c.OpenTime-tl.P0T)
		b := a
		if c.CloseTime > c.OpenTime {
			b = tl.P0Px + slope*float64(c.CloseTime-tl.P0T)
		}
		if long {
			lo := a
			if b < lo {
				lo = b
			}
			if c.Low < lo-trendlineTouchBandPts {
				return true
			}
		} else {
			hi := a
			if b > hi {
				hi = b
			}
			if c.High > hi+trendlineTouchBandPts {
				return true
			}
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
		if b.OpenTime < tl.breakScanFrom() {
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
// the trendline is refused [D2.3 p1 @02:19–03:45]. m311 (#46b): for the intraday
// (1m) line this is consulted ONLY on the line's FIRST test — the 3rd touch that
// JUST closed (ValidAt == the current bar) — a valid line past its 3rd-touch
// return has itself been respected and is no longer suppressed (see
// TrendlineLevels). FORMING lines never enter here: they stay non-locations
// (the 3rd-touch return rule is unchanged).
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
// same price also beats the trendline [D2.3 p1 @02:19–03:45] — but (m311, #46b)
// ONLY on the trendline's FIRST test: a valid line past its 3rd-touch return
// has itself been respected and is no longer suppressed. Its key is returned
// in refused so the caller counts tl_first_test_level_preferred.
func TrendlineLevels(trendlines []Trendline, boxes []Box, respected []Level, bars []market.Kline) (levels []Level, refused []string) {
	if len(trendlines) == 0 || len(bars) == 0 {
		return nil, nil
	}
	cur := bars[len(bars)-1]
	lastIdx := len(bars) - 1
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
		// m311 (#46b): the level beats an intraday (1m) trendline only on its
		// FIRST test — the 3rd touch that just closed (ValidAt == the current
		// bar). A valid line past its 3rd-touch return is no longer suppressed
		// [D2.3 p1 @02:19–03:45]. Persistent HTF lines keep the preference
		// unchanged (their ValidAt is an HTF-bar index, not comparable here).
		if (tl.TFMin != 0 || (!tl.carried && tl.ValidAt == lastIdx)) && trendlineBeatenByLevel(respected, price) {
			refused = append(refused, tl.key())
			continue
		}
		out = append(out, Level{Key: tl.key(), Kind: KindTrendline, Price: price, AtTime: cur.OpenTime})
	}
	return out, refused
}
