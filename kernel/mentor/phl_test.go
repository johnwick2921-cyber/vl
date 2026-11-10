package mentor

import (
	"testing"

	"vl/market"
)

// TestPHLPLHWorkedExampleGeometry — §2.2 [D2.2 p1 @ 06:50, R2 @ 19:34]:
// the buy stop sits at the PREVIOUS candle's high + the entry ray offset
// (29,396.25 = high 29,395.75 + 0.50, D2.2-R10 [p1 @04:14–05:43]), stop = the
// low of that broken candle − the stop ray offset (29,387.50 − 0.50 =
// 29,387.00), target 29,425.75 — visibly below the old high 29,430–29,440. The
// setup must PASS the room rule (reward 29.5 vs risk 9.25 = 3.2R ≥ 2×) and the
// stop ceiling.
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
	oldHigh := Level{Key: "old-high", Kind: KindOldExtreme, Role: OldHigh, Price: 29_431.75}
	in, ok, reason := PHLPLH(touch, oldHigh, 0, 3, cfg)
	if !ok {
		t.Fatalf("worked example refused: %s", reason)
	}
	if in.Side != SideLong || in.Price != 29_396.75 || in.Stop != 29_386.5 {
		t.Fatalf("entry = %+v, want long price 29396.75 (high 29395.75 + 1.00) stop 29386.50 (low 29387.50 − 1.00) [Oct D2.3 @ 46:20-47:20 OctD2.3-R46 supersedes Sept D2.2-R10]", in)
	}
	// target = old high − shy = 29,425.75
	if in.Target != 29_425.75 {
		t.Fatalf("target = %.2f, want 29425.75 (near, not at, the old high)", in.Target)
	}
	if !(29_425.75 < 29_431.75) {
		t.Fatalf("target must sit BEFORE the old high [D4.1 p1 @ 07:27]")
	}
}

// TestPHLEntryBufferKnob — D2.2-R10 [p1 @04:14–05:43, screen]: the PHL entry
// is the candle extreme PLUS the entry buffer (default 0.50: signal high
// 29,395.75 → entry ray 29,396.25), outward; the STOP is the broken candle's
// other extreme MINUS the stop buffer (default 0.50: low 29,386.50 → stop ray
// 29,386.00). The entry knob moves the entry only.
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
	if in.Stop != 94 {
		t.Fatalf("stop = %.2f, want the candle low 95 − the 1.00 default stop buffer = 94 [Oct D2.3 @ 46:20-47:20 OctD2.3-R46 supersedes Sept D2.2-R10]", in.Stop)
	}
}

// TestPHLStopBufferKnob — D2.2-R10 [p1 @04:14–05:43]: the stop ray sits 0.50
// UNDER the signal low (long) / OVER the signal high (short); the knob moves
// the stop outward only.
// MUTANT: drop the −cfg.PHLStopBufferPts on the stop → 95 / 100 → RED.
func TestPHLStopBufferKnob(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PHLStopBufferPts = 1.25
	long := Touch{Outcome: TouchReject, RefBar: market.Kline{High: 100, Low: 95}, ApproachedFrom: SideLong}
	in, ok, reason := PHLPLH(long, Level{Kind: KindOldExtreme, Price: 130}, 0, 3, cfg)
	if !ok || in.Stop != 93.75 || in.Price != 100+cfg.PHLEntryBufferPts {
		t.Fatalf("long: ok=%v %s entry %.2f stop %.2f, want stop 95 − 1.25 = 93.75", ok, reason, in.Price, in.Stop)
	}
	short := Touch{Outcome: TouchReject, RefBar: market.Kline{High: 100, Low: 95}, ApproachedFrom: SideShort}
	in, ok, reason = PHLPLH(short, Level{Kind: KindOldExtreme, Price: 60}, 0, 3, cfg)
	if !ok || in.Stop != 101.25 || in.Price != 95-cfg.PHLEntryBufferPts {
		t.Fatalf("short: ok=%v %s entry %.2f stop %.2f, want stop 100 + 1.25 = 101.25", ok, reason, in.Price, in.Stop)
	}
}

