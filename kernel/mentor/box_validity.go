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

// rthFormed reports whether ms is inside RTH (08:30 <= CT < 17:00).
func rthFormed(ms int64) bool {
	t := time.UnixMilli(ms).In(ctime())
	m := t.Hour()*60 + t.Minute()
	return m >= 8*60+30 && m < 17*60
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

// carryRTHBoxes runs at the day roll, BEFORE the latch clears: each role's
// live RTH-formed box is carried to the next RTH open.
func (e *Evaluator) carryRTHBoxes(now int64) {
	for _, b := range liveBoxes(e.State.LatchedBoxes, e.State.PostEscapeBoxes) {
		if !rthFormed(b.FormedAtMs) || now >= nextRTHOpenAfterMs(b.FormedAtMs) {
			continue
		}
		if e.State.CarriedBoxes == nil {
			e.State.CarriedBoxes = map[string]Box{}
		}
		e.State.CarriedBoxes[string(b.Kind)] = b
		e.note("box %s %.2f–%.2f (formed %s CT) carried past the 17:00 CT roll — valid to the next RTH open 08:30 CT [Oct D3.3 @ 40:40-41:20]", string(b.Kind), b.Bottom, b.Top, kernel.TableTimeCT(time.UnixMilli(b.FormedAtMs)))
	}
}

// withCarriedBoxes expires carried boxes at their next RTH open and appends
// the still-valid ones for any role the live set lacks (floor, then ceiling).
func (e *Evaluator) withCarriedBoxes(boxes []Box, bars []market.Kline, now int64) []Box {
	for role, b := range e.State.CarriedBoxes {
		if now >= nextRTHOpenAfterMs(b.FormedAtMs) {
			delete(e.State.CarriedBoxes, role)
			e.note("carried box %s %.2f–%.2f expired at the next RTH open 08:30 CT [Oct D3.3 @ 41:20]", role, b.Bottom, b.Top)
		}
	}
	for _, kind := range []BoxKind{FTGL, FTGH} {
		b, ok := e.State.CarriedBoxes[string(kind)]
		if !ok {
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
		e.State.CarriedBoxes[string(kind)] = b
		boxes = append(boxes, b)
	}
	return boxes
}
