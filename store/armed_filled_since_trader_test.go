package store

import (
	"path/filepath"
	"testing"
	"time"
)

// W1b FOLD-4 — ListFilledSince is ONE trader's FILLED rows from a since bound
// widened by LedgerClockSlack: another trader's fill, a non-filled row and a
// row older than the widened bound are never returned, and a fresh fill written
// in another zone (text sorting before a UTC bound) is never dropped. The
// caller judges the exact window on the parsed UpdatedAt.
func TestListFilledSinceIsTraderScopedAndZoneSafe(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "lfs.db"))
	if err != nil {
		t.Fatal(err)
	}
	led := st.ArmedOrders()
	mk := func(trader, scen, state string) int64 {
		// The plan id carries the trader, as MakePlanIDForTrader does in
		// production — a shared "p" made two traders one ledger slot.
		r := &ArmedOrderDB{TraderID: trader, PlanID: "p-" + trader, Scenario: scen, Version: 1, State: "armed", Side: "long", EntryPx: 100, StopPx: 99, TargetPx: 102}
		if err := led.UpsertArm(r); err != nil {
			t.Fatal(err)
		}
		if err := led.BeginPlacement(r.ID, "sig-"+trader+"-"+scen); err != nil {
			t.Fatal(err)
		}
		if err := led.SetState(r.ID, state, "fixture"); err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	setAt := func(id int64, text string) {
		if err := st.GormDB().Exec("UPDATE armed_orders SET updated_at = ? WHERE id = ?", text, id).Error; err != nil {
			t.Fatal(err)
		}
	}
	const layout = "2006-01-02 15:04:05.999999999-07:00"
	since := time.Now().UTC().Add(-3 * time.Minute)

	fresh := mk("trader-a", "S1", StateFilled)
	setAt(fresh, time.Now().Add(-30*time.Second).UTC().Format(layout))
	freshCT := mk("trader-a", "S2", StateFilled)
	ctText := time.Now().Add(-20 * time.Second).In(time.FixedZone("CT", -5*3600)).Format(layout)
	if ctText >= since.Format(layout) {
		t.Fatalf("fixture: the -05:00 text %q must sort before the UTC bound %q", ctText, since.Format(layout))
	}
	setAt(freshCT, ctText)
	old := mk("trader-a", "S3", StateFilled)
	setAt(old, time.Now().Add(-72*time.Hour).UTC().Format(layout))
	other := mk("trader-b", "S1", StateFilled)
	setAt(other, time.Now().Add(-10*time.Second).UTC().Format(layout))
	working := mk("trader-a", "S4", StateWorking)
	setAt(working, time.Now().UTC().Format(layout))

	rows, err := led.ListFilledSince("trader-a", since)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]time.Time{}
	for _, r := range rows {
		if r.TraderID != "trader-a" || r.State != StateFilled {
			t.Fatalf("returned #%d trader=%q state=%q — must be trader-a's FILLED rows only", r.ID, r.TraderID, r.State)
		}
		got[r.ID] = r.UpdatedAt
	}
	if _, ok := got[fresh]; !ok {
		t.Fatalf("the fresh fill #%d was not returned: %v", fresh, got)
	}
	at, ok := got[freshCT]
	if !ok {
		t.Fatalf("a fresh fill written as %q (-05:00) was dropped by a UTC since bound — LedgerClockSlack must cover the zone gap", ctText)
	}
	if at.Before(since) {
		t.Fatalf("the cross-zone row parses to %v, before since %v — the fixture is not fresh", at, since)
	}
	if _, ok := got[old]; ok {
		t.Fatalf("a fill 72h old is outside even the widened SQL bound: #%d", old)
	}
	if _, ok := got[other]; ok {
		t.Fatalf("another trader's fill #%d must never be returned", other)
	}
}
