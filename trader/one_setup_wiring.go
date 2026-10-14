// ── ONE SETUP (dispatch 102, 2026-09-10) — THE PRODUCTION CALL PATH ─────────
//
// ONE predicate (kernel.OneSetupAllowsAt), ONE call site (oneSetupVerdictsAt,
// called once per arm cycle from maybeManageArmedOrdersAt before the scenario
// loop) and ONE consult (oneSetupConsult, at the seam after the class-48 gate
// and before the arm row is composed). Registered in the A29 gate by THEIR
// OWN NAMES — W1 proved that registering only the inner function lets an
// unwired wrapper satisfy the gate for everything inside it.
//
// What this file never does (A31/H): it never cancels a resting arm — a
// transient best-level flip that cancelled would meet MANUAL-CANCEL-WINS and
// kill the re-arm for the plan version; a resting arm keeps its slot until it
// is terminal by the code's own predicate, and `declined_while_resting` is
// COUNTED so the owner can rule on it. It never touches the map. It never
// reaches the wire.
package trader

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// oneSetupConfig resolves the two knobs from THE STRATEGY BOUND TO THIS TRADER
// (A11): nil → ON [O], B [O]. The consumer the knob registry names.
func (at *AutoTrader) oneSetupConfig(cfg *store.StrategyConfig) kernel.OneSetupConfig {
	if cfg == nil && at != nil {
		cfg = at.config.StrategyConfig
	}
	en, grade, _, _ := store.ResolveOneSetup(cfg)
	return kernel.OneSetupConfig{Enabled: en, MinGrade: grade}
}

// oneSetupLevelRef resolves the scenario's level the way the dispatch orders:
// by level_id (105's LevelByID over the plan's levels and identity levels →
// candidate_id); else the scenario's own anchor price (confirm ref / arm entry
// → price_proximity); a level_id that does not resolve is unresolved and NEVER
// resolves by nearest.
func oneSetupLevelRef(sc kernel.PlanScenario, doc *kernel.PlanDoc, geometryRefIDs bool) kernel.OneSetupLevelRef {
	if sc.LevelID != nil && *sc.LevelID != "" {
		levels := make([]kernel.PlanLevel, 0, len(doc.Levels)+len(doc.IdentityLevels))
		levels = append(levels, doc.Levels...)
		levels = append(levels, doc.IdentityLevels...)
		if l, ok := kernel.LevelByID(sc.LevelID, levels); ok && l.Price > 0 {
			id := *sc.LevelID
			return kernel.OneSetupLevelRef{Price: l.Price, ID: &id, Basis: kernel.LevelBasisCandidateID}
		}
		// W-GEOMETRY-REFUSAL (F3): a stable ref| id (reference-anchor level
		// without a formation close) resolves through the reference lookup when
		// the knob is ON — exactly as the executor does. Otherwise the one-setup
		// consult declines every ref-id scenario as level_unresolved and a new
		// ONH reject play can never arm.
		if geometryRefIDs {
			if l, ok := kernel.LevelByReferenceID(sc.LevelID, levels); ok && l.Price > 0 {
				id := *sc.LevelID
				return kernel.OneSetupLevelRef{Price: l.Price, ID: &id, Basis: kernel.LevelBasisCandidateID}
			}
		}
		return kernel.OneSetupLevelRef{Basis: "unresolved:unknown_level_id"}
	}
	if a, ok := scenarioAnchorFor(sc); ok && a.Price > 0 {
		return kernel.OneSetupLevelRef{Price: a.Price, Basis: kernel.LevelBasisPriceProximity}
	}
	return kernel.OneSetupLevelRef{Basis: "unresolved:no_anchor"}
}

// oneSetupTestFacts is the test seam's payload (see AutoTrader.oneSetupFactsForTest).
type oneSetupTestFacts struct {
	Candidates []kernel.MapCandidate
	Price      float64
	BandPts    float64
	Permission map[string]kernel.FadeVerdict // by scenario id
}

// oneSetupCycle is what one arm cycle carries from the call site to the
// consult: the verdicts, the resolved config, and the record being built.
type oneSetupCycle struct {
	cfg      kernel.OneSetupConfig
	verdicts map[string]kernel.OneSetupVerdict
	refs     map[string]kernel.OneSetupLevelRef
	record   store.OneSetupRecord
	planID   string
	version  int
	session  string
	dirty    bool
}

