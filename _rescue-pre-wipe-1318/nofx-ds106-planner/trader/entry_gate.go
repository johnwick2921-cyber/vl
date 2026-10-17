package trader

import (
	"fmt"
	"strings"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
	"nofx/telemetry"
	ntTrader "nofx/trader/ninjatrader"
)

// ── CLASS 48 — ONE ENTRY GATE FOR BOTH ORDER PATHS ──────────────────────────
//
// The five entry protections (R:R floor, shadow map 0C, scenario-direction
// consistency, min-SL ×ATR5m, one-live-arm) historically lived ONLY at the arm
// seam (armed_executor.go) while the decision path (auto_trader_orders.go →
// OpenLong market entry) ran a different, thinner chain and the agent chat
// path ran almost none. Live proof 2026-09-02: positions 587/589 filled BELOW
// the owner's 2.0 R:R floor at the real fill (the floor had been evaluated
// against the prompt-time snapshot), and 589/590 traded the SHADOWED condition
// breakout_retest, and 590 opened long citing a scenario that other versions
// authored short — all three admitted by the decision path.
//
// EntryGate is THE one canonical check. Both seams call it before any order
// leaves. The callers resolve every input (the function itself is pure and
// testable): the arm seam feeds arm-leg prices, the decision path feeds the
// LIVE market price at execution time (which is what actually fixes the
// snapshot mismatch — the floor is now judged at the price the order will
// transact at).
//
// Fail-open contract (mirrors validateDecision): a leg whose input is missing
// (no market price, no ATR, no plan, no shadow resolver) SKIPS. The gate
// refuses only on POSITIVE evidence, never on absence of evidence.

// EntryIntent is the resolved input to EntryGate.
type EntryIntent struct {
	Path   string // "arm" | "decision" — recorded per path on refusal
	Action string // "open_long" | "open_short"
	Symbol string

	Entry  float64 // the price the entry will actually transact at (arm leg entry / live market price)
	Stop   float64
	Target float64

	ATR5m                   float64
	MinRR                   float64 // resolved min_risk_reward_ratio — ONE floor for BOTH paths (R1, 2026-09-03; ARM_MIN_RR deleted)
	MinSLMult               float64 // resolved MIN_SL_ATR_MULT (0 = leg off)
	StructuralStopValidated bool    // only the min-SL admission leg changes role

	// Plan context (all optional — legs skip on absence).
	PlanBias      string // resolved plan bias ("long"/"short"/"")
	PlanMode      string // "advisory" | "direction" | "strict"
	CitedScenario string // the scenario the intent cites ("" = none)
	ScenarioDir   string // the cited scenario's direction ("long"/"short"/"")
	ScenarioCond  string // the cited scenario's condition ("" = none)

	// ONE OPEN POSITION PER INSTRUMENT (owner ruling 2026-09-03). "" / 0 = flat.
	//
	// The predecessor leg refused only the OPPOSITE side, because on a netting
	// account an opposite-side fill silently nets the position; a same-side add
	// was "outside this guard's scope". The same-version re-arm block lives in
	// the store, so a NEW plan version could re-authorize a terminal row and
	// add to a position that was still open. The ruling closes both: while any
	// position is open, every open is refused, whatever the side and version.
	//
	// The id/version/scenario ride along so the refusal names WHAT is open
	// rather than merely that something is.
	OpenPositionSide     string
	OpenPositionID       int64
	OpenPositionVersion  int
	OpenPositionScenario string

	// IsExitLeg marks an arm explicitly authored to FLATTEN the open position.
	// It is not an open, and refusing it would strand the position.
	IsExitLeg bool

	// Shadow resolver (nil = shadow leg off, fail-open).
	ConditionShadowed func(condition string) bool

	// DAILY GUARDRAIL resolver (nil = leg off, fail-open). Returns the daily
	// force-flat trip reason, or "" when the daily loss limit has not tripped.
	// Set on BOTH paths so a tripped limit closes the arm seam too — before
	// this wave the arm path never consulted the guardrail at all.
	DailyForceFlat func() string

	// NO-CHASE (2026-09-02) — the level the scenario CITES and its most recent
	// touch, so the gate can ask how far this entry sits from the thing it
	// claims to be trading. Zero values mean "not known": the leg abstains
	// rather than inventing a distance.
	CitedLevelPx   float64
	CitedLevelKind string
	LastTouchPx    float64
	HasTouch       bool
	// OnNoChase receives the leg's measurement. WARN-FIRST: the gate NEVER
	// refuses on it in this wave; the callback records and logs.
	OnNoChase func(NoChaseVerdict)

	// INVALIDATION (owner ruling 2026-09-03) — the system's OWN verdict, made
	// execution-wired.
	//
	// On 2026-09-03 the evaluator logged "🎯 scenario S1 → ≈invalidated @
	// 29285.00 (price accepted through the level against the trade —
	// display-only estimate, never execution-wired)" at 08:50:54, and the arm
	// seam armed that same S1 short at 29285 twelve minutes later. It filled,
	// stopped at the widened stop, and cost $140. The label was accurate: no
	// leg read the conclusion the system had already reached.
	//
	// The resolver calls the SAME evaluator the display path uses — never a
	// second copy of the predicate. nil = leg off, which is how the decision
	// path stays out of scope. ok=false = the evaluator could not run (no
	// bars): the leg PASSES and OnInvalidationUnavailable is told, because an
	// unresolved check is not a refusal.
	ScenarioInvalidation      func(scenarioID string) (InvalidationVerdict, bool)
	OnInvalidationUnavailable func(note string)
}

