package trader

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vl/calendar"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// TestMentorNoChaseRule pins the pure rule: AT OR BEYOND the trigger the
// entry is skipped (he never enters at market, §3); strictly before it the
// entry passes. The mutant (inverting the comparison) fails this.
func TestMentorNoChaseRule(t *testing.T) {
	cases := []struct {
		name    string
		side    mentor.Side
		latest  float64
		trigger float64
		skip    bool
	}{
		{"long below passes", mentor.SideLong, 99.5, 100, false},
		{"long at trigger skips", mentor.SideLong, 100, 100, true},
		{"long through skips", mentor.SideLong, 100.25, 100, true},
		{"short above passes", mentor.SideShort, 100.5, 100, false},
		{"short at trigger skips", mentor.SideShort, 100, 100, true},
		{"short through skips", mentor.SideShort, 99.75, 100, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			skip, why := mentorNoChase(c.side, c.latest, c.trigger)
			if skip != c.skip {
				t.Fatalf("skip=%v want %v (%q)", skip, c.skip, why)
			}
			if skip && why == "" {
				t.Fatal("a skip must name why")
			}
		})
	}
}

// wireMentorPlacementSeams sets EVERY source seam to a wired, no-data state so
// the placement-level fail-closed check and the other gates stay open for a
// test that targets a later gate.
func wireMentorPlacementSeams(t *testing.T) {
	t.Helper()
	mentorDayNetSource = func() float64 { return 0 }
	mentorClosedProfitSource = func() bool { return false }
	mentorOpenStopSource = func() (float64, bool) { return 0, false }
	mentorOpenSideSource = func() string { return "" }
	mentorLegProtectedSource = func(leg string) bool { return true }
	mentorLatestPriceSource = func() (float64, bool) { return 0, false } // bound, inert: no-chase sees no price
	mentorConfluenceForIntent = func(in mentor.Intent) bool { return false }
	mentorSetArmExpiryWire = func(armID int64, expiryMs int64) error { return nil }
	mentorDayEventsForTest = func() ([]calendar.Event, bool) { return nil, true }
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline { return nil }
	mentorSourceDepthSource = func(name string) (int, bool) { return 9999, true }
	t.Cleanup(func() {
		mentorDayNetSource = nil
		mentorClosedProfitSource = nil
		mentorOpenStopSource = nil
		mentorOpenSideSource = nil
		mentorLegProtectedSource = nil
		mentorLatestPriceSource = nil
		mentorConfluenceForIntent = nil
		mentorSetArmExpiryWire = nil
		mentorDayEventsForTest = nil
		market.FuturesBarsProvider = nil
		mentorSourceDepthSource = nil
	})
}

// TestMentorNoChaseAtPlacementCallSite: the placement path consults the latest
// live price BEFORE sending. The mutant that drops the check makes the
// recorder fire on a through-price intent and this test goes RED.
func TestMentorNoChaseAtPlacementCallSite(t *testing.T) {
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	// 09:00 CT is inside the default trading window (b) — the other gates run
	// first and must stay open for this test.
	ct := kernel.CTLocation()
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, ct) }
	t.Cleanup(func() { mentorNowSource = nil })
	wireMentorPlacementSeams(t)

	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12}
	choice := mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}

	var placed []mentor.Intent
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed = append(placed, i) }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	// price already through the trigger → no placement, counted skip
	mentorLatestPriceSource = func() (float64, bool) { return 21000.50, true }
	t.Cleanup(func() { mentorLatestPriceSource = nil })
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if len(placed) != 0 {
		t.Fatalf("a through-price entry must NOT be placed (no chase), placed=%v", placed)
	}
	if got := MentorCountSnapshot()["no_chase_skip"]; got != 1 {
		t.Fatalf("the skip must be counted once, got %d", got)
	}

	// price still before the trigger → the placement proceeds
	mentorLatestPriceSource = func() (float64, bool) { return 20999.50, true }
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if len(placed) != 1 {
		t.Fatalf("a safe-price entry must reach the placement path, placed=%v", placed)
	}
}

