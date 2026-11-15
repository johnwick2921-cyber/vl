package trader

import (
	"testing"
	"time"

	"vl/calendar"
	"vl/store"
)

// Review r2 (P2): with the entry hold OFF and the news flatten ON (both
// defaults), a placement inside the print window would be armed and then swept
// by mentorNewsCancelFlattenAt on the next bar — arm-then-cancel churn, and a
// fresh fill flattened at a loss. The gate must refuse what the flatten would
// sweep: same calendar, same window. No calendar → no hold (the flatten cannot
// act on a guess either). Flatten OFF → nothing held.
func TestOctReview2NewsGateRefusesWhatTheFlattenSweeps(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	t.Cleanup(at.Stop)
	prev := getMentorNowSource()
	t.Cleanup(func() { setMentorNowSource(prev) })

	// No calendar seam installed: no print day known → no hold.
	setMentorNowSource(func() time.Time { return newsClock(7, 22) })
	mentorDayEventsForTest = func() ([]calendar.Event, bool) { return nil, false }
	t.Cleanup(func() { mentorDayEventsForTest = nil })
	if hold, _ := at.mentorNewsGate(); hold {
		t.Fatal("hold OFF, no calendar: the gate must not hold (the flatten cannot act either)")
	}

	newsPrintEvents(t)
	if hold, _ := at.mentorNewsGate(); !hold {
		t.Fatal("hold OFF + flatten ON on a print day at 07:22: the gate must refuse the entry the flatten would sweep")
	}
	setMentorNowSource(func() time.Time { return newsClock(7, 40) })
	if hold, _ := at.mentorNewsGate(); hold {
		t.Fatal("07:40 is after the print window: no hold")
	}

	setMentorNowSource(func() time.Time { return newsClock(7, 22) })
	off := false
	at.mentorRiskControl().MentorTuning = &store.MentorTuning{MentorNewsFlattenEnabled: &off}
	if hold, _ := at.mentorNewsGate(); hold {
		t.Fatal("hold OFF + flatten OFF: nothing is held")
	}
}
