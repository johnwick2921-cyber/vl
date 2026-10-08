package mentor

import (
	"strings"
	"testing"

	"vl/market"
)

// TestPHLPLHWorkedExampleGeometry — §2.2 [D2.2 p1 @ 06:50, R2 @ 19:34]:
// the buy stop sits at the PREVIOUS candle's high + the course buffer
// (29,396.75 = high 29,395.75 + 1.0, D2-15), stop = the low of that broken
// candle (29,387.50), target 29,425.75 — visibly below the old high
// 29,430–29,440. The setup must PASS the room rule (reward 29 vs risk
// 9.25 = 3.1R ≥ 2×) and the stop ceiling.
func TestPHLPLHWorkedExampleGeometry(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PHLTargetShyPts = 6 // worked example: 29,425.75 vs old high ~29,431.75
	ref := market.Kline{High: 29_395.75, Low: 29_387.5, Close: 29_392.0}
	touch := Touch{
		LevelKey: "old-low", Outcome: TouchReject,
		RefBar:         ref,
		ApproachedFrom: SideLong,
	}
	oldHigh := Level{Key: "old-high", Kind: KindOldExtreme, Price: 29_431.75}
	in, ok, reason := PHLPLH(touch, oldHigh, 0, 3, cfg)
	if !ok {
		t.Fatalf("worked example refused: %s", reason)
	}
	if in.Side != SideLong || in.Price != 29_396.75 || in.Stop != 29_387.5 {
		t.Fatalf("entry = %+v, want long price 29396.75 (high 29395.75 + 1.0 buffer) stop 29387.50", in)
	}
	// target = old high − shy = 29,425.75
	if in.Target != 29_425.75 {
		t.Fatalf("target = %.2f, want 29425.75 (near, not at, the old high)", in.Target)
	}
	if !(29_425.75 < 29_431.75) {
		t.Fatalf("target must sit BEFORE the old high [D4.1 p1 @ 07:27]")
	}
}

// TestPHLEntryBufferKnob — D2-15 [D2.2 p1 @06:50 drawn]: the PHL entry is the
// candle extreme PLUS the course buffer (default 1.0; high 29,396.25 → entry
// 29,397.25), outward. The STOP stays at the broken candle's extreme (the
// frame draws it AT the low). The knob moves the entry; the stop is untouched.
// MUTANT: drop the +cfg.PHLEntryBufferPts on the entry → the 2.0-knob entry
// comes back at 100 instead of 102 → RED.
func TestPHLEntryBufferKnob(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PHLEntryBufferPts = 2.0
	touch := Touch{
		Outcome: TouchReject, RefBar: market.Kline{High: 100, Low: 95},
		ApproachedFrom: SideLong,
	}
	in, ok, reason := PHLPLH(touch, Level{Kind: KindOldExtreme, Price: 130}, 0, 3, cfg)
	if !ok {
		t.Fatalf("buffered PHL refused: %s", reason)
	}
	if in.Price != 102 {
		t.Fatalf("entry = %.2f, want high 100 + 2.0 buffer = 102", in.Price)
	}
	if in.Stop != 95 {
		t.Fatalf("stop = %.2f, want the candle low 95 (no stop buffer)", in.Stop)
	}
}

// TestPHLPLHRequiresThreeCandlesFromOldExtreme — [D4.1 p1 @ 09:40 written]:
// "Phải đợi đi xa đỉnh/đáy cũ trước đó (ít nhất là 3 cây nến backtest)".
func TestPHLPLHRequiresThreeCandlesFromOldExtreme(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	touch := Touch{
		Outcome: TouchReject, RefBar: market.Kline{High: 101, Low: 99},
		ApproachedFrom: SideLong,
	}
	oldHigh := Level{Kind: KindOldExtreme, Price: 120}
	_, ok, reason := PHLPLH(touch, oldHigh, 0, 2, cfg)
	if ok {
		t.Fatalf("entry 2 candles from the old extreme was allowed")
	}
	if reason == "" {
		t.Fatal("refusal must carry a reason")
	}
	if _, ok, _ := PHLPLH(touch, oldHigh, 0, 3, cfg); !ok {
		t.Fatal("entry exactly 3 candles away must pass the distance gate")
	}
}

