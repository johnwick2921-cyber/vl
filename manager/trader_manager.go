package manager

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"vl/config"
	"vl/kernel"
	"vl/logger"
	"vl/safe"
	"vl/store"
	"vl/trader"
)

func traderLogTag(traderID, traderName string) string {
	if traderName != "" {
		return fmt.Sprintf("[trader_id=%s trader_name=%s]", traderID, traderName)
	}
	return fmt.Sprintf("[trader_id=%s]", traderID)
}

// bootSymbolAccountKey canonicalizes the (symbol, account) pair the boot dedup
// keys on — the SAME normalization the provider's ordered-execution registry
// uses (uppercase symbol, trimmed account), so the two guards agree on what
// "same (symbol, account)" means.
func bootSymbolAccountKey(symbol, account string) string {
	return strings.ToUpper(strings.TrimSpace(symbol)) + "\x00" + strings.TrimSpace(account)
}

// bootClaimFor is the REL18-C boot-dedup DECISION: may traderCfg auto-start?
// It records the (symbol, account) claim for the first trader (returns
// refused=false) and refuses the second (returns the holder id, refused=true).
// A nil claim map (per-user reload) or a non-NT/unbound trader always proceeds.
func (tm *TraderManager) bootClaimFor(exchangeCfg *store.Exchange, traderCfg *store.Trader) (holderID string, refused bool) {
	if tm.startPassClaims == nil || exchangeCfg.ExchangeType != "ninjatrader" || strings.TrimSpace(traderCfg.Account) == "" {
		return "", false
	}
	key := bootSymbolAccountKey(exchangeCfg.NTInstrumentName, traderCfg.Account)
	if holder, exists := tm.startPassClaims[key]; exists && holder != traderCfg.ID {
		return holder, true
	}
	tm.startPassClaims[key] = traderCfg.ID
	return "", false
}

// shouldAutoStart is the REL18-C item-5 gate addTraderFromStore calls before
// auto-starting a trader marked running. At boot the first trader on a
// (symbol, account) claims it and starts; a second is refused (not started,
// marked not-running, P0). Returns false when the trader must NOT auto-start.
func (tm *TraderManager) shouldAutoStart(exchangeCfg *store.Exchange, traderCfg *store.Trader, st *store.Store) bool {
	if holder, refused := tm.bootClaimFor(exchangeCfg, traderCfg); refused {
		tm.refuseBootDuplicate(traderCfg, exchangeCfg, holder, st)
		return false
	}
	return true
}

// refuseBootDuplicate handles a REL18-C item-5 refusal: a second trader marked
// running on an already-claimed (symbol, account) at boot is NOT auto-started.
// It is loaded (stopped), marked not-running in the DB, and a P0 names both
// traders. The boot line READS the resolved values (L7) — never asserts.
func (tm *TraderManager) refuseBootDuplicate(traderCfg *store.Trader, exchangeCfg *store.Exchange, holderID string, st *store.Store) {
	symbol := strings.TrimSpace(exchangeCfg.NTInstrumentName)
	account := strings.TrimSpace(traderCfg.Account)
	logger.Warnf("⛔ boot: trader %q (%s) NOT auto-started — %s / %s already claimed by trader %q at boot (one trader per symbol,account); marked not-running",
		traderCfg.Name, traderCfg.ID, symbol, account, holderID)
	if st != nil {
		_, _ = st.Alert().Emit(&store.AlertDB{
			TraderID:  traderCfg.ID,
			Level:     "P0",
			Kind:      "boot_duplicate_symbol_account",
			EventID:   "boot-dup:" + bootSymbolAccountKey(symbol, account),
			Title:     "Two traders bound to one account — the second was not started",
			Body:      fmt.Sprintf("At boot, trader %q (%s) was not started because %s / %s is already owned by trader %q (one trader per symbol, account). It was marked not-running.", traderCfg.Name, traderCfg.ID, symbol, account, holderID),
			CreatedAt: time.Now().Unix(),
		})
		_ = st.Trader().UpdateStatus(traderCfg.UserID, traderCfg.ID, false)
	}
}

// CompetitionCache competition data cache
type CompetitionCache struct {
	data      map[string]interface{}
	timestamp time.Time
	mu        sync.RWMutex
}

