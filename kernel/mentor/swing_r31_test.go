package mentor

import (
	"testing"

	"vl/market"
)

// R31 [D5.2 p1 @15:48–16:24 "1 cây nến sau đó đóng ở trên level… không đặt
// lệnh"]: while a swing REJECT order rests, any later 5m close THROUGH the line
// cancels it — the level went invalid, so the resting stop order must not keep
// holding the account's single entry slot (one_contract.go) against a line price
// has already cut through.
//
// These pins run through SwingTick — the swing's production call site
// (eval.go runSwing → SwingTick).

// TestSwingR31RestSurvivesANonThroughClose — a later bar that closes on the
// entry side (no through) leaves the order resting: no CancelArm, Pending kept.
// TestSwingR31CloseThroughCancelsRestingOrder — a later bar that closes THROUGH
// the line emits CancelArm(swing ArmID) and clears Pending.

func swingR31Entry(t *testing.T) (*SwingState, []market.Kline, string, SwingCfg) {
	t.Helper()
	cfg := DefaultSwingCfg()
	entryBars := swingTape(t, []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),     // prev below (resistance)
		mk5m(t, 15, 5, 5, 10160, 10175, 10155, 10160), // short reject → resting sell stop 10155
	})
	s := &SwingState{}
	out := SwingTick(s, entryBars, cfg, entryBars[len(entryBars)-1].OpenTime+60_000)
	var armID string
	for _, in := range out {
		if in.Action == PlaceStopEntry {
			armID = in.ArmID
		}
	}
	if s.Pending == nil || armID == "" || s.Pending.Side != SideShort {
		t.Fatalf("setup: want a resting SHORT swing with an ArmID; pending=%v armID=%q out=%+v", s.Pending, armID, out)
	}
	return s, entryBars, armID, cfg
}

func TestSwingR31CloseThroughCancelsRestingOrder(t *testing.T) {
	s, entryBars, armID, cfg := swingR31Entry(t)

	// 05:10 — no close through (close 10158 < line 10168.16), no fill (low
	// 10156 > entry 10155): the order rests, no cancel.
	rest := append(entryBars, mk5m(t, 15, 5, 10, 10158, 10160, 10156, 10158))
	out2 := SwingTick(s, rest, cfg, rest[len(rest)-1].OpenTime+60_000)
	for _, in := range out2 {
		if in.Action == CancelArm {
			t.Fatalf("a close on the entry side must not cancel the resting order; got %+v", out2)
		}
	}
	if s.Pending == nil {
		t.Fatal("the resting order must survive a bar that does not close through")
	}

	// 05:15 — closes THROUGH (close 10200 > line; low 10170 > entry so it does
	// not fill): CancelArm(swing ArmID) emitted, Pending cleared.
	through := append(rest, mk5m(t, 15, 5, 15, 10170, 10200, 10170, 10200))
	out3 := SwingTick(s, through, cfg, through[len(through)-1].OpenTime+60_000)
	cancelled := false
	for _, in := range out3 {
		if in.Action == CancelArm && in.ArmID == armID {
			cancelled = true
		}
	}
	if !cancelled {
		t.Fatalf("a later close through the line must CancelArm(%s); got %+v", armID, out3)
	}
	if s.Pending != nil {
		t.Fatalf("the cancel must clear Pending; pending=%v", s.Pending)
	}
}

func TestSwingR31RestSurvivesANonThroughClose(t *testing.T) {
	s, entryBars, armID, cfg := swingR31Entry(t)
	_ = armID

	// A bar that neither fills nor closes through leaves the order resting.
	rest := append(entryBars, mk5m(t, 15, 5, 10, 10158, 10160, 10156, 10158))
	out := SwingTick(s, rest, cfg, rest[len(rest)-1].OpenTime+60_000)
	if s.Pending == nil {
		t.Fatal("the resting order must remain Pending after a non-through close")
	}
	for _, in := range out {
		if in.Action == CancelArm {
			t.Fatalf("no cancel for a non-through close; got %+v", out)
		}
	}
}
