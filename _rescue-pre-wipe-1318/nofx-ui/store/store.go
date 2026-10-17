// Package store provides unified database storage layer
// All database operations should go through this package
package store

import (
	"database/sql"
	"fmt"
	"nofx/logger"
	"sync"

	"gorm.io/gorm"
)

// Store unified data storage interface
type Store struct {
	gdb    *gorm.DB  // GORM database connection
	db     *sql.DB   // Legacy sql.DB for backward compatibility
	driver *DBDriver // Database driver for abstraction (legacy)

	// Sub-stores (lazy initialization)
	user             *UserStore
	aiModel          *AIModelStore
	exchange         *ExchangeStore
	trader           *TraderStore
	decision         *DecisionStore
	position         *PositionStore
	strategy         *StrategyStore
	equity           *EquityStore
	order            *OrderStore
	grid             *GridStore
	aiCharge         *AIChargeStore
	plan             *PlanStore
	levelState       *LevelStateStore
	sessionProfile   *SessionProfileStore
	barHistory       *BarHistoryStore
	armedOrders      *ArmedOrderStore
	abConfirm        *AbConfirmStore
	tradeExcursions  *TradeExcursionStore
	plannerRejected  *PlannerRejectedStore
	plannerReadFacts *PlannerReadFactsStore
	configChanges    *ConfigChangeStore
	watchdogFires    *WatchdogFireStore
	levelStats       *LevelStatsStore
	touchEpisodes    *TouchEpisodeStore
	calendarSlice    *CalendarSliceStore
	digest           *DigestStore
	ownerLevel       *OwnerLevelStore
	alert            *AlertStore
	logEvent         *LogEventStore
	watchAssessment  *WatchAssessmentStore
	planQA           *PlanQAStore
	matchedRandom    *MatchedRandomStore
	telegramConfig   TelegramConfigStore

	mu sync.RWMutex
}

// New creates new Store instance (SQLite mode for backward compatibility)
func New(dbPath string) (*Store, error) {
	gdb, err := InitGorm(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Get underlying sql.DB for legacy compatibility
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	s := &Store{gdb: gdb, db: sqlDB}

	// Initialize all table structures
	if err := s.initTables(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to initialize table structure: %w", err)
	}

	// Initialize default data
	if err := s.initDefaultData(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to initialize default data: %w", err)
	}

	logger.Infof("✅ Database initialized (GORM, SQLite)")
	return s, nil
}

// NewWithConfig creates new Store instance with provided database configuration
func NewWithConfig(cfg DBConfig) (*Store, error) {
	gdb, err := InitGormWithConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Get underlying sql.DB for legacy compatibility
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	s := &Store{gdb: gdb, db: sqlDB}

	// Initialize all table structures
	if err := s.initTables(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to initialize table structure: %w", err)
	}

	// Initialize default data
	if err := s.initDefaultData(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to initialize default data: %w", err)
	}

	dbTypeStr := "SQLite"
	if cfg.Type == DBTypePostgres {
		dbTypeStr = "PostgreSQL"
	}
	logger.Infof("✅ Database initialized (GORM, %s)", dbTypeStr)
	return s, nil
}

// NewFromGorm creates Store from existing GORM connection
func NewFromGorm(gdb *gorm.DB) (*Store, error) {
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}
	return &Store{gdb: gdb, db: sqlDB}, nil
}

// NewFromDB creates Store from existing database connection (legacy)
// Deprecated: Use NewFromGorm instead
func NewFromDB(db *sql.DB) *Store {
	return &Store{db: db}
}

