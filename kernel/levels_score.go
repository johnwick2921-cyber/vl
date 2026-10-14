package kernel

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"vl/logger"
)

// P1.5 — CONFLUENCE SCORER → graded TOP-8.
//
// Deterministic (the LLM never sorts). Each in-band level is scored by
// type-evidence × freshness × confluence × HTF-origin → an A/B/C grade; the
// day-trade lock filters to ±1.5×dATR and seats a capped set with today's
// structural levels getting first seats. S/D + FVG/OB are confluence-only (grade
// capped at C, excluded when they stand alone).

// ScoredLevel is a graded, seated level for the KEY LEVELS block.
type ScoredLevel struct {
	DetectedLevel
	Grade      string  `json:"grade"`      // A | B | C
	Fresh      string  `json:"fresh"`      // freshness label: fresh | tested | B | C
	Score      float64 `json:"score"`      // deterministic composite
	Confluence int     `json:"confluence"` // # other in-band FAMILIES clustered nearby
	Distance   float64 `json:"distance"`   // signed points from price (level - price)
	// Role is the machine-assigned level_role (addendum 1, 2026-08-26) —
	// rendered as a column in both prompts and the executor KEY LEVELS block.
	Role LevelRole `json:"role,omitempty"`
}

// freshLabel normalizes a level-state freshness grade to a display label.
func freshLabel(f string) string {
	switch strings.ToLower(strings.TrimSpace(f)) {
	case "", "a", "fresh":
		return "fresh"
	case "b":
		return "B"
	case "c", "tested":
		return "tested"
	case "done", "consumed":
		// P1c design rule — a consumed level ROLE-FLIPS (support↔resistance) and
		// STAYS on the map; it is never deleted, and the label says what it is.
		return "flipped"
	default:
		return f
	}
}

// DefaultMaxLevels is the KEY LEVELS table cap (spec max_levels default 8).
const DefaultMaxLevels = 8

var levelGradeRank = map[string]int{"A": 3, "B": 2, "C": 1}

// GradeRank orders a level grade A > B > C (unknown/empty → 0). Exported for
// the write-site machine-grade stamp's collision rule.
func GradeRank(g string) int {
	if r, ok := levelGradeRank[strings.ToUpper(strings.TrimSpace(g))]; ok {
		return r
	}
	return 0
}

// FilterLevelsByMinGrade drops levels graded below minGrade (A > B > C). An empty
// or unknown minGrade is a no-op (no filter). Owner levels grade "A", so they
// survive any minGrade (they are always seated by design).
func FilterLevelsByMinGrade(scored []ScoredLevel, minGrade string) []ScoredLevel {
	min, ok := levelGradeRank[strings.ToUpper(strings.TrimSpace(minGrade))]
	if !ok {
		return scored
	}
	out := make([]ScoredLevel, 0, len(scored))
	for _, l := range scored {
		// B2 (2026-08-26) — min_grade Tier-1 exception: Tier-1 structural rows
		// survive ANY min_grade cut (the map must always carry today's anchors).
		if levelGradeRank[strings.ToUpper(l.Grade)] >= min || isTier1Kind(l.Kind) {
			out = append(out, l)
		} else {
			researchCut(l.DetectedLevel, fmt.Sprintf("minimum grade: %s below %s", l.Grade, minGrade))
		}
	}
	return out
}

// typeEvidence weights a level kind's standalone evidence.
func typeEvidence(k LevelKind) float64 {
	switch k {
	case KindPDH, KindPDL, KindPDC, KindRTHH, KindRTHL, KindPWH, KindPWL, KindPMH, KindPML:
		return 1.0 // strong structural / HTF references
	case KindONH, KindONL, KindNPOC:
		return 0.85
	// Pack B (owner override 2026-08-26) — VOLUME FAMILY weights, provisional:
	// the B4 level_stats forward-validation table is the 2-week verdict that
	// re-weights these (spec: no volume wave was ever live-validated before).
	case KindVWAP, KindPOC:
		return 0.90
	// Level-truth wave (2026-08-27) — ±2σ bands EMITTED at 0.85 (owner ruling).
	case KindVWAP2S:
		return 0.85
	// Level-truth wave (2026-08-27) — swing points are anchor-class evidence:
	// the structure engine already filtered them (k fractal + min-move ATR),
	// so they carry structural weight and decay on the ANCHOR ladder.
	case KindSWGH, KindSWGL:
		return 0.85
	case KindEVWAP, KindPDVWAP:
		return 0.85
	case KindVAH, KindVAL, KindSETT:
		return 0.80
	case KindMIDO:
		return 0.60
	case KindASH, KindASL, KindLDNH, KindLDNL, KindORH, KindORL, KindIBH, KindIBL, KindEQH, KindEQL:
		return 0.70
	case KindRound, KindGap:
		return 0.55
	case KindSupply, KindDemand, KindFVG, KindIFVG, KindOB:
		return 0.30 // confluence-only, never standalone
	default:
		return 0.50
	}
}

func isZoneKind(k LevelKind) bool {
	switch k {
	case KindSupply, KindDemand, KindFVG, KindIFVG, KindOB:
		return true
	}
	return false
}

// ── v3 zone grading (owner-approved 2026-08-24, research-grounded) ──────────
// Evidence = kindBase(TF) × reversalBonus × TFmult × freshness × (1+0.2·conf).
// Ground: freshness · departure speed · HTF alignment (SMC quality filters);
// RBD/DBR (reversal) > RBR/DBD (continuation). 1m zones stay C (noise); 15m
// floor B, cap B; 1h floor B, cap A (1h wave, 2026-08-25); 4h may reach A.
//
// R3 note (grading audit §4.1, 2026-08-25): zoneEvidenceByKind ALREADY tiers by
// TF and zoneTFMult multiplies AGAIN, so the EFFECTIVE 4h:1m spread is ≈2.3×
// (0.72/0.40 × 1.3/1.0 = 1.80 × 1.30), not the 1.3× a raw TFmult reading
// suggests. DOCUMENTED, NOT CHANGED — the 1h-wave calibration numbers are
// computed against this formula and 4h seniority rides TFMult 1.3 vs 1.2.
// Revisit after the 1h wave has live data (owner queue).

// zoneEvidenceByKind maps kind → TF tier → base evidence. Tiers: 1m / 15m / 1h / 4h.
// 1h values raised by the 1h wave (2026-08-25, owner R1): the 1h is the
// setup/context rung of the intraday ladder (4H trend → 1H setup → 15M entry);
// its zones may now reach A with confluence like 4h.
var zoneEvidenceByKind = map[LevelKind]map[string]float64{
	KindOB:     {"1m": 0.40, "15m": 0.50, "1h": 0.70, "4h": 0.72},
	KindFVG:    {"1m": 0.35, "15m": 0.45, "1h": 0.65, "4h": 0.65},
	KindIFVG:   {"1m": 0.35, "15m": 0.45, "1h": 0.65, "4h": 0.65},
	KindSupply: {"1m": 0.35, "15m": 0.45, "1h": 0.65, "4h": 0.65},
	KindDemand: {"1m": 0.35, "15m": 0.45, "1h": 0.65, "4h": 0.65},
}

