package mentor

import (
	"math"
	"testing"
	"time"
)

// MENTOR-NY item 1 — on a SPENT day the room rule is measured to the LEVEL,
// never to the 15-pt capped take-profit. The course:
//
//	"Bây giờ dù target của em là 15 điểm … Từ chỗ em xuống target nó phải từ
//	 30 điểm đổ lên" [D5.3 p1 @09:32–09:50]
//
// The take-profit stays capped (CapTargetForDay) and the 1:1 floor still reads
// the capped take-profit. Before the fix every SPENT-day ISB / reverse ISB / box
// measured the room on the cap, so a setup needed stop ≤ cap/2 (7.5 at the
// default 15) — the course's own worked figure (target 15, room 30) was
// refused.
//
// Both pins run Evaluator.Tick (the production call site). Each one measures
// the natural trade-day geometry first — risk R and the level distance L — and
// fails if the fixture cannot discriminate (L must clear 2R). The spent cap is
// then set to 1.5R: ≥ R (the 1:1 floor holds) and < 2R (the room measured on
// the CAP fails), so only the room-to-the-LEVEL reading lets it emit.

// TestMentorNYSpentDayISBRoomIsToTheLevel — the normal ISB at Tick.
// MUTANT: eval.go ISB room back on capped.Target → RED (refused "room").
func TestMentorNYSpentDayISBRoomIsToTheLevel(t *testing.T) {
	run := func(verdict DayVerdict, capPts float64) (Intent, map[string]int) {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.EMAPeriod34 = 0
		cfg.EMAPeriod9 = 0
		cfg.EMALocationTFMinutes = 0 // the seeded level is the only target
		cfg.DayGateSpentPts = 300
		cfg.DayGateTargetCapPts = capPts
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
				return in, e.State.Refusals
			}
		}
		return Intent{}, e.State.Refusals
	}

	natural, led := run(DayTrade, 15)
	if natural.Price == 0 {
		t.Fatalf("trade-day control: the long ISB did not emit: %v", led)
	}
	risk := abs(natural.Price - natural.Stop)
	level := abs(natural.Target - natural.Price)
	if risk <= 0 || level < 2*risk {
		t.Fatalf("fixture cannot discriminate: risk %.2f, level %.2f (need level ≥ 2×risk)", risk, level)
	}
	capPts := 1.5 * risk // ≥ R (floor holds), < 2R (room on the cap fails)

	spent, led := run(DaySpent, capPts)
	if spent.Price == 0 {
		t.Fatalf("SPENT-day ISB with room %.2f ≥ 2×%.2f to the LEVEL was refused (the room read the %.2f-pt capped take-profit): ledger %v",
			level, risk, capPts, led)
	}
	if d := abs(spent.Target - spent.Price); abs(d-capPts) > 1e-9 {
		t.Fatalf("the take-profit must still be capped at %.2f, got %.2f pts (%+v)", capPts, d, spent)
	}
	if spent.Price != natural.Price || spent.Stop != natural.Stop {
		t.Fatalf("the cap moved entry/stop: trade %+v spent %+v", natural, spent)
	}
}

// TestMentorNYSpentDayBoxRoomIsToTheLevel — the box trade at Tick, with the
// LIVE room multiple (2, not the 0.05 the B9 cap pins run with).
// MUTANT: eval.go box room back on capped.Target → RED (refused "room").
func TestMentorNYSpentDayBoxRoomIsToTheLevel(t *testing.T) {
	run := func(verdict DayVerdict, capPts float64) ([]Intent, map[string]int) {
		e, bars, now := b9BoxFixture(HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}, verdict)
		e.Cfg.RoomMultiple = DefaultConfig().RoomMultiple
		e.Cfg.DayGateTargetCapPts = capPts
		return b9BoxEntries(e.Tick(bars, now)), e.State.Refusals
	}
	natural, led := run(DayTrade, 15)
	if len(natural) == 0 {
		t.Fatalf("trade-day control emitted no box entry: %v", led)
	}
	maxRisk := 0.0
	for _, in := range natural {
		risk := abs(in.Price - in.Stop)
		if abs(in.Target-in.Price) < 2*risk {
			t.Fatalf("fixture cannot discriminate: %+v (level < 2×risk)", in)
		}
		if risk > maxRisk {
			maxRisk = risk
		}
	}
	// One cap for the tick: ≥ every entry's risk, < 2× the largest risk.
	capPts := 1.5 * maxRisk
	for _, in := range natural {
		if capPts < abs(in.Price-in.Stop) {
			t.Fatalf("cap %.2f would break the 1:1 floor for %+v", capPts, in)
		}
	}
	spent, led := run(DaySpent, capPts)
	if len(spent) != len(natural) {
		t.Fatalf("SPENT-day box emitted %d of the %d entries whose room to the LEVEL is ≥ 2R (the room read the %.2f-pt cap): ledger %v", len(spent), len(natural), capPts, led)
	}
	for _, in := range spent {
		if d := abs(in.Target - in.Price); d > capPts+1e-9 {
			t.Fatalf("spent-day box take-profit %.2f pts out, must be capped at %.2f: %+v", d, capPts, in)
		}
	}
}

