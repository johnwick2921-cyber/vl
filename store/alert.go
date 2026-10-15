package store

import (
	"errors"
	"fmt"
	"strconv"

	"gorm.io/gorm"
)

// P4.4 — in-app alert store (no external push). P0 = toast + persistent banner
// until ack · P1 = feed · P2 = digest-only. Deduped by event_id (idempotent bus).

// EpisodeEventID (W26, 2026-10-15) builds a PER-EPISODE dedupe key for a
// recurring event: "<kind>:<traderID>:<epochMs>". A fixed event id dedupes
// forever (the idempotent bus has no expiry), so a recurring DOWN episode used
// to be silent the second time. Callers pass the episode's start instant (the
// handoff timeout, the link-down instant, …) so every episode is its own row;
// the dedupe still collapses a re-emit inside the same instant. epochMs <= 0
// falls back to the bare "<kind>:<traderID>" key (no fabricated instant).
func EpisodeEventID(kind, traderID string, epochMs int64) string {
	if epochMs <= 0 {
		return kind + ":" + traderID
	}
	return kind + ":" + traderID + ":" + strconv.FormatInt(epochMs, 10)
}

// AlertStore persists in-app alerts.
type AlertStore struct {
	db *gorm.DB
}

// AlertDB is one alert.
type AlertDB struct {
	ID       int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	TraderID string `gorm:"column:trader_id;not null;default:'';index:idx_alert_trader" json:"trader_id"`
	Level    string `gorm:"column:level;not null;default:'P2'" json:"level"`                           // P0 | P1 | P2
	EventID  string `gorm:"column:event_id;not null;default:'';index:idx_alert_event" json:"event_id"` // dedupe key
	Kind     string `gorm:"column:kind;not null;default:''" json:"kind"`
	Title    string `gorm:"column:title;not null;default:''" json:"title"`
	Body     string `gorm:"column:body;not null;default:''" json:"body"`
	Acked    bool   `gorm:"column:acked;not null;default:false" json:"acked"`
	// Dismissed hides the alert from the owner's FEED without destroying the
	// event (ITEM 5, 2026-08-17). AUDIT, NOT AMNESIA: the row stays, so history
	// and any later analysis are intact — a P0 halt that was dismissed is still
	// discoverable. Nothing in the codebase deletes an alert row.
	Dismissed   bool  `gorm:"column:dismissed;not null;default:false" json:"dismissed"`
	DismissedAt int64 `gorm:"column:dismissed_at;not null;default:0" json:"dismissed_at"`
	CreatedAt   int64 `gorm:"column:created_at" json:"created_at"`
}

// TableName implements the gorm Tabler interface.
func (AlertDB) TableName() string { return "day_plan_alerts" }

// NewAlertStore creates an AlertStore.
func NewAlertStore(db *gorm.DB) *AlertStore { return &AlertStore{db: db} }