// zoneTFMult is the "HTF alignment" multiplier per detection timeframe tier.
var zoneTFMult = map[string]float64{"1m": 1.0, "15m": 1.1, "1h": 1.2, "4h": 1.3}

// HTFScoreMultiplier is the higher-timeframe weight applied to a level whose
// DetectedLevel.HTF is set. NAMED: the boot line and the Guide must print the
// number the scorer actually uses rather than one typed beside it (A11).
// W-KNOB-PRUNE (owner ruling 2026-09-18): 1.2 → 1.0 and the
// day_plan.htf_score_multiplier knob is gone — Round 23 Q-C measured that the
// 1.2 premium promoted a group that holds LESS. The seat diff on the identity
// fixture is in docs/superpowers/reports/2026-09-18-knob-prune.md; the
// goldens (identity_legacy_output.json, knob_prune/seating.json) are pinned
// at 1.0.
const HTFScoreMultiplier = 1.0

// zoneReversalBonus rewards RBD/DBR (reversal) over RBR/DBD (continuation).
const zoneReversalBonus = 1.1

// zoneTierFor maps a detection timeframe string to one of the four v3 tiers
// ("" and short TFs → 1m; 30m → 15m; 2h → 1h; 6h/8h/12h → 4h). ANY unknown TF
// falls back to "1m" — a raw pass-through used to miss the zoneTFMult table
// and ZERO the entire zone score (the comment said "noise floor" but the code
// returned the unknown TF as-is).
func zoneTierFor(tf string) string {
	switch strings.ToLower(strings.TrimSpace(tf)) {
	case "", "1m", "3m", "5m":
		return "1m"
	case "30m":
		return "15m"
	case "2h":
		return "1h"
	case "6h", "8h", "12h":
		return "4h"
	case "1d", "3d", "1w":
		// W-TF (owner ruling 2026-09-10). These timeframes could not reach a
		// level before this wave, so no value moves: this classifies an input
		// that was previously impossible. Without it the default below would
		// route a DAILY level to the 1m noise floor — multiplier 1.0 against
		// 4h's 1.3, KindOB evidence 0.40 against 0.72, and the zone grader's
		// 1m clause forcing grade C — making a daily zone the weakest thing on
		// the map. Inheriting the 4h tier is [I], not established: round 12
		// (12c) says no timeframe hierarchy is proven and E4 measures whether
		// this classification is right. No weight, multiplier, cap or tolerance
		// changes here.
		return "4h"
	default:
		// Keep the known tier names as-is; anything else → 1m (noise floor),
		// never a missing-map zero.
		if _, ok := zoneTFMult[strings.ToLower(strings.TrimSpace(tf))]; ok {
			return strings.ToLower(strings.TrimSpace(tf))
		}
		return "1m"
	}
}

// ConfluenceCap (C14, 2026-08-25) — the confluence count is capped before it
// multiplies the score: research shows diminishing returns beyond ~3 confirming
// levels, and an uncapped count let a crowded price cluster inflate its own
// grade. Env CONFLUENCE_CAP overrides; the value is a USER knob, never a
// hardcoded rule.
func ConfluenceCap() int {
	if v := os.Getenv("CONFLUENCE_CAP"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
			return n
		}
	}
	return 3
}

// zoneSizeMult (C13 / A5, 2026-08-25) — the zone-size axis: a tight small base
// is stronger than an oversized one (S/D literature: a wide zone is a poorly
// defined decision point). Banded 0.5..1.25 in daily-ATR units; a missing
// [Lo,Hi] (line levels, not zones) is neutral 1.0.
func zoneSizeMult(lo, hi, atr float64) float64 {
	if lo <= 0 || hi < lo || atr <= 0 {
		return 1.0
	}
	size := (hi - lo) / atr
	switch {
	case size <= 0.30:
		return 1.25
	case size <= 0.60:
		return 1.10
	case size <= 1.00:
		return 1.0
	case size <= 1.50:
		return 0.85
	case size <= 2.50:
		return 0.70
	default:
		return 0.50
	}
}

// zoneEvidence computes the v3 base evidence for a zone level (kind × TF tier ×
// reversal pattern). Unknown TF → 1m tier (noise floor).
func zoneEvidence(l DetectedLevel) float64 {
	tier := zoneTierFor(l.TF)
	table, ok := zoneEvidenceByKind[l.Kind]
	if !ok {
		return 0.30
	}
	base, ok := table[tier]
	if !ok {
		base = table["1m"]
	}
	if l.Research != nil {
		l.Research.ZoneBase = scoreValue(base)
		l.Research.ReversalMultiplier = scoreValue(1.0)
	}
	if l.ZonePattern == "reversal" {
		if l.Research != nil {
			l.Research.ReversalMultiplier = scoreValue(zoneReversalBonus)
		}
		base *= zoneReversalBonus
	}
	return base
}

// isTodayPriority marks the kinds that get first seats at the cap.
func isTodayPriority(k LevelKind) bool {
	switch k {
	case KindPDH, KindPDL, KindPDC, KindRTHH, KindRTHL, KindORH, KindORL, KindONH, KindONL:
		return true
	}
	return false
}

// ── Pack B tiering (B2, owner override 2026-08-26) ─────────────────────────

// Tier1ProximityTicks is the B2 spec constant: a pattern (zone) may grade
// above C ONLY within 12 ticks of a Tier-1 anchor. 12 MNQ ticks = 3.00 points.
const Tier1ProximityTicks = 12

// isTier1Kind marks the Tier-1 structural family (today's anchors + week/month
// extremes + R-A13 volume anchors). Tier-1 rows are exempt from the min_grade
// cut (spec: "min_grade Tier-1 exception") and anchor the pattern-above-C gate.
// R-A13 (owner ruling, S-wave 2026-08-26): VAH/VAL/SETT/nPOC JOIN the set —
// volume levels are the institutional references the B2 gate must honor.
func isTier1Kind(k LevelKind) bool {
	switch k {
	case KindPDH, KindPDL, KindPDC, KindRTHH, KindRTHL, KindORH, KindORL, KindONH, KindONL, KindPWH, KindPWL, KindPMH, KindPML,
		KindVAH, KindVAL, KindSETT, KindNPOC:
		return true
	}
	return false
}

// IsTier1Kind is the exported form for the write-site validator + level_stats.
func IsTier1Kind(k LevelKind) bool { return isTier1Kind(k) }

