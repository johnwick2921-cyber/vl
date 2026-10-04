package trader

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	nt "vl/provider/ninjatrader"
	"vl/store"
	"vl/store/sqlitedriver"
)

// ── CANCEL-REPORT REGIME (2026-10-03) — trader half ─────────────────────────
//
// The pure seams tested here are the production call sites' decision cores:
// slotReportBlock is armSlotGuard's FIRST check (before the book),
// cancelReportQualifies is confirmPendingCancelsReport's settle predicate,
// cancelCensusLine is the census WARN, cancelConfirmRequireReport is the L4
// knob. The final test drives confirmPendingCancelsReport itself against an
// in-memory ledger — the production settlement path.

func TestCancelConfirmRequireReportKnobDefaultsOff(t *testing.T) {
	t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", "")
	if cancelConfirmRequireReport() {
		t.Fatalf("the report regime must default OFF")
	}
	for _, v := range []string{"1", "true", "on", "yes", "TRUE"} {
		t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", v)
		if !cancelConfirmRequireReport() {
			t.Fatalf("the report regime must be ON for %q", v)
		}
	}
	t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", "0")
	if cancelConfirmRequireReport() {
		t.Fatalf(`the report regime must be OFF for "0"`)
	}
}

func pendingRow(signalID string, requestMs, reportMs int64, reportState string) store.ArmedOrderDB {
	return store.ArmedOrderDB{
		State: store.StateCancelPending, SignalID: signalID,
		CancelRequestedAtMs: requestMs,
		CancelReportMs:      reportMs, CancelReportState: reportState,
	}
}

func TestCancelReportQualifies(t *testing.T) {
	cases := []struct {
		name string
		r    store.ArmedOrderDB
		want bool
		why  string
	}{
		{"post-request cancelled report", pendingRow("sig-a", 1000, 1500, "cancelled"), true, "reported cancelled"},
		{"no report at all", pendingRow("sig-a", 1000, 0, ""), false, "no AddOn terminal report"},
		{"filled report is the filled path's word, not a cancellation", pendingRow("sig-a", 1000, 1500, "filled"), false, "not cancelled"},
		{"report predates the request", pendingRow("sig-a", 2000, 1000, "cancelled"), false, "predates"},
		{"undated request cannot be proven", pendingRow("sig-a", 0, 1000, "cancelled"), false, "undated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, why := cancelReportQualifies(tc.r)
			if ok != tc.want {
				t.Fatalf("want ok=%v, got ok=%v (why=%q)", tc.want, ok, why)
			}
			if !strings.Contains(why, tc.why) {
				t.Fatalf("why %q must mention %q", why, tc.why)
			}
		})
	}
}

func TestSlotReportBlock(t *testing.T) {
	sigs := []string{"sig-a"}
	t.Run("knob OFF never blocks", func(t *testing.T) {
		rows := []store.ArmedOrderDB{pendingRow("sig-a", 1000, 0, "")}
		if blocked, _ := slotReportBlock(false, false, rows, sigs); blocked {
			t.Fatalf("with the knob OFF the regime must not block anything")
		}
	})
	t.Run("uncertified AddOn blocks every slot fail-closed", func(t *testing.T) {
		blocked, why := slotReportBlock(true, false, nil, sigs)
		if !blocked || !strings.Contains(why, "fail-closed") {
			t.Fatalf("an AddOn below the floor must fail closed, got blocked=%v why=%q", blocked, why)
		}
	})
	t.Run("unconfirmed cancel keeps the slot busy", func(t *testing.T) {
		rows := []store.ArmedOrderDB{pendingRow("sig-a", 1000, 0, "")}
		blocked, why := slotReportBlock(true, true, rows, sigs)
		if !blocked || !strings.Contains(why, "no AddOn terminal report") {
			t.Fatalf("an unconfirmed cancel must keep the slot busy, got blocked=%v why=%q", blocked, why)
		}
	})
	t.Run("a qualifying report frees the slot", func(t *testing.T) {
		rows := []store.ArmedOrderDB{pendingRow("sig-a", 1000, 1500, "cancelled")}
		if blocked, why := slotReportBlock(true, true, rows, sigs); blocked {
			t.Fatalf("a qualifying report must free the slot, got %q", why)
		}
	})
	t.Run("a predating report keeps the slot busy", func(t *testing.T) {
		rows := []store.ArmedOrderDB{pendingRow("sig-a", 2000, 1000, "cancelled")}
		if blocked, _ := slotReportBlock(true, true, rows, sigs); !blocked {
			t.Fatalf("a report that predates the request must not free the slot")
		}
	})
	t.Run("another slot's pending cancel does not block this slot", func(t *testing.T) {
		rows := []store.ArmedOrderDB{pendingRow("sig-b", 1000, 0, "")}
		if blocked, _ := slotReportBlock(true, true, rows, sigs); blocked {
			t.Fatalf("a pending cancel for a different signal must not block this slot")
		}
	})
}

