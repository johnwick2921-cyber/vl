package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// m466 P2 (release #61) — levels and touches. Each test is the course example
// (or its shape) the rule is built from; every rule carries a knob back to the
// pre-#61 behaviour, and the knob-off leg pins that.

// ---------------------------------------------------------------- helpers

func m61Bar(base int64, i int, o, h, l, c float64) market.Kline {
	ot := base + int64(i)*60_000
	return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
}

const m61UnoLevel = 29104.25

var m61UnoKey = "key_level:29104.25"

// m61UnoEvaluator is the D1.3-R21 drawing's evaluator: the key level
// 29,104.25, an old low 29,040 under it (the PLH target side), the 4h trigger
// SHORT (so the support reject at visit 1 cannot arm a long — the test is about
// the level's role, not the long).
func m61UnoEvaluator(t *testing.T, uno string) (*Evaluator, int64) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = false
	cfg.UnoKeyLevels = uno
	cfg.RollingPHL = false // the m464 rolling ladder is another path (its rungs would spend the leg budget)
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = []Level{
		{Key: "old-low:29040.00", Kind: KindOldExtreme, Role: OldLow, Price: 29040},
		{Key: m61UnoKey, Kind: KindKeyLevel, Price: m61UnoLevel},
	}
	e.State.Trigger = TriggerLine{Dir: SideShort, Price: 29200}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideShort, Price: 29300}}
	base := auditMs(2026, 9, 15, 10, 0, 0)
	return e, base
}

// m61UnoHistory: the tape BEFORE the first tick — the prior high 29,160 (the
// leg-start high a PLH's lower-high test reads), the old low 29,040, the rally
// back over the level. Touches are classified only on ticked candles. The
// timeline keeps the last two closed 5m candles at the backtest from forming an
// ISB pair (the 5m ISB box ban is another rule).
func m61UnoHistory(base int64) []market.Kline {
	return []market.Kline{
		m61Bar(base, 0, 29150, 29160, 29140, 29150),
		m61Bar(base, 1, 29150, 29152, 29040, 29050),
		m61Bar(base, 2, 29050, 29130, 29048, 29125),
		m61Bar(base, 3, 29125, 29135, 29118, 29130),
		m61Bar(base, 4, 29130, 29134, 29120, 29130),
	}
}

// m61Tick ticks one more candle, isolating the PHL/PLH gates from the ISB
// rest boxes (the 5m/15m/30m box bans are other rules).
func m61Tick(e *Evaluator, bars []market.Kline, b market.Kline) ([]market.Kline, []Intent) {
	bars = append(append([]market.Kline{}, bars...), b)
	now := b.CloseTime + 1
	e.State.ISBBox, e.State.ISBBox15m, e.State.ISBBox30m = nil, nil, nil
	e.State.ORB = ORB{Day: dayStartCT(now), High: 29300, Low: 29200, Drawn: true, Escaped: SideShort}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return bars, e.Tick(bars, now)
}

func m61Ticks(e *Evaluator, bars []market.Kline, bs ...market.Kline) ([]market.Kline, []Intent) {
	var ints []Intent
	for _, b := range bs {
		bars, ints = m61Tick(e, bars, b)
	}
	return bars, ints
}

func m61PLH(ints []Intent) (Intent, bool) {
	for _, in := range ints {
		if in.Action == PlaceStopEntry && in.Setup == "PLH" && in.Side == SideShort {
			return in, true
		}
	}
	return Intent{}, false
}

// m61UnoHeld: 10:05 visit 1 touches from above and closes back above (29,110)
// — the level HELD; 10:06 departs (Low over the level); 10:07–10:09 stay over.
func m61UnoHeld(base int64) []market.Kline {
	return []market.Kline{
		m61Bar(base, 5, 29130, 29132, 29100, 29110),
		m61Bar(base, 6, 29110, 29125, 29108, 29120),
		m61Bar(base, 7, 29120, 29126, 29110, 29122),
		m61Bar(base, 8, 29122, 29124, 29110, 29118),
		m61Bar(base, 9, 29118, 29120, 29106, 29112),
	}
}

