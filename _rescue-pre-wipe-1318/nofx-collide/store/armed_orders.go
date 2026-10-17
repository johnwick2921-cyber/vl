package store

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"strings"

	"nofx/logger"
)

// ARMED ORDERS (Wave 2, 2026-08-27) — the durable ledger of scenario-arm
// authorizations the AI granted. The LLM stays the authorizer; Go manages
// WHEN a working order exists (placement/cancel/fill lineage). Every state
// transition is a row update with a reason — nothing armed is ever dropped.

// ArmedOrderDB is one armed scenario (one row per scenario-arm; upserted on
// plan version change, re-armed only by a NEW authorization).
type ArmedOrderDB struct {
	ID int64 `gorm:"primaryKey;autoIncrement"`

	TraderID string `gorm:"index"`
	PlanID   string `gorm:"index"`
	// Version is the LAST plan version that touched this row, not the one it was
	// armed under: UpsertArm overwrites it on every re-authorization. Documented
	// 2026-09-02 after the cadence audit trusted it as "armed under" and could
	// not defend the reading. Use ArmedUnderVersion for attribution.
	Version int
	// ArmedUnderVersion is set ONCE, when the arm is first authorized, and is
	// never overwritten. This is the version the arm actually belongs to.
	ArmedUnderVersion int `gorm:"index"`
	Session           string
	Scenario          string // S1, S2, …

	Side     string  // long | short
	EntryPx  float64 // resting limit price
	StopPx   float64 // bracket stop
	TargetPx float64 // bracket target

	// State: armed (authorized) | place_pending (registered, awaiting receipt) |
	// working (received live entry) | filled | rejected | cancelled | expired.
	State        string `gorm:"index"`
	StateReason  string
	EntryClass   string // armed_fill when filled (fills bypass stale_reeval)
	SignalID     string // the wire signal_id registered before sending
	FillPrice    float64
	FillQuantity int

	// E4 (entry-mechanics 2026-08-30) — split-entry legs: a two-leg arm writes
	// TWO rows sharing (plan_id, scenario) distinguished by LegIndex. LegCount
	// = the pair size (2 for split arms, 0 for legacy single arms). Kind =
	// "limit" (default) | "stop_entry" (E7).
	// BootID (CLASS 33, 2026-09-02) — the process that AUTHORED this row
	// (store.ProcessBootID). A non-terminal row whose BootID differs from the
	// running process was placed by a DEAD process: its broker order has no
	// listener, so the boot sweep cancels it before anything is re-armed.
	// Never refreshed on a same-identity re-arm — the stamp must survive an
	// upsert or the sweep would lose its evidence.
	BootID string `gorm:"index"`

	LegIndex int    `gorm:"default:0"`
	LegCount int    `gorm:"default:0"`
	Kind     string `gorm:"default:''"`
	// Condition (arms-follow-bias 2026-09-04) — the scenario condition this arm
	// was authored from. The executor needs it to tell a PRIMARY stop-entry
	// (reclaim) from the E7 no-retest FALLBACK, which share a Kind. Legacy rows
	// carry '' = UNKNOWN, which is never treated as a condition.
	Condition string `gorm:"default:''"`
	// PlacementSeq (D5, arms-follow-bias 2026-09-04) — every BROKER PLACEMENT is
	// one row forever. A terminal row that reached the broker is never revived
	// in place; the next authorization lands as seq+1 and the old row keeps its
	// prices, its signal id and its ending. Rows that never reached the broker
	// still revive in place (PRE-REOPEN F3), because there is nothing to keep.
	PlacementSeq int `gorm:"default:0"`

	// ── CANCEL LIFECYCLE (cancel-confirmation, 2026-09-06) ──────────────────
	//
	// A cancel used to be a RETURN VALUE: nt.CancelOrder put a frame on a
	// socket, returned nil, and the row was written 'cancelled' on the
	// strength of it. nt8_order_snapshots id 1664 is what that costs — nine
	// working orders at the broker against nine rows reading 'cancelled', all
	// one arm slot. A cancel is now a LIFECYCLE settled by the broker's book.
	//
	// 0 on every one of these means "no cancel has been requested for this
	// row", which is the truth for every historical row. It is not a computed
	// zero standing in for a measurement.
	CancelRequestedAtMs int64 `gorm:"default:0"`
	// CancelAttempts counts REQUESTS SENT, not confirmations. A re-request
	// bumps it; the confirmation does not.
	CancelAttempts int `gorm:"default:0"`
	// CancelSettledSnapshotID is the nt8_order_snapshots id whose book no
	// longer listed the order — the evidence the cancel actually happened.
	// 0 on a row that reached 'cancelled' any other way, which is every row
	// written before this wave.
	CancelSettledSnapshotID int64 `gorm:"default:0"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Arm ledger states. cancel_pending is NON-TERMINAL by design: while a cancel
// is in flight the order may still be resting at the broker, so the slot is not
// free and nothing may replace it.
const (
	StateArmed         = "armed"
	StateWorking       = "working"
	StatePlacePending  = "place_pending"
	StateRejected      = "rejected"
	StateCancelPending = "cancel_pending"
	StateCancelled     = "cancelled"
	// StateFilled — the entry became a position. A filled arm is NEVER
	// cancelled: the only orders left under its signal are its protections
	// (2026-09-06 23:37:02, position 592).
	StateFilled = "filled"
)

// TableName is the armed_orders table (spec name).
func (ArmedOrderDB) TableName() string { return "armed_orders" }

const armedOrdersDDL = `
CREATE TABLE IF NOT EXISTS armed_orders (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	trader_id     TEXT    NOT NULL DEFAULT '',
	plan_id       TEXT    NOT NULL DEFAULT '',
	version       INTEGER NOT NULL DEFAULT 0,
	session       TEXT    NOT NULL DEFAULT '',
	scenario      TEXT    NOT NULL DEFAULT '',
	side          TEXT    NOT NULL DEFAULT '',
	entry_px      REAL    NOT NULL DEFAULT 0,
	stop_px       REAL    NOT NULL DEFAULT 0,
	target_px     REAL    NOT NULL DEFAULT 0,
	state         TEXT    NOT NULL DEFAULT 'armed',
	state_reason  TEXT    NOT NULL DEFAULT '',
	entry_class   TEXT    NOT NULL DEFAULT '',
	signal_id     TEXT    NOT NULL DEFAULT '',
	fill_price    REAL    NOT NULL DEFAULT 0,
	fill_quantity INTEGER NOT NULL DEFAULT 0,
	leg_index     INTEGER NOT NULL DEFAULT 0,
	leg_count     INTEGER NOT NULL DEFAULT 0,
	kind          TEXT    NOT NULL DEFAULT '',
	boot_id       TEXT    NOT NULL DEFAULT '',
	created_at    DATETIME,
	updated_at    DATETIME
)`

// ArmedOrderStore persists the armed ledger.
type ArmedOrderStore struct {
	db *gorm.DB
}

// NewArmedOrderStore constructs the sub-store.
func NewArmedOrderStore(db *gorm.DB) *ArmedOrderStore { return &ArmedOrderStore{db: db} }

// Migrate creates the table (sqlite: exact DDL; else AutoMigrate). E4 adds
// the leg/kind columns idempotently for EXISTING databases (ALTER TABLE ADD
// COLUMN is a no-op-safe guarded by pragma table_info).
func (s *ArmedOrderStore) Migrate() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store required")
	}
	if s.db.Dialector.Name() == "sqlite" {
		if err := s.db.Exec(armedOrdersDDL).Error; err != nil {
			return err
		}
		for _, col := range []struct{ name, decl string }{
			{"leg_index", "INTEGER NOT NULL DEFAULT 0"},
			{"leg_count", "INTEGER NOT NULL DEFAULT 0"},
			{"kind", "TEXT NOT NULL DEFAULT ''"},
			{"boot_id", "TEXT NOT NULL DEFAULT ''"},         // class 33 — pre-boot decidability
			{"condition", "TEXT NOT NULL DEFAULT ''"},       // arms-follow-bias 2026-09-04
			{"placement_seq", "INTEGER NOT NULL DEFAULT 0"}, // D5 — append-only placements
			// ATTRIBUTION (2026-09-02): the version the arm was FIRST authorized
			// under. 0 on legacy rows; UpsertArm adopts their current version
			// once, so the table self-heals without a guessing migration.
			{"armed_under_version", "INTEGER NOT NULL DEFAULT 0"},
			// CANCEL LIFECYCLE (cancel-confirmation 2026-09-06). 0 means "no
			// cancel has been requested for this row", which is the truth for
			// every historical row — not an uncomputed value dressed as data.
			{"cancel_requested_at_ms", "INTEGER NOT NULL DEFAULT 0"},
			{"cancel_attempts", "INTEGER NOT NULL DEFAULT 0"},
			{"cancel_settled_snapshot_id", "INTEGER NOT NULL DEFAULT 0"},
		} {
			var n int64
			if err := s.db.Raw("SELECT COUNT(*) FROM pragma_table_info('armed_orders') WHERE name = ?", col.name).Scan(&n).Error; err != nil {
				return err
			}
			if n == 0 {
				if err := s.db.Exec(fmt.Sprintf("ALTER TABLE armed_orders ADD COLUMN %s %s", col.name, col.decl)).Error; err != nil {
					return err
				}
			}
		}
		// E4 (entry-mechanics 2026-08-30): the split entry writes TWO rows per
		// (plan_id, scenario) distinguished by leg_index — the legacy 2-column
		// unique index would reject the second leg. Replace it with the
		// 3-column form (idempotent: DROP IF EXISTS + CREATE IF NOT EXISTS).
		if err := s.db.Exec("DROP INDEX IF EXISTS idx_armed_orders_plan_scenario").Error; err != nil {
			return err
		}
		// D5 (2026-09-04): the placement sequence joins the key so a cancelled
		// placement and its replacement can coexist. Without it the replacement
		// had to overwrite the row the broker had already acted on.
		return s.db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_armed_orders_plan_scenario_seq ON armed_orders(plan_id, scenario, leg_index, placement_seq)").Error
	}
	return s.db.AutoMigrate(&ArmedOrderDB{})
}

// UpsertArm writes/refreshes the arm row for (plan_id, scenario, leg_index).
// Same key = same row (state reset to armed only when the spec CHANGED
// materially — entry/stop/target diff >= 2 ticks — the caller decides and
// passes reset). E4: leg rows of a split arm share (plan_id, scenario) and
// differ by LegIndex.
func (s *ArmedOrderStore) UpsertArm(row *ArmedOrderDB) error {
	if row == nil || row.PlanID == "" || row.Scenario == "" {
		return fmt.Errorf("plan_id and scenario required")
	}
	// CANONICAL CASING AT THE WRITE (class 28, owner ruling 2026-09-03). This
	// table stored lowercase (long 19 / short 17) while trader_positions stores
	// uppercase (LONG 280 / SHORT 304), and the fill handler's side-keyed
	// lookup compared them literally — so it could never match. UPPER() on the
	// read makes existing rows work; canonicalizing HERE, where the value
	// enters, is what stops the two tables disagreeing at all.
	row.Side = strings.ToUpper(strings.TrimSpace(row.Side))
	// PRE-REOPEN F3 (2026-08-28) — dead re-arm fix: a TERMINAL row for the same
	// (plan, scenario) is re-authorized as a fresh armed row (new identity, no
	// stale fill); a non-terminal row keeps its identity and only its prices
	// are refreshed. The old Assign-based upsert left terminal rows terminal
	// forever, so a legit same-scenario re-arm was impossible and the executor
	// re-logged the dead row every cycle.
	var existing ArmedOrderDB
	// A LIVE row wins the lookup, then the newest placement.
	//
	// This was First() with no ORDER BY, so SQLite returned the lowest rowid —
	// which, once a first placement had been cancelled, was always the TERMINAL
	// row. The "terminal row that reached the broker keeps its record" branch
	// below then fired on every re-authorization and minted a new placement each
	// cycle: one NY scenario reached 24 rows in 100 minutes and pushed the
	// cutover gate's leg 4 to "broker 1 vs ledger 23 — MISMATCH". The
	// record-keeping law was right; the row it was applied to was wrong.
	err := s.db.Where("plan_id = ? AND scenario = ? AND leg_index = ?", row.PlanID, row.Scenario, row.LegIndex).
		Order("CASE WHEN " + NonTerminalArmStateSQL() + " THEN 0 ELSE 1 END, placement_seq DESC, id DESC").First(&existing).Error
	if err == nil {
		// D5 — a WORKING row is a LIVE BROKER ORDER. Rewriting its prices in
		// place overwrote the slot and lost the brackets (rows 582, 585): the
		// ledger and the broker then held two different orders under one id.
		// Replacing a live order requires a cancel, and the store cannot issue
		// one, so it declines rather than diverge.
		if !IsTerminalArmState(existing.State) && existing.State != StateArmed && existing.State != StateCancelPending {
			return fmt.Errorf("armed_orders: refusing to rewrite %s/%s — the row is %s (signal %q); replace requires cancel first",
				row.PlanID, row.Scenario, existing.State, existing.SignalID)
		}
		// CANCEL-CONFIRMATION (2026-09-06) — A CANCEL IN FLIGHT IS STILL A LIVE
		// BROKER ORDER. Until a fresh snapshot says the order is gone, nobody
		// can prove it is, so this row may neither be rewritten in place nor
		// used as the predecessor of a new placement.
		//
		// Without this branch the row falls through to the mint below (it is
		// not 'armed' and it has a signal id), which would create a fresh
		// 'armed' row with the signal id cleared — and the executor would place
		// a SECOND order while the first may still be resting. That is the
		// stacking of 2026-09-04 arriving by a new road.
		if existing.State == StateCancelPending {
			return fmt.Errorf("armed_orders: refusing to rewrite %s/%s — a cancel is in flight for signal %q and is not yet confirmed by the broker's book; the slot is not free",
				row.PlanID, row.Scenario, existing.SignalID)
		}
		// A TERMINAL row that reached the broker keeps its record forever; the
		// new authorization becomes the NEXT placement rather than erasing it.
		// A row that never reached the broker has nothing to keep and still
		// revives in place (PRE-REOPEN F3).
		if existing.State != StateArmed && strings.TrimSpace(existing.SignalID) != "" {
			var maxSeq int
			s.db.Model(&ArmedOrderDB{}).
				Where("plan_id = ? AND scenario = ? AND leg_index = ?", row.PlanID, row.Scenario, row.LegIndex).
				Select("COALESCE(MAX(placement_seq), 0)").Scan(&maxSeq)
			row.ID = 0
			row.PlacementSeq = maxSeq + 1
			// A fresh authorization is ARMED and carries no lineage from the
			// placement it follows: a new row must never inherit the old row's
			// signal id or fill, or the two placements become indistinguishable.
			row.State = "armed"
			row.StateReason = ""
			row.SignalID = ""
			row.FillPrice = 0
			row.FillQuantity = 0
			return s.db.Create(row).Error
		}
		if existing.State == "armed" {
			row.ID = existing.ID
			// ATTRIBUTION (2026-09-02): armed_under_version is NOT in this map —
			// it belongs to the first authorization and must survive every
			// re-authorization. "version" continues to mean last-touch.
			row.ArmedUnderVersion = existing.ArmedUnderVersion
			if row.ArmedUnderVersion == 0 {
				// A row armed before the column existed: adopt its current
				// version once, so the backfill is self-healing rather than a
				// migration that has to guess.
				row.ArmedUnderVersion = existing.Version
				if err := s.db.Model(&existing).Update("armed_under_version", row.ArmedUnderVersion).Error; err != nil {
					return err
				}
			}
			return s.db.Model(&existing).Updates(map[string]any{
				"version": row.Version, "session": row.Session,
				"side": row.Side, "entry_px": row.EntryPx, "stop_px": row.StopPx,
				"target_px": row.TargetPx, "updated_at": row.UpdatedAt,
				"leg_count": row.LegCount, "kind": row.Kind,
			}).Error
		}
		// MANUAL-CANCEL-WINS (2026-08-30 E7 incident): a TERMINAL row is
		// re-authorized ONLY on a plan VERSION change. The old
		// re-authorize-every-cycle behavior was the re-place loop:
		// terminal → armed → marketable fill → stop-out → terminal → armed…
		// forever while the confirm stayed MET, so an owner/NT8 cancel
		// never won. Same version + terminal = the row STAYS terminal.
		// 0B (owner ruling 2026-09-02) — RE-ARM AFTER BOOT SWEEP. The
		// manual-cancel-wins law exists so the OWNER's cancels stick. A boot
		// sweep is the machine's own housekeeping: it cancels pre-boot orders
		// because the process that owned them died, not because anyone judged
		// the setup dead. Leaving those rows sticky killed the live setup
		// until the next plan version — on 09-02 00:16 that rule would have
		// meant no position 587. Swept rows (state_reason prefixed
		// "boot_sweep") re-authorize under the SAME version; every other
		// terminal row stays terminal.
		if existing.Version == row.Version && !IsBootSweepReason(existing.StateReason) {
			return nil
		}
		// 0B — the re-arm is LOUD: a swept row coming back under the SAME version
		// is the machine undoing its own boot housekeeping, and the journal must
		// say so with the dead broker identity it replaces.
		if existing.Version == row.Version && IsBootSweepReason(existing.StateReason) {
			logger.Warnf("⚖ re-armed after boot sweep: %s %s leg %d — signal %s → (fresh arm, awaiting placement) · same plan version v%d",
				row.Session, row.Scenario, row.LegIndex+1, signalOrNone(existing.SignalID), row.Version)
		}
		// New plan version → RE-AUTHORIZE: fresh armed state, fresh lineage.
		row.ID = existing.ID
		return s.db.Model(&existing).Updates(map[string]any{
			"state": "armed", "state_reason": "", "signal_id": "",
			"entry_class": "", "fill_price": 0, "fill_quantity": 0,
			"trader_id": row.TraderID, "version": row.Version, "session": row.Session,
			// ATTRIBUTION: a terminal row re-authorized under a NEW plan version
			// is a NEW arm reusing the row id — its first authorization is now.
			"armed_under_version": row.Version,
			"side":                row.Side, "entry_px": row.EntryPx, "stop_px": row.StopPx,
			"target_px": row.TargetPx, "leg_count": row.LegCount, "kind": row.Kind,
			"created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
			// class 33: a re-authorized row belongs to THIS process.
			"boot_id": ProcessBootID(),
		}).Error
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	// class 33 — a freshly created row belongs to THIS process, so the boot
	// sweep never mistakes it for an orphan of a dead one.
	if row.BootID == "" {
		row.BootID = ProcessBootID()
	}
	// ATTRIBUTION: first authorization stamps the version the arm belongs to.
	if row.ArmedUnderVersion == 0 {
		row.ArmedUnderVersion = row.Version
	}
	return s.db.Create(row).Error
}

// ListNonTerminal returns ONE TRADER's armed orders that are NOT in a
// terminal state. PRE-SUNDAY F4 (2026-08-28): the old unscoped scan crossed
// trader boundaries the moment more than one trader runs.
func (s *ArmedOrderStore) ListNonTerminal(traderID string) ([]ArmedOrderDB, error) {
	var out []ArmedOrderDB
	// cancel_pending is NON-TERMINAL (cancel-confirmation 2026-09-06): a cancel
	// that has been REQUESTED but not CONFIRMED still has an order at the
	// broker as far as anyone can prove, so it holds its slot, it is swept at
	// boot like any live row, and cutover leg 4 counts it — which is what makes
	// leg 4 agree with the broker instead of with our intentions.
	err := s.db.Where("trader_id = ?", traderID).Where(NonTerminalArmStateSQL()).
		Order("id").Find(&out).Error
	return out, err
}

// SetState transitions one row's state with a reason (the ledger rule: a
// terminal state change is never silent).
func (s *ArmedOrderStore) SetState(id int64, state, reason string) error {
	return s.db.Model(&ArmedOrderDB{}).Where("id = ?", id).
		Updates(map[string]any{"state": state, "state_reason": reason}).Error
}

// BeginPlacement persists identity BEFORE the socket write. A received reply can
// then find the row even when it beats SendSignal's return. A second placement
// cannot reuse an in-flight row, and no post-send write can erase a rejection.
func (s *ArmedOrderStore) BeginPlacement(id int64, signalID string) error {
	if strings.TrimSpace(signalID) == "" {
		return fmt.Errorf("armed_orders: placement requires signal id")
	}
	r := s.db.Model(&ArmedOrderDB{}).Where("id = ? AND state = ? AND (signal_id = '' OR signal_id IS NULL)", id, StateArmed).
		Updates(map[string]any{"signal_id": signalID, "state": StatePlacePending, "state_reason": ""})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return fmt.Errorf("armed_orders: row %d is no longer eligible for placement", id)
	}
	return nil
}

const PlacementReasonUnavailable = "reason unavailable (NT8 frame omitted reason)"

// ApplyPlacementReceipt is called only for received entry frames. Cancellation
// intent survives a late acceptance; filled/terminal rows cannot be resurrected.
func (s *ArmedOrderStore) ApplyPlacementReceipt(traderID, signalID, state, reason string) error {
	if traderID == "" || signalID == "" {
		return nil
	}
	q := s.db.Model(&ArmedOrderDB{}).Where("trader_id = ? AND signal_id = ?", traderID, signalID)
	switch state {
	case StateWorking:
		q = q.Where("state = ?", StatePlacePending)
		if reason == "" {
			reason = "received entry order_update"
		}
		reason = "confirmed by " + reason
	case StateRejected:
		if strings.TrimSpace(reason) == "" {
			q = q.Where(NonTerminalArmStateSQL()).Where("state <> ?", StateArmed)
			reason = PlacementReasonUnavailable
		} else {
			// A second receipt may supply the reason h1's first frame omitted.
			// Enrich that absence without replacing an already received reason.
			q = q.Where("(("+NonTerminalArmStateSQL()+" AND state <> ?) OR (state = ? AND state_reason = ?))", StateArmed, StateRejected, PlacementReasonUnavailable)
		}
	default:
		return fmt.Errorf("armed_orders: unsupported placement receipt %q", state)
	}
	return q.Updates(map[string]any{"state": state, "state_reason": reason}).Error
}

// RequestCancel moves a row to cancel_pending and records that a cancel was
// SENT. It never writes 'cancelled': that word now means the broker's book
// stopped listing the order, and only ConfirmCancel may say it.
//
// Idempotent on the timestamp — a re-request bumps the attempt count and leaves
// the original request time, so the age in the timeout WARN is the age of the
// FIRST attempt, which is the number that matters.
func (s *ArmedOrderStore) RequestCancel(id int64, reason string, nowMs int64) error {
	if s == nil || s.db == nil {
		return nil
	}
	var row ArmedOrderDB
	if err := s.db.First(&row, id).Error; err != nil {
		return err
	}
	upd := map[string]any{
		"state":           StateCancelPending,
		"state_reason":    reason,
		"cancel_attempts": row.CancelAttempts + 1,
	}
	if row.CancelRequestedAtMs == 0 {
		upd["cancel_requested_at_ms"] = nowMs
	}
	return s.db.Model(&ArmedOrderDB{}).Where("id = ?", id).Updates(upd).Error
}

// ConfirmCancel is the ONLY way a row becomes 'cancelled' through the cancel
// path, and it requires the id of the snapshot whose book no longer listed the
// order. A caller with no snapshot cannot call it — which is the point.
func (s *ArmedOrderStore) ConfirmCancel(id int64, snapshotID int64, reason string) error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Model(&ArmedOrderDB{}).Where("id = ?", id).Updates(map[string]any{
		"state":                      StateCancelled,
		"state_reason":               reason,
		"cancel_settled_snapshot_id": snapshotID,
	}).Error
}

// ListCancelPending returns this trader's rows awaiting confirmation, oldest
// request first — the work list for the confirmation pass.
func (s *ArmedOrderStore) ListCancelPending(traderID string) ([]ArmedOrderDB, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var out []ArmedOrderDB
	err := s.db.Where("trader_id = ? AND state = ?", traderID, StateCancelPending).
		Order("cancel_requested_at_ms ASC, id ASC").Find(&out).Error
	return out, err
}

// CountCancelPending is the boot line's figure — READ, never a literal (A11).
func (s *ArmedOrderStore) CountCancelPending() int64 {
	if s == nil || s.db == nil {
		return 0
	}
	var n int64
	_ = s.db.Model(&ArmedOrderDB{}).Where("state = ?", StateCancelPending).Count(&n).Error
	return n
}

// CountCancelUnconfirmed counts pending rows whose first request is older than
// olderThanMs — the ones that have already missed their window.
func (s *ArmedOrderStore) CountCancelUnconfirmed(nowMs, timeoutMs int64) int64 {
	if s == nil || s.db == nil {
		return 0
	}
	var n int64
	_ = s.db.Model(&ArmedOrderDB{}).
		Where("state = ? AND cancel_requested_at_ms > 0 AND cancel_requested_at_ms < ?",
			StateCancelPending, nowMs-timeoutMs).Count(&n).Error
	return n
}

// SetFillPrice records the actual fill price on a FILLED row (PRE-SUNDAY F2 —
// the lineage matcher keys on this; entry_px drifts on re-arm and is NOT the
// fill).
func (s *ArmedOrderStore) SetFillPrice(id int64, fillPrice float64) error {
	return s.db.Model(&ArmedOrderDB{}).Where("id = ?", id).
		Update("fill_price", fillPrice).Error
}

// SetSignal records the wire signal_id once the resting limit is placed
// (armed → working transition).
func (s *ArmedOrderStore) SetSignal(id int64, signalID string) error {
	return s.db.Model(&ArmedOrderDB{}).Where("id = ?", id).
		Update("signal_id", signalID).Error
}

// SetFillQuantity stamps the contracts a fill actually delivered
// (invalidation-wired, 2026-09-03).
//
// armed row 35 (2026-09-03, NY v2 S1) read state=filled with fill_quantity=0
// while trader_positions carried quantity 1. Nothing wrote the column, so the
// ledger could say a row filled and not how much — and 0 is also a legal
// "nothing filled", so the row could not be read either way. WHERE-scoped and
// idempotent; a zero quantity is never written over a real one.
func (s *ArmedOrderStore) SetFillQuantity(id int64, qty int) error {
	if s == nil || s.db == nil || id == 0 || qty <= 0 {
		return nil
	}
	return s.db.Model(&ArmedOrderDB{}).Where("id = ?", id).
		Update("fill_quantity", qty).Error
}

// ListForPlan returns every armed row of one plan chain (card render + API).
func (s *ArmedOrderStore) ListForPlan(planID string) ([]ArmedOrderDB, error) {
	var out []ArmedOrderDB
	err := s.db.Where("plan_id = ?", planID).Order("id").Find(&out).Error
	return out, err
}

// ListFilled returns one trader's most recent FILLED rows, newest first
// (F3, LONDON-FORENSICS 2026-08-28 — the lineage-repair matcher for
// reconcile-materialized positions reads this).
func (s *ArmedOrderStore) ListFilled(traderID string, limit int) ([]ArmedOrderDB, error) {
	if limit <= 0 {
		limit = 20
	}
	var out []ArmedOrderDB
	err := s.db.Where("trader_id = ? AND state = 'filled'", traderID).
		Order("updated_at DESC").Limit(limit).Find(&out).Error
	return out, err
}

// Touch refreshes UpdatedAt (the stale-working reconnect safety net reads it).
func (s *ArmedOrderStore) Touch(id int64) error {
	return s.db.Model(&ArmedOrderDB{}).Where("id = ?", id).
		Update("updated_at", time.Now()).Error
}

// BootSweepReasonPrefix marks a ledger row cancelled by the class-33 boot
// sweep (the machine's own housekeeping), as opposed to an owner/NT8 cancel.
const BootSweepReasonPrefix = "boot_sweep"

// IsBootSweepReason reports whether a terminal row was swept at boot — the ONE
// terminal class that re-authorizes under the same plan version (0B).
func IsBootSweepReason(reason string) bool {
	return strings.HasPrefix(strings.TrimSpace(reason), BootSweepReasonPrefix)
}

// signalOrNone renders a possibly-empty broker signal id for the re-arm line.
func signalOrNone(id string) string {
	if strings.TrimSpace(id) == "" {
		return "(never placed)"
	}
	return id
}

// StateCensus counts armed_orders by state. READ, for the boot line: a claim
// that the ledger is clear must be answered by the table, not asserted.
// WAVE A / cutover 2026-09-05 — the two never-placed arms (104, 105) that
// failed cutover leg 4 were terminalized under owner authorization, and the
// boot line has to be able to SAY that rather than have it live in a chat log.
func (s *ArmedOrderStore) StateCensus() map[string]int64 {
	out := map[string]int64{}
	if s == nil || s.db == nil {
		return out
	}
	type row struct {
		State string
		N     int64
	}
	var rows []row
	if err := s.db.Model(&ArmedOrderDB{}).
		Select("COALESCE(state,'') AS state, COUNT(*) AS n").
		Group("state").Scan(&rows).Error; err != nil {
		return out
	}
	for _, r := range rows {
		out[r.State] = r.N
	}
	return out
}

// ── PLACEMENT CONFIRMATION (class 81, the placement side — 2026-09-07) ───────

// ConfirmPlacement promotes a place_pending row to working, naming the RECEIVED
// frame that justified it. Nothing else may write working.
//
// It is a no-op on any other state, so a late frame cannot resurrect a row that
// has already been rejected, cancelled or filled.
func (s *ArmedOrderStore) ConfirmPlacement(id int64, frame string) error {
	if strings.TrimSpace(frame) == "" {
		return fmt.Errorf("placement confirmation requires received frame evidence")
	}
	return s.db.Model(&ArmedOrderDB{}).Where("id = ? AND state = ?", id, StatePlacePending).
		Updates(map[string]any{"state": StateWorking, "state_reason": "confirmed by " + frame}).Error
}

// RejectPlacement moves a row terminal with THE BROKER'S OWN WORDS. reason is
// passed through verbatim — our summary of a refusal is not the refusal.
//
// It applies to place_pending and working alike: a broker can refuse an order it
// previously acknowledged, and a row that already reads working must still be
// corrected rather than left claiming a state the broker has withdrawn.
func (s *ArmedOrderStore) RejectPlacement(id int64, brokerReason string) error {
	var row ArmedOrderDB
	if err := s.db.First(&row, id).Error; err != nil {
		return err
	}
	return s.ApplyPlacementReceipt(row.TraderID, row.SignalID, StateRejected, brokerReason)
}

// ExpirePlacement records overdue evidence without releasing the slot. An
// elapsed wait is not a broker receipt; the owner requires place_pending until
// received evidence settles it. Keep UpdatedAt as the registration/receipt time.
func (s *ArmedOrderStore) ExpirePlacement(id int64, waited time.Duration) error {
	reason := fmt.Sprintf("unconfirmed:no_frame — awaiting broker receipt for %s; slot held", waited.Round(time.Second))
	result := s.db.Model(&ArmedOrderDB{}).Where("id = ? AND state = ?", id, StatePlacePending).UpdateColumn("state_reason", reason)
	recordResearchPlacementTimeout(id, waited, reason, result.RowsAffected, result.Error)
	return result.Error
}

// ListPlacePending returns one trader's unconfirmed placements, oldest first.
func (s *ArmedOrderStore) ListPlacePending(traderID string) ([]ArmedOrderDB, error) {
	var out []ArmedOrderDB
	err := s.db.Where("trader_id = ? AND state = ?", traderID, StatePlacePending).
		Order("id").Find(&out).Error
	return out, err
}

// FindBySignal resolves a row by the signal id a frame names. Frames carry the
// signal, not our row id, so every confirmation path needs this join.
func (s *ArmedOrderStore) FindBySignal(traderID, signalID string) (*ArmedOrderDB, error) {
	sig := strings.TrimSpace(signalID)
	if sig == "" {
		return nil, nil
	}
	var row ArmedOrderDB
	err := s.db.Where("trader_id = ? AND signal_id = ?", traderID, sig).
		Order("id DESC").First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}
