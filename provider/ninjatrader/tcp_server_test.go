package ninjatrader

import (
	"context"
	"net"
	"testing"
	"time"
)

// freeEphemeralAddr returns a 127.0.0.1 address with an unused port. We bind
// briefly to find a free port, close it, and return the string. There's a
// small race window vs the next bind; tests retry implicitly via test harness.
func freeEphemeralAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func TestTCPServer_StartStop(t *testing.T) {
	srv := NewTCPServer(nil)
	srv.SetAddrForTest(freeEphemeralAddr(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if srv.IsConnected() {
		t.Error("IsConnected should be false before any client dials")
	}
	if err := srv.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	// After Stop, the port should be releasable — bind to verify.
	ln, err := net.Listen("tcp", srv.addr)
	if err != nil {
		t.Errorf("port still bound after Stop: %v", err)
	} else {
		_ = ln.Close()
	}
}

func TestTCPServer_AcceptSingleClient_RejectSecond(t *testing.T) {
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	c1, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial 1: %v", err)
	}
	defer c1.Close()

	// Wait for server-side accept to complete.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.IsConnected() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !srv.IsConnected() {
		t.Fatal("server did not accept first client")
	}

	c2, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial 2: %v", err)
	}
	defer c2.Close()

	// c2 should be closed by the server. ReadFrame on c2 should return EOF
	// (or net.ErrClosed equivalents) quickly.
	_ = c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	one := make([]byte, 1)
	_, err = c2.Read(one)
	if err == nil {
		t.Error("expected second client to be closed; got readable bytes")
	}
}

func TestTCPServer_ContextCancellation(t *testing.T) {
	srv := NewTCPServer(nil)
	srv.SetAddrForTest(freeEphemeralAddr(t))
	ctx, cancel := context.WithCancel(context.Background())
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel()
	// Give the accept loop a tick to notice cancellation, then Stop.
	time.Sleep(acceptLoopPollInterval + 50*time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- srv.Stop() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Stop after cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not return within 3s after context cancellation")
	}
}

func TestTCPServer_SendSignal_QueuedOnDisconnect(t *testing.T) {
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	// Send a signal before any client is connected — should queue.
	sig := SignalPayload{
		Symbol: "MNQ", Side: "long", Quantity: 1,
		Entry: 21500.00, StopLoss: 21485.00, TakeProfit: 21525.00,
		SignalID: "queued-sig", Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := srv.SendSignal(sig); err != nil {
		t.Fatalf("SendSignal pre-connect: %v", err)
	}

	// Now connect the mock client; it should receive the queued signal.
	client := NewMockTCPClient(addr, 50*time.Millisecond)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client Start: %v", err)
	}
	defer client.Stop()

	select {
	case got := <-client.SignalsReceived():
		if got.SignalID != "queued-sig" {
			t.Errorf("signal_id: want queued-sig got %q", got.SignalID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client did not receive queued signal within 3s")
	}
}

func TestTCPServer_StaleSignalDropped(t *testing.T) {
	if testing.Short() {
		t.Skip("skip stale-signal test in short mode")
	}
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	// Override stale cutoff to 100ms so the test doesn't take 60s.
	srv.SetStaleSignalAgeForTest(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	sig := SignalPayload{
		Symbol: "MNQ", Side: "long", Quantity: 1,
		Entry: 21500.00, StopLoss: 21485.00, TakeProfit: 21525.00,
		SignalID: "stale-sig", Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := srv.SendSignal(sig); err != nil {
		t.Fatalf("SendSignal: %v", err)
	}
	// Wait long enough that the signal is considered stale.
	time.Sleep(250 * time.Millisecond)

	client := NewMockTCPClient(addr, 50*time.Millisecond)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client Start: %v", err)
	}
	defer client.Stop()

	// Client should NOT receive the stale signal within 1s.
	select {
	case got := <-client.SignalsReceived():
		t.Errorf("expected stale signal dropped; got %+v", got)
	case <-time.After(1 * time.Second):
		// Pass — no signal delivered.
	}
}

func TestTCPServer_FillRoundTrip(t *testing.T) {
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	client := NewMockTCPClient(addr, 100*time.Millisecond)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client Start: %v", err)
	}
	defer client.Stop()

	// Wait for server to register the connection.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.IsConnected() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	sig := SignalPayload{
		Symbol: "MNQ", Side: "long", Quantity: 1,
		Entry: 21500.00, StopLoss: 21485.00, TakeProfit: 21525.00,
		SignalID: "roundtrip-1", Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := srv.SendSignal(sig); err != nil {
		t.Fatalf("SendSignal: %v", err)
	}

	select {
	case fill := <-srv.Fills():
		if fill.SignalID != "roundtrip-1" {
			t.Errorf("fill.SignalID: want roundtrip-1 got %q", fill.SignalID)
		}
		if fill.Status != "filled" {
			t.Errorf("fill.Status: want filled got %q", fill.Status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no fill within 3s")
	}
}

// TestTCPServer_IdleConnectionSurvives (Plan 1.5.7) — proves that an idle
// client connection (no inbound frames) is NOT dropped by a spurious read
// timeout. Under the pre-1.5.7 code path, the read loop set a 2s read
// deadline; after 2s of silence the deadline fired, and although the loop
// continued, any frame that arrived mid-deadline could be misaligned by
// the partial-read consumption. This test exercises the "no frame at all"
// case: a properly-implemented read loop with no deadline must hold the
// connection open indefinitely.
//
// We wait 5 seconds (>2× the old deadline) and assert IsConnected stays
// true throughout.
func TestTCPServer_IdleConnectionSurvives(t *testing.T) {
	if testing.Short() {
		t.Skip("skip 5s idle test in short mode")
	}
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	// Open a raw TCP connection (no MockTCPClient — we want to send ZERO
	// bytes, not even heartbeats, to isolate the deadline behavior).
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// Wait for the server to register the connection.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.IsConnected() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !srv.IsConnected() {
		t.Fatal("server did not register the client connection")
	}

	// Sample IsConnected every 500ms for 5 seconds. Under the pre-1.5.7
	// 2s-deadline code, any spurious disconnect would flip this to false
	// well before the 5s mark.
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		if !srv.IsConnected() {
			t.Fatalf("connection dropped at t=%dms despite client sending nothing",
				(i+1)*500)
		}
	}
}

// TestTCPServer_ShutdownUnblocksIdleRead (Plan 1.5.7) — proves the
// per-connection ctx-cancel watcher correctly unblocks an idle blocked-on-
// read goroutine when the server context is cancelled. Without the watcher,
// removing the 2s read deadline would leave the readLoop blocked forever
// on an idle connection during shutdown.
//
// We dial in, wait for IsConnected, then call Stop() and assert it returns
// within a few seconds (proving the read goroutine was unblocked, drained
// the wg, and Stop completed).
func TestTCPServer_ShutdownUnblocksIdleRead(t *testing.T) {
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// Wait for the server to register the connection.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.IsConnected() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !srv.IsConnected() {
		t.Fatal("server did not register the client connection")
	}

	// Now Stop the server. The readLoop is blocked in ReadFrame on an
	// idle conn; the ctx-cancel watcher must close the conn, unblocking
	// the read so Stop() can drain the WaitGroup.
	done := make(chan error, 1)
	go func() { done <- srv.Stop() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Stop: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not return within 3s — readLoop likely still blocked, watcher broken")
	}
}
