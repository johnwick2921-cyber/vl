package mentor

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vl/market"
)

// ── release #22 LINES-RESTART — a restart must reproduce the live trigger state ──
//
// P1-1 (MENTOR-AUDIT-SETUPS-CONTEXT, MATH-LEVELS-LINES #7): the live path reads
// the forming 5m/1h/4h bucket INTRABAR on every 1m tick, so the order in which a
// bucket broke the prior high and low decides the line [D3.4 p1 @08:19, p2
// @18:49]. The seed read history bucket-complete, so a restart inside a
// "broke both" bucket handed the bot a different line — and a different trading
// direction. Measured at the base on these bars: the HTF verdict differed in 20
// of 276 reseed samples (10-06→10-08), the 4h line in 44 of 138 (10-13).
//
// These pins drive the PRODUCTION call sites on REAL recorded bars
// (testdata/mnq_1m_lines_restart_2026-10.json.gz, extracted read-only by
// extract_lines_restart_fixture.py): a continuous evaluator seeded once and then
// ticked bar by bar exactly as mentorEvalOnce does (Tick over the last 1500
// closed bars at BarCloseInstant) vs a FRESH SeedFull at the same instant —
// what a restart does. Mutant: seed from completed candles again (TriggerTick
// over barsTF of the whole history in seed()) → RED.

type lrFixtureSpan struct {
	FromCT string      `json:"from_ct"`
	ToCT   string      `json:"to_ct"`
	Bars   [][]float64 `json:"bars"`
}

func loadLinesRestartFixture(t *testing.T) map[string][]market.Kline {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "mnq_1m_lines_restart_2026-10.json.gz"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gunzip fixture: %v", err)
	}
	var doc struct {
		Spans map[string]lrFixtureSpan `json:"spans"`
	}
	if err := json.NewDecoder(gz).Decode(&doc); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	out := map[string][]market.Kline{}
	for name, sp := range doc.Spans {
		bars := make([]market.Kline, 0, len(sp.Bars))
		for _, r := range sp.Bars {
			ot := int64(r[0])
			bars = append(bars, market.Kline{
				OpenTime: ot, Open: r[1], High: r[2], Low: r[3], Close: r[4], Volume: r[5],
				CloseTime: ot + 59_999, Final: true,
			})
		}
		out[name] = bars
	}
	return out
}

// lrProdCfg is mentorEvaluatorConfig for the live trader with no tuning:
// DefaultConfig + Enabled + RealFillOnly, with the 10-13/10-14 historical pin
// (news_bars_excluded OFF — the live bot on those days ran the item-18
// print-window drop, not the release #29 freeze; release #25 CE-4 did not exist).
func lrProdCfg() Config {
	c := DefaultConfig()
	c.Enabled = true
	c.RealFillOnly = true
	c.NewsBarsExcluded = false // historical 10-13/10-14 pin (pre-#25 CE-4)
	return c
}

// lrRestartAt is what a restart does at `now`: a fresh evaluator, SeedFull on
// every bar closed by now (the trader passes the stitched history; the key-level
// walk runs on its RTH 1H aggregation).
func lrRestartAt(bars []market.Kline, now int64) *Evaluator {
	hist := closedOnly(bars, now)
	e := New(lrProdCfg())
	SeedFull(e, hist, keyLevel1HBars(hist), now)
	return e
}

func lrWindow(bars []market.Kline, i int) []market.Kline {
	lo := i + 1 - mentorTickWindow
	if lo < 0 {
		lo = 0
	}
	return bars[lo : i+1]
}

// mentorTickWindow mirrors trader.mentorBars1mDepth (the live Tick slice).
const mentorTickWindow = 1500

func lrCT(t time.Time) string { return t.In(ctime()).Format("01-02 15:04") }

