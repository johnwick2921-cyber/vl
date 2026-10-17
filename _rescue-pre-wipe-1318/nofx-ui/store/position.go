package store

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// adaptivePriceRound rounds a price based on its magnitude to preserve meaningful precision.
// For small prices (like meme coins), it preserves more decimal places.
// It detects the number of decimal places needed from the reference price(s).
func adaptivePriceRound(price float64, referencePrices ...float64) float64 {
	if price == 0 {
		return 0
	}

	// Find the minimum magnitude among all prices (including the price itself)
	minMagnitude := math.Abs(price)
	for _, ref := range referencePrices {
		if ref > 0 && ref < minMagnitude {
			minMagnitude = ref
		}
	}

	// Determine decimal places needed based on price magnitude
	// For price 0.000000541, we need ~15 decimal places
	// For price 0.0001, we need ~8 decimal places
	// For price 1.0, we need ~4 decimal places
	var multiplier float64
	switch {
	case minMagnitude < 0.000001: // Ultra small (meme coins like CHEEMS, SHIB)
		multiplier = 1e15 // 15 decimal places
	case minMagnitude < 0.0001: // Very small (PEPE, FLOKI)
		multiplier = 1e12 // 12 decimal places
	case minMagnitude < 0.01: // Small
		multiplier = 1e10 // 10 decimal places
	case minMagnitude < 1: // Medium
		multiplier = 1e8 // 8 decimal places
	default: // Large
		multiplier = 1e6 // 6 decimal places
	}

	return math.Round(price*multiplier) / multiplier
}

// getPriceDecimalPlaces returns the number of decimal places in a price string
func getPriceDecimalPlaces(price float64) int {
	if price == 0 {
		return 0
	}
	s := strconv.FormatFloat(price, 'f', -1, 64)
	idx := strings.Index(s, ".")
	if idx == -1 {
		return 0
	}
	return len(s) - idx - 1
}

// formatDuration formats a duration
func formatDuration(d time.Duration) string {
	return formatDurationMs(d.Milliseconds())
}

// formatDurationMs formats a duration in milliseconds
func formatDurationMs(ms int64) string {
	seconds := ms / 1000
	minutes := seconds / 60
	hours := minutes / 60
	days := hours / 24

	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	if hours < 24 {
		remainingMins := minutes % 60
		if remainingMins == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh%dm", hours, remainingMins)
	}
	remainingHours := hours % 24
	if remainingHours == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd%dh", days, remainingHours)
}

// CloseReasonReconcileFlat marks an NT8 orphan-close written by the position
// reconcile loop when NT8 reports FLAT for a position whose real exit fill was
// never captured by close-sync. Such a row has NO real exit price and NO known
// realized P&L — reconcile.go records entry-as-exit / 0 only as a placeholder to
// clear the phantom. P&L is genuinely UNKNOWN, so every P&L presenter/aggregator
// treats this marker as "unknown" (excluded from stats; rendered "—" in the UI)
// rather than a false breakeven. realized_pnl is a non-nullable float64, so the
// marker — not a NULL/sentinel value — is the single source of truth for "unknown".
const CloseReasonReconcileFlat = "reconcile_flat"

// CloseReasonUnresolved (class 27, 2026-08-31) marks an NT8 orphan-close where
// NO real exit could be derived — no position_close frame, no netting fill.
// Unlike reconcile_flat's old placeholder (entry-as-exit → fake $0), unresolved
// rows are CLOSED with exit_price 0 and a visible note; every P&L presenter/
// aggregator treats both markers as "unknown" (excluded from stats, rendered
// "—") — a visible gap beats a fabricated zero.
const CloseReasonUnresolved = "unresolved"

// CloseReasonTestSeam marks rows written by the E7 far-side TEST harness
// (e7_farside_test) — experiments run against the live wire, never real trades.
// The E7 closeout promised these are excluded from strategy P&L; the
// UNKNOWN-P&L reason set now enforces it at every aggregator.
const CloseReasonTestSeam = "e7_farside_test"

// UnknownPnLReason reports whether a close reason carries NO trustworthy P&L
// (reconcile_flat placeholder, class-27 unresolved, or an E7 test-seam row)
// and must be excluded from stats/streaks/guardrails while remaining visible
// in history lists.
func UnknownPnLReason(reason string) bool {
	return reason == CloseReasonReconcileFlat || reason == CloseReasonUnresolved || reason == CloseReasonTestSeam
}

