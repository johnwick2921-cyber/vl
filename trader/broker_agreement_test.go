package trader

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// m447 broker_agreement pins. Every test drives the PRODUCTION call site:
// datatruth.Evaluate over the check RegisterBrokerAgreement registered, a REAL
// AutoTrader (registerBrokerAgreement, the call Run makes), a REAL store, the
// REAL TCPServer position / order-snapshot caches, and - for the sampling pin -
// the REAL reconcile pass (ReconcilePassForTest = reconcilePositions).

var (
	agreeT0 = time.Date(2026, 10, 26, 10, 0, 0, 0, kernel.CTLocation()) // Monday, CME open
)

// realUnknownOrderJSON is nt8_order_snapshots id 153026 (SimAccount1,
// 2026-10-22 09:20:22 UTC) verbatim: a SELL limit 29458.5 named by a bot-style
// uuid that no armed_orders row and no trader_positions row names. It rested 30
// minutes and then filled into position 687, born from reconcile.
const realUnknownOrderJSON = `[{"order_id":"350c88fd3f2a4768a99a87594376cc78","symbol":"MNQ","name":"2cba226c-ecad-470a-90ad-b2313529c6d2","action":"sell","type":"limit","limit_price":29458.5,"quantity":1,"state":"Working","tif":"Day"}]`

type agreeFx struct {
	t   *testing.T
	at  *AutoTrader
	st  *store.Store
	srv *ntwire.TCPServer
	tr  *ntTrader.TCPTrader
}

func newAgreeFx(t *testing.T) *agreeFx {
	t.Helper()
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	at, st := resetTrader(t, store.StrategyConfig{})
	at.id = "agree-t1"
	s := ntwire.NewTCPServer(nil)
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	tr := ntTrader.NewTCPTrader(s, "MNQ", "Sim101")
	at.trader = tr
	f := &agreeFx{t: t, at: at, st: st, srv: s, tr: tr}
	f.positions() // a known-flat frame
	f.book(`[]`, agreeT0.Add(-5*time.Second))
	at.registerBrokerAgreement()
	t.Cleanup(at.unregisterBrokerAgreement)
	RegisterBrokerAgreement()
	return f
}

func (f *agreeFx) positions(ps ...ntwire.OpenPosition) {
	f.t.Helper()
	f.srv.SeedPositionsForTestAt("Sim101", ps, agreeT0.Add(-5*time.Second))
}

func (f *agreeFx) book(ordersJSON string, at time.Time) {
	f.t.Helper()
	var orders []ntwire.NT8Order
	if err := json.Unmarshal([]byte(ordersJSON), &orders); err != nil {
		f.t.Fatal(err)
	}
	f.srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", BuildID: "t", Orders: orders}, at)
}

func (f *agreeFx) row(side string, qty float64, entry time.Time) int64 {
	f.t.Helper()
	p := &store.TraderPosition{TraderID: f.at.id, ExchangeType: "ninjatrader", ExchangePositionID: "x-" + side,
		Symbol: "MNQ", Side: side, Quantity: qty, EntryQuantity: qty, EntryPrice: 29000, EntryTime: entry.UnixMilli(),
		Leverage: 1, Status: "OPEN", Source: "armed_entry", Account: "Sim101", EntryOrderID: "ee-" + side}
	if err := f.st.Position().CreateOpenPosition(p); err != nil {
		f.t.Fatal(err)
	}
	return p.ID
}

func (f *agreeFx) arm(state, signal string) store.ArmedOrderDB {
	f.t.Helper()
	a := &store.ArmedOrderDB{TraderID: f.at.id, PlanID: "2026-10-26:NY:" + f.at.id, Version: 1, Session: "NY",
		Scenario: "S1", Side: "long", EntryPx: 29000, StopPx: 28990, TargetPx: 29020, State: state, SignalID: signal}
	if err := f.st.ArmedOrders().UpsertArm(a); err != nil {
		f.t.Fatal(err)
	}
	return *a
}

// eval = the production call: the datatruth verdict for MNQ at now.
func (f *agreeFx) eval(now time.Time) (bool, []datatruth.Reason) {
	return datatruth.Evaluate("MNQ", now)
}

func mustClear(t *testing.T, f *agreeFx, now time.Time, what string) {
	t.Helper()
	if b, rs := f.eval(now); b {
		t.Fatalf("%s: must NOT pause entries, got %+v", what, rs)
	}
}