// TestMentorWindowActivePure (b): the pure window check [D1.2 p1 @23:52–24:59].
func TestMentorWindowActivePure(t *testing.T) {
	ct := kernel.CTLocation()
	if active, why := mentorWindowActive("08:30", 60, time.Date(2026, 10, 2, 9, 0, 0, 0, ct)); !active || why != "" {
		t.Fatalf("09:00 CT is inside 08:30+60: active=%v why=%q", active, why)
	}
	if active, why := mentorWindowActive("08:30", 60, time.Date(2026, 10, 2, 9, 29, 0, 0, ct)); !active || why != "" {
		t.Fatalf("09:29 CT is inside the 08:30–09:30 window: active=%v why=%q", active, why)
	}
	if active, why := mentorWindowActive("08:30", 60, time.Date(2026, 10, 2, 10, 0, 0, 0, ct)); active || why == "" {
		t.Fatalf("10:00 CT is outside: active=%v why=%q", active, why)
	}
	if active, why := mentorWindowActive("08:30", 60, time.Date(2026, 10, 2, 9, 30, 0, 0, ct)); active || why == "" {
		t.Fatalf("09:30 CT is the exclusive end of the 08:30–09:30 window: active=%v why=%q", active, why)
	}
	for _, start := range []string{"25:00", "8-30", "", "bogus"} {
		t.Run("invalid_"+start, func(t *testing.T) {
			active, why := mentorWindowActive(start, 60, time.Date(2026, 10, 2, 9, 0, 0, 0, ct))
			if active || !strings.Contains(why, fmt.Sprintf("trading window start %q unparseable", start)) {
				t.Fatalf("invalid start %q must refuse with the fail-closed message: active=%v why=%q", start, active, why)
			}
		})
	}
}

// TestMentorWindowGateAtPlacementCallSite: outside the trading window no entry
// places; the SWING4H setup is exempt at any hour. The mutant that drops the
// gate makes the recorder fire at 10:00 CT and this test goes RED.
func TestMentorWindowGateAtPlacementCallSite(t *testing.T) {
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireMentorPlacementSeams(t)
	ct := kernel.CTLocation()
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 10, 0, 0, 0, ct) }
	t.Cleanup(func() { mentorNowSource = nil })

	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12}
	choice := mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}

	var placed int
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed++ }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	// 10:00 CT is outside the default 08:30–09:30 window: refused + counted.
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 0 {
		t.Fatalf("an entry outside the trading window must be refused, placed=%d", placed)
	}
	if got := MentorCountSnapshot()["window_refused"]; got != 1 {
		t.Fatalf("the window refusal must be counted once, got %d", got)
	}
	// the SWING setup is exempt at any hour (D5.2).
	sw := in
	sw.Setup = "SWING4H"
	at.mentorPlaceIntent(sw, choice, 1000, 1100)
	if placed != 1 {
		t.Fatalf("the SWING4H setup must be exempt from the window, placed=%d", placed)
	}
	// inside the window the same ISB proceeds.
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, ct) }
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 2 {
		t.Fatalf("inside the window the entry must proceed, placed=%d", placed)
	}
}

// TestMentorDoneAfterWinGateAtPlacementCallSite (a): a trade closed in profit
// with the day net positive ends the mentor's day. The mutant that drops the
// gate makes the recorder fire after a win and this test goes RED.
func TestMentorDoneAfterWinGateAtPlacementCallSite(t *testing.T) {
	if !mentorDoneAfterWin(10, true) || mentorDoneAfterWin(10, false) || mentorDoneAfterWin(-10, true) {
		t.Fatal("the pure rule: only a winning close AND a positive day end the day")
	}
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireMentorPlacementSeams(t)
	ct := kernel.CTLocation()
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, ct) }
	mentorDayNetSource = func() float64 { return 120 }
	mentorClosedProfitSource = func() bool { return true }
	t.Cleanup(func() {
		mentorNowSource = nil
		mentorDayNetSource = nil
		mentorClosedProfitSource = nil
	})

	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12}
	choice := mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}

	var placed int
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed++ }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	// a winning close + positive day → done for the day.
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 0 {
		t.Fatalf("after a win the entry must be refused, placed=%d", placed)
	}
	if got := MentorCountSnapshot()["done_after_win_refused"]; got != 1 {
		t.Fatalf("the done-after-win refusal must be counted once, got %d", got)
	}
	// no winning close → proceeds.
	mentorClosedProfitSource = func() bool { return false }
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 1 {
		t.Fatalf("without a winning close the entry must proceed, placed=%d", placed)
	}
	// the knob explicitly OFF → proceeds even after a win.
	off := false
	at.config.StrategyConfig.RiskControl.MentorDoneAfterWin = &off
	mentorClosedProfitSource = func() bool { return true }
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 2 {
		t.Fatalf("with the knob explicitly OFF the entry must proceed, placed=%d", placed)
	}
	// day net <= 0 → proceeds (knob back ON).
	on := true
	at.config.StrategyConfig.RiskControl.MentorDoneAfterWin = &on
	mentorDayNetSource = func() float64 { return -50 }
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 3 {
		t.Fatalf("a negative day must not end the mentor's day, placed=%d", placed)
	}
	// FAIL-CLOSED: missing P&L sources refuse the entry (an unknown is not
	// "no win") — asserted on the gate directly, since the placement-level
	// sources check refuses first at the full path.
	mentorDayNetSource = nil
	mentorClosedProfitSource = nil
	ResetMentorCountersForTest()
	if refuse, why := at.mentorDoneAfterWinGate(); !refuse || why == "" {
		t.Fatalf("missing P&L sources must refuse (fail-closed): refuse=%v why=%q", refuse, why)
	}
	if got := MentorCountSnapshot()["done_after_win_no_data"]; got != 1 {
		t.Fatalf("the fail-closed refusal must be counted done_after_win_no_data once, got %d", got)
	}
}

