package market

import (
	"fmt"
)

func calculateTimeframeSeries(klines []Kline, timeframe string, count int, ip IndicatorPeriods) *TimeframeSeriesData {
	if count <= 0 {
		count = 10 // default
	}

	data := &TimeframeSeriesData{
		Timeframe:   timeframe,
		Klines:      make([]KlineBar, 0, count),
		MidPrices:   make([]float64, 0, count),
		EMA20Values: make([]float64, 0, count),
		EMA50Values: make([]float64, 0, count),
		MACDValues:  make([]float64, 0, count),
		RSI7Values:  make([]float64, 0, count),
		RSI14Values: make([]float64, 0, count),
		Volume:      make([]float64, 0, count),
		BOLLUpper:   make([]float64, 0, count),
		BOLLMiddle:  make([]float64, 0, count),
		BOLLLower:   make([]float64, 0, count),
	}
	if len(ip.EMA) > 0 {
		data.EMAByPeriod = make(map[int][]float64, len(ip.EMA))
		for _, p := range ip.EMA {
			if p > 0 {
				data.EMAByPeriod[p] = make([]float64, 0, count)
			}
		}
	}
	if len(ip.RSI) > 0 {
		data.RSIByPeriod = make(map[int][]float64, len(ip.RSI))
		for _, p := range ip.RSI {
			if p > 0 {
				data.RSIByPeriod[p] = make([]float64, 0, count)
			}
		}
	}
	if len(ip.BOLL) > 0 {
		data.BOLLByPeriod = make(map[int]*BollBands, len(ip.BOLL))
		for _, p := range ip.BOLL {
			if p > 0 {
				data.BOLLByPeriod[p] = &BollBands{}
			}
		}
	}

	// Get latest N data points based on count from config
	start := len(klines) - count
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		// Store full OHLCV kline data
		data.Klines = append(data.Klines, KlineBar{
			Time:   klines[i].OpenTime,
			Open:   klines[i].Open,
			High:   klines[i].High,
			Low:    klines[i].Low,
			Close:  klines[i].Close,
			Volume: klines[i].Volume,
		})

		// Keep MidPrices and Volume for backward compatibility
		data.MidPrices = append(data.MidPrices, klines[i].Close)
		data.Volume = append(data.Volume, klines[i].Volume)

		// Calculate EMA20 for each point
		if i >= 19 {
			ema20 := calculateEMA(klines[:i+1], 20)
			data.EMA20Values = append(data.EMA20Values, ema20)
		}

		// Calculate EMA50 for each point
		if i >= 49 {
			ema50 := calculateEMA(klines[:i+1], 50)
			data.EMA50Values = append(data.EMA50Values, ema50)
		}

		// Calculate the strategy-CONFIGURED EMA periods (e.g. 9/21/200) for each
		// point. R22 F-2: a point gets a value only once the EMA is WARMED
		// (EMAWarmupBars — the SMA seed weighs ≤1%); before that the configured
		// period stays present with no values and the prompt prints n/a.
		for _, p := range ip.EMA {
			if p > 0 && i+1 >= EMAWarmupBars(p) {
				data.EMAByPeriod[p] = append(data.EMAByPeriod[p], calculateEMA(klines[:i+1], p))
			}
		}

		// Calculate MACD for each point
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}

		// Calculate RSI for each point
		if i >= 7 {
			rsi7 := calculateRSI(klines[:i+1], 7)
			data.RSI7Values = append(data.RSI7Values, rsi7)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}

		// Configured RSI periods (same i>=period guard as RSI7/RSI14 above).
		for _, p := range ip.RSI {
			if p > 0 && i >= p {
				data.RSIByPeriod[p] = append(data.RSIByPeriod[p], calculateRSI(klines[:i+1], p))
			}
		}

		// Calculate Bollinger Bands (period 20, std dev multiplier 2)
		if i >= 19 {
			upper, middle, lower := calculateBOLL(klines[:i+1], 20, 2.0)
			data.BOLLUpper = append(data.BOLLUpper, upper)
			data.BOLLMiddle = append(data.BOLLMiddle, middle)
			data.BOLLLower = append(data.BOLLLower, lower)
		}

		// Configured BOLL periods (std-dev multiplier fixed at 2; same i>=period-1
		// guard as the fixed period-20 BOLL above).
		for _, p := range ip.BOLL {
			if p > 0 && i >= p-1 {
				u, m, l := calculateBOLL(klines[:i+1], p, 2.0)
				if b := data.BOLLByPeriod[p]; b != nil {
					b.Upper = append(b.Upper, u)
					b.Middle = append(b.Middle, m)
					b.Lower = append(b.Lower, l)
				}
			}
		}
	}

	// Calculate ATR14 (legacy fixed) + the configured ATR periods (latest value).
	data.ATR14 = calculateATR(klines, 14)
	if len(ip.ATR) > 0 {
		data.ATRByPeriod = make(map[int]float64, len(ip.ATR))
		for _, p := range ip.ATR {
			if p > 0 {
				data.ATRByPeriod[p] = calculateATR(klines, p)
			}
		}
	}

	return data
}

