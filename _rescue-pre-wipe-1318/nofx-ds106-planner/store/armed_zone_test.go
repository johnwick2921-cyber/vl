package store

import (
	"testing"
	"time"
)

// ── W-EXEC-TRUTH W3 — the market_in_zone ledger writers ─────────────────────
//
// The columns shipped with the foundation (e74fce17); these are their writers:
// UpsertArm carries the policy and zone through create / armed refresh /
// re-authorize, BeginPlacementEval stamps the placement evidence under the
// SAME compare-and-set as BeginPlacement, the D15 pin stops the re-arm loop,
// and the receipts are written only for policy rows.

func zoneRow(version int) *ArmedOrderDB {
	now := time.Date(2026, 9, 11, 15, 0, 0, 0, time.UTC)
	lo, hi, planned := 31000.0, 31010.0, 31005.0
	return &ArmedOrderDB{
		TraderID: "t1", PlanID: "2026-09-11:NY:t1", Version: version, Session: "NY", Scenario: "S2", Side: "long",
		EntryPx: 31010, StopPx: 30990, TargetPx: 31060, State: StateArmed, CreatedAt: now, UpdatedAt: now,
		Policy: ArmPolicyMarketInZone, ZoneLo: &lo, ZoneHi: &hi, ZoneProvenance: "frozen_zone:PDH", PlannedEntryPx: &planned,
	}
}

