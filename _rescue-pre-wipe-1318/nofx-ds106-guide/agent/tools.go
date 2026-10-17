package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"vl/kernel"
	"vl/logger"
	"vl/market"
	"vl/mcp"
	"vl/safe"
	"vl/security"
	"vl/store"
	"vl/trader"
	ntTrader "vl/trader/ninjatrader"
)

// cachedTools holds the static tool definitions (built once, reused per message).
var cachedTools = buildAgentTools()

var (
	marketDataHTTPClient        = http.DefaultClient
	traderInitialBalanceFetcher = defaultTraderInitialBalanceFetcher
)

// agentTools returns the tools available to the LLM for autonomous action.
func agentTools() []mcp.Tool { return cachedTools }

func plannerToolsForText(text string) []mcp.Tool {
	domain := plannerToolDomainForText(text)
	compactStrategy := !looksLikeStrategyMutationIntent(text)
	names := plannerToolNamesForDomain(domain)
	return toolsByName(names, compactStrategy)
}

func plannerToolDomainForText(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return "general"
	}
	if containsAny(lower, []string{"诊断", "排查", "为什么", "为啥", "失败", "报错", "异常", "停止", "没下单", "failed", "error", "diagnose", "debug", "logs", "stopped", "not trading"}) {
		return "diagnosis"
	}
	if hasExplicitManagementDomainCue(text, "exchange") || containsAny(lower, []string{"交易所", "exchange", "apikey", "secret", "passphrase", "api凭证"}) {
		return "exchange"
	}
	if hasExplicitManagementDomainCue(text, "model") || containsAny(lower, []string{"ai model", "模型", "provider", "api key", "custom_model", "custom api"}) {
		return "model"
	}
	if hasExplicitManagementDomainCue(text, "strategy") || containsAny(lower, []string{"策略", "strategy", "选币", "止盈", "止损", "杠杆", "风控", "risk control"}) {
		return "strategy"
	}
	if hasExplicitManagementDomainCue(text, "trader") || containsAny(lower, []string{"交易员", "trader", "启动", "停止交易员", "扫描间隔", "竞技场"}) {
		return "trader"
	}
	if containsAny(lower, []string{"余额", "资产", "仓位", "持仓", "订单", "成交", "交易历史", "balance", "position", "positions", "trade history", "account"}) {
		return "account"
	}
	if containsAny(lower, []string{"行情", "价格", "k线", "kline", "market", "price", "股票", "stock"}) {
		return "market"
	}
	return "general"
}

func plannerToolNamesForDomain(domain string) []string {
	switch domain {
	case "market":
		return []string{"get_market_price", "search_stock"}
	case "account":
		return []string{"get_balance", "get_positions", "get_trade_history", "get_exchange_configs"}
	case "trader":
		return []string{"get_model_configs", "get_exchange_configs", "get_strategies", "manage_trader"}
	case "model":
		return []string{"get_model_configs", "manage_model_config"}
	case "exchange":
		return []string{"get_exchange_configs", "manage_exchange_config"}
	case "strategy":
		return []string{"get_strategies", "manage_strategy"}
	case "diagnosis":
		return []string{"get_decisions", "get_backend_logs", "get_model_configs", "get_exchange_configs", "get_strategies", "manage_trader"}
	default:
		return []string{
			"get_preferences", "manage_preferences",
			"get_decisions", "get_backend_logs",
			"get_exchange_configs", "manage_exchange_config",
			"get_model_configs", "manage_model_config",
			"get_strategies", "manage_strategy",
			"manage_trader",
			"get_balance", "get_positions", "get_trade_history",
			"get_market_price", "search_stock",
		}
	}
}

func toolsByName(names []string, compactStrategy bool) []mcp.Tool {
	if len(names) == 0 {
		return nil
	}
	byName := make(map[string]mcp.Tool, len(cachedTools))
	for _, tool := range cachedTools {
		byName[tool.Function.Name] = tool
	}
	out := make([]mcp.Tool, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		tool, ok := byName[name]
		if !ok {
			continue
		}
		if compactStrategy && name == "manage_strategy" {
			tool = compactManageStrategyTool(tool)
		}
		out = append(out, tool)
	}
	return out
}

func compactManageStrategyTool(tool mcp.Tool) mcp.Tool {
	tool.Function.Description = "List, query, delete, activate, duplicate, create, or update strategy templates. Planning schema is compact; use action plus strategy_id/name/description/lang/is_public/config_visible, and include config only when the user explicitly provides strategy config fields."
	tool.Function.Parameters = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":         map[string]any{"type": "string", "enum": []string{"list", "create", "update", "delete", "activate", "duplicate", "get_default_config"}},
			"strategy_id":    map[string]any{"type": "string"},
			"name":           map[string]any{"type": "string"},
			"description":    map[string]any{"type": "string"},
			"lang":           map[string]any{"type": "string", "enum": []string{"zh", "en"}},
			"is_public":      map[string]any{"type": "boolean"},
			"config_visible": map[string]any{"type": "boolean"},
			"config":         map[string]any{"type": "object", "description": "Strategy config patch. Use precise field paths/objects from the user request; omit when listing/querying/deleting/activating/duplicating."},
		},
		"required": []string{"action"},
	}
	return tool
}

func looksLikeStrategyMutationIntent(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return hasExplicitManagementDomainCue(text, "strategy") &&
		containsAny(lower, []string{"创建", "新建", "创一个", "创个", "建一个", "修改", "更新", "编辑", "调整", "配置", "create", "new", "update", "edit", "configure"})
}

func normalizedEntityName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func sameEntityName(a, b string) bool {
	return normalizedEntityName(a) != "" && normalizedEntityName(a) == normalizedEntityName(b)
}

func (a *Agent) ensureUniqueModelName(storeUserID, name, excludeID string) error {
	models, err := a.store.AIModel().List(storeUserID)
	if err != nil {
		return err
	}
	for _, model := range models {
		if model == nil || strings.TrimSpace(model.ID) == strings.TrimSpace(excludeID) {
			continue
		}
		if sameEntityName(model.Name, name) {
			return fmt.Errorf("model name %q already exists", strings.TrimSpace(name))
		}
	}
	return nil
}

// findModelByProvider resolves the deterministic winner among rows sharing a provider
// (enabled + most-recent, via store.PickProviderModel). With multiple entries per
// provider this returns the primary — never a silent first-by-id — and logs the choice.
func (a *Agent) findModelByProvider(storeUserID, provider string) (*store.AIModel, error) {
	models, err := a.store.AIModel().List(storeUserID)
	if err != nil {
		return nil, err
	}
	chosen, n := store.PickProviderModel(models, provider)
	if chosen != nil && n > 1 {
		logger.Warnf("⚠️ findModelByProvider %q → %d entries; using enabled+most-recent id=%s", provider, n, chosen.ID)
	}
	return chosen, nil
}

func (a *Agent) ensureUniqueExchangeAccountName(storeUserID, accountName, excludeID string) error {
	exchanges, err := a.store.Exchange().List(storeUserID)
	if err != nil {
		return err
	}
	for _, exchange := range exchanges {
		if exchange == nil || strings.TrimSpace(exchange.ID) == strings.TrimSpace(excludeID) {
			continue
		}
		if sameEntityName(exchange.AccountName, accountName) {
			return fmt.Errorf("exchange account name %q already exists", strings.TrimSpace(accountName))
		}
	}
	return nil
}

func (a *Agent) ensureUniqueStrategyName(storeUserID, name, excludeID string) error {
	strategies, err := a.store.Strategy().List(storeUserID)
	if err != nil {
		return err
	}
	for _, strategy := range strategies {
		if strategy == nil || strings.TrimSpace(strategy.ID) == strings.TrimSpace(excludeID) {
			continue
		}
		if sameEntityName(strategy.Name, name) {
			return fmt.Errorf("strategy name %q already exists", strings.TrimSpace(name))
		}
	}
	return nil
}

func (a *Agent) ensureUniqueTraderName(storeUserID, name, excludeID string) error {
	traders, err := a.store.Trader().List(storeUserID)
	if err != nil {
		return err
	}
	for _, trader := range traders {
		if trader == nil || strings.TrimSpace(trader.ID) == strings.TrimSpace(excludeID) {
			continue
		}
		if sameEntityName(trader.Name, name) {
			return fmt.Errorf("trader name %q already exists", strings.TrimSpace(name))
		}
	}
	return nil
}

func stringArraySchema(description string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": description,
		"items":       map[string]any{"type": "string"},
	}
}

func intArraySchema(description string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": description,
		"items":       map[string]any{"type": "number"},
	}
}

func strategyConfigSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "Full or partial strategy config. Only include the fields you want to create or update.",
		"properties": map[string]any{
			"strategy_type": map[string]any{"type": "string", "enum": []string{"ai_trading", "grid_trading"}, "description": "Top-level discriminator. ai_trading must use ai_config only. grid_trading must use grid_config only."},
			"language":      map[string]any{"type": "string", "enum": []string{"zh", "en"}},
			"ai_config": map[string]any{
				"type":        "object",
				"description": "AI trading only. Do not include this for grid_trading.",
				"properties": map[string]any{
					"coin_source": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"source_type":    map[string]any{"type": "string", "enum": []string{"static"}, "description": "Manual page coin source: static."},
							"static_coins":   stringArraySchema("Static symbols. Manual page allows at most 10. xyz: assets such as xyz:TSLA, xyz:GOLD, xyz:XYZ100 are also supported."),
							"excluded_coins": stringArraySchema("Symbols to exclude from all sources."),
						},
					},
					"indicators": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"klines": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"primary_timeframe":      map[string]any{"type": "string", "enum": []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w"}},
									"primary_count":          map[string]any{"type": "number", "minimum": 10, "maximum": 30, "description": "Manual page range 10-30."},
									"longer_timeframe":       map[string]any{"type": "string"},
									"longer_count":           map[string]any{"type": "number"},
									"enable_multi_timeframe": map[string]any{"type": "boolean"},
									"selected_timeframes":    stringArraySchema("Selected analysis timeframes. Allowed values: 1m,3m,5m,15m,30m,1h,2h,4h,6h,8h,12h,1d,3d,1w. Manual page allows at most 4."),
								},
							},
							"enable_raw_klines":   map[string]any{"type": "boolean"},
							"enable_ema":          map[string]any{"type": "boolean"},
							"enable_macd":         map[string]any{"type": "boolean"},
							"enable_rsi":          map[string]any{"type": "boolean"},
							"enable_atr":          map[string]any{"type": "boolean"},
							"enable_boll":         map[string]any{"type": "boolean"},
							"enable_volume":       map[string]any{"type": "boolean"},
							"enable_oi":           map[string]any{"type": "boolean"},
							"enable_funding_rate": map[string]any{"type": "boolean"},
							"ema_periods":         intArraySchema("EMA periods such as [20,50]."),
							"rsi_periods":         intArraySchema("RSI periods such as [7,14]."),
							"atr_periods":         intArraySchema("ATR periods such as [14]."),
							"boll_periods":        intArraySchema("BOLL periods such as [20]."),
						},
					},
					"custom_prompt": map[string]any{"type": "string"},
					"risk_control": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"min_risk_reward_ratio": map[string]any{"type": "number", "minimum": 1, "maximum": 10, "description": "Manual page range 1-10, step 0.5."},
							"min_confidence":        map[string]any{"type": "number", "minimum": 50, "maximum": 100, "description": "Manual page range 50-100."},
						},
					},
					"prompt_sections": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"role_definition":   map[string]any{"type": "string"},
							"trading_frequency": map[string]any{"type": "string"},
							"entry_standards":   map[string]any{"type": "string"},
							"decision_process":  map[string]any{"type": "string"},
						},
					},
				},
			},
			"grid_config": map[string]any{
				"description": "Grid trading only. Do not include this for ai_trading.",
				"type":        "object",
				"properties": map[string]any{
					"symbol":                  map[string]any{"type": "string", "description": "Grid trading symbol."},
					"grid_count":              map[string]any{"type": "number", "minimum": 5, "maximum": 50, "description": "Manual page range 5-50."},
					"total_investment":        map[string]any{"type": "number", "minimum": 100, "description": "User's actual capital/margin budget for the grid strategy, not leveraged notional exposure. Minimum 100 USD."},
					"leverage":                map[string]any{"type": "number", "minimum": 1, "maximum": 5, "description": "Manual page range 1-5."},
					"upper_price":             map[string]any{"type": "number"},
					"lower_price":             map[string]any{"type": "number"},
					"use_atr_bounds":          map[string]any{"type": "boolean"},
					"atr_multiplier":          map[string]any{"type": "number", "minimum": 1, "maximum": 5, "description": "Manual page range 1-5, step 0.5."},
					"distribution":            map[string]any{"type": "string", "enum": []string{"uniform", "gaussian", "pyramid"}},
					"max_drawdown_pct":        map[string]any{"type": "number", "minimum": 5, "maximum": 50, "description": "Manual page range 5-50."},
					"stop_loss_pct":           map[string]any{"type": "number", "minimum": 1, "maximum": 20, "description": "Manual page range 1-20."},
					"daily_loss_limit_pct":    map[string]any{"type": "number", "minimum": 1, "maximum": 30, "description": "Manual page range 1-30."},
					"use_maker_only":          map[string]any{"type": "boolean"},
					"enable_direction_adjust": map[string]any{"type": "boolean"},
					"direction_bias_ratio":    map[string]any{"type": "number", "minimum": 0.55, "maximum": 0.9, "description": "Manual page range 0.55-0.90 (shown as 55%-90%)."},
				},
			},
			"publish_config": map[string]any{
				"type":        "object",
				"description": "Shared publish settings for both AI and grid strategies.",
				"properties": map[string]any{
					"is_public":      map[string]any{"type": "boolean"},
					"config_visible": map[string]any{"type": "boolean"},
				},
			},
		},
	}
}

