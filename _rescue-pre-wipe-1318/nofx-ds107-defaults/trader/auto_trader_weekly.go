package trader

// WEEKLY-BIAS WAVE (2026-08-30) — W2/W4/W5 trader layer: the Sunday weekly
// read (planner seat), the mid-week invalidation watch, and the WARN/shadow
// annotators. ALL of it rides the EXISTING cycle — no new loop, no new gate.
//
// LAW (W5.4): nothing here changes seating, grades, gates or sizes. Every
// shadow artifact is a LOG or a telemetry counter — the real system is frozen.
//
// W2 storage contract: one plans-table row per governed week with
// session='WEEKLY' and trade_date = the week's Monday; the doc JSON carries
// facts_hash (sha256 of the rendered facts sections) — no new DB columns.

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
	"nofx/telemetry"
)

const weeklySystemPrompt = "You are a disciplined CME index-futures weekly-bias reasoner. Output ONLY the single JSON object requested — no prose outside the JSON."

// weeklyState is the per-trader weekly machinery state (all touched from the
// run-loop goroutine except the cache read from the monitor goroutine — the
// mutex keeps the two honest).
type weeklyState struct {
	mu             sync.Mutex
	doc            *kernel.WeeklyDoc // cached weekly doc
	docKey         string            // governing Monday of the cached doc
	loaded         bool
	skipLogDate    string          // dedupes the "skip-fresh" line per week
	shadowLog      map[string]bool // "tradeDate|session|roundedPx" → logged (W5.1 dedupe)
	shadowSeatLog  map[string]bool // "tradeDate|session" → reorder line logged
	invalidatedLog map[string]bool // "monday" → invalidation line logged
}

func (w *weeklyState) resetWeek(key string) {
	if w.docKey != key {
		w.docKey, w.doc, w.loaded = key, nil, false
	}
	if w.shadowLog == nil {
		w.shadowLog = map[string]bool{}
	}
	if w.shadowSeatLog == nil {
		w.shadowSeatLog = map[string]bool{}
	}
	if w.invalidatedLog == nil {
		w.invalidatedLog = map[string]bool{}
	}
}

// weeklyReadClaim dedupes the async Sunday read across traders/restarts the
// same way the planner-read claim does (one AI call per week per trader).
var weeklyReadClaim sync.Map // "weekly:<traderID>:<monday>" → struct{}

func claimWeeklyRead(key string) bool {
	_, loaded := weeklyReadClaim.LoadOrStore(key, struct{}{})
	return !loaded
}
func releaseWeeklyRead(key string) { weeklyReadClaim.Delete(key) }

// weeklyBars1m loads the STORED 1m bars for the trader's futures symbol (the
// same table the W1 computations were built for). Empty on a cold store —
// the read then fails loud, never fakes.
func (at *AutoTrader) weeklyBars1m(now time.Time) []market.Kline {
	if at.store == nil {
		return nil
	}
	// ROLL WAVE — the weekly bias is a statement about the contract being
	// TRADED. A read from epoch 0 spanned September and December and built a
	// weekly bar with a 292-point step inside it. Current contract only; the
	// retired contract's weeks are history, not this instrument's.
	contract, _ := at.currentContract(at.futuresSymbol())
	if contract == "" {
		at.logErrorf("📅 WEEKLY READ: no contract named yet — stored bars not read across an unknown roll")
		return nil
	}
	rows, err := at.store.BarHistory().BarsBetweenFromNT8On(at.futuresSymbol(), "1m", contract, 0, now.UnixMilli()) // NT8-only planner door (2026-09-16)
	if err != nil {
		at.logErrorf("📅 WEEKLY READ: stored 1m bars load failed: %v", err)
		return nil
	}
	bars := make([]market.Kline, 0, len(rows))
	for _, r := range rows {
		bars = append(bars, market.Kline{
			OpenTime: r.OpenTimeMs, Open: r.O, High: r.H, Low: r.L, Close: r.C, Volume: r.V,
		})
	}
	return bars
}

