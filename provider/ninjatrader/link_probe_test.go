package ninjatrader

import (
	"context"
	"net"
	"testing"
	"time"
)

// probeServer is a started server with one dialed AddOn whose heartbeats are
// answered only when the test says so.
func probeServer(t *testing.T) (*TCPServer, func() net.Conn) {
	t.Helper()
	s := NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })
	return s, func() net.Conn {
		c, err := net.Dial("tcp", s.ListenAddrForTest().String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		go func() { // drain what the server writes
			for {
				if _, err := ReadFrame(c); err != nil {
					return
				}
			}
		}()
		probeWait(t, "the connection to publish", s.IsConnected)
		return c
	}
}

func probeWait(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !ok(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func probeAck(t *testing.T, c net.Conn, what string, seq uint64) {
	t.Helper()
	if err := WriteFrame(c, FrameAck, AckPayload{Acks: what, Seq: seq}); err != nil {
		t.Fatal(err)
	}
}

// probeSeen returns once the read loop has processed everything written on c.
func probeSeen(t *testing.T, s *TCPServer, c net.Conn) {
	t.Helper()
	n := s.framesTotal.Load()
	if err := WriteFrame(c, FrameHeartbeat, HeartbeatPayload{BuildID: "b"}); err != nil {
		t.Fatal(err)
	}
	probeWait(t, "the read loop", func() bool { return s.framesTotal.Load() > n })
	time.Sleep(20 * time.Millisecond)
}

func TestLinkProbeIsAckedOnlyByItsOwnHeartbeat(t *testing.T) {
	s, dial := probeServer(t)
	if _, err := s.SendLinkProbe(); err == nil {
		t.Fatal("no connection: the probe must refuse")
	}
	c := dial()
	probeAck(t, c, "heartbeat", 0) // an ack of nothing we sent
	probeAck(t, c, "some-signal", 0)
	probeSeen(t, s, c)
	p1, err := s.SendLinkProbe()
	if err != nil {
		t.Fatal(err)
	}
	p2, err := s.SendLinkProbe()
	if err != nil {
		t.Fatal(err)
	}
	probeSeen(t, s, c)
	if p1.Acked() || p2.Acked() {
		t.Fatal("an ack received BEFORE the probe, or of a signal, must not answer it")
	}
	probeAck(t, c, "heartbeat", 7) // an ORDER ack (seq>0) is not a heartbeat's
	probeSeen(t, s, c)
	if p1.Acked() {
		t.Fatal("an order ack answered a heartbeat probe")
	}
	probeAck(t, c, "heartbeat", 0)
	probeWait(t, "probe 1", p1.Acked)
	if p2.Acked() {
		t.Fatal("the first ack answered the second probe")
	}
	probeAck(t, c, "heartbeat", 0)
	probeWait(t, "probe 2", p2.Acked)
}

func TestLinkProbeWaitEndsOnAckContextOrTimeout(t *testing.T) {
	s, dial := probeServer(t)
	c := dial()
	p, err := s.SendLinkProbe()
	if err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	p.Wait(context.Background(), 60*time.Millisecond)
	if d := time.Since(began); d < 55*time.Millisecond || d > 2*time.Second || p.Acked() {
		t.Fatalf("timeout: waited %s acked=%v", d, p.Acked())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	began = time.Now()
	p.Wait(ctx, 10*time.Second)
	if d := time.Since(began); d > time.Second {
		t.Fatalf("cancelled: waited %s", d)
	}
	time.AfterFunc(30*time.Millisecond, func() { _ = WriteFrame(c, FrameAck, AckPayload{Acks: "heartbeat"}) })
	began = time.Now()
	p.Wait(context.Background(), 10*time.Second)
	if d := time.Since(began); d > 2*time.Second || !p.Acked() {
		t.Fatalf("ack: waited %s acked=%v", d, p.Acked())
	}
}

// A probe of one connection is never answered on the next, and the next one's
// own probe needs its own ack.
func TestLinkProbeDiesWithItsConnection(t *testing.T) {
	s, dial := probeServer(t)
	c1 := dial()
	p1, err := s.SendLinkProbe()
	if err != nil {
		t.Fatal(err)
	}
	seq1 := s.ProtectionEvidenceFor("Sim101", "MNQ").AcceptSeq
	_ = c1.Close()
	probeWait(t, "the disconnect", func() bool { return !s.IsConnected() })
	c2 := dial()
	p2, err := s.SendLinkProbe()
	if err != nil {
		t.Fatal(err)
	}
	if seq2 := s.ProtectionEvidenceFor("Sim101", "MNQ").AcceptSeq; seq2 == seq1 {
		t.Fatalf("a new connection must change the accept sequence (%d)", seq2)
	}
	probeAck(t, c2, "heartbeat", 0)
	probeWait(t, "probe 2", p2.Acked)
	if p1.Acked() {
		t.Fatal("a probe of the old connection was answered on the new one")
	}
}

// The periodic heartbeat shares the count, so its outstanding ack cannot answer
// a probe sent after it.
func TestLinkProbeCountsThePeriodicHeartbeatToo(t *testing.T) {
	old := TCPHeartbeatInterval
	TCPHeartbeatInterval = 40 * time.Millisecond
	t.Cleanup(func() { TCPHeartbeatInterval = old })
	s, dial := probeServer(t)
	c := dial()
	probeWait(t, "a periodic heartbeat", func() bool {
		l := s.linkFor(s.connectedAuthed())
		if l == nil {
			return false
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.sent >= 1
	})
	p, err := s.SendLinkProbe()
	if err != nil {
		t.Fatal(err)
	}
	probeAck(t, c, "heartbeat", 0)
	probeSeen(t, s, c)
	if p.Acked() {
		t.Fatal("the ack of the periodic heartbeat answered the probe sent after it")
	}
}

// Evidence: the newest own send, positions-on-this-link, and the exact match.
func TestProtectionEvidence(t *testing.T) {
	s, dial := probeServer(t)
	c := dial()
	base := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	now := base
	s.SetReceiptClockForTest(func() time.Time { return now })
	ev := s.ProtectionEvidenceFor("Sim101", "MNQ")
	if ev.Known || ev.OnLink || !ev.LastExecution().IsZero() || ev.Holds("MNQ", "SHORT", 1, 100) {
		t.Fatalf("nothing received: %+v", ev)
	}
	if e := s.ProtectionEvidenceFor("", "MNQ"); e.Known || e.OnLink || e.AcceptSeq != 0 {
		t.Fatalf("no account = no evidence: %+v", e)
	}
	if err := WriteFrame(c, FramePositions, PositionsPayload{Account: "Sim101", Positions: []OpenPosition{{Symbol: "MNQ", Side: "short", Quantity: 1, AvgPrice: 100.25}}}); err != nil {
		t.Fatal(err)
	}
	probeWait(t, "the positions frame", func() bool { return s.ProtectionEvidenceFor("Sim101", "MNQ").OnLink })
	ev = s.ProtectionEvidenceFor("Sim101", "MNQ")
	if !ev.Known || !ev.Holds("MNQ", "SHORT", 1, 100.25) || ev.Holds("MNQ", "LONG", 1, 100.25) || ev.Holds("MNQ", "SHORT", 2, 100.25) || ev.Holds("MNQ", "SHORT", 1, 100.50) {
		t.Fatalf("Holds must match side, quantity and price exactly: %+v", ev)
	}
	if s.ProtectionEvidenceFor("Sim102", "MNQ").OnLink {
		t.Fatal("another account's frame is not this account's")
	}
	now = base.Add(time.Second)
	if err := s.SendClosePosition(ClosePositionPayload{Symbol: "MNQ", Side: "short", SignalID: "c", Account: "Sim101"}); err != nil {
		t.Fatal(err)
	}
	if ev = s.ProtectionEvidenceFor("Sim101", "MNQ"); !ev.CloseSent.Equal(now) || !ev.LastExecution().Equal(now) || !ev.StopSent.IsZero() {
		t.Fatalf("own close: %+v", ev)
	}
	now = base.Add(2 * time.Second)
	if err := s.SendPlaceProtectiveStop(PlaceProtectiveStopPayload{Symbol: "MNQ", SignalID: "p", PositionSide: "SHORT", Quantity: 1, StopPrice: 101, Account: "Sim101"}); err != nil {
		t.Fatal(err)
	}
	if ev = s.ProtectionEvidenceFor("Sim101", "MNQ"); !ev.StopSent.Equal(now) || !ev.LastExecution().Equal(now) {
		t.Fatalf("own stop: %+v", ev)
	}
	_ = c.Close()
	probeWait(t, "the disconnect", func() bool { return !s.IsConnected() })
	dial()
	if ev = s.ProtectionEvidenceFor("Sim101", "MNQ"); ev.OnLink || !ev.Known || ev.StopSent.IsZero() {
		t.Fatalf("a new link: the old frame stays known but is not ON this link; our own sends survive: %+v", ev)
	}
}