func modelConfigFieldsSchema() map[string]any {
	return map[string]any{
		"model_id": map[string]any{
			"type":        "string",
			"description": "Existing model id for update/delete, or the desired id for create.",
		},
		"provider": map[string]any{
			"type":        "string",
			"description": "Provider slug such as openai, claude, gemini, deepseek, qwen, kimi, grok, minimax.",
		},
		"name": map[string]any{
			"type":        "string",
			"description": "Display name for the model binding.",
		},
		"enabled": map[string]any{
			"type":        "boolean",
			"description": "Whether this model binding is enabled.",
		},
		"api_key": map[string]any{
			"type":        "string",
			"description": "Provider credential. For standard providers this is an API key. Sensitive and never returned in full.",
		},
		"custom_api_url": map[string]any{
			"type":        "string",
			"description": "Custom API base URL or endpoint override. Optional for standard providers.",
		},
		"custom_model_name": map[string]any{
			"type":        "string",
			"description": "Actual upstream model name to send to the provider. Optional when the provider has a default model.",
		},
	}
}

func exchangeConfigFieldsSchema() map[string]any {
	return map[string]any{
		"exchange_id": map[string]any{
			"type":        "string",
			"description": "Existing exchange account id. Required for update and delete.",
		},
		"exchange_type": map[string]any{
			"type":        "string",
			"description": "Exchange type.",
		},
		"account_name": map[string]any{
			"type":        "string",
			"description": "User-visible account name like Main, Testnet, or Mom Account.",
		},
		"enabled": map[string]any{
			"type":        "boolean",
			"description": "Whether this exchange binding should be enabled.",
		},
		"api_key":    map[string]any{"type": "string", "description": "API key for CEX-style exchanges."},
		"secret_key": map[string]any{"type": "string", "description": "Secret key for CEX-style exchanges."},
		"passphrase": map[string]any{"type": "string", "description": "Optional passphrase."},
		"testnet":    map[string]any{"type": "boolean", "description": "Whether to use the exchange testnet/sandbox."},
	}
}

func traderConfigFieldsSchema() map[string]any {
	return map[string]any{
		"trader_id": map[string]any{
			"type":        "string",
			"description": "Required for update, delete, start, and stop.",
		},
		"name":                  map[string]any{"type": "string", "description": "Trader display name. Required for create."},
		"ai_model_id":           map[string]any{"type": "string", "description": "Bound AI model id."},
		"exchange_id":           map[string]any{"type": "string", "description": "Bound exchange id."},
		"strategy_id":           map[string]any{"type": "string", "description": "Bound strategy id."},
		"scan_interval_minutes": map[string]any{"type": "number", "description": "Trading scan interval in minutes."},
		"is_cross_margin":       map[string]any{"type": "boolean", "description": "Whether cross margin is enabled."},
		"show_in_competition":   map[string]any{"type": "boolean", "description": "Whether to show this trader in competition views."},
	}
}

func buildAgentTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "get_preferences",
				Description: "Get all persistent user preferences that the agent should remember long-term.",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "manage_preferences",
				Description: "Add, update, or delete a persistent user preference. Use this when the user asks to remember something long-term, change an existing long-term preference, or remove one.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action": map[string]any{
							"type":        "string",
							"enum":        []string{"add", "update", "delete"},
							"description": "What to do with the persistent preference.",
						},
						"text": map[string]any{
							"type":        "string",
							"description": "The new preference text. Required for add and update.",
						},
						"match": map[string]any{
							"type":        "string",
							"description": "How to find the existing preference to update or delete. Can be an id or distinctive text like '每天8点'.",
						},
					},
					"required": []string{"action"},
				},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "get_backend_logs",
				Description: "Get recent backend log lines for a trader diagnosis. Prefer this when the user asks why a specific trader failed, stopped, or behaved unexpectedly. Returns recent matching log lines for the authenticated user's trader. You can identify the trader by name or id — name is preferred when the user provides it.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"trader_id":   map[string]any{"type": "string", "description": "Trader id to diagnose."},
						"trader_name": map[string]any{"type": "string", "description": "Trader name to diagnose. Used to look up the trader when id is not known."},
						"limit":       map[string]any{"type": "number", "description": "Maximum number of recent log lines to return. Default 30."},
						"errors_only": map[string]any{"type": "boolean", "description": "When true, only return error-like log lines. Default true."},
					},
				},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "get_decisions",
				Description: "Get recent AI decision records for a trader diagnosis. Use this before concluding why a trader is not opening orders: it shows candidate coins, wait/hold/open decisions, validation errors, execution logs, and AI call duration.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"trader_id":   map[string]any{"type": "string", "description": "Trader id to diagnose."},
						"trader_name": map[string]any{"type": "string", "description": "Trader name to diagnose. Used to look up the trader when id is not known."},
						"limit":       map[string]any{"type": "number", "description": "Maximum number of recent decision records to return. Default 5, max 20."},
					},
				},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "get_exchange_configs",
				Description: "Get the user's current exchange account bindings. Returns safe metadata only and whether credentials are already stored.",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "manage_exchange_config",
				Description: "Create, update, or delete an exchange account binding. Use this when the user asks to add/edit/remove an exchange account, API key, secret, or account name. Prefer passing exact field values instead of vague summaries. Sensitive fields are stored securely and are never returned in full.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action": map[string]any{
							"type": "string",
							"enum": []string{"create", "update", "delete"},
						},
						"exchange_id":   exchangeConfigFieldsSchema()["exchange_id"],
						"exchange_type": exchangeConfigFieldsSchema()["exchange_type"],
						"account_name":  exchangeConfigFieldsSchema()["account_name"],
						"enabled":       exchangeConfigFieldsSchema()["enabled"],
						"api_key":       exchangeConfigFieldsSchema()["api_key"],
						"secret_key":    exchangeConfigFieldsSchema()["secret_key"],
						"passphrase":    exchangeConfigFieldsSchema()["passphrase"],
						"testnet":       exchangeConfigFieldsSchema()["testnet"],
					},
					"required": []string{"action"},
				},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "get_model_configs",
				Description: "Get the user's current AI model bindings. Returns safe metadata only and whether an API key is already stored.",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "manage_model_config",
				Description: "Create, update, or delete an AI model binding. Use this when the user asks to add/edit/remove a model provider, API key, custom API URL, or custom model name. Prefer passing exact field values instead of vague summaries. Sensitive fields are stored securely and are never returned in full.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action": map[string]any{
							"type": "string",
							"enum": []string{"create", "update", "delete"},
						},
						"model_id":          modelConfigFieldsSchema()["model_id"],
						"provider":          modelConfigFieldsSchema()["provider"],
						"name":              modelConfigFieldsSchema()["name"],
						"enabled":           modelConfigFieldsSchema()["enabled"],
						"api_key":           modelConfigFieldsSchema()["api_key"],
						"custom_api_url":    modelConfigFieldsSchema()["custom_api_url"],
						"custom_model_name": modelConfigFieldsSchema()["custom_model_name"],
						"thinking_mode": map[string]any{
							"type":        "string",
							"enum":        []string{"enabled", "disabled"},
							"description": "DeepSeek thinking mode override for THIS model (omit = inherit env default DEEPSEEK_THINKING_MODE).",
						},
						"reasoning_effort": map[string]any{
							"type":        "string",
							"enum":        []string{"low", "high", "max"},
							"description": "DeepSeek reasoning effort override for THIS model; max is the true maximum (omit = inherit env default AI_REASONING_EFFORT).",
						},
					},
					"required": []string{"action"},
				},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "get_strategies",
				Description: "Get the user's current strategy templates, including system default strategies available to that user.",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "manage_strategy",
				Description: "List, create, update, delete, activate, duplicate strategies, or get the default strategy config template. Use this when the user asks to create or edit a strategy template. Prefer passing precise field-level config patches in `config` instead of vague natural-language summaries.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action": map[string]any{
							"type": "string",
							"enum": []string{"list", "create", "update", "delete", "activate", "duplicate", "get_default_config"},
						},
						"strategy_id":    map[string]any{"type": "string"},
						"name":           map[string]any{"type": "string"},
						"description":    map[string]any{"type": "string"},
						"lang":           map[string]any{"type": "string", "enum": []string{"zh", "en"}},
						"is_public":      map[string]any{"type": "boolean"},
						"config_visible": map[string]any{"type": "boolean"},
						"config":         strategyConfigSchema(),
					},
					"required": []string{"action"},
				},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "manage_trader",
				Description: "List, create, update, delete, start, or stop traders. Trader edits are limited to exchange/model/strategy bindings, scan interval, margin mode, and competition visibility so they match the manual trader panel. If the user wants to modify the internal config of a strategy, model, or exchange, use the corresponding management tool instead.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action": map[string]any{
							"type": "string",
							"enum": []string{"list", "create", "update", "delete", "start", "stop"},
						},
						"trader_id":             traderConfigFieldsSchema()["trader_id"],
						"name":                  traderConfigFieldsSchema()["name"],
						"ai_model_id":           traderConfigFieldsSchema()["ai_model_id"],
						"exchange_id":           traderConfigFieldsSchema()["exchange_id"],
						"strategy_id":           traderConfigFieldsSchema()["strategy_id"],
						"scan_interval_minutes": traderConfigFieldsSchema()["scan_interval_minutes"],
						"is_cross_margin":       traderConfigFieldsSchema()["is_cross_margin"],
						"show_in_competition":   traderConfigFieldsSchema()["show_in_competition"],
					},
					"required": []string{"action"},
				},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "search_stock",
				Description: "Search for a stock by name, ticker symbol, or keyword. Searches across A-share (沪深), Hong Kong, and US markets. Returns a list of matching stocks with their codes. Use this when the user asks about a stock not in your known list, or when you need to find the exact code for a stock.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"keyword": map[string]any{
							"type":        "string",
							"description": "Search keyword: stock name (e.g. '宁德时代', '腾讯'), ticker (e.g. 'TSLA', 'AAPL'), or stock code (e.g. '300750')",
						},
					},
					"required": []string{"keyword"},
				},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "execute_trade",
				Description: "Execute a trade order (US stocks). Use this only when the user explicitly asks to trade. For stocks (e.g. AAPL, TSLA), use open_long to buy and close_long to sell. This creates a pending trade first; it does not execute immediately. Large orders require an extra confirmation with 确认大额 trade_xxx / confirm large trade_xxx, and pending trades expire after 5 minutes.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action": map[string]any{
							"type":        "string",
							"enum":        []string{"open_long", "open_short", "close_long", "close_short"},
							"description": "Trade action: open_long (做多/buy), open_short (做空/sell), close_long (平多), close_short (平空)",
						},
						"symbol": map[string]any{
							"type":        "string",
							"description": "Trading symbol. For US stocks: AAPL, TSLA, NVDA (no suffix needed).",
						},
						"quantity": map[string]any{
							"type":        "number",
							"description": "Trade quantity/amount. Required for opening positions. Use 0 to close entire position.",
						},
						"leverage": map[string]any{
							"type":        "number",
							"description": "Leverage multiplier (e.g. 5, 10, 20). Optional, defaults to trader's current setting.",
						},
						"stop_loss": map[string]any{
							"type":        "number",
							"description": "Protective stop price (absolute). REQUIRED for open_long/open_short: an open proposed without its own stop AND target is refused before any confirmation is asked.",
						},
						"take_profit": map[string]any{
							"type":        "number",
							"description": "Take-profit price (absolute). REQUIRED for open_long/open_short, on the far side of the price from the stop.",
						},
					},
					"required": []string{"action", "symbol", "quantity"},
				},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "get_positions",
				Description: "Get all current open positions across all traders. Returns symbol, side, size, entry price, mark price, and unrealized PnL.",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "get_balance",
				Description: "Get account balance and equity across all traders.",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "get_market_price",
				Description: "Get the current market price for a stock symbol.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"symbol": map[string]any{
							"type":        "string",
							"description": "Trading symbol, e.g. AAPL for stocks",
						},
					},
					"required": []string{"symbol"},
				},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "get_trade_history",
				Description: "Get recent closed trade history with PnL. Use when user asks about past trades, performance, or trade results. Returns the most recent closed positions.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"limit": map[string]any{
							"type":        "number",
							"description": "Number of recent trades to return (default 10, max 50)",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: mcp.FunctionDef{
				Name:        "get_candidate_coins",
				Description: "Get the current candidate symbol list for a trader or strategy, including coin-source settings and the selected symbols.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"trader_id": map[string]any{
							"type":        "string",
							"description": "Optional trader id. Prefer this when asking about a running trader.",
						},
						"strategy_id": map[string]any{
							"type":        "string",
							"description": "Optional strategy id. Use this when asking about a strategy template directly.",
						},
					},
				},
			},
		},
	}
}