func (c *oneSetupCycle) on() bool { return c != nil && c.cfg.Enabled }

// allowed is the D4 input: which scenarios the predicate allowed this cycle.
func (c *oneSetupCycle) allowed() map[string]bool {
	if c == nil || !c.cfg.Enabled {
		return nil
	}
	out := map[string]bool{}
	for id, v := range c.verdicts {
		if v.Allowed {
			out[id] = true
		}
	}
	return out
}

// oneSetupVerdictsAt — THE ONE CALL SITE. Evaluates every armable scenario
// ONCE per cycle at `now` against the LIVE map (the same assemble + merge the
// card and the planner use — never the frozen plan map) and the LIVE
// permission. OFF → an inert cycle: nil verdicts, doc order, no record.
// recover() is pinned (A10): a fault here declines nothing and arms nothing
// beyond what today's seam would.
func (at *AutoTrader) oneSetupVerdictsAt(plan *kernel.ActivePlan, doc *kernel.PlanDoc, bars []market.Kline, atr5m float64, cfg *store.StrategyConfig, now time.Time) (c *oneSetupCycle) {
	c = &oneSetupCycle{cfg: at.oneSetupConfig(cfg)}
	defer func() {
		if r := recover(); r != nil {
			at.logWarnf("🎯 one setup: verdicts recovered from panic: %v — this cycle authorizes NOTHING under one-setup (fail closed)", r)
			c.verdicts = map[string]kernel.OneSetupVerdict{}
		}
	}()
	if !c.cfg.Enabled || plan == nil || doc == nil {
		return c
	}
	c.planID, c.version, c.session = plan.PlanID, plan.Version, plan.Session
	c.verdicts = map[string]kernel.OneSetupVerdict{}
	c.refs = map[string]kernel.OneSetupLevelRef{}
	c.record = store.OneSetupRecord{Enabled: true, MinGrade: c.cfg.MinGrade, EvaluatedMs: now.UnixMilli(), Scenarios: map[string]store.OneSetupScenarioRecord{}}

	symbol := at.futuresSymbol()
	var scored []kernel.ScoredLevel
	var cands []kernel.MapCandidate
	price, dATR, band := 0.0, 0.0, 0.0
	var testPerm map[string]kernel.FadeVerdict
	if at.oneSetupFactsForTest != nil {
		f := at.oneSetupFactsForTest(now)
		cands, price, band, testPerm = f.Candidates, f.Price, f.BandPts, f.Permission
	} else {
		if len(bars) > 0 {
			scored, price, dATR = kernel.AssembleScoredLevelsBand(at.id, bars, at.sessionRegistry(now), symbol, kernel.PlanHardMaxLevels, now, at.proximityFilterATR(), at.confluenceBandPts())
		}
		if price > 0 && len(scored) > 0 {
			cands = kernel.BuildMapCandidates(scored, price, atr5m, kernel.MapCandidateOpts{})
		}
		if dATR > 0 {
			band = at.proximityFilterATR() * dATR
		}
	}
	for i, sc := range doc.Scenarios {
		if sc.Arm == nil || !sc.Arm.Enabled {
			continue
		}
		ref := oneSetupLevelRef(sc, doc, cfg.DayPlan.GeometryRefIDsEnabled())
		facts := kernel.OneSetupLevelFacts{Price: price, BandPts: band, Candidates: cands, Scenario: ref}
		var perm kernel.FadeVerdict
		if testPerm != nil {
			perm = testPerm[sc.ID]
		} else {
			perm = kernel.FadePermissionAt(now, at.fadeFactsAt(now, symbol, price, scored, strings.ToLower(sc.Direction)))
		}
		v := kernel.OneSetupAllowsAt(now, sc, facts, perm, c.cfg)
		c.verdicts[sc.ID] = v
		c.refs[sc.ID] = ref
		c.record.Scenarios[sc.ID] = store.OneSetupScenarioRecord{
			Allowed: v.Allowed, Level: v.Level, Play: v.Play, Permission: v.Permission, Reason: v.Reason,
			BestPrice: v.BestPrice, BestNames: v.BestNames, BestGrade: v.BestGrade, Rank: i + 1, EvaluatedMs: v.EvaluatedMs,
		}
	}
	c.dirty = len(c.record.Scenarios) > 0
	return c
}