// TraderManager manages multiple trader instances
type TraderManager struct {
	traders          map[string]*trader.AutoTrader // key: trader ID
	loadErrors       map[string]error              // key: trader ID, stores last load error
	competitionCache *CompetitionCache
	mu               sync.RWMutex
	// startPassClaims is the REL18-C boot dedup: one (symbol, account) per
	// auto-started trader. Key = bootSymbolAccountKey(symbol, account), value =
	// the trader id that claimed it. nil = not in a boot pass (per-user reloads
	// do NOT dedup — O3b/O2 own the runtime guard). Written only under tm.mu,
	// which every loader holds for its whole pass.
	startPassClaims map[string]string
}

// NewTraderManager creates a trader manager
func NewTraderManager() *TraderManager {
	return &TraderManager{
		traders:    make(map[string]*trader.AutoTrader),
		loadErrors: make(map[string]error),
		competitionCache: &CompetitionCache{
			data: make(map[string]interface{}),
		},
	}
}

// GetLoadError returns the last load error for a trader
func (tm *TraderManager) GetLoadError(traderID string) error {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.loadErrors[traderID]
}

// SetTraderForTest installs a prebuilt AutoTrader in the roster under id.
// TEST-ONLY seam for another package's production-call-site test: the roster
// is otherwise filled only from store rows (addTraderFromStore), which pulls
// NewAutoTrader's process-wide side effects. Never called in production.
func (tm *TraderManager) SetTraderForTest(id string, at *trader.AutoTrader) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.traders[id] = at
}

// GetTrader retrieves a trader by ID
func (tm *TraderManager) GetTrader(id string) (*trader.AutoTrader, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	t, exists := tm.traders[id]
	if !exists {
		return nil, fmt.Errorf("trader ID '%s' does not exist", id)
	}
	return t, nil
}

// GetAllTraders retrieves all traders
func (tm *TraderManager) GetAllTraders() map[string]*trader.AutoTrader {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	result := make(map[string]*trader.AutoTrader)
	for id, t := range tm.traders {
		result[id] = t
	}
	return result
}

// GetTraderIDs retrieves all trader IDs
func (tm *TraderManager) GetTraderIDs() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	ids := make([]string, 0, len(tm.traders))
	for id := range tm.traders {
		ids = append(ids, id)
	}
	return ids
}

// StartAll starts all traders
func (tm *TraderManager) StartAll() {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	logger.Info("🚀 Starting all traders...")
	for id, t := range tm.traders {
		safe.GoNet("trader-start-all", id, func() {
			traderID := id
			at := t
			logger.Infof("%s ▶️ Starting trader runtime", traderLogTag(traderID, at.GetName()))
			if err := at.Run(); err != nil {
				logger.Warnf("%s runtime error: %v", traderLogTag(traderID, at.GetName()), err)
			}
		})
	}
}

// StopAll stops all traders
func (tm *TraderManager) StopAll() {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	logger.Info("⏹  Stopping all traders...")
	for _, t := range tm.traders {
		t.Stop()
	}
}

// (6.8 deprecation sweep) AutoStartRunningTraders removed — zero callers
// [A, PR #54]: the real boot restore reads is_running directly in
// addTraderFromStore (see the auto-start goroutine below in this file).

// GetComparisonData retrieves comparison data
func (tm *TraderManager) GetComparisonData() (map[string]interface{}, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	comparison := make(map[string]interface{})
	traders := make([]map[string]interface{}, 0, len(tm.traders))

	for _, t := range tm.traders {
		account, err := t.GetAccountInfo()
		if err != nil {
			continue
		}

		status := t.GetStatus()

		traders = append(traders, map[string]interface{}{
			"trader_id":       t.GetID(),
			"trader_name":     t.GetName(),
			"ai_model":        t.GetAIModel(),
			"exchange":        t.GetExchange(),
			"total_equity":    account["total_equity"],
			"total_pnl":       account["total_pnl"],
			"total_pnl_pct":   account["total_pnl_pct"],
			"position_count":  account["position_count"],
			"margin_used_pct": account["margin_used_pct"],
			"call_count":      status["call_count"],
			"is_running":      status["is_running"],
		})
	}

	comparison["traders"] = traders
	comparison["count"] = len(traders)

	return comparison, nil
}

