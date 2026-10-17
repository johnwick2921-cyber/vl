package trader

import (
	"encoding/json"
	"fmt"
	"math"
	"nofx/kernel"
	"nofx/store"
	ntTrader "nofx/trader/ninjatrader"
	"strings"
	"time"
)

func geometryNumber(v float64) *float64 { return &v }
func geometryFinite(v float64) bool     { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func usableGeometryZone(z kernel.LevelZone) bool {
	return z.Lo != nil && z.Hi != nil && geometryFinite(*z.Lo) && geometryFinite(*z.Hi) && *z.Lo > 0 && *z.Hi >= *z.Lo && !z.Incomplete && len(z.Sources) > 0
}
func geometryZoneNames(z kernel.LevelZone) []string {
	names := []string{}
	for _, s := range z.Sources {
		names = append(names, s.Label)
	}
	return names
}

// ResolveEntryGeometryZone reads frozen source provenance. Matching an arbitrary
// nearest price, rebuilding a historical map, and model-authored entry_zone are
// not substitutes for the machine snapshot. Ambiguous identity fails closed.
// This exported form is the LEGACY contract (no tf wildcard) and stays
// byte-identical to today; the executor resolves through ArmGeometryVerdict.
func ResolveEntryGeometryZone(doc *kernel.PlanDoc, sc kernel.PlanScenario) (int, string) {
	idx, why, _ := resolveEntryGeometryZone(doc, sc, false)
	return idx, why
}

// ArmGeometryVerdict is the STABLE executor contract (W-GEOMETRY-REFUSAL,
// 2026-09-18; also exported for DS-101's write-time feasibility): the
// executor's own geometry verdict for a scenario, resolving
// day_plan.geometry_reference_levels. geometryRefLevels=true → an EMPTY
// zone-source tf is a wildcard (VWAP-family sources carry tf "" against
// identity tf "1m"); false = today's exact-match behaviour.
func ArmGeometryVerdict(doc *kernel.PlanDoc, sc kernel.PlanScenario, geometryRefLevels bool) (int, string) {
	idx, why, _ := resolveEntryGeometryZone(doc, sc, geometryRefLevels)
	return idx, why
}

// resolveEntryGeometryZone resolves the scenario's frozen entry zone. The
// third return is the SYNTHESIZED entry band for a zero-width reference-line
// admission — a LOCAL copy with its edges set. F8 (2026-09-18 re-check): the
// shared PlanDoc.Zones map must NEVER be mutated — it is a pointer shared by
// every scenario, leg and shadow in the cycle, and the old in-place edge write
// made composed targets depend on scenario ORDER. Non-admission paths return
// nil.
func resolveEntryGeometryZone(doc *kernel.PlanDoc, sc kernel.PlanScenario, geometryRefLevels bool) (int, string, *kernel.LevelZone) {
	if doc == nil || doc.Zones == nil {
		return -1, "frozen_zone_map_missing", nil
	}
	if sc.LevelID == nil || *sc.LevelID == "" {
		return -1, "scenario_level_id_missing", nil
	}
	identityValue, valid := kernel.LevelByID(sc.LevelID, doc.IdentityLevels)
	if !valid && geometryRefLevels {
		// W-GEOMETRY-REFUSAL (b1): with the knob ON, a stable reference id
		// (anchor kind whose formation close was unknown at authoring) resolves
		// through the reference lookup. OFF = strict only, byte-identical.
		identityValue, valid = kernel.LevelByReferenceID(sc.LevelID, doc.IdentityLevels)
	}
	if !valid {
		return -1, "identity_not_valid_in_frozen_map", nil
	}
	identity := &identityValue
	match := -1
	wildcardMatch := false
	for i, z := range doc.Zones.Zones {
		for _, s := range z.Sources {
			if math.Abs(s.Price-identity.Price) > 1e-7 {
				continue
			}
			named := s.Label == identity.Label
			for _, name := range identity.Names {
				named = named || s.Label == name
			}
			if !named {
				continue
			}
			// W-GEOMETRY-REFUSAL (b2): with the knob ON an EMPTY source tf is a
			// wildcard (matches any identity tf); a non-empty tf must still match
			// exactly. OFF = today's exact-match behaviour byte-identical.
			if identity.TF != nil && *identity.TF != "" && s.TF != *identity.TF && !(geometryRefLevels && s.TF == "") {
				continue
			}
			if geometryRefLevels && s.TF == "" && identity.TF != nil && *identity.TF != "" {
				wildcardMatch = true // the wildcard was actually exercised
			}
			if match >= 0 && match != i {
				return -1, "entry_zone_ambiguous", nil
			}
			match = i
			break
		}
	}
	if match < 0 {
		return -1, "entry_source_not_in_frozen_zones", nil
	}
	if geometryRefLevels && wildcardMatch {
		// W-GEOMETRY-REFUSAL (F4, narrowed after the v9/v14 regressions): ONLY a
		// match made through the empty-tf wildcard is checked for a competing
		// zone, and only on the identity's PRIMARY label — the merged member
		// names (an SWG candidate that also wears EQL·1h/EQL·4h) are the
		// identity itself appearing under its aliases, not a second zone.
		for i, z := range doc.Zones.Zones {
			if i == match {
				continue
			}
			for _, s := range z.Sources {
				if math.Abs(s.Price-identity.Price) > 1e-7 || s.Label != identity.Label {
					continue
				}
				return -1, "entry_zone_ambiguous", nil
			}
		}
	}
	if !usableGeometryZone(doc.Zones.Zones[match]) {
		// W-GEOMETRY-REFUSAL (b1, the admission half): a matched NULL-WIDTH
		// reference LINE (ONH/ONL/RTH/VWAP-family — lo/hi nil, incomplete_width,
		// one source) is a real level with a real identity; the map just stores it
		// without a band. With the knob ON the line is admitted as a zero-width
		// band at the anchor, so the structural stop composes exactly like a zone
		// edge: stop = line − buffer (long), target = first distinct complete zone.
		// OFF = today's refusal byte-identical.
		if geometryRefLevels && referenceLineZone(doc.Zones.Zones[match]) {
			// F8 (CTO re-check 2026-09-18): compose the synthesized band from a
			// LOCAL COPY. doc.Zones is a pointer into the shared plan — writing
			// lo/hi in place made later scenarios compose targets against the
			// mutated map (scenario-order dependence).
			z := doc.Zones.Zones[match]
			a := z.Anchor
			z.Lo, z.Hi = &a, &a
			z.Incomplete = false
			return match, "", &z
		}
		return -1, "entry_zone_edges_or_provenance_unusable", nil
	}
	return match, "", nil
}

// referenceLineZone reports whether a frozen zone is a NULL-WIDTH reference
// LINE — lo/hi nil, incomplete_width, exactly one reference-anchor source at
// the anchor price. That is the shape ONH/ONL/RTH/VWAP-family lines take in
// the frozen map (BuildLevelZones marks any source without a width incomplete).
func referenceLineZone(z kernel.LevelZone) bool {
	if z.Anchor <= 0 || !z.Incomplete || z.Lo != nil || z.Hi != nil || len(z.Sources) != 1 {
		return false
	}
	s := z.Sources[0]
	if !kernel.ReferenceAnchorKind(string(s.Kind)) {
		return false
	}
	return s.Price > 0 && math.Abs(s.Price-z.Anchor) <= 1e-7
}

// FirstGeometryTarget consumes the map's already merged intervals. It performs
// NO second merge (which would violate the non-transitive rule). Distinct means
// strictly beyond the entry zone's far profit-side edge; all complete sourced
// zones qualify, independent of distance, R:R, grade or source count.
func FirstGeometryTarget(zones []kernel.LevelZone, entryIdx int, long bool) (int, string) {
	e := zones[entryIdx]
	best := -1
	for i, z := range zones {
		if i == entryIdx || !usableGeometryZone(z) {
			continue
		}
		if long && *z.Lo > *e.Hi {
			if best < 0 || *z.Lo < *zones[best].Lo {
				best = i
			}
		}
		if !long && *z.Hi < *e.Lo {
			if best < 0 || *z.Hi > *zones[best].Hi {
				best = i
			}
		}
	}
	if best < 0 {
		return -1, "no_distinct_complete_target"
	}
	return best, ""
}

// ComposeLevelFadeGeometry is used by the production arm seam and research
// harness. It freezes S/T before admission. No gate can modify either price.
// Quantity remains zero here; only the production path can authorize one after
// the remaining, unchanged entry gates have passed.
func ComposeLevelFadeGeometry(doc *kernel.PlanDoc, sc kernel.PlanScenario, leg kernel.PlanArmLeg, p store.StructuralStopPolicy, atr, tick, pointValue float64) store.StructuralGeometryRecord {
	idx, why, _ := resolveEntryGeometryZone(doc, sc, false)
	return composeGeometry(doc, sc, leg, p, atr, tick, pointValue, idx, why, nil)
}

// ComposeLevelFadeGeometryWith is ComposeLevelFadeGeometry resolving the
// geometry_reference_levels knob — the executor's arm path; the research
// harness keeps the legacy form byte-identical.
func ComposeLevelFadeGeometryWith(doc *kernel.PlanDoc, sc kernel.PlanScenario, leg kernel.PlanArmLeg, p store.StructuralStopPolicy, atr, tick, pointValue float64, geometryRefLevels bool) store.StructuralGeometryRecord {
	idx, why, band := resolveEntryGeometryZone(doc, sc, geometryRefLevels)
	return composeGeometry(doc, sc, leg, p, atr, tick, pointValue, idx, why, band)
}

// ComposeFrozenLevelFadeGeometry replays an already identified detector zone.
// Research supplies its frozen index; no synthetic planner identity is invented.
// The production wrapper resolves the canonical scenario ID before this core.
func ComposeFrozenLevelFadeGeometry(zones []kernel.LevelZone, idx int, side string, entry float64, p store.StructuralStopPolicy, atr, tick, pointValue float64) store.StructuralGeometryRecord {
	why := ""
	if idx < 0 || idx >= len(zones) || !usableGeometryZone(zones[idx]) {
		idx = -1
		why = "frozen_zone_unusable"
	}
	return composeGeometry(&kernel.PlanDoc{Zones: &kernel.LevelZoneMap{Zones: zones}}, kernel.PlanScenario{Direction: side}, kernel.PlanArmLeg{Entry: entry}, p, atr, tick, pointValue, idx, why, nil)
}

func composeGeometry(doc *kernel.PlanDoc, sc kernel.PlanScenario, leg kernel.PlanArmLeg, p store.StructuralStopPolicy, atr, tick, pointValue float64, idx int, why string, band *kernel.LevelZone) store.StructuralGeometryRecord {
	r := store.StructuralGeometryRecord{Side: strings.ToLower(sc.Direction), Entry: leg.Entry, Scenario: sc.ID, BufferSource: p.BufferSource, Calibration: p.Calibration, Percentile: p.Percentile}
	refuse := func(reason, detail string) store.StructuralGeometryRecord {
		r.Reason = reason
		r.Detail = detail
		return r
	}
	long := r.Side == "long"
	if (!long && r.Side != "short") || !geometryFinite(leg.Entry) || leg.Entry <= 0 || tick <= 0 || !geometryFinite(tick) || pointValue <= 0 || !geometryFinite(pointValue) {
		return refuse("no_provenance", "invalid_direction_entry_or_contract_spec")
	}
	// Freeze the same entry the existing execution adapter would send. This is
	// tick normalization, not an entry adjustment to improve the ratio.
	leg.Entry = ntTrader.RoundToTick(leg.Entry, tick)
	r.Entry = leg.Entry
	if idx < 0 {
		r.StopSource = "atr_fallback"
		r.StopSourceReason = why
		if atr > 0 && geometryFinite(atr) {
			s := leg.Entry - kernel.MinSLATRMult()*atr
			if !long {
				s = leg.Entry + kernel.MinSLATRMult()*atr
			}
			if long {
				s = math.Floor(s/tick) * tick
			} else {
				s = math.Ceil(s/tick) * tick
			}
			r.Stop = geometryNumber(s)
		}
		return refuse("no_provenance", why)
	}
	// F8 (2026-09-18): the zero-width admission's synthesized band is a LOCAL
	// copy. When it is present, compose against a LOCAL shallow copy of the
	// zones slice with the entry slot replaced — the target search must see the
	// band (stop composes from it, targets compare against it), while the shared
	// map keeps lo/hi nil for every other scenario, leg and shadow in the cycle.
	zones := doc.Zones.Zones
	z := zones[idx]
	if band != nil {
		zones = append([]kernel.LevelZone(nil), doc.Zones.Zones...)
		zones[idx] = *band
		z = *band
	}
	r.ZoneLo = z.Lo
	r.ZoneHi = z.Hi
	r.ZoneNames = geometryZoneNames(z)
	r.StopSource = "zone_edge"
	if !p.BufferKnown || !geometryFinite(p.BufferPoints) || p.BufferPoints <= 0 {
		return refuse("no_provenance", "buffer_missing_or_invalid")
	}
	r.Buffer = geometryNumber(p.BufferPoints)
	stop := math.Floor((*z.Lo-p.BufferPoints)/tick) * tick
	if !long {
		stop = math.Ceil((*z.Hi+p.BufferPoints)/tick) * tick
	}
	r.Stop = geometryNumber(stop)
	ti, why := FirstGeometryTarget(zones, idx, long)
	if ti < 0 {
		return refuse("no_target", why)
	}
	targetZone := zones[ti]
	r.TargetLo = targetZone.Lo
	r.TargetHi = targetZone.Hi
	r.TargetNames = geometryZoneNames(targetZone)
	target := math.Floor(*targetZone.Lo/tick) * tick
	if !long {
		target = math.Ceil(*targetZone.Hi/tick) * tick
	}
	r.Target = geometryNumber(target)
	d, g := leg.Entry-stop, target-leg.Entry
	if !long {
		d, g = stop-leg.Entry, leg.Entry-target
	}
	r.RiskPoints = geometryNumber(d)
	r.GainPoints = geometryNumber(g)
	if d <= 0 || g <= 0 || stop <= 0 {
		return refuse("invalid_geometry", fmt.Sprintf("risk=%.4f gain=%.4f stop=%.4f", d, g, stop))
	}
	if !p.CostKnown || !geometryFinite(p.CostPoints) || p.CostPoints < 0 {
		return refuse("no_provenance", "cost_model_missing_or_invalid")
	}
	net := g - p.CostPoints
	loss := (d + p.CostPoints) * pointValue
	r.NetGainPoints = geometryNumber(net)
	r.LossUSD = geometryNumber(loss)
	if net <= 0 {
		return refuse("net_nonpositive", fmt.Sprintf("gain=%.4f cost=%.4f net=%.4f", g, p.CostPoints, net))
	}
	if p.MinRR <= 0 || !geometryFinite(p.MinRR) {
		return refuse("no_provenance", "rr_policy_missing")
	}
	if g/d+1e-9 < p.MinRR {
		return refuse("rr", fmt.Sprintf("gain=%.4f risk=%.4f rr=%.6f min=%.6f", g, d, g/d, p.MinRR))
	}
	// Owner clarification 2026-09-13: the owner controls the DAILY loss limit.
	// Geometry reports planned contract exposure; the existing daily-loss and
	// other entry gates remain responsible for admission after this step.
	r.Detail = fmt.Sprintf("geometry_pass risk=%.4f gain=%.4f net=%.4f one_contract_loss=%.4f", d, g, net, loss)
	return r
}

type armStructuralContext struct {
	Doc        *kernel.PlanDoc
	Scenario   kernel.PlanScenario
	Leg        kernel.PlanArmLeg
	Policy     store.StructuralStopPolicy
	PointValue float64
	// GeometryRefIDs (W-GEOMETRY-REFUSAL, 2026-09-18) — the resolved
	// day_plan.geometry_reference_levels knob; true = empty source tf wildcard.
	GeometryRefIDs bool
}

func (at *AutoTrader) saveArmGeometry(r store.StructuralGeometryRecord) bool {
	if at.store == nil {
		at.logWarnf("🎯 geometry record unavailable: %s %s", r.Scenario, r.Reason)
		return false
	}
	if err := at.store.SaveStructuralGeometry(r); err != nil {
		at.logWarnf("🎯 geometry record failed: %s %s: %v", r.Scenario, r.Reason, err)
		return false
	}
	// A composition is logged with all available numbers; no silent refusal.
	b, err := json.Marshal(r)
	if err != nil {
		at.logWarnf("🎯 geometry log failed: %v", err)
		return false
	}
	at.logInfof("🎯 stop/target composition %s", b)
	return true
}

// Retire unplaced authorizations and cancel only broker-confirmed entry orders
// through the existing safety predicate. A geometry refusal must not leave an
// older authorization eligible for the placement pass later in the same cycle.
func (at *AutoTrader) retireGeometryRefusal(plan *kernel.ActivePlan, sc kernel.PlanScenario, legIndex int, reason string, now time.Time) bool {
	if at.store == nil {
		return false
	}
	ledger := at.store.ArmedOrders()
	rows, err := ledger.ListNonTerminal(at.id)
	if err != nil {
		at.logWarnf("🎯 geometry refusal retirement unreadable: %v", err)
		return false
	}
	safe := true
	for _, r := range rows {
		if r.PlanID != plan.PlanID || r.Scenario != sc.ID || r.LegIndex != legIndex {
			continue
		}
		if r.State == store.StateArmed && r.SignalID == "" {
			if err := ledger.SetState(r.ID, store.StateCancelled, "geometry refusal: "+reason); err != nil {
				at.logWarnf("🎯 geometry retirement failed row=%d: %v", r.ID, err)
				safe = false
			}
			continue
		}
		if r.SignalID == "" || r.State == store.StateCancelPending {
			continue
		}
		if nt := at.armedTrader(); nt != nil {
			if v := at.cancelSafetyFor(r, now); !v.Allow {
				at.logWarnf("🛟 geometry cancellation refused row=%d: %s", r.ID, v.Why)
				safe = false
				continue
			}
			if err := nt.CancelOrder(r.SignalID); err != nil {
				at.logWarnf("🎯 geometry cancel send failed row=%d: %v", r.ID, err)
				safe = false
				continue
			}
			if err := ledger.RequestCancel(r.ID, "geometry refusal: "+reason, now.UnixMilli()); err != nil {
				at.logWarnf("🎯 geometry cancel record failed row=%d: %v", r.ID, err)
				safe = false
			}
		}
	}
	return safe
}
