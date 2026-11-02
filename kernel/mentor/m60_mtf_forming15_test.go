package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// m465 P1 (release #60) — the EARLY 15m read: two churning 5m candles inside
// the last closed 15m candle are a 15m inside bar FORMING, boxed at once
// (provisional) instead of 5–10 minutes later at the 15m close.
//
//   - D4.2-R2 [D4.2 p1 @00:08–00:39]: "3 cây 5 phút mà nó cứ xào quần xào quần
//     ngay chỗ đó mà nó không đi đâu hết... ngay lập tức mình biết khung 15
//     phút đang tạo inside bar"; "2, 3 cây 5 phút anh chị đã có được 1 cây 15
//     phút" [@07:40].
//   - D4.2-R11 [D4.2 p1 @05:45, 07:20, 11:12, 11:40–11:42]: the trick — box it
//     early; the 5m and the 15m close together, so at the shared close the real
//     15m box takes over; "né được rất nhiều đoạn ranging".
//   - D4.3-R21 [D4.3 @12:20–12:32]: "2 cái nến 5 phút, 3 cái nến 5 phút, nó
//     đang làm cái nến 15 phút rồi đó" — the sideways read.
//
// WE-1 (Mon 14 Sep, D4.2 p1 @00:38–09:39): the 15m mother H29,026.00
// L28,970.75; the 5m candles that follow sit inside it.

// ── shared tape helpers (m60p1 prefix: P2–P5 add m60_* tests in parallel) ──

// m60p1At is the CT instant hh:mm on Mon 14 Sep 2026 (the WE-1/WE-2 day).
func m60p1At(h, m int) int64 {
	return time.Date(2026, time.September, 14, h, m, 0, 0, ctime()).UnixMilli()
}

func m60p1K(open int64, o, h, l, c float64) market.Kline {
	return market.Kline{OpenTime: open, CloseTime: open + 59_999, Open: o, High: h, Low: l, Close: c}
}

// m60p1Five splits one 5m candle into five 1m bars whose bodies march from the
// open to the close (so no 1m body leaves the 5m body) and whose extremes sit
// on the first / last bar: green → the low on bar 0 and the high on bar 4; red
// → the high on bar 0 and the low on bar 4.
func m60p1Five(open int64, o, h, l, c float64) []market.Kline {
	out := make([]market.Kline, 0, 5)
	prev := o
	for k := 0; k < 5; k++ {
		cl := o + (c-o)*float64(k+1)/5
		bo := prev
		hi := math.Max(bo, cl) + 0.25
		lo := math.Min(bo, cl) - 0.25
		if c >= o {
			if k == 0 {
				lo = l
			}
			if k == 4 {
				hi = h
			}
		} else {
			if k == 0 {
				hi = h
			}
			if k == 4 {
				lo = l
			}
		}
		hi = math.Min(hi, h)
		lo = math.Max(lo, l)
		out = append(out, m60p1K(open+int64(k)*60_000, bo, hi, lo, cl))
		prev = cl
	}
	return out
}

// m60p1Tape builds the 1m tape from 5m candles {h, m, o, h, l, c}.
func m60p1Tape(fives [][6]float64) []market.Kline {
	var bars []market.Kline
	for _, f := range fives {
		bars = append(bars, m60p1Five(m60p1At(int(f[0]), int(f[1])), f[2], f[3], f[4], f[5])...)
	}
	return bars
}

