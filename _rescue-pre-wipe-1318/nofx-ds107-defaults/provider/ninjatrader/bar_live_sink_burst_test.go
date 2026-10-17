package ninjatrader

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"nofx/logger"
)

// DEFAULTS-SANE addendum (2026-09-24): the 22:00 CT hour-open storm dropped 101
// frames on 09-24. A normal hour-open burst must not drop at all, and anything
// above the queue cap must be counted and WARNed ONCE per burst with the count
// — never a silent counter.
//
// realisticLiveBurstFrames is the burst the queue must absorb with a STALLED
// consumer (worst case): the hour-open sends one closed frame per subscribed
// TF plus intrabar frames, far below this. RED against the old 256-cap queue:
// 512 frames with a stalled consumer drop.

const realisticLiveBurstFrames = 512

// waitQueueEmpty drains any frames earlier tests left in the process-wide queue.
func waitQueueEmpty(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(liveSinkCh) > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(liveSinkCh) > 0 {
		t.Fatal("live sink queue never drained between tests")
	}
}

// installWedgedSink installs a consumer that BLOCKS, and returns once the
// worker has taken a primer frame and is wedged on it — so the queue below is
// exactly empty and the tests' push counts are deterministic.
func installWedgedSink(t *testing.T, block chan struct{}) {
	t.Helper()
	waitQueueEmpty(t)
	consumed := make(chan struct{}, 1)
	SetLiveBarSink(func(symbol, tf, contract string, bars []Bar, receivedAt time.Time) {
		// Signal ONLY for the primer frame (T==0) — later frames must not fill
		// the signal channel or the wedged worker blocks on it instead of on
		// the block channel and can never drain.
		if len(bars) == 1 && bars[0].T == 0 {
			consumed <- struct{}{}
		}
		<-block
	})
	fanOutLiveBars("MNQ", "5m", "MNQ 12-26", []Bar{{T: 0}})
	select {
	case <-consumed:
	case <-time.After(5 * time.Second):
		t.Fatal("sink worker never took the primer frame")
	}
}

// drainLiveSink ends a wedged worker and empties the queue so later tests start
// from a clean process-wide channel.
func drainLiveSink(t *testing.T, block chan struct{}) {
	t.Helper()
	select {
	case <-block:
	default:
		close(block)
	}
	SetLiveBarSink(func(symbol, tf, contract string, bars []Bar, receivedAt time.Time) {})
	waitQueueEmpty(t)
	SetLiveBarSink(nil)
}

func TestLiveSinkRealisticHourOpenBurstDropsNothing(t *testing.T) {
	block := make(chan struct{})
	installWedgedSink(t, block)
	t.Cleanup(func() { drainLiveSink(t, block) })
	before := LiveSinkDrops()
	for i := 0; i < realisticLiveBurstFrames; i++ {
		fanOutLiveBars("MNQ", "5m", "MNQ 12-26", []Bar{{T: int64(i + 1)}})
	}
	if d := LiveSinkDrops() - before; d != 0 {
		t.Fatalf("a realistic hour-open burst (%d frames) must not drop; dropped %d", realisticLiveBurstFrames, d)
	}
}

func TestLiveSinkOverCapBurstIsCountedAndWarnedOnce(t *testing.T) {
	var buf bytes.Buffer
	old := logger.Log.Writer()
	logger.Log.SetOutput(&buf)
	defer logger.Log.SetOutput(old)

	block := make(chan struct{})
	installWedgedSink(t, block)
	t.Cleanup(func() { drainLiveSink(t, block) })

	before := LiveSinkDrops()
	total := cap(liveSinkCh) + 40
	for i := 0; i < total; i++ {
		fanOutLiveBars("MNQ", "5m", "MNQ 12-26", []Bar{{T: int64(i + 1)}})
	}
	dropped := LiveSinkDrops() - before
	if dropped != 40 {
		t.Fatalf("expected exactly 40 drops over the cap, got %d", dropped)
	}
	// Unblock the worker: it drains, and the NEXT successfully enqueued frame
	// closes the burst with ONE WARN carrying the actual count. Send only when
	// the queue has room, so the poll itself never drops (which would re-open
	// the burst and change the count).
	close(block)
	want := fmt.Sprintf("%d frame(s) dropped during the burst", dropped)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(buf.String(), want) && time.Now().Before(deadline) {
		if len(liveSinkCh) < cap(liveSinkCh) {
			fanOutLiveBars("MNQ", "5m", "MNQ 12-26", []Bar{{T: 9999}})
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n := strings.Count(buf.String(), want); n != 1 {
		t.Fatalf("the burst must be WARNed once with its count %q — got %d occurrence(s) in:\n%s", want, n, buf.String())
	}
}