// TestMentorNeverWidenAtStopMoveCallSite (c): a stop amendment that increases
// open risk never reaches the wire. The mutant that drops the guard makes the
// widened stop reach moveStopWire and this test goes RED.
func TestMentorNeverWidenAtStopMoveCallSite(t *testing.T) {
	// pure
	if refuse, why := mentorNeverWiden("long", 100, 99); !refuse || why == "" {
		t.Fatalf("a lower long stop must be refused: refuse=%v why=%q", refuse, why)
	}
	if refuse, _ := mentorNeverWiden("long", 100, 101); refuse {
		t.Fatal("a higher long stop must pass")
	}
	if refuse, _ := mentorNeverWiden("long", 100, 100); refuse {
		t.Fatal("an equal stop must pass")
	}
	if refuse, _ := mentorNeverWiden("short", 100, 101); !refuse {
		t.Fatal("a higher short stop must be refused")
	}
	if refuse, _ := mentorNeverWiden("short", 100, 99); refuse {
		t.Fatal("a lower short stop must pass")
	}
	// call site: the widening move never reaches the wire.
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	// FAIL-CLOSED: no open-stop source (mentor mode ON) refuses the move.
	if err := at.mentorMoveStop(&ntTrader.TCPTrader{}, "long", 99); err == nil {
		t.Fatal("a stop move with no open-stop source must be refused (fail-closed)")
	}
	if got := MentorCountSnapshot()["stop_move_no_source"]; got != 1 {
		t.Fatalf("the fail-closed stop-move refusal must be counted once, got %d", got)
	}
	var sentSide string
	var sentPx float64
	moveStopWire = func(nt *ntTrader.TCPTrader, side string, newStop float64) error {
		sentSide, sentPx = side, newStop
		return nil
	}
	t.Cleanup(func() { moveStopWire = nil })
	mentorOpenStopSource = func() (float64, bool) { return 100, true }
	t.Cleanup(func() { mentorOpenStopSource = nil })

	if err := at.mentorMoveStop(&ntTrader.TCPTrader{}, "long", 99); err == nil {
		t.Fatal("a widening long move must be refused")
	}
	if sentPx != 0 {
		t.Fatalf("the widened stop must not reach the wire, got %.2f", sentPx)
	}
	if got := MentorCountSnapshot()["widen_refused"]; got != 1 {
		t.Fatalf("the widen refusal must be counted once, got %d", got)
	}
	if err := at.mentorMoveStop(&ntTrader.TCPTrader{}, "long", 101); err != nil || sentSide != "long" || sentPx != 101 {
		t.Fatalf("a tightening move must reach the wire: err=%v side=%q px=%.2f", err, sentSide, sentPx)
	}
}

// TestMentorNeverAddAtPlacementCallSite (d): no second same-direction fill
// while a position is open — the resonance ISB is a hold signal, not an entry.
// The mutant that drops the gate makes the recorder fire and this test goes RED.
func TestMentorNeverAddAtPlacementCallSite(t *testing.T) {
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireMentorPlacementSeams(t)
	ct := kernel.CTLocation()
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, ct) }
	mentorOpenSideSource = func() string { return "long" }
	t.Cleanup(func() { mentorNowSource = nil; mentorOpenSideSource = nil })

	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12}
	choice := mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}

	var placed int
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed++ }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	// the resonance ISB while long is already open: a hold, NOT an entry.
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 0 {
		t.Fatalf("a same-direction fill while long is open must be refused (never add/average), placed=%d", placed)
	}
	if got := MentorCountSnapshot()["add_refused"]; got != 1 {
		t.Fatalf("the never-add refusal must be counted once, got %d", got)
	}
	// the opposite side proceeds.
	in.Side = mentor.SideShort
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 1 {
		t.Fatalf("an opposite-side entry must proceed, placed=%d", placed)
	}
	// FAIL-CLOSED: no open-side source refuses the entry — asserted on the
	// gate directly (the placement-level sources check refuses first at the
	// full path).
	mentorOpenSideSource = nil
	ResetMentorCountersForTest()
	if refuse, why := at.mentorAddGate(in); !refuse || why == "" {
		t.Fatalf("a missing open-side source must refuse (fail-closed): refuse=%v why=%q", refuse, why)
	}
	if got := MentorCountSnapshot()["add_no_source"]; got != 1 {
		t.Fatalf("the fail-closed refusal must be counted add_no_source once, got %d", got)
	}
}

