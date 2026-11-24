package ninjatrader

import (
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

// Key mode publishes the connection in authenticateKeyHello: its link accounting
// must start there. MUTANT: drop s.hbLink = newHeartbeatLink() there.
func TestLinkProbeConnectKeyMode(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	s := startedServer(t, nil)
	for round := 1; round <= 2; round++ {
		w := dialWire(t, s, HelloPayload{ConnectKey: testConnectKey})
		if _, ok := w.next(FrameHello, 3*time.Second); !ok {
			t.Fatalf("round %d: no hello", round)
		}
		if s.ProtectionEvidenceFor("Sim101", "MNQ").OnLink {
			t.Fatalf("round %d: the previous connection's positions frame counts on this one", round)
		}
		if err := WriteFrame(w.conn, FramePositions, PositionsPayload{Account: "Sim101", Positions: []OpenPosition{}}); err != nil {
			t.Fatal(err)
		}
		probeWait(t, "positions on this link", func() bool { return s.ProtectionEvidenceFor("Sim101", "MNQ").OnLink })
		p, err := s.SendLinkProbe()
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		probeAck(t, w.conn, "heartbeat", 0)
		probeWait(t, "the ack", p.Acked)
		_ = w.conn.Close()
		probeWait(t, "the disconnect", func() bool { return !s.IsConnected() })
	}
}

// deadWrite is a connection whose writes fail.
type deadWrite struct{ net.Conn }

func (d deadWrite) Write([]byte) (int, error) { return 0, fmt.Errorf("i/o timeout") }

// A link check whose write fails closes THAT connection (the stream may hold half
// a frame) and never a newer one. MUTANTS: drop closeConnIf; close unconditionally.
func TestLinkProbeWriteErrorClosesItsConnection(t *testing.T) {
	s := NewTCPServer(nil)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	s.connMu.Lock()
	s.conn, s.hbLink = deadWrite{a}, newHeartbeatLink()
	s.connMu.Unlock()
	s.farSideBuild.Store("b")
	if _, err := s.SendLinkProbe(); err == nil {
		t.Fatal("a failed write must be an error")
	}
	if s.IsConnected() || s.FarSideBuildID() != "" {
		t.Fatalf("the broken connection must be closed: connected=%v build=%q", s.IsConnected(), s.FarSideBuildID())
	}
	s.connMu.Lock()
	s.conn, s.hbLink = deadWrite{b}, newHeartbeatLink()
	s.connMu.Unlock()
	s.closeConnIf(deadWrite{a}) // the old one
	if !s.IsConnected() {
		t.Fatal("a newer connection was closed for an older one's error")
	}
}

// A guarded send asks with the writer held: whatever happened while it waited for
// the writer is seen. MUTANT: ask before writeMu.
func TestGuardedSendAsksAfterTheWriterWait(t *testing.T) {
	s, dial := probeServer(t)
	dial()
	for name, send := range map[string]func(func() error) error{
		"close": func(g func() error) error {
			return s.SendClosePositionIf(ClosePositionPayload{Symbol: "MNQ", Side: "short", SignalID: "c", Account: "Sim101"}, g)
		},
		"stop": func(g func() error) error {
			return s.SendPlaceProtectiveStopIf(PlaceProtectiveStopPayload{Symbol: "MNQ", SignalID: "p", PositionSide: "SHORT", Quantity: 1, StopPrice: 1, Account: "Sim101"}, g)
		},
	} {
		asked := make(chan struct{})
		done := make(chan error, 1)
		s.writeMu.Lock()
		go func() {
			done <- send(func() error { close(asked); return fmt.Errorf("%w: flat", ErrSendWithheld) })
		}()
		select {
		case <-asked:
			t.Fatalf("%s: the guard was asked while another writer held the socket", name)
		case <-time.After(80 * time.Millisecond):
		}
		s.writeMu.Unlock()
		if err := <-done; !errors.Is(err, ErrSendWithheld) {
			t.Fatalf("%s: err = %v", name, err)
		}
		if ev := s.ProtectionEvidenceFor("Sim101", "MNQ"); !ev.CloseSent.IsZero() || !ev.StopSent.IsZero() || len(s.OwnCloses("Sim101", "MNQ")) != 0 {
			t.Fatalf("%s: a withheld send must leave no send record: %+v", name, ev)
		}
	}
}

// EntryOpened moves with a real entry fill and not with NT8's "Close".
func TestProtectionEvidenceEntryOpened(t *testing.T) {
	s := NewTCPServer(nil)
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	s.SetReceiptClockForTest(func() time.Time { return now })
	s.NoteEntryExecution("MNQ", "Sim101", "sig", 1)
	opened := now
	now = now.Add(time.Second)
	s.NoteEntryExecution("MNQ", "Sim101", "Close", 1)
	if ev := s.ProtectionEvidenceFor("Sim101", "MNQ"); !ev.EntryOpened.Equal(opened) || !ev.Entry.Equal(now) {
		t.Fatalf("EntryOpened %s Entry %s", ev.EntryOpened, ev.Entry)
	}
}
