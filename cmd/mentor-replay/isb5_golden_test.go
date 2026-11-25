package main

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"vl/calendar"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

// loadFixtureFile reads a stored-day fixture (exported READ-ONLY from data.db,
// columns open_time_ms,o,h,l,c,v,source,convention) into the tool's in-memory
// bar set under fixtureContract.
func loadFixtureFile(t *testing.T, name string) *barSet {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var rows []row
	sc := bufio.NewScanner(gz)
	for sc.Scan() {
		p := strings.Split(sc.Text(), ",")
		if len(p) != 8 {
			t.Fatalf("bad fixture line %q", sc.Text())
		}
		var r row
		r.T, _ = strconv.ParseInt(p[0], 10, 64)
		r.O, _ = strconv.ParseFloat(p[1], 64)
		r.H, _ = strconv.ParseFloat(p[2], 64)
		r.L, _ = strconv.ParseFloat(p[3], 64)
		r.C, _ = strconv.ParseFloat(p[4], 64)
		r.V, _ = strconv.ParseFloat(p[5], 64)
		r.Src, r.Conv = p[6], p[7]
		rows = append(rows, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return &barSet{byTF: map[string]map[string][]row{"1m": {fixtureContract: rows}}}
}

// tickState is the evaluator state the goldens read after one production tick.
type tickState struct {
	box  *mentor.ISBBox
	lock *mentor.BreakLock
	evs  []Event
}

// replayFixtureDay runs day d of the fixture through the tool's own stepping —
// the production seed body at the session start (17:00 CT the evening before),
// then every closed 1m bar to endHM CT through the production per-bar entry
// (trader.MentorReplay.EvalOnce = mentorEvalOnce) with the stored strategy
// config sc (nil = the DEFAULT mentor config) — and returns the state after each tick keyed by the bar's CT HH:MM.
func replayFixtureDay(t *testing.T, fixture, d, endHM string, sc *store.StrategyConfig) map[string]tickState {
	t.Helper()
	bs := loadFixtureFile(t, fixture)
	seedAt, err := seedInstant("session", d)
	if err != nil {
		t.Fatal(err)
	}
	loc := kernel.CTLocation()
	day, _ := time.ParseInLocation("2006-01-02", d, loc)
	end, _ := time.ParseInLocation("15:04", endHM, loc)
	endMs := time.Date(day.Year(), day.Month(), day.Day(), end.Hour(), end.Minute(), 0, 0, loc).UnixMilli()
	var evs []Event
	testCap.take()
	rp := newReplayer(bs, sc, map[string][]calendar.Event{}, testCap, func(ev Event) { evs = append(evs, ev) })
	rp.seed(seedAt)
	out := map[string]tickState{}
	for _, r := range bs.byTF["1m"][fixtureContract] {
		if r.T+60_000 <= seedAt.UnixMilli() || r.T+60_000 > endMs {
			continue
		}
		evs = nil
		b := klines([]row{r}, 60_000)[0]
		if rp.step(b, seedAt.UnixMilli()) {
			t.Fatalf("panic on the %s bar: %v", ct(b.OpenTime), evs)
		}
		st := rp.r.Evaluator().State
		ts := tickState{evs: evs}
		if st.ISBBox != nil {
			c := *st.ISBBox
			ts.box = &c
		}
		if st.BreakLock != nil {
			c := *st.BreakLock
			ts.lock = &c
		}
		if t := time.UnixMilli(b.OpenTime).In(loc); t.Format("2006-01-02") == d {
			out[t.Format("15:04")] = ts
		}
	}
	return out
}

func hm(h, m int) string { return fmt.Sprintf("%02d:%02d", h, m) }

func boxStr(b *mentor.ISBBox) string {
	if b == nil {
		return "<none>"
	}
	return fmt.Sprintf("%s %.2f–%.2f at %s escaped=%q", b.Dir, b.Low, b.High, ct(b.AtTime), b.Escaped)
}

// TestISB5F1CourseDayMergedBox — ISB5-F1, the course's own worked day (Mon
// 2026-11-02, MNQ 12-26 1m from data.db): OctD4.3-R58 [Oct D4.3 @ 57:20] the
// 08:35 candle is the inside bar (box 28,639.75–28,727, screen
// 28,727.50/28,639.75, frame s_03455.jpg); OctD4.3-R59 [Oct D4.3 @ 58:00] "1
// cái nến 5 phút, 2 cái nến 5 phút, 3 cái nến 5 phút nó vẫn chưa thoát được
// cái box này thì nó thành 1 cái Inside Bar ... bọc từ head to toe" — the box
// grows to the 08:40 high 28,748.75 (screen top ~28,750, frames s_03492.jpg /
// s_03507.jpg) and stands until a 5m candle CLOSES outside it (OctD4.3-R67):
// the 09:10 candle, close 28,769.50, read at the 09:14 tick.
//
// Production path end to end: the production seed at 17:00 CT 11-01, then
// every closed 1m bar through mentorEvalOnce with the DEFAULT config.
//
// The 08:41 1m close 28,737.75 is above the box as it stands at that minute
// (28,727) — R66 makes that a break the bot cannot know is false until the
// 08:40 5m candle closes back inside; the merge then takes it back. From the
// 08:45 bar to the 09:09 bar no isb_5m break may happen at all (the newest-ISB
// read broke it short at 08:50 and deleted it at 08:54 and 09:09).
func TestISB5F1CourseDayMergedBox(t *testing.T) {
	if d := mentor.DefaultConfig().FiveMinBoxSource; d != mentor.FiveMinBoxMerge {
		t.Errorf("OctD4.3-R59: the default 5m box source must be %q, got %q", mentor.FiveMinBoxMerge, d)
	}
	got := replayFixtureDay(t, "mnq_12-26_1m_20261030-20261102.csv.gz", "2026-11-02", "09:20", nil)
	at0835 := time.Date(2026, 11, 2, 8, 35, 0, 0, kernel.CTLocation()).UnixMilli()

	if b := got["08:39"].box; b == nil || b.AtTime != at0835 || b.Low != 28_639.75 || b.High != 28_727.00 {
		t.Fatalf("08:39 tick: want the 08:35 box 28,639.75–28,727.00 (OctD4.3-R58), got %s", boxStr(b))
	}
	for m := 44; m <= 9*60+13-8*60; m++ {
		k := hm(8+m/60, m%60)
		b := got[k].box
		if b == nil || b.AtTime != at0835 || b.Low != 28_639.75 || b.High != 28_748.75 {
			t.Fatalf("%s tick: want the merged 08:35 box 28,639.75–28,748.75 (OctD4.3-R59 head to toe), got %s", k, boxStr(b))
		}
		if m >= 45 && m <= 9*60+9-8*60 {
			if b.Escaped != "" {
				t.Errorf("%s tick: no 1m close left the merged box, yet it reads escaped %q", k, b.Escaped)
			}
			if l := got[k].lock; l != nil && l.Src == "isb_5m" {
				t.Errorf("%s tick: an isb_5m break lock stands inside the mentor's one no-trade box: %+v", k, *l)
			}
		}
	}
	if b := got["09:14"].box; b != nil {
		t.Fatalf("09:14 tick: the 09:10 5m candle closed 28,769.50 above the merged box — it must be deleted (OctD4.3-R67), got %s", boxStr(b))
	}
}

// TestISB5F5LateFormationSeesTheBreak — ISB5-F5 on 2026-10-14 (MNQ 12-26 1m
// from data.db). The defect is in the shared build step, so it is pinned in the
// mode the replay found it in (mentor_5m_box_source=newest, stored through the
// production resolver): the standing box is deleted by the same 5m close that
// makes the 08:35 candle an inside bar (mother 08:30), so the 08:35 box
// (30,498–30,586.75) must be built on THAT tick (the 08:39 bar), and the 08:40
// 1m close 30,610.75 above it is the break (OctD4.3-R66 [Oct D4.3 @ 1:05:00]):
// the box breaks long on the 08:40 bar, not one minute later.
//
// The default (merge) gets the same break at 08:40 from the merged box
// (30,498–30,597.75, the 08:20 inside bar merged through 08:35).
func TestISB5F5LateFormationSeesTheBreak(t *testing.T) {
	newest := &store.StrategyConfig{}
	newest.RiskControl.MentorTuning = &store.MentorTuning{Mentor5mBoxSource: mentor.FiveMinBoxNewest}
	got := replayFixtureDay(t, "mnq_12-26_1m_20261013-20261014.csv.gz", "2026-10-14", "08:45", newest)
	at0835 := time.Date(2026, 10, 14, 8, 35, 0, 0, kernel.CTLocation()).UnixMilli()
	if b := got["08:39"].box; b == nil || b.AtTime != at0835 || b.Low != 30_498 || b.High != 30_586.75 {
		t.Fatalf("08:39 tick: the 08:35 inside candle closed — its box 30,498–30,586.75 must stand now (OctD4.3-R58), got %s", boxStr(b))
	}
	for _, mode := range []string{"newest", "default merge"} {
		if mode != "newest" {
			got = replayFixtureDay(t, "mnq_12-26_1m_20261013-20261014.csv.gz", "2026-10-14", "08:45", nil)
		}
		// The 08:40 tick also breaks the 30m box (the last break event wins
		// the lock), so the 5m break is read off the box itself: escaped long
		// on the 08:40 close, standing (not escaped) at 08:39.
		if b := got["08:39"].box; b == nil || b.Escaped != "" {
			t.Fatalf("%s: 08:39 tick: want the box standing unbroken, got %s", mode, boxStr(b))
		}
		if b := got["08:40"].box; b == nil || b.Escaped != mentor.SideLong {
			t.Fatalf("%s: 08:40 tick: the 1m close 30,610.75 is above the box — it must break long at 08:40, got %s (lock %+v)", mode, boxStr(b), got["08:40"].lock)
		}
	}
}

// TestISB5F6MotherAcrossTheHalt — ISB5-F6 (tool output only): a 5m pair across
// the 16:00–17:00 CT halt pairs the 17:00 inside candle with the 15:55 bucket
// (production closedBuckets has no 16:55 bucket). The event's mother_at and
// mother_ohlc must name that real previous bucket, not inside_at − 5 minutes.
// Real case: 2026-10-16.jsonl, bar 2026-10-15 17:04, mother 15:55
// [30,133.5, 30,135.5, 30,036.5, 30,077.75].
func TestISB5F6MotherAcrossTheHalt(t *testing.T) {
	loc := kernel.CTLocation()
	at := func(h, m int) int64 { return time.Date(2026, 10, 15, h, m, 0, 0, loc).UnixMilli() }
	var bars []market.Kline
	add := func(h, m int, o, hi, lo, c float64) {
		ms := at(h, m)
		bars = append(bars, market.Kline{OpenTime: ms, CloseTime: ms + 59_999, Open: o, High: hi, Low: lo, Close: c, Final: true})
	}
	for i := 0; i < 5; i++ { // the 15:50 bucket
		add(15, 50+i, 30_120, 30_125, 30_115, 30_120)
	}
	add(15, 55, 30_133.5, 30_135.5, 30_120, 30_125) // the 15:55 bucket (the real mother's OHLC)
	add(15, 56, 30_125, 30_130, 30_090, 30_100)
	add(15, 57, 30_100, 30_110, 30_036.5, 30_050)
	add(15, 58, 30_050, 30_080, 30_040, 30_070)
	add(15, 59, 30_070, 30_090, 30_060, 30_077.75)
	for i := 0; i < 5; i++ { // the 17:00 bucket, inside the 15:55 one
		add(17, i, 30_080, 30_100, 30_070, 30_090)
	}
	box := &mentor.ISBBox{High: 30_100, Low: 30_070, Dir: mentor.SideShort, AtTime: at(17, 0)}
	ev := boxFields("", box, 5, bars)
	if got := ev["mother_at"]; got != "2026-10-15 15:55" {
		t.Errorf("mother_at: want the real previous bucket 2026-10-15 15:55, got %v", got)
	}
	want := []float64{30_133.5, 30_135.5, 30_036.5, 30_077.75}
	if got, _ := ev["mother_ohlc"].([]float64); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("mother_ohlc: want the 15:55 bucket %v, got %v", want, ev["mother_ohlc"])
	}
	if got := ev["inside_at"]; got != "2026-10-15 17:00" {
		t.Errorf("inside_at: want 2026-10-15 17:00, got %v", got)
	}
}
