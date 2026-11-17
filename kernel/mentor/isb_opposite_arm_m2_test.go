package mentor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// ── ISB-M2 (11-16 full re-read): two opposite ISB arms must never rest at once ──
//
// October: every ISB has ONE order. While it rests, the next candle whose body
// stays inside the first ISB is "Insider 3 của Insider 3" and the SAME order is
// kept — frame 52:09 shows a green mother, a red inside candle and ONE buy line,
// frame 52:45 the next inside candle with the same single line [Oct D4.3
// @52:03-52:49]. When two directions are on offer he picks one: "Mình có 2 hướng
// … Lúc nào cũng chọn cái hướng theo trend" [Oct D5.3 @17:40-17:56]. The course
// never rests a buy stop and a sell stop at once.
//
// The code: isbArmActive blocks only the SAME side, and the trigger-side gate is
// skipped when the governing trigger has no direction (TriggerVerdict side "").
// While the long arm (M green, I1 red) rests, the pair (I1, I2) is itself an ISB
// whose mother I1 is red → a SHORT arm is authored → a two-sided bracket around
// I1. Every pin below drives the PRODUCTION entry, e.Tick.

// isbM2Tape is the v-M2-codetrace tape (1m, 09:00 CT): a filler, the GREEN
// mother M, the RED inside candle I1 (body inside M), and I2 whose body sits
// inside I1 — so (I1, I2) is a fresh ISB whose mother is red (a short).
func isbM2Tape() []market.Kline {
	return []market.Kline{
		rthBars(0, 121, 121.5, 120.5, 120),
		rthBars(1, 98, 106, 97, 105),     // M: green mother
		rthBars(2, 104, 104.5, 99, 100),  // I1: red, body inside M → LONG ISB
		rthBars(3, 101, 102, 100.5, 102), // I2: body inside I1 → (I1,I2) is a SHORT ISB
	}
}

// isbM2Eval is the October default config (DefaultConfig: the 4h gate OFF), a
// trade day, and the given governing 5m trigger.
func isbM2Eval(guard bool, trig TriggerLine) *Evaluator {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ISBOppositeArmGuard = guard
	e := New(cfg)
	e.State.Trigger = trig
	bars := isbM2Tape()
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(bars[0].OpenTime).In(ctime())), Verdict: DayTrade}
	return e
}

type isbM2Placement struct {
	tick int
	side Side
	px   float64
	arm  string
}

func isbM2Run(t *testing.T, e *Evaluator, bars []market.Kline, beforeTick func(i int)) (placed []isbM2Placement, log []string) {
	t.Helper()
	for i := 2; i <= len(bars); i++ {
		if beforeTick != nil {
			beforeTick(i)
		}
		for _, in := range e.Tick(bars[:i], bars[i-1].CloseTime+1) {
			if (in.Action == PlaceStopLimitEntry || in.Action == PlaceStopEntry) && strings.HasPrefix(in.ArmID, "isb-") {
				placed = append(placed, isbM2Placement{tick: i, side: in.Side, px: in.Price, arm: in.ArmID})
				log = append(log, fmt.Sprintf("tick%d place %s %s px=%.2f stop=%.2f arm=%s", i, in.Setup, in.Side, in.Price, in.Stop, in.ArmID))
			} else if in.ArmID != "" {
				log = append(log, fmt.Sprintf("tick%d %s arm=%s", i, in.Action, in.ArmID))
			}
		}
		var arms []string
		for id, a := range e.State.ISBArms {
			arms = append(arms, fmt.Sprintf("%s:%s/in%d", id, a.Side, a.Inside))
		}
		log = append(log, fmt.Sprintf("tick%d arms=%v trig=%q refusals=%v", i, arms, e.State.Trigger.Dir, e.State.Refusals))
	}
	return
}

// isbM2Sides reports which sides have a live (Inside >= 0) ISB arm.
func isbM2Sides(e *Evaluator) (long, short bool) {
	for _, a := range e.State.ISBArms {
		if a.Inside < 0 {
			continue
		}
		switch a.Side {
		case SideLong:
			long = true
		case SideShort:
			short = true
		}
	}
	return
}

// TestISBM2NoTriggerNoOppositeArm — path (a) of the verifier: no governing 5m
// trigger direction yet. The long arm off (M, I1) rests; the short (I1, I2) arm
// must be refused by name and the long arm kept (extended), never a bracket.
func TestISBM2NoTriggerNoOppositeArm(t *testing.T) {
	e := isbM2Eval(true, TriggerLine{})
	placed, log := isbM2Run(t, e, isbM2Tape(), nil)
	t.Logf("trace:\n  %s", strings.Join(log, "\n  "))
	if len(placed) != 1 || placed[0].side != SideLong || placed[0].tick != 3 {
		t.Fatalf("want exactly ONE ISB placement, the long off (M, I1) on tick 3 (one order, Oct D4.3 @52:03-52:49); got %+v", placed)
	}
	if long, short := isbM2Sides(e); !long || short {
		t.Fatalf("resting arms long=%v short=%v: want the long kept and NO short (two-sided bracket)", long, short)
	}
	if e.State.Refusals["isb_opposite_arm_active"] == 0 {
		t.Fatalf("the short (I1, I2) ISB must be refused by name isb_opposite_arm_active; refusals %v", e.State.Refusals)
	}
}

