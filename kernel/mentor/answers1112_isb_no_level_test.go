package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// answers-1112 Q3 (verdict corrected, confidence medium): an ISB with NO level
// beyond it is TRADED; its first target is the close of the candle that fills
// the order, whatever that candle does [Oct D2.2 @28:37-28:56 "target đầu tiên
// của mình mà mình phải bán bớt … là khi cây nến trend phiêu lĩnh của mình …
// Đóng"; @30:24-30:43 "Target của Insha Bar là khi nến trend kế tiếp … Đóng
// Thì mình sẽ bán bớt Không quan tâm … nó làm gì hết"]. The knob
// isb_no_level_fill_close (default ON) carries ONLY that case; a level inside
// 1R (isb_target_below_floor) is not covered by any video and stays with
// isb_target_fill_close (default OFF).
//
// The worked example is the Oct D2.2 live chart (Fri 30 Oct ~08:37, frame
// s_02392): a short ISB, "điểm entry … ở đấy cái nến NC3 … Stop Lock … ở đỉnh
// cái nến NC3" [@39:00-39:19]; the frame's measure tool reads 26.25 pts / 105
// ticks between the 28,709.50 entry line and the 28,735.75 top; "Stop Loss của
// anh chị là 27 điểm. Stop Gain … đợi 27 điểm, anh chị bán … Hoặc ngay tới đây,
// anh chị bán bớt, vẫn được" [@39:47-39:59]. The bot's ISB buffer is 1.5 pts
// each side, so the inside bar below is drawn 1.5 pts inside those lines: the
// emitted intent is exactly entry 28,709.50 / stop 28,735.75. The mother bar,
// the ORB and the trigger lines are fixture scaffolding [C].
const (
	q3Entry = 28709.50
	q3Stop  = 28735.75
	q3Risk  = 26.25 // s_02392 measure "26.25 (0.09%) 105"
)

// q3Run is the production Tick on the s_02392 short ISB. level4h > 0 puts the
// 4h trigger line (a target-only level) there; ema34 > 0 puts the EMA34 there.
func q3Run(t *testing.T, ema34, level4h float64, mut func(*Config)) (*Evaluator, []Intent) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	// the target-rule fixture exercises this stage only: no later-lesson 4h/1h gate.
	cfg.HTFGateNewsOnly = true
	if mut != nil {
		mut(&cfg)
	}
	e := New(cfg)
	e.State.Trigger = TriggerLine{Dir: SideShort, Price: 28800}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideShort, Price: 28800}}
	if level4h > 0 {
		e.State.HTF.FourH.Price = level4h
	}
	bars := []market.Kline{
		rthBars(1, 28748, 28750, 28700, 28740),            // mother bar (red)
		rthBars(2, 28730, q3Stop-1.5, q3Entry+1.5, 28725), // NC3, the inside bar (red)
	}
	now := bars[1].CloseTime + 1
	// ORB above the trade: no ORB edge below the short entry.
	e.State.ORB = ORB{Day: dayStartCT(now), High: 28900, Low: 28850, Drawn: true, Escaped: SideShort}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA34 = ema34
	e.State.EMA9 = 0
	return e, e.Tick(bars, now)
}

func TestAnswers1112ISBNoLevelDefaults(t *testing.T) {
	c := DefaultConfig()
	if !c.ISBNoLevelFillClose {
		t.Error("ISBNoLevelFillClose must default ON (answers-1112 Q3: Oct D2.2 @28:37-28:56, @30:24-30:43 — the ISB's first target is the fill candle's close; no video refuses a no-level ISB)")
	}
	if c.ISBTargetFillClose {
		t.Error("ISBTargetFillClose must stay default OFF: its remaining job is the level-inside-1R case, which no video covers [B]")
	}
}

