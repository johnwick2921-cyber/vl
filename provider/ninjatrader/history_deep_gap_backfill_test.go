package ninjatrader

import (
	"errors"
	"testing"
	"time"
)

// ── DEEP GAP BACKFILL (m444 review round 2) — the wire's deep 1m ask ────────

// The deep ask is ONE timeframe (1m) with the enlarged bars_back, clamped to
// DeepGapBackfillMaxBarsBack, on the SAME admission as every gap re-request:
// refused while the feed is down (no frame), spaced, and counted against the
// same per-session-day budget. It never touches the scale-break budget.
// MUTANT (shown RED): send Timeframes = s.barsSubscribe.Timeframes (all 3) ->
// the timeframes assertion fails; drop the clamp -> the 9000 -> 8640 assertion
// fails; skip admitGapBackfill -> the feed-down refusal + spacing assertions fail.
func TestDeepGapBackfillIsOneTimeframeClampedAndOnTheSameAdmission(t *testing.T) {
	s, got := gapPipe(t, false)
	t0 := time.Date(2026, 10, 14, 12, 50, 0, 0, time.UTC) // 07:50 CT
	if err := s.RequestDeepGapBackfillAt("MNQ", 3000, t0); !errors.Is(err, ErrGapBackfillFeedDown) {
		t.Fatalf("feed down: want ErrGapBackfillFeedDown, got %v", err)
	}
	if n := drainFrames(got, 200*time.Millisecond); n != 0 {
		t.Fatalf("feed down: %d frame(s) sent", n)
	}
	s.markFeedConnectedForTest(true)
	if err := s.RequestDeepGapBackfillAt("MNQ", 0, t0); err == nil {
		t.Fatal("bars_back 0 must be refused")
	}
	if err := s.RequestDeepGapBackfillAt("MNQ", 3000, t0); err != nil {
		t.Fatalf("deep ask: %v", err)
	}
	p := <-got
	if p.Symbol != "MNQ" || len(p.Timeframes) != 1 || p.Timeframes[0] != "1m" || p.BarsBack != 3000 {
		t.Fatalf("deep frame = %+v, want MNQ [1m] bars_back 3000 (only the 1m request is rebuilt)", p)
	}
	if err := s.RequestDeepGapBackfillAt("MNQ", 3000, t0.Add(30*time.Second)); !errors.Is(err, ErrGapBackfillTooSoon) {
		t.Fatalf("inside the spacing: want ErrGapBackfillTooSoon, got %v", err)
	}
	if err := s.RequestDeepGapBackfillAt("MNQ", 9000, t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if p := <-got; p.BarsBack != DeepGapBackfillMaxBarsBack {
		t.Fatalf("bars_back %d, want the clamp %d", p.BarsBack, DeepGapBackfillMaxBarsBack)
	}
	// the same budget as the standard gap ask, and not the scale-break one
	if s.GapBackfillsSent("MNQ") != 2 || s.HistoryReplaysSent("MNQ") != 0 {
		t.Fatalf("gap budget %d (want 2), scale-break %d (want 0)", s.GapBackfillsSent("MNQ"), s.HistoryReplaysSent("MNQ"))
	}
}
