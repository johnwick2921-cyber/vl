package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"vl/store"

	"vl/logger"
	"vl/market"
	"vl/provider/alpaca"
	"vl/provider/twelvedata"
	"vl/trader"

	"github.com/gin-gonic/gin"
)

func resolveKlinesLimit(exchange, limitStr string) int {
	const (
		defaultLimit     = 1000
		ntKlinesMaxLimit = 20000 // bounded by store retention and payload size
	)
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		return defaultLimit
	}
	if strings.EqualFold(strings.TrimSpace(exchange), "ninjatrader") {
		if limit > ntKlinesMaxLimit {
			return ntKlinesMaxLimit
		}
		return limit
	}
	return limit
}

// handleKlines K-line data (multiple providers: ninjatrader, alpaca, twelvedata)
func (s *Server) handleKlines(c *gin.Context) {
	// Get query parameters
	symbol := c.Query("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol parameter is required"})
		return
	}

	interval := c.DefaultQuery("interval", "5m")
	exchange := c.DefaultQuery("exchange", "ninjatrader")
	limit := resolveKlinesLimit(exchange, c.DefaultQuery("limit", "1000"))

	var klines []market.Kline
	var err error
	exchangeLower := strings.ToLower(exchange)

	// Route to appropriate data source based on exchange type
	switch exchangeLower {
	case "alpaca":
		// US Stocks via Alpaca
		klines, err = s.getKlinesFromAlpaca(symbol, interval, limit)
		if err != nil {
			SafeInternalError(c, "Get klines from Alpaca", err)
			return
		}
	case "forex", "metals":
		// Forex and Metals via Twelve Data
		klines, err = s.getKlinesFromTwelveData(symbol, interval, limit)
		if err != nil {
			SafeInternalError(c, "Get klines from TwelveData", err)
			return
		}
	case "ninjatrader":
		// CME futures (e.g. MNQ) via the live NT8 BarCache — the SAME feed the
		// kernel reads (market.FuturesBarsProvider), so the chart matches
		// decisions. No second source. Returns empty (HTTP 200, [])
		// when the provider is unbound or the cache is cold (e.g. NT8 closed).
		// W-ROLL-DAY-CHART: when a prior-contract segment was stitched, the
		// response is an envelope {klines, roll} so the chart can render the
		// derived + basis-adjusted segment and its legend; otherwise the bare
		// array (byte-identical to the pre-wave wire).
		var roll *rollStitchInfo
		klines, roll = s.getKlinesFromNinjaTrader(symbol, interval, limit)
		if roll != nil {
			c.JSON(http.StatusOK, gin.H{"klines": klines, "roll": roll})
			return
		}
	}

	c.JSON(http.StatusOK, klines)
}

// getKlinesFromAlpaca fetches kline data from Alpaca API for US stocks
func (s *Server) getKlinesFromAlpaca(symbol, interval string, limit int) ([]market.Kline, error) {
	// Create Alpaca client
	client := alpaca.NewClient()

	// Map interval to Alpaca timeframe format
	timeframe := alpaca.MapTimeframe(interval)

	// Fetch bars from Alpaca
	ctx := context.Background()
	bars, err := client.GetBars(ctx, symbol, timeframe, limit)
	if err != nil {
		return nil, fmt.Errorf("alpaca API error: %w", err)
	}

	// Convert Alpaca bars to market.Kline format
	klines := make([]market.Kline, len(bars))
	for i, bar := range bars {
		klines[i] = market.Kline{
			OpenTime:    bar.Timestamp.UnixMilli(),
			Open:        bar.Open,
			High:        bar.High,
			Low:         bar.Low,
			Close:       bar.Close,
			Volume:      float64(bar.Volume),             // share count
			QuoteVolume: float64(bar.Volume) * bar.Close, // turnover = shares * close price (USD)
			CloseTime:   bar.Timestamp.UnixMilli(),
		}
	}

	return klines, nil
}

