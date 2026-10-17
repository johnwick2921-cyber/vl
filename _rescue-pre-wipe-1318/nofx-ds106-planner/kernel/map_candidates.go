package kernel

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// W3 — CANDIDATES, NOT ENTITLEMENTS (2026-09-09).
//
// The whole map is kept. What changes is how many of its levels are offered as
// ENTRY candidates and in what order — plus projections that exist beyond the
// mapped range so a 400-point day is not a 60-point target.
//
// OWNER RULING 2026-09-09: this wave is PRESENTATION AND ORDERING ONLY.
//   · The merge below is a RENDER-TIME VIEW. It never writes back into
//     []ScoredLevel, so kernel/testdata/stage_a_score_legacy.json — which
//     json.Marshal's the full Seated+Pool slices across 64 fixtures and
//     byte-compares (kernel/stage_a_parity_test.go:21-46) — stays identical.
//   · The scored confluence term (levels_score.go:462-476, the
//     `(1 + 0.20*effConf)` factor at :500/:507) is NOT recomputed here.
//     That it credits correlated labels, and that the window it counts over
//     (confBand = 0.10×dATR, ≈±20 pt) is ~6.8× the width clusters collapse
//     within (3.00 pt), are RECORDED FINDINGS for experiment E4 —
//     docs/superpowers/reports/2026-09-09-candidates-data/E4-recorded-findings.md.
//     E4 is the controlled comparison the research demands; this wave may not
//     pre-empt it.
//
// Every rule this file adds is [I] — invented, unvalidated — until E4 measures
// it. The boot line and the Guide say so in those words.

// MapRole is the W3 role axis: what a reference is FOR on the map.
//
// It is deliberately a SEPARATE axis from LevelRole (levels_role.go:24-30,
// magnet_meanrevert / liquidity_break / react_zone / target_only / pivot).
// LevelRole describes the level's auction character and is serialized into the
// parity golden; MapRole describes its use in THIS read and is render-time only.
// Two axes, two names, never merged — a level can be a react_zone (character)
// and an obstacle (use in this read) at the same time.
type MapRole string

const (
	// MapRoleEntryCandidate — offered on the entry shortlist.
	MapRoleEntryCandidate MapRole = "entry-candidate"
	// MapRoleTarget — a plausible destination, never offered as an entry.
	MapRoleTarget MapRole = "target"
	// MapRoleObstacle — in the way of a move; not an entry, not a destination.
	MapRoleObstacle MapRole = "obstacle"
	// MapRoleInvalidation — beyond it the idea is wrong.
	MapRoleInvalidation MapRole = "invalidation"
)

// MapCandidate is ONE reference on the map after overlapping references merge.
// It is a view built at render time from []ScoredLevel and is never stored on
// a ScoredLevel.
type MapCandidate struct {
	ID       *string   `json:"id"`
	Identity PlanLevel `json:"identity"`
	Price    float64
	// Names carries EVERY merged reference's label, strongest first
	// ("Supply·1h", "PDC", "VWAP+1σ"). D2: three names for one price.
	Names []string
	Kinds []LevelKind
	// Grade / Score / Fresh come from the STRONGEST member. The score is
	// carried and shown on every row (D4) but ranks second to reachability.
	Grade string
	Score float64
	Fresh string
	// MergedCount is how many references folded into this candidate (1 = none).
	MergedCount int
	// MergedCredit is what this candidate contributes as confirmation: ALWAYS
	// 1, however many names it wears. This is the display counterpart of the
	// scored confluence term, which is untouched (see the E4 note above).
	MergedCredit int
	// Distance is signed points from price (candidate - price).
	Distance float64
	// DistanceATR is Distance in ATR5m units. HasATR distinguishes "zero ATR
	// distance" from "ATR was not available" (A24: never a plausible zero).
	DistanceATR float64
	HasATR      bool
	Role        MapRole
	// EntryCandidate is D3's verdict. RefusedReason is non-empty exactly when
	// candidacy was refused, and says why in words the card can print.
	EntryCandidate bool
	RefusedReason  string
	// Projection marks a reference PROJECTED beyond the mapped range (D5).
	// A projection is a target or an obstacle and is NEVER an entry candidate.
	Projection       bool
	ProjectionMethod string
}