// TestPHLPLHRoomRule — the D1.2 floor [@07:41–08:45] and the D5.3 room-vs-target
// rule [@09:16–10:17] on the PHL path. A sub-1:1 target is refused by the floor;
// a 1:1 target whose next opposing level sits closer than 2× the target is
// refused by the room rule.
func TestPHLPLHRoomRule(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PHLTargetShyPts = 0
	// entry 101 (ref high 100 + 1.0 buffer), stop 95 → risk 6.
	touch := Touch{
		Outcome: TouchReject, RefBar: market.Kline{High: 100, Low: 95},
		ApproachedFrom: SideLong,
	}
	// Floor: target 102 → reward 1 < risk 6 → refused by the 1:1 floor.
	if _, ok, reason := PHLPLH(touch, Level{Kind: KindOldExtreme, Price: 102}, 0, 3, cfg); ok {
		t.Fatal("sub-1:1 PHL passed the floor")
	} else if !strings.HasPrefix(reason, "rr_floor") {
		t.Fatalf("floor refusal reason = %q, want rr_floor prefix", reason)
	}
	// Room: target = key level 107 (reward 6 = risk 6, floor passes); the next
	// opposing level beyond the old extreme's anchor is 111 → room 10 < 2×6 = 12
	// → refused by the room rule.
	levels := []Level{
		{Key: "k", Kind: KindKeyLevel, Price: 107},
		{Key: "room", Kind: KindKeyLevel, Price: 111},
	}
	if _, ok, reason := PHLPLHR2Levels(touch, Level{Kind: KindOldExtreme, Price: 108}, 0, 3, 0, levels, cfg); ok {
		t.Fatal("PHL whose room (10) is under 2× target (12) passed the room rule")
	} else if !strings.HasPrefix(reason, "room_vs_target") {
		t.Fatalf("room refusal reason = %q, want room_vs_target prefix", reason)
	}
}

// TestPHLPLHStopCeiling — §6 [D3.3 p1 @ 02:04]: stop over 25 pts is never a
// trade, regardless of reward.
func TestPHLPLHStopCeiling(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	touch := Touch{
		Outcome: TouchReject, RefBar: market.Kline{High: 130, Low: 100},
		ApproachedFrom: SideLong,
	}
	// risk ≈ 30 > 25 → refused even with a huge target
	if _, ok, _ := PHLPLH(touch, Level{Kind: KindOldExtreme, Price: 200}, 0, 3, cfg); ok {
		t.Fatal("stop over the 25-pt ceiling was accepted")
	}
}

// TestPHLPLHOldExtremeOnTargetSide — the old extreme must be on the target
// side: long → old high above the entry; a high BELOW the entry is refused.
func TestPHLPLHOldExtremeOnTargetSide(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	touch := Touch{
		Outcome: TouchReject, RefBar: market.Kline{High: 101, Low: 99},
		ApproachedFrom: SideLong,
	}
	if _, ok, _ := PHLPLH(touch, Level{Kind: KindOldExtreme, Price: 95}, 0, 3, cfg); ok {
		t.Fatal("long targeting an old extreme BELOW the entry was accepted")
	}
	if _, ok, _ := PHLPLH(touch, Level{Kind: KindOldExtreme, Price: 125}, 0, 3, cfg); !ok {
		t.Fatal("long targeting the old high above the entry must pass")
	}
}

// TestPHLPLHWrongWayTouchIsNotASetup — §3 [D5.2 p1 @ 19:51]: only a REJECT
// touch can be a PHL/PLH; a wrong-way (invalidated) level never produces one.
func TestPHLPLHWrongWayTouchIsNotASetup(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	touch := Touch{Outcome: TouchWrongWay, RefBar: market.Kline{High: 101, Low: 99}}
	if _, ok, _ := PHLPLH(touch, Level{Kind: KindOldExtreme, Price: 125}, 0, 3, cfg); ok {
		t.Fatal("wrong-way touch produced a PHL/PLH")
	}
}
