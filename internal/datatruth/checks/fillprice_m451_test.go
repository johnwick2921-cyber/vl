package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
)

// release #55 m451 - fill_price guard pins (the guard itself; the production
// fill handlers are pinned in trader/ninjatrader, the gate in trader/).

func farObs(id, event, acct string, at int64) FillObs {
	return FillObs{ID: id, Event: event, Account: acct, Symbol: "MNQ", Kind: "exit", Side: "LONG", Price: 29100, AtMs: at, Tick: 0.25}
}

func ringAround(m int64) RingFn {
	return ringOf(bar(m, 29000, 29005, true), bar(m+minuteMs, 29000, 29005, true))
}

func TestM451OneStrikePerFillEventAcrossFrames(t *testing.T) {
	g, alerts := newTestGuard(nil)
	day := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	m := day.UnixMilli() / minuteMs * minuteMs
	g.Record(farObs("f1", "ev1", "Sim101", m+5_000), ringAround(m), day)
	g.Record(farObs("f2", "ev1", "Sim101", m+5_003), ringAround(m), day) // next leg, same event
	g.Record(farObs("f3", "ev1", "Sim101", m+5_006), ringAround(m), day)
	if s := g.Stats(); s.Far != 1 || s.FarRepeat != 2 || len(*alerts) != 1 {
		t.Fatalf("one event, one strike, one alert: %+v %v", s, *alerts)
	}
	if b, _, _ := g.Check()("MNQ", day); b {
		t.Fatal("one event must not pause")
	}
	g.Record(farObs("f4", "ev2", "Sim101", m+9_000), ringAround(m), day)
	if b, _, _ := g.Check()("MNQ", day); !b {
		t.Fatal("a second event pauses")
	}
}

func TestM451StrikesAreScopedToTheAccountAndAckAccountLiftsOnlyThat(t *testing.T) {
	g, _ := newTestGuard(nil)
	day := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	m := day.UnixMilli() / minuteMs * minuteMs
	g.Record(farObs("a1", "a1", "Sim101", m+5_000), ringAround(m), day)
	g.Record(farObs("b1", "b1", "SimAccount1", m+5_000), ringAround(m), day)
	if b, _, _ := g.Check()("MNQ", day); b {
		t.Fatal("one strike on each of two accounts is not a pause for either")
	}
	g.Record(farObs("a2", "a2", "Sim101", m+6_000), ringAround(m), day)
	g.Record(farObs("b2", "b2", "SimAccount1", m+6_000), ringAround(m), day)
	b, why, scopes, _ := g.CheckScoped()("MNQ", day)
	if !b || len(scopes) != 2 || !strings.Contains(why, "account SIM101") || !strings.Contains(why, "account SIMACCOUNT1") {
		t.Fatalf("both accounts now pause, each named: %v %v %q", b, scopes, why)
	}
	if !g.AckAccount("MNQ", "sim101") {
		t.Fatal("ack of one account")
	}
	b, _, scopes, _ = g.CheckScoped()("MNQ", day)
	if !b || len(scopes) != 1 || scopes[0] != datatruth.ScopeAccount("SimAccount1") {
		t.Fatalf("only SimAccount1 still paused: %v %v", b, scopes)
	}
	// a fill that names no account strikes everyone
	g.Record(farObs("n1", "n1", "", m+7_000), ringAround(m), day)
	g.Record(farObs("n2", "n2", "", m+8_000), ringAround(m), day)
	if _, _, scopes, _ = g.CheckScoped()("MNQ", day); len(scopes) != 0 {
		t.Fatalf("an unattributed pause is for everyone (no scope): %v", scopes)
	}
}

// A fill whose own record cannot be judged is counted as a far fill and logged.
func TestM451MalformedFillsAreCountedAsFar(t *testing.T) {
	g, alerts := newTestGuard(nil)
	day := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	m := day.UnixMilli() / minuteMs * minuteMs
	g.Record(FillObs{ID: "z1", Account: "Sim101", Symbol: "MNQ", Kind: "entry", Price: 0, AtMs: m + 5_000, Tick: 0.25}, ringAround(m), day)
	g.Record(FillObs{ID: "z2", Account: "Sim101", Symbol: "MNQ", Kind: "entry", Price: 29001, AtMs: 0, Tick: 0.25}, ringAround(m), day)
	if s := g.Stats(); s.Malformed != 2 || s.Far != 2 || len(*alerts) != 2 {
		t.Fatalf("price<=0 and time<=0 are far strikes: %+v %v", s, *alerts)
	}
	if b, why, _ := g.Check()("MNQ", day); !b || !strings.Contains(why, "no usable fill time") {
		t.Fatalf("cannot judge = blocked: %v %q", b, why)
	}
	g.Record(FillObs{Account: "Sim101", Symbol: "MNQ", Kind: "entry", Price: 29001, AtMs: m + 5_000, Tick: 0.25}, ringAround(m), day)
	if s := g.Stats(); s.Malformed != 3 {
		t.Fatalf("an id-less fill is malformed too: %+v", s)
	}
}

// ── persistence ──────────────────────────────────────────────────────────────

