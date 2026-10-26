package mentor

// r22 LEVELS lane — replay pins at the PRODUCTION evaluator call site
// (Evaluator.Tick over the live 1500-bar window, one tick per closed 1m bar, the
// default config + Enabled + RealFillOnly = the live hoang trader's config) on
// the RECORDED tapes of trading days 10-13 and 10-12 (MNQ 12-26, data.db
// read-only via testdata/extract_r22_levels_fixture.py). Canon 53: the inputs
// are the bot's own bars; nothing here re-builds an evaluator input by hand.
//
// Expected old-extreme values come from an INDEPENDENT oracle (Python, its own
// stitch + 5m/15m fractal pivots + the course reading "nearest unbroken old
// high above / old low below"), the same oracle that measured the 18/390
// single-pair figure in review-1014/MATH-LEVELS-LINES.md §3.

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"

	"vl/market"
)

const (
	r22Open1013    = int64(1791842400000) // 2026-10-12 17:00 CT — trading day 10-13 opens
	r22Open1012    = int64(1791756000000) // 2026-10-11 17:00 CT — trading day 10-12 opens
	r22RTHOpen1013 = int64(1791898200000) // 2026-10-13 08:30 CT
	r22RTHEnd1013  = int64(1791921600000) // 2026-10-13 15:00 CT
)

func r22Cfg() Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RealFillOnly = true
	// B52 (release #29) ships ON in production and changes the box set on the
	// recorded days (post-escape boxes cascade). These r22 tests pin the
	// LEVEL fix against the pre-B52 box world — the knob OFF reproduces
	// exactly today's behaviour, so the r22 placement expectations hold.
	cfg.BoxNewZoneAfterEscape = false
	// The recorded 10-12/10-13 days ran the 1m read (exec_2m defaulted OFF
	// then) — pin it so the r22 level expectations hold.
	cfg.Exec2mAfter30m = false
	// EmaMaxCross30m now defaults OFF (#45 — the mentor gives no count); the
	// r22 refusal-counter test must still see the E4 gate fire on the recorded
	// days, so pin it ON explicitly.
	cfg.EmaMaxCross30m = 2
	return cfg
}

// r22Replay ticks a fresh production evaluator over one recorded session with
// the live 1500-bar sliding window and hands every tick to each.
func r22Replay(t *testing.T, fixture string, open int64, cfg Config, each func(e *Evaluator, win []market.Kline, out []Intent)) *Evaluator {
	t.Helper()
	bars := loadFixture(t, fixture, "1m")
	e := New(cfg)
	ticks := 0
	for i := range bars {
		if bars[i].OpenTime < open {
			continue
		}
		lo := i + 1 - 1500
		if lo < 0 {
			t.Fatalf("fixture %s: only %d bars before the first tick, need 1500", fixture, i)
		}
		win := bars[lo : i+1]
		out := e.Tick(win, BarCloseInstant(bars[i]))
		ticks++
		if each != nil {
			each(e, win, out)
		}
	}
	if ticks < 1300 {
		t.Fatalf("fixture %s: %d session ticks, want a full session", fixture, ticks)
	}
	return e
}

func r22OldExtremePrices(levels []Level) []float64 {
	var ps []float64
	for _, l := range levels {
		// m428 R2: a flat support/resistance (two same-role swings at the same price) is
		// an EXTRA old low/high level beside the nearest pair — this pin counts the pivot pair.
		if l.Kind == KindOldExtreme && !strings.Contains(l.Key, "flat_") {
			ps = append(ps, l.Price)
		}
	}
	sort.Float64s(ps)
	return ps
}