// IsTier1Label matches a plan-doc label (PDH, ONH, nPOC·Tue…) to the Tier-1
// family for the min-grade exception on []PlanLevel surfaces (labels are all
// those surfaces carry).
func IsTier1Label(label string) bool {
	l := strings.ToUpper(strings.TrimSpace(label))
	switch {
	case l == "PDH", l == "PDL", l == "PDC", strings.HasPrefix(l, "RTH-"):
		return true
	case l == "ONH", l == "ONL", l == "OR-H", l == "OR-L":
		return true
	case l == "PWH", l == "PWL", l == "PMH", l == "PML":
		return true
	// R-A13 (S-wave 2026-08-26) — volume anchors join Tier-1 (both emission
	// paths share the nPOC prefix).
	case l == "VAH", l == "VAL", l == "SETT", strings.HasPrefix(l, "NPOC"):
		return true
	}
	return false
}

// withinTier1Proximity reports whether a pattern level sits within
// Tier1ProximityTicks of a Tier-1 anchor (band-aware: zones measure from their
// NEAREST band edge, so a wide zone doesn't piggyback on distance).
func withinTier1Proximity(l DetectedLevel, pool []DetectedLevel, price float64) bool {
	// MNQ tick = 0.25 (the same constant clusterToleranceFor uses) → 12 ticks =
	// 3.00 points. `price` is the anchor for the fallback only.
	_ = price
	tol := Tier1ProximityTicks * 0.25
	dist := func(p float64) float64 {
		switch {
		case l.Lo < l.Hi && p < l.Lo:
			return l.Lo - p
		case l.Lo < l.Hi && p > l.Hi:
			return p - l.Hi
		default:
			return 0
		}
	}
	for _, o := range pool {
		if !isTier1Kind(o.Kind) {
			continue
		}
		if math.Abs(o.Price-l.Price) <= tol || dist(o.Price) <= tol {
			return true
		}
	}
	return false
}

// levelFamily (B3) buckets kinds into confluence FAMILIES — distinct-family
// confluence counting never double-counts a family (VWAP+1σ+VWAP−1σ are ONE
// family, not three confirming levels).
func levelFamily(k LevelKind) string {
	switch k {
	case KindVWAP, KindEVWAP, KindPDVWAP, KindVWAP2S:
		return "vwap"
	case KindPOC, KindNPOC, KindVAH, KindVAL:
		return "profile"
	case KindPDH, KindPDL, KindPDC, KindRTHH, KindRTHL, KindSETT:
		return "prior"
	case KindPWH, KindPWL, KindPMH, KindPML:
		return "anchor"
	case KindONH, KindONL, KindASH, KindASL, KindLDNH, KindLDNL, KindORH, KindORL, KindIBH, KindIBL, KindMIDO:
		return "overnight"
	case KindEQH, KindEQL:
		return "liquidity"
	case KindSupply, KindDemand, KindFVG, KindIFVG, KindOB:
		return "zone"
	case KindRound:
		return "round"
	case KindGap:
		return "gap"
	default:
		return "other"
	}
}

// FamilyFor exposes the family bucket for level_stats + reports.
func FamilyFor(k LevelKind) string { return levelFamily(k) }

// freshMult maps a freshness grade to a score multiplier. "" → fresh.
// P1c design rule: a consumed level is NOT zeroed (that deleted the map's best
// levels) — it role-flips and stays seated at a reduced score.
func freshMult(f string) float64 {
	switch strings.ToLower(strings.TrimSpace(f)) {
	case "", "a", "fresh":
		return 1.0
	case "b":
		return 0.8
	case "c", "tested":
		return 0.6
	case "done", "consumed":
		return 0.5 // role-flipped, still on the map
	default:
		return 1.0
	}
}

// zoneFreshMult is the Pack B (2026-08-26) ZONE freshness ladder — 1.0 / 0.6 /
// 0.3 / 0.15. Zones (S/D/FVG/IFVG/OB) decay hard: a twice-tested base is
// mostly printed. ANCHORS (line levels) keep the original ladder — "anchors
// no-decay" per spec, i.e. the volume ladder applies to ZONES ONLY.
func zoneFreshMult(f string) float64 {
	switch strings.ToLower(strings.TrimSpace(f)) {
	case "", "a", "fresh":
		return 1.0
	case "b":
		return 0.6
	case "c", "tested":
		return 0.3
	case "done", "consumed":
		return 0.15
	default:
		return 1.0
	}
}

// ScoreLevels applies the day-trade lock + confluence grading and returns the
// seated TOP-N graded levels, nearest-first. `freshness` returns a level's grade
// from the level-state table (nil → everything fresh). maxLevels ≤ 0 → default 8.
// proximityK is the resolved day-trade lock half-width in daily-ATR multiples
// (the owner's proximity_filter_atr; ≤0 → the spec constant 1.5) — the band
// OUTSIDE which no level is generated or seated. The freshness callback is
// built by levelFreshnessFn WITH the read's now already captured (class 60).
func ScoreLevels(levels []DetectedLevel, price, dATR float64, freshness func(DetectedLevel) string, maxLevels int, proximityK float64) []ScoredLevel {
	return scoreLevelsPool(levels, price, dATR, freshness, maxLevels, proximityK, nil, HTFScoreMultiplier)
}

// LegacyHtfSeats (S3, 2026-09-16) is the seatHTF count the pre-S3 code
// hardcoded. The legacy path (seats nil) seats exactly this many; the value is
// kept as a named constant for the boot line and tests.
const LegacyHtfSeats = 2

