package trader

import (
	"fmt"
	"strings"
	"sync"
	"time"

	ntwire "vl/provider/ninjatrader"
)

// m442 #19 - a rejected exit used to be ONE log line and a counter
// (close_sync.go: "NT close REJECTED ... STILL OPEN in NT8"). The position is
// still open, nobody reads a log line, and the next decision cycle re-issues the
// close and gets rejected again. It is now an ops P0 alert (alert center + the
// owner's phone through the existing m432 push), ONE per episode.

var (
	closeRejectMu       sync.Mutex
	closeRejectEpisodes = map[string]time.Time{} // trader|symbol|side -> episode start
	closeRejectLast     = map[string]time.Time{}
)

// closeRejectEpisodeStart returns the start (UnixMilli) of the rejection episode
// this frame belongs to: a new episode starts when none ran in the last
// closeRejectEpisodeGap.
func closeRejectEpisodeStart(traderID, symbol, side string, now time.Time) int64 {
	k := traderID + "|" + strings.ToUpper(symbol) + "|" + strings.ToLower(side)
	closeRejectMu.Lock()
	defer closeRejectMu.Unlock()
	start, ok := closeRejectEpisodes[k]
	if !ok || now.Sub(closeRejectLast[k]) > closeRejectEpisodeGap {
		start = now
		closeRejectEpisodes[k] = start
	}
	closeRejectLast[k] = now
	return start.UnixMilli()
}

// dispatchCloseRejected is ntTrader.OnCloseRejected: it routes the frame to the
// owning running trader. A trader that is not running has no alert surface.
func dispatchCloseRejected(traderID string, r ntwire.PositionCloseRejectedPayload) {
	if at, ok := runningTrader(traderID); ok {
		at.alertCloseRejected(r, time.Now())
	}
}

// alertCloseRejected raises the ops P0 for one rejected exit. The event id is the
// EPISODE (symbol + side + episode start), never the frame time or signal: the
// retry loop must not mint a P0 and a push per attempt.
func (at *AutoTrader) alertCloseRejected(r ntwire.PositionCloseRejectedPayload, now time.Time) {
	start := closeRejectEpisodeStart(at.id, r.Symbol, r.PositionSide, now)
	reason := strings.TrimSpace(r.Reason)
	if reason == "" {
		reason = "no reason given"
	}
	at.emitAlert("P0", alertKindCloseRejected,
		fmt.Sprintf("close-rejected:%s:%s:%d", strings.ToUpper(r.Symbol), strings.ToLower(r.PositionSide), start),
		fmt.Sprintf("NT8 REFUSED to close the %s %s position", r.Symbol, strings.ToLower(r.PositionSide)),
		fmt.Sprintf("The exit was rejected (%s, account %s). The position is STILL OPEN in NT8 - the bot retries on its next cycle; check NinjaTrader and flatten by hand if it keeps failing.", reason, r.Account))
}
