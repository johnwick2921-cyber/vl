package trader

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// ── SINGLE SESSION-DAY REPLAY FIXTURES (m277 r3) ─────────────────────────────
//
// One fixture per CME session-day, generated READ-ONLY from data.db by
// scripts/gen_day_fixtures.py in the SAME shape and seed convention as the
// proven multi-day fixture (mnq_1m_2026-10-21_multiday.json.gz): bars/{1m,1h}
// from the first recorded 1m bar (the seed depth is the whole warm-up history),
// seed_now_ct_ms = the session open (17:00 CT the prior day; Sunday 17:00 CT
// for Monday), window_ct_ms = [open, 16:00 CT of the trade date].
//
// Run any one by date, without touching the default 10-13 → 10-21 replay:
//
//	FULL_DAY_REPLAY=1 FULL_DAY_REPLAY_DATE=2026-10-14 go test ./trader -run '^TestDayReplay$' -v
//
// 10-17 is a Saturday: no session, no fixture. The 10-18 Sunday open (17:00 CT)
// is the first half of session-day 10-19 (2026-10-19 fixture).
//
// HOLES: no day fixture has a hole inside its window (all 1380 one-minute bars
// present). The 10-20 23:24 → 00:38 CT 74-minute live outage belongs to session
// 10-21 (inside the multi-day fixture, not a day fixture); the bars table holds
// it BACKFILLED (source=historical), so the replay sees a full tape where the
// live bot saw a hole. The backfilled-bar counts below are the same caveat for
// each day: live trades on those minutes were decided on a thinner tape than
// the replay's.

type dayFixtureSpec struct {
	date       string
	seedMs     int64 // session open (17:00 CT)
	endMs      int64 // 16:00 CT of the trade date
	bars1m     int   // total 1m rows in the file (seed + window + 2 h tail)
	bars1h     int
	seed1m     int // 1m rows strictly before seedMs
	backfilled int // window minutes the bars table holds as source=historical (NOT in the file; documentation)
}

var dayFixtureSpecs = []dayFixtureSpec{
	{"2026-10-13", 1791842400000, 1791925200000, 129483, 3737, 128042, 0},
	{"2026-10-14", 1791928800000, 1792011600000, 130863, 3760, 129422, 397},
	{"2026-10-15", 1792015200000, 1792098000000, 132243, 3783, 130802, 255},
	{"2026-10-16", 1792101600000, 1792184400000, 133562, 3804, 132182, 155},
	{"2026-10-19", 1792360800000, 1792443600000, 135003, 3829, 133562, 15},
	{"2026-10-20", 1792447200000, 1792530000000, 136383, 3852, 134942, 198},
}

const (
	dayWindow1m   = 1380          // 23 h x 60: a full 17:00 → 16:00 CT session, no holes
	dayFirst1mMs  = 1780437660000 // first recorded MNQ 09-26 1m bar = the multi-day fixture's first row
	dayFixtureFmt = "testdata/mnq_1m_%s_day.json.gz"
)

type dayFixtureDoc struct {
	Bars        map[string][][]json.RawMessage `json:"bars"`
	SeedNowCTMs int64                          `json:"seed_now_ct_ms"`
	WindowCTMs  []int64                        `json:"window_ct_ms"`
}

