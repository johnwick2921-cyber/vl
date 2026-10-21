package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── P1-1 (rel22 mentor audit) — the ISB "keep" must win the expiry race ─────
//
// The course [D4.2 p2 @ 08:21–09:01; 00-METHOD §2.1 stacking table]: when the
// candle after the inside bar does not fill but its BODY stays inside I1, KEEP
// the order (stack candles 2 and 3) — "một cái EXCUSE DUY NHẤT" [@ 09:41].
//
// Every mentor ISB arm is authored with expiry = the close of the NEXT candle,
// i.e. exactly the instant the evaluator decides keep-or-cancel. On the live
// box the FINAL frame for that candle arrives a fraction of a second AFTER the
// wall-clock close, and the event loop then ran the ARMED pass first: the row
// was already past its expiry, so the sweep cancelled it at NT8 — and the
// mentor pass's ExtendArm, a few milliseconds later, was refused "row N is not
// an unfilled arm". 16 resting orders died this way (rows 253, 257, 258, 262,
// 266, 267, 270, 272, 275, 279, 309, 319, 322, 326, 357, 358), every one while
// the trader's active plan carried an enabled market_in_zone arm (that flag is
// what makes the event loop run its armed pass at all).
//
// This pin drives the REAL event-loop goroutine (startArmedEventLoop → poke),
// with the trader in exactly that state: mentor mode ON, the day plan ON, a
// zone arm active, a resting (working, unfilled) mentor ISB order whose expiry
// is the close of the candle that just closed, and an evaluator still holding
// that ISB arm with the new candle's body inside I1. The keep must land: the
// row stays working and its expiry moves to the close of the NEXT candle.
//
// MUTANT: put the armed pass back in front of the mentor pass in
// runArmedEventLoop → the row is cancel_pending and this test goes RED.
func TestMentorISBKeepWinsTheExpiryRace(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	at, st, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	resetMentorCounters(at)

	// The planner side of the trader: day plan ON + an NT8 account + running,
	// so armedEventPassAt is live exactly as it was for trader hoang on 10-13
	// 22:19 CT (ASIA v4 active, S1 market_in_zone enabled).
	at.config.StrategyConfig.DayPlan = &store.DayPlanConfig{PlanEnabled: true}
	if err := st.Trader().Create(&store.Trader{ID: at.id, Name: at.id, Account: "Sim101"}); err != nil {
		t.Fatal(err)
	}
	at.isRunningMutex.Lock()
	at.isRunning = true
	at.isRunningMutex.Unlock()
	at.zoneArmActive.Store(true)

	// The tape: I1 is the inside-bar candle the arm was authored on; cur is
	// stack candle 2, whose BODY sits inside I1 → keep. The bar before I1 is
	// the mother candle. A FINAL frame for cur arrives 400 ms after its close.
	rth := rthInstant()                    // 10:30:00 CT
	curOpen := rth.Add(-time.Minute)       // 10:29:00
	now := rth.Add(400 * time.Millisecond) // the Final frame's arrival
	bar := func(open time.Time, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: open.UnixMilli(), CloseTime: open.Add(time.Minute).UnixMilli() - 1,
			Open: o, High: h, Low: l, Close: c, Final: true}
	}
	var bars []market.Kline
	for i := 40; i >= 3; i-- {
		px := 29590.0 + float64(i%3)
		bars = append(bars, bar(curOpen.Add(-time.Duration(i)*time.Minute), px, px+1, px-1, px+0.5))
	}
	mother := bar(curOpen.Add(-2*time.Minute), 29590, 29604, 29586, 29600) // green: the ISB direction is long
	i1 := bar(curOpen.Add(-time.Minute), 29598, 29601, 29592, 29596)
	cur := bar(curOpen, 29595, 29599, 29594, 29597) // body 29595–29597 inside I1 [29592, 29601]
	bars = append(bars, mother, i1, cur)
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline { return bars }

	oldNow := getMentorNowSource()
	setMentorNowSource(func() time.Time { return now })
	t.Cleanup(func() { setMentorNowSource(oldNow) })
	at.armedEventNowForTest = func() time.Time { return now }

	// The resting order: placed on the I1 close with expiry = the close of the
	// NEXT candle (cur) — mentorIntentExpiry's ISB rule — and WORKING at NT8.
	const armID = "isb-race"
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: armID + "-1", Side: "long", EntryPx: 29602.5, StopPx: 29590.5, TargetPx: 29640,
		Kind: "stop_entry", Condition: "ISB", Origin: store.ArmOriginMentor, ExpiryMs: cur.CloseTime, State: store.StateArmed}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	if err := ledger.BeginPlacement(row.ID, "sig-isb-race"); err != nil {
		t.Fatal(err)
	}
	if err := ledger.SetState(row.ID, store.StateWorking, "test receipt: resting at NT8"); err != nil {
		t.Fatal(err)
	}
	at.armedTrader().GetServer().OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101",
		Orders: []ntwire.NT8Order{{Name: "sig-isb-race", State: "Working", Type: "stop_limit"}}}, now)
	mentorRegisterLiveArmForTest(armID, at.id, row.ID, "long", row.EntryPx)

	// The evaluator still holds the ISB arm (placed on I1, no candle counted
	// yet) and has not ticked cur.
	at.mentorEval = mentor.New(at.mentorEvaluatorConfig())
	at.mentorEval.State.ISBArms[armID] = mentor.ISBArm{FirstBar: i1, Inside: 0, Side: mentor.SideLong}
	at.mentorLastTickOpen = i1.OpenTime

	at.startArmedEventLoop()
	t.Cleanup(at.stopArmedEventLoop)
	l := at.armedEvent.Load()
	if l == nil {
		t.Fatal("fixture: the event loop did not start")
	}
	l.poke() // the FINAL frame for cur

	var got store.ArmedOrderDB
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if l.passes.Load() >= 1 && at.mentorCountSnapshot()["extend_ok"]+at.mentorCountSnapshot()["extend_refused"]+
			at.mentorCountSnapshot()["extend_lost_expiry_race"] > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	at.stopArmedEventLoop() // no further pass may run after the read below
	if err := ledger.DB().First(&got, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	want := cur.CloseTime + 60_000
	if got.State != store.StateWorking || got.ExpiryMs != want {
		t.Fatalf("the mentor keep LOST the expiry race: row %d state=%q reason=%q expiry=%d (want state=working expiry=%d — the close of stack candle 3) · counters=%v [P1-1; D4.2 p2 @ 08:21–09:01]",
			got.ID, got.State, got.StateReason, got.ExpiryMs, want, at.mentorCountSnapshot())
	}
	if c := at.mentorCountSnapshot()["extend_ok"]; c != 1 {
		t.Fatalf("extend_ok = %d, want 1 (the keep must reach the ledger)", c)
	}
	if strings.Contains(got.StateReason, "expiry") {
		t.Fatalf("the row carries an expiry reason %q — the sweep reached it", got.StateReason)
	}
}

