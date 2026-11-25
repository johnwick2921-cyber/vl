package mentor

import (
	"strings"
	"testing"
)

// TestArmSnapshotJSON pins m134 DS-108 (a): the per-arm snapshot carries the
// evaluator reads it promises — EMA9/34, the 1h/4h/5m triggers, the HTF verdict,
// the ORB, valid/forming trendlines, the swing and the nearest levels — and
// never fabricates a value it does not have. MUTANT: drop a field (e.g. the
// trendlines or the HTF verdict) → RED.
func TestArmSnapshotJSON(t *testing.T) {
	s := &State{
		EMA9:    29901.5,
		EMA34:   29850.25,
		Trigger: TriggerLine{Dir: SideLong, Price: 29890.0},
		HTF: HTF{
			FourH: TriggerLine{Dir: SideLong, Price: 29780.0},
			OneH:  TriggerLine{Dir: SideLong, Price: 29840.0},
		},
		ORB:   ORB{High: 30010.25, Low: 29726.75, Drawn: true, Escaped: SideLong},
		Swing: SwingState{Line: 29790.0, Pending: nil, Pos: nil},
		ChartTrendlines: []Trendline{
			{Side: SideLong, P0Px: 29600, P1Px: 29750, ValidAt: 0, Dead: false},  // forming
			{Side: SideShort, P0Px: 30100, P1Px: 30000, ValidAt: 7, Dead: false}, // valid
			{Side: SideLong, P0Px: 29500, P1Px: 29600, ValidAt: 3, Dead: true},   // dead → skipped
		},
		ChartLevels: []Level{
			{Kind: KindKeyLevel, Price: 29950},
			{Kind: KindKeyLevel, Price: 29800},
			{Kind: KindEMA9, Price: 29901.5},
			{Kind: KindEMA34, Price: 29850.25},
			{Kind: KindHTFTrigger, Price: 29780},
		},
	}
	snap := s.ArmSnapshot(29900.0)
	j, err := snap.JSON()
	if err != nil {
		t.Fatalf("snapshot marshal: %v", err)
	}
	for _, want := range []string{
		`"ema9":29901.5`, `"ema34":29850.25`,
		`"trigger_5m"`, `"dir":"long"`, `"trigger_1h"`, `"trigger_4h"`,
		`"htf_verdict":"agree_long"`,
		`"drawn":true`, `"escaped":"long"`,
		`"valid":true`, `"forming":true`,
		`"line":29790`,
		`"nearest_levels"`,
	} {
		if !strings.Contains(j, want) {
			t.Fatalf("snapshot missing %q:\n%s", want, j)
		}
	}
	// The dead trendline is excluded; exactly 2 trendlines survive.
	if got := strings.Count(j, `"forming":true`); got != 1 {
		t.Fatalf("want exactly 1 forming trendline, got %d:\n%s", got, j)
	}
	if strings.Contains(j, "29500") {
		t.Fatalf("the dead trendline must not appear:\n%s", j)
	}
}

// TestArmSnapshotEmptyState pins absent ≠ fabricated: a zero state marshals
// with no made-up directions or prices.
func TestArmSnapshotEmptyState(t *testing.T) {
	j, err := (&State{}).ArmSnapshot(0).JSON()
	if err != nil {
		t.Fatalf("empty snapshot: %v", err)
	}
	if strings.Contains(j, `"dir":"`) || strings.Contains(j, `"htf_verdict"`) || strings.Contains(j, `"nearest_levels"`) {
		t.Fatalf("empty state fabricated values:\n%s", j)
	}
}
