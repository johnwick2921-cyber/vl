package kernel

import (
	"testing"
)

// R-A13 (owner ruling, S-wave 2026-08-26) — volume anchors JOIN the Tier-1
// set: a pattern zone within 12 ticks of VAH now escapes the C-cap (before the
// ruling, VAH was not Tier-1 and the zone graded C regardless of score).
func TestTier1VolumeAnchorPatternGate(t *testing.T) {
	levels := []DetectedLevel{
		{Kind: KindVAH, Price: 100.5, Lo: 100.5, Hi: 100.5, Label: "VAH", TF: "15m"},
		{Kind: KindFVG, Price: 100.8, Lo: 100.7, Hi: 100.9, Label: "FVG", TF: "15m"},
		{Kind: KindRound, Price: 100.6, Lo: 100.6, Hi: 100.6, Label: "RN100", TF: "1m"}, // confluence, NOT Tier-1
	}
	scored := scoreLevelsPool(levels, 100.0, 1000.0, nil, 8, 1.5, nil, HTFScoreMultiplier, 0)
	var fvg *ScoredLevel
	for i := range scored {
		if scored[i].Kind == KindFVG {
			fvg = &scored[i]
			break
		}
	}
	if fvg == nil {
		t.Fatal("FVG must seat")
	}
	if fvg.Grade != "B" {
		t.Fatalf("FVG beside VAH (R-A13 Tier-1) must escape the C-cap, got %s (score %.2f)", fvg.Grade, fvg.Score)
	}
}

// R-A13 — the min_grade exception and label surface honor the volume anchors.
func TestTier1VolumeAnchorsInSets(t *testing.T) {
	for _, k := range []LevelKind{KindVAH, KindVAL, KindSETT, KindNPOC} {
		if !isTier1Kind(k) {
			t.Fatalf("kind %s must be Tier-1 after R-A13", k)
		}
	}
	for _, l := range []string{"VAH", "VAL", "SETT", "nPOC·2026-08-25", "nPOC·wk·2026-08-20"} {
		if !IsTier1Label(l) {
			t.Fatalf("label %q must be Tier-1 after R-A13", l)
		}
	}
}
