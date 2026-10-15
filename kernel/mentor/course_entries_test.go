package mentor

import (
	"testing"

	"vl/market"
)

// ── Release #25 course-entry knobs (fix/mentor-course-entries) ──────────────
// Every test drives the PRODUCTION call site — Evaluator.Tick, emaLossTick (the
// function Tick calls) or Limits.Apply (the Tick hook) — and pins the knob's
// default (DefaultConfig) AND its other value.

func longISBEmitted(ins []Intent) bool {
	for _, in := range entryIntents(ins) {
		if in.Setup == "ISB" && in.Side == SideLong {
			return true
		}
	}
	return false
}

// CE-1 (a) — "Nếu như khung 5 phút xuất hiện Insaba cho lệnh short thì em
// trade. Còn không em không trade" [D4.2 p1 @17:09–17:13]. Default
// require_5m_isb: a long 15m box with NO 5m ISB refuses the same-way 1m ISB.
// MUTANT: make fifteenMinISBGate return "" for require_5m_isb → RED.
func TestCE1FifteenMinBoxNeedsSameWay5mISB(t *testing.T) {
	e, bars, now := mtfFixture(t, false) // long 1m ISB, every other gate passes
	if e.Cfg.FifteenMinIBRule != FifteenMinRequire5mISB {
		t.Fatalf("default 15m_ib_requires_5m_isb = %q, want %q", e.Cfg.FifteenMinIBRule, FifteenMinRequire5mISB)
	}
	e.State.ISBBox15m = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: bars[0].OpenTime}
	ins := e.Tick(bars, now)
	if longISBEmitted(ins) {
		t.Fatalf("no 5m ISB inside the 15m box — the 1m ISB must be refused; got %+v", entryIntents(ins))
	}
	if e.State.Refusals["isb_15m_needs_5m_isb"] == 0 {
		t.Fatalf("refusals %v, want isb_15m_needs_5m_isb counted", e.State.Refusals)
	}
}

// CE-1: a STANDING same-way 5m ISB box lets the 1m ISB trade ("cùng chiều với
// khung 15 thì tôi kiếm lệnh — tôi dùng 1 phút" [@14:24–14:34]).
func TestCE1FifteenMinBoxWithStanding5mISBTrades(t *testing.T) {
	e, bars, now := mtfFixture(t, false)
	e.State.ISBBox15m = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: bars[0].OpenTime}
	e.State.ISBBox = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: bars[0].OpenTime}
	if ins := e.Tick(bars, now); !longISBEmitted(ins) {
		t.Fatalf("same-way 5m ISB stands — the 1m ISB must trade; refusals %v", e.State.Refusals)
	}
}

// CE-1: a 5m ISB drawn since the 15m inside candle that ESCAPED the 15m way
// also satisfies the rule; one that escaped the other way does not.
func TestCE1FifteenMinBoxWithEscaped5mISB(t *testing.T) {
	for _, tc := range []struct {
		escape Side
		want   bool
	}{{SideLong, true}, {SideShort, false}} {
		e, bars, now := mtfFixture(t, false)
		e.State.ISBBox15m = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: bars[0].OpenTime}
		e.State.LastISBBoxAt = bars[5].OpenTime
		e.State.LastISBBoxDir = SideLong
		e.State.LastISBBoxEscape = tc.escape
		if got := longISBEmitted(e.Tick(bars, now)); got != tc.want {
			t.Fatalf("5m ISB escaped %s: emitted=%v, want %v (refusals %v)", tc.escape, got, tc.want, e.State.Refusals)
		}
	}
}

// CE-1 option 1 — "Tôi không trade luôn… Đợi nó thoát khỏi range rồi mình
// trade" [D4.2 p1 @01:06–01:14]: no ISB at all while the 15m box stands, even
// with a same-way 5m ISB. "off" restores the pre-#25 read (the 1m ISB trades
// without any 5m ISB).
func TestCE1FifteenMinUntilEscapeAndOff(t *testing.T) {
	e, bars, now := mtfFixture(t, false)
	e.Cfg.FifteenMinIBRule = FifteenMinUntilEscape
	e.State.ISBBox15m = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: bars[0].OpenTime}
	e.State.ISBBox = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: bars[0].OpenTime}
	if longISBEmitted(e.Tick(bars, now)) || e.State.Refusals["isb_15m_until_escape"] == 0 {
		t.Fatalf("no_trade_until_escape must refuse every ISB; refusals %v", e.State.Refusals)
	}

	e, bars, now = mtfFixture(t, false)
	e.Cfg.FifteenMinIBRule = FifteenMinOff
	e.State.ISBBox15m = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: bars[0].OpenTime}
	if !longISBEmitted(e.Tick(bars, now)) {
		t.Fatalf("off = the pre-#25 read: the same-way 1m ISB trades; refusals %v", e.State.Refusals)
	}
}

