package store

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// P0.5 — LEVEL-STATE table (identity-keyed, cross-session persistence).
//
// A level's STATE (how many times it was tested, whether it's been consumed, its
// freshness A→B→C) must outlive the session it formed in — "a level burned in
// one session cannot return fresh in the next". Levels are RE-DERIVED each read,
// but their state persists here, keyed by level IDENTITY, not by which session
// is reading.
//
// Identity = (symbol, level_type, origin_date, bin_index). origin_date is the
// CME session-day the level FORMED (not the current day) so a naked POC keeps a
// stable key across days while its state accumulates. bin_index is the absolute
// price grid (floor(price / row-height)) so the same price lands in the same row
// regardless of session (mirrors kernel/svp.go svpBinIndex).
//
// Kernel is DB-blind (imports store for types only); this store is written FROM
// the trader layer at the session roll, exactly like decisions/equity. This file
// is the FOUNDATION (table + state API); the 17:00-CT snapshot WRITER that feeds
// it is a P1 item (RECON #2).

// Freshness grades. A level decays A→B→C with each play; below C it is done for
// the day (and marked consumed).
const (
	FreshnessA    = "A"
	FreshnessB    = "B"
	FreshnessC    = "C"
	FreshnessDone = "done"
)

// Common level types (arbitrary strings are allowed; these are the canonical set).
const (
	LevelTypePOC = "POC"
	LevelTypeVAH = "VAH"
	LevelTypeVAL = "VAL"
	LevelTypePDH = "PDH"
	LevelTypePDL = "PDL"
)

// LevelStateStore persists cross-session level state.
type LevelStateStore struct {
	db *gorm.DB
}

