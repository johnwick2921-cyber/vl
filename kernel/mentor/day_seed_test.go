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

// ── release #30 B1 — a restart must NOT re-decide the trading day ──────────
//
// 10-15: the 08:30 latch froze SPENT (run 456, 4h short · 1h short). The 11:32
// and 13:43 restarts re-latched DAY OFF from the BOOT's 4h/1h (4h long · 1h
// short), and the 13:43 boot lost the 13:00 recheck clear — 165 RTH minutes
// wrong. The seed now restores the 08:30 decision (SeedDayLatch), so a restart
// holds the same Day verdict as a bot that never restarted.
//
// These are KERNEL-level pins (SeedDayLatch / SeedTrendlines in isolation, via
// the dsSeedAtStart mirror below). The PRODUCTION call-site pin — at.mentorSeedAtStart
// + the evaluator Tick on the production window — lives in
// trader/mentor_day_seed_test.go (TestMentorSeedRestoresDayLatch1015), which is
// the test whose mutants drop the trader's SeedDayLatch / SeedTrendlines calls.

// dsCfg is the evaluator config for the 10-15 tape: DefaultConfig + the live
// switches. The tape has no red-folder print windows, so the news knobs are inert.
func dsCfg() Config {
	c := DefaultConfig()
	c.Enabled = true
	c.RealFillOnly = true
	return c
}

// dsSeedAtStart mirrors the trader's mentorSeedAtStart seed sequence on the
// kernel side: SeedFull + SeedOpenHTF + SeedDayLatch + SeedTrendlines. It is
// NOT the call-site proof — it exists so the kernel logic can be pinned in
// isolation; the trader test drives the real mentorSeedAtStart.
func dsSeedAtStart(bars []market.Kline, now int64) *Evaluator {
	hist := closedOnly(bars, now)
	e := New(dsCfg())
	SeedFull(e, hist, keyLevel1HBars(hist), now)
	SeedOpenHTF(e, hist, HTF{}, now)
	SeedDayLatch(e, hist, now)
	SeedTrendlines(e, hist, now)
	return e
}

func loadDaySeedFixture(t *testing.T) []market.Kline {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "mnq_1m_2026-10-15_dayseed.json.gz"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gunzip fixture: %v", err)
	}
	var doc struct {
		Bars [][]float64 `json:"bars"`
	}
	if err := json.NewDecoder(gz).Decode(&doc); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	out := make([]market.Kline, 0, len(doc.Bars))
	for _, r := range doc.Bars {
		ot := int64(r[0])
		out = append(out, market.Kline{
			OpenTime: ot, Open: r[1], High: r[2], Low: r[3], Close: r[4], Volume: r[5],
			CloseTime: ot + 59_999, Final: true,
		})
	}
	return out
}

func dsWindow(bars []market.Kline) []market.Kline {
	if len(bars) <= mentorTickWindow {
		return bars
	}
	return bars[len(bars)-mentorTickWindow:]
}

func dsIdx(bars []market.Kline, openTime int64) int {
	for i := range bars {
		if bars[i].OpenTime == openTime {
			return i
		}
	}
	return -1
}

