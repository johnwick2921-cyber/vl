package trader

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"nofx/logger"
	"nofx/researchsnapshot"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"nofx/calendar"
	"nofx/kernel"
	"nofx/market"
	"nofx/mcp"
	nt "nofx/provider/ninjatrader"
	"nofx/store"
	"nofx/telemetry"
)

const plannerSystemPrompt = "You are a disciplined CME index-futures day-plan reasoner. Output ONLY the single JSON object requested — reasoning first, then the answer fields. No prose outside the JSON."

// P3.2 — PLANNER MODEL BINDING (RECON #12). The day-plan reasoner runs on a
// SECOND per-strategy model binding (day_plan.planner_model), independent of the
// executor's primary model. The EXACT pinned model ID is used (never an alias)
// and logged on every plan; an empty binding falls back to the primary model.

// resolvePlannerModelID is the pure decision: an empty planner model → the
// primary (usePrimary=true); otherwise the pinned planner model ID.
func resolvePlannerModelID(plannerModel, primaryModel string) (modelID string, usePrimary bool) {
	pm := strings.TrimSpace(plannerModel)
	if pm == "" {
		return primaryModel, true
	}
	return pm, false
}

// ResolvePlannerClient is the exported entry point (P5.4 Ask-Planner) so the API
// layer can reach the SAME planner model that authored the plan. Read-only use:
// the caller must never mutate traders/plans/bindings through it.
func (at *AutoTrader) ResolvePlannerClient() (mcp.AIClient, string) {
	return at.resolvePlannerClient()
}

// resolvePlannerClient returns the AI client for the planner + the resolved
// (pinned) model ID. Empty binding → the executor's primary client. A model that
// the registry can't resolve → the primary client (never a silent nil).
// ownerUserID resolves the owning user's id for C2 owner-level scoping (sticky
// levels are per-user now; an empty id degrades to the legacy ” bucket).
func (at *AutoTrader) ownerUserID() string {
	if at.store == nil {
		return ""
	}
	t, err := at.store.Trader().Get(at.id)
	if err != nil || t == nil {
		return ""
	}
	return t.UserID
}

func (at *AutoTrader) resolvePlannerClient() (mcp.AIClient, string) {
	primaryModel := at.aiModel
	if primaryModel == "" {
		primaryModel = "deepseek"
	}
	var plannerModel string
	if at.config.StrategyConfig != nil && at.config.StrategyConfig.DayPlan != nil {
		plannerModel = at.config.StrategyConfig.DayPlan.PlannerModel
	}

	modelID, usePrimary := resolvePlannerModelID(plannerModel, primaryModel)
	if usePrimary {
		exact := at.pinExactModel(at.mcpClient, modelID)
		at.logInfof("🧠 planner model: empty binding → using primary, pinned %q", exact)
		return at.mcpClient, exact
	}

	client := mcp.NewAIClientByProvider(modelID)
	if client == nil {
		exact := at.pinExactModel(at.mcpClient, primaryModel)
		at.logWarnf("🧠 planner model %q unresolved by the registry → falling back to primary %q", modelID, exact)
		return at.mcpClient, exact
	}
	// 4.5 — per-model thinking knobs override the env defaults (best-effort).
	if at.store != nil {
		if row, err := at.store.AIModel().GetByID(modelID); err == nil && row != nil {
			mcp.ApplyThinking(client, row.ThinkingMode, row.ReasoningEffort)
		}
	}
	// Mirror the primary key resolution (provider-specific overrides).
	apiKey := at.config.CustomAPIKey
	customURL := at.config.CustomAPIURL
	switch modelID {
	case "qwen":
		if at.config.QwenKey != "" {
			apiKey = at.config.QwenKey
		}
	case "deepseek":
		if at.config.DeepSeekKey != "" {
			apiKey = at.config.DeepSeekKey
		}
	}
	client.SetAPIKey(apiKey, customURL, at.config.CustomModelName)
	exact := at.pinExactModel(client, modelID)
	at.logInfof("🧠 planner model resolved (pinned): %q", exact)
	return client, exact
}

// pinExactModel resolves a possibly-alias model id to the EXACT model string
// (§125 — never stamp a provider alias on a plan): prefer the client's own
// resolved model, else map the alias to its provider default, else keep it as-is
// with a warning. Also records the model + resets the matched-random stats window
// on a model change (§128 — no pooling across models).
func (at *AutoTrader) pinExactModel(client mcp.AIClient, modelID string) string {
	exact := modelID
	if client != nil {
		if rm := strings.TrimSpace(client.ResolvedModel()); rm != "" && !mcp.IsProviderAlias(rm) {
			exact = rm
		}
	}
	if mcp.IsProviderAlias(exact) {
		if def := mcp.DefaultModelForAlias(exact); def != "" {
			at.logInfof("🧠 model %q is a provider alias → pinned exact %q", exact, def)
			exact = def
		} else {
			at.logWarnf("⚠️ planner model %q is a provider alias and could not be pinned to an exact string", exact)
		}
	}
	at.maybeResetStatsOnModelChange(exact)
	return exact
}

// maybeResetStatsOnModelChange resets the matched-random window when the pinned
// planner model changes (§128). Idempotent; the first-ever pin only records.
func (at *AutoTrader) maybeResetStatsOnModelChange(exactModel string) {
	if at.store == nil || strings.TrimSpace(exactModel) == "" {
		return
	}
	const key = "dayplan_pinned_model"
	prev, _ := at.store.GetSystemConfig(key)
	if prev == exactModel {
		return
	}
	if prev != "" {
		if err := at.store.MatchedRandom().ResetWindow(); err != nil {
			at.logWarnf("📊 stats-window reset on model change failed: %v", err)
		} else {
			at.logInfof("📊 planner model changed %q → %q — matched-random stats window RESET (no cross-model pooling).", prev, exactModel)
		}
	}
	_ = at.store.SetSystemConfig(key, exactModel)
}

// ---- P3.3 — read jobs + the planner call --------------------------------------

// plannerTradeDateCT returns the trade_date (CT calendar date) a read belongs to.
func plannerTradeDateCT(now time.Time) string {
	loc := kernel.CTLocation()
	return now.In(loc).Format("2006-01-02")
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}

// SessionReadFired records a scheduled session read the scheduler JUST STARTED
// (the planner call runs async — F6). Class 32 (2026-08-31): the wall-clock
// tick path uses it to emit the halt-fired log line exactly once per started
// read, and never for the ordinary live-tape path.
type SessionReadFired struct {
	Session   string
	TradeDate string
}

// maybeRunSessionReads fires the per-session planner read at the registry read
// time for each ENABLED session, once per session-day (idempotent via the plan
// store). GATED on day_plan → dormant by default. Called per-tick, BEFORE the
// data-gated skips (class 32 — scheduled work is wall-clock, never bar-gated).
func (at *AutoTrader) maybeRunSessionReads() []SessionReadFired {
	return at.maybeRunSessionReadsAt(traderNow())
}

// maybeRunSessionReadsAt is maybeRunSessionReads with an injectable clock
// (P0-B: the 16:55 closed-market read and the midnight wrap are time-sensitive
// and unit-tested against a fixed `now`). Returns the reads it just started.
func (at *AutoTrader) maybeRunSessionReadsAt(now time.Time) []SessionReadFired {
	if !at.dayPlanEnabled() || at.store == nil {
		return nil
	}
	reg := at.sessionRegistry(now) // W8 — admin registry from system_config (fallback default)
	fired := make([]SessionReadFired, 0, 1)
	for i := range reg.Sessions {
		s := &reg.Sessions[i]
		// W1 — fire the read ONLY inside this session's own read window. The
		// market-open test is made against the SESSION INSTANCE'S OPEN (P0-B):
		// the ASIA read is designed for 16:30 CT (owner ruling 2026-08-31), inside
		// the 16:00–17:00 CME maintenance break, and the contract says it builds
		// from STORED data while the market is closed — gating on IsCMEOpen(now)
		// made that read UNREACHABLE. Weekend/holiday protection is preserved:
		// a session instance whose OPEN falls on a closed day never reads, and
		// the death-check still runs through the wrapped tail.
		if !inSessionReadWindow(now, s.ReadCT, s.WindowEndCT) {
			continue
		}
		// A2 (owner ruling 2026-08-31) — Sunday sequencing: the weekly read
		// fires Sunday 16:30, the same minute as the moved ASIA read. The ASIA
		// read waits for this week's weekly doc to land; the per-cycle retry
		// makes it fire right after the weekly write, with no timers.
		if sundayAsiaDeferred(s, now, at.weeklyDocCached(now)) {
			at.logInfof("⏳ ASIA read deferred — Sunday weekly doc not landed yet (weekly 16:30 → ASIA follows)")
			continue
		}
		instOpen, okOpen := kernel.SessionInstanceStart(s, now)
		if !okOpen || !kernel.IsCMEOpen(instOpen) {
			continue
		}
		// W9 — the strategy's sessions_enabled subset (default [NY]) + per-session
		// Enable override gate which sessions THIS trader reads, on top of the
		// registry Enabled flag.
		// PART A — one resolver for both enable layers (explicit per-session override
		// wins; else registry + sessions_enabled). Enabling a session NEVER backfills
		// a past read: the read still only fires inside this session's own read
		// window, and the plan-store dedupe keeps it to once per session-day.
		if runnable, _ := at.sessionRunnable(s); !runnable {
			continue
		}
		// P0-B — the chain identity is the SESSION INSTANCE's date (wrap-aware),
		// never the midnight-roll calendar date: at 00:30 CT the cycle still
		// belongs to the ASIA instance that opened 17:00 yesterday.
		tradeDate, okDate := kernel.PlanChainTradeDate(s, now)
		if !okDate {
			tradeDate = plannerTradeDateCT(now)
		}
		existing, err := at.store.Plan().GetLatestPlanForTraderSession(tradeDate, s.Name, at.id)
		if err != nil {
			// P0-cleanup — a DB read error must not silently skip the
			// session's plan read (the plan simply never appears).
			at.logErrorf("🚨 planner session-read skipped: GetLatestPlanForTraderSession %s %s failed: %v", tradeDate, s.Name, err)
			telemetry.RecordError(at.id, "plan_read_failed", "GetLatestPlanForTraderSession: "+err.Error(), telemetry.CostDecisionLost)
			continue
		}
		// W-EXEC-TRUTH W5 (F3/D20) — a chain whose newest row is a MACHINE plan
		// (the Picture no-plan door) is "no plan" to the scheduler: the AI
		// session read still fires and lands the next version, which supersedes
		// it. Nothing below — dormant re-arm, death, flip, MSS or level wakes —
		// ever runs on a machine row: the `continue` skips all of it.
		if existing == nil || store.IsMachinePlan(existing) {
			// F6 (LONDON-FORENSICS 2026-08-28) — the first read's planner call
			// (300-500s observed) must not stall the executor loop: async, the
			// same pattern as the W6/MSS wake re-reads. The plan-store dedupe
			// keeps it one read per session-day.
			fired = append(fired, SessionReadFired{Session: s.Name, TradeDate: tradeDate})
			go at.runPlannerRead(s.Name, tradeDate)
			continue
		}
		// PLAN-LIFECYCLE WAVE (2026-08-27) — DEACTIVATE-AND-REARM: a plan whose
		// flip/death line fired goes DORMANT (same version, no replan-budget
		// burn) and re-arms automatically when price closes back on the valid
		// side (same hysteresis). Structural replans (MSS wake, session read,
		// owner) still write new versions.
		if existing.Lifecycle == "dormant" {
			if cleared, why := at.describeDormantCleared(existing); cleared {
				if err := at.store.Plan().UpdatePlanLifecycle(existing.PlanID, existing.Version, "active", "rearmed:"+why); err != nil {
					at.logErrorf("⚡ plan re-arm write failed: %v", err)
					continue
				}
				_ = at.store.SetSystemConfig(dormantSinceKey(existing), "0")
				at.logInfof("⚡ plan %s %s v%d REARMED — %s", tradeDate, s.Name, existing.Version, why)
				continue // wakes resume next cycle after a re-arm
			}
			// W-FLIP-REREAD BLOCKER 2 (2026-09-17) — RETRY while the row sleeps:
			// a structure_flip read that was refused (preflight, cadence, a
			// stream already open) or that wrote no version leaves the once-key
			// clear, and THIS is the cycle-by-cycle retry those log lines
			// promise. maybeRereadAfterFlip gates on the knob, the once-key and
			// the in-flight guard, so OFF stays byte-identical and a running
			// read is never doubled. The killer is the row's own dormant:flip
			// marker (lifecycle log), read only when the knob is ON.
			if cfgDP := at.config.StrategyConfig.DayPlan; cfgDP != nil && cfgDP.FlipRereadEnabled() {
				if killer, ok := at.dormantFlipKillerOf(existing); ok {
					at.maybeRereadAfterFlip(now, s.Name, tradeDate, existing, killer)
				}
			}
			// W-DEATH-REREAD (2026-09-18) — the death counterpart of the flip
			// retry: while the row sleeps, a refused or failed death read
			// retries next cycle (once-key + in-flight guard + budget gate all
			// live inside maybeRereadAfterDeath).
			if cfgDP := at.config.StrategyConfig.DayPlan; cfgDP != nil && cfgDP.DeathRereadEnabled() {
				if killer, ok := at.dormantDeathKillerOf(existing); ok {
					at.maybeRereadAfterDeath(now, s.Name, tradeDate, existing, killer,
						priceOf(market.FuturesBarsProvider(at.futuresSymbol(), kernel.AISVPBarInterval, kernel.AISVPBarCount)))
				}
			}
			// FIX 5 (F3, 2026-08-27) — DORMANT KEEPS EYES: while dormant, level
			// events still wake the PLANNER for a FRESH read (new version). The
			// dormant row is NEVER flipped active here — re-arm happens ONLY via
			// the close-back predicate above. Never resurrect dormant entries.
			at.maybeWakePlannerOnLevelEvents(s.Name, tradeDate, existing)
			continue
		}
		if existing.Lifecycle != "active" {
			// FIX 5 (F3) — a no_trade plan keeps its seats live too: level events
			// wake the planner for a fresh read instead of leaving the session
			// blind (yesterday: OR-H 12:30 + ONH 13:30 touches woke nothing).
			if existing.Lifecycle == "no_trade" {
				at.maybeWakePlannerOnLevelEvents(s.Name, tradeDate, existing)
			}
			continue // no_trade / died → done for the session
		}
		// C5 (2026-08-25) — DEATH CHECK FIRST: a dead vN must not also fire an
		// MSS wake this cycle — the wake dedupes by (plan,version,event), so a
		// same-cycle re-plan would re-wake on the SAME event under the NEW
		// version and double-append. G4.6 MSS wake runs only when no death was
		// handled below.
		handledDeath := false
		// P3.6 — RE-PLAN ON DEATH (cap replan_cap/session → NO-TRADE).
		// W-DEATH-REREAD (c) — a death-born plan's first death check waits out
		// its 10-minute birth wick (2 full 5m closes), so the SAME line's wick
		// noise cannot kill the fresh plan. Inside the wick the death check is
		// skipped entirely (MSS wakes still run).
		if detail, dead := at.describeActivePlanDeath(existing); dead && !deathBornWickActive(existing, at.dayPlanCfg(), now, at.priorDeathLinePrice(existing)) {
			handledDeath = true
			// PLAN-LIFECYCLE WAVE: a STRUCTURED flip/death-line hit goes DORMANT
			// instead of burning a re-plan (wick-noise protection; the rearm
			// predicate above restores it). The legacy all-levels-consumed
			// death keeps the original re-plan-with-cap path.
			if strings.HasPrefix(detail.Killer, "flip-condition:") || strings.HasPrefix(detail.Killer, "death-condition:") {
				marker := "dormant:flip:" + detail.Killer
				if strings.HasPrefix(detail.Killer, "death-condition:") {
					marker = "dormant:death:" + detail.Killer
				}
				if err := at.store.Plan().UpdatePlanLifecycle(existing.PlanID, existing.Version, "dormant", marker); err != nil {
					at.logErrorf("😴 dormant write failed: %v", err)
				} else {
					// flap guard — record WHEN the plan went dormant (the re-arm
					// predicate refuses before DORMANT_MIN_HOLD_MIN has elapsed).
					_ = at.store.SetSystemConfig(dormantSinceKey(existing), strconv.FormatInt(time.Now().UnixMilli(), 10))
					at.logInfof("😴 plan %s %s v%d DORMANT — %s (entries blocked; auto re-arms when price closes back; replan budget untouched)",
						tradeDate, s.Name, existing.Version, detail.Killer)
					// B2 (2026-09-18 review): record the RAW death line of the killed
					// version — the wick guard compares the born plan's line against
					// THIS raw number (the killer's buffered line is another price
					// space, off by the ATR buffer, and must never be compared).
					// Lenient parse: the raw price is metadata and must be readable
					// even from a doc strict validation refuses.
					var rawDoc kernel.PlanDoc
					if json.Unmarshal([]byte(existing.Doc), &rawDoc) == nil && rawDoc.DeathStructured != nil {
						_ = at.store.SetSystemConfig(deathRereadPriorLineKey(existing), fmt.Sprintf("%.6f", rawDoc.DeathStructured.Price))
					}
					// W-FLIP-REREAD (2026-09-17) — the dormant marker protects
					// entries; with the knob ON, ONE free re-read in the flipped
					// direction is the only way the flipped bias ever materializes.
					if strings.HasPrefix(detail.Killer, "flip-condition:") {
						at.maybeRereadAfterFlip(now, s.Name, tradeDate, existing, detail.Killer)
					} else {
						// W-DEATH-REREAD (2026-09-18) — a death only parks the
						// plan; with the knob ON, ONE budgeted re-read authors a
						// FRESH plan (bias free) with the death evidence. Gated
						// inside on the knob + class-35 budget.
						at.maybeRereadAfterDeath(now, s.Name, tradeDate, existing, detail.Killer, detail.Price)
					}
				}
				continue // skip MSS/level wakes while dormant (re-arm path above runs first next cycle)
			}
			replanCap := at.replanCapFor(s.Name) // W9 — per-session override wins
			// CLASS 35 (2026-09-01) — the budget is a RECORDED counter keyed under
			// the chain baseline (an owner reset re-arms it): only death re-plans
			// and owner re-reads spend; wake reads, dormant flips and fail-closed
			// markers never did and now never count. Every consumer reads this seam.
			budget := store.GetReplanBudget(at.store, at.id, tradeDate, s.Name, replanCap)
			// A death must never again be an unexplained line. On 2026-08-16 six
			// plans died in 25 minutes and the only record was five identical
			// "DIED" lines with no condition and no price.
			at.logInfof("🗓️ plan %s %s v%d DIED — %s. Re-planning (cap %d/session, %d spent, %d left).",
				tradeDate, s.Name, existing.Version, detail.Killer, replanCap, budget.Used, budget.Left())
			for _, l := range detail.Levels {
				at.logInfof("🗓️   ↳ %s", l)
			}
			if at.deathReplanAllowed(s.Name, tradeDate, existing, detail.Killer, budget) {
				// F6 (LONDON-FORENSICS 2026-08-28) — the death re-plan's planner
				// call blocked the cycle 19m33s (the 02:14 overrun). Async, same
				// pattern as the W6/MSS wake re-reads; the plan-store's single-
				// writer queue serializes the writes.
				go at.runDeathReplan(s.Name, tradeDate, existing, detail.Killer)
			}
		}
		if !handledDeath {
			// G4.6 (addendum, regime wave) — a fresh structure MSS on the
			// plan's bias TF is the FOURTH planner wake-up (one per MSS
			// event, deduped).
			at.maybeWakePlannerOnMSS(s.Name, tradeDate, existing)
			// W6 (2026-08-25) — level events (fresh zones/FVGs/OBs/iFVGs the
			// plan never saw + seated-level invalidations) are the FIFTH
			// wake-up. Death-first ordering is preserved: this runs only
			// when no death was handled above, and it shares the MSS wake's
			// min-interval throttle + per-session budget.
			at.maybeWakePlannerOnLevelEvents(s.Name, tradeDate, existing)
		}
	}
	return fired
}

// Owner overlays are keyed (plan_id, plan_version) and every reader resolves
// them against the LATEST version, so appending a new version silently orphans
// every edit attached to the old one: the card stops showing them and, worse, the
// executor stops citing them — the owner's levels quietly leave the plan the bot
// is trading. Nothing carries, re-points or rebases them.
//
// The real fix is a rebase (an index-based RFC-6902 patch like /levels/3 cannot
// simply be re-pointed at a doc whose level array changed) and is sized M in the
// report. It is deliberately NOT attempted here: getting it wrong would move an
// owner's edit onto a DIFFERENT level, which is worse than dropping it.
//
// What ships now is the honesty: a P1 alert naming exactly how many edits the
// re-plan is about to strand. The defect is live in code but has never fired in
// production — plan_overlays holds one demo-seeded row — so the first REAL owner
// edit is the one at risk, and this is what will tell them.
func (at *AutoTrader) warnIfReplanOrphansOverlays(row *store.PlanDB) {
	if at.store == nil || row == nil {
		return
	}
	overlays, err := at.store.Plan().ListOverlays(row.PlanID, row.Version)
	if err != nil {
		return
	}
	// W5 (F5) — a machine overlay is not an owner edit: never counted here,
	// never carried (a live Picture scenario is re-appended instead).
	overlays = userOverlays(overlays)
	if len(overlays) == 0 {
		return
	}
	at.logInfof("🗓️ re-plan %s v%d carries %d owner overlay(s) forward by price identity.",
		row.PlanID, row.Version, len(overlays))
}

// userOverlays drops machine-origin overlay rows (W5): the owner-edit carry and
// its count see only what a person or the planner's revision wrote.
func userOverlays(rows []*store.PlanOverlayDB) []*store.PlanOverlayDB {
	out := make([]*store.PlanOverlayDB, 0, len(rows))
	for _, r := range rows {
		if r != nil && !kernel.IsMachineOverlayOrigin(r.Origin) {
			out = append(out, r)
		}
	}
	return out
}

// carryOwnerEditsInto re-establishes the PREVIOUS version's owner edits on the
// version just written (ITEM 4, 2026-08-17).
//
// Owner levels are sticky by contract. Before this, a re-plan silently stranded
// them: overlays are keyed (plan_id, plan_version) and every reader resolves
// against the LATEST version, so the owner's levels quietly left the plan the bot
// was trading. The carry re-anchors by PRICE, never by array index — replaying
// `/levels/3` against a re-planned doc would move an edit onto a different level,
// which is worse than losing it.
//
// Anything that cannot be re-anchored (structural edits, and deletes the planner
// has undone) is recorded for REVIEW and alerted, never silently dropped.
func (at *AutoTrader) carryOwnerEditsInto(planID string, oldVersion, newVersion int) {
	if at.store == nil || oldVersion <= 0 || newVersion <= oldVersion {
		return
	}
	overlays, err := at.store.Plan().ListOverlays(planID, oldVersion)
	if err != nil {
		return
	}
	// W5 (F5) — machine overlays are never owner edits: carrying one would
	// raise a false P1 "overlays-need-review" on every re-plan.
	overlays = userOverlays(overlays)
	if len(overlays) == 0 {
		return
	}
	oldRow, err := at.store.Plan().GetPlan(planID, oldVersion)
	if err != nil || oldRow == nil {
		return
	}
	newRow, err := at.store.Plan().GetPlan(planID, newVersion)
	if err != nil || newRow == nil {
		return
	}
	var oldBase, newDoc kernel.PlanDoc
	if json.Unmarshal([]byte(oldRow.Doc), &oldBase) != nil || json.Unmarshal([]byte(newRow.Doc), &newDoc) != nil {
		return
	}
	patches := make([]string, 0, len(overlays))
	for _, ov := range overlays {
		patches = append(patches, ov.Patch)
	}
	// Resolve what the owner actually SAW on the old version. If that resolution
	// fails, oldFinal stays equal to oldBase, which carries nothing — fail-safe
	// (an edit is never mis-applied) but worth saying out loud, because silently
	// carrying nothing is the very failure this whole item exists to end.
	oldFinal := oldBase
	resolved := false
	if merged, mErr := json.Marshal(oldBase); mErr == nil {
		if applied, _ := kernel.ApplyOverlayPatches(merged, patches); len(applied) > 0 {
			var f kernel.PlanDoc
			// Re-validation of an overlay-resolved doc is an integrity check, not
			// the write-time policy gate — use the HARD ceilings (12/5) so a plan
			// validly written under a raised max_levels/scenario_cap never fails
			// here.
			if json.Unmarshal(applied, &f) == nil && kernel.ValidatePlanDocWithCaps(&f, kernel.PlanHardMaxLevels, kernel.PlanHardMaxScenarios) == nil {
				oldFinal, resolved = f, true
			}
		}
	}
	if !resolved {
		at.logErrorf("🚨 %s v%d: plan_final would not resolve, so %d owner overlay(s) cannot be carried into v%d — re-apply them by hand.",
			planID, oldVersion, len(overlays), newVersion)
		at.emitAlert("P1", "overlays-need-review",
			fmt.Sprintf("review:%s:v%d", planID, newVersion),
			fmt.Sprintf("%d owner edit(s) could not be carried into v%d", len(overlays), newVersion),
			"The previous version's edits could not be resolved, so nothing was carried forward. Re-apply them on the new version.")
		return
	}

	res := kernel.CarryOwnerEdits(oldBase, oldFinal, newDoc, patches)

	if res.Patch != "" {
		if _, aErr := at.store.Plan().AppendOverlay(&store.PlanOverlayDB{
			PlanID: planID, PlanVersion: newVersion,
			Patch: res.Patch, Origin: "owner-carried",
		}); aErr != nil {
			at.logErrorf("🚨 carrying %d owner level(s) into %s v%d FAILED: %v",
				len(res.Carried), planID, newVersion, aErr)
			at.emitAlert("P1", "overlays-orphaned",
				fmt.Sprintf("orphan:%s:v%d", planID, oldVersion),
				fmt.Sprintf("%d owner edit(s) could not be carried into v%d", len(res.Carried), newVersion),
				"The re-plan could not re-apply your levels. Re-add them on the new version.")
			return
		}
		at.logInfof("🗓️ carried %d owner level(s) into %s v%d.", len(res.Carried), planID, newVersion)
	}

	if len(res.Uncarried) > 0 {
		lines := make([]string, 0, len(res.Uncarried))
		for _, u := range res.Uncarried {
			lines = append(lines, "• "+u.Summary)
		}
		at.storeUncarriedEdits(planID, newVersion, res.Uncarried)
		at.logErrorf("🚨 %d owner edit(s) could NOT carry into %s v%d — awaiting review.",
			len(res.Uncarried), planID, newVersion)
		at.emitAlert("P1", "overlays-need-review",
			fmt.Sprintf("review:%s:v%d", planID, newVersion),
			fmt.Sprintf("%d edit(s) could not carry into v%d — review", len(res.Uncarried), newVersion),
			strings.Join(lines, "\n"))
	}
}

