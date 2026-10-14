package kernel

import (
	"reflect"
	"strings"
	"testing"
)

func TestScoreLevels(t *testing.T) {
	levels := []DetectedLevel{
		lineLevel(KindPDH, 15600, "PDH", "d", true),
		lineLevel(KindPDL, 15450, "PDL", "d", true),
		lineLevel(KindPDC, 15555, "PDC", "d", true),
		lineLevel(KindRound, 15550, "RN 15550 (50)", "", false),
		lineLevel(KindPWH, 16000, "PWH", "w", true), // 470 pts away → proximity-filtered
		lineLevel(KindONH, 15500, "ONH", "d", false),
		{Kind: KindDemand, Price: 15400, Lo: 15390, Hi: 15410, Label: "Demand", OriginDate: "d"}, // standalone → excluded
		{Kind: KindSupply, Price: 15552, Lo: 15548, Hi: 15556, Label: "Supply", OriginDate: "d"}, // clustered → C
	}
	freshness := func(l DetectedLevel) string {
		if l.Kind == KindONH {
			return "done" // consumed → role-flip, stays on the map (P1c)
		}
		return "" // fresh
	}
	scored := ScoreLevels(levels, 15530, 200, freshness, 8, 1.5)

	grades := map[LevelKind]string{}
	fresh := map[LevelKind]string{}
	present := map[LevelKind]bool{}
	for _, s := range scored {
		grades[s.Kind] = s.Grade
		fresh[s.Kind] = s.Fresh
		present[s.Kind] = true
	}

	if present[KindPWH] {
		t.Fatalf("PWH is beyond ±1.5×dATR → must be proximity-filtered")
	}
	if !present[KindONH] || fresh[KindONH] != "flipped" {
		t.Fatalf("consumed ONH must be seated as flipped (P1c), got present=%v fresh=%q", present[KindONH], fresh[KindONH])
	}
	if present[KindDemand] {
		t.Fatalf("standalone demand zone must be excluded (confluence-only)")
	}
	if grades[KindPDH] != "A" || grades[KindPDL] != "A" || grades[KindPDC] != "A" {
		t.Fatalf("HTF structurals should grade A: %v", grades)
	}
	if grades[KindRound] != "B" {
		t.Fatalf("clustered round number should grade B, got %q", grades[KindRound])
	}
	if grades[KindSupply] != "C" {
		t.Fatalf("clustered supply zone must be capped at C, got %q", grades[KindSupply])
	}
	// Output is nearest-first: RN 15550 (dist 20) leads.
	if len(scored) == 0 || scored[0].Price != 15550 {
		t.Fatalf("nearest-first ordering broken: first = %+v", scored[0])
	}
}

func TestScoreLevelsCapAndPriority(t *testing.T) {
	// 4 today-priority + 4 round numbers, cap 3 → only priority survive.
	levels := []DetectedLevel{
		lineLevel(KindPDH, 15600, "PDH", "d", true),
		lineLevel(KindPDL, 15460, "PDL", "d", true),
		lineLevel(KindONH, 15590, "ONH", "d", false),
		lineLevel(KindORH, 15580, "OR-H", "d", false),
		lineLevel(KindRound, 15525, "RN 15525 (25)", "", false),
		lineLevel(KindRound, 15575, "RN 15575 (25)", "", false),
		lineLevel(KindRound, 15625, "RN 15625 (25)", "", false),
		lineLevel(KindRound, 15475, "RN 15475 (25)", "", false),
	}
	scored := ScoreLevels(levels, 15530, 200, nil, 3, 1.5)
	if len(scored) != 3 {
		t.Fatalf("cap 3 not honored: got %d", len(scored))
	}
	for _, s := range scored {
		if s.Kind == KindRound {
			t.Fatalf("round numbers must lose their seats to today-priority levels: %+v", s)
		}
	}
}

func TestRenderKeyLevelsBlock(t *testing.T) {
	scored := ScoreLevels([]DetectedLevel{
		lineLevel(KindPDH, 15600, "PDH", "d", true),
		lineLevel(KindPDC, 15555, "PDC", "d", true),
	}, 15530, 200, nil, 8, 1.5)
	block := RenderKeyLevelsBlock(scored, 15530)
	if !strings.HasPrefix(block, "KEY LEVELS (map, nearest-first; price 15530.00):") {
		t.Fatalf("block header wrong:\n%s", block)
	}
	if !strings.Contains(block, "PDH") || !strings.Contains(block, "PDC") {
		t.Fatalf("block missing a level:\n%s", block)
	}
	if !strings.Contains(block, "Anchor: react AT these levels") {
		t.Fatalf("block missing anchor line:\n%s", block)
	}
	// Empty input → empty block (dormant / B9-style).
	if RenderKeyLevelsBlock(nil, 15530) != "" {
		t.Fatalf("empty scored set must render an empty block")
	}
}

