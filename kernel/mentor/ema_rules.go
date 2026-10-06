package mentor

import "vl/market"

// emaCross30 counts how many times the close CROSSED the EMA line over the
// last 30 closed 1m candles (sign changes of close - ema between consecutive
// bars). E4: "xien len xien xuong" [D4.2 p1 @ 22:27] — crossing back and forth
// at the line is noise; the mentor never gives a number, so the knob gates.
func emaCross30(bars []market.Kline, ema float64) int {
	if len(bars) < 2 {
		return 0
	}
	n := len(bars)
	start := n - 30
	if start < 1 {
		start = 1
	}
	cross := 0
	prev := bars[start-1].Close - ema
	for _, b := range bars[start:] {
		cur := b.Close - ema
		if (prev < 0 && cur >= 0) || (prev > 0 && cur <= 0) || (prev == 0 && cur != 0) {
			cross++
		}
		prev = cur
	}
	return cross
}

// emaSetupAllowed — the EMA34 setup seam (E2 + E4, CTO 12:27:25Z, moved to
// DS-103 12:33:43Z). Item 16 (CTO 23:49Z): the gate applies to the line that
// TRADES — KindEMA34HTF (the location-gate line), not only KindEMA34 (which is
// never a location). Key levels keep their own rules.
func emaSetupAllowed(e *Evaluator, lvl Level, bars []market.Kline, cfg Config) bool {
	if !isEMA34(lvl) {
		return true
	}
	// E2: one loss at the line blocks the EMA until a departure.
	if e.State.EmaBlocked {
		return false
	}
	// E4: crossing the line too many times in 30m is noise.
	if cfg.EmaMaxCross30m > 0 && emaCross30(bars, lvl.Price) >= cfg.EmaMaxCross30m {
		return false
	}
	return true
}

// emaLossTick — E2 (CTO 12:50:13Z, precise reading): the block is a LOSS at
// the line, not a missed order. The entry must FILL first (price reaches the
// stop-entry); only THEN does a stop touch block the EMA. Three cases on
// closed candles:
//
//	(a) entry touched (filled), then the stop touched  → EMA blocked until a
//	    departure candle (a closed candle whose range does not touch the line);
//	(b) entry never touched before the expiry        → NOT blocked (the order
//	    expired: no position, no loss);
//	(c) entry filled, then the target reached first  → NOT blocked (a win).
func emaLossTick(e *Evaluator, emaPrice float64, cur market.Kline, now int64) {
	if e.State.EmaPendingSide != "" {
		s := &e.State
		touchEntry := s.EmaPendingSide == SideLong && cur.High >= s.EmaPendingEntry ||
			s.EmaPendingSide == SideShort && cur.Low <= s.EmaPendingEntry
		touchStop := s.EmaPendingSide == SideLong && cur.Low <= s.EmaPendingStop ||
			s.EmaPendingSide == SideShort && cur.High >= s.EmaPendingStop
		touchTarget := s.EmaPendingSide == SideLong && cur.High >= s.EmaPendingTarget ||
			s.EmaPendingSide == SideShort && cur.Low <= s.EmaPendingTarget
		switch {
		case !s.EmaPendingFilled && touchEntry:
			s.EmaPendingFilled = true // filled: the position is live
		case !s.EmaPendingFilled && now > s.EmaPendingExpiry:
			s.EmaPendingSide = "" // (b) expired unfilled — no loss, no block
		case s.EmaPendingFilled && touchStop:
			s.EmaBlocked = true // (a) one loss at the line
			s.EmaLossPrice = s.EmaPendingStop
			s.EmaLossBarTime = cur.CloseTime
			s.EmaPendingSide = ""
			s.EmaPendingFilled = false
		case s.EmaPendingFilled && touchTarget:
			s.EmaPendingSide = "" // (c) target first — not a loss
			s.EmaPendingFilled = false
		}
	}
	// Departure (CTO 13:24:53Z, ONE rule with G2): a CLOSED candle AFTER the
	// loss candle whose |close - loss price| reaches LossDeparturePts lifts
	// the block. The loss candle itself is never its own departure (R-a).
	if e.State.EmaBlocked &&
		cur.CloseTime > e.State.EmaLossBarTime &&
		e.Cfg.LossDeparturePts > 0 &&
		abs(cur.Close-e.State.EmaLossPrice) >= e.Cfg.LossDeparturePts {
		e.State.EmaBlocked = false
	}
}

func isEMA34(lvl Level) bool {
	return lvl.Kind == KindEMA34 || lvl.Kind == KindEMA34HTF
}
