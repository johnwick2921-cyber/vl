package store

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"vl/internal/installpath"
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

	// Direction is the scenario's own direction, taken from the PLAN and
	// stored (D6, 2026-09-03) so no later reader has to infer it. Inference
	// from geometry is wrong: a short whose fill drifted past its target does
	// not look like one, and classifying that way gave 55 shorts where the plan
	// says 121.
	Direction string `gorm:"column:direction;not null;default:''"`
	// Recompute records what the backfill could do with this row: "" (never
	// visited), "recomputed", or "unrecomputable" (inputs or direction absent).
	// A row it cannot compute keeps its numbers and says so — never a guess.
	Recompute string `gorm:"column:recompute;not null;default:''"`

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
	direction       TEXT    NOT NULL DEFAULT '',
	recompute       TEXT    NOT NULL DEFAULT '',
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
	{"direction", "TEXT NOT NULL DEFAULT ''"},
	{"recompute", "TEXT NOT NULL DEFAULT ''"},
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

// ── E8 BACKFILL (data-integrity wave D6, 2026-09-03) ────────────────────────
//
// Every short counterfactual in this table was computed by subtracting a
// mirrored price space from a real one (see kernel/shadow_ab.go). Measured on
// the live store with direction read from the PLAN, never inferred:
//
//	long   67 rows ·   0 with RR < −0.9 ·  0 with |MAE| > 1000
//	short 121 rows · 109 with RR < −0.9 · 46 with |MAE| > 1000 · 109 recomputable
//
// net_pnl is broken the same way: 40 rows below −1000 because the exit was the
// MIRRORED target, so (−29418.62 − 29413.00) × 2 = −117 664 — that is
// −(target+fill)×pv, not "exit treated as zero". Four more are 0 beside a
// resolved outcome. The other 88 zeros sit on OPEN rows where nothing resolved,
// and those are correct and untouched.

// AbBackfillResult is what one run measured. Counts, never rates (A24).
type AbBackfillResult struct {
	Scanned    int
	Recomputed int
	// THREE distinct ways a row resists repair, kept apart because they mean
	// different things and one of them is much worse than the others.
	NoInputs    int // fill/stop/target absent — nothing to compute from
	NoDirection int // the plan cannot say long or short
	BadFillBar  int // inputs present but inconsistent: a short whose stop sits
	// below its fill, or whose target sits above it. The mirror broke the
	// close-rule comparison as well as the arithmetic, so these rows' fill_px
	// came from the WRONG BAR — clean arithmetic on it would be a precise
	// answer about the wrong moment.
	LongsUntouched int
}

// Unrecomputable is the three failure states together, for a single headline.
func (r AbBackfillResult) Unrecomputable() int { return r.NoInputs + r.NoDirection + r.BadFillBar }

const (
	abRecomputed  = "recomputed"
	abNoInputs    = "unrecomputable:no-inputs"
	abNoDirection = "unrecomputable:no-direction"
	abBadFillBar  = "unrecomputable:fill-bar"
)

// ListForPlan returns every row for a plan id.
func (s *AbConfirmStore) ListForPlan(planID string) ([]AbConfirmLogDB, error) {
	var out []AbConfirmLogDB
	err := s.db.Where("plan_id = ?", planID).Order("id").Find(&out).Error
	return out, err
}

