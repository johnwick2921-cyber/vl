package trader

// m471 L1-1 — on a SHORTENED day (kernel/session_calendar.json: 2026-11-27 and
// 2026-12-24, close 12:15 CT) the Day-Plan / mentor flat lands at least
// earlyCloseFlatMarginMin BEFORE the early close, never at the halt minute, and
// mentor arms stop being admitted before that flat. Before the fix the mentor's
// only last-entry was 15:45 (admitted at 12:14:59) and the flat was the close
// itself (12:15:00 = the instant IsCMEOpen turns false), so the flatten went
// into a halted book that does not reopen until after the holiday.
//
// Every assertion drives a production call site: admitEntry (mentor arm),
// enforceEODFlatAt, forceFlatWindowAt, cmeSessionClosedSkip — at the shipped
// offsets (last-entry 15, EOD flat 0) via earlyCloseFixture.

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

var shortenedDays = []struct{ y, m, d int }{{2026, 11, 27}, {2026, 12, 24}}

func ctDay(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, chicagoLoc())
}

func hm(day time.Time, h, m, s int) time.Time {
	return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(s)*time.Second)
}

func mentorArmAdmit(at *AutoTrader, now time.Time) (string, bool) {
	return at.admitEntry(admitIntent{Path: admitArm, MentorArm: true, Symbol: "MNQ", Action: "open_long", Now: now, Key: "m471-l1-1"})
}

func isTimeGateRefusal(reason string) bool {
	return strings.HasPrefix(reason, "last_entry") || strings.HasPrefix(reason, "cme_closed") || strings.HasPrefix(reason, "force_flat")
}

// The mentor's shortened-day last entry is early close − 15 (12:00 on a 12:15
// day): admitted at 11:59, refused at 12:00 and at 12:14:30, while CME is open.
// Run with the Day Plan OFF (a mentor-only trader), so the mentor's own cutoff
// is the gate that answers — with it ON the force-flat window also refuses
// from 12:10.
func TestMentorArmRefusedBeforeShortenedDayFlat(t *testing.T) {
	if mentorEarlyCloseLeadMin <= earlyCloseFlatMarginMin {
		t.Fatalf("the mentor last entry (close − %dm) must come BEFORE the flat (close − %dm)", mentorEarlyCloseLeadMin, earlyCloseFlatMarginMin)
	}
	for _, d := range shortenedDays {
		day := ctDay(d.y, d.m, d.d)
		acks := make(chan ntwire.OrderUpdatePayload, 4)
		at, _ := earlyCloseFixture(t, hm(day, 10, 0, 0), "sig-adm-"+day.Format("0102"), acks)
		at.config.StrategyConfig.RiskControl.MentorMode = true
		at.config.StrategyConfig.DayPlan.PlanEnabled = false

		if r, refused := mentorArmAdmit(at, hm(day, 11, 59, 0)); refused && isTimeGateRefusal(r) {
			t.Errorf("%s 11:59 CT: a mentor arm must still be admitted by the time gates, got %q", day.Format("2006-01-02"), r)
		}
		for _, now := range []time.Time{hm(day, 12, 0, 0), hm(day, 12, 14, 30)} {
			if !kernel.IsCMEOpen(now) {
				t.Fatalf("premise: %s is open", now)
			}
			r, refused := mentorArmAdmit(at, now)
			if !refused || !strings.HasPrefix(r, "last_entry") || !strings.Contains(r, "12:00 CT") {
				t.Errorf("%s %s CT: a mentor arm must be refused at the shortened-day last entry (12:00 CT), got refused=%v %q",
					day.Format("2006-01-02"), now.Format("15:04:05"), refused, r)
			}
		}
	}
}

