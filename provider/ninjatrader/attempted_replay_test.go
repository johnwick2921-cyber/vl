package ninjatrader

import (
	"net"
	"strings"
	"testing"
	"time"

	"vl/telemetry"
)

// FIX-DOUBLE-ENTRY — RED-first tests at the production flush path.
//
// The harness drives the REAL SendSignal → flushPendingReportFor path with a
// write hook that fails mid-flush (creating an `attempted` frame, exactly the
// production shape at tcp_server.go:2591) and then exercises the guard the
// flush must consult before re-sending it.

func newAttemptedHarness(t *testing.T) (*TCPServer, net.Conn, func(sig SignalPayload), func() net.Conn) {
	t.Helper()
	// The attempted guard's refusal counter (telemetry.gateBlockCnts) is a
	// package-global never reset between tests — an absolute-count assertion
	// breaks at -count=2+ once a prior iteration incremented the same key.
	// Reset at the START of every harness (and on cleanup) so each test owns
	// its counters. (TEST-ISOLATION-FLAKES, DS-101.)
	telemetry.ResetGateBlocksForTest()
	t.Cleanup(telemetry.ResetGateBlocksForTest)
	s := NewTCPServer(nil)
	s.attemptedVerifyWaitOverride = 200 * time.Millisecond
	srv, cli := net.Pipe()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = cli.Close()
	})
	s.conn = srv
	// A failed flush calls closeConn (s.conn -> nil AND the socket is dead);
	// rearm builds a FRESH pipe pair (a reconnect) and returns the new peer.
	rearm := func() net.Conn {
		ns, nc := net.Pipe()
		t.Cleanup(func() {
			_ = ns.Close()
			_ = nc.Close()
		})
		s.conn = ns
		return nc
	}
	return s, cli, func(sig SignalPayload) { _ = s.SendSignal(sig) }, rearm
}

func failOnceHook(t *testing.T, real func(c net.Conn, sig SignalPayload) error) func(c net.Conn, sig SignalPayload) error {
	var once bool
	return func(c net.Conn, sig SignalPayload) error {
		if !once {
			once = true
			return &net.OpError{Op: "write", Err: errDeadPipe}
		}
		return real(c, sig)
	}
}

var errDeadPipe = &testErr{"dead pipe"}

type testErr struct{ s string }

func (e *testErr) Error() string { return e.s }

func sigNow(id string) string { return time.Now().UTC().Format(time.RFC3339Nano) }

func TestAttemptedEntryFoundAtBrokerIsNotResent(t *testing.T) {
	s, _, send, rearm := newAttemptedHarness(t)
	s.SetWriteFrameHookForTest(func(c net.Conn, sig SignalPayload) error { return errDeadPipe })
	send(SignalPayload{SignalID: "e1", Account: "Sim101", Timestamp: sigNow("e1")}) // write fails → attempted, requeued
	rearm()
	if s.PendingSignalCount() != 1 || s.PendingAttemptedCount() != 1 {
		t.Fatalf("fixture: attempted frame expected in pending: %+v", s.pending)
	}
	rec := time.Now()
	s.SetReconnectAtForTest(rec)
	// A FRESH post-reconnect snapshot carrying the order named signal_id.
	s.orderSnaps.PutAt(OrderSnapshotPayload{Account: "Sim101", Orders: []NT8Order{{OrderID: "o1", Name: "e1", State: "working"}}}, rec.Add(10*time.Millisecond))
	wrote := false
	s.SetWriteFrameHookForTest(func(c net.Conn, sig SignalPayload) error { wrote = true; return nil })
	if err := s.flushPending(); err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Fatal("attempted entry found at the broker must NOT be resent")
	}
	if s.PendingSignalCount() != 0 {
		t.Fatalf("settled frame must leave the queue: %+v", s.pending)
	}
}

