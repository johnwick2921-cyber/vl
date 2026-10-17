package store

import (
	"fmt"
	"nofx/logger"
	"sync"
	"time"

	"gorm.io/gorm"
)

// P0.2 — durable, append-only storage for the Day Plan system.
//
// Two tables:
//   - plans          : one immutable row per (plan_id, version). Never UPDATEd;
//                      a re-plan appends a new version. doc holds the plan JSON,
//                      guarded by CHECK(json_valid(doc)).
//   - plan_overlays  : append-only RFC-6902 patch versions per (plan_id,
//                      plan_version); owner/AI edits each add a new overlay_version.
//
// The executor_decisions table (store/decision.go) gains 4 additive FK columns
// (plan_id, plan_version, overlay_version, cited_scenario_id) so a decision can
// be joined back to the exact plan+overlay+scenario it cited.
//
// Writes go through a dedicated SINGLE-WRITER GOROUTINE. The append-only version
// assignment is read-max-then-insert (two statements); SetMaxOpenConns(1) alone
// serializes each statement but NOT the pair, so two concurrent appends could
// read the same max and collide on the composite PK. The writer goroutine runs
// each write closure to completion before the next, making the read+insert
// atomic without a table lock. (WAL — see store/gorm.go — lets card/replay reads
// proceed alongside; raising MaxOpenConns later stays safe because this goroutine
// remains the only writer of these tables.)

// PlanDB is one immutable plan version. TableName → "plans".
type PlanDB struct {
	PlanID  string `gorm:"column:plan_id;primaryKey"` // stable per (trade_date, session); see MakePlanID
	Version int    `gorm:"column:version;primaryKey"` // append-only, 1-based
	// S3 (mega-research 2026-08-26) — SEMANTICS NOTE: this column stores the
	// TRADER id, not the strategy id (historical; do NOT repurpose — analysis
	// joins use position_plan_join, which keys on plan_id).
	StrategyID    string `gorm:"column:strategy_id;not null;default:''"` // trader id (historical naming)
	TradeDate     string `gorm:"column:trade_date;not null;default:''"`  // CME session-day YYYY-MM-DD
	Session       string `gorm:"column:session;not null;default:'NY'"`   // NY|ASIA|LONDON
	TriggerReason string `gorm:"column:trigger_reason;not null;default:''"`
	Lifecycle     string `gorm:"column:lifecycle;not null;default:'active'"` // active|expired|died|superseded
	ModelID       string `gorm:"column:model_id;not null;default:''"`        // resolved planner model id
	PromptHash    string `gorm:"column:prompt_hash;not null;default:''"`
	Doc           string `gorm:"column:doc;not null;default:'{}'"` // plan JSON; CHECK(json_valid(doc)) added in DDL
	// W11 — the indicator mirror the planner saw, FROZEN at read time (replay-safe:
	// later ai_config toggle changes never rewrite history) + the ai_config
	// fingerprint (which indicator config produced this plan).
	IndicatorsBlock string `gorm:"column:indicators_block;not null;default:''"`
	AIConfigHash    string `gorm:"column:ai_config_hash;not null;default:''"`
	// P2 — how much of the 7-field regime map was DARK when this plan was written,
	// and whether that crossed the "don't fully trust this" threshold. Stamped at
	// write so a replay can tell a full-map plan from a half-blind one.
	DarkRegimeCount int       `gorm:"column:dark_regime_count;not null;default:0"`
	Degraded        bool      `gorm:"column:degraded;not null;default:false"`
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime"`
}

// TableName implements the gorm Tabler interface.
func (PlanDB) TableName() string { return "plans" }

// PlanOverlayDB is one append-only overlay (RFC-6902 patch) version.
type PlanOverlayDB struct {
	PlanID         string    `gorm:"column:plan_id;primaryKey"`
	PlanVersion    int       `gorm:"column:plan_version;primaryKey"`
	OverlayVersion int       `gorm:"column:overlay_version;primaryKey"` // append-only, 1-based per (plan_id, plan_version)
	OverlayID      string    `gorm:"column:overlay_id;not null;default:''"`
	Patch          string    `gorm:"column:patch;not null;default:'[]'"` // RFC-6902 JSON; CHECK(json_valid(patch)) in DDL
	Origin         string    `gorm:"column:origin;not null;default:''"`  // owner|ai
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"`
}

// TableName implements the gorm Tabler interface.
func (PlanOverlayDB) TableName() string { return "plan_overlays" }

