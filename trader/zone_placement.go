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
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
)

// ── W-EXEC-TRUTH W3 — market_in_zone: FOLLOW THE PLAN, ENTER AROUND THE PRICE ─
//
// A market_in_zone leg is a LIMIT at the FAR edge of the planner's own
// economics.entry_zone (buy → zone high, sell → zone low), on the existing
// limit path (R1). Inside the zone it is marketable and fills at once; the
// exchange can never fill it beyond the far edge. The composer (armed pass)
// stamps the row's EntryPx = the far bound, rounded inward, so every existing
// gate judges the worst fill; this file is the executor half.
//
//	composer   zoneLegFor — the zone verdict at authoring (the D9 defence for
//	           owner overlays that skip the write-time check)
//	executor   placeZoneRow — the placement verdict against the live price
//	rest cap   zoneRestCap — a resting policy limit is cancelled after
//	           day_plan.zone_rest_max_min (measured from placed_at_ms, D16)
//	receipts   recordZoneFillReceipt — fill vs the wire limit, side-adjusted
//
// Legacy (policy "") and planned_order rows never reach any of this: their
// placement branch is byte-identical to the base.

// zoneBarStale (D18) — a placement verdict needs a PRESENT price. The executor
// price is the last 1m bar's close; on a live tape that bar opened within the
// last minute (forming) or two (the tape shows closed bars only). A last bar
// that OPENED more than 3 minutes before the pass is not the present — the
// verdict is unknown and nothing is placed (the same no-op law as the stop
// guard's UNKNOWN).
const zoneBarStale = 3 * time.Minute

// zoneVerdict is the executor's reading of the live price against a policy
// row's zone. UNKNOWN is the iota ZERO: an unset verdict can never place.
type zoneVerdict int

const (
	zoneUnknown     zoneVerdict = iota // no present price, a bad zone or a bad side → no-op
	zoneInside                         // lo ≤ price ≤ hi → the far-edge limit is marketable now
	zoneBeyond                         // past the far edge → the limit rests at the bound
	zoneShortOfZone                    // not yet at the zone → refuse, no cancel, the row stays armed
)

// String is the bare verdict code (the card reads it by prefix).
func (v zoneVerdict) String() string {
	switch v {
	case zoneInside:
		return "inside"
	case zoneBeyond:
		return "beyond"
	case zoneShortOfZone:
		return "short_of_zone"
	}
	return "unknown"
}

const zoneVerdictEps = 1e-9

// zonePlacementVerdict is pure and boundary-inclusive. Long: lo≤p≤hi inside,
// p>hi beyond, p<lo short_of_zone. Short mirrored: lo≤p≤hi inside, p<lo
// beyond, p>hi short_of_zone. Unknown on p≤0, a non-positive / inverted /
// non-finite zone, a side that is neither long nor short, or a stale last bar
// (lastBarOpenMs ≤ 0, or older than zoneBarStale at now).
func zonePlacementVerdict(price, lo, hi float64, side string, lastBarOpenMs int64, now time.Time) zoneVerdict {
	finite := func(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }
	if !finite(price) || !finite(lo) || !finite(hi) || price <= 0 || lo <= 0 || hi <= 0 || lo > hi {
		return zoneUnknown
	}
	if lastBarOpenMs <= 0 || now.UnixMilli()-lastBarOpenMs > zoneBarStale.Milliseconds() {
		return zoneUnknown
	}
	return zonePriceVerdict(price, lo, hi, side)
}

// zonePriceVerdict is the price-vs-zone half of zonePlacementVerdict, without
// the staleness leg (the live-bar sink judges a frame that is live by
// construction; the pass judges staleness).
func zonePriceVerdict(price, lo, hi float64, side string) zoneVerdict {
	if !(price > 0) || !(lo > 0) || !(hi > 0) || lo > hi || math.IsInf(price, 0) || math.IsInf(hi, 0) {
		return zoneUnknown
	}
	above := price > hi+zoneVerdictEps
	below := price < lo-zoneVerdictEps
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "long":
		if above {
			return zoneBeyond
		}
		if below {
			return zoneShortOfZone
		}
		return zoneInside
	case "short":
		if below {
			return zoneBeyond
		}
		if above {
			return zoneShortOfZone
		}
		return zoneInside
	}
	return zoneUnknown
}

// ── the pass scope (the strict nudge, D13) ──────────────────────────────────

// armedPassScope narrows ONE armed pass's PLACEMENT to one scenario and
// carries back what the pass did to it. nil = the scan and the event pass:
// every scenario, no report. The authoring gates run for every scenario; rows
// of other scenarios are skipped in the placement loop BEFORE armAdmitted, so
// a scoped pass never counts arm_not_admitted for them.
type armedPassScope struct {
	Scenario string
	Trigger  string // "nudge"

	placed bool
	limit  float64
	signal string
	reason string // the first verdict or refusal the pass reached for the scenario
}

