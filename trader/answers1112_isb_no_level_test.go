package trader

import (
	"testing"

	"vl/store"
)

// TestAnswers1112ISBNoLevelFillCloseKnob — answers-1112 Q3: unset = ON (an ISB
// with no level beyond is traded [Oct D2.2 @28:37-28:56, @30:24-30:43]); a
// stored false reaches the evaluator (today's isb_missing_target refusal), and
// isb_target_fill_close keeps its default OFF and its own value.
func TestAnswers1112ISBNoLevelFillCloseKnob(t *testing.T) {
	off, on := false, true
	if c := tuningTick(t, &store.MentorTuning{}); !c.ISBNoLevelFillClose || c.ISBTargetFillClose {
		t.Fatalf("unset: isb_no_level_fill_close=%v isb_target_fill_close=%v, want true / false", c.ISBNoLevelFillClose, c.ISBTargetFillClose)
	}
	if c := tuningTick(t, &store.MentorTuning{ISBNoLevelFillClose: &off}); c.ISBNoLevelFillClose {
		t.Fatal("stored isb_no_level_fill_close=false (revert) must reach the evaluator")
	}
	if c := tuningTick(t, &store.MentorTuning{ISBTargetFillClose: &on}); !c.ISBTargetFillClose || !c.ISBNoLevelFillClose {
		t.Fatalf("stored isb_target_fill_close=true: %v / no-level %v, want true / true (independent knobs)", c.ISBTargetFillClose, c.ISBNoLevelFillClose)
	}
}

// TestAnswers1112ISBNoLevelPartialAtFillClose — the first target of the
// no-level ISB is the fill candle's close. The kernel emits the +1R leg-1 TP
// (kernel TestAnswers1112ISBNoLevelTradedAtDefault, the s_02392 short: entry
// 28,709.50, stop 28,735.75, risk 26.25 — "Stop Loss của anh chị là 27 điểm.
// Stop Gain … đợi 27 điểm, anh chị bán … Hoặc ngay tới đây, anh chị bán bớt,
// vẫn được" [Oct D2.2 @39:47-39:59]); the exit drive takes the partial at the
// fill candle's close when +1R was not reached inside it. The fill candle's
// prices are illustrative [C]: the frame does not show its close.
func TestAnswers1112ISBNoLevelPartialAtFillClose(t *testing.T) {
	pos := mentorPosition{Side: "short", Origin: "ISB", Entry: 28709.50, Stop: 28735.75, R: 26.25}
	if got := mentorISBPartialTP(pos, 28695, 28712, 28690); got != 28695 {
		t.Fatalf("fill candle closes 28,695 short of +1R: partial at %.2f, want the close 28,695.00 (\"ngay tới đây, anh chị bán bớt\")", got)
	}
	if got := mentorISBPartialTP(pos, 28690, 28712, 28683.25); got != 0 {
		t.Fatalf("fill candle reaches +1R 28,683.25: the native +1R TP filled (\"đợi 27 điểm, anh chị bán\"), want 0, got %.2f", got)
	}
}
