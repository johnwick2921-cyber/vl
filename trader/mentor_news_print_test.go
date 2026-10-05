package trader

import (
	"testing"
	"time"

	"vl/calendar"
	"vl/kernel"
	"vl/store"
)

// ── item 18 part 2 (R12) — at print −10m on a red-folder print day, cancel
// live INTRADAY mentor arms by ArmID and flatten intraday mentor positions
// [D4.4 p1 @20:44–21:46]. SWING4H arms and positions are exempt. ─────────────

// newsPrintEvents is the events seam: a T1 CPI print at 07:30 CT.
func newsPrintEvents(t *testing.T) {
	t.Helper()
	mentorDayEventsForTest = func() ([]calendar.Event, bool) {
		return []calendar.Event{{
			Time:   time.Date(2026, 9, 23, 7, 30, 0, 0, kernel.CTLocation()),
			Title:  "CPI m/m",
			Impact: calendar.T1,
		}}, true
	}
	t.Cleanup(func() { mentorDayEventsForTest = nil })
}

// newsClock returns an instant inside the 07:20–07:35 CT print window.
func newsClock(h, m int) time.Time {
	return b3Clock(h, m) // Wed 2026-09-23 CT
}

// TestMentorNewsCancelFlattenCancelsIntradayArms — the production call site:
// inside the print window on a print day, a live INTRADAY arm is cancelled by
// ArmID; a SWING4H arm survives. The mutant that skips the sweep leaves the
// intraday arm resting.
func TestMentorNewsCancelFlattenCancelsIntradayArms(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	newsPrintEvents(t)

	// live intraday arm (never placed → SignalID empty → cancel settles now).
	seedMentorArmedRow(t, at, ledger, "intraday-news", newsClock(7, 25).UnixMilli()+60_000)
	intraday := readArmRow(t, ledger, "intraday-news")
	mentorRegisterLiveArm("isb-news", intraday.ID, "long", intraday.EntryPx)
	t.Cleanup(func() {
		mentorLiveMu.Lock()
		mentorLiveArms = map[string]mentorLiveArm{}
		mentorLiveMu.Unlock()
	})

	// live SWING4H arm — exempt (held by the 4h).
	seedSwingMentorArmedRow(t, at, ledger, "swing-news", newsClock(7, 25).UnixMilli()+60_000)
	swing := readArmRow(t, ledger, "swing-news")
	mentorRegisterLiveArm("swing-news", swing.ID, "long", swing.EntryPx)

	at.mentorNewsCancelFlattenAt(newsClock(7, 25))

	if got := readArmRow(t, ledger, "intraday-news"); got.State != store.StateCancelled {
		t.Fatalf("intraday mentor arm must be cancelled before the print, got state=%q", got.State)
	}
	if got := readArmRow(t, ledger, "swing-news"); store.IsTerminalArmState(got.State) {
		t.Fatalf("SWING4H mentor arm must survive the news sweep, got state=%q", got.State)
	}
}

// newsFlattenRecorder counts CloseLong/CloseShort separately so the swing
// exemption is pinned (the swing must not be flattened, whatever its side).
type newsFlattenRecorder struct {
	*MockTrader
	closedLong, closedShort int
}

func (r *newsFlattenRecorder) CloseLong(string, float64) (map[string]interface{}, error) {
	r.closedLong++
	return nil, nil
}
func (r *newsFlattenRecorder) CloseShort(string, float64) (map[string]interface{}, error) {
	r.closedShort++
	return nil, nil
}

// TestMentorNewsCancelFlattenFlattensIntradayPositions — the flatten half: an
// intraday mentor position is force-closed before the print; a SWING4H position
// is exempt. The mutant that drops the flatten (or the swing exemption) fails.
func TestMentorNewsCancelFlattenFlattensIntradayPositions(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	rec := &newsFlattenRecorder{MockTrader: &MockTrader{}}
	at.trader = rec
	t.Cleanup(at.Stop)
	newsPrintEvents(t)

	seedOpen := func(scenario, side string) {
		p := &store.TraderPosition{
			TraderID: at.id, Symbol: "MNQ", Side: side, Account: "Sim101",
			EntryPrice: 30000, EntryTime: newsClock(7, 10).UnixMilli(),
			EntryQuantity: 1, Quantity: 1, Status: "OPEN", CitedScenarioID: scenario,
		}
		if err := st.Position().Create(p); err != nil {
			t.Fatal(err)
		}
	}
	seedOpen("isb-1", "LONG")       // intraday long
	seedOpen("swing-1234", "SHORT") // swing short

	at.mentorNewsCancelFlattenAt(newsClock(7, 25))

	if rec.closedLong != 1 || rec.closedShort != 0 {
		t.Fatalf("exactly the intraday LONG must be flattened: closedLong=%d closedShort=%d (want 1/0)", rec.closedLong, rec.closedShort)
	}
}

// TestMentorNewsCancelFlattenNoOpOutsideWindow — outside the print window (or
// on a non-print day) the destructive sweep must NOT fire.
func TestMentorNewsCancelFlattenNoOpOutsideWindow(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	newsPrintEvents(t)

	seedMentorArmedRow(t, at, ledger, "intraday-keep", newsClock(8, 0).UnixMilli()+60_000)
	intraday := readArmRow(t, ledger, "intraday-keep")
	mentorRegisterLiveArm("isb-keep", intraday.ID, "long", intraday.EntryPx)
	t.Cleanup(func() {
		mentorLiveMu.Lock()
		mentorLiveArms = map[string]mentorLiveArm{}
		mentorLiveMu.Unlock()
	})

	// 07:10 CT — BEFORE the −10m window opens.
	if at.mentorNewsCancelFlattenAt(newsClock(7, 10)) {
		t.Fatal("outside the print window the sweep must not act")
	}
	if got := readArmRow(t, ledger, "intraday-keep"); store.IsTerminalArmState(got.State) {
		t.Fatalf("outside the window the intraday arm must survive, got state=%q", got.State)
	}

	// A non-print day (empty events) in the window must also no-op.
	mentorDayEventsForTest = func() ([]calendar.Event, bool) { return nil, true }
	if at.mentorNewsCancelFlattenAt(newsClock(7, 25)) {
		t.Fatal("a non-print day must not flatten or cancel")
	}
}
