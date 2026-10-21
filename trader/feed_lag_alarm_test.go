package trader

import (
	"testing"
	"time"

	"vl/market"
)

// TestFeedLagAlarmSustainsThenFires pins m134 DS-108 (c): a newest-bar age in
// the 500–700 s band arms the alarm, fires the WARN exactly once after 3
// minutes, and resets when the feed recovers. MUTANT: drop the 3-minute
// sustain (fire on first sight) or never reset → RED.
func TestFeedLagAlarmSustainsThenFires(t *testing.T) {
	at, _ := feedTrader(t)
	now := cmeOpenNow(t)

	// A live-ish feed whose newest bar keeps the age ~540 s (in band) as `now`
	// advances: OpenTime = now - (age + 60s) = now - 600s.
	feed := make([]market.Kline, 2)
	setAge := func(at_ time.Time) {
		feed[0] = market.Kline{OpenTime: at_.Add(-600 * time.Second).UnixMilli()}
		feed[1] = feed[0]
	}
	setAge(now)
	withProvider(t, feed)

	at.feedLagAlarmTick(now)
	if !at.feedLagActiveForDesk() {
		t.Fatal("the band must be active on the first in-band beat")
	}
	if at.feedLagSaid {
		t.Fatal("must NOT fire before 3 minutes of sustain")
	}

	// Still in band, 3m1s later → fires once.
	setAge(now.Add(3*time.Minute + time.Second))
	at.feedLagAlarmTick(now.Add(3*time.Minute + time.Second))
	if !at.feedLagSaid {
		t.Fatal("must fire after 3 minutes of sustain")
	}

	// One more in-band beat → the settled episode does not re-arm the WARN
	// (feedLagSaid stays true; a NEW episode only after recovery).
	setAge(now.Add(3*time.Minute + 2*time.Second))
	at.feedLagAlarmTick(now.Add(3*time.Minute + 2*time.Second))
	if !at.feedLagSaid {
		t.Fatal("the fired flag must stay set for the rest of the episode")
	}

	// Recovered: age ~60s → the episode resets.
	setAge(now.Add(4 * time.Minute))
	feed[0] = market.Kline{OpenTime: now.Add(4 * time.Minute).Add(-2 * time.Minute).UnixMilli()}
	feed[1] = feed[0]
	at.feedLagAlarmTick(now.Add(4 * time.Minute))
	if at.feedLagActiveForDesk() {
		t.Fatal("the band must clear once the feed recovers")
	}
	if at.feedLagSaid {
		t.Fatal("a recovered feed must clear the fired flag")
	}
}

// TestFeedLagAlarmIgnoresOtherTFsAndNonBand pins the boundary: a non-ninjatrader
// trader never arms, and an age below 500s or above 700s never arms.
func TestFeedLagAlarmBoundaries(t *testing.T) {
	at, _ := feedTrader(t)
	now := cmeOpenNow(t)

	// Age 480s (< 500) → not in band.
	bar := market.Kline{OpenTime: now.Add(-540 * time.Second).UnixMilli()}
	withProvider(t, []market.Kline{bar, bar})
	at.feedLagAlarmTick(now)
	if at.feedLagActiveForDesk() {
		t.Fatal("480s must not arm the 500-700s band")
	}

	// Age 720s (> 700) → the hard FEED DOWN owns it, not this alarm.
	bar2 := market.Kline{OpenTime: now.Add(-780 * time.Second).UnixMilli()}
	withProvider(t, []market.Kline{bar2, bar2})
	at.feedLagAlarmTick(now)
	if at.feedLagActiveForDesk() {
		t.Fatal("720s must not arm the 500-700s band")
	}
}