// armedPassOpts are the FULL pass's options (maybeManageArmedOrdersAtOpts).
// The zero value is the scan's pass.
type armedPassOpts struct {
	// scope narrows PLACEMENT to one scenario (the strict nudge). Every
	// authoring gate still runs for every scenario and the admitted set is
	// computed exactly as for the scan; the scope filters rows after that and
	// before armAdmitted.
	scope *armedPassScope
}

// firstPassScope is the optional scope argument of runArmedPlacementAt.
func firstPassScope(scopes []*armedPassScope) *armedPassScope {
	if len(scopes) == 0 {
		return nil
	}
	return scopes[0]
}

// scenarioOrEmpty is the scoped scenario (a pass-level refusal is noted
// against it).
func (s *armedPassScope) scenarioOrEmpty() string {
	if s == nil {
		return ""
	}
	return s.Scenario
}

func (s *armedPassScope) skips(scenario string) bool {
	return s != nil && s.Scenario != "" && !strings.EqualFold(strings.TrimSpace(scenario), s.Scenario)
}

// note records the first reason the pass reached for the scoped scenario.
func (s *armedPassScope) note(scenario, why string) {
	if s == nil || s.skips(scenario) || s.reason != "" || strings.TrimSpace(why) == "" {
		return
	}
	s.reason = why
}

func (s *armedPassScope) notePlaced(scenario string, limit float64, sid string) {
	if s == nil || s.skips(scenario) {
		return
	}
	s.placed, s.limit, s.signal = true, limit, sid
}

// ── the composer half ───────────────────────────────────────────────────────

// zoneLeg is one market_in_zone leg's authoring verdict.
type zoneLeg struct {
	on         bool
	v          kernel.ZoneVerdict
	planned    float64 // the authored entry (PlannedEntryPx); the row's EntryPx is v.Far
	provenance string
}

// stamp writes the policy and its zone onto the ledger row (a legacy leg
// writes nothing — every W3 field stays absent).
func (z zoneLeg) stamp(row *store.ArmedOrderDB) {
	if !z.on || row == nil {
		return
	}
	lo, hi, planned := z.v.Lo, z.v.Hi, z.planned
	row.Policy = kernel.EntryPolicyMarketInZone
	row.ZoneLo, row.ZoneHi, row.PlannedEntryPx = &lo, &hi, &planned
	row.ZoneProvenance = z.provenance
}

// zoneLegFor judges one leg at authoring. A leg whose effective policy is not
// market_in_zone returns {on:false} and is untouched (legacy byte-identical).
// A market_in_zone leg with a missing or invalid zone is REFUSED (D9): logged
// once per change, counted, never admitted and so never placed — the
// write-time check lives in the planner hook; this is the defence for an
// overlay that skipped it. ok=false means "skip this leg".
func (at *AutoTrader) zoneLegFor(plan *kernel.ActivePlan, doc *kernel.PlanDoc, sc kernel.PlanScenario, li int, leg kernel.PlanArmLeg, cfg *store.StrategyConfig, scope *armedPassScope) (zoneLeg, bool) {
	if kernel.EffectiveArmPolicy(sc.Arm, &leg) != kernel.EntryPolicyMarketInZone {
		return zoneLeg{}, true
	}
	side := strings.ToLower(strings.TrimSpace(sc.Direction))
	tick := market.FuturesTickSize(at.futuresSymbol())
	if tick <= 0 {
		tick = 0.25
	}
	var dp *store.DayPlanConfig
	if cfg != nil {
		dp = cfg.DayPlan
	}
	maxPts, _ := store.ResolveZoneMaxPts(dp)
	v := kernel.ArmZoneVerdict(sc, leg.Entry, leg.Stop, leg.Target, side, tick, maxPts)
	if v.Code != "" {
		key := plan.PlanID + ":" + strconv.Itoa(plan.Version) + ":" + sc.ID + ":leg" + strconv.Itoa(li+1) + ":zone_authoring"
		detail := fmt.Sprintf("entry %.2f stop %.2f target %.2f side %s zone %s (max %.1f pts)", leg.Entry, leg.Stop, leg.Target, side, zoneText(sc), maxPts)
		if armRefusalChanged(&at.armRefusalLast, key, v.Code) {
			telemetry.IncGateBlock(at.id, "market_in_zone_zone_refused")
			shown := ""
			if at.store != nil {
				if n, err := store.IncArmRefusal(at.store, at.id, kernel.PlanTradeDateFor(plan), plan.Session, "market_in_zone:"+v.Code); err == nil {
					shown = fmt.Sprintf(" · market_in_zone:%s refusals this session: %d", v.Code, n)
				}
			}
			at.logWarnf("⚔️ arm REFUSED %s %s leg %d: market_in_zone:%s — %s; never placed (D9)%s", plan.Session, sc.ID, li+1, v.Code, detail, shown)
		}
		scope.note(sc.ID, "refused: market_in_zone:"+v.Code+": "+detail)
		return zoneLeg{}, false
	}
	geometryRefs := false
	if cfg != nil && cfg.DayPlan != nil {
		geometryRefs = cfg.DayPlan.GeometryRefIDsEnabled()
	}
	return zoneLeg{on: true, v: v, planned: leg.Entry, provenance: zoneProvenanceLabel(doc, sc, v.Lo, v.Hi, geometryRefs)}, true
}

