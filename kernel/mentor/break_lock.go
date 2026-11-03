package mentor

import (
	"strings"

	"vl/market"
)

// ── The BREAK DIRECTION LOCK (m466 P1, release #61: G21 + G29) ─────────────
//
// D4.1-R31 [D4.1 p1 @18:29–20:33]: "Break ra phải có setup để mình đánh…
// break UP → inside bar long hoặc potential high-low; break DOWN → inside bar
// short / PLH". D4.1-R33 applies it to "the 5m box, anything — một cái vùng
// range, FTGH/FTGL, inside-bar box". D4.1-R55 [D4.1 p1 @07:47–08:07] "thoát
// [đầu] nào thì mình trade [đầu] đó"; D3.4-R32 [D3.4 p2 @03:13–04:30] "thoát
// đầu nào đánh đầu đó". D4.1-R30: a break that closes back inside FAILED.
//
// Pinned definition (triage-61, no new number):
//
//   - SET by a break event on the current closed 1m candle: an FTGH/FTGL box's
//     FIRST body escape (boxEscapeIndex — the D3.2-R16 body test, unchanged),
//     or a 5m / 15m (incl. the #60 provisional) / 30m ISB rest-box escape (the
//     G20 1m close test). The lock carries the break side and the broken box's
//     range [Lo, Hi].
//   - While it stands every intraday ENTRY on the other side — ISB, reverse
//     ISB, PHL/PLH (level and rolling), box return, level reject — is refused
//     break_counter_setup (named, counted). The swing (§8) runs its own
//     machine and is exempt. The break candle itself is never an entry (a
//     break is not a setup — boxBanFilter, unchanged).
//   - CLEARED by a later closed 1m candle that CLOSES back inside [Lo, Hi] —
//     or through it to the far side (breakFailed) — the failed break of
//     D4.1-R30 [D4.1 p1 @17:43–18:29] "bull flag đi lên xong bắn ngược xuống, cũng
//     như không" (counted break_lock_failed), by an opposite break
//     (the lock flips, counted break_lock_flipped), or at the 17:00 CT roll.
//
// Knob risk_control.mentor_tuning.mentor_break_direction_lock (default ON);
// OFF never builds the state, so the JSON is the pre-#61 one.

// BreakLock is one standing break direction.
type BreakLock struct {
	Side Side    `json:"side"` // the break direction: only this side trades
	Lo   float64 `json:"lo"`   // the broken box's bottom
	Hi   float64 `json:"hi"`   // the broken box's top
	At   int64   `json:"at"`   // the break candle's OpenTime (1m)
	Src  string  `json:"src"`  // ftgh | ftgl | isb_5m | isb_15m | isb_15m_forming | isb_30m
	Day  string  `json:"day"`  // the 17:00 CT trading day the lock belongs to
}

// breakEvent is one box break seen on the current closed 1m candle.
type breakEvent struct {
	Side   Side
	Lo, Hi float64
	Src    string
}

// isbBreakEvent is the break event of an ISB rest box that escaped toward dir.
func isbBreakEvent(box ISBBox, dir Side, src string) breakEvent {
	if box.Provisional {
		src += "_forming"
	}
	return breakEvent{Side: dir, Lo: box.Low, Hi: box.High, Src: src}
}

// boxBreakEvents reports the FTGH/FTGL boxes whose FIRST body escape
// (boxEscapeOpenMs → boxEscapeIndex, the D14 flip candle) is the candle that
// closed at cur: an FTGH escaped UP → a long break, an FTGL escaped DOWN → a
// short break. A box that escaped earlier is no event (its break was read
// when it happened). A base-frame box reads its own frame — the event is the
// tick its escape candle closes; a "2m" box escapes on the 1m (m410).
func boxBreakEvents(bars []market.Kline, boxes []Box, baseCfg BoxCfg, cur market.Kline) []breakEvent {
	var out []breakEvent
	for _, b := range boxes {
		escMs, ok := boxEscapeOpenMs(bars, b, baseCfg)
		if !ok {
			continue
		}
		tfMs := int64(60_000)
		if b.TF != "2m" {
			switch baseCfg.TF {
			case "2m":
				tfMs = 2 * 60_000
			case "5mha":
				tfMs = 5 * 60_000
			}
		}
		if escMs+tfMs != cur.OpenTime+60_000 {
			continue
		}
		switch b.Kind {
		case FTGH:
			out = append(out, breakEvent{Side: SideLong, Lo: b.Bottom, Hi: b.Top, Src: "ftgh"})
		case FTGL:
			out = append(out, breakEvent{Side: SideShort, Lo: b.Bottom, Hi: b.Top, Src: "ftgl"})
		}
	}
	return out
}