func mustBlock(t *testing.T, f *agreeFx, now time.Time, want ...string) {
	t.Helper()
	b, rs := f.eval(now)
	if !b || len(rs) != 1 || rs[0].Check != CheckBrokerAgreement {
		t.Fatalf("want a broker_agreement block, got blocked=%v %+v", b, rs)
	}
	for _, w := range want {
		if !strings.Contains(rs[0].Detail, w) {
			t.Fatalf("reason %q must say %q", rs[0].Detail, w)
		}
	}
}

// (0) The two views agree: no pause.
func TestBrokerAgreementAgreedViewsDoNotPause(t *testing.T) {
	f := newAgreeFx(t)
	f.row("LONG", 1, agreeT0.Add(-10*time.Minute))
	f.positions(ntwire.OpenPosition{Symbol: "MNQ", Side: "LONG", Quantity: 1, AvgPrice: 29000})
	mustClear(t, f, agreeT0, "agreed")
	later := agreeT0.Add(2 * agreementSettle)
	f.book(`[]`, later.Add(-5*time.Second))
	mustClear(t, f, later, "agreed, later")
}

// (1) NT8 holds a position the bot has no row for (UNTRACKED): settling for the
// reconcile's own 60 s, then entries pause; it clears the moment the books agree.
func TestBrokerAgreementUntrackedPositionPausesAfterSettle(t *testing.T) {
	f := newAgreeFx(t)
	f.positions(ntwire.OpenPosition{Symbol: "MNQ", Side: "SHORT", Quantity: 1, AvgPrice: 29310})
	mustClear(t, f, agreeT0, "first sighting is inside the settle window")
	mustClear(t, f, agreeT0.Add(agreementSettle-time.Second), "still settling")
	mustBlock(t, f, agreeT0.Add(agreementSettle+time.Second), "NT8 holds MNQ SHORT qty 1", "no open row")
	// the books agree again (a row appears): measured clear, entries resume.
	f.row("SHORT", 1, agreeT0.Add(-time.Minute))
	mustClear(t, f, agreeT0.Add(agreementSettle+2*time.Second), "agreed again")
	if st := datatruth.Snapshot()[CheckBrokerAgreement]; st.Blocked || st.Episodes != 1 {
		t.Fatalf("one episode, now closed: %+v", st)
	}
}

// (1b) An NT8 position the LEDGER explains is a fill in flight, not a
// disagreement - the same W0 (c) rule the pre-open reconcile uses. A working arm
// with a signal on that side explains it however long.
func TestBrokerAgreementLedgerExplainedPositionIsNotADisagreement(t *testing.T) {
	f := newAgreeFx(t)
	f.arm(store.StateWorking, "11111111-2222-3333-4444-555555555555")
	f.book(`[{"order_id":"o1","symbol":"MNQ","name":"11111111-2222-3333-4444-555555555555","action":"buy","type":"limit","limit_price":29000,"quantity":1,"state":"Working"}]`, agreeT0.Add(-5*time.Second))
	f.positions(ntwire.OpenPosition{Symbol: "MNQ", Side: "LONG", Quantity: 1, AvgPrice: 29000})
	mustClear(t, f, agreeT0, "ledger explains")
	later := agreeT0.Add(3 * agreementSettle)
	f.book(`[{"order_id":"o1","symbol":"MNQ","name":"11111111-2222-3333-4444-555555555555","action":"buy","type":"limit","limit_price":29000,"quantity":1,"state":"Working"}]`, later.Add(-5*time.Second))
	mustClear(t, f, later, "ledger explains, later")
}

// (2) The bot has an OPEN row, NT8 is flat (ORPHAN) past the reconcile's own
// orphan grace; a row younger than that is waiting for its positions frame.
func TestBrokerAgreementOrphanRowPausesAfterSettle(t *testing.T) {
	f := newAgreeFx(t)
	f.row("LONG", 1, agreeT0.Add(-10*time.Minute))
	mustClear(t, f, agreeT0, "first sighting")
	mustBlock(t, f, agreeT0.Add(agreementSettle+time.Second), "bot has OPEN row", "NT8 holds no LONG")
}

func TestBrokerAgreementFreshRowIsWaitingForItsPositionsFrame(t *testing.T) {
	f := newAgreeFx(t)
	f.row("LONG", 1, agreeT0.Add(-30*time.Second)) // younger than OrphanGraceMs at every probe below
	mustClear(t, f, agreeT0, "a fresh row has not had its positions frame yet")
	mustClear(t, f, agreeT0.Add(time.Duration(ntTrader.OrphanGraceMs-31_000)*time.Millisecond), "still inside the orphan grace")
}