// getKlinesFromTwelveData fetches kline data from Twelve Data API for forex and metals
func (s *Server) getKlinesFromTwelveData(symbol, interval string, limit int) ([]market.Kline, error) {
	// Create Twelve Data client
	client := twelvedata.NewClient()

	// Map interval to Twelve Data timeframe format
	timeframe := twelvedata.MapTimeframe(interval)

	// Fetch time series from Twelve Data
	ctx := context.Background()
	result, err := client.GetTimeSeries(ctx, symbol, timeframe, limit)
	if err != nil {
		return nil, fmt.Errorf("twelvedata API error: %w", err)
	}

	// Convert Twelve Data bars to market.Kline format
	// Note: Twelve Data returns bars in reverse order (newest first)
	klines := make([]market.Kline, len(result.Values))
	for i, bar := range result.Values {
		open, high, low, close, volume, timestamp, err := twelvedata.ParseBar(bar)
		if err != nil {
			logger.Warnf("⚠️ Failed to parse TwelveData bar: %v", err)
			continue
		}

		// Reverse order: put oldest first
		idx := len(result.Values) - 1 - i
		klines[idx] = market.Kline{
			OpenTime:  timestamp,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			CloseTime: timestamp,
		}
	}

	return klines, nil
}

// getKlinesFromNinjaTrader reads CME futures OHLCV from the live NT8 BarCache
// via the market.FuturesBarsProvider hook (bound at TCP-server startup in
// trader/ninjatrader/transport.go). This is the SAME feed the kernel uses for
// decisions, so chart candles and AI decisions never diverge.
//
// Returns an empty slice (NOT nil, NOT an error) when:
//   - the symbol is not a CME futures root (guards against a crypto symbol
//     accidentally hitting this branch), or
//   - the provider is unbound (no NT8 trader loaded yet), or
//   - the cache is cold (NT8 disconnected / market closed → no bars yet).
//
// An empty 200 lets the chart render "no data" gracefully rather than 500ing
// or falling back to crypto. The BarCache is only auto-subscribed for the
// timeframes in provider/ninjatrader defaultAutoBarsTimeframes (5m/15m/1h), so
// other intervals legitimately return empty until a strategy subscribes them.
func (s *Server) getKlinesFromNinjaTrader(symbol, interval string, limit int) ([]market.Kline, *rollStitchInfo) {
	if !market.IsCMEFuturesSymbol(symbol) {
		logger.Warnf("⚠️ klines: non-futures symbol %q requested on ninjatrader exchange", symbol)
		return []market.Kline{}, nil
	}
	provider := market.FuturesBarsProvider
	if provider == nil {
		logger.Warnf("⚠️ klines: FuturesBarsProvider unbound (no NT8 trader loaded); returning empty for %s", symbol)
		return []market.Kline{}, nil
	}
	klines := provider(symbol, interval, limit)
	if klines == nil {
		klines = []market.Kline{}
	}
	// F1 (2026-09-14) — DASHBOARD DEPTH. The ring caps at
	// DefaultBarCacheMaxBars (2,500) per (symbol, timeframe), so after a
	// restart — or any ask past the ceiling — the chart was shallow although
	// the store held the bars. When the ask exceeds what the ring served,
	// splice the CURRENT contract's stored bars onto the older end. The
	// contract is the store's own fallback (newest usable bar) — the same
	// shadow the trader's currentContract uses when no ACK has arrived. An
	// unnamed contract or a failed store read degrades to the ring alone: the
	// chart may be shallow, it is never mixed-scale (A10/A24, roll wave).
	current := ""
	if s.store != nil && len(klines) < limit {
		if contract, ok := s.store.BarHistory().LatestContract(symbol); ok {
			current = contract
			klines = trader.BarsWithStoreDepthDisplay(klines, s.store, contract, symbol, interval, limit, time.Now())
		}
	}
	// 101 D2 (owner ruling 2026-09-16) — THE CHART ACROSS THE ROLL. When the
	// current contract's ring+store still fall short of the ask, prior
	// contracts fill the time STRICTLY BEFORE the current contract's first
	// live row: one continuous series, one visible basis step at the real
	// roll, every kline labelled with its contract, nothing back-adjusted
	// (research law). DISPLAY ONLY — the kernel/levels/arm readers are
	// contract-scoped and untouched (E4).
	if chartAcrossRoll && s.store != nil && current != "" && len(klines) < limit {
		if chartRollStitchDerive {
			var roll *rollStitchInfo
			klines, roll = klinesAcrossRollDerived(klines, s.store.BarHistory(), current, symbol, interval, limit)
			if roll != nil {
				return klines, roll
			}
		} else {
			klines = klinesAcrossRoll(klines, s.store.BarHistory(), current, symbol, interval, limit)
		}
	}
	// F1.1 (2026-09-14) — COARSE-TF AGGREGATION. On a young contract NT8's
	// replay is deep on 1m/5m/15m/1h but nearly empty on 2h/4h/1d (measured
	// live 2026-09-14: the 4h ring held 10 bars while the 1h ring held 1,500,
	// so a 4h chart showed a handful of candles although weeks of 4h exist in
	// the finer rungs). When the series is still short of the ask, aggregate
	// the first finer ladder rung with depth into the requested TF — CLOSED
	// buckets, strictly older than the series' oldest — and prepend. The ring
	// is contract-pure (purged on roll), so the aggregate never mixes
	// contracts, and a forming bucket is never served.
	if len(klines) < limit {
		klines = klinesWithAggregatedDepth(klines, provider, symbol, interval, limit, time.Now())
	}
	return klines, nil
}