func TestM451StrikesAndAckSurviveAKill9Restart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "fill.json")
	sessionOf := func(ts time.Time) string { return ts.UTC().Format("2006-01-02") }
	day := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	m := day.UnixMilli() / minuteMs * minuteMs

	g1, _ := newTestGuard(sessionOf)
	if n, err := g1.EnableState(path); n != 0 || err != nil {
		t.Fatalf("no file yet: %d %v", n, err)
	}
	g1.Record(farObs("p1", "p1", "Sim101", m+5_000), ringAround(m), day)
	g1.Record(farObs("p2", "p2", "Sim101", m+6_000), ringAround(m), day)
	if b, _, _ := g1.Check()("MNQ", day); !b {
		t.Fatal("paused")
	}
	// m55r3: the state write is off the exit path; the kill -9 lands after it.
	if !g1.Flush(5 * time.Second) {
		t.Fatal("the writer did not go idle")
	}

	g2, _ := newTestGuard(sessionOf) // the restarted process
	n, err := g2.EnableState(path)
	if err != nil || n != 1 {
		t.Fatalf("one standing state restored: %d %v", n, err)
	}
	b, why, scopes, _ := g2.CheckScoped()("MNQ", day)
	if !b || len(scopes) != 1 || !strings.Contains(why, "p1,p2") {
		t.Fatalf("the unacknowledged pause survives the restart: %v %v %q", b, scopes, why)
	}
	// a replay of an already-counted fill after the restart is not a third strike
	g2.Record(farObs("p1", "p1", "Sim101", m+5_000), ringAround(m), day)
	if s := g2.Stats(); s.Far != 0 || s.Judged != 0 {
		t.Fatalf("a replayed counted fill is ignored: %+v", s)
	}
	if !g2.Ack("MNQ") { // Ack waits (bounded) for its own write
		t.Fatal("ack")
	}
	g3, _ := newTestGuard(sessionOf)
	if _, err := g3.EnableState(path); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := g3.Check()("MNQ", day); b {
		t.Fatal("the acknowledgement survives the restart")
	}
	// the next session spends the restored strikes
	g4, _ := newTestGuard(sessionOf)
	if _, err := g4.EnableState(path); err != nil {
		t.Fatal(err)
	}
	g4.Record(farObs("q1", "q1", "Sim101", m+9_000), ringAround(m), day)
	if b, _, _ := g4.Check()("MNQ", day.Add(24*time.Hour)); b {
		t.Fatal("the next session starts clean")
	}
	g4.Flush(5 * time.Second)
}

func TestM451UnreadableStateFileIsToldToTheOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fill.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	g, alerts := newTestGuard(nil)
	n, err := g.EnableState(path)
	if err == nil || n != 0 {
		t.Fatalf("a corrupt file is reported: %d %v", n, err)
	}
	if len(*alerts) != 1 || !strings.Contains((*alerts)[0], "fill_price_state") {
		t.Fatalf("the owner is told the standing strikes may be lost: %v", *alerts)
	}
}

// ── the 96 h state age cutoff reads the GUARD's clock (time-bomb pin, #57) ───
//
// The cutoff used to be time.Now().Add(-fillStateMaxAge): a fixture stamped on a
// fixed calendar day silently aged out of the state file as real time passed
// (trader.TestM451FillPriceStrikesAndAckSurviveARestart went red on 2026-10-30).
// Now the guard's clock (SetClock; production = the wall clock) decides, so the
// boundary is exact and independent of when the suite runs.

func TestFillStateAgeCutoffUsesTheGuardClock(t *testing.T) {
	sessionOf := func(ts time.Time) string { return ts.UTC().Format("2006-01-02") }
	day := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	m := day.UnixMilli() / minuteMs * minuteMs
	last := m + 6_000 // the strike state's newest fill (LastMs)
	lastT := time.UnixMilli(last)

	path := filepath.Join(t.TempDir(), "data", "fill.json")
	g1, _ := newTestGuard(sessionOf)
	g1.SetClock(func() time.Time { return day })
	if _, err := g1.EnableState(path); err != nil {
		t.Fatal(err)
	}
	g1.Record(farObs("p1", "p1", "Sim101", m+5_000), ringAround(m), day)
	g1.Record(farObs("p2", "p2", "Sim101", last), ringAround(m), day)
	if !g1.Flush(5 * time.Second) {
		t.Fatal("the writer did not go idle")
	}

	restore := func(now time.Time) int {
		g, _ := newTestGuard(sessionOf)
		g.SetClock(func() time.Time { return now })
		n, err := g.EnableState(path)
		if err != nil {
			t.Fatalf("EnableState(%v): %v", now, err)
		}
		return n
	}
	if n := restore(lastT.Add(fillStateMaxAge - time.Second)); n != 1 {
		t.Fatalf("a state 96h-1s old must survive on the guard's clock, restored %d", n)
	}
	if n := restore(lastT.Add(fillStateMaxAge)); n != 1 {
		t.Fatalf("a state exactly 96h old is still inside the window, restored %d", n)
	}
	if n := restore(lastT.Add(fillStateMaxAge + time.Second)); n != 0 {
		t.Fatalf("a state 96h+1s old must be dropped on the guard's clock, restored %d", n)
	}

	// The write side uses the same clock: once the guard's clock is past the
	// window, the next snapshot no longer carries the stale strike.
	g1.SetClock(func() time.Time { return lastT.Add(fillStateMaxAge + time.Second) })
	g1.AckAccount("MNQ", "Sim101") // any state change persists a snapshot
	if !g1.Flush(5 * time.Second) {
		t.Fatal("the writer did not go idle")
	}
	g2, _ := newTestGuard(sessionOf)
	g2.SetClock(func() time.Time { return lastT }) // a clock that would keep it, were it in the file
	if n, err := g2.EnableState(path); err != nil || n != 0 {
		t.Fatalf("the snapshot written at 96h+1s must not carry the stale strike: n=%d err=%v", n, err)
	}
}