// CE-2 (b, OQ-2) — "Đừng trade. Đợi nó thoát cả luôn" [D4.2 p1 @07:54–08:02].
// both_boxes: a latched conflict holds while the 15m box still stands (the 5m
// box already escaped); first_box (default) lets the ISB through.
// MUTANT: return (now, false, false) unconditionally in tfConflictHeld → RED.
func TestCE2ConflictHoldsUntilBothBoxesEscape(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want bool // ISB emitted
	}{{TFConflictBothBoxes, false}, {TFConflictFirstBox, true}} {
		e, bars, now := mtfFixture(t, false)
		e.Cfg.FifteenMinIBRule = FifteenMinOff // isolate CE-2
		e.Cfg.TFConflictEnd = tc.mode
		e.State.TFConflictLatched = true // a conflict was seen; the 5m box has escaped
		e.State.ISBBox15m = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: bars[0].OpenTime}
		got := longISBEmitted(e.Tick(bars, now))
		if got != tc.want {
			t.Fatalf("tf_conflict_end=%s: emitted=%v, want %v (refusals %v)", tc.mode, got, tc.want, e.State.Refusals)
		}
		if tc.mode == TFConflictBothBoxes && e.State.Refusals["isb_tf_conflict_both_boxes"] == 0 {
			t.Fatalf("held conflict must be counted; refusals %v", e.State.Refusals)
		}
	}
	if DefaultConfig().TFConflictEnd != TFConflictFirstBox {
		t.Fatal("tf_conflict_end default must stay first_box (case b)")
	}
}

// CE-5 (a) — "cái thân nến… nằm bên ngoài mà cái râu nến nằm bên trong cái box
// ok… Cái đó là at least" [D4.1 p1 @20:52–21:02]. The fixture's last 1m candle
// OPENS inside the 5m box (102 < 103.5) and CLOSES out (104): body (default)
// keeps the box; close deletes it.
// MUTANT: route ISBBoxEscapeWith to ISBBoxEscape for "body" → RED.
func TestCE5ISBBoxEscapeIsBody(t *testing.T) {
	for _, tc := range []struct {
		mode      string
		wantStand bool
	}{{"", true}, {ISBBoxEscapeBody, true}, {ISBBoxEscapeClose, false}} {
		e, bars, now := mtfFixture(t, false)
		if tc.mode != "" {
			e.Cfg.ISBBoxEscapeMode = tc.mode
		}
		e.State.ISBBox = &ISBBox{High: 103.5, Low: 95, Dir: SideLong, AtTime: bars[0].OpenTime}
		e.State.LastISBBoxAt = bars[0].OpenTime
		e.Tick(bars, now)
		if stands := e.State.ISBBox != nil; stands != tc.wantStand {
			t.Fatalf("isb_box_escape=%q: box stands=%v, want %v", tc.mode, stands, tc.wantStand)
		}
	}
}

// CE-6 (a) — "Tắt EMA 34 đi. Đợi xíu nữa nó thoát khỏi cái vùng đó đi. Rồi
// tôi bật EMA 34 lên lại" [D4.2 p2 @01:33–01:39]. until_zone_exit (default):
// a candle still touching the losing 98–102 zone keeps the block; the first
// closed candle completely outside lifts it. session keeps it to the roll.
// MUTANT: drop the zone-exit branch in emaLossTick → RED.
func TestCE6EmaLossRelocksOnZoneExit(t *testing.T) {
	run := func(mode string) *Evaluator {
		cfg := DefaultConfig()
		if mode != "" {
			cfg.EmaLossRelock = mode
		}
		e := New(cfg)
		d := auditMs(2026, 9, 15, 10, 0, 0)
		e.State.EmaPendingSide = SideLong
		e.State.EmaPendingEntry = 102
		e.State.EmaPendingStop = 98
		e.State.EmaPendingTarget = 108
		e.State.EmaPendingExpiry = d + 3600_000
		emaLossTick(e, 100, market.Kline{High: 102.5, Low: 101, CloseTime: d - 60_000}, d)
		emaLossTick(e, 100, market.Kline{High: 101, Low: 97.5, CloseTime: d}, d+60_000)
		if !e.State.EmaBlocked {
			t.Fatal("filled then stopped must block the EMA line")
		}
		// Still touching the zone (wick into 98–102): no exit yet.
		emaLossTick(e, 100, market.Kline{High: 104, Low: 101.5, Close: 103.5, CloseTime: d + 60_000}, d+120_000)
		if !e.State.EmaBlocked {
			t.Fatal("a candle whose wick is still in the loss zone must not lift the block")
		}
		// Completely outside the zone (low 102.25 > 102).
		emaLossTick(e, 100, market.Kline{High: 106, Low: 102.25, Close: 105, CloseTime: d + 120_000}, d+180_000)
		return e
	}
	if e := run(""); e.State.EmaBlocked {
		t.Fatal("default until_zone_exit: a closed candle wholly outside the loss zone must lift the block")
	}
	if e := run(EmaRelockSession); !e.State.EmaBlocked {
		t.Fatal("session: the block holds until the 17:00 CT roll")
	}
}

