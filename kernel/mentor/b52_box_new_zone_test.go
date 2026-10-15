package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// B52 (release #29) — box new zone after escape. The standing FTGH/FTGL latch
// is unchanged (D4.1-22); the new rule latches ONE new same-role box, built by
// the SAME construction rule, from confirmed swings that form AFTER the escape
// and lie ENTIRELY OUTSIDE the old range [D3.4 p3 recovered2 @00:00–00:21;
// "Đừng vẽ vùng thứ 3… Rối lắm" D3.2 p2 @09:47–09:54]. The old box keeps its
// flip; the new box is the role's active box, and each further escape repeats.
//
// All pins run the PRODUCTION call site — Evaluator.Tick over the live window
// (canon 53) — never a re-built evaluator input.

// b52Cfg is the production evaluator config with the non-box machinery turned
// down so the synthetic tape's boxes are the only thing alive.
func b52Cfg() Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	return cfg
}

// b52Tape builds the 10-14 SHAPE synthetically, 1m bars on one trading day:
//
//	first FTGL [96, 96.8]  (extreme low 96 @2, partner low 96.5 @6, lower body 96.8)
//	escape @8              (body 95.5→95, wholly below 96)
//	new double bottom      (extreme low 92 @11, partner low 92.8 @15, lower body 92.9)
//	→ new FTGL [92, 92.9], entirely below the old floor 96.
func b52Tape() ([]market.Kline, Config) {
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 100.5, 98, 100),
		mk(2, 99, 99.5, 96, 97), // extreme low 96
		mk(3, 97, 98, 97, 97.5), // confirms @2 (low 97 > 96)
		mk(4, 98, 98.5, 98, 98),
		mk(5, 98, 98.5, 97.5, 98),
		mk(6, 97, 97.5, 96.5, 96.8), // partner low 96.5 (>96, fails to break)
		mk(7, 96.8, 97.5, 96.6, 97), // confirms @6 → box [96, 96.8]
		mk(8, 95.5, 96, 95, 95),     // ESCAPE: body wholly below 96
		mk(9, 95, 95.5, 94.5, 95),
		mk(10, 94.8, 95, 94, 94.6),
		mk(11, 94, 94.5, 92, 93),   // extreme low 92 (below the old floor)
		mk(12, 93, 93.5, 92.5, 93), // confirms @11 (low 92.5 > 92)
		mk(13, 93, 93.5, 93, 93.2),
		mk(14, 93.2, 94, 93.2, 93.8),
		mk(15, 93.5, 94, 92.8, 92.9), // partner low 92.8 (>92)
		mk(16, 93, 94, 93, 93.5),     // confirms @15 → new box [92, 92.9]
	}
	return bars, b52Cfg()
}

// b52Run ticks the production call site one bar at a time so the LATCH pins
// the first box per role at its actual formation bar (never a later re-scan).
func b52Run(bars []market.Kline, cfg Config) *Evaluator {
	e := New(cfg)
	for i := 2; i <= len(bars); i++ {
		e.Tick(bars[:i], bars[i-1].OpenTime+59_999)
	}
	return e
}

// b52LastBox returns the newest post-escape box of the given role, ok=false
// when none.
func b52LastBox(post []Box, kind BoxKind) (Box, bool) {
	for i := len(post) - 1; i >= 0; i-- {
		if post[i].Kind == kind {
			return post[i], true
		}
	}
	return Box{}, false
}

// TestB52NewZoneAfterEscape — rule 2: after the standing FTGL escapes, ONE new
// FTGL is latched below the old floor, by the same construction rule, and the
// old box is NOT redrawn (D4.1-22 stands). Mutant: latch the new box with
// Top > old.Bottom (drop the outside check) → the zone would sit inside the
// standing range and this pin goes RED.
func TestB52NewZoneAfterEscape(t *testing.T) {
	bars, cfg := b52Tape()
	e := b52Run(bars, cfg)
	if got := e.State.LatchedBoxes["ftgl"]; got.Key != "ftgl:96.80:96.00" || got.Bottom != 96 || got.Top != 96.8 {
		t.Fatalf("first FTGL = %+v, want key ftgl:96.80:96.00 bottom 96 top 96.8", got)
	}
	nb, ok := b52LastBox(e.State.PostEscapeBoxes, FTGL)
	if !ok {
		t.Fatalf("no post-escape FTGL latched — post = %+v", e.State.PostEscapeBoxes)
	}
	if nb.Bottom != 92 || nb.Top != 92.9 {
		t.Fatalf("new FTGL = [%.2f, %.2f], want [92, 92.9] (entirely below the old floor 96)", nb.Bottom, nb.Top)
	}
	if nb.Top > e.State.LatchedBoxes["ftgl"].Bottom {
		t.Fatalf("new zone [%.2f, %.2f] is not outside the old floor 96", nb.Bottom, nb.Top)
	}
}