// NamesLine renders every merged name as the card and the model both see it.
func (c MapCandidate) NamesLine() string {
	if len(c.Names) == 0 {
		return ""
	}
	return strings.Join(c.Names, " · ")
}

// DistanceATRLabel renders the ATR distance, or "n/a" when ATR was not
// available. A field the process cannot know prints n/a, never 0 (canon 49).
func (c MapCandidate) DistanceATRLabel() string {
	if !c.HasATR {
		return "n/a"
	}
	return trimFloat(math.Abs(c.DistanceATR))
}

// MapCandidateOpts carries the RESOLVED inputs. Nothing here is a literal at a
// call site: every value is read from a resolver by the caller and passed in.
type MapCandidateOpts struct {
	// MergeWidth is the zone width within which references are ONE reference.
	// Zero → the existing resolved cluster width (clusterToleranceFor).
	MergeWidth float64
	// MinTargetDistance is D3's resolved minimum: an entry candidate needs an
	// opposing reference at least this far away, so a first target inside the
	// risk is impossible by construction. Zero → the stop floor
	// (MinSLATRMult() × atr5m), which is exactly that guarantee.
	MinTargetDistance float64
}

// BuildMapCandidates is the W3 selection path: merge overlapping references,
// assign the map role, apply D3's candidacy test, and return the WHOLE map in
// reachability order.
//
// It never drops a reference. A candidate refused entry candidacy stays in the
// returned set carrying target / obstacle / invalidation — exclusion is not
// invalidation (class 93, AUDIT-CHECKLIST.md:2776).
//
// The input slice is never mutated (E7): members are copied by value.
func BuildMapCandidates(scored []ScoredLevel, price, atr5m float64, opts MapCandidateOpts) []MapCandidate {
	if len(scored) == 0 || price <= 0 {
		return nil
	}
	width := opts.MergeWidth
	if width <= 0 {
		width = clusterToleranceFor(price)
	}
	minTarget := opts.MinTargetDistance
	if minTarget <= 0 && atr5m > 0 {
		minTarget = MinSLATRMult() * atr5m
	}

	// Strongest first, so the merge keeper is the strongest member and its
	// grade/score/fresh represent the merged candidate. Stable + deterministic.
	order := make([]ScoredLevel, len(scored))
	copy(order, scored)
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].Score != order[j].Score {
			return order[i].Score > order[j].Score
		}
		return order[i].Price < order[j].Price
	})

	out := make([]MapCandidate, 0, len(order))
	for _, s := range order {
		merged := false
		for i := range out {
			if math.Abs(out[i].Price-s.Price) <= width {
				out[i].Names = appendDistinct(out[i].Names, s.Label)
				for _, n := range s.CollapsedNames {
					out[i].Names = appendDistinct(out[i].Names, n)
				}
				out[i].Kinds = append(out[i].Kinds, s.Kind)
				if member := CandidateIdentity(s.DetectedLevel); member.ID != nil {
					out[i].Identity.SourceIDs = appendDistinct(out[i].Identity.SourceIDs, *member.ID)
				}
				out[i].MergedCount++
				merged = true
				break
			}
		}
		if merged {
			continue
		}
		c := MapCandidate{
			Price:        s.Price,
			Names:        namesWithCollapsed(s),
			Kinds:        []LevelKind{s.Kind},
			Grade:        s.Grade,
			Score:        s.Score,
			Fresh:        s.Fresh,
			MergedCount:  1,
			MergedCredit: 1, // one price, one confirmation, however many names
			Distance:     s.Price - price,
		}
		c.Identity = CandidateIdentity(s.DetectedLevel)
		c.ID = c.Identity.ID
		if atr5m > 0 {
			c.DistanceATR = c.Distance / atr5m
			c.HasATR = true
		}
		out = append(out, c)
	}

	assignMapRoles(out, price, minTarget)

	// D4 — the entry shortlist orders by REACHABILITY: nearest first, in ATR5m
	// when it is available and in points when it is not. [I] until E4.
	// The score is carried on every row and shown, but ranks second.
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := math.Abs(out[i].Distance), math.Abs(out[j].Distance)
		if di != dj {
			return di < dj
		}
		return out[i].Score > out[j].Score
	})
	return out
}

