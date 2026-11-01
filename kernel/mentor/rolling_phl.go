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

	// m464 (release #59): the LADDER, separate from its resting rung. The
	// ladder starts on the first counter-trend candle and then re-arms on EVERY
	// closed candle, whatever its colour; a refused rung leaves no order resting
	// but the ladder goes on [D2.2-R7 p1 @02:26–03:27].
	Ladder bool    `json:"ladder,omitempty"`  // the ladder has started (a pullback candle showed)
	RungPx float64 `json:"rung_px,omitempty"` // the newest rung's entry price, armed OR refused (the reaction test reads it)
	RungAt int64   `json:"rung_at,omitempty"` // CloseTime of the newest rung candle, armed OR refused
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

// rollingCancel ENDS THE LADDER: it emits the cancel for a resting rung (if
// any) and resets the whole state. Only the conditions that kill the setup
// itself end the ladder — the flag off, no trend side / the side flipped, no
// old extreme to target, a level touch owning the entry (m216). A refused RUNG
// never ends it (m464: rollingSkipRung).
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

// dropRung forgets the resting rung only (its order is cancelled or was never
// authored); the ladder and the newest rung's price stay, so the next closed
// candle re-arms and the reaction test still reads the rung [m464].
func (st *RollingPHL) dropRung() {
	st.ArmID = ""
	st.LastArmed = 0
	st.LastPrice = 0
}

// rollingSkipRung — m464 (release #59): the rung on THIS closed candle is
// refused, but the LADDER goes on. The mentor re-places the stop on every
// closed candle ("Cây nến này đóng xong nên mình ra tiếp" [D2.2-R7 p1
// @03:22–03:24]); a rung that fails a gate (the 25-pt ceiling, the room, the
// floor, a location ban, the trigger side, …) is skipped with its NAMED reason
// (counted once per candle) and the next closed candle is the next rung. The
// resting rung of an earlier candle is cancelled — the ladder has moved past
// it, the mentor's order is no longer there — and the newest rung's would-be
// price is kept for the reaction test (rollingPHLTick step 0).
//
// The 10:13 rung of the D2.2 worked buy (H 29,403.25 L 29,375.25 — risk 29 >
// StopCeilingPts 25) is the case this exists for: the old code ended the
// ladder there and the 10:14 signal candle (29,396.25 / 29,386.00) never armed.
func (e *Evaluator) rollingSkipRung(st *RollingPHL, side Side, execCur market.Kline, price float64, name, why string) []Intent {
	e.refuseOncePerVisit(fmt.Sprintf("%s@%d", rollingLevelKey, execCur.CloseTime), name)
	var out []Intent
	if st.ArmID != "" {
		out = append(out, Intent{Action: CancelArm, ArmID: st.ArmID,
			Reason: "rolling PHL/PLH rung refused (" + name + ": " + why + ") — the stale rung is cancelled, the ladder continues on the next closed candle [D2.2-R7 p1 @02:26–03:27]"})
		if _, ok := e.State.LevelArms[rollingLevelKey]; ok {
			delete(e.State.LevelArms, rollingLevelKey)
		}
		st.dropRung()
	}
	st.Ladder = true
	st.Side = side
	st.RungPx = price
	st.RungAt = execCur.CloseTime
	return out
}

// rollingRungPrice is the rung's entry price on the reference candle: its
// extreme + the course buffer (RejectEntry, the same price the gated build
// emits) [D2.2-R10].
func rollingRungPrice(c market.Kline, side Side, cfg Config) float64 {
	if side == SideShort {
		return c.Low - cfg.PHLEntryBufferPts
	}
	return c.High + cfg.PHLEntryBufferPts
}

