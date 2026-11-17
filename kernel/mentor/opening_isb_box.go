package mentor

import (
	"strings"
	"time"

	"vl/kernel"
	"vl/market"
)

// J-BOX-01 (P0, full re-read 11-16) — the OPENING 5m ISB box is a no-trade box.
//
// October:
//   - Oct D4.3 @58:00-58:33: "1 cái nến 5 phút, 2 cái nến 5 phút, 3 cái nến 5
//     phút nó vẫn chưa thoát được cái box này thì nó thành 1 cái Inside Bar …
//     bọc từ head to toe … nguyên một cái box này buổi sáng ngày hôm nay mình
//     không được tray" — the whole morning box is no-trade.
//   - @59:50-1:00:29: a student asks for a SHORT, the red mother's own side.
//     "Không. Đã nói là INSABA không được trade mà … Chừng nào thoát khỏi INSABA
//     mới được trade" — the same-direction ISB is refused too (OctD4.3-R61).
//   - @1:01:06-1:04:20: why — "Nó làm IN-X3 khung 5 phút đầu giờ" (a 5m ISB at
//     the open), inside the 2-minute ORB, in a sideways morning.
//   - @1:04:54-1:05:29: "khi có một cây nến 1 phút đóng ở trên … thì mình được
//     trade … Ở trong box là mình không được trade. Nhưng mà nó thoát ra khỏi box
//     rồi thì mình được trade" — a 1m close outside gives permission;
//     @1:05:33-1:06:13 the box is deleted on a 5m close outside (OctD4.3-R67).
//   - Oct D4.1 @11:00-11:41 (the same Mon 02 Nov morning): "tầm 30 phút đồng hồ
//     đầu tiên … em [vẽ] cái box ngay chỗ này. Em nói là em không đụng tới …
//     Khi nào thoát ra đi em trade".
//
// Code: the box is the standing 5m ISB box (e.State.ISBBox) — the 08:35 inside
// candle against the 08:30 mother (OctD4.3-R58 @57:20-57:35), merged head to toe
// (R59), Escaped on a 1m close outside (R66), deleted on a 5m close outside (R67).
// It is the OPENING box when it is the first 5m inside bar of the RTH session
// and its inside candle opens in the first half hour (08:35-08:55 CT; D4.1
// @11:00-11:20 "tầm 30 phút đồng hồ đầu tiên", D4.3 "đầu giờ"). While it stands
// unbroken, every intraday entry whose candle or entry price is inside it is
// refused, named opening_isb_box_no_trade. The §8 swing runs its own machine
// and is exempt (the orbGateFilter pattern). Knob
// mentor_opening_isb_box_no_trade (default ON; false reverts to the plain box
// gate: a same-direction 1m ISB passes, R70 @1:08:24).
//
// Not built (not October canon, verify-J-BOX-01): a same-side 5m trigger
// required to be PRESENT for a same-direction trade inside an ordinary 5m box.

// openingISBBoxWindowMin is the opening half hour, in minutes after the 08:30
// CT RTH open, inside which the opening box's inside candle must open [Oct D4.1
// @11:00-11:20 "không phải 1 tiếng mà tầm 30 phút đồng hồ đầu tiên"].
const openingISBBoxWindowMin = 30

// isOpeningISBBox reports whether the 5m ISB box b is the opening box: its
// inside candle opened in the first half hour of a CME-open RTH session (never
// the 08:30 candle itself, R58), and no earlier 5m inside-bar pair of that
// session precedes it in the closed 5m buckets cb5.
func isOpeningISBBox(b ISBBox, cb5 []market.Kline) bool {
	t := time.UnixMilli(b.AtTime).In(ctime())
	open := time.Date(t.Year(), t.Month(), t.Day(), 8, 30, 0, 0, ctime())
	if !kernel.IsCMEOpen(open) {
		return false
	}
	first := open.Add(5 * time.Minute).UnixMilli()
	if b.AtTime < first || b.AtTime >= open.Add(openingISBBoxWindowMin*time.Minute).UnixMilli() {
		return false
	}
	for i := 1; i < len(cb5); i++ {
		c := cb5[i]
		if c.OpenTime < first || c.OpenTime >= b.AtTime {
			continue
		}
		if _, ok := ISBBoxFromRTH5m(cb5[i-1], c); ok {
			return false // an earlier 5m inside bar this session: b is not the first
		}
	}
	return true
}

// openingISBBox is the opening box as the gate sees it: the live (not yet
// broken by a 1m close) 5m ISB box when it is the opening one and the knob is
// on; nil otherwise.
func (e *Evaluator) openingISBBox(cb5 []market.Kline) *ISBBox {
	if !e.Cfg.OpeningISBBoxNoTrade {
		return nil
	}
	b := e.box5()
	if b == nil || b.Provisional || !isOpeningISBBox(*b, cb5) {
		return nil
	}
	return b
}

// openingISBBoxFilter refuses every non-swing entry intent while the opening
// box stands: the candle (cur.Close) or the entry price inside the box [Oct
// D4.3 @58:00-58:33, @59:50-1:00:29].
func openingISBBoxFilter(out []Intent, b ISBBox, cur market.Kline) (kept []Intent, refusals []string) {
	in := func(px float64) bool { return px >= b.Low && px <= b.High }
	kept = out[:0:0]
	for _, it := range out {
		if (it.Action == PlaceStopEntry || it.Action == PlaceStopLimitEntry) &&
			!strings.HasPrefix(it.Reason, "swing") && (in(cur.Close) || in(it.Price)) {
			refusals = append(refusals, "opening_isb_box_no_trade")
			continue
		}
		kept = append(kept, it)
	}
	return kept, refusals
}
