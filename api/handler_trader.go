package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"vl/kernel"
	"vl/logger"
	"vl/safe"
	"vl/store"
	"vl/telemetry"
	traderpkg "vl/trader"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxManualAltLeverage = 20
)

// traderStartReadyTimeout bounds how long handleStartTrader waits for Run's
// ordered-install verdict before answering 200 (REL18-C FOLD). The install is a
// local registry write, so it completes in milliseconds; the bound exists so a
// slow setup can never turn a start into an indefinite HTTP hold. Not a sleep —
// a select deadline on the ready channel.
const traderStartReadyTimeout = 5 * time.Second

// AI trader management related structures
type CreateTraderRequest struct {
	Name                string  `json:"name" binding:"required"`
	AIModelID           string  `json:"ai_model_id" binding:"required"`
	ExchangeID          string  `json:"exchange_id" binding:"required"`
	StrategyID          string  `json:"strategy_id"` // Strategy ID (new version)
	InitialBalance      float64 `json:"initial_balance"`
	ScanIntervalMinutes int     `json:"scan_interval_minutes"`
	CadenceMode         string  `json:"cadence_mode"`        // P10: "interval" (default) | "bar_close" (legacy); empty keeps existing
	PositionMode        string  `json:"position_mode"`       // Phase 3: "ai_watch" (default) | "bracket_only"; empty keeps default/existing
	IsCrossMargin       *bool   `json:"is_cross_margin"`     // Pointer type, nil means use default value true
	ShowInCompetition   *bool   `json:"show_in_competition"` // Pointer type, nil means use default value true
	// The following fields are kept for backward compatibility, new version uses strategy config
	TradingSymbols       string `json:"trading_symbols"`
	CustomPrompt         string `json:"custom_prompt"`
	OverrideBasePrompt   bool   `json:"override_base_prompt"`
	SystemPromptTemplate string `json:"system_prompt_template"` // System prompt template name
}

// UpdateTraderRequest Update trader request
type UpdateTraderRequest struct {
	Name                string  `json:"name" binding:"required"`
	AIModelID           string  `json:"ai_model_id" binding:"required"`
	ExchangeID          string  `json:"exchange_id" binding:"required"`
	StrategyID          string  `json:"strategy_id"` // Strategy ID (new version)
	InitialBalance      float64 `json:"initial_balance"`
	ScanIntervalMinutes int     `json:"scan_interval_minutes"`
	CadenceMode         string  `json:"cadence_mode"`  // P10: "interval" | "bar_close"; empty keeps existing
	PositionMode        string  `json:"position_mode"` // Phase 3: "ai_watch" | "bracket_only"; empty keeps existing
	IsCrossMargin       *bool   `json:"is_cross_margin"`
	ShowInCompetition   *bool   `json:"show_in_competition"`
	// The following fields are kept for backward compatibility, new version uses strategy config
	TradingSymbols       string `json:"trading_symbols"`
	CustomPrompt         string `json:"custom_prompt"`
	OverrideBasePrompt   bool   `json:"override_base_prompt"`
	SystemPromptTemplate string `json:"system_prompt_template"`
}

func formatTraderCreationError(reason, nextStep string) string {
	if nextStep == "" {
		return fmt.Sprintf("这次未能创建机器人：%s。", reason)
	}
	return fmt.Sprintf("这次未能创建机器人：%s。%s。", reason, nextStep)
}

func traderCreationRequestError(reason string) string {
	return formatTraderCreationError(reason, "请检查你刚刚填写的内容后，再重新提交")
}

func exchangeDisplayName(exchange *store.Exchange) string {
	if exchange == nil {
		return "所选交易所账户"
	}
	if exchange.AccountName != "" {
		return fmt.Sprintf("%s（%s）", exchange.Name, exchange.AccountName)
	}
	if exchange.Name != "" {
		return exchange.Name
	}
	return "所选交易所账户"
}

func missingExchangeFields(exchange *store.Exchange) []string {
	// No venue requires API credentials any more (NinjaTrader SIM only); kept
	// as a seam in case a future venue adds required fields.
	return nil
}

func mapStringPairs(kv ...string) map[string]string {
	if len(kv) == 0 {
		return nil
	}

	params := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		params[kv[i]] = kv[i+1]
	}
	return params
}