// CE-7 (b, OQ-3) — "cái điểm entry… mà nằm gần cái resistance quá thì thôi bỏ
// đi" [X5 @08:52–09:09]. Entry 104.5 sits 1.5 above the ORB high 103: a 2-pt
// veto refuses it (orb_edge_too_near); the default 0 lets it through.
// MUTANT: make orbEdgeTooNear return false → RED.
func TestCE7OrbEdgeVeto(t *testing.T) {
	for _, tc := range []struct {
		veto float64
		want bool
	}{{0, true}, {2, false}, {1, true}} {
		e, bars, now := mtfFixture(t, false)
		e.Cfg.FifteenMinIBRule = FifteenMinOff
		e.Cfg.OrbEdgeVetoPts = tc.veto
		e.State.ORB = ORB{Day: dayStartCT(now), High: 103, Low: 99, Drawn: true, Escaped: SideLong}
		got := longISBEmitted(e.Tick(bars, now))
		if got != tc.want {
			t.Fatalf("orb_edge_veto_pts=%g: emitted=%v, want %v (refusals %v)", tc.veto, got, tc.want, e.State.Refusals)
		}
		if !tc.want && e.State.Refusals["orb_edge_too_near"] == 0 {
			t.Fatalf("veto must be counted as orb_edge_too_near; refusals %v", e.State.Refusals)
		}
	}
}

// CE-3 (b, OQ-6) — "Đợi canh nó nằm trên hai cái đường line này… Hoặc… nằm ở
// trên cái đường line 4 giờ" [D4.4 p1 @21:55–22:12]. Inside a news period
// (window open, before its end), ON refuses a long whose close (104) is not
// above the 4h line (104.25 — under the 104.5 entry, so it is not the target);
// a line under the close passes; OFF never gates.
// MUTANT: drop the newsWaitFilter call in Tick → RED.
func TestCE3NewsWaitFor4hLine(t *testing.T) {
	for _, tc := range []struct {
		on   bool
		line float64
		want bool
	}{{true, 104.25, false}, {true, 90, true}, {false, 104.25, true}} {
		e, bars, now := mtfFixture(t, false)
		e.Cfg.FifteenMinIBRule = FifteenMinOff
		e.Cfg.NewsWaitBothLines = tc.on
		e.Cfg.PrintWindows = []PrintWindow{{FromMs: now - 20*60_000, ToMs: now + 5*60_000}}
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: tc.line}}
		got := longISBEmitted(e.Tick(bars, now))
		if got != tc.want {
			t.Fatalf("news_wait_both_lines=%v line=%g: emitted=%v, want %v (refusals %v)", tc.on, tc.line, got, tc.want, e.State.Refusals)
		}
		if !tc.want && e.State.Refusals["news_wait_4h_line"] == 0 {
			t.Fatalf("refusal must be counted as news_wait_4h_line; refusals %v", e.State.Refusals)
		}
	}
	if DefaultConfig().NewsWaitBothLines {
		t.Fatal("news_wait_both_lines default must be OFF (case b)")
	}
}

