package api

import (
	"fmt"
	"net/http"

	"vl/config"
	"vl/logger"
	ntwire "vl/provider/ninjatrader"
	"vl/trader"
	ntTrader "vl/trader/ninjatrader"

	"github.com/gin-gonic/gin"
)

// AccountInfo mirrors the wire protocol struct for JSON response.
type AccountInfo struct {
	Name      string `json:"name"`       // e.g. "Sim101", "LiveAcct"
	IsSim     bool   `json:"is_sim"`     // true if a SIM account
	IsCurrent bool   `json:"is_current"` // true if this is the currently selected account
}

// GetAccountsResponse is the response structure for GET /api/accounts.
type GetAccountsResponse struct {
	Current  string        `json:"current"`  // the NT8 connection's streamed current account
	Selected string        `json:"selected"` // the trader's PERSISTED chosen account ("" = none → gated)
	Accounts []AccountInfo `json:"accounts"` // list of all available accounts
}

// SelectAccountRequest is the request structure for POST /api/account/select.
type SelectAccountRequest struct {
	Account string `json:"account" binding:"required"` // e.g. "Sim101"
}

// SelectAccountResponse is the response structure for POST /api/account/select.
type SelectAccountResponse struct {
	Current string `json:"current"` // the newly selected account
	Message string `json:"message"` // status message
}

// handleGetAccounts returns the list of available NT accounts discovered by the
// C# AddOn and the currently selected account. Protected endpoint (?trader_id=).
func (s *Server) handleGetAccounts(c *gin.Context) {
	traderID := c.Query("trader_id")
	if traderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "trader_id query parameter required"})
		return
	}

	// Get the trader to access its NT server instance
	autoTrader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("trader not found: %s", traderID)})
		return
	}

	// Verify trader uses NinjaTrader via TCP (not CSV or other broker)
	tcpServer := extractTCPServer(autoTrader)
	if tcpServer == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "trader does not use NinjaTrader TCP bridge"})
		return
	}

	// Retrieve account list and current account from the TCP server
	accounts, current := tcpServer.GetAccountsList()
	logger.Infof("api/accounts: retrieved %d accounts from TCP server, current=%q", len(accounts), current)

	// Convert internal wire structs to API response structs
	apiAccounts := make([]AccountInfo, len(accounts))
	for i, a := range accounts {
		apiAccounts[i] = AccountInfo{
			Name:      a.Name,
			IsSim:     a.IsSim,
			IsCurrent: a.Name == current,
		}
		logger.Infof("api/accounts: account[%d]=%q is_sim=%v", i, a.Name, a.IsSim)
	}

	// The trader's PERSISTED chosen account (multi-account Stage 1) — "" means the
	// user hasn't picked one yet, so the trader is gated (does not trade). The FE
	// highlights this, not the streamed `current` (which can drift on the stream).
	selected := ""
	if tr, err := s.store.Trader().GetByID(traderID); err == nil && tr != nil {
		selected = tr.Account
	}

	// `current` normally reflects the ONE account the shared connection streams.
	// But GetBalance now serves THIS trader's bound account when that account has
	// its own snapshot (tcp_trader.go decouple). Report that account as `current`
	// here too, so the FE masquerade banner (`current != selected`) stays honest:
	// silent when the on-screen numbers ARE this trader's account, warning only
	// when we genuinely fell back to the streamed account.
	if selected != "" {
		if _, ok := tcpServer.AccountStateFor(selected); ok {
			current = selected
		}
	}

	c.JSON(http.StatusOK, GetAccountsResponse{
		Current:  current,
		Selected: selected,
		Accounts: apiAccounts,
	})
}