// storeUncarriedEdits parks the review list where the card can read it. Follows
// the scenario_status precedent (system_config keyed by plan), so no migration.
func (at *AutoTrader) storeUncarriedEdits(planID string, version int, items []kernel.UncarriedEdit) {
	blob, err := json.Marshal(items)
	if err != nil {
		return
	}
	_ = at.store.SetSystemConfig(store.UncarriedEditsKey(planID, version), string(blob))
}

// (A7/F13, fail-register wave) activePlanIsDead removed — a second, WEAKER
// death definition with zero production callers; the live path is
// describeActivePlanDeath (structured death → flip → legacy consumption).

// testNow is a test-only clock seam (nil in production).
var testNow func() time.Time

// traderNow returns the clock the plan-lifecycle evaluators use.
func traderNow() time.Time {
	if testNow != nil {
		return testNow()
	}
	return time.Now()
}

// describeActivePlanDeath is activePlanIsDead plus the EVIDENCE. Same window,
// same timeframe, same verdict — the explanation is derived from the decision
// rather than reconstructed alongside it, so the two cannot disagree.
func (at *AutoTrader) describeActivePlanDeath(row *store.PlanDB) (kernel.PlanDeathDetail, bool) {
	if market.FuturesBarsProvider == nil || row == nil {
		return kernel.PlanDeathDetail{}, false
	}
	var doc kernel.PlanDoc
	if json.Unmarshal([]byte(row.Doc), &doc) != nil {
		return kernel.PlanDeathDetail{}, false
	}
	noteFlipDirectionInverted(at, row, &doc, "active")
	noteLinesBeyondPrice(at, row, &doc, "active")
	bars := market.FuturesBarsProvider(at.futuresSymbol(), kernel.AISVPBarInterval, kernel.AISVPBarCount)
	if len(bars) == 0 {
		return kernel.PlanDeathDetail{}, false
	}
	now := traderNow()
	sinceMs := row.CreatedAt.UnixMilli()
	if row.CreatedAt.IsZero() {
		sinceMs = 0
	}
	// P0.3 (2026-08-19) — the planner's own stated death/flip conditions are now
	// MACHINE-EVALUATED (they used to be display-only prose; on 2026-08-18 the
	// plan's death text fired at ~09:00 and nothing re-planned). The structured
	// predicate runs first; the legacy all-levels-consumed check stays as the
	// fallback for old stored plans.
	// G7 (2026-08-21) — freshness-gated: a condition whose rule-TF series is
	// provably stale is SKIPPED (logged flip_eval_skipped), never guessed. A
	// fully-stale cycle defers the whole death check to the next fresh one.
	// W-FLIP-HOLD-ANCHOR (2026-09-17): the flip hysteresis counts from the
	// plan's STATE anchor (chain birth / re-plan / bias change / flip / re-arm,
	// latest wins), NOT from this re-read version's created_at — sinceMs above
	// still windows the condition's own bars, unchanged.
	// W-FLIP-OWNS-THE-BREACH (2026-09-17): the FLIP condition window is the
	// chain anchor when same-bias wake re-reads kept the line (R2) — the
	// death window and the legacy consumption stay on this version's birth.
	versions, transitions, fallback, okChain := at.planChainFacts(row)
	hold := kernel.FlipHoldAnchor{SinceMs: fallback, Source: kernel.FlipHoldAnchorVersion}
	cw := kernel.FlipConditionAnchor{SinceMs: fallback, Source: kernel.FlipWindowFallback, AnchorVersion: row.Version}
	if okChain {
		hold = kernel.ResolveFlipHoldAnchor(versions, transitions, row.Version, fallback)
		cw = kernel.ResolveFlipConditionAnchor(versions, row.Version, kernel.FlipLineClusterTolerance(), fallback)
	}
	at.noteFlipWindow(row, &doc, cw)
	killer, fired, skipped := kernel.PlanDeathOrFlipSinceFreshHoldWindows(doc, bars, at.acceptanceRuleFor(row.Session), sinceMs, cw.SinceMs, now.UnixMilli(), hold)
	for _, s := range skipped {
		at.logWarnf("flip_eval_skipped plan=%s v%d %s", row.PlanID, row.Version, s)
	}
	if fired {
		return kernel.PlanDeathDetail{Killer: killer, Price: priceOf(bars)}, true
	}
	if len(skipped) > 0 {
		return kernel.PlanDeathDetail{}, false
	}
	return kernel.DescribePlanDeath(doc, bars, at.acceptanceRuleFor(row.Session), sinceMs, now.UnixMilli())
}

// flipHoldAnchor resolves the instant the flip hysteresis counts from for
// this plan row: the latest of the chain's first version, a deliberate
// re-plan version, a bias-change version, or the last flip→dormant / re-arm
// transition (kernel.FlipHoldAnchorKinds) — never a same-bias wake re-read.
// A chain the store cannot read falls back to the row's own created_at,
// tagged so the skip line reads "since version(fallback)".
func (at *AutoTrader) flipHoldAnchor(row *store.PlanDB) kernel.FlipHoldAnchor {
	versions, transitions, fallback, ok := at.planChainFacts(row)
	if !ok {
		return kernel.FlipHoldAnchor{SinceMs: fallback, Source: kernel.FlipHoldAnchorVersion}
	}
	return kernel.ResolveFlipHoldAnchor(versions, transitions, row.Version, fallback)
}

// executorPlanDeadReason (C6 / S2-1, 2026-08-25) — the executor evaluates the
// ACTIVE plan's death with the SAME machine predicate the planner uses, so a
// dead-but-not-yet-replanned plan stops producing entries the moment the
// condition fires. A day_plan trader with no active plan (no_trade / budget
// exhausted / never written) is refused too: planless futures entries are not
// allowed. Empty string = no block.
func (at *AutoTrader) executorPlanDeadReason() string {
	sc := at.GetStrategyConfig()
	if sc == nil || sc.DayPlan == nil || !sc.DayPlan.PlanEnabled {
		return ""
	}
	if at.store == nil {
		return "day_plan on but store unavailable — planless entries refused"
	}
	now := traderNow()
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
		return "no active day plan for this session (day_plan on) — planless entries refused"
	}
	// W5 (D20) — a MACHINE plan (the Picture no-plan door) is not an AI plan:
	// the decision path stays exactly where "no plan" left it (refused), and
	// no death/flip predicate is ever run on the machine row. Its Picture
	// scenario trades only through the armed executor.
	if store.IsMachinePlan(row) {
		return "no AI day plan for this session yet (only a machine Picture plan, which trades through the armed executor alone) — planless AI entries refused"
	}
	if row.Lifecycle == "dormant" {
		reason := strings.TrimPrefix(row.TriggerReason, "dormant:")
		if reason == "" || reason == row.TriggerReason {
			reason = "flip/death line breached"
		}
		return fmt.Sprintf("plan dormant (%s) — entries refused until price closes back on the valid side", reason)
	}
	if row.Lifecycle != "active" {
		return fmt.Sprintf("plan lifecycle %q — entries refused", row.Lifecycle)
	}
	// W-DEATH-REREAD (c) — the wick guard the planner honours is honoured
	// here too: inside a death-born plan's 10-minute birth wick the executor
	// does not treat it as machine-dead (the same line's noise must not
	// block the fresh plan's entries).
	if detail, dead := at.describeActivePlanDeath(row); dead && !deathBornWickActive(row, sc.DayPlan, traderNow(), at.priorDeathLinePrice(row)) {
		return "active plan is MACHINE-DEAD (" + detail.Killer + ") — entries refused until the planner re-plans"
	}
	return ""
}

// dormantSinceKey keys the dormancy timestamp (flap guard) in system_config.
func dormantSinceKey(row *store.PlanDB) string {
	return fmt.Sprintf("plan_dormant_since:%s:%d", row.PlanID, row.Version)
}

// flipRereadDoneKey keys the once-per-fired-flip structure_flip read in
// system_config. "" or "0" = NOT done. It is written with a timestamp ONLY
// after the read's goroutine has decided success by the STORE (a newer
// active version exists — BLOCKER 2), never at launch: a process restart
// mid-read therefore loses nothing, and a refused read, a read that returned
// without a row, or a read that failed all clears it to "0" so the dormant
// branch of maybeRunSessionReadsAt retries next cycle.
func flipRereadDoneKey(row *store.PlanDB) string {
	return fmt.Sprintf("flip_reread_done:%s:%d", row.PlanID, row.Version)
}

// flipRereadInFlight is the in-memory "a structure_flip read for this
// plan+version is running" guard (BLOCKER 2): the once-key is no longer set
// at launch, so without this a second cycle could launch a second read while
// the first (a 5–20 minute planner call) is still open. Keyed
// trader|plan|version; entries live only as long as the goroutine.
var flipRereadInFlight sync.Map

// at.flipRereadLaunchAt (W-FLIP-REREAD-IMMEDIATE) records, per plan|version,
// WHEN a structure_flip read last LAUNCHED (reached the planner) and wrote
// nothing. The flip read is exempt from the wake throttles, and the
// dormant branch calls back every scan cycle, so without this a read that
// launched and failed (3 model calls) would relaunch every cycle for as long
// as the model kept failing. Only a LAUNCH starts this clock; a refusal
// (preflight, cutoff, open stream) never does, so those retry next cycle. It
// is measured from the flip read's OWN launch — an ordinary wake never sets
// it, so an earlier ordinary wake still cannot delay a flip read.

func flipRereadInFlightKey(at *AutoTrader, row *store.PlanDB) string {
	return fmt.Sprintf("%s|%s|%d", at.id, row.PlanID, row.Version)
}

// flipRereadPriorLine is the ONE producer of the prior-plan context the
// structure_flip read hands the write site as priorKiller. Its LAST arrow is
// the killer's, which is what kernel.FlipToDirection reads (BLOCKER 1) — the
// old-bias echo before it is prose for the model, never parsed.
func flipRereadPriorLine(version int, oldBias, flipTo, killer string) string {
	return fmt.Sprintf("PRIOR PLAN v%d bias %s — its flip condition fired → bias is now expected %s unless the tape says otherwise; the prior plan is dormant. %s",
		version, oldBias, flipTo, killer)
}

// dormantFlipKillerOf returns the killer of the row's most recent dormant
// transition when that transition was a FLIP ("dormant:flip:<killer>" in the
// lifecycle log). The trigger_reason column is the AUTHORING trigger (D3) and
// never carries the marker, so the log is the only place the killer lives.
// A row whose last dormant event was a death answers false.
func (at *AutoTrader) dormantFlipKillerOf(row *store.PlanDB) (string, bool) {
	if at.store == nil || row == nil {
		return "", false
	}
	events, err := at.store.Plan().LifecycleLog(row.PlanID, row.Version)
	if err != nil {
		return "", false
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Event != "dormant" {
			continue
		}
		if strings.HasPrefix(events[i].Reason, "dormant:flip:") {
			return strings.TrimPrefix(events[i].Reason, "dormant:flip:"), true
		}
		return "", false
	}
	return "", false
}

// flipRereadRun is the read-call seam (fixtures substitute a recorder to assert
// the request without running a live planner stream).
var flipRereadRun = func(at *AutoTrader, session, tradeDate, prior string, row *store.PlanDB, failClosed bool) bool {
	return at.runPlannerReadWithTriggerClaimedCtx(session, tradeDate, "structure_flip", prior, priorPlanLevelLines(row), failClosed)
}

// maybeRereadAfterFlip (W-FLIP-REREAD, 2026-09-17) — with day_plan.flip_reread
// ON, a fired flip requests ONE free planner re-read in the flipped direction
// (trigger structure_flip). OFF = not called (the caller gates on the knob, and
// this function double-checks it): today's dormant behaviour, byte-identical.
//
// W-FLIP-REREAD-IMMEDIATE (2026-09-17, owner: "why does the plan go dormant
// when the bias flips"): a structure_flip read is a REACTION to a
// machine-confirmed event (two 5m closes beyond the flip line with the ATR
// buffer), not a speculative wake, so it is EXEMPT from the two LOAD rules a
// level wake obeys — the class-47 30m cooldown (measured from the last
// wake-authored version) and the shared wake_min_interval_min throttle on
// at.lastPlannerWakeAt. Before this, a flip that fired inside either window sat
// dormant for up to 30 minutes with no plan in the new direction. What STAYS:
// the once-key, the in-flight guard, preflight (fresh bars), the class-47
// CUTOFF (a SAFETY rule: no read within 25 min of the session flat — a plan
// authored there can never be entered), and the one-planner-stream-at-a-time
// defer. After the launch at.lastPlannerWakeAt is still set, so ORDINARY wakes
// back off from the flip read; an earlier ordinary wake never delays it.
//
// Semantics after the 2026-09-17 review (BLOCKERs 2 + 3):
//   - "ONE read" means one SUCCESSFUL read per fired flip. Success is decided
//     by the STORE, not by flipRereadRun's bool (that bool means "this call
//     claimed the read", and a wake-class read that exhausts its 3 attempts
//     returns (0,"kept_active",nil) with NO row and still reports true).
//     Success = GetLatestPlanForTraderSession shows a version newer than the
//     dormant row with lifecycle "active" (the lifecycle the planner writes on
//     a good read; fail-closed writes "no_trade" and never happens on this
//     failClosed=false path).
//   - The once-key is written ONLY after that decision. Until then a run is
//     tracked in memory (flipRereadInFlight) so a second cycle cannot launch a
//     second read, and a restart mid-read loses nothing: the key is clear and
//     the dormant branch retries.
//   - Refused (preflight, cadence, open stream) or unsuccessful reads clear the
//     key to "0"; the dormant branch of maybeRunSessionReadsAt calls back in
//     here every cycle while the row sleeps, so "retry next cycle" is real.
//   - The dormant version is superseded only by a compare-and-set FROM
//     "dormant" (UpdatePlanLifecycleIf). If the re-arm path moved it to
//     "active" meanwhile, the supersede is refused and BOTH rows stand; the
//     newest version governs at read time (GetLatestPlanForTraderSession is
//     ORDER BY version DESC).
//   - The goroutine re-reads the row right before the read and skips if it is
//     no longer dormant.
//   - The write site ENFORCES the flipped bias (kernel.FlipToDirection on the
//     prior line, "bias %s is MANDATORY"): the model authors the flipped bias
//     or the read writes nothing and the dormant plan stands.
func (at *AutoTrader) maybeRereadAfterFlip(now time.Time, session, tradeDate string, row *store.PlanDB, killer string) {
	if at.store == nil || row == nil || market.FuturesBarsProvider == nil {
		return
	}
	cfg := at.config.StrategyConfig.DayPlan
	if cfg == nil || !cfg.FlipRereadEnabled() {
		return // knob OFF → dormant only, no read
	}
	if v, err := at.store.GetSystemConfig(flipRereadDoneKey(row)); err == nil && v != "" && v != "0" {
		return // one SUCCESSFUL read per fired flip (plan+version key)
	}
	inflightKey := flipRereadInFlightKey(at, row)
	if _, running := flipRereadInFlight.Load(inflightKey); running {
		return // a read for this plan+version is still open — never double it
	}
	// Same preflight as every read — synchronously, so a refusal never consumes
	// the once-key: the dormant plan stands and the next cycle may retry.
	if !at.plannerPreflight(session, tradeDate, "structure_flip") {
		at.logWarnf("🗓️ structure_flip read %s %s v%d — REFUSED by preflight (no fresh bars); the dormant plan stands.", tradeDate, session, row.Version)
		return
	}
	// Class-47 CUTOFF only (SAFETY rule — kept). CooldownMin is 0 on purpose:
	// a flip read is a reaction, not a wake, and SkipForCooldown must never
	// fire for it (W-FLIP-REREAD-IMMEDIATE).
	dec := WakeCadenceDecision{
		Session: session, Desc: "structure_flip: " + killer,
		CutoffMin: wakeCutoffMinutes(), CooldownMin: 0,
	}
	if sess, okS := at.sessionRegistry(now).ActiveSession(now); okS {
		dec.MinutesToFlat, dec.HaveFlat = minutesToSessionFlat(now, sess)
	}
	if dec.SkipForCutoff() {
		at.logWarnf("%s", wakeCutoffLine(session, dec.Desc, dec.MinutesToFlat, dec.CutoffMin, 0))
		return
	}
	if held, open := anyPlannerStreamOpen(); open {
		at.logWarnf("%s", wakeStreamDeferLine(session, dec.Desc, held))
		return
	}
	// W-ONE-BUTTON M2.1 (review F15/N7): the maintenance hold refuses HERE,
	// with the other refusals — before the launch clock, the wake timestamp and
	// the in-flight claim. (Inside the launched read it came too late: a short
	// hold parked the retry for a whole wake_min_interval.)
	if at.refusePlannerClaimWhileHeld(store.MakePlanIDForTrader(at.id, tradeDate, session), "structure_flip read") {
		return
	}
	// The two LOAD rules a level wake obeys are computed only to SAY that the
	// exemption applied (never to refuse): the class-47 cooldown since the last
	// wake-authored version, and the shared wake_min_interval_min throttle.
	if exempt := flipRereadExemptionNote(now, at.lastPlannerWakeAt, cfg.WakeMinIntervalMinutes(), at.lastWakeAuthoredVersionAge(now, tradeDate, session), wakeCooldownMinutes()); exempt != "" {
		at.logWarnf("🗓️ structure_flip read %s %s v%d — immediate (flip reads are exempt from cooldown/min-interval; cutoff + stream guard still apply): %s",
			tradeDate, session, row.Version, exempt)
	}
	// Self-backoff only: a previous flip LAUNCH for this row that wrote nothing
	// holds the retry for wake_min_interval_min, measured from that launch.
	if v, ok := at.flipRereadLaunchAt.Load(inflightKey); ok {
		if last, isT := v.(time.Time); isT && now.Sub(last) < time.Duration(cfg.WakeMinIntervalMinutes())*time.Minute {
			at.logWarnf("🗓️ structure_flip read %s %s v%d — retry held: %.0fm since this row's last flip launch that wrote nothing < wake_min_interval_min (%dm); refusals never start this clock.",
				tradeDate, session, row.Version, now.Sub(last).Minutes(), cfg.WakeMinIntervalMinutes())
			return
		}
	}
	if _, busy := flipRereadInFlight.LoadOrStore(inflightKey, now); busy {
		at.logInfof("🗓️ structure_flip read %s %s v%d already in flight — not launching a second.", tradeDate, session, row.Version)
		return
	}
	at.flipRereadLaunchAt.Store(inflightKey, now)
	// Ordinary wakes back off from THIS read; the flip read never backed off
	// from them.
	at.lastPlannerWakeAt = now
	// BLOCKER 2 — the once-key is NOT set here. It lands only after the
	// goroutine below has seen a newer active version in the store.

	oldBias, flipTo := "", kernel.FlipToDirection(killer)
	if doc, derr := kernel.ParsePlanDoc(row.Doc); derr == nil {
		oldBias = doc.Bias.Direction
	} else {
		// The prior is a live plan in prod; here, the bias is metadata — read it
		// even from a doc that strict validation refuses, so the prompt line
		// always names the old side.
		var raw kernel.PlanDoc
		if json.Unmarshal([]byte(row.Doc), &raw) == nil {
			oldBias = raw.Bias.Direction
		}
	}
	prior := flipRereadPriorLine(row.Version, oldBias, flipTo, killer)
	at.logWarnf("🗓️ structure_flip read %s %s v%d — waking the planner (W-FLIP-REREAD): %s", tradeDate, session, row.Version, killer)
	// Non-fatal and async, exactly like the level-event wake: a read that
	// does not land a newer active version keeps the dormant plan and clears
	// the once-key so the dormant branch retries next cycle.
	go func() {
		defer flipRereadInFlight.Delete(inflightKey)
		// BLOCKER 3(b) — the row may have been re-armed between the dormant
		// write and this goroutine's first instruction (or, on the retry
		// path, since the cycle that launched us). Read it back and skip.
		if cur, gerr := at.store.Plan().GetPlan(row.PlanID, row.Version); gerr != nil || cur == nil || cur.Lifecycle != "dormant" {
			lc := "unreadable"
			if cur != nil {
				lc = cur.Lifecycle
			}
			at.logWarnf("🗓️ structure_flip read %s %s v%d — SKIPPED before the read: the row is %q, no longer dormant; nothing authored, the once-key stays clear.", tradeDate, session, row.Version, lc)
			return
		}
		if !flipRereadRun(at, session, tradeDate, prior, row, false) {
			at.logWarnf("🗓️ structure_flip read %s %s v%d did not complete — the dormant plan stands; the once-key is cleared for a retry next cycle.", tradeDate, session, row.Version)
			_ = at.store.SetSystemConfig(flipRereadDoneKey(row), "0")
			return
		}
		// BLOCKER 2 — success is what the STORE says, not the bool above.
		fresh, fErr := at.store.Plan().GetLatestPlanForTraderSession(tradeDate, session, at.id)
		if fErr != nil || fresh == nil || fresh.Version <= row.Version || fresh.Lifecycle != "active" {
			at.logWarnf("🗓️ structure_flip read %s %s v%d wrote NO new version — the dormant plan stands; the once-key is cleared for a retry next cycle", tradeDate, session, row.Version)
			_ = at.store.SetSystemConfig(flipRereadDoneKey(row), "0")
			return
		}
		_ = at.store.SetSystemConfig(flipRereadDoneKey(row), strconv.FormatInt(now.UnixMilli(), 10))
		at.flipRereadLaunchAt.Delete(inflightKey) // done for this row; nothing to back off from
		if fdoc, derr := kernel.ParsePlanDoc(fresh.Doc); derr == nil && strings.EqualFold(fdoc.Bias.Direction, oldBias) {
			// Unreachable through the production write site (it rejects a
			// plan whose bias is not the flipped one); a non-production writer
			// on the seam can land it. Named, never looped.
			at.logWarnf("🗓️ structure_flip: new v%d carries bias %s, the SAME as the prior — the write site enforces the flipped bias, so this row did not come through it; the read counts as done (one successful read per fired flip).", fresh.Version, oldBias)
		}
		// BLOCKER 3(a) — supersede ONLY from dormant. A re-armed v%d keeps its
		// "active"; both rows stand and the newest version governs at read time.
		moved, uerr := at.store.Plan().UpdatePlanLifecycleIf(row.PlanID, row.Version, "dormant", "superseded:flip", fmt.Sprintf("superseded:flip:v%d", fresh.Version))
		switch {
		case uerr != nil:
			at.logWarnf("🗓️ structure_flip: supersede write failed for v%d: %v", row.Version, uerr)
		case !moved:
			at.logWarnf("🗓️ structure_flip: v%d was RE-ARMED meanwhile (no longer dormant) — supersede REFUSED; the re-armed v%d and the new v%d both stand as written, and the newest version (v%d) governs at read time.", row.Version, row.Version, fresh.Version, fresh.Version)
		default:
			at.logInfof("🗓️ plan %s %s v%d SUPERSEDED by the structure_flip read (new v%d).", tradeDate, session, row.Version, fresh.Version)
		}
		at.carryOwnerEditsInto(fresh.PlanID, row.Version, fresh.Version)
	}()
}

// lastWakeAuthoredVersionAge returns whole minutes since the session's latest
// version when that version was WAKE-authored (WakeCadenceGoverns), else -1 —
// the same measurement the class-47 cooldown uses, taken here only to
// describe the exemption (W-FLIP-REREAD-IMMEDIATE), never to refuse.
func (at *AutoTrader) lastWakeAuthoredVersionAge(now time.Time, tradeDate, session string) int {
	if at.store == nil {
		return -1
	}
	last, err := at.store.Plan().GetLatestPlanForTraderSession(tradeDate, session, at.id)
	if err != nil || last == nil || !WakeCadenceGoverns(last.TriggerReason) || last.CreatedAt.IsZero() {
		return -1
	}
	return int(now.Sub(last.CreatedAt).Minutes())
}

// flipRereadExemptionNote is the pure "which throttle WOULD have skipped this
// flip read" renderer: empty when neither the wake_min_interval_min throttle
// (sinceWake < minInterval) nor the class-47 cooldown (wakeAuthoredAgeMin in
// [0, cooldown)) would have applied, so the immediate line prints only when
// the exemption actually did something. A zero lastWake / negative age means
// "no prior wake" and never manufactures a note (A24).
func flipRereadExemptionNote(now, lastWake time.Time, minIntervalMin, wakeAuthoredAgeMin, cooldownMin int) string {
	var parts []string
	if !lastWake.IsZero() && minIntervalMin > 0 && now.Sub(lastWake) < time.Duration(minIntervalMin)*time.Minute {
		parts = append(parts, fmt.Sprintf("%.0fm since the last planner wake < wake_min_interval_min (%dm)", now.Sub(lastWake).Minutes(), minIntervalMin))
	}
	if cooldownMin > 0 && wakeAuthoredAgeMin >= 0 && wakeAuthoredAgeMin < cooldownMin {
		parts = append(parts, fmt.Sprintf("%d min since the last wake-authored version < cooldown (%dm)", wakeAuthoredAgeMin, cooldownMin))
	}
	return strings.Join(parts, "; ")
}

// dormantDeathKillerOf is the death counterpart of dormantFlipKillerOf:
// the killer of the row's most recent dormant transition when that transition
// was a DEATH ("dormant:death:<killer>" in the lifecycle log). A row whose
// last dormant event was a flip answers false. (W-DORMANT-DEATH-REARM,
// 2026-09-17 — the re-arm predicate keyed off trigger_reason, which D3 made
// the AUTHORING trigger; the log is the only place the marker lives.)
func (at *AutoTrader) dormantDeathKillerOf(row *store.PlanDB) (string, bool) {
	if at.store == nil || row == nil {
		return "", false
	}
	events, err := at.store.Plan().LifecycleLog(row.PlanID, row.Version)
	if err != nil {
		return "", false
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Event != "dormant" {
			continue
		}
		if strings.HasPrefix(events[i].Reason, "dormant:death:") {
			return strings.TrimPrefix(events[i].Reason, "dormant:death:"), true
		}
		return "", false
	}
	return "", false
}

