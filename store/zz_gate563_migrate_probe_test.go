package store

// GATE #563 probe (reviewer-owned): run the HEAD Migrate on a scratch COPY of
// the live armed_orders table (dumped read-only), never the live DB.

import (
	"os"
	"testing"
	"time"
)

func TestG563MigrateLiveCopyIsAdditive(t *testing.T) {
	p := os.Getenv("G563_MIGDB")
	if p == "" {
		t.Skip("G563_MIGDB not set")
	}
	st, err := New(p)
	if err != nil {
		t.Fatalf("store.New on the copy: %v", err)
	}
	defer st.Close()
	db := st.ArmedOrders().DB()
	var cols []struct {
		Name    string
		Type    string
		NotNull int `gorm:"column:notnull"`
		Dflt    *string `gorm:"column:dflt_value"`
	}
	if err := db.Raw("SELECT name, type, \"notnull\", dflt_value FROM pragma_table_info('armed_orders') WHERE name IN ('leg2_qty','leg2_tp','leg1_qty','leg1_tp')").Scan(&cols).Error; err != nil {
		t.Fatal(err)
	}
	for _, c := range cols {
		d := "NULL"
		if c.Dflt != nil {
			d = *c.Dflt
		}
		t.Logf("column %s %s notnull=%d default=%s", c.Name, c.Type, c.NotNull, d)
	}
	if len(cols) != 4 {
		t.Fatalf("want leg1_qty, leg1_tp, leg2_qty, leg2_tp; got %d columns", len(cols))
	}
	var n, nullLeg2, zeroTP, leg1 int64
	db.Raw("SELECT COUNT(*) FROM armed_orders").Scan(&n)
	db.Raw("SELECT COUNT(*) FROM armed_orders WHERE leg2_qty IS NULL").Scan(&nullLeg2)
	db.Raw("SELECT COUNT(*) FROM armed_orders WHERE leg2_tp = 0").Scan(&zeroTP)
	db.Raw("SELECT COUNT(*) FROM armed_orders WHERE leg1_qty IS NOT NULL").Scan(&leg1)
	t.Logf("rows=%d leg2_qty NULL=%d leg2_tp=0: %d leg1_qty set=%d", n, nullLeg2, zeroTP, leg1)
	if n == 0 || nullLeg2 != n || zeroTP != n {
		t.Fatalf("migration not additive: rows=%d nullLeg2=%d zeroTP=%d", n, nullLeg2, zeroTP)
	}
	var rows []ArmedOrderDB
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("GORM read of every migrated row: %v", err)
	}
	for _, r := range rows {
		if r.Leg2Qty != nil || r.Leg2TP != 0 {
			t.Fatalf("row %d reads leg2 (%v, %.2f) — want absent", r.ID, r.Leg2Qty, r.Leg2TP)
		}
	}
	// idempotent: a second Migrate (a second boot) is a no-op
	if err := st.ArmedOrders().Migrate(); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

// Fresh DB: the DDL + ALTER path yields the same columns, and UpsertArm round-trips leg 2.
func TestG563FreshDBRoundTripsLeg2(t *testing.T) {
	st, err := New(t.TempDir() + "/fresh.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row := &ArmedOrderDB{TraderID: "g", PlanID: "p", Scenario: "s", Side: "long", State: StateArmed,
		EntryPx: 100, StopPx: 98, TargetPx: 106, Contracts: IntPtr(5), Leg1Qty: IntPtr(3), Leg1TP: 102,
		Leg2Qty: IntPtr(1), Leg2TP: 104}
	if err := st.ArmedOrders().UpsertArm(row); err != nil {
		t.Fatal(err)
	}
	var got ArmedOrderDB
	if err := st.ArmedOrders().DB().Where("plan_id = ? AND scenario = ?", "p", "s").First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.Leg2Qty == nil || *got.Leg2Qty != 1 || got.Leg2TP != 104 {
		t.Fatalf("leg 2 not persisted: (%v, %.2f)", got.Leg2Qty, got.Leg2TP)
	}
	// re-authorization drops leg 2 (NULL/0 over 1/104)
	row2 := *row
	row2.ID = 0
	row2.Leg2Qty, row2.Leg2TP = nil, 0
	if err := st.ArmedOrders().UpsertArm(&row2); err != nil {
		t.Fatal(err)
	}
	if err := st.ArmedOrders().DB().Where("plan_id = ? AND scenario = ?", "p", "s").First(&got).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("after re-authorization without leg 2: (%v, %.2f)", got.Leg2Qty, got.Leg2TP)
	if got.Leg2Qty != nil || got.Leg2TP != 0 {
		t.Fatalf("a re-authorization without leg 2 must clear it, got (%v, %.2f)", got.Leg2Qty, got.Leg2TP)
	}
}

// M26 — the NEW-plan-version re-authorization branch must also write leg 2
// (NULL/0 over a stale 1/104). A terminal row re-authorized under a bumped
// version reaches the version-change Updates map; dropping leg2_qty/leg2_tp
// from it leaves the stale partial behind.
func TestG563VersionBumpReauthorizationClearsLeg2(t *testing.T) {
	st, err := New(t.TempDir() + "/fresh2.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()
	row := &ArmedOrderDB{TraderID: "g", PlanID: "p", Scenario: "s", Side: "long", State: StateArmed,
		Version: 1, EntryPx: 100, StopPx: 98, TargetPx: 106, Contracts: IntPtr(5),
		Leg1Qty: IntPtr(3), Leg1TP: 102, Leg2Qty: IntPtr(1), Leg2TP: 104,
		CreatedAt: now, UpdatedAt: now}
	if err := st.ArmedOrders().UpsertArm(row); err != nil {
		t.Fatal(err)
	}
	if err := st.ArmedOrders().SetState(row.ID, StateCancelled, "owner manual cancel"); err != nil {
		t.Fatal(err)
	}
	next := &ArmedOrderDB{TraderID: "g", PlanID: "p", Scenario: "s", Side: "long", State: StateArmed,
		Version: 2, EntryPx: 100, StopPx: 98, TargetPx: 106, Contracts: IntPtr(5),
		Leg1Qty: IntPtr(3), Leg1TP: 102, CreatedAt: now, UpdatedAt: now}
	if err := st.ArmedOrders().UpsertArm(next); err != nil {
		t.Fatal(err)
	}
	var got ArmedOrderDB
	if err := st.ArmedOrders().DB().Where("id = ?", row.ID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.State != StateArmed || got.Version != 2 {
		t.Fatalf("state=%q version=%d, want armed v2 (version bump re-authorizes)", got.State, got.Version)
	}
	if got.Leg2Qty != nil || got.Leg2TP != 0 {
		t.Fatalf("a version-bump re-authorization without leg 2 must clear it, got (%v, %.2f)", got.Leg2Qty, got.Leg2TP)
	}
}
