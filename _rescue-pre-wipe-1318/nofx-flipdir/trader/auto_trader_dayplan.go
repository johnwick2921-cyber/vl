package trader

import (
	"encoding/json"
	"fmt"
	"math"
	ntTrader "nofx/trader/ninjatrader"
	"sync"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// P1.3 — day-plan durable session-profile snapshot + nPOC provider wiring.
//
// snapshotSessionProfiles runs each cycle but is GATED (futures + day_plan
// enabled) so it is DORMANT by default — crypto and plan-off traders are a
// no-op. When active it persists any newly-completed (frozen) session's volume
// profile, idempotently (SaveIfAbsent), so the map warms FORWARD and a restart
// replays with no loss and no dupes. It also installs the nPOC provider (once)
// so BuildKeyLevelsBlock can surface untested prior-session POCs.

var nakedPOCProviderOnce sync.Once

func (at *AutoTrader) snapshotSessionProfiles() {
	if at.exchange != "ninjatrader" {
		return // futures only
	}
	dp := at.config.StrategyConfig.DayPlan
	if dp == nil || !dp.PlanEnabled {
		return // dormant until the owner enables day_plan
	}
	if market.FuturesBarsProvider == nil {
		return
	}
	symbol := at.config.NinjaTraderSymbol
	if symbol == "" {
		symbol = "MNQ"
	}

	// Install the nPOC + level-state providers once (store-backed, symbol-scoped
	// market facts — shared across traders by design). The ACTIVE-PLAN +
	// SESSION-REGISTRY providers are PER-TRADER (P0-A): re-registered
	// idempotently every cycle under THIS trader's id, never a process-global
	// singleton closing over whoever arrived first.
	st := at.store
	nakedPOCProviderOnce.Do(func() {
		installNakedPOCProvider(st)
		installLevelStateProvider(at, st) // W11b — surface persisted freshness/consumed
		// 1h wave + R4 (2026-08-25) — one boot observability line for the new
		// day-plan knobs so a config question is answered from the log.
		at.logInfof("🗺️ day-plan knobs: seat_1h_zone=%v min_scenario_quality=%s ob_lookback_bars=%d",
			dp.Seat1HZoneEnabled(), dp.MinScenarioQualityFor(""), kernel.OBLookbackBars()) // S2 (2026-09-16) — freshness mode + HTF weight, READ not typed:
		// the mode comes from the resolved knob, the multiplier from the
		// const the scorer actually applies.
		at.logInfof("🧮 freshness: fresh=%s htf×=%g",
			freshnessBootLabel(dp), kernel.HTFScoreMultiplier) // A3 (2026-08-26) — min-SL guard observability (0 = off).
		at.logInfof("🛑 min-sl guard: atr_mult=%.1f level_clearance=%dtick(s)",
			kernel.MinSLATRMult(), kernel.MinSLTickClearance)
		// PLAN-LIFECYCLE WAVE (2026-08-27) — hysteresis + dormant/re-arm +
		// latency routing observability, so the mode is answerable from the log.
		at.logInfof("🧬 plan lifecycle: hysteresis=buffer%.1f×ATR14 confirm=%dclose(s) · flip/death→dormant+auto-rearm (version unchanged, budget untouched) · exec_reasoning=%s plan_reasoning=%s",
			kernel.FlipATRBuffer(), kernel.FlipConfirmCloses(), execReasoningLabel(), planReasoningLabel())
		// Wave 2 armed orders (Phase 2, 2026-08-27) — placement engine mode.
		at.logInfof("⚔️ armed_orders=on place_band=%dt stale_working=%dm test_seam=%s arm_rr=%.1f (gate-at-arm only; market-entry floor %.1f unchanged) (resting limits fill at the authorized price; stale_reeval NOT applied)",
			armedPlaceTicks(), armedWorkingStaleMin(), armedSeamStateLabel(), at.armMinRRFor(nil), at.config.StrategyConfig.RiskControl.MinRiskRewardRatio) // F1a (LONDON-FORENSICS 2026-08-28) — planner completion budget boot line
		// GAR-F1 (2026-08-28) — move_stop identity observability (the
		// #566 dead-cell fix): materialized positions carry the armed
		// ledger's signal identity so BE+40/trailing can address them.
		at.logInfof("🩹 move_stop identity: materialized positions persist armed-ledger signal_id → entry_order_id; move_stop/trailing resolve it (GAR-F1)")
		// GAR-F3 (2026-08-28) — HTF veto mode observability.
		at.logInfof("🛡️ htf veto: mode=%s tf=%s (1h|cross|4h via HTF_VETO_MODE; cross = 1h AND 4h agree)", kernel.HTFVetoMode(), kernel.HTFVetoTF())
		// (the cutover verification quotes it): plan_max_tokens resolved from
		// AI_PLAN_MAX_TOKENS, default 65536 = 2× the observed truncation ceiling.
		at.logInfof("📐 planner cap: plan_max_tokens=%d (AI_PLAN_MAX_TOKENS; default 65536) · truncation → 🚨 WARN, never silent", aiPlanMaxTokens())
		// S1 (2026-09-16) — the STRUCTURE table knob, READ from the bound strategy.
		sEn, sKnown := at.structureMapEnabled()
		at.logInfof("%s", kernel.StructureBootLine(sEn, sKnown))
		at.logPlannerClientBootLine() // class 37 (C7): effective planner client config, resolved
		// ROOT-FIX part B — the shadow A/B instrument's resolved state.
		done, _ := store.ShadowABCount(at.store)
		at.logInfof("%s", ShadowABBootLine(ShadowABEnabled(), ShadowABTarget(), done))
		// BAR-SOURCE WAVE — per-TF source/earliest/count, read from the resolver.
		at.logInfof("%s", NoChaseBootLine())
		at.logInfof("%s", BarSourceBootLine(at.barResolver(), at.futuresSymbol(), time.Now()))
		// HISTORY IMPORT (wave 101) — per-contract × per-TF held history, READ
		// from the store, never a literal. A contract the store has never seen
		// is not printed as zero (A24): the line reports what is HELD.
		at.logInfof("%s", HistoryHeldBootLine(at.store, at.futuresSymbol()))
		// R1 — print again once the NT8 replay has actually landed.
		ntTrader.SetAfterBackfillHook(func() {
			hookNow := time.Now() // `now` at the entry point, handed down (A28)
			at.logInfof("%s", BarSourceBootLineAfterBackfill(at.barResolver(), at.futuresSymbol(), hookNow))
			// BARS HORIZON owner condition (c), 2026-09-09 — the regime
			// input's served window IN DAYS, BEFORE and AFTER, so the change
			// this wave makes to what RVBaseline is fed is visible and dated.
			// It runs HERE because the hook fires after the boot rehydrate has
			// landed (trader/ninjatrader/bar_persist_wire.go), so both halves
			// describe the ring the bot will actually use. Every field is READ
			// (A11); an uncomputed window prints UNKNOWN, never 0 (A24).
			sym := at.futuresSymbol()
			var ring5m []market.Kline
			if market.FuturesBarsProvider != nil {
				ring5m = market.FuturesBarsProvider(sym, "5m", rvBaselineFallback5mBarsAsk)
			}
			at.logInfof("%s", RegimeInputWindowBootLine(
				at.barsWithStoreDepth(sym, "1m", plannerCandleTapeBars, hookNow),
				ring5m, rvBaselineMaxDays, hookNow))
			// NT8-only planner tape (CTO ruling 2026-09-16): the class-82
			// accounting of what the import exclusion moved, both ways.
			at.logPlannerTapeAccounting(sym, ring5m, hookNow)
		})
	})
	installActivePlanProvider(at, st)
	// P0-cleanup (2026-08-19) — soft-alert: guardrails that WOULD have tripped
	// (master OFF) reach the owner's alert feed; they never block.
	kernel.SoftGuardrailFunc = func(trader, what string) {
		at.emitAlert("P1", "guardrail-would-have-tripped",
			fmt.Sprintf("soft-guardrail:%s:%s", trader, shortHash(what)),
			"Guardrail would have tripped (not enforced)",
			what+" — the guardrails master is OFF (owner decision); nothing was blocked. This is what the cage would have caught.")
	}

	bars := market.FuturesBarsProvider(symbol, kernel.AISVPBarInterval, kernel.AISVPBarCount)
	if len(bars) == 0 {
		return
	}
	prof := kernel.BuildSVPProfile(bars, time.Now())
	for _, s := range prof.Sessions {
		if !s.Frozen || len(s.Bins) == 0 {
			continue // only completed sessions with data
		}
		exists, err := at.store.SessionProfile().Exists(symbol, s.Date)
		if err != nil || exists {
			continue
		}
		hi, lo := sessionHiLoFromBins(s.Bins)
		pj, _ := json.Marshal(s)
		wrote, err := at.store.SessionProfile().SaveIfAbsent(&store.SessionProfileDB{
			Symbol:      symbol,
			SessionDate: s.Date,
			POC:         s.POC,
			VAH:         s.VAH,
			VAL:         s.VAL,
			SessHigh:    hi,
			SessLow:     lo,
			ProfileJSON: string(pj),
			CreatedAt:   time.Now().UnixMilli(),
		})
		if err != nil {
			at.logWarnf("🗺️ day-plan: session-profile snapshot failed for %s %s: %v", symbol, s.Date, err)
			continue
		}
		if wrote {
			at.logInfof("🗺️ day-plan: stored session profile %s %s (POC %.2f) — warming forward.", symbol, s.Date, s.POC)
		}
	}
}

func sessionHiLoFromBins(bins []kernel.SVPBin) (hi, lo float64) {
	hi, lo = math.Inf(-1), math.Inf(1)
	for _, b := range bins {
		if b.Price > hi {
			hi = b.Price
		}
		if b.Price < lo {
			lo = b.Price
		}
	}
	if math.IsInf(hi, 1) {
		hi = 0
	}
	if math.IsInf(lo, 1) {
		lo = 0
	}
	return hi, lo
}

// installLevelStateProvider wires kernel.LevelStateProvider to read this store's
// cross-session level_state (freshness A→B→C, consumed) for a level identity — the
// SAME identity (type-from-label + price-bin) W7's writer uses. This is the W11b
// surfacing: consumed levels ROLE-FLIP (flipped label, reduced score) instead of
// disappearing; PLAN STATUS annotates them; P1d aging heals scars across
// session-days. Unknown level → "" (fresh).
func installLevelStateProvider(at *AutoTrader, st *store.Store) {
	// S2 F3 (CTO review) — the own-TF bar series is assembled ONCE per install
	// (not once per HTF level; today that was 339 store queries inside the
	// freshness callback) and passed into the closure as a per-TF cache.
	// Built lazily on the first READ so the builder's upper bound is the READ's
	// now (A28), never time.Now() of its own. The grader's re-entry semantics
	// only read OpenTime/High/Low/Close.
	var barsByTF map[string][]market.Kline
	var barsOnce sync.Once
	build := func(now time.Time) {
		if at == nil || st == nil || st.BarHistory() == nil {
			return
		}
		barsByTF = levelTFBarsCache(st, at.futuresSymbol(), now)
	}
	kernel.LevelStateProvider = func(traderID, symbol string, l kernel.DetectedLevel, now time.Time) string {
		// S2 (2026-09-16) — timeframe-aware freshness. ONLY the HTF branch is
		// routed through the new grader, and only when the knob is ON; every
		// non-HTF level and every OFF config keeps the W7 persisted 1m-touch
		// ladder below byte-identically. `now` is the READ's now, threaded from
		// the scoring call site (class 60 / F4), never time.Now() here.
		if at != nil && at.config.StrategyConfig != nil && at.config.StrategyConfig.DayPlan != nil &&
			at.config.StrategyConfig.DayPlan.LevelsFreshByTFEnabled() &&
			l.HTF && kernel.IsHTFFreshTF(l.TF) {
			barsOnce.Do(func() { build(now) })
			grade, _, _ := kernel.LevelFreshnessByTF(l, now, barsByTF[l.TF])
			return grade
		}
		key := store.MakeLevelKey(traderID, symbol, kernel.LevelTypeFromLabel(l.Label), "", kernel.LevelBinIndex(l.Price))
		cur, err := st.LevelState().Get(key)
		if err != nil || cur == nil {
			return "" // no persisted state → fresh (pre-W11b behavior)
		}
		return store.AgedFreshness(cur, now)
	}
}

// freshnessBootLabel resolves the S2 freshness-mode boot label from the knob:
// "1m" (today's ladder) or "by-tf". READ from the resolved config, never typed.
func freshnessBootLabel(dp *store.DayPlanConfig) string {
	if dp.LevelsFreshByTFEnabled() {
		return "by-tf"
	}
	return "1m"
}

// levelTFBarsCache assembles, ONCE per install, the per-TF bar series the S2
// grader reads: the live ring first, then the persisted NT8 history leg for the
// newest usable contract (same combination the nPOC provider uses). F2 (CTO
// review): the ring and the store overlap on recent bars — deduped by OpenTime;
// CloseTime is left 0 because the grader never reads it (it reads
// OpenTime/High/Low/Close only). `now` is the READ's now (A28): the store query
// upper bound is now.UnixMilli(), never time.Now().
func levelTFBarsCache(st *store.Store, symbol string, now time.Time) map[string][]market.Kline {
	out := map[string][]market.Kline{}
	tfs := []string{"1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w"}
	for _, tf := range tfs {
		var bars []market.Kline
		if market.FuturesBarsProvider != nil {
			bars = market.FuturesBarsProvider(symbol, tf, 500)
		}
		contract, _ := st.BarHistory().LatestContract(symbol)
		if contract != "" {
			if old, err := st.BarHistory().BarsBetweenFromNT8On(symbol, tf, contract, 0, now.UnixMilli()); err == nil {
				for _, b := range old {
					bars = append(bars, market.Kline{
						OpenTime: b.OpenTimeMs,
						Open:     b.O, High: b.H, Low: b.L, Close: b.C, Volume: b.V,
					})
				}
			}
		}
		// F2 — ring + store overlap: one bar = one OpenTime.
		seen := map[int64]bool{}
		deduped := bars[:0:0]
		for _, b := range bars {
			if seen[b.OpenTime] {
				continue
			}
			seen[b.OpenTime] = true
			deduped = append(deduped, b)
		}
		out[tf] = deduped
	}
	return out
}

// installNakedPOCProvider wires kernel.NakedPOCProvider to read this store's
// session_profiles and compute the untested (naked) POCs for a symbol.
func installNakedPOCProvider(st *store.Store) {
	kernel.NakedPOCProvider = func(symbol string) []kernel.DetectedLevel {
		rows, err := st.SessionProfile().List(symbol, 30)
		if err != nil || len(rows) == 0 {
			return nil
		}
		now := time.Now()
		// Sessions older than ~5 CME days rank as weekly (HTF grade bump).
		weeklyBefore := kernel.CMESessionDayKey(now.AddDate(0, 0, -5))
		pocs := make([]kernel.PriorPOC, 0, len(rows))
		for _, r := range rows {
			pocs = append(pocs, kernel.PriorPOC{
				SessionDate: r.SessionDate,
				POC:         r.POC,
				Weekly:      r.SessionDate < weeklyBefore,
			})
		}
		var bars []market.Kline
		if market.FuturesBarsProvider != nil {
			bars = market.FuturesBarsProvider(symbol, kernel.AISVPBarInterval, kernel.AISVPBarCount)
		}
		// S4 (mega-research 2026-08-26) — the 2000-bar slice covers ~2 sessions
		// but the provider feeds up to 30 stored POCs; a POC touched on day 3
		// could never retire. The bars table supplies the historical leg.
		if len(bars) > 0 {
			earliest := bars[0].OpenTime
			// ROLL WAVE — the historical leg is the CURRENT contract's. A POC
			// seated on the retired contract's scale must not be "touched" by
			// bars 292 points away on the new one.
			// This installer has no trader handle, so the contract is the
			// store's newest usable stamp — the same fallback the persist
			// wire uses before an ACK, and the same value the ACK converges
			// to within seconds of a boot. Empty → the historical leg is
			// skipped, never read unfiltered.
			contract, _ := st.BarHistory().LatestContract(symbol)
			if old, err := st.BarHistory().BarsBetweenFromNT8On(symbol, "1m", contract, 0, earliest); contract != "" && err == nil && len(old) > 0 { // NT8-only planner door (2026-09-16): an imported bar must not retire a POC
				combined := make([]market.Kline, 0, len(old)+len(bars))
				for _, b := range old {
					combined = append(combined, market.Kline{OpenTime: b.OpenTimeMs, CloseTime: b.OpenTimeMs + 59_999, Open: b.O, High: b.H, Low: b.L, Close: b.C, Volume: b.V})
				}
				combined = append(combined, bars...)
				bars = combined
			}
		}
		return kernel.NakedPOCs(pocs, bars, now)
	}
}