// m60p1Eval is an evaluator that passes every gate the tests do not pin: the
// 5m trigger and the 4h/1h lines are FROZEN (LastBucket = MaxInt64, so the
// tape never moves them), the day is a trade day, the ORB escaped `orb`, key
// levels sit far above and below, EMAs are off, and the exec frame is the 1m.
func m60p1Eval(t *testing.T, trig5 Side, trigPx float64, htf, orb Side, levels []Level) *Evaluator {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = false // the 1m pair is the entry read in every m60 P1 pin
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 9
	cfg.ISBReverseEMA9Enabled = false
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = levels
	day := m60p1At(9, 0)
	orbHi, orbLo := 28_780.0, 28_770.0 // below the tape: an ORB escaped long
	if orb == SideShort {
		orbHi, orbLo = 29_210, 29_200 // above the tape: an ORB escaped short
	}
	e.State.ORB = ORB{Day: dayStartCT(day), High: orbHi, Low: orbLo, Drawn: true, Escaped: orb}
	frozen := func(d Side, px float64) TriggerLine { return TriggerLine{Dir: d, Price: px, LastBucket: math.MaxInt64} }
	htfPx := 28_800.0
	if htf == SideShort {
		htfPx = 29_400
	}
	e.State.HTF = HTF{FourH: frozen(htf, htfPx), OneH: frozen(htf, htfPx)}
	e.State.Trigger = frozen(trig5, trigPx)
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(day).In(ctime())), Verdict: DayTrade}
	return e
}

// m60p1Run ticks the evaluator once per 1m bar (the live cadence) and returns
// the intents of the LAST tick. Live ISB arms from the synthetic tape are
// cleared before the last tick so ONE ARM PER SIDE never masks the pinned
// verdict (the pins are about the 15m / MTF gates, not the stacking).
func m60p1Run(e *Evaluator, bars []market.Kline) []Intent {
	for i := 0; i < len(bars)-1; i++ {
		e.Tick(bars[:i+1], BarCloseInstant(bars[i]))
	}
	e.State.ISBArms = map[string]ISBArm{}
	before := map[string]int{}
	for k, v := range e.State.Refusals {
		before[k] = v
	}
	out := e.Tick(bars, BarCloseInstant(bars[len(bars)-1]))
	m60p1LastDelta = map[string]int{}
	for k, v := range e.State.Refusals {
		if v != before[k] {
			m60p1LastDelta[k] = v - before[k]
		}
	}
	return out
}

// m60p1LastDelta is the refusal delta of m60p1Run's LAST tick (diagnostics in
// failure messages; the m60 P1 tests do not run in parallel).
var m60p1LastDelta map[string]int

func m60p1ISB(ins []Intent, side Side) *Intent {
	for i := range ins {
		in := ins[i]
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" && in.Side == side {
			return &ins[i]
		}
	}
	return nil
}

// m60p1WE1 is the WE-1 shape: a rising morning, the 09:30 15m mother GREEN
// O28,972 H29,026 L28,970.75 C29,020, then the 09:45 and 09:50 5m candles
// churning inside it (09:50 is itself a 5m inside bar of 09:45, long). The
// second 5m close is `close2`.
func m60p1WE1(close2 float64) []market.Kline {
	hi2 := math.Max(29_008, close2)
	return m60p1Tape([][6]float64{
		{9, 0, 28_900, 28_915, 28_895, 28_912},
		{9, 5, 28_912, 28_928, 28_908, 28_925},
		{9, 10, 28_925, 28_945, 28_920, 28_940},
		{9, 15, 28_950, 28_958, 28_940, 28_955},
		{9, 20, 28_955, 28_966, 28_950, 28_962},
		{9, 25, 28_962, 28_975, 28_958, 28_970},
		// the 09:30 15m mother: O28,972 H29,026 L28,970.75 C29,020 (green)
		{9, 30, 28_972, 28_990, 28_970.75, 28_988},
		{9, 35, 28_988, 29_010, 28_980, 29_005},
		{9, 40, 29_005, 29_026, 29_000, 29_020},
		// the forming 09:45 15m: two 5m candles that go nowhere
		{9, 45, 28_990, 29_020, 28_985, 29_005},
		{9, 50, 29_004, hi2, 28_995, close2},
	})
}