// barResolver builds THIS trader's resolver: NT8 BarCache as the native
// fetcher, our persisted 1m as the last rung. One door for every TF.
func (at *AutoTrader) barResolver() *market.BarResolver {
	return &market.BarResolver{
		Native: func(symbol, tf string, count int) []market.Kline {
			if market.FuturesBarsProvider == nil {
				return nil
			}
			return market.FuturesBarsProvider(symbol, tf, count)
		},
		Own1m: func(symbol string, fromMs, toMs int64) []market.Kline {
			if at.store == nil {
				return nil
			}
			contract, _ := at.currentContract(symbol)
			if contract == "" {
				return nil
			}
			rows, err := at.store.BarHistory().BarsBetweenFromNT8On(symbol, "1m", contract, fromMs, toMs) // NT8-only planner door (2026-09-16)
			if err != nil {
				return nil
			}
			out := make([]market.Kline, 0, len(rows))
			for _, r := range rows {
				out = append(out, market.Kline{OpenTime: r.OpenTimeMs, Open: r.O, High: r.H, Low: r.L, Close: r.C, Volume: r.V})
			}
			return out
		},
	}
}

// weeklyDailyBars (BAR-SOURCE WAVE 2026-09-02) is the weekly reader's source.
// It was `weeklyBars1m` — the persisted 1m table, which starts 2026-08-19 and
// yields TWO completed weeks, so the card rendered "WEEKLY thin · low" while
// the NT8 cache held 1500 native DAILY bars back to 2020-11-11.
//
// It resolves "1w", whose ladder is 1d → 1m: native 1w is deliberately
// EXCLUDED because NT8 stamps weekly bars Friday→Thursday while our weekly
// vocabulary is Monday-governed (market.ExcludedNative("1w") carries the
// reason). The returned bars are DAILY; CompletedWeekCandles buckets them with
// weekStartMonday exactly as it bucketed 1m bars — same convention, same
// output shape, more history. The weekly SIGNAL is untouched by this wave.
func (at *AutoTrader) weeklyDailyBars(now time.Time) (bars []market.Kline, source string) {
	s, err := at.barResolver().CompletedBars(at.futuresSymbol(), "1w", 0, now.UnixMilli())
	if err != nil {
		at.logWarnf("📅 WEEKLY READ: resolver failed (%v) — falling back to stored 1m", err)
	}
	if len(s.Bars) > 0 {
		return s.Bars, fmt.Sprintf("%s via %s", s.Source, s.FromTF)
	}
	// The resolver already tries own-1m as its last rung; reaching here means
	// nothing anywhere. Fall back to the legacy path so behaviour is never
	// WORSE than before this wave.
	if legacy := at.weeklyBars1m(now); len(legacy) > 0 {
		return legacy, "legacy_own1m"
	}
	return nil, "none"
}

// weeklyReadVerdict is the pure W2 decision (fixture-tested): before the
// week's read deadline → "wait"; at/after it with no stored doc → "read"
// (covers the boot-backfill case — a Monday boot is past the deadline);
// with a stored doc → "skip" (idempotent — never re-run).
func weeklyReadVerdict(now, deadline time.Time, hasDoc bool) string {
	if now.Before(deadline) {
		return "wait"
	}
	if hasDoc {
		return "skip"
	}
	return "read"
}

