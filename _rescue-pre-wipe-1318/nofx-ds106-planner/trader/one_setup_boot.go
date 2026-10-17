// ── ONE SETUP D8 — THE BOOT LINE, and D9 — THE BACKFILLS. READ, never literal.
package trader

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"nofx/kernel"
	"nofx/store"
)

// boundOneSetup resolves the two knobs from THE STRATEGY BOUND TO A TRADER,
// via bootRiskFacts' one-bound-strategy discipline (never the file default).
func boundOneSetup(st *store.Store) (enabled bool, minGrade, src string) {
	if cfg, _, _, _, ok := bootRiskFacts(st); ok && cfg != nil {
		en, g, es, gs := store.ResolveOneSetup(cfg)
		return en, g, es + "/" + gs
	}
	en, g, es, _ := store.ResolveOneSetup(nil)
	return en, g, es + " (no bound strategy readable)"
}

// OneSetupBootLine renders D8. Every count is READ from the counters and the
// episode table; the switch and the grade from the bound strategy; the two
// evidence labels are the dispatch's: the fade is [O] (owner-ruled), the
// follow-plan is [T] (recorded only, round 17).
func OneSetupBootLine(st *store.Store, now time.Time, traderIDs []string, bfV store.OneSetupBackfillResult, bfF store.FollowBackfillResult) string {
	enabled, grade, src := boundOneSetup(st)
	sw := "OFF"
	if enabled {
		sw = "ON"
	}
	// The counters are keyed by the PLAN'S trade date (kernel.PlanTradeDateFor
	// at the seam = the session instance's chain date), so the boot line asks
	// the same resolver: the active session's chain date, else the calendar
	// date. The first boot (01:45 CT 09-11) derived a session-day date instead
	// and printed 0 for a key that held 2 (class 45/49 on this line's own
	// first emission; report §F).
	tradeDate := oneSetupBootTradeDate(now)
	counts := store.OneSetupCounts{}
	follow := store.FollowPlanCounts{}
	if st != nil {
		for _, id := range traderIDs {
			c := store.OneSetupCountsFor(st, id, tradeDate)
			counts.Armable += c.Armable
			counts.Declined += c.Declined
			counts.Level += c.Level
			counts.Play += c.Play
			counts.Day += c.Day
			counts.NotEvaluated += c.NotEvaluated
			counts.Wait += c.Wait
			counts.ObstacleBelowFloor += c.ObstacleBelowFloor
			counts.ObstacleMissing += c.ObstacleMissing
			counts.DeclinedWhileResting += c.DeclinedWhileResting
			counts.Retired += c.Retired
			counts.Readable = counts.Readable || c.Readable
			f := st.TouchOutcomes().CountFollowPlans(id, kernel.CMESessionDayStart(now).UnixMilli())
			follow.Rows += f.Rows
			follow.Breaks += f.Breaks
			follow.Retests += f.Retests
			follow.Reversed += f.Reversed
			follow.RetestJudged += f.RetestJudged
			follow.Readable = follow.Readable || f.Readable
		}
	}
	today := "today armable=n/a declined=n/a (counters unreadable)"
	if counts.Readable {
		today = fmt.Sprintf("today armable=%d declined=%d (level=%d play=%d day=%d not-evaluated=%d waiting=%d) · obstacle-below-floor=%d obstacle-missing=%d declined-while-resting=%d retired=%d",
			counts.Armable, counts.Declined, counts.Level, counts.Play, counts.Day, counts.NotEvaluated, counts.Wait, counts.ObstacleBelowFloor, counts.ObstacleMissing, counts.DeclinedWhileResting, counts.Retired)
	}
	fol := "breaks=n/a retests=n/a role-reversed=n/a (table unreadable)"
	if follow.Readable {
		fol = fmt.Sprintf("breaks=%d retests=%d role-reversed=%d/%d", follow.Breaks, follow.Retests, follow.Reversed, follow.RetestJudged)
	}
	bf := "backfill=not-run"
	if bfV.Ran || bfF.Ran {
		bf = fmt.Sprintf("backfill verdicts recomputed=%d unrecomputable=%d untouched=%d · follow recomputed=%d unrecomputable=%d untouched=%d",
			bfV.Recomputed, bfV.Unrecomputable, bfV.Untouched, bfF.Recomputed, bfF.Unrecomputable, bfF.Untouched)
	}
	return fmt.Sprintf("🎯 one setup: %s[O] · level=best-near-price(min-grade %s)[O] · play=%s · target=first-distinct-eligible-zone · permission-required=yes · map=untouched · %s · follow-plan=RECORDED-ONLY[T] %s · bias-flip=recorded-only · off-switch=one_setup_enabled (source: %s) · %s",
		sw, grade, kernel.OneSetupPlay, today, fol, src, bf)
}

