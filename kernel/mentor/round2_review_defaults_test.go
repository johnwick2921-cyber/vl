package mentor

import "testing"

// Round-2 review (P2/P3): a behaviour the course does not state ships as a
// knob that DEFAULTS OFF (today's September behaviour).
//   - isb_target_fill_close: OctD2.2-R34/R35/R37 say the first partial is taken
//     when the filling candle closes and that real targets are "taught later";
//     they do NOT say an ISB with no level beyond, or a level inside 1R, may be
//     traded. Default OFF = isb_missing_target / isb_target_below_floor refuse.
//   - phl_room_multiple: OctD2.3-R31 @ 27:33 says reward:risk "about 1:2, no
//     longer 1:1"; October states no 1:1 room floor. Default 2 (today's room).
func TestRound2ReviewDefaultsAreToday(t *testing.T) {
	c := DefaultConfig()
	if c.ISBTargetFillClose {
		t.Error("ISBTargetFillClose must default OFF (course does not state a no-target ISB may be traded)")
	}
	if c.PHLRoomMultiple != 2 {
		t.Errorf("PHLRoomMultiple default = %v, want 2 (OctD2.3-R31 states 1:2; no 1:1 room rule in October)", c.PHLRoomMultiple)
	}
}

// At the production default Tick the no-level ISB is refused by name.
func TestRound2ReviewDefaultRefusesNoTargetISB(t *testing.T) {
	e, ins := d2g3Run(t, 0, 100, nil)
	if d2g3Entry(ins) != nil || e.State.Refusals["isb_missing_target"] == 0 {
		t.Fatalf("default config must refuse isb_missing_target; intents %+v ledger %v", ins, e.State.Refusals)
	}
}