// (3) NT8 and the bot disagree on QUANTITY (the id=45->46 net-2 tell).
func TestBrokerAgreementQuantityDivergencePauses(t *testing.T) {
	f := newAgreeFx(t)
	f.row("LONG", 1, agreeT0.Add(-10*time.Minute))
	f.positions(ntwire.OpenPosition{Symbol: "MNQ", Side: "LONG", Quantity: 2, AvgPrice: 29000})
	mustClear(t, f, agreeT0, "first sighting")
	mustBlock(t, f, agreeT0.Add(agreementSettle+time.Second), "NT8 holds 2 MNQ LONG", "has 1")
}

// (4) The broker has a live ENTRY order nothing of the bot's names - the REAL
// nt8_order_snapshots 153026 order.
func TestBrokerAgreementRealUnknownBrokerOrderPauses(t *testing.T) {
	f := newAgreeFx(t)
	f.book(realUnknownOrderJSON, agreeT0.Add(-5*time.Second))
	mustClear(t, f, agreeT0, "first sighting")
	later := agreeT0.Add(agreementSettle + time.Second)
	f.book(realUnknownOrderJSON, later.Add(-5*time.Second))
	mustBlock(t, f, later, "2cba226c-ecad-470a-90ad-b2313529c6d2", "no ledger row, position or pending entry")
	// once the ledger knows it, the books agree.
	f.arm(store.StateWorking, "2cba226c-ecad-470a-90ad-b2313529c6d2")
	mustClear(t, f, later.Add(time.Second), "ledger now names the order")
}

// (4b) NT8's own "Close" order, a bracket child and a held-locally order are not
// the bot's entry data: no pause.
func TestBrokerAgreementIgnoresNonEntryOrders(t *testing.T) {
	f := newAgreeFx(t)
	orders := `[{"order_id":"a","symbol":"MNQ","name":"Close","action":"sell","type":"market","quantity":1,"state":"Working"},
	 {"order_id":"b","symbol":"MNQ","name":"1065bf48-49c9-486d-a64d-ce303ab9aae6-lx","action":"sell","type":"limit","limit_price":29000,"quantity":1,"state":"Working"},
	 {"order_id":"c","symbol":"MNQ","name":"6b843443-1111-2222-3333-444444444444","action":"buy","type":"limit","limit_price":29000,"quantity":1,"state":"Initialized"}]`
	f.book(orders, agreeT0.Add(-5*time.Second))
	mustClear(t, f, agreeT0, "first")
	later := agreeT0.Add(2 * agreementSettle)
	f.book(orders, later.Add(-5*time.Second))
	mustClear(t, f, later, "Close / bracket child / Initialized are not live bot entries")
}

// (5) The ledger says an arm is WORKING; the broker's fresh book does not list it.
func TestBrokerAgreementLedgerWorkingBrokerAbsentPauses(t *testing.T) {
	f := newAgreeFx(t)
	f.arm(store.StateWorking, "aaaaaaaa-1111-2222-3333-444444444444")
	mustClear(t, f, agreeT0, "first sighting")
	later := agreeT0.Add(agreementSettle + time.Second)
	f.book(`[]`, later.Add(-5*time.Second))
	mustBlock(t, f, later, "ledger says arm", "is working", "does not list it")
}

// (5b) The ledger settled the arm CANCELLED; NT8 still has a live order for it.
func TestBrokerAgreementLedgerCancelledBrokerLivePauses(t *testing.T) {
	f := newAgreeFx(t)
	sig := "bbbbbbbb-1111-2222-3333-444444444444"
	a := f.arm(store.StateWorking, sig)
	if err := f.st.ArmedOrders().SetState(a.ID, store.StateCancelled, "cancelled in NT8"); err != nil {
		t.Fatal(err)
	}
	book := `[{"order_id":"o2","symbol":"MNQ","name":"` + sig + `","action":"buy","type":"limit","limit_price":29000,"quantity":1,"state":"Working"}]`
	f.book(book, agreeT0.Add(-5*time.Second))
	mustClear(t, f, agreeT0, "first sighting")
	later := agreeT0.Add(agreementSettle + time.Second)
	f.book(book, later.Add(-5*time.Second))
	mustBlock(t, f, later, "is cancelled but NT8 still has order", sig)
}

// (6) ACCOUNT BINDING: the bound account is not one NT8 offers; an open row on a
// different account.
func TestBrokerAgreementAccountBindingPauses(t *testing.T) {
	f := newAgreeFx(t)
	f.srv.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim999", IsSim: true}}, "Sim999")
	mustClear(t, f, agreeT0, "first sighting")
	mustBlock(t, f, agreeT0.Add(agreementSettle+time.Second), `bound to account "Sim101"`, "does not offer it")
}

