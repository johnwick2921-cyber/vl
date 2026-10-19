package trader

import (
	"time"

	"vl/market"
)

// HealthIsRunning reports whether this trader's loop is currently running
// (P2-6: /api/health's trader-running count). Read-only.
func (at *AutoTrader) HealthIsRunning() bool {
	if at == nil {
		return false
	}
	at.isRunningMutex.RLock()
	defer at.isRunningMutex.RUnlock()
	return at.isRunning
}

// HealthFeedStatus returns the NT8 price-feed status string from the most
// recent feed_status frame ("" until one arrives — the admission gate is
// default-ALLOW in that state, and the health payload says so, never a
// fabricated "up").
func (at *AutoTrader) HealthFeedStatus() string {
	if at == nil {
		return ""
	}
	nt := at.armedTrader()
	if nt == nil {
		return ""
	}
	return nt.FeedStatus()
}

// HealthFeedMode returns the broker market-data mode from the most recent
// feed_status frame ("realtime" | "delayed" | "" = UNKNOWN). "" never means
// realtime — the UI must label it as broker mode unknown, not realtime.
func (at *AutoTrader) HealthFeedMode() string {
	if at == nil {
		return ""
	}
	nt := at.armedTrader()
	if nt == nil {
		return ""
	}
	return nt.FeedMode()
}

// HealthLinkConnected reports the REAL NT8 TCP socket state for the health
// payload (P-D stale link latch): true only while a client is actually
// connected. A latched feed_status frame alone is NOT link evidence —
// feed_status is edge-triggered (sent only on change), so its last value can
// outlive the socket by hours. ok=false when there is no NT8 TCP trader to
// measure.
func (at *AutoTrader) HealthLinkConnected() (connected, ok bool) {
	if at == nil {
		return false, false
	}
	nt := at.armedTrader()
	if nt == nil {
		return false, false
	}
	return nt.IsConnected(), true
}

// HealthLastBarAgeMs returns the age of the newest 1m bar from the shared
// futures bars provider (the same source the kernel reads), or ok=false when
// no bar is available.
func (at *AutoTrader) HealthLastBarAgeMs(now time.Time) (ageMs int64, ok bool) {
	if at == nil || market.FuturesBarsProvider == nil {
		return 0, false
	}
	bars := market.FuturesBarsProvider(at.futuresSymbol(), "1m", 1)
	if len(bars) == 0 {
		return 0, false
	}
	return now.UnixMilli() - bars[len(bars)-1].OpenTime, true
}
