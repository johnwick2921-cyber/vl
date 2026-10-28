package mentor

import (
	"fmt"

	"vl/market"
)

// RollingPHL is the D2.2 pullback-continuation rolling stop-entry state
// (m216 scope: the PULLBACK leg only — a candle touching a DRAWN level stays
// first-touch-fixed in touch.go and never rolls). During a pullback in a trend
// the entry is a buy stop (uptrend) / sell stop (downtrend) placed on the
// extreme of EACH closed candle and re-armed every candle until it fills:
//
//	"Mình ra một lệnh buy stop trên đỉnh của từng cây nến đã đóng xong"
//	[D2.2 p1 @02:44]; "Cây nến này đóng xong / Nên mình ra tiếp"
//	[@03:22–03:24]; the mirror sell stop "Tôi ra một lệnh sell stop đã sẵn"
//	[@11:05–11:19]; "Cây nến nào vượt qua được … cây nến trước đó / Market
//	nó sẽ fill lệnh của mình" [@03:36–03:44, 11:34–11:36].
//
// The entry geometry is the SAME as the level-touch PHL/PLH (entry = the
// reference candle's extreme + the course buffer, stop = the broken candle's
// other extreme, target near the old high/low) — only the reference candle
// ROLLS to each new closed candle instead of being fixed at the first touch.
type RollingPHL struct {
	Side      Side    `json:"side,omitempty"`       // the trend side (long = pullback in an uptrend)
	TargetPx  float64 `json:"target_px,omitempty"`  // the old extreme the leg targets (observability)
	PriorPx   float64 `json:"prior_px,omitempty"`   // the structural prior swing (higher-low / lower-high)
	ArmID     string  `json:"arm_id,omitempty"`     // the resting rolling order (a "lvl-" arm)
	LastArmed int64   `json:"last_armed,omitempty"` // CloseTime of the last armed candle
	LastPrice float64 `json:"last_price,omitempty"` // the armed entry price (the fill test reads it)
}

// rollingLevelKey is the LevelArms key the rolling order rests under. Reusing
// the LevelArms map lets the trader's existing reconcile (mentorReconcileLevelArms)
// clear the entry the moment the order fills / is cancelled, so the evaluator
// stops re-arming a leg that already filled — no new trader hook.
const rollingLevelKey = "__rolling__"

// levelTouchRejectActive reports whether any CURRENT level is rejecting on the
// given side (and not ISB-only). A level touch OWNS the entry — the
// first-touch-fixed rule [D2.3 p1 @17:55–19:12; D5.3 p1 @22:10] — so the
// rolling path must never double it. This is the m216 "never conflict" guard:
// a level-touch entry does NOT roll; a pullback entry DOES.
//
// m436: only touches of levels STILL in the level set count. Touches is written
// only for levels in the set and nothing prunes an entry when its level leaves
// it (an old extreme wicked through by the touching candle, a replaced S/R, a
// deleted key level or trendline), so an orphan TouchReject used to own the
// entry for that side forever — across days too — and silently killed every
// roll. A level that is gone owns nothing: its touch can never produce a
// level entry (the PHL/PLH loop iterates `levels`).
func levelTouchRejectActive(st *State, side Side, levels []Level) bool {
	live := make(map[string]struct{}, len(levels))
	for _, l := range levels {
		live[l.Key] = struct{}{}
	}
	for key, tr := range st.Touches {
		if tr.Outcome != TouchReject || tr.ApproachedFrom == "" || st.ISBOnly[key] {
			continue
		}
		if _, ok := live[key]; !ok {
			continue
		}
		if tr.ApproachedFrom == side {
			return true
		}
	}
	return false
}