func validateExchangeForTraderCreation(exchange *store.Exchange) (string, string, map[string]string) {
	if exchange == nil {
		return formatTraderCreationError("还没有找到你选择的交易所账户", "请前往「设置 > 交易所配置」先添加一个可用账户，再回来创建机器人"),
			"trader.create.exchange_not_found", nil
	}
	if !exchange.Enabled {
		return formatTraderCreationError(
			fmt.Sprintf("交易所账户「%s」目前处于未启用状态", exchangeDisplayName(exchange)),
			"请前往「设置 > 交易所配置」启用该账户后，再重新创建机器人",
		), "trader.create.exchange_disabled", mapStringPairs("exchange_name", exchangeDisplayName(exchange))
	}

	missing := missingExchangeFields(exchange)
	if len(missing) > 0 {
		return formatTraderCreationError(
				fmt.Sprintf("交易所账户「%s」的配置还不完整，缺少 %s", exchangeDisplayName(exchange), strings.Join(missing, "、")),
				"请前往「设置 > 交易所配置」补全该账户的必填信息后，再重新创建机器人",
			), "trader.create.exchange_missing_fields", mapStringPairs(
				"exchange_name", exchangeDisplayName(exchange),
				"missing_fields", strings.Join(missing, ", "),
			)
	}

	switch exchange.ExchangeType {
	case "ninjatrader":
		return "", "", nil
	default:
		return formatTraderCreationError(
				fmt.Sprintf("交易所账户「%s」使用了当前版本暂不支持的类型 %s", exchangeDisplayName(exchange), exchange.ExchangeType),
				"请改用当前版本支持的交易所账户后，再重新创建机器人",
			), "trader.create.exchange_unsupported", mapStringPairs(
				"exchange_name", exchangeDisplayName(exchange),
				"exchange_type", exchange.ExchangeType,
			)
	}
}

func classifyTraderSetupReason(reason string) (string, string) {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return "", ""
	}

	lower := strings.ToLower(trimmed)

	switch {
	case strings.Contains(lower, "failed to parse strategy config"),
		strings.Contains(lower, "failed to parse strategy configuration"):
		return "trader.reason.strategy_config_invalid", "当前策略配置内容已损坏，系统暂时无法解析"
	case strings.Contains(lower, "has no strategy configured"):
		return "trader.reason.strategy_missing", "当前机器人缺少有效的交易策略配置"
	case strings.Contains(lower, "failed to parse private key"),
		(strings.Contains(lower, "invalid hex character") && strings.Contains(lower, "private key")):
		return "trader.reason.private_key_invalid", "私钥格式不正确，系统无法识别"
	case strings.Contains(lower, "failed to get meta information"):
		return "trader.reason.exchange_meta_unavailable", "系统暂时无法从交易所读取账户元信息"
	case strings.Contains(lower, "security check failed"):
		return "trader.reason.security_check_failed", "账户安全校验未通过"
	case strings.Contains(lower, "failed to initialize account"):
		return "trader.reason.exchange_account_init_failed", "交易所账户初始化失败，请确认钱包地址和 API Key 是否匹配"
	case strings.Contains(lower, "unsupported trading platform"):
		return "trader.reason.exchange_unsupported", "当前交易所类型暂不支持机器人初始化"
	case strings.Contains(lower, "initial balance not set and unable to fetch balance from exchange"):
		return "trader.reason.exchange_balance_unavailable", "系统暂时无法从交易所读取账户余额"
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "no such host"), strings.Contains(lower, "connection refused"):
		return "trader.reason.exchange_service_unreachable", "系统暂时无法连接交易所服务"
	default:
		return "trader.reason.unknown", trimmed
	}
}

func humanizeTraderSetupReason(reason string) string {
	_, message := classifyTraderSetupReason(reason)
	return message
}

func traderSetupReasonParams(err error, fallback string, kv ...string) map[string]string {
	params := mapStringPairs(kv...)
	rawReason := SanitizeError(err, fallback)
	reasonKey, reasonMessage := classifyTraderSetupReason(rawReason)
	if reasonMessage == "" && fallback != "" {
		reasonMessage = fallback
	}
	if reasonMessage != "" {
		if params == nil {
			params = map[string]string{}
		}
		params["reason"] = reasonMessage
	}
	if reasonKey != "" {
		if params == nil {
			params = map[string]string{}
		}
		params["reason_key"] = reasonKey
	}
	return params
}

func describeTraderLoadError(traderName string, err error) string {
	if err == nil {
		return formatTraderCreationError("机器人配置虽然保存了，但运行实例没有成功初始化", "请检查模型、策略和交易所配置是否完整，然后再试一次")
	}

	reason := humanizeTraderSetupReason(SanitizeError(err, ""))
	if reason == "" {
		return formatTraderCreationError(
			fmt.Sprintf("机器人「%s」在初始化运行实例时没有成功启动", traderName),
			"请检查模型、策略和交易所配置是否完整，然后再试一次",
		)
	}

	return formatTraderCreationError(
		fmt.Sprintf("机器人「%s」在初始化运行实例时没有成功启动，原因是：%s", traderName, reason),
		"请检查模型、策略和交易所配置是否完整，然后再试一次",
	)
}

func describeTraderCreationWarning(traderName string, err error) string {
	if err == nil {
		return fmt.Sprintf("机器人「%s」已经保存，但当前还没有通过启动前校验。请先检查模型、策略和交易所配置，修正后再点击启动。", traderName)
	}

	reason := humanizeTraderSetupReason(SanitizeError(err, ""))
	if reason == "" {
		return fmt.Sprintf("机器人「%s」已经保存，但当前暂时还不能启动。请先检查模型、策略和交易所配置，修正后再点击启动。", traderName)
	}

	return fmt.Sprintf("机器人「%s」已经保存，但当前暂时还不能启动，原因是：%s。请先检查模型、策略和交易所配置，修正后再点击启动。", traderName, reason)
}

