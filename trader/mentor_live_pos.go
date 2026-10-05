package trader

import (
	"math"
	"strings"
	"sync"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
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
}

type mentorLivePos struct {
	Pos           mentorPosition // the exit-driver state (mode, entry, stop, R, …)
	Legs          [2]mentorLeg   // [0] = the whole position (single leg); [1] reserved for the split redesign
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
func (at *AutoTrader) registerMentorLivePos(r store.ArmedOrderDB, u ntwire.OrderUpdatePayload) {
	if r.SignalID == "" {
		return
	}
	side := strings.ToLower(strings.TrimSpace(r.Side))
	if side == "" {
		return
	}
	n := u.Quantity
	if n < 1 && r.FillQuantity > 0 {
		n = r.FillQuantity
	}
	if n < 1 {
		n = 1
	}
	entry := u.FillPrice
	// REVIEW-SPLIT-2 P2-4: when the armed row carries a split (Leg1Qty set),
	// register BOTH legs — Legs[0] = leg 1 (its own TP), Legs[1] = the runner —
	// so a leg-1 exit does not flatten the runner's exit-drive.
	leg1Qty := 0
	if r.Leg1Qty != nil {
		leg1Qty = *r.Leg1Qty
	}
	if leg1Qty >= n {
		leg1Qty = 0
	}
	leg1TP := r.Leg1TP
	if leg1Qty > 0 && leg1TP == 0 {
		leg1TP = r.TargetPx
	}
	runnerQty := n - leg1Qty
	pos := mentorPosition{
		Symbol:    at.futuresSymbol(),
		Side:      side,
		Origin:    r.Condition,
		Entry:     entry,
		Stop:      r.StopPx,
		Target:    r.TargetPx,
		R:         math.Abs(entry - r.StopPx),
		Contracts: n,
		Leg1:      leg1Qty,
		Leg2:      runnerQty,
		Mode:      at.mentorExitMode(side),
		Leg1TP:    leg1TP,
	}
	if leg1Qty == 0 {
		pos.Leg1 = n // no split: the whole position is one leg
		pos.Leg1TP = r.TargetPx
	}
	lp := &mentorLivePos{Pos: pos}
	if v, ok := mentorRunnerTargets.Load(r.Scenario); ok {
		lp.RunnerTarget, _ = v.(float64)
	}
	if leg1Qty > 0 {
		lp.Legs[0] = mentorLeg{SignalID: r.SignalID, Qty: leg1Qty, TP: leg1TP, Stop: r.StopPx, Final: false}
		lp.Legs[1] = mentorLeg{SignalID: r.SignalID, Qty: runnerQty, TP: r.TargetPx, Stop: r.StopPx, Final: true}
	} else {
		lp.Legs[0] = mentorLeg{SignalID: r.SignalID, Qty: n, TP: r.TargetPx, Stop: r.StopPx, Final: true}
	}
	at.mentorRegisterLivePos(r.SignalID, lp)
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

// mentorUnregisterLivePos removes a position when its side goes flat.
func (at *AutoTrader) mentorUnregisterLivePos(key string) {
	if at == nil || key == "" {
		return
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	delete(at.mentorLivePos, key)
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