// oneSetupSaveRecord writes the scenario record for the card (A15: the chip
// shows what the seam DECIDED, never a re-evaluation).
func (at *AutoTrader) oneSetupSaveRecord(c *oneSetupCycle) {
	if c == nil || !c.dirty || at == nil || at.store == nil {
		return
	}
	if err := at.store.SaveOneSetupRecord(at.id, c.planID, c.version, c.record); err != nil {
		at.logWarnf("🎯 one setup: record write failed for %s v%d: %v", c.planID, c.version, err)
	}
}

// oneSetupObstacleTarget — D3. The arm's target is the scenario's RECORDED
// first obstacle when one is authored and it lies on the profit side of the
// entry. ok=false leaves the authored target standing and names why.
func oneSetupObstacleTarget(sc kernel.PlanScenario, entry float64) (target float64, label string, ok bool) {
	if sc.Economics == nil || sc.Economics.FirstObstacle == nil || sc.Economics.FirstObstacle.Price == nil {
		return 0, "authored(obstacle_missing)", false
	}
	px := *sc.Economics.FirstObstacle.Price
	if px <= 0 || math.IsNaN(px) || math.IsInf(px, 0) || entry <= 0 {
		return 0, "authored(obstacle_missing)", false
	}
	long := strings.EqualFold(strings.TrimSpace(sc.Direction), "long")
	if (long && px <= entry) || (!long && px >= entry) {
		return 0, "authored(obstacle_wrong_side)", false
	}
	return px, "first_obstacle@" + strconv.FormatFloat(px, 'f', 2, 64), true
}

// oneSetupOtherSlot — D4. ONE arm at a time per plan: reports the scenario
// that already holds a non-terminal row for this plan, if it is not this one.
// Terminal is the CODE'S predicate (store.IsTerminalArmState), never a list.
func oneSetupOtherSlot(rows []store.ArmedOrderDB, traderID, planID, scenario string) (holder string, held bool) {
	for _, r := range rows {
		if r.TraderID != traderID || r.PlanID != planID || r.Scenario == scenario {
			continue
		}
		if store.IsTerminalArmState(r.State) {
			continue
		}
		return r.Scenario, true
	}
	return "", false
}

func oneSetupHasOwnSlot(rows []store.ArmedOrderDB, traderID, planID, scenario string) bool {
	for _, r := range rows {
		if r.TraderID == traderID && r.PlanID == planID && r.Scenario == scenario && !store.IsTerminalArmState(r.State) {
			return true
		}
	}
	return false
}

