package trader

import (
	"math"
	"time"

	ntTrader "vl/trader/ninjatrader"
)

// ── C52 W1 A2: a confluence upgrade moves leg 1's TP to 2R ─────────────────
//
// [D3.4 p3 @10:07–10:33, @11:01–11:05] "em vô rồi em mới đợi trigger… nếu như
// em thấy trigger buy setup luôn… thì em hold lệnh, at least risk reward 1, 2":
// a trade entered first and confirmed by the trigger afterwards is held to at
// least 1:2, exactly like one that entered with the confluence in hand.
//
// Before this, mentorConfluenceUpgrade only flipped the branch to C (no stop
// moves, no scale-out logic) while leg 1's TP — set to +1R at the entry frame
// — kept resting, so an upgraded school-1 trade still sold half at 1R and only
// lost its BE/trail. A mode-C ENTRY gets the 2R leg-1 TP in the entry frame
// (mentorExitFork -> mentorLeg1TPForC); this is the same TP for a trade that
// BECAME C.

// mentorConfluenceTPMaxTries bounds the sends of one position's 2R move.
const mentorConfluenceTPMaxTries = 3

// mentorLeg1TPHeldForC reports whether leg 1's resting TP already sits at the
// confluence distance. 1.5R is the midpoint between the two real values (1R
// normal, 2R confluence), so a fill slipped a few ticks from the authored
// entry never looks "not at 2R" and a mode-C entry is never re-modified.
func mentorLeg1TPHeldForC(side string, entry, r, tp float64) bool {
	if tp <= 0 || r <= 0 {
		return false
	}
	if side == "short" {
		return tp <= entry-1.5*r
	}
	return tp >= entry+1.5*r
}

// mentorLeg1TPToC moves the open position's leg-1 TP to entry ± 2R
// (mentorLeg1TPForC) once. Safe to call every candle: it sends nothing when
// the position is not a split with leg 1 still resting, when the TP is already
// at the confluence distance, while an earlier TP modify awaits its NT8
// verdict, past the retry bound, or when the 2R line would sit beyond the
// runner's own TP. Every branch that sends nothing is counted and logged,
// never silent. Returns true only when a modify_bracket frame was SENT.
func (at *AutoTrader) mentorLeg1TPToC(nt *ntTrader.TCPTrader, p *mentorLivePos, src string) bool {
	if at == nil || p == nil {
		return false
	}
	pos := &p.Pos
	leg := &p.Legs[0]
	if pos.Mode != "C" {
		return false
	}
	if leg.Wire != 1 || leg.SignalID == "" || leg.Qty <= 0 || p.Legs[1].Qty <= 0 {
		// One bracket on the wire: its TP is the trade target, there is no
		// separate leg 1 to hold.
		at.mentorCount("confluence_tp_not_split")
		return false
	}
	if scaled, _ := at.mentorScaledState(p); scaled || at.mentorExitedLegs(p)[0] {
		at.mentorCount("confluence_tp_leg1_gone")
		return false
	}
	if pos.R <= 0 || pos.Entry <= 0 {
		at.mentorCount("confluence_tp_no_geometry")
		return false
	}
	at.mentorExitMu.Lock()
	pending, tries, cur := p.TPModWant > 0, p.ConfTPTries, leg.TP
	at.mentorExitMu.Unlock()
	if mentorLeg1TPHeldForC(pos.Side, pos.Entry, pos.R, cur) {
		return false // already at the confluence distance (a mode-C entry, or already moved)
	}
	if pending {
		at.mentorCount("confluence_tp_modify_pending")
		return false // an earlier leg-1 TP modify is awaiting its NT8 verdict
	}
	if tries >= mentorConfluenceTPMaxTries {
		return false // bounded: reported once at the last failure
	}
	want := mentorLeg1TPForC(pos.Entry, pos.R, pos.Side)
	if tick := at.mentorInstrumentTick(); tick > 0 {
		want = math.Round(want/tick) * tick
	}
	if runner := mentorRunnerLeg(p); runner != nil && runner.TP > 0 && mentorStopTighter(pos.Side, want, runner.TP) {
		at.mentorCount("confluence_tp_beyond_runner")
		at.logWarnf("🧑‍🏫 mentor confluence leg-1 TP %.2f NOT sent (%s) — beyond the runner's TP %.2f; leg 1 keeps %.2f", want, src, runner.TP, cur)
		return false
	}
	if modifyBracketWire == nil {
		at.mentorCount("modify_bracket_unwired")
		at.logWarnf("🧑‍🏫 mentor confluence leg-1 modify unwired (TP %.2f, %s) — logged, NOT sent; leg 1 keeps %.2f", want, src, cur)
		return false
	}
	at.mentorExitMu.Lock()
	p.ConfTPTries++
	at.mentorExitMu.Unlock()
	if err := modifyBracketWire(nt, leg.SignalID, pos.Side, 1, want); err != nil {
		at.mentorCount("modify_bracket_failed")
		at.logErrorf("🧑‍🏫 mentor confluence leg-1 modify FAILED for %s (TP %.2f, %s): %v — leg 1 keeps %.2f (try %d of %d)", leg.SignalID, want, src, err, cur, tries+1, mentorConfluenceTPMaxTries)
		return false
	}
	at.mentorCount("confluence_tp_sent")
	at.logInfof("🧑‍🏫 mentor confluence leg-1 modify SENT: %s leg 1 TP %.2f → %.2f (2R, hold ≥ 1:2) (%s) [D3.4 p3 @10:07–10:33]", leg.SignalID, cur, want, src)
	// Awaiting the NT8 verdict: the shared verifier (mentorVerifyPendingTPModifies)
	// reads the broker's "-tp" order and writes the belief; it also stops this
	// reconcile from re-sending while the verdict is outstanding.
	at.mentorExitMu.Lock()
	p.TPModWant = want
	p.TPModSentAt = time.Now()
	p.TPModStaleWarned = false
	p.TPModConfirmedWant = 0
	pos.Leg1TP = want
	at.mentorExitMu.Unlock()
	return true
}
