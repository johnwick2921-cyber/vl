package trader

import (
	"fmt"
	"strings"

	"vl/market"
)

// ── Release #25 PK-3 (STRICT-12345 F-5) — NO-CHASE ENFORCE + MOMENTUM INTO
// THE ZONE ──────────────────────────────────────────────────────────────────
//
// Two arm-path switches, both DEFAULT OFF:
//
//  1. day_plan.planner_no_chase_enforce — the no-chase leg (no_chase.go) has
//     been WARN-first since 2026-09-02. ON: its arm-path WOULD_REFUSE verdict
//     REFUSES the placement (class no_chase_refused). CAVEAT (rescan N4): the
//     arm metric is |live price − cited level|, i.e. how far price must come
//     back to a resting limit — on 653 it printed WOULD_REFUSE at 08:54/08:55
//     and would have PASSED at 08:56, right before the dump filled it.
//     Enforcing it alone does not stop the 653 shape; switch 2 is aimed at it.
//
//  2. day_plan.planner_momentum_into_zone_max_atr — X > 0 refuses an arm
//     placement when price has moved TOWARD the zone (long: down, short: up)
//     over the last MomentumIntoZoneWindowMin minutes by more than X × ATR5m
//     (the arm seam's ATR5m). A limit that can only fill on adverse momentum
//     is a bet that the momentum stops at the level; 650 moved 3.5×ATR5m into
//     its fill and 653 1.3× (report §1). Unmeasurable (no tape covering the
//     window, or no ATR) → REFUSED while ON (fail-closed: ON means "prove the
//     approach is not a dump").

// NoChaseRefusedClass is the PK-3 enforced no-chase refusal class.
const NoChaseRefusedClass = "no_chase_refused"

// MomentumIntoZoneClass is the PK-3 momentum refusal class.
const MomentumIntoZoneClass = "momentum_into_zone"

// MomentumIntoZoneWindowMin is the look-back: 3 × 5m = 15 minutes, the window
// the STRICT-12345 report measured "the 15-minute move into the fill" over.
const MomentumIntoZoneWindowMin = 15

// momentumVerdict is the pure measurement + opinion.
type momentumVerdict struct {
	Known   bool
	MovePts float64 // signed: + = toward the zone (adverse to the entry side)
	MoveATR float64
	Refuse  bool
	Why     string
}

// momentumIntoZoneVerdict measures the move over the last window: from the
// OPEN of the first 1m bar inside [now − window, now) to the CLOSE of the
// latest bar (the live price — the forming bar included, as lastTapeClose
// reads it). The window must be covered from its first minute; otherwise the
// move is unknown. maxATR ≤ 0 → off (never called by the gate).
func momentumIntoZoneVerdict(side string, bars []market.Kline, nowMs int64, atr5m, maxATR float64) momentumVerdict {
	v := momentumVerdict{}
	start := nowMs - int64(MomentumIntoZoneWindowMin)*60_000
	var first, last *market.Kline
	for i := range bars {
		b := &bars[i]
		if b.OpenTime < start || b.OpenTime >= nowMs || b.Open <= 0 || b.Close <= 0 {
			continue
		}
		if first == nil || b.OpenTime < first.OpenTime {
			first = b
		}
		if last == nil || b.OpenTime > last.OpenTime {
			last = b
		}
	}
	if first == nil || last == nil || first.OpenTime > start+60_000 || atr5m <= 0 {
		v.Why = fmt.Sprintf("%s UNKNOWN — no tape covering the last %d min or no ATR5m (%.2f); refused while planner_momentum_into_zone_max_atr is on",
			MomentumIntoZoneClass, MomentumIntoZoneWindowMin, atr5m)
		v.Refuse = true
		return v
	}
	v.Known = true
	if strings.EqualFold(strings.TrimSpace(side), "long") {
		v.MovePts = first.Open - last.Close // a fall into a long's zone
	} else {
		v.MovePts = last.Close - first.Open // a rise into a short's zone
	}
	v.MoveATR = v.MovePts / atr5m
	if v.MoveATR > maxATR+1e-9 {
		v.Refuse = true
		v.Why = fmt.Sprintf("%s: price moved %.2f pts toward the zone in the last %d min = %.2f×ATR5m (%.2f) > %.2f (planner_momentum_into_zone_max_atr) — a limit there fills only on adverse momentum",
			MomentumIntoZoneClass, v.MovePts, MomentumIntoZoneWindowMin, v.MoveATR, atr5m, maxATR)
	}
	return v
}
