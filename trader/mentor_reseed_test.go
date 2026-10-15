package trader

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vl/calendar"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── release #23 MENTOR-SEED-STATE — the trader call sites, on the 10-14 tape ──
//
// The fixture is the kernel's (kernel/mentor/testdata/
// mnq_1m_seed_state_2026-10-14.json.gz, extracted read-only): MNQ 12-26 1m from
// 10-11 17:00 to 10-14 11:00 CT; refill=1 marks the 244 rows of the
// 03:44–07:47 hole the AddOn's replay released to the store at 08:23:00.

type seedStateBar struct {
	k      market.Kline
	refill bool
}

func loadSeedStateBars(t *testing.T) []seedStateBar {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "kernel", "mentor", "testdata", "mnq_1m_seed_state_2026-10-14.json.gz"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	var doc struct {
		Bars [][]float64 `json:"bars"`
	}
	if err := json.NewDecoder(gz).Decode(&doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := make([]seedStateBar, 0, len(doc.Bars))
	for _, r := range doc.Bars {
		ot := int64(r[0])
		out = append(out, seedStateBar{k: market.Kline{
			OpenTime: ot, Open: r[1], High: r[2], Low: r[3], Close: r[4], Volume: r[5],
			CloseTime: ot + 59_999, Final: true,
		}, refill: r[6] == 1})
	}
	return out
}

func seedStateRows(bars []seedStateBar, keep func(seedStateBar) bool) []store.BarHistoryDB {
	var rows []store.BarHistoryDB
	for _, b := range bars {
		if !keep(b) {
			continue
		}
		src := store.BarSourceLive
		if b.refill {
			src = store.BarSourceHistorical
		}
		rows = append(rows, store.BarHistoryDB{
			Symbol: "MNQ", TF: "1m", Contract: "MNQ 12-26", Source: src, OpenTimeMs: b.k.OpenTime,
			O: b.k.Open, H: b.k.High, L: b.k.Low, C: b.k.Close, V: b.k.Volume,
		})
	}
	return rows
}

func seedStateCT(h, m, s int) time.Time {
	return time.Date(2026, time.October, 14, h, m, s, 0, kernel.CTLocation())
}

// cpiEvents1014 is the stored 10-14 slice's T1 07:30 print (CPI m/m — the
// calendar_slices row for 2026-10-14 holds it at 12:30Z).
func cpiEvents1014() []calendar.Event {
	return []calendar.Event{{Time: seedStateCT(7, 30, 0), Title: "CPI m/m", Impact: calendar.T1}}
}

func seedStateTrader(t *testing.T) *AutoTrader {
	t.Helper()
	off := false
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{
		MentorMode: true, MentorWindowStart: "00:00", MentorWindowMinutes: 1440,
		// 10-14 historical pin: the live bot ran the item-18 print-window drop,
		// not the release #29 freeze (news_bars_excluded OFF).
		MentorTuning: &store.MentorTuning{NewsBarsExcluded: &off},
	}})
	wireMentorPlacementSeams(t, at)
	mentorDayEventsForTest = func() ([]calendar.Event, bool) { return cpiEvents1014(), true }
	mentorDayEventsOnForTest = func(date string) ([]calendar.Event, bool) {
		if date == "2026-10-14" {
			return cpiEvents1014(), true
		}
		return nil, true
	}
	t.Cleanup(func() { mentorDayEventsOnForTest = nil; setMentorNowSource(nil) })
	if err := store.NewBarHistoryStore(at.store.GormDB()).Migrate(); err != nil {
		t.Fatal(err)
	}
	return at
}

func seedStateLine(l mentor.TriggerLine) string {
	return fmt.Sprintf("%s@%.2f since %s", l.Dir, l.Price, kernel.TableTimeCT(time.UnixMilli(l.MovedAt)))
}