// InvalidationVerdict is the evaluator's conclusion about one scenario.
type InvalidationVerdict struct {
	Invalidated bool
	AtCT        string  // when the verdict was reached, CT wall clock "HH:MM"
	Anchor      float64 // the level price the tape accepted through
	Reason      string  // the evaluator's own words
}

// openPositionLabel names what is open, for the refusal. A version of 0 means
// the column was never stamped (rows written before the attribution wave) and
// renders as such, never as "v0".
func openPositionLabel(in EntryIntent) string {
	side := strings.ToLower(strings.TrimSpace(in.OpenPositionSide))
	ver := fmt.Sprintf("v%d", in.OpenPositionVersion)
	if in.OpenPositionVersion <= 0 {
		ver = "version not recorded"
	}
	sc := in.OpenPositionScenario
	if sc == "" {
		sc = "no cited scenario"
	}
	if in.OpenPositionID > 0 {
		return fmt.Sprintf("position %d open (%s %s %s)", in.OpenPositionID, ver, sc, side)
	}
	return fmt.Sprintf("a %s position is open (%s %s)", side, ver, sc)
}

// StrictNonArmRefusalPrefix is leg 0's strict refusal of a non-arm entry, up
// to the path name (the text is unchanged). W3's strict nudge keys on it: only
// THIS refusal — a matched decision refused for not being on the arm path —
// may trigger an armed pass; every other gate's refusal never does.
const StrictNonArmRefusalPrefix = "entry_gate: refused: strict — plan_mode=strict executes plan scenarios on the ARM path only"

