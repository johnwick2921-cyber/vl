package mentor

// W7 (release #52, course-alignment plan A9 + R7 + B3 + B28).
//
//	A9  the post-print news trade (07:35–08:30 CT on a print day) is locked to
//	    the 4h: "có news hay không có news đi nữa thì mình nhắm theo khung 4
//	    giờ" and 4h/1h opposite → "nghỉ trade khúc đó" [D4.4 p1 @13:18–13:33,
//	    @18:03–18:52]. HTFGateActive used to stop at 07:35, so the news trade
//	    ran with GateOff=true and no side lock.
//	R7  on a day with NO print the 07:20–07:35 window is ordinary trading —
//	    "đánh news nên sử dụng cho news trước … đừng sử dụng cho thường"
//	    [D4.4 p1 @14:01–14:06] — so no gate there.
//	B3  "an earlier 1h trigger is ignored" is a question of real time, not of
//	    which bucket opened first [D4.4 p1 @18:10–18:54; p2 @03:10–03:55].

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// TestHTFGateActivePrintDay pins the pure window test: the news period runs
// from the print window's start to the 08:30 CT open ONLY on a print day.
func TestHTFGateActivePrintDay(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HTFGateNewsOnly = true
	at := func(hh, mm int) int64 { return auditMs(2026, 9, 15, hh, mm, 0) }

	// A print day: 07:20 through the open, then the open window to 09:30.
	cfg.PrintWindows = []PrintWindow{printWindowCT(2026, 9, 15)}
	for _, c := range []struct {
		hh, mm int
		want   bool
	}{
		{7, 19, false}, {7, 20, true}, {7, 35, true}, {7, 36, true}, {7, 40, true},
		{8, 0, true}, {8, 29, true}, {8, 30, true}, {9, 30, true}, {9, 31, false}, {10, 0, false},
	} {
		if got := HTFGateActive(at(c.hh, c.mm), cfg); got != c.want {
			t.Fatalf("print day %02d:%02d CT: gate = %v, want %v", c.hh, c.mm, got, c.want)
		}
	}

	// No print today (R7): 07:20–08:29 is ordinary trading; only the open window gates.
	cfg.PrintWindows = nil
	for _, c := range []struct {
		hh, mm int
		want   bool
	}{
		{7, 20, false}, {7, 30, false}, {7, 35, false}, {7, 40, false}, {8, 29, false},
		{8, 30, true}, {9, 0, true}, {9, 30, true}, {9, 31, false},
	} {
		if got := HTFGateActive(at(c.hh, c.mm), cfg); got != c.want {
			t.Fatalf("no-print day %02d:%02d CT: gate = %v, want %v", c.hh, c.mm, got, c.want)
		}
	}

	// A print window of ANOTHER day never opens today's news period.
	cfg.PrintWindows = []PrintWindow{printWindowCT(2026, 9, 14)}
	if HTFGateActive(at(7, 40), cfg) {
		t.Fatal("yesterday's print window must not gate 07:40 today")
	}
}

// w7HTFBlocks runs the production Tick at 07:41 CT (09-15) with the given 4h/1h
// lines frozen as the pre-print read, and reports whether the 4h/1h gate
// (phl_htf_blocked) refused the long PHL. Price is ABOVE every line, so
// newsWaitFilter passes it: the only thing that can stop the long is the
// 4h/1h side lock. (Later filters, e.g. the pre-open day gate, are not under
// test and may still refuse a long that the HTF gate passed.)
func w7HTFBlocks(t *testing.T, printDay bool, htf HTF) bool {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PHLTargetShyPts = 6
	cfg.EMALocationTFMinutes = 0
	cfg.Exec2mAfter30m = false
	cfg.HTFGateNewsOnly = true
	if printDay {
		cfg.PrintWindows = []PrintWindow{printWindowCT(2026, 9, 15)}
	}
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = []Level{
		{Key: "L", Kind: KindKeyLevel, Price: 29386},
		{Key: "old-low:29335", Kind: KindOldExtreme, Role: OldLow, Price: 29335},
		{Key: "old-high:29431.75", Kind: KindOldExtreme, Role: OldHigh, Price: 29431.75},
	}
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 29300}
	e.State.HTF = htf
	b := func(i int, o, h, l, c float64) market.Kline {
		ot := auditMs(2026, 9, 15, 7, 35, 0) + int64(i)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		b(0, 29400, 29410, 29335, 29405),
		b(1, 29420, 29430, 29415, 29425),
		b(2, 29425, 29431.75, 29420, 29428),
		b(3, 29410, 29415, 29400, 29408),
		b(4, 29400, 29405, 29395, 29398),
		b(5, 29395, 29397, 29386, 29392),
	}
	now := bars[5].CloseTime + 1 // 07:41 CT, a print day's post-print news trade
	e.State.ORB = ORB{Day: dayStartCT(now), High: 29300, Low: 29200, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	e.Tick(bars, now)
	return e.State.Refusals["phl_htf_blocked"] > 0
}

