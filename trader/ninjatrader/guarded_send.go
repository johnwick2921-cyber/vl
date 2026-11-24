package ninjatrader

import (
	"errors"

	ntwire "vl/provider/ninjatrader"
)

// ── GUARDED SENDS (look-twice 2026-11-24) — the adapter half of
// provider/ninjatrader SendClosePositionIf / SendPlaceProtectiveStopIf. The
// protection reconciler decided on evidence; guard re-reads it in memory as the
// last step before the frame is written. Not part of types.Trader.

// CloseAllIf is CloseLong/CloseShort(symbol, 0) behind a guard.
func (t *TCPTrader) CloseAllIf(side string, guard func() error) error {
	t.mu.Lock()
	entrySig := t.lastEntrySignalID
	t.mu.Unlock()
	_, err := t.sendCloseAtIf(side, 0, 0, guard)
	if errors.Is(err, ntwire.ErrSendWithheld) {
		// no close went out: the entry's fill correlation is still the live one
		t.mu.Lock()
		if t.lastEntrySignalID == "" {
			t.lastEntrySignalID = entrySig
		}
		t.mu.Unlock()
	}
	return err
}

func (t *TCPTrader) sendCloseAt(side string, quantity float64, limitPrice float64) (map[string]interface{}, error) {
	return t.sendCloseAtIf(side, quantity, limitPrice, nil)
}

// PlaceProtectiveStop — see PlaceProtectiveStopIf.
func (t *TCPTrader) PlaceProtectiveStop(symbol, positionSide string, quantity int, stopPrice float64, signalID, reason string) error {
	return t.PlaceProtectiveStopIf(symbol, positionSide, quantity, stopPrice, signalID, reason, nil)
}