// Ordinary days keep the 15:45 mentor cutoff and nothing earlier.
func TestMentorArmCutoffUnchangedOnOrdinaryDay(t *testing.T) {
	day := ctDay(2026, 11, 20) // an ordinary Friday
	acks := make(chan ntwire.OrderUpdatePayload, 4)
	at, _ := earlyCloseFixture(t, hm(day, 10, 0, 0), "sig-ord", acks)
	at.config.StrategyConfig.RiskControl.MentorMode = true
	for _, now := range []time.Time{hm(day, 12, 0, 0), hm(day, 12, 14, 30), hm(day, 15, 44, 59)} {
		if why, blocked := at.mentorPastDailyHaltCutoff(now); blocked {
			t.Errorf("ordinary %s: the mentor cutoff must not fire, got %q", now.Format("15:04:05"), why)
		}
	}
	if why, blocked := at.mentorPastDailyHaltCutoff(hm(day, 15, 45, 0)); !blocked || !strings.Contains(why, "15:45") {
		t.Errorf("ordinary 15:45: the mentor cutoff must fire at 15:45, got blocked=%v %q", blocked, why)
	}
	if _, due := earlyCloseFlatDue(hm(day, 12, 14, 0)); due {
		t.Error("an ordinary day has no early-close flat")
	}
}

// The flat goes out at early close − margin (12:10) while CME is still open —
// for a mentor trader and for a Day-Plan-only trader — and not a beat sooner.
func TestShortenedDayFlatLandsBeforeTheHalt(t *testing.T) {
	for _, mentor := range []bool{true, false} {
		for _, d := range shortenedDays {
			day := ctDay(d.y, d.m, d.d)
			sig := "sig-flat-" + day.Format("0102")
			acks := make(chan ntwire.OrderUpdatePayload, 4)
			at, rt := earlyCloseFixture(t, hm(day, 10, 0, 0), sig, acks)
			at.config.StrategyConfig.RiskControl.MentorMode = mentor
			label := day.Format("2006-01-02")
			if mentor {
				label += " mentor"
			} else {
				label += " day-plan"
			}

			before := hm(day, 12, 9, 30)
			setTraderClock(t, before)
			at.enforceEODFlatAt(before)
			if hasEvent(rt, "close_long:MNQ") || hasEvent(rt, "cancel:"+sig) {
				t.Fatalf("%s 12:09:30: nothing may be retired before the 12:10 flat, wire=%v", label, rt.snapshot())
			}

			flatAt := hm(day, 12, 10, 0)
			if !kernel.IsCMEOpen(flatAt) {
				t.Fatalf("premise: %s 12:10 is open", label)
			}
			setTraderClock(t, flatAt)
			go func() {
				time.Sleep(20 * time.Millisecond)
				acks <- ntwire.OrderUpdatePayload{SignalID: sig, State: "cancelled"}
			}()
			if !at.enforceEODFlatAt(flatAt) {
				t.Errorf("%s 12:10: enforceEODFlatAt must act", label)
			}
			if !hasEvent(rt, "close_long:MNQ") || !hasEvent(rt, "cancel:"+sig) {
				t.Errorf("%s: the open long and the resting arm must be retired at 12:10 CT, %d min before the 12:15 halt, while CME is open; wire=%v",
					label, earlyCloseFlatMarginMin, rt.snapshot())
			}
		}
	}
}

// The Day-Plan entry refusal (forceFlatWindowAt) reads the same pulled-in flat.
func TestShortenedDayForceFlatWindowRefusesFromTheFlat(t *testing.T) {
	day := ctDay(2026, 11, 27)
	acks := make(chan ntwire.OrderUpdatePayload, 4)
	at, _ := earlyCloseFixture(t, hm(day, 10, 0, 0), "sig-ffw", acks)
	none := []kernel.CTWindow{}
	if why, due := at.forceFlatWindowAt(hm(day, 12, 9, 0), &none); due {
		t.Errorf("12:09: not yet past the flat, got %q", why)
	}
	if why, due := at.forceFlatWindowAt(hm(day, 12, 10, 0), &none); !due || !strings.Contains(why, "12:10") {
		t.Errorf("12:10: past the shortened-day flat 12:10 CT, got due=%v %q", due, why)
	}
}