// EntryGate runs the single canonical entry gate chain. Empty reason = allow.
func EntryGate(in EntryIntent) (reason string, refused bool) {
	side := strings.ToLower(strings.TrimSpace(in.Action))
	if side == "open_long" {
		side = "long"
	} else if side == "open_short" {
		side = "short"
	}
	if side != "long" && side != "short" {
		return "", false // not an entry intent — not this gate's job
	}

	// Leg D — DAILY LOSS LIMIT (wiring wave 2026-09-05). Runs BEFORE leg 0:
	// a tripped daily limit outranks every other reason to take or refuse an
	// entry, and its refusal is the one the journal should show.
	//
	// This leg is the half of the daily guardrail that was missing.
	// DailyGuardrails.Check() always returned RiskForceFlat on a daily-loss
	// trip, its only caller discarded the value, and this file contained
	// neither "daily" nor "guardrail" — so the limit stopped the decision cycle
	// and a RESTING ARM FILLED STRAIGHT THROUGH IT.
	//
	// Fail-open per the file's contract: a nil resolver, or an empty reason,
	// is not evidence of a trip and never refuses.
	if in.DailyForceFlat != nil {
		if why := in.DailyForceFlat(); strings.TrimSpace(why) != "" {
			return fmt.Sprintf("entry_gate: refused: daily_force_flat — %s (new entries blocked on the %s path until the daily window resets; open positions are not closed by this leg)", why, in.Path), true
		}
	}

	// Leg 0 — plan_mode STRICT (R4, OWNER RULING 2026-09-03). FIRST
	// IMPLEMENTATION, not a restoration: "strict" was listed in a doc comment
	// and never implemented — PlanModeFor returned it unchanged and no consumer
	// ever compared against it. This is therefore a NEW GATE, ruled by the
	// owner, and it runs FIRST so its refusal is the one the journal shows.
	//
	// Only plan scenarios execute: the arm path may proceed when it cites a
	// scenario and its side matches; the decision path's market entries are
	// refused outright.
	if in.PlanMode == "strict" {
		if in.Path != "arm" {
			return fmt.Sprintf(StrictNonArmRefusalPrefix+", and this is a %s-path market entry", in.Path), true
		}
		if strings.TrimSpace(in.CitedScenario) == "" {
			return "entry_gate: refused: strict — plan_mode=strict requires the entry to cite a plan scenario (none cited)", true
		}
		dir := strings.ToLower(strings.TrimSpace(in.ScenarioDir))
		if dir != "long" && dir != "short" {
			return fmt.Sprintf("entry_gate: refused: strict — scenario %s has no authored direction, so a strict entry cannot be matched to it", in.CitedScenario), true
		}
		if dir != side {
			return fmt.Sprintf("entry_gate: refused: strict — %s entry against scenario %s authored %s (plan_mode=strict)", side, in.CitedScenario, dir), true
		}
	}

	// Leg 1 — plan bias (direction mode only; the arm seam's legacy chain and
	// the decision path's planModeBlocked both already enforce this in
	// direction mode — kept here so the ONE gate is complete on its own).
	if in.PlanMode == "direction" {
		bias := strings.ToLower(strings.TrimSpace(in.PlanBias))
		if bias != "" && bias != side {
			return fmt.Sprintf("entry_gate: %s entry against plan bias %q (plan_mode=direction)", side, bias), true
		}
	}

	// Leg 2 — scenario-direction consistency (CLASS 48 core): an entry must
	// match the direction of the scenario it CITES. The decision path's
	// recordPlanCitation only ever logged this ("advisory, never gates") —
	// position 590-class refusals were impossible there.
	if in.CitedScenario != "" {
		dir := strings.ToLower(strings.TrimSpace(in.ScenarioDir))
		if dir == "long" || dir == "short" {
			if dir != side {
				return fmt.Sprintf("entry_gate: %s entry cites scenario %s authored %s — direction mismatch (class 48)", side, in.CitedScenario, dir), true
			}
		}
	}

	// Leg 3 — INVALIDATION (owner ruling 2026-09-03). ARM PATH ONLY: the
	// resolver is wired by entryGateForArm and left nil everywhere else.
	//
	// This runs before the pricing legs on purpose. R:R and min-SL ask whether
	// a trade is well-formed; this asks whether the setup still exists at all,
	// and a well-formed trade into a dead setup is the 09:02 arm.
	if in.Path == "arm" && in.CitedScenario != "" && in.ScenarioInvalidation != nil {
		v, ok := in.ScenarioInvalidation(in.CitedScenario)
		switch {
		case !ok:
			// Fail-open, loudly. An unresolved check is not a refusal.
			if in.OnInvalidationUnavailable != nil {
				in.OnInvalidationUnavailable(fmt.Sprintf(
					"entry_gate: invalidation check unavailable for %s — leg PASSED (no verdict is not a refusal)", in.CitedScenario))
			}
		case v.Invalidated:
			at := v.AtCT
			if at == "" {
				at = "an earlier cycle"
			}
			msg := fmt.Sprintf("entry_gate: scenario %s invalidated at %s", in.CitedScenario, at)
			if v.Anchor > 0 {
				msg += fmt.Sprintf(" (accepted through %.2f)", v.Anchor)
			}
			if v.Reason != "" {
				msg += " — " + v.Reason
			}
			return msg, true
		}
	}

	// Leg 4 — shadow map (0C owner ruling 2026-08-31): a shadowed condition is
	// authored + E8-scored but NEVER placed, on ANY path. The decision path had
	// no copy of this check — 589 and 590 both traded breakout_retest.
	if in.ScenarioCond != "" && in.ConditionShadowed != nil && in.ConditionShadowed(in.ScenarioCond) {
		return fmt.Sprintf("entry_gate: scenario %s condition %s is SHADOW (0C) — authored + E8-scored, never placed on any path", in.CitedScenario, in.ScenarioCond), true
	}

	// W1b E12(a) — legs 5 and 6 judge the prices the WIRE sends. The wire
	// rounds entry and target to the nearest tick and the stop AWAY from the
	// entry (ntTrader.WireBracket — the SAME function tcp_trader.go sends
	// with). Judging the continuous prices passed orders the broker then
	// received with a worse R:R (the wider stop) or a shorter stop distance
	// (an off-grid entry rounded toward the stop). A rounding that drops
	// either under its floor is now REFUSED here, never sent. Absent prices
	// (<= 0) stay absent: the legs below skip them exactly as before.
	wEntry, wStop, wTarget, wireNote := entryGateWirePrices(side, in)

	// Leg 5 — R:R at the REAL entry price (the fix for 587/589): the floor is
	// judged at the price the order will transact at, not the prompt snapshot.
	// entry<=0 or stop<=0 → skip (fail-open; validateDecision owns wrong-side).
	rr := 0.0
	ok := false
	if wEntry > 0 && wStop > 0 && wTarget > 0 && wEntry != wStop {
		if side == "long" && wEntry > wStop {
			rr = (wTarget - wEntry) / (wEntry - wStop)
			ok = true
		} else if side == "short" && wStop > wEntry {
			rr = (wEntry - wTarget) / (wStop - wEntry)
			ok = true
		}
	}
	if ok {
		floor := in.MinRR
		if floor <= 0 {
			floor = 3.0 // same fallback validateDecision uses for an unset knob
		}
		if rr+1e-9 < floor {
			return fmt.Sprintf("entry_gate: R:R %.2f below floor %.2f at execution price %.4f (SL %.4f TP %.4f)%s", rr, floor, wEntry, wStop, wTarget, wireNote), true
		}
	}

	// Leg 6 — min-SL ×ATR5m (same floor as both legacy chains).
	if !in.StructuralStopValidated && in.ATR5m > 0 && in.MinSLMult > 0 && wEntry > 0 && wStop > 0 {
		dist := wEntry - wStop
		if side == "short" {
			dist = wStop - wEntry
		}
		if dist+1e-9 < in.MinSLMult*in.ATR5m {
			return fmt.Sprintf("entry_gate: stop %.2f too close (%.2f < %.2f = %.1f×ATR5m)%s", wStop, dist, in.MinSLMult*in.ATR5m, in.MinSLMult, wireNote), true
		}
	}

	// Leg 7 — ONE OPEN POSITION PER INSTRUMENT (owner ruling 2026-09-03).
	// Was one-live-ARM (class 27 FIX 4), which refused only the opposite side.
	// Now: any open position refuses any open, either side, any plan version.
	// An explicitly authored exit leg is exempt — that is how a position is
	// flattened, and exits are out of this wave's scope.
	if in.OpenPositionSide != "" && !in.IsExitLeg {
		open := strings.ToLower(strings.TrimSpace(in.OpenPositionSide))
		if open == "long" || open == "short" {
			return fmt.Sprintf("entry_gate: %s entry refused: %s (one_open_position, owner ruling 2026-09-03); no adds, no flips",
				side, openPositionLabel(in)), true
		}
	}

	// NO-CHASE LEG (WARN-FIRST, refuses nothing — A24). It runs LAST so it
	// measures only intents every other leg allowed, and it runs on BOTH paths
	// because they share this function.
	if in.OnNoChase != nil {
		in.OnNoChase(EvaluateNoChase(NoChaseInputs{
			Entry: in.Entry, CitedLevel: in.CitedLevelPx, LevelKind: in.CitedLevelKind,
			LastTouchPx: in.LastTouchPx, HasTouch: in.HasTouch,
			ATR5m: in.ATR5m, MinSLMult: in.MinSLMult,
		}))
	}

	return "", false
}

