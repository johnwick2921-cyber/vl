package api

import (
	"errors"
	"math"
	"net/http"
	"strconv"

	"gorm.io/gorm"

	"vl/logger"
	"vl/market"
	storepkg "vl/store"

	"github.com/gin-gonic/gin"
	"time"
	"vl/kernel"
)

// handleTraderList Trader list
func (s *Server) handleTraderList(c *gin.Context) {
	userID := c.GetString("user_id")
	traders, err := s.store.Trader().List(userID)
	if err != nil {
		SafeInternalError(c, "Failed to get trader list", err)
		return
	}

	result := make([]map[string]interface{}, 0, len(traders))
	for _, trader := range traders {
		// Get real-time running status
		isRunning := trader.IsRunning
		if at, err := s.traderManager.GetTrader(trader.ID); err == nil {
			status := at.GetStatus()
			if running, ok := status["is_running"].(bool); ok {
				isRunning = running
			}
		}

		// Get strategy name if strategy_id is set
		var strategyName string
		tradingMode, pepStyle := storepkg.TradingModeAI, ""
		if trader.StrategyID != "" {
			if strategy, err := s.store.Strategy().Get(userID, trader.StrategyID); err == nil {
				strategyName = strategy.Name
				// PEP MODE: the dashboard's mode chip (additive fields).
				if cfg, perr := strategy.ParseConfig(); perr == nil {
					tradingMode, pepStyle = tradingModeFields(cfg)
				}
			}
		}

		// Return complete AIModelID (e.g. "admin_deepseek"), don't truncate
		// Frontend needs complete ID to verify model exists (consistent with handleGetTraderConfig)
		result = append(result, map[string]interface{}{
			"trader_id":           trader.ID,
			"trader_name":         trader.Name,
			"ai_model":            trader.AIModelID, // Use complete ID
			"exchange_id":         trader.ExchangeID,
			"is_running":          isRunning,
			"show_in_competition": trader.ShowInCompetition,
			"initial_balance":     trader.InitialBalance,
			"strategy_id":         trader.StrategyID,
			"strategy_name":       strategyName,
			"trading_mode":        tradingMode, // ai | mentor | pep | conflict
			"pep_style":           pepStyle,    // "A" | "E" | "Custom" when trading_mode is pep, else ""
		})
	}

	c.JSON(http.StatusOK, result)
}

// handleGetTraderConfig Get trader detailed configuration
func (s *Server) handleGetTraderConfig(c *gin.Context) {
	userID := c.GetString("user_id")
	traderID := c.Param("id")

	if traderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Trader ID cannot be empty"})
		return
	}

	fullCfg, err := s.store.Trader().GetFullConfig(userID, traderID)
	if err != nil {
		SafeNotFound(c, "Trader config")
		return
	}
	traderConfig := fullCfg.Trader

	// Get real-time running status
	isRunning := traderConfig.IsRunning
	if at, err := s.traderManager.GetTrader(traderID); err == nil {
		status := at.GetStatus()
		if running, ok := status["is_running"].(bool); ok {
			isRunning = running
		}
	}

	// Return complete model ID without conversion, consistent with frontend model list
	aiModelID := traderConfig.AIModelID

	result := map[string]interface{}{
		"trader_id":             traderConfig.ID,
		"trader_name":           traderConfig.Name,
		"ai_model":              aiModelID,
		"exchange_id":           traderConfig.ExchangeID,
		"strategy_id":           traderConfig.StrategyID,
		"initial_balance":       traderConfig.InitialBalance,
		"scan_interval_minutes": traderConfig.ScanIntervalMinutes,
		"btc_eth_leverage":      traderConfig.BTCETHLeverage,
		"altcoin_leverage":      traderConfig.AltcoinLeverage,
		"trading_symbols":       traderConfig.TradingSymbols,
		"custom_prompt":         traderConfig.CustomPrompt,
		"override_base_prompt":  traderConfig.OverrideBasePrompt,
		"is_cross_margin":       traderConfig.IsCrossMargin, "is_running": isRunning,
	}
	// PEP MODE: the trader's trading mode (additive fields).
	result["trading_mode"], result["pep_style"] = storepkg.TradingModeAI, ""
	if fullCfg.Strategy != nil {
		if cfg, perr := fullCfg.Strategy.ParseConfig(); perr == nil {
			result["trading_mode"], result["pep_style"] = tradingModeFields(cfg)
		}
	}

	c.JSON(http.StatusOK, result)
}

