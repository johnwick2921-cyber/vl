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
// DefaultConfig + Enabled + RealFillOnly.
func lrProdCfg() Config {
	c := DefaultConfig()
	c.Enabled = true
	c.RealFillOnly = true
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
