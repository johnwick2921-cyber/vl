package ninjatrader

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── DATA-INTEGRITY GATE (m445) — PRODUCTION CALL SITES ─────────────────────
//
// The verdict tests below run the REAL tracker (newBarHoleTracker) installed
// where production installs it (barHoles), fed by the real persister closure on
// a real SQLite store through a started TCPServer and a fake AddOn socket
// (newGHFixture) — or, for calendar and real-data cases, by observeLive with
// the rows the persister would hand it. They ask IntegrityVerdictFor, the
// function the trader package asks.
//
// MUTANTS (each shown RED in the wave report):
//   M1  `add` skips t.holes             -> TestIntegrityBlocksAnOpenHoleInTheWindow RED
//   M2  `add` skips t.retired           -> TestARetiredHoleStillBlocksUntilMeasuredFilled RED
//   M3  retainRetiredLocked is a no-op  -> TestARetiredHoleStillBlocksUntilMeasuredFilled RED
//   M4  an unscanned symbol reads clear -> TestIntegrityDetectorUnknownIsBlockedNotClear RED
//   M5  the window start is ignored     -> TestAHoleOutsideTheWindowDoesNotBlock RED
//   M6  integrityArmed ignored (always armed) -> TestIntegrityIsInertWithoutTheNT8Bridge RED
//   M7  judgeErr never set              -> TestIntegrityDetectorUnknownIsBlockedNotClear RED

func armIntegrity(t *testing.T) {
	t.Helper()
	t.Cleanup(SetIntegrityArmedForTest(true))
}

// ── the 10-14 fixture hole, through the real persister and tracker ─────────

func TestIntegrityBlocksAnOpenHoleInTheWindow(t *testing.T) {
	armIntegrity(t)
	f := newGHFixture(t)
	det := f.openHole() // 03:44–07:48 CT, 244 open minutes, refilling

	v := IntegrityVerdictFor("MNQ", IntegrityIntradayOpenMin, det)
	if !v.Armed || !v.Blocked || v.Class != IntegrityClassHole {
		t.Fatalf("an open hole in the intraday window must BLOCK with the hole class: %+v", v)
	}
	if len(v.Holes) != 1 {
		t.Fatalf("want exactly the one hole: %+v", v.Holes)
	}
	h := v.Holes[0]
	if h.Window != "10-14 03:44–07:48 CT" || h.Missing != 244 || h.State != holeStateRefilling || h.Unfillable {
		t.Fatalf("hole facts (all READ): %+v", h)
	}
	if got := IntegrityVerdictFor("ES", IntegrityIntradayOpenMin, det); got.Class == IntegrityClassHole {
		t.Fatalf("an MNQ hole must not block ES: %+v", got)
	}
}

// A hole outside the window does not block; the same hole inside the DEEP
// window does (the swing path reads its 4h EMA warm-up; the AI / planner paths their 1m tape).
func TestAHoleOutsideTheWindowDoesNotBlock(t *testing.T) {
	armIntegrity(t)
	f := newGHFixture(t)
	f.openHole()
	// Fri 10-16 10:00 CT: the hole ended 10-14 07:48 — more than 1500 open
	// minutes (≈ 25 trading hours) earlier, but well inside the deep window.
	later := ct(2026, 10, 16, 10, 0)
	intra := IntegrityVerdictFor("MNQ", IntegrityIntradayOpenMin, later)
	for _, h := range intra.Holes {
		t.Fatalf("a hole that ended before the intraday window starts must not block: %+v (window from %s)", h, kernel.TableTimeCT(time.UnixMilli(intra.WindowFromMs)))
	}
	deep := IntegrityVerdictFor("MNQ", IntegrityDeepOpenMin, later)
	if !deep.Blocked || deep.Class != IntegrityClassHole || len(deep.Holes) != 1 {
		t.Fatalf("the same hole IS inside the deep (4h EMA 34) window and must block there: %+v", deep)
	}
	if intra.WindowFromMs <= deep.WindowFromMs {
		t.Fatalf("the intraday window must start later than the deep one: %d vs %d", intra.WindowFromMs, deep.WindowFromMs)
	}
}

// The hole fills (NT8 answered the re-request): the verdict resumes — on the
// tracker's MEASUREMENT, not on a timer.
func TestIntegrityResumesWhenTheHoleIsMeasuredFilled(t *testing.T) {
	armIntegrity(t)
	f := newGHFixture(t)
	f.addon.setAnswer(func(n int, _ ntwire.BarsSubscribePayload) {
		if n == 1 {
			f.answerEmpty()
			return
		}
		f.answerFull()
	})
	det := f.openHole()
	if v := IntegrityVerdictFor("MNQ", IntegrityDeepOpenMin, det); !v.Blocked {
		t.Fatalf("setup: blocked while the hole is open: %+v", v)
	}
	f.tr.tick(det.Add(61 * time.Second))
	f.waitReRequests(1)
	f.tr.tick(det.Add(3*time.Minute + 5*time.Second))
	f.waitReRequests(2)
	waitFor(t, "NT8's replay to reach the store", func() bool {
		return f.storedOpen(ct(2026, 10, 14, 3, 44), ct(2026, 10, 14, 7, 48)) == 244
	})
	// BEFORE the measure: the store holds the minutes but the tracker has not
	// re-read them — still blocked (resume is a measurement, not a guess).
	if v := IntegrityVerdictFor("MNQ", IntegrityDeepOpenMin, det.Add(4*time.Minute)); !v.Blocked {
		t.Fatalf("resume only after the tracker's re-measure: %+v", v)
	}
	f.tr.tick(det.Add(5*time.Minute + 10*time.Second)) // the measure
	v := IntegrityVerdictFor("MNQ", IntegrityDeepOpenMin, det.Add(6*time.Minute))
	if v.Blocked || len(v.Holes) != 0 {
		t.Fatalf("a measured-filled hole must resume trading: %+v\n%s", v, f.sink.dump())
	}
}