// initTables initializes all database tables using GORM AutoMigrate
func (s *Store) initTables() error {
	// Create system_config table (GORM handles this via raw SQL for simplicity)
	if err := s.gdb.Exec(`
		CREATE TABLE IF NOT EXISTS system_config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)
	`).Error; err != nil {
		return fmt.Errorf("failed to create system_config table: %w", err)
	}

	// Initialize sub-store tables
	if err := s.User().initTables(); err != nil {
		return fmt.Errorf("failed to initialize user tables: %w", err)
	}
	if err := s.AIModel().initTables(); err != nil {
		return fmt.Errorf("failed to initialize AI model tables: %w", err)
	}
	if err := s.Exchange().initTables(); err != nil {
		return fmt.Errorf("failed to initialize exchange tables: %w", err)
	}
	if err := s.Trader().initTables(); err != nil {
		return fmt.Errorf("failed to initialize trader tables: %w", err)
	}
	if err := s.Decision().initTables(); err != nil {
		return fmt.Errorf("failed to initialize decision log tables: %w", err)
	}
	if err := s.Position().InitTables(); err != nil {
		return fmt.Errorf("failed to initialize position tables: %w", err)
	}
	if err := s.Strategy().initTables(); err != nil {
		return fmt.Errorf("failed to initialize strategy tables: %w", err)
	}
	if err := s.Equity().initTables(); err != nil {
		return fmt.Errorf("failed to initialize equity tables: %w", err)
	}
	if err := s.Order().InitTables(); err != nil {
		return fmt.Errorf("failed to initialize order tables: %w", err)
	}
	if err := s.Grid().InitTables(); err != nil {
		return fmt.Errorf("failed to initialize grid tables: %w", err)
	}
	if err := s.TelegramConfig().(*telegramConfigStore).initTables(); err != nil {
		return fmt.Errorf("failed to initialize telegram config tables: %w", err)
	}
	if err := s.AICharge().initTables(); err != nil {
		return fmt.Errorf("failed to initialize AI charge tables: %w", err)
	}
	if err := s.Plan().initTables(); err != nil {
		return fmt.Errorf("failed to initialize plan tables: %w", err)
	}
	if err := s.LevelState().initTables(); err != nil {
		return fmt.Errorf("failed to initialize level_state tables: %w", err)
	}
	if err := s.SessionProfile().initTables(); err != nil {
		return fmt.Errorf("failed to initialize session_profiles tables: %w", err)
	}
	if err := s.Calendar().initTables(); err != nil {
		return fmt.Errorf("failed to initialize calendar_slices tables: %w", err)
	}
	if err := s.Digest().initTables(); err != nil {
		return fmt.Errorf("failed to initialize day_plan_digests tables: %w", err)
	}
	if err := s.OwnerLevel().initTables(); err != nil {
		return fmt.Errorf("failed to initialize owner_levels tables: %w", err)
	}
	if err := s.Alert().initTables(); err != nil {
		return fmt.Errorf("failed to initialize day_plan_alerts tables: %w", err)
	}
	if err := s.WatchAssessment().initTables(); err != nil {
		return fmt.Errorf("failed to initialize watch assessment tables: %w", err)
	}
	if err := s.LogEvent().initTables(); err != nil {
		return fmt.Errorf("failed to initialize log_events tables: %w", err)
	}
	if err := s.PlanQA().initTables(); err != nil {
		return fmt.Errorf("failed to initialize plan_qa tables: %w", err)
	}
	if err := s.MatchedRandom().initTables(); err != nil {
		return fmt.Errorf("failed to initialize matched_random tables: %w", err)
	}
	if err := s.ArmedOrders().Migrate(); err != nil {
		return fmt.Errorf("failed to initialize armed_orders table: %w", err)
	}
	if err := s.AbConfirm().Migrate(); err != nil {
		return fmt.Errorf("failed to initialize ab_confirm_log table: %w", err)
	}
	if err := s.TradeExcursions().Migrate(); err != nil {
		return fmt.Errorf("failed to initialize trade_excursions table: %w", err)
	}
	// E4 (wave 1A, 2026-09-02) — retire the never-computed mae/mfe zeros on
	// trader_positions. Idempotent: a second boot matches nothing.
	if _, err := s.Position().MigrateExcursionZerosToNull(); err != nil {
		logger.Warnf("📐 excursion zero→NULL migration skipped: %v", err)
	}
	return nil
}

// initDefaultData initializes default data
func (s *Store) initDefaultData() error {
	if err := s.AIModel().initDefaultData(); err != nil {
		return err
	}
	if err := s.Exchange().initDefaultData(); err != nil {
		return err
	}
	if err := s.Strategy().initDefaultData(); err != nil {
		return err
	}
	// Migrate old decision_account_snapshots data to new trader_equity_snapshots table
	if migrated, err := s.Equity().MigrateFromDecision(); err != nil {
		logger.Warnf("failed to migrate equity data: %v", err)
	} else if migrated > 0 {
		logger.Infof("✅ Migrated %d equity records to new table", migrated)
	}
	return nil
}

// User gets user storage
func (s *Store) User() *UserStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.user == nil {
		s.user = NewUserStore(s.gdb)
	}
	return s.user
}

// AIModel gets AI model storage
func (s *Store) AIModel() *AIModelStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aiModel == nil {
		s.aiModel = NewAIModelStore(s.gdb)
	}
	return s.aiModel
}

// Exchange gets exchange storage
func (s *Store) Exchange() *ExchangeStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exchange == nil {
		s.exchange = NewExchangeStore(s.gdb)
	}
	return s.exchange
}

