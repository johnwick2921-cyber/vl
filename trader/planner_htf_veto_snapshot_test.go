package trader

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// ── planner HTF veto snapshot (2026-10-16, owner "fix all now") ─────────────
//
// [A] /home/hoang/vl/data/vl_2026-10-15.log: 136 × "🛡️ HTF veto (cross)
// SKIPPED — no 1h+4h structure snapshot this cycle; entry proceeds
// (fail-open)." Every one of the 68 pairs sits immediately before a "🎯 zone at
// write" line: the planner's write-time feasibility mirror of the arm chain
// (write_time_feasibility.go) called armGateVerdictFor with a literal nil
// snapshot, at the zone's far AND near edge. The arm pass itself was fed (the
// two real refusals that day are arm-path lines with both TFs), so the cost
// was a plan published with an arm the executor would refuse: 10:11:55 NY v4
// S1 long written with the veto skipped → 10:32:59 "arm REFUSED NY S1 leg 1:
// HTF veto … long vs 1h TRENDING_DOWN + 4h TRENDING_DOWN (cross-check)".
//
// These tests run on the REAL 10-14 tape and plan docs (touchArmTrader), under
// the LIVE mode HTF_VETO_MODE=cross, at the production call sites the planner
// write path calls (writeTimeZoneVerdicts / writeTimeFeasibilityVerdicts, the
// two calls at auto_trader_planner.go's write-time feasibility block) and the
// production live-bar arm pass (armedEventPassAt). Neither side's snapshot is
// built by the test: each call site builds its own.
//
// Tape facts (kernel.StructureSnapshotForVeto on the touch fixtures, asserted
// below where a test rests on them): 1h TRENDING_DOWN from 06:00; native 4h
// RANGING until the 05:00–09:00 bar closes, TRENDING_DOWN from 09:00; 1h
// RANGING 04:00–05:30.

// vetoWriteClock pins the write-time clock seam (traderNow) to the harness
// clock, so the write site and the bar provider agree on "now".
func vetoWriteClock(t *testing.T, clock *time.Time, now time.Time) {
	t.Helper()
	*clock = now
	testNow = func() time.Time { return *clock }
	t.Cleanup(func() { testNow = nil })
}

// vetoTrendsAt reads what the production builder sees at `now` — a
// precondition guard, so no test below passes on a tape that changed under it.
func vetoTrendsAt(t *testing.T, at *AutoTrader, now time.Time) (h1, h4 string) {
	t.Helper()
	snap := at.armVetoSnapshot(now)
	if st, ok := snap["1h"]; ok {
		h1 = st.VetoTrend()
	}
	if st, ok := snap["4h"]; ok {
		h4 = st.VetoTrend()
	}
	return h1, h4
}

// legacyArmDoc returns the fixture doc with its arm policy cleared, so the
// non-zone write-time path (writeTimeFeasibilityVerdicts) judges it. A legacy
// arm may not carry "hold" (not in the legacy armable set) and a legacy
// "reject" takes the frozen-map geometry path (no map in these fixtures), so
// the condition becomes "sweep_reclaim" — legacy-armable via the docs' own
// wait_confirm + confirm{}. Same side, prices and levels; only the path changes.
func legacyArmDoc(doc kernel.PlanDoc) *kernel.PlanDoc {
	d := doc
	d.Scenarios = append([]kernel.PlanScenario(nil), doc.Scenarios...)
	for i := range d.Scenarios {
		if d.Scenarios[i].Arm != nil {
			arm := *d.Scenarios[i].Arm
			arm.Policy = ""
			d.Scenarios[i].Arm = &arm
			d.Scenarios[i].Condition = "sweep_reclaim"
		}
	}
	return &d
}

// noIssues fails unless the verdict admitted every arm — a "no veto" claim
// is vacuous if an earlier gate refused the leg before the veto ran.
func noIssues(t *testing.T, path string, issues []writeTimeFeasibilityIssue) {
	t.Helper()
	if len(issues) != 0 {
		t.Fatalf("%s: the arm must pass the WHOLE write-time chain (the veto included): %+v", path, issues)
	}
}

func vetoIssue(issues []writeTimeFeasibilityIssue) (writeTimeFeasibilityIssue, bool) {
	for _, is := range issues {
		if is.Class == "veto" {
			return is, true
		}
	}
	return writeTimeFeasibilityIssue{}, false
}