// TestMentorSourcesMissingRefusesPlacement: with any mentor source seam
// missing, EVERY entry refuses at the placement call site (mentor_sources_missing).
// The mutant that removes the check makes the recorder fire with a nil seam.
func TestMentorSourcesMissingRefusesPlacement(t *testing.T) {
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireMentorPlacementSeams(t)
	ct := kernel.CTLocation()
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, ct) }
	t.Cleanup(func() { mentorNowSource = nil })

	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12}
	choice := mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}

	var placed int
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed++ }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	// all wired → proceeds.
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 1 {
		t.Fatalf("all seams wired: the placement must proceed, placed=%d", placed)
	}
	// one seam missing → every entry refuses at the placement call site.
	mentorOpenStopSource = nil
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 1 {
		t.Fatalf("a missing seam must refuse every entry, placed=%d", placed)
	}
	if got := MentorCountSnapshot()["mentor_sources_missing"]; got != 1 {
		t.Fatalf("the placement-level refusal must be counted mentor_sources_missing once, got %d", got)
	}
	// restore → proceeds again.
	mentorOpenStopSource = func() (float64, bool) { return 0, false }
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 2 {
		t.Fatalf("with the seam restored the placement must proceed, placed=%d", placed)
	}
}

// TestMentorIntentExpiry (N12, PR #313): the expiry belongs to the rules — an
// intent-carried expiry wins; a level touch or a single ISB expires at the
// close of the NEXT 1m candle; the swing lives until the close of the current
// 4h candle (mentorSwingExpiry).
func TestMentorIntentExpiry(t *testing.T) {
	ct := kernel.CTLocation()
	base := time.Date(2026, 10, 2, 8, 0, 0, 0, ct).UnixMilli()
	if got := mentorIntentExpiry(mentor.Intent{ExpiryMs: 123456}, base); got != 123456 {
		t.Fatalf("an intent-carried expiry must win, got %d", got)
	}
	if got := mentorIntentExpiry(mentor.Intent{Setup: "ISB"}, base); got != base+60_000 {
		t.Fatalf("a single ISB expires at the next 1m candle close, got %d", got)
	}
	if got := mentorIntentExpiry(mentor.Intent{Setup: "PHL"}, base); got != base+60_000 {
		t.Fatalf("a level touch expires at the next 1m candle close, got %d", got)
	}
	// 08:00 CT → the current 4h candle is [05:00, 09:00) → the swing expires
	// at 09:00 CT.
	want := time.Date(2026, 10, 2, 9, 0, 0, 0, ct).UnixMilli()
	if got := mentorIntentExpiry(mentor.Intent{Setup: "SWING4H"}, base); got != want {
		t.Fatalf("the swing expires at the 4h candle close 09:00 CT, got %d want %d", got, want)
	}
}

// TestMentorSwingExpiry (RULING [C]): the 4h candles chain from the 17:00 CT
// anchor — the swing order lives until the close of the CURRENT 4h candle.
func TestMentorSwingExpiry(t *testing.T) {
	ct := kernel.CTLocation()
	cases := []struct {
		hhmm    string // wall clock CT on 2026-10-02
		want    string // the 4h candle close, HH:MM CT (the next day where it chains)
		wantDay string
	}{
		{"00:15", "01:00", "2026-10-02"},
		{"01:15", "05:00", "2026-10-02"},
		{"08:00", "09:00", "2026-10-02"},
		{"11:30", "13:00", "2026-10-02"},
		{"15:00", "17:00", "2026-10-02"},
		{"17:00", "21:00", "2026-10-02"},
		{"19:00", "21:00", "2026-10-02"},
		{"22:30", "01:00", "2026-10-03"},
	}
	for _, c := range cases {
		t.Run(c.hhmm, func(t *testing.T) {
			var hh, mm int
			if _, err := fmt.Sscanf(c.hhmm, "%d:%d", &hh, &mm); err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 10, 2, hh, mm, 0, 0, ct)
			var wh, wm int
			fmt.Sscanf(c.want, "%d:%d", &wh, &wm)
			wd := now
			if c.wantDay != now.Format("2006-01-02") {
				wd, _ = time.ParseInLocation("2006-01-02", c.wantDay, ct)
			}
			want := time.Date(wd.Year(), wd.Month(), wd.Day(), wh, wm, 0, 0, ct).UnixMilli()
			if got := mentorSwingExpiry(now.UnixMilli()); got != want {
				t.Fatalf("mentorSwingExpiry(%s CT) = %s, want %s CT", c.hhmm,
					time.UnixMilli(got).In(ct).Format("15:04"), c.want)
			}
		})
	}
}

