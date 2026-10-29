package trader

import (
	"strings"
	"sync"
	"testing"
	"time"

	"vl/internal/ownerpush"
	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// m442 - owner alerts must be TRUE and must REACH the owner. These pins drive
// the PRODUCTION call sites (the real wire + close-sync goroutine, the mentor
// dispatch switch, the stall alarm), never just the helpers.

type m442Sink struct {
	mu   sync.Mutex
	msgs []string
}

func (s *m442Sink) send(text string) error {
	s.mu.Lock()
	s.msgs = append(s.msgs, text)
	s.mu.Unlock()
	return nil
}
func (s *m442Sink) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.msgs...)
}

func m442InstallPush(t *testing.T) (*m442Sink, *ownerpush.Pusher) {
	t.Helper()
	s := &m442Sink{}
	p := ownerpush.NewE(s.send)
	t.Cleanup(ownerpush.Use(p))
	return s, p
}

func m442Count(msgs []string, sub string) int {
	n := 0
	for _, m := range msgs {
		if strings.Contains(m, sub) {
			n++
		}
	}
	return n
}

// m442WaitFor polls cond WITHOUT p.Flush(): Flush is WaitGroup.Wait, and the
// production goroutine under test (close-sync -> NoteRefusal -> Push) does
// pending.Add(1) from zero concurrently - a WaitGroup Add-from-zero racing a
// Wait, which -race flags (review D1, 3 of 8 runs). The condition reads the
// sink, which the worker fills; callers Flush ONCE, after it holds.
func m442WaitFor(t *testing.T, what string, p *ownerpush.Pusher, cond func() bool) {
	t.Helper()
	_ = p
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// ── #19 + #6: a rejected exit over the REAL wire ─────────────────────────────

// Three position_close_rejected frames from NT8 (the next cycle keeps retrying
// the close): ONE P0 alert row for the episode (not three), ONE owner push for
// it, and ONE refusal-storm push once the third arrives.
// MUTANT 1 (shown RED): drop the OnCloseRejected call in close_sync.go -> no
// alert row, no push -> RED. MUTANT 2: drop ownerpush.NoteRefusal there -> no
// storm push -> RED. MUTANT 3: use the frame's RejectTime in the event id ->
// three rows -> RED.
func TestM442RejectedExitRaisesOneP0AndOneStormPushOverTheRealWire(t *testing.T) {
	sink, p := m442InstallPush(t)
	r := crRigFor(t, time.Now(), nil)
	registerPostExitDispatch(r.b)
	t.Cleanup(func() { unregisterPostExitDispatch(r.b) })
	r.b.name = "Mess display"

	frame := ntwire.PositionCloseRejectedPayload{
		SignalID: "Close", Symbol: "MNQ", PositionSide: "long", Account: crAcctB,
		Reason: "There is no market data available to drive the simulation engine.",
	}
	for i := 0; i < 3; i++ {
		frame.RejectTime = time.Now().Add(time.Duration(i) * time.Second).UTC().Format(time.RFC3339)
		if err := ntwire.WriteFrame(r.w.conn, ntwire.FramePositionCloseRejected, frame); err != nil {
			t.Fatalf("reject write: %v", err)
		}
	}
	m442WaitFor(t, "the alert row and the storm push", p, func() bool {
		rows, _ := r.st.Alert().List(r.b.id, 20)
		return len(rows) >= 1 && m442Count(sink.all(), "REFUSAL STORM") >= 1
	})
	time.Sleep(50 * time.Millisecond) // a late duplicate would show now
	p.Flush()

	rows, err := r.st.Alert().List(r.b.id, 20)
	if err != nil {
		t.Fatal(err)
	}
	var closeRows []*store.AlertDB
	for _, a := range rows {
		if a.Kind == "position_close_rejected" {
			closeRows = append(closeRows, a)
		}
	}
	if len(closeRows) != 1 {
		t.Fatalf("three rejections in one episode must be ONE alert row, got %d: %+v", len(closeRows), rows)
	}
	if closeRows[0].Level != "P0" || !strings.Contains(closeRows[0].Body, "STILL OPEN") || !strings.Contains(closeRows[0].Body, "no market data") {
		t.Fatalf("alert must be a P0 saying the position is still open and why: %+v", closeRows[0])
	}
	msgs := sink.all()
	if n := m442Count(msgs, "REFUSED to close"); n != 1 {
		t.Fatalf("want exactly one owner push for the rejected exit, got %d: %q", n, msgs)
	}
	if n := m442Count(msgs, "REFUSAL STORM"); n != 1 {
		t.Fatalf("want exactly one storm push for 3 rejections, got %d: %q", n, msgs)
	}
	for _, m := range msgs {
		if strings.Contains(m, "REFUSAL STORM") {
			for _, want := range []string{"exit REJECTED by NT8", "3 times", "Mess display", "MNQ long"} {
				if !strings.Contains(m, want) {
					t.Errorf("storm text %q lacks %q", m, want)
				}
			}
		}
	}
	_ = telemetry.NT8CloseRejectedCount()
}

// A rejection arriving more than the episode gap later is a NEW episode and a
// new alert (the owner is told again if it happens again).
func TestM442CloseRejectEpisodeGap(t *testing.T) {
	base := time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)
	a := closeRejectEpisodeStart("m442-ep", "MNQ", "long", base)
	b := closeRejectEpisodeStart("m442-ep", "MNQ", "LONG", base.Add(closeRejectEpisodeGap-time.Second))
	c := closeRejectEpisodeStart("m442-ep", "MNQ", "long", base.Add(2*closeRejectEpisodeGap+time.Minute))
	if a != b {
		t.Fatalf("a rejection inside the gap must stay in the episode: %d vs %d", a, b)
	}
	if c == a {
		t.Fatal("a rejection after the gap must start a new episode")
	}
	if d := closeRejectEpisodeStart("m442-ep-other", "MNQ", "long", base); d != base.UnixMilli() {
		t.Fatalf("episodes are per trader: got %d", d)
	}
}

