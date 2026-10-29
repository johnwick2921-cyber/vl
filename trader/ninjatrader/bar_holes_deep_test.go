package ninjatrader

import (
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── m444 review round 2 — a hole older than the standard replay is asked for
// DEEPLY, not given up on ─────────────────────────────────────────────────────
//
// Round 1 retired every hole past the subscription's bars_back=2000 and told the
// owner "the bot cannot refill it: a reload or a restart will NOT bring it back".
// False: RequestDeepBarsBackfill (the arbiter's) reaches 8640 1m bars and the
// 10-26 hole sat ~2800 open minutes back. These pins run the PRODUCTION path:
// fixture (started TCPServer + fake AddOn + SQLite + the real tracker/persister)
// -> tracker.tick -> TCPServer.RequestDeepGapBackfillAt -> the frame at the AddOn.

// openDeepHole is the Fri 10-09 → Wed 10-14 shape: ~4500 open minutes back,
// past the 2000-bar replay and inside the 8640 deep reach.
func (f *ghFixture) openDeepHole() time.Time {
	f.t.Helper()
	f.stream(ct(2026, 10, 9, 10, 0), ct(2026, 10, 9, 10, 5))
	f.stream(ct(2026, 10, 14, 7, 48), ct(2026, 10, 14, 7, 49))
	waitFor(f.t, "the tracker to open the Fri→Wed hole", func() bool { return len(f.holes()) == 1 })
	return f.clk.Now()
}

// MUTANT (shown RED): in placeBeyondReplayLocked return the retire branch
// unconditionally (drop the deepHoleWire block) -> no frame is sent, the hole
// is retired -> RED on waitReRequests.
func TestHoleBeyondTheStandardReplayIsAskedForDeeplyAndFills(t *testing.T) {
	f := newGHFixture(t)
	det := f.openDeepHole()
	f.tr.tick(det.Add(61 * time.Second))
	f.waitReRequests(1)
	sub := f.addon.lastSub()
	want := openMinutes(ct(2026, 10, 9, 10, 6), det.Add(61*time.Second).Truncate(time.Minute)) + barHoleDeepMargin
	if len(sub.Timeframes) != 1 || sub.Timeframes[0] != "1m" || sub.BarsBack != want || want <= 2000 || want > ntwire.DeepGapBackfillMaxBarsBack {
		t.Fatalf("deep frame = %+v, want [1m] with bars_back %d (open minutes since the hole + margin; >2000, <=8640)", sub, want)
	}
	hs := f.holes()
	if len(hs) != 1 || hs[0].State != holeStateRefilling || hs[0].Attempts != 1 {
		t.Fatalf("a deep hole keeps working, not retired: %+v\n%s", hs, f.sink.dump())
	}
	if f.sink.count("bars hole needs a DEEP re-request: MNQ 1m 10-09 10:06–10-14 07:48 CT") != 1 ||
		f.sink.count("DEEP bars_subscribe for 1m only, bars_back") != 1 || f.sink.count("RETIRED") != 0 {
		t.Fatalf("DEEP said once, never RETIRED:\n%s", f.sink.dump())
	}
	// NT8 serves the span: the store holds it, the next measure closes the hole
	bars := make([]ntwire.Bar, 0, 5000)
	for m := ct(2026, 10, 9, 10, 6); !m.After(ct(2026, 10, 14, 7, 49)); m = m.Add(time.Minute) { // a replay ends at now: it overlaps the live minutes, which is what lets the ring judge its scale
		if barHoleExpected(m) {
			bars = append(bars, wireBar(m, 30700))
		}
	}
	f.addon.send(ntwire.FrameBarsHistorical, ntwire.BarsHistoricalPayload{Symbol: "MNQ", Timeframe: "1m", Contract: ghContract, Bars: bars})
	time.Sleep(100 * time.Millisecond)
	f.addon.live(ct(2026, 10, 14, 7, 50), 30700)
	waitFor(t, "the deep replay to land in the store", func() bool { return f.storedOpen(ct(2026, 10, 9, 10, 6), ct(2026, 10, 9, 10, 7)) == 1 })
	f.tr.tick(det.Add(61*time.Second + 3*time.Minute))
	if hs := f.holes(); len(hs) != 0 {
		t.Fatalf("the deep replay filled the span: the hole must close, got %+v\n%s", hs, f.sink.dump())
	}
	if d := f.delta(); d.Filled != 1 || d.Unfilled != 0 {
		t.Fatalf("filled +%d unfilled +%d, want 1/0", d.Filled, d.Unfilled)
	}
}

// A deep ask is ~4x a standard replay, so its budget is smaller: 2 scheduled, then
// UNFILLED, then 2 slow — 4 in total, never the 4 + 8 of a standard hole.
// MUTANT (shown RED): schedLen/slowCap ignore h.deep -> 4 scheduled + 8 slow
// (12 frames) -> RED.
func TestDeepHoleHasASmallerBudgetAndUnfilledNamesTheRightLever(t *testing.T) {
	f := newGHFixture(t)
	f.addon.setAnswer(func(int, ntwire.BarsSubscribePayload) { f.answerEmpty() })
	det := f.openDeepHole()
	f.tr.tick(det.Add(61 * time.Second))
	f.waitReRequests(1)
	f.tr.tick(det.Add(3*time.Minute + 5*time.Second))
	f.waitReRequests(2)
	unfilledAt := det.Add(5*time.Minute + 10*time.Second)
	f.tr.tick(unfilledAt) // the measure after #2 -> UNFILLED
	if f.sink.count("UNFILLED after 2 re-requests") != 1 {
		t.Fatalf("a deep hole is declared unfilled after its 2 scheduled asks:\n%s", f.sink.dump())
	}
	for k := 1; k <= 4; k++ {
		f.tr.tick(unfilledAt.Add(time.Duration(k) * 15 * time.Minute))
	}
	f.noNewReRequest(2+2, "a deep hole gets 2 scheduled + 2 slow asks in total")
	if n := f.sink.count("bars hole slow retries spent: MNQ 1m 10-09 10:06–10-14 07:48 CT"); n != 1 || f.sink.count("POST /api/nt/bar-arbiter") < 1 {
		t.Fatalf("the spent line is said once and names the lever a restart cannot replace:\n%s", f.sink.dump())
	}
	if strings.Contains(f.sink.dump(), "plus a bot restart (which re-subscribes) is the fix") {
		t.Fatalf("a deep hole must not be told a restart fixes it:\n%s", f.sink.dump())
	}
	got := f.tr.unfilledFeeding("MNQ", det) // the owner read (live frames just arrived in this fixture)
	if len(got) != 1 || !got[0].Deep || !got[0].SlowSpent || got[0].OutsideReplay || got[0].ReaskHorizon != 30*time.Minute {
		t.Fatalf("the owner read must carry Deep + SlowSpent with a 30 min horizon (2 slow asks x 15 min): %+v", got)
	}
}

// A hole that slides past the deepest ask while it is being worked is retired
// (listed), not asked for with a bars_back the wire would clamp short of it.
// MUTANT (shown RED): delete the 4b block in collectDue -> a 5th, clamped ask
// goes out for a hole it cannot reach -> RED.
func TestDeepHoleThatSlidesBeyondTheDeepestAskIsRetired(t *testing.T) {
	f := newGHFixture(t)
	det := f.openDeepHole()
	// a week later the hole is ~9000+ open minutes back; it was marked deep
	f.tr.mu.Lock()
	f.tr.holes[0].deep, f.tr.holes[0].deepBarsBack = true, 8000
	f.tr.mu.Unlock()
	f.tr.tick(det.Add(7*24*time.Hour + 61*time.Second))
	f.noNewReRequest(0, "past the deepest reach nothing is sent")
	if hs := f.holes(); len(hs) != 1 || hs[0].State != holeStateRetired {
		t.Fatalf("slid beyond the deepest ask: retired and listed: %+v\n%s", hs, f.sink.dump())
	}
}

// A wire with no deep path leaves the old behaviour (retired) — the tracker never
// invents a deep ask the wire cannot make.
func TestPlainWireWithoutADeepPathRetiresABeyondReplayHole(t *testing.T) {
	f := newGHFixture(t)
	det := f.openDeepHole()
	f.tr.mu.Lock()
	real := f.tr.wire
	f.tr.wire = func() holeWire { return plainHoleWire{real()} }
	f.tr.mu.Unlock()
	f.tr.tick(det.Add(61 * time.Second))
	f.noNewReRequest(0, "no deep wire -> no ask")
	if hs := f.holes(); len(hs) != 1 || hs[0].State != holeStateRetired {
		t.Fatalf("retired and listed: %+v", hs)
	}
}

// plainHoleWire hides the deep method (embedding the interface exposes only its
// methods, not the concrete type's).
type plainHoleWire struct{ holeWire }

// REVIEW 1 DEFECT 2: with the audit OFF nothing closes or ages out a retired
// hole, so it is dropped as before m444 (not kept for the life of the process,
// taking a tracker slot and a banner line).
// MUTANT (shown RED): retireLocked without the auditDays<=0 branch -> the
// retired hole stays listed 200 simulated hours later -> RED.
func TestRetiredHoleIsDroppedWhenTheAuditIsOff(t *testing.T) {
	f := newGHFixture(t)
	f.tr.auditDays = 0
	f.stream(ct(2026, 9, 25, 10, 0), ct(2026, 9, 25, 10, 5))
	f.stream(ct(2026, 10, 14, 7, 48), ct(2026, 10, 14, 7, 49))
	waitFor(t, "the hole", func() bool { return len(f.holes()) == 1 })
	det := f.clk.Now()
	f.tr.tick(det.Add(61 * time.Second))
	if hs := f.holes(); len(hs) != 0 {
		t.Fatalf("audit off: a retired hole must be dropped, got %+v\n%s", hs, f.sink.dump())
	}
	f.tr.tick(det.Add(200 * time.Hour))
	if hs := f.holes(); len(hs) != 0 {
		t.Fatalf("still listed after 200h: %+v", hs)
	}
}

// REVIEW 1 DEFECT 1: a retired hole leaves the list ONLY by aging out of the
// audit window — the one branch with no pin.
// MUTANT (shown RED): `case false && h.ToMs <= winFrom:` in applyAuditLocked ->
// the hole is listed forever, no "aged out" line -> RED.
func TestRetiredHoleAgesOutOfTheAuditWindow(t *testing.T) {
	f := newGHFixture(t)
	f.tr.auditDays = 14
	f.putOpen("1m", ct(2026, 9, 29, 8, 0), ct(2026, 9, 29, 9, 0), store.BarSourceHistorical)
	f.putOpen("1m", ct(2026, 10, 13, 12, 0), ct(2026, 10, 14, 7, 46), store.BarSourceHistorical)
	now := f.clk.Now()
	f.tr.auditPass(now)
	f.tr.tick(now.Add(20 * time.Second))
	if hs := f.holes(); len(hs) != 1 || hs[0].State != holeStateRetired {
		t.Fatalf("precondition: one retired hole: %+v\n%s", hs, f.sink.dump())
	}
	later := now.AddDate(0, 0, 25) // the hole ended 10-13: far outside 14 CME sessions
	f.tr.auditPass(later)
	if hs := f.holes(); len(hs) != 0 {
		t.Fatalf("a hole wholly before the audit window must leave the list: %+v\n%s", hs, f.sink.dump())
	}
	if n := f.sink.count("bars hole aged out of the audit window: MNQ 1m 09-29 09:00–10-13 12:00 CT"); n != 1 {
		t.Fatalf("one aged-out line, said %d:\n%s", n, f.sink.dump())
	}
	f.tr.auditPass(later.Add(10 * time.Minute))
	if n := f.sink.count("aged out of the audit window"); n != 1 {
		t.Fatalf("said once, said %d", n)
	}
}

// REVIEW 1 NOTED: retired holes are listed, not worked on — they must not take
// the slots new holes need.
// MUTANT (shown RED): atCapLocked as `len(t.holes) >= barHoleMaxOpen` -> the new
// hole is not tracked -> RED.
func TestRetiredHolesDoNotEatTheWorkingSlots(t *testing.T) {
	f := newGHFixture(t)
	f.tr.mu.Lock()
	for i := 0; i < barHoleMaxOpen+8; i++ {
		f.tr.holes = append(f.tr.holes, &barHole{Symbol: "MNQ", Contract: ghContract, State: holeStateRetired,
			FromMs: ct(2026, 9, 1, 10, 0).UnixMilli() + int64(i)*600_000, ToMs: ct(2026, 9, 1, 10, 5).UnixMilli() + int64(i)*600_000})
	}
	f.tr.mu.Unlock()
	f.putOpen("1m", ct(2026, 10, 13, 8, 0), ct(2026, 10, 13, 10, 0), store.BarSourceHistorical)
	f.putOpen("1m", ct(2026, 10, 13, 12, 0), ct(2026, 10, 14, 7, 46), store.BarSourceHistorical)
	f.tr.auditPass(f.clk.Now())
	if f.sink.count("found by the AUDIT: MNQ 1m 10-13 10:00–12:00 CT") != 1 {
		t.Fatalf("a new hole must be tracked while only RETIRED ones fill the list:\n%s", f.sink.dump())
	}
}

// REVIEW 1 DEFECT 3: the trailing-edge evidence must be CALENDAR-OPEN minutes. A
// 5m bar opening 16:58 covers 16:58-17:02; the two break minutes are not
// evidence, so the hole starts at the 17:00 open (not 16:58).
// MUTANT (shown RED): drop `&& isOpen(time.UnixMilli(m))` from the edge loop ->
// the span starts at 16:58 -> RED.
func TestAuditEvidenceInTheDailyBreakIsNotEvidence(t *testing.T) {
	oneMin := []int64{ct(2026, 10, 13, 15, 50).UnixMilli()}
	evid := map[int64]struct{}{}
	evidenceMinutes(5*time.Minute, []int64{ct(2026, 10, 13, 16, 58).UnixMilli()}, evid)
	got := auditSpans(oneMin, evid, ct(2026, 10, 13, 15, 0).UnixMilli(), ct(2026, 10, 13, 17, 30).UnixMilli(), 3, barHoleExpected)
	if len(got) != 1 || got[0].FromMs != ct(2026, 10, 13, 17, 0).UnixMilli() || got[0].OpenMissing != 3 {
		t.Fatalf("want one span [17:00, 17:03) with 3 open minutes, got %+v", got)
	}
}
