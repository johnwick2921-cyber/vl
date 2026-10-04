package mentor

import (
	"testing"

	"vl/market"
)

// TestORBDrawsAfterTheFirst2mCandle — the ORB is the high and the low of the
// FIRST 2-minute candle of the regular session (08:30–08:32 CT), drawn ONLY
// once that candle has completed [X5 @01:52, 06:29]. Pre-market bars never
// draw it.
func TestORBDrawsAfterTheFirst2mCandle(t *testing.T) {
	mk := func(hh, mm int, h, l, c float64) market.Kline {
		// real-UTC epochs (EPOCH RULING): 2026-09-15 CT bars.
		ot := auditMs(2026, 9, 15, hh, mm, 0)
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(8, 0, 30000, 29000, 29500), // pre-market — never draws the ORB
		mk(8, 30, 24844, 24814, 24830),
		mk(8, 31, 24830, 24816, 24820),
	}
	// at 08:31:59 the candle has NOT completed
	orb := ORBAdvance(ORB{}, bars, auditMs(2026, 9, 15, 8, 31, 59))
	if orb.Drawn {
		t.Fatal("the ORB must not be drawn before 08:32")
	}
	// at 08:32:00 it is drawn from the 08:30+08:31 bars only
	orb = ORBAdvance(ORB{}, bars, auditMs(2026, 9, 15, 8, 32, 0))
	if !orb.Drawn || orb.High != 24844 || orb.Low != 24814 {
		t.Fatalf("ORB = %+v, want drawn 24844/24814", orb)
	}
}

// TestORBDrawsAndTestsTheSameClosedCandle — P5: the ORB is drawn at the
// 08:32 tick (the tick processing the bar that closed at 08:32) and the
// escape test must run on that SAME closed candle, not the next one.
// Range-day shape: the ORB's high comes from the 08:30 bar and the 08:32
// bar's BODY closes above it → the escape latches at 08:32, not 08:33.
func TestORBDrawsAndTestsTheSameClosedCandle(t *testing.T) {
	mk := func(hh, mm int, o, h, l, c float64) market.Kline {
		ot := auditMs(2026, 9, 15, hh, mm, 0)
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(8, 30, 24820, 24844, 24814, 24830), // ORB high 24844, low 24814
		mk(8, 31, 24830, 24836, 24816, 24820),
		mk(8, 32, 24840, 24870, 24830, 24860), // BODY closes above the high → escape
	}
	// the 08:32 tick: the 08:32 bar just closed at 08:32:59.999
	orb := ORBAdvance(ORB{}, bars, auditMs(2026, 9, 15, 8, 33, 0)-1)
	if !orb.Drawn || orb.High != 24844 || orb.Low != 24814 {
		t.Fatalf("ORB = %+v, want drawn 24844/24814", orb)
	}
	if orb.Escaped != SideLong {
		t.Fatalf("escape = %q, want long at the 08:32 tick — the drawing candle itself is tested [P5]", orb.Escaped)
	}
}

// TestORBGateWorkedExample — the seen example: Mon 22 Sep '25, ORB
// 24,814.00–24,844.00; price clears 24,844.00 → longs only. Inside the ORB
// nothing trades and there is no reversal at the edges; the escape only picks
// the side [X5 @02:36, 03:29–03:47]. The §8 swing is exempt.
func TestORBGateWorkedExample(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	if !cfg.OrbGateEnabled {
		t.Fatal("orb_gate_enabled must default ON")
	}
	day := auditMs(2026, 9, 15, 9, 0, 0)
	orb := ORB{Day: dayStartCT(day), High: 24844, Low: 24814, Drawn: true}
	// before the escape: nothing anywhere
	for _, p := range []float64{24830, 24850, 24800} {
		if ok, _ := ORBVerdict(orb, SideLong, p, cfg); ok {
			t.Fatalf("before the escape nothing may trade at %v", p)
		}
		if ok, _ := ORBVerdict(orb, SideShort, p, cfg); ok {
			t.Fatalf("before the escape nothing may trade at %v", p)
		}
	}
	// price clears 24,844 with a 1m BODY close above → longs only
	esc := market.Kline{OpenTime: day, CloseTime: day + 59_999, High: 24860, Low: 24840, Close: 24850}
	orb = ORBAdvance(orb, []market.Kline{esc}, day+59_999)
	if orb.Escaped != SideLong {
		t.Fatalf("escape = %q, want long", orb.Escaped)
	}
	if ok, _ := ORBVerdict(orb, SideLong, 24850, cfg); !ok {
		t.Fatal("after an upward escape, longs outside the ORB are allowed")
	}
	if ok, reason := ORBVerdict(orb, SideShort, 24810, cfg); ok || reason == "" {
		t.Fatalf("shorts are off after an upward escape: %q", reason)
	}
	if ok, reason := ORBVerdict(orb, SideLong, 24830, cfg); ok || reason == "" {
		t.Fatalf("a long INSIDE the ORB range is off: %q", reason)
	}
	// the filter drops the refused intents, keeps cancels, and EXEMPTS swings
	ints := []Intent{
		{Action: PlaceStopEntry, Side: SideLong, Price: 24850, Reason: "PHL"},
		{Action: PlaceStopEntry, Side: SideShort, Price: 24810, Reason: "PLH"},
		{Action: PlaceStopLimitEntry, Side: SideLong, Price: 24855, Reason: "ISB"},
		{Action: CancelArm, Reason: "stacking"},
		{Action: PlaceStopEntry, Side: SideShort, Price: 24810, Reason: "swing §8: reject touch"},
	}
	got, _ := orbGateFilter(ints, orb, day, cfg)
	if len(got) != 4 {
		t.Fatalf("gate kept %d intents, want 4 (long PHL, long ISB, cancel, exempt swing; short PLH dropped): %+v", len(got), got)
	}
}

// TestORBNotDrawnBlocksEverything — before 08:32 the gate blocks every
// intraday entry (and a new session day resets the ORB).
func TestORBNotDrawnBlocksEverything(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	if ok, reason := ORBVerdict(ORB{Day: 1}, SideLong, 25000, cfg); ok || reason == "" {
		t.Fatal("an undrawn ORB must block every entry")
	}
	// knob OFF: the gate is silent
	cfg.OrbGateEnabled = false
	if ok, _ := ORBVerdict(ORB{Day: 1}, SideLong, 25000, cfg); !ok {
		t.Fatal("orb_gate_enabled=false must not gate")
	}
	// a new day resets the ORB
	orb := ORB{Day: dayStartCT(auditMs(2026, 9, 15, 9, 0, 0)), High: 100, Low: 90, Drawn: true, Escaped: SideLong}
	day2 := dayStartCT(auditMs(2026, 9, 16, 9, 0, 0))
	next := ORBAdvance(orb, nil, auditMs(2026, 9, 16, 9, 0, 0))
	if next.Drawn || next.Escaped != "" || next.Day != day2 {
		t.Fatalf("a new session day must reset the ORB: %+v", next)
	}
}
