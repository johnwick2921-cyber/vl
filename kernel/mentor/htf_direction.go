package mentor

import (
	"time"

	"vl/market"
)

// HTF is the §5.4 higher-timeframe direction state [D4.4 p1 @ 02:53 "READ THE
// 4-HOUR FIRST — deliberately backwards", @ 03:21]. The 4-hour may have
// triggered hours earlier; the 1-hour tells the state NOW. Both lines use the
// same break rule as the 5-minute line (Law 2), kept as persisted state: they
// survive across days, exactly as the mentor's chart lines do.
type HTF struct {
	FourH TriggerLine
	OneH  TriggerLine
	// GateOff — D4.4-11: the 4h/1h DIRECTION gate is off outside the news
	// window (the course uses the HTF read for news first, not ordinary
	// trading yet). Transient, recomputed every tick; never persisted.
	GateOff bool `json:"-"`
}

// HTFAdvance feeds the closed 4h/1h bars since the last tick into the two
// trigger lines (aggregated by the evaluator from the 1m series). Pure:
// state in, state out.
func HTFAdvance(h HTF, bars4h, bars1h []market.Kline, cfg Config) HTF {
	h.FourH = TriggerTick(h.FourH, bars4h, 240, cfg)
	h.OneH = TriggerTick(h.OneH, bars1h, 60, cfg)
	return h
}

// HTFVerdict is the three cases verbatim from the mentor's screen
// [D4.4 p1 @ 16:00]:
//
//  1. 4H trigger + 1h trigger same direction            → trade that side.
//  2. 4H trigger, 1h "ko có gì hết" (nothing)           → follow the 4h.
//  3. 4H trigger, 1h trigger OPPOSITE                   → sit out until the
//     1h flips to the 4h ("Ngồi chờ khi nào 1h trigger buy theo khung 4h
//     thì trade").
//
// The 1h "ko có gì hết" case includes a 1h line that fired BEFORE the 4h
// (oneHSilent): an earlier 1h trigger is ignored [D4.4 p1 @18:36; p2 @03:10].
//
// With no 4h trigger at all there is no direction to follow — refuse
// (fail-closed: the read always starts from the 4-hour).
func HTFVerdict(h HTF) (ok bool, side Side, reason string) {
	if h.GateOff {
		return true, "", "" // D4.4-11: the HTF direction gate is off outside the news window
	}
	if h.FourH.Dir == "" {
		return false, "", "no 4h trigger yet — the 4-hour is read first, before the 1-hour [D4.4 p1 @ 02:53]"
	}
	if oneHSilent(h) || h.OneH.Dir == h.FourH.Dir {
		return true, h.FourH.Dir, ""
	}
	return false, "", "case 3: 1h trigger opposite the 4h — sit out until the 1h flips to the 4h [D4.4 p1 @ 16:00; §12]"
}

// HTFGateActive — D4.4-11 [D4.4 p1 @13:44–14:06, @24:48]: reports whether the
// 4h/1h direction gate APPLIES at `now`. With HTFGateNewsOnly on (the course
// default) the gate applies only inside the 07:20–07:35 CT news window (the
// T1 print window time; the calendar-day refinement is the trader's news
// seam — DS-102 item 18). With the knob off the gate applies all day (legacy).
func HTFGateActive(now int64, cfg Config) bool {
	if !cfg.HTFGateNewsOnly {
		return true
	}
	t := time.UnixMilli(now).In(ctime())
	mins := t.Hour()*60 + t.Minute()
	return mins >= 7*60+20 && mins <= 7*60+35 // 07:20–07:35 CT
}

// oneHSilent reports whether the 1h has nothing NEW to say after the 4h:
// either it never drew a line, or its last move is OLDER than the 4h line's
// last move — an earlier 1h trigger is ignored and the 1h reads "silent", so
// case 2 follows the 4h [D4.4 p1 @18:36; p2 @03:10] (D4.4-05, item 19).
func oneHSilent(h HTF) bool {
	return h.OneH.Dir == "" || h.OneH.MovedAt < h.FourH.MovedAt
}

// HTFConflict reports the §7 / §12 conflict: both lines drawn and opposite
// [D4.4 p1 @ 13:18 "4-hour and 1-hour triggers in conflict — especially with
// the daily range already spent"; D5.1 p1 @ 19:22].
func HTFConflict(h HTF) bool {
	if h.FourH.Dir == "" || oneHSilent(h) {
		return false
	}
	return h.OneH.Dir != h.FourH.Dir
}

// HTFAgrees reports whether the 4h AND the 1h trigger BOTH stand and point
// the entry's side — case 1 of the mentor's screen [D4.4 p1 @16:00]. Case 2
// (1h "ko có gì hết", follow the 4h) trades but is not agreement; case 3
// (opposite) never reaches an entry. It gates the 20-contract size tier.
func HTFAgrees(h HTF, side Side) bool {
	return side != "" && h.FourH.Dir == side && !oneHSilent(h) && h.OneH.Dir == side
}

// stampHTFAgree stamps HTFAgree on every ENTRY intent from the evaluator's
// 4h/1h state, where the intents leave Tick (next to stampLeave).
func stampHTFAgree(out []Intent, h HTF) {
	for i := range out {
		if out[i].Action == PlaceStopEntry || out[i].Action == PlaceStopLimitEntry {
			out[i].HTFAgree = HTFAgrees(h, out[i].Side)
		}
	}
}

// HTFSideIsOK reports whether an entry side obeys the HTF gate. The trigger
// having FIRED sets the direction — price need NOT sit on the trigger side of
// the line [D4.4 p2 @ 03:51: "Price need NOT be on the 'right' side of the
// trigger line — the trigger having FIRED sets the direction"].
func HTFSideIsOK(h HTF, side Side) bool {
	ok, want, _ := HTFVerdict(h)
	return ok && want == side
}
