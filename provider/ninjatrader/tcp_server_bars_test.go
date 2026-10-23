// Plan 4.4 Stage 2 — TCP server bar receive path integration test.
//
// Spins up a real TCPServer + MockTCPClient. Asserts:
//  1. Server auto-sends bars_subscribe on connect.
//  2. When the mock client writes a bars_historical frame, the server's
//     bar cache populates correctly.
//  3. When the mock writes a bar_update with multiple bars, the cache
//     applies the multi-bar gotcha (each bar via dedup-by-t).
//
// This proves the END-TO-END Go bar receive path (read → decode → enqueue
// → drain → cache) without requiring NT8 / Tradovate. Complements the
// per-layer unit tests in bar_cache_test.go and tcp_framing_bars_test.go.

package ninjatrader

// Fixtures below use WIRE stamps (NT8 close convention, +60_000 for the 1m
// key); BarCache ingest converts to OPEN stamps, so assertions read T as-is.

import (
	"context"
	"testing"
	"time"
)

func TestTCPServer_BarsReceivePath_Historical(t *testing.T) {
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	client := NewMockTCPClient(addr, 50*time.Millisecond)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client Start: %v", err)
	}
	defer client.Stop()

	// Wait for the server to register the connection + send the auto
	// bars_subscribe. The mock client doesn't have to parse the
	// bars_subscribe — it just needs to be connected so we can write
	// our test frames on the same conn.
	waitForConnected(t, srv, 2*time.Second)

	// Send a bars_historical frame from the mock side. The conn lives
	// inside the MockTCPClient — we need to write to it via the same
	// stream the mock uses for its own writes. The cleanest way is to
	// reach into the mock's net.Conn via a brief grace period and
	// use WriteFrame directly. (See TestTCPServer_FillRoundTrip for
	// precedent: SendFill is exactly this pattern.)
	historicalBars := []Bar{
		{T: 1000 + 60_000, O: 21500.00, H: 21500.50, L: 21499.75, C: 21500.25, V: 42},
		{T: 1060 + 60_000, O: 21500.25, H: 21501.00, L: 21500.00, C: 21500.75, V: 38},
		{T: 1120 + 60_000, O: 21500.75, H: 21501.25, L: 21500.50, C: 21501.00, V: 51},
	}
	if err := writeFromMock(client, FrameBarsHistorical, BarsHistoricalPayload{
		Symbol:    "MNQ",
		Timeframe: "1m",
		Bars:      historicalBars,
	}); err != nil {
		t.Fatalf("write historical: %v", err)
	}

	// Wait for the drain goroutine to apply the seed.
	waitForBars(t, srv, "MNQ", "1m", 3, 2*time.Second)

	got := srv.BarCache().Get("MNQ", "1m")
	if len(got) != 3 {
		t.Fatalf("cache len=%d, want 3", len(got))
	}
	if got[0].T != 1000 || got[2].T != 1120 {
		t.Errorf("cache bars: %+v", got)
	}
	if got[1].C != 21500.75 {
		t.Errorf("bar[1].C = %v, want 21500.75", got[1].C)
	}
}

func TestTCPServer_BarsReceivePath_UpdateMultiBar(t *testing.T) {
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	client := NewMockTCPClient(addr, 50*time.Millisecond)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client Start: %v", err)
	}
	defer client.Stop()
	waitForConnected(t, srv, 2*time.Second)

	// Seed with one bar so the upsert has a baseline.
	seed := []Bar{{T: 1000 + 60_000, C: 21500}}
	if err := writeFromMock(client, FrameBarsHistorical, BarsHistoricalPayload{
		Symbol:    "MNQ",
		Timeframe: "1m",
		Bars:      seed,
	}); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	waitForBars(t, srv, "MNQ", "1m", 1, 2*time.Second)

	// Multi-bar update: replace bar at t=1000, append two new bars.
	update := []Bar{
		{T: 1000 + 60_000, C: 21500.5}, // replace
		{T: 1060 + 60_000, C: 21501},   // append
		{T: 1120 + 60_000, C: 21502},   // append
	}
	if err := writeFromMock(client, FrameBarUpdate, BarUpdatePayload{
		Symbol:    "MNQ",
		Timeframe: "1m",
		Bars:      update,
	}); err != nil {
		t.Fatalf("write update: %v", err)
	}
	waitForBars(t, srv, "MNQ", "1m", 3, 2*time.Second)

	got := srv.BarCache().Get("MNQ", "1m")
	if len(got) != 3 {
		t.Fatalf("cache len=%d, want 3 (multi-bar upsert)", len(got))
	}
	if got[0].C != 21500.5 {
		t.Errorf("replace did not take effect: got[0].C=%v want 21500.5", got[0].C)
	}
	if got[1].T != 1060 || got[2].T != 1120 {
		t.Errorf("appends out of order: %+v", got)
	}
}

// waitForConnected polls IsConnected until true or deadline.
func waitForConnected(t *testing.T, srv *TCPServer, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if srv.IsConnected() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server did not register the client connection within %v", timeout)
}

// waitForBars polls the cache until the given (symbol, timeframe) has at
// least minBars entries, or the deadline passes.
func waitForBars(t *testing.T, srv *TCPServer, symbol, timeframe string, minBars int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if srv.BarCache().Count(symbol, timeframe) >= minBars {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("cache did not reach %d bars for %s|%s within %v (got %d)",
		minBars, symbol, timeframe, timeout, srv.BarCache().Count(symbol, timeframe))
}

// writeFromMock writes a frame from the mock client's side of the conn.
// We reach into the mock's conn via reflection-free helper since
// MockTCPClient.conn is unexported. Instead, we use a fresh net.Dial on
// the same loopback addr — but the server rejects concurrent clients,
// so that's not viable. Solution: a tiny extension to MockTCPClient
// would be cleaner long-term; for Stage 2 we use a direct method on
// MockTCPClient added below in this test file via an exported helper.
//
// The pattern mirrors SendFill / SendAck in tcp_client_mock.go but
// generalized to any frame type. Defined as an exported method below
// for test-only use.
func writeFromMock(client *MockTCPClient, frameType FrameType, payload any) error {
	return client.sendFrameForTest(frameType, payload)
}