// BackfillOneSetupVerdicts — D9 for the verdicts, three-state. For every
// episode opened at or after sinceMs with no verdict: the LEVEL leg is judged
// against the last candidate-pool read before the episode's open (merged at
// the map's width, grade first, distance second, price from the store's bars
// through the contract filter); the PLAY leg from the plan's scenario whose
// anchor sits on the episode's level; the PERMISSION leg from W2's stamp,
// which was fixed at the same open. Anything the record cannot supply is
// unrecomputable:<which>, never guessed. Rows before sinceMs are untouched.
func (at *AutoTrader) BackfillOneSetupVerdicts(sinceMs int64, now time.Time) store.OneSetupBackfillResult {
	res := store.OneSetupBackfillResult{Ran: true}
	if at == nil || at.store == nil {
		return res
	}
	defer func() {
		if r := recover(); r != nil {
			at.logWarnf("🎯 one setup backfill recovered from panic: %v", r)
		}
	}()
	ts := at.store.TouchOutcomes()
	symbol := at.futuresSymbol()
	res.Untouched = int(ts.CountEpisodesBefore(at.id, sinceMs))
	rows, err := ts.OpenOneSetupCandidates(at.id, sinceMs)
	if err != nil {
		at.logWarnf("🎯 one setup backfill: read failed: %v", err)
		return res
	}
	cfg := at.oneSetupConfig(nil)
	cfg.Enabled = true // the verdict is recomputed as the predicate would have read it, regardless of the switch
	docs := map[string]*kernel.PlanDoc{}
	mark := func(id uint, why string) {
		res.Unrecomputable++
		_ = ts.MarkOneSetupUnrecomputable(id, why)
	}
	for i := range rows {
		r := rows[i]
		// The scenario this episode belongs to: the plan's scenario whose anchor
		// sits on the level (price within the map's width). None → not a
		// scenario's level; two → ambiguous. Both are honest NULLs.
		key := r.PlanID + "#" + fmt.Sprint(r.PlanVersion)
		doc, seen := docs[key]
		if !seen {
			if p, err := at.store.Plan().GetPlan(r.PlanID, r.PlanVersion); err == nil && p != nil {
				var d kernel.PlanDoc
				if json.Unmarshal([]byte(p.Doc), &d) == nil {
					doc = &d
				}
			}
			docs[key] = doc
		}
		if doc == nil {
			mark(r.ID, "no_plan")
			continue
		}
		var sc *kernel.PlanScenario
		hits := 0
		for j := range doc.Scenarios {
			s := doc.Scenarios[j]
			if s.Arm == nil || !s.Arm.Enabled {
				continue
			}
			if a, ok := scenarioAnchorFor(s); ok && math.Abs(a.Price-r.LevelPrice) <= scenarioLinkBand() {
				sc = &doc.Scenarios[j]
				hits++
			}
		}
		if hits == 0 {
			mark(r.ID, "no_scenario_at_level")
			continue
		}
		if hits > 1 {
			mark(r.ID, "ambiguous_scenario")
			continue
		}
		// The map at the open: the last pool read of the same session-day.
		dayStart := kernel.CMESessionDayStart(time.UnixMilli(r.OpenedAtMs)).UnixMilli()
		pool, _, ok := at.store.CandidatePool().PoolReadBefore(at.id, symbol, dayStart, r.OpenedAtMs)
		if !ok {
			mark(r.ID, "no_pool_read")
			continue
		}
		// Price at the open, through the contract filter.
		bh := at.store.BarHistory()
		contract, cok := bh.WindowContract(symbol, r.OpenedAtMs-5*60_000, r.OpenedAtMs+60_000)
		if !cok {
			mark(r.ID, "spans_roll")
			continue
		}
		bars, berr := bh.BarsBetweenOn(symbol, "1m", contract, r.OpenedAtMs-5*60_000, r.OpenedAtMs+60_000)
		if berr != nil || len(bars) == 0 {
			mark(r.ID, "no_bars")
			continue
		}
		price := bars[len(bars)-1].C
		cands := mergedFromPool(pool, price)
		ref := oneSetupLevelRef(*sc, doc, at.dayPlanCfg().GeometryRefIDsEnabled())
		if ref.Price <= 0 {
			ref = kernel.OneSetupLevelRef{Price: r.LevelPrice, Basis: kernel.LevelBasisPriceProximity}
		}
		facts := kernel.OneSetupLevelFacts{Price: price, BandPts: 0, Candidates: cands, Scenario: ref}
		perm := kernel.FadeVerdict{}
		if r.FadePermitted != nil {
			perm.Evaluated = true
			perm.Permitted = *r.FadePermitted
			if !perm.Permitted && r.FadeExclusions != nil {
				for _, n := range strings.Split(*r.FadeExclusions, ",") {
					if n = strings.TrimSpace(n); n != "" {
						perm.Exclusions = append(perm.Exclusions, kernel.FadeExclusion{Name: n})
					}
				}
			}
		}
		v := kernel.OneSetupAllowsAt(time.UnixMilli(r.OpenedAtMs), *sc, facts, perm, cfg)
		stamp := store.OneSetupStamp{Scenario: sc.ID, Verdicts: oneSetupVerdictText(v), Reason: v.Reason, EvaluatedMs: r.OpenedAtMs, Backfill: "recomputed"}
		if err := ts.StampOneSetup(r.ID, stamp); err == nil {
			res.Recomputed++
		}
	}
	return res
}

