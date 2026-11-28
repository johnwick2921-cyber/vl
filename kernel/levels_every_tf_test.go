// W-TF — every detector, every timeframe. RED-first pins.
//
// The owner's standing rule is that every timeframe the store holds feeds every
// computation. What existed before this wave: DetectHTFLevels already looped
// per timeframe (G2/G3, 2026-08-24), but isHTFDetectionTF gated it to 15m…12h,
// so 1d/3d/1w could not reach the map at all — measured on 2026-09-10, 0 of 297
// stored plans carried a daily or weekly level.
//
// These tests pin the four things this wave changes and nothing else:
//
//	E1  a daily timeframe reaches the map, tagged tf=1d
//	E3  timeframe is part of a level's identity — same kind and price on two
//	    timeframes are TWO levels, not one dedupe casualty
//	E4  a timeframe with too few bars for the definition emits nothing AND says
//	    so — never a level from a partial window
//	C6  the tier a daily level is classified into, and the HTF flag it carries
//
// A28: this file owns ONE clock. Every bar timestamp and every `now` derives
// from tfTestNow() — a moment inside a regular session and outside every band,
// so no time predicate reads differently between two assertions.
//
// A24: no fixture retypes a production constant. Tier names, multipliers and
// the detection-gate membership are read from the code under test.
package kernel

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// tfTestNow is this file's single clock: 2026-09-10 13:30 CT, a Thursday inside
// the regular session. Bars are built backwards from it so every bar is closed.
func tfTestNow() time.Time {
	return time.Date(2026, 9, 10, 13, 30, 0, 0, CTLocation())
}

const (
	tfTestMinuteMs = int64(60 * 1000)
	tfTestDayMs    = 24 * 60 * tfTestMinuteMs
)

// barsWithTwoEqualPivotHighs builds n closed bars on a `stepMs` grid ending
// before `end`, shaped so that EqualHighsLows finds at least two strict pivot
// highs at the SAME price — the cheapest input that makes a detector fire on
// any timeframe. The shape is identical whatever the timeframe, which is the
// point: 12e says hold the family definition constant across timeframes.
func barsWithTwoEqualPivotHighs(n int, base float64, stepMs int64, end time.Time) []market.Kline {
	out := make([]market.Kline, 0, n)
	endMs := end.UnixMilli()
	for i := 0; i < n; i++ {
		openMs := endMs - int64(n-i)*stepMs
		o := base
		h := base + 2
		l := base - 2
		// Two isolated peaks at the same high. Their positions are DERIVED from
		// the window size, not typed: a strict pivot with k=2 needs two bars
		// either side, so index 2 and index n-3 are the outermost valid
		// positions for any n >= 7. Hardcoding 3 and 8 silently produced a
		// one-pivot window at n=9 and made a test SKIP rather than assert.
		if i == 2 || i == n-3 {
			h = base + 20
		}
		out = append(out, market.Kline{
			OpenTime:  openMs,
			Open:      o,
			High:      h,
			Low:       l,
			Close:     base,
			Volume:    1000,
			CloseTime: openMs + stepMs - 1,
		})
	}
	return out
}

// fetchFor returns a fetch func that serves the same bar shape for one
// timeframe and nothing for any other, so a test states exactly which
// timeframe it is exercising.
func fetchFor(tf string, bars []market.Kline) func(string, int) []market.Kline {
	return func(want string, _ int) []market.Kline {
		if want == tf {
			return bars
		}
		return nil
	}
}