// TestMentorPlacementCarriesExpiry: the placement path carries an expiry on
// every order — the intent's own when the evaluator set it, else the setup
// default. The mutant that drops the expiry default makes the recorder see 0.
func TestMentorPlacementCarriesExpiry(t *testing.T) {
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireMentorPlacementSeams(t)
	ct := kernel.CTLocation()
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, ct) }
	t.Cleanup(func() { mentorNowSource = nil })

	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12}
	choice := mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}
	barCloseMs := int64(1_700_000_000_000)

	var placed []mentor.Intent
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed = append(placed, i) }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	// no carried expiry → the injector sets the next-1m-close default.
	at.mentorPlaceIntent(in, choice, barCloseMs, 1100)
	if len(placed) != 1 || placed[0].ExpiryMs != barCloseMs+60_000 {
		t.Fatalf("the placement must carry the setup default expiry, placed=%+v", placed)
	}
	if got := MentorCountSnapshot()["expiry_defaulted"]; got != 1 {
		t.Fatalf("the injector-computed expiry must be counted once, got %d", got)
	}
	// a carried expiry (the evaluator's stacking extension) is preserved.
	in.ExpiryMs = barCloseMs + 3*60_000
	at.mentorPlaceIntent(in, choice, barCloseMs, 1100)
	if len(placed) != 2 || placed[1].ExpiryMs != barCloseMs+3*60_000 {
		t.Fatalf("an intent-carried expiry must be preserved, placed=%+v", placed)
	}
}

// TestMentorHistoryDepthGate (P0): mentor mode must never trade on a cold EMA
// or truncated levels — with any history source short or unknown EVERY entry
// refuses (fail-closed) and the boot line names the gap. The mutant that drops
// the per-source depth loop makes the recorder fire on a short history.
func TestMentorHistoryDepthGate(t *testing.T) {
	ResetMentorCountersForTest()
	// pin the 4h floor at the absolute minimum for this test's (33/34) row —
	// the warm-up default is covered by TestMentor4hEMA34WarmupGate.
	oldWarmup := mentor4hEMA34Warmup
	t.Cleanup(func() { mentor4hEMA34Warmup = oldWarmup })
	mentor4hEMA34Warmup = mentorMin4hEMA34
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireMentorPlacementSeams(t)
	ct := kernel.CTLocation()
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, ct) }
	t.Cleanup(func() { mentorNowSource = nil })

	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12}
	choice := mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}
	var placed int
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed++ }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	// every source at/above its floor → proceeds.
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 1 {
		t.Fatalf("with every source seeded the placement must proceed, placed=%d", placed)
	}
	// one source short (the 4h EMA 34 at 33) → refuses + names it.
	mentorSourceDepthSource = func(name string) (int, bool) {
		if name == "4h EMA34" {
			return 33, true
		}
		return 9999, true
	}
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 1 {
		t.Fatalf("a short 4h EMA history must refuse the entry, placed=%d", placed)
	}
	if got := MentorCountSnapshot()["mentor_sources_missing"]; got != 1 {
		t.Fatalf("the depth refusal must be counted once, got %d", got)
	}
	line := MentorSourcesBootLine(map[string]*AutoTrader{"t1": at})
	if !textHas(line, "4h EMA34 (33/34)") {
		t.Fatalf("the boot line must name the short source: %q", line)
	}
	// an unknown source (not seeded yet) refuses too, printed as n/a.
	mentorSourceDepthSource = func(name string) (int, bool) {
		if name == "1h level set" {
			return 0, false
		}
		return 9999, true
	}
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 1 {
		t.Fatalf("an unknown level-set source must refuse the entry, placed=%d", placed)
	}
	if line := MentorSourcesBootLine(map[string]*AutoTrader{"t1": at}); !textHas(line, "1h level set (n/a)") {
		t.Fatalf("the boot line must print n/a for the unknown source: %q", line)
	}
	// restore → proceeds again.
	mentorSourceDepthSource = func(name string) (int, bool) { return 9999, true }
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 2 {
		t.Fatalf("with the source restored the placement must proceed, placed=%d", placed)
	}
}