func describeTraderStartError(traderName string, err error) string {
	if err == nil {
		return fmt.Sprintf("这次未能启动机器人：机器人「%s」暂时还不能启动。请检查模型、策略和交易所配置后，再重新点击启动。", traderName)
	}

	reason := humanizeTraderSetupReason(SanitizeError(err, ""))
	if reason == "" {
		return fmt.Sprintf("这次未能启动机器人：机器人「%s」暂时还不能启动。请检查模型、策略和交易所配置后，再重新点击启动。", traderName)
	}

	return fmt.Sprintf("这次未能启动机器人：机器人「%s」暂时还不能启动，原因是：%s。请检查模型、策略和交易所配置后，再重新点击启动。", traderName, reason)
}

func formatTraderStartError(reason, nextStep string) string {
	if nextStep == "" {
		return fmt.Sprintf("这次未能启动机器人：%s。", reason)
	}
	return fmt.Sprintf("这次未能启动机器人：%s。%s。", reason, nextStep)
}

// validateNTAccountBoundForStart blocks starting a NinjaTrader trader that has no
// bound account. Multi-account safety: without an account the trader's orders are
// refused at submit (tcp_trader.go placeEntry — no fallback to the shared active
// account), so it could never trade and would confuse the display. Returns
// ("","",nil) for any non-NinjaTrader exchange or a non-empty account.
func validateNTAccountBoundForStart(fullCfg *store.TraderFullConfig, traderName string) (string, string, map[string]string) {
	if fullCfg == nil || fullCfg.Exchange == nil || fullCfg.Exchange.ExchangeType != "ninjatrader" {
		return "", "", nil
	}
	if fullCfg.Trader == nil || strings.TrimSpace(fullCfg.Trader.Account) != "" {
		return "", "", nil
	}
	return formatTraderStartError(
		fmt.Sprintf("机器人「%s」还没有绑定交易账户", traderName),
		"请先在仪表盘为它选择一个 SIM 账户，然后再点击启动 (select an account first)",
	), "trader.start.no_account", mapStringPairs("trader_name", traderName)
}