// rollingTrendSide is the side the rolling pullback leg follows: the gate-aware
// HTF verdict inside the news / open windows, and the 4h trigger direction
// alone outside them (m436).
//
// HTFVerdict returns an EMPTY side whenever the 4h/1h direction gate is OFF
// (HTF.GateOff, the default outside 08:30–09:30 CT and the print window — "NÊN
// SỬ DỤNG CHO NEWS TRƯỚC … đừng sử dụng cho thường" [D4.4 p1 @14:01–14:06]).
// The level PHL/PLH tolerates that (its side comes from the touch), but the
// rolling leg's whole job is to continue THE TREND, so an empty side cancelled
// every resting roll at 09:30 and started none for the rest of the day. The
// pullback continuation of D2.2 has no time window in the lesson — the trend is
// the 4h read the mentor starts from ("Ngồi chờ khi nào 1h trigger buy theo
// khung 4 giờ thì trade" is the GATE's sit-out, off outside the window) [D2.2
// p1 @02:44, @11:05–11:19; D4.4 p1 @02:53]. With the gate off the 4h direction
// IS the trend; no 4h trigger at all = no direction = no continuation.
func rollingTrendSide(h HTF) Side {
	if _, side, _ := HTFVerdict(h); side != "" {
		return side
	}
	if h.GateOff {
		return h.FourH.Dir
	}
	return ""
}

// rollingCancel emits the cancel for a resting rolling order and resets the
// state (the setup died — the mentor re-armed every candle, so a dead leg has
// no order left resting).
func (e *Evaluator) rollingCancel(st *RollingPHL, why string) []Intent {
	if st == nil || st.ArmID == "" {
		*st = RollingPHL{}
		return nil
	}
	out := []Intent{{Action: CancelArm, ArmID: st.ArmID, Reason: why}}
	if _, ok := e.State.LevelArms[rollingLevelKey]; ok {
		delete(e.State.LevelArms, rollingLevelKey)
	}
	*st = RollingPHL{}
	return out
}

