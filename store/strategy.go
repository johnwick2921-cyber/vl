package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vl/logger"

	"gorm.io/gorm"
)

// SupportedTimeframes is the SINGLE SOURCE OF TRUTH for the Strategy Studio
// timeframe selector: every interval the engine can genuinely serve end-to-end.
// For NT8 futures these are exactly the timeframes the AddOn auto-subscribes and
// streams into the BarCache (provider/ninjatrader defaultAutoBarsTimeframes —
// kept in lockstep by TestDefaultAutoBarsTimeframes_MatchesSupported); the live
// BarCache serves all of them per-series (there is NO Go-side aggregation, so an
// interval outside this set — e.g. 2m — would yield empty futures klines). The
// standard intervals are the same across venues. The frontend fetches this
// list via GET /api/strategies/timeframes instead of hardcoding its own copy.
var SupportedTimeframes = []string{
	"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w",
}

// SoftWarnTimeframesAbove is the advisory threshold: selecting more than this
// many timeframes is allowed but warns (bigger AI prompt → slower, costlier
// decisions). It is NEVER a hard block (that was the old artificial max-4 cap).
const SoftWarnTimeframesAbove = 6

// MaxTimeframes caps SelectedTimeframes at the full supported capability. It was
// a hard 4 — an artificial limit that silently truncated configs on save; the AI
// prompt (formatKlineTimeframes) and the per-timeframe analysis loop both handle
// N timeframes. The clamp now only guards against absurd input beyond the real
// capability, never the legitimate 5-14 range.
var MaxTimeframes = len(SupportedTimeframes)

// Hard limits to prevent token explosion in AI requests
const (
	MaxCandidateCoins = 10
	MaxPositions      = 3
	MinKlineCount     = 10
	MaxKlineCount     = 30
	MinLeverage       = 1
	MaxBTCETHLeverage = 20
	MaxAltLeverage    = 20
	MinPositionRatio  = 0.5
	MaxPositionRatio  = 10.0
	MinRiskReward     = 1.0
	MaxRiskReward     = 10.0
	MinMarginUsage    = 0.1
	MaxMarginUsage    = 1.0
	MinPositionSize   = 10.0
	MaxPositionSize   = 1000.0
	MinConfidence     = 50
	MaxConfidence     = 100
)

// SAFE DEFAULTS FOR AN UNSET RISK FIELD (P0 follow-up, 2026-08-17).
//
// ClampLimits used to treat "unset" and "explicitly low" identically: a zero
// value was raised only to the RANGE FLOOR (R:R 1.0 / confidence 50), which is
// the LOOSEST setting the system permits. So a strategy that simply never set
// these ran at the most permissive bar available — unset silently WIDENED risk.
//
// An absent value now resolves to the RESEARCHED value instead
// (docs/VL-DAYPLAN-FULL-SPEC.md: R:R ≥ 3.0, confidence ≥ 65). An EXPLICIT value
// is untouched apart from the existing range clamp, so an owner who deliberately
// configures 1.5 still gets 1.5 — this changes the default, never a choice.
const (
	SafeDefaultMinRiskReward = 3.0
	// SafeDefaultMinConfidence — 6.1 (final-bundle 2026-08-19): ONE default,
	// shared by the ClampLimits gate default AND the futures prompt default.
	// Was 65 here vs a literal 60 in engine_prompt_futures.go — an UNSET
	// strategy was told "open ≥60" and then judged at ≥65, silently discarding
	// 60-64 setups (PR #54 finding). Aligned to 60 per owner ruling; an
	// explicitly stored value (the active strategy stores 60) is untouched.
	SafeDefaultMinConfidence = 60
)

// ClampLimits enforces product-level limits on strategy config to prevent token overflow.
// MaxIndicatorPeriod is the largest allowed indicator period. Beyond this a series
// can never warm up within the fetch/cache depth (2500 cap), so it would silently
// render an EMPTY series — reject at save instead (F11c).
const MaxIndicatorPeriod = 500

// ValidateIndicatorPeriods (F11c) rejects out-of-range indicator periods at save:
// each configured EMA/RSI/ATR/BOLL period must be > 0 and ≤ MaxIndicatorPeriod. An
// absurd value like 9999 is a hard error here, never a silently-empty series.
// Empty period lists are fine (they fall back to the legacy fixed periods).
func (c *StrategyConfig) ValidateIndicatorPeriods() error {
	check := func(name string, ps []int) error {
		for _, p := range ps {
			if p <= 0 || p > MaxIndicatorPeriod {
				return fmt.Errorf("%s period %d is out of range — each indicator period must be between 1 and %d", name, p, MaxIndicatorPeriod)
			}
		}
		return nil
	}
	for _, f := range []struct {
		name string
		ps   []int
	}{
		{"EMA", c.Indicators.EMAPeriods},
		{"RSI", c.Indicators.RSIPeriods},
		{"ATR", c.Indicators.ATRPeriods},
		{"BOLL", c.Indicators.BOLLPeriods},
	} {
		if err := check(f.name, f.ps); err != nil {
			return err
		}
	}
	return nil
}

func (c *StrategyConfig) ClampLimits() {
	c.NormalizeProductSchema()

	// Clamp static coins
	if len(c.CoinSource.StaticCoins) > MaxCandidateCoins {
		c.CoinSource.StaticCoins = c.CoinSource.StaticCoins[:MaxCandidateCoins]
	}

	// Clamp kline count
	if c.Indicators.Klines.PrimaryCount < MinKlineCount {
		c.Indicators.Klines.PrimaryCount = MinKlineCount
	}
	if c.Indicators.Klines.PrimaryCount > MaxKlineCount {
		c.Indicators.Klines.PrimaryCount = MaxKlineCount
	}
	if c.Indicators.Klines.LongerCount > MaxKlineCount {
		c.Indicators.Klines.LongerCount = MaxKlineCount
	}

	// Clamp timeframes
	if len(c.Indicators.Klines.SelectedTimeframes) > MaxTimeframes {
		c.Indicators.Klines.SelectedTimeframes = c.Indicators.Klines.SelectedTimeframes[:MaxTimeframes]
	}

	// Clamp max positions
	if c.RiskControl.MaxPositions < 1 {
		c.RiskControl.MaxPositions = 1
	}
	if c.RiskControl.MaxPositions > MaxPositions {
		c.RiskControl.MaxPositions = MaxPositions
	}

	// Clamp leverage limits to the same bounds as the manual config UI.
	if c.RiskControl.BTCETHMaxLeverage < MinLeverage {
		c.RiskControl.BTCETHMaxLeverage = MinLeverage
	}
	if c.RiskControl.BTCETHMaxLeverage > MaxBTCETHLeverage {
		c.RiskControl.BTCETHMaxLeverage = MaxBTCETHLeverage
	}
	if c.RiskControl.AltcoinMaxLeverage < MinLeverage {
		c.RiskControl.AltcoinMaxLeverage = MinLeverage
	}
	if c.RiskControl.AltcoinMaxLeverage > MaxAltLeverage {
		c.RiskControl.AltcoinMaxLeverage = MaxAltLeverage
	}

	// Clamp position value ratio limits.
	if c.RiskControl.BTCETHMaxPositionValueRatio < MinPositionRatio {
		c.RiskControl.BTCETHMaxPositionValueRatio = MinPositionRatio
	}
	if c.RiskControl.BTCETHMaxPositionValueRatio > MaxPositionRatio {
		c.RiskControl.BTCETHMaxPositionValueRatio = MaxPositionRatio
	}
	if c.RiskControl.AltcoinMaxPositionValueRatio < MinPositionRatio {
		c.RiskControl.AltcoinMaxPositionValueRatio = MinPositionRatio
	}
	if c.RiskControl.AltcoinMaxPositionValueRatio > MaxPositionRatio {
		c.RiskControl.AltcoinMaxPositionValueRatio = MaxPositionRatio
	}

	// Clamp risk parameters and entry requirements.
	// UNSET (zero) → the researched default, NOT the range floor. See the
	// SafeDefault* block: the old behavior made "never configured" the loosest
	// possible setting.
	if c.RiskControl.MinRiskRewardRatio == 0 {
		c.RiskControl.MinRiskRewardRatio = SafeDefaultMinRiskReward
	}
	if c.RiskControl.MinRiskRewardRatio < MinRiskReward {
		c.RiskControl.MinRiskRewardRatio = MinRiskReward
	}
	if c.RiskControl.MinRiskRewardRatio > MaxRiskReward {
		c.RiskControl.MinRiskRewardRatio = MaxRiskReward
	}
	if c.RiskControl.MaxMarginUsage < MinMarginUsage {
		c.RiskControl.MaxMarginUsage = MinMarginUsage
	}
	if c.RiskControl.MaxMarginUsage > MaxMarginUsage {
		c.RiskControl.MaxMarginUsage = MaxMarginUsage
	}
	if c.RiskControl.MinPositionSize < MinPositionSize {
		c.RiskControl.MinPositionSize = MinPositionSize
	}
	if c.RiskControl.MinPositionSize > MaxPositionSize {
		c.RiskControl.MinPositionSize = MaxPositionSize
	}
	if c.RiskControl.MinConfidence == 0 {
		c.RiskControl.MinConfidence = SafeDefaultMinConfidence
	}
	if c.RiskControl.MinConfidence < MinConfidence {
		c.RiskControl.MinConfidence = MinConfidence
	}
	if c.RiskControl.MinConfidence > MaxConfidence {
		c.RiskControl.MinConfidence = MaxConfidence
	}
}

// NormalizeProductSchema keeps saved strategy JSON aligned with the product
// editor schema. LLMs may emit user-facing labels such as "AI500" (a legacy
// source that now degrades to static); persistence
// must use the exact frontend/backend enum values.
func (c *StrategyConfig) NormalizeProductSchema() {
	c.StrategyType = normalizeStrategyType(c.StrategyType)
	c.CoinSource.SourceType = normalizeCoinSourceType(c.CoinSource.SourceType)
	switch c.CoinSource.SourceType {
	case "ai500", "oi_top", "oi_low", "hyper_all", "hyper_main", "mixed":
		// D2-DEAD (item 12): these sources were backed by the deleted legacy
		// provider; stored rows degrade to the static coin list.
		c.CoinSource.SourceType = "static"
	case "":
		c.CoinSource.SourceType = "static"
	}

	c.CoinSource.StaticCoins = normalizeSymbols(c.CoinSource.StaticCoins)
	c.CoinSource.ExcludedCoins = normalizeSymbols(c.CoinSource.ExcludedCoins)
	c.Indicators.Klines.PrimaryTimeframe = normalizeTimeframe(c.Indicators.Klines.PrimaryTimeframe)
	c.Indicators.Klines.LongerTimeframe = normalizeTimeframe(c.Indicators.Klines.LongerTimeframe)
	c.Indicators.Klines.SelectedTimeframes = normalizeTimeframes(c.Indicators.Klines.SelectedTimeframes)
	if len(c.Indicators.Klines.SelectedTimeframes) > 0 {
		c.Indicators.Klines.EnableMultiTimeframe = true
	}
}

func normalizeStrategyType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "grid", "grid_strategy", "grid-trading", "grid trading", "grid_trading", "网格", "网格策略", "网格交易":
		return "grid_trading"
	case "", "ai", "ai_strategy", "ai-trading", "ai trading", "ai_trading", "ai策略", "ai 策略", "ai交易策略", "ai智能策略":
		return "ai_trading"
	default:
		return value
	}
}

func normalizeCoinSourceType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	compact := strings.NewReplacer(" ", "", "_", "", "-", "", "数据源", "", "选币", "", "币种", "").Replace(value)
	switch {
	case compact == "":
		return ""
	case strings.Contains(compact, "ai500"):
		return "ai500"
	case strings.Contains(compact, "oitop") || strings.Contains(value, "oi top") || strings.Contains(value, "持仓量最高") || strings.Contains(value, "持仓量靠前"):
		return "oi_top"
	case strings.Contains(compact, "oilow") || strings.Contains(value, "oi low") || strings.Contains(value, "持仓量最低") || strings.Contains(value, "持仓量较低"):
		return "oi_low"
	case strings.Contains(value, "static") || strings.Contains(value, "固定") || strings.Contains(value, "静态"):
		return "static"
	default:
		return value
	}
}

func normalizeSymbols(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range splitLooseStringList(values) {
		value = strings.TrimSpace(value)
		// Task 12 / Cluster D — CME futures symbols keep their case
		// (NQ.c.0 not NQ.C.0). Uppercasing breaks the Databento
		// continuous symbology convention. Crypto path unchanged.
		if isCMEFuturesSymbol(value) {
			value = strings.Trim(value, "，,;； ")
		} else {
			value = strings.ToUpper(value)
			value = strings.Trim(value, "，,;； ")
		}
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// isCMEFuturesSymbol mirrors market.IsCMEFuturesSymbol. Duplicated here
// because store/ deliberately does not depend on market/. Keep the two
// in lockstep — see market/futures_symbol.go for the canonical version
// + documentation.
func isCMEFuturesSymbol(symbol string) bool {
	s := strings.TrimSpace(symbol)
	if s == "" {
		return false
	}
	if strings.Contains(s, ".c.") {
		return true
	}
	upper := strings.ToUpper(s)
	if _, ok := cmeFuturesRootsStore[upper]; ok {
		return true
	}
	for root := range cmeFuturesRootsStore {
		if strings.HasPrefix(upper, root+".") {
			return true
		}
		if strings.HasPrefix(upper, root) && len(upper) == len(root)+2 {
			tail := upper[len(root):]
			if isContractMonthStore(root, tail[0]) && tail[1] >= '0' && tail[1] <= '9' {
				return true
			}
		}
	}
	return false
}

// cmeFuturesRootsStore mirrors market.cmeFuturesRoots. See note on
// isCMEFuturesSymbol above.
var cmeFuturesRootsStore = map[string]struct{}{
	"NQ":  {},
	"MNQ": {},
	"ES":  {},
	"MES": {},
	"RTY": {},
	"M2K": {},
	"YM":  {},
	"MYM": {},
	"CL":  {},
	"MCL": {},
	"NG":  {},
	"GC":  {},
	"MGC": {},
	"SI":  {},
	"ZB":  {},
	"ZN":  {},
	"ZF":  {},
	"ZT":  {},
}

func isQuarterlyMonthStore(b byte) bool {
	switch b {
	case 'H', 'M', 'U', 'Z':
		return true
	}
	return false
}

// futuresMonthCodesStore mirrors market.futuresMonthCodes — per-root month codes
// so contract-code recognition is family-correct: index/Treasury stay quarterly
// (NQF6 not matched) while energy lists all 12 (NGF6 matched). Keep in sync with
// cmeFuturesRootsStore (every root needs an entry).
var futuresMonthCodesStore = map[string]string{
	"NQ": "HMUZ", "MNQ": "HMUZ", "ES": "HMUZ", "MES": "HMUZ",
	"RTY": "HMUZ", "M2K": "HMUZ", "YM": "HMUZ", "MYM": "HMUZ",
	"ZB": "HMUZ", "ZN": "HMUZ", "ZF": "HMUZ", "ZT": "HMUZ",
	"CL": "FGHJKMNQUVXZ", "MCL": "FGHJKMNQUVXZ", "NG": "FGHJKMNQUVXZ",
	"GC": "GJMQVZ", "MGC": "GJMQVZ", "SI": "FHKNUZ",
}

// isContractMonthStore mirrors market.isContractMonth.
func isContractMonthStore(root string, b byte) bool {
	return strings.IndexByte(futuresMonthCodesStore[root], b) >= 0
}

func normalizeTimeframes(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range splitLooseStringList(values) {
		tf := normalizeTimeframe(value)
		if tf == "" || seen[tf] {
			continue
		}
		seen[tf] = true
		out = append(out, tf)
	}
	return out
}

func splitLooseStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	joined := strings.TrimSpace(strings.Join(values, ","))
	if strings.HasPrefix(joined, "[") && strings.HasSuffix(joined, "]") {
		var parsed []string
		if err := json.Unmarshal([]byte(joined), &parsed); err == nil {
			return parsed
		}
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			var parsed []string
			if err := json.Unmarshal([]byte(value), &parsed); err == nil {
				parts = append(parts, parsed...)
				continue
			}
		}
		value = strings.Trim(value, "[]")
		for _, part := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == '，' || r == ';' || r == '；' || r == '\n'
		}) {
			part = strings.Trim(strings.TrimSpace(part), "\"'")
			if part != "" {
				parts = append(parts, part)
			}
		}
	}
	return parts
}

