package trader

import (
	"testing"
	"time"

	"vl/store"
	ntwire "vl/provider/ninjatrader"
)

// TestLiveSinkKicksFireBeforePictureEval — FIX-LIVE-SINK-KICKS. The one sink
// worker (pictureHtfLiveBars) used to run the full Picture evaluation BEFORE
// the mentor/armed kicks, so a slow Picture (a pending setup re-evaluated per
// frame + the broken noteSilent WARN dedupe) held the kicks behind it. The fix
// fires the kicks first. This pin blocks the Picture half on the evaluator's own
// mutex (~the same stall a slow Picture evaluation causes) and asserts the
// mentor kick (mentorFinalArrival) still lands immediately, per FINAL frame.
// Named RED: restore the old order (NotifyLiveBars first) → the kick waits for
// the blocked evaluation and the poll times out.
func TestLiveSinkKicksFireBeforePictureEval(t *testing.T) {
	at := &AutoTrader{
		id:       "t-sink-kicks",
		exchange: "ninjatrader",
		config: AutoTraderConfig{
			NinjaTraderSymbol: "MNQ",
			StrategyConfig: &store.StrategyConfig{
				RiskControl: store.RiskControlConfig{MentorMode: true},
				DayPlan:     &store.DayPlanConfig{PictureHtf: &store.PictureHtfConfig{Enabled: true}},
			},
		},
	}
	ev := at.pictureHtfEvaluator()
	if ev == nil {
		t.Fatal("the Picture evaluator must build for an enabled ninjatrader trader")
	}
	ev.mu.Lock() // hold the evaluator so OnBars blocks — the "slow Picture" stand-in

	at.registerPictureHtf()
	t.Cleanup(func() { at.unregisterPictureHtf() })

	now := time.Now()
	bar := ntwire.Bar{T: now.Add(-time.Minute).UnixMilli(), O: 100, H: 101, L: 99, C: 100, Final: true, EmittedAt: now.UnixMilli()}

	done := make(chan struct{})
	go func() {
		pictureHtfLiveBars("MNQ 12-26", "5m", "MNQ 12-26", []ntwire.Bar{bar}, now)
		close(done)
	}()

	// The mentor kick must fire while the Picture evaluation is still blocked.
	deadline := time.Now().Add(250 * time.Millisecond)
	for at.mentorFinalArrival.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if at.mentorFinalArrival.Load() == 0 {
		ev.mu.Unlock()
		<-done
		t.Fatal("the mentor kick must fire before the blocked Picture evaluation (old order delays it)")
	}

	// Release the evaluator; the Picture half then returns.
	ev.mu.Unlock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pictureHtfLiveBars did not return after the evaluator unlock")
	}
}
