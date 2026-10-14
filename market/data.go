package market

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"vl/logger"
)

// futuresOIFunding is the ONE place the CME futures path decides open interest
// and funding (W-NO-BINANCE A): ABSENT — OI nil, funding not known — because
// the futures path makes no external market-data call. OI and funding are
// crypto-perp feeds; there is no CME symbol behind them.
func futuresOIFunding() (oi *OIData, funding float64, fundingKnown bool) {
	return nil, 0, false
}

// marketRoute is where one market read for (symbol, venue) goes — the ONE
// decision GetWithExchange takes and the 📊 boot line READS (W-NO-BINANCE A).
type marketRoute int

const (
	routeCrypto       marketRoute = iota // the removed crypto providers’ klines + OI/funding (refused)
	routeFutures                         // NT8 bars; OI/funding absent (futuresOIFunding)
	routeRefusedVenue                    // the NinjaTrader venue with a non-CME symbol
)

// marketRouteFor decides the route. The NinjaTrader venue IS the futures
// path: a non-CME symbol there is REFUSED — it must never fall through to the
// crypto branch (the removed providers mapped unknown venues to a crypto exchange, and the
// crypto branch calls fapi OI/funding). Critic G1, fail-closed.
func marketRouteFor(symbol, exchange string) marketRoute {
	if IsCMEFuturesSymbol(symbol) {
		return routeFutures
	}
	if strings.EqualFold(strings.TrimSpace(exchange), "ninjatrader") {
		return routeRefusedVenue
	}
	return routeCrypto
}

// FuturesOIFundingBootLine READS what the market read does for OI and funding
// on the NinjaTrader venue, for the symbols the loaded NT8 traders trade:
// each goes through marketRouteFor and, on the futures route, through
// futuresOIFunding — the same two functions the read calls. No symbol → n/a.
func FuturesOIFundingBootLine(nt8Symbols ...string) string {
	if len(nt8Symbols) == 0 {
		return "oi/funding: n/a (no NinjaTrader trader loaded)"
	}
	external, refused := false, []string{}
	for _, s := range nt8Symbols {
		switch marketRouteFor(Normalize(s), "ninjatrader") {
		case routeFutures:
			if oi, _, known := futuresOIFunding(); oi != nil || known {
				external = true
			}
		case routeRefusedVenue:
			refused = append(refused, s)
		}
	}
	line := "oi/funding: n/a (no external market data on the futures path)"
	if external {
		line = "oi/funding: EXTERNAL (the futures path reads an external feed)"
	}
	if len(refused) > 0 {
		line += " · REFUSED on the NinjaTrader venue (non-CME symbol): " + strings.Join(refused, ",")
	}
	return line
}

// Get retrieves market data for the specified token (defaults to the NinjaTrader venue)
func Get(symbol string) (*Data, error) {
	return GetWithExchange(symbol, "ninjatrader")
}

