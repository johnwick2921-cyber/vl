package mentor

import "testing"

// TestISBTFF2CrossingNeverLiftsABoxGate — isbTF-F2: the ONE helper every ISB
// and reverse-ISB box gate (5m, 15m, 30m, the CE-1 15m rule) reads. D3.4-R43
// [D3.4 p2 @17:07–17:49] escalates only the TRIGGER; the box gates stand
// [OctD4.3-R70 @ 68:24, R61 @ 59:56, R59 @ 58:00]. Fed crossing buckets (the
// m60 crossing tape), it lifts nothing on any timeframe, in any 5m box mode.
// Mutant (the pre-F2 seam): the 15m/30m gates and the 5m gate outside merge
// are lifted again → RED.
func TestISBTFF2CrossingNeverLiftsABoxGate(t *testing.T) {
	bars := m60p1Crossing()
	cb := closedBuckets(bars, BarCloseInstant(bars[len(bars)-1]), DefaultConfig())
	if !crossingISBs(cb, 3) {
		t.Fatal("precondition: the buckets must hold crossing ISBs")
	}
	for _, mode := range []string{FiveMinBoxMerge, FiveMinBoxNewest, FiveMinBoxFirst} {
		e := New(DefaultConfig())
		e.Cfg.FiveMinBoxSource = mode
		for _, tf := range []int{5, 15, 30} {
			if e.boxGateLiftedByCrossing(cb, tf) {
				t.Errorf("mode %s: crossing ISBs lifted the %dm box gate — escalation changes the trigger only (D3.4-R43)", mode, tf)
			}
		}
	}
	prev := isbBoxGateLiftedByCrossingSeam
	isbBoxGateLiftedByCrossingSeam = true
	defer func() { isbBoxGateLiftedByCrossingSeam = prev }()
	e := New(DefaultConfig())
	e.Cfg.FiveMinBoxSource = FiveMinBoxNewest
	if !e.boxGateLiftedByCrossing(cb, 15) || !e.boxGateLiftedByCrossing(cb, 5) {
		t.Fatal("pre-F2 seam: the crossing read lifted the 15m gate and (outside merge) the 5m gate")
	}
	e.Cfg.FiveMinBoxSource = FiveMinBoxMerge
	if e.boxGateLiftedByCrossing(cb, 5) {
		t.Fatal("pre-F2 seam: mode merge never lifted the 5m gate (ISB5-F2)")
	}
}
