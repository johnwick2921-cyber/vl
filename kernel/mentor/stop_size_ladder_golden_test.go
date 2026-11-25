package mentor

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
)

// Stop-size ladder (owner order 2026-11-10) on the four real tapes the m434 /
// m473 goldens replay. Every pre-ladder golden runs the ladder OFF
// (m434StopLadder = false) and stays byte-identical — that is the proof the
// ladder-OFF bot is the pre-ladder bot. THIS golden pins the ladder-ON
// behaviour, recorded once at the ladder head; TestStopLadderRealTapeEntries
// says what moved and checks it is only what the ladder allows.
//
// Since owner order 2026-11-15 (no stop-size cap by default) THIS golden runs
// the ladder with a STORED mentor_wide_stop_max_pts 40 (m434WideStopMaxPts) —
// the revert value, byte-identical to the #70 golden; the no-cap default is
// pinned by TestStopLadderNoCapTickGolden (testdata/stop_ladder_nocap_tick.golden).
//
// Regenerate (only for a deliberate behaviour change):
//
//	STOP_LADDER_WRITE_GOLDEN=1 go test -count=1 -run 'TestStopLadderTickGolden|TestStopLadderNoCapTickGolden' ./kernel/mentor
const (
	stopLadderGoldenPath      = "testdata/stop_ladder_tick.golden"
	stopLadderNoCapGoldenPath = "testdata/stop_ladder_nocap_tick.golden"
)

func withStopLadder(t *testing.T, on bool) {
	t.Helper()
	prev := m434StopLadder
	m434StopLadder = on
	t.Cleanup(func() { m434StopLadder = prev })
}

func withWideStopMax(t *testing.T, v float64) {
	t.Helper()
	prev := m434WideStopMaxPts
	m434WideStopMaxPts = v
	t.Cleanup(func() { m434WideStopMaxPts = prev })
}

// TestStopLadderTickGolden — the ladder ON with the stored REVERT cap 40.
func TestStopLadderTickGolden(t *testing.T) {
	withStopLadder(t, true)
	withWideStopMax(t, 40)
	stopLadderGoldenCheck(t, stopLadderGoldenPath)
}

// TestStopLadderNoCapTickGolden — the ladder ON with the production default
// mentor_wide_stop_max_pts 0 = NO stop-size cap (owner order 2026-11-15 "tarde
// all isb or any thing just need adjust contact"). Recorded once with
// STOP_LADDER_WRITE_GOLDEN=1 at the no-cap head.
func TestStopLadderNoCapTickGolden(t *testing.T) {
	withStopLadder(t, true)
	withWideStopMax(t, DefaultCourseExitsCfg().WideStopMaxPts)
	if m434WideStopMaxPts != 0 {
		t.Fatalf("fixture: the no-cap golden must run the production default 0, got %g", m434WideStopMaxPts)
	}
	stopLadderGoldenCheck(t, stopLadderNoCapGoldenPath)
}

func stopLadderGoldenCheck(t *testing.T, path string) {
	t.Helper()
	if os.Getenv("STOP_LADDER_WRITE_GOLDEN") != "" {
		var all []string
		for _, c := range m434Cases() {
			all = append(all, m434Digest(t, c)...)
		}
		if err := os.WriteFile(path, []byte(strings.Join(all, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d tick digests to %s", len(all), path)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name := strings.SplitN(sc.Text(), " ", 2)[0]
		want[name] = append(want[name], sc.Text())
	}
	for _, c := range m434Cases() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, w := m434Digest(t, c), want[c.name]
			if len(w) == 0 || len(got) != len(w) {
				t.Fatalf("%s: %d ticks, golden has %d", c.name, len(got), len(w))
			}
			for i := range got {
				if got[i] != w[i] {
					t.Fatalf("%s: first divergence at row %d\n got  %s\n want %s", c.name, i, got[i], w[i])
				}
			}
		})
	}
}

