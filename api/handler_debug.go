package api

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"vl/config"
	ntTrader "vl/trader/ninjatrader"
)

// runtimeSymbolsEnabled is the P5.3 feature flag (NT_RUNTIME_SYMBOLS=true/1).
// OFF by default → the runtime add/remove endpoints refuse and the system
// behaves as the static P5.2 symbols-list (the rollback position).
func runtimeSymbolsEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("NT_RUNTIME_SYMBOLS")))
	return v == "true" || v == "1"
}

// ntFromQuery resolves the request's trader to the NT8 TCP trader (shared by
// the P5.3 symbol endpoints; mirrors the debug-handler pattern).
func (s *Server) ntFromQuery(c *gin.Context) (*ntTrader.TCPTrader, bool) {
	if cfg := config.Get(); cfg == nil {
		SafeBadRequest(c, "futures-mode only")
		return nil, false
	}
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return nil, false
	}
	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return nil, false
	}
	nt, ok := trader.GetUnderlyingTrader().(*ntTrader.TCPTrader)
	if !ok {
		SafeBadRequest(c, "trader is not an NT8 TCP trader (NT_TRANSPORT=tcp required)")
		return nil, false
	}
	return nt, true
}

// handleNTSymbolsList (GET) — the current bar-subscription set + lifecycle
// states (always available; read-only).
func (s *Server) handleNTSymbolsList(c *gin.Context) {
	nt, ok := s.ntFromQuery(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"primary":         nt.BarsPrimarySymbol(),
		"states":          nt.BarsSubscriptionStates(),
		"runtime_enabled": runtimeSymbolsEnabled(),
	})
}

// handleNTSymbolsAdd (POST ?symbol=NQ) — subscribe an extra root at runtime.
// P5.3 feature-flagged; bars-only (no trading path).
func (s *Server) handleNTSymbolsAdd(c *gin.Context) {
	if !runtimeSymbolsEnabled() {
		SafeBadRequest(c, "runtime symbol management is disabled (set NT_RUNTIME_SYMBOLS=true)")
		return
	}
	nt, ok := s.ntFromQuery(c)
	if !ok {
		return
	}
	symbol := c.Query("symbol")
	if symbol == "" {
		SafeBadRequest(c, "symbol is required")
		return
	}
	if err := nt.RuntimeSubscribeBars(symbol); err != nil {
		SafeInternalError(c, "NT runtime subscribe", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "symbol": symbol, "state": "pending"})
}

// handleNTSymbolsRemove (DELETE ?symbol=NQ) — unsubscribe an extra root at
// runtime (the PRIMARY trading symbol is refused server-side) + purge its
// cached bars. P5.3 feature-flagged.
func (s *Server) handleNTSymbolsRemove(c *gin.Context) {
	if !runtimeSymbolsEnabled() {
		SafeBadRequest(c, "runtime symbol management is disabled (set NT_RUNTIME_SYMBOLS=true)")
		return
	}
	nt, ok := s.ntFromQuery(c)
	if !ok {
		return
	}
	symbol := c.Query("symbol")
	if symbol == "" {
		SafeBadRequest(c, "symbol is required")
		return
	}
	if err := nt.RuntimeUnsubscribeBars(symbol); err != nil {
		SafeInternalError(c, "NT runtime unsubscribe", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "symbol": symbol, "state": "unsubscribed"})
}

// handleNTBarsUnsubscribe drops an EXTRA bar subscription at runtime (P5.1
// dispose-one proof / P5.3 building block). Futures-mode only; the primary
// (trading) symbol is refused at the TCP-server layer, so this can never tear
// down the live trader's feed. Bars-only — no order/position path involved.
func (s *Server) handleNTBarsUnsubscribe(c *gin.Context) {
	if cfg := config.Get(); cfg == nil {
		SafeBadRequest(c, "bars unsubscribe is futures-mode only")
		return
	}
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
	nt, ok := trader.GetUnderlyingTrader().(*ntTrader.TCPTrader)
	if !ok {
		SafeBadRequest(c, "trader is not an NT8 TCP trader (NT_TRANSPORT=tcp required)")
		return
	}
	symbol := c.Query("symbol")
	if symbol == "" {
		SafeBadRequest(c, "symbol is required")
		return
	}
	if err := nt.DebugUnsubscribeBars(symbol); err != nil {
		SafeInternalError(c, "NT bars unsubscribe", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "symbol": symbol})
}

// handleNTTestTrade places a deterministic 1-contract SIM bracket order on the
// NT trader for end-to-end dashboard proof (Plan 4.11: signal → NT8 SIM fill →
// position). Futures/SIM only; bypasses the AI + risk gate. NOT a trading path
// — a debug harness so the operator can see a real position + balance move on
// the dashboard without depending on an AI decision.
func (s *Server) handleNTTestTrade(c *gin.Context) {
	if cfg := config.Get(); cfg == nil {
		SafeBadRequest(c, "test trade is futures-mode only")
		return
	}
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
	if _, ok := trader.GetUnderlyingTrader().(*ntTrader.TCPTrader); !ok {
		SafeBadRequest(c, "trader is not an NT8 TCP trader (NT_TRANSPORT=tcp required)")
		return
	}

	side := c.DefaultQuery("side", "long")
	if side != "long" && side != "short" {
		SafeBadRequest(c, "side must be long or short")
		return
	}

	// m445: through the trader, never the broker adapter directly — the
	// data-integrity verdict (missing bars) gates this MARKET entry too.
	result, err := trader.DebugNTTestTrade(side)
	if err != nil {
		if strings.Contains(err.Error(), "data_integrity") {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		SafeInternalError(c, "NT test trade", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "side": side, "result": result})
}