// maybeRunWeeklyRead is the W2 scheduler, wired into the EXISTING cycle above
// the session gate (same hoist as maybeFetchCalendar): the Sunday read needs
// no market, no NT8, no account. One AI read per week, idempotent — a stored
// WEEKLY doc for the governed week means never re-run. Boot-backfill: a boot
// AFTER this week's read time with no doc runs exactly once.
func (at *AutoTrader) maybeRunWeeklyRead(now time.Time) {
	if !at.dayPlanEnabled() || at.store == nil || at.exchange != "ninjatrader" {
		return
	}
	monday := kernel.WeekGoverningMonday(now).Format("2006-01-02")
	deadline := kernel.WeeklyReadDeadline(now)
	existing, err := at.store.Plan().GetLatestPlanForTraderSession(monday, "WEEKLY", at.id)
	if err != nil {
		at.logErrorf("📅 WEEKLY READ: doc lookup failed for week %s: %v", monday, err)
		return
	}
	switch weeklyReadVerdict(now, deadline, existing != nil) {
	case "wait":
		return // before this week's read time — nothing to do
	case "skip":
		at.weeklyState.mu.Lock()
		first := at.weeklyState.skipLogDate != monday
		if first {
			at.weeklyState.skipLogDate = monday
		}
		at.weeklyState.mu.Unlock()
		if first {
			at.logInfof("📅 WEEKLY READ skip-fresh — week %s doc already stored (v%d), idempotent.", monday, existing.Version)
		}
		return
	}
	bootBackfill := now.Sub(deadline) > 2*time.Hour // read time long past → this boot caught up
	key := fmt.Sprintf("weekly:%s:%s", at.id, monday)
	if at.refusePlannerClaimWhileHeld(key, "weekly read") { // W-ONE-BUTTON M2 site 5
		return
	}
	if !claimWeeklyRead(key) {
		at.logInfof("📅 WEEKLY READ already in flight for week %s — skipping duplicate call.", monday)
		return
	}
	at.logInfof("📅 WEEKLY READ starting for week %s (boot_backfill=%v)", monday, bootBackfill)
	go func() {
		defer releaseWeeklyRead(key)
		at.runWeeklyRead(now, monday, bootBackfill)
	}()
}

// runWeeklyRead performs ONE read attempt pair (initial + one retry with the
// reject reason appended) and persists the validated doc. The read must never
// crash the trader loop: panic recovery + loud failure logs.
func (at *AutoTrader) runWeeklyRead(now time.Time, monday string, bootBackfill bool) {
	defer func() {
		if r := recover(); r != nil {
			at.logErrorf("⚠️ WEEKLY READ panic recovered for %s: %v", monday, r)
		}
	}()
	bars, barSource := at.weeklyDailyBars(now)
	if len(bars) == 0 {
		at.logErrorf("⚠️ WEEKLY READ FAILED for %s: no bars from any rung of the 1w ladder %v (thin/cold store)", monday, market.LadderFor("1w"))
		return
	}
	at.logInfof("📅 WEEKLY READ %s: %d bar(s) from %s → %d completed week(s) (ladder %v; native 1w excluded: %s)",
		monday, len(bars), barSource, kernel.CompletedWeekCount(bars, now), market.LadderFor("1w"), market.ExcludedNative("1w"))
	price := bars[len(bars)-1].Close
	facts := kernel.ComputeWeeklyFacts(bars, now, price)
	prompt := kernel.BuildWeeklyPrompt(facts)

	client, modelID := at.resolvePlannerClient()
	if client == nil {
		at.logErrorf("⚠️ WEEKLY READ FAILED for %s: no AI client resolved", monday)
		return
	}
	var lastErr string
	for attempt := 1; attempt <= 2; attempt++ { // initial + ONE retry (spec W2)
		raw, err := client.CallWithMessages(weeklySystemPrompt, prompt)
		if err != nil {
			lastErr = err.Error()
			at.logWarnf("📅 WEEKLY READ attempt %d/2 failed for %s: %v", attempt, monday, err)
			continue
		}
		doc, perr := kernel.ParseWeeklyDoc(raw)
		if perr != nil {
			lastErr = perr.Error()
			at.logWarnf("📅 WEEKLY READ attempt %d/2 parse rejected for %s: %v", attempt, monday, perr)
			continue
		}
		if reason := kernel.ValidateWeeklyDoc(doc, kernel.WeeklyRefSet(facts), facts.ThinHistory); reason != "" {
			lastErr = reason
			at.logWarnf("📅 WEEKLY READ attempt %d/2 validator rejected for %s: %s — retrying once with the reason.", attempt, monday, reason)
			prompt = prompt + "\n\nREJECTED by the validator — fix ONLY these and answer again:\n" + reason
			continue
		}
		// Accepted — stamp the audit fields and write the plan row.
		doc.FactsHash = facts.FactsHash
		doc.ThinHistory = facts.ThinHistory
		doc.Conviction = "n/a" // class 50: refs-only — the field is fixed "n/a"
		// CLASS 50 (refs-only wave, 2026-09-02): the deterministic bias rule
		// survives as SHADOW — stamped on the doc and logged so the
		// anti-prediction keeps being measured (calibration report 2026-09-02:
		// holdout hit 25-28%, called-only 45-51%). NOTHING reads it as a
		// direction. Never inverted.
		shadowBias, shadowWhy := kernel.WeeklyRuleBias(facts)
		doc.ShadowBias, doc.ShadowWhy = shadowBias, shadowWhy
		at.logInfof("📅 WEEKLY SHADOW (never a direction): rule bias would have been %s — %s", shadowBias, shadowWhy)
		docJSON, _ := json.Marshal(doc)
		trigger := "sunday_weekly_read"
		if bootBackfill {
			trigger = "weekly_boot_backfill"
		}
		version, werr := at.store.Plan().AppendPlan(&store.PlanDB{
			PlanID:        at.store.Plan().ResolvePlanID(monday, "WEEKLY", at.id),
			StrategyID:    at.id,
			TradeDate:     monday,
			Session:       "WEEKLY",
			TriggerReason: trigger,
			Lifecycle:     "active",
			ModelID:       modelID,
			PromptHash:    facts.FactsHash,
			Doc:           string(docJSON),
		})
		if werr != nil {
			at.logErrorf("⚠️ WEEKLY READ FAILED for %s: plan row write: %v", monday, werr)
			return
		}
		at.logInfof("📅 WEEKLY READ written %s v%d refs_only=true PWH=%.2f PWL=%.2f levels=%d thin=%v shadow_bias=%s facts_hash=%s…",
			monday, version, facts.Refs.PWH, facts.Refs.PWL, len(doc.WeeklyLevels), doc.ThinHistory, shadowBias, facts.FactsHash[:12])
		at.weeklyState.mu.Lock()
		at.weeklyState.resetWeek(monday)
		at.weeklyState.doc, at.weeklyState.loaded = doc, true
		at.weeklyState.mu.Unlock()
		return
	}
	// Second reject → no doc this week (fail-open downstream) + loud line.
	at.logErrorf("⚠️ WEEKLY READ FAILED for %s: %s — no weekly doc this week (sessions render WEEKLY: none; nothing else changes).", monday, lastErr)
	telemetry.IncWeeklyReadFailed(at.id)
}