// mergedFromPool rebuilds a merged map from a pool read: rows within the
// map's width fold into the strongest (highest score); grade from the keeper.
// It is a reconstruction of a RECORDED read, labelled as such by the caller.
func mergedFromPool(pool []store.CandidatePoolRow, price float64) []kernel.MapCandidate {
	sorted := make([]store.CandidatePoolRow, len(pool))
	copy(sorted, pool)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].Score > sorted[j-1].Score; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	var out []kernel.MapCandidate
	for _, p := range sorted {
		if p.LevelPrice <= 0 {
			continue
		}
		merged := false
		for k := range out {
			if math.Abs(out[k].Price-p.LevelPrice) <= scenarioLinkBand() {
				out[k].Names = append(out[k].Names, p.Label)
				out[k].MergedCount++
				merged = true
				break
			}
		}
		if merged {
			continue
		}
		out = append(out, kernel.MapCandidate{Price: p.LevelPrice, Names: []string{p.Label}, Kinds: []kernel.LevelKind{kernel.LevelKind(p.LevelKind)},
			Grade: p.Grade, Score: p.Score, MergedCount: 1, MergedCredit: 1, Distance: p.LevelPrice - price})
	}
	return out
}

// oneSetupBootTradeDate resolves "today" the way the plan chain does.
func oneSetupBootTradeDate(now time.Time) string {
	if sess, ok := kernel.DefaultSessionRegistry().ActiveSession(now); ok {
		if td, ok := kernel.PlanChainTradeDate(sess, now); ok {
			return td
		}
	}
	return plannerTradeDateCT(now)
}
