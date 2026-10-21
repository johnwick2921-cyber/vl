package kernel

import (
	"strings"
	"testing"
	"time"

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

	// 16:30 CT — the daily maintenance break: CLOSED, naming the reason and the
	// reopen (m150-2), never an invented close. (2026-10-21 is a Wednesday.)
	if got := formingBarLine("1d", tfData, ctMs(2026, 10, 21, 16, 30)); !strings.Contains(got, "CLOSED (daily break)") || !strings.Contains(got, "reopens Wed 17:00 CT") {
		t.Fatalf("16:30 CT must be CLOSED (daily break) with a Wed 17:00 reopen; got %q", got)
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

// TestDailySessionBarLine_ShortenedClose (m150-1) — a half day closes at the
// calendar's shortened close (12:15 CT), never the hard-coded 16:00.
func TestDailySessionBarLine_ShortenedClose(t *testing.T) {
	tfData := oneDummy1D()

	// 2026-11-27 (day after Thanksgiving): shortened, close 12:15 CT.
	if got := formingBarLine("1d", tfData, ctMs(2026, 11, 27, 10, 0)); !strings.Contains(got, "FORMING (closes 12:15 CT)") {
		t.Fatalf("11-27 10:00 must be FORMING closing 12:15 CT; got %q", got)
	}
	if got := formingBarLine("1d", tfData, ctMs(2026, 11, 27, 13, 0)); !strings.Contains(got, "CLOSED (early close 12:15 CT)") {
		t.Fatalf("11-27 13:00 must be CLOSED (early close 12:15 CT); got %q", got)
	}
	// 2026-12-24 (Christmas Eve): shortened, close 12:15 CT.
	if got := formingBarLine("1d", tfData, ctMs(2026, 12, 24, 10, 0)); !strings.Contains(got, "FORMING (closes 12:15 CT)") {
		t.Fatalf("12-24 10:00 must be FORMING closing 12:15 CT; got %q", got)
	}
}

// TestDailySessionBarLine_ClosedNamesReasonAndReopen (m150-2) — weekend/holiday
// CLOSED lines name the reason (CMEClosedReason) and the reopen (NextCMEOpen),
// never an invented close.
func TestDailySessionBarLine_ClosedNamesReasonAndReopen(t *testing.T) {
	tfData := oneDummy1D()

	// Saturday 16:30 CT — weekend, reopens Sunday 17:00 CT.
	if got := formingBarLine("1d", tfData, ctMs(2026, 10, 24, 16, 30)); !strings.Contains(got, "CLOSED (weekend)") || !strings.Contains(got, "reopens Sun 17:00 CT") {
		t.Fatalf("Sat 16:30 must be CLOSED (weekend) reopening Sun 17:00; got %q", got)
	}
	// Sunday 16:30 CT — still before the 17:00 reopen.
	if got := formingBarLine("1d", tfData, ctMs(2026, 10, 25, 16, 30)); !strings.Contains(got, "CLOSED (weekend)") || !strings.Contains(got, "reopens Sun 17:00 CT") {
		t.Fatalf("Sun 16:30 must be CLOSED (weekend) reopening Sun 17:00; got %q", got)
	}
	// Christmas Day 2026 — a full holiday.
	if got := formingBarLine("1d", tfData, ctMs(2026, 12, 25, 10, 0)); !strings.Contains(got, "CLOSED (holiday)") || !strings.Contains(got, "reopens") {
		t.Fatalf("12-25 must be CLOSED (holiday) with a reopen; got %q", got)
	}
}

// TestDailySessionBarLine_DST (m150-3, comment corrected m162) — the 1d label's
// close reads 16:00 CT wall clock across both 2026 DST transition Sundays. This
// pin documents the WALL-CLOCK invariant (the close is built by time.Date(…,16),
// never by duration arithmetic), so a duration mutant in the LABEL path cannot
// shift it. The transition days themselves are the subject of
// TestCMESessionDayStart_DST below, which is the pin a duration mutant actually
// breaks. The transitions fall on Sundays, so 10:00 is weekend-closed and 18:00
// is the re-opened session.
func TestDailySessionBarLine_DST(t *testing.T) {
	tfData := oneDummy1D()

	// Spring-forward: 2026-03-08 (Sunday). 10:00 = weekend-closed; 18:00 = open.
	if got := formingBarLine("1d", tfData, ctMs(2026, 3, 8, 10, 0)); !strings.Contains(got, "CLOSED (weekend)") || !strings.Contains(got, "reopens Sun 17:00 CT") {
		t.Fatalf("03-08 10:00 must be CLOSED (weekend) reopening Sun 17:00; got %q", got)
	}
	if got := formingBarLine("1d", tfData, ctMs(2026, 3, 8, 18, 0)); !strings.Contains(got, "closes 16:00 CT") {
		t.Fatalf("03-08 18:00 must close 16:00 CT; got %q", got)
	}

	// Fall-back: 2026-11-01 (Sunday). 10:00 = weekend-closed; 18:00 = open.
	if got := formingBarLine("1d", tfData, ctMs(2026, 11, 1, 10, 0)); !strings.Contains(got, "CLOSED (weekend)") || !strings.Contains(got, "reopens Sun 17:00 CT") {
		t.Fatalf("11-01 10:00 must be CLOSED (weekend) reopening Sun 17:00; got %q", got)
	}
	if got := formingBarLine("1d", tfData, ctMs(2026, 11, 1, 18, 0)); !strings.Contains(got, "closes 16:00 CT") {
		t.Fatalf("11-01 18:00 must close 16:00 CT; got %q", got)
	}
}

// TestCMESessionDayStart_DST (m162) — the pin a DURATION mutant actually breaks.
// CMESessionDayStart moves a 17:00 CT boundary back one CALENDAR day via
// AddDate(0,0,-1). A mutant that swaps in Add(-24h) lands on the wrong wall
// clock at both transition instants (the 24h interval crosses the 02:00
// transition), and this pin goes RED in both.
func TestCMESessionDayStart_DST(t *testing.T) {
	t.Run("spring-forward", func(t *testing.T) {
		// 2026-03-08 is a 23h day: at 08:00 CT the session start is 03-07 17:00
		// CT. Add(-24h) from the 03-08 17:00 boundary lands at 03-07 16:00 CST
		// (it crosses the 02:00→03:00 jump) — one hour early.
		if got := CMESessionDayStart(time.UnixMilli(ctMs(2026, 3, 8, 8, 0))).UnixMilli(); got != ctMs(2026, 3, 7, 17, 0) {
			t.Fatalf("session start = %s, want 2026-03-07 17:00 CT", time.UnixMilli(got).In(CTLocation()))
		}
	})
	t.Run("fall-back", func(t *testing.T) {
		// 2026-11-01 is a 25h day: Add(-24h) from the 11-01 17:00 boundary lands
		// at 10-31 18:00 CDT — one hour late.
		if got := CMESessionDayStart(time.UnixMilli(ctMs(2026, 11, 1, 8, 0))).UnixMilli(); got != ctMs(2026, 10, 31, 17, 0) {
			t.Fatalf("session start = %s, want 2026-10-31 17:00 CT", time.UnixMilli(got).In(CTLocation()))
		}
	})
}

// TestDailySessionClose_WallClock (m150-3) — dailySessionClose returns 16:00 CT
// wall clock on the session's close day. It builds the close with
// time.Date(…,16) from the session's close day, so no duration arithmetic is
// involved; this pin documents that invariant (it is NOT a mutant-catcher —
// no CME session spans a DST transition, so a 23h shortcut would also be
// correct here).
func TestDailySessionClose_WallClock(t *testing.T) {
	// Sunday 03-08 18:00 CT (post spring-forward): session closes Mon 03-09 16:00.
	c := dailySessionClose(time.UnixMilli(ctMs(2026, 3, 8, 18, 0)))
	ct := c.In(CTLocation())
	if ct.Hour() != 16 || ct.Minute() != 0 || ct.Day() != 9 {
		t.Fatalf("03-08 18:00 session must close 03-09 16:00 CT, got %v", ct)
	}
	// Sunday 11-01 18:00 CT (post fall-back): session closes Mon 11-02 16:00.
	c2 := dailySessionClose(time.UnixMilli(ctMs(2026, 11, 1, 18, 0)))
	if ct2 := c2.In(CTLocation()); ct2.Hour() != 16 || ct2.Minute() != 0 || ct2.Day() != 2 {
		t.Fatalf("11-01 18:00 session must close 11-02 16:00 CT, got %v", ct2)
	}
}

// TestBuildUserPrompt_CandidateHeaderCountsShown (m150-4) — the "Candidate Coins
// (N)" header counts the coins ACTUALLY shown (positions excluded), and an open
// position whose market fetch failed is stated, never silently dropped.
func TestBuildUserPrompt_CandidateHeaderCountsShown(t *testing.T) {
	eng := NewStrategyEngine(&store.StrategyConfig{})
	ctx := &Context{
		CurrentTime:    "2026-10-21 10:00 CT",
		CallCount:      1,
		CandidateCoins: []CandidateCoin{{Symbol: "MNQ"}, {Symbol: "ES"}},
		Positions:      []PositionInfo{{Symbol: "MNQ"}},
	}
	ctx.Account.TotalEquity = 70000
	ctx.Account.AvailableBalance = 70000

	out := eng.BuildUserPrompt(ctx)
	// MNQ is a position (shown in the positions section, with the unavailable
	// line); ES is the only candidate → the header counts 1, not 0 and not 2.
	if !strings.Contains(out, "## Candidate Coins (1 coins)") {
		t.Fatalf("candidate header must count the coins shown; got:\n%s", out)
	}
	if !strings.Contains(out, "market data unavailable") {
		t.Fatalf("an open position with no market data must be stated; got:\n%s", out)
	}
}

// TestMarketDataUnavailableSkipsAI (m150-5) — no market data for the primary
// symbol → the AI is NEVER called and the cycle HOLDs with a named reason and a
// gate-block counter (fail closed; broker stops stay in place).
func TestMarketDataUnavailableSkipsAI(t *testing.T) {
	pinDecisionCycleNow(t)
	trader := "p27-nodata"
	ctx := &Context{
		TraderID:       trader,
		Account:        AccountInfo{TotalEquity: 60000},
		CandidateCoins: []CandidateCoin{{Symbol: "MNQ"}},
	}
	engine := &StrategyEngine{config: &store.StrategyConfig{RiskControl: store.RiskControlConfig{MaxPositions: 1}}}

	orig := market.FuturesBarsProvider
	market.FuturesBarsProvider = nil // no provider → the fetch cannot produce data
	defer func() { market.FuturesBarsProvider = orig }()

	never := &mockAIClient{replies: []string{"{"}} // a stray call would parse-fail, not panic
	fd, err := GetFullDecisionWithStrategy(ctx, never, engine, "futures")
	if err != nil {
		t.Fatalf("a market-data HOLD must not error: %v", err)
	}
	if fd == nil || fd.SkipReason != "market_data_unavailable" {
		t.Fatalf("want SkipReason market_data_unavailable, got %+v", fd)
	}
	if never.calls != 0 {
		t.Fatalf("the AI must not be called when the primary symbol has no data; got %d calls", never.calls)
	}
	if !gateBlockCountHas(t, trader, "market_data_unavailable") {
		t.Fatalf("market_data_unavailable HOLD not counted in the gate-block table")
	}
}

// TestFetchMarketDataWithRetry_SkipsDeterministicErrors (m150-6) — a stale fetch
// is deterministic (the same call returns the same error), so it must NOT be
// retried: exactly one provider call.
func TestFetchMarketDataWithRetry_SkipsDeterministicErrors(t *testing.T) {
	calls := 0
	orig := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(symbol, timeframe string, count int) []market.Kline {
		if timeframe != "5m" {
			return nil
		}
		calls++
		// 6 frozen, zero-volume bars → isStaleData confirms the freeze.
		bars := make([]market.Kline, 6)
		for i := range bars {
			bars[i] = market.Kline{OpenTime: ctMs(2026, 10, 21, 9, 50) + int64(i)*300_000, CloseTime: ctMs(2026, 10, 21, 9, 50) + int64(i)*300_000 + 299_000, Open: 100, High: 100, Low: 100, Close: 100}
		}
		return bars
	}
	defer func() { market.FuturesBarsProvider = orig }()

	_, err := fetchMarketDataWithRetry("MNQ", "", []string{"5m"}, "5m", 30, market.IndicatorPeriods{})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("want a stale error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("a stale fetch must NOT retry; got %d provider calls", calls)
	}
}