func TestAttemptedEntryAbsentFromFreshSnapshotResendsOnce(t *testing.T) {
	s, _, send, rearm := newAttemptedHarness(t)
	s.SetWriteFrameHookForTest(func(c net.Conn, sig SignalPayload) error { return errDeadPipe })
	send(SignalPayload{SignalID: "e2", Account: "Sim101", Timestamp: sigNow("e2")})
	cli := rearm()
	if s.PendingSignalCount() != 1 || s.PendingAttemptedCount() != 1 {
		t.Fatalf("fixture: attempted frame expected in pending: %+v", s.pending)
	}
	rec := time.Now()
	s.SetReconnectAtForTest(rec)
	s.orderSnaps.PutAt(OrderSnapshotPayload{Account: "Sim101", Orders: []NT8Order{}}, rec.Add(10*time.Millisecond))
	s.SetWriteFrameHookForTest(nil)
	frames := make(chan Envelope, 1)
	go func() {
		env, err := ReadFrame(cli)
		if err == nil {
			frames <- env
		}
		close(frames)
	}()
	if err := s.flushPending(); err != nil {
		t.Fatal(err)
	}
	if s.PendingSignalCount() != 0 {
		t.Fatalf("resent frame must leave the queue: %+v", s.pending)
	}
	select {
	case env := <-frames:
		if env.Type != FrameSignal {
			t.Fatalf("frame type %v, want signal", env.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the absent-from-book attempted entry must be resent exactly once")
	}
}

func TestAttemptedEntryNoFreshSnapshotDropsAfterWait(t *testing.T) {
	s, _, send, rearm := newAttemptedHarness(t)
	s.SetWriteFrameHookForTest(func(c net.Conn, sig SignalPayload) error { return errDeadPipe })
	send(SignalPayload{SignalID: "e3", TraderID: "t3", Timestamp: sigNow("e3")})
	rearm()
	s.SetReconnectAtForTest(time.Now())
	s.SetWriteFrameHookForTest(nil)
	if err := s.flushPending(); err != nil {
		t.Fatal(err)
	}
	if s.PendingSignalCount() != 1 {
		t.Fatalf("within the wait the frame must be HELD, got %d", s.PendingSignalCount())
	}
	time.Sleep(400 * time.Millisecond) // past the 200ms wait → fail closed
	if err := s.flushPending(); err != nil {
		t.Fatal(err)
	}
	if s.PendingSignalCount() != 0 {
		t.Fatalf("after the wait the unverified frame must be DROPPED, got %d", s.PendingSignalCount())
	}
	if got := GateBlockCountForTest("t3", attemptedGateName); got != 1 {
		t.Fatalf("refusal must be counted (attempted_entry_unverified), got %d", got)
	}
}

func TestAttemptedEntryStaleSnapshotTreatedAsNoSnapshot(t *testing.T) {
	s, _, send, rearm := newAttemptedHarness(t)
	s.SetWriteFrameHookForTest(func(c net.Conn, sig SignalPayload) error { return errDeadPipe })
	send(SignalPayload{SignalID: "e4", Account: "Sim101", Timestamp: sigNow("e4")})
	rearm()
	rec := time.Now()
	// A snapshot received BEFORE the reconnect proves nothing about it.
	s.orderSnaps.PutAt(OrderSnapshotPayload{Account: "Sim101", Orders: []NT8Order{{OrderID: "o4", Name: "e4", State: "working"}}}, rec.Add(-time.Minute))
	s.SetReconnectAtForTest(rec)
	s.SetWriteFrameHookForTest(nil)
	if err := s.flushPending(); err != nil {
		t.Fatal(err)
	}
	if s.PendingSignalCount() != 1 {
		t.Fatalf("a pre-reconnect snapshot must be treated as NO snapshot (hold), got %d", s.PendingSignalCount())
	}
}

func TestAttemptedEntryVariantsLimitAndStopEntry(t *testing.T) {
	for _, kind := range []string{"limit", "stop_entry"} {
		t.Run(kind, func(t *testing.T) {
			s, _, send, rearm := newAttemptedHarness(t)
			s.SetWriteFrameHookForTest(func(c net.Conn, sig SignalPayload) error { return errDeadPipe })
			send(SignalPayload{SignalID: "e5-" + kind, Account: "Sim101", OrderType: kind, LimitPrice: 100, StopPrice: 101, Timestamp: sigNow("e5")})
			rearm()
			rec := time.Now()
			s.SetReconnectAtForTest(rec)
			// found at broker → not resent
			s.orderSnaps.PutAt(OrderSnapshotPayload{Account: "Sim101", Orders: []NT8Order{{OrderID: "o5", Name: "e5-" + kind, State: "working"}}}, rec.Add(10*time.Millisecond))
			wrote := false
			s.SetWriteFrameHookForTest(func(c net.Conn, sig SignalPayload) error { wrote = true; return nil })
			if err := s.flushPending(); err != nil {
				t.Fatal(err)
			}
			if wrote {
				t.Fatalf("%s attempted entry found at broker must NOT be resent", kind)
			}
			if s.PendingSignalCount() != 0 {
				t.Fatalf("%s settled frame must leave the queue", kind)
			}
		})
	}
}

func TestFreshEntryFlushesWithoutGuard(t *testing.T) {
	// A never-attempted entry keeps today's behaviour: no snapshot, no wait.
	s, cli, send, _ := newAttemptedHarness(t)
	s.SetReconnectAtForTest(time.Now())
	frames := make(chan Envelope, 1)
	go func() {
		env, err := ReadFrame(cli)
		if err == nil {
			frames <- env
		}
		close(frames)
	}()
	send(SignalPayload{SignalID: "e6", Timestamp: sigNow("e6")})
	if s.PendingSignalCount() != 0 {
		t.Fatalf("fresh frame should flush immediately, got %d queued", s.PendingSignalCount())
	}
	select {
	case env := <-frames:
		if env.Type != FrameSignal {
			t.Fatalf("fresh entry must flush unguarded: %v", env.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fresh entry must flush unguarded (no frame)")
	}
}

func TestAttemptedGuardBootLineValues(t *testing.T) {
	// The boot line reads its values; the wait knob is parsed from env.
	t.Setenv(attemptedEntryVerifyWaitEnv, "7")
	if attemptedVerifyWait() != 7*time.Second {
		t.Fatalf("env knob must tune the wait: %v", attemptedVerifyWait())
	}
	t.Setenv(attemptedEntryVerifyWaitEnv, "bogus")
	if attemptedVerifyWait() != attemptedVerifyWaitDefault {
		t.Fatalf("bogus env must fall back to the default: %v", attemptedVerifyWait())
	}
	if !strings.Contains(attemptedGateName, "attempted_entry") {
		t.Fatal("gate name drift")
	}
}

// P1 pin (DS-101 adversarial review, adopted into the wave, RED first): a
// frame's decision comes from its OWN account book, never another account's.
func TestReviewAttemptedFrameDecidesFromItsOwnAccountBook(t *testing.T) {
	s, _, send, rearm := newAttemptedHarness(t)
	s.SetWriteFrameHookForTest(func(c net.Conn, sig SignalPayload) error { return errDeadPipe })
	send(SignalPayload{SignalID: "e-a", Account: "A", TraderID: "t-a", Timestamp: sigNow("e-a")})
	rearm()
	rec := time.Now()
	s.SetReconnectAtForTest(rec)
	// Account B has a FRESH EMPTY book; account A has NO book. B's evidence
	// must never resend A's frame — the P1 double.
	s.orderSnaps.PutAt(OrderSnapshotPayload{Account: "B", Orders: []NT8Order{}}, rec.Add(5*time.Millisecond))
	wrote := false
	s.SetWriteFrameHookForTest(func(c net.Conn, sig SignalPayload) error { wrote = true; return nil })
	if err := s.flushPending(); err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Fatal("REVIEW FINDING: frame account=A resend-decided from account B's fresh book — the guard must HOLD, never resend on another account's evidence")
	}
	if s.PendingSignalCount() != 1 {
		t.Fatalf("A's frame must stay HELD on B's evidence: got %d", s.PendingSignalCount())
	}
	// Fail closed after the wait: still no A book → drop + count.
	time.Sleep(400 * time.Millisecond) // past the 200ms wait
	if err := s.flushPending(); err != nil {
		t.Fatal(err)
	}
	if s.PendingSignalCount() != 0 {
		t.Fatalf("after the wait the unverified A frame must be DROPPED, got %d", s.PendingSignalCount())
	}
	if got := GateBlockCountForTest("t-a", attemptedGateName); got != 1 {
		t.Fatalf("refusal must be counted (attempted_entry_unverified), got %d", got)
	}
}

// P1 positive pin: only A's OWN fresh book releases A's frame.
func TestAttemptedEntryResendsOnlyOnItsOwnFreshBook(t *testing.T) {
	s, _, send, rearm := newAttemptedHarness(t)
	s.SetWriteFrameHookForTest(func(c net.Conn, sig SignalPayload) error { return errDeadPipe })
	send(SignalPayload{SignalID: "e-ab", Account: "A", TraderID: "t-ab", Timestamp: sigNow("e-ab")})
	cli := rearm()
	rec := time.Now()
	s.SetReconnectAtForTest(rec)
	// B fresh empty → hold (the negative pin above); then A's OWN fresh empty
	// book → resend exactly once.
	s.orderSnaps.PutAt(OrderSnapshotPayload{Account: "B", Orders: []NT8Order{}}, rec.Add(5*time.Millisecond))
	s.SetWriteFrameHookForTest(nil)
	frames := make(chan Envelope, 1)
	go func() {
		env, err := ReadFrame(cli)
		if err == nil {
			frames <- env
		}
		close(frames)
	}()
	if err := s.flushPending(); err != nil {
		t.Fatal(err)
	}
	if s.PendingSignalCount() != 1 {
		t.Fatalf("fixture: B's book must not release A's frame, got %d", s.PendingSignalCount())
	}
	s.orderSnaps.PutAt(OrderSnapshotPayload{Account: "A", Orders: []NT8Order{}}, rec.Add(15*time.Millisecond))
	if err := s.flushPending(); err != nil {
		t.Fatal(err)
	}
	if s.PendingSignalCount() != 0 {
		t.Fatalf("A's own fresh book must release the frame, got %d", s.PendingSignalCount())
	}
	select {
	case env := <-frames:
		if env.Type != FrameSignal {
			t.Fatalf("frame type %v, want signal", env.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("A's frame must be resent exactly once on A's OWN fresh book")
	}
}

// P2 pin (DS-101): the echo-settle branch is live code — deleting it (M3)
// must fail this test.
func TestAttemptedEntryEchoSettlesWithNoFreshBook(t *testing.T) {
	s, _, send, rearm := newAttemptedHarness(t)
	s.SetWriteFrameHookForTest(func(c net.Conn, sig SignalPayload) error { return errDeadPipe })
	send(SignalPayload{SignalID: "e-echo", Account: "A", TraderID: "t-echo", Timestamp: sigNow("e-echo")})
	rearm()
	rec := time.Now()
	s.SetReconnectAtForTest(rec)
	// A fill echo lands after the reconnect; NO snapshot at all.
	s.NoteEchoForTest("e-echo", rec.Add(5*time.Millisecond))
	wrote := false
	s.SetWriteFrameHookForTest(func(c net.Conn, sig SignalPayload) error { wrote = true; return nil })
	if err := s.flushPending(); err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Fatal("an echoed attempted entry must never be resent")
	}
	if s.PendingSignalCount() != 0 {
		t.Fatalf("echo-settled frame must leave the queue, got %d", s.PendingSignalCount())
	}
	if got := GateBlockCountForTest("t-echo", attemptedGateName); got != 0 {
		t.Fatalf("an echo settle is NOT a refusal, got %d", got)
	}
}
