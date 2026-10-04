package trader

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"vl/kernel"
	"vl/logger"
	"vl/market"
	"vl/mcp"
	"vl/store"
	"vl/telemetry"
)

// reasoningWire maps a reasoning knob (off|fast|low|high|max) to DeepSeek wire
// values (thinking mode + reasoning_effort). "fast" maps to effort "low" —
// DeepSeek only accepts low|high|max, and low is the cheapest/fastest.
// Returns (mode, effort, label).
func reasoningWire(raw, def string) (string, string, string) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		v = def
	}
	switch v {
	case "off":
		return "disabled", "", "off"
	case "fast":
		return "enabled", "low", "fast→low"
	case "low":
		return "enabled", "low", "low"
	case "high":
		return "enabled", "high", "high"
	case "max":
		return "enabled", "max", "max"
	}
	// unknown knob — fall back to the cheapest safe wire values, never recurse.
	switch def {
	case "off":
		return "disabled", "", "off"
	case "max":
		return "enabled", "max", "max"
	case "high":
		return "enabled", "high", "high"
	}
	return "enabled", "low", "low"
}

// execReasoningWire is the EXECUTOR in-loop thinking (AI_EXEC_REASONING,
// default fast) — entries must land fresh.
func execReasoningWire() (string, string) {
	m, e, _ := reasoningWire(os.Getenv("AI_EXEC_REASONING"), "fast")
	return m, e
}

func execReasoningLabel() string {
	_, _, l := reasoningWire(os.Getenv("AI_EXEC_REASONING"), "fast")
	return l
}

// planReasoningWire is the PLANNER read thinking (AI_PLAN_REASONING, default
// max) — full reasoning is reserved for the once-per-session plan reads.
func planReasoningWire() (string, string) {
	m, e, _ := reasoningWire(os.Getenv("AI_PLAN_REASONING"), "max")
	return m, e
}

func planReasoningLabel() string {
	_, _, l := reasoningWire(os.Getenv("AI_PLAN_REASONING"), "max")
	return l
}

// fastMarketATR — F3 (waterfall-class wave, 2026-08-28): the drift threshold in
// ATR5m multiples (FAST_MARKET_ATR, default 1.5). A wake read whose price has
// moved more than this since the last plan write runs on the fast wire.
func fastMarketATR() float64 {
	if v := os.Getenv("FAST_MARKET_ATR"); v != "" {
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && n > 0 {
			return n
		}
	}
	return 1.5
}

// fastMarketReasoningWire — F3: FAST_MARKET_REASONING, default fast. Fast-tape
// reads must return fast (<120s target), not deep-think 6-minute answers.
func fastMarketReasoningWire() (string, string) {
	m, e, _ := reasoningWire(os.Getenv("FAST_MARKET_REASONING"), "fast")
	return m, e
}

func fastMarketReasoningLabel() string {
	_, _, l := reasoningWire(os.Getenv("FAST_MARKET_REASONING"), "fast")
	return l
}

// resolvePromptVariant picks the live AI prompt mode (Strategy Studio Phase 2).
// A non-empty per-strategy saved variant wins; otherwise the original venue
// rule applies — CME futures (NinjaTrader) → "futures", everything else →
// "balanced". This is the back-compat guarantee: an empty savedVariant resolves
// EXACTLY as the pre-Phase-2 code, so strategies with no saved variant are
// byte-identical. Prompt-layer only — it never touches a risk gate.
func resolvePromptVariant(exchange, savedVariant string) string {
	if v := strings.TrimSpace(savedVariant); v != "" {
		return v
	}
	if exchange == "ninjatrader" {
		return "futures"
	}
	return "balanced"
}

// runCycle runs one trading cycle (using AI full decision-making)
// stampGuardrailSkip (F10) records the TRUTH for a cycle held by a guardrail/gate:
// a named execution_status + the gate reason, never a silently-empty success (the
// "ghost record" bug where guardrail-skip cycles were saved success=1 with an empty
// prompt). An empty prompt on such a record is fine — the pre-prompt gates fire
// before any prompt is built.
func stampGuardrailSkip(record *store.DecisionRecord, reason string) {
	record.Success = false
	record.ExecutionStatus = "guardrail_skip"
	record.RiskCheckPassed = false
	record.RiskCheckError = reason
	record.ErrorMessage = "guardrail_skip: " + reason
}

// The decision-call timeout is CONFIG-DRIVEN (mcp.ResolvedAITimeout — env
// AI_HTTP_TIMEOUT_SECONDS, default 300s), no literal left on the path.
//
// HISTORY of the literal this replaces: a 180s cap was chosen when normal calls
// averaged ~51s, to keep a cycle inside one 5m bar. Then max_tokens was raised
// from the truncating 2000 default and reasoning responses legitimately run
// 150s+ — the observed 150565ms SUCCESSFUL call sat 30s under the cap, and the
// slower tail died mid-read ("failed to read response: context deadline
// exceeded"), every death a missed decision (incident 2026-08-18, zero-trade
// cause A). A slow call that finishes is handled: staleBarDiscard() throws away
// a decision computed on a bar the market has already moved past, so completing
// late is safe while dying mid-read never is.

// staleBarDiscard reports whether a decision must be DISCARDED because the bar
// it was computed on (decisionBarCloseMs) is no longer the latest closed primary
// bar (latestClosedMs > decisionBarCloseMs) — the AI call spanned a bar close,
// so acting on it would trade on data the market has already moved past.
// haveBar=false (no bars / provider down) never discards: absent evidence, the
// existing stale-data armor (B4) is the only judge. decisionBarCloseMs==0 means
// the cycle never captured a bar (e.g. crypto) → never discards.
func orNight(s string) string {
	if s == "" {
		return "night"
	}
	return s
}

func staleBarDiscard(decisionBarCloseMs, latestClosedMs int64, haveBar bool) bool {
	return haveBar && decisionBarCloseMs > 0 && latestClosedMs > decisionBarCloseMs
}

// applyDecisionCallTimeout aligns the futures decision client with the ONE
// config-driven AI timeout — futures (ninjatrader) only. Crypto and the planner
// client (a separate client, auto_trader_planner.go) already inherit the same
// resolved value through DefaultConfig, so executor and planner can no longer
// diverge (the 180s-vs-300s split was defect class 7).
func applyDecisionCallTimeout(mcpClient mcp.AIClient, exchange string) {
	if mcpClient == nil || exchange != "ninjatrader" {
		return
	}
	mcpClient.SetTimeout(mcp.ResolvedAITimeout())
}

