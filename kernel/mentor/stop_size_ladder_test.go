package mentor

import (
	"strings"
	"testing"

	"vl/market"
)

// Stop-to-size LADDER — kernel half (owner order 2026-11-10: "stop loss can
// adjust contract to fit like mentor said ... cause some day it go big").
// The kernel only LETS a big stop through to the trader's size table; the
// size cut is the trader's. Course numbers (October wins over September):
//   - "Cái nến INSHABA này tầm 20 điểm, 30 điểm... Tôi vô size 3 hoặc tôi
//     không trade... 12 điểm. Ok, tôi vô size 5" [OctD2.2-R73 @01:12:32];
//   - "40 điểm thì mình làm gì? Mình không trade. ... Trade vẫn được. 1 MNQ"
//     [OctD2.3-R47 @48:12]; why: a 40-pt stop needs a 40 (≥35) target
//     [OctD2.3-R48 @49:38];
//   - "giảm size only chứ không có tăng target" [OctD2.2-R49 @40:47].
// Supersedes for sizing purposes D4.1-R12 [D4.1 p1 @05:41–06:08] "inside bar
// mà nó hai mươi mấy điểm… đừng vô" — owner ordered size-down over refusal.

func ladderISBCandle(rng float64) market.Kline {
	ot := auditMs(2026, 11, 10, 9, 0, 0)
	lo := 29000.0
	return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: lo + 1, High: lo + rng, Low: lo, Close: lo + rng - 1}
}

// TestStopLadderKernelDefaults — the ladder is ON by default with a 40-pt
// wide-stop max [OctD2.3-R47 @48:12]; the pre-ladder ceilings are unchanged (they
// are what the ladder-OFF path still reads).
func TestStopLadderKernelDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.CourseExits.StopSizeLadder {
		t.Fatal("CourseExits.StopSizeLadder default = OFF, want ON (owner order 2026-11-10)")
	}
	if cfg.CourseExits.WideStopMaxPts != 40 {
		t.Fatalf("CourseExits.WideStopMaxPts default = %g, want 40 [OctD2.3-R47 @48:12]", cfg.CourseExits.WideStopMaxPts)
	}
	if cfg.CourseExits.ISBMaxStopPts != 20 || cfg.StopCeilingPts != 25 {
		t.Fatalf("ladder-OFF ceilings moved: ISB %g PHL/BOX %g, want 20/25", cfg.CourseExits.ISBMaxStopPts, cfg.StopCeilingPts)
	}
}

// TestStopLadderISBVerdict runs the production ISB stop gate (ISBStopVerdict,
// which ISBStopLimitOrder and the reverse ISB call) on the course numbers:
// 12 → taken, 25 → taken (sized 3 by the trader), 35 → taken (1 MNQ), 40 →
// taken (at the max, "above" refuses), 45 → refused. Ladder OFF → 25 refused
// with the pre-ladder reason, byte-identical.
// RED before the change: the 25/35/40 stops are refused at the 20-pt ceiling.
func TestStopLadderISBVerdict(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CourseExits.StopSizeLadder = true
	cfg.CourseExits.WideStopMaxPts = 40
	buf2 := 2 * cfg.ISBBufferPts
	for _, tc := range []struct {
		stop  float64
		taken bool
	}{
		{12, true}, // OctD2.2-R73: 12 pts → size 5
		{20, true},
		{25, true}, // OctD2.2-R73: 20–30 → size 3
		{30, true},
		{35, true}, // OctD2.3-R47: → 1 MNQ
		{40, true}, // at the max — only ABOVE is refused
		{45, false},
	} {
		c := ladderISBCandle(tc.stop - buf2)
		stop, ok, why := ISBStopVerdict(c, cfg)
		if stop != tc.stop || ok != tc.taken {
			t.Fatalf("ladder ON: ISB stop %.2f → ok=%v (%s), want stop %.2f ok=%v", stop, ok, why, tc.stop, tc.taken)
		}
		if !ok && (!strings.Contains(why, "mentor_wide_stop_max_pts") || !strings.Contains(why, "OctD2.3-R47")) {
			t.Fatalf("ladder ON refusal must name the knob and the citation, got %q", why)
		}
	}
	// The R1 order itself (the evaluator's call site) emits a 35-pt ISB.
	prev := market.Kline{Open: 28990, High: 29040, Low: 28980, Close: 29030} // green candle 1 → long
	cur := ladderISBCandle(35 - buf2)
	if side, in, ok, why := ISBStopLimitOrder(prev, cur, cfg); !ok || side != SideLong || in.Price-in.Stop != 35 {
		t.Fatalf("ISBStopLimitOrder 35-pt ISB: side %q ok=%v why=%q risk %.2f, want long taken risk 35", side, ok, why, in.Price-in.Stop)
	}

	// Ladder OFF → today's behaviour exactly: 25 refused, the same reason.
	off := cfg
	off.CourseExits.StopSizeLadder = false
	if _, ok, why := ISBStopVerdict(ladderISBCandle(25-buf2), off); ok || why != ISBMaxStopReason {
		t.Fatalf("ladder OFF: 25-pt ISB ok=%v why=%q, want refused with ISBMaxStopReason", ok, why)
	}
	if _, ok, _ := ISBStopVerdict(ladderISBCandle(20-buf2), off); !ok {
		t.Fatal("ladder OFF: 20-pt ISB refused, want taken (unchanged)")
	}
	// A stored mentor_isb_max_pts ABOVE the wide max is never narrowed.
	wide := cfg
	wide.CourseExits.ISBMaxStopPts = 50
	if _, ok, why := ISBStopVerdict(ladderISBCandle(45-buf2), wide); !ok {
		t.Fatalf("stored isb_max 50 + ladder ON: 45-pt ISB refused (%s), want taken (never narrowed)", why)
	}
}

