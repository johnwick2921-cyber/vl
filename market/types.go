package market

import "time"

// IndicatorPeriods carries the strategy-configured indicator periods into the
// market-data layer so the computed series use the USER's periods instead of the
// historical hardcoded defaults (EMA 20/50, BOLL 20, RSI 7/14, ATR 14). Passed
// (optionally, variadic) to GetWithTimeframes; an empty/zero value preserves the
// legacy hardcoded output byte-for-byte. Wired incrementally: EMA first.
type IndicatorPeriods struct {
	EMA  []int
	RSI  []int
	ATR  []int
	BOLL []int
}

// Data market data structure
type Data struct {
	Symbol       string
	CurrentPrice float64
	// PriceChange1h / PriceChange4h: percent change over that much WALL TIME,
	// measured on bar close times (ChangeOverWindow). nil = ABSENT (the series
	// cannot measure the window) — renderers print n/a, never a fabricated 0.
	PriceChange1h *float64
	PriceChange4h *float64
	CurrentEMA20  float64
	// CurrentEMAByPeriod holds the latest EMA per CONFIGURED period (e.g.
	// {9: …, 21: …, 200: …}). Populated only when periods are supplied; empty →
	// readers fall back to CurrentEMA20 (legacy 20). Prompt-data only.
	CurrentEMAByPeriod map[int]float64
	CurrentMACD        float64
	CurrentRSI7        float64
	// CurrentRSIByPeriod holds the latest RSI per CONFIGURED period (e.g. {14: …}).
	// Populated only when RSI periods are supplied; empty → readers fall back to the
	// legacy CurrentRSI7 (period 7). Prompt-data only. Mirrors CurrentEMAByPeriod.
	CurrentRSIByPeriod map[int]float64
	// OpenInterest is nil when ABSENT — the CME futures path has no external
	// market data (W-NO-BINANCE A): it is never a fabricated {0,0}.
	OpenInterest *OIData
	FundingRate  float64
	// FundingRateKnown reports whether FundingRate was actually read. false on
	// the CME futures path — a 0 there means "absent", and renderers print n/a.
	FundingRateKnown  bool
	IntradaySeries    *IntradayData
	LongerTermContext *LongerTermData
	// Multi-timeframe data (new)
	TimeframeData map[string]*TimeframeSeriesData `json:"timeframe_data,omitempty"`
}

// KlineBar single kline bar with OHLCV data
type KlineBar struct {
	Time   int64   `json:"time"`   // Unix timestamp in milliseconds
	Open   float64 `json:"open"`   // Open price
	High   float64 `json:"high"`   // High price
	Low    float64 `json:"low"`    // Low price
	Close  float64 `json:"close"`  // Close price
	Volume float64 `json:"volume"` // Volume
}