// scoreLevelsPool is the full scorer (lock → grade → collapse → seat → top-N).
func scoreLevelsPool(levels []DetectedLevel, price, dATR float64, freshness func(DetectedLevel) string, maxLevels int, proximityK float64, htfSeats *int, htfMult float64) []ScoredLevel {
	if price <= 0 || dATR <= 0 {
		return nil
	}
	if maxLevels <= 0 {
		maxLevels = DefaultMaxLevels
	}
	if proximityK <= 0 {
		proximityK = ActivationWindowK
	}
	band := proximityK * dATR
	// S1 (mega-research 2026-08-26) — confBand = 0.10 × daily-range proxy:
	// ±35pt at dATR≈350 — wide enough that distant rows count as "confluence".
	// Documented, not changed here (the proximity re-tune is config-side).
	confBand := 0.10 * dATR // cluster tolerance

	// Proximity filter (day-trade lock).
	levels = researchLevels(levels)
	inBand := make([]DetectedLevel, 0, len(levels))
	for _, l := range levels {
		if math.Abs(l.Price-price) <= band {
			inBand = append(inBand, l)
		} else {
			researchCut(l, fmt.Sprintf("proximity distance=%g exceeds band=%g", math.Abs(l.Price-price), band))
		}
	}

	scored := make([]ScoredLevel, 0, len(inBand))
	for _, l := range inBand {
		fRaw := ""
		if freshness != nil {
			fRaw = freshness(l)
		}
		fm := freshMult(normalizeByTFGrade(fRaw))
		if isZoneKind(l.Kind) {
			// Pack B (2026-08-26) — the zone freshness ladder is steeper than
			// the anchor ladder (1.0/0.6/0.3/0.15 vs 1.0/0.8/0.6/0.5).
			fm = zoneFreshMult(normalizeByTFGrade(fRaw))
		}
		l.Research.Freshness = fRaw
		l.Research.FreshMultiplier = scoreValue(fm)
		if fm == 0 {
			researchCut(l, "consumed freshness multiplier=0")
			continue // consumed
		}
		// B3 (2026-08-26) — confluence counts DISTINCT FAMILIES within the
		// cluster band, never raw levels: three VWAP-band rows are one family
		// and must not triple-inflate the grade. Capped (C14, env
		// CONFLUENCE_CAP, default 3) before it multiplies the score.
		conf := 0
		seenFamilies := map[string]bool{}
		for _, o := range inBand {
			if o.Price == l.Price && o.Kind == l.Kind && o.Label == l.Label {
				continue
			}
			if math.Abs(o.Price-l.Price) <= confBand {
				fam := levelFamily(o.Kind)
				if fam != levelFamily(l.Kind) && !seenFamilies[fam] {
					seenFamilies[fam] = true
					conf++
				}
			}
		}
		// C14 (2026-08-25) — diminishing returns: the confluence count feeding
		// the score is capped (env CONFLUENCE_CAP, default 3).
		effConf := float64(conf)
		if capC := ConfluenceCap(); conf > capC {
			effConf = float64(capC)
		}
		l.Research.ConfluenceRaw = scoreValue(conf)
		l.Research.ConfluenceCapped = scoreValue(effConf)
		if isZoneKind(l.Kind) && conf == 0 {
			// P0.1 (2026-08-19) — a zone with an HTF origin seats on its own
			// merit (grade C): large-account auctions don't need a crowd. Pure
			// intraday S/D + FVG/OB remain confluence-only, never standalone.
			if !(l.HTF) {
				researchCut(l, "intraday zone without other-family confluence")
				continue
			}
		}
		htf := 1.0
		if l.HTF {
			htf = htfMult
		}
		var score float64
		if isZoneKind(l.Kind) {
			// v3 zone grading: kindBase × size × TFmult × freshness × confluence.
			evidence, size, confMult, tfMult := zoneEvidence(l), zoneSizeMult(l.Lo, l.Hi, dATR), (1 + 0.20*effConf), zoneTFMult[zoneTierFor(l.TF)]
			score = evidence * size * fm * confMult * tfMult
			l.Research.Evidence = scoreValue(evidence)
			l.Research.SizeMultiplier = scoreValue(size)
			l.Research.ConfluenceMultiplier = scoreValue(confMult)
			l.Research.TFMultiplier = scoreValue(tfMult)
		} else {
			evidence, confMult := typeEvidence(l.Kind), (1 + 0.20*effConf)
			score = evidence * fm * confMult * htf
			l.Research.Evidence = scoreValue(evidence)
			l.Research.ConfluenceMultiplier = scoreValue(confMult)
			l.Research.HTFMultiplier = scoreValue(htf)
		}
		l.Research.Score = scoreValue(score)
		grade := gradeFromScore(score)
		rawGrade := grade
		if isZoneKind(l.Kind) {
			// v3 floors/caps: 1m zones never above C; 15m floor B, cap B
			// (entry TF, not a zone-defining TF — research R20); 1h floor B,
			// cap A (1h wave 2026-08-25 — setup/context rung may reach A with
			// confluence); 4h floor B and may reach A (the professionals' level).
			switch zoneTierFor(l.TF) {
			case "15m":
				if grade != "B" {
					grade = "B"
				}
			case "1h":
				if grade == "C" {
					grade = "B"
				}
			case "4h":
				if grade == "C" {
					grade = "B"
				}
			default: // 1m tier
				if grade != "C" {
					grade = "C"
				}
			}
			researchGrade(l, rawGrade, grade, "zone timeframe floor/cap")
			rawGrade = grade
			// B2 (2026-08-26) — pattern-above-C only within
			// TIER1_PROXIMITY_TICKS of a Tier-1 anchor: a pattern (zone) earns
			// grade B/A ONLY when it sits beside a Tier-1 structural level.
			// Otherwise the pattern is a context mark, grade C.
			if grade != "C" && !withinTier1Proximity(l, inBand, price) {
				grade = "C"
			}
		}
		researchGrade(l, rawGrade, grade, "Tier-1 proximity cap")
		l.Research.Grade = scoreValue(grade)
		role := RoleFor(l, fRaw)
		l.Research.Role = scoreValue(role)
		scored = append(scored, ScoredLevel{
			DetectedLevel: l,
			Grade:         grade,
			Fresh:         freshLabel(fRaw),
			Score:         score,
			Confluence:    conf,
			Distance:      l.Price - price,
			Role:          role,
		})
	}

	// P0.4 (2026-08-19) — CLUSTER COLLAPSE: levels within LevelClusterTolerance
	// merge into ONE entry keeping the strongest provenance (highest score,
	// then today-priority kind, then nearer distance). Before this, an equal-
	// high/low family 3 points wide shipped as 4 separate "A" rows and the
	// planner copied every duplicate into the plan (grade inflation + wasted
	// seats).
	scored = collapseLevelClusters(scored, clusterToleranceFor(price))

	// Seat: today's priority kinds first, then the rest, both by score desc
	// (deterministic tie-break: nearer distance, then lower price).
	sort.SliceStable(scored, func(i, j int) bool {
		pi, pj := isTodayPriority(scored[i].Kind), isTodayPriority(scored[j].Kind)
		if pi != pj {
			return pi
		}
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		di, dj := math.Abs(scored[i].Distance), math.Abs(scored[j].Distance)
		if di != dj {
			return di < dj
		}
		return scored[i].Price < scored[j].Price
	})

	// P0.1 (2026-08-19) — BOTH-SIDE SEATING: the plan must always carry levels
	// on BOTH sides of price. On a gap-down day every today-priority kind sits
	// above price and used to fill all 8 seats (the 2026-08-18 one-sided map
	// that left the model with "no trigger" for 110 points of breakdown). When
	// the pure top-N leaves one side under-supplied while candidates exist,
	// swap in the best in-band levels from the missing side.
	// G2/G3/G6 (2026-08-24) — seatHTF first: the model only ever sees the
	// top-N table, so HTF swing/zone levels must WIN seats to reach the plan.
	// The P0.1 side-balance pass may still swap a promoted seat if a side ends
	// under-supplied (the hard rule wins).
	scored = researchSeat("HTF seating", scored, maxLevels, func(s []ScoredLevel, n int) []ScoredLevel { return seatHTF(s, n, htfSeats) })
	scored = researchSeat("volume-family seating", scored, maxLevels, SeatVolumeFamily) // Pack B (2026-08-26) — E1 volume-family seat
	scored = researchSeat("both-side seating", scored, maxLevels, seatBothSides)

	if len(scored) > maxLevels {
		researchCap(scored, maxLevels, "pre-pool seating cap")
		scored = scored[:maxLevels]
	}

	// S1/A13 (mega-research 2026-08-26) — the seated table was never logged, so
	// per-hour in-band counts were unmeasurable for Sep-3. One line per planner
	// read makes the pre/post proximity re-tune pool size observable.
	logger.Infof("🗺️ seated %d/%d in-band levels (proximity band ±%.0fpt, %d of them retained)", len(scored), len(inBand), band, len(scored))

	// Output nearest-first for the executor table.
	sort.SliceStable(scored, func(i, j int) bool {
		return math.Abs(scored[i].Distance) < math.Abs(scored[j].Distance)
	})
	return scored
}

