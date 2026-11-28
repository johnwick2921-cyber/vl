package ninjatrader

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"vl/kernel"
	"vl/logger"
	ntwire "vl/provider/ninjatrader"
	"vl/safe"
)

// ── ingest_loss RE-REQUEST (#55 m449, admission fixed m55r3-h1) ──────────────
//
// m446's ingest_loss check pauses entries while a FINAL or HISTORICAL bar the
// ingest queue dropped is not back in the ring. It was built with a hook
// (ntwire.DataTruthRerequest) that asks NT8 for the lost span again - and the
// integrator never assigned it, so a dropped batch stayed a pause until the ring
// aged the minute out (about 41 hours of 1m; 8.7 days of 5m) or the bot was
// restarted, with nothing asking NT8 for the bars.
//
// This wires the hook to the tracker's OWN re-request path - the same wire sends
// (RequestGapBackfillAt / RequestDeepGapBackfillAt) - under the SAME admission
// the m444 hole tracker uses, not a second mechanism and not a subset of it:
//
//   - reRequestGateReason (bar_holes.go): link up, feed up, CME open. The tracker's
//     gateLocked calls the same function.
//   - barHoleInSendWindow (bar_holes.go): a re-request makes the AddOn rebuild
//     every BarsRequest, which freezes the symbol's forming bars for 20-35 s; a
//     freeze that crosses a minute boundary stores that 1m candle truncated, as
//     LIVE. So a send goes out only in the first barHoleSendWindow of a CT
//     minute. The first m449 draft called the wire directly and fired at the
//     second the ingest queue dropped the batch (a random second, under load).
//   - the wire's own per-symbol spacing and per-session-day budget.
//
// An admission that says "not now" DEFERS the send (the loss is still owed): to
// the next send window, or - while the link, feed or CME is down - to a slow
// poll. It is never dropped silently and never sent out of turn. Drops are
// COALESCED per symbol+timeframe: one chain of attempts, whose span widens as
// more batches are lost (under queue pressure drops arrive in the hundreds).
//
// A 1m span older than NT8's standard replay is asked for with the DEEP request
// the m444 tracker uses; every other frame's span is inside the standard replay
// by construction (the ring is 2500 bars). The lost bars clear the pause
// themselves when the replay puts them in the ring (the ledger verifies
// PRESENCE, never the request).
//
// ENTRIES ONLY: nothing here touches an exit, a stop, break-even or a flatten.

const (
	// dataTruthRerequestRetryEvery / MaxRetries: a request refused as TOO SOON
	// (the spacing the tracker's own sends share) is retried, not forgotten - the
	// loss is still owed. Any other refusal (budget spent, a send error) is said
	// once and left to the tracker's schedule and the audit, which look for the
	// same minutes in the store.
	dataTruthRerequestRetryEvery = 90 * time.Second
	dataTruthRerequestMaxRetries = 3
	// dataTruthRerequestDeferEvery: while the link, feed or CME is down the
	// request waits, polling this often. dataTruthRerequestGiveUpAfter bounds
	// the wait (a weekend plus a holiday); past it the chain is dropped, said
	// once, and the pause stands as the ledger text says.
	dataTruthRerequestDeferEvery  = 2 * time.Minute
	dataTruthRerequestGiveUpAfter = 72 * time.Hour
)

// dtRerequestWire is the slice of the wire server the hook needs.
type dtRerequestWire interface {
	reRequestGateWire
	RequestGapBackfillAt(symbol string, now time.Time) error
	RequestDeepGapBackfillAt(symbol string, barsBack int, now time.Time) error
	BarsBackAsked() int
}

// untilBarHoleSendWindow is how long until the next first-barHoleSendWindow of
// a CT minute opens (0 = open now). It lands one second into the minute.
func untilBarHoleSendWindow(now time.Time) time.Duration {
	if barHoleInSendWindow(now) {
		return 0
	}
	in := now.In(kernel.CTLocation())
	into := time.Duration(in.Second())*time.Second + time.Duration(in.Nanosecond())
	return time.Minute - into + time.Second
}

