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
// latches the escape: a CLOSED 1m candle whose BODY closes outside the box
// (close beyond the high → longs only; below the low → shorts only). The
// escape is NOT an entry — it only picks the side [X5 @03:29–03:47]. A new
// session day resets the ORB.
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
	if orb.Escaped != "" {
		return orb
	}
	if len(bars) == 0 {
		return orb
	}
	cur := bars[len(bars)-1]
	if cur.CloseTime > now {
		return orb // the last 1m candle has not closed — no escape test yet
	}
	if cur.Close > orb.High {
		orb.Escaped = SideLong
	} else if cur.Close < orb.Low {
		orb.Escaped = SideShort
	}
	return orb
}

// ORBVerdict gates one intraday entry on the ORB: nothing trades until the ORB
// is drawn and price has LEFT it with a 1m body close outside, and after the
// escape ONLY the escape side trades, and never inside the range
// [X5 @02:36, 03:29–03:47]. The swing is exempt (it is gated upstream, not
// here).
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
	return true, ""
}

// orbGateFilter applies the ORB gate to every intraday entry intent. The §8
// swing is EXEMPT (its reasons start with "swing") — the ORB is a gate on
// intraday entries only. No ORB for pre-market [X5 @05:42]: before the RTH
// open (08:30 CT) the gate does not block — it applies only from 08:30 CT
// until the ORB is drawn and escaped.
func orbGateFilter(ints []Intent, orb ORB, now int64, cfg Config) (out []Intent, refusals []string) {
	if !cfg.OrbGateEnabled {
		return ints, nil
	}
	if now < dayStartCT(now)+int64(globexCloseMin)*60_000 {
		return ints, nil // pre-market (before the 08:30 CT RTH open): no ORB gate [X5 @05:42]
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
