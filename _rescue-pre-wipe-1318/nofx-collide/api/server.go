package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"nofx/auth"
	"nofx/crypto"
	"nofx/kernel"
	"nofx/logger"
	"nofx/manager"
	"nofx/store"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server HTTP API server
type Server struct {
	router                    *gin.Engine
	traderManager             *manager.TraderManager
	store                     *store.Store
	cryptoHandler             *CryptoHandler
	exchangeAccountStateCache *ExchangeAccountStateCache
	httpServer                *http.Server
	host                      string // bind interface; "" → 127.0.0.1 (loopback-only default)
	port                      int
	telegramReloadCh          chan<- struct{} // signal Telegram bot to reload
}

// NewServer Creates API server. host is the bind interface — pass
// cfg.APIServerHost; an empty value falls back to the loopback-only default.
func NewServer(traderManager *manager.TraderManager, st *store.Store, cryptoService *crypto.CryptoService, host string, port int) *Server {
	// Set to Release mode (reduce log output)
	gin.SetMode(gin.ReleaseMode)

	router := gin.Default()

	// Enable CORS
	router.Use(corsMiddleware())

	// Create crypto handler
	cryptoHandler := NewCryptoHandler(cryptoService)

	s := &Server{
		router:                    router,
		traderManager:             traderManager,
		store:                     st,
		cryptoHandler:             cryptoHandler,
		exchangeAccountStateCache: NewExchangeAccountStateCache(),
		host:                      host,
		port:                      port,
	}

	// Setup routes
	s.setupRoutes()

	return s
}

// corsMiddleware CORS middleware
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	}
}

