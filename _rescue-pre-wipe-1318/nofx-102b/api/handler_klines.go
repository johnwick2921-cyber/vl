package api

import (
	"context"
	"fmt"
	"net/http"
	"nofx/store"
	"os"
	"strconv"
	"strings"
	"time"

	"nofx/logger"
	"nofx/market"
	"nofx/provider/alpaca"
	"nofx/provider/coinank/coinank_api"
	"nofx/provider/coinank/coinank_enum"
	"nofx/provider/hyperliquid"
	"nofx/provider/twelvedata"
	"nofx/trader"

	"github.com/gin-gonic/gin"
)

// resolveKlinesLimit parses and clamps the klines `limit` query per exchange.
//
// Coinank (and the other external providers routed through it) caps a request
// at 1500 klines. The ninjatrader path serves from the live ring PLUS our own
// bars store (F1, 2026-09-14 — the dashboard asks 5,000 so the store splice has
// room past the 2,500-bar ring ceiling), so it keeps its own ceiling instead of
// inheriting the Coinank one. A bad or missing value falls back to 1000.
func resolveKlinesLimit(exchange, limitStr string) int {
	const (
		defaultLimit     = 1000
		coinankMax       = 1500
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
	if limit > coinankMax {
		return coinankMax
	}
	return limit
}

// handleKlines K-line data (supports multiple exchanges via coinank)
func (s *Server) handleKlines(c *gin.Context) {
	// Get query parameters
	symbol := c.Query("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol parameter is required"})
		return
	}

	interval := c.DefaultQuery("interval", "5m")
	exchange := c.DefaultQuery("exchange", "binance") // Default to binance for backward compatibility
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
	case "hyperliquid", "hyperliquid-xyz", "xyz":
		// Hyperliquid native API - supports both crypto perps and stock perps (xyz dex)
		klines, err = s.getKlinesFromHyperliquid(symbol, interval, limit)
		if err != nil {
			SafeInternalError(c, "Get klines from Hyperliquid", err)
			return
		}
	case "ninjatrader":
		// CME futures (e.g. MNQ) via the live NT8 BarCache — the SAME feed the
		// kernel reads (market.FuturesBarsProvider), so the chart matches
		// decisions. No CoinAnk, no second source. Returns empty (HTTP 200, [])
		// when the provider is unbound or the cache is cold (e.g. NT8 closed)
		// instead of falling through to crypto.
		klines = s.getKlinesFromNinjaTrader(symbol, interval, limit)
	default:
		// Crypto exchanges via CoinAnk
		symbol = market.Normalize(symbol)
		klines, err = s.getKlinesFromCoinank(symbol, interval, exchange, limit)
		if err != nil {
			SafeInternalError(c, "Get klines from CoinAnk", err)
			return
		}
	}

	c.JSON(http.StatusOK, klines)
}

