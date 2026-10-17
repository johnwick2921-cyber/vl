package trader

import (
	"os"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// NEWS-HYGIENE (2026-08-29) — the synthetic-T1 fixture. The gate contract under
// test: once a T1 (red-news) window is DUE (blackout −2min → window end), every
// working armed order is cancelled (reason=news_window) and any open position
// is force-flattened — and the arm-cancel runs even when the trader is FLAT
// (pre-wave, a resting limit survived the whole window because the cancel sat
// behind the open-position check).
//
// Synthetic setup: a stored forexfactory slice carrying one T1 event ("Synthetic
// CPI") at 08:45 CT. now = 08:40 CT (inside the ±15m blackout and the −2m lead),
// NY session active (default registry, 08:30–14:45 CT).

const t1NewsDate = "2026-09-04" // Friday — September CT = CDT (UTC-5)

func t1NewsFixture(t *testing.T, withPosition bool, ackTimeout time.Duration) (*AutoTrader, *store.Store, *wireRecorder, time.Time) {
	t.Helper()
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	at, st := resetTrader(t, cfg)
	rt := &wireRecorder{MockTrader: &MockTrader{}}
	at.trader = rt
	at.armedSyncSeam = &armedSyncSeam{
		Cancel: func(sid string) error {
			rt.record("cancel:" + sid)
			return nil
		},
		Stream:  func() <-chan ntwire.OrderUpdatePayload { return nil }, // no acks → unacked path flips the ledger anyway
		Timeout: ackTimeout,
	}

	// Stored calendar slice with the synthetic T1 event (08:45 CT = 13:45Z in CDT).
	slice := &store.CalendarSliceDB{
		TradeDate: t1NewsDate, Source: "forexfactory",
		EventsJSON: `[{"time":"2026-09-04T13:45:00Z","currency":"USD","title":"Synthetic CPI","impact":"T1"}]`,
		CreatedAt:  time.Now().UnixMilli(),
	}
	if _, err := st.Calendar().SaveSliceIfAbsent(slice); err != nil {
		t.Fatalf("save slice: %v", err)
	}

	now, _ := time.Parse(time.RFC3339, "2026-09-04T13:40:00Z") // 08:40 CT
	if err := st.ArmedOrders().UpsertArm(&store.ArmedOrderDB{
		TraderID: at.id, PlanID: "2026-09-04:NY:trader-1", Version: 1, Session: "NY",
		Scenario: "S1", Side: "short", EntryPx: 29950, StopPx: 29970, TargetPx: 29910,
		State: "working", SignalID: "sig-news",
	}); err != nil {
		t.Fatalf("arm upsert: %v", err)
	}
	if withPosition {
		if err := st.Position().Create(&store.TraderPosition{
			TraderID: at.id, Symbol: "MNQ", Side: "SHORT", Account: "Sim101",
			ExchangeType: "ninjatrader", EntryQuantity: 1, Quantity: 1,
			EntryPrice: 30000, EntryTime: now.Add(-2 * time.Hour).UnixMilli(), Status: "OPEN",
		}); err != nil {
			t.Fatalf("position create: %v", err)
		}
	}
	return at, st, rt, now
}

func armedRowState(t *testing.T, st *store.Store) (string, string) {
	t.Helper()
	rows, err := st.ArmedOrders().ListForPlan("2026-09-04:NY:trader-1")
	if err != nil || len(rows) == 0 {
		t.Fatalf("plan list: %v / %d rows", err, len(rows))
	}
	return rows[0].State, rows[0].StateReason
}

// TestT1NewsFlatTraderArmCancelled — a FLAT trader's working armed limit is
// cancelled (reason=news_window) inside the due window, and nothing is
// flattened. This is the case the pre-wave code missed entirely.
func TestT1NewsFlatTraderArmCancelled(t *testing.T) {
	at, st, rt, now := t1NewsFixture(t, false, 50*time.Millisecond)

	if !at.enforceT1ForceFlatAt(now) {
		t.Fatal("a due T1 window with a working arm must report an action")
	}
	// UPDATED 2026-09-10 (settlement wave, C1). The fixture's ack stream is
	// silent, so this cancel is UNCONFIRMED — and an unconfirmed cancel is not a
	// cancellation. The row is held cancel_pending for the settlement pass; the
	// test's real subject (a FLAT trader's working arm IS cancelled inside the
	// window, reason=news_window, nothing flattened) is unchanged and still
	// asserted below.
	state, reason := armedRowState(t, st)
	if state != store.StateCancelPending {
		t.Fatalf("armed row state = %q, want %q — the ack never came, and promoting on a timeout is the C1 defect", state, store.StateCancelPending)
	}
	if !strings.HasPrefix(reason, "news_window") {
		t.Fatalf("state_reason = %q, want news_window prefix", reason)
	}
	for _, ev := range rt.snapshot() {
		if strings.HasPrefix(ev, "close_") {
			t.Fatalf("flat trader must NOT flatten, saw %q", ev)
		}
	}
	cancels := 0
	for _, ev := range rt.snapshot() {
		if ev == "cancel:sig-news" {
			cancels++
		}
	}
	if cancels == 0 {
		t.Fatal("the working arm's wire cancel never issued")
	}
}

// TestT1NewsArmedCancelBeforeFlatten — the twin with an open position: the
// armed cancel lands BEFORE the flatten, and the position is flattened.
func TestT1NewsArmedCancelBeforeFlatten(t *testing.T) {
	at, st, rt, now := t1NewsFixture(t, true, 50*time.Millisecond)

	if !at.enforceT1ForceFlatAt(now) {
		t.Fatal("a due T1 window with an open position must act")
	}
	// UPDATED 2026-09-10 (settlement wave, C1) — same reason as its twin above.
	// The subject here is the WIRE ORDER (cancel before close), asserted below.
	state, _ := armedRowState(t, st)
	if state != store.StateCancelPending {
		t.Fatalf("armed row state = %q, want %q — the fixture never acks the cancel", state, store.StateCancelPending)
	}
	ev := rt.snapshot()
	firstCancel, firstClose := -1, -1
	for i, e := range ev {
		if e == "cancel:sig-news" && firstCancel < 0 {
			firstCancel = i
		}
		if strings.HasPrefix(e, "close_short") && firstClose < 0 {
			firstClose = i
		}
	}
	if firstCancel < 0 || firstClose < 0 {
		t.Fatalf("expected a wire cancel AND a flatten, got %v", ev)
	}
	if firstCancel > firstClose {
		t.Fatalf("armed cancel must precede the flatten, got %v", ev)
	}
}

// TestT1NewsEntryBlockedInWindow — the session gate refuses entries inside the
// ±15m blackout with the red-news reason.
func TestT1NewsEntryBlockedInWindow(t *testing.T) {
	at, _, _, now := t1NewsFixture(t, false, 50*time.Millisecond)

	windows := at.currentT1Windows(now)
	if len(windows) == 0 {
		t.Fatal("the synthetic T1 slice must produce a blackout window")
	}
	why, blocked := sessionGateDecision(at.sessionRegistry(now), now, windows, nil)
	if !blocked || !strings.Contains(why, "red-news blackout") {
		t.Fatalf("entry gate must block with red-news reason; blocked=%v why=%q", blocked, why)
	}
}

// TestCalendarStaticLoaderDropsNotes — the AMENDED static file may carry NOTE
// annotations (holidays / contract rolls / quad-witching). They must never
// gate and never reach the prompt: the loader drops them, T1/T2 pass through.
func TestCalendarStaticLoaderDropsNotes(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/cal.json"
	writeFile := func(content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write static file: %v", err)
		}
	}
	t.Setenv("NOFX_CALENDAR_STATIC", path)

	writeFile(`[
	  {"time":"2026-09-04T12:30:00Z","currency":"USD","title":"NFP","impact":"T1"},
	  {"time":"2026-09-10T12:30:00Z","currency":"USD","title":"Claims","impact":"T2"},
	  {"time":"2026-09-07T00:00:00Z","currency":"USD","title":"NOTE: Labor Day","impact":"note"}
	]`)
	evs := calendarStaticLoader()
	if len(evs) != 2 {
		t.Fatalf("loader must drop note entries, got %d events", len(evs))
	}
	for _, e := range evs {
		if strings.HasPrefix(e.Title, "NOTE:") || string(e.Impact) == "note" {
			t.Fatalf("note entry leaked: %+v", e)
		}
	}
}
