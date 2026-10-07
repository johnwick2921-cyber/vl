package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// handleMentorTruth GET /api/traders/:id/mentor-truth — MENTOR-TRUTH PANEL
// (release #10). Read-only: the live evaluator state the planner page and the
// trader dashboard render as "what actually trades" — the 4h/1h/5m trigger
// directions, the HTF gate verdict, the mentor key levels in effect (with
// today's visits), the history-depth snapshot, and the window / day-stop state.
// When mentor mode is OFF the payload is {"enabled": false} and the frontend
// hides the card.
func (s *Server) handleMentorTruth(c *gin.Context) {
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
	panel, _ := at.MentorTruthSnapshot(time.Now())
	c.JSON(http.StatusOK, panel)
}