// entryGateWirePrices returns the entry/stop/target legs 5 and 6 judge: the
// values ntTrader.WireBracket puts on the wire (W1b E12(a)). A price that is
// absent (<= 0) is returned as given so the legs keep skipping it. note is ""
// when rounding moved nothing (the refusal text is then byte-identical to the
// pre-E12 text); otherwise it names the authored prices the wire values came
// from, so a refusal caused by rounding says so. "Moved" is judged within the
// wire's tick epsilon (ntTrader.WireMoved, W1b FOLD-7): on a non-power-of-two
// tick an on-grid price rounds to a float a few ulps off (2000.3 on 0.10 →
// 2000.3000000000002), which is not a rounding and must not claim one.
//
// Only a CME futures symbol is rounded (verifier defect 1): the tick grid is
// the NT8 wire's, and a non-CME venue (a crypto trader still reaches EntryGate
// through admitEntry) fell through to the 0.25 index default — DOGEUSDT entry
// 0.12 rounded to 0 and legs 5/6 skipped a R:R 0.10 open that base refused.
// Any other symbol is judged on the authored prices, exactly as at base. The
// tick comes from ntTrader.InstrumentTickSize, the SAME root-resolving lookup
// the wire calls on the trader's symbol (verifier defect 4).
func entryGateWirePrices(side string, in EntryIntent) (entry, stop, target float64, note string) {
	entry, stop, target = in.Entry, in.Stop, in.Target
	if !market.IsCMEFuturesSymbol(in.Symbol) && market.FuturesRoot(in.Symbol) == "" {
		return entry, stop, target, ""
	}
	tick := ntTrader.InstrumentTickSize(in.Symbol)
	if in.Entry > 0 {
		entry = ntTrader.RoundToTick(in.Entry, tick)
	}
	if in.Stop > 0 {
		// side is long/short here (EntryGate returned early otherwise), so the
		// error branch is unreachable; on one, keep the authored stop.
		if ws, err := ntTrader.WireStop(side, in.Stop, tick); err == nil {
			stop = ws
		}
	}
	if in.Target > 0 {
		target = ntTrader.RoundToTick(in.Target, tick)
	}
	if ntTrader.WireMoved(in.Entry, entry, tick) || ntTrader.WireMoved(in.Stop, stop, tick) || ntTrader.WireMoved(in.Target, target, tick) {
		note = fmt.Sprintf(" — judged on the wire-rounded prices (authored entry %.4f SL %.4f TP %.4f)", in.Entry, in.Stop, in.Target)
	}
	return entry, stop, target, note
}