// The video's ISB geometry (s_02392 entry/stop) with NO level below it placed
// in the test — on his chart a recent low does stand below the entry, and the
// 26.25-pt stop is over the trader's 20-pt mentor_isb_max_pts, so end to end
// this exact trade is refused (review P3); the kernel case is the no-level
// branch on his numbers, not a replay of his trade. Default → traded; the first
// target is the +1R leg-1 take-profit the trader owns ("đợi 27 điểm, anh chị
// bán"), pulled in to the fill candle's close by mentorISBPartialTP ("Hoặc ngay
// tới đây, anh chị bán bớt"; trader TestAnswers1112ISBNoLevelPartialAtFillClose).
func TestAnswers1112ISBNoLevelTradedAtDefault(t *testing.T) {
	e, ins := q3Run(t, 0, 0, nil)
	in := d2g3Entry(ins)
	if in == nil {
		t.Fatalf("Oct D2.2 @28:37-31:33: an ISB with no level beyond is traded; got no entry, refusals %v", e.State.Refusals)
	}
	if in.Side != SideShort || in.Price != q3Entry || in.Stop != q3Stop {
		t.Fatalf("intent %s entry %.2f stop %.2f, want short %.2f / %.2f (s_02392)", in.Side, in.Price, in.Stop, q3Entry, q3Stop)
	}
	if want := q3Entry - q3Risk; in.Target != want {
		t.Fatalf("first target = %.2f, want the +1R leg-1 TP %.2f (\"đợi 27 điểm, anh chị bán\" @39:47-39:55)", in.Target, want)
	}
	if in.StopPts != q3Risk {
		t.Fatalf("StopPts = %.2f, want %.2f (s_02392 measure)", in.StopPts, q3Risk)
	}
	if n := e.State.Refusals["isb_missing_target"]; n != 0 {
		t.Fatalf("isb_missing_target counted %d at default", n)
	}
}

// Revert: isb_no_level_fill_close=false is today's refusal exactly.
func TestAnswers1112ISBNoLevelKnobOffIsToday(t *testing.T) {
	e, ins := q3Run(t, 0, 0, func(c *Config) { c.ISBNoLevelFillClose = false })
	if in := d2g3Entry(ins); in != nil {
		t.Fatalf("knob off must refuse as today; got intent %+v", *in)
	}
	if e.State.Refusals["isb_missing_target"] != 1 || e.State.Refusals["isb_target_below_floor"] != 0 {
		t.Fatalf("knob off: want isb_missing_target=1 (today), ledger %v", e.State.Refusals)
	}
	// The old combined knob still lifts it (unchanged meaning).
	_, ins = q3Run(t, 0, 0, func(c *Config) { c.ISBNoLevelFillClose = false; c.ISBTargetFillClose = true })
	if d2g3Entry(ins) == nil {
		t.Fatal("isb_target_fill_close=true must still trade the no-level ISB")
	}
}

// A level inside 1R (the EMA34 14.5 pts below a 26.25-pt risk): no video covers
// it, so the defaults keep refusing it isb_target_below_floor; only
// isb_target_fill_close=true lifts it, as before.
func TestAnswers1112ISBLevelInside1RStillRefused(t *testing.T) {
	const ema = q3Entry - 14.5
	for _, knob := range []bool{true, false} {
		e, ins := q3Run(t, ema, 0, func(c *Config) { c.ISBNoLevelFillClose = knob })
		if in := d2g3Entry(ins); in != nil {
			t.Fatalf("isb_no_level_fill_close=%v: a level inside 1R must stay refused; got intent %+v", knob, *in)
		}
		if e.State.Refusals["isb_target_below_floor"] != 1 {
			t.Fatalf("isb_no_level_fill_close=%v: want isb_target_below_floor=1, ledger %v", knob, e.State.Refusals)
		}
	}
	_, ins := q3Run(t, ema, 0, func(c *Config) { c.ISBTargetFillClose = true })
	if d2g3Entry(ins) == nil {
		t.Fatal("isb_target_fill_close=true must still lift the level-inside-1R case")
	}
}

// A target-only level (the 4h trigger line, 1.5R below) is skipped by the
// target ladder for room, so nextLevelBeyondRoom returns no target — but a
// level DOES stand ahead. That is not the video's case: the default keeps
// today's isb_missing_target refusal there [B].
func TestAnswers1112ISBSkippedTargetOnlyLevelIsNotNoLevel(t *testing.T) {
	const line4h = q3Entry - 1.5*q3Risk
	e, ins := q3Run(t, 0, line4h, nil)
	if in := d2g3Entry(ins); in != nil {
		t.Fatalf("a 4h trigger line 1.5R ahead is a level ahead; the no-level knob must not trade it, got %+v", *in)
	}
	if e.State.Refusals["isb_missing_target"] != 1 {
		t.Fatalf("want today's isb_missing_target=1, ledger %v", e.State.Refusals)
	}
}
