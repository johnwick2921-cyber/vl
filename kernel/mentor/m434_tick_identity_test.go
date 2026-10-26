package mentor

// m434 — the per-Tick performance work (#51 made the mentor Tick several times
// slower: m428 R5's non-adjacent pairing re-ran an O(swings^2 x bars) crossed
// scan on every Tick) must change NO output. This test replays the REAL
// Evaluator, one Tick per closed 1m bar over a recorded tape, and compares per
// tick a digest of (1) the intents stream, (2) the State JSON, (3) the notes,
// (4) ChartLevels and (5) ChartTrendlines against testdata/m434_tick_identity.golden,
// recorded from the UNOPTIMISED code at 75707f1ed. One line per tick; a mismatch
// names the first differing tick and which of the five streams moved.
//
// Regenerate (only when a deliberate behaviour change lands):
//   M434_WRITE_GOLDEN=1 go test -count=1 -run TestM434TickOutputIsByteIdentical ./kernel/mentor
// The reference implementation of the hot function also lives here
// (m434RefBestNonAdjacentPartner = the 75707f1ed body) and is compared on the
// real tape in TestM434BestPartnerMatchesReference.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
)

const m434GoldenPath = "testdata/m434_tick_identity.golden"

type m434Case struct {
	name  string
	scale float64 // flatNearScale: 1 = production, 0 = the package's synthetic-tape default
	tape  func(t *testing.T) []market.Kline
	cfg   func() Config
	from  int // first tick's bar index
}

func m434Cases() []m434Case {
	return []m434Case{
		{
			name:  "m423_1023_default_cfg_near1",
			scale: 1,
			tape:  m423Bars,
			cfg:   func() Config { c := DefaultConfig(); c.Enabled = true; return c },
			from:  150,
		},
		{
			name:  "m423_1023_1m_read_near0",
			scale: 0,
			tape:  m423Bars,
			cfg: func() Config {
				c := DefaultConfig()
				c.Enabled = true
				c.Exec2mAfter30m = false
				return c
			},
			from: 150,
		},
		{
			name:  "r22_1013_session_near1",
			scale: 1,
			tape:  func(t *testing.T) []market.Kline { return loadFixture(t, "mnq_1m_2026-10-13_session", "1m") },
			cfg:   r22Cfg,
			from:  -1, // r22Open1013 window
		},
		{
			name:  "r22_1013_session_exec2m_near1",
			scale: 1,
			tape:  func(t *testing.T) []market.Kline { return loadFixture(t, "mnq_1m_2026-10-13_session", "1m") },
			cfg: func() Config {
				c := r22Cfg()
				c.Exec2mAfter30m = true
				return c
			},
			from: -1,
		},
	}
}

func m434Sum(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:10]
}

// m434Digest ticks a fresh evaluator over the case's tape and returns one line
// per tick: "<case> <i> <intents> <state> <notes> <levels> <trendlines>".
func m434Digest(t *testing.T, c m434Case) []string {
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
	e := New(c.cfg())
	var lines []string
	for i := from; i < len(bars); i++ {
		lo := i + 1 - 1500
		if lo < 0 {
			lo = 0
		}
		win := bars[lo : i+1]
		out := e.Tick(win, BarCloseInstant(bars[i]))
		ij, _ := json.Marshal(out)
		sj, err := json.Marshal(e.State)
		if err != nil {
			t.Fatalf("%s tick %d: state json: %v", c.name, i, err)
		}
		notes := strings.Join(e.TakeNotes(), "\n")
		// Order-insensitive: carryIntradayLines appends carried lines in Go map
		// order (pre-existing, display-only), so the SET is compared, not the order.
		var lvs, tls []string
		for _, l := range e.State.ChartLevels {
			lvs = append(lvs, fmt.Sprintf("%+v", l))
		}
		for _, l := range e.State.ChartTrendlines {
			tls = append(tls, fmt.Sprintf("%+v", l))
		}
		sort.Strings(lvs)
		sort.Strings(tls)
		lv, tl := strings.Join(lvs, "\n"), strings.Join(tls, "\n")
		lines = append(lines, fmt.Sprintf("%s %d %s %s %s %s %s", c.name, i,
			m434Sum(string(ij)), m434Sum(string(sj)), m434Sum(notes), m434Sum(lv), m434Sum(tl)))
	}
	return lines
}

func TestM434TickOutputIsByteIdentical(t *testing.T) {
	if os.Getenv("M434_WRITE_GOLDEN") != "" {
		var all []string
		for _, c := range m434Cases() {
			all = append(all, m434Digest(t, c)...)
		}
		if err := os.WriteFile(m434GoldenPath, []byte(strings.Join(all, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d tick digests", len(all))
		return
	}
	f, err := os.Open(m434GoldenPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		ln := sc.Text()
		name := strings.SplitN(ln, " ", 2)[0]
		want[name] = append(want[name], ln)
	}
	for _, c := range m434Cases() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := m434Digest(t, c)
			w := want[c.name]
			if len(w) == 0 {
				t.Fatalf("golden has no rows for %s", c.name)
			}
			if len(got) != len(w) {
				t.Fatalf("%s: %d ticks, golden has %d", c.name, len(got), len(w))
			}
			names := []string{"intents", "state", "notes", "levels", "trendlines"}
			for i := range got {
				if got[i] == w[i] {
					continue
				}
				g, wf := strings.Fields(got[i]), strings.Fields(w[i])
				var moved []string
				for k := 2; k < len(g) && k < len(wf); k++ {
					if g[k] != wf[k] {
						moved = append(moved, names[k-2])
					}
				}
				t.Fatalf("%s: first divergence at tick row %d (bar %s): %v differ\n got  %s\n want %s", c.name, i, g[1], moved, got[i], w[i])
			}
		})
	}
}