// P0.1 (2026-08-19) — a gap-down day must NOT ship a one-sided map.
func TestScoreLevelsBothSideSeating(t *testing.T) {
	price := 29600.0
	dATR := 300.0
	// Every today-priority kind above price (the 2026-08-18 pathology) +
	// round numbers and a prior-week low below.
	above := []DetectedLevel{
		lineLevel(KindONL, 29680.75, "ONL", "", false),
		lineLevel(KindPDL, 29853.0, "PDL", "", false),
		lineLevel(KindPDC, 29919.0, "PDC", "", false),
		lineLevel(KindPDH, 30054.0, "PDH", "", false),
		lineLevel(KindRTHH, 30079.0, "RTH-H", "", false),
		lineLevel(KindORH, 29644.5, "OR-H", "", false),
		lineLevel(KindORL, 29590.0, "OR-L", "", false),
		lineLevel(KindEQL, 29380.0, "EQL", "", false),
	}
	below := []DetectedLevel{
		lineLevel(KindPWL, 29360.0, "PWL", "", true),
		lineLevel(KindRound, 29400.0, "RN 29400", "", false),
		lineLevel(KindRound, 29300.0, "RN 29300", "", false),
	}
	levels := append(append([]DetectedLevel{}, above...), below...)
	got := ScoreLevels(levels, price, dATR, nil, 8, 1.5)
	belowCount := 0
	for _, l := range got {
		if l.Distance < 0 {
			belowCount++
		}
	}
	if belowCount < MinSideLevels {
		t.Fatalf("gap-down seating kept only %d levels below price, want >= %d: %+v", belowCount, MinSideLevels, got)
	}
	if len(got) > 8 {
		t.Fatalf("cap violated: %d", len(got))
	}
}

// P0.4 (2026-08-19) — an EQ family within 3 points must collapse to ONE entry.
func TestScoreLevelsClusterCollapse(t *testing.T) {
	price := 30000.0
	dATR := 300.0
	levels := []DetectedLevel{
		lineLevel(KindEQL, 30089.25, "EQL", "d1", false),
		lineLevel(KindEQL, 30090.75, "EQL", "d2", false),
		lineLevel(KindEQH, 30091.5, "EQH", "d3", false),
		lineLevel(KindEQL, 30092.0, "EQL", "d4", false),
		lineLevel(KindPDH, 30150.0, "PDH", "", false),
	}
	got := ScoreLevels(levels, price, dATR, nil, 8, 1.5)
	eq := 0
	for _, l := range got {
		if l.Kind == KindEQL || l.Kind == KindEQH {
			eq++
		}
	}
	if eq != 1 {
		t.Fatalf("EQ cluster within 3pts must collapse to one entry, got %d: %+v", eq, got)
	}
}

// P0.5 (2026-08-19) — an HTF zone seats on its own merit (grade C) even with
// zero confluence; a pure intraday zone still does not.
func TestScoreLevelsHTFZoneSeatsAlone(t *testing.T) {
	price := 30000.0
	dATR := 300.0
	htfZone := DetectedLevel{Kind: KindSupply, Price: 30050, Lo: 30040, Hi: 30060, Label: "S/D", HTF: true}
	intradayZone := DetectedLevel{Kind: KindDemand, Price: 29950, Lo: 29940, Hi: 29960, Label: "S/D", HTF: false}
	got := ScoreLevels([]DetectedLevel{htfZone, intradayZone}, price, dATR, nil, 8, 1.5)
	seatedHTF := false
	for _, l := range got {
		if l.Kind == KindSupply {
			seatedHTF = true
			if l.Grade != "C" {
				t.Fatalf("HTF zone must be capped at C, got %s", l.Grade)
			}
		}
		if l.Kind == KindDemand {
			t.Fatalf("zero-confluence intraday zone must NOT seat: %+v", got)
		}
	}
	if !seatedHTF {
		t.Fatalf("HTF zone did not seat: %+v", got)
	}
}

