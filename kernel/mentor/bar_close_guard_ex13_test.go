package mentor

import (
	"fmt"
	"strings"
	"testing"

	"vl/market"
)

// ── EX-13 (11-16 full re-read): an entry is never authored on a bar that was not
// truly closed ─────────────────────────────────────────────────────────────────
//
// October reads a candle only after it closes: "Em quan tâm việc đóng nến"
// (Oct D3.2 @34:20-34:33); "Ngồi đợi cây nến này đóng xong" (Oct D4.2
// @30:48-30:56). On 11-16 the boot's bars_historical batch carried NT8's
// IN-PROGRESS 19:12 bar stamped final=true, emitted at ~19:12:45 — fifteen
// seconds before its 19:13:00 close (VLBarsSubscriptionManager.cs EmitHistorical
// stamps every replay bar final), and the AddOn's watermark then suppressed that
// bar's real close, so the cache kept the 19:12:45 snapshot (v=5140, 615
// contracts short of its 3m/5m parents) as the "final" 19:12 candle. At 19:14 the
// long ISB armed on the (19:12, 19:13) pair: its MOTHER was that snapshot.
//
// The frame's own emission clock proves it: Final=true with EmittedAt before the
// bar's close instant is a forming snapshot labelled final. Every pin below drives
// the PRODUCTION entry, e.Tick, on the ISB-M2 tape (green mother M, red inside I1
// → one long ISB at 106 on tick 3).

// ex13Snapshot marks b Final with an emission stamp `beforeClose` ms before its
// close instant (the 11-16 batch: 15 s before).
func ex13Snapshot(b market.Kline, beforeClose int64) market.Kline {
	b.Final = true
	b.EmittedAt = BarCloseInstant(b) - beforeClose
	return b
}

// ex13Closed marks b Final with an emission stamp after its close (the AddOn's
// boundary finalisation, emitted on the first tick of the next bar).
func ex13Closed(b market.Kline) market.Kline {
	b.Final = true
	b.EmittedAt = BarCloseInstant(b) + 250
	return b
}

// ex13Tick3 runs the ISB-M2 tape through tick 3 (M = bars[1], I1 = bars[2]) after
// edit marks the bars, and returns the ISB placements of tick 3 and the
// evaluator.
func ex13Tick3(t *testing.T, cfg func(*Config), edit func(bars []market.Kline)) ([]Intent, *Evaluator) {
	t.Helper()
	e := isbM2Eval(true, TriggerLine{})
	if cfg != nil {
		cfg(&e.Cfg)
	}
	bars := isbM2Tape()
	for i := range bars {
		bars[i] = ex13Closed(bars[i])
	}
	if edit != nil {
		edit(bars)
	}
	var placed []Intent
	for i := 2; i <= 3; i++ {
		for _, in := range e.Tick(bars[:i], BarCloseInstant(bars[i-1])) {
			if i == 3 && (in.Action == PlaceStopLimitEntry || in.Action == PlaceStopEntry) {
				placed = append(placed, in)
			}
		}
	}
	return placed, e
}

// TestEX13SnapshotMotherRefusesTheISB — the 11-16 19:14 shape: the MOTHER is a
// final-labelled snapshot emitted 15 s before its close. The ISB on (M, I1) must
// not arm; it is refused by name.
func TestEX13SnapshotMotherRefusesTheISB(t *testing.T) {
	placed, e := ex13Tick3(t, nil, func(bars []market.Kline) {
		bars[1] = ex13Snapshot(bars[1], 15_000)
	})
	if len(placed) != 0 {
		t.Fatalf("an entry was authored on a mother bar that was not truly closed (Final=true, emitted 15 s before its close): %+v", placed)
	}
	if e.State.Refusals["bar_not_truly_closed"] == 0 {
		t.Fatalf("the ISB on a snapshot mother must be refused by name bar_not_truly_closed; refusals %v", e.State.Refusals)
	}
	if e.State.ISBArms != nil {
		for id, a := range e.State.ISBArms {
			if a.Inside >= 0 {
				t.Fatalf("a refused ISB left arm %s registered (%+v)", id, a)
			}
		}
	}
}

// TestEX13SnapshotInsideBarRefusesTheISB — the bar being ticked (I1) is itself a
// final-labelled snapshot (the 19:12:46 boot tick's shape).
func TestEX13SnapshotInsideBarRefusesTheISB(t *testing.T) {
	placed, e := ex13Tick3(t, nil, func(bars []market.Kline) {
		bars[2] = ex13Snapshot(bars[2], 15_000)
	})
	if len(placed) != 0 {
		t.Fatalf("an entry was authored on a current bar that was not truly closed: %+v", placed)
	}
	if e.State.Refusals["bar_not_truly_closed"] == 0 {
		t.Fatalf("refusal bar_not_truly_closed missing; refusals %v", e.State.Refusals)
	}
	found := false
	for _, n := range e.TakeNotes() {
		if strings.Contains(n, "not truly closed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the refusal must be noted (logged) once, naming the bar and its emission stamp")
	}
}

