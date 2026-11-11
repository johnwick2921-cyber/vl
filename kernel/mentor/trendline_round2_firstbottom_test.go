package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// round-2 D3-G9 — a trendline anchors on the FIRST bottom of the move and joins
// the next higher bottoms to it, not only consecutive swings. "Hai cái đáy, rồi
// giá quay lại cái đường" [Oct D3.2 @ 41:20] (OctD3.2-R59); "một đáy thấp rồi một
// đáy cao hơn" [@ 01:00] (R3); "nối 2 cái đáy lại rồi kéo dài ra" [@ 33:00] (R46);
// the first anchor is the FIRST bottom of the move [@ 31:20] (R45). EX7/EX10 Sun
// 1 Nov: the line 17:07 28,595.00 -> 17:29 28,612.00 (touches 18:32, 18:40, ~20:00;
// frames s_02462/s_02745/s_02911). 17:29 (28,612.00) is LOWER than the swing lows
// between (17:17 28,626.75, 17:25 28,643.25), so the adjacent pair cannot form it
// and the single non-adjacent slot stayed on 17:07 -> 17:17. Real stored bars:
// testdata/r2_mnq_1m_20261101_1700_to_20261102_0000ct.csv.

func round2SunTick(t *testing.T, cfg Config, until time.Time) *Evaluator {
	t.Helper()
	bars := m413Load(t, "testdata/r2_mnq_1m_20261101_1700_to_20261102_0000ct.csv")
	e := New(cfg)
	for i := range bars {
		if bars[i].CloseTime+1 > until.UnixMilli() {
			break
		}
		e.Tick(append([]market.Kline(nil), bars[:i+1]...), bars[i].CloseTime+1)
	}
	return e
}

func round2HasLine(e *Evaluator, p0, p1 float64) bool {
	for _, tl := range e.State.ChartTrendlines {
		if tl.TFMin == 0 && tl.P0Px == p0 && tl.P1Px == p1 {
			return true
		}
	}
	return false
}

func TestRound2D3G9FirstBottomLineToTheLowerSecondBottom(t *testing.T) {
	withFlatNear(t)
	e := round2SunTick(t, m473Cfg(), time.Date(2026, 11, 1, 17, 33, 0, 0, ctime()))
	if !round2HasLine(e, 28595.00, 28612.00) {
		t.Fatalf("no line 17:07 28,595.00 -> 17:29 28,612.00 at 17:33; lines: %+v", e.State.ChartTrendlines)
	}
}

func TestRound2D3G9KnobOffKeepsConsecutiveAndOneNonAdjacent(t *testing.T) {
	withFlatNear(t)
	cfg := m473Cfg()
	cfg.TrendlineFirstBottomAnchor = false
	e := round2SunTick(t, cfg, time.Date(2026, 11, 1, 17, 33, 0, 0, ctime()))
	if round2HasLine(e, 28595.00, 28612.00) {
		t.Fatal("knob off: the 17:07 -> 17:29 line must not exist (today's behaviour)")
	}
}

// round-2 D3-G10 / D3-G14 — knobs that ship OFF because the course states the
// rule but no number (uncited numbers -> knob OFF). Real Sun 1 Nov bars.
//
// D3-G10: "a low after which price only drifts sideways is rejected" [Oct D3.2 @
// 03:13] (R9); "a bottom counts only if price bounced up a lot" [@ 24:00] (R35);
// not clear -> do not draw [@ 24:34] (R36). The 17:25 low 28,643.25 bounced 16 pts,
// drifted three bars and dumped to 28,621: with trendline_min_bounce_pts = 20 no
// line is drawn from it; at 0 (default) today's line 17:17 -> 17:25 stands.
// D3-G14: "Góc 30-45 độ" [@ 29:12] (R40), not 80-85, "cầu tuột" [@ 29:23-29:38]
// (R41): the 17:17 -> 17:25 line climbs 2.06 pts per bar; a 1.5 cap refuses it.
func TestRound2D3G10BounceKnobRefusesTheDriftLow(t *testing.T) {
	withFlatNear(t)
	until := time.Date(2026, 11, 1, 17, 27, 0, 0, ctime())
	if !round2HasLine(round2SunTick(t, m473Cfg(), until), 28626.75, 28643.25) {
		t.Fatal("default (OFF): today's 17:17 -> 17:25 line must stand")
	}
	cfg := m473Cfg()
	cfg.TrendlineMinBouncePts = 20
	if round2HasLine(round2SunTick(t, cfg, until), 28626.75, 28643.25) {
		t.Fatal("trendline_min_bounce_pts=20: the 16-pt-bounce 17:25 low must anchor no line")
	}
}

func TestRound2D3G14SlopeCapRefusesTheSteepLine(t *testing.T) {
	withFlatNear(t)
	until := time.Date(2026, 11, 1, 17, 27, 0, 0, ctime())
	cfg := m473Cfg()
	cfg.TrendlineMaxSlopePtsPerBar = 1.5
	if round2HasLine(round2SunTick(t, cfg, until), 28626.75, 28643.25) {
		t.Fatal("trendline_max_slope_pts_per_bar=1.5: the 2.06 pts/bar line must be refused")
	}
}

func TestRound2D3G10G14DefaultsAreOff(t *testing.T) {
	c := DefaultConfig()
	if c.TrendlineMinBouncePts != 0 || c.TrendlineMaxSlopePtsPerBar != 0 {
		t.Fatalf("uncited numbers ship OFF: bounce=%v slope=%v", c.TrendlineMinBouncePts, c.TrendlineMaxSlopePtsPerBar)
	}
}
