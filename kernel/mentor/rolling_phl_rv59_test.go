package mentor

import (
	"sort"
	"strings"
	"testing"

	"vl/market"
)

// Release #59 review round 2 (course-ladder lens, 2026-11-01).

// TestRV59RealTapeProductionLadderIsNamedNotTaken — course-ladder P2: on the
// mentor's REAL replay day (MNQ 09-11, testdata/m464_mnq_1m_20260911.csv) the
// production bot, with NOTHING hand-set — the tape's own 5m trigger, no 15m
// box marked seen — arms no rung from 10:07 to 10:14, so the D2.2 worked buy
// (29,396.25 / 29,386.00) is not taken. This pins the DECISION (owner order
// 2026-11-01 "mentor-exact", read across the whole course, not one lesson):
//
//   - the 10:00 15m candle (O 29,370.25 H 29,433.00 L 29,360.00 C 29,394.00)
//     closes at 10:15:00 — the very instant the 10:14 rung is placed — as an
//     inside bar of the RED 09:45 15m candle: a SHORT 15m ISB box standing
//     around the rung. Day 4 rules every lower-timeframe trade AGAINST a 15m
//     ISB out until price leaves the box ("15 phút short ISB, 5 phút long ISB
//     … đừng trade. Đợi nó thoát cả luôn" [D4.2-R10 p1 @05:34–05:37, @08:00];
//     "đợi nó thoát ra khỏi cái range của khung 15 phút, anh chị mới trade"
//     [D4.2-R8 p1 @04:01–04:03]; [D4.1-R55 p2 @07:35–08:11]) — the long rung
//     sits inside it, against it, target 29,428.00 still inside (29,433.00);
//   - the tape's own 5m trigger flips SHORT at 29,410 on the pullback ("lúc
//     nào mình cũng phải nhìn khung 5 phút" [X8 part1 @06:25]) — with it as the
//     first obstacle the 10:14 rung has no 2:1 room.
//
// D2.2 is taught on day 2, before either rule; the mentor's own chart shows the
// trade. The later lessons win in the bot, every refusal is NAMED and counted,
// and the lesson-order conflict is an open mentor question (AUDIT-CHECKLIST
// class rv59-L1). If the mentor rules D2.2 over D4.2 here, THIS test is the
// one to flip.
func TestRV59RealTapeProductionLadderIsNamedNotTaken(t *testing.T) {
	all := m464RealTape(t)
	from := m464RealIdx(t, all, 9, 40)
	bars := all[from:]
	oldHigh := Level{Key: "old-high:29433", Kind: KindOldExtreme, Role: OldHigh, Price: 29433.0}
	e := m464Evaluator(SideLong, oldHigh, bars[m464Idx(10, 7)].OpenTime)
	e.Cfg.ISBBoxEscapeMode = ISBBoxEscapeClose // m473 B3: this Sept lesson pin reads the 1m-delete mode explicitly (default is close5m, OctD4.3-R67 [Oct D4.3 @ 65:00-66:13])
	e.State.Trigger = TriggerLine{}            // the tape's OWN 5m trigger — production
	e.Cfg.PHLRoomMultiple = 2                  // D2-G2: rolling_room is the September 2R room (October default 1)
	steps := m464Run(e, bars, m464Idx(10, 7), m464Idx(10, 14))
	for _, s := range steps {
		if len(s.places) != 0 {
			t.Fatalf("production arms no rung on the real 10:07–10:14 tape (15m short ISB box + 5m short trigger), got %s %+v", s.at, s.places)
		}
	}
	if bx := e.State.ISBBox15m; bx == nil || bx.Dir != SideShort || bx.High != 29433 || bx.Low != 29360 {
		t.Fatalf("the 10:00 15m SHORT ISB box (29,433.00 / 29,360.00) must stand at 10:14, got %+v", bx)
	}
	if e.State.Trigger.Dir != SideShort {
		t.Fatalf("the tape's own 5m trigger must be short at 10:14, got %+v", e.State.Trigger)
	}
	var names []string
	for k := range e.State.PHLRefusedVisit {
		if strings.HasPrefix(k, rollingLevelKey+"@") {
			names = append(names, k[strings.Index(k, "|")+1:])
		}
	}
	sort.Strings(names)
	want := map[string]bool{"rolling_phl_15m_box_ban": false, "rolling_room": false, "rolling_phl_stop_ceiling": false}
	for _, n := range names {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for n, seen := range want {
		if !seen {
			t.Fatalf("every skipped rung must be NAMED — %s missing; named rungs: %v", n, names)
		}
	}
	for n := range want {
		if e.State.Refusals[n] == 0 {
			t.Fatalf("%s must be COUNTED in the ledger, refusals=%v", n, e.State.Refusals)
		}
	}
	if !e.State.RollingPHL.Ladder {
		t.Fatalf("refused rungs never end the ladder, state=%+v", e.State.RollingPHL)
	}
}

// TestRV59RollingCancelKeepsTheChaseGuard — course-ladder P3: rollingCancel
// with no resting rung reset the whole state, ReactedAt included. On the tick
// that re-reads the BREAKING candle (a 2m/5m exec frame's odd-minute 1m tick)
// a cancel path — the side going empty, the old extreme gone — wiped the chase
// guard, and the breaking candle (red = a long's pullback colour) restarted the
// ladder on the impulse [D2.2-R8 p1 @03:27–04:16: the breaking candle is the
// entry candle, never a pullback rung].
// Mutant: `*st = RollingPHL{}` in rollingCancel's no-arm branch → the re-read
// starts a ladder on the breaking candle → RED.
func TestRV59RollingCancelKeepsTheChaseGuard(t *testing.T) {
	e, bars, now := rollingFixture()
	if len(rollingPlaceEntries(e.Tick(bars, now))) != 1 {
		t.Fatal("setup: the first roll must arm (entry 119)")
	}
	t0 := bars[0].OpenTime
	next := append(append([]market.Kline{}, bars...), rollingKline(t0, 6, 119.5, 120, 113, 114)) // RED, fills 119
	at := next[6].OpenTime + 59_999
	_ = e.Tick(next, at)
	reacted := e.State.RollingPHL.ReactedAt
	if reacted != next[6].CloseTime {
		t.Fatalf("setup: the breaking candle must set the chase guard, state=%+v", e.State.RollingPHL)
	}
	// a cancel path fires with no rung resting (the side went empty on this tick).
	_ = e.rollingCancel(&e.State.RollingPHL, "no HTF trend direction — the rolling pullback leg has no side")
	if e.State.RollingPHL.ReactedAt != reacted {
		t.Fatalf("rollingCancel must keep the chase guard ReactedAt=%d, state=%+v", reacted, e.State.RollingPHL)
	}
	if p := rollingPlaceEntries(e.Tick(next, at+1)); len(p) != 0 || e.State.RollingPHL.Ladder {
		t.Fatalf("the re-read of the breaking candle restarted the ladder after a cancel: %+v state=%+v", p, e.State.RollingPHL)
	}
}