// handleToolCall processes a single tool call from the LLM and returns the result.
func (a *Agent) handleToolCall(ctx context.Context, storeUserID string, userID int64, lang string, tc mcp.ToolCall) string {
	switch tc.Function.Name {
	case "get_preferences":
		return a.toolGetPreferences(userID)
	case "manage_preferences":
		return a.toolManagePreferences(userID, tc.Function.Arguments)
	case "get_backend_logs":
		return a.toolGetBackendLogs(storeUserID, tc.Function.Arguments)
	case "get_decisions":
		return a.toolGetDecisions(storeUserID, tc.Function.Arguments)
	case "get_exchange_configs":
		return a.toolGetExchangeConfigs(storeUserID)
	case "manage_exchange_config":
		return a.toolManageExchangeConfig(storeUserID, tc.Function.Arguments)
	case "get_model_configs":
		return a.toolGetModelConfigs(storeUserID)
	case "manage_model_config":
		return a.toolManageModelConfig(storeUserID, tc.Function.Arguments)
	case "get_strategies":
		return a.toolGetStrategies(storeUserID)
	case "manage_strategy":
		return a.toolManageStrategy(storeUserID, tc.Function.Arguments)
	case "manage_trader":
		return a.toolManageTrader(storeUserID, tc.Function.Arguments)
	case "search_stock":
		return a.toolSearchStock(tc.Function.Arguments)
	case "execute_trade":
		return a.toolExecuteTrade(ctx, userID, lang, tc.Function.Arguments)
	case "get_positions":
		return a.toolGetPositions(storeUserID)
	case "get_balance":
		return a.toolGetBalance(storeUserID)
	case "get_market_price":
		return a.toolGetMarketPrice(tc.Function.Arguments)
	case "get_trade_history":
		return a.toolGetTradeHistory(tc.Function.Arguments)
	case "get_candidate_coins":
		return a.toolGetCandidateCoins(storeUserID, userID, tc.Function.Arguments)
	default:
		return fmt.Sprintf(`{"error": "unknown tool: %s"}`, tc.Function.Name)
	}
}

type safeExchangeToolConfig struct {
	ID            string `json:"id"`
	ExchangeType  string `json:"exchange_type"`
	AccountName   string `json:"account_name"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	Enabled       bool   `json:"enabled"`
	HasAPIKey     bool   `json:"has_api_key"`
	HasSecretKey  bool   `json:"has_secret_key"`
	HasPassphrase bool   `json:"has_passphrase"`
	Testnet       bool   `json:"testnet"`
}

type safeModelToolConfig struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	Enabled         bool   `json:"enabled"`
	HasAPIKey       bool   `json:"has_api_key"`
	CustomAPIURL    string `json:"custom_api_url,omitempty"`
	CustomModelName string `json:"custom_model_name,omitempty"`
	ThinkingMode    string `json:"thinking_mode,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	WalletAddress   string `json:"wallet_address,omitempty"`
	BalanceUSDC     string `json:"balance_usdc,omitempty"`
}

type safeTraderToolConfig struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	AIModelID           string  `json:"ai_model_id"`
	ExchangeID          string  `json:"exchange_id"`
	StrategyID          string  `json:"strategy_id,omitempty"`
	InitialBalance      float64 `json:"initial_balance"`
	ScanIntervalMinutes int     `json:"scan_interval_minutes"`
	IsRunning           bool    `json:"is_running"`
	IsCrossMargin       bool    `json:"is_cross_margin"`
	ShowInCompetition   bool    `json:"show_in_competition"`
}

type safeStrategyToolConfig struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	IsActive      bool           `json:"is_active"`
	IsDefault     bool           `json:"is_default"`
	IsPublic      bool           `json:"is_public"`
	ConfigVisible bool           `json:"config_visible"`
	Config        map[string]any `json:"config,omitempty"`
	HasConfig     bool           `json:"has_config"`
}

var sensitiveToolKeys = map[string]struct{}{
	"api_key":                     {},
	"secret_key":                  {},
	"passphrase":                  {},
	"private_key":                 {},
	"password_hash":               {},
	"lighter_api_key_private_key": {},
}

func stripSensitiveToolFields(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cleaned := make(map[string]any, len(typed))
		for key, inner := range typed {
			if _, blocked := sensitiveToolKeys[strings.ToLower(strings.TrimSpace(key))]; blocked {
				continue
			}
			cleaned[key] = stripSensitiveToolFields(inner)
		}
		return cleaned
	case []any:
		out := make([]any, 0, len(typed))
		for _, inner := range typed {
			out = append(out, stripSensitiveToolFields(inner))
		}
		return out
	default:
		return value
	}
}

type manageTraderArgs struct {
	Action              string `json:"action"`
	TraderID            string `json:"trader_id"`
	Name                string `json:"name"`
	AIModelID           string `json:"ai_model_id"`
	ExchangeID          string `json:"exchange_id"`
	StrategyID          string `json:"strategy_id"`
	ScanIntervalMinutes *int   `json:"scan_interval_minutes"`
	IsCrossMargin       *bool  `json:"is_cross_margin"`
	ShowInCompetition   *bool  `json:"show_in_competition"`
}

func safeExchangeForTool(ex *store.Exchange) safeExchangeToolConfig {
	return safeExchangeToolConfig{
		ID:            ex.ID,
		ExchangeType:  ex.ExchangeType,
		AccountName:   ex.AccountName,
		Name:          ex.Name,
		Type:          ex.Type,
		Enabled:       ex.Enabled,
		HasAPIKey:     ex.APIKey != "",
		HasSecretKey:  ex.SecretKey != "",
		HasPassphrase: ex.Passphrase != "",
		Testnet:       ex.Testnet,
	}
}

func defaultTraderInitialBalanceFetcher(exchangeCfg *store.Exchange, userID string) (float64, bool, error) {
	if exchangeCfg == nil {
		return 0, false, fmt.Errorf("exchange config not found")
	}
	probe, err := buildTraderExchangeProbe(exchangeCfg, userID)
	if err != nil {
		return 0, false, err
	}
	balanceInfo, err := probe.GetBalance()
	if err != nil {
		return 0, false, err
	}
	return extractTraderInitialBalance(balanceInfo)
}

func buildTraderExchangeProbe(exchangeCfg *store.Exchange, userID string) (trader.Trader, error) {
	switch exchangeCfg.ExchangeType {
	case "ninjatrader":
		return ntTrader.New(ntTrader.Config{
			DataDir: exchangeCfg.NTDataDir,
			Symbol:  exchangeCfg.NTInstrumentName,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported exchange type: %s", exchangeCfg.ExchangeType)
	}
}

func extractTraderInitialBalance(balanceInfo map[string]interface{}) (float64, bool, error) {
	for _, key := range []string{"total_equity", "totalEquity", "totalWalletBalance", "wallet_balance", "totalEq", "balance"} {
		raw, ok := balanceInfo[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case float64:
			return v, true, nil
		case float32:
			return float64(v), true, nil
		case int:
			return float64(v), true, nil
		case int64:
			return float64(v), true, nil
		case int32:
			return float64(v), true, nil
		case string:
			parsed, err := strconv.ParseFloat(v, 64)
			if err == nil {
				return parsed, true, nil
			}
		}
	}
	return 0, false, fmt.Errorf("initial balance not set and unable to fetch balance from exchange")
}

func safeModelForTool(model *store.AIModel) safeModelToolConfig {
	safeModel := safeModelToolConfig{
		ID:              model.ID,
		Name:            model.Name,
		Provider:        model.Provider,
		Enabled:         model.Enabled,
		HasAPIKey:       model.APIKey != "",
		CustomAPIURL:    model.CustomAPIURL,
		CustomModelName: model.CustomModelName,
	}
	return safeModel
}

func modelConfigUsable(provider, modelID, apiKey, customAPIURL, customModelName string) bool {
	if strings.TrimSpace(apiKey) == "" {
		return false
	}
	resolvedURL, resolvedModel := resolveModelRuntimeConfig(provider, customAPIURL, customModelName, modelID)
	return strings.TrimSpace(resolvedURL) != "" && strings.TrimSpace(resolvedModel) != ""
}

func safeTraderForTool(trader *store.Trader, isRunning bool) safeTraderToolConfig {
	return safeTraderToolConfig{
		ID:                  trader.ID,
		Name:                trader.Name,
		AIModelID:           trader.AIModelID,
		ExchangeID:          trader.ExchangeID,
		StrategyID:          trader.StrategyID,
		InitialBalance:      trader.InitialBalance,
		ScanIntervalMinutes: trader.ScanIntervalMinutes,
		IsRunning:           isRunning,
		IsCrossMargin:       trader.IsCrossMargin,
		ShowInCompetition:   trader.ShowInCompetition,
	}
}

func safeStrategyForTool(strategy *store.Strategy) safeStrategyToolConfig {
	out := safeStrategyToolConfig{
		ID:            strategy.ID,
		Name:          strategy.Name,
		Description:   strategy.Description,
		IsActive:      strategy.IsActive,
		IsDefault:     strategy.IsDefault,
		IsPublic:      strategy.IsPublic,
		ConfigVisible: strategy.ConfigVisible,
		HasConfig:     strings.TrimSpace(strategy.Config) != "",
	}
	if out.HasConfig {
		var cfg map[string]any
		if err := json.Unmarshal([]byte(strategy.Config), &cfg); err == nil {
			out.Config = cfg
		}
	}
	return out
}

func (a *Agent) toolGetExchangeConfigs(storeUserID string) string {
	if a.store == nil {
		return `{"error":"store unavailable"}`
	}
	exchanges, err := a.store.Exchange().List(storeUserID)
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to load exchange configs: %s"}`, err)
	}
	safe := make([]safeExchangeToolConfig, 0, len(exchanges))
	for _, ex := range exchanges {
		if !store.IsVisibleExchange(ex) {
			continue
		}
		safe = append(safe, safeExchangeForTool(ex))
	}
	result, _ := json.Marshal(map[string]any{
		"exchange_configs": safe,
		"count":            len(safe),
	})
	var payload any
	if err := json.Unmarshal(result, &payload); err == nil {
		result, _ = json.Marshal(stripSensitiveToolFields(payload))
	}
	return string(result)
}

func latestBackendLogFilePath() string {
	// BOTH prefixes; the newest by MTIME wins (lexicographic order would let
	// a vl_/nofx_ prefix flip the choice — R5 removes the nofx glob).
	var matches []string
	for _, pat := range []string{"vl_*.log", "nofx_*.log"} {
		hits, err := filepath.Glob(filepath.Join("data", pat))
		if err == nil {
			matches = append(matches, hits...)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	best, bestAt := "", time.Time{}
	for _, m := range matches {
		if st, err := os.Stat(m); err == nil && st.ModTime().After(bestAt) {
			best, bestAt = m, st.ModTime()
		}
	}
	if best == "" {
		return matches[len(matches)-1] // nothing statable: keep the old lexicographic result
	}
	return best
}

func isBackendErrorLikeLogLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "[erro]") ||
		strings.Contains(lower, " panic") ||
		strings.Contains(lower, "🔥") ||
		strings.Contains(lower, "❌") ||
		strings.Contains(lower, " failed") ||
		strings.Contains(lower, " error") ||
		strings.Contains(lower, "invalid ")
}

func readBackendLogEntries(limit int, contains string, errorsOnly bool) (string, []string, error) {
	path := latestBackendLogFilePath()
	if path == "" {
		return "", nil, fmt.Errorf("backend log file not found")
	}
	file, err := os.Open(path)
	if err != nil {
		return path, nil, err
	}
	defer file.Close()

	filter := strings.ToLower(strings.TrimSpace(contains))
	matches := make([]string, 0, max(limit, 1))
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if errorsOnly && !isBackendErrorLikeLogLine(line) {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(line), filter) {
			continue
		}
		matches = append(matches, line)
	}
	if err := scanner.Err(); err != nil {
		return path, nil, err
	}
	if limit <= 0 {
		limit = 30
	}
	if len(matches) > limit {
		matches = matches[len(matches)-limit:]
	}
	return path, matches, nil
}

func filterBackendLogEntriesAny(entries []string, needles ...string) []string {
	if len(entries) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(needles))
	for _, needle := range needles {
		needle = strings.ToLower(strings.TrimSpace(needle))
		if needle == "" {
			continue
		}
		normalized = append(normalized, needle)
	}
	if len(normalized) == 0 {
		return entries
	}
	filtered := make([]string, 0, len(entries))
	for _, entry := range entries {
		lower := strings.ToLower(entry)
		for _, needle := range normalized {
			if strings.Contains(lower, needle) {
				filtered = append(filtered, entry)
				break
			}
		}
	}
	return filtered
}

func (a *Agent) resolveTraderForTool(storeUserID, traderID, traderName string) (*store.Trader, error) {
	traderID = strings.TrimSpace(traderID)
	traderName = strings.TrimSpace(traderName)
	if traderID == "" && traderName == "" {
		return nil, fmt.Errorf("trader_id or trader_name is required")
	}
	if traderID != "" {
		traderCfg, err := a.store.Trader().GetByID(traderID)
		if err != nil {
			return nil, fmt.Errorf("failed to load trader: %w", err)
		}
		if traderCfg.UserID != storeUserID {
			return nil, fmt.Errorf("trader not found for current user")
		}
		return traderCfg, nil
	}
	traders, err := a.store.Trader().List(storeUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to list traders: %w", err)
	}
	for _, traderCfg := range traders {
		if strings.EqualFold(strings.TrimSpace(traderCfg.Name), traderName) {
			return traderCfg, nil
		}
	}
	return nil, fmt.Errorf("trader %q not found", traderName)
}