// TestR22OldExtremesAreTheNearestUnbrokenPair1013 — r22 P1. The course has ONE
// nearest old high above and ONE nearest old low below [§6 ladder D3.3 p1
// @04:11; §2.1 rule 2 D4.1 p1 @08:05 written]. On every tick of 10-13 the
// evaluator's level set carries at most one old high and one old low, and at the
// oracle's snapshots it is exactly the oracle's pair. Before the fix it was the
// 12 raw 5m/15m pivots (08:30: 8 of them already traded through).
func TestR22OldExtremesAreTheNearestUnbrokenPair1013(t *testing.T) {
	// bar OpenTime (the last bar closed by the snapshot instant) → the oracle's
	// nearest unbroken pair (sorted; a side with no unbroken pivot is absent).
	want := map[int64][]float64{
		1791874740000: {30644.25, 30702.00}, // 02:00 CT
		1791885540000: {30734.75, 30774.25}, // 05:00 CT
		// N7 (release #23): the unbroken test runs over the WHOLE lookback, then
		// the nearest is picked. At 08:30 the 3 newest pivot lows had all traded
		// through, but an OLDER one (30613.25) had not; at 09:00 the same for the
		// highs (30905.50). The #516 values {30688.00} / {30591.25} were the
		// 3-per-side cap's. Independent oracle (stdlib Python, the same fractal
		// k=2 + 0.25×ATR14 filter + 144/96-bar lookback, uncapped vs capped):
		// ~/deck-fix-1005/rel22/fixwave/mentor-seed-state/py/oldext.py — capped3
		// reproduces every #516 value, uncapped gives these.
		1791898140000: {30613.25, 30688.00}, // 08:30 CT
		1791899940000: {30591.25, 30905.50}, // 09:00 CT
		1791903540000: {30591.25, 30792.75}, // 10:00 CT
		1791910740000: {30635.25, 30809.50}, // 12:00 CT
		1791917940000: {30710.75, 30780.75}, // 14:00 CT
		1791925140000: {30760.50, 30804.00}, // 16:00 CT
	}
	seen := 0
	r22Replay(t, "mnq_1m_2026-10-13_session", r22Open1013, r22Cfg(), func(e *Evaluator, win []market.Kline, _ []Intent) {
		got := r22OldExtremePrices(e.State.Levels)
		if len(got) > 2 {
			t.Fatalf("%d: %d old extremes %v — the course has ONE nearest old high and ONE old low", win[len(win)-1].OpenTime, len(got), got)
		}
		if w, ok := want[win[len(win)-1].OpenTime]; ok {
			seen++
			if fmt.Sprint(got) != fmt.Sprint(w) {
				t.Fatalf("bar %d: old extremes %v, want the oracle's nearest unbroken pair %v", win[len(win)-1].OpenTime, got, w)
			}
		}
	})
	if seen != len(want) {
		t.Fatalf("checked %d snapshots, want %d", seen, len(want))
	}
}

// TestR22OldExtremeZoneShareIsTheSinglePair1013 — r22 P1. The ISB "at an old
// high/low → size 3" zone (touchesOldExtreme, the isbFlagsFor read) on the 390
// RTH candles of 10-13 is the single-pair figure 20/390 (5.1%) — the oracle's
// number (18/390 under the 3-per-side cap N7 removed). Before the fix: 183/390 (47%).
func TestR22OldExtremeZoneShareIsTheSinglePair1013(t *testing.T) {
	n, zone := 0, 0
	r22Replay(t, "mnq_1m_2026-10-13_session", r22Open1013, r22Cfg(), func(e *Evaluator, win []market.Kline, _ []Intent) {
		cur := win[len(win)-1]
		if cur.OpenTime < r22RTHOpen1013 || cur.OpenTime >= r22RTHEnd1013 {
			return
		}
		n++
		if touchesOldExtreme(cur, e.State.Levels, 2) {
			zone++
		}
	})
	// N7 (release #23): 20, not #516's 18 — the uncapped nearest unbroken pair
	// (oldext.py: "RTH zone uncapped 20 of 390"); 183 = the old 12-pivot list.
	if n != 390 || zone != 20 {
		t.Fatalf("ISB old-extreme zone = %d of %d RTH candles, want 20 of 390 (the nearest unbroken pair over the whole lookback; 18 = the 3-per-side cap; 183 = the 12-pivot list)", zone, n)
	}
}

// TestR22PHLTargetExtremeHasTheTradeRole — r22 P1-4. For every new reject touch
// on 10-13 and 10-12 the PHL/PLH target pick (nearestOldExtremeOnSide over
// oldExtremeIndexes — the eval.go call sites) is an old HIGH for a long and an
// old LOW for a short, and the bar it indexes really made that extreme (its
// HIGH, resp. LOW, is the level price). Before the fix the role came from the
// LAST bar whose high OR low sat at the price, so a revisit relabelled an old
// high a "low" (42 wrong-role picks on 10-13).
func TestR22PHLTargetExtremeHasTheTradeRole(t *testing.T) {
	for _, day := range []struct {
		fixture string
		open    int64
	}{{"mnq_1m_2026-10-13_session", r22Open1013}, {"mnq_1m_2026-10-12_session", r22Open1012}} {
		picks, wrong := 0, 0
		r22Replay(t, day.fixture, day.open, r22Cfg(), func(e *Evaluator, win []market.Kline, _ []Intent) {
			cur := win[len(win)-1]
			oex := oldExtremeIndexes(e.State.Levels, win)
			for _, tr := range e.State.Touches {
				if tr.Outcome != TouchReject || tr.RefBar.OpenTime != cur.OpenTime {
					continue
				}
				side, _, ok := RejectEntry(tr, e.Cfg)
				if !ok {
					continue
				}
				tref := tr.RefBar.High
				if side == SideShort {
					tref = tr.RefBar.Low
				}
				ex, found := nearestOldExtremeOnSide(oex, tref, side)
				if !found {
					continue
				}
				picks++
				b := win[ex.idx]
				madeIt := side == SideLong && abs(b.High-ex.level.Price) < 0.26 || side == SideShort && abs(b.Low-ex.level.Price) < 0.26
				if ex.isHigh != (side == SideLong) || !madeIt {
					wrong++
				}
			}
		})
		if picks == 0 {
			t.Fatalf("%s: no PHL target picks — the replay never reached the pick", day.fixture)
		}
		if wrong != 0 {
			t.Fatalf("%s: %d of %d PHL/PLH target picks had the wrong role (a long must target an old HIGH, a short an old LOW)", day.fixture, wrong, picks)
		}
	}
}

