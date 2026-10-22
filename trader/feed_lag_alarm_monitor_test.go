package trader

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// TestFeedLagAlarmFiresViaMonitorTick pins m134 DS-108 (c) at the PRODUCTION
// seam: the alarm fires from monitorTick (the 60 s beat), not just from the
// inner feedLagAlarmTick helper, and the FEED desk line names the fix. MUTANT:
// drop the at.feedLagAlarmTick(now) call from monitorTick → the alarm never
// fires → RED. This replaces the old TestFeedLagAlarmSustainsThenFires, which
// drove the helper directly and could pass even if the beat never wired it.
func TestFeedLagAlarmFiresViaMonitorTick(t *testing.T) {
	at, _ := feedTrader(t)
	now := cmeOpenNow(t)

	// A live-ish feed whose newest bar keeps the age ~540 s (in the soft band
	// just under FEED DOWN) as `now` advances.
	feed := make([]market.Kline, 2)
	setAge := func(at_ time.Time) {
		feed[0] = market.Kline{OpenTime: at_.Add(-600 * time.Second).UnixMilli()}
		feed[1] = feed[0]
	}
	setAge(now)
	withProvider(t, feed)

	at.monitorTick(now)
	if !at.feedLagActiveForDesk() {
		t.Fatal("the band must be active on the first in-band monitor beat")
	}
	if at.feedLagSaid {
		t.Fatal("must NOT fire before 3 minutes of sustain")
	}

	// Still in band, 3m1s later → monitorTick fires the WARN exactly once.
	setAge(now.Add(3*time.Minute + time.Second))
	at.monitorTick(now.Add(3*time.Minute + time.Second))
	if !at.feedLagSaid {
		t.Fatal("the alarm must fire after 3 minutes of sustain, via monitorTick")
	}

	// The FEED desk line names the concrete fix.
	line := at.deskFeed(now.Add(3*time.Minute + time.Second))
	if !strings.Contains(line.Reason, "restart NinjaTrader") {
		t.Fatalf("the FEED desk line must name the fix, got reason=%q", line.Reason)
	}
}