// THE MUTANT THIS PINS (dispatch: "free on send → RED"). The 2026-09-06 wave's
// defect was call sites treating `cerr == nil` as proof the order was gone.
// Here the send succeeded (no error to read), the broker book is EMPTY and
// FRESH — the snapshot pass calls that free — and the regime's guard STILL
// refuses, because the only settlement evidence is the AddOn's report. If a
// caller "fixes" slotReportBlock to free the slot on a successful send or an
// empty book, this goes RED.
func TestFreeOnSendIsNotFreeWithoutAReport(t *testing.T) {
	rows := []store.ArmedOrderDB{pendingRow("sig-a", 1000, 0, "")} // send returned nil; no report ever came
	book := []nt.NT8Order{}
	v := adjudicateSlot(book, true, time.Second, bookBound, 77, []string{"sig-a"})
	if !v.Allowed() {
		t.Fatalf("fixture broken: an empty FRESH book is free under the snapshot rule, got %+v", v)
	}
	if blocked, why := slotReportBlock(true, true, rows, []string{"sig-a"}); !blocked || !strings.Contains(why, "no AddOn terminal report") {
		t.Fatalf("send-ok + empty book must NOT free an unconfirmed slot; blocked=%v why=%q", blocked, why)
	}
}

func TestCancelCensusLineNamesEveryId(t *testing.T) {
	now := time.Unix(10_000, 0)
	rows := []store.ArmedOrderDB{
		{ID: 41, SignalID: "sig-a", CancelRequestedAtMs: now.Add(-2 * time.Minute).UnixMilli(), CancelAttempts: 3},
		{ID: 42, SignalID: "sig-b", CancelRequestedAtMs: 0, CancelAttempts: 1},
	}
	line := cancelCensusLine(rows, now)
	for _, want := range []string{"2 cancel(s)", "id=41", "sig-a", "id=42", "sig-b", "age=n/a", "attempts=3", "NOT promoted", "slots stay busy"} {
		if !strings.Contains(line, want) {
			t.Fatalf("census line missing %q:\n%s", want, line)
		}
	}
}

func TestCancelBootLineReportsTheRegimeItReads(t *testing.T) {
	t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", "")
	if line := CancelBootLine(nil, ReconcileCounts{}, 0, ""); !strings.Contains(line, "report-regime=off") {
		t.Fatalf("knob OFF must read off:\n%s", line)
	}
	t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", "1")
	if line := CancelBootLine(nil, ReconcileCounts{}, 0, ""); !strings.Contains(line, "addon-proof=n/a") {
		t.Fatalf("no build yet must read n/a, never a fabricated proof:\n%s", line)
	}
	if line := CancelBootLine(nil, ReconcileCounts{}, 0, "2026-09-30-m22"); !strings.Contains(line, "addon-proof=NO") {
		t.Fatalf("an old build must read NO and fail closed:\n%s", line)
	}
	if line := CancelBootLine(nil, ReconcileCounts{}, 0, nt.MinAddonBuildCancelReport); !strings.Contains(line, "addon-proof=yes") {
		t.Fatalf("a certified build must read yes:\n%s", line)
	}
	t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", "")
}