func zoneRows(t *testing.T, st *ArmedOrderStore) []ArmedOrderDB {
	t.Helper()
	var rows []ArmedOrderDB
	if err := st.db.Where("plan_id = ? AND scenario = ?", "2026-09-11:NY:t1", "S2").Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

// The migrate adds every W3 column to an EXISTING table, and a legacy row
// reads them absent (NULL, or empty text), never 0.
func TestArmedZoneMigrateAddsColumnsAndLegacyRowsReadAbsent(t *testing.T) {
	db := newArmedTestDB(t)
	st := NewArmedOrderStore(db)
	for _, c := range []string{"policy", "zone_lo", "zone_hi", "zone_provenance", "planned_entry_px", "eval_price",
		"eval_bar_ms", "placed_at_ms", "filled_at_ms", "fill_slippage_ticks", "last_verdict", "last_verdict_ms"} {
		var n int64
		if err := db.Raw("SELECT COUNT(*) FROM pragma_table_info('armed_orders') WHERE name = ?", c).Scan(&n).Error; err != nil || n != 1 {
			t.Fatalf("column %s missing after Migrate (n=%d err=%v)", c, n, err)
		}
	}
	if err := st.Migrate(); err != nil { // idempotent
		t.Fatalf("second Migrate: %v", err)
	}
	legacy := zoneRow(1)
	legacy.Policy, legacy.ZoneLo, legacy.ZoneHi, legacy.ZoneProvenance, legacy.PlannedEntryPx = "", nil, nil, "", nil
	if err := st.UpsertArm(legacy); err != nil {
		t.Fatal(err)
	}
	var got ArmedOrderDB
	if err := db.First(&got, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Policy != "" || got.ZoneLo != nil || got.ZoneHi != nil || got.PlannedEntryPx != nil || got.EvalPrice != nil ||
		got.PlacedAtMs != nil || got.FillSlippageTicks != nil || got.LastVerdict != "" {
		t.Fatalf("a legacy row must read every W3 field absent, got %+v", got)
	}
}

// create, armed refresh and re-authorize all carry the policy and zone; a
// re-authorize clears the previous placement's evidence.
func TestArmedZoneUpsertCarriesPolicyAndClearsReceiptsOnReauthorize(t *testing.T) {
	st := NewArmedOrderStore(newArmedTestDB(t))
	r := zoneRow(1)
	if err := st.UpsertArm(r); err != nil {
		t.Fatal(err)
	}
	rows := zoneRows(t, st)
	if len(rows) != 1 || rows[0].Policy != ArmPolicyMarketInZone || rows[0].ZoneLo == nil || *rows[0].ZoneLo != 31000 ||
		*rows[0].ZoneHi != 31010 || *rows[0].PlannedEntryPx != 31005 || rows[0].ZoneProvenance != "frozen_zone:PDH" {
		t.Fatalf("create must carry the policy and zone: %+v", rows)
	}
	// armed refresh: a moved zone is written through.
	moved := zoneRow(1)
	lo, hi := 31002.0, 31008.0
	moved.ZoneLo, moved.ZoneHi, moved.EntryPx = &lo, &hi, 31008
	if err := st.UpsertArm(moved); err != nil {
		t.Fatal(err)
	}
	rows = zoneRows(t, st)
	if len(rows) != 1 || *rows[0].ZoneLo != 31002 || *rows[0].ZoneHi != 31008 || rows[0].EntryPx != 31008 {
		t.Fatalf("armed refresh must rewrite the zone: %+v", rows)
	}
	// place + a never-sent terminal (no signal) under v1, then re-authorize at v2.
	id := rows[0].ID
	if _, err := st.SetLastVerdict(id, "short_of_zone", 1); err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(id, StateCancelled, "owner cancel"); err != nil {
		t.Fatal(err)
	}
	if err := st.db.Model(&ArmedOrderDB{}).Where("id = ?", id).Updates(map[string]any{"eval_price": 31009.5, "placed_at_ms": 5}).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertArm(zoneRow(2)); err != nil {
		t.Fatal(err)
	}
	rows = zoneRows(t, st)
	if len(rows) != 1 || rows[0].State != StateArmed || rows[0].Version != 2 {
		t.Fatalf("a never-placed terminal row re-authorizes in place on a new version: %+v", rows)
	}
	if rows[0].EvalPrice != nil || rows[0].PlacedAtMs != nil || rows[0].LastVerdict != "" || rows[0].LastVerdictMs != nil {
		t.Fatalf("re-authorize must clear the prior placement's receipts: %+v", rows[0])
	}
	if rows[0].Policy != ArmPolicyMarketInZone || *rows[0].ZoneLo != 31000 {
		t.Fatalf("re-authorize must carry the new authorization's zone: %+v", rows[0])
	}
}

// BeginPlacementEval = BeginPlacement's compare-and-set + a non-empty policy,
// and it stamps the evidence in the same write.
func TestBeginPlacementEvalCAS(t *testing.T) {
	st := NewArmedOrderStore(newArmedTestDB(t))
	r := zoneRow(1)
	if err := st.UpsertArm(r); err != nil {
		t.Fatal(err)
	}
	if err := st.BeginPlacementEval(r.ID, "", 1, 2, 3); err == nil {
		t.Fatal("an empty signal id must be refused")
	}
	if err := st.BeginPlacementEval(r.ID, "sig-z1", 31011.25, 1_000, 2_000); err != nil {
		t.Fatalf("first stamp: %v", err)
	}
	var got ArmedOrderDB
	if err := st.db.First(&got, r.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.State != StatePlacePending || got.SignalID != "sig-z1" || got.EvalPrice == nil || *got.EvalPrice != 31011.25 ||
		got.EvalBarMs == nil || *got.EvalBarMs != 1_000 || got.PlacedAtMs == nil || *got.PlacedAtMs != 2_000 {
		t.Fatalf("stamp must write signal, place_pending and the evidence: %+v", got)
	}
	if err := st.BeginPlacementEval(r.ID, "sig-z2", 1, 2, 3); err == nil {
		t.Fatal("a second stamp on an in-flight row must be refused (CAS)")
	}
	// A legacy row can never be stamped by it.
	legacy := zoneRow(1)
	legacy.Scenario, legacy.Policy = "S9", ""
	if err := st.UpsertArm(legacy); err != nil {
		t.Fatal(err)
	}
	if err := st.BeginPlacementEval(legacy.ID, "sig-l", 1, 2, 3); err == nil {
		t.Fatal("BeginPlacementEval must refuse a legacy (policy '') row")
	}
	if err := st.BeginPlacement(legacy.ID, "sig-l"); err != nil {
		t.Fatalf("BeginPlacement is unchanged for a legacy row: %v", err)
	}
}

// D15 — a policy row that reached the broker is never re-minted within its
// version; a new version re-arms; a boot-swept row keeps the 0B law.
func TestArmedZoneReArmPinnedWithinVersion(t *testing.T) {
	for _, ending := range []struct{ state, reason string }{
		{StateFilled, "fill@31009.00"},
		{StateCancelled, "zone rest expired"},
		{StateCancelled, WithdrawReasonPrefix + "maintenance job j1"},
	} {
		st := NewArmedOrderStore(newArmedTestDB(t))
		r := zoneRow(3)
		if err := st.UpsertArm(r); err != nil {
			t.Fatal(err)
		}
		if err := st.BeginPlacementEval(r.ID, "sig-p", 31010, 1, 2); err != nil {
			t.Fatal(err)
		}
		if err := st.SetState(r.ID, ending.state, ending.reason); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			if err := st.UpsertArm(zoneRow(3)); err != nil {
				t.Fatalf("%s: same-version re-authorization: %v", ending.reason, err)
			}
		}
		if rows := zoneRows(t, st); len(rows) != 1 {
			t.Fatalf("%s: a same-version re-authorization minted %d rows, want the pinned 1: %+v", ending.reason, len(rows), rows)
		}
		if err := st.UpsertArm(zoneRow(4)); err != nil {
			t.Fatal(err)
		}
		rows := zoneRows(t, st)
		if len(rows) != 2 || rows[1].State != StateArmed || rows[1].PlacementSeq != 1 || rows[1].SignalID != "" ||
			rows[1].EvalPrice != nil || rows[1].PlacedAtMs != nil {
			t.Fatalf("%s: a new version must mint ONE fresh armed placement with no inherited receipts: %+v", ending.reason, rows)
		}
	}
	// A legacy row keeps today's D5 mint (the pin is policy-only).
	st := NewArmedOrderStore(newArmedTestDB(t))
	legacy := zoneRow(3)
	legacy.Policy = ""
	if err := st.UpsertArm(legacy); err != nil {
		t.Fatal(err)
	}
	if err := st.BeginPlacement(legacy.ID, "sig-l"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(legacy.ID, StateFilled, "fill"); err != nil {
		t.Fatal(err)
	}
	l2 := zoneRow(3)
	l2.Policy = ""
	if err := st.UpsertArm(l2); err != nil {
		t.Fatal(err)
	}
	if rows := zoneRows(t, st); len(rows) != 2 {
		t.Fatalf("legacy D5 mint must be unchanged (2 rows), got %d", len(rows))
	}
	// A boot-swept policy row re-arms under the same version (0B).
	st = NewArmedOrderStore(newArmedTestDB(t))
	sw := zoneRow(3)
	if err := st.UpsertArm(sw); err != nil {
		t.Fatal(err)
	}
	if err := st.BeginPlacementEval(sw.ID, "sig-s", 31010, 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(sw.ID, StateCancelled, BootSweepReasonPrefix+": dead process"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertArm(zoneRow(3)); err != nil {
		t.Fatal(err)
	}
	if rows := zoneRows(t, st); len(rows) != 2 {
		t.Fatalf("a boot-swept policy row re-arms under the same version (0B), got %d rows", len(rows))
	}
}

// The receipts: SetFillReceipt writes only policy rows; SetLastVerdict writes
// only on change.
func TestArmedZoneReceiptWriters(t *testing.T) {
	st := NewArmedOrderStore(newArmedTestDB(t))
	r := zoneRow(1)
	if err := st.UpsertArm(r); err != nil {
		t.Fatal(err)
	}
	slip := -16.0
	if err := st.SetFillReceipt(r.ID, 9_000, &slip); err != nil {
		t.Fatal(err)
	}
	legacy := zoneRow(1)
	legacy.Scenario, legacy.Policy = "S9", ""
	if err := st.UpsertArm(legacy); err != nil {
		t.Fatal(err)
	}
	if err := st.SetFillReceipt(legacy.ID, 9_000, &slip); err != nil {
		t.Fatal(err)
	}
	var got, gotL ArmedOrderDB
	_ = st.db.First(&got, r.ID).Error
	_ = st.db.First(&gotL, legacy.ID).Error
	if got.FilledAtMs == nil || *got.FilledAtMs != 9_000 || got.FillSlippageTicks == nil || *got.FillSlippageTicks != -16 {
		t.Fatalf("policy row receipt not written: %+v", got)
	}
	if gotL.FilledAtMs != nil || gotL.FillSlippageTicks != nil {
		t.Fatalf("a legacy row's receipt must stay NULL: %+v", gotL)
	}
	if w, err := st.SetLastVerdict(r.ID, "short_of_zone", 100); err != nil || !w {
		t.Fatalf("first verdict must write (w=%v err=%v)", w, err)
	}
	if w, _ := st.SetLastVerdict(r.ID, "short_of_zone", 200); w {
		t.Fatal("an unchanged verdict must not write")
	}
	_ = st.db.First(&got, r.ID).Error
	if got.LastVerdictMs == nil || *got.LastVerdictMs != 100 {
		t.Fatalf("last_verdict_ms is when the verdict BEGAN: %+v", got.LastVerdictMs)
	}
	if w, _ := st.SetLastVerdict(r.ID, "inside", 300); !w {
		t.Fatal("a changed verdict must write")
	}
}
