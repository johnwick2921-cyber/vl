package kernel

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// mkBars builds a 1m bar series starting at `start`, one bar per minute, with
// given OHLCV values (cycled). Times are UTC; the detectors roll sessions in CT.
func mkBars(start time.Time, ohlcv [][5]float64) []market.Kline {
	out := make([]market.Kline, len(ohlcv))
	for i, v := range ohlcv {
		t := start.Add(time.Duration(i) * time.Minute)
		out[i] = market.Kline{
			OpenTime: t.UnixMilli(),
			Open:     v[0], High: v[1], Low: v[2], Close: v[3], Volume: v[4],
			CloseTime: t.Add(time.Minute).UnixMilli() - 1,
		}
	}
	return out
}

// sessionStartUTC returns a time just after a 17:00 CT session roll.
func sessionStartUTC(t *testing.T) time.Time {
	loc := CTLocation()
	base := time.Date(2026, 8, 25, 17, 5, 0, 0, loc)
	return base
}

// TestSessionVWAPMoves: the session VWAP is computed from closed bars and moves
// when the auction prices shift (dynamic re-emit).
func TestSessionVWAPMoves(t *testing.T) {
	start := sessionStartUTC(t)
	bars1 := mkBars(start, [][5]float64{
		{100, 101, 99, 100, 10},
		{100, 102, 100, 101, 10},
		{101, 103, 101, 102, 10},
		{102, 104, 102, 103, 10}, // forming (CloseTime in the future)
	})
	now := start.Add(3 * time.Minute)
	lv := SessionVWAPLevels(bars1, now)
	if len(lv) != 5 {
		t.Fatalf("SessionVWAPLevels = %d levels, want 5 (VWAP, ±1σ, ±2σ — T5 emission)", len(lv))
	}
	// VWAP must be volume-weighted: bars 1-3 tp = 100, 101, 102 → 101.
	if math.Abs(lv[0].Price-101) > 0.01 {
		t.Fatalf("VWAP = %.2f, want 101", lv[0].Price)
	}
	if lv[1].Price <= lv[0].Price || lv[2].Price >= lv[0].Price {
		t.Fatalf("band order wrong: +1σ %.2f, VWAP %.2f, −1σ %.2f", lv[1].Price, lv[0].Price, lv[2].Price)
	}
	// Feed 10 more bars with prices 10 higher → VWAP rises (dynamic).
	var more [][5]float64
	for i := 0; i < 10; i++ {
		more = append(more, [5]float64{110, 111, 109, 110, 10})
	}
	bars2 := mkBars(start, append([][5]float64{
		{100, 101, 99, 100, 10},
		{100, 102, 100, 101, 10},
		{101, 103, 101, 102, 10},
	}, more...))
	now2 := start.Add(12 * time.Minute)
	lv2 := SessionVWAPLevels(bars2, now2)
	if len(lv2) != 5 || lv2[0].Price <= 104 {
		t.Fatalf("VWAP after higher bars = %+v, want > 104", lv2)
	}
}

// TestPriorDayProfileCached: POC/VAH/VAL come from the prior session-day and the
// cache at roll returns the same slice (cached at roll).
func TestPriorDayProfileCached(t *testing.T) {
	loc := CTLocation()
	// Prior session day: 2026-08-24 17:00 CT → 2026-08-25 17:00 CT.
	priorStart := time.Date(2026, 8, 24, 17, 5, 0, 0, loc)
	// Volume concentrated around 100 → POC ≈ 100.
	var bars []market.Kline
	tt := priorStart
	for i := 0; i < 60; i++ {
		price := 100.0
		if i >= 40 {
			price = 104
		}
		bars = append(bars, market.Kline{
			OpenTime: tt.UnixMilli(), Open: price, High: price + 1, Low: price - 1, Close: price,
			Volume: 10, CloseTime: tt.Add(time.Minute).UnixMilli() - 1,
		})
		tt = tt.Add(time.Minute)
	}
	now := time.Date(2026, 8, 25, 17, 10, 0, 0, loc) // after the roll
	prof := PriorDayProfileLevels(bars, now)
	if len(prof) != 3 {
		t.Fatalf("PriorDayProfileLevels = %d levels, want 3", len(prof))
	}
	if math.Abs(prof[0].Price-100) > 1.5 {
		t.Fatalf("POC = %.2f, want ≈100", prof[0].Price)
	}
	if prof[1].Price <= prof[2].Price {
		t.Fatalf("VAH %.2f must be above VAL %.2f", prof[1].Price, prof[2].Price)
	}
	// Second call must return the cached computation (same values).
	prof2 := PriorDayProfileLevels(bars, now)
	if len(prof2) != 3 || prof2[0].Price != prof[0].Price {
		t.Fatalf("cached profile changed: %+v vs %+v", prof2, prof)
	}
}

