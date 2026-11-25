package trader

// GATE #563 adversarial probes (reviewer-owned, NOT part of the PR). Every
// probe drives a PRODUCTION call site: mentorArmIntent, placeOneStopEntry,
// registerMentorLivePos + mentorExitDrivePos, mentorRebuildLivePosAtBoot.

import (
	"testing"
	"time"

	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

type g563Placer struct {
	calls            int
	qty              float64
	leg1Qty, leg2Qty int
	leg1TP, leg2TP   float64
}

func (f *g563Placer) rec(q float64, l1 int, t1 float64, l2 int, t2 float64, beforeSend []func(string) error) (string, error) {
	f.calls++
	f.qty, f.leg1Qty, f.leg1TP, f.leg2Qty, f.leg2TP = q, l1, t1, l2, t2
	for _, r := range beforeSend {
		if err := r("sid-g563"); err != nil {
			return "", err
		}
	}
	return "sid-g563", nil
}
func (f *g563Placer) PlaceStopEntry(symbol, side string, quantity float64, stopPx, sl, tp float64, leg1Qty int, leg1TP float64, leg2Qty int, leg2TP float64, beforeSend ...func(string) error) (string, error) {
	return f.rec(quantity, leg1Qty, leg1TP, leg2Qty, leg2TP, beforeSend)
}
func (f *g563Placer) PlaceStopEntryWithLimit(symbol, side string, quantity float64, stopPx, sl, tp float64, leg1Qty int, leg1TP float64, leg2Qty int, leg2TP float64, beforeSend ...func(string) error) (string, error) {
	return f.rec(quantity, leg1Qty, leg1TP, leg2Qty, leg2TP, beforeSend)
}

// G1 — the split table at the production writer (mentorArmIntent).
func TestG563ArmIntentSplitTable(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	ledger := st.ArmedOrders()
	cases := []struct {
		arm      string
		n        int
		mode     string
		forkTP   float64
		partial  float64
		wantLeg2 int // 0 = nil/absent
		wantLeg1 int
	}{
		{"g5", 5, "B", 0, 107, 1, 3},
		{"g3", 3, "B", 0, 107, 0, 2},
		{"g4", 4, "B", 0, 107, 1, 2},
		{"g6", 6, "B", 0, 107, 1, 3},
		{"g5c", 5, "C", 110, 107, 0, 3},
		{"g5none", 5, "B", 0, 0, 0, 3},
	}
	for _, c := range cases {
		in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: c.arm, Setup: "ISB",
			Side: mentor.SideLong, Price: 100, Stop: 95, Target: 110, StopPts: 5, TargetPts: 10,
			RunnerPartialTP: c.partial, ExpiryMs: time.Now().UnixMilli() + 60_000}
		at.mentorArmIntent(in, mentorSizeChoice{Contracts: c.n, Tier: "base", Why: "test"}, 1000, 1100, c.mode, c.forkTP)
		var rows []store.ArmedOrderDB
		if err := ledger.DB().Where("scenario LIKE ?", "%"+c.arm+"-%").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		var r *store.ArmedOrderDB
		for i := range rows {
			if rows[i].Contracts != nil && *rows[i].Contracts == c.n {
				r = &rows[i]
			}
		}
		if r == nil {
			t.Fatalf("%s: no row (rows=%d)", c.arm, len(rows))
		}
		got := 0
		if r.Leg2Qty != nil {
			got = *r.Leg2Qty
		}
		if got != c.wantLeg2 {
			t.Errorf("%s n=%d mode=%s: leg2_qty=%d want %d", c.arm, c.n, c.mode, got, c.wantLeg2)
		}
		if c.wantLeg2 > 0 && r.Leg2TP != c.partial {
			t.Errorf("%s: leg2_tp=%.2f want %.2f", c.arm, r.Leg2TP, c.partial)
		}
		if r.Leg1Qty == nil || *r.Leg1Qty != c.wantLeg1 {
			t.Errorf("%s: leg1_qty=%v want %d", c.arm, r.Leg1Qty, c.wantLeg1)
		}
	}
}

// G2 — the row's leg 2 reaches the placer (placeOneStopEntry pass-through).
func TestG563PlaceOneStopEntryCarriesLeg2(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "1")
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.id = "g563-pass"
	r := store.ArmedOrderDB{
		ID: 8, TraderID: at.id, PlanID: "mentor", Version: 1,
		Session: "MENTOR", Scenario: "isb-1", Side: "long",
		EntryPx: 29650, StopPx: 29640, TargetPx: 29680,
		Origin: store.ArmOriginMentor, ExpiryMs: 90_000, Contracts: store.IntPtr(5),
		Leg1Qty: store.IntPtr(3), Leg1TP: 29660, Leg2Qty: store.IntPtr(1), Leg2TP: 29670,
	}
	d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
	if d.Action != stopEntryPlace {
		t.Fatalf("fixture must be placeable, got %q", d.Action)
	}
	pl := &g563Placer{}
	if got := at.placeOneStopEntry(pl, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != stopPlaceCommitted {
		t.Fatalf("outcome = %d, want committed", got)
	}
	if pl.calls != 1 || pl.leg1Qty != 3 || pl.leg2Qty != 1 || pl.leg2TP != 29670 {
		t.Fatalf("placer got leg1=%d leg2=%d@%.2f (calls %d), want leg1=3 leg2=1@29670", pl.leg1Qty, pl.leg2Qty, pl.leg2TP, pl.calls)
	}
}