// TestISBM2TriggerFlipNoOppositeArm — path (b): the long arm was authored under a
// 5m BUY line; the line flips to SELL while the arm rests. The short (I1, I2)
// ISB now passes the trigger-side gate; it must still not rest beside the long.
func TestISBM2TriggerFlipNoOppositeArm(t *testing.T) {
	e := isbM2Eval(true, TriggerLine{Dir: SideLong, Price: 90})
	placed, log := isbM2Run(t, e, isbM2Tape(), func(i int) {
		if i == 4 {
			e.State.Trigger = TriggerLine{Dir: SideShort, Price: 110}
		}
	})
	t.Logf("trace:\n  %s", strings.Join(log, "\n  "))
	if len(placed) != 1 || placed[0].side != SideLong {
		t.Fatalf("want ONE ISB placement (the long, before the flip); got %+v", placed)
	}
	if long, short := isbM2Sides(e); long && short {
		t.Fatalf("two opposite ISB arms rest at once after the trigger flip: %+v", e.State.ISBArms)
	}
	if e.State.Refusals["isb_opposite_arm_active"] == 0 {
		t.Fatalf("the short (I1, I2) ISB must be refused by name isb_opposite_arm_active; refusals %v", e.State.Refusals)
	}
}

// TestISBM2KnobOffRevertsToBracket — mentor_isb_opposite_arm_guard=false is the
// revert: the pre-ISB-M2 code authors the short (I1, I2) arm beside the resting
// long (the v-M2-codetrace output: long px 106, short px 99).
func TestISBM2KnobOffRevertsToBracket(t *testing.T) {
	e := isbM2Eval(false, TriggerLine{})
	placed, log := isbM2Run(t, e, isbM2Tape(), nil)
	t.Logf("trace:\n  %s", strings.Join(log, "\n  "))
	if len(placed) != 2 || placed[0].side != SideLong || placed[1].side != SideShort || placed[1].tick != 4 {
		t.Fatalf("revert: want the long on tick 3 AND the short on tick 4 (the old two-sided bracket); got %+v", placed)
	}
	if placed[0].px != 106.25 && placed[0].px != 106 {
		t.Logf("note: long px %.2f", placed[0].px)
	}
	if long, short := isbM2Sides(e); !long || !short {
		t.Fatalf("revert: want both arms resting (old behaviour), got long=%v short=%v", long, short)
	}
	if e.State.Refusals["isb_opposite_arm_active"] != 0 {
		t.Fatalf("revert: isb_opposite_arm_active must not fire with the knob off; refusals %v", e.State.Refusals)
	}
}

// TestISBM2OppositeArmAllowedOnceTheFirstIsGone — the guard holds only while the
// opposite arm RESTS: once the long arm has gone (filled, cancelled, expired or
// dropped by the trader), the short ISB is an ordinary new ISB again.
func TestISBM2OppositeArmAllowedOnceTheFirstIsGone(t *testing.T) {
	e := isbM2Eval(true, TriggerLine{})
	placed, log := isbM2Run(t, e, isbM2Tape(), func(i int) {
		if i == 4 {
			for id := range e.State.ISBArms {
				e.DropArm(id) // the trader refused / cancelled the long
			}
		}
	})
	t.Logf("trace:\n  %s", strings.Join(log, "\n  "))
	if len(placed) != 2 || placed[1].side != SideShort || placed[1].tick != 4 {
		t.Fatalf("with no resting long, the short (I1, I2) ISB must arm on tick 4; got %+v", placed)
	}
	if e.State.Refusals["isb_opposite_arm_active"] != 0 {
		t.Fatalf("isb_opposite_arm_active must not fire with no resting opposite arm; refusals %v", e.State.Refusals)
	}
}

// TestISBM2ReverseISBNotBesideARestingOppositeArm — the reverse ISB at EMA 9 is
// an ISB order too: with a SHORT ISB arm still resting from an earlier pair, the
// reverse LONG (the isbReverseFixture trade) is refused
// isbrev_opposite_arm_active; with the knob off it fires beside it (the revert).
// The same-pair case stays R85's (TestReverseISBEmitsWithTarget: the normal
// short authored THIS tick is suppressed, the reverse long fires).
func TestISBM2ReverseISBNotBesideARestingOppositeArm(t *testing.T) {
	for _, guard := range []bool{true, false} {
		e, bars, now := isbReverseFixture(t)
		e.Cfg.ISBOppositeArmGuard = guard
		prior := bars[len(bars)-4]
		e.State.ArmSeq = 7
		e.State.ISBArms["isb-7"] = ISBArm{FirstBar: prior, Inside: 1, Side: SideShort, TFMin: 1, LastTick: bars[len(bars)-2].CloseTime}
		var longs int
		for _, in := range e.Tick(bars, now) {
			if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Side == SideLong {
				longs++
			}
		}
		if guard {
			if longs != 0 || e.State.Refusals["isbrev_opposite_arm_active"] == 0 {
				t.Fatalf("guard ON: the reverse long must be refused isbrev_opposite_arm_active beside the resting short; longs=%d refusals %v", longs, e.State.Refusals)
			}
		} else {
			if longs != 1 || e.State.Refusals["isbrev_opposite_arm_active"] != 0 {
				t.Fatalf("guard OFF (revert): the reverse long fires beside the resting short; longs=%d refusals %v", longs, e.State.Refusals)
			}
		}
	}
}