// weeklyDocCached returns the parsed WEEKLY doc for the current governed week
// (nil when absent/failed). Cached per week so per-cycle callers are cheap.
// ONLY successful loads are cached — a negative (nil) result is retried next
// call, so a cycle that runs before the Sunday weekly read lands can never pin
// "WEEKLY: none" into the planner prompt for the rest of the week (the
// 2026-08-31 owner-reported F1 miss).
func (at *AutoTrader) weeklyDocCached(now time.Time) *kernel.WeeklyDoc {
	if at.store == nil {
		return nil
	}
	monday := kernel.WeekGoverningMonday(now).Format("2006-01-02")
	at.weeklyState.mu.Lock()
	defer at.weeklyState.mu.Unlock()
	at.weeklyState.resetWeek(monday)
	if at.weeklyState.loaded {
		return at.weeklyState.doc
	}
	row, err := at.store.Plan().GetLatestPlanForTraderSession(monday, "WEEKLY", at.id)
	if err != nil || row == nil {
		return nil // not cached — the next call retries the lookup
	}
	var doc kernel.WeeklyDoc
	if json.Unmarshal([]byte(row.Doc), &doc) != nil {
		return nil // not cached — the next call retries the lookup
	}
	at.weeklyState.doc = &doc
	at.weeklyState.loaded = true
	return &doc
}

// CLASS 50 (refs-only wave, 2026-09-02): the W4 mid-week invalidation watch is
// REMOVED — a refs-only doc has no bias and no invalidation, so there is
// nothing to flip and nothing reads bias as a direction anymore. Pre-wave
// stored docs with bias fields are also left untouched: nothing consumes
// them. The pure helpers (ApplyWeeklyDOA / WeeklyInvalidationCrossed) remain
// in kernel for the offline anti-prediction measurement only.

