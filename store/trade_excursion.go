package store

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// TRADE EXCURSION LOGGING (wave 1A, 2026-09-02) — one row per position, the
// whole path from entry to exit.
//
// Why a new table rather than more columns on trader_positions: D15. There,
// `mae REAL DEFAULT 0` makes a computed zero and a value nobody ever computed
// the same bit pattern, and 521 of 586 closed rows read 0 — a column that
// cannot say "unknown" cannot be the input to a stop-size ruling. Every
// numeric here is NULLABLE and starts NULL. A stored 0 means measured zero.
//
// This table is TELEMETRY. Nothing reads it to decide anything: no gate, no
// exit, no size. It is the evidence the exit rules will later be derived from.

// TradeExcursion is the stored row (Appendix B1). Pointer fields are the ones
// that are unknown until computed; a nil is an honest "not measured".
type TradeExcursion struct {
	ID         int64  `gorm:"column:id;primaryKey;autoIncrement"`
	PositionID int64  `gorm:"column:position_id;index"`
	PlanID     string `gorm:"column:plan_id"`
	Version    int    `gorm:"column:version"`
	Session    string `gorm:"column:session"`
	Scenario   string `gorm:"column:scenario"`
	Condition  string `gorm:"column:condition"`
	Side       string `gorm:"column:side"`

	EntryPx float64  `gorm:"column:entry_px"`
	EntryTs int64    `gorm:"column:entry_ts"`
	ExitPx  *float64 `gorm:"column:exit_px"`
	ExitTs  *int64   `gorm:"column:exit_ts"`
	// ExitReason is the position's own close reason, copied verbatim.
	ExitReason string `gorm:"column:exit_reason"`

	StopPxInitial float64  `gorm:"column:stop_px_initial"`
	StopPxFinal   *float64 `gorm:"column:stop_px_final"`
	TargetPx      float64  `gorm:"column:target_px"`
	Size          float64  `gorm:"column:size"`

	MAEPts  *float64 `gorm:"column:mae_pts"`
	MAETs   *int64   `gorm:"column:mae_ts"`
	MAEBars *int     `gorm:"column:mae_bars_after_entry"`
	MFEPts  *float64 `gorm:"column:mfe_pts"`
	MFETs   *int64   `gorm:"column:mfe_ts"`
	MFEBars *int     `gorm:"column:mfe_bars_after_entry"`

	BarsHeld      *int `gorm:"column:bars_held"`
	AmbiguousBars *int `gorm:"column:ambiguous_bars"`
	// AmbiguousExit marks a close inside a bar that reached both the stop and
	// the target; the record then reads the stop (against the trade).
	AmbiguousExit bool `gorm:"column:ambiguous_exit"`

	// A22 — the CORRECTED P&L, copied at close. Never realized_pnl.
	PnlCorrected *float64 `gorm:"column:pnl_corrected"`

	ATR5mAtEntry     float64  `gorm:"column:atr5m_at_entry"`
	ATRMultStopEntry *float64 `gorm:"column:atr_mult_stop_at_entry"`

	// Source says how the row came to exist: "live" (the hooks, as it happened)
	// or "backfill" (rebuilt afterwards from the stored tape). The boot line
	// reports them separately so nobody reads a backfilled corpus as live
	// evidence.
	Source string `gorm:"column:source"`

	// Resolution names the tape the path was built from: "1m", "5m" (fallback,
	// stated) or "none" (no coverage — every excursion field stays NULL).
	Resolution string `gorm:"column:resolution"`

	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (TradeExcursion) TableName() string { return "trade_excursions" }

// TradeExcursionPath is one recomputation of the path (per bar close, or once
// at backfill). Every field is a measured number; the writer stores them as
// non-NULL precisely because they were computed.
type TradeExcursionPath struct {
	MAEPts        float64
	MAETs         int64
	MAEBars       int
	MFEPts        float64
	MFETs         int64
	MFEBars       int
	BarsHeld      int
	AmbiguousBars int
	Resolution    string
}

// TradeExcursionClose carries the exit half.
type TradeExcursionClose struct {
	ExitPx       float64
	ExitTs       int64
	ExitReason   string
	StopPxFinal  float64
	PnlCorrected *float64 // nil → the column stays NULL (A22: never realized_pnl)
	Ambiguous    bool
	// Size is the WHOLE position's contract count at close (MATH-EXECUTOR
	// P2-1): the open hook stamps the first partial fill. 0 leaves it as is.
	Size float64
}

const tradeExcursionDDL = `
CREATE TABLE IF NOT EXISTS trade_excursions (
	id                      INTEGER PRIMARY KEY AUTOINCREMENT,
	position_id             INTEGER NOT NULL DEFAULT 0,
	plan_id                 TEXT    NOT NULL DEFAULT '',
	version                 INTEGER NOT NULL DEFAULT 0,
	session                 TEXT    NOT NULL DEFAULT '',
	scenario                TEXT    NOT NULL DEFAULT '',
	condition               TEXT    NOT NULL DEFAULT '',
	side                    TEXT    NOT NULL DEFAULT '',
	entry_px                REAL    NOT NULL DEFAULT 0,
	entry_ts                INTEGER NOT NULL DEFAULT 0,
	exit_px                 REAL,
	exit_ts                 INTEGER,
	exit_reason             TEXT    NOT NULL DEFAULT '',
	stop_px_initial         REAL    NOT NULL DEFAULT 0,
	stop_px_final           REAL,
	target_px               REAL    NOT NULL DEFAULT 0,
	size                    REAL    NOT NULL DEFAULT 0,
	mae_pts                 REAL,
	mae_ts                  INTEGER,
	mae_bars_after_entry    INTEGER,
	mfe_pts                 REAL,
	mfe_ts                  INTEGER,
	mfe_bars_after_entry    INTEGER,
	bars_held               INTEGER,
	ambiguous_bars          INTEGER,
	ambiguous_exit          INTEGER NOT NULL DEFAULT 0,
	pnl_corrected           REAL,
	atr5m_at_entry          REAL    NOT NULL DEFAULT 0,
	atr_mult_stop_at_entry  REAL,
	resolution              TEXT    NOT NULL DEFAULT '',
	source                  TEXT    NOT NULL DEFAULT 'live',
	created_at              DATETIME,
	updated_at              DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_trade_excursions_position ON trade_excursions(position_id);
CREATE INDEX IF NOT EXISTS idx_trade_excursions_session ON trade_excursions(session, condition);
`

// TradeExcursionStore writes and reads the excursion table.
type TradeExcursionStore struct{ db *gorm.DB }

func NewTradeExcursionStore(db *gorm.DB) *TradeExcursionStore {
	return &TradeExcursionStore{db: db}
}

// Migrate creates the table and its indexes. Idempotent.
func (s *TradeExcursionStore) Migrate() error {
	return s.db.Exec(tradeExcursionDDL).Error
}

// Open inserts the entry half. Every excursion field is left NULL: nothing has
// been measured yet, and this table never says zero when it means unknown.
// Idempotent per position — a second Open returns the existing row's id.
func (s *TradeExcursionStore) Open(row TradeExcursion) (int64, error) {
	if existing, err := s.GetByPosition(row.PositionID); err == nil && existing != nil {
		return existing.ID, nil
	}
	now := time.Now()
	row.CreatedAt, row.UpdatedAt = now, now
	if err := s.db.Create(&row).Error; err != nil {
		return 0, err
	}
	return row.ID, nil
}

// UpdatePath stores a recomputed path. Called on every bar close while the
// position is open, and once by the backfill.
func (s *TradeExcursionStore) UpdatePath(id int64, p TradeExcursionPath) error {
	return s.db.Model(&TradeExcursion{}).Where("id = ?", id).Updates(map[string]any{
		"mae_pts": p.MAEPts, "mae_ts": p.MAETs, "mae_bars_after_entry": p.MAEBars,
		"mfe_pts": p.MFEPts, "mfe_ts": p.MFETs, "mfe_bars_after_entry": p.MFEBars,
		"bars_held": p.BarsHeld, "ambiguous_bars": p.AmbiguousBars,
		"resolution": p.Resolution, "updated_at": time.Now(),
	}).Error
}

// MarkNoCoverage records that the path could NOT be built. The excursion
// columns stay NULL and the row says so out loud (E5: never a guessed number).
func (s *TradeExcursionStore) MarkNoCoverage(id int64) error {
	return s.db.Model(&TradeExcursion{}).Where("id = ?", id).
		Updates(map[string]any{"resolution": "none", "updated_at": time.Now()}).Error
}

// Close stores the exit half.
func (s *TradeExcursionStore) Close(id int64, c TradeExcursionClose) error {
	up := map[string]any{
		"exit_px": c.ExitPx, "exit_ts": c.ExitTs, "exit_reason": c.ExitReason,
		"stop_px_final": c.StopPxFinal, "ambiguous_exit": c.Ambiguous,
		"updated_at": time.Now(),
	}
	if c.PnlCorrected != nil { // A22 — absent stays NULL, never realized_pnl
		up["pnl_corrected"] = *c.PnlCorrected
	}
	if c.Size > 0 {
		up["size"] = c.Size
	}
	return s.db.Model(&TradeExcursion{}).Where("id = ?", id).Updates(up).Error
}

// SetLevels fills in the stop/target a row was opened without, once they are
// resolved from the opening decision. It never overwrites a level already set.
func (s *TradeExcursionStore) SetLevels(id int64, stopPx, targetPx float64) error {
	return s.db.Model(&TradeExcursion{}).
		Where("id = ? AND stop_px_initial = 0", id).
		Updates(map[string]any{
			"stop_px_initial": stopPx, "target_px": targetPx, "updated_at": time.Now(),
		}).Error
}

// Get reads one row by its own id.
func (s *TradeExcursionStore) Get(id int64) (*TradeExcursion, error) {
	var row TradeExcursion
	if err := s.db.Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// GetByPosition reads the row for a position, or (nil, nil) when absent.
func (s *TradeExcursionStore) GetByPosition(positionID int64) (*TradeExcursion, error) {
	var row TradeExcursion
	err := s.db.Where("position_id = ?", positionID).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// Counts reports the boot-line numbers: total rows, rows with a measured path,
// and rows whose path could not be built. READ, never assumed.
func (s *TradeExcursionStore) Counts() (total, backfilled, unresolved int64, err error) {
	if err = s.db.Model(&TradeExcursion{}).Count(&total).Error; err != nil {
		return
	}
	if err = s.db.Model(&TradeExcursion{}).Where("source = ?", "backfill").Count(&backfilled).Error; err != nil {
		return
	}
	err = s.db.Model(&TradeExcursion{}).Where("resolution = ?", "none").Count(&unresolved).Error
	return
}

// ExcursionBootLine is the boot block's excursion row. Every number is READ
// from the table; nothing here is a literal (A24).
func (s *TradeExcursionStore) ExcursionBootLine() string {
	total, backfilled, unresolved, err := s.Counts()
	if err != nil {
		return fmt.Sprintf("excursions: rows=? backfilled=? unresolved=? (count failed: %v)", err)
	}
	// WAVE A — "logging=on" was a LITERAL here and has been removed. It was not
	// resolved from excursionsEnabled(), dayPlanEnabled() or any trader, so it
	// read "on" even where no hook could ever fire. The counts below ARE read.
	return fmt.Sprintf("excursions: rows=%d backfilled=%d unresolved=%d (unresolved = no 1m coverage; those rows keep NULLs, never zeros)",
		total, backfilled, unresolved)
}

// ClosedPositionsBetween lists the closed positions whose ENTRY falls inside
// [fromMs, toMs], oldest first — the backfill's work list. It lives here so the
// backfill (which needs kernel, and so cannot live in this package) does not
// reach for the gorm handle directly.
func (s *TradeExcursionStore) ClosedPositionsBetween(symbol, traderID string, fromMs, toMs int64) ([]TraderPosition, error) {
	q := s.db.Model(&TraderPosition{}).
		Where("status = ? AND entry_time >= ? AND entry_time <= ?", "CLOSED", fromMs, toMs)
	if traderID != "" {
		q = q.Where("trader_id = ?", traderID)
	}
	if symbol != "" {
		q = q.Where("symbol = ?", symbol)
	}
	var out []TraderPosition
	err := q.Order("entry_time asc").Find(&out).Error
	return out, err
}