// GetCompetitionData retrieves competition data (all traders across platform)
func (tm *TraderManager) GetCompetitionData() (map[string]interface{}, error) {
	// Check if cache is valid (within 30 seconds)
	tm.competitionCache.mu.RLock()
	if time.Since(tm.competitionCache.timestamp) < 30*time.Second && len(tm.competitionCache.data) > 0 {
		// Return cached data
		cachedData := make(map[string]interface{})
		for k, v := range tm.competitionCache.data {
			cachedData[k] = v
		}
		tm.competitionCache.mu.RUnlock()
		logger.Infof("📋 Returning competition data cache (cache age: %.1fs)", time.Since(tm.competitionCache.timestamp).Seconds())
		return cachedData, nil
	}
	tm.competitionCache.mu.RUnlock()

	tm.mu.RLock()

	// Get all trader list (only those with ShowInCompetition = true)
	allTraders := make([]*trader.AutoTrader, 0, len(tm.traders))
	for id, t := range tm.traders {
		if t.GetShowInCompetition() {
			allTraders = append(allTraders, t)
			logger.Infof("📋 Competition data includes trader: %s (%s)", t.GetName(), id)
		} else {
			logger.Infof("📋 Competition data excludes trader (hidden): %s (%s)", t.GetName(), id)
		}
	}
	tm.mu.RUnlock()

	logger.Infof("🔄 Refreshing competition data, trader count: %d", len(allTraders))

	// Concurrently fetch trader data
	traders := tm.getConcurrentTraderData(allTraders)

	// Sort by profit rate (descending)
	sort.Slice(traders, func(i, j int) bool {
		pnlPctI, okI := traders[i]["total_pnl_pct"].(float64)
		pnlPctJ, okJ := traders[j]["total_pnl_pct"].(float64)
		if !okI {
			pnlPctI = 0
		}
		if !okJ {
			pnlPctJ = 0
		}
		return pnlPctI > pnlPctJ
	})

	// Limit to top 50
	totalCount := len(traders)
	limit := 50
	if len(traders) > limit {
		traders = traders[:limit]
	}

	comparison := make(map[string]interface{})
	comparison["traders"] = traders
	comparison["count"] = len(traders)
	comparison["total_count"] = totalCount // Total number of traders

	// Update cache
	tm.competitionCache.mu.Lock()
	tm.competitionCache.data = comparison
	tm.competitionCache.timestamp = time.Now()
	tm.competitionCache.mu.Unlock()

	return comparison, nil
}

