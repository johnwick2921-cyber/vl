// Plan 4 Task 23 — risk endpoints (force-flat + risk status).
//
// Exposes two endpoints under the protected /api group:
//   POST /api/risk/force-flat  — operator-initiated emergency flatten
//   GET  /api/risk/status      — current risk-limit + account state snapshot
//
// The force-flat path delegates to kernel.MaybeForceFlat with a thin
// ForceFlatSignaler adapter wrapping the trader's underlying ninjatrader
// CSVWriter. Other broker types are not
// wired and return {triggered:false, reason:"trader is not a ninjatrader
// CSV bridge"} so the endpoint stays safe to call on any trader.

package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"vl/kernel"
	"vl/logger"
	"vl/provider/ninjatrader"
	"vl/trader"
	ntTrader "vl/trader/ninjatrader"

	"github.com/gin-gonic/gin"
)

// forceFlatSignalAdapter wraps a ninjatrader.CSVWriter into a kernel.ForceFlatSignaler.
//
// On force-flat we cannot literally tell NT to close a position via the CSV
// bridge (the bridge has no "close" command in its protocol), but we CAN:
//  1. Overwrite trade_signals.csv with a deliberately invalid sentinel row so
//     NT consumes it and stops acting on any pending signal.
//  2. Reset our internal SL/TP cache so future entries require explicit
//     operator action (handled by the caller after ForceFlat returns).
//
// Plan 1 SIM-mode acceptable: the operator MUST still manually flatten on the
// NT chart. This endpoint records the intent and resets the bot's daily PnL
// window so we don't keep submitting signals after the kill switch trips.
type forceFlatSignalAdapter struct {
	writer    *ninjatrader.CSVWriter
	flattened int
}

// ForceFlat implements kernel.ForceFlatSignaler. Returns nil on best-effort
// success. The actual position close is operator-initiated on the NT chart.
// When writer is nil (Plan 4 v1 — the *ntTrader.Trader doesn't yet expose its
// internal CSVWriter), we still record the intent and let the caller's
// ResetDailyPnL side-effect halt the trader loop.
func (a *forceFlatSignalAdapter) ForceFlat(traderID string) error {
	if a == nil {
		return fmt.Errorf("force-flat: nil adapter")
	}
	a.flattened++
	if a.writer == nil {
		logger.Warnf("🔴 force-flat adapter: trader=%s signaled (no CSV writer wired — operator must manually flatten on NT chart)", traderID)
		return nil
	}
	logger.Warnf("🔴 force-flat adapter: trader=%s signaled (operator must manually flatten on NT chart)", traderID)
	return nil
}

// forceFlatResponse is the JSON shape returned by POST /api/risk/force-flat.
type forceFlatResponse struct {
	Triggered bool   `json:"triggered"`
	TraderID  string `json:"trader_id"`
	// PositionsFlattened is READ, never a literal (r22, T33 — it used to be a
	// hard-coded 1): on the TCP bridge, the open positions NT8 reported for
	// this trader's bound account when the flatten was SENT. null = that read
	// failed, so the count is unknown. NT8 confirms each close separately
	// (position_close).
	PositionsFlattened *int   `json:"positions_flattened"`
	TimestampUTC       string `json:"timestamp_utc"`
	LogMessage         string `json:"log_message"`
	Reason             string `json:"reason,omitempty"`
}