func TestBrokerAgreementRowOnAnotherAccountPauses(t *testing.T) {
	f := newAgreeFx(t)
	if err := f.st.Position().CreateOpenPosition(&store.TraderPosition{TraderID: f.at.id, ExchangeType: "ninjatrader",
		ExchangePositionID: "x-other", Symbol: "MNQ", Side: "LONG", Quantity: 1, EntryQuantity: 1, EntryPrice: 29000,
		EntryTime: agreeT0.Add(-10 * time.Minute).UnixMilli(), Leverage: 1, Status: "OPEN", Source: "armed_entry",
		Account: "SimAccount12"}); err != nil {
		t.Fatal(err)
	}
	f.positions(ntwire.OpenPosition{Symbol: "MNQ", Side: "LONG", Quantity: 1, AvgPrice: 29000})
	mustClear(t, f, agreeT0, "first sighting")
	mustBlock(t, f, agreeT0.Add(agreementSettle+time.Second), `on account "SimAccount12"`)
}

// (7) CANNOT JUDGE = BLOCK, immediately (no settle): no positions frame for the
// account; a stale broker book.
func TestBrokerAgreementNoPositionsFrameFailsClosed(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	at, _ := resetTrader(t, store.StrategyConfig{})
	at.id = "agree-nopos"
	at.trader = ntTrader.NewTCPTrader(ntwire.NewTCPServer(nil), "MNQ", "Sim101") // Sim101 never reported
	at.registerBrokerAgreement()
	t.Cleanup(at.unregisterBrokerAgreement)
	RegisterBrokerAgreement()
	b, rs := datatruth.Evaluate("MNQ", agreeT0)
	if !b || !strings.Contains(rs[0].Detail, "cannot judge (fail closed)") || !strings.Contains(rs[0].Detail, "has not reported a positions frame") {
		t.Fatalf("no positions frame must fail closed at once: %v %+v", b, rs)
	}
}

func TestBrokerAgreementStaleBookFailsClosed(t *testing.T) {
	f := newAgreeFx(t)
	f.book(`[]`, agreeT0.Add(-10*time.Minute))
	b, rs := datatruth.Evaluate("MNQ", agreeT0)
	if !b || !strings.Contains(rs[0].Detail, "order book is stale") {
		t.Fatalf("a stale book must fail closed at once: %v %+v", b, rs)
	}
}

func TestBrokerAgreementNoRegisteredTraderFailsClosed(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	RegisterBrokerAgreement()
	b, rs := datatruth.Evaluate("MNQ", agreeT0)
	if !b || !strings.Contains(rs[0].Detail, "no started NT8 trader is registered") {
		t.Fatalf("no trader to read = cannot judge = block: %v %+v", b, rs)
	}
}

// (8) CME closed: a quiet broker is expected, nothing can fill - not judged.
func TestBrokerAgreementMarketClosedIsNotJudged(t *testing.T) {
	f := newAgreeFx(t)
	sunday := time.Date(2026, 10, 25, 12, 0, 0, 0, kernel.CTLocation())
	f.book(`[]`, sunday.Add(-time.Hour)) // an hour-old book while closed
	f.positions(ntwire.OpenPosition{Symbol: "MNQ", Side: "LONG", Quantity: 1, AvgPrice: 1})
	mustClear(t, f, sunday, "CME closed")
}

// (9) READ-ONLY: a pause never closes, materializes or edits anything - that is
// the reconcile's job, and exits/management never consult this check.
func TestBrokerAgreementIsReadOnly(t *testing.T) {
	f := newAgreeFx(t)
	f.row("LONG", 1, agreeT0.Add(-10*time.Minute))
	f.positions(ntwire.OpenPosition{Symbol: "MNQ", Side: "SHORT", Quantity: 3, AvgPrice: 1})
	mustClear(t, f, agreeT0, "first")
	mustBlock(t, f, agreeT0.Add(agreementSettle+time.Second))
	rows, _ := f.st.Position().GetOpenPositions(f.at.id)
	if len(rows) != 1 || rows[0].Status != "OPEN" || rows[0].Quantity != 1 {
		t.Fatalf("the check must not touch rows: %+v", rows)
	}
}

