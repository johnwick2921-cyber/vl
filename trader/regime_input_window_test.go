package trader

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"path/filepath"
	"vl/kernel"
	"vl/market"
)

// ── OWNER-CONDITION PINS, BARS HORIZON D3 (ruling 2026-09-09 18:18 CT) ──────
//
// The ruling allowed the regime INPUT to change and forbade the RULE from
// changing, with four conditions. (a) 1m-only rehydrate and (c) the boot line
// are pinned in trader/ninjatrader; (b) and (d) are pinned here.

// rvSessionTape builds `days` COMPLETE CME session-days of 1m bars, each
// running 17:00 CT → 16:00 CT (23 h, the halt hour omitted), skipping the
// Friday-evening/Saturday non-sessions. Prices oscillate deterministically so
// realized vol is stable and non-zero. `startCT` must be a session open.
func rvSessionTape(startCT time.Time, days int) []market.Kline {
	var out []market.Kline
	d := startCT
	for made := 0; made < days; {
		if wd := d.Weekday(); wd == time.Friday || wd == time.Saturday {
			d = d.AddDate(0, 0, 1)
			continue
		}
		for i := 0; i < 23*60; i++ {
			ts := d.Add(time.Duration(i) * time.Minute)
			o := 29000 + 8*math.Sin(float64(i)/17.0) + float64(made)
			out = append(out, market.Kline{
				OpenTime: ts.UnixMilli(), Open: o, High: o + 3, Low: o - 3, Close: o + 1.5, Volume: 11,
			})
		}
		made++
		d = d.AddDate(0, 0, 1)
	}
	return out
}

// rvLabel renders the ONE clause the model reads, through the production
// estimator + the production regime renderer.
func rvLabel(t *testing.T, tape RVBaselineTape, min5 []market.Kline) string {
	t.Helper()
	r := kernel.ComputeRegime(kernel.RegimeInputs{
		Price: 29000, Min5Bars: min5, RVBaseline: tape.Baseline, RVBaselineDays: tape.Days,
	})
	for _, f := range strings.Split(r.Render(), " · ") {
		if strings.HasPrefix(f, "RV=") {
			return f
		}
	}
	return "NO RV CLAUSE: " + r.Render()
}

// PIN D3-F — OWNER CONDITION (d). THE E7 GOLDEN: ON A TAPE WHOSE WINDOW DOES
// NOT CHANGE, THE REHYDRATE MOVES NOTHING.
//
// THIS IS THE PIN THAT SEPARATES "the input got deeper" FROM "the rule
// changed". The fixture's ring ALREADY serves the ask, so
// barsWithStoreDepthFrom rule 2 refuses the store read outright — exactly the
// state of a ring that the boot rehydrate has already filled to its cap. The
// regime label must then be BYTE-IDENTICAL to the pre-splice label, and equal
// to a written-down golden so a change in the RULE cannot hide behind a change
// in the input.
func TestRegimeLabelUnchangedWhenTheWindowDoesNotChange(t *testing.T) {
	// THE GOLDEN. Captured once from this fixture and written down: any change to
	// the estimator, the aggregation bucket, the completeness filter or the
	// minimum-days rule moves it, which is precisely what condition (d) asks a
	// pin to detect. Proven to fail under three mutations (see the report).
	const goldenRV = "RV=103%-of-baseline(9 complete session-days)"
	now := time.Date(2026, 9, 9, 13, 18, 0, 0, kernel.CTLocation())
	full := rvSessionTape(time.Date(2026, 8, 26, 17, 0, 0, 0, kernel.CTLocation()), 9)
	older := rvSessionTape(time.Date(2026, 8, 17, 17, 0, 0, 0, kernel.CTLocation()), 3)
	min5 := kernel.AggregateBars(full, 5*60*1000)

	ask := len(full) // the ring ALREADY serves the ask
	pre := ResolveRVBaselineTape(full, nil, rvBaselineMaxDays)
	if !pre.OK {
		t.Fatalf("fixture does not produce a baseline: %s", pre.Line())
	}
	preLabel := rvLabel(t, pre, min5)
	if preLabel != goldenRV {
		t.Fatalf("GOLDEN MOVED — the RULE changed, not just the input.\n got: %s\nwant: %s\n(tape: %s)", preLabel, goldenRV, pre.Line())
	}

	// Offer the store 3 further session-days. A full ring must refuse them.
	spliced := barsWithStoreDepthFrom(full, func(int) ([]market.Kline, error) {
		return append(append([]market.Kline{}, older...), full...), nil
	}, "MNQ", "1m", ask, now)
	if len(spliced) != len(full) {
		t.Fatalf("a ring that already serves the ask took %d bars from the store (want 0)", len(spliced)-len(full))
	}
	post := ResolveRVBaselineTape(spliced, nil, rvBaselineMaxDays)
	if postLabel := rvLabel(t, post, min5); postLabel != preLabel {
		t.Fatalf("the rehydrate MOVED the regime label on a tape whose window did not change:\n before: %s\n  after: %s", preLabel, postLabel)
	}
	if post.Days != pre.Days || post.Baseline != pre.Baseline || post.Source != pre.Source {
		t.Fatalf("the resolved tape moved on an unchanged window: %s → %s", pre.Line(), post.Line())
	}
}

