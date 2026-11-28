package store

import (
	"errors"
	"math"
	"strings"

	"gorm.io/gorm"
)

// ── OWN REVERSE (look-twice, spec 2026-11-24 section 3 C) — the arithmetic ───

// OverClose answers: by how many contracts did the broker's exit receipts close
// MORE than the row they belong to ever held, and at what average price did
// those extra contracts fill?
type OverClose struct {
	Row     *TraderPosition // the row the exits belong to; nil = none (nothing can be proven)
	Entry   float64         // its original size
	Applied float64         // Σ receipts applied to it
	Pending float64         // Σ unapplied receipts around this one
	Excess  float64         // Applied + Pending − Entry
	AvgPx   float64         // quantity-weighted price of the LAST Excess contracts
	FirstID string          // the oldest unapplied receipt (one reverse = one identity)
}

// OverCloseFor reads it for one unapplied receipt. Only receipts at or after
// windowMs before it are summed: an older unapplied one belongs to a reverse
// that already ended.
func (s *PositionStore) OverCloseFor(traderID string, r NT8ExitReceipt, windowMs int64) (OverClose, error) {
	var out OverClose
	var row TraderPosition
	err := s.db.Where("trader_id = ? AND symbol = ? AND UPPER(side) = ? AND entry_time <= ?", traderID, r.Symbol, strings.ToUpper(r.Side), r.ExitMs).
		Where("account = ? OR account = '' OR account IS NULL", r.Account).
		Order("entry_time DESC, id DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Entry = row.EntryQuantity
	if out.Entry <= 0 {
		if row.Status != "CLOSED" {
			return out, nil // a residual is not an original size: nothing is proven
		}
		out.Entry = row.Quantity // a CLOSED row's quantity is its original size
	}
	if err := s.db.Model(&NT8ExitReceipt{}).Where("position_id = ? AND applied = ?", row.ID, true).
		Select("COALESCE(SUM(quantity), 0)").Scan(&out.Applied).Error; err != nil {
		return out, err
	}
	var pending []NT8ExitReceipt
	if err := s.db.Where("account = ? AND symbol = ? AND UPPER(side) = ? AND applied = ? AND exit_ms >= ? AND exit_ms >= ?",
		r.Account, r.Symbol, strings.ToUpper(r.Side), false, row.EntryTime, r.ExitMs-windowMs).
		Order("exit_ms ASC, id ASC").Find(&pending).Error; err != nil {
		return out, err
	}
	for _, p := range pending {
		out.Pending += p.Quantity
	}
	out.Row = &row
	out.Excess = out.Applied + out.Pending - out.Entry
	if len(pending) == 0 || out.Excess <= 0 || out.Excess > out.Pending {
		return out, nil // nothing over-closed by these receipts
	}
	out.FirstID = pending[0].ID
	left, sum := out.Excess, 0.0
	for i := len(pending) - 1; i >= 0 && left > 0; i-- {
		q := math.Min(left, pending[i].Quantity)
		sum += q * pending[i].Price
		left -= q
	}
	out.AvgPx = sum / out.Excess
	return out, nil
}

// ExitReceipt reads one stored exit receipt; nil = not stored.
func (s *PositionStore) ExitReceipt(id string) (*NT8ExitReceipt, error) {
	var r NT8ExitReceipt
	err := s.db.Where("id = ?", id).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// MarkOwnReverseBot corrects ONE reconcile adoption from manual to bot: our own
// close is proven to have opened it. WHERE-scoped by id, trader, OPEN, the
// manual flag and the adoption fingerprint; a second call writes nothing.
func (s *PositionStore) MarkOwnReverseBot(id int64, traderID string) (bool, error) {
	tx := s.db.Model(&TraderPosition{}).
		Where("id = ? AND trader_id = ? AND status = ?", id, traderID, "OPEN").
		Where(ManualOriginSQL).Where(manualOriginEvidenceSQL).
		Update("origin", OriginBot)
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected == 1, nil
}
