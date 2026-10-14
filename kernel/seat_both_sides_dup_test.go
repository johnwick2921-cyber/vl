package kernel

import (
	"fmt"
	"testing"
)

// R22 P2 (MATH-PLANNER 1e) — seatBothSides seated ONE level twice when a side
// had fewer than MinSideLevels candidates: the promoted candidate never left
// `rest`, so the second pass promoted it again and evicted another level.
// Production read 10969 (10-08 18:30 CT) showed SWG-L·5m twice; OR-H and
// OB(bear)·1h lost their seats. Production call site: ScoreLevelsMinGradeFullSeats
// (the planner's AssembleScoredLevelsFullMinGrade and the executor's
// AssembleScoredLevelsMinGrade both seat through it).
func TestSeatBothSidesNeverSeatsALevelTwice(t *testing.T) {
	price := 30000.0
	var levels []DetectedLevel
	for i, k := range []LevelKind{KindPDH, KindRTHH, KindORH, KindONH, KindPDC, KindORL} {
		p := price + 100 + float64(i)*20
		levels = append(levels, DetectedLevel{Kind: k, Price: p, Label: string(k)})
	}
	// Only TWO candidates below price (MinSideLevels is 3).
	levels = append(levels, DetectedLevel{Kind: KindPWL, Price: 29900, Label: "PWL"}, DetectedLevel{Kind: KindPML, Price: 29850, Label: "PML"})
	seated, _ := ScoreLevelsMinGradeFullSeats(levels, price, 300, nil, 6, 1.5, "B", nil, HTFScoreMultiplier)
	seen := map[string]int{}
	below := 0
	for _, l := range seated {
		seen[fmt.Sprintf("%s@%.2f", l.Kind, l.Price)]++
		if l.Distance < 0 {
			below++
		}
	}
	for k, n := range seen {
		if n > 1 {
			t.Fatalf("level %s takes %d seats — one level, one seat: %v", k, n, seen)
		}
	}
	if len(seated) != 6 || below != 2 {
		t.Fatalf("cap 6 with both below candidates seated once: got %d seated, %d below: %v", len(seated), below, seen)
	}
}