// ── Arm-seam builder ────────────────────────────────────────────────────────

// armSeamATR5mFromBars is the ONE ATR5m math for BOTH seams (no-trade-rider
// 2026-09-03): the arm seam's exact expression — 5m aggregate, ATR(14). The
// decision path MUST share this, never kernel.PlanDATRFor: SetPlanDATR stores
// the DAILY ATR (kernel/plan_render.go:370, `SetPlanDATR(traderID, dATR)`), and
// the 09-02 evening proved the difference — decision records 36640/36641/36642
// were refused against `1.5×ATR5m = 450.56` (dATR 300.4) while the arm seam in
// the same minutes used ATR5m 12.78-14.12. "One gate, two ATRs" is the bug.
func armSeamATR5mFromBars(bars []market.Kline) float64 {
	if len(bars) == 0 {
		return 0
	}
	return market.ExportCalculateATR(kernel.AcceptanceBars(bars, "2x5m"), 14)
}

// armSeamATR5m is the decision path's entry to the same resolver: fetch the
// AISVP bars exactly like the arm seam does, then share the same math.
func armSeamATR5m(symbol string) float64 {
	if market.FuturesBarsProvider == nil {
		return 0
	}
	return armSeamATR5mFromBars(market.FuturesBarsProvider(symbol, kernel.AISVPBarInterval, kernel.AISVPBarCount))
}