func (at *AutoTrader) runCycle() error {
	at.callCount++

	logger.Info("\n" + strings.Repeat("=", 70) + "\n")
	logger.Infof("⏰ %s - AI decision cycle #%d", kernel.FormatCT(time.Now()), at.callCount)
	logger.Info(strings.Repeat("=", 70))

	// 0. Check if trader is stopped (early exit to prevent trades after Stop() is called)
	at.isRunningMutex.RLock()
	running := at.isRunning
	at.isRunningMutex.RUnlock()
	if !running {
		at.logInfof("⏹ Trader is stopped, aborting cycle #%d", at.callCount)
		return nil
	}

	// W3/F0 — CALENDAR PRODUCER, hoisted ABOVE the session gate: fetching the
	// week's calendar needs no market, no NT8 and no account, and MUST run on a
	// weekend/closed-hours boot so the T1 blackout slices exist BEFORE the next
	// open (the F0 live finding: a Saturday restart never reached the producer —
	// the session gate below skipped the entire cycle all weekend). Gated on
	// day_plan; throttled; idempotent (skip-fresh).
	at.maybeFetchCalendar(time.Now())

	// W2 (weekly-bias wave) — the SUNDAY WEEKLY READ used to run here, above
	// the session gate. CLASS 36 (2026-09-01): runCycle itself sits behind the
	// bar-close gate and the no-new-data dedup in tickOnce, so on a closed
	// market the read was still data-gated (31 minutes late 2026-08-30). It now
	// runs on the wall-clock evaluator (evaluateWallClockWeeklyRead, called at
	// the top of tickOnce beside the session reads).

	// P4 (ledger-close 2026-08-19) — HALF-DAYS PRODUCER, also above the session
	// gate (same F0 reasoning: a weekend boot must seed Monday's early close
	// BEFORE the open). Once per session-day; idempotent merge; fail-open.
	at.maybeSeedHalfDays(time.Now())

	// P5 (ledger-close 2026-08-19) — optional daily AI balance check
	// (AI_BALANCE_WARN, default OFF). Never blocks; WARN + P1 below threshold.
	at.maybeCheckAIBalance(time.Now())

	// 0a. PART A — CME SESSION GATE (hoisted to the TOP, before the account gate
	// and buildTradingContext). When the futures market is closed we skip the
	// ENTIRE cycle — no context build, no NT8 round-trips, no AI — and idle with
	// a longer cadence, logging only on the open⇄closed edge. This is the
	// approved fix for the "bot scans while the market is closed" symptom.
	if at.cmeSessionClosedSkip() {
		return nil
	}

	// 0b. MULTI-ACCOUNT GATE (Stage 1): a NinjaTrader trader must have an EXPLICITLY
	// chosen NT8 account before it trades — never auto-trade on whatever account NT8
	// streams (that could be the LIVE account). The pick is persisted on the trader
	// row by handleSelectAccount; until it's set, skip the whole cycle (no decision,
	// no order). Re-read each cycle so a fresh pick opens the gate without a restart.
	if at.exchange == "ninjatrader" {
		chosenAccount := ""
		if tr, err := at.store.Trader().GetByID(at.id); err == nil && tr != nil {
			chosenAccount = tr.Account
		}
		if chosenAccount == "" {
			at.logWarnf("🚫 No NT8 account selected for this trader — skipping cycle #%d. Pick an account in the dashboard to start trading.", at.callCount)
			return nil
		}
	}

	// 0c. B5 — DEAD-MAN WATCHDOG. Observe the NT8 TCP link each cycle: a dropped
	// link blocks NEW entries (open-position management/exits are never gated) and,
	// on reconnect, holds entries blocked until a clean positions/orders
	// reconciliation, then auto-resumes. Non-NT traders are a no-op (crypto
	// byte-identical). The block itself is enforced in executeDecisionWithRecord.
	at.driveDeadManWatchdog()

	// B6 — GATE-BLOCK COUNTERS: advance the per-trader/session-day tally to today's
	// CME session-day. The first trader across the 17:00 CT rollover boundary logs a
	// one-line journal summary of the ending day's gate blocks, then the table
	// resets. Individual gate sites increment via telemetry.IncGateBlock.
	if summary := telemetry.RolloverGateBlocks(kernel.CMESessionDayStart(time.Now()).UnixMilli()); summary != "" {
		at.logInfof("📊 %s", summary)
	}
	// P0-cleanup — the structured error-event day rolls on the same boundary
	// (idempotent: same-day calls adopt the day; a changed day resets the table).
	telemetry.SetErrorSessionDay(kernel.CMESessionDayStart(time.Now()).UnixMilli())

	// P1.3 — DURABLE SESSION-PROFILE SNAPSHOT (day-plan). Gated (futures +
	// day_plan enabled) → DORMANT by default; idempotent → restart-safe, no
	// dupes. Persists newly-completed session profiles so nPOC/multi-day levels
	// have cross-session memory (warms forward).
	at.snapshotSessionProfiles()

	// P3.6-D — NIGHT MODE: observe night↔day transitions (event on the edge).
	// Reads + entries are already night-safe (session gate); this makes the state
	// explicit. Gated → dormant. Restart during night resumes cleanly.
	at.observeNightEdge()

	// P3.3 — PLANNER READ JOBS: MOVED to tickOnce's wall-clock evaluation
	// (class 32, 2026-08-31). The scheduled session read fires on wall-clock
	// every tick, BEFORE the data-gated skips, so a quiet tape or the
	// 16:00-17:00 CME halt can never delay it (tonight: the 16:30 ASIA read
	// sat behind cycle_skip=no_new_data until ~17:00:03). See
	// evaluateWallClockSessionReads in auto_trader_clock.go.

	// CLASS 50 (refs-only wave, 2026-09-02): the W4 mid-week invalidation
	// watch is REMOVED — refs-only weekly docs carry no bias/invalidation,
	// so there is nothing to flip. The rule bias survives as shadow
	// (stamped + logged by the Sunday read), never read as a direction.

	// P3.6-A — DIGEST WRITERS: 3-line session digest at each session close + the
	// daily roll-up at the trade-date close. Idempotent; gated → dormant.
	at.maybeWriteDigests()

	// B-fix — ALERT-FEED PRUNE: hides ACKED P2 alerts older than 7 days, once
	// per CME session-day (PruneAckedOlderThan previously had no caller).
	at.maybePruneAckedAlerts(time.Now())

	// W5 — LEARNING LOOP on REAL exits: grade every ungraded closed trade (NT8 OCO
	// SL/TP, EOD-flat, manual) — the paths the AI decision cycle never sees.
	// Idempotent; gated → dormant.
	at.maybeRecordClosedTradeAnalytics()

	// W7 — LEVEL-STATE WRITER: persist each active level's cross-session identity +
	// state (times-tested / consumed / freshness) so a level burned in one session
	// can't return fresh in the next; re-arm reads the persisted cooldown. Gated →
	// dormant; idempotent (EnsureLevel preserves prior state).
	at.recordLevelState()

	// W16/R1 — SCENARIO-STATE WRITER: derive each scenario's live status from the
	// same P0.4 facts and persist it, so the card stops painting every play
	// "armed". Reporting only — never touches the prompt. Gated → dormant.
	at.recordScenarioState()

	// P5.6 — WEEKLY matched-random eval (fixed cadence, never nightly re-peek):
	// on Sundays, freeze one honesty-gate snapshot per ISO week. Idempotent
	// (first-writer-wins across traders); gated → dormant.
	at.maybeRunWeeklyMatchedRandom(time.Now())

	// P2.3 — EOD-FLAT: at/after the session flat time, force-close any open
	// position via the trader close path (bypasses hold-lock naturally, RECON
	// #10), then skip the rest of the cycle. Runs BEFORE skip-while-open so a held
	// trade is FLATTENED at the close, not merely skipped. Gated → dormant.
	if at.enforceEODFlat() {
		return nil
	}

	// W3.4 — T1 FORCE-FLAT (research v5 C.5): from T-2min before each red-news
	// blackout through the window, force-close open positions (FOMC/NFP must
	// never ride through the event). Also before skip-while-open; when no
	// positions remain it no-ops and the cycle continues into the blackout
	// no-trade gate (which blocks new entries).
	if at.enforceT1ForceFlat() {
		return nil
	}

	// PHASE 3.5 — clock health at each SESSION ROLL (log-only). Detects the
	// active-session name changing between cycles (incl. →night as ""). Hoisted
	// ABOVE skip-while-open (in-position silence fix 2026-08-19): a session roll
	// during a held trade must still be observed.
	if at.config.Exchange == "ninjatrader" {
		nowRoll := time.Now()
		cur := ""
		if sess, ok := at.sessionRegistry(nowRoll).ActiveSession(nowRoll); ok {
			cur = sess.Name
		}
		if cur != at.lastClockHealthSession {
			kernel.LogClockHealth("session-roll:"+orNight(cur), at.futuresSymbol())
			at.lastClockHealthSession = cur
		}
	}

	// P2.2 note — skip-while-open RELOCATED below buildTradingContext +
	// saveEquitySnapshot (in-position silence fix 2026-08-19). Skipping HERE
	// froze the equity curve and the decision feed for the whole life of every
	// position (#521: 1 scan in 82 min; #522: 1 scan in 16 min — the owner's
	// "updates stop while position open"). Only the AI call may be skipped, and
	// only AFTER snapshot+broadcast.

	// Create decision record
	record := &store.DecisionRecord{
		ExecutionLog: []string{},
		Success:      true,
		CycleTrigger: at.cycleTrigger, // Phase 2/4: "" (timer) | "stale_dodge" | "post_exit"
	}
	if at.cycleTrigger == "post_exit" {
		at.logInfof("↻ cycle_trigger=post_exit — immediate rescan after a position close (all gates apply; prompt identical to a timer cycle).")
	}

	// (A11, fail-register wave) The legacy at.stopUntil consumer that sat here
	// is GONE — it had no producer anywhere (a pause switch that never was,
	// anatomy order-correction #5) and blocked the WHOLE cycle, contradicting
	// the owner pause contract. The ONE live pause is pauseUntilMs
	// (auto_trader_pause.go), enforced entry-only in executeDecisionWithRecord.

	// U1 3.1 — the feed-down watch moved to monitorTick (the 60s wall-clock
	// ticker in auto_trader_risk.go): inside this cycle it was doubly dead —
	// skip-while-open returned before it while holding, and a dead feed stops
	// the bar-close cadence, so the cycle that would report the outage never
	// fired (in-position silence fix 2026-08-19).

	// 2. Reset daily P&L. AUDIT NOTE (2026-08-18, report-only): this is a
	// rolling-24h window where CME session-day scope is intended, AND nothing
	// ever writes at.dailyPnL (grep: reset + one display read only) — it is a
	// permanently-zero display field. Every real daily guard reads the store.
	// Left as-is: wiring it up is a behavior change outside the timegate train.
	if time.Since(at.lastResetTime) > 24*time.Hour {
		at.dailyPnL = 0
		at.lastResetTime = time.Now()
		logger.Info("📅 Daily P&L reset")
	}

	// 4. Collect trading context
	ctx, err := at.buildTradingContext()
	if err != nil {
		at.logErrorf("failed to build trading context: %v", err)
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("Failed to build trading context: %v", err)
		at.saveDecision(record)
		return fmt.Errorf("failed to build trading context: %w", err)
	}

	// MENTOR P3 — one evaluator tick per new 1m close. Inert unless the
	// per-strategy mentor_mode is ON; placements stay behind the MENTOR_PLACE
	// env gate until P1 (#309) lands (L4: default OFF, byte-identical bot).
	at.mentorTick(ctx)

	// G2 (regime wave 2026-08-21) — per-cycle STRUCTURE snapshot: computed from
	// the same 1m cache every other futures consumer reads, threaded into the
	// executor prompt (engine_prompt.go) and persisted on the decision row —
	// G1/G4/G8's input and future forensics' gold.
	if market.FuturesBarsProvider != nil {
		bars1m := market.FuturesBarsProvider(at.futuresSymbol(), kernel.AISVPBarInterval, kernel.AISVPBarCount)
		ctx.Structure = kernel.StructureSnapshot(bars1m, time.Now().UnixMilli())
		if blob, jerr := json.Marshal(ctx.Structure); jerr == nil {
			record.StructureJSON = string(blob)
		}
	}

	// G1 (regime wave 2026-08-21) — HTF veto inputs: Studio toggle (default ON,
	// nil-safe) + the veto timeframe (env HTF_VETO_TF, default 1h). The gate
	// itself runs inside validateDecision (after min-conf, before sizing).
	if sc := at.GetStrategyConfig(); sc != nil {
		ctx.HTFVetoEnabled = sc.HTFVetoEnabled()
	} else {
		ctx.HTFVetoEnabled = true // shipped default when no config is present
	}
	ctx.HTFVetoTF = kernel.HTFVetoTF()

	// G4 (regime wave 2026-08-21) — transition stand-down state machine: opens
	// on an unconfirmed counter-trend CHoCH/MSS on the plan's bias TF (15m),
	// closes on flip/re-plan, BOS resumption or the TRANSITION_MAX_MIN cap;
	// wires the executor gate + the plan-card chip mirror.
	at.observeTransitionStanddown(ctx)

	// C6 (2026-08-25) — executor dead-plan gate input: re-evaluate the active
	// plan's death on the SAME bars every cycle; entries are refused inside
	// validateDecision (management/closes proceed).
	ctx.ExecutorPlanDead = at.executorPlanDeadReason()
	// Wave 2 armed orders (2026-08-27) — Phase 1 arming contract. No-op until a
	// plan carries arm specs; cancels on dormant/session-end per 1.4/2.4.
	at.maybeManageArmedOrders(ctx.Structure)
	// Phase 3 — the executor prompt shows its resting orders' live state.
	if eng := at.strategyEngine; eng != nil {
		eng.SetArmedContext(at.armedLines())
	}

	// R4 (2026-08-25) — min_scenario_quality gate inputs: the resolved floor
	// (default C = no restriction) + the active plan's scenario qualities.
	ctx.MinScenarioQuality = "C"
	ctx.PlanScenarioQuality = map[string]string{}
	if sc := at.GetStrategyConfig(); sc != nil && sc.DayPlan != nil {
		ctx.MinScenarioQuality = sc.DayPlan.MinScenarioQualityFor(at.activeSessionName(time.Now()))
		if ap := kernel.ActivePlanFor(at.id, at.futuresSymbol()); ap != nil {
			for _, s := range ap.Doc.Scenarios {
				ctx.PlanScenarioQuality[s.ID] = s.Quality
			}
		}
	}

	// Plan 4 Stage 4 — defer-until-balance guard (NinjaTrader TCP only)
	// If equity is 0 and no account_balance frame has arrived yet, skip the cycle silently.
	// This prevents phantom HOLD decisions while waiting for the AddOn to connect.
	// Once the first account_balance arrives, equity > 0, and the gate opens normally.
	if ctx.Account.TotalEquity == 0 && !at.HasReceivedBalance() {
		// P0-cleanup (2026-08-19) — was a completely silent return (no row,
		// no log). Now it logs once per boot and records the skip.
		at.logWarnf("⏳ skipping decision cycle #%d — no balance frame yet (equity 0, waiting for the NT8 AddOn)", at.callCount)
		telemetry.RecordError(at.id, "no_balance_frame", "equity 0 and no account_balance frame received yet", telemetry.CostNone)
		return nil
	}

	// Save equity snapshot independently (decoupled from AI decision, used for drawing profit curve)
	// NOTE: Must be called BEFORE candidate coins check to ensure equity is always recorded
	at.saveEquitySnapshot(ctx)

	// P2.2 — SKIP-WHILE-OPEN (relocated 2026-08-19, in-position silence fix).
	// IN-POSITION CONTRACT: while holding, everything above still ran — session
	// reads, EOD-flat, clock health, context build, equity snapshot — so the
	// guards stay live and the dashboard keeps moving. ONLY the AI decision is
	// skipped (spend saving), as a documented branch AFTER snapshot+broadcast,
	// never before. The trade itself is managed outside this cycle: the NT8 OCO
	// bracket, auto-breakeven (60s risk loop), and close-sync/reconcile. Gated
	// on day_plan → dormant by default.
	if skip, why := at.skipWhileOpen(); skip {
		// Phase 3 (final-bundle): ai_watch is the DEFAULT in-position mode — a
		// full WATCH-ONLY cycle replaces the silent skip (the watch row IS the
		// heartbeat). bracket_only keeps the legacy skip byte-identical below.
		if at.positionMode() == PositionModeWatch {
			return at.runWatchCycle(ctx, record)
		}
		at.logInfof("🧘 skip-while-open: holding %s — AI decision skipped for cycle #%d (snapshot+equity recorded; bracket/breakeven manage the trade).", why, at.callCount)
		record.Success = true
		record.ExecutionLog = append(record.ExecutionLog,
			fmt.Sprintf("skip-while-open: holding %s — AI decision skipped after snapshot+equity (in-position heartbeat)", why))
		record.AccountState = store.AccountSnapshot{
			TotalBalance:          ctx.Account.TotalEquity,
			AvailableBalance:      ctx.Account.AvailableBalance,
			TotalUnrealizedProfit: ctx.Account.UnrealizedPnL,
			PositionCount:         ctx.Account.PositionCount,
			InitialBalance:        at.initialBalance,
		}
		at.saveDecision(record)
		return nil
	}

	// Phase 3 — flat again: clear watcher hysteresis + thesis memory so the next
	// position starts a fresh episode.
	at.pruneWatchState()

	// If no candidate coins available, log but do not error
	if len(ctx.CandidateCoins) == 0 {
		at.logInfof("ℹ️ No candidate coins available, skipping this cycle")
		record.Success = true // Not an error, just no candidate coins
		record.ExecutionLog = append(record.ExecutionLog, "No candidate coins available, cycle skipped")
		record.AccountState = store.AccountSnapshot{
			TotalBalance:          ctx.Account.TotalEquity,
			AvailableBalance:      ctx.Account.AvailableBalance,
			TotalUnrealizedProfit: ctx.Account.UnrealizedPnL,
			PositionCount:         ctx.Account.PositionCount,
			InitialBalance:        at.initialBalance,
		}
		at.saveDecision(record)
		return nil
	}

	logger.Info(strings.Repeat("=", 70))
	for _, coin := range ctx.CandidateCoins {
		record.CandidateCoins = append(record.CandidateCoins, coin.Symbol)
	}

	at.logInfof("📊 Account equity: %.2f | Available: %.2f | Positions: %d",
		ctx.Account.TotalEquity, ctx.Account.AvailableBalance, ctx.Account.PositionCount)

	// 5. Use strategy engine to call AI for decision
	at.logInfof("🤖 Requesting AI analysis and decision... [Strategy Engine]")
	// Plan 4 Task 25 — decision latency timer (start)
	decisionStart := time.Now()
	// LATENCY ROUTING (plan-lifecycle wave, 2026-08-27) — executor in-loop
	// calls run CHEAP thinking (AI_EXEC_REASONING, default fast → wire effort
	// low) so entries stop landing stale; reasoning=max is reserved for planner
	// reads. Re-asserted per call because the planner may share this client.
	eMode, eEffort := execReasoningWire()
	mcp.ApplyThinking(at.mcpClient, eMode, eEffort)
	// P0-latency — capture the bar this decision will be computed on (the latest
	// closed primary bar). If a NEWER bar closes while the AI call is in flight,
	// the decision is discarded below instead of being acted on stale data.
	decisionBarCloseMs, _ := at.latestClosedPrimaryBarMs()
	// Prompt mode (Strategy Studio Phase 2): a per-strategy saved variant wins;
	// when NONE is saved we keep the original venue rule EXACTLY (see
	// resolvePromptVariant) so strategies with no saved variant are byte-
	// identical to the prior behavior. Read the saved variant null-safely.
	savedVariant := ""
	if eng := at.strategyEngine; eng != nil {
		if cfg := eng.GetConfig(); cfg != nil {
			savedVariant = cfg.PromptVariant
		}
	}
	promptVariant := resolvePromptVariant(at.exchange, savedVariant)
	aiDecision, err := kernel.GetFullDecisionWithStrategy(ctx, at.mcpClient, at.strategyEngine, promptVariant)
	// Plan 4 Task 25 — decision metrics
	telemetry.DecisionLatency.WithLabelValues(at.id).Observe(time.Since(decisionStart).Seconds())
	// Discard-burn 2.1 — feed the dodge's rolling average (last 20 calls).
	at.recordAICallMs(time.Since(decisionStart).Milliseconds())
	if aiDecision != nil {
		for _, d := range aiDecision.Decisions {
			status := "queued"
			if err != nil {
				status = "rejected"
			}
			telemetry.DecisionsTotal.WithLabelValues(at.id, d.Action, status).Inc()
		}
	}

	if aiDecision != nil && aiDecision.AIRequestDurationMs > 0 {
		record.AIRequestDurationMs = aiDecision.AIRequestDurationMs
		// C-fix (2026-08-18): ai_latency_ms is the field the DecisionAudit UI
		// actually reads; it was never written in production (always 0). Mirror
		// the measured duration so the audit column shows the real number.
		record.AILatencyMs = aiDecision.AIRequestDurationMs
		at.logInfof("⏱️ AI call (reasoning=%s) duration: %.2f seconds", execReasoningLabel(), float64(record.AIRequestDurationMs)/1000)
		record.ExecutionLog = append(record.ExecutionLog,
			fmt.Sprintf("AI call duration: %d ms", record.AIRequestDurationMs))
	}

	// Save chain of thought, decisions, and input prompt even if there's an error (for debugging)
	if aiDecision != nil {
		record.SystemPrompt = aiDecision.SystemPrompt // Save system prompt
		record.InputPrompt = aiDecision.UserPrompt
		record.CoTTrace = aiDecision.CoTTrace
		record.RawResponse = aiDecision.RawResponse // Save raw AI response for debugging
		if len(aiDecision.Decisions) > 0 {
			decisionJSON, _ := json.MarshalIndent(aiDecision.Decisions, "", "  ")
			record.DecisionJSON = string(decisionJSON)
			// P0-cleanup (2026-08-19) — a gate/armor refusal must never
			// look like a plain wait: every rewrite carries its reason
			// into the execution log.
			for _, d := range aiDecision.Decisions {
				if d.RefusalReason != "" {
					record.ExecutionLog = append(record.ExecutionLog, "ENTRY REFUSED: "+d.RefusalReason)
				}
			}
			// P0-cleanup — plan attribution: every decision names the plan
			// it followed (plan_id/version/overlay) + the cited scenario.
			if ap := kernel.ActivePlanFor(at.id, at.futuresSymbol()); ap != nil {
				record.PlanID = ap.PlanID
				record.PlanVersion = ap.Version
				record.OverlayVersion = ap.OverlayVersion
			}
			if aiDecision.Decisions[0].CitedScenario != "" {
				record.CitedScenarioID = aiDecision.Decisions[0].CitedScenario
			}
		}
	}

	// Record AI charge (track cost regardless of decision outcome). F10: a
	// guardrail HOLD now returns a non-nil FullDecision (with SkipReason) instead of
	// nil — only charge when a real decision cycle ran (SkipReason==""), preserving
	// the prior "no charge on a HOLD" behavior for the pre-prompt gates.
	if aiDecision != nil && aiDecision.SkipReason == "" && at.store != nil {
		if chargeErr := at.store.AICharge().Record(at.id, at.aiModel, at.config.AIModel); chargeErr != nil {
			at.logWarnf("⚠️ Failed to record AI charge: %v", chargeErr)
		}
	}

	if err != nil {
		at.consecutiveAIFailures++
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("Failed to get AI decision: %v", err)
		// P5 (ledger-close 2026-08-19) — typed class for one-query forensics.
		record.ErrorClass = classifyAIError(err)

		// P0-cleanup — structured error event (type stable, cause plain,
		// cost named). 402 = payment; else decision lost.
		if record.ErrorClass == "ai_payment_402" {
			telemetry.RecordError(at.id, "ai_payment_402", err.Error(), telemetry.CostDecisionLost)
		} else {
			telemetry.RecordError(at.id, "ai_call_failed", err.Error(), telemetry.CostDecisionLost)
		}

		// P0 2026-08-18 — DeepSeek "Insufficient Balance" (HTTP 402) silently
		// killed 139 overnight cycles today. Make it unmissable. P5 adds the
		// OUTAGE latch: one P0 banner per outage (event-id dedup), auto-cleared
		// by the first successful call.
		if record.ErrorClass == "ai_payment_402" {
			at.logErrorf("💸 DEEPSEEK PAYMENT FAILURE (402 Insufficient Balance) — cycles are dying with NO decision. Top up the DeepSeek account (api.deepseek.com). trader=%s", at.id)
			at.on402Failure(time.Now(), err)
		}

		// Activate safe mode after 3 consecutive failures
		if at.consecutiveAIFailures >= 3 && !at.safeMode {
			at.safeMode = true
			at.safeModeReason = fmt.Sprintf("AI failed %d consecutive times: %v", at.consecutiveAIFailures, err)
			at.logErrorf("🛡️ SAFE MODE ACTIVATED — AI failed %d times in a row. No new positions will be opened. Existing positions are protected with current stop-loss settings.", at.consecutiveAIFailures)
			at.logErrorf("🛡️ Reason: %v", err)
			at.logErrorf("🛡️ Action: Will keep trying AI each cycle. Safe mode auto-deactivates when AI recovers.")
		}

		// Print system prompt and AI chain of thought (output even with errors for debugging)
		if aiDecision != nil {
			logger.Info("\n" + strings.Repeat("=", 70) + "\n")
			logger.Infof("📋 System prompt (error case)")
			logger.Info(strings.Repeat("=", 70))
			logger.Info(aiDecision.SystemPrompt)
			logger.Info(strings.Repeat("=", 70))

			if aiDecision.CoTTrace != "" {
				logger.Info("\n" + strings.Repeat("-", 70) + "\n")
				logger.Info("💭 AI chain of thought analysis (error case):")
				logger.Info(strings.Repeat("-", 70))
				logger.Info(aiDecision.CoTTrace)
				logger.Info(strings.Repeat("-", 70))
			}
		}

		at.saveDecision(record)

		// In safe mode, don't return error — keep the loop running to retry next cycle
		if at.safeMode {
			at.logWarnf("🛡️ Safe mode: skipping this cycle, will retry in %v", at.config.ScanInterval)
			return nil
		}

		return fmt.Errorf("failed to get AI decision: %w", err)
	}

	// AI succeeded — reset failure counter and deactivate safe mode
	if at.consecutiveAIFailures > 0 {
		at.logInfof("✅ AI recovered after %d consecutive failures", at.consecutiveAIFailures)
	}
	// P5 — a success ends any latched 402 outage (banner auto-ack).
	at.onAISuccess(time.Now())
	at.consecutiveAIFailures = 0
	if at.safeMode {
		at.logInfof("🛡️ SAFE MODE DEACTIVATED — AI is working again. Resuming normal trading.")
		at.safeMode = false
		at.safeModeReason = ""
	}

	// // 5. Print system prompt
	// logger.Infof("\n" + strings.Repeat("=", 70))
	// logger.Infof("📋 System prompt [template: %s]", at.systemPromptTemplate)
	// logger.Info(strings.Repeat("=", 70))
	// logger.Info(decision.SystemPrompt)
	// logger.Infof(strings.Repeat("=", 70) + "\n")

	// 6. Print AI chain of thought
	// logger.Infof("\n" + strings.Repeat("-", 70))
	// logger.Info("💭 AI chain of thought analysis:")
	// logger.Info(strings.Repeat("-", 70))
	// logger.Info(decision.CoTTrace)
	// logger.Infof(strings.Repeat("-", 70) + "\n")

	// 7. Print AI decisions
	// logger.Infof("📋 AI decision list (%d items):\n", len(kernel.Decisions))
	// for i, d := range kernel.Decisions {
	//     logger.Infof("  [%d] %s: %s - %s", i+1, d.Symbol, d.Action, d.Reasoning)
	//     if d.Action == "open_long" || d.Action == "open_short" {
	//        logger.Infof("      Leverage: %dx | Position: %.2f | Stop loss: %.4f | Take profit: %.4f",
	//           d.Leverage, d.PositionSizeUSD, d.StopLoss, d.TakeProfit)
	//     }
	// }
	logger.Info()
	logger.Info(strings.Repeat("-", 70))
	// 8. Sort decisions: ensure close positions first, then open positions (prevent position stacking overflow)
	logger.Info(strings.Repeat("-", 70))

	// GetFullDecisionWithStrategy returns (nil, nil) when the risk gate HOLDs
	// the cycle (e.g. Plan 3 T21 daily-loss limit) — there is no decision to
	// execute. Guard before dereferencing aiDecision.Decisions below. (A real
	// $0 balance in the brief pre-first-account_balance-frame window, Plan
	// 4.11, can trip the gate and surface this otherwise-latent nil deref.)
	if aiDecision == nil || aiDecision.SkipReason != "" {
		// F10 — a guardrail/gate held the cycle. Record the TRUTH: stamp
		// execution_status="guardrail_skip" + the gate reason instead of leaving a
		// silently-empty success (the "ghost record" bug). The prompt may legitimately
		// be empty (pre-prompt gates) or present (schema-parse hold, copied above).
		reason := "risk_gate_hold"
		if aiDecision != nil && aiDecision.SkipReason != "" {
			reason = aiDecision.SkipReason
		}
		at.logInfof("ℹ️ No actionable decision this cycle (guardrail_skip: %s); skipping execution", reason)
		stampGuardrailSkip(record, reason)
		// P0 — a model that emitted reasoning but NO parseable JSON is a lost
		// setup, never a deliberate "no trade". Make every such miss visible to
		// the owner (P1 feed), so "how many setups is the format eating?" is
		// answerable instead of invisible. Deduped per cycle so each loss shows.
		if reason == "schema_parse_failed" {
			// P5.5 — keep a truncated copy of the decision-less output on
			// the record itself, so the owner sees WHAT was lost without
			// opening the raw-response blob.
			if snip := aiDecision.RawResponse; snip != "" {
				if len(snip) > 240 {
					snip = snip[:240] + "…"
				}
				record.ExecutionLog = append(record.ExecutionLog,
					"last model output (truncated): "+strings.ReplaceAll(snip, "\n", " "))
			}
			at.emitAlert("P1", "decision-unparseable",
				fmt.Sprintf("unparseable:%s:%d", at.id, at.callCount),
				"Decision unparseable — safe wait",
				fmt.Sprintf("cycle %d: the model produced reasoning but its decision JSON could not be parsed after retries; no trade was taken.", at.callCount))
		}
		at.saveDecision(record)
		return nil
	}

	// P0-latency — a decision whose bar has already closed is DISCARDED, never
	// acted on. The AI call spanned a primary-bar close: the decision was computed
	// on data the market has already moved past, so executing it would trade on a
	// stale bar. The loss is visible (P1 feed + gate-block counter) and the next
	// cycle re-decides on the fresh bar. Crypto never captures a bar (0) → no-op.
	if latest, ok := at.latestClosedPrimaryBarMs(); staleBarDiscard(decisionBarCloseMs, latest, ok) {
		// Discard-burn 2.2 — the superseded set is no longer thrown away
		// wholesale. Three classes:
		//   wait/hold-only  → free, QUIET discard (no WARN, no alert — #51 E17
		//                     showed these were pure noise, all supersessions
		//                     were logged as losses).
		//   entries-only    → mechanical re-eval against the fresh CLOSED bar;
		//                     pass = execute, refuse = clean skip with reason.
		//   contains close_* → legacy conservative discard, unchanged.
		entries, closes := classifyDecisions(aiDecision.Decisions)
		switch {
		case closes > 0:
			at.logWarnf("⏰ decision bar (close %d) is no longer the latest (close %d) — the AI call spanned a bar close; DISCARDING the decision (contains close actions).", decisionBarCloseMs, latest)
			stampGuardrailSkip(record, "stale_bar_discarded")
			telemetry.IncGateBlock(at.id, "stale_bar_discarded")
			at.emitAlert("P1", "decision-stale-bar",
				fmt.Sprintf("stalebar:%s:%d", at.id, at.callCount),
				"Decision discarded — bar closed during AI call",
				fmt.Sprintf("cycle %d: the AI call spanned a primary-bar close, so the decision was computed on a stale bar and was NOT acted on; the next cycle re-decides.", at.callCount))
			at.saveDecision(record)
			return nil
		case entries == 0:
			at.logInfof("ℹ️ superseded_wait — the AI call spanned a %s close but the decision was wait/hold-only; discarded quietly (free).", at.primaryTimeframe())
			stampGuardrailSkip(record, "superseded_wait")
			telemetry.IncGateBlock(at.id, "superseded_wait")
			// S5 (autopsy-response wave) — the honest-wait leak gauge: a decline
			// while a FRESH confirm was MET is the exact class the autopsy
			// quantified (+$1,763 hypothetical in the 08-26→27 window).
			if at.declineHadFreshMet() {
				telemetry.IncGateBlock(at.id, "decline_fresh_met")
			}
			at.saveDecision(record)
			return nil
		default:
			if v := at.reevalSupersededEntries(aiDecision.Decisions, ctx); !v.pass {
				at.logWarnf("⛔ stale_reeval outcome=refused reason=%s — superseded entry did not survive the fresh %s bar; clean skip.", v.reason, at.primaryTimeframe())
				stampGuardrailSkip(record, "stale_reeval_refused: "+v.reason)
				telemetry.IncGateBlock(at.id, "stale_reeval_refused")
				at.saveDecision(record)
				return nil
			}
			at.logInfof("✅ stale_reeval outcome=pass — superseded entry re-validated against the fresh %s bar (stop untouched, drift < %.2f×ATR%d); executing.",
				at.primaryTimeframe(), reevalDriftATRMult(), reevalATRPeriod)
			record.ExecutionLog = append(record.ExecutionLog, "stale_reeval outcome=pass (superseded entry re-validated on the fresh bar)")
		}
	}

	// 8. Sort decisions: ensure close positions first, then open positions (prevent position stacking overflow)
	sortedDecisions := sortDecisionsByPriority(aiDecision.Decisions)

	logger.Info("🔄 Execution order (optimized): Close positions first → Open positions later")
	for i, d := range sortedDecisions {
		logger.Infof("  [%d] %s %s", i+1, d.Symbol, d.Action)
	}
	logger.Info()

	// Check if trader is stopped before executing any decisions (prevent trades after Stop())
	at.isRunningMutex.RLock()
	running = at.isRunning
	at.isRunningMutex.RUnlock()
	if !running {
		at.logInfof("⏹ Trader stopped before decision execution, aborting cycle #%d", at.callCount)
		return nil
	}

	// Safe mode: filter out open positions, only allow close/hold
	if at.safeMode {
		filtered := make([]kernel.Decision, 0)
		for _, d := range sortedDecisions {
			if d.Action == "open_long" || d.Action == "open_short" {
				at.logWarnf("🛡️ Safe mode: BLOCKED %s %s (no new positions allowed)", d.Action, d.Symbol)
				continue
			}
			filtered = append(filtered, d)
		}
		sortedDecisions = filtered
		if len(sortedDecisions) == 0 {
			at.logInfof("🛡️ Safe mode: all decisions were open positions, nothing to execute")
		}
	}

	// Execute decisions and record results
	for _, d := range sortedDecisions {
		// Check if trader is stopped before each decision (allow immediate stop during execution)
		at.isRunningMutex.RLock()
		running = at.isRunning
		at.isRunningMutex.RUnlock()
		if !running {
			at.logInfof("⏹ Trader stopped during decision execution, aborting remaining decisions")
			break
		}

		actionRecord := store.DecisionAction{
			Action:     d.Action,
			Symbol:     d.Symbol,
			Quantity:   0,
			Leverage:   d.Leverage,
			Price:      0,
			StopLoss:   d.StopLoss,
			TakeProfit: d.TakeProfit,
			Confidence: d.Confidence,
			Reasoning:  d.Reasoning,
			Timestamp:  time.Now().UTC(),
			Success:    false,
		}

		// Hold-lock: once a position is OPEN, suppress an AI-initiated close so the
		// trade rides to the stop/target the AI set (a real OCO bracket resting at
		// the exchange). Opt-in per strategy, default OFF. Only AI decisions pass
		// through here; Emergency Flat + the drawdown monitor call the trader
		// directly and bypass this. Opening logic and closing a flat symbol are
		// untouched.
		if d.Action == "close_long" || d.Action == "close_short" {
			if at.holdLockSuppressesClose(&d, &actionRecord) {
				record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("🔒 %s %s suppressed (hold-lock: riding to stop/target)", d.Symbol, d.Action))
				record.Decisions = append(record.Decisions, actionRecord)
				continue
			}
		}

		if err := at.executeDecisionWithRecord(&d, &actionRecord); err != nil {
			at.logErrorf("❌ Failed to execute decision (%s %s): %v", d.Symbol, d.Action, err)
			actionRecord.Error = err.Error()
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("❌ %s %s failed: %v", d.Symbol, d.Action, err))
		} else if actionRecord.Error != "" {
			// A GATE REFUSED IT. Every entry gate in executeDecisionWithRecord sets
			// Success=false + Error="<gate>: <reason>" and returns nil — a refusal is
			// not an error. This branch used to be the plain else below, so it
			// overwrote Success with true and logged "✓ succeeded": a blocked entry
			// was recorded, and rendered in the UI, as an executed one. That made
			// "why was my entry refused?" unanswerable and every gate invisible.
			// Leave Success=false and say what stopped it.
			record.ExecutionLog = append(record.ExecutionLog,
				fmt.Sprintf("⛔ %s %s refused: %s", d.Symbol, d.Action, actionRecord.Error))
			// W16/R3 — the PARENT record must agree with its children. It is born
			// Success:true and nothing downgraded it, so a cycle whose only action
			// was refused still rendered a green SUCCESS pill over a ⛔ row, and the
			// audit's Execution Status column stayed blank. Carry the first refusal
			// reason up so the row itself says why.
			record.Success = false
			if record.RiskCheckError == "" {
				record.RiskCheckError = actionRecord.Error
			}
		} else {
			actionRecord.Success = true
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("✓ %s %s succeeded", d.Symbol, d.Action))
			// Brief delay after successful execution
			time.Sleep(1 * time.Second)
		}

		record.Decisions = append(record.Decisions, actionRecord)
	}

	// 9. Save decision record
	// D3-fix (2026-08-18) — the audit columns must reflect what happened:
	// risk_check_passed was only ever written false (guardrail skip), so every
	// normal cycle rendered as FAILED in the audit UI; prompt_version and
	// ai_model were never populated at all.
	if record.RiskCheckError == "" {
		record.RiskCheckPassed = true
	}
	record.PromptVersion = promptVariant
	record.AIModel = at.aiModel
	if err := at.saveDecision(record); err != nil {
		at.logWarnf("⚠ Failed to save decision record: %v", err)
	}

	return nil
}

