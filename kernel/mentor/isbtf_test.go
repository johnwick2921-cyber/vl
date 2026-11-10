package mentor

import (
	"strings"
	"testing"

	"vl/market"
)

// TestISBTFF2CrossingNeverLiftsABoxGate — isbTF-F2: the ONE helper every ISB
// and reverse-ISB box gate (5m, 15m, 30m, the CE-1 15m rule) reads. D3.4-R43
// [D3.4 p2 @17:07–17:49] escalates only the TRIGGER; the box gates stand
// [OctD4.3-R70 @ 68:24, R61 @ 59:56, R59 @ 58:00]. Fed crossing buckets (the
// m60 crossing tape), it lifts nothing on any timeframe, in any 5m box mode.
// Mutant (the pre-F2 seam): the 15m/30m gates and the 5m gate outside merge
// are lifted again → RED.
func TestISBTFF2CrossingNeverLiftsABoxGate(t *testing.T) {
	bars := m60p1Crossing()
	cb := closedBuckets(bars, BarCloseInstant(bars[len(bars)-1]), DefaultConfig())
	if !crossingISBs(cb, 3) {
		t.Fatal("precondition: the buckets must hold crossing ISBs")
	}
	for _, mode := range []string{FiveMinBoxMerge, FiveMinBoxNewest, FiveMinBoxFirst} {
		e := New(DefaultConfig())
		e.Cfg.FiveMinBoxSource = mode
		for _, tf := range []int{5, 15, 30} {
			if e.boxGateLiftedByCrossing(cb, tf) {
				t.Errorf("mode %s: crossing ISBs lifted the %dm box gate — escalation changes the trigger only (D3.4-R43)", mode, tf)
			}
		}
	}
	prev := isbBoxGateLiftedByCrossingSeam
	isbBoxGateLiftedByCrossingSeam = true
	defer func() { isbBoxGateLiftedByCrossingSeam = prev }()
	e := New(DefaultConfig())
	e.Cfg.FiveMinBoxSource = FiveMinBoxNewest
	if !e.boxGateLiftedByCrossing(cb, 15) || !e.boxGateLiftedByCrossing(cb, 5) {
		t.Fatal("pre-F2 seam: the crossing read lifted the 15m gate and (outside merge) the 5m gate")
	}
	e.Cfg.FiveMinBoxSource = FiveMinBoxMerge
	if e.boxGateLiftedByCrossing(cb, 5) {
		t.Fatal("pre-F2 seam: mode merge never lifted the 5m gate (ISB5-F2)")
	}
}

// isbTFSwingTape drives the PRODUCTION Tick over the real 10-28 MNQ 12-26 1m
// tape (testdata/m463_mnq_1m_20261028_0800ct.csv, extracted read-only from
// data.db) with the swing seeded WARM at the 17:00 CT bucket at the live line
// (m463Line1700 = 29,645.53, the value the live arms 469-478 carried), from
// 16:00 CT to endCT. It returns, per bar (CT "HH:MM"), the refusal deltas and
// the swing state after the tick.
type isbTFSwingTick struct {
	ref     map[string]int
	fired   bool
	pending bool
	ins     []Intent
}

func isbTFSwingTape(t *testing.T, end int64) (*Evaluator, map[string]isbTFSwingTick) {
	t.Helper()
	bars := m413Load(t, "testdata/m463_mnq_1m_20261028_0800ct.csv")
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	e.State.Swing = SwingState{Line: m463Line1700, BucketStart: m463Bucket1700, EmaCount: FourHEMA34Min, LastBarTime: m463Bucket1700 - 65*60_000}
	out := map[string]isbTFSwingTick{}
	for i := range bars {
		if bars[i].OpenTime < m463Bucket1700-60*60_000 || bars[i].OpenTime > end {
			continue
		}
		before := map[string]int{}
		for k, v := range e.State.Refusals {
			before[k] = v
		}
		ins := e.Tick(bars[:i+1], bars[i].CloseTime+1)
		tk := isbTFSwingTick{ref: map[string]int{}, ins: ins, pending: e.State.Swing.Pending != nil}
		tk.fired = e.State.Swing.FirstTouch != nil && e.State.Swing.FirstTouch.ISBFired
		for k, v := range e.State.Refusals {
			if d := v - before[k]; d != 0 {
				tk.ref[k] = d
			}
		}
		out[m463CT(bars[i].OpenTime)[6:]] = tk
	}
	return e, out
}

