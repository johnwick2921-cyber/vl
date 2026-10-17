package store

import (
	"errors"
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
	// bumps it; the confirmation does not. It is counted PER PROCESS — see
	// CancelAttemptsBoot.
	CancelAttempts int `gorm:"default:0"`
	// CancelAttemptsBoot (B2, 2026-09-10) is the ProcessBootID under which
	// CancelAttempts was counted. A restart resets the budget, because the
	// restart is exactly the event that changes the facts the cap was guarding
	// against: a new wire, a re-seeded book, a broker that may now answer.
	// Before this column a row that hit the cap pre-restart arrived capped and
	// confirmPendingCancels (trader/cancel_confirm.go) would neither re-request
	// nor promote it — it was stranded in cancel_pending for the life of the
	// ledger.
	//
	// It is DELIBERATELY not the existing BootID column: that one answers "which
	// process AUTHORED this row" (class 33). Two questions on one field is the
	// failure this repo keeps meeting; they get one field each.
	CancelAttemptsBoot string `gorm:"default:''"`
	// CancelSettledSnapshotID is the nt8_order_snapshots id whose book no
	// longer listed the order — the evidence the cancel actually happened.
	// 0 on a row that reached 'cancelled' any other way, which is every row
	// written before this wave.
	CancelSettledSnapshotID int64 `gorm:"default:0"`

	// W3 market_in_zone (2026-09-23). Every field is ABSENT (NULL / '') on a
	// legacy or planned_order row — 0 is a real value for slippage and a real
	// price nowhere, so absence is never written as 0.
	//   Policy          — the leg's entry policy ('' = legacy).
	//   ZoneLo/ZoneHi   — the planner's entry_zone rounded INWARD to the tick.
	//   ZoneProvenance  — resolveEntryGeometryZone's label (never a refusal).
	//   PlannedEntryPx  — the authored entry (EntryPx is the far bound).
	//   EvalPrice/EvalBarMs — the price and bar the placement verdict read.
	//   PlacedAtMs      — when the limit was sent (the rest-cap clock;
	//                     updated_at is rewritten by every pass).
	//   FilledAtMs, FillSlippageTicks — the fill receipt (slippage vs the
	//                     wire limit, the AddOn's formula, side-adjusted + =
	//                     worse); LastVerdict/LastVerdictMs — the executor's
	//                     latest verdict for the card ("Waiting"/"Blocked").
	Policy            string `gorm:"default:''"`
	ZoneLo            *float64
	ZoneHi            *float64
	ZoneProvenance    string `gorm:"default:''"`
	PlannedEntryPx    *float64
	EvalPrice         *float64
	EvalBarMs         *int64
	PlacedAtMs        *int64
	FilledAtMs        *int64
	FillSlippageTicks *float64
	LastVerdict       string `gorm:"default:''"`
	LastVerdictMs     *int64

	// W-EXEC-TRUTH W5 (2026-09-23) — a MACHINE-SOURCED row (a Picture
	// scenario of the Day Plan). '' / NULL on every planner row.
	//   Source          — ArmSourcePicture.
	//   SourceRef       — the opportunity key: ONE opportunity, ONE order,
	//                     across plan versions (UpsertArm's source pin — a
	//                     Picture scenario is re-appended to the version that
	//                     supersedes a machine plan, so the version cannot be
	//                     its identity).
	//   SourceRule      — the rule that produced it (h1_close_break).
	//   EligibleUntilMs — the eligibility deadline (never placed after it).
	//   SourceRunEpoch  — the trader run that recorded it (a reload cannot
	//                     place a scenario recorded by a previous run).
	Source          string `gorm:"default:''"`
	SourceRef       string `gorm:"default:''"`
	SourceRule      string `gorm:"default:''"`
	EligibleUntilMs *int64
	SourceRunEpoch  *int64

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

// ArmPolicyMarketInZone is kernel.EntryPolicyMarketInZone (W3), restated here
// because store cannot import kernel (kernel imports store). A trader test pins
// the two equal, so this is a mirror with a check, not a second truth.
const ArmPolicyMarketInZone = "market_in_zone"

// ArmSourcePicture is kernel.ScenarioSourcePicture (W5), mirrored for the same
// reason; a kernel test pins the two equal.
const ArmSourcePicture = "picture"

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
	cancel_attempts_boot TEXT NOT NULL DEFAULT '',
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
			// B2 (2026-09-10) — the boot that counted cancel_attempts. Empty on
			// every historical row, which reads as "counted by no process this
			// one can identify", so the first request after this ships resets the
			// budget once. That is the correct answer for a row whose attempts
			// were accumulated by a process that is gone.
			{"cancel_attempts_boot", "TEXT NOT NULL DEFAULT ''"},
			{"cancel_settled_snapshot_id", "INTEGER NOT NULL DEFAULT 0"},
			// W3 market_in_zone (2026-09-23): NULLable where 0 would be a
			// fabricated value (absent ≠ 0); '' where the text is a label.
			{"policy", "TEXT NOT NULL DEFAULT ''"},
			{"zone_lo", "REAL"},
			{"zone_hi", "REAL"},
			{"zone_provenance", "TEXT NOT NULL DEFAULT ''"},
			{"planned_entry_px", "REAL"},
			{"eval_price", "REAL"},
			{"eval_bar_ms", "INTEGER"},
			{"placed_at_ms", "INTEGER"},
			{"filled_at_ms", "INTEGER"},
			{"fill_slippage_ticks", "REAL"},
			{"last_verdict", "TEXT NOT NULL DEFAULT ''"},
			{"last_verdict_ms", "INTEGER"},
			// W5 machine source (2026-09-23): '' on every planner row; the
			// deadline and run epoch are NULL where none exists (absent ≠ 0).
			{"source", "TEXT NOT NULL DEFAULT ''"},
			{"source_ref", "TEXT NOT NULL DEFAULT ''"},
			{"source_rule", "TEXT NOT NULL DEFAULT ''"},
			{"eligible_until_ms", "INTEGER"},
			{"source_run_epoch", "INTEGER"},
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

// ErrArmSourceMismatch is UpsertArm's refusal for a write that would land on
// a row carrying ANOTHER opportunity (W5 R13, CTO round 2): a ledger row never
// changes opportunity. The (plan, scenario, leg) key is version-insensitive,
// so a P id reused on a later version used to rewrite the older opportunity's
// row — source_ref, deadline, epoch and prices — with only an INFO.
var ErrArmSourceMismatch = errors.New("armed_orders: the ledger row belongs to another opportunity")

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
	// W5 — ONE OPPORTUNITY, ONE LIFE, ACROSS PLAN VERSIONS. A machine-sourced
	// row is identified by its opportunity (source_ref), not by the plan
	// version: the Picture scenario is re-appended to the version that
	// supersedes a machine plan (CTO 1790191033566), so the version-scoped
	// pins below would let a placed, filled or expired opportunity arm again.
	// Once ANY row for the opportunity is terminal, no row for it is ever
	// armed again; a live row for it under another identity keeps the slot.
	if ref := strings.TrimSpace(row.SourceRef); ref != "" {
		var prior ArmedOrderDB
		perr := s.db.Where("trader_id = ? AND source_ref = ?", row.TraderID, ref).
			Order("CASE WHEN " + NonTerminalArmStateSQL() + " THEN 0 ELSE 1 END, id DESC").First(&prior).Error
		if perr == nil {
			if IsTerminalArmState(prior.State) {
				return nil
			}
			if prior.PlanID != row.PlanID || prior.Scenario != row.Scenario || prior.LegIndex != row.LegIndex {
				return nil
			}
		} else if perr != gorm.ErrRecordNotFound {
			return perr
		}
	}
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
		// W5 R13(a) — A LEDGER ROW NEVER CHANGES OPPORTUNITY. Every branch
		// below writes onto (or mints the next placement of) this key; when the
		// row already carries a DIFFERENT opportunity, none of them may run —
		// the armed branch would rewrite A's unplaced row to B's source_ref,
		// deadline, epoch and prices. Refused by type, and SILENTLY here
		// (WAVE 1b E7): every pass re-authors the same leg and re-hits this
		// refusal, and the store has no identity to dedupe on. The typed error
		// carries every field (row, scenario, leg, state, both redacted keys);
		// the caller logs it — the authoring loop's armSourceRefused WARNs and
		// counts once per change, the shadow path and the API seams log/return it.
		if ex := strings.TrimSpace(existing.SourceRef); ex != "" && ex != strings.TrimSpace(row.SourceRef) {
			return fmt.Errorf("%w: row #%d (%s leg %d, %s) holds %s, the write carries %s",
				ErrArmSourceMismatch, existing.ID, row.Scenario, row.LegIndex+1, existing.State,
				RedactPictureOppKey(ex), RedactPictureOppKey(strings.TrimSpace(row.SourceRef)))
		}
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
		// W3 D15 — RE-ARM PINNED. A market_in_zone row that REACHED THE BROKER
		// (it carries a signal) is terminal for its plan version: filled,
		// stopped, cancelled by the rest cap or withdrawn for maintenance, the
		// same version never mints it again. Without this the mint below is the
		// re-place loop (fill → stop-out → seq+1 → marketable limit → fill…),
		// and the rest cap becomes a 30-minute re-placement timer. A NEW
		// version re-arms (the mint runs); a boot-swept row keeps the 0B law.
		if existing.State != StateArmed && strings.TrimSpace(existing.SignalID) != "" &&
			existing.Policy == ArmPolicyMarketInZone && existing.Version == row.Version &&
			!IsBootSweepReason(existing.StateReason) {
			return nil
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
			row.EvalPrice, row.EvalBarMs, row.PlacedAtMs = nil, nil, nil
			row.FilledAtMs, row.FillSlippageTicks = nil, nil
			row.LastVerdict, row.LastVerdictMs = "", nil
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
				// W3 — the entry policy and its zone follow the authorization.
				// A legacy row writes '' / NULL over '' / NULL.
				"policy": row.Policy, "zone_lo": row.ZoneLo, "zone_hi": row.ZoneHi,
				"zone_provenance": row.ZoneProvenance, "planned_entry_px": row.PlannedEntryPx,
				// W5 — the machine source follows the authorization too (a
				// planner row writes '' / NULL over '' / NULL).
				"source": row.Source, "source_ref": row.SourceRef, "source_rule": row.SourceRule,
				"eligible_until_ms": row.EligibleUntilMs, "source_run_epoch": row.SourceRunEpoch,
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
			// W3 — the new authorization's policy and zone; the receipts of the
			// placement it replaces are cleared, never inherited (absent ≠ 0).
			"policy": row.Policy, "zone_lo": row.ZoneLo, "zone_hi": row.ZoneHi,
			"zone_provenance": row.ZoneProvenance, "planned_entry_px": row.PlannedEntryPx,
			"eval_price": nil, "eval_bar_ms": nil, "placed_at_ms": nil,
			"filled_at_ms": nil, "fill_slippage_ticks": nil,
			"last_verdict": "", "last_verdict_ms": nil,
			// W5 — the new authorization's machine source ('' on planner rows).
			"source": row.Source, "source_ref": row.SourceRef, "source_rule": row.SourceRule,
			"eligible_until_ms": row.EligibleUntilMs, "source_run_epoch": row.SourceRunEpoch,
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

// ListNonTerminalAllTraders is the INSTALLATION-WIDE twin of ListNonTerminal
// (W-ONE-BUTTON M2 gate): every trader id, loaded or not — a row for a
// stopped, deleted or never-loaded trader may still be an order at the broker.
// Deliberately unscoped (F4 scoped the per-trader reader, not this one); the
// state filter is the canonical NonTerminalArmStateSQL.
func (s *ArmedOrderStore) ListNonTerminalAllTraders() ([]ArmedOrderDB, error) {
	var out []ArmedOrderDB
	err := s.db.Where(NonTerminalArmStateSQL()).Order("id").Find(&out).Error
	return out, err
}

// SettleNeverSent retires the place_pending row for signalID as cancelled when
// the entry provably never reached NT8 (W-ONE-BUTTON M2, M-2: the maintenance
// hold dropped it from the reconnect queue before any byte was written). Only
// place_pending moves — a row a frame already confirmed, rejected or filled is
// never touched. Returns the rows moved.
func (s *ArmedOrderStore) SettleNeverSent(signalID, reason string) (int64, error) {
	sig := strings.TrimSpace(signalID)
	if sig == "" {
		return 0, nil
	}
	r := s.db.Model(&ArmedOrderDB{}).Where("signal_id = ? AND state = ?", sig, StatePlacePending).
		Updates(map[string]any{"state": StateCancelled, "state_reason": reason})
	return r.RowsAffected, r.Error
}

// SetState transitions one row's state with a reason (the ledger rule: a
// terminal state change is never silent).
func (s *ArmedOrderStore) SetState(id int64, state, reason string) error {
	return s.db.Model(&ArmedOrderDB{}).Where("id = ?", id).
		Updates(map[string]any{"state": state, "state_reason": reasonKeepingWithdraw(reason)}).Error
}

// ── W-EXEC-TRUTH W0 (f) — a withdrawn row keeps its withdraw head ──────────
//
// The withdraw writes "withdraw: <why>" as the row's reason when it asks
// NinjaTrader to cancel a resting entry. Every later lifecycle write — a
// re-request, a received order_update, a snapshot confirm, a placement
// receipt — used to REPLACE the reason, and the withdraw view (which finds a
// job's rows by that head) lost the row the moment its cancel progressed.
// The store now owns the rule: a row whose reason starts with the withdraw
// prefix keeps it, and each later reason is appended after the separator —
// an audit trail with a named bound: at most CANCEL_REREQUEST_MAX re-request
// appends per process boot (default 5; RequestCancel restarts the count on a
// new boot, B2, so N boots allow N×5) plus one terminal write (the
// order_update, the snapshot confirm or a placement receipt) — each append a
// few dozen bytes. Plain SQL (CASE, LIKE, ||) so it holds on both store
// dialects.
const (
	WithdrawReasonPrefix = "withdraw: "
	WithdrawReasonSep    = " ‖ "
)

// reasonKeepingWithdraw is the state_reason value every lifecycle writer
// uses.
func reasonKeepingWithdraw(reason string) any {
	return gorm.Expr("CASE WHEN state_reason LIKE ? THEN state_reason || ? || ? ELSE ? END",
		WithdrawReasonPrefix+"%", WithdrawReasonSep, reason, reason)
}

// ListWithdrawn lists the rows a withdraw with exactly this head asked to
// cancel: the reason IS the head, or starts with the head and the separator.
// Never a bare prefix — "…job job1" must not match "…job job10".
func (s *ArmedOrderStore) ListWithdrawn(head string) ([]ArmedOrderDB, error) {
	var out []ArmedOrderDB
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("armed_orders: no ledger")
	}
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(head + WithdrawReasonSep)
	err := s.db.Where("state_reason = ? OR state_reason LIKE ? ESCAPE '\\'", head, esc+"%").Order("id").Find(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
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

// BeginPlacementEval is BeginPlacement for a market_in_zone row (W3): the SAME
// compare-and-set (armed, no signal yet) plus a non-empty policy, and in the same
// write the evidence the placement verdict read — the price, the bar it came
// from — and the send time the rest cap measures from (placed_at_ms; updated_at
// is rewritten by every pass and is not a placement age). A legacy row can
// never be stamped by it; BeginPlacement is untouched.
func (s *ArmedOrderStore) BeginPlacementEval(id int64, signalID string, evalPx float64, evalBarMs, placedAtMs int64) error {
	if strings.TrimSpace(signalID) == "" {
		return fmt.Errorf("armed_orders: placement requires signal id")
	}
	r := s.db.Model(&ArmedOrderDB{}).Where("id = ? AND state = ? AND (signal_id = '' OR signal_id IS NULL) AND policy <> ''", id, StateArmed).
		Updates(map[string]any{"signal_id": signalID, "state": StatePlacePending, "state_reason": "",
			"eval_price": evalPx, "eval_bar_ms": evalBarMs, "placed_at_ms": placedAtMs})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return fmt.Errorf("armed_orders: row %d is no longer eligible for a zone placement", id)
	}
	return nil
}

// SetFillReceipt records a market_in_zone fill's receipt (W3 D17): when the
// fill frame was received and the slippage against the wire limit in ticks,
// side-adjusted (+ = worse). slip nil = not computable (NULL, never 0). Only a
// policy row is written — a legacy row keeps NULL.
func (s *ArmedOrderStore) SetFillReceipt(id int64, filledAtMs int64, slip *float64) error {
	if s == nil || s.db == nil || id == 0 {
		return nil
	}
	return s.db.Model(&ArmedOrderDB{}).Where("id = ? AND policy <> ''", id).
		Updates(map[string]any{"filled_at_ms": filledAtMs, "fill_slippage_ticks": slip}).Error
}

// SetLastVerdict records the executor's latest placement verdict for a row
// (the card's "Waiting for price" / "Blocked: …"). Written only when the
// verdict CHANGES, so a pass that re-reads the same verdict writes nothing and
// last_verdict_ms is when the verdict began. Returns whether it wrote.
func (s *ArmedOrderStore) SetLastVerdict(id int64, verdict string, atMs int64) (bool, error) {
	if s == nil || s.db == nil || id == 0 {
		return false, nil
	}
	r := s.db.Model(&ArmedOrderDB{}).Where("id = ? AND last_verdict <> ?", id, verdict).
		Updates(map[string]any{"last_verdict": verdict, "last_verdict_ms": atMs})
	return r.RowsAffected == 1, r.Error
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
	return q.Updates(map[string]any{"state": state, "state_reason": reasonKeepingWithdraw(reason)}).Error
}

// ResetToArmedUnplaced (WAVE PLANNER B1) returns a row whose resting order
// was cancelled by the zone rest cap to armed-unplaced: state=armed, the
// placement stamp cleared (signal_id, eval_price, eval_bar_ms, placed_at_ms)
// and placement_seq+1 — the next broker placement is a NEW seq under the D5
// append-only rule. The wire cancel is the CALLER's, sent BEFORE this write;
// until the broker's book confirms it the placement slot guard refuses, so a
// re-place cannot double-book the old order.
func (s *ArmedOrderStore) ResetToArmedUnplaced(id int64, reason string) error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Model(&ArmedOrderDB{}).Where("id = ?", id).Updates(map[string]any{
		"state":                  StateArmed,
		"state_reason":           reasonKeepingWithdraw(reason),
		"signal_id":              "",
		"eval_price":             nil,
		"eval_bar_ms":            nil,
		"placed_at_ms":           nil,
		"placement_seq":          gorm.Expr("placement_seq + 1"),
		"cancel_requested_at_ms": 0,
		"cancel_attempts":        0,
		"cancel_attempts_boot":   "",
	}).Error
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
	// B2 — the budget is PER PROCESS. A row whose attempts were counted by a
	// different (or no longer identifiable) process starts again at 1 under
	// this one, so a restart re-opens a capped row instead of stranding it.
	attempts := row.CancelAttempts + 1
	if row.CancelAttemptsBoot != ProcessBootID() {
		attempts = 1
	}
	upd := map[string]any{
		"state":                StateCancelPending,
		"state_reason":         reasonKeepingWithdraw(reason),
		"cancel_attempts":      attempts,
		"cancel_attempts_boot": ProcessBootID(),
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
		"state_reason":               reasonKeepingWithdraw(reason),
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

// CountForeignBootCancelAttempts (B2) counts rows whose cancel_attempts were
// tallied by a process OTHER than this one — the rows whose budget resets on
// their next request. It is the carry the cancel boot line reports, and it is a
// MEASURED number: a read failure returns -1 so the caller prints UNKNOWN
// rather than a zero it did not earn (A24).
func (s *ArmedOrderStore) CountForeignBootCancelAttempts() int64 {
	if s == nil || s.db == nil {
		return -1
	}
	var n int64
	if err := s.db.Model(&ArmedOrderDB{}).
		Where("cancel_attempts > 0 AND cancel_attempts_boot <> ?", ProcessBootID()).
		Count(&n).Error; err != nil {
		return -1
	}
	return n
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

// LedgerClockSlack widens a SQL bound on a time column stored as zone-bearing
// text: the lexical compare is exact only when every writer used one zone, so
// a "since" read fetches this much extra and its caller re-checks the exact
// window on the parsed time. Over-fetching only costs rows; under-fetching
// would hide a fresh fill.
const LedgerClockSlack = 24 * time.Hour

// ListFilledSinceAllTraders returns FILLED rows of EVERY trader — loaded,
// running, stopped or deleted — whose updated_at may fall at or after since
// (W1b E10: "did any producer fill on this account just now?" is a ledger
// question; a trader that stopped between its fill and the read still owns
// that fill). The SQL bound is widened by LedgerClockSlack; callers MUST
// re-check the exact window on UpdatedAt. Single-state filter on the canonical
// StateFilled constant. Newest first.
func (s *ArmedOrderStore) ListFilledSinceAllTraders(since time.Time) ([]ArmedOrderDB, error) {
	var out []ArmedOrderDB
	err := s.db.Where("state = ? AND updated_at >= ?", StateFilled, since.Add(-LedgerClockSlack)).
		Order("updated_at DESC").Find(&out).Error
	return out, err
}

// ListFilledSince returns ONE trader's FILLED rows whose updated_at may fall at
// or after since (W1b FOLD-4: the untracked materialization's price-match
// fallback reads only arms filled inside the fill ring's own window — an older
// arm never matches). The SQL bound is widened by LedgerClockSlack; callers
// MUST re-check the exact window on the parsed UpdatedAt. Newest first by text
// (the caller orders by instant).
func (s *ArmedOrderStore) ListFilledSince(traderID string, since time.Time) ([]ArmedOrderDB, error) {
	var out []ArmedOrderDB
	err := s.db.Where("trader_id = ? AND state = ? AND updated_at >= ?", traderID, StateFilled, since.Add(-LedgerClockSlack)).
		Order("updated_at DESC").Find(&out).Error
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
// StateOf returns one row's CURRENT state, and whether it could be read.
//
// The drain loop in cancelArmedOrdersSyncWith previously asked only "is this row
// still in the non-terminal set?" and treated false as "the cancel was acked".
// Every terminal state answers false — including FILLED. A limit that filled two
// seconds before the close was therefore counted and logged as an order we
// cancelled. The two facts are opposite and the caller needs to tell them apart,
// so it reads the state rather than a boolean derived from it.
//
// ok=false means the row could not be read AT ALL (a failed query, a deleted
// row). It is never "terminal" — A24: unknown takes no branch that declares an
// outcome.
func (s *ArmedOrderStore) StateOf(id int64) (state string, ok bool) {
	if s == nil || s.db == nil {
		return "", false
	}
	var row ArmedOrderDB
	if err := s.db.Select("state").First(&row, id).Error; err != nil {
		return "", false
	}
	return strings.ToLower(strings.TrimSpace(row.State)), true
}

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