// setupRoutes Setup routes
func (s *Server) setupRoutes() {
	// Plan 4 Task 25 — Prometheus metrics endpoint (mounted on root, unauth).
	// Placed BEFORE the /api group so the convergence stage can verify
	// ordering vs T23's risk + audit endpoints (T23 lives inside /api).
	s.router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// API route group
	api := s.router.Group("/api")
	{
		// Health check
		api.Any("/health", s.handleHealth)

		// Admin login (used in admin mode, public)

		// System supported models and exchanges (no authentication required)
		s.route(api, "GET", "/supported-models", "List supported AI model providers", s.handleGetSupportedModels)
		s.route(api, "GET", "/supported-exchanges", "List supported exchange types", s.handleGetSupportedExchanges)

		// System config (no authentication required, for frontend to determine admin mode/registration status)
		s.route(api, "GET", "/config", "Get system configuration", s.handleGetSystemConfig)

		// Wallet validation (no authentication required — used by frontend config form)
		api.POST("/wallet/validate", s.handleWalletValidate)
		api.POST("/wallet/generate", s.handleWalletGenerate)

		// Crypto related endpoints (no authentication required, not exposed to bot)
		api.GET("/crypto/config", s.cryptoHandler.HandleGetCryptoConfig)
		api.GET("/crypto/public-key", s.cryptoHandler.HandleGetPublicKey)
		// NOTE: POST /crypto/decrypt was here (public). It is a decryption oracle
		// for the server's RSA key — moved into the protected group below (P0 S4).

		// Public competition data (no authentication required)
		s.route(api, "GET", "/traders", "Public trader list", s.handlePublicTraderList)
		s.route(api, "GET", "/competition", "Public competition data", s.handlePublicCompetition)
		s.route(api, "GET", "/top-traders", "Top traders leaderboard", s.handleTopTraders)
		s.route(api, "GET", "/equity-history", "Equity history for a trader", s.handleEquityHistory)
		s.route(api, "POST", "/equity-history-batch", "Batch equity history for multiple traders", s.handleEquityHistoryBatch)
		s.route(api, "GET", "/traders/:id/public-config", "Public trader configuration", s.handleGetPublicTraderConfig)

		// Market data (no authentication required)
		s.route(api, "GET", "/symbols", "Available trading symbols", s.handleSymbols)

		// Live NT8 bar stream over SSE (Plan 4.4 Stage 4). Self-authed via a
		// short-lived ?ticket= (minted below) because EventSource cannot set an
		// Authorization header and a JWT in the URL leaks into logs.
		s.route(api, "GET", "/v1/bars/stream", "SSE live NT8 bars (?trader_id=&symbol=&tf=&ticket=)", s.handleBarsStream)

		// Public strategy market (no authentication required)
		s.route(api, "GET", "/strategies/public", "Public strategy market", s.handlePublicStrategies)
		s.route(api, "POST", "/strategies/estimate-tokens", "Estimate token usage for a strategy config", s.handleEstimateTokens)

		// Authentication related routes (no authentication required)
		s.route(api, "POST", "/register", "Register new user (first-time setup only)", s.handleRegister)
		s.route(api, "POST", "/login", "User login, returns JWT token", s.handleLogin)
		// SECURITY (P0 S2): /reset-password and /reset-account used to live HERE,
		// unauthenticated. reset-password reset ANY account from an email alone;
		// reset-account deleted every user, and a follow-up /register then adopted
		// the orphaned credential rows. Together: unauthenticated account takeover.
		// reset-password is now permanently disabled (no mail/token path exists to
		// make it safe); reset-account moved into the protected group below and is
		// additionally env-gated + confirm-token gated.
		s.route(api, "POST", "/reset-password", "DISABLED — always 410 (no verification path)", s.handleResetPasswordDisabled)

		// Routes requiring authentication
		protected := api.Group("/", s.authMiddleware(), s.planTraderOwnership())
		{

			// Settings integrity (D5): the knob registry — what this build
			// actually honours. Protected: it enumerates strategy knobs and
			// their consumers. Classification only, never values (A25).
			s.route(protected, "GET", "/config/resolved", "Resolved knob registry — status and label per knob", s.handleConfigResolved)
			// Market data — JWT-protected (C4, 2026-08-25): candle history +
			// SVP moved out of the public group; the FE httpClient always
			// attaches the Bearer token, and the agent calls handlers in-process.
			s.route(protected, "GET", "/klines", "Candlestick data (?symbol=&interval=&limit=)", s.handleKlines)
			s.route(protected, "GET", "/klines/svp", "Session Volume Profile (?symbol=&exchange=ninjatrader) — server-computed POC/VAH/VAL + histogram bins", s.handleKlinesSVP)
			// Logout (add to blacklist)
			s.route(protected, "POST", "/logout", "Logout (blacklist token)", s.handleLogout)
			// Mint a short-lived single-use SSE ticket for the live bar stream (Stage 4).
			s.route(protected, "POST", "/v1/bars/stream-ticket", "Mint a short-lived SSE stream ticket", s.handleBarsStreamTicket)
			s.route(protected, "POST", "/onboarding/beginner", "Prepare beginner claw402 wallet and default model", s.handleBeginnerOnboarding)
			s.route(protected, "GET", "/onboarding/beginner/current", "Get current beginner claw402 wallet", s.handleCurrentBeginnerWallet)
			s.route(protected, "GET", "/agent/preferences", "Get persistent agent preferences", s.handleGetAgentPreferences)
			s.route(protected, "POST", "/agent/preferences", "Create persistent agent preference", s.handleCreateAgentPreference)
			s.route(protected, "DELETE", "/agent/preferences/:id", "Delete persistent agent preference", s.handleDeleteAgentPreference)

			// User account management
			s.routeWithSchema(protected, "PUT", "/user/password", "Change current user password",
				`Body: {"new_password":"<string, min 8 chars>"}`,
				s.handleChangePassword)

			// SECURITY (P0 S4): RSA decryption oracle — JWT + only when transport
			// encryption is actually enabled (the handler re-checks).
			protected.POST("/crypto/decrypt", s.cryptoHandler.HandleDecryptSensitiveData)

			// SECURITY (P0 S2): destructive account reset — JWT + env flag +
			// confirm token. Deletes every user/trader/strategy, so it is OFF
			// unless ALLOW_ACCOUNT_RESET=1 is set in the environment.
			s.routeWithSchema(protected, "POST", "/reset-account",
				"DESTRUCTIVE — delete all users/traders/strategies (env-gated, confirm-token required)",
				`Body: {"confirm":"RESET-ALL-DATA"}. Requires ALLOW_ACCOUNT_RESET=1 in the server environment.`,
				s.handleResetAccount)

			// Server IP query (requires authentication, for whitelist configuration)
			s.route(protected, "GET", "/server-ip", "Get server public IP (for exchange whitelist)", s.handleGetServerIP)

			// AI trader management
			s.routeWithSchema(protected, "GET", "/my-traders", "List user's traders with status",
				`Returns: [{"trader_id":"<EXACT id — use this as trader_id in all ?trader_id= queries and POST /traders/:id/start|stop>","trader_name":"<string>","is_running":<bool>}]
NOTE: The id field is "trader_id" (NOT "id"). Always read trader_id from this endpoint before querying data.`,
				s.handleTraderList)
			s.routeWithSchema(protected, "GET", "/traders/:id/config", "Get full trader configuration",
				`:id = trader_id from GET /api/my-traders`,
				s.handleGetTraderConfig)
			s.routeWithSchema(protected, "POST", "/traders", "Create a new AI trader",
				`Body: {"name":"<string, required>","ai_model_id":"<EXACT id field from GET /api/models — e.g. 'abc123_deepseek', NOT the provider name 'deepseek'>","exchange_id":"<EXACT id field from GET /api/exchanges — e.g. '05785d3b-841e-...', NOT the type name>","strategy_id":"<EXACT id field from GET /api/strategies>","scan_interval_minutes":<int, default 3, minimum 3>}
IMPORTANT: ai_model_id and exchange_id must be the full "id" value from the Account State, not the provider/type name.`,
				s.handleCreateTrader)
			s.routeWithSchema(protected, "PUT", "/traders/:id", "Update trader configuration",
				`:id = trader_id from GET /api/my-traders
Body: {"name":"<string>","ai_model_id":"<EXACT id from GET /api/models>","exchange_id":"<EXACT id from GET /api/exchanges>","strategy_id":"<EXACT id from GET /api/strategies>","scan_interval_minutes":<int, min 3>,"is_cross_margin":<bool>}
Only include fields you want to change.`,
				s.handleUpdateTrader)
			s.routeWithSchema(protected, "DELETE", "/traders/:id", "Delete trader",
				`:id = trader_id from GET /api/my-traders. Stops and permanently removes the trader and all its data.`,
				s.handleDeleteTrader)
			s.routeWithSchema(protected, "POST", "/traders/:id/start", "Start trader — begins live trading",
				`:id = trader_id from GET /api/my-traders. No request body needed. The trader must have a valid exchange and AI model configured.`,
				s.handleStartTrader)
			s.routeWithSchema(protected, "POST", "/traders/:id/stop", "Stop trader — halts live trading",
				`:id = trader_id from GET /api/my-traders. No request body needed. Gracefully stops the trading loop.`,
				s.handleStopTrader)
			s.routeWithSchema(protected, "POST", "/traders/:id/pause", "Pause NEW entries (stop_until) — position management continues",
				`:id = trader_id. Body: exactly one of {"minutes":30} | {"until_ct":"HH:MM"} (CT, wraps to tomorrow if past) | {"until":"session_end"}. Blocks NEW entries only; stops/targets/EOD-flat/closes continue. Survives restart; auto-resumes on expiry.`,
				s.handlePauseTrader)
			s.routeWithSchema(protected, "POST", "/traders/:id/resume", "Clear the stop_until pause — entries resume immediately",
				`:id = trader_id. No request body needed.`,
				s.handleResumeTrader)
			s.routeWithSchema(protected, "PUT", "/traders/:id/prompt", "Override the trader's AI system prompt",
				`Body: {"prompt":"<string — the full custom prompt text>"}`,
				s.handleUpdateTraderPrompt)
			s.routeWithSchema(protected, "POST", "/traders/:id/sync-balance", "Sync account balance from exchange",
				`:id = trader_id from GET /api/my-traders. No request body needed. Refreshes initial_balance from the exchange.`,
				s.handleSyncBalance)
			s.routeWithSchema(protected, "POST", "/traders/:id/close-position", "Force-close an open position",
				`:id = trader_id from GET /api/my-traders.
Body: {"symbol":"<string, e.g. BTCUSDT — must match an open position symbol from GET /api/positions>"}`,
				s.handleClosePosition)
			s.routeWithSchema(protected, "PUT", "/traders/:id/competition", "Toggle competition leaderboard visibility",
				`:id = trader_id from GET /api/my-traders.
Body: {"show_in_competition":<bool>}`,
				s.handleToggleCompetition)
			s.routeWithSchema(protected, "GET", "/traders/:id/grid-risk", "Get grid trading risk info",
				`:id = trader_id from GET /api/my-traders.`,
				s.handleGetGridRiskInfo)

			// AI cost tracking
			s.route(protected, "GET", "/ai-costs", "Get AI call costs for a trader (?trader_id=xxx&period=today)", s.handleGetAICosts)
			s.route(protected, "GET", "/ai-costs/summary", "Get AI cost summary (?period=today)", s.handleGetAICostsSummary)

			// AI model configuration
			s.routeWithSchema(protected, "GET", "/models", "List AI model configs",
				`Returns: [{"id":"<EXACT id — use this as ai_model_id when creating/updating a trader>","name":"<display name>","provider":"<short provider name — NOT a valid id>","enabled":<bool>}]
CRITICAL: The "id" field (e.g. "abc123_deepseek") is what you must use for ai_model_id. The "provider" field ("deepseek") is NOT valid as an id.`,
				s.handleGetModelConfigs)
			s.routeWithSchema(protected, "PUT", "/models", "Configure an AI model provider",
				`Body: {"models":{"<model_id>":{"enabled":<bool>,"api_key":"<string>","custom_api_url":"<string, leave empty to use provider default>","custom_model_name":"<string, leave empty to use provider default>"}}}
model_id values: "openai","deepseek","qwen","kimi","grok","gemini","claude"
Defaults when custom fields empty: openai→api.openai.com/v1, deepseek→api.deepseek.com, qwen→dashscope.aliyuncs.com/compatible-mode/v1, kimi→api.moonshot.ai/v1, grok→api.x.ai/v1, gemini→generativelanguage.googleapis.com/v1beta/openai, claude→api.anthropic.com/v1`,
				s.handleUpdateModelConfigs)
			s.routeWithSchema(protected, "POST", "/models/entry", "Create an additional AI model entry for a provider",
				`Body: {"provider":"<e.g. deepseek>","name":"<display label, e.g. DeepSeek-backup>","api_key":"<string>","custom_api_url":"<string, optional>","custom_model_name":"<string, optional>","enabled":<bool, default true>}
Creates a NEW independently-addressable row (unique id) so a provider can hold multiple keys/entries. Returns: {"id":"<new EXACT id — use as ai_model_id>"}`,
				s.handleCreateModelEntry)
			s.routeWithSchema(protected, "DELETE", "/models/:id", "Delete an AI model entry",
				`Deletes the model row with the given EXACT id. Refused if a trader is still bound to it.`,
				s.handleDeleteModelEntry)

			// Exchange configuration
			s.routeWithSchema(protected, "GET", "/exchanges", "List exchange accounts",
				`Returns: [{"id":"<EXACT id — use this as exchange_id when creating/updating a trader>","exchange_type":"<e.g. okx, binance>","account_name":"<user label>","enabled":<bool>}]
CRITICAL: Always use the "id" field for exchange_id. Do not use "exchange_type" as an id.`,
				s.handleGetExchangeConfigs)
			s.routeWithSchema(protected, "GET", "/exchanges/account-state", "Get connection and balance state for each exchange account",
				`Returns: {"states":{"<exchange_id>":{"status":"ok|disabled|missing_credentials|invalid_credentials|permission_denied|unavailable","display_balance":"<string>","total_equity":<number>,"available_balance":<number>,"asset":"USDT|USDC","checked_at":"<RFC3339>","error_code":"<string>","error_message":"<string>"}}}
Use this endpoint to show balance and health in the exchange list without depending on traders.`,
				s.handleGetExchangeAccountStates)
			s.routeWithSchema(protected, "POST", "/exchanges", "Create a new exchange account",
				`Body: {"exchange_type":"<string>","account_name":"<string, user label>","enabled":true,"api_key":"<string>","secret_key":"<string>","passphrase":"<string, required for okx/gate/kucoin>"}
exchange_type values: "binance","bybit","okx","bitget","gate","kucoin","indodax" (CEX) | "hyperliquid","aster","lighter" (DEX)
Required fields by exchange:
  binance/bybit/bitget/indodax: api_key + secret_key
  okx/gate/kucoin: api_key + secret_key + passphrase
  hyperliquid: hyperliquid_wallet_addr
  aster: aster_user + aster_signer + aster_private_key
  lighter: lighter_wallet_addr + lighter_private_key + lighter_api_key_private_key + lighter_api_key_index`,
				s.handleCreateExchange)
			s.routeWithSchema(protected, "PUT", "/exchanges", "Update an existing exchange account configuration",
				`Body: {"id":"<EXACT id from GET /api/exchanges>","exchange_type":"<string>","account_name":"<string>","enabled":<bool>,"api_key":"<string>","secret_key":"<string>","passphrase":"<string, for okx/gate/kucoin>"}
Use this to enable/disable an exchange or update API credentials. The "id" field is required to identify which exchange to update.`,
				s.handleUpdateExchangeConfigs)
			s.routeWithSchema(protected, "DELETE", "/exchanges/:id", "Delete exchange account",
				`:id = EXACT id from GET /api/exchanges. Permanently removes the exchange account and disconnects any traders using it.`,
				s.handleDeleteExchange)

			// Telegram bot configuration
			s.routeWithSchema(protected, "GET", "/telegram", "Get Telegram bot configuration",
				`Returns: {"bot_token":"<string>","model_id":"<EXACT id of configured AI model>","chat_id":"<bound Telegram chat id, empty if not bound>"}`,
				s.handleGetTelegramConfig)
			s.routeWithSchema(protected, "POST", "/telegram", "Set Telegram bot token and AI model",
				`Body: {"bot_token":"<string — Telegram BotFather token>","model_id":"<EXACT id from GET /api/models>"}
Both fields are required. After saving, the user must send /start in Telegram to bind their account.`,
				s.handleUpdateTelegramConfig)
			s.routeWithSchema(protected, "POST", "/telegram/model", "Update Telegram bot AI model only",
				`Body: {"model_id":"<EXACT id from GET /api/models>"}`,
				s.handleUpdateTelegramModel)
			s.routeWithSchema(protected, "DELETE", "/telegram/binding", "Unbind Telegram account",
				`No body needed. Clears the Telegram chat_id binding so the user can re-bind with /start.`,
				s.handleUnbindTelegram)

			// Strategy management
			s.routeWithSchema(protected, "GET", "/strategies", "List user's strategies",
				`Returns: [{"id":"<EXACT id — use as strategy_id when creating/updating a trader>","name":"<string>","is_active":<bool>,"is_default":<bool>}]
CRITICAL: Always use the "id" field for strategy_id.`,
				s.handleGetStrategies)
			s.routeWithSchema(protected, "GET", "/strategies/active", "Get the currently active strategy",
				`Returns the strategy marked is_active=true for this user, or the system default. Use this to find which strategy is currently in use.`,
				s.handleGetActiveStrategy)
			s.routeWithSchema(protected, "GET", "/strategies/default-config", "Get default strategy config with all fields and sensible values — use as reference for building configs",
				`No parameters needed. Returns a complete StrategyConfig object with all fields populated with recommended defaults. Read this before building a custom config.`,
				s.handleGetDefaultStrategyConfig)
			s.route(protected, "GET", "/strategies/timeframes", "Get supported kline timeframes (single source of truth for the Strategy Studio selector)", s.handleGetSupportedTimeframes)
			s.route(protected, "POST", "/strategies/preview-prompt", "Preview the AI prompt that will be generated from a config", s.handlePreviewPrompt)
			s.route(protected, "POST", "/strategies/test-run", "Test-run strategy AI analysis", s.handleStrategyTestRun)
			s.route(protected, "GET", "/strategies/:id", "Get strategy by ID", s.handleGetStrategy)
			s.routeWithSchema(protected, "POST", "/strategies", "Create a new trading strategy",
				`Body: {"name":"<string, required>","description":"<string, optional>","lang":"zh|en","config":<StrategyConfig object, OPTIONAL — if omitted the system applies complete working defaults automatically (ai500 top coins, all standard indicators, standard risk control)>}
IMPORTANT: For most use cases just POST {"name":"<name>"} — the backend fills everything in. Only include "config" when the user explicitly requests custom settings (specific coins, custom leverage, custom timeframes).

StrategyConfig fields:
  coin_source.source_type: "static"(fixed coin list) | "ai500"(AI top500 ranking) | "oi_top"(OI increasing, suited for long) | "oi_low"(OI decreasing, suited for short)
  coin_source.static_coins: ["BTCUSDT","ETHUSDT"] — only when source_type="static"
  coin_source.use_ai500, ai500_limit: number of coins from AI500 pool (default 10)
  coin_source.use_oi_top/use_oi_low, oi_top_limit/oi_low_limit: OI-based coin selection
  indicators.klines.primary_timeframe: "1m"|"3m"|"5m"|"15m"|"1h"|"4h" — scalping→"5m", trend/swing→"1h"/"4h"
  indicators.klines.primary_count: number of candles (20-100)
  indicators.klines.enable_multi_timeframe: true for trend/swing analysis
  indicators.klines.selected_timeframes: e.g. ["5m","15m","1h","4h"]
  indicators.enable_raw_klines: ALWAYS true (raw OHLCV required)
  indicators.enable_ema: true for trend-following (EMA crossover signals)
  indicators.enable_macd: true for trend + momentum confirmation
  indicators.enable_rsi: true for overbought/oversold, divergence detection
  indicators.enable_boll: true for volatility, range trading, breakout strategies
  indicators.enable_atr: true for volatility measurement and stop-loss sizing
  indicators.enable_volume: ALWAYS true
  indicators.enable_oi: ALWAYS true (open interest data)
  indicators.enable_funding_rate: ALWAYS true
  indicators.ema_periods: [20,50] default, [9,21] for faster signals
  indicators.rsi_periods: [7,14] default
  indicators.atr_periods: [14] default
  indicators.boll_periods: [20] default
  indicators.nofxos_api_key: ALWAYS "cm_568c67eae410d912c54c"
  indicators.enable_quant_data: ALWAYS true
  indicators.enable_quant_oi: ALWAYS true
  indicators.enable_quant_netflow: ALWAYS true
  indicators.enable_oi_ranking: ALWAYS true, oi_ranking_duration:"1h", oi_ranking_limit:10
  indicators.enable_netflow_ranking: ALWAYS true, netflow_ranking_duration:"1h", netflow_ranking_limit:10
  indicators.enable_price_ranking: ALWAYS true, price_ranking_duration:"1h,4h,24h", price_ranking_limit:10
  risk_control.max_positions: max simultaneous positions (1=single coin, 3=diversified, 5=wide)
  risk_control.btc_eth_max_leverage: BTC/ETH leverage (conservative:3-5, moderate:5-10, aggressive:10-20)
  risk_control.altcoin_max_leverage: altcoin leverage (usually lower than BTC leverage)
  risk_control.btc_eth_max_position_value_ratio: max position size as multiple of equity (default 5)
  risk_control.altcoin_max_position_value_ratio: default 1
  risk_control.max_margin_usage: 0.5-0.95 (default 0.9 = use up to 90% margin)
  risk_control.min_position_size: minimum USDT per trade (default 12)
  risk_control.min_risk_reward_ratio: minimum profit/loss ratio required (default 3 = 3:1)
  risk_control.min_confidence: minimum AI confidence to open position (default 75, range 60-90)
  prompt_sections.role_definition: describe the AI's trading persona and goal
  prompt_sections.trading_frequency: guidelines on how often to trade
  prompt_sections.entry_standards: conditions that must align before entering a position
  prompt_sections.decision_process: step-by-step decision-making framework`,
				s.handleCreateStrategy)
			s.routeWithSchema(protected, "PUT", "/strategies/:id", "Update an existing strategy — WORKFLOW: 1) GET /api/strategies/:id first to read current config 2) Merge your changes into the full config 3) PUT with complete merged config 4) GET again to verify saved values",
				`Body: {"name":"<string>","description":"<string>","config":<complete StrategyConfig — same structure as POST /api/strategies>}
IMPORTANT: config is merged with existing values server-side, but always send the complete section you are modifying.
After updating, always GET /api/strategies/:id to verify and show the user actual saved values.`,
				s.handleUpdateStrategy)
			s.routeWithSchema(protected, "DELETE", "/strategies/:id", "Delete strategy",
				`:id = EXACT id from GET /api/strategies. Cannot delete a strategy that is currently assigned to a running trader.`,
				s.handleDeleteStrategy)
			s.routeWithSchema(protected, "POST", "/strategies/:id/activate", "Mark a strategy as the active strategy for this user",
				`:id = EXACT id from GET /api/strategies.
No request body needed. Sets this strategy as is_active=true (and deactivates the previous active strategy).
After activating, create or update a trader with this strategy_id to apply it.`,
				s.handleActivateStrategy)
			s.routeWithSchema(protected, "POST", "/strategies/:id/duplicate", "Duplicate an existing strategy",
				`:id = EXACT id from GET /api/strategies. Creates a copy with " (copy)" appended to the name.`,
				s.handleDuplicateStrategy)

			// Data for specified trader (using query parameter ?trader_id=xxx)
			// IMPORTANT: All ?trader_id= values must be the EXACT "trader_id" field from GET /api/my-traders
			s.routeWithSchema(protected, "GET", "/status", "Trader running status",
				`Query: ?trader_id=<EXACT trader_id from GET /api/my-traders>
Returns: {"is_running":<bool>,"trader_id":"<string>"}`,
				s.handleStatus)
			s.routeWithSchema(protected, "GET", "/account", "Account balance and equity",
				`Query: ?trader_id=<EXACT trader_id from GET /api/my-traders>
Returns: {"balance":<float>,"equity":<float>,"unrealized_pnl":<float>,"initial_balance":<float>,"total_return_pct":<float>}`,
				s.handleAccount)
			s.routeWithSchema(protected, "GET", "/positions", "Current open positions",
				`Query: ?trader_id=<EXACT trader_id from GET /api/my-traders>
Returns: [{"symbol":"<string>","side":"long|short","quantity":<float>,"entry_price":<float>,"mark_price":<float>,"unrealized_pnl":<float>,"leverage":<int>}]`,
				s.handlePositions)
			s.routeWithSchema(protected, "GET", "/positions/history", "Closed position history",
				`Query: ?trader_id=<EXACT trader_id from GET /api/my-traders>&limit=<int, default 20>`,
				s.handlePositionHistory)
			s.routeWithSchema(protected, "POST", "/debug/nt-test-trade", "DEBUG: deterministic 1-contract SIM test trade (futures/NT only)",
				`Query: ?trader_id=<id>&side=long|short — places one MNQ bracket on the NT SIM account for end-to-end proof (signal→fill→position). SIM/futures only; bypasses AI + risk gate.`,
				s.handleNTTestTrade)
			s.routeWithSchema(protected, "POST", "/debug/nt-bars-unsubscribe", "DEBUG: drop an EXTRA bar subscription (P5.1 dispose-one proof; futures/NT only)",
				`Query: ?trader_id=<id>&symbol=<extra root, e.g. ES> — sends bars_unsubscribe so the AddOn disposes that root's BarsRequests. The PRIMARY trading symbol is refused. Bars-only; no order path.`,
				s.handleNTBarsUnsubscribe)
			s.routeWithSchema(protected, "GET", "/nt/symbols", "P5.3: current NT bar-subscription set + lifecycle states (read-only)",
				`Query: ?trader_id=<id> — returns {primary, states{SYMBOL:{state,contract,reason}}, runtime_enabled}`,
				s.handleNTSymbolsList)
			s.routeWithSchema(protected, "POST", "/nt/symbols", "P5.3: ADD an extra bar-subscription root at runtime (flag NT_RUNTIME_SYMBOLS)",
				`Query: ?trader_id=<id>&symbol=<root, e.g. NQ> — subscribes NOW (no restart); the AddOn acks subscribed/subscribe_error. Bars-only.`,
				s.handleNTSymbolsAdd)
			s.routeWithSchema(protected, "DELETE", "/nt/symbols", "P5.3: REMOVE an extra bar-subscription root at runtime (flag NT_RUNTIME_SYMBOLS)",
				`Query: ?trader_id=<id>&symbol=<root> — unsubscribes + purges its cached bars. The PRIMARY trading symbol is refused.`,
				s.handleNTSymbolsRemove)
			// BAR-TRUTH WAVE (2026-08-28) — deep backfill + three-way
			// arbiter (S-1): NT8 replay truth vs cache vs persisted bars.
			s.routeWithSchema(protected, "POST", "/nt/bar-arbiter", "Owner: deep bars backfill + NT8/cache/DB three-way diff (BAR-TRUTH)",
				`Body: {"trader_id":"<id>","action":"backfill"|"diff","symbol":"MNQ","timeframe":"1m","bars_back":8640}. backfill sends a one-shot deep bars_subscribe; diff returns the three-way counts+hashes+ATR14(5m)+deltas+drops.`,
				s.handleBarTruthArbiter)
			s.routeWithSchema(protected, "GET", "/trades", "Trade records",
				`Query: ?trader_id=<EXACT trader_id from GET /api/my-traders>&limit=<int, default 20>`,
				s.handleTrades)
			s.routeWithSchema(protected, "GET", "/orders", "All order records",
				`Query: ?trader_id=<EXACT trader_id from GET /api/my-traders>&limit=<int, default 20>`,
				s.handleOrders)
			s.routeWithSchema(protected, "GET", "/orders/:id/fills", "Order fill details",
				`:id = order id from GET /api/orders`,
				s.handleOrderFills)
			s.routeWithSchema(protected, "GET", "/open-orders", "Open orders currently on exchange",
				`Query: ?trader_id=<EXACT trader_id from GET /api/my-traders>`,
				s.handleOpenOrders)
			s.routeWithSchema(protected, "GET", "/cutover-gate", "Pre-cutover gate: all five legs in one payload (class 33)",
				`Query: ?trader_id=<EXACT trader_id from GET /api/my-traders>
Returns: {"ready":<bool>,"legs":[{"n":1..5,"name":"<string>","pass":<bool>,"detail":"<string>","source":"<string>"}],"note":"<string>"}
Legs: 1 db_open_positions · 2 api_positions · 3 nt8_positions_snapshot · 4 working_orders (broker versus placed/unconfirmed ledger rows) · 5 planner_in_flight. Leg 4's armed_unplaced count is informational: authorized arms without a signal id do not fail it.`,
				s.handleCutoverGate)
			s.routeWithSchema(protected, "GET", "/decisions", "AI trading decisions (decision records)",
				`Query: ?trader_id=<EXACT trader_id from GET /api/my-traders>&limit=<int, default 20>
Returns: [{"id":"<string>","symbol":"<string>","action":"open_long|open_short|close_long|close_short|hold","confidence":<int>,"reasoning":"<string>","created_at":"<timestamp>"}]`,
				s.handleDecisions)
			s.routeWithSchema(protected, "GET", "/decisions/latest", "Latest AI decisions (most recent scan results)",
				`Query: ?trader_id=<EXACT trader_id from GET /api/my-traders>
Returns the most recent AI decision for each symbol analyzed in the last scan cycle.`,
				s.handleLatestDecisions)
			s.routeWithSchema(protected, "GET", "/statistics", "Trading performance statistics",
				`Query: ?trader_id=<EXACT trader_id from GET /api/my-traders>
Returns: {"total_trades":<int>,"winning_trades":<int>,"win_rate":<float>,"total_pnl":<float>,"sharpe_ratio":<float>,"max_drawdown":<float>}`,
				s.handleStatistics)

			// Plan 4 Task 23 — risk + audit endpoints
			s.routeWithSchema(protected, "POST", "/risk/force-flat", "Emergency flatten all positions for a trader",
				`Body or query: trader_id=<EXACT trader_id from GET /api/my-traders>
Returns: {"triggered":<bool>,"trader_id":"<string>","positions_flattened":<int>,"timestamp_utc":"<RFC3339>","log_message":"<string>","reason":"<string, only when triggered=false>"}
For non-ninjatrader brokers returns triggered=false with reason explaining why.`,
				s.handleForceFlat)
			// THE DESK STRIP (2026-09-06) — one read, one row per fact the owner
			// needs during a session. Read-only; writes nothing.
			s.routeWithSchema(protected, "GET", "/desk", "The desk strip: one row per fact the owner needs during a session",
				`Query: ?trader_id=<EXACT trader_id from GET /api/my-traders>
Returns one rendered row per strip line, each carrying its value, unit, SOURCE, as-of instant, age and a verified flag.
A row it cannot compute is returned with state "unknown" AND a reason string - never omitted, never zero, never a dash.
Returns: {"trader_id","generated_at_ms","cadence_ms","unknown_count","stale_count","lines":[{"n","key","label","text","state","unit","source","as_of_ms","age_ms","verified","reason"}]}
state is one of: ok | flat | stale | unknown. cadence_ms is 5000 while a position or arm is live, 15000 otherwise.`,
				s.handleDesk)
			s.routeWithSchema(protected, "GET", "/risk/status", "Get current risk-limit + account state snapshot",
				`Query: ?trader_id=<EXACT trader_id from GET /api/my-traders>
Returns: {"trader_id":"<string>","daily_pnl_usd":<float>,"daily_loss_limit_usd":<float>,"concurrent_trades":<int>,"max_concurrent_trades":<int>,"current_notional_usd":<float>,"max_notional_usd":<float>,"kill_switch_armed":<bool>,"last_reset_utc":"<RFC3339>"}`,
				s.handleRiskStatus)
			s.routeWithSchema(protected, "GET", "/risk/gate-blocks", "Per-trader/session-day gate-block counters (B6)",
				`No params. Returns today's tally of how many times each risk/safety gate blocked an entry or cycle.
Returns: {"session_day_utc":"<RFC3339>","summary":"<one-line>","by_trader":{"<trader_id>":{"<gate>":<count>}}}
The empty-string trader key holds process-wide gates (e.g. the B3 order guard). Resets at the 17:00 CT CME session rollover.`,
				s.handleGateBlocks)
			// INSTRUMENT HONESTY (owner ruling 2026-09-03) — read-only.
			s.routeWithSchema(protected, "GET", "/risk/stream-cuts", "Stream cuts and watchdog fires, grouped by how long the connection had been idle",
				`No params. Every early stream end — a peer FIN ("cut") or our own watchdog ("watchdog") — with the idleness of the connection it rode and what the identical resend then did.
Returns: {"rows":[{"bucket","n","cuts","fires","recovered","lost","unresolved"}],"table":"<rendered>","note":"<why nothing is set yet>"}
NO THRESHOLD IS SET. IdleConnTimeout is untouched until the evidence is in; this endpoint is the evidence.`,
				s.handleStreamCuts)
			s.routeWithSchema(protected, "GET", "/risk/errors", "Per-trader/session-day structured error events (P0-cleanup)",
				`No params. Returns structured error events: stable type, plain cause, cost.
Returns: {"rows":[{"trader","type","cause","cost","count","decisions_lost","trades_lost"}],"summary":"errors today: N (types: …), decisions lost: N"}
Resets at the 17:00 CT CME session rollover.`,
				s.handleErrors)
			s.routeWithSchema(protected, "GET", "/risk/freezes", "List frozen traders (A4/G4)",
				`No params. Returns traders frozen by an identity/account mismatch or reconcile divergence.
Returns: {"frozen":{"<trader_id>":"<reason>"}}  (empty object = none frozen)`,
				s.handleListFreezes)
			s.routeWithSchema(protected, "POST", "/risk/clear-freeze", "Clear a trader freeze — owner action (A4/G4)",
				`Query: ?trader_id=<EXACT trader_id>
Clears the freeze so NEW entries resume. Returns: {"trader_id":"<id>","was_frozen":<bool>,"cleared":<bool>}`,
				s.handleClearFreeze)
			s.routeWithSchema(protected, "GET", "/audit/decisions", "Decision audit trail (Plan 4 T23)",
				`Query: ?trader_id=<EXACT trader_id>&since=<YYYY-MM-DD, default 7d ago>&limit=<int, default 100, max 1000>
Returns: []DecisionRecord JSON ordered by timestamp DESC, including PromptVersion, AIModel, AILatencyMs, RiskCheck*, ExecutionStatus, FillPrice, FillLatencyMs.`,
				s.handleDecisionAudit)

			// P4.1 — Day-Plan API (mirror /api/risk/* inline trader_id). Additive;
			// found=false when day_plan enabled but no plan yet (pre-★2) or at night.
			s.routeWithSchema(protected, "GET", "/plan/today", "Active day-plan (overlay-resolved) + live scenario facts",
				`Query: ?trader_id=<EXACT trader_id>[&symbol=MNQ][&session=NY|ASIA|LONDON][&version=<n>]
Returns: {found, trade_date, session, version, historical, latest_version, trigger_reason, created_at, lifecycle, model_id, mode, night, doc:{bias,levels,scenarios,no_trade,death_condition}, level_facts:[{price,label,grade,distance,sweep,closes_beyond,accept_have,accept_need,still_valid}], price, replans_left, warming}. found=false = no active plan (night/disabled/none-yet).
version=<n> serves that HISTORICAL version (overlays resolved on the version they belonged to); omit for the latest. List versions with GET /plan/versions.`,
				s.handlePlanToday)
			// ITEM 15 — every stored version of ONE session's plan, with each
			// version's death reason and a plain-language diff vs its successor.
			// /plan/history is a global 30-row feed and strips the doc; this is
			// the per-session read path the version chips need.
			s.routeWithSchema(protected, "GET", "/plan/versions", "All stored versions of one session's plan",
				`Query: ?trader_id=<EXACT trader_id>&session=<NY|ASIA|LONDON>[&trade_date=YYYY-MM-DD]
Returns: {trade_date, session, latest_version, versions:[{version, lifecycle, trigger_reason, created_at, model_id, degraded, is_latest, level_count, scenario_count, bias, day_type, death_condition, superseded_by, death_reason, diff_vs_next:[string]}]} oldest first.
Fetch a specific version's full doc with GET /plan/today?version=<n>.`,
				s.handlePlanVersions)
			s.routeWithSchema(protected, "GET", "/plan/history", "Recent plan versions (trade_date, session, version)",
				`Query: ?trader_id=<EXACT trader_id>. Returns: {history:[{trade_date,session,version,lifecycle,model_id,trigger_reason}]}`,
				s.handlePlanHistory)
			// ITEM 3 — the owner's manual re-read. GET reports whether it is
			// available and what it costs; POST spends one re-plan.
			s.routeWithSchema(protected, "GET", "/plan/reread", "May the owner force a planner re-read now?",
				`Query: ?trader_id=<EXACT trader_id>. Returns: {allowed, reason, session, replans_left, replan_cap, version}.`,
				s.handlePlanRereadStatus)
			s.routeWithSchema(protected, "POST", "/plan/reread", "Force a fresh planner read for the current session",
				`Body: {"trader_id":"<id>"}. SPENDS one re-plan from the session budget and writes a new version with trigger_reason "owner_reread". 409 with {error, gate} when refused (budget spent, session closed/disabled, market closed, already NO-TRADE).`,
				s.handlePlanReread)
			// P6 — the owner reset: ABANDON the chain (history preserved), re-arm
			// the whole re-plan budget, clear NO-TRADE, fresh plan via the normal
			// path with trigger_reason "owner reset". Never touches positions,
			// brackets, guardrail counters or the daily cage.
			s.routeWithSchema(protected, "GET", "/plan/reset", "May the owner reset this session's plan chain now?",
				`Query: ?trader_id=<EXACT trader_id>. Returns: {allowed, reason, session, version, replan_cap}.`,
				s.handlePlanResetStatus)
			s.routeWithSchema(protected, "POST", "/plan/reset", "Reset the session's plan chain (abandon + fresh budget + fresh plan)",
				`Body: {"trader_id":"<id>"}. Marks the current chain ABANDONED (append-only history preserved), restores the budget to replan_cap, clears NO-TRADE state, and runs a fresh read with trigger_reason "owner reset". 409 with {error, gate} when refused. Owner sticky levels carry by price identity.`,
				s.handlePlanReset) // E2 DEBUG SEAM (2026-08-27) — drives the REAL armed
			// placement path with a TEST-E2 row. Env-gated
			// (ARMED_TEST_SEAM=on, default OFF) + SIM-only.
			s.routeWithSchema(protected, "POST", "/armed/test-arm", "E2 seam: place/cancel a TEST-E2 armed order on the real path (env-gated + SIM-only)",
				`Body: {"trader_id":"<id>","action":"place","side":"long","entry":1,"stop":1,"target":1} or {"action":"cancel","signal_id":"<sid>"}.`,
				s.handleArmedTestArm)
			s.routeWithSchema(protected, "GET", "/plan/alerts", "In-app alert feed (P0/P1/P2) + unacked count",
				`Query: ?trader_id=<EXACT trader_id>. Returns: {alerts:[{id,level,kind,title,body,acked,created_at}], unacked:<int>}`,
				s.handlePlanAlerts)
			s.routeWithSchema(protected, "POST", "/plan/alert-ack", "Acknowledge an in-app alert",
				`Body: {"trader_id":"<EXACT trader_id>","alert_id":<int>}. Returns: {acked:true, alert_id}`,
				s.handlePlanAlertAck)
			// ITEM 5 — the feed can be cleared; the event rows survive (soft-delete).
			s.routeWithSchema(protected, "POST", "/plan/alert-dismiss", "Hide one alert from the feed",
				`Body: {"trader_id":"<id>","alert_id":<int>}. Soft-delete: the row stays for audit. 404 if the alert is not the caller's. 409 {needs_ack:true} for an UNACKNOWLEDGED P0 — acknowledge it first.`,
				s.handlePlanAlertDismiss)
			s.routeWithSchema(protected, "POST", "/plan/alert-clear-read", "Clear every acknowledged alert",
				`Body: {"trader_id":"<id>"}. Returns {cleared:<int>}. Unacknowledged alerts (especially P0) are left in place.`,
				s.handlePlanAlertClearRead)
			// P5.1 — overlay editing (RFC-6902 + test-op concurrency + B2 armor) + sticky owner levels.
			s.routeWithSchema(protected, "POST", "/plan/overlay", "Append an RFC-6902 overlay to the active plan",
				`Body: {"trader_id":"<id>","symbol":"MNQ","patch":"<JSON array of RFC-6902 ops>","origin":"owner|planner-revised"}.
Applies the patch strictly onto the current plan_final (test-op concurrency → 409 on conflict), armors owner prices (422 on 8×dATR fat-finger), validates enums/counts. Returns: {overlay_version, plan_version, origin}.`,
				s.handlePlanOverlay)
			s.routeWithSchema(protected, "POST", "/plan/owner-level", "Add a sticky owner level (note + scenario tag ride to the planner)",
				`Body: {"trader_id":"<id>","symbol":"MNQ","price":<float>,"label":"<str>","note":"<any-lang>","scenario_tag":"<S1|＋new>"}. B2-armored. Returns: {id, symbol, price}.`,
				s.handlePlanOwnerLevel)
			s.routeWithSchema(protected, "POST", "/plan/owner-level/delete", "Delete a sticky owner level by id",
				`Body: {"trader_id":"<id>","id":<int>}. Returns: {deleted:true, id}.`,
				s.handlePlanOwnerLevelDelete)
			// P5.4 — Ask-Planner (plan-scoped Q&A, anti-sycophancy contract, verdict log).
			s.routeWithSchema(protected, "POST", "/plan/ask", "Ask the planner about today's plan (anti-sycophancy)",
				`Body: {"trader_id":"<id>","symbol":"MNQ","question":"<any language>"}.
Returns: {qa_id, plan_id, plan_version, reply:{evidence, point_class:NEW-INFO|BARE-DISAGREEMENT, verdict:DEFEND|CONCEDE|PROPOSE-MERGE, summary, patch}}. A bare disagreement never carries a patch.`,
				s.handlePlanAsk)
			s.routeWithSchema(protected, "GET", "/plan/ask", "Ask-Planner thread + sycophancy KPI",
				`Query: ?trader_id=<id>[&plan_id=<trade_date:session>]. Returns: {thread:[{role,content,evidence,point_class,verdict,patch,applied,created_at}], kpi:{total,new_info,bare_disagreement,defend,concede,propose_merge,applied,defend_on_bare}}.`,
				s.handlePlanThread)
			s.routeWithSchema(protected, "POST", "/plan/ask/apply", "Apply a PROPOSE-MERGE patch as a planner-revised overlay",
				`Body: {"trader_id":"<id>","symbol":"MNQ","qa_id":<int>}. Applies the reply's patch (origin planner-revised) + marks it applied. Returns: {applied:true, overlay_version, plan_version}.`,
				s.handlePlanAskApply)
			s.routeWithSchema(protected, "POST", "/plan/ask/decline", "Decline a PROPOSE-MERGE proposal (recorded, plan untouched)",
				`Body: {"trader_id":"<id>","qa_id":<int>}. Records an owner-role DECLINED row carrying the reply's plan/trigger provenance so the KPI series counts rejections, not just silence. The plan is NOT modified. Idempotent. Returns: {declined:true, qa_id}.`,
				s.handlePlanAskDecline)
			// P5.5 — adherence grade feed (graded closed trades + GPA, separate from P&L).
			s.routeWithSchema(protected, "GET", "/plan/trades", "Graded closed trades + adherence summary (A–F, separate from P&L)",
				`Query: ?trader_id=<id>. Returns: {trades:[{symbol,side,entry_price,exit_price,entry_time,exit_time,realized_pnl,mae,mfe,cited_scenario_id,plan_matched,adherence_grade,adherence_label}], summary:{counts,total,gpa}}.`,
				s.handlePlanTrades)
			// 1D — per-condition expectancy read model (READ-ONLY; rules on
			// nothing). The floor and the promotion criterion travel in the
			// payload so the panel cannot hold its own copy of either.
			s.routeWithSchema(protected, "GET", "/expectancy", "Per-condition expectancy table on pnl_corrected (1D)",
				`Query: ?by=condition|session|kind|path|full (default condition) &era=pre-0B|post-0B (default: both).
Returns: {by, rows:[{key:{condition,session,level_kind,path,era}, n, wins, losses, flats, sum_pnl_corrected, mean, sd, win_rate, wilson_lo, wilson_hi, mean_lo, mean_hi, t_stat, avg_realized_r, avg_planned_rr, median_mae, median_mfe, stop_hit_share, target_hit_share, excluded_unresolved, row_ids, descriptive_only, status}], counterfactual_e8:[...], excluded:{unresolved_pnl,unresolvable,test_seam,no_condition,crypto_era}, min_n, as_of_ms, as_of_utc, built_at_ms, era_0b_start, promotion_rule}.
Money is pnl_corrected ONLY; a NULL is UNRESOLVED, excluded and counted. status is NOT ENOUGH DATA below min_n. Statistics that cannot be computed are null, never 0.`,
				s.handleExpectancy)
			// W8 — admin session registry (GLOBAL; the gates read it, fallback default).
			s.routeWithSchema(protected, "GET", "/plan/session-registry", "Effective admin session registry (+ is_default)",
				`Query: ?trader_id=<id>. Returns: {registry:{sessions:[{name,window_start_ct,window_end_ct,read_ct,flat_ct,killzones,enabled}],half_days}, is_default:<bool>, sessions:<int>}.`,
				s.handlePlanSessionRegistry)
			s.routeWithSchema(protected, "POST", "/plan/session-registry", "Save the admin session registry (validated; next-day gates honor it)",
				`Body: {"trader_id":"<id>","registry":{sessions:[{name,window_start_ct,window_end_ct,read_ct,flat_ct,killzones,enabled}]}}. Malformed → 400 (never silently defaulted). Returns: {saved:true, sessions:<int>}.`,
				s.handlePlanSessionRegistrySave)
			// W13 — plan re-alignment on owner edit (auto after an overlay save, or
			// the manual "Re-align plan" fallback once the auto cap is spent).
			s.routeWithSchema(protected, "POST", "/plan/realign", "Re-examine the whole plan after an owner edit (proposal only)",
				`Body: {"trader_id":"<id>","symbol":"MNQ","manual":<bool>,"change":{"kind":"add-level|edit-level|delete-level|bulk-add","summary":"<str>","price":<float>,"label":"<str>","grade":"A|B|C","instruction":"<str>","note":"<any-lang>","scenario_tag":"<S1|+new>","batch_count":<int>}}.
Always 200. status: proposal (PROPOSE-MERGE + RFC-6902 patch, Apply via POST /plan/ask/apply) | no-change | skipped | debounced | capped | failed (fail-closed: plan untouched + alert row). Returns {qa_id, plan_version, would_become, latency_ms, cost_usd, reply{evidence,point_class,verdict,summary,patch}}.`,
				s.handlePlanRealign)
			// W9 — approval gate: owner approves entries for the current session-day
			// (only meaningful when approval_required is ON).
			s.routeWithSchema(protected, "POST", "/plan/approve", "Approve entries for this trader's current CME session-day",
				`Body: {"trader_id":"<id>"}. Grants entries for the current CME session-day (when approval_required is ON). Returns: {approved:true, session_day}.`,
				s.handlePlanApprove)
			// P5.6 — matched-random honesty gate (frozen weekly snapshot + WARMING progress).
			s.routeWithSchema(protected, "GET", "/plan/stats", "Matched-random honesty gate (WARMING until pre-registered N)",
				`Query: ?trader_id=<id>. Returns: {weekly:{iso_week,computed_at,verdicts:[{level_type,n,react_rate,delta_pp,p_value,status:WARMING|BEATS-RANDOM|NO-EDGE,label}]}|null, progress:[{level_type,n,target_n,react_rate,warming}], target_n, alpha}. No green on an underpowered sample, ever.`,
				s.handlePlanStats)

			// Plan 4 Stage 4 — NinjaTrader account management
			s.routeWithSchema(protected, "GET", "/accounts", "List available NT accounts (NinjaTrader TCP bridge only)",
				`Query: ?trader_id=<EXACT trader_id from GET /api/my-traders>
Returns: {"current":"<account name or empty>","accounts":[{"name":"<string>","is_sim":<bool>}]}
Empty list + current=null means no accounts_list frame received yet (AddOn not connected).`,
				s.handleGetAccounts)
			s.routeWithSchema(protected, "POST", "/account/select", "Switch to a different NT account (SIM-only, server-side guard)",
				`Query: ?trader_id=<EXACT trader_id from GET /api/my-traders>
Body: {"account":"<account name, e.g. Sim101>"}
Returns: {"current":"<new account name>","message":"<status>"}
Server rejects non-SIM accounts (is_sim == false) with HTTP 400.`,
				s.handleSelectAccount)

		}
	}

	// UI SERVING PATH (owner ruling 2026-09-03). Mounted LAST, after every API
	// route, because it installs the NoRoute handler: anything that reached here
	// matched no API route, and only then may it be a page request. A stale or
	// missing bundle degrades loudly via the boot line rather than failing here.
	MountUI(s.router, UIDistDir)
}