// handleStatus System status
func (s *Server) handleStatus(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}

	status := trader.GetStatus()
	c.JSON(http.StatusOK, status)
}

// handleAccount Account information
func (s *Server) handleAccount(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}

	logger.Infof("📊 Received account info request [%s]", trader.GetName())
	account, err := trader.GetAccountInfo()
	if err != nil {
		SafeInternalError(c, "Get account info", err)
		return
	}
	// P&L-TRUTH WAVE (2026-09-01): the dashboard header used to show the
	// NT8-native daily_pnl (permanently 0.00) beside a +212.00 ledger day
	// total. The ledger figure — the SAME rule as the position-history footer
	// (rows closed this session day, unknown-P&L / test-seam reasons out,
	// strict pnl_corrected, unresolved counted + excluded) — now rides beside
	// it, labelled. The NT8 fields stay for their other readers.
	//
	// R22 PNL-TRUTH: ONE "today" — the CME session day that starts 17:00 CT
	// (kernel.CMESessionDayStart, the same boundary every risk gate counts
	// from). This used to be the CT CALENDAR day, so from 17:00 to 24:00 every
	// evening the header's "day" disagreed with the gates' day.
	// Manual NT8 rows (owner ruling R1) are excluded from the bot figures and
	// carried as their own counted line.
	if s.store != nil {
		dayStart, dayEnd := ledgerDayWindow(time.Now())
		ledger, lerr := s.store.Position().GetLedgerDayTotal(traderID, c.Query("account"), dayStart.UnixMilli(), dayEnd.UnixMilli())
		if lerr != nil {
			logger.Warnf("🧾 ledger day total unavailable for %s: %v", traderID, lerr)
			account["ledger_day_status"] = "UNRESOLVED: " + lerr.Error()
		} else {
			account["ledger_day_pnl"] = ledger.Total
			account["ledger_day_resolved"] = ledger.Resolved
			account["ledger_day_unresolved"] = ledger.Unresolved
			account["ledger_day_manual_excluded"] = ledger.ManualTrades
			account["ledger_day_manual_excluded_pnl"] = ledger.ManualPnL
			account["ledger_day_date"] = dayStart.Format("2006-01-02")
			account["ledger_day_start_ms"] = dayStart.UnixMilli()
			account["ledger_day_source"] = "trader_positions.pnl_corrected (strict; bot rows only; CME session day from 17:00 CT; same rule as the position-history footer)"
		}
		s.attachBotRecord(account, traderID, c.Query("account"))
	}

	logger.Infof("✓ Returning account info [%s]: equity=%.2f, available=%.2f, pnl=%.2f (%.2f%%)",
		trader.GetName(),
		account["total_equity"],
		account["available_balance"],
		account["total_pnl"],
		account["total_pnl_pct"])
	c.JSON(http.StatusOK, account)
}

// attachBotRecord puts THE BOT'S RECORD on the /api/account response (the
// "Bot P&L (record)" card), scoped to acct ("" = trader-wide), with its scope
// and — when scoped — the bot rows on other accounts it leaves out, counted
// (RT-7). handleAccount is its one caller.
func (s *Server) attachBotRecord(account map[string]interface{}, traderID, acct string) {
	// THE BOT'S RECORD — all-time, bot rows only (the dashboard stats / AI
	// track record figure), with the manual rows as their own line.
	if rec, rerr := s.store.Position().GetFullStats(traderID, acct); rerr != nil {
		logger.Warnf("🧾 bot record unavailable for %s: %v", traderID, rerr)
		account["bot_record_status"] = "UNRESOLVED: " + rerr.Error()
	} else {
		account["bot_record_pnl"] = rec.TotalPnL
		account["bot_record_trades"] = rec.TotalTrades
		account["bot_record_unresolved"] = rec.UnresolvedExcluded
		account["manual_excluded_trades"] = rec.ManualExcludedTrades
		account["manual_excluded_pnl"] = rec.ManualExcludedPnL
		account["manual_excluded_line"] = rec.ManualExcludedLine
		// RT-7 (release #24, N1): this card is scoped to the selected
		// account while Position History's total is trader-wide. Which
		// scope is "the record" is an OWNER DECISION; until then neither
		// figure changes and each says its scope, with the bot rows the
		// scoped figure leaves out COUNTED from the same reader
		// (trader-wide GetFullStats minus this one), never a literal.
		if acct == "" {
			account["bot_record_scope"] = "all accounts"
		} else if all, aerr := s.store.Position().GetFullStats(traderID); aerr != nil {
			account["bot_record_scope"] = "this account"
			account["bot_record_other_accounts_status"] = "UNRESOLVED: " + aerr.Error()
		} else {
			account["bot_record_scope"] = "this account"
			account["bot_record_other_accounts_trades"] = all.TotalTrades - rec.TotalTrades
			account["bot_record_other_accounts_pnl"] = math.Round((all.TotalPnL-rec.TotalPnL)*100) / 100
			account["bot_record_other_accounts_unresolved"] = all.UnresolvedExcluded - rec.UnresolvedExcluded
		}
	}
}

