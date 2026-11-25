// W117 F3 (a982cc74 + c20d0a82 store half, re-derived on today's code) —
// durable NT8 exit receipts. A valid completed position_close frame may beat
// the row update that carries the later cumulative entry (the row's residual
// is still the pre-entry value). Applying it would discard broker evidence or
// invent holdings; refusing it as an error loses the exit forever. The receipt
// is RETAINED as PENDING and retried once the row catches up — never dropped.

package store

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NT8ExitReceipt preserves the complete exit evidence, including when the entry
// row has not arrived yet. Receipt identity is supplied by the wire adapter.
type NT8ExitReceipt struct {
	ID           string `gorm:"primaryKey"`
	Account      string `gorm:"index:idx_nt8_exit_pending,priority:1"`
	Symbol       string
	Side         string
	SignalID     string
	TraderID     string
	ExchangeID   string
	ExchangeType string
	Reason       string
	Quantity     float64
	Price        float64
	PointValue   float64
	Fee          float64
	ExitMs       int64
	ReceivedMs   int64
	PositionID   int64
	Applied      bool `gorm:"index:idx_nt8_exit_pending,priority:2"`
}

func (NT8ExitReceipt) TableName() string { return "nt8_exit_receipts" }

type NT8ExitResult struct {
	Applied, Pending, Closed bool
	Position                 TraderPosition
	Quantity, RealizedPnL    float64
}

// pendingPersistBusyTimeoutMs is the busy wait for the receipt-persist small
// write: the full live busy_timeout, independent of the retry-loop test seam.
const pendingPersistBusyTimeoutMs = 5000

// ApplyNT8Exit atomically records one actual execution, reduces its exact owned
// row and records the fill. It never turns missing/excess quantity into a
// close: when the row is incomplete (residual below the exit quantity) the
// receipt is retained as PENDING and retried later.
func (s *PositionStore) ApplyNT8Exit(in NT8ExitReceipt) (out NT8ExitResult, err error) {
	finitePositive := func(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }
	if in.ID == "" || in.Account == "" || in.Symbol == "" || in.SignalID == "" || (in.Side != "LONG" && in.Side != "SHORT") ||
		!finitePositive(in.Quantity) || math.Trunc(in.Quantity) != in.Quantity || !finitePositive(in.Price) || !finitePositive(in.PointValue) || in.ExitMs <= 0 || math.IsNaN(in.Fee) || math.IsInf(in.Fee, 0) || in.Fee < 0 {
		return out, fmt.Errorf("invalid NT8 exit evidence: identity, account, side, integral quantity, price, point value and time required")
	}
	// W117 a3 — the transaction takes SQLite's write lock UP FRONT (BEGIN
	// IMMEDIATE on a dedicated connection). A deferred tx reads first and
	// BUSYs at the read→write upgrade when another connection holds the lock
	// (busy_timeout cannot help there), which lost a live close (row 618,
	// 2026-09-25 08:55 CT). On other backends this is the plain GORM tx.
	err = s.immediateOrPlainTx(func(tx *gorm.DB) error {
		receipt := in
		var existing NT8ExitReceipt
		e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", in.ID).First(&existing).Error
		if e == nil {
			if existing.Account != in.Account || existing.Symbol != in.Symbol || existing.Side != in.Side || existing.SignalID != in.SignalID || existing.Quantity != in.Quantity || existing.Price != in.Price || existing.Fee != in.Fee || existing.TraderID != in.TraderID || existing.PointValue != in.PointValue || existing.ExchangeID != in.ExchangeID || existing.ExchangeType != in.ExchangeType || existing.Reason != in.Reason {
				return fmt.Errorf("conflicting payload for NT8 exit receipt %s", in.ID)
			}
			if existing.Applied {
				return nil
			}
			receipt = existing
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		} else if e = tx.Create(&receipt).Error; e != nil {
			return e
		}
		in = receipt // retries retain the original evidence timestamp
		q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account = ? AND symbol = ? AND UPPER(side) = ? AND status = ?", in.Account, in.Symbol, in.Side, "OPEN")
		if in.TraderID != "" {
			q = q.Where("trader_id = ?", in.TraderID)
		}
		var rows []TraderPosition
		if e = q.Limit(2).Find(&rows).Error; e != nil {
			return e
		}
		if len(rows) == 0 {
			out.Pending = true
			return nil
		}
		if len(rows) != 1 {
			return fmt.Errorf("ambiguous NT8 exit ownership for account %s %s %s", in.Account, in.Symbol, in.Side)
		}
		row := rows[0]
		exactBracketEntry := (strings.EqualFold(in.Reason, "sl") || strings.EqualFold(in.Reason, "tp")) && row.EntryOrderID != "" && row.EntryOrderID == in.SignalID
		if row.EntryTime > receipt.ExitMs && !exactBracketEntry {
			out.Pending = true
			return nil
		} // never consume against a later position
		if (strings.EqualFold(in.Reason, "sl") || strings.EqualFold(in.Reason, "tp")) && row.EntryOrderID != "" && row.EntryOrderID != in.SignalID {
			return fmt.Errorf("NT8 exit entry lineage mismatch")
		}
		if !finitePositive(row.Quantity) || math.Trunc(row.Quantity) != row.Quantity || !finitePositive(row.EntryPrice) {
			return fmt.Errorf("NT8 exit quantity %.0f exceeds or cannot resolve owned residual %.8g", in.Quantity, row.Quantity)
		}
		if in.Quantity > row.Quantity {
			// A valid completed exit may beat later cumulative entry updates.
			// Retain the original receipt for retry; never invent extra holdings
			// or discard broker evidence because the current row is incomplete.
			out.Pending = true
			return nil
		}
		entryQty := row.EntryQuantity
		if entryQty <= 0 {
			entryQty = row.Quantity
		} // established legacy row size, not a fabricated wire quantity
		if !finitePositive(entryQty) || math.Trunc(entryQty) != entryQty || entryQty < row.Quantity {
			return fmt.Errorf("invalid NT8 entry/residual quantity")
		}
		closedBefore := entryQty - row.Quantity
		avgExit := (row.ExitPrice*closedBefore + in.Price*in.Quantity) / (closedBefore + in.Quantity)
		pnl := (in.Price - row.EntryPrice) * in.Quantity * in.PointValue
		if in.Side == "SHORT" {
			pnl = -pnl
		}
		residual := row.Quantity - in.Quantity // integer futures contracts; no fractional rounding
		totalPnL := row.RealizedPnL + pnl
		updates := map[string]any{"quantity": residual, "entry_quantity": entryQty, "exit_price": avgExit, "realized_pnl": totalPnL, "fee": row.Fee + in.Fee, "updated_at": in.ExitMs}
		closed := residual == 0
		if closed {
			// Existing history convention: CLOSED.Quantity is original size; OPEN.Quantity is residual.
			updates["quantity"] = entryQty
			updates["status"] = "CLOSED"
			updates["exit_order_id"] = in.SignalID
			updates["exit_time"] = in.ExitMs
			updates["close_reason"] = ExitCauseFromBroker(in.Reason)
			updates["pnl_corrected"] = totalPnL
			updates["pnl_correction_note"] = "NT8 execution receipts: sum(price delta × actual contracts × futures point value)"
		} else {
			// m232: a PARTIAL leg exit keeps the row OPEN (residual qty) but the
			// realized leg P&L must be visible, not NULL. Stamp pnl_corrected with
			// the partial so the row is RESOLVED for the booked leg and the
			// dashboard/history can show it labelled "partial" (the full P&L is
			// corrected again on the final close). pnl_corrected on an OPEN row
			// means "the realized partial so far", never the open P&L.
			updates["pnl_corrected"] = totalPnL
			updates["pnl_correction_note"] = "partial: NT8 execution receipts — realized leg P&L so far (row OPEN, runner residual)"
		}
		res := tx.Model(&TraderPosition{}).Where("id = ? AND account = ? AND status = ? AND quantity = ? AND entry_quantity = ?", row.ID, in.Account, "OPEN", row.Quantity, row.EntryQuantity).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return fmt.Errorf("NT8 position changed during exit application")
		}
		fillSide := "SELL"
		if in.Side == "SHORT" {
			fillSide = "BUY"
		}
		fill := TraderFill{TraderID: row.TraderID, ExchangeID: in.ExchangeID, ExchangeType: in.ExchangeType, ExchangeOrderID: in.SignalID, ExchangeTradeID: in.ID, Symbol: in.Symbol, Side: fillSide, Price: in.Price, Quantity: in.Quantity, QuoteQuantity: in.Price * in.Quantity, Commission: in.Fee, CommissionAsset: "USD", RealizedPnL: pnl, CreatedAt: in.ExitMs}
		if e = tx.Create(&fill).Error; e != nil {
			return e
		}
		if e = tx.Model(&NT8ExitReceipt{}).Where("id = ? AND applied = ?", in.ID, false).Updates(map[string]any{"applied": true, "position_id": row.ID}).Error; e != nil {
			return e
		}
		if e = tx.First(&row, row.ID).Error; e != nil {
			return e
		}
		out = NT8ExitResult{Applied: true, Closed: closed, Position: row, Quantity: in.Quantity, RealizedPnL: pnl}
		return nil
	})
	return out, err
}