// handleCreateTrader Create new AI trader
func (s *Server) handleCreateTrader(c *gin.Context) {
	userID := c.GetString("user_id")
	var req CreateTraderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequestWithDetails(c, traderCreationRequestError("提交的信息不完整，或者格式不正确"), "trader.create.invalid_request", nil)
		return
	}

	// (crypto symbol-format validation removed with the crypto venues —
	// futures symbols like MNQ come from the exchange row's instrument name.)

	model, err := s.store.AIModel().Get(userID, req.AIModelID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			SafeBadRequestWithDetails(c, formatTraderCreationError("还没有找到你选择的 AI 模型", "请前往「设置 > 模型配置」先添加并启用一个可用模型，再回来创建机器人"), "trader.create.model_not_found", nil)
			return
		}
		SafeError(c, http.StatusInternalServerError,
			formatTraderCreationError("暂时无法读取你的 AI 模型配置", "请稍后重试；如果问题持续，再检查本地服务是否正常"),
			err,
		)
		return
	}
	if !model.Enabled {
		SafeBadRequestWithDetails(c, formatTraderCreationError(
			fmt.Sprintf("AI 模型「%s」目前还没有启用", model.Name),
			"请前往「设置 > 模型配置」启用它后，再重新创建机器人",
		), "trader.create.model_disabled", mapStringPairs("model_name", model.Name))
		return
	}
	if model.APIKey == "" {
		SafeBadRequestWithDetails(c, formatTraderCreationError(
			fmt.Sprintf("AI 模型「%s」缺少 API Key 或支付凭证", model.Name),
			"请前往「设置 > 模型配置」补全模型凭证后，再重新创建机器人",
		), "trader.create.model_missing_credentials", mapStringPairs("model_name", model.Name))
		return
	}

	if req.StrategyID == "" {
		SafeBadRequestWithDetails(c, formatTraderCreationError("你还没有选择交易策略", "请先选择一个策略，再继续创建机器人"), "trader.create.strategy_required", nil)
		return
	}

	if req.StrategyID != "" {
		_, err = s.store.Strategy().Get(userID, req.StrategyID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				SafeBadRequestWithDetails(c, formatTraderCreationError("你选择的策略不存在，或者已经被删除了", "请重新选择一个可用策略后，再继续创建机器人"), "trader.create.strategy_not_found", nil)
				return
			}
			SafeError(c, http.StatusInternalServerError,
				formatTraderCreationError("暂时无法读取你选择的策略配置", "请稍后重试；如果问题持续，再检查本地服务是否正常"),
				err,
			)
			return
		}
	}

	// Generate trader ID (use short UUID prefix for readability)
	exchangeIDShort := req.ExchangeID
	if len(exchangeIDShort) > 8 {
		exchangeIDShort = exchangeIDShort[:8]
	}
	traderID := fmt.Sprintf("%s_%s_%d", exchangeIDShort, req.AIModelID, time.Now().Unix())

	// Set default values
	isCrossMargin := true // Default to cross margin mode
	if req.IsCrossMargin != nil {
		isCrossMargin = *req.IsCrossMargin
	}

	showInCompetition := true // Default to show in competition
	if req.ShowInCompetition != nil {
		showInCompetition = *req.ShowInCompetition
	}

	// Set system prompt template default value
	systemPromptTemplate := "default"
	if req.SystemPromptTemplate != "" {
		systemPromptTemplate = req.SystemPromptTemplate
	}

	// Set scan interval default value. Minimum lowered 3 → 1 (2026-06-09,
	// scalper request): any positive value is accepted; unset/invalid (<= 0)
	// still defaults to the recommended 3.
	scanIntervalMinutes := req.ScanIntervalMinutes
	if scanIntervalMinutes <= 0 {
		scanIntervalMinutes = 3 // unset → the recommended default
	}

	// Query exchange actual balance, override user input
	actualBalance := req.InitialBalance // Default to use user input
	exchanges, err := s.store.Exchange().List(userID)
	if err != nil {
		SafeError(c, http.StatusInternalServerError,
			formatTraderCreationError("暂时无法读取你的交易所配置", "请稍后重试；如果问题持续，再检查本地服务是否正常"),
			err,
		)
		return
	}

	// Find matching exchange configuration
	var exchangeCfg *store.Exchange
	for _, ex := range exchanges {
		if ex.ID == req.ExchangeID {
			exchangeCfg = ex
			break
		}
	}

	if exchangeMsg, exchangeErrorKey, exchangeErrorParams := validateExchangeForTraderCreation(exchangeCfg); exchangeMsg != "" {
		SafeBadRequestWithDetails(c, exchangeMsg, exchangeErrorKey, exchangeErrorParams)
		return
	}

	{
		tempTrader, createErr := buildExchangeProbeTrader(exchangeCfg, userID)
		if createErr != nil {
			SafeBadRequestWithDetails(c, formatTraderCreationError(
				fmt.Sprintf("交易所账户「%s」没有通过初始化校验，原因是：%s", exchangeDisplayName(exchangeCfg), humanizeTraderSetupReason(SanitizeError(createErr, "配置校验未通过"))),
				"请前往「设置 > 交易所配置」检查这个账户的密钥、地址和账户信息是否填写正确",
			), "trader.create.exchange_probe_failed", traderSetupReasonParams(createErr, "配置校验未通过",
				"exchange_name", exchangeDisplayName(exchangeCfg),
			))
			return
		} else if tempTrader != nil {
			// Query actual balance
			balanceInfo, balanceErr := tempTrader.GetBalance()
			if balanceErr != nil {
				logger.Infof("⚠️ Failed to query exchange balance, using user input for initial balance: %v", balanceErr)
			} else {
				if extractedBalance, found := extractExchangeTotalEquity(balanceInfo); found {
					actualBalance = extractedBalance
					logger.Infof("✓ Queried exchange total equity: %.2f %s (user input: %.2f)",
						actualBalance, accountAssetForExchange(exchangeCfg.ExchangeType), req.InitialBalance)
				} else {
					logger.Infof("⚠️ Unable to extract total equity from balance info, balanceInfo=%v, using user input for initial balance", balanceInfo)
				}
			}
		}
	}

	// Create trader configuration (database entity)
	logger.Infof("🔧 DEBUG: Starting to create trader config, ID=%s, Name=%s, AIModel=%s, Exchange=%s, StrategyID=%s", traderID, req.Name, req.AIModelID, req.ExchangeID, req.StrategyID)
	traderRecord := &store.Trader{
		ID:                   traderID,
		UserID:               userID,
		Name:                 req.Name,
		AIModelID:            req.AIModelID,
		ExchangeID:           req.ExchangeID,
		StrategyID:           req.StrategyID, // Associated strategy ID (new version)
		InitialBalance:       actualBalance,  // Use actual queried balance
		TradingSymbols:       req.TradingSymbols,
		CustomPrompt:         req.CustomPrompt,
		OverrideBasePrompt:   req.OverrideBasePrompt,
		SystemPromptTemplate: systemPromptTemplate,
		IsCrossMargin:        isCrossMargin,
		ShowInCompetition:    showInCompetition,
		ScanIntervalMinutes:  scanIntervalMinutes,
		CadenceMode:          req.CadenceMode,  // P10 (store validates the enum)
		PositionMode:         req.PositionMode, // Phase 3 (store validates the enum)
		IsRunning:            false,
	}

	// Save to database
	logger.Infof("🔧 DEBUG: Preparing to call CreateTrader")
	err = s.store.Trader().Create(traderRecord)
	if err != nil {
		logger.Infof("❌ Failed to create trader: %v", err)
		publicMsg := SanitizeError(err, formatTraderCreationError("机器人配置没有保存成功", "请检查名称、模型、策略和交易所配置后，再试一次"))
		statusCode := http.StatusBadRequest
		if publicMsg == formatTraderCreationError("机器人配置没有保存成功", "请检查名称、模型、策略和交易所配置后，再试一次") {
			statusCode = http.StatusInternalServerError
		}
		SafeError(c, statusCode, publicMsg, err)
		return
	}
	logger.Infof("🔧 DEBUG: CreateTrader succeeded")

	// Immediately load new trader into TraderManager
	logger.Infof("🔧 DEBUG: Preparing to call LoadUserTraders")
	startupWarning := ""
	err = s.traderManager.LoadUserTradersFromStore(s.store, userID)
	if err != nil {
		logger.Infof("⚠️ Failed to load user traders into memory: %v", err)
		startupWarning = describeTraderCreationWarning(req.Name, err)
	}
	logger.Infof("🔧 DEBUG: LoadUserTraders completed")

	if startupWarning == "" {
		if loadErr := s.traderManager.GetLoadError(traderID); loadErr != nil {
			logger.Infof("⚠️ Trader %s failed to load after creation: %v", traderID, loadErr)
			startupWarning = describeTraderCreationWarning(req.Name, loadErr)
		}
	}

	if startupWarning == "" {
		if _, getErr := s.traderManager.GetTrader(traderID); getErr != nil {
			logger.Infof("⚠️ Trader %s not found in memory after creation: %v", traderID, getErr)
			startupWarning = describeTraderCreationWarning(req.Name, getErr)
		}
	}

	logger.Infof("✓ Trader created successfully: %s (model: %s, exchange: %s)", req.Name, req.AIModelID, req.ExchangeID)

	c.JSON(http.StatusCreated, gin.H{
		"trader_id":       traderID,
		"trader_name":     req.Name,
		"ai_model":        req.AIModelID,
		"is_running":      false,
		"startup_warning": startupWarning,
	})
}

