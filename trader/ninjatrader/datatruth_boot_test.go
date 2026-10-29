package ninjatrader

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// Release #55 (integrator): the boot wiring of the m446 checks. The pins drive
// the PRODUCTION function (WireDataTruthChecks, the one bar_persist_wire.go calls
// the moment the wire server exists) and the production store reader.
//
// MUTANTS (RED in the report):
//   B1  WireDataTruthChecks never calls ntwire.WireDataTruth       -> registration pin RED
//   B2  storeSwingInputs reads the wrong timeframe                  -> store-reader pin RED

func dtStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "dt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestWireDataTruthChecksRegistersAllEightAndSignalsReady(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	t.Cleanup(SetDataTruthReadyForTest(false))
	dataTruthWiring.Store(false)
	t.Cleanup(func() { dataTruthWiring.Store(false) })

	st := dtStore(t)
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	srv := ntwire.NewTCPServer(nil)

	// nil inputs register nothing and do NOT signal ready (the trader-side stubs keep blocking)
	WireDataTruthChecks(nil, srv)
	WireDataTruthChecks(bh, nil)
	if DataTruthReady() || len(datatruth.Snapshot()) != 0 {
		t.Fatalf("a missing input must register nothing and stay not-ready: ready=%v checks=%v", DataTruthReady(), datatruth.Snapshot())
	}

	WireDataTruthChecks(bh, srv)
	if !DataTruthReady() {
		t.Fatal("the wiring must signal ready once the checks are registered")
	}
	snap := datatruth.Snapshot()
	for _, n := range []string{checks.CheckBadPrint, checks.CheckCrossFrame, checks.CheckLookback, checks.CheckLookbackSwing4h,
		checks.CheckLookbackLevels, checks.CheckLookbackSession, checks.CheckIngestLoss, checks.CheckContractRoll} {
		if _, ok := snap[n]; !ok {
			t.Fatalf("check %q is not registered after WireDataTruthChecks (have %v)", n, snap)
		}
	}
	// every check is a REAL one: none still says "required ... not wired"
	_, reasons := datatruth.Evaluate("MNQ", time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC))
	for _, r := range reasons {
		if strings.Contains(r.Detail, "is not wired") {
			t.Fatalf("%s is still a not-wired stub after the wiring: %s", r.Check, r.Detail)
		}
	}
	// a second call is a no-op (once-guard)
	WireDataTruthChecks(bh, srv)
}

func TestStoreSwingInputsReadsTheCurrentContractOneAndFiveMinute(t *testing.T) {
	st := dtStore(t)
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC).UnixMilli()
	var rows []store.BarHistoryDB
	for i := 0; i < 10; i++ {
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: "1m", Contract: "MNQ 12-26", Source: store.BarSourceHistorical,
			OpenTimeMs: base + int64(i)*60_000, O: 100, H: 101, L: 99, C: 100.5 + float64(i), V: 7})
	}
	for i := 0; i < 2; i++ {
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: "5m", Contract: "MNQ 12-26", Source: store.BarSourceHistorical,
			OpenTimeMs: base + int64(i)*300_000, O: 100, H: 101, L: 99, C: 200 + float64(i), V: 35})
	}
	if err := bh.InsertBars(rows); err != nil {
		t.Fatal(err)
	}
	m1, m5, line, err := storeSwingInputs(bh)("MNQ", time.UnixMilli(base))
	if err != nil {
		t.Fatal(err)
	}
	if len(m1) != 10 || len(m5) != 2 || line != 0 {
		t.Fatalf("m1=%d m5=%d line=%v, want 10 / 2 / 0 (the evaluator line is deliberately not coupled)", len(m1), len(m5), line)
	}
	if m1[0].T != base || m1[9].C != 109.5 || m5[1].C != 201 || !m1[0].Final {
		t.Fatalf("the store rows must map to checks.Bar in ascending order: %+v %+v", m1[0], m5[1])
	}
	if _, _, _, err := storeSwingInputs(nil)("MNQ", time.Now()); err == nil {
		t.Fatal("no store must be an ERROR (a check that cannot judge fails closed), not an empty read")
	}
}

// ── #55 m449 — the seed edge (ready at WIRE time was before the replay landed) ──
//
// MUTANT S3: rehydrateRingFromStore (the boot entry bar_persist_wire.go calls;
// TestRingRehydrateIsWiredAtBoot census-pins that call site) without
// MarkDataTruthSeeded -> RED. The roll / scale-break reseeds use
// rehydrateRingFromStoreWith and must NOT open the edge.

func TestWireDataTruthChecksIsRegisteredButNotYetJudgeable(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	t.Cleanup(SetDataTruthReadyForTest(false))
	dataTruthWiring.Store(false)
	t.Cleanup(func() { dataTruthWiring.Store(false) })
	st := dtStore(t)
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	srv := ntwire.NewTCPServer(nil)
	WireDataTruthChecks(bh, srv)
	if !DataTruthReady() {
		t.Fatal("registered")
	}
	if DataTruthJudgeable() || DataTruthSeeded() {
		t.Fatal("registration alone must NOT make the checks judgeable: the replay has not landed")
	}
}

func TestBootRehydrateOpensTheSeedEdge(t *testing.T) {
	t.Cleanup(SetDataTruthReadyForTest(true))
	t.Cleanup(SetDataTruthSeededForTest(false))
	st := dtStore(t)
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	srv := ntwire.NewTCPServer(nil)
	if DataTruthJudgeable() {
		t.Fatal("precondition: not judgeable before the seed")
	}
	rehydrateRingFromStoreWith(bh, srv, time.Now(), true) // a roll reseed is not the boot seed
	if DataTruthSeeded() {
		t.Fatal("a roll-driven reseed must not open the boot seed edge")
	}
	rehydrateRingFromStore(bh, srv, time.Now())
	if !DataTruthSeeded() || !DataTruthJudgeable() {
		t.Fatal("the boot seed step must open the edge, however the rehydrate ended")
	}
}