func TestM442PositionCloseRejectedIsAnOwnerPushedOpsKind(t *testing.T) {
	if !ownerPushKind("P0", alertKindCloseRejected) {
		t.Fatal("a P0 position_close_rejected must reach the owner's phone")
	}
	if !isOpsAlertKind(alertKindCloseRejected) {
		t.Fatal("position_close_rejected must be an ops kind (no day-plan gate)")
	}
}

// ── #6: swing BE / close refused, through the dispatch switch ────────────────

// MUTANT (shown RED): drop the noteRefusalStorm line in mentorMoveStopBE's
// unknown-arm refusal -> no storm push -> RED.
func TestM442SwingBERefusedThreeTimesPushesOneStorm(t *testing.T) {
	sink, p := m442InstallPush(t)
	at, _ := stallAlarmTrader(t, "m442-swing")
	at.name = "hoang"
	registerPostExitDispatch(at) // the running-trader registry the storm text's display-name lookup reads
	t.Cleanup(func() { unregisterPostExitDispatch(at) })
	resetMentorCounters(at)
	for i := 0; i < 4; i++ {
		at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionMoveStopBE, ArmID: "swing-ghost-1", Reason: "BE after leg 2"}, mentorTierInputs{}, 0, 0)
	}
	p.Flush()
	msgs := sink.all()
	if len(msgs) != 1 || !strings.Contains(msgs[0], "swing break-even refused") || !strings.Contains(msgs[0], "hoang") || !strings.Contains(msgs[0], "swing-ghost-1") {
		t.Fatalf("want exactly ONE storm push naming trader + arm + class, got %q", msgs)
	}
	// a different arm is a different scope
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionClosePosition, ArmID: "swing-ghost-2", Reason: "hold close"}, mentorTierInputs{}, 0, 0)
	p.Flush()
	if len(sink.all()) != 1 {
		t.Fatalf("one refusal of another class/arm must not push: %q", sink.all())
	}
}

func TestM442SwingCloseRefusedThreeTimesPushesOneStorm(t *testing.T) {
	sink, p := m442InstallPush(t)
	at, _ := stallAlarmTrader(t, "m442-swing2")
	resetMentorCounters(at)
	for i := 0; i < 3; i++ {
		at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionClosePosition, ArmID: "swing-ghost-3", Reason: "hold close"}, mentorTierInputs{}, 0, 0)
	}
	p.Flush()
	msgs := sink.all()
	if len(msgs) != 1 || !strings.Contains(msgs[0], "swing hold-close refused") {
		t.Fatalf("want ONE swing hold-close storm push, got %q", msgs)
	}
}

