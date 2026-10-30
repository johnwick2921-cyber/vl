package ninjatrader

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
)

// ── #55 m55r3-h1 - the ingest_loss re-request obeys the m444 tracker's admission ─
//
// The first m449 hook called the wire the instant the ingest queue dropped a
// batch. The tracker never does: a re-request rebuilds the AddOn's BarsRequests
// (a 20-35 s freeze of the forming bars) and a freeze that crosses a minute
// boundary stores that 1m candle truncated, as LIVE - so it sends only in the
// first barHoleSendWindow of a CT minute, never while the link/feed is down or
// CME is closed. MUTANTS (shown RED in the report):
//   A1  drop the send-window check in the hook        -> TestIngestLossHookWaitsForTheSendWindow RED
//   A2  drop the reRequestGateReason call in the hook -> TestIngestLossHookDefersWhileLinkFeedOrMarketIsDown RED
//   A3  no coalescing (every drop its own chain)      -> TestIngestLossHookCoalescesDropsIntoOneChain RED
//   A4  gateLocked stops using the shared gate        -> TestTrackerGateAndHookGateAgree RED
//   A5  the spent branch stops using ingestLossSpentMessage -> TestRerequestAdmissionIsOneFunction RED

// a clock the test moves; `after` records the wait and runs it when told to.
type rqHarness struct {
	now    time.Time
	waits  []time.Duration
	queued []func()
}

func (h *rqHarness) clock() time.Time { return h.now }
func (h *rqHarness) after(d time.Duration, f func()) {
	h.waits = append(h.waits, d)
	h.queued = append(h.queued, f)
}

// fire advances the clock by the oldest queued wait and runs that callback.
func (h *rqHarness) fire(t *testing.T) {
	t.Helper()
	if len(h.queued) == 0 {
		t.Fatal("nothing queued to fire")
	}
	d, f := h.waits[0], h.queued[0]
	h.waits, h.queued = h.waits[1:], h.queued[1:]
	h.now = h.now.Add(d)
	f()
}

func ctAt(h, m, s int) time.Time {
	return time.Date(2026, 10, 29, h, m, s, 0, kernel.CTLocation()) // a Thursday, CME open
}

func TestIngestLossHookWaitsForTheSendWindow(t *testing.T) {
	h := &rqHarness{now: ctAt(10, 0, 40)} // :40 - OUTSIDE the first 15 s
	w := &fakeRerequestWire{bb: 2000}
	hook := dataTruthRerequester(w, rqOpen, h.clock, syncSpawn, h.after)
	hook("MNQ", "1m", h.now.Add(-10*time.Minute).UnixMilli(), h.now.Add(-9*time.Minute).UnixMilli())
	if len(w.std)+len(w.deep) != 0 {
		t.Fatalf("a drop at :40 must NOT send (a rebuild crossing the minute stores a truncated candle): std=%v deep=%v", w.std, w.deep)
	}
	if len(h.queued) != 1 {
		t.Fatalf("the request is DEFERRED to the next window, not dropped: queued=%d", len(h.queued))
	}
	if h.waits[0] <= 0 || h.waits[0] > time.Minute {
		t.Fatalf("the wait lands in the next minute's window, got %s", h.waits[0])
	}
	h.fire(t)
	if got := h.now.In(kernel.CTLocation()); got.Second() >= int(barHoleSendWindow/time.Second) {
		t.Fatalf("the deferred send fired at :%02d, outside the window", got.Second())
	}
	if len(w.std) != 1 {
		t.Fatalf("the deferred request goes out in the window: std=%v", w.std)
	}
}

func TestIngestLossHookSendsInsideTheWindow(t *testing.T) {
	h := &rqHarness{now: ctAt(10, 0, 5)}
	w := &fakeRerequestWire{bb: 2000}
	hook := dataTruthRerequester(w, rqOpen, h.clock, syncSpawn, h.after)
	hook("MNQ", "1m", h.now.Add(-10*time.Minute).UnixMilli(), h.now.Add(-9*time.Minute).UnixMilli())
	if len(w.std) != 1 || len(h.queued) != 0 {
		t.Fatalf("a drop at :05 sends at once: std=%v queued=%d", w.std, len(h.queued))
	}
}

