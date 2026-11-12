package mentor

import "testing"

// Round-2 review (P2/P3): a behaviour the course does not state ships as a
// knob that DEFAULTS OFF (today's September behaviour).
//   - isb_target_fill_close: OctD2.2-R34/R35/R37 say the first partial is taken
//     when the filling candle closes and that real targets are "taught later";
//     they do NOT say an ISB with a level inside 1R may be traded. Default OFF =
//     isb_target_below_floor refuses. The NO-level half moved to
//     isb_no_level_fill_close (answers-1112 Q3, default ON: Oct D2.2
//     @28:37-28:56, @30:24-30:43 trade it, first target the fill candle's close;
//     isb_no_level_fill_close=false reverts, TestAnswers1112ISBNoLevelKnobOffIsToday).
//   - phl_room_multiple: answers-1112 Q2 moved this default to October's 1:1
//     [Oct D2.3 @49:37-49:56 "stop-loss 40 điểm ... target 40 điểm ... Risk
//     reward 1-1 ... at least là cũng 35 điểm"; WE10 frames s_04120/s_04126:
//     3.31R dragged in to the old low, R:R 1.71 taken]. R31 @ 27:33 "1,2" is one
//     trade's result (s_01661 reads 1.8), not a minimum. phl_room_multiple=2
//     reverts (TestAnswers1112PHLRoomWorkedTradesAcceptedAtDefault).
func TestRound2ReviewDefaultsAreToday(t *testing.T) {
	c := DefaultConfig()
	if c.ISBTargetFillClose {
		t.Error("ISBTargetFillClose must default OFF (course does not state a level-inside-1R ISB may be traded)")
	}
	if !c.ISBNoLevelFillClose {
		t.Error("ISBNoLevelFillClose must default ON (answers-1112 Q3: Oct D2.2 @28:37-28:56, @30:24-30:43)")
	}
	if c.PHLRoomMultiple != 1 {
		t.Errorf("PHLRoomMultiple default = %v, want 1 (answers-1112 Q2: Oct D2.3 @49:37-49:56 \"Risk reward 1-1\"; WE10 s_04126 R:R 1.71)", c.PHLRoomMultiple)
	}
}

// At the production default Tick the no-level ISB is TRADED (answers-1112 Q3);
// the revert knob isb_no_level_fill_close=false refuses it by name, as before.
func TestRound2ReviewDefaultRefusesNoTargetISB(t *testing.T) {
	e, ins := d2g3Run(t, 0, 100, nil)
	if d2g3Entry(ins) == nil || e.State.Refusals["isb_missing_target"] != 0 {
		t.Fatalf("default config must trade the no-level ISB (answers-1112 Q3); intents %+v ledger %v", ins, e.State.Refusals)
	}
	e, ins = d2g3Run(t, 0, 100, func(c *Config) { c.ISBNoLevelFillClose = false })
	if d2g3Entry(ins) != nil || e.State.Refusals["isb_missing_target"] == 0 {
		t.Fatalf("isb_no_level_fill_close=false must refuse isb_missing_target; intents %+v ledger %v", ins, e.State.Refusals)
	}
}
