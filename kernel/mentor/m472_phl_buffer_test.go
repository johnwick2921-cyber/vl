package mentor

import (
	"testing"

	"vl/market"
)

// TestM472PHLBufferWorkedExample — the D2.3 short worked example through the
// production call site (DefaultConfig -> PHLPLH): reference candle low
// 28,317.25 / high 28,330.75; "move the entry 1 point beyond the candle low
// and the stop 1 point beyond the candle high: 2 points of buffer in total"
// -> entry 28,316.25, stop 28,331.75 (risk 15.5 = range 13.5 + 2.0).
// [Oct D2.3 @ 46:20-47:20] OctD2.3-R45/R46; frames
// /home/hoang/mm-course/oct/scenes/Ngày 2.3 PHL PLH/s_02487.jpg, s_02827.jpg,
// s_02872.jpg (stop ray 28,332 / entry ray 28,316.25 / old low 28,288 on screen; the test uses a lower old low so the 2x room gate has headroom; the real 28,288 is room-refused, see FrameOldLowRoomRefused).
// Supersedes the Sept 0.50/0.50 offsets [D2.2-R10 p1 @04:14-05:43].
// The on-screen stop ray reads 28,332.00 (high 28,330.75 + 1.25); the bot
// computes 28,331.75 (high + 1.00): a 0.25 screen-read difference, the entry
// matches exactly. The frame's own trade is refused by the 2x room gate
// (reward 23.25 < 2 x risk 15.5): a standing code-vs-video gap raised to the CTO.
func TestM472PHLBufferWorkedExample(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	ref := market.Kline{High: 28_330.75, Low: 28_317.25, Close: 28_319.25}
	touch := Touch{Outcome: TouchReject, RefBar: ref, ApproachedFrom: SideShort}
	oldLow := Level{Key: "old-low", Kind: KindOldExtreme, Role: OldLow, Price: 28_250}
	in, ok, reason := PHLPLH(touch, oldLow, 0, 3, cfg)
	if !ok {
		t.Fatalf("worked example refused: %s", reason)
	}
	if in.Side != SideShort || in.Price != 28_316.25 || in.Stop != 28_331.75 {
		t.Fatalf("got %+v, want short entry 28316.25 (low-1) stop 28331.75 (high+1)", in)
	}
}

// TestM472PHLBufferKnockOn — risk is now range+2.0: a 23.5-pt range candle
// (risk 25.5) is over the 25 ceiling, a 22.5 one (risk 24.5) still passes.
// [Oct D2.3 @ 46:20-47:20] OctD2.3-R46.
func TestM472PHLBufferKnockOn(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.CourseExits.StopSizeLadder = false // pins the ladder-OFF (pre-2026-11-10) ceiling; the ladder-ON path is stop_size_ladder_test.go
	oldLow := Level{Key: "old-low", Kind: KindOldExtreme, Role: OldLow, Price: 28_000}
	for _, c := range []struct {
		rng float64
		ok  bool
	}{{22.5, true}, {23.5, false}} {
		ref := market.Kline{High: 28_340 + c.rng, Low: 28_340, Close: 28_345}
		_, ok, why := PHLPLH(Touch{Outcome: TouchReject, RefBar: ref, ApproachedFrom: SideShort}, oldLow, 0, 3, cfg)
		if ok != c.ok {
			t.Fatalf("range %.1f: ok=%v want %v (%s)", c.rng, ok, c.ok, why)
		}
	}
}

// TestM472PHLBufferFrameOldLowRoomRefused records the on-screen example with
// the frame's REAL old low (28,288.00): reward 28,316.25-(28,288+5)=23.25 vs
// risk 15.5 is 1.5R. The September 2R room rule refused the mentor's trade
// (need 31); D2-G2 (oct round 2) ruled October's room 1:1 [OctD2.3-R31 @ 27:33],
// so at the default the frame's own trade now passes, and phl_room_multiple=2
// restores the refusal. Frame: Ngày 2.3 PHL PLH/s_02872.jpg.
func TestM472PHLBufferFrameOldLowRoomRefused(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	ref := market.Kline{High: 28_330.75, Low: 28_317.25, Close: 28_319.25}
	touch := Touch{Outcome: TouchReject, RefBar: ref, ApproachedFrom: SideShort}
	oldLow := Level{Key: "old-low", Kind: KindOldExtreme, Role: OldLow, Price: 28_288}
	cfg.PHLRoomMultiple = 1
	if in, ok, reason := PHLPLH(touch, oldLow, 0, 3, cfg); !ok {
		t.Fatalf("phl_room_multiple=1 (1R room): the frame's own trade must pass, refused: %s", reason)
	} else if in.Price != 28_316.25 {
		t.Fatalf("entry %.2f, want 28,316.25", in.Price)
	}
	sep := cfg
	sep.PHLRoomMultiple = 2 // the revert value (default 1 since answers-1112 Q2)
	if _, ok, reason := PHLPLH(touch, oldLow, 0, 3, sep); ok {
		t.Fatal("phl_room_multiple=2 must restore the September 2R refusal of the frame's example")
	} else {
		t.Logf("September room refuses: %s", reason)
	}
}
