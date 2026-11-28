package ninjatrader

import (
	"strings"
	"time"
)

// StopMoveRecord is the last stop move this process sent for a side.
type StopMoveRecord struct {
	Px   float64
	AtMs int64
}

// noteStopMove records a successfully sent stop move (m425b E1). Takes t.mu.
func (t *TCPTrader) noteStopMove(side string, px float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.noteStopMoveLocked(side, px)
}

// noteStopMoveLocked is noteStopMove for a caller already holding t.mu.
func (t *TCPTrader) noteStopMoveLocked(side string, px float64) {
	if px <= 0 {
		return
	}
	if t.lastStopMove == nil {
		t.lastStopMove = map[string]StopMoveRecord{}
	}
	t.lastStopMove[strings.ToUpper(strings.TrimSpace(side))] = StopMoveRecord{Px: px, AtMs: time.Now().UnixMilli()}
}

// LastStopMove returns the last stop move this process sent for side (any
// path, any leg), or ok=false when none was sent.
func (t *TCPTrader) LastStopMove(side string) (StopMoveRecord, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.lastStopMove[strings.ToUpper(strings.TrimSpace(side))]
	return r, ok
}

// CreatedMs is when this TCPTrader was constructed (0 = unknown). A trade
// that opened before it was not watched from the start by this process.
func (t *TCPTrader) CreatedMs() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.createdMs
}