// TestMentorReseedsAfterTheHistoryRefill1014 — N1 (P1), the production call
// site. mentorSeedAtStart reads a store that lacks the 03:44–07:47 hole and
// ends at 08:15 — the live 08:22:40 boot — and seeds "1h short @30790.75". The
// replay release then puts the hole in the store and announces it
// (ntTrader.NoteHistoryRefill — the call replayHold.resolve makes); the next
// mentorEvalOnce re-seeds from the store BEFORE it ticks, and the evaluator is
// then exactly a bot that never had the hole: 1h short @30716.00 since 07:00.
// The re-seed is counted and logged with its before → after.
// Mutant: drop the mentorReseedOnRefill call from mentorEvalOnce → RED.
func TestMentorReseedsAfterTheHistoryRefill1014(t *testing.T) {
	bars := loadSeedStateBars(t)
	at := seedStateTrader(t)
	bh := store.NewBarHistoryStore(at.store.GormDB())
	storeLast := seedStateCT(8, 15, 0).UnixMilli()
	if err := bh.InsertBars(seedStateRows(bars, func(b seedStateBar) bool { return !b.refill && b.k.OpenTime <= storeLast })); err != nil {
		t.Fatal(err)
	}
	seedAt := seedStateCT(8, 22, 40)
	setMentorNowSource(func() time.Time { return seedAt })
	at.mentorSeedAtStart()
	if got := seedStateLine(at.mentorEval.State.HTF.OneH); got != "short@30790.75 since 10-14 07:00" {
		t.Fatalf("control: the holed store must seed the live boot's 1h line, got %s", got)
	}

	// 08:23:00 — the replay is released to the store (+ the live rows the
	// persister wrote since 08:15), and announced.
	tick := seedStateCT(8, 23, 0).UnixMilli()
	if err := bh.InsertBars(seedStateRows(bars, func(b seedStateBar) bool {
		return b.refill || b.k.OpenTime > storeLast && b.k.OpenTime <= tick
	})); err != nil {
		t.Fatal(err)
	}
	ntTrader.NoteHistoryRefill("MNQ", "1m", 244, "replay released 244 MNQ 1m row(s) to the store")

	// The first tick after the release: the 08:23 bar, on the live window.
	var full []market.Kline
	for _, b := range bars {
		if b.k.OpenTime <= tick {
			full = append(full, b.k)
		}
	}
	win := full[len(full)-mentorBars1mDepth:]
	setMentorNowSource(func() time.Time { return seedStateCT(8, 24, 0) })
	if !at.mentorEvalOnce(win) {
		t.Fatal("the tick did not run")
	}

	// Truth: a fresh evaluator seeded on the FULL tape at the same instant and
	// ticked on the same window (what a never-holed bot holds).
	now := mentor.BarCloseInstant(full[len(full)-1])
	truth := mentor.New(at.mentorEval.Cfg)
	truth.Cfg.PrintWindows = mentorNewsPrintWindowsFromEvents(cpiEvents1014())
	mentor.Seed(truth, full, now)
	truth.Tick(win, now)
	got := at.mentorEval.State
	// The store-read klines differ from the fixture's only in bookkeeping
	// fields (Final), so the lines compare by what trades: direction, price,
	// since, the committed bucket and its extremes.
	same := func(a, b mentor.TriggerLine) bool {
		return a.Dir == b.Dir && a.Price == b.Price && a.MovedAt == b.MovedAt && a.LastBucket == b.LastBucket &&
			a.LastBar.High == b.LastBar.High && a.LastBar.Low == b.LastBar.Low
	}
	if !same(got.HTF.OneH, truth.State.HTF.OneH) || !same(got.HTF.FourH, truth.State.HTF.FourH) || !same(got.Trigger, truth.State.Trigger) || got.EMA34 != truth.State.EMA34 {
		t.Fatalf("after the refill tick: 1h %s vs truth %s · 4h %s vs %s · 5m %s vs %s · EMA34 %.4f vs %.4f",
			seedStateLine(got.HTF.OneH), seedStateLine(truth.State.HTF.OneH), seedStateLine(got.HTF.FourH), seedStateLine(truth.State.HTF.FourH),
			seedStateLine(got.Trigger), seedStateLine(truth.State.Trigger), got.EMA34, truth.State.EMA34)
	}
	if l := seedStateLine(got.HTF.OneH); l != "short@30716.00 since 10-14 07:00" {
		t.Fatalf("the 1h line must be the never-stopped truth short@30716.00 since 10-14 07:00, got %s", l)
	}
	c := at.mentorCountSnapshot()
	if c["reseed_after_refill"] != 1 || c["reseed_changed"] != 1 {
		t.Fatalf("the re-seed must be counted once and as a change: %v", c)
	}
	// A second tick with no new refill does NOT re-seed.
	next := append(append([]market.Kline(nil), full...), bars[len(full)].k)
	setMentorNowSource(func() time.Time { return seedStateCT(8, 25, 0) })
	at.mentorEvalOnce(next[len(next)-mentorBars1mDepth:])
	if c := at.mentorCountSnapshot(); c["reseed_after_refill"] != 1 {
		t.Fatalf("no refill since → no re-seed: %v", c)
	}
}