// TestPHLEMA34StopNuance — D3.3 p1 @00:40–01:05: when the PHL location is the
// EMA 34 and the reference candle only TOUCHED it (no wick through), the stop
// sits right AT the EMA 34; a wick that passed through keeps the normal
// beyond-the-wick stop. MUTANT: drop the PriceAtTouch branch in phlPLHR2 → the
// no-wick-through stop comes back at the candle low instead of the EMA 34 → RED.
func TestPHLEMA34StopNuance(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	ema := 95.0
	oldHigh := Level{Key: "old-high", Kind: KindOldExtreme, Role: OldHigh, Price: 140}
	// No wick through: candle low (96) sits ABOVE the EMA 34 → stop = the EMA 34.
	touch := Touch{
		LevelKey: string(KindEMA34), Outcome: TouchReject,
		RefBar:         market.Kline{High: 101, Low: 96, Close: 98},
		ApproachedFrom: SideLong, PriceAtTouch: ema,
	}
	in, ok, reason := PHLPLH(touch, oldHigh, 0, 3, cfg)
	if !ok {
		t.Fatalf("EMA34 PHL refused: %s", reason)
	}
	if in.Stop != ema {
		t.Fatalf("no wick through: stop = %.2f, want the EMA 34 price %.2f", in.Stop, ema)
	}
	// Wick through: candle low (93) sits BELOW the EMA 34 → the normal stop,
	// the candle low − the 0.50 stop ray offset [D2.2-R10 p1 @04:14–05:43].
	touch2 := Touch{
		LevelKey: string(KindEMA34), Outcome: TouchReject,
		RefBar:         market.Kline{High: 101, Low: 93, Close: 98},
		ApproachedFrom: SideLong, PriceAtTouch: ema,
	}
	in2, ok2, reason2 := PHLPLH(touch2, oldHigh, 0, 3, cfg)
	if !ok2 {
		t.Fatalf("EMA34 PHL refused: %s", reason2)
	}
	if in2.Stop != 92 {
		t.Fatalf("wick through: stop = %.2f, want the candle low 93 − 1.00 = 92 [Oct D2.3 @ 46:20-47:20 OctD2.3-R46 supersedes Sept D2.2-R10]", in2.Stop)
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

// TestPHLPLHRoomRule — §6 [D5.3 p1 @ 09:16]: reward must be at least 2× risk.
// A 1:1 shape is refused.
func TestPHLPLHRoomRule(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PHLTargetShyPts = 0
	// entry 110 (buy stop above ref high 108.5 + 1.5), stop 103 (ref low 104.5 − 1.5)
	touch := Touch{
		Outcome: TouchReject, RefBar: market.Kline{High: 108.5, Low: 104.5},
		ApproachedFrom: SideLong,
	}
	// target 115: reward 5, risk 7 → 0.71R < 2 → refused
	if _, ok, _ := PHLPLH(touch, Level{Kind: KindOldExtreme, Price: 115}, 0, 3, cfg); ok {
		t.Fatal("1:1-ish trade passed the room rule")
	}
	// target 125: reward 15, risk 7 → 2.14R → accepted
	if _, ok, reason := PHLPLH(touch, Level{Kind: KindOldExtreme, Price: 125}, 0, 3, cfg); !ok {
		t.Fatalf("2.14R trade refused: %s", reason)
	}
}

// TestPHLPLHStopCeiling — §6 [D3.3 p1 @ 02:04]: stop over 25 pts is never a
// trade, regardless of reward.
func TestPHLPLHStopCeiling(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.CourseExits.StopSizeLadder = false // pins the ladder-OFF (pre-2026-11-10) ceiling; the ladder-ON path is stop_size_ladder_test.go
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
