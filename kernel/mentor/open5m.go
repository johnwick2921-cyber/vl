package mentor

import (
	"strings"
	"time"

	"vl/market"
)

// The D4.4 p2 open-window rule ("at the open do not buy market on the 4h bias;
// drop to the 5-minute and wait for it to break the 5m range, then go with the
// 4h"):
//
//	"Nhưng mà không phải là cứ cắm đầu / Mà cứ vô lệnh buy market …
//	 chuyển về khung giờ nhỏ hơn / Khung 5 phút … Nếu mà bây giờ 4 giờ
//	 Trigger … Thì mình làm gì mình cứ coi theo khung 5 phút thôi …
//	 nó đi lên nó break được cái rank khung 5 phút" [D4.4 p2 @04:41–05:04,
//	 @05:45–06:00].
//
// The mentor gives no formal definition of the 5m range; the opening 5m range is
// the FIRST 5-minute candle of RTH (08:30–08:35 CT, the five 1m bars 08:30–08:34)
// [example: the ORB's own 08:30 opening anchor, X5 @01:52].

// open5mRange returns the opening 5m range of the RTH session: the HIGH and LOW
// of the first 5-minute candle (08:30–08:35 CT — the five 1m bars at 08:30–08:34),
// and whether it is drawn (all five 1m bars are present in the window).
func open5mRange(bars []market.Kline, now int64) (high, low float64, drawn bool) {
	day := dayStartCT(now)
	start := day + int64(8*60+30)*60_000 // 08:30 CT
	var present [5]bool
	first := true
	for i := range bars {
		b := &bars[i]
		idx := (b.OpenTime - start) / 60_000
		if idx < 0 || idx >= 5 {
			continue
		}
		present[idx] = true
		if first {
			high, low, first = b.High, b.Low, false
		} else {
			if b.High > high {
				high = b.High
			}
			if b.Low < low {
				low = b.Low
			}
		}
	}
	for _, p := range present {
		if !p {
			return 0, 0, false
		}
	}
	return high, low, true
}

// open5mBroken reports whether a CLOSED 5m candle's close has broken the opening
// 5m range: brokeLong = a close above the high, brokeShort = a close below the
// low. A 5m candle is closed when the 1m bar at minute 4 of its bucket has
// CloseTime <= now (the 08:35 bucket closes at 08:40, and so on).
func open5mBroken(bars []market.Kline, now int64, high, low float64) (brokeLong, brokeShort bool) {
	day := dayStartCT(now)
	base := day + int64(8*60+35)*60_000 // the first 5m bucket after the opening one starts 08:35
	for i := range bars {
		b := &bars[i]
		if b.CloseTime > now || b.OpenTime < base {
			continue
		}
		if ((b.OpenTime - base) / 60_000 % 5) == 4 {
			if b.Close > high {
				brokeLong = true
			}
			if b.Close < low {
				brokeShort = true
			}
		}
	}
	return brokeLong, brokeShort
}

// open5mRangeFilter — D4.4 p2 @04:41–06:00: in the open window (08:30–09:30 CT)
// a with-4h entry is allowed only after a 5m candle has CLOSED outside the
// opening 5m range in the 4h's direction. The swing is exempt (it never consults
// the 4h). Off-4h entries are the HTF gate's job, not this filter's.
//
// D4.4 p2 @05:45–05:50 narrows WHEN the 5m-drop rule applies: only while price
// sits on the WRONG side of the 4h trigger line — a 4h buy with price still
// below the 4h trigger line (or a 4h sell with price still above). With price
// already on the 4h's side the 4h is confirmed and no 5m break is required.
// open5mMissingNote is the note key emitted when the filter fails OPEN because
// the opening 5m range is undrawn at/after 08:35 (a truncated series or a data
// gap). The caller turns the note's key into a counter (e.count) so the event is
// diffable against the replay funnel.
const open5mMissingNote = "open5m_range_missing"

func open5mRangeFilter(ints []Intent, h HTF, bars []market.Kline, now int64) (out []Intent, refusals []string, notes []string) {
	t := time.UnixMilli(now).In(ctime())
	mins := t.Hour()*60 + t.Minute()
	if mins < 8*60+30 || mins > 9*60+30 {
		return ints, nil, nil
	}
	dir := h.FourH.Dir
	if dir == "" {
		return ints, nil, nil // no 4h trigger — nothing to confirm
	}
	if len(bars) == 0 {
		return ints, nil, nil
	}
	px := bars[len(bars)-1].Close
	if !open5mGateApplies(dir, px, h.FourH.Price) {
		return ints, nil, nil // price already on the 4h side — the 4h is confirmed
	}
	high, low, drawn := open5mRange(bars, now)
	var brokeLong, brokeShort bool
	switch {
	case drawn:
		brokeLong, brokeShort = open5mBroken(bars, now, high, low)
	case now < dayStartCT(now)+int64(8*60+35)*60_000:
		// Before 08:35 the opening 5m is still forming — no 5m close outside the
		// range can have happened yet, so broke* stay false and a with-4h entry
		// on the wrong side of the 4h line is REFUSED below (the old pass-through
		// left the 08:32–08:34 ORB-escaped gap unguarded).
	default:
		// At/after 08:35 a missing range means the series is truncated (a test
		// fixture or a data gap) — fail OPEN: the ORB and the HTF gate still hold.
		// The fail-open is LOUD (a note + a counter), never silent.
		return ints, nil, []string{open5mMissingNote}
	}
	out = make([]Intent, 0, len(ints))
	for _, in := range ints {
		if in.Action != PlaceStopEntry && in.Action != PlaceStopLimitEntry || strings.HasPrefix(in.Reason, "swing") {
			out = append(out, in)
			continue
		}
		if in.Side != dir {
			out = append(out, in) // off-4h — the HTF gate owns that refusal
			continue
		}
		ok := (dir == SideLong && brokeLong) || (dir == SideShort && brokeShort)
		if ok {
			out = append(out, in)
			continue
		}
		refusals = append(refusals, "open_5m_range_not_broken")
	}
	return out, refusals, notes
}

// open5mGateApplies is the D4.4 p2 @05:45–05:50 narrower condition: the 5m-drop
// rule applies only while the current price is on the WRONG side of the 4h
// trigger line — a 4h buy trigger with price still below the 4h trigger line, or
// a 4h sell with price still above — "Nếu mà bây giờ 4 giờ Trigger, mà nó nằm
// dưới cái đường Trigger của khung 4 giờ, thì mình cứ coi theo khung 5 phút
// thôi". Price already on the 4h's side means the 4h is confirmed.
func open5mGateApplies(dir Side, px, trigger float64) bool {
	switch dir {
	case SideLong:
		return px < trigger
	case SideShort:
		return px > trigger
	default:
		return false
	}
}