// CE-4 (a, release #29 rework) — "Còn cái khúc mà news ra rồi thì không tính"
// [D4.4 p1 @18:13–18:25]. ON FREEZES the pre-print 1h/4h trigger read from the
// print window start until the 08:30 CT day-gate read (the bars still build
// their whole 1h/4h candles); OFF keeps the item-18 print-window drop.
func TestCE4NewsBarsExcludedToTheOpen(t *testing.T) {
	if !DefaultConfig().NewsBarsExcluded {
		t.Fatal("news_bars_excluded default must be ON (case a)")
	}
	w := printWindowCT(2026, 9, 15)

	// ON freezes every tick from the print window start through the 08:30
	// day-gate read (the 08:30-close tick at now = 08:31:00 releases).
	on := DefaultConfig()
	on.NewsBarsExcluded = true
	on.PrintWindows = []PrintWindow{w}
	for _, tc := range []struct {
		now   int64
		frozen bool
	}{
		{auditMs(2026, 9, 15, 7, 19, 0), false},
		{auditMs(2026, 9, 15, 7, 20, 0), true},
		{auditMs(2026, 9, 15, 7, 50, 0), true},
		{auditMs(2026, 9, 15, 8, 29, 0), true},
		{auditMs(2026, 9, 15, 8, 30, 0), true},  // the day-gate read tick
		{auditMs(2026, 9, 15, 8, 31, 0), false}, // after the open: candles whole again
	} {
		if got := newsHTFFrozen(on, tc.now); got != tc.frozen {
			t.Fatalf("newsHTFFrozen(%d) = %v, want %v", tc.now, got, tc.frozen)
		}
	}

	// The per-bar freeze (the seed replay) covers [07:20, 08:30), and a bar is
	// never deleted — the candle aggregation stays whole.
	if newsBarFrozen(auditMs(2026, 9, 15, 7, 19, 0), on) {
		t.Fatal("a pre-print bar must not be frozen")
	}
	if !newsBarFrozen(auditMs(2026, 9, 15, 7, 50, 0), on) {
		t.Fatal("a post-print bar before the open must be frozen")
	}
	if newsBarFrozen(auditMs(2026, 9, 15, 8, 30, 0), on) {
		t.Fatal("the 08:30 bar must not be frozen")
	}
	bars := []market.Kline{{OpenTime: auditMs(2026, 9, 15, 7, 19, 0)}, {OpenTime: auditMs(2026, 9, 15, 7, 50, 0)}, {OpenTime: auditMs(2026, 9, 15, 8, 30, 0)}}
	if n := len(htfFeedBars(bars, on.PrintWindows)); n != 3 {
		t.Fatalf("the item-18 OFF feed keeps %d bars, want 3 (no print-window deletion beyond 07:35)", n)
	}

	// OFF: no freeze at all (the item-18 drop is the only behaviour).
	off := DefaultConfig()
	off.NewsBarsExcluded = false
	off.PrintWindows = []PrintWindow{w}
	if newsHTFFrozen(off, auditMs(2026, 9, 15, 7, 50, 0)) {
		t.Fatal("OFF must not freeze")
	}
}

// CE-4 at the Tick call site: a 07:40 CT bar that breaks the prior 1h high
// moves the 1h line with the knob OFF and does not with it ON (the freeze).
// MUTANT: drop the newsHTFFrozen guard in Tick → RED.
func TestCE4TickHTFIgnoresPostPrintBars(t *testing.T) {
	build := func(on bool) *Evaluator {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.NewsBarsExcluded = on
		cfg.PrintWindows = []PrintWindow{printWindowCT(2026, 9, 15)}
		e := New(cfg)
		var bars []market.Kline
		start := auditMs(2026, 9, 15, 6, 0, 0)
		for i := 0; i < 101; i++ { // 06:00 … 07:40
			o := start + int64(i)*60_000
			b := market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: 100.5, High: 101, Low: 100, Close: 100.5}
			if i == 100 { // 07:40 — after the print window, before the open
				b.High, b.Close = 110, 109
			}
			bars = append(bars, b)
		}
		e.Tick(bars, bars[len(bars)-1].CloseTime+1)
		return e
	}
	if off := build(false); off.State.HTF.OneH.Dir != SideLong {
		t.Fatalf("OFF: the 07:40 break must move the 1h line to long, got %+v", off.State.HTF.OneH)
	}
	if on := build(true); on.State.HTF.OneH.Dir == SideLong {
		t.Fatalf("ON: a post-print bar before the open must not move the 1h line, got %+v", on.State.HTF.OneH)
	}
}