// ── #6: STOP MOVE NOT APPLIED, through the verify loop ───────────────────────

// The not-applied P0 used to reach the alert center only. It is now also an
// owner-pushed ops kind (first miss of an episode), and repeated not-applied
// moves on one TRADER (the manage-by-hand latch stops one leg after two misses,
// so the count is per trader) raise the storm push.
// MUTANT 1 (shown RED): remove "stop-move-not-applied" from ownerPushKind ->
// no push on the first miss -> RED. MUTANT 2: drop the noteRefusalStorm line
// before mentorBumpStopMoveEpisode in mentor_exit_drive.go -> no storm -> RED.
func TestM442StopMoveNotAppliedReachesTheOwnerOverTheVerifyLoop(t *testing.T) {
	sink, p := m442InstallPush(t)
	b := r22Fixture(t, store.RiskControlConfig{})
	b.at.name = "hoang"
	b.positions("short", 1)
	b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
	b.book(r22RunnerBook(r22Stop)...)
	b.receiptLeg1(r22Sig, "short", r22L1TP, r22Minute(0).Add(30*time.Second))
	b.candle(r22Minute(0), 30730.00, 30731.00, 30722.00, 30725.00) // BE sent
	if got := b.waitMoves(1); len(got) != 1 {
		t.Fatalf("fixture: BE first; got %+v", got)
	}
	b.book(r22RunnerBook(r22Stop)...) // NT8 did NOT apply it
	b.candle(r22Minute(1), 30725.00, 30745.00, 30724.00, 30744.00)
	b.book(r22RunnerBook(r22Stop)...) // still not applied after the one re-send
	b.candle(r22Minute(2), 30744.00, 30746.00, 30740.00, 30741.00)
	p.Flush()
	if snap := b.at.mentorCountSnapshot(); snap["stop_move_not_applied"] != 1 || snap["stop_move_not_applied_twice"] != 1 {
		t.Fatalf("fixture: no not-applied episode ran: %v", snap)
	}
	msgs := sink.all()
	if n := m442Count(msgs, "Stop move not applied at NT8"); n != 1 {
		t.Fatalf("the not-applied P0 must reach the owner exactly once, got %d: %q", n, msgs)
	}
	// The verify loop noted TWO misses (first + the one after the re-send, then
	// the leg goes to manual management). One more on this trader - another
	// position's leg - is the third inside the window: the storm push.
	b.at.noteRefusalStorm(ownerpush.ClassStopNotApplied, "stop moves", "other position: wanted 1.00, broker at 2.00")
	p.Flush()
	if n := m442Count(sink.all(), "STOP MOVE NOT APPLIED at NT8"); n != 1 {
		t.Fatalf("want one storm push, got %d: %q", n, sink.all())
	}
}

// ── #9: the stall alarm and the owner's own rules ────────────────────────────

