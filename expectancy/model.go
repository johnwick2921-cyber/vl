// Package expectancy is wave 1D: the per-condition expectancy READ MODEL.
//
// It is the table the shadow/promote rulings are made from, and it is a read
// model only — it never gates, sizes, exits, or prompts. Class 23 applies: a
// failure here WARNs and returns an empty table; it may never stop the loop.
//
// Three laws are load-bearing in this package and each one is enforced in code
// rather than in a comment:
//
//   - CORRECTED-COLUMN LAW (A22). Money is read from pnl_corrected ONLY. A NULL
//     is UNRESOLVED: excluded from every figure and COUNTED in Exclusions, never
//     coerced to realized_pnl and never coerced to zero.
//   - SAMPLE-ID LAW (A21). Every cell carries the row ids it was computed from,
//     so any figure in it can be recomputed by a reader who has only the table.
//     This is why every prior money verdict was irreproducible (C4).
//   - NO FABRICATED VALUES. A statistic that cannot be computed is ABSENT (a nil
//     pointer that marshals to null), never a plausible zero. MAE/MFE and the
//     stop/target hit shares stay absent until wave 1A puts rows in
//     trade_excursions.
package expectancy

import "time"

// MinN is the pre-registered sample floor from the Guide (0C). Below it a cell
// is DESCRIPTIVE ONLY: the numbers are still shown, but no verdict is rendered
// from them. Pre-registered means it is not re-chosen after seeing the data.
const MinN = 30

// z is the two-sided 95% normal quantile used for BOTH intervals — the Wilson
// interval on the win rate and the normal interval on the mean. One constant,
// so a cell can never mix confidence levels between its two intervals.
const z = 1.96

// Status values. Computed from (n, mean, mean interval) — never hand-written.
const (
	StatusNotEnoughData = "NOT ENOUGH DATA"
	StatusFails         = "FAILS"
	StatusPasses        = "PASSES"
)

// Era labels. The split is by TIMESTAMP, not by session-day: 0B booted mid-day,
// so one session-day holds trades from both eras (E3).
const (
	EraPre0B  = "pre-0B"
	EraPost0B = "post-0B"
)

// Entry paths.
const (
	PathArmed    = "armed"
	PathDecision = "decision"
)

// TestSeamSource is the ARMED_TEST_SEAM marker. Rows carrying it are seam
// artifacts, not trades, and are excluded and counted. Named here because the
// production writer has no exported constant for it — and because the
// 2026-09-01 audit found this same seam contaminating an unfiltered count in
// store/position_query.go and then again in an ad-hoc adherence query. It is a
// class, not an accident, so the exclusion is part of the model.
const TestSeamSource = "e7_farside_test"

// Era0BStart is the instant the 0B exit-sanity code went LIVE.
//
// RESOLVED, not typed: 0B booted at 2026-09-02 07:49:06 CT, recorded in commit
// 617faae4 ("rollback target is 4175e0b6 — lane 2 booted at 07:49:06 CT
// mid-wave"). The commit instant of 4175e0b6 (07:44:37 CT) is NOT the boundary:
// code that is committed is not code that is running, and trades are graded by
// the binary that took them.
//
// Built from the date+time in the era's own zone, the same way
// store.DayPlanEraStart is, because a UTC literal cannot express a CT wall
// clock. The test asserts the resolved wall-clock fields, so a wrong fallback
// zone is visible rather than latent.
var Era0BStart = era0BStart()

func era0BStart() time.Time {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		loc = time.FixedZone("CDT", -5*60*60)
	}
	return time.Date(2026, 9, 2, 7, 49, 6, 0, loc)
}

// Key is one row of the table (D1). The zero value of any dimension means "this
// cell rolls up over that dimension" — a roll-up never claims a dimension it
// aggregated away.
type Key struct {
	Condition string `json:"condition,omitempty"`
	Session   string `json:"session,omitempty"`
	LevelKind string `json:"level_kind,omitempty"`
	Path      string `json:"path,omitempty"`
	Era       string `json:"era,omitempty"`
}

// Cell is one computed row. Pointer fields are ABSENT when uncomputable.
type Cell struct {
	Key `json:"key"`

	N      int `json:"n"`
	Wins   int `json:"wins"`
	Losses int `json:"losses"`
	Flats  int `json:"flats"`

	SumPnL float64 `json:"sum_pnl_corrected"`
	Mean   float64 `json:"mean"`
	SD     float64 `json:"sd"`

	WinRate  float64 `json:"win_rate"`
	WilsonLo float64 `json:"wilson_lo"`
	WilsonHi float64 `json:"wilson_hi"`

	// MeanLo/MeanHi is the 95% interval on the MEAN — the interval the
	// promotion criterion is judged on. WilsonLo/Hi is the interval on the WIN
	// RATE. They answer different questions and are never interchanged.
	MeanLo float64 `json:"mean_lo"`
	MeanHi float64 `json:"mean_hi"`
	TStat  float64 `json:"t_stat"`

	// Absent until the inputs exist. Never zero-filled.
	AvgRealizedR   *float64 `json:"avg_realized_r"`
	AvgPlannedRR   *float64 `json:"avg_planned_rr"`
	MedianMAE      *float64 `json:"median_mae"`
	MedianMFE      *float64 `json:"median_mfe"`
	StopHitShare   *float64 `json:"stop_hit_share"`
	TargetHitShare *float64 `json:"target_hit_share"`

	// ExcludedUnresolved is how many rows fell in this cell's key but carried a
	// NULL pnl_corrected. Shown beside n so the cell cannot hide a shrinking
	// denominator.
	ExcludedUnresolved int `json:"excluded_unresolved"`

	// RowIDs is the sample-id law made machine-readable (A21).
	RowIDs []int64 `json:"row_ids"`

	Descriptive bool   `json:"descriptive_only"`
	Status      string `json:"status"`
}

