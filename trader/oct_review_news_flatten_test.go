package trader

import (
	"testing"

	"vl/store"
)

// Review fix (fidelity P2): turning the 07:20-07:35 entry HOLD off (October has no news rule)
// must not also turn off the destructive-safety half (cancel + flatten at print -10m).
// "Safety stays" (owner rule): the flatten is its own knob mentor_news_flatten_enabled, nil -> ON;
// a stored false is the explicit owner acceptance of holding through a print.
func TestOctReviewNewsFlattenSurvivesHoldOff(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	rec := &newsFlattenRecorder{MockTrader: &MockTrader{}}
	at.trader = rec
	t.Cleanup(at.Stop)
	newsPrintEvents(t)
	if at.mentorNewsHoldEnabled() {
		t.Fatal("entry hold must stay OFF by default (October has no news rule)")
	}
	p := &store.TraderPosition{
		TraderID: at.id, Symbol: "MNQ", Side: "LONG", Account: "Sim101",
		EntryPrice: 30000, EntryTime: newsClock(7, 10).UnixMilli(),
		EntryQuantity: 1, Quantity: 1, Status: "OPEN", CitedScenarioID: "isb-1",
	}
	if err := st.Position().Create(p); err != nil {
		t.Fatal(err)
	}
	if !at.mentorNewsFlattenEnabled() {
		t.Fatal("news flatten default must be ON (safety stays)")
	}
	at.mentorNewsCancelFlattenAt(newsClock(7, 25))
	if rec.closedLong != 1 {
		t.Fatalf("intraday long not flattened at print-10m with the hold OFF: closedLong=%d", rec.closedLong)
	}

	off := false
	at.mentorRiskControl().MentorTuning = &store.MentorTuning{MentorNewsFlattenEnabled: &off}
	if at.mentorNewsFlattenEnabled() {
		t.Fatal("stored false must turn the flatten off")
	}
}