// getConcurrentTraderData concurrently fetches data for multiple traders
func (tm *TraderManager) getConcurrentTraderData(traders []*trader.AutoTrader) []map[string]interface{} {
	type traderResult struct {
		index int
		data  map[string]interface{}
	}

	// Create result channel
	resultChan := make(chan traderResult, len(traders))

	// Concurrently fetch data for each trader
	for i, t := range traders {
		safe.GoNet("trader-account-fetch", t.GetID(), func() {
			index := i
			trader := t
			// Set timeout to 10 seconds for single trader (increased from 3s for DEX reliability)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Use channel for timeout control
			accountChan := make(chan map[string]interface{}, 1)
			errorChan := make(chan error, 1)

			safe.GoNet("trader-account-info", trader.GetID(), func() {
				account, err := trader.GetAccountInfo()
				if err != nil {
					errorChan <- err
				} else {
					accountChan <- account
				}
			})

			status := trader.GetStatus()
			var traderData map[string]interface{}

			select {
			case account := <-accountChan:
				// Successfully got account info
				traderData = map[string]interface{}{
					"trader_id":              trader.GetID(),
					"trader_name":            trader.GetName(),
					"ai_model":               trader.GetAIModel(),
					"exchange":               trader.GetExchange(),
					"total_equity":           account["total_equity"],
					"total_pnl":              account["total_pnl"],
					"total_pnl_pct":          account["total_pnl_pct"],
					"position_count":         account["position_count"],
					"margin_used_pct":        account["margin_used_pct"],
					"is_running":             status["is_running"],
					"system_prompt_template": trader.GetSystemPromptTemplate(),
				}
			case err := <-errorChan:
				// Failed to get account info
				logger.Infof("⚠️ Failed to get account info for trader %s (%s/%s): %v", trader.GetName(), trader.GetID(), trader.GetExchange(), err)
				traderData = map[string]interface{}{
					"trader_id":              trader.GetID(),
					"trader_name":            trader.GetName(),
					"ai_model":               trader.GetAIModel(),
					"exchange":               trader.GetExchange(),
					"total_equity":           0.0,
					"total_pnl":              0.0,
					"total_pnl_pct":          0.0,
					"position_count":         0,
					"margin_used_pct":        0.0,
					"is_running":             status["is_running"],
					"system_prompt_template": trader.GetSystemPromptTemplate(),
					"error":                  "Failed to get account data",
				}
			case <-ctx.Done():
				// Timeout
				logger.Infof("⏰ Timeout (10s) getting account info for trader %s (%s/%s)", trader.GetName(), trader.GetID(), trader.GetExchange())
				traderData = map[string]interface{}{
					"trader_id":              trader.GetID(),
					"trader_name":            trader.GetName(),
					"ai_model":               trader.GetAIModel(),
					"exchange":               trader.GetExchange(),
					"total_equity":           0.0,
					"total_pnl":              0.0,
					"total_pnl_pct":          0.0,
					"position_count":         0,
					"margin_used_pct":        0.0,
					"is_running":             status["is_running"],
					"system_prompt_template": trader.GetSystemPromptTemplate(),
					"error":                  "Request timeout",
				}
			}

			resultChan <- traderResult{index: index, data: traderData}
		})
	}

	// Collect all results
	results := make([]map[string]interface{}, len(traders))
	for i := 0; i < len(traders); i++ {
		result := <-resultChan
		results[result.index] = result.data
	}

	return results
}

// GetTopTradersData retrieves top 5 traders data (for performance comparison)
func (tm *TraderManager) GetTopTradersData() (map[string]interface{}, error) {
	// Reuse competition data cache, as top 5 is filtered from all data
	competitionData, err := tm.GetCompetitionData()
	if err != nil {
		return nil, err
	}

	// Extract top 5 from competition data
	allTraders, ok := competitionData["traders"].([]map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid competition data format")
	}

	// Limit to top 5
	limit := 5
	topTraders := allTraders
	if len(allTraders) > limit {
		topTraders = allTraders[:limit]
	}

	result := map[string]interface{}{
		"traders": topTraders,
		"count":   len(topTraders),
	}

	return result, nil
}

// RemoveTrader removes a trader from memory (does not affect database)
// Used to force reload when updating trader configuration
// If the trader is running, it will be stopped first
func (tm *TraderManager) RemoveTrader(traderID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if t, exists := tm.traders[traderID]; exists {
		// Stop the trader if it's running (this ensures the goroutine exits)
		status := t.GetStatus()
		if isRunning, ok := status["is_running"].(bool); ok && isRunning {
			logger.Infof("⏹ Stopping trader %s before removing from memory...", traderID)
			t.Stop()
		}
		delete(tm.traders, traderID)
		logger.Infof("✓ Trader %s removed from memory", traderID)
	}
}

