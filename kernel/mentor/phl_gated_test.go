package mentor

import (
	"testing"

	"vl/market"
)

// The gated call site: fold items 3 + 4 on top of the §2.2 PHL/PLH rules
// (which fold item 2 declares unchanged: a touch-and-reject at a level IS a
// PHL/PLH and carries the ≥3-candle / target-near / room-≥2× rules).

// workedTouch builds the §2.2 worked-example touch [D2.2 p1 @ 06:50]:
// entry 29,397.25, stop 29,386.00, target near the old high 29,431.75.
func workedTouch() Touch {
	return Touch{
		LevelKey: "old-low", Outcome: TouchReject,
		RefBar:         market.Kline{High: 29_395.75, Low: 29_387.5, Close: 29_392.0},
		ApproachedFrom: SideLong,
	}
}

func workedCfg() Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PHLTargetShyPts = 6 // target 29,425.75
	return cfg
}

// TestPHLPLHGatedAgreePasses — 4h long + 1h long (case 1), normal day: the
// setup ships unchanged.
func TestPHLPLHGatedAgreePasses(t *testing.T) {
	htf := HTF{FourH: TriggerLine{Dir: SideLong, Price: 29400}, OneH: TriggerLine{Dir: SideLong, Price: 29350}}
	in, ok, reason := PHLPLHGated(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, workedCfg(), htf, DayTrade, DefaultDayGate())
	if !ok {
		t.Fatalf("agree + normal day refused: %s", reason)
	}
	if in.Target != 29_425.75 {
		t.Fatalf("target = %.2f, want the uncapped worked-example 29425.75", in.Target)
	}
}

// TestPHLPLHGatedCase3Refuses — 4h long, 1h short: the valid PHL is refused
// until the 1h flips [D4.4 p1 @ 16:00].
func TestPHLPLHGatedCase3Refuses(t *testing.T) {
	htf := HTF{FourH: TriggerLine{Dir: SideLong, Price: 29400}, OneH: TriggerLine{Dir: SideShort, Price: 29350}}
	if _, ok, reason := PHLPLHGated(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, workedCfg(), htf, DayTrade, DefaultDayGate()); ok || reason == "" {
		t.Fatalf("case-3 setup shipped: ok=%v reason=%q", ok, reason)
	}
	// the flip opens it again
	htf.OneH.Dir = SideLong
	if _, ok, _ := PHLPLHGated(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, workedCfg(), htf, DayTrade, DefaultDayGate()); !ok {
		t.Fatal("after the 1h flip the same setup must pass")
	}
}

// TestPHLPLHGatedSideMismatchRefuses — a short PHL against a long 4h trigger
// is refused: entries only with the 4h direction.
func TestPHLPLHGatedSideMismatchRefuses(t *testing.T) {
	// mirror touch: short entry below a reject candle at resistance
	touch := Touch{
		Outcome: TouchReject, ApproachedFrom: SideShort,
		RefBar: market.Kline{High: 29_440, Low: 29_431, Close: 29_438},
	}
	htf := HTF{FourH: TriggerLine{Dir: SideLong, Price: 29400}}
	if _, ok, reason := PHLPLHGated(touch, Level{Kind: KindOldExtreme, Price: 29_400}, 0, 3, workedCfg(), htf, DayTrade, DefaultDayGate()); ok || reason == "" {
		t.Fatalf("short against the 4h long shipped: ok=%v reason=%q", ok, reason)
	}
}

// TestPHLPLHGatedDayOffRefuses — a valid setup on a spent+conflict day is
// refused: "TẮT MÁY NGHỈ LUÔN CHO EM" [D5.1 p1 @ 19:22]. (The latch itself
// is DayLatch's job; the wrapper refuses whatever DayOff reaches it.)
func TestPHLPLHGatedDayOffRefuses(t *testing.T) {
	htf := HTF{FourH: TriggerLine{Dir: SideLong, Price: 29400}, OneH: TriggerLine{Dir: SideLong, Price: 29350}}
	if _, ok, reason := PHLPLHGated(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, workedCfg(), htf, DayOff, DefaultDayGate()); ok || reason == "" {
		t.Fatalf("DayOff setup shipped: ok=%v reason=%q", ok, reason)
	}
}

// TestPHLPLHGatedNotMeasuredRefuses — E3: an unmeasured day run fails
// closed, with a reason ("any trade you are vague about — don't" [§12]).
func TestPHLPLHGatedNotMeasuredRefuses(t *testing.T) {
	htf := HTF{FourH: TriggerLine{Dir: SideLong, Price: 29400}}
	if _, ok, reason := PHLPLHGated(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, workedCfg(), htf, DayNotMeasured, DefaultDayGate()); ok || reason == "" {
		t.Fatalf("DayNotMeasured setup shipped: ok=%v reason=%q", ok, reason)
	}
}