// TraderPosition position record
// All time fields use int64 millisecond timestamps (UTC) to avoid timezone issues
type TraderPosition struct {
	ID       int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	TraderID string `gorm:"column:trader_id;not null;index:idx_positions_trader" json:"trader_id"`
	// Account is the NT sub-account this position belongs to (ITEM 2 per-account).
	// Empty for crypto and pre-migration rows; excluded by account-scoped reads.
	Account            string  `gorm:"column:account;not null;default:''" json:"account"`
	ExchangeID         string  `gorm:"column:exchange_id;not null;default:'';index:idx_positions_exchange" json:"exchange_id"`
	ExchangeType       string  `gorm:"column:exchange_type;not null;default:''" json:"exchange_type"`
	ExchangePositionID string  `gorm:"column:exchange_position_id;not null;default:''" json:"exchange_position_id"`
	Symbol             string  `gorm:"column:symbol;not null" json:"symbol"`
	Side               string  `gorm:"column:side;not null" json:"side"`
	EntryQuantity      float64 `gorm:"column:entry_quantity;default:0" json:"entry_quantity"`
	Quantity           float64 `gorm:"column:quantity;not null" json:"quantity"`
	EntryPrice         float64 `gorm:"column:entry_price;not null" json:"entry_price"`
	EntryOrderID       string  `gorm:"column:entry_order_id;default:''" json:"entry_order_id"`
	EntryTime          int64   `gorm:"column:entry_time;not null;index:idx_positions_entry" json:"entry_time"` // Unix milliseconds UTC
	ExitPrice          float64 `gorm:"column:exit_price;default:0" json:"exit_price"`
	ExitOrderID        string  `gorm:"column:exit_order_id;default:''" json:"exit_order_id"`
	ExitTime           int64   `gorm:"column:exit_time;index:idx_positions_exit" json:"exit_time"` // Unix milliseconds UTC, 0 means not set
	RealizedPnL        float64 `gorm:"column:realized_pnl;default:0" json:"realized_pnl"`
	// P0 pnl-record-integrity (2026-08-20): a wrong recorded PnL is corrected
	// by a NEW value + note — the original is NEVER destructively edited
	// (audit trail). Readers use EffectivePnL / COALESCE(pnl_corrected,
	// realized_pnl).
	PnlCorrected      *float64 `gorm:"column:pnl_corrected" json:"pnl_corrected,omitempty"`
	PnlCorrectionNote string   `gorm:"column:pnl_correction_note;default:''" json:"pnl_correction_note,omitempty"`
	Fee               float64  `gorm:"column:fee;default:0" json:"fee"`
	Leverage          int      `gorm:"column:leverage;default:1" json:"leverage"`
	Status            string   `gorm:"column:status;default:OPEN;index:idx_positions_status" json:"status"`
	CloseReason       string   `gorm:"column:close_reason;default:''" json:"close_reason"`
	Source            string   `gorm:"column:source;default:system" json:"source"`
	CreatedAt         int64    `gorm:"column:created_at" json:"created_at"` // Unix milliseconds UTC
	UpdatedAt         int64    `gorm:"column:updated_at" json:"updated_at"` // Unix milliseconds UTC
	// P2.4 — excursion analytics (additive, futures day-plan): max adverse /
	// favorable excursion over the hold (points) + the AI's entry confidence.
	// Zero when not computed (crypto / pre-migration).
	// E4 (wave 1A, 2026-09-02) — NULLABLE. `float64` with DEFAULT 0 could not
	// tell a computed zero from a value nobody ever computed, and 517 of 586
	// closed rows carried the never-computed pair (D15). nil means UNKNOWN.
	MAE             *float64 `gorm:"column:mae;default:0" json:"mae"`
	MFE             *float64 `gorm:"column:mfe;default:0" json:"mfe"`
	EntryConfidence int      `gorm:"column:entry_confidence;default:0" json:"entry_confidence"`
	// P5.5 — plan link (additive, futures day-plan): the cited scenario + plan
	// version stamped at OPEN, and the adherence grade (A–F) computed at CLOSE.
	// Empty/zero for crypto / off-plan trades.
	PlanVersion     int    `gorm:"column:plan_version;default:0" json:"plan_version"`
	CitedScenarioID string `gorm:"column:cited_scenario_id;default:''" json:"cited_scenario_id"`
	PlanMatched     bool   `gorm:"column:plan_matched;default:false" json:"plan_matched"`
	// PlanBand (B3/F6, fail-register wave): structural verdict of the entry vs
	// the cited scenario — "" legacy | "ok" | "off_band" | "struct".
	PlanBand       string `gorm:"column:plan_band;default:''" json:"plan_band,omitempty"`
	AdherenceGrade string `gorm:"column:adherence_grade;default:''" json:"adherence_grade"`
	// S3 (mega-research 2026-08-26) — full plan attribution at entry-write time.
	// plan_id / plan_trade_date / plan_session are stamped from the ACTIVE plan
	// the entry decision cited, NEVER reconstructed later: the version-leak class
	// (register S3: versions are per (date,session) and leak across handoffs,
	// e.g. pos 563 plan_version=9 vs LONDON max v2) is impossible when the link
	// is captured at decision time. Empty for crypto / reconcile rows.
	PlanID        string `gorm:"column:plan_id;default:'';index:idx_positions_plan" json:"plan_id"`
	PlanTradeDate string `gorm:"column:plan_trade_date;default:''" json:"plan_trade_date"`
	PlanSession   string `gorm:"column:plan_session;default:''" json:"plan_session"`
	// PlanLinkNote carries backfill verdicts: "" (live stamp) |
	// "backfill:<reason>" | "unresolvable:<reason>" (never a guessed join).
	PlanLinkNote string `gorm:"column:plan_link_note;default:''" json:"plan_link_note"`
}

