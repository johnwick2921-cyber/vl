package mentor

import (
	"strings"
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
	return HTFAdvanceAt(h, bars4h, bars1h, cfg, 0)
}

// HTFAdvanceAt is HTFAdvance with the BREAK INSTANT: breakAt is the open time
// of the 1m bar the evaluator is ticking on. A line that moves on the forming
// (tail) bucket this tick is stamped MovedAt = breakAt, the minute it really
// fired, not the bucket open — so oneHSilent compares real instants, not 4h
// vs 1h bucket opens [B3; D4.4 p1 @18:36; p2 @03:10]. breakAt 0 = bucket open
// (the legacy stamp).
func HTFAdvanceAt(h HTF, bars4h, bars1h []market.Kline, cfg Config, breakAt int64) HTF {
	h.FourH = triggerTickAt(h.FourH, bars4h, 240, cfg, breakAt)
	h.OneH = triggerTickAt(h.OneH, bars1h, 60, cfg, breakAt)
	return h
}

// HTFVerdict is the three cases verbatim from the mentor's screen
// [D4.4 p1 @14:08–15:34 — the written list; first spoken 05:17–06:46. @16:00 only restates case 1]:
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
	return false, "", "case 3: 1h trigger opposite the 4h — sit out until the 1h flips to the 4h [D4.4 p1 @ 15:09–15:33] (§12)"
}

// HTFRefusal names WHY the shared 4h/1h check refused an intraday entry
// (m462). Every setup prefixes it: isb_htf_conflict, phl_htf_side_mismatch,
// box_htf_no_4h, rolling_htf_conflict, … — one name per setup, one counter.
type HTFRefusal string

const (
	// HTFRefuseNo4h — no 4h trigger at all: "the 4-hour is read first"
	// [D4.4 p1 @02:53; D5.1-R8 D5.1 p1 @08:29–09:13] — nothing to follow.
	HTFRefuseNo4h HTFRefusal = "htf_no_4h"
	// HTFRefuseConflict — case 3: the 1h trigger (fired at/after the 4h)
	// points opposite the 4h → sit out until the 1h flips to the 4h
	// [D4.4 p1 @15:09–15:33; D5.1-R11 D5.1 p1 @08:29–09:52, @11:27–11:50
	// "Trigger 4h, trigger 1h nằm ngược nhau… mình không làm được gì hết trơn"].
	HTFRefuseConflict HTFRefusal = "htf_conflict"
	// HTFRefuseSide — case 1/2: the entry's side is against the 4h direction
	// (1h agreeing or silent) — "entries only with the 4h" [D4.4 p1
	// @16:10–16:20; case 2 @17:11–18:52; D5.1-R20(a) D5.1 p1 @19:11–20:10].
	HTFRefuseSide HTFRefusal = "htf_side_mismatch"
)

// HTFEntryCheck is the ONE shared 4h/1h check every INTRADAY mentor entry
// path calls (m462): ISB, reverse ISB (incl. its CX-6 PHL/PLH retag), level
// PHL/PLH, rolling PHL/PLH, FTGH/FTGL box returns — and htfEntryFilter, the
// backstop at Tick's exit, re-runs it on every entry intent so a path that
// forgets its own call is still gated. Trendline / wick-microscalp / flag-wall
// are TARGET paths on those same entries, so they are covered through them.
// side "" checks only the setup-wide verdict (no 4h / case 3).
//
// It returns "" (allowed) when the gate is off — HTF.GateOff, which Tick sets
// from HTFGateActive: since m462 the DEFAULT gate is ALL SESSION (D5.1-R9 [D5.1
// p1 @08:46–11:27] "Lúc nào khi anh chị open ra, không cần phải là news");
// only an explicitly stored htf_gate_news_only=true keeps the old windows.
//
// SWING4H never calls it: the swing's direction is the 4h EMA34 — "4 giờ đụng
// resistance thì mình đánh xuống chứ không lẽ đánh lên?" [D5.2-R7, D5.2 p3
// @15:04–18:25] — not the 4h/1h trigger lines.
func HTFEntryCheck(h HTF, side Side) (HTFRefusal, string) {
	ok, dir, why := HTFVerdict(h)
	if !ok {
		if h.FourH.Dir == "" {
			return HTFRefuseNo4h, why
		}
		return HTFRefuseConflict, why
	}
	if side != "" && dir != "" && side != dir {
		return HTFRefuseSide, "entry side " + string(side) + " against the " + string(dir) + " 4h trigger — entries only with the 4h direction [D4.4 p1 @16:10–16:20; case 2 @17:11–18:52]"
	}
	return "", ""
}