func loadDayFixtureDoc(t *testing.T, path string) dayFixtureDoc {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip %s: %v", path, err)
	}
	defer gz.Close()
	var doc dayFixtureDoc
	if err := json.NewDecoder(gz).Decode(&doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

func openMs(t *testing.T, row []json.RawMessage) int64 {
	t.Helper()
	var ot float64
	if len(row) < 6 || json.Unmarshal(row[0], &ot) != nil {
		t.Fatalf("bad bar row %v", row)
	}
	return int64(ot)
}

// TestDayFixturesLoad pins every single-day fixture: bar counts, the seed depth
// before the session open, the window, and a hole-free 1380-minute window.
// MUTANTS (each run RED): drop one 1m bar inside a window → window count/hole
// assertion; truncate the 1m series before the seed → seed1m/first-bar; shift
// seed_now_ct_ms by a minute → seed/window assertion.
func TestDayFixturesLoad(t *testing.T) {
	for _, sp := range dayFixtureSpecs {
		sp := sp
		t.Run(sp.date, func(t *testing.T) {
			doc := loadDayFixtureDoc(t, fmt.Sprintf(dayFixtureFmt, sp.date))
			if doc.SeedNowCTMs != sp.seedMs || len(doc.WindowCTMs) != 2 ||
				doc.WindowCTMs[0] != sp.seedMs || doc.WindowCTMs[1] != sp.endMs {
				t.Fatalf("seed/window = %d %v, want %d [%d %d]", doc.SeedNowCTMs, doc.WindowCTMs, sp.seedMs, sp.seedMs, sp.endMs)
			}
			m, h := doc.Bars["1m"], doc.Bars["1h"]
			if len(m) != sp.bars1m || len(h) != sp.bars1h {
				t.Fatalf("bars 1m=%d 1h=%d, want 1m=%d 1h=%d", len(m), len(h), sp.bars1m, sp.bars1h)
			}
			if got := openMs(t, m[0]); got != dayFirst1mMs {
				t.Fatalf("first 1m open = %d, want %d (seed depth = full warm-up history)", got, dayFirst1mMs)
			}
			seed, win := 0, 0
			var prev int64
			for i, r := range m {
				ot := openMs(t, r)
				if i > 0 && ot <= prev {
					t.Fatalf("1m not strictly ascending at row %d (%d after %d)", i, ot, prev)
				}
				switch {
				case ot < sp.seedMs:
					seed++
				case ot < sp.endMs:
					win++
					if prev >= sp.seedMs && ot-prev != 60_000 {
						t.Fatalf("HOLE in window: %d → %d (%d min)", prev, ot, (ot-prev)/60_000-1)
					}
				}
				prev = ot
			}
			if seed != sp.seed1m {
				t.Fatalf("seed 1m rows = %d, want %d", seed, sp.seed1m)
			}
			if win != dayWindow1m {
				t.Fatalf("window 1m rows = %d, want %d (a full 17:00 → 16:00 CT session, no holes)", win, dayWindow1m)
			}
			// 1h: same lower bound as 1m, ascending, ends inside the tail.
			if got := openMs(t, h[0]); got < dayFirst1mMs {
				t.Fatalf("first 1h open %d precedes the first 1m bar", got)
			}
			for i := 1; i < len(h); i++ {
				if openMs(t, h[i]) < openMs(t, h[i-1]) {
					t.Fatalf("1h not ascending at row %d", i)
				}
			}
			// The tail past 16:00 CT is at most 2 h of 1m bars.
			if last := openMs(t, m[len(m)-1]); last > sp.endMs+2*3600*1000 {
				t.Fatalf("last 1m open %d is past the 2 h tail after %d", last, sp.endMs)
			}
		})
	}
}

// TestDayFixtureSpecsAreSessions pins the spec table itself against the CME
// session rule (open = 17:00 CT of the prior calendar day, Sunday for Monday;
// close = 16:00 CT), so a wrong constant cannot hide in the table.
func TestDayFixtureSpecsAreSessions(t *testing.T) {
	for _, sp := range dayFixtureSpecs {
		// every session, Sunday-open Monday included, is 17:00 → 16:00 CT = 23 h
		if got := sp.endMs - sp.seedMs; got != 23*3600*1000 {
			t.Errorf("%s: window = %d ms, want 23 h", sp.date, got)
		}
	}
}

// TestDayReplay replays ONE session-day fixture through the production mentor
// path (same driver as TestMultiDayReplay). FULL_DAY_REPLAY=1 and
// FULL_DAY_REPLAY_DATE=YYYY-MM-DD; the default multi-day replay is untouched.
func TestDayReplay(t *testing.T) {
	date := os.Getenv("FULL_DAY_REPLAY_DATE")
	if os.Getenv("FULL_DAY_REPLAY") != "1" || date == "" {
		t.Skip("single-day replay runs with FULL_DAY_REPLAY=1 FULL_DAY_REPLAY_DATE=YYYY-MM-DD")
	}
	for _, sp := range dayFixtureSpecs {
		if sp.date == date {
			mdrRun(t, fmt.Sprintf(dayFixtureFmt, date), sp.seedMs, sp.endMs, "DAY "+date)
			return
		}
	}
	t.Fatalf("FULL_DAY_REPLAY_DATE=%q has no fixture (have: 2026-10-13 14 15 16 19 20)", date)
}