// CE-4 reworked (release #29): the freeze holds the pre-print 1h/4h read
// through the 08:30 day-gate read, then the lines resume on WHOLE candles — the
// 07:30 print spike is still IN the 07:00 1h bucket (never deleted), so it
// moves the 1h line only after the open, not during the news window.
// MUTANT: drop the newsHTFFrozen guard in Tick → the 07:50 tick moves the line.
func TestCE4FreezeResumesOnWholeCandles(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.NewsBarsExcluded = true
	cfg.PrintWindows = []PrintWindow{printWindowCT(2026, 9, 15)}
	e := New(cfg)

	start := auditMs(2026, 9, 15, 6, 0, 0)
	spike := auditMs(2026, 9, 15, 7, 30, 0)
	var fed []market.Kline
	for i := 0; i <= 210; i++ { // 06:00 … 09:30
		o := start + int64(i)*60_000
		b := market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: 100.5, High: 101, Low: 100, Close: 100.5}
		if o == spike {
			b.High, b.Close = 120, 119 // the print spike
		}
		fed = append(fed, b)
		e.Tick(fed, b.CloseTime+1)
		switch o {
		case auditMs(2026, 9, 15, 7, 50, 0):
			if e.State.HTF.OneH.Dir != "" {
				t.Fatalf("07:50 (frozen): 1h line must hold its pre-print read, got %+v", e.State.HTF.OneH)
			}
		case auditMs(2026, 9, 15, 8, 29, 0):
			if e.State.HTF.OneH.Dir != "" {
				t.Fatalf("08:29 day-gate read (frozen): 1h line must hold its pre-print read, got %+v", e.State.HTF.OneH)
			}
		}
	}
	// After 08:30 the freeze releases and the whole 07:00 bucket (spike high
	// 120) breaks the prior 1h high → long. The spike was never deleted.
	if e.State.HTF.OneH.Dir != SideLong {
		t.Fatalf("after the open the whole-candle spike must move the 1h line long, got %+v", e.State.HTF.OneH)
	}
}

// CE-4 reworked at the SEED call site (seedHistoryState): a boot after the open
// seeds the 1h line from the whole-candle freeze replay — the 07:30 spike moves
// the line long, exactly as a continuous bot held it. The OLD delete (htfFeedBars
// of the news window) seeded the line as never-moved ("").
// MUTANT: revert seedHistoryState to the htfFeedBars delete → RED.
func TestCE4SeedHistoryStateFreeze(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.NewsBarsExcluded = true
	cfg.PrintWindows = []PrintWindow{printWindowCT(2026, 9, 15)}

	start := auditMs(2026, 9, 15, 6, 0, 0)
	spike := auditMs(2026, 9, 15, 7, 30, 0)
	var bars []market.Kline
	for i := 0; i <= 210; i++ {
		o := start + int64(i)*60_000
		b := market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: 100.5, High: 101, Low: 100, Close: 100.5}
		if o == spike {
			b.High, b.Close = 120, 119
		}
		bars = append(bars, b)
	}
	now := bars[len(bars)-1].CloseTime + 1
	hist := closedOnly(bars, now)
	e := New(cfg)
	e.seedHistoryState(hist, keyLevel1HBars(hist), now, false)
	if e.State.HTF.OneH.Dir != SideLong {
		t.Fatalf("seed: the whole-candle spike must move the 1h line long, got %+v", e.State.HTF.OneH)
	}
	// The OLD delete drops the spike from the 07:00 bucket, so the line never
	// moves — the behaviour the rework removes.
	del := TriggerReplay1m(TriggerLine{}, htfFeedBars(bars, newsHTFWindows(cfg.PrintWindows, cfg)), 60, cfg)
	if del.Dir != "" {
		t.Fatalf("the OLD delete must NOT move the line (spike deleted), got %+v", del)
	}
}