// TestDayLatchSurvivesRestart1015 — a restart after 08:30 on the 10-15 tape
// must seed the SAME Day verdict the continuous bot froze at 08:30, and tick
// identically through 16:00. MUTANT: drop the SeedDayLatch call → the restart
// re-latches from the boot HTF (DAY OFF) and the seed/parity checks go RED.
func TestDayLatchSurvivesRestart1015(t *testing.T) {
	bars := loadDaySeedFixture(t)
	roll := int64(1792015200000) // 10-14 17:00 CT — the session the day gate reads
	end := int64(1792098000000)  // 10-15 16:00 CT

	// The continuous bot: seed at the roll, tick every bar to 16:00.
	live := dsSeedAtStart(bars, roll)
	if live.State.Day.Key != "" {
		t.Fatalf("a pre-open seed must not latch the day gate yet, got key %q", live.State.Day.Key)
	}
	liveDay := map[int64]DayVerdict{}
	var win []market.Kline
	for _, b := range bars {
		if b.OpenTime < roll || b.OpenTime > end {
			continue
		}
		win = append(win, b)
		live.Tick(dsWindow(win), BarCloseInstant(b))
		liveDay[b.OpenTime] = live.State.Day.Verdict
	}
	// The 10-15 08:30 read must be a SPENT day (run ≥ 300, 4h/1h agree short) —
	// the premise of the bug.
	if openIdx := dsIdx(bars, 1792071000000); openIdx > 0 {
		if v := liveDay[1792071000000]; v != DaySpent {
			t.Fatalf("the continuous 08:30 latch must be SPENT, got %v", v)
		}
	}

	for _, rs := range []int64{1792081976000, 1792089811000} { // 11:32:56, 13:43:31 CT
		rsIdx := -1
		for i := range bars {
			if BarCloseInstant(bars[i]) >= rs {
				rsIdx = i
				break
			}
		}
		if rsIdx < 0 {
			t.Fatalf("restart instant %d has no bar", rs)
		}
		seedAt := BarCloseInstant(bars[rsIdx])
		re := dsSeedAtStart(bars, seedAt)
		// The seed ALONE must carry the 08:30 verdict (no tick to hide a re-latch).
		if want := liveDay[bars[rsIdx].OpenTime]; re.State.Day.Verdict != want {
			t.Fatalf("restart @%s: seeded day verdict %v, continuous %v (key %q vs %q)",
				clockCT(bars[rsIdx].OpenTime), re.State.Day.Verdict, want, re.State.Day.Key, live.State.Day.Key)
		}
		// And tick identically through 16:00.
		var rwin []market.Kline
		for i := rsIdx + 1; i < len(bars); i++ {
			b := bars[i]
			if b.OpenTime > end {
				break
			}
			rwin = append(rwin, b)
			re.Tick(dsWindow(rwin), BarCloseInstant(b))
			if got, want := re.State.Day.Verdict, liveDay[b.OpenTime]; got != want {
				t.Fatalf("restart @%s, bar %s: day verdict %v, continuous %v",
					clockCT(bars[rsIdx].OpenTime), clockCT(b.OpenTime), got, want)
			}
		}
	}
}

func clockCT(ms int64) string {
	return timeCT(ms).Format("01-02 15:04")
}

func timeCT(ms int64) time.Time {
	return time.UnixMilli(ms).In(ctime())
}

// TestDayRecheckSurvivesRestart — B1's recheck replay at the SeedDayLatch call
// site: a DayOff latched at 08:30 that CLEARED on a closed 1h bar before the
// restart must stay cleared. The tape draws 4h short (01:00) and 1h long
// (08:00) at the read → DAY OFF (run 400); the 10:00 1h bucket breaks the prior
// 1h low → the 1h flips short and the clear replays to DaySpent.
// DayOffRecheck is set TRUE explicitly — this tests the recheck path, not the
// default (which #561 turns OFF).
// MUTANT: drop the replayDayOffRecheck call in SeedDayLatch → RED (stays DayOff).
func TestDayRecheckSurvivesRestart(t *testing.T) {
	tape := dayOffClearTape()
	cfg := dsCfg()
	cfg.DayOffRecheck = true
	e := New(cfg)
	now := int64(1792015200000) + 1080*60_000 // 11:00 CT — the 10:00 bucket has closed
	SeedDayLatch(e, tape, now)
	if e.State.Day.Verdict != DaySpent {
		t.Fatalf("the 08:30 DayOff cleared at 10:00 must be restored DaySpent, got %v", e.State.Day.Verdict)
	}
	if e.State.DayRecheck.ClearedAt == 0 || e.State.DayRecheck.Clears != 1 {
		t.Fatalf("the recheck clear must be replayed once, got %+v", e.State.DayRecheck)
	}
}

