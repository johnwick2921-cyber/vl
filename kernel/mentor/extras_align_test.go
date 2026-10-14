package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// ── EXTRAS-ALIGN (2026-10-14) — the bot against the course + the 18 extras ─
//
// Every pin drives the PRODUCTION call site: Evaluator.Tick (which calls
// ORBAdvance and, at its end, orbGateFilter on the evaluator's own ORB state)
// and TrendlinesBuild (the exported face of the trendlinesBuild Tick calls).
//
// Named mutants (each anchored to ONE site, run RED then restored):
//   E1 close-only escape — orb.go orbBodyEscape
//      `case c.Open > orb.High && c.Close > orb.High:` → `case c.Close > orb.High:`
//      → TestORBEscapeNeedsTheBodyOutsideAtTick RED.
//   E2 all-day side lock — orb.go ORBAdvance
//      `if side := orbBodyEscape(orb, cur); side != "" {` →
//      `if side := orbBodyEscape(orb, cur); side != "" && orb.Escaped == "" {`
//      → TestORBSideFollowsTheLatestEscapeAtTick RED.
//   E5 close-only break — trendline.go brokenBy5m `return b.High < p` →
//      `return b.Close < p` → TestTrendlineCloseThroughWithTheWickAboveIsNotABreak RED.
//   E5b open-time line — trendline.go brokenBy5m
//      `p := t.priceAt(b.CloseTime)` → `p := t.priceAt(b.OpenTime)`
//      → TestTrendlineBreakReadsTheLineAtTheCloseTime RED.

// orbDayBars: a flat pre-open, then the 08:30/08:31 ORB 99–101.
func orbDayBars(day int64) []market.Kline {
	at := func(h, m int) int64 { return day + int64(h*60+m)*60_000 }
	bars := flatDay(day, 6*60+30, 8*60+30)
	return append(bars,
		clockBar(at(8, 30), 100, 101, 99, 100),
		clockBar(at(8, 31), 100, 100.5, 99.5, 100),
	)
}

// tickAll drives Tick bar by bar at each bar's close instant (production
// clock, trader mentorEvalOnce).
func tickAll(e *Evaluator, bars []market.Kline, from int) {
	for i := from; i <= len(bars); i++ {
		e.Tick(bars[:i], BarCloseInstant(bars[i-1]))
	}
}

// TestORBEscapeNeedsTheBodyOutsideAtTick — item 1. "Break là sao? … cái THÂN
// NẾN nằm bên ngoài mà cái râu nến nằm bên trong cái box ok… Cái đó là AT
// LEAST" [D4.1 p1 @20:36–21:03]; X5 asks for more ("COMPLETELY nằm ở ngoài"
// [X5 @07:04]). The 08:32 candle OPENS inside the ORB (100.5) and closes one
// tick above the high (101.25): its body straddles the edge — NOT an escape,
// and the gate still refuses a long. The 08:33 candle's body is wholly above
// (open 101.5, close 102) → the escape, longs only. Pre-fix the close alone
// escaped at 08:32 (RED).
func TestORBEscapeNeedsTheBodyOutsideAtTick(t *testing.T) {
	day := ctMs(t, 0, 0, 0)
	at := func(h, m int) int64 { return day + int64(h*60+m)*60_000 }
	bars := append(orbDayBars(day), clockBar(at(8, 32), 100.5, 101.5, 100.25, 101.25))
	e := New(clockCfg())
	tickAll(e, bars, 2)
	if !e.State.ORB.Drawn || e.State.ORB.High != 101 || e.State.ORB.Low != 99 {
		t.Fatalf("ORB = %+v, want drawn 99–101", e.State.ORB)
	}
	if e.State.ORB.Escaped != "" {
		t.Fatalf("a 1m candle that OPENS inside the ORB and closes one tick out escaped %q — the course minimum is the BODY outside [D4.1 p1 @20:36]", e.State.ORB.Escaped)
	}
	long := []Intent{{Action: PlaceStopEntry, Side: SideLong, Price: 102, Reason: "PHL"}}
	if kept, refused := orbGateFilter(long, e.State.ORB, BarCloseInstant(bars[len(bars)-1]), e.Cfg); len(kept) != 0 || len(refused) != 1 || refused[0] != "orb_not_escaped" {
		t.Fatalf("the gate must still refuse (orb_not_escaped): kept %+v refused %v", kept, refused)
	}
	bars = append(bars, clockBar(at(8, 33), 101.5, 102.25, 101.25, 102))
	e.TakeNotes()
	e.Tick(bars, BarCloseInstant(bars[len(bars)-1]))
	if e.State.ORB.Escaped != SideLong {
		t.Fatalf("the 08:33 body wholly above 101 must escape long, got %q", e.State.ORB.Escaped)
	}
	if notes := strings.Join(e.TakeNotes(), "\n"); !strings.Contains(notes, "ORB escape long") {
		t.Fatalf("the escape must be noted (logged) with its candle; notes:\n%s", notes)
	}
	if kept, _ := orbGateFilter(long, e.State.ORB, BarCloseInstant(bars[len(bars)-1]), e.Cfg); len(kept) != 1 {
		t.Fatal("after the body escape a long above the ORB passes the gate")
	}
}

