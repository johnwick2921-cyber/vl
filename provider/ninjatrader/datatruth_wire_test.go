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

// enqueueBarHistorical gives up after 2s on a stalled drain and drops the batch:
// that is the restart seed lost. It must land in the ledger, bar by bar.
func TestIngestHistoricalTimeoutIsLedgered(t *testing.T) {
	t.Setenv("INGEST_QUEUE_CAP", "1")
	resetIngestCounters()
	freshLedger(t)
	s := NewTCPServer(nil)
	wireForTest(t, s)
	s.barIngestCh <- barIngestMsg{symbol: "MNQ", timeframe: "1m", bars: formingFrame(60_000)} // stalled drain: the queue stays full
	s.enqueueBarHistorical("MNQ", "5m", []Bar{{T: 30 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3}, {T: 35 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3}})
	if ingestDropHist.Load() == 0 && dataTruthLedger.Pending() == 0 {
		t.Fatal("fixture: the historical batch was not dropped")
	}
	if got := dataTruthLedger.Pending(); got != 2 {
		t.Fatalf("both bars of the dropped historical batch must be ledgered, pending=%d", got)
	}
	b, rs := datatruth.Evaluate("MNQ", time.Now())
	if !b {
		t.Fatalf("a dropped historical batch must pause entries: %+v", rs)
	}
	// the replay lands (re-request): both minutes present -> clear
	s.barCache.SeedHistorical("MNQ", "5m", []Bar{{T: 30 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3}, {T: 35 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3}})
	for _, r := range func() []datatruth.Reason { _, r := datatruth.Evaluate("MNQ", time.Now()); return r }() {
		if r.Check == checks.CheckIngestLoss {
			t.Fatalf("landed replay must clear ingest_loss: %+v", r)
		}
	}
}

func ingestLossReason(t *testing.T, now time.Time) (bool, string) {
	t.Helper()
	_, rs := datatruth.Evaluate("MNQ", now)
	for _, r := range rs {
		if r.Check == checks.CheckIngestLoss {
			return true, r.Detail
		}
	}
	return false, ""
}

// Review round 1, D1: a dropped message that carried NO real bar (an empty
// bars_historical frame, a placeholder-only batch, a final frame of
// placeholders) lost no minute. It used to ledger an "unknown" loss that nothing
// could clear, pausing MNQ entries until the process restarted.
func TestIngestDropOfAMessageWithNoRealBarOwesNothing(t *testing.T) {
	freshLedger(t)
	s := NewTCPServer(nil)
	wireForTest(t, s)
	placeholder := Bar{T: 40 * 60_000, O: 5, H: 5, L: 5, C: 5, V: 0, Final: true}
	for name, m := range map[string]barIngestMsg{
		"empty historical":            {historical: true, symbol: "MNQ", timeframe: "1m"},
		"placeholder-only historical": {historical: true, symbol: "MNQ", timeframe: "1m", bars: []Bar{placeholder}},
		"placeholder-only final":      {symbol: "MNQ", timeframe: "1m", bars: []Bar{placeholder}},
	} {
		noteIngestLoss(m)
		if blocked, why := ingestLossReason(t, time.Now()); blocked {
			t.Fatalf("%s: nothing was lost, entries must not pause: %s", name, why)
		}
	}
	if dataTruthLedger.Pending() != 0 {
		t.Fatalf("pending=%d, want 0", dataTruthLedger.Pending())
	}
}

// D1: the ledger-cap overflow is a RANGE; it clears through the production
// whole-range verifier once every open-market slot of it is in the ring.
func TestIngestLedgerOverflowClearsWhenTheRangeIsWholeInTheRing(t *testing.T) {
	old := dataTruthLedger
	dataTruthLedger = checks.NewLedger(1)
	t.Cleanup(func() { dataTruthLedger = old })
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	s := NewTCPServer(nil)
	wireForTest(t, s)
	// 5m minutes inside an open CME session (Tue 2026-10-27 13:00Z = 08:00 CT)
	base := time.Date(2026, 10, 27, 13, 0, 0, 0, time.UTC).UnixMilli()
	mk := func(i int) Bar { return Bar{T: base + int64(i)*5*60_000, O: 1, H: 2, L: 1, C: 2, V: 3, Final: true} }
	// the batch carries 4 bars; the ledger holds 1, so 3 overflow into the range
	noteIngestLoss(barIngestMsg{historical: true, symbol: "MNQ", timeframe: "5m", bars: []Bar{mk(1), mk(2), mk(3), mk(4)}})
	// the ring holds a successor of the range, so the bars before it can be judged against it
	s.barCache.SeedHistorical("MNQ", "5m", []Bar{mk(0), mk(1), mk(2), mk(4)}) // slot 3 is missing
	// wire-stamp: Seed converts close-stamp -> open-stamp; the ledger speaks the same stamp
	if blocked, why := ingestLossReason(t, time.Now()); !blocked || !strings.Contains(why, "not yet verified") {
		t.Fatalf("a range with a missing slot must keep pausing: %q", why)
	}
	s.barCache.SeedHistorical("MNQ", "5m", []Bar{mk(0), mk(1), mk(2), mk(3), mk(4)})
	if blocked, why := ingestLossReason(t, time.Now()); blocked {
		t.Fatalf("the whole range is in the ring: must clear: %s", why)
	}
}