func TestIngestLossHookDefersWhileLinkFeedOrMarketIsDown(t *testing.T) {
	cases := []struct {
		name  string
		setup func(w *fakeRerequestWire, h *rqHarness)
		heal  func(w *fakeRerequestWire, h *rqHarness)
	}{
		{"link down", func(w *fakeRerequestWire, _ *rqHarness) { w.linkDown = true }, func(w *fakeRerequestWire, _ *rqHarness) { w.linkDown = false }},
		{"feed down", func(w *fakeRerequestWire, _ *rqHarness) { w.feedDown = true }, func(w *fakeRerequestWire, _ *rqHarness) { w.feedDown = false }},
		// 16:30 CT is the daily break: CME is closed
		{"CME closed", func(_ *fakeRerequestWire, h *rqHarness) { h.now = ctAt(16, 30, 5) }, func(_ *fakeRerequestWire, h *rqHarness) { h.now = ctAt(17, 5, 5) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := &rqHarness{now: ctAt(10, 0, 5)}
			w := &fakeRerequestWire{bb: 2000}
			c.setup(w, h)
			hook := dataTruthRerequester(w, rqOpen, h.clock, syncSpawn, h.after)
			hook("MNQ", "1m", h.now.Add(-10*time.Minute).UnixMilli(), h.now.Add(-9*time.Minute).UnixMilli())
			if len(w.std)+len(w.deep) != 0 {
				t.Fatalf("%s: nothing may be sent: std=%v deep=%v", c.name, w.std, w.deep)
			}
			if len(h.queued) != 1 || h.waits[0] != dataTruthRerequestDeferEvery {
				t.Fatalf("%s: the request is deferred (polled), not dropped: waits=%v", c.name, h.waits)
			}
			c.heal(w, h)
			h.queued[0]()
			if len(w.std) != 1 {
				t.Fatalf("%s: once it clears the request goes out: std=%v", c.name, w.std)
			}
		})
	}
}

func TestIngestLossHookCoalescesDropsIntoOneChain(t *testing.T) {
	h := &rqHarness{now: ctAt(10, 0, 40)} // out of window so the chain stays pending
	w := &fakeRerequestWire{bb: 2000}
	hook := dataTruthRerequester(w, rqOpen, h.clock, syncSpawn, h.after)
	for i := 0; i < 50; i++ {
		hook("MNQ", "1m", h.now.Add(-10*time.Minute).UnixMilli(), h.now.Add(-9*time.Minute).UnixMilli())
	}
	if len(h.queued) != 1 {
		t.Fatalf("50 drops on one symbol+frame are ONE chain (under queue pressure drops arrive in the hundreds): queued=%d", len(h.queued))
	}
	// an older loss widens the same chain: the deep ask is sized from the oldest minute
	hook("MNQ", "1m", h.now.Add(-60*time.Hour).UnixMilli(), h.now.Add(-59*time.Hour).UnixMilli())
	if len(h.queued) != 1 {
		t.Fatalf("a wider span joins the chain: queued=%d", len(h.queued))
	}
	h.fire(t)
	if len(w.deep) != 1 || w.deepBack <= 2000 {
		t.Fatalf("the chain sends one DEEP ask sized from the oldest lost minute: deep=%v back=%d", w.deep, w.deepBack)
	}
	// a symbol of its own is its own chain (the clock now sits inside the window, so it sends)
	hook("ES", "1m", h.now.Add(-5*time.Minute).UnixMilli(), h.now.Add(-4*time.Minute).UnixMilli())
	if len(w.std) != 1 || w.std[0] != "ES" {
		t.Fatalf("ES opens its own chain and is asked for: std=%v", w.std)
	}
}

func TestIngestLossHookSpentBudgetIsNotRetriedAndFreesTheChain(t *testing.T) {
	h := &rqHarness{now: ctAt(10, 0, 5)}
	w := &fakeRerequestWire{bb: 2000, err: ntwire.ErrGapBackfillSpent}
	hook := dataTruthRerequester(w, rqOpen, h.clock, syncSpawn, h.after)
	hook("MNQ", "1m", h.now.Add(-10*time.Minute).UnixMilli(), h.now.Add(-9*time.Minute).UnixMilli())
	if len(w.std) != 1 || len(h.queued) != 0 {
		t.Fatalf("a spent budget is said once and not retried in a loop: sent=%d queued=%d", len(w.std), len(h.queued))
	}
	// the chain is freed: a later drop (after the 17:00 roll, say) asks again
	w.err = nil
	hook("MNQ", "1m", h.now.Add(-8*time.Minute).UnixMilli(), h.now.Add(-7*time.Minute).UnixMilli())
	if len(w.std) != 2 {
		t.Fatalf("a spent budget must not wedge the symbol: sent=%d", len(w.std))
	}
}

