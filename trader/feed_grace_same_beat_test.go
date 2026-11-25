package trader

import (
	"testing"
	"time"

	"vl/logger"
	"vl/market"
	"vl/telemetry"
)

// m456 (10-29 15:06:51 CT): the monitor beat that FIRST sees the NT8 link-up
// edge must still get the FOLD 3 reconnect grace. The ticker hands monitorTick
// its `now`; driveDeadManWatchdog then ran inside the beat and stamped the
// link-up with its OWN, later time.Now(); checkFeedDown(now) read that anchor
// as AFTER now (elapsed < 0 = "a clock that moved back", never grace) and
// raised "FEED DOWN: no NT8 bar for 6h38m51s" one line after "link back UP".
//
// Production call site: monitorTick(now) + a REAL link replacement through the
// dead-man watchdog. `now` is the beat's injected clock (cmeOpenNow, CME open),
// so the test is deterministic and independent of the wall clock / CME halt.
func TestMonitorTickKeepsTheReconnectGraceOnTheLinkUpBeat(t *testing.T) {
	at, s, broker, conn := deadManLoopback(t)
	at.config.Exchange = "ninjatrader"
	at.exchange = "ninjatrader"
	at.driveDeadManWatchdog() // baseline sample: link up, epoch recorded
	reconnectUnseen(t, s, broker, conn)

	now := cmeOpenNow(t)
	at.startTime = now.Add(-7 * time.Minute) // booted 7 min before the beat: not the boot grace
	// The cold ring holds only NT8's stale replay tail: 6h38m51s old at the beat.
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - (6*3600+38*60+51)*1000 - 60_000}})
	hook := &warnCaptureHook{}
	logger.Log.AddHook(hook)
	graced := telemetry.FeedDownGracedCount()

	at.monitorTick(now) // this beat is the one that sees the link-up edge

	if n := hook.count("dead-man watchdog: NT8 TCP link"); n != 1 {
		t.Fatalf("fixture: this beat must be the one that sees the link-up edge, saw %d watchdog lines", n)
	}
	if n := hook.count("🚨 FEED DOWN"); n != 0 {
		t.Fatalf("FEED DOWN raised on the very beat that logged 'link back UP' (stale replay tail, no live bar yet): want the reconnect grace, got %d ERROR line(s)", n)
	}
	alerts, _ := at.store.Alert().List(at.id, 50)
	for _, a := range alerts {
		if a.Kind == "feed-down" {
			t.Fatalf("no FEED DOWN P0 alert inside the reconnect grace, got %+v", a)
		}
	}
	if n := hook.count("⏳ feed: waiting for the first live bar after reconnect"); n != 1 {
		t.Fatalf("the wait must be SAID once, got %d", n)
	}
	if telemetry.FeedDownGracedCount() != graced+1 {
		t.Fatal("the graced verdict must be counted")
	}
}