// TestMentorExtendArmNamesWhyTheKeepCouldNotLand — item 4 (rel22: every refusal
// path is logged AND counted, from the row's OWN state). At the production
// call site (mentorDispatchIntent → mentorExtendArm):
//   - a row the expiry sweep already took ("stop-limit expiry elapsed") counts
//     extend_lost_expiry_race — the P1-1 race, counted apart so a recurrence is
//     visible on its own;
//   - a FILLED arm counts extend_skipped_filled (nothing to keep — 7 of the 39
//     live "REFUSED … not an unfilled arm" lines were this benign case: rows
//     252, 274, 278, 305, 310 ×2, 316);
//   - anything else stays extend_refused, with the state and reason printed.
//
// MUTANT: mentorRowTakenByExpiry → false: the race row lands in extend_refused
// and this test goes RED.
func TestMentorExtendArmNamesWhyTheKeepCouldNotLand(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	mk := func(armID string) store.ArmedOrderDB {
		t.Helper()
		row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: armID + "-1",
			Side: "long", EntryPx: 29600, StopPx: 29595, TargetPx: 29610, Kind: "stop_entry", Condition: "ISB",
			Origin: store.ArmOriginMentor, State: store.StateArmed, ExpiryMs: time.Now().UnixMilli() - 1}
		if err := ledger.UpsertArm(&row); err != nil {
			t.Fatal(err)
		}
		if err := ledger.BeginPlacement(row.ID, "sig-"+armID); err != nil {
			t.Fatal(err)
		}
		mentorRegisterLiveArmForTest(armID, at.id, row.ID, "long", row.EntryPx)
		return row
	}
	extend := func(armID string) map[string]int {
		resetMentorCounters(at)
		at.mentorDispatchIntent(mentor.Intent{Action: mentor.ExtendArm, ArmID: armID, ExpiryMs: time.Now().UnixMilli() + 60_000,
			Reason: "ISB stacking: inside I1 — extend the expiry"}, mentorTierInputs{}, 1000, 1100)
		return at.mentorCountSnapshot()
	}

	raced := mk("isb-raced")
	if err := ledger.RequestCancel(raced.ID, "stop-limit expiry elapsed", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if c := extend("isb-raced"); c["extend_lost_expiry_race"] != 1 || c["extend_refused"] != 0 {
		t.Fatalf("a row the expiry sweep took must count extend_lost_expiry_race (not extend_refused): %v", c)
	}

	filled := mk("isb-filled")
	if err := ledger.SetState(filled.ID, store.StateFilled, "fill@29600.00"); err != nil {
		t.Fatal(err)
	}
	if c := extend("isb-filled"); c["extend_skipped_filled"] != 1 || c["extend_refused"] != 0 {
		t.Fatalf("a FILLED arm must count extend_skipped_filled (nothing to keep): %v", c)
	}

	other := mk("isb-other")
	if err := ledger.RequestCancel(other.ID, "mentor: day-stop never-add", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if c := extend("isb-other"); c["extend_refused"] != 1 || c["extend_lost_expiry_race"] != 0 {
		t.Fatalf("a row cancelled for another reason stays extend_refused: %v", c)
	}
}