// handleUpdateTrader Update trader configuration
func (s *Server) handleUpdateTrader(c *gin.Context) {
	userID := c.GetString("user_id")
	traderID := c.Param("id")

	var req UpdateTraderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}

	// Check if trader exists and belongs to current user
	traders, err := s.store.Trader().List(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get trader list"})
		return
	}

	var existingTrader *store.Trader
	for _, t := range traders {
		if t.ID == traderID {
			existingTrader = t
			break
		}
	}

	if existingTrader == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Trader does not exist"})
		return
	}

	// Set default values
	isCrossMargin := existingTrader.IsCrossMargin // Keep original value
	if req.IsCrossMargin != nil {
		isCrossMargin = *req.IsCrossMargin
	}

	showInCompetition := existingTrader.ShowInCompetition // Keep original value
	if req.ShowInCompetition != nil {
		showInCompetition = *req.ShowInCompetition
	}

	// Set scan interval, allow updates
	scanIntervalMinutes := req.ScanIntervalMinutes
	logger.Infof("📊 Update trader scan_interval: req=%d, existing=%d", req.ScanIntervalMinutes, existingTrader.ScanIntervalMinutes)
	if scanIntervalMinutes <= 0 {
		scanIntervalMinutes = existingTrader.ScanIntervalMinutes // Keep original value
	}
	// Minimum lowered 3 → 1 (2026-06-09): any positive value is accepted.
	logger.Infof("📊 Final scan_interval_minutes: %d", scanIntervalMinutes)

	// P10 — cadence mode: explicit values only; empty keeps the existing mode.
	cadenceMode := existingTrader.CadenceMode
	if req.CadenceMode == "interval" || req.CadenceMode == "bar_close" {
		cadenceMode = req.CadenceMode
	}

	// Phase 3 — position mode: explicit values only; empty keeps the existing mode.
	positionMode := existingTrader.PositionMode
	if req.PositionMode == "ai_watch" || req.PositionMode == "bracket_only" {
		positionMode = req.PositionMode
	}

	// Set system prompt template
	systemPromptTemplate := req.SystemPromptTemplate
	if systemPromptTemplate == "" {
		systemPromptTemplate = existingTrader.SystemPromptTemplate // Keep original value
	}

	// Handle strategy ID (if not provided, keep original value)
	strategyID := req.StrategyID
	if strategyID == "" {
		strategyID = existingTrader.StrategyID
	}

	exchangeChanged := req.ExchangeID != "" && req.ExchangeID != existingTrader.ExchangeID
	resetInitialBalance := exchangeChanged && req.InitialBalance <= 0

	initialBalance := existingTrader.InitialBalance
	if req.InitialBalance > 0 {
		initialBalance = req.InitialBalance
	}
	if resetInitialBalance {
		initialBalance = 0
	}

	// Update trader configuration
	traderRecord := &store.Trader{
		ID:             traderID,
		UserID:         userID,
		Name:           req.Name,
		AIModelID:      req.AIModelID,
		ExchangeID:     req.ExchangeID,
		StrategyID:     strategyID, // Associated strategy ID
		InitialBalance: initialBalance,
		// P0 CAPS (CTO ruling 10-01 10:5x): these columns are live futures
		// risk caps in the crypto-named storage — preserve the stored values
		// on every update (C1: never write 0 over the owner's configured caps).
		BTCETHLeverage:       existingTrader.BTCETHLeverage,
		AltcoinLeverage:      existingTrader.AltcoinLeverage,
		TradingSymbols:       req.TradingSymbols,
		CustomPrompt:         req.CustomPrompt,
		OverrideBasePrompt:   req.OverrideBasePrompt,
		SystemPromptTemplate: systemPromptTemplate,
		IsCrossMargin:        isCrossMargin,
		ShowInCompetition:    showInCompetition,
		ScanIntervalMinutes:  scanIntervalMinutes,
		CadenceMode:          cadenceMode,              // P10
		PositionMode:         positionMode,             // Phase 3
		IsRunning:            existingTrader.IsRunning, // Keep original value
	}

	// Check if trader was running before update (we'll restart it after)
	wasRunning := false
	if existingMemTrader, memErr := s.traderManager.GetTrader(traderID); memErr == nil {
		status := existingMemTrader.GetStatus()
		if running, ok := status["is_running"].(bool); ok && running {
			wasRunning = true
			logger.Infof("🔄 Trader %s was running, will restart with new config after update", traderID)
		}
	}

	// Update database
	logger.Infof("🔄 Updating trader: ID=%s, Name=%s, AIModelID=%s, StrategyID=%s, ScanInterval=%d min",
		traderRecord.ID, traderRecord.Name, traderRecord.AIModelID, traderRecord.StrategyID, scanIntervalMinutes)
	err = s.store.Trader().Update(traderRecord)
	if err != nil {
		SafeInternalError(c, "Failed to update trader", err)
		return
	}

	if resetInitialBalance {
		logger.Infof("🔄 Exchange changed for trader %s, resetting stale initial_balance to 0", traderID)
		if err := s.store.Trader().UpdateInitialBalance(userID, traderID, 0); err != nil {
			SafeInternalError(c, "Failed to reset trader initial balance", err)
			return
		}
	}

	// Remove old trader from memory first (this also stops if running)
	s.traderManager.RemoveTrader(traderID)

	// Reload traders into memory with fresh config
	err = s.traderManager.LoadUserTradersFromStore(s.store, userID)
	if err != nil {
		logger.Infof("⚠️ Failed to reload user traders into memory: %v", err)
	}

	// If trader was running before, restart it with new config
	if wasRunning {
		if reloadedTrader, getErr := s.traderManager.GetTrader(traderID); getErr == nil {
			safe.GoNet("trader-restart", traderID, func() {
				logger.Infof("▶️ Restarting trader %s with new config...", traderID)
				if runErr := reloadedTrader.Run(); runErr != nil {
					logger.Infof("❌ Trader %s runtime error: %v", traderID, runErr)
				}
			})
		}
	}

	logger.Infof("✓ Trader updated successfully: %s (model: %s, exchange: %s, strategy: %s)", req.Name, req.AIModelID, req.ExchangeID, strategyID)

	c.JSON(http.StatusOK, gin.H{
		"trader_id":   traderID,
		"trader_name": req.Name,
		"ai_model":    req.AIModelID,
		"message":     "Trader updated successfully",
	})
}

