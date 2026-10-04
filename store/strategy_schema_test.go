package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestStrategyConfigMarshalSeparatesGridAndAIConfig(t *testing.T) {
	cfg := GetDefaultStrategyConfig("zh")
	cfg.StrategyType = "grid_trading"
	cfg.GridConfig = &GridStrategyConfig{
		Symbol:          "BTCUSDT",
		GridCount:       20,
		TotalInvestment: 200,
		Leverage:        2,
		UseATRBounds:    true,
		ATRMultiplier:   2,
		Distribution:    "uniform",
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal grid config: %v", err)
	}

	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatalf("unmarshal grid config map: %v", err)
	}
	if asMap["strategy_type"] != "grid_trading" {
		t.Fatalf("expected grid strategy_type, got %v", asMap["strategy_type"])
	}
	if _, ok := asMap["grid_config"]; !ok {
		t.Fatalf("expected grid_config in grid strategy JSON: %s", string(raw))
	}
	for _, key := range []string{"ai_config", "coin_source", "indicators", "risk_control", "prompt_sections", "custom_prompt"} {
		if _, ok := asMap[key]; ok {
			t.Fatalf("did not expect %s in grid strategy JSON: %s", key, string(raw))
		}
	}
}

func TestStrategyConfigUnmarshalLegacyFlatAIConfig(t *testing.T) {
	raw := []byte(`{
		"strategy_type":"ai_trading",
		"coin_source":{"source_type":"static","static_coins":["ETHUSDT"]},
		"indicators":{"klines":{"primary_timeframe":"15m"}},
		"risk_control":{"max_positions":2,"min_confidence":80},
		"prompt_sections":{"entry_standards":"trend only"},
		"custom_prompt":"prefer ETH"
	}`)

	var cfg StrategyConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal legacy flat config: %v", err)
	}
	if cfg.CoinSource.SourceType != "static" || len(cfg.CoinSource.StaticCoins) != 1 || cfg.CoinSource.StaticCoins[0] != "ETHUSDT" {
		t.Fatalf("legacy coin source was not normalized: %+v", cfg.CoinSource)
	}
	if cfg.Indicators.Klines.PrimaryTimeframe != "15m" {
		t.Fatalf("legacy indicators were not normalized: %+v", cfg.Indicators.Klines)
	}

	normalized, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal normalized config: %v", err)
	}
	var asMap map[string]any
	if err := json.Unmarshal(normalized, &asMap); err != nil {
		t.Fatalf("unmarshal normalized map: %v", err)
	}
	if _, ok := asMap["ai_config"]; !ok {
		t.Fatalf("expected ai_config after normalizing legacy config: %s", string(normalized))
	}
	if _, ok := asMap["coin_source"]; ok {
		t.Fatalf("did not expect legacy coin_source at top level: %s", string(normalized))
	}
}

func TestStrategyConfigNormalizeProductSchemaForLLMLabels(t *testing.T) {
	cfg := GetDefaultStrategyConfig("zh")
	patch := map[string]any{
		"strategy_type": "AI 策略",
		"ai_config": map[string]any{
			"coin_source": map[string]any{
				"source_type": "AI500",
			},
			"indicators": map[string]any{
				"klines": map[string]any{
					"primary_timeframe":   "1分钟",
					"selected_timeframes": []any{`["1m"`, `"5m"`, `"15m"]`},
				},
			},
		},
	}

	merged, err := MergeStrategyConfig(cfg, patch)
	if err != nil {
		t.Fatalf("merge strategy config: %v", err)
	}
	merged.ClampLimits()

	if merged.StrategyType != "ai_trading" {
		t.Fatalf("strategy_type = %q, want ai_trading", merged.StrategyType)
	}
	if merged.CoinSource.SourceType != "static" {
		t.Fatalf("source_type = %q, want static (legacy ai500 label degrades after the VlOS provider was deleted)", merged.CoinSource.SourceType)
	}
	if merged.Indicators.Klines.PrimaryTimeframe != "1m" {
		t.Fatalf("primary_timeframe = %q, want 1m", merged.Indicators.Klines.PrimaryTimeframe)
	}
	want := []string{"1m", "5m", "15m"}
	if len(merged.Indicators.Klines.SelectedTimeframes) != len(want) {
		t.Fatalf("selected_timeframes = %+v, want %+v", merged.Indicators.Klines.SelectedTimeframes, want)
	}
	for i := range want {
		if merged.Indicators.Klines.SelectedTimeframes[i] != want[i] {
			t.Fatalf("selected_timeframes = %+v, want %+v", merged.Indicators.Klines.SelectedTimeframes, want)
		}
	}
}