// TestISBTFF3DroppedSwingISBDoesNotSpendTheLevel — isbTF-F3 on the course
// example days (10-28 evening, 4h line 29,645.53). The 17:50 5m candle is an
// inside bar at the invalid line; its swing ISB is BUILT at the 17:54 tick
// (ISBFired latched) and dropped by the box ban (inside_any_box) before it was
// ever authored. Only a FILL spends the level (the swing's own rule, rv59; "xuất
// hiện inside bar, tôi vô lệnh ... cứ đánh thôi", Sept D5.2-R25 [D5.2 p2
// @10:23–10:51] — no time limit). Before: the latch stayed, and every later
// inside bar of the 17:00 4h candle (18:14, 18:24, 18:34, ...) was refused
// swing_isb_spent "already filled". Mutant: the pre-F3 seam → RED.
func TestISBTFF3DroppedSwingISBDoesNotSpendTheLevel(t *testing.T) {
	_, got := isbTFSwingTape(t, m463Bucket1700+4*3600_000-60_000)
	tk := got["17:54"]
	if tk.ref["inside_any_box"] == 0 {
		t.Fatalf("precondition: the 17:50 inside bar's swing ISB is dropped by the box ban at the 17:54 tick, got %v", tk.ref)
	}
	if tk.fired || tk.pending {
		t.Errorf("17:54: the dropped (never authored) inside-bar order must not spend the level: ISBFired=%v pending=%v", tk.fired, tk.pending)
	}
	spent := 0
	for hm, tk := range got {
		if n := tk.ref[SwingRefusedISBSpent]; n > 0 {
			spent += n
			t.Errorf("%s: refused %s — nothing of this level ever filled in the 17:00 4h candle", hm, SwingRefusedISBSpent)
		}
	}
	if spent != 0 {
		t.Fatalf("%d false swing_isb_spent refusals", spent)
	}
	prev := isbSwingDropSpendsLevelSeam
	isbSwingDropSpendsLevelSeam = true
	defer func() { isbSwingDropSpendsLevelSeam = prev }()
	if _, old := isbTFSwingTape(t, m463Bucket1700+4*3600_000-60_000); !old["17:54"].fired || old["18:14"].ref[SwingRefusedISBSpent] == 0 {
		t.Fatalf("pre-F3 seam: the dropped order latched the level and 18:14 was refused spent, got 17:54 %+v / 18:14 %+v", old["17:54"], old["18:14"])
	}
}

// TestISBTFF3TraderRefusalDoesNotSpendTheLevel — the trader-side drop
// (mentorDropEvalArm → Evaluator.DropArm, a placement the trader refused) runs
// the same unregisterArm: on the same tape the 18:50 inside bar arms (after
// F3) at the 18:54 tick; a trader refusal of that order leaves the level
// unspent, so the next inside bar may enter (D5.2-R25).
func TestISBTFF3TraderRefusalDoesNotSpendTheLevel(t *testing.T) {
	e, got := isbTFSwingTape(t, m463Bucket1700+114*60_000) // to the 18:54 bar
	var arm string
	for _, in := range got["18:54"].ins {
		if in.Setup == "SWING4H" && in.Action == PlaceStopEntry {
			arm = in.ArmID
		}
	}
	if arm == "" || !got["18:54"].fired || !got["18:54"].pending {
		t.Fatalf("precondition: the 18:50 inside bar arms a swing ISB at the 18:54 tick, got %+v", got["18:54"])
	}
	e.DropArm(arm)
	if ft := e.State.Swing.FirstTouch; ft == nil || ft.ISBFired || e.State.Swing.Pending != nil {
		t.Fatalf("a trader-refused (never authored) inside-bar order must not spend the level: %+v pending %+v", ft, e.State.Swing.Pending)
	}
}

// isbTFLine0900_1110 is the live swing line of the 11-10 09:00 CT 4h candle
// (armed_orders 522/523 mentor_snapshot_json swing.line, read-only).
const (
	isbTFLine0900_1110   = 27919.986668976147
	isbTFBucket0900_1110 = int64(1794322800000) // 2026-11-10 09:00 CT
)

