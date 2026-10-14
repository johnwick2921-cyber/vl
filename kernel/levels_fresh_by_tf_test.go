package kernel

import (
	"testing"
	"time"

	"vl/market"
)

// S2 — timeframe-aware freshness tests (2026-09-16). Parity rule (canon 53):
// the equivalence test below runs through scoreLevelsPool — the production
// call site — not a re-built input.

func mkKline(openMs int64, o, h, l, c float64) market.Kline {
	return market.Kline{OpenTime: openMs, Open: o, High: h, Low: l, Close: c}
}

// reentryFixture: a zone [102,106]. Bars:
//
//	0-2: formation (inside band), 3: closes fully outside (leaves),
//	4: re-enters (test 1), 5: stays in (same visit), 6: leaves,
//	7: re-enters (test 2), 8: stays in, 9: leaves, 10: re-enters (test 3).
func reentryFixture() []market.Kline {
	base := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC).UnixMilli()
	step := int64(4 * time.Hour / time.Millisecond)
	rows := [][4]float64{ // o,h,l,c
		{103, 104, 102.5, 103.5}, {103.5, 104.5, 103, 104}, {104, 105, 103.5, 104.5}, // formation
		{106.5, 107, 106.2, 106.7},   // leaves (closes fully above)
		{105.5, 106.2, 104.5, 105.8}, // re-enter → test 1
		{104, 105, 103.8, 104.6},     // stays in — same visit
		{106.6, 107, 106.4, 106.8},   // leaves
		{105.4, 106, 104.4, 105.7},   // re-enter → test 2
		{104.2, 105, 103.9, 104.5},   // stays in — same visit
		{106.8, 107.2, 106.5, 107},   // leaves
		{105.3, 106.1, 104.3, 105.6}, // re-enter → test 3
	}
	bars := make([]market.Kline, len(rows))
	for i, r := range rows {
		bars[i] = mkKline(base+int64(i)*step, r[0], r[1], r[2], r[3])
	}
	return bars
}

func TestLevelFreshnessByTF_ReEntryCountsVisitsNotTouches(t *testing.T) {
	bars := reentryFixture()
	now := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	l := DetectedLevel{Kind: KindSupply, Lo: 102, Hi: 106, TF: "4h", HTF: true,
		FormedAtMs: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC).UnixMilli()}
	grade, n, src := LevelFreshnessByTF(l, now, bars)
	if grade != "stale" || n != 3 || src != "formed_at" {
		t.Fatalf("got %q/%d/%q, want stale/3/formed_at (3 re-entry visits)", grade, n, src)
	}
}

func TestLevelFreshnessByTF_FormationNeverCounts(t *testing.T) {
	// Bars that never leave the band: formation only → fresh/0 even if touched 10×.
	bars := reentryFixture()[:3]
	now := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	l := DetectedLevel{Kind: KindSupply, Lo: 102, Hi: 106, TF: "4h", HTF: true,
		FormedAtMs: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC).UnixMilli()}
	grade, n, _ := LevelFreshnessByTF(l, now, bars)
	if grade != "fresh" || n != 0 {
		t.Fatalf("formation-only got %q/%d, want fresh/0 (F1: birth is not a test)", grade, n)
	}
}

func TestLevelFreshnessByTF_OriginFallbackAndNone(t *testing.T) {
	bars := reentryFixture()
	now := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	// FormedAtMs absent → OriginDate midnight fallback; same visit count, but the
	// source must say origin_date.
	l := DetectedLevel{Kind: KindSupply, Lo: 102, Hi: 106, TF: "4h", HTF: true, OriginDate: "2026-09-10"}
	if grade, n, src := LevelFreshnessByTF(l, now, bars); grade != "stale" || n != 3 || src != "origin_date" {
		t.Fatalf("fallback got %q/%d/%q, want stale/3/origin_date", grade, n, src)
	}
	// No origin at all → fresh/0/none.
	l2 := DetectedLevel{Kind: KindSupply, Lo: 102, Hi: 106, TF: "4h", HTF: true}
	if grade, n, src := LevelFreshnessByTF(l2, now, bars); grade != "fresh" || n != 0 || src != "none" {
		t.Fatalf("no-origin got %q/%d/%q, want fresh/0/none", grade, n, src)
	}
}

