package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
	"nofx/trader"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// traderDayPlanEnabled reports whether a trader has the day-plan feature on — the
// gate for mutating the (session-global) day plan.
func traderDayPlanEnabled(at *trader.AutoTrader) bool {
	cfg := at.GetStrategyConfig()
	return cfg != nil && cfg.DayPlan != nil && cfg.DayPlan.PlanEnabled
}

// weeklyPayload renders the WEEKLY chip payload for the day-plan card: the
// current governed week's Sunday weekly doc (REFS ONLY since class 50), or nil
// (the card renders the grey "none" chip).
func weeklyPayload(st *store.Store, traderID string, now time.Time) gin.H {
	if st == nil {
		return nil
	}
	monday := kernel.WeekGoverningMonday(now).Format("2006-01-02")
	row, err := st.Plan().GetLatestPlanForTraderSession(monday, "WEEKLY", traderID)
	if err != nil || row == nil {
		return nil
	}
	var doc kernel.WeeklyDoc
	if json.Unmarshal([]byte(row.Doc), &doc) != nil {
		return nil
	}
	// CLASS 50 (refs-only wave): the chip renders refs only. The legacy bias
	// fields are dropped from the payload entirely — the frontend has no
	// directional field to render anymore.
	payload := gin.H{
		"refs_only":     true,
		"narrative":     doc.Narrative,
		"weekly_levels": doc.WeeklyLevels,
		"thin_history":  doc.ThinHistory,
		"shadow_bias":   doc.ShadowBias,
	}
	for _, l := range doc.WeeklyLevels {
		if strings.EqualFold(strings.TrimSpace(l.Name), "PWH") && l.Px > 0 {
			payload["pwh"] = l.Px
		}
		if strings.EqualFold(strings.TrimSpace(l.Name), "PWL") && l.Px > 0 {
			payload["pwl"] = l.Px
		}
	}
	return payload
}

// planOverlayMu serializes the read-fold-test-append span of an overlay write so
// the §42 test-op concurrency guard is atomic (two racing writers can't both test
// against the same stale plan_final and both append). Overlay writes are rare
// (owner edits), so a single coarse mutex is ample.
var planOverlayMu sync.Mutex

// P4.1 — /api/plan/* (mirror /api/risk/* inline trader_id). Additive + read-only
// except the alert ack. Handles the "day_plan enabled but no plan yet" pre-★2
// state gracefully: found=false, so the card renders its no-plan-yet state.

func planChicago() *time.Location {
	return kernel.CTLocation()
}

func maxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// C1 (2026-08-25) — cross-user IDOR gate for every /api/plan/* AND /api/risk/*
// route that carries a trader_id. Handlers previously trusted ?trader_id (or a
// body-carried one) blindly: any authenticated user could read or mutate another
// user's trader by guessing a trader id (plan docs, force-flat, clear-freeze...).
// Mounted on the protected group; it fires only for those path prefixes and 404s
// (not 403 — never leak trader existence) when the JWT user does not own the
// queried trader. Global risk feeds (/risk/freezes, /risk/gate-blocks,
// /risk/errors) carry no trader_id and pass through unchanged.
func (s *Server) planTraderOwnership() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.FullPath()
		if !strings.HasPrefix(p, "/api/plan/") && !strings.HasPrefix(p, "/api/risk/") {
			c.Next()
			return
		}
		traderID := strings.TrimSpace(c.Query("trader_id"))
		// Body-carried trader_id (POST/PUT handlers accept it as an alternative
		// to the query) is probed WITHOUT consuming the body: the bytes are read
		// and restored so the handler's ShouldBindJSON still sees them.
		if traderID == "" && c.Request != nil && c.Request.Body != nil &&
			(c.Request.Method == "POST" || c.Request.Method == "PUT") {
			if raw, err := io.ReadAll(c.Request.Body); err == nil {
				c.Request.Body = io.NopCloser(bytes.NewReader(raw))
				var probe struct {
					TraderID string `json:"trader_id"`
				}
				if json.Unmarshal(raw, &probe) == nil {
					traderID = strings.TrimSpace(probe.TraderID)
				}
			}
		}
		if traderID == "" {
			c.Next() // the handler's own "trader_id is required" governs
			return
		}
		if !s.traderOwnedBy(c.GetString("user_id"), traderID) {
			SafeNotFound(c, "Trader")
			c.Abort()
			return
		}
		c.Next()
	}
}

// traderOwnedBy reports whether the trader id belongs to the JWT user (C1/C3).
func (s *Server) traderOwnedBy(userID, traderID string) bool {
	if userID == "" || traderID == "" {
		return false
	}
	rows, err := s.store.Trader().List(userID)
	if err != nil {
		return false
	}
	for _, t := range rows {
		if t.ID == traderID {
			return true
		}
	}
	return false
}

// planRules resolves THE RULEBOOK THE GATES ACTUALLY RUN for one trader+session:
// the acceptance rule, the plan-restriction mode, and the remaining re-plan
// budget. W15.B — every one of these was HARDCODED here ("2x5m", "advisory", a
// budget of 2), so a strategy configured for 15m-close acceptance, strict mode,
// or a non-default replan_cap was narrated WRONG on the card the owner reads,
// while the executor ran the real values. Per-session overrides resolve too.
//
// An unknown trader falls back to the shipped defaults, which is what the card
// showed before, so the no-config path is unchanged.
func (s *Server) planRules(traderID, session, tradeDate string) (rule, mode string, replansLeft int) {
	rule, mode, replansLeft, _ = s.planRulesWithCap(traderID, session, tradeDate)
	return rule, mode, replansLeft
}

// approvalRequired resolves the strategy's approval_required flag (W9): ON =
// entries wait for the owner's plan-header Approve per CME session-day.
func (s *Server) approvalRequired(traderID string) bool {
	if at, err := s.traderManager.GetTrader(traderID); err == nil && at != nil {
		if cfg := at.GetStrategyConfig(); cfg != nil && cfg.DayPlan != nil {
			return cfg.DayPlan.ApprovalRequired
		}
	}
	return false
}

// planRulesWithCap also returns the RESOLVED re-plan cap, so the card can say
// "4 of 4 re-plans spent" instead of re-deriving a number it does not have.
// The budget is measured from the chain baseline (P6 owner reset) — the same
// seam the death path and the executor provider read.
// armedMapFor builds the scenario→state map the card chips read (Wave 2).
// E4 (entry-mechanics 2026-08-30): a split arm has TWO rows (leg_index 0/1) —
// the chip gets the per-leg states so the card shows the split contract.
func (s *Server) planRulesWithCap(traderID, session, tradeDate string) (rule, mode string, replansLeft, replanCap int) {
	dp := s.dayPlanCfgFor(traderID)
	rule = dp.AcceptanceRuleFor(session)
	mode = dp.PlanModeFor(session)
	replanCap = dp.ReplanCapFor(session)
	// CLASS 35 (2026-09-01) — the RECORDED budget, the same seam the death gate
	// and the executor prompt read. It used to be cap − (version − baseline)
	// from the row being served, which counted wake reads and dormant flips
	// as spends (today's LONDON: six rows, nothing spent, "0 re-reads left").
	replansLeft = store.GetReplanBudget(s.store, traderID, tradeDate, session, replanCap).Left()
	return rule, mode, replansLeft, replanCap
}

// dayPlanCfgFor resolves a trader's day-plan config through the trader manager;
// nil (→ the shipped defaults in every resolver) when the trader or the manager
// is absent, so the budget seam is usable without a live manager.
func (s *Server) dayPlanCfgFor(traderID string) *store.DayPlanConfig {
	if s == nil || s.traderManager == nil {
		return nil
	}
	if at, err := s.traderManager.GetTrader(traderID); err == nil && at != nil {
		if cfg := at.GetStrategyConfig(); cfg != nil {
			return cfg.DayPlan
		}
	}
	return nil
}

// resolveRequestedSession applies an OPTIONAL ?session=NY|ASIA|LONDON to the
// plan lookup. W15.B — the card's session tabs used to be pure highlighting:
// clicking ASIA moved the chip and NOTHING else, because the request had no
// session dimension and always returned the ACTIVE session's plan. With the
// param the tab shows that session's plan for the trade date.
//
// Absent or unrecognized → the live session, i.e. unchanged behavior. The
// explicit flag lets the caller read a session that is not currently live, which
// is the entire point of the tabs.
func resolveRequestedSession(reg kernel.SessionRegistry, want, activeName string, active *kernel.SessionDef) (string, *kernel.SessionDef, bool) {
	want = strings.ToUpper(strings.TrimSpace(want))
	if want == "" {
		return activeName, active, false
	}
	def, found := reg.SessionByName(want)
	if !found {
		return activeName, active, false
	}
	return def.Name, def, true
}

// handlePlanToday GET /api/plan/today?trader_id=xxx[&symbol=MNQ] — the active
// plan (overlay-resolved) + live per-level facts from the P0.4 evaluator.

// planReadingFields derives the card's reading semantics FROM THE STORE (F7,
// 2026-08-30). The raw in-flight claim is only "the planner is writing" while
// NO row is committed; once a row exists the card renders that plan and a
// running read is exposed as replan_in_flight instead. A failed read (benign
// wake failures) therefore always lands back on the kept plan, never on a
// stuck writing state.
func planReadingFields(hasRow, inFlight bool) (reading, replanInFlight bool) {
	if !hasRow {
		return inFlight, false
	}
	if inFlight {
		return false, true
	}
	return false, false
}