// handleDeleteTrader Delete trader
func (s *Server) handleDeleteTrader(c *gin.Context) {
	userID := c.GetString("user_id")
	traderID := c.Param("id")

	// Delete from database
	err := s.store.Trader().Delete(userID, traderID)
	if err != nil {
		SafeInternalError(c, "Failed to delete trader", err)
		return
	}

	// If trader is running, stop it first
	if trader, err := s.traderManager.GetTrader(traderID); err == nil {
		status := trader.GetStatus()
		if isRunning, ok := status["is_running"].(bool); ok && isRunning {
			trader.Stop()
			logger.Infof("⏹  Stopped running trader: %s", traderID)
		}
	}

	// Remove trader from memory
	s.traderManager.RemoveTrader(traderID)

	logger.Infof("✓ Trader deleted: %s", traderID)
	c.JSON(http.StatusOK, gin.H{"message": "Trader deleted"})
}

// handleStartTrader Start trader
func (s *Server) handleStartTrader(c *gin.Context) {
	userID := c.GetString("user_id")
	traderID := c.Param("id")

	// Verify trader belongs to current user
	fullCfg, err := s.store.Trader().GetFullConfig(userID, traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Trader does not exist or no access permission"})
		return
	}
	traderName := traderID
	if fullCfg != nil && fullCfg.Trader != nil && fullCfg.Trader.Name != "" {
		traderName = fullCfg.Trader.Name
	}

	// NT8 MULTI-ACCOUNT SAFETY: refuse to start a NinjaTrader trader with no bound
	// account (its orders are refused at submit — no fallback to the shared active
	// account — so it could never trade AND would confuse the display).
	if msg, code, params := validateNTAccountBoundForStart(fullCfg, traderName); msg != "" {
		SafeBadRequestWithDetails(c, msg, code, params)
		return
	}

	// REL18-C O3b — one trader per (symbol, account): refuse to start when
	// ANOTHER RUNNING trader already holds the same (symbol, account). This is
	// the fail-closed mirror of the ordered-execution owner conflict (O2), and
	// it fires BEFORE any load/start so the DB is never marked running.
	if fullCfg != nil && fullCfg.Exchange != nil && fullCfg.Trader != nil {
		symbol := strings.TrimSpace(fullCfg.Exchange.NTInstrumentName)
		account := strings.TrimSpace(fullCfg.Trader.Account)
		if otherID, otherName, ok := s.runningTraderOnSameSymbolAccount(userID, traderID, symbol, account); ok {
			telemetry.IncGateBlock(traderID, "trader_start_duplicate_symbol_account")
			SafeConflictWithDetails(c,
				fmt.Sprintf("Trader %q is already running on %s / %s — stop it before starting %q on the same (symbol, account).",
					otherName, symbol, account, traderName),
				"trader.start.duplicate_symbol_account",
				mapStringPairs("trader_name", traderName, "other_trader", otherName, "other_trader_id", otherID, "symbol", symbol, "account", account))
			return
		}
	}

	// Check if trader exists in memory and if it's running
	existingTrader, _ := s.traderManager.GetTrader(traderID)
	if existingTrader != nil {
		status := existingTrader.GetStatus()
		if isRunning, ok := status["is_running"].(bool); ok && isRunning {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Trader is already running"})
			return
		}
		// Trader exists but is stopped - remove from memory to reload fresh config
		logger.Infof("🔄 Removing stopped trader %s from memory to reload config...", traderID)
		s.traderManager.RemoveTrader(traderID)
	}

	// Load trader from database (always reload to get latest config)
	logger.Infof("🔄 Loading trader %s from database...", traderID)
	if loadErr := s.traderManager.LoadUserTradersFromStore(s.store, userID); loadErr != nil {
		logger.Infof("❌ Failed to load user traders: %v", loadErr)
		SafeErrorWithDetails(c, http.StatusInternalServerError, describeTraderStartError(traderName, loadErr), "trader.start.load_failed", traderSetupReasonParams(loadErr, "", "trader_name", traderName), loadErr)
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		if fullCfg != nil && fullCfg.Trader != nil {
			// Check strategy
			if fullCfg.Strategy == nil {
				SafeBadRequestWithDetails(c, describeTraderStartError(traderName, fmt.Errorf("trader has no strategy configured")), "trader.start.strategy_missing", mapStringPairs("trader_name", traderName))
				return
			}
			// Check AI model
			if fullCfg.AIModel == nil {
				SafeBadRequestWithDetails(c, formatTraderStartError("这个机器人关联的 AI 模型不存在", "请前往「设置 > 模型配置」检查后，再重新点击启动"), "trader.start.model_not_found", mapStringPairs("trader_name", traderName))
				return
			}
			if !fullCfg.AIModel.Enabled {
				SafeBadRequestWithDetails(c, formatTraderStartError(
					fmt.Sprintf("机器人「%s」关联的 AI 模型「%s」目前还没有启用", traderName, fullCfg.AIModel.Name),
					"请前往「设置 > 模型配置」启用它后，再重新点击启动",
				), "trader.start.model_disabled", mapStringPairs("trader_name", traderName, "model_name", fullCfg.AIModel.Name))
				return
			}
			// Check exchange
			if fullCfg.Exchange == nil {
				SafeBadRequestWithDetails(c, formatTraderStartError("这个机器人关联的交易所账户不存在", "请前往「设置 > 交易所配置」检查后，再重新点击启动"), "trader.start.exchange_not_found", mapStringPairs("trader_name", traderName))
				return
			}
			if !fullCfg.Exchange.Enabled {
				SafeBadRequestWithDetails(c, formatTraderStartError(
					fmt.Sprintf("机器人「%s」关联的交易所账户「%s」目前还没有启用", traderName, exchangeDisplayName(fullCfg.Exchange)),
					"请前往「设置 > 交易所配置」启用它后，再重新点击启动",
				), "trader.start.exchange_disabled", mapStringPairs("trader_name", traderName, "exchange_name", exchangeDisplayName(fullCfg.Exchange)))
				return
			}
		}
		// Check if there's a specific load error
		if loadErr := s.traderManager.GetLoadError(traderID); loadErr != nil {
			SafeBadRequestWithDetails(c, describeTraderStartError(traderName, loadErr), "trader.start.load_failed", traderSetupReasonParams(loadErr, "", "trader_name", traderName))
			return
		}
		SafeBadRequestWithDetails(c, describeTraderStartError(traderName, err), "trader.start.setup_invalid", traderSetupReasonParams(err, "", "trader_name", traderName))
		return
	}

	// REL18-C FOLD — the ordered-install verdict handshake: Run reports nil once
	// the ordered execution is installed, or the O2 refusal error. Read the
	// channel BEFORE spawning so both share it; no sleeps-as-sync.
	ready := trader.RunReadyChan()

	// Mark running BEFORE spawning Run so any later self-refusal (O2 ordered
	// owner, grid-init error, load error) writes AFTER and always wins the DB
	// bit — a refused start must never read as running.
	if err := s.store.Trader().UpdateStatus(userID, traderID, true); err != nil {
		logger.Warnf("⚠️  Failed to mark trader %s running before start: %v", trader.GetName(), err)
	}

	// Start trader
	safe.GoNet("trader-start", traderID, func() {
		logger.Infof("▶️  Starting trader %s (%s)", traderID, trader.GetName())
		if err := trader.Run(); err != nil {
			// FIX-RUNNING-FLAG: a duplicate self-start (already running in
			// memory) is a NO-OP, not a runtime error.
			if errors.Is(err, traderpkg.ErrTraderAlreadyRunning) {
				logger.Infof("🔄 Trader %s is already running — duplicate start ignored", trader.GetName())
				return
			}
			logger.Infof("❌ Trader %s runtime error: %v", trader.GetName(), err)
		}
	})

	// Keep the response honest: if Run's ordered install was refused before the
	// handler returns, answer 409, not "Trader started". A bounded wait only —
	// if Run has not reached the install yet, the start is assumed to proceed
	// and the self-refusal above still corrects the DB bit asynchronously.
	select {
	case rerr := <-ready:
		if rerr != nil {
			SafeConflictWithDetails(c,
				fmt.Sprintf("Start refused: %s", rerr.Error()),
				"trader.start.ordered_owner_conflict",
				mapStringPairs("trader_name", traderName, "trader_id", traderID))
			return
		}
	case <-time.After(traderStartReadyTimeout):
	}

	logger.Infof("✓ Trader %s started", trader.GetName())
	c.JSON(http.StatusOK, gin.H{"message": "Trader started"})
}

