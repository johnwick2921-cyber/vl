package trader

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

// ── release #30 B1 trader call-site pin ──────────────────────────────────────
//
// The kernel restart-parity test (kernel/mentor/day_seed_test.go) drives a test
// mirror (dsSeedAtStart). This drives the PRODUCTION call site: the trader's
// at.mentorSeedAtStart() over a store holding the 10-15 bars closed by the seed
// instant (mentor clock pinned), then the evaluator Tick the production
// mentorEvalOnce uses (Tick at BarCloseInstant, on the last mentorBars1mDepth
// bars — the same window the live tick passes). A restart at 11:32:56 and at
// 13:43:31 must seed the SAME Day verdict the continuous bot froze at 08:30
// (SPENT), hold it at every minute to 16:00, and render the boot line
// "RESTORED from the 08:30 read". The first tick must not recount the
// trendline pairs the seed already marked crossed (B3).

// loadDaySeed1015Bars loads the 10-13 09:00 → 10-15 23:35 CT MNQ 1m tape (the
// fixture the kernel test shares; extracted read-only from the align-1015
// replay data).
func loadDaySeed1015Bars(t *testing.T) []market.Kline {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "kernel", "mentor", "testdata", "mnq_1m_2026-10-15_dayseed.json.gz"))
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

// daySeed1015Trader builds the production AutoTrader over an in-memory store
// holding every fixture bar CLOSED by seedAt (CloseTime <= seedAt) — the same
// bar set closedOnly would keep, so mentorSeedAtStart reads exactly what the
// running bot had stored at that instant.
func daySeed1015Trader(t *testing.T, bars []market.Kline, seedAt int64) *AutoTrader {
	t.Helper()
	st := mentorSeedStore(t)
	bh := store.NewBarHistoryStore(st.GormDB())
	var rows []store.BarHistoryDB
	for _, b := range bars {
		if b.CloseTime > seedAt {
			break // bars are ascending; the rest are not closed yet
		}
		rows = append(rows, store.BarHistoryDB{
			Symbol: "MNQ", TF: "1m", Contract: "MNQ 12-26", Source: store.BarSourceLive,
			OpenTimeMs: b.OpenTime, O: b.Open, H: b.High, L: b.Low, C: b.Close, V: b.Volume,
		})
	}
	if len(rows) == 0 {
		t.Fatalf("no bars closed by the seed instant %d", seedAt)
	}
	if err := bh.InsertBars(rows); err != nil {
		t.Fatalf("InsertBars: %v", err)
	}
	at := &AutoTrader{
		id: "t-day-seed-1015",
		config: AutoTraderConfig{
			StrategyConfig: &store.StrategyConfig{
				RiskControl: store.RiskControlConfig{MentorMode: true},
			},
		},
		store: st,
	}
	wireMentorPlacementSeams(t, at)
	return at
}

// daySeed1015Win is the production tick window: the last mentorBars1mDepth
// bars ending at index i — the same window mentorEvalOnce passes.
func daySeed1015Win(bars []market.Kline, i int) []market.Kline {
	lo := i - mentorBars1mDepth + 1
	if lo < 0 {
		lo = 0
	}
	return bars[lo : i+1 : i+1]
}

func daySeed1015CT(ms int64) string {
	return time.UnixMilli(ms).In(chicagoLoc()).Format("01-02 15:04")
}