// m60p1Pair appends the 1m ISB pair 09:55 (mother) / 09:56 (inside) whose bodies
// sit ABOVE the 5m ISB box (29,008 — the 5m box escapes long, which satisfies
// require_5m_isb) but INSIDE the provisional 15m box (top 29,020).
func m60p1Pair(bars []market.Kline, side Side) []market.Kline {
	mother := m60p1K(m60p1At(9, 55), 29_016, 29_018, 29_009, 29_010) // red → short
	if side == SideLong {
		mother = m60p1K(m60p1At(9, 55), 29_010, 29_018, 29_009, 29_016) // green → long
	}
	child := m60p1K(m60p1At(9, 56), 29_012, 29_015, 29_010.5, 29_013)
	return append(append([]market.Kline{}, bars...), mother, child)
}

func m60p1Levels() []Level {
	return []Level{
		{Key: "K-below", Kind: KindKeyLevel, Price: 28_850},
		{Key: "K-above", Kind: KindKeyLevel, Price: 29_150},
	}
}

// TestM60Forming15mBoxRefusesCounterISB — D4.2-R2 / D4.3-R21: from 09:55 the
// forming 09:45 15m has two closed 5m candles whose combined body (O28,990 →
// C28,998) sits inside the 09:30 mother (28,970.75–29,026) → a PROVISIONAL long
// 15m box (28,985–29,020). A 1m SHORT ISB (09:55 red / 09:56 inside) is refused
// isb_15m_box_dir_forming; a LONG one at the same tick is allowed. Mutants: the
// 2nd 5m closes above 29,026 (body outside the mother) → no box; the knob
// 15m_early_read OFF → today (no forming refusal).
func TestM60Forming15mBoxRefusesCounterISB(t *testing.T) {
	// Short: refused by the provisional box.
	e := m60p1Eval(t, SideLong, 28_980, SideLong, SideLong, m60p1Levels())
	ins := m60p1Run(e, m60p1Pair(m60p1WE1(28_998), SideShort))
	if bx := e.State.ISBBox15m; bx == nil || !bx.Provisional || bx.Dir != SideLong || bx.High != 29_020 || bx.Low != 28_985 || bx.AtTime != m60p1At(9, 45) {
		t.Fatalf("D4.3-R21 sideways read: want a provisional LONG 15m box 28,985–29,020 at 09:45, got %+v", bx)
	}
	if in := m60p1ISB(ins, SideShort); in != nil {
		t.Fatalf("a short 1m ISB against the forming long 15m box must be refused, got %+v", *in)
	}
	if e.State.Refusals["isb_15m_box_dir_forming"] == 0 {
		t.Fatalf("want isb_15m_box_dir_forming counted; refusals %v", e.State.Refusals)
	}
	if e.State.Refusals["isb_box_blocked_15m"] != 0 {
		t.Fatalf("a provisional box must refuse under its _forming name only; refusals %v", e.State.Refusals)
	}

	// Long: allowed at the same tick — it passes every ISB gate, the 15m ones
	// included. (On this tape the WE-1 range has drawn an FTGH 29,005–29,026
	// around the coil, so the intent is then dropped by the box ban "never trade
	// inside the box" [D3.2 p1 @06:59] — a different rule, downstream; the emit
	// itself is pinned on the flat fixture below.)
	e = m60p1Eval(t, SideLong, 28_980, SideLong, SideLong, m60p1Levels())
	ins = m60p1Run(e, m60p1Pair(m60p1WE1(28_998), SideLong))
	if in := m60p1ISB(ins, SideLong); in == nil {
		for k := range m60p1LastDelta {
			if k != "inside_any_box" {
				t.Fatalf("a long 1m ISB with the forming long 15m box must pass the ISB gates; last-tick refusals %v", m60p1LastDelta)
			}
		}
	}
	if e.State.ISBBox15m == nil || !e.State.ISBBox15m.Provisional {
		t.Fatalf("the provisional box must still stand for the long case, got %+v", e.State.ISBBox15m)
	}
	// The emit path on the flat fixture: a provisional LONG 15m box + a long 5m
	// box → the long ISB goes out; a SHORT ISB → isb_15m_box_dir_forming.
	f, fb, now := mtfFixture(t, false)
	// the forming 15m bucket of the fixture's last bar (09:41) opened at 09:30
	f.State.ISBBox15m = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: fb[30].OpenTime, Provisional: true}
	f.State.LastForming15mAt = fb[30].OpenTime
	f.State.ISBBox = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: fb[0].OpenTime}
	if in := m60p1ISB(f.Tick(fb, now), SideLong); in == nil {
		t.Fatalf("flat fixture: the long ISB must emit with the forming long box; refusals %v", f.State.Refusals)
	} else if in.Confluence {
		t.Fatalf("a PROVISIONAL 15m box never counts for mode C, got %+v", *in)
	}
	f, fb, now = mtfFixture(t, true)
	// the forming 15m bucket of the fixture's last bar (09:41) opened at 09:30
	f.State.ISBBox15m = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: fb[30].OpenTime, Provisional: true}
	f.State.LastForming15mAt = fb[30].OpenTime
	if in := m60p1ISB(f.Tick(fb, now), SideShort); in != nil || f.State.Refusals["isb_15m_box_dir_forming"] == 0 {
		t.Fatalf("flat fixture: a short ISB against the forming long box → isb_15m_box_dir_forming; got %+v refusals %v", in, f.State.Refusals)
	}

	// The 2nd 5m closes ABOVE the mother high: the forming body is outside → no box.
	e = m60p1Eval(t, SideLong, 28_980, SideLong, SideLong, m60p1Levels())
	m60p1Run(e, m60p1Pair(m60p1WE1(29_030), SideShort))
	if e.State.ISBBox15m != nil && e.State.ISBBox15m.Provisional {
		t.Fatalf("a forming body outside the mother is no inside bar; got box %+v", *e.State.ISBBox15m)
	}
	if e.State.Refusals["isb_15m_box_dir_forming"] != 0 {
		t.Fatalf("no provisional box → no forming refusal; refusals %v", e.State.Refusals)
	}

	// Knob OFF → today: no provisional box, no forming refusal.
	e = m60p1Eval(t, SideLong, 28_980, SideLong, SideLong, m60p1Levels())
	e.Cfg.Early15mRead = false
	m60p1Run(e, m60p1Pair(m60p1WE1(28_998), SideShort))
	if e.State.ISBBox15m != nil {
		t.Fatalf("15m_early_read OFF must not box the forming 15m; got %+v", *e.State.ISBBox15m)
	}
	for k := range e.State.Refusals {
		if len(k) > 8 && k[len(k)-8:] == "_forming" {
			t.Fatalf("15m_early_read OFF must not refuse under a _forming name; refusals %v", e.State.Refusals)
		}
	}
}