// BuildMapWithProjections is BuildMapCandidates plus D5's projected references.
//
// Projections are references the detectors cannot produce because they lie
// BEYOND the mapped range — the 2026-09-03 case, where price ran +483 pts, left
// the highest seated level, and the map then held no target at all while the
// fade book kept selling.
//
// A projection is a TARGET or an OBSTACLE and is never an entry candidate. It
// carries the word `projection` and the method it came from, so neither the
// owner nor the model can mistake it for an observed level.
func BuildMapWithProjections(scored []ScoredLevel, projections []MapCandidate, price, atr5m float64, opts MapCandidateOpts) []MapCandidate {
	out := BuildMapCandidates(scored, price, atr5m, opts)

	width := opts.MergeWidth
	if width <= 0 {
		width = clusterToleranceFor(price)
	}
	minTarget := opts.MinTargetDistance
	if minTarget <= 0 && atr5m > 0 {
		minTarget = MinSLATRMult() * atr5m
	}

	for _, p := range projections {
		if p.Price <= 0 {
			continue
		}
		// A projection that lands on an OBSERVED reference is not new
		// information: fold its name in and keep the observed row, which is
		// evidence rather than arithmetic.
		folded := false
		for i := range out {
			if !out[i].Projection && math.Abs(out[i].Price-p.Price) <= width {
				for _, n := range p.Names {
					out[i].Names = appendDistinct(out[i].Names, n)
				}
				out[i].MergedCount++
				folded = true
				break
			}
		}
		if folded {
			continue
		}
		c := p
		c.Projection = true
		c.MergedCredit = 1
		if c.MergedCount <= 0 {
			c.MergedCount = 1
		}
		c.Distance = c.Price - price
		if atr5m > 0 {
			c.DistanceATR = c.Distance / atr5m
			c.HasATR = true
		}
		out = append(out, c)
	}

	assignMapRoles(out, price, minTarget)
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := math.Abs(out[i].Distance), math.Abs(out[j].Distance)
		if di != dj {
			return di < dj
		}
		return out[i].Score > out[j].Score
	})
	return out
}

// EntryShortlist returns only the entry candidates, in the reachability order
// BuildMapCandidates already established. The full map is what the caller keeps;
// this is the short, ranked set the research asks for.
func EntryShortlist(cs []MapCandidate) []MapCandidate {
	out := make([]MapCandidate, 0, len(cs))
	for _, c := range cs {
		if c.EntryCandidate {
			out = append(out, c)
		}
	}
	return out
}

// MapCounts are D7's per-read numbers. Every one is COUNTED from the map that
// was actually built — none is inferred and none is a literal.
type MapCounts struct {
	Detected        int
	Merged          int
	EntryCandidates int
	NoTargetRefused int
	Projections     int
}

// CountMap tallies a built map for the per-read boot/telemetry line.
func CountMap(detected int, cs []MapCandidate) MapCounts {
	m := MapCounts{Detected: detected, Merged: len(cs)}
	for _, c := range cs {
		switch {
		case c.Projection:
			m.Projections++
		case c.EntryCandidate:
			m.EntryCandidates++
		case c.RefusedReason != "":
			m.NoTargetRefused++
		}
	}
	return m
}

// RenderMapBlock is D6: the model sees the SAME map the card shows.
//
// One row per MERGED candidate, carrying every name it wears, its map role, its
// distance in ATR5m (or n/a), and — for a projection — the word `projection`
// and the method. Score is carried and shown but ranks second to reachability.
//
// No score COMPONENTS appear here (that is Stage A's job) and no instruction
// text (that is the Guide-strings lane's). This renders what the map IS, never
// what to do about it.
func RenderMapBlock(cs []MapCandidate, price float64) string {
	return renderMapBlock(cs, price, false)
}

// RenderIdentityMapBlock adds only the identity column to the planner table.
// The executor's existing map text is unchanged.
func RenderIdentityMapBlock(cs []MapCandidate, price float64) string {
	return renderMapBlock(cs, price, true)
}

