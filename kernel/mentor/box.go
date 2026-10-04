package mentor

import (
	"time"

	"vl/kernel"
	"vl/market"
)

// §4.1 — FTGH / FTGL boxes [D3.2 p1 @ 01:21–04:25; D3.2 p2 @ 03:45–09:14],
// with the BOX RULING parts 1+2 (CTO 2026-10-03, verified against the course
// frame D3.3 FTGH/FTGL part1_06-25.jpg):
//
//   - Pair by ROLE — lows with lows (floor), highs with highs (ceiling) —
//     with NO price tolerance. A box is drawn the moment price approaches
//     an old extreme and FAILS to break it; the pairing is the extreme
//     (lowest low / highest high) with the NEAREST same-role extreme.
//   - "Mình lấy cái râu thấp nhất… của cái đáy trước đó… kéo lên tới… cái
//     BODY của cái đáy GẦN NHẤT" [Cách vẽ box @ 02:16–02:41] — FTGL: from
//     the LOWEST WICK of the earlier low up to the BODY of the nearest low.
//   - "râu ở cái đỉnh cao nhất, và cái BODY của cái đỉnh GẦN NHẤT"
//     [@ 03:29] — FTGH: from the HIGHEST WICK of the earlier high down to
//     the BODY of the nearest high.
//   - Role detection [B]: a rolling 3-candle extreme — a low is a candle
//     whose low is the min of the last three lows; a high, the max of the
//     last three highs. This reproduces BOTH golden boxes on the 13-Sep
//     frame (±1 pt).
//   - Draw at once and EXTEND RIGHT. The box is reused indefinitely
//     [D3.2 p2 @ 06:21]. Valid by construction — no 3rd-touch TEST for the
//     box [@ 03:45]; the 3rd-touch rule belongs to the TRADE [@ 04:29],
//     exposed as Box.Touches.
//   - Deleted only on escape (a BODY closes outside) [D3.2 p1 @ 19:05;
//     D3.4 p2 @ 12:11] or at the end of the day [D4.1 p2 @ 02:39–03:31].
//   - "NEVER trade inside the box" [D3.2 p1 @ 06:59] → InsideAnyBox.
//   - "Draw TWO zones, never a third." [D3.2 p2 @ 09:14] → one floor +
//     one ceiling.
//   - TF: 1m REGULAR by default (the course frame outranks the 2025
//     extras); "2m" regular and "5mha" (5m Heikin Ashi) stay as knobs,
//     default off.

// BoxKind is which failure the zone names.
type BoxKind string

const (
	// FTGH — failure to go higher (ceiling; green→red).
	FTGH BoxKind = "ftgh"
	// FTGL — failure to go lower (floor; red→green).
	FTGL BoxKind = "ftgl"
)

// Level kinds for DS-103's location gate (box edges are one of the four
// setup locations).
const (
	KindFTGHEdge LevelKind = "ftgh_edge"
	KindFTGLEdge LevelKind = "ftgl_edge"
)

// Box is one drawn FTGH/FTGL zone. Never redrawn: once built, the edges do
// not move. Escaped boxes are deleted (dropped from the set).
type Box struct {
	Kind   BoxKind
	Top    float64 // FTGH: highest wick of the earlier high · FTGL: body of the nearest low
	Bottom float64 // FTGH: body of the nearest high · FTGL: lowest wick of the earlier low
	Key    string  // stable id "ftgh:<top>:<bottom>"
	// Touches counts closed candles whose wick reached an edge since the
	// box formed — the entry layer requires the THIRD touch [@ 04:29].
	Touches int
	// FormedAt is the bar index the box was drawn (extend-right origin):
	// max(nearest idx, extreme idx + 1) — born when the extreme's confirming
	// bar closes, and never earlier than the later pairing candle, so a
	// pairing candle is never walked as a return (B10 T1 ruling + T3).
	FormedAt int
}