// nativeDailyBarsWithTwoEqualPivotHighs builds n CLOSED native daily bars with
// the same pivot-high shape as barsWithTwoEqualPivotHighs, but stamped the way
// the NT8 native 1d series actually is (#15): OpenTime = (D-1) 00:00 CT,
// CloseTime = D 00:00 CT − 1ms. The newest bar is the most recent trading date
// whose session (which closes at D 16:00 CT) has already closed at `now`.
func nativeDailyBarsWithTwoEqualPivotHighs(n int, base float64, now time.Time) []market.Kline {
	out := make([]market.Kline, 0, n)
	lastTD := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, CTLocation())
	startTD := lastTD.AddDate(0, 0, -(n - 1))
	for i := 0; i < n; i++ {
		td := startTD.AddDate(0, 0, i)
		open := time.Date(td.Year(), td.Month(), td.Day()-1, 0, 0, 0, 0, CTLocation())
		o := base
		h := base + 2
		l := base - 2
		if i == 2 || i == n-3 {
			h = base + 20
		}
		out = append(out, market.Kline{
			OpenTime: open.UnixMilli(),
			Open:     o, High: h, Low: l, Close: base, Volume: 1000,
			CloseTime: time.Date(td.Year(), td.Month(), td.Day(), 0, 0, 0, 0, CTLocation()).UnixMilli() - 1,
		})
	}
	return out
}

// ---------------------------------------------------------------- E1

// TestE1_DailyTimeframeReachesTheMap — RED before this wave: isHTFDetectionTF
// rejects "1d", so DetectHTFLevels returns nothing however good the bars are.
func TestE1_DailyTimeframeReachesTheMap(t *testing.T) {
	now := tfTestNow()
	daily := nativeDailyBarsWithTwoEqualPivotHighs(30, 29500, now)

	got := DetectHTFLevels(fetchFor("1d", daily), []string{"1d"}, "MNQ", now)
	if len(got) == 0 {
		t.Fatalf("no level detected on 1d from %d daily bars — the daily timeframe never reaches the map", len(daily))
	}
	for _, l := range got {
		if l.TF != "1d" {
			t.Errorf("level %s @%.2f carries TF=%q, want %q — a level must name the timeframe it formed on", l.Kind, l.Price, l.TF, "1d")
		}
	}
}

// TestE1b_WeeklyTimeframeReachesTheMap — the top of the range the owner ruled.
func TestE1b_WeeklyTimeframeReachesTheMap(t *testing.T) {
	now := tfTestNow()
	weekly := barsWithTwoEqualPivotHighs(30, 29500, 7*tfTestDayMs, now)

	got := DetectHTFLevels(fetchFor("1w", weekly), []string{"1w"}, "MNQ", now)
	if len(got) == 0 {
		t.Fatalf("no level detected on 1w from %d weekly bars", len(weekly))
	}
	for _, l := range got {
		if l.TF != "1w" {
			t.Errorf("level %s @%.2f carries TF=%q, want %q", l.Kind, l.Price, l.TF, "1w")
		}
	}
}

// TestE1c_SubFifteenStaysOut — the owner's ruling kept the existing gate's
// reason: intraday noise below 15m adds nothing to HTF detection, and base
// swings continue to serve 5m/15m. This pins the gate so a later widening is a
// decision rather than an accident.
func TestE1c_SubFifteenStaysOut(t *testing.T) {
	now := tfTestNow()
	for _, tf := range []string{"1m", "3m", "5m"} {
		bars := barsWithTwoEqualPivotHighs(30, 29500, tfTestMinuteMs, now)
		if got := DetectHTFLevels(fetchFor(tf, bars), []string{tf}, "MNQ", now); len(got) != 0 {
			t.Errorf("tf %s produced %d HTF level(s); sub-15m must stay out of HTF detection", tf, len(got))
		}
	}
}

// ---------------------------------------------------------------- E3