// CE-8 (b) — "Nhìn từ bên trái qua bên phải… Đỉnh cao, đỉnh thấp hơn, đỉnh
// thấp hơn — … Giảm" [D4.3 @12:43–12:54]: the structure read.
func TestCE8StructureTrend5m(t *testing.T) {
	mk := func(hl ...[2]float64) []market.Kline {
		var out []market.Kline
		for i, x := range hl {
			o := int64(i) * 300_000
			out = append(out, market.Kline{OpenTime: o, CloseTime: o + 299_999, High: x[0], Low: x[1], Open: (x[0] + x[1]) / 2, Close: (x[0] + x[1]) / 2})
		}
		return out
	}
	up := mk([2]float64{10, 5}, [2]float64{12, 7}, [2]float64{8, 3}, [2]float64{9, 4}, [2]float64{14, 9}, [2]float64{11, 6}, [2]float64{10, 5}, [2]float64{16, 11}, [2]float64{13, 8})
	if got := structureTrend5m(up); got != SideLong {
		t.Fatalf("HH+HL: got %q, want long", got)
	}
	down := mk([2]float64{20, 15}, [2]float64{18, 12}, [2]float64{22, 17}, [2]float64{21, 16}, [2]float64{16, 10}, [2]float64{17, 11}, [2]float64{19, 14}, [2]float64{14, 8}, [2]float64{15, 9})
	if got := structureTrend5m(down); got != SideShort {
		t.Fatalf("LH+LL: got %q, want short", got)
	}
	if got := structureTrend5m(mk([2]float64{10, 5})); got != "" {
		t.Fatalf("too few swings: got %q, want none", got)
	}
}

// CE-11/B12 (a) — "Target là về những level kế tiếp… về trendline, anything"
// [D3.3 p1 @05:18–05:35]. Default ON: the Tick adds a target-only mirror of
// each trendline, and the target pick takes it as the first obstacle; the
// trendline LOCATION itself never is.
// MUTANT: skip KindTrendlineTarget in nextLevelBeyondRoom → RED.
func TestB12TrendlineIsTarget(t *testing.T) {
	if !DefaultConfig().TrendlineIsTarget {
		t.Fatal("trendline_is_target default must be ON (case a)")
	}
	levels := []Level{
		{Key: "trendline:x", Kind: KindTrendline, Price: 105},
		{Key: "K", Kind: KindKeyLevel, Price: 120},
	}
	if got := nextLevelBeyondRoom(levels, 100, 98, SideLong, 2); got != 120 {
		t.Fatalf("location only (knob OFF): target %g, want 120", got)
	}
	withMirror := append(append([]Level(nil), levels...), trendlineTargets(levels)...)
	if got := nextLevelBeyondRoom(withMirror, 100, 98, SideLong, 2); got != 105 {
		t.Fatalf("knob ON: the trendline is the first target, got %g want 105", got)
	}
	if got := nextLevelBeyond(withMirror, 100, SideLong); got != 105 {
		t.Fatalf("PHL target pick: got %g want 105", got)
	}
}

// CE-12 (a, extra) — "đợi cho nó break được cái đỉnh, hoặc đợi cho nó break
// được cái đáy của con sóng đó" [X-FOMO @06:10–06:22]. A long leg closed by a
// stop-out resets once a candle closes below the losing wave's low (87);
// with the knob OFF only a close above the old high does.
// MUTANT: drop the long wave-break reset in resetBreaks → RED.
func TestCE12StoppedLegResetsOnWaveBreak(t *testing.T) {
	for _, tc := range []struct {
		on   bool
		want int // entries emitted after the wave break
	}{{true, 1}, {false, 0}} {
		cfg := limitsCfg()
		cfg.LegResetOnWaveBreak = tc.on
		var l Limits
		exp := limitsNow(60) + 86400_000
		applyAtCfg(&l, []Intent{limitsPHL(90, 88, 99, exp)}, limitsK(94, 96, 94, 95, 0), limitsK(93, 94, 92, 93, 1), 1, cfg)
		// Fill + stop-out on one candle: the wave low is 87.
		applyAtCfg(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(89, 91, 87, 88, 2), 2, cfg)
		if l.Long == nil || !l.Long.Stopped || l.Long.Wave != 87 {
			t.Fatalf("stopped leg with wave 87 expected, got %+v", l.Long)
		}
		// Candle 3 closes BELOW the wave low; candle 4's entry is gated by it.
		applyAtCfg(&l, nil, limitsK(89, 91, 87, 88, 2), limitsK(87, 88, 85, 86, 3), 3, cfg)
		out := applyAtCfg(&l, []Intent{limitsISB(87, 84, 95, exp)}, limitsK(87, 88, 85, 86, 3), limitsK(86, 87.5, 85.5, 87, 4), 4, cfg)
		if len(out) != tc.want {
			t.Fatalf("leg_reset_on_wave_break=%v: %d entries after the wave break, want %d (leg %+v)", tc.on, len(out), tc.want, l.Long)
		}
	}
}