// TestMentorSeedAtStartHonoursHistoryPrintWindows1014 — N6 (P2), the production
// call site. A restart at 08:30 on the CPI day must commit the 1h 07:00 bucket
// WITHOUT the 07:20–07:35 print candles — H 30817.00, what the running bot
// committed — not H 30860.00 (MATH-LEVELS-TIME N6, trig.py). mentorSeedAtStart
// hands SeedFull the windows of every replayed day from the stored calendar.
// Mutant: mentorHistoryPrintWindows returns nil → RED (30860.00).
func TestMentorSeedAtStartHonoursHistoryPrintWindows1014(t *testing.T) {
	bars := loadSeedStateBars(t)
	at := seedStateTrader(t)
	bh := store.NewBarHistoryStore(at.store.GormDB())
	seedAt := seedStateCT(8, 30, 0)
	if err := bh.InsertBars(seedStateRows(bars, func(b seedStateBar) bool { return b.k.OpenTime < seedAt.UnixMilli() })); err != nil {
		t.Fatal(err)
	}
	setMentorNowSource(func() time.Time { return seedAt })
	at.mentorSeedAtStart()
	oneH := at.mentorEval.State.HTF.OneH
	if oneH.LastBar.OpenTime != seedStateCT(7, 0, 0).UnixMilli() || oneH.LastBar.High != 30817.00 {
		t.Fatalf("the 08:30 restart committed the 1h %s bucket at H %.2f — want the 07:00 bucket at H 30817.00 (print candles excluded, as the running bot did)",
			kernel.TableTimeCT(time.UnixMilli(oneH.LastBar.OpenTime)), oneH.LastBar.High)
	}
	// The tick restores TODAY's windows (the union is a seed-only setting).
	if n := len(at.mentorEval.Cfg.PrintWindows); n != 0 {
		t.Fatalf("the seed must restore the evaluator's own windows after seeding, left %d", n)
	}
}

// TestMentorHistoryPrintWindowsUnionEveryDay — N6: every replayed day's stored
// slice contributes its own window (a running bot applied each day's windows
// on that day); a day with no slice contributes none.
func TestMentorHistoryPrintWindowsUnionEveryDay(t *testing.T) {
	at := &AutoTrader{}
	mentorDayEventsOnForTest = func(date string) ([]calendar.Event, bool) {
		switch date {
		case "2026-10-13":
			return []calendar.Event{{Time: time.Date(2026, 10, 13, 7, 30, 0, 0, kernel.CTLocation()), Title: "PPI m/m", Impact: calendar.T1}}, true
		case "2026-10-14":
			return cpiEvents1014(), true
		}
		return nil, false
	}
	t.Cleanup(func() { mentorDayEventsOnForTest = nil })
	var bars []market.Kline
	for _, d := range []int{12, 13, 14} {
		ot := time.Date(2026, 10, d, 7, 25, 0, 0, kernel.CTLocation()).UnixMilli()
		bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 59_999})
	}
	ws := at.mentorHistoryPrintWindows(bars)
	if len(ws) != 2 {
		t.Fatalf("want the 10-13 and 10-14 windows, got %+v", ws)
	}
	for i, d := range []int{13, 14} {
		from := time.Date(2026, 10, d, 7, 20, 0, 0, kernel.CTLocation()).UnixMilli()
		if ws[i].FromMs != from || ws[i].ToMs != from+15*60_000 {
			t.Fatalf("window %d = %+v, want 07:20–07:35 CT on 10-%d", i, ws[i], d)
		}
	}
}

// TestMentorTickWindowMatchesTheSeedReplay — the box-latch replay rebuilds each
// past minute's scan over mentor.SeedTickWindow1m bars; the live tick asks for
// mentorBars1mDepth. One number.
func TestMentorTickWindowMatchesTheSeedReplay(t *testing.T) {
	if mentorBars1mDepth != mentor.SeedTickWindow1m {
		t.Fatalf("mentorBars1mDepth %d ≠ mentor.SeedTickWindow1m %d", mentorBars1mDepth, mentor.SeedTickWindow1m)
	}
}

// TestPriorSessionStoreProviderServesTheWholeSession1014 — N4 (P2), the store
// side. At 10-13 08:30 the 2,000-bar ring holds 1,070 of the prior session's
// 1,378 bars; through the store provider the trader installs, pdVWAP is the
// WHOLE session's 30999.9259 (independent oracle pdvwap.py; the tail gives
// 30983.70, the rescan's −16.23). Mutant: the provider reads the 5m series →
// nothing whole → refused → RED.
func TestPriorSessionStoreProviderServesTheWholeSession1014(t *testing.T) {
	bars := loadSeedStateBars(t)
	at := seedStateTrader(t)
	bh := store.NewBarHistoryStore(at.store.GormDB())
	if err := bh.InsertBars(seedStateRows(bars, func(seedStateBar) bool { return true })); err != nil {
		t.Fatal(err)
	}
	prev := kernel.PriorSessionBarsProvider
	t.Cleanup(func() { kernel.PriorSessionBarsProvider = prev })
	kernel.PriorSessionBarsProvider = priorSessionBarsFromStore(bh)
	now := time.Date(2026, 10, 13, 8, 30, 0, 0, kernel.CTLocation())
	var ring []market.Kline
	for _, b := range bars {
		if b.k.CloseTime < now.UnixMilli() {
			ring = append(ring, b.k)
		}
	}
	ring = ring[len(ring)-2000:]
	var pd []float64
	for _, l := range kernel.VolumeLevelsFor("MNQ", ring, now) {
		if l.Kind == kernel.KindPDVWAP {
			pd = append(pd, l.Price)
		}
	}
	if len(pd) != 1 || pd[0] < 30999.9258 || pd[0] > 30999.9260 {
		t.Fatalf("pdVWAP via the store = %v, want the whole session's [30999.9259]", pd)
	}
}
