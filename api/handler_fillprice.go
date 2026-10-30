// m447 (7) - owner acknowledgement of far fills.
//
//	POST /api/risk/ack-fill-price  ?symbol=<MNQ>[&account=<Sim101>]  -> {"symbol","account","acknowledged"}
//
// Two far fill events (a fill price outside the stored 1m bars of its minute) in
// one CME session on one account pause NEW entries of the traders on that account
// (m451: per account, not per symbol). The owner acknowledges here after looking -
// the account named in the pause text, or every account of the symbol when none is
// given; entries resume until the next far fill. The next session resumes them
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
	account := strings.TrimSpace(c.Query("account"))
	var acked, durable bool
	if account != "" {
		acked, durable = checks.DefaultFills().AckAccountDurable(symbol, account)
	} else {
		acked, durable = checks.DefaultFills().AckDurable(symbol)
	}
	if acked {
		logger.Warnf("🔓 fill_price pause ACKNOWLEDGED by owner for %s (account %q) - new entries may resume", symbol, account)
		if !durable {
			logger.Errorf("fill_price ack for %s (account %q) is in memory only - the state file did not take it inside 2s; a restart before the disk catches up re-pauses (acknowledge again then)", symbol, account)
		}
	}
	// persisted=false: the acknowledgement is live but NOT on disk yet (additive field).
	c.JSON(http.StatusOK, gin.H{"symbol": symbol, "account": account, "acknowledged": acked, "persisted": durable})
}