// TestStopLadderPHLCeiling runs the production PHL/PLH builder (PHLPLH →
// phlPLHR2, the rolling ladder's and the level PHL's core): a 28-pt PHL is
// taken with the ladder ON (the trader sizes it 3), a 45-pt one refused; OFF
// refuses 28 with the pre-ladder 25-pt reason.
// RED before the change: 28 refused at the 25-pt ceiling.
func TestStopLadderPHLCeiling(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.CourseExits.StopSizeLadder = true
	cfg.CourseExits.WideStopMaxPts = 40
	// risk = range + entry buffer + stop buffer (m472 A3: +2.0 total).
	pad := cfg.PHLEntryBufferPts + cfg.PHLStopBufferPts
	touch := func(risk float64) Touch {
		return Touch{Outcome: TouchReject, RefBar: market.Kline{High: 100 + risk - pad, Low: 100}, ApproachedFrom: SideLong}
	}
	in, ok, why := PHLPLH(touch(28), Level{Kind: KindOldExtreme, Price: 300}, 0, 3, cfg)
	if !ok {
		t.Fatalf("ladder ON: 28-pt PHL refused (%s), want taken", why)
	}
	if r := in.Price - in.Stop; r < 27.99 || r > 28.01 {
		t.Fatalf("fixture: PHL risk %.2f, want 28", r)
	}
	if _, ok, why := PHLPLH(touch(45), Level{Kind: KindOldExtreme, Price: 400}, 0, 3, cfg); ok || !strings.Contains(why, "mentor_wide_stop_max_pts") {
		t.Fatalf("ladder ON: 45-pt PHL ok=%v why=%q, want refused naming mentor_wide_stop_max_pts", ok, why)
	}
	off := cfg
	off.CourseExits.StopSizeLadder = false
	if _, ok, why := PHLPLH(touch(28), Level{Kind: KindOldExtreme, Price: 300}, 0, 3, off); ok || why != "stop over the 25-pt ceiling — not worth trading [D3.3 p1 @ 02:04]" {
		t.Fatalf("ladder OFF: 28-pt PHL ok=%v why=%q, want the pre-ladder 25-pt refusal", ok, why)
	}
}