// LoadUserTradersFromStore loads traders from store for a specific user to memory
func (tm *TraderManager) LoadUserTradersFromStore(st *store.Store, userID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// REL18-C: a per-user reload is NOT a boot pass — the runtime guards
	// (O3b start duplicate + O2 ordered-owner) own dedup here, and the boot
	// claim map must not leak stale claims from a prior boot.
	tm.startPassClaims = nil

	// Get all traders for the specified user
	traders, err := st.Trader().List(userID)
	if err != nil {
		return fmt.Errorf("failed to get trader list for user %s: %w", userID, err)
	}

	logger.Infof("📋 Loading trader configurations for user %s: %d traders", userID, len(traders))

	// Get AI model and exchange lists (query only once outside loop)
	aiModels, err := st.AIModel().List(userID)
	if err != nil {
		logger.Infof("⚠️ Failed to get AI model config for user %s: %v", userID, err)
		return fmt.Errorf("failed to get AI model config: %w", err)
	}

	exchanges, err := st.Exchange().List(userID)
	if err != nil {
		logger.Infof("⚠️ Failed to get exchange config for user %s: %v", userID, err)
		return fmt.Errorf("failed to get exchange config: %w", err)
	}

	// Load configuration for each trader
	for _, traderCfg := range traders {
		// Check if this trader is already loaded
		if _, exists := tm.traders[traderCfg.ID]; exists {
			// Trader already loaded - this is normal, no need to log
			continue
		}

		// Find AI model config from already queried list
		var aiModelCfg *store.AIModel
		for _, model := range aiModels {
			if model.ID == traderCfg.AIModelID {
				aiModelCfg = model
				break
			}
		}
		if aiModelCfg == nil {
			// Legacy binding: trader.AIModelID holds a bare provider name. With multiple
			// entries per provider, resolve DETERMINISTICALLY (enabled + most-recent) — a
			// trader bound to a specific row id already matched exactly above.
			if chosen, n := store.PickProviderModel(aiModels, traderCfg.AIModelID); chosen != nil {
				aiModelCfg = chosen
				if n > 1 {
					logger.Infof("⚠️ Trader %s legacy provider-match %q → %d entries; using id=%s", traderCfg.Name, traderCfg.AIModelID, n, chosen.ID)
				}
			}
		}

		if aiModelCfg == nil {
			logger.Infof("⚠️ AI model %s for trader %s does not exist, skipping", traderCfg.AIModelID, traderCfg.Name)
			continue
		}

		if !aiModelCfg.Enabled {
			logger.Infof("⚠️ AI model %s for trader %s is not enabled, skipping", traderCfg.AIModelID, traderCfg.Name)
			continue
		}

		// Find exchange config from already queried list
		var exchangeCfg *store.Exchange
		for _, exchange := range exchanges {
			if exchange.ID == traderCfg.ExchangeID {
				exchangeCfg = exchange
				break
			}
		}

		if exchangeCfg == nil {
			logger.Infof("⚠️ Exchange %s for trader %s does not exist, skipping", traderCfg.ExchangeID, traderCfg.Name)
			continue
		}

		if !exchangeCfg.Enabled {
			logger.Infof("⚠️ Exchange %s for trader %s is not enabled, skipping", traderCfg.ExchangeID, traderCfg.Name)
			continue
		}

		// Use existing method to load trader
		logger.Infof("📦 Loading trader %s (AI Model: %s, Exchange: %s/%s, Strategy ID: %s)", traderCfg.Name, aiModelCfg.Provider, exchangeCfg.ExchangeType, exchangeCfg.AccountName, traderCfg.StrategyID)
		err = tm.addTraderFromStore(traderCfg, aiModelCfg, exchangeCfg, st)
		if err != nil {
			logger.Warnf("%s failed to load trader: %v", traderLogTag(traderCfg.ID, traderCfg.Name), err)
			// Save error for later retrieval
			tm.loadErrors[traderCfg.ID] = err
		} else {
			// Clear any previous error on success
			delete(tm.loadErrors, traderCfg.ID)
		}
	}

	return nil
}

