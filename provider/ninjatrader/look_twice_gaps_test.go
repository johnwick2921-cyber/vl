package ninjatrader

import (
	"net"
	"testing"
	"time"
)

// ── LOOK TWICE, the mutants no test saw (full test of 9acafc81f) ─────────────
// One case per mutant, named by its id in fulltest-71/C1-table.log.

// A14: only a name whose order is OVER may start counting again. A working report
// of a part-filled order is not another order, and its replayed fill is no news.
func TestExitFenceAPartFilledOrderIsNotANewOne(t *testing.T) {
	r := newExitFenceRig(t)
	r.ou(100, "sig-sl", "partfilled", 1)
	r.ou(110, "sig-sl", "working", 0)
	r.ou(120, "sig-sl", "partfilled", 1)
	r.want(100, "the same part fill again")
}

// A22: a position_close ends its order even when no order_update said so; the next
// order of that name then fills less, and that is a new fill.
func TestExitFenceACloseEndsItsOrder(t *testing.T) {
	r := newExitFenceRig(t)
	r.pc(100, "sig", "sl", 0, 2, "t1")
	r.ou(200, "sig-sl", "working", 0)
	r.ou(300, "sig-sl", "filled", 1)
	r.want(300, "the replacement's smaller fill")
}

// D02: an ack read on a connection that is no longer the published one (its read
// loop drains after a reconnect) answers nothing on the new one.
func TestLinkProbeAnAckOfAnOldConnectionCountsNothing(t *testing.T) {
	s := NewTCPServer(nil)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	link := newHeartbeatLink()
	link.sent = 1
	s.connMu.Lock()
	s.conn, s.hbLink = b, link
	s.connMu.Unlock()
	p := &LinkProbe{link: link, seq: 1}
	s.noteHeartbeatAck(a)
	s.notePositionsOnLink(a, "Sim101")
	if p.Acked() || s.ProtectionEvidenceFor("Sim101", "MNQ").OnLink {
		t.Fatal("a frame of the old connection spoke for the new one")
	}
	s.noteHeartbeatAck(b)
	s.notePositionsOnLink(b, "Sim101")
	if !p.Acked() || !s.ProtectionEvidenceFor("Sim101", "MNQ").OnLink {
		t.Fatal("control: the published connection")
	}
}

// D09: the ack of a SIGNAL carries seq 0 from an old AddOn. With a probe out, it
// must not answer it.
func TestLinkProbeASignalAckDoesNotAnswerIt(t *testing.T) {
	s, dial := probeServer(t)
	c := dial()
	p, err := s.SendLinkProbe()
	if err != nil {
		t.Fatal(err)
	}
	probeAck(t, c, "some-signal", 0)
	probeSeen(t, s, c)
	if p.Acked() {
		t.Fatal("a signal's ack answered the probe")
	}
	probeAck(t, c, "heartbeat", 0)
	probeWait(t, "its own ack", p.Acked)
}

// D26: the same side, size and price of ANOTHER symbol is not this position.
func TestProtectionEvidenceHoldsNeedsTheSymbol(t *testing.T) {
	ev := ProtectionEvidence{Known: true, Snapshot: []OpenPosition{{Symbol: "MES", Side: "short", Quantity: 1, AvgPrice: 100}}}
	if ev.Holds("MNQ", "SHORT", 1, 100) || !ev.Holds("mes", "SHORT", 1, 100) {
		t.Fatal("Holds must match the symbol")
	}
}

// D19 / D20 and their siblings: each of the four instants is an execution.
func TestProtectionEvidenceLastExecution(t *testing.T) {
	at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	old := at.Add(-time.Minute)
	for name, ev := range map[string]ProtectionEvidence{
		"entry":     {ExecutionReceipts: ExecutionReceipts{Entry: at, Exit: old}, CloseSent: old, StopSent: old},
		"exit":      {ExecutionReceipts: ExecutionReceipts{Entry: old, Exit: at}, CloseSent: old, StopSent: old},
		"own close": {ExecutionReceipts: ExecutionReceipts{Entry: old, Exit: old}, CloseSent: at, StopSent: old},
		"own stop":  {ExecutionReceipts: ExecutionReceipts{Entry: old, Exit: old}, CloseSent: old, StopSent: at},
	} {
		if got := ev.LastExecution(); !got.Equal(at) {
			t.Errorf("%s is the newest: LastExecution = %s", name, got)
		}
	}
}

// pipeLink publishes one end of a pipe as the connection and reports the first
// frame written on it.
func pipeLink(t *testing.T) (c net.Conn, frames chan Envelope) {
	t.Helper()
	c, far := net.Pipe()
	t.Cleanup(func() { _ = c.Close(); _ = far.Close() })
	frames = make(chan Envelope, 4)
	go func() {
		for {
			env, err := ReadFrame(far)
			if err != nil {
				return
			}
			frames <- env
		}
	}()
	return c, frames
}

// D35 / D36: the connection is taken BEFORE the guard. One that appears after it
// was judged by nobody, so the frame must not go out there.
func TestGuardedSendWritesOnTheConnectionItTookBeforeTheGuard(t *testing.T) {
	for name, send := range map[string]func(*TCPServer, func() error) error{
		"close": func(s *TCPServer, g func() error) error {
			return s.SendClosePositionIf(ClosePositionPayload{Symbol: "MNQ", Side: "short", SignalID: "c", Account: "Sim101"}, g)
		},
		"stop": func(s *TCPServer, g func() error) error {
			return s.SendPlaceProtectiveStopIf(PlaceProtectiveStopPayload{Symbol: "MNQ", SignalID: "p", PositionSide: "SHORT", Quantity: 1, StopPrice: 1, Account: "Sim101"}, g)
		},
	} {
		s := NewTCPServer(nil)
		a, onA := pipeLink(t)
		b, onB := pipeLink(t)
		publish := func(c net.Conn) {
			s.connMu.Lock()
			s.conn, s.hbLink = c, newHeartbeatLink()
			s.connMu.Unlock()
		}
		publish(a)
		if err := send(s, func() error { publish(b); return nil }); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		select {
		case <-onA:
		case <-onB:
			t.Fatalf("%s: the frame went out on a connection the guard never saw", name)
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: no frame", name)
		}
		// none before the guard, one after: nothing to write on
		publish(nil)
		if err := send(s, func() error { publish(b); return nil }); err == nil {
			t.Fatalf("%s: there was no connection when the send began", name)
		}
	}
}

// D38: a close whose write failed never reached NT8 whole, so it explains no fill.
func TestOwnCloseLogForgetsAFailedWrite(t *testing.T) {
	s := NewTCPServer(nil)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	s.connMu.Lock()
	s.conn, s.hbLink = deadWrite{a}, newHeartbeatLink()
	s.connMu.Unlock()
	if err := s.SendClosePosition(ClosePositionPayload{Symbol: "MNQ", Side: "short", SignalID: "c", Account: "Sim101"}); err == nil {
		t.Fatal("fixture: the write must fail")
	}
	if got := s.OwnCloses("Sim101", "MNQ"); len(got) != 0 {
		t.Fatalf("a close that was not written is on record: %+v", got)
	}
	if ev := s.ProtectionEvidenceFor("Sim101", "MNQ"); ev.CloseSent.IsZero() {
		t.Fatal("… but the send instant stays: half a frame may have arrived, and it only makes the net wait")
	}
}
