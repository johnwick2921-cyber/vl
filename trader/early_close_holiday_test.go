package trader

// R22 TIME-WEEKLY — P1-B, the half-day early close, driven through the
// production clock path (testNow → traderNow, the class-32 trader clock).
//
// kernel/session_calendar.json keys a shortened day by the CALENDAR DATE of the
// shortened session (2026-11-27 close 12:15, 2026-12-24 close 12:15). The
// trader looked it up with kernel.CMESessionDayKey(now) — the date of the
// session's 17:00 START, i.e. the PREVIOUS day — so on Fri 11-27 it asked for
// "2026-11-26", found no early close, and kept the NY last-entry at 14:30 and
// the EOD flat at 14:45. Then at 12:15 IsCMEOpen turns false and
// cmeSessionClosedSkip (runCycle step 0a) returns BEFORE enforceEODFlat
// (runCycle, far below) can run: resting arms and open positions carry over
// the holiday weekend.

import (
	"os"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// earlyCloseFixture: NY enabled at the SHIPPED offsets (last-entry 15, EOD
// flat 0 — what both live strategies run), one OPEN long, one WORKING arm.
func earlyCloseFixture(t *testing.T, now time.Time, signalID string, acks <-chan ntwire.OrderUpdatePayload) (*AutoTrader, *wireRecorder) {
	t.Helper()
	trueV := true
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{
		PlanEnabled: true,
		Sessions:    []store.DayPlanSessionOverride{{Session: "NY", Enable: &trueV}},
	}}
	at, st := resetTrader(t, cfg)
	rt := &wireRecorder{MockTrader: &MockTrader{}}
	at.trader = rt
	at.armedSyncSeam = &armedSyncSeam{
		Cancel:  func(sid string) error { rt.record("cancel:" + sid); return nil },
		Stream:  func() <-chan ntwire.OrderUpdatePayload { return acks },
		Timeout: 200 * time.Millisecond,
	}
	if err := st.Position().Create(&store.TraderPosition{
		TraderID: at.id, Symbol: "MNQ", Side: "LONG", Account: "Sim101",
		ExchangeType: "ninjatrader", EntryQuantity: 1, Quantity: 1,
		EntryPrice: 30000, EntryTime: now.Add(-2 * time.Hour).UnixMilli(),
		Status: "OPEN",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.ArmedOrders().UpsertArm(&store.ArmedOrderDB{
		TraderID: at.id, PlanID: "2026-11-27:NY:trader-1", Version: 1, Session: "NY",
		Scenario: "S1", Side: "long", EntryPx: 29950, StopPx: 29930, TargetPx: 29990,
		State: store.StateWorking, SignalID: signalID,
	}); err != nil {
		t.Fatal(err)
	}
	return at, rt
}

func setTraderClock(t *testing.T, at time.Time) {
	t.Helper()
	prev := testNow
	testNow = func() time.Time { return at }
	t.Cleanup(func() { testNow = prev })
}

func hasEvent(rt *wireRecorder, ev string) bool {
	for _, e := range rt.snapshot() {
		if e == ev {
			return true
		}
	}
	return false
}

// TestEarlyCloseDay1127TickSequence walks Fri 2026-11-27 (CME equity close
// 12:15 CT) through the production gates in runCycle order.
func TestEarlyCloseDay1127TickSequence(t *testing.T) {
	ct := chicagoLoc()
	at1150 := time.Date(2026, 11, 27, 11, 50, 0, 0, ct)
	acks := make(chan ntwire.OrderUpdatePayload, 4)
	at, rt := earlyCloseFixture(t, at1150, "sig-1127", acks)

	// 11:50 — before the pulled-in last entry (12:15 − 15 = 12:00): open.
	setTraderClock(t, at1150)
	if why, blocked := at.entryBlockedByLastEntryAt(at1150); blocked {
		t.Fatalf("11:50 must still admit entries, got blocked: %s", why)
	}

	// 12:05 — past the early-close last entry. The calendar row is keyed
	// 2026-11-27; the session-start key would be 2026-11-26 and find nothing.
	at1205 := time.Date(2026, 11, 27, 12, 5, 0, 0, ct)
	setTraderClock(t, at1205)
	why, blocked := at.entryBlockedByLastEntryAt(at1205)
	if !blocked || !strings.Contains(why, "12:00") {
		t.Errorf("12:05 on the 12:15 early-close day must refuse new entries at the pulled-in 12:00 cutoff; "+
			"got blocked=%v %q (the lookup key %s → no early close; %s → 12:15)",
			blocked, why, kernel.CMESessionDayKey(at1205), at1205.Format("2006-01-02"))
	}

	// 12:10 — market still open; the flat (12:15 − 0) is not due yet, and the
	// cycle must not skip.
	at1210 := time.Date(2026, 11, 27, 12, 10, 0, 0, ct)
	setTraderClock(t, at1210)
	if at.cmeSessionClosedSkip() {
		t.Fatal("12:10 is before the 12:15 early close — the cycle must not take the closed path")
	}
	if hasEvent(rt, "cancel:sig-1127") || hasEvent(rt, "close_long:MNQ") {
		t.Fatalf("nothing may be retired before the early close: %v", rt.snapshot())
	}

	// 12:15 — the early close. IsCMEOpen is false, so runCycle stops at
	// cmeSessionClosedSkip; the book must be retired THERE, before the skip.
	at1215 := time.Date(2026, 11, 27, 12, 15, 0, 0, ct)
	setTraderClock(t, at1215)
	if kernel.IsCMEOpen(at1215) {
		t.Fatal("fixture premise: 12:15 CT on 11-27 is the early close")
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		acks <- ntwire.OrderUpdatePayload{SignalID: "sig-1127", State: "cancelled"}
	}()
	if !at.cmeSessionClosedSkip() {
		t.Fatal("12:15 CT on the early-close day must take the closed path")
	}
	if !hasEvent(rt, "cancel:sig-1127") {
		t.Errorf("the resting arm was NOT cancelled at the 12:15 early close — it rests over the holiday weekend. wire=%v", rt.snapshot())
	}
	if !hasEvent(rt, "close_long:MNQ") {
		t.Errorf("the open long was NOT flattened at the 12:15 early close — it carries over the holiday weekend. wire=%v", rt.snapshot())
	}
	rows, err := at.store.ArmedOrders().ListNonTerminal(at.id)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("%d arm(s) still non-terminal after the early close: %+v", len(rows), rows)
	}
}

// The ordering the fix relies on: runCycle's closed-path skip sits ABOVE the
// EOD flat, so on an early-close day the retirement must live INSIDE the
// closed path. If a later wave moves enforceEODFlat above the skip this pin
// says why the closed-path retirement exists.
func TestRunCycleClosedSkipPrecedesEODFlat(t *testing.T) {
	b, err := os.ReadFile("auto_trader_loop.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	fn := strings.Index(src, "func (at *AutoTrader) runCycle() error {")
	if fn < 0 {
		t.Fatal("runCycle not found — this pin has lost its subject")
	}
	body := src[fn:]
	skip := strings.Index(body, "if at.cmeSessionClosedSkip() {")
	flat := strings.Index(body, "if at.enforceEODFlat() {")
	if skip < 0 || flat < 0 {
		t.Fatalf("runCycle lost a step: closedSkip@%d eodFlat@%d", skip, flat)
	}
	if skip > flat {
		t.Log("enforceEODFlat now runs before the closed skip — the closed-path retirement is belt-and-braces")
	}
}
