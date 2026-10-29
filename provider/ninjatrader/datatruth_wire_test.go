package ninjatrader

import (
	"context"
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	"vl/market"
)

// Production-call-site pins for the m446 data-truth wiring: the ledger is fed
// by the REAL ingest drop sites (evictForIngest / enqueueBarHistorical / the
// drain), the contract tracker by the REAL drain and observeContract, and the
// verdict is read through the REAL registered checks via datatruth.Evaluate.

func freshLedger(t *testing.T) {
	t.Helper()
	old := dataTruthLedger
	dataTruthLedger = checks.NewLedger(1000)
	t.Cleanup(func() { dataTruthLedger = old })
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
}

func wireForTest(t *testing.T, s *TCPServer) *DataTruthSources {
	t.Helper()
	ingestLastSum.Store(time.Now().Unix()) // the 1-line/min summary swaps the counters to 0: keep it quiet
	src := &DataTruthSources{Server: s, Cache: s.barCache, SnapshotTTL: time.Nanosecond}
	WireDataTruth(src, nil)
	return src
}

// A FINAL displaced from a queue holding only finals is DATA LOSS: the ledger
// names the minute, the ingest_loss check blocks the symbol, and it clears only
// when the minute is present AND closed in the ring.
func TestIngestFinalDropPausesUntilMinuteVerified(t *testing.T) {
	t.Setenv("INGEST_QUEUE_CAP", "3")
	resetIngestCounters()
	ingestFinalKept.Store(0)
	ingestFinalDropped.Store(0)
	freshLedger(t)
	s := NewTCPServer(nil)
	wireForTest(t, s)

	const wireT = 10 * 60_000 // close-stamped: the minute that OPENED at 9:00 of the epoch
	for i := 0; i < 3; i++ {
		s.enqueueBarUpdate("MNQ", "1m", "MNQ 12-26", finalFrame(wireT+int64(i)*60_000))
	}
	s.enqueueBarUpdate("MNQ", "1m", "MNQ 12-26", finalFrame(wireT+9*60_000)) // displaces the oldest final
	if ingestFinalDropped.Load() != 1 {
		t.Fatalf("fixture: final_dropped=%d, want 1", ingestFinalDropped.Load())
	}
	if got := dataTruthLedger.Pending(); got != 1 {
		t.Fatalf("ledger pending = %d, want the 1 dropped final", got)
	}
	now := time.Now()
	blocked, reasons := datatruth.Evaluate("MNQ", now)
	var why string
	for _, r := range reasons {
		if r.Check == checks.CheckIngestLoss {
			why = r.Detail
		}
	}
	if !blocked || !strings.Contains(why, "1 lost bar(s)") {
		t.Fatalf("a dropped FINAL must pause entries: blocked=%v reasons=%+v", blocked, reasons)
	}
	// the symbol-scoped ledger does not pause ES
	for _, r := range func() []datatruth.Reason { _, rs := datatruth.Evaluate("ES", now); return rs }() {
		if r.Check == checks.CheckIngestLoss {
			t.Fatalf("ES must not be paused by MNQ's loss: %+v", r)
		}
	}
	// the minute lands as a FORMING bar only: present but not closed -> still blocked
	s.barCache.Upsert("MNQ", "1m", []Bar{{T: wireT, O: 1, H: 2, L: 0.5, C: 1.2, V: 5}})
	if b, _ := datatruth.Evaluate("MNQ", now); !b {
		t.Fatal("a forming snapshot without a final or a successor is NOT verified")
	}
	// the final arrives (the re-request / late delivery): verified
	s.barCache.Upsert("MNQ", "1m", finalFrame(wireT))
	b, rs := datatruth.Evaluate("MNQ", now)
	for _, r := range rs {
		if r.Check == checks.CheckIngestLoss {
			t.Fatalf("verified minute must clear the ingest_loss check: %+v (blocked=%v)", r, b)
		}
	}
	if dataTruthLedger.Pending() != 0 {
		t.Fatal("verified entry must leave the ledger")
	}
}

// A HISTORICAL batch evicted by a live burst is counted by evictForIngest as an
// "intrabar drop-oldest"; the ledger must classify it by the MESSAGE.
func TestIngestHistoricalEvictionIsLedgered(t *testing.T) {
	t.Setenv("INGEST_QUEUE_CAP", "2")
	resetIngestCounters()
	freshLedger(t)
	s := NewTCPServer(nil)
	wireForTest(t, s)
	s.barIngestCh <- barIngestMsg{historical: true, symbol: "MNQ", timeframe: "5m", bars: []Bar{
		{T: 20 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3}, {T: 25 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3}}}
	s.barIngestCh <- barIngestMsg{historical: false, symbol: "MNQ", timeframe: "1m", bars: formingFrame(60_000)}
	s.enqueueBarUpdate("MNQ", "1m", "MNQ 12-26", formingFrame(2*60_000)) // full queue: the historical head is the victim
	if ingestDropOld.Load() != 1 {
		t.Fatalf("fixture: drop-oldest=%d", ingestDropOld.Load())
	}
	if got := dataTruthLedger.Pending(); got != 2 {
		t.Fatalf("both bars of the evicted historical batch must be ledgered, pending=%d", got)
	}
	if b, rs := datatruth.Evaluate("MNQ", time.Now()); !b || len(rs) == 0 {
		t.Fatalf("a dropped historical batch must pause entries: %v %+v", b, rs)
	}
}