func TestRestartReproducesLiveTriggerState(t *testing.T) {
	fx := loadLinesRestartFixture(t)
	for _, name := range []string{"A", "B"} {
		bars := fx[name]
		if len(bars) < 2000 {
			t.Fatalf("fixture span %s has %d bars — extraction broken", name, len(bars))
		}
		// The continuous bot boots after ~10h of history (so the 4h line exists),
		// then ticks every closed bar — the live path.
		const bootIdx = 600
		live := lrRestartAt(bars, BarCloseInstant(bars[bootIdx-1]))
		samples, diff, verdictDiff := 0, 0, 0
		for i := bootIdx; i < len(bars); i++ {
			now := BarCloseInstant(bars[i])
			live.Tick(lrWindow(bars, i), now)
			if (i-bootIdx)%10 != 9 {
				continue
			}
			samples++
			re := lrRestartAt(bars, now)
			// 1) the seed alone must already BE the live state (no tick to hide it).
			same := re.State.Trigger == live.State.Trigger &&
				re.State.HTF.FourH == live.State.HTF.FourH &&
				re.State.HTF.OneH == live.State.HTF.OneH
			// 2) and stay it after the first live tick on the same window.
			re.Tick(lrWindow(bars, i), now)
			same = same && re.State.Trigger == live.State.Trigger &&
				re.State.HTF.FourH == live.State.HTF.FourH &&
				re.State.HTF.OneH == live.State.HTF.OneH
			lv, ls, _ := HTFVerdict(live.State.HTF)
			rv, rs, _ := HTFVerdict(re.State.HTF)
			if lv != rv || ls != rs {
				verdictDiff++
			}
			if !same {
				diff++
				if diff <= 6 {
					t.Errorf("span %s %s CT: restart ≠ live — 4h live %s@%.2f(%s) vs restart %s@%.2f(%s) · 1h live %s@%.2f vs restart %s@%.2f · 5m live %s@%.2f vs restart %s@%.2f",
						name, lrCT(time.UnixMilli(bars[i].OpenTime)),
						live.State.HTF.FourH.Dir, live.State.HTF.FourH.Price, lrCT(time.UnixMilli(live.State.HTF.FourH.MovedAt)),
						re.State.HTF.FourH.Dir, re.State.HTF.FourH.Price, lrCT(time.UnixMilli(re.State.HTF.FourH.MovedAt)),
						live.State.HTF.OneH.Dir, live.State.HTF.OneH.Price, re.State.HTF.OneH.Dir, re.State.HTF.OneH.Price,
						live.State.Trigger.Dir, live.State.Trigger.Price, re.State.Trigger.Dir, re.State.Trigger.Price)
				}
			}
		}
		t.Logf("span %s: %d restart samples · trigger state differs %d · HTF verdict differs %d", name, samples, diff, verdictDiff)
		if diff != 0 || verdictDiff != 0 {
			t.Errorf("span %s: a restart changed the trigger state in %d/%d samples (HTF verdict in %d) — the seed must replay the 1m bars like live", name, diff, samples, verdictDiff)
		}
	}
}