// Ordinary day: the NY flat stays at 14:45 (session end − 0), unchanged.
func TestOrdinaryDayEODFlatUnchanged(t *testing.T) {
	day := ctDay(2026, 11, 20)
	sig := "sig-ordflat"
	acks := make(chan ntwire.OrderUpdatePayload, 4)
	at, rt := earlyCloseFixture(t, hm(day, 10, 0, 0), sig, acks)
	at.config.StrategyConfig.RiskControl.MentorMode = true
	for _, now := range []time.Time{hm(day, 12, 10, 0), hm(day, 14, 44, 30)} {
		setTraderClock(t, now)
		at.enforceEODFlatAt(now)
		if len(rt.snapshot()) != 0 {
			t.Fatalf("ordinary %s: nothing may be retired before 14:45, wire=%v", now.Format("15:04:05"), rt.snapshot())
		}
	}
	now := hm(day, 14, 45, 0)
	setTraderClock(t, now)
	go func() {
		time.Sleep(20 * time.Millisecond)
		acks <- ntwire.OrderUpdatePayload{SignalID: sig, State: "cancelled"}
	}()
	at.enforceEODFlatAt(now)
	if !hasEvent(rt, "close_long:MNQ") {
		t.Errorf("ordinary 14:45: the NY flat must act, wire=%v", rt.snapshot())
	}
}

// swingFixture: a mentor trader holding one SWING4H position and one resting
// SWING4H mentor arm.
func swingFixture(t *testing.T, now time.Time, signalID string, acks <-chan ntwire.OrderUpdatePayload) (*AutoTrader, *wireRecorder) {
	t.Helper()
	trueV := true
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{
		PlanEnabled: true,
		Sessions:    []store.DayPlanSessionOverride{{Session: "NY", Enable: &trueV}},
	}}
	cfg.RiskControl.MentorMode = true
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
		EntryPrice: 30000, EntryTime: now.Add(-time.Hour).UnixMilli(),
		Status: "OPEN", CitedScenarioID: "swing-1795000000000",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.ArmedOrders().UpsertArm(&store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor:" + now.Format("2006-01-02"), Version: 1, Session: "NY",
		Scenario: "swing-1795000300000", Side: "long", EntryPx: 29950, StopPx: 29930, TargetPx: 29990,
		State: store.StateWorking, SignalID: signalID, Origin: store.ArmOriginMentor, Condition: "SWING4H",
	}); err != nil {
		t.Fatal(err)
	}
	return at, rt
}

// A SWING4H position/arm is exempt from the ordinary flat (held by the 4h), but
// not from a shortened day's: the 4h hold is capped at the early close, and the
// hold-close on the bar ending AT the close would go into a halted book.
func TestShortenedDayFlatIncludesSwing(t *testing.T) {
	day := ctDay(2026, 11, 27)
	sig := "sig-swing-1127"
	acks := make(chan ntwire.OrderUpdatePayload, 4)
	at, rt := swingFixture(t, hm(day, 11, 0, 0), sig, acks)
	now := hm(day, 12, 10, 0)
	setTraderClock(t, now)
	go func() {
		time.Sleep(20 * time.Millisecond)
		acks <- ntwire.OrderUpdatePayload{SignalID: sig, State: "cancelled"}
	}()
	at.enforceEODFlatAt(now)
	if !hasEvent(rt, "close_long:MNQ") || !hasEvent(rt, "cancel:"+sig) {
		t.Errorf("11-27 12:10: the SWING4H position and arm must be retired before the 12:15 early close, wire=%v", rt.snapshot())
	}

	ord := ctDay(2026, 11, 20)
	acks2 := make(chan ntwire.OrderUpdatePayload, 4)
	at2, rt2 := swingFixture(t, hm(ord, 14, 0, 0), "sig-swing-ord", acks2)
	now2 := hm(ord, 14, 50, 0)
	setTraderClock(t, now2)
	at2.enforceEODFlatAt(now2)
	if hasEvent(rt2, "close_long:MNQ") || hasEvent(rt2, "cancel:sig-swing-ord") {
		t.Errorf("ordinary 14:50: the SWING4H stays exempt from the EOD flat (N4), wire=%v", rt2.snapshot())
	}
}