// getKlinesFromCoinank fetches kline data from coinank free/open API for multiple exchanges
func (s *Server) getKlinesFromCoinank(symbol, interval, exchange string, limit int) ([]market.Kline, error) {
	// Map exchange string to coinank enum
	var coinankExchange coinank_enum.Exchange
	switch strings.ToLower(exchange) {
	case "binance":
		coinankExchange = coinank_enum.Binance
	case "bybit":
		coinankExchange = coinank_enum.Bybit
	case "okx":
		coinankExchange = coinank_enum.Okex
	case "bitget":
		coinankExchange = coinank_enum.Bitget
	case "gate":
		coinankExchange = coinank_enum.Gate
	case "aster":
		coinankExchange = coinank_enum.Aster
	case "lighter":
		// Lighter doesn't have direct CoinAnk support, use Binance data as fallback
		coinankExchange = coinank_enum.Binance
	case "kucoin":
		// KuCoin doesn't have direct CoinAnk support, use Binance data as fallback
		coinankExchange = coinank_enum.Binance
	default:
		// For any unknown exchange, default to Binance
		logger.Warnf("⚠️ Unknown exchange '%s', defaulting to Binance for CoinAnk", exchange)
		coinankExchange = coinank_enum.Binance
	}

	// Map interval string to coinank enum
	var coinankInterval coinank_enum.Interval
	switch interval {
	case "1s":
		coinankInterval = coinank_enum.Second1
	case "5s":
		coinankInterval = coinank_enum.Second5
	case "10s":
		coinankInterval = coinank_enum.Second10
	case "30s":
		coinankInterval = coinank_enum.Second30
	case "1m":
		coinankInterval = coinank_enum.Minute1
	case "3m":
		coinankInterval = coinank_enum.Minute3
	case "5m":
		coinankInterval = coinank_enum.Minute5
	case "10m":
		coinankInterval = coinank_enum.Minute10
	case "15m":
		coinankInterval = coinank_enum.Minute15
	case "30m":
		coinankInterval = coinank_enum.Minute30
	case "1h":
		coinankInterval = coinank_enum.Hour1
	case "2h":
		coinankInterval = coinank_enum.Hour2
	case "4h":
		coinankInterval = coinank_enum.Hour4
	case "6h":
		coinankInterval = coinank_enum.Hour6
	case "8h":
		coinankInterval = coinank_enum.Hour8
	case "12h":
		coinankInterval = coinank_enum.Hour12
	case "1d":
		coinankInterval = coinank_enum.Day1
	case "3d":
		coinankInterval = coinank_enum.Day3
	case "1w":
		coinankInterval = coinank_enum.Week1
	case "1M":
		coinankInterval = coinank_enum.Month1
	default:
		return nil, fmt.Errorf("unsupported interval for coinank: %s", interval)
	}

	// Convert symbol format for different exchanges
	// OKX uses "BTC-USDT-SWAP" format instead of "BTCUSDT"
	apiSymbol := symbol
	if coinankExchange == coinank_enum.Okex {
		// Convert BTCUSDT -> BTC-USDT-SWAP
		if strings.HasSuffix(symbol, "USDT") {
			base := strings.TrimSuffix(symbol, "USDT")
			apiSymbol = fmt.Sprintf("%s-USDT-SWAP", base)
		}
	}

	// Call coinank free/open API (no authentication required)
	ctx := context.Background()
	ts := time.Now().UnixMilli()
	// Use "To" side to search backward from current time (get historical klines)
	coinankKlines, err := coinank_api.Kline(ctx, apiSymbol, coinankExchange, ts, coinank_enum.To, limit, coinankInterval)
	if err != nil {
		// Free API doesn't support all exchanges (e.g., OKX, Bitget)
		// Fallback to Binance data as reference
		if coinankExchange != coinank_enum.Binance {
			logger.Warnf("⚠️ CoinAnk free API doesn't support %s, falling back to Binance data", coinankExchange)
			coinankKlines, err = coinank_api.Kline(ctx, symbol, coinank_enum.Binance, ts, coinank_enum.To, limit, coinankInterval)
			if err != nil {
				return nil, fmt.Errorf("coinank API error (fallback): %w", err)
			}
		} else {
			return nil, fmt.Errorf("coinank API error: %w", err)
		}
	}

	// Convert coinank kline format to market.Kline format
	// Coinank: Volume = BTC quantity, Quantity = USDT turnover
	klines := make([]market.Kline, len(coinankKlines))
	for i, ck := range coinankKlines {
		klines[i] = market.Kline{
			OpenTime:    ck.StartTime,
			Open:        ck.Open,
			High:        ck.High,
			Low:         ck.Low,
			Close:       ck.Close,
			Volume:      ck.Volume,   // BTC quantity
			QuoteVolume: ck.Quantity, // USDT turnover
			CloseTime:   ck.EndTime,
		}
	}

	return klines, nil
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

// getKlinesFromHyperliquid fetches kline data from Hyperliquid API
// Supports both crypto perps (default dex) and stock perps/forex/commodities (xyz dex)
func (s *Server) getKlinesFromHyperliquid(symbol, interval string, limit int) ([]market.Kline, error) {
	// Create Hyperliquid client
	client := hyperliquid.NewClient()

	// Map interval to Hyperliquid format
	timeframe := hyperliquid.MapTimeframe(interval)

	// Fetch candles from Hyperliquid
	// FormatCoinForAPI will automatically add xyz: prefix for stock perps
	ctx := context.Background()
	candles, err := client.GetCandles(ctx, symbol, timeframe, limit)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid API error: %w", err)
	}

	// Convert Hyperliquid candles to market.Kline format
	klines := make([]market.Kline, len(candles))
	for i, candle := range candles {
		open, _ := strconv.ParseFloat(candle.Open, 64)
		high, _ := strconv.ParseFloat(candle.High, 64)
		low, _ := strconv.ParseFloat(candle.Low, 64)
		close, _ := strconv.ParseFloat(candle.Close, 64)
		volume, _ := strconv.ParseFloat(candle.Volume, 64)

		klines[i] = market.Kline{
			OpenTime:    candle.OpenTime,
			Open:        open,
			High:        high,
			Low:         low,
			Close:       close,
			Volume:      volume,         // contract quantity
			QuoteVolume: volume * close, // turnover (USD)
			CloseTime:   candle.CloseTime,
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
func (s *Server) getKlinesFromNinjaTrader(symbol, interval string, limit int) []market.Kline {
	if !market.IsCMEFuturesSymbol(symbol) {
		logger.Warnf("⚠️ klines: non-futures symbol %q requested on ninjatrader exchange", symbol)
		return []market.Kline{}
	}
	provider := market.FuturesBarsProvider
	if provider == nil {
		logger.Warnf("⚠️ klines: FuturesBarsProvider unbound (no NT8 trader loaded); returning empty for %s", symbol)
		return []market.Kline{}
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
		klines = klinesAcrossRoll(klines, s.store.BarHistory(), current, symbol, interval, limit)
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
	return klines
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
	exchange := c.DefaultQuery("exchange", "hyperliquid")

	type SymbolInfo struct {
		Symbol      string `json:"symbol"`
		Name        string `json:"name"`
		Category    string `json:"category"` // crypto, stock, forex, commodity, index
		MaxLeverage int    `json:"maxLeverage,omitempty"`
	}

	var symbols []SymbolInfo

	switch strings.ToLower(exchange) {
	case "hyperliquid", "hyperliquid-xyz", "xyz":
		// Fetch symbols from Hyperliquid
		client := hyperliquid.NewClient()
		ctx := context.Background()

		// Get crypto perps from default dex
		if exchange == "hyperliquid" || exchange == "hyperliquid-xyz" {
			mids, err := client.GetAllMids(ctx)
			if err == nil {
				for symbol := range mids {
					// Skip spot tokens (start with @)
					if strings.HasPrefix(symbol, "@") {
						continue
					}
					symbols = append(symbols, SymbolInfo{
						Symbol:   symbol,
						Name:     symbol,
						Category: "crypto",
					})
				}
			}
		}

		// Get xyz dex symbols (stocks, forex, commodities)
		xyzMids, err := client.GetAllMidsXYZ(ctx)
		if err == nil {
			for symbol := range xyzMids {
				// Remove xyz: prefix for display
				displaySymbol := strings.TrimPrefix(symbol, "xyz:")
				category := "stock"
				if displaySymbol == "GOLD" || displaySymbol == "SILVER" {
					category = "commodity"
				} else if displaySymbol == "EUR" || displaySymbol == "JPY" {
					category = "forex"
				} else if displaySymbol == "XYZ100" {
					category = "index"
				}
				symbols = append(symbols, SymbolInfo{
					Symbol:   displaySymbol,
					Name:     displaySymbol,
					Category: category,
				})
			}
		}

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported exchange for symbol listing"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"exchange": exchange,
		"symbols":  symbols,
		"count":    len(symbols),
	})
}

// chartAcrossRoll is the D2 flag, default ON per the owner's ruling. Set
// NOFX_CHART_ACROSS_ROLL=off to serve the current contract only.
var chartAcrossRoll = strings.ToLower(strings.TrimSpace(os.Getenv("NOFX_CHART_ACROSS_ROLL"))) != "off"

// ChartAcrossRollResolved is READ onto the boot line (A11).
func ChartAcrossRollResolved() string {
	if chartAcrossRoll {
		return "on[O]"
	}
	return "off[env]"
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
	// nothing older than the series' own oldest bar may overlap it
	if len(base) > 0 && base[0].OpenTime < boundary {
		boundary = base[0].OpenTime
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