// klinesFinerRungFetch is how many bars the finer aggregation rung may fetch
// from the ring — comfortably past the 2,500-bar ring ceiling.
const klinesFinerRungFetch = 5000

// klinesWithAggregatedDepth extends a thin coarse-TF series backwards by
// aggregating a finer ladder rung that has depth. PURE so a pin drives it.
func klinesWithAggregatedDepth(base []market.Kline, provider func(string, string, int) []market.Kline, symbol, tf string, limit int, now time.Time) []market.Kline {
	mins := market.TFMinutes(tf)
	if mins == 0 || limit <= 0 {
		return base
	}
	span := int64(mins) * 60000
	nowMs := now.UnixMilli()
	// An EMPTY native ring is not a dead chart: on a young contract NT8 can
	// return zero bars for the requested TF itself (measured 2026-09-14: 2h
	// and 4h EMPTY on re-subscribe) while a finer rung is deep. Aggregating
	// the finer LIVE rung is contract-pure ring data — not a store substitute
	// — so an empty base is aggregated, never left empty when a finer rung can
	// answer (closed buckets only).
	oldest := int64(0)
	haveBase := len(base) > 0
	if haveBase {
		oldest = base[0].OpenTime
	}
	for _, finer := range market.LadderFor(tf) {
		if finer == tf {
			continue
		}
		finerMins := market.TFMinutes(finer)
		if finerMins == 0 || finerMins >= mins {
			continue
		}
		raw := provider(symbol, finer, klinesFinerRungFetch)
		if len(raw) == 0 {
			continue
		}
		agg := market.AggregateToTF(raw, finerMins, mins)
		older := make([]market.Kline, 0, len(agg))
		for _, k := range agg {
			// Closed buckets only; when the base exists, only buckets strictly
			// older than its oldest bar — a forming bucket is never served and
			// a bucket the ring already covers is never duplicated.
			if k.OpenTime+span <= nowMs && (!haveBase || k.OpenTime < oldest) {
				older = append(older, k)
			}
		}
		if len(older) == 0 {
			continue
		}
		out := append(older, base...)
		if len(out) > limit {
			out = out[len(out)-limit:]
		}
		return out
	}
	return base
}

// handleSymbols returns available symbols for a given exchange
func (s *Server) handleSymbols(c *gin.Context) {
	// The futures venues have no per-symbol catalog endpoint (the strategy
	// lists MNQ).
	c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported exchange for symbol listing"})
}