// m434RefBestNonAdjacentPartner is bestNonAdjacentPartner exactly as it stood at
// 75707f1ed (the unoptimised reference: test-only).
func m434RefBestNonAdjacentPartner(fb []market.Kline, earlier []swingPairAt, p1 swingPairAt, role kernel.LevelKind, dayStart int) (swingPairAt, bool) {
	var best swingPairAt
	bestScore, found := -1, false
	side := SideLong
	if role == kernel.KindSWGH {
		side = SideShort
	}
	for _, p0 := range earlier {
		tl, ok := newSlopedLine(fb, role, side, p0, p1)
		if !ok || m434RefCrossed(fb, tl) {
			continue
		}
		if sc := m434RefStanding(fb, p0, dayStart); sc > bestScore {
			best, bestScore, found = p0, sc, true
		}
	}
	return best, found
}

func m434RefCrossed(bars []market.Kline, tl Trendline) bool {
	for i := tl.P0Idx + 1; i < tl.P1Idx && i < len(bars); i++ {
		if tl.wickBeyond(bars[i]) {
			return true
		}
	}
	return false
}

func m434RefStanding(fb []market.Kline, s swingPairAt, dayStart int) int {
	n := 0
	for k := s.idx - 1; k >= dayStart && k >= 0; k-- {
		if s.kind == kernel.KindSWGL && fb[k].Low <= s.price {
			break
		}
		if s.kind == kernel.KindSWGH && fb[k].High >= s.price {
			break
		}
		n++
	}
	return n
}

// TestM434BestPartnerMatchesReference — the optimised partner pick equals the
// reference for EVERY (later swing, role) of the real 10-23 tape on both build
// frames, with the near distance both on and off.
func TestM434BestPartnerMatchesReference(t *testing.T) {
	bars := m423Bars(t)
	now := time.UnixMilli(BarCloseInstant(bars[len(bars)-1]))
	for _, scale := range []float64{0, 1} {
		old := flatNearScale
		flatNearScale = scale
		for _, exec2m := range []bool{false, true} {
			fb, _ := execBuildFrame(bars, now, exec2m)
			today := tradingDayKey(now.In(ctime()))
			all := swings3(fb)
			dayStart := len(fb)
			for i, b := range fb {
				if tradingDayKey(time.UnixMilli(b.OpenTime).In(ctime())) == today {
					dayStart = i
					break
				}
			}
			checked := 0
			for _, role := range []kernel.LevelKind{kernel.KindSWGL, kernel.KindSWGH} {
				var pts []swingPairAt
				for _, s := range all {
					if s.kind == role && swingConfirmed(fb, s) && tradingDayKey(time.UnixMilli(fb[s.idx].OpenTime).In(ctime())) == today {
						pts = append(pts, s)
					}
				}
				for j := 2; j < len(pts); j++ {
					wb, wok := m434RefBestNonAdjacentPartner(fb, pts[:j-1], pts[j], role, dayStart)
					gb, gok := bestNonAdjacentPartner(fb, pts[:j-1], pts[j], role, dayStart, newStandingMemo(fb, dayStart))
					if wb != gb || wok != gok {
						t.Fatalf("scale %v exec2m %v role %v j=%d: got (%+v,%v) want (%+v,%v)", scale, exec2m, role, j, gb, gok, wb, wok)
					}
					checked++
				}
			}
			if checked < 20 {
				t.Fatalf("scale %v exec2m %v: only %d partner picks compared", scale, exec2m, checked)
			}
		}
		flatNearScale = old
	}
}

// BenchmarkM434Tick is ONE production Tick on the real 10-23 tape late in the
// session (15:00 CT: the whole day's swings exist, so the per-Tick trendline
// build is at its largest), default config, production near distance.
func BenchmarkM434Tick(b *testing.B) {
	t := &testing.T{}
	bars := m423Bars(t)
	win, now := m423Upto(t, bars, 15, 0)
	if len(win) < 600 {
		b.Fatalf("window %d", len(win))
	}
	cfg := DefaultConfig()
	cfg.Enabled = true
	old := flatNearScale
	flatNearScale = 1
	defer func() { flatNearScale = old }()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := New(cfg)
		e.Tick(win, now)
	}
}

// BenchmarkM434TickR22 is one Tick at 14:30 CT on the busier recorded 10-13 day
// over the live 1500-bar window (the r22 tests' shape).
func BenchmarkM434TickR22(b *testing.B) {
	t := &testing.T{}
	bars := loadFixture(t, "mnq_1m_2026-10-13_session", "1m")
	cut := time.Date(2026, time.October, 13, 14, 30, 0, 0, ctime()).UnixMilli()
	n := 0
	for n < len(bars) && bars[n].OpenTime < cut {
		n++
	}
	win := bars[n-1500 : n]
	cfg := r22Cfg()
	old := flatNearScale
	flatNearScale = 1
	defer func() { flatNearScale = old }()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := New(cfg)
		e.Tick(win, BarCloseInstant(win[len(win)-1]))
	}
}