// LoadTradersFromStore loads all traders from store to memory (new API)
func (tm *TraderManager) LoadTradersFromStore(st *store.Store) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// REL18-C item 5: the boot pass claims one (symbol, account) per running
	// trader, FIRST by created_at. The map is built during THIS pass only.
	tm.startPassClaims = make(map[string]string)

	// Get all users
	userIDs, err := st.User().GetAllIDs()
	if err != nil {
		return fmt.Errorf("failed to get user list: %w", err)
	}

	logger.Infof("📋 Found %d users, loading all trader configurations...", len(userIDs))

	var allTraders []*store.Trader
	for _, userID := range userIDs {
		// Get traders for each user
		traders, err := st.Trader().List(userID)
		if err != nil {
			logger.Infof("⚠️ Failed to get traders for user %s: %v", userID, err)
			continue
		}
		logger.Infof("📋 User %s: %d traders", userID, len(traders))
		allTraders = append(allTraders, traders...)
	}

	logger.Infof("📋 Total loaded trader configurations: %d", len(allTraders))

	// REL18-C item 5: "the first by created_at" claims the (symbol, account).
	// List() returns created_at DESC, so sort ASC here so the OLDEST trader
	// wins the boot claim and a younger duplicate is the one refused.
	sort.Slice(allTraders, func(i, j int) bool {
		return allTraders[i].CreatedAt.Before(allTraders[j].CreatedAt)
	})

	// Get AI model and exchange configs for each trader
	for _, traderCfg := range allTraders {
		// Get AI model config
		aiModels, err := st.AIModel().List(traderCfg.UserID)
		if err != nil {
			logger.Infof("⚠️  Failed to get AI model config: %v", err)
			continue
		}

		var aiModelCfg *store.AIModel
		// Prioritize exact match on model.ID
		for _, model := range aiModels {
			if model.ID == traderCfg.AIModelID {
				aiModelCfg = model
				break
			}
		}
		// If no exact match, try matching provider (backward compat, legacy bare-provider
		// binding). Deterministic pick (enabled + most-recent) when multiple entries exist.
		if aiModelCfg == nil {
			if chosen, n := store.PickProviderModel(aiModels, traderCfg.AIModelID); chosen != nil {
				aiModelCfg = chosen
				logger.Infof("⚠️  Trader %s using legacy provider match: %s -> %s (of %d)", traderCfg.Name, traderCfg.AIModelID, chosen.ID, n)
			}
		}

		if aiModelCfg == nil {
			logger.Infof("⚠️  AI model %s for trader %s does not exist, skipping", traderCfg.AIModelID, traderCfg.Name)
			continue
		}

		if !aiModelCfg.Enabled {
			logger.Infof("⚠️  AI model %s for trader %s is not enabled, skipping", traderCfg.AIModelID, traderCfg.Name)
			continue
		}

		// Get exchange config
		exchanges, err := st.Exchange().List(traderCfg.UserID)
		if err != nil {
			logger.Infof("⚠️  Failed to get exchange config: %v", err)
			continue
		}

		var exchangeCfg *store.Exchange
		for _, exchange := range exchanges {
			if exchange.ID == traderCfg.ExchangeID {
				exchangeCfg = exchange
				break
			}
		}

		if exchangeCfg == nil {
			logger.Infof("⚠️  Exchange %s for trader %s does not exist, skipping", traderCfg.ExchangeID, traderCfg.Name)
			continue
		}

		if !exchangeCfg.Enabled {
			logger.Infof("⚠️  Exchange %s for trader %s is not enabled, skipping", traderCfg.ExchangeID, traderCfg.Name)
			continue
		}

		// Add to TraderManager
		err = tm.addTraderFromStore(traderCfg, aiModelCfg, exchangeCfg, st)
		if err != nil {
			logger.Warnf("%s failed to add trader: %v", traderLogTag(traderCfg.ID, traderCfg.Name), err)
			continue
		}
	}

	logger.Infof("✓ Successfully loaded %d traders to memory", len(tm.traders))
	return nil
}