func (s *Server) handlePlanToday(c *gin.Context) {
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if _, err := s.traderManager.GetTrader(traderID); err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	symbol := strings.TrimSpace(c.Query("symbol"))
	if symbol == "" {
		symbol = "MNQ"
	}

	reg := s.planRegistry()
	now := time.Now()
	sess, ok := reg.ActiveSession(now)
	sessName := ""
	if ok {
		sessName = sess.Name
	}
	night := reg.IsNightMode(now)

	// W15.B — which sessions THIS strategy runs, resolved by the same gate the
	// bot uses. The card's tabs used to derive this from a hardcoded FE constant,
	// so switching ASIA on left its tab permanently greyed out.
	runnable := []string{}
	if at, err := s.traderManager.GetTrader(traderID); err == nil && at != nil {
		runnable = at.RunnableSessions()
	}
	activeName := sessName
	sessName, sess, explicit := resolveRequestedSession(reg, c.Query("session"), sessName, sess)

	// P0-B — the chain identity is the SESSION INSTANCE's date (wrap-aware):
	// an ASIA card opened at 00:30 CT reads the instance that opened 17:00
	// yesterday, never tomorrow's empty chain.
	tradeDate := now.In(planChicago()).Format("2006-01-02")
	if d, okD := kernel.PlanChainTradeDate(sess, now); okD {
		tradeDate = d
	}

	// W15.B — the card narrates THE REAL RULEBOOK (acceptance rule / plan mode /
	// re-plan budget), resolved per trader+session, instead of the hardcoded
	// "2x5m" + "advisory" + budget-of-2 it used to show.
	rule, mode, _ := s.planRules(traderID, sessName, tradeDate)
	// UI-verification (2026-08-18): the card must SAY when a planner read is in
	// flight — the owner reset while a death re-plan was writing and the UI
	// showed nothing for minutes, which read as "the button does nothing".
	//
	// F7 (2026-08-30): the reading status must derive from the STORE, not the
	// raw in-flight claim. Wake re-reads hold the claim for 3-9 minutes each,
	// back-to-back all evening, so a claim-keyed flag kept the card on "writing
	// a fresh plan" for hours while a committed, tradeable plan row sat in the
	// DB. Rule: "writing" ONLY while no row exists yet; once a row is committed
	// the card renders the plan and a running read is exposed as
	// replan_in_flight (a subtle chip, never the writing state).
	inFlight := false
	if at, aErr := s.traderManager.GetTrader(traderID); aErr == nil && at != nil {
		inFlight = at.PlannerReadInFlight(tradeDate, sessName)
	}
	row, rowErr := s.store.Plan().GetLatestPlanForTraderSession(tradeDate, sessName, traderID)
	hasRow := rowErr == nil && row != nil
	reading, replanInFlight := planReadingFields(hasRow, inFlight)
	base := gin.H{
		"found": false, "trade_date": tradeDate, "session": sessName, "night": night,
		"mode": mode, "acceptance_rule": rule, "reading": reading,
		"replan_in_flight":  replanInFlight,
		"approval_required": s.approvalRequired(traderID),
		"active_session":    activeName, "is_active": sessName != "" && sessName == activeName,
		"runnable_sessions": runnable,
		// W7 (weekly-bias wave) — the WEEKLY chip payload (null → grey "none").
		"weekly": weeklyPayload(s.store, traderID, now),
		// W-EXEC-TRUTH W0 (CTO Q6) — Picture's plan-mode verdict, READ (null
		// when the trader is not loaded): the card says when strict refuses it.
		"picture": s.picturePlanGate(traderID, now),
	}
	// An EXPLICITLY requested session is readable whether or not it is the live one
	// (that is the point of the tabs); without the param we keep the old gate.
	// P1 — enablement is resolved through the trader's sessionRunnable (the bot's
	// own resolver), never the raw registry flag: a strategy-enabled session the
	// bot RUNS must never read found:false on its card.
	runnableOK := true
	if at, err := s.traderManager.GetTrader(traderID); err == nil && at != nil {
		if okR, _ := at.SessionRunnable(sess); !okR {
			runnableOK = false
		}
	}
	if !explicit && (!ok || !runnableOK) {
		c.JSON(200, base) // night / disabled session → no active plan
		return
	}
	if explicit && sessName == "" {
		c.JSON(200, base)
		return
	}
	if !hasRow {
		c.JSON(200, base) // enabled but no plan yet (pre-★2) → graceful
		return
	}
	// ITEM 15 — ?version=N serves a HISTORICAL version instead of the latest.
	// Every version has always been stored; nothing could ask for one. Reusing this
	// handler (rather than adding a parallel one) means a historical view resolves
	// overlays, facts, degradation and scenario status through exactly the same
	// code as the live card — including the requirement that owner overlays render
	// on the version they belonged to, since overlays are keyed (plan_id, version).
	latestVersion := row.Version
	historical := false
	if raw := strings.TrimSpace(c.Query("version")); raw != "" {
		want, convErr := strconv.Atoi(raw)
		if convErr != nil || want < 1 {
			SafeBadRequest(c, "version must be a positive integer")
			return
		}
		if want != row.Version {
			old, gErr := s.store.Plan().GetPlan(row.PlanID, want)
			if gErr != nil || old == nil {
				SafeNotFound(c, "Plan version")
				return
			}
			row = old
			historical = true
		}
	}
	var doc kernel.PlanDoc
	if json.Unmarshal([]byte(row.Doc), &doc) != nil {
		c.JSON(200, base)
		return
	}
	// P5.1 — plan_final = base doc + overlays (RFC-6902, applied in ASC order). A
	// bad overlay is skipped; the applied result is re-armored via ValidatePlanDoc
	// and falls back to the base doc on failure (the plan-doc analog of B2 armor).
	overlays, _ := s.store.Plan().ListOverlays(row.PlanID, row.Version)
	var overlayErrStrings []string // A8: surfaced on the response below
	// W-EXEC-TRUTH W5 — the ONE fold (kernel.ResolvePlanFinal): user overlays
	// exactly as before, machine (Picture) scenarios appended after, never
	// counted against the caps. composed_of records which overlay rows made
	// the doc the card shows (CTO requirement); [] when none, never null.
	composedUser := []int{}
	composedMachine := []gin.H{}
	if len(overlays) > 0 {
		if base, mErr := json.Marshal(doc); mErr == nil {
			if pf, pErr := kernel.ResolvePlanFinal(base, kernel.OverlayRefsFrom(overlays)); pErr == nil {
				// A8 (F14): a stored overlay that no longer applies must never be a
				// silent no-op — the owner's edit visibly vanished. WARN (deduped
				// by content via log_events) + expose on the response.
				for _, oe := range pf.OverlayErrs {
					if oe != nil {
						overlayErrStrings = append(overlayErrStrings, oe.Error())
						logger.Warnf("⚠️ plan overlay SKIPPED at read-merge (%s %s): %v — the owner edit in that patch is not in the rendered plan", row.PlanID, row.Session, oe)
					}
				}
				for _, ms := range pf.MachineSkipped {
					logger.Warnf("⚠️ machine overlay SKIPPED at read-merge (%s %s): %v — that Picture scenario is not in the rendered plan", row.PlanID, row.Session, ms)
				}
				doc = pf.Doc // plan_final (the base on a failed user fold, as before)
				composedUser = append(composedUser, pf.UserApplied...)
				for _, m := range pf.MachineApplied {
					composedMachine = append(composedMachine, gin.H{"overlay_version": m.OverlayVersion, "scenario_id": m.ScenarioID, "ref": m.Ref})
				}
			}
		}
	}

	owners := map[int64]*store.OwnerLevelDB{}
	if rows, oErr := s.store.OwnerLevel().ListActiveForUser(c.GetString("user_id"), symbol); oErr == nil {
		for _, r := range rows {
			owners[roundKey(r.Price)] = r
		}
	}
	facts, price, fvgStates := planLevelFacts(traderID, symbol, doc, now, rule, owners, row.CreatedAt.UnixMilli())
	// CLASS 35 — the budget is a session-chain property (recorded counter),
	// not a property of the version being viewed.
	_, _, replansLeft, replanCap := s.planRulesWithCap(traderID, sessName, tradeDate)
	warming := ""
	if n, _ := s.store.SessionProfile().Count(symbol); n < 10 {
		warming = fmt.Sprintf("%d/10", n)
	}

	scenarioIDs := make([]string, 0, len(doc.Scenarios))
	scenarioDeaths := make(map[string]*store.ScenarioDeath)
	for _, sc := range doc.Scenarios {
		scenarioIDs = append(scenarioIDs, sc.ID)
		if anchor, ok := kernel.ScenarioAnchor(sc, doc.Levels); ok {
			if r, err := s.store.ScenarioDeathFor(traderID, row.PlanID, row.Version, sc.ID, anchor); err == nil && r != nil {
				scenarioDeaths[sc.ID] = r
			}
		}
	}
	liveness := s.store.ScenarioLivenessFor(traderID, row.PlanID, row.Version, scenarioIDs, now)
	if row.Lifecycle != "active" {
		liveness.Tradeable = nil
		liveness.Reason = "plan is not active; recorded scenario history shown separately"
	}

	// W-ARM-STATE-UI — the dormant chip must say WHY the plan went dormant
	// (death vs flip), not why the version was WRITTEN (trigger_reason). The
	// marker lives in plan_lifecycle_log; the newest event whose Event matches
	// the row's CURRENT lifecycle is the truth. Absent -> the key is omitted
	// entirely, never "".
	lifecycleReason := ""
	if row.Lifecycle != "" {
		if events, lErr := s.store.Plan().LifecycleLog(row.PlanID, row.Version); lErr == nil {
			for i := len(events) - 1; i >= 0; i-- {
				if events[i].Event == row.Lifecycle {
					lifecycleReason = events[i].Reason
					break
				}
			}
		}
	}

	resp := gin.H{
		"scenario_identity": kernel.ScenarioIdentities(&doc),
		"scenario_liveness": liveness,
		"scenario_deaths":   scenarioDeaths,
		"found":             true,
		"trade_date":        tradeDate,
		"session":           sessName,
		"version":           row.Version,
		"reading":           reading,
		// F7 — a read is running while THIS plan row is committed: the card
		// renders the plan and shows a subtle re-reading chip, never "writing".
		"replan_in_flight": replanInFlight,
		// Wave 2 armed orders — the per-scenario arm state for the card chips.
		"armed": s.armedMapFor(row.PlanID, row.Version, doc, s.planOrderBook(traderID, now)),
		// INVALIDATION-WIRED F2 (2026-09-03) — the OPEN position's own
		// provenance, so the card can never render the live plan's scenario as
		// the position's. On 2026-09-03 the card showed v3 S1 LONG (written
		// 09:15) over a position armed under v2 S1 SHORT, and the owner read
		// long on a short.
		"open_position": s.openPositionProvenance(traderID, row.Version),
		// ITEM 15 — the card marks itself HISTORICAL and offers the way back.
		"historical":        historical,
		"latest_version":    latestVersion,
		"trigger_reason":    row.TriggerReason,
		"created_at":        row.CreatedAt,
		"overlay_count":     len(overlays),
		"lifecycle":         row.Lifecycle,
		"model_id":          row.ModelID,
		"night":             false,
		"mode":              mode,
		"approval_required": s.approvalRequired(traderID),
		// which session this payload IS, vs which one is live right now
		"active_session":    activeName,
		"is_active":         sessName == activeName,
		"runnable_sessions": runnable,
		"doc":               doc,
		// NO-TRADE BAND (2026-09-02) — the machine-written windows, judged
		// against THIS read's clock for THIS card's session. Each carries a
		// status (live / elapsed / other_session) so the card shows what still
		// constrains trading instead of every rule the plan was born with. Nil
		// for a doc written before the band existed; the card falls back to the
		// model's prose, which this wave leaves exactly as authored.
		"no_trade_band": kernel.RenderNoTradeBand(&doc, sess, now),
		"level_facts":   facts,
		"price":         price,
		"replans_left":  replansLeft,
		// The RESOLVED cap (config, never a literal) so the card can state the
		// budget instead of inferring it from a version number — a NO-TRADE marker
		// consumes a version, so version-1 overcounts by exactly one.
		"replan_cap": replanCap,
		// The acceptance rule the executor evaluates these levels with — the card
		// used to imply 2x5m unconditionally.
		"acceptance_rule": rule,
		"warming":         warming,
		// P2 — how blind the planner was when it wrote this plan. The card shows a
		// DEGRADED badge past the threshold so a half-map plan says so out loud.
		"dark_regime_count": row.DarkRegimeCount,
		"degraded":          row.Degraded,
		// Current evaluator statuses, read only from the displayed version.
		// An uncomputed version has no map; legacy unversioned keys are ignored.
		"scenario_status": scenarioStatusForLifecycle(s.scenarioStatus(traderID, row.PlanID, row.Version), row.Lifecycle, doc),
		// W2 FADE PERMISSION (2026-09-10) — per-scenario label and the
		// session-day counter, both READ. A LABEL, never a gate: the card
		// renders it beside the scenario and nothing downstream refuses on it.
		"fade_permission": s.fadePermissionFor(traderID, &doc),
		"fade_counter":    s.fadeCounterFor(traderID),
		// ONE SETUP (dispatch 102, 2026-09-10) — the arm seam's RECORDED verdict
		// per scenario (what it decided, never a re-evaluation) plus the resolved
		// switch; absent record → the switch only, scenarios empty (A24).
		"one_setup":           s.oneSetupFor(traderID, row.PlanID, row.Version),
		"structural_geometry": s.planStructuralGeometry(traderID, row.PlanID, row.Version),
		// A1/A4 (fail-register wave): verdict basis (machine vs prose-anchor
		// heuristic) + unevaluable scenario ids — the card renders them
		// distinctly instead of dressing a heuristic as a machine verdict.
		"scenario_meta": s.scenarioMeta(traderID, row.PlanID, row.Version),
		// FVG ENTRY MODEL (2026-08-26) — per-scenario gap-band live states for
		// the card chips (IN_ZONE/ABOVE/BELOW/FILLED_INVALID + touch number).
		"fvg_states":     fvgStates,
		"overlay_errors": overlayErrStrings,
		// W7 (weekly-bias wave) — the WEEKLY chip payload. F1 (2026-08-30):
		// the found=true branch built its own map WITHOUT this key, so the card
		// rendered grey "WEEKLY none" while a neutral-invalidated doc existed.
		"weekly": weeklyPayload(s.store, traderID, now),
		// W-EXEC-TRUTH W0 (CTO Q6) — Picture's plan-mode verdict, READ.
		"picture": s.picturePlanGate(traderID, now),
		// ITEM 4 — owner edits that could NOT be re-anchored onto this version.
		// Never dropped silently: the card asks for review.
		"uncarried_edits": s.uncarriedEdits(row.PlanID, row.Version),
		// W-EXEC-TRUTH W5 — what composed this doc: the user overlay versions
		// applied, and every machine (Picture) scenario the fold appended.
		"composed_of": gin.H{"user_overlays": composedUser, "machine": composedMachine},
		// W5 — the row is a MACHINE plan (the Picture no-plan door): no AI
		// plan exists yet for this session.
		"machine_plan": store.IsMachinePlan(row),
	}
	if lifecycleReason != "" {
		resp["lifecycle_reason"] = lifecycleReason
	}
	// W-EXEC-TRUTH W2 A1/A2 — the born check the write site stored on THIS
	// row (policy, read/publish clocks, judged 5m groups); a pre-W2 row says
	// recorded=false and the card renders n/a.
	resp["authored_invalidation"] = authoredInvalidationFor(row)
	c.JSON(200, resp)
}

