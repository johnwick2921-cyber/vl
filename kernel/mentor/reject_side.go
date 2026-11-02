package mentor

import "vl/market"

// ── Mid-range: trade WITH the rejection (m466 P1, release #61: G43) ─────────
//
// D2.2-R71 [D2.2 p3 @14:28–15:58]: "nếu thật sự nó vừa reject FTGH thì về tới
// đây tôi đánh short xuống tiếp chứ… mắc mớ gì tôi đánh ngược lên? Nó mới
// reject thì đánh theo hướng nó reject". Mid-range only an inside bar trades
// (D2.2-R72 — the PHL/PLH mid-range ban, live); this rule gives that inside
// bar its side.
//
// Pinned definition (triage-61): the MOST RECENT box-edge event decides.
//
//   - FTGH reject: a closed 1m candle with High >= the ceiling's bottom and
//     Close < it (the box's EFFECTIVE role, boxEffectiveKind — a flipped FTGL
//     is a ceiling) → the rejection points SHORT: while price sits between
//     the floor and the ceiling (midRangeBoxed, G37-fixed) a LONG entry is
//     refused mid_range_against_rejection (break_lock.go courseRangeFilter).
//   - It ends when a LATER candle touches the floor's top (Low <= top): the
//     move the rejection called has arrived. Mirror for an FTGL reject (Low
//     <= the floor's top, Close above) → SHORT entries refused until a candle
//     reaches the ceiling's bottom.
//   - A new reject of the other edge replaces it; a reject whose box is no
//     longer live (the 17:00 CT roll, a box retired) is dropped.
//
// Knob risk_control.mentor_tuning.mentor_mid_range_reject_side (default ON);
// OFF never builds the state.

// MidRangeReject is the standing box-edge rejection.
type MidRangeReject struct {
	Side   Side   `json:"side"`    // the side the rejection points: short after a ceiling reject
	At     int64  `json:"at"`      // the rejecting 1m candle's OpenTime
	BoxKey string `json:"box_key"` // the rejected box
}

// advanceMidRangeReject steps the rejection on the closed 1m candle cur.
func (e *Evaluator) advanceMidRangeReject(cur market.Kline, boxes []Box) {
	if !e.Cfg.MidRangeRejectSide {
		e.State.MidRangeReject = nil
		return
	}
	if r := e.State.MidRangeReject; r != nil {
		live := false
		for _, b := range boxes {
			if b.Key == r.BoxKey {
				live = true
				break
			}
		}
		switch {
		case !live:
			e.State.MidRangeReject = nil
		case cur.OpenTime > r.At && rejectReachedOtherEdge(r.Side, cur, boxes):
			e.State.MidRangeReject = nil
			e.count("mid_range_reject_done")
		}
	}
	for _, b := range boxes {
		var side Side
		switch boxEffectiveKind(b) {
		case FTGH:
			if cur.High >= b.Bottom && cur.Close < b.Bottom {
				side = SideShort
			}
		case FTGL:
			if cur.Low <= b.Top && cur.Close > b.Top {
				side = SideLong
			}
		}
		if side == "" {
			continue
		}
		if r := e.State.MidRangeReject; r == nil || r.Side != side || r.BoxKey != b.Key {
			e.count("mid_range_reject_" + string(side))
		}
		e.State.MidRangeReject = &MidRangeReject{Side: side, At: cur.OpenTime, BoxKey: b.Key}
	}
}

// rejectReachedOtherEdge reports whether cur reached the edge the rejection
// pointed at: a short rejection ends at a floor's top, a long one at a
// ceiling's bottom.
func rejectReachedOtherEdge(side Side, cur market.Kline, boxes []Box) bool {
	for _, b := range boxes {
		k := boxEffectiveKind(b)
		if side == SideShort && k == FTGL && cur.Low <= b.Top {
			return true
		}
		if side == SideLong && k == FTGH && cur.High >= b.Bottom {
			return true
		}
	}
	return false
}

// midRangeRejectRefuses reports whether an entry sits mid-range (between the
// floor and the ceiling, effective roles) AGAINST the standing rejection.
func midRangeRejectRefuses(r *MidRangeReject, boxes []Box, in Intent) bool {
	if r == nil || in.Side == "" || in.Side == r.Side {
		return false
	}
	return midRangeBoxed(boxes, in.Price)
}
