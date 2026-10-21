package store

import (
	"fmt"
	"math"
	"strings"
	"time"
	"vl/logger"
)

// PositionBuilder handles position creation and updates with support for:
// - Position averaging (merging multiple opens)
// - Partial closes (reducing quantity)
// - FIFO matching
// - Time-ordered processing
type PositionBuilder struct {
	positionStore *PositionStore
}

// NewPositionBuilder creates a new PositionBuilder
func NewPositionBuilder(positionStore *PositionStore) *PositionBuilder {
	return &PositionBuilder{
		positionStore: positionStore,
	}
}

// ProcessTrade processes a single trade and updates position accordingly
// tradeTimeMs is Unix milliseconds UTC
func (pb *PositionBuilder) ProcessTrade(
	traderID, exchangeID, exchangeType, symbol, side, action string,
	quantity, price, fee, realizedPnL float64,
	tradeTimeMs int64,
	orderID string,
) error {
	// The eleven CEX/DEX sync paths have no broker OCO event to report, so they
	// keep today's value exactly. Only NT8 can name a cause (D3).
	return pb.ProcessTradeWithExitReason(traderID, exchangeID, exchangeType, symbol, side, action,
		quantity, price, fee, realizedPnL, tradeTimeMs, orderID, "")
}

// ProcessTradeWithExitReason is ProcessTrade plus the BROKER's own exit cause.
//
// WAVE A / D3 — every one of the 58 eligible closed positions carries
// close_reason='sync', a word that says how the ROW was written and nothing
// about why the TRADE ended. The cause then had to be reconstructed from raw
// NT8 logs. It never needed reconstructing: NT8 sends it
// (provider/ninjatrader/tcp_framing.go:288, `exit_reason` = "sl"|"tp"|"manual"),
// Go parses it, and trader/ninjatrader/close_sync.go:204 already uses it to arm
// the re-entry cooldown — eight lines after handing this builder the literal
// "sync". The value was in the same function, in the same variable, and was
// dropped on the way to the column.
//
// exitReason is the BROKER's word or "" when there is none. It is never
// inferred from price proximity (D3a).
func (pb *PositionBuilder) ProcessTradeWithExitReason(
	traderID, exchangeID, exchangeType, symbol, side, action string,
	quantity, price, fee, realizedPnL float64,
	tradeTimeMs int64,
	orderID string,
	exitReason string,
) error {
	if strings.HasPrefix(action, "open_") {
		return pb.handleOpen(traderID, exchangeID, exchangeType, symbol, side, quantity, price, fee, tradeTimeMs, orderID)
	} else if strings.HasPrefix(action, "close_") {
		return pb.handleClose(traderID, exchangeID, exchangeType, symbol, side, quantity, price, fee, realizedPnL, tradeTimeMs, orderID, exitReason, true)
	}
	return nil
}

// ProcessTradeCloseUnresolvedPnl records a close whose realized P&L is UNKNOWN
// (no futures point value for the symbol — P3-1, release #26). It never
// recomputes a price-delta figure and never stamps pnl_corrected: the row
// closes with pnl_corrected NULL, which the corrected-column law (canon 40)
// reads as UNRESOLVED — excluded from every P&L sum and counted. The caller
// logs and counts the event. Only the NT8 close path should call this.
func (pb *PositionBuilder) ProcessTradeCloseUnresolvedPnl(
	traderID, exchangeID, exchangeType, symbol, side, action string,
	quantity, price, fee float64,
	tradeTimeMs int64,
	orderID string,
	exitReason string,
) error {
	if !strings.HasPrefix(action, "close_") {
		return pb.ProcessTradeWithExitReason(traderID, exchangeID, exchangeType, symbol, side, action,
			quantity, price, fee, 0, tradeTimeMs, orderID, exitReason)
	}
	return pb.handleClose(traderID, exchangeID, exchangeType, symbol, side, quantity, price, fee, 0, tradeTimeMs, orderID, exitReason, false)
}

// handleOpen handles opening positions (create new or average into existing)
// tradeTimeMs is Unix milliseconds UTC
func (pb *PositionBuilder) handleOpen(
	traderID, exchangeID, exchangeType, symbol, side string,
	quantity, price, fee float64,
	tradeTimeMs int64,
	orderID string,
) error {
	// Get existing OPEN position for (symbol, side)
	existing, err := pb.positionStore.GetOpenPositionBySymbol(traderID, symbol, side)
	if err != nil {
		return fmt.Errorf("failed to get open position: %w", err)
	}

	nowMs := time.Now().UTC().UnixMilli()
	if existing == nil {
		// Create new position
		position := &TraderPosition{
			TraderID:           traderID,
			ExchangeID:         exchangeID,
			ExchangeType:       exchangeType,
			ExchangePositionID: fmt.Sprintf("sync_%s_%s_%d", symbol, side, tradeTimeMs),
			Symbol:             symbol,
			Side:               side,
			Quantity:           quantity,
			EntryPrice:         price,
			EntryOrderID:       orderID,
			EntryTime:          tradeTimeMs,
			Leverage:           1,
			Status:             "OPEN",
			Source:             "sync",
			Fee:                fee,
			CreatedAt:          nowMs,
			UpdatedAt:          nowMs,
		}
		return pb.positionStore.CreateOpenPosition(position)
	}

	// Merge: Calculate weighted average entry price and update position
	logger.Infof("  📊 Averaging position: %s %s %.6f @ %.2f + %.6f @ %.2f",
		symbol, side, existing.Quantity, existing.EntryPrice, quantity, price)

	// Also update exchange_id and exchange_type if they were empty
	if existing.ExchangeID == "" || existing.ExchangeType == "" {
		if err := pb.positionStore.UpdatePositionExchangeInfo(existing.ID, exchangeID, exchangeType); err != nil {
			logger.Infof("  ⚠️  Failed to update exchange info: %v", err)
		}
	}

	return pb.positionStore.UpdatePositionQuantityAndPrice(existing.ID, quantity, price, fee)
}