// planLevelFacts computes per-level live facts from the latest 1m bars. Also
// returns the per-scenario fvg_entry live states (FVG ENTRY MODEL, 2026-08-26).
func planLevelFacts(traderID, symbol string, doc kernel.PlanDoc, now time.Time, rule string, owners map[int64]*store.OwnerLevelDB, birthMs int64) ([]gin.H, float64, []kernel.FvgScenarioState) {
	if market.FuturesBarsProvider == nil {
		return nil, 0, nil
	}
	bars := market.FuturesBarsProvider(symbol, "1m", kernel.AISVPBarCount)
	if len(bars) == 0 {
		return nil, 0, nil
	}
	// W3 (2026-09-09) — the merged map the MODEL is shown, so the card shows the
	// same thing (D6). Built once per request from these same bars. Enrichment is
	// OPTIONAL: when the map cannot be built the extra keys are simply absent and
	// the card renders nothing extra, rather than a fabricated role or a 0 ATR.
	w3Map := planW3Map(traderID, symbol, bars, now)
	nowMs := now.UnixMilli()
	price := 0.0
	for i := len(bars) - 1; i >= 0; i-- {
		if bars[i].CloseTime < nowMs {
			price = bars[i].Close
			break
		}
	}
	// Zero-reference guard (2026-08-24): right after a plan write, the only
	// bars may be forming ones, leaving price at 0 and every distance rendered
	// as the negative of the level price. Fall back to the latest bar close.
	if price == 0 && len(bars) > 0 {
		price = bars[len(bars)-1].Close
	}
	out := make([]gin.H, 0, len(doc.Levels))
	for _, l := range doc.Levels {
		dir := kernel.DirAbove
		if l.Price < price {
			dir = kernel.DirBelow
		}
		f := kernel.EvaluateLevelFacts(kernel.BarsSince(bars, birthMs), l.Price, dir, rule, 3, nowMs)
		// Zero-reference regression guard (2026-08-26, "dist wrong again"): right
		// after a re-plan, birthMs ≈ now → BarsSince is EMPTY → referenceClose
		// returns 0 → DistancePoints = 0 − level (the card showed −29095 etc.).
		// Distance is a SNAPSHOT fact — compute it against the zero-guarded
		// current price directly; the sweep/acceptance fields keep the
		// birth-scoped bars.
		distance := f.DistancePoints
		if price > 0 {
			distance = kernel.SignedDistancePoints(price, l.Price)
		}
		row := gin.H{
			"price":         l.Price,
			"label":         l.Label,
			"grade":         l.Grade,
			"machine_grade": l.MachineGrade,
			"instruction":   l.Instruction,
			"distance":      distance,
			// T4 (2026-08-26) — live touch chip state for the card row
			// (approaching | touching | rejected | accepted | "").
			"touch_state":   kernel.TouchStateForCard(traderID, symbol, l.Label, l.Price, price, now.UnixMilli()),
			"sweep":         f.Swept,
			"closes_beyond": maxI(f.ClosesBeyondUp, f.ClosesBeyondDown),
			"accept_have":   f.AcceptHave,
			"accept_need":   f.AcceptNeed,
			"still_valid":   f.StillValid,
		}
		// W3 — merged names, the map role, the ATR distance and any projection
		// method, matched from the map by price. Absent when unmatched (A24).
		if mc, ok := kernel.MatchMapCandidate(w3Map, l.Price); ok {
			if len(mc.Names) > 1 {
				row["names"] = mc.Names
				row["merged_count"] = mc.MergedCount
			}
			if mc.Role != "" {
				row["map_role"] = string(mc.Role)
			}
			row["entry_candidate"] = mc.EntryCandidate
			if mc.RefusedReason != "" {
				row["not_entry_reason"] = mc.RefusedReason
			}
			if mc.Projection {
				row["projection"] = true
				row["projection_method"] = mc.ProjectionMethod
			}
			if mc.HasATR {
				row["distance_atr"] = mc.DistanceATR
			}
		}
		// OWNER PROVENANCE: the card renders 👤 / 📝 / an S-tag and the ⚡ conflict
		// chip from these three fields. They were typed as optional on the FE from
		// the start but never emitted, so the whole owner-attribution layer was
		// unreachable (design-conformance audit, D1/D6). Matched by price against
		// the sticky owner-level store, which is where note + scenario tag live.
		if ow, ok := owners[roundKey(l.Price)]; ok {
			row["origin"] = "OWNER"
			if ow.Note != "" {
				row["note"] = ow.Note
			}
			if ow.ScenarioTag != "" {
				row["scenario_id"] = ow.ScenarioTag
			}
		} else {
			row["origin"] = "AI"
		}
		out = append(out, row)
	}
	return out, price, kernel.FvgScenarioStatesFor(doc, bars, birthMs, now.UnixMilli())
}

// roundKey buckets a price to the instrument tick so an owner level entered as
// 30156 matches a plan level stored as 30156.00.
func roundKey(p float64) int64 { return int64(p*4 + 0.5) }

// handlePlanVersions GET /api/plan/versions?trader_id=xxx&session=NY[&trade_date=]
// — every stored version of ONE session's plan, oldest first, with the metadata
// the version chips need to be more than decoration.
//
// ITEM 15. The chips used to fabricate their labels from a single integer
// (Array.from({length: version})), so they could not say which versions existed,
// when they were written, why each one died, or what changed. All of that has
// been in the database since P3.6 — this is purely the missing read path.
func (s *Server) handlePlanVersions(c *gin.Context) {
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if _, err := s.traderManager.GetTrader(traderID); err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	session := strings.ToUpper(strings.TrimSpace(c.Query("session")))
	if session == "" {
		SafeBadRequest(c, "session is required")
		return
	}
	tradeDate := strings.TrimSpace(c.Query("trade_date"))
	if tradeDate == "" {
		now := time.Now()
		tradeDate = now.In(planChicago()).Format("2006-01-02")
		if def, found := s.planRegistry().SessionByName(session); found {
			if d, okD := kernel.PlanChainTradeDate(def, now); okD {
				tradeDate = d
			}
		}
	}

	rows, err := s.store.Plan().ListVersionsForTrader(tradeDate, session, traderID)
	if err != nil {
		SafeInternalError(c, "list plan versions", err)
		return
	}
	docs := make([]*kernel.PlanDoc, len(rows))
	for i, r := range rows {
		var d kernel.PlanDoc
		if json.Unmarshal([]byte(r.Doc), &d) == nil {
			docs[i] = &d
		}
	}

	out := make([]gin.H, 0, len(rows))
	for i, r := range rows {
		item := gin.H{
			"version":        r.Version,
			"lifecycle":      r.Lifecycle,
			"trigger_reason": r.TriggerReason,
			"created_at":     r.CreatedAt,
			"model_id":       r.ModelID,
			"degraded":       r.Degraded,
			"is_latest":      i == len(rows)-1,
		}
		if d := docs[i]; d != nil {
			item["level_count"] = len(d.Levels)
			item["scenario_count"] = len(d.Scenarios)
			item["bias"] = d.Bias.Direction
			// CLASS 50b — the stamped dual label (AI · tree · regime), a LABEL
			// not a direction; "" on pre-50b stored rows.
			item["bias_label"] = d.BiasLabel
			item["day_type"] = d.DayType
			item["death_condition"] = d.DeathCondition
			// WHY this version stopped being the plan: the successor's
			// trigger_reason is the record of what replaced it. A superseded
			// version whose successor says nothing still names its own stated
			// death condition, which is the checkable claim it was judged on.
			if i < len(rows)-1 {
				item["superseded_by"] = rows[i+1].Version
				reason := strings.TrimSpace(rows[i+1].TriggerReason)
				if reason == "" {
					reason = "replaced by v" + strconv.Itoa(rows[i+1].Version)
				}
				item["death_reason"] = reason
				item["diff_vs_next"] = planDocDiff(docs[i], docs[i+1])
			}
		}
		out = append(out, item)
	}
	c.JSON(200, gin.H{
		"trade_date": tradeDate, "session": session, "versions": out,
		"latest_version": func() int {
			if len(rows) == 0 {
				return 0
			}
			return rows[len(rows)-1].Version
		}(),
	})
}

// planDocDiff describes, in plain language, what changed between two plan
// versions. It is deliberately coarse — the point is for the owner to see at a
// glance WHY a re-plan happened, not to reconstruct a patch.
func planDocDiff(from, to *kernel.PlanDoc) []string {
	if from == nil || to == nil {
		return nil
	}
	var out []string
	if from.Bias.Direction != to.Bias.Direction {
		out = append(out, fmt.Sprintf("bias %s → %s", from.Bias.Direction, to.Bias.Direction))
	}
	if from.Bias.Conviction != to.Bias.Conviction {
		out = append(out, fmt.Sprintf("conviction %s → %s", from.Bias.Conviction, to.Bias.Conviction))
	}
	if from.DayType != to.DayType && to.DayType != "" {
		out = append(out, fmt.Sprintf("day type %s → %s", orNone(from.DayType), to.DayType))
	}

	had := map[int64]string{}
	for _, l := range from.Levels {
		had[roundKey(l.Price)] = l.Label
	}
	kept, added := 0, []string{}
	for _, l := range to.Levels {
		if _, ok := had[roundKey(l.Price)]; ok {
			kept++
			delete(had, roundKey(l.Price))
			continue
		}
		added = append(added, fmt.Sprintf("%s %g", l.Label, l.Price))
	}
	if len(added) > 0 {
		out = append(out, "added "+strings.Join(added, ", "))
	}
	if len(had) > 0 {
		dropped := make([]string, 0, len(had))
		for k, label := range had {
			dropped = append(dropped, fmt.Sprintf("%s %g", label, float64(k)/4))
		}
		sort.Strings(dropped)
		out = append(out, "dropped "+strings.Join(dropped, ", "))
	}
	if len(added) == 0 && len(had) == 0 && len(from.Levels) > 0 {
		out = append(out, fmt.Sprintf("same %d levels", kept))
	}
	if len(to.Levels) == 0 && len(from.Levels) > 0 {
		out = append(out, "NO LEVELS in the replacement")
	}
	if len(from.Scenarios) != len(to.Scenarios) {
		out = append(out, fmt.Sprintf("scenarios %d → %d", len(from.Scenarios), len(to.Scenarios)))
	}
	return out
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}

// handlePlanHistory GET /api/plan/history?trader_id=xxx — recent plan versions.
func (s *Server) handlePlanHistory(c *gin.Context) {
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if _, err := s.traderManager.GetTrader(traderID); err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	rows, err := s.store.Plan().ListRecentForTrader(traderID, 30)
	if err != nil {
		SafeInternalError(c, "list plans", err)
		return
	}
	hist := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		hist = append(hist, gin.H{
			"trade_date": r.TradeDate, "session": r.Session, "version": r.Version,
			"lifecycle": r.Lifecycle, "model_id": r.ModelID, "trigger_reason": r.TriggerReason,
		})
	}
	c.JSON(200, gin.H{"history": hist})
}

// handlePlanAlerts GET /api/plan/alerts?trader_id=xxx — the alert feed.
func (s *Server) handlePlanAlerts(c *gin.Context) {
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	rows, err := s.store.Alert().List(traderID, 50)
	if err != nil {
		SafeInternalError(c, "list alerts", err)
		return
	}
	unacked, _ := s.store.Alert().UnackedCount(traderID)
	c.JSON(200, gin.H{"alerts": rows, "unacked": unacked})
}