// htfEntryFilter is the m462 backstop: the shared HTFEntryCheck over every
// intraday entry intent that is about to leave Tick. The per-setup paths
// already refuse by name before they emit, so on a correct build this drops
// nothing; it exists so that NO intraday entry path (present or future) can
// trade through a 4h/1h conflict unnamed. SWING4H is exempt (D5.2-R7).
func htfEntryFilter(out []Intent, h HTF) (kept []Intent, refusals []string) {
	kept = out[:0:0]
	for _, in := range out {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && !htfExempt(in) {
			if r, _ := HTFEntryCheck(h, in.Side); r != "" {
				refusals = append(refusals, htfRefusalName(in.Setup, r))
				continue
			}
		}
		kept = append(kept, in)
	}
	return kept, refusals
}

// htfExempt — the SWING4H runs its own §8 machine on the 4h EMA34 [D5.2-R7].
func htfExempt(in Intent) bool {
	return in.Setup == "SWING4H" || strings.HasPrefix(in.Reason, "swing")
}

// htfRefusalName is the per-setup refusal name: the setup prefix + the kind.
// PLH shares the "phl" prefix (one setup family, as the level path counts it).
func htfRefusalName(setup string, r HTFRefusal) string {
	p := strings.ToLower(setup)
	if p == "plh" {
		p = "phl"
	}
	return p + "_" + string(r)
}

// HTFGateActive — D4.4-11 [D4.4 p1 @13:44–14:06, @24:48]: reports whether the
// 4h/1h direction gate APPLIES at `now`. With HTFGateNewsOnly on (the default,
// the mentor's rule) the gate applies
//
//   - on a PRINT DAY (cfg.PrintWindows lists today's red-folder 07:30 print),
//     from the print window's start (07:20 CT) through the 08:30 CT open — the
//     whole news period, because the post-print news trade lives in
//     07:35–08:30 and the course locks it to the 4h: "có news hay không có
//     news đi nữa thì mình nhắm theo khung 4 giờ … khung 4 giờ trigger buy mà
//     khung 1 giờ trigger sell … nghỉ trade khúc đó" [D4.4 p1 @13:18–13:33,
//     @18:03–18:52] (A9). On a day with NO print the 07:20–07:35 window is
//     ordinary trading, and the gate "NÊN SỬ DỤNG CHO NEWS TRƯỚC … đừng sử
//     dụng cho thường" [@14:01–14:06] is not applied there (R7). The trader
//     holds the 07:20–07:35 window fail-closed when the calendar is missing,
//     so an empty list never opens that window to an entry; AND
//   - at the 08:30 OPEN — "open nó cũng giống news thôi" [D4.4 p2 @00:35–00:46].
//     D4.4 p2 gives no LENGTH for the open window, so the 60 minutes
//     08:30–09:30 CT is OURS (owner-accepted INVENTED, borrowed from the
//     mentor's 60-minute entry window [example: D1.2 p1 @23:52–24:59]; B28 —
//     a mentor question: how long after the open does the 4h/1h read govern?).
//
// With the knob off (owner decision A 2026-10-21 m134 all-day, REVERTED by
// #45) it applies all day.
func HTFGateActive(now int64, cfg Config) bool {
	if !cfg.HTFGateNewsOnly {
		return true
	}
	t := time.UnixMilli(now).In(ctime())
	mins := t.Hour()*60 + t.Minute()
	// The open window 08:30–09:30 CT, every day.
	if mins >= 8*60+30 && mins <= 9*60+30 {
		return true
	}
	// The news period: print-window start to the 08:30 CT open, print days only.
	for _, w := range cfg.PrintWindows {
		if now >= w.FromMs && now < newsPeriodEnd(w) {
			return true
		}
	}
	return false
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

// HTFAgreement reports §5.4 case 1: BOTH the 4h and the 1h trigger stand and
// point the SAME direction (the 1h is not silent). It is the exact inverse of
// HTFConflict — the premise a DayOff latch was built on. Owner ruling
// 2026-10-09: a latched DayOff clears the moment this becomes true.
func HTFAgreement(h HTF) (ok bool, dir Side) {
	if h.FourH.Dir == "" || oneHSilent(h) || h.OneH.Dir != h.FourH.Dir {
		return false, ""
	}
	return true, h.FourH.Dir
}

// HTFAgrees reports whether the 4h AND the 1h trigger BOTH stand and point
// the entry's side — case 1 of the mentor's screen [D4.4 p1 @16:10–16:20 (also 05:04–05:15)]. Case 2
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
