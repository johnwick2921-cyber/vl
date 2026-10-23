// instrument_info receive-path integration test (Phase 4a).
//
// Spins a real TCPServer + MockTCPClient, has the mock write an instrument_info
// frame (NT8's resolved contract specs), and asserts the server decodes it (no
// silent drop) and delivers it on InstrumentInfo(). Proves the Go receive path
// without NT8. Mirrors tcp_server_feed_status_test.go.
package ninjatrader

import (
	"context"
	"testing"
	"time"
)

func TestTCPServer_InstrumentInfoReceivePath(t *testing.T) {
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

	want := InstrumentInfoPayload{Symbol: "MNQ", Contract: "MNQ 06-26", PointValue: 2.0, TickSize: 0.25}
	if err := writeFromMock(client, FrameInstrumentInfo, want); err != nil {
		t.Fatalf("write instrument_info: %v", err)
	}

	select {
	case got := <-srv.InstrumentInfo():
		if got != want {
			t.Errorf("InstrumentInfo delivered %+v, want %+v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("instrument_info never delivered on InstrumentInfo() — frame decode missing (silent drop)")
	}
}

// TestFrameInstrumentInfo_WireString guards the wire type string so the Go const
// and the C# sendFrame("instrument_info", …) cannot drift.
func TestFrameInstrumentInfo_WireString(t *testing.T) {
	if got := string(FrameInstrumentInfo); got != "instrument_info" {
		t.Errorf("FrameInstrumentInfo = %q, want %q (must match C# sendFrame)", got, "instrument_info")
	}
}