// (a) — the NY v2 long (zone 30509–30517) against 1h + 4h TRENDING_DOWN at
// 09:05:05 10-14 is REFUSED AT WRITE, on both write-time paths, with the
// veto's own words, and the veto is never skipped. RED before the fix: the
// nil snapshot made the write site log SKIPPED and pass the arm.
func TestPlannerWriteVetoRefusesCounterTrendArm(t *testing.T) {
	t.Setenv("HTF_VETO_MODE", "cross")
	at, _, plan, clock := touchArmTrader(t, "ny_v2")
	now := touchCT(t, 2026, 10, 14, 9, 5, 5, 0)
	vetoWriteClock(t, clock, now)
	if h1, h4 := vetoTrendsAt(t, at, now); h1 != "TRENDING_DOWN" || h4 != "TRENDING_DOWN" {
		t.Fatalf("precondition: the 10-14 tape at 09:05:05 must read 1h+4h TRENDING_DOWN, got 1h=%q 4h=%q", h1, h4)
	}
	logBuf := captureTraderLog(t)
	const want = "HTF veto: htf_veto: long vs 1h TRENDING_DOWN + 4h TRENDING_DOWN (cross-check)"

	doc := plan.Doc
	zone := at.writeTimeZoneVerdicts(&doc, 8, at.config.StrategyConfig, "NY")
	is, ok := vetoIssue(zone)
	if !ok || !strings.Contains(is.Verbose, want) || is.Scenario != "S1" {
		t.Fatalf("zone path: the counter-trend long must be refused at write by the HTF veto (%q): issues=%+v\nlog:\n%s", want, zone, logBuf.String())
	}
	legacy := at.writeTimeFeasibilityVerdicts(legacyArmDoc(plan.Doc), 8, at.config.StrategyConfig, "NY")
	is, ok = vetoIssue(legacy)
	if !ok || !strings.Contains(is.Verbose, want) {
		t.Fatalf("legacy path: the counter-trend long must be refused at write by the HTF veto: issues=%+v\nlog:\n%s", legacy, logBuf.String())
	}
	if strings.Contains(logBuf.String(), "HTF veto (cross) SKIPPED") {
		t.Fatalf("the write site had the bars — the veto must judge, never skip:\n%s", logBuf.String())
	}
	if hint := writeTimeFeasibilityHint(zone); !strings.Contains(hint, "for an HTF veto: author the side the higher-timeframe trend allows") {
		t.Fatalf("the repair hint must name the veto's fix: %q", hint)
	}
}