// PIN D3-G — OWNER CONDITION (b). THE BASELINE IS SERVED FROM THE 1m TAIL,
// AND THE 1m TAIL IS THE DEEPER WINDOW.
//
// THE DEFECT THIS CATCHES: the first cut deepened the 5m RING from the store
// instead, feeding a LIVE regime input the NT8 5m aggregates this repo has
// already judged inconsistent with their own 1m constituents. Measured against
// data/data.db on 2026-09-09, over the SAME nine session-days: stored 5m gives
// 0.884746, the 1m constituents give 0.893543.
func TestRVBaselineIsServedFromThe1mTail(t *testing.T) {
	long1m := rvSessionTape(time.Date(2026, 8, 26, 17, 0, 0, 0, kernel.CTLocation()), 9)
	// A SHALLOWER 5m ring: six complete session-days, which the estimator can
	// still answer — so a wrong selection would be silent, not a crash.
	ring5m := kernel.AggregateBars(rvSessionTape(time.Date(2026, 9, 1, 17, 0, 0, 0, kernel.CTLocation()), 6), 5*60*1000)

	got := ResolveRVBaselineTape(long1m, ring5m, rvBaselineMaxDays)
	if got.Source != "1m-tail-agg5m" {
		t.Fatalf("the baseline was served from %q, not the 1m tail: %s", got.Source, got.Line())
	}
	ringOnly := ResolveRVBaselineTape(nil, ring5m, rvBaselineMaxDays)
	if !ringOnly.OK {
		t.Fatalf("fixture ring cannot answer — the pin would prove nothing")
	}
	if got.Days <= ringOnly.Days {
		t.Fatalf("the 1m tail did not widen the window: 1m=%d days, 5m ring=%d days", got.Days, ringOnly.Days)
	}
}

// PIN D3-H — A10. THE FALLBACK NEVER BLANKS A WORKING BASELINE.
//
// The 1m tape can be thin (a cold store, a fresh boot, a short retention). When
// it is, the pre-wave 5m ring read must still answer, and the source must SAY
// which arm answered — a degradation, never a gate.
func TestThin1mTapeFallsBackToTheRingAndNeverBlanks(t *testing.T) {
	thin := rvSessionTape(time.Date(2026, 9, 8, 17, 0, 0, 0, kernel.CTLocation()), 1) // 1 day: below minDays
	ring5m := kernel.AggregateBars(rvSessionTape(time.Date(2026, 8, 26, 17, 0, 0, 0, kernel.CTLocation()), 7), 5*60*1000)

	got := ResolveRVBaselineTape(thin, ring5m, rvBaselineMaxDays)
	if !got.OK {
		t.Fatalf("a thin 1m tape TURNED THE BASELINE OFF although the ring could answer: %s", got.Line())
	}
	if got.Source != "5m-ring-fallback" {
		t.Fatalf("the fallback arm did not name itself: %s", got.Line())
	}
	if got.Days == 0 {
		t.Fatalf("OK with 0 days — a plausible zero (A24): %s", got.Line())
	}
	// And the pre-wave value is reproduced EXACTLY by the fallback.
	wantV, wantD, _ := kernel.RVBaselineFrom5mDays(ring5m, rvBaselineMaxDays, rvBaselineMinDays)
	if got.Baseline != wantV || got.Days != wantD {
		t.Fatalf("the fallback is not the pre-wave value: %v/%d vs %v/%d", got.Baseline, got.Days, wantV, wantD)
	}
}

