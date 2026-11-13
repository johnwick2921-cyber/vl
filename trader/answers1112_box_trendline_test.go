package trader

import (
	"testing"

	"vl/store"
)

// TestAnswers1112BoxTrendlinePartialNotRoomKnob — answers-1112 Q12: unset = ON
// (for a BOX trade a trendline in the path is a place to sell part, not the
// room cap [Sept D3.3 p1 @05:15-07:46; Sept D3.2 p2 @11:06-11:39]); a stored
// false reaches the evaluator (today's box_room to the trendline). The kernel
// tape test (kernel/mentor/answers1112_box_trendline_test.go) pins what the
// evaluator does with it.
func TestAnswers1112BoxTrendlinePartialNotRoomKnob(t *testing.T) {
	off, on := false, true
	if c := tuningTick(t, &store.MentorTuning{}); !c.BoxTrendlinePartialNotRoom {
		t.Fatal("unset box_trendline_partial_not_room must resolve ON [Sept D3.3 p1 @05:15-07:46]")
	}
	if c := tuningTick(t, nil); !c.BoxTrendlinePartialNotRoom {
		t.Fatal("no mentor_tuning block: box_trendline_partial_not_room must resolve ON")
	}
	if c := tuningTick(t, &store.MentorTuning{BoxTrendlinePartialNotRoom: &off}); c.BoxTrendlinePartialNotRoom {
		t.Fatal("stored box_trendline_partial_not_room=false (revert) must reach the evaluator")
	}
	if c := tuningTick(t, &store.MentorTuning{BoxTrendlinePartialNotRoom: &on}); !c.BoxTrendlinePartialNotRoom {
		t.Fatal("stored box_trendline_partial_not_room=true must reach the evaluator")
	}
	// The knob leaves the trendline-target and first-bottom-anchor knobs alone.
	if c := tuningTick(t, &store.MentorTuning{BoxTrendlinePartialNotRoom: &off}); !c.TrendlineIsTarget || !c.TrendlineFirstBottomAnchor {
		t.Fatalf("trendline_is_target=%v trendline_first_bottom_anchor=%v, want both ON (unchanged)", c.TrendlineIsTarget, c.TrendlineFirstBottomAnchor)
	}
}