// handleSelectAccount switches the selected account to the one specified in the request.
// Server-side SIM guard: only SIM accounts (is_sim == true) are selectable.
// Protected endpoint, requires ?trader_id=.
func (s *Server) handleSelectAccount(c *gin.Context) {
	traderID := c.Query("trader_id")
	if traderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "trader_id query parameter required"})
		return
	}
	userID := c.GetString("user_id")

	var req SelectAccountRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}

	if req.Account == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account name cannot be empty"})
		return
	}

	// Get the trader to access its NT server instance
	autoTrader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("trader not found: %s", traderID)})
		return
	}

	// Verify trader uses NinjaTrader via TCP
	tcpServer := extractTCPServer(autoTrader)
	if tcpServer == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "trader does not use NinjaTrader TCP bridge"})
		return
	}

	// REL18-C O1 — a RUNNING trader's binding must NOT change. Nothing is
	// persisted, no cache reset, no binding change: the running TCPTrader stays
	// bound to its account (the live 00:43 mismatch: the UI showed the new
	// account while the running trader kept trading the old one).
	if autoTrader.HealthIsRunning() {
		SafeConflictWithDetails(c,
			"Stop the trader first, then choose the account, then start it.",
			"account.select.trader_running",
			mapStringPairs("trader_id", traderID))
		return
	}

	// Retrieve the full account list to validate and check SIM status
	accounts, _ := tcpServer.GetAccountsList()
	var targetAccount *ntwire.AccountInfo
	for i := range accounts {
		if accounts[i].Name == req.Account {
			targetAccount = &accounts[i]
			break
		}
	}

	if targetAccount == nil {
		c.JSON(http.StatusBadRequest,
			gin.H{"error": fmt.Sprintf("account '%s' not found in available accounts", req.Account)})
		return
	}

	// SERVER-SIDE SIM GUARD: reject non-SIM accounts
	if !targetAccount.IsSim {
		logger.Warnf("api/account/select: attempted to select non-SIM account %s", req.Account)
		c.JSON(http.StatusBadRequest,
			gin.H{"error": "Non-SIM accounts not selectable for auto-trading"})
		return
	}

	// ALLOW-LIST GUARD (multi-account safety rail): if NT_ALLOWED_ACCOUNTS is set,
	// the chosen account MUST be on it — defense-in-depth so a funded/live account
	// can never be bound even if it somehow passed the SIM check.
	if allow := config.Get().AllowedNTAccounts; len(allow) > 0 {
		permitted := false
		for _, a := range allow {
			if a == req.Account {
				permitted = true
				break
			}
		}
		if !permitted {
			logger.Warnf("api/account/select: %s not on the tradeable allow-list", req.Account)
			c.JSON(http.StatusBadRequest,
				gin.H{"error": fmt.Sprintf("account '%s' is not on the tradeable allow-list", req.Account)})
			return
		}
	}

	// G7 account-select decouple: account select does exactly ONE thing — persist
	// THIS trader's binding. It must NOT flip the shared connection's active/display
	// account, because that is a per-CONNECTION state read by every trader and the
	// record/stream layer, so flipping it for one trader disturbed the OTHER
	// trader's display and (pre-G6) its record attribution. We therefore no longer
	// send SendAccountSelect here: per-order routing already carries this trader's
	// account (tcp_trader.go SignalPayload.Account → the AddOn's per-order routing),
	// balances AND positions now stream for ALL accounts, and records stamp the
	// trader's boundAccount (G6) — so nothing needs the shared active account moved.

	// REL18-C O3a — one trader per (symbol, account): refuse to bind an account
	// that ANOTHER trader already holds on the same symbol (running or not).
	if thisSymbol, _, _ := s.traderSymbolAccount(userID, traderID); thisSymbol != "" && req.Account != "" {
		if otherID, otherName, ok := s.otherTraderBoundToSameSymbolAccount(userID, traderID, thisSymbol, req.Account); ok {
			SafeConflictWithDetails(c,
				fmt.Sprintf("Account %s is already bound to trader %q on the same symbol %s — one trader per (symbol, account). Stop or rebind that trader first.",
					req.Account, otherName, thisSymbol),
				"account.select.account_in_use",
				mapStringPairs("trader_id", traderID, "other_trader", otherName, "other_trader_id", otherID, "account", req.Account, "symbol", thisSymbol))
			return
		}
	}

	//
	// Reset THIS trader's own cached fill/position state so its card refreshes to
	// the (now persisted) binding on the next reload — a self-only effect that never
	// touches the shared stream or any other trader.
	if tcpTrader, ok := autoTrader.GetUnderlyingTrader().(*ntTrader.TCPTrader); ok {
		tcpTrader.ResetAccountState()
		logger.Infof("api/account/select: reset Go cached state for %s", req.Account)
	}

	// PERSIST the pick per-trader so it STICKS: it survives restart and the
	// account_balance stream can't unset it, and the trade gate (runCycle) opens for
	// this trader.
	if err := s.store.Trader().UpdateAccount(userID, traderID, req.Account); err != nil {
		logger.Warnf("api/account/select: failed to persist account %s for trader %s: %v", req.Account, traderID, err)
	} else {
		logger.Infof("api/account/select: persisted chosen account %s for trader %s", req.Account, traderID)
	}

	logger.Infof("api/account/select: switched to account %s (SIM=%v)", req.Account, targetAccount.IsSim)
	c.JSON(http.StatusOK, gin.H{
		"current_account": req.Account,
		"message":         fmt.Sprintf("switched to account %s", req.Account),
	})
}

// extractTCPServer attempts to extract a TCPServer from an AutoTrader.
// Returns the server if the trader uses NinjaTrader TCP bridge, otherwise nil.
func extractTCPServer(autoTrader *trader.AutoTrader) *ntwire.TCPServer {
	if autoTrader == nil {
		return nil
	}

	// Get the underlying broker trader (Trader interface)
	underlyingTrader := autoTrader.GetUnderlyingTrader()
	if underlyingTrader == nil {
		return nil
	}

	// Try to assert as TCPTrader (NinjaTrader TCP bridge)
	if tcpTrader, ok := underlyingTrader.(*ntTrader.TCPTrader); ok {
		return tcpTrader.GetServer()
	}

	return nil
}