// TestEX13TrulyClosedBarsStillArm — controls: the same tape with every bar
// emitted after its close (the boundary finalisation), and with no emission
// stamp at all (store / backfill bars, an AddOn older than emitted_at), arms the
// long ISB at 106 exactly as before.
func TestEX13TrulyClosedBarsStillArm(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func([]market.Kline)
	}{
		{"emitted after close", nil},
		{"no emission stamp", func(bars []market.Kline) {
			for i := range bars {
				bars[i].EmittedAt = 0
			}
		}},
		{"emitted exactly at close", func(bars []market.Kline) {
			bars[1].EmittedAt = BarCloseInstant(bars[1])
		}},
	} {
		placed, e := ex13Tick3(t, nil, c.edit)
		if len(placed) != 1 || placed[0].Side != SideLong || placed[0].Price != 106 {
			t.Errorf("%s: want the one long ISB at 106; got %+v (refusals %v)", c.name, placed, e.State.Refusals)
		}
		if e.State.Refusals["bar_not_truly_closed"] != 0 {
			t.Errorf("%s: refused bar_not_truly_closed on truly closed bars", c.name)
		}
	}
}

// TestEX13KnobOffReverts — mentor_bar_close_guard=false: Final alone, the long
// ISB at 106 arms on the snapshot mother exactly as before the guard.
func TestEX13KnobOffReverts(t *testing.T) {
	placed, e := ex13Tick3(t, func(c *Config) { c.BarCloseGuard = false }, func(bars []market.Kline) {
		bars[1] = ex13Snapshot(bars[1], 15_000)
	})
	if len(placed) != 1 || placed[0].Side != SideLong || placed[0].Price != 106 {
		t.Fatalf("knob false must revert: want the long ISB at 106 on the snapshot mother; got %+v", placed)
	}
	if e.State.Refusals["bar_not_truly_closed"] != 0 {
		t.Fatalf("knob false still refused bar_not_truly_closed")
	}
}

// TestEX13SnapshotOutsideTheEntryCandlesDoesNotRefuse — a snapshot older than the
// newest two 5m candles is history (state only): the run is identical to the same
// tape with that bar truly closed, and nothing is refused bar_not_truly_closed.
func TestEX13SnapshotOutsideTheEntryCandlesDoesNotRefuse(t *testing.T) {
	run := func(snap bool) (intents []string, refusals map[string]int) {
		e := isbM2Eval(true, TriggerLine{})
		// 12 filler bars (09:00-09:11), the oldest optionally a snapshot; the
		// ISB-M2 tape then runs 09:12-09:15, so the 09:00 bar is outside the
		// 09:05-09:15 5m pair of every tape tick.
		var bars []market.Kline
		for i := 0; i < 12; i++ {
			b := ex13Closed(rthBars(i, 121, 121.5, 120.5, 120))
			if i == 0 && snap {
				b = ex13Snapshot(b, 15_000)
			}
			bars = append(bars, b)
		}
		for i, b := range isbM2Tape() {
			bars = append(bars, ex13Closed(rthBars(12+i, b.Open, b.High, b.Low, b.Close)))
		}
		for i := 2; i <= len(bars); i++ {
			for _, in := range e.Tick(bars[:i], BarCloseInstant(bars[i-1])) {
				intents = append(intents, fmt.Sprintf("tick%d %s %s %s %.2f", i, in.Action, in.Setup, in.Side, in.Price))
			}
		}
		return intents, e.State.Refusals
	}
	clean, cleanRef := run(false)
	got, gotRef := run(true)
	if fmt.Sprint(clean) != fmt.Sprint(got) || fmt.Sprint(cleanRef) != fmt.Sprint(gotRef) {
		t.Fatalf("a snapshot 12+ minutes back changed the run:\n clean %v %v\n snap  %v %v", clean, cleanRef, got, gotRef)
	}
	if gotRef["bar_not_truly_closed"] != 0 {
		t.Fatalf("refused bar_not_truly_closed on a snapshot outside the entry candles")
	}
}

// TestEX13SnapshotInEntryCandlesWindow pins which bars the guard reads: the 1m
// pair always, and the 1m bars of the newest two 5m candles.
func TestEX13SnapshotInEntryCandlesWindow(t *testing.T) {
	mk := func(n int) []market.Kline {
		var bars []market.Kline
		for i := 0; i < n; i++ {
			bars = append(bars, ex13Closed(rthBars(i, 1, 2, 0.5, 1.5)))
		}
		return bars
	}
	// 20 bars 09:00..09:19; cur = 09:19 (close 09:20) → window from 09:10.
	for _, c := range []struct {
		snapAt int
		want   bool
	}{{19, true}, {18, true}, {10, true}, {9, false}, {0, false}} {
		bars := mk(20)
		bars[c.snapAt] = ex13Snapshot(bars[c.snapAt], 1)
		if _, ok := snapshotInEntryCandles(bars); ok != c.want {
			t.Errorf("snapshot at 09:%02d with cur 09:19: got %v, want %v", c.snapAt, ok, c.want)
		}
	}
}
