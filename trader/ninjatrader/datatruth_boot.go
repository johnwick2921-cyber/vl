package ninjatrader

import (
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"vl/internal/datatruth/checks"
	"vl/logger"
	ntwire "vl/provider/ninjatrader"
	"vl/safe"
	"vl/store"
)

// ── DATA-TRUTH BOOT WIRING (release #55, integrator) ─────────────────────────
//
// m446 built the eight cache/store checks and left the registration to the
// integrator. WireDataTruthChecks is that registration: it runs ONCE, from the
// bar-persistence goroutine the moment the wire server exists (the BarCache the
// checks read is reachable from then on), on the OWNER instance AND on friend
// instances alike (the gate protects a SIM account wherever it trades; the owner
// push is a no-op without a bound chat, so a friend gets the WARN line and the
// /api/health data_truth block).
//
// The trader package (trader/data_truth_gate.go) Requires every name FIRST, so
// until this runs each is a stub that BLOCKS (fail closed); DataTruthReady is the
// edge it uses to start asking Evaluate at all (a boot is not alerted as eight
// "required check not wired" pushes).

var (
	dataTruthReady   atomic.Bool
	dataTruthSeeded  atomic.Bool // the boot replay + store rehydrate have landed in the ring
	dataTruthWiring  atomic.Bool // the once-guard
	dataTruthWarming atomic.Bool // the warmer once-guard (m452)
)

// DataTruthReady is true once WireDataTruthChecks has registered the checks.
func DataTruthReady() bool { return dataTruthReady.Load() }

// DataTruthSeeded is true once the boot seed has LANDED: the AddOn's
// bars_historical replay was waited for and the ring rehydrated from the store
// (rehydrateRingFromStore, the boot entry). #55 m449: the checks are registered a few seconds after
// the wire server exists - BEFORE any of that - so judging at the registration
// edge read an empty, not-yet-arrived ring as lost data: contract_roll,
// indicator_lookback (113 of 113 slots missing) and session eVWAP each opened an
// episode and pushed the owner on EVERY restart (live 10-29 15:35:53: the AddOn
// matched at :55, the ring was rehydrated at 15:36:08). The data was not lost, it
// had not arrived.
func DataTruthSeeded() bool { return dataTruthSeeded.Load() }

// DataTruthJudgeable is the edge the trader-side gate asks: the checks are
// registered AND the ring they read has been seeded. Until then entries wait
// (blocked, fail-closed, quietly - no episode, no push); past the trader's
// boot grace the checks speak anyway, so a replay that never lands is still a
// loud, named pause rather than a silent one.
func DataTruthJudgeable() bool { return dataTruthReady.Load() && dataTruthSeeded.Load() }

// MarkDataTruthSeeded records that the boot seed step has finished, however it
// ended (a replay that never came is judged - loudly - by the checks, not
// waited on forever).
func MarkDataTruthSeeded() { dataTruthSeeded.Store(true) }

// SetDataTruthReadyForTest forces BOTH edges (registered and seeded), so a test
// that drives the checks does not also have to drive the boot sequence; the
// pins for the seed edge use SetDataTruthSeededForTest. TEST-ONLY.
func SetDataTruthReadyForTest(v bool) (restore func()) {
	prev := dataTruthReady.Swap(v)
	prevSeed := dataTruthSeeded.Swap(v)
	return func() { dataTruthReady.Store(prev); dataTruthSeeded.Store(prevSeed) }
}

// SetDataTruthSeededForTest forces only the seed edge. TEST-ONLY.
func SetDataTruthSeededForTest(v bool) (restore func()) {
	prev := dataTruthSeeded.Swap(v)
	return func() { dataTruthSeeded.Store(prev) }
}

// The swing / levels / session checks read the STORE (the 2500-bar ring is
// too shallow for a 34-bucket 4h EMA, review m446 round 1): the current
// contract's newest dataTruthStore1mN 1m rows (about 19 sessions = 117 4h buckets,
// past the 102-candle EMA warm-up) and the matching 5m rows.
const (
	dataTruthStore1mN = 27000
	dataTruthStore5mN = 5400
	// dataTruthStitchReachMs bounds how far before the current contract's first
	// row the stitch looks for the older contract it rolled from.
	dataTruthStitchReachMs = int64(60 * 24 * 3600 * 1000)
)