// g563Register registers a 5-lot 3/1/1 mentor position through the production
// full-fill registration (registerMentorLivePos) with the SentSplit the frame
// carried. entry 10, stop 8 (R=2), leg1 TP 12, runner partial line 14, target 16.
func g563Register(t *testing.T, at *AutoTrader, sid string) *mentorLivePos {
	t.Helper()
	old := mentorSentSplit
	mentorSentSplit = func(_ *AutoTrader, s string) (nttrader.SentSplit, bool) {
		if s == sid {
			return nttrader.SentSplit{Leg1Qty: 3, Leg1TP: 12, Leg2Qty: 1, Leg2TP: 14}, true
		}
		return nttrader.SentSplit{}, false
	}
	t.Cleanup(func() { mentorSentSplit = old })
	row := store.ArmedOrderDB{ID: 7, SignalID: sid, Side: "long", StopPx: 8, TargetPx: 16, FillQuantity: 5, Condition: "PHL"}
	at.registerMentorLivePos(row, ntwire.OrderUpdatePayload{State: "filled", SignalID: sid, Account: "Sim101", FillPrice: 10, Quantity: 5})
	lp := at.mentorLivePos[sid]
	if lp == nil {
		t.Fatal("not registered")
	}
	if lp.Legs[0].Qty != 3 || lp.Legs[0].Wire != 1 || lp.Legs[1].Qty != 1 || lp.Legs[1].Wire != 2 || lp.Legs[1].Final ||
		lp.Legs[2].Qty != 1 || lp.Legs[2].Wire != 3 || !lp.Legs[2].Final {
		t.Fatalf("3-pair registration wrong: %+v", lp.Legs)
	}
	return lp
}

// G3 — DEFECT PROBE (candle mode = the live default): once leg 1's +1R TP is
// CONFIRMED, the runner goes to break-even [R3; D2.4 p1 @16:17]. The runner is
// now TWO wire legs (leg 2 = the line partial, leg 3 = the trailer). Both are
// runner contracts and both must leave the original stop. Expected legs [2 3].
func TestG563CandleBEMovesBothRunnerLegs(t *testing.T) {
	at, moves := newDriveAT(t)
	at.config.StrategyConfig.RiskControl.MentorTrailMode = "candle"
	at.mentorExitModes = map[string]string{"long": "B"}
	T := int64(1_000_000)
	oldNow := getMentorNowSource()
	setMentorNowSource(func() time.Time { return time.UnixMilli(T) })
	t.Cleanup(func() { setMentorNowSource(oldNow) })
	lp := g563Register(t, at, "sig-g3")
	at.mentorMarkLeg1Scaled(ntwire.PositionClosePayload{SignalID: "sig-g3", PositionSide: "long", ExitReason: "tp", Leg: 1})
	if !lp.Pos.Scaled {
		t.Fatal("leg 1 receipt must mark Scaled")
	}
	// a closed candle well past break-even (entry 10): close 13
	at.mentorExitDrivePos(nil, lp, 13, 13.5, 12.5, T+60_000)
	legs := capturedDriveLegs()
	has := map[int]bool{}
	for _, l := range legs {
		has[l] = true
	}
	t.Logf("moves=%v legs=%v leg2.Stop=%.2f leg3.Stop=%.2f", moves(), legs, lp.Legs[1].Stop, lp.Legs[2].Stop)
	if !has[3] {
		t.Fatalf("the trailer (wire 3) never went to BE: legs %v", legs)
	}
	if !has[2] {
		t.Fatalf("DEFECT: the runner partial (wire 2, 1 contract) was left at the ORIGINAL stop %.2f after the confirmed +1R partial — only legs %v moved to BE", lp.Legs[1].Stop, legs)
	}
}