// TestB52KnobOffNoNewZone — the knob OFF reproduces today's behaviour exactly:
// the same escape + lower double bottom latches NO new box (release #29 is the
// only change). Mutant: drop the knob guard in latchBoxes → this pin goes RED.
func TestB52KnobOffNoNewZone(t *testing.T) {
	bars, cfg := b52Tape()
	cfg.BoxNewZoneAfterEscape = false
	e := b52Run(bars, cfg)
	if len(e.State.PostEscapeBoxes) != 0 {
		t.Fatalf("knob OFF still latched %d post-escape boxes: %+v", len(e.State.PostEscapeBoxes), e.State.PostEscapeBoxes)
	}
	if got := e.State.LatchedBoxes["ftgl"]; got.Key != "ftgl:96.80:96.00" || got.Bottom != 96 || got.Top != 96.8 {
		t.Fatalf("first FTGL changed under knob OFF: %+v, want [96, 96.8]", got)
	}
}

// TestB52InsideRangeNoBox — rule 3: a failure that forms INSIDE the standing
// range (its top above the old floor) is never latched — "no third zone in the
// middle". Same first box + escape, but the next double bottom sits ABOVE the
// old floor. Mutant: change the outside check from `b.Top > old.Bottom` to
// `b.Bottom >= old.Bottom` (a zone straddling the floor) → RED.
func TestB52InsideRangeNoBox(t *testing.T) {
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	// First FTGL [96, 96.8] (as b52Tape), escape @8, then price rallies back
	// ABOVE the old floor and makes a double bottom at 96.5/97.2 — its zone
	// [96.5, 97.5] would sit INSIDE (above) the old floor 96.
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 100.5, 98, 100),
		mk(2, 99, 99.5, 96, 97),
		mk(3, 97, 98, 97, 97.5),
		mk(4, 98, 98.5, 98, 98),
		mk(5, 98, 98.5, 97.5, 98),
		mk(6, 97, 97.5, 96.5, 96.8),
		mk(7, 96.8, 97.5, 96.6, 97),
		mk(8, 95.5, 96, 95, 95), // escape below 96
		mk(9, 96, 96.5, 95.5, 96),
		mk(10, 97, 97.5, 96.5, 97), // back above the floor
		mk(11, 97.5, 98, 96.5, 97), // swing low 96.5 (inside)
		mk(12, 97.5, 98, 97, 97.5), // confirms @11
		mk(13, 98, 98.5, 97.5, 98),
		mk(14, 98, 98.5, 97.8, 98),
		mk(15, 98, 98.5, 97.2, 97.5), // partner low 97.2 (>96.5, fails to break)
		mk(16, 97.8, 98.5, 97.6, 98), // confirms @15 → zone [96.5, 97.5]
	}
	e := b52Run(bars, b52Cfg())
	if len(e.State.PostEscapeBoxes) != 0 {
		t.Fatalf("latched an inside-range zone: %+v — no third zone in the middle [D3.2 p2 @09:47–09:54]", e.State.PostEscapeBoxes)
	}
}