// TimeframeSeriesData series data for a single timeframe
type TimeframeSeriesData struct {
	Timeframe   string     `json:"timeframe"`    // Timeframe identifier, e.g. "5m", "15m", "1h"
	Klines      []KlineBar `json:"klines"`       // Full OHLCV kline data
	MidPrices   []float64  `json:"mid_prices"`   // Price series (deprecated, kept for compatibility)
	EMA20Values []float64  `json:"ema20_values"` // EMA20 series (legacy fixed period; kept for back-compat readers)
	EMA50Values []float64  `json:"ema50_values"` // EMA50 series (legacy fixed period; kept for back-compat readers)
	// EMAByPeriod holds one EMA series per CONFIGURED period (e.g. 9/21/200).
	// Populated only when periods are supplied to GetWithTimeframes; empty → the
	// prompt falls back to EMA20Values/EMA50Values (byte-identical legacy output).
	EMAByPeriod map[int][]float64 `json:"ema_by_period,omitempty"`
	MACDValues  []float64         `json:"macd_values"`  // MACD series
	RSI7Values  []float64         `json:"rsi7_values"`  // RSI7 series (legacy fixed period; back-compat)
	RSI14Values []float64         `json:"rsi14_values"` // RSI14 series (legacy fixed period; back-compat)
	// RSIByPeriod holds one RSI series per CONFIGURED period; empty → prompt falls
	// back to RSI7Values/RSI14Values (byte-identical legacy output).
	RSIByPeriod map[int][]float64 `json:"rsi_by_period,omitempty"`
	Volume      []float64         `json:"volume"` // Volume series (deprecated, use Klines)
	ATR14       float64           `json:"atr14"`  // ATR14 (legacy fixed period; back-compat)
	// ATRByPeriod holds the latest ATR per CONFIGURED period; empty → prompt falls
	// back to ATR14 (byte-identical legacy output).
	ATRByPeriod map[int]float64 `json:"atr_by_period,omitempty"`
	// Bollinger Bands (period 20, std dev multiplier 2) — legacy fixed; back-compat.
	BOLLUpper  []float64 `json:"boll_upper"`  // Upper band
	BOLLMiddle []float64 `json:"boll_middle"` // Middle band (SMA)
	BOLLLower  []float64 `json:"boll_lower"`  // Lower band
	// BOLLByPeriod holds Bollinger Bands per CONFIGURED period (std-dev mult fixed
	// at 2); empty → prompt falls back to BOLLUpper/Middle/Lower (byte-identical).
	BOLLByPeriod map[int]*BollBands `json:"boll_by_period,omitempty"`
}

// BollBands holds one Bollinger Bands triple (upper/middle/lower) for a period.
type BollBands struct {
	Upper  []float64 `json:"upper"`
	Middle []float64 `json:"middle"`
	Lower  []float64 `json:"lower"`
}

// OIData Open Interest data
type OIData struct {
	Latest  float64
	Average float64
}

// IntradayData intraday data (3-minute interval)
type IntradayData struct {
	MidPrices   []float64
	EMA20Values []float64
	MACDValues  []float64
	RSI7Values  []float64
	RSI14Values []float64
	Volume      []float64
	ATR14       float64
}

// LongerTermData longer-term data (4-hour timeframe)
type LongerTermData struct {
	EMA20         float64
	EMA50         float64
	ATR3          float64
	ATR14         float64
	CurrentVolume float64
	AverageVolume float64
	MACDValues    []float64
	RSI14Values   []float64
}

// API response structure
type ExchangeInfo struct {
	Symbols []SymbolInfo `json:"symbols"`
}

type SymbolInfo struct {
	Symbol            string `json:"symbol"`
	Status            string `json:"status"`
	BaseAsset         string `json:"baseAsset"`
	QuoteAsset        string `json:"quoteAsset"`
	ContractType      string `json:"contractType"`
	PricePrecision    int    `json:"pricePrecision"`
	QuantityPrecision int    `json:"quantityPrecision"`
}

