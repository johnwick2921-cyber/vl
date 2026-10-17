package kernel

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// W-EXEC-TRUTH W2 — A3 (identity ≠ price = REFUSE) + A4 (obstacle-chain
// contract). CTO amendment msg 1790166603535; lane defaults ruled in msg
// 1790178967603 (+ clarification 1790178997353).
//
// Both are WRITE-TIME refusals on NEW authoring only. They run at the planner
// write loop and in the shadow A/B verdict — never in ValidatePlanDocWithCaps,
// which also reads stored and overlay documents: history is untouched, no
// stored id is rewritten, and no runtime lifecycle (buffers, windows, flip
// hold) changes. They are CORRECTIONS (no knob): the old behaviour — publish a
// scenario whose level_id names one level while its trigger trades another,
// or whose first obstacle skips a seated level — was the defect.

// Write-truth issue classes. Counters are keyed by these, never by price text.
const (
	WriteTruthIdentityPrice      = "identity_price"
	WriteTruthIdentityUnresolved = "identity_unresolved"
	WriteTruthAnchorReuse        = "anchor_reuse"
	WriteTruthAnchorUnrelated    = "anchor_unrelated"
	WriteTruthChainUnsorted      = "chain_unsorted"
	WriteTruthObstacleNotNearest = "obstacle_not_nearest"
	WriteTruthPathLevelMissing   = "path_level_missing"
	WriteTruthPathRoleInvalid    = "path_role_invalid"
	WriteTruthReduceQty1         = "reduce_qty1"
)

// WriteTruthClasses is the fixed order the boot line and counters print in.
func WriteTruthClasses() []string {
	return []string{WriteTruthIdentityPrice, WriteTruthIdentityUnresolved, WriteTruthAnchorReuse, WriteTruthAnchorUnrelated,
		WriteTruthChainUnsorted, WriteTruthObstacleNotNearest, WriteTruthPathLevelMissing, WriteTruthPathRoleInvalid, WriteTruthReduceQty1}
}

// WriteTruthIssue is one refused fact about one scenario.
type WriteTruthIssue struct {
	Scenario string
	Class    string
	Text     string // starts with the scenario id; the class phrase follows
}

// WriteTruthVerdict is the result of one write-time check over one document.
// IdentityChecked / ChainChecked are false when the frozen map was UNKNOWN
// (nil) — nothing was judged, which is not the same as nothing refused.
type WriteTruthVerdict struct {
	IdentityChecked bool
	ChainChecked    bool
	Issues          []WriteTruthIssue
	Normalized      []string // "S2 first_obstacle 31015.23→31015.25"
}

// Err joins every issue into the ONE rejection the retry loop reads. Each
// issue leads with its scenario id so the 120-char defect prefix
// (samePlannerDefect) carries the scenario and the level.
func (v WriteTruthVerdict) Err() error {
	if len(v.Issues) == 0 {
		return nil
	}
	parts := make([]string, 0, len(v.Issues))
	for _, is := range v.Issues {
		parts = append(parts, is.Text)
	}
	return fmt.Errorf("%s", strings.Join(parts, " | "))
}

// CheckScenarioWriteTruth runs A3 then A4 over a freshly authored document.
// seated is facts.IdentityMap (nil = UNKNOWN → both checks skipped);
// capacityCut is the pool the seat race dropped (accepted as a first obstacle,
// never required — the model never sees it). tick <= 0 (unknown instrument)
// skips A4. Authored obstacle-chain prices are normalized to the tick grid IN
// PLACE and each normalization is returned for recording.
func CheckScenarioWriteTruth(d *PlanDoc, seated, capacityCut []MapCandidate, tick float64) WriteTruthVerdict {
	var v WriteTruthVerdict
	if d == nil {
		return v
	}
	if seated == nil {
		// CTO note (msg 1790182338037): a nil frozen map means there is
		// nothing an id could name, so a NAMED id is refused as unresolved —
		// never skipped as UNKNOWN. With no ids the check has nothing to judge
		// (and the obstacle chain needs a map): UNKNOWN, not refused.
		v.Issues = nilMapNamedIDIssues(d)
		v.IdentityChecked = len(v.Issues) > 0
		return v
	}
	v.IdentityChecked = true
	v.Issues = append(v.Issues, scenarioIdentityWriteIssues(d, seated)...)
	if tick > 0 {
		v.ChainChecked = true
		issues, normalized := obstacleChainWriteIssues(d, seated, capacityCut, tick)
		v.Issues = append(v.Issues, issues...)
		v.Normalized = normalized
	}
	return v
}