// A hole no re-request can reach is RETIRED by the tracker. The old tracker
// dropped it, which read as "all clear" while the minutes were still gone: the
// gate keeps it until it is measured filled or ages out.
func TestARetiredHoleStillBlocksUntilMeasuredFilled(t *testing.T) {
	armIntegrity(t)
	f := newGHFixture(t)
	f.stream(ct(2026, 10, 2, 10, 0), ct(2026, 10, 2, 10, 5))   // Fri 10-02 (m444: >DeepGapBackfillMaxBarsBack open min back, so no deep re-request reaches it)
	f.stream(ct(2026, 10, 14, 7, 48), ct(2026, 10, 14, 7, 49)) // Wed 10-14
	waitFor(t, "the tracker to open the Fri→Wed hole", func() bool { return len(f.holes()) == 1 })
	det := f.clk.Now()
	f.tr.tick(det.Add(61 * time.Second))                           // beyond NT8's standard AND deep replay: RETIRED
	if hs := f.holes(); len(hs) != 1 || hs[0].State != "retired" { // m444: a retired hole stays LISTED in the tracker
		t.Fatalf("setup: the tracker must have retired the hole: %+v", hs)
	}
	now := det.Add(2 * time.Minute)
	v := IntegrityVerdictFor("MNQ", IntegrityDeepOpenMin, now)
	if !v.Blocked || v.Class != IntegrityClassHole || len(v.Holes) != 1 {
		t.Fatalf("a RETIRED hole is not a filled one: still blocked: %+v", v)
	}
	if h := v.Holes[0]; h.State != "retired" || !h.Unfillable || h.Window != "10-02 10:06–10-14 07:48 CT" {
		t.Fatalf("retired hole facts: %+v", h)
	}
	// the owner reloads NT8 history and the minutes reach the store; the
	// retired hole is re-measured (at most once a minute) and the gate resumes.
	var rows []store.BarHistoryDB
	for m := ct(2026, 10, 2, 10, 6); m.Before(ct(2026, 10, 14, 7, 48)); m = m.Add(time.Minute) {
		if barHoleExpected(m) {
			rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: "1m", Contract: ghContract, Source: store.BarSourceHistorical, OpenTimeMs: m.UnixMilli(), O: 1, H: 2, L: 0.5, C: 1.5, V: 1})
		}
	}
	if err := f.bh.InsertBars(rows); err != nil {
		t.Fatal(err)
	}
	f.tr.tick(now.Add(30 * time.Second)) // the re-measure of the retained hole
	v = IntegrityVerdictFor("MNQ", IntegrityDeepOpenMin, now.Add(30*time.Second))
	if v.Blocked || len(v.Holes) != 0 {
		t.Fatalf("a retired hole that is measured filled must release the gate: %+v\n%s", v, f.sink.dump())
	}
}

// ── fake-store harness for calendar / unknown / real-data cases ────────────

type memHoleStore struct {
	mu       sync.Mutex
	times    []int64        // ascending open times
	imports  map[int64]bool // open times stored as history-import snapshots (Source)
	prevErr  error
	rangeErr error
}

func (s *memHoleStore) add(ms int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.times = append(s.times, ms) // callers add ascending
}

func (s *memHoleStore) PrevOpenTimeOn(_, _, _ string, beforeMs int64) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prevErr != nil {
		return 0, false, s.prevErr
	}
	i := sort.Search(len(s.times), func(i int) bool { return s.times[i] >= beforeMs })
	if i == 0 {
		return 0, false, nil
	}
	return s.times[i-1], true, nil
}

func (s *memHoleStore) BarsBetweenOn(_, _, _ string, fromMs, toMs int64) ([]store.BarHistoryDB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rangeErr != nil {
		return nil, s.rangeErr
	}
	lo := sort.Search(len(s.times), func(i int) bool { return s.times[i] >= fromMs })
	hi := sort.Search(len(s.times), func(i int) bool { return s.times[i] >= toMs })
	out := make([]store.BarHistoryDB, 0, hi-lo)
	for _, ms := range s.times[lo:hi] {
		row := store.BarHistoryDB{Symbol: "MNQ", TF: "1m", Contract: ghContract, OpenTimeMs: ms}
		if s.imports[ms] {
			row.Source = store.BarSourceHistoricalImport
		}
		out = append(out, row)
	}
	return out, nil
}

type memHarness struct {
	t   *testing.T
	st  *memHoleStore
	tr  *barHoleTracker
	say *lineSink
	clk time.Time
}

func newMemHarness(t *testing.T) *memHarness {
	t.Helper()
	armIntegrity(t)
	st := &memHoleStore{}
	tr := newBarHoleTracker(st, func() *ntwire.TCPServer { return nil })
	sink := &lineSink{}
	tr.say = sink.say
	h := &memHarness{t: t, st: st, tr: tr, say: sink}
	tr.clock = func() time.Time { return h.clk }
	prev := barHoles.Swap(tr)
	t.Cleanup(func() { barHoles.Store(prev) })
	return h
}

