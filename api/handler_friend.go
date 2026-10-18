package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// W-FRIEND-HOSTED (CTO ruling 2026-10-18 07:36): GET /api/friend/flat is the ONE
// friend-facing probe DS-105's updater may use to decide a restart. It answers
// whether EVERY trader in this friend instance is flat and nothing else — no
// symbols, sizes, prices or accounts. It exists ONLY in friend mode (the route
// is never registered on the owner's bot, so it 404s there), and it sits on the
// tunnel-bound friend port (S1/S2), not on the owner's.
func (s *Server) handleFriendFlat(c *gin.Context) {
	flat := true
	for _, at := range s.traderManager.GetAllTraders() {
		positions, err := at.GetPositions()
		if err != nil {
			// A probe error = NOT flat (fail closed): an unreadable position
			// state must never read as "safe to restart".
			flat = false
			break
		}
		if !flatFromPositions(positions) {
			flat = false
			break
		}
	}
	c.JSON(http.StatusOK, gin.H{"flat": flat, "at": time.Now().UnixMilli()})
}

// flatFromPositions reports flat when no position carries a non-zero quantity.
// A position map missing "quantity" cannot prove non-flat, so it reads flat —
// the fail-closed side is the GetPositions error, handled by the caller.
func flatFromPositions(positions []map[string]interface{}) bool {
	for _, p := range positions {
		if qty, ok := p["quantity"].(float64); ok && qty != 0 {
			return false
		}
	}
	return true
}
