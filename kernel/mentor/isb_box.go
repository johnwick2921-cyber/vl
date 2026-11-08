package mentor

import (
	"time"

	"vl/kernel"
	"vl/market"
)

// R5 — the 5m ISB rest box (RULES-FIX-v3): box the latest 5m inside-bar candle
// and extend it right. While it stands, use the box, not the trigger line
// [D3.4 p2 @ 00:14–13:00; D4.1 p1 @ 21:38].
//
// Nothing trades inside it except a 1m ISB in the SAME direction as the 5m ISB
// (the R1 direction: the colour of the 5m candle BEFORE the inside bar).
// Escape = a 1m candle closes with its BODY outside the box; then trade the
// escape direction (ISB or PHL/PLH — the evaluator's choice) and DELETE the
// box [D3.4 p2 @ 12:02]. No per-touch weight change belongs here: a level only
// ever gets WEAKER with each test (deck slide 17; CTO 2026-10-03).

// ISBBox is one standing 5m ISB rest box.
type ISBBox struct {
	High   float64 // the 5m ISB candle's wick high (the box top)
	Low    float64 // the 5m ISB candle's wick low (the box bottom)
	Dir    Side    // the 5m ISB's R1 direction (candle-1 colour) [D1.4 p1 @ 09:20–10:20]
	AtTime int64   // the 5m ISB candle's open time
	// Provisional (m465 P1, D4.2-R2/R11) marks a 15m box read from the FORMING
	// 15m candle (forming15mISB): two closed 5m candles whose combined body sits
	// inside the last closed 15m candle. It gates exactly like the closed box —
	// under "_forming" refusal names — but never counts for mode C
	// (MTFConfluenceStanding), and the real box replaces it at the 15m close.
	Provisional bool `json:",omitempty"`
	// Escaped (m473 B3, mode close5m): the side a 1m candle has CLOSED beyond
	// this box while it still stands. The box is deleted only when a candle of
	// its OWN timeframe closes outside; until then it exists but no longer
	// gates (trade permission starts at the 1m close) [Oct D4.3 @ 65:00-66:13].
	Escaped Side `json:",omitempty"`
}

// ISBBoxFrom5m reports whether (prev5m, cur5m) form a 5m ISB and, if so, boxes
// cur5m (the inside-bar candle itself, wicks included) [D3.4 p2 @ 00:14].
func ISBBoxFrom5m(prev5m, cur5m market.Kline) (ISBBox, bool) {
	if !IsISB(prev5m, cur5m) {
		return ISBBox{}, false
	}
	return ISBBox{
		High:   cur5m.High,
		Low:    cur5m.Low,
		Dir:    ISBDirection(prev5m),
		AtTime: cur5m.OpenTime,
	}, true
}

// ISBBoxAllows answers the R5 trade rule for a candidate 1m ISB while the box
// stands: allowed only when the pair IS an ISB AND its R1 direction equals the
// 5m ISB's direction [D3.4 p2 @ 00:14–13:00].
func ISBBoxAllows(box ISBBox, prev1m, cur1m market.Kline) (allowed bool, reason string) {
	if !IsISB(prev1m, cur1m) {
		return false, "R5 box: not a 1m ISB [D3.4 p2 @ 00:14]"
	}
	if ISBDirection(prev1m) != box.Dir {
		return false, "R5 box: a 1m ISB inside the box must match the 5m ISB direction [D3.4 p2 @ 00:14–13:00]"
	}
	return true, ""
}

// ISBBoxEscape is the pre-#25 CLOSE-only escape test (a 1m close beyond the
// edge), kept as the "close" value of the CE-5 knob. On escape the box is
// deleted [D3.4 p2 @ 12:02].
func ISBBoxEscape(box ISBBox, bar1m market.Kline) (escaped bool, dir Side) {
	if bar1m.Close > box.High {
		return true, SideLong
	}
	if bar1m.Close < box.Low {
		return true, SideShort
	}
	return false, ""
}

// ISBBoxEscapeWith is THE ISB rest-box escape the evaluator reads (CE-5,
// release #25, knob isb_box_escape). "body" (the shipped default): the 1m
// candle's BODY wholly beyond the edge — OPEN AND CLOSE above the high (long)
// / below the low (short) — "Là một cây nến một phút… nó đóng bên ngoài cái
// box là at least… cái thân nến nó chỉ nằm ở cái thân nến nằm bên ngoài mà
// cái râu nến nằm bên trong cái box ok bình thường không sao hết. Cái đó là at
// least" [D4.1 p1 @20:36–21:02] — the same test as the FTGH/FTGL box escape
// (box.go) and the ORB (orbBodyEscape). "close" or "" = the close-only test
// (a close one tick out with the open inside used to delete the box).
func ISBBoxEscapeWith(box ISBBox, bar1m market.Kline, mode string) (escaped bool, dir Side) {
	if mode != ISBBoxEscapeBody {
		return ISBBoxEscape(box, bar1m)
	}
	if bar1m.Open > box.High && bar1m.Close > box.High {
		return true, SideLong
	}
	if bar1m.Open < box.Low && bar1m.Close < box.Low {
		return true, SideShort
	}
	return false, ""
}