// rollingPHLTick evaluates the D2.2 rolling pullback LADDER once per tick and
// returns the CancelArm + PlaceStopEntry intents that re-arm the stop to the
// newest closed exec candle. It reuses PHLPLHGatedR2Levels — the SAME gates
// (HTF direction, day gate, spent cap, room, floor, higher-low) as the level
// PHL/PLH — with the reference candle = execCur instead of a touch bar.
//
// m464 (release #59), the ladder model:
//   - the colour of the candle gates only the START of the ladder (the first
//     counter-trend candle — the pullback shows [D2.2-R6/R16]);
//   - once started, EVERY closed candle is a rung, whatever its colour — the
//     mentor's last rung is the small GREEN candle of an uptrend pullback
//     [D2.2-R7, R9: O 29,390.50 H 29,395.75 L 29,386.50 C 29,394.00];
//   - a refused rung is skipped with its named reason and the ladder goes on
//     (rollingSkipRung); only the setup's own death ends it (rollingCancel);
//   - the ladder ends when a candle breaks the newest rung — the fill when it
//     was armed, the reaction the rung was for when it was refused (no chase:
//     the next candle is not a pullback rung any more) [D2.2-R8].
func (e *Evaluator) rollingPHLTick(bars []market.Kline, cur, execCur market.Kline, execTFMin int, oldExtremes []oldExtreme, levels []Level, boxes []Box, dg DayGate) []Intent {
	st := &e.State.RollingPHL

	if !e.Cfg.RollingPHL || !e.Cfg.Enabled {
		return e.rollingCancel(st, "rolling PHL/PLH disabled — cancel the resting roll")
	}

	// 0. The FILL / REACTION: the candle that trades through the newest rung
	// fills it [D2.2 p1 @03:36–03:44] — the leg is over, no cancel (an armed
	// rung already filled), no re-arm. A REFUSED rung that is broken is the
	// same event with no order of ours: the pullback reacted, the ladder is
	// over (re-arming on the impulse would chase it) — counted. Read from the
	// bars, never the trader's ledger, so the live bot and the full-day replay
	// harness stay byte-identical. The rung's own candle never "breaks" it (a
	// 1m tick re-reading the same closed 2m candle, F2).
	if st.Ladder && st.RungPx > 0 && execCur.CloseTime > st.RungAt {
		broke := st.Side == SideLong && execCur.High >= st.RungPx ||
			st.Side == SideShort && execCur.Low <= st.RungPx
		if broke {
			if st.ArmID == "" {
				e.count("rolling_ladder_reacted_no_order")
			}
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

	// 1b. The side flipped under a running ladder: that ladder is dead. A
	// candle that is the NEW side's pullback may start the new ladder below.
	var out []Intent
	if st.Ladder && st.Side != side {
		out = e.rollingCancel(st, "the HTF side flipped — cancel the resting roll of the old side")
	}

	// 1c. The START gate (c52 W6, plan A8; m464: the start ONLY): the ladder
	// starts once the pullback SHOWS — in an uptrend the retrace candle is RED
	// ("Nó xả nến đỏ … Nó rớt ngược lại" [D2.2 p1 @01:52–01:57]), in a
	// downtrend it is GREEN ("bật nến xanh" [D2.2 p1 @10:47–10:51; @10:58]). A
	// with-trend candle with no ladder running is the impulse, not the
	// pullback — arming on it would chase the move. Once the ladder runs, the
	// colour no longer matters: every closed candle is a rung [D2.2-R7].
	if !st.Ladder && !rollingPullbackCandle(execCur, side) {
		return out
	}

	// 2. The target is the NEAREST old extreme on the trade side (B14b), read
	// from the reference candle's extreme exactly like the level path's tref.
	tref := execCur.High
	if side == SideShort {
		tref = execCur.Low
	}
	ex, found := nearestOldExtremeOnSide(oldExtremes, tref, side)
	if !found {
		return append(out, e.rollingCancel(st, "no old extreme on the trend side — the rolling leg has no target")...)
	}

	// 3. m216: a level touch-reject on this side OWNS the entry (first-touch
	// fixed). The rolling is the no-level case — never both in one tick.
	if levelTouchRejectActive(&e.State, side, levels) {
		return append(out, e.rollingCancel(st, "a level touch owns the entry (first-touch fixed) — the rolling pullback does not double it [D2.3 p1 @17:55–19:12]")...)
	}

	// From here on every refusal is a RUNG refusal: named, counted once per
	// candle, the stale rung cancelled, the ladder kept (m464).
	price := rollingRungPrice(execCur, side, e.Cfg)
	skip := func(name, why string) []Intent {
		return append(out, e.rollingSkipRung(st, side, execCur, price, name, why)...)
	}

	// 4. The entry must sit far enough from the old extreme (the 1–2 pullback
	// candle wait [D2.2 p2 @05:25, 07:02]).
	if len(bars)-1-ex.idx < e.Cfg.PHLMinCandlesFromExtreme {
		return skip("rolling_phl_too_close_to_extreme", "too close to the old extreme — wait 1–2 more pullback candles [D2.2 p2 @ 05:25, 07:02]")
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
	// once per reference candle; a ban skips THIS rung (m464: the location of
	// the next candle's rung is read again).
	tr := Touch{Outcome: TouchReject, RefBar: execCur, ApproachedFrom: side} // RejectEntry maps SideLong → long, SideShort → short
	if _, rp, okEntry := RejectEntry(tr, e.Cfg); okEntry {
		if name, _ := e.phlLocationBan(ex.level, side, rp, cur, levels, boxes); name != "" {
			return skip("rolling_"+name, "the rolling entry sits in a banned PHL/PLH location")
		}
		if phlNearBoxRefuses(e.Cfg, boxes, tref, side, execCur.High-execCur.Low) {
			return skip("rolling_near_box", "the rolling entry is too close to a box [D3.2 p1 @21:53–23:08]")
		}
	}

	// 5. The higher-low / lower-high check, fail closed (D2-28).
	prior, okLeft := priorLeftExtremeOnTape(bars, ex, oldExtremes)
	if !okLeft {
		return skip("rolling_phl_no_left_low", "no left structural extreme — the rolling PHL/PLH is refused rather than emitted unchecked")
	}

	// 6. Build through the SAME gated path as the level PHL/PLH, with the
	// reference candle = the newest closed exec candle (the rolling ref). A
	// refusal (higher-low broken, room, floor, the 25-pt ceiling, spent cap,
	// HTF gate, day gate, …) is THIS rung's, named by the level path's own
	// ledger name with the rolling_ prefix (it used to cancel the leg
	// silently).
	in, ok, reason := PHLPLHGatedR2Levels(tr, ex.level, ex.idx, len(bars)-1, prior, levels, e.Cfg, e.State.HTF, e.State.Day.Verdict, dg, false)
	if !ok {
		return skip("rolling_"+phlRefusalKey(reason), reason)
	}

	// 6b. The 5m trigger side (c52 W6, plan A8 / X8-R3b): "lúc nào mình cũng
	// phải nhìn khung 5 phút" [X8 part1 @06:25; @08:37–08:48] — the rolling
	// entry must agree with the 5m trigger line like every other entry path.
	// Below the buy line / above the sell line, or against the line's side, the
	// rung is refused and a resting order is cancelled. The same
	// LocTriggerFilter switch the level path honours turns it off; an empty
	// trigger passes (TriggerVerdict). Counted once per candle.
	if e.Cfg.LocTriggerFilter {
		if dirOK, trigSide, _ := TriggerVerdict(e.State.Trigger, in.Price); !dirOK || trigSide != "" && trigSide != side {
			return skip("rolling_phl_trigger_side", "the 5m trigger side disagrees with the rolling leg [X8 part1 @06:25, 08:37–08:48]")
		}
	}

	// Already armed on THIS candle: a 1m tick on an odd minute re-reads the same
	// closed 2m exec candle (F2) — never re-emit the same roll twice.
	if st.ArmID != "" && st.LastArmed == execCur.CloseTime {
		return out
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

	if st.ArmID != "" {
		out = append(out, Intent{Action: CancelArm, ArmID: st.ArmID,
			Reason: "rolling pullback re-arms to the next closed candle — replace the stop [D2.2 p1 @03:22–03:24]"})
	}
	out = append(out, in)

	if e.State.LevelArms == nil {
		e.State.LevelArms = map[string]LevelArm{}
	}
	e.State.LevelArms[rollingLevelKey] = LevelArm{ArmID: id, Side: in.Side, LevelPrice: ex.level.Price, PlacedAt: execCur.CloseTime}
	*st = RollingPHL{Side: side, TargetPx: ex.level.Price, PriorPx: prior, ArmID: id, LastArmed: execCur.CloseTime, LastPrice: in.Price,
		Ladder: true, RungPx: in.Price, RungAt: execCur.CloseTime}
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