type Kline struct {
	OpenTime            int64   `json:"openTime"`
	Open                float64 `json:"open"`
	High                float64 `json:"high"`
	Low                 float64 `json:"low"`
	Close               float64 `json:"close"`
	Volume              float64 `json:"volume"`
	CloseTime           int64   `json:"closeTime"`
	QuoteVolume         float64 `json:"quoteVolume"`
	Trades              int     `json:"trades"`
	TakerBuyBaseVolume  float64 `json:"takerBuyBaseVolume"`
	TakerBuyQuoteVolume float64 `json:"takerBuyQuoteVolume"`
	// Contract (101 D2, 2026-09-16) names the futures contract this bar
	// belongs to when the chart is served across a roll — "MNQ 09-26" then
	// "MNQ 12-26" with a real basis step between them. ADDITIVE and
	// omitempty: every existing crypto/NT8 JSON stays byte-identical; every
	// named-field literal in the repo compiles unchanged (this is a
	// high-cascade type — that is the whole reason it is a trailing optional
	// field and nothing else).
	Contract string `json:"contract,omitempty"`
	// Derived (W-ROLL-DAY-CHART, 2026-09-19) marks a prior-contract bar
	// aggregated from THAT contract's stored 1m rows instead of its stored
	// higher-TF rows. ADDITIVE + omitempty (same trailing-optional rule).
	Derived bool `json:"derived,omitempty"`
	// Adjusted (W-ROLL-DAY-CHART, 2026-09-19) marks a prior-contract bar whose
	// OHLC was shifted by the measured roll basis. ADDITIVE + omitempty.
	Adjusted bool `json:"adjusted,omitempty"`
	// Final (W-PICTURE-HTF, 2026-09-20) is the AddOn's proof the bar CLOSED
	// (final=true on the boundary frame; forming bars carry false). Zero for
	// every crypto/backfill producer. ADDITIVE + omitempty.
	Final bool `json:"final,omitempty"`
	// EmittedAt (W-PICTURE-HTF, 2026-09-20) is the AddOn's emission clock for
	// this frame (ms, UTC). ADDITIVE + omitempty.
	EmittedAt int64 `json:"emitted_at,omitempty"`
	// Historical (EX-13 review P0, 11-17) marks a bar that reached the cache in
	// an AddOn bars_historical replay batch (the cache's Bar.Source ==
	// "historical"), not as a live frame. Only such a bar can be NT8's
	// in-progress bar labelled final (VLBarsSubscriptionManager.cs
	// EmitHistorical stamps every replay bar final). GO-SIDE ONLY, json:"-"
	// like the wire Bar.Source it mirrors: every Kline JSON stays
	// byte-identical, every named-field literal compiles unchanged, and a bar
	// that went through JSON reads false (the guard then stays silent — the old
	// behaviour, never a fabricated verdict).
	Historical bool `json:"-"`
}

type KlineResponse []interface{}

type PriceTicker struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}

type Ticker24hr struct {
	Symbol             string `json:"symbol"`
	PriceChange        string `json:"priceChange"`
	PriceChangePercent string `json:"priceChangePercent"`
	Volume             string `json:"volume"`
	QuoteVolume        string `json:"quoteVolume"`
}

// SymbolFeatures feature data structure
type SymbolFeatures struct {
	Symbol           string    `json:"symbol"`
	Timestamp        time.Time `json:"timestamp"`
	Price            float64   `json:"price"`
	PriceChange15Min float64   `json:"price_change_15min"`
	PriceChange1H    float64   `json:"price_change_1h"`
	PriceChange4H    float64   `json:"price_change_4h"`
	Volume           float64   `json:"volume"`
	VolumeRatio5     float64   `json:"volume_ratio_5"`
	VolumeRatio20    float64   `json:"volume_ratio_20"`
	VolumeTrend      float64   `json:"volume_trend"`
	RSI14            float64   `json:"rsi_14"`
	SMA5             float64   `json:"sma_5"`
	SMA10            float64   `json:"sma_10"`
	SMA20            float64   `json:"sma_20"`
	HighLowRatio     float64   `json:"high_low_ratio"`
	Volatility20     float64   `json:"volatility_20"`
	PositionInRange  float64   `json:"position_in_range"`
}

