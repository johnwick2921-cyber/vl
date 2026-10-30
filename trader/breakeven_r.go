package trader

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// ── Release #25 PK-5 (STRICT-12345 F-8) — BREAKEVEN IN R ────────────────────
//
// risk_control.breakeven_trigger_points (40 on the live strategy) is in
// POINTS: on a 39.5-pt stop it is ~1R, on a 112.5-pt stop (654) 0.36R. Six
// planner losers reached ≥ 1R and still took a full stop (623, 599, 620, 595,
// 607, 621 — −$595, report §3 S4 [B]).
//
// Knob risk_control.breakeven_trigger_r (store.ResolveBreakevenTriggerR),
// DEFAULT 0 = OFF = the points rule, byte-identical. When > 0 (and
// breakeven_enabled is on): the stop moves to entry once the position is
// breakeven_trigger_r × its INITIAL risk in profit, initial risk =
// |entry − the stop at fill| (the trade_excursions row's stop_px_initial, else
// the armed ledger row's bracket stop). Unknown initial risk → the points rule
// for that position, logged once + counted breakeven_r_unknown_risk (never a
// fabricated risk). A mentor-mode trader never reaches this (R3 owns its BE):
// the existing mentor gate in maybeMoveStopToBreakeven runs on the points
// path, untouched.

// BreakevenRUnknownRiskClass counts a position judged on the points rule
// because its initial risk is unknown.
const BreakevenRUnknownRiskClass = "breakeven_r_unknown_risk"

var breakevenRUnknownLogged sync.Map // trader|symbol_side|entry → struct{}
var breakevenRFiredLogged sync.Map   // trader|symbol_side|entry → struct{}

// breakevenTriggerR is the pure R decision: (fire, points in profit).
func breakevenTriggerR(rc store.RiskControlConfig, side string, entry, mark, r, risk float64) (bool, float64) {
	if !hlBool(rc.BreakevenEnabled, false) {
		return false, 0
	}
	var pts float64
	if strings.ToLower(side) == "long" {
		pts = mark - entry
	} else {
		pts = entry - mark
	}
	return pts >= r*risk, pts
}

// breakevenTriggerFor is the ONE trigger resolution maybeMoveStopToBreakeven
// calls: OFF (breakeven_trigger_r unset/0) or a mentor-mode trader → the
// points rule exactly as before.
func (at *AutoTrader) breakevenTriggerFor(symbol, side string, entry, mark float64) (bool, float64) {
	rc := at.config.StrategyConfig.RiskControl
	r, _ := store.ResolveBreakevenTriggerR(rc)
	if r <= 0 || at.modeOwnsExits() || !hlBool(rc.BreakevenEnabled, false) {
		return breakevenTrigger(rc, side, entry, mark)
	}
	risk, src, ok := at.initialRiskFor(symbol, side, entry)
	if !ok {
		key := fmt.Sprintf("%s|%s_%s|%.2f", at.id, symbol, strings.ToUpper(side), entry)
		if _, seen := breakevenRUnknownLogged.LoadOrStore(key, struct{}{}); !seen {
			at.logWarnf("🎯 auto-breakeven: %s %s initial risk UNKNOWN (no stop at fill on record) — breakeven_trigger_r %.2f not applied, the points rule (%.1f pts) governs this position",
				symbol, side, r, breakevenTriggerPoints(rc))
			at.countPlannerKnob(BreakevenRUnknownRiskClass, kernel.CMESessionDayKey(time.Now()), "")
		}
		return breakevenTrigger(rc, side, entry, mark)
	}
	fire, pts := breakevenTriggerR(rc, side, entry, mark, r, risk)
	if fire {
		key := fmt.Sprintf("%s|%s_%s|%.2f", at.id, symbol, strings.ToUpper(side), entry)
		if _, seen := breakevenRFiredLogged.LoadOrStore(key, struct{}{}); !seen {
			at.logInfof("🎯 auto-breakeven (R rule): %s %s +%.1f pts ≥ %.2fR × initial risk %.2f pts (%s)", symbol, side, pts, r, risk, src)
		}
	}
	return fire, pts
}

// initialRiskFor reads the open position's stop AT FILL from the record:
// trade_excursions.stop_px_initial first (armed fills and AI fills alike),
// else the armed ledger row's bracket stop. risk = |entry − stop|.
func (at *AutoTrader) initialRiskFor(symbol, side string, entry float64) (float64, string, bool) {
	if at.store == nil || entry <= 0 {
		return 0, "", false
	}
	opens, err := at.store.Position().GetOpenPositions(at.id)
	if err != nil {
		return 0, "", false
	}
	want := positionSide(side)
	for i := range opens {
		p := opens[i]
		if positionSide(p.Side) != want || !(strings.EqualFold(p.Symbol, symbol) || strings.EqualFold(market.Normalize(p.Symbol), market.Normalize(symbol))) {
			continue
		}
		if row, xerr := at.store.TradeExcursions().GetByPosition(p.ID); xerr == nil && row != nil && row.StopPxInitial > 0 {
			if risk := math.Abs(entry - row.StopPxInitial); risk > 0 {
				return risk, fmt.Sprintf("stop at fill %.2f, trade_excursions pos %d", row.StopPxInitial, p.ID), true
			}
		}
		if p.EntryOrderID != "" {
			if r, aerr := at.store.ArmedOrders().FindBySignal(at.id, p.EntryOrderID); aerr == nil && r != nil && r.StopPx > 0 {
				if risk := math.Abs(entry - r.StopPx); risk > 0 {
					return risk, fmt.Sprintf("bracket stop %.2f, armed row %d", r.StopPx, r.ID), true
				}
			}
		}
		return 0, "", false
	}
	return 0, "", false
}
