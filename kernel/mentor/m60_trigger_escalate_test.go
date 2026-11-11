package mentor

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// m465 P1 (release #60) — D3.4-R43: "inside bars cross back and forth so the
// 5m buy/sell trigger is not valid → go to the 15m; if that fails 30m; then
// 1h, 4h" [D3.4 p2 @17:07–17:49]. The whipsaw read is the production
// crossingISBs(…, 3) the box ladder already uses (no new number); the 1h/4h
// rungs already gate through the HTF check.

// m60p1Crossing is a tape whose last three closed 5m candles hold a LONG then a
// SHORT inside bar: 09:30 green (big), 09:35 red inside it (ISB long), 09:40
// green inside 09:35 (ISB short). Then the 1m pair 09:45 red / 09:46 inside —
// a 1m SHORT ISB.
func m60p1Crossing() []market.Kline {
	bars := m60p1Tape([][6]float64{
		{9, 0, 28_900, 28_915, 28_895, 28_912},
		{9, 5, 28_912, 28_928, 28_908, 28_925},
		{9, 10, 28_925, 28_945, 28_920, 28_940},
		{9, 15, 28_950, 28_958, 28_940, 28_955},
		{9, 20, 28_955, 28_966, 28_950, 28_962},
		{9, 25, 28_962, 28_975, 28_958, 28_970},
		{9, 30, 28_972, 29_030, 28_965, 29_025}, // green
		{9, 35, 29_020, 29_028, 28_980, 28_990}, // red, inside → 5m ISB LONG
		{9, 40, 28_995, 29_010, 28_985, 29_005}, // green, inside 09:35 → 5m ISB SHORT
	})
	return append(bars,
		m60p1K(m60p1At(9, 45), 29_004, 29_008, 28_996, 28_998),   // red mother
		m60p1K(m60p1At(9, 46), 28_999, 29_000, 28_998, 28_999.5), // inside → 1m ISB short
	)
}

// m60p1CrossingEval: the 5m trigger is SHORT (it agrees with the short ISB), the
// 4h/1h are short, and the 15m trigger is LONG at `px15` (frozen).
func m60p1CrossingEval(t *testing.T, px15 float64) *Evaluator {
	t.Helper()
	e := m60p1Eval(t, SideShort, 29_040, SideShort, SideShort, m60p1Levels())
	e.Cfg.TriggerEscalation = true // G3 oct round 2: the default is OFF; these pins exercise the September escalation
	e.State.Trigger15 = &TriggerLine{Dir: SideLong, Price: px15, LastBucket: math.MaxInt64}
	e.State.Trigger30 = &TriggerLine{Dir: SideShort, Price: 29_100, LastBucket: math.MaxInt64}
	// The escalation ladder pins were written against the #61–#66 newest box
	// (the 09:40 short pair replaces the 09:35 long box). Under the ISB5 merge
	// default the merged LONG box gates the short ISB first (isb_box_blocked,
	// TestISB5F2CrossingDoesNotLiftTheMergedBoxGate).
	e.Cfg.FiveMinBoxSource = FiveMinBoxNewest
	return e
}

// TestM60TriggerEscalatesOnCrossingISBs — D3.4-R43: with crossing 5m ISBs the
// 5m trigger is not valid and the 15m trigger governs. 15m buy line BELOW price
// → a 1m SHORT ISB is the wrong side: isb_trigger_side_mismatch_15m; 15m buy
// line ABOVE price → isb_trigger_side_15m. Mutant: mentor trigger_escalation
// OFF → today's verdict (the standing 5m box hides the trigger and the crossing
// skips the box gate, so the short ISB trades).
func TestM60TriggerEscalatesOnCrossingISBs(t *testing.T) {
	bars := m60p1Crossing()

	e := m60p1CrossingEval(t, 28_950)
	ins := m60p1Run(e, bars)
	if !crossingISBs(closedBuckets(bars, BarCloseInstant(bars[len(bars)-1]), e.Cfg), 3) {
		t.Fatal("precondition: the last three closed 5m candles must hold crossing ISBs")
	}
	if in := m60p1ISB(ins, SideShort); in != nil {
		t.Fatalf("the 15m trigger is LONG: a 1m short ISB must be refused, got %+v", *in)
	}
	if e.State.Refusals["isb_trigger_side_mismatch_15m"] == 0 {
		t.Fatalf("want isb_trigger_side_mismatch_15m, refusals %v", e.State.Refusals)
	}
	if e.State.Counters["trigger_escalated_15m"] == 0 {
		t.Fatalf("the escalation must be counted (trigger_escalated_15m), counters %v", e.State.Counters)
	}

	e = m60p1CrossingEval(t, 29_020) // buy line above price → do nothing
	ins = m60p1Run(e, bars)
	if in := m60p1ISB(ins, SideShort); in != nil {
		t.Fatalf("below the 15m buy line nothing trades, got %+v", *in)
	}
	if e.State.Refusals["isb_trigger_side_15m"] == 0 {
		t.Fatalf("want isb_trigger_side_15m, refusals %v", e.State.Refusals)
	}

	// Knob OFF → the pre-#60 verdict: the short ISB trades.
	e = m60p1CrossingEval(t, 28_950)
	e.Cfg.TriggerEscalation = false
	ins = m60p1Run(e, bars)
	if in := m60p1ISB(ins, SideShort); in == nil {
		t.Fatalf("trigger_escalation OFF: today's verdict lets the short ISB trade; last-tick refusals %v", m60p1LastDelta)
	}
	for _, k := range []string{"isb_trigger_side_15m", "isb_trigger_side_mismatch_15m", "isb_trigger_side_30m", "isb_trigger_side_mismatch_30m"} {
		if e.State.Refusals[k] != 0 {
			t.Fatalf("trigger_escalation OFF must not refuse under an escalated name: %s (refusals %v)", k, e.State.Refusals)
		}
	}
	if e.State.Counters["trigger_escalated_15m"] != 0 {
		t.Fatalf("trigger_escalation OFF must not count an escalation, counters %v", e.State.Counters)
	}
}