// ── THE PRODUCTION SETTLEMENT PATH ──────────────────────────────────────────

func newReportLedger(t *testing.T) (*store.ArmedOrderStore, func(string) error, func() int) {
	t.Helper()
	db, err := gorm.Open(sqlitedriver.GormDialector(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	ledger := store.NewArmedOrderStore(db)
	if err := ledger.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sends := 0
	cancelFn := func(signalID string) error {
		sends++
		return nil // the send ALWAYS succeeds — the point of the regime
	}
	return ledger, cancelFn, func() int { return sends }
}

func seedPendingRow(t *testing.T, ledger *store.ArmedOrderStore, requestMs int64) *store.ArmedOrderDB {
	t.Helper()
	now := time.Now()
	r := &store.ArmedOrderDB{
		TraderID: "t1", PlanID: "2026-10-03:planX", Version: 1, Session: "RTH",
		Scenario: "S1", Side: "long", EntryPx: 100, StopPx: 99, TargetPx: 102,
		State: "armed", CreatedAt: now, UpdatedAt: now,
	}
	if err := ledger.UpsertArm(r); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := ledger.SetSignal(r.ID, "sig-a"); err != nil {
		t.Fatalf("set signal: %v", err)
	}
	if err := ledger.RequestCancel(r.ID, "gate changed", requestMs); err != nil {
		t.Fatalf("request cancel: %v", err)
	}
	return r
}

func pendingRows(t *testing.T, ledger *store.ArmedOrderStore) []store.ArmedOrderDB {
	t.Helper()
	rows, err := ledger.ListCancelPending("t1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	return rows
}

// THE DISPATCH'S TWO SIDES IN ONE TEST: a cancel whose send succeeded but that
// has NO report is NOT done — the row stays cancel_pending, the slot stays
// busy, nothing re-arms (the free-on-send mutant would settle here and go
// RED). The SAME row, once the AddOn's positive report is recorded, settles
// with the original reason preserved.
func TestConfirmPendingCancelsReportSettlesOnlyOnTheReport(t *testing.T) {
	ledger, cancelFn, sends := newReportLedger(t)
	at := &AutoTrader{id: "t1"}
	reqMs := time.Now().UnixMilli()
	r := seedPendingRow(t, ledger, reqMs)
	now := time.UnixMilli(reqMs + 30_000)

	// Act 1 — send succeeded, no report: NOT settled, and NOT silently
	// re-placed. The row stays cancel_pending.
	settled, still, reReq := at.confirmPendingCancelsReport(ledger, cancelFn, pendingRows(t, ledger), now, time.Minute, 5, nt.MinAddonBuildCancelReport)
	if settled != 0 || still != 1 || reReq != 0 {
		t.Fatalf("no report must settle nothing: settled=%d still=%d reRequested=%d", settled, still, reReq)
	}
	if row := pendingRows(t, ledger); len(row) != 1 || row[0].State != store.StateCancelPending {
		t.Fatalf("the row must stay cancel_pending without a report, got %+v", row)
	}
	if sends() != 0 {
		t.Fatalf("inside the timeout window nothing may be re-requested, got %d sends", sends())
	}
	// The slot is busy while the cancel is unconfirmed — armSlotGuard's first
	// check refuses even though the book would be empty.
	rows := pendingRows(t, ledger)
	rows[0].ID = r.ID
	if blocked, why := slotReportBlock(true, true, rows, []string{"sig-a"}); !blocked || !strings.Contains(why, "no AddOn terminal report") {
		t.Fatalf("slot must stay busy without a report, blocked=%v why=%q", blocked, why)
	}

	// Act 2 — the AddOn reports the order cancelled for this order id.
	if err := ledger.RecordCancelReport(r.ID, reqMs+5_000, "cancelled"); err != nil {
		t.Fatalf("record report: %v", err)
	}
	settled, still, reReq = at.confirmPendingCancelsReport(ledger, cancelFn, pendingRows(t, ledger), now, time.Minute, 5, nt.MinAddonBuildCancelReport)
	if settled != 1 || still != 0 || reReq != 0 {
		t.Fatalf("a qualifying report must settle exactly one row: settled=%d still=%d reRequested=%d", settled, still, reReq)
	}
	var got *store.ArmedOrderDB
	var err error
	if got, err = ledger.FindBySignal("t1", "sig-a"); err != nil || got == nil {
		t.Fatalf("read back: %v", err)
	}
	if got.State != store.StateCancelled {
		t.Fatalf("want cancelled, got %q", got.State)
	}
	if !strings.Contains(got.StateReason, "gate changed") {
		t.Fatalf("the original cancel reason must survive the confirmation, got %q", got.StateReason)
	}
	if !strings.Contains(got.StateReason, "reported cancelled") {
		t.Fatalf("the confirmation must cite the report, got %q", got.StateReason)
	}
}

// An AddOn below the floor confirms NOTHING: fail-closed, no promotion, no
// re-arm, no re-request — and the owner-visible WARN names the build.
func TestConfirmPendingCancelsReportFailClosedOnOldAddOn(t *testing.T) {
	ledger, cancelFn, sends := newReportLedger(t)
	at := &AutoTrader{id: "t1"}
	reqMs := time.Now().UnixMilli()
	seedPendingRow(t, ledger, reqMs)
	settled, still, reReq := at.confirmPendingCancelsReport(ledger, cancelFn, pendingRows(t, ledger), time.UnixMilli(reqMs+300_000), time.Minute, 5, "2026-09-30-m22")
	if settled != 0 || still != 1 || reReq != 0 {
		t.Fatalf("an old AddOn must settle and re-request nothing: settled=%d still=%d reRequested=%d", settled, still, reReq)
	}
	if sends() != 0 {
		t.Fatalf("an old AddOn must not be re-requested against, got %d sends", sends())
	}
	if row := pendingRows(t, ledger); len(row) != 1 || row[0].State != store.StateCancelPending {
		t.Fatalf("the row must stay cancel_pending on an old AddOn, got %+v", row)
	}
}

// Timeout → census + capped re-request, never a promotion.
func TestConfirmPendingCancelsReportTimeoutCensusAndRerequest(t *testing.T) {
	ledger, cancelFn, sends := newReportLedger(t)
	at := &AutoTrader{id: "t1"}
	reqMs := time.Now().UnixMilli()
	seedPendingRow(t, ledger, reqMs)
	now := time.UnixMilli(reqMs + 2*60_000) // 2 min later, past the 1-min timeout
	settled, still, reReq := at.confirmPendingCancelsReport(ledger, cancelFn, pendingRows(t, ledger), now, time.Minute, 5, nt.MinAddonBuildCancelReport)
	if settled != 0 || still != 1 || reReq != 1 {
		t.Fatalf("past the timeout the row must be re-requested, never promoted: settled=%d still=%d reRequested=%d", settled, still, reReq)
	}
	if sends() != 1 {
		t.Fatalf("want exactly one re-request, got %d", sends())
	}
	// The re-request is recorded on the row, and the row STAYS cancel_pending.
	rows := pendingRows(t, ledger)
	if len(rows) != 1 || rows[0].State != store.StateCancelPending {
		t.Fatalf("the row must stay cancel_pending, got %+v", rows)
	}
	if rows[0].CancelAttempts < 1 {
		t.Fatalf("the re-request must be counted, got attempts=%d", rows[0].CancelAttempts)
	}
}

// ── REVIEW r2 MUTANT PINS (M6, M9) AT THE PRODUCTION CALL SITES ─────────────

// M6 pin: remove the knob-OFF echo no-op in onArmedOrderUpdate and a
// cancel-report echo settles a cancel_pending row through SetState while the
// regime is OFF — the exact bypass the regime exists to remove. The echo is
// a report, never a state event.
func TestEchoIsANoOpWithTheRegimeOFF(t *testing.T) {
	t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", "")
	if cancelConfirmRequireReport() {
		t.Fatalf("fixture: the regime must be OFF")
	}
	ledger, _, _ := newReportLedger(t)
	at := &AutoTrader{id: "t1"}
	seedPendingRow(t, ledger, time.Now().UnixMilli())
	at.onArmedOrderUpdate(nt.OrderUpdatePayload{
		OrderName: "sig-a", SignalID: "sig-a", State: "cancelled", CancelReport: true,
	}, ledger)
	rows := pendingRows(t, ledger)
	if len(rows) != 1 || rows[0].State != store.StateCancelPending {
		t.Fatalf("an echo with the regime OFF must be a complete no-op, got %+v", rows)
	}
	if rows[0].CancelReportMs != 0 || rows[0].CancelReportState != "" {
		t.Fatalf("an echo with the regime OFF must not record a report, got ms=%d state=%q", rows[0].CancelReportMs, rows[0].CancelReportState)
	}
}

// M9 pin: remove the settledIDs skip and a row the pass just settled is
// re-requested (resurrected to cancel_pending) by the census loop. cancelFn
// must fire EXACTLY once — for the still-pending row only.
func TestReportPassNeverResurrectsSettledRows(t *testing.T) {
	ledger, cancelFn, sends := newReportLedger(t)
	at := &AutoTrader{id: "t1"}
	reqMs := time.Now().UnixMilli()
	// Row A: qualifies for settlement AND is past the timeout.
	a := seedPendingRow(t, ledger, reqMs)
	if err := ledger.RecordCancelReport(a.ID, reqMs+5_000, "cancelled"); err != nil {
		t.Fatalf("record report A: %v", err)
	}
	// Row B: no report, past the timeout — the one re-request is for B.
	seedPendingRowB := &store.ArmedOrderDB{
		TraderID: "t1", PlanID: "2026-10-03:planY", Version: 1, Session: "RTH",
		Scenario: "S2", Side: "short", EntryPx: 100, StopPx: 101, TargetPx: 98,
		State: "armed", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := ledger.UpsertArm(seedPendingRowB); err != nil {
		t.Fatalf("upsert B: %v", err)
	}
	if err := ledger.SetSignal(seedPendingRowB.ID, "sig-b"); err != nil {
		t.Fatalf("signal B: %v", err)
	}
	if err := ledger.RequestCancel(seedPendingRowB.ID, "gate changed", reqMs); err != nil {
		t.Fatalf("request B: %v", err)
	}
	now := time.UnixMilli(reqMs + 2*60_000) // both rows past the 1-min timeout
	settled, still, reReq := at.confirmPendingCancelsReport(ledger, cancelFn, pendingRows(t, ledger), now, time.Minute, 5, nt.MinAddonBuildCancelReport)
	if settled != 1 || still != 1 || reReq != 1 {
		t.Fatalf("want settled=1 still=1 reRequested=1, got settled=%d still=%d reRequested=%d", settled, still, reReq)
	}
	if sends() != 1 {
		t.Fatalf("the only re-request must be for the still-pending row, got %d sends", sends())
	}
	// A must be settled, B still pending — never resurrected.
	for _, r := range pendingRows(t, ledger) {
		if r.SignalID == "sig-a" {
			t.Fatalf("settled row A was resurrected: %+v", r)
		}
	}
}

// ── REVIEW-312 r3 MUTANT PINS AT THE PRODUCTION CALL SITES (P1-2) ────────────
//
// M3/M4/M5 stayed GREEN in the review because the round-2 call-site pins were
// not carried into this split. Each pin below drives the PRODUCTION call site —
// not the pure helper — so removing the guarded call goes RED.

// M3 pin: delete the slotReportBlock call inside armSlotGuard. The guard must
// refuse on an unconfirmed cancel BEFORE the broker book is even consulted.
func TestArmSlotGuardReportBlockIsACallSite(t *testing.T) {
	t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", "1")
	defer t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", "")
	at := &AutoTrader{id: "t1"}
	r := store.ArmedOrderDB{TraderID: "t1", PlanID: "2026-10-03:planX", Scenario: "S1", LegIndex: 0, SignalID: "sig-a"}
	ledgerRows := []store.ArmedOrderDB{{
		TraderID: "t1", PlanID: "2026-10-03:planX", Scenario: "S1", LegIndex: 0,
		SignalID: "sig-a", State: store.StateCancelPending, CancelRequestedAtMs: 1000,
	}}
	v := at.armSlotGuard(ledgerRows, r, time.Now())
	if v.Allowed() {
		t.Fatalf("the guard must refuse a slot with an unconfirmed cancel, got %+v", v)
	}
	if !v.ReportRegime {
		t.Fatalf("the refusal must be the report regime's own, got %+v", v)
	}
	if !strings.Contains(v.Why, "terminal report") && !strings.Contains(v.Why, "certify") {
		t.Fatalf("the refusal must name the missing report or the uncertified AddOn, got %q", v.Why)
	}
}

// M4 pin: delete the RecordCancelReport branch in onArmedOrderUpdate. A
// `cancelled` order_update for a cancel_pending row must RECORD the report and
// leave the settlement to the pass — a direct SetState(cancelled) is the
// bypass the regime exists to remove.
func TestOnArmedOrderUpdateRecordsTheReportNotTheSettlement(t *testing.T) {
	t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", "1")
	defer t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", "")
	ledger, _, _ := newReportLedger(t)
	at := &AutoTrader{id: "t1"}
	seedPendingRow(t, ledger, time.Now().UnixMilli())
	at.onArmedOrderUpdate(nt.OrderUpdatePayload{
		OrderName: "sig-a", SignalID: "sig-a", State: "cancelled",
	}, ledger)
	rows := pendingRows(t, ledger)
	if len(rows) != 1 || rows[0].State != store.StateCancelPending {
		t.Fatalf("the event must NOT settle the row — the settlement pass is the single writer, got %+v", rows)
	}
	if rows[0].CancelReportMs <= 0 || rows[0].CancelReportState != "cancelled" {
		t.Fatalf("the event must RECORD the report, got ms=%d state=%q", rows[0].CancelReportMs, rows[0].CancelReportState)
	}
}

// M5 pin: delete the dispatch from confirmPendingCancels to the report pass.
// With the knob ON and a qualifying report recorded, a cancel settles EVEN
// THOUGH the broker book still lists the order — the snapshot pass would not
// settle it. If the dispatch is removed this goes RED.
func TestConfirmPendingCancelsDispatchesToTheReportPass(t *testing.T) {
	t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", "1")
	defer t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", "")
	dbPath := filepath.Join(t.TempDir(), "t.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// The book STILL LISTS the order as working — snapshot absence cannot
	// settle this, only the recorded report can.
	book := []nt.NT8Order{{OrderID: "o1", Name: "sig-a", State: "Working", Type: "stop"}}
	bookJSON, err := json.Marshal(book)
	if err != nil {
		t.Fatalf("marshal book: %v", err)
	}
	if err := st.NT8OrderSnapshots().Insert(&store.NT8OrderSnapshot{
		Account: "", Symbol: "", Reason: "test", OrdersJSON: string(bookJSON),
		ReceivedMs: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	ledger := st.ArmedOrders()
	reqMs := time.Now().UnixMilli()
	seedPendingRow(t, ledger, reqMs)
	if err := ledger.RecordCancelReport(pendingRows(t, ledger)[0].ID, reqMs+5_000, "cancelled"); err != nil {
		t.Fatalf("record report: %v", err)
	}
	at := &AutoTrader{id: "t1", store: st}
	oldBuild := cancelReportBuildIDFor
	cancelReportBuildIDFor = func(*AutoTrader) string { return nt.MinAddonBuildCancelReport }
	defer func() { cancelReportBuildIDFor = oldBuild }()
	settled, still, reReq := at.confirmPendingCancels(ledger, func(sid string) error { return nil }, time.UnixMilli(reqMs+30_000))
	if settled != 1 || still != 0 || reReq != 0 {
		t.Fatalf("the report pass must settle on the recorded report: settled=%d still=%d reRequested=%d", settled, still, reReq)
	}
	if got := pendingRows(t, ledger); len(got) != 0 {
		t.Fatalf("the row must be settled, got %+v", got)
	}
}