// IdentityLevelsFromCandidates is the ONE projection of the frozen map into
// identity PlanLevels. StampAuthoredIdentity and the A3 write check share it,
// so the level a check resolves is the level the stored doc carries.
func IdentityLevelsFromCandidates(candidates []MapCandidate) []PlanLevel {
	if candidates == nil {
		return nil
	}
	out := make([]PlanLevel, 0, len(candidates))
	for _, c := range candidates {
		l := c.Identity
		l.Names = append([]string(nil), c.Names...)
		out = append(out, l)
	}
	return out
}

// resolveIdentity is the read-side pair LevelByID → LevelByReferenceID, the
// same order ResolveScenarioIdentity uses.
func resolveIdentity(id *string, levels []PlanLevel) (PlanLevel, bool) {
	if l, ok := LevelByID(id, levels); ok {
		return l, true
	}
	return LevelByReferenceID(id, levels)
}

// identityZone is the level's own zone [lo, hi] (a line level is [price,
// price]) — the A3 tolerance is that zone widened by the cluster tolerance.
func identityZone(l PlanLevel) (lo, hi float64) {
	lo, hi = l.Price, l.Price
	if l.Lo != nil && l.Hi != nil && *l.Lo > 0 && *l.Hi >= *l.Lo {
		lo, hi = math.Min(lo, *l.Lo), math.Max(hi, *l.Hi)
	}
	return lo, hi
}

// identityAgreesZoneAware is ResolveScenarioIdentity's Disagreed predicate
// (|price − anchor| > cluster tolerance) made zone-aware: an anchor inside the
// resolved level's own [lo − tol, hi + tol] agrees. For a line level the two
// predicates are identical.
func identityAgreesZoneAware(l PlanLevel, anchor float64) bool {
	tol := clusterToleranceFor(l.Price)
	lo, hi := identityZone(l)
	return anchor >= lo-tol-1e-9 && anchor <= hi+tol+1e-9
}

func identityLabel(l PlanLevel) string {
	if len(l.Names) > 0 && strings.TrimSpace(l.Names[0]) != "" {
		return l.Names[0]
	}
	if strings.TrimSpace(l.Label) != "" {
		return l.Label
	}
	if l.Kind != nil {
		return *l.Kind
	}
	return "level"
}

func shortIdentityID(id string) string {
	if len(id) > 16 {
		return id[:16] + "…"
	}
	return id
}

// identityAtPrice names the map level that DOES sit at a price (zone-aware,
// nearest first). It is the one level the rejection names — never a menu of
// ids (the closed-id menu is owner-gated A3b).
func identityAtPrice(levels []PlanLevel, price float64) (PlanLevel, bool) {
	best, found, bestD := PlanLevel{}, false, math.Inf(1)
	for _, l := range levels {
		if l.ID == nil || *l.ID == "" || !identityAgreesZoneAware(l, price) {
			continue
		}
		if d := math.Abs(l.Price - price); d < bestD {
			best, found, bestD = l, true, d
		}
	}
	return best, found
}

func authoredID(p *string) (string, bool) {
	if p == nil || strings.TrimSpace(*p) == "" {
		return "", false
	}
	return *p, true
}