// m61UnoUnder: 10:11–10:14 under the level, every high short of it.
func m61UnoUnder(base int64) []market.Kline {
	return []market.Kline{
		m61Bar(base, 11, 29090, 29095, 29080, 29088),
		m61Bar(base, 12, 29088, 29098, 29085, 29095),
		m61Bar(base, 13, 29095, 29099, 29088, 29092),
		m61Bar(base, 14, 29092, 29098, 29086, 29095),
	}
}

// m61UnoBacktest: 10:15 from BELOW — H 29,105, closes 29,100 under the level.
func m61UnoBacktest(base int64) market.Kline {
	return m61Bar(base, 15, 29095, 29105, 29096, 29100)
}

// m61UnoRun plays the drawing: visit 1 holds, departs, visit 2's first touch
// breaks (10:10 closes 29,090), price stays under, and the backtest from below
// rejects.
func m61UnoRun(t *testing.T, uno string) (e *Evaluator, afterBreak State, backtest []Intent) {
	t.Helper()
	e, base := m61UnoEvaluator(t, uno)
	bars := m61UnoHistory(base)
	held := m61UnoHeld(base)
	bars, _ = m61Tick(e, bars, held[0])
	if e.State.Touches[m61UnoKey].Outcome != TouchReject || e.State.Visits[m61UnoKey] != 1 {
		t.Fatalf("precondition: visit 1 must be a reject (held), touch=%+v visits=%v", e.State.Touches[m61UnoKey], e.State.Visits)
	}
	bars, _ = m61Ticks(e, bars, held[1:]...)
	bars, _ = m61Tick(e, bars, m61Bar(base, 10, 29112, 29114, 29085, 29090)) // visit 2: first touch closes THROUGH (29,090)
	afterBreak = e.State
	afterBreak.ISBOnly = copyBoolMap(e.State.ISBOnly)
	afterBreak.UnoFlipped = copyBoolMap(e.State.UnoFlipped)
	bars, _ = m61Ticks(e, bars, m61UnoUnder(base)...)
	_, backtest = m61Tick(e, bars, m61UnoBacktest(base))
	return e, afterBreak, backtest
}

