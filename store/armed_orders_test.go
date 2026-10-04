package store

import (
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"vl/store/sqlitedriver"
)

func newArmedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlitedriver.GormDialector(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	st := NewArmedOrderStore(db)
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// PRE-REOPEN F3 (2026-08-28) — the dead re-arm fix: a TERMINAL row for the
// same (plan, scenario) must come back to life when a new authorization
// arrives. The old Assign-based upsert left terminal rows terminal forever.
func TestUpsertArmReauthorizesTerminalRow(t *testing.T) {
	db := newArmedTestDB(t)
	st := NewArmedOrderStore(db)
	now := time.Now()

	orig := &ArmedOrderDB{
		TraderID: "t1", PlanID: "2026-08-28:planX", Version: 3, Session: "RTH",
		Scenario: "S1", Side: "long", EntryPx: 100, StopPx: 99, TargetPx: 102,
		State: "armed", CreatedAt: now, UpdatedAt: now,
	}
	if err := st.UpsertArm(orig); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	// Terminal: cancelled with lineage from the prior life.
	if err := st.SetState(orig.ID, "cancelled", "gate changed: stale_reeval"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if err := st.SetSignal(orig.ID, "sig-42"); err != nil {
		t.Fatalf("signal: %v", err)
	}

	// New authorization for the same scenario arrives.
	re := &ArmedOrderDB{
		TraderID: "t1", PlanID: "2026-08-28:planX", Version: 4, Session: "ETH",
		Scenario: "S1", Side: "short", EntryPx: 105, StopPx: 106, TargetPx: 102,
		CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
	}
	if err := st.UpsertArm(re); err != nil {
		t.Fatalf("re-arm upsert: %v", err)
	}

	row, err := st.ListNonTerminal("t1")
	if err != nil {
		t.Fatalf("list non-terminal: %v", err)
	}
	if len(row) != 1 {
		t.Fatalf("non-terminal rows = %d, want 1", len(row))
	}
	// D5 (arms-follow-bias, owner ruling 2026-09-04) — CHANGED HERE. This row
	// reached the broker (sig-42), so it is no longer revived in place: every
	// broker placement is one row forever. The re-authorization lands as the
	// NEXT placement and the cancelled row keeps its prices, its signal and its
	// ending. Rows that never reached the broker still revive in place.
	var prior ArmedOrderDB
	if err := db.First(&prior, orig.ID).Error; err != nil {
		t.Fatalf("fetch prior: %v", err)
	}
	if prior.State != "cancelled" || prior.SignalID != "sig-42" || prior.EntryPx != 100 {
		t.Fatalf("the placed row must keep its record: %+v", prior)
	}
	var got ArmedOrderDB
	if err := db.Where("plan_id = ? AND scenario = ? AND id <> ?", re.PlanID, re.Scenario, orig.ID).First(&got).Error; err != nil {
		t.Fatalf("fetch replacement: %v", err)
	}
	if got.PlacementSeq != 1 {
		t.Fatalf("replacement placement_seq = %d, want 1", got.PlacementSeq)
	}
	if got.State != "armed" {
		t.Fatalf("state = %q, want armed", got.State)
	}
	if got.StateReason != "" {
		t.Fatalf("state_reason = %q, want cleared", got.StateReason)
	}
	if got.SignalID != "" {
		t.Fatalf("signal_id = %q, want cleared on re-arm", got.SignalID)
	}
	if got.FillPrice != 0 || got.FillQuantity != 0 {
		t.Fatalf("fill lineage not cleared: price=%v qty=%d", got.FillPrice, got.FillQuantity)
	}
	// SUPERSEDED (class 28, owner ruling 2026-09-03): UpsertArm canonicalizes
	// side to UPPERCASE at the write now, so armed_orders stops disagreeing
	// with trader_positions. The input here is "short"; the stored value is
	// "SHORT".
	if got.Side != "SHORT" || got.EntryPx != 105 || got.StopPx != 106 || got.Version != 4 {
		t.Fatalf("fresh prices not applied: %+v", got)
	}
	if got.ID == orig.ID {
		t.Fatalf("a placed row must NOT be overwritten by its replacement (id %d)", got.ID)
	}
}

// Non-terminal rows keep their identity and only refresh prices.
func TestUpsertArmPreservesNonTerminalIdentity(t *testing.T) {
	db := newArmedTestDB(t)
	st := NewArmedOrderStore(db)
	now := time.Now()

	orig := &ArmedOrderDB{
		TraderID: "t1", PlanID: "2026-08-28:planY", Version: 1, Session: "RTH",
		Scenario: "S2", Side: "long", EntryPx: 200, StopPx: 198, TargetPx: 205,
		State: "armed", CreatedAt: now, UpdatedAt: now,
	}
	if err := st.UpsertArm(orig); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := st.SetState(orig.ID, "working", "placed"); err != nil {
		t.Fatalf("working: %v", err)
	}
	if err := st.SetSignal(orig.ID, "sig-7"); err != nil {
		t.Fatalf("signal: %v", err)
	}

	upd := &ArmedOrderDB{
		TraderID: "t1", PlanID: "2026-08-28:planY", Version: 2, Session: "RTH",
		Scenario: "S2", Side: "long", EntryPx: 201, StopPx: 199, TargetPx: 206,
		CreatedAt: now, UpdatedAt: now.Add(time.Minute),
	}
	// D5 (owner ruling 2026-09-04) — CHANGED HERE. This test used to assert
	// that a WORKING row's prices were refreshed in place. That is the defect:
	// the row describes a LIVE broker order, and rewriting it overwrote the
	// slot and lost the brackets (rows 582, 585). The refresh is now refused;
	// replacing a live order requires a cancel first.
	err := st.UpsertArm(upd)
	if err == nil {
		t.Fatal("refreshing a WORKING row must be refused — the broker order is still live")
	}
	if !strings.Contains(err.Error(), "working") {
		t.Fatalf("the refusal must name the state it protected: %v", err)
	}

	var got ArmedOrderDB
	if err := db.Where("id = ?", orig.ID).First(&got).Error; err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.State != "working" || got.SignalID != "sig-7" {
		t.Fatalf("working identity mutated: state=%q signal=%q", got.State, got.SignalID)
	}
	// The row must still describe the order the broker actually holds.
	if got.EntryPx != orig.EntryPx || got.StopPx != orig.StopPx || got.TargetPx != orig.TargetPx {
		t.Fatalf("prices were rewritten under a live order: %+v", got)
	}
}

// PRE-SUNDAY F2/F4 (2026-08-28) — fill_price is the lineage-matcher's
// authoritative fill (entry_px drifts on re-arm); ListNonTerminal is
// trader-scoped.
func TestSetFillPriceAndTraderScopedList(t *testing.T) {
	db := newArmedTestDB(t)
	st := NewArmedOrderStore(db)
	now := time.Now()
	a := &ArmedOrderDB{TraderID: "t1", PlanID: "P1", Version: 1, Session: "NY",
		Scenario: "S1", Side: "short", EntryPx: 29702.0, State: "armed", CreatedAt: now, UpdatedAt: now}
	if err := st.UpsertArm(a); err != nil {
		t.Fatal(err)
	}
	b := &ArmedOrderDB{TraderID: "t2", PlanID: "P2", Version: 1, Session: "NY",
		Scenario: "S2", Side: "long", EntryPx: 100, State: "working", CreatedAt: now, UpdatedAt: now}
	if err := st.UpsertArm(b); err != nil {
		t.Fatal(err)
	}
	if rows, err := st.ListNonTerminal("t1"); err != nil || len(rows) != 1 || rows[0].Scenario != "S1" {
		t.Fatalf("t1 scope: rows=%+v err=%v", rows, err)
	}
	if rows, err := st.ListNonTerminal("t2"); err != nil || len(rows) != 1 || rows[0].Scenario != "S2" {
		t.Fatalf("t2 scope: rows=%+v err=%v", rows, err)
	}
	if err := st.SetFillPrice(a.ID, 29642.00); err != nil {
		t.Fatal(err)
	}
	var got ArmedOrderDB
	if err := db.Where("id = ?", a.ID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.FillPrice != 29642.00 {
		t.Fatalf("fill_price = %.2f, want 29642.00", got.FillPrice)
	}
}

// TestUpsertArmManualCancelWinsSameVersion — the 2026-08-30 E7-incident law:
// a TERMINAL row (owner/NT8 cancel or a completed fill) must NOT be
// re-authorized by the same plan version (the re-place loop: terminal → armed
// → marketable fill → stop-out → terminal → armed … forever while the confirm
// stayed MET). Manual cancel wins until the planner writes a NEW version.
func TestUpsertArmManualCancelWinsSameVersion(t *testing.T) {
	db := newArmedTestDB(t)
	st := NewArmedOrderStore(db)
	now := time.Now()
	a := &ArmedOrderDB{TraderID: "t1", PlanID: "P1", Version: 2, Session: "ASIA",
		Scenario: "S2", Side: "long", EntryPx: 29371.5, StopPx: 29350.0, TargetPx: 29420.0,
		State: "armed", CreatedAt: now, UpdatedAt: now}
	if err := st.UpsertArm(a); err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(a.ID, "cancelled", "owner manual cancel"); err != nil {
		t.Fatal(err)
	}
	// The executor's eval loop calls UpsertArm with State:"armed" every cycle.
	retry := &ArmedOrderDB{TraderID: "t1", PlanID: "P1", Version: 2, Session: "ASIA",
		Scenario: "S2", Side: "long", EntryPx: 29371.5, StopPx: 29350.0, TargetPx: 29420.0,
		State: "armed", CreatedAt: now, UpdatedAt: now}
	if err := st.UpsertArm(retry); err != nil {
		t.Fatal(err)
	}
	var got ArmedOrderDB
	if err := db.Where("id = ?", a.ID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.State != "cancelled" {
		t.Fatalf("state = %q, want cancelled (manual cancel must win within the same version)", got.State)
	}
	// A FILLED row is equally protected (the fill→stop-out→re-arm half of the loop).
	if err := st.SetState(a.ID, "filled", "fill@29347.25"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertArm(retry); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", a.ID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.State != "filled" {
		t.Fatalf("state = %q, want filled (same version never resurrects a terminal row)", got.State)
	}
}

// TestUpsertArmReauthorizesOnVersionBump — a NEW plan version is the sanctioned
// re-arm path (the planner re-authored the scenario).
func TestUpsertArmReauthorizesOnVersionBump(t *testing.T) {
	db := newArmedTestDB(t)
	st := NewArmedOrderStore(db)
	now := time.Now()
	a := &ArmedOrderDB{TraderID: "t1", PlanID: "P1", Version: 2, Session: "ASIA",
		Scenario: "S2", Side: "long", EntryPx: 29371.5, StopPx: 29350.0, TargetPx: 29420.0,
		State: "armed", CreatedAt: now, UpdatedAt: now}
	if err := st.UpsertArm(a); err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(a.ID, "cancelled", "owner manual cancel"); err != nil {
		t.Fatal(err)
	}
	next := &ArmedOrderDB{TraderID: "t1", PlanID: "P1", Version: 3, Session: "ASIA",
		Scenario: "S2", Side: "long", EntryPx: 29371.5, StopPx: 29350.0, TargetPx: 29420.0,
		State: "armed", CreatedAt: now, UpdatedAt: now}
	if err := st.UpsertArm(next); err != nil {
		t.Fatal(err)
	}
	var got ArmedOrderDB
	if err := db.Where("id = ?", a.ID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.State != "armed" || got.Version != 3 {
		t.Fatalf("state=%q version=%d, want armed v3 (version bump re-authorizes)", got.State, got.Version)
	}
}

// SetArmExpiry (PR B stop-limit, 2026-10-03): an expiry can only be stamped on
// an unfilled arm. A positive stamp lands and survives; a filled or terminal
// row refuses it (never silent) — the caller's intent and the ledger
// disagreeing must not be papered over.
func TestSetArmExpiryStampsOnlyUnfilledRows(t *testing.T) {
	db := newArmedTestDB(t)
	st := NewArmedOrderStore(db)
	now := time.Now()
	armed := &ArmedOrderDB{
		TraderID: "t1", PlanID: "2026-10-03:planZ", Version: 1, Session: "RTH",
		Scenario: "S1", Side: "long", EntryPx: 100, StopPx: 99, TargetPx: 102,
		State: "armed", CreatedAt: now, UpdatedAt: now,
	}
	if err := st.UpsertArm(armed); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	expiry := now.UnixMilli() + 60_000
	if err := st.SetArmExpiry(armed.ID, expiry); err != nil {
		t.Fatalf("an unfilled arm must accept its expiry: %v", err)
	}
	if err := st.SetArmExpiry(armed.ID, 0); err == nil {
		t.Fatal("a non-positive expiry must be refused")
	}
	if err := st.SetState(armed.ID, StateWorking, "entry order_update"); err != nil {
		t.Fatalf("to working: %v", err)
	}
	extended := expiry + 60_000
	if err := st.SetArmExpiry(armed.ID, extended); err != nil {
		t.Fatalf("a WORKING, UNFILLED row is the resting stop-limit the evaluator extends (ISB stacking) — the stamp must be accepted: %v", err)
	}
	var got ArmedOrderDB
	if err := st.DB().First(&got, armed.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ExpiryMs != extended {
		t.Fatalf("the working row's expiry was not stamped: got %d, want %d", got.ExpiryMs, extended)
	}
	if err := st.DB().Model(&ArmedOrderDB{}).Where("id = ?", armed.ID).Update("fill_quantity", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.SetArmExpiry(armed.ID, expiry); err == nil {
		t.Fatal("a PARTIALLY FILLED working row is a trade in progress — its expiry must never be moved")
	}
	if err := st.SetState(armed.ID, StateCancelled, "owner"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if err := st.SetArmExpiry(armed.ID, expiry); err == nil {
		t.Fatal("a terminal row must refuse an expiry stamp")
	}
}

// TestArmExpirySurvivesTheTransitionToWorking (PR B stop-limit, 2026-10-03): a
// stop-limit resting at NT8 is a working, unfilled order, and its expiry must
// survive armed → place_pending → working. If the transition cleared it, every
// order that reached the broker would never expire.
func TestArmExpirySurvivesTheTransitionToWorking(t *testing.T) {
	db := newArmedTestDB(t)
	st := NewArmedOrderStore(db)
	now := time.Now()
	row := &ArmedOrderDB{
		TraderID: "t1", PlanID: "2026-10-03:planY", Version: 1, Session: "RTH",
		Scenario: "S1", Side: "long", EntryPx: 100, StopPx: 99, TargetPx: 102,
		State: "armed", CreatedAt: now, UpdatedAt: now,
	}
	if err := st.UpsertArm(row); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := st.BeginPlacement(row.ID, "sig-exp"); err != nil {
		t.Fatalf("begin placement: %v", err)
	}
	expiry := now.UnixMilli() + 60_000
	if err := st.SetArmExpiry(row.ID, expiry); err != nil {
		t.Fatalf("set expiry: %v", err)
	}
	if err := st.ConfirmPlacement(row.ID, "order_update"); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	rows, err := st.ListNonTerminal("t1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range rows {
		if r.ID == row.ID {
			if r.State != StateWorking {
				t.Fatalf("state=%q, want working", r.State)
			}
			if r.ExpiryMs != expiry {
				t.Fatalf("the expiry must survive the transition to working, got %d want %d", r.ExpiryMs, expiry)
			}
			return
		}
	}
	t.Fatal("row not found in the non-terminal list")
}