// describeDormantCleared evaluates the SAME structured condition that put the
// plan dormant and reports whether price has closed back on the valid side
// (the re-arm half of the hysteresis pair). The dormant KIND — flip or death —
// comes from the lifecycle log (dormant:flip:… / dormant:death:…), because D3
// made plans.trigger_reason the AUTHORING trigger. Pre-D3 rows carry the
// marker in trigger_reason and are handled by the legacy fallback.
// The flap guard refuses re-arm until DORMANT_MIN_HOLD_MIN has elapsed.
func (at *AutoTrader) describeDormantCleared(row *store.PlanDB) (bool, string) {
	if market.FuturesBarsProvider == nil || row == nil {
		return false, ""
	}
	var doc kernel.PlanDoc
	if json.Unmarshal([]byte(row.Doc), &doc) != nil {
		return false, ""
	}
	noteFlipDirectionInverted(at, row, &doc, "dormant")
	noteLinesBeyondPrice(at, row, &doc, "dormant")
	c := kernel.PlanCondition{}
	if _, ok := at.dormantDeathKillerOf(row); ok {
		if doc.DeathStructured != nil {
			c = *doc.DeathStructured
		}
	} else if _, ok := at.dormantFlipKillerOf(row); ok {
		if doc.FlipStructured != nil {
			c = *doc.FlipStructured
		}
	} else if strings.HasPrefix(row.TriggerReason, "dormant:death:") {
		// Pre-D3 legacy rows: the marker lived in trigger_reason.
		if doc.DeathStructured != nil {
			c = *doc.DeathStructured
		}
	} else if doc.FlipStructured != nil {
		c = *doc.FlipStructured
	}
	if c.Price <= 0 {
		return true, "no machine condition (re-arm immediately)"
	}
	// flap guard — a plan that JUST went dormant cannot re-arm instantly.
	if hold := kernel.DormantMinHoldMin(); hold > 0 {
		if v, err := at.store.GetSystemConfig(dormantSinceKey(row)); err == nil && v != "" && v != "0" {
			if since, err := strconv.ParseInt(v, 10, 64); err == nil {
				if elapsed := time.Since(time.UnixMilli(since)); elapsed < time.Duration(hold)*time.Minute {
					return false, "" // still inside the min-dormant hold
				}
			}
		}
	}
	bars := market.FuturesBarsProvider(at.futuresSymbol(), kernel.AISVPBarInterval, kernel.AISVPBarCount)
	if len(bars) == 0 {
		return false, ""
	}
	now := traderNow()
	sinceMs := row.CreatedAt.UnixMilli()
	if row.CreatedAt.IsZero() {
		sinceMs = 0
	}
	return kernel.PlanConditionClearedSince(c, bars, sinceMs, now.UnixMilli())
}

// priceOf returns the latest closed close of the bar series (0 if empty).
func priceOf(bars []market.Kline) float64 {
	if len(bars) == 0 {
		return 0
	}
	return bars[len(bars)-1].Close
}

// warnFlipDeathSanity (P0.4-F, 2026-08-25) — advisory checks at plan write:
// the 11-plan audit found 7/11 active plans with a flip that can never fire
// (death preempts it), flip==death degeneracy, or a flip/death anchored on a
// level absent from the plan's own list. These are WARN-only by design — the
// machine evaluator + fail-closed path stay the enforcers; this just makes the
// defect visible in the journal at write time instead of after a session.
func (at *AutoTrader) warnFlipDeathSanity(d *kernel.PlanDoc) {
	if d == nil {
		return
	}
	levelPrice := func(p float64) bool {
		for _, l := range d.Levels {
			if math.Abs(l.Price-p) <= 3.0 {
				return true
			}
		}
		return false
	}
	if d.DeathStructured != nil && d.DeathStructured.Price > 0 && !levelPrice(d.DeathStructured.Price) {
		at.logWarnf("⚠️ flip/death sanity: death{price %.2f} matches NO level in this plan's list (orphan anchor).", d.DeathStructured.Price)
	}
	if d.FlipStructured != nil && d.FlipStructured.Price > 0 && !levelPrice(d.FlipStructured.Price) {
		at.logWarnf("⚠️ flip/death sanity: flip{price %.2f} matches NO level in this plan's list (orphan anchor).", d.FlipStructured.Price)
	}
	if d.DeathStructured != nil && d.FlipStructured != nil && d.DeathStructured.Price > 0 && d.FlipStructured.Price > 0 {
		dd, ff := d.DeathStructured, d.FlipStructured
		if math.Abs(dd.Price-ff.Price) <= 0.01 && dd.Side == ff.Side && dd.Rule == ff.Rule {
			at.logWarnf("⚠️ flip/death sanity: flip == death (%.2f %s %s) — flip_to fires at the same tick the plan dies; void.", ff.Price, ff.Side, ff.Rule)
		}
		// death preempts flip when death sits closer in the same direction
		// with an easier/equal rule.
		easier := func(rule string) int {
			switch rule {
			case "15m_close":
				return 1
			case "5m_close":
				return 2
			default: // 2x5m
				return 3
			}
		}
		if dd.Side == ff.Side && easier(dd.Rule) <= easier(ff.Rule) {
			up := dd.Side == "above"
			if (up && dd.Price <= ff.Price) || (!up && dd.Price >= ff.Price) {
				at.logWarnf("⚠️ flip/death sanity: death (%.2f %s %s) preempts flip (%.2f %s %s) — flip likely unreachable.", dd.Price, dd.Side, dd.Rule, ff.Price, ff.Side, ff.Rule)
			}
		}
	}
}

// noTradeLevelMap assembles the CURRENT detector/scorer output as plan levels
// for a NO-TRADE doc (P7) — the same pipeline every other surface uses
// (bars → AssembleScoredLevels at the resolved proximity + max_levels), with the
// sticky owner levels prepended like the planner input. Returns nil when the
// detector genuinely has nothing (no bars provider / no bars), which the doc
// turns into the explicit "detector data unavailable" line.
func (at *AutoTrader) noTradeLevelMap(session string) []kernel.PlanLevel {
	symbol := at.futuresSymbol()
	if market.FuturesBarsProvider == nil {
		return nil
	}
	bars := market.FuturesBarsProvider(symbol, kernel.AISVPBarInterval, kernel.AISVPBarCount)
	if len(bars) == 0 {
		return nil
	}
	now := time.Now()
	maxLevels, htfSeats, htfMult, minGrade, _ := resolveSessionPlanCfg(at.dayPlanCfg(), session)
	// R2 4.7 (2026-08-25) — fail-closed maps obey min_grade: a NO-TRADE doc's
	// level map must match what an active plan would have carried.
	scored, _, _ := kernel.AssembleScoredLevelsMinGrade(at.id, bars, at.sessionRegistry(now), symbol, maxLevels, htfSeats, htfMult, now, at.proximityFilterATR(), minGrade)

	out := make([]kernel.PlanLevel, 0, len(scored)+4)
	if owned, err := at.store.OwnerLevel().ListActiveForUser(at.ownerUserID(), symbol); err == nil {
		for _, o := range owned {
			label := "👤 " + o.Label
			out = append(out, kernel.PlanLevel{Price: o.Price, Label: label, Grade: "A", Instruction: "monitor"})
		}
	}
	for _, s := range scored {
		out = append(out, kernel.PlanLevel{Price: s.Price, Label: s.Label, Grade: s.Grade, Instruction: "monitor"})
	}
	return out
}

// deathReplanAllowed (CLASS 35, 2026-09-01) is the death path's budget gate:
// false ⇒ the NO-TRADE marker was written and the session sits out; true ⇒
// the caller may run the re-plan. The spend itself is RECORDED when the
// re-plan row lands (runPlannerReadCoreWithFactsGrades, keyed by the
// death_replan trigger class), so a read refused by preflight / clock-hold /
// a lost claim still costs nothing — "no plan row, no budget consumed" holds.
func (at *AutoTrader) deathReplanAllowed(session, tradeDate string, existing *store.PlanDB, killer string, budget store.ReplanBudget) bool {
	if !budget.May() {
		at.writeNoTradePlan(session, tradeDate,
			fmt.Sprintf("re-plans exhausted (%d/%d) after %d death re-plan(s) — last: %s",
				budget.Used, budget.Cap, budget.Used, killer))
		return false
	}
	at.warnIfReplanOrphansOverlays(existing)
	// The guard the owner asked for: once deaths reach the cap, the alert
	// NAMES the killing condition and the price rather than the count.
	if !(store.ReplanBudget{Used: budget.Used + 1, Cap: budget.Cap}).May() {
		version := 0
		if existing != nil {
			version = existing.Version
		}
		at.emitAlert("P1", "plan-death-streak",
			fmt.Sprintf("deaths:%s:%s:v%d", tradeDate, session, version),
			fmt.Sprintf("%s plan died — re-plan %d of %d", session, budget.Used+1, budget.Cap),
			fmt.Sprintf("Killed by: %s. This is the last re-plan before the session sits out.", killer))
	}
	return true
}

// runDeathReplan (CLASS 35) — the death re-plan read, labelled with the
// SPENDING class death_replan (it used to land as "<S>_scheduled_read", which
// is why the row count had to stand in for the spend). P0.4-G: the killer and
// the prior plan's levels ride into the prompt (flip bias enforced at write,
// level-set continuity). ITEM 4: the owner's sticky levels re-establish on
// the version just written, re-anchored by price; anything that cannot be
// re-anchored is parked for review, never dropped.
func (at *AutoTrader) runDeathReplan(session, tradeDate string, existing *store.PlanDB, killer string) {
	_ = at.runPlannerReadWithTriggerClaimedCtx(session, tradeDate, store.TriggerDeathReplan, killer, priorPlanLevelLines(existing), true)
	if fresh, fErr := at.store.Plan().GetLatestPlanForTraderSession(tradeDate, session, at.id); fErr == nil && fresh != nil && existing != nil && fresh.Version != existing.Version {
		at.carryOwnerEditsInto(fresh.PlanID, existing.Version, fresh.Version)
	}
}

// writeNoTradePlan appends a NO-TRADE plan (re-plans exhausted) + an alert event.
func (at *AutoTrader) writeNoTradePlan(session, tradeDate, reason string) {
	// P7 — levels are market FACTS; the plan is an opinion about them. A no-trade
	// decision must never erase the map: the fail-closed doc carries the current
	// detector/scorer output (owner sticky levels included) so the card keeps
	// showing the map under the NO-TRADE banner. Unavailable detector data says so.
	doc := kernel.NoTradePlanDocWithLevels(reason, at.noTradeLevelMap(session))
	docJSON, _ := json.Marshal(doc)
	_, err := at.store.Plan().AppendPlan(&store.PlanDB{
		PlanID: at.store.Plan().ResolvePlanID(tradeDate, session, at.id), StrategyID: at.id,
		TradeDate: tradeDate, Session: session, TriggerReason: "replans_exhausted",
		Lifecycle: "no_trade", Doc: string(docJSON),
	})
	if err != nil {
		at.logErrorf("🗓️ planner: write NO-TRADE plan failed %s %s: %v", tradeDate, session, err)
		return
	}
	at.logErrorf("🚨 PLAN NO-TRADE %s %s: %s — session sits out.", tradeDate, session, reason)
	telemetry.IncGateBlock(at.id, "plan_replans_exhausted")
	// W6 — P0 plan-died → no-trade alert.
	at.emitAlert("P0", "plan-died", "notrade:"+tradeDate+":"+session,
		fmt.Sprintf("%s plan died — sitting out", session), reason)
}

// plannerReadInFlight claims a planner read for one (trade_date, session) for as
// long as the AI call is running. W16/R6.
//
// The dedupe in maybeRunSessionReads is `GetLatestPlanForSession(...) == nil`,
// but that check and the AppendPlan that satisfies it are separated by a FULL AI
// round trip (up to 3 attempts — seconds to minutes). Nothing held a claim across
// that window.
//
// Intra-trader this was safe: each trader drives one sequential loop goroutine
// (auto_trader.go:776-790), so its cycles cannot overlap. The exposure is
// CROSS-TRADER: two day-plan traders on the same symbol both see "no plan
// yet" at the read time, both pay for a full planner call, and both append a
// version of the same session's plan. That is the live configuration today (two
// MNQ day-plan traders).
//
// All traders share one process, so a process-wide claim covers both axes.
//
// H6 (hygiene 2026-09-03) — THIS COMMENT USED TO SAY THE OPPOSITE OF THE CODE.
// It claimed the key "deliberately excludes the trader id" so that two traders
// reading the SAME session collapse to one call. The key is built by
// store.MakePlanIDForTrader(at.id, tradeDate, session) (see the claim site
// below), which INCLUDES the trader id — so each trader holds its own claim and
// two traders on the same session are NOT collapsed. The code is authoritative;
// only the comment moved. Whether the collapse SHOULD happen is a live
// question, not a fixed one: it is recorded in the hygiene report, unchanged.
var plannerReadInFlight sync.Map // "tradeDate:session" -> struct{}

// claimPlannerRead returns false when another read for this session is already
// running. The winner must call releasePlannerRead.
func claimPlannerRead(key string) bool {
	_, loaded := plannerReadInFlight.LoadOrStore(key, struct{}{})
	return !loaded
}

func releasePlannerRead(key string) { plannerReadInFlight.Delete(key) }

// PlannerReadInFlight reports whether a planner read for this trader's chain is
// currently running (the claim is held). The API exposes it as the card's
// "reading…" state; ForceReset uses it to avoid a silent claim-skip — the
// 2026-08-18 live finding: the owner's reset read silently no-op'd because a
// death re-plan held the claim, and the fresh plan was written by that OTHER
// read (wrong trigger_reason, zero UI feedback).
func (at *AutoTrader) PlannerReadInFlight(tradeDate, session string) bool {
	_, held := plannerReadInFlight.Load(store.MakePlanIDForTrader(at.id, tradeDate, session))
	return held
}

// runPlannerReadWithTriggerClaimed is runPlannerReadWithTrigger returning whether
// THIS call claimed the read (false = another read was already in flight and
// this one skipped). The wrapper keeps the old signature for existing callers.
func (at *AutoTrader) runPlannerReadWithTriggerClaimed(session, tradeDate, triggerOverride string) bool {
	return at.runPlannerReadWithTriggerClaimedCtx(session, tradeDate, triggerOverride, "", nil, true)
}

// runPlannerReadWithTriggerClaimedCtx (P0.4-G, 2026-08-25) is the claimed read
// with the prior-plan context: priorKiller (the dead plan's killer line) and
// priorLevels (the previous version's levels for map continuity). A flip-fired
// killer carries a MANDATORY bias the new plan must honor; the prior levels keep
// the map from being rebuilt from scratch every re-plan.
//
// failClosed=true (scheduled reads, death re-plans, owner re-reads/resets): a
// read that fails every retry writes the terminal NO-TRADE marker.
// failClosed=false (W6 wake reads): the wake is OPPORTUNISTIC — if the re-read
// fails, the still-active plan keeps trading and nothing is written.
func (at *AutoTrader) runPlannerReadWithTriggerClaimedCtx(session, tradeDate, triggerOverride, priorKiller string, priorLevels []string, failClosed bool) bool {
	if !at.dayPlanEnabled() || at.store == nil {
		return false
	}
	key := store.MakePlanIDForTrader(at.id, tradeDate, session)
	if at.refusePlannerClaimWhileHeld(key, "planner read") { // W-ONE-BUTTON M2 site 5
		return false
	}
	if !claimPlannerRead(key) {
		at.logInfof("🗓️ planner read for %s already in flight — skipping duplicate call.", key)
		return false
	}
	defer releasePlannerRead(key)
	// F6 — CLOCK-HOLD (2026-08-30): never author a new plan on a clock known
	// broken (|local-vs-feed drift| > C2 tolerance). No plan row, no budget
	// consumed, exits and armed management untouched; the read window retries
	// next cycle once the clock heals. The same measurement widens the T1 news
	// windows below (warn band and critical band alike).
	holdDeferred, holdWiden, holdDrift, holdHave := at.clockHoldAuthoring()
	if holdDeferred {
		at.logErrorf("%s. Fix the host clock / NTP.", clockHoldDeferLine(tradeDate, session, holdWiden, kernel.C2ToleranceMs()))
		return false
	}
	// U1 3.2 — never call the LLM on an empty/stale bar window (the 08-19
	// outage produced 0-scenario fail-closed stubs this way). No plan row is
	// written and no budget consumed; the read window retries next cycle.
	if !at.plannerPreflight(session, tradeDate, triggerOverride) { // CLASS 36 — scoped by trigger class
		return false
	}
	client, modelID := at.resolvePlannerClient()
	if client == nil {
		// C9 (2026-08-25) — HONEST failure: return false so the reread path
		// reports the real outcome instead of a silent "success" that wrote
		// nothing (the fail-closed NO-TRADE write lives in the retry core,
		// which this branch never reached).
		at.logErrorf("🗓️ planner: no client resolved for %s %s", tradeDate, session)
		return false
	}
	input := at.assemblePlannerInputWithCtx(session, tradeDate, priorKiller, priorLevels)
	// F3 — FAST-MARKET WAKE READS (waterfall-class wave, 2026-08-28): when a wake
	// fires with |price drift| since the last plan write > FAST_MARKET_ATR ×
	// ATR5m, this read runs on the fast reasoning wire and the prompt carries a
	// FAST TAPE line. The 361.6s / 90pt-stale wake-read class dies here.
	if driftPts, driftAtr := at.fastMarketDrift(input.Price); driftPts > 0 {
		input.FastTape = true
		input.FastTapeNote = fmt.Sprintf("price has moved %.1f pts (%.1f×ATR5m) since the last plan write", driftPts, driftAtr)
		at.fastTapePending.Store(true)
		at.logInfof("🧠 planner mode: fast-market (drift %.1f pts = %.1f×ATR5m) — reasoning downgraded to %s for this read (F3)", driftPts, driftAtr, fastMarketReasoningLabel())
	}
	p2Start := time.Now()
	prompt := kernel.BuildPlannerPrompt(input)
	at.logInfof("📝 prompt render (T2): %dms ~%d tokens", time.Since(p2Start).Milliseconds(), estimatePromptTokens(prompt))
	hash := shortHash(prompt)
	// W3 — HARD red-news blackout lines auto-written into the plan (§80),
	// widened by the CAPPED clock measurement (F6 / CLASS 145).
	t1Lines := at.plannerT1Lines(input.Calendar, holdHave, holdWiden, holdDrift, tradeDate, session)
	// P0.1/P0.2 (2026-08-19) — write-time facts: both-side levels (0-on-a-side
	// hard fail since the owner ruling 2026-08-31 removed the count concept),
	// continuation scenario on gaps. PDH/PDL come from the detector universe
	// (seated or raw).
	identityMap := kernel.BuildMapCandidates(input.Levels, input.Price, input.ATR5m, kernel.MapCandidateOpts{})
	// W-GEOMETRY-REFUSAL (b1) — the SAME fill the prompt map gets, so the write-site
	// stamp and the renderer can never disagree about a reference level's id.
	if dp := at.dayPlanCfg(); dp.GeometryRefIDsEnabled() {
		kernel.EnsureReferenceLevelIDs(identityMap)
	}
	facts := kernel.PlanFacts{Zones: input.Zones, IdentityMap: identityMap, Price: input.Price, DATR: input.DATR, Regime: input.Regime, Structure: input.Structure, ReadAt: input.Now}
	facts.CapacityCut = kernel.CapacityCutCandidates(input.Pool, identityMap, input.Price, input.ATR5m) // W2 A4 — accepted as a first obstacle, never required
	// 8.4 — machine grades from the Go-ranked candidate table, keyed by rounded
	// price so the write-site stamp can match the model's levels.
	machineGrades := map[float64]string{}
	machineLabels := map[float64]string{} // P0.4-H: price → detector label
	for _, l := range input.Levels {
		switch l.Kind {
		case kernel.KindPDH:
			facts.PDH = l.Price
		case kernel.KindPDL:
			facts.PDL = l.Price
		case kernel.KindPDC:
			facts.PDC = l.Price
		}
	}
	// S1-wave A3 (2026-08-29) — the record loops moved into collectMachineGrades
	// (pure, fixture-tested) and now include the FULL HTF-zone universe.
	mapStart := time.Now()
	collectMachineGrades(input, machineGrades, machineLabels)
	at.logInfof("🗺️ map assembly (T1): %dms", time.Since(mapStart).Milliseconds())
	// S-dispatch (2026-08-27) — the P0.2 gap rules' PDH/PDL must come from the
	// universe too: the seated loop above skips them post-roll and the gap
	// continuation rules silently went unevaluated (PDH/PDL = 0 = "unknown").
	if facts.PDH <= 0 {
		facts.PDH = input.BiasCtxFacts.PDH
	}
	if facts.PDL <= 0 {
		facts.PDL = input.BiasCtxFacts.PDL
	}
	if facts.PDC <= 0 {
		facts.PDC = input.BiasCtxFacts.PDC
	}
	requiredBias := kernel.FlipToDirection(priorKiller)
	// W11 — carry the frozen indicator mirror + ai_config hash to the write site.
	// LATENCY ROUTING (plan-lifecycle wave, 2026-08-27) — planner reads keep
	// FULL reasoning (AI_PLAN_REASONING, default max); the executor loop runs
	// cheap. Re-asserted per call because the client may be shared.
	pMode, pEffort := planReasoningWire()
	modeLabel := planReasoningLabel()
	if at.fastTapePending.Swap(false) {
		pMode, pEffort = fastMarketReasoningWire()
		modeLabel = fastMarketReasoningLabel()
	}
	// ROOT-FIX part B (2026-09-02) — register what a SHADOW fast-mode call needs
	// (client, system prompt, budget, fast wire). The shadow fires only after
	// the live read finishes, writes no plan, and is off by default.
	// CLASS 46 D5 — a new READ gets a fresh provider-call budget.
	mcp.ResetStormCounterFor(client)
	fMode, fEffort := fastMarketReasoningWire()
	at.RegisterShadowRunner(client, plannerSystemPrompt, aiPlanMaxTokens(), fMode, fEffort)
	recordResearchInput(input.ResearchSnapshotID, input, plannerSystemPrompt, modelID)
	researchTrace := &researchsnapshot.PlanTrace{SnapshotID: input.ResearchSnapshotID, Model: modelID, ConfigVersion: input.AIConfigHash, SystemPrompt: plannerSystemPrompt}
	at.runPlannerReadCoreObserved(time.Now, researchTrace, session, tradeDate, triggerOverride, modelID, hash, input.IndicatorsBlock, input.AIConfigHash, requiredBias, prompt, facts, machineGrades, machineLabels, htfLabels(input), failClosed, func(userPrompt string) (string, error) {
		mcp.ApplyThinking(client, pMode, pEffort)
		// PLANNER SPEED WAVE 4 (2026-08-31) — the session planner now rides the
		// SSE streaming client with the idle watchdog (split deadlines). The
		// completion budget rides req.MaxTokens (no shared-client mutation).
		cap := aiPlanMaxTokens()
		req := &mcp.Request{
			Messages: []mcp.Message{
				mcp.NewSystemMessage(plannerSystemPrompt),
				mcp.NewUserMessage(userPrompt),
			},
			MaxTokens: &cap,
		}
		researchTrace.RequestConfig(pMode, pEffort, cap)
		start := time.Now()
		var raw string
		var err error
		if bc, ok := client.(interface{ BaseClient() *mcp.Client }); ok {
			// CLASS 37 (2026-09-01) — the planner's OWN whole-call ceiling
			// (AI_PLAN_TOTAL_DEADLINE_SECS) rides beside the idle watchdog; the
			// 600s http.Client.Timeout no longer bounds a LIVE reasoning stream
			// (it killed 11 of 80 max-reasoning attempts 08-30 → 09-01).
			raw, err = bc.BaseClient().CallWithRequestStreamRetryDeadlines(req, nil, plannerStreamIdle(), plannerStreamTotal())
		} else {
			// Non-base clients (tests, legacy callers) keep the full-body path.
			raw, err = client.CallWithMessages(plannerSystemPrompt, userPrompt)
		}
		if err == nil && mcp.LastFinishReason(client) == "length" {
			at.logWarnf("📐 planner output TRUNCATED by the provider (finish_reason=length, cap=%d) — the plan JSON may be incomplete; retrying at the same cap will not fix truncation", cap)
		}
		if err != nil {
			// CLASS 37 — every failed provider call names its class, the
			// provider ROW (never the key), HTTP status and request id: "the
			// API keeps failing" is never again a log line without a class.
			at.logWarnf("🛰 planner call FAILED class=%s provider_row=%s model=%s http_status=%d request_id=%q elapsed=%.1fs idle=%ds total=%ds — %v",
				mcp.LastErrClass(client), at.config.AIModelID, modelID, mcp.LastHTTPStatus(client), mcp.LastRequestID(client),
				time.Since(start).Seconds(), int(plannerStreamIdle().Seconds()), int(plannerStreamTotal().Seconds()), err)
		}
		at.logInfof("🧠 planner call (reasoning=%s wire=%s/%s cap=%d stream idle=%ds total=%ds) completed in %.1fs", modeLabel, pMode, pEffort, cap, int(plannerStreamIdle().Seconds()), int(plannerStreamTotal().Seconds()), time.Since(start).Seconds())
		// root-fix part B: the live side of the A/B pair.
		at.RecordLiveCallMetrics(mcp.LastCompletionTokens(client), time.Since(start).Milliseconds())
		return raw, err
	}, t1Lines...)
	return true
}