func (a *Agent) toolGetBackendLogs(storeUserID, argsJSON string) string {
	var args struct {
		TraderID   string `json:"trader_id"`
		TraderName string `json:"trader_name"`
		Limit      int    `json:"limit"`
		ErrorsOnly *bool  `json:"errors_only"`
	}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf(`{"error":"invalid arguments: %s"}`, err)
		}
	}
	if a.store == nil {
		return `{"error":"store unavailable"}`
	}
	errorsOnly := true
	if args.ErrorsOnly != nil {
		errorsOnly = *args.ErrorsOnly
	}
	traderCfg, err := a.resolveTraderForTool(storeUserID, args.TraderID, args.TraderName)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err)
	}
	path, entries, err := readBackendLogEntries(args.Limit, "", errorsOnly)
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to read backend logs: %s"}`, err)
	}
	entries = filterBackendLogEntriesAny(entries, traderCfg.ID, traderCfg.Name)
	if args.Limit <= 0 {
		args.Limit = 30
	}
	if len(entries) > args.Limit {
		entries = entries[len(entries)-args.Limit:]
	}
	result, _ := json.Marshal(map[string]any{
		"trader_id":   traderCfg.ID,
		"trader_name": traderCfg.Name,
		"log_file":    path,
		"entries":     entries,
		"count":       len(entries),
		"errors_only": errorsOnly,
	})
	return string(result)
}

func (a *Agent) toolGetDecisions(storeUserID, argsJSON string) string {
	var args struct {
		TraderID   string `json:"trader_id"`
		TraderName string `json:"trader_name"`
		Limit      int    `json:"limit"`
	}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf(`{"error":"invalid arguments: %s"}`, err)
		}
	}
	if a.store == nil {
		return `{"error":"store unavailable"}`
	}
	traderCfg, err := a.resolveTraderForTool(storeUserID, args.TraderID, args.TraderName)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err)
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	records, err := a.store.Decision().GetLatestRecords(traderCfg.ID, limit)
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to get decision records: %s"}`, err)
	}
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, map[string]any{
			"id":                     record.ID,
			"cycle_number":           record.CycleNumber,
			"timestamp":              record.Timestamp,
			"success":                record.Success,
			"error_message":          record.ErrorMessage,
			"ai_request_duration_ms": record.AIRequestDurationMs,
			"candidate_coins":        record.CandidateCoins,
			"execution_log":          record.ExecutionLog,
			"decisions":              record.Decisions,
			"decision_json":          record.DecisionJSON,
		})
	}
	result, _ := json.Marshal(map[string]any{
		"trader_id":   traderCfg.ID,
		"trader_name": traderCfg.Name,
		"count":       len(items),
		"records":     items,
	})
	return string(result)
}

func (a *Agent) toolManageExchangeConfig(storeUserID, argsJSON string) string {
	if a.store == nil {
		return `{"error":"store unavailable"}`
	}
	var args struct {
		Action               string `json:"action"`
		ExchangeID           string `json:"exchange_id"`
		ExchangeType         string `json:"exchange_type"`
		AccountName          string `json:"account_name"`
		Enabled              *bool  `json:"enabled"`
		APIKey               string `json:"api_key"`
		SecretKey            string `json:"secret_key"`
		Passphrase           string `json:"passphrase"`
		Testnet              *bool  `json:"testnet"`
		NTDataDir            string `json:"nt_data_dir"`
		NTInstrumentName     string `json:"nt_instrument_name"`
		NTDefaultContractQty int    `json:"nt_default_contract_qty"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf(`{"error":"invalid arguments: %s"}`, err)
	}
	action := strings.TrimSpace(args.Action)
	switch action {
	case "create":
		missing := missingRequiredActionSlots("exchange_management", "create", map[string]string{
			"exchange_type": strings.TrimSpace(args.ExchangeType),
			"account_name":  strings.TrimSpace(args.AccountName),
		})
		if len(missing) > 0 {
			return fmt.Sprintf(`{"error":"missing required fields for create: %s"}`, strings.Join(missing, ", "))
		}
		exchangeType := strings.TrimSpace(args.ExchangeType)
		if exchangeType == "" {
			return `{"error":"exchange_type is required for create"}`
		}
		enabled := true
		testnet := false
		if args.Testnet != nil {
			testnet = *args.Testnet
		}
		if err := (exchangeConfigValidator{
			exchangeType: exchangeType,
			enabled:      enabled,
			apiKey:       strings.TrimSpace(args.APIKey),
			secretKey:    strings.TrimSpace(args.SecretKey),
			passphrase:   strings.TrimSpace(args.Passphrase),
			ntDataDir:    strings.TrimSpace(args.NTDataDir),
		}).Validate(); err != nil {
			return fmt.Sprintf(`{"error":"%s"}`, err)
		}
		if err := a.ensureUniqueExchangeAccountName(storeUserID, strings.TrimSpace(args.AccountName), ""); err != nil {
			return fmt.Sprintf(`{"error":"%s"}`, err)
		}
		id, err := a.store.Exchange().Create(
			storeUserID,
			exchangeType,
			strings.TrimSpace(args.AccountName),
			enabled,
			strings.TrimSpace(args.APIKey),
			strings.TrimSpace(args.SecretKey),
			strings.TrimSpace(args.Passphrase),
			testnet,
			strings.TrimSpace(args.NTDataDir),
			strings.TrimSpace(args.NTInstrumentName),
			args.NTDefaultContractQty,
		)
		if err != nil {
			return fmt.Sprintf(`{"error":"failed to create exchange config: %s"}`, err)
		}
		created, err := a.store.Exchange().GetByID(storeUserID, id)
		if err != nil {
			return fmt.Sprintf(`{"error":"exchange created but failed to reload: %s"}`, err)
		}
		result, _ := json.Marshal(map[string]any{
			"status":   "ok",
			"action":   "create",
			"exchange": safeExchangeForTool(created),
		})
		var payload any
		if err := json.Unmarshal(result, &payload); err == nil {
			result, _ = json.Marshal(stripSensitiveToolFields(payload))
		}
		return string(result)
	case "query":
		if strings.TrimSpace(args.ExchangeID) == "" {
			return `{"error":"exchange_id is required for query"}`
		}
		existing, err := a.store.Exchange().GetByID(storeUserID, strings.TrimSpace(args.ExchangeID))
		if err != nil {
			return fmt.Sprintf(`{"error":"failed to load exchange config: %s"}`, err)
		}
		result, _ := json.Marshal(map[string]any{
			"status":   "ok",
			"action":   "query",
			"exchange": safeExchangeForTool(existing),
		})
		var payload any
		if err := json.Unmarshal(result, &payload); err == nil {
			result, _ = json.Marshal(stripSensitiveToolFields(payload))
		}
		return string(result)
	case "update":
		if strings.TrimSpace(args.ExchangeID) == "" {
			return `{"error":"exchange_id is required for update"}`
		}
		existing, err := a.store.Exchange().GetByID(storeUserID, strings.TrimSpace(args.ExchangeID))
		if err != nil {
			return fmt.Sprintf(`{"error":"failed to load exchange config: %s"}`, err)
		}
		enabled := true
		testnet := existing.Testnet
		if args.Testnet != nil {
			testnet = *args.Testnet
		}
		effectiveAPIKey := strings.TrimSpace(string(existing.APIKey))
		if trimmed := strings.TrimSpace(args.APIKey); trimmed != "" {
			effectiveAPIKey = trimmed
		}
		effectiveSecretKey := strings.TrimSpace(string(existing.SecretKey))
		if trimmed := strings.TrimSpace(args.SecretKey); trimmed != "" {
			effectiveSecretKey = trimmed
		}
		effectivePassphrase := strings.TrimSpace(string(existing.Passphrase))
		if trimmed := strings.TrimSpace(args.Passphrase); trimmed != "" {
			effectivePassphrase = trimmed
		}
		ntDataDir := existing.NTDataDir
		if trimmed := strings.TrimSpace(args.NTDataDir); trimmed != "" {
			ntDataDir = trimmed
		}
		ntInstrument := existing.NTInstrumentName
		if trimmed := strings.TrimSpace(args.NTInstrumentName); trimmed != "" {
			ntInstrument = trimmed
		}
		ntQty := existing.NTDefaultContractQty
		if args.NTDefaultContractQty != 0 {
			ntQty = args.NTDefaultContractQty
		}
		validator := exchangeConfigValidator{
			exchangeType: existing.ExchangeType,
			enabled:      true,
			apiKey:       effectiveAPIKey,
			secretKey:    effectiveSecretKey,
			passphrase:   effectivePassphrase,
			ntDataDir:    ntDataDir,
		}
		if err := validator.Validate(); err != nil {
			return fmt.Sprintf(`{"error":"%s"}`, err)
		}
		if err := a.store.Exchange().Update(
			storeUserID,
			existing.ID,
			enabled,
			strings.TrimSpace(args.APIKey),
			strings.TrimSpace(args.SecretKey),
			strings.TrimSpace(args.Passphrase),
			testnet,
			ntDataDir, ntInstrument, ntQty,
		); err != nil {
			return fmt.Sprintf(`{"error":"failed to update exchange config: %s"}`, err)
		}
		if trimmed := strings.TrimSpace(args.AccountName); trimmed != "" && trimmed != existing.AccountName {
			if err := a.ensureUniqueExchangeAccountName(storeUserID, trimmed, existing.ID); err != nil {
				return fmt.Sprintf(`{"error":"%s"}`, err)
			}
			if err := a.store.Exchange().UpdateAccountName(storeUserID, existing.ID, trimmed); err != nil {
				return fmt.Sprintf(`{"error":"exchange updated but failed to rename account: %s"}`, err)
			}
		}
		updated, err := a.store.Exchange().GetByID(storeUserID, existing.ID)
		if err != nil {
			return fmt.Sprintf(`{"error":"exchange updated but failed to reload: %s"}`, err)
		}
		result, _ := json.Marshal(map[string]any{
			"status":   "ok",
			"action":   "update",
			"exchange": safeExchangeForTool(updated),
		})
		var payload any
		if err := json.Unmarshal(result, &payload); err == nil {
			result, _ = json.Marshal(stripSensitiveToolFields(payload))
		}
		return string(result)
	case "delete":
		if strings.TrimSpace(args.ExchangeID) == "" {
			return `{"error":"exchange_id is required for delete"}`
		}
		if err := a.store.Exchange().Delete(storeUserID, strings.TrimSpace(args.ExchangeID)); err != nil {
			return fmt.Sprintf(`{"error":"failed to delete exchange config: %s"}`, err)
		}
		result, _ := json.Marshal(map[string]any{
			"status":      "ok",
			"action":      "delete",
			"exchange_id": strings.TrimSpace(args.ExchangeID),
		})
		return string(result)
	default:
		return `{"error":"invalid action"}`
	}
}

func (a *Agent) toolGetModelConfigs(storeUserID string) string {
	if a.store == nil {
		return `{"error":"store unavailable"}`
	}
	models, err := a.store.AIModel().List(storeUserID)
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to load model configs: %s"}`, err)
	}
	safe := make([]safeModelToolConfig, 0, len(models))
	for _, model := range models {
		if !store.IsVisibleAIModel(model) {
			continue
		}
		safe = append(safe, safeModelForTool(model))
	}
	result, _ := json.Marshal(map[string]any{
		"model_configs": safe,
		"count":         len(safe),
	})
	var payload any
	if err := json.Unmarshal(result, &payload); err == nil {
		result, _ = json.Marshal(stripSensitiveToolFields(payload))
	}
	return string(result)
}