// ScoreLevelsMinGrade (grading audit §4.6/4.7, 2026-08-25) — ScoreLevels with
// a minGrade floor that CANNOT gut a side of the table: the scorer runs on a
// 2× pool, the sub-floor rows are filtered, and seatBothSides re-balances
// AFTER the filter from the surviving pool, so a min_grade cut refills seats
// with in-band same-side candidates instead of leaving the executor/planner
// table one-sided. Empty minGrade → byte-identical to ScoreLevels.
func ScoreLevelsMinGrade(levels []DetectedLevel, price, dATR float64, freshness func(DetectedLevel) string, maxLevels int, proximityK float64, minGrade string) []ScoredLevel {
	seated, _ := ScoreLevelsMinGradeFull(levels, price, dATR, freshness, maxLevels, proximityK, minGrade)
	return seated
}

// ScoreLevelsMinGradeFull is ScoreLevelsMinGrade returning the GRADED PRE-SEAT
// POOL alongside the seated result. Level-truth wave (2026-08-27) — the planner
// write site uses the pool for machine-grade stamping: a level the model copies
// from the prompt that LOST the seat race (e.g. a far nPOC) must still get its
// machine grade stamped, so the stamp map now records EVERY graded candidate,
// not just the seated top-N.
func ScoreLevelsMinGradeFull(levels []DetectedLevel, price, dATR float64, freshness func(DetectedLevel) string, maxLevels int, proximityK float64, minGrade string) ([]ScoredLevel, []ScoredLevel) {
	return ScoreLevelsMinGradeFullSeats(levels, price, dATR, freshness, maxLevels, proximityK, minGrade, nil, HTFScoreMultiplier)
}

// ScoreLevelsMinGradeFullSeats is ScoreLevelsMinGradeFull with the resolved
// day_plan.htf_seats + htf_score_multiplier knobs (S3) — the production path;
// the legacy wrapper above keeps every historical caller byte-identical.
func ScoreLevelsMinGradeFullSeats(levels []DetectedLevel, price, dATR float64, freshness func(DetectedLevel) string, maxLevels int, proximityK float64, minGrade string, htfSeats *int, htfMult float64) ([]ScoredLevel, []ScoredLevel) {
	eff := maxLevels
	if eff <= 0 {
		eff = DefaultMaxLevels
	}
	if price <= 0 || dATR <= 0 {
		return nil, nil
	}
	levels = researchLevels(levels)
	pool := scoreLevelsPool(levels, price, dATR, freshness, eff*2, proximityK, htfSeats, htfMult)
	filtered := FilterLevelsByMinGrade(pool, minGrade)
	if minGrade == "" || len(filtered) <= eff {
		if len(filtered) > eff {
			researchCap(filtered, eff, "final nearest-first cap")
			filtered = filtered[:eff]
		}
		return filtered, pool
	}
	// Re-seat the filtered pool with the SAME seating rules as ScoreLevels so
	// the both-side guarantee (P0.1) holds after the cut.
	sort.SliceStable(filtered, func(i, j int) bool {
		pi, pj := isTodayPriority(filtered[i].Kind), isTodayPriority(filtered[j].Kind)
		if pi != pj {
			return pi
		}
		if filtered[i].Score != filtered[j].Score {
			return filtered[i].Score > filtered[j].Score
		}
		di, dj := math.Abs(filtered[i].Distance), math.Abs(filtered[j].Distance)
		if di != dj {
			return di < dj
		}
		return filtered[i].Price < filtered[j].Price
	})
	filtered = researchSeat("post-grade HTF seating", filtered, eff, func(s []ScoredLevel, n int) []ScoredLevel { return seatHTF(s, n, htfSeats) })
	filtered = researchSeat("post-grade volume seating", filtered, eff, SeatVolumeFamily) // Pack B — same guarantee after the min_grade cut
	filtered = researchSeat("post-grade both-side seating", filtered, eff, seatBothSides)
	if len(filtered) > eff {
		researchCap(filtered, eff, "final reseating cap")
		filtered = filtered[:eff]
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return math.Abs(filtered[i].Distance) < math.Abs(filtered[j].Distance)
	})
	return filtered, pool
}

// FilterPlanLevelsByMinGrade (grading audit §4.7, 2026-08-25) — the plan-doc
// twin of FilterLevelsByMinGrade, for surfaces that consume []PlanLevel
// (PLAN STATUS, level-state writers). Empty/unknown minGrade is a no-op.
func FilterPlanLevelsByMinGrade(levels []PlanLevel, minGrade string) []PlanLevel {
	min, ok := levelGradeRank[strings.ToUpper(strings.TrimSpace(minGrade))]
	if !ok {
		return levels
	}
	out := make([]PlanLevel, 0, len(levels))
	for _, l := range levels {
		// B2 — Tier-1 labels exempt from the min_grade cut (see above).
		if levelGradeRank[strings.ToUpper(l.Grade)] >= min || IsTier1Label(l.Label) {
			out = append(out, l)
		}
	}
	return out
}

func gradeFromScore(s float64) string {
	switch {
	case s >= 1.0:
		return "A"
	case s >= 0.70:
		return "B"
	default:
		return "C"
	}
}

// LevelClusterTicks is the P0.4 cluster-collapse width in ticks (12 MNQ ticks =
// 3.00 points): levels closer than this are the SAME reference on an intraday
// map, not separate seats. An equal-high/low family within 3 points collapses to
// one entry instead of four separate "A" rows.
const LevelClusterTicks = 12

// clusterToleranceFor derives the cluster width from the MNQ tick size (0.25;
// fallback when price is unset). 12 ticks = 3.00 points.
func clusterToleranceFor(price float64) float64 {
	_ = price
	return LevelClusterTicks * 0.25
}