// TestE3_TimeframeIsPartOfIdentity — two levels of the SAME kind at the SAME
// price on DIFFERENT timeframes are two distinct references, not one. Before
// this wave dedupeSameKind keyed on (kind, price±tick) alone, so a 1d order
// block and a 1h order block at one price collapsed to whichever the detector
// emitted first — and the survivor kept the loser's timeframe silently.
func TestE3_TimeframeIsPartOfIdentity(t *testing.T) {
	in := []DetectedLevel{
		{Kind: KindOB, Price: 29500, Lo: 29500, Hi: 29500, Label: "OB·1h", TF: "1h"},
		{Kind: KindOB, Price: 29500, Lo: 29500, Hi: 29500, Label: "OB·1d", TF: "1d"},
	}
	out := dedupeSameKind(in)
	if len(out) != 2 {
		t.Fatalf("dedupeSameKind collapsed %d levels to %d — a 1h and a 1d level at one price are two references, not a duplicate", len(in), len(out))
	}
	seen := map[string]bool{}
	for _, l := range out {
		seen[l.TF] = true
	}
	if !seen["1h"] || !seen["1d"] {
		t.Errorf("survivors carry timeframes %v, want both 1h and 1d", seen)
	}
}

// TestE3b_SameTimeframeStillDedupes — the other half of the identity change.
// Widening the key must not stop the thing the key was added for (register S4:
// the dual nPOC emission paths could seat one POC twice).
func TestE3b_SameTimeframeStillDedupes(t *testing.T) {
	in := []DetectedLevel{
		{Kind: KindOB, Price: 29500.00, Lo: 29500, Hi: 29500, Label: "OB·1h", TF: "1h"},
		{Kind: KindOB, Price: 29500.10, Lo: 29500.10, Hi: 29500.10, Label: "OB·1h", TF: "1h"},
	}
	if out := dedupeSameKind(in); len(out) != 1 {
		t.Fatalf("two same-kind same-tf levels within a tick produced %d survivors, want 1", len(out))
	}
}

// ---------------------------------------------------------------- E4

// TestE4_PartialWindowEmitsNothingAndSaysSo — a timeframe holding fewer bars
// than the definition needs must produce NO level and must record why. Silence
// is the failure mode this pins: before the wave the loop `continue`d with no
// record, so "1w produced nothing" and "1w was never read" were the same
// observation from outside.
func TestE4_PartialWindowEmitsNothingAndSaysSo(t *testing.T) {
	now := tfTestNow()
	tooFew := nativeDailyBarsWithTwoEqualPivotHighs(3, 29500, now)

	levels, report := DetectHTFLevelsReport(fetchFor("1d", tooFew), []string{"1d"}, "MNQ", now)
	if len(levels) != 0 {
		t.Fatalf("a %d-bar window produced %d level(s); a partial window must emit nothing", len(tooFew), len(levels))
	}
	skip, ok := report.Skipped["1d"]
	if !ok || skip == "" {
		t.Fatalf("no skip reason recorded for 1d; report=%+v — a timeframe that emits nothing must say why", report)
	}
	// The reason must be the PARTIAL WINDOW, not "outside the detection set".
	// Before the gate admitted 1d this assertion passed for the wrong reason —
	// a skip was recorded, but because the timeframe was rejected outright. A
	// test that cannot tell those two apart would go green the day someone
	// narrows the gate again.
	if !strings.Contains(skip, "closed bars") {
		t.Errorf("1d skipped with %q; want the partial-window reason naming the bar shortfall, not a gate rejection", skip)
	}
	if _, counted := report.Counts["1d"]; counted {
		t.Errorf("1d appears in Counts as well as Skipped; a timeframe belongs to exactly one so that 'produced nothing' and 'was never read' stay distinguishable")
	}
}

// TestE4b_ReportCountsPerTimeframe — D6's boot line reads per-tf counts from
// this report rather than recomputing them, so the line cannot drift from the
// detection that produced it (A11: boot lines are READ, never literal).
func TestE4b_ReportCountsPerTimeframe(t *testing.T) {
	now := tfTestNow()
	daily := nativeDailyBarsWithTwoEqualPivotHighs(30, 29500, now)

	levels, report := DetectHTFLevelsReport(fetchFor("1d", daily), []string{"1d"}, "MNQ", now)
	if report.Counts["1d"] != len(levels) {
		t.Errorf("report says 1d=%d, detection returned %d levels — the boot line would print a number nothing produced", report.Counts["1d"], len(levels))
	}
	if len(levels) == 0 {
		t.Fatalf("fixture produced no levels; this test cannot distinguish a correct zero from a broken fetch")
	}
}

