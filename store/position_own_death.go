package store

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// OwnDeath is the evidence for look-twice rule 5: does OUR ledger say the
// position on (account, symbol, side) has just ended?
type OwnDeath struct {
	OpenRow    bool            // an OPEN row still exists there (nothing is dead)
	Last       *TraderPosition // the trader's newest CLOSED row there; nil = none
	ReceiptQty float64         // Σ quantity of the NT8 receipts applied to Last
	EntryQty   float64         // Last's original size
}

// ByReceipts reports a row closed by broker receipts that sum to its whole
// entry quantity - an execution fact, not a ledger guess.
func (d OwnDeath) ByReceipts() bool {
	return !d.OpenRow && d.Last != nil && d.EntryQty > 0 && d.ReceiptQty == d.EntryQty
}

// OwnDeathFor reads it. A row with a blank account counts as ours (it predates
// the account column or was written before the binding was known).
func (s *PositionStore) OwnDeathFor(traderID, account, symbol, side string) (OwnDeath, error) {
	var out OwnDeath
	side = strings.ToUpper(strings.TrimSpace(side))
	var open int64
	if err := s.db.Model(&TraderPosition{}).
		Where("symbol = ? AND UPPER(side) = ? AND status = ?", symbol, side, "OPEN").
		Where("account = ? OR (trader_id = ? AND (account = '' OR account IS NULL))", account, traderID).
		Count(&open).Error; err != nil {
		return out, err
	}
	if open > 0 {
		out.OpenRow = true
		return out, nil
	}
	var last TraderPosition
	err := s.db.Where("trader_id = ? AND symbol = ? AND UPPER(side) = ? AND status = ?", traderID, symbol, side, "CLOSED").
		Where("account = ? OR account = '' OR account IS NULL", account).
		Order("exit_time DESC, id DESC").First(&last).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.EntryQty = last.EntryQuantity
	if out.EntryQty <= 0 {
		out.EntryQty = last.Quantity // a CLOSED row's quantity is its original size
	}
	if err := s.db.Model(&NT8ExitReceipt{}).Where("position_id = ? AND applied = ?", last.ID, true).
		Select("COALESCE(SUM(quantity), 0)").Scan(&out.ReceiptQty).Error; err != nil {
		return out, err
	}
	out.Last = &last
	return out, nil
}
