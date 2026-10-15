package mentor

import (
	"sort"
	"strings"
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
	// Bars 0–8 = b52Tape (first FTGL [96, 96.8], escape @8). Then price rallies
	// and makes a STRADDLING double bottom: bottom 96.5 is BELOW the old floor
	// 96, its partner's body 97.1 is ABOVE it — the candidate [96.5, 97.1]
	// straddles the old range and must be refused ("no third zone in the
	// middle"). The tape is the CTO's fold-551 replacement: the candidate really
	// forms (swing low 96.5 @12, partner low 96.9 @16, both confirmed), so the
	// pin reaches the outside check instead of returning nil first.
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
		mk(9, 95.5, 97.5, 95.3, 97.2),
		mk(10, 97.2, 98.5, 97.0, 98.2),
		mk(11, 98.2, 98.6, 97.8, 98.0),
		mk(12, 98.0, 98.2, 96.5, 96.7), // swing low 96.5 (below the floor 96)
		mk(13, 96.7, 97.8, 96.8, 97.6), // confirms @12 (low 96.8 > 96.5)
		mk(14, 97.6, 98.4, 97.4, 98.2),
		mk(15, 98.2, 98.5, 97.9, 98.3),
		mk(16, 98.3, 98.4, 96.9, 97.1), // partner low 96.9 (>96.5, fails to break)
		mk(17, 97.1, 97.9, 97.0, 97.7), // confirms @16 → candidate [96.5, 97.1]
		mk(18, 97.7, 98.0, 97.5, 97.8),
	}
	e := b52Run(bars, b52Cfg())
	if len(e.State.PostEscapeBoxes) != 0 {
		t.Fatalf("latched a straddling zone: %+v — no third zone in the middle [D3.2 p2 @09:47–09:54]", e.State.PostEscapeBoxes)
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

// b52LiveBoxKeys reads the LIVE box set back through the production surface:
// the box edges the tick put into ChartLevels (the same `boxes` slice that
// feeds inside_any_box / near_box / midRangeBoxed / BoxEdgeLocations).
func b52LiveBoxKeys(e *Evaluator) []string {
	keys := map[string]bool{}
	for _, l := range e.State.ChartLevels {
		if l.Kind != KindFTGHEdge && l.Kind != KindFTGLEdge {
			continue
		}
		base := strings.TrimSuffix(strings.TrimSuffix(l.Key, ":top"), ":bottom")
		keys[base] = true
	}
	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestB52TwoLiveZonesRetireOlder — the CTO 2-zone ruling (fold-551): three
// successive FTGL escapes latch F0→P1→P2→P3, and after EACH latch exactly TWO
// zones are live — the newest plus the zone it escaped from. Every older zone
// retires from the live reads. The ladder still counts (PostEscapeBoxes grows
// 1→2→3). Mutant: liveBoxes returns all boxes (drop the retire) → the live set
// after P2/P3 grows past 2 → RED.
func TestB52TwoLiveZonesRetireOlder(t *testing.T) {
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	// F0 [96, 96.8] → escape @8 → P1 [92, 92.9] → escape @17 → P2 [88, 88.9]
	// → escape @26 → P3 [84, 84.9].
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 100.5, 98, 100),
		mk(2, 99, 99.5, 96, 97), // F0 extreme 96
		mk(3, 97, 98, 97, 97.5),
		mk(4, 98, 98.5, 98, 98),
		mk(5, 98, 98.5, 97.5, 98),
		mk(6, 97, 97.5, 96.5, 96.8), // F0 partner 96.5
		mk(7, 96.8, 97.5, 96.6, 97), // F0 [96, 96.8]
		mk(8, 95.5, 96, 95, 95),     // F0 escape
		mk(9, 95, 95.5, 94.5, 95),
		mk(10, 94.8, 95, 94, 94.6),
		mk(11, 94, 94.5, 92, 93), // P1 extreme 92
		mk(12, 93, 93.5, 92.5, 93),
		mk(13, 93, 93.5, 93, 93.2),
		mk(14, 93.2, 94, 93.2, 93.8),
		mk(15, 93.5, 94, 92.8, 92.9), // P1 partner 92.8
		mk(16, 93, 94, 93, 93.5),     // P1 [92, 92.9]
		mk(17, 91.5, 92, 91, 91),     // P1 escape
		mk(18, 91, 91.5, 90.5, 91),
		mk(19, 90.8, 91, 90, 90.6),
		mk(20, 90, 90.5, 88, 89), // P2 extreme 88
		mk(21, 89, 89.5, 88.5, 89),
		mk(22, 89, 89.5, 89, 89.2),
		mk(23, 89.2, 90, 89.2, 89.8),
		mk(24, 89.5, 90, 88.8, 88.9), // P2 partner 88.8
		mk(25, 89, 90, 89, 89.5),     // P2 [88, 88.9]
		mk(26, 87.5, 88, 87, 87),     // P2 escape
		mk(27, 87, 87.5, 86.5, 87),
		mk(28, 86.8, 87, 86, 86.6),
		mk(29, 86, 86.5, 84, 85), // P3 extreme 84
		mk(30, 85, 85.5, 84.5, 85),
		mk(31, 85, 85.5, 85, 85.2),
		mk(32, 85.2, 86, 85.2, 85.8),
		mk(33, 85.5, 86, 84.8, 84.9), // P3 partner 84.8
		mk(34, 85, 86, 85, 85.5),     // P3 [84, 84.9]
	}
	e := New(b52Cfg())
	check := func(at int, wantLadder int, wantLive []string) {
		t.Helper()
		if got := len(e.State.PostEscapeBoxes); got != wantLadder {
			t.Fatalf("tick %d: ladder = %d, want %d (post = %+v)", at, got, wantLadder, e.State.PostEscapeBoxes)
		}
		if got := b52LiveBoxKeys(e); strings.Join(got, ",") != strings.Join(wantLive, ",") {
			t.Fatalf("tick %d: live zones = %v, want %v", at, got, wantLive)
		}
	}
	for i := 2; i <= len(bars); i++ {
		e.Tick(bars[:i], bars[i-1].OpenTime+59_999)
		switch i {
		case 8: // F0 latched, no escape yet
			check(i, 0, []string{"ftgl:96.80:96.00"})
		case 17: // P1 latched
			check(i, 1, []string{"ftgl:92.90:92.00", "ftgl:96.80:96.00"})
		case 26: // P2 latched — F0 retires
			check(i, 2, []string{"ftgl:88.90:88.00", "ftgl:92.90:92.00"})
		case 35: // P3 latched — P1 retires
			check(i, 3, []string{"ftgl:84.90:84.00", "ftgl:88.90:88.00"})
		}
	}
}