// TestM60GoverningTriggerLadder — the ladder itself: 5m when the 5m reads
// cleanly; 15m when the 5m ISBs cross; 30m when the 15m ISBs cross too; the
// 5m always with the knob off.
func TestM60GoverningTriggerLadder(t *testing.T) {
	k := func(o, h, l, c float64) market.Kline { return market.Kline{Open: o, High: h, Low: l, Close: c} }
	crossing := []market.Kline{k(100, 120, 90, 118), k(115, 119, 95, 98), k(99, 110, 96, 108)}
	clean := []market.Kline{k(100, 110, 99, 109), k(109, 120, 108, 119), k(119, 130, 118, 129)}
	t5 := TriggerLine{Dir: SideShort, Price: 1}
	t15 := TriggerLine{Dir: SideLong, Price: 2}
	t30 := TriggerLine{Dir: SideShort, Price: 3}
	on := DefaultConfig()
	on.TriggerEscalation = true // G3 oct round 2: the default is OFF; the ladder pins the September read
	off := DefaultConfig()
	off.TriggerEscalation = false
	cases := []struct {
		name      string
		cfg       Config
		cb5, cb15 []market.Kline
		wantTF    int
		wantPrice float64
	}{
		{"5m clean", on, clean, crossing, 5, 1},
		{"5m crossing → 15m", on, crossing, clean, 15, 2},
		{"5m + 15m crossing → 30m", on, crossing, crossing, 30, 3},
		{"knob off → 5m", off, crossing, crossing, 5, 1},
	}
	for _, c := range cases {
		got, tf := governingTrigger(c.cfg, t5, t15, t30, c.cb5, c.cb15)
		if tf != c.wantTF || got.Price != c.wantPrice {
			t.Errorf("%s: got %dm @%v want %dm @%v", c.name, tf, got.Price, c.wantTF, c.wantPrice)
		}
	}
}

