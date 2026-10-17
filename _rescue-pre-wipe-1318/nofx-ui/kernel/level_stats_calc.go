package kernel

import (
	"math"

	"nofx/market"
)

// B4 — LEVEL_STATS outcome evaluation (forward-validation substrate, Pack B
// owner override 2026-08-26). Pure + deterministic: the nightly job feeds this
// function the previous session-day's 1m bars and every level the session's
// plans seated; the resulting rows are the forward-validation table whose
// 2-week verdict re-weights the volume family.

// Outcome constants (spec): a level is TOUCHED when price trades within
// ±4 points of it; REACTED when, within 3 bars of a touch, price moves ≥8
// points away; BROKE-CLEAN when it closes ≥8 points through and stays beyond;
// CHOPPED when it is touched ≥3 times without any clean move.
const (
	LevelTouchTolPoints = 4.0
	LevelReactPoints    = 8.0
	LevelReactBars      = 3
	LevelChopTouches    = 3
	LevelBreakLookback  = 12 // bars after the first touch to judge broke-clean
)

// LevelOutcome is the per-level, per-session verdict.
type LevelOutcome struct {
	Touched    bool `json:"touched"`     // price traded within ±4pts at least once
	Reacted    bool `json:"reacted"`     // ≥8pt move away within 3 bars of a touch
	BrokeClean bool `json:"broke_clean"` // closed ≥8pts through and stayed beyond
	Chopped    bool `json:"chopped"`     // ≥3 touches, no clean move either way
}

// EvaluateLevelOutcome computes the spec outcome for one level over one
// session's closed 1m bars (ascending by time). The first bar that trades
// within ±touchTol of the level counts as the first touch; the remaining
// verdicts are measured from there. Untouched levels get all-false.
func EvaluateLevelOutcome(bars []market.Kline, levelPrice, touchTol, reactPts float64) LevelOutcome {
	o := LevelOutcome{}
	if levelPrice <= 0 {
		return o
	}
	if touchTol <= 0 {
		touchTol = LevelTouchTolPoints
	}
	if reactPts <= 0 {
		reactPts = LevelReactPoints
	}
	touchAt := func(b market.Kline) bool {
		return b.Low <= levelPrice+touchTol && b.High >= levelPrice-touchTol
	}
	first := -1
	touches := 0
	for i := range bars {
		if touchAt(bars[i]) {
			if first < 0 {
				first = i
			}
			touches++
		}
	}
	if first < 0 {
		return o
	}
	o.Touched = true
	// Reacted: ≥reactPts move away from the level within LevelReactBars of the
	// first touch (either side; measured on closes).
	for i := first + 1; i <= first+LevelReactBars && i < len(bars); i++ {
		if math.Abs(bars[i].Close-levelPrice) >= reactPts {
			o.Reacted = true
			break
		}
	}
	// Broke-clean: closes ≥reactPts beyond the level at some point and NEVER
	// trades back within touchTol afterwards (lookback window).
	cleanUp := false
	cleanDn := false
	broke := false
	for i := first + 1; i < len(bars); i++ {
		if !broke {
			if bars[i].Close >= levelPrice+reactPts {
				broke, cleanUp = true, true
			} else if bars[i].Close <= levelPrice-reactPts {
				broke, cleanDn = true, true
			}
			continue
		}
		// After the break: any return within touchTol voids the clean break.
		if touchAt(bars[i]) {
			cleanUp, cleanDn = false, false
		}
		if i-first > LevelBreakLookback {
			break
		}
	}
	o.BrokeClean = cleanUp || cleanDn
	// Chopped: ≥3 touches and no clean break in either direction.
	o.Chopped = touches >= LevelChopTouches && !o.BrokeClean
	return o
}

// GradeOutcomeBuckets is the report-facing aggregation helper: given a set of
// (grade, outcome) pairs it returns per-grade touched/reacted/broke counts.
func GradeOutcomeBuckets(pairs []struct {
	Grade string
	Out   LevelOutcome
}) map[string]LevelOutcome {
	out := map[string]LevelOutcome{}
	for _, p := range pairs {
		cur := out[p.Grade]
		if p.Out.Touched {
			cur.Touched = true
		}
		if p.Out.Reacted {
			cur.Reacted = true
		}
		if p.Out.BrokeClean {
			cur.BrokeClean = true
		}
		if p.Out.Chopped {
			cur.Chopped = true
		}
		out[p.Grade] = cur
	}
	return out
}