// TestB52OneBoxPerEscape — one new zone per escape, and only when the ACTIVE
// box escapes. Here P1 forms below F0's floor, then a LOWER double bottom (88 /
// 88.8) forms while P1's floor 92 is never BROKEN (no body closes below 92) —
// so the correct code latches exactly P1 and stops. Mutant B2 (activeBoxOf
// ignores the post-escape boxes, box.go ~479) keeps F0 as the active box, so
// F0's still-escaped state re-fires and latches the 88 zone too → 2 boxes → RED.
func TestB52OneBoxPerEscape(t *testing.T) {
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 100.5, 98, 100),
		mk(2, 99, 99.5, 96, 97), // F0 extreme 96
		mk(3, 97, 98, 97, 97.5),
		mk(4, 98, 98.5, 98, 98),
		mk(5, 98, 98.5, 97.5, 98),
		mk(6, 97, 97.5, 96.5, 96.8), // F0 partner 96.5
		mk(7, 96.8, 97.5, 96.6, 97), // F0 [96, 96.8]
		mk(8, 95.5, 96, 95, 95),     // F0 escape
		mk(9, 95, 95.5, 94.5, 95),
		mk(10, 94.8, 95, 94, 94.6),
		mk(11, 94, 94.5, 92, 93), // P1 extreme 92
		mk(12, 93, 93.5, 92.5, 93),
		mk(13, 93, 93.5, 93, 93.2),
		mk(14, 93.2, 94, 93.2, 93.8),
		mk(15, 93.5, 94, 92.8, 92.9), // P1 partner 92.8
		mk(16, 93, 94, 93, 93.5),     // P1 [92, 92.9]
		// A lower double bottom (wicks to 88 / 88.8) whose BODIES never close
		// below P1's floor 92 — P1 never escapes.
		mk(17, 93.5, 94, 92, 93.5),
		mk(18, 93.5, 94, 92.2, 93.8),
		mk(19, 93, 93.5, 91.5, 93),
		mk(20, 92.5, 93, 88, 92.5), // lower extreme wick 88, body 92.5
		mk(21, 92.5, 93, 91, 92.8), // confirms @20 (91 > 88)
		mk(22, 92.8, 93, 91.5, 92.5),
		mk(23, 92.5, 93, 92, 92.5),
		mk(24, 92.5, 93, 88.8, 92.5), // partner wick 88.8, body 92.5
		mk(25, 92.5, 93, 92, 92.5),   // confirms @24 (92 > 88.8)
	}
	e := b52Run(bars, b52Cfg())
	if len(e.State.PostEscapeBoxes) != 1 {
		t.Fatalf("latched %d boxes, want 1 (P1 only — P1 never escaped): %+v", len(e.State.PostEscapeBoxes), e.State.PostEscapeBoxes)
	}
	if e.State.PostEscapeBoxes[0].Key != "ftgl:92.90:92.00" {
		t.Fatalf("post-escape box = %+v, want P1 ftgl:92.90:92.00", e.State.PostEscapeBoxes[0])
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