// zoneAwareGateVerdict is armGateVerdictFor at each gate's WORST fill (D7),
// exactly as the write-time zone check (writeTimeZoneVerdicts) judges it: a
// market_in_zone leg is judged at the FAR bound (R:R) and, when that passes,
// again at the NEAR bound (min-SL). A legacy leg is ONE call, unchanged.
func (at *AutoTrader) zoneAwareGateVerdict(zl zoneLeg, sc kernel.PlanScenario, leg kernel.PlanArmLeg, bias string, snap map[string]kernel.StructureState, atr5m float64, minQuality string, cfg *store.StrategyConfig, session string, structural bool) string {
	g := at.armGateVerdictFor(sc, leg, bias, snap, atr5m, minQuality, cfg, session, structural)
	if g != "" || !zl.on {
		return g
	}
	near := leg
	near.Entry = zl.v.Near
	return at.armGateVerdictFor(sc, near, bias, snap, atr5m, minQuality, cfg, session, structural)
}

func zoneText(sc kernel.PlanScenario) string {
	if sc.Economics == nil || len(sc.Economics.EntryZone) != 2 {
		return "absent"
	}
	return fmt.Sprintf("%.2f–%.2f", sc.Economics.EntryZone[0], sc.Economics.EntryZone[1])
}

// scenarioHasZoneArm reports whether a scenario's DOC arm is enabled and any of
// its legs is market_in_zone (the nudge predicate and the event flag both read
// the doc, never the ledger: a wait_confirm leg has no row until it confirms).
func scenarioHasZoneArm(sc kernel.PlanScenario) bool {
	if sc.Arm == nil || !sc.Arm.Enabled {
		return false
	}
	if len(sc.Arm.Legs) == 0 {
		return kernel.EffectiveArmPolicy(sc.Arm, nil) == kernel.EntryPolicyMarketInZone
	}
	for i := range sc.Arm.Legs {
		if kernel.EffectiveArmPolicy(sc.Arm, &sc.Arm.Legs[i]) == kernel.EntryPolicyMarketInZone {
			return true
		}
	}
	return false
}

// ── the executor half ───────────────────────────────────────────────────────

// zonePass is the per-pass state runArmedPlacementAt hands the policy branch.
type zonePass struct {
	nt             *ntTrader.TCPTrader
	ledger         *store.ArmedOrderStore
	rows           []store.ArmedOrderDB
	bars           []market.Kline
	price          float64
	now            time.Time
	admitted       armAdmission
	held           bool
	holdReason     string
	contract       contractVerdict
	placedThisPass bool
	scope          *armedPassScope
}

func zoneKey(r store.ArmedOrderDB) string {
	return r.PlanID + ":" + strconv.Itoa(r.Version) + ":" + r.Scenario + ":leg" + strconv.Itoa(r.LegIndex+1) + ":zone"
}

