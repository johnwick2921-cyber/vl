package pep

import "testing"

// The box is a fact of the day: the card and the chart draw it after the day's
// trade is taken, while a position is open, and after the flat time. Before this
// pin Evaluate returned those three refusals before it measured the box, so the
// production card printed "not drawn yet" under "trades 1/1" (re-review lens 6, P2).
//
// MUTANT: remove the diagBox() call from the three early refusals in Evaluate
// -> every row below is RED (Box.Drawn false, High/Low 0).
func TestBoxStaysMeasuredOnTheEarlyRefusals(t *testing.T) {
	bars := aDay(t, dayCST, map[string]ohlc{"08:37": {25030, 25041, 25029, 25040}})
	now := nowAfter(t, dayCST, "08:37")
	for name, tc := range map[string]struct {
		now  int64
		st   State
		code string
	}{
		"position open": {now, State{Day: dayCST, Trades: 1, PositionOpen: true}, CodePositionOpen},
		"trade taken":   {now, State{Day: dayCST, Trades: 1}, CodeMaxTrades},
		"after flat":    {ctms(t, dayCST, "15:30"), State{Day: dayCST}, CodeAfterFlat},
	} {
		r := Evaluate(bars, tc.now, cfgA(), tc.st)
		wantRefusal(t, r, tc.code, SevWait)
		if !r.Box.Drawn || r.Box.High != 25040 || r.Box.Low != 25000 || r.Box.Size != 40 {
			t.Errorf("%s: box %+v, want drawn 25000..25040 size 40", name, r.Box)
		}
	}
	// the diagnostic never invents a box: before the box is complete, or with a
	// minute missing, nothing is drawn
	early := Evaluate(bars, ctms(t, dayCST, "08:33"), cfgA(), State{Day: dayCST, Trades: 1})
	if early.Box.Drawn {
		t.Errorf("a box that is not complete yet must not be drawn: %+v", early.Box)
	}
	hole := aDay(t, dayCST, nil, "08:32")
	r := Evaluate(hole, now, cfgA(), State{Day: dayCST, Trades: 1})
	if r.Box.Drawn {
		t.Errorf("a box with a missing minute must not be drawn: %+v", r.Box)
	}
	// and it is not a second path to an order
	if r := Evaluate(bars, now, cfgA(), State{Day: dayCST, Trades: 1, PositionOpen: true}); len(r.Intents) != 0 {
		t.Fatalf("the diagnostic produced an intent: %+v", r.Intents)
	}
}
