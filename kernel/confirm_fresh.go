package kernel

import (
	"fmt"
	"math"
	"strings"
	"time"

	"vl/market"
)

// CONFIRM FRESHNESS (planner-touch-fills, rescan 10-14 CTO binding input) — a
// MET confirm authorizes an arm only while it is FRESH:
//
//  1. AGE, judged on EVENT instants, never the evaluation instant: #519 (#23)
//     re-requests history after a feed gap and merges it into the cache the
//     gate reads, so a touch or hold that happened during an outage would
//     otherwise read as a brand-new trigger on the first pass after the
//     backfill. Fresh by age ⇔ the verdict's event (v.EventMs) is no older
//     than MaxAgeMs, OR the SAME scenario confirm re-evaluated over the last
//     MaxAgeMs alone (since = max(birth, now − MaxAgeMs)) is MET — a level
//     that reacted again recently is a fresh trigger; a reaction that exists
//     only in older tape is not. Bound: the caller passes
//     day_plan.zone_rest_max_min (a trigger is good for as long as a limit
//     placed on it may rest). #654's row was authorized from a
//     24.5-minute-old touch.
//  2. DISTANCE, the executor prompt's existing staleness rule
//     (staleConfirmAnnotation: |price − context| > STALE_CONFIRM_ATR × ATR5m),
//     now consulted by the gate. The context is the level the confirm was
//     judged at: a level play's touch band (entry_zone ∪ ref — 0 inside it),
//     else the FVG anchor, else the ref. No ATR5m → this leg fails open (the
//     annotation's own rule), the age leg still binds.
//
// A MET verdict without a known event instant cannot be aged: UNKNOWN does not
// satisfy (fail-closed).

// ConfirmFreshInput is the instant, price and bounds a freshness read uses.
type ConfirmFreshInput struct {
	NowMs    int64
	Price    float64 // last CLOSED 1m close at NowMs; ≤0 = unknown (distance leg skipped)
	ATR5m    float64 // StaleConfirmATR5m on the closed tape; ≤0 = unknown (distance leg skipped)
	MaxAgeMs int64   // ≤0 = no age bound
}

// recentlyMet re-reads the scenario's confirm over the last MaxAgeMs alone.
func recentlyMet(s PlanScenario, bars []market.Kline, birthMs int64, in ConfirmFreshInput) bool {
	since := in.NowMs - in.MaxAgeMs
	if birthMs > since {
		since = birthMs
	}
	return scenarioConfirm(s, bars, since, in.NowMs).Met
}

// confirmContext is the price band the distance leg measures from.
func confirmContext(s PlanScenario, v ConfirmVerdict) (lo, hi float64, label string) {
	if TouchGatedCondition(s.Condition) {
		if l, h, ok := touchBand(s); ok {
			if h > l {
				return l, h, fmt.Sprintf("zone %.2f–%.2f", l, h)
			}
			return l, h, fmt.Sprintf("level %.2f", l)
		}
	}
	ctx := v.RefPrice
	if s.Fvg != nil && (s.Fvg.Lo > 0 || s.Fvg.Hi > 0) {
		if a, ok := ScenarioAnchor(s, nil); ok {
			ctx = a
		}
	}
	return ctx, ctx, fmt.Sprintf("level %.2f", ctx)
}

// ConfirmFresh returns whether a MET verdict is fresh at in.NowMs, the stale
// class (age | distance | event_unknown) and the reason. bars/birthMs are the
// tape and plan birth the verdict was evaluated on (the windowed re-read).
func ConfirmFresh(s PlanScenario, v ConfirmVerdict, bars []market.Kline, birthMs int64, in ConfirmFreshInput) (fresh bool, class, why string) {
	if !v.Met {
		return false, "not_met", "confirm not met"
	}
	if !v.EventKnown || v.EventMs <= 0 {
		return false, "event_unknown", "the MET event instant is unknown — its age cannot be judged (UNKNOWN does not satisfy)"
	}
	if in.MaxAgeMs > 0 && in.NowMs-v.EventMs > in.MaxAgeMs && !recentlyMet(s, bars, birthMs, in) {
		return false, "age", fmt.Sprintf("the trigger fired at %s, %.0f min ago > %d min (day_plan.zone_rest_max_min), and no newer reaction exists inside that window — judged on event times, so a backfilled outage-time trigger is not new",
			FormatCT(time.UnixMilli(v.EventMs)), float64(in.NowMs-v.EventMs)/60_000, in.MaxAgeMs/60_000)
	}
	if in.Price > 0 && in.ATR5m > 0 {
		lo, hi, label := confirmContext(s, v)
		if lo > 0 {
			d := 0.0
			if in.Price < lo {
				d = lo - in.Price
			} else if in.Price > hi {
				d = in.Price - hi
			}
			if lim := StaleConfirmATR() * in.ATR5m; d > lim && !math.IsNaN(d) {
				return false, "distance", fmt.Sprintf("price %.2f is %.2f pts from the %s > %.1f×ATR5m %.2f (STALE_CONFIRM_ATR) — treat as expired",
					in.Price, d, label, StaleConfirmATR(), lim)
			}
		}
	}
	return true, "", strings.TrimSpace("fresh")
}