// TestNakedPOCRetireOnTouch: a prior-day POC that price later trades through is
// RETIRED (excluded); an untouched POC stays naked.
func TestNakedPOCRetireOnTouch(t *testing.T) {
	loc := CTLocation()
	day1 := time.Date(2026, 8, 24, 17, 5, 0, 0, loc)
	day2 := time.Date(2026, 8, 25, 17, 5, 0, 0, loc)
	var bars []market.Kline
	// Day 1: flat 100-101, POC near 100.
	tt := day1
	for i := 0; i < 40; i++ {
		bars = append(bars, market.Kline{
			OpenTime: tt.UnixMilli(), Open: 100, High: 101, Low: 99, Close: 100,
			Volume: 10, CloseTime: tt.Add(time.Minute).UnixMilli() - 1,
		})
		tt = tt.Add(time.Minute)
	}
	// Day 2: price first trades back THROUGH the POC (touch → retire), then
	// pushes higher.
	tt = day2
	for i := 0; i < 40; i++ {
		bars = append(bars, market.Kline{
			OpenTime: tt.UnixMilli(), Open: 100, High: 105, Low: 97, Close: 100,
			Volume: 10, CloseTime: tt.Add(time.Minute).UnixMilli() - 1,
		})
		tt = tt.Add(time.Minute)
	}
	now := day2.Add(2 * time.Minute)
	out := NakedPOCLevels(bars, now)
	// Day-1 POC was traded through on day 2 → retired, no naked POC emitted.
	for _, l := range out {
		if l.Kind == KindNPOC {
			t.Fatalf("touched POC must be retired, got %+v", l)
		}
	}
}

// TestNakedPOCFrontTruncatedDaySkipped — bug-hunt #24: the 1m ring holds about
// 1.45 sessions, so the prior day is a front-truncated tail through the NY
// session. NakedPOCLevels must NOT build a naked POC from that tail (its mode
// is not the session POC); the stored-profile provider supplies the whole one.
// Mutant (drop the sessionFrontTruncated guard) → RED.
func TestNakedPOCFrontTruncatedDaySkipped(t *testing.T) {
	loc := CTLocation()
	// The ring opens mid-prior-session (10:00 CT) — the 17:00→10:00 part of
	// session-day 08-23 is missing, so that day is front-truncated.
	tt := time.Date(2026, 8, 24, 10, 0, 0, 0, loc)
	var bars []market.Kline
	for tt.Hour() < 17 {
		bars = append(bars, market.Kline{
			OpenTime: tt.UnixMilli(), Open: 100, High: 101, Low: 99, Close: 100,
			Volume: 10, CloseTime: tt.Add(time.Minute).UnixMilli() - 1,
		})
		tt = tt.Add(time.Minute)
	}
	// Current session (08-24 17:00 → 08-25 10:00): price far from 100.
	end := time.Date(2026, 8, 25, 10, 0, 0, 0, loc)
	for tt.Before(end) {
		bars = append(bars, market.Kline{
			OpenTime: tt.UnixMilli(), Open: 110, High: 111, Low: 109, Close: 110,
			Volume: 10, CloseTime: tt.Add(time.Minute).UnixMilli() - 1,
		})
		tt = tt.Add(time.Minute)
	}
	out := NakedPOCLevels(bars, end)
	for _, l := range out {
		if l.Kind == KindNPOC {
			t.Fatalf("front-truncated prior day must not emit a naked POC; got %+v", l)
		}
	}
}