// scenarioIdentityWriteIssues is A3. Per scenario:
//   - a level_id / sweep_level_id / reclaim_level_id that does not resolve in
//     the frozen map (invented, mangled, or a ref| id whose digest matches no
//     frozen identity) is REFUSED (CTO tightening msg 1790178967603);
//   - a resolved level_id whose level disagrees with the scenario's anchor
//     (ScenarioAnchor — the SAME anchor the recording and the evaluator use),
//     outside the level's own zone ± the cluster tolerance, is REFUSED —
//     unless it is one of the scenario's own validated two-anchor ids;
//   - two-anchor setups: sweep_level_id and reclaim_level_id must name two
//     DIFFERENT levels, each at its own leg (confirm.ref_price for the sweep,
//     confirm2.ref_price — else confirm.ref_price — for the reclaim).
//
// A NULL level_id stays the legacy unnamed WARN path; a scenario with no
// anchor the evaluator can read is not judged (UNKNOWN, never guessed).
func scenarioIdentityWriteIssues(d *PlanDoc, candidates []MapCandidate) []WriteTruthIssue {
	levels := IdentityLevelsFromCandidates(candidates)
	var out []WriteTruthIssue
	for _, sc := range d.Scenarios {
		legIDs := map[string]bool{}
		sweepID, hasSweep := authoredID(sc.SweepLevelID)
		reclaimID, hasReclaim := authoredID(sc.ReclaimLevelID)
		if hasSweep && hasReclaim && sweepID == reclaimID {
			out = append(out, WriteTruthIssue{sc.ID, WriteTruthAnchorReuse, fmt.Sprintf("%s identity≠price: sweep_level_id and reclaim_level_id are the same id %s — a two-anchor setup names two different levels; a sweep and reclaim of ONE level uses level_id alone", sc.ID, shortIdentityID(sweepID))})
			hasSweep, hasReclaim = false, false // the pair is refused as a whole
		}
		type leg struct {
			field string
			id    string
			has   bool
			ref   *PlanConfirm
			name  string
		}
		reclaimRef, reclaimName := sc.Confirm2, "confirm2.ref_price"
		if reclaimRef == nil {
			reclaimRef, reclaimName = sc.Confirm, "confirm.ref_price"
		}
		for _, lg := range []leg{
			{"sweep_level_id", sweepID, hasSweep, sc.Confirm, "confirm.ref_price"},
			{"reclaim_level_id", reclaimID, hasReclaim, reclaimRef, reclaimName},
		} {
			if !lg.has {
				continue
			}
			l, ok := resolveIdentity(&lg.id, levels)
			if !ok {
				out = append(out, WriteTruthIssue{sc.ID, WriteTruthIdentityUnresolved, fmt.Sprintf("%s identity unresolved: %s %q is not in the frozen map — copy the id of that leg's level from the map exactly", sc.ID, lg.field, lg.id)})
				continue
			}
			if lg.ref == nil || lg.ref.RefPrice <= 0 {
				continue // no leg price to compare — UNKNOWN, not refused
			}
			if !identityAgreesZoneAware(l, lg.ref.RefPrice) {
				out = append(out, WriteTruthIssue{sc.ID, WriteTruthAnchorUnrelated, fmt.Sprintf("%s identity≠price: %s %s names %s %.2f but that leg (%s) is %.2f — name the level at %.2f", sc.ID, lg.field, shortIdentityID(lg.id), identityLabel(l), l.Price, lg.name, lg.ref.RefPrice, lg.ref.RefPrice)})
				continue
			}
			legIDs[lg.id] = true
		}
		id, named := authoredID(sc.LevelID)
		if !named {
			continue
		}
		l, ok := resolveIdentity(&id, levels)
		if !ok {
			out = append(out, WriteTruthIssue{sc.ID, WriteTruthIdentityUnresolved, fmt.Sprintf("%s identity unresolved: level_id %q is not in the frozen map (no map row carries it) — copy the id of the level this scenario trades from the map exactly, or null when that row's id is NULL", sc.ID, id)})
			continue
		}
		anchor, hasAnchor := ScenarioAnchor(sc, d.Levels)
		r := ResolveScenarioIdentity(sc, levels, anchor, hasAnchor)
		if !r.Disagreed || identityAgreesZoneAware(l, anchor) || legIDs[id] {
			continue
		}
		fix := fmt.Sprintf("no map level sits at %.2f — name the level the trigger trades", anchor)
		if right, found := identityAtPrice(levels, anchor); found {
			fix = fmt.Sprintf("the level at %.2f is %s id %s — re-author level_id with it", anchor, identityLabel(right), *right.ID)
		}
		lo, hi := identityZone(l)
		out = append(out, WriteTruthIssue{sc.ID, WriteTruthIdentityPrice, fmt.Sprintf("%s identity≠price: level_id names %s %.2f but the scenario trades %.2f (trigger/confirm anchor; %.2f pts outside that level's zone %.2f–%.2f ±%.2f) — %s (named id %s)", sc.ID, identityLabel(l), l.Price, anchor, zoneGap(lo, hi, anchor), lo, hi, clusterToleranceFor(l.Price), fix, id)})
	}
	return out
}

func zoneGap(lo, hi, p float64) float64 {
	switch {
	case p < lo:
		return lo - p
	case p > hi:
		return p - hi
	}
	return 0
}

// PathLevelRoles is the A4 role vocabulary for economics.path_levels.
var PathLevelRoles = map[string]bool{"pass_through": true, "reduce": true, "exit": true}