// cmeSessionClosedSkip is the hoisted CME session gate (PART A). It returns true
// when the whole cycle should be skipped because the futures market is closed —
// after logging the open⇄closed edge and idling with a backoff.
func (at *AutoTrader) cmeSessionClosedSkip() bool {
	open := kernel.IsCMEOpen(time.Now())
	at.noteCMESessionEdge(open)
	if open {
		return false
	}
	// E5 (owner ruling 2026-09-07): mark the cycle as having taken the closed
	// path, so the loop's overrun check can tell the deliberate backoff from a
	// fault. Set BEFORE the sleep: a Stop() during the backoff still leaves a
	// truthful flag rather than an unexplained 3-minute cycle.
	at.lastTickClosedSkip = true
	at.backoffWhileClosed()
	return true
}

// noteCMESessionEdge logs the market open⇄closed transition ONCE (not every
// cycle). On a fresh start into a closed market it announces the closure + the
// next open immediately; a fresh start into an open market stays silent (there
// is nothing to "resume"). Single-goroutine (runCycle) → no locking on cmePrevOpen.
func (at *AutoTrader) noteCMESessionEdge(open bool) {
	prev := at.cmePrevOpen
	at.cmePrevOpen = &open
	if prev != nil && *prev == open {
		return // no edge — stay quiet
	}
	if !open {
		_, reason := kernel.CMEClosedReason(time.Now())
		next := kernel.NextCMEOpen(time.Now())
		chicago := kernel.CTLocation()
		at.logInfof("🌙 CME closed (%s) — next open %s", reason, next.In(chicago).Format("Mon 2006-01-02 15:04 MST"))
	} else if prev != nil {
		at.logInfof("☀️ CME open — resuming.")
	}
}