// entryGateForArm builds the intent for an arm leg and runs EntryGate. The arm
// chain's own gates (armGateVerdictFor, oneLiveArmGuard) run before this —
// EntryGate is the SAME function the decision path runs, so an arm can never
// be held to a weaker standard than a market entry. now is the armed pass's
// clock (maybeManageArmedOrdersAtOpts): leg 3 judges the bars closed at THAT
// instant, the same instant every other leg of the pass judges (W1b E3).
func (at *AutoTrader) entryGateForArm(plan *kernel.ActivePlan, sc kernel.PlanScenario, leg kernel.PlanArmLeg, side, biasDir string, atr5m float64, now time.Time, structural ...bool) (string, bool) {
	openSide, openID, openVer, openScenario := "", int64(0), 0, ""
	isExit := strings.EqualFold(strings.TrimSpace(leg.Kind), "exit")
	// ONE OPEN POSITION (2026-09-03): the position's identity rides into the
	// gate so the refusal names what is open. The exit-leg escape survives —
	// that is how a position is flattened.
	if !isExit && at.store != nil {
		opens, err := at.store.Position().GetOpenPositions(at.id)
		if err == nil {
			sym := market.Normalize(at.futuresSymbol())
			for _, p := range opens {
				// canon 28: writers store "LONG"/"SHORT" — read through the
				// canonicalizer, or leg 7 never sees the position (W-EXEC-TRUTH W0).
				if side := positionSide(p.Side); strings.EqualFold(p.Symbol, sym) && side != "" {
					openSide = side
					openID, openVer, openScenario = p.ID, p.PlanVersion, p.CitedScenarioID
					break
				}
			}
		}
	}
	session := plan.Session
	levelPx, levelKind := citedLevelFor(sc, plan)
	touchPx, hasTouch := at.lastTouchFor(levelPx)
	return EntryGate(EntryIntent{
		Path:           "arm",
		DailyForceFlat: func() string { return kernel.DailyForceFlatReason(at.id) },
		CitedLevelPx:   levelPx,
		CitedLevelKind: levelKind,
		LastTouchPx:    touchPx,
		HasTouch:       hasTouch,
		OnNoChase:      at.noChaseObserver("arm", sc.ID),
		// INVALIDATION (owner ruling 2026-09-03) — the arm path, and only it,
		// judged on the PASS clock every other leg of the pass reads (W1b E3).
		ScenarioInvalidation:      at.scenarioInvalidationResolverClock(plan, func() time.Time { return now }),
		OnInvalidationUnavailable: func(note string) { at.logWarnf("🛡 %s", note) },
		Action:                    "open_" + side,
		Symbol:                    at.futuresSymbol(),
		Entry:                     leg.Entry,
		Stop:                      leg.Stop,
		Target:                    leg.Target,
		ATR5m:                     atr5m,
		MinRR:                     at.armMinRRFor(nil), // R1: ONE floor — the Studio value, same as the decision path
		MinSLMult:                 kernel.MinSLATRMult(),
		StructuralStopValidated:   len(structural) == 1 && structural[0],
		PlanBias:                  biasDir,
		PlanMode:                  at.planModeFor(session),
		CitedScenario:             sc.ID,
		ScenarioDir:               sc.Direction,
		ScenarioCond:              sc.Condition,
		OpenPositionSide:          openSide,
		OpenPositionID:            openID,
		OpenPositionVersion:       openVer,
		OpenPositionScenario:      openScenario,
		IsExitLeg:                 isExit,
		ConditionShadowed:         func(cond string) bool { return at.conditionShadowedFor(cond, session) },
	})
}

// ── Decision-path builder ───────────────────────────────────────────────────