// handleHealth Health check
func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"time":     c.Request.Context().Value("time"),
		"revision": kernel.RunningRevision(),
	})
}

// handleGetSystemConfig Get system configuration (configuration that client needs to know)
func (s *Server) handleGetSystemConfig(c *gin.Context) {
	userCount, _ := s.store.User().Count()
	c.JSON(http.StatusOK, gin.H{
		"initialized": userCount > 0,
		// RunningRevision is the vcs.revision embedded in THIS binary — bug
		// reports can now be checked against the running rev without a shell
		// (master-audit v1 finding 5.6).
		"revision":         kernel.RunningRevision(),
		"btc_eth_leverage": 10,
		"altcoin_leverage": 5,
		// SANDBOX (isolated demo instance) → the UI paints a permanent banner so a
		// sandbox can never be mistaken for the live system. false in production.
		"sandbox": SandboxMode(),
	})
}

// handleGetServerIP Get server IP address (for whitelist configuration)
func (s *Server) handleGetServerIP(c *gin.Context) {
	// Try to get public IP via third-party API
	publicIP := getPublicIPFromAPI()

	// If third-party API fails, get first public IP from network interface
	if publicIP == "" {
		publicIP = getPublicIPFromInterface()
	}

	// If still cannot get it, return error
	if publicIP == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unable to get public IP address"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"public_ip": publicIP,
		"message":   "Please add this IP address to the whitelist",
	})
}