func floatOr0(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// setZoneVerdict records the card's verdict — evaluated on every pass,
// WRITTEN only when it changes: the row read this pass carries the current
// verdict, so an unchanged one issues no statement at all (a no-op UPDATE
// still takes SQLite's write lock, and the event pass runs up to 1/s). The
// store's own `last_verdict <> ?` guard is the second layer.
func (at *AutoTrader) setZoneVerdict(p zonePass, r store.ArmedOrderDB, verdict string) {
	if p.ledger == nil || r.LastVerdict == verdict {
		return
	}
	wrote, err := p.ledger.SetLastVerdict(r.ID, verdict, p.now.UnixMilli())
	if err != nil {
		at.logWarnf("📌 zone verdict write failed %s leg %d: %v", r.Scenario, r.LegIndex+1, err)
		return
	}
	if !wrote {
		return
	}
	// CTO 1790187980085: each verdict CHANGE is one counted event per class and
	// one log line — counted from the store's own answer (it wrote), never from
	// the comparison above (counters record, never infer).
	class := zoneVerdictChangeClass(verdict)
	n, _ := store.IncSystemCounter(at.store, "market_in_zone:verdict:"+class)
	prev := r.LastVerdict
	if prev == "" {
		prev = "none"
	}
	at.logInfof("🧭 zone verdict %s leg %d: %s → %s (%s #%d)", r.Scenario, r.LegIndex+1, prev, verdict, class, n)
}

// zoneVerdictChangeClass is the counter class of a written verdict: the four
// placement verdicts by name, any "refused: …" as refused, anything else other.
func zoneVerdictChangeClass(v string) string {
	switch {
	case v == "inside", v == "beyond", v == "short_of_zone", v == "unknown":
		return v
	case strings.HasPrefix(v, "refused:"):
		return "refused"
	default:
		return "other"
	}
}

// noteZoneVerdictOnly evaluates and records the verdict of an armed
// market_in_zone row the pass does NOT place (a nudge's scope skipped it), so
// every pass — scan, event and nudge — evaluates every policy row's verdict.
// No log, no count, no placement: those belong to the pass that may place it.
func (at *AutoTrader) noteZoneVerdictOnly(ledger *store.ArmedOrderStore, r store.ArmedOrderDB, bars []market.Kline, price float64, now time.Time) {
	if r.Policy != kernel.EntryPolicyMarketInZone || r.State != store.StateArmed {
		return
	}
	var barOpenMs int64
	if len(bars) > 0 {
		barOpenMs = bars[len(bars)-1].OpenTime
	}
	v := zonePlacementVerdict(price, floatOr0(r.ZoneLo), floatOr0(r.ZoneHi), strings.ToLower(strings.TrimSpace(r.Side)), barOpenMs, now)
	at.setZoneVerdict(zonePass{ledger: ledger, now: now}, r, v.String())
}

// placeZoneRow places one armed market_in_zone row. Order: verdict → hold →
// armAdmitted (G1 + the one admission chain) → one contract / in-pass →
// slot → PlaceLimitEntry at the far bound with BeginPlacementEval. Returns
// true when the ledger row was STAMPED (the latch is pessimistic, like the
// stop path: once stamped, whether the frame reached NT8 is unknown), so the
// caller closes the pass and cancels the plan's other arms.
func (at *AutoTrader) placeZoneRow(p zonePass, r store.ArmedOrderDB, side string) bool {
	// W5 D6/D21 — a machine-sourced row is judged FIRST on its eligibility
	// deadline and its run epoch: never placed after the deadline, never placed
	// by a run that did not record it — terminal, named, counted
	// (picture_scenario_exec.go). A planner row (Source "") passes through.
	if at.pictureRowRefusedAtPlacement(p, r) {
		return false
	}
	// N-I (release #22 rescan): past the session's last-entry cutoff the row
	// can never place, so its zone verdict is neither computed nor COUNTED
	// (row 365 counted five short_of_zone WAITINGs after the cutoff refused
	// it). The admission chain refuses it here, logged and counted once as
	// last_entry. Mentor rows keep their own cutoff (admission).
	if !isMentorArmOrigin(r) {
		if _, past := at.entryBlockedByLastEntryAt(p.now); past {
			if ok, why := at.armAdmitted(r, side, p.price, p.now, p.admitted); !ok {
				at.setZoneVerdict(p, r, "refused: "+zoneVerdictClass(why))
				p.scope.note(r.Scenario, "refused: "+why)
				return false
			}
		}
	}
	lo, hi := floatOr0(r.ZoneLo), floatOr0(r.ZoneHi)
	var barOpenMs int64
	if len(p.bars) > 0 {
		barOpenMs = p.bars[len(p.bars)-1].OpenTime
	}
	v := zonePlacementVerdict(p.price, lo, hi, side, barOpenMs, p.now)
	key := zoneKey(r)
	switch v {
	case zoneUnknown:
		// D18 / the stopGuardUnknown law: nothing is cancelled, nothing placed.
		if armRefusalChanged(&at.armRefusalLast, key, v.String()) {
			shown := at.countStopEntryRefusal(r, "market_in_zone:unknown", p.now)
			age := "none"
			if barOpenMs > 0 {
				age = p.now.Sub(time.UnixMilli(barOpenMs)).Round(time.Second).String()
			}
			at.logWarnf("⚠️ armed %s leg %d market_in_zone NOT adjudicated — price %.2f zone %.2f–%.2f side %s last-bar age %s (stale > %s) — no placement, no cancel%s",
				r.Scenario, r.LegIndex+1, p.price, lo, hi, side, age, zoneBarStale, shown)
		}
		at.setZoneVerdict(p, r, v.String())
		p.scope.note(r.Scenario, "waiting: price unknown")
		return false
	case zoneShortOfZone:
		word := "below"
		if strings.EqualFold(side, "short") {
			word = "above"
		}
		if armRefusalChanged(&at.armRefusalLast, key, v.String()) {
			shown := at.countStopEntryRefusal(r, "market_in_zone:short_of_zone", p.now)
			at.logWarnf("⏳ armed %s leg %d market_in_zone WAITING — price %.2f %s zone %.2f–%.2f (%s limit %.2f would fill outside the zone); the row stays armed%s",
				r.Scenario, r.LegIndex+1, p.price, word, lo, hi, strings.ToUpper(side), r.EntryPx, shown)
		}
		at.setZoneVerdict(p, r, v.String())
		p.scope.note(r.Scenario, fmt.Sprintf("waiting: price %s zone %.2f–%.2f (last %.2f)", word, lo, hi, p.price))
		return false
	}
	// inside | beyond — the far-edge limit is sendable. The dedupe value moves,
	// so a later short_of_zone / unknown is stated again.
	armRefusalChanged(&at.armRefusalLast, key, v.String())
	// WAVE PLANNER B1 — the reach gate: a beyond row whose zone is farther
	// than the placement bound from the eval price stays armed-unplaced (no
	// rest clock — placed_at_ms is stamped only at placement) and places on a
	// later pass when price comes within the bound. The bound is
	// day_plan.zone_place_within_pts (nil → 25 = the armed placement band;
	// 0 = OFF = legacy). An inside row is inside the zone by definition, and
	// short_of_zone already waits.
	if v == zoneBeyond {
		bound, _ := store.ResolveZonePlaceWithinPts(at.dayPlanCfg())
		if bound > 0 {
			dist := math.Min(math.Abs(p.price-lo), math.Abs(p.price-hi))
			if dist > bound {
				if armRefusalChanged(&at.armRefusalLast, key, v.String()+":beyond_proximity") {
					shown := at.countStopEntryRefusal(r, "market_in_zone:beyond_proximity", p.now)
					at.logWarnf("🚫 armed %s leg %d market_in_zone WAITING — price %.2f is %.1f pts beyond the zone %.2f–%.2f (> %.1f zone_place_within_pts); the row stays armed-unplaced, no rest clock%s",
						r.Scenario, r.LegIndex+1, p.price, dist, lo, hi, bound, shown)
				}
				at.setZoneVerdict(p, r, fmt.Sprintf("beyond: waiting — %.1f pts beyond the zone > %.1f proximity bound (zone_place_within_pts)", dist, bound))
				p.scope.note(r.Scenario, fmt.Sprintf("waiting: beyond %.1f pts > %.1f", dist, bound))
				return false
			}
		}
	}
	if p.held {
		at.refuseMaintenanceHold(r, p.holdReason, "zone limit", p.now, nil)
		at.setZoneVerdict(p, r, "refused: maintenance_hold: installation maintenance hold")
		p.scope.note(r.Scenario, "refused: maintenance_hold: "+p.holdReason)
		return false
	}
	if ok, why := at.armAdmitted(r, side, p.price, p.now, p.admitted); !ok {
		at.setZoneVerdict(p, r, "refused: "+zoneVerdictClass(why))
		p.scope.note(r.Scenario, "refused: "+why)
		return false
	}
	if !p.contract.Allowed() || p.placedThisPass {
		at.refuseContract(r, p.contract, p.placedThisPass, "zone limit", p.now)
		why := "one_contract: " + p.contract.Refusal()
		if p.placedThisPass {
			why = "one_contract: another arm in this plan reached the wire this pass"
		}
		at.setZoneVerdict(p, r, "refused: one_contract: account committed")
		p.scope.note(r.Scenario, "refused: "+why)
		return false
	}
	if g := at.armSlotGuard(p.rows, r, p.now); !g.Allowed() {
		at.refuseSlot(r, g, "zone limit", p.now)
		at.setZoneVerdict(p, r, "refused: slot: slot not free at the broker")
		p.scope.note(r.Scenario, "refused: slot: "+g.Refusal())
		return false
	}
	at.setZoneVerdict(p, r, v.String())
	stamped := false
	evalPx, now := p.price, p.now
	sid, perr := p.nt.PlaceLimitEntry(at.futuresSymbol(), side, 1, r.EntryPx, r.StopPx, r.TargetPx, func(sid string) error {
		if err := p.ledger.BeginPlacementEval(r.ID, sid, evalPx, barOpenMs, now.UnixMilli()); err != nil {
			return err
		}
		stamped = true // past this point a failure is a SEND failure — its fate is unknown
		return nil
	})
	recordResearchPlacement(r, sid, "limit", r.EntryPx, r.StopPx, r.TargetPx, perr)
	if perr != nil {
		if ntTrader.IsMaintenanceHold(perr) {
			at.refuseMaintenanceHold(r, perr.Error(), "zone limit", p.now, perr)
			p.scope.note(r.Scenario, "refused: maintenance_hold: "+perr.Error())
			return false
		}
		at.logWarnf("📌 armed %s leg %d market_in_zone place failed (verdict %s, limit %.2f): %v", r.Scenario, r.LegIndex+1, v, r.EntryPx, perr)
		p.scope.note(r.Scenario, "refused: send: "+perr.Error())
		return stamped
	}
	at.recordArmWirePrices(p.ledger, r, sid, side, r.EntryPx, r.StopPx, r.TargetPx, nil) // F-7: ledger = wire
	at.logInfof("📌 armed %s leg %d market_in_zone placement requested %s limit %.2f (zone %.2f–%.2f, verdict %s, price %.2f) signal=%s",
		r.Scenario, r.LegIndex+1, strings.ToUpper(side), r.EntryPx, lo, hi, v, p.price, sid)
	at.noteVWAPDriftAtPlacement(r, p.bars, p.now) // R22 P2: observability only — the entry is unchanged
	p.scope.notePlaced(r.Scenario, r.EntryPx, sid)
	return true
}

// zoneVerdictClass keeps the stored "refused: <class>: <text>" free of moving
// numbers (the verdict is written only on change): the refusal's own leading
// token is the class, and the text is dropped past it.
func zoneVerdictClass(why string) string {
	why = strings.TrimSpace(why)
	if i := strings.Index(why, ":"); i > 0 {
		return why[:i] + ": " + strings.TrimSpace(firstClause(why[i+1:]))
	}
	return "admission: " + why
}

func firstClause(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{" — ", " (", "; "} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
		}
	}
	return s
}