// D7: every FINAL / HISTORICAL loss hands its span to the integrator's
// re-request hook; intrabar drops and empty messages do not.
func TestIngestLossFiresTheRerequestHook(t *testing.T) {
	freshLedger(t)
	var got [][3]int64
	old := DataTruthRerequest
	DataTruthRerequest = func(sym, tf string, from, to int64) { got = append(got, [3]int64{from, to, int64(len(tf))}) }
	t.Cleanup(func() { DataTruthRerequest = old })
	noteIngestLoss(barIngestMsg{historical: true, symbol: "MNQ", timeframe: "5m", bars: []Bar{
		{T: 30 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3}, {T: 35 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3}}})
	noteIngestLoss(barIngestMsg{symbol: "MNQ", timeframe: "1m", bars: formingFrame(60_000)})
	noteIngestLoss(barIngestMsg{historical: true, symbol: "MNQ", timeframe: "1m"})
	if len(got) != 1 {
		t.Fatalf("only the real historical loss re-requests: %v", got)
	}
}

// D3: the contract of the SEED batches is what the history was built from. A
// ring seeded on the OLD month with live frames on the NEW month is the
// 2026-09-10 failure; with no HistoryContract reader wired the first draft read
// the AddOn's ACK against its own live frames and called it clean.
func TestContractGuardSeesAnOldMonthSeedUnderNewMonthLiveFrames(t *testing.T) {
	freshLedger(t)
	liveContracts.reset("MNQ")
	histContracts.reset("MNQ")
	t.Cleanup(func() { liveContracts.reset("MNQ"); histContracts.reset("MNQ") })
	s := NewTCPServer(nil)
	s.barIngestCh = make(chan barIngestMsg, 16)
	src := &DataTruthSources{Server: s, Cache: s.barCache, Now: func() time.Time { return time.UnixMilli(100 * 60_000) }} // HistoryContract: nil
	WireDataTruth(src, nil)
	s.wg.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); s.wg.Wait() }()
	go s.drainBarIngest(ctx)

	s.observeContract("MNQ", "MNQ 03-27", time.UnixMilli(1)) // the ACK names the NEW month
	// the seed batch still carries the OLD month's bars
	s.enqueueBarHistoricalContract("MNQ", "1m", "MNQ 12-26", []Bar{{T: 98 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3, Final: true}})
	s.barIngestCh <- barIngestMsg{symbol: "MNQ", timeframe: "1m", contract: "MNQ 03-27", bars: []Bar{{T: 99 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3}}}
	waitFor(t, func() bool {
		return len(liveContracts.window("MNQ", 0)) == 1 && len(histContracts.window("MNQ", 0)) == 1
	})
	got := contractVerdict(t)
	if !strings.Contains(got, "2 contracts") && !strings.Contains(got, "differs from the stored history") {
		t.Fatalf("an old-month seed under new-month live frames must pause, got %q", got)
	}
	// the seed is replayed on the new month (the AddOn's post-roll replay): clear
	s.observeContract("MNQ", "MNQ 03-27", time.UnixMilli(2))
	histContracts.reset("MNQ")
	liveContracts.reset("MNQ")
	s.enqueueBarHistoricalContract("MNQ", "1m", "MNQ 03-27", []Bar{{T: 98 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3, Final: true}})
	s.barIngestCh <- barIngestMsg{symbol: "MNQ", timeframe: "1m", contract: "MNQ 03-27", bars: []Bar{{T: 99 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3}}}
	waitFor(t, func() bool {
		return len(liveContracts.window("MNQ", 0)) == 1 && len(histContracts.window("MNQ", 0)) == 1
	})
	if got := contractVerdict(t); got != "" {
		t.Fatalf("a seed on the live month must clear, got %q", got)
	}
}

