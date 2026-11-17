package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// J-BOX-01 — the opening 5m ISB box is a no-trade box. The production replay of
// Mon 02 Nov (the D4.3 / D4.1 morning) is pinned in cmd/mentor-replay
// opening_isb_box_test.go; these pin the opening-box read and the filter.

// jb01Bar5 is a closed 5m candle opening at hh:mm CT on Mon 02 Nov 2026 (CST).
func jb01Bar5(hh, mm int, o, h, l, c float64) market.Kline {
	t := time.Date(2026, 11, 2, hh, mm, 0, 0, ctime()).UnixMilli()
	return market.Kline{OpenTime: t, CloseTime: t + 5*60_000 - 1, Open: o, High: h, Low: l, Close: c, Final: true}
}

// The 02 Nov tape (MNQ 12-26 5m, data.db read-only): 08:30 RED mother, 08:35
// inside bar [Oct D4.3 frame s_02830; @57:20-57:35 the 08:30 candle itself is not
// the inside bar].
func jb01Tape() []market.Kline {
	return []market.Kline{
		jb01Bar5(8, 25, 28758.50, 28762.75, 28717.00, 28721.50),
		jb01Bar5(8, 30, 28721.75, 28739.25, 28672.75, 28709.50),
		jb01Bar5(8, 35, 28710.25, 28727.00, 28639.75, 28711.75),
		jb01Bar5(8, 40, 28711.75, 28748.75, 28687.50, 28714.50),
	}
}

func TestJBOX01_DefaultOn(t *testing.T) {
	if !DefaultConfig().OpeningISBBoxNoTrade {
		t.Fatal("OpeningISBBoxNoTrade default false; J-BOX-01 ships ON [Oct D4.3 @58:00-58:33, @59:50-1:00:29]")
	}
}

func TestJBOX01_IsOpeningISBBox(t *testing.T) {
	cb := jb01Tape()
	box, ok := ISBBoxFromRTH5m(cb[1], cb[2])
	if !ok || box.Dir != SideShort || box.High != 28727.00 || box.Low != 28639.75 {
		t.Fatalf("02 Nov 08:35 box = %+v ok=%v, want short 28,639.75-28,727.00", box, ok)
	}
	if !isOpeningISBBox(box, cb) {
		t.Errorf("the 08:35 inside bar against the 08:30 mother is the opening box")
	}
	// The 08:30 candle can never be the inside bar (R58): no box from (08:25, 08:30).
	if _, ok := ISBBoxFromRTH5m(cb[0], cb[1]); ok {
		t.Errorf("the 08:30 RTH candle read as an inside bar of the 08:25 Globex candle")
	}
	// A first RTH inside bar at 08:40 (08:35 not inside 08:30) is still the opening box.
	cb2 := []market.Kline{
		jb01Bar5(8, 30, 28721.75, 28739.25, 28672.75, 28709.50),
		jb01Bar5(8, 35, 28710.25, 28760.00, 28639.75, 28745.00), // body closes above the 08:30 high: not inside
		jb01Bar5(8, 40, 28711.75, 28748.75, 28687.50, 28714.50), // inside 08:35
	}
	b2, ok := ISBBoxFromRTH5m(cb2[1], cb2[2])
	if !ok || !isOpeningISBBox(b2, cb2) {
		t.Errorf("first RTH inside bar at 08:40: ok=%v opening=%v, want the opening box", ok, isOpeningISBBox(b2, cb2))
	}
	// A SECOND inside bar (08:40 inside 08:35 while 08:35 was already one) is not.
	cb3 := append(append([]market.Kline{}, cb...), jb01Bar5(8, 45, 28714.75, 28744.00, 28701.00, 28731.00))
	b3 := ISBBox{High: 28748.75, Low: 28687.50, Dir: SideLong, AtTime: cb3[3].OpenTime}
	if isOpeningISBBox(b3, cb3) {
		t.Errorf("a box at 08:40 after the 08:35 inside bar is not the opening box")
	}
	// Outside the opening half hour (D4.1 @11:00-11:20): an inside candle at 09:00 is not.
	late := ISBBox{High: 1, Low: 0, Dir: SideShort, AtTime: time.Date(2026, 11, 2, 9, 0, 0, 0, ctime()).UnixMilli()}
	if isOpeningISBBox(late, nil) {
		t.Errorf("an inside candle opening 09:00 CT read as the opening box")
	}
	in := ISBBox{High: 1, Low: 0, Dir: SideShort, AtTime: time.Date(2026, 11, 2, 8, 55, 0, 0, ctime()).UnixMilli()}
	if !isOpeningISBBox(in, nil) {
		t.Errorf("an inside candle opening 08:55 CT (last bucket of the half hour) is the opening box when it is the first")
	}
	// No RTH on a closed day (Saturday).
	sat := ISBBox{High: 1, Low: 0, Dir: SideShort, AtTime: time.Date(2026, 11, 7, 8, 35, 0, 0, ctime()).UnixMilli()}
	if isOpeningISBBox(sat, nil) {
		t.Errorf("a Saturday 08:35 box read as the opening box")
	}
}