// TestRestartBootLine1013 pins the exact live incident on its own bars: the
// 10-13 18:57:09 CT boot printed "4h trigger long since 2026-10-13T01:00"
// (30741.25) while the running bot held the 4h long at 30774.25, moved by the
// 05:00 bucket (short 08:28, long 08:41 — two moves inside one 4h candle). The
// restart line must print what live holds: direction, price AND since.
func TestRestartBootLine1013(t *testing.T) {
	bars := loadLinesRestartFixture(t)["B"]
	boot := time.Date(2026, time.October, 13, 18, 57, 9, 0, ctime()).UnixMilli()
	start := time.Date(2026, time.October, 12, 17, 0, 0, 0, ctime()).UnixMilli()
	// live: booted at the 10-12 17:00 open, ticked every bar to the 18:57 boot.
	var live *Evaluator
	for i, b := range bars {
		if b.CloseTime >= boot {
			break
		}
		now := BarCloseInstant(b)
		if live == nil {
			if now < start {
				continue
			}
			live = lrRestartAt(bars, now)
		}
		live.Tick(lrWindow(bars, i), now)
	}
	if live == nil {
		t.Fatal("fixture span B does not cover 10-12 17:00 → 10-13 18:57")
	}
	if live.State.HTF.FourH.Dir != SideLong || live.State.HTF.FourH.Price != 30774.25 {
		t.Fatalf("control: the live 4h at the 18:57 boot should be long @30774.25 (05:00 bucket), got %s@%.2f", live.State.HTF.FourH.Dir, live.State.HTF.FourH.Price)
	}
	re := lrRestartAt(bars, boot)
	if re.State.HTF.FourH != live.State.HTF.FourH {
		t.Fatalf("the 18:57 restart re-seeded the 4h at %s@%.2f since %s; live held %s@%.2f since %s",
			re.State.HTF.FourH.Dir, re.State.HTF.FourH.Price, lrCT(time.UnixMilli(re.State.HTF.FourH.MovedAt)),
			live.State.HTF.FourH.Dir, live.State.HTF.FourH.Price, lrCT(time.UnixMilli(live.State.HTF.FourH.MovedAt)))
	}
	line := re.SeedLine(2)
	const want = "4h trigger long @30774.25 since 2026-10-13T05:00"
	if !containsStr(line, want) {
		t.Fatalf("boot line %q must carry %q (direction, price AND since, read from the seeded state)", line, want)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestBoxLatchHoldsThroughRTHOpenUntil1700 — P2-2 (MATH-LEVELS-LINES #5): the
// box latch was keyed on the day-gate key, which is "" from 17:00 to 08:30 and
// the date afterwards, so the latch cleared at the 08:30 RTH open and the boxes
// were REDRAWN (10-13: FTGH 30894.25–30901.75 → 30899.25–30905.50 on a higher
// high). The course: "never erase, never redraw — a higher high does NOT
// justify a new box; keep to end of day" [D4.1 p2 @02:39, @02:58]. The latch
// clears only at the 17:00 CT session roll. Production call site: Tick on the
// real 10-12/10-13 bars. Mutant: key the latch on e.State.Day.Key again → RED.
func TestBoxLatchHoldsThroughRTHOpenUntil1700(t *testing.T) {
	bars := loadLinesRestartFixture(t)["B"]
	open := time.Date(2026, time.October, 12, 17, 0, 0, 0, ctime()).UnixMilli()
	rth := time.Date(2026, time.October, 13, 8, 30, 0, 0, ctime()).UnixMilli()
	roll := time.Date(2026, time.October, 13, 17, 0, 0, 0, ctime()).UnixMilli()
	var e *Evaluator
	var preOpen, afterOpen, beforeRoll map[string]Box
	snap := func() map[string]Box {
		m := map[string]Box{}
		for k, v := range e.State.LatchedBoxes {
			m[k] = v
		}
		return m
	}
	for i, b := range bars {
		now := BarCloseInstant(b)
		if now <= open {
			continue
		}
		if e == nil {
			e = lrRestartAt(bars, BarCloseInstant(bars[i-1]))
		}
		e.Tick(lrWindow(bars, i), now)
		switch {
		case now < rth:
			preOpen = snap()
		case afterOpen == nil:
			afterOpen = snap()
		}
		if now < roll {
			beforeRoll = snap()
		}
	}
	if len(preOpen) == 0 {
		t.Fatal("control: no box latched overnight 10-12 17:00 → 10-13 08:30 — the fixture does not exercise the latch")
	}
	for role, b := range preOpen {
		if got := afterOpen[role]; got.Key != b.Key {
			t.Errorf("%s redrawn at the 08:30 RTH open: %s → %s (the latch must hold to the 17:00 roll)", role, b.Key, got.Key)
		}
		if got := beforeRoll[role]; got.Key != b.Key {
			t.Errorf("%s changed before the 17:00 roll: %s → %s", role, b.Key, got.Key)
		}
	}
	if e.State.LatchedBoxesDay != "2026-10-14" {
		t.Errorf("after the 10-13 17:00 roll the latch must belong to trading day 2026-10-14, got %q", e.State.LatchedBoxesDay)
	}
	for role, b := range e.State.LatchedBoxes {
		if b.FormedAtMs < roll {
			t.Errorf("after the 17:00 roll the %s latch still holds a box formed before the roll (%s)", role, b.Key)
		}
	}
}

// TestTriggerMoveIsLogged1014 — the 02:10 boot's "4h short since
// 2026-10-14T01:00" was a REAL live move (the 10-14 01:00 bucket broke the
// 21:00–01:00 low on its first minute), but no line ever recorded it, so it
// read like a restart flip. Every 4h/1h move is now a change line the trader
// logs (TakeNotes), with the bucket and the bar that fired it.
func TestTriggerMoveIsLogged1014(t *testing.T) {
	bars := loadLinesRestartFixture(t)["B"]
	boot := time.Date(2026, time.October, 13, 18, 57, 9, 0, ctime()).UnixMilli()
	end := time.Date(2026, time.October, 14, 1, 5, 0, 0, ctime()).UnixMilli()
	e := lrRestartAt(bars, boot)
	var notes []string
	for i, b := range bars {
		now := BarCloseInstant(b)
		if now <= boot || now > end {
			continue
		}
		e.Tick(lrWindow(bars, i), now)
		notes = append(notes, e.TakeNotes()...)
	}
	const want = "4h trigger moved long @30774.25 → short @30745.50 (bucket 10-14 01:00 CT, broke on the 10-14 01:00 CT bar"
	for _, n := range notes {
		if containsStr(n, want) {
			return
		}
	}
	t.Fatalf("no change line for the 10-14 01:00 4h move; notes=%q", notes)
}

// TestTrendlinesKeptAllSession1013 (m428: an UNBROKEN line is kept all session; the
// single non-adjacent line per side may be replaced, R5) — owner ruling R4 + course D4.1 ("Extend
// right; never erase during the session; DELETE at end of day" [D4.1 p2
// @01:17]); the 3rd-touch rule (#504) is unchanged. MATH-LEVELS-LINES #6
// measured on 10-13: ONE line per side, the newest pair replacing the older —
// 135 of 151 lines vanished without a 5m break; and 40 of 151 were crossed by
// price between their own anchors ("TRENDLINE chứ không phải CẦU TUỘT"
// [X9 @01:22–02:10]). Production call site: TrendlinesBuild with the exact
// inputs Tick hands it (the last 1500 closed bars, the bar-close instant), on
// the real 10-12 17:00 → 10-13 16:00 session.
// Mutant: newest-pair replacement (only the last pair draws) → RED.
func TestTrendlinesKeptAllSession1013(t *testing.T) {
	bars := loadLinesRestartFixture(t)["B"]
	open := time.Date(2026, time.October, 12, 17, 0, 0, 0, ctime()).UnixMilli()
	end := time.Date(2026, time.October, 13, 16, 0, 0, 0, ctime()).UnixMilli()
	prevDrawn := map[string]Trendline{}
	drawn, vanished, validErased, crossedDrawn, validSeen, maxLive := map[string]bool{}, 0, 0, 0, map[string]bool{}, 0
	for i, b := range bars {
		now := BarCloseInstant(b)
		if now <= open || now > end {
			continue
		}
		win := lrWindow(bars, i)
		lines := TrendlinesBuild(win, time.UnixMilli(now))
		cur := map[string]Trendline{}
		live := 0
		for _, tl := range lines {
			if tl.ValidAt > 0 && !tl.Dead {
				live++
			}
			k := tl.key()
			cur[k] = tl
			if !drawn[k] {
				drawn[k] = true
				// re-resolve the anchors in this window and test the candles between
				if trendlineCrossed(win, tl) {
					crossedDrawn++
				}
			}
			if tl.ValidAt > 0 && !tl.Dead {
				validSeen[k] = true
			}
		}
		anchors := map[int64]bool{}
		for _, p := range prevDrawn {
			anchors[p.P0T], anchors[p.P1T] = true, true
		}
		for _, c := range cur {
			anchors[c.P0T], anchors[c.P1T] = true, true
		}
		skips := func(l Trendline) bool { // a non-adjacent line: an anchor lies strictly between its two
			for a := range anchors {
				if a > l.P0T && a < l.P1T {
					return true
				}
			}
			return false
		}
		for k, p := range prevDrawn {
			if _, ok := cur[k]; ok {
				continue
			}
			// m428 R5 (owner 2026-10-25): the ONE non-adjacent line per side may be replaced by
			// a better / continuing one while it is still FORMING — the only drawn line that
			// may leave without a break. A VALID line is never replaced (counted below).
			replaced := skips(p) && !(p.ValidAt > 0 && !p.Dead)
			if replaced {
				continue
			}
			vanished++
			if p.ValidAt > 0 && !p.Dead {
				validErased++
				if validErased <= 3 {
					t.Errorf("%s CT: VALID %s line %.2f→%.2f erased mid-session without a 5m break", lrCT(time.UnixMilli(b.OpenTime)), p.Side, p.P0Px, p.P1Px)
				}
			}
		}
		prevDrawn = cur
		if live > maxLive {
			maxLive = live
		}
	}
	t.Logf("10-13 session: %d distinct lines drawn · %d vanished mid-session · %d VALID lines erased · %d valid ever · max %d valid+unbroken at once · %d drawn through price between anchors", len(drawn), vanished, validErased, len(validSeen), maxLive, crossedDrawn)
	if len(validSeen) == 0 {
		t.Fatal("control: no trendline ever turned valid on 10-13 — the pin would be vacuous")
	}
	if vanished != 0 || validErased != 0 {
		t.Errorf("%d drawn lines (%d VALID) vanished mid-session without a 5m-close break — R4: never erased during the session", vanished, validErased)
	}
	if crossedDrawn != 0 {
		t.Errorf("%d lines were drawn through price between their own anchors", crossedDrawn)
	}
}

// swingGapTape: a 24h synthetic minute tape whose 4h closes swing by ±150 pts
// (so a missed close moves the EMA measurably), `days` long from 10-01 00:00 CT.
func swingGapTape(t *testing.T, days int) []market.Kline {
	t.Helper()
	var bars []market.Kline
	for d := 0; d < days; d++ {
		for m := 0; m < 24*60; m++ {
			ms := ctMs(t, d, 0, m)
			x := float64(d*24*60+m) / 97.0
			cl := 20000 + 150*float64(int(x)%5-2)/2 + float64(m%7)*0.25
			bars = append(bars, market.Kline{OpenTime: ms, CloseTime: ms + 59_999, Open: cl - 0.5, High: cl + 1, Low: cl - 1, Close: cl, Final: true})
		}
	}
	return bars
}

func swingTickRange(e *Evaluator, bars []market.Kline, from, to int64) {
	for i, b := range bars {
		now := BarCloseInstant(b)
		if now <= from || now > to {
			continue
		}
		e.Tick(lrWindow(bars, i), now)
	}
}

// TestSwingLineFoldsEveryBucketAcrossAFeedGap — MATH-INDICATORS F-3: on a 4h
// flip the warm swing line folded exactly ONE close (the bucket it was in), so
// a 4h bucket that opened and closed between two ticks (a feed gap of 4h+
// while the process stays up) was lost for good — measured +39.8 pts vs a
// rebuild after a 9.5h gap, decaying ~5.7% per bucket. Production call site:
// Tick (runSwing → SwingTick). After a 9h30 gap the live line must equal a
// fresh restart's line exactly. Mutant: fold only the s.BucketStart close → RED.
func TestSwingLineFoldsEveryBucketAcrossAFeedGap(t *testing.T) {
	bars := swingGapTape(t, 12)
	seedAt := ctMs(t, 10, 8, 0)
	gapFrom := ctMs(t, 10, 12, 0)
	resume := ctMs(t, 10, 21, 30)
	e := New(lrProdCfg())
	Seed(e, closedOnly(bars, seedAt), seedAt)
	if e.State.Swing.EmaCount < FourHEMA34Min {
		t.Fatalf("control: the seeded swing line must be warm (%d closes)", e.State.Swing.EmaCount)
	}
	swingTickRange(e, bars, seedAt, gapFrom)
	// the feed stops 12:00 → 21:30 (buckets 13:00 and 17:00 open AND close unseen)
	swingTickRange(e, bars, resume-60_000, resume)
	re := New(lrProdCfg())
	Seed(re, closedOnly(bars, resume), resume)
	idx := 0
	for i, b := range bars {
		if BarCloseInstant(b) == resume {
			idx = i
		}
	}
	re.Tick(lrWindow(bars, idx), resume)
	if e.State.Swing.Line != re.State.Swing.Line || e.State.Swing.EmaCount != re.State.Swing.EmaCount {
		t.Fatalf("after a 9h30 feed gap the live swing line is %.4f (%d closes); a restart reads %.4f (%d closes) — the missed 4h buckets were never folded",
			e.State.Swing.Line, e.State.Swing.EmaCount, re.State.Swing.Line, re.State.Swing.EmaCount)
	}
	if e.State.Swing.LineStale {
		t.Fatal("a gap inside the 1500-bar slice is recoverable — the line must not be marked stale")
	}
}

// TestSwingLineGapBeyondSliceFailsClosed — a gap longer than the live slice
// (1500 1m bars ≈ 25h) leaves closes the evaluator cannot read: the line is
// marked stale (SWING4H entries refused, counted, logged) instead of jumping
// to the ~6-bucket cold recompute, which F-3 measured up to ~300 pts off.
func TestSwingLineGapBeyondSliceFailsClosed(t *testing.T) {
	bars := swingGapTape(t, 13)
	seedAt := ctMs(t, 9, 8, 0)
	gapFrom := ctMs(t, 9, 12, 0)
	resume := ctMs(t, 11, 2, 30) // 38h30 later
	e := New(lrProdCfg())
	Seed(e, closedOnly(bars, seedAt), seedAt)
	swingTickRange(e, bars, seedAt, gapFrom)
	before := e.State.Swing.Line
	swingTickRange(e, bars, resume-60_000, resume)
	if !e.State.Swing.LineStale {
		t.Fatalf("a 38h gap beyond the slice must mark the swing line stale (line %.2f → %.2f)", before, e.State.Swing.Line)
	}
	notes := e.TakeNotes()
	found := false
	for _, n := range notes {
		if containsStr(n, "swing 4h EMA34 line STALE") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the stale swing line must be a change line; notes=%q", notes)
	}
	n0 := e.State.Refusals["swing_line_gap_stale"]
	kept := e.dropStaleSwingLineEntries([]Intent{{Action: PlaceStopEntry, Setup: "SWING4H"}, {Action: CancelArm, Setup: "SWING4H"}})
	if len(kept) != 1 || kept[0].Action != CancelArm || e.State.Refusals["swing_line_gap_stale"] != n0+1 {
		t.Fatalf("a stale line must refuse swing ENTRIES (counted) and keep cancels: kept=%+v refusals=%v", kept, e.State.Refusals)
	}
}