func normalizeTimeframe(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(value, "\"'，,。 ")
	if value == "" {
		return ""
	}
	aliases := map[string]string{
		"1分钟":  "1m",
		"3分钟":  "3m",
		"5分钟":  "5m",
		"15分钟": "15m",
		"30分钟": "30m",
		"1小时":  "1h",
		"2小时":  "2h",
		"4小时":  "4h",
		"6小时":  "6h",
		"8小时":  "8h",
		"12小时": "12h",
		"1天":   "1d",
		"3天":   "3d",
		"1周":   "1w",
	}
	if alias, ok := aliases[value]; ok {
		return alias
	}
	allowed := map[string]bool{
		"1m": true, "3m": true, "5m": true, "15m": true, "30m": true,
		"1h": true, "2h": true, "4h": true, "6h": true, "8h": true, "12h": true,
		"1d": true, "3d": true, "1w": true,
	}
	if !allowed[value] {
		return ""
	}
	return value
}

// MergeStrategyConfig applies a partial JSON-style patch onto a full strategy config.
// Nested objects are merged recursively so omitted fields keep their previous values.
func MergeStrategyConfig(base StrategyConfig, patch map[string]any) (StrategyConfig, error) {
	baseJSON, err := json.Marshal(base)
	if err != nil {
		return StrategyConfig{}, err
	}

	var mergedMap map[string]any
	if err := json.Unmarshal(baseJSON, &mergedMap); err != nil {
		return StrategyConfig{}, err
	}

	normalizeStrategyConfigPatch(patch)
	if fmt.Sprint(patch["strategy_type"]) == "grid_trading" {
		ensureDefaultGridConfigMap(mergedMap)
	}
	mergeJSONMaps(mergedMap, patch)

	mergedJSON, err := json.Marshal(mergedMap)
	if err != nil {
		return StrategyConfig{}, err
	}

	var merged StrategyConfig
	if err := json.Unmarshal(mergedJSON, &merged); err != nil {
		return StrategyConfig{}, err
	}
	return merged, nil
}

// PreserveAIConfigOnTypeSwitch (F11b) guards against the grid-switch ai_config
// DESTROYER: when a strategy's TYPE changes, the AI-config bundle (coin source,
// indicators, risk control, prompt sections, custom prompt) must NOT be silently
// destroyed. Unless the caller explicitly confirmed the loss, `merged` gets its AI
// bundle restored from `base`, so switching ai→grid (and back) never loses it. With
// confirmed==true (the FE sent it after showing a dialog naming what's lost), the
// caller's merged config stands.
func PreserveAIConfigOnTypeSwitch(base, merged StrategyConfig, confirmed bool) StrategyConfig {
	if confirmed || base.StrategyType == merged.StrategyType {
		return merged
	}
	merged.CoinSource = base.CoinSource
	merged.Indicators = base.Indicators
	merged.RiskControl = base.RiskControl
	merged.PromptSections = base.PromptSections
	merged.CustomPrompt = base.CustomPrompt
	return merged
}

func DefaultGridStrategyConfig() GridStrategyConfig {
	return GridStrategyConfig{
		Symbol:                "MNQ",
		GridCount:             10,
		TotalInvestment:       1000,
		Leverage:              5,
		UpperPrice:            0,
		LowerPrice:            0,
		UseATRBounds:          true,
		ATRMultiplier:         2.0,
		Distribution:          "gaussian",
		MaxDrawdownPct:        15,
		StopLossPct:           5,
		DailyLossLimitPct:     10,
		UseMakerOnly:          true,
		EnableDirectionAdjust: false,
		DirectionBiasRatio:    0.7,
	}
}

func ensureDefaultGridConfigMap(config map[string]any) {
	if config == nil {
		return
	}
	if existing, ok := config["grid_config"].(map[string]any); ok && len(existing) > 0 {
		return
	}
	defaultGrid := DefaultGridStrategyConfig()
	raw, err := json.Marshal(defaultGrid)
	if err != nil {
		return
	}
	var gridMap map[string]any
	if err := json.Unmarshal(raw, &gridMap); err != nil {
		return
	}
	config["grid_config"] = gridMap
}

func normalizeStrategyConfigPatch(patch map[string]any) {
	if patch == nil {
		return
	}

	if gridConfig, hasGrid := patch["grid_config"]; hasGrid && gridConfig != nil {
		if _, hasType := patch["strategy_type"]; !hasType {
			patch["strategy_type"] = "grid_trading"
		}
	}

	aiKeys := []string{"coin_source", "indicators", "risk_control", "prompt_sections", "custom_prompt"}
	for _, key := range aiKeys {
		value, ok := patch[key]
		if !ok {
			continue
		}
		aiConfig, _ := patch["ai_config"].(map[string]any)
		if aiConfig == nil {
			aiConfig = map[string]any{}
			patch["ai_config"] = aiConfig
		}
		aiConfig[key] = value
		delete(patch, key)
	}

	if fmt.Sprint(patch["strategy_type"]) == "grid_trading" {
		delete(patch, "ai_config")
	}

	if _, hasType := patch["strategy_type"]; hasType {
		return
	}
	if gridConfig, hasGrid := patch["grid_config"]; hasGrid && gridConfig != nil {
		patch["strategy_type"] = "grid_trading"
	}
}

func mergeJSONMaps(dst, src map[string]any) {
	for key, srcVal := range src {
		srcMap, srcIsMap := srcVal.(map[string]any)
		dstMap, dstIsMap := dst[key].(map[string]any)
		if srcIsMap && dstIsMap {
			mergeJSONMaps(dstMap, srcMap)
			continue
		}
		dst[key] = srcVal
	}
}

func StrategyClampWarnings(before, after StrategyConfig, lang string) []string {
	if lang != "zh" {
		lang = "en"
	}
	warnings := make([]string, 0, 8)
	appendInt := func(labelZH, labelEN string, from, to int) {
		if from == to {
			return
		}
		if lang == "zh" {
			warnings = append(warnings, fmt.Sprintf("%s 已从 %d 调整为 %d", labelZH, from, to))
			return
		}
		warnings = append(warnings, fmt.Sprintf("%s adjusted from %d to %d", labelEN, from, to))
	}
	appendFloat := func(labelZH, labelEN string, from, to float64) {
		if from == to {
			return
		}
		if lang == "zh" {
			warnings = append(warnings, fmt.Sprintf("%s 已从 %.2f 调整为 %.2f", labelZH, from, to))
			return
		}
		warnings = append(warnings, fmt.Sprintf("%s adjusted from %.2f to %.2f", labelEN, from, to))
	}

	appendInt("最大持仓数", "max_positions", before.RiskControl.MaxPositions, after.RiskControl.MaxPositions)
	appendInt("BTC/ETH 最大杠杆", "btc_eth_max_leverage", before.RiskControl.BTCETHMaxLeverage, after.RiskControl.BTCETHMaxLeverage)
	appendInt("山寨币最大杠杆", "altcoin_max_leverage", before.RiskControl.AltcoinMaxLeverage, after.RiskControl.AltcoinMaxLeverage)
	appendFloat("BTC/ETH 最大仓位价值倍数", "btc_eth_max_position_value_ratio", before.RiskControl.BTCETHMaxPositionValueRatio, after.RiskControl.BTCETHMaxPositionValueRatio)
	appendFloat("山寨币最大仓位价值倍数", "altcoin_max_position_value_ratio", before.RiskControl.AltcoinMaxPositionValueRatio, after.RiskControl.AltcoinMaxPositionValueRatio)
	appendFloat("最小盈亏比", "min_risk_reward_ratio", before.RiskControl.MinRiskRewardRatio, after.RiskControl.MinRiskRewardRatio)
	appendFloat("最大保证金使用率", "max_margin_usage", before.RiskControl.MaxMarginUsage, after.RiskControl.MaxMarginUsage)
	appendFloat("最小开仓金额", "min_position_size", before.RiskControl.MinPositionSize, after.RiskControl.MinPositionSize)
	appendInt("最低置信度", "min_confidence", before.RiskControl.MinConfidence, after.RiskControl.MinConfidence)
	return warnings
}

// StrategyStore strategy storage
type StrategyStore struct {
	db *gorm.DB
}

