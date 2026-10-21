package mentor

import (
	"strings"
	"time"

	"vl/market"
)

// ORB is the §7 step 0 opening-range gate ("ĐIỀU BẮT BUỘC" [X11 @16:43]): the
// HIGH and the LOW of the FIRST 2-minute candle of the regular session
// (08:30–08:32 CT), drawn only once that candle has completed [X5 @01:52,
// 06:29]. No ORB for pre-market [X5 @05:42] — it draws from the 08:30/08:31
// 1m bars of the regular session only.
type ORB struct {
	Day     int64   `json:"day"`     // the CT session day the ORB belongs to
	High    float64 `json:"high"`    // first 2m candle high
	Low     float64 `json:"low"`     // first 2m candle low
	Drawn   bool    `json:"drawn"`   // the 08:30–08:32 candle has completed
	Escaped Side    `json:"escaped"` // "" = not escaped yet; else the escape direction
}

// dayStartCT floors a REAL-UTC epoch-millis time to its CT-midnight epoch
// (EPOCH RULING 2026-10-03: one convention = real UTC everywhere; wall
// arithmetic shifts by 5h/6h with DST).
func dayStartCT(t int64) int64 {
	tt := time.UnixMilli(t).In(ctime())
	return time.Date(tt.Year(), tt.Month(), tt.Day(), 0, 0, 0, 0, ctime()).UnixMilli()
}

// ORBAdvance draws the ORB once the 08:30 2-minute candle has completed and
// reads the escape on every CLOSED 1m candle.
//
// ESCAPE = the candle's BODY wholly outside — OPEN AND CLOSE beyond the edge:
// "Break là sao? Là một cây nến một phút… nó đóng bên ngoài cái box là AT
// LEAST… nếu như mà cái THÂN NẾN nó chỉ nằm ở cái thân nến nằm bên ngoài mà
// cái râu nến nằm bên trong cái box ok bình thường không sao hết. Cái đó là at
// least" [D4.1 p1 @20:36–21:03] — the course's minimum, the same test as the
// bot's own box escape (box.go escaped: open AND close outside). X5 asks for
// more ("COMPLETELY nằm ở ngoài mình mới trade" [X5 @07:04]); the course,
// later, sets the minimum and calls the whole candle "hoàn toàn hợp lý" — a
// close one tick outside with the open inside is NOT an escape (the pre-10-14
// close-only test was looser than both).
//
// The side is the side of the LATEST escape: above the high → longs only;
// below the low → shorts only — "Nó thoát ra khỏi đầu nào, nó sẽ đi theo đầu
// đó" [X5 @02:57–03:02]. It is NOT locked for the session: a later body
// escape beyond the OTHER edge switches the side — "Nếu mà không có nằm
// completely ở ngoài cái box này thì mình đâu có kiếm lệnh short nữa. Mình vẫn
// sẽ kiếm lệnh long… cho tới khi nào nó crack" [X7 @07:42–07:51]. No source
// holds the first side all day (the pre-10-14 latch did). The escape is NOT
// an entry — it only picks the side [X5 @03:29–03:47]. A new session day
// resets the ORB.
func ORBAdvance(orb ORB, bars []market.Kline, now int64) ORB {
	day := dayStartCT(now)
	if orb.Day != day {
		orb = ORB{Day: day} // new session day
	}
	if !orb.Drawn {
		const rthOpen = 8*60 + 30 // 08:30 CT
		var b1, b2 *market.Kline
		for i := range bars {
			b := &bars[i]
			switch b.OpenTime {
			case day + int64(rthOpen)*60_000:
				b1 = b
			case day + int64(rthOpen+1)*60_000:
				b2 = b
			}
		}
		if b1 != nil && b2 != nil && now >= day+int64(rthOpen+2)*60_000 {
			orb.High = maxf(b1.High, b2.High)
			orb.Low = minf(b1.Low, b2.Low)
			orb.Drawn = true
		} else {
			return orb
		}
		// P5 (493c7ead8): fall through — the escape test runs on the SAME closed
		// candle that completed the 2m ORB. Returning here skipped the 08:32
		// escape candle and latched one tick late (08:33).
	}
	if len(bars) == 0 {
		return orb
	}
	cur := bars[len(bars)-1]
	if cur.CloseTime > now {
		return orb // the last 1m candle has not closed — no escape test yet
	}
	if side := orbBodyEscape(orb, cur); side != "" {
		orb.Escaped = side // the latest escape picks the side [X5 @02:57; X7 @07:42]
	}
	return orb
}