// TestR22VisitCapPerLevelIdentity1013 — r22 P1-2. On 10-13 the B23 visit cap
// never refuses an EMA 34 line (the course gives the EMA its own repeat rules —
// D4.2 p1 @20:49–24:29, D4.2 p2 @01:23 — not knock knock), and it refuses the
// trigger line ONLY when the line AT ITS CURRENT PRICE has had more than
// LevelMaxVisits reject visits today (each reversal draws a new line, D3.4 p1
// @11:48–13:33). The trigger line and EMA 34 are separate levels. Before the
// fix: 82 of 88 trigger-retest and 59 of 65 EMA 34 rejects refused, because
// each moving line was ONE constant key all day.
func TestR22VisitCapPerLevelIdentity1013(t *testing.T) {
	cfg := r22Cfg()
	trigVisits := map[float64]int{} // the test's own count: reject visits per trigger price
	day := ""
	var badEMA, badTrig []string
	prevOutcome := map[string]TouchOutcome{}
	prevCap := map[string]bool{}
	r22Replay(t, "mnq_1m_2026-10-13_session", r22Open1013, cfg, func(e *Evaluator, win []market.Kline, _ []Intent) {
		cur := win[len(win)-1]
		if e.State.Day.Key != day {
			day = e.State.Day.Key
			trigVisits = map[float64]int{}
		}
		key := string(KindTriggerRetest)
		if tr := e.State.Touches[key]; tr.Outcome == TouchReject && prevOutcome[key] == TouchNone {
			trigVisits[e.State.Trigger.Price]++
		}
		for k, tr := range e.State.Touches {
			prevOutcome[k] = tr.Outcome
		}
		for k := range e.State.VisitCapRefused {
			if prevCap[k] {
				continue // the once-per-visit mark of a refusal already judged
			}
			switch k {
			case string(KindEMA34), string(KindEMA34HTF):
				badEMA = append(badEMA, fmt.Sprintf("%d %s", cur.OpenTime, k))
			case key:
				if trigVisits[e.State.Trigger.Price] <= cfg.LevelMaxVisits {
					badTrig = append(badTrig, fmt.Sprintf("%d line@%.2f visits %d", cur.OpenTime, e.State.Trigger.Price, trigVisits[e.State.Trigger.Price]))
				}
			}
		}
		prevCap = map[string]bool{}
		for k := range e.State.VisitCapRefused {
			prevCap[k] = true
		}
	})
	if len(badEMA) > 0 {
		t.Fatalf("the visit cap refused an EMA 34 line %d time(s) (first %s) — knock knock is one spot; the EMA has its own crossing/loss rules", len(badEMA), badEMA[0])
	}
	if len(badTrig) > 0 {
		t.Fatalf("the visit cap refused the trigger line %d time(s) at a price visited <= %d times (first: %s) — a moved line is a new level", len(badTrig), cfg.LevelMaxVisits, badTrig[0])
	}
}

