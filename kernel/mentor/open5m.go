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
func open5mRangeFilter(ints []Intent, h HTF, bars []market.Kline, now int64) (out []Intent, refusals []string) {
	t := time.UnixMilli(now).In(ctime())
	mins := t.Hour()*60 + t.Minute()
	if mins < 8*60+30 || mins > 9*60+30 {
		return ints, nil
	}
	dir := h.FourH.Dir
	if dir == "" {
		return ints, nil // no 4h trigger — nothing to confirm
	}
	high, low, drawn := open5mRange(bars, now)
	if !drawn {
		return ints, nil // before 08:35 the opening 5m is still forming (the ORB holds)
	}
	brokeLong, brokeShort := open5mBroken(bars, now, high, low)
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
	return out, refusals
}
