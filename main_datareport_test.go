package main

import (
	"path/filepath"
	"testing"
	"time"

	"vl/internal/datareport"
	"vl/kernel"
	"vl/store"
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
)

// m448 PRODUCTION WIRING PIN: dataReportSources is the one place the nightly
// report is bound to live counters. This drives the real binding (not a fake
// Sources) and checks each reader answers from the thing it names, and says
// "not measured" when that thing is not wired.
func TestDataReportSourcesBindToTheLiveCounters(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	defer datareport.Register(datareport.CounterBadPrints, nil)
	t.Setenv("VL_RESTORE_TEST_DIR", filepath.Join(t.TempDir(), "rt"))

	boot := time.Date(2026, 10, 29, 6, 0, 0, 0, kernel.CTLocation())
	src := dataReportSources(st, boot)

	if src.DB == nil || !src.BootAt.Equal(boot) {
		t.Fatalf("db/boot not bound: %+v", src)
	}
	if want := filepath.Base(src.RestoreDir); want != "rt" {
		t.Fatalf("VL_RESTORE_TEST_DIR not honoured: %q", src.RestoreDir)
	}

	// No tracker wired -> hole counts are NOT measured (never a zero).
	if _, ok := src.HoleCounts(); ok {
		t.Fatal("hole counts reported while no tracker is wired")
	}
	restore := ntTrader.SetBarHolesWiredForTest()
	defer restore()
	telemetry.IncBarsHoleDetected()
	if hc, ok := src.HoleCounts(); !ok || hc.Detected < 1 {
		t.Fatalf("wired tracker: %+v ok=%v", hc, ok)
	}

	if _, _, ok := src.IngestDrops(); !ok {
		t.Fatal("ingest drop totals are always readable")
	}
	if _, _, ok := src.ClockSteps(); !ok {
		t.Fatal("clock step counters are always readable")
	}

	// Gate table: unset day -> not measured; after a rollover + a block -> the table.
	telemetry.ResetGateBlocksForTest()
	if _, _, ok := src.Gates(); ok {
		t.Fatal("gate table reported before any session day is adopted")
	}
	day := kernel.CMESessionDayStart(time.Now()).UnixMilli()
	telemetry.RolloverGateBlocks(day)
	telemetry.IncGateBlock("t1", "wired_gate")
	gotDay, table, ok := src.Gates()
	if !ok || gotDay != day || table["t1"]["wired_gate"] != 1 {
		t.Fatalf("gates: day=%d ok=%v table=%v", gotDay, ok, table)
	}
	telemetry.ResetGateBlocksForTest()

	// Bad prints is registered (the stale-print quarantine); the two counters the
	// data-truth lanes add are NOT, so they stay n/a until the integrator wires them.
	rep := datareport.Collect(src, datareport.SessionFor(time.Date(2026, 10, 28, 16, 10, 0, 0, kernel.CTLocation())))
	if !rep.BadPrints.OK {
		t.Fatal("bad prints must be wired to the stale-print quarantine counter")
	}
	if rep.CrossFrame.OK || rep.PausedMin.OK {
		t.Fatalf("cross-frame / paused minutes must read n/a until their counters exist: %+v %+v", rep.CrossFrame, rep.PausedMin)
	}
}