func copyBoolMap(m map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------- G2

// TestM61UnoKeyLevelBacktest — G2 D1.3-R21: "Hỗ trợ bị phá → sẽ thành kháng
// cự khi backtest" [D1.3 p1 @12:31–12:58]. A key level that HELD (visit 1
// rejected) and then broke is NOT ISB-only: it flips (level_broken_uno) and
// the backtest reject from below arms a PLH sell stop under the candle.
// RED before #61: the break set ISBOnly → no PLH.
func TestM61UnoKeyLevelBacktest(t *testing.T) {
	e, afterBreak, backtest := m61UnoRun(t, UnoKeyLevelsOn)
	if afterBreak.ISBOnly[m61UnoKey] {
		t.Fatal("a key level that HELD and then broke must not turn ISB-only — Uno Reverse [D1.3 p1 @12:31–12:58]")
	}
	if !afterBreak.UnoFlipped[m61UnoKey] || e.State.Counters["level_broken_uno"] != 1 {
		t.Fatalf("the break must flip the level once and count level_broken_uno: flipped=%v counters=%v", afterBreak.UnoFlipped, e.State.Counters)
	}
	in, ok := m61PLH(backtest)
	if !ok {
		t.Fatalf("the backtest reject from below must arm a PLH sell stop; refusals=%v limits=%v counters=%v", e.State.Refusals, e.State.Limits.Refusals, e.State.Counters)
	}
	if in.AnchorKey != m61UnoKey || in.Price != 29096-e.Cfg.PHLEntryBufferPts || in.Stop != 29105+e.Cfg.PHLStopBufferPts {
		t.Fatalf("PLH must sit under the backtest candle (29,096 − 0.50, stop 29,105 + 0.50) at the flipped level: %+v", in)
	}
	if in.Target >= in.Price || in.Target < 29040 {
		t.Fatalf("target must be toward the old low 29,040: %+v", in)
	}
}

// TestM61UnoFirstVisitBreakIsISBOnly — a key level whose FIRST visit closes
// through never held: D5.2-R22 "cây nến chạm đầu tiên đóng ở trên level…
// invalid" [D5.2 p1 @19:44–20:25] → ISB-only, no flip.
func TestM61UnoFirstVisitBreakIsISBOnly(t *testing.T) {
	e, base := m61UnoEvaluator(t, UnoKeyLevelsOn)
	bars := m61UnoHistory(base)
	m61Tick(e, bars, m61Bar(base, 5, 29130, 29132, 29085, 29090))
	if !e.State.ISBOnly[m61UnoKey] || e.State.UnoFlipped[m61UnoKey] || e.State.Counters["level_broken_uno"] != 0 {
		t.Fatalf("a wrong-way FIRST visit is ISB-only, never a flip: isbonly=%v flipped=%v counters=%v", e.State.ISBOnly, e.State.UnoFlipped, e.State.Counters)
	}
}

// TestM61UnoSecondBreakAfterFlipIsISBOnly — after the flip, a wrong-way close
// of the flipped role ("missed" (D5.2-R24) [D5.2 p1 @20:31–21:43]) is ISB-only: one flip per day.
func TestM61UnoSecondBreakAfterFlipIsISBOnly(t *testing.T) {
	e, base := m61UnoEvaluator(t, UnoKeyLevelsOn)
	bars := m61UnoHistory(base)
	bars, _ = m61Ticks(e, bars, m61UnoHeld(base)...)
	bars, _ = m61Tick(e, bars, m61Bar(base, 10, 29112, 29114, 29085, 29090)) // flip
	bars, _ = m61Tick(e, bars, m61Bar(base, 11, 29090, 29095, 29080, 29088))
	m61Tick(e, bars, m61Bar(base, 12, 29088, 29112, 29086, 29110)) // backtest from below closes ABOVE: the flipped role failed
	if !e.State.ISBOnly[m61UnoKey] {
		t.Fatalf("a wrong-way close after the flip must be ISB-only; touch=%+v", e.State.Touches[m61UnoKey])
	}
	if e.State.Counters["level_broken_uno"] != 1 {
		t.Fatalf("one flip per day: level_broken_uno=%d", e.State.Counters["level_broken_uno"])
	}
}

// TestM61UnoSameVisitBreak — the drawing's other shape: the reject candle held
// and a LATER candle of the SAME visit closes through. The visit never ends on
// its own (every candle under a support still "touches" it from above), so
// without the same-visit read the backtest could never classify.
func TestM61UnoSameVisitBreak(t *testing.T) {
	e, base := m61UnoEvaluator(t, UnoKeyLevelsOn)
	bars := m61UnoHistory(base)
	// 10:05 holds; 10:06–10:09 keep touching (every low at or under the level,
	// every close back over it) — ONE visit; 10:10 closes through.
	bars, _ = m61Ticks(e, bars,
		m61Bar(base, 5, 29130, 29132, 29100, 29110),
		m61Bar(base, 6, 29110, 29118, 29103, 29112),
		m61Bar(base, 7, 29112, 29120, 29104, 29115),
		m61Bar(base, 8, 29115, 29119, 29102, 29110),
		m61Bar(base, 9, 29110, 29116, 29104, 29112))
	if e.State.Visits[m61UnoKey] != 1 || e.State.Touches[m61UnoKey].Outcome != TouchReject {
		t.Fatalf("precondition: one held visit, visits=%v touch=%+v", e.State.Visits, e.State.Touches[m61UnoKey])
	}
	bars, _ = m61Tick(e, bars, m61Bar(base, 10, 29112, 29114, 29085, 29090)) // same visit: closes through
	if e.State.ISBOnly[m61UnoKey] || !e.State.UnoFlipped[m61UnoKey] || e.State.Counters["level_broken_uno"] != 1 {
		t.Fatalf("a held visit closing through must flip: isbonly=%v flipped=%v counters=%v", e.State.ISBOnly, e.State.UnoFlipped, e.State.Counters)
	}
	bars, _ = m61Ticks(e, bars, m61UnoUnder(base)...)
	_, ints := m61Tick(e, bars, m61UnoBacktest(base))
	if _, ok := m61PLH(ints); !ok {
		t.Fatalf("the backtest after a same-visit break must arm the PLH; refusals=%v limits=%v", e.State.Refusals, e.State.Limits.Refusals)
	}
}

// TestM61UnoKnobOff — knob off: the pre-#61 behaviour, the break is ISB-only
// and the backtest arms nothing.
func TestM61UnoKnobOff(t *testing.T) {
	e, afterBreak, backtest := m61UnoRun(t, UnoKeyLevelsOff)
	if !afterBreak.ISBOnly[m61UnoKey] || e.State.Counters["level_broken_uno"] != 0 {
		t.Fatalf("knob off: the break is ISB-only as before: isbonly=%v counters=%v", afterBreak.ISBOnly, e.State.Counters)
	}
	if _, ok := m61PLH(backtest); ok {
		t.Fatal("knob off: no PLH at an ISB-only level")
	}
}

// TestM61UnoFlipResetsAtDayRoll — UnoFlipped belongs to the session day, like
// Visits.
func TestM61UnoFlipResetsAtDayRoll(t *testing.T) {
	e, _, _ := m61UnoRun(t, UnoKeyLevelsOn)
	if !e.State.UnoFlipped[m61UnoKey] {
		t.Fatal("precondition: flipped")
	}
	base := auditMs(2026, 9, 16, 9, 0, 0)
	bars := []market.Kline{m61Bar(base, 0, 29100, 29101, 29099, 29100), m61Bar(base, 1, 29100, 29101, 29099, 29100)}
	m61Tick(e, bars[:1], bars[1])
	if len(e.State.UnoFlipped) != 0 {
		t.Fatalf("a new session day clears UnoFlipped: %v", e.State.UnoFlipped)
	}
}

// ---------------------------------------------------------------- G10 / G11

// m61WeekendTape is D3.1's weekend: Friday 11 Sep 2026 closes (the last 1m close
// = friClose), Sunday 13 Sep 17:00 opens with a 5m candle H 29,136.00 L 29,000.00
// [D3.1 p1 @07:48–08:13, @11:58–12:22], then price drifts down to 29,016 at
// 18:30 with every later high under the open wick and every low over 29,000.
func m61WeekendTape(friClose float64) []market.Kline {
	fri := auditMs(2026, 9, 11, 15, 50, 0)
	var bars []market.Kline
	for i := 0; i < 10; i++ {
		c := friClose + float64(9-i)*0.25
		if i == 9 {
			c = friClose
		}
		bars = append(bars, m61Bar(fri, i, c+0.5, c+1, c-1, c))
	}
	sun := auditMs(2026, 9, 13, 17, 0, 0)
	open := []market.Kline{
		m61Bar(sun, 0, 29130, 29136, 29100, 29110),
		m61Bar(sun, 1, 29110, 29120, 29050, 29060),
		m61Bar(sun, 2, 29060, 29070, 29000, 29020),
		m61Bar(sun, 3, 29020, 29060, 29010, 29055),
		m61Bar(sun, 4, 29055, 29100, 29050, 29095),
	}
	bars = append(bars, open...)
	// 17:05 → 18:30: a steady drift from 29,095 down to 29,016 (descending
	// highs and lows: no fractal pivot can form above price).
	n := 86
	for i := 0; i < n; i++ {
		c := 29095 - float64(i+1)*(29095-29016)/float64(n)
		c = math.Round(c*4) / 4
		o := c + 0.75
		bars = append(bars, m61Bar(sun, 5+i, o, o+0.25, c-0.25, c))
	}
	return bars
}

func m61Find(levels []Level, kind LevelKind, price float64) (Level, bool) {
	for _, l := range levels {
		if l.Kind == kind && l.Price == price {
			return l, true
		}
	}
	return Level{}, false
}

// TestM61SessionOpenWickIsOldHigh — G10 D3.1-R26: "đây là mới lúc open… cái
// râu này nó sẽ là cái đỉnh" (29,136.00, Sun 13 Sep 17:00) [D3.1 p1
// @11:58–12:22]. The open candle's left neighbour is Friday's (higher), so the
// fractal never makes it a pivot. RED before #61: absent.
func TestM61SessionOpenWickIsOldHigh(t *testing.T) {
	bars := m61WeekendTape(29388.50)
	last := bars[len(bars)-1]
	if last.Close != 29016 {
		t.Fatalf("fixture: last close %.2f, want 29,016", last.Close)
	}
	if _, hh, mm := ctOf(last.OpenTime); hh != 18 || mm != 30 {
		t.Fatalf("fixture: last bar %02d:%02d CT, want 18:30", hh, mm)
	}
	now := last.CloseTime + 1
	cfg := DefaultConfig()
	cfg.Enabled = true
	l, ok := m61Find(Levels(bars, cfg, now), KindOldExtreme, 29136)
	if !ok || l.Role != OldHigh {
		t.Fatalf("the session-open wick 29,136.00 must be THE old high; levels=%+v", Levels(bars, cfg, now))
	}
	if lo, ok := m61Find(Levels(bars, cfg, now), KindOldExtreme, 29000); !ok || lo.Role != OldLow {
		t.Fatalf("the session-open low 29,000.00 must be the old low (\"lấy cái low này làm chuẩn\" [@11:32–11:48]); levels=%+v", Levels(bars, cfg, now))
	}
	cfg.SessionOpenLevels = SessionOpenLevelsOff
	if _, ok := m61Find(Levels(bars, cfg, now), KindOldExtreme, 29136); ok {
		t.Fatal("knob off: the open wick is no old high (the pre-#61 fractal read)")
	}
}

// TestM61SessionOpenWickBroken — the open pivot obeys the ordinary unbroken
// test: a later candle above 29,136 removes it.
func TestM61SessionOpenWickBroken(t *testing.T) {
	bars := m61WeekendTape(29388.50)
	sun := auditMs(2026, 9, 13, 17, 0, 0)
	bars[10+6] = m61Bar(sun, 6, 29090, 29137, 29085, 29090)
	now := bars[len(bars)-1].CloseTime + 1
	cfg := DefaultConfig()
	cfg.Enabled = true
	if _, ok := m61Find(Levels(bars, cfg, now), KindOldExtreme, 29136); ok {
		t.Fatal("a broken open wick is no old high")
	}
}

// TestM61GapEdgeLevel — G11 D3.1-R20: "Cái cạnh dưới của cái gap, lúc nào nó
// cũng sẽ là resistance… Level là resistance" [D3.1 p1 @09:41–09:51]; the gap
// from Friday's last close 29,391.25 down to the Sunday open candle's high.
// RED before #61: no gap level kind at all.
func TestM61GapEdgeLevel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	bars := m61WeekendTape(29391.25)
	now := bars[len(bars)-1].CloseTime + 1
	l, ok := m61Find(Levels(bars, cfg, now), KindGapEdge, 29391.25)
	if !ok {
		t.Fatalf("the gap's far edge 29,391.25 must be a level; got %+v", gapEdgeLevels(bars, cfg, now))
	}
	if !levelIsLocation(l, nil) {
		t.Fatal("the gap edge is a LOCATION (\"Level là resistance\")")
	}
	if nextLevelBeyond([]Level{l}, 29100, SideLong) != 29391.25 {
		t.Fatal("the gap edge is a target / obstacle")
	}

	// a 12-pt gap is under the D5.3-R24 prune distance (20): no far edge.
	small := m61WeekendTape(29148)
	if got := gapEdgeLevels(small, cfg, small[len(small)-1].CloseTime+1); len(got) != 0 {
		t.Fatalf("a 12-pt gap makes no far-edge level: %+v", got)
	}

	// a later 1m candle trading through 29,391.25 fills the gap: removed.
	filled := append(append([]market.Kline{}, bars...),
		m61Bar(bars[len(bars)-1].OpenTime+60_000, 0, 29380, 29392, 29379, 29385),
		m61Bar(bars[len(bars)-1].OpenTime+120_000, 0, 29385, 29386, 29380, 29382))
	if got := gapEdgeLevels(filled, cfg, filled[len(filled)-1].CloseTime+1); len(got) != 0 {
		t.Fatalf("a 1m High 29,392 fills the gap: the edge must go, got %+v", got)
	}
	// a touch AT the edge is not a fill.
	touched := append(append([]market.Kline{}, bars...),
		m61Bar(bars[len(bars)-1].OpenTime+60_000, 0, 29380, 29391.25, 29379, 29385),
		m61Bar(bars[len(bars)-1].OpenTime+120_000, 0, 29385, 29386, 29380, 29382))
	if got := gapEdgeLevels(touched, cfg, touched[len(touched)-1].CloseTime+1); len(got) != 1 {
		t.Fatalf("a touch AT the edge is the touch the level exists for, not a fill: %+v", got)
	}

	// a contract roll between the sessions is no gap (the roll spread).
	rolled := append([]market.Kline{}, bars...)
	for i := range rolled {
		rolled[i].Contract = "MNQ 09-26"
		if i < 10 {
			rolled[i].Contract = "MNQ 12-26"
		}
	}
	if got := gapEdgeLevels(rolled, cfg, now); len(got) != 0 {
		t.Fatalf("a contract change at the session boundary makes no gap edge: %+v", got)
	}

	cfg.SessionOpenLevels = SessionOpenLevelsOff
	if _, ok := m61Find(Levels(bars, cfg, now), KindGapEdge, 29391.25); ok {
		t.Fatal("knob off: no gap edge")
	}
}