// orbBodyEscape is the ONE escape test: the 1m candle's BODY wholly beyond an
// edge — open AND close above the high (long) / below the low (short)
// [D4.1 p1 @20:36–21:03]. "" = no escape on this candle (inside, or a body
// that straddles an edge).
func orbBodyEscape(orb ORB, c market.Kline) Side {
	switch {
	case c.Open > orb.High && c.Close > orb.High:
		return SideLong
	case c.Open < orb.Low && c.Close < orb.Low:
		return SideShort
	}
	return ""
}

// ORBVerdict gates one intraday entry on the ORB: nothing trades until the ORB
// is drawn and price has LEFT it with a 1m BODY wholly outside, after the
// escape ONLY the side of the LATEST escape trades (a body escape beyond the
// other edge switches it — ORBAdvance), and never inside the range
// [X5 @02:36, 03:29–03:47; X7 @07:42–07:51]. The swing is exempt (it is gated
// upstream, not here).
func ORBVerdict(orb ORB, side Side, price float64, cfg Config) (ok bool, reason string) {
	if !cfg.OrbGateEnabled {
		return true, ""
	}
	if !orb.Drawn {
		return false, "ORB gate: the opening range is not drawn yet — no trade until the 08:30 2-minute candle completes [X5 @01:52, 06:29]"
	}
	if orb.Escaped == "" {
		return false, "ORB gate: price has not left the opening range — no trade inside it, no reversal at its edges [X5 @02:36]"
	}
	if side != orb.Escaped {
		return false, "ORB gate: the escape picked the " + string(orb.Escaped) + " side — " + string(side) + " entries are off [X5 @03:29–03:47]"
	}
	if side == SideLong && price <= orb.High {
		return false, "ORB gate: no trade inside the opening range [X5 @02:36]"
	}
	if side == SideShort && price >= orb.Low {
		return false, "ORB gate: no trade inside the opening range [X5 @02:36]"
	}
	if orbEdgeTooNear(orb, side, price, cfg) {
		return false, "ORB gate: the entry sits too near the ORB edge — \"điểm entry… nằm gần cái resistance quá thì thôi bỏ đi\" [X5 @08:52–09:09]"
	}
	return true, ""
}

// orbEdgeTooNear is the CE-7 veto (release #25, knob orb_edge_veto_pts,
// default 0 = off): an escape-side entry whose price lies within the knob's
// points of the ORB edge it broke is skipped — the edge is the resistance
// (support) the breakout fights: "cái cạnh trên của cái ORB là cái resistance,
// cái risk nó rất cao… cái điểm entry… mà nằm gần cái resistance quá thì thôi
// bỏ đi" [X5 @08:20–09:09]. The source gives no number (OQ-3).
func orbEdgeTooNear(orb ORB, side Side, price float64, cfg Config) bool {
	if cfg.OrbEdgeVetoPts <= 0 {
		return false
	}
	switch side {
	case SideLong:
		return price-orb.High < cfg.OrbEdgeVetoPts
	case SideShort:
		return orb.Low-price < cfg.OrbEdgeVetoPts
	}
	return false
}