// TableName returns the table name
func (TraderPosition) TableName() string {
	return "trader_positions"
}

// PositionStore position storage
type PositionStore struct {
	db *gorm.DB
}

// SetEntryConfidence records the AI's entry confidence on a position (P2.4).
func (s *PositionStore) SetEntryConfidence(id int64, confidence int) error {
	return s.db.Model(&TraderPosition{}).Where("id = ?", id).
		Update("entry_confidence", confidence).Error
}

// UpdateExcursion records the MAE/MFE (points) on a closed position (P2.4).
// A caller with nothing to record must NOT call this with zeros — leave the
// columns NULL instead (E4).
func (s *PositionStore) UpdateExcursion(id int64, mae, mfe float64) error {
	return s.db.Model(&TraderPosition{}).Where("id = ?", id).
		Updates(map[string]any{"mae": mae, "mfe": mfe}).Error
}

// SetPlanLink stamps the cited scenario + plan version + direction-match onto a
// position at OPEN (P5.5). Additive; only called when day_plan is enabled.
func (s *PositionStore) SetPlanLink(id int64, planVersion int, citedScenarioID string, matched bool, band string) error {
	return s.SetPlanLinkFull(id, planVersion, citedScenarioID, matched, band, "", "", "")
}

// SetPlanLinkFull stamps the cited scenario + plan IDENTITY onto a position at
// OPEN (S3, mega-research 2026-08-26). planID/tradeDate/session come from the
// ACTIVE plan the entry cited — never from a later reconstruction.
func (s *PositionStore) SetPlanLinkFull(id int64, planVersion int, citedScenarioID string, matched bool, band, planID, tradeDate, session string) error {
	return s.db.Model(&TraderPosition{}).Where("id = ?", id).
		Updates(map[string]any{
			"plan_version":      planVersion,
			"cited_scenario_id": citedScenarioID,
			"plan_matched":      matched,
			"plan_band":         band, // B3 (F6) — structural verdict, forward-only
			"plan_id":           planID,
			"plan_trade_date":   tradeDate,
			"plan_session":      session,
		}).Error
}

// SetAdherence records the A–F adherence grade on a closed position (P5.5).
func (s *PositionStore) SetAdherence(id int64, grade string) error {
	return s.db.Model(&TraderPosition{}).Where("id = ?", id).
		Update("adherence_grade", grade).Error
}

// SetEntryOrderID stamps the entry order/signal identity onto a position row
// (grand-audit response F1, 2026-08-28) — reconcile-materialized positions must
// carry a usable order identity so move_stop/trailing can address them on the
// wire. Idempotent: never overwrites a non-empty value.
func (s *PositionStore) SetEntryOrderID(id int64, entryOrderID string) error {
	if entryOrderID == "" {
		return nil
	}
	return s.db.Model(&TraderPosition{}).
		Where("id = ? AND (entry_order_id = '' OR entry_order_id IS NULL)", id).
		Update("entry_order_id", entryOrderID).Error
}

