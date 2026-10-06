package trader

import (
	"math"
	"strings"
	"sync"
	"time"

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
	if split, ok := mentorSentSplit(at, r.SignalID); ok {
		if split.Leg1Qty < n {
			// Full split: leg 1 (-sl/-tp, its own TP) + the runner (-sl2/-tp2,
			// the trade target).
			leg1, runner := split.Leg1Qty, n-split.Leg1Qty
			lp.Pos.Leg1, lp.Pos.Leg2, lp.Pos.Leg1TP = leg1, runner, split.Leg1TP
			lp.Legs[0] = mentorLeg{SignalID: r.SignalID, Qty: leg1, TP: split.Leg1TP, Stop: r.StopPx, Final: false, Wire: 1}
			lp.Legs[1] = mentorLeg{SignalID: r.SignalID, Qty: runner, TP: r.TargetPx, Stop: r.StopPx, Final: true, Wire: 2}
			mentorCount("exit_drive_split_registered")
		} else {
			// I10: a partial fill ≤ leg 1's quantity means ONLY leg 1 is live —
			// the wire holds only the leg-1 bracket at 1:1. Register leg 1 only,
			// with Leg1TP = split.Leg1TP (the 1:1 target), NOT the trade target.
			lp.Pos.Leg1, lp.Pos.Leg2 = n, 0
			lp.Pos.Leg1TP = split.Leg1TP
			lp.Legs[0] = mentorLeg{SignalID: r.SignalID, Qty: n, TP: split.Leg1TP, Stop: r.StopPx, Final: false, Wire: 1}
			mentorCount("exit_drive_leg1_only_registered")
		}
	}
	// I13 (merge of #423 into the #422 build/register split): the staged exit
	// branch has been read into lp.Pos.Mode above, so it is consumed — prune it
	// here so the per-arm map does not grow for the life of the process (every
	// registration path — full fill, B1 expiry, I4 cancel — goes through here).
	at.deleteMentorExitMode(r.Scenario)
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
	// I9 (U3): forget the split record ONLY when the armed row is TERMINAL.
	// While the row is still working its REMAINDER may still be at the broker —
	// a forget here would leave a later re-registration without its split legs.
	if at.mentorArmedRowTerminal(key) {
		if nt := at.armedTrader(); nt != nil {
			nt.ForgetSignalMaps(key)
		}
	}
}

// mentorArmedRowTerminal reports whether the armed row carrying signalID has
// reached a TERMINAL state. An unknown/unreadable row reads terminal (fail-safe
// for the forget — a read failure never blocks the unregister).
func (at *AutoTrader) mentorArmedRowTerminal(signalID string) bool {
	if at == nil || signalID == "" || at.store == nil {
		return true
	}
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		return true
	}
	var r store.ArmedOrderDB
	if err := ledger.DB().Where("signal_id = ?", signalID).First(&r).Error; err != nil {
		return true
	}
	return store.IsTerminalArmState(r.State)
}

// mentorCloseReceiptMs resolves the receipt instant from the position_close
// payload: the broker's ExitTime (RFC3339) when parseable, else the clock now.
func mentorCloseReceiptMs(p ntwire.PositionClosePayload) int64 {
	if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(p.ExitTime)); err == nil {
		return ts.UnixMilli()
	}
	return mentorClockNow().UnixMilli()
}

// mentorLeg1ExitedBefore (I6) is the pure next-candle rule: leg 1 counts as
// "exited on a PRIOR candle" only when its confirmation instant predates the
// candle's OPEN. exitedAtMs <= 0 means no confirmation (a single bracket, which
// is candle-marked) — treated as prior, so the single-leg trail keeps its old
// timing.
func mentorLeg1ExitedBefore(exitedAtMs, candleOpenMs int64) bool {
	if exitedAtMs <= 0 {
		return true
	}
	return exitedAtMs < candleOpenMs
}

// mentorScaledState reads the shared Scaled/Leg1ExitedAtMs under mentorExitMu —
// the receipt (mentorMarkLeg1Scaled) and the I7 fallback write them from other
// goroutines, so an unlocked read races (I6).
func (at *AutoTrader) mentorScaledState(p *mentorLivePos) (scaled bool, exitedAtMs int64) {
	if at == nil || p == nil {
		return false, 0
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	return p.Pos.Scaled, p.Pos.Leg1ExitedAtMs
}

// mentorSetScaledCandle marks Scaled for the SINGLE-leg candle-priced 1:1 point
// (no confirmation instant — Leg1ExitedAtMs stays 0). Guarded because Pos is
// shared. Returns true when this call FIRST marked it.
func (at *AutoTrader) mentorSetScaledCandle(p *mentorLivePos) bool {
	if at == nil || p == nil {
		return false
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	if p.Pos.Scaled {
		return false
	}
	p.Pos.Scaled = true
	return true
}

// mentorLatchLeg1Scaled marks a live position's leg 1 as scaled (confirmed
// exited) at the given instant, under mentorExitMu. Idempotent — a later latch
// never rewinds an earlier one. Returns true when this call FIRST marked it.
func (at *AutoTrader) mentorLatchLeg1Scaled(key string, atMs int64) bool {
	if at == nil || key == "" {
		return false
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	lp, ok := at.mentorLivePos[key]
	if !ok || lp == nil || lp.Pos.Scaled {
		return false
	}
	lp.Pos.Scaled = true
	lp.Pos.Leg1ExitedAtMs = atMs
	return true
}

// mentorMarkLeg1Scaled (B2, BUILD-ALL L9 bookkeeping half) marks a live position
// scaled ONLY on the broker's position_close receipt of LEG 1's TP. A whole-
// position close (leg 0), a runner close (leg 2), a stop exit, or a manual close
// is NOT a scale-out and leaves Scaled untouched. The candle-price guess that
// used to set Scaled in the exit drive is gone — the trail now begins only once
// the broker confirms leg 1 actually exited (and only on candles that OPEN after
// that confirmation, I6).
func (at *AutoTrader) mentorMarkLeg1Scaled(p ntwire.PositionClosePayload) {
	if at == nil || p.Leg != 1 || p.ExitReason != "tp" || p.SignalID == "" {
		return
	}
	if at.mentorLatchLeg1Scaled(p.SignalID, mentorCloseReceiptMs(p)) {
		mentorCount("leg1_at_target")
		at.logInfof("🧑‍🏫 mentor leg 1 scaled on the broker TP receipt: %s (%s)", p.SignalID, p.PositionSide)
	}
}

// mentorRegisterPartialFillIfAbsent (B1 + I4) registers a partially filled
// mentor arm's FILLED quantity in the exit drive (register-if-absent) and pokes
// the drive + bumps the funnel. No-op unless the row is a partially filled
// mentor arm with a signal id. Returns true when it registered.
func (at *AutoTrader) mentorRegisterPartialFillIfAbsent(r store.ArmedOrderDB) bool {
	if !isMentorArmOrigin(r) || strings.TrimSpace(r.SignalID) == "" || r.FillQuantity <= 0 || mentorRemainderToCancel(r) <= 0 {
		return false
	}
	lp := at.mentorBuildLivePos(r, ntwire.OrderUpdatePayload{
		SignalID:  r.SignalID,
		Quantity:  r.FillQuantity,
		FillPrice: r.FillPrice,
	})
	if lp == nil || !at.mentorRegisterLivePosIfAbsent(r.SignalID, lp) {
		return false
	}
	at.pokeMentorExitDrive()
	at.mentorFunnel.bumpFilled()
	return true
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