// (10) SAMPLING ON THE RECONCILE LOOP: the production reconcile pass (not an
// entry attempt) starts a disagreement's clock, so the FIRST Evaluate after the
// settle window already blocks. Without the observer the first Evaluate is the
// first sighting and an entry would slip through.
func TestBrokerAgreementIsSampledByTheReconcilePass(t *testing.T) {
	f := newAgreeFx(t)
	f.positions(ntwire.OpenPosition{Symbol: "MNQ", Side: "SHORT", Quantity: 1, AvgPrice: 29310})
	prev := agreementNow
	agreementNow = func() time.Time { return agreeT0 }
	t.Cleanup(func() { agreementNow = prev })
	f.tr.ReconcilePassForTest(f.at.id, "nt", "ninjatrader", f.st) // the production reconcile pass
	mustBlock(t, f, agreeT0.Add(agreementSettle+time.Second), "NT8 holds MNQ SHORT")
}

// (11) A stopped trader is not a voice: unregistering removes it.
func TestBrokerAgreementStoppedTraderIsNotConsulted(t *testing.T) {
	f := newAgreeFx(t)
	f.positions(ntwire.OpenPosition{Symbol: "MNQ", Side: "SHORT", Quantity: 1, AvgPrice: 29310})
	mustClear(t, f, agreeT0, "first")
	mustBlock(t, f, agreeT0.Add(agreementSettle+time.Second))
	f.at.unregisterBrokerAgreement()
	b, rs := datatruth.Evaluate("MNQ", agreeT0.Add(agreementSettle+2*time.Second))
	if !b || !strings.Contains(rs[0].Detail, "no started NT8 trader is registered") {
		t.Fatalf("a stopped trader leaves nobody to read: %v %+v", b, rs)
	}
}

// (12) The Stop side at the production teardown: teardownRunStarted (what Stop
// and a refused Run call) removes the trader from the check.
func TestBrokerAgreementTeardownRunStartedUnregisters(t *testing.T) {
	f := newAgreeFx(t)
	f.positions(ntwire.OpenPosition{Symbol: "MNQ", Side: "SHORT", Quantity: 1, AvgPrice: 29310})
	mustClear(t, f, agreeT0, "first")
	f.at.teardownRunStarted()
	b, rs := datatruth.Evaluate("MNQ", agreeT0.Add(agreementSettle+time.Second))
	if !b || !strings.Contains(rs[0].Detail, "no started NT8 trader is registered") {
		t.Fatalf("teardownRunStarted must unregister the trader: %v %+v", b, rs)
	}
}

// (13) The Run side: registerBrokerAgreement is called right after
// StartPositionReconcile in Run. Run itself needs a live exchange, so this is a
// SOURCE pin [B]: the call must follow the reconcile start in auto_trader.go.
func TestBrokerAgreementIsRegisteredWhereRunStartsTheReconcile(t *testing.T) {
	b, err := os.ReadFile("auto_trader.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	i := strings.Index(src, "ntTCP.StartPositionReconcile(")
	j := strings.Index(src, "at.registerBrokerAgreement()")
	if i < 0 || j < 0 || j < i || j-i > 600 {
		t.Fatalf("Run must call at.registerBrokerAgreement() right after ntTCP.StartPositionReconcile (i=%d j=%d)", i, j)
	}
}

// m447 review defect 5 (mutant: agreementTracker.apply keeps a cleared key's
// first-seen time). A disagreement that clears and then RECURS is a NEW episode
// and gets a fresh settle window: the second sighting must not inherit the first
// one's clock and block instantly.
func TestBrokerAgreementRecurringDisagreementGetsAFreshSettle(t *testing.T) {
	f := newAgreeFx(t)
	short := ntwire.OpenPosition{Symbol: "MNQ", Side: "SHORT", Quantity: 1, AvgPrice: 29310}
	at := func(d time.Duration) time.Time {
		n := agreeT0.Add(d)
		f.book(`[]`, n.Add(-5*time.Second))
		return n
	}
	f.positions(short)
	mustClear(t, f, at(0), "first sighting")
	f.positions() // NT8 flat again: the disagreement is measured clear
	mustClear(t, f, at(40*time.Second), "cleared")
	f.positions(short) // it recurs 70 s after the FIRST sighting
	mustClear(t, f, at(70*time.Second), "a recurrence starts a fresh settle window, it does not inherit the first sighting's clock")
	mustClear(t, f, at(70*time.Second+agreementSettle-time.Second), "still inside the fresh window")
	mustBlock(t, f, at(70*time.Second+agreementSettle+time.Second), "NT8 holds MNQ SHORT qty 1")
}