// TestStoredLegacyVlOSConfigLoadsWithoutError (D2-DEAD must-hold 1): the
// owner's saved strategies in data.db carry the keys the VlOS deletion
// removed (vlos_api_key, enable_quant*, ranking flags, coin_source
// use_ai500/use_oi_top/use_oi_low). The production loader is
// (*Strategy).ParseConfig — plain json.Unmarshal (no DisallowUnknownFields
// anywhere on the strategy-config path) — so those keys must be
// readable-and-ignored, the load must succeed, and the legacy source_type must
// degrade to static (never a DB write, never a migration).
func TestStoredLegacyVlOSConfigLoadsWithoutError(t *testing.T) {
	legacyJSON := `{
		"strategy_type":"ai_trading",
		"coin_source":{"source_type":"ai500","use_ai500":true,"ai500_limit":5,"use_oi_top":false,"oi_top_limit":3,"use_oi_low":false,"oi_low_limit":3},
		"indicators":{
			"klines":{"primary_timeframe":"5m","selected_timeframes":["5m","15m","1h"],"enable_multi_timeframe":true,"enable_raw_klines":true},
			"vlos_api_key":"cm_568c67eae410d912c54c",
			"enable_quant_data":true,
			"enable_quant_oi":true,
			"enable_quant_netflow":true,
			"enable_oi_ranking":true,
			"oi_ranking_duration":"1h",
			"oi_ranking_limit":10,
			"enable_netflow_ranking":true,
			"netflow_ranking_duration":"1h",
			"netflow_ranking_limit":10,
			"enable_price_ranking":true,
			"price_ranking_duration":"1h,4h,24h",
			"price_ranking_limit":10
		},
		"risk_control":{"max_positions":3,"btc_eth_max_leverage":3,"altcoin_max_leverage":2,"min_confidence":70,"min_risk_reward_ratio":1.5},
		"prompt_sections":{"trading_frequency":"每天最多 2~4 笔","entry_standards":"趋势确认后入场"}
	}`

	dbPath := filepath.Join(t.TempDir(), "legacy-vlos-load.db")
	st, err := New(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	strategy := &Strategy{
		ID:     "strategy-legacy-vlos",
		UserID: "default",
		Name:   "Legacy VlOS row",
		Config: legacyJSON,
	}
	if err := st.Strategy().Create(strategy); err != nil {
		t.Fatalf("create legacy strategy row: %v", err)
	}

	got, err := st.Strategy().Get("default", strategy.ID)
	if err != nil {
		t.Fatalf("load legacy strategy row: %v", err)
	}
	cfg, err := got.ParseConfig()
	if err != nil {
		t.Fatalf("ParseConfig must load a stored config carrying the deleted keys: %v", err)
	}
	if cfg.CoinSource.SourceType != "static" {
		t.Fatalf("legacy ai500 source_type must degrade to static on load, got %q", cfg.CoinSource.SourceType)
	}
	if cfg.Indicators.Klines.PrimaryTimeframe != "5m" {
		t.Fatalf("legacy klines must survive the load, got %+v", cfg.Indicators.Klines)
	}
	if cfg.RiskControl.MinConfidence != 70 {
		t.Fatalf("legacy risk_control must survive the load, got %+v", cfg.RiskControl)
	}
}