// handlePlanAlertAck POST /api/plan/alert-ack — ack an alert (query or JSON body).
func (s *Server) handlePlanAlertAck(c *gin.Context) {
	traderID := strings.TrimSpace(c.Query("trader_id"))
	var body struct {
		TraderID string `json:"trader_id"`
		AlertID  int64  `json:"alert_id"`
	}
	_ = c.ShouldBindJSON(&body)
	if traderID == "" {
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if body.AlertID <= 0 {
		SafeBadRequest(c, "alert_id is required")
		return
	}
	// Scope the ack to the caller's trader (IDOR guard) — 404 if not owned.
	found, err := s.store.Alert().AckForTrader(traderID, body.AlertID)
	if err != nil {
		SafeInternalError(c, "ack alert", err)
		return
	}
	if !found {
		SafeNotFound(c, "Alert")
		return
	}
	c.JSON(200, gin.H{"acked": true, "alert_id": body.AlertID})
}

// marketRef returns the last CLOSED price + the daily-ATR proxy from the live 1m
// bars (the B2 armor scale for owner-entered level prices). Both 0 when no bars.
func marketRef(symbol string, now time.Time) (lastPrice, dATR float64) {
	if market.FuturesBarsProvider == nil {
		return 0, 0
	}
	bars := market.FuturesBarsProvider(symbol, "1m", kernel.AISVPBarCount)
	if len(bars) == 0 {
		return 0, 0
	}
	nowMs := now.UnixMilli()
	for i := len(bars) - 1; i >= 0; i-- {
		if bars[i].CloseTime < nowMs {
			lastPrice = bars[i].Close
			break
		}
	}
	dATR = kernel.DailyRangeProxy(bars, now)
	return lastPrice, dATR
}

// mergedPriceViolations runs the B2 level armor over every price in the RESOLVED
// plan_final (level prices + scenario target-chain prices), so an implausible
// owner/planner price is rejected regardless of which patch shape introduced it
// (a whole-array /levels replace, a target_chain add, etc.) — armoring the result,
// not the patch text.
func mergedPriceViolations(doc *kernel.PlanDoc, lastPrice, dATR float64) []string {
	var out []string
	check := func(p float64) {
		if reason, bad := kernel.LevelPriceViolation(p, lastPrice, dATR); bad {
			out = append(out, reason)
		}
	}
	for _, l := range doc.Levels {
		check(l.Price)
	}
	for _, s := range doc.Scenarios {
		for _, t := range s.TargetChain {
			check(t)
		}
	}
	return out
}

// handlePlanOverlay POST /api/plan/overlay — append an RFC-6902 overlay to the
// active plan. test-op concurrency (§42): the patch is applied strictly to the
// current plan_final; a failed `test` or bad op → 409. Owner prices pass B2 armor.
// Origin defaults to "owner"; the Ask-Planner Apply passes "planner-revised".
func (s *Server) handlePlanOverlay(c *gin.Context) {
	var body struct {
		TraderID string `json:"trader_id"`
		Symbol   string `json:"symbol"`
		Patch    string `json:"patch"`  // JSON array of RFC-6902 ops
		Origin   string `json:"origin"` // owner | planner-revised (default owner)
	}
	_ = c.ShouldBindJSON(&body)
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	at, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	// Only a day-plan-owning trader may mutate the (session-global) day plan — a
	// crypto / plan-off trader must not edit the futures plan.
	if !traderDayPlanEnabled(at) {
		SafeForbidden(c, "day_plan is not enabled for this trader")
		return
	}
	if strings.TrimSpace(body.Patch) == "" {
		SafeBadRequest(c, "patch is required")
		return
	}
	origin := "owner"
	if body.Origin == "planner-revised" {
		origin = "planner-revised"
	}
	symbol := strings.TrimSpace(body.Symbol)
	if symbol == "" {
		symbol = "MNQ"
	}

	// UI-verification (2026-08-18): the edit sheet rides note + scenario_tag on
	// the replace value for OWNER-origin levels. The plan doc has no such fields
	// — strip them from the stored patch and write them through to the sticky
	// owner row (price identity + label) after a successful apply, so editing a
	// note can never be a silent no-op and the overlay history stays schema-pure.
	type opShape struct {
		Op    string         `json:"op"`
		Path  string         `json:"path"`
		Value map[string]any `json:"value,omitempty"`
	}
	var ops []opShape
	cleanPatch := body.Patch
	type writeThrough struct {
		price float64
		label string
		note  string
		tag   string
	}
	var throughs []writeThrough
	if json.Unmarshal([]byte(body.Patch), &ops) == nil {
		for i := range ops {
			if ops[i].Op != "replace" || !strings.HasPrefix(ops[i].Path, "/levels/") {
				continue
			}
			note, hasNote := ops[i].Value["note"].(string)
			tag, hasTag := ops[i].Value["scenario_tag"].(string)
			if !hasNote && !hasTag {
				continue
			}
			var price float64
			switch p := ops[i].Value["price"].(type) {
			case float64:
				price = p
			case json.Number:
				price, _ = p.Float64()
			}
			label, _ := ops[i].Value["label"].(string)
			throughs = append(throughs, writeThrough{price: price, label: label, note: note, tag: tag})
			delete(ops[i].Value, "note")
			delete(ops[i].Value, "scenario_tag")
		}
		if len(throughs) > 0 {
			if b, mErr := json.Marshal(ops); mErr == nil {
				cleanPatch = string(b)
			}
		}
	}

	overlayVersion, planVersion, code, msg := s.applyPlanOverlay(traderID, symbol, cleanPatch, origin, time.Now())
	if code != 0 {
		c.JSON(code, gin.H{"error": msg})
		return
	}
	for _, wt := range throughs {
		if wt.price > 0 && wt.label != "" {
			_, _ = s.store.OwnerLevel().UpdateNoteTag(c.GetString("user_id"), symbol, wt.price, wt.label, wt.note, wt.tag)
		}
	}
	c.JSON(200, gin.H{"overlay_version": overlayVersion, "plan_version": planVersion, "origin": origin})
}

// applyPlanOverlay resolves the active plan, applies patchJSON strictly onto the
// current plan_final (test-op concurrency guard), B2-armors every owner price,
// validates enums/counts, then appends the overlay. Shared by the overlay POST
// and the Ask-Planner Apply. Returns (overlayVersion, planVersion, httpCode, msg);
// httpCode==0 means success.
func (s *Server) applyPlanOverlay(traderID, symbol, patchJSON, origin string, now time.Time) (int, int, int, string) {
	if strings.TrimSpace(patchJSON) == "" {
		return 0, 0, 400, "patch is required"
	}
	// Atomic read-fold-test-append (test-op concurrency is only a guard if the
	// span can't interleave with another writer).
	planOverlayMu.Lock()
	defer planOverlayMu.Unlock()
	reg := s.planRegistry()
	sess, ok := reg.ActiveSession(now)
	// P1 — enablement via the trader's sessionRunnable (never the raw registry
	// flag): an edit must be allowed for a session the bot actually runs.
	runnable := true
	if s.traderManager != nil {
		if at, aErr := s.traderManager.GetTrader(traderID); aErr == nil && at != nil {
			if okR, _ := at.SessionRunnable(sess); !okR {
				runnable = false
			}
		}
	}
	if !ok || !runnable {
		return 0, 0, 400, "no active plan to edit (night / disabled session)"
	}
	tradeDate := now.In(planChicago()).Format("2006-01-02")
	if d, okD := kernel.PlanChainTradeDate(sess, now); okD {
		tradeDate = d
	}
	row, err := s.store.Plan().GetLatestPlanForTraderSession(tradeDate, sess.Name, traderID)
	if err != nil || row == nil {
		return 0, 0, 404, "active plan not found"
	}
	overlays, _ := s.store.Plan().ListOverlays(row.PlanID, row.Version)
	// W-EXEC-TRUTH W5 — `current` folds USER overlays only: a machine (Picture)
	// scenario folds after every user patch, so an owner or planner-revised
	// patch is judged against the doc it can actually reach, and its indexes
	// never point at a machine scenario.
	patches := make([]string, 0, len(overlays))
	for _, ov := range overlays {
		if !kernel.IsMachineOverlayOrigin(ov.Origin) {
			patches = append(patches, ov.Patch)
		}
	}
	current, _ := kernel.ApplyOverlayPatches([]byte(row.Doc), patches)
	candidate, err := kernel.ApplyPatchStrict(current, patchJSON)
	if err != nil {
		return 0, 0, 409, "overlay rejected: " + err.Error() // test-op / validity conflict
	}
	var merged kernel.PlanDoc
	if json.Unmarshal(candidate, &merged) != nil || kernel.ValidatePlanDocWithCaps(&merged, kernel.PlanHardMaxLevels, kernel.PlanHardMaxScenarios) != nil {
		return 0, 0, 400, "patch produces an invalid plan (enum/count/sign armor)"
	}
	// W5 (D18) — AI commentary cannot rewrite Picture evidence: an edit that
	// adds, alters or removes a machine scenario is refused, judged on the
	// user fold (a machine plan's own P scenarios live in its base doc) AND on
	// the full fold with the machine overlays appended after both sides.
	if msg := machineScenarioEditRefusal(row, overlays, current, merged, patchJSON, origin); msg != "" {
		return 0, 0, 409, msg
	}
	// B2 price armor — sweep the RESULTING plan_final's prices (levels +
	// scenario targets), robust to every patch shape (/levels, /levels/N,
	// /levels/-, a whole-array /levels replace, /scenarios/N/target_chain/-…).
	lastPrice, dATR := marketRef(symbol, now)
	if v := mergedPriceViolations(&merged, lastPrice, dATR); len(v) > 0 {
		return 0, 0, 422, "⛔ price armor: " + strings.Join(v, "; ")
	}
	ov := &store.PlanOverlayDB{
		OverlayID:   fmt.Sprintf("%s:o:%d", row.PlanID, now.UnixNano()),
		PlanID:      row.PlanID,
		PlanVersion: row.Version,
		Patch:       patchJSON,
		Origin:      origin,
	}
	overlayVersion, err := s.store.Plan().AppendOverlay(ov)
	if err != nil {
		return 0, 0, 500, "append overlay: " + err.Error()
	}
	return overlayVersion, row.Version, 0, ""
}

// machineScenarioEditRefusal is applyPlanOverlay's D18 check: "" when the
// candidate leaves every machine scenario exactly as recorded.
func machineScenarioEditRefusal(row *store.PlanDB, overlays []*store.PlanOverlayDB, current []byte, merged kernel.PlanDoc, patchJSON, origin string) string {
	var cur kernel.PlanDoc
	if err := json.Unmarshal(current, &cur); err != nil {
		return "overlay refused: the current plan does not parse, so a machine (Picture) scenario cannot be proven untouched"
	}
	if err := kernel.MachineScenariosPreserved(cur, merged); err != nil {
		return "overlay refused: " + err.Error()
	}
	refs := kernel.OverlayRefsFrom(overlays)
	next := 1
	for _, r := range refs {
		if r.Version >= next {
			next = r.Version + 1
		}
	}
	// The FULL-fold comparison (machine overlays appended after both sides)
	// is the CTO spec's literal form. With today's ValidateMachineScenario —
	// a machine overlay's validity reads only ids, refs and the doc's levels —
	// every violation it can see is refused first by the user-fold check above
	// (mutation A29 in the W5 builder-A report survives for exactly that
	// reason [B]). It stays as the guard for the day a machine overlay's fold
	// comes to depend on something a user patch can change.
	before, err := kernel.ResolvePlanFinal([]byte(row.Doc), refs)
	if err != nil {
		return "overlay refused: the stored plan does not parse, so a machine (Picture) scenario cannot be proven untouched"
	}
	after, err := kernel.ResolvePlanFinal([]byte(row.Doc), append(append([]kernel.OverlayRef(nil), refs...), kernel.OverlayRef{Version: next, Origin: origin, Patch: patchJSON}))
	if err != nil {
		return "overlay refused: the stored plan does not parse, so a machine (Picture) scenario cannot be proven untouched"
	}
	if err := kernel.MachineScenariosPreserved(before.Doc, after.Doc); err != nil {
		return "overlay refused: " + err.Error()
	}
	return ""
}

// handlePlanAlertDismiss POST /api/plan/alert-dismiss — ITEM 5a. Hides ONE alert
// from the owner's feed. Trader-scoped like the ack (IDOR guard): 404 on an
// alert the caller does not own. An UNACKNOWLEDGED P0 refuses — the persistent
// banner exists so a halt cannot be swiped away unseen.
func (s *Server) handlePlanAlertDismiss(c *gin.Context) {
	var body struct {
		TraderID string `json:"trader_id"`
		AlertID  int64  `json:"alert_id"`
	}
	_ = c.ShouldBindJSON(&body)
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if body.AlertID <= 0 {
		SafeBadRequest(c, "alert_id is required")
		return
	}
	found, err := s.store.Alert().DismissForTrader(traderID, body.AlertID, time.Now().Unix())
	if errors.Is(err, store.ErrP0NotAcked) {
		c.JSON(409, gin.H{"error": err.Error(), "needs_ack": true})
		return
	}
	if err != nil {
		SafeInternalError(c, "dismiss alert", err)
		return
	}
	if !found {
		SafeNotFound(c, "Alert")
		return
	}
	c.JSON(200, gin.H{"dismissed": true, "alert_id": body.AlertID})
}

// handlePlanAlertClearRead POST /api/plan/alert-clear-read — ITEM 5b. Clears
// every ACKNOWLEDGED alert in one tap, leaving unacknowledged ones (especially
// P0) in place.
func (s *Server) handlePlanAlertClearRead(c *gin.Context) {
	var body struct {
		TraderID string `json:"trader_id"`
	}
	_ = c.ShouldBindJSON(&body)
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	n, err := s.store.Alert().DismissAckedForTrader(traderID, time.Now().Unix())
	if err != nil {
		SafeInternalError(c, "clear read alerts", err)
		return
	}
	c.JSON(200, gin.H{"cleared": n})
}

// handlePlanRereadStatus GET /api/plan/reread?trader_id=xxx — may the owner force
// a fresh planner read right now, and what would it cost? Read-only.
func (s *Server) handlePlanRereadStatus(c *gin.Context) {
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	at, err := s.traderManager.GetTrader(traderID)
	if err != nil || at == nil {
		SafeNotFound(c, "Trader")
		return
	}
	c.JSON(200, at.CanForceReread(time.Now()))
}

// handlePlanReread POST /api/plan/reread — ITEM 3, the owner's manual escape
// hatch. SPENDS one re-plan from the same budget the automatic path uses, so it
// can never talk the bot past its own limits, and writes through the normal path
// with trigger_reason "owner_reread" so the history shows who asked.
func (s *Server) handlePlanReread(c *gin.Context) {
	var body struct {
		TraderID string `json:"trader_id"`
	}
	_ = c.ShouldBindJSON(&body)
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	at, err := s.traderManager.GetTrader(traderID)
	if err != nil || at == nil {
		SafeNotFound(c, "Trader")
		return
	}
	// The trader re-checks eligibility itself; the FE's earlier check may be stale.
	gate, rErr := at.ForceReread(time.Now())
	if rErr != nil {
		c.JSON(409, gin.H{"error": gate.Reason, "gate": gate})
		return
	}
	c.JSON(200, gin.H{"ok": true, "gate": gate})
}

// handlePlanResetStatus GET /api/plan/reset?trader_id=xxx — may the owner reset
// this session's plan chain right now, and what does the reset do? Read-only.
func (s *Server) handlePlanResetStatus(c *gin.Context) {
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	at, err := s.traderManager.GetTrader(traderID)
	if err != nil || at == nil {
		SafeNotFound(c, "Trader")
		return
	}
	c.JSON(200, at.CanForceReset(time.Now()))
}

// handlePlanReset POST /api/plan/reset — P6, the owner reset. Abandons the
// current chain (history + death reasons preserved), re-arms the re-plan budget,
// clears NO-TRADE, and runs a fresh planner read (trigger_reason "owner reset")
// through the normal fail-closed path. Plan rows + one marker only — positions,
// brackets, guardrail counters and the daily cage are never touched.
func (s *Server) handlePlanReset(c *gin.Context) {
	var body struct {
		TraderID string `json:"trader_id"`
	}
	_ = c.ShouldBindJSON(&body)
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	at, err := s.traderManager.GetTrader(traderID)
	if err != nil || at == nil {
		SafeNotFound(c, "Trader")
		return
	}
	// The trader re-checks eligibility itself; the FE's earlier check may be stale.
	gate, rErr := at.ForceReset(time.Now())
	if rErr != nil {
		c.JSON(409, gin.H{"error": gate.Reason, "gate": gate})
		return
	}
	c.JSON(200, gin.H{"ok": true, "gate": gate})
}

// transitionState reads the G4 stand-down mirror for this plan version. Absent
// (the normal case) → nil, and the card shows no chip.
func (s *Server) transitionState(planID string, version int) *kernel.TransitionState {
	if s.store == nil {
		return nil
	}
	raw, _ := s.store.GetSystemConfig(store.TransitionKey(planID, version))
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out kernel.TransitionState
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	if !out.Active {
		return nil
	}
	return &out
}

// uncarriedEdits reads the review list a re-plan parked for this version.
// Absent (the normal case) → nil, and the card shows nothing.
func (s *Server) uncarriedEdits(planID string, version int) []kernel.UncarriedEdit {
	if s.store == nil {
		return nil
	}
	raw, _ := s.store.GetSystemConfig(store.UncarriedEditsKey(planID, version))
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []kernel.UncarriedEdit
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}

// ── ITEM 2 — Ask-Planner context resolution ────────────────────────────────
//
// The thread used to be hard-gated on an ACTIVE plan (400 on night/disabled,
// 404 when no row existed), which locked it at exactly the moment it is most
// useful. It now degrades through three clearly-labelled contexts instead.

const (
	askContextActive     = "active"     // a live plan for the live session
	askContextHistorical = "historical" // the most recent stored plan, possibly dead
	askContextNoPlan     = "no-plan"    // nothing stored — market facts only
)

type askContext struct {
	kind       string
	label      string
	tradeDate  string
	session    string
	planID     string
	row        *store.PlanDB
	doc        kernel.PlanDoc
	planBlock  string
	liveStatus string
}

// resolveAskContext never fails: worst case it returns a no-plan context carrying
// only live market facts, which is still a useful thing to ask about.
func (s *Server) resolveAskContext(traderID, symbol string, now time.Time) askContext {
	reg := s.planRegistry()
	ctx := askContext{kind: askContextNoPlan, tradeDate: now.In(planChicago()).Format("2006-01-02")}

	// 1) The live session's plan, if there is one.
	// P1 — resolve enablement through the trader's sessionRunnable; the raw
	// registry flag used to demote a strategy-enabled session to 'historical'.
	if sess, ok := reg.ActiveSession(now); ok {
		runnable := true
		if s.traderManager != nil {
			if at, aErr := s.traderManager.GetTrader(traderID); aErr == nil && at != nil {
				if okR, _ := at.SessionRunnable(sess); !okR {
					runnable = false
				}
			}
		}
		if !runnable {
			return ctx
		}
		ctx.session = sess.Name
		tradeDate := ctx.tradeDate
		if d, okD := kernel.PlanChainTradeDate(sess, now); okD {
			tradeDate = d
		}
		ctx.tradeDate = tradeDate
		if row, err := s.store.Plan().GetLatestPlanForTraderSession(tradeDate, sess.Name, traderID); err == nil && row != nil {
			ctx.row, ctx.planID = row, row.PlanID
			if doc, okDoc := s.resolvePlanFinal(row); okDoc {
				ctx.doc = doc
				// A dead / no-trade row is NOT the live plan the executor follows.
				if row.Lifecycle == "active" {
					ctx.kind = askContextActive
				} else {
					ctx.kind = askContextHistorical
				}
			}
		}
	}

	// 2) Nothing for the live session → the most recent stored plan, even dead.
	if ctx.row == nil {
		if rows, err := s.store.Plan().ListRecent(1); err == nil && len(rows) > 0 {
			row := rows[0]
			ctx.row, ctx.planID = row, row.PlanID
			ctx.session, ctx.tradeDate = row.Session, row.TradeDate
			if doc, okDoc := s.resolvePlanFinal(row); okDoc {
				ctx.doc = doc
				ctx.kind = askContextHistorical
			}
		}
	}

	// Label + context blocks.
	switch ctx.kind {
	case askContextActive:
		ctx.label = fmt.Sprintf("ACTIVE PLAN — %s v%d", ctx.session, ctx.row.Version)
		ctx.planBlock = kernel.RenderPlanBlock(ctx.doc, ctx.session)
	case askContextHistorical:
		lifecycle := ""
		if ctx.row != nil {
			lifecycle = strings.ToUpper(ctx.row.Lifecycle)
		}
		ctx.label = fmt.Sprintf("HISTORICAL CONTEXT — %s %s v%d (%s), NOT the live plan",
			ctx.tradeDate, ctx.session, ctx.row.Version, lifecycle)
		var b strings.Builder
		fmt.Fprintf(&b, "# CONTEXT: THIS IS NOT A LIVE PLAN\nYou are answering about a STORED plan that is no longer active (%s %s v%d, lifecycle %s).\nThere is nothing to patch. Explain what happened; do NOT propose changes.\n",
			ctx.tradeDate, ctx.session, ctx.row.Version, ctx.row.Lifecycle)
		if r := strings.TrimSpace(ctx.row.TriggerReason); r != "" {
			fmt.Fprintf(&b, "why this version was written: %s\n", r)
		}
		if r := strings.TrimSpace(ctx.doc.Reasoning); r != "" {
			fmt.Fprintf(&b, "the plan's own reasoning: %s\n", r)
		}
		if r := strings.TrimSpace(ctx.doc.DeathCondition); r != "" {
			fmt.Fprintf(&b, "its stated death condition: %s\n", r)
		}
		b.WriteString("\n")
		b.WriteString(kernel.RenderPlanBlock(ctx.doc, ctx.session))
		ctx.planBlock = b.String()
	default:
		ctx.label = "NO PLAN — market facts only"
		ctx.planBlock = "# CONTEXT: NO PLAN EXISTS\nNo plan is stored for this session. Answer from the live market facts below.\nThere is nothing to patch; do NOT propose changes.\n"
	}

	// Live facts are attached in EVERY context — they are the part that is always
	// true, and the no-plan case has nothing else.
	if market.FuturesBarsProvider != nil {
		if bars := market.FuturesBarsProvider(symbol, "1m", kernel.AISVPBarCount); len(bars) > 0 {
			price, dATR := marketRef(symbol, now)
			rule, _, left := s.planRules(traderID, ctx.session, ctx.tradeDate)
			ctx.liveStatus = kernel.RenderPlanStatus(traderID, symbol, ctx.doc, bars, price, dATR, rule, left, now.UnixMilli(), ctx.row.CreatedAt.UnixMilli())
		}
	}
	if ctx.planID == "" {
		ctx.planID = s.store.Plan().ResolvePlanID(ctx.tradeDate, ctx.session, traderID)
	}
	return ctx
}

// handlePlanAsk POST /api/plan/ask — a plan-scoped Q&A with the planner. The
// verbatim anti-sycophancy contract is the system prompt AND enforced in code
// (kernel.ParsePlannerReply): a bare disagreement never yields a patch. The owner
// question + structured reply are persisted; the verdict is logged for the KPI.
func (s *Server) handlePlanAsk(c *gin.Context) {
	var body struct {
		TraderID string `json:"trader_id"`
		Symbol   string `json:"symbol"`
		Question string `json:"question"`
	}
	_ = c.ShouldBindJSON(&body)
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	at, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	if strings.TrimSpace(body.Question) == "" {
		SafeBadRequest(c, "question is required")
		return
	}
	symbol := strings.TrimSpace(body.Symbol)
	if symbol == "" {
		symbol = "MNQ"
	}

	// ITEM 2 (2026-08-17) — ASKING IS ALLOWED WITH NO ACTIVE PLAN.
	//
	// This used to 400 on night/disabled and 404 when no plan row existed, which
	// locked the thread at exactly the moment the owner most wants it: "why did
	// the plan die?", "why no levels tonight?". The thread now opens scoped to
	// whatever DOES exist — the most recent stored plan, even dead — and says so.
	now := time.Now()
	askCtx := s.resolveAskContext(traderID, symbol, now)
	tradeDate, sessName, row, doc := askCtx.tradeDate, askCtx.session, askCtx.row, askCtx.doc

	userPrompt := kernel.BuildAskPlannerUserPrompt(askCtx.planBlock, askCtx.liveStatus, body.Question)

	// Call the SAME planner model that authored the plan.
	client, _ := at.ResolvePlannerClient()
	if client == nil && !(SandboxMode() && !SandboxRealLLM()) {
		SafeInternalError(c, "resolve planner client", fmt.Errorf("no planner client"))
		return
	}
	var raw string
	if SandboxMode() && !SandboxRealLLM() {
		raw = sandboxPlannerRaw(userPrompt) // canned, zero cost
	} else {
		var callErr error
		raw, callErr = client.CallWithMessages(kernel.AskPlannerSystemPrompt, userPrompt)
		if callErr != nil {
			SafeInternalError(c, "ask planner", callErr)
			return
		}
	}
	reply, err := kernel.ParsePlannerReply(raw)
	if err != nil {
		c.JSON(502, gin.H{"error": "planner reply malformed", "raw": raw})
		return
	}
	// CODE-ENFORCED, not prompt-enforced: a proposal only exists against an ACTIVE
	// plan. Outside that there is nothing to patch, so any patch the model emits is
	// discarded here rather than trusted to the prompt or hidden by the UI.
	if askCtx.kind != askContextActive {
		reply.Patch = nil
		if reply.Verdict == "PROPOSE-MERGE" {
			reply.Verdict = "DEFEND"
		}
	}

	// Persist the owner question + the structured planner reply (verdict logged).
	_, _ = s.store.PlanQA().Append(&store.PlanQADB{
		TraderID: traderID, PlanID: askCtx.planID, TradeDate: tradeDate, Session: sessName,
		Role: "owner", Content: body.Question, ContextType: askCtx.kind, CreatedAt: now.Unix(),
	})
	patchStr := ""
	if len(reply.Patch) > 0 {
		patchStr = string(reply.Patch)
	}
	qaID, _ := s.store.PlanQA().Append(&store.PlanQADB{
		TraderID: traderID, PlanID: askCtx.planID, TradeDate: tradeDate, Session: sessName,
		Role: "planner", Content: reply.Summary, Evidence: reply.Evidence,
		PointClass: reply.PointClass, Verdict: reply.Verdict, Patch: patchStr,
		ContextType: askCtx.kind,
		CreatedAt:   now.Unix() + 1, // keep the reply after the question in id/time order
	})

	planVersion := 0
	if row != nil {
		planVersion = row.Version
	}
	_ = doc
	c.JSON(200, gin.H{
		"qa_id":        qaID,
		"plan_id":      askCtx.planID,
		"plan_version": planVersion,
		// The owner must never mistake a historical/no-plan answer for one about
		// the live plan.
		"context_type":  askCtx.kind,
		"context_label": askCtx.label,
		"reply": gin.H{
			"evidence": reply.Evidence, "point_class": reply.PointClass,
			"verdict": reply.Verdict, "summary": reply.Summary, "patch": patchStr,
		},
	})
}

// handlePlanThread GET /api/plan/ask?trader_id=xxx — the plan's Q&A thread + the
// sycophancy KPI (verdict counts).
func (s *Server) handlePlanThread(c *gin.Context) {
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if _, err := s.traderManager.GetTrader(traderID); err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	planID := strings.TrimSpace(c.Query("plan_id"))
	if planID == "" {
		// default to today's active plan id
		now := time.Now()
		reg := s.planRegistry()
		tradeDate := now.In(planChicago()).Format("2006-01-02")
		if sess, ok := reg.ActiveSession(now); ok {
			if d, okD := kernel.PlanChainTradeDate(sess, now); okD {
				tradeDate = d
			}
			planID = s.store.Plan().ResolvePlanID(tradeDate, sess.Name, traderID)
		}
	}
	msgs, _ := s.store.PlanQA().ListForPlan(traderID, planID, 200)
	kpi, _ := s.store.PlanQA().VerdictStats(traderID)
	c.JSON(200, gin.H{"thread": msgs, "kpi": kpi})
}

// handlePlanAskApply POST /api/plan/ask/apply — apply a PROPOSE-MERGE patch as an
// overlay (origin planner-revised) and mark the reply applied. Bare-disagreement
// replies carry no patch, so there is nothing to apply (guarded here too).
// handlePlanAskDecline POST /api/plan/ask/decline — the owner declines a
// PROPOSE-MERGE proposal.
//
// W16/R2. Declining used to be pure local React state: the panel set the same
// flag Apply sets, so the card claimed "Applied — card updated" while NOTHING
// was persisted. Two things were wrong with that. The copy lied, and the KPI
// series lost half its signal: a proposal the owner explicitly REJECTED and one
// they simply never answered both sat at applied=false, indistinguishable.
//
// The decline is recorded as an OWNER-role row carrying the same plan/trigger
// provenance as the planner reply it answers, with verdict DECLINED. Owner rows
// are excluded from the planner-reply counters (VerdictStats filters
// role='planner'), so DEFEND/CONCEDE/PROPOSE-MERGE keep their existing meaning
// and the decline count joins the same series without redefining it.
//
// The plan is never touched — declining is the absence of a mutation, by design.
func (s *Server) handlePlanAskDecline(c *gin.Context) {
	var body struct {
		TraderID string `json:"trader_id"`
		QaID     int64  `json:"qa_id"`
	}
	_ = c.ShouldBindJSON(&body)
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if _, err := s.traderManager.GetTrader(traderID); err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	if body.QaID <= 0 {
		SafeBadRequest(c, "qa_id is required")
		return
	}
	msg, err := s.store.PlanQA().GetForTrader(traderID, body.QaID)
	if err != nil {
		SafeInternalError(c, "get qa message", err)
		return
	}
	if msg == nil {
		SafeNotFound(c, "Ask-Planner message")
		return
	}
	if msg.Verdict != "PROPOSE-MERGE" {
		SafeBadRequest(c, "this reply has no proposal to decline")
		return
	}
	// An applied proposal cannot then be declined — the overlay already exists.
	if msg.Applied {
		c.JSON(200, gin.H{"declined": false, "already_applied": true})
		return
	}
	// Idempotent: a re-tap must not stack duplicate decline rows.
	if n, _ := s.store.PlanQA().CountOwnerDeclines(traderID, msg.PlanID, body.QaID); n > 0 {
		c.JSON(200, gin.H{"declined": true, "already_declined": true})
		return
	}
	id, aerr := s.store.PlanQA().Append(&store.PlanQADB{
		TraderID:      traderID,
		PlanID:        msg.PlanID,
		TradeDate:     msg.TradeDate,
		Session:       msg.Session,
		Role:          "owner",
		Content:       store.DeclineContentFor(body.QaID),
		Verdict:       store.VerdictDeclined,
		Trigger:       msg.Trigger,       // same provenance as the reply it answers
		ChallengeType: msg.ChallengeType, // …so the series stays comparable
		Applied:       false,
	})
	if aerr != nil {
		SafeInternalError(c, "record decline", aerr)
		return
	}
	logger.Infof("🙅 Ask-Planner proposal qa_id=%d DECLINED by owner (plan %s) — plan untouched.", body.QaID, msg.PlanID)
	c.JSON(200, gin.H{"declined": true, "qa_id": id})
}

func (s *Server) handlePlanAskApply(c *gin.Context) {
	var body struct {
		TraderID string `json:"trader_id"`
		Symbol   string `json:"symbol"`
		QaID     int64  `json:"qa_id"`
	}
	_ = c.ShouldBindJSON(&body)
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if _, err := s.traderManager.GetTrader(traderID); err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	if body.QaID <= 0 {
		SafeBadRequest(c, "qa_id is required")
		return
	}
	msg, err := s.store.PlanQA().GetForTrader(traderID, body.QaID)
	if err != nil {
		SafeInternalError(c, "get qa message", err)
		return
	}
	if msg == nil {
		SafeNotFound(c, "Ask-Planner message")
		return
	}
	if msg.Verdict != "PROPOSE-MERGE" || strings.TrimSpace(msg.Patch) == "" {
		SafeBadRequest(c, "this reply has no patch to apply")
		return
	}
	// One reply = one overlay: a re-tap / retry must not stack duplicate overlays.
	if msg.Applied {
		c.JSON(200, gin.H{"applied": true, "already_applied": true})
		return
	}
	// Bind the apply to the plan the reply was authored against: a PROPOSE-MERGE
	// from an earlier (rolled/expired) plan must not silently patch a different
	// active plan.
	now := time.Now()
	reg := s.planRegistry()
	tradeDate := now.In(planChicago()).Format("2006-01-02")
	sess, ok := reg.ActiveSession(now)
	// P1 — sessionRunnable, not the raw registry flag.
	runnable := true
	if s.traderManager != nil {
		if at, aErr := s.traderManager.GetTrader(traderID); aErr == nil && at != nil {
			if okR, _ := at.SessionRunnable(sess); !okR {
				runnable = false
			}
		}
	}
	legacy := store.MakePlanID(tradeDate, sess.Name)
	scoped := store.MakePlanIDForTrader(traderID, tradeDate, sess.Name)
	if !ok || !runnable || (msg.PlanID != legacy && msg.PlanID != scoped) {
		c.JSON(409, gin.H{"error": "this reply was authored against a plan that is no longer active"})
		return
	}
	symbol := strings.TrimSpace(body.Symbol)
	if symbol == "" {
		symbol = "MNQ"
	}
	overlayVersion, planVersion, code, emsg := s.applyPlanOverlay(traderID, symbol, msg.Patch, "planner-revised", now)
	if code != 0 {
		c.JSON(code, gin.H{"error": emsg})
		return
	}
	_ = s.store.PlanQA().MarkApplied(traderID, body.QaID)
	c.JSON(200, gin.H{"applied": true, "overlay_version": overlayVersion, "plan_version": planVersion})
}

// handlePlanTrades GET /api/plan/trades?trader_id=xxx — graded closed trades (the
// trade-review feed) + an adherence summary (counts + GPA). Grade is SEPARATE
// from P&L; both are shown so discipline and outcome can be compared.
func (s *Server) handlePlanTrades(c *gin.Context) {
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if _, err := s.traderManager.GetTrader(traderID); err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	rows, err := s.store.Position().GetGradedClosedPositions(traderID, 50)
	if err != nil {
		SafeInternalError(c, "list graded trades", err)
		return
	}
	trades := make([]gin.H, 0, len(rows))
	grades := make([]string, 0, len(rows))
	for _, p := range rows {
		grades = append(grades, p.AdherenceGrade)
		t := gin.H{
			"symbol": p.Symbol, "side": p.Side,
			"entry_price": p.EntryPrice, "exit_price": p.ExitPrice,
			"entry_time": p.EntryTime, "exit_time": p.ExitTime,
			// E4 (wave 1A) — mae/mfe are nullable: the payload carries null for
			// "never measured" rather than a 0 the card would render as a fact.
			"realized_pnl": p.RealizedPnL, "mae": p.MAE, "mfe": p.MFE,
			"entry_confidence":  p.EntryConfidence,
			"cited_scenario_id": p.CitedScenarioID, "plan_matched": p.PlanMatched,
			"plan_version":    p.PlanVersion,
			"adherence_grade": p.AdherenceGrade,
			"adherence_label": kernel.AdherenceLabel(p.AdherenceGrade),
			// W-EXEC-TRUTH W5 (F15) — every trade names its session plan and
			// its source. plan_id / plan_session are the position's own stamp
			// (written at entry); source is JOINED from the armed ledger row
			// whose signal is the position's entry order, never inferred.
			"plan_id": p.PlanID, "plan_session": p.PlanSession,
		}
		source, sourceRef, unresolved := s.tradeSource(traderID, p)
		t["source"] = source
		if sourceRef != "" {
			t["source_ref"] = sourceRef
		}
		if unresolved {
			t["source_unresolved"] = true
		}
		trades = append(trades, t)
	}
	c.JSON(200, gin.H{"trades": trades, "summary": kernel.SummarizeAdherence(grades)})
}

// tradeSource names where a trade came from (W5): the armed ledger row whose
// signal id is the position's entry order carries its source ("picture" for a
// Picture scenario, with the opportunity key as source_ref; a planner arm
// reads "armed_entry"). No armed row: a position the armed executor wrote
// still says "armed_entry"; the decision path's default position source
// ("system" / empty) reads "ai"; any other stored source (e.g. "snapshot", a
// reconcile row) is shown as stored — never relabelled. The inference runs
// ONLY when the ledger answered "no such row": a ledger that could not be read
// says so — source "unknown", unresolved true (W5 R7) — never a guess.
func (s *Server) tradeSource(traderID string, p *store.TraderPosition) (string, string, bool) {
	if id := strings.TrimSpace(p.EntryOrderID); id != "" && s.store != nil {
		row, err := s.store.ArmedOrders().FindBySignal(traderID, id)
		switch {
		case err == nil && row != nil:
			if row.Source != "" {
				return row.Source, row.SourceRef, false
			}
			return "armed_entry", "", false
		case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
			logger.Warnf("plan/trades: the armed ledger could not be read for position #%d (%v) — source unresolved, not inferred", p.ID, err)
			return "unknown", "", true
		}
	}
	switch strings.TrimSpace(p.Source) {
	case "armed_entry":
		return "armed_entry", "", false
	case "", "system":
		return "ai", "", false
	default:
		return p.Source, "", false
	}
}

// handlePlanStats GET /api/plan/stats?trader_id=xxx — the matched-random honesty
// gate. Returns the FROZEN weekly snapshot (the authoritative "beats random"
// verdict — never recomputed live, so no optional-stopping) + live progress
// counts (n/target) for the WARMING badges. Progress makes NO significance claim.
func (s *Server) handlePlanStats(c *gin.Context) {
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if _, err := s.traderManager.GetTrader(traderID); err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	mr := s.store.MatchedRandom()

	// The authoritative gate = the frozen weekly snapshot (may be null pre-first-eval).
	var weekly gin.H
	if row, _ := mr.LatestWeekly(); row != nil {
		var verdicts []kernel.TypeVerdict
		_ = json.Unmarshal([]byte(row.SummaryJSON), &verdicts)
		weekly = gin.H{"iso_week": row.ISOWeek, "computed_at": row.ComputedAt, "verdicts": verdicts}
	}

	// Live progress: raw counts only (WARMING framing) — no green claim here.
	progress := make([]gin.H, 0)
	if counts, err := mr.CountsByType(); err == nil {
		for t, cc := range counts {
			rate := 0.0
			if cc.Touches > 0 {
				rate = float64(cc.Reactions) / float64(cc.Touches)
			}
			progress = append(progress, gin.H{
				"level_type": t, "n": cc.Touches, "reactions": cc.Reactions,
				"target_n": kernel.PreRegisteredN, "react_rate": rate,
				"warming": cc.Touches < kernel.PreRegisteredN,
			})
		}
	}

	c.JSON(200, gin.H{
		"weekly":   weekly, // null until the first Sunday eval
		"progress": progress,
		"target_n": kernel.PreRegisteredN,
		"alpha":    kernel.BonferroniAlpha(),
	})
}

// handlePlanOwnerLevel POST /api/plan/owner-level — add a STICKY owner level
// (P3.6-C store). Guarded write: B2-armored price, WHERE-scoped by symbol, note +
// scenario tag ride along to the planner. Owner data is SACRED — never a live acct.
func (s *Server) handlePlanOwnerLevel(c *gin.Context) {
	var body struct {
		TraderID    string  `json:"trader_id"`
		Symbol      string  `json:"symbol"`
		Price       float64 `json:"price"`
		Label       string  `json:"label"`
		Note        string  `json:"note"`
		ScenarioTag string  `json:"scenario_tag"`
	}
	_ = c.ShouldBindJSON(&body)
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if _, err := s.traderManager.GetTrader(traderID); err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	// C1 — defense in depth (the group middleware now probes body trader_id too).
	if !s.traderOwnedBy(c.GetString("user_id"), traderID) {
		SafeNotFound(c, "Trader")
		return
	}
	symbol := strings.TrimSpace(body.Symbol)
	if symbol == "" {
		symbol = "MNQ"
	}
	if body.Price <= 0 {
		SafeBadRequest(c, "price is required")
		return
	}
	lastPrice, dATR := marketRef(symbol, time.Now())
	if reason, bad := kernel.LevelPriceViolation(body.Price, lastPrice, dATR); bad {
		c.JSON(422, gin.H{"error": "⛔ price armor: " + reason})
		return
	}
	lvl := &store.OwnerLevelDB{
		Symbol:      symbol,
		Price:       body.Price,
		Label:       strings.TrimSpace(body.Label),
		Note:        strings.TrimSpace(body.Note),
		ScenarioTag: strings.TrimSpace(body.ScenarioTag),
		UserID:      c.GetString("user_id"), // C2 — stamp the creator
		CreatedAt:   time.Now().Unix(),      // no autoCreateTime on this table
	}
	if err := s.store.OwnerLevel().Save(lvl); err != nil {
		SafeInternalError(c, "save owner level", err)
		return
	}
	c.JSON(200, gin.H{"id": lvl.ID, "symbol": symbol, "price": body.Price})
}

// handlePlanOwnerLevelDelete POST /api/plan/owner-level/delete — remove a sticky
// owner level by id (the edit sheet's Delete for a 👤 level).
func (s *Server) handlePlanOwnerLevelDelete(c *gin.Context) {
	var body struct {
		TraderID string `json:"trader_id"`
		ID       int64  `json:"id"`
	}
	_ = c.ShouldBindJSON(&body)
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if _, err := s.traderManager.GetTrader(traderID); err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	// C1 — defense in depth (the group middleware now probes body trader_id too).
	if !s.traderOwnedBy(c.GetString("user_id"), traderID) {
		SafeNotFound(c, "Trader")
		return
	}
	if body.ID <= 0 {
		SafeBadRequest(c, "id is required")
		return
	}
	if err := s.store.OwnerLevel().DeleteForUser(body.ID, c.GetString("user_id")); err != nil {
		SafeInternalError(c, "delete owner level", err)
		return
	}
	c.JSON(200, gin.H{"deleted": true, "id": body.ID})
}

// handlePlanOwnerLevels GET /api/plan/owner-levels?trader_id=<id>[&symbol=MNQ][&session=NY]
// — W-OWNER-LEVELS-API (2026-09-17). The sticky owner rows the POST above
// writes, listed. WHY: the edit sheet's "＋ Add level" POSTs a STICKY row that
// the planner only seats at its NEXT read (auto_trader_planner.go, the P3.6-C
// prepend), so the card showed nothing, the toast said "Plan updated", and
// there was no way to see or delete what was pending. This is the read side
// the card needs to render "pending — applies at next read" and offer Delete
// (the existing POST /plan/owner-level/delete takes the `id` returned here).
//
// Identifiers mirror the POST: trader_id (query or nothing else — a GET has no
// body) + symbol (default MNQ). Owner levels are NOT plan/session-scoped in
// the store (no plan_id / session / trade_date column — they persist across
// sessions until consumed or deleted), so no plan identifier is taken.
//
// status is judged at read time against the plan the card renders for the
// requested (or active) session: "applied" = that plan_final carries a level
// at this price (the SAME roundKey match planLevelFacts uses to stamp
// origin=OWNER on the card); "pending" otherwise, including when no plan row
// resolves. The store records only a `consumed` bool and NOTHING sets it in
// production (OwnerLevelStore.MarkConsumed has zero production callers), so
// there is no "which read consumed this row" to report: applied_version is
// deliberately ABSENT, never invented. `judged_against` names the plan the
// status was computed from, or null when there was none — a status with no
// referent is not "applied".
//
// Only ACTIVE rows (consumed=false, the ListActiveForUser set the planner and
// the card read) are listed: a consumed row is invisible to every reader and
// cannot apply again. An empty result is `[]`, never null.
func (s *Server) handlePlanOwnerLevels(c *gin.Context) {
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if _, err := s.traderManager.GetTrader(traderID); err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	// C1 — defense in depth (the group middleware probes ?trader_id too).
	if !s.traderOwnedBy(c.GetString("user_id"), traderID) {
		SafeNotFound(c, "Trader")
		return
	}
	symbol := strings.TrimSpace(c.Query("symbol"))
	if symbol == "" {
		symbol = "MNQ"
	}
	rows, err := s.store.OwnerLevel().ListActiveForUser(c.GetString("user_id"), symbol)
	if err != nil {
		SafeInternalError(c, "list owner levels", err)
		return
	}

	// The plan the status is judged against — resolved exactly as the card
	// resolves its own (active session unless ?session= names one; chain
	// trade date; latest row; overlays folded in).
	now := time.Now()
	reg := s.planRegistry()
	sess, ok := reg.ActiveSession(now)
	sessName := ""
	if ok {
		sessName = sess.Name
	}
	sessName, sess, _ = resolveRequestedSession(reg, c.Query("session"), sessName, sess)
	inPlan := map[int64]bool{}
	var judged gin.H // nil → JSON null
	if sessName != "" {
		tradeDate := now.In(planChicago()).Format("2006-01-02")
		if d, okD := kernel.PlanChainTradeDate(sess, now); okD {
			tradeDate = d
		}
		if row, rErr := s.store.Plan().GetLatestPlanForTraderSession(tradeDate, sessName, traderID); rErr == nil && row != nil {
			if doc, okDoc := s.resolvePlanFinal(row); okDoc {
				for _, l := range doc.Levels {
					inPlan[roundKey(l.Price)] = true
				}
				judged = gin.H{"plan_id": row.PlanID, "version": row.Version, "session": sessName, "trade_date": tradeDate}
			}
		}
	}

	levels := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		status := "pending"
		if inPlan[roundKey(r.Price)] {
			status = "applied"
		}
		levels = append(levels, gin.H{
			"id":           r.ID,
			"symbol":       r.Symbol,
			"price":        r.Price,
			"label":        r.Label,
			"note":         r.Note,
			"scenario_tag": r.ScenarioTag,
			"created_at":   r.CreatedAt,
			"consumed":     r.Consumed,
			"status":       status,
		})
	}
	c.JSON(200, gin.H{
		"levels":         levels,
		"count":          len(levels),
		"symbol":         symbol,
		"as_of_ms":       now.UnixMilli(),
		"judged_against": judged,
	})
}