// ledgerDayWindow is the dashboard LEDGER_DAY window: the CME session day
// that contains now, [17:00 CT, next 17:00 CT) — the boundary every risk gate
// counts from (R22: it used to be the CT calendar day).
func ledgerDayWindow(now time.Time) (start, end time.Time) {
	start = kernel.CMESessionDayStart(now)
	return start, kernel.NextSessionRollCT(start)
}

// handlePositions Position list
func (s *Server) handlePositions(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}

	positions, err := trader.GetPositions()
	if err != nil {
		SafeInternalError(c, "Get positions", err)
		return
	}

	c.JSON(http.StatusOK, positions)
}

// handlePositionHistory Historical closed positions with statistics
func (s *Server) handlePositionHistory(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}

	// Get optional query parameters
	limitStr := c.DefaultQuery("limit", "100")
	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 500 {
		limit = l
	}

	// Get store
	store := trader.GetStore()
	if store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Store not available"})
		return
	}

	// Get closed positions (ITEM 2 per-account: scope the list to the selected
	// account when provided; aggregate stats below remain trader-scoped pending
	// trade data — there are no closed positions yet to exercise them).
	positions, err := store.Position().GetHistoryPositions(trader.GetID(), limit, c.Query("account"))
	if err != nil {
		SafeInternalError(c, "Get position history", err)
		return
	}

	// Get statistics
	stats, _ := store.Position().GetFullStats(trader.GetID())

	// Get symbol stats
	symbolStats, _ := store.Position().GetSymbolStats(trader.GetID(), 10)

	// Get direction stats
	directionStats, _ := store.Position().GetDirectionStats(trader.GetID())

	// Canon class 40: a NULL pnl_corrected is UNRESOLVED — every row carries an
	// explicit pnl_status and the count is shown, so no consumer can render a
	// NULL as 0 or blank.
	rows, unresolved := annotatePositionHistory(positions)

	c.JSON(http.StatusOK, gin.H{
		"positions":        rows,
		"unresolved_count": unresolved,
		"stats":            stats,
		"symbol_stats":     symbolStats,
		"direction_stats":  directionStats,
		// RT-7 (release #24, N1): stats above are TRADER-WIDE (GetFullStats
		// with no account), while the dashboard's "Bot P&L (record)" card is
		// scoped to the selected account. The scope rides with the figure.
		"stats_scope": "all accounts",
		// R22: the ONE "today" — the CME session day from 17:00 CT
		// (kernel.CMESessionDayStart, the risk gates' boundary). The footer's
		// day total is computed from this instant, never from a browser clock.
		"session_day_start_ms": kernel.CMESessionDayStart(time.Now()).UnixMilli(),
	})
}

// positionHistoryRow is the API shape of one closed position: the store row
// plus its corrected-column status. Canon class 40: pnl_corrected NULL is
// UNRESOLVED — surfaced explicitly so no consumer can render it as 0/blank.
type positionHistoryRow struct {
	*storepkg.TraderPosition
	PnlStatus string `json:"pnl_status"` // "resolved" | "unresolved"
	// m426 (E3 reader): these shadow the embedded row's fields in the JSON so a
	// legacy second-valued row (ids 691-697) reaches the UI in milliseconds via
	// store.PositionEpochMs instead of rendering as 1970.
	UpdatedAt int64 `json:"updated_at"`
	CreatedAt int64 `json:"created_at"`
}