// fifteenMinISBGate is the CE-1 rule (release #25, knob
// 15m_ib_requires_5m_isb) for an ISB candidate of `side` while the 15m ISB rest
// box stands (box15 != nil). It returns the refusal stage ("" = allowed):
//
//   - "require_5m_isb" — the 5m must show an inside bar the SAME way as the
//     15m: "Nếu như khung 5 xuất hiện INSA 3 cùng chiều với khung 15 thì tôi
//     kiếm lệnh — tôi dùng 1 phút tôi kiếm lệnh tôi trade" [D4.2 p1
//     @14:24–14:34]; "Nếu như khung 5 phút xuất hiện Insaba cho lệnh short thì
//     em trade. Còn không em không trade" [@17:09–17:13]. Satisfied by a
//     STANDING 5m box in the 15m direction, or by the last 5m box — drawn at or
//     after the 15m inside candle opened — having escaped the 15m way.
//   - "no_trade_until_escape" — option 1: "Tôi không trade luôn… Đợi nó thoát
//     khỏi range rồi mình trade" [D4.2 p1 @01:06–01:14] — no ISB at all.
//   - "off" / "" — the pre-#25 read (only the 15m box's own direction gate).
//
// The 15m escalation ladder (crossing 15m ISBs, D3.4 p2 @17:07–17:49) lifts
// this gate exactly as it lifts the 15m direction gate.
func fifteenMinISBGate(mode string, box15, box5 *ISBBox, last5mAt int64, last5mDir, last5mEscape Side) string {
	if box15 == nil {
		return ""
	}
	switch mode {
	case FifteenMinUntilEscape:
		return "15m_until_escape"
	case FifteenMinRequire5mISB:
		if box5 != nil {
			if box5.Dir == box15.Dir {
				return ""
			}
			return "15m_needs_5m_isb"
		}
		if last5mAt >= box15.AtTime && last5mDir == box15.Dir && last5mEscape == box15.Dir {
			return ""
		}
		return "15m_needs_5m_isb"
	}
	return ""
}

// mtfConflict is the D4.2-03 PERSISTENT 15m/5m conflict: BOTH the 5m and the
// 15m ISB boxes stand with OPPOSITE directions → "làm ơn đừng trade luôn" — no
// trade at all until one of the two boxes escapes [D4.2 p1 @13:59–14:53,
// @07:37–08:00]. Unlike the old one-shot pair veto it is a standing STATE, so
// it refuses every setup (ISB, PHL/PLH, box returns) for as long as the two
// boxes disagree.
func mtfConflict(box5, box15 *ISBBox) bool {
	return box5 != nil && box15 != nil && box5.Dir != box15.Dir
}

// tfConflictHeld is the CE-2 extension (release #25, knob tf_conflict_end):
// with "both_boxes" a 15m/5m conflict, once seen, keeps refusing until BOTH
// boxes have escaped — "Đừng trade. Đợi nó thoát CẢ luôn" [D4.2 p1
// @07:54–08:02]. It returns the conflict verdict for this tick and the new
// latch. "first_box" / "" = the pre-#25 read: the conflict ends the moment
// either box escapes (the 5m box, one 5m candle wide, usually goes first).
func tfConflictHeld(mode string, box5, box15 *ISBBox, latched bool) (conflict, held, newLatch bool) {
	now := mtfConflict(box5, box15)
	if mode != TFConflictBothBoxes {
		return now, false, false
	}
	if now {
		return true, false, true
	}
	if latched && (box5 != nil || box15 != nil) {
		return true, true, true
	}
	return false, false, false
}

// tfConflictLatchStep is tfConflictHeld plus the latch's ORIGIN (m465 review):
// latchForming is true while the latch was set only by a conflict with a
// PROVISIONAL (forming) 15m box. A conflict with a real 15m box marks it real;
// a held tick keeps the origin; no latch → false.
func tfConflictLatchStep(mode string, box5, box15 *ISBBox, latched, forming bool) (conflict, held, newLatch, newForming bool) {
	conflict, held, newLatch = tfConflictHeld(mode, box5, box15, latched)
	switch {
	case !newLatch:
		newForming = false
	case mtfConflict(box5, box15):
		provisional := box15.Provisional
		newForming = provisional && (!latched || forming)
	default:
		newForming = forming
	}
	return conflict, held, newLatch, newForming
}