// BoxCfg holds the §4.1 knobs.
type BoxCfg struct {
	// TF is the box timeframe: "1m" (default, the course frame), "2m"
	// (2025 extra), "5mha" (5m Heikin Ashi, 2025 extra). Default off for
	// the non-course options.
	TF string
	// TouchBandPts: a wick within this of an edge counts as a touch;
	// default 0.25.
	TouchBandPts float64
}

// DefaultBoxCfg returns the ruling defaults: 1m regular.
func DefaultBoxCfg() BoxCfg {
	return BoxCfg{TF: "1m", TouchBandPts: 0.25}
}

// swingPairAt is one detected role extreme with its defining bar index.
type swingPairAt struct {
	kind  kernel.LevelKind
	price float64
	idx   int
}

// HeikinAshi converts regular OHLC bars to Heikin Ashi (the 2025-extra box
// TF option): HAclose = (O+H+L+C)/4; HAopen = (prev HAopen + prev HAclose)/2
// (first HAopen = (O+C)/2); HAhigh = max(H, HAopen, HAclose);
// HAlow = min(L, HAopen, HAclose).
func HeikinAshi(bars []market.Kline) []market.Kline {
	out := make([]market.Kline, 0, len(bars))
	var prevOpen, prevClose float64
	for i, b := range bars {
		haClose := (b.Open + b.High + b.Low + b.Close) / 4
		haOpen := (b.Open + b.Close) / 2
		if i > 0 {
			haOpen = (prevOpen + prevClose) / 2
		}
		haHigh := b.High
		if haOpen > haHigh {
			haHigh = haOpen
		}
		if haClose > haHigh {
			haHigh = haClose
		}
		haLow := b.Low
		if haOpen < haLow {
			haLow = haOpen
		}
		if haClose < haLow {
			haLow = haClose
		}
		out = append(out, market.Kline{OpenTime: b.OpenTime, Open: haOpen, High: haHigh, Low: haLow, Close: haClose, CloseTime: b.CloseTime})
		prevOpen, prevClose = haOpen, haClose
	}
	return out
}

// barsForTF resolves the box timeframe from the 1m series: "1m" passes
// through; "2m" aggregates; "5mha" aggregates 5m then converts to Heikin
// Ashi.
func barsForTF(bars []market.Kline, tf string) []market.Kline {
	switch tf {
	case "2m":
		return barsTF(bars, 2)
	case "5mha":
		return HeikinAshi(barsTF(bars, 5))
	default:
		return bars
	}
}

// swings3 is the role detector [B]: a rolling 3-candle extreme — candle i is
// a LOW when its low is the min of the last three lows, a HIGH when its high
// is the max of the last three highs. LEFT-only, deliberately: the NEAREST
// pairing swing keeps no right-side confirmation (B10 T1 ruling 21:00Z);
// only the EXTREME is confirmed, by swingConfirmed, at the pairing site.
func swings3(bars []market.Kline) []swingPairAt {
	var seq []swingPairAt
	for i := 2; i < len(bars); i++ {
		if bars[i].CloseTime == 0 {
			continue
		}
		lo, hi := bars[i-2].Low, bars[i-2].High
		for j := i - 1; j <= i; j++ {
			if bars[j].Low < lo {
				lo = bars[j].Low
			}
			if bars[j].High > hi {
				hi = bars[j].High
			}
		}
		switch {
		case bars[i].Low == lo && bars[i].Low < bars[i-1].Low && bars[i].Low < bars[i-2].Low:
			seq = append(seq, swingPairAt{kind: kernel.KindSWGL, price: bars[i].Low, idx: i})
		case bars[i].High == hi && bars[i].High > bars[i-1].High && bars[i].High > bars[i-2].High:
			seq = append(seq, swingPairAt{kind: kernel.KindSWGH, price: bars[i].High, idx: i})
		}
	}
	return seq
}

