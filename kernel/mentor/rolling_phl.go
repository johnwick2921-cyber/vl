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

// levelTouchRejectActive reports whether any level is currently rejecting on
// the given side (and not ISB-only). A level touch OWNS the entry — the
// first-touch-fixed rule [D2.3 p1 @17:55–19:12; D5.3 p1 @22:10] — so the
// rolling path must never double it. This is the m216 "never conflict" guard:
// a level-touch entry does NOT roll; a pullback entry DOES.
func levelTouchRejectActive(st *State, side Side) bool {
	for key, tr := range st.Touches {
		if tr.Outcome != TouchReject || tr.ApproachedFrom == "" || st.ISBOnly[key] {
			continue
		}
		if tr.ApproachedFrom == side {
			return true
		}
	}
	return false
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
func (e *Evaluator) rollingPHLTick(bars []market.Kline, execCur market.Kline, execTFMin int, oldExtremes []oldExtreme, levels []Level, dg DayGate) []Intent {
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
	_, side, _ := HTFVerdict(e.State.HTF)
	if side == "" {
		return e.rollingCancel(st, "no HTF trend direction — the rolling pullback leg has no side")
	}

	// 1b. The PULLBACK precondition (c52 W6, plan A8): the ladder starts only
	// once the pullback SHOWS — in an uptrend the retrace candle is RED ("Nó xả
	// nến đỏ … Nó rớt ngược lại" [D2.2 p1 @01:52–01:57]), in a downtrend it is
	// GREEN ("bật nến xanh" [D2.2 p1 @10:47–10:51; @10:58]). A with-trend candle
	// is the impulse, not the pullback — arming a stop on it chased the move.
	// A with-trend candle mid-pullback does NOT kill a resting order (the stop
	// on the previous pullback candle's extreme still stands and fills the
	// moment price exceeds it); it just does not roll or start one.
	if !rollingPullbackCandle(execCur, side) {
		return nil
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
	if levelTouchRejectActive(&e.State, side) {
		return e.rollingCancel(st, "a level touch owns the entry (first-touch fixed) — the rolling pullback does not double it [D2.3 p1 @17:55–19:12]")
	}

	// 5. The higher-low / lower-high check, fail closed (D2-28).
	prior, okLeft := priorLeftExtremeOnTape(bars, ex, oldExtremes)
	if !okLeft {
		return e.rollingCancel(st, "no left structural extreme — the rolling PHL/PLH is refused rather than emitted unchecked")
	}

	// 6. Build through the SAME gated path as the level PHL/PLH, with the
	// reference candle = the newest closed exec candle (the rolling ref).
	approach := side // RejectEntry maps SideLong → long, SideShort → short
	tr := Touch{Outcome: TouchReject, RefBar: execCur, ApproachedFrom: approach}
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
