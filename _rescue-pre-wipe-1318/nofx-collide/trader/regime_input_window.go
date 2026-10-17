package trader

import (
	"fmt"
	"time"

	"nofx/kernel"
	"nofx/market"
)

// ── THE REGIME INPUT'S WINDOW ───────────────────────────────────────────────
// (owner ruling, 2026-09-09 18:18 CT, wave BARS HORIZON)
//
//	"RULING on D3: the regime input MAY change. Rehydrating the ring from the
//	 store changes what RVBaseline is fed — and what it is fed today is 41 hours
//	 labelled as 20 days. A regime value computed on a shorter window than its
//	 name is the defect; correcting the window is not a scope violation, it is
//	 the fix. A31 forbids changing the RULE, not correcting the INPUT the rule
//	 was promised."
//
// FOUR CONDITIONS CAME WITH IT. (a) the boot rehydrate touches 1m ONLY — see
// trader/ninjatrader/bar_persist_wire.go; (b) THE 5m ASK IS SERVED FROM THE
// REHYDRATED 1m TAIL and renamed to what it actually receives — this file;
// (c) the boot line reports the served window IN DAYS, before and after —
// RegimeInputWindowBootLine below; (d) an E7-style golden proves the rule did
// NOT move on a tape whose window does not change — TestRegimeLabelUnchanged…
// in regime_input_window_test.go.
//
// WHY THE 1m TAIL AND NOT THE STORED 5m ROWS. Measured 2026-09-09 against
// data/data.db (read-only), MNQ, kernel.RVBaselineFrom5mDays(·, 20, 5):
//
//	stored 5m, newest 2000 rows  → rv 0.876371 over  7 complete session-days
//	stored 5m, newest 2500 rows  → rv 0.884746 over  9 complete session-days
//	1m tape 12000 → agg 5m 2400  → rv 0.893543 over  9 complete session-days
//
// The last two cover the SAME nine session-days and disagree by 0.9%: NT8's
// stored 5m rows do not agree with their own 1m constituents, which is exactly
// why store/bar_history.go's migration once deleted every non-1m row. So the
// deeper window comes from the 1m bars — the feed's own closed bars, the tape
// every other series is aggregated FROM — and never from a 5m aggregate.
//
// A10/A24: this NEVER blanks the baseline. If the 1m tape cannot support a
// baseline (fewer than kernel's minimum complete session-days), the 5m ring
// read that served it before this wave is used instead, and the boot line and
// the per-read log say which arm answered. Nothing here gates or refuses.

// rvBaselineMinDays is the estimator's minimum complete session-days. Named
// once, here, so the selection below and the call cannot drift (A11).
const rvBaselineMinDays = 5

// RVBaselineTape is the resolved answer to "what window is the realized-vol
// baseline actually computed over, and where did it come from".
//
// Days is 0 ONLY when OK is false — there is then no baseline to qualify and a
// reader must not treat the 0 as "zero days of a real baseline" (A24).
type RVBaselineTape struct {
	Baseline float64
	Days     int
	OK       bool
	Source   string // "1m-tail-agg5m" | "5m-ring-fallback" | "none"
	Rows5m   int    // the 5m rows the estimator was handed
}

// Line renders the tape for a log. An uncomputed baseline says UNKNOWN, never 0.
func (r RVBaselineTape) Line() string {
	if !r.OK {
		return fmt.Sprintf("window=UNKNOWN (warming: %d 5m rows via %s did not reach %d complete session-days)",
			r.Rows5m, r.Source, rvBaselineMinDays)
	}
	return fmt.Sprintf("window=%d complete session-days · baseline=%.6f · %d 5m rows via %s",
		r.Days, r.Baseline, r.Rows5m, r.Source)
}

// ResolveRVBaselineTape is THE selection, extracted so a pin drives IT rather
// than a copy of it (class 86). bars1m is the store-deepened 1m tape; ring5m is
// the pre-wave 5m ring read, used only when the 1m tape cannot answer.
func ResolveRVBaselineTape(bars1m, ring5m []market.Kline, maxDays int) RVBaselineTape {
	agg := kernel.AggregateBars(bars1m, 5*60*1000)
	if v, d, ok := kernel.RVBaselineFrom5mDays(agg, maxDays, rvBaselineMinDays); ok {
		return RVBaselineTape{Baseline: v, Days: d, OK: true, Source: "1m-tail-agg5m", Rows5m: len(agg)}
	}
	// FALLBACK — exactly the pre-wave input, so a thin 1m tape can never turn
	// a working baseline OFF. This is a degradation, not a gate (A10).
	if v, d, ok := kernel.RVBaselineFrom5mDays(ring5m, maxDays, rvBaselineMinDays); ok {
		return RVBaselineTape{Baseline: v, Days: d, OK: true, Source: "5m-ring-fallback", Rows5m: len(ring5m)}
	}
	return RVBaselineTape{Source: "none", Rows5m: len(agg)}
}

// RegimeInputWindowBootLine is owner condition (c): the regime input's served
// window IN DAYS, BEFORE and AFTER, printed once at boot after the rehydrate
// has landed, so the change this wave makes is visible and dated.
//
// EVERY FIELD IS RESOLVED (A11): BEFORE is the 5m ring read that fed the
// baseline before this wave, measured HERE at boot; AFTER is the tape the
// planner will actually use, measured through the SAME production selection
// (ResolveRVBaselineTape) the read will run. Neither number is a literal, and
// an uncomputed window prints UNKNOWN rather than 0.
func RegimeInputWindowBootLine(bars1m, ring5m []market.Kline, maxDays int, now time.Time) string {
	before := RVBaselineTape{Source: "5m-ring (pre-wave)", Rows5m: len(ring5m)}
	if v, d, ok := kernel.RVBaselineFrom5mDays(ring5m, maxDays, rvBaselineMinDays); ok {
		before = RVBaselineTape{Baseline: v, Days: d, OK: true, Source: "5m-ring (pre-wave)", Rows5m: len(ring5m)}
	}
	after := ResolveRVBaselineTape(bars1m, ring5m, maxDays)
	delta := "UNKNOWN"
	if before.OK && after.OK {
		delta = fmt.Sprintf("%+d day(s)", after.Days-before.Days)
	}
	return fmt.Sprintf(
		"📈 regime input window @%s: BEFORE %s · AFTER %s · Δ%s · cap=%d days (rule UNCHANGED — same estimator, deeper input; owner ruling 2026-09-09)",
		kernel.ClockCTSeconds(now), before.Line(), after.Line(), delta, maxDays)
}