// ingestLossSpentMessage is what the log says when the per-session-day NT8
// re-request budget is spent: exactly what happens, and what clears the pause.
func ingestLossSpentMessage(symbol, tf string, fromMs int64) string {
	return fmt.Sprintf("🧪 data-truth ingest_loss: the NT8 history re-request budget for %s is SPENT for this CME session day - the lost %s span from %s is NOT asked for again (the tracker's own re-requests draw on the same budget). New entries for %s stay paused until the bars are back in the ring by another replay, the minute ages out of the ring (open-market hours, not wall-clock: about 41 hours of 1m, about 208 of 5m, months for 1h and 4h), or the bot is restarted (the loss ledger lives in memory; the restart's NT8 replay and the bar gap audit's first pass re-read the store). The budget itself resets at the 17:00 CT roll, but this span is NOT re-asked for after the roll: a request is made once per drop, so only a NEW drop starts another, and a restart is the way out",
		symbol, tf, kernel.TableTimeCT(time.UnixMilli(fromMs)), symbol)
}

type dtRerequestSpan struct {
	fromMs, toMs int64
	started      time.Time
	retries      int
	// gen counts the times a later drop WIDENED the span (an older fromMs or a
	// newer toMs). A send covers the span the attempt read; a drop that widened
	// it after that read is a loss the send did not ask for, so a success does
	// not close the chain over it (m55r4-h1).
	gen int
}

