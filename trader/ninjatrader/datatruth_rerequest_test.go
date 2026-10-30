package ninjatrader

import (
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
)

// ── #55 m449 — ingest_loss HAS A WORKING REMEDY ─────────────────────────────
//
// ntwire.DataTruthRerequest was never assigned in production: a dropped final /
// historical bar paused entries until the ring aged it out, and nothing asked NT8
// for it. WireDataTruthChecks now installs the hook over the tracker's own wire
// sends. MUTANTS (shown RED in the report):
//   R1  WireDataTruthChecks does not call wireDataTruthRerequest -> boot pin RED
//   R2  the hook always sends the STANDARD request (no deep branch) -> deep pin RED
//   R3  a TOO-SOON refusal is dropped, not retried                  -> retry pin RED

type fakeRerequestWire struct {
	std, deep []string
	deepBack  int
	bb        int
	err       error
	// down is false by default: the link and feed are UP (fakeRerequestWire{}
	// is a healthy wire); linkDown / feedDown flip one half.
	linkDown, feedDown bool
}

func (f *fakeRerequestWire) IsConnected() bool     { return !f.linkDown }
func (f *fakeRerequestWire) IsFeedConnected() bool { return !f.feedDown }

func (f *fakeRerequestWire) RequestGapBackfillAt(sym string, now time.Time) error {
	f.std = append(f.std, sym)
	return f.err
}
func (f *fakeRerequestWire) RequestDeepGapBackfillAt(sym string, bb int, now time.Time) error {
	f.deep = append(f.deep, sym)
	f.deepBack = bb
	return f.err
}
func (f *fakeRerequestWire) BarsBackAsked() int { return f.bb }

func syncSpawn(f func()) { f() }

// rqOpen is the production calendar the hook is wired with.
var rqOpen = barHoleExpected

func TestIngestLossHookIsWiredByTheBootWiring(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	t.Cleanup(SetDataTruthReadyForTest(false))
	dataTruthWiring.Store(false)
	t.Cleanup(func() { dataTruthWiring.Store(false) })
	prev := ntwire.DataTruthRerequest
	ntwire.SetDataTruthRerequest(nil)
	t.Cleanup(func() { ntwire.SetDataTruthRerequest(prev) })
	st := dtStore(t)
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	if ntwire.DataTruthRerequestWired() {
		t.Fatal("precondition: no hook")
	}
	WireDataTruthChecks(bh, ntwire.NewTCPServer(nil))
	if !ntwire.DataTruthRerequestWired() {
		t.Fatal("the production boot wiring must install the ingest_loss re-request hook")
	}
}

func TestIngestLossHookUsesTheTrackersStandardAndDeepSends(t *testing.T) {
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, kernel.CTLocation())
	w := &fakeRerequestWire{bb: 2000}
	hook := dataTruthRerequester(w, rqOpen, func() time.Time { return now }, syncSpawn, func(time.Duration, func()) {})

	// a fresh 1m loss (10 minutes ago) is inside the standard replay
	hook("MNQ", "1m", now.Add(-10*time.Minute).UnixMilli(), now.Add(-9*time.Minute).UnixMilli())
	if len(w.std) != 1 || len(w.deep) != 0 {
		t.Fatalf("a recent 1m loss uses the STANDARD re-request: std=%v deep=%v", w.std, w.deep)
	}
	// a 1m loss ~2 sessions back (~2700 open minutes) needs the DEEP one, sized past the span
	w2 := &fakeRerequestWire{bb: 2000}
	hook2 := dataTruthRerequester(w2, rqOpen, func() time.Time { return now }, syncSpawn, func(time.Duration, func()) {})
	hook2("MNQ", "1m", now.Add(-60*time.Hour).UnixMilli(), now.Add(-59*time.Hour).UnixMilli())
	if len(w2.deep) != 1 || w2.deepBack <= 2000 || w2.deepBack > ntwire.DeepGapBackfillMaxBarsBack {
		t.Fatalf("an old 1m loss uses the DEEP re-request sized past the span: deep=%v bars_back=%d", w2.deep, w2.deepBack)
	}
	// a higher frame is inside the standard replay by construction
	w3 := &fakeRerequestWire{bb: 2000}
	hook3 := dataTruthRerequester(w3, rqOpen, func() time.Time { return now }, syncSpawn, func(time.Duration, func()) {})
	hook3("MNQ", "5m", now.Add(-100*time.Hour).UnixMilli(), now.Add(-99*time.Hour).UnixMilli())
	if len(w3.std) != 1 || len(w3.deep) != 0 {
		t.Fatalf("a 5m loss uses the standard re-request: std=%v deep=%v", w3.std, w3.deep)
	}
	// beyond even the deepest ask: nothing is sent (and it is said, not silent)
	w4 := &fakeRerequestWire{bb: 2000}
	hook4 := dataTruthRerequester(w4, rqOpen, func() time.Time { return now }, syncSpawn, func(time.Duration, func()) {})
	hook4("MNQ", "1m", now.Add(-30*24*time.Hour).UnixMilli(), now.Add(-29*24*time.Hour).UnixMilli())
	if len(w4.std)+len(w4.deep) != 0 {
		t.Fatalf("a span beyond the deepest reach is not re-requested: %v %v", w4.std, w4.deep)
	}
}

func TestIngestLossHookRetriesATooSoonRefusal(t *testing.T) {
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, kernel.CTLocation())
	w := &fakeRerequestWire{bb: 2000, err: ntwire.ErrGapBackfillTooSoon}
	var queued []func()
	hook := dataTruthRerequester(w, rqOpen, func() time.Time { return now }, syncSpawn, func(d time.Duration, f func()) { queued = append(queued, f) })
	hook("MNQ", "1m", now.Add(-10*time.Minute).UnixMilli(), now.Add(-9*time.Minute).UnixMilli())
	if len(w.std) != 1 || len(queued) != 1 {
		t.Fatalf("a too-soon refusal is retried later: sent=%d queued=%d", len(w.std), len(queued))
	}
	w.err = nil
	queued[0]()
	if len(w.std) != 2 {
		t.Fatalf("the retry must send again, sent=%d", len(w.std))
	}
	// a hard refusal (budget spent, link down) is NOT retried in a loop
	w2 := &fakeRerequestWire{bb: 2000, err: ntwire.ErrGapBackfillFeedDown}
	var q2 []func()
	h2 := dataTruthRerequester(w2, rqOpen, func() time.Time { return now }, syncSpawn, func(d time.Duration, f func()) { q2 = append(q2, f) })
	h2("MNQ", "1m", now.Add(-10*time.Minute).UnixMilli(), now.Add(-9*time.Minute).UnixMilli())
	if len(q2) != 0 {
		t.Fatal("only a too-soon refusal is retried")
	}
}