// Strategy strategy configuration
type Strategy struct {
	ID            string    `gorm:"primaryKey" json:"id"`
	UserID        string    `gorm:"column:user_id;not null;default:'';index" json:"user_id"`
	Name          string    `gorm:"not null" json:"name"`
	Description   string    `gorm:"default:''" json:"description"`
	IsActive      bool      `gorm:"column:is_active;default:false;index" json:"is_active"`
	IsDefault     bool      `gorm:"column:is_default;default:false" json:"is_default"`
	IsPublic      bool      `gorm:"column:is_public;default:false;index" json:"is_public"`    // whether visible in strategy market
	ConfigVisible bool      `gorm:"column:config_visible;default:true" json:"config_visible"` // whether config details are visible
	Config        string    `gorm:"not null;default:'{}'" json:"config"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (Strategy) TableName() string { return "strategies" }

// StrategyConfig strategy configuration details (JSON structure)
type StrategyConfig struct {
	// Strategy type: "ai_trading" (default) or "grid_trading"
	StrategyType string `json:"strategy_type,omitempty"`

	// language setting: "zh" for Chinese, "en" for English
	// This determines the language used for data formatting and prompt generation
	Language string `json:"language,omitempty"`

	// PromptVariant selects the live AI prompt mode (balanced / aggressive /
	// conservative / scalping / futures). Persisted per-strategy; when EMPTY the
	// live loop falls back to the venue rule (ninjatrader→futures, else balanced)
	// so existing strategies (no variant saved) are byte-identical. Prompt-layer
	// only — it does NOT touch any risk gate or the live-account block.
	PromptVariant string `json:"prompt_variant,omitempty"`

	// AI trading configuration fields are kept on the Go struct for engine
	// compatibility, but JSON persistence nests them under ai_config.
	CoinSource     CoinSourceConfig     `json:"-"`
	Indicators     IndicatorConfig      `json:"-"`
	CustomPrompt   string               `json:"-"`
	RiskControl    RiskControlConfig    `json:"-"`
	PromptSections PromptSectionsConfig `json:"-"`

	// Grid trading configuration (only used when StrategyType == "grid_trading")
	GridConfig *GridStrategyConfig `json:"grid_config,omitempty"`

	// Publish settings are shared by AI and grid strategies. The database still
	// stores the authoritative booleans on Strategy, but config JSON may carry
	// this object for agent/frontend schema consistency.
	PublishConfig *PublishStrategyConfig `json:"publish_config,omitempty"`

	// DayPlan is the optional per-strategy Day Plan settings block (P0.1). ROOT
	// placement (sibling of strategy_type) so a grid switch — which drops
	// ai_config — never drops it (RECON #1). Additive + defaults-off: a nil
	// pointer emits no day_plan key, keeping every existing strategy
	// byte-identical.
	DayPlan *DayPlanConfig `json:"day_plan,omitempty"`

	// Regime (G1, regime wave 2026-08-21) — the regime gates' Studio block.
	// Additive + nil-pointer-safe: a nil block emits no regime key, keeping
	// every existing strategy byte-identical. Pointer fields resolve shipped
	// defaults in the accessors (HTFVetoEnabled: nil → ON — dispatch 1.3).
	Regime *RegimeConfig `json:"regime,omitempty"`
}

// RegimeConfig holds the regime-wave Studio toggles (G1 HTF veto now; G4
// transition stand-down joins later in the wave).
type RegimeConfig struct {
	// HTFVeto: refuse NEW entries opposing the CONFIRMED HTF trend (G2
	// structure). nil → ON (shipped default per dispatch 1.3); false = today's
	// pre-wave behavior.
	HTFVeto *bool `json:"htf_veto,omitempty"`
	// TransitionStanddown (G4): pause plan-direction entries while an
	// unconfirmed counter-trend CHoCH/MSS is outstanding. nil → ON; false =
	// today's pre-wave behavior.
	TransitionStanddown *bool `json:"transition_standdown,omitempty"`
}

// HTFVetoEnabled resolves the shipped default (nil/absent → ON).
func (c *StrategyConfig) HTFVetoEnabled() bool {
	on, _ := ResolveHTFVeto(c)
	return on
}

// TransitionStanddownEnabled resolves the shipped default (nil/absent → ON).
func (c *StrategyConfig) TransitionStanddownEnabled() bool {
	if c.Regime == nil || c.Regime.TransitionStanddown == nil {
		return true
	}
	return *c.Regime.TransitionStanddown
}

// AIStrategyConfig contains fields only used by AI trading strategies.
type AIStrategyConfig struct {
	CoinSource     CoinSourceConfig     `json:"coin_source"`
	Indicators     IndicatorConfig      `json:"indicators"`
	CustomPrompt   string               `json:"custom_prompt,omitempty"`
	RiskControl    RiskControlConfig    `json:"risk_control"`
	PromptSections PromptSectionsConfig `json:"prompt_sections,omitempty"`
}

// PublishStrategyConfig contains settings shared by all strategy types.
type PublishStrategyConfig struct {
	IsPublic      bool `json:"is_public"`
	ConfigVisible bool `json:"config_visible"`
}

// MarshalJSON writes the product-facing strategy schema:
// strategy_type + grid_config or ai_config + shared publish_config.
func (c StrategyConfig) MarshalJSON() ([]byte, error) {
	strategyType := strings.TrimSpace(c.StrategyType)
	if strategyType == "" {
		strategyType = "ai_trading"
	}

	out := struct {
		StrategyType  string                 `json:"strategy_type"`
		Language      string                 `json:"language,omitempty"`
		PromptVariant string                 `json:"prompt_variant,omitempty"`
		AIConfig      *AIStrategyConfig      `json:"ai_config,omitempty"`
		GridConfig    *GridStrategyConfig    `json:"grid_config,omitempty"`
		PublishConfig *PublishStrategyConfig `json:"publish_config,omitempty"`
		DayPlan       *DayPlanConfig         `json:"day_plan,omitempty"`
		Regime        *RegimeConfig          `json:"regime,omitempty"`
	}{
		StrategyType:  strategyType,
		Language:      c.Language,
		PromptVariant: strings.TrimSpace(c.PromptVariant),
		PublishConfig: c.PublishConfig,
		DayPlan:       c.DayPlan,
		Regime:        c.Regime,
	}

	if strategyType == "grid_trading" {
		out.GridConfig = c.GridConfig
	} else {
		out.AIConfig = &AIStrategyConfig{
			CoinSource:     c.CoinSource,
			Indicators:     c.Indicators,
			CustomPrompt:   c.CustomPrompt,
			RiskControl:    c.RiskControl,
			PromptSections: c.PromptSections,
		}
	}

	return json.Marshal(out)
}

// UnmarshalJSON accepts both the new nested schema and old flat configs. Old
// top-level AI fields are normalized into the Go compatibility fields.
func (c *StrategyConfig) UnmarshalJSON(data []byte) error {
	type rawStrategyConfig struct {
		StrategyType  string                 `json:"strategy_type"`
		Language      string                 `json:"language"`
		PromptVariant string                 `json:"prompt_variant"`
		AIConfig      *AIStrategyConfig      `json:"ai_config"`
		GridConfig    *GridStrategyConfig    `json:"grid_config"`
		PublishConfig *PublishStrategyConfig `json:"publish_config"`
		DayPlan       *DayPlanConfig         `json:"day_plan"`
		Regime        *RegimeConfig          `json:"regime"`

		CoinSource     *CoinSourceConfig     `json:"coin_source"`
		Indicators     *IndicatorConfig      `json:"indicators"`
		CustomPrompt   *string               `json:"custom_prompt"`
		RiskControl    *RiskControlConfig    `json:"risk_control"`
		PromptSections *PromptSectionsConfig `json:"prompt_sections"`
	}

	var raw rawStrategyConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	c.StrategyType = raw.StrategyType
	c.Language = raw.Language
	c.PromptVariant = strings.TrimSpace(raw.PromptVariant)
	c.GridConfig = raw.GridConfig
	c.PublishConfig = raw.PublishConfig
	c.DayPlan = raw.DayPlan
	c.Regime = raw.Regime

	if raw.AIConfig != nil {
		c.CoinSource = raw.AIConfig.CoinSource
		c.Indicators = raw.AIConfig.Indicators
		c.CustomPrompt = raw.AIConfig.CustomPrompt
		c.RiskControl = raw.AIConfig.RiskControl
		c.PromptSections = raw.AIConfig.PromptSections
	} else {
		if raw.CoinSource != nil {
			c.CoinSource = *raw.CoinSource
		}
		if raw.Indicators != nil {
			c.Indicators = *raw.Indicators
		}
		if raw.CustomPrompt != nil {
			c.CustomPrompt = *raw.CustomPrompt
		}
		if raw.RiskControl != nil {
			c.RiskControl = *raw.RiskControl
		}
		if raw.PromptSections != nil {
			c.PromptSections = *raw.PromptSections
		}
	}

	if strings.TrimSpace(c.StrategyType) == "" && c.GridConfig != nil {
		c.StrategyType = "grid_trading"
	}
	return nil
}

// DayPlanConfig is the per-strategy Day Plan settings block (spec PAGE 2 field
// list). Additive + defaults-off: a nil *DayPlanConfig (absent day_plan) leaves
// an existing strategy byte-identical, and PlanEnabled=false is the master
// switch even when the block is present. Lives at ROOT of StrategyConfig.
// PictureHtf default timing knobs (DEFAULTS-SANE fold, DS-105 2026-09-25).
// Sized from the production trace (PR #212 STEP 1 measurements + code read):
// the evaluator's window is anchored at the 5m interval after the confirming
// H1 close (nextFiveMBoundary); the live sink admits frames up to 30s old
// (LiveFrameMaxAgeMs); a dropped boundary frame (measured at EVERY hour
// storm) is recoverable only by the successor completed 5m frame, whose
// receipt sits one 5m interval + the sink admission into the window (330s
// worst). 360s = 330s worst + 30s margin; 30s freshness admits exactly every
// frame the sink admitted. FLOOR-pinned in trader/picture_htf_floor_pins_test.go.
const (
	PictureHtfDefaultEntryWindowSec = 360
	PictureHtfDefaultFreshnessSec   = 30
)

// PictureHtfConfig (W-PICTURE-HTF, 2026-09-19) — the named SIM entry mode's
// knobs. The explicit defaults are ENGINEERING DEFAULTS chosen to translate the
// owner's two pictures into repeatable rules; they are not research-proven
// optimums (the Guide says the same). Zero values mean "use the default".
type PictureHtfConfig struct {
	Enabled        bool    `json:"enabled"`
	TickSize       float64 `json:"tick_size,omitempty"`        // default 0.25 (MNQ)
	PivotWindow    int     `json:"pivot_window,omitempty"`     // default 120 completed 4H candles
	SwingLookback  int     `json:"swing_lookback,omitempty"`   // default 24 completed 5m candles
	EntryWindowSec int     `json:"entry_window_sec,omitempty"` // default 360s from the new 5m interval start
	FreshnessSec   int     `json:"freshness_sec,omitempty"`    // default 30s max data age at evaluation
	MinRR          float64 `json:"min_rr,omitempty"`           // 0 = the strategy's configured min R:R
}

// PictureHtfResolved returns the mode's effective knobs (defaults applied).
func PictureHtfResolved(c *PictureHtfConfig) PictureHtfConfig {
	out := PictureHtfConfig{Enabled: c != nil && c.Enabled}
	if c == nil {
		c = &PictureHtfConfig{}
	}
	out.TickSize, out.PivotWindow, out.SwingLookback, out.EntryWindowSec, out.FreshnessSec, out.MinRR =
		c.TickSize, c.PivotWindow, c.SwingLookback, c.EntryWindowSec, c.FreshnessSec, c.MinRR
	if out.TickSize <= 0 {
		out.TickSize = 0.25
	}
	if out.PivotWindow <= 0 {
		out.PivotWindow = 120
	}
	if out.SwingLookback <= 0 {
		out.SwingLookback = 24
	}
	if out.EntryWindowSec <= 0 {
		out.EntryWindowSec = PictureHtfDefaultEntryWindowSec
	}
	if out.FreshnessSec <= 0 {
		out.FreshnessSec = PictureHtfDefaultFreshnessSec
	}
	return out
}

type DayPlanConfig struct {
	StructuralStop *StructuralStopConfig `json:"structural_stop,omitempty"`
	// PlanEnabled is the master switch (default false = off).
	PlanEnabled bool `json:"plan_enabled"`
	// PlannerModel is the reasoner binding from the multi-key registry; empty
	// falls back to the strategy's primary model (RECON #9).
	PlannerModel string `json:"planner_model,omitempty"`
	// PlanMode: advisory (default) | direction | strict. Promotion by evidence.
	PlanMode string `json:"plan_mode,omitempty"`
	// FadeORWideK (W2, 2026-09-10) overrides exclusion (a)'s k — "opening range
	// wider than k× the prior-session median". Zero means the C5 own-tape
	// default (p80/median = 1.28, n=13). A LABEL knob: it changes what is
	// recorded, never what is armed.
	FadeORWideK float64 `json:"fade_or_wide_k,omitempty"`
	// OneSetupEnabled (dispatch 102, 2026-09-10) — the book arms ONE play (the
	// fade) at the best level near price, only on a permitted day; the follow
	// side is recorded, never armed. *bool: nil = ON (the [O] default), an
	// explicit false restores today's wide book byte-identically (E2). A plain
	// bool would read an unset strategy as OFF — the plausible zero A24 forbids.
	OneSetupEnabled *bool `json:"one_setup_enabled,omitempty"`
	// PictureHtf (W-PICTURE-HTF, 2026-09-19) — the owner's two-picture method
	// as a named SIM entry mode. nil = OFF. When ON, this strategy's entry
	// selection is the DETERMINISTIC picture path (4H body levels → H1 close
	// break → next-5m entry through the shared execution gate) and the AI
	// provides commentary + momentum context instead of authoring fade entries.
	PictureHtf *PictureHtfConfig `json:"picture_htf,omitempty"`
	// PlannerContract (WAVE PLANNER A3, 2026-09-25) — the prompt, the
	// validator and the executor are ONE contract: confirming-close authorship,
	// planned_order entry policy, nonzero-risk economics, the REJECT composed-stop
	// exception. *bool: nil = ON (the shipped default); an explicit false renders
	// the pre-A3 prompt bytes (pinned by TestW3PlannerPromptLegacyPolicyByteIdentical).
	PlannerContract *bool `json:"planner_contract,omitempty"`
	// OneSetupMinGrade — the lowest merged-candidate grade the best level may
	// carry ("A+" | "A" | "B" | "C"); empty = B [O].
	OneSetupMinGrade string `json:"one_setup_min_grade,omitempty"`
	// PlannerTimeframes are the structure-summary TFs (default D,4h,1h,15m).
	PlannerTimeframes []string `json:"planner_timeframes,omitempty"`
	// StructureMap (S1, 2026-09-16) — the STRUCTURE table (D/4h/1h, bias only,
	// never an entry) computed at each planner read, stamped on the doc and
	// rendered as a prompt section. nil/false = OFF (the shipped default; a
	// saved false and an unset knob read the same — OFF is the zero, honestly).
	// W-KNOB-PRUNE (2026-09-18): FOLDED — the Studio control is gone (advisory
	// text only, R25 pending); the stored value is still honoured at read and
	// logged once at trader load (FoldedKnobLines).
	StructureMap *bool `json:"structure_map,omitempty"`
	// ProximityFilterATR: day-trade lock, 0.5–3.0 (default 1.5).
	ProximityFilterATR float64 `json:"proximity_filter_atr,omitempty"`
	// MaxLevels: level table cap, 3–12 (default 8).
	MaxLevels int `json:"max_levels,omitempty"`
	// ScenarioCap: scenarios cap, 1–5. W-KNOB-PRUNE (2026-09-18): FOLDED —
	// the constant DefaultScenarioCap (3) unless a stored value says otherwise;
	// no Studio control. Stored values are honoured (the owner's MNQ strategy
	// stores 5) and logged once at trader load.
	ScenarioCap int `json:"scenario_cap,omitempty"`
	// HtfSeats (S3, 2026-09-16): how many HTF swing/zone levels seatHTF may
	// promote into the ENTRY table, 0–6. A POINTER because 0 is a legal value
	// (no HTF seating at all); nil = the shipped default 2 (today's behaviour).
	HtfSeats *int `json:"htf_seats,omitempty"`
	// htf_score_multiplier REMOVED by W-KNOB-PRUNE (owner ruling 2026-09-18):
	// the HTF weight is the constant kernel.HTFScoreMultiplier (1.0 — Q-C: 1.2
	// promoted a group that holds LESS). Old stored JSON still loads
	// (encoding/json ignores unknown fields); no strategy stored it.
	// FlipReread (W-FLIP-REREAD, 2026-09-17): when a flip condition fires, the
	// plan still goes DORMANT exactly as before, and ON adds ONE free planner
	// re-read in the flipped direction (trigger structure_flip). OFF = today's
	// behaviour byte-identical.
	FlipReread bool `json:"flip_reread,omitempty"`
	// DeathReread (W-DEATH-REREAD, 2026-09-18, owner ruling 12:3x CT "fix all"):
	// when a DEATH condition fires, the plan still goes DORMANT exactly as
	// before, and ON adds ONE BUDGETED planner re-read (trigger death_replan —
	// it SPENDS one unit of the class-35 replan budget, unlike the flip read)
	// that authors a FRESH plan, bias free, with the death evidence in the read
	// prompt. A POINTER because the default is ON: nil = ON, explicit false =
	// today's behaviour byte-identical (dormant only).
	DeathReread *bool `json:"death_reread,omitempty"`
	// DeathRereadRetryMin (FIX-PLANNER 2026-09-26, item 4): the self-backoff a
	// FAILED death re-read holds before retrying the same dormant row, in
	// minutes. nil = today's value (wake_min_interval_min — the shared wake
	// throttle the hold currently borrows); an explicit value replaces ONLY the
	// death re-read hold. wake_min_interval_min and every other throttle are
	// untouched. A POINTER because the default is "today", and 0 is a legal
	// value (retry next cycle).
	DeathRereadRetryMin *int `json:"death_reread_retry_min,omitempty"`
	// PlannerFreshTape (A6, planner-born-dead wave 2026-09-25): when an attempt
	// is refused born-dead / flip-met, attempt N+1's prompt carries the
	// COMPLETED bars between the read clock and the refusal (last 30 completed
	// 1m closes + last 6 completed 5m closes) and the breached condition
	// verbatim, so the re-author reads the tape that exists now instead of
	// retrying blind against the stale read. A POINTER because the default is
	// ON: nil = ON, explicit false = today's behaviour byte-identical.
	PlannerFreshTape *bool `json:"planner_fresh_tape,omitempty"`
	// T1Currencies (W-T1-CURRENCIES, 2026-09-18): the currencies whose T1
	// (red) calendar events HARD-block entries (±T1BlackoutMinutes). Empty/nil
	// = the shipped default ["USD"]. An explicit ["ALL"] (or ["*"]) restores
	// the pre-wave behaviour: every T1 event hard-blocks. T1 events in any
	// other currency stay VISIBLE as an advisory line (plan no_trade + card +
	// prompt) and never gate. Born the night the BOJ rate decision (JPY)
	// blacked out the MNQ bot.
	T1Currencies []string `json:"t1_currencies,omitempty"`
	// AcceptanceRule — W-KNOB-PRUNE (2026-09-18): FOLDED. There is exactly one
	// rule (DefaultAcceptanceRule, 1×5m close); AcceptanceRuleFor returns it
	// regardless of what is stored (the old resolver already mapped every other
	// vocabulary onto it). Field kept so old JSON round-trips; no Studio control.
	AcceptanceRule string `json:"acceptance_rule,omitempty"`
	// ReplanCap: re-reads per session, 0–4. W1 (settings truth, 2026-09-23):
	// a POINTER because 0 is a legal value — nil/absent = the shipped default 2,
	// an explicit 0 = no re-plan at all, N = N. As an int, 0 could never be
	// stored (omitempty dropped it) and the resolver read a hand-set 0 as 2.
	// Resolved ONLY by ResolveReplanCap (store/resolve_source.go).
	ReplanCap *int `json:"replan_cap,omitempty"`
	// SessionsEnabled: subset of NY | ASIA | LONDON (default [NY]); each other
	// session earns enablement via replay + NY match-rate evidence.
	SessionsEnabled []string `json:"sessions_enabled,omitempty"`
	// ApprovalRequired: OFF (default) = fully automatic.
	ApprovalRequired bool `json:"approval_required"`
	// EveningDigest — W-KNOB-PRUNE (2026-09-18): FOLDED. Constant OFF unless a
	// stored true says otherwise (every live strategy stores true — honoured and
	// logged once). No Studio control; DefaultDayPlanConfig no longer seeds it.
	EveningDigest bool `json:"evening_digest,omitempty"`
	// RealignCap (W13): max AUTO plan re-alignments per plan/session.
	// W-KNOB-PRUNE (2026-09-18): FOLDED into the re-plan section — the constant
	// DefaultRealignCap (5) unless a stored value says otherwise (the owner's
	// MNQ strategy stores 10 — honoured, logged once). No Studio control.
	RealignCap int `json:"realign_cap,omitempty"`
	// last_entry_ct / eod_flat_ct DELETED by W-KNOB-PRUNE (2026-09-18): both
	// were unreachable since the P2 session-scope redesign (2026-08-18) — the
	// live clock resolves per session via last_entry_offset_min /
	// eod_flat_offset_min below. Old stored JSON still loads (unknown fields
	// are ignored); nothing read them (grep in the wave report).
	// Sessions holds minimal per-session overrides; absent/nil fields inherit
	// from the strategy-level values above (⚪ inherit / 🔸 override).
	Sessions []DayPlanSessionOverride `json:"sessions,omitempty"`
	// ── W6 wake wave (2026-08-25) — event-diff planner wake-ups.
	// W-KNOB-PRUNE (2026-09-18): the five per-class switches collapsed into ONE
	// switch, WakeOnLevelEvents (pointer-bool, nil = ON), plus the interval. ON
	// means the level-event wake runs — HTF S/D zones and seated-level
	// invalidation are what it is for (R23/R24); the 15m zone/FVG and iFVG
	// classes ride along at their shipped-ON default, and the HTF order-block
	// class stays OFF unless a legacy stored wake_on_htf_ob=true says otherwise
	// (the owner's MNQ strategy — honoured, logged once).
	WakeOnLevelEvents *bool `json:"wake_on_level_events,omitempty"`
	// LEGACY (read for the mapping only, never written by the Studio again):
	// if the new switch is absent, ON when ANY of the five legacy switches was
	// ON (nil pointers read ON, the plain bool reads its value) — so a strategy
	// that never touched them stays ON, and only an all-five-false config maps
	// to OFF. wake_on_htf_ob additionally keeps its own effect (OB class).
	WakeOn15mZone            *bool `json:"wake_on_15m_zone,omitempty"`
	WakeOnHTFZone            *bool `json:"wake_on_htf_zone,omitempty"`
	WakeOnHTFOB              bool  `json:"wake_on_htf_ob,omitempty"`
	WakeOnSeatedInvalidation *bool `json:"wake_on_seated_invalidation,omitempty"`
	WakeOnIFVG               *bool `json:"wake_on_ifvg,omitempty"`
	// WakeMinIntervalMin — FOLDED (W-KNOB-PRUNE): the constant
	// DefaultWakeMinIntervalMin (30) unless a stored value says otherwise; no
	// Studio control. ≤ 0 → the constant.
	WakeMinIntervalMin int `json:"wake_min_interval_min,omitempty"`
	// seat_1h_zone REMOVED by W-KNOB-PRUNE (owner ruling 2026-09-18, R24: no
	// zone kind/TF beats random). The 1h S/D seat guarantee itself is unchanged
	// and now unconditional (it was ON by default and no strategy stored it);
	// only the switch is gone. Old JSON carrying the field still loads.
	// LevelsFreshByTF (S2, 2026-09-16) — HTF levels grade their freshness on
	// their OWN timeframe bars instead of the 1m-touch ladder. Default false =
	// today's grading byte-identical. W-KNOB-PRUNE (2026-09-18): FOLDED — the
	// verdict removes the knob (S4c: freshness separates nothing) but the
	// owner's MNQ strategy stores true, so the code path stays, the stored
	// value is honoured and logged once; the Studio control is gone. When the
	// owner clears it, a follow-up deletes kernel/levels_fresh_by_tf.go.
	LevelsFreshByTF bool `json:"levels_fresh_by_tf,omitempty"`
	// WriteTimeFeasibility (W-WRITE-TIME-FEASIBILITY, 2026-09-18, owner ruling
	// "fix all" 08:3x CT) — at plan write, every armed scenario runs the SAME
	// predicates the executor's gate-at-arm chain runs (min-SL floor, R:R at arm,
	// geometry). Unarmable scenarios are repair-hinted first; after the last
	// repair attempt they are written with arm.enabled=false + a disabled reason
	// instead of a silent WARN. *bool: nil/unset = ON (the owner's default).
	// Explicit false = today's WARN-only behaviour byte-identical.
	WriteTimeFeasibility *bool `json:"write_time_feasibility,omitempty"`
	// GeometryReferenceLevels (W-GEOMETRY-REFUSAL, 2026-09-18, carried here by
	// W-WRITE-TIME-FEASIBILITY so the merged-head write site and executor share
	// ONE knob — IDENTICAL field name + resolver to DS-102's branch): reference
	// levels with unknown formation close get stable ids, and an empty source tf
	// is a wildcard. nil = ON (default); explicit false = today's behaviour
	// GeometryReferenceLevels (W-GEOMETRY-REFUSAL, 2026-09-18) — the owner ruled
	// the contract fix ON by default ("both fix now", 2026-09-18 08:1x CT):
	// the identity map assigns stable sha ids to session reference levels whose
	// formation close is unknown (ONH/ONL and the other anchor kinds that
	// today emit NULL), and an empty zone-source tf is a wildcard in the frozen
	// geometry match. nil = ON (default); explicit false = today's behaviour
	// byte-identical (OFF).
	GeometryReferenceLevels *bool `json:"geometry_reference_levels,omitempty"`
	// MinScenarioQuality (R4, 2026-08-25) — the per-strategy scenario quality
	// floor (A | B | C). Default C = no restriction (today's behavior,
	// byte-identical). Per-session override below (like min_grade).
	MinScenarioQuality string `json:"min_scenario_quality,omitempty"`
	// min_side_levels REMOVED by owner ruling 2026-08-31 — the per-side count
	// concept is deleted. Old stored JSON carrying the field still loads
	// (encoding/json ignores unknown fields).
	// ConditionStatus (0C shadow demotion, 2026-08-31) — per-condition live|
	// shadow map, resolved session override → base → LIVE → SHADOW env → defaults (fvg_entry
	// and breakout_retest default SHADOW per owner ruling). The ARM SEAM is the
	// only enforcement point; authoring/validation/E8 scoring stay untouched.
	ConditionStatus map[string]string `json:"condition_status,omitempty"`
	// W-EXEC-TRUTH W3 (2026-09-23) — STRICT: follow the plan, enter around the
	// price. EntryPolicyDefault is the entry policy STAMPED at parse on every
	// arm of a NEWLY authored plan (a stored doc is never stamped):
	// market_in_zone (empty = the shipped default) — a LIMIT at the far edge of
	// the planner's economics.entry_zone; planned_order — today's resting order
	// (legal on reject, fvg_entry and sweep_reclaim leg 0 only; anywhere else the
	// arm stays legacy); legacy — stamp nothing (the explicit off: prompt and
	// validator byte-identical to before W3). An unrecognised value resolves to
	// the shipped default and the source says so (ResolveEntryPolicyDefault).
	EntryPolicyDefault string `json:"entry_policy_default,omitempty"`
	// ZoneMaxPts — the widest economics.entry_zone (points) a market_in_zone
	// arm may carry, judged at write. nil/≤0 = 10 (ResolveZoneMaxPts).
	ZoneMaxPts *float64 `json:"zone_max_pts,omitempty"`
	// ZoneRestMaxMin — a resting market_in_zone limit older than this many
	// minutes (from placed_at_ms) is cancelled "zone rest expired" by the
	// executor. nil/≤0 = 30 (ResolveZoneRestMaxMin).
	ZoneRestMaxMin *int `json:"zone_rest_max_min,omitempty"`
	// ZonePlaceWithinPts — WAVE PLANNER B1: a market_in_zone arm whose zone is
	// farther than this many points from the eval price stays armed-unplaced
	// and places when price comes within the bound; a rest-cap expiry returns
	// the row to armed-unplaced instead of dismantling it. nil = 25 (the armed
	// placement band, ResolveZonePlaceWithinPts — ON); 0 = OFF = legacy
	// behaviour, byte-identical.
	ZonePlaceWithinPts *float64 `json:"zone_place_within_pts,omitempty"`
	// MinHoldMin — the floor (minutes) on the RESOLVED hold of an armed
	// market_in_zone time_hold scenario, refused below it at write (new
	// authoring only). nil/≤0 = 3 (ResolveMinHoldMin).
	MinHoldMin *int `json:"min_hold_min,omitempty"`
}

// DayPlanSessionOverride is a minimal per-session override. Every field is a
// pointer: nil means "inherit from the strategy-level DayPlanConfig", a set
// value overrides only that field for the named session.
type DayPlanSessionOverride struct {
	Session        string  `json:"session"` // NY | ASIA | LONDON
	Enable         *bool   `json:"enable,omitempty"`
	ReplanCap      *int    `json:"replan_cap,omitempty"`
	PlanMode       *string `json:"plan_mode,omitempty"`
	AcceptanceRule *string `json:"acceptance_rule,omitempty"` // FOLDED (W-KNOB-PRUNE): read by nothing, one rule exists
	MinGrade       *string `json:"min_grade,omitempty"`       // A | B | C
	MaxTrades      *int    `json:"max_trades,omitempty"`
	// MinScenarioQuality (R4, 2026-08-25) — per-session scenario quality floor
	// (A | B | C); nil inherits the strategy-level value.
	MinScenarioQuality *string `json:"min_scenario_quality,omitempty"`
	// LastEntryOffsetMin: minutes BEFORE this session's end after which NEW
	// entries are refused (P2 session-scope redesign, 2026-08-18). Replaces the
	// old day-scoped 13:00 CT cutoff, which blocked every entry from 13:00 CT to
	// midnight — i.e. permanently blocked Asia evenings. nil → default 15.
	LastEntryOffsetMin *int `json:"last_entry_offset_min,omitempty"`
	// EODFlatOffsetMin: minutes before this session's end at which any open
	// position is force-flattened. Same day-scope disease as last-entry (the
	// 14:45 CT literal would have flattened an Asia position on sight the moment
	// the last-entry fix landed). nil → default 15.
	EODFlatOffsetMin *int `json:"eod_flat_offset_min,omitempty"`
	// ConditionStatus (0C shadow demotion, 2026-08-31) — per-session live|
	// shadow map override; nil inherits the strategy-level map.
	ConditionStatus *map[string]string `json:"condition_status,omitempty"`
}

// DefaultLastEntryOffsetMin / DefaultEODFlatOffsetMin are the session-relative
// defaults (minutes before session end). R-A15 (owner ruling, S-wave
// 2026-08-26): EOD flat = the session END — NY flattens at 14:45 CT (the
// standing R5 ruling), not the drifted 14:30. Last-entry keeps its 15-min
// lead (entries stop 14:30, flat at 14:45).
const (
	DefaultLastEntryOffsetMin = 15
	DefaultEODFlatOffsetMin   = 0
)

// LastEntryOffsetFor resolves the per-session last-entry offset (minutes before
// session end). Override → default. Config only — no caller may carry a literal.
func (c *DayPlanConfig) LastEntryOffsetFor(session string) int {
	v, _ := LastEntryOffsetForWithSource(c, session)
	return v
}

// EODFlatOffsetFor resolves the per-session EOD-flat offset (minutes before
// session end). Override → default.
func (c *DayPlanConfig) EODFlatOffsetFor(session string) int {
	v, _ := EODFlatOffsetForWithSource(c, session)
	return v
}

// SessionOverride returns the named session's override block, or nil. Shared by
// the trader gates, the kernel prompt path, and the API card renderer so all
// three resolve a per-session setting the SAME way.
func (c *DayPlanConfig) SessionOverride(session string) *DayPlanSessionOverride {
	if c == nil {
		return nil
	}
	for i := range c.Sessions {
		if strings.EqualFold(c.Sessions[i].Session, session) {
			return &c.Sessions[i]
		}
	}
	return nil
}

// DefaultAcceptanceRule is the shipped acceptance rule. ENTRY-MECHANICS
// ADDENDUM (2026-08-30): was "2x5m" (two consecutive 5m closes) — the new
// per-condition entry law RESERVES 2x5m for waterfall-class plays, so the
// config-level default becomes the one-close rule. Scenario confirms are
// chosen by the per-condition law (kernel.ValidateEntryLaw), not by this
// string — this only feeds legacy death/consumption evaluation.
const DefaultAcceptanceRule = "5m_close"

// AcceptanceRuleFor resolves the acceptance rule for a session: per-session
// override → strategy-level → the shipped default. Before this existed, the
// per-session override was persisted and rendered but read by NOTHING — every
// consumer went straight to the strategy-level field.
//
// W-KNOB-PRUNE (2026-09-18): FOLDED. The ENTRY-MECHANICS addendum (2026-08-30)
// had already mapped every stored vocabulary ("2x5m", "15m-close", …) onto the
// one-close rule at read, so the dropdown offered one option. There is ONE
// rule; this returns it for every config and every session. The stored fields
// stay readable (old JSON round-trips) and the boot repair still rewrites
// "2x5m" rows. A stored value that is not the rule is logged once
// (FoldedKnobLines) — it never takes effect, and never did since 08-30.
func (c *DayPlanConfig) AcceptanceRuleFor(session string) string {
	return DefaultAcceptanceRule
}

// RepairAcceptanceRuleMigration (ENTRY-MECHANICS ADDENDUM, 2026-08-30) —
// boot-time DB migration aligning stored acceptance rules with the new
// per-condition entry law. The knob census (docs/knob-census, 39a0481e) found
// day_plan.acceptance_rule="2x5m" at the strategy level AND per-session
// acceptance="2x5m" on NY/ASIA/LONDON. Under the new law that string would
// steer the executor prompt + death/consumption evaluation toward the double
// close the law now RESERVES for waterfall-class plays — the prompt would
// contradict the validator (reject loops). Rewrites "2x5m" → "5m_close"
// (the one-close rule) in both spots, idempotently. Runs at boot BEFORE
// LoadTradersFromStore so the loaded config is already aligned.
func (s *StrategyStore) RepairAcceptanceRuleMigration() (baseMigrated, sessionMigrated int, err error) {
	var all []Strategy
	if err := s.db.Order("created_at DESC").Find(&all).Error; err != nil {
		return 0, 0, err
	}
	for i := range all {
		st := &all[i]
		var raw map[string]any
		if err := json.Unmarshal([]byte(st.Config), &raw); err != nil {
			continue
		}
		changed := false
		patchDayPlan := func(dp map[string]any) {
			if v, ok := dp["acceptance_rule"].(string); ok && strings.TrimSpace(v) == "2x5m" {
				dp["acceptance_rule"] = "5m_close"
				baseMigrated++
				changed = true
			}
			if sess, ok := dp["sessions"].([]any); ok {
				for _, sv := range sess {
					if m, ok := sv.(map[string]any); ok {
						if v, ok := m["acceptance_rule"].(string); ok && strings.TrimSpace(v) == "2x5m" {
							m["acceptance_rule"] = "5m_close"
							sessionMigrated++
							changed = true
						}
					}
				}
			}
		}
		// Stored shape has day_plan at the config top level; accept the nested
		// ai_config spelling too so a legacy row can never hide the old string.
		if dp, ok := raw["day_plan"].(map[string]any); ok {
			patchDayPlan(dp)
		}
		if ac, ok := raw["ai_config"].(map[string]any); ok {
			if dp, ok := ac["day_plan"].(map[string]any); ok {
				patchDayPlan(dp)
			}
		}
		if !changed {
			continue
		}
		if b, mErr := json.Marshal(raw); mErr == nil {
			st.Config = string(b)
			if uErr := s.Update(st); uErr != nil {
				return baseMigrated, sessionMigrated, uErr
			}
		}
	}
	return baseMigrated, sessionMigrated, nil
}

// ReplanCapFor resolves the re-read cap for a session: per-session override →
// strategy-level → the shipped default of 2. A 0 is meaningful at BOTH levels
// (no re-plan after death). W1: delegates to ResolveReplanCap — one resolver,
// canon 28 — so the boot line, the card and the gates read one rule.
func (c *DayPlanConfig) ReplanCapFor(session string) int {
	n, _ := ResolveReplanCap(c, session)
	return n
}

// ReplansUsed / MayReplan / ReplansLeft are the ONE definition of the re-plan
// budget. Every consumer — the enforcer, the card and the executor prompt — must
// go through these, so a literal budget can never disagree with the config again
// (installActivePlanProvider once carried a hardcoded 2 and told the AI it had 0
// re-plans left while the card said 2).
//
// SEMANTICS, settled 2026-08-17. The cap counts RE-PLANS, not versions and not
// deaths. Version 1 is the session's first read and costs nothing; each
// subsequent REAL plan version is one re-plan. So:
//
//	replan_cap = N  ⇒  at most N re-plans  ⇒  real versions v1…v(N+1)
//	the (N+1)th death  ⇒  NO-TRADE for the session
//
// The NO-TRADE row is a TERMINAL MARKER, not a re-plan. It consumes a version
// number because the plans table is append-only, which is why cap=4 legitimately
// produces a row labelled "v6": v1…v5 are the five real plans (four re-plans) and
// v6 is the marker. That is correct behaviour, and it is exactly what read as
// "the cap didn't work" on 2026-08-16.

// ── RE-PLAN BUDGET (CLASS 35, 2026-09-01) — counters RECORD events; they do
// not infer them. ─────────────────────────────────────────────────────────────
//
// The budget used to be inferred as version − baseline: every appended row
// counted as a spent re-plan regardless of why it was written. On 2026-09-01
// LONDON the chain was [planner_fail_closed, level_event, dormant:flip,
// level_event ×3] — zero death re-plans, zero owner re-reads — and the
// arithmetic said 5 of 4 spent, so the next death would have fail-closed a
// session whose budget was never touched. Now the two consuming classes
// (death re-plan, owner re-read) RECORD each spend in system_config; every
// other row — the session's scheduled read, level_event / structure_mss wakes
// (fast-market is a reasoning mode of those), owner_reset, dormant/rearm
// markers, fail-closed markers — is free.
//
// The counter is keyed UNDER the chain's reset baseline, so an owner reset
// (new baseline) starts a fresh counter at 0 without the reset path knowing
// about counters; the abandoned chain's spends stay on record.
//
// cap N still means N spends; the NO-TRADE marker still consumes a version
// number (the plans table is append-only) — it just no longer counts.

const (
	// TriggerDeathReplan labels a re-plan written because the active plan's
	// death condition fired (trader death path). SPENDS.
	TriggerDeathReplan = "death_replan"
	// TriggerOwnerReread labels an owner-requested re-read. SPENDS.
	TriggerOwnerReread = "owner_reread"
)

// TriggerSpendsReplan names the ONLY trigger classes that spend budget.
func TriggerSpendsReplan(trigger string) bool {
	switch strings.TrimSpace(trigger) {
	case TriggerDeathReplan, TriggerOwnerReread:
		return true
	}
	return false
}

// ReplanBudget is the resolved budget for one (trader, trade_date, session)
// chain: what was RECORDED as spent, the resolved cap, and the baseline the
// counter is keyed under. The card, the executor prompt, the death gate and
// the owner re-read gate all read this one value.
type ReplanBudget struct {
	Used     int
	Cap      int
	Baseline int
}

// Left is what the card and the executor prompt display.
func (b ReplanBudget) Left() int {
	if n := b.Cap - b.Used; n > 0 {
		return n
	}
	return 0
}

// May reports whether another spending re-plan is allowed. False ⇒ the
// session sits out with a NO-TRADE marker.
func (b ReplanBudget) May() bool { return b.Used < b.Cap }

// ReplansUsedKey is the system_config key holding the recorded spend count for
// one chain (trader, trade_date, session, baseline).
func ReplansUsedKey(traderID, tradeDate, session string, baseline int) string {
	return "dayplan_replans_used:" + traderID + ":" + tradeDate + ":" + session + ":b" + strconv.Itoa(baseline)
}

// replanSpendMu serializes read-modify-write on the counter (both spending
// paths live in this process: the trader cycle and the API re-read handler).
var replanSpendMu sync.Mutex

// readReplansUsed reads one counter. A malformed or negative value can never
// exhaust a session or panic the loop: it reads as 0 (full budget) and is
// logged loudly so the row gets fixed (A24: never a placeholder that reads as
// data — the log line IS the alarm).
func readReplansUsed(st *Store, key string) int {
	raw, _ := st.GetSystemConfig(key)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		logger.Errorf("🧮 replan budget: malformed counter %s=%q — reading as 0 (full budget); fix the system_config row (class 35)", key, raw)
		return 0
	}
	return n
}

// GetReplanBudget resolves the recorded budget for a chain: baseline (reset
// seam) → counter under that baseline → cap from the caller's resolver.
func GetReplanBudget(st *Store, traderID, tradeDate, session string, cap int) ReplanBudget {
	if cap < 0 {
		cap = 0
	}
	if st == nil {
		return ReplanBudget{Cap: cap, Baseline: 1}
	}
	baseline := GetResetBaseline(st, traderID, tradeDate, session)
	return ReplanBudget{
		Used:     readReplansUsed(st, ReplansUsedKey(traderID, tradeDate, session, baseline)),
		Cap:      cap,
		Baseline: baseline,
	}
}

// SpendReplan RECORDS one spend on the chain's current baseline and returns the
// new used count. Called when a death_replan / owner_reread row actually
// lands — never at a gate whose read may still be refused.
func SpendReplan(st *Store, traderID, tradeDate, session string) (int, error) {
	if st == nil {
		return 0, fmt.Errorf("store required")
	}
	replanSpendMu.Lock()
	defer replanSpendMu.Unlock()
	baseline := GetResetBaseline(st, traderID, tradeDate, session)
	key := ReplansUsedKey(traderID, tradeDate, session, baseline)
	used := readReplansUsed(st, key) + 1
	if err := st.SetSystemConfig(key, strconv.Itoa(used)); err != nil {
		return used - 1, err
	}
	return used, nil
}

// ReplanBudgetBootLine is the boot-block line naming the accounting mode.
func ReplanBudgetBootLine() string {
	return "replan budget: recorded-counter (class 35) — spends: " + TriggerDeathReplan + ", " + TriggerOwnerReread +
		" · free: <S>_scheduled_read, level_event, structure_mss (incl. fast-market), owner_reset, dormant/rearm + fail-closed markers · key dayplan_replans_used:<trader>:<date>:<session>:b<baseline>"
}

// ── OWNER RESET (2026-08-17) — a reset re-opens the budget for ONE chain ──────
//
// The owner reset marks the current chain ABANDONED (the rows stay — plans are
// append-only, history and death reasons preserved) and re-arms the session's
// re-plan budget from a new baseline: the version at which the reset happened.
// The recorded counter is keyed under the baseline, so the new chain starts
// at 0 spent.

// ResetBaselineKey is the system_config key holding the reset seam version for
// one (trader, trade_date, session). C7 (2026-08-25) — trader-scoped: the key
// used to be (trade_date, session), so two day-plan traders sharing a session
// shared the reset seam and one trader's reset re-armed the other's budget.
func ResetBaselineKey(traderID, tradeDate, session string) string {
	return "dayplan_reset:" + traderID + ":" + tradeDate + ":" + session
}

// ScenarioStatusKey is the system_config key holding a trader's live scenario
// statuses. P0-A (2026-08-18): the key used to be "scenario_status:<plan_id>"
// — with two day-plan traders sharing a plan_id, the last writer's statuses
// governed both cards. Trader-scoped so one trader's scenario facts can never
// reach another's card. Version-scoped so a newer plan cannot borrow them.
func ScenarioStatusKey(traderID, planID string, version int) string {
	return fmt.Sprintf("scenario_status:%s:%s:v%d", traderID, planID, version)
}

// ScenarioInvalidatedAtKey identifies the JSON first-observed invalidation
// record by trader, plan, version and scenario. Legacy unversioned timestamps
// remain untouched; readers never infer their version or anchor.
func ScenarioInvalidatedAtKey(traderID, planID string, version int, scenarioID string) string {
	return fmt.Sprintf(ScenarioDeathRecordPrefix+"%s:%s:v%d:%s", traderID, planID, version, scenarioID)
}

// ScenarioMetaKey (A1/A4, fail-register wave) — sibling of ScenarioStatusKey:
// {"basis":{"S1":"machine|heuristic"},"unevaluable":["S3"]} so the card can
// render heuristic verdicts distinctly and name unevaluable scenarios.
func ScenarioMetaKey(traderID, planID string, version int) string {
	return fmt.Sprintf("scenario_meta:%s:%s:v%d", traderID, planID, version)
}

// SetResetBaseline records the version the reset chain starts measuring from.
func SetResetBaseline(st *Store, traderID, tradeDate, session string, version int) error {
	if st == nil {
		return fmt.Errorf("store required")
	}
	if version < 1 {
		return fmt.Errorf("baseline version must be >= 1, got %d", version)
	}
	return st.SetSystemConfig(ResetBaselineKey(traderID, tradeDate, session), strconv.Itoa(version))
}

// GetResetBaseline returns the reset baseline for (trade_date, session); 1 when
// none was recorded (the original chain). A malformed value falls back to 1 —
// a bad marker can never inflate or destroy budget.
func GetResetBaseline(st *Store, traderID, tradeDate, session string) int {
	if st == nil {
		return 1
	}
	raw, _ := st.GetSystemConfig(ResetBaselineKey(traderID, tradeDate, session))
	if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && n >= 1 {
		return n
	}
	return 1
}

// MaxTradesFor resolves the per-session entry cap. ok=false means NO cap is
// configured for this session (the shipped behavior — the strategy-level daily
// guardrail still applies). A 0 cap is meaningful: no entries this session.
func (c *DayPlanConfig) MaxTradesFor(session string) (int, bool) {
	n, ok, _ := MaxTradesForWithSource(c, session)
	return n, ok
}

// MinGradeFor (grading audit §4.7, 2026-08-25) resolves the per-session
// min_grade floor: per-session override → "" (no filter). The ONE resolution
// seam so the kernel executor path (KEY LEVELS + PLAN STATUS) and the trader
// planner path can never disagree on the floor.
func (c *DayPlanConfig) MinGradeFor(session string) string {
	v, _ := MinGradeForWithSource(c, session)
	return v
}

// PlanModeFor resolves the plan-restriction mode for a session: per-session
// override → strategy-level → "advisory".
func (c *DayPlanConfig) PlanModeFor(session string) string {
	mode, _ := ResolvePlanMode(c, session)
	return mode
}

// DefaultDayPlanConfig returns the spec default block (plan OFF). It is NOT
// injected into GetDefaultStrategyConfig — creating a strategy leaves day_plan
// absent (byte-identical) until the owner opts in; the frontend/creation flow
// seeds this when the block is first turned on.
func DefaultDayPlanConfig() *DayPlanConfig {
	return &DayPlanConfig{
		PlanEnabled:        false,
		PlanMode:           "advisory",
		PlannerTimeframes:  []string{"D", "4h", "1h", "15m"},
		ProximityFilterATR: 1.5,
		MaxLevels:          8,
		HtfSeats:           intPtr(2),
		ReplanCap:          intPtr(2),
		SessionsEnabled:    []string{"NY"},
		ApprovalRequired:   false,
		// W-KNOB-PRUNE (2026-09-18): the folded knobs (scenario_cap,
		// acceptance_rule, evening_digest, realign_cap, wake_*,
		// wake_min_interval_min) are NOT seeded — their constants apply unless a
		// stored value exists; the deleted ones (last_entry_ct, eod_flat_ct,
		// seat_1h_zone, htf_score_multiplier) no longer exist.
		// R4 (2026-08-25) — scenario quality floor DEFAULT C (no restriction).
		MinScenarioQuality: "C",
	}
}

func wakeBoolPtr(v bool) *bool { return &v }

func intPtr(v int) *int { return &v }

// IntPtr returns a pointer to v — for presence-aware *int knobs (W1:
// consecutive_loss_halt, replan_cap), where nil = inherit and &0 = an explicit 0.
func IntPtr(v int) *int { return &v }

// DefaultWakeMinIntervalMin is the shipped wake spacing (minutes). W6-D
// (2026-08-25): raised 10 → 30 — wakes are unlimited (no budget), so the
// interval is the ONLY frequency guard; 30 min keeps unlimited wakes from
// churning the plan book.
const DefaultWakeMinIntervalMin = 30

// legacyWakeOn reads one legacy pointer switch: nil = ON (its shipped default).
func legacyWakeOn(p *bool) bool { return p == nil || *p }

// hasLegacyWakeFields reports whether any of the five legacy wake switches is
// present in the stored config (a pointer set, or the plain bool true).
func (c *DayPlanConfig) hasLegacyWakeFields() bool {
	return c != nil && (c.WakeOn15mZone != nil || c.WakeOnHTFZone != nil || c.WakeOnHTFOB ||
		c.WakeOnSeatedInvalidation != nil || c.WakeOnIFVG != nil)
}

// WakeOnLevelEventsEnabled is the ONE resolution seam for the level-event wake
// (W-KNOB-PRUNE, 2026-09-18). nil config or unset switch → ON (the shipped
// default of every legacy class that mattered). When the new switch is absent
// the five legacy switches decide: ON if ANY of them was ON — a nil legacy
// pointer reads ON — so the only stored shape that maps to OFF is all five
// explicitly false. Every consumer must go through this.
func (c *DayPlanConfig) WakeOnLevelEventsEnabled() bool {
	if c == nil {
		return true
	}
	if c.WakeOnLevelEvents != nil {
		return *c.WakeOnLevelEvents
	}
	return legacyWakeOn(c.WakeOn15mZone) || legacyWakeOn(c.WakeOnHTFZone) || c.WakeOnHTFOB ||
		legacyWakeOn(c.WakeOnSeatedInvalidation) || legacyWakeOn(c.WakeOnIFVG)
}

// WakeOnHTFOrderBlocks reports whether the HTF order-block wake class runs. It
// was OFF by default and the knob is removed; the class runs only when a
// legacy stored wake_on_htf_ob=true exists AND the level-event wake is ON —
// the shipped default (OFF) is byte-identical, the owner's stored true is
// honoured.
func (c *DayPlanConfig) WakeOnHTFOrderBlocks() bool {
	return c != nil && c.WakeOnHTFOB && c.WakeOnLevelEventsEnabled()
}

// WakeMinIntervalMinutes — FOLDED: the constant unless a stored value exists.
func (c *DayPlanConfig) WakeMinIntervalMinutes() int {
	if c == nil || c.WakeMinIntervalMin <= 0 {
		return DefaultWakeMinIntervalMin
	}
	return c.WakeMinIntervalMin
}

// DefaultScenarioCap / DefaultRealignCap are the folded constants
// (W-KNOB-PRUNE). ScenarioCapResolved / RealignCapResolved / EveningDigestOn
// are their ONE resolution seams: the constant unless a stored value exists.
const (
	DefaultScenarioCap = 3
	DefaultRealignCap  = 5
)

func (c *DayPlanConfig) ScenarioCapResolved() int {
	if c != nil && c.ScenarioCap >= 1 && c.ScenarioCap <= 5 {
		return c.ScenarioCap
	}
	return DefaultScenarioCap
}

func (c *DayPlanConfig) RealignCapResolved() int {
	if c != nil && c.RealignCap > 0 {
		return c.RealignCap
	}
	return DefaultRealignCap
}

// EveningDigestOn — constant OFF unless a stored true (owner verdict
// 2026-09-17: "evening_digest → constant off unless stored on"). Every live
// strategy stores true, so the live digest keeps writing.
func (c *DayPlanConfig) EveningDigestOn() bool {
	return c != nil && c.EveningDigest
}

// FoldedKnobLines (W-KNOB-PRUNE, 2026-09-18) — one line per folded knob whose
// STORED value differs from the constant it folded into, in the form
// `⚙ folded knob <name>=<value> honoured from stored config`. Logged once at
// trader load so a removed control can never silently keep a value the owner
// had set. An unset field or a stored default produces nothing.
func (c *DayPlanConfig) FoldedKnobLines() []string {
	if c == nil {
		return nil
	}
	var out []string
	add := func(name string, v interface{}) {
		out = append(out, fmt.Sprintf("⚙ folded knob %s=%v honoured from stored config", name, v))
	}
	if c.StructureMap != nil && *c.StructureMap {
		add("structure_map", true)
	}
	if c.ScenarioCap >= 1 && c.ScenarioCap <= 5 && c.ScenarioCap != DefaultScenarioCap {
		add("scenario_cap", c.ScenarioCap)
	}
	if c.RealignCap > 0 && c.RealignCap != DefaultRealignCap {
		add("realign_cap", c.RealignCap)
	}
	if c.EveningDigest {
		add("evening_digest", true)
	}
	if c.WakeMinIntervalMin > 0 && c.WakeMinIntervalMin != DefaultWakeMinIntervalMin {
		add("wake_min_interval_min", c.WakeMinIntervalMin)
	}
	if c.LevelsFreshByTF {
		add("levels_fresh_by_tf", true)
	}
	if r := strings.TrimSpace(c.AcceptanceRule); r != "" && r != DefaultAcceptanceRule {
		out = append(out, fmt.Sprintf("⚙ folded knob acceptance_rule=%s stored — NOT honoured, the one rule is %s (has been since 2026-08-30)", r, DefaultAcceptanceRule))
	}
	if c.WakeOnLevelEvents == nil && c.hasLegacyWakeFields() {
		add("wake_on_15m_zone/htf_zone/htf_ob/seated_invalidation/ifvg → wake_on_level_events", c.WakeOnLevelEventsEnabled())
		if c.WakeOnHTFOB {
			add("wake_on_htf_ob (order-block class)", true)
		}
	}
	return out
}

// FlipRereadEnabled is the ONE resolution seam for the W-FLIP-REREAD knob
// (absent/false = OFF = today's dormant behaviour).
func (c *DayPlanConfig) FlipRereadEnabled() bool {
	return c != nil && c.FlipReread
}

// PlannerFreshTapeEnabled is the ONE resolution seam for the PLANNER A6 knob:
// born-dead / flip-met retries carry the fresh completed tape between the read
// clock and the refusal. nil = ON (the shipped default); explicit false =
// today's blind-retry behaviour byte-identical.
func (c *DayPlanConfig) PlannerFreshTapeEnabled() bool {
	return c == nil || c.PlannerFreshTape == nil || *c.PlannerFreshTape
}

// DeathRereadRetryMinutes is the ONE resolution seam for the death-re-read
// self-backoff knob (FIX-PLANNER item 4): nil = today's value
// (wake_min_interval_min); an explicit value (clamped to ≥0) replaces ONLY the
// death re-read hold. wake_min_interval_min itself is never changed here.
func (c *DayPlanConfig) DeathRereadRetryMinutes() int {
	if c == nil || c.DeathRereadRetryMin == nil {
		return c.WakeMinIntervalMinutes()
	}
	if *c.DeathRereadRetryMin < 0 {
		return 0
	}
	return *c.DeathRereadRetryMin
}

// T1CurrencyAll is the sentinel meaning "every currency hard-blocks" — the
// pre-W-T1-CURRENCIES behaviour. "*" is accepted on input and canonicalised
// to this.
const T1CurrencyAll = "ALL"

// DefaultT1Currencies is the shipped default: only USD red events hard-block.
func DefaultT1Currencies() []string { return []string{"USD"} }

// T1CurrenciesFor is the ONE resolution seam for the W-T1-CURRENCIES knob
// (the value's canonicaliser — trim, upper-case, dedupe, drop blanks — so every
// consumer sees one spelling). nil config or an empty/blank list → the shipped
// default ["USD"]. Any entry "ALL" or "*" → ["ALL"] (every currency hard-blocks,
// today's behaviour). Never returns nil or an empty list.
func (c *DayPlanConfig) T1CurrenciesFor() []string {
	if c == nil {
		return DefaultT1Currencies()
	}
	out := make([]string, 0, len(c.T1Currencies))
	seen := map[string]bool{}
	for _, raw := range c.T1Currencies {
		ccy := strings.ToUpper(strings.TrimSpace(raw))
		if ccy == "" || seen[ccy] {
			continue
		}
		if ccy == "*" || ccy == T1CurrencyAll {
			return []string{T1CurrencyAll}
		}
		seen[ccy] = true
		out = append(out, ccy)
	}
	if len(out) == 0 {
		return DefaultT1Currencies()
	}
	return out
}

// T1CurrenciesSaved reports whether the strategy carries an explicit,
// non-blank t1_currencies list (for the boot line's "(saved)" vs "(default)"
// — READ, never inferred from the resolved value, since a saved ["USD"] must
// still print "(saved)").
func (c *DayPlanConfig) T1CurrenciesSaved() bool {
	if c == nil {
		return false
	}
	for _, raw := range c.T1Currencies {
		if strings.TrimSpace(raw) != "" {
			return true
		}
	}
	return false
}

// LevelsFreshByTFEnabled is the ONE resolution seam for the S2 by-TF freshness
// knob: nil config or unset → OFF (today's 1m-touch grading).
func (c *DayPlanConfig) LevelsFreshByTFEnabled() bool {
	return c != nil && c.LevelsFreshByTF
}

// WriteTimeFeasibilityEnabled is the ONE resolution seam for the write-time
// feasibility knob: nil config or unset → ON (owner ruling "fix all",
// 2026-09-18 08:3x CT). Explicit false = today's WARN-only behaviour.
func (c *DayPlanConfig) WriteTimeFeasibilityEnabled() bool {
	return c == nil || c.WriteTimeFeasibility == nil || *c.WriteTimeFeasibility
}

// GeometryRefIDsEnabled is the ONE resolution seam for the
// day_plan.geometry_reference_levels knob (W-GEOMETRY-REFUSAL): nil config or
// nil pointer → ON (the owner's default); explicit false → OFF (today's
// behaviour byte-identical). Identical to DS-102's seam so the merge keeps
// both.
// GeometryRefIDsEnabled is the ONE resolution seam for the
// day_plan.geometry_reference_levels knob (W-GEOMETRY-REFUSAL): nil config or
// nil pointer → ON (the owner's default); explicit false → OFF (today's
// behaviour byte-identical). The executor, the prompt map and the boot line all
// resolve through this seam so they can never disagree.
func (c *DayPlanConfig) GeometryRefIDsEnabled() bool {
	return c == nil || c.GeometryReferenceLevels == nil || *c.GeometryReferenceLevels
}

// DeathRereadEnabled is the ONE resolution seam for the day_plan.death_reread
// knob (W-DEATH-REREAD, 2026-09-18): nil config or nil pointer → ON (the
// owner's default per the 12:3x CT "fix all" ruling); explicit false → OFF
// (today's behaviour byte-identical — a death only parks the plan). The death
// branch, the dormant retry loop, the wick guard and the boot line all resolve
// through this seam so they can never disagree.
func (c *DayPlanConfig) DeathRereadEnabled() bool {
	return c == nil || c.DeathReread == nil || *c.DeathReread
}

// MinScenarioQualityFor (R4, 2026-08-25) resolves the scenario quality floor:
// per-session override → strategy-level → "C" (no restriction). The ONE
// resolution seam so the kernel gate and the Studio card can never disagree.
func (c *DayPlanConfig) MinScenarioQualityFor(session string) string {
	v, _ := MinScenarioQualityForWithSource(c, session)
	return v
}

// MinSideLevelsFor REMOVED by owner ruling 2026-08-31 — the per-side count
// concept is deleted from the system (knob, resolver, WARN, labels). Old
// stored config JSON with "min_side_levels" loads harmlessly: encoding/json
// ignores unknown fields on unmarshal.

// GridStrategyConfig grid trading specific configuration
type GridStrategyConfig struct {
	// Trading pair (e.g., "MNQ")
	Symbol string `json:"symbol"`
	// Number of grid levels (5-50)
	GridCount int `json:"grid_count"`
	// Total investment in the value currency
	TotalInvestment float64 `json:"total_investment"`
	// Leverage (1-20)
	Leverage int `json:"leverage"`
	// Upper price boundary (0 = auto-calculate from ATR)
	UpperPrice float64 `json:"upper_price"`
	// Lower price boundary (0 = auto-calculate from ATR)
	LowerPrice float64 `json:"lower_price"`
	// Use ATR to auto-calculate bounds
	UseATRBounds bool `json:"use_atr_bounds"`
	// ATR multiplier for bound calculation (default 2.0)
	ATRMultiplier float64 `json:"atr_multiplier"`
	// Position distribution: "uniform" | "gaussian" | "pyramid"
	Distribution string `json:"distribution"`
	// Maximum drawdown percentage before emergency exit
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
	// Stop loss percentage per position
	StopLossPct float64 `json:"stop_loss_pct"`
	// Daily loss limit percentage
	DailyLossLimitPct float64 `json:"daily_loss_limit_pct"`
	// Use maker-only orders for lower fees
	UseMakerOnly bool `json:"use_maker_only"`
	// Enable automatic grid direction adjustment based on box breakouts
	EnableDirectionAdjust bool `json:"enable_direction_adjust"`
	// Direction bias ratio for long_bias/short_bias modes (default 0.7 = 70%/30%)
	DirectionBiasRatio float64 `json:"direction_bias_ratio"`
}

// PromptSectionsConfig editable sections of System Prompt
type PromptSectionsConfig struct {
	// role definition (title + description)
	RoleDefinition string `json:"role_definition,omitempty"`
	// trading frequency awareness
	TradingFrequency string `json:"trading_frequency,omitempty"`
	// entry standards
	EntryStandards string `json:"entry_standards,omitempty"`
	// decision process
	DecisionProcess string `json:"decision_process,omitempty"`
}

// CoinSourceConfig coin source configuration
type CoinSourceConfig struct {
	// source type shown in the product editor: "static" (the legacy multi-pool types collapse to static on load)"
	SourceType string `json:"source_type"`
	// static coin list (used when source_type = "static")
	StaticCoins []string `json:"static_coins,omitempty"`
	// excluded coins list (filtered out from all sources)
	ExcludedCoins []string `json:"excluded_coins,omitempty"`
}

// IndicatorConfig indicator configuration
type IndicatorConfig struct {
	// K-line configuration
	Klines KlineConfig `json:"klines"`
	// raw kline data (OHLCV) - always enabled, required for AI analysis
	EnableRawKlines bool `json:"enable_raw_klines"`
	// technical indicator switches
	EnableEMA         bool `json:"enable_ema"`
	EnableMACD        bool `json:"enable_macd"`
	EnableRSI         bool `json:"enable_rsi"`
	EnableATR         bool `json:"enable_atr"`
	EnableBOLL        bool `json:"enable_boll"` // Bollinger Bands
	EnableVolume      bool `json:"enable_volume"`
	EnableOI          bool `json:"enable_oi"`           // open interest
	EnableFundingRate bool `json:"enable_funding_rate"` // funding rate
	EnableSVP         bool `json:"enable_svp"`          // session volume profile → futures AI prompt line (POC/VAH/VAL); default OFF
	// EMA period configuration
	EMAPeriods []int `json:"ema_periods,omitempty"` // default [20, 50]
	// RSI period configuration
	RSIPeriods []int `json:"rsi_periods,omitempty"` // default [7, 14]
	// ATR period configuration
	ATRPeriods []int `json:"atr_periods,omitempty"` // default [14]
	// BOLL period configuration (period, standard deviation multiplier is fixed at 2)
	BOLLPeriods []int `json:"boll_periods,omitempty"` // default [20] - can select multiple timeframes
	// external data sources
	ExternalDataSources []ExternalDataSource `json:"external_data_sources,omitempty"`
}

// KlineConfig K-line configuration
type KlineConfig struct {
	// primary timeframe: "1m", "3m", "5m", "15m", "1h", "4h"
	PrimaryTimeframe string `json:"primary_timeframe"`
	// primary timeframe K-line count
	PrimaryCount int `json:"primary_count"`
	// longer timeframe
	LongerTimeframe string `json:"longer_timeframe,omitempty"`
	// longer timeframe K-line count
	LongerCount int `json:"longer_count,omitempty"`
	// whether to enable multi-timeframe analysis
	EnableMultiTimeframe bool `json:"enable_multi_timeframe"`
	// selected timeframe list (new: supports multi-timeframe selection)
	SelectedTimeframes []string `json:"selected_timeframes,omitempty"`
}

// ExternalDataSource external data source configuration
type ExternalDataSource struct {
	Name        string            `json:"name"`   // data source name
	Type        string            `json:"type"`   // type: "api" | "webhook"
	URL         string            `json:"url"`    // API URL
	Method      string            `json:"method"` // HTTP method
	Headers     map[string]string `json:"headers,omitempty"`
	DataPath    string            `json:"data_path,omitempty"`    // JSON data path
	RefreshSecs int               `json:"refresh_secs,omitempty"` // refresh interval (seconds)
}

// MentorTuning holds the mentor-method numbers the owner may tune from Studio.
// Zero / nil = "use the ruled default" (never "off", except LevelMaxVisits
// where an explicit 0 turns the per-day visit cap off).
type MentorTuning struct {
	// TriggerSchool: 1 = enter at the level without waiting for the 5m
	// confirm (his own school, D3.4 p3 @09:22); 2 = wait for the 5m trigger.
	TriggerSchool int `json:"trigger_school,omitempty"`
	// Box ping-pong gate (D4.2 p2 @05:17): the box must be wider than this
	// many points AND the largest 1m candle of the last Lookback bars must
	// stay at or below CandleMax.
	PingPongMinGapPts      float64 `json:"ping_pong_min_gap_pts,omitempty"`
	PingPongCandleMaxPts   float64 `json:"ping_pong_candle_max_pts,omitempty"`
	PingPongCandleLookback int     `json:"ping_pong_candle_lookback,omitempty"`
	// LevelMaxVisits: a level trades its first N visits of the day (D1.3 p1
	// @11:02). nil → 3; explicit 0 → no cap.
	LevelMaxVisits *int `json:"level_max_visits,omitempty"`
	// OrbGateEnabled: the §7 step-0 opening-range gate (nil → ON).
	OrbGateEnabled *bool `json:"orb_gate_enabled,omitempty"`
	// ISBReverseEMA9Enabled: the reverse ISB at EMA 9 (D5.4; nil → ON, R-C).
	ISBReverseEMA9Enabled *bool `json:"isb_reverse_ema9_enabled,omitempty"`
	// HTFGateNewsOnly (D4.4-11): the 4h/1h direction gate only inside the
	// 07:20–07:35 CT news window (nil → OFF = the all-day gate; U-6 open).
	HTFGateNewsOnly *bool `json:"htf_gate_news_only,omitempty"`
	// Exec2mAfter30m (X5-10): after the first 30 minutes of RTH (09:00 CT)
	// the ISB entry is read on the 2m chart (nil → OFF; the course trades the
	// 1m throughout).
	Exec2mAfter30m *bool `json:"exec_2m_after_30m,omitempty"`
	// Spent-day gate (METHOD §7 table, D5.1 p1 @14:21–15:57): a run of at
	// least SpentPts before the open = spent; the target cap then applies,
	// and any setup whose stop is over the cap is skipped (R9).
	DayGateSpentPts     float64 `json:"day_gate_spent_pts,omitempty"`
	DayGateTargetCapPts float64 `json:"day_gate_target_cap_pts,omitempty"`
	// SwingMaxStopPts: SWING4H skips at a stop >= this (R-D, default 100). The
	// trader rule gate and the kernel swing gate read this ONE value.
	SwingMaxStopPts float64 `json:"swing_max_stop_pts,omitempty"`
}

// RiskControlConfig risk control configuration
type RiskControlConfig struct {
	// Max number of coins held simultaneously (CODE ENFORCED)
	MaxPositions int `json:"max_positions"`

	// BTC/ETH exchange leverage for opening positions (AI guided)
	BTCETHMaxLeverage int `json:"btc_eth_max_leverage"`
	// Altcoin exchange leverage for opening positions (AI guided)
	AltcoinMaxLeverage int `json:"altcoin_max_leverage"`

	// BTC/ETH single position max value = equity × this ratio (CODE ENFORCED, default: 5)
	BTCETHMaxPositionValueRatio float64 `json:"btc_eth_max_position_value_ratio"`
	// Altcoin single position max value = equity × this ratio (CODE ENFORCED, default: 1)
	AltcoinMaxPositionValueRatio float64 `json:"altcoin_max_position_value_ratio"`

	// Max margin utilization (e.g. 0.9 = 90%) (CODE ENFORCED)
	MaxMarginUsage float64 `json:"max_margin_usage"`
	// Min position size in the value currency (CODE ENFORCED)
	MinPositionSize float64 `json:"min_position_size"`

	// Min take_profit / stop_loss ratio (AI guided)
	MinRiskRewardRatio float64 `json:"min_risk_reward_ratio"`
	// Min AI confidence to open position (AI guided)
	MinConfidence int `json:"min_confidence"`

	// === Strategy Studio Phase 1 — prop-firm daily guardrails (per-strategy;
	// env = fallback for the value; the per-guardrail toggle governs enforcement). ===

	// Master switch: nil/true = guardrails active; false = ALL guardrails bypassed.
	GuardrailsEnabled *bool `json:"guardrails_enabled,omitempty"`

	// Daily realized-LOSS limit (USD, positive): loss ≥ this halts new entries for
	// the CME session-day. Enabled defaults ON (nil) to preserve the existing live
	// env daily-loss gate; the value falls back to RISK_MAX_DAILY_LOSS_USD when 0.
	DailyLossLimitUSD float64 `json:"daily_loss_limit_usd,omitempty"`
	DailyLossEnabled  *bool   `json:"daily_loss_enabled,omitempty"`

	// Daily realized-PROFIT target (USD, positive): profit ≥ this stops new entries
	// for the session-day. New guardrail → defaults OFF (nil).
	DailyProfitTargetUSD float64 `json:"daily_profit_target_usd,omitempty"`
	DailyProfitEnabled   *bool   `json:"daily_profit_enabled,omitempty"`

	// Max ENTRIES per CME session-day. New guardrail → defaults OFF (nil).
	MaxDailyTrades        int   `json:"max_daily_trades,omitempty"`
	MaxDailyTradesEnabled *bool `json:"max_daily_trades_enabled,omitempty"`

	// D1 — CONSECUTIVE-LOSS halt: after this many consecutive LOSING closed trades
	// in the CME session-day, block NEW entries until the next session (open-pos
	// management via SL/TP is unaffected). Resets on a winning/break-even close or
	// a new session. NOT gated by the guardrails master switch — it is a
	// per-strategy circuit breaker.
	//
	// W1 (settings truth, 2026-09-23) — PRESENCE-AWARE. nil/absent = INHERIT
	// (env BREAKER_HALT_N, else the shipped default 8 — the breaker is ON);
	// an explicit 0 = OFF; N = N. The old int said "0 = OFF" here while the
	// runtime read 0 as "unset → 8" and no writer could store a 0 at all
	// (omitempty) — the UI's OFF was a switch wired to nothing. Resolved ONLY
	// by ResolveBreakerHalt (store/resolve_source.go).
	ConsecutiveLossHalt *int `json:"consecutive_loss_halt,omitempty"`

	// B7 — RE-ENTRY COOLDOWN: after a STOP-LOSS exit, block a SAME-DIRECTION
	// re-entry on that symbol for this many minutes OR until price moves ≥ 1×ATR15
	// away from the stop — whichever comes first. Per-trader; opposite direction and
	// other symbols are never blocked. FE default 20; 0 = OFF (existing strategies
	// stay off until set). Not gated by the guardrails master switch.
	ReentryCooldownMinutes int `json:"reentry_cooldown_minutes,omitempty"`

	// Chunk 3 — max CONTRACTS per futures order (clamp). Unset → the 2-contract
	// default (the prior hidden const maxFuturesContracts). Toggle default ON.
	MaxContractsPerOrder int `json:"max_contracts_per_order,omitempty"`

	// MENTOR P3 — mentor mode, per strategy. OFF by default: while off the bot
	// is byte-identical (L4). A mentor-mode trader takes AI entries off, sizes
	// from the mentor table, and its exits follow the mentor §6 modes; the AI
	// 0B size clamp and EXIT_MECHS_SUSPENDED do not apply to it.
	MentorMode bool `json:"mentor_mode,omitempty"`
	// Mentor size-table knobs (CTO defaults when unset — the owner can change
	// them): base setup at a location 5; confluence 10; confluence+4h/1h agree
	// + room≥2× target≥30pts 20 (hard cap); stop 20–25 pts or spent day 3;
	// SWING4H 3; never above MentorMaxContracts (default 20).
	MentorBaseContracts       int `json:"mentor_base_contracts,omitempty"`
	MentorConfluenceContracts int `json:"mentor_confluence_contracts,omitempty"`
	MentorBigContracts        int `json:"mentor_big_contracts,omitempty"`
	MentorReducedContracts    int `json:"mentor_reduced_contracts,omitempty"`
	MentorSwing4HContracts    int `json:"mentor_swing4h_contracts,omitempty"`
	MentorSpentDayContracts   int `json:"mentor_spent_day_contracts,omitempty"`
	MentorMaxContracts        int `json:"mentor_max_contracts,omitempty"`
	// EXIT-SPEC-v3 (B): the candle-trail timeframe. "1m" default (course frame
	// D2.4 p1 @08:35 on the 1-MINUTE chart); 30s/45s allowed (@09:07); "off" =
	// the video-8 legacy "never trail on the 1m" (SUPERSEDED, kept as a knob).
	MentorTrailTF string `json:"mentor_trail_tf,omitempty"`
	// STOP RULES (owner ruling 00:1x CT "exactly like he said") — class rules,
	// NOT his personal routine. All default ON:
	// (a) done for the day after a winning close with the day net positive
	//     (nil → ON; explicit false disables);
	// (b) the trading window: MentorWindowStart (HH:MM CT) + MentorWindowMinutes
	//     (30/60/90/120; unset → 60). The SWING setup is exempt.
	// (c)/(d) never-widen / never-add are guards, not knobs.
	MentorDoneAfterWin  *bool  `json:"mentor_done_after_win,omitempty"`
	MentorWindowStart   string `json:"mentor_window_start,omitempty"`
	MentorWindowMinutes int    `json:"mentor_window_minutes,omitempty"`
	// Knob routing (CTO 1791033257041): the evaluator's G1/L1/E4/location
	// knobs ride the strategy config like the other mentor knobs, defaults as
	// ruled:
	//   MentorLegBudgetEnabled — G1 leg budget (nil → ON);
	//   MentorLegResetOn — G1 parity ("close" default | "touch"; bad → close);
	//   MentorLvlRevisitMinPts — L1 per-visit departure (default 0);
	//   MentorEmaMaxCross30m — E4 "xien len xien xuong" (default ON = 2,
	//     the most conservative; 0 = OFF; mentor question open — no count in
	//     the course);
	//   MentorLocationTriggerFilter — the 5m trigger filter at locations
	//     (nil → ON, L3: keep it ON in the base).
	//   MentorLossDeparturePts — optional fixed-points fallback for B22's
	//     structural loss-area departure (default 0 = OFF).
	MentorLegBudgetEnabled      *bool   `json:"mentor_leg_budget_enabled,omitempty"`
	MentorLegResetOn            string  `json:"mentor_leg_reset_on,omitempty"`
	MentorLvlRevisitMinPts      float64 `json:"mentor_lvl_revisit_min_pts,omitempty"`
	MentorEmaMaxCross30m        int     `json:"mentor_ema_max_cross_30m,omitempty"`
	MentorLocationTriggerFilter *bool   `json:"mentor_loc_trigger_filter,omitempty"`
	MentorLossDeparturePts      float64 `json:"mentor_loss_departure_pts,omitempty"`
	// MentorTuning — the owner-tunable mentor method numbers (K3, OWNER RULING
	// 2026-10-04 "do all as mentor"). EVERY field unset (nil/0) resolves to the
	// ruled default in kernel/mentor DefaultConfig, so an absent block runs
	// exactly the shipped method; an out-of-range value fails closed to that
	// default (trader/mentor_tuning.go).
	MentorTuning *MentorTuning `json:"mentor_tuning,omitempty"`
	// Deprecated (6.4 ruling B): the enabled toggle never had a reader — the
	// contracts clamp is always-on venue safety. Field kept so old stored
	// configs still parse; nothing reads it, the UI no longer writes it.
	MaxContractsEnabled *bool `json:"max_contracts_enabled,omitempty"`

	// Chunk 3 — futures NOTIONAL ceiling multiplier: max position notional =
	// equity × this. Unset → 20 (the prior hidden const futuresMaxNotionalLeverage),
	// now VISIBLE + EDITABLE. Toggle default ON (safety backstop).
	MaxNotionalLeverage float64 `json:"max_notional_leverage,omitempty"`
	// Deprecated (6.4 ruling B): same as MaxContractsEnabled — parse-only.
	NotionalCapEnabled *bool `json:"notional_cap_enabled,omitempty"`

	// Chunk 4 — time/news BLACKOUT window (daily, HH:MM in America/Chicago). When
	// enabled, the bot makes no new decisions inside [start,end] CT (NT8-side SL/TP
	// still protect open positions). New guardrail → toggle defaults OFF.
	BlackoutEnabled *bool  `json:"blackout_enabled,omitempty"`
	BlackoutStartCT string `json:"blackout_start_ct,omitempty"`
	BlackoutEndCT   string `json:"blackout_end_ct,omitempty"`

	// Chunk 5 — CONSISTENCY rule: no single CME session-day's realized profit may
	// exceed this % of all-time total realized profit. Only triggers once there is
	// prior-day profit (a fresh/single-day account never self-locks). New guardrail
	// → toggle defaults OFF.
	ConsistencyMaxDayPct float64 `json:"consistency_max_day_pct,omitempty"`
	ConsistencyEnabled   *bool   `json:"consistency_enabled,omitempty"`

	// Hold-lock ("Hold discipline"): when ON, once a position is OPEN the bot
	// SUPPRESSES an AI-initiated close — the trade rides to the stop/target the
	// AI set (a real OCO bracket resting at the exchange), instead of the AI
	// bailing on noise minutes in. Only AI decisions are gated; Emergency Flat
	// and the drawdown monitor bypass it. New feature → default OFF (nil/false).
	HoldDisciplineEnabled *bool `json:"hold_discipline,omitempty"`

	// Auto-breakeven (NT8 futures): once an open position is BreakevenTriggerPoints
	// in profit, move its stop to entry (breakeven) so a winner can't turn into a
	// loss. The move is sent to the AddOn (move_stop frame), which modifies the
	// resting bracket in place. New feature → default OFF; trigger defaults to 50.
	BreakevenEnabled       *bool   `json:"breakeven_enabled,omitempty"`
	BreakevenTriggerPoints float64 `json:"breakeven_trigger_points,omitempty"`

	// Trailing profit (final-bundle Phase 3B, 2026-08-19; NT8 futures only).
	// Mechanical, deterministic, zero AI: once ARMED (per trailing_arm), each 60s
	// monitor beat computes trail = best_price_since_entry ∓ mult×ATR(period,5m)
	// and ratchets the resting stop via the proven move_stop path — never
	// backward, never below entry after breakeven fired. DEFAULT OFF.
	TrailingEnabled   *bool   `json:"trailing_enabled,omitempty"`
	TrailingATRMult   float64 `json:"trailing_atr_mult,omitempty"`   // default 2.0
	TrailingATRPeriod int     `json:"trailing_atr_period,omitempty"` // default 14 (5m ATR)
	// TrailingArm: "after_breakeven" (default) | "after_trigger_points" | "immediate".
	TrailingArm       string  `json:"trailing_arm,omitempty"`
	TrailingArmPoints float64 `json:"trailing_arm_points,omitempty"` // used iff after_trigger_points
}

// NewStrategyStore creates a new StrategyStore
func NewStrategyStore(db *gorm.DB) *StrategyStore {
	return &StrategyStore{db: db}
}

func (s *StrategyStore) initTables() error {
	// AutoMigrate will add missing columns without dropping existing data
	return s.db.AutoMigrate(&Strategy{})
}

func (s *StrategyStore) initDefaultData() error {
	// No longer pre-populate strategies - create on demand when user configures
	return nil
}

// defaultCoinSource returns the seed coin source for a fresh strategy.
// Futures-only build (C2): seeds the single NT8 instrument as a static coin.
func defaultCoinSource() CoinSourceConfig {
	return CoinSourceConfig{
		SourceType:  "static",
		StaticCoins: []string{"MNQ"},
	}
}

// GetDefaultStrategyConfig returns the default strategy configuration for the given language
func GetDefaultStrategyConfig(lang string) StrategyConfig {
	// Normalize language to "zh" or "en"
	normalizedLang := "en"
	if lang == "zh" {
		normalizedLang = "zh"
	}

	config := StrategyConfig{
		Language:   normalizedLang,
		CoinSource: defaultCoinSource(),
		Indicators: IndicatorConfig{
			Klines: KlineConfig{
				PrimaryTimeframe:     "5m",
				PrimaryCount:         20,
				LongerTimeframe:      "4h",
				LongerCount:          10,
				EnableMultiTimeframe: true,
				SelectedTimeframes:   []string{"5m", "15m", "1h"},
			},
			EnableRawKlines:   true, // Required - raw OHLCV data for AI analysis
			EnableEMA:         false,
			EnableMACD:        false,
			EnableRSI:         false,
			EnableATR:         false,
			EnableBOLL:        false,
			EnableVolume:      true,
			EnableOI:          true,
			EnableFundingRate: true,
			EMAPeriods:        []int{20, 50},
			RSIPeriods:        []int{7, 14},
			ATRPeriods:        []int{14},
			BOLLPeriods:       []int{20},
		},
		RiskControl: RiskControlConfig{
			MaxPositions:                 3,   // Max 3 coins simultaneously (CODE ENFORCED)
			BTCETHMaxLeverage:            5,   // BTC/ETH exchange leverage (AI guided)
			AltcoinMaxLeverage:           5,   // Altcoin exchange leverage (AI guided)
			BTCETHMaxPositionValueRatio:  5.0, // BTC/ETH: max position = 5x equity (CODE ENFORCED)
			AltcoinMaxPositionValueRatio: 1.0, // Altcoin: max position = 1x equity (CODE ENFORCED)
			MaxMarginUsage:               0.9, // Max 90% margin usage (CODE ENFORCED)
			MinPositionSize:              12,  // Min 12 USD per position (CODE ENFORCED)
			MinRiskRewardRatio:           3.0, // Min 3:1 profit/loss ratio (AI guided)
			MinConfidence:                75,  // Min 75% confidence (AI guided)
		},
	}

	// Role + Decision are intentionally NOT seeded: an empty box lets each market's
	// prompt builder supply the correct default — the instrument-aware CME role on
	// futures (engine_prompt_futures.go), the crypto role on crypto
	// (engine_prompt.go) — instead of one shared (market-wrong) seed. Frequency +
	// Entry stay seeded (market-neutral). Saved user boxes are untouched.
	if lang == "zh" {
		config.PromptSections = PromptSectionsConfig{
			TradingFrequency: `# ⏱️ 交易频率意识

- 优秀交易员：每天2-4笔 ≈ 每小时0.1-0.2笔
- 每小时超过2笔 = 过度交易
- 单笔持仓时间 ≥ 30-60分钟
如果你发现自己每个周期都在交易 → 标准太低；如果持仓不到30分钟就平仓 → 太冲动。`,
			EntryStandards: `# 🎯 入场标准（严格）

只在多个信号共振时入场。自由使用任何有效的分析方法，避免单一指标、信号矛盾、横盘震荡、或平仓后立即重新开仓等低质量行为。`,
		}
	} else {
		config.PromptSections = PromptSectionsConfig{
			TradingFrequency: `# ⏱️ Trading Frequency Awareness

- Excellent trader: 2-4 trades per day ≈ 0.1-0.2 trades per hour
- >2 trades per hour = overtrading
- Single position holding time ≥ 30-60 minutes
If you find yourself trading every cycle → standards are too low; if closing positions in <30 minutes → too impulsive.`,
			EntryStandards: `# 🎯 Entry Standards (Strict)

Only enter positions when multiple signals resonate. Freely use any effective analysis methods, avoid low-quality behaviors such as single indicators, contradictory signals, sideways oscillation, or immediately restarting after closing positions.`,
		}
	}

	// CME futures (NT8): tune the indicator DEFAULTS for a new futures strategy —
	// enable the technical indicators the futures prompt leans on (the deleted
	// crypto-only feeds need no disabling any more). Defaults-only (new-strategy
	// template); existing saved strategies are never mutated. See helper.
	applyFuturesIndicatorDefaults(&config.Indicators)

	return config
}

// applyFuturesIndicatorDefaults tunes the indicator defaults for a NEW
// CME-futures strategy:
//
//  1. Keep Open Interest OFF — it is the legacy crypto-perp feed and the
//     futures path never reads it (W-NO-BINANCE A).
//  2. Enable the computed technical indicators the futures prompt actually leans
//     on — ATR (stop sizing), EMA (trend), RSI (momentum) — which otherwise
//     default OFF, leaving the futures AI with raw bars + volume only. Periods
//     are already seeded ([20,50] / [7,14] / [14]).
//
// Indicators are prompt-data and NEVER gate a trade (the gate reads only Risk
// Control), so this is a defaults-only, prompt-input change. It runs only inside
// GetDefaultStrategyConfig (the new-strategy template) — existing saved
// strategies are never touched. MACD/BOLL are deliberately left off.
func applyFuturesIndicatorDefaults(ind *IndicatorConfig) {
	// Open Interest is the legacy crypto-perp feed too — the futures path never
	// reads it (W-NO-BINANCE A: OI is absent and renders n/a on MNQ; the NT8
	// bridge carries OHLCV only). Off by default so a new futures strategy
	// doesn't list an OI section that can only say n/a.
	ind.EnableOI = false
	// (2) computed technical indicators ON for futures.
	ind.EnableATR = true
	ind.EnableEMA = true
	ind.EnableRSI = true
}

// Create create a strategy
func (s *StrategyStore) Create(strategy *Strategy) error {
	return s.db.Create(strategy).Error
}

// Update update a strategy
func (s *StrategyStore) Update(strategy *Strategy) error {
	return updateStrategyRow(s.db, strategy, time.Now().UTC()).Error
}

// updateStrategyRow is Update's one statement, on db (the store or a
// transaction) — UpdateWithExplicitZeros runs it inside its transaction.
func updateStrategyRow(db *gorm.DB, strategy *Strategy, updatedAt time.Time) *gorm.DB {
	return db.Model(&Strategy{}).
		Where("id = ? AND user_id = ?", strategy.ID, strategy.UserID).
		Updates(map[string]interface{}{
			"name":           strategy.Name,
			"description":    strategy.Description,
			"config":         strategy.Config,
			"is_public":      strategy.IsPublic,
			"config_visible": strategy.ConfigVisible,
			"updated_at":     updatedAt,
		})
}

// Delete delete a strategy
func (s *StrategyStore) Delete(userID, id string) error {
	// do not allow deleting system default strategy
	var st Strategy
	if err := s.db.Where("id = ?", id).First(&st).Error; err == nil {
		if st.IsDefault {
			return fmt.Errorf("cannot delete system default strategy")
		}
		if st.IsActive {
			return fmt.Errorf("cannot delete active strategy")
		}
	}

	// Check if any trader references this strategy
	var count int64
	if err := s.db.Model(&Trader{}).
		Where("user_id = ? AND strategy_id = ?", userID, id).
		Count(&count).Error; err == nil && count > 0 {
		return fmt.Errorf("cannot delete strategy in use by %d trader(s) - reassign those traders first", count)
	}

	return s.db.Where("id = ? AND user_id = ?", id, userID).Delete(&Strategy{}).Error
}

// List get user's strategy list
func (s *StrategyStore) List(userID string) ([]*Strategy, error) {
	var strategies []*Strategy
	err := s.db.Where("user_id = ? OR is_default = ?", userID, true).
		Order("is_default DESC, created_at DESC").
		Find(&strategies).Error
	if err != nil {
		return nil, err
	}
	return strategies, nil
}

// ListPublic get all public strategies for the strategy market
func (s *StrategyStore) ListPublic() ([]*Strategy, error) {
	var strategies []*Strategy
	err := s.db.Where("is_public = ?", true).
		Order("created_at DESC").
		Find(&strategies).Error
	if err != nil {
		return nil, err
	}
	return strategies, nil
}

// Get get a single strategy
func (s *StrategyStore) Get(userID, id string) (*Strategy, error) {
	return getStrategy(s.db, userID, id)
}

// getStrategy is Get on db (the store or a transaction).
func getStrategy(db *gorm.DB, userID, id string) (*Strategy, error) {
	var st Strategy
	err := db.Where("id = ? AND (user_id = ? OR is_default = ?)", id, userID, true).
		First(&st).Error
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// GetActive get user's currently active strategy
func (s *StrategyStore) GetActive(userID string) (*Strategy, error) {
	var st Strategy
	err := s.db.Where("user_id = ? AND is_active = ?", userID, true).First(&st).Error
	if err == gorm.ErrRecordNotFound {
		// no active strategy, return system default strategy
		return s.GetDefault()
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// GetDefault get system default strategy
func (s *StrategyStore) GetDefault() (*Strategy, error) {
	var st Strategy
	err := s.db.Where("is_default = ?", true).First(&st).Error
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// SetActive set active strategy (will first deactivate other strategies)
func (s *StrategyStore) SetActive(userID, strategyID string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// first deactivate all strategies for the user
		if err := tx.Model(&Strategy{}).Where("user_id = ?", userID).
			Update("is_active", false).Error; err != nil {
			return err
		}

		// activate specified strategy
		return tx.Model(&Strategy{}).
			Where("id = ? AND (user_id = ? OR is_default = ?)", strategyID, userID, true).
			Update("is_active", true).Error
	})
}

// Duplicate duplicate a strategy (used to create custom strategy based on default strategy)
//
// W1 (settings truth): the copy holds the same bytes, so it carries the
// source's record of which explicit zeros a W1 save confirmed — a copy of a
// confirmed OFF breaker is still the owner's OFF. The source read, the new row
// and the copied record are ONE transaction (CTO ruling msg 1790176346377): a
// copy never lands without its record, nor pairs one source's bytes with
// another moment's record.
func (s *StrategyStore) Duplicate(userID, sourceID, newID, newName string) error {
	// P2-4: BEGIN IMMEDIATE, not a deferred read→write upgrade.
	return immediateOrPlainTxAny(s.db, func(tx *gorm.DB) error {
		// get source strategy
		source, err := getStrategy(tx, userID, sourceID)
		if err != nil {
			return fmt.Errorf("failed to get source strategy: %w", err)
		}

		// create new strategy
		newStrategy := &Strategy{
			ID:          newID,
			UserID:      userID,
			Name:        newName,
			Description: "Created based on [" + source.Name + "]",
			IsActive:    false,
			IsDefault:   false,
			Config:      source.Config,
		}
		if err := tx.Create(newStrategy).Error; err != nil {
			return err
		}
		return copyExplicitZeroMarker(tx, sourceID, newID)
	})
}

// ParseConfig parse strategy configuration JSON
func (s *Strategy) ParseConfig() (*StrategyConfig, error) {
	var config StrategyConfig
	if err := json.Unmarshal([]byte(s.Config), &config); err != nil {
		return nil, fmt.Errorf("failed to parse strategy configuration: %w", err)
	}
	config.applyMissingDefaults()
	return &config, nil
}

// applyMissingDefaults backfills GetDefaultStrategyConfig values for config
// blocks that were persisted empty, so a strategy saved without a coin_source
// or without a klines block never silently falls back to a blank coin source
// (kernel/engine.go default case "unknown coin source type") or to the crypto
// "3m" primary timeframe (kernel/engine_analysis.go), which NT8 never subscribes
// (it auto-subscribes 5m/15m/1h). Only genuinely-empty blocks are filled;
// explicitly-set values are preserved. This is the durable twin of the
// per-strategy DB fixes for coin_source and klines.
func (c *StrategyConfig) applyMissingDefaults() {
	def := GetDefaultStrategyConfig(c.Language)

	// Coin source: a blank source_type with no static coins and no source flags
	// means the block was never set. Default the TYPE to "static" — matching the
	// engine-level guard in GetCandidateCoins (commit abda753d) so the two layers
	// agree — rather than a legacy pool default, which would be wrong for a
	// futures trader. An empty static list then degrades to the upstream
	// "no candidates" path instead of the unknown-type hard error.
	if c.CoinSource.SourceType == "" && len(c.CoinSource.StaticCoins) == 0 {
		c.CoinSource.SourceType = "static"
	}
	switch c.CoinSource.SourceType {
	case "ai500", "oi_top", "oi_low", "hyper_all", "hyper_main", "mixed":
		c.CoinSource.SourceType = "static"
	}

	// Klines: no selected timeframes and no primary timeframe means the block was
	// never set -> adopt the default timeframe set (5m/15m/1h, primary 5m), which
	// matches the NT8 auto-subscribed set and avoids the engine "3m" fallback.
	if len(c.Indicators.Klines.SelectedTimeframes) == 0 && c.Indicators.Klines.PrimaryTimeframe == "" {
		c.Indicators.Klines.SelectedTimeframes = def.Indicators.Klines.SelectedTimeframes
		c.Indicators.Klines.PrimaryTimeframe = def.Indicators.Klines.PrimaryTimeframe
		c.Indicators.Klines.LongerTimeframe = def.Indicators.Klines.LongerTimeframe
		c.Indicators.Klines.EnableMultiTimeframe = true
		if c.Indicators.Klines.PrimaryCount == 0 {
			c.Indicators.Klines.PrimaryCount = def.Indicators.Klines.PrimaryCount
		}
		if c.Indicators.Klines.LongerCount == 0 {
			c.Indicators.Klines.LongerCount = def.Indicators.Klines.LongerCount
		}
		// raw OHLCV is required for AI analysis
		c.Indicators.EnableRawKlines = true
	}
}

// SetConfig set strategy configuration
func (s *Strategy) SetConfig(config *StrategyConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize strategy configuration: %w", err)
	}
	s.Config = string(data)
	return nil
}

// ============================================================================
// Token Estimation
// ============================================================================

// TokenEstimate holds the result of token estimation
type TokenEstimate struct {
	Total       int            `json:"total"`
	Breakdown   TokenBreakdown `json:"breakdown"`
	ModelLimits []ModelLimit   `json:"model_limits"`
	Suggestions []string       `json:"suggestions"`
}

// TokenBreakdown shows estimated tokens per component
type TokenBreakdown struct {
	SystemPrompt  int `json:"system_prompt"`
	MarketData    int `json:"market_data"`
	FixedOverhead int `json:"fixed_overhead"`
}

// ModelLimit shows token usage against a specific model's context limit
type ModelLimit struct {
	Name         string `json:"name"`
	ContextLimit int    `json:"context_limit"`
	UsagePct     int    `json:"usage_pct"`
	Level        string `json:"level"` // "ok" | "warning" | "danger"
}

// Context window sizes (tokens) for each model family
const (
	contextLimitDeepSeek = 131_072   // 128K
	contextLimitOpenAI   = 128_000   // 128K
	contextLimitClaude   = 200_000   // 200K
	contextLimitQwen     = 131_072   // 128K
	contextLimitGemini   = 1_000_000 // 1M
	contextLimitGrok     = 131_072   // 128K
	contextLimitKimi     = 131_072   // 128K
	contextLimitMinimax  = 1_000_000 // 1M
)

// ModelContextLimits maps provider names to their context window sizes (in tokens)
var ModelContextLimits = map[string]int{
	"deepseek": contextLimitDeepSeek,
	"openai":   contextLimitOpenAI,
	"claude":   contextLimitClaude,
	"qwen":     contextLimitQwen,
	"gemini":   contextLimitGemini,
	"grok":     contextLimitGrok,
	"kimi":     contextLimitKimi,
	"minimax":  contextLimitMinimax,
}

// GetContextLimit returns the context limit for a given provider
func GetContextLimit(provider string) int {
	if limit, ok := ModelContextLimits[provider]; ok {
		return limit
	}
	return contextLimitDeepSeek // safe default
}

// GetContextLimitForClient returns context limit for a provider+model pair.
func GetContextLimitForClient(provider, model string) int {
	return GetContextLimit(provider)
}

// EstimateTokens estimates the total token count for a strategy configuration.
// This is a pure computation based on config fields — no network calls.
func (c *StrategyConfig) EstimateTokens() TokenEstimate {
	breakdown := TokenBreakdown{}

	// --- System Prompt ---
	// Base system prompt: schema + role + rules + output format
	baseChars := 4000 // English default
	if c.Language == "zh" {
		baseChars = 3000
	}
	// Add prompt sections
	baseChars += len(c.PromptSections.RoleDefinition)
	baseChars += len(c.PromptSections.TradingFrequency)
	baseChars += len(c.PromptSections.EntryStandards)
	baseChars += len(c.PromptSections.DecisionProcess)
	baseChars += len(c.CustomPrompt)

	if c.Language == "zh" {
		breakdown.SystemPrompt = baseChars / 2 // CJK: ~2 chars per token
	} else {
		breakdown.SystemPrompt = baseChars / 4 // English: ~4 chars per token
	}

	// --- Fixed Overhead ---
	// Time, price, account info, section headers
	breakdown.FixedOverhead = 800 / 4 // ~200 tokens

	// --- Market Data ---
	numCoins := c.getEffectiveCoinCount()
	numTimeframes := c.getEffectiveTimeframeCount()
	klineCount := c.Indicators.Klines.PrimaryCount
	if klineCount <= 0 {
		klineCount = 20
	}

	// Per coin per timeframe: kline OHLCV rows
	charsPerCoinTF := klineCount * 80 // each OHLCV line ~80 chars

	// Add enabled indicator overhead per timeframe
	indicatorCharsPerLine := 0
	if c.Indicators.EnableEMA {
		indicatorCharsPerLine += 20 // EMA values appended
	}
	if c.Indicators.EnableMACD {
		indicatorCharsPerLine += 30
	}
	if c.Indicators.EnableRSI {
		indicatorCharsPerLine += 15
	}
	if c.Indicators.EnableATR {
		indicatorCharsPerLine += 15
	}
	if c.Indicators.EnableBOLL {
		indicatorCharsPerLine += 25
	}
	if c.Indicators.EnableVolume {
		indicatorCharsPerLine += 10
	}
	charsPerCoinTF += klineCount * indicatorCharsPerLine

	totalMarketChars := numCoins * numTimeframes * charsPerCoinTF

	// OI + Funding per coin
	if c.Indicators.EnableOI || c.Indicators.EnableFundingRate {
		totalMarketChars += numCoins * 100
	}

	breakdown.MarketData = totalMarketChars / 4 // numeric data: ~4 chars per token

	// --- Total with 15% safety margin ---
	subtotal := breakdown.SystemPrompt + breakdown.MarketData + breakdown.FixedOverhead
	total := subtotal * 115 / 100

	// --- Model limits ---
	modelLimits := make([]ModelLimit, 0, len(ModelContextLimits))
	for name, limit := range ModelContextLimits {
		pct := total * 100 / limit
		level := "ok"
		if pct >= 100 {
			level = "danger"
		} else if pct >= 80 {
			level = "warning"
		}
		modelLimits = append(modelLimits, ModelLimit{
			Name:         name,
			ContextLimit: limit,
			UsagePct:     pct,
			Level:        level,
		})
	}

	// Sort by usage_pct desc, then name asc for deterministic order
	sort.Slice(modelLimits, func(i, j int) bool {
		if modelLimits[i].UsagePct != modelLimits[j].UsagePct {
			return modelLimits[i].UsagePct > modelLimits[j].UsagePct
		}
		return modelLimits[i].Name < modelLimits[j].Name
	})

	// --- Suggestions ---
	var suggestions []string
	// Find the strictest model (smallest context)
	minLimit := 0
	for _, limit := range ModelContextLimits {
		if minLimit == 0 || limit < minLimit {
			minLimit = limit
		}
	}
	if minLimit > 0 && total > minLimit {
		if numTimeframes > 1 {
			savedPerTF := (numCoins * klineCount * (80 + indicatorCharsPerLine)) / 4 * 115 / 100
			suggestions = append(suggestions, fmt.Sprintf("Reduce 1 timeframe to save ~%d tokens", savedPerTF))
		}
		if numCoins > 1 {
			savedPerCoin := (numTimeframes * klineCount * (80 + indicatorCharsPerLine)) / 4 * 115 / 100
			suggestions = append(suggestions, fmt.Sprintf("Reduce 1 coin to save ~%d tokens", savedPerCoin))
		}
		if klineCount > 15 {
			suggestions = append(suggestions, "Reduce K-line count to 15 to save tokens")
		}
	}

	return TokenEstimate{
		Total:       total,
		Breakdown:   breakdown,
		ModelLimits: modelLimits,
		Suggestions: suggestions,
	}
}

// getEffectiveCoinCount returns the estimated number of coins that will be analyzed
func (c *StrategyConfig) getEffectiveCoinCount() int {
	count := 0
	switch c.CoinSource.SourceType {
	case "static":
		count = len(c.CoinSource.StaticCoins)
	default:
		count = len(c.CoinSource.StaticCoins)
	}
	if count <= 0 {
		count = 3
	}
	return count
}

// getEffectiveTimeframeCount returns the number of timeframes that will be used
func (c *StrategyConfig) getEffectiveTimeframeCount() int {
	if len(c.Indicators.Klines.SelectedTimeframes) > 0 {
		return len(c.Indicators.Klines.SelectedTimeframes)
	}
	count := 1
	if c.Indicators.Klines.LongerTimeframe != "" {
		count++
	}
	return count
}

// PlannerContractOn (WAVE PLANNER A3): nil = ON (shipped default); explicit
// false renders the pre-A3 prompt bytes.
func (c *DayPlanConfig) PlannerContractOn() bool {
	if c == nil || c.PlannerContract == nil {
		return true
	}
	return *c.PlannerContract
}