// entryGateForDecision builds the intent for an AI market entry and runs
// EntryGate. livePrice is the execution-time market price (the caller resolves
// it; ≤0 skips the R:R/min-SL legs, fail-open). All plan inputs resolve from
// the trader's ActivePlan; absent plan → those legs skip.
//
// W-EXEC-TRUTH W0: it takes the caller's clock (admitEntry passes it; the
// wall-clock wrapper had no production caller left and was removed).
func (at *AutoTrader) entryGateForDecisionAt(d *kernel.Decision, livePrice float64, now time.Time) (string, bool) {
	if d == nil {
		return "", false
	}
	openSide, openID, openVer, openScenario := "", int64(0), 0, ""
	if at.store != nil {
		opens, err := at.store.Position().GetOpenPositions(at.id)
		if err == nil {
			sym := market.Normalize(d.Symbol)
			for _, p := range opens {
				// canon 28: writers store "LONG"/"SHORT" — read through the
				// canonicalizer, or leg 7 never sees the position (W-EXEC-TRUTH W0).
				if side := positionSide(p.Side); strings.EqualFold(p.Symbol, sym) && side != "" {
					openSide = side
					// ONE OPEN POSITION (2026-09-03) — the identity, so the
					// refusal names what is open.
					openID, openVer, openScenario = p.ID, p.PlanVersion, p.CitedScenarioID
					break
				}
			}
		}
	}
	// R1 (owner ruling 2026-09-03) — ONE floor, one resolver. This used to
	// start at a hardcoded 3.0 and override it from the config, so a strategy
	// with no value silently got a DIFFERENT floor from the arm seam's 2.0.
	// Both paths now call the same resolver.
	minRR := at.armMinRRFor(nil)
	session := ""
	if s, ok := at.sessionRegistry(now).ActiveSession(now); ok {
		session = s.Name
	}
	intent := EntryIntent{
		Path:                 "decision",
		DailyForceFlat:       func() string { return kernel.DailyForceFlatReason(at.id) },
		Action:               d.Action,
		Symbol:               d.Symbol,
		Entry:                livePrice,
		Stop:                 d.StopLoss,
		Target:               d.TakeProfit,
		ATR5m:                armSeamATR5m(d.Symbol),
		MinRR:                minRR,
		MinSLMult:            kernel.MinSLATRMult(),
		PlanMode:             at.planModeFor(session),
		CitedScenario:        strings.TrimSpace(d.CitedScenario),
		OpenPositionSide:     openSide,
		OpenPositionID:       openID,
		OpenPositionVersion:  openVer,
		OpenPositionScenario: openScenario,
	}
	if kernel.HasTraderPlanProvider(at.id) {
		if ap := kernel.ActivePlanFor(at.id, at.futuresSymbol()); ap != nil {
			intent.PlanBias = strings.ToLower(strings.TrimSpace(ap.Doc.Bias.Direction))
			for _, sc := range ap.Doc.Scenarios {
				if sc.ID == intent.CitedScenario {
					intent.ScenarioDir = strings.ToLower(strings.TrimSpace(sc.Direction))
					intent.ScenarioCond = sc.Condition
					// NO-CHASE: the level this entry claims to be trading.
					intent.CitedLevelPx, intent.CitedLevelKind = citedLevelFor(sc, ap)
					intent.LastTouchPx, intent.HasTouch = at.lastTouchFor(intent.CitedLevelPx)
					break
				}
			}
		}
	}
	if intent.ScenarioCond != "" {
		intent.ConditionShadowed = func(cond string) bool { return at.conditionShadowedFor(cond, session) }
	}
	intent.OnNoChase = at.noChaseObserver("decision", intent.CitedScenario)
	return EntryGate(intent)
}

// recordEntryGateRefusal is the per-path refusal RECORD. The arm seam records
// into the arm-refusal counter family (per session-day, per class); the
// decision path records into decision_records via actionRecord + the gate-block
// counter. Both log.
func (at *AutoTrader) recordEntryGateRefusal(path, symbol, action, reason string, plan *kernel.ActivePlan) {
	class := armRefusalClass(reason)
	if class == "" {
		class = "entry_gate"
	}
	if path == "arm" && plan != nil && at.store != nil {
		if n, cerr := store.IncArmRefusal(at.store, at.id, kernel.PlanTradeDateFor(plan), plan.Session, "entry_gate:"+class); cerr == nil {
			at.logWarnf("🚦 entry-gate REFUSED arm %s: %s · refusals this session: %d", plan.Session, reason, n)
			return
		}
	}
	at.logWarnf("🚦 entry-gate REFUSED %s %s %s: %s", path, symbol, action, reason)
}

// entryGateDecisionTelemetry stamps the decision-path refusal (recorded in
// decision_records through actionRecord.Error + execution_log).
func entryGateDecisionTelemetry(at *AutoTrader, actionRecord *store.DecisionAction, reason string) {
	telemetry.IncGateBlock(at.id, "entry_gate")
	actionRecord.Success = false
	// D32 (corrected 2026-09-04): the counter above was never the missing half —
	// it has always fired. What was missing was the LINE. A decision-path
	// refusal was recorded in decision_records and the counter and said nothing
	// in the log, while the arm path logged its own 🚦, so a reader grepping
	// refusals saw one path and concluded the other was idle. Same marker as
	// the arm path so both are one grep (A9).
	at.logWarnf("🚦 entry-gate REFUSED decision-path: %s", reason)
	// The leg messages already carry the "entry_gate:" prefix — do not double it
	// (the 09-02 refusals read "entry_gate: entry_gate: …").
	if !strings.HasPrefix(reason, "entry_gate:") {
		reason = "entry_gate: " + reason
	}
	actionRecord.Error = reason
}