// handleForceFlat handles POST /api/risk/force-flat.
//
// Body or query: trader_id=<string>
// On non-ninjatrader brokers returns triggered:false with reason.
func (s *Server) handleForceFlat(c *gin.Context) {
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		// Try body fallback for clients that POST JSON.
		var body struct {
			TraderID string `json:"trader_id"`
		}
		_ = c.ShouldBindJSON(&body)
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}

	at, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}

	logger.Warnf("🔴 FORCE FLAT initiated by user (trader=%s)", traderID)

	underlying := at.GetUnderlyingTrader()

	// TCP bridge: real flatten via the AddOn (closes at market + cancels the
	// protective bracket). CloseLong flattens regardless of the held side.
	if ntTCP, ok := underlying.(*ntTrader.TCPTrader); ok {
		var openAtSend *int
		if positions, perr := ntTCP.GetPositions(); perr == nil {
			n := len(positions)
			openAtSend = &n
		}
		if _, err := ntTCP.CloseLong("", 0); err != nil {
			c.JSON(http.StatusOK, forceFlatResponse{
				Triggered:    false,
				TraderID:     traderID,
				TimestampUTC: time.Now().UTC().Format(time.RFC3339),
				Reason:       "flatten command failed: " + err.Error(),
				LogMessage:   "force-flat: NT TCP flatten send failed",
			})
			return
		}
		// Also trip the kill-switch reset side-effect, as the CSV path does.
		// r22 (T4): the reset is THIS trader's only — MaybeForceFlat used to
		// call ResetDailyPnL(), which lifted EVERY trader's daily-loss trip.
		// ClearDailyLossLatch also drops this trader's persisted latch so a
		// restart cannot re-arm what the owner just cleared. If this trader's
		// own loss still stands, its gate re-trips on the next 60s beat.
		_ = kernel.MaybeForceFlat(traderID, &forceFlatSignalAdapter{writer: nil})
		at.ClearDailyLossLatch("Emergency Flat")
		msg := "force-flat: flatten SENT to NinjaTrader (TCP bridge) for this trader's bound account — open positions at send unknown (position read failed); NT8 confirms each close"
		if openAtSend != nil {
			msg = fmt.Sprintf("force-flat: flatten SENT to NinjaTrader (TCP bridge) for this trader's bound account — %d open position(s) at send; NT8 confirms each close", *openAtSend)
		}
		c.JSON(http.StatusOK, forceFlatResponse{
			Triggered:          true,
			TraderID:           traderID,
			PositionsFlattened: openAtSend,
			TimestampUTC:       time.Now().UTC().Format(time.RFC3339),
			LogMessage:         msg,
		})
		return
	}

	// Only the ninjatrader CSV bridge has a writer we can adapt today.
	ntInstance, ok := underlying.(*ntTrader.Trader)
	if !ok {
		c.JSON(http.StatusOK, forceFlatResponse{
			Triggered:    false,
			TraderID:     traderID,
			TimestampUTC: time.Now().UTC().Format(time.RFC3339),
			Reason:       "trader is not a ninjatrader CSV bridge",
			LogMessage:   fmt.Sprintf("force-flat skipped: broker=%s does not support CSV-bridge flatten", at.GetExchange()),
		})
		_ = ntInstance // suppress unused
		return
	}

	// We don't (yet) expose the *CSVWriter from *ntTrader.Trader; pass nil to
	// MaybeForceFlat to log the no-op + reset PnL. The kernel will warn but
	// not error so the endpoint is safe to call.
	adapter := &forceFlatSignalAdapter{writer: nil}
	if err := kernel.MaybeForceFlat(traderID, adapter); err != nil {
		SafeInternalError(c, "Force flat", err)
		return
	}
	at.ClearDailyLossLatch("Emergency Flat (CSV bridge)")

	c.JSON(http.StatusOK, forceFlatResponse{
		Triggered:          true,
		TraderID:           traderID,
		PositionsFlattened: &adapter.flattened,
		TimestampUTC:       time.Now().UTC().Format(time.RFC3339),
		LogMessage:         fmt.Sprintf("force-flat invoked for trader=%s (manual close required on NT chart)", traderID),
	})
}

// riskStatusResponse is the JSON shape returned by GET /api/risk/status.
type riskStatusResponse struct {
	TraderID            string  `json:"trader_id"`
	DailyPnLUSD         float64 `json:"daily_pnl_usd"`
	DailyLossLimitUSD   float64 `json:"daily_loss_limit_usd"`
	ConcurrentTrades    int     `json:"concurrent_trades"`
	MaxConcurrentTrades int     `json:"max_concurrent_trades"`
	CurrentNotionalUSD  float64 `json:"current_notional_usd"`
	MaxNotionalUSD      float64 `json:"max_notional_usd"`
	KillSwitchArmed     bool    `json:"kill_switch_armed"`
	LastResetUTC        string  `json:"last_reset_utc"`
	// NotEnforced names the fields above that a reader would take for risk
	// controls and that nothing on this response actually enforces (D6d,
	// 2026-09-06). A number that looks like a limit and gates nothing is worse
	// than no number, because it is acted on.
	NotEnforced []string `json:"not_enforced,omitempty"`
	// DailyLossGate is the ENFORCED daily-loss gate (r22 RISK-GATES): the
	// resolved posture, the latch and the bot-only P&L it judges. Read-only —
	// a status read never trips anything.
	DailyLossGate trader.DailyLossGateStatus `json:"daily_loss_gate"`
}

