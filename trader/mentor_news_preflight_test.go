package trader

import (
	"strings"
	"testing"
	"time"

	"vl/calendar"
	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── item 18 (D4.4-03 + D4.4-08): news print ───────────────────────────────

func TestMentorNewsPrintAt(t *testing.T) {
	loc := kernel.CTLocation()
	now := time.Date(2026, 10, 5, 7, 20, 0, 0, loc)
	mk := func(title string, impact calendar.Impact, hh, mm int) calendar.Event {
		return calendar.Event{Time: time.Date(2026, 10, 5, hh, mm, 0, 0, loc), Impact: impact, Title: title}
	}
	cases := []struct {
		name      string
		events    []calendar.Event
		wantZero  bool
		wantTitle string
	}{
		{"cpi", []calendar.Event{mk("CPI m/m", calendar.T1, 7, 30)}, false, "CPI m/m"},
		{"ppi", []calendar.Event{mk("PPI m/m", calendar.T1, 7, 30)}, false, "PPI m/m"},
		{"unemployment", []calendar.Event{mk("Unemployment Rate", calendar.T1, 7, 30)}, false, "Unemployment Rate"},
		{"fomc not a named print", []calendar.Event{mk("FOMC Statement", calendar.T1, 7, 30)}, true, ""},
		{"t2 not hard", []calendar.Event{mk("CPI m/m", calendar.T2, 7, 30)}, true, ""},
		{"wrong minute", []calendar.Event{mk("CPI m/m", calendar.T1, 8, 30)}, true, ""},
	}
	for _, c := range cases {
		printAt, title := mentorNewsPrintAt(c.events, now)
		if c.wantZero != printAt.IsZero() {
			t.Fatalf("%s: printAt zero=%v, want zero=%v", c.name, printAt.IsZero(), c.wantZero)
		}
		if c.wantTitle != "" && title != c.wantTitle {
			t.Fatalf("%s: title=%q, want %q", c.name, title, c.wantTitle)
		}
	}
}

// newsPreflightFixture builds the pre-print scene: one intraday mentor arm
// (Condition "") + one SWING4H arm, one intraday LONG position + one swing LONG
// position, a recording trader and the sync-cancel seam.
func newsPreflightFixture(t *testing.T, now time.Time) (*AutoTrader, *wireRecorder) {
	t.Helper()
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	rt := &wireRecorder{MockTrader: &MockTrader{}}
	at.trader = rt
	at.armedSyncSeam = &armedSyncSeam{
		Cancel:  func(sid string) error { rt.record("cancel:" + sid); return nil },
		Stream:  func() <-chan ntwire.OrderUpdatePayload { return make(chan ntwire.OrderUpdatePayload) },
		Timeout: 200 * time.Millisecond,
	}
	arms := []store.ArmedOrderDB{
		{TraderID: at.id, PlanID: "p", Version: 1, Session: "NY", Scenario: "isb-1", Side: "long", EntryPx: 100, StopPx: 95, TargetPx: 110, State: "armed", SignalID: "isb-1", Origin: store.ArmOriginMentor},
		{TraderID: at.id, PlanID: "p", Version: 1, Session: "NY", Scenario: "swing-1", Side: "long", EntryPx: 100, StopPx: 95, TargetPx: 110, State: "armed", SignalID: "swing-1", Origin: store.ArmOriginMentor, Condition: "SWING4H"},
	}
	for i := range arms {
		if err := st.ArmedOrders().UpsertArm(&arms[i]); err != nil {
			t.Fatal(err)
		}
	}
	positions := []store.TraderPosition{
		{TraderID: at.id, Symbol: "MNQ", Side: "LONG", Account: "Sim101", ExchangeType: "ninjatrader", EntryQuantity: 1, Quantity: 1, EntryPrice: 30000, EntryTime: now.Add(-2 * time.Hour).UnixMilli(), Status: "OPEN", CitedScenarioID: "isb-1"},
		{TraderID: at.id, Symbol: "MNQ", Side: "LONG", Account: "Sim101", ExchangeType: "ninjatrader", EntryQuantity: 1, Quantity: 1, EntryPrice: 30000, EntryTime: now.Add(-2 * time.Hour).UnixMilli(), Status: "OPEN", CitedScenarioID: "swing-1"},
	}
	for i := range positions {
		if err := st.Position().Create(&positions[i]); err != nil {
			t.Fatal(err)
		}
	}
	return at, rt
}

func newsEventsSeam(t *testing.T, events []calendar.Event) {
	t.Helper()
	prev := mentorDayEventsForTest
	mentorDayEventsForTest = func() ([]calendar.Event, bool) { return events, true }
	t.Cleanup(func() { mentorDayEventsForTest = prev })
}

func countEv(rt *wireRecorder, prefix string) int {
	n := 0
	for _, e := range rt.snapshot() {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

// TestMentorPrintPreflightCancelsIntradayFlattensIntradaySwingExempt — the
// D4.4-08 call-site pin: at 07:20 CT on a T1 CPI print day, the intraday arm is
// cancelled and the intraday position flattened; the SWING arm and position keep
// their stop (U-5 default).
func TestMentorPrintPreflightCancelsIntradayFlattensIntradaySwingExempt(t *testing.T) {
	loc := kernel.CTLocation()
	now := time.Date(2026, 10, 5, 7, 20, 0, 0, loc)
	cpi := calendar.Event{Time: time.Date(2026, 10, 5, 7, 30, 0, 0, loc), Impact: calendar.T1, Title: "CPI m/m"}
	at, rt := newsPreflightFixture(t, now)
	newsEventsSeam(t, []calendar.Event{cpi})

	at.mentorPrintPreflight(now)

	if got := countEv(rt, "cancel:isb-1"); got < 1 {
		t.Fatalf("the intraday mentor arm must be cancelled (≥1 attempt), got %d", got)
	}
	if got := countEv(rt, "cancel:swing-1"); got != 0 {
		t.Fatalf("the SWING4H arm must NOT be cancelled, got %d", got)
	}
	if got := countEv(rt, "close_long:"); got != 1 {
		t.Fatalf("exactly the intraday LONG position must be flattened, got %d close_long", got)
	}

	// latch: a second preflight in the same window requests nothing new.
	before := countEv(rt, "cancel:isb-1")
	at.mentorPrintPreflight(now)
	if got := countEv(rt, "cancel:isb-1"); got != before {
		t.Fatalf("the preflight must latch (fire once): cancels %d → %d", before, got)
	}
	if got := countEv(rt, "close_long:"); got != 1 {
		t.Fatalf("the preflight must latch: close_long %d, want still 1", got)
	}
}

// TestMentorPrintPreflightOutsideWindowDoesNothing — before the 07:20 cut or at
// the print itself the preflight is a no-op.
func TestMentorPrintPreflightOutsideWindowDoesNothing(t *testing.T) {
	loc := kernel.CTLocation()
	cpi := calendar.Event{Time: time.Date(2026, 10, 5, 7, 30, 0, 0, loc), Impact: calendar.T1, Title: "CPI m/m"}
	for _, now := range []time.Time{
		time.Date(2026, 10, 5, 7, 19, 0, 0, loc), // just before the cut
		time.Date(2026, 10, 5, 7, 30, 0, 0, loc), // the print itself
		time.Date(2026, 10, 5, 8, 0, 0, 0, loc),  // past it
	} {
		at, rt := newsPreflightFixture(t, now)
		newsEventsSeam(t, []calendar.Event{cpi})
		at.mentorPrintPreflight(now)
		if n := countEv(rt, "cancel:"); n != 0 {
			t.Fatalf("now=%v: nothing may be cancelled outside the pre-print window, got %d", now, n)
		}
		if n := countEv(rt, "close_"); n != 0 {
			t.Fatalf("now=%v: nothing may be flattened outside the pre-print window, got %d", now, n)
		}
	}
}
