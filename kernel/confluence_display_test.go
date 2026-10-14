package kernel

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// R22 P2 (MATH-PLANNER 1g) — the "·xN" the executor AI reads must be the
// count of DISTINCT confluent families, never a sum of overlapping counts.
// collapseLevelClusters did kept.Confluence += member.Confluence + 1 on every
// merge: 4,308 / 5,714 seated rows displayed a number different from their
// family count, the max was x119 (10 families exist). Production path: the
// scorer → RenderKeyLevelsBlock, which feeds the executor prompt.
func TestConfluenceDisplayIsDistinctFamilies(t *testing.T) {
	price := 30000.0
	levels := []DetectedLevel{
		{Kind: KindPDH, Price: 30100, Label: "PDH"},
		{Kind: KindONH, Price: 30101, Label: "ONH"},
		{Kind: KindPOC, Price: 30102, Label: "POC"},
		{Kind: KindRound, Price: 30102.5, Label: "RN 30102.5"},
		{Kind: KindPDL, Price: 29800, Label: "PDL"},
	}
	seated, _ := ScoreLevelsMinGradeFullSeats(levels, price, 300, nil, 12, 1.5, "", nil, HTFScoreMultiplier)
	block := RenderKeyLevelsBlock(seated, price)
	var pdh *ScoredLevel
	for i := range seated {
		if seated[i].Kind == KindPDH {
			pdh = &seated[i]
		}
	}
	if pdh == nil {
		t.Fatalf("PDH must seat:\n%s", block)
	}
	// PDH's other families within 0.10×dATR: overnight (ONH), profile (POC),
	// round (RN) — 3, whatever merged into it.
	if pdh.Confluence != 3 || !strings.Contains(block, "fresh·x3") {
		t.Fatalf("PDH confluence must be its 3 distinct families, got %d:\n%s", pdh.Confluence, block)
	}
	for _, m := range regexp.MustCompile(`·x(\d+)`).FindAllStringSubmatch(block, -1) {
		if n, _ := strconv.Atoi(m[1]); n > 9 {
			t.Fatalf("a distinct-family count cannot exceed 9 (10 families): x%d\n%s", n, block)
		}
	}
}