// TestStopLadderBoxCeiling runs the production box entry (boxEntryIntentWhy):
// a 38-pt box stop is emitted with the ladder ON (the trader sizes it 1 MNQ),
// 45 is refused box_stop_ceiling; OFF refuses 38 box_stop_ceiling as today.
// RED before the change: 38 refused at the 25-pt ceiling.
func TestStopLadderBoxCeiling(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0.05
	cfg.CourseExits.StopSizeLadder = true
	cfg.CourseExits.WideStopMaxPts = 40
	floor := Box{Kind: FTGL, Top: 100, Bottom: 50, Key: "ftgl:100.00:50.00"}
	far := []Level{{Key: "k", Kind: KindKeyLevel, Price: 400}}
	buy := TriggerLine{Dir: SideLong, Price: 60}
	ref := func(risk float64) market.Kline { return market.Kline{High: 101, Low: 101 - risk, Close: 101} }
	out, why := boxEntryIntentWhy(ref(38), floor, []Box{floor}, far, buy, nil, cfg)
	if why != "" || len(out) != 1 {
		t.Fatalf("ladder ON: 38-pt box got %d intents why=%q, want 1 intent", len(out), why)
	}
	if r := out[0].Price - out[0].Stop; r != 38 {
		t.Fatalf("box risk %.2f, want 38", r)
	}
	if _, why := boxEntryIntentWhy(ref(45), floor, []Box{floor}, far, buy, nil, cfg); why != "box_stop_ceiling" {
		t.Fatalf("ladder ON: 45-pt box why=%q, want box_stop_ceiling", why)
	}
	off := cfg
	off.CourseExits.StopSizeLadder = false
	if _, why := boxEntryIntentWhy(ref(38), floor, []Box{floor}, far, buy, nil, off); why != "box_stop_ceiling" {
		t.Fatalf("ladder OFF: 38-pt box why=%q, want box_stop_ceiling (today)", why)
	}
}

// TestStopLadderM464WorkedBuyRungArms — the D2.2 worked buy with the ladder
// ON (production default). The 10:13 rung (risk 29) is no longer stopped by
// the old 25-pt ceiling — it now meets the 1:1 floor instead: its target
// (old high 29,433.25 − shy 5 = 29,428.25) is ~24 pts away, under the 29-pt
// risk, and the ladder NEVER enlarges a target to fit a big stop ("giảm size
// only chứ không có tăng target" [OctD2.2-R49 @40:47]; "40 điểm em cũng phải
// kiếm cái target 40 điểm" [OctD2.3-R48 @49:38]). So it is refused by NAME
// (rolling_phl_target_below_floor), never rolling_phl_stop_ceiling, the 10:12
// rung is cancelled, and the 10:14 green signal candle is still the last rung
// at the course price 29,396.75 / 29,385.50 — the course outcome is unchanged.
func TestStopLadderM464WorkedBuyRungArms(t *testing.T) {
	bars := m464CourseTape()
	e := m464Evaluator(SideLong, Level{Key: "old-high:29433.25", Kind: KindOldExtreme, Role: OldHigh, Price: 29433.25}, bars[m464Idx(10, 7)].OpenTime)
	if !e.Cfg.CourseExits.StopSizeLadder {
		t.Fatal("fixture: the production default must run the ladder")
	}
	steps := m464Run(e, bars, m464Idx(10, 7), m464Idx(10, 12))
	floorBefore := e.State.Refusals["rolling_phl_target_below_floor"]
	steps = append(steps, m464Run(e, bars, m464Idx(10, 13), m464Idx(10, 14))...)
	var s1013, last m464Step
	for _, s := range steps {
		if s.at == "10:13" {
			s1013 = s
		}
	}
	last = steps[len(steps)-1]
	if len(s1013.places) != 0 || len(s1013.cancels) != 1 {
		t.Fatalf("10:13: places %+v cancels %+v, want the 29-pt rung refused (1:1 floor) and the 10:12 rung cancelled", s1013.places, s1013.cancels)
	}
	if got := e.State.Refusals["rolling_phl_stop_ceiling"]; got != 0 {
		t.Fatalf("ladder ON: the 29-pt rung must not be refused at the stop ceiling, ledger=%v", e.State.Refusals)
	}
	if e.State.Refusals["rolling_phl_target_below_floor"] != floorBefore+1 {
		t.Fatalf("ladder ON: the 29-pt rung must be refused by the 1:1 floor (never a bigger target), ledger=%v", e.State.Refusals)
	}
	if last.at != "10:14" || len(last.places) != 1 {
		t.Fatalf("the 10:14 green signal candle must be the last rung, steps=%+v", steps)
	}
	if in := last.places[0]; in.Setup != "PHL" || !m464Approx(in.Price, 29396.75) || !m464Approx(in.Stop, 29385.50) || !m464Approx(in.Target, 29428.25) {
		t.Fatalf("last rung %s %.2f / %.2f → %.2f, want PHL 29,396.75 / 29,385.50 → 29,428.25 (unchanged by the ladder)", in.Setup, in.Price, in.Stop, in.Target)
	}
}
