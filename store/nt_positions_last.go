// m433 (release #52) — the AddOn's last positions frame per account, persisted.
//
// WHY. The installation gate's trader_cutover leg 2 (api_positions) FAILS when
// the bot has no positions snapshot for the bound account ("NT8 account
// positions unknown", F4 — unknown is never flat). A bot that restarted while
// NT8's broker connection was down (a closed-market Sunday) has none, and no
// frame will arrive until the broker returns, so an update was blocked for as
// long as the broker stayed down. This table remembers the LAST positions frame
// the AddOn sent for each account, so the gate can quote "the last broker
// answer before the link went quiet" instead of a blank.
//
// It is evidence for ONE narrow gate rule (trader/installation_gate_m433.go)
// and nothing else: no order path, no sizing, no display reads it.
//
// One row per account (upper-cased, trimmed — one canonicalizer, here where the
// value enters). Every frame UPSERTs; nothing is ever deleted (THE ONE RULE).
package store

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// IsRecordNotFound reports whether err is the "no such row" answer of a gorm
// read, so a caller outside this package can tell "never stored" from a failure
// without importing gorm.
func IsRecordNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }

// NTPositionsLast is one account's last positions frame, reduced to the one
// fact the gate needs: how many NON-FLAT positions the frame carried.
type NTPositionsLast struct {
	Account      string `gorm:"column:account;primaryKey" json:"account"`
	OpenCount    int    `gorm:"column:open_count" json:"open_count"`
	ReceivedAtMs int64  `gorm:"column:received_at_ms" json:"received_at_ms"`
}

func (NTPositionsLast) TableName() string { return "nt_positions_last" }

// NTPositionsLastStore wraps the gorm handle.
type NTPositionsLastStore struct{ db *gorm.DB }

func NewNTPositionsLastStore(db *gorm.DB) *NTPositionsLastStore {
	return &NTPositionsLastStore{db: db}
}

func (s *NTPositionsLastStore) Migrate() error {
	return s.db.AutoMigrate(&NTPositionsLast{})
}

func ntPositionsKey(account string) string {
	return strings.ToUpper(strings.TrimSpace(account))
}

// Upsert records the latest frame for an account. An empty account is
// unaddressable and is refused (never filed under "").
func (s *NTPositionsLastStore) Upsert(account string, openCount int, receivedAtMs int64) error {
	key := ntPositionsKey(account)
	if key == "" {
		return nil
	}
	return s.db.Exec(
		`INSERT INTO nt_positions_last (account, open_count, received_at_ms)
		 VALUES (?, ?, ?)
		 ON CONFLICT(account) DO UPDATE SET
		   open_count = excluded.open_count,
		   received_at_ms = excluded.received_at_ms`,
		key, openCount, receivedAtMs,
	).Error
}

// Get returns the account's last frame, or nil when none was ever stored.
func (s *NTPositionsLastStore) Get(account string) (*NTPositionsLast, error) {
	var r NTPositionsLast
	err := s.db.Where("account = ?", ntPositionsKey(account)).First(&r).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
