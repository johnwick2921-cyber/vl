package trader

import (
	"math"
	"strings"
	"sync"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── MENTOR LIVE POSITION REGISTRY (the exit-drive's input) ─────────────────
//
// dev's mentor entry is ONE armed row → ONE position row (no split legs — #353
// was rejected and redesigned into one-entry-two-brackets). The live position
// the exit-drive loop drives is that whole position as a SINGLE leg: Legs[0] =
// the whole position (Final = the runner), Legs[1] empty. The registry is keyed
// by the entry signal id and guarded by mentorExitMu.

type mentorLeg struct {
	SignalID string  // the leg's own entry signal id (move_stop key)
	Qty      int     // contracts in this leg
	TP       float64 // the leg's own take-profit (leg 1 = +1R / fill-candle close; runner = the trade target)
	Stop     float64 // the leg's CURRENT resting stop (the loop writes back here)
	Final    bool    // marks the RUNNER — the leg that holds to the trade target
	Wire     int     // the AddOn leg this addresses on move_stop: 1 (-sl), 2 (-sl2); 0 = the single bracket (every leg)
}

type mentorLivePos struct {
	Pos           mentorPosition // the exit-driver state (mode, entry, stop, R, …)
	Legs          [2]mentorLeg   // single bracket: [0] = the whole position; split: [0] = leg 1, [1] = the runner
	FillBarOpen   int64          // the fill candle's OpenTime
	FillBarClose  float64        // the fill candle's close (ISB leg-1 TP)
	BarsSinceFill int            // closed 1m candles since the fill
	FlatReads     int            // consecutive closed candles the account read flat on this side
	RunnerTarget  float64        // #360 item 5: the level beyond the old high (resonance runner target); 0 = none
	SpentDay      bool           // §7 spent day → mode D (runner ≤ 2)
	Confluence    bool           // R2 confluence flag → mode C
}

// registerMentorLivePos (DS-107, one-row entry) builds the single-leg live
// position from the ONE filled mentor arm row and registers it under the entry
// signal id. Legs[0] = the whole position (Final = the runner); Legs[1] empty.
// Unconditional: the FULL-fill path owns the authoritative (re)registration —
// a completing fill frame may grow the position, so an overwrite is correct.
func (at *AutoTrader) registerMentorLivePos(r store.ArmedOrderDB, u ntwire.OrderUpdatePayload) {
	if lp := at.mentorBuildLivePos(r, u); lp != nil {
		at.mentorRegisterLivePos(r.SignalID, lp)
	}
}

// mentorBuildLivePos builds the single-leg live position from the ONE filled
// mentor arm row WITHOUT registering it (nil when the row names no side or
// signal). Split from registerMentorLivePos so the B1 expiry path can register
// the built position only when the signal is not already live (register-if-
// absent) instead of overwriting an in-flight position's BE/1:1/trail state.
func (at *AutoTrader) mentorBuildLivePos(r store.ArmedOrderDB, u ntwire.OrderUpdatePayload) *mentorLivePos {
	if r.SignalID == "" {
		return nil
	}
	side := strings.ToLower(strings.TrimSpace(r.Side))
	if side == "" {
		return nil
	}
	n := u.Quantity
	if n < 1 && r.FillQuantity > 0 {
		n = r.FillQuantity
	}
	if n < 1 {
		n = 1
	}
	entry := u.FillPrice
	pos := mentorPosition{
		Symbol:    at.futuresSymbol(),
		Side:      side,
		Origin:    r.Condition,
		Entry:     entry,
		Stop:      r.StopPx,
		Target:    r.TargetPx,
		R:         math.Abs(entry - r.StopPx),
		Contracts: n,
		Leg1:      n, // the whole position is one leg
		Leg2:      0,
		Mode:      at.mentorExitMode(r.Scenario), // B5 (L8): per arm/signal id, never per side
		Leg1TP:    r.TargetPx,
	}
	lp := &mentorLivePos{Pos: pos}
	if v, ok := mentorRunnerTargets.Load(r.Scenario); ok {
		lp.RunnerTarget, _ = v.(float64)
	}
	lp.Legs[0] = mentorLeg{SignalID: r.SignalID, Qty: n, TP: r.TargetPx, Stop: r.StopPx, Final: true}
	// REVIEW-SPLIT-2 P2: the split the entry frame ACTUALLY carried (after the
	// far-side gate and the leg1_tp check) registers as two legs under the one
	// signal id — leg 1 (-sl/-tp, its own TP) and the runner (-sl2/-tp2, the
	// trade target) — so every stop move names its leg. No record (single
	// bracket, or a restart lost it) → the single-leg view, whose leg-less
	// move_stop moves every live leg together (tightening only, never a widen).
	if split, ok := mentorSentSplit(at, r.SignalID); ok && split.Leg1Qty < n {
		leg1, runner := split.Leg1Qty, n-split.Leg1Qty
		lp.Pos.Leg1, lp.Pos.Leg2, lp.Pos.Leg1TP = leg1, runner, split.Leg1TP
		lp.Legs[0] = mentorLeg{SignalID: r.SignalID, Qty: leg1, TP: split.Leg1TP, Stop: r.StopPx, Final: false, Wire: 1}
		lp.Legs[1] = mentorLeg{SignalID: r.SignalID, Qty: runner, TP: r.TargetPx, Stop: r.StopPx, Final: true, Wire: 2}
		mentorCount("exit_drive_split_registered")
	}
	return lp
}

// mentorSentSplit reads the split the entry frame carried (a seam so the
// registration pin can drive it without a live AddOn connection).
var mentorSentSplit = func(at *AutoTrader, signalID string) (ntTrader.SentSplit, bool) {
	nt := at.armedTrader()
	if nt == nil {
		return ntTrader.SentSplit{}, false
	}
	return nt.SplitSentFor(signalID)
}

// mentorRegisterLivePos stores a filled position under its signal id. A nil
// position or an empty key is ignored (fail-closed: the loop only sees what was
// actually registered).
func (at *AutoTrader) mentorRegisterLivePos(key string, p *mentorLivePos) {
	if at == nil || key == "" || p == nil {
		return
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	if at.mentorLivePos == nil {
		at.mentorLivePos = map[string]*mentorLivePos{}
	}
	at.mentorLivePos[key] = p
}

// mentorRegisterLivePosIfAbsent (B1 defensive fold, CTO 2026-10-05) registers a
// position ONLY when the signal is not already live — an atomic check-and-set
// under mentorExitMu. Returns true when it wrote. The B1 expiry path calls this
// so a partial-then-expiry registration can never reset an ALREADY-registered
// position's BE-armed / Scaled / trail state mid-trade (the full-fill path owns
// the authoritative overwrite and is unchanged).
func (at *AutoTrader) mentorRegisterLivePosIfAbsent(key string, p *mentorLivePos) bool {
	if at == nil || key == "" || p == nil {
		return false
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	if at.mentorLivePos == nil {
		at.mentorLivePos = map[string]*mentorLivePos{}
	}
	if _, exists := at.mentorLivePos[key]; exists {
		return false
	}
	at.mentorLivePos[key] = p
	return true
}

// mentorUnregisterLivePos removes a position when its side goes flat.
func (at *AutoTrader) mentorUnregisterLivePos(key string) {
	if at == nil || key == "" {
		return
	}
	at.mentorExitMu.Lock()
	delete(at.mentorLivePos, key)
	at.mentorExitMu.Unlock()
	// UR-FIX U3: both legs are flat — drop the TCPTrader's per-signal split and
	// per-leg stop records so they do not grow for the life of the process.
	// This function IS the full-close path; a leg-1 partial close never reaches
	// it (the runner keeps the side open), so the split record the runner still
	// needs for SplitSentFor / MoveStopForSignalLeg survives until the full flat.
	if nt := at.armedTrader(); nt != nil {
		nt.ForgetSignalMaps(key)
	}
}

// mentorMarkLeg1Scaled (B2, BUILD-ALL L9 bookkeeping half) marks a live position
// scaled ONLY on the broker's position_close receipt of LEG 1's TP. A whole-
// position close (leg 0), a runner close (leg 2), a stop exit, or a manual close
// is NOT a scale-out and leaves Scaled untouched. The candle-price guess that
// used to set Scaled in the exit drive is gone — the trail now begins only once
// the broker confirms leg 1 actually exited.
func (at *AutoTrader) mentorMarkLeg1Scaled(p ntwire.PositionClosePayload) {
	if at == nil || p.Leg != 1 || p.ExitReason != "tp" || p.SignalID == "" {
		return
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	lp, ok := at.mentorLivePos[p.SignalID]
	if !ok || lp == nil || lp.Pos.Scaled {
		return
	}
	lp.Pos.Scaled = true
	mentorCount("leg1_at_target")
	at.logInfof("🧑‍🏫 mentor leg 1 scaled on the broker TP receipt: %s (%s)", p.SignalID, p.PositionSide)
}

// mentorLivePosList returns every live mentor position (the exit-drive loop
// iterates it). The slice is a fresh copy; the pointed-to structs are the live
// state the loop owns the write-back for.
func (at *AutoTrader) mentorLivePosList() []*mentorLivePos {
	if at == nil {
		return nil
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	out := make([]*mentorLivePos, 0, len(at.mentorLivePos))
	for _, p := range at.mentorLivePos {
		out = append(out, p)
	}
	return out
}

// mentorRunnerTargets carries an intent's RunnerTarget (#360, item 5) from the
// authoring (mentorArmIntent, keyed by the ledger scenario) to the fill-time
// live position, without a schema change.
var mentorRunnerTargets sync.Map