// LevelStateDB is one level's durable identity + state.
type LevelStateDB struct {
	LevelKey    string    `gorm:"column:level_key;primaryKey"` // traderID|symbol|type|origin_date|bin_index (P0-cleanup: trader-scoped)
	TraderID    string    `gorm:"column:trader_id;not null;default:'';index:idx_levelstate_trader"`
	Symbol      string    `gorm:"column:symbol;not null;default:'';index:idx_levelstate_symbol"`
	LevelType   string    `gorm:"column:level_type;not null;default:''"`
	OriginDate  string    `gorm:"column:origin_date;not null;default:''"` // YYYY-MM-DD CME session-day of formation
	BinIndex    int       `gorm:"column:bin_index;not null;default:0"`
	Price       float64   `gorm:"column:price;not null;default:0"`
	TimesTested int       `gorm:"column:times_tested;not null;default:0"`
	Consumed    bool      `gorm:"column:consumed;not null;default:false"`
	Freshness   string    `gorm:"column:freshness;not null;default:'A';index:idx_levelstate_fresh"`
	LastPlayMs  int64     `gorm:"column:last_play_ms;not null;default:0"` // P3.6-B: last play time (re-arm cooldown)
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

// ReArmCooldownMin is the minutes a level must wait between plays (P3.6-B).
const ReArmCooldownMin = 20

// ReArmEligible (P3.6-B) reports whether a level may re-arm for another play:
// NOT consumed, freshness above the floor (not "done"), the setup has re-formed
// (not a bare re-touch), and the re-arm cooldown has elapsed since the last play.
func ReArmEligible(l *LevelStateDB, nowMs int64, cooldownMin int, setupReformed bool) (bool, string) {
	if l == nil {
		return false, "no level state"
	}
	if l.Consumed || l.Freshness == FreshnessDone {
		return false, "consumed / below-floor (done for the day)"
	}
	if !setupReformed {
		return false, "setup has not re-formed (bare re-touch)"
	}
	if cooldownMin <= 0 {
		cooldownMin = ReArmCooldownMin
	}
	if l.LastPlayMs > 0 && nowMs-l.LastPlayMs < int64(cooldownMin)*60_000 {
		return false, "within re-arm cooldown"
	}
	return true, ""
}

// TableName implements the gorm Tabler interface.
func (LevelStateDB) TableName() string { return "level_state" }

// MakeLevelKey builds the stable identity key for a level. P0-cleanup
// (2026-08-19): trader-scoped — two day-plan traders never share burn/freshness
// state (the cross-trader class the disease sweep flagged). An empty traderID
// yields the legacy unscoped key (pre-migration rows).
func MakeLevelKey(traderID, symbol, levelType, originDate string, binIndex int) string {
	if traderID == "" {
		return fmt.Sprintf("%s|%s|%s|%d", symbol, levelType, originDate, binIndex)
	}
	return fmt.Sprintf("%s|%s|%s|%s|%d", traderID, symbol, levelType, originDate, binIndex)
}

// NewLevelStateStore creates a LevelStateStore.
func NewLevelStateStore(db *gorm.DB) *LevelStateStore {
	return &LevelStateStore{db: db}
}

// initTables initializes the level_state table and runs the additive
// trader-scope migration: existing rows are backfilled to the SINGLE day-plan
// trader when exactly one exists (safe); otherwise their keys stay unscoped
// (legacy rows become cold — never shared).
func (s *LevelStateStore) initTables() error {
	if s.db.Dialector.Name() == "postgres" {
		var tableExists int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'level_state'`).Scan(&tableExists)
		if tableExists > 0 {
			return nil
		}
	}
	if err := s.db.AutoMigrate(&LevelStateDB{}); err != nil {
		return err
	}
	s.backfillTraderScope()
	return nil
}

// backfillTraderScope assigns unscoped level-state rows to the single day-plan
// trader when exactly one exists (additive; never shares across traders).
func (s *LevelStateStore) backfillTraderScope() {
	var ids []string
	if err := s.db.Table("traders").Pluck("id", &ids).Error; err != nil || len(ids) != 1 {
		return // zero or multiple traders → leave legacy rows cold
	}
	trader := ids[0]
	var rows []LevelStateDB
	if err := s.db.Where("trader_id = ''").Find(&rows).Error; err != nil {
		return
	}
	for i := range rows {
		oldKey := rows[i].LevelKey
		if strings.Contains(oldKey, trader+"|") {
			continue
		}
		s.db.Model(&LevelStateDB{}).Where("level_key = ?", oldKey).Updates(map[string]any{
			"trader_id": trader,
			"level_key": trader + "|" + oldKey,
		})
	}
}

// EnsureLevel creates the level with fresh state if its identity is new; if it
// already exists (re-derived in a later session), its STATE is preserved and
// only the price is refreshed — a burned level never returns fresh.
func (s *LevelStateStore) EnsureLevel(l *LevelStateDB) error {
	if l == nil || l.Symbol == "" || l.LevelType == "" {
		return fmt.Errorf("symbol and level_type required")
	}
	if l.LevelKey == "" {
		l.LevelKey = MakeLevelKey(l.TraderID, l.Symbol, l.LevelType, l.OriginDate, l.BinIndex)
	}
	if l.Freshness == "" {
		l.Freshness = FreshnessA
	}
	var existing LevelStateDB
	err := s.db.Where("level_key = ?", l.LevelKey).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		return s.db.Create(l).Error
	}
	if err != nil {
		return err
	}
	// Exists — keep state, refresh price only.
	return s.db.Model(&LevelStateDB{}).Where("level_key = ?", l.LevelKey).
		Update("price", l.Price).Error
}

// RecordTest atomically increments times_tested for a level.
func (s *LevelStateStore) RecordTest(levelKey string) error {
	return s.db.Model(&LevelStateDB{}).Where("level_key = ?", levelKey).
		Updates(map[string]any{"times_tested": gorm.Expr("times_tested + 1")}).Error
}

// RecordPlay (P3.6-B) registers a play on a level: times_tested++, stamps the
// last-play time (for the cooldown), and decays freshness one grade (A→B→C→done).
// Returns the new freshness grade.
func (s *LevelStateStore) RecordPlay(levelKey string, nowMs int64) (string, error) {
	if err := s.db.Model(&LevelStateDB{}).Where("level_key = ?", levelKey).
		Updates(map[string]any{
			"times_tested": gorm.Expr("times_tested + 1"),
			"last_play_ms": nowMs,
		}).Error; err != nil {
		return "", err
	}
	return s.DecrementFreshness(levelKey)
}

// MarkConsumed consumes a level (price accepted through it) — it ROLE-FLIPS
// (support↔resistance) and stays on the map; it is never deleted. Ageing
// (AgedFreshness) later heals the scar across sessions.
// MarkConsumed consumes a level (price accepted through it) — it ROLE-FLIPS
// (support↔resistance) and STAYS on the map at reduced weight.
// Level-truth wave (2026-08-27) — consumption IS a touch by definition
// (ConsumedSince requires LevelTouchedOn), but the old writer marked consumed
// without recording the play when the level was born already-accepted — rows
// read consumed=1 with times_tested=0 and last_play_ms=0 (8 live zones). The
// consuming touch is now recorded: a consumed row always carries ≥1 touch.
func (s *LevelStateStore) MarkConsumed(levelKey string, nowMs int64) error {
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	res := s.db.Model(&LevelStateDB{}).
		Where("level_key = ? AND consumed = ?", levelKey, false).
		Updates(map[string]any{
			"consumed":     true,
			"freshness":    FreshnessDone,
			"times_tested": gorm.Expr("CASE WHEN last_play_ms = 0 THEN 1 ELSE times_tested END"),
			"last_play_ms": gorm.Expr("CASE WHEN last_play_ms = 0 THEN ? ELSE last_play_ms END", nowMs),
		})
	return res.Error
}

// RepairConsumedWithoutTouch (T4, forensics hygiene 2026-08-28) backfills the
// legacy consumed=1 / times_tested=0 rows left by the pre-level-truth writer
// (it marked consumed without recording the consuming touch). Idempotent —
// after one pass no rows match. Stamps times_tested=1 and, where missing,
// derives last_play_ms from created_at.
func (s *LevelStateStore) RepairConsumedWithoutTouch() (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	res := s.db.Model(&LevelStateDB{}).
		Where("consumed = ? AND times_tested = ?", true, 0).
		Updates(map[string]any{
			"times_tested": 1,
			"last_play_ms": gorm.Expr("CASE WHEN last_play_ms = 0 THEN CAST(strftime('%s', created_at) AS INTEGER) * 1000 ELSE last_play_ms END"),
		})
	return res.RowsAffected, res.Error
}

// ResetBurns (P1b repair) clears every burned mark so the WINDOWED
// re-evaluation can rebuild consumption honestly from bars. Decayed rows land
// on FreshnessC (tested×2) — a repair never resurrects a level as fresh A.
// cutoffMs > 0 limits the reset to rows last updated before that instant
// (the purge of pre-fix burns). Returns rows affected.
func (s *LevelStateStore) ResetBurns(cutoffMs int64) (int64, error) {
	q := s.db.Model(&LevelStateDB{}).
		Where("consumed = ? OR freshness = ?", true, FreshnessDone)
	if cutoffMs > 0 {
		q = q.Where("updated_at < ?", time.UnixMilli(cutoffMs))
	}
	res := q.Updates(map[string]any{"consumed": false, "freshness": FreshnessC})
	return res.RowsAffected, res.Error
}

// cmeDayKey mirrors kernel.CMESessionDayKey (17:00 America/Chicago boundary)
// without importing kernel (store is imported BY kernel).
func cmeDayKey(t time.Time) string {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		chicago = time.UTC
	}
	ct := t.In(chicago)
	boundary := time.Date(ct.Year(), ct.Month(), ct.Day(), 17, 0, 0, 0, chicago)
	if ct.Hour() < 17 {
		boundary = boundary.AddDate(0, 0, -1)
	}
	return boundary.Format("2006-01-02")
}

// AgedFreshness (P1d) applies the explicit prior-session aging rule to a level's
// persisted state. A CONSUMED level role-flips for the rest of its session-day
// ("done"); once the CME session rolls, the scar heals one grade per period:
// next 1–2 session-days → "C" (tested×2, still flipped), and from the third
// session-day on → "B" (tested) so the level re-enters as a normal candidate.
// Non-consumed rows are returned unchanged.
func AgedFreshness(cur *LevelStateDB, now time.Time) string {
	if cur == nil {
		return ""
	}
	if !cur.Consumed && cur.Freshness != FreshnessDone {
		return cur.Freshness
	}
	// Age from the CONSUMING TOUCH, not updated_at [bug-hunt #53]: EnsureLevel
	// refreshes updated_at every cycle for every seated level, so a burned
	// level that keeps reappearing near price read days==0 ("done") forever.
	// last_play_ms is stamped by MarkConsumed at the consumption instant.
	last := time.UnixMilli(cur.LastPlayMs)
	if cur.LastPlayMs == 0 {
		last = cur.UpdatedAt
	}
	if last.IsZero() {
		last = cur.CreatedAt
	}
	if last.IsZero() {
		return FreshnessDone
	}
	days := 0
	for cmeDayKey(now.AddDate(0, 0, -days)) != cmeDayKey(last) {
		days++
		if days > 366 { // future / garbage timestamp → treat as fully healed
			return FreshnessB
		}
	}
	switch {
	case days == 0:
		return FreshnessDone // burned this session-day: still flipped
	case days <= 2:
		return FreshnessC // yesterday / day before: scar fading
	default:
		return FreshnessB // ≥3 session-days: healed to tested
	}
}

// DecrementFreshness decays a level one grade (A→B→C→done) and returns the new
// grade. Reaching "done" also marks it consumed.
func (s *LevelStateStore) DecrementFreshness(levelKey string) (string, error) {
	var cur LevelStateDB
	if err := s.db.Where("level_key = ?", levelKey).First(&cur).Error; err != nil {
		return "", err
	}
	next := nextFreshness(cur.Freshness)
	updates := map[string]any{"freshness": next}
	if next == FreshnessDone {
		updates["consumed"] = true
	}
	if err := s.db.Model(&LevelStateDB{}).Where("level_key = ?", levelKey).Updates(updates).Error; err != nil {
		return "", err
	}
	return next, nil
}

func nextFreshness(f string) string {
	switch f {
	case FreshnessA:
		return FreshnessB
	case FreshnessB:
		return FreshnessC
	default:
		return FreshnessDone // C or already-done → done
	}
}

// Get returns a level by key, or (nil, nil) if absent.
func (s *LevelStateStore) Get(levelKey string) (*LevelStateDB, error) {
	var l LevelStateDB
	err := s.db.Where("level_key = ?", levelKey).First(&l).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// Backdate rewrites a row's created/updated timestamps. TEST SUPPORT ONLY:
// consumption is windowed on created_at (P1c), and tests need a row to look
// like it was born when the synthetic bars began. Not for production use.
func (s *LevelStateStore) Backdate(levelKey string, t time.Time) error {
	return s.db.Model(&LevelStateDB{}).Where("level_key = ?", levelKey).
		Updates(map[string]any{"created_at": t, "updated_at": t}).Error
}

// ListForSymbol returns all stored levels for a symbol (any state).
func (s *LevelStateStore) ListForSymbol(symbol string) ([]*LevelStateDB, error) {
	var rows []*LevelStateDB
	err := s.db.Where("symbol = ?", symbol).Order("origin_date DESC, price ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ListValid returns levels for a symbol that are still tradeable — not consumed
// and freshness above done.
func (s *LevelStateStore) ListValid(symbol string) ([]*LevelStateDB, error) {
	var rows []*LevelStateDB
	err := s.db.Where("symbol = ? AND consumed = ? AND freshness <> ?", symbol, false, FreshnessDone).
		Order("origin_date DESC, price ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