// TestM60Forming15mReplacedAtClose — D4.2-R11: the 5m and the 15m close
// together; at that shared close the REAL ISBBox15m replaces the provisional
// one (same AtTime, built once), and a 15m candle that closes as NO inside bar
// drops the provisional box. The provisional box is built ONCE per forming
// bucket however many ticks it stands.
func TestM60Forming15mReplacedAtClose(t *testing.T) {
	inside := func(lastClose float64) []market.Kline {
		bars := m60p1WE1(28_998)
		// 09:55–09:59: the third 5m stays inside the provisional box (28,985–29,020)
		for i, c := range []float64{29_000, 29_004, 29_006, 29_010, lastClose} {
			o := 28_998.0
			if i > 0 {
				o = []float64{29_000, 29_004, 29_006, 29_010}[i-1]
			}
			bars = append(bars, m60p1K(m60p1At(9, 55+i), o, math.Max(o, c)+0.25, math.Min(o, c)-0.25, c))
		}
		return append(bars, m60p1K(m60p1At(10, 0), lastClose, lastClose+0.5, lastClose-0.5, lastClose))
	}

	// Confirmed: the 09:45 15m closes at 29,012 — body 28,990→29,012 inside the
	// mother → the real box (AtTime 09:45, not provisional).
	e := m60p1Eval(t, SideLong, 28_980, SideLong, SideLong, m60p1Levels())
	bars := inside(29_012)
	var provAt int64
	for i := range bars {
		e.Tick(bars[:i+1], BarCloseInstant(bars[i]))
		if bars[i].OpenTime == m60p1At(9, 55) {
			if bx := e.State.ISBBox15m; bx == nil || !bx.Provisional {
				t.Fatalf("09:55: want the provisional 15m box, got %+v", bx)
			}
			provAt = e.State.ISBBox15m.AtTime
		}
	}
	bx := e.State.ISBBox15m
	if bx == nil || bx.Provisional || bx.AtTime != provAt || bx.AtTime != m60p1At(9, 45) {
		t.Fatalf("10:00: want the REAL 15m box at 09:45 replacing the provisional one, got %+v", bx)
	}
	if e.State.LastISBBox15mAt != m60p1At(9, 45) {
		t.Fatalf("the real box must be recorded once (LastISBBox15mAt), got %d", e.State.LastISBBox15mAt)
	}
	if n := e.State.Counters["isb_15m_forming_built"]; n != 1 {
		t.Fatalf("the provisional box is built ONCE per forming bucket, got %d (counters %v)", n, e.State.Counters)
	}
	if e.State.Counters["isb_15m_forming_confirmed"] != 1 || e.State.Counters["isb_15m_forming_dropped"] != 0 {
		t.Fatalf("want confirmed=1 dropped=0, counters %v", e.State.Counters)
	}

	// Dropped: the last 1m closes 29,030 (open 29,010 — still inside, no
	// escape) → the 15m body ends above the mother high → no inside bar → the
	// provisional box is dropped at the close, and no real box is drawn.
	e = m60p1Eval(t, SideLong, 28_980, SideLong, SideLong, m60p1Levels())
	bars = inside(29_030)
	for i := range bars {
		e.Tick(bars[:i+1], BarCloseInstant(bars[i]))
	}
	if e.State.ISBBox15m != nil {
		t.Fatalf("a 15m that closes as no inside bar must drop the provisional box, got %+v", *e.State.ISBBox15m)
	}
	if e.State.Counters["isb_15m_forming_dropped"] != 1 {
		t.Fatalf("want isb_15m_forming_dropped counted once, counters %v", e.State.Counters)
	}
}