// oneSetupConsult — THE CONSULT at the seam, after the gate legs and before
// the arm row is composed. Returns true when the seam must `continue` (the
// scenario is declined, or waits); false when it may arm. Everything it does
// is a log line, a counter, a stamp or a record — never a cancel.
func (at *AutoTrader) oneSetupConsult(c *oneSetupCycle, plan *kernel.ActivePlan, sc kernel.PlanScenario, li int, ledger *store.ArmedOrderStore, now time.Time) (skip bool) {
	if !c.on() {
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			at.logWarnf("🎯 one setup: consult recovered from panic on %s: %v — declined this cycle (fail closed)", sc.ID, r)
			skip = true
		}
	}()
	v, ok := c.verdicts[sc.ID]
	if !ok {
		// Not evaluated (a fault upstream): fail closed, say so.
		at.logWarnf("🎯 one setup: %s %s has no verdict this cycle — declined (fail closed)", plan.Session, sc.ID)
		return true
	}
	key := plan.PlanID + ":" + strconv.Itoa(plan.Version) + ":" + sc.ID + ":leg" + strconv.Itoa(li+1) + ":one_setup"
	tradeDate := kernel.PlanTradeDateFor(plan)
	var rows []store.ArmedOrderDB
	if ledger != nil {
		rows, _ = ledger.ListNonTerminal(at.id)
	}
	rec := c.record.Scenarios[sc.ID]

	if !v.Allowed {
		class := oneSetupDeclineClass(v)
		if armRefusalChanged(&at.armRefusalLast, key, class) {
			n := 0
			if at.store != nil {
				n, _ = store.IncArmRefusal(at.store, at.id, tradeDate, plan.Session, class)
			}
			// A9 LOUD: every declined scenario names ALL THREE verdicts.
			at.logWarnf("🎯 one setup DECLINED %s %s leg %d: level=%s · play=%s · permission=%s (%s this session: %d)",
				plan.Session, sc.ID, li+1, v.Level, v.Play, v.Permission, class, n)
			if oneSetupHasOwnSlot(rows, at.id, plan.PlanID, sc.ID) {
				// The resting arm is NOT cancelled (see the file header); the
				// case is counted so the owner can rule on it with a number.
				if at.store != nil {
					_, _ = store.IncArmRefusal(at.store, at.id, tradeDate, plan.Session, store.OneSetupClassResting)
				}
				at.logWarnf("🎯 one setup: %s %s is declined now but holds a resting arm — left alone (counted declined_while_resting; a cancel here would meet MANUAL-CANCEL-WINS)", plan.Session, sc.ID)
			}
		}
		at.oneSetupStampEpisodes(plan.PlanID, sc, c.refs[sc.ID], v)
		return true
	}

	// D4 — one arm at a time per plan. The seam iterates in rank order, so the
	// first allowed scenario to reach this point is the top-ranked one; a
	// later one waits while any other scenario holds a non-terminal row.
	if holder, held := oneSetupOtherSlot(rows, at.id, plan.PlanID, sc.ID); held {
		if armRefusalChanged(&at.armRefusalLast, key, store.OneSetupClassWaiting) {
			if at.store != nil {
				_, _ = store.IncArmRefusal(at.store, at.id, tradeDate, plan.Session, store.OneSetupClassWaiting)
			}
			at.logInfof("🎯 one setup: %s %s leg %d second_setup_waiting — %s holds the plan's one arm; arms after it goes terminal", plan.Session, sc.ID, li+1, holder)
		}
		rec.Waiting = true
		c.record.Scenarios[sc.ID] = rec
		at.oneSetupStampEpisodes(plan.PlanID, sc, c.refs[sc.ID], v)
		return true
	}
	if armRefusalChanged(&at.armRefusalLast, key, store.OneSetupClassArmable) {
		if at.store != nil {
			_, _ = store.IncArmRefusal(at.store, at.id, tradeDate, plan.Session, store.OneSetupClassArmable)
		}
		at.logInfof("🎯 one setup ALLOWED %s %s leg %d: level=ok(%s@%.2f %s) · play=reject · permission=ok · target=%s", plan.Session, sc.ID, li+1, v.BestNames, v.BestPrice, v.BestGrade, rec.Target)
	}
	rec.Waiting = false
	c.record.Scenarios[sc.ID] = rec
	at.oneSetupStampEpisodes(plan.PlanID, sc, c.refs[sc.ID], v)
	return false
}

// oneSetupDeclineClass maps the three verdicts onto the counter classes —
// the first failing leg in the order the dispatch lists them (level, play,
// permission); the log line still carries all three.
func oneSetupDeclineClass(v kernel.OneSetupVerdict) string {
	switch {
	case v.Level != "ok":
		return store.OneSetupClassLevel
	case v.Play != "ok":
		return store.OneSetupClassPlay
	case v.Permission == "not_evaluated":
		return store.OneSetupClassNotEvaluated
	default:
		return store.OneSetupClassDay
	}
}

// oneSetupRecordTarget notes D3's target choice on the scenario record.
func (c *oneSetupCycle) recordTarget(scID, label string) {
	if c == nil || c.record.Scenarios == nil {
		return
	}
	rec := c.record.Scenarios[scID]
	rec.Target = label
	c.record.Scenarios[scID] = rec
}