// TestPostPrintNewsTradeFollowsThe4h pins A9/R7 through the production Tick.
// MUTANT: restore the old HTFGateActive (07:20–07:35 and 08:30–09:30 only) →
// the print-day 4h-SELL long and the print-day conflict long pass the HTF gate → RED.
func TestPostPrintNewsTradeFollowsThe4h(t *testing.T) {
	sell4h := TriggerLine{Dir: SideShort, Price: 29300, MovedAt: 1_000}
	buy4h := TriggerLine{Dir: SideLong, Price: 29300, MovedAt: 1_000}
	sell1h := TriggerLine{Dir: SideShort, Price: 29200, MovedAt: 2_000} // later than the 4h: NOT silent

	// 4h SELL (frozen pre-print read), 1h silent: a long against the 4h is refused.
	if !w7HTFBlocks(t, true, HTF{FourH: sell4h}) {
		t.Fatal("A9: 07:41 print day, 4h SELL: the long PHL above the line must be refused by the HTF gate — follow the 4h [D4.4 p1 @18:03–18:52]")
	}
	// 4h BUY + 1h SELL (opposite, 1h not silent): case 3, sit out.
	if !w7HTFBlocks(t, true, HTF{FourH: buy4h, OneH: sell1h}) {
		t.Fatal("A9: 07:41 print day, 4h buy / 1h sell: must sit out [D4.4 p1 @13:18–13:33]")
	}
	// 4h BUY, 1h silent (never drew): case 2, follow the 4h — the long EMITS.
	if w7HTFBlocks(t, true, HTF{FourH: buy4h}) {
		t.Fatal("A9: 07:41 print day, 4h buy, 1h silent: case 2 follows the 4h — the HTF gate must pass the long")
	}
	// R7: the SAME 4h-SELL / conflict tapes at 07:41 on a day with NO print are
	// ordinary trading — the news-only gate does not apply there.
	if w7HTFBlocks(t, false, HTF{FourH: sell4h}) {
		t.Fatal("R7: 07:41 on a non-print day the 4h/1h gate must not apply — the HTF gate must pass the long")
	}
	if w7HTFBlocks(t, false, HTF{FourH: buy4h, OneH: sell1h}) {
		t.Fatal("R7: 07:41 on a non-print day a 4h/1h conflict must not sit the trade out")
	}
}

