package trader

import (
	"errors"

	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── EX-01: THE RUNNER'S LIVE 1:1 AT THE LEG-1 FILL (full re-read 11-16) ──────
//
// October: once in profit "việc làm duy nhất của mình là 1 là mình bán bớt 2 là
// mình kéo stop lên giữ cái mức reward 1 1 đừng làm gì khác hết" (the only jobs:
// 1 sell part, 2 pull the stop up to keep 1:1) [Oct D1.2 @26:40-26:57], and keep
// 1:1 "khi anh chị đã vô lệnh rồi và nến nó đang chạy" (even while the candle
// is still running) [D1.2 @17:45-18:14]. The sale and the stop pull are one
// action. The bot sold leg 1 on the broker's TP receipt and pulled nothing
// until the NEXT closed candle: 11-16 19:14 ISB, leg 1 sold 3 @ 27366.50 at
// 19:15:00, the runner stayed on its original 27345.25 and was stopped there
// at 19:15:40, twenty seconds before the 19:16:00 exit pass (trader_fills
// 738-741).
//
// Now the receipt owes the move (mentorOweRunnerRR11) and wakes the event pass;
// the pass moves every runner leg still on the wire to the live 1:1
// max(stop, 2·price − T) (long; mirrored short) — price = leg 1's fill, T = the
// leg's own target — through the one production move (mentorMoveLegStop: never
// widen, judged against the broker book, verified). Break-even is NOT part of
// this move: it stays where October puts it — after the partial on the closed
// candle (the R3 stage) and on the resonance path [D4.2 @12:02-12:27].
// Knob mentor_runner_1to1_at_fill (nil -> ON); false reverts: the
// receipt only latches Scaled and the runner waits for the next closed candle.

// mentorRunnerRR11AtFillOn reads mentor_runner_1to1_at_fill (nil -> ON).
func mentorRunnerRR11AtFillOn(rc *store.RiskControlConfig) bool {
	if rc == nil || rc.MentorTuning == nil || rc.MentorTuning.MentorRunnerRR11AtFill == nil {
		return true
	}
	return *rc.MentorTuning.MentorRunnerRR11AtFill
}

// mentorOweRunnerRR11 records, under mentorExitMu, that the runner's live 1:1
// is owed at the leg-1 fill price px. Only a SPLIT position (leg 1 on its own
// wire) with a known fill price owes it; mode C (the stop never moves) and the
// swing (held by the 4h) never do. The knob is read by the drain, not here.
// Returns true when a move is now owed (the caller pokes the event loop).
func (at *AutoTrader) mentorOweRunnerRR11(key string, px float64) bool {
	if at == nil || key == "" || px <= 0 {
		return false
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	lp, ok := at.mentorLivePos[key]
	if !ok || lp == nil || lp.Legs[0].Wire != 1 {
		return false
	}
	if lp.Pos.Mode == "C" || lp.Pos.Mode == "swing" {
		return false
	}
	lp.RunnerRR11Owed = true
	lp.RunnerRR11Px = px
	return true
}

// mentorDrainRunnerRR11AtFill sends every owed at-fill runner move. It runs
// under mentorEvalMu: from mentorEvalOnce (the event pass the receipt woke,
// before the once-per-bar watermark) and at the top of mentorExitDrive (so a
// closed candle never drives a runner whose owed move has not gone out).
func (at *AutoTrader) mentorDrainRunnerRR11AtFill() {
	if at == nil || !at.mentorEnabled() {
		return
	}
	var owed []*mentorLivePos
	var pxs []float64
	at.mentorExitMu.Lock()
	for _, lp := range at.mentorLivePos {
		if lp != nil && lp.RunnerRR11Owed {
			owed = append(owed, lp)
			pxs = append(pxs, lp.RunnerRR11Px)
		}
	}
	at.mentorExitMu.Unlock()
	if len(owed) == 0 {
		return
	}
	on := mentorRunnerRR11AtFillOn(at.mentorRiskControl())
	nt := at.armedTrader()
	if on && nt == nil {
		return // no trader yet — keep it owed; the next pass (or candle) sends it
	}
	for i, p := range owed {
		at.mentorExitMu.Lock()
		p.RunnerRR11Owed = false
		at.mentorExitMu.Unlock()
		if !on {
			at.mentorCount("runner_rr11_at_partial_fill_off")
			continue
		}
		at.mentorRunnerRR11AtFill(nt, p, pxs[i])
	}
}

// mentorRunnerRR11AtFill moves one position's runner leg(s) to the live 1:1 at
// the leg-1 fill price px. Never wider (mentorRR11Stop keeps the stop when the
// candidate is not tighter; mentorMoveLegStop refuses a widen against the belief
// AND the broker).
func (at *AutoTrader) mentorRunnerRR11AtFill(nt *ntTrader.TCPTrader, p *mentorLivePos, px float64) {
	pos := &p.Pos
	side := pos.Side
	runners := mentorRunnerLegs(p, at.mentorExitedLegs(p))
	if len(runners) == 0 {
		at.mentorCount("runner_rr11_at_partial_fill_no_runner")
		return
	}
	moved := false
	for _, leg := range runners {
		target := leg.TP
		if target == 0 {
			target = pos.Target
		}
		if target == 0 {
			at.mentorCount("runner_rr11_at_partial_fill_no_target")
			continue
		}
		if (side == "long" && target <= px) || (side != "long" && target >= px) {
			// leg 1 sold AT (or past) this leg's own target — its take-profit is
			// filling with it (an ISB whose trade target is the +1R): the "1:1"
			// would be the market itself, and a stop there is a market close.
			at.mentorCount("runner_rr11_at_partial_fill_target_reached")
			continue
		}
		next := mentorRR11Stop(side, leg.Stop, px, target)
		if !mentorStopTighter(side, next, leg.Stop) {
			at.mentorCount("runner_rr11_at_partial_fill_not_tighter")
			continue
		}
		err := at.mentorMoveLegStop(nt, side, leg, next)
		if errors.Is(err, errMentorStopThroughMarket) {
			// the market already traded through the 1:1 level — the leg was closed
			// at market (a full flatten); a sibling leg's move would be redundant.
			at.mentorCount("stop_move_skipped_leg_closed")
			return
		}
		if !mentorMoveApplied(err) {
			if errors.Is(err, errMentorLegClosed) {
				at.mentorCount("stop_move_skipped_leg_closed")
				continue
			}
			if err != nil && !mentorMoveErrAlreadyLogged(err) {
				at.logWarnf("🧑‍🏫 mentor runner 1:1 at the leg-1 fill: %s leg %d move FAILED: %v — the next closed candle drives it", leg.SignalID, leg.Wire, err)
			}
			if errors.Is(err, errMentorStopBrokerTighter) {
				pos.Stop = leg.Stop // the broker-tighter refusal corrected the belief
			}
			continue
		}
		leg.Stop = next
		pos.Stop = next
		moved = true
		at.mentorCount("runner_rr11_at_partial_fill")
		at.logInfof("🧑‍🏫 mentor runner 1:1 at the leg-1 fill: %s leg %d stop → %.2f = 2·%.2f − %.2f (leg 1 sold at %.2f; the runner keeps 1:1 at once, not on the next candle) [EX-01; Oct D1.2 @26:40-26:57, @17:45-18:14]",
			leg.SignalID, leg.Wire, next, px, target, px)
	}
	if moved {
		p.RunnerRR11Moved = true
	}
}
