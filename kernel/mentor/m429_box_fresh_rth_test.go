package mentor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// m429 R4 (owner 2026-10-25, "if there is a new box use it, if not use the old
// box"): a fresh FTGH/FTGL that forms in RTH (after 08:30 CT) REPLACES the
// evening (17:00+) box of THAT role; a role with no fresh RTH box keeps its
// evening box.

func m429Cfg(on bool) Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = false // 1m box geometry
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.CourseExits.BoxFreshRTHPair = on
	return cfg
}

// m429Mirror flips a FTGH-shaped fixture into the FTGL shape (price p → 200-p).
func m429Mirror(in []market.Kline) []market.Kline {
	out := make([]market.Kline, len(in))
	for i, k := range in {
		k.Open, k.High, k.Low, k.Close = 200-k.Open, 200-k.Low, 200-k.High, 200-k.Close
		out[i] = k
	}
	return out
}

// m429Tape is: an evening FTGH (top 102) at 18:00 CT 09-14 and (mirrored) FTGL
// at 18:10, then — at 08:30 CT 09-15 — n RTH bars: ftgh adds an RTH FTGH
// shifted +10 (top 112), ftgl an RTH FTGL mirror shifted -10, flat adds flat
// bars that form no box.
func m429Tape(rthH, rthL, flat bool) []market.Kline { return m429TapeOff(rthH, rthL, flat, 10) }

// m429TapeOff is m429Tape with the RTH floor shifted by floorOff (mirror of an
// FTGH at +floorOff): a NEGATIVE offset makes the RTH floor LESS extreme than
// the evening one, so only a scan over the RTH slice (not the full window)
// finds it.
func m429TapeOff(rthH, rthL, flat bool, floorOff float64) []market.Kline {
	eve := time.Date(2026, time.September, 14, 18, 0, 0, 0, ctime()).UnixMilli()
	eveL := time.Date(2026, time.September, 14, 18, 10, 0, 0, ctime()).UnixMilli()
	rth := time.Date(2026, time.September, 15, 8, 30, 0, 0, ctime()).UnixMilli()
	bars := cx11FTGH(cx11Mk(eve), 0, 0)
	bars = append(bars, m429Mirror(cx11FTGH(cx11Mk(eveL), 0, 0))...)
	i := 0
	if rthH {
		bars = append(bars, cx11FTGH(cx11Mk(rth), i, 10)...)
		i += 7
	}
	if rthL {
		bars = append(bars, m429Mirror(cx11FTGH(cx11Mk(rth), i, floorOff))...)
		i += 7
	}
	if flat {
		mk := cx11Mk(rth)
		for j := 0; j < 8; j++ {
			bars = append(bars, mk(i+j, 150, 150, 150, 150))
		}
	}
	return bars
}

func m429Run(cfg Config, bars []market.Kline) *Evaluator {
	e := New(cfg)
	// one tick per closed bar from the evening pair on, as live (a one-shot tick
	// over the whole tape would re-pick the most extreme swing, not the first)
	for i := 14; i <= len(bars); i++ {
		e.Tick(bars[:i], bars[i-1].OpenTime+59_999)
	}
	return e
}

// TestM429EveningBoxKeptWhenNoRTHBoxForms — no fresh RTH box forms: BOTH
// evening boxes stand (knob ON, the default). Mutant: clear the latch at 08:30
// (the pre-m429 ON behaviour / "|rth" key) → RED.
func TestM429EveningBoxKeptWhenNoRTHBoxForms(t *testing.T) {
	e := m429Run(m429Cfg(true), m429Tape(false, false, true))
	h, okH := e.State.LatchedBoxes["ftgh"]
	l, okL := e.State.LatchedBoxes["ftgl"]
	if !okH || !okL || h.Top != 102 || l.Bottom != 98 {
		t.Fatalf("both evening boxes must stand; ftgh %+v ftgl %+v", h, l)
	}
}