// TestPHLPLHGatedSpentDayCapsTarget — spent + agree: the target distance is
// capped at 15 pts ("15 điểm bán, 10 điểm bán" [D5.1 p1 @ 15:57]). The
// fixture uses a 7.5-pt stop so the capped 15-pt target still clears the room
// rule (2× risk) — the B7 (L13) consistency: room reads the CAPPED target.
func TestPHLPLHGatedSpentDayCapsTarget(t *testing.T) {
	htf := HTF{FourH: TriggerLine{Dir: SideLong, Price: 29400}}
	// tight stop: risk 7.5 → the 15-pt capped target is exactly 2× risk.
	touch := Touch{Outcome: TouchReject, ApproachedFrom: SideLong,
		RefBar: market.Kline{High: 29_395.75, Low: 29_389.25, Close: 29_392.0}}
	in, ok, _ := PHLPLHGated(touch, Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, workedCfg(), htf, DaySpent, DefaultDayGate())
	if !ok {
		t.Fatal("spent+agree with a 7.5-pt stop must still trade (capped 15 ≥ 2×risk)")
	}
	if in.Target != 29_411.75 {
		t.Fatalf("target = %.2f, want the 15-pt cap 29411.75 (entry 29396.75 + 15)", in.Target)
	}
	if in.Stop != 29_389.25 || in.Price != 29_396.75 {
		t.Fatalf("the cap must only touch the target: %+v", in)
	}
}

// TestPHLPLHGatedSpentDayCappedTargetRoom (option B): on a spent day the
// take-profit is min(15-pt cap, half the room to the first level). The first
// level (29,421.75) is 25 pts from the entry → half = 12.5 ≥ 1R(9.25), and
// min(15, 12.5) = 12.5 → the PHL ships at 29,409.25, BELOW the 15-pt cap.
func TestPHLPLHGatedSpentDayCappedTargetRoom(t *testing.T) {
	htf := HTF{FourH: TriggerLine{Dir: SideLong, Price: 29400}}
	levels := []Level{{Key: "key_level:29421.75", Kind: KindKeyLevel, Price: 29_421.75}}
	in, ok, reason := PHLPLHGatedR2Levels(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, 29_380, levels, workedCfg(), htf, DaySpent, DefaultDayGate())
	if !ok {
		t.Fatalf("option B: spent-day PHL must halve the room (not refuse): %s", reason)
	}
	if in.Target != 29_409.25 {
		t.Fatalf("target = %.2f, want 29409.25 (half the 25-pt room, below the 15 cap)", in.Target)
	}
}

// TestPHLPLHR2HigherLowRequired — R2: a HIGHER low is required; a flat low
// is an FTGL, not a PHL [D2.2 p1 R2]. The mirror refuses a flat high.
func TestPHLPLHR2HigherLowRequired(t *testing.T) {
	cfg := workedCfg()
	// prior swing low 29,387.5 — the reference low EQUALS it → flat → FTGL
	if _, ok, reason := PHLPLHR2(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, 29_387.5, cfg); ok || reason == "" {
		t.Fatalf("flat low shipped: ok=%v reason=%q", ok, reason)
	}
	// a higher low passes
	if _, ok, _ := PHLPLHR2(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, 29_380.0, cfg); !ok {
		t.Fatal("a higher low must pass")
	}
	// mirror: short, prior swing high == ref high → flat high refused
	shortTouch := Touch{Outcome: TouchReject, ApproachedFrom: SideShort, RefBar: market.Kline{High: 29_440, Low: 29_431, Close: 29_438}}
	if _, ok, _ := PHLPLHR2(shortTouch, Level{Kind: KindOldExtreme, Price: 29_400}, 0, 3, 29_440, cfg); ok {
		t.Fatal("flat high shipped — must be an FTGH, not a PLH")
	}
}

// TestPHLPLHGatedSpentDaySkipStopOver15 — R9: on a spent day, skip any
// setup whose stop is over the 15-pt cap [D1.2 p1 @ 07:48–09:00].
func TestPHLPLHGatedSpentDaySkipStopOver15(t *testing.T) {
	cfg := workedCfg()
	htf := HTF{FourH: TriggerLine{Dir: SideLong, Price: 29400}}
	// reference candle 20 pts tall → stop distance 20 > 15; the old extreme
	// sits far enough that ONLY R9 can refuse (reward 44 ≥ 2×20).
	touch := Touch{Outcome: TouchReject, ApproachedFrom: SideLong, RefBar: market.Kline{High: 29_400, Low: 29_380, Close: 29_385}}
	if _, ok, reason := PHLPLHGated(touch, Level{Kind: KindOldExtreme, Price: 29_450}, 0, 3, cfg, htf, DaySpent, DefaultDayGate()); ok || reason == "" {
		t.Fatalf("spent day with a 20-pt stop shipped: ok=%v reason=%q", ok, reason)
	}
}

// TestPHLPLHGatedMissing4hRefuses — no 4h trigger: no direction to follow,
// even with a perfectly valid setup [D4.4 p1 @ 02:53].
func TestPHLPLHGatedMissing4hRefuses(t *testing.T) {
	if _, ok, reason := PHLPLHGated(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, workedCfg(), HTF{}, DayTrade, DefaultDayGate()); ok || reason == "" {
		t.Fatalf("no-4h setup shipped: ok=%v reason=%q", ok, reason)
	}
}