// handlePlanSessionRegistry GET /api/plan/session-registry?trader_id=xxx — returns
// the EFFECTIVE admin session registry (stored or the shipped default) + whether the
// default is in force. W8 — the read side of the admin-registry wire the gates honor.
func (s *Server) handlePlanSessionRegistry(c *gin.Context) {
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if _, err := s.traderManager.GetTrader(traderID); err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	raw, _ := s.store.GetSystemConfig(kernel.SessionRegistryConfigKey)
	reg, perr := kernel.LoadSessionRegistry(raw) // fail-safe to default
	c.JSON(200, gin.H{
		"registry":      reg,
		"is_default":    strings.TrimSpace(raw) == "" || perr != nil,
		"sessions":      len(reg.Sessions),
		"parse_warning": errString(perr),
	})
}

// handlePlanSessionRegistrySave POST /api/plan/session-registry — validate + persist
// an admin-edited registry. Unlike the fail-safe loader, a MALFORMED edit is REFUSED
// (400), never silently defaulted — an admin must not be able to disable the gates by
// posting junk. The next CME session-day's gates pick it up (never mid-session).
func (s *Server) handlePlanSessionRegistrySave(c *gin.Context) {
	var body struct {
		TraderID string                 `json:"trader_id"`
		Registry kernel.SessionRegistry `json:"registry"`
	}
	_ = c.ShouldBindJSON(&body)
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if _, err := s.traderManager.GetTrader(traderID); err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	if err := kernel.ValidateSessionRegistry(body.Registry); err != nil {
		SafeBadRequest(c, "invalid registry: "+err.Error())
		return
	}
	raw, err := body.Registry.Marshal()
	if err != nil {
		SafeInternalError(c, "marshal registry", err)
		return
	}
	if err := s.store.SetSystemConfig(kernel.SessionRegistryConfigKey, raw); err != nil {
		SafeInternalError(c, "save registry", err)
		return
	}
	c.JSON(200, gin.H{"saved": true, "sessions": len(body.Registry.Sessions),
		"note": "honored by the next CME session-day's gates (not mid-session)"})
}