// TestMentor4hEMA34WarmupGate (P0 seed change): the 4h EMA 34 floor is the
// seed's stated warm-up — the gate reads the constant, never the hard-coded
// 34. The mutant that hard-codes 34 fails the larger-warm-up rows.
func TestMentor4hEMA34WarmupGate(t *testing.T) {
	old := mentor4hEMA34Warmup
	t.Cleanup(func() { mentor4hEMA34Warmup = old })
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireMentorPlacementSeams(t)
	// the SAFE default (3×34 = 102): 101 refused, 102 passes.
	if mentor4hEMA34Warmup != 102 {
		t.Fatalf("the default 4h warm-up must be 102, got %d", mentor4hEMA34Warmup)
	}
	mentorSourceDepthSource = func(name string) (int, bool) {
		if name == "4h EMA34" {
			return 101, true
		}
		return 9999, true
	}
	if missing := strings.Join(at.mentorSourcesMissing(), ", "); !textHas(missing, "4h EMA34 (101/102)") {
		t.Fatalf("101 candles must be refused against the default 102 warm-up: %q", missing)
	}
	// the 1m EMA 34 floor shares the same safe default.
	mentorSourceDepthSource = func(name string) (int, bool) {
		if name == "1m EMA34" {
			return 101, true
		}
		return 9999, true
	}
	if missing := strings.Join(at.mentorSourcesMissing(), ", "); !textHas(missing, "1m EMA34 (101/102)") {
		t.Fatalf("101 1m bars must be refused against the default 102 floor: %q", missing)
	}
	mentorSourceDepthSource = func(name string) (int, bool) { return 9999, true }
	if missing := at.mentorSourcesMissing(); len(missing) != 0 {
		t.Fatalf("102 everywhere must pass the defaults: %q", strings.Join(missing, ", "))
	}
	// absolute minimum 34: below it refuses.
	mentor4hEMA34Warmup = mentorMin4hEMA34
	mentorSourceDepthSource = func(name string) (int, bool) {
		if name == "4h EMA34" {
			return 33, true
		}
		return 9999, true
	}
	if missing := strings.Join(at.mentorSourcesMissing(), ", "); !textHas(missing, "4h EMA34 (33/34)") {
		t.Fatalf("33 candles must be refused against the 34 floor: %q", missing)
	}
	// DS-103 states a larger warm-up (100): the gate reads the constant.
	SetMentor4hEMA34Warmup(100)
	mentorSourceDepthSource = func(name string) (int, bool) {
		if name == "4h EMA34" {
			return 50, true
		}
		return 9999, true
	}
	if missing := strings.Join(at.mentorSourcesMissing(), ", "); !textHas(missing, "4h EMA34 (50/100)") {
		t.Fatalf("50 candles must be refused against the 100 warm-up: %q", missing)
	}
	mentorSourceDepthSource = func(name string) (int, bool) {
		if name == "4h EMA34" {
			return 100, true
		}
		return 9999, true
	}
	if missing := at.mentorSourcesMissing(); len(missing) != 0 {
		t.Fatalf("100 candles must pass the 100 warm-up: %q", strings.Join(missing, ", "))
	}
	// a warm-up below the absolute minimum clamps to 34.
	SetMentor4hEMA34Warmup(10)
	if mentor4hEMA34Warmup != mentorMin4hEMA34 {
		t.Fatalf("the warm-up must clamp to the absolute minimum 34, got %d", mentor4hEMA34Warmup)
	}
}

// TestMentorSourcesBootLine: the boot wiring check — with mentor_mode ON every
// source seam must be non-nil, or mentor_mode refuses to arm and ONE error line
// names the missing seam. Each seam is tested nil in turn.
func TestMentorSourcesBootLine(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireAll := func() {
		mentorDayNetSource = func() float64 { return 0 }
		mentorClosedProfitSource = func() bool { return false }
		mentorOpenStopSource = func() (float64, bool) { return 0, false }
		mentorOpenSideSource = func() string { return "" }
		mentorLegProtectedSource = func(leg string) bool { return true }
		mentorLatestPriceSource = func() (float64, bool) { return 0, false }
		mentorNowSource = func() time.Time { return time.Now() }
		mentorConfluenceForIntent = func(in mentor.Intent) bool { return false }
		mentorSetArmExpiryWire = func(armID int64, expiryMs int64) error { return nil }
		mentorDayEventsForTest = func() ([]calendar.Event, bool) { return nil, true }
		market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline { return nil }
		mentorSourceDepthSource = func(name string) (int, bool) { return 9999, true }
	}
	clearAll := func() {
		mentorDayNetSource = nil
		mentorClosedProfitSource = nil
		mentorOpenStopSource = nil
		mentorOpenSideSource = nil
		mentorLegProtectedSource = nil
		mentorLatestPriceSource = nil
		mentorNowSource = nil
		mentorConfluenceForIntent = nil
		mentorSetArmExpiryWire = nil
		mentorDayEventsForTest = nil
		market.FuturesBarsProvider = nil
		mentorSourceDepthSource = nil
	}
	t.Cleanup(clearAll)
	loaded := map[string]*AutoTrader{"t1": at}

	wireAll()
	if line := MentorSourcesBootLine(loaded); !textHas(line, "wired") {
		t.Fatalf("all seams wired: %q", line)
	}
	clearAll()
	if line := MentorSourcesBootLine(loaded); !textHas(line, "MISSING") {
		t.Fatalf("nothing wired must report MISSING: %q", line)
	}
	for _, c := range []struct {
		name  string
		clear func()
		want  string
	}{
		{"day net", func() { mentorDayNetSource = nil }, "day net"},
		{"closed profit", func() { mentorClosedProfitSource = nil }, "closed profit"},
		{"open stop", func() { mentorOpenStopSource = nil }, "open_stop"},
		{"open side", func() { mentorOpenSideSource = nil }, "open_side"},
		{"news events", func() { mentorDayEventsForTest = nil }, "news events"},
		{"5m feed", func() { market.FuturesBarsProvider = nil }, "5m feed"},
		{"4h EMA34", func() {
			mentorSourceDepthSource = func(name string) (int, bool) {
				if name == "4h EMA34" {
					return 0, false
				}
				return 9999, true
			}
		}, "4h EMA34"},
	} {
		t.Run(c.name, func(t *testing.T) {
			wireAll()
			c.clear()
			line := MentorSourcesBootLine(loaded)
			if !textHas(line, c.want) {
				t.Fatalf("a missing %s must be named in %q", c.want, line)
			}
		})
	}
	// a non-mentor trader is not reported.
	if line := MentorSourcesBootLine(map[string]*AutoTrader{"off": mentoredTrader(t, store.RiskControlConfig{})}); textHas(line, "off") {
		t.Fatalf("a non-mentor trader must not be reported: %q", line)
	}
}