// TestM61GapEdgeSeededPath — the evaluator's own (seeded) level set carries the
// open pivots and the gap edge too.
func TestM61GapEdgeSeededPath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	bars := m61WeekendTape(29391.25)
	now := bars[len(bars)-1].CloseTime + 1
	lv := e.seededLevels(bars, now)
	if _, ok := m61Find(lv, KindGapEdge, 29391.25); !ok {
		t.Fatal("seeded path: gap edge missing")
	}
	if _, ok := m61Find(lv, KindOldExtreme, 29136); !ok {
		t.Fatal("seeded path: open wick old high missing")
	}
}

// ---------------------------------------------------------------- G17

// TestM61TrendlineArmCancelsOnCurrentLine — G17 D2.3-R34: "Mình làm gì? Mình
// cancel. Cancel. Chẳng làm gì khác hết" [D2.3 p1 @23:35–24:03]. A buy stop
// placed at 10:00 on a RISING support line (29,375 then; 29,384 at 10:20); the
// 10:20 candle closes 29,380 — under the CURRENT line, over the frozen
// placement price. RED before #61: no cancel.
func TestM61TrendlineArmCancelsOnCurrentLine(t *testing.T) {
	p0 := auditMs(2026, 9, 15, 10, 0, 0)
	p1 := auditMs(2026, 9, 15, 10, 20, 0)
	tl := Trendline{Side: SideLong, P0Px: 29375, P0T: p0, P1Px: 29384, P1T: p1, ValidAt: 1}
	key := tl.key()
	run := func(mode string, lines []Trendline) ([]Intent, *Evaluator) {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.LevelArmLineCancel = mode
		e := New(cfg)
		e.State.LevelArms = map[string]LevelArm{key: {ArmID: "lvl-7", Side: SideLong, LevelPrice: 29375, PlacedAt: p0 + 59_999}}
		cur := market.Kline{OpenTime: p1, CloseTime: p1 + 59_999, Open: 29386, High: 29387, Low: 29379, Close: 29380}
		levels := []Level{{Key: key, Kind: KindTrendline, Price: tl.priceAt(cur.OpenTime)}}
		return e.levelArmCancels([]market.Kline{cur}, cur, cur.CloseTime+1, levels, lines), e
	}
	ints, e := run(LevelArmLineCancelCurrent, []Trendline{tl})
	if !hasCancel(ints, "lvl-7") || e.State.Counters["level_arm_cancel_current_line"] != 1 {
		t.Fatalf("a close 29,380 under the CURRENT line (~29,384) must cancel: %+v counters=%v", ints, e.State.Counters)
	}
	ints, _ = run(LevelArmLineCancelPlacement, []Trendline{tl})
	if hasCancel(ints, "lvl-7") {
		t.Fatal("placement_price (the pre-#61 read): 29,380 is over the frozen 29,375 — no cancel")
	}
	dead := tl
	dead.Dead = true
	ints, e = run(LevelArmLineCancelCurrent, []Trendline{dead})
	if !hasCancel(ints, "lvl-7") || e.State.Counters["level_arm_line_gone"] != 1 {
		t.Fatalf("a dead line cancels its resting order: %+v counters=%v", ints, e.State.Counters)
	}
	ints, e = run(LevelArmLineCancelCurrent, nil)
	if !hasCancel(ints, "lvl-7") || e.State.Counters["level_arm_line_gone"] != 1 {
		t.Fatalf("a line gone from the set cancels its resting order: %+v", ints)
	}
}

