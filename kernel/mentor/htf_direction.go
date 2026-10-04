package mentor

import (
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
// With no 4h trigger at all there is no direction to follow — refuse
// (fail-closed: the read always starts from the 4-hour).
func HTFVerdict(h HTF) (ok bool, side Side, reason string) {
	if h.FourH.Dir == "" {
		return false, "", "no 4h trigger yet — the 4-hour is read first, before the 1-hour [D4.4 p1 @ 02:53]"
	}
	if h.OneH.Dir == "" || h.OneH.Dir == h.FourH.Dir {
		return true, h.FourH.Dir, ""
	}
	return false, "", "case 3: 1h trigger opposite the 4h — sit out until the 1h flips to the 4h [D4.4 p1 @ 16:00; §12]"
}

// HTFConflict reports the §7 / §12 conflict: both lines drawn and opposite
// [D4.4 p1 @ 13:18 "4-hour and 1-hour triggers in conflict — especially with
// the daily range already spent"; D5.1 p1 @ 19:22].
func HTFConflict(h HTF) bool {
	return h.FourH.Dir != "" && h.OneH.Dir != "" && h.OneH.Dir != h.FourH.Dir
}

// HTFSideIsOK reports whether an entry side obeys the HTF gate. The trigger
// having FIRED sets the direction — price need NOT sit on the trigger side of
// the line [D4.4 p2 @ 03:51: "Price need NOT be on the 'right' side of the
// trigger line — the trigger having FIRED sets the direction"].
func HTFSideIsOK(h HTF, side Side) bool {
	ok, want, _ := HTFVerdict(h)
	return ok && want == side
}
