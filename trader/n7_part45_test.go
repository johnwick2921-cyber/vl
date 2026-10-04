package trader

import (
	"encoding/json"
	"testing"
	"time"

	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// N7 part 4 (CTO, release #4) — a mentor CancelArm on a row that was NEVER
// SENT (armed, no signal id) ends it cancelled at once. The old path wrote
// cancel_pending with signal_id "", which no regime can ever settle (it WARNed
// every pass forever). No cancel frame may go out: there is nothing to cancel.
func TestMentorCancelArmUnplacedRowEndsCancelled(t *testing.T) {
	at, _, ledger, frames := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "lvl-5-0",
		Side: "long", EntryPx: 29600, StopPx: 29595, TargetPx: 29610, Kind: "stop_entry", Condition: "PHL",
		State: store.StateArmed, Origin: store.ArmOriginMentor, ExpiryMs: time.Now().Add(time.Hour).UnixMilli()}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	mentorRegisterLiveArm("lvl-5", row.ID, "long", 29600)
	resetMentorCounters()
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.CancelArm, ArmID: "lvl-5", Reason: "close-through"}, mentorTierInputs{}, 1000, 1100)
	if sawMentorFrame(t, frames, ntwire.FrameCancelOrder, 300*time.Millisecond) {
		t.Fatal("an unplaced row has nothing at the broker — no cancel_order may be sent")
	}
	var got store.ArmedOrderDB
	if err := ledger.DB().First(&got, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.State != store.StateCancelled {
		t.Fatalf("an unplaced row must end cancelled directly, got %q", got.State)
	}
	if c := MentorCountSnapshot()["cancel_unplaced"]; c != 1 {
		t.Fatalf("cancel_unplaced = %d, want 1", c)
	}
}

// N7 part 5 (CTO, release #4) — at the production call site
// confirmPendingCancels: a FILLED entry in the fresh book is not a cancel (the
// row stays cancel_pending and the broker-side pending marker is kept); mere
// ABSENCE settles the row but keeps the marker; a positive Cancelled clears it.
func TestConfirmPendingCancelsFilledIsNotACancelAndForgetNeedsAPositiveCancel(t *testing.T) {
	cases := []struct {
		name        string
		book        []ntwire.NT8Order
		wantState   string
		wantPending bool
	}{
		{"filled entry in the book", []ntwire.NT8Order{{Name: "SIG", State: "Filled"}}, store.StateCancelPending, true},
		{"absent from the book", []ntwire.NT8Order{}, store.StateCancelled, true},
		{"cancelled in the book", []ntwire.NT8Order{{Name: "SIG", State: "Cancelled"}}, store.StateCancelled, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at, st, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
			nt := at.armedTrader()
			sid, err := nt.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200)
			if err != nil {
				t.Fatalf("seed entry: %v", err)
			}
			if !nt.HasPendingEntry(sid) {
				t.Fatalf("precondition: the sent entry must be pending")
			}
			now := time.Now()
			row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "isb-9-0",
				Side: "long", EntryPx: 29600, StopPx: 29595, TargetPx: 29610, Kind: "stop_entry", Condition: "ISB",
				State: store.StateWorking, SignalID: sid, Origin: store.ArmOriginMentor}
			if err := ledger.DB().Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			if err := ledger.RequestCancel(row.ID, "test cancel", now.Add(-10*time.Second).UnixMilli()); err != nil {
				t.Fatal(err)
			}
			book := make([]ntwire.NT8Order, len(c.book))
			for i, o := range c.book {
				o.Name = sid
				book[i] = o
			}
			js, _ := json.Marshal(book)
			if err := st.NT8OrderSnapshots().Insert(&store.NT8OrderSnapshot{Account: "Sim101", OrdersJSON: string(js), ReceivedMs: now.UnixMilli()}); err != nil {
				t.Fatal(err)
			}
			at.confirmPendingCancels(ledger, nil, now)
			var got store.ArmedOrderDB
			if err := ledger.DB().First(&got, row.ID).Error; err != nil {
				t.Fatal(err)
			}
			if got.State != c.wantState {
				t.Fatalf("row state = %q, want %q", got.State, c.wantState)
			}
			if p := nt.HasPendingEntry(sid); p != c.wantPending {
				t.Fatalf("pending marker kept = %v, want %v", p, c.wantPending)
			}
		})
	}
}
