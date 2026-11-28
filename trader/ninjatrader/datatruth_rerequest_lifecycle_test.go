package ninjatrader

import (
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
)

// ── #55 m55r4-h1 - the LIFECYCLE of an ingest_loss re-request chain ──────────
//
// The round-3 pins covered the admission (window, link/feed/CME gate,
// coalescing) and ONE chain exit (a spent budget frees the chain). Every other
// exit - a successful send, a refusal, "older than the deepest ask", the 72 h
// give-up, the TOO-SOON retry cap - was unpinned: a chain that is never closed
// keeps pending["MNQ|1m"] forever, every later drop only widens the dead entry,
// and the pause stands until the minute leaves the ring (about 41 h) or a
// restart - the original m449 defect again. MUTANTS (shown RED in the report):
//   L1  no finish() after a successful send              -> ...FreedAfterSend RED
//   L2  no finish() on the "beyond the deepest" branch    -> ...FreedAfterBeyond RED
//   L3  no finish() on the give-up branch                 -> ...FreedAfterGiveUp RED
//   L4  no finish() on the default refused branch         -> ...FreedAfterRefusal RED
//   L5  sp.retries never incremented                      -> ...TooSoonRetryCap RED
//   L6  success closes the chain over a span widened mid-flight (gen ignored) -> ...WidenedWhileSending RED
//   L7  no sp.gen++ on the newer-toMs arm of the widening branch -> ...WidenedToMsOnly RED
//   L8  no sp.gen++ on the older-fromMs arm of the widening branch -> ...WidenedFromMsOnly RED
//   L9  no sp.retries = 0 on the widened branch after a send -> ...WidenedChainGetsFreshRetries RED

// secondDropSends drops a second, recent loss and reports whether a NEW send went out.
func secondDropSends(h *rqHarness, w *fakeRerequestWire, hook func(string, string, int64, int64)) bool {
	before := len(w.std) + len(w.deep)
	hook("MNQ", "1m", h.now.Add(-3*time.Minute).UnixMilli(), h.now.Add(-2*time.Minute).UnixMilli())
	return len(w.std)+len(w.deep) > before
}

func TestIngestLossChainIsFreedAfterASuccessfulSend(t *testing.T) {
	h := &rqHarness{now: ctAt(10, 0, 5)}
	w := &fakeRerequestWire{bb: 2000}
	hook := dataTruthRerequester(w, rqOpen, h.clock, syncSpawn, h.after)
	hook("MNQ", "1m", h.now.Add(-10*time.Minute).UnixMilli(), h.now.Add(-9*time.Minute).UnixMilli())
	if len(w.std) != 1 || len(h.queued) != 0 {
		t.Fatalf("precondition: one send, nothing queued: sent=%d queued=%d", len(w.std), len(h.queued))
	}
	if !secondDropSends(h, w, hook) {
		t.Fatalf("after a successful send the chain must be closed: a later drop starts a NEW request (sent=%d)", len(w.std))
	}
}

func TestIngestLossChainIsFreedAfterBeyondTheDeepestAsk(t *testing.T) {
	h := &rqHarness{now: ctAt(10, 0, 5)}
	w := &fakeRerequestWire{bb: 2000}
	hook := dataTruthRerequester(w, rqOpen, h.clock, syncSpawn, h.after)
	hook("MNQ", "1m", h.now.Add(-30*24*time.Hour).UnixMilli(), h.now.Add(-29*24*time.Hour).UnixMilli())
	if len(w.std)+len(w.deep) != 0 || len(h.queued) != 0 {
		t.Fatalf("precondition: beyond the deepest ask sends nothing: %v %v queued=%d", w.std, w.deep, len(h.queued))
	}
	if !secondDropSends(h, w, hook) {
		t.Fatal("a span beyond the deepest ask must not wedge the symbol: a later recent drop is asked for")
	}
}