func (a *Agent) toolManageModelConfig(storeUserID, argsJSON string) string {
	if a.store == nil {
		return `{"error":"store unavailable"}`
	}
	var args struct {
		Action          string `json:"action"`
		ModelID         string `json:"model_id"`
		Provider        string `json:"provider"`
		Name            string `json:"name"`
		Enabled         *bool  `json:"enabled"`
		APIKey          string `json:"api_key"`
		CustomAPIURL    string `json:"custom_api_url"`
		CustomModelName string `json:"custom_model_name"`
		ThinkingMode    string `json:"thinking_mode"`
		ReasoningEffort string `json:"reasoning_effort"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf(`{"error":"invalid arguments: %s"}`, err)
	}
	// 4.5 — per-model DeepSeek thinking knobs (empty = inherit env defaults).
	thinkingMode := strings.ToLower(strings.TrimSpace(args.ThinkingMode))
	reasoningEffort := strings.ToLower(strings.TrimSpace(args.ReasoningEffort))
	if err := mcp.ValidateThinkingKnobs(thinkingMode, reasoningEffort); err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	if trimmed := strings.TrimSpace(args.CustomAPIURL); trimmed != "" {
		if err := security.ValidateURL(strings.TrimSuffix(trimmed, "#")); err != nil {
			return fmt.Sprintf(`{"error":"invalid custom_api_url: %s"}`, err)
		}
	}
	action := strings.TrimSpace(args.Action)
	switch action {
	case "create":
		missing := missingRequiredActionSlots("model_management", "create", map[string]string{
			"provider": strings.TrimSpace(args.Provider),
		})
		if len(missing) > 0 {
			return fmt.Sprintf(`{"error":"missing required fields for create: %s"}`, strings.Join(missing, ", "))
		}
		provider := strings.TrimSpace(args.Provider)
		if provider == "" {
			return `{"error":"provider is required for create"}`
		}
		if strings.TrimSpace(args.APIKey) == "" {
			return `{"error":"api_key is required for create"}`
		}
		modelID := strings.TrimSpace(args.ModelID)
		if modelID == "" {
			modelID = provider
		}
		// Match the manual settings page: newly created model configs should be
		// enabled unless the caller explicitly asks to keep them disabled.
		enabled := true
		if args.Enabled != nil {
			enabled = *args.Enabled
		}
		name := strings.TrimSpace(args.Name)
		if name == "" {
			name = defaultModelConfigName(provider)
		}
		customModelName := strings.TrimSpace(args.CustomModelName)
		if customModelName == "" && modelProviderSupportsCustomModel(provider) {
			customModelName = defaultModelNameForProvider(provider)
		}
		customAPIURL := strings.TrimSpace(args.CustomAPIURL)
		if !modelProviderSupportsCustomAPIURL(provider) {
			customAPIURL = ""
		}
		if err := (modelConfigValidator{
			provider:        provider,
			enabled:         enabled,
			apiKey:          strings.TrimSpace(args.APIKey),
			customAPIURL:    customAPIURL,
			customModelName: customModelName,
			modelID:         modelID,
		}).Validate(); err != nil {
			return fmt.Sprintf(`{"error":"%s"}`, err)
		}
		existingByProvider, err := a.findModelByProvider(storeUserID, provider)
		if err != nil {
			return fmt.Sprintf(`{"error":"failed to inspect existing model configs: %s"}`, err)
		}
		excludeID := ""
		if existingByProvider != nil {
			modelID = existingByProvider.ID
			excludeID = existingByProvider.ID
		}
		if err := a.ensureUniqueModelName(storeUserID, name, excludeID); err != nil {
			return fmt.Sprintf(`{"error":"%s"}`, err)
		}
		if err := a.store.AIModel().UpdateWithName(
			storeUserID,
			modelID,
			name,
			enabled,
			strings.TrimSpace(args.APIKey),
			customAPIURL,
			customModelName,
		); err != nil {
			return fmt.Sprintf(`{"error":"failed to create model config: %s"}`, err)
		}
		createdID := modelID
		if modelID == provider {
			createdID = fmt.Sprintf("%s_%s", storeUserID, provider)
		}
		if err := a.store.AIModel().UpdateThinking(storeUserID, createdID, thinkingMode, reasoningEffort); err != nil {
			a.log().Warn("thinking knobs not persisted", "model_id", createdID, "error", err)
		}
		model, err := a.store.AIModel().Get(storeUserID, createdID)
		if err != nil {
			model, err = a.store.AIModel().Get(storeUserID, modelID)
		}
		if err != nil {
			return fmt.Sprintf(`{"error":"model created but failed to reload: %s"}`, err)
		}
		result, _ := json.Marshal(map[string]any{
			"status": "ok",
			"action": "create",
			"model":  safeModelForTool(model),
		})
		var payload any
		if err := json.Unmarshal(result, &payload); err == nil {
			result, _ = json.Marshal(stripSensitiveToolFields(payload))
		}
		return string(result)
	case "update":
		modelID := strings.TrimSpace(args.ModelID)
		if modelID == "" {
			return `{"error":"model_id is required for update"}`
		}
		existing, err := a.store.AIModel().Get(storeUserID, modelID)
		if err != nil {
			return fmt.Sprintf(`{"error":"failed to load model config: %s"}`, err)
		}
		enabled := existing.Enabled
		if args.Enabled != nil {
			enabled = *args.Enabled
		}
		customAPIURL := existing.CustomAPIURL
		if strings.TrimSpace(args.CustomAPIURL) != "" {
			customAPIURL = strings.TrimSpace(args.CustomAPIURL)
		}
		customModelName := existing.CustomModelName
		if strings.TrimSpace(args.CustomModelName) != "" {
			customModelName = strings.TrimSpace(args.CustomModelName)
		}
		apiKey := strings.TrimSpace(args.APIKey)
		effectiveAPIKey := string(existing.APIKey)
		if apiKey != "" {
			effectiveAPIKey = apiKey
		}
		if err := (modelConfigValidator{
			provider:        existing.Provider,
			enabled:         enabled,
			apiKey:          effectiveAPIKey,
			customAPIURL:    customAPIURL,
			customModelName: customModelName,
			modelID:         existing.ID,
		}).Validate(); err != nil {
			return fmt.Sprintf(`{"error":"%s"}`, err)
		}
		if trimmed := strings.TrimSpace(args.Name); trimmed != "" && !sameEntityName(trimmed, existing.Name) {
			if err := a.ensureUniqueModelName(storeUserID, trimmed, existing.ID); err != nil {
				return fmt.Sprintf(`{"error":"%s"}`, err)
			}
		}
		if err := a.store.AIModel().UpdateWithName(
			storeUserID,
			existing.ID,
			strings.TrimSpace(args.Name),
			enabled,
			apiKey,
			customAPIURL,
			customModelName,
		); err != nil {
			return fmt.Sprintf(`{"error":"failed to update model config: %s"}`, err)
		}
		if err := a.store.AIModel().UpdateThinking(storeUserID, existing.ID, thinkingMode, reasoningEffort); err != nil {
			a.log().Warn("thinking knobs not persisted", "model_id", existing.ID, "error", err)
		}
		updated, err := a.store.AIModel().Get(storeUserID, existing.ID)
		if err != nil {
			return fmt.Sprintf(`{"error":"model updated but failed to reload: %s"}`, err)
		}
		result, _ := json.Marshal(map[string]any{
			"status": "ok",
			"action": "update",
			"model":  safeModelForTool(updated),
		})
		var payload any
		if err := json.Unmarshal(result, &payload); err == nil {
			result, _ = json.Marshal(stripSensitiveToolFields(payload))
		}
		return string(result)
	case "delete":
		modelID := strings.TrimSpace(args.ModelID)
		if modelID == "" {
			return `{"error":"model_id is required for delete"}`
		}
		if err := a.store.AIModel().Delete(storeUserID, modelID); err != nil {
			return fmt.Sprintf(`{"error":"failed to delete model config: %s"}`, err)
		}
		result, _ := json.Marshal(map[string]any{
			"status":   "ok",
			"action":   "delete",
			"model_id": modelID,
		})
		return string(result)
	default:
		return `{"error":"invalid action"}`
	}
}

func (a *Agent) toolGetStrategies(storeUserID string) string {
	if a.store == nil {
		return `{"error":"store unavailable"}`
	}
	strategies, err := a.store.Strategy().List(storeUserID)
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to load strategies: %s"}`, err)
	}
	safeStrategies := make([]safeStrategyToolConfig, 0, len(strategies))
	for _, strategy := range strategies {
		if !store.IsVisibleStrategy(strategy) {
			continue
		}
		safeStrategies = append(safeStrategies, safeStrategyForTool(strategy))
	}
	result, _ := json.Marshal(map[string]any{
		"strategies": safeStrategies,
		"count":      len(safeStrategies),
	})
	return string(result)
}

func (a *Agent) toolManageStrategy(storeUserID, argsJSON string) string {
	if a.store == nil {
		return `{"error":"store unavailable"}`
	}
	var args struct {
		Action        string         `json:"action"`
		StrategyID    string         `json:"strategy_id"`
		Name          string         `json:"name"`
		Description   string         `json:"description"`
		Lang          string         `json:"lang"`
		IsPublic      *bool          `json:"is_public"`
		ConfigVisible *bool          `json:"config_visible"`
		AllowClamped  bool           `json:"allow_clamped_update"`
		Confirmed     bool           `json:"confirmed"`
		Config        map[string]any `json:"config"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf(`{"error":"invalid arguments: %s"}`, err)
	}

	switch strings.TrimSpace(args.Action) {
	case "list":
		return a.toolGetStrategies(storeUserID)
	case "get_default_config":
		lang := strings.TrimSpace(args.Lang)
		if lang != "zh" {
			lang = "en"
		}
		cfg := store.GetDefaultStrategyConfig(lang)
		payload, _ := json.Marshal(map[string]any{
			"status": "ok",
			"action": "get_default_config",
			"config": cfg,
		})
		return string(payload)
	case "create":
		name := strings.TrimSpace(args.Name)
		if name == "" {
			return `{"error":"name is required for create"}`
		}
		if !args.Confirmed {
			return `{"error":"strategy create requires explicit chat confirmation before execution. Present the strategy config summary to the user and ask them to reply 确认创建; do not claim the strategy was created.","requires_confirmation":true}`
		}
		if lockedField, ok := strategyConfigContainsLockedField(args.Config); ok {
			return fmt.Sprintf(`{"error":"%s"}`, strategyLockedFieldError("zh", lockedField))
		}
		if err := a.ensureUniqueStrategyName(storeUserID, name, ""); err != nil {
			return fmt.Sprintf(`{"error":"%s"}`, err)
		}
		defaultConfig := store.GetDefaultStrategyConfig(strings.TrimSpace(args.Lang))
		var cfg any = defaultConfig
		var warnings []string
		if len(args.Config) > 0 {
			merged, err := store.MergeStrategyConfig(defaultConfig, args.Config)
			if err != nil {
				return fmt.Sprintf(`{"error":"invalid strategy config: %s"}`, err)
			}
			before := merged
			merged.ClampLimits()
			warnings = store.StrategyClampWarnings(before, merged, merged.Language)
			if len(warnings) > 0 && !args.AllowClamped {
				return fmt.Sprintf(`{"error":"%s"}`, formatRiskControlRefusalPrompt(merged.Language, warnings, "确认应用"))
			}
			cfg = merged
		}
		configJSON, err := json.Marshal(cfg)
		if err != nil {
			return fmt.Sprintf(`{"error":"failed to serialize strategy config: %s"}`, err)
		}
		record := &store.Strategy{
			ID:            fmt.Sprintf("strategy_%d", time.Now().UnixNano()),
			UserID:        storeUserID,
			Name:          name,
			Description:   strings.TrimSpace(args.Description),
			IsActive:      false,
			IsDefault:     false,
			IsPublic:      args.IsPublic != nil && *args.IsPublic,
			ConfigVisible: args.ConfigVisible == nil || *args.ConfigVisible,
			Config:        string(configJSON),
		}
		if err := a.store.Strategy().Create(record); err != nil {
			return fmt.Sprintf(`{"error":"failed to create strategy: %s"}`, err)
		}
		payload, _ := json.Marshal(map[string]any{
			"status":   "ok",
			"action":   "create",
			"strategy": safeStrategyForTool(record),
			"warnings": warnings,
		})
		return string(payload)
	case "update":
		strategyID := strings.TrimSpace(args.StrategyID)
		if strategyID == "" {
			return `{"error":"strategy_id is required for update"}`
		}
		if lockedField, ok := strategyConfigContainsLockedField(args.Config); ok {
			return fmt.Sprintf(`{"error":"%s"}`, strategyLockedFieldError("zh", lockedField))
		}
		existing, err := a.store.Strategy().Get(storeUserID, strategyID)
		if err != nil {
			return fmt.Sprintf(`{"error":"failed to load strategy: %s"}`, err)
		}
		if existing.IsDefault {
			return `{"error":"cannot modify system default strategy"}`
		}
		name := existing.Name
		if trimmed := strings.TrimSpace(args.Name); trimmed != "" {
			name = trimmed
		}
		if !sameEntityName(name, existing.Name) {
			if err := a.ensureUniqueStrategyName(storeUserID, name, existing.ID); err != nil {
				return fmt.Sprintf(`{"error":"%s"}`, err)
			}
		}
		description := existing.Description
		if trimmed := strings.TrimSpace(args.Description); trimmed != "" {
			description = trimmed
		}
		isPublic := existing.IsPublic
		if args.IsPublic != nil {
			isPublic = *args.IsPublic
		}
		configVisible := existing.ConfigVisible
		if args.ConfigVisible != nil {
			configVisible = *args.ConfigVisible
		}
		configJSON := existing.Config
		var warnings []string
		if len(args.Config) > 0 {
			var existingConfig store.StrategyConfig
			if strings.TrimSpace(existing.Config) != "" {
				if err := json.Unmarshal([]byte(existing.Config), &existingConfig); err != nil {
					return fmt.Sprintf(`{"error":"failed to load existing strategy config: %s"}`, err)
				}
			}
			merged, err := store.MergeStrategyConfig(existingConfig, args.Config)
			if err != nil {
				return fmt.Sprintf(`{"error":"invalid strategy config: %s"}`, err)
			}
			before := merged
			merged.ClampLimits()
			warnings = store.StrategyClampWarnings(before, merged, merged.Language)
			if len(warnings) > 0 && !args.AllowClamped {
				return fmt.Sprintf(`{"error":"%s"}`, formatRiskControlRefusalPrompt(merged.Language, warnings, "确认应用"))
			}
			normalized, err := json.Marshal(merged)
			if err != nil {
				return fmt.Sprintf(`{"error":"failed to serialize strategy config: %s"}`, err)
			}
			configJSON = string(normalized)
		}
		record := &store.Strategy{
			ID:            existing.ID,
			UserID:        storeUserID,
			Name:          name,
			Description:   description,
			IsPublic:      isPublic,
			ConfigVisible: configVisible,
			Config:        configJSON,
		}
		if err := a.store.Strategy().Update(record); err != nil {
			return fmt.Sprintf(`{"error":"failed to update strategy: %s"}`, err)
		}
		updated, err := a.store.Strategy().Get(storeUserID, existing.ID)
		if err != nil {
			return fmt.Sprintf(`{"error":"strategy updated but failed to reload: %s"}`, err)
		}
		payload, _ := json.Marshal(map[string]any{
			"status":   "ok",
			"action":   "update",
			"strategy": safeStrategyForTool(updated),
			"warnings": warnings,
		})
		return string(payload)
	case "delete":
		strategyID := strings.TrimSpace(args.StrategyID)
		if strategyID == "" {
			return `{"error":"strategy_id is required for delete"}`
		}
		if err := a.store.Strategy().Delete(storeUserID, strategyID); err != nil {
			if strings.Contains(err.Error(), "cannot delete active strategy") {
				strategies, listErr := a.store.Strategy().List(storeUserID)
				if listErr != nil {
					return fmt.Sprintf(`{"error":"failed to prepare active strategy deletion: %s"}`, listErr)
				}

				var fallbackID string
				for _, strategy := range strategies {
					if strategy == nil || strategy.ID == strategyID {
						continue
					}
					if strategy.IsDefault {
						fallbackID = strategy.ID
						break
					}
					if fallbackID == "" {
						fallbackID = strategy.ID
					}
				}
				if fallbackID == "" {
					defaultConfig := store.GetDefaultStrategyConfig("zh")
					defaultConfig.ClampLimits()
					configJSON, marshalErr := json.Marshal(defaultConfig)
					if marshalErr != nil {
						return fmt.Sprintf(`{"error":"failed to create fallback strategy config: %s"}`, marshalErr)
					}

					fallbackID = fmt.Sprintf("strategy_%d", time.Now().UnixNano())
					fallbackStrategy := &store.Strategy{
						ID:          fallbackID,
						UserID:      storeUserID,
						Name:        "默认策略",
						Description: "Agent-generated fallback strategy",
						Config:      string(configJSON),
					}
					if createErr := a.store.Strategy().Create(fallbackStrategy); createErr != nil {
						return fmt.Sprintf(`{"error":"failed to create fallback strategy before deletion: %s"}`, createErr)
					}
				}
				if activateErr := a.store.Strategy().SetActive(storeUserID, fallbackID); activateErr != nil {
					return fmt.Sprintf(`{"error":"failed to switch active strategy before deletion: %s"}`, activateErr)
				}
				if retryErr := a.store.Strategy().Delete(storeUserID, strategyID); retryErr != nil {
					return fmt.Sprintf(`{"error":"failed to delete strategy: %s"}`, retryErr)
				}
			} else {
				return fmt.Sprintf(`{"error":"failed to delete strategy: %s"}`, err)
			}
		}
		payload, _ := json.Marshal(map[string]any{
			"status":      "ok",
			"action":      "delete",
			"strategy_id": strategyID,
		})
		return string(payload)
	case "activate":
		strategyID := strings.TrimSpace(args.StrategyID)
		if strategyID == "" {
			return `{"error":"strategy_id is required for activate"}`
		}
		if err := a.store.Strategy().SetActive(storeUserID, strategyID); err != nil {
			return fmt.Sprintf(`{"error":"failed to activate strategy: %s"}`, err)
		}
		updated, err := a.store.Strategy().Get(storeUserID, strategyID)
		if err != nil {
			return fmt.Sprintf(`{"error":"strategy activated but failed to reload: %s"}`, err)
		}
		payload, _ := json.Marshal(map[string]any{
			"status":   "ok",
			"action":   "activate",
			"strategy": safeStrategyForTool(updated),
		})
		return string(payload)
	case "duplicate":
		sourceID := strings.TrimSpace(args.StrategyID)
		name := strings.TrimSpace(args.Name)
		if sourceID == "" {
			return `{"error":"strategy_id is required for duplicate"}`
		}
		if name == "" {
			return `{"error":"name is required for duplicate"}`
		}
		if err := a.ensureUniqueStrategyName(storeUserID, name, ""); err != nil {
			return fmt.Sprintf(`{"error":"%s"}`, err)
		}
		newID := fmt.Sprintf("strategy_%d", time.Now().UnixNano())
		if err := a.store.Strategy().Duplicate(storeUserID, sourceID, newID, name); err != nil {
			return fmt.Sprintf(`{"error":"failed to duplicate strategy: %s"}`, err)
		}
		created, err := a.store.Strategy().Get(storeUserID, newID)
		if err != nil {
			return fmt.Sprintf(`{"error":"strategy duplicated but failed to reload: %s"}`, err)
		}
		payload, _ := json.Marshal(map[string]any{
			"status":   "ok",
			"action":   "duplicate",
			"strategy": safeStrategyForTool(created),
		})
		return string(payload)
	default:
		return `{"error":"invalid action"}`
	}
}

func (a *Agent) toolManageTrader(storeUserID, argsJSON string) string {
	if a.store == nil {
		return `{"error":"store unavailable"}`
	}
	var args manageTraderArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf(`{"error":"invalid arguments: %s"}`, err)
	}

	switch strings.TrimSpace(args.Action) {
	case "list":
		return a.toolListTraders(storeUserID)
	case "create":
		return a.toolCreateTrader(storeUserID, args)
	case "update":
		return a.toolUpdateTrader(storeUserID, args)
	case "delete":
		return a.toolDeleteTrader(storeUserID, strings.TrimSpace(args.TraderID))
	case "start":
		return a.toolStartTrader(storeUserID, strings.TrimSpace(args.TraderID))
	case "stop":
		return a.toolStopTrader(storeUserID, strings.TrimSpace(args.TraderID))
	default:
		return `{"error":"invalid action"}`
	}
}

func (a *Agent) toolListTraders(storeUserID string) string {
	traders, err := a.store.Trader().List(storeUserID)
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to list traders: %s"}`, err)
	}
	if len(traders) == 0 && a != nil && a.store != nil {
		if all, listErr := a.store.Trader().ListAll(); listErr == nil && len(all) > 0 {
			counts := make(map[string]int)
			for _, trader := range all {
				uid := strings.TrimSpace(trader.UserID)
				if uid == "" {
					uid = "default"
				}
				counts[uid]++
			}
			a.log().Warn("toolListTraders returned empty for current store user while traders exist under other user scopes",
				"store_user_id", storeUserID,
				"known_user_scopes", counts,
			)
		}
	}
	safeTraders := make([]safeTraderToolConfig, 0, len(traders))
	for _, traderCfg := range traders {
		if !store.IsVisibleTrader(traderCfg) {
			continue
		}
		isRunning := traderCfg.IsRunning
		if a.traderManager != nil {
			if memTrader, err := a.traderManager.GetTrader(traderCfg.ID); err == nil {
				if running, ok := memTrader.GetStatus()["is_running"].(bool); ok {
					isRunning = running
				}
			}
		}
		safeTraders = append(safeTraders, safeTraderForTool(traderCfg, isRunning))
	}
	result, _ := json.Marshal(map[string]any{
		"traders": safeTraders,
		"count":   len(safeTraders),
	})
	return string(result)
}

