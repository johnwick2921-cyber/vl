package trader

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// W-DEATH-REREAD (2026-09-18, owner ruling 12:3x CT "fix all") — a death line
// that parks the plan until price returns SITS OUT the session when it doesn't
// (2026-09-18 NY v2 died at 09:10:18 and the bot had no plan for the whole
// session while price ran 100 pt away). With day_plan.death_reread ON (nil =
// ON, the default), a fired death-condition kill goes dormant exactly as
// today AND then launches ONE BUDGETED planner re-read (trigger death_replan —
// it SPENDS one unit of the class-35 replan budget, unlike the flip read,
// which is free) that authors a FRESH plan, bias free, with the death evidence
// in the read prompt. The fresh version supersedes the dormant one
// (superseded:death). OFF = today's behaviour byte-identical (dormant only).

// deathRereadBirthWickMinutes is the wick-noise guard width: a death-born plan
// cannot itself die inside this many minutes of birth on the SAME line — its
// first death check runs only after 2 full 5m closes post-birth.
func deathRereadBirthWickMinutes() int { return 10 }

// deathRereadDoneKey keys the once-per-fired-death re-read in system_config.
// Written with a timestamp ONLY after the read's goroutine has decided success
// by the STORE (a newer active version exists) — never at launch, exactly like
// the flip once-key: a refused or failed read clears it to "0" so the dormant
// branch retries next cycle.
func deathRereadDoneKey(row *store.PlanDB) string {
	return fmt.Sprintf("death_reread_done:%s:%d", row.PlanID, row.Version)
}

// deathRereadBudgetWarnKey keys the one-WARN-per-row budget-exhaustion marker.
func deathRereadBudgetWarnKey(row *store.PlanDB) string {
	return fmt.Sprintf("death_reread_budget_warn:%s:%d", row.PlanID, row.Version)
}

// deathRereadInFlightKey is the in-memory "a death re-read for this
// plan+version is running" guard key. Distinct from the flip key so a flip
// read and a death read for the same row can never collide.
func deathRereadInFlightKey(at *AutoTrader, row *store.PlanDB) string {
	return fmt.Sprintf("%s|%s|%d|death", at.id, row.PlanID, row.Version)
}

// deathRereadInFlight guards a running death re-read per plan|version.
var deathRereadInFlight sync.Map

// deathRereadPriorLine is the ONE producer of the prior-plan context the death
// re-read hands the write site as priorKiller: the dead version, its kill line,
// the price at death and the direction of the break. No flip vocabulary —
// kernel.FlipToDirection returns "" so the write site forces NO bias (the death
// read is bias free).
func deathRereadPriorLine(version int, oldBias, killer string, priceAtDeath float64) string {
	return fmt.Sprintf("PRIOR PLAN v%d bias %s DIED — break %s — price at death %.2f — the plan is dormant and will be superseded by the version you author now. Kill line: %s. Author a FRESH plan from the tape; the bias is NOT forced.",
		version, oldBias, deathRereadKillerDirection(killer), priceAtDeath, killer)
}

// deathRereadPriorLineKey keys the RAW death line of the killed version in
// system_config (B2, 2026-09-18 review): the killer string carries the BUFFERED
// line and the fresh plan's doc carries the RAW one — two different price spaces
// that differ by the ATR buffer (12 pts on a real row against a 3-pt tolerance),
// so the "same line" wick never fired exactly where plans flap. The RAW line is
// recorded at the dormant write and compared in the SAME space.
func deathRereadPriorLineKey(row *store.PlanDB) string {
	return fmt.Sprintf("death_reread_prior_line:%s:%d", row.PlanID, row.Version)
}

// priorDeathLinePriceSeam is the test seam the verifier's sabotage control
// overrides (2026-09-18 independent review): returning 0 must disable the wick
// guard end-to-end, which the chain test proves by observing v2 die.
var priorDeathLinePriceSeam = func(at *AutoTrader, row *store.PlanDB) float64 {
	return at.priorDeathLinePriceImpl(row)
}

// priorDeathLinePrice returns the RAW death line of the version BEFORE `row`,
// from the key recorded at that version's dormant write. 0 when absent — the
// wick guard then treats the lines as DIFFERENT (never suppresses a fresh
// plan's death).
func (at *AutoTrader) priorDeathLinePrice(row *store.PlanDB) float64 {
	return priorDeathLinePriceSeam(at, row)
}