// errString renders an error as a string ("" when nil) for JSON responses.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// handlePlanApprove POST /api/plan/approve — grant entries for THIS trader's current
// CME session-day when approval_required is ON (W9). The owner clicks it after
// reviewing the plan; the entry gate then lets entries through for that session-day.
func (s *Server) handlePlanApprove(c *gin.Context) {
	var body struct {
		TraderID string `json:"trader_id"`
	}
	_ = c.ShouldBindJSON(&body)
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if _, err := s.traderManager.GetTrader(traderID); err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	dayKey := kernel.CMESessionDayKey(time.Now())
	if err := s.store.SetSystemConfig(trader.ApprovalKey(traderID, dayKey), "granted"); err != nil {
		SafeInternalError(c, "grant approval", err)
		return
	}
	c.JSON(200, gin.H{"approved": true, "session_day": dayKey,
		"note": "entries flow for this CME session-day"})
}

// planRegistry returns the EFFECTIVE session registry for the read-side plan
// APIs: the admin-edited registry stored in system_config (the same source the
// trader gates read since W8), falling back to the shipped default when the key
// is absent or malformed. Before this, every /api/plan/* handler hardcoded
// DefaultSessionRegistry, so an admin who edited the registry got a card, an
// overlay gate and an Ask-Planner scope running on a DIFFERENT clock than the
// executor — two clocks, silently disagreeing (design-conformance audit, D7).
// Empty key → byte-identical to the previous behavior.
func (s *Server) planRegistry() kernel.SessionRegistry {
	if s.store == nil {
		return kernel.DefaultSessionRegistry()
	}
	raw, _ := s.store.GetSystemConfig(kernel.SessionRegistryConfigKey)
	reg, _ := kernel.LoadSessionRegistry(raw) // fail-safe to the default
	return reg
}