// collapseLevelClusters merges levels within tol of a STRONGER survivor (kept in
// the same relative position). Kept: highest score, then today-priority kind,
// then nearer distance, then lower price. The survivor's confluence absorbs the
// merged member count; duplicates are removed before seating.
func collapseLevelClusters(scored []ScoredLevel, tol float64) []ScoredLevel {
	if len(scored) < 2 || tol <= 0 {
		return scored
	}
	// Prefer survivors: sort by strength so the first member of any cluster is
	// the keeper.
	order := append([]ScoredLevel(nil), scored...)
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		ai, bi := isTodayPriority(a.Kind), isTodayPriority(b.Kind)
		if ai != bi {
			return ai
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		di, dj := math.Abs(a.Distance), math.Abs(b.Distance)
		if di != dj {
			return di < dj
		}
		return a.Price < b.Price
	})
	kept := make([]ScoredLevel, 0, len(scored))
	for _, cand := range order {
		if isZoneKind(cand.Kind) {
			// Zones are BANDS with their own semantics (proximal/distal),
			// never duplicates of a line level — they survive collapse.
			kept = append(kept, cand)
			continue
		}
		merged := false
		for i := range kept {
			if isZoneKind(kept[i].Kind) {
				continue
			}
			if math.Abs(kept[i].Price-cand.Price) <= tol {
				researchCut(cand.DetectedLevel, fmt.Sprintf("cluster collapsed into %s %.2f [%s]", kept[i].Kind, kept[i].Price, kept[i].Label))
				// R22 (MATH-PLANNER 1g): Confluence is the keeper's count of DISTINCT
				// other families within confBand; a member within tol already sits in
				// that band, so its family is already counted. Summing overlapping
				// counts (+= member + 1) printed ·x48 for a 6-family PDH, max ·x119.
				if kept[i].Research != nil {
					kept[i].Research.Overrides = append(kept[i].Research.Overrides, fmt.Sprintf("cluster member %s %.2f merged; confluence stays %d distinct families; score unchanged", cand.Kind, cand.Price, kept[i].Confluence))
				}
				// fix/collapse-keeps-names (2026-09-10). A level collapsed from a
				// DIFFERENT timeframe is a distinct reference the owner reads
				// differently, not a duplicate — W-TF's D3 promised its name on the
				// map. Carried on a json:"-" field, so the survivor set, its score
				// and the Stage A golden are untouched (E7). Same-timeframe
				// collapses are genuine duplicates and carry nothing. Transitive:
				// whatever the loser had already absorbed rides along.
				if cand.TF != kept[i].TF {
					kept[i].CollapsedNames = appendDistinct(kept[i].CollapsedNames, cand.Label)
				}
				for _, n := range cand.CollapsedNames {
					kept[i].CollapsedNames = appendDistinct(kept[i].CollapsedNames, n)
				}
				merged = true
				break
			}
		}
		if !merged {
			kept = append(kept, cand)
		}
	}
	return kept
}

// MinSideLevels is the SEATING balance target: seatBothSides rebalances the
// seated top-N so each side of price keeps at least this many rows when the
// in-band universe can supply them. (The per-side VALIDATION quota was removed
// entirely by owner ruling 2026-08-31 — this const is not a validator and
// does not reject anything.)
const MinSideLevels = 3

// seatBothSides rebalances the seated top-N so each side of price keeps at least
// MinSideLevels when candidates exist. The input must already be in seating
// order (priority, then score). Pure + deterministic: only swaps the WEAKEST
// currently-seated entries of the over-supplied side for the STRONGEST
// un-seated candidates of the under-supplied side.
// isHTFSwingZone reports whether a scored level is an HTF-origin swing/zone
// (EQH/EQL/S/D/FVG/OB with the HTF flag) — the kinds the seatHTF guarantee
// protects. Multi-day anchors (PDH/PWL…) are today-priority and don't need it.
func isHTFSwingZone(l ScoredLevel) bool {
	if !l.HTF {
		return false
	}
	switch l.Kind {
	case KindEQH, KindEQL, KindSupply, KindDemand, KindFVG, KindIFVG, KindOB:
		return true
	}
	return false
}

// isHTFSeatEligible (B2, 2026-08-26) — the HTF seat rule: Tier-1 structural
// kinds OR displacement patterns (reversal zones) only. HTF continuation zones
// and HTF EQH/EQL no longer outrank the Tier-1/volume map for the 2 seatHTF
// seats (owner: seats are for decision-relevant references, not context).
func isHTFSeatEligible(l ScoredLevel) bool {
	if !l.HTF {
		return false
	}
	if isTier1Kind(l.Kind) {
		return true
	}
	return isZoneKind(l.Kind) && l.ZonePattern == "reversal"
}

// isVolumeFamilyKind (Pack B, 2026-08-26) — the kinds the volume-family seat
// guarantee protects: VWAP family, profile family, pdVWAP/SETT/MID-O.
func isVolumeFamilyKind(k LevelKind) bool {
	switch k {
	case KindVWAP, KindEVWAP, KindPDVWAP, KindVWAP2S, KindPOC, KindNPOC, KindVAH, KindVAL, KindSETT, KindMIDO, KindSWGH, KindSWGL:
		return true
	}
	return false
}

// SeatVolumeFamily (Pack B, 2026-08-26) — reserve ONE seat for the volume
// family when an in-band volume level exists (the E1 acceptance: the first
// plan's seated table carries VWAP/POC). Priority anchors + HTF seats are
// protected; the weakest demotable non-priority head entry is swapped out.
// No-op when a volume level is already seated or nothing was cut.
func SeatVolumeFamily(scored []ScoredLevel, maxLevels int) []ScoredLevel {
	if maxLevels <= 0 {
		maxLevels = DefaultMaxLevels
	}
	if len(scored) <= maxLevels {
		return scored
	}
	head := append([]ScoredLevel(nil), scored[:maxLevels]...)
	tail := append([]ScoredLevel(nil), scored[maxLevels:]...)
	for _, l := range head {
		if isVolumeFamilyKind(l.Kind) {
			return scored // already seated
		}
	}
	best := -1
	for i, l := range tail {
		if !isVolumeFamilyKind(l.Kind) {
			continue
		}
		if best < 0 || l.Score > tail[best].Score {
			best = i
		}
	}
	if best < 0 {
		return scored
	}
	cand := tail[best]
	dropIdx := -1
	for i := len(head) - 1; i >= 0; i-- {
		// Protect A-grade Tier-1 anchors, HTF-eligible seats and other volume
		// rows; B-grade/consumed anchors (target_only in practice) are
		// displaceable — the E1 guarantee must hold even when every seat is an
		// anchor (the live 2026-08-26 map: 8/8 seats were priority anchors).
		if (isTier1Kind(head[i].Kind) && head[i].Grade == "A") || isHTFSeatEligible(head[i]) || isVolumeFamilyKind(head[i].Kind) {
			continue
		}
		dropIdx = i
		break
	}
	if dropIdx < 0 {
		return scored // everything in the head is protected
	}
	tail = append(tail[:best], tail[best+1:]...)
	tail = append(tail, head[dropIdx])
	head = append(head[:dropIdx], head[dropIdx+1:]...)
	head = append(head, cand)
	// NOTE: no re-sort here — a general score re-sort would deterministically
	// re-seat the demoted row (its score outranks the volume candidate; that's
	// why it held the seat) and silently undo the E1 guarantee. The downstream
	// nearest-first sort handles display order.
	return append(head, tail...)
}