// handleRiskStatus handles GET /api/risk/status?trader_id=xxx.
//
// Returns the current risk-limit configuration plus a snapshot of the
// trader's account/positions so the UI can render the EmergencyFlatButton
// state alongside live numbers.
func (s *Server) handleRiskStatus(c *gin.Context) {
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}

	at, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}

	limits := kernel.LoadRiskLimitsFromConfig()

	// D6(d), desk-strip wave 2026-09-06 — WHAT ON THIS RESPONSE IS ACTUALLY
	// ENFORCED. The pre-prompt gate calls CheckPreTrade(0, positions, 0, 0)
	// (kernel/engine_analysis.go:118-136): zero for both notional arguments and
	// zero for pnl. So at that gate ONLY the concurrent-position cap applies.
	//
	//   max_notional_usd     NOT enforced anywhere as published. It is the
	//                        futures-unaware CRYPTO cap (default $50k) that a
	//                        single MNQ contract (~$61k) exceeds; passing it
	//                        used to skip every cycle while a position was open.
	//                        Notional IS enforced at EXECUTION time, futures-
	//                        aware, as equity × max_notional_leverage — a
	//                        DIFFERENT number from this one.
	//   kill_switch_armed    a derived boolean (limit > 0), not an armed state.
	//                        The limit that actually gates is the Studio
	//                        guardrail, and only while the guardrails master is
	//                        ON; this field cannot see either.
	//   daily_loss_limit_usd the ENV value. When a Studio guardrail is set, THAT
	//                        is the enforced number and this one is not.
	//
	// The fields stay (removing them would break existing readers) and now say
	// so in their own names. The DESK strip renders the RESOLVED enforced limit
	// with its source instead.
	resp := riskStatusResponse{
		TraderID:            traderID,
		DailyLossLimitUSD:   limits.MaxDailyLossUSD,
		MaxConcurrentTrades: limits.MaxConcurrentTrades,
		MaxNotionalUSD:      limits.MaxNotionalUSD,
		KillSwitchArmed:     limits.MaxDailyLossUSD > 0,
		LastResetUTC:        time.Now().UTC().Format(time.RFC3339),
		NotEnforced: []string{
			"max_notional_usd: the crypto cap is not applied at the pre-prompt gate; notional is enforced at execution time as equity × max_notional_leverage",
			"kill_switch_armed: derived from the env limit being non-zero — it is not an armed state and does not see the Studio guardrail or its master toggle",
			"daily_loss_limit_usd: the ENV value; when a Studio guardrail is set that value is the enforced one. GET /api/desk renders the resolved limit with its source",
		},
	}

	resp.DailyLossGate = at.DailyLossGateStatusAt(time.Now())

	// Best-effort populate live values; failures don't 500 the endpoint.
	if account, err := at.GetAccountInfo(); err == nil {
		if v, ok := account["total_pnl"].(float64); ok {
			resp.DailyPnLUSD = v
		}
		if v, ok := account["position_count"].(int); ok {
			resp.ConcurrentTrades = v
		} else if v, ok := account["position_count"].(float64); ok {
			resp.ConcurrentTrades = int(v)
		}
	}

	if positions, err := at.GetUnderlyingTrader().GetPositions(); err == nil {
		var notional float64
		for _, pos := range positions {
			price, _ := pos["markPrice"].(float64)
			if price == 0 {
				price, _ = pos["entryPrice"].(float64)
			}
			qty, _ := pos["positionAmt"].(float64)
			if qty == 0 {
				qty, _ = pos["quantity"].(float64)
			}
			if qty < 0 {
				qty = -qty
			}
			notional += price * qty
		}
		resp.CurrentNotionalUSD = notional
		if resp.ConcurrentTrades == 0 {
			resp.ConcurrentTrades = len(positions)
		}
	}

	c.JSON(http.StatusOK, resp)
}