// CE-9 (b, owner ruled school 1) — "Bất kỳ trường hợp nào đi nữa… nằm ở dưới
// đường buy trigger không được đánh" [D3.4 p1 @17:48–18:00]. Under school 1 a
// long level reject below a SELL line trades by default; with the knob ON it is
// refused (phl_level_trigger_side).
// MUTANT: drop `|| e.Cfg.LevelEntriesNeedTriggerSide` from the level filter → RED.
func TestCE9LevelEntriesNeedTriggerSide(t *testing.T) {
	oldHighs := []Level{{Key: "old-high:130", Kind: KindOldExtreme, Role: OldHigh, Price: 130}}
	for _, on := range []bool{false, true} {
		e, bars, now, _ := b14Fixture(oldHighs, 99.5)
		e.Cfg.LocTriggerFilter = true
		e.Cfg.TriggerSchool = 1
		e.Cfg.LevelEntriesNeedTriggerSide = on
		e.State.Trigger = TriggerLine{Dir: SideShort, Price: 120, LastBucket: maxInt64}
		n := b14Entries(e.Tick(bars, now))
		if on && (n != 0 || e.State.Refusals["phl_level_trigger_side"] == 0) {
			t.Fatalf("knob ON: want the level reject refused + counted, got %d entries, refusals %v", n, e.State.Refusals)
		}
		if !on && n == 0 {
			t.Fatalf("knob OFF (school 1): the level reject must trade without the trigger; refusals %v", e.State.Refusals)
		}
	}
	if DefaultConfig().LevelEntriesNeedTriggerSide {
		t.Fatal("level_entries_need_trigger_side default must be OFF (case b)")
	}
}

// CE-11/B13 (b) — between an FTGL (floor) and an FTGH (ceiling) every level
// PHL/PLH is refused; ON exempts a TRENDLINE reject only.
// MUTANT: drop the trendline exemption in midRangeBoxedRefuses → RED.
func TestB13TrendlineBetweenBoxes(t *testing.T) {
	boxes := []Box{{Kind: FTGL, Top: 95, Bottom: 90}, {Kind: FTGH, Top: 115, Bottom: 110}}
	tl := Level{Key: "trendline:x", Kind: KindTrendline, Price: 100}
	kl := Level{Key: "K", Kind: KindKeyLevel, Price: 100}
	cfg := DefaultConfig()
	if !midRangeBoxedRefuses(cfg, tl, boxes, 101) || !midRangeBoxedRefuses(cfg, kl, boxes, 101) {
		t.Fatal("default OFF: every level reject between two boxes is refused")
	}
	cfg.TrendlineBetweenBoxes = true
	if midRangeBoxedRefuses(cfg, tl, boxes, 101) {
		t.Fatal("ON: a trendline reject between two boxes may trade")
	}
	if !midRangeBoxedRefuses(cfg, kl, boxes, 101) {
		t.Fatal("ON exempts the trendline only — a key level stays refused")
	}
	if midRangeBoxedRefuses(DefaultConfig(), kl, boxes[:1], 101) {
		t.Fatal("one box is not mid-range")
	}
}

// CE-11 FLAG-TARGET (b, ported from PR #416) — "target của anh chị là chỉ là
// trên cái đỉnh của cái flag… vẫn scalping được, không sai" [X9 @05:47–06:12].
// A long inside a live flag (support 95, resistance 106 at the bar) targets the
// closer resistance wall when ON; a wall under 1:1 refuses; OFF never touches
// the intent. MUTANT: make wallFor always return ok=false → RED.
func TestCE11FlagWallTarget(t *testing.T) {
	const bt = int64(600_000)
	fw := flagWalls{
		support:        Trendline{Side: SideLong, P0Px: 95, P0T: 0, P1Px: 95, P1T: bt},
		resistance:     Trendline{Side: SideShort, P0Px: 106, P0T: 0, P1Px: 106, P1T: bt},
		haveSupport:    true,
		haveResistance: true,
	}
	in := Intent{Action: PlaceStopLimitEntry, Side: SideLong, Price: 100, Stop: 97, Target: 120}
	var refused []string
	ref := func(r string) { refused = append(refused, r) }

	cfg := DefaultConfig()
	if cfg.FlagWallTarget {
		t.Fatal("flag_wall_target default must be OFF (case b)")
	}
	if got := applyFlagWallTarget([]Intent{in}, fw, bt, cfg, ref); got[0].Target != 120 {
		t.Fatalf("OFF: target must stay 120, got %g", got[0].Target)
	}
	cfg.FlagWallTarget = true
	got := applyFlagWallTarget([]Intent{in}, fw, bt, cfg, ref)
	if len(got) != 1 || got[0].Target != 106 {
		t.Fatalf("ON: the resistance wall 106 is the closer target, got %+v", got)
	}
	tight := in
	tight.Stop = 90 // risk 10, wall 6 away → under 1:1
	if got := applyFlagWallTarget([]Intent{tight}, fw, bt, cfg, ref); len(got) != 0 {
		t.Fatalf("a wall under 1:1 must refuse the entry, got %+v", got)
	}
	if len(refused) < 2 || refused[len(refused)-1] != "flag_wall_room" {
		t.Fatalf("refusals %v, want flag_wall_target_used then flag_wall_room", refused)
	}
}

