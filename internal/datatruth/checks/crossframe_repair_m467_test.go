package checks

import (
	"testing"
	"time"
)

// m467: every cross_frame mismatch reaches the repair hook (the bar-repair
// driver re-asks NT8 for the window). Before m467 the check only paused.
// Real 11-03 rows: 5m 17:55 live_partial vs its 1m (17:56 synthesised inside
// the logged built range, see trader/ninjatrader/bar_repair_m467_test.go).
type m467Src struct{ fakeSrc }

func (m467Src) Bars(_ string, tf int) ([]Bar, error) {
	if tf == 1 {
		return []Bar{
			{T: 1793750100000, O: 28356.25, H: 28362.75, L: 28331.25, C: 28337.75, V: 2166, Final: true},
			{T: 1793750160000, O: 28337.75, H: 28339.0, L: 28318.0, C: 28330.5, V: 2500, Final: true},
			{T: 1793750220000, O: 28330.75, H: 28333.5, L: 28310.75, C: 28320.0, V: 1969, Final: true},
			{T: 1793750280000, O: 28319.25, H: 28331.75, L: 28310.75, C: 28324.0, V: 1466, Final: true},
			{T: 1793750340000, O: 28324.25, H: 28328.0, L: 28312.0, C: 28324.5, V: 1520, Final: true},
			{T: 1793750400000, O: 28324.5, H: 28351.25, L: 28317.0, C: 28346.75, V: 2484, Final: true},
		}, nil
	}
	if tf == 5 {
		return []Bar{
			{T: 1793750100000, O: 28356.25, H: 28362.75, L: 28328.5, C: 28333.25, V: 4666},
			{T: 1793750400000, O: 28324.5, H: 28351.25, L: 28317.0, C: 28346.75, V: 2484},
		}, nil
	}
	return nil, nil
}

// MUTANT (RED): drop the hook call in crossFrameCheck -> no repair is asked.
func TestCrossFrameMismatchIsHandedToRepair(t *testing.T) {
	type call struct {
		sym string
		tf  int
		t   int64
	}
	var got []call
	SetCrossFrameRepair(func(s string, tf int, t int64, _ string) { got = append(got, call{s, tf, t}) })
	defer SetCrossFrameRepair(nil)
	fn := crossFrameCheck(Config{Src: m467Src{}, Cross: DefaultCross(), CrossTFs: []int{5}})
	blocked, why, _, err := fn("MNQ", time.UnixMilli(1793750843801))
	if err != nil || !blocked {
		t.Fatalf("baseline: the 11-03 18:07 pause must reproduce: %v %q %v", blocked, why, err)
	}
	if len(got) != 1 || got[0] != (call{"MNQ", 5, 1793750100000}) {
		t.Fatalf("the mismatch must reach the repair hook once: %+v", got)
	}
}