// TestM429RTHFloorReplacesEveningFloorCeilingStays — a fresh RTH FTGL replaces
// the evening floor; no RTH ceiling forms, so the evening ceiling stays.
func TestM429RTHFloorReplacesEveningFloorCeilingStays(t *testing.T) {
	e := m429Run(m429Cfg(true), m429Tape(false, true, true))
	h, l := e.State.LatchedBoxes["ftgh"], e.State.LatchedBoxes["ftgl"]
	if h.Top != 102 {
		t.Fatalf("the evening ceiling must stay (top 102): %+v", h)
	}
	if l.Bottom != 88 {
		t.Fatalf("the RTH floor must replace the evening floor (bottom 88, was 98): %+v", l)
	}
	live := liveBoxes(e.State.LatchedBoxes, e.State.PostEscapeBoxes)
	if len(live) != 2 || live[0].Kind != FTGL || live[1].Kind != FTGH {
		t.Fatalf("T3: exactly one floor + one ceiling live: %+v", live)
	}
	// OFF: the evening floor stands (today's behaviour).
	off := m429Run(m429Cfg(false), m429Tape(false, true, true))
	if off.State.LatchedBoxes["ftgl"].Bottom != 98 {
		t.Fatalf("OFF: the evening floor stands through RTH: %+v", off.State.LatchedBoxes["ftgl"])
	}
}

// TestM429RTHBoxStandsOnceFormed — the first RTH box of a role stands; a later
// RTH box does not replace it again.
func TestM429RTHBoxStandsOnceFormed(t *testing.T) {
	bars := m429Tape(true, false, false)
	rth := time.Date(2026, time.September, 15, 8, 30, 0, 0, ctime()).UnixMilli()
	bars = append(bars, cx11FTGH(cx11Mk(rth), 30, 30)...) // a higher RTH FTGH later
	e := m429Run(m429Cfg(true), bars)
	if got := e.State.LatchedBoxes["ftgh"].Top; got != 112 {
		t.Fatalf("the first RTH ceiling (112) stands, got %.2f", got)
	}
}

// TestM429RestartInsideRTHKeepsSeededLatch — the production restart path
// (SeedFull then Tick): the seeded latch carries the SAME day key the tick
// compares, so the first tick does not clear it, and the seed lands on the
// box the continuous bot holds (evening floor/ceiling replaced exactly as the
// tick replaced them). Mutant: boxLatchDayKey returns the "|rth" suffix again
// → the day keys differ, the first tick clears the latch → RED.
func TestM429RestartInsideRTHKeepsSeededLatch(t *testing.T) {
	bars := m429Tape(false, true, true)
	now := bars[len(bars)-1].OpenTime + 59_999
	cont := m429Run(m429Cfg(true), bars)

	re := New(m429Cfg(true))
	SeedFull(re, bars, keyLevel1HBars(bars), now)
	if re.State.LatchedBoxesDay != boxLatchDayKey(now, re.Cfg) {
		t.Fatalf("seeded latch day %q != the tick's key %q", re.State.LatchedBoxesDay, boxLatchDayKey(now, re.Cfg))
	}
	seeded := map[string]Box{}
	for k, v := range re.State.LatchedBoxes {
		seeded[k] = v
	}
	re.Tick(bars, now)
	for _, role := range []string{"ftgh", "ftgl"} {
		if seeded[role].Key == "" {
			t.Fatalf("the seed latched no %s: %+v", role, seeded)
		}
		if re.State.LatchedBoxes[role].Key != seeded[role].Key {
			t.Errorf("%s: the first tick after the restart redrew the seeded latch: %s -> %s", role, seeded[role].Key, re.State.LatchedBoxes[role].Key)
		}
		if re.State.LatchedBoxes[role].Key != cont.State.LatchedBoxes[role].Key {
			t.Errorf("%s: restart %s != continuous %s", role, re.State.LatchedBoxes[role].Key, cont.State.LatchedBoxes[role].Key)
		}
	}
	if seeded["ftgl"].Bottom != 88 || seeded["ftgh"].Top != 102 {
		t.Errorf("the seed replay must build the RTH slice (floor 88 replaces 98, ceiling 102 stays): %+v", seeded)
	}
}

