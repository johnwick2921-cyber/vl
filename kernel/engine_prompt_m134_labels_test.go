package kernel

import (
	"strings"
	"testing"

	"vl/market"
	"vl/store"
)

// oneDummy1D returns a 1d TimeframeSeriesData with one bar — enough for
// formingBarLine to run. The bar's Time is deliberately a calendar-day open
// (00:00 CT) to reproduce the shape that produced the 528/658 mislabel.
func oneDummy1D() *market.TimeframeSeriesData {
	return &market.TimeframeSeriesData{
		Timeframe: "1d",
		Klines:    []market.KlineBar{{Time: ctMs(2026, 10, 20, 0, 0), Open: 29500, Close: 29900}},
	}
}

// TestFormingBarLine_DailySession pins the m134-a fix at the production call
// site: the 1d label is computed from the CME session (17:00 open → 16:00
// close), never calendar +24h. Under the pre-fix code a 10:00 CT snapshot
// rendered "CLOSED at 00:00 CT" (mutant RED) — the session close is 16:00 CT.
func TestFormingBarLine_DailySession(t *testing.T) {
	tfData := oneDummy1D()

	// 10:00 CT — the RTH session (opened 17:00 the prior day) is LIVE.
	if got := formingBarLine("1d", tfData, ctMs(2026, 10, 21, 10, 0)); !strings.Contains(got, "FORMING (closes 16:00 CT)") {
		t.Fatalf("10:00 CT must be FORMING closing 16:00 CT; got %q", got)
	}

	// 16:30 CT — the daily maintenance break: the session closed at 16:00.
	if got := formingBarLine("1d", tfData, ctMs(2026, 10, 21, 16, 30)); !strings.Contains(got, "CLOSED at 16:00 CT (next close 16:00 CT)") {
		t.Fatalf("16:30 CT must be CLOSED at 16:00 CT; got %q", got)
	}

	// 18:00 CT — the evening session is open (closes 16:00 the NEXT day).
	if got := formingBarLine("1d", tfData, ctMs(2026, 10, 21, 18, 0)); !strings.Contains(got, "FORMING (closes 16:00 CT)") {
		t.Fatalf("18:00 CT must be FORMING; got %q", got)
	}
}

// TestFormatMarketData_HeadlinePrimaryTF pins the m134-b fix: the headline EMA
// values carry the primary timeframe — "current_ema50(5m)", not an anonymous
// "current_ema50". The legacy (empty primary TF) label is unchanged, so the
// existing emaperiods pins stay green.
func TestFormatMarketData_HeadlinePrimaryTF(t *testing.T) {
	eng := NewStrategyEngine(&store.StrategyConfig{
		Indicators: store.IndicatorConfig{
			EnableEMA: true,
			Klines:    store.KlineConfig{PrimaryTimeframe: "5m"},
		},
	})

	out := eng.formatMarketData(&market.Data{
		Symbol: "MNQ", CurrentPrice: 30000,
		CurrentEMAByPeriod: map[int]float64{50: 29950.5, 200: 29800.25},
	})
	for _, want := range []string{"current_ema50(5m) = 29950.500", "current_ema200(5m) = 29800.250"} {
		if !strings.Contains(out, want) {
			t.Fatalf("headline must emit %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "current_ema50 =") || strings.Contains(out, "current_ema200 =") {
		t.Fatalf("headline must carry the (5m) suffix; got:\n%s", out)
	}

	// Legacy fallback with a primary TF: the 20-period line is labelled too.
	legacy := eng.formatMarketData(&market.Data{Symbol: "MNQ", CurrentPrice: 30000, CurrentEMA20: 29980})
	if !strings.Contains(legacy, "current_ema20(5m) = 29980.000") {
		t.Fatalf("legacy current_ema20 must carry (5m); got:\n%s", legacy)
	}
}

// TestFetchMarketDataWithRetry pins the m134-c retry at the production call
// site: one transient miss is retried once. The provider fails the primary
// timeframe on the first call and succeeds on the second, so the function must
// return data and the provider must have been called exactly twice.
func TestFetchMarketDataWithRetry(t *testing.T) {
	calls := 0
	orig := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(symbol, timeframe string, count int) []market.Kline {
		if timeframe != "5m" {
			return nil
		}
		calls++
		if calls == 1 {
			return nil // first attempt: empty cache (the 59076/59600 shape)
		}
		return []market.Kline{
			{OpenTime: ctMs(2026, 10, 21, 9, 55), CloseTime: ctMs(2026, 10, 21, 9, 59), Open: 29900, High: 29950, Low: 29890, Close: 29920},
			{OpenTime: ctMs(2026, 10, 21, 10, 0), CloseTime: ctMs(2026, 10, 21, 10, 4), Open: 29920, High: 29980, Low: 29910, Close: 29960},
		}
	}
	defer func() { market.FuturesBarsProvider = orig }()

	data, err := fetchMarketDataWithRetry("MNQ", "", []string{"5m"}, "5m", 30, market.IndicatorPeriods{})
	if err != nil {
		t.Fatalf("retry must succeed on the second attempt: %v", err)
	}
	if data == nil || data.CurrentPrice == 0 {
		t.Fatalf("retry must return market data")
	}
	if calls != 2 {
		t.Fatalf("expected exactly 2 fetch attempts (one retry), got %d", calls)
	}
}

// TestBuildUserPrompt_MissingMarketBlockLine pins the m134-c explicit line: a
// candidate coin with no market data in the map renders "market data
// unavailable", never a silent skip (the 59076/59600 missing-block shape).
func TestBuildUserPrompt_MissingMarketBlockLine(t *testing.T) {
	eng := NewStrategyEngine(&store.StrategyConfig{})
	ctx := &Context{
		CurrentTime:    "2026-10-21 10:00 CT",
		CallCount:      1,
		CandidateCoins: []CandidateCoin{{Symbol: "MNQ"}},
	}
	// Account equity non-zero so the balance ratio renders as a number, not NaN.
	ctx.Account.TotalEquity = 70000
	ctx.Account.AvailableBalance = 70000

	out := eng.BuildUserPrompt(ctx)
	if !strings.Contains(out, "market data unavailable") {
		t.Fatalf("a candidate with no market data must render an explicit unavailable line; got:\n%s", out)
	}
	if !strings.Contains(out, "do NOT infer prices") {
		t.Fatalf("the unavailable line must tell the model not to infer prices; got:\n%s", out)
	}
}
