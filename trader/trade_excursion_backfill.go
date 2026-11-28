package trader

import (
	"encoding/json"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// TRADE EXCURSION BACKFILL (wave 1A, E5) — build the record for positions that
// closed before the logging existed, from the SAME stored 1m tape the live
// hooks read, so a backfilled row and a live row are comparable.
//
// A position with no bar coverage gets a row that says so (resolution="none")
// and keeps every excursion column NULL. It never gets a number (A24). The
// count of those rows is reported and printed on the boot line, because a
// distribution built from 60% of the trades must say which 60%.

// BackfillResult is what one run measured. Every field is a count, never a
// rate — a rate without n is exactly what A24 forbids.
type BackfillResult struct {
	Scanned     int // closed positions considered
	Computed    int // rows with a real path
	NoCoverage  int // rows marked "none" — the tape does not reach them
	LevelsFound int // rows whose stop/target were resolved from the decision
}

// Backfill walks the closed positions in [from, to] and writes their excursion
// rows. Idempotent: an existing row is recomputed in place, never duplicated.
func BackfillExcursions(st *store.Store, symbol, traderID string, from, to time.Time) (BackfillResult, error) {
	var res BackfillResult
	if st == nil {
		return res, nil
	}
	positions, err := st.TradeExcursions().ClosedPositionsBetween(symbol, traderID, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return res, err
	}

	for i := range positions {
		p := positions[i]
		res.Scanned++
		exitMs := p.ExitTime
		exitFill := p.ExitPrice
		if exitMs <= 0 {
			exitMs = p.EntryTime
			exitFill = 0 // the fill's minute is unknown: do not place it at the entry
		}
		// Levels come from the decision that opened the position — read, never
		// reconstructed. Absent means the ambiguity count stays honest at 0
		// because an unknown level cannot be shown to have been spanned.
		stop, target, haveLevels := st.Decision().StopTargetNear(p.TraderID, p.Symbol, p.EntryTime, 120_000)
		if haveLevels {
			res.LevelsFound++
		}
		row := store.TradeExcursion{
			PositionID: p.ID, PlanID: p.PlanID, Version: p.PlanVersion,
			Session: p.PlanSession, Scenario: p.CitedScenarioID,
			Condition: excursionCondition(st, p.PlanID, p.PlanVersion, p.CitedScenarioID),
			Side:      p.Side, EntryPx: p.EntryPrice, EntryTs: p.EntryTime,
			StopPxInitial: stop, TargetPx: target, Size: p.Quantity,
			Source: "backfill",
		}
		id, err := st.TradeExcursions().Open(row)
		if err != nil {
			return res, err
		}
		if haveLevels {
			_ = st.TradeExcursions().SetLevels(id, stop, target)
		}

		bars := excursionBarsFor(st, p.Symbol, p.EntryTime, exitMs)
		// RT-8 — the same exit-minute bound as the live close hook, so a
		// backfilled row and a live row of one trade agree.
		path := closedHoldPath(p.EntryPrice, p.Side, stop, target, bars, p.EntryTime, exitMs, exitFill, excursionOneFill(&p))
		if !path.Computed {
			res.NoCoverage++
			if err := st.TradeExcursions().MarkNoCoverage(id); err != nil {
				return res, err
			}
		} else {
			res.Computed++
			if err := st.TradeExcursions().UpdatePath(id, store.TradeExcursionPath{
				MAEPts: path.MAEPts, MAETs: path.MAETs, MAEBars: path.MAEBars,
				MFEPts: path.MFEPts, MFETs: path.MFETs, MFEBars: path.MFEBars,
				BarsHeld: path.BarsHeld, AmbiguousBars: path.AmbiguousBars,
				Resolution: path.Resolution,
			}); err != nil {
				return res, err
			}
		}
		// The exit half, with the CORRECTED P&L only (A22).
		ambiguous := false
		if len(bars) > 0 && haveLevels {
			if _, amb := kernel.ResolveAmbiguousExit(p.Side, stop, target, bars[len(bars)-1]); amb {
				ambiguous = true
			}
		}
		if err := st.TradeExcursions().Close(id, store.TradeExcursionClose{
			ExitPx: p.ExitPrice, ExitTs: exitMs, ExitReason: p.CloseReason,
			PnlCorrected: p.PnlCorrected, Ambiguous: ambiguous,
		}); err != nil {
			return res, err
		}
	}
	return res, nil
}

// barsFor reads the 1m tape covering a hold, with one bar of slack each side so
// the bar CONTAINING the fill is present.
func excursionBarsFor(st *store.Store, symbol string, fromMs, toMs int64) []market.Kline {
	// ROLL WAVE — same rule as the live hook: the trade's own contract, or
	// unrecomputable.
	contract, ok := st.BarHistory().WindowContract(symbol, fromMs-60_000, toMs+60_000)
	if !ok {
		// nil is this function's "no usable bars" — the caller already treats
		// it as UNRESOLVED rather than zero. A window that spans the roll is
		// exactly that: not measurable on one scale.
		_ = contract
		return nil
	}
	rows, err := st.BarHistory().BarsBetweenOn(symbol, "1m", contract, fromMs-60_000, toMs+60_000)
	if err != nil || len(rows) == 0 {
		return nil
	}
	out := make([]market.Kline, 0, len(rows))
	for _, r := range rows {
		out = append(out, market.Kline{OpenTime: r.OpenTimeMs, Open: r.O, High: r.H, Low: r.L, Close: r.C, Volume: r.V})
	}
	return out
}

// excursionCondition resolves the SCENARIO CONDITION a position was opened on
// (reject, sweep_reclaim, breakdown_continue, …) by reading the plan version
// the position cites. The condition is what the exit study groups by, and it
// lives only in the plan doc — the position row carries the scenario id alone.
//
// Returns "" when the plan, the version or the scenario cannot be found. An
// unknown condition is left unknown; it is never inferred from the side or the
// close reason (A24).
func excursionCondition(st *store.Store, planID string, version int, scenarioID string) string {
	if st == nil || planID == "" || scenarioID == "" || version <= 0 {
		return ""
	}
	row, err := st.Plan().GetPlan(planID, version)
	if err != nil || row == nil {
		return ""
	}
	var doc kernel.PlanDoc
	if json.Unmarshal([]byte(row.Doc), &doc) != nil {
		return ""
	}
	for _, sc := range doc.Scenarios {
		if sc.ID == scenarioID {
			return sc.Condition
		}
	}
	return ""
}
