package main

import (
	"os"
	"path/filepath"
	"time"

	"vl/internal/datareport"
	"vl/kernel"
	"vl/manager"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
)

// m448 (release #54): the nightly data report. The report package is pure and
// takes its readers by injection; this file is the ONE place they are bound to
// the live system, so every figure the owner reads on his phone is a counter
// the bot already keeps (counters record, never infer):
//
//	bar holes found/filled/unfilled  telemetry.BarsHoleCountsSnapshot (only while the tracker is wired)
//	bad prints                       ntwire.StalePrintQuarantined (registered below)
//	ingest drops final/historical    ntwire.IngestDropTotals (cumulative twins of the per-minute-swapped counters)
//	clock steps                      kernel.ClockStepSnapshot
//	refusal classes                  telemetry.GateBlockSnapshot (per session day)
//	why no trade (m469)              each running trader's WhyNoTradeInput (mentor funnel copy;
//	                                 AI-planner traders are read from decision_records)
//
// cross-frame mismatches and paused minutes are NOT registered here: the
// data-truth lanes add those counters, and until the integrator calls
// datareport.Register for them the report prints n/a for them.

// restoreTestDir is where deploy/backup-restore-test.sh leaves its result files.
func restoreTestDir() string {
	if d := os.Getenv("VL_RESTORE_TEST_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "" // unknown: the report prints n/a, it never guesses a path
	}
	return filepath.Join(home, "vl-backups", "restore-test")
}

// dataReportSources binds the report's readers to the live system.
func dataReportSources(st *store.Store, bootAt time.Time) datareport.Sources {
	datareport.Register(datareport.CounterBadPrints, func() (int64, bool) {
		return ntwire.StalePrintQuarantined(), true
	})
	return datareport.Sources{
		DB:     st.GormDB(),
		BootAt: bootAt,
		HoleCounts: func() (datareport.HoleCounts, bool) {
			if _, wired := ntTrader.BarHolesSnapshot(); !wired {
				return datareport.HoleCounts{}, false // no tracker: nothing is known
			}
			c := telemetry.BarsHoleCountsSnapshot()
			return datareport.HoleCounts{Detected: c.Detected, Filled: c.Filled, Unfilled: c.Unfilled}, true
		},
		IngestDrops: func() (int64, int64, bool) {
			f, h := ntwire.IngestDropTotals()
			return f, h, true
		},
		ClockSteps: func() (int64, int64, bool) {
			v := kernel.ClockStepSnapshot()
			return v.StepsBackward, v.StepsForward, true
		},
		Gates: func() (int64, map[string]map[string]int, bool) {
			day, table := telemetry.GateBlockSnapshot()
			return day, table, day != 0
		},
		RestoreDir: restoreTestDir(),
	}
}

// whyNoTradeSource reads every trader this process runs for the why-no-trade
// lines (m469): a copy of each mentor funnel, and the ids of the AI-planner
// traders whose cycles the report reads from decision_records. Friends run
// their own process and get no report, so this is the owner's traders only.
func whyNoTradeSource(tm *manager.TraderManager) func() []datareport.TraderFunnel {
	if tm == nil {
		return nil // not wired: the report prints n/a
	}
	return func() []datareport.TraderFunnel {
		var out []datareport.TraderFunnel
		for _, at := range tm.GetAllTraders() {
			out = append(out, at.WhyNoTradeInput())
		}
		return out
	}
}

// startNightlyDataReport starts the once-per-session-day report scheduler.
func startNightlyDataReport(dbPath string, st *store.Store, tm *manager.TraderManager) {
	src := dataReportSources(st, time.Now())
	src.Funnel = whyNoTradeSource(tm)
	datareport.Start(datareport.Config{
		Sources:   src,
		MarkerDir: filepath.Join(filepath.Dir(dbPath), "datareport"),
	})
}