// calculateIntradaySeries calculates intraday series data
func calculateIntradaySeries(klines []Kline) *IntradayData {
	data := &IntradayData{
		MidPrices:   make([]float64, 0, 10),
		EMA20Values: make([]float64, 0, 10),
		MACDValues:  make([]float64, 0, 10),
		RSI7Values:  make([]float64, 0, 10),
		RSI14Values: make([]float64, 0, 10),
		Volume:      make([]float64, 0, 10),
	}

	// Get latest 10 data points
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		data.MidPrices = append(data.MidPrices, klines[i].Close)
		data.Volume = append(data.Volume, klines[i].Volume)

		// Calculate EMA20 for each point
		if i >= 19 {
			ema20 := calculateEMA(klines[:i+1], 20)
			data.EMA20Values = append(data.EMA20Values, ema20)
		}

		// Calculate MACD for each point
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}

		// Calculate RSI for each point
		if i >= 7 {
			rsi7 := calculateRSI(klines[:i+1], 7)
			data.RSI7Values = append(data.RSI7Values, rsi7)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}
	}

	// Calculate 3m ATR14
	data.ATR14 = calculateATR(klines, 14)

	return data
}

// calculateLongerTermData calculates longer-term data
func calculateLongerTermData(klines []Kline) *LongerTermData {
	data := &LongerTermData{
		MACDValues:  make([]float64, 0, 10),
		RSI14Values: make([]float64, 0, 10),
	}

	// Calculate EMA
	data.EMA20 = calculateEMA(klines, 20)
	data.EMA50 = calculateEMA(klines, 50)

	// Calculate ATR
	data.ATR3 = calculateATR(klines, 3)
	data.ATR14 = calculateATR(klines, 14)

	// Calculate volume
	if len(klines) > 0 {
		data.CurrentVolume = klines[len(klines)-1].Volume
		// Calculate average volume
		sum := 0.0
		for _, k := range klines {
			sum += k.Volume
		}
		data.AverageVolume = sum / float64(len(klines))
	}

	// Calculate MACD and RSI series
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}
	}

	return data
}

// GetBoxData fetches 1h klines and calculates box data for a symbol
func GetBoxData(symbol string) (*BoxData, error) {
	symbol = Normalize(symbol)

	// Fetch 500 1h klines
	var klines []Kline

	// CME futures read the live BarCache via the injected provider; the crypto
	// fetchers were removed with the crypto venues. A non-CME symbol is refused
	// rather than returning an empty box, which would read as "no range yet".
	if !IsCMEFuturesSymbol(symbol) {
		return nil, fmt.Errorf("%s: box data needs a CME futures symbol — crypto market reads were removed", symbol)
	}
	if FuturesBarsProvider == nil {
		return nil, fmt.Errorf("%s: CME futures but no NT8 bar provider wired", symbol)
	}
	klines = FuturesBarsProvider(symbol, "1h", LongBoxPeriod)

	if len(klines) == 0 {
		return nil, fmt.Errorf("no kline data available")
	}

	currentPrice := klines[len(klines)-1].Close

	return calculateBoxData(klines, currentPrice), nil
}