// ── the rest cap (D16) ──────────────────────────────────────────────────────

// zoneRestCap ends a market_in_zone limit that has rested past
// day_plan.zone_rest_max_min, measured from placed_at_ms (updated_at is
// rewritten by every pass). The cancel mirrors the withdraw: the filled-arm
// guard first, then the wire cancel. WAVE PLANNER B1 (P1 fold, CTO #213):
// with zone_place_within_pts ON the expiry REQUESTS the cancel exactly like
// the legacy path — cancel_pending with the signal id KEPT — and only when
// the broker book CONFIRMS it (confirmPendingCancels) does the row return to
// armed-unplaced (seq+1, re-placeable) under a NEW signal. A failed wire
// send leaves the row cancel_pending with its signal id and the settlement's
// re-request loop retries it; a fill during cancel_pending attributes to the
// row as today (the signal id was never dropped). The knob OFF (0) keeps the
// legacy cancel_pending terminal-for-version path byte-identical.
// Legacy rows are never touched.
func (at *AutoTrader) zoneRestCap(nt *ntTrader.TCPTrader, ledger *store.ArmedOrderStore, rows []store.ArmedOrderDB, now time.Time) {
	if nt == nil || ledger == nil {
		return
	}
	// W5 D6 — a Picture row's rest is bounded by ITS eligibility deadline, not
	// by zone_rest_max_min: a resting Picture limit past the deadline gets its
	// cancel requested here, beside the rest cap, and the rest cap skips it.
	at.pictureDeadlineCancel(nt, ledger, rows, now)
	maxMin, src := store.ResolveZoneRestMaxMin(at.dayPlanCfg())
	if maxMin <= 0 {
		return
	}
	limit := time.Duration(maxMin) * time.Minute
	for _, r := range rows {
		if r.TraderID != at.id || r.Policy != kernel.EntryPolicyMarketInZone || r.PlacedAtMs == nil ||
			strings.TrimSpace(r.SignalID) == "" || r.Source != "" {
			continue
		}
		if store.IsTerminalArmState(r.State) {
			continue
		}
		if r.State == store.StateArmed {
			continue
		}
		if r.State == store.StateCancelPending {
			continue
		}
		rested := now.Sub(time.UnixMilli(*r.PlacedAtMs))
		if rested <= limit {
			continue
		}
		if v := at.cancelSafetyFor(r, now); !v.Allow {
			if at.admitLast.changed("zone-rest|"+r.SignalID, v.Why) {
				at.logWarnf("🛟 zone rest cancel REFUSED %s leg %d signal=%s — %s", r.Scenario, r.LegIndex+1, shortID(r.SignalID), v.Why)
			}
			continue
		}
		if cerr := nt.CancelOrder(r.SignalID); cerr != nil {
			at.logWarnf("✕ zone rest cancel SEND failed %s leg %d signal=%s: %v", r.Scenario, r.LegIndex+1, shortID(r.SignalID), cerr)
		}
		bound, _ := store.ResolveZonePlaceWithinPts(at.dayPlanCfg())
		if bound <= 0 {
			// knob OFF — the legacy dismantle, byte-identical to pre-B1.
			if err := ledger.RequestCancel(r.ID, "zone rest expired", now.UnixMilli()); err != nil {
				at.logWarnf("✕ zone rest: ledger write failed for %s: %v", r.Scenario, err)
				continue
			}
			shown := ""
			if at.store != nil {
				if n, err := store.IncSystemCounter(at.store, "market_in_zone:rest_expired"); err == nil {
					shown = fmt.Sprintf(" · rest expiries recorded: %d", n)
				}
			}
			at.logWarnf("⏱ zone rest expired: %s leg %d limit %.2f signal=%s rested %s > %d min (%s) — cancel REQUESTED, pending broker confirmation; the arm is done for this plan version%s",
				r.Scenario, r.LegIndex+1, r.EntryPx, shortID(r.SignalID), rested.Round(time.Second), maxMin, src, shown)
			continue
		}
		// WAVE PLANNER B1 (P1 fold, CTO #213 review): the expiry REQUESTS the
		// cancel exactly like the legacy path — cancel_pending with the signal
		// id KEPT. A failed wire send above leaves the row's order at the
		// broker and the settlement's re-request loop retries it; a fill
		// during cancel_pending attributes to the row as today. Only when the
		// broker book CONFIRMS the cancel (confirmPendingCancels →
		// zoneRestReArmOnConfirm) does the row return to armed-unplaced
		// (placement stamp cleared, seq+1) for a NEW signal on the next
		// placement. It ends terminal only on invalidation (E1/E2, deadline)
		// or window close. The filled/stopped re-arm loop (W3) is untouched.
		if err := ledger.RequestCancel(r.ID, zoneRestReArmReason, now.UnixMilli()); err != nil {
			at.logWarnf("✕ zone rest: ledger write failed for %s: %v", r.Scenario, err)
			continue
		}
		shown := ""
		if at.store != nil {
			if n, err := store.IncSystemCounter(at.store, "market_in_zone:rest_expired"); err == nil {
				shown = fmt.Sprintf(" · rest expiries recorded: %d", n)
			}
		}
		at.logWarnf("⏱ zone rest expired: %s leg %d limit %.2f signal=%s rested %s > %d min (%s) — cancel REQUESTED, signal kept; on broker-book confirm the row returns to armed-unplaced (re-placeable within %.1f pts)%s",
			r.Scenario, r.LegIndex+1, r.EntryPx, shortID(r.SignalID), rested.Round(time.Second), maxMin, src, bound, shown)
	}
}