// GetWithExchange retrieves market data for the specified token using exchange-specific data
func GetWithExchange(symbol, exchange string) (*Data, error) {
	var klines3m, klines4h []Kline
	// Normalize symbol
	symbol = Normalize(symbol)

	// CME futures (NT8) read the live BarCache via the injected provider —
	// never the removed crypto providers. BarCache holds 5m/15m/1h (not 3m/4h), so we map 5m->short
	// and 1h->longer. Crypto path below is untouched.
	// W-NO-BINANCE A: the route is the ONE decision (marketRouteFor); the
	// NinjaTrader venue with a non-CME symbol is refused, never a crypto read.
	route := marketRouteFor(symbol, exchange)
	if route == routeRefusedVenue {
		return nil, fmt.Errorf("%s: the NinjaTrader venue reads CME futures only — a non-CME symbol is refused (no crypto market read on the futures venue)", symbol)
	}
	isFutures := route == routeFutures

	// Get 3-minute K-line data (or 5-minute for xyz assets as 3m may not be available)
	if isFutures {
		if FuturesBarsProvider == nil {
			return nil, fmt.Errorf("%s: CME futures but no NT8 bar provider wired", symbol)
		}
		klines3m = FuturesBarsProvider(symbol, "5m", 200)
		if len(klines3m) == 0 {
			return nil, fmt.Errorf("%s: no NT8 5m bars cached", symbol)
		}
	} else {
		// Crypto market reads were removed with the crypto venues. A non-CME
		// symbol has no bar source, and a silent empty slice would read as
		// "no data yet" instead of "unsupported" — so refuse explicitly.
		return nil, fmt.Errorf("%s: no bar source — CME futures via NT8 is the only supported market", symbol)
	}

	// Data staleness detection: prevent frozen-ticker price freeze issues
	if isStaleData(klines3m, symbol) {
		logger.Infof("⚠️  WARNING: %s detected stale data (consecutive price freeze), skipping symbol", symbol)
		return nil, fmt.Errorf("%s data is stale, possible cache failure", symbol)
	}

	// Get 4-hour K-line data (futures: use 1h from BarCache as the longer TF)
	if isFutures {
		klines4h = FuturesBarsProvider(symbol, "1h", 200)
		if len(klines4h) == 0 {
			return nil, fmt.Errorf("%s: no NT8 1h bars cached", symbol)
		}
	} else {
		return nil, fmt.Errorf("%s: no bar source for the longer timeframe — CME futures via NT8 only", symbol)
	}

	// Check if data is empty
	if len(klines3m) == 0 {
		return nil, fmt.Errorf("3-minute K-line data is empty")
	}
	if len(klines4h) == 0 {
		return nil, fmt.Errorf("4-hour K-line data is empty")
	}

	// Calculate current indicators (based on 3-minute latest data)
	currentPrice := klines3m[len(klines3m)-1].Close
	currentEMA20 := calculateEMA(klines3m, 20)
	currentMACD := calculateMACD(klines3m)
	currentRSI7 := calculateRSI(klines3m, 7)

	// Price changes (W1): both windows by bar CLOSE TIME on the short series,
	// the finest one this read has (futures 5m×200)
	// 3m×100); absent when it does not reach back. The long series has the
	// window's own resolution, so it could only give "the previous bar".
	priceChange1h := ChangeOverWindow(klines3m, time.Hour)
	priceChange4h := ChangeOverWindow(klines3m, 4*time.Hour)

	// OI + funding. W-NO-BINANCE A: the CME futures path makes NO external
	// market-data call — crypto-perp feeds have no CME symbol behind them.
	// — and reports them ABSENT (nil / not known), never a fabricated 0.
	var oiData *OIData
	var fundingRate float64
	var fundingKnown bool
	oiData, fundingRate, fundingKnown = futuresOIFunding()

	// Calculate intraday series data
	intradayData := calculateIntradaySeries(klines3m)

	// Calculate longer-term data
	longerTermData := calculateLongerTermData(klines4h)

	return &Data{
		Symbol:            symbol,
		CurrentPrice:      currentPrice,
		PriceChange1h:     priceChange1h,
		PriceChange4h:     priceChange4h,
		CurrentEMA20:      currentEMA20,
		CurrentMACD:       currentMACD,
		CurrentRSI7:       currentRSI7,
		OpenInterest:      oiData,
		FundingRate:       fundingRate,
		FundingRateKnown:  fundingKnown,
		IntradaySeries:    intradayData,
		LongerTermContext: longerTermData,
	}, nil
}

// GetWithTimeframes retrieves market data for specified multiple timeframes
// timeframes: list of timeframes, e.g. ["5m", "15m", "1h", "4h"]
// primaryTimeframe: primary timeframe (used for calculating current indicators), defaults to timeframes[0]
// count: number of K-lines for each timeframe
// clampInt clamps v to [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// maxConfiguredPeriod returns the largest configured indicator period across all
// families (EMA/RSI/ATR/BOLL), or 0 when none are configured. Drives the F8 fetch
// depth so the longest-period series (e.g. EMA200) can fully warm up.
func maxConfiguredPeriod(ip IndicatorPeriods) int {
	m := 0
	for _, fam := range [][]int{ip.EMA, ip.RSI, ip.ATR, ip.BOLL} {
		for _, p := range fam {
			if p > m {
				m = p
			}
		}
	}
	return m
}