// weeklyConfluenceShadow (W5.1) is the SHADOW scorer: seated levels within
// WEEKLY_CONFLUENCE_BAND_ATR × ATR5m of a weekly-class reference log their
// shadow grade ONCE per level per session, plus one per-session line counting
// how many of the top-8 seats the shadow ordering would change. View only —
// the real seating is already written and never touched.
// Clock seam (class 60): the entry point owns the wall clock and does nothing
// else; the rule lives in the …At body so a test can state its own hour.
func (at *AutoTrader) weeklyConfluenceShadow(tradeDate, session string, levels []kernel.PlanLevel) {
	at.weeklyConfluenceShadowAt(time.Now(), tradeDate, session, levels)
}

func (at *AutoTrader) weeklyConfluenceShadowAt(now time.Time, tradeDate, session string, levels []kernel.PlanLevel) {
	if !at.dayPlanEnabled() || at.store == nil || len(levels) == 0 {
		return
	}
	if market.FuturesBarsProvider == nil {
		return
	}
	// D2 CHOICE (a) — READ THE STORE. WHY: ComputeWeeklyFacts asks for 12
	// COMPLETED WEEKS (kernel/weekly_prompt.go) plus the last 5 weekend gaps.
	// The ring ceiling is 2,500 1m bars = 41.7 h — under two days — so neither
	// could ever have been satisfied from it. The store reaches 21 days today
	// (~3 weeks) and up to the 90-day 1m retention, which is still short of 12
	// weeks: ComputeWeeklyFacts already stamps that honestly via ThinHistory.
	//
	// IT DOES CHANGE WHAT THIS COMPUTES, AND HERE ARE THE NUMBERS (corrected in
	// review, 2026-09-09 — the earlier comment claimed no change, which is
	// class 82). MEASURED against the live store, MNQ 1m, now=2026-09-09:
	//
	//	2,500 bars (43.6 h)  → WeeklyShadowRefs=1 CompletedWeekCount=0 Weeks=0 NWOGs=0
	//	12,000 bars (309.0 h) → WeeklyShadowRefs=3 CompletedWeekCount=2 Weeks=2 NWOGs=2
	//
	// ThinHistory stays TRUE in both (12 completed weeks is still out of reach).
	// The direction is a more accurate count from a deeper tape, but it is a
	// change: the 🌗 SHADOW counters and the WEEKLY line's "(thin history %dw)"
	// clause both move at the first boot. Shadow only — no order path.
	bars1m := at.barsWithStoreDepth(at.futuresSymbol(), "1m", weeklyFactsTapeBars, now)
	bars5m := market.FuturesBarsProvider(at.futuresSymbol(), "5m", kernel.AISVPBarCount)
	price := 0.0
	if len(bars1m) > 0 {
		price = bars1m[len(bars1m)-1].Close
	}
	atr5m := kernel.StaleConfirmATR5m(bars5m)
	if atr5m <= 0 {
		atr5m = market.ExportCalculateATR(bars5m, 14)
	}
	refs := kernel.WeeklyShadowRefs(bars1m, now, price)
	band := kernel.WeeklyConfluenceBandATR()
	mult := kernel.WeeklyShadowMult()

	shadowLevels := make([]kernel.WeeklyShadowLevel, 0, len(levels))
	for _, l := range levels {
		shadowLevels = append(shadowLevels, kernel.WeeklyShadowLevel{Price: l.Price, Label: l.Label, Grade: l.Grade})
	}
	confluent, reorder := kernel.WeeklyShadowReorder(shadowLevels, refs, band, atr5m, mult)

	at.weeklyState.mu.Lock()
	if at.weeklyState.shadowLog == nil {
		at.weeklyState.shadowLog = map[string]bool{}
	}
	if at.weeklyState.shadowSeatLog == nil {
		at.weeklyState.shadowSeatLog = map[string]bool{}
	}
	seatKey := tradeDate + "|" + session
	for _, lv := range shadowLevels {
		if !kernel.WeeklyConfluent(lv, refs, band, atr5m) {
			continue
		}
		key := fmt.Sprintf("%s|%.0f", seatKey, lv.Price*100)
		if at.weeklyState.shadowLog[key] {
			continue
		}
		at.weeklyState.shadowLog[key] = true
		at.logInfof("🌗 SHADOW wk-confl %s@%.2f (%s) real=%s shadow=%.1f — view only, real seating unchanged.",
			lv.Label, lv.Price, session, lv.Grade, float64(kernel.GradeRank(lv.Grade))*mult)
	}
	if !at.weeklyState.shadowSeatLog[seatKey] {
		at.weeklyState.shadowSeatLog[seatKey] = true
		at.logInfof("🌗 SHADOW wk-seating %s %s: %d confluent level(s); shadow top-8 would differ from real on %d seat(s) — counter for the Sep-9 promotion table.",
			tradeDate, session, confluent, reorder)
	}
	at.weeklyState.mu.Unlock()
}