// zoneRestReArmReason is the RequestCancel reason the rest cap writes with
// zone_place_within_pts ON: the cancel must be CONFIRMED by the broker book
// before the row re-arms. confirmPendingCancels reads it back off the row —
// the reason is the only marker that travels with it.
const zoneRestReArmReason = "zone rest expired — re-arm on broker-book confirm"

// zoneRestReArmOnConfirm decides whether a CONFIRMED cancel is a zone-rest
// re-arm: the marker + a market_in_zone policy + the knob still ON at confirm
// time (it may have flipped OFF between the request and the confirm). It
// returns the reset reason; OFF at confirm time falls through to the legacy
// ConfirmCancel (terminal).
func (at *AutoTrader) zoneRestReArmOnConfirm(r store.ArmedOrderDB, confirmed string) (bool, string) {
	if !strings.Contains(strings.TrimSpace(r.StateReason), zoneRestReArmReason) {
		return false, ""
	}
	if r.Policy != kernel.EntryPolicyMarketInZone {
		return false, ""
	}
	bound, _ := store.ResolveZonePlaceWithinPts(at.dayPlanCfg())
	if bound <= 0 {
		return false, ""
	}
	return true, strings.TrimSpace(r.StateReason) + " — confirmed: " + confirmed + fmt.Sprintf(" (re-placeable within %.1f pts)", bound)
}

