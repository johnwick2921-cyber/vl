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
//   - Ceiling reject: a closed 1m candle with High >= a ceiling box's bottom
//     and Close < it → the rejection points SHORT: while the entry sits
//     between the floor and the ceiling (midRangeBoxed, G37) a LONG entry is
//     refused mid_range_against_rejection (break_lock.go courseRangeFilter).
//     The box is a ceiling by its EFFECTIVE role (boxEffectiveKind — an FTGL
//     that escaped down is resistance) or because the candle came at it from
//     below (Open under the bottom — the side-of-price role of G37) AND it is
//     a box that has played the ceiling role: a native FTGH, or a flipped box
//     (the same test as midRangeBoxed). Release #61 review round 2 (course
//     lens P3): an un-flipped FTGL hit from below is not a "reject FTGH"
//     (D2.2-R71) and no longer sets a short-pointing rejection (mirror: an
//     un-flipped FTGH hit from above sets no long one).
//   - It ends when a LATER candle reaches a box below the rejected one (Low <=
//     that box's top): the move the rejection called has arrived. Mirror for a
//     floor reject (Low <= the floor's top, Close above) → SHORT entries
//     refused until a candle reaches a box above it (High >= its bottom).
//   - A new reject replaces it; a reject whose box is no longer live (the
//     17:00 CT roll, a box retired) is dropped.
//
// Knob risk_control.mentor_tuning.mentor_mid_range_reject_side (default ON);
// OFF never builds the state.

// MidRangeReject is the standing box-edge rejection.
type MidRangeReject struct {
	Side   Side    `json:"side"`    // the side the rejection points: short after a ceiling reject
	At     int64   `json:"at"`      // the rejecting 1m candle's OpenTime
	BoxKey string  `json:"box_key"` // the rejected box
	Lo     float64 `json:"lo"`      // the rejected box's bottom
	Hi     float64 `json:"hi"`      // the rejected box's top
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
		case cur.OpenTime > r.At && rejectReachedOtherEdge(r, cur, boxes):
			e.State.MidRangeReject = nil
			e.count("mid_range_reject_done")
		}
	}
	for _, b := range boxes {
		var side Side
		k := boxEffectiveKind(b)
		switch {
		case cur.High >= b.Bottom && cur.Close < b.Bottom && (k == FTGH || cur.Open < b.Bottom && (b.Kind == FTGH || b.Flipped)):
			side = SideShort
		case cur.Low <= b.Top && cur.Close > b.Top && (k == FTGL || cur.Open > b.Top && (b.Kind == FTGL || b.Flipped)):
			side = SideLong
		}
		if side == "" {
			continue
		}
		if r := e.State.MidRangeReject; r == nil || r.Side != side || r.BoxKey != b.Key {
			e.count("mid_range_reject_" + string(side))
		}
		e.State.MidRangeReject = &MidRangeReject{Side: side, At: cur.OpenTime, BoxKey: b.Key, Lo: b.Bottom, Hi: b.Top}
	}
}

// rejectReachedOtherEdge reports whether cur reached the box the rejection
// pointed at: a short (ceiling) rejection ends at the top of a box below the
// rejected one, a long (floor) rejection at the bottom of a box above it.
func rejectReachedOtherEdge(r *MidRangeReject, cur market.Kline, boxes []Box) bool {
	for _, b := range boxes {
		if b.Key == r.BoxKey {
			continue
		}
		if r.Side == SideShort && b.Top < r.Lo && cur.Low <= b.Top {
			return true
		}
		if r.Side == SideLong && b.Bottom > r.Hi && cur.High >= b.Bottom {
			return true
		}
	}
	return false
}

// midRangeRejectRefuses reports whether an entry sits mid-range (between the
// floor and the ceiling, side-of-price roles — G37) AGAINST the standing
// rejection.
func midRangeRejectRefuses(r *MidRangeReject, boxes []Box, in Intent) bool {
	if r == nil || in.Side == "" || in.Side == r.Side {
		return false
	}
	return midRangeBoxed(boxes, in.Price)
}
