package trader

import (
	"fmt"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// TRADE EXCURSION LOGGING (wave 1A, 2026-09-02) — the three hooks that fill
// trade_excursions: open, every bar while the position lives, and close.
//
// CLASS 23 GOVERNS THIS FILE. Every path here is telemetry. It reads bars,
// does arithmetic and writes one row; it decides nothing. A bad bar, a locked
// database or a nil dependency must produce a WARN and nothing else. Every
// entry point goes through safeExcursion, which recovers panics — a measuring
// instrument that can kill the trading loop is worse than no instrument.

// safeExcursion runs one telemetry unit. It NEVER propagates a panic and never
// returns an error to a caller who could act on it.
func (at *AutoTrader) safeExcursion(what string, fn func() error) {
	defer func() {
		if r := recover(); r != nil {
			at.logWarnf("📐 excursion telemetry panicked in %s: %v — trading continues, this row is lost", what, r)
		}
	}()
	if err := fn(); err != nil {
		at.logWarnf("📐 excursion telemetry failed in %s: %v — trading continues, this row is lost", what, err)
	}
}

// excursionsEnabled reports whether this trader can log excursions at all.
func (at *AutoTrader) excursionsEnabled() bool {
	return at != nil && at.store != nil && at.dayPlanEnabled()
}

// excursionOnOpen (E1) writes the entry half. Every excursion column stays
// NULL: nothing has been measured yet.
func (at *AutoTrader) excursionOnOpen(p *store.TraderPosition, stopPx, targetPx, atr5m float64) {
	if p == nil || !at.excursionsEnabled() {
		return
	}
	at.safeExcursion(fmt.Sprintf("open pos=%d", p.ID), func() error {
		row := store.TradeExcursion{
			PositionID: p.ID, PlanID: p.PlanID, Version: p.PlanVersion,
			Session: p.PlanSession, Scenario: p.CitedScenarioID,
			Condition: excursionCondition(at.store, p.PlanID, p.PlanVersion, p.CitedScenarioID),
			Side:      p.Side, EntryPx: p.EntryPrice, EntryTs: p.EntryTime,
			StopPxInitial: stopPx, TargetPx: targetPx,
			Size: p.Quantity, ATR5mAtEntry: atr5m, Source: "live",
		}
		// atr_mult_stop_at_entry: how many ATR5m the initial stop sat away from
		// the fill. NULL when either input is unknown — never a computed 0/0.
		if atr5m > 0 && stopPx > 0 && p.EntryPrice > 0 {
			mult := (p.EntryPrice - stopPx) / atr5m
			if mult < 0 {
				mult = -mult
			}
			row.ATRMultStopEntry = &mult
		}
		id, err := at.store.TradeExcursions().Open(row)
		if err != nil {
			return err
		}
		at.logInfof("📐 excursion opened row=%d pos=%d %s %s entry=%.2f stop=%.2f target=%.2f atr5m=%.2f",
			id, p.ID, p.Symbol, p.Side, p.EntryPrice, stopPx, targetPx, atr5m)
		return nil
	})
}

// excursionOnBarTick (E2) recomputes the path for every open position of this
// trader. It runs from monitorTick, i.e. about once a minute, and rebuilds the
// WHOLE path from the stored bars each time rather than accumulating — so a
// missed tick, a restart or a late bar costs nothing.
func (at *AutoTrader) excursionOnBarTick() {
	if !at.excursionsEnabled() {
		return
	}
	at.safeExcursion("bar tick", func() error {
		open, err := at.store.Position().GetOpenPositions(at.id)
		if err != nil {
			return err
		}
		for i := range open {
			p := open[i]
			row, err := at.store.TradeExcursions().GetByPosition(p.ID)
			if err != nil || row == nil {
				continue // no entry half (pre-wave position) — nothing to update
			}
			at.resolveExcursionLevels(row, p)
			at.recomputeExcursionPath(row, p.EntryTime, time.Now().UnixMilli())
		}
		return nil
	})
}

// resolveExcursionLevels fills a row's stop/target from the decision that
// opened the position, for entries whose execution path never carried them
// down to the position row (the AI path; armed fills bring their own from the
// ledger). It reads the authoritative record and writes nothing when there is
// nothing to read — an unknown level stays unknown.
func (at *AutoTrader) resolveExcursionLevels(row *store.TradeExcursion, p *store.TraderPosition) {
	if row == nil || row.StopPxInitial > 0 || at.store == nil {
		return
	}
	stop, target, ok := at.store.Decision().StopTargetNear(at.id, p.Symbol, p.EntryTime, excursionLevelWindowMs)
	if !ok {
		return
	}
	if err := at.store.TradeExcursions().SetLevels(row.ID, stop, target); err != nil {
		at.logWarnf("📐 excursion level resolve failed row=%d: %v", row.ID, err)
		return
	}
	row.StopPxInitial, row.TargetPx = stop, target
	at.logInfof("📐 excursion row=%d levels resolved from the opening decision: stop=%.2f target=%.2f", row.ID, stop, target)
}

// excursionLevelWindowMs bounds how far from a fill an opening decision may sit
// and still be read as that fill's decision. Two minutes: the confirm-and-fill
// round trip is seconds, and the next cycle is a minute away.
const excursionLevelWindowMs int64 = 120_000

// recomputeExcursionPath rebuilds one row's path from the bar store and writes
// it. A hold with no bar coverage is marked "none" and keeps its NULLs.
func (at *AutoTrader) recomputeExcursionPath(row *store.TradeExcursion, fromMs, toMs int64) {
	bars := at.excursionBars(fromMs, toMs)
	path := kernel.ComputePathExcursion(row.EntryPx, row.Side, row.StopPxInitial, row.TargetPx,
		bars, fromMs, toMs, "1m")
	if !path.Computed {
		if row.Resolution == "" {
			if err := at.store.TradeExcursions().MarkNoCoverage(row.ID); err != nil {
				at.logWarnf("📐 excursion no-coverage mark failed row=%d: %v", row.ID, err)
			}
		}
		return
	}
	if err := at.store.TradeExcursions().UpdatePath(row.ID, store.TradeExcursionPath{
		MAEPts: path.MAEPts, MAETs: path.MAETs, MAEBars: path.MAEBars,
		MFEPts: path.MFEPts, MFETs: path.MFETs, MFEBars: path.MFEBars,
		BarsHeld: path.BarsHeld, AmbiguousBars: path.AmbiguousBars,
		Resolution: path.Resolution,
	}); err != nil {
		at.logWarnf("📐 excursion path write failed row=%d: %v", row.ID, err)
	}
}

// excursionBars reads the 1m tape covering a hold. The persisted bars table is
// the source: it is the same tape a replay would read, so a live row and a
// backfilled row are built from identical inputs.
func (at *AutoTrader) excursionBars(fromMs, toMs int64) []market.Kline {
	if at.store == nil {
		return nil
	}
	// one bar of slack on each side so the bar CONTAINING the fill is present
	// ROLL WAVE — a trade's excursion is measured on the contract the TRADE
	// was on. A window across the roll is unrecomputable, not "the bars we
	// happen to have".
	contract, ok := at.store.BarHistory().WindowContract(at.futuresSymbol(), fromMs-60_000, toMs+60_000)
	if !ok {
		// nil is this function's "no usable bars" — the caller already treats
		// it as UNRESOLVED rather than zero. A window that spans the roll is
		// exactly that: not measurable on one scale.
		_ = contract
		return nil
	}
	rows, err := at.store.BarHistory().BarsBetweenOn(at.futuresSymbol(), "1m", contract, fromMs-60_000, toMs+60_000)
	if err != nil || len(rows) == 0 {
		return nil
	}
	out := make([]market.Kline, 0, len(rows))
	for _, r := range rows {
		out = append(out, market.Kline{
			OpenTime: r.OpenTimeMs, Open: r.O, High: r.H, Low: r.L, Close: r.C, Volume: r.V,
		})
	}
	return out
}

// excursionOnClose (E3) writes the exit half and one final path recomputation.
// The exit is read AGAINST the trade when the closing bar reached both levels.
func (at *AutoTrader) excursionOnClose(p *store.TraderPosition) {
	if p == nil || !at.excursionsEnabled() {
		return
	}
	at.safeExcursion(fmt.Sprintf("close pos=%d", p.ID), func() error {
		row, err := at.store.TradeExcursions().GetByPosition(p.ID)
		if err != nil || row == nil {
			return err // no entry half: a pre-wave position, nothing to close
		}
		if row.ExitTs != nil {
			// Already closed — never re-close (LOG-NOISE-2: a position whose
			// excursion row already carries its exit half is not re-processed).
			return nil
		}
		exitMs := p.ExitTime
		if exitMs <= 0 {
			exitMs = time.Now().UnixMilli()
		}
		at.recomputeExcursionPath(row, row.EntryTs, exitMs)

		// C4 — did the position close inside a bar that reached BOTH levels?
		ambiguous := false
		if bars := at.excursionBars(exitMs-60_000, exitMs); len(bars) > 0 {
			last := bars[len(bars)-1]
			if _, amb := kernel.ResolveAmbiguousExit(row.Side, row.StopPxInitial, row.TargetPx, last); amb {
				ambiguous = true
				at.logWarnf("📐 excursion pos=%d closed inside a bar spanning BOTH stop %.2f and target %.2f — recorded against the trade",
					p.ID, row.StopPxInitial, row.TargetPx)
			}
		}
		if err := at.store.TradeExcursions().Close(row.ID, store.TradeExcursionClose{
			ExitPx: p.ExitPrice, ExitTs: exitMs, ExitReason: p.CloseReason,
			StopPxFinal:  row.StopPxInitial, // no trail wave yet; the initial stop is the final one
			PnlCorrected: p.PnlCorrected,    // A22 — corrected only; nil stays NULL
			Ambiguous:    ambiguous,
		}); err != nil {
			return err
		}
		at.logInfof("📐 excursion closed row=%d pos=%d exit=%.2f reason=%s ambiguous=%v",
			row.ID, p.ID, p.ExitPrice, p.CloseReason, ambiguous)
		return nil
	})
}