// PIN D3-I — A24. NO BASELINE AT ALL IS UNKNOWN, NEVER 0.
func TestNoBaselineReportsUnknownNotZero(t *testing.T) {
	got := ResolveRVBaselineTape(nil, nil, rvBaselineMaxDays)
	if got.OK {
		t.Fatalf("a baseline was reported from nothing: %s", got.Line())
	}
	if !strings.Contains(got.Line(), "window=UNKNOWN") {
		t.Fatalf("an uncomputed window must print UNKNOWN, never a number: %s", got.Line())
	}
	if strings.Contains(got.Line(), "window=0") {
		t.Fatalf("an uncomputed window rendered as 0: %s", got.Line())
	}
}

// PIN D3-J — OWNER CONDITION (c). THE BOOT LINE REPORTS THE WINDOW IN DAYS,
// BEFORE AND AFTER, WITH RESOLVED VALUES (A11).
func TestRegimeInputWindowBootLineReportsBeforeAndAfterInDays(t *testing.T) {
	now := time.Date(2026, 9, 9, 8, 30, 0, 0, kernel.CTLocation())
	long1m := rvSessionTape(time.Date(2026, 8, 26, 17, 0, 0, 0, kernel.CTLocation()), 9)
	ring5m := kernel.AggregateBars(rvSessionTape(time.Date(2026, 9, 1, 17, 0, 0, 0, kernel.CTLocation()), 6), 5*60*1000)

	line := RegimeInputWindowBootLine(long1m, ring5m, rvBaselineMaxDays, now)
	for _, want := range []string{
		"📈 regime input window @08:30:00 CT",
		"BEFORE window=6 complete session-days",
		"AFTER window=9 complete session-days",
		"Δ+3 day(s)",
		fmt.Sprintf("cap=%d days", rvBaselineMaxDays),
		"rule UNCHANGED",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("the boot line does not carry %q:\n%s", want, line)
		}
	}
	// UNMEASURABLE HALVES SAY SO. A24: never a plausible zero.
	blank := RegimeInputWindowBootLine(nil, nil, rvBaselineMaxDays, now)
	if !strings.Contains(blank, "BEFORE window=UNKNOWN") || !strings.Contains(blank, "AFTER window=UNKNOWN") {
		t.Fatalf("an unmeasurable window must print UNKNOWN on both halves:\n%s", blank)
	}
	if !strings.Contains(blank, "ΔUNKNOWN") {
		t.Fatalf("an undelta-able pair must print ΔUNKNOWN, never Δ+0:\n%s", blank)
	}
}

// PIN D3-K — A29. THE OWNER-CONDITION CODE IS WIRED.
func TestRegimeInputWindowIsWired(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	for fn, wantIn := range map[string]string{
		"ResolveRVBaselineTape(":      "trader/auto_trader_planner.go",
		"RegimeInputWindowBootLine(":  "trader/auto_trader_dayplan.go",
		"rvBaselineFallback5mBarsAsk": "trader/auto_trader_planner.go",
		"rehydrateTimeframe":          "trader/ninjatrader/bar_persist_wire.go",
	} {
		n, where := d2ProdCallSites(t, root, fn)
		if n == 0 {
			t.Errorf("%s: 0 production call sites (A29)", fn)
			continue
		}
		if !strings.Contains(strings.Join(where, " "), wantIn) {
			t.Errorf("%s: production call sites %v, want one in %s", fn, where, wantIn)
		}
	}
	// The pre-ruling name must be GONE, so a stale reader cannot find an ask
	// that no longer describes what the baseline receives.
	if n, where := d2ProdCallSites(t, root, "rvBaseline5mBarsAsk"); n > 0 {
		t.Errorf("the pre-ruling ask name survives in %v — the rename must be complete", where)
	}
}