// bar delivers the 1m bar opening at `open`, the way the persister does once it
// has written it: the store first, then the tracker.
func (h *memHarness) bar(open time.Time) {
	h.t.Helper()
	ms := open.UnixMilli()
	h.st.add(ms)
	h.clk = open.Add(time.Minute)
	h.tr.observeLive("MNQ", "1m", ghContract, []store.BarHistoryDB{{Symbol: "MNQ", TF: "1m", Contract: ghContract, OpenTimeMs: ms}})
}

func (h *memHarness) run(from, to time.Time) {
	for m := from; !m.After(to); m = m.Add(time.Minute) {
		if barHoleExpected(m) {
			h.bar(m)
		}
	}
}

func (h *memHarness) verdict(win int, now time.Time) IntegrityVerdict {
	return IntegrityVerdictFor("MNQ", win, now)
}

// UNKNOWN is BLOCKED, never "no holes".
func TestIntegrityDetectorUnknownIsBlockedNotClear(t *testing.T) {
	// 1. no tracker at all (no bar store wired) while the NT8 bridge is armed.
	armIntegrity(t)
	prev := barHoles.Swap(nil)
	if v := IntegrityVerdictFor("MNQ", IntegrityIntradayOpenMin, ct(2026, 10, 14, 9, 0)); !v.Blocked || v.Class != IntegrityClassUnknown || !strings.Contains(v.Unknown, "not running") {
		t.Fatalf("no tracker = UNKNOWN = blocked: %+v", v)
	}
	barHoles.Store(prev)

	// 2. a tracker that has not judged the symbol yet this process (the boot
	//    scan has not run): blocked until the first live bar is judged.
	h := newMemHarness(t)
	now := ct(2026, 10, 14, 9, 0)
	if v := h.verdict(IntegrityIntradayOpenMin, now); !v.Blocked || v.Class != IntegrityClassUnknown || !strings.Contains(v.Unknown, "boot hole scan") {
		t.Fatalf("an unscanned symbol must be blocked, not clear: %+v", v)
	}
	h.run(ct(2026, 10, 14, 8, 0), ct(2026, 10, 14, 8, 59))
	if v := h.verdict(IntegrityIntradayOpenMin, now); v.Blocked {
		t.Fatalf("after the boot scan judged clean bars the verdict is clear: %+v", v)
	}

	// 3. a store read failure leaves the minute unjudged -> unknown until the
	//    next good judgement.
	h.st.rangeErr = errors.New("database is locked")
	h.bar(ct(2026, 10, 14, 9, 0))
	if v := h.verdict(IntegrityIntradayOpenMin, now.Add(time.Minute)); !v.Blocked || v.Class != IntegrityClassUnknown || !strings.Contains(v.Unknown, "database is locked") {
		t.Fatalf("a failed store read = an unjudged minute = blocked: %+v", v)
	}
	h.st.rangeErr = nil
	h.bar(ct(2026, 10, 14, 9, 1))
	if v := h.verdict(IntegrityIntradayOpenMin, now.Add(2*time.Minute)); v.Blocked {
		t.Fatalf("the next good judgement clears the read failure: %+v", v)
	}
}

// Unarmed (no NT8 bars bridge: crypto, sandbox, a synthetic provider) the gate
// is inert and claims nothing.
func TestIntegrityIsInertWithoutTheNT8Bridge(t *testing.T) {
	t.Cleanup(SetIntegrityArmedForTest(false))
	prev := barHoles.Swap(nil)
	defer barHoles.Store(prev)
	if v := IntegrityVerdictFor("MNQ", IntegrityDeepOpenMin, ct(2026, 10, 14, 9, 0)); v.Armed || v.Blocked {
		t.Fatalf("unarmed must be inert: %+v", v)
	}
	if views, armed := TradingPausedSnapshot(ct(2026, 10, 14, 9, 0)); armed || views != nil {
		t.Fatalf("unarmed health field must be ABSENT: %+v %v", views, armed)
	}
}

// CME break, weekend and a short gap are not holes.
func TestIntegrityCalendarGapsAndShortGapsAreNotHoles(t *testing.T) {
	h := newMemHarness(t)
	// Thu 10-15 → across the 16:00–17:00 break and the Fri→Sun weekend.
	h.run(ct(2026, 10, 15, 14, 0), ct(2026, 10, 16, 15, 59)) // Thu 14:00 .. Fri 15:59 (break inside)
	h.run(ct(2026, 10, 18, 17, 0), ct(2026, 10, 18, 18, 0))  // Sun 17:00 reopen (weekend closed between)
	now := ct(2026, 10, 18, 18, 1)
	if v := h.verdict(IntegrityDeepOpenMin, now); v.Blocked {
		t.Fatalf("the daily break and the weekend are CLOSED minutes, not holes: %+v\n%s", v, h.say.dump())
	}
	// two missing minutes: under the 3-minute threshold, not a hole
	h.bar(ct(2026, 10, 18, 18, 1))
	h.bar(ct(2026, 10, 18, 18, 4)) // 18:02, 18:03 missing
	if v := h.verdict(IntegrityDeepOpenMin, ct(2026, 10, 18, 18, 5)); v.Blocked {
		t.Fatalf("a 2-minute gap is under BARS_HOLE_MIN_GAP_MIN (3): %+v", v)
	}
	// three missing minutes: a hole
	h.bar(ct(2026, 10, 18, 18, 8)) // 18:05, 18:06, 18:07 missing
	if v := h.verdict(IntegrityDeepOpenMin, ct(2026, 10, 18, 18, 9)); !v.Blocked || len(v.Holes) != 1 || v.Holes[0].Missing != 3 {
		t.Fatalf("three missing open minutes ARE a hole: %+v", v)
	}
}

