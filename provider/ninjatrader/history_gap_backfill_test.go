package ninjatrader

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// ── GAP-BACKFILL (2026-10-14) — the wire's gap re-request ─────────────────
//
// The same bars_subscribe resend as the scale-break re-request, on a budget of
// its own. Three pins: refused while the feed is down (no frame), its own
// budget (the scale-break one-per-boot guard is untouched both ways), and its
// spacing + per-session-day cap.

func gapPipe(t *testing.T, feedUp bool) (*TCPServer, chan BarsSubscribePayload) {
	t.Helper()
	s := NewTCPServer(nil)
	srv, cli := net.Pipe()
	t.Cleanup(func() { srv.Close(); cli.Close() })
	s.conn = srv
	s.barsSubscribe.Timeframes = []string{"1m", "5m", "1h"}
	s.barsSubscribe.BarsBack = 2000
	s.markFeedConnectedForTest(feedUp)
	got := make(chan BarsSubscribePayload, 64)
	go func() {
		for {
			p, ok := readSubscribeFrame(t, cli, 3*time.Second)
			if !ok {
				return
			}
			got <- p
		}
	}()
	return s, got
}

func drainFrames(ch chan BarsSubscribePayload, wait time.Duration) int {
	n := 0
	deadline := time.After(wait)
	for {
		select {
		case <-ch:
			n++
		case <-deadline:
			return n
		}
	}
}

func TestGapBackfillIsRefusedWhileTheFeedIsDown(t *testing.T) {
	s, got := gapPipe(t, false)
	err := s.RequestGapBackfillAt("MNQ", time.Date(2026, 10, 14, 12, 50, 0, 0, time.UTC))
	if err == nil || !errors.Is(err, ErrGapBackfillFeedDown) {
		t.Fatalf("feed down: want ErrGapBackfillFeedDown, got %v", err)
	}
	if n := drainFrames(got, 200*time.Millisecond); n != 0 {
		t.Fatalf("feed down: %d frame(s) sent — a BarsRequest in an outage returns bars=0", n)
	}
	if s.GapBackfillsSent("MNQ") != 0 {
		t.Fatalf("a refused request must not be counted as sent")
	}
}

func TestGapBackfillHasItsOwnBudgetAndLeavesTheScaleBreakOneAlone(t *testing.T) {
	s, got := gapPipe(t, true)
	t0 := time.Date(2026, 10, 14, 12, 50, 0, 0, time.UTC) // 07:50 CT, a Wednesday
	for i := 0; i < 3; i++ {
		if err := s.RequestGapBackfillAt("MNQ", t0.Add(time.Duration(i)*61*time.Second)); err != nil {
			t.Fatalf("gap re-request %d: %v", i+1, err)
		}
	}
	if s.HistoryReplaysSent("MNQ") != 0 {
		t.Fatalf("gap re-requests spent %d of the scale-break budget", s.HistoryReplaysSent("MNQ"))
	}
	if err := s.RequestHistoryReplayAt("MNQ", t0.Add(4*time.Minute)); err != nil {
		t.Fatalf("the scale-break re-request must keep its one-per-boot budget: %v", err)
	}
	if err := s.RequestHistoryReplayAt("MNQ", t0.Add(5*time.Minute)); !errors.Is(err, ErrHistoryReplaySpent) {
		t.Fatalf("the scale-break guard must still refuse its second: %v", err)
	}
	// and spending the scale-break budget does not touch the gap budget
	if err := s.RequestGapBackfillAt("MNQ", t0.Add(6*time.Minute)); err != nil {
		t.Fatalf("the gap budget is its own: %v", err)
	}
	if s.GapBackfillsSent("MNQ") != 4 || s.HistoryReplaysSent("MNQ") != 1 {
		t.Fatalf("counts: gap %d (want 4) scale-break %d (want 1)", s.GapBackfillsSent("MNQ"), s.HistoryReplaysSent("MNQ"))
	}
	if n := drainFrames(got, 300*time.Millisecond); n != 5 {
		t.Fatalf("frames at the AddOn = %d, want 5 (4 gap + 1 scale-break)", n)
	}
}

func TestGapBackfillSpacingAndSessionDayCap(t *testing.T) {
	s, got := gapPipe(t, true)
	go drainFrames(got, 10*time.Second)
	t0 := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) // 07:00 CT
	if err := s.RequestGapBackfillAt("MNQ", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestGapBackfillAt("MNQ", t0.Add(30*time.Second)); !errors.Is(err, ErrGapBackfillTooSoon) {
		t.Fatalf("inside the spacing: want ErrGapBackfillTooSoon, got %v", err)
	}
	// ES is its own symbol
	if err := s.RequestGapBackfillAt("ES", t0.Add(30*time.Second)); err != nil {
		t.Fatalf("another symbol is not spaced by MNQ: %v", err)
	}
	at := t0
	for i := 1; i < gapBackfillMaxPerDay; i++ {
		at = at.Add(61 * time.Second)
		if err := s.RequestGapBackfillAt("MNQ", at); err != nil {
			t.Fatalf("send %d of %d: %v", i+1, gapBackfillMaxPerDay, err)
		}
	}
	if err := s.RequestGapBackfillAt("MNQ", at.Add(61*time.Second)); !errors.Is(err, ErrGapBackfillSpent) {
		t.Fatalf("past the per-day cap: want ErrGapBackfillSpent, got %v", err)
	}
	// the next CME session-day (after the 17:00 CT roll) has a fresh budget
	next := time.Date(2026, 10, 14, 22, 1, 0, 0, time.UTC) // 17:01 CT
	if err := s.RequestGapBackfillAt("MNQ", next); err != nil {
		t.Fatalf("a new session-day resets the budget: %v", err)
	}
}

// TestGapBackfillArmsTheOverlapWindowBeforeTheSend (#23 review P3-2): the
// window the replay is judged in must be open BEFORE the frame leaves —
// armed after the write, a reply processed first would miss its verdict.
// net.Pipe makes it deterministic: the reader holds back the frame's last
// byte, so WriteFrame cannot have returned, and looks at the window then.
func TestGapBackfillArmsTheOverlapWindowBeforeTheSend(t *testing.T) {
	s := NewTCPServer(nil)
	srv, cli := net.Pipe()
	t.Cleanup(func() { srv.Close(); cli.Close() })
	s.conn = srv
	s.barsSubscribe.Timeframes = []string{"1m"}
	s.barsSubscribe.BarsBack = 2000
	s.markFeedConnectedForTest(true)
	armedMidSend := make(chan bool, 1)
	go func() {
		var hdr [4]byte
		if _, err := io.ReadFull(cli, hdr[:]); err != nil {
			armedMidSend <- false
			return
		}
		n := binary.BigEndian.Uint32(hdr[:])
		body := make([]byte, n)
		if _, err := io.ReadFull(cli, body[:n-1]); err != nil {
			armedMidSend <- false
			return
		}
		s.barCache.mu.RLock()
		until, ok := s.barCache.overlapJudgeUntil["MNQ"]
		s.barCache.mu.RUnlock()
		armedMidSend <- ok && time.Now().Before(until)
		_, _ = io.ReadFull(cli, body[n-1:])
	}()
	if err := s.RequestGapBackfillAt("MNQ", time.Date(2026, 10, 14, 12, 50, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	select {
	case armed := <-armedMidSend:
		if !armed {
			t.Fatal("the overlap window must be armed BEFORE the bars_subscribe frame is written")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the fake AddOn never read the frame")
	}
}
