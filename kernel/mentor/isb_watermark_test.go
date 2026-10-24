package mentor

import (
	"testing"
)

// TestISBWatermarkNeverMovesBackwards pins m417 F2 on the production path
// (e.Tick): on a frame flip the exec candle can be OLDER than the watermark —
// a 5m candle closes at 09:10, then the schedule flips to the 2m and the next
// exec candle closes at 09:03. `isbEval := execCur.CloseTime !=
// LastISBEvalClose` used to see 09:03 != 09:10 and move the watermark
// BACKWARDS, re-evaluating the ISB pair on a stale candle. `>` keeps the
// watermark monotonic.
// MUTANT: `>` back to `!=` → the watermark regresses to 09:03 → RED.
func TestISBWatermarkNeverMovesBackwards(t *testing.T) {
	cfg := m410Cfg(true) // Exec2mAfter30m ON → the exec frame is 2m after 09:00
	e := New(cfg)
	// A prior watermark LATER than any 2m close this tick will see — the shape
	// of a 5m candle that closed at 09:10 while the next read is a 2m candle.
	watermark := auditMs(2026, 9, 15, 9, 10, 0)
	e.State.LastISBEvalClose = watermark

	// 09:00..09:04 1m tape: at the 09:04 close the last CLOSED 2m bucket is the
	// 09:03 candle — older than the 09:10 watermark (the 5m→2m flip shape).
	bars := m410Tape(9*60, 5)
	now := BarCloseInstant(bars[len(bars)-1])
	e.Tick(bars, now)
	if e.State.LastISBEvalClose != watermark {
		t.Fatalf("watermark regressed from %d to %d on an older exec candle", watermark, e.State.LastISBEvalClose)
	}
}