func TestNormalizeByTFGrade_IdentityOnLegacy(t *testing.T) {
	// Every pre-existing freshness string must pass through untouched (knob OFF
	// byte-identity). freshMult/zoneFreshMult tables are NOT changed.
	legacy := []string{"", "a", "b", "c", "tested", "done", "consumed", "fresh", "A", "B"}
	for _, f := range legacy {
		if got := normalizeByTFGrade(f); got != f {
			t.Fatalf("normalize(%q) = %q, want identity", f, got)
		}
	}
	if normalizeByTFGrade("tested-1") != "b" || normalizeByTFGrade("tested-2") != "c" || normalizeByTFGrade("stale") != "done" {
		t.Fatal("S2 vocabulary mapping wrong: tested-1→b, tested-2→c, stale→done")
	}
}

// TestScoreLevels_ByTFVocabScoresLikeCanonical — production call site: the same
// level pool graded through the S2 vocabulary must score byte-identically to
// the canonical letters the unchanged tables know (tested-1≡b, tested-2≡c,
// stale≡done). Only Research.Freshness (the display string) differs.
func TestScoreLevels_ByTFVocabScoresLikeCanonical(t *testing.T) {
	levels := []DetectedLevel{
		{Kind: KindPDH, Price: 100, Lo: 100, Hi: 100, Label: "PDH", TF: "1d", HTF: true},
		{Kind: KindSupply, Price: 104, Lo: 102, Hi: 106, Label: "Supply·4h", TF: "4h", HTF: true},
	}
	pairs := []struct{ vocab, canon string }{
		{"tested-1", "b"}, {"tested-2", "c"}, {"stale", "done"}, {"fresh", "a"},
	}
	price, dATR := 101.0, 10.0
	for _, p := range pairs {
		withVocab := scoreLevelsPool(levels, price, dATR, func(DetectedLevel) string { return p.vocab }, 8, 0, nil, HTFScoreMultiplier, 0)
		withCanon := scoreLevelsPool(levels, price, dATR, func(DetectedLevel) string { return p.canon }, 8, 0, nil, HTFScoreMultiplier, 0)
		if len(withVocab) != len(withCanon) {
			t.Fatalf("%s vs %s: different seat counts", p.vocab, p.canon)
		}
		for i := range withVocab {
			if withVocab[i].Score != withCanon[i].Score || withVocab[i].Grade != withCanon[i].Grade {
				t.Fatalf("%s vs %s seat %d: score %.6f/%.6f grade %s/%s diverge",
					p.vocab, p.canon, i, withVocab[i].Score, withCanon[i].Score, withVocab[i].Grade, withCanon[i].Grade)
			}
		}
	}
}

// TestS2_CollapsePin_PDHAbsorbsEQH4h — (c) pin: a today-priority PDH absorbing
// an EQH·4h within the cluster tolerance keeps BOTH names on the map.
func TestS2_CollapsePin_PDHAbsorbsEQH4h(t *testing.T) {
	p := 28910.25
	pair := []ScoredLevel{
		{DetectedLevel: DetectedLevel{Kind: KindPDH, Price: p, Lo: p, Hi: p, Label: "PDH", TF: "1d"}, Score: 0.5, Grade: "C"},
		{DetectedLevel: DetectedLevel{Kind: KindEQH, Price: p + 0.25, Lo: p + 0.25, Hi: p + 0.25, Label: "EQH·4h", TF: "4h"}, Score: 1.5, Grade: "A"},
	}
	collapsed := collapseLevelClusters(pair, clusterToleranceFor(p))
	if len(collapsed) != 1 {
		t.Fatalf("collapse kept %d, want 1", len(collapsed))
	}
	if collapsed[0].Label != "PDH" {
		t.Fatalf("survivor %q, want PDH (today-priority wins regardless of score)", collapsed[0].Label)
	}
	names := namesWithCollapsed(collapsed[0])
	if !namesContain(names, "PDH") || !namesContain(names, "EQH·4h") {
		t.Fatalf("names %v must contain both PDH and EQH·4h", names)
	}
}

func namesContain(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
