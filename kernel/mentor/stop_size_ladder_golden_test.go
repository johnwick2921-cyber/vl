package mentor

import (
	"bufio"
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
// Regenerate (only for a deliberate behaviour change):
//
//	STOP_LADDER_WRITE_GOLDEN=1 go test -count=1 -run TestStopLadderTickGolden ./kernel/mentor
const stopLadderGoldenPath = "testdata/stop_ladder_tick.golden"

func withStopLadder(t *testing.T, on bool) {
	t.Helper()
	prev := m434StopLadder
	m434StopLadder = on
	t.Cleanup(func() { m434StopLadder = prev })
}

func TestStopLadderTickGolden(t *testing.T) {
	withStopLadder(t, true)
	if os.Getenv("STOP_LADDER_WRITE_GOLDEN") != "" {
		var all []string
		for _, c := range m434Cases() {
			all = append(all, m434Digest(t, c)...)
		}
		if err := os.WriteFile(stopLadderGoldenPath, []byte(strings.Join(all, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d tick digests", len(all))
		return
	}
	f, err := os.Open(stopLadderGoldenPath)
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
//   - ON: no non-swing stop above mentor_wide_stop_max_pts (40) [OctD2.3-R47
//     @48:12], every target at least 1:1 (the ladder never enlarges a target
//     [OctD2.2-R49 @40:47]).
//
// It logs every ladder-band entry (stop above the pre-ladder ceiling) so the
// report can name them.
func TestStopLadderRealTapeEntries(t *testing.T) {
	type entry struct {
		at          int
		setup, side string
		risk, rew   float64
	}
	run := func(c m434Case, on bool) []entry {
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
	band := 0
	for _, c := range m434Cases() {
		cfg := c.cfg()
		off, on := run(c, false), run(c, true)
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
			if x.risk > cfg.CourseExits.WideStopMaxPts+eps {
				t.Fatalf("%s ON row %d: %s stop %.2f above mentor_wide_stop_max_pts %g", c.name, x.at, x.setup, x.risk, cfg.CourseExits.WideStopMaxPts)
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
}