// GetUngradedClosedPositions returns a trader's closed positions that have NO
// adherence grade yet and closed at/after sinceMs (W5 — the loop poll grades every
// real exit; the epoch excludes pre-day-plan history). Oldest exit first.
func (s *PositionStore) GetUngradedClosedPositions(traderID string, sinceMs int64, limit int) ([]*TraderPosition, error) {
	if limit <= 0 {
		limit = 20
	}
	var rows []*TraderPosition
	err := s.db.Where("trader_id = ? AND status = ? AND adherence_grade = '' AND exit_time >= ?", traderID, "CLOSED", sinceMs).
		Order("exit_time ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// GetGradedClosedPositions returns a trader's most-recent closed positions that
// carry an adherence grade (the trade-review feed), newest exit first.
func (s *PositionStore) GetGradedClosedPositions(traderID string, limit int) ([]*TraderPosition, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []*TraderPosition
	err := s.db.Where("trader_id = ? AND status = ? AND adherence_grade <> ''", traderID, "CLOSED").
		Order("exit_time DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// NewPositionStore creates position storage instance
func NewPositionStore(db *gorm.DB) *PositionStore {
	return &PositionStore{db: db}
}

// isPostgres checks if the database is PostgreSQL
func (s *PositionStore) isPostgres() bool {
	return s.db.Dialector.Name() == "postgres"
}

// InitTables initializes position tables
func (s *PositionStore) InitTables() error {
	// For PostgreSQL with existing table, skip AutoMigrate
	if s.isPostgres() {
		var tableExists int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'trader_positions'`).Scan(&tableExists)
		if tableExists > 0 {
			// Migrate timestamp columns to bigint (Unix milliseconds UTC)
			// Check if column is still timestamp type before migrating
			timestampColumns := []string{"entry_time", "exit_time", "created_at", "updated_at"}
			for _, col := range timestampColumns {
				var dataType string
				s.db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_name = 'trader_positions' AND column_name = ?`, col).Scan(&dataType)
				if dataType == "timestamp with time zone" || dataType == "timestamp without time zone" {
					// Convert timestamp to Unix milliseconds (bigint)
					s.db.Exec(fmt.Sprintf(`ALTER TABLE trader_positions ALTER COLUMN %s TYPE BIGINT USING EXTRACT(EPOCH FROM %s) * 1000`, col, col))
				}
			}

			// Just ensure index exists
			s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_positions_exchange_pos_unique ON trader_positions(exchange_id, exchange_position_id) WHERE exchange_position_id != ''`)
			return nil
		}
	}

	if err := s.db.AutoMigrate(&TraderPosition{}); err != nil {
		return fmt.Errorf("failed to migrate trader_positions table: %w", err)
	}

	// Create unique partial index for exchange position deduplication
	var indexSQL string
	if s.isPostgres() {
		indexSQL = `CREATE UNIQUE INDEX IF NOT EXISTS idx_positions_exchange_pos_unique ON trader_positions(exchange_id, exchange_position_id) WHERE exchange_position_id != ''`
	} else {
		indexSQL = `CREATE UNIQUE INDEX IF NOT EXISTS idx_positions_exchange_pos_unique ON trader_positions(exchange_id, exchange_position_id) WHERE exchange_position_id != ''`
	}
	if err := s.db.Exec(indexSQL).Error; err != nil {
		if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("failed to create unique index: %w", err)
		}
	}

	// S3 (mega-research 2026-08-26) — position_plan_join: the ONE canonical join
	// used by all analysis/expectancy queries. plan_id-first (stamped at entry),
	// so the legacy (date, session, trader, version) reconstruction is never the
	// primary path. Unresolvable rows keep plan_id='UNRESOLVABLE' and fall into
	// the LEFT-JOIN NULL side — counted, never silently dropped.
	var viewSQL string
	if s.isPostgres() {
		viewSQL = `CREATE OR REPLACE VIEW position_plan_join AS
SELECT p.id AS position_id, p.trader_id, p.symbol, p.plan_id AS pos_plan_id,
       p.plan_trade_date, p.plan_session, p.plan_version, p.cited_scenario_id, p.plan_link_note,
       pl.plan_id AS plans_plan_id, pl.version AS plans_version, pl.doc
FROM trader_positions p
LEFT JOIN plans pl ON pl.plan_id = p.plan_id AND pl.version = p.plan_version`
	} else {
		viewSQL = `CREATE VIEW IF NOT EXISTS position_plan_join AS
SELECT p.id AS position_id, p.trader_id, p.symbol, p.plan_id AS pos_plan_id,
       p.plan_trade_date, p.plan_session, p.plan_version, p.cited_scenario_id, p.plan_link_note,
       pl.plan_id AS plans_plan_id, pl.version AS plans_version, pl.doc
FROM trader_positions p
LEFT JOIN plans pl ON pl.plan_id = p.plan_id AND pl.version = p.plan_version`
	}
	if err := s.db.Exec(viewSQL).Error; err != nil {
		return fmt.Errorf("failed to create position_plan_join view: %w", err)
	}

	return nil
}

// Create creates position record
func (s *PositionStore) Create(pos *TraderPosition) error {
	pos.Status = "OPEN"
	if pos.EntryQuantity == 0 {
		pos.EntryQuantity = pos.Quantity
	}
	// captured BEFORE the insert: GORM writes the DDL default back into the
	// struct, so after Create these pointers are no longer nil
	unmeasured := pos.MAE == nil || pos.MFE == nil
	if err := s.db.Create(pos).Error; err != nil {
		return err
	}
	// E4 (wave 1A, 2026-09-02) — write the excursion columns as NULL.
	//
	// The DDL keeps `mae/mfe REAL DEFAULT 0` on purpose: dropping the default
	// makes GORM rebuild trader_positions, and the position_plan_join VIEW
	// fails mid-rebuild ("no such table: main.trader_positions"), which takes
	// store initialization — and therefore the whole process — down. Since the
	// default stays, GORM omits a nil pointer on INSERT and SQLite fills in 0,
	// which is exactly the ambiguity this wave removes. So the two columns are
	// nulled explicitly, once, right after the insert.
	if unmeasured {
		// raw Exec: a GORM Updates map with nil values is dropped, not emitted
		if err := s.db.Exec(`UPDATE trader_positions SET mae = NULL, mfe = NULL WHERE id = ?`, pos.ID).Error; err != nil {
			return err
		}
		pos.MAE, pos.MFE = nil, nil
	}
	return nil
}

// ClosePosition closes a still-OPEN position. The WHERE clause is guarded on
// status='OPEN' so a stale-snapshot caller (the NT8 reconcile loop — its sole
// caller) can NEVER overwrite a row that close-sync already closed with the real
// ×point-value P&L. This kills the reconcile-overwrites-close-sync race: when
// close-sync wins (it commits first, event-driven), the row is already CLOSED so
// this UPDATE matches 0 rows and the real P&L stands. Returns whether a row was
// actually closed (RowsAffected>0); false means it was already closed (the
// desired no-op — close-sync's real-P&L close was kept).
func (s *PositionStore) ClosePosition(id int64, exitPrice float64, exitOrderID string, realizedPnL float64, fee float64, closeReason string) (bool, error) {
	nowMs := time.Now().UTC().UnixMilli()
	res := s.db.Model(&TraderPosition{}).Where("id = ? AND status = ?", id, "OPEN").Updates(map[string]interface{}{
		"exit_price":    exitPrice,
		"exit_order_id": exitOrderID,
		"exit_time":     nowMs,
		"realized_pnl":  realizedPnL,
		"fee":           fee,
		"status":        "CLOSED",
		"close_reason":  closeReason,
		"updated_at":    nowMs,
	})
	return res.RowsAffected > 0, res.Error
}

// ClosePositionUnresolved (class 27, 2026-08-31) closes an OPEN row whose real
// exit could NOT be derived — no position_close frame and no netting fill.
// It writes close_reason=unresolved with exit_price 0 and a visible note, and
// NEVER fabricates exit=entry (the old fake-$0). Status stays CLOSED so the
// row remains visible in history lists; UnknownPnLReason excludes it from
// every P&L sum/streak/guardrail. Guarded on status='OPEN' like ClosePosition,
// so a last-moment close-sync win is never overwritten.
func (s *PositionStore) ClosePositionUnresolved(id int64, note string) (bool, error) {
	nowMs := time.Now().UTC().UnixMilli()
	res := s.db.Model(&TraderPosition{}).Where("id = ? AND status = ?", id, "OPEN").Updates(map[string]interface{}{
		"exit_price":          0.0,
		"exit_order_id":       "",
		"exit_time":           nowMs,
		"realized_pnl":        0.0,
		"fee":                 0.0,
		"status":              "CLOSED",
		"close_reason":        CloseReasonUnresolved,
		"pnl_correction_note": note,
		"updated_at":          nowMs,
	})
	return res.RowsAffected > 0, res.Error
}

// UpdateEntryPrice overwrites a position's entry price. Used by the NinjaTrader
// reconcile to anchor a stale decision-time entry (the 5m-mark reference) to the
// broker-reported position average (NT8 Position.AveragePrice / AverageFillPrice).
// Does NOT average — a direct replacement to the single source of truth.
func (s *PositionStore) UpdateEntryPrice(id int64, entryPrice float64) error {
	return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
		"entry_price": entryPrice,
		"updated_at":  time.Now().UTC().UnixMilli(),
	}).Error
}

// SetPositionAccount backfills the NT account on an OPEN row whose materializing
// fill frame carried no account (class 27 FIX 3, 2026-08-31). Without it,
// close-sync's account-scoped owner lookup misses the row and the real exit is
// dropped — the 577+578 duplicate class. Only ever fills an EMPTY account.
func (s *PositionStore) SetPositionAccount(id int64, account string) error {
	return s.db.Model(&TraderPosition{}).Where("id = ? AND account = ?", id, "").Updates(map[string]interface{}{
		"account":    account,
		"updated_at": time.Now().UTC().UnixMilli(),
	}).Error
}

// QuantityOf reads one position's quantity by id (invalidation-wired, 2026-09-03).
// The armed-ledger fill stamp needs it from the reconcile path, which knows the
// position id but not its size. Returns 0 when the row is absent — the caller
// then writes nothing, because SetFillQuantity refuses a zero.
func (s *PositionStore) QuantityOf(id int64) float64 {
	if s == nil || s.db == nil || id == 0 {
		return 0
	}
	var pos TraderPosition
	if err := s.db.First(&pos, id).Error; err != nil {
		return 0
	}
	return pos.Quantity
}

// UpdatePositionQuantityAndPrice updates position quantity and recalculates entry price
func (s *PositionStore) UpdatePositionQuantityAndPrice(id int64, addQty float64, addPrice float64, addFee float64) error {
	var pos TraderPosition
	if err := s.db.First(&pos, id).Error; err != nil {
		return fmt.Errorf("failed to get current position: %w", err)
	}

	currentEntryQty := pos.EntryQuantity
	if currentEntryQty == 0 {
		currentEntryQty = pos.Quantity
	}

	newQty := math.Round((pos.Quantity+addQty)*10000) / 10000
	newEntryQty := math.Round((currentEntryQty+addQty)*10000) / 10000
	newEntryPrice := (pos.EntryPrice*pos.Quantity + addPrice*addQty) / newQty
	// Use adaptive precision based on price magnitude (for meme coins with very small prices)
	newEntryPrice = adaptivePriceRound(newEntryPrice, pos.EntryPrice, addPrice)
	newFee := pos.Fee + addFee
	nowMs := time.Now().UTC().UnixMilli()

	return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
		"quantity":       newQty,
		"entry_quantity": newEntryQty,
		"entry_price":    newEntryPrice,
		"fee":            newFee,
		"updated_at":     nowMs,
	}).Error
}

// ReducePositionQuantity reduces position quantity for partial close
// If quantity reaches 0 (or near 0), automatically closes the position
func (s *PositionStore) ReducePositionQuantity(id int64, reduceQty float64, exitPrice float64, addFee float64, addPnL float64) error {
	var pos TraderPosition
	if err := s.db.First(&pos, id).Error; err != nil {
		return fmt.Errorf("failed to get current position: %w", err)
	}

	newQty := math.Round((pos.Quantity-reduceQty)*10000) / 10000
	newFee := pos.Fee + addFee
	newPnL := pos.RealizedPnL + addPnL

	closedQty := pos.EntryQuantity - pos.Quantity
	newClosedQty := closedQty + reduceQty

	var newExitPrice float64
	if newClosedQty > 0 {
		newExitPrice = (pos.ExitPrice*closedQty + exitPrice*reduceQty) / newClosedQty
		// Use adaptive precision based on price magnitude (for meme coins with very small prices)
		newExitPrice = adaptivePriceRound(newExitPrice, pos.ExitPrice, exitPrice, pos.EntryPrice)
	}

	nowMs := time.Now().UTC().UnixMilli()

	// Check if position should be fully closed (quantity reduced to ~0)
	const QUANTITY_TOLERANCE = 0.0001
	if newQty <= QUANTITY_TOLERANCE {
		// Auto-close: set status to CLOSED
		return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
			"quantity":     0,
			"fee":          newFee,
			"exit_price":   newExitPrice,
			"realized_pnl": newPnL,
			"status":       "CLOSED",
			"exit_time":    nowMs,
			"close_reason": "sync",
			"updated_at":   nowMs,
		}).Error
	}

	return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
		"quantity":     newQty,
		"fee":          newFee,
		"exit_price":   newExitPrice,
		"realized_pnl": newPnL,
		"updated_at":   nowMs,
	}).Error
}

// UpdatePositionExchangeInfo updates exchange_id and exchange_type
func (s *PositionStore) UpdatePositionExchangeInfo(id int64, exchangeID, exchangeType string) error {
	nowMs := time.Now().UTC().UnixMilli()
	return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
		"exchange_id":   exchangeID,
		"exchange_type": exchangeType,
		"updated_at":    nowMs,
	}).Error
}

// ClosePositionFully marks position as fully closed
// exitTimeMs is Unix milliseconds UTC
func (s *PositionStore) ClosePositionFully(id int64, exitPrice float64, exitOrderID string, exitTimeMs int64, totalRealizedPnL float64, totalFee float64, closeReason string) error {
	var pos TraderPosition
	if err := s.db.First(&pos, id).Error; err != nil {
		return fmt.Errorf("failed to get position: %w", err)
	}

	quantity := pos.Quantity
	if pos.EntryQuantity > 0 {
		quantity = pos.EntryQuantity
	}

	return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
		"quantity":      quantity,
		"exit_price":    exitPrice,
		"exit_order_id": exitOrderID,
		"exit_time":     exitTimeMs,
		"realized_pnl":  totalRealizedPnL,
		"fee":           totalFee,
		"status":        "CLOSED",
		"close_reason":  closeReason,
		"updated_at":    time.Now().UTC().UnixMilli(),
	}).Error
}

// DeleteAllOpenPositions deletes all OPEN positions for a trader
func (s *PositionStore) DeleteAllOpenPositions(traderID string) error {
	return s.db.Where("trader_id = ? AND status = ?", traderID, "OPEN").Delete(&TraderPosition{}).Error
}

// GetOpenPositions gets all open positions
func (s *PositionStore) GetOpenPositions(traderID string) ([]*TraderPosition, error) {
	var positions []*TraderPosition
	err := s.db.Where("trader_id = ? AND status = ?", traderID, "OPEN").
		Order("entry_time DESC").
		Find(&positions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query open positions: %w", err)
	}

	// Fix EntryQuantity if it's 0
	for _, pos := range positions {
		if pos.EntryQuantity == 0 {
			pos.EntryQuantity = pos.Quantity
		}
	}
	return positions, nil
}

// ListUnlinked returns recent positions with NO plan linkage (plan_version = 0)
// — the F3 (LONDON-FORENSICS 2026-08-28) lineage-repair scan: a fill stamped
// before the position row existed left plan_version 0.
func (s *PositionStore) ListUnlinked(traderID string, limit int) ([]*TraderPosition, error) {
	if limit <= 0 {
		limit = 200
	}
	var out []*TraderPosition
	err := s.db.Where("trader_id = ? AND plan_version = 0", traderID).
		Order("entry_time DESC").Limit(limit).Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list unlinked positions: %w", err)
	}
	return out, nil
}

// ListClosedByEntryOrderIDs returns CLOSED positions whose entry_order_id is in
// ids — E4 (entry-mechanics 2026-08-30): the split-sibling law looks up the
// filled leg's position by its wire signal id to learn whether it STOPPED OUT.
func (s *PositionStore) ListClosedByEntryOrderIDs(traderID string, ids []string) ([]*TraderPosition, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []*TraderPosition
	err := s.db.Where("trader_id = ? AND status = ? AND entry_order_id IN ?", traderID, "CLOSED", ids).
		Order("exit_time DESC").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list closed positions by entry order ids: %w", err)
	}
	return out, nil
}

// GetOpenPositionBySymbol gets open position for specified symbol and direction
func (s *PositionStore) GetOpenPositionBySymbol(traderID, symbol, side string) (*TraderPosition, error) {
	var pos TraderPosition
	// SIDE CASING (2026-09-03) — UPPER() on both sides. `side = ?` is
	// case-sensitive on a plain TEXT column, and the two writers disagree:
	// armed_orders.side is always lowercase (built with strings.ToLower), while
	// trader_positions.side is overwhelmingly uppercase (live: LONG 280 /
	// SHORT 304 against long 1 / short 2). So the armed fill handler, which
	// passes the ARMED row's lowercase side, could NEVER find an armed-entry
	// position however well the timing went — measured on the live store,
	// side='short' matched 0 rows for position 591 while side='SHORT' matched 1.
	// The materialization race merely got there first.
	err := s.db.Where("trader_id = ? AND symbol = ? AND UPPER(side) = UPPER(?) AND status = ?", traderID, symbol, side, "OPEN").
		Order("entry_time DESC").
		First(&pos).Error

	if err == nil {
		if pos.EntryQuantity == 0 {
			pos.EntryQuantity = pos.Quantity
		}
		return &pos, nil
	}

	if err == gorm.ErrRecordNotFound {
		// Try without USDT suffix for backward compatibility
		if strings.HasSuffix(symbol, "USDT") {
			baseSymbol := strings.TrimSuffix(symbol, "USDT")
			err = s.db.Where("trader_id = ? AND symbol = ? AND UPPER(side) = UPPER(?) AND status = ?", traderID, baseSymbol, side, "OPEN").
				Order("entry_time DESC").
				First(&pos).Error
			if err == nil {
				if pos.EntryQuantity == 0 {
					pos.EntryQuantity = pos.Quantity
				}
				return &pos, nil
			}
		}
		return nil, nil
	}
	return nil, err
}

// GetOpenPositionByAccountSymbol finds the OPEN position matching (account, symbol,
// side) across ALL traders — the trader that actually OWNS the row. A position_close
// frame routes by SYMBOL to ONE trader's close-sync (dispatchBySymbol), which may NOT
// be the owner when multiple traders share a symbol; recording against the receiver's
// trader_id then misses and the priced close is lost. This lets close-sync record
// against the owning trader instead. account "" widens to (symbol, side). nil,nil = none.
func (s *PositionStore) GetOpenPositionByAccountSymbol(account, symbol, side string) (*TraderPosition, error) {
	find := func(sym string) (*TraderPosition, error) {
		var pos TraderPosition
		// Same casing rule as GetOpenPositionBySymbol — close-sync routes by
		// symbol and side, and a case-sensitive compare loses a priced close.
		q := s.db.Where("symbol = ? AND UPPER(side) = UPPER(?) AND status = ?", sym, side, "OPEN")
		if account != "" {
			q = q.Where("account = ?", account)
		}
		err := q.Order("entry_time DESC").First(&pos).Error
		if err == nil {
			if pos.EntryQuantity == 0 {
				pos.EntryQuantity = pos.Quantity
			}
			return &pos, nil
		}
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	pos, err := find(symbol)
	if err != nil || pos != nil {
		return pos, err
	}
	// Backward-compat: retry without the USDT suffix (mirrors GetOpenPositionBySymbol).
	if strings.HasSuffix(symbol, "USDT") {
		return find(strings.TrimSuffix(symbol, "USDT"))
	}
	return nil, nil
}

// GetClosedPositions gets closed positions (optionally scoped to one account).
// account=="" → trader-global (crypto + legacy); account!="" → only that NT
// account's positions, excluding pre-migration rows (account=”).
func (s *PositionStore) GetClosedPositions(traderID string, limit int, account ...string) ([]*TraderPosition, error) {
	var positions []*TraderPosition
	q := s.db.Where("trader_id = ? AND status = ?", traderID, "CLOSED")
	if len(account) > 0 && account[0] != "" {
		q = q.Where("account = ?", account[0])
	}
	err := q.Order("exit_time DESC").
		Limit(limit).
		Find(&positions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query closed positions: %w", err)
	}

	for _, pos := range positions {
		if pos.EntryQuantity == 0 {
			pos.EntryQuantity = pos.Quantity
		}
	}
	return positions, nil
}

// GetAllOpenPositions gets all traders' open positions
func (s *PositionStore) GetAllOpenPositions() ([]*TraderPosition, error) {
	var positions []*TraderPosition
	err := s.db.Where("status = ?", "OPEN").
		Order("trader_id, entry_time DESC").
		Find(&positions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query all open positions: %w", err)
	}

	for _, pos := range positions {
		if pos.EntryQuantity == 0 {
			pos.EntryQuantity = pos.Quantity
		}
	}
	return positions, nil
}

// ExistsWithExchangePositionID checks if a position exists
func (s *PositionStore) ExistsWithExchangePositionID(exchangeID, exchangePositionID string) (bool, error) {
	if exchangePositionID == "" {
		return false, nil
	}

	var count int64
	err := s.db.Model(&TraderPosition{}).
		Where("exchange_id = ? AND exchange_position_id = ?", exchangeID, exchangePositionID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check position existence: %w", err)
	}
	return count > 0, nil
}

// GetOpenPositionByExchangePositionID gets an OPEN position by exchange_position_id
func (s *PositionStore) GetOpenPositionByExchangePositionID(exchangeID, exchangePositionID string) (*TraderPosition, error) {
	if exchangePositionID == "" {
		return nil, nil
	}

	var pos TraderPosition
	err := s.db.Where("exchange_id = ? AND exchange_position_id = ? AND status = ?", exchangeID, exchangePositionID, "OPEN").
		First(&pos).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	if pos.EntryQuantity == 0 {
		pos.EntryQuantity = pos.Quantity
	}
	return &pos, nil
}

// CreateOpenPosition creates an open position
func (s *PositionStore) CreateOpenPosition(pos *TraderPosition) error {
	if pos.ExchangePositionID != "" && pos.ExchangeID != "" {
		existingPos, err := s.GetOpenPositionByExchangePositionID(pos.ExchangeID, pos.ExchangePositionID)
		if err != nil {
			return err
		}
		if existingPos != nil {
			return s.UpdatePositionQuantityAndPrice(existingPos.ID, pos.Quantity, pos.EntryPrice, pos.Fee)
		}
		exists, err := s.ExistsWithExchangePositionID(pos.ExchangeID, pos.ExchangePositionID)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
	}

	if pos.Status == "" {
		pos.Status = "OPEN"
	}
	if pos.Source == "" {
		pos.Source = "system"
	}
	if pos.EntryQuantity == 0 {
		pos.EntryQuantity = pos.Quantity
	}

	// E4 — captured before the insert; see Create for why the DDL default stays
	unmeasured := pos.MAE == nil || pos.MFE == nil
	err := s.db.Create(pos).Error
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			existingPos, findErr := s.GetOpenPositionByExchangePositionID(pos.ExchangeID, pos.ExchangePositionID)
			if findErr != nil {
				return findErr
			}
			if existingPos != nil {
				return s.UpdatePositionQuantityAndPrice(existingPos.ID, pos.Quantity, pos.EntryPrice, pos.Fee)
			}
			return nil
		}
		return fmt.Errorf("failed to create open position: %w", err)
	}
	if unmeasured { // E4 — NULL, not the DDL's 0
		if err := s.db.Exec(`UPDATE trader_positions SET mae = NULL, mfe = NULL WHERE id = ?`, pos.ID).Error; err != nil {
			return err
		}
		pos.MAE, pos.MFE = nil, nil
	}

	return nil
}

// ClosePositionWithAccurateData closes a position with accurate data from exchange
// exitTimeMs is Unix milliseconds UTC
func (s *PositionStore) ClosePositionWithAccurateData(id int64, exitPrice float64, exitOrderID string, exitTimeMs int64, realizedPnL float64, fee float64, closeReason string) error {
	return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
		"exit_price":    exitPrice,
		"exit_order_id": exitOrderID,
		"exit_time":     exitTimeMs,
		"realized_pnl":  realizedPnL,
		"fee":           fee,
		"status":        "CLOSED",
		"close_reason":  closeReason,
		"updated_at":    time.Now().UTC().UnixMilli(),
	}).Error
}

// EffectivePnL returns the corrected realized P&L when a correction exists,
// else the original (P0 pnl-record-integrity, 2026-08-20).
//
// P&L-TRUTH WAVE (2026-09-01): this accessor COERCES — a NULL pnl_corrected
// silently becomes raw realized_pnl, which is how 115 unresolved rows put
// "Total PnL -203.68 (220 trades)" into the executor prompt when the strict
// truth was +304.32 over 105 resolved. It is BANNED from every aggregator
// (store/pnl_surface_guard_test.go) and kept only for per-row display of a
// row the caller already knows is resolved. Aggregate with CorrectedPnL.
func (p *TraderPosition) EffectivePnL() float64 {
	if p.PnlCorrected != nil {
		return *p.PnlCorrected
	}
	return p.RealizedPnL
}

// CorrectedPnL is the STRICT accessor (corrected-column law, A22): the
// corrected P&L and true, or (0, false) when the row is UNRESOLVED
// (pnl_corrected NULL). An unresolved row has no captured/verified exit and
// must be COUNTED and EXCLUDED — never coerced to realized_pnl, never a $0.
func (p *TraderPosition) CorrectedPnL() (float64, bool) {
	if p.PnlCorrected == nil {
		return 0, false
	}
	return *p.PnlCorrected, true
}

// IsUnresolved reports whether the row carries no verified P&L.
func (p *TraderPosition) IsUnresolved() bool { return p.PnlCorrected == nil }
