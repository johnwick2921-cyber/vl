package mentor

import (
	"testing"

	"vl/market"
)

// m466 P3 (release #61, owner order 2026-11-02 "fix everything the video check
// found"): the ISB ceiling 20 (G23 D4.1-R12), the open-range break on a 1m
// close (G55 D4.4-R40), the news wait on BOTH lines (G56 D4.4-R22) and the
// 1h/4h read lead 30 minutes before the print (G58 D4.4-R6). Each test is the
// course example first; each names its mutant.

// TestM61ISBOverTwentyRefused — G23 D4.1-R12: "nếu như một cái setup inside
// bar mà nó hai mươi mấy điểm, làm ơn giúp em đừng vô, làm ơn đừng vô… lệnh
// inside bar phải là một lệnh có stop loss nhỏ" [D4.1 p1 @05:41–06:08]; his own
// stop that morning was "20 points (really under 20)" [D4.1 p1 @02:39–03:20].
// D4.1 is LATER than D3.3-R1/R4 [D3.3 p1 @01:09–02:09] (20–25 = reduce size),
// so for an ISB the reduce-size option is gone: the ceiling is 20, measured
// entry-to-stop the way ISBStopVerdict measures it ((High−Low) + 2×buffer).
// PHL/PLH keep the D3.3-R1 25 (StopCeilingPts).
// MUTANT: default ISBMaxStopPts back to 25 → the 20.25 and 22.50 stops pass → RED.
func TestM61ISBOverTwentyRefused(t *testing.T) {
	cfg := DefaultConfig()
	if got := cfg.CourseExits.ISBMaxStopPts; got != 20 {
		t.Fatalf("ISB ceiling default = %g, want 20 (D4.1-R12)", got)
	}
	if cfg.StopCeilingPts != 25 {
		t.Fatalf("PHL/PLH StopCeilingPts = %g, want 25 unchanged (D3.3-R1)", cfg.StopCeilingPts)
	}
	if cfg.ISBBufferPts != 1.5 {
		t.Fatalf("fixture assumes the 1.5-pt ISB buffer, got %g", cfg.ISBBufferPts)
	}
	isb := func(lo, hi float64) market.Kline {
		ot := auditMs(2026, 9, 14, 9, 0, 0)
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: lo + 1, High: hi, Low: lo, Close: hi - 1}
	}
	for _, tc := range []struct {
		name  string
		rng   float64
		stop  float64
		taken bool
	}{
		{"range 17.25 → stop 20.25 (twenty-something)", 17.25, 20.25, false},
		{"range 17.00 → stop 20.00 (his own '20 points')", 17.0, 20.0, true},
		{"range 12.00 → stop 15.00", 12.0, 15.0, true},
	} {
		c := isb(29000, 29000+tc.rng)
		stop, ok, why := ISBStopVerdict(c, cfg)
		if stop != tc.stop || ok != tc.taken {
			t.Fatalf("%s: stop %.2f ok=%v, want %.2f ok=%v (%s)", tc.name, stop, ok, tc.stop, tc.taken, why)
		}
		if !ok && why != ISBMaxStopReason {
			t.Fatalf("%s: reason %q, want ISBMaxStopReason", tc.name, why)
		}
	}

	// D3.4-R28 [D3.4 p1 @23:22–24:00, Sun 13 Sep 17:04]: sell stop 29,034.25,
	// stop loss 29,056.75 — 22.50 pts entry-to-stop. The ISB candle is
	// 29,035.75–29,055.25 (range 19.50 + 2×1.5). Under D4.1-R12 it is refused.
	prev := market.Kline{Open: 29060, High: 29062, Low: 29030, Close: 29032} // red candle 1 → short
	cur := isb(29035.75, 29055.25)
	_, short := ISBOrders(cur, cfg)
	if short.Price != 29034.25 || short.Stop != 29056.75 {
		t.Fatalf("D3.4-R28 geometry: short %.2f / stop %.2f, want 29,034.25 / 29,056.75", short.Price, short.Stop)
	}
	side, _, ok, why := ISBStopLimitOrder(prev, cur, cfg)
	if side != SideShort || ok || why != ISBMaxStopReason {
		t.Fatalf("D3.4-R28 22.50-pt ISB: side %q ok=%v why=%q, want short refused (isb stop over max)", side, ok, why)
	}
	// Stored overrides win: the owner's 25 keeps the pre-#61 ceiling.
	cfg25 := cfg
	cfg25.CourseExits.ISBMaxStopPts = 25
	if _, _, ok, why := ISBStopLimitOrder(prev, cur, cfg25); !ok {
		t.Fatalf("a stored 25 must keep the 22.50 ISB (stored values win): %s", why)
	}
}

