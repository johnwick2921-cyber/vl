package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"vl/kernel"
	"vl/logger"
	"vl/market"
	"vl/mcp"
	_ "vl/mcp/provider"
	"vl/store"
	"vl/trader"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// validateStrategyConfig validates strategy configuration and returns warnings
func validateStrategyConfig(config *store.StrategyConfig) []string {
	var warnings []string
	if config.StrategyType == "grid_trading" {
		return warnings
	}

	return warnings
}

func attachPublishConfig(config *store.StrategyConfig, strategy *store.Strategy) {
	if config == nil || strategy == nil {
		return
	}
	config.ClampLimits()
	config.PublishConfig = &store.PublishStrategyConfig{
		IsPublic:      strategy.IsPublic,
		ConfigVisible: strategy.ConfigVisible,
	}
}

// handleEstimateTokens estimates token usage for a strategy config (no auth required, pure computation)
func (s *Server) handleEstimateTokens(c *gin.Context) {
	var req struct {
		Config store.StrategyConfig `json:"config" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}

	estimate := req.Config.EstimateTokens()
	c.JSON(http.StatusOK, estimate)
}

// handlePublicStrategies Get public strategies for strategy market (no auth required)
func (s *Server) handlePublicStrategies(c *gin.Context) {
	strategies, err := s.store.Strategy().ListPublic()
	if err != nil {
		SafeInternalError(c, "Failed to get public strategies", err)
		return
	}

	// Convert to frontend format with visibility control
	result := make([]gin.H, 0, len(strategies))
	for _, st := range strategies {
		item := gin.H{
			"id":             st.ID,
			"name":           st.Name,
			"description":    st.Description,
			"author_email":   "", // Will be filled if we have user info
			"is_public":      st.IsPublic,
			"config_visible": st.ConfigVisible,
			"created_at":     st.CreatedAt,
			"updated_at":     st.UpdatedAt,
		}

		// Only include config if config_visible is true
		if st.ConfigVisible {
			var config store.StrategyConfig
			json.Unmarshal([]byte(st.Config), &config)
			attachPublishConfig(&config, st)
			item["config"] = config
		}

		result = append(result, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"strategies": result,
	})
}

// handleGetStrategies Get strategy list
func (s *Server) handleGetStrategies(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	strategies, err := s.store.Strategy().List(userID)
	if err != nil {
		SafeInternalError(c, "Failed to get strategy list", err)
		return
	}

	// Convert to frontend format
	result := make([]gin.H, 0, len(strategies))
	for _, st := range strategies {
		var config store.StrategyConfig
		json.Unmarshal([]byte(st.Config), &config)
		attachPublishConfig(&config, st)

		result = append(result, gin.H{
			"id":             st.ID,
			"name":           st.Name,
			"description":    st.Description,
			"is_active":      st.IsActive,
			"is_default":     st.IsDefault,
			"is_public":      st.IsPublic,
			"config_visible": st.ConfigVisible,
			"config":         config,
			"created_at":     st.CreatedAt,
			"updated_at":     st.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"strategies": result,
	})
}

// handleGetStrategy Get single strategy
func (s *Server) handleGetStrategy(c *gin.Context) {
	userID := c.GetString("user_id")
	strategyID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	strategy, err := s.store.Strategy().Get(userID, strategyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Strategy not found"})
		return
	}

	var config store.StrategyConfig
	json.Unmarshal([]byte(strategy.Config), &config)
	attachPublishConfig(&config, strategy)

	c.JSON(http.StatusOK, gin.H{
		"id":          strategy.ID,
		"name":        strategy.Name,
		"description": strategy.Description,
		"is_active":   strategy.IsActive,
		"is_default":  strategy.IsDefault,
		"config":      config,
		"created_at":  strategy.CreatedAt,
		"updated_at":  strategy.UpdatedAt,
	})
}

// handleCreateStrategy Create strategy.
// If "config" is omitted from the request body, the system default config is used automatically.
func (s *Server) handleCreateStrategy(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		Name          string                `json:"name" binding:"required"`
		Description   string                `json:"description"`
		Lang          string                `json:"lang"`   // "zh" or "en", used when config is omitted
		Config        *store.StrategyConfig `json:"config"` // optional — uses default if omitted
		IsPublic      bool                  `json:"is_public"`
		ConfigVisible bool                  `json:"config_visible"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}

	// Use default config when none provided
	if req.Config == nil {
		lang := req.Lang
		if lang == "" {
			lang = "zh"
		}
		defaultCfg := store.GetDefaultStrategyConfig(lang)
		req.Config = &defaultCfg
	}
	beforeClamp := *req.Config
	req.Config.ClampLimits()
	// F11c — reject absurd indicator periods (e.g. 9999) at save with a clear error,
	// never a silently-empty series.
	if err := req.Config.ValidateIndicatorPeriods(); err != nil {
		SafeBadRequest(c, err.Error())
		return
	}
	hadPublishConfig := req.Config.PublishConfig != nil
	isPublic := req.IsPublic
	configVisible := req.ConfigVisible
	if hadPublishConfig {
		isPublic = req.Config.PublishConfig.IsPublic
		configVisible = req.Config.PublishConfig.ConfigVisible
	}
	req.Config.PublishConfig = &store.PublishStrategyConfig{
		IsPublic:      isPublic,
		ConfigVisible: configVisible,
	}

	// Serialize configuration
	configJSON, err := json.Marshal(req.Config)
	if err != nil {
		SafeInternalError(c, "Serialize configuration", err)
		return
	}

	strategy := &store.Strategy{
		ID:          uuid.New().String(),
		UserID:      userID,
		Name:        req.Name,
		Description: req.Description,
		IsActive:    false,
		IsDefault:   false,
		IsPublic:    isPublic,
		// Existing default is true; keep that behavior when no explicit publish config is sent.
		ConfigVisible: configVisible || !hadPublishConfig,
		Config:        string(configJSON),
	}

	// W1 (settings truth) — the row AND the record of which knobs this save
	// holds as an explicit 0 (so the load-time conversion check knows the 0 is
	// the owner's) are ONE transaction: no row is created without its record.
	if err := s.store.Strategy().CreateWithExplicitZeros(strategy, req.Config); err != nil {
		SafeInternalError(c, "Failed to create strategy", err)
		return
	}

	// Validate configuration and collect warnings
	warnings := validateStrategyConfig(req.Config)
	warnings = append(warnings, store.StrategyClampWarnings(beforeClamp, *req.Config, req.Config.Language)...)

	response := gin.H{
		"id":      strategy.ID,
		"message": "Strategy created successfully",
	}
	if len(warnings) > 0 {
		response["warnings"] = warnings
	}

	c.JSON(http.StatusOK, response)
}