// TestORBSideFollowsTheLatestEscapeAtTick — item 2. The side is the side of
// the LATEST escape: "Nó thoát ra khỏi đầu nào, nó sẽ đi theo đầu đó" [X5
// @02:57–03:02]; "Nếu mà không có nằm completely ở ngoài cái box này thì mình
// đâu có kiếm lệnh short nữa. Mình vẫn sẽ kiếm lệnh long… CHO TỚI KHI NÀO NÓ
// CRACK" [X7 @07:42–07:51]. Escape up at 08:32; a candle that opens ABOVE the
// high and closes back INSIDE switches nothing (no crack); at 10:10 a 1m body
// wholly BELOW the low → shorts only. Pre-fix the 08:32 side was latched until
// 15:00 and the short stayed refused (orb_wrong_side) all day (RED).
func TestORBSideFollowsTheLatestEscapeAtTick(t *testing.T) {
	day := ctMs(t, 0, 0, 0)
	at := func(h, m int) int64 { return day + int64(h*60+m)*60_000 }
	bars := append(orbDayBars(day), clockBar(at(8, 32), 101.5, 102.5, 101.25, 102))
	e := New(clockCfg())
	tickAll(e, bars, 2)
	if e.State.ORB.Escaped != SideLong {
		t.Fatalf("08:32 body above → long, got %q", e.State.ORB.Escaped)
	}
	// a failed push: opens above, closes back inside — no crack, side unchanged.
	bars = append(bars, clockBar(at(9, 40), 101.5, 101.75, 99.75, 100))
	e.Tick(bars, BarCloseInstant(bars[len(bars)-1]))
	if e.State.ORB.Escaped != SideLong {
		t.Fatalf("a candle closing back INSIDE is not an escape the other way, got %q", e.State.ORB.Escaped)
	}
	e.TakeNotes()
	// the crack: a 1m body wholly below the low 99.
	bars = append(bars, clockBar(at(10, 10), 98.75, 99.25, 97.5, 98))
	now := BarCloseInstant(bars[len(bars)-1])
	e.Tick(bars, now)
	if e.State.ORB.Escaped != SideShort {
		t.Fatalf("a 1m body wholly below the ORB low must switch the side to short (X7 'cho tới khi nào nó crack'), got %q — the first side is NOT held all day", e.State.ORB.Escaped)
	}
	if notes := strings.Join(e.TakeNotes(), "\n"); !strings.Contains(notes, "ORB side SWITCHED long → short") {
		t.Fatalf("the side switch must be noted (logged) with its candle; notes:\n%s", notes)
	}
	ints := []Intent{
		{Action: PlaceStopEntry, Side: SideShort, Price: 97.25, Reason: "PLH"},
		{Action: PlaceStopEntry, Side: SideLong, Price: 102, Reason: "PHL"},
	}
	kept, refused := orbGateFilter(ints, e.State.ORB, now, e.Cfg)
	if len(kept) != 1 || kept[0].Side != SideShort {
		t.Fatalf("after the switch only the short passes; kept %+v", kept)
	}
	if len(refused) != 1 || refused[0] != "orb_wrong_side" {
		t.Fatalf("the long is still REFUSED and named (orb_wrong_side) — refusals stay counted; got %v", refused)
	}
}

// TestTrendlineCloseThroughWithTheWickAboveIsNotABreak — item 5 at the call
// site. "đợi khung 5 phút đóng… ĐÓNG DƯỚI NGUYÊN CÂY luôn" [X9 @06:25–06:45];
// the course (D4.1) gives no trendline-break rule — it keeps the line all
// session [D4.1 p2 @01:17] — so X9's whole candle decides. The 09:10 bucket
// closes at 98.1, below the line, with its high (102) far above it: the line
// stays VALID. Pre-fix the close alone killed it (RED).
func TestTrendlineCloseThroughWithTheWickAboveIsNotABreak(t *testing.T) {
	bars, mk := trendlineReturnTape()
	bars = append(bars, mk(14, 101.2, 101.3, 98.0, 98.1))
	now := time.UnixMilli(bars[14].OpenTime + 60_000).In(ctime())
	tls := TrendlinesBuild(bars, now)
	if len(tls) != 1 || tls[0].Dead {
		t.Fatalf("lines = %+v — a 5m close through with the candle's HIGH above the support line is NOT a break (X9: nguyên cây)", tls)
	}
}

// TestTrendlineBreakReadsTheLineAtTheCloseTime — item 5, the line value. The
// 09:10 bucket (bars 10–14) sits wholly under the rising support line AT ITS
// CLOSE (line ≈99.6, bucket high 99.4) but closes at 99.0, above the line's
// value at the bucket OPEN (98.6). Read at the close it is a whole-candle
// break → Dead. Pre-fix the line was read at the open, five minutes of slope
// early, and the candle never counted (RED).
func TestTrendlineBreakReadsTheLineAtTheCloseTime(t *testing.T) {
	bars, mk := trendlineBars()
	bars = append(bars,
		mk(10, 99.3, 99.4, 98.9, 99.0),
		mk(11, 99.0, 99.3, 98.8, 98.9),
		mk(12, 98.9, 99.2, 98.7, 98.8),
		mk(13, 98.8, 99.1, 98.6, 98.7),
		mk(14, 98.7, 99.0, 98.5, 99.0), // bucket 10–14: high 99.4, close 99.0
	)
	now := time.UnixMilli(bars[14].OpenTime + 60_000).In(ctime())
	tls := TrendlinesBuild(bars, now)
	if len(tls) != 1 || !tls[0].Dead {
		t.Fatalf("lines = %+v — a 5m candle wholly below the line at its CLOSE time must break it", tls)
	}
}
