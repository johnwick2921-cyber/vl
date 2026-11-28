package kernel

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// E5 — THE PWH/PWL PIN.
//
// C6: the prior-week extremes are emitted but guarded by priorWeekMinBars=4320
// on a ~33h 1m ring, so they can NEVER seat — the const block at
// levels_multiday.go:219-221 says so itself, and candidate_pool has held zero
// PWH/PWL rows ever. The const block also names the correct fix: "those anchors
// must come from a multi-day source, not the ring."
//
// So this pin does NOT assert a relaxed guard. It asserts the DAILY source.
// Relaxing the ring guard would compute a prior-week high from 33 hours of
// data, which is not a prior-week high.

func dailyBar(t time.Time, high, low float64) market.Kline {
	return market.Kline{OpenTime: t.UnixMilli(), High: high, Low: low, Open: low, Close: high}
}

func TestE5_PriorWeekExtremesComeFromTheDailySource(t *testing.T) {
	loc := chicago()
	// "now" is Wednesday 2026-09-09; the prior week is Mon 08-31 → Sun 09-06.
	now := time.Date(2026, 9, 9, 13, 0, 0, 0, loc)

	daily := []market.Kline{
		dailyBar(time.Date(2026, 8, 24, 0, 0, 0, 0, loc), 29100, 28900), // two weeks back — excluded
		dailyBar(time.Date(2026, 8, 31, 0, 0, 0, 0, loc), 29400, 29050), // prior week
		dailyBar(time.Date(2026, 9, 1, 0, 0, 0, 0, loc), 29610, 29200),  // prior week — the HIGH
		dailyBar(time.Date(2026, 9, 3, 0, 0, 0, 0, loc), 29500, 28980),  // prior week — the LOW
		dailyBar(time.Date(2026, 9, 8, 0, 0, 0, 0, loc), 29800, 29600),  // THIS week — excluded
	}

	got := ProjectPriorWeekExtremes(daily, now)
	if len(got) != 2 {
		t.Fatalf("want PWH and PWL from the daily source, got %d: %+v", len(got), got)
	}
	var pwh, pwl *MapCandidate
	for i := range got {
		switch got[i].Names[0] {
		case "PWH":
			pwh = &got[i]
		case "PWL":
			pwl = &got[i]
		}
	}
	if pwh == nil || pwl == nil {
		t.Fatalf("missing PWH or PWL: %+v", got)
	}
	if pwh.Price != 29610 {
		t.Errorf("PWH = %.2f, want 29610 (the prior week's high, excluding this week's 29800)", pwh.Price)
	}
	if pwl.Price != 28980 {
		t.Errorf("PWL = %.2f, want 28980 (the prior week's low, excluding 28900 two weeks back)", pwl.Price)
	}
	for _, c := range []*MapCandidate{pwh, pwl} {
		if !c.Projection {
			t.Errorf("%s must be marked a projection", c.Names[0])
		}
		if !strings.Contains(c.ProjectionMethod, "daily bars") {
			t.Errorf("%s method = %q; it must name the DAILY source, not the ring", c.Names[0], c.ProjectionMethod)
		}
	}
}

// E5c — THE GUARD IS UNTOUCHED.
//
// The owner corrected D5(a): PWH/PWL come from the daily source, "never a
// relaxed guard on the ring, which would manufacture a fake prior-week
// extreme." This pin proves the ring guard still refuses, so a later wave
// cannot quietly relax it and call this pin's PWH the ring's work.
func TestE5c_TheRingGuardStillRefusesPriorWeek(t *testing.T) {
	if priorWeekMinBars != 4320 {
		t.Fatalf("priorWeekMinBars = %d, want 4320 — W3 must not relax the ring guard", priorWeekMinBars)
	}
	loc := chicago()
	start := time.Date(2026, 8, 31, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 7)
	// A ~33h ring: two calendar days of 1m bars, far under 4320.
	calDays := map[string]int{
		"2026-08-31": 900,
		"2026-09-01": 900,
	}
	if pwCovered(calDays, start, end, priorWeekMinBars) {
		t.Error("the 33h ring satisfied the prior-week guard — the guard was relaxed, which is exactly what the const block forbids")
	}
}

// E5b — an uncovered prior week emits NOTHING rather than a zero or a guess.
func TestE5b_UncoveredPriorWeekEmitsNothing(t *testing.T) {
	loc := chicago()
	now := time.Date(2026, 9, 9, 13, 0, 0, 0, loc)
	// Only THIS week's bars — the prior week has no coverage at all.
	daily := []market.Kline{dailyBar(time.Date(2026, 9, 8, 0, 0, 0, 0, loc), 29800, 29600)}

	if got := ProjectPriorWeekExtremes(daily, now); len(got) != 0 {
		t.Fatalf("an uncovered prior week must emit nothing; got %+v", got)
	}
}

// D5b — round numbers project OUTWARD only; the mapped range is untouched.
func TestD5b_RoundNumbersProjectBeyondTheRangeOnly(t *testing.T) {
	got := ProjectRoundNumbersBeyond(29400, 29600, 100, 250)
	if len(got) == 0 {
		t.Fatal("no round numbers projected beyond a 200 pt range with 250 pt reach")
	}
	for _, c := range got {
		if c.Price > 29400 && c.Price < 29600 {
			t.Errorf("RN %.0f is INSIDE the mapped range — the in-range detector owns that", c.Price)
		}
		if !c.Projection || c.ProjectionMethod == "" {
			t.Errorf("RN %.0f must be a labelled projection", c.Price)
		}
		if c.Grade != "" {
			t.Errorf("RN %.0f carries grade %q; a projection has no detector grade", c.Price, c.Grade)
		}
	}
}

// D5c — the ATR-projected session extreme reads its k from the resolver.
func TestD5c_SessionExtremeUsesTheResolvedMultiple(t *testing.T) {
	got := ProjectSessionExtreme(29500, 200)
	if len(got) != 2 {
		t.Fatalf("want a high and a low projection, got %d", len(got))
	}
	k := SessionExtremeATRMult()
	wantHi := 29500 + k*200
	if got[0].Price != wantHi {
		t.Errorf("projected high = %.2f, want %.2f (open + %v×200)", got[0].Price, wantHi, k)
	}
	if !strings.Contains(got[0].ProjectionMethod, "[I]") {
		t.Errorf("method %q must carry the [I] label until E4 measures it", got[0].ProjectionMethod)
	}
}

// D5d — the measured move needs a COMPLETED break; inside the swing there is
// none, and nothing is emitted rather than a guess.
func TestD5d_MeasuredMoveNeedsACompletedBreak(t *testing.T) {
	if got := ProjectMeasuredMove(29400, 29600, 29500); len(got) != 0 {
		t.Errorf("price inside the swing is not a break; want nothing, got %+v", got)
	}
	up := ProjectMeasuredMove(29400, 29600, 29610)
	if len(up) != 1 || up[0].Price != 29800 {
		t.Fatalf("a break above 29600 projects 29600+200=29800; got %+v", up)
	}
	dn := ProjectMeasuredMove(29400, 29600, 29390)
	if len(dn) != 1 || dn[0].Price != 29200 {
		t.Fatalf("a break below 29400 projects 29400-200=29200; got %+v", dn)
	}
}