// TestStopLadderRealTapeEntries replays the four real tapes through the
// PRODUCTION evaluator (e.Tick, the cases' own configs) with the ladder ON and
// OFF and checks the ladder's contract on every emitted non-swing entry:
//   - OFF: no ISB/reverse-ISB-as-ISB stop above the 20-pt ISB ceiling and no
//     PHL/PLH/BOX stop above 25 (the pre-ladder ceilings) — sanity;
//   - ON (default, mentor_wide_stop_max_pts 0 = no cap since owner order
//     2026-11-15): every target at least 1:1 (the ladder never enlarges a
//     target [OctD2.2-R49 @40:47]);
//   - ON with a STORED max (stopLadderTapeBindingMax): no non-swing stop
//     above it [OctD2.3-R47 @48:12] — and the no-cap run must carry at least
//     one stop above that max, so the assertion provably BINDS on these tapes
//     (reviewer P2 on 7e790a741: the old `WideStopMaxPts > 0 &&` guard read
//     the cases' 0 and could never fire). The tapes' stops top out under 40,
//     so the stored-40 revert itself is pinned by stop_ladder_tick.golden.
//
// It logs every ladder-band entry (stop above the pre-ladder ceiling) so the
// report can name them.
func TestStopLadderRealTapeEntries(t *testing.T) {
	type entry struct {
		at          int
		setup, side string
		risk, rew   float64
	}
	run := func(c m434Case, on bool, wideMax float64) []entry {
		old := flatNearScale
		flatNearScale = c.scale
		defer func() { flatNearScale = old }()
		bars := c.tape(t)
		from := c.from
		if from < 0 {
			for i := range bars {
				if bars[i].OpenTime >= r22Open1013 {
					from = i
					break
				}
			}
		}
		cfg := c.cfg()
		cfg.CourseExits.StopSizeLadder = on
		cfg.CourseExits.WideStopMaxPts = wideMax
		e := New(cfg)
		var out []entry
		for i := from; i < len(bars); i++ {
			lo := i + 1 - 1500
			if lo < 0 {
				lo = 0
			}
			ins := e.Tick(bars[lo:i+1], BarCloseInstant(bars[i]))
			for _, in := range ins {
				switch in.Action {
				case ActionMoveStopBE:
					e.SwingBEApplied(in.ArmID)
				case ActionClosePosition:
					e.SwingEnded(in.ArmID)
				}
				if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup != "SWING4H" {
					out = append(out, entry{i, in.Setup, string(in.Side), math.Abs(in.Price - in.Stop), math.Abs(in.Target - in.Price)})
				}
			}
		}
		return out
	}
	const eps = 1e-6
	band, bindAbove := 0, 0
	for _, c := range m434Cases() {
		cfg := c.cfg()
		if cfg.CourseExits.WideStopMaxPts != 0 {
			t.Fatalf("%s: case cfg WideStopMaxPts %g, want the production default 0 (no cap)", c.name, cfg.CourseExits.WideStopMaxPts)
		}
		off, on := run(c, false, 0), run(c, true, 0)
		capped := run(c, true, stopLadderTapeBindingMax)
		for _, x := range capped {
			if x.risk > stopLadderTapeBindingMax+eps {
				t.Fatalf("%s stored-max row %d: %s stop %.2f above the stored mentor_wide_stop_max_pts %g", c.name, x.at, x.setup, x.risk, stopLadderTapeBindingMax)
			}
		}
		for _, x := range off {
			ceil := cfg.StopCeilingPts
			if x.setup == "ISB" {
				ceil = cfg.CourseExits.ISBMaxStopPts
			}
			if x.risk > ceil+eps {
				t.Fatalf("%s OFF row %d: %s stop %.2f above the pre-ladder ceiling %g", c.name, x.at, x.setup, x.risk, ceil)
			}
		}
		for _, x := range on {
			if x.risk > stopLadderTapeBindingMax+eps {
				bindAbove++
			}
			if x.rew > 0 && x.rew+eps < x.risk {
				t.Fatalf("%s ON row %d: %s target %.2f under the stop %.2f — the ladder must never shrink the R:R", c.name, x.at, x.setup, x.rew, x.risk)
			}
			ceil := cfg.StopCeilingPts
			if x.setup == "ISB" {
				ceil = cfg.CourseExits.ISBMaxStopPts
			}
			if x.risk > ceil+eps {
				band++
				t.Logf("%s row %d: ladder-band entry %s %s stop %.2f target %.2f", c.name, x.at, x.setup, x.side, x.risk, x.rew)
			}
		}
		t.Logf("%s: entries OFF %d, ON %d", c.name, len(off), len(on))
	}
	t.Logf("ladder-band entries across the four tapes: %d", band)
	if bindAbove == 0 {
		t.Fatalf("no no-cap entry above the stored max %g on any tape — the stored-max assertion would be vacuous; lower stopLadderTapeBindingMax", stopLadderTapeBindingMax)
	}
	t.Logf("no-cap entries above the stored max %g (refused when it is stored): %d", stopLadderTapeBindingMax, bindAbove)
}

