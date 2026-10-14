package trader

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"vl/kernel"
	"vl/logger"
	"vl/market"
	"vl/telemetry"
)

// ── FOLD 3 (DS-106, 10-14 15:04:28) — no false FEED DOWN in the reconnect
// window ──────────────────────────────────────────────────────────────────
//
// After the 15:01 reboot the dead-man watchdog logged "NT8 TCP link back UP"
// at 15:04:22, and ONE line later this beat said "🚨 FEED DOWN: no NT8 bar for
// 6h41m29s while CME is OPEN" (trader 12345): the ring still held the
// rehydrated store tail and no live bar had landed yet. Inside the grace the
// verdict is said as "⏳ feed: waiting…" and counted; a real outage still
// fires FEED DOWN once the grace ends.

type warnCaptureHook struct {
	mu    sync.Mutex
	lines []string
}

func (h *warnCaptureHook) Levels() []logrus.Level {
	return []logrus.Level{logrus.WarnLevel, logrus.ErrorLevel}
}
func (h *warnCaptureHook) Fire(e *logrus.Entry) error {
	h.mu.Lock()
	h.lines = append(h.lines, e.Message)
	h.mu.Unlock()
	return nil
}
func (h *warnCaptureHook) count(sub string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, l := range h.lines {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

// The 10-14 shape at the production call site (monitorTick → checkFeedDown):
// a stale rehydrated stamp 6 s after a link-up waits; the grace ends and a
// feed that never sent a live bar is FEED DOWN, exactly as before.
func TestFeedDownWaitsForTheFirstLiveBarAfterReconnect(t *testing.T) {
	at, st := feedTrader(t)
	now := cmeOpenNow(t)
	at.startTime = now.Add(-time.Hour) // not a fresh boot: the link-up is the anchor
	linkUp := now.Add(-6 * time.Second)
	at.noteFeedLinkUp(linkUp)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - (6*time.Hour + 41*time.Minute).Milliseconds()}})
	hook := &warnCaptureHook{}
	logger.Log.AddHook(hook)
	graced := telemetry.FeedDownGracedCount()

	at.monitorTick(now)
	if got := feedAlerts(t, st, "feed-down"); got != 0 {
		t.Fatalf("6 s after the link came back, a stale rehydrated stamp is not an outage — got %d FEED DOWN alert(s)", got)
	}
	if hook.count("🚨 FEED DOWN") != 0 {
		t.Fatal("no FEED DOWN line inside the reconnect grace")
	}
	if n := hook.count("⏳ feed: waiting for the first live bar after reconnect (link up " + kernel.ClockCTSeconds(linkUp) + " CT"); n != 1 {
		t.Fatalf("the wait must be SAID once, naming the link-up, got %d", n)
	}
	if telemetry.FeedDownGracedCount() != graced+1 {
		t.Fatal("the graced verdict must be counted")
	}
	at.monitorTick(now.Add(30 * time.Second))
	if hook.count("⏳ feed: waiting") != 1 || telemetry.FeedDownGracedCount() != graced+1 {
		t.Fatal("one link-up, one ⏳ line and one count")
	}

	// No live bar by the end of the grace: FEED DOWN, as before.
	at.monitorTick(linkUp.Add(feedReconnectGraceWindow + time.Second))
	if got := feedAlerts(t, st, "feed-down"); got != 1 {
		t.Fatalf("a feed with no live bar after the grace is a real outage — want 1 FEED DOWN alert, got %d", got)
	}
	if hook.count("🚨 FEED DOWN: no NT8 bar for") != 1 {
		t.Fatal("the FEED DOWN ERROR must fire once the grace has ended")
	}
}

// The grace ends at the FIRST live bar: a live 1m frame received after the
// link-up hands the verdict back to the ordinary rule at once.
func TestFeedDownGraceEndsAtTheFirstLiveBar(t *testing.T) {
	at, st := feedTrader(t)
	now := cmeOpenNow(t)
	at.startTime = now.Add(-time.Hour)
	at.noteFeedLinkUp(now.Add(-6 * time.Second))
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 11*60_000 - 60_000}})
	recv := now.Add(-time.Second)
	kernel.FeedLastLiveReceipt = func(string, string) (time.Time, bool) { return recv, true }
	t.Cleanup(func() { kernel.FeedLastLiveReceipt = nil })

	at.monitorTick(now)
	if got := feedAlerts(t, st, "feed-down"); got != 1 {
		t.Fatalf("after a live bar the grace is over: an 11-min-old newest stamp is FEED DOWN, got %d", got)
	}
}

// The anchor comes from the production edge: a real link replaced between two
// samples (dead-man wdReconnected) records the link-up the grace reads.
func TestDeadManReconnectAnchorsTheFeedGrace(t *testing.T) {
	at, s, broker, conn := deadManLoopback(t)
	at.driveDeadManWatchdog()
	reconnectUnseen(t, s, broker, conn)
	before := time.Now()
	if ev := at.driveDeadManWatchdog(); ev != wdReconnected {
		t.Fatalf("fixture: want a reconnect, got event %d", ev)
	}
	at.feedGraceMu.Lock()
	up := at.feedLinkUpAt
	at.feedGraceMu.Unlock()
	if up.Before(before) || time.Since(up) > 5*time.Second {
		t.Fatalf("the reconnect edge must anchor the FEED DOWN grace at the link-up, got %v (sample began %v)", up, before)
	}
}