// The 17:00 CT reopen lag: NT8 delivered the first bar ten minutes late. Until a
// bar arrives there is no tracked hole (the stale-feed leg owns that stretch, in
// the trader package); once the first late bar lands the lag is a 10-minute hole
// and entries stay blocked until the bars arrive — and it does not block
// FOREVER: it ages out of the window.
func TestReopenLagBecomesAHoleThatAgesOut(t *testing.T) {
	h := newMemHarness(t)
	h.run(ct(2026, 10, 14, 14, 0), ct(2026, 10, 14, 15, 59)) // Wed afternoon, then the break
	// 17:00..17:09 never arrive; the first live bar is 17:10
	if v := h.verdict(IntegrityIntradayOpenMin, ct(2026, 10, 14, 17, 5)); v.Class == IntegrityClassHole {
		t.Fatalf("at the live edge (no later bar yet) there is no TRACKED hole — the stale leg owns it: %+v", v)
	}
	h.bar(ct(2026, 10, 14, 17, 10))
	v := h.verdict(IntegrityIntradayOpenMin, ct(2026, 10, 14, 17, 11))
	if !v.Blocked || len(v.Holes) != 1 || v.Holes[0].Window != "10-14 17:00–17:10 CT" || v.Holes[0].Missing != 10 {
		t.Fatalf("the lagged reopen must read as the 10-minute hole 17:00–17:10: %+v", v)
	}
	// the data never comes: 1500 open minutes later the stretch has left the window
	h.run(ct(2026, 10, 14, 17, 11), ct(2026, 10, 15, 15, 59))
	h.run(ct(2026, 10, 15, 17, 0), ct(2026, 10, 15, 23, 59))
	if got := h.verdict(IntegrityIntradayOpenMin, ct(2026, 10, 16, 0, 5)); got.Blocked {
		t.Fatalf("an unfillable stretch must age out of the intraday window, not block forever: %+v", got)
	}
	if got := h.verdict(IntegrityDeepOpenMin, ct(2026, 10, 16, 0, 5)); !got.Blocked {
		t.Fatalf("but it is still inside the deep window: %+v", got)
	}
}

// ── REAL DATA: the 2026-10-26 hole, exported read-only from data/data.db ───
//
// MNQ 12-26 1m, 10-26 06:00 → 10-28 06:00 CT, exactly the rows BarsBetweenOn
// reads (source not mixed / off-scale), as consecutive-minute RUNS. The runs
// show NT8's 17:00–21:56 CT silence on 10-26 (297 open minutes, never refilled
// — the store still has the hole today). The replay feeds the runs through
// observeLive minute by minute at the clock of each bar close — the live path —
// and asks IntegrityVerdictFor every minute. (The stale-feed leg is added here
// the way the trader package adds it: kernel.StaleEntryGateFeed on the newest
// stored bar.)

type runsFixture struct {
	Contract string     `json:"contract"`
	Runs     [][2]int64 `json:"runs"`
}

func loadRuns(t *testing.T) []int64 {
	t.Helper()
	raw, err := os.ReadFile("testdata/mnq_1m_2026-10-26_27_runs.json")
	if err != nil {
		t.Fatal(err)
	}
	var f runsFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	var ts []int64
	for _, r := range f.Runs {
		for i := int64(0); i < r[1]; i++ {
			ts = append(ts, r[0]+i*60_000)
		}
	}
	return ts
}