// oneSetupStampEpisodes writes the verdict ONCE onto the scenario's episode
// rows — linked by price inside the map's own merge width (E7). A row with a
// verdict already keeps it; the clock is the seam's `now`.
func (at *AutoTrader) oneSetupStampEpisodes(planID string, sc kernel.PlanScenario, ref kernel.OneSetupLevelRef, v kernel.OneSetupVerdict) {
	if at == nil || at.store == nil || ref.Price <= 0 {
		return
	}
	ts := at.store.TouchOutcomes()
	rows, err := ts.UnstampedEpisodesNear(at.id, planID, ref.Price, scenarioLinkBand())
	if err != nil || len(rows) == 0 {
		return
	}
	stamp := store.OneSetupStamp{Scenario: sc.ID, Verdicts: oneSetupVerdictText(v), Reason: v.Reason, EvaluatedMs: v.EvaluatedMs}
	for _, r := range rows {
		if err := ts.StampOneSetup(r.ID, stamp); err != nil {
			at.logWarnf("🎯 one setup: episode %d stamp failed: %v", r.ID, err)
		}
	}
}

func oneSetupVerdictText(v kernel.OneSetupVerdict) string {
	return fmt.Sprintf("level=%s play=%s permission=%s", v.Level, v.Play, v.Permission)
}

// oneSetupRetireDeclined — THE GAP THE FIRST BOOT FOUND, CLOSED (owner ruling
// 2026-09-11 01:5x CT). One-setup filtered AUTHORIZATION; it did not touch an
// authorization that predated it. The class-33 sweep leaves pre-boot rows
// that were authorized but never placed "for this process to place", and the
// placement engine places any non-terminal `armed` row inside its band — so
// rows 150/153 (ASIA v10 S2/S1, both DECLINED on the first cycle) could have
// reached the wire.
//
// THE RULE: at placement time, an authorization whose scenario is CURRENTLY
// declined is NOT placed; it is retired by ledger state — cancelled, with the
// owner's reason and all three verdicts — and NOTHING is sent to the broker,
// because a row with no signal id has nothing at the broker under it. An
// allowed scenario's row still places. Runs once per cycle right after the
// verdicts and BEFORE the scenario loop and the placement pass, so D4's slot
// check never counts a doomed row and the placement engine never sees it.
// Rows already at the broker (signal id set) are not touched here — the
// predicate never cancels a broker order (file header).
func (at *AutoTrader) oneSetupRetireDeclined(c *oneSetupCycle, plan *kernel.ActivePlan, ledger *store.ArmedOrderStore, now time.Time) (retired int, retireErr error) {
	if !c.on() || plan == nil || ledger == nil {
		return 0, nil
	}
	defer func() {
		if r := recover(); r != nil {
			retireErr = fmt.Errorf("one-setup retirement panic: %v", r)
		}
	}()
	rows, err := ledger.ListNonTerminal(at.id)
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		if r.TraderID != at.id || r.PlanID != plan.PlanID || r.State != store.StateArmed || strings.TrimSpace(r.SignalID) != "" {
			continue
		}
		if r.Source == store.ArmSourcePicture {
			// Picture rows answer to their own epoch/admission gates, not to
			// one-setup verdicts — a waiting Picture arm has no verdict here
			// and must never be retired by this pass (W5 picture executor).
			continue
		}
		v, ok := c.verdicts[r.Scenario]
		if ok && v.Allowed {
			continue
		}
		if !ok {
			// F13 (port of #117 2dc94a19): no current verdict is NOT an
			// authorization — an inherited row whose permission is unknown is
			// retired, never placed.
			v = kernel.OneSetupVerdict{Level: "unknown", Play: "unknown", Permission: "unknown", Reason: "current verdict unavailable"}
		}
		origin := "authorization not placed"
		if r.BootID != "" && r.BootID != store.ProcessBootID() {
			origin = "pre-boot authorization not placed"
		}
		reason := "declined by one-setup; " + origin + " · " + oneSetupVerdictText(v)
		if err := ledger.SetState(r.ID, store.StateCancelled, reason); err != nil {
			return retired, fmt.Errorf("retire row %d: %w", r.ID, err)
		}
		retired++
		if at.store != nil {
			_, _ = store.IncArmRefusal(at.store, at.id, kernel.PlanTradeDateFor(plan), plan.Session, store.OneSetupClassRetired)
		}
		at.logWarnf("🎯 one setup RETIRED row %d (%s %s leg %d %s entry=%.2f, %s): level=%s · play=%s · permission=%s — never placed, nothing at the broker under it",
			r.ID, plan.Session, r.Scenario, r.LegIndex+1, r.Side, r.EntryPx, origin, v.Level, v.Play, v.Permission)
	}
	return retired, nil
}
