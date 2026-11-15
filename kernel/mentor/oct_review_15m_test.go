package mentor

import "testing"

// Review fix (fidelity P2): D4.3-R9 @07:28 ("only the 5m, at this moment") turned the whole 15m/30m
// machinery off, but the SAME video teaches the 15m check: OctD4.3-R60 @58:38 "Day la ly do tai sao
// buoi sang ngay hom nay anh chi thua ne. Tai vi dang lam Insapa khung 15 phut" (the 08:30 cluster is a
// 15m inside bar; the morning losses came from trading inside it; check by switching to 15m). Open
// mentor question 2 of Ngay 4.3 (whether the 15m check still applies) was never answered, so the
// R60 no-trade stays ON behind its own knob Box15mNoTrade (revert = false). The 30m box, tf_conflict,
// MTF confluence, the early 15m read and the 15m IB 5m-ISB rule stay OFF.
func TestOctReview15mBoxNoTradeStaysOn(t *testing.T) {
	d := DefaultConfig()
	if !d.Box15mNoTrade {
		t.Fatal("Box15mNoTrade default must be ON (OctD4.3-R60 @58:38)")
	}
	if d.HTFBoxGates || d.TFConflictEnabled || d.MTFConfluenceEnabled || d.Early15mRead || d.FifteenMinIBRule != FifteenMinOff {
		t.Fatal("the other 15m/30m gates stay OFF (D4.3-R9)")
	}
	e, bars, now := mtfFixture(t, false) // long ISB
	e.Cfg = d
	e.Cfg.Enabled = true
	e.Cfg.KeyLevelTFMinutes = 1
	e.Cfg.EMAPeriod34 = 0
	e.Cfg.EMAPeriod9 = 9
	e.Cfg.ISBReverseEMA9Enabled = false
	e.Cfg.ISBBufferPts = 0
	e.Cfg.LocTriggerFilter = false
	e.Cfg.RoomMultiple = 0
	e.Cfg.Exec2mAfter30m = false
	e.State.ISBBox15m = &ISBBox{High: 110, Low: 95, Dir: SideShort, AtTime: bars[0].OpenTime}
	e.State.ISBBox30m = &ISBBox{High: 110, Low: 95, Dir: SideShort, AtTime: bars[0].OpenTime}
	ins := e.Tick(bars, now)
	if got := entryIntents(ins); len(got) != 0 {
		t.Fatalf("counter-direction ISB inside a standing 15m box must be refused, got %+v", got)
	}
	if e.State.Refusals["isb_box_blocked_15m"] == 0 {
		t.Fatalf("ledger = %v, want isb_box_blocked_15m", e.State.Refusals)
	}
	if e.State.ISBBox30m != nil {
		t.Fatal("the 30m box must stay unbuilt (D4.3-R9)")
	}
}