// ─── W13 · PLAN RE-ALIGNMENT ON OWNER EDIT ────────────────────────────────────
// Owner decision 2026-08-16: an overlay save AUTO-triggers a whole-plan
// re-examination that returns a PROPOSAL. Nothing changes without an Apply tap —
// the apply path is the existing POST /api/plan/ask/apply (same qa row, same
// planner-revised overlay), so there is exactly one mutation door.

// resolvePlanFinal folds a plan row's overlays into plan_final (the doc the card
// and the executor see), armored: a bad overlay falls back to the base doc.
// W5 — through kernel.ResolvePlanFinal, the ONE fold (machine scenarios
// appended after the user fold; byte-identical with none).
func (s *Server) resolvePlanFinal(row *store.PlanDB) (kernel.PlanDoc, bool) {
	var doc kernel.PlanDoc
	if json.Unmarshal([]byte(row.Doc), &doc) != nil {
		return kernel.PlanDoc{}, false
	}
	overlays, _ := s.store.Plan().ListOverlays(row.PlanID, row.Version)
	if len(overlays) == 0 {
		return doc, true
	}
	base, err := json.Marshal(doc)
	if err != nil {
		return doc, true
	}
	pf, err := kernel.ResolvePlanFinal(base, kernel.OverlayRefsFrom(overlays))
	if err != nil {
		return doc, true
	}
	for _, ms := range pf.MachineSkipped {
		logger.Warnf("⚠️ machine overlay SKIPPED at the re-align fold (%s v%d): %v — that Picture scenario is not in plan_final", row.PlanID, row.Version, ms)
	}
	return pf.Doc, true
}

// RealignDebounceSec collapses rapid saves (bulk-add rows, fast successive edits)
// into ONE call.
const RealignDebounceSec int64 = 20

// realignGateDecision is the pure auto-trigger gate: "" = proceed with the call,
// "debounced" = an auto re-align already ran inside the window (this is what makes
// a bulk-add batch ONE call), "capped" = the auto budget for this plan is spent and
// the owner should use the manual button. A MANUAL re-align bypasses both — the
// owner asked for it explicitly. lastAt < 0 means "no previous auto re-align".
func realignGateDecision(manual bool, used, capN int, lastAt, nowUnix int64) string {
	if manual {
		return ""
	}
	if lastAt >= 0 && nowUnix-lastAt < RealignDebounceSec {
		return "debounced"
	}
	if capN > 0 && used >= capN {
		return "capped"
	}
	return ""
}