// getPublicIPFromAPI Get public IP via third-party API (IPv4 only)
func getPublicIPFromAPI() string {
	// Try multiple public IP query services (IPv4-only endpoints)
	services := []string{
		"https://api4.ipify.org?format=text", // IPv4 only
		"https://ipv4.icanhazip.com",         // IPv4 only
		"https://v4.ident.me",                // IPv4 only
		"https://api.ipify.org?format=text",  // May return IPv4 or IPv6
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	for _, service := range services {
		resp, err := client.Get(service)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			body := make([]byte, 128)
			n, err := resp.Body.Read(body)
			if err != nil && err.Error() != "EOF" {
				continue
			}

			ip := strings.TrimSpace(string(body[:n]))
			parsedIP := net.ParseIP(ip)
			// Verify if it's a valid IPv4 address (not containing ":")
			if parsedIP != nil && parsedIP.To4() != nil {
				return ip
			}
		}
	}

	return ""
}

// getPublicIPFromInterface Get first public IP from network interface
func getPublicIPFromInterface() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range interfaces {
		// Skip disabled interfaces and loopback interfaces
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() {
				continue
			}

			// Only consider IPv4 addresses
			if ip.To4() != nil {
				ipStr := ip.String()
				// Exclude private IP address ranges
				if !isPrivateIP(ip) {
					return ipStr
				}
			}
		}
	}

	return ""
}