// TestMentorEventPassDedup: the event pass evaluates each FINAL bar exactly
// once (a second pass over the same bar is a no-op).
func TestMentorEventPassDedup(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	old := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = old })
	open := int64(1_700_000_000_000)
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline {
		return []market.Kline{
			{OpenTime: open, CloseTime: open + 59_000, Open: 100, High: 101, Low: 99, Close: 100.5},
		}
	}
	if !at.mentorEventPassAt(time.Now()) {
		t.Fatal("the first pass over a new FINAL bar must run")
	}
	if at.mentorEventPassAt(time.Now()) {
		t.Fatal("a second pass over the same bar must be a no-op")
	}
}

// TestMentorLatencySnapshotAndBootLine: p50/p95 from the reservoir; the boot
// line prints n/a until the first sample (canon L7).
func TestMentorLatencySnapshotAndBootLine(t *testing.T) {
	ResetMentorLatencyForTest()
	if line := MentorLatencyBootLine(); line == "" || !textHas(line, "n/a") {
		t.Fatalf("empty reservoir boot line must print n/a, got %q", line)
	}
	base := int64(1_000_000)
	for i := int64(0); i < 100; i++ {
		recordMentorLatency(base, base+10, base+20, base+100+i) // 100..199 ms
	}
	n, p50, p95 := MentorLatencySnapshot()
	if n != 100 {
		t.Fatalf("n=%d want 100", n)
	}
	if p50 < 145 || p50 > 155 {
		t.Fatalf("p50=%dms, want ~150ms", p50)
	}
	if p95 < 190 || p95 > 199 {
		t.Fatalf("p95=%dms, want ~195ms", p95)
	}
	if line := MentorLatencyBootLine(); textHas(line, "n/a") {
		t.Fatalf("a populated reservoir must not print n/a: %q", line)
	}
}