// storeSwingInputs is checks.Config.SwingInputs over the bar history. botLine is
// 0 (unknown): JudgeSwing4h then compares the line built from the 1m with the
// line rebuilt from every frame, both from the SAME store - it measures the hole's
// effect on the swing line without coupling this check to the evaluator's
// in-memory state.
//
// THE STITCHED SERIES (release #55 review, P2): the evaluator seeds its 4h EMA 34
// from the contract-STITCHED, back-adjusted 1m (trader/mentor_tick.go
// mentorFullKeyLevelHistory: "so every boot sees >=102 closed 4h candles instead of
// the current contract's ~95"). Judging the CURRENT contract's rows alone made
// SWING4H pause for days after every roll although the series the evaluator reads
// was complete: right after a roll the new contract holds only its replay and a
// few sparse one-row-a-day import snapshots, so the check saw "only 14 buckets"
// (09-11), then "8 buckets inside the warm-up exist in no frame" until 10-06 on
// the real 12-26 rows. stitchedBarRows therefore reads the series the way the
// evaluator does: when the current contract does not hold the whole window, the
// older contract supplies every bar up to ITS OWN LAST STORED BAR and the newer
// one every bar strictly after it (the REL10-STITCH-CUT rule), the older bars
// shifted onto the newer price scale by the one seam difference. The shift is a
// level, not a hole: JudgeSwing4h compares two recurrences over the SAME stitched
// closes, so a constant offset cancels, and the one 1m-measured offset is applied
// to the 5m as well so the two frames can never disagree about the seam itself.
func storeSwingInputs(bh *store.BarHistoryStore) func(symbol string, now time.Time) ([]checks.Bar, []checks.Bar, float64, error) {
	conv := func(rows []store.BarHistoryDB) []checks.Bar {
		out := make([]checks.Bar, len(rows))
		for i, r := range rows {
			out[i] = checks.Bar{T: r.OpenTimeMs, O: r.O, H: r.H, L: r.L, C: r.C, V: r.V, Final: true}
		}
		return out
	}
	return func(symbol string, now time.Time) ([]checks.Bar, []checks.Bar, float64, error) {
		if bh == nil {
			return nil, nil, 0, fmt.Errorf("data truth: no bar history store")
		}
		r1, r5, err := stitchedBarRows(bh, symbol)
		if err != nil {
			return nil, nil, 0, err
		}
		return conv(r1), conv(r5), 0, nil
	}
}

// stitchedBarRows returns the 1m and 5m rows the swing / levels / session checks
// judge: the current contract's newest rows, extended backwards over older
// contracts while the window is not full (see storeSwingInputs). A store read
// error is returned (the check fails closed).
func stitchedBarRows(bh *store.BarHistoryStore, symbol string) (r1, r5 []store.BarHistoryDB, err error) {
	r1, err = bh.LastNBarsCurrentContract(symbol, "1m", dataTruthStore1mN)
	if err != nil {
		return nil, nil, fmt.Errorf("1m store read: %w", err)
	}
	r5, err = bh.LastNBarsCurrentContract(symbol, "5m", dataTruthStore5mN)
	if err != nil {
		return nil, nil, fmt.Errorf("5m store read: %w", err)
	}
	if len(r1) == 0 || len(r1) >= dataTruthStore1mN {
		return r1, r5, nil // cold store, or the current contract alone fills the window
	}
	// a time-bounded read (an index range over the last weeks), not the aggregate
	// over the symbol's whole history: this runs once per symbol-minute while a
	// fresh roll leaves the current contract short.
	spans, err := bh.RecentContracts(symbol, "1m", r1[0].OpenTimeMs-dataTruthStitchReachMs)
	if err != nil {
		return nil, nil, fmt.Errorf("1m contract spans: %w", err)
	}
	idx := -1
	for i, sp := range spans {
		if sp.Contract == r1[0].Contract {
			idx = i
		}
	}
	for i := idx - 1; i >= 0 && len(r1) < dataTruthStore1mN; i-- {
		oc := spans[i].Contract
		// a FULL 1m window, like the 5m below - never "N minus what the newer contract
		// already holds" (release #55 round 3, review 3 P2): len(r1) counts the new
		// contract's replay rows that overlap the older's tail, and rowsAfter(r1, last)
		// drops them again, so the stitched 1m came out about len(r1) rows SHORT of the
		// window while the full 5m window below kept its whole reach. The 5m then began
		// earlier than the 1m, JudgeSwing4h saw those early buckets as "N missing from
		// the 1m" (they sit outside the 1m's read window, not lost), repaired the line
		// from the 5m, and the two lines seeded from different buckets: the EMA seed
		// residual read as a disagreement -> SWING4H paused ~2 trading days after every
		// roll with no data lost (simulated 12-26 -> 03-27 roll, 1.42 pt > 1.00 pt).
		old1, err := bh.LastNBarsOn(symbol, "1m", oc, dataTruthStore1mN)
		if err != nil {
			return nil, nil, fmt.Errorf("1m store read (%s): %w", oc, err)
		}
		if len(old1) == 0 {
			continue
		}
		last := old1[len(old1)-1]
		head1 := rowsAfter(r1, last.OpenTimeMs)
		if len(head1) == 0 {
			break // the newer series has nothing after the older's last bar: no seam to measure
		}
		// the seam shift: the newer close at the older's last minute when it stores
		// one, else the newer's first close after it
		anchor := head1[0].C
		for _, b := range r1 {
			if b.OpenTimeMs == last.OpenTimeMs {
				anchor = b.C
				break
			}
		}
		delta := anchor - last.C
		// a FULL window, not "N minus what the current contract already holds": once
		// the current contract's 5m ring is full while its 1m is still short (the
		// 12-26 contract on 10-07 14:10 CT), N-len(r5) is 0, the older 5m comes back
		// empty, and the head5 cut below then drops every newer 5m row at or before
		// the older's last bar - the independent 5m frame loses data that exists in
		// BOTH contracts and SWING4H pauses on a bucket only 5m could have covered
		// (release #55 review round 1, finding 1).
		old5, err := bh.LastNBarsOn(symbol, "5m", oc, dataTruthStore5mN)
		if err != nil {
			return nil, nil, fmt.Errorf("5m store read (%s): %w", oc, err)
		}
		var old5cut []store.BarHistoryDB
		for _, b := range old5 {
			if b.OpenTimeMs <= last.OpenTimeMs {
				old5cut = append(old5cut, b)
			}
		}
		head5 := rowsAfter(r5, last.OpenTimeMs)
		r1 = append(shiftRows(old1, delta), head1...)
		r5 = append(shiftRows(old5cut, delta), head5...)
	}
	// THE REACH INVARIANT (structural, price-path independent): the stitched 5m never
	// begins before the stitched 1m. The 1m is the series the evaluator reads and the
	// series the swing line is built from; a 5m bucket from before its first bar is
	// outside the window the check judges, not data the 1m lost. Holds across any
	// number of stitched contracts (a third, thin one would otherwise reopen it).
	if len(r1) > 0 {
		r5 = rowsFrom(r5, r1[0].OpenTimeMs)
	}
	return r1, r5, nil
}