// swingConfirmed is the B10 T1 fractal right side (CTO ruling 21:00Z),
// applied to the EXTREME only: a swing low is the extreme only when the NEXT
// closed bar has a HIGHER low (the spike failed to go lower); a swing high,
// only when the NEXT closed bar has a LOWER high. A one-way tape never
// confirms, so no extreme and no box. The box is BORN when the confirming
// bar closes.
func swingConfirmed(bars []market.Kline, s swingPairAt) bool {
	if s.idx+1 >= len(bars) || bars[s.idx+1].CloseTime == 0 {
		return false
	}
	if s.kind == kernel.KindSWGL {
		return bars[s.idx+1].Low > s.price
	}
	return bars[s.idx+1].High < s.price
}

// BoxesBuild is the pure, deterministic box scan: role extremes pair with
// the NEAREST same-role extreme (in time, on the non-broken side) with NO
// tolerance; escaped boxes are dropped; only boxes of the current trading
// day survive. The same history always rebuilds the same set — a drawn box
// is never redrawn. At most one floor and one ceiling ("two zones").
func BoxesBuild(bars []market.Kline, cfg BoxCfg, now time.Time) []Box {
	if cfg.TF == "" {
		cfg = DefaultBoxCfg()
	}
	tfBars := barsForTF(bars, cfg.TF)
	seq := swings3(tfBars)
	today := tradingDayKey(now.In(ctime()))
	// B10 T2 (CTO 2026-10-03): pick the extreme among TODAY's swings only.
	// The slice is 1500 1m bars since A9; pairing against a yesterday extreme
	// and dropping the box at the day check left a day whose low sits above
	// yesterday's with no floor at all.
	var seqToday []swingPairAt
	for _, s := range seq {
		if tradingDayKey(time.UnixMilli(tfBars[s.idx].OpenTime).In(ctime())) == today {
			seqToday = append(seqToday, s)
		}
	}
	seq = seqToday
	var out []Box
	for _, role := range []kernel.LevelKind{kernel.KindSWGL, kernel.KindSWGH} {
		extreme := -1
		for i, s := range seq {
			if s.kind != role {
				continue
			}
			// B10 T1 (CTO ruling 21:00Z): only a CONFIRMED swing can be the
			// extreme — the next closed bar failed to break it. A one-way
			// tape has no confirmed swing, so no extreme and no box.
			if !swingConfirmed(tfBars, s) {
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
			// the NEAREST keeps no confirmation (B10 T1 ruling): the
			// left-only swing nearest in time that did not break the extreme.
			if i == extreme || s.kind != role {
				continue
			}
			// the nearest did not break the extreme
			if role == kernel.KindSWGL && s.price < seq[extreme].price {
				continue
			}
			if role == kernel.KindSWGH && s.price > seq[extreme].price {
				continue
			}
			if nearest < 0 {
				nearest = i
				continue
			}
			di := seq[i].idx - seq[extreme].idx
			if di < 0 {
				di = -di
			}
			dn := seq[nearest].idx - seq[extreme].idx
			if dn < 0 {
				dn = -dn
			}
			if di < dn {
				nearest = i
			}
		}
		if nearest < 0 {
			continue
		}
		b := boxFromPair(tfBars, seq[extreme], seq[nearest])
		if b == nil {
			continue
		}
		b.Touches = countBoxTouches(tfBars, *b, seq[extreme].idx, cfg)
		// B4 (10-03 ruling, PLAN.md: "NEVER deleted during the session… delete
		// at the end of the day" [D4.1 p2 @02:39–03:09]): an escaped body does
		// NOT delete the box — v3's "delete on escape" mixed in the 5m ISB box
		// rule and killed every box trade. The today-only candidate filter
		// above is the box's only death.
		b.Key = "ftgh:" + fnum(b.Top) + ":" + fnum(b.Bottom)
		if b.Kind == FTGL {
			b.Key = "ftgl:" + fnum(b.Top) + ":" + fnum(b.Bottom)
		}
		// B10 T3 (CTO 2026-10-03): the box exists only once BOTH pairing
		// candles are known, so the formation completes at the LATER of the
		// two — the return walk must never include the pairing candles. Per
		// the T1 ruling the box is BORN when the extreme's confirming bar
		// closes, so the origin is at least extreme+1.
		b.FormedAt = seq[extreme].idx + 1
		if seq[nearest].idx > b.FormedAt {
			b.FormedAt = seq[nearest].idx
		}
		out = append(out, *b)
	}
	return out
}

// boxFromPair builds the zone from the extreme and its nearest same-role
// swing: FTGH: [nearest high's upper body edge, extreme's highest wick];
// FTGL: [extreme's lowest wick, nearest low's lower body edge].
// nil when the zone is degenerate.
func boxFromPair(bars []market.Kline, s1, s2 swingPairAt) *Box {
	b1, b2 := bars[s1.idx], bars[s2.idx]
	if s1.kind == kernel.KindSWGH {
		top := b1.High // highest wick of the extreme high
		body := b2.Open
		if b2.Close > body {
			body = b2.Close // nearest high's upper body edge
		}
		if top <= body {
			return nil
		}
		return &Box{Kind: FTGH, Top: top, Bottom: body}
	}
	bottom := b1.Low // lowest wick of the extreme low
	body := b2.Open
	if b2.Close < body {
		body = b2.Close // nearest low's lower body edge
	}
	if body <= bottom {
		return nil
	}
	return &Box{Kind: FTGL, Top: body, Bottom: bottom}
}

// escaped reports whether a closed candle AFTER formation had its whole
// body outside the zone — "ESCAPE = the candle's BODY outside (whole candle
// better)" [D3.2 p1 @ 19:05].
func escaped(bars []market.Kline, b Box, formedAt int) bool {
	for _, c := range bars[formedAt+1:] {
		if c.CloseTime == 0 {
			continue
		}
		switch b.Kind {
		case FTGH:
			if c.Open > b.Top && c.Close > b.Top {
				return true
			}
		case FTGL:
			if c.Open < b.Bottom && c.Close < b.Bottom {
				return true
			}
		}
	}
	return false
}

// countBoxTouches counts closed candles after formation whose wick reached
// an edge — the third touch is the trade [D3.2 p1 @ 04:29].
func countBoxTouches(bars []market.Kline, b Box, formedAt int, cfg BoxCfg) int {
	n := 0
	for _, c := range bars[formedAt+1:] {
		if c.CloseTime == 0 {
			continue
		}
		if touchesEdge(b, c, cfg) {
			n++
		}
	}
	return n
}

// touchesEdge reports a wick touch of either edge within TouchBandPts.
func touchesEdge(b Box, c market.Kline, cfg BoxCfg) bool {
	switch b.Kind {
	case FTGH:
		if abs(c.High-b.Top) <= cfg.TouchBandPts {
			return true
		}
		return c.High >= b.Bottom && c.Low <= b.Bottom+cfg.TouchBandPts
	case FTGL:
		if abs(c.Low-b.Bottom) <= cfg.TouchBandPts {
			return true
		}
		return c.Low <= b.Top && c.High >= b.Top-cfg.TouchBandPts
	}
	return false
}

// BoxEdgeLocations exports each live box's edges as locations for DS-103's
// location gate (the box edge is one of the four setup locations).
func BoxEdgeLocations(boxes []Box) []Level {
	var out []Level
	for _, b := range boxes {
		out = append(out,
			Level{Key: b.Key + ":top", Kind: KindFTGHEdge, Price: b.Top},
			Level{Key: b.Key + ":bottom", Kind: KindFTGLEdge, Price: b.Bottom},
		)
	}
	return out
}

// InsideAnyBox is the §12 ban: "NEVER trade inside the box — 'hoàn toàn
// không' — neither the candle nor your entry point" [D3.2 p1 @ 06:59].
func InsideAnyBox(boxes []Box, price float64) bool {
	for _, b := range boxes {
		if price > b.Bottom && price < b.Top {
			return true
		}
	}
	return false
}