// G3b — off mode: after the confirmed partial, the 1:1/trail loop skips every
// non-Final leg, so wire 2 never trails with the runner.
func TestG563OffModeTrailSkipsLeg2(t *testing.T) {
	at, _ := newDriveAT(t)
	at.mentorExitModes = map[string]string{"long": "B"}
	T := int64(1_000_000)
	oldNow := getMentorNowSource()
	setMentorNowSource(func() time.Time { return time.UnixMilli(T) })
	t.Cleanup(func() { setMentorNowSource(oldNow) })
	lp := g563Register(t, at, "sig-g3b")
	at.mentorExitDrivePos(nil, lp, 12.8, 13.2, 12, T-2) // BE at half distance → all legs
	before := capturedDriveLegs()
	at.mentorMarkLeg1Scaled(ntwire.PositionClosePayload{SignalID: "sig-g3b", PositionSide: "long", ExitReason: "tp", Leg: 1})
	at.mentorExitDrivePos(nil, lp, 14, 14.5, 13.5, T+1)
	at.mentorExitDrivePos(nil, lp, 15, 15.5, 14.6, T+60_001)
	legs := capturedDriveLegs()
	t.Logf("BE legs=%v all legs=%v leg2.Stop=%.2f leg3.Stop=%.2f", before, legs, lp.Legs[1].Stop, lp.Legs[2].Stop)
	after := legs[len(before):]
	for _, l := range after {
		if l == 2 {
			return
		}
	}
	t.Fatalf("OBSERVATION: after the partial only legs %v moved — wire 2 stays at %.2f while wire 3 trails to %.2f", after, lp.Legs[1].Stop, lp.Legs[2].Stop)
}

// G4 — DEFECT PROBE: restart rebuild with the row carrying leg2_qty but a
// 2-pair book (old AddOn, or wireLeg2TP dropped leg 2 at the send). The row's
// leg2_qty was never sent, yet the rebuild reads leg 3 as live, finds no -sl3,
// and refuses the whole position (P0 alert, no exit management).
func TestG563RebuildTwoPairBookWithLeg2Row(t *testing.T) {
	at, st := f2Trader(t)
	sig := "g563-0000-0000-0000-000000000004"
	arm := f2Arm(sig, "PHL", 5, 3, 31020, 31000, 31060, nil)
	arm.Leg1TP = 31040
	arm.Leg2Qty = store.IntPtr(1)
	arm.Leg2TP = 31050
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(f2Pos(sig, 5, 5, 31020)); err != nil {
		t.Fatal(err)
	}
	f2PinBook(t, []ntwire.NT8Order{
		f2StopOrder(sig+"-sl", 31000, 3),
		f2TPOrder(sig+"-tp", 31040),
		f2StopOrder(sig+"-sl2", 31000, 2),
		f2TPOrder(sig+"-tp2", 31060),
	}, true, 0)
	rebuilt, refused, ran := at.mentorRebuildLivePosAtBoot(time.Now())
	if !ran || rebuilt != 1 || refused != 0 {
		t.Fatalf("DEFECT: rebuilt=%d refused=%d ran=%v — a 2-pair position whose row carries an UNSENT leg2_qty is refused at restart (want 1/0/true)", rebuilt, refused, ran)
	}
	lp := at.mentorLivePos[sig]
	if lp == nil || lp.Legs[1].Wire != 2 || !lp.Legs[1].Final || lp.Legs[1].Qty != 2 {
		t.Fatalf("want the 2-pair view (runner wire 2 ×2), got %+v", lp)
	}
}

// G5 — restart rebuild of a 3-pair position after leg 1's partial filled
// (book: -sl2/-tp2 ×1, -sl3/-tp3 ×1; residual 2). Leg 1 is gone, so Scaled.
func TestG563RebuildThreePairAfterLeg1(t *testing.T) {
	at, st := f2Trader(t)
	sig := "g563-0000-0000-0000-000000000005"
	arm := f2Arm(sig, "PHL", 5, 3, 31020, 31000, 31060, nil)
	arm.Leg1TP = 31040
	arm.Leg2Qty = store.IntPtr(1)
	arm.Leg2TP = 31050
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(f2Pos(sig, 5, 2, 31020)); err != nil {
		t.Fatal(err)
	}
	f2PinBook(t, []ntwire.NT8Order{
		f2StopOrder(sig+"-sl2", 31000, 1),
		f2TPOrder(sig+"-tp2", 31050),
		f2StopOrder(sig+"-sl3", 31000, 1),
		f2TPOrder(sig+"-tp3", 31060),
	}, true, 0)
	rebuilt, refused, ran := at.mentorRebuildLivePosAtBoot(time.Now())
	if !ran || rebuilt != 1 || refused != 0 {
		t.Fatalf("rebuilt=%d refused=%d ran=%v want 1/0/true", rebuilt, refused, ran)
	}
	lp := at.mentorLivePos[sig]
	t.Logf("legs=%+v scaled=%v", lp.Legs, lp.Pos.Scaled)
	if lp.Legs[2].Wire != 3 || !lp.Legs[2].Final || lp.Legs[2].Qty != 1 {
		t.Fatalf("want the runner on wire 3 ×1, got %+v", lp.Legs)
	}
	if !lp.Pos.Scaled {
		t.Fatalf("DEFECT: leg 1 is gone (residual 2 = leg 2 + leg 3) but the rebuild reads Scaled=false — BE/trail wait for the I7 fallback")
	}
}
