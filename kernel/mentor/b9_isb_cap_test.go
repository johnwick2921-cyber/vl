package mentor

import (
	"math"
	"testing"
	"time"
)

// (c) B9 [D5.1 p1 @16:24, @19:11–20:07]: an ISB on a SPENT day is CAPPED at
// the 15-pt target and emits exactly the cap.
//
// Why TestISBSpentDayTargetCap (isb_po_test.go) could never go RED: its
// fixture's natural ISB target is the EMA 34 at 114.16 — 10.56 pts from the
// 103.60 entry, INSIDE the 15-pt cap — so the cap was a no-op there and the
// assertion held with the cap removed (and with the cap never applied to the
// emit at all). This pin seeds ONE key level 21.4 pts beyond the entry and
// switches the EMAs off, so the natural target is well past the cap and the
// VALUE is asserted.
func TestB9ISBOnSpentDayEmitsExactlyTheCap(t *testing.T) {
	run := func(verdict DayVerdict) Intent {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.EMAPeriod34 = 0
		cfg.EMAPeriod9 = 0
		cfg.EMALocationTFMinutes = 0 // no EMA34 location line: the seeded level is the only target
		cfg.RoomMultiple = 0        // this pin is about the cap value, not the room rule
		cfg.DayGateSpentPts = 300
		cfg.DayGateTargetCapPts = b9CapPts
		e := newISBEval(cfg)
		bars := isbFixture()
		now := bars[len(bars)-1].CloseTime + 1
		e.seeded = true
		e.State.Seed1mWatermark = math.MaxInt64
		e.State.Seed1HWatermark = math.MaxInt64
		e.State.SeedLevels = []Level{{Key: "far", Kind: KindKeyLevel, Price: 125}}
		e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: verdict}
		for _, in := range e.Tick(bars, now) {
			if in.Action == PlaceStopLimitEntry {
				return in
			}
		}
		t.Fatalf("verdict %v: the long ISB did not emit: %v", verdict, e.State.Refusals)
		return Intent{}
	}

	natural := run(DayTrade)
	if b9Dist(natural) < b9CapPts+5 {
		t.Fatalf("trade-day ISB target is %.2f pts out — need >= %.0f beyond the cap for the pin to discriminate",
			b9Dist(natural), b9CapPts+5)
	}
	spent := run(DaySpent)
	if d := b9Dist(spent); abs(d-b9CapPts) > 1e-9 {
		t.Fatalf("spent-day ISB target %.2f is %.2f pts from entry %.2f — must be exactly the %.0f cap (trade-day natural %.2f)",
			spent.Target, d, spent.Price, b9CapPts, b9Dist(natural))
	}
	if spent.Price != natural.Price || spent.Stop != natural.Stop {
		t.Fatalf("the cap moved the entry or stop: trade %+v spent %+v", natural, spent)
	}
	if !spent.SpentDay {
		t.Fatalf("spent-day ISB not stamped SpentDay: %+v", spent)
	}
}