// chartAcrossRoll is the D2 flag, default ON per the owner's ruling. Set
// VL_CHART_ACROSS_ROLL=off to serve the current contract only.
var chartAcrossRollRaw = os.Getenv("VL_CHART_ACROSS_ROLL")
var chartAcrossRoll = strings.ToLower(strings.TrimSpace(chartAcrossRollRaw)) != "off"

// chartAcrossRollResolved renders the boot-line value and the env SOURCE it
// was read from (A11: READ, never a guess).
func chartAcrossRollResolved(on bool, src string) string {
	if on {
		return "on[O]"
	}
	return "off[env:" + src + "]"
}

// ChartAcrossRollResolved is READ onto the boot line (A11).
func ChartAcrossRollResolved() string {
	src := "default"
	if chartAcrossRollRaw != "" {
		src = "VL"
	}
	return chartAcrossRollResolved(chartAcrossRoll, src)
}

// dropCurrentRowsBefore returns base without the rows whose open time is
// older than boundary (the current contract's first LIVE bar). Order is kept.
func dropCurrentRowsBefore(base []market.Kline, boundary int64) []market.Kline {
	out := make([]market.Kline, 0, len(base))
	for _, k := range base {
		if k.OpenTime < boundary {
			continue
		}
		out = append(out, k)
	}
	return out
}

// klinesAcrossRoll prepends prior-contract rows older than the current
// contract's first live row, labels every kline, and caps at limit. PURE over
// its inputs so a pin drives it.
func klinesAcrossRoll(base []market.Kline, bh *store.BarHistoryStore, current, symbol, tf string, limit int) []market.Kline {
	if bh == nil || limit <= 0 {
		return base
	}
	for i := range base {
		if base[i].Contract == "" {
			base[i].Contract = current
		}
	}
	boundary, ok, err := bh.FirstLiveOn(symbol, tf, current)
	if err != nil || !ok {
		return base
	}
	// W-CHART-ROLL-HOLE (2026-09-17) — THE ROLL IS A TIME SPLIT AND THE
	// BOUNDARY NEVER MOVES. t < boundary → prior-contract rows; t ≥ boundary →
	// current-contract rows. A current-contract row OLDER than its first live
	// bar is a stray history survivor: NT8 served ~2000 bars of "MNQ 12-26" at
	// subscribe, the bars PK (symbol, tf, open_time_ms) has no contract, and
	// where 09-26 held no row (holidays, Sunday evenings, NT8-off windows) the
	// 12-26 row landed. This used to pull the boundary back to the OLDEST such
	// stray ("nothing older than the series' own oldest bar may overlap it"),
	// and PriorContractBarsBefore then excluded every 09-26 row after it — the
	// live store lost 09-10 22:10 → 09-14 10:00 CT (three trading days) and
	// drew 13 December candles, 292 points up, in the gap. Dropping the strays
	// leaves their slots as holes in the prior series; a hole stays a hole
	// (the store reader's own ruling) — a candle of the next contract's price
	// space in the middle of the prior series is never the answer.
	if dropped := dropCurrentRowsBefore(base, boundary); len(dropped) != len(base) {
		base = dropped
	}
	need := limit - len(base)
	if need <= 0 {
		return base
	}
	rows, err := bh.PriorContractBarsBefore(symbol, tf, current, boundary, need)
	if err != nil || len(rows) == 0 {
		return base
	}
	prior := make([]market.Kline, 0, len(rows))
	durMs := int64(market.TFMinutes(tf)) * 60_000
	for _, r := range rows {
		k := market.Kline{OpenTime: r.OpenTimeMs, Open: r.O, High: r.H, Low: r.L, Close: r.C, Volume: r.V, Contract: r.Contract}
		if durMs > 0 {
			k.CloseTime = r.OpenTimeMs + durMs - 1
		}
		prior = append(prior, k)
	}
	out := append(prior, base...)
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}