// TestM60P1TapeReplayDiff — the m465 P1 before/after harness (opt-in,
// READ-ONLY): ticks the evaluator over an exported MNQ 1m tape like the live
// loop (one Tick per closed 1m bar, the last 2000 bars as the window) TWICE —
// the #60 defaults and the three P1 knobs at their pre-#60 values (proven
// byte-identical to the pre-#60 kernel by the m434 golden) — and logs every
// ENTRY intent that appears or disappears, with a tape outcome (fill when a
// later 1m bar trades through the entry before the next exec candle closes for
// an ISB / by 15:00 CT for a level order; then stop-first, else target, else
// flat at the tape end — no partials, no BE: an approximation, [B]).
//
//	M60P1_REPLAY_CSV=/path/mnq_1m.csv   ot,o,h,l,c,v (the m413 loader)
//	M60P1_REPLAY_FROM=2026-10-28        first CT date whose entries are reported
func TestM60P1TapeReplayDiff(t *testing.T) {
	path := os.Getenv("M60P1_REPLAY_CSV")
	if path == "" {
		t.Skip("set M60P1_REPLAY_CSV (+ M60P1_REPLAY_FROM) to run the m465 P1 before/after tape replay")
	}
	from, err := time.ParseInLocation("2006-01-02", os.Getenv("M60P1_REPLAY_FROM"), ctime())
	if err != nil {
		t.Fatalf("M60P1_REPLAY_FROM: %v", err)
	}
	bars := m413Load(t, path)
	type entry struct {
		at, setup string
		side      Side
		px, stop  float64
		target    float64
		conf      bool
		i         int
		expiry    int64
	}
	run := func(pre bool) (map[string]entry, map[string]int) {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.RealFillOnly = true
		cfg.LossDeparturePts = 0
		cfg.LegBudgetEnabled = true
		cfg.LegResetOn = "close"
		cfg.LocTriggerFilter = true
		cfg.DayOffRecheck = false
		cfg.EmaMaxCross30m = 0
		if pre {
			m462PreM465Knobs(&cfg)
		}
		e := New(cfg)
		got := map[string]entry{}
		for i := 1; i < len(bars); i++ {
			lo := i + 1 - 2000
			if lo < 0 {
				lo = 0
			}
			out := e.Tick(bars[lo:i+1], BarCloseInstant(bars[i]))
			for _, in := range out {
				if in.Action == ActionMoveStopBE {
					e.SwingBEApplied(in.ArmID)
				}
				if in.Action == ActionClosePosition {
					e.SwingEnded(in.ArmID)
				}
				if (in.Action != PlaceStopEntry && in.Action != PlaceStopLimitEntry) || bars[i].OpenTime < from.UnixMilli() {
					continue
				}
				at := time.UnixMilli(bars[i].OpenTime).In(ctime()).Format("01-02 15:04")
				k := fmt.Sprintf("%s %s %s %.2f", at, in.Setup, in.Side, in.Price)
				got[k] = entry{at: at, setup: in.Setup, side: in.Side, px: in.Price, stop: in.Stop, target: in.Target, conf: in.Confluence, i: i, expiry: in.ExpiryMs}
			}
		}
		return got, e.State.Refusals
	}
	outcome := func(en entry) (string, float64) {
		long := en.side == SideLong
		exp := en.expiry
		if exp == 0 {
			d := time.UnixMilli(bars[en.i].OpenTime).In(ctime())
			exp = time.Date(d.Year(), d.Month(), d.Day(), 15, 0, 0, 0, ctime()).UnixMilli()
		}
		filled := -1
		for j := en.i + 1; j < len(bars); j++ {
			b := bars[j]
			if filled < 0 {
				if b.OpenTime > exp {
					return "no fill before expiry", 0
				}
				if long && b.High >= en.px || !long && b.Low <= en.px {
					filled = j
				} else {
					continue
				}
			}
			if long && b.Low <= en.stop || !long && b.High >= en.stop {
				return "stop " + time.UnixMilli(b.OpenTime).In(ctime()).Format("15:04"), -abs(en.px - en.stop)
			}
			if en.target > 0 && (long && b.High >= en.target || !long && b.Low <= en.target) {
				return "target " + time.UnixMilli(b.OpenTime).In(ctime()).Format("15:04"), abs(en.target - en.px)
			}
		}
		if filled < 0 {
			return "no fill", 0
		}
		last := bars[len(bars)-1].Close
		if long {
			return "open at tape end", last - en.px
		}
		return "open at tape end", en.px - last
	}
	after, refA := run(false)
	pre, _ := run(true)
	net := 0.0
	for k, en := range pre {
		if _, ok := after[k]; !ok {
			o, pts := outcome(en)
			net -= pts
			t.Logf("DISAPPEARS %s %s %s entry %.2f stop %.2f target %.2f — tape: %s %+.2f pts", en.at, en.setup, en.side, en.px, en.stop, en.target, o, pts)
		}
	}
	for k, en := range after {
		p, ok := pre[k]
		if !ok {
			o, pts := outcome(en)
			net += pts
			t.Logf("APPEARS    %s %s %s entry %.2f stop %.2f target %.2f conf=%v — tape: %s %+.2f pts", en.at, en.setup, en.side, en.px, en.stop, en.target, en.conf, o, pts)
		} else if p.conf != en.conf || p.target != en.target {
			t.Logf("CHANGED    %s %s %s entry %.2f: confluence %v→%v target %.2f→%.2f", en.at, en.setup, en.side, en.px, p.conf, en.conf, p.target, en.target)
		}
	}
	var m60 []string
	for k, v := range refA {
		if strings.HasSuffix(k, "_forming") || strings.HasSuffix(k, "_15m") && strings.Contains(k, "trigger") || strings.HasSuffix(k, "_30m") && strings.Contains(k, "trigger") {
			m60 = append(m60, fmt.Sprintf("%s=%d", k, v))
		}
	}
	sort.Strings(m60)
	t.Logf("entries pre-#60 %d, #60 %d; net tape change %+.2f pts (1 MNQ, stop/target only); P1 refusals (final-day ledger): %v", len(pre), len(after), net, m60)
}
