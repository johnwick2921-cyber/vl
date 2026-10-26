package ninjatrader

import (
	"context"
	"sync"
	"testing"
	"time"
)

// m433: the production FramePositions receive path hands the persistence hook
// the account and the count of NON-FLAT positions (flat quantities do not
// count), and a server with no hook wired keeps serving its cache.
func TestTCPServer_PositionsSinkReceivesNonFlatCount(t *testing.T) {
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	var mu sync.Mutex
	got := map[string]int{}
	srv.SetPositionsSink(func(account string, openCount int) {
		mu.Lock()
		got[account] = openCount
		mu.Unlock()
	})
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

	if err := writeFromMock(client, FramePositions, PositionsPayload{Account: "Sim101", Positions: []OpenPosition{
		{Symbol: "MNQ", Side: "long", Quantity: 2, AvgPrice: 1}, {Symbol: "ES", Side: "long", Quantity: 0},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := writeFromMock(client, FramePositions, PositionsPayload{Account: "SimAccount12", Positions: []OpenPosition{}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		a, okA := got["Sim101"]
		b, okB := got["SimAccount12"]
		mu.Unlock()
		if okA && okB {
			if a != 1 || b != 0 {
				t.Fatalf("sink counts: Sim101=%d (want 1 non-flat), SimAccount12=%d (want 0)", a, b)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the positions sink never fired for both frames: %+v", got)
}
