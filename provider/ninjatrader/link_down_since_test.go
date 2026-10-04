package ninjatrader

import (
	"testing"
	"time"
)

// UPDATER-NT8-CLOSED never-connected seed. A bot that booted with NT8 closed
// has no connection record, hence no disconnect stamp; "no AddOn connection
// since the listener came up" is the measured link-down. A measured disconnect
// is never overridden by the seed; an unmeasurable shape stays unmeasured.

// The 2026-10-04 07:27 shape: listener up, nothing ever accepted.
func TestLinkDownSinceNeverConnectedIsWhenTheListenerCameUp(t *testing.T) {
	before := time.Now()
	s := startedServer(t, nil)
	after := time.Now()
	rec, connected := s.ConnectionRecord()
	if connected || rec.AcceptSeq != 0 {
		t.Fatalf("fixture: want never connected, got connected=%v accept_seq=%d", connected, rec.AcceptSeq)
	}
	got, ok := rec.LinkDownSince(connected)
	if !ok {
		t.Fatal("a listener that never saw a connection must report a measured link-down start")
	}
	// the stamp is monotonic-ms resolution: allow a millisecond either side
	if got.Before(before.Add(-time.Millisecond)) || got.After(after.Add(time.Millisecond)) {
		t.Fatalf("link-down start %v is not when the listener came up (between %v and %v)", got, before, after)
	}
	// moving the stamp moves the answer (the seam the trader-level pins use)
	ago := time.Now().Add(-10 * time.Minute)
	s.SetListeningSinceForTest(ago)
	rec, connected = s.ConnectionRecord()
	got, ok = rec.LinkDownSince(connected)
	if !ok || got.Sub(ago) > time.Millisecond || ago.Sub(got) > time.Millisecond {
		t.Fatalf("SetListeningSinceForTest(%v) → %v ok=%v", ago, got, ok)
	}
}

// A server that never started listening has nothing to measure: fail closed.
func TestLinkDownSinceUnstartedServerIsUnmeasured(t *testing.T) {
	s := NewTCPServer(nil)
	rec, connected := s.ConnectionRecord()
	if _, ok := rec.LinkDownSince(connected); ok {
		t.Fatal("a server that never started listening must not report a link-down start")
	}
}

// Connected-then-lost keeps today's rule: the disconnect stamp, NOT the
// (much older) listener stamp — and a live link is never "down".
func TestLinkDownSinceConnectedThenLostUsesTheDisconnectStamp(t *testing.T) {
	s := startedServer(t, nil)
	s.SetListeningSinceForTest(time.Now().Add(-10 * time.Minute))
	w := dialWire(t, s, HelloPayload{})
	rec, connected := s.ConnectionRecord()
	if !connected {
		t.Fatal("fixture: the client must be connected")
	}
	if _, ok := rec.LinkDownSince(connected); ok {
		t.Fatal("a connected link must never report a link-down start")
	}
	closedAt := time.Now()
	_ = w.conn.Close()
	var got time.Time
	var ok bool
	for i := 0; i < 400; i++ {
		rec, connected = s.ConnectionRecord()
		if !connected {
			if got, ok = rec.LinkDownSince(connected); ok {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !ok {
		t.Fatalf("the closed connection reports no link-down start: %+v", rec)
	}
	if d := got.Sub(closedAt); d < -time.Millisecond || d > 2*time.Second {
		t.Fatalf("link-down start %v is not the disconnect (closed at %v); the 10-minute-old listener stamp must not win", got, closedAt)
	}
}

// A record that WAS accepted but is not connected and carries no stamp is not a
// shape closeConn writes: it stays unmeasured (fail closed).
func TestLinkDownSinceAcceptedWithoutStampStaysUnmeasured(t *testing.T) {
	rec := ConnectionRecord{AcceptSeq: 3, Listening: true, ListenMonoMs: 1}
	if _, ok := rec.LinkDownSince(false); ok {
		t.Fatal("an accepted record without a disconnect stamp must not fall back to the listener stamp")
	}
}