func (s *AlertStore) initTables() error {
	if s.db.Dialector.Name() == "postgres" {
		var n int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'day_plan_alerts'`).Scan(&n)
		if n > 0 {
			return nil
		}
	}
	return s.db.AutoMigrate(&AlertDB{})
}

// Emit inserts an alert, deduped by (trader_id, event_id) when event_id is set
// (idempotent bus). Returns true when a new row was written.
func (s *AlertStore) Emit(a *AlertDB) (bool, error) {
	if a == nil || a.TraderID == "" {
		return false, fmt.Errorf("trader_id required")
	}
	if a.EventID != "" {
		var n int64
		s.db.Model(&AlertDB{}).Where("trader_id = ? AND event_id = ?", a.TraderID, a.EventID).Count(&n)
		if n > 0 {
			return false, nil // duplicate event → no-op
		}
	}
	if err := s.db.Omit("ID").Create(a).Error; err != nil {
		return false, err
	}
	return true, nil
}

// List returns a trader's alerts, newest first (up to limit).
func (s *AlertStore) List(traderID string, limit int) ([]*AlertDB, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []*AlertDB
	err := s.db.Where("trader_id = ? AND dismissed = ?", traderID, false).
		Order("id DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// UnackedCount returns the number of un-acked alerts (the bell badge).
func (s *AlertStore) UnackedCount(traderID string) (int, error) {
	var n int64
	err := s.db.Model(&AlertDB{}).
		Where("trader_id = ? AND acked = ? AND dismissed = ?", traderID, false, false).Count(&n).Error
	return int(n), err
}

// AckByEvent acknowledges every unacked alert matching (trader, event_id) —
// P5 (ledger-close 2026-08-19): a recovered condition (the first successful AI
// call after a 402 outage) clears its own banner instead of waiting for a
// human ack. Returns rows acked.
func (s *AlertStore) AckByEvent(traderID, eventID string) (int64, error) {
	res := s.db.Model(&AlertDB{}).
		Where("trader_id = ? AND event_id = ? AND acked = ?", traderID, eventID, false).
		Update("acked", true)
	return res.RowsAffected, res.Error
}

// AckAllByKind acknowledges every unacked alert of one kind for a trader.
// F5 (REL17, 2026-10-11) — ai-payment recovery is RESTART-SAFE: a fresh
// process has no in-memory outage latch, so it must clear every open banner
// of the kind rather than only the one event id the dead process remembered.
// Returns rows acked.
func (s *AlertStore) AckAllByKind(traderID, kind string) (int64, error) {
	res := s.db.Model(&AlertDB{}).
		Where("trader_id = ? AND kind = ? AND acked = ?", traderID, kind, false).
		Update("acked", true)
	return res.RowsAffected, res.Error
}

// Ack marks an alert acknowledged (unscoped; internal callers only).
func (s *AlertStore) Ack(id int64) error {
	return s.db.Model(&AlertDB{}).Where("id = ?", id).Update("acked", true).Error
}

// AckForTrader marks an alert acknowledged ONLY if it belongs to traderID
// (IDOR guard — the bell feed is per-trader, so acks must be too). Returns
// found=false when no row matched the (id, trader_id) pair, so the handler can
// 404 instead of silently acking nothing (or another trader's alert).
func (s *AlertStore) AckForTrader(traderID string, id int64) (bool, error) {
	res := s.db.Model(&AlertDB{}).
		Where("id = ? AND trader_id = ?", id, traderID).
		Update("acked", true)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// ── ITEM 5 (2026-08-17) — the feed can be cleared, the record cannot ─────────
//
// Alerts could be acknowledged but never removed, so the feed only grew. These
// SOFT-delete: the row is flagged dismissed and disappears from List/UnackedCount
// while remaining in storage for audit.

// ErrP0NotAcked is returned when an UNACKNOWLEDGED P0 is dismissed. The whole
// point of the persistent P0 banner is that a halt, a fill or a read failure
// cannot be swiped away unseen — so it must be acknowledged first. Dismissing an
// ACKNOWLEDGED P0 is fine.
var ErrP0NotAcked = errors.New("an unacknowledged P0 alert must be acknowledged before it can be dismissed")

// DismissForTrader hides ONE alert from traderID's feed. found=false when no row
// matches the (id, trader_id) pair, so the handler 404s instead of silently
// dismissing nothing — or another trader's alert (the same IDOR guard as
// AckForTrader).
func (s *AlertStore) DismissForTrader(traderID string, id int64, now int64) (found bool, err error) {
	var row AlertDB
	if e := s.db.Where("id = ? AND trader_id = ?", id, traderID).First(&row).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, e
	}
	if row.Level == "P0" && !row.Acked {
		return true, ErrP0NotAcked
	}
	res := s.db.Model(&AlertDB{}).
		Where("id = ? AND trader_id = ?", id, traderID).
		Updates(map[string]any{"dismissed": true, "dismissed_at": now})
	if res.Error != nil {
		return true, res.Error
	}
	return res.RowsAffected > 0, nil
}

// DismissAckedForTrader clears every ACKNOWLEDGED alert from the feed in one
// action, leaving unacknowledged ones — especially P0 — exactly where they are.
// Returns how many were hidden.
func (s *AlertStore) DismissAckedForTrader(traderID string, now int64) (int64, error) {
	res := s.db.Model(&AlertDB{}).
		Where("trader_id = ? AND acked = ? AND dismissed = ?", traderID, true, false).
		Updates(map[string]any{"dismissed": true, "dismissed_at": now})
	return res.RowsAffected, res.Error
}

// PruneAckedOlderThan hides ACKNOWLEDGED low-priority alerts (P2 and digests)
// older than cutoff so the feed cannot grow without bound. P0/P1 are never
// auto-pruned — those are the ones worth scrolling back to — and, as everywhere
// else here, the rows survive.
func (s *AlertStore) PruneAckedOlderThan(traderID string, cutoff, now int64) (int64, error) {
	q := s.db.Model(&AlertDB{}).
		Where("acked = ? AND dismissed = ? AND created_at < ? AND level = ?", true, false, cutoff, "P2")
	if traderID != "" {
		q = q.Where("trader_id = ?", traderID)
	}
	res := q.Updates(map[string]any{"dismissed": true, "dismissed_at": now})
	return res.RowsAffected, res.Error
}