// weeklyCounterShadow (W5.2) — CLASS 50 (refs-only wave): the weekly doc no
// longer carries a direction, so the counter annotation is retired — nothing
// reads bias as a direction anymore, not even in shadow. The rule bias
// survives in shadow_bias on the doc + the WeeklyRuleBias log line (measured
// offline; never inverted). The pure helpers stay in kernel for that
// measurement only.
func (at *AutoTrader) weeklyCounterShadow(decision *kernel.Decision) {
	return // class 50: no weekly direction exists to be counter to
}

// weeklyScenarioGrade resolves the cited scenario's quality grade from the
// active session plan ("" when unknown — treated as non-A by the clauses).
// Clock seam (class 60): the entry point owns the wall clock and does nothing
// else; the rule lives in the …At body so a test can state its own hour.
func (at *AutoTrader) weeklyScenarioGrade(cited string) string {
	return at.weeklyScenarioGradeAt(time.Now(), cited)
}

func (at *AutoTrader) weeklyScenarioGradeAt(now time.Time, cited string) string {
	if at.store == nil || strings.TrimSpace(cited) == "" || cited == "off-plan" {
		return ""
	}
	reg := at.sessionRegistry(now)
	sess, ok := reg.ActiveSession(now)
	if !ok {
		return ""
	}
	tradeDate, okDate := kernel.PlanChainTradeDate(sess, now)
	if !okDate {
		tradeDate = plannerTradeDateCT(now)
	}
	row, err := at.store.Plan().GetLatestPlanForTraderSession(tradeDate, sess.Name, at.id)
	if err != nil || row == nil {
		return ""
	}
	var doc kernel.PlanDoc
	if json.Unmarshal([]byte(row.Doc), &doc) != nil {
		return ""
	}
	for _, s := range doc.Scenarios {
		if strings.EqualFold(strings.TrimSpace(s.ID), strings.TrimSpace(cited)) {
			return s.Quality
		}
	}
	return ""
}

// weeklyDrawAlignTag (W5.3) — CLASS 50 (refs-only wave): the weekly doc has no
// direction anymore, so the draw-alignment tag is pinned "neutral" for every
// row. The kernel.WeeklyDrawAlignTag helper stays for the offline measurement
// of pre-wave rows only.
func (at *AutoTrader) weeklyDrawAlignTag(record *store.DecisionRecord) string {
	return "neutral"
}

// applyWeeklyDecisionShadow is the single per-entry hook: counter annotation
// (W5.2, log-only). The draw-align tag rides saveDecision.
func (at *AutoTrader) applyWeeklyDecisionShadow(decision *kernel.Decision) {
	defer func() {
		if r := recover(); r != nil {
			at.logWarnf("weekly shadow annotation recovered: %v (shadow never affects the trade)", r)
		}
	}()
	at.weeklyCounterShadow(decision)
}

// weeklyFactsTapeBars is the 1m depth the weekly facts read asks for. Named so
// the ask and the disclosure that reports it cannot drift (D2, 2026-09-09).
const weeklyFactsTapeBars = 12000