// BackfillShortRows re-derives the short rows from their stored inputs.
//
// dirFor resolves a row's direction from the PLAN. A row whose plan cannot
// answer is marked unrecomputable — direction is NEVER inferred from geometry.
//
// IMPORTANT — what this can and cannot repair. The mirror also broke the FILL
// BAR: the short close-rule compared a real close against a negated ref
// (`b.Close > -ref`), true for every bar, so these rows' fill_px came from the
// wrong bar. Re-deriving arithmetic from a wrong fill would produce a clean
// number about the wrong moment. So a recomputed row carries recompute=
// "recomputed" for its ARITHMETIC only, and the fill bar is corrected for new
// rows by the kernel fix rather than invented here. That limit is stated on
// the row instead of being papered over.
func (s *AbConfirmStore) BackfillShortRows(dirFor func(planID string, version int, scenario string) (string, bool)) (AbBackfillResult, error) {
	var res AbBackfillResult
	var rows []AbConfirmLogDB
	if err := s.db.Order("id").Find(&rows).Error; err != nil {
		return res, err
	}
	for i := range rows {
		r := rows[i]
		res.Scanned++
		dir, ok := "", false
		if dirFor != nil {
			dir, ok = dirFor(r.PlanID, r.Version, r.Scenario)
		}
		if !ok || dir == "" {
			if r.Recompute != abNoDirection {
				if err := s.db.Model(&AbConfirmLogDB{}).Where("id = ?", r.ID).
					Update("recompute", abNoDirection).Error; err != nil {
					return res, err
				}
			}
			res.NoDirection++
			continue
		}
		if dir != "short" {
			// Long rows are clean — all 67 of them — and must not move.
			if r.Direction == "" {
				if err := s.db.Model(&AbConfirmLogDB{}).Where("id = ?", r.ID).
					Update("direction", dir).Error; err != nil {
					return res, err
				}
			}
			res.LongsUntouched++
			continue
		}
		if r.FillPx <= 0 || r.StopPx <= 0 || r.TargetPx <= 0 {
			if r.Recompute != abNoInputs {
				if err := s.db.Model(&AbConfirmLogDB{}).Where("id = ?", r.ID).
					Updates(map[string]any{"direction": dir, "recompute": abNoInputs}).Error; err != nil {
					return res, err
				}
			}
			res.NoInputs++
			continue
		}
		if r.Recompute == abRecomputed {
			continue // idempotent
		}
		risk, reward := r.StopPx-r.FillPx, r.FillPx-r.TargetPx
		// A short's stop sits ABOVE its fill and its target BELOW. When the
		// stored geometry says otherwise, the fill came from the wrong bar —
		// the mirror broke the close-rule comparison too — and no arithmetic on
		// it can be trusted. Those rows are UNRECOMPUTABLE, not "recomputed
		// with an odd number": marking them recomputed would dress 23 negative
		// RRs and 14 impossible MAEs as repaired.
		if risk <= 0 || reward <= 0 {
			if err := s.db.Model(&AbConfirmLogDB{}).Where("id = ?", r.ID).
				Updates(map[string]any{"direction": dir, "recompute": abBadFillBar}).Error; err != nil {
				return res, err
			}
			res.BadFillBar++
			continue
		}
		up := map[string]any{"direction": dir, "recompute": abRecomputed}
		up["rr"] = reward / risk
		// MAE/MFE are distances and cannot exceed the bracket they were
		// measured inside; the stored 58 409 is two spaces subtracted.
		if r.MAE < 0 || r.MAE > risk {
			up["mae"] = math.Min(math.Abs(r.MAE), risk)
		}
		if r.MFE < 0 || r.MFE > reward {
			up["mfe"] = math.Min(math.Abs(r.MFE), reward)
		}
		// net_pnl from the RESOLVED exit, in the short's own direction. An OPEN
		// row's exit was the last close, which this table does not store, so it
		// is not derivable here — the stale mixed-space number is CLEARED
		// rather than left standing (recompute="recomputed" + outcome="open"
		// + 0 reads as "not derivable", which the column disambiguates).
		switch r.Outcome {
		case "target":
			up["net_pnl"] = (r.FillPx - r.TargetPx) * abPointValue
		case "stop":
			up["net_pnl"] = (r.FillPx - r.StopPx) * abPointValue
		default:
			up["net_pnl"] = 0.0
		}
		if err := s.db.Model(&AbConfirmLogDB{}).Where("id = ?", r.ID).Updates(up).Error; err != nil {
			return res, err
		}
		res.Recomputed++
	}
	return res, nil
}

// abPointValue is MNQ's $/pt. SIM-only, MNQ-only per the dispatch.
const abPointValue = 2.0

// ── D6 FLAG GUARD + BOOT LINE (2026-09-03) ─────────────────────────────────

// E8BackfillEnabled reports whether the recompute is armed. Default OFF: it
// rewrites stored counterfactuals, so it runs only when the operator says so.
// A24 — the flag lives in .env in the working directory; the systemd unit has
// no Environment=, so an `export` never reaches the process.
func E8BackfillEnabled() bool {
	v := strings.TrimSpace(os.Getenv("E8_BACKFILL"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// E8Counts reads the current state of the table for the boot line. Every
// number READ, never a literal.
func (s *AbConfirmStore) E8Counts() (total, recomputed, fillBar, noInputs, noDirection int64, err error) {
	count := func(where string, args ...any) (int64, error) {
		var n int64
		q := s.db.Model(&AbConfirmLogDB{})
		if where != "" {
			q = q.Where(where, args...)
		}
		return n, q.Count(&n).Error
	}
	if total, err = count(""); err != nil {
		return
	}
	if recomputed, err = count("recompute = ?", abRecomputed); err != nil {
		return
	}
	if fillBar, err = count("recompute = ?", abBadFillBar); err != nil {
		return
	}
	if noInputs, err = count("recompute = ?", abNoInputs); err != nil {
		return
	}
	noDirection, err = count("recompute = ?", abNoDirection)
	return
}

// E8BootLine states what the side-table can and cannot be used for.
//
// "usable" is the count a ruling may rest on: rows whose arithmetic was
// re-derived from inputs that are internally consistent. It deliberately does
// NOT include the fill-bar rows — a precise number about the wrong moment is
// not evidence, and folding them in is how "30% usable" became a table people
// quoted.
func (s *AbConfirmStore) E8BootLine() string {
	total, recomputed, fillBar, noInputs, noDirection, err := s.E8Counts()
	if err != nil {
		return "e8: counts unavailable"
	}
	if total == 0 {
		return "e8: no rows yet"
	}
	return fmt.Sprintf("e8: rows=%d usable=%d · unrecomputable fill-bar=%d no-inputs=%d no-direction=%d · backfill=%s",
		total, recomputed, fillBar, noInputs, noDirection, onOff(E8BackfillEnabled()))
}

func onOff(b bool) string {
	if b {
		return "armed"
	}
	return "off"
}

// BackupBeforeE8Backfill takes an online sqlite3 backup before the recompute
// writes. No backup, no write.
func BackupBeforeE8Backfill(dbPath, stamp string) (string, error) {
	root := installpath.BackupRoot()
	if root == "" {
		return "", fmt.Errorf("resolve home for the backup dir")
	}
	dir := filepath.Join(root, "e8-backfill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	dst := filepath.Join(dir, stamp+".db")
	if _, err := os.Stat(dst); err == nil {
		return dst, nil // already taken for this stamp — idempotent
	}
	out, err := exec.Command("sqlite3", dbPath, fmt.Sprintf(".backup '%s'", dst)).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("sqlite3 backup: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return dst, nil
}