// E8Cell is the COUNTERFACTUAL side-table (D4). It is kept in a separate field
// with a separate type precisely so a counterfactual number can never be added
// to a realized one by accident.
type E8Cell struct {
	Key  `json:"key"`
	Rule string `json:"rule,omitempty"`

	// N is every row the cell saw. UsableN is how many of them had a net_pnl
	// this model is willing to arithmetic on. They differ, and both are shown,
	// because a cell that reported only N would let 40 corrupt rows and 92
	// uncomputed zeros hide inside a healthy-looking sample size.
	N       int `json:"n"`
	UsableN int `json:"usable_n"`
	Wins    int `json:"wins"`
	Losses  int `json:"losses"`

	// SumPnL/Mean are ABSENT when no row in the cell is usable. Measured on the
	// live store 2026-09-03: ab_confirm_log.net_pnl held ≈ −(price × multiplier)
	// on 40 of 188 rows (an exit treated as zero) and a bare 0 beside a resolved
	// outcome on another 92. A mean over either is a fabricated number wearing a
	// currency symbol. Reported, not repaired — the writer is out of 1D's scope.
	SumPnL *float64 `json:"sum_net_pnl"`
	Mean   *float64 `json:"mean"`

	// ExcludedPriceScale — |net_pnl| >= entry_px, so the "P&L" is at least the
	// price of the instrument: impossible for a 1–2 lot MNQ trade.
	ExcludedPriceScale int `json:"excluded_price_scale"`
	// ExcludedZeroPnL — a bare 0 beside a resolved outcome: uncomputed, not
	// break-even. The difference is the whole of A24's "plausible zero".
	ExcludedZeroPnL int `json:"excluded_zero_pnl"`

	// Counterfactual is always true. It is a field rather than an implicit
	// property of the type so that a serialized row carries its own warning.
	Counterfactual bool `json:"counterfactual"`
	// ShortSuspect marks a cell whose rows are, or may be, short-side — the E8
	// sign bug (kernel/shadow_ab.go) corrupts those. Direction is recovered from
	// the plan doc; when it cannot be recovered the cell stays SUSPECT, because
	// an unrecoverable direction cannot be cleared.
	ShortSuspect bool   `json:"short_suspect"`
	Note         string `json:"note,omitempty"`
}

// Exclusions is the honesty ledger: every row the model refused, by reason.
type Exclusions struct {
	// UnresolvedPnL — pnl_corrected IS NULL (A22).
	UnresolvedPnL int `json:"unresolved_pnl"`
	// Unresolvable — plan_id is the UNRESOLVABLE sentinel.
	Unresolvable int `json:"unresolvable"`
	// TestSeam — ARMED_TEST_SEAM artifacts.
	TestSeam int `json:"test_seam"`
	// Manual — origin=manual: an NT8 position the bot adopted with no order or
	// arm of its own (R22, owner ruling R1). Not the bot's trade.
	Manual int `json:"manual"`
	// NoCondition — no condition recoverable from the plan doc.
	NoCondition int `json:"no_condition"`
	// CryptoEra is ALWAYS 0 by construction: pre-day-plan-era rows are never
	// loaded, so they are absent rather than excluded. The field exists so the
	// distinction is visible in the payload instead of implied.
	CryptoEra int `json:"crypto_era"`
}

// Table is the whole read model as of one instant.
type Table struct {
	// Cells is the full five-dimensional table (D1).
	Cells []Cell `json:"cells"`
	// Conditions and Sessions are the roll-ups.
	Conditions []Cell `json:"by_condition"`
	Sessions   []Cell `json:"by_session"`

	// Kinds and Paths are the remaining single-dimension roll-ups. Like the
	// others they are aggregated from the RAW rows, never from the cells: a
	// mean-of-means is not a mean, and an sd cannot be recovered from cells at
	// all, so a roll-up built by folding cells would quietly lose its spread.
	Kinds []Cell `json:"by_level_kind"`
	Paths []Cell `json:"by_path"`

	Counterfactual []E8Cell `json:"counterfactual_e8"`

	Excluded Exclusions `json:"excluded"`

	// AsOfMs is the last closed position in the model — the data's own clock
	// (D6). BuiltAtMs is the caller's clock. They are separate because a table
	// built now over stale data must not look fresh.
	AsOfMs    int64 `json:"as_of_ms"`
	BuiltAtMs int64 `json:"built_at_ms"`

	// recs is the atom list every cell above was folded from. It is retained,
	// unexported, so a re-projection (a different roll-up, an era filter) is a
	// re-aggregation of the raw rows rather than a fold of already-folded
	// numbers. Without it, FilterEra could only slice — and a sliced roll-up
	// keeps the unfiltered population's mean while claiming to be scoped.
	recs []rec
}

// ByCondition returns the condition roll-up, or nil when the condition has no
// rows. nil is the honest answer for "no data"; a zero-valued cell would read
// as "measured, and it came out zero".
func (t *Table) ByCondition(cond string) *Cell {
	for i := range t.Conditions {
		if t.Conditions[i].Condition == cond {
			return &t.Conditions[i]
		}
	}
	return nil
}