func TestRealData_1026_ReconnectHoleBlocksEntriesUntilItAgesOut(t *testing.T) {
	ts := loadRuns(t)
	h := newMemHarness(t)

	var staleBlocked, holeBlocked []time.Time
	newest := int64(0)
	next := 0
	// walk every wall minute from 10-26 06:00 to 10-28 05:59 CT
	for m := ct(2026, 10, 26, 6, 0); m.Before(ct(2026, 10, 28, 6, 0)); m = m.Add(time.Minute) {
		// the bar that OPENED at m-1m has just closed at m
		for next < len(ts) && ts[next] <= m.Add(-time.Minute).UnixMilli() {
			h.bar(time.UnixMilli(ts[next]))
			newest = ts[next]
			next++
		}
		h.clk = m
		if next == 0 || !kernel.IsCMEOpen(m) {
			continue // nothing delivered yet / market closed
		}
		v := h.verdict(IntegrityIntradayOpenMin, m)
		stale := newest > 0 && kernel.StaleEntryGateFeed(newest, m)
		switch {
		case v.Blocked && v.Class == IntegrityClassHole:
			holeBlocked = append(holeBlocked, m)
		case stale:
			staleBlocked = append(staleBlocked, m)
		case v.Blocked:
			t.Fatalf("unexpected unknown/other block at %s: %+v", kernel.TableTimeCT(m), v)
		}
	}
	if len(holeBlocked) == 0 {
		t.Fatal("the 10-26 reconnect hole must BLOCK entries once the first bar after it lands")
	}
	firstHole, lastHole := holeBlocked[0], holeBlocked[len(holeBlocked)-1]
	t.Logf("stale-feed blocked %d min (%s .. %s); HOLE blocked %d min (%s .. %s)",
		len(staleBlocked), kernel.TableTimeCT(staleBlocked[0]), kernel.TableTimeCT(staleBlocked[len(staleBlocked)-1]),
		len(holeBlocked), kernel.TableTimeCT(firstHole), kernel.TableTimeCT(lastHole))

	// the hole's first minute after the reconnect bar (21:57 open) is judged at 21:58
	if want := ct(2026, 10, 26, 21, 58); !firstHole.Equal(want) {
		t.Fatalf("the hole gate must engage at the reconnect bar's close %s, got %s", kernel.TableTimeCT(want), kernel.TableTimeCT(firstHole))
	}
	// 17:00..21:57 the feed itself was silent: the stale leg covers it from
	// ~17:02 (75 s after the last bar's close)
	if len(staleBlocked) == 0 || staleBlocked[0].After(ct(2026, 10, 26, 17, 3)) {
		t.Fatalf("stale leg must cover the silent evening from ~17:02: %v", staleBlocked)
	}
	// no minute of the reconnect-to-age-out span traded: continuous block
	for m := firstHole; !m.After(lastHole); m = m.Add(time.Minute) {
		if kernel.IsCMEOpen(m) && !containsT(holeBlocked, m) {
			t.Fatalf("entries were ALLOWED at %s inside the reconnect hole's data window", kernel.TableTimeCT(m))
		}
	}
	// it ages out: the intraday window is 1500 OPEN minutes, so the last blocked
	// minute is the one whose window still starts before the hole's end (21:57).
	if lastHole.Before(ct(2026, 10, 27, 23, 0)) || lastHole.After(ct(2026, 10, 28, 0, 30)) {
		t.Fatalf("the hole must age out of the 1500-open-minute window ≈ 23:57 CT on 10-27, last blocked %s", kernel.TableTimeCT(lastHole))
	}
	// and it did NOT block the clean morning before it
	for m := ct(2026, 10, 26, 8, 30); m.Before(ct(2026, 10, 26, 15, 0)); m = m.Add(time.Minute) {
		if containsT(holeBlocked, m) || containsT(staleBlocked, m) {
			t.Fatalf("false pause on the clean 10-26 session at %s", kernel.TableTimeCT(m))
		}
	}
}

func containsT(ts []time.Time, m time.Time) bool {
	for _, x := range ts {
		if x.Equal(m) {
			return true
		}
	}
	return false
}

// lagWire is a holeWire whose cache the test controls: the detector is judged
// against what the CACHE (the series the decisions read) already holds.
type lagWire struct{ cache *ntwire.BarCache }

func (w *lagWire) IsConnected() bool                            { return true }
func (w *lagWire) IsFeedConnected() bool                        { return true }
func (w *lagWire) RequestGapBackfillAt(string, time.Time) error { return nil }
func (w *lagWire) BarCache() *ntwire.BarCache                   { return w.cache }
func (w *lagWire) CurrentContract(string) (ntwire.ContractFact, bool) {
	return ntwire.ContractFact{}, false
}
func (w *lagWire) BarsBackAsked() int { return 0 }

// The persister writes and judges closed bars asynchronously. A decision made
// while the cache is already N bars past what the detector judged could rest on
// a hole not yet seen: the detector is UNKNOWN until it catches up.
func TestIntegrityDetectorLaggingBehindTheCacheIsUnknown(t *testing.T) {
	h := newMemHarness(t)
	cache := ntwire.NewBarCache(3000)
	h.tr.wire = func() holeWire { return &lagWire{cache: cache} }
	h.run(ct(2026, 10, 14, 8, 0), ct(2026, 10, 14, 8, 59)) // judged through the 08:59 bar

	finals := func(from, to time.Time) {
		var bars []ntwire.Bar
		for m := from; !m.After(to); m = m.Add(time.Minute) {
			b := wireBar(m, 30700)
			b.Final = true
			bars = append(bars, b)
		}
		cache.Upsert("MNQ", "1m", bars)
	}
	now := ct(2026, 10, 14, 9, 6)
	finals(ct(2026, 10, 14, 8, 0), ct(2026, 10, 14, 9, 0)) // the cache is one bar ahead: the persister is mid-flight
	if v := h.verdict(IntegrityIntradayOpenMin, now); v.Blocked {
		t.Fatalf("one bar of persister lag is normal: %+v", v)
	}
	finals(ct(2026, 10, 14, 9, 1), ct(2026, 10, 14, 9, 5)) // five closed bars the detector never judged
	v := h.verdict(IntegrityIntradayOpenMin, now)
	if !v.Blocked || v.Class != IntegrityClassUnknown || !strings.Contains(v.Unknown, "behind the cache") {
		t.Fatalf("a detector 5 closed bars behind the cache must be UNKNOWN (blocked): %+v", v)
	}
	// it catches up
	h.run(ct(2026, 10, 14, 9, 0), ct(2026, 10, 14, 9, 5))
	if v := h.verdict(IntegrityIntradayOpenMin, now); v.Blocked {
		t.Fatalf("caught up: clear: %+v", v)
	}
}