// GetWithTimeframesVenue is GetWithTimeframes for a KNOWN venue (CTO F2): the
// read goes through marketRouteFor exactly like GetWithExchange, so the
// NinjaTrader venue with a non-CME symbol is REFUSED — never the crypto branch
// (the removed crypto providers + their OI/funding). The engine's two cycle reads
// and the indicator mirror call this with the trader's exchange; an empty
// venue routes by symbol, as GetWithTimeframes does.
func GetWithTimeframesVenue(symbol, venue string, timeframes []string, primaryTimeframe string, count int, indPeriods ...IndicatorPeriods) (*Data, error) {
	if marketRouteFor(Normalize(symbol), venue) == routeRefusedVenue {
		return nil, fmt.Errorf("%s: the NinjaTrader venue reads CME futures only — a non-CME symbol is refused (no crypto market read on the futures venue)", Normalize(symbol))
	}
	return GetWithTimeframes(symbol, timeframes, primaryTimeframe, count, indPeriods...)
}

func GetWithTimeframes(symbol string, timeframes []string, primaryTimeframe string, count int, indPeriods ...IndicatorPeriods) (*Data, error) {
	symbol = Normalize(symbol)

	// Strategy-configured indicator periods (optional). When absent, the computed
	// series fall back to the legacy fixed periods byte-for-byte.
	var ip IndicatorPeriods
	if len(indPeriods) > 0 {
		ip = indPeriods[0]
	}

	if len(timeframes) == 0 {
		return nil, fmt.Errorf("at least one timeframe is required")
	}

	// If primary timeframe is not specified, use the first one
	if primaryTimeframe == "" {
		primaryTimeframe = timeframes[0]
	}

	// Ensure primary timeframe is in the list
	hasPrimary := false
	for _, tf := range timeframes {
		if tf == primaryTimeframe {
			hasPrimary = true
			break
		}
	}
	if !hasPrimary {
		timeframes = append([]string{primaryTimeframe}, timeframes...)
	}

	// F8 — fetch depth. Each series value for a period-P indicator needs P prior
	// bars, so to render a full `count`-length series for the LONGEST configured
	// period (e.g. EMA200) we must fetch maxPeriod + count bars — not a flat 200,
	// which left EMA200 a 1-value warm-up stub. Clamp to [200, 2500]: 200 = the
	// legacy floor (never fetch less than before); 2500 = the BarCache cap.
	//
	// R22 F-2: an EMA needs more than its period — EMAWarmupBars(P) (661 for
	// EMA200) before its last value stops being the SMA seed, so the depth
	// covers the longest configured EMA's warm-up too.
	need := maxConfiguredPeriod(ip)
	for _, p := range ip.EMA {
		if w := EMAWarmupBars(p); w > need {
			need = w
		}
	}
	fetchDepth := clampInt(need+count, 200, 2500)

	// Store data for all timeframes
	timeframeData := make(map[string]*TimeframeSeriesData)
	var primaryKlines []Kline

	// CME futures (NQ.c.0 / MNQ / ...) read the live NT8 bar feed via the
	// injected FuturesBarsProvider. Crypto path below is
	// untouched.
	isFutures := IsCMEFuturesSymbol(symbol)

	// Get K-line data for each timeframe
	for _, tf := range timeframes {
		var klines []Kline

		if isFutures {
			// NT8 futures: read closed bars from the BarCache (Stage 3).
			if FuturesBarsProvider == nil {
				logger.Infof("⚠️ %s %s: CME futures symbol but no NT8 bar provider wired; skipping", symbol, tf)
				continue
			}
			klines = FuturesBarsProvider(symbol, tf, fetchDepth)
			if len(klines) == 0 {
				logger.Infof("⚠️ %s %s: no NT8 bars cached yet", symbol, tf)
				continue
			}
		} else {
			// Crypto market reads were removed with the crypto venues; a
			// non-CME symbol has no bar source here.
			logger.Infof("⚠️ %s %s: no bar source — CME futures via NT8 only; skipping", symbol, tf)
			continue
		}

		if len(klines) == 0 {
			logger.Infof("⚠️ %s %s K-line data is empty", symbol, tf)
			continue
		}

		// Save primary timeframe K-lines for calculating base indicators
		if tf == primaryTimeframe {
			primaryKlines = klines
		}

		// Calculate series data for this timeframe (use count from config)
		seriesData := calculateTimeframeSeries(klines, tf, count, ip)
		timeframeData[tf] = seriesData
	}

	// If primary timeframe data is empty, return error
	if len(primaryKlines) == 0 {
		return nil, fmt.Errorf("Primary timeframe %s K-line data is empty", primaryTimeframe)
	}

	// Data staleness detection
	if isStaleData(primaryKlines, symbol) {
		logger.Infof("⚠️  WARNING: %s detected stale data (consecutive price freeze), skipping symbol", symbol)
		return nil, fmt.Errorf("%s data is stale, possible cache failure", symbol)
	}

	// Calculate current indicators (based on primary timeframe latest data)
	currentPrice := primaryKlines[len(primaryKlines)-1].Close
	currentEMA20 := calculateEMA(primaryKlines, 20)
	currentMACD := calculateMACD(primaryKlines)
	currentRSI7 := calculateRSI(primaryKlines, 7)

	// Latest EMA per CONFIGURED period (e.g. {9,21,200}) — alongside the legacy
	// CurrentEMA20. Empty when no periods supplied (back-compat readers use EMA20).
	var currentEMAByPeriod map[int]float64
	if len(ip.EMA) > 0 {
		currentEMAByPeriod = make(map[int]float64, len(ip.EMA))
		for _, p := range ip.EMA {
			if p > 0 {
				currentEMAByPeriod[p] = calculateEMAWarmed(primaryKlines, p) // 0 = not warmed → n/a
			}
		}
	}

	// F9 — latest RSI per CONFIGURED period (e.g. {14}), so the snapshot label is
	// honest (current_rsi14, not a hardcoded current_rsi7). Empty when no periods
	// supplied → the prompt falls back to the legacy period-7 line.
	var currentRSIByPeriod map[int]float64
	if len(ip.RSI) > 0 {
		currentRSIByPeriod = make(map[int]float64, len(ip.RSI))
		for _, p := range ip.RSI {
			if p > 0 {
				currentRSIByPeriod[p] = calculateRSI(primaryKlines, p)
			}
		}
	}

	// Price changes (W1): by bar close time on the primary series; absent when
	// its timeframe is too coarse for the window or it does not reach back.
	priceChange1h := changeOnTimeframe(primaryKlines, primaryTimeframe, time.Hour)
	priceChange4h := changeOnTimeframe(primaryKlines, primaryTimeframe, 4*time.Hour)

	// Get OI + Funding Rate. CME futures have no
	// external symbol: the futures path makes no call and reports both ABSENT
	// (W-NO-BINANCE A — never the fabricated {0,0} / 0 this used to write).
	var oiData *OIData
	var fundingRate float64
	var fundingKnown bool
	oiData, fundingRate, fundingKnown = futuresOIFunding()

	return &Data{
		Symbol:             symbol,
		CurrentPrice:       currentPrice,
		PriceChange1h:      priceChange1h,
		PriceChange4h:      priceChange4h,
		CurrentEMA20:       currentEMA20,
		CurrentEMAByPeriod: currentEMAByPeriod,
		CurrentMACD:        currentMACD,
		CurrentRSI7:        currentRSI7,
		CurrentRSIByPeriod: currentRSIByPeriod,
		OpenInterest:       oiData,
		FundingRate:        fundingRate,
		FundingRateKnown:   fundingKnown,
		TimeframeData:      timeframeData,
	}, nil
}