// plannerT1Lines is the plan-write T1 step: the HARD red-news lines, widened
// by the clock measurement through the SAME capped kernel.WidenCTWindows the
// arm path uses (auto_trader_calendar.go t1WindowsFor). CLASS 145
// (2026-09-17): the 16:38 CT ASIA read inside the CME halt measured a 2,326 s
// "drift" — the 15:59 bar's age — and the uncapped path wrote "+31m (clock
// drift)" into the plan; the cap holds the widening to ClockWidenCapMinutes
// and the journal names staleness instead of the clock.
func (at *AutoTrader) plannerT1Lines(cal []kernel.PlannerCalendarEvent, holdHave bool, holdWiden, holdDrift int64, tradeDate, session string) []string {
	// W-T1-CURRENCIES: the same resolved set the arm gate (t1WindowsFor)
	// reads — hard lines for the set, advisory lines for every other T1 event.
	ccy := at.t1Currencies()
	if !holdHave || holdWiden <= 0 {
		return kernel.T1NoTradeLines(cal, ccy)
	}
	lines := kernel.T1NoTradeLinesDrift(cal, ccy, holdDrift)
	at.logWarnf("🕰 clock-hold: T1 news windows widened by %dm (|drift| %dms, cap %dm) for %s %s (F6)",
		kernel.ClockWidenMinutes(holdDrift), holdWiden, kernel.ClockWidenCapMinutes, tradeDate, session)
	if note := kernel.ClockDriftStaleNote(holdDrift); note != "" {
		at.logWarnf("🕰 clock-hold: %s — %s %s (CLASS 145)", note, tradeDate, session)
	}
	return lines
}

// clockHoldDriftFn is the F6 measurement seam: tests inject fake drift; the
// production default measures the freshest feed bar (kernel.FeedClockDriftMs).
var clockHoldDriftFn = func(symbol string) (int64, bool) {
	return kernel.FeedClockDriftMs(symbol)
}

// clockHoldAuthoring returns the F6 clock-hold verdict for plan authoring:
// deferred (true when |drift| > C2 tolerance), the widening band (0 = none),
// and the raw measurement for the log lines.
func (at *AutoTrader) clockHoldAuthoring() (deferred bool, widenMs int64, driftMs int64, have bool) {
	driftMs, have = clockHoldDriftFn(at.futuresSymbol())
	deferred, widenMs = kernel.ClockHoldDecision(driftMs, have, kernel.ClockWarnMs(), kernel.C2ToleranceMs())
	return
}

// clockHoldDeferLine renders the exact authoring-deferred log line (pure, so
// the F6 fixture can assert the wording the journal will carry).
func clockHoldDeferLine(tradeDate, session string, widenMs, tolMs int64) string {
	return fmt.Sprintf("🕰 clock-hold: planner authoring DEFERRED for %s %s (|drift| %dms > tolerance %dms) — no plan written, no budget consumed; exits and armed management unaffected (F6)",
		tradeDate, session, widenMs, tolMs)
}

// aiPlanMaxTokens is the planner completion budget (F1a, LONDON-FORENSICS
// 2026-08-28): AI_PLAN_MAX_TOKENS, default 65536 — 2× the observed 32768-token
// truncation ceiling. Provider ceiling is 393216 (probed 2026-08-19).
func aiPlanMaxTokens() int {
	if v := os.Getenv("AI_PLAN_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 65536
}

// PlannerMaxTokens (S1-wave A4, 2026-08-29) — exported boot-line accessor so
// the boot block prints BOTH caps (client vs planner) instead of the
// misleading single 32768.
func PlannerMaxTokens() int { return aiPlanMaxTokens() }

// runPlannerRead assembles the input package, calls the pinned planner client,
// and persists the plan (or a fail-closed NO-TRADE plan).
func (at *AutoTrader) runPlannerRead(session, tradeDate string) {
	at.runPlannerReadWithTrigger(session, tradeDate, "")
}

// priorPlanLevelLines renders the previous version's levels as "price label"
// lines for the continuity block (empty when the stored doc is unreadable).
func priorPlanLevelLines(row *store.PlanDB) []string {
	if row == nil {
		return nil
	}
	var doc kernel.PlanDoc
	if json.Unmarshal([]byte(row.Doc), &doc) != nil {
		return nil
	}
	lines := make([]string, 0, len(doc.Levels))
	for _, l := range doc.Levels {
		lines = append(lines, fmt.Sprintf("%.2f %s", l.Price, l.Label))
	}
	return lines
}

// htfLabels maps the planner's HTF-zone section prices → labels (the fvg
// origin-level check accepts a seated-table OR HTF-section anchor).
func htfLabels(in kernel.PlannerInput) map[float64]string {
	if len(in.HTFZones) == 0 {
		return nil
	}
	out := make(map[float64]string, len(in.HTFZones))
	for _, z := range in.HTFZones {
		out[z.Price] = z.Label
	}
	return out
}

// runPlannerReadCoreWithTrigger is runPlannerReadWithTriggerClaimed with the old
// signature (claim result discarded) for existing callers.
func (at *AutoTrader) runPlannerReadWithTrigger(session, tradeDate, triggerOverride string) {
	_ = at.runPlannerReadWithTriggerClaimed(session, tradeDate, triggerOverride)
}

// runPlannerReadCore is the testable core: ≤2 retries, then FAIL-CLOSED to a
// NO-TRADE plan (never a stale plan, never nothing). Writes the append-only plan
// row. Returns (version, lifecycle, err).
func (at *AutoTrader) runPlannerReadCore(session, tradeDate, modelID, promptHash, indicatorsBlock, aiConfigHash string, call func() (string, error), extraNoTrade ...string) (int, string, error) {
	return at.runPlannerReadCoreWithTrigger(session, tradeDate, "", modelID, promptHash, indicatorsBlock, aiConfigHash, call, extraNoTrade...)
}

// runPlannerReadCoreWithTrigger is runPlannerReadCore with an explicit
// trigger_reason and NO facts (schema-only validation — legacy callers/tests).
func (at *AutoTrader) runPlannerReadCoreWithTrigger(session, tradeDate, triggerOverride, modelID, promptHash, indicatorsBlock, aiConfigHash string, call func() (string, error), extraNoTrade ...string) (int, string, error) {
	return at.runPlannerReadCoreWithFacts(session, tradeDate, triggerOverride, modelID, promptHash, indicatorsBlock, aiConfigHash, kernel.PlanFacts{}, call, extraNoTrade...)
}

// runPlannerReadCoreWithFacts is the production core: same retry/fail-closed
// loop PLUS the P0.1/P0.2 facts validation (both-side levels, continuation
// scenario on gaps, duplicate/target reachability). Legacy callers keep the
// facts-free signature (schema-only validation).
func (at *AutoTrader) runPlannerReadCoreWithFacts(session, tradeDate, triggerOverride, modelID, promptHash, indicatorsBlock, aiConfigHash string, facts kernel.PlanFacts, call func() (string, error), extraNoTrade ...string) (int, string, error) {
	// Legacy signature: no reject block (schema-only callers/tests).
	return at.runPlannerReadCoreWithFactsGrades(session, tradeDate, triggerOverride, modelID, promptHash, indicatorsBlock, aiConfigHash, "", "", facts, nil, nil, nil, true, func(userPrompt string) (string, error) {
		return call()
	}, extraNoTrade...)
}

// runPlannerReadCoreWithFactsGrades is the production core + the machine-grade
// carryMachineGrades (level-truth wave, 2026-08-27) stamps doc levels that the
// current pool could not match (carried nPOC / out-of-seat rows) with the
// PREVIOUS version's machine grade for the same rounded price. A level reused
// across versions keeps its machine verdict instead of silently losing the
// stamp. Fetches the latest prior version for (tradeDate, session, at.id) —
// at stamp time the new version has not been appended yet, so "latest" IS the
// prior version.
func (at *AutoTrader) carryMachineGrades(tradeDate, session string, doc *kernel.PlanDoc) {
	if at.store == nil || doc == nil {
		return
	}
	prev, err := at.store.Plan().GetLatestPlanForTraderSession(tradeDate, session, at.id)
	if err != nil || prev == nil {
		return
	}
	pd := kernel.PlanDoc{}
	if json.Unmarshal([]byte(prev.Doc), &pd) != nil {
		return
	}
	carry := map[float64]string{}
	for _, l := range pd.Levels {
		if l.Price <= 0 {
			continue
		}
		g := l.MachineGrade
		if g == "" {
			g = l.Grade
		}
		if g == "" {
			continue
		}
		k := math.Round(l.Price*100) / 100
		// Keep the stronger carry grade on price collisions.
		if old, ok := carry[k]; !ok || kernel.GradeRank(g) > kernel.GradeRank(old) {
			carry[k] = g
		}
	}
	if len(carry) == 0 {
		return
	}
	for i := range doc.Levels {
		if doc.Levels[i].MachineGrade != "" {
			continue
		}
		if g, ok := carry[math.Round(doc.Levels[i].Price*100)/100]; ok {
			doc.Levels[i].MachineGrade = g
		}
	}
}

// stamp (master-audit finding 8.4): machineGrades maps rounded level price →
// deterministic detector grade from the Go-ranked candidate table; plan levels
// that match get their machine grade persisted for the card to display beside
// the model-written one.
// recordPlanWritePrice (F3) snapshots the price at the last successful plan
// write — the fast-market drift baseline for the next wake read.
// collectMachineGrades (S1-wave A3, 2026-08-29) — builds the write-site stamp
// maps from EVERY level source the prompt renders: the seated table, the graded
// pool, the cap-4 HTF-zones section, AND the full HTF-zone universe
// (HTFZonesFull). The 13 Demand·1h escapes were universe rows the model wrote
// from the key-levels block while the stamp map only knew the cap-4 section.
func collectMachineGrades(in kernel.PlannerInput, grades, labels map[float64]string) {
	record := func(price float64, grade string) {
		if grade == "" {
			return
		}
		k := math.Round(price*100) / 100
		// Collision rule: keep the STRONGER grade per rounded price — an
		// owner "A" prepended first is never clobbered by a same-price
		// detector entry later in the slice.
		if old, ok := grades[k]; !ok || kernel.GradeRank(grade) > kernel.GradeRank(old) {
			grades[k] = grade
		}
	}
	recordLabel := func(price float64, label string) {
		if label == "" {
			return
		}
		k := math.Round(price*100) / 100
		if _, ok := labels[k]; !ok {
			labels[k] = label
		}
	}
	for _, l := range in.Levels {
		record(l.Price, l.Grade)
		recordLabel(l.Price, l.Label)
	}
	for _, pl := range in.Pool {
		record(pl.Price, pl.Grade)
		recordLabel(pl.Price, pl.Label)
	}
	for _, z := range in.HTFZones {
		record(z.Price, z.Grade)
		recordLabel(z.Price, z.Label)
	}
	for _, z := range in.HTFZonesFull {
		record(z.Price, z.Grade)
		recordLabel(z.Price, z.Label)
	}
}

func (at *AutoTrader) recordPlanWritePrice(p float64) {
	if p > 0 {
		at.lastPlanWritePrice.Store(math.Float64bits(p))
	}
}

// fastMarketDrift (F3) returns (driftPts, driftAtr) when the price has moved
// more than FAST_MARKET_ATR × ATR5m since the last plan write; (0,0) otherwise.
func (at *AutoTrader) fastMarketDrift(price float64) (float64, float64) {
	last := math.Float64frombits(at.lastPlanWritePrice.Load())
	if last <= 0 || price <= 0 {
		return 0, 0
	}
	drift := math.Abs(price - last)
	var bars []market.Kline
	if market.FuturesBarsProvider != nil {
		bars = market.FuturesBarsProvider(at.futuresSymbol(), "1m", kernel.AISVPBarCount)
	}
	atr5m := kernel.StaleConfirmATR5m(bars)
	if atr5m <= 0 || drift <= fastMarketATR()*atr5m {
		return 0, 0
	}
	return drift, drift / atr5m
}

// plannerRejectBlock renders the retry-append tail (CHANGE 2, owner ruling
// 2026-08-31): the previous attempt's validator reason VERBATIM plus a fixed
// instruction. Port of the weekly retry pattern — the 2026-08-31 LONDON read
// burned attempts 1+2 on the IDENTICAL split-arm reject because the session
// retry never told the model why it was rejected.
func plannerRejectBlock(err error, live []string, struct4h string) string {
	if err == nil {
		return ""
	}
	block := "\n\n## PREVIOUS ATTEMPT REJECTED / Validator reason (verbatim):\n" + err.Error() + "\nFix ONLY this defect, keep the rest structurally identical." + kernel.LiveConditionsLine(live)
	// S3 (2026-09-16) — when the structure table holds a directional 4h trend,
	// the repair prompt carries the relation vocabulary it is judged by.
	switch struct4h {
	case "up", "down":
		block += "\n" + kernel.RepairStructureRelationLaw
	}
	return block
}

// ── CLASS 45 E4 (owner addition, 2026-09-02) — THE CHAIN'S CUMULATIVE REJECTS,
// AT THE TOP AND THE TAIL ────────────────────────────────────────────────────
//
// Measured on LONDON 2026-09-02 (rows 92/93/94): attempt 3's block carried
// attempt 2's fade defect, NOT attempt 1's void — so the model was corrected
// about the fade and walked straight back into the void it had already been
// rejected for. A block that names only the LAST defect can only ever teach the
// model to avoid its most recent mistake.
//
// It also sat in the last 239 chars — 59 of 6,602 tokens, under 1%, at 99%
// depth — against a standing MUST at 70% depth. Correction has to arrive before
// the rules it overrides, not after them.

// addDistinctReject appends a reason to the chain history if it is new.
// Distinctness is by exact text: two rejects that read identically ARE the same
// defect, and the whack-a-mole counter already tracks repeats separately.
func addDistinctReject(history []string, err error) []string {
	if err == nil {
		return history
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return history
	}
	for _, h := range history {
		if h == msg {
			return history
		}
	}
	return append(history, msg)
}

// plannerRejectHeader is the TOP block: every distinct defect seen so far in
// THIS read, plus the override sentence that resolves any conflict with the
// standing rules below it.
func plannerRejectHeader(history []string, live []string) string {
	if len(history) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## CORRECTIONS FROM THIS READ — read these FIRST\n")
	b.WriteString("The standing rules below still apply EXCEPT where this correction overrides them.\n")
	if len(history) == 1 {
		b.WriteString("Your previous attempt was REJECTED for:\n")
	} else {
		fmt.Fprintf(&b, "This read has already been rejected %d times, for %d DISTINCT defects. Avoid ALL of them, not only the last:\n", len(history), len(history))
	}
	for i, h := range history {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, h)
	}
	b.WriteString(kernel.LiveConditionsLine(live))
	b.WriteString("\n\n")
	return b.String()
}

// plannerReadLine (WAVE PLANNER B3) is the ONE per-read line: session,
// attempts, each reject's item class (the shared kernel classifier), the
// read→publish latency and the final lifecycle. n/a when the read never
// published a born-check (no_trade / fail-closed reads record none).
func plannerReadLine(session string, attempts int, rejectReasons []string, readMs, publishMs *int64, lifecycle string) string {
	classes := make([]string, 0, len(rejectReasons))
	for _, r := range rejectReasons {
		classes = append(classes, kernel.PlannerRejectItemClass(r))
	}
	classesPart := "none"
	if len(classes) > 0 {
		classesPart = strings.Join(classes, ",")
	}
	latency := "n/a"
	if readMs != nil && publishMs != nil {
		latency = fmt.Sprintf("%dms", *publishMs-*readMs)
	}
	return fmt.Sprintf("🧭 planner read: session=%s attempts=%d reject_classes=%s read→publish=%s lifecycle=%s",
		session, attempts, classesPart, latency, lifecycle)
}

// logPlannerReadLine (WAVE PLANNER B3) emits the ONE 🧭 per-read line and
// records its counters — read total + per-class rejects, counted from the
// read's own recorded history, never inferred from row counts.
func (at *AutoTrader) logPlannerReadLine(session string, attempts int, rejectReasons []string, readMs, publishMs *int64, lifecycle string) {
	at.logInfof("%s", plannerReadLine(session, attempts, rejectReasons, readMs, publishMs, lifecycle))
	if at.store == nil {
		return
	}
	_, _ = store.IncSystemCounter(at.store, "planner:read")
	seen := map[string]bool{}
	for _, r := range rejectReasons {
		c := kernel.PlannerRejectItemClass(r)
		if !seen[c] {
			seen[c] = true
			_, _ = store.IncSystemCounter(at.store, "planner:read_reject_"+c)
		}
	}
}

// plannerRejectTail repeats the same cumulative list at the end — the model
// reads a 6.6k-token prompt; the correction appears at both ends of it.
func plannerRejectTail(history []string, live []string) string {
	if len(history) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## CORRECTIONS FROM THIS READ (repeated — these override the rules above)\n")
	for i, h := range history {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, h)
	}
	b.WriteString("Fix ALL of the above; keep the rest structurally identical.")
	b.WriteString(kernel.LiveConditionsLine(live))
	return b.String()
}

// repairUnparseableLine (CLASS 38 F6, 2026-09-01) renders the loud line for a
// repair attempt whose output would not parse: the parse error, the defect the
// repair was aimed at, and the HEAD of what the model actually sent. Before
// this the journal carried one bare sentence, so rejected-prompt row 79 could
// only be reconstructed from the DB. Pure, so the fixture pins the wording;
// bounded, so a 30k malformed response cannot flood the journal (class 12).
// Retry semantics are untouched — the caller still falls back to ONE full
// re-author.
func repairUnparseableLine(raw, repairingReason string, parseErr error) string {
	return fmt.Sprintf("🧩 repair returned UNPARSEABLE output — falling back to a full re-author next attempt · parse_err=%v · was repairing: %s · raw_head=%q",
		parseErr, clampLine(repairingReason, 200), clampLine(raw, 400))
}