func TestIngestLossChainIsFreedAfterTheGiveUp(t *testing.T) {
	h := &rqHarness{now: ctAt(16, 30, 0)} // CME break: the chain defers
	w := &fakeRerequestWire{bb: 2000}
	hook := dataTruthRerequester(w, rqOpen, h.clock, syncSpawn, h.after)
	hook("MNQ", "1m", h.now.Add(-10*time.Minute).UnixMilli(), h.now.Add(-9*time.Minute).UnixMilli())
	if len(h.queued) != 1 || len(w.std) != 0 {
		t.Fatalf("precondition: a deferred chain: queued=%d sent=%d", len(h.queued), len(w.std))
	}
	// 73 h later the chain gives up; the market is open again by then
	h.now = h.now.Add(dataTruthRerequestGiveUpAfter + time.Hour)
	for h.now.Weekday() == time.Saturday || h.now.Weekday() == time.Sunday || !rqOpen(h.now) {
		h.now = h.now.Add(time.Hour)
	}
	f := h.queued[0]
	h.queued = nil
	f()
	if len(w.std) != 0 || len(h.queued) != 0 {
		t.Fatalf("past the give-up bound nothing is sent and nothing is queued: sent=%d queued=%d", len(w.std), len(h.queued))
	}
	h.now = ctAt(10, 0, 5).Add(4 * 24 * time.Hour)
	for !rqOpen(h.now) || !barHoleInSendWindow(h.now) {
		h.now = h.now.Add(time.Second)
	}
	if !secondDropSends(h, w, hook) {
		t.Fatal("a given-up chain must be closed: a later drop starts a NEW request")
	}
}

func TestIngestLossChainIsFreedAfterARefusal(t *testing.T) {
	h := &rqHarness{now: ctAt(10, 0, 5)}
	w := &fakeRerequestWire{bb: 2000, err: ntwire.ErrGapBackfillFeedDown}
	hook := dataTruthRerequester(w, rqOpen, h.clock, syncSpawn, h.after)
	hook("MNQ", "1m", h.now.Add(-10*time.Minute).UnixMilli(), h.now.Add(-9*time.Minute).UnixMilli())
	if len(w.std) != 1 || len(h.queued) != 0 {
		t.Fatalf("precondition: a hard refusal is said once, not retried: sent=%d queued=%d", len(w.std), len(h.queued))
	}
	w.err = nil
	if !secondDropSends(h, w, hook) {
		t.Fatal("a refused chain must be closed: a later drop starts a NEW request")
	}
}

func TestIngestLossTooSoonRetryCap(t *testing.T) {
	h := &rqHarness{now: ctAt(10, 0, 5)}
	w := &fakeRerequestWire{bb: 2000, err: ntwire.ErrGapBackfillTooSoon}
	hook := dataTruthRerequester(w, rqOpen, h.clock, syncSpawn, h.after)
	hook("MNQ", "1m", h.now.Add(-10*time.Minute).UnixMilli(), h.now.Add(-9*time.Minute).UnixMilli())
	for i := 0; i < 50 && len(h.queued) > 0; i++ {
		h.fire(t)
	}
	if len(h.queued) != 0 {
		t.Fatal("a TOO-SOON refusal must stop being retried")
	}
	if want := 1 + dataTruthRerequestMaxRetries; len(w.std) != want {
		t.Fatalf("a too-soon refusal is sent once and retried exactly %d times: sent=%d, want %d", dataTruthRerequestMaxRetries, len(w.std), want)
	}
	// and the exhausted chain is closed, not wedged
	w.err = nil
	h.now = ctAt(10, 30, 5)
	if !secondDropSends(h, w, hook) {
		t.Fatal("an exhausted retry chain must be closed: a later drop starts a NEW request")
	}
}

// widenWire drops a wider loss into the hook WHILE the standard send is in flight.
type widenWire struct {
	*fakeRerequestWire
	onSend func()
}

func (w *widenWire) RequestGapBackfillAt(sym string, now time.Time) error {
	if f := w.onSend; f != nil {
		w.onSend = nil
		f()
	}
	return w.fakeRerequestWire.RequestGapBackfillAt(sym, now)
}

func TestIngestLossSpanWidenedWhileSendingIsAskedForAgain(t *testing.T) {
	h := &rqHarness{now: ctAt(10, 0, 5)}
	fw := &fakeRerequestWire{bb: 2000}
	w := &widenWire{fakeRerequestWire: fw}
	hook := dataTruthRerequester(w, rqOpen, h.clock, syncSpawn, h.after)
	w.onSend = func() { // a later batch is dropped after the attempt read its span
		hook("MNQ", "1m", h.now.Add(-20*time.Minute).UnixMilli(), h.now.Add(-1*time.Minute).UnixMilli())
	}
	hook("MNQ", "1m", h.now.Add(-10*time.Minute).UnixMilli(), h.now.Add(-9*time.Minute).UnixMilli())
	if len(fw.std) != 1 {
		t.Fatalf("precondition: the first send went out: %d", len(fw.std))
	}
	if len(h.queued) != 1 {
		t.Fatalf("a success must not close the chain over a span widened mid-flight: queued=%d", len(h.queued))
	}
	h.fire(t)
	for i := 0; i < 5 && len(fw.std) < 2 && len(h.queued) > 0; i++ {
		h.fire(t)
	}
	if len(fw.std) != 2 {
		t.Fatalf("the widened span is asked for again: sent=%d", len(fw.std))
	}
	if len(h.queued) != 0 {
		t.Fatalf("and then the chain is closed: queued=%d", len(h.queued))
	}
}