// handlePlanRealign POST /api/plan/realign — re-examine the whole plan after an
// owner edit. Always 200 with a `status` the FE renders:
//
//	proposal   → PROPOSE-MERGE + patch (the card slides in; Apply-gated)
//	no-change  → the plan stands (quiet auto-fading chip)
//	skipped    → no active plan / night / disabled / expired / day-plan off
//	debounced  → an identical-window call already ran (bulk-add safety)
//	capped     → auto budget spent → the FE shows the manual "Re-align plan" button
//	failed     → fail-closed: plan untouched, alert row written, NO partial patch
func (s *Server) handlePlanRealign(c *gin.Context) {
	var body struct {
		TraderID string `json:"trader_id"`
		Symbol   string `json:"symbol"`
		Manual   bool   `json:"manual"` // owner tapped Re-align → bypasses the cap
		Change   struct {
			Kind        string  `json:"kind"`
			Summary     string  `json:"summary"`
			Price       float64 `json:"price"`
			Label       string  `json:"label"`
			Grade       string  `json:"grade"`
			Instruction string  `json:"instruction"`
			Note        string  `json:"note"`
			ScenarioTag string  `json:"scenario_tag"`
			BatchCount  int     `json:"batch_count"`
		} `json:"change"`
	}
	_ = c.ShouldBindJSON(&body)
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	at, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}
	symbol := strings.TrimSpace(body.Symbol)
	if symbol == "" {
		symbol = "MNQ"
	}
	if !at.DayPlanOn() {
		c.JSON(200, gin.H{"status": "skipped", "reason": "day_plan_off"})
		return
	}

	// SKIP: no active plan · night / disabled session · expired plan.
	now := time.Now()
	reg := s.planRegistry()
	tradeDate := now.In(planChicago()).Format("2006-01-02")
	sess, ok := reg.ActiveSession(now)
	// P1 — sessionRunnable, not the raw registry flag.
	runnable := true
	if s.traderManager != nil {
		if at, aErr := s.traderManager.GetTrader(traderID); aErr == nil && at != nil {
			if okR, _ := at.SessionRunnable(sess); !okR {
				runnable = false
			}
		}
	}
	if !ok || !runnable {
		c.JSON(200, gin.H{"status": "skipped", "reason": "night_or_disabled_session"})
		return
	}
	row, err := s.store.Plan().GetLatestPlanForTraderSession(tradeDate, sess.Name, traderID)
	if err != nil || row == nil {
		c.JSON(200, gin.H{"status": "skipped", "reason": "no_active_plan"})
		return
	}
	if row.Lifecycle != "active" {
		c.JSON(200, gin.H{"status": "skipped", "reason": "plan_" + row.Lifecycle})
		return
	}

	trigger := kernel.TriggerOwnerEdit
	if body.Manual {
		trigger = kernel.TriggerManual
	}

	// DEBOUNCE + CAP (auto only). The debounce is what makes a bulk-add batch ONE
	// call even if the FE fires per row; the cap bounds automation spend.
	var lastAt int64 = -1
	var lastID int64
	used := 0
	if !body.Manual {
		if last, _ := s.store.PlanQA().LastPlannerByTrigger(traderID, row.PlanID, trigger); last != nil {
			lastAt, lastID = last.CreatedAt, last.ID
		}
		used, _ = s.store.PlanQA().CountPlannerByTrigger(traderID, row.PlanID, trigger)
	}
	capN := at.RealignCap()
	switch realignGateDecision(body.Manual, used, capN, lastAt, now.Unix()) {
	case "debounced":
		c.JSON(200, gin.H{"status": "debounced", "window_sec": RealignDebounceSec, "qa_id": lastID})
		return
	case "capped":
		c.JSON(200, gin.H{"status": "capped", "used": used, "cap": capN})
		return
	}

	doc, okDoc := s.resolvePlanFinal(row) // overlay-resolved: what the owner sees
	if !okDoc {
		c.JSON(200, gin.H{"status": "failed", "reason": "bad_plan_doc"})
		return
	}

	// SAME context blocks Ask-Planner uses.
	planBlock := kernel.RenderPlanBlock(doc, sess.Name)
	liveStatus := ""
	if market.FuturesBarsProvider != nil {
		if bars := market.FuturesBarsProvider(symbol, "1m", kernel.AISVPBarCount); len(bars) > 0 {
			price, dATR := marketRef(symbol, now)
			rule, _, left := s.planRules(traderID, sess.Name, row.TradeDate) // W15.B
			liveStatus = kernel.RenderPlanStatus(traderID, symbol, doc, bars, price, dATR, rule, left, now.UnixMilli(), row.CreatedAt.UnixMilli())
		}
	}
	change := kernel.OwnerChange{
		Kind: body.Change.Kind, Summary: body.Change.Summary, Price: body.Change.Price,
		Label: body.Change.Label, Grade: body.Change.Grade, Instruction: body.Change.Instruction,
		Note: body.Change.Note, ScenarioTag: body.Change.ScenarioTag, BatchCount: body.Change.BatchCount,
	}
	userPrompt := kernel.BuildRealignUserPrompt(planBlock, liveStatus, change)

	client, modelID := at.ResolvePlannerClient()
	if client == nil && !(SandboxMode() && !SandboxRealLLM()) {
		s.realignFailClosed(traderID, row, sess.Name, tradeDate, "no planner client")
		c.JSON(200, gin.H{"status": "failed", "reason": "no_planner_client"})
		return
	}

	// FAIL-CLOSED: any error or malformed reply → no proposal, plan untouched.
	started := time.Now()
	var raw string
	var callErr error
	if SandboxMode() && !SandboxRealLLM() {
		raw = sandboxPlannerRaw(userPrompt) // canned, zero cost
	} else {
		raw, callErr = client.CallWithMessages(kernel.AskPlannerSystemPrompt, userPrompt)
	}
	latencyMs := time.Since(started).Milliseconds()
	if callErr != nil {
		s.realignFailClosed(traderID, row, sess.Name, tradeDate, "planner call failed: "+callErr.Error())
		c.JSON(200, gin.H{"status": "failed", "reason": "planner_call_failed", "latency_ms": latencyMs})
		return
	}
	reply, perr := kernel.ParsePlannerReply(raw)
	if perr != nil {
		s.realignFailClosed(traderID, row, sess.Name, tradeDate, "planner reply malformed")
		c.JSON(200, gin.H{"status": "failed", "reason": "planner_reply_malformed", "latency_ms": latencyMs})
		return
	}

	// LOG into the SAME KPI table as Ask-Planner (one comparable series).
	patchStr := ""
	if len(reply.Patch) > 0 {
		patchStr = string(reply.Patch)
	}
	noChange := kernel.IsNoChange(reply)
	if noChange {
		patchStr = "" // a no-change verdict never carries a patch
	}
	costUSD := estimateRealignCostUSD(userPrompt, raw)
	qaID, _ := s.store.PlanQA().Append(&store.PlanQADB{
		TraderID: traderID, PlanID: row.PlanID, TradeDate: tradeDate, Session: sess.Name,
		Role: "planner", Content: reply.Summary, Evidence: reply.Evidence,
		PointClass: reply.PointClass, Verdict: reply.Verdict, Patch: patchStr,
		Trigger: trigger, ChallengeType: strings.TrimSpace(body.Change.Kind),
		CostUSD: costUSD, LatencyMs: latencyMs, CreatedAt: now.Unix(),
	})
	logger.Infof("🔄 plan re-align (%s/%s) %s → %s · %dms · ~$%.4f · model %s",
		trigger, body.Change.Kind, row.PlanID, reply.Verdict, latencyMs, costUSD, modelID)

	status := "proposal"
	if noChange {
		status = "no-change"
	}
	c.JSON(200, gin.H{
		"status": status, "qa_id": qaID,
		"plan_id": row.PlanID, "plan_version": row.Version,
		"would_become": fmt.Sprintf("v%d+o%d", row.Version, s.nextOverlayVersion(row)),
		"latency_ms":   latencyMs, "cost_usd": costUSD,
		"reply": gin.H{
			"evidence": reply.Evidence, "point_class": reply.PointClass,
			"verdict": reply.Verdict, "summary": reply.Summary, "patch": patchStr,
		},
	})
}

// realignFailClosed records the failure as a P1 alert row so a silent-failing
// automation is visible. The plan is never touched on this path.
func (s *Server) realignFailClosed(traderID string, row *store.PlanDB, session, tradeDate, reason string) {
	logger.Warnf("🔄 plan re-align FAIL-CLOSED (%s %s): %s — plan untouched", tradeDate, session, reason)
	if s.store == nil {
		return
	}
	_, _ = s.store.Alert().Emit(&store.AlertDB{
		TraderID: traderID, Level: "P1", Kind: "realign-failed",
		EventID: fmt.Sprintf("realign-fail:%s:%d", row.PlanID, time.Now().Unix()),
		Title:   "Plan re-align failed — plan unchanged", Body: reason,
		CreatedAt: time.Now().Unix(),
	})
}

// nextOverlayVersion is the overlay version an Apply would create (for the card's
// "would become v1+oN" header).
func (s *Server) nextOverlayVersion(row *store.PlanDB) int {
	overlays, _ := s.store.Plan().ListOverlays(row.PlanID, row.Version)
	return len(overlays) + 1
}

// estimateRealignCostUSD approximates the spend of one re-align call from prompt +
// reply size (~4 chars/token) at the pinned reasoner's published rate. Approximate
// by construction — it exists so the owner can see the automation's running cost,
// not for billing.
func estimateRealignCostUSD(prompt, reply string) float64 {
	const (
		inPerMTok   = 0.28 // deepseek-v4-pro input, USD / 1M tokens
		outPerMTok  = 0.42 // deepseek-v4-pro output
		charsPerTok = 4.0
	)
	inTok := float64(len(prompt)) / charsPerTok
	outTok := float64(len(reply)) / charsPerTok
	return (inTok*inPerMTok + outTok*outPerMTok) / 1_000_000
}

// scenarioStatusForLifecycle projects the stored statuses onto the PLAN's
// lifecycle. W16/R1 — a plan that is no longer active has no armed plays, so a
// non-active lifecycle overrides every scenario to "expired" rather than leaving
// yesterday's dots looking live. Scenarios with no stored status stay absent.
func scenarioStatusForLifecycle(m map[string]string, lifecycle string, doc kernel.PlanDoc) map[string]string {
	if strings.TrimSpace(lifecycle) == "" || lifecycle == "active" {
		return m
	}
	out := make(map[string]string, len(doc.Scenarios))
	for _, sc := range doc.Scenarios {
		out[sc.ID] = kernel.ScenarioExpired
	}
	if len(out) == 0 {
		return m
	}
	return out
}

// scenarioStatus reads the stored per-scenario live status map for a plan, or nil.
//
// W16/R1 — this is no longer a sandbox-only passthrough: the trader writes the
// map every cycle from the SAME P0.4 facts the executor sees
// (trader/auto_trader_levelstate.go recordScenarioState → kernel/scenario_state.go).
// A scenario whose anchor level could not be resolved is deliberately ABSENT
// from the map, and the card's fallback covers it — saying nothing beats
// inventing a status.
func (s *Server) scenarioStatus(traderID, planID string, version int) map[string]string {
	if s.store == nil {
		return nil
	}
	raw, _ := s.store.GetSystemConfig(store.ScenarioStatusKey(traderID, planID, version))
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var m map[string]string
	if json.Unmarshal([]byte(raw), &m) != nil || len(m) == 0 {
		return nil
	}
	return m
}

// scenarioMeta (A1/A4) reads the basis/unevaluable envelope; nil when absent.
func (s *Server) scenarioMeta(traderID, planID string, version int) map[string]any {
	if s.store == nil {
		return nil
	}
	raw, _ := s.store.GetSystemConfig(store.ScenarioMetaKey(traderID, planID, version))
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil || len(m) == 0 {
		return nil
	}
	return m
}

// planW3Map builds the W3 merged map for one card request, from the same bars
// the level facts use. Returns nil when the map cannot be assembled; callers
// then emit no W3 keys at all rather than empty ones.
func planW3Map(traderID, symbol string, bars []market.Kline, now time.Time) []kernel.MapCandidate {
	reg := kernel.ResolvedSessionRegistryFor(traderID)
	scored, price, _ := kernel.AssembleScoredLevels(traderID, bars, reg, symbol, kernel.PlanHardMaxLevels, now, 0)
	if price <= 0 || len(scored) == 0 {
		return nil
	}
	return kernel.BuildMapCandidates(scored, price, kernel.StaleConfirmATR5m(bars), kernel.MapCandidateOpts{})
}

// oneSetupFor reads the arm seam's scenario record for the displayed version.
// When no record exists (the seam has not run on this version, or the switch
// is OFF) the payload carries the RESOLVED switch and grade with an empty
// scenarios map — the chip then reads "one-setup: off" or "not evaluated",
// never "allowed".
func (s *Server) oneSetupFor(traderID, planID string, version int) map[string]any {
	enabled, grade := true, "B"
	if at, err := s.traderManager.GetTrader(traderID); err == nil && at != nil {
		enabled, grade, _, _ = store.ResolveOneSetup(at.GetStrategyConfig())
	}
	out := map[string]any{"enabled": enabled, "min_grade": grade, "scenarios": map[string]any{}}
	if s.store == nil {
		return out
	}
	if rec := s.store.GetOneSetupRecord(traderID, planID, version); rec != nil {
		out["enabled"] = rec.Enabled
		out["min_grade"] = rec.MinGrade
		out["evaluated_ms"] = rec.EvaluatedMs
		out["scenarios"] = rec.Scenarios
	}
	return out
}

// picturePlanGate is the card's Picture line: the trader's own read (mode on,
// strict refusal); nil — absent, never a fabricated "admitted" — when the
// trader is not loaded.
func (s *Server) picturePlanGate(traderID string, now time.Time) *trader.PicturePlanGateView {
	at, err := s.traderManager.GetTrader(traderID)
	if err != nil || at == nil {
		return nil
	}
	v := at.PicturePlanGateAt(now)
	return &v
}

// fadePermissionFor evaluates the live fade label for every scenario in doc.
// Nil-safe: a trader that cannot be resolved yields an empty map, which the
// card renders as "not evaluated" — never as permitted.
func (s *Server) fadePermissionFor(traderID string, doc *kernel.PlanDoc) map[string]trader.FadeLabelView {
	at, err := s.traderManager.GetTrader(traderID)
	if err != nil || at == nil || doc == nil {
		return map[string]trader.FadeLabelView{}
	}
	return at.FadeLabelsFor(time.Now(), doc, at.LastPriceForDesk(), nil)
}

func (s *Server) fadeCounterFor(traderID string) trader.FadeCounterView {
	at, err := s.traderManager.GetTrader(traderID)
	if err != nil || at == nil {
		return trader.FadeCounterView{}
	}
	return at.FadeCounterToday(time.Now())
}