// RenderScoredReferenceBlock preserves the legacy identity/score references
// without offering a second entry ordering beside the full zone shortlist.
func RenderScoredReferenceBlock(cs []MapCandidate, price float64) string {
	return renderMapBlock(cs, price, true, false)
}

func renderMapBlock(cs []MapCandidate, price float64, showID bool, shortlist ...bool) string {
	if len(cs) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "MAP (merged references, nearest-first; price %s):\n", trimFloat(price))
	for _, c := range cs {
		sign := "+"
		if c.Distance < 0 {
			sign = "-"
		}
		grade := c.Grade
		if grade == "" {
			grade = "—" // a projection carries no detector grade (canon 49)
		}
		names := c.NamesLine()
		if c.MergedCount > 1 {
			names = fmt.Sprintf("%s [merged x%d]", names, c.MergedCount)
		}
		tail := ""
		if c.Projection {
			tail = "  projection: " + c.ProjectionMethod
		} else if !c.EntryCandidate && c.RefusedReason != "" {
			tail = "  not-an-entry: " + c.RefusedReason
		}
		if showID {
			id := "NULL"
			if c.ID != nil {
				id = *c.ID
			}
			fmt.Fprintf(&b, "  id=%s", id)
		}
		fmt.Fprintf(&b, "  %-9s %-44s %s  %-16s %s%s pt / %s ATR%s\n",
			trimFloat(c.Price), names, grade, c.Role,
			sign, trimFloat(math.Abs(c.Distance)), c.DistanceATRLabel(), tail)
	}
	if len(shortlist) > 0 && !shortlist[0] {
		return b.String()
	}
	short := EntryShortlist(cs)
	if len(short) == 0 {
		b.WriteString("ENTRY SHORTLIST: none — no reference has a plausible target at the resolved minimum.\n")
	} else {
		b.WriteString("ENTRY SHORTLIST (reachability order — nearest first; [I] unvalidated, E4 pending):\n")
		for i, c := range short {
			fmt.Fprintf(&b, "  %d. %-9s %-44s score %s\n", i+1, trimFloat(c.Price), c.NamesLine(), trimFloat(c.Score))
		}
	}
	b.WriteString("Levels excluded from the shortlist remain on the map as target/obstacle/invalidation — exclusion is not invalidation.\n")
	return b.String()
}

// MapBootLine is D7's boot posture. Every field is READ, never a literal.
//
// TWO kinds of field, and the line is honest about both:
//
//   - PER-READ (detected / merged / entry-candidates / refused / projections)
//     cannot be known at boot — no planner read has happened. They print n/a.
//     Printing 0 would assert a measurement that was never taken, the exact
//     fault kernel/detector_d1prime.go:270-271 exists to prevent. The real
//     numbers ride MapReadLine, once per read.
//
//   - PER-TRADER (`cap`) is not a global. This process serves several traders
//     and each resolves its own max_levels, so the boot line LABELS it rather
//     than printing one trader's number as if it were everyone's — exactly the
//     shape VolumeWaveBootLine adopted when 0e016635 fixed it printing seats=8
//     from a package default while the bound strategy resolved 12.
func MapBootLine(defaultCap, hardCap int, dailySourceInstalled bool) string {
	seatable := "no"
	if dailySourceInstalled {
		seatable = "yes"
	}
	return fmt.Sprintf(
		"🗺 map: detected=n/a merged=n/a entry-candidates=n/a (no-target refused=n/a) · "+
			"order=reachability[I] · projections=n/a (pwh/pwl seatable=%s) · "+
			"cap=per-trader (default %d, hard cap %d) · merge=zone-width[I] · min-target=stop-floor[I]",
		seatable, defaultCap, hardCap)
}

// MapReadLine is the per-read counterpart: the same fields with the numbers
// COUNTED from the map that was actually built (canon 35 — counters record,
// never infer).
func MapReadLine(m MapCounts) string {
	return fmt.Sprintf(
		"🗺 map read: detected=%d merged=%d entry-candidates=%d (no-target refused=%d) · "+
			"order=reachability[I] · projections=%d",
		m.Detected, m.Merged, m.EntryCandidates, m.NoTargetRefused, m.Projections)
}