// stopLadderTapeBindingMax is a STORED mentor_wide_stop_max_pts chosen to sit
// above both pre-ladder ceilings (ISB 20, PHL/PLH/BOX 25) and under the four
// tapes' widest ladder-band stop (27.25, -v run 2026-11-15), so the stored-max
// branch of StopLadderCeiling is exercised on real bars, not only on fixtures.
const stopLadderTapeBindingMax = 26.0

// stopLadderEntry is one non-swing entry the production evaluator emitted.
type stopLadderEntry struct {
	at          int
	setup, side string
	risk        float64
}

// The four real tapes only ever carry 4.5–17.5-pt entry stops (probe
// 2026-11-10), so none reaches the course's 20 / 30 / 40 bands. The test
// therefore scales the three band edges by the SAME factor 0.4 (20→8, 30→12,
// 40→16) and replays the REAL, unscaled tapes: a real stop of s pts stands for
// s/0.4 course pts (11.5→28.75, 13→32.5, 17.5→43.75). The trader side of the
// same mapping is TestStopLadderRealTapeStopSizes (trader package).
const (
	stopLadderScaleOff  = 8.0  // 20 · 0.4 — the pre-ladder ceiling
	stopLadderScaleMid  = 12.0 // 30 · 0.4 — top of the reduced (3) band
	stopLadderScaleWide = 16.0 // 40 · 0.4 — mentor_wide_stop_max_pts
)

// stopLadderTapeRun replays one case's real tape through e.Tick with the
// scaled ceilings. unbounded lifts every ceiling to 1000 (what the evaluator
// WOULD enter with no refusal at all).
func stopLadderTapeRun(t *testing.T, c m434Case, ladder, unbounded bool) []stopLadderEntry {
	t.Helper()
	old := flatNearScale
	flatNearScale = c.scale
	defer func() { flatNearScale = old }()
	bars := c.tape(t)
	from := c.from
	if from < 0 {
		for i := range bars {
			if bars[i].OpenTime >= r22Open1013 {
				from = i
				break
			}
		}
	}
	cfg := c.cfg()
	cfg.CourseExits.StopSizeLadder = ladder
	cfg.StopCeilingPts, cfg.CourseExits.ISBMaxStopPts, cfg.CourseExits.WideStopMaxPts = stopLadderScaleOff, stopLadderScaleOff, stopLadderScaleWide
	if unbounded {
		cfg.StopCeilingPts, cfg.CourseExits.ISBMaxStopPts, cfg.CourseExits.WideStopMaxPts = 1000, 1000, 1000
	}
	e := New(cfg)
	var out []stopLadderEntry
	for i := from; i < len(bars); i++ {
		lo := i + 1 - 1500
		if lo < 0 {
			lo = 0
		}
		for _, in := range e.Tick(bars[lo:i+1], BarCloseInstant(bars[i])) {
			switch in.Action {
			case ActionMoveStopBE:
				e.SwingBEApplied(in.ArmID)
			case ActionClosePosition:
				e.SwingEnded(in.ArmID)
			}
			if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup != "SWING4H" {
				out = append(out, stopLadderEntry{i, in.Setup, string(in.Side), math.Abs(in.Price - in.Stop)})
			}
		}
	}
	return out
}