// MakePlanID builds the stable plan identity from its (trade_date, session) key
// so the (trade_date, session, version) uniqueness the campaign mandates holds.
func MakePlanID(tradeDate, session string) string {
	return tradeDate + ":" + session
}

// MakePlanIDForTrader builds the trader-scoped plan identity (P0-A2). Legacy
// rows use MakePlanID(tradeDate, session) — session-GLOBAL — so two day-plan
// traders shared one plan chain and the last writer's version governed both.
// All NEW writers must use the trader-scoped form; readers match on
// (trade_date, session, strategy_id) so both id forms keep working.
func MakePlanIDForTrader(traderID, tradeDate, session string) string {
	return tradeDate + ":" + session + ":" + traderID
}

// ResolvePlanID returns the plan_id a writer must append under: the trader's
// EXISTING chain id for this (trade_date, session) if one exists — version
// continuity across owner resets depends on it (a reset starts a new chain at
// v1 only when there is nothing to continue) — otherwise a fresh trader-scoped
// id. Two traders on the same session can never land on the same chain.
func (s *PlanStore) ResolvePlanID(tradeDate, session, strategyID string) string {
	var row PlanDB
	err := s.db.Select("plan_id").
		Where("trade_date = ? AND session = ? AND strategy_id = ?", tradeDate, session, strategyID).
		Order("version DESC").First(&row).Error
	if err == nil && row.PlanID != "" {
		return row.PlanID // continue the trader's chain (legacy or scoped id)
	}
	return MakePlanIDForTrader(strategyID, tradeDate, session)
}

const plansDDL = `
CREATE TABLE IF NOT EXISTS plans (
	plan_id        TEXT    NOT NULL,
	version        INTEGER NOT NULL,
	strategy_id    TEXT    NOT NULL DEFAULT '',
	trade_date     TEXT    NOT NULL DEFAULT '',
	session        TEXT    NOT NULL DEFAULT 'NY',
	trigger_reason TEXT    NOT NULL DEFAULT '',
	lifecycle      TEXT    NOT NULL DEFAULT 'active',
	model_id       TEXT    NOT NULL DEFAULT '',
	prompt_hash    TEXT    NOT NULL DEFAULT '',
	doc            TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(doc)),
	indicators_block TEXT  NOT NULL DEFAULT '',
	ai_config_hash TEXT    NOT NULL DEFAULT '',
	dark_regime_count INTEGER NOT NULL DEFAULT 0,
	degraded       INTEGER NOT NULL DEFAULT 0,
	created_at     DATETIME,
	PRIMARY KEY (plan_id, version)
)`

const planOverlaysDDL = `
CREATE TABLE IF NOT EXISTS plan_overlays (
	plan_id         TEXT    NOT NULL,
	plan_version    INTEGER NOT NULL,
	overlay_version INTEGER NOT NULL,
	overlay_id      TEXT    NOT NULL DEFAULT '',
	patch           TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(patch)),
	origin          TEXT    NOT NULL DEFAULT '',
	created_at      DATETIME,
	PRIMARY KEY (plan_id, plan_version, overlay_version)
)`

