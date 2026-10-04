package trader

// ── MENTOR LIVE POSITION REGISTRY (the exit-drive's input) ─────────────────
//
// mentorLeg / mentorLivePos are DS-103's part-1 shape, reproduced verbatim so
// part 2 (the exit-drive loop) compiles and tests independently. When DS-103's
// branch lands these identical definitions and the fill-callback registration
// merge to theirs — the loop CONSUMES this shape, it does not own it. The
// registry is keyed by the leg-1 signal id and guarded by mentorExitMu.

type mentorLeg struct {
	SignalID string  // the leg's own entry signal id (move_stop key)
	Qty      int     // contracts in this leg
	TP       float64 // the leg's own take-profit (leg 1 = +1R / fill-candle close; runner = the trade target)
	Stop     float64 // the leg's CURRENT resting stop (the loop writes back here)
	Final    bool    // the leg has exited (leg 1 at its native TP)
}

type mentorLivePos struct {
	Pos           mentorPosition // the exit-driver state (mode, entry, stop, R, …)
	Legs          [2]mentorLeg   // [0] = leg 1, [1] = the runner
	FillBarOpen   int64          // the fill candle's OpenTime
	FillBarClose  float64        // the fill candle's close (ISB leg-1 TP)
	BarsSinceFill int            // closed 1m candles since the fill
	SpentDay      bool           // §7 spent day → mode D (runner ≤ 2)
	Confluence    bool           // R2 confluence flag → mode C
}

// mentorRegisterLivePos stores a filled position under the leg-1 signal id. A
// nil position or an empty key is ignored (fail-closed: the loop only sees
// what was actually registered).
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