// ── REAL CLEAN DAYS: no false pause ────────────────────────────────────────
//
// INTEGRITY_REAL_DAYS=/tmp/realday-1021:/tmp/realday-1022:/tmp/realday-1023
// go test ./trader/ninjatrader/ -run TestRealCleanDaysNeverPause -v
//
// Each dir is a REAL_DAY_REPLAY export (mnq_realday.json.gz, 1m bars read
// read-only from the live DB). Every bar from 08-26 to the day's end is fed
// through observeLive at its close; then, for EVERY wall minute of the last
// ~30 hours with CME open, the verdict is asked over BOTH windows. Clean days
// (only the daily break, weekends and the Labor Day early close missing) must
// never pause: zero blocked minutes.

func loadRealDayBars(t *testing.T, dir string) []int64 {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "mnq_realday.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Bars map[string][][]json.RawMessage `json:"bars"`
	}
	if err := json.NewDecoder(zr).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	var ts []int64
	for _, row := range doc.Bars["1m"] {
		var ot int64
		if err := json.Unmarshal(row[0], &ot); err != nil {
			t.Fatal(err)
		}
		ts = append(ts, ot)
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })
	return ts
}

func TestRealCleanDaysNeverPause(t *testing.T) {
	dirs := strings.Split(os.Getenv("INTEGRITY_REAL_DAYS"), ":")
	if len(dirs) == 0 || dirs[0] == "" {
		t.Skip("INTEGRITY_REAL_DAYS not set (REAL_DAY_REPLAY exports)")
	}
	for _, dir := range dirs {
		ts := loadRealDayBars(t, dir)
		h := newMemHarness(t)
		last := time.UnixMilli(ts[len(ts)-1])
		evalFrom := last.Add(-30 * time.Hour)
		asked, blocked := 0, 0
		next := 0
		for next < len(ts) {
			h.bar(time.UnixMilli(ts[next]))
			closeAt := time.UnixMilli(ts[next]).Add(time.Minute)
			next++
			if closeAt.Before(evalFrom) {
				continue
			}
			if !kernel.IsCMEOpen(closeAt) {
				continue
			}
			for _, win := range []int{IntegrityIntradayOpenMin, IntegrityDeepOpenMin, IntegritySwingOpenMin} {
				// the plain verdict AND the mentor one (round 2: the key-level leg
				// must not pause a clean day either)
				for _, v := range []IntegrityVerdict{h.verdict(win, closeAt), IntegrityVerdictForMentor("MNQ", win, closeAt)} {
					asked++
					if v.Blocked {
						blocked++
						t.Errorf("%s: FALSE PAUSE at %s (window %d): %+v", dir, kernel.TableTimeCT(closeAt), win, v)
						if blocked > 5 {
							t.FailNow()
						}
					}
				}
			}
		}
		t.Logf("%s: %d bars fed (%s .. %s), %d verdicts asked over both windows, %d blocked", dir, len(ts),
			kernel.TableTimeCT(time.UnixMilli(ts[0])), kernel.TableTimeCT(last), asked, blocked)
		if asked == 0 {
			t.Fatalf("%s: nothing was asked", dir)
		}
	}
}

// /api/health trading_paused: scope + windows + reasons, READ from the tracker.
func TestTradingPausedSnapshotNamesScopeAndWindow(t *testing.T) {
	armIntegrity(t)
	f := newGHFixture(t)
	f.openHole()
	// two days later the hole is outside the intraday window, inside the deep one
	later := ct(2026, 10, 16, 10, 0)
	views, armed := TradingPausedSnapshot(later)
	if !armed || len(views) != 1 || views[0].Symbol != "MNQ" {
		t.Fatalf("one MNQ view: %+v armed=%v", views, armed)
	}
	v := views[0]
	if !v.Paused || v.Scope != "swing_and_deep_entries" || len(v.Reasons) != 1 || v.Reasons[0].Class != IntegrityClassHole ||
		len(v.Reasons[0].Window) != 1 || v.Reasons[0].Window[0] != "10-14 03:44–07:48 CT" || v.Action != IntegrityAction {
		t.Fatalf("deep-only scope with the CT window and the action: %+v", v)
	}
	// right after the reconnect the intraday window holds it too: all entries
	views, _ = TradingPausedSnapshot(f.clk.Now())
	if len(views) != 1 || views[0].Scope != "all_entries" {
		t.Fatalf("a hole inside the intraday window pauses ALL entries: %+v", views)
	}
}

// RESTART MID-EPISODE: nothing "all clear" is persisted. A process that boots
// while the hole is still in the store re-derives it from the BOOT SCAN of the
// first live bar and is blocked at once; a boot AFTER the hole's window has
// aged out of the intraday window is clear.
func TestRestartMidEpisodeRederivesTheHoleFromTheStore(t *testing.T) {
	ts := loadRuns(t)
	armIntegrity(t)
	st := &memHoleStore{}
	bootAt := ct(2026, 10, 27, 9, 0) // the process starts Tuesday 09:00 CT; the hole is the 10-26 evening
	for _, ms := range ts {
		if ms < bootAt.UnixMilli() {
			st.add(ms) // everything the store already holds
		}
	}
	tr := newBarHoleTracker(st, func() *ntwire.TCPServer { return nil })
	tr.say = (&lineSink{}).say
	tr.clock = func() time.Time { return bootAt }
	prev := barHoles.Swap(tr)
	t.Cleanup(func() { barHoles.Store(prev) })

	if v := IntegrityVerdictFor("MNQ", IntegrityIntradayOpenMin, bootAt); !v.Blocked || v.Class != IntegrityClassUnknown {
		t.Fatalf("freshly booted, nothing judged: UNKNOWN is blocked: %+v", v)
	}
	// the first live bar after the boot: 09:00 open, closing at 09:01
	st.add(bootAt.UnixMilli())
	tr.observeLive("MNQ", "1m", ghContract, []store.BarHistoryDB{{Symbol: "MNQ", TF: "1m", Contract: ghContract, OpenTimeMs: bootAt.UnixMilli()}})
	v := IntegrityVerdictFor("MNQ", IntegrityIntradayOpenMin, bootAt.Add(time.Minute))
	if !v.Blocked || v.Class != IntegrityClassHole || len(v.Holes) != 1 || v.Holes[0].Window != "10-26 17:00–21:57 CT" || v.Holes[0].Missing != 297 {
		t.Fatalf("the boot scan must re-derive the 10-26 hole from the store (nothing persisted): %+v", v)
	}
}