// Intrabar drops self-heal: they are logged as a rate and NEVER pause.
func TestIngestIntrabarDropsDoNotPause(t *testing.T) {
	t.Setenv("INGEST_QUEUE_CAP", "2")
	resetIngestCounters()
	freshLedger(t)
	s := NewTCPServer(nil)
	wireForTest(t, s)
	for i := 0; i < 8; i++ {
		s.enqueueBarUpdate("MNQ", "1m", "MNQ 12-26", formingFrame(int64(i+1)*60_000))
	}
	if ingestDropOld.Load() == 0 {
		t.Fatal("fixture: no intrabar drop happened")
	}
	if dataTruthLedger.Pending() != 0 {
		t.Fatal("intrabar drops must not enter the loss ledger")
	}
	for _, r := range func() []datatruth.Reason { _, rs := datatruth.Evaluate("MNQ", time.Now()); return rs }() {
		if r.Check == checks.CheckIngestLoss {
			t.Fatalf("intrabar drops paused entries: %+v", r)
		}
	}
	if dataTruthLedger.IntrabarPerMin(time.Now()) <= 0 {
		t.Fatal("the intrabar rate must be recorded for the log")
	}
}

// Two contracts on live frames, then the REAL roll purge (observeContract):
// the check blocks while the window holds both, and a ring purged on the roll
// forgets the retired contract.
func TestContractRollGuardThroughDrainAndObserve(t *testing.T) {
	freshLedger(t)
	liveContracts.reset("MNQ")
	t.Cleanup(func() { liveContracts.reset("MNQ") })
	s := NewTCPServer(nil)
	s.barIngestCh = make(chan barIngestMsg, 16)
	src := &DataTruthSources{Server: s, Cache: s.barCache, HistoryContract: func(string) (string, bool) { return "MNQ 12-26", true }, Now: func() time.Time { return time.UnixMilli(100 * 60_000) }}
	WireDataTruth(src, nil)
	s.wg.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); s.wg.Wait() }()
	go s.drainBarIngest(ctx)

	s.observeContract("MNQ", "MNQ 12-26", time.UnixMilli(1))
	s.barIngestCh <- barIngestMsg{symbol: "MNQ", timeframe: "1m", contract: "MNQ 12-26", bars: []Bar{{T: 99 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3}}}
	waitFor(t, func() bool { return len(liveContracts.window("MNQ", 0)) == 1 })
	if got := contractVerdict(t); got != "" {
		t.Fatalf("single contract must be clear, got %q", got)
	}
	// a stray frame on the NEXT contract lands before any roll was observed
	s.barIngestCh <- barIngestMsg{symbol: "MNQ", timeframe: "1m", contract: "MNQ 03-27", bars: []Bar{{T: 100 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3}}}
	waitFor(t, func() bool { return len(liveContracts.window("MNQ", 0)) == 2 })
	if got := contractVerdict(t); !strings.Contains(got, "2 contracts") {
		t.Fatalf("two contracts in the window must pause, got %q", got)
	}
	// the AddOn's ACK names the new contract: the production roll path purges the
	// ring AND the tracker; history (store) still says 12-26 -> live != history
	s.observeContract("MNQ", "MNQ 03-27", time.UnixMilli(2))
	if got := contractVerdict(t); !strings.Contains(got, "differs from the stored history contract") {
		t.Fatalf("after the roll the stale history contract must pause, got %q", got)
	}
	src.HistoryContract = func(string) (string, bool) { return "MNQ 03-27", true }
	if got := contractVerdict(t); got != "" {
		t.Fatalf("history re-seeded on the new contract must clear, got %q", got)
	}
}

func contractVerdict(t *testing.T) string {
	t.Helper()
	_, rs := datatruth.Evaluate("MNQ", time.UnixMilli(100*60_000))
	for _, r := range rs {
		if r.Check == checks.CheckContractRoll {
			return r.Detail
		}
	}
	return ""
}

func waitFor(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

// The shared EMA warm-up formula must not drift from the production one.
func TestDataTruthEMAWarmupMatchesMarket(t *testing.T) {
	for _, p := range []int{9, 14, 20, 26, 34, 50, 200} {
		if got, want := checks.EMAWarmupBars(p), market.EMAWarmupBars(p); got != want {
			t.Errorf("period %d: checks %d vs market %d", p, got, want)
		}
	}
}

// The cache-backed sources deliver the ring in the checks' shape.
func TestDataTruthSourcesBarsFromCache(t *testing.T) {
	c := NewBarCache(50)
	c.Upsert("MNQ", "1m", []Bar{{T: 120_000, O: 1, H: 3, L: 0.5, C: 2, V: 9, Final: true}})
	src := &DataTruthSources{Cache: c}
	got, err := src.Bars("MNQ", 1)
	if err != nil || len(got) != 1 || got[0].T != 60_000 || got[0].H != 3 || !got[0].Final {
		t.Fatalf("%v %+v", err, got)
	}
	if src.Partner("MNQ") != "ES" || src.Partner("NQ") != "ES" {
		t.Fatal("partner map")
	}
	if _, err := (&DataTruthSources{}).Bars("MNQ", 1); err == nil {
		t.Fatal("no cache must be an error (fail closed)")
	}
}
