// FLAT MEANS FLAT (2026-09-09, dispatch 104 D1).
//
// The session close must leave the BOOK flat, not merely the position. The
// research's "do not" is explicit: "do not assume a 14:45 CT exit prevents
// trend-day damage… Canceling remaining entries is part of being flat."
//
// The S-LIST CLOSER (2026-08-27) already cancels WORKING arms before flattening,
// and TestSListEODFlatCancelsArmsBeforeFlatten pins that. These two pins cover
// the holes it left, both of which leave a live order at the broker while our
// ledger believes the book is flat.

package trader

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// flatFixture builds the EOD scene with independent control over whether a
// POSITION exists and what STATE the arm is in — the two axes the existing
// fixture holds constant.
func flatFixture(t *testing.T, now time.Time, withPosition bool, armState, signalID string,
	acks <-chan ntwire.OrderUpdatePayload) (*AutoTrader, *wireRecorder) {
	t.Helper()
	offset := 15
	trueV := true
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{
		PlanEnabled: true,
		Sessions:    []store.DayPlanSessionOverride{{Session: "NY", Enable: &trueV, EODFlatOffsetMin: &offset}},
	}}
	at, st := resetTrader(t, cfg)
	rt := &wireRecorder{MockTrader: &MockTrader{}}
	at.trader = rt
	at.armedSyncSeam = &armedSyncSeam{
		Cancel:  func(sid string) error { rt.record("cancel:" + sid); return nil },
		Stream:  func() <-chan ntwire.OrderUpdatePayload { return acks },
		Timeout: 200 * time.Millisecond,
	}
	if withPosition {
		if err := st.Position().Create(&store.TraderPosition{
			TraderID: at.id, Symbol: "MNQ", Side: "LONG", Account: "Sim101",
			ExchangeType: "ninjatrader", EntryQuantity: 1, Quantity: 1,
			EntryPrice: 30000, EntryTime: now.Add(-2 * time.Hour).UnixMilli(),
			Status: "OPEN",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.ArmedOrders().UpsertArm(&store.ArmedOrderDB{
		TraderID: at.id, PlanID: "2026-08-18:NY:trader-1", Version: 1, Session: "NY",
		Scenario: "S1", Side: "long", EntryPx: 29950, StopPx: 29970, TargetPx: 29910,
		State: armState, SignalID: signalID,
	}); err != nil {
		t.Fatal(err)
	}
	return at, rt
}

func cancelsFor(rt *wireRecorder, sid string) int {
	n := 0
	for _, e := range rt.snapshot() {
		if e == "cancel:"+sid {
			n++
		}
	}
	return n
}

// TestFlatWithNoPositionStillCancelsRestingArms — E1(a).
//
// enforceEODFlatAt reads the open positions and returns false when there are
// none — BEFORE it reaches cancelArmedOrdersSync. So a session that ends flat
// BY LUCK (nothing filled) leaves every resting arm alive at the broker, past
// the close, into the next session's tape. A day that ends flat by luck is not
// flat, and nothing in the process says otherwise.
func TestFlatWithNoPositionStillCancelsRestingArms(t *testing.T) {
	now := time.Date(2026, 8, 18, 14, 30, 0, 0, chicagoLoc())
	acks := make(chan ntwire.OrderUpdatePayload, 4)
	at, rt := flatFixture(t, now, false, store.StateWorking, "sig-noposition", acks)
	go func() {
		time.Sleep(20 * time.Millisecond)
		acks <- ntwire.OrderUpdatePayload{SignalID: "sig-noposition", State: "cancelled"}
	}()

	at.enforceEODFlatAt(now)

	if got := cancelsFor(rt, "sig-noposition"); got == 0 {
		t.Errorf("NO wire cancel was sent for a resting arm at the close because no position was open — "+
			"the flatten returned on len(positions)==0 before reaching the cancel. The arm is still "+
			"live at the broker while the book reads flat. wire=%v", rt.snapshot())
	}
	rows, err := at.store.ArmedOrders().ListNonTerminal(at.id)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("%d arm(s) still non-terminal after the close with no position open: %+v", len(rows), rows)
	}
}

// TestFlatCancelsPlacePendingOnTheWire — E1(b).
//
// cancelArmedOrdersSyncWith refuses any row whose state is not exactly
// "working" and writes 'cancelled' into the ledger WITHOUT sending anything:
//
//	if r.State != "working" || r.SignalID == "" || cancelFn == nil || src == nil {
//	    _ = ledger.SetState(r.ID, "cancelled", reason)
//	    continue
//	}
//
// BeginPlacement sets signal_id AND state=place_pending in ONE update, before
// the order reaches the broker — so a place_pending row ALWAYS carries a signal
// id and its order may already be resting. This marks it dead without asking.
// Class 81 (a send read as a settlement) reached by omitting the send entirely.
func TestFlatCancelsPlacePendingOnTheWire(t *testing.T) {
	now := time.Date(2026, 8, 18, 14, 30, 0, 0, chicagoLoc())
	acks := make(chan ntwire.OrderUpdatePayload, 4)
	at, rt := flatFixture(t, now, true, store.StatePlacePending, "sig-pending", acks)
	go func() {
		time.Sleep(20 * time.Millisecond)
		acks <- ntwire.OrderUpdatePayload{SignalID: "sig-pending", State: "cancelled"}
	}()

	at.enforceEODFlatAt(now)

	if got := cancelsFor(rt, "sig-pending"); got == 0 {
		t.Errorf("a place_pending arm was written 'cancelled' with NO wire cancel. BeginPlacement had "+
			"already assigned its signal id, so the order may be resting at the broker right now — "+
			"the ledger says dead and the book may say working. wire=%v", rt.snapshot())
	}
	rows, _ := at.store.ArmedOrders().ListForPlan("2026-08-18:NY:trader-1")
	for _, r := range rows {
		if r.State == store.StateCancelled && !strings.Contains(strings.ToLower(r.StateReason), "confirm") &&
			cancelsFor(rt, r.SignalID) == 0 {
			t.Errorf("row %d reads cancelled with no cancel ever sent (reason %q)", r.ID, r.StateReason)
		}
	}
}

var _ = sync.Mutex{}

// E5 — ON A SHORTENED DAY THE FLATTEN FIRES AT THE EARLY CLOSE.
//
// The half-day pull-in is what makes "flat at the close" mean the REAL close.
// D1 moved the arm cancel above the position read, so this re-pins that the
// pull-in still governs WHEN the whole retirement happens — a cancel that fires
// at 14:45 on a day the exchange shut at 12:00 is 2h45m of resting orders on a
// closed book.
func TestShortenedDayFlattensAtTheEarlyClose(t *testing.T) {
	ny := &kernel.SessionDef{Name: "NY", WindowStartCT: "08:30", WindowEndCT: "14:45"}
	full, _, ok := sessionCutoffCT(ny, 0)
	if !ok {
		t.Fatal("the NY cutoff must resolve")
	}
	if full != 14*60+45 {
		t.Fatalf("full-day NY cutoff = %d, want 14:45", full)
	}
	// An early close at 12:00 PULLS IN.
	early := 12 * 60
	if !halfDayPullsIn(ny, early, full) {
		t.Fatal("a 12:00 CT early close must pull the flat IN from 14:45 — otherwise the book stays " +
			"open on an exchange that has shut")
	}
	// A "late close" never pushes the flat OUT past the session's own end.
	if halfDayPullsIn(ny, 15*60+30, full) {
		t.Fatal("a calendar time AFTER the session end must never push the flat out")
	}
	// Garbage never invents a cutoff.
	if _, _, ok := halfDayCutoffMin(kernel.DefaultSessionRegistry(), "not-a-day", 0); ok {
		t.Fatal("an unknown session-day must not produce a half-day cutoff")
	}
}

// The flatten's ORDER is the contract D1 established: the arm cancel is reached
// before the position read, so it cannot be skipped by a flat book. A source pin
// because the defect was a sequence, and a sequence never exercised cannot be
// observed at runtime.
func TestFlattenCancelsBeforeItReadsPositions(t *testing.T) {
	b, err := os.ReadFile("auto_trader_clock.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	fn := strings.Index(src, "func (at *AutoTrader) enforceEODFlatAt(")
	if fn < 0 {
		t.Fatal("enforceEODFlatAt not found — this pin has lost its subject")
	}
	body := src[fn:]
	if e := strings.Index(body[1:], "\nfunc "); e >= 0 {
		body = body[:e+1]
	}
	cancel := strings.Index(body, "at.cancelArmedOrdersSyncFiltered(")
	read := strings.Index(body, "at.store.Position().GetOpenPositions(")
	if cancel < 0 || read < 0 {
		t.Fatalf("cannot locate both halves (cancel=%d read=%d)", cancel, read)
	}
	if cancel > read {
		t.Fatalf("the position read comes BEFORE the arm cancel (read@%d < cancel@%d) — a flat book "+
			"short-circuits the close and every resting arm survives it", read, cancel)
	}
}

// A ROW WITH A SIGNAL ID IS NEVER RETIRED BY OUR OWN PLUMBING BEING ABSENT.
//
// The D1 fix replaced `r.State != "working"` with a test on the signal id — and
// left the rest of the disjunction standing:
//
//	if r.SignalID == "" || cancelFn == nil || src == nil { SetState(cancelled) }
//
// So a row that HAS a signal id — by definition an order at the broker — was
// still written 'cancelled' whenever the wire or the ack stream was missing.
// That is the same class-81 shape the fix was written to end, surviving in the
// half of the condition nobody re-read. My own comment above it claimed "every
// other row gets a cancel on the wire", which the code did not do.
//
// one_contract.go already learned this exact lesson: "an unreachable AddOn sent
// the row down the terminal branch below — writing 'cancelled' on an order the
// broker still holds, which is class 81 exactly". A missing wire is a reason to
// record the INTENT, not to declare the outcome.
func TestNoWireNeverRetiresARowThatReachedTheBroker(t *testing.T) {
	now := time.Date(2026, 8, 18, 14, 30, 0, 0, chicagoLoc())
	at, _ := flatFixture(t, now, true, store.StateWorking, "sig-nowire", nil)

	// The wire is gone: no cancel function, no ack stream.
	n, unacked := at.cancelArmedOrdersSyncWith("session close — EOD flat", time.Millisecond, nil, nil, nil)
	rows, err := at.store.ArmedOrders().ListForPlan("2026-08-18:NY:trader-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.SignalID == "" {
			continue
		}
		if r.State == store.StateCancelled {
			t.Fatalf("row %d (signal %s) was written 'cancelled' with NO wire available — the order it "+
				"names may be resting at the broker right now, and the ledger has declared it dead. "+
				"n=%d unacked=%d reason=%q", r.ID, r.SignalID, n, unacked, r.StateReason)
		}
	}
}

// The T1 red-news force-flat must not read a FAILED position query as "flat".
// The EOD path closed this on 2026-09-09; its sibling two functions away did
// not get the fix, and a DB hiccup two minutes before FOMC silently becomes
// "no position to flatten" while the position rides the print.
func TestT1ForceFlatDoesNotReadAFailedQueryAsFlat(t *testing.T) {
	b, err := os.ReadFile("auto_trader_clock.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	fn := strings.Index(src, "func (at *AutoTrader) enforceT1ForceFlatAt(")
	if fn < 0 {
		t.Fatal("enforceT1ForceFlatAt not found — this pin has lost its subject")
	}
	body := src[fn:]
	if e := strings.Index(body[1:], "\nfunc "); e >= 0 {
		body = body[:e+1]
	}
	if strings.Contains(body, "if err != nil || len(positions) == 0 {") {
		t.Fatal("T1 force-flat still collapses a FAILED position read into \"flat\" — " +
			"`if err != nil || len(positions) == 0`. A24: an unreadable book is not an empty one, and " +
			"this is the two-minutes-before-red-news path")
	}
	if !strings.Contains(body, "UNVERIFIED") {
		t.Error("T1 force-flat does not report unverified flatness — the EOD path says " +
			"\"flatness UNVERIFIED this cycle\"; its sibling must be as honest")
	}
}