// isbTFSwing1110 drives the PRODUCTION Tick over the real 11-10 MNQ 12-26 1m
// tape 01:00–13:10 CT (testdata/isbtf_mnq_1m_20261110_0100ct.csv, read-only
// from data.db), the swing seeded WARM at the 09:00 4h candle at the live line.
// It emulates the trader's end receipt (SwingEnded) when a filled swing hits
// its stop or target, as the m463 replay does.
func isbTFSwing1110(t *testing.T) (*Evaluator, map[string]isbTFSwingTick, []string) {
	t.Helper()
	bars := m413Load(t, "testdata/isbtf_mnq_1m_20261110_0100ct.csv")
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	e.State.Swing = SwingState{Line: isbTFLine0900_1110, BucketStart: isbTFBucket0900_1110, EmaCount: FourHEMA34Min, LastBarTime: isbTFBucket0900_1110 - 5*60_000}
	out := map[string]isbTFSwingTick{}
	var order []string
	for i := range bars {
		if bars[i].OpenTime < isbTFBucket0900_1110 {
			continue
		}
		if p := e.State.Swing.Pos; p != nil {
			b := bars[i]
			if p.Side == SideLong && (b.Low <= p.Stop || p.Target > 0 && b.High >= p.Target) ||
				p.Side == SideShort && (b.High >= p.Stop || p.Target > 0 && b.Low <= p.Target) {
				e.SwingEnded(p.ArmID)
			}
		}
		before := map[string]int{}
		for k, v := range e.State.Refusals {
			before[k] = v
		}
		ins := e.Tick(bars[:i+1], bars[i].CloseTime+1)
		tk := isbTFSwingTick{ref: map[string]int{}, ins: ins, pending: e.State.Swing.Pending != nil}
		tk.fired = e.State.Swing.FirstTouch != nil && e.State.Swing.FirstTouch.ISBFired
		for k, v := range e.State.Refusals {
			if d := v - before[k]; d != 0 {
				tk.ref[k] = d
			}
		}
		hm := m463CT(bars[i].OpenTime)[6:]
		out[hm] = tk
		order = append(order, hm)
	}
	return e, out, order
}

// TestISBTFF4FarInsideBarsAreNotNamedSpent — isbTF-F4 on today's tape (Tue
// 2026-11-10, the 09:00 CT 4h candle, live line 27,919.99). The 09:25 5m candle
// is the swing ISB: armed at the 09:29 tick (buy stop 27,961, stop AT the line,
// R29) — the live bot armed it at 09:30, filled 09:32:13. Every later 5m inside
// bar of that 4h candle (09:55 ... 12:55, 25 bars) sits 66–339 pts BELOW the
// long line: its buy stop would rest beyond the line, so it is not a setup at
// this level at all (G63, swing_isb_entry_beyond_line [D5.2-R26/R29 p3
// @07:42–08:36]). Before: each was refused swing_isb_spent "inside bar at the
// invalid 4h line ... already filled" — the live log's 25 refusals per trader.
// The refusal names the bar's distance from the line.
func TestISBTFF4FarInsideBarsAreNotNamedSpent(t *testing.T) {
	_, got, order := isbTFSwing1110(t)
	armed := false
	for _, in := range got["09:29"].ins {
		if in.Setup == "SWING4H" && in.Action == PlaceStopEntry && in.Side == SideLong && in.Price == 27_961 && in.Stop == isbTFLine0900_1110 {
			armed = true
		}
	}
	if !armed {
		t.Fatalf("precondition: the 09:25 inside bar arms LONG 27,961 stop AT the line at the 09:29 tick, got %+v", got["09:29"].ins)
	}
	spent, beyond := 0, 0
	for _, hm := range order {
		spent += got[hm].ref[SwingRefusedISBSpent]
		beyond += got[hm].ref[SwingRefusedISBEntryBeyondLine]
		if got[hm].ref[SwingRefusedISBSpent] > 0 {
			t.Errorf("%s: refused %s — the inside bar's buy stop sits below the long line, it is not a setup at this level", hm, SwingRefusedISBSpent)
		}
	}
	if spent != 0 || beyond != 25 {
		t.Fatalf("want 0 swing_isb_spent and the 25 inside bars named swing_isb_entry_beyond_line, got spent=%d beyond=%d", spent, beyond)
	}
}