// handleStopTrader Stop trader
func (s *Server) handleStopTrader(c *gin.Context) {
	userID := c.GetString("user_id")
	traderID := c.Param("id")

	// Verify trader belongs to current user
	_, err := s.store.Trader().GetFullConfig(userID, traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Trader does not exist or no access permission"})
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Trader does not exist"})
		return
	}

	// Check if trader is running
	status := trader.GetStatus()
	if isRunning, ok := status["is_running"].(bool); ok && !isRunning {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Trader is already stopped"})
		return
	}

	// Stop trader
	trader.Stop()

	// Update running status in database
	err = s.store.Trader().UpdateStatus(userID, traderID, false)
	if err != nil {
		logger.Infof("⚠️  Failed to update trader status: %v", err)
	}

	logger.Infof("⏹  Trader %s stopped", trader.GetName())
	c.JSON(http.StatusOK, gin.H{"message": "Trader stopped"})
}

// ── P2 (ledger-close 2026-08-19) — owner pause control (stop_until producer).
// POST /traders/:id/pause  body: {"minutes":30} | {"until_ct":"HH:MM"} |
// {"until":"session_end"} — exactly one. POST /traders/:id/resume clears it.
// Semantics: NEW entries refused while paused; closes, EOD-flat, brackets and
// the 60s monitor guards continue (see trader/auto_trader_pause.go).