// backoffWhileClosed idles for cmeClosedBackoff while the market is shut, in
// short stop-responsive slices so Stop() returns promptly instead of the loop
// spinning a full cycle every ScanInterval (~1 min) all weekend. Returns early
// the moment the trader is stopped.
func (at *AutoTrader) backoffWhileClosed() {
	const slice = 10 * time.Second
	const cmeClosedBackoff = 3 * time.Minute
	// A11 — hand the calendar the ENFORCING value so the boot line quotes this
	// constant rather than a literal typed into a format string.
	kernel.SetClosedBackoffForBootLine(cmeClosedBackoff) // within the ~2–5 min target
	for waited := time.Duration(0); waited < cmeClosedBackoff; waited += slice {
		at.isRunningMutex.RLock()
		running := at.isRunning
		at.isRunningMutex.RUnlock()
		if !running {
			return
		}
		time.Sleep(slice)
	}
}

// buildTradingContext builds trading context
func (at *AutoTrader) buildTradingContext() (*kernel.Context, error) {
	// 1. Get account information
	balance, err := at.trader.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("failed to get account balance: %w", err)
	}

	// Get account fields
	totalWalletBalance := 0.0
	totalUnrealizedProfit := 0.0
	availableBalance := 0.0
	totalEquity := 0.0

	if wallet, ok := balance["totalWalletBalance"].(float64); ok {
		totalWalletBalance = wallet
	}
	if unrealized, ok := balance["totalUnrealizedProfit"].(float64); ok {
		totalUnrealizedProfit = unrealized
	}
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Use totalEquity directly if provided by trader (more accurate)
	if eq, ok := balance["totalEquity"].(float64); ok && eq > 0 {
		totalEquity = eq
	} else {
		// Fallback: Total Equity = Wallet balance + Unrealized profit
		totalEquity = totalWalletBalance + totalUnrealizedProfit
	}

	// 2. Get position information
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	var positionInfos []kernel.PositionInfo
	totalMarginUsed := 0.0

	// Current position key set (for cleaning up closed position records)
	currentPositionKeys := make(map[string]bool)

	for _, pos := range positions {
		// Comma-ok every assert: NT futures positions omit crypto-only fields
		// (e.g. liquidationPrice — futures have no liquidation), so an unchecked
		// .(float64) on a missing key panics and takes down the whole bot.
		symbol, _ := pos["symbol"].(string)
		if symbol == "" {
			continue
		}
		side, _ := pos["side"].(string)
		entryPrice, _ := pos["entryPrice"].(float64)
		markPrice, _ := pos["markPrice"].(float64)
		quantity, _ := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity // Short position quantity is negative, convert to positive
		}

		// Skip closed positions (quantity = 0), prevent "ghost positions" from being passed to AI
		if quantity == 0 {
			continue
		}

		unrealizedPnl, _ := pos["unRealizedProfit"].(float64)
		liquidationPrice, _ := pos["liquidationPrice"].(float64)

		// Calculate margin used (estimated)
		leverage := 10 // Default value, should actually be fetched from position info
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}
		marginUsed := (quantity * markPrice) / float64(leverage)
		totalMarginUsed += marginUsed

		// Calculate P&L percentage (based on margin, considering leverage)
		pnlPct := calculatePnLPercentage(unrealizedPnl, marginUsed)

		// Get position open time from exchange (preferred) or fallback to local tracking
		posKey := symbol + "_" + side
		currentPositionKeys[posKey] = true

		var updateTime int64
		// Priority 1: Get from database (trader_positions table) - most accurate
		if at.store != nil {
			if dbPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, symbol, side); err == nil && dbPos != nil {
				if dbPos.EntryTime > 0 {
					updateTime = dbPos.EntryTime
				}
			}
		}
		// Priority 2: Get from the exchange API
		if updateTime == 0 {
			if createdTime, ok := pos["createdTime"].(int64); ok && createdTime > 0 {
				updateTime = createdTime
			}
		}
		// Priority 3: Fallback to local tracking
		if updateTime == 0 {
			if _, exists := at.positionFirstSeenTime[posKey]; !exists {
				at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()
			}
			updateTime = at.positionFirstSeenTime[posKey]
		}

		// Get peak profit rate for this position
		at.peakPnLCacheMutex.RLock()
		peakPnlPct := at.peakPnLCache[posKey]
		at.peakPnLCacheMutex.RUnlock()

		positionInfos = append(positionInfos, kernel.PositionInfo{
			Symbol:           symbol,
			Side:             side,
			EntryPrice:       entryPrice,
			MarkPrice:        markPrice,
			Quantity:         quantity,
			Leverage:         leverage,
			UnrealizedPnL:    unrealizedPnl,
			UnrealizedPnLPct: pnlPct,
			PeakPnLPct:       peakPnlPct,
			LiquidationPrice: liquidationPrice,
			MarginUsed:       marginUsed,
			UpdateTime:       updateTime,
		})
	}

	// Clean up closed position records
	for key := range at.positionFirstSeenTime {
		if !currentPositionKeys[key] {
			delete(at.positionFirstSeenTime, key)
		}
	}

	// 3. Use strategy engine to get candidate coins (must have strategy engine)
	var candidateCoins []kernel.CandidateCoin
	if at.strategyEngine == nil {
		at.logWarnf("⚠️ No strategy engine configured, skipping candidate coins")
	} else {
		coins, err := at.strategyEngine.GetCandidateCoins()
		if err != nil {
			// Log warning but don't fail - equity snapshot should still be saved
			at.logWarnf("⚠️ Failed to get candidate coins: %v (will use empty list)", err)
		} else {
			candidateCoins = coins
			logger.Infof("📋 [%s] Strategy engine fetched candidate coins: %d", at.name, len(candidateCoins))
		}
	}

	// 4. Calculate total P&L
	totalPnL := totalEquity - at.initialBalance
	totalPnLPct := 0.0
	if at.initialBalance > 0 {
		totalPnLPct = (totalPnL / at.initialBalance) * 100
	}

	marginUsedPct := 0.0
	if totalEquity > 0 {
		marginUsedPct = (totalMarginUsed / totalEquity) * 100
	}

	// 5. Get leverage from strategy config
	strategyConfig := at.strategyEngine.GetConfig()
	btcEthLeverage := strategyConfig.RiskControl.BTCETHMaxLeverage
	altcoinLeverage := strategyConfig.RiskControl.AltcoinMaxLeverage

	// 6. Build context
	ctx := &kernel.Context{
		CurrentTime:     kernel.FormatCT(time.Now()), // CT canonical (P0 timezone)
		RuntimeMinutes:  int(time.Since(at.startTime).Minutes()),
		CallCount:       at.callCount,
		TraderID:        at.id,                  // B6: per-trader gate-block counters
		SnapshotMs:      time.Now().UnixMilli(), // B4 evaluates the feed at THIS instant, not post-call
		BTCETHLeverage:  btcEthLeverage,
		AltcoinLeverage: altcoinLeverage,
		Account: kernel.AccountInfo{
			TotalEquity:      totalEquity,
			AvailableBalance: availableBalance,
			UnrealizedPnL:    totalUnrealizedProfit,
			TotalPnL:         totalPnL,
			TotalPnLPct:      totalPnLPct,
			MarginUsed:       totalMarginUsed,
			MarginUsedPct:    marginUsedPct,
			PositionCount:    len(positionInfos),
		},
		Positions:      positionInfos,
		CandidateCoins: candidateCoins,
	}

	// 7. Add recent closed trades + the track record (P&L-TRUTH WAVE: one
	// strict-corrected plumbing seam, attachTradeContext, fixture-tested).
	at.attachTradeContext(ctx)

	// A5 (G5) — tag every account-scoped context field with THIS trader's id. The
	// kernel asserts all tags == the deciding trader (ctx.TraderID == at.id) before
	// assembling the prompt; a field ever populated from another trader's data would
	// carry a different owner and skip the cycle. Byte-identical (tags are json:"-").
	ctx.OwnerTags = map[string]string{
		"account":       at.id,
		"positions":     at.id,
		"market_data":   at.id,
		"recent_orders": at.id,
		"trading_stats": at.id,
	}

	return ctx, nil
}

