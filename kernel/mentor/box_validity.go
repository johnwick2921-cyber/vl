package mentor

import (
	"time"

	"vl/kernel"
	"vl/market"
)

// m473 B1 — FTGH/FTGL box validity: from the RTH open (08:30 CT) until the
// NEXT RTH open 08:30 CT, about 23-24 h — not killed at the 17:00 CT day roll.
// "Nó sẽ valid cho tới khi market open ngày mai … 8 giờ rưỡi sáng … 23 tiếng
// 24 tiếng gì đó cũng được" [Oct D3.3 @ 40:40-41:20] (OctD3.3-R51, R55); a
// Friday box still works on the live Sunday chart [Oct D3.3 @ 17:00] (R27).
// Supersedes the September "dùng box này hoài" [D3.2 p2 @ 06:21].
//
// The video states nothing about a box that forms in the EVENING, so the
// existing rule stands: the newest box per role wins (liveBoxes); a carried
// box only fills a role that has no box of its own.

// boxExpiryMs is when a carried box stops being valid: the first RTH open
// (08:30 CT) after the RTH open of the box's trading day. A box standing at
// the open — including one drawn from the pre-open peaks (about 08:00) and
// confirmed before 08:30 — is "valid from market open" to the next open
// [Oct D3.3 @ 41:28-41:36] (OctD3.3-R51, R52, frame s_02464). A box formed
// after that open expires at the first open after its own formation.
func boxExpiryMs(b Box) int64 {
	t := time.UnixMilli(b.FormedAtMs).In(ctime())
	day := t
	if t.Hour() >= 17 { // the 17:00 CT roll: the box belongs to the next trading day
		day = t.AddDate(0, 0, 1)
	}
	open := time.Date(day.Year(), day.Month(), day.Day(), 8, 30, 0, 0, ctime()).UnixMilli()
	from := b.FormedAtMs
	if open > from {
		from = open
	}
	return nextRTHOpenAfterMs(from)
}

// nextRTHOpenAfterMs is the first RTH open (08:30 CT on a day the CME is
// open at that minute — weekends and holidays follow the calendar) strictly
// after ms.
func nextRTHOpenAfterMs(ms int64) int64 {
	t := time.UnixMilli(ms).In(ctime())
	for d := 0; d <= 14; d++ {
		c := time.Date(t.Year(), t.Month(), t.Day()+d, 8, 30, 0, 0, ctime())
		if c.UnixMilli() > ms && kernel.IsCMEOpen(c) {
			return c.UnixMilli()
		}
	}
	return time.Date(t.Year(), t.Month(), t.Day()+1, 8, 30, 0, 0, ctime()).UnixMilli()
}

// boxCarryRTH switches the m473 B1 carry (frozen pre-m473 goldens set it false).
var boxCarryRTH = true

// carryRTHBoxes runs at the day roll, BEFORE the latch clears: each role's
// live box standing at the RTH open is carried to the next RTH open.
func (e *Evaluator) carryRTHBoxes(now int64) {
	if !boxCarryRTH {
		return
	}
	for _, b := range liveBoxes(e.State.LatchedBoxes, e.State.PostEscapeBoxes) {
		if now >= boxExpiryMs(b) {
			continue
		}
		if e.State.CarriedBoxes == nil {
			e.State.CarriedBoxes = map[string]Box{}
		}
		e.State.CarriedBoxes[string(b.Kind)] = b
		e.note("box %s %.2f–%.2f (formed %s CT) carried past the 17:00 CT roll — valid to the next RTH open 08:30 CT [Oct D3.3 @ 40:40-41:20]", string(b.Kind), b.Bottom, b.Top, kernel.TableTimeCT(time.UnixMilli(b.FormedAtMs)))
	}
}

// carriedBoxesAt is the pure read of the carried set at `now` (no state
// writes, no notes): the still-valid carried boxes for roles `boxes` lacks,
// refreshed on a copy. SeedChartDisplay uses it directly (display-only
// contract); withCarriedBoxes adds the expiry bookkeeping.
func (e *Evaluator) carriedBoxesAt(boxes []Box, bars []market.Kline, now int64) []Box {
	for _, kind := range []BoxKind{FTGL, FTGH} {
		b, ok := e.State.CarriedBoxes[string(kind)]
		if !ok || now >= boxExpiryMs(b) {
			continue
		}
		have := false
		for _, x := range boxes {
			if x.Kind == kind {
				have = true
				break
			}
		}
		if have {
			continue
		}
		refreshBox(bars, &b, e.Cfg.Box)
		boxes = append(boxes, b)
	}
	return boxes
}

// withCarriedBoxes expires carried boxes at their next RTH open and appends
// the still-valid ones for any role the live set lacks (floor, then ceiling).
func (e *Evaluator) withCarriedBoxes(boxes []Box, bars []market.Kline, now int64) []Box {
	for role, b := range e.State.CarriedBoxes {
		if now >= boxExpiryMs(b) {
			delete(e.State.CarriedBoxes, role)
			e.note("carried box %s %.2f–%.2f expired at the next RTH open 08:30 CT [Oct D3.3 @ 41:20]", role, b.Bottom, b.Top)
		}
	}
	out := e.carriedBoxesAt(boxes, bars, now)
	for _, b := range out[len(boxes):] {
		e.State.CarriedBoxes[string(b.Kind)] = b
	}
	return out
}