// assignMapRoles applies D1's role axis and D3's candidacy test in one pass
// over the merged set. Mutates the slice in place; the slice is ours.
func assignMapRoles(cs []MapCandidate, price, minTarget float64) {
	// Price-ordered index so "the next merged candidate in the trade
	// direction" is a neighbour lookup rather than an O(n²) scan.
	idx := make([]int, len(cs))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return cs[idx[a]].Price < cs[idx[b]].Price })

	for pos, ci := range idx {
		c := &cs[ci]
		if c.Projection {
			// D5 — a projection is a target or an obstacle, NEVER an entry.
			if c.Role == "" {
				c.Role = MapRoleTarget
			}
			c.EntryCandidate = false
			c.RefusedReason = "projection — targets and obstacles only"
			continue
		}
		// The trade direction from a level is AWAY from price: a reference
		// above price is faded short (target below it), one below is faded
		// long (target above it). The opposing reference is the next merged
		// candidate in that direction.
		var opposing *MapCandidate
		if c.Price >= price {
			if pos-1 >= 0 {
				opposing = &cs[idx[pos-1]]
			}
		} else {
			if pos+1 < len(idx) {
				opposing = &cs[idx[pos+1]]
			}
		}
		if minTarget <= 0 {
			// A24 — the minimum could not be resolved, so candidacy is not
			// decided. Say so rather than guessing either way.
			c.Role = MapRoleObstacle
			c.EntryCandidate = false
			c.RefusedReason = "no resolved minimum target distance — candidacy undecided"
			continue
		}
		if opposing == nil {
			c.Role = mapRoleForNonEntry(c, price)
			c.EntryCandidate = false
			c.RefusedReason = "no opposing reference beyond it"
			continue
		}
		gap := math.Abs(opposing.Price - c.Price)
		if gap < minTarget {
			c.Role = mapRoleForNonEntry(c, price)
			c.EntryCandidate = false
			c.RefusedReason = "nearest opposing reference " + trimFloat(gap) +
				" pt away, under the " + trimFloat(minTarget) + " pt minimum"
			continue
		}
		c.Role = MapRoleEntryCandidate
		c.EntryCandidate = true
	}
}

// mapRoleForNonEntry names what a reference is when it is not an entry: the
// far side of price is where a move is going (target); the near side is what
// is in the way (obstacle).
func mapRoleForNonEntry(c *MapCandidate, price float64) MapRole {
	if math.Abs(c.Price-price) <= 0 {
		return MapRoleObstacle
	}
	return MapRoleTarget
}

// appendDistinct appends a label once, preserving first-seen order.
// namesWithCollapsed seeds a candidate's name list from the level's own label
// plus every label collapseLevelClusters folded into it from another timeframe
// (fix/collapse-keeps-names). One credit, one seat, several names.
func namesWithCollapsed(s ScoredLevel) []string {
	names := appendDistinct(nil, s.Label)
	for _, n := range s.CollapsedNames {
		names = appendDistinct(names, n)
	}
	return names
}

func appendDistinct(dst []string, label string) []string {
	l := strings.TrimSpace(label)
	if l == "" {
		return dst
	}
	for _, e := range dst {
		if e == l {
			return dst
		}
	}
	return append(dst, l)
}

// trimFloat renders a float with at most 2 decimals and no trailing zeros, so
// a refusal reason reads "29.64 pt" rather than "29.640000 pt".
func trimFloat(f float64) string {
	s := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", f), "0"), ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

// MatchMapCandidate finds the merged candidate a price belongs to, using the
// SAME zone width the merge used. Returns ok=false when nothing matches, so a
// caller emits no role and no distance rather than a fabricated one (A24).
func MatchMapCandidate(cs []MapCandidate, price float64) (MapCandidate, bool) {
	if price <= 0 {
		return MapCandidate{}, false
	}
	width := clusterToleranceFor(price)
	best, bestD, found := MapCandidate{}, math.MaxFloat64, false
	for _, c := range cs {
		d := math.Abs(c.Price - price)
		if d <= width && d < bestD {
			best, bestD, found = c, d, true
		}
	}
	return best, found
}