// addTraderFromStore internal method: adds trader from store configuration
func (tm *TraderManager) addTraderFromStore(traderCfg *store.Trader, aiModelCfg *store.AIModel, exchangeCfg *store.Exchange, st *store.Store) error {
	if _, exists := tm.traders[traderCfg.ID]; exists {
		return fmt.Errorf("trader ID '%s' already exists", traderCfg.ID)
	}

	// Load strategy config (must have strategy)
	var strategyConfig *store.StrategyConfig
	if traderCfg.StrategyID != "" {
		strategy, err := st.Strategy().Get(traderCfg.UserID, traderCfg.StrategyID)
		if err != nil {
			return fmt.Errorf("failed to load strategy %s for trader %s: %w", traderCfg.StrategyID, traderCfg.Name, err)
		}
		// Parse JSON config
		strategyConfig, err = strategy.ParseConfig()
		if err != nil {
			return fmt.Errorf("failed to parse strategy config for trader %s: %w", traderCfg.Name, err)
		}
		// W1 (settings truth, 2026-09-23) — FAIL-CLOSED CONVERSION. A stored
		// explicit 0 on consecutive_loss_halt / day_plan.replan_cap meant
		// "inherit" before W1 and means OFF / 0 now. If this strategy holds one
		// no Studio save has confirmed, the trader does not load: an inherited
		// breaker is never silently disabled, and nothing is rewritten.
		if reason := st.Strategy().SettingsTruthRefusal(strategy, os.Getenv); reason != "" {
			logger.Warnf("⛔ settings truth: trader %s REFUSED at load — %s", traderCfg.Name, reason)
			return fmt.Errorf("settings truth: %s", reason)
		}
		logger.Infof("✓ Trader %s loaded strategy config: %s", traderCfg.Name, strategy.Name)
		// S3 (2026-09-16) — the resolved HTF knobs, with their sources.
		logger.Infof("%s", trader.HtfKnobsBootLine(strategyConfig.DayPlan))
		// W1 (settings truth, 2026-09-23) — the re-plan cap per session and
		// the consecutive-loss breaker, READ from the resolvers the gates run,
		// each with its origin. The process 🛑 line prints breaker=n/a when
		// more than one strategy is bound; this line is where each one reads.
		logger.Infof("%s", trader.ReplanCapBootLine(strategyConfig.DayPlan))
		logger.Infof("🛑 [%s] %s", traderCfg.Name, trader.BreakerBootLineForStrategy(strategyConfig))
		// W-KNOB-PRUNE (2026-09-18) — every folded knob whose STORED value
		// differs from its constant, once, so a removed control never keeps
		// a value silently.
		for _, line := range strategyConfig.DayPlan.FoldedKnobLines() {
			logger.Infof("%s", line)
		}
		// W-FLIP-REREAD (2026-09-17) — the resolved flip knob, with its source.
		logger.Infof("%s", trader.FlipRereadBootLine(strategyConfig.DayPlan)) // W-GEOMETRY-REFUSAL (2026-09-18) — the resolved geometry-reference
		// knob (default ON per owner ruling; the process boot line prints n/a).
		logger.Infof("%s", trader.GeometryRefBootLine(strategyConfig.DayPlan))  // W-T1-CURRENCIES (2026-09-18) — the resolved red-news hard-block set.
		logger.Infof("%s", trader.T1CurrenciesBootLine(strategyConfig.DayPlan)) // W-DEATH-REREAD (2026-09-18) — the resolved death knob (nil=ON per
		// the owner's 12:3x CT "fix all" ruling), sibling of the flip line.
		logger.Infof("%s", trader.DeathRereadBootLine(strategyConfig.DayPlan)) // D102-1 (2026-09-16): the exit posture, READ from the strategy
		// toggles the mechanics honour — the main boot line prints n/a
		// for these fields; this line prints the real values, prefixed with the
		// TRADER NAME (never an account) so it cannot be mistaken for the
		// main-boot 🛑 line when two traders run.
		logger.Infof("🛑 [%s] %s", traderCfg.Name, trader.ExitPolicyBootLineForStrategy(kernel.MinSLATRMult(), strategyConfig.RiskControl))
	} else {
		return fmt.Errorf("trader %s has no strategy configured", traderCfg.Name)
	}

	// Build AutoTraderConfig
	traderConfig := trader.AutoTraderConfig{
		ID:              traderCfg.ID,
		Name:            traderCfg.Name,
		AIModel:         aiModelCfg.Provider,
		AIModelID:       aiModelCfg.ID,
		Exchange:        exchangeCfg.ExchangeType,
		ExchangeID:      exchangeCfg.ID, // Exchange account UUID (for multi-account)
		UseQwen:         aiModelCfg.Provider == "qwen",
		DeepSeekKey:     "",
		QwenKey:         "",
		CustomAPIURL:    aiModelCfg.CustomAPIURL,
		CustomModelName: aiModelCfg.CustomModelName,
		ScanInterval:    time.Duration(traderCfg.ScanIntervalMinutes) * time.Minute,
		CadenceMode:     traderCfg.CadenceMode,
		// 4.3 — limit-then-market flatten knobs (dormant 0/0 = market flatten).
		LimitCloseTicks:        config.Get().LimitCloseTicks,
		LimitCloseMarketAfterS: config.Get().LimitCloseMarketAfterS,
		PositionMode:           traderCfg.PositionMode,
		InitialBalance:         traderCfg.InitialBalance,
		IsCrossMargin:          traderCfg.IsCrossMargin,
		ShowInCompetition:      traderCfg.ShowInCompetition,
		StrategyConfig:         strategyConfig,
	}

	// P10 — the boot line names the cadence SOURCE + resolved behavior, so the
	// interval-vs-bar-close ambiguity that confused the owner can't recur.
	resolvedMode := traderCfg.CadenceMode
	if resolvedMode == "" {
		resolvedMode = "interval"
	}
	logger.Infof("📊 Loading trader %s: ScanIntervalMinutes=%d (source=Studio/DB), cadence=interval %v, mode=%s%s",
		traderCfg.Name, traderCfg.ScanIntervalMinutes, traderConfig.ScanInterval, resolvedMode,
		map[bool]string{true: "", false: " (DB empty → default)"}[traderCfg.CadenceMode != ""])

	// Set API keys based on exchange type (convert EncryptedString to string)
	switch exchangeCfg.ExchangeType {
	case "ninjatrader":
		// CME futures via NT8 CSV bridge. No API key — uses a filesystem path
		// shared with NinjaTrader (see provider/ninjatrader + trader/ninjatrader).
		traderConfig.NinjaTraderDataDir = exchangeCfg.NTDataDir
		if exchangeCfg.NTInstrumentName != "" {
			traderConfig.NinjaTraderSymbol = exchangeCfg.NTInstrumentName
			// P5.4 — bind the trader to its persisted NT8 sub-account (set by
			// account-select). Empty = the AddOn's active account (legacy).
			traderConfig.NinjaTraderAccount = traderCfg.Account
		}
	}

	// Set API keys based on AI model (convert EncryptedString to string)
	switch aiModelCfg.Provider {
	case "qwen":
		traderConfig.QwenKey = string(aiModelCfg.APIKey)
	case "deepseek":
		traderConfig.DeepSeekKey = string(aiModelCfg.APIKey)
	default:
		// For other providers (grok, openai, claude, gemini, kimi, etc.), use CustomAPIKey
		traderConfig.CustomAPIKey = string(aiModelCfg.APIKey)
	}

	// Create trader instance
	at, err := trader.NewAutoTrader(traderConfig, st, traderCfg.UserID)
	if err != nil {
		return fmt.Errorf("failed to create trader: %w", err)
	}

	// (E3, fail-register wave): the trader-row custom_prompt/override_base_prompt
	// loads are GONE — write-only since birth (census #53/#54 [A]: zero readers;
	// the live prompt text is StrategyConfig.CustomPrompt). The columns stay for
	// API/schema compat; physical drop parked for a maintenance window (ruling).

	tm.traders[traderCfg.ID] = at
	logger.Infof("✓ Trader '%s' (%s + %s/%s) loaded to memory", traderCfg.Name, aiModelCfg.Provider, exchangeCfg.ExchangeType, exchangeCfg.AccountName)

	// Auto-start if trader was running before shutdown
	if traderCfg.IsRunning {
		// REL18-C item 5: at BOOT (startPassClaims != nil), one trader per
		// (symbol, account). The first by created_at claims the pair; a second
		// trader marked running on the same pair is refused (not auto-started,
		// marked not-running, P0 + READ boot line). Per-user reloads skip this.
		if !tm.shouldAutoStart(exchangeCfg, traderCfg, st) {
			return nil
		}
		logger.Infof("%s 🔄 Auto-starting trader (was running before shutdown)...", traderLogTag(traderCfg.ID, traderCfg.Name))
		safe.GoNet("trader-autostart", traderCfg.ID, func() {
			trader := at
			traderName := traderCfg.Name
			traderID := traderCfg.ID
			userID := traderCfg.UserID
			if err := trader.Run(); err != nil {
				logger.Warnf("%s trader stopped with error: %v", traderLogTag(traderID, traderName), err)
				// Update database to reflect stopped state
				if st != nil {
					_ = st.Trader().UpdateStatus(userID, traderID, false)
				}
			}
		})
		logger.Infof("%s ✅ Trader auto-started successfully", traderLogTag(traderCfg.ID, traderCfg.Name))
	}

	return nil
}
