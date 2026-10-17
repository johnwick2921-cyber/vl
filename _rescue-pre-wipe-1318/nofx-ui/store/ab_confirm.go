package store

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// AB-CONFIRM SHADOW (E8, entry-mechanics 2026-08-30) — the Sep-9 courtroom
// table. Per armed/authored setup, the machine logs 4 COUNTERFACTUAL fills
// (touch · 1x5m_close · 2x5m_close · 1m_mss): what each entry rule would have
// filled, its MFE/MAE, its target/stop outcome, and the time-to-fill.
// ZERO effect on real paths — this table is written by nothing except the
// shadow logger and read by nothing except replays/reports.

type AbConfirmLogDB struct {
	ID int64 `gorm:"primaryKey;autoIncrement"`

	TraderID string `gorm:"index"`
	PlanID   string `gorm:"index"`
	Version  int
	Session  string
	Scenario string
	Rule     string // touch | 1x5m_close | 2x5m_close | 1m_mss

	FillPx       float64 // counterfactual fill price
	MFE          float64 // max favorable excursion (pts, favorable sign)
	MAE          float64 // max adverse excursion (pts)
	Outcome      string  // target | stop | open (neither by snapshot end)
	TimeToFillMs int64   // entry bar open − plan birth

	// ── 0C shadow demotion (2026-08-31) — the complete would-have-been trade.
	Condition         string  // the scenario condition (fvg_entry etc.)
	EntryPx           float64 // authored entry ref
	StopPx            float64 // authored stop
	TargetPx          float64 // authored target
	RR                float64 // authored reward:risk from the fill
	Atr5m             float64 // ATR(5m) at entry
	MfeR              float64 // MFE in R-multiples
	MaeR              float64 // MAE in R-multiples
	MfeAtr            float64 // MFE in ATR units
	MaeAtr            float64 // MAE in ATR units
	TimeToMFEBars     int     // bars from fill to MFE peak
	TimeToMAEBars     int     // bars from fill to MAE trough
	TimeToResolveBars int     // bars from fill to stop/target (0 = open)
	// 0A-2 lesson: GORM snake-cases NetPnL to "net_pn_l" — explicit column tag
	// or the upsert fails against the DDL column net_pnl.
	NetPnL           float64 `gorm:"column:net_pnl"` // net-of-friction P&L in USD
	Ambiguous        bool    // a replay bar contained BOTH stop and target
	IsCounterfactual bool    // shadowed condition: the trade was NEVER placed

	// CLASS 39 (owner ruling 2026-09-01) — the scenario's arm was NORMALIZED at
	// plan write (legs dropped from a non-sweep condition). DroppedLegs is the
	// JSON of what the model authored and the machine removed, so the effect
	// of normalizing instead of rejecting is measurable later.
	Normalized  bool   `gorm:"column:normalized"`
	DroppedLegs string `gorm:"column:dropped_legs"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (AbConfirmLogDB) TableName() string { return "ab_confirm_log" }

const abConfirmDDL = `
CREATE TABLE IF NOT EXISTS ab_confirm_log (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	trader_id       TEXT    NOT NULL DEFAULT '',
	plan_id         TEXT    NOT NULL DEFAULT '',
	version         INTEGER NOT NULL DEFAULT 0,
	session         TEXT    NOT NULL DEFAULT '',
	scenario        TEXT    NOT NULL DEFAULT '',
	rule            TEXT    NOT NULL DEFAULT '',
	fill_px         REAL    NOT NULL DEFAULT 0,
	mfe             REAL    NOT NULL DEFAULT 0,
	mae             REAL    NOT NULL DEFAULT 0,
	outcome         TEXT    NOT NULL DEFAULT 'open',
	time_to_fill_ms INTEGER NOT NULL DEFAULT 0,
	condition         TEXT    NOT NULL DEFAULT '',
	entry_px          REAL    NOT NULL DEFAULT 0,
	stop_px           REAL    NOT NULL DEFAULT 0,
	target_px         REAL    NOT NULL DEFAULT 0,
	rr                REAL    NOT NULL DEFAULT 0,
	atr5m             REAL    NOT NULL DEFAULT 0,
	mfe_r             REAL    NOT NULL DEFAULT 0,
	mae_r             REAL    NOT NULL DEFAULT 0,
	mfe_atr           REAL    NOT NULL DEFAULT 0,
	mae_atr           REAL    NOT NULL DEFAULT 0,
	time_to_mfe_bars    INTEGER NOT NULL DEFAULT 0,
	time_to_mae_bars    INTEGER NOT NULL DEFAULT 0,
	time_to_resolve_bars INTEGER NOT NULL DEFAULT 0,
	net_pnl           REAL    NOT NULL DEFAULT 0,
	ambiguous         INTEGER NOT NULL DEFAULT 0,
	is_counterfactual INTEGER NOT NULL DEFAULT 0,
	normalized        INTEGER NOT NULL DEFAULT 0,
	dropped_legs      TEXT    NOT NULL DEFAULT '',
	created_at      DATETIME,
	updated_at      DATETIME
)`

// abConfirmAddedCols are the 0C columns ADDED to a pre-existing live table —
// CREATE TABLE IF NOT EXISTS never alters an existing table, so the live DB
// must be patched column-by-column (class-29: never a silent empty column).
var abConfirmAddedCols = []struct{ name, ddl string }{
	{"condition", "TEXT NOT NULL DEFAULT ''"},
	{"entry_px", "REAL NOT NULL DEFAULT 0"},
	{"stop_px", "REAL NOT NULL DEFAULT 0"},
	{"target_px", "REAL NOT NULL DEFAULT 0"},
	{"rr", "REAL NOT NULL DEFAULT 0"},
	{"atr5m", "REAL NOT NULL DEFAULT 0"},
	{"mfe_r", "REAL NOT NULL DEFAULT 0"},
	{"mae_r", "REAL NOT NULL DEFAULT 0"},
	{"mfe_atr", "REAL NOT NULL DEFAULT 0"},
	{"mae_atr", "REAL NOT NULL DEFAULT 0"},
	{"time_to_mfe_bars", "INTEGER NOT NULL DEFAULT 0"},
	{"time_to_mae_bars", "INTEGER NOT NULL DEFAULT 0"},
	{"time_to_resolve_bars", "INTEGER NOT NULL DEFAULT 0"},
	{"net_pnl", "REAL NOT NULL DEFAULT 0"},
	{"ambiguous", "INTEGER NOT NULL DEFAULT 0"},
	{"is_counterfactual", "INTEGER NOT NULL DEFAULT 0"},
	{"normalized", "INTEGER NOT NULL DEFAULT 0"}, // class 39
	{"dropped_legs", "TEXT NOT NULL DEFAULT ''"}, // class 39
}

// AbConfirmStore persists the shadow A/B table (E8).
type AbConfirmStore struct {
	db *gorm.DB
}

func NewAbConfirmStore(db *gorm.DB) *AbConfirmStore { return &AbConfirmStore{db: db} }

// Migrate creates the table + the per-(plan,scenario,rule) unique index so the
// logger is idempotent by construction. For a pre-existing sqlite table, the
// 0C columns are added individually (duplicate-column = already there).
func (s *AbConfirmStore) Migrate() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store required")
	}
	if s.db.Dialector.Name() == "sqlite" {
		if err := s.db.Exec(abConfirmDDL).Error; err != nil {
			return err
		}
		for _, c := range abConfirmAddedCols {
			if err := s.db.Exec("ALTER TABLE ab_confirm_log ADD COLUMN " + c.name + " " + c.ddl).Error; err != nil {
				// "duplicate column name" = the column already exists — fine.
				if !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
					return err
				}
			}
		}
		return s.db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_ab_confirm_key ON ab_confirm_log(plan_id, version, scenario, rule)").Error
	}
	return s.db.AutoMigrate(&AbConfirmLogDB{})
}

// Upsert writes one counterfactual row idempotently (same key = same row).
func (s *AbConfirmStore) Upsert(row *AbConfirmLogDB) error {
	if row == nil || row.PlanID == "" || row.Scenario == "" || row.Rule == "" {
		return fmt.Errorf("plan_id, scenario and rule required")
	}
	var existing AbConfirmLogDB
	err := s.db.Where("plan_id = ? AND version = ? AND scenario = ? AND rule = ?",
		row.PlanID, row.Version, row.Scenario, row.Rule).First(&existing).Error
	if err == nil {
		row.ID = existing.ID
		return s.db.Model(&existing).Updates(map[string]any{
			"trader_id": row.TraderID, "session": row.Session,
			"fill_px": row.FillPx, "mfe": row.MFE, "mae": row.MAE,
			"outcome": row.Outcome, "time_to_fill_ms": row.TimeToFillMs,
			"condition": row.Condition, "entry_px": row.EntryPx,
			"stop_px": row.StopPx, "target_px": row.TargetPx, "rr": row.RR,
			"atr5m": row.Atr5m, "mfe_r": row.MfeR, "mae_r": row.MaeR,
			"mfe_atr": row.MfeAtr, "mae_atr": row.MaeAtr,
			"time_to_mfe_bars": row.TimeToMFEBars, "time_to_mae_bars": row.TimeToMAEBars,
			"time_to_resolve_bars": row.TimeToResolveBars, "net_pnl": row.NetPnL,
			"ambiguous": row.Ambiguous, "is_counterfactual": row.IsCounterfactual,
			"normalized": row.Normalized, "dropped_legs": row.DroppedLegs,
			"updated_at": row.UpdatedAt,
		}).Error
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	return s.db.Create(row).Error
}

// Has reports whether the (plan,version,scenario,rule) shadow row exists —
// the executor's once-per-plan dedup check.
func (s *AbConfirmStore) Has(planID string, version int, scenario, rule string) bool {
	var n int64
	if err := s.db.Model(&AbConfirmLogDB{}).Where(
		"plan_id = ? AND version = ? AND scenario = ? AND rule = ?",
		planID, version, scenario, rule).Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}