// m429Snap formats the live box set.
func m429Snap(e *Evaluator, now int64) string {
	var parts []string
	for _, b := range liveBoxes(e.State.LatchedBoxes, e.State.PostEscapeBoxes) {
		parts = append(parts, fmt.Sprintf("%s %.2f–%.2f formed %s", strings.ToUpper(string(b.Kind)), b.Bottom, b.Top, time.UnixMilli(b.FormedAtMs).In(ctime()).Format("01-02 15:04")))
	}
	return strings.Join(parts, " · ")
}

// m429Run1023 ticks the real 10-23 tape (10-22 17:00 CT -> 15:58) and returns
// the live box set at each hh:mm CT in at.
func m429Run1023(t *testing.T, on bool, at [][2]int) []string {
	cfg := m429Cfg(on)
	cfg.Exec2mAfter30m = true // production frame schedule (2m boxes after 09:00)
	cfg.KeyLevelTFMinutes = DefaultConfig().KeyLevelTFMinutes
	cfg.EMAPeriod34, cfg.EMAPeriod9 = DefaultConfig().EMAPeriod34, DefaultConfig().EMAPeriod9
	all := m423Bars(t)
	want := map[int64]int{}
	for i, a := range at {
		want[m423At(t, a[0], a[1])] = i
	}
	out := make([]string, len(at))
	e := New(cfg)
	for i, b := range all {
		now := BarCloseInstant(b)
		w := all[:i+1]
		if len(w) > SeedTickWindow1m {
			w = w[len(w)-SeedTickWindow1m:]
		}
		if len(w) < 3 {
			continue
		}
		e.Tick(w, now)
		for ms, idx := range want {
			if now >= ms && out[idx] == "" {
				out[idx] = m429Snap(e, now)
			}
		}
	}
	return out
}

// TestM429Fixture1023BoxSetBeforeAfter — the REAL 10-23 tape through Tick.
// BEFORE (knob OFF) the evening boxes stand all session; AFTER (default) a box
// that formed in RTH replaces the evening one of its role.
func TestM429Fixture1023BoxSetBeforeAfter(t *testing.T) {
	at := [][2]int{{8, 0}, {9, 30}, {12, 0}, {15, 0}}
	before := m429Run1023(t, false, at)
	after := m429Run1023(t, true, at)
	for i, a := range at {
		t.Logf("%02d:%02d CT  BEFORE: %s  |  AFTER: %s", a[0], a[1], before[i], after[i])
	}
	if before[0] != after[0] {
		t.Errorf("08:00 is before RTH — both must match: %q vs %q", before[0], after[0])
	}
	if before[3] == after[3] && before[1] == after[1] && before[2] == after[2] {
		t.Errorf("the fresh-RTH default changed nothing on 10-23 — the fixture lost its teeth")
	}
}

// TestM429SeedReplaysRTHSlice — fix (b): the seed replay builds from the RTH
// slice (boxBuildBars), not the full window. An RTH floor that is LESS extreme
// than the evening floor is invisible to a full-window scan (it re-picks the
// most extreme swing), so the restart would keep the evening floor while the
// continuous bot replaced it. Mutant: seed calls BoxesBuild(w, ...) → RED.
func TestM429SeedReplaysRTHSlice(t *testing.T) {
	bars := m429TapeOff(false, true, true, -10)
	now := bars[len(bars)-1].OpenTime + 59_999
	cont := m429Run(m429Cfg(true), bars)
	re := New(m429Cfg(true))
	SeedFull(re, bars, keyLevel1HBars(bars), now)
	c, r := cont.State.LatchedBoxes["ftgl"], re.State.LatchedBoxes["ftgl"]
	if c.Bottom != 108 {
		t.Fatalf("control: the continuous bot must hold the RTH floor (bottom 108): %+v", c)
	}
	if r.Key != c.Key {
		t.Fatalf("restart floor %+v != continuous %+v", r, c)
	}
}