// rollingPHLTick evaluates the D2.2 rolling pullback entry once per tick and
// returns the CancelArm + PlaceStopEntry intents that re-arm the stop to the
// newest closed exec candle. It reuses PHLPLHGatedR2Levels — the SAME gates
// (HTF direction, day gate, spent cap, room, floor, higher-low) as the level
// PHL/PLH — with the reference candle = execCur instead of a touch bar.
func (e *Evaluator) rollingPHLTick(bars []market.Kline, cur, execCur market.Kline, execTFMin int, oldExtremes []oldExtreme, levels []Level, boxes []Box, dg DayGate) []Intent {
	st := &e.State.RollingPHL

	if !e.Cfg.RollingPHL || !e.Cfg.Enabled {
		return e.rollingCancel(st, "rolling PHL/PLH disabled — cancel the resting roll")
	}

	// Self-contained FILL: the candle that trades through the armed stop fills
	// it [D2.2 p1 @03:36–03:44] — the leg is over, no cancel (it already
	// filled), no re-arm. Read from the bars, never the trader's ledger, so the
	// live bot and the full-day replay harness stay byte-identical.
	if st.ArmID != "" && st.LastPrice > 0 {
		filled := st.Side == SideLong && execCur.High >= st.LastPrice ||
			st.Side == SideShort && execCur.Low <= st.LastPrice
		if filled {
			*st = RollingPHL{}
			if _, ok := e.State.LevelArms[rollingLevelKey]; ok {
				delete(e.State.LevelArms, rollingLevelKey)
			}
			return nil
		}
	}

	// 1. Trend direction (the 4h read, §5.4). No direction = no continuation.
	side := rollingTrendSide(e.State.HTF)
	if side == "" {
		return e.rollingCancel(st, "no HTF trend direction — the rolling pullback leg has no side")
	}

	// 1b. The PULLBACK precondition (c52 W6, plan A8): the ladder starts only
	// once the pullback SHOWS — in an uptrend the retrace candle is RED ("Nó xả
	// nến đỏ … Nó rớt ngược lại" [D2.2 p1 @01:52–01:57]), in a downtrend it is
	// GREEN ("bật nến xanh" [D2.2 p1 @10:47–10:51; @10:58]). A with-trend candle
	// is the impulse, not the pullback — arming a stop on it chased the move.
	// A with-trend candle mid-pullback does NOT roll or start a stop (the stop on
	// the previous pullback candle's extreme still stands and fills the moment
	// price exceeds it) — but it still runs EVERY invalidation below (side
	// flipped, old extreme gone, higher-low broken, trigger disagrees) and
	// cancels a resting order whose leg died. Skipping only the ROLL (not the
	// checks) is the D1 fix: an early return here let a resting long survive an
	// HTF flip to short whenever the next candle happened to be with-trend.
	withTrend := !rollingPullbackCandle(execCur, side)
	if withTrend && st.ArmID == "" {
		return nil // nothing resting to invalidate, nothing to start
	}
	// The resting order belongs to the OTHER side (HTF flipped): it is dead
	// whatever the new side's checks below say.
	if withTrend && st.Side != side {
		return e.rollingCancel(st, "the HTF side flipped — cancel the resting roll of the old side")
	}

	// 2. The target is the NEAREST old extreme on the trade side (B14b), read
	// from the reference candle's extreme exactly like the level path's tref.
	tref := execCur.High
	if side == SideShort {
		tref = execCur.Low
	}
	ex, found := nearestOldExtremeOnSide(oldExtremes, tref, side)
	if !found {
		return e.rollingCancel(st, "no old extreme on the trend side — the rolling leg has no target")
	}

	// 3. The entry must sit far enough from the old extreme (the 1–2 pullback
	// candle wait [D2.2 p2 @05:25, 07:02]).
	if len(bars)-1-ex.idx < e.Cfg.PHLMinCandlesFromExtreme {
		return e.rollingCancel(st, "too close to the old extreme — wait 1–2 more pullback candles [D2.2 p2 @ 05:25, 07:02]")
	}

	// 4. m216: a level touch-reject on this side OWNS the entry (first-touch
	// fixed). The rolling is the no-level case — never both in one tick.
	if levelTouchRejectActive(&e.State, side, levels) {
		return e.rollingCancel(st, "a level touch owns the entry (first-touch fixed) — the rolling pullback does not double it [D2.3 p1 @17:55–19:12]")
	}

	// 4b. m436: the PHL/PLH LOCATION bans. The course bans them for EVERY PHL/PLH
	// ("No PHL, no PLH, no opposite ISB" inside a standing 5m ISB box [D3.4 p2
	// @07:27–10:35; D3.2 p2 @08:51]; none between two boxes or mid-range [D3.2
	// p2 @08:34–08:55; D2.2 p3 @14:03–16:03]; none next to a box [D3.2 p1
	// @21:53–23:08]; sit out a candle as big as the key-level rank [X4 @06:31]),
	// and the rolling path skipped all of them once it became default ON. They
	// run here through the SAME functions the level touch loop calls
	// (phlLocationBan / phlNearBoxRefuses), on the rolling entry price, the
	// newest closed 1m candle and the old extreme the leg targets (the rolling
	// setup's place, in.Anchor). Named "rolling_<level-path name>" and counted
	// once per reference candle; a ban cancels a resting roll (the leg's
	// location is dead).
	tr := Touch{Outcome: TouchReject, RefBar: execCur, ApproachedFrom: side} // RejectEntry maps SideLong → long, SideShort → short
	if _, price, okEntry := RejectEntry(tr, e.Cfg); okEntry {
		banKey := fmt.Sprintf("%s@%d", rollingLevelKey, execCur.CloseTime)
		if name, _ := e.phlLocationBan(ex.level, side, price, cur, levels, boxes); name != "" {
			e.refuseOncePerVisit(banKey, "rolling_"+name)
			return e.rollingCancel(st, "the rolling entry sits in a banned PHL/PLH location ("+name+") — cancel the resting roll")
		}
		if phlNearBoxRefuses(e.Cfg, boxes, tref, side, execCur.High-execCur.Low) {
			e.refuseOncePerVisit(banKey, "rolling_near_box")
			return e.rollingCancel(st, "the rolling entry is too close to a box — cancel the resting roll [D3.2 p1 @21:53–23:08]")
		}
	}

	// 5. The higher-low / lower-high check, fail closed (D2-28).
	prior, okLeft := priorLeftExtremeOnTape(bars, ex, oldExtremes)
	if !okLeft {
		return e.rollingCancel(st, "no left structural extreme — the rolling PHL/PLH is refused rather than emitted unchecked")
	}

	// 6. Build through the SAME gated path as the level PHL/PLH, with the
	// reference candle = the newest closed exec candle (the rolling ref).
	in, ok, _ := PHLPLHGatedR2Levels(tr, ex.level, ex.idx, len(bars)-1, prior, levels, e.Cfg, e.State.HTF, e.State.Day.Verdict, dg, false)
	if !ok {
		// The leg no longer qualifies on this candle (higher-low broken, room,
		// floor, spent cap, HTF flip, …). The mentor re-arms every candle, so a
		// dead leg has no valid order left — cancel the resting one.
		return e.rollingCancel(st, "the rolling leg no longer qualifies — cancel the resting roll")
	}

	// 6b. The 5m trigger side (c52 W6, plan A8 / X8-R3b): "lúc nào mình cũng
	// phải nhìn khung 5 phút" [X8 part1 @06:25; @08:37–08:48] — the rolling
	// entry must agree with the 5m trigger line like every other entry path.
	// Below the buy line / above the sell line, or against the line's side, the
	// roll is refused and a resting order is cancelled (the leg is dead). The
	// same LocTriggerFilter switch the level path honours turns it off; an empty
	// trigger passes (TriggerVerdict). Counted once per candle.
	if e.Cfg.LocTriggerFilter {
		if dirOK, trigSide, _ := TriggerVerdict(e.State.Trigger, in.Price); !dirOK || trigSide != "" && trigSide != side {
			e.refuseOncePerVisit(fmt.Sprintf("%s@%d", rollingLevelKey, execCur.CloseTime), "rolling_phl_trigger_side")
			return e.rollingCancel(st, "the 5m trigger side disagrees with the rolling leg — cancel the resting roll [X8 part1 @06:25, 08:37–08:48]")
		}
	}

	// A with-trend candle only validates: the leg is still alive, so the
	// resting order stands and nothing new is emitted (D1).
	if withTrend {
		return nil
	}

	// Already armed on THIS candle: a 1m tick on an odd minute re-reads the same
	// closed 2m exec candle (F2) — never re-emit the same roll twice.
	if st.ArmID != "" && st.LastArmed == execCur.CloseTime {
		return nil
	}

	e.State.ArmSeq++
	id := fmt.Sprintf("lvl-%d", e.State.ArmSeq)
	in.ArmID = id
	in.AnchorKey = rollingLevelKey
	in.Anchor = ex.level.Price
	in.Reason = "PHL/PLH rolling pullback: " + in.Reason
	in.ExecTFMin = execTFMin // 1m, 2m or 5m (m417)
	in.RunnerTarget = nextLevelBeyond(levels, ex.level.Price, side)
	in.Confluence = false

	var out []Intent
	if st.ArmID != "" {
		out = append(out, Intent{Action: CancelArm, ArmID: st.ArmID,
			Reason: "rolling pullback re-arms to the next closed candle — replace the stop [D2.2 p1 @03:22–03:24]"})
	}
	out = append(out, in)

	if e.State.LevelArms == nil {
		e.State.LevelArms = map[string]LevelArm{}
	}
	e.State.LevelArms[rollingLevelKey] = LevelArm{ArmID: id, Side: in.Side, LevelPrice: ex.level.Price, PlacedAt: execCur.CloseTime}
	*st = RollingPHL{Side: side, TargetPx: ex.level.Price, PriorPx: prior, ArmID: id, LastArmed: execCur.CloseTime, LastPrice: in.Price}
	return out
}

// rollingPullbackCandle reports whether the reference candle is the visible
// pullback of the trend side: RED (close < open) for a long (uptrend), GREEN
// (close > open) for a short (downtrend) [D2.2 p1 @01:52–01:57, @10:47–10:51].
// A doji is neither — no pullback shown, no roll.
func rollingPullbackCandle(c market.Kline, side Side) bool {
	if side == SideShort {
		return c.Close > c.Open
	}
	return c.Close < c.Open
}