// D3: a seed batch that names no contract (an AddOn that omits it) is UNKNOWN
// history: the guard blocks, it never reads "" as a match.
func TestContractGuardBlocksWhenTheSeedNamesNoContract(t *testing.T) {
	freshLedger(t)
	liveContracts.reset("MNQ")
	histContracts.reset("MNQ")
	t.Cleanup(func() { liveContracts.reset("MNQ"); histContracts.reset("MNQ") })
	s := NewTCPServer(nil)
	s.observeContract("MNQ", "MNQ 12-26", time.UnixMilli(1))
	src := &DataTruthSources{Server: s, Cache: s.barCache, Now: func() time.Time { return time.UnixMilli(100 * 60_000) }}
	WireDataTruth(src, nil)
	if got := contractVerdict(t); !strings.Contains(got, "history contract unknown") {
		t.Fatalf("no seed contract known: must block, got %q", got)
	}
}

// D5: WireDataTruth with no store reader leaves the swing and levels checks
// REQUIRED but unwired: they block (fail closed), they do not vanish.
func TestWiringWithoutStoreReaderBlocksTheSwingChecks(t *testing.T) {
	freshLedger(t)
	s := NewTCPServer(nil)
	wireForTest(t, s) // swing == nil
	_, rs := datatruth.Evaluate("MNQ", time.Now())
	want := map[string]bool{checks.CheckLookbackSwing4h: false, checks.CheckLookbackLevels: false, checks.CheckLookbackSession: false}
	for _, r := range rs {
		if _, ok := want[r.Check]; ok {
			want[r.Check] = strings.Contains(r.Detail, "not wired")
		}
	}
	for n, ok := range want {
		if !ok {
			t.Errorf("%s must block as 'not wired' when SwingInputs is nil", n)
		}
	}
	if h := DataTruthStats(time.Now())[checks.CheckLookbackSwing4h].(map[string]any); h["blocked"] != true {
		t.Fatalf("/api/health must show the missing check blocked: %v", h)
	}
}

// D3, the leg the window cannot see: the OLD-month seed's newest bar is already
// outside the 30-minute decision window, so only "live != the contract of the
// history" can catch it. With History read from the live frames (the first
// draft) this was a tautology and read clean.
func TestContractGuardSeesAnOldMonthSeedOlderThanTheWindow(t *testing.T) {
	freshLedger(t)
	liveContracts.reset("MNQ")
	histContracts.reset("MNQ")
	t.Cleanup(func() { liveContracts.reset("MNQ"); histContracts.reset("MNQ") })
	s := NewTCPServer(nil)
	s.barIngestCh = make(chan barIngestMsg, 16)
	src := &DataTruthSources{Server: s, Cache: s.barCache, Now: func() time.Time { return time.UnixMilli(100 * 60_000) }}
	WireDataTruth(src, nil)
	s.wg.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); s.wg.Wait() }()
	go s.drainBarIngest(ctx)

	s.observeContract("MNQ", "MNQ 03-27", time.UnixMilli(1))
	s.enqueueBarHistoricalContract("MNQ", "1m", "MNQ 12-26", []Bar{{T: 10 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3, Final: true}}) // 90 min old
	s.barIngestCh <- barIngestMsg{symbol: "MNQ", timeframe: "1m", contract: "MNQ 03-27", bars: []Bar{{T: 99 * 60_000, O: 1, H: 2, L: 1, C: 2, V: 3}}}
	waitFor(t, func() bool {
		return len(liveContracts.window("MNQ", 0)) == 1 && len(histContracts.window("MNQ", 0)) == 1
	})
	if got := contractVerdict(t); !strings.Contains(got, "differs from the stored history contract") {
		t.Fatalf("an old-month seed outside the window must still pause on live != history, got %q", got)
	}
}

// Release #55 round 3 (review 4, D4): the ingest_loss pause text words its
// duration from checks.LedgerRingBars; it must be the ring the cache really has.
func TestLedgerRingBarsMatchesTheBarCacheRing(t *testing.T) {
	if checks.LedgerRingBars != DefaultBarCacheMaxBars {
		t.Fatalf("checks.LedgerRingBars=%d but the bar cache ring is %d: the ingest_loss duration text would lie", checks.LedgerRingBars, DefaultBarCacheMaxBars)
	}
}
