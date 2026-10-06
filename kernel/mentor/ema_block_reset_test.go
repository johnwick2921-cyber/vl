package mentor

import (
	"testing"

	"vl/market"
)

// TestEmaLossBlockResetsAtSessionDay — B8 (L10): a loss at the EMA34 line must
// NOT switch the location off until a restart / strategy save. The E2 block
// resets at the 17:00 CT session-day boundary, exactly like the G2 loss boxes
// (Limits.Apply clears Places at the trading-day rollover). With the default
// LossDeparturePts (0) the numeric departure is OFF, so the day rollover is the
// ONLY reset. MUTANT: drop the day-key reset → this test stays RED.
func TestEmaLossBlockResetsAtSessionDay(t *testing.T) {
	cfg := DefaultConfig() // LossDeparturePts 0 — the block must lift WITHOUT the knob
	e := New(cfg)

	day1 := auditMs(2026, 9, 15, 16, 0, 0)
	e.State.EmaPendingSide = SideLong
	e.State.EmaPendingEntry = 102
	e.State.EmaPendingStop = 98
	e.State.EmaPendingTarget = 108
	e.State.EmaPendingExpiry = day1 + 86400_000
	e.State.EmaPendingFilled = false

	// Fill at 16:00 CT.
	emaLossTick(e, 100, market.Kline{High: 102.5, Low: 101, CloseTime: day1 - 60_000}, day1)
	if !e.State.EmaPendingFilled {
		t.Fatal("touching the entry must fill the pending setup")
	}
	// Stop-out at 16:01 CT → one loss at the line → blocked.
	stop := auditMs(2026, 9, 15, 16, 1, 0)
	emaLossTick(e, 100, market.Kline{High: 101, Low: 97.5, CloseTime: stop - 60_000}, stop)
	if !e.State.EmaBlocked {
		t.Fatal("(a) filled then stopped must block the EMA line")
	}
	// Same session day, a far-away candle: no numeric departure (knob OFF),
	// no day rollover → still blocked.
	later := auditMs(2026, 9, 15, 16, 2, 0)
	emaLossTick(e, 100, market.Kline{High: 130, Low: 129, Close: 129.5, CloseTime: later - 60_000}, later)
	if !e.State.EmaBlocked {
		t.Fatal("no departure and no day rollover yet — the block must hold")
	}
	// 17:00 CT rolls the session day over (the G2 boundary): the block lifts.
	roll := auditMs(2026, 9, 15, 17, 1, 0)
	emaLossTick(e, 100, market.Kline{High: 130, Low: 129, Close: 129.5, CloseTime: roll - 60_000}, roll)
	if e.State.EmaBlocked {
		t.Fatal("the 17:00 CT session-day rollover must lift the EMA block")
	}
}
