package store

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ── PARTIAL CLOSE (2026-10-03, mentor mode, behind its own knob) ────────────
//
// One table: position_reductions, the per-partial ledger — who asked, how
// many, at what fill, with what remaining. The protective-stop resize is NOT a
// ledger lifecycle: the AddOn shrinks the SL and TP IN PLACE (Account.Change)
// as part of reduce_position and reports the new bracket quantity on the fill;
// Go verifies it against the snapshot and FAILS CLOSED (flatten) on a mismatch.

// PositionReduction is ONE partial exit. FillPrice=0 and Remaining=-1 while
// the fill has not been reported (absent ≠ 0 law: an unreported fill is NOT a
// zero fill).
type PositionReduction struct {
	ID        int64   `gorm:"primaryKey;autoIncrement"`
	TraderID  string  `gorm:"index"`
	Symbol    string  `gorm:"index"`
	Side      string  `json:"side"` // "long" | "short"
	ClientID  string  `gorm:"index"`
	Quantity  int     `json:"quantity"`
	FillPrice float64 `json:"fill_price"`
	Remaining int     `json:"remaining"`
	// BracketQty is the new SL/TP quantity the AddOn set IN PLACE, reported on
	// the fill. -1 = unknown/unreported (absent ≠ 0 law).
	BracketQty int    `json:"bracket_qty"`
	Who        string `json:"who"`
	CreatedMs  int64  `json:"created_ms"`
	UpdatedMs  int64  `json:"updated_ms"`
}

type partialCloseStore struct {
	db *gorm.DB
}

// Migrate creates the partial-close ledger. sqlite gets the exact DDL (no
// guessing migration), every other dialect gets AutoMigrate.
func (s *partialCloseStore) Migrate() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store required")
	}
	if s.db.Dialector.Name() == "sqlite" {
		if err := s.db.Exec(`
			CREATE TABLE IF NOT EXISTS position_reductions (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				trader_id TEXT NOT NULL DEFAULT '',
				symbol TEXT NOT NULL DEFAULT '',
				side TEXT NOT NULL DEFAULT '',
				client_id TEXT NOT NULL DEFAULT '',
				quantity INTEGER NOT NULL DEFAULT 0,
				fill_price REAL NOT NULL DEFAULT 0,
				remaining INTEGER NOT NULL DEFAULT -1,
				bracket_qty INTEGER NOT NULL DEFAULT -1,
				who TEXT NOT NULL DEFAULT '',
				created_ms INTEGER NOT NULL DEFAULT 0,
				updated_ms INTEGER NOT NULL DEFAULT 0
			);
			CREATE INDEX IF NOT EXISTS idx_position_reductions_trader ON position_reductions(trader_id);
			CREATE INDEX IF NOT EXISTS idx_position_reductions_client ON position_reductions(client_id);
		`).Error; err != nil {
			return err
		}
		return nil
	}
	return s.db.AutoMigrate(&PositionReduction{})
}

// RecordReduce inserts the request-time row. The fill update (FillPrice,
// Remaining) upserts by client_id latest-wins so a part-fill then full-fill
// pair applies exactly once per progress step.
func (s *partialCloseStore) RecordReduce(r *PositionReduction) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store required")
	}
	if strings.TrimSpace(r.ClientID) == "" || r.Quantity <= 0 {
		return fmt.Errorf("a reduce needs a client id and a positive quantity")
	}
	nowMs := time.Now().UTC().UnixMilli()
	if r.CreatedMs <= 0 {
		r.CreatedMs = nowMs
	}
	r.UpdatedMs = nowMs
	return s.db.Create(r).Error
}

// ApplyReduceFill records the fill side of a partial: fill price, remaining
// and the AddOn's in-place bracket quantity, upserted by client_id
// latest-wins (idempotent under C# re-reports).
func (s *partialCloseStore) ApplyReduceFill(clientID string, fillPrice float64, remaining, bracketQty int) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store required")
	}
	if strings.TrimSpace(clientID) == "" {
		return fmt.Errorf("a reduce fill needs its client id")
	}
	res := s.db.Model(&PositionReduction{}).Where("client_id = ?", clientID).Updates(map[string]any{
		"fill_price":  fillPrice,
		"remaining":   remaining,
		"bracket_qty": bracketQty,
		"updated_ms":  time.Now().UTC().UnixMilli(),
	})
	if res.Error != nil {
		return res.Error
	}
	return nil
}

// ListReductions returns one trader's partials, newest first.
func (s *partialCloseStore) ListReductions(traderID string) ([]PositionReduction, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var out []PositionReduction
	err := s.db.Where("trader_id = ?", traderID).Order("id DESC").Find(&out).Error
	return out, err
}