// TestMentorSeedRestoresDayLatch1015 is the trader call-site proof of release
// #30. MUTANT 1: comment out the two mentor.SeedDayLatch calls in
// trader/mentor_tick.go → the seed leaves State.Day empty and the first tick
// re-latches the BOOT's conflict → RED. MUTANT 2: comment out the two
// mentor.SeedTrendlines calls → the first tick recounts every crossed pair →
// RED.
func TestMentorSeedRestoresDayLatch1015(t *testing.T) {
	bars := loadDaySeed1015Bars(t)
	roll := int64(1792015200000) // 10-14 17:00 CT — the session the day gate reads
	end := int64(1792098000000)  // 10-15 16:00 CT
	open := int64(1792071000000) // 10-15 08:30 CT

	rollIdx := -1
	for i := range bars {
		if bars[i].OpenTime == roll {
			rollIdx = i
			break
		}
	}
	if rollIdx < 0 {
		t.Fatalf("the fixture has no 10-14 17:00 CT bar")
	}

	// The continuous bot: production seed at the roll, then tick every bar to
	// 16:00 on the production window.
	cont := daySeed1015Trader(t, bars, roll)
	setMentorNowSource(func() time.Time { return time.UnixMilli(roll) })
	cont.mentorSeedAtStart()
	liveDay := map[int64]mentor.DayVerdict{}
	contCrossedByBar := map[int64]int{}
	for i := rollIdx; i < len(bars); i++ {
		b := bars[i]
		if b.OpenTime > end {
			break
		}
		cont.mentorEval.Tick(daySeed1015Win(bars, i), mentor.BarCloseInstant(b))
		liveDay[b.OpenTime] = cont.mentorEval.State.Day.Verdict
		contCrossedByBar[b.OpenTime] = cont.mentorEval.State.Refusals["trendline_crossed_between_anchors"]
	}
	contFinalCrossed := cont.mentorEval.State.Refusals["trendline_crossed_between_anchors"]
	// The premise of the bug: the continuous 08:30 latch is SPENT.
	if v := liveDay[open]; v != mentor.DaySpent {
		t.Fatalf("the continuous 08:30 latch must be SPENT, got %v", v)
	}

	for _, rs := range []int64{1792081976000, 1792089811000} { // 11:32:56, 13:43:31 CT
		rsIdx := -1
		for i := range bars {
			if mentor.BarCloseInstant(bars[i]) >= rs {
				rsIdx = i
				break
			}
		}
		if rsIdx < 0 {
			t.Fatalf("restart instant %d has no bar", rs)
		}
		seedAt := mentor.BarCloseInstant(bars[rsIdx])
		re := daySeed1015Trader(t, bars, seedAt)
		setMentorNowSource(func() time.Time { return time.UnixMilli(seedAt) })
		re.mentorSeedAtStart()

		// The seed ALONE must carry the 08:30 verdict (no tick to hide a
		// re-latch), and the boot line must say it was RESTORED.
		if want := liveDay[bars[rsIdx].OpenTime]; re.mentorEval.State.Day.Verdict != want {
			t.Fatalf("restart @%s: seeded day verdict %v, continuous %v (key %q vs %q)",
				daySeed1015CT(bars[rsIdx].OpenTime), re.mentorEval.State.Day.Verdict, want,
				re.mentorEval.State.Day.Key, cont.mentorEval.State.Day.Key)
		}
		if rep := re.mentorEval.State.DayGateReport; !rep.Restored {
			t.Fatalf("restart @%s: the day-gate report must be Restored, got %+v",
				daySeed1015CT(bars[rsIdx].OpenTime), rep)
		} else if line := mentorDayGateLine(rep); !strings.Contains(line, "RESTORED from the 08:30 read") {
			t.Fatalf("the boot line must say RESTORED from the 08:30 read, got %q", line)
		}

		// Tick through 16:00 on the production window, asserting verdict
		// parity at every minute.
		for i := rsIdx + 1; i < len(bars); i++ {
			b := bars[i]
			if b.OpenTime > end {
				break
			}
			re.mentorEval.Tick(daySeed1015Win(bars, i), mentor.BarCloseInstant(b))
			if got, want := re.mentorEval.State.Day.Verdict, liveDay[b.OpenTime]; got != want {
				t.Fatalf("restart @%s, bar %s: day verdict %v, continuous %v",
					daySeed1015CT(bars[rsIdx].OpenTime), daySeed1015CT(b.OpenTime), got, want)
			}
		}
		// B3: the seed marked the pairs crossed before the restart, so the
		// ticks count ONLY the pairs that first cross after it — the
		// continuous bot's total minus what it had already counted by the
		// restart bar. A recount (SeedTrendlines dropped) makes the restart
		// re-count every pre-restart pair, so the number comes back as the
		// continuous TOTAL instead of the post-restart remainder.
		wantTrend := contFinalCrossed - contCrossedByBar[bars[rsIdx].OpenTime]
		if got := re.mentorEval.State.Refusals["trendline_crossed_between_anchors"]; got != wantTrend {
			t.Fatalf("restart @%s: trendline refusals %d, want %d (continuous %d total, %d by the restart bar)",
				daySeed1015CT(bars[rsIdx].OpenTime), got, wantTrend, contFinalCrossed, contCrossedByBar[bars[rsIdx].OpenTime])
		}
	}
}
