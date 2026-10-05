package trader

import (
	"testing"
	"time"

	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── REVIEW-353 split-at-entry frame pins ───────────────────────────────────
// ONE entry of n → ONE row → ONE position; the ONE entry frame carries
// leg1_qty + leg1_tp and the AddOn places TWO OCO pairs under the one signal
// id. These pins prove the Go side sends exactly that.

// TestMentorLeg1ForFrame — the split math feeding the frame (pure). 5-lot →
// (3, entry±R); n=2 → (1, entry±R); n=1 / swing → (0,0) single bracket; mode
// C → leg1TP ≥ 2R; spent day caps the RUNNER (leg1 stays ceil(n/2)).
func TestMentorLeg1ForFrame(t *testing.T) {
	long := mentor.Intent{Side: mentor.SideLong, Price: 100, Stop: 95, Target: 110, StopPts: 5, TargetPts: 10}
	short := mentor.Intent{Side: mentor.SideShort, Price: 100, Stop: 105, Target: 90, StopPts: 5, TargetPts: 10}
	cases := []struct {
		name    string
		in      mentor.Intent
		n       int
		mode    string
		forkTP  float64
		wantQty int
		wantTPF float64
	}{
		{"5-lot B long", long, 5, "B", 0, 3, 105},
		{"5-lot B short", short, 5, "B", 0, 3, 95},
		{"2-lot B", long, 2, "B", 0, 1, 105},
		{"1-lot single", long, 1, "B", 0, 0, 0},
		{"swing single", long, 5, "swing", 0, 0, 0},
		{"5-lot C ≥2R", long, 5, "C", 110, 3, 110},
		{"5-lot spent day (runner capped)", mentor.Intent{Side: mentor.SideLong, Price: 100, Stop: 95, Target: 110, StopPts: 5, TargetPts: 10, SpentDay: true}, 5, "B", 0, 3, 105},
	}
	for _, c := range cases {
		qty, tp := mentorLeg1ForFrame(c.in, c.n, c.mode, c.forkTP)
		if qty != c.wantQty || tp != c.wantTPF {
			t.Errorf("%s: mentorLeg1ForFrame = (%d, %.2f), want (%d, %.2f)", c.name, qty, tp, c.wantQty, c.wantTPF)
		}
	}
}

// TestMentorSplitOneRowOneFrame — the PRODUCTION writer (mentorArmIntent)
// authors ONE row carrying the full split: Contracts=n, Leg1Qty=ceil(n/2),
// Leg1TP=entry±R, TargetPx=the runner target. Mutant: leg1_qty not stamped →
// the row's Leg1Qty is nil → RED.
func TestMentorSplitOneRowOneFrame(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	ledger := st.ArmedOrders()

	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-split", Setup: "ISB",
		Side: mentor.SideLong, Price: 100, Stop: 95, Target: 110, StopPts: 5, TargetPts: 10,
		ExpiryMs: time.Now().UnixMilli() + 60_000}
	at.mentorArmIntent(in, mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}, 1000, 1100, "B", 0)

	var rows []store.ArmedOrderDB
	if err := ledger.DB().Where("scenario LIKE ?", "isb-split-%").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("ONE entry must author ONE row, got %d", len(rows))
	}
	r := rows[0]
	if r.Contracts == nil || *r.Contracts != 5 {
		t.Fatalf("the one row must carry Contracts=5, got %v", r.Contracts)
	}
	if r.Leg1Qty == nil || *r.Leg1Qty != 3 {
		t.Fatalf("the one row must carry leg1_qty=3, got %v", r.Leg1Qty)
	}
	if r.Leg1TP != 105 {
		t.Fatalf("leg1_tp must be entry+R = 105, got %.2f", r.Leg1TP)
	}
	if r.TargetPx != 110 {
		t.Fatalf("the runner target must be the trade target 110, got %.2f", r.TargetPx)
	}
}

// TestRegisterMentorLivePosRegistersBothLegs — P2-4: a filled arm row that
// carries a split (Leg1Qty set) registers Legs[0] = leg 1 (its own TP) and
// Legs[1] = the runner, so the exit-drive sees the two legs separately and a
// leg-1 exit never flattens the runner's drive. Mutant: register one leg → RED.
func TestRegisterMentorLivePosRegistersBothLegs(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	row := store.ArmedOrderDB{
		SignalID:  "split-live",
		Side:      "long",
		StopPx:    95,
		TargetPx:  110,
		Condition: "ISB",
		Leg1Qty:   store.IntPtr(3),
		Leg1TP:    105,
	}
	u := ntwire.OrderUpdatePayload{Quantity: 5, FillPrice: 100}
	at.registerMentorLivePos(row, u)
	list := at.mentorLivePosList()
	if len(list) != 1 {
		t.Fatalf("want 1 live pos, got %d", len(list))
	}
	lp := list[0]
	if lp.Pos.Leg1 != 3 || lp.Pos.Leg2 != 2 || lp.Pos.Leg1TP != 105 {
		t.Fatalf("pos legs = (%d, %d, tp %.2f), want (3, 2, 105.00)", lp.Pos.Leg1, lp.Pos.Leg2, lp.Pos.Leg1TP)
	}
	if lp.Legs[0].Qty != 3 || lp.Legs[0].TP != 105 || lp.Legs[0].Final {
		t.Fatalf("Legs[0] = %+v, want leg 1 (qty 3, tp 105, not final)", lp.Legs[0])
	}
	if lp.Legs[1].Qty != 2 || lp.Legs[1].TP != 110 || !lp.Legs[1].Final {
		t.Fatalf("Legs[1] = %+v, want runner (qty 2, tp 110, final)", lp.Legs[1])
	}
}