// TestR22VisitCapSeparatesTriggerLinePositions — r22 P1-2, the unit pin. Four
// reject visits of the trigger line at 100: the 4th is refused (cap 3). The line
// then reverses and is redrawn at 90: its first reject visit is a NEW level and
// is not refused.
func TestR22VisitCapSeparatesTriggerLinePositions(t *testing.T) {
	e := triggerRetestFixture()
	// Freeze the 5m trigger line (no bucket ever commits or breaks): the test
	// draws the line by hand — at 100, then the reversal redraws it at 90.
	e.State.Trigger.LastBucket = math.MaxInt64
	bars := []market.Kline{rthBars(0, 101, 101.5, 100.9, 101.2)}
	i := 1
	visit := func(line float64) {
		// touch the line and close back above (a reject at support) …
		bars = append(bars, rthBars(i, line+1, line+1.2, line, line+0.5))
		e.Tick(bars, bars[len(bars)-1].CloseTime+1)
		i++
		// … then a candle clear above the line ends the visit (departure).
		bars = append(bars, rthBars(i, line+6, line+7, line+5, line+6))
		e.Tick(bars, bars[len(bars)-1].CloseTime+1)
		i++
	}
	for v := 0; v < 4; v++ {
		visit(100)
	}
	if got := e.State.Refusals["level_visit_cap"]; got != 1 {
		t.Fatalf("4 visits of the line at 100: level_visit_cap = %d, want 1 (the 4th visit)", got)
	}
	e.State.Trigger.Price = 90 // the line reverses and is redrawn at 90
	visit(90)
	if got := e.State.Refusals["level_visit_cap"]; got != 1 {
		t.Fatalf("first visit of the line redrawn at 90: level_visit_cap = %d, want still 1 — a moved trigger line is a new level", got)
	}
}

// TestR22PHLPlacesAtTheTriggerLine1012 — r22 P1-3. PHL/PLH is alive: on the
// trading day 10-12 replay the evaluator places PHL/PLH entries, including at
// the 5m trigger-line retest (the backtest entry he takes live, D3.4 p2
// @20:51–21:59). Before the fix the trigger retest was one capped key all day
// and the day placed a single PHL (none at the trigger line).
//
// 10-13 itself (the day the audit named) places none even after the fix — every
// first-touch reject there fails a COURSE gate (5m/15m/30m ISB box, mid-range
// boxes, EMA crossing, no room to the old extreme, HTF); the per-visit funnel is
// in the r22 PR body.
func TestR22PHLPlacesAtTheTriggerLine1012(t *testing.T) {
	total, atTrigger := 0, 0
	e := r22Replay(t, "mnq_1m_2026-10-12_session", r22Open1012, r22Cfg(), func(_ *Evaluator, _ []market.Kline, out []Intent) {
		for _, in := range out {
			if in.Action != PlaceStopEntry || in.Setup != "PHL" && in.Setup != "PLH" {
				continue
			}
			total++
			if in.AnchorKey == string(KindTriggerRetest) {
				atTrigger++
			}
		}
	})
	// m427: with the owner's whole-candle break read over the candle's whole life
	// a sloped trendline that the close-time read killed at 23:57 now stays alive
	// and is the nearer target, so the one trigger-retest PHL of that day (31256)
	// is refused by the target floor (phl_target_below_floor) instead of placed.
	// The retest is still evaluated — pinned EXACTLY (CTO, #50): no trigger-line
	// placement and at least one floor refusal, so any later shift is seen.
	if total < 2 || atTrigger != 0 || e.State.Refusals["phl_target_below_floor"] == 0 {
		t.Fatalf("10-12 replay placed %d PHL/PLH (%d at the trigger-line retest, %d floor-refused), want >= 2, 0 at the trigger line, >= 1 floor-refused (m427 whole-life break)", total, atTrigger, e.State.Refusals["phl_target_below_floor"])
	}
}

// TestR22PHLPlacesOn1013 — r22 P1-3 on the audit's own day. With the level
// fixes (and the release #22 trendline wave, #507, merged in) the trading day
// 10-13 replay places PHL/PLH entries; the live bot placed none.
func TestR22PHLPlacesOn1013(t *testing.T) {
	n := 0
	r22Replay(t, "mnq_1m_2026-10-13_session", r22Open1013, r22Cfg(), func(_ *Evaluator, _ []market.Kline, out []Intent) {
		for _, in := range out {
			if in.Action == PlaceStopEntry && (in.Setup == "PHL" || in.Setup == "PLH") {
				n++
			}
		}
	})
	if n == 0 {
		t.Fatal("10-13 replay placed no PHL/PLH")
	}
}