// PlanStore is the append-only plan/overlay store with a single-writer goroutine.
type PlanStore struct {
	db        *gorm.DB
	writeCh   chan planWriteReq
	done      chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

type planWriteReq struct {
	fn    func(*gorm.DB) error
	reply chan error
}

// NewPlanStore creates a PlanStore. The writer goroutine starts lazily on the
// first write.
func NewPlanStore(db *gorm.DB) *PlanStore {
	return &PlanStore{
		db:      db,
		writeCh: make(chan planWriteReq),
		done:    make(chan struct{}),
	}
}

// initTables creates the plans + plan_overlays tables. On SQLite (the live +
// tested dialect) it uses raw DDL so the composite PK and the json_valid CHECK
// are exact; on other dialects it falls back to AutoMigrate (no SQLite-only
// json_valid CHECK). Both are idempotent (IF NOT EXISTS / additive).
func (s *PlanStore) initTables() error {
	if s.db.Dialector.Name() == "sqlite" {
		if err := s.db.Exec(plansDDL).Error; err != nil {
			return fmt.Errorf("create plans table: %w", err)
		}
		if err := s.db.Exec(planOverlaysDDL).Error; err != nil {
			return fmt.Errorf("create plan_overlays table: %w", err)
		}
		// P0-A2 — the campaign key now includes strategy_id. The legacy unique
		// (trade_date, session, version) made two day-plan traders on one
		// session fight over the SAME version number, which forced them to
		// share a plan chain. Drop it (existing rows cannot violate the new
		// key: the old index is exactly what kept them apart) and enforce the
		// trader-scoped key.
		s.db.Exec(`DROP INDEX IF EXISTS idx_plans_date_session_version`)
		s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_plans_date_session_strategy_version ON plans(trade_date, session, strategy_id, version)`)
		s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_plans_strategy ON plans(strategy_id)`)
		s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_overlays_plan ON plan_overlays(plan_id, plan_version)`)
		// W11 — idempotent additive columns for EXISTING sqlite DBs (CREATE TABLE IF
		// NOT EXISTS won't add them). SQLite has no ADD COLUMN IF NOT EXISTS, so the
		// duplicate-column error on re-run is expected + swallowed (same as the
		// index Execs above).
		s.db.Exec(`ALTER TABLE plans ADD COLUMN indicators_block TEXT NOT NULL DEFAULT ''`)
		s.db.Exec(`ALTER TABLE plans ADD COLUMN ai_config_hash TEXT NOT NULL DEFAULT ''`)
		s.db.Exec(`ALTER TABLE plans ADD COLUMN dark_regime_count INTEGER NOT NULL DEFAULT 0`)
		s.db.Exec(`ALTER TABLE plans ADD COLUMN degraded INTEGER NOT NULL DEFAULT 0`)
		return nil
	}
	return s.db.AutoMigrate(&PlanDB{}, &PlanOverlayDB{})
}

// enqueue runs fn on the single writer goroutine and blocks for its result.
func (s *PlanStore) enqueue(fn func(*gorm.DB) error) error {
	s.startOnce.Do(func() {
		go s.writerLoop()
	})
	req := planWriteReq{fn: fn, reply: make(chan error, 1)}
	select {
	case s.writeCh <- req:
	case <-s.done:
		return fmt.Errorf("plan store closed")
	}
	select {
	case err := <-req.reply:
		return err
	case <-s.done:
		return fmt.Errorf("plan store closed")
	}
}

func (s *PlanStore) writerLoop() {
	for {
		select {
		case req := <-s.writeCh:
			req.reply <- s.execWrite(req.fn)
		case <-s.done:
			return
		}
	}
}

func (s *PlanStore) execWrite(fn func(*gorm.DB) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plan write panicked: %v", r)
		}
	}()
	return fn(s.db)
}

// Close stops the writer goroutine. Intended for tests/shutdown; the process
// Store never calls it (process exit reclaims the goroutine).
func (s *PlanStore) Close() {
	s.stopOnce.Do(func() { close(s.done) })
}

// AppendPlan appends a new immutable plan version (never UPDATE). Version is
// assigned as max(version)+1 for the plan_id inside the single-writer goroutine.
// Returns the assigned version.
func (s *PlanStore) AppendPlan(p *PlanDB) (int, error) {
	if p == nil || p.PlanID == "" {
		return 0, fmt.Errorf("plan_id required")
	}
	var assigned int
	err := s.enqueue(func(db *gorm.DB) error {
		var maxV *int
		if err := db.Model(&PlanDB{}).Where("plan_id = ?", p.PlanID).
			Select("MAX(version)").Scan(&maxV).Error; err != nil {
			return err
		}
		next := 1
		if maxV != nil {
			next = *maxV + 1
		}
		p.Version = next
		if p.Doc == "" {
			p.Doc = "{}"
		}
		if p.Lifecycle == "" {
			p.Lifecycle = "active"
		}
		if p.Session == "" {
			p.Session = "NY"
		}
		if err := db.Create(p).Error; err != nil {
			return err
		}
		assigned = next
		return nil
	})
	return assigned, err
}

// UpdatePlanLifecycle (plan-lifecycle wave, 2026-08-27) mutates ONE existing
// plan row's lifecycle + trigger_reason in place — the dormant/re-arm transition
// changes STATE, not version (the chain is append-only for VERSIONS, not for
// state markers). Idempotent; WHERE-scoped to (plan_id, version).
func (s *PlanStore) UpdatePlanLifecycle(planID string, version int, lifecycle, triggerReason string) error {
	if planID == "" || version <= 0 {
		return fmt.Errorf("plan_id and version required")
	}
	return s.enqueue(func(db *gorm.DB) error {
		// D3 (2026-09-03): the lifecycle marker NO LONGER overwrites
		// trigger_reason. That column is the AUTHORING trigger; the marker
		// appends to plan_lifecycle_log, so a row answers both questions.
		res := db.Model(&PlanDB{}).
			Where("plan_id = ? AND version = ?", planID, version).
			Update("lifecycle", lifecycle)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("plan row %s v%d not found", planID, version)
		}
		// The log is telemetry: a failure here WARNs and the transition still
		// stands (class 23 — an instrument never blocks the thing it measures).
		if err := db.Create(&PlanLifecycleEvent{
			PlanID: planID, Version: version, Event: lifecycle,
			Reason: triggerReason, At: time.Now(),
		}).Error; err != nil {
			logger.Warnf("🗓 plan lifecycle log append failed (%s v%d → %s): %v", planID, version, lifecycle, err)
		}
		return nil
	})
}

// AppendOverlay appends a new overlay version for (plan_id, plan_version).
// Returns the assigned overlay_version.
func (s *PlanStore) AppendOverlay(o *PlanOverlayDB) (int, error) {
	if o == nil || o.PlanID == "" || o.PlanVersion <= 0 {
		return 0, fmt.Errorf("plan_id and plan_version required")
	}
	var assigned int
	err := s.enqueue(func(db *gorm.DB) error {
		var maxV *int
		if err := db.Model(&PlanOverlayDB{}).
			Where("plan_id = ? AND plan_version = ?", o.PlanID, o.PlanVersion).
			Select("MAX(overlay_version)").Scan(&maxV).Error; err != nil {
			return err
		}
		next := 1
		if maxV != nil {
			next = *maxV + 1
		}
		o.OverlayVersion = next
		if o.Patch == "" {
			o.Patch = "[]"
		}
		if err := db.Create(o).Error; err != nil {
			return err
		}
		assigned = next
		return nil
	})
	return assigned, err
}

// GetPlan returns a specific plan version, or (nil, nil) if absent.
func (s *PlanStore) GetPlan(planID string, version int) (*PlanDB, error) {
	var p PlanDB
	err := s.db.Where("plan_id = ? AND version = ?", planID, version).First(&p).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetLatestPlan returns the newest version for a plan_id, or (nil, nil) if none.
func (s *PlanStore) GetLatestPlan(planID string) (*PlanDB, error) {
	var p PlanDB
	err := s.db.Where("plan_id = ?", planID).Order("version DESC").First(&p).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetLatestPlanForSession returns the newest plan version for a (trade_date,
// session), or (nil, nil) if none.
func (s *PlanStore) GetLatestPlanForSession(tradeDate, session string) (*PlanDB, error) {
	var p PlanDB
	err := s.db.Where("trade_date = ? AND session = ?", tradeDate, session).
		Order("version DESC").First(&p).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetLatestPlanForTraderSession returns the newest plan version for ONE trader
// (strategy_id holds the trader id — the column is misnamed) within a
// (trade_date, session), or (nil, nil) if none.
//
// P0-A (2026-08-18): the unscoped GetLatestPlanForSession let two day-plan
// traders share one plan chain — the last writer's version governed BOTH
// executors. Every production plan reader must use THIS scoped form, pinned by
// a source-scan test.
func (s *PlanStore) GetLatestPlanForTraderSession(tradeDate, session, strategyID string) (*PlanDB, error) {
	var p PlanDB
	err := s.db.Where("trade_date = ? AND session = ? AND strategy_id = ?", tradeDate, session, strategyID).
		Order("version DESC").First(&p).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListRecent returns the most recent plan rows (any session) newest-first, for
// the plan-history view.
func (s *PlanStore) ListRecent(limit int) ([]*PlanDB, error) {
	if limit <= 0 {
		limit = 30
	}
	var rows []*PlanDB
	err := s.db.Order("created_at DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ListRecentForTrader is ListRecent scoped to one trader (P0-A).
func (s *PlanStore) ListRecentForTrader(traderID string, limit int) ([]*PlanDB, error) {
	if limit <= 0 {
		limit = 30
	}
	var rows []*PlanDB
	err := s.db.Where("strategy_id = ?", traderID).Order("created_at DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// UncarriedEditsKey is the system_config key holding the owner edits that could
// not be carried into a plan version (ITEM 4). Mirrors the scenario_status
// precedent so the review list needs no migration.
func UncarriedEditsKey(planID string, version int) string {
	return fmt.Sprintf("uncarried_edits:%s:v%d", planID, version)
}

// TransitionKey names the system_config slot the G4 transition stand-down
// state is mirrored to (the plan card's "⏸ TRANSITION" chip reads it). Empty
// inputs → "" (callers skip persistence).
func TransitionKey(planID string, version int) string {
	if planID == "" {
		return ""
	}
	return fmt.Sprintf("transition:%s:v%d", planID, version)
}

// ListVersions returns EVERY stored version of one session's plan, oldest first.
//
// The plans table has always been append-only (PRIMARY KEY (plan_id, version);
// AppendPlan assigns max(version)+1 and only ever INSERTs), so the full history
// of a session — including each superseded version's doc and the trigger_reason
// that produced its successor — has been durable all along. Nothing could read
// it: GetLatestPlanForSession returns exactly one row and ListRecent is a global
// 30-row feed across every plan_id and trader. This is the missing read path.
func (s *PlanStore) ListVersions(tradeDate, session string) ([]*PlanDB, error) {
	var rows []*PlanDB
	err := s.db.Where("plan_id = ?", MakePlanID(tradeDate, session)).
		Order("version ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ListVersionsForTrader is ListVersions scoped to ONE trader's chain (P0-A):
// the version chips must never list another trader's versions. Matches on
// (trade_date, session, strategy_id) so both legacy (date:session) and
// trader-scoped (date:session:trader) plan_ids are found.
func (s *PlanStore) ListVersionsForTrader(tradeDate, session, strategyID string) ([]*PlanDB, error) {
	var rows []*PlanDB
	err := s.db.Where("trade_date = ? AND session = ? AND strategy_id = ?", tradeDate, session, strategyID).
		Order("version ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ListOverlays returns all overlay versions for (plan_id, plan_version) in
// ascending overlay_version order.
func (s *PlanStore) ListOverlays(planID string, planVersion int) ([]*PlanOverlayDB, error) {
	var rows []*PlanOverlayDB
	err := s.db.Where("plan_id = ? AND plan_version = ?", planID, planVersion).
		Order("overlay_version ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ── LIFECYCLE LOG (data-integrity wave D3, 2026-09-03) ──────────────────────
//
// UpdatePlanLifecycle used to write the lifecycle marker INTO trigger_reason,
// so a plan row could answer "why was this parked" or "why was this authored",
// never both — and the second answer destroyed the first. Live rows that lost
// their authoring trigger this way:
//
//	2026-08-27:ASIA … v7  active   "rearmed:2x5m close back below 29678.25 …"
//	2026-08-28:NY   … v2  dormant  "dormant:death:death-condition: 15m_close …"
//	2026-08-26:ASIA … v10 dormant  "dormant:flip:flip-condition: 15m_close …"
//
// trigger_reason is the AUTHORING trigger now and nothing else. Every
// transition appends here instead, so both questions have an answer and the
// order is recoverable.

// PlanLifecycleEvent is one transition.
type PlanLifecycleEvent struct {
	ID      int64     `gorm:"column:id;primaryKey;autoIncrement"`
	PlanID  string    `gorm:"column:plan_id;index"`
	Version int       `gorm:"column:version"`
	Event   string    `gorm:"column:event"`  // the lifecycle it moved TO
	Reason  string    `gorm:"column:reason"` // the marker the caller passed
	At      time.Time `gorm:"column:at"`
}

func (PlanLifecycleEvent) TableName() string { return "plan_lifecycle_log" }

const planLifecycleLogDDL = `
CREATE TABLE IF NOT EXISTS plan_lifecycle_log (
	id      INTEGER PRIMARY KEY AUTOINCREMENT,
	plan_id TEXT    NOT NULL DEFAULT '',
	version INTEGER NOT NULL DEFAULT 0,
	event   TEXT    NOT NULL DEFAULT '',
	reason  TEXT    NOT NULL DEFAULT '',
	at      DATETIME
);
CREATE INDEX IF NOT EXISTS idx_plan_lifecycle_log_plan ON plan_lifecycle_log(plan_id, version, id);
`

// MigrateLifecycleLog creates the table. Idempotent.
func (s *PlanStore) MigrateLifecycleLog() error {
	return s.db.Exec(planLifecycleLogDDL).Error
}

// LifecycleLog returns one plan version's transitions, oldest first.
func (s *PlanStore) LifecycleLog(planID string, version int) ([]PlanLifecycleEvent, error) {
	var out []PlanLifecycleEvent
	err := s.db.Where("plan_id = ? AND version = ?", planID, version).
		Order("id").Find(&out).Error
	return out, err
}
