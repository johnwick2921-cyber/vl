package ninjatrader

import (
	"context"
	"testing"
	"time"
)

// W-EXEC-TRUTH W4 / D24 — a bar_update frame that reaches us long after it was
// emitted is not a LIVE entry event. It is still real data and still belongs
// in the cache, but treating a backlogged frame as "something just happened"
// is how a stalled queue draining all at once mints entries on old prices.
// The check lives at the WIRE boundary and reads only the frame's own stamp —
// it knows nothing about any evaluator.

func TestDrainBarIngest_StaleLiveFrameReachesTheCacheButNotTheSink(t *testing.T) {
	live := make(chan string, 1)
	SetLiveBarSink(func(symbol, tf, contract string, bars []Bar, receivedAt time.Time) {
		live <- tf
	})
	t.Cleanup(func() { SetLiveBarSink(nil) })

	s := NewTCPServer(nil)
	s.barIngestCh = make(chan barIngestMsg, 4)
	s.wg.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); s.wg.Wait() }()
	go s.drainBarIngest(ctx)

	old := time.Now().Add(-5 * time.Minute).UnixMilli()
	s.barIngestCh <- barIngestMsg{
		symbol: "MNQ", timeframe: "5m", contract: "MNQ 12-26",
		bars: []Bar{{T: old + 300000, O: 1, H: 2, L: 0.5, C: 1.5, V: 7, Final: true, EmittedAt: old}},
	}
	select {
	case tf := <-live:
		t.Fatalf("a frame emitted five minutes ago must not be fanned out as a live entry event (got tf=%s)", tf)
	case <-time.After(400 * time.Millisecond):
	}
	if n := StaleLiveFrames(); n != 1 {
		t.Fatalf("the refused frame must be counted, got %d", n)
	}
	if s.barCache.Count("MNQ", "5m") == 0 {
		t.Fatalf("the frame is still real data and must reach the cache")
	}
}

func TestDrainBarIngest_FreshLiveFrameStillFansOut(t *testing.T) {
	live := make(chan string, 1)
	SetLiveBarSink(func(symbol, tf, contract string, bars []Bar, receivedAt time.Time) {
		live <- tf
	})
	t.Cleanup(func() { SetLiveBarSink(nil) })

	s := NewTCPServer(nil)
	s.barIngestCh = make(chan barIngestMsg, 4)
	s.wg.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); s.wg.Wait() }()
	go s.drainBarIngest(ctx)

	now := time.Now().UnixMilli()
	s.barIngestCh <- barIngestMsg{
		symbol: "MNQ", timeframe: "5m", contract: "MNQ 12-26",
		bars: []Bar{{T: now, O: 1, H: 2, L: 0.5, C: 1.5, V: 7, Final: true, EmittedAt: now}},
	}
	select {
	case <-live:
	case <-time.After(2 * time.Second):
		t.Fatalf("a frame emitted just now must still fan out")
	}
}