// isPrivateIP Determine if it's a private IP address
func isPrivateIP(ip net.IP) bool {
	// Private IP address ranges:
	// 10.0.0.0/8
	// 172.16.0.0/12
	// 192.168.0.0/16
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}

	for _, cidr := range privateRanges {
		_, subnet, _ := net.ParseCIDR(cidr)
		if subnet.Contains(ip) {
			return true
		}
	}

	return false
}

// getTraderFromQuery Get trader from query parameter
func (s *Server) getTraderFromQuery(c *gin.Context) (*manager.TraderManager, string, error) {
	userID := c.GetString("user_id")
	traderID := c.Query("trader_id")

	// Ensure user's traders are loaded into memory
	if s.traderManager != nil {
		if err := s.traderManager.LoadUserTradersFromStore(s.store, userID); err != nil {
			logger.Infof("⚠️ Failed to load traders for user %s: %v", userID, err)
		}
	}

	// C3 (2026-08-25) — PUBLIC endpoints (competition, equity-history, SSE
	// ticket) have NO JWT user_id, so there is no ownership to enforce: return
	// the explicitly-queried trader after an existence check. An empty
	// trader_id stays an error — public callers must name the trader they want
	// (the old ids[0] global fallback is gone).
	if userID == "" {
		if traderID == "" {
			return nil, "", fmt.Errorf("No available traders")
		}
		t, gerr := s.store.Trader().Get(traderID)
		if gerr != nil || t == nil {
			return nil, "", fmt.Errorf("No available traders")
		}
		return s.traderManager, traderID, nil
	}

	if traderID == "" {
		// C3 (2026-08-25) — NO global fallback: a user with no traders used to
		// receive ids[0] — the FIRST trader in the process, i.e. ANOTHER user's
		// trader (cross-user leak). Now the answer is strictly the user's own.
		userTraders, err := s.store.Trader().List(userID)
		if err == nil && len(userTraders) > 0 {
			traderID = userTraders[0].ID
		} else {
			return nil, "", fmt.Errorf("No available traders")
		}
	}
	// C1/C3 — an explicit ?trader_id must belong to the JWT user (404-class
	// error; the handler maps it without leaking trader existence).
	userTraders, err := s.store.Trader().List(userID)
	if err != nil || len(userTraders) == 0 {
		return nil, "", fmt.Errorf("No available traders")
	}
	for _, t := range userTraders {
		if t.ID == traderID {
			return s.traderManager, traderID, nil
		}
	}
	return nil, "", fmt.Errorf("No available traders")
}