// ── review round 1: D2 (boot scan reach) + the spent hole + the coverage belt ─

// bootFromStore boots a FRESH tracker at bootAt over a store that already holds
// every real 10-26/27 bar before bootAt (what the previous process left), feeds
// it the one live bar a boot sees, and returns the store and tracker.
func bootFromStore(t *testing.T, bootAt time.Time) (*memHoleStore, *barHoleTracker) {
	t.Helper()
	armIntegrity(t)
	st := &memHoleStore{}
	for _, ms := range loadRuns(t) {
		if ms < bootAt.UnixMilli() {
			st.add(ms)
		}
	}
	tr := newBarHoleTracker(st, func() *ntwire.TCPServer { return nil })
	tr.say = (&lineSink{}).say
	tr.clock = func() time.Time { return bootAt }
	prev := barHoles.Swap(tr)
	t.Cleanup(func() { barHoles.Store(prev) })
	st.add(bootAt.UnixMilli())
	tr.observeLive("MNQ", "1m", ghContract, []store.BarHistoryDB{{Symbol: "MNQ", TF: "1m", Contract: ghContract, OpenTimeMs: bootAt.UnixMilli()}})
	return st, tr
}

// A restart AFTER the session roll must still see the 10-26 17:00–21:57 hole the
// store holds: the boot scan reaches the deep window's start, not just the
// current session day's open. Before the fix a boot at 10-28 06:00 read
// blocked=false in the deep window (hole ~4 days inside it) and the AI /
// planner / Picture / agent / SWING4H paths traded over it.
func TestRestartAfterTheSessionRollStillSeesAnOlderHole(t *testing.T) {
	cases := []struct {
		boot       time.Time
		win        int
		wantBlock  bool
		wantWindow string
	}{
		// 10-27 17:30: the hole is ~19.5 h back — inside the 1500-open-minute intraday window
		{ct(2026, 10, 27, 17, 30), IntegrityIntradayOpenMin, true, "10-26 17:00–21:57 CT"},
		{ct(2026, 10, 27, 17, 30), IntegrityDeepOpenMin, true, "10-26 17:00–21:57 CT"},
		// 10-28 06:00: aged out of the intraday window (correct), still inside the deep one
		{ct(2026, 10, 28, 6, 0), IntegrityIntradayOpenMin, false, ""},
		{ct(2026, 10, 28, 6, 0), IntegrityDeepOpenMin, true, "10-26 17:00–21:57 CT"},
	}
	for _, c := range cases {
		_, _ = bootFromStore(t, c.boot)
		v := IntegrityVerdictFor("MNQ", c.win, c.boot.Add(time.Minute))
		if v.Blocked != c.wantBlock {
			t.Fatalf("boot %s window %d: blocked=%v want %v (%+v)", kernel.TableTimeCT(c.boot), c.win, v.Blocked, c.wantBlock, v)
		}
		if c.wantBlock {
			if v.Class != IntegrityClassHole || len(v.Holes) != 1 || v.Holes[0].Window != c.wantWindow {
				t.Fatalf("boot %s window %d: want the 10-26 hole named %q, got %+v", kernel.TableTimeCT(c.boot), c.win, c.wantWindow, v)
			}
		}
		if v.CoveredFromMs == 0 || v.CoveredFromMs > v.WindowFromMs {
			t.Fatalf("boot %s window %d: the scan must cover the whole window (covered %d, window from %d)", kernel.TableTimeCT(c.boot), c.win, v.CoveredFromMs, v.WindowFromMs)
		}
	}
}

// The belt under the boot scan: a window that reaches back further than the
// detector has judged is UNKNOWN (blocked), never "no holes".
func TestAWindowOlderThanTheDetectorsCoverageIsUnknown(t *testing.T) {
	boot := ct(2026, 10, 28, 6, 0)
	_, tr := bootFromStore(t, boot)
	// as if the scan had only reached the session open (the pre-fix reach): it
	// judged that stretch only, so it holds no hole for the older one
	tr.mu.Lock()
	tr.holes = nil
	for k := range tr.coveredFrom {
		tr.coveredFrom[k] = kernelSessionOpenMs(boot)
	}
	tr.mu.Unlock()
	v := IntegrityVerdictFor("MNQ", IntegrityDeepOpenMin, boot.Add(time.Minute))
	if !v.Blocked || v.Class != IntegrityClassUnknown || !strings.Contains(v.Unknown, "unjudged") {
		t.Fatalf("a window older than the coverage must be UNKNOWN-blocked, got %+v", v)
	}
	// control: the intraday window lies inside that coverage and reads clear
	if v := IntegrityVerdictFor("MNQ", 200, boot.Add(time.Minute)); v.Blocked {
		t.Fatalf("control: a window inside the coverage must not block: %+v", v)
	}
}