func TestJBOX01_GateLiveOnlyWhileUnbroken(t *testing.T) {
	cb := jb01Tape()
	box, _ := ISBBoxFromRTH5m(cb[1], cb[2])
	e := &Evaluator{Cfg: DefaultConfig()}
	e.State.ISBBox = &box
	if e.openingISBBox(cb[:3]) == nil {
		t.Fatalf("the standing 02 Nov opening box does not gate")
	}
	broken := box
	broken.Escaped = SideLong // a 1m close outside: "thoát ra khỏi box rồi thì mình được trade" @1:05:29
	e.State.ISBBox = &broken
	if e.openingISBBox(cb[:3]) != nil {
		t.Errorf("the opening box still gates after a 1m close outside it")
	}
	e.State.ISBBox = &box
	e.Cfg.OpeningISBBoxNoTrade = false
	if e.openingISBBox(cb[:3]) != nil {
		t.Errorf("knob false (revert): the opening box still gates")
	}
}

func TestJBOX01_FilterRefusesEverySetupInside(t *testing.T) {
	box := ISBBox{High: 28748.75, Low: 28639.75, Dir: SideShort}
	cur := market.Kline{Close: 28674.00} // the 08:58 candle, inside the box
	ints := []Intent{
		{Action: PlaceStopLimitEntry, Setup: "ISB", Side: SideShort, Price: 28672.00, Reason: "ISB: sell stop-limit"}, // same side as the box
		{Action: PlaceStopEntry, Setup: "PHL", Side: SideLong, Price: 28760.00, Reason: "PHL/PLH"},                    // priced outside, candle inside
		{Action: PlaceStopEntry, Setup: "SWING4H", Side: SideShort, Price: 28680.00, Reason: "swing 4h EMA34"},        // own machine, exempt
		{Action: CancelArm, ArmID: "x"},
	}
	kept, ref := openingISBBoxFilter(ints, box, cur)
	if len(ref) != 2 || ref[0] != "opening_isb_box_no_trade" {
		t.Errorf("refusals %v, want the ISB and the PHL refused opening_isb_box_no_trade", ref)
	}
	if len(kept) != 2 || kept[0].Setup != "SWING4H" || kept[1].Action != CancelArm {
		t.Errorf("kept %+v, want the swing and the cancel", kept)
	}
	// Candle and entry both outside (a body-mode close outside not yet a break): kept.
	out := market.Kline{Close: 28755.00}
	kept, ref = openingISBBoxFilter([]Intent{{Action: PlaceStopEntry, Setup: "ISB", Side: SideLong, Price: 28760.00, Reason: "ISB"}}, box, out)
	if len(ref) != 0 || len(kept) != 1 {
		t.Errorf("entry and candle outside the box: kept %d refused %v, want kept", len(kept), ref)
	}
}