// rowsFrom is the suffix of the ascending rows at or after ms.
func rowsFrom(rows []store.BarHistoryDB, ms int64) []store.BarHistoryDB {
	i := sort.Search(len(rows), func(i int) bool { return rows[i].OpenTimeMs >= ms })
	return rows[i:]
}

// rowsAfter is the suffix of the ascending rows strictly after ms.
func rowsAfter(rows []store.BarHistoryDB, ms int64) []store.BarHistoryDB {
	i := sort.Search(len(rows), func(i int) bool { return rows[i].OpenTimeMs > ms })
	return rows[i:]
}

// shiftRows returns a copy of rows with every price moved by delta.
func shiftRows(rows []store.BarHistoryDB, delta float64) []store.BarHistoryDB {
	out := make([]store.BarHistoryDB, len(rows))
	for i, r := range rows {
		r.O, r.H, r.L, r.C = r.O+delta, r.H+delta, r.L+delta, r.C+delta
		out[i] = r
	}
	return out
}

// WireDataTruthChecks registers the eight m446 checks over the live BarCache and
// the bar-history store, once. nil server / cache / store: nothing is registered
// and DataTruthReady stays false (the trader-side stubs keep blocking).
func WireDataTruthChecks(bh *store.BarHistoryStore, server *ntwire.TCPServer) {
	if bh == nil || server == nil || server.BarCache() == nil {
		return
	}
	if !dataTruthWiring.CompareAndSwap(false, true) {
		return
	}
	src := &ntwire.DataTruthSources{Server: server, Cache: server.BarCache()}
	ntwire.WireDataTruth(src, storeSwingInputs(bh))
	wireDataTruthRerequest(server) // #55 m449: a dropped final/historical bar is re-asked of NT8, not left to age out
	dataTruthReady.Store(true)     // after the registration, never before
	startDataTruthWarmer()
	logger.Infof("🧪 data-truth checks wired: bad_print · cross_frame · indicator_lookback (+swing4h, levels, session) · ingest_loss · contract_roll over the live NT8 ring + the 1m/5m store — NEW entries pause when any cannot be trusted (fail closed); exits, stops and protection never consult them")
}

// startDataTruthWarmer keeps the swing/levels store read HOT (m452). The read is
// ~100 ms (27000 1m + 5400 5m rows) and is cached per (symbol, wall-clock minute);
// without a warmer the first asker of every minute pays it - and that asker can be
// an entry path, or the armed placement pass holding armedPassMu. The warmer reads
// the minute just after it opens, off every trader lock, on its own goroutine, so
// the entry paths and the 60 s beats find it cached (concurrent readers share one
// read: cachedInputs is single-flight). It only warms symbols the cache has already
// been asked about, acts on nothing and ends with the process.
func startDataTruthWarmer() {
	if !dataTruthWarming.CompareAndSwap(false, true) {
		return
	}
	safe.GoNet("datatruth-warmer", "", func() {
		for {
			now := time.Now()
			time.Sleep(now.Truncate(time.Minute).Add(time.Minute + 1500*time.Millisecond).Sub(now))
			checks.WarmSwingInputs(time.Now())
		}
	})
}
