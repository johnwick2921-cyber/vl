package trader

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nofx/store"
)

// E3 — A CANCEL THAT CANNOT BE CONFIRMED IS NEVER PROMOTED.
//
// This is the branch the old code got wrong in the most consequential way:
// cancelArmedOrdersSyncWith wrote "cancelled" on ACK TIMEOUT with the reason
// "(ack timeout — flatten proceeds)". UNKNOWN took the destructive branch, and
// 'cancelled' is destructive here because it is what unlocks a replacement.
//
// With no broker book at all — the harshest case, and the live case at
// 2026-09-06 00:30 CT when the AddOn had stopped sending — the row must stay
// cancel_pending, be re-requested, honour its cap, and NEVER become cancelled.
func TestUnconfirmableCancelIsNeverPromoted(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "settle.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ledger := st.ArmedOrders()

	row := &store.ArmedOrderDB{
		TraderID: "hoang", PlanID: "2026-09-06:NY", Version: 1, Session: "NY",
		Scenario: "S2", Side: "short", EntryPx: 29720, StopPx: 29755, TargetPx: 29635,
		State: store.StateArmed,
	}
	if err := ledger.UpsertArm(row); err != nil {
		t.Fatal(err)
	}
	_ = ledger.SetState(row.ID, store.StateWorking, "")
	_ = ledger.SetSignal(row.ID, "sig-unconfirmable")

	at := &AutoTrader{id: "hoang", store: st}
	t0 := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	if err := ledger.RequestCancel(row.ID, "gate changed", t0.UnixMilli()); err != nil {
		t.Fatal(err)
	}

	sends := 0
	fake := func(string) error { sends++; return nil }

	// Inside the window: nothing said, nothing sent, nothing promoted.
	settled, pending, re := at.confirmPendingCancels(ledger, fake, t0.Add(10*time.Second))
	if settled != 0 || pending != 1 || re != 0 {
		t.Fatalf("inside the timeout: want settled=0 pending=1 rerequested=0, got %d/%d/%d", settled, pending, re)
	}

	// Past the timeout, repeatedly. It must re-request up to the cap and stop.
	at2 := at
	for i := 0; i < 10; i++ {
		at2.confirmPendingCancels(ledger, fake, t0.Add(time.Duration(120+i*120)*time.Second))
	}
	var got store.ArmedOrderDB
	if err := st.GormDB().First(&got, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.State != store.StateCancelPending {
		t.Fatalf("an unconfirmable cancel must STAY %q — it was promoted to %q, which is exactly the 'ack timeout — flatten proceeds' defect",
			store.StateCancelPending, got.State)
	}
	if got.CancelSettledSnapshotID != 0 {
		t.Fatalf("nothing settled it, so no snapshot may be recorded; got %d", got.CancelSettledSnapshotID)
	}
	if got.CancelRequestedAtMs != t0.UnixMilli() {
		t.Fatalf("the age must still measure from the FIRST request; got %d want %d", got.CancelRequestedAtMs, t0.UnixMilli())
	}
	if cap := cancelReRequestMax(); got.CancelAttempts > cap+1 {
		t.Fatalf("re-requests must honour the cap %d, attempts reached %d", cap, got.CancelAttempts)
	}
	if sends == 0 {
		t.Fatal("a cancel past its timeout must be RE-REQUESTED, no send was made")
	}
	t.Logf("after 10 cycles with no book: state=%s attempts=%d sends=%d snapshot=%d",
		got.State, got.CancelAttempts, sends, got.CancelSettledSnapshotID)
}

// A10 / class 23 — the settlement pass is telemetry. A nil ledger, a nil wire
// and a trader with no store must all WARN-or-return, never panic.
func TestSettlementPassNeverPanics(t *testing.T) {
	bare := &AutoTrader{id: "hoang"}
	if r := recoverOf(func() { bare.confirmPendingCancels(nil, nil, time.Now()) }); r != nil {
		t.Fatalf("settlement pass panicked with no store: %v", r)
	}
	st, err := store.New(filepath.Join(t.TempDir(), "safe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := &AutoTrader{id: "hoang", store: st}
	if r := recoverOf(func() { at.confirmPendingCancels(st.ArmedOrders(), nil, time.Now()) }); r != nil {
		t.Fatalf("settlement pass panicked with no wire: %v", r)
	}
}

// OWNER RULING 2026-09-06 — A DARK ADDON IS AN OUTAGE, NOT A QUIET DAY.
// Exactly ONE P0 per outage, carrying the book age; cleared when the book
// returns; a LATER outage raises a new one.
func TestDarkBookRaisesOneP0PerOutageAndClearsOnRecovery(t *testing.T) {
	ResetBookOutageForTest()
	st, err := store.New(filepath.Join(t.TempDir(), "outage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := &AutoTrader{
		id: "hoang", exchange: "ninjatrader", store: st,
		config: AutoTraderConfig{NinjaTraderSymbol: "MNQ", StrategyConfig: &store.StrategyConfig{
			DayPlan: &store.DayPlanConfig{PlanEnabled: true},
		}},
	}
	r := store.ArmedOrderDB{ID: 1, PlanID: "P", Scenario: "S2", Side: "SHORT"}
	stale := slotVerdict{Action: slotUnverifiable, Why: "book is 30m0s old", BookAge: 30 * time.Minute}
	now := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)

	// Many refusals across one outage — one alert.
	for i := 0; i < 5; i++ {
		at.refuseSlot(r, stale, "limit", now.Add(time.Duration(i)*time.Minute))
	}
	alerts, err := st.Alert().List("hoang", 50)
	if err != nil {
		t.Fatal(err)
	}
	p0 := 0
	for _, a := range alerts {
		if a.Level == "P0" && a.Kind == "broker_book" {
			p0++
			if !strings.Contains(a.Body, "30m0s") {
				t.Fatalf("the alert must carry the book age, body was: %s", a.Body)
			}
		}
	}
	if p0 != 1 {
		t.Fatalf("five refusals inside ONE outage must raise exactly one P0, got %d", p0)
	}

	// The book returns → the alert is acked (banner cleared).
	unackedBefore, _ := st.Alert().UnackedCount("hoang")
	if unackedBefore == 0 {
		t.Fatal("precondition: the P0 should be unacked while the outage is open")
	}
	// liveBook needs a broker link; with none, clearBookOutageIfHealthy must be
	// a no-op — an absent book is NOT a recovery.
	at.clearBookOutageIfHealthy(now.Add(10 * time.Minute))
	if _, stillOpen := bookOutageSince.Load("hoang"); !stillOpen {
		t.Fatal("an ABSENT book must not be mistaken for a recovered one")
	}

	// Simulate recovery by clearing the latch the way a fresh book would, then
	// prove a SECOND outage raises a SECOND alert (not deduped against the first).
	bookOutageSince.Delete("hoang")
	_, _ = st.Alert().AckByEvent("hoang", bookOutageEventID(now.UnixMilli()))
	at.refuseSlot(r, stale, "limit", now.Add(2*time.Hour))
	alerts, _ = st.Alert().List("hoang", 50)
	p0 = 0
	for _, a := range alerts {
		if a.Level == "P0" && a.Kind == "broker_book" {
			p0++
		}
	}
	if p0 != 2 {
		t.Fatalf("a SECOND outage must raise its own alert, got %d P0(s) total", p0)
	}
	t.Logf("one outage → 1 P0 with the age; a later outage → a second, independent P0")
}