// (a) at the ARM call site — the production live-bar pass (armedEventPassAt,
// which builds its own snapshot) refuses the same long. This path was already
// fed before the fix (it is the 10-15 "arm REFUSED … HTF veto" path); it is
// pinned here so the shared builder can never regress it.
func TestPlannerArmPassVetoRefusesCounterTrendArm(t *testing.T) {
	t.Setenv("HTF_VETO_MODE", "cross")
	at, st, plan, clock := touchArmTrader(t, "ny_v2")
	logBuf := captureTraderLog(t)
	eventPassReady(t, at, st, plan)
	*clock = touchCT(t, 2026, 10, 14, 9, 5, 5, 0)
	if !at.armedEventPassAt(*clock) {
		t.Fatalf("the live-bar pass did not run:\n%s", logBuf.String())
	}
	if n := nonTerminalArms(t, at, st); n != 0 {
		t.Fatalf("the long must be VETOED at arm: rows=%d\nlog:\n%s", n, logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "HTF veto: htf_veto: long vs 1h TRENDING_DOWN + 4h TRENDING_DOWN (cross-check)") ||
		strings.Contains(logBuf.String(), "HTF veto (cross) SKIPPED") {
		t.Fatalf("the arm refusal must be the veto, judged (not skipped):\n%s", logBuf.String())
	}
}

// (b) — a WITH-trend arm passes: the NY v3 short reject (zone 30456.5–30466)
// under the same 1h + 4h TRENDING_DOWN, at write and at arm.
func TestPlannerVetoPassesWithTrendArm(t *testing.T) {
	t.Setenv("HTF_VETO_MODE", "cross")
	at, st, plan, clock := touchArmTrader(t, "ny_v3")
	now := touchCT(t, 2026, 10, 14, 9, 51, 5, 0)
	vetoWriteClock(t, clock, now)
	if h1, h4 := vetoTrendsAt(t, at, now); h1 != "TRENDING_DOWN" || h4 != "TRENDING_DOWN" {
		t.Fatalf("precondition: 1h+4h TRENDING_DOWN at 09:51:05, got 1h=%q 4h=%q", h1, h4)
	}
	logBuf := captureTraderLog(t)
	doc := plan.Doc
	noIssues(t, "zone path, with-trend short", at.writeTimeZoneVerdicts(&doc, 8, at.config.StrategyConfig, "NY"))
	noIssues(t, "legacy path, with-trend short", at.writeTimeFeasibilityVerdicts(legacyArmDoc(plan.Doc), 8, at.config.StrategyConfig, "NY"))
	eventPassReady(t, at, st, plan)
	if !at.armedEventPassAt(now) {
		t.Fatalf("the live-bar pass did not run:\n%s", logBuf.String())
	}
	if n := nonTerminalArms(t, at, st); n != 1 {
		t.Fatalf("the with-trend short must ARM at 09:51:05 (the 09:50 rejection from below): rows=%d\nlog:\n%s", n, logBuf.String())
	}
	if strings.Contains(logBuf.String(), "htf_veto:") || strings.Contains(logBuf.String(), "HTF veto (cross) SKIPPED") {
		t.Fatalf("no veto refusal and no skip on a with-trend arm:\n%s", logBuf.String())
	}
}

// (c) — cross needs BOTH TFs against the side. A RANGING 1h passes the long
// even under a 4h TRENDING_DOWN; and a TRENDING_DOWN 1h passes it while the 4h
// is still RANGING (08:30, the real tape). The first case mixes the tape: 1m
// as of 05:00 (1h RANGING) with the native 4h as of 09:05 (TRENDING_DOWN).
func TestPlannerWriteVetoCrossPassesOneTFAgainst(t *testing.T) {
	t.Setenv("HTF_VETO_MODE", "cross")

	t.Run("1h RANGING + 4h TRENDING_DOWN", func(t *testing.T) {
		at, _, plan, clock := touchArmTrader(t, "ny_v2")
		now := touchCT(t, 2026, 10, 14, 9, 5, 5, 0)
		vetoWriteClock(t, clock, now)
		h1Cut := touchCT(t, 2026, 10, 14, 5, 0, 5, 0).UnixMilli()
		tapeAt := market.FuturesBarsProvider
		market.FuturesBarsProvider = func(sym, tf string, n int) []market.Kline {
			out := tapeAt(sym, tf, n)
			if tf != "1m" {
				return out
			}
			kept := out[:0:0]
			for _, b := range out {
				if b.CloseTime < h1Cut {
					kept = append(kept, b)
				}
			}
			return kept
		}
		if h1, h4 := vetoTrendsAt(t, at, now); h1 != "RANGING" || h4 != "TRENDING_DOWN" {
			t.Fatalf("precondition: 1h RANGING + 4h TRENDING_DOWN, got 1h=%q 4h=%q", h1, h4)
		}
		logBuf := captureTraderLog(t)
		doc := plan.Doc
		noIssues(t, "a RANGING 1h in cross mode", at.writeTimeZoneVerdicts(&doc, 8, at.config.StrategyConfig, "NY"))
		if strings.Contains(logBuf.String(), "SKIPPED") {
			t.Fatalf("both TFs were present — the veto judged, it must not say SKIPPED:\n%s", logBuf.String())
		}
	})

	t.Run("1h TRENDING_DOWN + 4h RANGING", func(t *testing.T) {
		at, _, plan, clock := touchArmTrader(t, "ny_v2")
		now := touchCT(t, 2026, 10, 14, 8, 30, 5, 0)
		vetoWriteClock(t, clock, now)
		if h1, h4 := vetoTrendsAt(t, at, now); h1 != "TRENDING_DOWN" || h4 != "RANGING" {
			t.Fatalf("precondition: 1h TRENDING_DOWN + 4h RANGING at 08:30:05, got 1h=%q 4h=%q", h1, h4)
		}
		doc := plan.Doc
		noIssues(t, "cross on the 1h alone", at.writeTimeZoneVerdicts(&doc, 8, at.config.StrategyConfig, "NY"))
	})
}

// (d) — a GENUINELY unavailable detector (no bar cache at all) still fails
// open, LOUDLY: no veto issue, and the WARN names both missing TFs. And a cache
// that serves 1m but no 4h fails open on the 4h alone, also loudly.
func TestPlannerWriteVetoUnavailableDetectorFailsOpenLoudly(t *testing.T) {
	t.Setenv("HTF_VETO_MODE", "cross")

	t.Run("no bar cache", func(t *testing.T) {
		at, _, plan, clock := touchArmTrader(t, "ny_v2")
		vetoWriteClock(t, clock, touchCT(t, 2026, 10, 14, 9, 5, 5, 0))
		market.FuturesBarsProvider = nil // touchArmTrader's cleanup restores it
		logBuf := captureTraderLog(t)
		doc := plan.Doc
		noIssues(t, "no detector input → fail open, never a fabricated veto", at.writeTimeZoneVerdicts(&doc, 8, at.config.StrategyConfig, "NY"))
		if !strings.Contains(logBuf.String(), "🛡️ HTF veto (cross) SKIPPED — no 1h+4h structure snapshot this cycle; entry proceeds (fail-open).") {
			t.Fatalf("the fail-open must be LOUD:\n%s", logBuf.String())
		}
	})

	t.Run("no native 4h", func(t *testing.T) {
		at, _, plan, clock := touchArmTrader(t, "ny_v2")
		vetoWriteClock(t, clock, touchCT(t, 2026, 10, 14, 9, 5, 5, 0))
		tapeAt := market.FuturesBarsProvider
		market.FuturesBarsProvider = func(sym, tf string, n int) []market.Kline {
			if tf == "4h" {
				return nil
			}
			return tapeAt(sym, tf, n)
		}
		logBuf := captureTraderLog(t)
		doc := plan.Doc
		noIssues(t, "no 4h → cross fails open for the 4h", at.writeTimeZoneVerdicts(&doc, 8, at.config.StrategyConfig, "NY"))
		if !strings.Contains(logBuf.String(), "🛡️ HTF veto (cross) SKIPPED — no 4h structure snapshot this cycle") {
			t.Fatalf("the 4h fail-open must name the missing TF:\n%s", logBuf.String())
		}
	})
}

// The ratchet behind the mutant: no PRODUCTION call that reaches the HTF veto
// may pass a literal nil snapshot. A nil there is not "detector unavailable"
// — it is a caller that did not pass the data, and the veto cannot tell the
// two apart. Reverting any write-time site to nil turns this RED (as well as
// TestPlannerWriteVetoRefusesCounterTrendArm).
func TestNoProductionVetoCallPassesNilSnapshot(t *testing.T) {
	// callee → index of its snapshot argument.
	snapArg := map[string]int{
		"armGateVerdictFor":            3,
		"armGateVerdict":               2,
		"zoneAwareGateVerdict":         4,
		"zoneLegVerdict":               4,
		"maybeManageArmedOrders":       0,
		"maybeManageArmedOrdersAt":     0,
		"maybeManageArmedOrdersAtOpts": 0,
		"HTFVetoVerdict":               0,
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	calls := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := ce.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			idx, ok := snapArg[sel.Sel.Name]
			if !ok || idx >= len(ce.Args) {
				return true
			}
			calls++
			if id, ok := ce.Args[idx].(*ast.Ident); ok && id.Name == "nil" {
				t.Errorf("%s: %s(...) passes a literal nil snapshot — build it with at.armVetoSnapshot (veto_snapshot.go)", fset.Position(ce.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
	if calls < 8 {
		t.Fatalf("found only %d veto-reaching calls — the scan no longer sees the call sites it guards", calls)
	}
}

// eventPassReady satisfies the live-bar pass's own preconditions (running, an
// NT8 account bound, a zone arm on the active plan) without touching its
// snapshot: the pass builds that itself.
func eventPassReady(t *testing.T, at *AutoTrader, st *store.Store, plan *kernel.ActivePlan) {
	t.Helper()
	if err := st.Trader().Create(&store.Trader{ID: at.id, UserID: "u", Name: at.id, AIModelID: "m",
		ExchangeID: "ex", StrategyID: "s", Account: "Sim101", IsRunning: true}); err != nil {
		t.Fatal(err)
	}
	at.isRunningMutex.Lock()
	at.isRunning = true
	at.isRunningMutex.Unlock()
	at.noteZoneArmActive(plan)
}

func nonTerminalArms(t *testing.T, at *AutoTrader, st *store.Store) int {
	t.Helper()
	rows, err := st.ArmedOrders().ListNonTerminal(at.id)
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}