// handleUpdateStrategy Update strategy.
// The incoming config is merged with the existing one: top-level sections present in the
// request overwrite the corresponding existing sections; absent sections are preserved.
// This prevents partial updates from zeroing out unmentioned fields.
func (s *Server) handleUpdateStrategy(c *gin.Context) {
	userID := c.GetString("user_id")
	strategyID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// Check if it's a system default strategy
	existing, err := s.store.Strategy().Get(userID, strategyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Strategy not found"})
		return
	}
	if existing.IsDefault {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot modify system default strategy"})
		return
	}

	var req struct {
		Name          string          `json:"name"`
		Description   string          `json:"description"`
		Config        json.RawMessage `json:"config"` // raw JSON so we can merge
		IsPublic      bool            `json:"is_public"`
		ConfigVisible bool            `json:"config_visible"`
		// F11b — the FE sets this true only after the user confirms a strategy-type
		// switch dialog that names the ai_config (indicators/risk/prompt) that would
		// be lost. Absent/false → the AI config is preserved across the switch.
		ConfirmTypeSwitch bool `json:"confirm_type_switch"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}

	// Start with the existing config as base — preserves all unmentioned fields.
	var mergedConfig store.StrategyConfig
	if err := json.Unmarshal([]byte(existing.Config), &mergedConfig); err != nil {
		// If existing config is corrupt, start from zero
		mergedConfig = store.StrategyConfig{}
	}
	// F11b — snapshot the pre-patch config so a strategy-type switch can't destroy
	// the AI-config bundle unless the user explicitly confirmed the loss.
	baseConfig := mergedConfig

	// Apply incoming config on top while preserving nested fields that were not sent.
	if len(req.Config) > 0 && string(req.Config) != "null" {
		var patch map[string]any
		if err := json.Unmarshal(req.Config, &patch); err != nil {
			SafeBadRequest(c, "Invalid config JSON")
			return
		}
		mergedConfig, err = store.MergeStrategyConfig(mergedConfig, patch)
		if err != nil {
			SafeBadRequest(c, "Invalid config JSON")
			return
		}
	}
	// F11b — preserve the AI config across an unconfirmed strategy-type switch.
	if mergedConfig.StrategyType != baseConfig.StrategyType && !req.ConfirmTypeSwitch {
		mergedConfig = store.PreserveAIConfigOnTypeSwitch(baseConfig, mergedConfig, false)
		logger.Warnf("🛡️ strategy %s: preserved ai_config across %q→%q type switch (no confirm_type_switch flag)", strategyID, baseConfig.StrategyType, mergedConfig.StrategyType)
	}
	beforeClamp := mergedConfig
	mergedConfig.ClampLimits()
	// F11c — reject absurd indicator periods (e.g. 9999) at save with a clear error,
	// never a silently-empty series.
	if err := mergedConfig.ValidateIndicatorPeriods(); err != nil {
		SafeBadRequest(c, err.Error())
		return
	}

	// Preserve existing name/description when not supplied
	name := req.Name
	if name == "" {
		name = existing.Name
	}
	description := req.Description
	if description == "" {
		description = existing.Description
	}

	configJSON, err := json.Marshal(mergedConfig)
	if err != nil {
		SafeInternalError(c, "Serialize configuration", err)
		return
	}

	strategy := &store.Strategy{
		ID:            strategyID,
		UserID:        userID,
		Name:          name,
		Description:   description,
		Config:        string(configJSON),
		IsPublic:      req.IsPublic,
		ConfigVisible: req.ConfigVisible,
	}

	// W1 (settings truth) — the row AND the record of which knobs this save
	// holds as an explicit 0 are ONE transaction, committed BEFORE the reload
	// below, so the reloaded trader's conversion check reads the owner's 0 as
	// the owner's. If the record cannot be written the row is rolled back and
	// the save answers an error — the stored config is unchanged.
	if err := s.store.Strategy().UpdateWithExplicitZeros(strategy, &mergedConfig); err != nil {
		SafeInternalError(c, "Failed to update strategy", err)
		return
	}

	// REPAIR-PARSE E5 (2026-09-02) — NAME WHAT MOVED. The 2026-09-01 08:13 CT
	// save changed min_risk_reward_ratio 3 → 2 and reloaded the trader mid-NY
	// with nothing on any surface saying so; the audit had to infer it. Every
	// save now logs one line per RESOLVED knob that changed and persists the
	// diff. Purely observational — the reload itself is unchanged.
	s.logConfigDiff("studio_save", strategyID, baseConfig, mergedConfig)

	// Token overflow check — block save if all models exceed context limits
	if mergedConfig.StrategyType == "" || mergedConfig.StrategyType == "ai_trading" {
		estimate := mergedConfig.EstimateTokens()
		allExceed := true
		for _, ml := range estimate.ModelLimits {
			if ml.UsagePct <= 100 {
				allExceed = false
				break
			}
		}
		if allExceed && len(estimate.ModelLimits) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":          fmt.Sprintf("Estimated %d tokens exceeds all known model context limits. Reduce coins, timeframes, or K-line count.", estimate.Total),
				"token_estimate": estimate,
			})
			return
		}
	}

	// Validate merged configuration and collect warnings
	warnings := validateStrategyConfig(&mergedConfig)
	warnings = append(warnings, store.StrategyClampWarnings(beforeClamp, mergedConfig, mergedConfig.Language)...)

	// Reload the running trader(s) bound to this strategy so the saved config
	// takes effect on the NEXT decision cycle WITHOUT a manual restart — the same
	// mechanism the AI-model + Exchange save handlers use (RemoveTrader +
	// LoadUserTradersFromStore). The config was validated + clamped + persisted
	// above; the reload re-reads it from the store. RemoveTrader stops the
	// in-memory loop, but Stop() does NOT persist is_running=false, so the reload
	// auto-starts the trader (addTraderFromStore honors the DB is_running flag).
	// The TCP bridge + BarCache are a shared singleton — bars + the NT8 connection
	// are preserved; an open position is re-attached on reload (NT8-side SL/TP
	// guard it during the brief swap). On reload failure the request still
	// succeeds (the config is saved) and we log — we never crash the trader.
	if s.traderManager != nil {
		if traders, listErr := s.store.Trader().List(userID); listErr == nil {
			reloaded := make([]string, 0, 1)
			for _, t := range traders {
				if t.StrategyID == strategyID {
					logger.Infof("🔄 Strategy %s saved — removing trader %s from memory to reload with the new config", strategyID, t.ID)
					s.traderManager.RemoveTrader(t.ID)
					reloaded = append(reloaded, t.ID)
				}
			}
			if len(reloaded) > 0 {
				if rErr := s.traderManager.LoadUserTradersFromStore(s.store, userID); rErr != nil {
					logger.Infof("⚠️ Strategy %s saved but trader reload failed: %v (config persisted; restart to apply)", strategyID, rErr)
				} else {
					logger.Infof("✓ Strategy %s saved → reloaded %d running trader(s) to apply the new config immediately: %v", strategyID, len(reloaded), reloaded)
					// B1 (DS-104): re-print the mentor sources line when a save
					// arms (or re-arms) a mentor-mode trader — not only at boot.
					if line := trader.MentorSourcesReloadLine(s.traderManager.GetAllTraders(), reloaded); line != "" {
						logger.Infof("%s", line)
					}
				}
			}
		}
	}

	response := gin.H{"message": "Strategy updated successfully"}
	if len(warnings) > 0 {
		response["warnings"] = warnings
	}

	c.JSON(http.StatusOK, response)
}

// handleDeleteStrategy Delete strategy
func (s *Server) handleDeleteStrategy(c *gin.Context) {
	userID := c.GetString("user_id")
	strategyID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	if err := s.store.Strategy().Delete(userID, strategyID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": SanitizeError(err, "Failed to delete strategy")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Strategy deleted successfully"})
}

// handleActivateStrategy Activate strategy
func (s *Server) handleActivateStrategy(c *gin.Context) {
	userID := c.GetString("user_id")
	strategyID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	if err := s.store.Strategy().SetActive(userID, strategyID); err != nil {
		SafeInternalError(c, "Failed to activate strategy", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Strategy activated successfully"})
}

// handleDuplicateStrategy Duplicate strategy
func (s *Server) handleDuplicateStrategy(c *gin.Context) {
	userID := c.GetString("user_id")
	sourceID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		Name string `json:"name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}

	newID := uuid.New().String()
	if err := s.store.Strategy().Duplicate(userID, sourceID, newID, req.Name); err != nil {
		SafeInternalError(c, "Failed to duplicate strategy", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":      newID,
		"message": "Strategy duplicated successfully",
	})
}

// handleGetActiveStrategy Get currently active strategy
func (s *Server) handleGetActiveStrategy(c *gin.Context) {
	userID := c.GetString("user_id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	strategy, err := s.store.Strategy().GetActive(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No active strategy"})
		return
	}

	var config store.StrategyConfig
	json.Unmarshal([]byte(strategy.Config), &config)
	attachPublishConfig(&config, strategy)

	c.JSON(http.StatusOK, gin.H{
		"id":          strategy.ID,
		"name":        strategy.Name,
		"description": strategy.Description,
		"is_active":   strategy.IsActive,
		"is_default":  strategy.IsDefault,
		"config":      config,
		"created_at":  strategy.CreatedAt,
		"updated_at":  strategy.UpdatedAt,
	})
}

