package mentor

import (
	"math"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// oct round 2, D5.3-R40/R43/R44 + reread OctD5.3-RR2-1 — the reverse ISB at
// EMA 9 reads the TREND from the lines, not from the 5m trigger.
//
// OctD5.3-R43 [Oct D5.3 @ 20:00]: an uptrend is price above all four lines (fish
// red/white, EMA 34, EMA 9). OctD5.3-R40 [@ 17:33] / R44 [@ 20:44]: at the EMA 9
// the market offers two orders ("inside bar for the long, right here, or a
// potential lower high for the short") and "lúc nào cũng chọn cái hướng theo
// trend". Worked example 13, Tue 27 Oct 2026 10:10-10:12 CT (circle ~29,716-
// 29,725): price is above the EMA 34 (~29,688) and sits on the EMA 9 (~29,722);
// the 1m ISBs point SHORT; the mentor takes the LONG. The bot's 5m trigger flips
// SELL at 10:10 (the forming 10:10 bucket's low 29,710.50 < the 10:05 bucket's
// 29,717.25), so the bot's "trend" was SHORT, the short ISBs pointed WITH it and
// no reverse long was ever emitted. The fish is not built [FINAL report]: the
// two lines the bot has are the EMA 34 and the EMA 9.
//
// Knob isb_reverse_trend: "ema" (October, default) | "trigger" (today).

func r2RevTrendRun(t *testing.T, mode string, hm string) (*Evaluator, []Intent, []market.Kline) {
	t.Helper()
	return r2RevTrendRunCfg(t, hm, func(c *Config) {
		if mode != "" {
			c.ISBReverseTrend = mode
		}
	})
}

func r2RevTrendRunCfg(t *testing.T, hm string, mod func(*Config), post ...func(*Evaluator)) (*Evaluator, []Intent, []market.Kline) {
	t.Helper()
	all := m413Load(t, "testdata/r2_mnq_1m_20261026_1700_to_20261027_1020ct.csv")
	idx := -1
	for i, b := range all {
		if time.UnixMilli(b.OpenTime).In(ctime()).Format("15:04") == hm && time.UnixMilli(b.OpenTime).In(ctime()).Day() == 27 {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("bar %s not in fixture", hm)
	}
	bars := all[:idx+1]
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.HTFGateNewsOnly = false
	cfg.PHLRoomMultiple = 1 // the 27 Oct tape's rolling PHL long arms at the 1R room (knob phl_room_multiple=1)
	cfg.ISBReverseEMA9Enabled = true
	cfg.LocTriggerFilter = false
	cfg.SmallRangeGuard = false
	if mod != nil {
		mod(&cfg)
	}
	now := BarCloseInstant(bars[len(bars)-1])
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = []Level{
		{Key: "K-below", Kind: KindKeyLevel, Price: 29400},
		{Key: "K-above", Kind: KindKeyLevel, Price: 30100},
	}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 29500, Low: 29450, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 29400}}
	// the live 5m trigger at 10:10: SELL on the 10:05 bucket's low 29,717.25.
	e.State.Trigger = TriggerLine{Dir: SideShort, Price: 29717.25, LastBucket: math.MaxInt64}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	for _, f := range post {
		f(e)
	}
	return e, e.Tick(bars, now), bars
}

func r2RevLong(ins []Intent) *Intent {
	for i := range ins {
		in := &ins[i]
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Side == SideLong && (in.Setup == "PHL" || in.Setup == "ISB") && strings.Contains(in.Reason, "reverse ISB at EMA 9") {
			return in
		}
	}
	return nil
}

// The course's own example through the production Tick: 10:10, 10:11 and 10:12
// each carry a short ISB at the EMA 9 with price above the EMA 34 — the reverse
// LONG buy stop sits over the ISB candle's high.
func TestRound2D53ReverseISBTrendFromEMAAlignment(t *testing.T) {
	// 10:11's candle is 19.5 pts (isbrev_stop_over_max, the ISB stop ceiling) and
	// 10:12 meets the room rule against the 10:0x highs — unrelated, correct
	// refusals — so the pin reads the first of the course's three ISBs, 10:10.
	for _, hm := range []string{"10:10"} {
		e, ins, bars := r2RevTrendRun(t, "", hm)
		cur := bars[len(bars)-1]
		r := r2RevLong(ins)
		if r == nil {
			t.Fatalf("%s: EMA 9 > EMA 34, price above both (Oct D5.3 @20:00): the short ISB at the EMA 9 must give the LONG; refusals %v counters %v", hm, e.State.Refusals, e.State.Counters)
		}
		if want := cur.High + e.Cfg.ISBBufferPts; math.Abs(r.Price-want) > 1e-6 {
			t.Fatalf("%s: buy stop %.2f, want ISB high %.2f + buffer = %.2f", hm, r.Price, cur.High, want)
		}
	}
}