// Alert alert data structure
type Alert struct {
	Type      string    `json:"type"`
	Symbol    string    `json:"symbol"`
	Value     float64   `json:"value"`
	Threshold float64   `json:"threshold"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

type Config struct {
	AlertThresholds AlertThresholds `json:"alert_thresholds"`
	UpdateInterval  int             `json:"update_interval"` // seconds
	CleanupConfig   CleanupConfig   `json:"cleanup_config"`
}

type AlertThresholds struct {
	VolumeSpike      float64 `json:"volume_spike"`
	PriceChange15Min float64 `json:"price_change_15min"`
	VolumeTrend      float64 `json:"volume_trend"`
	RSIOverbought    float64 `json:"rsi_overbought"`
	RSIOversold      float64 `json:"rsi_oversold"`
}
type CleanupConfig struct {
	InactiveTimeout   time.Duration `json:"inactive_timeout"`    // Inactive timeout duration
	MinScoreThreshold float64       `json:"min_score_threshold"` // Minimum score threshold
	NoAlertTimeout    time.Duration `json:"no_alert_timeout"`    // No alert timeout duration
	CheckInterval     time.Duration `json:"check_interval"`      // Check interval
}

var config = Config{
	AlertThresholds: AlertThresholds{
		VolumeSpike:      3.0,
		PriceChange15Min: 0.05,
		VolumeTrend:      2.0,
		RSIOverbought:    70,
		RSIOversold:      30,
	},
	CleanupConfig: CleanupConfig{
		InactiveTimeout:   30 * time.Minute,
		MinScoreThreshold: 15.0,
		NoAlertTimeout:    20 * time.Minute,
		CheckInterval:     5 * time.Minute,
	},
	UpdateInterval: 60, // 1 minute
}

// BoxData represents multi-period Donchian channel (box) data
type BoxData struct {
	// Short-term box (72 1h candles = 3 days)
	ShortUpper float64 `json:"short_upper"`
	ShortLower float64 `json:"short_lower"`

	// Mid-term box (240 1h candles = 10 days)
	MidUpper float64 `json:"mid_upper"`
	MidLower float64 `json:"mid_lower"`

	// Long-term box (500 1h candles = ~21 days)
	LongUpper float64 `json:"long_upper"`
	LongLower float64 `json:"long_lower"`

	// Current price position relative to boxes
	CurrentPrice float64 `json:"current_price"`
}

// RegimeLevel represents the ranging classification level
type RegimeLevel string

const (
	RegimeLevelNarrow   RegimeLevel = "narrow"   // narrow range oscillation
	RegimeLevelStandard RegimeLevel = "standard" // standard oscillation
	RegimeLevelWide     RegimeLevel = "wide"     // wide range oscillation
	RegimeLevelVolatile RegimeLevel = "volatile" // extreme volatility
	RegimeLevelTrending RegimeLevel = "trending" // trending
)

// BreakoutLevel represents which box level has been broken
type BreakoutLevel string

const (
	BreakoutNone  BreakoutLevel = "none"
	BreakoutShort BreakoutLevel = "short"
	BreakoutMid   BreakoutLevel = "mid"
	BreakoutLong  BreakoutLevel = "long"
)

// GridDirection represents the current grid trading direction bias
type GridDirection string

const (
	GridDirectionNeutral   GridDirection = "neutral"    // 50% buy + 50% sell
	GridDirectionLong      GridDirection = "long"       // 100% buy
	GridDirectionShort     GridDirection = "short"      // 100% sell
	GridDirectionLongBias  GridDirection = "long_bias"  // 70% buy + 30% sell (default)
	GridDirectionShortBias GridDirection = "short_bias" // 30% buy + 70% sell (default)
)

// GetBuySellRatio returns the buy and sell ratio for this direction
// biasRatio is the ratio for biased directions (default 0.7 means 70%/30%)
func (d GridDirection) GetBuySellRatio(biasRatio float64) (buyRatio, sellRatio float64) {
	if biasRatio <= 0 || biasRatio > 1 {
		biasRatio = 0.7 // Default 70%/30%
	}

	switch d {
	case GridDirectionNeutral:
		return 0.5, 0.5
	case GridDirectionLong:
		return 1.0, 0.0
	case GridDirectionShort:
		return 0.0, 1.0
	case GridDirectionLongBias:
		return biasRatio, 1.0 - biasRatio
	case GridDirectionShortBias:
		return 1.0 - biasRatio, biasRatio
	default:
		return 0.5, 0.5
	}
}