// open5mTape14Sep builds the 14 Sep opening 5m range 28,880.50–28,933.75
// (08:30–08:34) and the 1m bars 08:35… up to (not including) `upTo`. The 08:43
// 1m candle closes 28,940 — above the range — and the 08:44 one 28,941.
func open5mTape14Sep(upToHH, upToMM int) []market.Kline {
	mk := func(hh, mm int, o, h, l, c float64) market.Kline {
		ot := auditMs(2026, 9, 14, hh, mm, 0)
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	all := []market.Kline{
		mk(8, 30, 28900, 28933.75, 28895, 28920),
		mk(8, 31, 28920, 28925, 28880.50, 28890),
		mk(8, 32, 28890, 28910, 28885, 28905),
		mk(8, 33, 28905, 28915, 28895, 28910),
		mk(8, 34, 28910, 28920, 28900, 28915),
	}
	for m := 35; m <= 42; m++ { // inside the range
		all = append(all, mk(8, m, 28915, 28925, 28905, 28918))
	}
	all = append(all,
		mk(8, 43, 28918, 28942, 28915, 28940), // the 1m escape the mentor acted on
		mk(8, 44, 28940, 28945, 28935, 28941), // the 08:40 5m bucket closes 28,941 at 08:45
		mk(8, 45, 28941, 28950, 28938, 28948),
	)
	cut := auditMs(2026, 9, 14, upToHH, upToMM, 0)
	var out []market.Kline
	for _, b := range all {
		if b.OpenTime < cut {
			out = append(out, b)
		}
	}
	return out
}

// TestM61Open5mBreaksOnOneMinuteClose — G55 D4.4-R40: "nó break được cái range
// khung 5 phút, nó đi lên rồi, tôi biết tôi đánh" [D4.4 p2 @05:15–05:36]; the
// break minimum is "một cây nến một phút… đóng bên ngoài cái box là at least"
// [D4.1 p1 @20:36–21:03]. The mentor acted at the 08:43 1m escape. Default
// 1m_close: the long is allowed once the 08:43 candle CLOSES (at 08:44); the
// pre-#61 5m_close waits for the 08:40 bucket's close at 08:45.
// MUTANT: drop the 1m branch in open5mBrokenWith → the 08:44 long is refused → RED.
func TestM61Open5mBreaksOnOneMinuteClose(t *testing.T) {
	def := DefaultConfig()
	if def.CourseExits.Open5mBreak != Open5mBreak1mClose {
		t.Fatalf("mentor_open5m_break default = %q, want %q", def.CourseExits.Open5mBreak, Open5mBreak1mClose)
	}
	old := def
	old.CourseExits.Open5mBreak = Open5mBreak5mClose
	// 4h triggered buy with price still BELOW the 4h line — the D4.4 p2
	// @05:45–05:50 case where the 5m-drop rule applies.
	h := HTF{FourH: TriggerLine{Dir: SideLong, Price: 28990}}
	long := func() []Intent {
		return []Intent{{Action: PlaceStopLimitEntry, Setup: "ISB", Side: SideLong, Reason: "ISB", Price: 28943, Stop: 28925, Target: 28990}}
	}
	short := func() []Intent {
		return []Intent{{Action: PlaceStopLimitEntry, Setup: "ISB", Side: SideShort, Reason: "ISB", Price: 28878, Stop: 28896, Target: 28840}}
	}

	bars := open5mTape14Sep(8, 43) // through 08:42 — nothing closed outside yet
	if hi, lo, drawn := open5mRange(bars, auditMs(2026, 9, 14, 8, 43, 0)); !drawn || hi != 28933.75 || lo != 28880.50 {
		t.Fatalf("opening range %v/%v drawn=%v, want 28,933.75/28,880.50", hi, lo, drawn)
	}
	for _, c := range []Config{def, old} {
		if kept, ref, _ := open5mRangeFilter(long(), h, bars, auditMs(2026, 9, 14, 8, 43, 0), c); len(kept) != 0 || len(ref) != 1 || ref[0] != "open_5m_range_not_broken" {
			t.Fatalf("%s at 08:43: kept %d refused %v, want refused open_5m_range_not_broken", c.CourseExits.Open5mBreak, len(kept), ref)
		}
	}

	bars = open5mTape14Sep(8, 44) // the 08:43 candle has CLOSED at 28,940
	now := auditMs(2026, 9, 14, 8, 44, 0)
	if kept, ref, _ := open5mRangeFilter(long(), h, bars, now, def); len(kept) != 1 || len(ref) != 0 {
		t.Fatalf("1m_close at 08:44: kept %d refused %v, want the long allowed on the 08:43 1m close", len(kept), ref)
	}
	if kept, ref, _ := open5mRangeFilter(long(), h, bars, now, old); len(kept) != 0 || len(ref) != 1 {
		t.Fatalf("5m_close at 08:44: kept %d refused %v, want still refused (the 5m bucket closes 08:45)", len(kept), ref)
	}
	// The 1m close is the escape on ITS side only: a short is not broken.
	hs := HTF{FourH: TriggerLine{Dir: SideShort, Price: 28860}}
	if kept, _, _ := open5mRangeFilter(short(), hs, bars, now, def); len(kept) != 0 {
		t.Fatal("a 1m close ABOVE the range must not open the short side")
	}

	bars = open5mTape14Sep(8, 45) // the 08:40 5m bucket closed 28,941
	if kept, ref, _ := open5mRangeFilter(long(), h, bars, auditMs(2026, 9, 14, 8, 45, 0), old); len(kept) != 1 || len(ref) != 0 {
		t.Fatalf("5m_close at 08:45: kept %d refused %v, want allowed", len(kept), ref)
	}
	// A blank mode (a bare Config) reads as the course default.
	blank := def
	blank.CourseExits.Open5mBreak = ""
	if kept, _, _ := open5mRangeFilter(long(), h, open5mTape14Sep(8, 44), now, blank); len(kept) != 1 {
		t.Fatal("a blank mentor_open5m_break must read as 1m_close")
	}
}

// TestM61NewsWaitBothLines — G56 D4.4-R22: "Đợi canh nó nằm trên hai cái đường
// line này, kiếm inside bar đu vô… Hoặc anh chị chỉ cần đợi nó nằm ở trên cái
// đường line 4 giờ" [D4.4 p1 @21:48–22:12]. Default both: a long needs the
// close above the 4h line AND the pre-print 1h line; with no 1h line (D4.4-R12
// case 2, "1h ko có gì hết") the 4h alone. 4h_only = the pre-#61 rule.
// MUTANT: drop the 1h check in newsWaitFilter → the 29,350 long passes → RED.
func TestM61NewsWaitBothLines(t *testing.T) {
	def := DefaultConfig()
	if def.CourseExits.NewsWaitMode != NewsWaitBoth || !def.NewsWaitBothLines {
		t.Fatalf("news wait defaults: mode %q on=%v, want both / ON", def.CourseExits.NewsWaitMode, def.NewsWaitBothLines)
	}
	now := auditMs(2026, 9, 11, 7, 50, 0) // after the 07:30 print, before the open
	def.PrintWindows = []PrintWindow{{FromMs: auditMs(2026, 9, 11, 7, 20, 0), ToMs: auditMs(2026, 9, 11, 7, 35, 0), PrintAtMs: auditMs(2026, 9, 11, 7, 30, 0)}}
	four := TriggerLine{Dir: SideLong, Price: 29342.25, MovedAt: auditMs(2026, 9, 10, 23, 0, 0)}
	one := TriggerLine{Dir: SideLong, Price: 29360, MovedAt: auditMs(2026, 9, 11, 4, 0, 0)}
	long := []Intent{{Action: PlaceStopLimitEntry, Setup: "ISB", Side: SideLong, Reason: "ISB", Price: 29370, Stop: 29355, Target: 29400}}
	run := func(cfg Config, h HTF, close float64) (int, []string) {
		kept, ref := newsWaitFilter(long, h, close, now, cfg)
		return len(kept), ref
	}
	both := HTF{FourH: four, OneH: one}
	if n, ref := run(def, both, 29350); n != 0 || len(ref) != 1 || ref[0] != "news_wait_1h_line" {
		t.Fatalf("both, close 29,350 (above the 4h, below the 1h): kept %d refused %v, want news_wait_1h_line", n, ref)
	}
	if n, ref := run(def, both, 29365); n != 1 {
		t.Fatalf("both, close 29,365 (above both lines): kept %d refused %v, want allowed", n, ref)
	}
	if n, ref := run(def, both, 29340); n != 0 || len(ref) != 1 || ref[0] != "news_wait_4h_line" {
		t.Fatalf("both, close 29,340 (below the 4h): refused %v, want news_wait_4h_line", ref)
	}
	if n, ref := run(def, HTF{FourH: four}, 29350); n != 1 {
		t.Fatalf("no 1h line: the 4h alone (D4.4-R12): kept %d refused %v, want allowed", n, ref)
	}
	stale := one
	stale.MovedAt = auditMs(2026, 9, 10, 20, 0, 0) // fired BEFORE the 4h moved — "long before", irrelevant (D4.4-R12)
	if n, ref := run(def, HTF{FourH: four, OneH: stale}, 29350); n != 1 {
		t.Fatalf("a stale 1h (older than the 4h): the 4h alone: kept %d refused %v, want allowed", n, ref)
	}
	old := def
	old.CourseExits.NewsWaitMode = NewsWait4hOnly
	if n, ref := run(old, both, 29350); n != 1 {
		t.Fatalf("4h_only, close 29,350: kept %d refused %v, want allowed (pre-#61)", n, ref)
	}
	// Mirror: a short needs the close below BOTH lines.
	fourS := TriggerLine{Dir: SideShort, Price: 29360, MovedAt: four.MovedAt}
	oneS := TriggerLine{Dir: SideShort, Price: 29342.25, MovedAt: one.MovedAt}
	shortIn := []Intent{{Action: PlaceStopLimitEntry, Setup: "ISB", Side: SideShort, Reason: "ISB", Price: 29330, Stop: 29345, Target: 29300}}
	if kept, ref := newsWaitFilter(shortIn, HTF{FourH: fourS, OneH: oneS}, 29350, now, def); len(kept) != 0 || len(ref) != 1 || ref[0] != "news_wait_1h_line" {
		t.Fatalf("short, close 29,350 below the 4h but above the 1h: kept %d refused %v, want news_wait_1h_line", len(kept), ref)
	}
}

// TestM61NewsWaitBothLinesAtTick — the CE-3 production call site (Tick):
// the 1h line above the close refuses the long and counts news_wait_1h_line.
// MUTANT: pass a 4h_only config in Tick → RED.
func TestM61NewsWaitBothLinesAtTick(t *testing.T) {
	for _, mode := range []string{NewsWaitBoth, NewsWait4hOnly} {
		e, bars, now := mtfFixture(t, false)
		e.Cfg.FifteenMinIBRule = FifteenMinOff
		e.Cfg.NewsWaitBothLines = true
		e.Cfg.CourseExits.NewsWaitMode = mode
		e.Cfg.PrintWindows = []PrintWindow{{FromMs: now - 20*60_000, ToMs: now + 5*60_000}}
		// 4h line well under the close (passes); 1h line just above the 104 close.
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}, OneH: TriggerLine{Dir: SideLong, Price: 104.25}}
		got := longISBEmitted(e.Tick(bars, now))
		want := mode == NewsWait4hOnly
		if got != want {
			t.Fatalf("mode %s: emitted=%v, want %v (refusals %v, 1h %+v)", mode, got, want, e.State.Refusals, e.State.HTF.OneH)
		}
		if !want && e.State.Refusals["news_wait_1h_line"] == 0 {
			t.Fatalf("mode %s: the refusal must be counted news_wait_1h_line: %v", mode, e.State.Refusals)
		}
	}
}