// knob = "trigger": today's read — the SELL trigger is the trend, the short
// ISBs point with it, no reverse long.
func TestRound2D53ReverseISBTrendKnobTriggerIsToday(t *testing.T) {
	for _, hm := range []string{"10:10"} {
		_, ins, _ := r2RevTrendRun(t, ISBReverseTrendTrigger, hm)
		if r := r2RevLong(ins); r != nil {
			t.Fatalf("%s: isb_reverse_trend=trigger must reproduce today (no reverse long under a SELL trigger); got %+v", hm, r)
		}
	}
}

// the lines disagree (EMA 9 below the EMA 34 while price is above it, or price
// between them): no trend the course names -> fail closed, no reverse order.
func TestRound2D53ReverseISBTrendMixedLinesFailClosed(t *testing.T) {
	if got := emaAlignedTrend(100, 101, 102); got != "" {
		t.Fatalf("EMA 9 below the 34 with price above the 34 must read no trend, got %q", got)
	}
	if got := emaAlignedTrend(105, 100, 104); got != SideLong {
		t.Fatalf("ema9>ema34, close>ema34 = long, got %q", got)
	}
	if got := emaAlignedTrend(95, 100, 96); got != SideShort {
		t.Fatalf("ema9<ema34, close<ema34 = short, got %q", got)
	}
	if got := emaAlignedTrend(105, 100, 99); got != "" {
		t.Fatalf("ema9>ema34 but close below the 34 = no trend, got %q", got)
	}
	if got := emaAlignedTrend(0, 100, 105); got != "" {
		t.Fatalf("an unwarmed line = no trend, got %q", got)
	}
}

// D5.4-SCALP (minimal) — OctD5.4-R11 [@04:47] "giảm size lại", R13 [@05:00] "ăn ít
// lại": an entry against the 5m trigger is a scalp and sizes down. Production
// Tick over the same 27 Oct 10:10 tape: the 5m trigger is SELL, so the reverse
// LONG (and the rolling PHL long the same bar arms) are counter-5m and carry
// FlagCounter5m; knob counter_5m_reduce_size=off stamps nothing (today).
func TestRound2D54ScalpCounter5mEntriesCarryTheReduceFlag(t *testing.T) {
	_, ins, _ := r2RevTrendRun(t, "", "10:10")
	r := r2RevLong(ins)
	if r == nil {
		t.Fatal("fixture: the reverse long must exist at 10:10")
	}
	if !HasFlag(r.Flag, FlagCounter5m) {
		t.Fatalf("a long under a SELL 5m trigger must carry %q, flag=%q", FlagCounter5m, r.Flag)
	}
	n := 0
	for _, in := range ins {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Side == SideLong {
			n++
			if !HasFlag(in.Flag, FlagCounter5m) {
				t.Fatalf("every long entry under a SELL trigger is counter-5m; got %+v", in)
			}
		}
	}
	if n < 2 {
		t.Fatalf("expected the reverse long AND the rolling PHL long on this bar, got %d long entries", n)
	}
}

func TestRound2D54ScalpKnobOffStampsNothing(t *testing.T) {
	_, ins, _ := r2RevTrendRunCfg(t, "10:10", func(c *Config) { c.Counter5mReduceSize = Counter5mReduceOff })
	for _, in := range ins {
		if HasFlag(in.Flag, FlagCounter5m) {
			t.Fatalf("counter_5m_reduce_size=off must reproduce today (no flag); got %+v", in)
		}
	}
}

// a with-trigger entry is never flagged: flip the pinned trigger to BUY and the
// same long is a with-trend entry.
func TestRound2D54ScalpWithTriggerEntryIsNotFlagged(t *testing.T) {
	e0, ins, _ := r2RevTrendRunCfg(t, "10:10", nil, func(e *Evaluator) {
		e.State.Trigger = TriggerLine{Dir: SideLong, Price: 29600, LastBucket: math.MaxInt64}
	})
	seen := false
	for _, in := range ins {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Side == SideLong {
			seen = true
			if HasFlag(in.Flag, FlagCounter5m) {
				t.Fatalf("a long under a BUY trigger is with-trigger, must not be flagged: %+v", in)
			}
		}
	}
	if !seen {
		t.Fatalf("fixture: expected a long entry at 10:10 under a BUY trigger; refusals %v", e0.State.Refusals)
	}
}
