package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"vl/config"
	"vl/crypto"
	"vl/logger"
	"vl/mcp"
	"vl/security"
	"vl/store"

	"github.com/gin-gonic/gin"
)

type ModelConfig struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	Enabled      bool   `json:"enabled"`
	APIKey       string `json:"apiKey,omitempty"`
	CustomAPIURL string `json:"customApiUrl,omitempty"`
}

// SafeModelConfig Safe model configuration structure (does not contain sensitive information)
type SafeModelConfig struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	Enabled         bool   `json:"enabled"`
	HasAPIKey       bool   `json:"has_api_key"`
	CustomAPIURL    string `json:"customApiUrl"`    // Custom API URL (usually not sensitive)
	CustomModelName string `json:"customModelName"` // Custom model name (not sensitive)
	// DeepSeek thinking knobs (4.5 API auto max). Empty = inherit the env
	// defaults; a set value overrides per-model without env/redeploy.
	ThinkingMode    string `json:"thinkingMode,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

type UpdateModelConfigRequest struct {
	Models map[string]struct {
		Name            string `json:"name"`
		Enabled         bool   `json:"enabled"`
		APIKey          string `json:"api_key"`
		CustomAPIURL    string `json:"custom_api_url"`
		CustomModelName string `json:"custom_model_name"`
		ThinkingMode    string `json:"thinking_mode"`
		ReasoningEffort string `json:"reasoning_effort"`
	} `json:"models"`
}

// handleGetModelConfigs Get AI model configurations
func (s *Server) handleGetModelConfigs(c *gin.Context) {
	userID := c.GetString("user_id")
	logger.Infof("🔍 Querying AI model configs for user %s", userID)
	models, err := s.store.AIModel().List(userID)
	if err != nil {
		logger.Infof("❌ Failed to get AI model configs: %v", err)
		SafeInternalError(c, "Failed to get AI model configs", err)
		return
	}

	// If no models in database, return default models
	if len(models) == 0 {
		logger.Infof("⚠️ No AI models in database, returning defaults")
		defaultModels := []SafeModelConfig{
			{ID: "deepseek", Name: "DeepSeek AI", Provider: "deepseek", Enabled: false, HasAPIKey: false},
			{ID: "qwen", Name: "Qwen AI", Provider: "qwen", Enabled: false, HasAPIKey: false},
			{ID: "openai", Name: "OpenAI", Provider: "openai", Enabled: false, HasAPIKey: false},
			{ID: "claude", Name: "Claude AI", Provider: "claude", Enabled: false, HasAPIKey: false},
			{ID: "gemini", Name: "Gemini AI", Provider: "gemini", Enabled: false, HasAPIKey: false},
			{ID: "grok", Name: "Grok AI", Provider: "grok", Enabled: false, HasAPIKey: false},
			{ID: "kimi", Name: "Kimi AI", Provider: "kimi", Enabled: false, HasAPIKey: false},
			{ID: "minimax", Name: "MiniMax AI", Provider: "minimax", Enabled: false, HasAPIKey: false},
		}
		c.JSON(http.StatusOK, defaultModels)
		return
	}

	logger.Infof("✅ Found %d AI model configs", len(models))

	// Convert to safe response structure, remove sensitive information
	safeModels := make([]SafeModelConfig, 0, len(models))
	for _, model := range models {
		if !store.IsVisibleAIModel(model) {
			continue
		}
		safeModel := SafeModelConfig{
			ID:              model.ID,
			Name:            model.Name,
			Provider:        model.Provider,
			Enabled:         model.Enabled,
			HasAPIKey:       model.APIKey != "",
			CustomAPIURL:    model.CustomAPIURL,
			CustomModelName: model.CustomModelName,
			ThinkingMode:    model.ThinkingMode,
			ReasoningEffort: model.ReasoningEffort,
		}

		safeModels = append(safeModels, safeModel)
	}

	if len(safeModels) == 0 {
		logger.Infof("⚠️ No visible AI models in database, returning defaults")
		defaultModels := []SafeModelConfig{
			{ID: "deepseek", Name: "DeepSeek AI", Provider: "deepseek", Enabled: false, HasAPIKey: false},
			{ID: "qwen", Name: "Qwen AI", Provider: "qwen", Enabled: false, HasAPIKey: false},
			{ID: "openai", Name: "OpenAI", Provider: "openai", Enabled: false, HasAPIKey: false},
			{ID: "claude", Name: "Claude AI", Provider: "claude", Enabled: false, HasAPIKey: false},
			{ID: "gemini", Name: "Gemini AI", Provider: "gemini", Enabled: false, HasAPIKey: false},
			{ID: "grok", Name: "Grok AI", Provider: "grok", Enabled: false, HasAPIKey: false},
			{ID: "kimi", Name: "Kimi AI", Provider: "kimi", Enabled: false, HasAPIKey: false},
			{ID: "minimax", Name: "MiniMax AI", Provider: "minimax", Enabled: false, HasAPIKey: false},
		}
		c.JSON(http.StatusOK, defaultModels)
		return
	}

	c.JSON(http.StatusOK, safeModels)
}

// handleUpdateModelConfigs Update AI model configurations (supports both encrypted and plain text based on config)
func (s *Server) handleUpdateModelConfigs(c *gin.Context) {
	userID := c.GetString("user_id")
	cfg := config.Get()

	// Read raw request body
	bodyBytes, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}

	var req UpdateModelConfigRequest

	// Check if transport encryption is enabled
	if !cfg.TransportEncryption {
		// Transport encryption disabled, accept plain JSON
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			logger.Infof("❌ Failed to parse plain JSON request: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
			return
		}
		logger.Infof("📝 Received plain text model config (UserID: %s)", userID)
	} else {
		// Transport encryption enabled, require encrypted payload
		var encryptedPayload crypto.EncryptedPayload
		if err := json.Unmarshal(bodyBytes, &encryptedPayload); err != nil {
			logger.Infof("❌ Failed to parse encrypted payload: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format, encrypted transmission required"})
			return
		}

		// Verify encrypted data
		if encryptedPayload.WrappedKey == "" {
			logger.Infof("❌ Detected unencrypted request (UserID: %s)", userID)
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "This endpoint only supports encrypted transmission, please use encrypted client",
				"code":    "ENCRYPTION_REQUIRED",
				"message": "Encrypted transmission is required for security reasons",
			})
			return
		}

		// Decrypt data
		decrypted, err := s.cryptoHandler.cryptoService.DecryptSensitiveData(&encryptedPayload)
		if err != nil {
			logger.Infof("❌ Failed to decrypt model config (UserID: %s): %v", userID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to decrypt data"})
			return
		}

		// Parse decrypted data
		if err := json.Unmarshal([]byte(decrypted), &req); err != nil {
			logger.Infof("❌ Failed to parse decrypted data: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse decrypted data"})
			return
		}
		logger.Infof("🔓 Decrypted model config data (UserID: %s)", userID)
	}

	// Update each model's configuration and track traders that need reload
	tradersToReload := make(map[string]bool)
	for modelID, modelData := range req.Models {
		// SSRF protection: validate custom_api_url before storing (R3 — the ONE
		// helper reads friend mode and demands https there).
		if err := security.ValidateModelURL(modelData.CustomAPIURL); err != nil {
			logger.Warnf("Invalid custom_api_url for model %s: %v", modelID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid custom_api_url for model %s: URL must be a valid HTTPS endpoint", modelID)})
			return
		}

		// Find traders using this AI model BEFORE updating
		traders, _ := s.store.Trader().ListByAIModelID(userID, modelID)
		for _, t := range traders {
			tradersToReload[t.ID] = true
		}

		// UpdateWithName carries an optional display-name (rename); empty name preserves
		// the existing name (UpdateWithName only sets name when non-blank).
		err := s.store.AIModel().UpdateWithName(userID, modelID, modelData.Name, modelData.Enabled, modelData.APIKey, modelData.CustomAPIURL, modelData.CustomModelName)
		if err != nil {
			SafeInternalError(c, fmt.Sprintf("Update model %s", modelID), err)
			return
		}
		// 4.5 — per-model thinking knobs (empty = inherit env defaults).
		thinkingMode := strings.ToLower(strings.TrimSpace(modelData.ThinkingMode))
		reasoningEffort := strings.ToLower(strings.TrimSpace(modelData.ReasoningEffort))
		if err := mcp.ValidateThinkingKnobs(thinkingMode, reasoningEffort); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := s.store.AIModel().UpdateThinking(userID, modelID, thinkingMode, reasoningEffort); err != nil {
			logger.Warnf("⚠️ thinking knobs not persisted for model %s: %v", modelID, err)
		}
	}

	// Remove affected traders from memory BEFORE reloading to pick up new config
	for traderID := range tradersToReload {
		logger.Infof("🔄 Removing trader %s from memory to reload with new AI model config", traderID)
		s.traderManager.RemoveTrader(traderID)
	}

	// Reload all traders for this user to make new config take effect immediately
	err = s.traderManager.LoadUserTradersFromStore(s.store, userID)
	if err != nil {
		logger.Infof("⚠️ Failed to reload user traders into memory: %v", err)
		// Don't return error here since model config was successfully updated to database
	}

	// SECURITY (P0 S5): this used to be `logger.Infof(... "%+v", req.Models)`,
	// which wrote PLAINTEXT provider API keys into data/vl_*.log (mode 0644,
	// retained indefinitely — real keys were found in three existing log files).
	// Mask via the same helper the (previously unused) sanitizers use.
	safe := make([]string, 0, len(req.Models))
	for modelID, m := range req.Models {
		safe = append(safe, fmt.Sprintf("%s{enabled:%v key:%s url:%s}",
			modelID, m.Enabled, MaskSensitiveString(m.APIKey), m.CustomAPIURL))
	}
	sort.Strings(safe)
	logger.Infof("✓ AI model config updated: %s", strings.Join(safe, " "))
	c.JSON(http.StatusOK, gin.H{"message": "Model configuration updated"})
}

// handleGetSupportedModels Get list of AI models supported by the system
func (s *Server) handleGetSupportedModels(c *gin.Context) {
	// Return static list of supported AI models with default versions
	supportedModels := []map[string]interface{}{
		{"id": "deepseek", "name": "DeepSeek", "provider": "deepseek", "defaultModel": "deepseek-v4-pro"},
		{"id": "qwen", "name": "Qwen", "provider": "qwen", "defaultModel": "qwen3-max"},
		{"id": "openai", "name": "OpenAI", "provider": "openai", "defaultModel": "gpt-5.1"},
		{"id": "claude", "name": "Claude", "provider": "claude", "defaultModel": "claude-opus-4-6"},
		{"id": "gemini", "name": "Google Gemini", "provider": "gemini", "defaultModel": "gemini-3-pro-preview"},
		{"id": "grok", "name": "Grok (xAI)", "provider": "grok", "defaultModel": "grok-3-latest"},
		{"id": "kimi", "name": "Kimi (Moonshot)", "provider": "kimi", "defaultModel": "moonshot-v1-auto"},
		{"id": "minimax", "name": "MiniMax", "provider": "minimax", "defaultModel": "MiniMax-M2.7"},
	}

	c.JSON(http.StatusOK, supportedModels)
}

// CreateModelEntryRequest is the body for POST /api/models/entry — create an
// ADDITIONAL, independently-addressable entry for a provider (multi-key support).
type CreateModelEntryRequest struct {
	Provider        string `json:"provider"`
	Name            string `json:"name"`
	APIKey          string `json:"api_key"`
	CustomAPIURL    string `json:"custom_api_url"`
	CustomModelName string `json:"custom_model_name"`
	Enabled         *bool  `json:"enabled"`
}

// handleCreateModelEntry creates a NEW model row (unique id) for a provider, so a
// provider can hold multiple entries (e.g. DeepSeek-main + DeepSeek-backup). Existing
// rows — which traders reference by id — are never touched.
func (s *Server) handleCreateModelEntry(c *gin.Context) {
	userID := c.GetString("user_id")
	cfg := config.Get()

	bodyBytes, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}

	var req CreateModelEntryRequest
	if !cfg.TransportEncryption {
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
			return
		}
	} else {
		var encryptedPayload crypto.EncryptedPayload
		if err := json.Unmarshal(bodyBytes, &encryptedPayload); err != nil || encryptedPayload.WrappedKey == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Encrypted transmission required", "code": "ENCRYPTION_REQUIRED"})
			return
		}
		decrypted, derr := s.cryptoHandler.cryptoService.DecryptSensitiveData(&encryptedPayload)
		if derr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to decrypt data"})
			return
		}
		if err := json.Unmarshal([]byte(decrypted), &req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse decrypted data"})
			return
		}
	}

	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider is required"})
		return
	}
	if err := security.ValidateModelURL(req.CustomAPIURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid custom_api_url: URL must be a valid HTTPS endpoint"})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	id, err := s.store.AIModel().CreateEntry(userID, provider, req.Name, enabled, req.APIKey, req.CustomAPIURL, req.CustomModelName)
	if err != nil {
		SafeInternalError(c, "Create model entry", err)
		return
	}
	logger.Infof("✓ Created AI model entry id=%s provider=%s (user %s)", id, provider, userID)
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// handleDeleteModelEntry deletes one model row by EXACT id. Refused if a trader is
// still bound to it (bindings reference the row id).
func (s *Server) handleDeleteModelEntry(c *gin.Context) {
	userID := c.GetString("user_id")
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model id is required"})
		return
	}

	if traders, err := s.store.Trader().ListByAIModelID(userID, id); err == nil && len(traders) > 0 {
		names := make([]string, 0, len(traders))
		for _, t := range traders {
			names = append(names, t.Name)
		}
		c.JSON(http.StatusConflict, gin.H{
			"error":   "Cannot delete this AI model because it is being used by traders",
			"traders": names,
		})
		return
	}

	if err := s.store.AIModel().Delete(userID, id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted", "id": id})
}