// handleClose handles closing positions (partial or full)
// tradeTimeMs is Unix milliseconds UTC
//
// pnlKnown=false marks the close's P&L as UNKNOWN (no point value): realizedPnL
// is taken as 0, the price-delta recompute is skipped, and pnl_corrected is left
// NULL (the corrected-column law reads NULL as UNRESOLVED — excluded + counted).
func (pb *PositionBuilder) handleClose(
	traderID, exchangeID, exchangeType, symbol, side string,
	quantity, price, fee, realizedPnL float64,
	tradeTimeMs int64,
	orderID string,
	exitReason string,
	pnlKnown bool,
) error {
	// Get OPEN position
	position, err := pb.positionStore.GetOpenPositionBySymbol(traderID, symbol, side)
	if err != nil {
		return fmt.Errorf("failed to get open position: %w", err)
	}

	if position == nil {
		// No open position found - just skip
		// This can happen if trades are processed out of order or database was cleared
		logger.Infof("  ⚠️  No matching open position for %s %s (orderID: %s), skipping", symbol, side, orderID)
		return nil
	}

	const QUANTITY_TOLERANCE = 0.0001

	// Calculate realized PnL if not provided (some venues don't return it).
	// Skipped when the caller knows the P&L is UNRESOLVED (no point value): a
	// price-delta × quantity recompute would be a fabricated dollar figure.
	if pnlKnown && realizedPnL == 0 && position.EntryPrice > 0 {
		if side == "LONG" {
			realizedPnL = (price - position.EntryPrice) * quantity
		} else {
			realizedPnL = (position.EntryPrice - price) * quantity
		}
		// Round to 2 decimal places
		realizedPnL = math.Round(realizedPnL*100) / 100
	}

	if quantity < position.Quantity-QUANTITY_TOLERANCE {
		// Partial close: reduce quantity and update weighted average exit price
		logger.Infof("  📉 Partial close: %s %s %.6f → %.6f (closed %.6f @ %.2f, PnL: %.2f)",
			symbol, side, position.Quantity, position.Quantity-quantity, quantity, price, realizedPnL)
		return pb.positionStore.ReducePositionQuantity(position.ID, quantity, price, fee, realizedPnL, exitReason)
	} else {
		// Full close (or close with tolerance): mark as CLOSED
		closeQty := quantity
		if quantity > position.Quantity {
			logger.Infof("  ⚠️  Over-close detected: %s %s trying to close %.6f but only %.6f open, closing full position",
				symbol, side, quantity, position.Quantity)
			closeQty = position.Quantity
		}

		// Calculate final weighted average exit price
		// Include previously accumulated partial close prices + this final close
		closedBefore := position.EntryQuantity - position.Quantity
		totalClosed := closedBefore + closeQty
		var finalExitPrice float64
		if totalClosed > 0 {
			finalExitPrice = (position.ExitPrice*closedBefore + price*closeQty) / totalClosed
			// Use adaptive precision based on price magnitude (for meme coins with very small prices)
			finalExitPrice = adaptivePriceRound(finalExitPrice, position.ExitPrice, price, position.EntryPrice)
		} else {
			finalExitPrice = price
		}

		// Calculate total PnL (existing + new)
		totalPnL := position.RealizedPnL + realizedPnL

		// Calculate total fee (existing + new)
		totalFee := position.Fee + fee

		logger.Infof("  ✅ Full close: %s %s %.6f @ %.2f (avg exit: %.2f, entry: %.2f, PnL: %.2f)",
			symbol, side, closeQty, price, finalExitPrice, position.EntryPrice, totalPnL)

		// D3 — THE BROKER'S WORD, NOT A LITERAL. "sync" described how the row
		// was written and was recorded as though it described the trade.
		return pb.positionStore.ClosePositionFully(
			position.ID,
			finalExitPrice,
			orderID,
			tradeTimeMs,
			totalPnL,
			totalFee,
			ExitCauseFromBroker(exitReason),
		)
	}
}

// quantitiesMatch checks if two quantities are close enough (within tolerance)
func quantitiesMatch(a, b float64) bool {
	const QUANTITY_TOLERANCE = 0.0001
	return math.Abs(a-b) < QUANTITY_TOLERANCE
}