// ArmQuantityFor is the contract count a scenario's authored arm would rest:
// a single arm is 1 contract (armed_executor.go single-leg placement), a split
// arm is the sum of its legs (size 1 when absent). No enabled arm → UNKNOWN.
func ArmQuantityFor(sc PlanScenario) (int, bool) {
	if sc.Arm == nil || !sc.Arm.Enabled {
		return 0, false
	}
	if len(sc.Arm.Legs) == 0 {
		return 1, true
	}
	n := 0
	for _, lg := range sc.Arm.Legs {
		if lg.Size > 0 {
			n += lg.Size
		} else {
			n++
		}
	}
	return n, true
}

func tickRound(p, tick float64) float64 {
	r := math.Round(p/tick) * tick
	return math.Round(r*1e6) / 1e6
}

type pathCandidate struct {
	price float64 // tick-rounded
	label string
	dist  float64 // from entry, in the trade direction
}

// onPath returns the candidates strictly between entry and target in the
// trade direction, nearest first. Excluded: the scenario's own level_id
// candidate when the entry sits in its zone (the traded level itself — a
// level_id naming a validated reclaim leg further along stays on the path),
// anything inside entry_zone ±1 tick, and anything within one tick of the
// entry or the target (the target is the destination, not an obstacle).
func onPath(cs []MapCandidate, sc PlanScenario, entry, target, dir, tick float64) []pathCandidate {
	ownID, hasOwn := authoredID(sc.LevelID)
	zLo, zHi, hasZone := 0.0, 0.0, false
	if e := sc.Economics; e != nil && len(e.EntryZone) == 2 && e.EntryZone[0] > 0 && e.EntryZone[1] >= e.EntryZone[0] {
		zLo, zHi, hasZone = e.EntryZone[0]-tick, e.EntryZone[1]+tick, true
	}
	var out []pathCandidate
	for _, c := range cs {
		if c.Projection || c.Price <= 0 {
			continue
		}
		if hasOwn && c.ID != nil && *c.ID == ownID {
			own := c.Identity
			own.Price = c.Price
			if identityAgreesZoneAware(own, entry) {
				continue // the traded level itself (entry inside its zone) is not an obstacle
			}
		}
		p := tickRound(c.Price, tick)
		if hasZone && p >= zLo-1e-9 && p <= zHi+1e-9 {
			continue
		}
		dist := dir * (p - entry)
		span := dir * (target - entry)
		if dist <= tick+1e-9 || dist >= span-tick-1e-9 {
			continue
		}
		label := c.Identity.Label
		if len(c.Names) > 0 {
			label = c.Names[0]
		}
		out = append(out, pathCandidate{p, label, dist})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].dist < out[j].dist })
	return out
}

func within(a, b, tick float64) bool { return math.Abs(a-b) <= tick+1e-9 }