// handleGetDefaultStrategyConfig Get default strategy configuration template
func (s *Server) handleGetDefaultStrategyConfig(c *gin.Context) {
	// Get language from query parameter, default to "en"
	lang := c.Query("lang")
	if lang != "zh" {
		lang = "en"
	}

	// Return default configuration with i18n support
	defaultConfig := store.GetDefaultStrategyConfig(lang)
	c.JSON(http.StatusOK, defaultConfig)
}

// handleGetSupportedTimeframes returns the single source of truth for the
// Strategy Studio timeframe selector: the full set the engine can genuinely
// serve (store.SupportedTimeframes), plus the advisory soft-warn threshold.
// The frontend renders this instead of a hardcoded copy, so the menu can never
// drift from what the backend / NT8 AddOn actually streams.
func (s *Server) handleGetSupportedTimeframes(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"timeframes":      store.SupportedTimeframes,
		"soft_warn_above": store.SoftWarnTimeframesAbove,
		"max":             store.MaxTimeframes,
	})
}

// handlePreviewPrompt Preview prompt generated by strategy
func (s *Server) handlePreviewPrompt(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		Config        store.StrategyConfig `json:"config" binding:"required"`
		AccountEquity float64              `json:"account_equity"`
		PromptVariant string               `json:"prompt_variant"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}

	// Use default values
	if req.AccountEquity <= 0 {
		req.AccountEquity = 1000.0 // Default simulated account equity
	}
	if req.PromptVariant == "" {
		req.PromptVariant = "balanced"
	}

	// Create strategy engine to build prompt
	engine := kernel.NewStrategyEngine(&req.Config)

	// Build system prompt (using built-in method from strategy engine). Phase 3:
	// pass the strategy's symbol so a futures preview reflects the real instrument
	// (defaults to MNQ; ignored on the crypto path).
	previewSymbol := "MNQ"
	if len(req.Config.CoinSource.StaticCoins) > 0 && req.Config.CoinSource.StaticCoins[0] != "" {
		previewSymbol = req.Config.CoinSource.StaticCoins[0]
	}
	systemPrompt := engine.BuildSystemPrompt(
		req.AccountEquity,
		req.PromptVariant,
		previewSymbol,
	)

	c.JSON(http.StatusOK, gin.H{
		"system_prompt":  systemPrompt,
		"prompt_variant": req.PromptVariant,
		"config_summary": gin.H{
			"coin_source":   req.Config.CoinSource.SourceType,
			"primary_tf":    req.Config.Indicators.Klines.PrimaryTimeframe,
			"max_positions": req.Config.RiskControl.MaxPositions,
		},
	})
}

// handleStrategyTestRun AI test run (does not execute trades, only returns AI analysis results)
func (s *Server) handleStrategyTestRun(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		Config        store.StrategyConfig `json:"config" binding:"required"`
		PromptVariant string               `json:"prompt_variant"`
		AIModelID     string               `json:"ai_model_id"`
		RunRealAI     bool                 `json:"run_real_ai"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}

	if req.PromptVariant == "" {
		req.PromptVariant = "balanced"
	}

	// Create strategy engine to build prompt
	engine := kernel.NewStrategyEngine(&req.Config)

	// Get candidate coins
	candidates, err := engine.GetCandidateCoins()
	if err != nil {
		logger.Errorf("[API Error] Failed to get candidate coins: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":       "Failed to get candidate coins",
			"ai_response": "",
		})
		return
	}

	// Get timeframe configuration
	timeframes := req.Config.Indicators.Klines.SelectedTimeframes
	primaryTimeframe := req.Config.Indicators.Klines.PrimaryTimeframe
	klineCount := req.Config.Indicators.Klines.PrimaryCount

	// If no timeframes selected, use default values
	if len(timeframes) == 0 {
		// Backward compatibility: use primary and longer timeframes
		if primaryTimeframe != "" {
			timeframes = append(timeframes, primaryTimeframe)
		} else {
			timeframes = append(timeframes, "3m")
		}
		if req.Config.Indicators.Klines.LongerTimeframe != "" {
			timeframes = append(timeframes, req.Config.Indicators.Klines.LongerTimeframe)
		}
	}
	if primaryTimeframe == "" {
		primaryTimeframe = timeframes[0]
	}
	if klineCount <= 0 {
		klineCount = 30
	}

	fmt.Printf("📊 Using timeframes: %v, primary: %s, kline count: %d\n", timeframes, primaryTimeframe, klineCount)

	// Get real market data (using multiple timeframes)
	marketDataMap := make(map[string]*market.Data)
	for _, coin := range candidates {
		data, err := market.GetWithTimeframes(coin.Symbol, timeframes, primaryTimeframe, klineCount,
			market.IndicatorPeriods{
				EMA:  req.Config.Indicators.EMAPeriods,
				RSI:  req.Config.Indicators.RSIPeriods,
				ATR:  req.Config.Indicators.ATRPeriods,
				BOLL: req.Config.Indicators.BOLLPeriods,
			})
		if err != nil {
			// If getting data for a coin fails, log but continue
			fmt.Printf("⚠️  Failed to get market data for %s: %v\n", coin.Symbol, err)
			continue
		}
		marketDataMap[coin.Symbol] = data
	}

	// Build real context (for generating User Prompt)
	testContext := &kernel.Context{
		CurrentTime:    kernel.FormatCT(time.Now()),
		RuntimeMinutes: 0,
		CallCount:      1,
		Account: kernel.AccountInfo{
			TotalEquity:      1000.0,
			AvailableBalance: 1000.0,
			UnrealizedPnL:    0,
			TotalPnL:         0,
			TotalPnLPct:      0,
			MarginUsed:       0,
			MarginUsedPct:    0,
			PositionCount:    0,
		},
		Positions:      []kernel.PositionInfo{},
		CandidateCoins: candidates,
		PromptVariant:  req.PromptVariant,
		MarketDataMap:  marketDataMap,
	}

	// Build System Prompt (Phase 3 — pass the strategy's symbol; defaults to MNQ).
	previewSymbol := "MNQ"
	if len(req.Config.CoinSource.StaticCoins) > 0 && req.Config.CoinSource.StaticCoins[0] != "" {
		previewSymbol = req.Config.CoinSource.StaticCoins[0]
	}
	systemPrompt := engine.BuildSystemPrompt(1000.0, req.PromptVariant, previewSymbol)

	// Build User Prompt (using real market data)
	userPrompt := engine.BuildUserPrompt(testContext)

	// If requesting real AI call
	if req.RunRealAI && req.AIModelID != "" {
		aiResponse, aiErr := s.runRealAITest(userID, req.AIModelID, systemPrompt, userPrompt)
		if aiErr != nil {
			c.JSON(http.StatusOK, gin.H{
				"system_prompt":   systemPrompt,
				"user_prompt":     userPrompt,
				"candidate_count": len(candidates),
				"candidates":      candidates,
				"prompt_variant":  req.PromptVariant,
				"ai_response":     fmt.Sprintf("❌ AI call failed: %s", aiErr.Error()),
				"ai_error":        aiErr.Error(),
				"note":            "AI call error",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"system_prompt":   systemPrompt,
			"user_prompt":     userPrompt,
			"candidate_count": len(candidates),
			"candidates":      candidates,
			"prompt_variant":  req.PromptVariant,
			"ai_response":     aiResponse,
			"note":            "✅ Real AI test run successful",
		})
		return
	}

	// Return result (without actually calling AI, only return built prompt)
	c.JSON(http.StatusOK, gin.H{
		"system_prompt":   systemPrompt,
		"user_prompt":     userPrompt,
		"candidate_count": len(candidates),
		"candidates":      candidates,
		"prompt_variant":  req.PromptVariant,
		"ai_response":     "Please select an AI model and click 'Run Test' to perform real AI analysis.",
		"note":            "AI model not selected or real AI call not enabled",
	})
}