// Format formats and outputs market data
func Format(data *Data) string {
	var sb strings.Builder

	// Format price with dynamic precision
	priceStr := formatPriceWithDynamicPrecision(data.CurrentPrice)
	sb.WriteString(fmt.Sprintf("current_price = %s, current_ema20 = %.3f, current_macd = %.3f, current_rsi (7 period) = %.3f\n\n",
		priceStr, data.CurrentEMA20, data.CurrentMACD, data.CurrentRSI7))

	sb.WriteString(fmt.Sprintf("In addition, here is the latest %s open interest and funding rate for perps:\n\n",
		data.Symbol))

	if data.OpenInterest != nil {
		// Format OI data with dynamic precision
		oiLatestStr := formatPriceWithDynamicPrecision(data.OpenInterest.Latest)
		oiAverageStr := formatPriceWithDynamicPrecision(data.OpenInterest.Average)
		sb.WriteString(fmt.Sprintf("Open Interest: Latest: %s Average: %s\n\n",
			oiLatestStr, oiAverageStr))
	}

	sb.WriteString(fmt.Sprintf("Funding Rate: %.2e\n\n", data.FundingRate))

	if data.IntradaySeries != nil {
		sb.WriteString("Intraday series (3‑minute intervals, oldest → latest):\n\n")

		if len(data.IntradaySeries.MidPrices) > 0 {
			sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.IntradaySeries.MidPrices)))
		}

		if len(data.IntradaySeries.EMA20Values) > 0 {
			sb.WriteString(fmt.Sprintf("EMA indicators (20‑period): %s\n\n", formatFloatSlice(data.IntradaySeries.EMA20Values)))
		}

		if len(data.IntradaySeries.MACDValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.IntradaySeries.MACDValues)))
		}

		if len(data.IntradaySeries.RSI7Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (7‑Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI7Values)))
		}

		if len(data.IntradaySeries.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (14‑Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI14Values)))
		}

		if len(data.IntradaySeries.Volume) > 0 {
			sb.WriteString(fmt.Sprintf("Volume: %s\n\n", formatFloatSlice(data.IntradaySeries.Volume)))
		}

		sb.WriteString(fmt.Sprintf("3m ATR (14‑period): %.3f\n\n", data.IntradaySeries.ATR14))
	}

	if data.LongerTermContext != nil {
		sb.WriteString("Longer‑term context (4‑hour timeframe):\n\n")

		sb.WriteString(fmt.Sprintf("20‑Period EMA: %.3f vs. 50‑Period EMA: %.3f\n\n",
			data.LongerTermContext.EMA20, data.LongerTermContext.EMA50))

		sb.WriteString(fmt.Sprintf("3‑Period ATR: %.3f vs. 14‑Period ATR: %.3f\n\n",
			data.LongerTermContext.ATR3, data.LongerTermContext.ATR14))

		sb.WriteString(fmt.Sprintf("Current Volume: %.3f vs. Average Volume: %.3f\n\n",
			data.LongerTermContext.CurrentVolume, data.LongerTermContext.AverageVolume))

		if len(data.LongerTermContext.MACDValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.LongerTermContext.MACDValues)))
		}

		if len(data.LongerTermContext.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (14‑Period): %s\n\n", formatFloatSlice(data.LongerTermContext.RSI14Values)))
		}
	}

	// Multi-timeframe data (new)
	if len(data.TimeframeData) > 0 {
		// Output sorted by timeframe
		timeframeOrder := []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w"}
		for _, tf := range timeframeOrder {
			if tfData, ok := data.TimeframeData[tf]; ok {
				sb.WriteString(fmt.Sprintf("=== %s Timeframe ===\n\n", strings.ToUpper(tf)))
				formatTimeframeData(&sb, tfData)
			}
		}
	}

	return sb.String()
}