// ---------------------------------------------------------------- C6 / D5

// TestC6_DailyInheritsTheFourHourTier — the owner's ruling. Before the wave,
// zoneTierFor("1d") fell through to the "1m" noise floor: multiplier 1.0,
// KindOB evidence 0.40 against 4h's 0.72, and the zone grader's default clause
// forced grade C. A daily level would have been the weakest thing on the map.
//
// The tier VALUES are not asserted here — this wave changes no weight. What is
// asserted is which tier the classification lands in, read from the production
// table rather than retyped (A24).
func TestC6_DailyInheritsTheFourHourTier(t *testing.T) {
	for _, tf := range []string{"1d", "3d", "1w"} {
		if got := zoneTierFor(tf); got != "4h" {
			t.Errorf("zoneTierFor(%q) = %q, want %q — a daily/weekly level must not be graded at the 1m noise floor", tf, got, "4h")
		}
	}
	// The tier must be a real key in the production multiplier table, or the
	// classification resolves to a missing-map zero.
	if _, ok := zoneTFMult["4h"]; !ok {
		t.Fatalf("zoneTFMult has no %q key — the tier this wave classifies daily levels into does not exist", "4h")
	}
}

// TestC6b_KnownTiersUnmoved — the classification change must touch ONLY inputs
// that were previously impossible. Every timeframe that already had an answer
// keeps it.
func TestC6b_KnownTiersUnmoved(t *testing.T) {
	for tf, want := range map[string]string{
		"":    "1m",
		"1m":  "1m",
		"3m":  "1m",
		"5m":  "1m",
		"15m": "15m",
		"30m": "15m",
		"1h":  "1h",
		"2h":  "1h",
		"4h":  "4h",
		"6h":  "4h",
		"8h":  "4h",
		"12h": "4h",
	} {
		if got := zoneTierFor(tf); got != want {
			t.Errorf("zoneTierFor(%q) = %q, want %q — this wave must not move a timeframe that already had a tier", tf, got, want)
		}
	}
}

// TestC6c_DailyCarriesTheHTFFlag — the 1.2× fires on DetectedLevel.HTF, which
// tagHTFLevel sets for every timeframe the detection gate admits. Daily levels
// join that set by the same route as 4h, with no change to the multiplier
// itself. Round 12 (12c) says the 1.2× is untested; this asserts only that a
// daily level is treated like the other higher timeframes, not that it should
// be.
func TestC6c_DailyCarriesTheHTFFlag(t *testing.T) {
	now := tfTestNow()
	daily := nativeDailyBarsWithTwoEqualPivotHighs(30, 29500, now)
	got := DetectHTFLevels(fetchFor("1d", daily), []string{"1d"}, "MNQ", now)
	if len(got) == 0 {
		t.Fatalf("no daily level produced; cannot assert the HTF flag")
	}
	for _, l := range got {
		if !l.HTF {
			t.Errorf("daily level %s @%.2f has HTF=false — it would miss the multiplier every other higher timeframe receives", l.Kind, l.Price)
		}
	}
}

// ---------------------------------------------------------------- E6 / E2