// Trader gets trader storage
func (s *Store) Trader() *TraderStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trader == nil {
		s.trader = NewTraderStore(s.gdb)
	}
	return s.trader
}

// Decision gets decision log storage
func (s *Store) Decision() *DecisionStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.decision == nil {
		s.decision = NewDecisionStore(s.gdb)
	}
	return s.decision
}

// Position gets position storage
func (s *Store) Position() *PositionStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.position == nil {
		s.position = NewPositionStore(s.gdb)
	}
	return s.position
}

// Strategy gets strategy storage
func (s *Store) Strategy() *StrategyStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.strategy == nil {
		s.strategy = NewStrategyStore(s.gdb)
	}
	return s.strategy
}

// Equity gets equity storage
func (s *Store) Equity() *EquityStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.equity == nil {
		s.equity = NewEquityStore(s.gdb)
	}
	return s.equity
}

// Order gets order storage
func (s *Store) Order() *OrderStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.order == nil {
		s.order = NewOrderStore(s.gdb)
	}
	return s.order
}

// Grid gets grid trading storage
func (s *Store) Grid() *GridStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.grid == nil {
		s.grid = NewGridStore(s.gdb)
	}
	return s.grid
}

// AICharge gets AI charge storage
func (s *Store) AICharge() *AIChargeStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aiCharge == nil {
		s.aiCharge = NewAIChargeStore(s.gdb)
	}
	return s.aiCharge
}

// Plan gets the Day Plan append-only storage (plans + plan_overlays).
func (s *Store) Plan() *PlanStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.plan == nil {
		s.plan = NewPlanStore(s.gdb)
	}
	return s.plan
}

// LevelState gets the cross-session level-state storage.
func (s *Store) LevelState() *LevelStateStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.levelState == nil {
		s.levelState = NewLevelStateStore(s.gdb)
	}
	return s.levelState
}

// BarHistory gets the closed-bar persistence store (2026-08-26).
func (s *Store) BarHistory() *BarHistoryStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.barHistory == nil {
		s.barHistory = NewBarHistoryStore(s.gdb)
	}
	return s.barHistory
}

// ArmedOrders gets the armed-orders ledger (Wave 2, 2026-08-27).
func (s *Store) ArmedOrders() *ArmedOrderStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.armedOrders == nil {
		s.armedOrders = NewArmedOrderStore(s.gdb)
	}
	return s.armedOrders
}

// PlannerRejected returns the rejected-planner-prompt store (lazy, planner-speed
// wave 1.4).
// WatchdogFires — the stream-watchdog fire log (owner ruling 2026-09-02).
func (s *Store) WatchdogFires() *WatchdogFireStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watchdogFires == nil {
		s.watchdogFires = NewWatchdogFireStore(s.gdb)
	}
	return s.watchdogFires
}

// ConfigChanges (REPAIR-PARSE E5) — the per-save resolved-knob diff.
func (s *Store) ConfigChanges() *ConfigChangeStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.configChanges == nil {
		s.configChanges = NewConfigChangeStore(s.gdb)
	}
	return s.configChanges
}

// PlannerReadFacts gets the per-read facts table (VOID PARITY, 2026-09-02) —
// what the model was TOLD on every read, accepted or rejected.
func (s *Store) PlannerReadFacts() *PlannerReadFactsStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.plannerReadFacts == nil {
		s.plannerReadFacts = NewPlannerReadFactsStore(s.gdb)
	}
	return s.plannerReadFacts
}

func (s *Store) PlannerRejected() *PlannerRejectedStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.plannerRejected == nil {
		s.plannerRejected = NewPlannerRejectedStore(s.gdb)
	}
	return s.plannerRejected
}

// TradeExcursions gets the per-position excursion table (wave 1A, 2026-09-02).
func (s *Store) TradeExcursions() *TradeExcursionStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tradeExcursions == nil {
		s.tradeExcursions = NewTradeExcursionStore(s.gdb)
	}
	return s.tradeExcursions
}

// AbConfirm gets the E8 shadow A/B counterfactual table (2026-08-30).
func (s *Store) AbConfirm() *AbConfirmStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.abConfirm == nil {
		s.abConfirm = NewAbConfirmStore(s.gdb)
	}
	return s.abConfirm
}

// LevelStats gets the B4 forward-validation store (2026-08-26).
func (s *Store) LevelStats() *LevelStatsStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.levelStats == nil {
		s.levelStats = NewLevelStatsStore(s.gdb)
	}
	return s.levelStats
}