// widenedOnlyAsksAgain: the first drop is (-10m, -9m); WHILE its send is in
// flight a second drop widens ONE edge only. The widened span must be asked for
// again - a success that closes the chain over it strands those bars (m55r4-h1
// review: the both-edges test above cannot tell the two arms apart).
func widenedOnlyAsksAgain(t *testing.T, widenFromMin, widenToMin int) {
	t.Helper()
	h := &rqHarness{now: ctAt(10, 0, 5)}
	fw := &fakeRerequestWire{bb: 2000}
	w := &widenWire{fakeRerequestWire: fw}
	hook := dataTruthRerequester(w, rqOpen, h.clock, syncSpawn, h.after)
	w.onSend = func() {
		hook("MNQ", "1m", h.now.Add(-time.Duration(widenFromMin)*time.Minute).UnixMilli(),
			h.now.Add(-time.Duration(widenToMin)*time.Minute).UnixMilli())
	}
	hook("MNQ", "1m", h.now.Add(-10*time.Minute).UnixMilli(), h.now.Add(-9*time.Minute).UnixMilli())
	if len(fw.std) != 1 {
		t.Fatalf("precondition: the first send went out: %d", len(fw.std))
	}
	if len(h.queued) != 1 {
		t.Fatalf("a span widened mid-flight (one edge) must not be closed over by the success: queued=%d", len(h.queued))
	}
	for i := 0; i < 5 && len(fw.std) < 2 && len(h.queued) > 0; i++ {
		h.fire(t)
	}
	if len(fw.std) != 2 {
		t.Fatalf("the widened span is asked for again: sent=%d", len(fw.std))
	}
}

func TestIngestLossSpanWidenedFromMsOnlyIsAskedForAgain(t *testing.T) {
	widenedOnlyAsksAgain(t, 20, 9) // older fromMs, same toMs
}

func TestIngestLossSpanWidenedToMsOnlyIsAskedForAgain(t *testing.T) {
	widenedOnlyAsksAgain(t, 10, 1) // same fromMs, newer toMs
}

// scriptWire answers each standard send from a script and can widen the span
// while a chosen send is in flight.
type scriptWire struct {
	*fakeRerequestWire
	n      int
	script func(n int) error
	onSend func(n int)
}

func (w *scriptWire) RequestGapBackfillAt(sym string, now time.Time) error {
	w.n++
	if w.onSend != nil {
		w.onSend(w.n)
	}
	w.fakeRerequestWire.std = append(w.fakeRerequestWire.std, sym)
	return w.script(w.n)
}

// A chain that already used up retries, then succeeds while a wider drop lands,
// starts the widened chain with the FULL retry count - not what was left.
func TestIngestLossWidenedChainGetsFreshRetries(t *testing.T) {
	h := &rqHarness{now: ctAt(10, 0, 5)}
	w := &scriptWire{fakeRerequestWire: &fakeRerequestWire{bb: 2000}}
	var hook func(string, string, int64, int64)
	w.script = func(n int) error {
		if n == 3 {
			return nil // success; the widening below landed during this send
		}
		return ntwire.ErrGapBackfillTooSoon
	}
	w.onSend = func(n int) {
		if n == 3 { // sends 1 and 2 were TOO SOON: retries == 2 when send 3 goes out
			hook("MNQ", "1m", h.now.Add(-20*time.Minute).UnixMilli(), h.now.Add(-9*time.Minute).UnixMilli())
		}
	}
	hook = dataTruthRerequester(w, rqOpen, h.clock, syncSpawn, h.after)
	hook("MNQ", "1m", h.now.Add(-10*time.Minute).UnixMilli(), h.now.Add(-9*time.Minute).UnixMilli())
	for i := 0; i < 60 && len(h.queued) > 0; i++ {
		h.fire(t)
	}
	if len(h.queued) != 0 {
		t.Fatal("the chain must end (the retry cap holds)")
	}
	// sends 1-3 above; the widened chain then sends once and retries exactly MaxRetries times
	if want := 3 + 1 + dataTruthRerequestMaxRetries; w.n != want {
		t.Fatalf("a widened chain gets a fresh retry count: sends=%d, want %d", w.n, want)
	}
}