// advanceBreakLock steps the lock on the closed 1m candle cur: the day roll,
// then the failed break (a close back inside the broken box, never on the
// break candle itself), then this candle's break events (the last one wins;
// an opposite side is counted as a flip).
func (e *Evaluator) advanceBreakLock(cur market.Kline, now int64, events []breakEvent) {
	if !e.Cfg.BreakDirectionLock {
		e.State.BreakLock = nil
		return
	}
	day := boxLatchDayKey(now, e.Cfg)
	if l := e.State.BreakLock; l != nil && l.Day != day {
		e.State.BreakLock = nil
		e.count("break_lock_day_roll")
	}
	if l := e.State.BreakLock; l != nil && cur.OpenTime > l.At && breakFailed(l, cur.Close) {
		e.State.BreakLock = nil
		e.count("break_lock_failed")
		e.note("break lock %s (%s %.2f–%.2f) cleared: the 1m close %.2f at %s CT is back through the broken edge — the break failed [D4.1-R30 p1 @17:43–18:29]",
			l.Side, l.Src, l.Lo, l.Hi, cur.Close, swingCT(cur.OpenTime))
	}
	for _, ev := range events {
		if ev.Side == "" {
			continue
		}
		prev := e.State.BreakLock
		if prev != nil && prev.Side != ev.Side {
			e.count("break_lock_flipped")
		}
		e.State.BreakLock = &BreakLock{Side: ev.Side, Lo: ev.Lo, Hi: ev.Hi, At: cur.OpenTime, Src: ev.Src, Day: day}
		e.count("break_lock_set_" + ev.Src)
		if prev == nil || prev.Side != ev.Side {
			e.note("break lock %s set: the %s box %.2f–%.2f broke on the 1m close %.2f at %s CT — only %s setups until a close back inside [D4.1-R31/R33 p1 @18:29–20:33; D4.1-R55 @07:47–08:07]",
				ev.Side, ev.Src, ev.Lo, ev.Hi, cur.Close, swingCT(cur.OpenTime), ev.Side)
		}
	}
}

// breakFailed reports whether a 1m close undid the break: back inside the
// broken box — or straight through it to the far side (a one-candle reversal
// leaves the break just as failed) — i.e. on the inner side of the broken edge.
func breakFailed(l *BreakLock, close float64) bool {
	if l.Side == SideLong {
		return close <= l.Hi
	}
	return close >= l.Lo
}

// courseRangeFilter applies the three m466 P1 range rules to every intraday
// ENTRY intent still standing (the swing is exempt, like every late filter):
//
//   - break_counter_setup — G21/G29: an entry against the standing break lock;
//   - range_too_small_big_candles — G33: an entry priced inside the standing
//     too-small range (range_small.go);
//   - mid_range_against_rejection — G43: a mid-range entry against the last
//     box-edge rejection (reject_side.go).
//
// Each drop is named and counted. A LEVEL entry (the touch loop's "lvl-" arm)
// is re-read every tick while its touch stands, so it counts once per visit
// of the level (refuseOncePerVisit — r22); every other entry counts per drop.
// The arms of dropped entries are un-registered by Tick's exit pass (m431).
func (e *Evaluator) courseRangeFilter(out []Intent, boxes []Box) []Intent {
	lock, small, rej := e.State.BreakLock, e.State.SmallRange, e.State.MidRangeReject
	if lock == nil && small == nil && rej == nil {
		return out
	}
	kept := out[:0:0]
	for _, in := range out {
		if (in.Action != PlaceStopEntry && in.Action != PlaceStopLimitEntry) || strings.HasPrefix(in.Reason, "swing") {
			kept = append(kept, in)
			continue
		}
		name := ""
		switch {
		case lock != nil && in.Side != "" && in.Side != lock.Side:
			name = "break_counter_setup"
		case smallRangeRefuses(small, in):
			name = "range_too_small_big_candles"
		case midRangeRejectRefuses(rej, boxes, in):
			name = "mid_range_against_rejection"
		}
		if name == "" {
			kept = append(kept, in)
			continue
		}
		if in.AnchorKey != "" && in.AnchorKey != rollingLevelKey && strings.HasPrefix(in.ArmID, "lvl-") {
			e.refuseOncePerVisit(in.AnchorKey, name)
		} else {
			e.refuse(name)
		}
	}
	return kept
}