func (at *AutoTrader) priorDeathLinePriceImpl(row *store.PlanDB) float64 {
	if at.store == nil || row == nil {
		return 0
	}
	versions, _, _, ok := at.planChainFacts(row)
	if !ok {
		return 0
	}
	prior := 0
	for _, v := range versions {
		if v.Version < row.Version && v.Version > prior {
			prior = v.Version
		}
	}
	if prior == 0 {
		return 0
	}
	v, err := at.store.GetSystemConfig(deathRereadPriorLineKey(&store.PlanDB{PlanID: row.PlanID, Version: prior}))
	if err != nil || v == "" {
		return 0
	}
	f, ferr := strconv.ParseFloat(v, 64)
	if ferr != nil {
		return 0
	}
	return f
}

// deathRereadRun is the read-call seam (fixtures substitute a recorder to
// assert the request without running a live planner stream). It rides the
// class-35 death_replan trigger, so a landed fresh version SPENDS one replan
// budget unit and lands the FlipHoldAnchorReplan anchor.
var deathRereadRun = func(at *AutoTrader, session, tradeDate, prior string, row *store.PlanDB, failClosed bool) bool {
	return at.runPlannerReadWithTriggerClaimedCtx(session, tradeDate, store.TriggerDeathReplan, prior, priorPlanLevelLines(row), failClosed)
}

// deathBornWickActive reports whether a death-born plan is inside its birth
// wick ON ITS OWN LINE (W-DEATH-REREAD (c), SF-5/B2): the first death check for
// a death-born version runs only after 2 full 5m closes post-birth, and only
// when the fresh plan authored the SAME death line the prior version died on
// (± FlipLineClusterTolerance), both compared in the RAW price space — the
// prior line is the RAW death price recorded at the dormant write (the killer's
// buffered number is a DIFFERENT space, off by the ATR buffer). A model that
// authors a NEW death line dies normally. PURE — pinned directly in tests.
func deathBornWickActive(row *store.PlanDB, dp *store.DayPlanConfig, now time.Time, priorKillLine float64) bool {
	if row == nil || dp == nil || now.IsZero() || row.CreatedAt.IsZero() {
		return false
	}
	if row.TriggerReason != store.TriggerDeathReplan {
		return false
	}
	if !dp.DeathRereadEnabled() {
		return false // OFF = today's behaviour: the wick guard does not exist
	}
	if now.Sub(row.CreatedAt) >= time.Duration(deathRereadBirthWickMinutes())*time.Minute {
		return false
	}
	if priorKillLine <= 0 {
		return false // the prior line is unknown — never suppress on an unknown line
	}
	var doc kernel.PlanDoc
	if json.Unmarshal([]byte(row.Doc), &doc) != nil || doc.DeathStructured == nil {
		return false
	}
	return math.Abs(doc.DeathStructured.Price-priorKillLine) <= kernel.FlipLineClusterTolerance()
}