// ── the zone watch (the event pass trigger) ─────────────────────────────────

// zoneWatch is one armed policy row's zone and the verdict the last pass read,
// cached so the live-bar sink can tell a verdict CHANGE without a DB read.
type zoneWatch struct {
	lo, hi  float64
	side    string
	verdict zoneVerdict
}

// cacheZoneWatch stores the armed policy rows' zones after a full pass.
func (at *AutoTrader) cacheZoneWatch(rows []store.ArmedOrderDB, price float64, bars []market.Kline, now time.Time) {
	var barOpenMs int64
	if len(bars) > 0 {
		barOpenMs = bars[len(bars)-1].OpenTime
	}
	var out []zoneWatch
	for _, r := range rows {
		if r.TraderID != at.id || r.Policy != kernel.EntryPolicyMarketInZone || r.State != store.StateArmed {
			continue
		}
		side := strings.ToLower(strings.TrimSpace(r.Side))
		lo, hi := floatOr0(r.ZoneLo), floatOr0(r.ZoneHi)
		out = append(out, zoneWatch{lo: lo, hi: hi, side: side, verdict: zonePlacementVerdict(price, lo, hi, side, barOpenMs, now)})
	}
	at.zoneWatch.Store(out)
}

// ── receipts (D17) ──────────────────────────────────────────────────────────