// is1HSDZone reports whether a scored level is a 1h-tier supply/demand zone —
// the kind the 1h-wave seat guarantee protects (research: the 1h is the
// setup rung; its S/D bases are the large-account references day traders
// respect). TF survives into ScoredLevel via DetectedLevel.TF.
func is1HSDZone(l ScoredLevel) bool {
	if zoneTierFor(l.TF) != "1h" {
		return false
	}
	switch l.Kind {
	case KindSupply, KindDemand:
		return true
	}
	return false
}

// Seat1HZone (1h wave, 2026-08-25 — research §4 item 2, owner R1) — reserve
// ONE of the two HTF seats for an in-band 1h supply/demand zone when one
// exists. seatHTF fills both seats with the strongest HTF swing/zones, which
// are 4h every time (4h evidence outranks 1h); this post-pass swaps the
// weakest demotable head entry for the best 1h S/D zone in the tail. No-op
// when a 1h S/D zone is already seated, when nothing was cut, or when no
// candidate exists. Pure + deterministic; the seat_1h_zone knob gates the
// CALL SITE, never this function.
func Seat1HZone(scored []ScoredLevel, maxLevels int) []ScoredLevel {
	if maxLevels <= 0 {
		maxLevels = DefaultMaxLevels
	}
	if len(scored) <= maxLevels {
		return scored
	}
	head := append([]ScoredLevel(nil), scored[:maxLevels]...)
	tail := append([]ScoredLevel(nil), scored[maxLevels:]...)
	for _, l := range head {
		if is1HSDZone(l) {
			return scored // already seated
		}
	}
	best := -1
	for i, l := range tail {
		if !is1HSDZone(l) {
			continue
		}
		if best < 0 || l.Score > tail[best].Score {
			best = i
		}
	}
	if best < 0 {
		return scored
	}
	cand := tail[best]
	dropIdx := -1
	for i := len(head) - 1; i >= 0; i-- {
		if isTodayPriority(head[i].Kind) || isHTFSeatEligible(head[i]) {
			continue
		}
		dropIdx = i
		break
	}
	if dropIdx < 0 {
		return scored // everything in the head is protected
	}
	tail = append(tail[:best], tail[best+1:]...)
	tail = append(tail, head[dropIdx])
	head = append(head[:dropIdx], head[dropIdx+1:]...)
	head = append(head, cand)
	out := append(head, tail...)
	// Restore strict seating order (same sort as the seatHTF post-pass).
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := isTodayPriority(out[i].Kind), isTodayPriority(out[j].Kind)
		if pi != pj {
			return pi
		}
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		di, dj := math.Abs(out[i].Distance), math.Abs(out[j].Distance)
		if di != dj {
			return di < dj
		}
		return out[i].Price < out[j].Price
	})
	return out
}

// seatHTF (G2/G3/G6, 2026-08-24; seats knob S3, 2026-09-16) — promote up to N
// HTF swing/zone levels into the head seats (the top-N table is all the model
// sees). Demotes the weakest non-today-priority, non-HTF entries to make room.
//
// seats == nil → the LEGACY path, byte-identical to the pre-S3 code (2 seats,
// whole-list restore sort). That restore sort nullifies the promotion by
// construction (class NN) — kept verbatim so a knob-unset strategy reproduces
// today's table exactly.
// seats != nil → the EFFECTIVE path (CTO ruling 2026-09-17 02:35Z): promote
// *seats, then sort head and tail SEPARATELY with the same comparator — the
// promoted slots keep their seats and the demoted entries stay in the tail.
// *seats ≤ 0 = no HTF seating (a legal knob value).
func seatHTF(scored []ScoredLevel, maxLevels int, seats *int) []ScoredLevel {
	if maxLevels <= 0 {
		maxLevels = DefaultMaxLevels
	}
	if len(scored) <= maxLevels {
		return scored
	}
	if seats == nil {
		return seatHTFLegacy(scored, maxLevels)
	}
	if *seats <= 0 {
		return scored
	}
	head := append([]ScoredLevel(nil), scored[:maxLevels]...)
	tail := append([]ScoredLevel(nil), scored[maxLevels:]...)
	seated := 0
	for _, l := range head {
		if isHTFSeatEligible(l) {
			seated++
		}
	}
	need := *seats - seated
	if need <= 0 {
		return scored
	}
	var cands []ScoredLevel
	for _, l := range tail {
		if isHTFSeatEligible(l) {
			cands = append(cands, l)
		}
	}
	for need > 0 {
		if len(cands) == 0 {
			break
		}
		dropIdx := -1
		for i := len(head) - 1; i >= 0; i-- {
			if isTodayPriority(head[i].Kind) || isHTFSeatEligible(head[i]) {
				continue
			}
			dropIdx = i
			break
		}
		if dropIdx < 0 {
			break
		}
		cand := cands[0]
		cands = cands[1:]
		// Remove the candidate from the tail (it now sits in the head) and
		// move the demoted head entry into the tail.
		for i, l := range tail {
			if l.Price == cand.Price && l.Kind == cand.Kind && l.Label == cand.Label && l.HTF == cand.HTF {
				tail = append(tail[:i], tail[i+1:]...)
				break
			}
		}
		tail = append(tail, head[dropIdx])
		head = append(head[:dropIdx], head[dropIdx+1:]...)
		head = append(head, cand)
		need--
	}
	// EFFECTIVE: sort each block separately, never across the seat boundary —
	// the promoted slots survive (the class-NN fix).
	sortSeatBlock(head)
	sortSeatBlock(tail)
	return append(head, tail...)
}