func (a *Agent) validateTraderReferences(storeUserID, aiModelID, exchangeID, strategyID string) error {
	return (traderBindingValidator{
		store:       a.store,
		storeUserID: storeUserID,
		aiModelID:   aiModelID,
		exchangeID:  exchangeID,
		strategyID:  strategyID,
	}).Validate()
}

func (a *Agent) toolCreateTrader(storeUserID string, args manageTraderArgs) string {
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return `{"error":"name is required for create"}`
	}
	if err := a.ensureUniqueTraderName(storeUserID, name, ""); err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err)
	}
	if err := a.validateTraderReferences(storeUserID, args.AIModelID, args.ExchangeID, args.StrategyID); err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err)
	}
	exchangeCfg, err := a.store.Exchange().GetByID(storeUserID, strings.TrimSpace(args.ExchangeID))
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to load exchange config: %s"}`, err)
	}
	scanInterval := 3
	if args.ScanIntervalMinutes != nil && *args.ScanIntervalMinutes > 0 {
		scanInterval = *args.ScanIntervalMinutes
		if scanInterval < 3 {
			scanInterval = 3
		}
	}
	initialBalance, found, err := traderInitialBalanceFetcher(exchangeCfg, storeUserID)
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to auto-read trader initial balance from exchange: %s"}`, err)
	}
	if !found {
		return `{"error":"failed to auto-read trader initial balance from exchange"}`
	}
	isCrossMargin := true
	if args.IsCrossMargin != nil {
		isCrossMargin = *args.IsCrossMargin
	}
	showInCompetition := true
	if args.ShowInCompetition != nil {
		showInCompetition = *args.ShowInCompetition
	}
	btcEthLeverage := 10
	altcoinLeverage := 5
	overrideBasePrompt := false
	useAI500 := false
	useOITop := false
	systemPromptTemplate := "default"
	exchangeIDShort := strings.TrimSpace(args.ExchangeID)
	if len(exchangeIDShort) > 8 {
		exchangeIDShort = exchangeIDShort[:8]
	}
	traderID := fmt.Sprintf("%s_%s_%d", exchangeIDShort, strings.TrimSpace(args.AIModelID), time.Now().Unix())
	record := &store.Trader{
		ID:                   traderID,
		UserID:               storeUserID,
		Name:                 name,
		AIModelID:            strings.TrimSpace(args.AIModelID),
		ExchangeID:           strings.TrimSpace(args.ExchangeID),
		StrategyID:           strings.TrimSpace(args.StrategyID),
		InitialBalance:       initialBalance,
		ScanIntervalMinutes:  scanInterval,
		IsRunning:            false,
		IsCrossMargin:        isCrossMargin,
		ShowInCompetition:    showInCompetition,
		BTCETHLeverage:       btcEthLeverage,
		AltcoinLeverage:      altcoinLeverage,
		TradingSymbols:       "",
		UseAI500:             useAI500,
		UseOITop:             useOITop,
		CustomPrompt:         "",
		OverrideBasePrompt:   overrideBasePrompt,
		SystemPromptTemplate: systemPromptTemplate,
	}
	if err := a.store.Trader().Create(record); err != nil {
		return fmt.Sprintf(`{"error":"failed to create trader: %s"}`, err)
	}
	if a.traderManager != nil {
		_ = a.traderManager.LoadUserTradersFromStore(a.store, storeUserID)
	}
	result, _ := json.Marshal(map[string]any{
		"status": "ok",
		"action": "create",
		"trader": safeTraderForTool(record, false),
	})
	return string(result)
}

func (a *Agent) toolUpdateTrader(storeUserID string, args manageTraderArgs) string {
	traderID := strings.TrimSpace(args.TraderID)
	if traderID == "" {
		return `{"error":"trader_id is required for update"}`
	}
	traders, err := a.store.Trader().List(storeUserID)
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to load traders: %s"}`, err)
	}
	var existing *store.Trader
	for _, item := range traders {
		if item.ID == traderID {
			existing = item
			break
		}
	}
	if existing == nil {
		return `{"error":"trader not found"}`
	}
	if trimmed := strings.TrimSpace(args.Name); trimmed != "" && !sameEntityName(trimmed, existing.Name) {
		return `{"error":"trader rename is not supported here; only bindings, scan interval, margin mode, and competition visibility can be edited"}`
	}
	aiModelID := existing.AIModelID
	if trimmed := strings.TrimSpace(args.AIModelID); trimmed != "" {
		aiModelID = trimmed
	}
	exchangeID := existing.ExchangeID
	if trimmed := strings.TrimSpace(args.ExchangeID); trimmed != "" {
		exchangeID = trimmed
	}
	strategyID := existing.StrategyID
	if trimmed := strings.TrimSpace(args.StrategyID); trimmed != "" {
		strategyID = trimmed
	}
	if err := a.validateTraderReferences(storeUserID, aiModelID, exchangeID, strategyID); err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err)
	}
	record := &store.Trader{
		ID:                   existing.ID,
		UserID:               storeUserID,
		Name:                 existing.Name,
		AIModelID:            aiModelID,
		ExchangeID:           exchangeID,
		StrategyID:           strategyID,
		InitialBalance:       existing.InitialBalance,
		ScanIntervalMinutes:  existing.ScanIntervalMinutes,
		IsRunning:            existing.IsRunning,
		IsCrossMargin:        existing.IsCrossMargin,
		ShowInCompetition:    existing.ShowInCompetition,
		BTCETHLeverage:       existing.BTCETHLeverage,
		AltcoinLeverage:      existing.AltcoinLeverage,
		TradingSymbols:       existing.TradingSymbols,
		UseAI500:             existing.UseAI500,
		UseOITop:             existing.UseOITop,
		CustomPrompt:         existing.CustomPrompt,
		OverrideBasePrompt:   existing.OverrideBasePrompt,
		SystemPromptTemplate: existing.SystemPromptTemplate,
	}
	if args.ScanIntervalMinutes != nil && *args.ScanIntervalMinutes > 0 {
		record.ScanIntervalMinutes = *args.ScanIntervalMinutes
		if record.ScanIntervalMinutes < 3 {
			record.ScanIntervalMinutes = 3
		}
	}
	if args.IsCrossMargin != nil {
		record.IsCrossMargin = *args.IsCrossMargin
	}
	if args.ShowInCompetition != nil {
		record.ShowInCompetition = *args.ShowInCompetition
	}
	if err := a.store.Trader().Update(record); err != nil {
		return fmt.Sprintf(`{"error":"failed to update trader: %s"}`, err)
	}
	if a.traderManager != nil {
		a.traderManager.RemoveTrader(record.ID)
		_ = a.traderManager.LoadUserTradersFromStore(a.store, storeUserID)
	}
	result, _ := json.Marshal(map[string]any{
		"status": "ok",
		"action": "update",
		"trader": safeTraderForTool(record, record.IsRunning),
	})
	return string(result)
}

func (a *Agent) toolDeleteTrader(storeUserID, traderID string) string {
	if traderID == "" {
		return `{"error":"trader_id is required for delete"}`
	}
	if a.traderManager != nil {
		if trader, err := a.traderManager.GetTrader(traderID); err == nil {
			if running, ok := trader.GetStatus()["is_running"].(bool); ok && running {
				return `{"error":"trader is running; stop it before deleting"}`
			}
		}
	}
	if record, err := a.store.Trader().GetFullConfig(storeUserID, traderID); err == nil && record != nil && record.Trader != nil && record.Trader.IsRunning {
		return `{"error":"trader is running; stop it before deleting"}`
	}
	if traders, err := a.store.Trader().List(storeUserID); err == nil {
		for _, trader := range traders {
			if trader != nil && trader.ID == traderID && trader.IsRunning {
				return `{"error":"trader is running; stop it before deleting"}`
			}
		}
	}
	if err := a.store.Trader().Delete(storeUserID, traderID); err != nil {
		return fmt.Sprintf(`{"error":"failed to delete trader: %s"}`, err)
	}
	if a.traderManager != nil {
		a.traderManager.RemoveTrader(traderID)
	}
	result, _ := json.Marshal(map[string]any{
		"status":    "ok",
		"action":    "delete",
		"trader_id": traderID,
	})
	return string(result)
}