// zoneFillReceipt computes a policy fill's receipt against the wire limit
// (the far bound, rounded inward at authoring = the wire's own number).
// slip = side-adjusted ticks, + = WORSE: long (fill − limit)/tick, short
// (limit − fill)/tick; nil when not computable (absent, never 0). beyondFar:
// the fill is past the far bound on the adverse side — the exchange enforces
// that bound, so it is a contradiction. pastNear: the fill is past the NEAR
// bound (an improvement, but OUTSIDE the zone — flagged, never labelled
// in-zone; the near side is not exchange-enforced).
func zoneFillReceipt(side string, fill, limit, lo, hi, tick float64) (slip *float64, beyondFar, pastNear bool) {
	if fill <= 0 || limit <= 0 || tick <= 0 {
		return nil, false, false
	}
	long := strings.EqualFold(strings.TrimSpace(side), "long")
	s := (fill - limit) / tick
	if !long {
		s = (limit - fill) / tick
	}
	s = math.Round(s*1e6) / 1e6
	slip = &s
	if lo > 0 && hi > 0 {
		if long {
			beyondFar, pastNear = fill > hi+zoneVerdictEps, fill < lo-zoneVerdictEps
		} else {
			beyondFar, pastNear = fill < lo-zoneVerdictEps, fill > hi+zoneVerdictEps
		}
	}
	return slip, beyondFar, pastNear
}

// recordZoneFillReceipt writes the fill receipt of a market_in_zone row
// (legacy rows: nothing, the columns stay NULL) and states a fill outside the
// zone out loud.
func (at *AutoTrader) recordZoneFillReceipt(ledger *store.ArmedOrderStore, r store.ArmedOrderDB, fill float64) {
	if ledger == nil || r.Policy != kernel.EntryPolicyMarketInZone {
		return
	}
	tick := market.FuturesTickSize(at.futuresSymbol())
	lo, hi := floatOr0(r.ZoneLo), floatOr0(r.ZoneHi)
	slip, beyondFar, pastNear := zoneFillReceipt(r.Side, fill, r.EntryPx, lo, hi, tick)
	if err := ledger.SetFillReceipt(r.ID, time.Now().UnixMilli(), slip); err != nil {
		at.logWarnf("⚡ zone fill receipt write failed %s leg %d: %v", r.Scenario, r.LegIndex+1, err)
	}
	slipText := "n/a"
	if slip != nil {
		slipText = strconv.FormatFloat(*slip, 'f', 2, 64) + "t"
	}
	planned := "n/a"
	if r.PlannedEntryPx != nil {
		planned = strconv.FormatFloat(*r.PlannedEntryPx, 'f', 2, 64)
	}
	switch {
	case beyondFar:
		n := 0
		if at.store != nil {
			n, _ = store.IncSystemCounter(at.store, "market_in_zone:fill_beyond_far")
		}
		at.logWarnf("🚨 zone fill CONTRADICTION %s leg %d: filled %.2f BEYOND the far bound (limit %.2f, zone %.2f–%.2f, slippage %s) — a limit cannot fill there; recorded n=%d",
			r.Scenario, r.LegIndex+1, fill, r.EntryPx, lo, hi, slipText, n)
	case pastNear:
		n := 0
		if at.store != nil {
			n, _ = store.IncSystemCounter(at.store, "market_in_zone:fill_near_side")
		}
		at.logWarnf("⚡ zone fill OUTSIDE the zone (near side, improved) %s leg %d: filled %.2f vs zone %.2f–%.2f (limit %.2f, slippage %s) — not an in-zone fill; recorded n=%d",
			r.Scenario, r.LegIndex+1, fill, lo, hi, r.EntryPx, slipText, n)
	default:
		at.logInfof("⚡ zone fill %s leg %d @ %.2f inside zone %.2f–%.2f · limit %.2f · slippage %s (+ = worse) · planned %s",
			r.Scenario, r.LegIndex+1, fill, lo, hi, r.EntryPx, slipText, planned)
	}
}
