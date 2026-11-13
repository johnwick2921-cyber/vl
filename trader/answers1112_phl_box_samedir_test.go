package trader

import (
	"testing"

	"vl/store"
)

// TestAnswers1112PHL5mBoxSameDirKnob — answers-1112 Q9: unset = OFF (today:
// every PHL/PLH inside a standing 5m ISB box is refused phl_5m_box_ban —
// "experienced only" [Oct D4.3 @01:10:58-01:11:03] is the owner's call); a stored
// true reaches the evaluator (a same-direction PHL/PLH scalp to the box edge
// [@01:08:24-01:08:39, @01:11:20-01:11:45]). The kernel tests
// (kernel/mentor/answers1112_phl_box_samedir_test.go) pin what the evaluator
// does with it.
func TestAnswers1112PHL5mBoxSameDirKnob(t *testing.T) {
	off, on := false, true
	if c := tuningTick(t, &store.MentorTuning{}); c.PHL5mBoxSameDirAllowed {
		t.Fatal("unset phl_5m_box_same_dir_allowed must resolve OFF (today)")
	}
	if c := tuningTick(t, nil); c.PHL5mBoxSameDirAllowed {
		t.Fatal("no mentor_tuning block: phl_5m_box_same_dir_allowed must resolve OFF")
	}
	if c := tuningTick(t, &store.MentorTuning{PHL5mBoxSameDirAllowed: &on}); !c.PHL5mBoxSameDirAllowed {
		t.Fatal("stored phl_5m_box_same_dir_allowed=true must reach the evaluator")
	}
	if c := tuningTick(t, &store.MentorTuning{PHL5mBoxSameDirAllowed: &off}); c.PHL5mBoxSameDirAllowed {
		t.Fatal("stored phl_5m_box_same_dir_allowed=false must reach the evaluator")
	}
}