// maybeRereadAfterDeath is the death counterpart of maybeRereadAfterFlip.
// Deliberately a SIBLING COPY (2026-09-18 review NIT): the trigger, the
// class-35 budget, the price-at-death prior line, the flap-guard gate and the
// superseded:death marker make a shared parameterised body more entangled than
// the duplication costs; a future flip-guard fix must be propagated here too
// (mirror, do not merge). Keeps the flip body's preflight, class-47 cutoff,
// in-flight guard, once-key per plan+version and self-backoff. Knob OFF → not
// called (the caller gates on the knob and this function double-checks it).
func (at *AutoTrader) maybeRereadAfterDeath(now time.Time, session, tradeDate string, row *store.PlanDB, killer string, priceAtDeath float64) {
	if at.store == nil || row == nil || market.FuturesBarsProvider == nil {
		return
	}
	cfg := at.config.StrategyConfig.DayPlan
	if cfg == nil || !cfg.DeathRereadEnabled() {
		return // knob OFF → dormant only, no read
	}
	if v, err := at.store.GetSystemConfig(deathRereadDoneKey(row)); err == nil && v != "" && v != "0" {
		return // one SUCCESSFUL read per fired death (plan+version key)
	}
	// SF-2 (2026-09-18 review; money) — LAUNCH TIMING VS THE FLAP GUARD: five of
	// the six re-armed deaths on the DB copy re-armed 5m27s–10m after death, at
	// the earliest instant DORMANT_MIN_HOLD_MIN=5 permits; a planner call is
	// 300–500 s, so a read launched at +0 lands AFTER the re-arm — a spent unit
	// (default cap 2) and a fresh version authored on a tape that already closed
	// back. Do not launch until the flap guard has elapsed since the dormant
	// write: the runaway case loses 5 minutes, the flap case spends nothing.
	if v, err := at.store.GetSystemConfig(dormantSinceKey(row)); err == nil && v != "" {
		if ms, perr := strconv.ParseInt(v, 10, 64); perr == nil && kernel.DormantMinHoldMin() > 0 {
			elapsed := now.Sub(time.UnixMilli(ms))
			if elapsed < 0 {
				// B1 (2026-09-18 review, money): the dormant timestamp is written
				// a few milliseconds AFTER the cycle's clock was captured, so at
				// the +0 launch `elapsed` is negative — negative means "just now",
				// still INSIDE the guard. The old elapsed>=0 clause let exactly the
				// launch that matters fall through.
				elapsed = 0
			}
			if elapsed < time.Duration(kernel.DormantMinHoldMin())*time.Minute {
				at.logInfof("🗓️ death re-read %s %s v%d — HELD: the dormant row is inside the %.0f-minute flap guard (%.1fm in); the re-arm predicate may clear it — retrying once the guard elapses.",
					tradeDate, session, row.Version, float64(kernel.DormantMinHoldMin()), elapsed.Minutes())
				return
			}
		}
	}
	// (b) BUDGET GATE — a death re-read SPENDS one class-35 replan unit; the
	// flip read is free but a death is the planner being wrong, and an unbounded
	// loop of dead plans on a trend day must stop. At budget exhausted → dormant
	// only (today's behaviour) with ONE WARN naming it AND the same P1 alert the
	// legacy consumed-death path raises (the owner must never see a silent
	// dormant card).
	replanCap := at.replanCapFor(session)
	budget := store.GetReplanBudget(at.store, at.id, tradeDate, session, replanCap)
	if !budget.May() {
		if warned, err := at.store.GetSystemConfig(deathRereadBudgetWarnKey(row)); err == nil && warned != "1" {
			at.logWarnf("🗓️ death re-read %s %s v%d — BUDGET EXHAUSTED (%d/%d): the dormant plan stands, no re-read (today's behaviour). A death re-read spends one replan unit; the flip read is free.",
				tradeDate, session, row.Version, budget.Used, budget.Cap)
			_ = at.store.SetSystemConfig(deathRereadBudgetWarnKey(row), "1")
			at.emitAlert("P1", "plan-death-streak",
				fmt.Sprintf("deaths:%s:%s:v%d", tradeDate, session, row.Version),
				fmt.Sprintf("%s plan died — re-plan budget exhausted (%d/%d)", session, budget.Used, budget.Cap),
				fmt.Sprintf("Killed by: %s. The dormant plan stands (today's behaviour) — the session sits out unless price closes back on the valid side.", killer))
		}
		return
	}
	inflightKey := deathRereadInFlightKey(at, row)
	if _, running := deathRereadInFlight.Load(inflightKey); running {
		return // a read for this plan+version is still open — never double it
	}
	// Same preflight as every read — synchronously, so a refusal never consumes
	// the once-key: the dormant plan stands and the next cycle may retry.
	if !at.plannerPreflight(session, tradeDate, store.TriggerDeathReplan) {
		at.logWarnf("🗓️ death re-read %s %s v%d — REFUSED by preflight (no fresh bars); the dormant plan stands.", tradeDate, session, row.Version)
		return
	}
	// Class-47 CUTOFF only (SAFETY rule — kept), exactly like the flip read.
	dec := WakeCadenceDecision{
		Session: session, Desc: "death re-read: " + killer,
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
	// W-ONE-BUTTON M2.1 (review F15/N7): the maintenance hold refuses here,
	// before the launch clock, the wake timestamp and the in-flight claim.
	if at.refusePlannerClaimWhileHeld(store.MakePlanIDForTrader(at.id, tradeDate, session), "death re-read") {
		return
	}
	// The two LOAD rules a level wake obeys are computed only to SAY that the
	// exemption applied (never to refuse) — a death re-read is a reaction to a
	// machine-confirmed kill, like the flip read.
	if exempt := flipRereadExemptionNote(now, at.lastPlannerWakeAt, cfg.WakeMinIntervalMinutes(), at.lastWakeAuthoredVersionAge(now, tradeDate, session), wakeCooldownMinutes()); exempt != "" {
		at.logWarnf("🗓️ death re-read %s %s v%d — immediate (reaction reads are exempt from cooldown/min-interval; cutoff + stream guard still apply): %s",
			tradeDate, session, row.Version, exempt)
	}
	// Self-backoff only: a previous death LAUNCH for this row that wrote
	// nothing holds the retry for wake_min_interval_min, measured from that
	// launch. Shares the launch map with the flip read, keyed per kind.
	if v, ok := at.flipRereadLaunchAt.Load(inflightKey); ok {
		if last, isT := v.(time.Time); isT && now.Sub(last) < time.Duration(cfg.WakeMinIntervalMinutes())*time.Minute {
			at.logWarnf("🗓️ death re-read %s %s v%d — retry held: %.0fm since this row's last death launch that wrote nothing < wake_min_interval_min (%dm); refusals never start this clock.",
				tradeDate, session, row.Version, now.Sub(last).Minutes(), cfg.WakeMinIntervalMinutes())
			return
		}
	}
	if _, busy := deathRereadInFlight.LoadOrStore(inflightKey, now); busy {
		at.logInfof("🗓️ death re-read %s %s v%d already in flight — not launching a second.", tradeDate, session, row.Version)
		return
	}
	at.flipRereadLaunchAt.Store(inflightKey, now)
	at.lastPlannerWakeAt = now

	oldBias := ""
	if doc, derr := kernel.ParsePlanDoc(row.Doc); derr == nil {
		oldBias = doc.Bias.Direction
	} else {
		var raw kernel.PlanDoc
		if json.Unmarshal([]byte(row.Doc), &raw) == nil {
			oldBias = raw.Bias.Direction
		}
	}
	prior := deathRereadPriorLine(row.Version, oldBias, killer, priceAtDeath)
	at.logWarnf("🗓️ death re-read %s %s v%d — waking the planner (W-DEATH-REREAD, budget %d/%d): %s", tradeDate, session, row.Version, budget.Used, budget.Cap, killer)
	// Non-fatal and async, exactly like the flip read: a read that does not
	// land a newer active version keeps the dormant plan and clears the
	// once-key so the dormant branch retries next cycle.
	go func() {
		defer deathRereadInFlight.Delete(inflightKey)
		// The row may have been re-armed between the dormant write and this
		// goroutine's first instruction. Read it back and skip.
		if cur, gerr := at.store.Plan().GetPlan(row.PlanID, row.Version); gerr != nil || cur == nil || cur.Lifecycle != "dormant" {
			lc := "unreadable"
			if cur != nil {
				lc = cur.Lifecycle
			}
			at.logWarnf("🗓️ death re-read %s %s v%d — SKIPPED before the read: the row is %q, no longer dormant; nothing authored, the once-key stays clear.", tradeDate, session, row.Version, lc)
			return
		}
		if !deathRereadRun(at, session, tradeDate, prior, row, false) {
			at.logWarnf("🗓️ death re-read %s %s v%d did not complete — the dormant plan stands; the once-key is cleared for a retry next cycle.", tradeDate, session, row.Version)
			_ = at.store.SetSystemConfig(deathRereadDoneKey(row), "0")
			return
		}
		// Success is what the STORE says, not the bool above. The class-35
		// spend is recorded by the write site when the death_replan row lands.
		fresh, fErr := at.store.Plan().GetLatestPlanForTraderSession(tradeDate, session, at.id)
		if fErr != nil || fresh == nil || fresh.Version <= row.Version || fresh.Lifecycle != "active" {
			at.logWarnf("🗓️ death re-read %s %s v%d wrote NO new version — the dormant plan stands; the once-key is cleared for a retry next cycle", tradeDate, session, row.Version)
			_ = at.store.SetSystemConfig(deathRereadDoneKey(row), "0")
			return
		}
		_ = at.store.SetSystemConfig(deathRereadDoneKey(row), fmt.Sprintf("%d", now.UnixMilli()))
		at.flipRereadLaunchAt.Delete(inflightKey)
		if n, cerr := store.IncDeathReread(at.store, at.id, tradeDate, session); cerr == nil {
			at.logInfof("🗓️ death re-read %s %s: counter death_reread:%s:%s:%s=%d (recorded on the landed v%d).", tradeDate, session, at.id, tradeDate, session, n, fresh.Version)
		}
		// (a) supersede ONLY from dormant, with the death marker — the flip
		// path writes superseded:flip; this one writes superseded:death.
		moved, uerr := at.store.Plan().UpdatePlanLifecycleIf(row.PlanID, row.Version, "dormant", "superseded:death", fmt.Sprintf("superseded:death:v%d", fresh.Version))
		switch {
		case uerr != nil:
			at.logWarnf("🗓️ death re-read: supersede write failed for v%d: %v", row.Version, uerr)
		case !moved:
			at.logWarnf("🗓️ death re-read: v%d was RE-ARMED meanwhile (no longer dormant) — supersede REFUSED; the re-armed v%d and the new v%d both stand as written, and the newest version (v%d) governs at read time.", row.Version, row.Version, fresh.Version, fresh.Version)
		default:
			at.logInfof("🗓️ plan %s %s v%d SUPERSEDED by the death re-read (new v%d).", tradeDate, session, row.Version, fresh.Version)
		}
		at.carryOwnerEditsInto(fresh.PlanID, row.Version, fresh.Version)
	}()
}

// deathRereadKillerDirection renders the break direction for the prompt line
// from the killer's close vocabulary; "unknown" when it carries neither.
func deathRereadKillerDirection(killer string) string {
	k := strings.ToLower(killer)
	switch {
	case strings.Contains(k, "close below"):
		return "down"
	case strings.Contains(k, "close above"):
		return "up"
	}
	return "unknown"
}
