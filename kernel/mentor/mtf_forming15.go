package mentor

import "vl/market"

// ── The EARLY 15m read (m465 P1, release #60) ──────────────────────────────
//
// D4.2-R2 [D4.2 p1 @00:08–00:39]: "3 cây 5 phút mà nó cứ xào quần xào quần ngay
// chỗ đó mà nó không đi đâu hết... ngay lập tức mình biết khung 15 phút đang tạo
// inside bar"; "2, 3 cây 5 phút anh chị đã có được 1 cây 15 phút" [@07:40].
// D4.2-R11 [D4.2 p1 @05:45, 07:20, 08:15, 11:12, 11:40–11:42]: the trick — the
// 5m shows the 15m inside bar before it closes; box it; "anh chị sẽ né được
// rất nhiều đoạn ranging và bị bớt thua" (defensive). D4.3-R21 [D4.3
// @12:20–12:32]: "2 cái nến 5 phút, 3 cái nến 5 phút, nó đang làm cái nến 15
// phút rồi đó" — the sideways test.
//
// The pinned definition is the "2" of "2-3": the third 5m candle closes the 15m
// (the 5m and the 15m close together, D4.2-R11), after which the REAL 15m box
// (ISBBoxFrom5m over the closed 15m pair) takes over at the same AtTime. So the
// early read lives exactly while the forming 15m bucket holds TWO closed 5m
// candles.

// forming15mISB reads the FORMING 15m candle from the closed 5m buckets. It
// returns a PROVISIONAL 15m ISB box when:
//
//   - the 15m bucket after the last CLOSED 15m candle (cb15) holds exactly two
//     CLOSED 5m candles (the last two of cb5; the one before them belongs to an
//     earlier bucket), and
//   - the forming 15m body so far — the open of the 1st 5m, the close of the
//     2nd — sits inside the last closed 15m candle's FULL range (IsISB on the
//     synthetic candle: the mother's wicks included, a doji mother is no ISB).
//
// Box = the high/low of the two 5m candles (the forming candle so far, wicks
// included, like ISBBoxFrom5m); Dir = ISBDirection(the last closed 15m) — the
// candle-1 colour [D1.4 p1 @09:20–10:20]; AtTime = the forming 15m bucket open,
// the AtTime the real box will carry; Provisional = true.
func forming15mISB(cb5, cb15 []market.Kline) (ISBBox, bool) {
	if len(cb5) < 2 || len(cb15) < 1 {
		return ISBBox{}, false
	}
	mother := cb15[len(cb15)-1]
	a, b := cb5[len(cb5)-2], cb5[len(cb5)-1]
	form := bucketOpen(a.OpenTime, 15)
	if bucketOpen(b.OpenTime, 15) != form || form <= mother.OpenTime {
		return ISBBox{}, false
	}
	// Review fix: the mother is the 15m candle IMMEDIATELY before the forming
	// bucket. Across the 16:00–17:00 CT halt, a weekend or a missing bucket
	// the last closed 15m is an older candle, and two 5m candles are no
	// forming inside bar of it.
	if form != mother.OpenTime+15*60_000 {
		return ISBBox{}, false
	}
	// Exactly two: the 5m before them must belong to an earlier 15m bucket.
	if len(cb5) >= 3 && bucketOpen(cb5[len(cb5)-3].OpenTime, 15) == form {
		return ISBBox{}, false
	}
	hi, lo := a.High, a.Low
	if b.High > hi {
		hi = b.High
	}
	if b.Low < lo {
		lo = b.Low
	}
	synth := market.Kline{OpenTime: form, Open: a.Open, High: hi, Low: lo, Close: b.Close}
	if !IsISB(mother, synth) {
		return ISBBox{}, false
	}
	return ISBBox{High: hi, Low: lo, Dir: ISBDirection(mother), AtTime: form, Provisional: true}, true
}