func kernelSessionOpenMs(at time.Time) int64 { return kernel.CMESessionDayStart(at).UnixMilli() }

// A hole whose slow re-requests are spent used to be measured never again, so a
// history reload that refilled it never lifted the pause. The tracker now
// re-reads it from the store (no wire send), at most once a minute.
func TestSpentHoleIsRemeasuredAndResumes(t *testing.T) {
	h := newMemHarness(t)
	h.run(ct(2026, 10, 27, 8, 30), ct(2026, 10, 27, 9, 0))
	h.run(ct(2026, 10, 27, 9, 11), ct(2026, 10, 27, 9, 20)) // 10 minutes missing 09:01–09:10
	now := ct(2026, 10, 27, 9, 21)
	if v := h.verdict(IntegrityIntradayOpenMin, now); !v.Blocked || len(v.Holes) != 1 {
		t.Fatalf("fixture: want the one hole blocking: %+v", v)
	}
	// the state after 4 scheduled + 8 slow re-requests
	h.tr.mu.Lock()
	hole := h.tr.holes[0]
	hole.State, hole.Attempts, hole.slowCount = holeStateUnfilled, 12, barHoleSlowCapTotal
	hole.saidSlowCap = true
	hole.nextAttemptAt, hole.measureAt = time.Time{}, time.Time{}
	h.tr.mu.Unlock()
	// still missing: a tick leaves it blocking and says so
	h.tr.tick(now.Add(2 * time.Minute))
	if v := h.verdict(IntegrityIntradayOpenMin, now.Add(2*time.Minute)); !v.Blocked || len(v.Holes) != 1 || !v.Holes[0].Unfillable {
		t.Fatalf("an unrefilled spent hole keeps blocking: %+v", v)
	}
	// the owner reloads NT8 history; the minutes reach the store
	for m := ct(2026, 10, 27, 9, 1); m.Before(ct(2026, 10, 27, 9, 11)); m = m.Add(time.Minute) {
		h.st.mu.Lock()
		h.st.times = append(h.st.times, m.UnixMilli())
		sort.Slice(h.st.times, func(i, j int) bool { return h.st.times[i] < h.st.times[j] })
		h.st.mu.Unlock()
	}
	h.tr.tick(now.Add(4 * time.Minute)) // the tracker's own tick, the one production runs
	if v := h.verdict(IntegrityIntradayOpenMin, now.Add(4*time.Minute)); v.Blocked {
		t.Fatalf("the store holds every minute again — the gate must RESUME on that measurement: %+v", v)
	}
	if h.say.count("bars hole FILLED") != 1 {
		t.Fatalf("the fill is said once:\n%s", h.say.dump())
	}
}

// SWING4H reads the 102-candle warm-up history (kernel/mentor.FourHEMA34Warmup),
// far past the 35-candle window of the AI / planner / Picture / agent paths.
// A hole ~11 trading days old is outside the deep window (clear) and inside the
// swing window (blocked), after a restart: the boot scan reaches it, the health
// scope says swing-only.
func TestSwingWindowSeesAHoleTheDeepWindowDoesNot(t *testing.T) {
	armIntegrity(t)
	boot := ct(2026, 10, 28, 6, 0)
	st := &memHoleStore{}
	gapFrom, gapTo := ct(2026, 10, 13, 4, 0), ct(2026, 10, 13, 4, 30) // overnight: no RTH minute, so the mentor key-level leg stays out of this pin
	for m := ct(2026, 10, 8, 17, 0); m.Before(boot); m = m.Add(time.Minute) {
		if barHoleExpected(m) && (m.Before(gapFrom) || !m.Before(gapTo)) {
			st.add(m.UnixMilli())
		}
	}
	tr := newBarHoleTracker(st, func() *ntwire.TCPServer { return nil })
	tr.say = (&lineSink{}).say
	tr.clock = func() time.Time { return boot }
	prev := barHoles.Swap(tr)
	t.Cleanup(func() { barHoles.Store(prev) })
	st.add(boot.UnixMilli())
	tr.observeLive("MNQ", "1m", ghContract, []store.BarHistoryDB{{Symbol: "MNQ", TF: "1m", Contract: ghContract, OpenTimeMs: boot.UnixMilli()}})

	now := boot.Add(time.Minute)
	if v := IntegrityVerdictFor("MNQ", IntegrityDeepOpenMin, now); v.Blocked {
		t.Fatalf("a hole ~11 trading days back is outside the deep window: %+v", v)
	}
	v := IntegrityVerdictFor("MNQ", IntegritySwingOpenMin, now)
	if !v.Blocked || v.Class != IntegrityClassHole || len(v.Holes) != 1 || v.Holes[0].Window != "10-13 04:00–04:30 CT" {
		t.Fatalf("the swing window must see the 10-13 hole after a restart: %+v", v)
	}
	views, armed := TradingPausedSnapshot(now)
	if !armed || len(views) != 1 || !views[0].Paused || views[0].Scope != "swing_entries" {
		t.Fatalf("health must say swing_entries only: %+v", views)
	}
}