// obstacleChainWriteIssues is A4. Per scenario with known geometry:
//   - authored target_chain / first_obstacle / path_levels prices are
//     normalized to the tick grid (recorded, never refused);
//   - target_chain is sorted by distance from entry in the trade direction;
//   - first_obstacle is the nearest SEATED level strictly between entry and
//     the final (arm) target within one tick — a capacity-cut candidate nearer
//     on the path is accepted, never required; an empty seated path requires
//     the obstacle to be the arm target itself;
//   - every seated level on that path is listed — as the first obstacle or in
//     economics.path_levels with a role pass_through|reduce|exit — and a
//     missing one is refused BY NAME;
//   - "reduce" with an arm of quantity 1 is refused as infeasible (no arm →
//     quantity UNKNOWN → not checked).
func obstacleChainWriteIssues(d *PlanDoc, seated, cut []MapCandidate, tick float64) (out []WriteTruthIssue, normalized []string) {
	for i := range d.Scenarios {
		sc := &d.Scenarios[i]
		e := sc.Economics
		if e == nil {
			continue
		}
		norm := func(field string, p *float64) {
			if p == nil || *p <= 0 {
				return
			}
			if r := tickRound(*p, tick); math.Abs(r-*p) > 1e-9 {
				normalized = append(normalized, fmt.Sprintf("%s %s %.2f→%.2f", sc.ID, field, *p, r))
				*p = r
			}
		}
		for j := range sc.TargetChain {
			norm(fmt.Sprintf("target_chain[%d]", j), &sc.TargetChain[j])
		}
		if e.FirstObstacle != nil {
			norm("first_obstacle", e.FirstObstacle.Price)
		}
		for j := range e.PathLevels {
			norm(fmt.Sprintf("path_levels[%d]", j), &e.PathLevels[j].Price)
		}
		v := EconomicsFor(*sc)
		g := v.Geometry
		dir := 0.0
		switch strings.ToLower(strings.TrimSpace(sc.Direction)) {
		case "long":
			dir = 1
		case "short":
			dir = -1
		}
		if g == nil || dir == 0 || !economicsPrice(g.Entry) || !economicsPrice(g.Target) || dir*(g.Target-g.Entry) <= 0 {
			continue // geometry the parser already refused or cannot read — not judged here
		}
		entry, target := g.Entry, g.Target
		for j := 1; j < len(sc.TargetChain); j++ {
			if dir*(sc.TargetChain[j]-sc.TargetChain[j-1]) < -1e-9 {
				side := "each next target higher"
				if dir < 0 {
					side = "each next target lower"
				}
				out = append(out, WriteTruthIssue{sc.ID, WriteTruthChainUnsorted, fmt.Sprintf("%s obstacle chain: target_chain %s is not sorted by distance from entry %.2f in the trade direction (%s: %s)", sc.ID, priceList(sc.TargetChain), entry, sc.Direction, side)})
				break
			}
		}
		path := onPath(seated, *sc, entry, target, dir, tick)
		cutPath := onPath(cut, *sc, entry, target, dir, tick)
		var fo *float64
		if e.FirstObstacle != nil && e.FirstObstacle.Price != nil && economicsPrice(*e.FirstObstacle.Price) {
			fo = e.FirstObstacle.Price
		}
		if fo != nil {
			ok := false
			limit := math.Inf(1)
			if len(path) > 0 {
				ok = within(*fo, path[0].price, tick)
				limit = path[0].dist
			} else {
				ok = within(*fo, target, tick)
			}
			for _, c := range cutPath {
				if !ok && c.dist < limit-1e-9 && within(*fo, c.price, tick) {
					ok = true // capacity-cut candidate nearer on the path: accepted, never required
				}
			}
			if !ok {
				if len(path) > 0 {
					out = append(out, WriteTruthIssue{sc.ID, WriteTruthObstacleNotNearest, fmt.Sprintf("%s obstacle chain: first_obstacle %.2f is not the nearest seated level on the path — %s %.2f (%.2f pts from entry %.2f) comes first", sc.ID, *fo, path[0].label, path[0].price, path[0].dist, entry)})
				} else {
					out = append(out, WriteTruthIssue{sc.ID, WriteTruthObstacleNotNearest, fmt.Sprintf("%s obstacle chain: no seated level lies between entry %.2f and target %.2f — first_obstacle must be the arm target %.2f (got %.2f)", sc.ID, entry, target, target, *fo)})
				}
			}
		}
		var missing []string
		for _, p := range path {
			covered := fo != nil && within(*fo, p.price, tick)
			for _, pl := range e.PathLevels {
				covered = covered || within(pl.Price, p.price, tick)
			}
			if !covered {
				missing = append(missing, fmt.Sprintf("%s %.2f (%.2f pts from entry)", p.label, p.price, p.dist))
			}
		}
		if len(missing) > 0 {
			out = append(out, WriteTruthIssue{sc.ID, WriteTruthPathLevelMissing, fmt.Sprintf("%s obstacle chain: omits %s — list every seated level between entry %.2f and target %.2f in economics.path_levels with a role (pass_through|reduce|exit)", sc.ID, strings.Join(missing, ", "), entry, target)})
		}
		var badRoles []string
		reduceAt := []string{}
		if e.FirstObstacle != nil && e.FirstObstacle.Response == "reduce" && fo != nil {
			reduceAt = append(reduceAt, fmt.Sprintf("first_obstacle %.2f", *fo))
		}
		for j, pl := range e.PathLevels {
			if !PathLevelRoles[pl.Role] {
				badRoles = append(badRoles, fmt.Sprintf("path_levels[%d] %.2f role %q", j, pl.Price, pl.Role))
			}
			if pl.Role == "reduce" {
				reduceAt = append(reduceAt, fmt.Sprintf("path_levels[%d] %.2f", j, pl.Price))
			}
		}
		if len(badRoles) > 0 {
			out = append(out, WriteTruthIssue{sc.ID, WriteTruthPathRoleInvalid, fmt.Sprintf("%s obstacle chain: %s — a path level's role is pass_through|reduce|exit", sc.ID, strings.Join(badRoles, ", "))})
		}
		if qty, known := ArmQuantityFor(*sc); known && qty == 1 && len(reduceAt) > 0 {
			out = append(out, WriteTruthIssue{sc.ID, WriteTruthReduceQty1, fmt.Sprintf("%s obstacle chain: reduce at %s is infeasible — the arm rests 1 contract and one contract cannot be halved; use pass_through or exit", sc.ID, strings.Join(reduceAt, ", "))})
		}
	}
	return out, normalized
}