// TestE6_LegacyInputUnchangedByTheIdentityWiden — the no-regression pin, stated
// as a property rather than a fixture. Widening the dedupe key can only change
// behaviour for inputs that DIFFER in the new field; for any input where every
// level carries the same timeframe, the new key and the old key must select the
// same survivors. Proving it this way means the claim does not depend on
// whichever shapes the existing fixtures happen to contain — the Stage A golden
// passing is not by itself evidence, because nothing guarantees it holds a
// cross-timeframe collision.
func TestE6_LegacyInputUnchangedByTheIdentityWiden(t *testing.T) {
	// Every level on one timeframe: the pre-W-TF world, where TF was set by
	// tagHTFLevel for HTF detections and left "" for the 1m slice.
	for _, tf := range []string{"", "1h"} {
		in := []DetectedLevel{
			{Kind: KindOB, Price: 29500.00, Label: "OB", TF: tf},
			{Kind: KindOB, Price: 29500.10, Label: "OB", TF: tf},   // within a tick — collapses
			{Kind: KindOB, Price: 29520.00, Label: "OB", TF: tf},   // apart — survives
			{Kind: KindEQH, Price: 29500.00, Label: "EQH", TF: tf}, // other kind — survives
		}
		out := dedupeSameKind(in)
		if len(out) != 3 {
			t.Errorf("tf=%q: %d survivors, want 3 — the identity widen changed a single-timeframe result", tf, len(out))
		}
	}
}

// TestE2_CrossTimeframeCoincidenceIsOneCandidateWithBothNames — D3. W3's merge
// groups references within the cluster width into ONE map candidate carrying
// every name. It keys on price alone, so it was already timeframe-blind in the
// direction this wave needs: a 15m level and a 1d level at one price merge
// rather than compete. What this asserts is that both TIMEFRAMES survive into
// the candidate's names, so the owner reads "Supply·1h · SWG-H·1d" and not one
// of them silently standing for both.
func TestE2_CrossTimeframeCoincidenceIsOneCandidateWithBothNames(t *testing.T) {
	scored := []ScoredLevel{
		{DetectedLevel: DetectedLevel{Kind: KindOB, Price: 29500, Lo: 29500, Hi: 29500, Label: "OB·1h", TF: "1h"}, Score: 0.7, Grade: "B"},
		{DetectedLevel: DetectedLevel{Kind: KindEQH, Price: 29500, Lo: 29500, Hi: 29500, Label: "EQH·1d", TF: "1d"}, Score: 0.6, Grade: "B"},
	}
	got := BuildMapCandidates(scored, 29400, 20, MapCandidateOpts{})
	if len(got) != 1 {
		t.Fatalf("two references at one price produced %d candidates, want 1 merged", len(got))
	}
	names := got[0].NamesLine()
	if !strings.Contains(names, "1h") || !strings.Contains(names, "1d") {
		t.Errorf("merged candidate names = %q; both timeframes must appear — a cross-timeframe confluence the owner cannot see is one he cannot weigh", names)
	}
	if got[0].MergedCredit != 1 {
		t.Errorf("merged candidate carries credit %d, want 1 — W3's rule is one credit per candidate however many names", got[0].MergedCredit)
	}
}

// ---------------------------------------------------------------- D4 / A9

// TestD4_DetectedLevelNamesItsTimeframeAndWindow — E2 above proves the MERGE
// carries both timeframes, but it builds its labels by hand, so it never drives
// tagHTFLevel: a mutation removing the "·tf" suffix from real detection
// survived it. This test closes that by asserting on the output of detection
// itself.
//
// A9 (loud logging): every level emitted names its timeframe. D4 (12a): it also
// records the window that found it, which is a different fact from the
// timeframe and from its age.
func TestD4_DetectedLevelNamesItsTimeframeAndWindow(t *testing.T) {
	now := tfTestNow()
	const bars = 30
	daily := nativeDailyBarsWithTwoEqualPivotHighs(bars, 29500, now)

	got := DetectHTFLevels(fetchFor("1d", daily), []string{"1d"}, "MNQ", now)
	if len(got) == 0 {
		t.Fatalf("no daily level produced; this test cannot distinguish a correct zero from a broken fixture")
	}
	for _, l := range got {
		if !strings.HasSuffix(l.Label, "·1d") {
			t.Errorf("level label %q does not name its timeframe — the owner reads the label, not the struct", l.Label)
		}
		if l.LookbackBars != bars {
			t.Errorf("level %q records LookbackBars=%d, want %d — the window that found a level is not derivable from its timeframe", l.Label, l.LookbackBars, bars)
		}
		if l.TF == "" {
			t.Errorf("level %q has an empty TF field", l.Label)
		}
	}
}

