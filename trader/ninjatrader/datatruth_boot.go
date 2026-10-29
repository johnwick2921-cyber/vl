package ninjatrader

import (
	"fmt"
	"sync/atomic"
	"time"

	"vl/internal/datatruth/checks"
	"vl/logger"
	ntwire "vl/provider/ninjatrader"
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
	dataTruthReady  atomic.Bool
	dataTruthWiring atomic.Bool // the once-guard
)

// DataTruthReady is true once WireDataTruthChecks has registered the checks.
func DataTruthReady() bool { return dataTruthReady.Load() }

// SetDataTruthReadyForTest forces the edge. TEST-ONLY.
func SetDataTruthReadyForTest(v bool) (restore func()) {
	prev := dataTruthReady.Swap(v)
	return func() { dataTruthReady.Store(prev) }
}

// The swing / levels / session checks read the STORE (the 2500-bar ring is
// too shallow for a 34-bucket 4h EMA, review m446 round 1): the current
// contract's newest dataTruthStore1mN 1m rows (about 19 sessions = 117 4h buckets,
// past the 102-candle EMA warm-up) and the matching 5m rows.
const (
	dataTruthStore1mN = 27000
	dataTruthStore5mN = 5400
)

// storeSwingInputs is checks.Config.SwingInputs over the bar history. botLine is
// 0 (unknown): JudgeSwing4h then compares the line built from the 1m with the
// line rebuilt from every frame, both from the SAME store - it measures the hole's
// effect on the swing line without coupling this check to the evaluator's
// in-memory state (a stitched, back-adjusted series that a current-contract
// recompute could disagree with at a roll for reasons that are not data loss).
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
		r1, err := bh.LastNBarsCurrentContract(symbol, "1m", dataTruthStore1mN)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("1m store read: %w", err)
		}
		r5, err := bh.LastNBarsCurrentContract(symbol, "5m", dataTruthStore5mN)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("5m store read: %w", err)
		}
		return conv(r1), conv(r5), 0, nil
	}
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
	dataTruthReady.Store(true) // after the registration, never before
	logger.Infof("🧪 data-truth checks wired: bad_print · cross_frame · indicator_lookback (+swing4h, levels, session) · ingest_loss · contract_roll over the live NT8 ring + the 1m/5m store — NEW entries pause when any cannot be trusted (fail closed); exits, stops and protection never consult them")
}