// orbISBFractionOutside is the fraction of the ISB candle's range (high − low)
// that lies OUTSIDE the ORB on the trade side — rule 5 (release #31): "80% 90%
// là cái nến nó nằm ở ngoài… anh chị có quyền take risk" [extra "Routine trước
// khi trade" part1 @07:46–09:04]. A candle wholly outside the edge is 1; wholly
// inside/behind is 0; a candle with no range (hi <= lo) is unmeasured.
func orbISBFractionOutside(orb ORB, side Side, hi, lo float64) float64 {
	if hi <= lo {
		return 0
	}
	rng := hi - lo
	switch side {
	case SideLong:
		if lo >= orb.High {
			return 1
		}
		if hi <= orb.High {
			return 0
		}
		return (hi - orb.High) / rng
	case SideShort:
		if hi <= orb.Low {
			return 1
		}
		if lo >= orb.Low {
			return 0
		}
		return (orb.Low - lo) / rng
	}
	return 0
}

// orbISBMostlyInside reports whether an ISB at the ORB edge must be skipped:
// less than cfg.OrbISBFractionOutside of the ISB candle lies outside the ORB
// on the trade side — the entry sits too near the resistance (the edge) and the
// candle is mostly still inside: "điểm entry… nằm gần cái resistance quá thì
// thôi bỏ đi" [X5 @08:52–09:09]. Knob <= 0 = the rule is off.
func orbISBMostlyInside(orb ORB, side Side, hi, lo float64, cfg Config) bool {
	if cfg.OrbISBFractionOutside <= 0 || hi <= lo {
		return false
	}
	return orbISBFractionOutside(orb, side, hi, lo) < cfg.OrbISBFractionOutside
}

// orbGateFilter applies the ORB gate to every intraday entry intent. The §8
// swing is EXEMPT (its reasons start with "swing") — the ORB is a gate on
// intraday entries only. The ORB belongs to the RTH open only [X5 @05:42
// "No ORB for pre-market"; EXTRAS P2]: the gate applies only INSIDE RTH, from
// the 08:30 CT open to the 15:00 CT close (rthWindowEndMin). Outside RTH —
// pre-market before 08:30 and the evening Globex from 15:00 — there is no ORB
// filter.
func orbGateFilter(ints []Intent, orb ORB, now int64, cfg Config) (out []Intent, refusals []string) {
	if !cfg.OrbGateEnabled {
		return ints, nil
	}
	day := dayStartCT(now)
	rthOpen := day + int64(globexCloseMin)*60_000   // 08:30 CT
	rthClose := day + int64(rthWindowEndMin)*60_000 // 15:00 CT
	if now < rthOpen || now >= rthClose {
		return ints, nil // outside RTH: no ORB gate [X5 @05:42; EXTRAS P2]
	}
	out = make([]Intent, 0, len(ints))
	for _, in := range ints {
		if in.Action != PlaceStopEntry && in.Action != PlaceStopLimitEntry {
			out = append(out, in)
			continue
		}
		if strings.HasPrefix(in.Reason, "swing") {
			out = append(out, in) // swing exempt
			continue
		}
		if ok, _ := ORBVerdict(orb, in.Side, in.Price, cfg); ok {
			// Rule 5 (release #31): an ISB at the ORB edge trades only when
			// the ISB candle is mostly OUTSIDE the range on the trade side —
			// the fraction, not a points distance (that is CE-7).
			if in.Setup == "ISB" && orbISBMostlyInside(orb, in.Side, in.EntryCandleHigh, in.EntryCandleLow, cfg) {
				refusals = append(refusals, "orb_isb_mostly_inside")
				continue
			}
			out = append(out, in)
		} else {
			refusals = append(refusals, orbRefusalStage(orb, in.Side, in.Price, cfg))
		}
	}
	return out, refusals
}

// orbRefusalStage names the B-rules funnel stage an ORB refusal lands in
// (the long reason strings stay in ORBVerdict).
func orbRefusalStage(orb ORB, side Side, price float64, cfg Config) string {
	if !cfg.OrbGateEnabled {
		return ""
	}
	if !orb.Drawn {
		return "orb_not_drawn"
	}
	if orb.Escaped == "" {
		return "orb_not_escaped"
	}
	if side != orb.Escaped {
		return "orb_wrong_side"
	}
	if (side == SideLong && price > orb.High || side == SideShort && price < orb.Low) && orbEdgeTooNear(orb, side, price, cfg) {
		return "orb_edge_too_near" // CE-7
	}
	return "orb_inside"
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