// authMiddleware JWT authentication middleware
func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing Authorization header"})
			c.Abort()
			return
		}

		// Check Bearer token format
		tokenParts := strings.Split(authHeader, " ")
		if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid Authorization format"})
			c.Abort()
			return
		}

		tokenString := tokenParts[1]

		// Blacklist check
		if auth.IsTokenBlacklisted(tokenString) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token expired, please login again"})
			c.Abort()
			return
		}

		// Validate JWT token
		claims, err := auth.ValidateJWT(tokenString)
		if err != nil {
			logger.Errorf("[Auth] Invalid token: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			c.Abort()
			return
		}

		// Store user information in context
		c.Set("user_id", claims.UserID)
		c.Set("email", claims.Email)
		c.Next()
	}
}

// Start Start server
func (s *Server) Start() error {
	// SECURITY (P0 S1): bind the configured interface — 127.0.0.1 by default, so
	// the API is loopback-only unless the operator sets API_SERVER_HOST. The old
	// code bound ":port" (0.0.0.0, every interface) while logging "localhost",
	// which read as safe and was not.
	host := s.host
	if host == "" {
		host = "127.0.0.1"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(s.port))
	logger.Infof("🌐 API server starting at http://%s", addr)
	logger.Infof("📊 API Documentation:")
	logger.Infof("  • GET  /api/health           - Health check")
	logger.Infof("  • GET  /api/traders          - Public AI trader leaderboard top 50 (no auth required)")
	logger.Infof("  • GET  /api/competition      - Public competition data (no auth required)")
	logger.Infof("  • GET  /api/top-traders      - Top 5 trader data (no auth required, for performance comparison)")
	logger.Infof("  • GET  /api/equity-history?trader_id=xxx - Public return rate historical data (no auth required, for competition)")
	logger.Infof("  • GET  /api/equity-history-batch?trader_ids=a,b,c - Batch get historical data (no auth required, performance comparison optimization)")
	logger.Infof("  • GET  /api/traders/:id/public-config - Public trader config (no auth required, no sensitive info)")
	logger.Infof("  • POST /api/traders          - Create new AI trader")
	logger.Infof("  • DELETE /api/traders/:id    - Delete AI trader")
	logger.Infof("  • POST /api/traders/:id/start - Start AI trader")
	logger.Infof("  • POST /api/traders/:id/stop  - Stop AI trader")
	logger.Infof("  • GET  /api/models           - Get AI model config")
	logger.Infof("  • PUT  /api/models           - Update AI model config")
	logger.Infof("  • GET  /api/exchanges        - Get exchange config")
	logger.Infof("  • PUT  /api/exchanges        - Update exchange config")
	logger.Infof("  • GET  /api/status?trader_id=xxx     - Specified trader's system status")
	logger.Infof("  • GET  /api/account?trader_id=xxx    - Specified trader's account info")
	logger.Infof("  • GET  /api/positions?trader_id=xxx  - Specified trader's position list")
	logger.Infof("  • GET  /api/decisions?trader_id=xxx  - Specified trader's decision log")
	logger.Infof("  • GET  /api/decisions/latest?trader_id=xxx - Specified trader's latest decisions")
	logger.Infof("  • GET  /api/statistics?trader_id=xxx - Specified trader's statistics")
	logger.Infof("  • GET  /api/performance?trader_id=xxx - Specified trader's AI learning performance analysis")
	logger.Info()

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}
	return s.httpServer.ListenAndServe()
}

// Shutdown Gracefully shutdown server
func (s *Server) Shutdown() error {
	if s.httpServer == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

// SetTelegramReloadCh sets the channel used to signal the Telegram bot to reload
func (s *Server) SetTelegramReloadCh(ch chan<- struct{}) {
	s.telegramReloadCh = ch
}