// The 60-minute-window trader (08:30-09:30 CT) emits ISB intents outside its
// window all evening; every one is refused BY THE WINDOW, which is the window
// working. Before m442 the third raised "Mentor setup stalled: isb" (P1) - the
// false alarm of 10-28 18:17:59 (log_events 342827, and 14 more like it).
// Driven through mentorDispatchIntent -> mentorPlaceIntent -> the real window
// gate, with a FRESH feed so the stale-data block is not the refuser.
// MUTANT (shown RED): make mentorStallPolicyRefusal return false -> the window
// refusals count as a stall -> the P1 fires -> RED.
func TestM442WindowRefusalsAreNotAStall(t *testing.T) {
	telemetry.ResetSetupStallsForTest()
	t.Setenv("MENTOR_PLACE", "1")
	at, st := stallAlarmTrader(t, "m442-win")
	// DayPlan enabled: emitAlert silently drops the non-ops kind
	// "mentor-setup-stalled" without it, so the final no-alert assertion could
	// never fail (review D2).
	at.config.StrategyConfig = &store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}, RiskControl: store.RiskControlConfig{MentorMode: true}} // unset window = 08:30 + 60 min
	now := mentorStaleBlockClock(10, 0)                                                                                                                        // 10:00 CT: outside 08:30-09:30
	setMentorNowSource(func() time.Time { return now })
	t.Cleanup(func() { setMentorNowSource(nil) })
	wireMentorPlacementSeams(t, at)
	mentorStaleBlockFeed(t, now, 20*time.Second) // fresh

	var placed []mentor.Intent
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed = append(placed, i) }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	for i := 0; i < 5; i++ {
		in := mentorStaleBlockIntent()
		in.ArmID = "isb-" + string(rune('a'+i))
		at.mentorDispatchIntent(in, mentorExtraFor(in, false), 0, 0)
	}
	if len(placed) != 0 {
		t.Fatalf("fixture: outside the window nothing may be placed, got %d", len(placed))
	}
	_, table := telemetry.SetupStallSnapshot()
	c := table["m442-win"]["isb"]
	if c.Emitted != 5 || c.Policy != 5 || c.Placed != 0 || !strings.HasPrefix(c.LastRefusal, "window:") {
		t.Fatalf("fixture: want 5 emitted, 5 refused by the window, 0 placed; got %+v", c)
	}
	at.mentorStallAlarmCheck(now)
	rows, _ := st.Alert().List("m442-win", 10)
	if len(rows) != 0 {
		t.Fatalf("refusals by the owner's own trading window are not a stall; got alert %+v", rows[0])
	}
}

// A REAL stall still alerts, and says how many were the owner's own rules. 3
// stale_data refusals (the 10-11 / 10-14 kind, log_events 318256 et al.) plus 4
// window refusals: only the 3 count, the body says so.
func TestM442RealStallStillAlertsAndSaysHowManyWereYourRules(t *testing.T) {
	telemetry.ResetSetupStallsForTest()
	at, st := stallAlarmTrader(t, "m442-real")
	in := stallLevelIntent(1)
	for i := 0; i < 7; i++ {
		at.mentorCountSetupEmitted(in)
	}
	for i := 0; i < 4; i++ {
		at.mentorRecordStallRefusal(in, "window: outside the trading window 08:30–09:30 CT — no new entries")
	}
	for i := 0; i < 3; i++ {
		at.mentorRecordStallRefusal(in, "stale_data")
	}
	at.mentorStallAlarmCheck(mentorStallOpenTime())
	rows, _ := st.Alert().List("m442-real", 10)
	if len(rows) != 1 {
		t.Fatalf("3 real (stale_data) + 4 policy refusals must still raise ONE stall alert, got %d", len(rows))
	}
	if b := rows[0].Body; !strings.Contains(b, "7 emitted") || !strings.Contains(b, "4 of them refused by your own") || !strings.Contains(b, "last refusal: stale_data") {
		t.Fatalf("body must be honest about the policy refusals: %q", b)
	}
}

// The classification is pinned against the REAL last-refusal strings from the
// 27 "Mentor setup stalled" rows in log_events (read 10-29): 15 were the owner's
// own rules (11 window ids 311042..342827, 4 done_after_win ids 287106, 316278,
// 316811, 339381) and 12 were real stalls (stale_data x9, stale_intent x3).
func TestM442StallPolicyClassificationAgainstRealLogRows(t *testing.T) {
	policy := map[string]string{
		"log_events 342827 (window)":         "window: outside the trading window 08:30–09:30 CT — no new entries [D1.2 p1 @23:49–24:21, @24:36–24:59]",
		"log_events 339381 (done_after_win)": "done_after_win: done for the day after a win — a trade closed in profit and the day is net positive; no new entries until the next NY open (08:30 CT) [rule D1.2 p1 @20:53–21:16 + p2 @05:28–06:27; boundary = owner ruling 2026-10-09 (transcript silent)]",
	}
	for name, reason := range policy {
		if !mentorStallPolicyRefusal(reason) {
			t.Errorf("%s: the owner's own rule must not count as a stall: %q", name, reason)
		}
	}
	real := map[string]string{
		"log_events 318256 (stale_data)":   "stale_data",
		"log_events 310794 (stale_intent)": "stale_intent",
		"sources":                          "sources_missing: ISB",
	}
	for name, reason := range real {
		if mentorStallPolicyRefusal(reason) {
			t.Errorf("%s: a real stall cause was classified as policy: %q", name, reason)
		}
	}
}