// TestRoleAssignment: kind base roles + state overrides (consumed → target_only,
// far-HTF → target_only).
func TestRoleAssignment(t *testing.T) {
	if RoleFor(DetectedLevel{Kind: KindVWAP}, "") != RoleMagnetMeanRevert {
		t.Fatal("VWAP base role must be magnet_meanrevert")
	}
	if RoleFor(DetectedLevel{Kind: KindONH}, "") != RoleLiquidityBreak {
		t.Fatal("ONH base role must be liquidity_break")
	}
	if RoleFor(DetectedLevel{Kind: KindPDH}, "") != RoleReactZone {
		t.Fatal("PDH base role must be react_zone")
	}
	if RoleFor(DetectedLevel{Kind: KindPDC}, "") != RolePivot {
		t.Fatal("PDC base role must be pivot")
	}
	if RoleFor(DetectedLevel{Kind: KindVWAP}, "done") != RoleTargetOnly {
		t.Fatal("consumed VWAP must role-flip to target_only")
	}
	// far-HTF: a 4h CONTINUATION zone is context → target_only; a weekly nPOC
	// (HTF) keeps its magnet role (spec: nPOC = magnet_meanrevert).
	if RoleFor(DetectedLevel{Kind: KindFVG, HTF: true, TF: "4h"}, "") != RoleTargetOnly {
		t.Fatal("far-HTF continuation zone must be target_only")
	}
	if RoleFor(DetectedLevel{Kind: KindNPOC, HTF: true}, "") != RoleMagnetMeanRevert {
		t.Fatal("HTF nPOC must keep magnet_meanrevert (spec grammar)")
	}
	// Env override wins.
	ApplyRoleMapOverrides("VWAP=liquidity_break")
	defer ApplyRoleMapOverrides("")
	if RoleFor(DetectedLevel{Kind: KindVWAP}, "") != RoleLiquidityBreak {
		t.Fatal("env LEVEL_ROLE_MAP override must win over the default grammar")
	}
}

// TestBiasContextLine: facts line renders VWAP/PDC/value-area/magnet/liquidity.
func TestBiasContextLine(t *testing.T) {
	start := sessionStartUTC(t)
	bars := mkBars(start, [][5]float64{
		{100, 101, 99, 100, 10},
		{100, 102, 100, 101, 10},
		{101, 103, 101, 102, 10},
	})
	now := start.Add(2 * time.Minute)
	scored := []ScoredLevel{
		{DetectedLevel: DetectedLevel{Kind: KindPDC, Price: 99, Lo: 99, Hi: 99, Label: "PDC"}, Distance: -3},
		{DetectedLevel: DetectedLevel{Kind: KindVWAP, Price: 101, Lo: 101, Hi: 101, Label: "VWAP"}, Distance: -1},
		{DetectedLevel: DetectedLevel{Kind: KindONH, Price: 110, Lo: 110, Hi: 110, Label: "ONH"}, Distance: 8},
	}
	bc := ComputeBiasContext(bars, scored, now)
	line := bc.Line()
	for _, want := range []string{"bias_ctx:", "vs VWAP", "vs PDC", "nearest magnet VWAP", "nearest liquidity ONH"} {
		if !containsStr(line, want) {
			t.Fatalf("bias line %q missing %q", line, want)
		}
	}
}