// formingSuffix names a 15m-box refusal by the box that made it: a provisional
// (forming) box refuses under "<name>_forming", so the early read is counted
// apart from the closed-box gates it reuses.
func formingSuffix(box *ISBBox) string {
	if box != nil && box.Provisional {
		return "_forming"
	}
	return ""
}

// advance15mBox runs the 15m ISB rest box for one tick (D4.1-25 + the m465
// early read). Order:
//
//  1. a PROVISIONAL box whose forming bucket has now CLOSED (it is in cb15)
//     ends: the real read below replaces it when the closed pair is an ISB
//     (confirmed — same AtTime, built once), otherwise it is dropped (D4.2-R11:
//     the 5m and the 15m close together).
//  2. a standing box escapes on a 1m BODY beyond its edge (CE-5); else, with no
//     box, the latest CLOSED 15m pair builds the real box (flap-guarded by
//     LastISBBox15mAt) — the pre-#60 path, unchanged.
//  3. still no box and the knob 15m_early_read ON → the forming read
//     (forming15mISB), at most once per forming bucket (LastForming15mAt), so
//     an escaped provisional box is never re-read from the same bucket.
//
// With the knob OFF no provisional box ever exists, so steps 1 and 3 are inert
// and the tick is the pre-#60 one.
func (e *Evaluator) advance15mBox(cb5, cb15 []market.Kline, cur market.Kline) {
	if bx := e.State.ISBBox15m; bx != nil && bx.Provisional && len(cb15) > 0 && cb15[len(cb15)-1].OpenTime >= bx.AtTime {
		e.State.ISBBox15m = nil
		n := len(cb15)
		if n >= 2 && cb15[n-1].OpenTime == bx.AtTime && IsISB(cb15[n-2], cb15[n-1]) {
			e.count("isb_15m_forming_confirmed")
		} else {
			e.count("isb_15m_forming_dropped")
			// m465 review (D4.2-R10 holds a REAL 15m ISB conflict): a latch set
			// only by this provisional box had no 15m inside bar behind it.
			if e.State.TFConflictLatched && e.State.TFConflictLatchForming {
				e.State.TFConflictLatched, e.State.TFConflictLatchForming = false, false
				e.count("tf_conflict_forming_released")
			}
			e.note("15m early read dropped: the 15m candle at %s CT closed as no inside bar — the provisional %s box %.2f–%.2f is gone [D4.2-R11 p1 @07:20–08:15]",
				swingCT(bx.AtTime), bx.Dir, bx.Low, bx.High)
		}
	}
	if e.State.ISBBox15m != nil {
		if escaped, _ := ISBBoxEscapeWith(*e.State.ISBBox15m, cur, e.Cfg.ISBBoxEscapeMode); escaped { // CE-5
			if e.State.ISBBox15m.Provisional {
				e.count("isb_15m_forming_escaped")
			}
			e.State.ISBBox15m = nil
		}
	} else if len(cb15) >= 2 {
		if bx, ok := ISBBoxFrom5m(cb15[len(cb15)-2], cb15[len(cb15)-1]); ok && bx.AtTime != e.State.LastISBBox15mAt {
			e.State.ISBBox15m = &bx
			e.State.LastISBBox15mAt = bx.AtTime
		}
	}
	if e.State.ISBBox15m == nil && e.Cfg.Early15mRead {
		if bx, ok := forming15mISB(cb5, cb15); ok && bx.AtTime != e.State.LastForming15mAt {
			e.State.ISBBox15m = &bx
			e.State.LastForming15mAt = bx.AtTime
			e.count("isb_15m_forming_built")
			e.note("15m early read: two 5m candles inside the 15m candle at %s CT — the forming 15m at %s CT is an inside bar, provisional %s box %.2f–%.2f [D4.2-R2 p1 @00:08–00:39; D4.3-R21 @12:20–12:32]",
				swingCT(cb15[len(cb15)-1].OpenTime), swingCT(bx.AtTime), bx.Dir, bx.Low, bx.High)
		}
	}
}
