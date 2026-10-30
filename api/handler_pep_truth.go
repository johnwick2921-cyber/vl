package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"vl/store"
)

// handlePepTruth GET /api/traders/:id/pep-truth — the pep dashboard card.
// Read-only: the box, the signal state, trades today, the entry's stop/target
// and the countdown to pep's own flat time, every field READ from the code that
// enforces it (trader.PepTruthSnapshot). pep mode OFF -> {"enabled":false,...}
// with the trader's mode so the dashboard can paint the chip without a second
// call.
func (s *Server) handlePepTruth(c *gin.Context) {
	traderID := strings.TrimSpace(c.Param("id"))
	if traderID == "" {
		traderID = strings.TrimSpace(c.Query("trader_id"))
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if !s.traderOwnedBy(c.GetString("user_id"), traderID) {
		SafeUnauthorized(c)
		return
	}
	at, err := s.traderManager.GetTrader(traderID)
	if err != nil || at == nil {
		SafeNotFound(c, "Trader")
		return
	}
	c.JSON(http.StatusOK, at.PepTruthSnapshot(time.Now()))
}

// tradingModeFields is the additive mode block /api/my-traders and
// /api/traders/:id/config carry: trading_mode (ai | mentor | pep | conflict) and,
// for pep, the style. A strategy that cannot be read reads as "ai" with no style
// (the dashboard paints no chip).
func tradingModeFields(cfg *store.StrategyConfig) (mode, style string) {
	mode = cfg.TradingMode()
	if mode == store.TradingModePep {
		style = store.ResolvePep(cfg).Style
	}
	return mode, style
}
