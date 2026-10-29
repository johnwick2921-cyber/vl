package ninjatrader

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	"vl/kernel"
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

// rollFixture inserts a contract roll the way the real 09-10 12-26 roll left the
// store: the OLD contract holds dense, perfect 1m + 5m up to its last stored bar
// (the roll day's morning), the NEW contract holds sparse one-row-a-day import
// snapshots before the roll and dense replay after it, on a price scale 100 pt
// higher. Only the first few sessions of the new contract are dense.
func rollFixture(t *testing.T, bh *store.BarHistoryStore, loc *time.Location, hole func(tm time.Time) bool) (now time.Time) {
	t.Helper()
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	open := func(tm time.Time) bool { return kernel.IsCMEOpen(tm) }
	var rows []store.BarHistoryDB
	add := func(contract, tf string, tm time.Time, px float64) {
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: tf, Contract: contract, Source: store.BarSourceHistorical,
			OpenTimeMs: tm.UnixMilli(), O: px, H: px + 1, L: px - 1, C: px, V: 10})
	}
	cut := at("2026-09-10 08:00")
	px := 30000.0
	for tm := at("2026-08-12 17:00"); !tm.After(cut); tm = tm.Add(time.Minute) {
		if !open(tm) {
			continue
		}
		px += 0.25
		if hole != nil && hole(tm) {
			continue // lost in BOTH frames
		}
		add("MNQ 09-26", "1m", tm, px)
		if tm.Minute()%5 == 0 {
			add("MNQ 09-26", "5m", tm, px)
		}
	}
	// the new contract: sparse snapshots (one 1m + one 5m row a day, 12:00 CT) before the roll ...
	for d := at("2026-08-12 12:00"); d.Before(cut); d = d.AddDate(0, 0, 1) {
		if open(d) {
			add("MNQ 12-26", "1m", d, px+100)
			add("MNQ 12-26", "5m", d, px+100)
		}
	}
	// ... dense replay after the roll
	now = at("2026-09-14 12:30")
	for tm := cut.Add(time.Minute); tm.Before(now); tm = tm.Add(time.Minute) {
		if !open(tm) {
			continue
		}
		px += 0.25
		add("MNQ 12-26", "1m", tm, px+100)
		if tm.Minute()%5 == 0 {
			add("MNQ 12-26", "5m", tm, px+100)
		}
	}
	if err := bh.InsertBars(rows); err != nil {
		t.Fatal(err)
	}
	return now
}

// TestSwing4hJudgesTheStitchedSeriesAcrossARoll (release #55 review, P2 + P3) is
// the production-path pin: WireDataTruthChecks + datatruth.Evaluate over a store
// that just rolled. The series the evaluator reads (old contract up to its last
// bar + new contract after it) is complete and perfect, so the SWING4H check must
// NOT pause. Before the fix the check judged the new contract's rows alone ("only
// N closed 4h buckets" / "buckets inside the warm-up exist in no frame") for days.
//
// MUTANT R1: stitchedBarRows returns the current contract only -> RED.
func TestSwing4hJudgesTheStitchedSeriesAcrossARoll(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	t.Cleanup(SetDataTruthReadyForTest(false))
	dataTruthWiring.Store(false)
	t.Cleanup(func() { dataTruthWiring.Store(false) })
	loc := kernel.CTLocation()

	st := dtStore(t)
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := rollFixture(t, bh, loc, nil)
	WireDataTruthChecks(bh, ntwire.NewTCPServer(nil))
	_, reasons := datatruth.Evaluate("MNQ", now)
	for _, r := range reasons {
		if r.Check == checks.CheckLookbackSwing4h {
			t.Fatalf("SWING4H paused on a complete stitched series after a roll: %s", r.Detail)
		}
	}
	// the stitch is the only thing standing between this fixture and a pause: the
	// same store judged on the new contract alone must block (the pre-fix reading).
	m1, m5, _, err := storeSwingInputs(bh)("MNQ", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(m1) < 20000 {
		t.Fatalf("stitched 1m has %d rows - the older contract was not read", len(m1))
	}
	for i := 1; i < len(m1); i++ {
		if m1[i].T <= m1[i-1].T {
			t.Fatalf("stitched 1m not strictly ascending at %d", i)
		}
	}
	// one price scale across the seam: no 100 pt step anywhere in the stitched closes
	for i := 1; i < len(m1); i++ {
		if d := m1[i].C - m1[i-1].C; d > 5 || d < -5 {
			t.Fatalf("seam step %.2f pt at %s: the older contract was not shifted onto the newer scale", d, time.UnixMilli(m1[i].T).UTC())
		}
	}
	_ = m5
}

// TestSwing4hStillPausesOnARealHoleAcrossARoll keeps the stitch honest: a 4h
// stretch of the OLDER contract lost in BOTH frames, inside the warm-up, is a
// real unknowable bucket and must still pause SWING4H (fail closed), stitched or
// not.
func TestSwing4hStillPausesOnARealHoleAcrossARoll(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	t.Cleanup(SetDataTruthReadyForTest(false))
	dataTruthWiring.Store(false)
	t.Cleanup(func() { dataTruthWiring.Store(false) })
	loc := kernel.CTLocation()

	st := dtStore(t)
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 8, 31, 17, 0, 0, 0, loc)
	now := rollFixture(t, bh, loc, func(tm time.Time) bool { return !tm.Before(from) && tm.Before(from.Add(4*time.Hour)) })
	WireDataTruthChecks(bh, ntwire.NewTCPServer(nil))
	_, reasons := datatruth.Evaluate("MNQ", now)
	for _, r := range reasons {
		if r.Check == checks.CheckLookbackSwing4h {
			if !strings.Contains(r.Detail, "exist in no frame") {
				t.Fatalf("paused, but for the wrong reason: %s", r.Detail)
			}
			return
		}
	}
	t.Fatal("a 4h bucket lost in both frames inside the warm-up must still pause SWING4H")
}