// formatTimeframeData formats data for a single timeframe
func formatTimeframeData(sb *strings.Builder, data *TimeframeSeriesData) {
	// Use OHLCV table format if kline data is available
	if len(data.Klines) > 0 {
		sb.WriteString("Time(CT)       Open      High      Low       Close     Volume\n")
		for i, k := range data.Klines {
			// v1-audit #4 (fail-register wave): the chat table now renders CT
			// like every trading prompt (kernel/tz.go is the contract; market/
			// cannot import kernel, so the location loads here with the same
			// canonical zone name).
			t := time.Unix(k.Time/1000, 0).In(chatTableCT())
			timeStr := t.Format("01-02 15:04")
			marker := ""
			if i == len(data.Klines)-1 {
				marker = "  <- current"
			}
			sb.WriteString(fmt.Sprintf("%-14s %-9.4f %-9.4f %-9.4f %-9.4f %-12.2f%s\n",
				timeStr, k.Open, k.High, k.Low, k.Close, k.Volume, marker))
		}
		sb.WriteString("\n")
	} else if len(data.MidPrices) > 0 {
		// Fallback to old format for backward compatibility
		sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.MidPrices)))
		if len(data.Volume) > 0 {
			sb.WriteString(fmt.Sprintf("Volume: %s\n\n", formatFloatSlice(data.Volume)))
		}
	}

	// Technical indicators
	if len(data.EMA20Values) > 0 {
		sb.WriteString(fmt.Sprintf("EMA20: %s\n", formatFloatSlice(data.EMA20Values)))
	}

	if len(data.EMA50Values) > 0 {
		sb.WriteString(fmt.Sprintf("EMA50: %s\n", formatFloatSlice(data.EMA50Values)))
	}

	if len(data.MACDValues) > 0 {
		sb.WriteString(fmt.Sprintf("MACD: %s\n", formatFloatSlice(data.MACDValues)))
	}

	if len(data.RSI7Values) > 0 {
		sb.WriteString(fmt.Sprintf("RSI7: %s\n", formatFloatSlice(data.RSI7Values)))
	}

	if len(data.RSI14Values) > 0 {
		sb.WriteString(fmt.Sprintf("RSI14: %s\n", formatFloatSlice(data.RSI14Values)))
	}

	if data.ATR14 > 0 {
		sb.WriteString(fmt.Sprintf("ATR14: %.4f\n", data.ATR14))
	}

	sb.WriteString("\n")
}