// TestB52HigherHighNoRedrawNoNewBox — D4.1-22 with B52 ON: a later higher high
// that does NOT escape the ceiling neither redraws the box nor latches a new
// one (no escape ⇒ no new zone). Mutant: treat a higher high as an escape → RED.
func TestB52HigherHighNoRedrawNoNewBox(t *testing.T) {
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	// FTGH [100, 102] (extreme high 102 @2, partner high 101.5 @5); a later
	// higher high 101.8 @9 stays BELOW the ceiling 102 (no body escape).
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 100.5, 99, 100),
		mk(2, 100, 102, 99, 101),
		mk(3, 101, 101, 100, 100.5),
		mk(4, 100, 100, 99, 99.5),
		mk(5, 99.5, 101.5, 99, 100),
		mk(6, 100, 101, 99, 100),
		mk(7, 100, 101, 99, 100),
		mk(8, 100, 100.5, 99, 100),
		mk(9, 100, 101.8, 99, 101),
		mk(10, 101, 101.5, 100, 100.5),
		mk(11, 100, 101, 99, 100),
		mk(12, 100, 101.2, 99, 101),
		mk(13, 101, 101.5, 100, 100.5),
	}
	e := b52Run(bars, b52Cfg())
	if got := e.State.LatchedBoxes["ftgh"]; got.Key != "ftgh:102.00:100.00" || got.Top != 102 || got.Bottom != 100 {
		t.Fatalf("FTGH = %+v, want [100, 102]", got)
	}
	if len(e.State.PostEscapeBoxes) != 0 {
		t.Fatalf("a higher high without an escape latched a new zone: %+v", e.State.PostEscapeBoxes)
	}
}

// TestB52RestartSameBoxes — a restart after a post-escape latch replays the
// SAME boxes (the #527 latch replay extended for the new zone). The continuous
// run latches the new FTGL; a fresh evaluator seeded on the same history must
// reproduce it. Mutant: skip the post-escape leg in seedBoxLatch → RED.
func TestB52RestartSameBoxes(t *testing.T) {
	bars, cfg := b52Tape()
	live := b52Run(bars, cfg)
	liveLast, ok := b52LastBox(live.State.PostEscapeBoxes, FTGL)
	if !ok {
		t.Fatalf("continuous run never latched a post-escape FTGL: %+v", live.State.PostEscapeBoxes)
	}

	restart := New(cfg)
	SeedFull(restart, bars, keyLevel1HBars(bars), bars[len(bars)-1].OpenTime+59_999)
	reLast, ok := b52LastBox(restart.State.PostEscapeBoxes, FTGL)
	if !ok {
		t.Fatalf("restart seed did not replay the post-escape FTGL: %+v", restart.State.PostEscapeBoxes)
	}
	if reLast.Key != liveLast.Key || reLast.Bottom != liveLast.Bottom || reLast.Top != liveLast.Top {
		t.Fatalf("restart box %+v ≠ continuous %+v", reLast, liveLast)
	}
	if got := restart.State.LatchedBoxes["ftgl"]; got.Key != "ftgl:96.80:96.00" || got.Bottom != 96 || got.Top != 96.8 {
		t.Fatalf("restart first FTGL = %+v, want [96, 96.8]", got)
	}
}

// TestB52NewZoneAfterEscape1014 — the dispatch's canonical shape on the REAL
// 10-14 tape: the first-latched FTGL stands (never redrawn) and, after its
// 08:57 escape, a post-escape FTGL is latched below the old floor — the new
// zone the course draws ~150 pts below [D3.4 p3 recovered2 @00:00–00:21].
func TestB52NewZoneAfterEscape1014(t *testing.T) {
	all := loadSeedStateFixture(t)
	full := ssTape(all, true, 1<<62)
	first := ssIndexOf(full, ssRoll)
	if first < 0 {
		t.Fatal("fixture lacks the 10-13 17:00 roll")
	}
	e := ssSeed(full, BarCloseInstant(full[first-1]), ssCPI)
	for i := first; i < len(full); i++ {
		e.Cfg.PrintWindows = ssCPI
		e.Tick(lrWindow(full, i), BarCloseInstant(full[i]))
	}
	old := e.State.LatchedBoxes["ftgl"]
	if old.Bottom == 0 || old.Top == 0 {
		t.Fatalf("10-14 never latched a first FTGL: %+v", old)
	}
	nb, ok := b52LastBox(e.State.PostEscapeBoxes, FTGL)
	if !ok {
		t.Fatalf("10-14 latched no post-escape FTGL after the floor escaped — post = %+v", e.State.PostEscapeBoxes)
	}
	if nb.Top > old.Bottom {
		t.Fatalf("new FTGL [%.2f, %.2f] is not below the old floor %.2f", nb.Bottom, nb.Top, old.Bottom)
	}
}