// TestMentorNYSpentDayReverseISBRoomIsToTheLevel — the reverse ISB at EMA 9 at
// Tick, with the LIVE room multiple and the key level moved out to 130 so the
// level clears 2R. MUTANT: eval.go reverse-ISB room back on capped.Target →
// RED (refused "room").
func TestMentorNYSpentDayReverseISBRoomIsToTheLevel(t *testing.T) {
	run := func(verdict DayVerdict, capPts float64) (Intent, map[string]int) {
		e, bars, now := isbReverseFixture(t)
		e.Cfg.RoomMultiple = DefaultConfig().RoomMultiple
		e.Cfg.DayGateTargetCapPts = capPts
		e.State.SeedLevels = []Level{
			{Key: "K-below", Kind: KindKeyLevel, Price: 90},
			{Key: "K-above", Kind: KindKeyLevel, Price: 130},
		}
		e.State.Day.Verdict = verdict
		for _, in := range e.Tick(bars, now) {
			if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && (in.Setup == "ISB" || in.Setup == "PHL") && in.Side == SideLong {
				return in, e.State.Refusals
			}
		}
		return Intent{}, e.State.Refusals
	}
	natural, led := run(DayTrade, 15)
	if natural.Price == 0 {
		t.Fatalf("trade-day control: the reverse ISB did not emit: %v", led)
	}
	risk := abs(natural.Price - natural.Stop)
	level := abs(natural.Target - natural.Price)
	if risk <= 0 || level < 2*risk {
		t.Fatalf("fixture cannot discriminate: risk %.2f, level %.2f", risk, level)
	}
	capPts := 1.5 * risk
	spent, led := run(DaySpent, capPts)
	if spent.Price == 0 {
		t.Fatalf("SPENT-day reverse ISB with room %.2f ≥ 2×%.2f to the LEVEL was refused (the room read the %.2f-pt cap): ledger %v",
			level, risk, capPts, led)
	}
	if d := abs(spent.Target - spent.Price); abs(d-capPts) > 1e-9 {
		t.Fatalf("the reverse ISB take-profit must still be capped at %.2f, got %.2f pts", capPts, d)
	}
}

// TestMentorNYSpentDayISBRoomIsTwiceTheCappedTarget — DS-104 P2-3 at Tick, the
// course's worked figure "target 15 → room 30" [D5.3 p1 @09:32–09:50]: on a
// SPENT day the room to the level must also be ≥ 2× the CAPPED take-profit.
// The level sits 21.4 pts out (risk 4.2, so 2R = 8.4 alone passes it): with the
// 15-pt cap the ISB is refused (21.4 < 30); with a 10-pt cap it emits (21.4 ≥ 20)
// and the take-profit is entry + 10.
// MUTANT: drop the spent-day room check at the ISB site → the cap-15 case emits → RED.
func TestMentorNYSpentDayISBRoomIsTwiceTheCappedTarget(t *testing.T) {
	run := func(capPts float64) (Intent, map[string]int) {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.EMAPeriod34 = 0
		cfg.EMAPeriod9 = 0
		cfg.EMALocationTFMinutes = 0
		cfg.DayGateSpentPts = 300
		cfg.DayGateTargetCapPts = capPts
		e := newISBEval(cfg)
		bars := isbFixture()
		now := bars[len(bars)-1].CloseTime + 1
		e.seeded = true
		e.State.Seed1mWatermark = math.MaxInt64
		e.State.Seed1HWatermark = math.MaxInt64
		e.State.SeedLevels = []Level{{Key: "far", Kind: KindKeyLevel, Price: 125}}
		e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DaySpent}
		for _, in := range e.Tick(bars, now) {
			if in.Action == PlaceStopLimitEntry {
				return in, e.State.Refusals
			}
		}
		return Intent{}, e.State.Refusals
	}
	if in, led := run(15); in.Price != 0 || led["room"] == 0 {
		t.Fatalf("cap 15: room 21.4 < 2×15 must refuse as room (the course figure), got %+v ledger %v", in, led)
	}
	in, led := run(10)
	if in.Price == 0 {
		t.Fatalf("cap 10: room 21.4 ≥ 2×10 must emit, ledger %v", led)
	}
	if d := abs(in.Target - in.Price); abs(d-10) > 1e-9 {
		t.Fatalf("cap 10: the take-profit must be entry + 10, got %.2f pts", d)
	}
}
