package mentor

import "vl/market"

// wickMicroscalpLevel (D4.3-02/-03/-07) reports the wick-microscalp target level
// when the pressure read fires: 2+ consecutive CLOSED 5m candles rejecting with
// wicks the SAME way — LOWER wicks in an uptrend, UPPER wicks in a downtrend
// ("TỪ 2 ĐẾN 3 CÂY TRỞ LÊN… NÓ RÚT RÂU CÙNG CHIỀU NHA… NÓ ĐANG CÓ SỰ MUA VÔ"
// [D4.3 @00:00–03:20]; "RÚT TRÊN NHA — tại vì ĐANG XU HƯỚNG GIẢM MÀ… đang xu
// hướng giảm mà RÚT DƯỚI là TÔI KHÔNG BIẾT ĐƯỜNG TÔI ĐÁNH" [@13:22–13:44]).
//
// The target is the rejecting candles' FAR wick — the ceiling high for a long,
// the floor low for a short — "TARGET VẪN LÀ KHUNG 5 PHÚT — TARGET LÀ NHỮNG CÁI
// RÂU CỦA KHUNG 5 PHÚT" [D4.3 @09:27–09:46]; bounded, never 100–200 pts.
//
// Conservative reading (mentor question open): the threshold is 2 (the minimum
// of "2–3"); a wick "rejects" when the low is strictly below the body (long) /
// the high strictly above the body (short); the target is the extreme wick of
// the rejecting run.
func wickMicroscalpLevel(buckets []market.Kline, trend Side) (Level, bool) {
	if trend != SideLong && trend != SideShort {
		return Level{}, false
	}
	run := 0
	extreme := 0.0
	for i := len(buckets) - 1; i >= 0; i-- {
		c := buckets[i]
		bodyLow, bodyHigh := c.Open, c.Open
		if c.Close < bodyLow {
			bodyLow = c.Close
		}
		if c.Close > bodyHigh {
			bodyHigh = c.Close
		}
		var rejecting bool
		if trend == SideLong {
			rejecting = c.Low < bodyLow // lower wick = buyers bought the dip
			if rejecting && (extreme == 0 || c.High > extreme) {
				extreme = c.High
			}
		} else {
			rejecting = c.High > bodyHigh // upper wick = sellers sold the pop
			if rejecting && (extreme == 0 || c.Low < extreme) {
				extreme = c.Low
			}
		}
		if !rejecting {
			break
		}
		run++
	}
	if run < 2 {
		return Level{}, false
	}
	return Level{Key: "wick_microscalp", Kind: KindWickMicroscalp, Price: extreme}, true
}
