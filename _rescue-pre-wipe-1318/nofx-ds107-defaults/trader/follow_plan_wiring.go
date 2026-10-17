// ── THE FOLLOW-PLAN RECORDER — THE PRODUCTION CALL PATH ─────────────────────
//
// Two callers, both by name in the A29 gate: recordFollowPlans at the detector
// hook (once per planner read, on the SAME ring the detector judged) and
// BackfillFollowPlans at boot (D9, three-state, through the roll wave's
// contract filter). Neither places, cancels, gates or touches an arm row:
// this file has no reference to the ledger or to any placer (E9 pins that by
// text, TestFollowRecorderNeverReachesTheWire by behaviour).
//
// Cadence is the planner read's (30–120 min live on 2026-09-10), so a break
// or a retest is recorded at the next read, not the next tick — a record, not
// a signal. Every field NULL until its event; a never-broken level stays a
// row with break_at NULL until its session-day ends and it reads no_break.
package trader

import (
	"encoding/json"
	"strconv"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// followPlanHorizons are round 17's two horizons, in 5-minute buckets [T].
var followPlanHorizons = []int{10, 20}

// followScopeEnd is the end of the CME session-day the episode opened in.
func followScopeEnd(openedAtMs int64) int64 {
	start := kernel.CMESessionDayStart(time.UnixMilli(openedAtMs))
	return start.Add(24 * time.Hour).UnixMilli()
}

// followPlanBias reads the plan's FROZEN bias for the row — json.Unmarshal,
// never ParsePlanDoc, which rejects stored docs seated at the strategy's own
// max_levels (the W1 link's live defect, A15). "" when the plan is unknown.
func (at *AutoTrader) followPlanBias(cache map[string]string, planID string, version int) string {
	key := planID + "#" + strconv.Itoa(version)
	if v, ok := cache[key]; ok {
		return v
	}
	bias := ""
	if at != nil && at.store != nil && planID != "" && version > 0 {
		if p, err := at.store.Plan().GetPlan(planID, version); err == nil && p != nil {
			var doc struct {
				Bias struct {
					Direction string `json:"direction"`
				} `json:"bias"`
			}
			if json.Unmarshal([]byte(p.Doc), &doc) == nil {
				bias = doc.Bias.Direction
			}
		}
	}
	cache[key] = bias
	return bias
}

// followStampFrom turns the pure plan into the store's stamp, joining the
// retest's own detector verdict when the detector has written it.
func (at *AutoTrader) followStampFrom(r store.TouchOutcomeRow, fp kernel.FollowPlan, bias string, backfill string) store.FollowPlanStamp {
	st := store.FollowPlanStamp{State: fp.State, Backfill: backfill}
	if fp.BreakAtMs != nil {
		b := *fp.BreakAtMs
		st.BreakAtMs = &b
		d, flip := fp.BreakDir, fp.BiasWouldFlipTo
		st.BreakDir, st.BiasWouldFlip = &d, &flip
		if bias != "" {
			fb := bias
			st.PlanBiasFrozen = &fb
		}
	}
	if fp.RetestAtMs != nil {
		rt := *fp.RetestAtMs
		st.RetestAtMs = &rt
		basis := fp.EntryBasis
		st.EntryBasis = &basis
		if fp.EntryPx != nil {
			px := *fp.EntryPx
			st.EntryPx = &px
		}
		st.MAE10, st.MFE10, st.Net10 = fp.MAE10, fp.MFE10, fp.Net10
		st.MAE20, st.MFE20, st.Net20 = fp.MAE20, fp.MFE20, fp.Net20
		// The retest's OWN verdict from the detector: joined by level identity
		// (price within the map's width), session-day and order — the next
		// episode on this level at or after the retest bar. The retest of a
		// reversed level comes from the far side; a detector episode that
		// entered from the original side is not this retest and is not joined.
		if at != nil && at.store != nil {
			dayStart := kernel.CMESessionDayStart(time.UnixMilli(r.OpenedAtMs)).UnixMilli()
			if out, side, ok := at.store.TouchOutcomes().RetestVerdictFor(r.TraderID, r.Symbol, r.LevelPrice, scenarioLinkBand(), rt, dayStart, followScopeEnd(r.OpenedAtMs), r.ID); ok {
				far := "below"
				if fp.BreakDir == "up" {
					far = "above"
				}
				if side == far && (out == "hold" || out == "break") {
					o := out
					held := out == "hold"
					st.RetestOutcome, st.RoleReversed = &o, &held
				}
			}
		}
	}
	return st
}

// recordFollowPlans — THE RECORDER'S PRODUCTION CALL PATH at the detector
// hook. Scans every open follow-plan on this symbol against the ring the
// detector just judged. A fault here must never reach the planner (A10):
// recover() is pinned, and the pass returns what it managed.
func (at *AutoTrader) recordFollowPlans(symbol string, bars []market.Kline, now time.Time) (scanned, breaks, retests int) {
	if at == nil || at.store == nil || len(bars) == 0 {
		return 0, 0, 0
	}
	defer func() {
		if r := recover(); r != nil {
			at.logWarnf("📐 follow-plan recorder recovered from panic: %v (this read's pass abandoned; nothing armed either way)", r)
		}
	}()
	ts := at.store.TouchOutcomes()
	sinceMs := bars[0].OpenTime
	rows, err := ts.OpenFollowPlans(at.id, symbol, sinceMs)
	if err != nil {
		at.logWarnf("📐 follow-plan recorder: read failed: %v", err)
		return 0, 0, 0
	}
	biasCache := map[string]string{}
	for i := range rows {
		r := rows[i]
		fp := kernel.ComputeFollowPlan(kernel.FollowPlanInput{
			Level: r.LevelPrice, EntrySide: r.EntrySide, OpenedAtMs: r.OpenedAtMs, NowMs: now.UnixMilli(),
			ScopeEndMs: followScopeEnd(r.OpenedAtMs), Tick: market.FuturesTickSize(symbol),
			Bars: barsFrom(bars, r.OpenedAtMs-60_000), BucketMinutes: 5, Horizons: followPlanHorizons, FrictionPts: store.FollowFrictionPts,
		})
		scanned++
		stamp := at.followStampFrom(r, fp, at.followPlanBias(biasCache, r.PlanID, r.PlanVersion), "")
		if err := ts.StampFollowPlan(r.ID, stamp); err != nil {
			at.logWarnf("📐 follow-plan recorder: episode %d write failed: %v", r.ID, err)
			continue
		}
		// A9 LOUD, once per event: the break names its time and direction; the
		// retest names its time, its would-be entry and the horizons.
		if fp.BreakAtMs != nil && r.FollowBreakAtMs == nil {
			breaks++
			at.logInfof("📐 follow-plan (RECORDED ONLY, never armed): episode %d level %.2f BROKE %s at %s — role recorded as reversed; bias would flip to %s (live bias untouched)",
				r.ID, r.LevelPrice, fp.BreakDir, kernel.FormatCT(time.UnixMilli(*fp.BreakAtMs)), fp.BiasWouldFlipTo)
		}
		if fp.RetestAtMs != nil && r.FollowRetestAtMs == nil {
			retests++
			entry := "NULL (" + fp.EntryBasis + ")"
			if fp.EntryPx != nil {
				entry = strconv.FormatFloat(*fp.EntryPx, 'f', 2, 64) + " (" + fp.EntryBasis + ")"
			}
			at.logInfof("📐 follow-plan (RECORDED ONLY): episode %d level %.2f RETESTED at %s from the far side — would-be entry %s · horizons 10/20 five-minute buckets · state=%s",
				r.ID, r.LevelPrice, kernel.FormatCT(time.UnixMilli(*fp.RetestAtMs)), entry, fp.State)
		}
	}
	return scanned, breaks, retests
}

// barsFrom returns the bars at or after fromMs (the recorder needs the bar
// before the episode's open for the retest's previous-close test).
func barsFrom(bars []market.Kline, fromMs int64) []market.Kline {
	for i := range bars {
		if bars[i].OpenTime >= fromMs {
			return bars[i:]
		}
	}
	return nil
}

// BackfillFollowPlans — D9 for the follow side, three-state. Rows opened at or
// after sinceMs with no follow state are computed from the STORE's bars,
// read through the roll wave's contract filter: a window that spans the roll
// is unrecomputable:spans_roll; a window the store cannot serve is
// unrecomputable:no_bars. Rows before sinceMs are untouched and counted.
func (at *AutoTrader) BackfillFollowPlans(sinceMs int64, now time.Time) store.FollowBackfillResult {
	res := store.FollowBackfillResult{Ran: true}
	if at == nil || at.store == nil {
		return res
	}
	defer func() {
		if r := recover(); r != nil {
			at.logWarnf("📐 follow-plan backfill recovered from panic: %v", r)
		}
	}()
	ts := at.store.TouchOutcomes()
	symbol := at.futuresSymbol()
	res.Untouched = int(ts.CountEpisodesBefore(at.id, sinceMs))
	rows, err := ts.OpenFollowPlans(at.id, symbol, sinceMs)
	if err != nil {
		at.logWarnf("📐 follow-plan backfill: read failed: %v", err)
		return res
	}
	bh := at.store.BarHistory()
	biasCache := map[string]string{}
	for i := range rows {
		r := rows[i]
		if r.FollowState != nil {
			continue // the live recorder already owns it
		}
		from, to := r.OpenedAtMs-60_000, followScopeEnd(r.OpenedAtMs)
		if to > now.UnixMilli() {
			to = now.UnixMilli()
		}
		contract, ok := bh.WindowContract(symbol, from, to)
		if !ok {
			res.Unrecomputable++
			why := "no_contract"
			if contract == store.ContractMixed {
				why = "spans_roll"
			}
			_ = ts.StampFollowPlan(r.ID, store.FollowPlanStamp{State: "unrecomputable:" + why, Backfill: "unrecomputable:" + why})
			continue
		}
		hist, err := bh.BarsBetweenOn(symbol, "1m", contract, from, to)
		if err != nil || len(hist) < 2 {
			res.Unrecomputable++
			_ = ts.StampFollowPlan(r.ID, store.FollowPlanStamp{State: "unrecomputable:no_bars", Backfill: "unrecomputable:no_bars"})
			continue
		}
		bars := make([]market.Kline, 0, len(hist))
		for _, b := range hist {
			bars = append(bars, market.Kline{OpenTime: b.OpenTimeMs, CloseTime: b.OpenTimeMs + 59_999, Open: b.O, High: b.H, Low: b.L, Close: b.C, Volume: b.V})
		}
		fp := kernel.ComputeFollowPlan(kernel.FollowPlanInput{
			Level: r.LevelPrice, EntrySide: r.EntrySide, OpenedAtMs: r.OpenedAtMs, NowMs: now.UnixMilli(),
			ScopeEndMs: followScopeEnd(r.OpenedAtMs), Tick: market.FuturesTickSize(symbol),
			Bars: bars, BucketMinutes: 5, Horizons: followPlanHorizons, FrictionPts: store.FollowFrictionPts,
		})
		stamp := at.followStampFrom(r, fp, at.followPlanBias(biasCache, r.PlanID, r.PlanVersion), "recomputed")
		if err := ts.StampFollowPlan(r.ID, stamp); err == nil {
			res.Recomputed++
		}
	}
	return res
}