// TestM61KeyLevelArmUnchanged — a non-moving key keeps the placement price.
func TestM61KeyLevelArmUnchanged(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	p0 := auditMs(2026, 9, 15, 10, 0, 0)
	e.State.LevelArms = map[string]LevelArm{"L": {ArmID: "lvl-1", Side: SideLong, LevelPrice: 29375, PlacedAt: p0 + 59_999}}
	cur := market.Kline{OpenTime: p0 + 60_000, CloseTime: p0 + 119_999, Open: 29386, High: 29387, Low: 29379, Close: 29380}
	if ints := e.levelArmCancels([]market.Kline{cur}, cur, cur.CloseTime+1, []Level{{Key: "L", Kind: KindKeyLevel, Price: 29390}}, nil); len(ints) != 0 {
		t.Fatalf("a key level's arm reads its placement price: %+v", ints)
	}
}

// ---------------------------------------------------------------- G41

// TestM61PHLPullbackLowerLowRefused — G41 D2.2-R59: "nghĩa là anh đang đi kiếm
// một cái lệnh có đáy cao hơn đáy trước đó" [D2.2 p3 @08:25–09:25]. Prior low
// 29,338.00; the pullback after the old high printed 29,335.00; the signal
// candle's low 29,350 is higher — the signal-only check passed it.
func TestM61PHLPullbackLowerLowRefused(t *testing.T) {
	base := auditMs(2026, 9, 15, 10, 0, 0)
	mk := func(pull float64) []market.Kline {
		return []market.Kline{
			m61Bar(base, 0, 29400, 29431.75, 29395, 29425), // the old high
			m61Bar(base, 1, 29420, 29422, 29380, 29385),
			m61Bar(base, 2, 29385, 29390, pull, 29360),
			m61Bar(base, 3, 29360, 29372, 29350, 29368), // signal candle
		}
	}
	cfg := DefaultConfig()
	if got := phlPullbackRefusal(cfg, SideLong, mk(29335), 0, 3, mk(29335)[3], 29338); got != "phl_pullback_lower_low" {
		t.Fatalf("a pullback low 29,335 under the prior low 29,338 must refuse, got %q", got)
	}
	if got := phlPullbackRefusal(cfg, SideLong, mk(29345), 0, 3, mk(29345)[3], 29338); got != "" {
		t.Fatalf("a pullback low 29,345 passes to the next gate, got %q", got)
	}
	// the signal candle ITSELF under the prior low is phl_not_higher_low's
	// (downstream) — the pullback read leaves that name alone.
	sig := mk(29345)
	sig[3].Low = 29330
	if got := phlPullbackRefusal(cfg, SideLong, sig, 0, 3, sig[3], 29338); got != "" {
		t.Fatalf("a signal candle under the prior low is phl_not_higher_low's, got %q", got)
	}
	cfg.PHLPullbackLow = PHLPullbackLowSignal
	if got := phlPullbackRefusal(cfg, SideLong, mk(29335), 0, 3, mk(29335)[3], 29338); got != "" {
		t.Fatalf("signal (the pre-#61 read): passes, got %q", got)
	}
	// short mirror
	cfg.PHLPullbackLow = PHLPullbackLowPullback
	sh := []market.Kline{
		m61Bar(base, 0, 29300, 29305, 29268.25, 29275),
		m61Bar(base, 1, 29275, 29320, 29272, 29315),
		m61Bar(base, 2, 29308, 29310, 29300, 29305), // signal candle: its own high 29,310 is under the prior high
	}
	if got := phlPullbackRefusal(cfg, SideShort, sh, 0, 2, sh[2], 29318); got != "plh_pullback_higher_high" {
		t.Fatalf("mirror: a bounce high 29,320 over the prior high 29,318 refuses, got %q", got)
	}
	if got := phlRefIdx(sh, sh[1]); got != 1 {
		t.Fatalf("phlRefIdx: %d", got)
	}
}

