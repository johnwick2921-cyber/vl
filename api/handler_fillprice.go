// m447 (7) - owner acknowledgement of far fills.
//
//	POST /api/risk/ack-fill-price  ?symbol=<MNQ>  -> {"symbol","acknowledged"}
//
// Two far fills (a fill price outside the stored 1m bars of its minute) in one
// CME session pause NEW entries for the symbol. The owner acknowledges here after
// looking; entries resume until the next far fill. The next session resumes them
// without any call.

package api

import (
	"net/http"
	"strings"

	"vl/internal/datatruth/checks"
	"vl/logger"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleAckFillPrice(c *gin.Context) {
	symbol := strings.ToUpper(strings.TrimSpace(c.Query("symbol")))
	if symbol == "" {
		SafeBadRequest(c, "symbol is required")
		return
	}
	acked := checks.DefaultFills.Ack(symbol)
	if acked {
		logger.Warnf("🔓 fill_price pause ACKNOWLEDGED by owner for %s - new entries may resume", symbol)
	}
	c.JSON(http.StatusOK, gin.H{"symbol": symbol, "acknowledged": acked})
}