// A4 - the tracker and the hook share ONE gate function, and the hook is
// installed with the tracker's calendar. Read through go/parser (a comment is
// not an AST node).
func TestRerequestAdmissionIsOneFunction(t *testing.T) {
	calls := func(file, fn string) map[string]bool {
		fs := token.NewFileSet()
		f, err := parser.ParseFile(fs, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name != fn || fd.Body == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					switch f := c.Fun.(type) {
					case *ast.Ident:
						out[f.Name] = true
					case *ast.SelectorExpr:
						out[f.Sel.Name] = true
					}
				}
				if id, ok := n.(*ast.Ident); ok {
					out["ident:"+id.Name] = true
				}
				return true
			})
		}
		return out
	}
	if !calls("bar_holes.go", "gateLocked")["reRequestGateReason"] {
		t.Fatal("the m444 tracker's gateLocked must call reRequestGateReason")
	}
	hook := calls("datatruth_rerequest.go", "dataTruthRerequester")
	if !hook["reRequestGateReason"] || !hook["untilBarHoleSendWindow"] {
		t.Fatalf("the ingest_loss hook must use the tracker's gate and send window: %v", hook)
	}
	if !hook["ingestLossSpentMessage"] {
		t.Fatal("the spent-budget branch must say it through ingestLossSpentMessage")
	}
	if !calls("datatruth_rerequest.go", "untilBarHoleSendWindow")["barHoleInSendWindow"] {
		t.Fatal("untilBarHoleSendWindow must be built on barHoleInSendWindow")
	}
	if !calls("datatruth_rerequest.go", "wireDataTruthRerequest")["ident:barHoleExpected"] {
		t.Fatal("the production hook must be wired with the tracker's calendar (barHoleExpected)")
	}
	// the hook never touches a trader's exit path
	src, err := os.ReadFile("datatruth_rerequest.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"CloseLong", "CloseShort", "SetStop", "Flatten", "CancelOrder"} {
		if strings.Contains(string(src), bad) {
			t.Fatalf("the re-request hook must not reference %s", bad)
		}
	}
}

// the gate itself: every reason the tracker defers on is a reason the hook defers on.
func TestRerequestGateReasons(t *testing.T) {
	open := ctAt(10, 0, 0)
	w := &fakeRerequestWire{}
	if r := reRequestGateReason(w, rqOpen, open); r != "" {
		t.Fatalf("healthy wire, open market: %q", r)
	}
	if r := reRequestGateReason(nil, rqOpen, open); r == "" {
		t.Fatal("no wire must refuse")
	}
	w.linkDown = true
	if r := reRequestGateReason(w, rqOpen, open); !strings.Contains(r, "link") {
		t.Fatalf("link down: %q", r)
	}
	w.linkDown, w.feedDown = false, true
	if r := reRequestGateReason(w, rqOpen, open); !strings.Contains(r, "feed") {
		t.Fatalf("feed down: %q", r)
	}
	w.feedDown = false
	if r := reRequestGateReason(w, rqOpen, ctAt(16, 30, 0)); !strings.Contains(r, "CME closed") {
		t.Fatalf("break: %q", r)
	}
	if r := reRequestGateReason(w, nil, open); r == "" {
		t.Fatal("a missing calendar cannot judge: it must refuse (fail closed)")
	}
}

// the spent-budget log line says what happens and what clears the pause.
func TestIngestLossSpentMessageSaysWhatClearsIt(t *testing.T) {
	m := ingestLossSpentMessage("MNQ", "1m", ctAt(9, 50, 0).UnixMilli())
	for _, want := range []string{"SPENT", "NOT asked for again", "stay paused", "ages out of the ring", "restarted", "17:00 CT"} {
		if !strings.Contains(m, want) {
			t.Fatalf("the spent-budget line must say %q: %s", want, m)
		}
	}
}

// a gate-only wire: the tracker's real gateLocked over a table of states must
// agree with the shared function (A4's behavioural half - the AST pin alone
// misses a gateLocked that still mentions the function on a dead branch).
type gateOnlyWire struct {
	holeWire
	link, feed bool
}

func (g gateOnlyWire) IsConnected() bool     { return g.link }
func (g gateOnlyWire) IsFeedConnected() bool { return g.feed }

func TestTrackerGateAndHookGateAgree(t *testing.T) {
	tr := &barHoleTracker{isOpen: rqOpen}
	for _, c := range []struct {
		link, feed bool
		now        time.Time
		blocked    bool
	}{
		{true, true, ctAt(10, 0, 0), false},
		{false, true, ctAt(10, 0, 0), true},
		{true, false, ctAt(10, 0, 0), true},
		{true, true, ctAt(16, 30, 0), true},
	} {
		got := tr.gateLocked(gateOnlyWire{link: c.link, feed: c.feed}, c.now)
		want := reRequestGateReason(&fakeRerequestWire{linkDown: !c.link, feedDown: !c.feed}, rqOpen, c.now)
		if got != want || (got != "") != c.blocked {
			t.Fatalf("link=%v feed=%v at %s: tracker %q, shared %q, want blocked=%v", c.link, c.feed, c.now, got, want, c.blocked)
		}
	}
}