// annotatePositionHistory stamps every row with its corrected-column status and
// counts the unresolved ones (canon class 40: NULL is UNRESOLVED, excluded, and
// the COUNT is shown — a NULL row must read "unresolved", never 0 or blank).
func annotatePositionHistory(positions []*storepkg.TraderPosition) ([]positionHistoryRow, int) {
	rows := make([]positionHistoryRow, 0, len(positions))
	unresolved := 0
	for _, p := range positions {
		status := "resolved"
		if p == nil || p.PnlCorrected == nil {
			status = "unresolved"
			unresolved++
		} else if p.Status == "OPEN" {
			// m232: an OPEN row that booked a partial leg — label it "partial" so
			// the UI shows the realized partial P&L, not a blank.
			status = "partial"
		}
		row := positionHistoryRow{TraderPosition: p, PnlStatus: status}
		if p != nil {
			row.UpdatedAt = p.UpdatedAtMs()
			row.CreatedAt = storepkg.PositionEpochMs(p.CreatedAt)
		}
		rows = append(rows, row)
	}
	return rows, unresolved
}

// handleTrades Historical trades list
func (s *Server) handleTrades(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}

	// Get optional query parameters
	symbol := c.Query("symbol")
	limitStr := c.DefaultQuery("limit", "100")
	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	if symbol != "" {
		symbol = market.Normalize(symbol)
	}

	// Get trades from store
	store := trader.GetStore()
	if store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Store not available"})
		return
	}

	allTrades, err := store.Position().GetRecentTrades(trader.GetID(), limit)
	if err != nil {
		SafeInternalError(c, "Get trades", err)
		return
	}

	// Filter by symbol if specified
	if symbol != "" {
		var result []interface{}
		for _, trade := range allTrades {
			if trade.Symbol == symbol {
				result = append(result, trade)
			}
		}
		c.JSON(http.StatusOK, result)
		return
	}

	c.JSON(http.StatusOK, allTrades)
}

// handleOrders Order list (all orders including open, close, stop loss, take profit, etc.)
func (s *Server) handleOrders(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}

	// Get optional query parameters
	symbol := c.Query("symbol")
	statusFilter := c.Query("status") // NEW, FILLED, CANCELED, etc.
	limitStr := c.DefaultQuery("limit", "100")
	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	if symbol != "" {
		symbol = market.Normalize(symbol)
	}

	// Get orders from store
	store := trader.GetStore()
	if store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Store not available"})
		return
	}

	// Get orders with filters applied at database level
	orders, err := store.Order().GetTraderOrdersFiltered(trader.GetID(), symbol, statusFilter, limit)
	if err != nil {
		SafeInternalError(c, "Get orders", err)
		return
	}

	c.JSON(http.StatusOK, orders)
}

// handleOrderFills Order fill details (all fills for a specific order)
func (s *Server) handleOrderFills(c *gin.Context) {
	orderIDStr := c.Param("id")
	orderID, err := strconv.ParseInt(orderIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order ID"})
		return
	}

	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	// Historical fills belong to the authenticated trader even when its
	// execution engine is stopped. Never query the shared store by order ID alone.
	fills, err := s.store.Order().GetTraderOrderFills(traderID, orderID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		SafeNotFound(c, "Order")
		return
	}
	if err != nil {
		SafeInternalError(c, "Get order fills", err)
		return
	}

	c.JSON(http.StatusOK, fills)
}

// handleOpenOrders Get open orders (pending SL/TP) from exchange
// handleCutoverGate (CLASS 33, 2026-09-02) answers the FIVE-leg pre-cutover
// gate in ONE payload so an agent cannot quote four legs and skip the fifth.
// Leg 4 now reads the armed_orders ledger (it was a stub returning empty and
// passed vacuously at every cutover 35 → 41); leg 5 is in-flight planner work,
// which the rite never checked at all (2026-08-31 17:34 CT: a kill landed on
// attempt 3/3 and the chain died silently).
func (s *Server) handleCutoverGate(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}
	at, err := s.traderManager.GetTrader(traderID)
	if err != nil || at == nil {
		SafeNotFound(c, "Trader")
		return
	}
	c.JSON(http.StatusOK, at.CutoverGateStatus())
}

func (s *Server) handleOpenOrders(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}

	// Get symbol parameter (required for exchange query)
	symbol := c.Query("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol parameter is required"})
		return
	}

	// Normalize symbol
	symbol = market.Normalize(symbol)

	// Get open orders from exchange
	openOrders, err := trader.GetOpenOrders(symbol)
	if err != nil {
		SafeInternalError(c, "Get open orders", err)
		return
	}

	c.JSON(http.StatusOK, openOrders)
}