// formatPriceWithDynamicPrecision dynamically selects precision based on price range
// This perfectly supports the full price range, from ultra-low-price assets to high-price symbols
func formatPriceWithDynamicPrecision(price float64) string {
	switch {
	case price < 0.0001:
		// Ultra-low price meme coins: 1000SATS, 1000WHY, DOGS
		// 0.00002070 → "0.00002070" (8 decimal places)
		return fmt.Sprintf("%.8f", price)
	case price < 0.001:
		// Low price meme coins: NEIRO, HMSTR, HOT, NOT
		// 0.00015060 → "0.000151" (6 decimal places)
		return fmt.Sprintf("%.6f", price)
	case price < 0.01:
		// Mid-low price coins: PEPE, SHIB, MEME
		// 0.00556800 → "0.005568" (6 decimal places)
		return fmt.Sprintf("%.6f", price)
	case price < 1.0:
		// Low-price symbols
		// 0.9954 → "0.9954" (4 decimal places)
		return fmt.Sprintf("%.4f", price)
	case price < 100:
		// Mid price coins: SOL, AVAX, LINK, MATIC
		// 23.4567 → "23.4567" (4 decimal places)
		return fmt.Sprintf("%.4f", price)
	default:
		// High-price symbols (save tokens)
		// 45678.9123 → "45678.91" (2 decimal places)
		return fmt.Sprintf("%.2f", price)
	}
}

// formatFloatSlice formats float64 slice to string (using dynamic precision)
func formatFloatSlice(values []float64) string {
	strValues := make([]string, len(values))
	for i, v := range values {
		strValues[i] = formatPriceWithDynamicPrecision(v)
	}
	return "[" + strings.Join(strValues, ", ") + "]"
}

// xyz dex assets that should NOT get a crypto-quote suffix
var xyzDexAssets = map[string]bool{
	// Stocks
	"TSLA": true, "NVDA": true, "AAPL": true, "MSFT": true, "META": true,
	"AMZN": true, "GOOGL": true, "AMD": true, "COIN": true, "NFLX": true,
	"PLTR": true, "HOOD": true, "INTC": true, "MSTR": true, "TSM": true,
	"ORCL": true, "MU": true, "RIVN": true, "COST": true, "LLY": true,
	"CRCL": true, "SKHX": true, "SNDK": true,
	// Forex
	"EUR": true, "JPY": true,
	// Commodities
	"GOLD": true, "SILVER": true,
	// Index
	"XYZ100": true,
}

// IsXyzDexAsset checks if a symbol is an xyz dex asset
func IsXyzDexAsset(symbol string) bool {
	base := strings.ToUpper(symbol)
	// Remove any prefix/suffix
	base = strings.TrimPrefix(base, "XYZ:")
	for _, suffix := range []string{"USD"} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimSuffix(base, suffix)
			break
		}
	}
	return xyzDexAssets[base]
}