func (a *Agent) toolStartTrader(storeUserID, traderID string) string {
	if traderID == "" {
		return `{"error":"trader_id is required for start"}`
	}
	if a.traderManager == nil {
		return `{"error":"trader manager unavailable"}`
	}
	if _, err := a.store.Trader().GetFullConfig(storeUserID, traderID); err != nil {
		return fmt.Sprintf(`{"error":"trader not found or inaccessible: %s"}`, err)
	}
	if existing, err := a.traderManager.GetTrader(traderID); err == nil {
		if running, ok := existing.GetStatus()["is_running"].(bool); ok && running {
			return `{"error":"trader is already running"}`
		}
		a.traderManager.RemoveTrader(traderID)
	}
	if err := a.traderManager.LoadUserTradersFromStore(a.store, storeUserID); err != nil {
		return fmt.Sprintf(`{"error":"failed to load trader config: %s"}`, err)
	}
	trader, err := a.traderManager.GetTrader(traderID)
	if err != nil {
		if loadErr := a.traderManager.GetLoadError(traderID); loadErr != nil {
			return fmt.Sprintf(`{"error":"failed to load trader: %s"}`, loadErr)
		}
		return fmt.Sprintf(`{"error":"failed to get trader: %s"}`, err)
	}
	safe.GoNamed("agent-trader-start-"+traderID, func() {
		if runErr := trader.Run(); runErr != nil {
			a.logger.Error("agent tool trader runtime error", "trader_id", traderID, "error", runErr)
		}
	})
	_ = a.store.Trader().UpdateStatus(storeUserID, traderID, true)
	result, _ := json.Marshal(map[string]any{
		"status":    "ok",
		"action":    "start",
		"trader_id": traderID,
		"message":   "Trader started",
	})
	return string(result)
}

func (a *Agent) toolStopTrader(storeUserID, traderID string) string {
	if traderID == "" {
		return `{"error":"trader_id is required for stop"}`
	}
	if a.traderManager == nil {
		return `{"error":"trader manager unavailable"}`
	}
	if _, err := a.store.Trader().GetFullConfig(storeUserID, traderID); err != nil {
		return fmt.Sprintf(`{"error":"trader not found or inaccessible: %s"}`, err)
	}
	trader, err := a.traderManager.GetTrader(traderID)
	if err != nil {
		return fmt.Sprintf(`{"error":"trader not loaded: %s"}`, err)
	}
	if running, ok := trader.GetStatus()["is_running"].(bool); ok && !running {
		return `{"error":"trader is already stopped"}`
	}
	trader.Stop()
	_ = a.store.Trader().UpdateStatus(storeUserID, traderID, false)
	result, _ := json.Marshal(map[string]any{
		"status":    "ok",
		"action":    "stop",
		"trader_id": traderID,
		"message":   "Trader stopped",
	})
	return string(result)
}

func (a *Agent) toolGetPreferences(userID int64) string {
	prefs := a.getPersistentPreferences(userID)
	result, _ := json.Marshal(map[string]any{
		"preferences": prefs,
		"count":       len(prefs),
	})
	return string(result)
}

func (a *Agent) toolManagePreferences(userID int64, argsJSON string) string {
	var args struct {
		Action string `json:"action"`
		Text   string `json:"text"`
		Match  string `json:"match"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf(`{"error": "invalid arguments: %s"}`, err)
	}

	switch args.Action {
	case "add":
		prefs, created, err := a.addPersistentPreference(userID, args.Text)
		if err != nil {
			return fmt.Sprintf(`{"error": "%s"}`, err)
		}
		result, _ := json.Marshal(map[string]any{
			"status":      "ok",
			"action":      "add",
			"preference":  created,
			"preferences": prefs,
		})
		return string(result)
	case "update":
		prefs, updated, err := a.updatePersistentPreference(userID, args.Match, args.Text)
		if err != nil {
			return fmt.Sprintf(`{"error": "%s"}`, err)
		}
		result, _ := json.Marshal(map[string]any{
			"status":      "ok",
			"action":      "update",
			"preference":  updated,
			"preferences": prefs,
		})
		return string(result)
	case "delete":
		prefs, removed, err := a.deletePersistentPreference(userID, args.Match)
		if err != nil {
			return fmt.Sprintf(`{"error": "%s"}`, err)
		}
		result, _ := json.Marshal(map[string]any{
			"status":      "ok",
			"action":      "delete",
			"preference":  removed,
			"preferences": prefs,
		})
		return string(result)
	default:
		return `{"error": "invalid action"}`
	}
}

func (a *Agent) toolSearchStock(argsJSON string) string {
	var args struct {
		Keyword string `json:"keyword"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf(`{"error": "invalid arguments: %s"}`, err)
	}

	if args.Keyword == "" {
		return `{"error": "keyword is required"}`
	}

	results, err := searchStock(args.Keyword)
	if err != nil {
		return fmt.Sprintf(`{"error": "search failed: %s"}`, err)
	}

	if len(results) == 0 {
		return fmt.Sprintf(`{"results": [], "message": "no stocks found for '%s'"}`, args.Keyword)
	}

	// Limit to top 10 results
	if len(results) > 10 {
		results = results[:10]
	}

	// Also fetch real-time quotes for the top results (up to 3)
	type enrichedResult struct {
		Name   string      `json:"name"`
		Code   string      `json:"code"`
		Market string      `json:"market"`
		Quote  *StockQuote `json:"quote,omitempty"`
	}

	var enriched []enrichedResult
	for i, r := range results {
		er := enrichedResult{Name: r.Name, Code: r.Code, Market: r.Market}
		if i < 3 {
			q, qErr := fetchStockQuote(r.Code)
			if qErr == nil && q.Price > 0 {
				er.Quote = q
			}
		}
		enriched = append(enriched, er)
	}

	result, _ := json.Marshal(map[string]any{
		"keyword": args.Keyword,
		"count":   len(enriched),
		"results": enriched,
	})
	return string(result)
}

func (a *Agent) toolExecuteTrade(ctx context.Context, userID int64, lang, argsJSON string) string {
	policy := sessionPolicyFromContext(ctx)
	if !policy.Authenticated {
		return `{"error": "trade execution requires an authenticated session"}`
	}
	if !policy.CanExecuteTrade || a == nil || a.config == nil || !a.config.AllowTradeExecution {
		return `{"error": "trade execution is blocked by server policy for this session"}`
	}

	var args struct {
		Action   string  `json:"action"`
		Symbol   string  `json:"symbol"`
		Quantity float64 `json:"quantity"`
		Leverage int     `json:"leverage"`
		// W1b E9 — the entry's own bracket: required for an open (refused
		// below at proposal; the admission chain refuses it again at send).
		StopLoss   float64 `json:"stop_loss"`
		TakeProfit float64 `json:"take_profit"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf(`{"error": "invalid arguments: %s"}`, err)
	}

	// Normalize symbol (the one chat-symbol canonicalizer, W1b FOLD-5)
	sym := chatTradeSymbol(args.Symbol)

	// Validate action
	validActions := map[string]bool{
		"open_long": true, "open_short": true,
		"close_long": true, "close_short": true,
	}
	if !validActions[args.Action] {
		return fmt.Sprintf(`{"error": "invalid action: %s"}`, args.Action)
	}

	// For open actions, quantity must be > 0
	if (args.Action == "open_long" || args.Action == "open_short") && args.Quantity <= 0 {
		return `{"error": "quantity must be > 0 for opening positions"}`
	}
	// W1b E9 repair — an open needs its OWN stop and target at PROPOSAL time:
	// the admission chain refuses one without them, so a pending trade that
	// lacks either would only ask the owner to confirm a certain refusal.
	if (args.Action == "open_long" || args.Action == "open_short") && !(args.StopLoss > 0 && args.TakeProfit > 0) {
		return `{"error": "stop_loss and take_profit are both required for open_long/open_short (absolute prices): a chat entry is sent with its own bracket or not at all"}`
	}

	// For stock symbols, check market hours and warn if closed
	var marketWarning string
	if isStockSymbol(sym) && a.traderManager != nil {
		for _, t := range a.traderManager.GetAllTraders() {
			if t.GetExchange() == "alpaca" {
				ut := t.GetUnderlyingTrader()
				if ut == nil {
					continue
				}
				type marketChecker interface {
					IsMarketOpen() (bool, string, error)
				}
				if mc, ok := ut.(marketChecker); ok {
					isOpen, status, err := mc.IsMarketOpen()
					if err == nil && !isOpen {
						marketWarning = fmt.Sprintf("⚠️ US market is currently %s. Order will be queued for next market open.", status)
					}
				}
				break
			}
		}
	}

	// Create pending trade — requires user confirmation
	trade := &TradeAction{
		ID:         fmt.Sprintf("trade_%d", time.Now().UnixNano()),
		Action:     args.Action,
		Symbol:     sym,
		Quantity:   args.Quantity,
		Leverage:   args.Leverage,
		StopLoss:   args.StopLoss,
		TakeProfit: args.TakeProfit,
		Status:     "pending_confirmation",
		CreatedAt:  time.Now().Unix(),
	}
	if _, selectedTrader, underlyingTrader, err := a.resolveTradeExecutionContext(trade); err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	} else if err := validateTradeAction(trade, isStockSymbol(sym), selectedTrader, underlyingTrader); err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}

	a.pending.Add(trade)
	a.pending.CleanExpired()

	confirmMessage := fmt.Sprintf("Trade created. User must confirm with: 确认 %s (or: confirm %s)", trade.ID, trade.ID)
	if trade.RequiresLargeOrderConfirmation {
		confirmMessage = fmt.Sprintf("Trade created but flagged as high-risk. User must confirm with: 确认大额 %s (or: confirm large %s)", trade.ID, trade.ID)
	}

	// Return confirmation info to LLM so it can present it to the user
	resultMap := map[string]any{
		"status":                            "pending_confirmation",
		"trade_id":                          trade.ID,
		"action":                            trade.Action,
		"symbol":                            trade.Symbol,
		"quantity":                          trade.Quantity,
		"leverage":                          trade.Leverage,
		"stop_loss":                         trade.StopLoss,
		"take_profit":                       trade.TakeProfit,
		"estimated_price":                   trade.EstimatedPrice,
		"estimated_notional":                trade.EstimatedNotional,
		"requires_large_order_confirmation": trade.RequiresLargeOrderConfirmation,
		"message":                           confirmMessage,
		"expires":                           "5 minutes",
	}
	if marketWarning != "" {
		resultMap["market_warning"] = marketWarning
	}
	result, _ := json.Marshal(resultMap)
	return string(result)
}

func (a *Agent) toolGetPositions(storeUserID string) string {
	if a.traderManager == nil {
		return `{"error": "no trader manager configured"}`
	}
	if a.store == nil {
		return `{"error": "store unavailable"}`
	}
	traderConfigs, err := a.store.Trader().List(storeUserID)
	if err != nil {
		return fmt.Sprintf(`{"error": "failed to list traders: %s"}`, err)
	}

	var positions []map[string]any
	for _, traderCfg := range traderConfigs {
		if strings.TrimSpace(traderCfg.ID) == "" {
			continue
		}
		t, err := a.traderManager.GetTrader(traderCfg.ID)
		if err != nil {
			continue
		}
		pos, err := t.GetPositions()
		if err != nil {
			continue
		}
		for _, p := range pos {
			size := toFloat(p["size"])
			if size == 0 {
				continue
			}
			tid := traderCfg.ID
			if len(tid) > 8 {
				tid = tid[:8]
			}
			positions = append(positions, map[string]any{
				"trader":         tid,
				"exchange":       t.GetExchange(),
				"symbol":         p["symbol"],
				"side":           p["side"],
				"size":           size,
				"entry_price":    toFloat(p["entryPrice"]),
				"mark_price":     toFloat(p["markPrice"]),
				"unrealized_pnl": toFloat(p["unrealizedPnl"]),
				"leverage":       p["leverage"],
			})
		}
	}

	if len(positions) == 0 {
		return `{"positions": [], "message": "no open positions"}`
	}

	result, _ := json.Marshal(map[string]any{"positions": positions})
	return string(result)
}

func (a *Agent) toolGetBalance(storeUserID string) string {
	if a.traderManager == nil {
		return `{"error": "no trader manager configured"}`
	}
	if a.store == nil {
		return `{"error": "store unavailable"}`
	}
	traderConfigs, err := a.store.Trader().List(storeUserID)
	if err != nil {
		return fmt.Sprintf(`{"error": "failed to list traders: %s"}`, err)
	}

	var balances []map[string]any
	for _, traderCfg := range traderConfigs {
		if strings.TrimSpace(traderCfg.ID) == "" {
			continue
		}
		t, err := a.traderManager.GetTrader(traderCfg.ID)
		if err != nil {
			continue
		}
		info, err := t.GetAccountInfo()
		if err != nil {
			continue
		}
		tid := traderCfg.ID
		if len(tid) > 8 {
			tid = tid[:8]
		}
		balances = append(balances, map[string]any{
			"trader":       tid,
			"name":         t.GetName(),
			"exchange":     t.GetExchange(),
			"total_equity": toFloat(info["total_equity"]),
			"available":    toFloat(info["available_balance"]),
			"used_margin":  toFloat(info["used_margin"]),
		})
	}

	result, _ := json.Marshal(map[string]any{"balances": balances})
	return string(result)
}

func (a *Agent) toolGetMarketPrice(argsJSON string) string {
	var args struct {
		Symbol string `json:"symbol"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf(`{"error": "invalid arguments: %s"}`, err)
	}

	sym := chatTradeSymbol(args.Symbol)

	if a.traderManager == nil {
		return `{"error": "no trader manager configured"}`
	}

	wantStock := isStockSymbol(sym)
	wantCME := isCMEFuturesChatSymbol(sym) // W1b FOLD-5: a CME price is the NT8 trader's
	for _, t := range a.traderManager.GetAllTraders() {
		underlying := t.GetUnderlyingTrader()
		if underlying == nil {
			continue
		}
		// Route to correct exchange type (stock vs crypto)
		isAlpaca := t.GetExchange() == "alpaca"
		if wantStock && !isAlpaca {
			continue
		}
		if !wantStock && isAlpaca {
			continue
		}
		if wantCME && t.GetExchange() != "ninjatrader" {
			continue
		}
		price, err := underlying.GetMarketPrice(sym)
		if err == nil && price > 0 {
			priceResult := map[string]any{
				"symbol": sym,
				"price":  price,
			}
			// For stocks, include market status
			if wantStock && isAlpaca {
				type marketChecker interface {
					IsMarketOpen() (bool, string, error)
				}
				if mc, ok := underlying.(marketChecker); ok {
					isOpen, status, mErr := mc.IsMarketOpen()
					if mErr == nil {
						priceResult["market_open"] = isOpen
						priceResult["market_status"] = status
					}
				}
			}
			result, _ := json.Marshal(priceResult)
			return string(result)
		}
	}

	return fmt.Sprintf(`{"error": "could not get price for %s"}`, sym)
}