// TestM61NewsFreezeStartsThirtyBefore — G58 D4.4-R6: "Một giờ thì đợi gần trước
// news, tầm 30 phút" / board "(trước news 30')" [D4.4 p1 @05:28–05:36; board
// 14:33–15:36]. 07:30 print: the 1h/4h read freezes from 07:00 — a 07:05 1m bar
// that breaks the 1h line does not flip it; a 06:55 bar does. The order hold
// (the window 07:20–07:35) is unchanged.
// MUTANT: ignore PrintAtMs in newsHTFWindows → the 07:05 break flips the 1h → RED.
func TestM61NewsFreezeStartsThirtyBefore(t *testing.T) {
	if DefaultConfig().CourseExits.NewsReadLeadMin != 30 {
		t.Fatal("mentor_news_read_lead_min default must be 30 (D4.4-R6)")
	}
	w := PrintWindow{
		FromMs:    auditMs(2026, 9, 15, 7, 20, 0),
		ToMs:      auditMs(2026, 9, 15, 7, 35, 0),
		PrintAtMs: auditMs(2026, 9, 15, 7, 30, 0),
	}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PrintWindows = []PrintWindow{w}
	for _, tc := range []struct {
		hh, mm int
		frozen bool
	}{{6, 59, false}, {7, 0, true}, {7, 19, true}, {8, 30, true}, {8, 31, false}} {
		if got := newsHTFFrozen(cfg, auditMs(2026, 9, 15, tc.hh, tc.mm, 0)); got != tc.frozen {
			t.Fatalf("newsHTFFrozen(%02d:%02d) = %v, want %v", tc.hh, tc.mm, got, tc.frozen)
		}
	}
	if newsBarFrozen(auditMs(2026, 9, 15, 6, 59, 0), cfg) || !newsBarFrozen(auditMs(2026, 9, 15, 7, 0, 0), cfg) {
		t.Fatal("the seed's per-bar freeze must start at 07:00 (print − 30m)")
	}
	// The news PERIOD (CE-3 wait, the stored htf_gate_news_only window) still
	// starts at the print window — the lead moves the READ only.
	if HTFGateActiveNewsPeriodStart := newsPeriodEnd(w); HTFGateActiveNewsPeriodStart != auditMs(2026, 9, 15, 8, 30, 0) {
		t.Fatalf("news period end moved: %d", HTFGateActiveNewsPeriodStart)
	}
	// A window with no PrintAtMs (pre-#61 / a fixture) keeps the 07:20 start.
	legacy := cfg
	legacy.PrintWindows = []PrintWindow{{FromMs: w.FromMs, ToMs: w.ToMs}}
	if newsHTFFrozen(legacy, auditMs(2026, 9, 15, 7, 5, 0)) {
		t.Fatal("no PrintAtMs: the freeze must start at the window (07:20)")
	}
	// Lead 0 = the pre-#61 07:20 start.
	zero := cfg
	zero.CourseExits.NewsReadLeadMin = 0
	if newsHTFFrozen(zero, auditMs(2026, 9, 15, 7, 5, 0)) {
		t.Fatal("lead 0: the freeze must start at the window (07:20)")
	}

	// The Tick call site: flat 05:00… tape, one bar breaks the prior 1h high.
	run := func(c Config, breakHH, breakMM int) TriggerLine {
		e := New(c)
		start := auditMs(2026, 9, 15, 5, 0, 0)
		brk := auditMs(2026, 9, 15, breakHH, breakMM, 0)
		var bars []market.Kline
		for o := start; o <= brk; o += 60_000 {
			b := market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: 100.5, High: 101, Low: 100, Close: 100.5}
			if o == brk {
				b.High, b.Close = 110, 109
			}
			bars = append(bars, b)
			if o >= auditMs(2026, 9, 15, 6, 50, 0) { // tick minute by minute from 06:50
				e.Tick(bars, b.CloseTime+1)
			}
		}
		return e.State.HTF.OneH
	}
	if one := run(cfg, 7, 5); one.Dir == SideLong {
		t.Fatalf("lead 30: the 07:05 break must NOT move the 1h line (read frozen from 07:00), got %+v", one)
	}
	if one := run(zero, 7, 5); one.Dir != SideLong {
		t.Fatalf("lead 0 (pre-#61): the 07:05 break moves the 1h line, got %+v", one)
	}
	if one := run(cfg, 6, 55); one.Dir != SideLong {
		t.Fatalf("lead 30: the 06:55 break (before the read) must move the 1h line, got %+v", one)
	}
}