func priceList(ps []float64) string {
	parts := make([]string, 0, len(ps))
	for _, p := range ps {
		parts = append(parts, fmt.Sprintf("%.2f", p))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// Repair law excerpts (routed by lawExcerptsFor on the error phrases above).
// They name no rule token, so the class-38 field-scoped hint guard stays green.
const RepairIdentityPriceLaw = "IDENTITY = PRICE: a scenario's level_id is the id of the map row at the price the scenario trades — the number its trigger, confirm ref_price and arm entry use. An id naming a different level (or a level not in the map) is REFUSED at write; copy the id of the level at the traded price exactly. A two-anchor sweep_reclaim names its sweep level in sweep_level_id and its reclaim level in reclaim_level_id — two different ids, each the level at its own leg's ref_price."

const RepairObstacleChainLaw = "OBSTACLE CHAIN: target_chain runs outward from entry in the trade direction (long ascending, short descending). first_obstacle is the NEAREST seated map level strictly between entry and the arm target; when none lies between them the first obstacle is the arm target itself. Every other seated level on that path goes in economics.path_levels as {price, level, level_id, role} with role pass_through|reduce|exit — a missing one is REFUSED by name. reduce needs more than one contract: with a single-contract arm use pass_through or exit. Prices are read on the tick grid."

// CapacityCutCandidates is the pool the seat race dropped: pool references
// with no seated candidate within the merge width. It is never rendered to
// the model, so A4 accepts one as a first obstacle and never requires it.
func CapacityCutCandidates(pool []ScoredLevel, seated []MapCandidate, price, atr5m float64) []MapCandidate {
	all := BuildMapCandidates(pool, price, atr5m, MapCandidateOpts{})
	if all == nil {
		return nil
	}
	width := clusterToleranceFor(price)
	out := make([]MapCandidate, 0, len(all))
	for _, c := range all {
		dup := false
		for _, s := range seated {
			if math.Abs(s.Price-c.Price) <= width {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, c)
		}
	}
	return out
}

// ScenarioWriteTruthSentences is the unconditional (correction, no knob)
// output-contract text for A3 + A4. It renders BEFORE the write-time
// feasibility sentence, whose parity test requires that sentence LAST.
func ScenarioWriteTruthSentences() string {
	return "IDENTITY = PRICE (refused at write): a scenario's level_id must be the map id of the level at the price the scenario trades — the level its trigger, confirm ref_price and arm entry use; an id naming a level at another price, or an id not in the map (invented or altered), is REFUSED and the plan is re-authored. " +
		"A two-anchor setup (a sweep of one level, a reclaim of another) adds sweep_level_id and reclaim_level_id: two DIFFERENT map ids, each the level at its own leg's ref_price (confirm for the sweep leg, confirm2 for the reclaim leg). " +
		"OBSTACLE CHAIN (refused at write): target_chain is sorted outward from entry in the trade direction (long ascending, short descending); first_obstacle is the NEAREST seated map level strictly between entry and the arm target (when none lies between them, first_obstacle is the arm target itself); every other seated map level on that path is listed in economics.path_levels:[{price:<n>,level:<label>,level_id:<map id>,role:pass_through|reduce|exit}] — a seated level missing from the path is REFUSED by name. Prices are read on the MNQ tick grid (an off-tick price is normalized to the grid and recorded). "
}

// nilMapNamedIDIssues refuses every non-null level_id / sweep_level_id /
// reclaim_level_id when the frozen map is nil.
func nilMapNamedIDIssues(d *PlanDoc) []WriteTruthIssue {
	var out []WriteTruthIssue
	for _, sc := range d.Scenarios {
		for _, f := range []struct {
			field string
			id    *string
		}{{"level_id", sc.LevelID}, {"sweep_level_id", sc.SweepLevelID}, {"reclaim_level_id", sc.ReclaimLevelID}} {
			if f.id == nil || strings.TrimSpace(*f.id) == "" {
				continue
			}
			out = append(out, WriteTruthIssue{sc.ID, WriteTruthIdentityUnresolved, fmt.Sprintf("%s identity unresolved: %s %q names a level but the frozen map is nil — the map is nil, so no id can resolve; write null", sc.ID, f.field, *f.id)})
		}
	}
	return out
}