// Normalize normalizes symbol
// For crypto (dormant tail): ensures a crypto-quote trading pair
// For xyz dex assets (stocks, forex, commodities): uses xyz: prefix without a crypto-quote suffix
// For CME futures (Task 12): preserves case (NQ.c.0 not NQ.C.0) and never appends a crypto-quote suffix.
//
//	Databento's continuous symbology requires the lowercase `.c.` suffix
//	and rejects uppercased / crypto-quote-suffixed forms. Detection via the
//	shared helper IsCMEFuturesSymbol — keep the early-return BEFORE the
//	ToUpper below or the symbol is irreversibly corrupted.
func Normalize(symbol string) string {
	// Task 12 / Cluster D — CME futures bypass crypto normalization.
	if IsCMEFuturesSymbol(symbol) {
		return strings.TrimSpace(symbol)
	}
	symbol = strings.ToUpper(symbol)

	// Check if this is an xyz dex asset
	if IsXyzDexAsset(symbol) {
		// Remove any xyz: prefix (case-insensitive) and any crypto-quote suffix, then add xyz: prefix
		base := symbol
		// Handle both lowercase and uppercase xyz: prefix
		if strings.HasPrefix(strings.ToLower(base), "xyz:") {
			base = base[4:] // Remove first 4 characters ("xyz:")
		}
		for _, suffix := range []string{"USD"} {
			if strings.HasSuffix(base, suffix) {
				base = strings.TrimSuffix(base, suffix)
				break
			}
		}
		return "xyz:" + base
	}

	// Remove exchange-specific separators (legacy venue symbol forms)
	symbol = strings.ReplaceAll(symbol, "_", "")
	symbol = strings.ReplaceAll(symbol, "-SWAP", "")
	symbol = strings.ReplaceAll(symbol, "-", "")

	return symbol
}

// parseFloat parses float value
func parseFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case string:
		return strconv.ParseFloat(val, 64)
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	default:
		return 0, fmt.Errorf("unsupported type: %T", v)
	}
}

// isStaleData detects stale data (consecutive price freeze)
// Fix frozen-ticker issue: consecutive N periods with completely unchanged prices indicate a data-source anomaly
func isStaleData(klines []Kline, symbol string) bool {
	if len(klines) < 5 {
		return false // Insufficient data to determine
	}

	// Detection threshold: 5 consecutive 3-minute periods with unchanged price (15 minutes without fluctuation)
	const stalePriceThreshold = 5
	const priceTolerancePct = 0.0001 // 0.01% fluctuation tolerance (avoid false positives)

	// Take the last stalePriceThreshold K-lines
	recentKlines := klines[len(klines)-stalePriceThreshold:]
	firstPrice := recentKlines[0].Close

	// Check if all prices are within tolerance
	for i := 1; i < len(recentKlines); i++ {
		priceDiff := math.Abs(recentKlines[i].Close-firstPrice) / firstPrice
		if priceDiff > priceTolerancePct {
			return false // Price fluctuation exists, data is normal
		}
	}

	// Additional check: MACD and volume
	// If price is unchanged but MACD/volume shows normal fluctuation, it might be a real market situation (extremely low volatility)
	// Check if volume is also 0 (data completely frozen)
	allVolumeZero := true
	for _, k := range recentKlines {
		if k.Volume > 0 {
			allVolumeZero = false
			break
		}
	}

	if allVolumeZero {
		logger.Infof("⚠️  %s stale data confirmed: price freeze + zero volume", symbol)
		return true
	}

	// Price frozen but has volume: might be extremely low volatility market, allow but log warning
	logger.Infof("⚠️  %s detected extreme price stability (no fluctuation for %d consecutive periods), but volume is normal", symbol, stalePriceThreshold)
	return false
}

// chatTableCT loads America/Chicago for the chat-path OHLCV table (the one
// former Time(UTC) site outside the tz-guard dirs — v1 audit §1.1).
func chatTableCT() *time.Location {
	if loc, err := time.LoadLocation("America/Chicago"); err == nil {
		return loc
	}
	return time.UTC
}
