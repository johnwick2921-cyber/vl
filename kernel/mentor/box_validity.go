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
// (08:30 CT) after the box's start of validity. A box standing at the open —
// including one drawn from the pre-open peaks (about 08:00) and confirmed
// before 08:30 on its own calendar day — is "valid from market open" to the
// next open [Oct D3.3 @ 41:28-41:36] (OctD3.3-R51, R52, frame s_02464): its
// start is raised to that day's 08:30. Any other box (formed at or after 08:30,
// INCLUDING a Globex-evening box formed at or after 17:00 CT) expires at the
// first open after its own formation: Thu 18:00 -> Fri 08:30, Sun 18:00 ->
// Mon 08:30, never one open later ("valid until the market opens tomorrow,
// 23-24 h" [Oct D3.3 @ 40:40-41:20], OctD3.3-R51). round-2 fix: the old code
// pushed an evening box to the NEXT day's open, then one open further (38-86 h).
func boxExpiryMs(b Box) int64 {
	t := time.UnixMilli(b.FormedAtMs).In(ctime())
	from := b.FormedAtMs
	if t.Hour() < 17 {
		open := time.Date(t.Year(), t.Month(), t.Day(), 8, 30, 0, 0, ctime()).UnixMilli()
		if open > from {
			from = open
		}
	}
	return nextRTHOpenAfterMs(from)
}

// boxAnchorInWindow reports whether the box's formation bar is still inside
// the bar window: refreshBox fails closed on an absent anchor and would keep a
// stale window index (a carried box scrolled out of the 1500-bar window).
func boxAnchorInWindow(bars []market.Kline, b Box, cfg BoxCfg) bool {
	src := bars
	if b.TF != "2m" {
		src = barsForTF(bars, cfg.TF)
	}
	_, ok := resolveBarIndex(src, b.FormedAtMs)
	return ok
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
// writes, no notes): the still-valid carried boxes, refreshed on a copy.
// SeedChartDisplay uses it directly (display-only contract); withCarriedBoxes
// adds the expiry bookkeeping.
//
// D3-G4: by default (BoxCarriedStaysLive) a carried box takes its role from any
// box the live set holds for it — a Friday box "vẫn còn valid trên chart Chủ nhật"
// [Oct D3.3 @ 17:00] (OctD3.3-R27; E9: Sun 1 Nov 21:53 the Friday FTGH is the box
// the course shorts) and is valid "cho tới sáng ngày mai" [@ 41:28] (R51), so a
// box that formed later in the Sunday evening does not hide it. Knob OFF
// (box_carried_stays_live=false): the m473 rule — a carried box only fills a role
// the live set lacks. `used` lists the carried boxes now in the returned set.
func (e *Evaluator) carriedBoxesAt(boxes []Box, bars []market.Kline, now int64) (out, used []Box) {
	out = append([]Box(nil), boxes...)
	for _, kind := range []BoxKind{FTGL, FTGH} {
		b, ok := e.State.CarriedBoxes[string(kind)]
		if !ok || now >= boxExpiryMs(b) {
			continue
		}
		have := -1
		for i, x := range out {
			if x.Kind == kind {
				have = i
				break
			}
		}
		if have >= 0 && !e.Cfg.BoxCarriedStaysLive {
			continue
		}
		if !boxAnchorInWindow(bars, b, e.withBoxFlags(e.Cfg.Box)) {
			continue // anchor scrolled out of the window: never trust the stale index
		}
		refreshBox(bars, &b, e.withBoxFlags(e.Cfg.Box))
		if have >= 0 {
			out[have] = b
		} else {
			out = append(out, b)
		}
		used = append(used, b)
	}
	return out, used
}

// withCarriedBoxes expires carried boxes at their next RTH open and returns the
// live set with the still-valid carried boxes applied (floor, then ceiling).
func (e *Evaluator) withCarriedBoxes(boxes []Box, bars []market.Kline, now int64) []Box {
	for role, b := range e.State.CarriedBoxes {
		if now >= boxExpiryMs(b) {
			delete(e.State.CarriedBoxes, role)
			e.note("carried box %s %.2f–%.2f expired at the next RTH open 08:30 CT [Oct D3.3 @ 41:20]", role, b.Bottom, b.Top)
		}
	}
	out, used := e.carriedBoxesAt(boxes, bars, now)
	for _, b := range used {
		e.State.CarriedBoxes[string(b.Kind)] = b
	}
	return out
}
