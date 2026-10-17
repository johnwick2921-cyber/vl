package store

import (
	"time"

	"gorm.io/gorm"
)

// T1 — TOUCH EPISODES (telemetry addendum, 2026-08-26). Additive, append-only:
// one row per CLOSED price-vs-level touch episode, written by the trader's
// TouchEpisodeSink. Advisory telemetry — zero gates, zero order authority.
// level_stats joins this table for the 2-week verdict.

// TouchEpisodeDB is one persisted episode.
type TouchEpisodeDB struct {
	ID             uint64    `gorm:"primaryKey;autoIncrement"`
	TraderID       string    `gorm:"column:trader_id;not null;default:''"`
	SessionDay     string    `gorm:"column:session_day;not null;default:''"` // CME session-day key
	Symbol         string    `gorm:"column:symbol;not null;default:''"`
	Label          string    `gorm:"column:label;not null;default:''"`
	LevelPrice     float64   `gorm:"column:level_price;not null;default:0"`
	Number         int       `gorm:"column:touch_number;not null;default:0"`
	OpenedAtMs     int64     `gorm:"column:opened_at_ms;not null;default:0"`
	ClosedAtMs     int64     `gorm:"column:closed_at_ms;not null;default:0"`
	BarsIn         int       `gorm:"column:bars_in;not null;default:0"`
	PenetrationPts float64   `gorm:"column:penetration_pts;not null;default:0"`
	WickPenPts     float64   `gorm:"column:wick_pen_pts;not null;default:0"`
	BodyPenPts     float64   `gorm:"column:body_pen_pts;not null;default:0"`
	Close1m        string    `gorm:"column:close_1m;not null;default:''"`
	Close5m        string    `gorm:"column:close_5m;not null;default:''"`
	VolRatio       float64   `gorm:"column:vol_ratio;not null;default:0"`
	ApproachATR    float64   `gorm:"column:approach_atr;not null;default:0"`
	Shape          string    `gorm:"column:shape;not null;default:''"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"`
}

// TableName implements the gorm Tabler interface.
func (TouchEpisodeDB) TableName() string { return "touch_episodes" }

// TouchEpisodeStore wraps the gorm handle.
type TouchEpisodeStore struct {
	db *gorm.DB
}

// NewTouchEpisodeStore builds the store on the root gorm handle.
func NewTouchEpisodeStore(db *gorm.DB) *TouchEpisodeStore { return &TouchEpisodeStore{db: db} }

// Migrate creates the table (additive, idempotent).
func (s *TouchEpisodeStore) Migrate() error { return s.db.AutoMigrate(&TouchEpisodeDB{}) }

// Insert appends one episode (append-only).
func (s *TouchEpisodeStore) Insert(ep TouchEpisodeDB) error {
	return s.db.Create(&ep).Error
}

// CountForLevel returns the persisted episode count for one level identity —
// used by level_stats for the touch-number join.
func (s *TouchEpisodeStore) CountForLevel(traderID, sessionDay, label string, price float64) (int64, error) {
	var n int64
	err := s.db.Model(&TouchEpisodeDB{}).
		Where("trader_id = ? AND session_day = ? AND label = ? AND level_price = ?", traderID, sessionDay, label, price).
		Count(&n).Error
	return n, err
}

// EpisodeCountByLevel aggregates episodes per (label, price) for a session-day
// (level_stats feed).
type EpisodeCountRow struct {
	Label  string  `gorm:"column:label"`
	Price  float64 `gorm:"column:level_price"`
	Count  int64   `gorm:"column:count"`
	Reject int64   `gorm:"column:rejections"`
	Accept int64   `gorm:"column:acceptances"`
}

func (s *TouchEpisodeStore) EpisodeCountByLevel(traderID, sessionDay string) ([]EpisodeCountRow, error) {
	var out []EpisodeCountRow
	err := s.db.Model(&TouchEpisodeDB{}).
		Select("label, level_price, COUNT(*) AS count, SUM(CASE WHEN shape='rejection' THEN 1 ELSE 0 END) AS rejections, SUM(CASE WHEN shape='acceptance' THEN 1 ELSE 0 END) AS acceptances").
		Where("trader_id = ? AND session_day = ?", traderID, sessionDay).
		Group("label, level_price").Scan(&out).Error
	return out, err
}