// TestISBTFF4OnePerCandleKnob — isbTF-F4: the "one filled inside-bar trade per
// level per 4h candle" limit is the bot's [B] reading, now a knob
// (mentor_swing_isb_one_per_candle, kernel SwingCfg.ISBRepeatAfterFill
// inverted). Default ON = today: an ON-side inside bar after the level's
// inside-bar order filled and ended is refused swing_isb_spent, and the text
// names the distance and the knob. OFF: the same bar arms (D5.2-R25, no time
// limit). Bars: the m61 Example G shape (line 29,405.25, long).
func TestISBTFF4OnePerCandleKnob(t *testing.T) {
	const line = 29405.25
	bars := []market.Kline{
		mk5m(t, 11, 11, 40, 29430, 29440, 29380, 29393.75),
		mk5m(t, 11, 11, 45, 29395, 29424.25, 29388, 29397.25), // buy stop 29,424.25 above the line
	}
	now := bars[1].OpenTime + 5*60_000
	if DefaultSwingCfg().ISBRepeatAfterFill {
		t.Fatal("the default must be today's one-per-candle read")
	}
	s := m61InvalidLevel(t, line, SideLong, bars[0])
	s.FirstTouch.ISBFired = true // the level's inside-bar order filled; the leg ended
	if es := m463Entries(s.stepSwingBar(bars, 1, line, DefaultSwingCfg(), now, swingBarLive)); len(es) != 0 {
		t.Fatalf("default: the level gave its trade, got %+v", es)
	}
	if n := m463Refused(s, SwingRefusedISBSpent); n != 1 {
		t.Fatalf("default: NAMED %s once, got %d (%+v)", SwingRefusedISBSpent, n, s.refused)
	}
	if why := s.refused[0].Why; !strings.Contains(why, "19.00 pts from the invalid 4h line") || !strings.Contains(why, "mentor_swing_isb_one_per_candle") || strings.Contains(why, "inside bar at the invalid 4h line") {
		t.Fatalf("the refusal must name the distance and the knob, never claim the bar is at the line: %q", why)
	}
	off := DefaultSwingCfg()
	off.ISBRepeatAfterFill = true
	s = m61InvalidLevel(t, line, SideLong, bars[0])
	s.FirstTouch.ISBFired = true
	es := m463Entries(s.stepSwingBar(bars, 1, line, off, now, swingBarLive))
	if len(es) != 1 || es[0].Side != SideLong || es[0].Price != 29424.25 || es[0].Stop != line {
		t.Fatalf("knob OFF: the inside bar enters again (D5.2-R25), got %+v (refused %+v)", es, s.refused)
	}
}

// TestISBTFF6CrossingKnobs — isbTF-F6: the crossing read that escalates the
// trigger (D3.4-R43 [D3.4 p2 @17:07–17:49]) is a [B] reading — the course
// states no window or count — so it ships as knobs at today's values. Default
// (3 / 2) is exactly the pre-F6 read on the m60 crossing tape (governing TF
// 15); a stricter count the tape cannot meet (lookback 4, min 3) keeps the 5m.
func TestISBTFF6CrossingKnobs(t *testing.T) {
	bars := m60p1Crossing()
	cb := closedBuckets(bars, BarCloseInstant(bars[len(bars)-1]), DefaultConfig())
	d := DefaultConfig()
	if d.CrossingISBLookback != 3 || d.CrossingISBMin != 2 {
		t.Fatalf("defaults must be today's 3 / 2, got %d / %d", d.CrossingISBLookback, d.CrossingISBMin)
	}
	if crossingISBsCfg(d, cb) != crossingISBs(cb, 3) || !crossingISBsCfg(d, cb) {
		t.Fatal("the default knob read must equal the pre-F6 crossingISBs(…, 3) on the crossing tape")
	}
	var zero Config // unset (0) = today's 3 / 2
	if !crossingISBsCfg(zero, cb) {
		t.Fatal("0 / 0 must read as 3 / 2")
	}
	if _, tf := governingTrigger(d, TriggerLine{}, TriggerLine{}, TriggerLine{}, cb, nil); tf != 15 {
		t.Fatalf("default: crossing 5m ISBs escalate to the 15m, got %dm", tf)
	}
	strict := d
	strict.CrossingISBLookback, strict.CrossingISBMin = 4, 3
	if crossingISBsCfg(strict, cb) {
		t.Fatal("lookback 4 / min 3: the tape holds only 2 inside-bar pairs — no crossing")
	}
	if _, tf := governingTrigger(strict, TriggerLine{}, TriggerLine{}, TriggerLine{}, cb, nil); tf != 5 {
		t.Fatalf("stricter knob: the 5m keeps governing, got %dm", tf)
	}
}
