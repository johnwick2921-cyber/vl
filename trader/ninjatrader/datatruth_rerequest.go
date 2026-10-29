package ninjatrader

import (
	"errors"
	"sync"
	"time"

	"vl/kernel"
	"vl/logger"
	ntwire "vl/provider/ninjatrader"
	"vl/safe"
)

// ── ingest_loss RE-REQUEST (#55 m449) ────────────────────────────────────────
//
// m446's ingest_loss check pauses entries while a FINAL or HISTORICAL bar the
// ingest queue dropped is not back in the ring. It was built with a hook
// (ntwire.DataTruthRerequest) that asks NT8 for the lost span again - and the
// integrator never assigned it, so a dropped batch stayed a pause until the ring
// aged the minute out (about 41 hours of 1m; 8.7 days of 5m) or the bot was
// restarted, with nothing asking NT8 for the bars.
//
// This wires the hook to the tracker's OWN re-request path - the same wire sends
// (RequestGapBackfillAt / RequestDeepGapBackfillAt), under the same admission
// rule (link + feed up, per-symbol spacing, the per-session-day budget) - not a
// second mechanism. A 1m span older than NT8's standard replay is asked for with
// the DEEP request the m444 tracker uses; every other frame's span is inside the
// standard replay by construction (the ring is 2500 bars). The lost bars clear
// the pause themselves when the replay puts them in the ring (the ledger verifies
// PRESENCE, never the request).

const (
	// dataTruthRerequestRetryEvery / MaxRetries: a request refused as TOO SOON
	// (the spacing the tracker's own sends share) is retried, not forgotten - the
	// loss is still owed. Any other refusal (link down, feed down, budget spent)
	// is said once and left to the tracker's schedule and the audit, which look
	// for the same minutes in the store.
	dataTruthRerequestRetryEvery = 90 * time.Second
	dataTruthRerequestMaxRetries = 3
)

// dtRerequestWire is the slice of the wire server the hook needs.
type dtRerequestWire interface {
	RequestGapBackfillAt(symbol string, now time.Time) error
	RequestDeepGapBackfillAt(symbol string, barsBack int, now time.Time) error
	BarsBackAsked() int
}

// dataTruthRerequester builds the DataTruthRerequest hook over a wire. `spawn`
// runs the work off the ingest path (the hook "must not block"); `after` waits
// for a retry. Both are injected so the pin can drive them synchronously.
func dataTruthRerequester(w dtRerequestWire, clock func() time.Time, spawn func(func()), after func(time.Duration, func())) func(symbol, tf string, fromMs, toMs int64) {
	var mu sync.Mutex
	said := map[string]time.Time{} // symbol+reason -> last time it was said
	sayOnce := func(key string, f func()) {
		mu.Lock()
		defer mu.Unlock()
		if last, ok := said[key]; ok && clock().Sub(last) < 5*time.Minute {
			return
		}
		said[key] = clock()
		f()
	}
	return func(symbol, tf string, fromMs, toMs int64) {
		if w == nil {
			return
		}
		spawn(func() {
			var send func(now time.Time) error
			how := "standard (the AddOn's bars_back)"
			if tf == barHoleTF {
				now := clock()
				bb := w.BarsBackAsked()
				open := 0
				for m := fromMs; m < now.UnixMilli() && open <= ntwire.DeepGapBackfillMaxBarsBack; m += 60_000 {
					if barHoleExpected(time.UnixMilli(m)) {
						open++
					}
				}
				need := open + barHoleDeepMargin
				switch {
				case bb > 0 && need > bb && need <= ntwire.DeepGapBackfillMaxBarsBack:
					how = "DEEP 1m (older than NT8's standard replay)"
					send = func(n time.Time) error { return w.RequestDeepGapBackfillAt(symbol, need, n) }
				case bb > 0 && need > ntwire.DeepGapBackfillMaxBarsBack:
					sayOnce(symbol+"|beyond", func() {
						logger.Warnf("🧪 data-truth ingest_loss: the lost %s %s span from %s is older than the deepest NT8 re-request (%d 1m bars) - NOT re-requested; it clears when the ring or the store holds the bars, or when it ages out of the ring",
							symbol, tf, kernel.TableTimeCT(time.UnixMilli(fromMs)), ntwire.DeepGapBackfillMaxBarsBack)
					})
					return
				}
			}
			if send == nil {
				send = func(n time.Time) error { return w.RequestGapBackfillAt(symbol, n) }
			}
			var try func(attempt int)
			try = func(attempt int) {
				err := send(clock())
				switch {
				case err == nil:
					logger.Infof("🧪 data-truth ingest_loss: re-requested NT8 history for %s (%s lost span from %s) - %s; the pause lifts when the bars are present in the ring",
						symbol, tf, kernel.TableTimeCT(time.UnixMilli(fromMs)), how)
				case errors.Is(err, ntwire.ErrGapBackfillTooSoon) && attempt < dataTruthRerequestMaxRetries:
					after(dataTruthRerequestRetryEvery, func() { try(attempt + 1) })
				default:
					sayOnce(symbol+"|"+err.Error(), func() {
						logger.Warnf("🧪 data-truth ingest_loss: the NT8 history re-request for %s (%s lost span from %s) was REFUSED: %v - the m444 hole tracker and the gap audit look for the same minutes in the store; entries stay paused until the bars are back in the ring",
							symbol, tf, kernel.TableTimeCT(time.UnixMilli(fromMs)), err)
					})
				}
			}
			try(1)
		})
	}
}

// wireDataTruthRerequest installs the hook on the production wire server.
func wireDataTruthRerequest(server *ntwire.TCPServer) {
	if server == nil {
		return
	}
	ntwire.SetDataTruthRerequest(dataTruthRerequester(server, time.Now,
		func(f func()) { safe.GoNet("nt8-datatruth-rerequest", "", f) },
		func(d time.Duration, f func()) {
			time.AfterFunc(d, func() { safe.GoNet("nt8-datatruth-rerequest-retry", "", f) })
		}))
}