// TestM61PHLPullbackLowerLowThroughTick — the evaluator call site: the m425a
// PHL (old high 29,431.75, level 29,385) with a pullback candle dipping to
// 29,330 — under the leg-start low 29,335 — before the touch candle.
func TestM61PHLPullbackLowerLowThroughTick(t *testing.T) {
	for _, mode := range []string{PHLPullbackLowPullback, PHLPullbackLowSignal} {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.PHLTargetShyPts = 6
		cfg.Exec2mAfter30m = false
		cfg.PHLPullbackLow = mode
		e := New(cfg)
		e.seeded = true
		e.State.Seed1mWatermark = math.MaxInt64
		e.State.Seed1HWatermark = math.MaxInt64
		e.State.SeedLevels = []Level{{Key: "old-high:29431.75", Kind: KindOldExtreme, Role: OldHigh, Price: 29431.75}, {Key: "L", Kind: KindKeyLevel, Price: 29385}}
		e.State.Trigger = TriggerLine{Dir: SideLong, Price: 29300}
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 29300}}
		bars := []market.Kline{
			rthBars(-1, 29340, 29420, 29335, 29415),
			rthBars(0, 29420, 29431.75, 29410, 29425),
			rthBars(1, 29405, 29408, 29330, 29402), // the pullback printed a LOWER low (29,330 < 29,335)
			rthBars(2, 29392, 29402, 29386, 29396),
			rthBars(3, 29390, 29392, 29385, 29391),
		}
		now := bars[4].CloseTime + 1
		e.State.ORB = ORB{Day: dayStartCT(now), High: 29300, Low: 29200, Drawn: true, Escaped: SideLong}
		e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
		ints := e.Tick(bars, now)
		placed := countPHLPlace(ints) > 0
		switch mode {
		case PHLPullbackLowPullback:
			if placed || e.State.Refusals["phl_pullback_lower_low"] != 1 {
				t.Fatalf("pullback: refused phl_pullback_lower_low, placed=%v refusals=%v", placed, e.State.Refusals)
			}
		case PHLPullbackLowSignal:
			if !placed {
				t.Fatalf("signal: the pre-#61 read places the PHL; refusals=%v", e.State.Refusals)
			}
		}
	}
}

// m466P2PreValues puts the four m466 P2 knobs at their pre-#61 values — the
// identity tests whose goldens predate #61 call it, proving every P2 change is
// undone by its knob.
func m466P2PreValues(cfg *Config) {
	cfg.UnoKeyLevels = UnoKeyLevelsOff
	cfg.SessionOpenLevels = SessionOpenLevelsOff
	cfg.LevelArmLineCancel = LevelArmLineCancelPlacement
	cfg.PHLPullbackLow = PHLPullbackLowSignal
}