// runRealAITest Execute real AI test call
func (s *Server) runRealAITest(userID, modelID, systemPrompt, userPrompt string) (string, error) {
	// Get AI model configuration
	model, err := s.store.AIModel().Get(userID, modelID)
	if err != nil {
		return "", fmt.Errorf("failed to get AI model: %w", err)
	}

	if !model.Enabled {
		return "", fmt.Errorf("AI model %s is not enabled", model.Name)
	}

	if model.APIKey == "" {
		return "", fmt.Errorf("AI model %s is missing API Key", model.Name)
	}

	// Create AI client via registry
	provider := model.Provider
	apiKey := string(model.APIKey)

	aiClient := mcp.NewAIClientByProvider(provider)
	if aiClient == nil {
		aiClient = mcp.NewClient()
	}

	aiClient.SetAPIKey(apiKey, model.CustomAPIURL, model.CustomModelName)
	mcp.ApplyThinking(aiClient, model.ThinkingMode, model.ReasoningEffort)

	// Call AI API
	response, err := aiClient.CallWithMessages(systemPrompt, userPrompt)
	if err != nil {
		return "", fmt.Errorf("AI API call failed: %w", err)
	}

	return response, nil
}

// logConfigDiff renders and persists the per-save resolved-knob diff. Failures
// are WARNed, never fatal: an observability write must not break a save (A10).
func (s *Server) logConfigDiff(source, strategyID string, before, after store.StrategyConfig) {
	src := store.NormalizeSource(source)
	changes := store.DiffStrategyConfig(before, after)
	logger.Infof("%s", store.ConfigDiffSummaryLine(src, strategyID, len(changes)))
	if len(changes) == 0 {
		return
	}
	now := time.Now().UTC()
	for i := range changes {
		changes[i].Strategy = strategyID
		changes[i].Source = src
		changes[i].At = now
		logger.Infof("%s", store.ConfigDiffLine(src, changes[i]))
	}
	if s.store != nil && s.store.ConfigChanges() != nil {
		if err := s.store.ConfigChanges().Save(changes); err != nil {
			logger.Warnf("⚙ config diff: persist failed (%d change(s) logged but not stored): %v", len(changes), err)
		}
	}
}