// TestM60Forming15mISBUnit — the detector on its own: exactly TWO closed 5m in
// the forming bucket; one is too early, three means the 15m has closed.
func TestM60Forming15mISBUnit(t *testing.T) {
	cb := func(bars []market.Kline, now int64) ([]market.Kline, []market.Kline) {
		return closedBuckets(bars, now, DefaultConfig()), closedBucketsTF(bars, 15, now)
	}
	bars := m60p1WE1(28_998)
	// 09:55 tick: two closed 5m (09:45, 09:50) in the forming 09:45 bucket.
	tape := append(append([]market.Kline{}, bars...), m60p1K(m60p1At(9, 55), 28_998, 29_000, 28_996, 28_999))
	c5, c15 := cb(tape, BarCloseInstant(tape[len(tape)-1]))
	bx, ok := forming15mISB(c5, c15)
	if !ok || !bx.Provisional || bx.Dir != SideLong || bx.High != 29_020 || bx.Low != 28_985 || bx.AtTime != m60p1At(9, 45) {
		t.Fatalf("want the provisional long box 28,985–29,020 @09:45, got %+v ok=%v", bx, ok)
	}
	// 09:50 tick: only ONE closed 5m (09:45) in the forming bucket → no read yet.
	early := bars[:len(bars)-4] // through the 09:50 bar
	c5, c15 = cb(early, BarCloseInstant(early[len(early)-1]))
	if _, ok := forming15mISB(c5, c15); ok {
		t.Fatal("one closed 5m candle is not a forming 15m read")
	}
}