// sortDecisionsByPriority sorts decisions: close positions first, then open positions, finally hold/wait
// This avoids position stacking overflow when changing positions
func sortDecisionsByPriority(decisions []kernel.Decision) []kernel.Decision {
	if len(decisions) <= 1 {
		return decisions
	}

	// Define priority
	getActionPriority := func(action string) int {
		switch action {
		case "close_long", "close_short":
			return 1 // Highest priority: close positions first
		case "open_long", "open_short":
			return 2 // Second priority: open positions later
		case "hold", "wait":
			return 3 // Lowest priority: wait
		default:
			return 999 // Unknown actions at the end
		}
	}

	// Copy decision list
	sorted := make([]kernel.Decision, len(decisions))
	copy(sorted, decisions)

	// Sort by priority
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if getActionPriority(sorted[i].Action) > getActionPriority(sorted[j].Action) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted
}

// attachTradeContext (P&L-TRUTH WAVE, 2026-09-01) fills the prompt-facing
// trade context from the STRICT corrected-column aggregators: the session-day
// guardrail inputs, the recent closed trades (UNRESOLVED rows carry no P&L
// and no percentage) and the track record (figure + resolved n + unresolved
// exclusion count). Before this wave the block summed EffectivePnL(), which
// coerced 115 unresolved rows to raw realized_pnl and told the executor
// "Total PnL -203.68 (220 trades)" when the truth was +304.32 over 105.
func (at *AutoTrader) attachTradeContext(ctx *kernel.Context) {
	if at.store == nil {
		at.logWarnf("⚠️ Store is nil, cannot get recent trades")
		return
	}
	// Strategy Studio P1 — daily-guardrail inputs on the CME session-day:
	// today's realized P&L (closed trades) + entry count, scoped to the active
	// account. The kernel daily-guardrail gate (engine_analysis.go) reads these.
	sinceMs := kernel.CMESessionDayStart(time.Now()).UnixMilli()
	if dayPnL, dayTrades, derr := at.store.Position().GetSessionDayActivity(at.id, sinceMs, at.currentAccountName()); derr != nil {
		at.logWarnf("⚠️ Failed to compute daily-guardrail activity: %v", derr)
	} else {
		ctx.DailyRealizedPnL = dayPnL
		ctx.TradesToday = dayTrades
	}

	// Recent 10 closed trades for AI context, scoped to the ACTIVE account so a
	// post-switch cycle does not carry the old account's trades into the prompt.
	recentTrades, err := at.store.Position().GetRecentTrades(at.id, 10, at.currentAccountName())
	if err != nil {
		at.logWarnf("⚠️ Failed to get recent trades: %v", err)
	} else {
		unresolved := 0
		for _, trade := range recentTrades {
			entryTimeStr := ""
			if trade.EntryTime > 0 {
				entryTimeStr = kernel.FormatCT(time.Unix(trade.EntryTime, 0))
			}
			exitTimeStr := ""
			if trade.ExitTime > 0 {
				exitTimeStr = kernel.FormatCT(time.Unix(trade.ExitTime, 0))
			}
			if !trade.Resolved {
				unresolved++
			}
			ctx.RecentOrders = append(ctx.RecentOrders, kernel.RecentOrder{
				ID:           trade.ID,
				Resolved:     trade.Resolved,
				Symbol:       trade.Symbol,
				Side:         trade.Side,
				EntryPrice:   trade.EntryPrice,
				ExitPrice:    trade.ExitPrice,
				RealizedPnL:  trade.RealizedPnL,
				PnLPct:       trade.PnLPct,
				EntryTime:    entryTimeStr,
				ExitTime:     exitTimeStr,
				HoldDuration: trade.HoldDuration,
			})
		}
		logger.Infof("📊 [%s] Found %d recent closed trades for AI context (%d unresolved, rendered without P&L)", at.name, len(recentTrades), unresolved)
	}

	// Track record for AI context, scoped to the ACTIVE account.
	stats, err := at.store.Position().GetFullStats(at.id, at.currentAccountName())
	if err != nil {
		at.logWarnf("⚠️ Failed to get trading stats: %v", err)
		return
	}
	if stats == nil {
		at.logWarnf("⚠️ GetFullStats returned nil")
		return
	}
	if stats.TotalTrades == 0 && stats.UnresolvedExcluded == 0 {
		at.logWarnf("⚠️ GetFullStats returned 0 trades")
		return
	}
	ctx.TotalRealizedPnL = stats.TotalPnL // Chunk 5 — consistency-rule input (strict corrected all-time P&L)
	ctx.TradingStats = &kernel.TradingStats{
		TotalTrades:        stats.TotalTrades,
		ResolvedTrades:     stats.ResolvedTrades,
		UnresolvedExcluded: stats.UnresolvedExcluded,
		WinRate:            stats.WinRate,
		ProfitFactor:       stats.ProfitFactor,
		SharpeRatio:        stats.SharpeRatio,
		TotalPnL:           stats.TotalPnL,
		AvgWin:             stats.AvgWin,
		AvgLoss:            stats.AvgLoss,
		MaxDrawdownPct:     stats.MaxDrawdownPct,
	}
	logger.Infof("🧾 [%s] Track record: %+.2f over %d resolved trades (%d unresolved excluded), %.1f%% win rate, PF=%.2f, Sharpe=%.2f, DD=%.1f%%",
		at.name, stats.TotalPnL, stats.TotalTrades, stats.UnresolvedExcluded, stats.WinRate, stats.ProfitFactor, stats.SharpeRatio, stats.MaxDrawdownPct)
}