// CE-1 — the OPPOSITE-way 5m ISB (CTO gate fold, PR #532): "Nếu như khung 5
// phút xuất hiện Insaba cho lệnh short thì em trade" [D4.2 p1 @17:09–17:13] —
// only a SAME-direction 5m ISB lets a 1m ISB trade inside the 15m box.
//
// At the Tick call site a standing 5m box the opposite way to the 15m box IS
// the D4.2-03 conflict (mtfConflict reads the same two boxes), which refuses
// every setup BEFORE the ISB path runs — so the 1m short ISB never emits.
// fifteenMinISBGate's own opposite-box branch is pinned directly below (the
// Tick never reaches it while both boxes stand; it is the gate's defence if the
// conflict rule is ever switched or narrowed).
// MUTANT (CTO): in fifteenMinISBGate, box5 != nil && box5.Dir != box15.Dir →
// return "" — the direct pin goes RED.
func TestCE1Opposite5mISBInside15mBox(t *testing.T) {
	e, bars, now := mtfFixture(t, true) // a 1m SHORT ISB (red candle 1)
	e.State.ISBBox15m = &ISBBox{High: 110, Low: 95, Dir: SideShort, AtTime: bars[0].OpenTime}
	e.State.ISBBox = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: bars[0].OpenTime}
	shortSide := func(e *Evaluator) {
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideShort, Price: 120}}
		e.State.Trigger = TriggerLine{Dir: SideShort, Price: 101}
		e.State.ORB = ORB{Day: dayStartCT(now), High: 120, Low: 115, Drawn: true, Escaped: SideShort}
	}
	shortSide(e)
	for _, in := range entryIntents(e.Tick(bars, now)) {
		if in.Setup == "ISB" {
			t.Fatalf("15m SHORT + standing 5m LONG: no ISB may trade, got %+v", in)
		}
	}
	// Non-vacuous: the same short ISB DOES trade once the 5m ISB agrees with
	// the 15m (both SHORT).
	e2, bars2, now2 := mtfFixture(t, true)
	shortSide(e2)
	e2.State.ISBBox15m = &ISBBox{High: 110, Low: 95, Dir: SideShort, AtTime: bars2[0].OpenTime}
	e2.State.ISBBox = &ISBBox{High: 110, Low: 95, Dir: SideShort, AtTime: bars2[0].OpenTime}
	var shortISB bool
	for _, in := range entryIntents(e2.Tick(bars2, now2)) {
		if in.Setup == "ISB" && in.Side == SideShort {
			shortISB = true
		}
	}
	if !shortISB {
		t.Fatalf("control: a same-way 5m ISB must let the short ISB trade; refusals %v", e2.State.Refusals)
	}

	box15 := &ISBBox{High: 110, Low: 95, Dir: SideShort, AtTime: bars[0].OpenTime}
	box5 := &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: bars[0].OpenTime}
	if got := fifteenMinISBGate(FifteenMinRequire5mISB, box15, box5, box5.AtTime, SideLong, ""); got != "15m_needs_5m_isb" {
		t.Fatalf("require_5m_isb with an opposite standing 5m ISB: gate = %q, want 15m_needs_5m_isb", got)
	}
	if got := fifteenMinISBGate(FifteenMinRequire5mISB, box15, &ISBBox{Dir: SideShort, AtTime: box5.AtTime}, box5.AtTime, SideShort, ""); got != "" {
		t.Fatalf("a same-way standing 5m ISB must pass the gate, got %q", got)
	}
	if got := fifteenMinISBGate(FifteenMinOff, box15, box5, box5.AtTime, SideLong, ""); got != "" {
		t.Fatalf("off = the pre-#25 read: no CE-1 refusal, got %q", got)
	}
}