// b3Tape is the B3 fixture: the 4h bucket opens 01:00 CT (09-15); the prior 4h
// candle spans [90,110], the prior 1h candle (00:00) spans [95,105].
//
//	01:05  low 94  -> breaks the 1h LOW only            -> 1h SELL fires 01:05
//	01:10  high 111 -> breaks the 4h HIGH               -> 4h BUY fires 01:10
//	       (the same bar breaks BOTH 1h extremes and closes inside the prior
//	       1h range -> the 1h does not move: "a close INSIDE the prior range is
//	       beyond neither extreme")
//
// So the 1h triggered EARLIER than the 4h in real time, inside the same 4h
// bucket and the same 1h bucket (both open 01:00).
func b3Tape() []market.Kline {
	mk := func(hh, mm int, day int, o, h, l, c float64) market.Kline {
		ot := auditMs(2026, 9, day, hh, mm, 0)
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	return []market.Kline{
		mk(21, 30, 14, 100, 110, 90, 100), // the prior 4h candle (21:00 bucket): [90,110]
		mk(0, 30, 15, 100, 105, 95, 100),  // the prior 1h candle (00:00 bucket): [95,105]
		mk(1, 0, 15, 100, 101, 99, 100),
		mk(1, 5, 15, 100, 101, 94, 95),    // 1h LOW break (SELL) at 01:05
		mk(1, 10, 15, 95, 111, 94.5, 100), // 4h HIGH break (BUY) at 01:10; 1h: both broken, close inside
	}
}

// TestEarlier1hTriggerIgnoredByBreakInstant pins B3 through HTFAdvanceAt (the
// call Tick makes, one 1m bar at a time) AND the seed replay (TriggerReplay1m):
// both stamp the break MINUTE, so the 1h SELL (01:05) is OLDER than the 4h BUY
// (01:10) -> the 1h is silent -> case 2 follows the 4h. With the legacy bucket
// stamps both read 01:00 (equal, "not silent") and HTFVerdict sat the trade out
// as case 3 [D4.4 p1 @18:10–18:54; p2 @03:10–03:55: "nó trigger 1 giờ từ trước
// rồi thì đương nhiên là mình sẽ coi 4 giờ thôi"].
// MUTANT: stamp MovedAt = cur.OpenTime (bucket open) in applyBreak -> RED.
func TestEarlier1hTriggerIgnoredByBreakInstant(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	bars := b3Tape()

	var live HTF // the live path: one HTFAdvanceAt per closed 1m bar
	for i := range bars {
		w := bars[:i+1]
		live = HTFAdvanceAt(live, barsTF(w, 240), barsTF(w, 60), cfg, w[len(w)-1].OpenTime)
	}
	seed := HTF{ // the restart path: TriggerReplay1m over the same tape
		FourH: TriggerReplay1m(TriggerLine{}, bars, 240, cfg),
		OneH:  TriggerReplay1m(TriggerLine{}, bars, 60, cfg),
	}
	for name, h := range map[string]HTF{"live": live, "seed": seed} {
		if h.FourH.Dir != SideLong || h.FourH.Price != 110 {
			t.Fatalf("%s: fixture 4h must be BUY @110, got %s@%.2f", name, h.FourH.Dir, h.FourH.Price)
		}
		if h.OneH.Dir != SideShort || h.OneH.Price != 95 {
			t.Fatalf("%s: fixture 1h must be SELL @95, got %s@%.2f", name, h.OneH.Dir, h.OneH.Price)
		}
		if want := auditMs(2026, 9, 15, 1, 10, 0); h.FourH.MovedAt != want {
			t.Fatalf("%s: 4h MovedAt = %d, want the break minute 01:10 (%d)", name, h.FourH.MovedAt, want)
		}
		if want := auditMs(2026, 9, 15, 1, 5, 0); h.OneH.MovedAt != want {
			t.Fatalf("%s: 1h MovedAt = %d, want the break minute 01:05 (%d)", name, h.OneH.MovedAt, want)
		}
		if !oneHSilent(h) {
			t.Fatalf("%s: the 1h fired before the 4h -> silent", name)
		}
		if ok, side, why := HTFVerdict(h); !ok || side != SideLong {
			t.Fatalf("%s: case 2 must follow the 4h BUY, got ok=%v side=%q (%s)", name, ok, side, why)
		}
		if HTFConflict(h) {
			t.Fatalf("%s: an earlier 1h trigger is no conflict", name)
		}
	}
	if live.FourH != seed.FourH || live.OneH != seed.OneH {
		t.Fatalf("restart parity: live %+v != seed %+v", live, seed)
	}
}

// TestEarlier1hTriggerTickStampsBreakMinute is the Tick-level companion of the
// B3 pin above: the production Tick must hand HTFAdvanceAt the ticked 1m bar's
// open as the break minute, so the boot-line "since" of the 4h/1h lines is the
// minute each really fired (01:05 for the 1h, 01:10 for the 4h).
// MUTANT: eval.go Tick passes breakAt=0 -> the lines stamp the bucket open
// (01:00) and this test is RED.
func TestEarlier1hTriggerTickStampsBreakMinute(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	bars := b3Tape()
	for i := range bars {
		e.Tick(bars[:i+1], bars[i].CloseTime+1)
	}
	h := e.State.HTF
	if h.FourH.Dir != SideLong || h.OneH.Dir != SideShort {
		t.Fatalf("fixture: want 4h BUY / 1h SELL, got %s / %s", h.FourH.Dir, h.OneH.Dir)
	}
	if want := auditMs(2026, 9, 15, 1, 10, 0); h.FourH.MovedAt != want {
		t.Fatalf("Tick: 4h MovedAt = %d, want the break minute 01:10 (%d)", h.FourH.MovedAt, want)
	}
	if want := auditMs(2026, 9, 15, 1, 5, 0); h.OneH.MovedAt != want {
		t.Fatalf("Tick: 1h MovedAt = %d, want the break minute 01:05 (%d)", h.OneH.MovedAt, want)
	}
}