func strategyLockedFieldError(lang, field string) string {
	switch strings.TrimSpace(field) {
	case "max_positions":
		if lang == "zh" {
			return "最大持仓数是 System enforced 字段，策略编辑页不提供普通输入控件，Agent 不能修改。"
		}
		return "Max positions is System enforced in the strategy editor and cannot be changed by the agent."
	case "max_margin_usage":
		if lang == "zh" {
			return "最大保证金使用率是 System enforced 字段，策略编辑页不提供普通输入控件，Agent 不能修改。"
		}
		return "Max margin usage is System enforced in the strategy editor and cannot be changed by the agent."
	case "min_position_size":
		if lang == "zh" {
			return "最小开仓金额是系统固定值 12 USD，手动面板里也是 System enforced，Agent 不能修改。"
		}
		return "The minimum position size is a fixed system value of 12 USD. It is System enforced in the manual panel and cannot be changed by the agent."
	default:
		if lang == "zh" {
			return "这个字段是系统固定项，Agent 不能修改。"
		}
		return "This field is system enforced and cannot be changed by the agent."
	}
}

func strategyConfigContainsLockedField(config map[string]any) (string, bool) {
	if len(config) == 0 {
		return "", false
	}
	if _, ok := config["min_position_size"]; ok {
		return "min_position_size", true
	}
	if risk, ok := config["risk_control"].(map[string]any); ok {
		for _, field := range []string{"max_positions", "max_margin_usage", "min_position_size"} {
			if _, ok := risk[field]; ok {
				return field, true
			}
		}
	}
	if aiConfig, ok := config["ai_config"].(map[string]any); ok {
		if risk, ok := aiConfig["risk_control"].(map[string]any); ok {
			for _, field := range []string{"max_positions", "max_margin_usage", "min_position_size"} {
				if _, ok := risk[field]; ok {
					return field, true
				}
			}
		}
	}
	return "", false
}

func (a *Agent) toolGetTradeHistory(argsJSON string) string {
	if a.store == nil {
		return `{"error": "store not available"}`
	}

	var args struct {
		Limit int `json:"limit"`
	}
	if argsJSON != "" {
		_ = json.Unmarshal([]byte(argsJSON), &args)
	}
	if args.Limit <= 0 {
		args.Limit = 10
	}
	if args.Limit > 50 {
		args.Limit = 50
	}

	if a.traderManager == nil {
		return `{"error": "no trader manager configured"}`
	}

	// P&L-TRUTH WAVE: gather the sources, then build the payload through the
	// pure, fixture-tested builder (buildTradeHistory).
	var sources []tradeHistorySource
	for id, t := range a.traderManager.GetAllTraders() {
		positions, err := a.store.Position().GetClosedPositions(id, args.Limit)
		if err != nil {
			continue
		}
		tid := id
		if len(tid) > 8 {
			tid = tid[:8]
		}
		sources = append(sources, tradeHistorySource{Name: t.GetName(), TID: tid, Positions: positions})
	}
	payload := buildTradeHistory(sources, args.Limit)
	if payload == nil {
		return `{"trades": [], "message": "no closed trades found"}`
	}
	result, _ := json.Marshal(payload)
	return string(result)
}

// tradeHistorySource is one trader's closed rows for the trade-history tool.
type tradeHistorySource struct {
	Name      string
	TID       string
	Positions []*store.TraderPosition
}

// buildTradeHistory (P&L-TRUTH WAVE, 2026-09-01, corrected-column law) is the
// AgentBeta trade tool's payload: every figure is computed over RESOLVED rows
// (pnl_corrected present); an UNRESOLVED row is listed with pnl=null and
// resolved=false and excluded from every summary figure; test-seam rows are
// quarantined. The summary carries figure + resolved n + unresolved count —
// the strict shape. Returns nil when there are no rows at all.
func buildTradeHistory(sources []tradeHistorySource, limit int) map[string]any {
	var trades []map[string]any
	var totalPnL float64
	var wins, losses, unresolved int
	for _, src := range sources {
		for _, pos := range src.Positions {
			if pos.CloseReason == store.CloseReasonTestSeam {
				continue
			}
			pnl, resolved := pos.CorrectedPnL()
			var pnlField any
			if resolved {
				pnlField = pnl
				totalPnL += pnl
				if pnl >= 0 {
					wins++
				} else {
					losses++
				}
			} else {
				unresolved++
			}
			entryTime := ""
			if pos.EntryTime > 0 {
				entryTime = kernel.FormatCT(time.Unix(pos.EntryTime/1000, 0))
			}
			exitTime := ""
			if pos.ExitTime > 0 {
				exitTime = kernel.FormatCT(time.Unix(pos.ExitTime/1000, 0))
			}
			trades = append(trades, map[string]any{
				"id":          pos.ID,
				"trader":      src.Name,
				"trader_id":   src.TID,
				"symbol":      pos.Symbol,
				"side":        pos.Side,
				"entry_price": pos.EntryPrice,
				"exit_price":  pos.ExitPrice,
				"quantity":    pos.Quantity,
				"leverage":    pos.Leverage,
				"pnl":         pnlField, // corrected value, or null when unresolved
				"resolved":    resolved,
				"entry_time":  entryTime,
				"exit_time":   exitTime,
			})
		}
	}
	if len(trades) == 0 {
		return nil
	}
	// Most recent first, consistent across traders.
	sort.Slice(trades, func(i, j int) bool {
		ti, _ := trades[i]["exit_time"].(string)
		tj, _ := trades[j]["exit_time"].(string)
		return ti > tj
	})
	if limit > 0 && len(trades) > limit {
		trades = trades[:limit]
	}
	winRate := 0.0
	total := wins + losses
	if total > 0 {
		winRate = float64(wins) / float64(total) * 100
	}
	return map[string]any{
		"trades": trades,
		"summary": map[string]any{
			// the strict shape — total_trades == resolved_trades (compat)
			"total_trades":        total,
			"resolved_trades":     total,
			"unresolved_excluded": unresolved,
			"wins":                wins,
			"losses":              losses,
			"win_rate":            fmt.Sprintf("%.1f%%", winRate),
			"total_pnl":           totalPnL,
			"pnl_column":          "pnl_corrected (strict; unresolved rows excluded, never coerced)",
		},
	}
}

func (a *Agent) toolGetCandidateCoins(storeUserID string, userID int64, argsJSON string) string {
	if a.store == nil {
		return `{"error":"store unavailable"}`
	}

	var args struct {
		TraderID   string `json:"trader_id"`
		StrategyID string `json:"strategy_id"`
	}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf(`{"error":"invalid arguments: %s"}`, err)
		}
	}

	traderID := strings.TrimSpace(args.TraderID)
	strategyID := strings.TrimSpace(args.StrategyID)
	state := a.getExecutionState(userID)
	if traderID == "" && state.CurrentReferences != nil && state.CurrentReferences.Trader != nil {
		traderID = strings.TrimSpace(state.CurrentReferences.Trader.ID)
	}
	if strategyID == "" && state.CurrentReferences != nil && state.CurrentReferences.Strategy != nil {
		strategyID = strings.TrimSpace(state.CurrentReferences.Strategy.ID)
	}

	if traderID != "" {
		return a.toolGetCandidateCoinsForTrader(storeUserID, traderID)
	}
	if strategyID != "" {
		return a.toolGetCandidateCoinsForStrategy(storeUserID, strategyID)
	}
	return `{"error":"trader_id or strategy_id is required"}`
}

func (a *Agent) toolGetCandidateCoinsForTrader(storeUserID, traderID string) string {
	if a.traderManager == nil {
		return `{"error":"no trader manager configured"}`
	}
	record, err := a.store.Trader().GetFullConfig(storeUserID, traderID)
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to load trader: %s"}`, err)
	}
	memTrader, err := a.traderManager.GetTrader(traderID)
	if err != nil {
		return fmt.Sprintf(`{"error":"trader is not loaded in memory: %s"}`, err)
	}

	coins, coinErr := memTrader.GetCandidateCoins()
	cfg := memTrader.GetStrategyConfig()
	status := memTrader.GetStatus()
	isRunning, _ := status["is_running"].(bool)
	payload := map[string]any{
		"trader":            safeTraderForTool(record.Trader, isRunning),
		"coin_source":       candidateCoinSourceSummary(cfg),
		"candidate_count":   len(coins),
		"candidate_symbols": candidateCoinSymbols(coins),
		"candidates":        candidateCoinDetails(coins),
	}
	if coinErr != nil {
		payload["error"] = coinErr.Error()
	}
	result, _ := json.Marshal(payload)
	return string(result)
}

func (a *Agent) toolGetCandidateCoinsForStrategy(storeUserID, strategyID string) string {
	record, err := a.store.Strategy().Get(storeUserID, strategyID)
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to load strategy: %s"}`, err)
	}
	cfg, err := record.ParseConfig()
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to parse strategy config: %s"}`, err)
	}

	engine := kernel.NewStrategyEngine(cfg)
	coins, coinErr := engine.GetCandidateCoins()
	payload := map[string]any{
		"strategy":          safeStrategyForTool(record),
		"coin_source":       candidateCoinSourceSummary(cfg),
		"candidate_count":   len(coins),
		"candidate_symbols": candidateCoinSymbols(coins),
		"candidates":        candidateCoinDetails(coins),
	}
	if coinErr != nil {
		payload["error"] = coinErr.Error()
	}
	result, _ := json.Marshal(payload)
	return string(result)
}

func candidateCoinSourceSummary(cfg *store.StrategyConfig) map[string]any {
	if cfg == nil {
		return nil
	}
	return map[string]any{
		"source_type":    cfg.CoinSource.SourceType,
		"static_coins":   cfg.CoinSource.StaticCoins,
		"excluded_coins": cfg.CoinSource.ExcludedCoins,
	}
}

func candidateCoinSymbols(coins []kernel.CandidateCoin) []string {
	out := make([]string, 0, len(coins))
	for _, coin := range coins {
		out = append(out, coin.Symbol)
	}
	return out
}

func candidateCoinDetails(coins []kernel.CandidateCoin) []map[string]any {
	out := make([]map[string]any, 0, len(coins))
	for _, coin := range coins {
		out = append(out, map[string]any{
			"symbol":  coin.Symbol,
			"sources": coin.Sources,
		})
	}
	return out
}

func isCMEFuturesChatSymbol(sym string) bool {
	return market.IsCMEFuturesSymbol(sym) || market.FuturesRoot(sym) != ""
}

// chatTradeSymbol is the ONE canonicalizer for a symbol a chat trade names
// (W1b FOLD-5, canon 28), called where it enters: a CME futures symbol is its
// ROOT ("MNQU6" → "MNQ" — the NT8 trader trades its own resolved front month,
// and the pending trade the owner confirms shows the root); stock tickers
// (AAPL, TSLA) stay as-is.
func chatTradeSymbol(raw string) string {
	if isCMEFuturesChatSymbol(raw) {
		if root := market.FuturesRoot(raw); root != "" {
			return root
		}
		return strings.ToUpper(strings.TrimSpace(raw))
	}
	return strings.ToUpper(raw)
}

// isStockSymbol heuristically determines if a symbol is a stock ticker.
// Stock tickers are 1-5 uppercase letters.
func isStockSymbol(sym string) bool {
	// W1b FOLD-5 — a CME futures symbol is NEVER a stock, checked BEFORE the
	// letters heuristic: "MNQ" is three uppercase letters, and as a "stock"
	// every chat MNQ entry was routed to Alpaca and never reached the NT8
	// trader's door ("no running stock trader (Alpaca) found").
	if isCMEFuturesChatSymbol(sym) {
		return false
	}
	sym = strings.ToUpper(sym)
	// Pure uppercase letters, 1-5 chars = likely a stock ticker
	if len(sym) >= 1 && len(sym) <= 5 {
		allLetters := true
		for _, c := range sym {
			if c < 'A' || c > 'Z' {
				allLetters = false
				break
			}
		}
		if allLetters {
			return true
		}
	}
	return false
}