// TestD4b_LookbackIsTheWindowActuallySearched — not the window requested. A
// timeframe holding fewer bars than the fetch depth must record what it really
// searched, or the field asserts a depth that never existed (A24: a placeholder
// that reads as data).
func TestD4b_LookbackIsTheWindowActuallySearched(t *testing.T) {
	now := tfTestNow()
	const short = 9 // above htfMinClosedBars, far below the fetch depth
	weekly := barsWithTwoEqualPivotHighs(short, 29500, 7*tfTestDayMs, now)

	got, rep := DetectHTFLevelsReport(fetchFor("1w", weekly), []string{"1w"}, "MNQ", now)
	if len(got) == 0 {
		t.Fatalf("a %d-bar window produced no level; the fixture must assert, never skip", short)
	}
	for _, l := range got {
		if l.LookbackBars != short {
			t.Errorf("LookbackBars=%d, want %d — the recorded window must be what was searched, not what was asked for", l.LookbackBars, short)
		}
	}
	if !strings.Contains(rep.Resolved["1w"], "bars=9") {
		t.Errorf("report resolved params %q do not name the real window", rep.Resolved["1w"])
	}
}

// ---------------------------------------------------------------- the live config

// TestD1_TheOwnersConfiguredTimeframesReachDetection — the test that decides
// whether this wave is real. A29's question is "production call sites: 0?", and
// the answer here was nearly yes: the gate learned "1d" while the bound
// strategy's planner_timeframes says "D".
//
// The list below is the LIVE configured value, read from the bound strategy on
// 2026-09-10 (traders.strategy_id -> strategies.config.day_plan.
// planner_timeframes = ["D","4h","1h","15m","5m"]). It is exercised verbatim,
// including the entries that must NOT produce HTF levels, so this asserts the
// whole configured set behaves as intended rather than the one entry the wave
// added.
func TestD1_TheOwnersConfiguredTimeframesReachDetection(t *testing.T) {
	now := tfTestNow()
	configured := []string{"D", "4h", "1h", "15m", "5m"}

	fetch := func(tf string, _ int) []market.Kline {
		switch tf {
		case "1d":
			return nativeDailyBarsWithTwoEqualPivotHighs(30, 29500, now)
		case "4h", "1h", "15m":
			return barsWithTwoEqualPivotHighs(30, 29500, 60*tfTestMinuteMs, now)
		}
		return nil // 5m is not fetched: it is outside the HTF detection set
	}

	levels, rep := DetectHTFLevelsReport(fetch, configured, "MNQ", now)

	var daily int
	for _, l := range levels {
		if l.TF == "1d" {
			daily++
		}
	}
	if daily == 0 {
		t.Fatalf("the owner's configured timeframes produced NO daily level; \"D\" never reached detection (report=%+v)", rep)
	}
	if rep.Counts["1d"] == 0 {
		t.Errorf("report records 1d=%d; the boot line would say the daily pass found nothing", rep.Counts["1d"])
	}
	if _, skipped := rep.Skipped["5m"]; !skipped {
		t.Errorf("5m is not recorded as skipped; a configured timeframe that is deliberately not detected on must say so, not vanish")
	}
}

// TestD1b_DailyAliasesCanonicaliseOnce — the alias set, pinned. "D" is what the
// config writes; "1d" is what the store and every tier table speak.
func TestD1b_DailyAliasesCanonicaliseOnce(t *testing.T) {
	for in, want := range map[string]string{
		"D": "1d", "d": "1d", " D ": "1d", "daily": "1d", "1day": "1d",
		"W": "1w", "w": "1w", "weekly": "1w",
		"1d": "1d", "1w": "1w", "4h": "4h", "15m": "15m", "": "",
	} {
		if got := canonicalDetectionTF(in); got != want {
			t.Errorf("canonicalDetectionTF(%q) = %q, want %q", in, got, want)
		}
	}
}