// TestDayRecheckOffStaysDayOff — the knob: with DayOffRecheck OFF (the #561
// default), SeedDayLatch restores the 08:30 DayOff and does NOT replay the
// recheck, so a clear that happened before the restart is NOT resurrected —
// the restored day stays DayOff and no clear is recorded.
func TestDayRecheckOffStaysDayOff(t *testing.T) {
	tape := dayOffClearTape()
	cfg := dsCfg()
	cfg.DayOffRecheck = false
	e := New(cfg)
	now := int64(1792015200000) + 1080*60_000 // 11:00 CT — the 10:00 bucket has closed
	SeedDayLatch(e, tape, now)
	if e.State.Day.Verdict != DayOff {
		t.Fatalf("with the recheck OFF the restored DayOff must stay DayOff, got %v", e.State.Day.Verdict)
	}
	if e.State.DayRecheck.ClearedAt != 0 || e.State.DayRecheck.Clears != 0 {
		t.Fatalf("with the recheck OFF no clear may be replayed, got %+v", e.State.DayRecheck)
	}
}

// dayOffClearTape builds a 10-14 17:00 → 10-15 10:59 tape: overnight run 400
// (high 30300, low 29900); the 01:00 4h bucket breaks the prior low (4h short);
// the 08:00 1h bucket breaks the prior high (1h long) → 08:30 conflict = DAY
// OFF; the 10:00 1h bucket breaks the prior low → 1h short → agreement clear.
func dayOffClearTape() []market.Kline {
	roll := int64(1792015200000) // 10-14 17:00 CT
	var bars []market.Kline
	seg := func(fromMin, toMin int, o, h, l, c float64) {
		for m := fromMin; m < toMin; m++ {
			ms := roll + int64(m)*60_000
			bars = append(bars, market.Kline{OpenTime: ms, CloseTime: ms + 59_999, Open: o, High: h, Low: l, Close: c, Final: true})
		}
	}
	seg(0, 480, 30250, 30300, 30200, 30250)     // 17:00–00:59 (17:00 + 21:00 4h buckets)
	seg(480, 720, 30100, 30250, 30000, 30100)   // 01:00–04:59 (4h short at 01:00)
	seg(720, 840, 30050, 30100, 29900, 30050)   // 05:00–06:59
	seg(840, 900, 30020, 30050, 30000, 30020)   // 07:00–07:59
	seg(900, 930, 30050, 30150, 30020, 30080)   // 08:00–08:29 (1h long at 08:00)
	seg(930, 960, 30080, 30150, 30010, 30080)   // 08:30–08:59
	seg(960, 1020, 30080, 30150, 30010, 30080)  // 09:00–09:59
	seg(1020, 1080, 29800, 29900, 29600, 29800) // 10:00–10:59 (1h short at 10:00)
	return bars
}

// TestSeedTrendlinesMarksCrossed — B3: the seed marks the trendline pairs
// already crossed before now, so the first tick does not recount them. Uses the
// real 10-15 tape (which carries crossed-between-anchors pairs).
// MUTANT: drop the SeedTrendlines call → the first tick recounts → RED.
func TestSeedTrendlinesMarksCrossed(t *testing.T) {
	bars := loadDaySeedFixture(t)
	now := int64(1792098000000) // 10-15 16:00 CT
	e := New(dsCfg())
	SeedTrendlines(e, bars, now)

	// m407: the same frame the seed (and Tick) build on — 2m at 16:00 CT.
	lines, crossed := trendlinesBuildExec(closedOnly(bars, now), time.UnixMilli(now), e.Cfg.Exec2mAfter30m && exec2mReads2m(now))
	if len(crossed) == 0 {
		t.Fatal("the 10-15 fixture must carry crossed-between-anchors pairs for this test to pin anything")
	}
	if e.tlDay == "" {
		t.Fatal("SeedTrendlines must set the trendline day")
	}
	for _, k := range crossed {
		if !e.tlCrossed[k] {
			t.Fatalf("crossed pair %q not marked by the seed", k)
		}
	}
	before := e.State.Refusals["trendline_crossed_between_anchors"]
	e.trackTrendlines(lines, nil, crossed, now)
	if got := e.State.Refusals["trendline_crossed_between_anchors"]; got != before {
		t.Fatalf("the first tick recounted already-crossed pairs: %d → %d", before, got)
	}
}