func (s *Server) handlePauseTrader(c *gin.Context) {
	userID := c.GetString("user_id")
	traderID := c.Param("id")
	if _, err := s.store.Trader().GetFullConfig(userID, traderID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Trader does not exist or no access permission"})
		return
	}
	tr, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Trader does not exist"})
		return
	}

	var body struct {
		Minutes int    `json:"minutes"`
		UntilCT string `json:"until_ct"`
		Until   string `json:"until"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body: " + err.Error()})
		return
	}

	now := time.Now()
	var until time.Time
	switch {
	case body.Minutes > 0:
		until = now.Add(time.Duration(body.Minutes) * time.Minute)
	case body.Until == "session_end":
		end, ok := tr.SessionEndTime(now)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no active session to pause until (overnight/interim)"})
			return
		}
		until = end
	case body.UntilCT != "":
		// "HH:MM" CT today; a time at/before now means tomorrow (wrap-safe).
		// Parsed by hand — the TZ-guard forbids bare time layouts outside
		// kernel/tz.go, deservedly. E7-v2: STRICT parse (Sscanf alone accepted
		// trailing garbage like "12:34xyz") and the tomorrow-wrap reconstructs
		// the wall-clock via time.Date day+1 — Add(24h) lands one wall hour off
		// across a DST transition.
		var hh, mm int
		var tail string
		n, err := fmt.Sscanf(body.UntilCT, "%d:%d%s", &hh, &mm, &tail)
		if !(err != nil && n == 2) || tail != "" || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "until_ct must be HH:MM (CT)"})
			return
		}
		ct := now.In(kernel.CTLocation())
		until = time.Date(ct.Year(), ct.Month(), ct.Day(), hh, mm, 0, 0, kernel.CTLocation())
		if !until.After(now) {
			until = time.Date(ct.Year(), ct.Month(), ct.Day()+1, hh, mm, 0, 0, kernel.CTLocation())
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "provide one of: minutes>0, until_ct:\"HH:MM\", until:\"session_end\""})
		return
	}

	if err := tr.PauseEntriesUntil(until, "owner"); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":        "paused",
		"pause_until":    until.Format(time.RFC3339),
		"pause_until_ct": kernel.FormatCT(until),
	})
}

func (s *Server) handleResumeTrader(c *gin.Context) {
	userID := c.GetString("user_id")
	traderID := c.Param("id")
	if _, err := s.store.Trader().GetFullConfig(userID, traderID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Trader does not exist or no access permission"})
		return
	}
	tr, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Trader does not exist"})
		return
	}
	tr.ResumeEntries("owner")
	c.JSON(http.StatusOK, gin.H{"message": "resumed"})
}