// dataTruthRerequester builds the DataTruthRerequest hook over a wire. `isOpen`
// is the tracker's calendar (barHoleExpected). `spawn` runs the work off the
// ingest path (the hook "must not block"); `after` waits for a retry. Both are
// injected so the pin can drive them synchronously.
func dataTruthRerequester(w dtRerequestWire, isOpen func(time.Time) bool, clock func() time.Time, spawn func(func()), after func(time.Duration, func())) func(symbol, tf string, fromMs, toMs int64) {
	var mu sync.Mutex
	said := map[string]time.Time{} // symbol+reason -> last time it was said
	pending := map[string]*dtRerequestSpan{}
	sayOnce := func(key string, f func()) {
		mu.Lock()
		defer mu.Unlock()
		if last, ok := said[key]; ok && clock().Sub(last) < 5*time.Minute {
			return
		}
		said[key] = clock()
		f()
	}
	// chain runs one symbol+timeframe's attempts until the loss is asked for,
	// refused for good, or given up on.
	chain := func(symbol, tf string) {
		key := symbol + "|" + tf
		finish := func() {
			mu.Lock()
			delete(pending, key)
			mu.Unlock()
		}
		var try func()
		try = func() {
			mu.Lock()
			sp := pending[key]
			if sp == nil {
				mu.Unlock()
				return
			}
			fromMs, started, retries, gen := sp.fromMs, sp.started, sp.retries, sp.gen
			mu.Unlock()
			now := clock()
			if now.Sub(started) > dataTruthRerequestGiveUpAfter {
				sayOnce(key+"|giveup", func() {
					logger.Warnf("🧪 data-truth ingest_loss: gave up asking NT8 for the lost %s %s span from %s after %s of waiting for the link, feed and market to allow it - entries stay paused until the bars are back in the ring, the minute ages out of the ring, or the bot is restarted",
						symbol, tf, kernel.TableTimeCT(time.UnixMilli(fromMs)), dataTruthRerequestGiveUpAfter)
				})
				finish()
				return
			}
			// the tracker's admission, part 1: link, feed, CME open
			if reason := reRequestGateReason(w, isOpen, now); reason != "" {
				sayOnce(key+"|deferred|"+reason, func() {
					logger.Warnf("🧪 data-truth ingest_loss: the NT8 history re-request for %s (%s lost span from %s) is DEFERRED: %s - it goes out once that clears; entries stay paused meanwhile",
						symbol, tf, kernel.TableTimeCT(time.UnixMilli(fromMs)), reason)
				})
				after(dataTruthRerequestDeferEvery, try)
				return
			}
			// part 2: the send window (a rebuild that crosses a minute boundary
			// stores a truncated 1m candle as live)
			if wait := untilBarHoleSendWindow(now); wait > 0 {
				after(wait, try)
				return
			}
			var send func(now time.Time) error
			how := "standard (the AddOn's bars_back)"
			if tf == barHoleTF {
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
						logger.Warnf("🧪 data-truth ingest_loss: the lost %s %s span from %s is older than the deepest NT8 re-request (%d 1m bars) - NOT re-requested; it clears when the ring or the store holds the bars, when it ages out of the ring, or when the bot is restarted",
							symbol, tf, kernel.TableTimeCT(time.UnixMilli(fromMs)), ntwire.DeepGapBackfillMaxBarsBack)
					})
					finish()
					return
				}
			}
			if send == nil {
				send = func(n time.Time) error { return w.RequestGapBackfillAt(symbol, n) }
			}
			err := send(now)
			switch {
			case err == nil:
				logger.Infof("🧪 data-truth ingest_loss: re-requested NT8 history for %s (%s lost span from %s) - %s; the pause lifts when the bars are present in the ring",
					symbol, tf, kernel.TableTimeCT(time.UnixMilli(fromMs)), how)
				mu.Lock()
				sp := pending[key]
				widened := sp != nil && sp.gen != gen
				if widened {
					// more bars were dropped while this attempt was in flight:
					// ask again for the wider span (the wire's own spacing
					// applies, hence the retry delay), with a fresh retry count
					sp.retries = 0
				}
				mu.Unlock()
				if widened {
					after(dataTruthRerequestRetryEvery, try)
					return
				}
				finish()
			case errors.Is(err, ntwire.ErrGapBackfillTooSoon) && retries < dataTruthRerequestMaxRetries:
				mu.Lock()
				if sp := pending[key]; sp != nil {
					sp.retries++
				}
				mu.Unlock()
				after(dataTruthRerequestRetryEvery, try)
			case errors.Is(err, ntwire.ErrGapBackfillSpent):
				sayOnce(symbol+"|spent", func() {
					logger.Warnf("%s", ingestLossSpentMessage(symbol, tf, fromMs))
				})
				finish()
			default:
				sayOnce(symbol+"|"+err.Error(), func() {
					logger.Warnf("🧪 data-truth ingest_loss: the NT8 history re-request for %s (%s lost span from %s) was REFUSED: %v - the m444 hole tracker and the gap audit look for the same minutes in the store; entries stay paused until the bars are back in the ring, the minute ages out of the ring, or the bot is restarted",
						symbol, tf, kernel.TableTimeCT(time.UnixMilli(fromMs)), err)
				})
				finish()
			}
		}
		spawn(try)
	}
	return func(symbol, tf string, fromMs, toMs int64) {
		if w == nil {
			return
		}
		key := symbol + "|" + tf
		mu.Lock()
		if sp := pending[key]; sp != nil {
			// a chain is already working on this frame: widen its span (the
			// deep request is sized from the oldest minute lost)
			if fromMs < sp.fromMs {
				sp.fromMs = fromMs
				sp.gen++
			}
			if toMs > sp.toMs {
				sp.toMs = toMs
				sp.gen++
			}
			mu.Unlock()
			return
		}
		pending[key] = &dtRerequestSpan{fromMs: fromMs, toMs: toMs, started: clock()}
		mu.Unlock()
		chain(symbol, tf)
	}
}

// wireDataTruthRerequest installs the hook on the production wire server.
func wireDataTruthRerequest(server *ntwire.TCPServer) {
	if server == nil {
		return
	}
	ntwire.SetDataTruthRerequest(dataTruthRerequester(server, barHoleExpected, time.Now,
		func(f func()) { safe.GoNet("nt8-datatruth-rerequest", "", f) },
		func(d time.Duration, f func()) {
			time.AfterFunc(d, func() { safe.GoNet("nt8-datatruth-rerequest-retry", "", f) })
		}))
}