func textHas(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestMentorStrongDayFrom5m (S9, D5.2 p2 @05:21): a closed 5m candle in the
// window running 50–80 pts marks a strong day → the size table cuts to 1–2.
func TestMentorStrongDayFrom5m(t *testing.T) {
	mk := func(rng float64) market.Kline { return market.Kline{High: 100 + rng, Low: 100} }
	if mentorStrongDayFrom5m(nil) {
		t.Fatal("no bars must not be a strong day")
	}
	if mentorStrongDayFrom5m([]market.Kline{mk(49.75)}) {
		t.Fatal("a 49.75-pt 5m candle must NOT trigger the strong day")
	}
	for _, rng := range []float64{50, 60, 80, 85} {
		if !mentorStrongDayFrom5m([]market.Kline{mk(rng)}) {
			t.Fatalf("a %.1f-pt 5m candle IS a strong day", rng)
		}
	}
	if !mentorStrongDayFrom5m([]market.Kline{mk(20), mk(25), mk(55)}) {
		t.Fatal("a 55-pt candle anywhere in the 5m window qualifies")
	}
}

// TestMentorNewsHold (F11): the pure gate refuses a placement inside the
// 07:30 CT CPI/PPI/Unemployment print window on a T1 print day, and only then.
func TestMentorNewsHold(t *testing.T) {
	ct := kernel.CTLocation()
	event := func(title string, impact calendar.Impact, hh, mm int) calendar.Event {
		return calendar.Event{Title: title, Impact: impact,
			Time: time.Date(2026, 10, 2, hh, mm, 0, 0, ct).UTC()}
	}
	cpi := []calendar.Event{event("CPI m/m", calendar.T1, 7, 30)}
	cases := []struct {
		name string
		evs  []calendar.Event
		now  time.Time
		want bool
	}{
		{"window start", cpi, time.Date(2026, 10, 2, 7, 20, 0, 0, ct), true},
		{"window end exclusive", cpi, time.Date(2026, 10, 2, 7, 35, 0, 0, ct), false},
		{"just before the window", cpi, time.Date(2026, 10, 2, 7, 19, 0, 0, ct), false},
		{"inside", cpi, time.Date(2026, 10, 2, 7, 28, 0, 0, ct), true},
		{"after", cpi, time.Date(2026, 10, 2, 7, 36, 0, 0, ct), false},
		{"PPI", []calendar.Event{event("PPI m/m", calendar.T1, 7, 30)}, time.Date(2026, 10, 2, 7, 25, 0, 0, ct), true},
		{"unemployment", []calendar.Event{event("Unemployment Rate", calendar.T1, 7, 30)}, time.Date(2026, 10, 2, 7, 25, 0, 0, ct), true},
		{"T2 print does not hold", []calendar.Event{event("CPI m/m", calendar.T2, 7, 30)}, time.Date(2026, 10, 2, 7, 25, 0, 0, ct), false},
		{"other minute", []calendar.Event{event("CPI m/m", calendar.T1, 9, 0)}, time.Date(2026, 10, 2, 7, 25, 0, 0, ct), false},
		{"other title", []calendar.Event{event("GDP q/q", calendar.T1, 7, 30)}, time.Date(2026, 10, 2, 7, 25, 0, 0, ct), false},
		{"no events", nil, time.Date(2026, 10, 2, 7, 25, 0, 0, ct), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hold, why := mentorNewsHold(c.evs, c.now)
			if hold != c.want {
				t.Fatalf("hold=%v why=%q, want %v", hold, why, c.want)
			}
			if hold && why == "" {
				t.Fatal("a hold must carry the why")
			}
		})
	}
}

// TestMentorNewsGateAtPlacementCallSite: the placement path consults the news
// gate; a missing/unreadable calendar holds the window FAIL-CLOSED. The mutant
// that removes the gate makes the recorder fire inside the print window and
// this test goes RED.
func TestMentorNewsGateAtPlacementCallSite(t *testing.T) {
	ResetMentorCountersForTest()
	// the trading window (b) runs first: 07:00–09:00 CT covers every now below.
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorWindowStart: "07:00", MentorWindowMinutes: 120})
	wireMentorPlacementSeams(t)

	ct := kernel.CTLocation()
	cpi := []calendar.Event{{Title: "CPI m/m", Impact: calendar.T1,
		Time: time.Date(2026, 10, 2, 7, 30, 0, 0, ct).UTC()}}
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 7, 25, 0, 0, ct) }
	mentorDayEventsForTest = func() ([]calendar.Event, bool) { return cpi, true }
	t.Cleanup(func() { mentorNowSource = nil; mentorDayEventsForTest = nil })

	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12}
	choice := mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}

	var placed int
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed++ }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	// inside the 07:30 CPI print window: the recorder must NOT fire.
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 0 {
		t.Fatalf("a placement inside the 07:30 print window must be refused, placed=%d", placed)
	}
	if got := MentorCountSnapshot()["news_hold"]; got != 1 {
		t.Fatalf("the news hold must be counted once, got %d", got)
	}
	// outside the print window (08:55 CT) the same intent proceeds.
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 8, 55, 0, 0, ct) }
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 1 {
		t.Fatalf("outside the print window the placement must proceed, placed=%d", placed)
	}
	// FAIL-CLOSED: no readable calendar inside the window holds + counts.
	mentorDayEventsForTest = func() ([]calendar.Event, bool) { return nil, false }
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 7, 25, 0, 0, ct) }
	ResetMentorCountersForTest()
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 1 {
		t.Fatalf("no calendar inside the window must hold (fail-closed), placed=%d", placed)
	}
	if got := MentorCountSnapshot()["news_hold_no_calendar"]; got != 1 {
		t.Fatalf("the fail-closed hold must be counted news_hold_no_calendar once, got %d", got)
	}
	// no calendar OUTSIDE the window proceeds.
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 8, 55, 0, 0, ct) }
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 2 {
		t.Fatalf("no calendar outside the window must proceed, placed=%d", placed)
	}
}
