package mentor

import "testing"

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
