package main

import (
	"fmt"
	"testing"
	"time"

	"vl/calendar"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

// ── EX-13 (11-16 full re-read): the 19:12 forming bar on the LIVE event path ──
//
// The 11-16 boot (19:12:4x CT) received NT8's bars_historical batch. Its newest
// bar, 19:12, was still forming (emitted ~19:12:45, closes 19:13:00) but the
// AddOn stamps every replay bar final=true (VLBarsSubscriptionManager.cs
// EmitHistorical), and its watermark then suppressed the bar's real close, so the
// cache kept the 19:12:45 snapshot (O 27,330.50 H 27,376.25 L 27,330.50 C
// 27,368.00 v=5140, 615 contracts short of its 3m/5m parents — the fixture row
// IS that frozen row). Live log: the 19:12:46 event pass ticked it
// ("nt8_last_bar=19:12 … ref=forming.EmittedAt", "the 11-16 19:12 CT 1m body
// (open 27330.50, close 27368.00)", funnel "bars 1"); at 19:14:00 the history
// refill re-seeded ("replay released 1999 MNQ 1m row(s) to the store"), and at
// 19:14:01 the long ISB armed 27,364.25 on the (19:12, 19:13) pair — its MOTHER
// the snapshot. October reads a candle only after it closes: "Em quan tâm việc
// đóng nến" (Oct D3.2 @34:20-34:33); "Ngồi đợi cây nến này đóng xong" (Oct D4.2
// @30:48-30:56).
//
// These pins drive the LIVE event-loop entry (trader.MentorReplay.EventPass =
// mentorEventPassAt: the bar cache through market.FuturesBarsProvider, the
// forming-tail drop, mentorEvalOnce) with the cache exactly as NT8 delivered it.

var ex13Loc = kernel.CTLocation()

func ex13CT(h, m, s, ms int) time.Time {
	return time.Date(2026, 11, 16, h, m, s, ms*int(time.Millisecond), ex13Loc)
}

// ex13Bar is the fixture row at openCT as a cache bar.
func ex13Bar(t *testing.T, bs *barSet, open time.Time) market.Kline {
	t.Helper()
	for _, r := range bs.byTF["1m"][fixtureContract] {
		if r.T == open.UnixMilli() {
			return klines([]row{r}, 60_000)[0]
		}
	}
	t.Fatalf("no fixture bar at %s", open)
	return market.Kline{}
}

// ex13Forming is the first emission of the bar opening at open: one trade at its
// open price, final=false.
func ex13Forming(t *testing.T, bs *barSet, open time.Time, emitted time.Time) market.Kline {
	b := ex13Bar(t, bs, open)
	b.High, b.Low, b.Close, b.Volume = b.Open, b.Open, b.Open, 1
	b.Final = false
	b.EmittedAt = emitted.UnixMilli()
	return b
}

type ex13Pass struct {
	ran      bool
	intents  []mentor.Intent
	refusals map[string]int
}

func (p ex13Pass) armed(side, setup string, px float64) bool {
	for _, in := range p.intents {
		if (in.Action == mentor.PlaceStopEntry || in.Action == mentor.PlaceStopLimitEntry) &&
			string(in.Side) == side && in.Setup == setup && in.Price == px {
			return true
		}
	}
	return false
}

func (p ex13Pass) String() string {
	var ins []string
	for _, in := range p.intents {
		ins = append(ins, fmt.Sprintf("%s %s %s %.2f", in.Action, in.Setup, in.Side, in.Price))
	}
	return fmt.Sprintf("ran=%v intents=%v refusals=%v", p.ran, ins, p.refusals)
}

type ex13Run struct {
	at1912, at1913, at1914 ex13Pass
}

// ex13Replay seeds the 11-16 session (17:00 CT), ticks every bar closed by
// 19:12:00 through the tool's stepping, then runs the three live event passes
// over the cache NT8 delivered: 19:12:46 (the boot batch ending in the 19:12
// snapshot), 19:13:01 (the 19:13 bar forming; the 19:12 close never re-sent),
// and 19:14:00.4 after the 19:14:00 refill re-seed (19:13 finalised on the
// boundary, 19:14 forming).
func ex13Replay(t *testing.T, sc *store.StrategyConfig) ex13Run {
	t.Helper()
	return ex13ReplayOpt(t, sc, false)
}

// ex13ReplayOpt is ex13Replay; live1912=true is the control in which the 19:12
// bar reached Go as a LIVE boundary final instead (Historical=false), stamped
// 19:12:59.969 — 31 ms before its close instant, the early-stamp the live journal
// shows on ~40% of real finals (11-16: 32 of 90; the 19:13 bar itself arrived at
// 19:13:59.996).
func ex13ReplayOpt(t *testing.T, sc *store.StrategyConfig, live1912 bool) ex13Run {
	t.Helper()
	bs := jbFixture1116(t)
	seedAt, err := seedInstant("session", "2026-11-17")
	if err != nil {
		t.Fatal(err)
	}
	testCap.take()
	rp := newReplayer(bs, sc, map[string][]calendar.Event{}, testCap, func(Event) {})
	rp.seed(seedAt)
	t1912 := ex13CT(19, 12, 0, 0)
	for _, r := range bs.byTF["1m"][fixtureContract] {
		if r.T+60_000 <= seedAt.UnixMilli() || r.T+60_000 > t1912.UnixMilli() {
			continue
		}
		if rp.step(klines([]row{r}, 60_000)[0], seedAt.UnixMilli()) {
			t.Fatalf("panic on the %s bar", ct(r.T))
		}
	}

	// The boot batch: every bar up to and including the 19:12 snapshot, final,
	// emitted 19:12:45 (the AddOn stamps NowUtcMs() on each).
	batchAt := ex13CT(19, 12, 45, 0).UnixMilli()
	var batch []market.Kline
	for _, r := range bs.byTF["1m"][fixtureContract] {
		if r.T > t1912.UnixMilli() {
			break
		}
		b := klines([]row{r}, 60_000)[0]
		b.EmittedAt = batchAt
		b.Historical = true // a bars_historical replay batch (cache Source=historical)
		batch = append(batch, b)
	}
	if live1912 {
		batch[len(batch)-1].EmittedAt = ex13CT(19, 12, 59, 969).UnixMilli()
		batch[len(batch)-1].Historical = false
	}
	var cache []market.Kline
	srcWindow := rp.src.window
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline {
		if symbol != "MNQ" {
			return nil
		}
		if tf != "1m" {
			if _, ok := tfMs[tf]; !ok {
				return nil
			}
			return srcWindow(tf, n)
		}
		out := cache
		if n > 0 && len(out) > n {
			out = out[len(out)-n:]
		}
		return append([]market.Kline(nil), out...)
	}
	pass := func(now time.Time, asOf int64) ex13Pass {
		rp.r.SetNow(now)
		rp.src.AsOf = asOf
		before := map[string]int{}
		for k, v := range rp.r.Evaluator().State.Refusals {
			before[k] = v
		}
		ins, ran := rp.r.EventPass(now)
		diff := map[string]int{}
		for k, v := range rp.r.Evaluator().State.Refusals {
			if d := v - before[k]; d > 0 {
				diff[k] = d
			}
		}
		return ex13Pass{ran: ran, intents: ins, refusals: diff}
	}

	var run ex13Run
	cache = batch
	run.at1912 = pass(ex13CT(19, 12, 46, 0), t1912.UnixMilli())

	cache = append(append([]market.Kline(nil), batch...), ex13Forming(t, bs, ex13CT(19, 13, 0, 0), ex13CT(19, 13, 0, 900)))
	run.at1913 = pass(ex13CT(19, 13, 1, 0), t1912.UnixMilli())

	t1914 := ex13CT(19, 14, 0, 0).UnixMilli()
	rp.reseed(t1914, "replay released 1999 MNQ 1m row(s) to the store")
	b1913 := ex13Bar(t, bs, ex13CT(19, 13, 0, 0))
	// The real live 19:13 final: close=1794878039999 arrival=1794878039996 (11-16
	// journal, mentor eval latency) — emitted 4 ms BEFORE its close instant.
	b1913.EmittedAt = ex13CT(19, 13, 59, 996).UnixMilli()
	cache = append(append([]market.Kline(nil), batch...), b1913, ex13Forming(t, bs, ex13CT(19, 14, 0, 0), ex13CT(19, 14, 0, 350)))
	run.at1914 = pass(ex13CT(19, 14, 0, 400), t1914)
	return run
}

// TestEX13_1116_1912FormingBarNotTickedAndNoISBOnIt — RED for EX-13. Default
// guard, ISB-M1 scope box (the live rule: so the 19:14 ISB exists to be judged
// by this guard and not pre-empted by ISB-M1's level read):
//   - 19:12:46: the batch's 19:12 tail is final-labelled but emitted 15 s before
//     its close — it is NOT ticked (the 19:11 bar was, so the pass does not run);
//   - 19:13:01: a newer bar exists, so the 19:12 bar is ticked now (never
//     skipped), and nothing is armed on it;
//   - 19:14:00.4, after the refill re-seed: the long ISB 27,364.25 on the
//     (19:12 snapshot, 19:13) pair is NOT armed; it is refused bar_not_truly_closed.
func TestEX13_1116_1912FormingBarNotTickedAndNoISBOnIt(t *testing.T) {
	run := ex13Replay(t, jbScope("box"))
	t.Logf("19:12:46 %s", run.at1912)
	t.Logf("19:13:01 %s", run.at1913)
	t.Logf("19:14:00 %s", run.at1914)
	if run.at1912.ran {
		t.Errorf("19:12:46: the event pass ticked the forming 19:12 bar (final=true but emitted 19:12:45, before its 19:13:00 close) [Oct D3.2 @34:20-34:33]: %s", run.at1912)
	}
	if !run.at1913.ran {
		t.Errorf("19:13:01: the 19:12 bar was never ticked once a newer bar existed: %s", run.at1913)
	}
	if hasEntry(run.at1913.intents) {
		t.Errorf("19:13:01: an entry was authored on the 19:12 snapshot: %s", run.at1913)
	}
	if !run.at1914.ran {
		t.Fatalf("19:14:00: the 19:13 bar was not ticked: %s", run.at1914)
	}
	if run.at1914.armed("long", "ISB", 27364.25) {
		t.Errorf("19:14:00 after the refill re-seed: the long ISB 27,364.25 armed on the (19:12 snapshot, 19:13) pair — its mother was never truly closed: %s", run.at1914)
	}
	if run.at1914.refusals["bar_not_truly_closed"] == 0 {
		t.Errorf("19:14:00: no bar_not_truly_closed refusal: %s", run.at1914)
	}
}

func hasEntry(ins []mentor.Intent) bool {
	for _, in := range ins {
		if in.Action == mentor.PlaceStopEntry || in.Action == mentor.PlaceStopLimitEntry {
			return true
		}
	}
	return false
}

// TestEX13_1116_RevertReproducesTheLivePath — mentor_bar_close_guard=false (and
// ISB-M1 scope box, the live rule): the live 11-16 path comes back exactly — the
// 19:12:46 pass ticks the forming 19:12 bar, 19:13:01 finds it already ticked,
// and the 19:14 long ISB 27,364.25 arms on the (19:12 snapshot, 19:13) pair.
func TestEX13_1116_RevertReproducesTheLivePath(t *testing.T) {
	sc := jbScope("box")
	off := false
	sc.RiskControl.MentorTuning.MentorBarCloseGuard = &off
	run := ex13Replay(t, sc)
	if !run.at1912.ran || run.at1913.ran {
		t.Errorf("revert: want 19:12:46 ticked (the live boot tick) and 19:13:01 already ticked; got 19:12:46 %s / 19:13:01 %s", run.at1912, run.at1913)
	}
	if !run.at1914.armed("long", "ISB", 27364.25) {
		t.Errorf("revert: the live 19:14 long ISB 27,364.25 did not come back: %s", run.at1914)
	}
	if run.at1914.refusals["bar_not_truly_closed"] != 0 {
		t.Errorf("revert: still refused bar_not_truly_closed: %s", run.at1914)
	}
}

// TestEX13_1116_DefaultNoISBAt1914 — the shipped defaults (guard ON, ISB-M1
// scope all): no long ISB at 19:14 either way.
func TestEX13_1116_DefaultNoISBAt1914(t *testing.T) {
	run := ex13Replay(t, nil)
	if run.at1912.ran {
		t.Errorf("default: the forming 19:12 bar was ticked at 19:12:46: %s", run.at1912)
	}
	if run.at1914.armed("long", "ISB", 27364.25) {
		t.Errorf("default: the 19:14 long ISB 27,364.25 armed: %s", run.at1914)
	}
}

// TestEX13_1116_LiveEarlyFinalsDoNotRefuse — review P0 (11-17) on the LIVE event
// path: the same 19:14 pass, but the 19:12 mother reached Go as a LIVE boundary
// final stamped 31 ms before its close and the 19:13 inside bar 4 ms before its
// close (both real live shapes). They are closed bars: with ISB-M1 scope box (the
// live rule) the long ISB 27,364.25 arms, and nothing is refused
// bar_not_truly_closed. Only a bars_historical batch can carry NT8's forming bar
// labelled final.
func TestEX13_1116_LiveEarlyFinalsDoNotRefuse(t *testing.T) {
	run := ex13ReplayOpt(t, jbScope("box"), true)
	t.Logf("19:14:00 %s", run.at1914)
	if !run.at1914.ran {
		t.Fatalf("19:14:00: the 19:13 bar was not ticked: %s", run.at1914)
	}
	if run.at1914.refusals["bar_not_truly_closed"] != 0 {
		t.Errorf("19:14:00: live finals stamped a few ms before their close were refused bar_not_truly_closed: %s", run.at1914)
	}
	if !run.at1914.armed("long", "ISB", 27364.25) {
		t.Errorf("19:14:00: with live (truly closed) 19:12/19:13 bars the scope-box long ISB 27,364.25 must arm: %s", run.at1914)
	}
}