// P0.4-E (2026-08-24 audit): an unknown TF must fall back to the "1m" tier —
// a raw pass-through missed the zoneTFMult table and zeroed the whole score.
func TestZoneTierForUnknownFallsBackTo1m(t *testing.T) {
	if got := zoneTierFor("D"); got != "1m" {
		t.Fatalf("zoneTierFor(D) = %q, want 1m (noise floor, never a zero mult)", got)
	}
	if got := zoneTierFor(""); got != "1m" {
		t.Fatalf("zoneTierFor(\"\") = %q, want 1m", got)
	}
	for tf, want := range map[string]string{"30m": "15m", "2h": "1h", "6h": "4h", "8h": "4h", "12h": "4h", "15m": "15m", "1h": "1h", "4h": "4h"} {
		if got := zoneTierFor(tf); got != want {
			t.Fatalf("zoneTierFor(%s) = %q, want %q", tf, got, want)
		}
	}
}

// P0.4-E (2026-08-24 audit): GradeRank orders A > B > C with unknown = 0 —
// the machine-grade stamp keeps the stronger grade on a rounded-price collision.
func TestGradeRank(t *testing.T) {
	if GradeRank("A") <= GradeRank("B") || GradeRank("B") <= GradeRank("C") || GradeRank("C") <= GradeRank("") {
		t.Fatal("GradeRank ordering wrong: want A > B > C > unknown")
	}
}

// FIX-LEVEL-GRADE (2026-10-14) — the confluence band knob must be byte-identical
// when unset (confBandPts ≤ 0 → 0.10×dATR), and a fixed point width must spread
// the grades (tighter → fewer A).
type scoredProj struct {
	kind  LevelKind
	price float64
	score float64
	conf  int
	grade string
}

func projectScored(s []ScoredLevel) []scoredProj {
	out := make([]scoredProj, 0, len(s))
	for _, l := range s {
		out = append(out, scoredProj{l.Kind, l.Price, l.Score, l.Confluence, l.Grade})
	}
	return out
}

func TestConfluenceBandDefaultByteIdentical(t *testing.T) {
	levels := []DetectedLevel{
		lineLevel(KindPDH, 30000, "PDH", "d", true),
		lineLevel(KindPDL, 29850, "PDL", "d", true),
		lineLevel(KindONH, 29950, "ONH", "d", false),
		lineLevel(KindRound, 29990, "RN", "", false),
		{Kind: KindVWAP, Price: 29940, Lo: 29930, Hi: 29950, Label: "VWAP", OriginDate: "d"},
	}
	freshness := func(l DetectedLevel) string { return "" }
	def := ScoreLevels(levels, 29970, 200, freshness, 8, 1.5)
	band := ScoreLevelsBand(levels, 29970, 200, freshness, 8, 1.5, 0)
	neg := ScoreLevelsBand(levels, 29970, 200, freshness, 8, 1.5, -3) // ≤0 → default
	if !reflect.DeepEqual(projectScored(def), projectScored(band)) {
		t.Fatalf("ScoreLevelsBand(…, 0) must be byte-identical to ScoreLevels:\ndef=%+v\nband=%+v", projectScored(def), projectScored(band))
	}
	if !reflect.DeepEqual(projectScored(def), projectScored(neg)) {
		t.Fatalf("ScoreLevelsBand(…, ≤0) must be byte-identical to ScoreLevels:\ndef=%+v\nneg=%+v", projectScored(def), projectScored(neg))
	}
}

func TestConfluenceBandKnobSpreadsGrades(t *testing.T) {
	// dATR=350 → default band ±35pt. ONH at 29990 and VWAP at 30010 are 20pt
	// apart, so the default band counts them as one confluence family (ONH → A),
	// while a ±3pt band sees no confluence (ONH → B, evidence 0.85).
	levels := []DetectedLevel{
		lineLevel(KindONH, 29990, "ONH", "d", false),
		{Kind: KindVWAP, Price: 30010, Lo: 30005, Hi: 30015, Label: "VWAP", OriginDate: "d"},
	}
	freshness := func(l DetectedLevel) string { return "" }
	gradeFor := func(s []ScoredLevel, k LevelKind) string {
		for _, l := range s {
			if l.Kind == k {
				return l.Grade
			}
		}
		return ""
	}
	def := ScoreLevels(levels, 30000, 350, freshness, 8, 1.5)
	if got := gradeFor(def, KindONH); got != "A" {
		t.Fatalf("default band (±35pt): ONH with one confluence family should grade A, got %q (scores %+v)", got, projectScored(def))
	}
	tight := ScoreLevelsBand(levels, 30000, 350, freshness, 8, 1.5, 3)
	if got := gradeFor(tight, KindONH); got != "B" {
		t.Fatalf("±3pt band: ONH with no in-band family should grade B, got %q (scores %+v)", got, projectScored(tight))
	}
}