// clampLine truncates to n runes on ONE line (newlines collapsed) with an
// explicit ellipsis, so a log line can never be mistaken for the whole payload.
func clampLine(s string, n int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// plannerRejectBookkeeping (planner-speed wave 1.4/3.4, 2026-08-31) runs at
// every reject site: persists the rejected attempt's verbatim prompt + reason
// for the offline A/B, bumps the whack-a-mole counter when attempt N repeats
// attempt N-1's defect, and (WAVE PLANNER B2 follow-up) persists the AI's RAW
// answer so a future replay can re-check the same text the live attempt
// produced.
func (at *AutoTrader) plannerRejectBookkeeping(attempt int, tradeDate, session, hash, userPrompt, raw string, rejectErr error, prevReason *string, factsJSON ...string) {
	if rejectErr == nil {
		return
	}
	if attempt >= 2 && *prevReason != "" && samePlannerDefect(*prevReason, rejectErr.Error()) {
		telemetry.IncRepairRegression(at.id)
		at.logWarnf("🎯 repair regression: attempt %d repeated the attempt %d defect — %s", attempt, attempt-1, rejectErr.Error())
	}
	*prevReason = rejectErr.Error()
	if at.store != nil {
		// ROOT-FIX B-1: the facts snapshot travels with the prompt so an offline
		// A/B runs the FULL validator chain, not just the schema gate.
		fj := ""
		if len(factsJSON) > 0 {
			fj = factsJSON[0]
		}
		if serr := at.store.PlannerRejected().SaveRejectedPromptWithFacts(at.id, tradeDate, session, hash, attempt, rejectErr.Error(), userPrompt, raw, fj); serr != nil {
			at.logWarnf("🧾 rejected-prompt persist failed: %v", serr)
		}
	}
}

// samePlannerDefect compares two reject reasons on a normalized prefix — the
// repeated-defect class (split-arm, breakdown-void) repeats VERBATIM, so a
// 120-char prefix match is exact in practice and robust to tiny drift.
func samePlannerDefect(a, b string) bool {
	norm := func(s string) string {
		s = strings.TrimSpace(s)
		if len(s) > 120 {
			s = s[:120]
		}
		return s
	}
	return norm(a) == norm(b)
}

// resolvePlannerRetryMode delegates to the kernel resolver (RETRY_MODE env).
func resolvePlannerRetryMode() string { return kernel.ResolvePlannerRetryMode() }

// plannerStreamIdle delegates to the kernel resolver (AI_PLAN_STREAM_IDLE_SECS).
func plannerStreamIdle() time.Duration {
	return time.Duration(kernel.PlannerStreamIdleSeconds()) * time.Second
}

// plannerStreamTotal (class 37) delegates to the kernel resolver
// (AI_PLAN_TOTAL_DEADLINE_SECS, default 1200; always > idle).
func plannerStreamTotal() time.Duration {
	return time.Duration(kernel.PlannerStreamTotalSeconds()) * time.Second
}

// plannerClientBootLine (class 37, C7) renders the EFFECTIVE planner client
// config — resolved values, never file defaults — for the per-trader boot
// block: both stream deadlines, the HTTP ceiling that still governs the
// non-stream paths, retries/backoff and the provider ROW the trader is bound
// to. Pure so the fixture can pin the wording.
// R2 (2026-09-02) — RENAMED from "planner client". Two lines began with the
// same 🛰 and reported DIFFERENT retry numbers: this one (retries=2 backoff=2s,
// the non-stream executor path) and the class-46 stream policy line (tries=3
// backoff=2s→15s→45s). A reader scanning the boot log could not tell which
// governed what. This one names the path it actually describes; every value is
// still READ from the resolved snapshot, none is literal.
func plannerClientBootLine(providerRow string, idleS, totalS, httpCeilingS, retries, backoffS, cap int) string {
	return fmt.Sprintf("🛰 executor client: provider_row=%s retries=%d backoff=%ds http_ceiling=%ds (non-stream paths only — executor loop, weekly read, planner Q&A) · planner stream idle=%ds total=%ds cap=%d",
		providerRow, retries, backoffS, httpCeilingS, idleS, totalS, cap)
}

// plannerStreamPolicyBootLine is RETIRED (class 46). It printed
// watchdog_log=on, keepalive=30s, serialize_executor=off and
// resend_identical=on as STRING LITERALS, and its fixture asserted the same
// literals — so the line could not drift from the code, only from reality, and
// it did: keepalive on the wire was 14-20 s while the line said 30. Every
// field now comes from mcp.PlannerClientPolicyLine(), whose fixture calls the
// enforcing functions.

func (at *AutoTrader) logPlannerClientBootLine() {
	ai := mcp.EffectiveAIParamsSnapshot("")
	at.logInfof("%s", plannerClientBootLine(at.config.AIModelID, kernel.PlannerStreamIdleSeconds(), kernel.PlannerStreamTotalSeconds(), ai.TimeoutSeconds, ai.MaxRetries, ai.RetryBackoffSeconds, aiPlanMaxTokens()))
	at.logInfof("%s", mcp.PlannerClientPolicyLine())
	at.installWatchdogFireRecorder() // owner ruling 2026-09-02 — record every fire
}

// sundayAsiaDeferred (A2, owner ruling 2026-08-31) — the Sunday sequencing
// gate: the ASIA session read (now 16:30) defers while Sunday's weekly read
// (also 16:30) has not yet landed this week's doc. Pure + fixture-tested; the
// per-cycle retry makes the ASIA read fire right after the weekly write.
// Weekdays (and a landed doc) defer nothing.
func sundayAsiaDeferred(s *kernel.SessionDef, now time.Time, weeklyDoc *kernel.WeeklyDoc) bool {
	if s == nil || s.Name != kernel.SessionAsia {
		return false
	}
	if now.In(kernel.CTLocation()).Weekday() != time.Sunday {
		return false
	}
	return weeklyDoc == nil
}

// estimatePromptTokens is the cheap ~4 chars/token estimate used in the
// repair-vs-author size log lines (exact counts come from provider usage).
func estimatePromptTokens(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

// runPlannerReadCoreWithFactsGrades is the full write path (see its doc above).
func (at *AutoTrader) runPlannerReadCoreWithFactsGrades(session, tradeDate, triggerOverride, modelID, promptHash, indicatorsBlock, aiConfigHash, requiredBias, prompt string, facts kernel.PlanFacts, machineGrades map[float64]string, machineLabels map[float64]string, htfLabels map[float64]string, failClosed bool, call func(userPrompt string) (string, error), extraNoTrade ...string) (int, string, error) {
	return at.runPlannerReadCoreWithFactsGradesClock(time.Now, session, tradeDate, triggerOverride, modelID, promptHash, indicatorsBlock, aiConfigHash, requiredBias, prompt, facts, machineGrades, machineLabels, htfLabels, failClosed, call, extraNoTrade...)
}

func (at *AutoTrader) runPlannerReadCoreWithFactsGradesClock(authoringClock func() time.Time, session, tradeDate, triggerOverride, modelID, promptHash, indicatorsBlock, aiConfigHash, requiredBias, prompt string, facts kernel.PlanFacts, machineGrades map[float64]string, machineLabels map[float64]string, htfLabels map[float64]string, failClosed bool, call func(userPrompt string) (string, error), extraNoTrade ...string) (int, string, error) {
	return at.runPlannerReadCoreObserved(authoringClock, nil, session, tradeDate, triggerOverride, modelID, promptHash, indicatorsBlock, aiConfigHash, requiredBias, prompt, facts, machineGrades, machineLabels, htfLabels, failClosed, call, extraNoTrade...)
}

// plannerMaxAttempts is the single source of the attempt-loop bound
// (W-WRITE-TIME-FEASIBILITY NIT: the old literal `attempt < 3` duplicated it).
const plannerMaxAttempts = 3

func (at *AutoTrader) runPlannerReadCoreObserved(authoringClock func() time.Time, researchTrace *researchsnapshot.PlanTrace, session, tradeDate, triggerOverride, modelID, promptHash, indicatorsBlock, aiConfigHash, requiredBias, prompt string, facts kernel.PlanFacts, machineGrades map[float64]string, machineLabels map[float64]string, htfLabels map[float64]string, failClosed bool, call func(userPrompt string) (string, error), extraNoTrade ...string) (int, string, error) {
	// H4/H5 — validation must accept EXACTLY what the config allows: the resolved
	// max_levels / scenario_cap (hard ceilings 12/5). Before this the parse
	// hardcoded 8/3, so raising either setting made EVERY read fail-closed into a
	// NO-TRADE plan + P0 alert — the upper half of the UI range was unreachable.
	maxLevels, _, _, _, _ := resolveSessionPlanCfg(at.dayPlanCfg(), session)
	scenarioCap := at.scenarioCap()

	var authoredAt time.Time
	var bornCheck *kernel.BornCheck // W2 A2 — the accepted attempt's record
	var doc *kernel.PlanDoc
	// CLASS 34 (owner ruling 2026-08-31): the reject block now carries the
	// RESOLVED live condition vocabulary so the model can never be hinted
	// toward an unknown or shadowed condition name.
	baseCond := map[string]string(nil)
	var sessCond map[string]string
	if cfg := at.dayPlanCfg(); cfg != nil {
		baseCond = cfg.ConditionStatus
		if o := cfg.SessionOverride(session); o != nil && o.ConditionStatus != nil {
			sessCond = *o.ConditionStatus
		}
	}
	liveConditions := kernel.ResolvedLiveConditions(baseCond, sessCond, kernel.ShadowConditionsEnv())

	var lastErr error
	// RETRY-APPEND-REJECT-REASON (owner ruling 2026-08-31): attempt N≥2 carries
	// the PREVIOUS attempt's validator reason VERBATIM in the prompt tail — the
	// 2026-08-31 LONDON read burned attempts 1+2 on the IDENTICAL split-arm
	// reject because the retry never told the model why it was rejected.
	// PLANNER SPEED WAVE (2026-08-31): attempt ≥2 sends the REPAIR call by
	// default (RETRY_MODE=repair|reauthor) — rejected output + errors verbatim
	// + law excerpts only, a fraction of a full re-author's tokens.
	rejectBlock := ""
	// CLASS 45 E4 — every DISTINCT reject this read has produced, in order.
	var rejectHistory []string
	retryMode := resolvePlannerRetryMode()
	var prevReason string
	lastRaw := ""
	forceReauthor := false
	resendIdentical := ""        // class 41 M0: the exact prompt a provider-failed attempt sent
	resendAfterWatchdog := false // the prior attempt died on a watchdog close
	resendStart := time.Time{}
	lastAttempt := 0                            // WAVE PLANNER B3 — the final attempt count, recorded by the read line
	for attempt := 1; attempt <= 3; attempt++ { // 1 + ≤2 retries
		lastAttempt = attempt
		researchTrace.Finish(lastErr)
		userPrompt := prompt
		modeLabel := "author"
		if attempt >= 2 && resendIdentical != "" {
			// CLASS 41 M0 (owner ruling class 37, 2026-09-01; implemented
			// 2026-09-02): a transport/deadline failure produced NO model
			// answer, so there is nothing to repair and no validator reason —
			// re-send the IDENTICAL prompt, no reject block appended.
			userPrompt = resendIdentical
			resendIdentical = ""
			modeLabel = "resend-identical"
			at.logInfof("🧩 planner attempt %d/3 %s: prompt ~%d tokens (byte-identical to the provider-failed attempt; no reject block)", attempt, modeLabel, estimatePromptTokens(userPrompt))
		} else if attempt >= 2 {
			if retryMode == "repair" && !forceReauthor && lastRaw != "" && lastErr != nil {
				userPrompt = kernel.BuildPlannerRepairPrompt(lastRaw, lastErr.Error(), liveConditions)
				modeLabel = "repair"
			} else {
				// CLASS 45 E4: the cumulative corrections lead the prompt AND
				// close it. rejectBlock (the single-defect legacy tail) is kept
				// only when the history is somehow empty, so behaviour degrades
				// to the old shape rather than to nothing.
				if header := plannerRejectHeader(rejectHistory, liveConditions); header != "" {
					userPrompt = header + prompt + plannerRejectTail(rejectHistory, liveConditions)
					modeLabel = fmt.Sprintf("reauthor+block(top+tail, %d distinct)", len(rejectHistory))
				} else {
					userPrompt = prompt + rejectBlock
					modeLabel = "reauthor+block"
				}
			}
			at.logInfof("🧩 planner attempt %d/3 %s: prompt ~%d tokens (full-author ~%d tokens)", attempt, modeLabel, estimatePromptTokens(userPrompt), estimatePromptTokens(prompt))
		}
		researchTrace.Begin(attempt, modeLabel, userPrompt)
		raw, err := call(userPrompt)
		researchTrace.Reply(raw, err)
		if resendAfterWatchdog && modeLabel == "resend-identical" {
			note := "resend landed"
			if err != nil {
				note = mcp.ClassifyAIError(err)
			}
			at.recordWatchdogResend(err == nil, time.Since(resendStart), note)
			resendAfterWatchdog = false
		}
		if err != nil && mcp.IsProviderFailure(err) {
			// CLASS 41 M0: provider failure → the next attempt re-sends this
			// exact prompt. No reject block, no rejected-prompt row (that table
			// samples VALIDATOR rejects; the 🛰/ai_call lines already carry the
			// class), lastRaw/lastErr untouched so a later validator reject
			// still repairs against the last real model output.
			at.logWarnf("📐 planner attempt %d/3 failed on the provider (class=%s) — attempt %d re-sends the IDENTICAL prompt: %v", attempt, mcp.ClassifyAIError(err), attempt+1, err)
			researchTrace.Finish(err)
			resendIdentical = userPrompt
			// Owner ruling 2026-09-02 — remember a watchdog close so the NEXT
			// attempt's outcome attaches to the open fire row.
			resendAfterWatchdog = mcp.ClassifyAIError(err) == string(mcp.ClassIdle)
			resendStart = time.Now()
			if attempt == 3 {
				lastErr = err
			}
			continue
		}
		lastRaw = raw
		if err != nil {
			lastErr = err
			at.logWarnf("📐 planner attempt %d/3 failed: %v", attempt, err)
			at.plannerRejectBookkeeping(attempt, tradeDate, session, promptHash, userPrompt, lastRaw, lastErr, &prevReason, FactsSnapshotJSON(facts))
			rejectBlock = plannerRejectBlock(lastErr, liveConditions, kernel.StructureTrend4h(facts.Structure))
			rejectHistory = addDistinctReject(rejectHistory, lastErr)
			continue
		}
		if modeLabel == "repair" && kernel.IsPlanFragment(raw) {
			// REPAIR-PARSE E2: a partial document gets its OWN reason instead of
			// a confusing schema error. Not observed in the 2026-09-01 journals
			// — a guard, not a fix for a measured failure.
			lastErr = fmt.Errorf("%s", kernel.FragmentReason)
			forceReauthor = true
			at.recordRepairOutcome(raw, lastErr, prevReason)
			at.plannerRejectBookkeeping(attempt, tradeDate, session, promptHash, userPrompt, lastRaw, lastErr, &prevReason, FactsSnapshotJSON(facts))
			rejectBlock = plannerRejectBlock(lastErr, liveConditions, kernel.StructureTrend4h(facts.Structure))
			rejectHistory = addDistinctReject(rejectHistory, lastErr)
			continue
		}
		// W-EXEC-TRUTH W3 (R4): the resolved entry-policy default is stamped at
		// parse (before the validator) and the armable hold floor runs beside
		// the A5 prose check — ONE resolution shared with the shadow A/B replay.
		d, perr := kernel.ParsePlanDocForAuthoring(raw, maxLevels, scenarioCap, at.plannerAuthoringOpts())
		if perr != nil {
			lastErr = perr
			at.logWarnf("📐 planner attempt %d/3 parse/schema rejected: %v", attempt, perr)
			if modeLabel == "repair" {
				forceReauthor = true // 3.6 — a malformed repair falls back to one full re-author
				at.recordRepairOutcome(raw, perr, prevReason)
			}
			at.plannerRejectBookkeeping(attempt, tradeDate, session, promptHash, userPrompt, lastRaw, lastErr, &prevReason, FactsSnapshotJSON(facts))
			rejectBlock = plannerRejectBlock(lastErr, liveConditions, kernel.StructureTrend4h(facts.Structure))
			rejectHistory = addDistinctReject(rejectHistory, lastErr)
			continue
		}
		// W6-D (2026-08-25) — the G5 write-time demotion is REMOVED: stamping
		// scenarios consumed + C at plan birth contradicted P1c (a consumed
		// level's RETOUCH is the tradeable role-flip event) and poisoned 88%
		// of scenarios at write (42/48 on 2026-08-25). The LIVE per-cycle
		// evaluator still tracks consumption — scenarios demote only when a
		// level is actually re-touched after the plan is born, never at
		// birth. demoteConsumedScenarios stays for reference/tests only.
		// CLASS 39 (owner ruling 2026-09-01) — normalize, don't reject: inside
		// ParsePlanDocCapped the kernel dropped legs from every non-sweep arm
		// whose single top-level arm validates, and recorded each event on the
		// doc. Surface every one LOUDLY (A9) — the WARN names the condition, the
		// scenario, every dropped leg and the kept arm — and RECORD the count
		// (D5: system_config, survives restarts; the class-35 lesson).
		for _, n := range d.ArmNormalizations {
			at.logWarnf("%s", kernel.ArmNormalizationWarn(n))
			if at.store != nil {
				if cnt, cerr := store.IncArmsNormalized(at.store); cerr != nil {
					at.logWarnf("⚖ arms_normalized_class39 counter write failed: %v", cerr)
				} else {
					at.logInfof("⚖ arms_normalized_class39 = %d (recorded)", cnt)
				}
			}
		}
		// P0.4-C (2026-08-24) — the model may write near-duplicate levels (2.13
		// pts apart killed ASIA v2 with the duplicate-seat rule). Collapse them
		// with the same cluster tolerance the scorer uses, instead of burning
		// the retries → fail-closed session.
		if collapsed, n := kernel.CollapsePlanLevels(d.Levels, kernel.LevelClusterTicks*0.25); n > 0 {
			d.Levels = collapsed
			at.logWarnf("📐 plan level auto-collapse: %d near-duplicate level(s) merged at %s write.", n, session)
		}
		// P0.4-G (2026-08-25) — flip-fired re-plan bias enforcement: the prior
		// plan's flip ALREADY FIRED on machine-evaluated bars, so the new plan's
		// bias MUST match the flipped direction. Live bug: ASIA v3's flip fired
		// "→ bias long" but v4 came back short-biased — the flip was honored by
		// the evaluator and ignored by the re-planner.
		if requiredBias != "" && strings.ToLower(strings.TrimSpace(d.Bias.Direction)) != requiredBias {
			lastErr = fmt.Errorf("prior plan flip already fired → bias %s is MANDATORY, got %q — the flip cannot be re-written away", requiredBias, d.Bias.Direction)
			at.plannerRejectBookkeeping(attempt, tradeDate, session, promptHash, userPrompt, lastRaw, lastErr, &prevReason, FactsSnapshotJSON(facts))
			at.logWarnf("📐 planner attempt %d/3 rejected: %v", attempt, lastErr)
			rejectBlock = plannerRejectBlock(lastErr, liveConditions, kernel.StructureTrend4h(facts.Structure))
			rejectHistory = addDistinctReject(rejectHistory, lastErr)
			continue
		}
		// P0.4-H (2026-08-25) — label provenance: a plan level whose price
		// matches a machine-table row must NOT re-label it as a DIFFERENT
		// structural anchor. Live bug: LONDON v1 labeled 29297.75 "PDH" when
		// the table row at that price is a zone and the true prior-day high
		// was 29290.5 — the flip anchor rode a phantom label.
		if mis := kernel.MislabeledStructuralLevels(d, machineLabels); len(mis) > 0 {
			lastErr = fmt.Errorf("level label provenance: %s — copy the machine table's label for these prices", strings.Join(mis, "; "))
			at.plannerRejectBookkeeping(attempt, tradeDate, session, promptHash, userPrompt, lastRaw, lastErr, &prevReason, FactsSnapshotJSON(facts))
			at.logWarnf("📐 planner attempt %d/3 rejected: %v", attempt, lastErr)
			rejectBlock = plannerRejectBlock(lastErr, liveConditions, kernel.StructureTrend4h(facts.Structure))
			rejectHistory = addDistinctReject(rejectHistory, lastErr)
			continue
		}
		// P0.1 facts rules (count concept removed by owner ruling 2026-08-31):
		// 0 on a side = hard fail; empty machine map = hard fail; continuation
		// scenario on a gap out of the prior range, no duplicate seats,
		// reachable targets. Everything else fails → retry → fail-closed.
		if verr := kernel.ValidatePlanDocWithFactsMachine(d, facts, machineLabels, maxLevels, scenarioCap); verr != nil {
			lastErr = verr
			at.plannerRejectBookkeeping(attempt, tradeDate, session, promptHash, userPrompt, lastRaw, lastErr, &prevReason, FactsSnapshotJSON(facts))
			rejectBlock = plannerRejectBlock(lastErr, liveConditions, kernel.StructureTrend4h(facts.Structure))
			rejectHistory = addDistinctReject(rejectHistory, lastErr)
			at.logWarnf("📐 planner attempt %d/3 rejected: %v", attempt, verr)
			if modeLabel == "repair" {
				at.recordRepairOutcome(raw, verr, prevReason)
			}
			continue
		}
		// S3 (2026-09-16) — the relation census. The validator stamped
		// relation_d / relation_4h during validation; count each scenario once
		// (4h when present, else D) per session. Counters record, never infer.
		if at.store != nil {
			for _, sc := range d.Scenarios {
				rel := sc.Relation4h
				if rel == "" {
					rel = sc.RelationD
				}
				if rel == "" {
					continue
				}
				if cnt, cerr := store.IncScenarioRelation(at.store, session, rel); cerr != nil {
					at.logWarnf("⚖ relation counter write failed (%s/%s): %v", session, rel, cerr)
				} else {
					at.logInfof("⚖ dayplan_scenarios_by_relation:%s:%s = %d (recorded)", session, rel, cnt)
				}
			}
		}
		// F4 (LONDON-FORENSICS 2026-08-28) — arm feasibility WARN, never a
		// fail: arms the gate-at-arm chain would refuse EVERY cycle (R:R <
		// ARM_MIN_RR or stop < 1×ATR5m) are surfaced so the planner learns
		// instead of printing ~120 REFUSED lines a session. Suppressed when
		// the write-time feasibility knob is ON — the verdicts below carry
		// the same information into the repair prompt (W-WRITE-TIME-
		// FEASIBILITY NIT: no double noise).
		// SHOULD-FIX 4: the SAME atr5m resolver the executor's arm seam uses
		// (1m→5m aggregated bars), never a differently-sliced ATR.
		atr5m := armSeamATR5m(at.futuresSymbol())
		if !at.writeTimeFeasibilityOn() {
			for _, w := range kernel.ArmFeasibilityWarnings(d, atr5m, at.armMinRRFor(nil), kernel.MinSLATRMult()) {
				at.logWarnf("⚔️ arm feasibility: %s (WARN — write proceeds; the gate-at-arm chain enforces)", w)
			}
		}

		// D2 (arms-follow-bias) — WIRED 2026-09-05. BiasArmWarning shipped
		// 2026-09-04 to answer the planner-shape finding and had ZERO
		// production callers: it was written, tested, and never called, so a
		// plan whose bias direction carried no armed scenario shipped silently.
		// With plan_mode=strict the arm path is the ONLY entry, so such a plan
		// cannot trade the direction it just argued for.
		//
		// WARN-FIRST per the owner ruling of 2026-09-04 recorded in
		// kernel/arms_bias_coherent.go: a hard reject would have refused
		// 50/68 longs and 66/103 shorts across 171 stored plans. The write
		// proceeds; this makes the condition visible instead of silent.
		// W3: under the market_in_zone policy acceptance/hold/breakout_retest are
		// armable — the warning follows the resolved default policy.
		if w := kernel.BiasArmWarningFor(d, kernel.ResolvedConditionStatuses(nil, nil, kernel.ShadowConditionsEnv()), entryPolicyForPrompt(at.dayPlanCfg())); w != "" {
			at.logWarnf("🧭 bias-coherent arms: %s (WARN — write proceeds; owner ruling 2026-09-04 is warn-first)", w)
		}
		// FVG ENTRY MODEL (2026-08-26) — write-time re-verification from stored
		// bars: the 3-candle relation, the gap floor, the displacement body vs
		// 5m ATR, and the origin-level membership. A fake/stale gap fails the
		// retry loop (the planner re-writes or the plan ships without it).
		if kernel.HasFvgScenario(d) {
			var fvgBars []market.Kline
			if market.FuturesBarsProvider != nil {
				fvgBars = market.FuturesBarsProvider(at.futuresSymbol(), "1m", kernel.AISVPBarCount)
			}
			origin := make(map[string]bool, len(machineLabels)+len(htfLabels)+1)
			for _, lbl := range machineLabels {
				origin[lbl] = true
			}
			for _, lbl := range htfLabels {
				origin[lbl] = true
			}
			if verr := kernel.ValidateFvgEntryScenarios(d, fvgBars, at.futuresSymbol(), origin, time.Now()); verr != nil {
				lastErr = verr
				at.plannerRejectBookkeeping(attempt, tradeDate, session, promptHash, userPrompt, lastRaw, lastErr, &prevReason, FactsSnapshotJSON(facts))
				rejectBlock = plannerRejectBlock(lastErr, liveConditions, kernel.StructureTrend4h(facts.Structure))
				rejectHistory = addDistinctReject(rejectHistory, lastErr)
				at.logWarnf("📐 planner attempt %d/3 rejected: %v", attempt, verr)
				if modeLabel == "repair" {
					at.recordRepairOutcome(raw, verr, prevReason)
				}
				continue
			}
		}
		// F1 — waterfall-class validator (2026-08-28): every
		// breakdown_continue / breakup_continue scenario is re-verified against
		// the bars (displacement ≥ BD_MIN_DISP_ATR, no reclaim, reachable
		// retest, arm chain rules). The model declares, the math verifies.
		if kernel.HasBreakdownScenario(d) {
			// VOID PARITY (2026-09-02): the tape and window come from the ONE
			// resolver the prompt's VOID list also reads. This site used to
			// fetch its own slice and judge sinceMs=0 while the prompt windowed
			// to the session day — ONL 29141.25, 20:58 CT.
			bdScope := kernel.ResolveVoidScope(at.futuresSymbol(), time.Now())
			if verr := kernel.ValidateBreakdownContinueScenarios(d, bdScope, kernel.StaleConfirmATR5m(bdScope.Bars), facts.Price, time.Now().UnixMilli()); verr != nil {
				lastErr = verr
				at.plannerRejectBookkeeping(attempt, tradeDate, session, promptHash, userPrompt, lastRaw, lastErr, &prevReason, FactsSnapshotJSON(facts))
				rejectBlock = plannerRejectBlock(lastErr, liveConditions, kernel.StructureTrend4h(facts.Structure))
				rejectHistory = addDistinctReject(rejectHistory, lastErr)
				at.logWarnf("📐 planner attempt %d/3 rejected: %v", attempt, verr)
				if modeLabel == "repair" {
					at.recordRepairOutcome(raw, verr, prevReason)
				}
				continue
			}
		}
		// P0.4-F (2026-08-25) — advisory flip/death sanity (never a gate; the
		// machine evaluator is the enforcer). 11-plan audit found 7/11 active
		// plans with flip unreachable/void: death preempting flip, flip==death,
		// or flip anchored on a level absent from the plan's own list.
		at.warnFlipDeathSanity(d)
		// ADDENDUM (1) — role-vs-scenario validator: WARN only (never a fail);
		// the AI keeps judgment, the journal makes mismatches visible at write.
		for _, m := range kernel.RoleMismatches(d) {
			at.logWarnf("🧭 role mismatch: %s", m)
		}
		// A2 (planner-contract wave 2026-08-26) — setup-chain validator:
		// WARN only when an fvg_entry lacks a sweep_reclaim precursor at a
		// non-A/B origin (the bare-gap null result). Never a fail.
		for _, m := range kernel.ChainWarnings(*d) {
			at.logWarnf("🔗 chain warning: %s", m)
		}
		// GAR-F5 (2026-08-28) — FVG demand: fresh machine gaps existed and
		// agreed with the plan bias but no fvg_entry was authored → WARN the
		// missing one-line reason. Visibility only, never a fail.
		if provider := market.FuturesBarsProvider; provider != nil {
			if bars := provider(at.futuresSymbol(), "1m", kernel.AISVPBarCount); len(bars) > 0 {
				fresh := kernel.FreshFvgCandidates(bars, at.futuresSymbol(), time.Now())
				for _, m := range kernel.FvgDemandWarnings(*d, fresh) {
					at.logWarnf("🎯 fvg demand: %s", m)
				}
			}
		}
		// Autopsy-response wave (2026-08-27) — fantasy-target advisory:
		// an armed scenario with planned R:R > 6 gets WARN-flagged (the
		// 3.28–22.88-R loser class). Never a fail.
		for _, m := range kernel.FantasyTargetWarnings(*d) {
			at.logWarnf("🔮 fantasy-target warning: %s", m)
		}
		authoredAt = authoringClock()
		// W-EXEC-TRUTH W2 A1/A2/D5: grammar refusal + every 5m group closed
		// between the read clock and now, plus the plan's death/flip lines.
		check, verr := at.validateAuthoredScenariosAt(d, session, tradeDate, facts.ReadAt, authoredAt)
		if verr != nil {
			lastErr = verr
			repairing := prevReason
			at.logWarnf("📐 planner attempt %d/3 rejected: %v", attempt, verr)
			at.plannerRejectBookkeeping(attempt, tradeDate, session, promptHash, userPrompt, lastRaw, verr, &prevReason, FactsSnapshotJSON(facts))
			rejectBlock = plannerRejectBlock(verr, liveConditions, kernel.StructureTrend4h(facts.Structure))
			rejectHistory = addDistinctReject(rejectHistory, verr)
			if modeLabel == "repair" {
				at.recordRepairOutcome(raw, verr, repairing)
			}
			continue
		}
		// W-EXEC-TRUTH W2 A3+A4 (corrections, no knob) — identity ≠ price and
		// the obstacle chain are WRITE-TIME refusals: re-author within the
		// existing attempts; attempt 3 failing → the existing fail-closed path.
		if verr := at.scenarioWriteTruth(d, facts); verr != nil {
			lastErr = verr
			at.plannerRejectBookkeeping(attempt, tradeDate, session, promptHash, userPrompt, lastRaw, lastErr, &prevReason, FactsSnapshotJSON(facts))
			rejectBlock = plannerRejectBlock(lastErr, liveConditions, kernel.StructureTrend4h(facts.Structure))
			rejectHistory = addDistinctReject(rejectHistory, lastErr)
			at.logWarnf("📐 planner attempt %d/3 rejected: %v", attempt, verr)
			if modeLabel == "repair" {
				at.recordRepairOutcome(raw, verr, prevReason)
			}
			continue
		}
		bornCheck = check
		// W-WRITE-TIME-FEASIBILITY (2026-09-18, owner "fix all") — judge
		// the SAME predicates the gate-at-arm chain runs, at write time.
		// Runs LAST among the validators (CTO SHOULD-FIX 8): hard rejects
		// above keep their attempts; this check never pre-empts them. The
		// extra round-trip cost is stated in the AUDIT-CHECKLIST class.
		// Attempts 1..N-1: restriction-with-hint via the existing repair
		// machinery (budget unchanged). The last attempt: the scenarios are
		// written arm.enabled=false + arm_disabled_reason (spec c).
		//
		// CTO BLOCKER 1: stamp the frozen zone map + scenario identity
		// into d BEFORE judging — the geometry predicate resolves against
		// them and the final stamp runs only after the loop. Gated behind
		// the knob (OFF stays byte-identical, CTO NIT). The bare kernel
		// stamp is used HERE so the in-loop copy logs nothing — the final
		// stamp after the loop emits the 🪪 lines exactly once per read
		// (CTO RECHECK item 3). Panic containment matches the wrapper's.
		if at.writeTimeFeasibilityOn() {
			d.Zones = facts.Zones
			func() {
				defer at.containLevelIdentity()
				kernel.StampAuthoredIdentity(d, facts.IdentityMap)
			}()
		}
		// W-EXEC-TRUTH W3 (c): the market_in_zone zone verdicts join the
		// feasibility issues — NOT gated by the write_time_feasibility knob
		// (D5); hinted on attempts < max, the arm disabled on the last.
		feas := at.writeTimeFeasibilityVerdicts(d, atr5m, at.config.StrategyConfig, session)
		feas = append(feas, at.writeTimeZoneVerdicts(d, atr5m, at.config.StrategyConfig, session)...)
		if len(feas) > 0 {
			if attempt < plannerMaxAttempts {
				lastErr = fmt.Errorf("%s", writeTimeFeasibilityHint(feas))
				at.plannerRejectBookkeeping(attempt, tradeDate, session, promptHash, userPrompt, lastRaw, lastErr, &prevReason, FactsSnapshotJSON(facts))
				rejectBlock = plannerRejectBlock(lastErr, liveConditions, kernel.StructureTrend4h(facts.Structure))
				rejectHistory = addDistinctReject(rejectHistory, lastErr)
				at.logWarnf("📐 planner attempt %d/%d write-time feasibility: %v", attempt, plannerMaxAttempts, lastErr)
				if modeLabel == "repair" {
					at.recordRepairOutcome(raw, lastErr, prevReason)
				}
				continue
			}
			// last attempt — write the unarmable arms disabled, never a silent write.
			at.applyWriteTimeArmDisable(d, feas, tradeDate, session)
		}
		// S5 (autopsy-response wave) — arm-authored counter: one tick per
		// arm{} spec written; the before/after gauge of the arming mandate.
		nArms := 0
		for _, s := range d.Scenarios {
			if s.Arm != nil && s.Arm.Enabled {
				nArms++
			}
		}
		for i := 0; i < nArms; i++ {
			telemetry.IncGateBlock(at.id, "arm_authored")
		}
		if modeLabel == "repair" {
			// The denominator: an accepted repair is recorded ONLY here, where
			// the doc has survived the whole validator chain (A24: a rate
			// without its base is not a rate).
			if _, ierr := store.IncRepairOutcome(at.store, string(kernel.RepairOK)); ierr != nil {
				at.logWarnf("🩹 repair counter write failed: %v", ierr)
			}
		}
		doc = d
		break
	}
	if doc != nil {
		researchTrace.Finish(nil)
	} else {
		researchTrace.Finish(lastErr)
	}
	// A3 (F5, fail-register wave): a plan whose death/flip is prose-only has NO
	// machine evaluation — the owner must know (the prompt line says it too).
	if doc != nil && doc.DeathStructured == nil {
		at.logWarnf("📜 plan death is PROSE-ONLY (no structured death{} object) — AI-judged, not machine-evaluated; only the all-levels-consumed fallback protects the chain.")
	}
	// C1 (F3) — DUAL-ACCEPT window: scenarios missing confirm{} are accepted
	// with a WARN for the first CONFIRM_GRACE_SESSIONS (default 3) distinct
	// plan sessions after this feature landed; afterwards the plan is REJECTED
	// back into the retry loop (the planner model may lag the contract).
	if doc != nil {
		missing := 0
		for _, sc := range doc.Scenarios {
			if sc.Confirm == nil {
				missing++
			}
		}
		if missing > 0 {
			if at.confirmGraceExhausted() {
				lastErr = fmt.Errorf("%d scenario(s) missing the REQUIRED confirm{} object (grace window over)", missing)
				at.logWarnf("📐 plan REJECTED: %v", lastErr)
				doc = nil
			} else {
				at.logWarnf("📐 confirm-grace: %d scenario(s) missing confirm{} — accepted during the grace window (CONFIRM_GRACE_SESSIONS).", missing)
			}
		} else {
			at.noteConfirmCompliantSession()
		}
	}

	// 8.4 — stamp the deterministic machine grade beside the model-written one
	// (matched back to the Go-ranked candidate table by price). The card can then
	// show both, so a model's A next to a machine C is visible at a glance.
	if doc != nil && len(machineGrades) > 0 {
		for i := range doc.Levels {
			if g, ok := machineGrades[math.Round(doc.Levels[i].Price*100)/100]; ok && g != "" {
				doc.Levels[i].MachineGrade = g
			}
		}
	}
	// Level-truth wave (2026-08-27) — carry-forward: rows the model reuses from
	// the PREVIOUS version keep that version's machine grade when the current
	// pool has no stamp for the price (carried nPOC / out-of-seat rows).
	if doc != nil {
		at.carryMachineGrades(tradeDate, session, doc)
	}

	// W3 — auto-write the HARD red-news no-trade blackouts into the plan (§80),
	// deduped. The fail-closed NO-TRADE plan already sits out the whole session.
	if doc != nil && len(extraNoTrade) > 0 {
		have := map[string]bool{}
		for _, nt := range doc.NoTrade {
			have[nt] = true
		}
		for _, nt := range extraNoTrade {
			if !have[nt] {
				doc.NoTrade = append(doc.NoTrade, nt)
				have[nt] = true
			}
		}
	}

	// NO-TRADE BAND (2026-09-02) — the MACHINE writes the structured windows.
	// The model's no_trade prose stays exactly as authored (legacy readers and
	// the grader still read it); this is a parallel, additive field that the
	// card evaluates against the clock instead of trusting write-time text.
	//
	// Every window here is resolved from an enforcing source: the session
	// registry for the first-N and lunch bands (one definition, shared with the
	// entry gate and the adherence grader), and t1WindowsFor for red news —
	// literally the windows the gate will refuse entries inside, widening and
	// fail-closed fallback included. Nothing on this list is model-authored.
	if doc != nil {
		if sess, okS := at.sessionRegistry(time.Now()).SessionByName(session); okS && sess != nil {
			wins := kernel.BuildMachineNoTradeWindows(*sess)
			wins = append(wins, kernel.T1NoTradeWindowsFromCT(at.t1WindowsFor(tradeDate, sess))...)
			doc.NoTradeWindows = wins
		}
	}

	lifecycle := "active"
	trigger := session + "_scheduled_read"
	if strings.TrimSpace(triggerOverride) != "" {
		trigger = triggerOverride
	}
	// CLASS 35 — decide the spend by the CLASS the caller passed, before the
	// fail-closed branch relabels the row: a death re-plan / owner re-read that
	// fail-closes still landed a row for a consuming class.
	spendClass, spends := trigger, store.TriggerSpendsReplan(trigger)
	// W-EXEC-TRUTH W5 (CTO 1790194913337 + 1790195988056) — a read that lands
	// the FIRST AI version of a chain is not a re-plan: it spends nothing,
	// whatever class asked for it. "First AI version" = the chain has NO row
	// (nil IS the definition of no plan, and the re-read gate already promises
	// that read is free — recording a spend the gate denied was the class-35
	// lie), or its only row is a MACHINE plan (the Picture no-plan door).
	// Judged on the chain's latest row BEFORE this version is appended; a
	// failed read keeps the spend (fail-closed).
	if spends {
		if prev, perr := at.store.Plan().GetLatestPlanForTraderSession(tradeDate, session, at.id); perr == nil && (prev == nil || store.IsMachinePlan(prev)) {
			spends = false
			over := "an empty chain"
			if prev != nil {
				over = fmt.Sprintf("machine plan v%d", prev.Version)
			}
			// W5 R6: say only what is known here — whether this read lands a
			// plan, a NO-TRADE row or nothing is decided below.
			at.logInfof("🧮 replan budget: %s is the chain's first AI read of %s %s over %s — not a re-plan, nothing spent (class 35)",
				spendClass, tradeDate, session, over)
		}
	}
	if doc == nil {
		// W6-C (2026-08-25) — wake reads are NON-fatal: a failed wake re-read
		// must NOT no-trade a session whose active plan is still alive (live
		// bug 2026-08-25: a seated-OB invalidation wake's re-read timed out and
		// the fail-closed marker killed a healthy ASIA v1). Wake failures keep
		// the active plan and simply skip the wake.
		if !failClosed {
			if triggerOverride == store.TriggerDeathReplan {
				at.logWarnf("🗓️ death re-read failed for %s %s (benign — the dormant plan stands): %v", tradeDate, session, lastErr)
			} else {
				at.logWarnf("🗓️ wake re-read failed for %s %s (benign — active plan kept): %v", tradeDate, session, lastErr)
			}
			at.logPlannerReadLine(session, lastAttempt, rejectHistory, nil, nil, "kept_active")
			return 0, "kept_active", nil
		}
		// P7 — the fail-closed doc still carries the map: levels from the current
		// detector/scorer output (same pipeline), scenarios empty, explicit reason.
		doc = kernel.NoTradePlanDocWithLevels(
			fmt.Sprintf("read failed after retries: %v", lastErr), at.noTradeLevelMap(session))
		lifecycle = "no_trade"
		trigger = "planner_fail_closed"
		bornCheck = nil // W2 A2 — no candidate passed; the NO-TRADE row records none
		at.logErrorf("🚨 PLANNER FAIL-CLOSED %s %s: %v — writing a NO-TRADE plan (never stale, never uncalibrated).", tradeDate, session, lastErr)
		telemetry.IncGateBlock(at.id, "planner_fail_closed")
		// W6 — P0 read-fail / fail-closed alert.
		at.emitAlert("P0", "read-fail", "failclosed:"+tradeDate+":"+session,
			fmt.Sprintf("%s planner fail-closed — NO-TRADE", session), "read failed after retries")
	}

	// W9 — scenario_cap: keep at most N scenarios (default 3 = the schema hardcap,
	// so a no-op unless the owner lowered it). Applied post-parse so the executor
	// prompt reflects the cap.
	if doc != nil {
		if cap := at.scenarioCap(); cap > 0 && len(doc.Scenarios) > cap {
			doc.Scenarios = doc.Scenarios[:cap]
		}
		// W2b (weekly-bias wave) — planner_candle_citations: count scenario
		// prose lines citing the marker phrase "per candles" (the candle-table
		// ground-truth law), log once per read.
		cited := 0
		for _, s := range doc.Scenarios {
			for _, line := range strings.Split(s.Trigger+"\n"+s.Invalid, "\n") {
				if strings.Contains(strings.ToLower(line), "per candles") {
					cited++
					telemetry.IncPlannerCandleCitation(at.id)
				}
			}
		}
		at.logInfof("📊 planner_candle_citations: %d scenario line(s) cite \"per candles\" this read.", cited)
		// W5.1 (weekly-bias wave) — SHADOW confluence: view-only log + reorder
		// counter. Never touches the seated list.
		at.weeklyConfluenceShadow(tradeDate, session, doc.Levels)
	}
	// CLASS 50b (live-bias replay ruling 53498adb) — stamp the dual label on
	// the stored doc: AI bias + the machine tree/regime calls the read actually
	// saw. A LABEL, not a direction — no MUST attaches to either leg.
	doc.BiasLabel = kernel.BiasLabelLine(doc.Bias.Direction,
		kernel.TreeCallWord(facts.Price, facts.PDH, facts.PDL, facts.PDC),
		kernel.RegimeCallWord(facts.Regime))
	identityWarnings := at.stampPlanIdentity(doc, facts.IdentityMap)
	doc.Zones = facts.Zones         // frozen presentation; never model-authored or used by validators
	doc.Structure = facts.Structure // S1 — the STRUCTURE table the read saw; nil stays absent
	// W-FLIP-LINE-SIDE-OF-PRICE (2026-09-17) — the authoring price the
	// death/flip lines were judged against, so the read path can name a line
	// on the wrong side of price without inventing one. Unknown price (legacy
	// facts-less callers): the side-of-price rule was SKIPPED, say so once.
	doc.PriceAtWrite = facts.Price
	if facts.Price <= 0 && (doc.FlipStructured != nil || doc.DeathStructured != nil) {
		at.logWarnf("⚠️ flip/death line side-of-price UNJUDGED: authoring price unknown (facts absent) — the write site never invents a price; the line may sit on the wrong side of price")
	}
	docJSON, _ := json.Marshal(doc)
	version, err := at.store.Plan().AppendPlan(&store.PlanDB{
		CreatedAt:       authoredAt,
		PlanID:          at.store.Plan().ResolvePlanID(tradeDate, session, at.id),
		StrategyID:      at.id,
		TradeDate:       tradeDate,
		Session:         session,
		TriggerReason:   trigger,
		Lifecycle:       lifecycle,
		ModelID:         modelID,
		PromptHash:      promptHash,
		IndicatorsBlock: indicatorsBlock, // W11 — frozen at read time (replay-safe)
		AIConfigHash:    aiConfigHash,
		DarkRegimeCount: at.lastRegimeHealth.DarkCount, // P2
		Degraded:        at.lastRegimeHealth.Degraded,
		Doc:             string(docJSON),
		ReadClockMs:     bornCheck.ReadClockPtr(), // W2 A2 — NULL when unknown
		PublishClockMs:  bornCheck.PublishClockPtr(),
		BornCheck:       bornCheck.JSONPtr(),
	})
	if err != nil {
		at.logErrorf("🗓️ planner: write plan row failed for %s %s: %v", tradeDate, session, err)
		return 0, lifecycle, err
	}
	at.recordPlanIdentity(at.store.Plan().ResolvePlanID(tradeDate, session, at.id), version, identityWarnings, authoredAt)
	researchTrace.Published(at.store.Plan().ResolvePlanID(tradeDate, session, at.id), version, string(docJSON))
	at.logInfof("🗓️ PLAN written %s %s v%d (model %s, lifecycle %s, prompt %s, ai_config %s)", tradeDate, session, version, modelID, lifecycle, promptHash, aiConfigHash)
	at.logPlannerReadLine(session, lastAttempt, rejectHistory, bornCheck.ReadClockPtr(), bornCheck.PublishClockPtr(), lifecycle)
	// W-EXEC-TRUTH W5 (CTO 1790191033566) — the AI read that supersedes a
	// version carrying LIVE Picture scenarios re-appends each of them to the
	// version just written: the same scenario value (same id, same machine
	// record) as the same overlay, idempotent on the opportunity. Evidence
	// survives an AI read; an opportunity the ledger already finished is
	// never re-offered. A NO-TRADE version gets nothing (the Day Plan said no).
	if lifecycle == "active" {
		at.reappendLiveMachineScenarios(at.store.Plan().ResolvePlanID(tradeDate, session, at.id), version, traderNow())
	}
	if spends {
		// CLASS 35 — RECORD the spend now that the row exists (counters record
		// events; they do not infer them from row counts).
		if used, sErr := store.SpendReplan(at.store, at.id, tradeDate, session); sErr != nil {
			at.logErrorf("🧮 replan budget: %s %s v%d landed as %s but the spend FAILED to record: %v — the gate may over-allow by one until the row is fixed (class 35)",
				tradeDate, session, version, spendClass, sErr)
		} else {
			b := store.ReplanBudget{Used: used, Cap: at.replanCapFor(session)}
			at.logInfof("🧮 replan budget: %s spent one — %d/%d used, %d left (%s %s v%d; class 35 recorded-counter)",
				spendClass, b.Used, b.Cap, b.Left(), tradeDate, session, version)
		}
	}
	at.recordPlanWritePrice(facts.Price) // F3 fast-market drift baseline
	// ROOT-FIX part B — fire ONE shadow fast-mode call on the IDENTICAL prompt,
	// AFTER the live read has finished (never a concurrent stream: class 41's
	// concurrency question is still open). Writes nothing; measurement only.
	at.maybeRunShadowAB(session, tradeDate, prompt, maxLevels, scenarioCap, facts, machineLabels, htfLabels, requiredBias)
	// W6 — P1 plan-born/armed alert (active plans only; fail-closed already alerted P0).
	if lifecycle == "active" {
		at.emitAlert("P1", "armed", fmt.Sprintf("planborn:%s:%s:%d", tradeDate, session, version),
			fmt.Sprintf("%s plan v%d armed", session, version), fmt.Sprintf("model %s", modelID))
	}
	return version, lifecycle, nil
}

// resolveSessionPlanCfg resolves the effective planner config for a session:
// strategy-level day_plan values + the per-session override (min_grade). Nil /
// unset fields fall back to the spec defaults, so a default config reproduces the
// prior behavior byte-for-byte (max_levels 8, no min_grade filter, D/4h/1h/15m).
// S3 (2026-09-16): htf_seats nil → the LEGACY seatHTF path (byte-identical to
// the pre-S3 table); a saved value clamps to 0-6 and activates the EFFECTIVE
// promotion. htfMult is the constant kernel.HTFScoreMultiplier (W-KNOB-PRUNE
// 2026-09-18: the knob is gone, 1.0 by owner ruling).
// Pure — unit-tested without an AutoTrader.
func resolveSessionPlanCfg(dp *store.DayPlanConfig, session string) (maxLevels int, htfSeats *int, htfMult float64, minGrade string, timeframes []string) {
	maxLevels = kernel.DefaultMaxLevels
	htfMult = kernel.HTFScoreMultiplier
	timeframes = []string{"D", "4h", "1h", "15m"}
	if dp == nil {
		return maxLevels, nil, htfMult, minGrade, timeframes
	}
	if dp.MaxLevels > 0 {
		maxLevels = dp.MaxLevels
	}
	if dp.HtfSeats != nil {
		v := *dp.HtfSeats
		if v < 0 {
			v = 0
		}
		if v > 6 {
			v = 6
		}
		htfSeats = &v
	}
	if len(dp.PlannerTimeframes) > 0 {
		timeframes = dp.PlannerTimeframes
	}
	if so := dp.SessionOverride(session); so != nil && so.MinGrade != nil {
		minGrade = *so.MinGrade
	}
	return maxLevels, htfSeats, htfMult, minGrade, timeframes
}

// structureSummaryLines fetches one bar request per CONFIGURED planner timeframe
// and returns one honest prompt line per TF: "<tf>: structure read" when bars
// came back, "<tf>: unavailable" otherwise. "D" maps to the provider's "1d"
// interval; every other configured TF is requested verbatim. A nil fetch (no
// bars provider) marks every TF unavailable — the planner is told the read-set
// truth instead of a hardcoded claim that diverges from planner_timeframes (H9).
// consumedLevels (G5) computes which plan levels are CONSUMED right now using
// the same facts evaluator the card reads (EvaluateLevelFacts → StillValid).
func (at *AutoTrader) consumedLevels(bars []market.Kline, levels []kernel.PlanLevel, rule string, now int64) map[float64]bool {
	if len(bars) == 0 {
		return nil
	}
	out := map[float64]bool{}
	for _, l := range levels {
		if kernel.EvaluateLevelFacts(bars, l.Price, kernel.DirAbove, rule, 3, now).StillValid {
			continue
		}
		out[l.Price] = true
	}
	return out
}

// demoteConsumedScenarios (G5) applies the write-time demotion.
func (at *AutoTrader) demoteConsumedScenarios(session string, d *kernel.PlanDoc) int {
	if market.FuturesBarsProvider == nil {
		return 0
	}
	bars := market.FuturesBarsProvider(at.futuresSymbol(), kernel.AISVPBarInterval, kernel.AISVPBarCount)
	if len(bars) == 0 {
		return 0
	}
	consumed := at.consumedLevels(bars, d.Levels, at.acceptanceRuleFor(session), time.Now().UnixMilli())
	return kernel.MarkConsumedScenarios(d, consumed)
}

// structureSummaryLines (G2, regime wave 2026-08-21) — the REAL machine
// structure detector replaces the old "read/unavailable" placeholder: per-TF
// trend + newest swing + the latest BOS/CHoCH/MSS/SWEEP event, computed from
// the same 1m cache the executor reads (kernel.StructureSnapshot).
func structureSummaryLines(fetch func(tf string, count int) []market.Kline, timeframes []string) []string {
	lines := make([]string, 0, len(timeframes)+1)
	var bars1m []market.Kline
	if fetch != nil {
		bars1m = fetch(kernel.AISVPBarInterval, kernel.AISVPBarCount)
	}
	snap := kernel.StructureSnapshot(bars1m, time.Now().UnixMilli())
	for _, tf := range timeframes {
		st, ok := snap[tf]
		if !ok {
			lines = append(lines, tf+": unavailable")
			continue
		}
		label := st.Trend
		if st.Swing != nil {
			label += fmt.Sprintf(" (%s %.2f @%s)", st.Swing.Kind, st.Swing.Price, kernel.ClockCT(time.UnixMilli(st.Swing.TimeMs)))
		}
		lines = append(lines, tf+": "+label)
	}
	var lastEv *kernel.StructureEvent
	var lastTF string
	for _, tf := range kernel.StructureTFs {
		st, ok := snap[tf]
		if !ok {
			continue
		}
		for i := range st.LastEvents {
			if lastEv == nil || st.LastEvents[i].TimeMs > lastEv.TimeMs {
				e := st.LastEvents[i]
				lastEv = &e
				lastTF = tf
			}
		}
	}
	if lastEv != nil {
		lines = append(lines, fmt.Sprintf("last event: %s-%s %s @%s", lastEv.Type, lastEv.Dir, lastTF, kernel.ClockCT(time.UnixMilli(lastEv.TimeMs))))
	}
	return lines
}

// assemblePlannerInput builds the input package from stored + cached data (the
// 16:55 read builds entirely from stored data). Digests + owner note arrive with
// P3.6; regime daily/1h fields degrade to n/a until those TFs are fetched. It
// HONORS the day_plan config (max_levels, per-session min_grade, timeframes) —
// edits apply at the NEXT read (never mid-plan).
func (at *AutoTrader) assemblePlannerInput(session, tradeDate string) kernel.PlannerInput {
	return at.assemblePlannerInputWithCtx(session, tradeDate, "", nil)
}

// assemblePlannerInputWithCtx (P0.4-G, 2026-08-25) is assemblePlannerInput with
// the prior-plan context for re-plans: the dead plan's killer line and its
// levels (map continuity).
func (at *AutoTrader) assemblePlannerInputWithCtx(session, tradeDate, priorKiller string, priorLevels []string) kernel.PlannerInput {
	symbol := at.futuresSymbol()
	now := time.Now()
	researchID := uuid.NewString()
	reg := at.sessionRegistry(now) // W8

	var dp *store.DayPlanConfig
	if at.config.StrategyConfig != nil {
		dp = at.config.StrategyConfig.DayPlan
	}
	maxLevels, htfSeats, htfMult, minGrade, timeframes := resolveSessionPlanCfg(dp, session)

	var bars []market.Kline
	if market.FuturesBarsProvider != nil {
		bars = market.FuturesBarsProvider(symbol, kernel.AISVPBarInterval, kernel.AISVPBarCount)
	}
	var extra []kernel.DetectedLevel
	if kernel.NakedPOCProvider != nil {
		extra = kernel.NakedPOCProvider(symbol)
	}
	// G2/G3 (2026-08-24) — per-TF swing/zone detection on the CONFIGURED
	// planner timeframes: real 1h/4h support/resistance now enters the
	// candidate pool (before this, every detector ran on the 1m slice only).
	// G2.1 — one observability line per read so a missing-zone question is
	// answered with facts, not theories.
	zoneSeries := map[string][]market.Kline{"1m": bars, "5m": kernel.AggregateBars(bars, 5*60_000), "15m": kernel.AggregateBars(bars, 15*60_000)}
	var htfLevels []kernel.DetectedLevel
	if market.FuturesBarsProvider != nil {
		htfLevels = kernel.DetectHTFLevels(func(tf string, count int) []market.Kline {
			series := market.FuturesBarsProvider(symbol, tf, count)
			zoneSeries[tf] = series
			return series
		}, timeframes, symbol, now)
		if len(htfLevels) > 0 {
			counts := map[string]int{}
			for _, l := range htfLevels {
				counts[l.Label]++
			}
			at.logInfof("🗺️ G2 HTF detection @%s: %d level(s) %v", session, len(htfLevels), counts)
		} else {
			at.logInfof("🗺️ G2 HTF detection @%s: 0 levels — no swings/zones found on %v", session, timeframes)
		}
		extra = append(extra, htfLevels...)
	}
	scored, pool, price, dATR, researchRaw := kernel.AssembleResearchLevels(at.id, bars, reg, symbol, maxLevels, htfSeats, htfMult, now, at.proximityFilterATR(), minGrade, extra...)
	// 1h wave (2026-08-25) — the ranked table's HTF seats guarantee an in-band
	// 1h S/D zone when one exists. W-KNOB-PRUNE (2026-09-18): unconditional —
	// the seat_1h_zone switch (default ON, never stored) is gone.
	scored = kernel.Seat1HZone(scored, maxLevels)

	// G2.2 (2026-08-24) — the nearest in-band HTF ZONES become their own prompt
	// section: the top-8 seat race hides them, but the model must know where the
	// 1h/4h bases are.
	var htfZoneScored []kernel.ScoredLevel
	var htfZonesFull []kernel.ScoredLevel // S1-wave A3 — uncapped stamp universe
	if len(htfLevels) > 0 && price > 0 && dATR > 0 {
		zones := make([]kernel.DetectedLevel, 0, len(htfLevels))
		for _, l := range htfLevels {
			switch l.Kind {
			case kernel.KindSupply, kernel.KindDemand, kernel.KindFVG, kernel.KindOB:
				zones = append(zones, l)
			}
		}
		if len(zones) > 0 {
			// ScoreLevels filters to the ±proximity band and returns nearest-first.
			zs := kernel.ScoreLevels(zones, price, dATR, nil, 4, at.proximityFilterATR())
			// 1h wave (2026-08-25) — the cap-4 MUST keep a 1h S/D zone when one
			// is in band, so the prompt's conditional 1h mandate has data to
			// point at. Unconditional since W-KNOB-PRUNE (switch removed).
			zs = kernel.Seat1HZone(zs, 4)
			htfZoneScored = zs
			// S1-wave A3 (2026-08-29) — the FULL in-band graded HTF-zone
			// universe (no cap) rides along for the write-site stamp: the
			// model reads the whole key-levels block and may write zones the
			// cap-4 section hid (the 13 Demand·1h escapes). Prompt rendering
			// still uses the cap-4 HTFZones.
			htfZonesFull = kernel.ScoreLevels(zones, price, dATR, nil, len(zones), at.proximityFilterATR())
		}
	}

	// P3.6-C — STICKY OWNER LEVELS: always seated, tagged 👤, persisted across
	// sessions. Prepended so they lead the ranked table.
	if owned, err := at.store.OwnerLevel().ListActiveForUser(at.ownerUserID(), symbol); err == nil && len(owned) > 0 {
		ownerScored := make([]kernel.ScoredLevel, 0, len(owned))
		for _, o := range owned {
			label := "👤 " + o.Label
			if o.Note != "" {
				label += " (" + o.Note + ")"
			}
			ownerScored = append(ownerScored, kernel.ScoredLevel{
				DetectedLevel: kernel.DetectedLevel{Kind: kernel.KindOwner, Price: o.Price, Lo: o.Price, Hi: o.Price, Label: label, OriginDate: "owner", HTF: true, Info: o.ScenarioTag},
				Grade:         "A", Fresh: "owner", Distance: o.Price - price,
			})
		}
		for i := range ownerScored {
			ownerScored[i].Research = &kernel.LevelScoreCapture{Family: "owner", Grade: researchsnapshot.Value(ownerScored[i].Grade), Overrides: []string{"owner level prepended; grade supplied by owner rule; score not computed"}}
			researchRaw = append(researchRaw, ownerScored[i].DetectedLevel)
		}
		scored = append(ownerScored, scored...)
	}

	// Per-session min_grade filter (owner levels grade A → always survive).
	// R2 4.6/4.7 (2026-08-25) — the filter now runs inside
	// AssembleScoredLevelsMinGrade on a 2× pool so the cut REFILLS seats
	// from the same side instead of thinning the table; this final pass
	// keeps the owner-prepended levels above the floor.
	scored = kernel.FilterLevelsByMinGrade(scored, minGrade)

	// D2 CHOICE (a) — READ THE STORE. WHY: this tape is aggregated into the
	// "8 daily session candles" table, which needs ~11,040 open 1m intervals.
	// The ring ceiling is 2,500 bars (41.7 h), so the 8-row table rendered 2 or
	// 3 rows in 55 of 55 stored prompts and NEVER 8. The bars table holds MNQ 1m
	// back 21 days (measured 2026-09-09: 20,788 rows from 2026-08-19), bounded
	// by the 1m retention of 90 days. The ring still owns the live tail — the
	// store only extends it backwards (barsWithStoreDepthFrom rule 3).
	//
	// IT IS READ HERE, ABOVE THE REGIME BLOCK, because the realized-vol
	// baseline is now served from THIS tape (owner condition (b), below) as
	// well as by the candle tables and the weekly thin-history count. One read,
	// three consumers, so they can never disagree about the tape.
	//
	// THREE CONSUMERS, ALL NAMED (the second and third were undisclosed until
	// review, 2026-09-09):
	//   1. kernel.BuildPlannerCandleTablesAt — the candle tables (D1);
	//   2. ResolveRVBaselineTape             — the realized-vol baseline;
	//   3. kernel.CompletedWeekCount         — the WEEKLY line's "(thin history
	//      %dw)" clause. MEASURED against the live store 2026-09-09: 2,500 1m
	//      bars (43.6 h) → 0 completed weeks; 12,000 (309.0 h) → 2. The count
	//      gets MORE accurate, but it does move, and it renders in the prompt.
	bars1m := at.barsWithStoreDepth(symbol, "1m", plannerCandleTapeBars, now)

	var daily, hour1, min5, ring5m []market.Kline
	if market.FuturesBarsProvider != nil {
		daily = market.FuturesBarsProvider(symbol, "1d", 300)
		hour1 = market.FuturesBarsProvider(symbol, "1h", 300)
		min5 = market.FuturesBarsProvider(symbol, "5m", 300)                           // recent (~1 day) → RV recent
		ring5m = market.FuturesBarsProvider(symbol, "5m", rvBaselineFallback5mBarsAsk) // FALLBACK ONLY (see below)
	}
	// W10 — supply the realized-vol baseline (was never fed → RV stuck "warming").
	// Same 5m estimator as the recent value; VIX stays honest n/a (no feed).
	//
	// OWNER CONDITION (b), 2026-09-09 — THE 5m ASK IS SERVED FROM THE
	// REHYDRATED 1m TAIL. The estimator (kernel.RVBaselineFrom5mDays) is
	// byte-for-byte the same RULE; only the INPUT is corrected, from a 5m ring
	// that could reach ~7 complete session-days to the 1m tape's ~9. ring5m
	// remains as the FALLBACK so a thin 1m tape can never turn the baseline
	// OFF — see ResolveRVBaselineTape (trader/regime_input_window.go) for the
	// measured numbers and the reason stored 5m rows are refused.
	rvTape := ResolveRVBaselineTape(bars1m, ring5m, rvBaselineMaxDays)
	rvBaseline, rvBaselineDays := rvTape.Baseline, rvTape.Days
	at.logInfof("📈 regime input: %s (rule unchanged; owner ruling 2026-09-09)", rvTape.Line())
	// W11b — supply overnight-gap inputs (prior close + session open, ×ATR) from the
	// daily bars (was never fed → the gap field stayed inert).
	priorClose, sessionOpen := kernel.PriorCloseSessionOpen(daily)
	regime := kernel.ComputeRegime(kernel.RegimeInputs{
		Price: price, DailyBars: daily, Hour1Bars: hour1, Min5Bars: min5,
		RVBaseline: rvBaseline, RVBaselineDays: rvBaselineDays,
		PriorClose: priorClose, SessionOpen: sessionOpen,
	})

	var calEvents []kernel.PlannerCalendarEvent
	if slice, err := at.store.Calendar().GetSlice(tradeDate); err == nil && slice != nil {
		var evs []calendar.Event
		if json.Unmarshal([]byte(slice.EventsJSON), &evs) == nil {
			loc := kernel.CTLocation()
			if loc == nil {
				loc = time.UTC
			}
			for _, e := range calendar.EventsForSession(evs, session) {
				calEvents = append(calEvents, kernel.PlannerCalendarEvent{
					TimeCT:   e.Time.In(loc).Format("15:04"),
					Currency: e.Currency,
					Title:    e.Title,
					Impact:   string(e.Impact),
				})
			}
		}
	}

	warming := ""
	if n, _ := at.store.SessionProfile().Count(symbol); n < 10 {
		warming = fmt.Sprintf("session-profile store warming (%d/10)", n)
	}

	// P3.6-A — tapered week digest chain (current-date sessions + 3 full dailies +
	// days 4-7 one-liners).
	// W16/R4 — digests are per-TRADER: the text reports this trader's entries and
	// realized P&L, so another trader's day must never seed this planner read.
	sessionDigests, _ := at.store.Digest().SessionDigests(at.id, symbol, tradeDate)
	dailies, _ := at.store.Digest().RecentDailies(at.id, symbol, 7)
	digestChain := kernel.BuildDigestChain(sessionDigests, dailies)

	// H9 — the structure summary must only claim what was actually fetched. Each
	// CONFIGURED timeframe gets one honest line: "4h: structure read" when bars
	// came back, "4h: unavailable" when the provider is down or the TF is dark.
	// Before this the fetch was hardcoded 1d/1h/5m while the lines asserted the
	// configured set — the planner was told it read structure it never saw.
	var structureFetch func(tf string, count int) []market.Kline
	if market.FuturesBarsProvider != nil {
		structureFetch = func(tf string, count int) []market.Kline {
			return market.FuturesBarsProvider(symbol, tf, count)
		}
	}
	structure := structureSummaryLines(structureFetch, timeframes)

	// W11 — INDICATOR MIRROR: render the executor's per-TF indicator state + the
	// ai_config fingerprint (both frozen onto the plan row downstream).
	indicatorsBlock, aiConfigHash := at.renderIndicatorMirror(symbol)

	// P2 — DARK REGIME: name what the planner could NOT see, alert on it, and carry
	// the verdict to the write site so it lands on the plan row.
	at.lastRegimeHealth = kernel.AssessRegime(regime, 0)
	if at.lastRegimeHealth.DarkCount > 0 {
		body := at.lastRegimeHealth.AlertBody()
		level := "P1"
		if at.lastRegimeHealth.Degraded {
			level = "P0" // a half-blind plan is a decision-quality event, not FYI
		}
		at.logWarnf("🌑 dark regime at the %s read: %s", session, body)
		at.emitAlert(level, "regime-dark",
			fmt.Sprintf("regime-dark:%s:%s", tradeDate, session),
			fmt.Sprintf("%s plan: %d/%d regime fields dark", session, at.lastRegimeHealth.DarkCount, kernel.RegimeFieldCount),
			body)
	}

	// G5 (regime wave 2026-08-21) — consumed levels at read time, listed so the
	// planner works around them.
	var consumedLines []string
	if len(bars) > 0 {
		lvls := make([]kernel.PlanLevel, len(scored))
		for i, s := range scored {
			lvls[i] = kernel.PlanLevel{Price: s.Price, Label: s.Label}
		}
		consumed := at.consumedLevels(bars, lvls, at.acceptanceRuleFor(session), now.UnixMilli())
		for _, s := range scored {
			if consumed[s.Price] {
				consumedLines = append(consumedLines, fmt.Sprintf("%.2f %s", s.Price, s.Label))
			}
		}
	}

	// S-dispatch (2026-08-27) — bias facts for the BIAS-TREE, with the
	// universe day-anchors stamped on (post-roll the seated table drops
	// PDH/PDL — the 17:46/19:02 ASIA reads rendered "no PDH/PDL anchor").
	bcFacts := kernel.ComputeBiasContext(bars, scored, now)
	kernel.ApplyUniverseDayAnchors(&bcFacts, kernel.ExtractMultiDayLevels(bars, reg, now))

	// W2b (weekly-bias wave) — PLANNER EYES: the raw candle tables (12×15m ·
	// 12×1h · 8×4h · 8×daily) built from the 1m slice via kernel.AggregateBars.
	// Candles are ground truth for structure. Knob PLANNER_CANDLES (default on).
	var candleTables string
	// bars1m is read ABOVE, beside the regime block — one store-deepened tape,
	// three consumers (candle tables · RV baseline · CompletedWeekCount).
	if kernel.PlannerCandlesEnabled() {
		candleTables = kernel.BuildPlannerCandleTablesAt(bars1m, plannerCandleTapeBars, now)
	}
	// VOID PARITY (2026-09-02) — resolve the void scope ONCE. The prompt and the
	// persisted read-facts row must carry the identical list; computing it twice
	// would let them drift, which is the exact class this wave closes.
	voidScope := kernel.ResolveVoidScope(symbol, now)
	voidScopeLevels := kernel.ComputeVoidBreakdownLevels(scored, voidScope, now.UnixMilli())
	voidScopeATR := plannerATR5m(symbol)
	// DISPLACEMENT FEEDS FORWARD (owner ruling 2026-09-03) — the waterfall
	// floor's per-level measurement, from the SAME scope the void list uses so
	// the two cannot disagree about what the tape shows.
	levelDisplacements := kernel.ComputeLevelDisplacements(scored, voidScope, now.UnixMilli())
	if at.store != nil {
		detPlanID := at.store.Plan().ResolvePlanID(tradeDate, session, at.id)
		detVersion := 0
		// W1 item 1 — the in-force plan's scenarios, reduced to the one price a
		// level can be compared against. The Doc was already fetched here and
		// discarded; only .Version was read. Deriving the anchors at THIS point
		// is what lets the recorder stamp the link at write instead of a reader
		// price-matching plans.doc afterwards.
		var detAnchors []store.ScenarioAnchor
		detUnanchorable := 0
		if latest, lerr := at.store.Plan().GetLatestPlanForTraderSession(tradeDate, session, at.id); lerr == nil && latest != nil {
			detVersion = latest.Version
			if doc, derr := kernel.ParsePlanDoc(latest.Doc); derr == nil && doc != nil {
				detAnchors, detUnanchorable = scenarioAnchorsFrom(doc)
			}
		}
		if detUnanchorable > 0 {
			// A24: counted, never dropped silently. A scenario with no confirm
			// ref and no armed entry cannot be compared against a level at all.
			at.logWarnf("🎫 episodes: %d scenario(s) unanchorable (no confirm ref, no armed entry) — their touches record a NULL link", detUnanchorable)
		}
		// pool is []ScoredLevel; the recorder classifies raw DetectedLevels
		// against the seated set itself and must not be handed a second scoring.
		detAll := make([]kernel.DetectedLevel, 0, len(pool))
		for _, c := range pool {
			detAll = append(detAll, c.DetectedLevel)
		}
		at.recordDetectorOutputs(symbol, detPlanID, session, detVersion,
			detAll, scored, price, dATR, at.proximityFilterATR(), maxLevels, now, detAnchors, researchID)
	}

	// 1B WIRING (owner ruling 2026-09-03) — the detector's ONE production call
	// site. detector_record.go's header has claimed "THE PRODUCTION CALL PATH"
	// since 89aeb8be while NOTHING called it: every boot, including the running
	// 4d846e26, printed "touch_outcomes=0 · candidate_pool=0". It runs HERE,
	// beside the void scope, because the hook re-resolves that identical scope
	// (detector_record.go:38) — so the detector judges the SAME tape the prompt
	// and the validator read, which is what 1B's design required.
	//
	// planVersion is the version IN FORCE at read time, NOT the one this read is
	// about to author: a read can fail and author nothing, and A24 forbids
	// recording a version number that may never exist.
	//
	// A10: the hook logs and returns on every failure; it cannot fail this read.
	// W3 (weekly-bias wave) — the Sunday weekly-bias context line (≤3 lines;
	// "WEEKLY: none" when no doc — fail-open, nothing else changes).
	weeklyDoc := at.weeklyDocCached(now)
	nw := 0
	if weeklyDoc != nil && weeklyDoc.ThinHistory {
		nw = kernel.CompletedWeekCount(bars1m, now)
	}
	weeklyCtx := kernel.WeeklyContextLine(weeklyDoc, nw)

	// S1 (2026-09-16) — the STRUCTURE table: one call, knob-gated, logged once
	// per read. The pool is the uncapped HTF zone universe this read scored.
	readContract, _ := at.currentContract(symbol)
	structureMap := at.structureMapForRead(symbol, readContract, htfZonesFull, price, now)
	if en, _ := at.structureMapEnabled(); en {
		at.logInfof("%s", kernel.StructureLogLine(structureMap, session))
	}

	in := kernel.PlannerInput{
		TradeDate:        tradeDate,
		Session:          session,
		Now:              now, // P0 timezone — the planner's labelled CT clock
		ReadKind:         session + " scheduled read (stored+cached data)",
		Price:            price,
		DATR:             dATR,
		ATR5m:            kernel.StaleConfirmATR5m(bars),
		GeometryRefIDs:   at.dayPlanCfg().GeometryRefIDsEnabled(), // W-GEOMETRY-REFUSAL (b1)
		Regime:           regime,
		Levels:           scored,
		Pool:             pool,
		HTFZones:         htfZoneScored,
		HTFZonesFull:     htfZonesFull,
		StructureSummary: structure,
		Structure:        structureMap, // S1 — nil unless day_plan.structure_map is on
		ConsumedLevels:   consumedLines,
		// CLASS 45 E2/E3 (2026-09-02) — feed forward what the enforcers already
		// know. The void verdict is the VALIDATOR'S OWN predicate reached through
		// a level-oriented entry point (never a second implementation), and the
		// floor is the composer's own resolver. Empty/zero → the prompt renders
		// nothing, so a cold cache degrades to today's behaviour.
		VoidBreakdownLevels: voidScopeLevels,
		StopFloorATR5m:      voidScopeATR,
		LevelDisplacements:  levelDisplacements,
		StopFloorMult:       kernel.MinSLATRMult(),
		// Level-truth wave b2 (2026-08-27): the machine's fresh-gap candidate
		// list — the ONLY gaps the planner may author fvg_entry from.
		FreshFVGs:    kernel.FreshFvgCandidates(bars, symbol, now),
		Calendar:     calEvents,
		T1Currencies: at.t1Currencies(),
		// W-WRITE-TIME-FEASIBILITY (2026-09-18): the prompt renders the
		// arm-disabled-at-write rule only when the knob is ON.
		WriteFeasibilityOn: at.writeTimeFeasibilityOn(),
		// W-EXEC-TRUTH W3 (h): the ENTRY POLICY sentence follows the SAME
		// resolved knobs the parse stamps and judges with (plannerAuthoringOpts).
		EntryPolicyDefault: entryPolicyForPrompt(at.dayPlanCfg()),
		ZoneMaxPts:         zoneMaxPtsForPrompt(at.dayPlanCfg()),
		MinHoldMin:         at.plannerAuthoringOpts().MinHoldMin,
		DigestChain:        digestChain,
		Warming:            warming,
		IndicatorsBlock:    indicatorsBlock,
		AIConfigHash:       aiConfigHash,
		// ADDENDUM (2) — bias-context facts line (VWAP/PDC/value area/magnet/
		// liquidity). Facts only; the AI judges direction.
		// S-dispatch (2026-08-27) — the BIAS-TREE facts must carry the
		// prior-day anchors at ANY distance: post-roll the seated table can
		// drop PDH/PDL (out of the proximity band) and the tree rendered
		// "no PDH/PDL anchor". Stamp the universe anchors onto the facts.
		BiasCtx:      bcFacts.Line(),
		BiasCtxFacts: bcFacts,
		// H4/H5 — the prompt asks for EXACTLY what validation accepts: the
		// resolved max_levels / scenario_cap (never a hardcoded 8/3 the owner
		// cannot raise).
		MaxLevels:       maxLevels,
		ScenarioCap:     at.scenarioCap(),
		PriorPlanKiller: priorKiller,
		PriorPlanLevels: priorLevels,
		// W2b/W3 (weekly-bias wave) — the raw candle tables (planner eyes) and
		// the weekly-bias context line (soft law, fail-open).
		CandleTables: candleTables,
		WeeklyCtx:    weeklyCtx,
	}
	// VOID PARITY D3 (2026-09-02) — record what the model is TOLD on EVERY read.
	// Before this a rendered prompt survived only when the read FAILED, so a
	// working fix erased its own evidence. Best-effort: telemetry never fails a
	// read (A10).
	zoneView := kernel.BuildLevelZones(researchRaw, price, in.ATR5m, kernel.LevelZoneInputs(researchRaw, zoneSeries, now), kernel.ResolveZoneOptions(maxLevels), now)
	in.Zones = &zoneView
	at.logInfof("%s", zoneView.Render())
	in.ResearchSnapshotID = researchID
	recordResearchCandidates(in.ResearchSnapshotID, symbol, researchRaw, scored, now)
	at.persistReadFacts(in, voidScope, voidScopeLevels, voidScopeATR, now)
	return in
}

// persistReadFacts writes one planner_read_facts row per read. Loud on failure,
// never fatal.
func (at *AutoTrader) persistReadFacts(in kernel.PlannerInput, scope kernel.VoidScope, void []kernel.VoidBreakdownLevel, atr5m float64, now time.Time) {
	if at == nil || at.store == nil {
		return
	}
	row := buildReadFactRow(at.id, in, scope, void, atr5m, now)
	if err := at.store.PlannerReadFacts().SaveReadFact(row); err != nil {
		at.logWarnf("📓 read-facts write failed: %v", err)
		return
	}
	at.logInfof("📓 read facts: void=%d · floor=%.1f pts (%.1f×ATR5m %.2f) · scope=%s×%d since=%d (cap %d) · horizon %s",
		row.VoidCount, row.StopFloorPts, row.StopFloorMlt, row.ATR5m, row.ScopeIntv, row.ScopeBars,
		row.ScopeSinceMs, store.PlannerReadFactsCap, scope.Horizon.Line())
}

// maybeWriteDigests writes the 3-line session digest at each enabled session's
// close and the daily roll-up at the trade-date close (16:00 CT). Idempotent
// (SaveIfAbsent) → restart-safe. GATED on day_plan → dormant by default.
func (at *AutoTrader) maybeWriteDigests() {
	if !at.dayPlanEnabled() || at.store == nil {
		return
	}
	now := time.Now()
	reg := at.sessionRegistry(now) // W8
	tradeDate := plannerTradeDateCT(now)
	symbol := at.futuresSymbol()
	sinceMs := kernel.CMESessionDayStart(now).UnixMilli()
	// W16/R4 — scope the P&L to the ACTIVE ACCOUNT too, matching every other
	// session-day activity call (loop.go, auto_trader_session.go). Without it a
	// two-account trader's digest mixed both accounts' numbers.
	pnl, entries, _ := at.store.Position().GetSessionDayActivity(at.id, sinceMs, at.currentAccountName())

	for i := range reg.Sessions {
		s := &reg.Sessions[i]
		if runnable, _ := at.sessionRunnable(s); !runnable {
			continue
		}
		// Wrap-aware "is this session closed right now": the old test was
		// ctMinutesNow(now) >= end — TRUE for the whole rest of the DAY once the
		// end minute passed, so ASIA (end 02:00) read as "closed" at 21:00 CT
		// while its evening leg was RUNNING, and a mid-session digest with
		// mid-session P&L was written for the just-started instance (class 2).
		// Not-in-window is the correct predicate: the most recent instance has
		// ended, and sessionChainDate keys the digest to THAT instance's date.
		if _, ok := hhmmToMin(s.WindowEndCT); !ok {
			continue // malformed registry times — never digest on garbage
		}
		if s.InWindow(now) {
			continue // session running — not closed yet
		}
		// P0-B — a session digest carries the SESSION INSTANCE's date, so the
		// next read of the SAME session picks it up (ASIA closes 02:00 CT, after
		// the midnight roll; its digest must land on the day the plan was keyed).
		sessTradeDate := sessionChainDate(s, now)
		text := kernel.FormatSessionDigest(s.Name, sessTradeDate, "", entries, pnl)
		if wrote, _ := at.store.Digest().SaveIfAbsent(&store.DigestDB{
			TraderID: at.id,
			Symbol:   symbol, TradeDate: sessTradeDate, Session: s.Name, Kind: "session", Text: text, CreatedAt: now.UnixMilli(),
		}); wrote {
			at.logInfof("📓 session digest written %s %s.", sessTradeDate, s.Name)
		}
	}

	// W1 — daily roll-up in the [15:00,16:00) RTH-close→break window, where
	// tradeDate + the P&L window are still the CLOSING day's (they roll at 17:00).
	// Reachable Mon–Fri; idempotent (SaveIfAbsent). W9 — gated on evening_digest
	// (default true; the end-of-day roll-up IS the "evening digest" toggle).
	if inDailyRollWindow(now) && at.eveningDigestEnabled() {
		sessions, _ := at.store.Digest().SessionDigests(at.id, symbol, tradeDate)
		text := kernel.FormatDailyDigest(tradeDate, "", len(sessions), entries, pnl)
		if ll := at.learningDigestLine(); ll != "" {
			text += "\n" + ll
		}
		if el := telemetry.ErrorDigestLine(at.id); el != "" {
			text += "\n" + el
		}
		if el := telemetry.ErrorDigestLine(at.id); el != "" {
			text += "\n" + el
		}
		if wrote, _ := at.store.Digest().SaveIfAbsent(&store.DigestDB{
			TraderID: at.id,
			Symbol:   symbol, TradeDate: tradeDate, Kind: "daily", Text: text, CreatedAt: now.UnixMilli(),
		}); wrote {
			at.logInfof("📓 daily digest written %s.", tradeDate)
		}
	}
}

// learningDigestLine renders the learning-loop line (avg MAE/MFE + adherence
// grades, linkable to plan versions) from the last graded closed positions.
// P0-cleanup (2026-08-19) — MAE/MFE + grades now reach the digest.
func (at *AutoTrader) learningDigestLine() string {
	if at.store == nil {
		return ""
	}
	rows, err := at.store.Position().GetGradedClosedPositions(at.id, 20)
	if err != nil || len(rows) == 0 {
		return ""
	}
	trades := make([]kernel.LearningTrade, 0, len(rows))
	for _, p := range rows {
		lt := kernel.LearningTrade{Grade: p.AdherenceGrade, PlanVersion: p.PlanVersion}
		if p.MAE != nil && p.MFE != nil { // E4 — NULL means never computed
			lt.MAE, lt.MFE, lt.Measured = *p.MAE, *p.MFE, true
		}
		trades = append(trades, lt)
	}
	return kernel.LearningLine(trades)
}

// storedReplanCap resolves the re-plan cap for (plan owner, session) from the
// LIVE stored strategy config — the same resolver GET /api/plan/today uses, so
// the executor prompt and the owner's card can never narrate different numbers.
//
// It reads the store rather than a cached struct on purpose: a mid-session cap
// edit is exactly the case that exposed the divergence. Note that plans.strategy_id
// holds the TRADER id (the column is misnamed), hence the trader→strategy hop.
// Every failure path falls back to DayPlanConfig's shipped default, which is what
// the literal it replaces assumed anyway.
func storedReplanCap(st *store.Store, traderID, session string) int {
	var dp *store.DayPlanConfig // nil → ReplanCapFor returns the shipped default
	if st == nil || traderID == "" {
		return dp.ReplanCapFor(session)
	}
	tr, err := st.Trader().GetByID(traderID)
	if err != nil || tr == nil || tr.StrategyID == "" {
		return dp.ReplanCapFor(session)
	}
	var strat store.Strategy
	if err := st.GormDB().Where("id = ?", tr.StrategyID).First(&strat).Error; err != nil {
		return dp.ReplanCapFor(session)
	}
	var cfg store.StrategyConfig
	if json.Unmarshal([]byte(strat.Config), &cfg) != nil {
		return dp.ReplanCapFor(session)
	}
	return cfg.DayPlan.ReplanCapFor(session)
}

// installActivePlanProvider registers THIS trader's active-plan provider with
// the kernel (P3.4). P0-A (2026-08-18): this used to write a PROCESS-GLOBAL
// singleton closing over whichever trader reached the sync.Once first — so a
// second day-plan trader's executor read the first trader's plan, session
// enablement and budget. Now the provider is registered per-traderID and its
// plan lookup is trader-scoped: a trader can NEVER receive another trader's
// plan row. no_trade/died plans and off-session return nil → the executor
// prompt is unchanged.
//
// CLEANUP BATCH 2, B3 (2026-09-11): the wall clock is a SEAM now —
// installActivePlanProviderAt takes the clock, this entry point hands it
// time.Now (clock-seams.list). And the nil path is no longer silent: a
// refused arm logs why, but a MISSING PLAN logged nothing, which is what
// sent a lane to inspect the split logic — the one thing that was not
// wrong. The provider is called every executor cycle, so the WARN fires on
// the CHANGE of reason (and once more when a plan reappears), never per call.
func installActivePlanProvider(at *AutoTrader, st *store.Store) {
	installActivePlanProviderAt(at, st, time.Now)
}

// planProviderNilReason is the reason the provider last returned nil for a
// trader ("" = it last returned a plan). Keyed per trader so two day-plan
// traders never share a state (P0-A).
var (
	planProviderNilMu     sync.Mutex
	planProviderNilReason = map[string]string{}
)

// flipDirectionNoted remembers which stored plan versions have already been
// named as inverted, keyed per trader + plan + version, so the line prints
// ONCE per version rather than twice per tick for the plan's whole life
// (the notePlanProviderNil idiom: record, never re-derive per cycle).
var (
	flipDirectionNotedMu sync.Mutex
	flipDirectionNoted   = map[string]bool{}
)

// noteFlipDirectionInverted (W-FLIP-DIRECTION, 2026-09-17) names a stored
// plan whose flip points the wrong way for its bias. The write site now
// REJECTS that shape, but a plan written BEFORE it learned direction never
// passes the write site again — the only places it is seen are the two
// read-path evaluators, describeActivePlanDeath (active plans) and
// describeDormantCleared (dormant plans), which both call this first. WARN
// only, once per plan version: the evaluation itself is unchanged (LONDON v3
// 2026-09-17: short + flip{below → long} could never flip on the rally).
func noteFlipDirectionInverted(at *AutoTrader, row *store.PlanDB, doc *kernel.PlanDoc, site string) {
	err := kernel.FlipDirectionContradiction(doc.Bias.Direction, doc.FlipStructured)
	if err == nil {
		return
	}
	key := fmt.Sprintf("%s|%s|v%d", at.id, row.PlanID, row.Version)
	flipDirectionNotedMu.Lock()
	seen := flipDirectionNoted[key]
	flipDirectionNoted[key] = true
	flipDirectionNotedMu.Unlock()
	if seen {
		return
	}
	at.logWarnf("flip_direction_inverted plan=%s v%d (%s) %v — this flip can never fire on the move it is meant to catch (written before W-FLIP-DIRECTION); printed once per plan version", row.PlanID, row.Version, site, err)
}

var (
	linesBeyondPriceMu    sync.Mutex
	linesBeyondPriceNoted = map[string]bool{}
)

// authoringPriceWindowMs bounds the tape fallback in authoringPriceFor: a
// close older than this before the row's created_at is not the close the
// write site judged against, so the price stays UNKNOWN rather than invented.
const authoringPriceWindowMs = 10 * 60_000

// authoringPriceFor returns the price the plan's death/flip lines were judged
// against at write: the stamped doc.price_at_write when present, else the
// last CLOSED bar of the tape at or before the row's created_at (the same
// value AssembleResearchLevels handed the write site as facts.Price), and 0
// when neither is known. Never invents a price.
func authoringPriceFor(doc *kernel.PlanDoc, createdAt time.Time, bars []market.Kline) float64 {
	if doc != nil && doc.PriceAtWrite > 0 {
		return doc.PriceAtWrite
	}
	if createdAt.IsZero() {
		return 0
	}
	atMs := createdAt.UnixMilli()
	var best market.Kline
	for _, b := range bars {
		if b.CloseTime >= atMs || b.Close <= 0 {
			continue
		}
		if b.CloseTime > best.CloseTime {
			best = b
		}
	}
	if best.CloseTime == 0 || atMs-best.CloseTime > authoringPriceWindowMs {
		return 0
	}
	return best.Close
}

// noteLinesBeyondPrice (W-FLIP-LINE-SIDE-OF-PRICE, 2026-09-17) is the sibling
// of noteFlipDirectionInverted: it names a stored plan whose flip or death
// line already sat beyond price on its own side when it was written — a line
// the touch gate can never fire. The write site now REJECTS that shape when
// the authoring price is known; plans written before it (and the ASIA v2 row
// that motivated it) are seen only by the two read-path evaluators, which
// both call this first. WARN only, once per plan version per line; the
// evaluation itself is unchanged. A row whose authoring price cannot be
// established is left unjudged (and unmarked, so a later tape can judge it).
func noteLinesBeyondPrice(at *AutoTrader, row *store.PlanDB, doc *kernel.PlanDoc, site string) {
	if row == nil || doc == nil || (doc.FlipStructured == nil && doc.DeathStructured == nil) {
		return
	}
	key := fmt.Sprintf("%s|%s|v%d", at.id, row.PlanID, row.Version)
	linesBeyondPriceMu.Lock()
	seen := linesBeyondPriceNoted[key]
	linesBeyondPriceMu.Unlock()
	if seen {
		return
	}
	var bars []market.Kline
	if doc.PriceAtWrite <= 0 && market.FuturesBarsProvider != nil {
		bars = market.FuturesBarsProvider(at.futuresSymbol(), kernel.AISVPBarInterval, kernel.AISVPBarCount)
	}
	price := authoringPriceFor(doc, row.CreatedAt, bars)
	if price <= 0 {
		return // unknown authoring price: never invent one, never mark judged
	}
	linesBeyondPriceMu.Lock()
	linesBeyondPriceNoted[key] = true
	linesBeyondPriceMu.Unlock()
	if err := kernel.FlipLineBeyondPrice(doc.FlipStructured, price); err != nil {
		at.logWarnf("flip_line_beyond_price plan=%s v%d (%s) %v — this flip can never be touched from the near side, so it never fires (written before W-FLIP-LINE-SIDE-OF-PRICE or with the price unknown); printed once per plan version", row.PlanID, row.Version, site, err)
	}
	if err := kernel.DeathLineBeyondPrice(doc.DeathStructured, price); err != nil {
		at.logWarnf("death_line_beyond_price plan=%s v%d (%s) %v — this plan was born dead by its own death line; printed once per plan version", row.PlanID, row.Version, site, err)
	}
}

// notePlanProviderNil logs the reason the provider returns nil, once per
// change of reason, and logs the recovery once when a plan is served again.
func notePlanProviderNil(at *AutoTrader, reason string, now time.Time) {
	planProviderNilMu.Lock()
	prev, had := planProviderNilReason[at.id]
	planProviderNilReason[at.id] = reason
	planProviderNilMu.Unlock()
	switch {
	case reason == "" && had && prev != "":
		at.logInfof("🗓️ active-plan provider: a plan is served again at %s (was: %s)", kernel.ClockCTSeconds(now), prev)
	case reason != "" && reason != prev:
		at.logWarnf("🗓️ active-plan provider returns NO PLAN at %s — %s. The executor runs without a plan until this changes; this line prints once per reason, not per cycle (cleanup batch 2, B3).", kernel.ClockCTSeconds(now), reason)
	}
}

func installActivePlanProviderAt(at *AutoTrader, st *store.Store, clock func() time.Time) {
	kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{
		SessionRegistry: func() kernel.SessionRegistry { return at.sessionRegistry(clock()) },
		ActivePlan: func(symbol string) *kernel.ActivePlan {
			now := clock()
			reg := at.sessionRegistry(now) // W8 — provider honors the admin registry too
			sess, ok := reg.ActiveSession(now)
			if !ok {
				notePlanProviderNil(at, "no session is live in the registry", now)
				return nil
			}
			// H8 — the executor must honor the SAME resolver the read scheduler uses.
			// The registry flag is a DEFAULT, never a veto: a session the owner switched
			// on at the strategy level must reach the executor (before this it was
			// written by the read and then dropped here).
			if runnable, _ := at.sessionRunnable(sess); !runnable {
				notePlanProviderNil(at, "session "+sess.Name+" is live but not runnable for this trader", now)
				return nil
			}
			// P0-B — chain identity is the session INSTANCE's date (wrap-aware):
			// at 00:30 CT the provider still resolves the ASIA instance that
			// opened 17:00 yesterday, never tomorrow's empty chain.
			tradeDate, okDate := kernel.PlanChainTradeDate(sess, now)
			if !okDate {
				tradeDate = plannerTradeDateCT(now)
			}
			// P0-A — trader-scoped lookup: THIS trader's row only.
			row, err := st.Plan().GetLatestPlanForTraderSession(tradeDate, sess.Name, at.id)
			if err != nil {
				notePlanProviderNil(at, fmt.Sprintf("plan lookup for %s %s failed: %v", sess.Name, tradeDate, err), now)
				return nil
			}
			if row == nil {
				notePlanProviderNil(at, fmt.Sprintf("no plan row for %s %s (nothing authored yet)", sess.Name, tradeDate), now)
				return nil
			}
			if row.Lifecycle != "active" {
				notePlanProviderNil(at, fmt.Sprintf("plan %s v%d for %s %s is %q, not active", row.PlanID, row.Version, sess.Name, tradeDate, row.Lifecycle), now)
				return nil
			}
			// W4 — the executor cites the OVERLAY-RESOLVED plan_final (owner edits reach
			// the brain), not the base doc. resolveActivePlanDoc folds overlays + armors.
			doc, ok := resolveActivePlanDoc(st, row)
			if !ok {
				notePlanProviderNil(at, fmt.Sprintf("plan %s v%d could not be resolved with its overlays", row.PlanID, row.Version), now)
				return nil
			}
			notePlanProviderNil(at, "", now)
			// The cap must come from the SAME resolver the card uses. A literal 2 here
			// meant the executor prompt and the dashboard narrated different rulebooks
			// the moment a session overrode replan_cap: on 2026-08-16 the owner raised
			// ASIA to 4 mid-session, so at v3 the card said "replans left 2" while the
			// AI was being told 0. P6 — the budget is measured from the chain baseline
			// (an owner reset re-arms it), same seam the death path reads.
			replansLeft := store.GetReplanBudget(st, at.id, row.TradeDate, sess.Name,
				storedReplanCap(st, row.StrategyID, sess.Name)).Left()
			// P0-cleanup — decision records carry the full plan attribution
			// (plan_id, plan_version, overlay_version).
			overlayVersion := 0
			if ovs, err := st.Plan().ListOverlays(row.PlanID, row.Version); err == nil {
				overlayVersion = len(ovs)
			}
			return &kernel.ActivePlan{Doc: doc, Session: sess.Name, Version: row.Version, ReplansLeft: replansLeft, BirthMs: row.CreatedAt.UnixMilli(), PlanID: row.PlanID, OverlayVersion: overlayVersion}
		},
		// W3 (weekly-bias wave) — THIS trader's Sunday weekly-bias doc (nil →
		// no executor line). Per-trader, like the plan provider above.
		WeeklyDoc: func() *kernel.WeeklyDoc { return at.weeklyDocCached(time.Now()) },
	})
	// P0-A — loud startup/runtime assertion: if MORE THAN ONE day-plan trader
	// has registered providers, announce per-trader isolation so the state is
	// visible in the journal on day one, not discovered after the fact.
	if n := kernel.TraderPlanProviderCount(); n > 1 {
		at.logWarnf("🔀 %d day-plan traders registered — per-trader plan/provider isolation is in force (P0-A); no shared plan or provider state.", n)
	}
}

// resolveActivePlanDoc folds a plan's overlays (RFC-6902) into plan_final and
// armors the result via ValidatePlanDoc (falling back to the base doc on any
// failure) — the SAME resolution GET /api/plan/today does, so the card and the
// executor can never diverge. Returns (doc, ok=false) only when the base itself is
// unparseable.
//
// W-EXEC-TRUTH W5 — the fold is kernel.ResolvePlanFinal, the ONE fold every
// reader uses: user overlays fold and re-validate at the hard caps exactly as
// before (a failure falls back to the base), and machine (Picture) scenarios
// are appended AFTER, each validated alone and never counted against the caps.
// With no machine overlay the result is byte-identical to the old fold.
func resolveActivePlanDoc(st *store.Store, row *store.PlanDB) (kernel.PlanDoc, bool) {
	var base kernel.PlanDoc
	if json.Unmarshal([]byte(row.Doc), &base) != nil {
		return kernel.PlanDoc{}, false
	}
	overlays, _ := st.Plan().ListOverlays(row.PlanID, row.Version)
	if len(overlays) == 0 {
		return base, true
	}
	pf, err := kernel.ResolvePlanFinal([]byte(row.Doc), kernel.OverlayRefsFrom(overlays))
	if err != nil {
		return base, true // unreachable: the base parsed above
	}
	if pf.FoldErr != nil {
		// A8 (F14): the fallback-to-base is no longer silent — the owner's
		// overlay is NOT in what the executor reads, and they must know.
		// (free function — package logger, still WARN → log_events sink)
		logger.Warnf("⚠️ merged plan+overlay FAILED re-validation for %s v%d (%v) — falling back to the BASE plan; the overlay edits are NOT active.", row.PlanID, row.Version, pf.FoldErr)
	}
	for _, ms := range pf.MachineSkipped {
		logger.Warnf("⚠️ machine overlay SKIPPED at the fold for %s v%d (%v) — that Picture scenario is NOT in the plan the executor reads.", row.PlanID, row.Version, ms)
	}
	return pf.Doc, true // plan_final (the base on a failed user fold — a bad overlay never corrupts the executor's plan)
}

// recordPlanCitation records the executor's plan citation for an entry decision
// (P3.5 advisory): match-rate counters via B6 + a log line. Advisory only — it
// never gates the trade (plan restricts, never compels). GATED on day_plan.
func (at *AutoTrader) recordPlanCitation(d *kernel.Decision) {
	if !at.dayPlanEnabled() || d == nil {
		return
	}
	if d.Action != "open_long" && d.Action != "open_short" {
		return
	}
	// P5.5 hardening — a new open decision invalidates any citation a PRIOR
	// rejected/failed open left valid, so an open with no active plan can't
	// inherit a stale plan link. Only a live ActivePlan below re-arms it.
	at.lastCitation.valid = false
	if !kernel.HasTraderPlanProvider(at.id) {
		return
	}
	ap := kernel.ActivePlanFor(at.id, at.futuresSymbol())
	if ap == nil {
		return
	}
	res := kernel.ClassifyCitation(d.Action, d.CitedScenario, ap.Doc)
	// B3 (F6): structural verdict — entry in the cited scenario's band, SL/TP
	// consistent. Forward-only; "" = unknown/fail-open.
	band := kernel.CitationStructure(d.Action, d.CitedScenario, ap.Doc, d.Price, d.StopLoss, d.TakeProfit, kernel.PlanDATRFor(at.id))
	switch {
	case res.OffPlan:
		telemetry.IncGateBlock(at.id, "plan_off_plan")
		at.logInfof("📋 advisory: %s cited off-plan (plan v%d).", d.Action, ap.Version)
	case res.Matched:
		telemetry.IncGateBlock(at.id, "plan_matched")
		at.logInfof("📋 advisory: %s cited %s ✓ matched (plan v%d).", d.Action, res.Cited, ap.Version)
	default:
		telemetry.IncGateBlock(at.id, "plan_cited_mismatch")
		at.logInfof("📋 advisory: %s cited %s (direction mismatch; plan v%d).", d.Action, res.Cited, ap.Version)
	}
	// P5.5 — capture the citation so the next position-open stamps its plan link.
	// S3 — also capture the active plan's IDENTITY (plan id / trade date /
	// session) at decision time; the position row then carries an unambiguous
	// join key even across session handoffs (register S3).
	at.lastCitation = planCitation{
		planVersion: ap.Version,
		scenarioID:  res.Cited, // "" when off-plan
		matched:     res.Matched,
		band:        band,
		planID:      ap.PlanID,
		tradeDate:   kernel.PlanTradeDateFor(ap),
		session:     ap.Session,
		valid:       true,
	}
}

// ---- C1 (F3) confirm-grace window ------------------------------------------

// confirmGraceSessions resolves CONFIRM_GRACE_SESSIONS (default 3).
func confirmGraceSessions() int {
	if v := os.Getenv("CONFIRM_GRACE_SESSIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return 3
}

const confirmGraceKey = "confirm_grace_sessions_seen"

// confirmGraceExhausted reports whether the dual-accept window is over: after
// CONFIRM_GRACE_SESSIONS distinct plan-write sessions, confirm{} is REQUIRED.
func (at *AutoTrader) confirmGraceExhausted() bool {
	if at.store == nil {
		return false
	}
	raw, _ := at.store.GetSystemConfig(confirmGraceKey)
	n, _ := strconv.Atoi(strings.TrimSpace(raw))
	if n >= confirmGraceSessions() {
		return true
	}
	_ = at.store.SetSystemConfig(confirmGraceKey, strconv.Itoa(n+1))
	return false
}

// noteConfirmCompliantSession fast-forwards the grace window once the model
// proves it authors confirm{} — no reason to keep accepting regressions.
func (at *AutoTrader) noteConfirmCompliantSession() {
	if at.store == nil {
		return
	}
	_ = at.store.SetSystemConfig(confirmGraceKey, strconv.Itoa(confirmGraceSessions()))
}

// recordRepairOutcome (REPAIR-PARSE E4, 2026-09-02) classifies what a repair
// attempt actually produced, logs it loudly with the class, and RECORDS it.
// The old line called every failure "UNPARSEABLE"; measured, 17 of 18 parsed
// fine and were rejected on field values, so the label was misleading exactly
// where a diagnosis was needed.
func (at *AutoTrader) recordRepairOutcome(raw string, err error, repairingReason string) {
	outcome := kernel.ClassifyRepairOutcome(raw, err)
	at.logWarnf("🩹 repair outcome=%s — falling back to a full re-author next attempt · reject=%v · was repairing: %s · raw_head=%q",
		outcome, err, clampLine(repairingReason, 200), clampLine(raw, 400))
	if _, ierr := store.IncRepairOutcome(at.store, string(outcome)); ierr != nil {
		at.logWarnf("🩹 repair counter write failed: %v", ierr)
	}
}

// voidWindowStartMs is DELETED (VOID PARITY, 2026-09-02). It scoped the prompt's
// void scan to the session day "so a level broken and reclaimed days ago is not
// reported as today's news" — a deliberate noise choice that DISAGREED with the
// write-site validator, which has always judged the whole slice. The prompt then
// omitted exactly the levels the validator rejects: ONL 29141.25 on the 20:58 CT
// read, an overnight low, broken overnight, before the 17:00 boundary. The scope
// now comes from kernel.ResolveVoidScope for BOTH sides. Owner ruling: the
// validator's scope wins, because the prompt must list what the validator will
// reject. The noise concern is real and is answered by the list, not the window.

// plannerATR5m resolves the SAME ATR(14) on 5m the arm composer floors against
// (trader/arm_stop_anchor.go reads it from the identical helper), so the number
// the prompt states is the number the executor enforces. 0 when unavailable —
// the prompt then renders no floor line rather than an invented one.
func plannerATR5m(symbol string) float64 {
	if market.FuturesBarsProvider == nil {
		return 0
	}
	return market.ExportCalculateATR(kernel.AcceptanceBars(market.FuturesBarsProvider(symbol, "5m", 200), "2x5m"), 14)
}

// plannerCandleTapeBars is the 1m depth the planner's candle tables ask for.
// 8 CME session-days × ~1,380 open minutes ≈ 11,040, so 12,000 is the honest
// ask for an "8 daily session candles" table. It is NAMED rather than a
// literal so the ask and the disclosure that reports it cannot drift.
const plannerCandleTapeBars = 12000

// rvBaselineFallback5mBarsAsk is the 5m depth the realized-vol baseline asks
// for WHEN THE 1m TAPE CANNOT ANSWER — it is no longer the primary input.
//
// RENAMED TO WHAT IT ACTUALLY RECEIVES (owner condition (b), 2026-09-09). It
// was rvBaseline5mBarsAsk, the one and only source; the baseline now comes from
// the store-deepened 1m tape aggregated to 5m (ResolveRVBaselineTape,
// trader/regime_input_window.go) and falls back to this ring read only so a
// thin 1m tape can never turn a working baseline OFF (A10 — degrade, never
// gate).
//
// THE ASK ITSELF WAS ALSO CORRECTED (D2 choice (b)): it was 5m × 3000 = 250.0 h
// against a 208.3 h ring ceiling — unreachable BY CONSTRUCTION, not by market
// conditions. It is now READ from the ring's own ceiling rather than copied
// from it, so the ask and the ceiling cannot drift (A11).
//
// WHY THE STORE'S 5m ROWS ARE STILL REFUSED, at this site and in the boot
// rehydrate:
//  1. the store's 5m does not reach 20 days either — measured 2026-09-09 it
//     held 3,314 MNQ 5m rows back to 2026-08-24, about 16 days — so a store
//     read would swap one unmet promise for another; and
//  2. the stored 5m rows are NT8 aggregates that this repo has already judged
//     inconsistent with their own 1m constituents (store/bar_history.go
//     Migrate step 4 deleted every tf != "1m" row for exactly that reason, and
//     the per-TF persistence added back on 2026-09-02 did not re-establish
//     their agreement). MEASURED: over the SAME nine complete session-days the
//     stored 5m rows give 0.884746 and the 1m constituents give 0.893543.
//     Feeding the aggregate into a LIVE regime input is not a depth
//     improvement; it is an unverified substitution.
const rvBaselineFallback5mBarsAsk = nt.DefaultBarCacheMaxBars

// rvBaselineMaxDays is the averaging window cap handed to the estimator. It is
// UNCHANGED at 20 — this wave does not alter what anything computes — but it is
// now named rather than a literal beside a field that claimed to be fed it.
const rvBaselineMaxDays = 20

// buildReadFactRow builds the one planner_read_facts row per read. Extracted
// from persistReadFacts (class 86) so a pin drives THE PRODUCTION BUILDER
// rather than a copy of it, and pure — it reads no clock (A28: `now` comes in).
//
// D4 (BARS HORIZON 2026-09-09) — WHAT CHANGED, AND WHAT DELIBERATELY DID NOT:
//
// ScopeBars IS NOT REDEFINED. It has always been len(scope.Bars): the SERVED
// count, after truncation. The premise that it recorded the REQUEST is NOT
// REPRODUCED — 67 of 67 live rows (ids 1..67) record scope_bars=2000 against a
// 2000 ask, so the void scope has never been short, and reinterpreting the
// column would silently rewrite the meaning of 67 correct rows.
//
// What a COUNT cannot express is a SPAN or a HOLE. Rows 64 and 66 were
// identical on the record, yet row 66's tape reached back to 2026-09-07 10:23
// CT — 3,055 minutes — with 696 OPEN-MARKET minutes missing inside it. So four
// additive columns carry the request, the span, the oldest bar's age and the
// gap count, plus read_horizons as the JSON of the horizons themselves.
//
// UNKNOWN vs ZERO: read_horizons == "" marks a row whose horizon was never
// computed; its four numerics are UNKNOWN, not zero (HorizonRecorded, A24).
func buildReadFactRow(traderID string, in kernel.PlannerInput, scope kernel.VoidScope, void []kernel.VoidBreakdownLevel, atr5m float64, now time.Time) *store.PlannerReadFact {
	recs := make([]store.VoidLevelRecord, 0, len(void))
	for _, v := range void {
		recs = append(recs, store.VoidLevelRecord{Price: v.Price, Short: v.Short, ReclaimedAt: v.ReclaimedAtCT})
	}
	mult := kernel.MinSLATRMult()
	floor := 0.0
	if atr5m > 0 && mult > 0 {
		floor = atr5m * mult
	}
	row := &store.PlannerReadFact{
		TraderID:     traderID,
		TradeDate:    plannerTradeDateCT(now),
		Session:      in.Session,
		PromptHash:   in.AIConfigHash,
		VoidLevels:   store.EncodeVoidLevels(recs),
		VoidCount:    len(recs),
		StopFloorPts: floor,
		ATR5m:        atr5m,
		StopFloorMlt: mult,
		BiasRegime:   fmt.Sprintf("%s/%s", in.Regime.TrendDaily, in.Regime.ATRRegime),
		ScopeSinceMs: scope.SinceMs,
		ScopeBars:    len(scope.Bars), // SERVED — unchanged, and NOT the request
		ScopeIntv:    scope.Interval,
	}
	h := scope.Horizon
	if h.Interval == "" {
		// The scope was built without a horizon (a pre-wave caller or a bare
		// literal). Leave every horizon column at its zero value AND
		// read_horizons empty, so the row reads UNKNOWN rather than "no gaps".
		return row
	}
	row.ScopeRequestedBars = h.Requested
	row.ScopeSpanMs = h.SpanMs
	row.ScopeOldestAgeMs = h.OldestAgeMs
	row.ScopeGapCount = h.GapCount
	if b, err := json.Marshal([]kernel.BarHorizon{h}); err == nil {
		row.ReadHorizons = string(b)
	}
	return row
}

// reappendLiveMachineScenarios (W5) re-appends every LIVE machine scenario of
// version newVersion-1 to newVersion as a machine overlay carrying the SAME
// scenario value. LIVE = inside its eligibility window (kernel.MachineEligibleAt)
// and no ledger row for its opportunity is terminal (the ledger's source pin:
// one opportunity, one order, across versions). Idempotent on the opportunity
// (the check runs inside the plan store's single writer).
func (at *AutoTrader) reappendLiveMachineScenarios(planID string, newVersion int, now time.Time) {
	if at.store == nil || planID == "" || newVersion <= 1 {
		return
	}
	plans := at.store.Plan()
	prev, err := plans.GetPlan(planID, newVersion-1)
	if err != nil || prev == nil {
		return
	}
	prevOvs, err := plans.ListOverlays(planID, prev.Version)
	if err != nil {
		at.logWarnf("🖼 picture re-append %s v%d: the previous version's overlays are unreadable (%v) — no Picture scenario carried", planID, newVersion, err)
		return
	}
	pf, err := kernel.ResolvePlanFinal([]byte(prev.Doc), kernel.OverlayRefsFrom(prevOvs))
	if err != nil {
		return // the previous doc does not parse: it carried nothing we can read
	}
	var live []kernel.PlanScenario
	for _, sc := range pf.Doc.Scenarios {
		if sc.Machine == nil {
			continue
		}
		if !kernel.MachineEligibleAt(sc, now.UnixMilli()) {
			at.logInfof("🖼 picture scenario %s (ref %s) not re-appended to %s v%d — its eligibility window closed at %s", sc.ID, store.RedactPictureOppKey(sc.Machine.Ref), planID, newVersion, kernel.ClockCTSeconds(time.UnixMilli(sc.Machine.EligibleUntilMs)))
			continue
		}
		if done, why := at.machineOpportunityFinished(planID, sc.Machine.Ref); done {
			at.logInfof("🖼 picture scenario %s (ref %s) not re-appended to %s v%d — %s", sc.ID, store.RedactPictureOppKey(sc.Machine.Ref), planID, newVersion, why)
			continue
		}
		live = append(live, sc)
	}
	if len(live) == 0 {
		return
	}
	row, err := plans.GetPlan(planID, newVersion)
	if err != nil || row == nil {
		at.logWarnf("🖼 picture re-append: %s v%d is unreadable (%v) — %d live Picture scenario(s) NOT carried", planID, newVersion, err, len(live))
		return
	}
	for _, sc := range live {
		sc := sc
		ref := sc.Machine.Ref
		o := &store.PlanOverlayDB{OverlayID: "picture:" + ref, PlanID: planID, PlanVersion: newVersion, Origin: kernel.MachineOverlayOriginPicture}
		ver, appended, aerr := plans.AppendOverlayChecked(o, func(existing []*store.PlanOverlayDB) (bool, error) {
			cur, perr := kernel.ResolvePlanFinal([]byte(row.Doc), kernel.OverlayRefsFrom(existing))
			if perr != nil {
				return false, perr
			}
			if _, ok := kernel.MachineScenarioByRef(cur.Doc, ref); ok {
				return true, nil // already carried
			}
			if verr := kernel.ValidateMachineScenario(cur.Doc, sc); verr != nil {
				return false, verr
			}
			patch, merr := kernel.MachineOverlayPatch(sc)
			if merr != nil {
				return false, merr
			}
			o.Patch = patch
			return false, nil
		})
		switch {
		case aerr != nil:
			at.logWarnf("🖼 picture scenario %s (ref %s) could NOT be re-appended to %s v%d: %v — it is not in the new plan and will not trade", sc.ID, store.RedactPictureOppKey(ref), planID, newVersion, aerr)
		case appended:
			at.logInfof("🖼 picture scenario %s re-appended to %s v%d (overlay o%d) — same ref %s, same evidence; window until %s", sc.ID, planID, newVersion, ver, store.RedactPictureOppKey(ref), kernel.ClockCTSeconds(time.UnixMilli(sc.Machine.EligibleUntilMs)))
		}
	}
}

// machineOpportunityFinished reports whether the armed ledger already finished
// an opportunity: any row for its source_ref is terminal (the same rule as
// UpsertArm's source pin — a finished opportunity never arms again).
func (at *AutoTrader) machineOpportunityFinished(planID, ref string) (bool, string) {
	rows, err := at.store.ArmedOrders().ListForPlan(planID)
	if err != nil {
		return true, fmt.Sprintf("the armed ledger is unreadable (%v) — fail-closed", err)
	}
	for _, r := range rows {
		if r.TraderID == at.id && r.SourceRef == ref && store.IsTerminalArmState(r.State) {
			return true, fmt.Sprintf("its ledger row #%d is %s", r.ID, r.State)
		}
	}
	return false, ""
}