// crossingISBs reports whether the last `lookback` CLOSED buckets hold 2 ISBs
// with OPPOSITE directions — the choppy read that makes that TF invalid and
// escalates the read one TF up [D3.4 p2 @17:07–17:49]. Conservative reading
// (mentor question open, Q10): lookback 3 buckets, 2 crossing ISBs.
func crossingISBs(buckets []market.Kline, lookback int) bool {
	if len(buckets) < lookback {
		return false
	}
	window := buckets[len(buckets)-lookback:]
	var dirs []Side
	for i := 1; i < len(window); i++ {
		if IsISB(window[i-1], window[i]) {
			dirs = append(dirs, ISBDirection(window[i-1]))
		}
	}
	if len(dirs) < 2 {
		return false
	}
	return dirs[0] != dirs[len(dirs)-1]
}

// liveBox is the box as the GATES see it: nil when absent or already broken by
// a 1m close (mode close5m keeps a broken box alive until its own timeframe
// closes outside, but it no longer gates) [Oct D4.3 @ 65:00-66:13].
func liveBox(b *ISBBox) *ISBBox {
	if b == nil || b.Escaped != "" {
		return nil
	}
	return b
}

func (e *Evaluator) box5() *ISBBox  { return liveBox(e.State.ISBBox) }
func (e *Evaluator) box15() *ISBBox { return liveBox(e.State.ISBBox15m) }
func (e *Evaluator) box30() *ISBBox { return liveBox(e.State.ISBBox30m) }

// isbBoxStep runs one closed 1m candle against a standing box. newBreak = a 1m
// close beyond the edge that was not already recorded (trade permission and the
// break lock); gone = the box goes away now. Modes close/body/"" delete on the
// 1m escape (legacy). Mode close5m (the default, m473 B3) marks the box Escaped
// and deletes it only when a closed candle of the box's OWN timeframe (cbTF,
// opened after the box candle) closes outside it [Oct D4.3 @ 65:00-66:13,
// OctD4.3-R67]. A provisional (forming) 15m box keeps the 1m delete: it is
// dropped at its bucket close anyway.
func isbBoxStep(b *ISBBox, cur market.Kline, mode string, cbTF []market.Kline) (newBreak bool, dir Side, gone bool) {
	esc, d := ISBBoxEscapeWith(*b, cur, mode)
	if mode != ISBBoxEscapeCloseBoxTF || b.Provisional {
		return esc, d, esc
	}
	prev := b.Escaped
	if esc {
		b.Escaped = d
	} else {
		b.Escaped = ""
	}
	newBreak, dir = esc && prev != d, d
	if n := len(cbTF); n > 0 && cbTF[n-1].OpenTime > b.AtTime {
		if c := cbTF[n-1].Close; c > b.High || c < b.Low {
			gone = true
			if c > b.High {
				dir = SideLong
			} else {
				dir = SideShort
			}
			if !esc && prev == "" {
				newBreak = true // the TF close is the first sight of the break
			}
		}
	}
	return newBreak, dir, gone
}

// isFirstRTH5m reports whether the 5m candle opening at openMs is the RTH-open
// bucket: 08:30 CT on a day the CME is open at that minute (calendar-driven,
// so half-days too).
// firstRTH5mNotISB switches the m473 B4 opening rule (frozen pre-m473 goldens set it false).
var firstRTH5mNotISB = true

func isFirstRTH5m(openMs int64) bool {
	if !firstRTH5mNotISB {
		return false
	}
	t := time.UnixMilli(openMs).In(ctime())
	return t.Hour() == 8 && t.Minute() == 30 && kernel.IsCMEOpen(t)
}

// ISBBoxFromRTH5m is ISBBoxFrom5m for the 5m trigger TF with the October
// opening rule: the first RTH 5m candle (08:30 CT) is never itself the inside
// bar — judged against the 08:25 Globex candle it would be a false ISB; the
// next 5m candle that stays inside it is [Oct D4.3 @ 57:20-57:35, OctD4.3-R58;
// frames scenes/Ngày 4.3 Multi Time Frame/s_03442.jpg, s_03450.jpg, s_03455.jpg].
func ISBBoxFromRTH5m(prev5m, cur5m market.Kline) (ISBBox, bool) {
	if isFirstRTH5m(cur5m.OpenTime) {
		return ISBBox{}, false
	}
	return ISBBoxFrom5m(prev5m, cur5m)
}