// TestR22SilentPHLDropsAreCounted1013 — r22 (P2-1). Every PHL/PLH refusal is
// named and counted: the 5m ISB-box ban (kept — the course bans PHL/PLH inside
// it, D3.4 p2 @07:27–10:35), the mid-range boxes, the EMA crossing gate and the
// location gate used to drop SILENTLY. Each counts once per visit of the level.
// The two recorded days (10-13, 10-12) between them exercise every one of them.
func TestR22SilentPHLDropsAreCounted1013(t *testing.T) {
	seen := map[string]int{}
	for _, day := range []struct {
		fixture string
		open    int64
	}{{"mnq_1m_2026-10-13_session", r22Open1013}, {"mnq_1m_2026-10-12_session", r22Open1012}} {
		visits := 0
		prev := map[string]TouchOutcome{}
		e := r22Replay(t, day.fixture, day.open, r22Cfg(), func(e *Evaluator, _ []market.Kline, _ []Intent) {
			for k, tr := range e.State.Touches {
				if tr.Outcome == TouchReject && prev[k] == TouchNone {
					visits++
				}
				prev[k] = tr.Outcome
			}
		})
		for _, reason := range []string{"phl_5m_box_ban", "phl_mid_range_boxed", "phl_ema_crossing_30m", "phl_not_location"} {
			got := e.State.Refusals[reason]
			if got > visits {
				t.Fatalf("%s: refusal %q = %d > %d reject visits — it must count once per visit, not per tick", day.fixture, reason, got, visits)
			}
			seen[reason] += got
		}
	}
	for _, reason := range []string{"phl_5m_box_ban", "phl_mid_range_boxed", "phl_ema_crossing_30m", "phl_not_location"} {
		if seen[reason] == 0 {
			t.Fatalf("refusal %q never counted on 10-13 or 10-12 — the drop is silent (%v)", reason, seen)
		}
	}
}

// TestR22ChartDrawsTheTradingOldExtremes1013 — r22 P1 "one source". On every
// tick of 10-13 the DRAW set (DisplayLevels over the tick's ChartLevels — what
// the mentor-truth panel and the dashboard chart draw) carries exactly the old
// extremes the trading code reads.
// Before the fix the chart folded/pruned the 12 pivots (5 drawn at 08:30).
func TestR22ChartDrawsTheTradingOldExtremes1013(t *testing.T) {
	cfg := r22Cfg()
	r22Replay(t, "mnq_1m_2026-10-13_session", r22Open1013, cfg, func(e *Evaluator, win []market.Kline, _ []Intent) {
		trading := r22OldExtremePrices(e.State.Levels)
		// The panel/chart draws DisplayLevels over the tick's chart set
		// (trader/mentor_truth_panel.go: DisplayLevels(st.ChartLevels, …)).
		drawn := r22OldExtremePrices(DisplayLevels(e.State.ChartLevels, cfg.KeyLevelPrunePts))
		if fmt.Sprint(trading) != fmt.Sprint(drawn) {
			t.Fatalf("bar %d: chart draws old extremes %v, trading reads %v — one source", win[len(win)-1].OpenTime, drawn, trading)
		}
	})
}

// TestR22SeedLineCountsLiveKeyLevels — r22 (P3 "92 levels"). The boot line
// reads the seeded key levels split into LIVE and DELETED (a closed 1H body
// through it) — never the total that includes deleted levels.
func TestR22SeedLineCountsLiveKeyLevels(t *testing.T) {
	bars := loadFixture(t, "mnq_1m_2026-10-13_session", "1m")
	e := New(r22Cfg())
	Seed(e, bars, BarCloseInstant(bars[len(bars)-1]))
	live, deleted := 0, 0
	for _, l := range e.State.SeedLevels {
		if e.State.DeletedLevels[l.Key] {
			deleted++
		} else {
			live++
		}
	}
	if deleted == 0 {
		t.Fatalf("fixture: want at least one body-deleted key level (live %d)", live)
	}
	line := e.SeedLine(0)
	want := fmt.Sprintf("key levels %d live · %d deleted", live, deleted)
	if !strings.Contains(line, want) {
		t.Fatalf("seed line %q missing %q", line, want)
	}
}

// TestR22NearestOldExtremeOnSideSkipsTheWrongRole — r22 P1-4, the unit pin at
// the PHL target pick (eval.go nearestOldExtremeOnSide). An old LOW nearer than
// the old HIGH on the long side is never a long target; mirror for a short.
func TestR22NearestOldExtremeOnSideSkipsTheWrongRole(t *testing.T) {
	ex := []oldExtreme{
		{level: Level{Kind: KindOldExtreme, Price: 105}, idx: 3, isHigh: false},
		{level: Level{Kind: KindOldExtreme, Price: 110}, idx: 2, isHigh: true},
	}
	if got, ok := nearestOldExtremeOnSide(ex, 100, SideLong); !ok || got.level.Price != 110 {
		t.Fatalf("long from 100 picked %.2f (ok %v), want the old HIGH 110 — never the old LOW 105", got.level.Price, ok)
	}
	if got, ok := nearestOldExtremeOnSide(ex, 120, SideShort); !ok || got.level.Price != 105 {
		t.Fatalf("short from 120 picked %.2f (ok %v), want the old LOW 105 — never the old HIGH 110", got.level.Price, ok)
	}
}