// seatHTFLegacy is the pre-S3 seatHTF, byte-identical (2 seats, whole-list
// restore sort). Knob unset keeps today's behaviour — including the class-NN
// nullification — until the S4 measurement gates the effective path.
func seatHTFLegacy(scored []ScoredLevel, maxLevels int) []ScoredLevel {
	if maxLevels <= 0 {
		maxLevels = DefaultMaxLevels
	}
	if len(scored) <= maxLevels {
		return scored
	}
	const maxHTFSeats = 2
	head := append([]ScoredLevel(nil), scored[:maxLevels]...)
	tail := append([]ScoredLevel(nil), scored[maxLevels:]...)
	seated := 0
	for _, l := range head {
		if isHTFSeatEligible(l) {
			seated++
		}
	}
	need := maxHTFSeats - seated
	if need <= 0 {
		return scored
	}
	var cands []ScoredLevel
	for _, l := range tail {
		if isHTFSeatEligible(l) {
			cands = append(cands, l)
		}
	}
	for need > 0 {
		if len(cands) == 0 {
			break
		}
		dropIdx := -1
		for i := len(head) - 1; i >= 0; i-- {
			if isTodayPriority(head[i].Kind) || isHTFSeatEligible(head[i]) {
				continue
			}
			dropIdx = i
			break
		}
		if dropIdx < 0 {
			break
		}
		cand := cands[0]
		cands = cands[1:]
		for i, l := range tail {
			if l.Price == cand.Price && l.Kind == cand.Kind && l.Label == cand.Label && l.HTF == cand.HTF {
				tail = append(tail[:i], tail[i+1:]...)
				break
			}
		}
		tail = append(tail, head[dropIdx])
		head = append(head[:dropIdx], head[dropIdx+1:]...)
		head = append(head, cand)
		need--
	}
	out := append(head, tail...)
	// Restore strict seating order (same sort as the pre-pass).
	sortSeatBlock(out)
	return out
}

// sortSeatBlock is the seating comparator (today-priority first, then score
// desc, nearer |distance|, lower price), applied to ONE block.
func sortSeatBlock(s []ScoredLevel) {
	sort.SliceStable(s, func(i, j int) bool {
		pi, pj := isTodayPriority(s[i].Kind), isTodayPriority(s[j].Kind)
		if pi != pj {
			return pi
		}
		if s[i].Score != s[j].Score {
			return s[i].Score > s[j].Score
		}
		di, dj := math.Abs(s[i].Distance), math.Abs(s[j].Distance)
		if di != dj {
			return di < dj
		}
		return s[i].Price < s[j].Price
	})
}

func seatBothSides(scored []ScoredLevel, maxLevels int) []ScoredLevel {
	if maxLevels <= 0 {
		maxLevels = DefaultMaxLevels
	}
	n := len(scored)
	if n <= maxLevels || maxLevels < 2*MinSideLevels {
		// Nothing was cut (the cap is not the constraint) or the cap is too
		// small to hold MinSideLevels on each side — no rebalance.
		return scored
	}
	seated := append([]ScoredLevel(nil), scored[:maxLevels]...)
	rest := append([]ScoredLevel(nil), scored[maxLevels:]...)
	for side := 0; side < 2; side++ {
		count := func(s []ScoredLevel, below bool) int {
			c := 0
			for _, l := range s {
				if (l.Distance < 0) == below {
					c++
				}
			}
			return c
		}
		for _, below := range []bool{true, false} {
			if count(seated, below) >= MinSideLevels {
				continue
			}
			need := MinSideLevels - count(seated, below)
			// Candidates from the missing side, in seating order (strongest
			// first, since rest preserves the sorted order).
			cands := make([]ScoredLevel, 0)
			for _, l := range rest {
				if (l.Distance < 0) == below {
					cands = append(cands, l)
				}
			}
			// Drop the weakest seated levels of the OPPOSITE side to make room.
			// Pack B (2026-08-26) — a seated volume-family row is protected from
			// this swap (the E1 seat guarantee wins over side rebalance).
			for len(cands) > 0 && need > 0 {
				dropIdx := -1
				for i := len(seated) - 1; i >= 0; i-- {
					if (seated[i].Distance < 0) != below && !isVolumeFamilyKind(seated[i].Kind) {
						dropIdx = i
						break
					}
				}
				if dropIdx < 0 {
					break
				}
				cand := cands[0]
				// R22 (MATH-PLANNER 1e): a promoted candidate LEAVES rest — else the
				// second pass re-promotes it and one level takes two seats (read
				// 10969: SWG-L·5m seated twice, OR-H + OB(bear)·1h evicted).
				for i, l := range rest {
					if l.Price == cand.Price && l.Kind == cand.Kind && l.Label == cand.Label && l.HTF == cand.HTF {
						rest = append(rest[:i], rest[i+1:]...)
						break
					}
				}
				rest = append(rest, seated[dropIdx])
				seated = append(seated[:dropIdx], seated[dropIdx+1:]...)
				seated = append(seated, cand)
				cands = cands[1:]
				need--
			}
		}
	}
	// Restore strict seating order for the final nearest-first pass downstream.
	sort.SliceStable(seated, func(i, j int) bool {
		pi, pj := isTodayPriority(seated[i].Kind), isTodayPriority(seated[j].Kind)
		if pi != pj {
			return pi
		}
		if seated[i].Score != seated[j].Score {
			return seated[i].Score > seated[j].Score
		}
		di, dj := math.Abs(seated[i].Distance), math.Abs(seated[j].Distance)
		if di != dj {
			return di < dj
		}
		return seated[i].Price < seated[j].Price
	})
	return seated
}

// RenderKeyLevelsBlock renders the seated levels as the executor prompt block
// (price · label · grade · fresh · signed distance) + one anchor-instruction
// line. Returns "" when there are no levels (the caller injects nothing and logs
// INFO — B9-style dormant block).
func RenderKeyLevelsBlock(scored []ScoredLevel, price float64) string {
	if len(scored) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "KEY LEVELS (map, nearest-first; price %.2f):\n", price)
	for _, s := range scored {
		fresh := s.Fresh
		if s.Confluence > 0 {
			fresh = fmt.Sprintf("%s·x%d", s.Fresh, s.Confluence)
		}
		label := s.Label
		if isHTFSwingZone(s) {
			label = label + " (HTF)"
		}
		role := string(s.Role)
		if role == "" {
			role = string(RoleReactZone)
		}
		sign := "+"
		if s.Distance < 0 {
			sign = "-"
		}
		fmt.Fprintf(&b, "  %-9.2f %-20s %s  %-9s %-15s %s%.1f\n",
			s.Price, label, s.Grade, fresh, role, sign, math.Abs(s.Distance))
	}
	// W-why-no-trades (2026-08-18): "do not chase price between them" was the
	// base-prompt line the model quoted while waiting cycle after cycle (0/3
	// stripped-prompt replays produced an entry). Between levels, a confirmed
	// momentum/breakout setup stays tradeable.
	b.WriteString(KeyLevelsAnchorLine + "\n")
	b.WriteString(RoleLegend)
	return b.String()
}

// KeyLevelsAnchorLine is the KEY LEVELS block's anchor sentence (advisory
// truth). KeyLevelsAnchorStrictLine replaces it in the executor prompt when
// the active plan renders under plan_mode=strict (W-EXEC-TRUTH W3 §3): the
// gate refuses the between-levels momentum entry the advisory line offers.
const (
	KeyLevelsAnchorLine       = "Anchor: react AT these levels (grade A>B>C); between them, a confirmed momentum/breakout may still be traded."
	KeyLevelsAnchorStrictLine = "Anchor: react AT these levels (grade A>B>C); plan_mode=strict: nothing between them is traded off-plan — only an armed plan scenario enters."
)