// stopLadderBands counts entries per scaled band: (off,mid], (mid,wide], >wide.
func stopLadderBands(es []stopLadderEntry) (mid, wide, over int) {
	for _, x := range es {
		switch {
		case x.risk > stopLadderScaleWide+1e-6:
			over++
		case x.risk > stopLadderScaleMid+1e-6:
			wide++
		case x.risk > stopLadderScaleOff+1e-6:
			mid++
		}
	}
	return
}

// stopLadderWidenedProblems is the contract. got is the run under test
// (ladder ON in production, forced OFF in the negative test); unbounded is the
// same tapes with no ceiling. Returns every violation.
func stopLadderWidenedProblems(got, unbounded []stopLadderEntry) []string {
	var p []string
	gm, gw, gover := stopLadderBands(got)
	um, uw, uo := stopLadderBands(unbounded)
	if um == 0 || uw == 0 || uo == 0 {
		p = append(p, fmt.Sprintf("the real tapes do not reach every band unbounded (%d/%d/%d)", um, uw, uo))
	}
	if gm == 0 {
		p = append(p, "no admitted entry in the (20,30] course band (size-3 band)")
	}
	if gw == 0 {
		p = append(p, "no admitted entry in the (30,40] course band (wide, 1 MNQ band)")
	}
	if gover != 0 {
		p = append(p, "an entry above the wide max was admitted — it must be refused")
	}
	return p
}

// TestStopLadderRealTapeWidenedStops: the ladder-ON half of the test above
// could not fail (no real entry exceeds 17.5 pts). Here the real tapes run
// with the band edges scaled (see the constants) so real entries land in the
// (20,30], (30,40] and above-40 bands: the PRODUCTION evaluator (e.Tick) must
// admit the first two and refuse the last with the ladder ON, and with it OFF
// admit none above the pre-ladder ceiling [OctD2.2-R73 @01:12:32, OctD2.3-R47
// @48:12].
func TestStopLadderRealTapeWidenedStops(t *testing.T) {
	var on, off, unbounded []stopLadderEntry
	for _, c := range m434Cases() {
		on = append(on, stopLadderTapeRun(t, c, true, false)...)
		off = append(off, stopLadderTapeRun(t, c, false, false)...)
		unbounded = append(unbounded, stopLadderTapeRun(t, c, true, true)...)
	}
	if p := stopLadderWidenedProblems(on, unbounded); len(p) != 0 {
		t.Fatalf("ladder ON: %v", p)
	}
	if m, w, o := stopLadderBands(off); m+w+o != 0 {
		t.Fatalf("ladder OFF admitted a stop above the pre-ladder ceiling (%d/%d/%d)", m, w, o)
	}
	m, w, o := stopLadderBands(on)
	um, uw, uo := stopLadderBands(unbounded)
	t.Logf("real tapes, ladder ON admitted mid=%d wide=%d over=%d; unbounded mid=%d wide=%d over=%d", m, w, o, um, uw, uo)
	for _, x := range unbounded {
		if x.risk > stopLadderScaleOff+1e-6 {
			t.Logf("  row %d %s %s stop %.2f (course-equivalent %.2f)", x.at, x.setup, x.side, x.risk, x.risk/0.4)
		}
	}
}

// TestStopLadderRealTapeWidenedStopsFailsOff proves the test above can fail:
// with the ladder forced OFF the same contract reports violations.
func TestStopLadderRealTapeWidenedStopsFailsOff(t *testing.T) {
	var forcedOff, unbounded []stopLadderEntry
	for _, c := range m434Cases() {
		forcedOff = append(forcedOff, stopLadderTapeRun(t, c, false, false)...)
		unbounded = append(unbounded, stopLadderTapeRun(t, c, true, true)...)
	}
	p := stopLadderWidenedProblems(forcedOff, unbounded)
	if len(p) == 0 {
		t.Fatal("the widened-stop contract passes with the ladder forced OFF — the test cannot fail")
	}
	t.Logf("ladder forced OFF reports: %v", p)
}