// SavePendingExit persists the receipt row in its OWN small write — outside the
// multi-statement ApplyNT8Exit transaction — for the case where ApplyNT8Exit
// exhausted its bounded busy retries. RetryPendingNT8Exits later re-applies it.
// Idempotent: an existing receipt row (any state) is kept, never overwritten.
//
// W117 a3 — the write runs on a dedicated connection with the FULL busy wait
// (not the retry loop's budget): the park is the final net, so this single
// INSERT is allowed to block up to busy_timeout for the holder to release — a
// pooled connection would carry busy_timeout=0 (store/gorm.go's PRAGMA reaches
// only one of the four) and could lose the receipt to the SAME contention.
func (s *PositionStore) SavePendingExit(in NT8ExitReceipt) error {
	if in.ID == "" {
		return fmt.Errorf("NT8 exit receipt identity required")
	}
	return withImmediateWriteTxBusy(s.db, pendingPersistBusyTimeoutMs, func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&in).Error
	})
}

func (s *PositionStore) PendingNT8Exits(account string) ([]NT8ExitReceipt, error) {
	rows := []NT8ExitReceipt{}
	if account == "" {
		return rows, nil
	}
	err := s.db.Where("account = ? AND applied = ?", account, false).Order("exit_ms ASC, id ASC").Find(&rows).Error
	return rows, err
}

// LatestNT8ExitReceiptMs restores the account snapshot fence after restart.
func (s *PositionStore) LatestNT8ExitReceiptMs(account string) (int64, error) {
	var latest int64
	err := s.db.Model(&NT8ExitReceipt{}).Where("account = ?", account).Select("COALESCE(MAX(received_ms), 0)").Scan(&latest).Error
	return latest, err
}