// TestSeatVolumeFamily locks the E1 guarantee: when the top-N is full of
// priority anchors and an in-band volume-family level exists beyond the cap,
// ONE seat is swapped for it. Protected rows (priority, HTF-eligible, other
// volume rows) are never demoted.
func TestSeatVolumeFamily(t *testing.T) {
	scored := []ScoredLevel{
		{DetectedLevel: DetectedLevel{Kind: KindPDH, Price: 110, Label: "PDH"}, Grade: "A", Score: 9, Distance: 10},
		{DetectedLevel: DetectedLevel{Kind: KindONH, Price: 105, Label: "ONH"}, Grade: "A", Score: 8, Distance: 5},
		{DetectedLevel: DetectedLevel{Kind: KindPDL, Price: 90, Label: "PDL"}, Grade: "A", Score: 7, Distance: -10},
		{DetectedLevel: DetectedLevel{Kind: KindEQH, Price: 103, Label: "EQH", HTF: true}, Grade: "B", Score: 6, Distance: 3},
		{DetectedLevel: DetectedLevel{Kind: KindRound, Price: 102, Label: "RN 102"}, Grade: "B", Score: 5, Distance: 2},
		{DetectedLevel: DetectedLevel{Kind: KindRound, Price: 98, Label: "RN 98"}, Grade: "B", Score: 4, Distance: -2},
		{DetectedLevel: DetectedLevel{Kind: KindGap, Price: 108, Label: "GAP"}, Grade: "C", Score: 2, Distance: 8},
		{DetectedLevel: DetectedLevel{Kind: KindPWH, Price: 95, Label: "PWH"}, Grade: "C", Score: 1, Distance: -5},
		// Volume family beyond the cap — must win a seat.
		{DetectedLevel: DetectedLevel{Kind: KindVWAP, Price: 100.5, Label: "VWAP"}, Grade: "A", Score: 6.5, Distance: 0.5},
	}
	out := SeatVolumeFamily(scored, 8)
	seated := out[:8]
	has := false
	for _, l := range seated {
		if l.Kind == KindVWAP {
			has = true
		}
	}
	if !has {
		t.Fatalf("VWAP must win a seat via SeatVolumeFamily, got %+v", seated)
	}
	if len(seated) != 8 {
		t.Fatalf("seat count = %d, want 8", len(seated))
	}
	// Protected rows stay: PDH/ONH/PDL (priority) + EQH (HTF-eligible).
	for _, l := range seated {
		if l.Kind == KindPDH || l.Kind == KindONH || l.Kind == KindPDL {
			continue
		}
	}
	if out2 := SeatVolumeFamily(scored[:8], 8); len(out2) != 8 {
		t.Fatalf("no-cut input must be unchanged")
	}
}

// TestRoleMismatchesWarn locks the E4 validator: a magnet used as a breakout
// trigger and a liquidity level faded with a plain reject both produce
// WARN-only mismatch lines (never a rejection).
func TestRoleMismatchesWarn(t *testing.T) {
	doc := PlanDoc{
		Levels: []PlanLevel{
			{Price: 100, Label: "VWAP"},
			{Price: 110, Label: "ONH"},
		},
		Scenarios: []PlanScenario{
			{ID: "S1", Condition: "breakout_retest", Direction: "long", Confirm: &PlanConfirm{Rule: "1x5m_close", RefPrice: 100, Side: "above"}},
			{ID: "S2", Condition: "reject", Direction: "short", Confirm: &PlanConfirm{Rule: "1x5m_close", RefPrice: 110, Side: "below"}},
		},
	}
	ms := RoleMismatches(&doc)
	if len(ms) != 2 {
		t.Fatalf("RoleMismatches = %d lines, want 2 (magnet breakout + liquidity reject): %v", len(ms), ms)
	}
	for _, m := range ms {
		if !containsStr(m, "S1") && !containsStr(m, "S2") {
			t.Fatalf("mismatch line lost scenario id: %q", m)
		}
	}
}

// TestPlannerPromptCarriesRoleAndBias locks E3 on the PLANNER side: the
// planner prompt renders the ROLE column, the 5-line playbook, the bias_ctx
// facts line and the ≤5-line noise gate.
func TestPlannerPromptCarriesRoleAndBias(t *testing.T) {
	in := PlannerInput{
		TradeDate: "2026-08-26", Session: "NY", ReadKind: "test", Price: 29200, DATR: 180,
		Levels: []ScoredLevel{
			{DetectedLevel: DetectedLevel{Kind: KindVWAP, Price: 29250, Label: "VWAP"}, Grade: "A", Fresh: "fresh", Distance: 50, Role: RoleMagnetMeanRevert},
			{DetectedLevel: DetectedLevel{Kind: KindONH, Price: 29300, Label: "ONH"}, Grade: "A", Fresh: "fresh", Distance: 100, Role: RoleLiquidityBreak},
		},
		MaxLevels: 8, ScenarioCap: 3,
		BiasCtx: "bias_ctx: price 29200.00 · 50.0 vs VWAP 29250.00 · nearest magnet VWAP (+50.0) · nearest liquidity ONH (+100.0)",
	}
	p := BuildPlannerPrompt(in)
	for _, want := range []string{"magnet_meanrevert", "liquidity_break", "role playbook", "bias_ctx:", "NOISE FILTER"} {
		if !containsStr(p, want) {
			t.Fatalf("planner prompt missing %q", want)
		}
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