// TouchEpisodes gets the T1 touch-telemetry store (2026-08-26).
func (s *Store) TouchEpisodes() *TouchEpisodeStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.touchEpisodes == nil {
		s.touchEpisodes = NewTouchEpisodeStore(s.gdb)
	}
	return s.touchEpisodes
}

// SessionProfile gets the durable session-profile storage.
func (s *Store) SessionProfile() *SessionProfileStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionProfile == nil {
		s.sessionProfile = NewSessionProfileStore(s.gdb)
	}
	return s.sessionProfile
}

// Calendar gets the per-day calendar-slice storage.
func (s *Store) Calendar() *CalendarSliceStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calendarSlice == nil {
		s.calendarSlice = NewCalendarSliceStore(s.gdb)
	}
	return s.calendarSlice
}

// Digest gets the day-plan digest storage.
func (s *Store) Digest() *DigestStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.digest == nil {
		s.digest = NewDigestStore(s.gdb)
	}
	return s.digest
}

// OwnerLevel gets the sticky owner-level storage.
func (s *Store) OwnerLevel() *OwnerLevelStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ownerLevel == nil {
		s.ownerLevel = NewOwnerLevelStore(s.gdb)
	}
	return s.ownerLevel
}

// Alert gets the in-app alert storage.
func (s *Store) Alert() *AlertStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.alert == nil {
		s.alert = NewAlertStore(s.gdb)
	}
	return s.alert
}

// WatchAssessment gets the Phase-3.6 watcher scoring storage.
func (s *Store) WatchAssessment() *WatchAssessmentStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watchAssessment == nil {
		s.watchAssessment = NewWatchAssessmentStore(s.gdb)
	}
	return s.watchAssessment
}

// LogEvent gets the P6 log-shipping storage (WARN+ → DB, async).
func (s *Store) LogEvent() *LogEventStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.logEvent == nil {
		s.logEvent = NewLogEventStore(s.gdb)
	}
	return s.logEvent
}

// PlanQA gets the Ask-Planner thread storage.
func (s *Store) PlanQA() *PlanQAStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.planQA == nil {
		s.planQA = NewPlanQAStore(s.gdb)
	}
	return s.planQA
}

// MatchedRandom gets the matched-random verdict storage (stats honesty gate).
func (s *Store) MatchedRandom() *MatchedRandomStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.matchedRandom == nil {
		s.matchedRandom = NewMatchedRandomStore(s.gdb)
	}
	return s.matchedRandom
}

// TelegramConfig gets Telegram bot configuration storage
func (s *Store) TelegramConfig() TelegramConfigStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.telegramConfig == nil {
		s.telegramConfig = NewTelegramConfigStore(s.gdb)
	}
	return s.telegramConfig
}

// Close closes database connection
func (s *Store) Close() error {
	if s.driver != nil {
		return s.driver.Close()
	}
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// GormDB returns the GORM database connection
func (s *Store) GormDB() *gorm.DB {
	return s.gdb
}

// Driver returns database driver for abstraction (legacy)
func (s *Store) Driver() *DBDriver {
	return s.driver
}

// DBType returns current database type
func (s *Store) DBType() DBType {
	if s.driver != nil {
		return s.driver.Type
	}
	// Detect from GORM dialector
	if s.gdb != nil {
		switch s.gdb.Dialector.Name() {
		case "postgres":
			return DBTypePostgres
		default:
			return DBTypeSQLite
		}
	}
	return DBTypeSQLite
}

// q converts query placeholders for current database type (legacy helper)
func (s *Store) q(query string) string {
	return convertQuery(query, s.DBType())
}

// DB gets underlying database connection (for legacy code compatibility)
// Deprecated: use GormDB() instead
func (s *Store) DB() *sql.DB {
	return s.db
}

// GetSystemConfig gets a system configuration value by key
func (s *Store) GetSystemConfig(key string) (string, error) {
	var value string
	result := s.gdb.Raw("SELECT value FROM system_config WHERE key = ?", key).Scan(&value)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return "", nil
		}
		return "", result.Error
	}
	if result.RowsAffected == 0 {
		return "", nil
	}
	return value, nil
}

// SetSystemConfig sets a system configuration value
func (s *Store) SetSystemConfig(key, value string) error {
	// Use GORM-compatible upsert
	return s.gdb.Exec(`
		INSERT INTO system_config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value).Error
}

// Transaction executes transaction with GORM
func (s *Store) Transaction(fn func(tx *gorm.DB) error) error {
	return s.gdb.Transaction(fn)
}

// TransactionSQL executes transaction with sql.Tx (legacy)
// Deprecated: Use Transaction() instead
func (s *Store) TransactionSQL(fn func(tx *sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}
