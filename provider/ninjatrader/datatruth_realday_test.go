package ninjatrader

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	"vl/kernel"
)

// REAL-DAY REPLAY of the data-truth checks (m446). The corpus is a READ-ONLY
// export of data/data.db (MNQ 12-26 and ES 12-26, 1m/3m/5m/15m, 10-19 17:00 CT
// .. 10-29 07:00 CT, with rowids) made on 2026-10-29:
//
//	sqlite3 -readonly data/data.db  (see the f7a report for the exporter)
//
// It is NOT committed (1.1 MB gz; sha256
// 0a6f2a61e002d31769817c184e25b3353585f38c9f4a07d2bf47a673e7a74af5, rebuilt by the
// read-only exporter scripts/datatruth_export_corpus.py); the test skips when it is absent
// - the committed fixtures under internal/datatruth/checks/testdata carry the
// pins that need no corpus. For each
// minute of the 10-21 / 10-22 / 10-23 trading days (17:00 CT to 17:00 CT) the
// REAL registered checks run through datatruth.Evaluate on exactly the bars the
// bot held at that instant (time-sliced).

// the corpus carries bars only: no ingest queue, no store reader for the 4h/levels.
var corpusUnrequired = []string{checks.CheckIngestLoss, checks.CheckLookbackSwing4h, checks.CheckLookbackLevels}

const corpusPath = "/tmp/datatruth-f7a/corpus_1019_1029.json.gz"

type corpusSrc struct {
	data map[string][]checks.Bar
	meta map[string]map[int64][2]any // key -> T -> {rowid, source}
	now  time.Time
}

func loadCorpus(t *testing.T) *corpusSrc {
	p := os.Getenv("DATATRUTH_CORPUS")
	if p == "" {
		p = corpusPath
	}
	f, err := os.Open(p)
	if err != nil {
		t.Skipf("corpus %s not present: %v", p, err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string][][]any
	if err := json.NewDecoder(zr).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	c := &corpusSrc{data: map[string][]checks.Bar{}, meta: map[string]map[int64][2]any{}}
	for k, rows := range raw {
		m := map[int64][2]any{}
		for _, r := range rows {
			b := checks.Bar{T: int64(r[0].(float64)), O: r[1].(float64), H: r[2].(float64), L: r[3].(float64), C: r[4].(float64), V: r[5].(float64)}
			c.data[k] = append(c.data[k], b)
			m[b.T] = [2]any{r[6], r[7]}
		}
		c.meta[k] = m
	}
	return c
}

func (c *corpusSrc) Bars(symbol string, tfMin int) ([]checks.Bar, error) {
	all := c.data[fmt.Sprintf("%s|%dm", symbol, tfMin)]
	n := sort.Search(len(all), func(i int) bool { return all[i].T > c.now.UnixMilli() })
	return all[:n], nil
}
func (c *corpusSrc) Partner(s string) string {
	if s == "MNQ" {
		return "ES"
	}
	return ""
}
func (c *corpusSrc) Open() checks.OpenFn {
	return func(ms int64) bool { return kernel.IsCMEOpen(time.UnixMilli(ms)) }
}
func (c *corpusSrc) Contract(string) (checks.ContractFacts, error) {
	return checks.ContractFacts{Live: "MNQ 12-26", History: "MNQ 12-26", Window: []string{"MNQ 12-26"}}, nil
}

// indepHoldMin recomputes, by counting ages instead of the closed-form log, how
// long (minutes) a bar wrong by e points keeps moving an indicator of its frame
// by more than its tolerance: EMA20/EMA50 (1 pt) and MACD26 (2 pt) weight
// k(1-k)^age, ATR14/RSI14/RSI7 (2 pt) weight 1/n for n bars. A frame no
// indicator reads (15m) holds for its own length only.
func indepHoldMin(tf int, e float64) int {
	best := 0
	if tf != 3 && tf != 5 {
		return 0
	}
	for _, sp := range []struct {
		n   int
		ema bool
		tol float64
	}{{20, true, 1}, {50, true, 1}, {26, true, 2}, {14, false, 2}, {7, false, 2}} {
		cnt := 0
		for a := 0; a < 2000; a++ {
			var w float64
			if sp.ema {
				k := 2.0 / float64(sp.n+1)
				w = k
				for i := 0; i < a; i++ {
					w *= 1 - k
				}
			} else if a < sp.n {
				w = 1.0 / float64(sp.n)
			}
			if w*e > sp.tol {
				cnt++
			}
		}
		if cnt > 0 && (cnt+1)*tf > best {
			best = (cnt + 1) * tf
		}
	}
	if best > 480 {
		best = 480
	}
	return best
}

// independentMismatch recomputes, WITHOUT the production helper, whether any
// 3m/5m/15m bar still inside its window disagrees with the 1m it is made of
// (> 0.5 pt on an OHLC field, or volume > 40% and > 200 contracts off). A bar's
// window is max(12 min, its own frame length, the hold of its worst price error).
func independentMismatch(c *corpusSrc, now time.Time) bool {
	m1 := map[int64]checks.Bar{}
	for _, b := range c.data["MNQ|1m"] {
		m1[b.T] = b
	}
	nowMs := now.UnixMilli()
	for _, tf := range []int{3, 5, 15} {
		span := int64(tf) * 60_000
		for _, h := range c.data[fmt.Sprintf("MNQ|%dm", tf)] {
			if h.T+span > nowMs || h.T+span <= nowMs-480*60_000 {
				continue
			}
			var o, hi, lo, cl, v float64
			n, ok := 0, true
			for t := h.T; t < h.T+span; t += 60_000 {
				b, found := m1[t]
				if !found {
					if kernel.IsCMEOpen(time.UnixMilli(t)) {
						ok = false
						break
					}
					continue
				}
				if n == 0 {
					o, hi, lo = b.O, b.H, b.L
				}
				if b.H > hi {
					hi = b.H
				}
				if b.L < lo {
					lo = b.L
				}
				cl = b.C
				v += b.V
				n++
			}
			if !ok || n == 0 {
				continue
			}
			d := func(a, b float64) float64 {
				if a > b {
					return a - b
				}
				return b - a
			}
			worst := 0.0
			px := false
			for _, e := range []float64{d(h.O, o), d(h.H, hi), d(h.L, lo), d(h.C, cl)} {
				if e > 0.5 {
					px = true
					if e > worst {
						worst = e
					}
				}
			}
			if !px && !(d(h.V, v) > 200 && d(h.V, v)/v > 0.40) {
				continue
			}
			win := 12
			if tf > win {
				win = tf
			}
			if hm := indepHoldMin(tf, worst); hm > win {
				win = hm
			}
			if age := (nowMs - (h.T + span)) / 60_000; age < int64(win) {
				return true
			}
		}
	}
	return false
}

type pauseRec struct {
	minutes int
	first   time.Time
	last    time.Time
	detail  string
}

// TestDataTruthRealDayReplay1021_1023 is the proof the checks do not fire on a
// real market and DO fire on the real data defects. It prints the per-day,
// per-check pause minutes (run with -v) and asserts the contract:
//
//   - bad_print: ZERO pauses on 10-21, 10-22, 10-23 (CPI-style news, the 10-22
//     08:30 run of 60-78 pt bars, all confirmed by ES / volume / continuation);
//   - cross_frame: pauses ONLY inside the windows of the real mismatches the
//     corpus holds (every one cited by row id), never anywhere else.
func TestDataTruthRealDayReplay1021_1023(t *testing.T) {
	c := loadCorpus(t)
	loc, _ := time.LoadLocation("America/Chicago")
	datatruth.Reset()
	defer datatruth.Reset()
	checks.RegisterAll(checks.Config{Src: c, Unrequired: corpusUnrequired})
	days := []string{"2026-10-21", "2026-10-22", "2026-10-23"}
	for _, day := range days {
		d0, _ := time.ParseInLocation("2006-01-02", day, loc)
		start := d0.Add(-7 * time.Hour) // 17:00 CT the evening before
		end := d0.Add(17 * time.Hour)
		per := map[string]*pauseRec{}
		evaluated := 0
		for tm := start; tm.Before(end); tm = tm.Add(time.Minute) {
			if !kernel.IsCMEOpen(tm) {
				continue
			}
			c.now = tm.Add(30 * time.Second)
			evaluated++
			_, reasons := datatruth.Evaluate("MNQ", c.now)
			cf := false
			for _, r := range reasons {
				if r.Check == checks.CheckCrossFrame {
					cf = true
				}
				p := per[r.Check]
				if p == nil {
					p = &pauseRec{first: c.now, detail: r.Detail}
					per[r.Check] = p
				}
				p.minutes++
				p.last = c.now
			}
			// the verdict agrees with an independent recomputation, minute by minute
			if want := independentMismatch(c, c.now); want != cf {
				t.Errorf("%s: cross_frame=%v but the independent recomputation says %v", c.now.In(loc).Format("01-02 15:04:05"), cf, want)
			}
		}
		var names []string
		for n := range per {
			names = append(names, n)
		}
		sort.Strings(names)
		t.Logf("REALDAY %s: %d open minutes evaluated; checks that paused: %d", day, evaluated, len(names))
		for _, n := range names {
			p := per[n]
			t.Logf("  %-26s %4d min  first %s  last %s\n      %s", n, p.minutes, p.first.In(loc).Format("01-02 15:04"), p.last.In(loc).Format("15:04"), trim(p.detail, 260))
		}
		if p := per[checks.CheckBadPrint]; p != nil {
			t.Errorf("%s: bad_print paused %d min on a real day: %s", day, p.minutes, p.detail)
		}
		if p := per[checks.CheckContractRoll]; p != nil {
			t.Errorf("%s: contract_roll paused: %s", day, p.detail)
		}
	}
}

func trim(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// TestDataTruthFullCorpusEpisodes replays the whole export (10-19 17:00 .. 10-29
// 07:00 CT) and pins that the checks fire on the REAL defects inside it: the two
// 10-19 bad prints (rows 2020662, 2020732), the 10-26 17:00-21:00 CT 1m hole
// (the P2-6 hole) and the 10-27 08:34 restart-truncated HTF bars (rows 2045884,
// 2045706) - each with its first/last pause minute in the log.
func TestDataTruthFullCorpusEpisodes(t *testing.T) {
	c := loadCorpus(t)
	loc, _ := time.LoadLocation("America/Chicago")
	datatruth.Reset()
	defer datatruth.Reset()
	checks.RegisterAll(checks.Config{Src: c, Unrequired: corpusUnrequired})
	type span struct{ first, last time.Time }
	spans := map[string][]span{}
	open := map[string]bool{}
	start, _ := time.ParseInLocation("2006-01-02 15:04", "2026-10-19 07:00", loc)
	end, _ := time.ParseInLocation("2006-01-02 15:04", "2026-10-29 06:30", loc)
	for tm := start; tm.Before(end); tm = tm.Add(time.Minute) {
		if !kernel.IsCMEOpen(tm) {
			continue
		}
		c.now = tm.Add(30 * time.Second)
		_, reasons := datatruth.Evaluate("MNQ", c.now)
		cur := map[string]bool{}
		for _, r := range reasons {
			cur[r.Check] = true
			if !open[r.Check] {
				spans[r.Check] = append(spans[r.Check], span{first: c.now, last: c.now})
			}
			spans[r.Check][len(spans[r.Check])-1].last = c.now
		}
		open = cur
	}
	for _, n := range []string{checks.CheckBadPrint, checks.CheckCrossFrame, checks.CheckLookback, checks.CheckContractRoll} {
		t.Logf("%s: %d episode(s)", n, len(spans[n]))
		for _, s := range spans[n] {
			t.Logf("    %s -> %s (%d min)", s.first.In(loc).Format("01-02 15:04"), s.last.In(loc).Format("01-02 15:04"), int(s.last.Sub(s.first).Minutes())+1)
		}
	}
	for n, st := range datatruth.Snapshot() {
		t.Logf("owner-push episodes %-26s episodes=%d reopens(merged flaps)=%d", n, st.Episodes, st.Reopens)
	}
	has := func(check, from, to string) bool {
		f, _ := time.ParseInLocation("2006-01-02 15:04", from, loc)
		e, _ := time.ParseInLocation("2006-01-02 15:04", to, loc)
		for _, s := range spans[check] {
			if !s.last.Before(f) && !s.first.After(e) {
				return true
			}
		}
		return false
	}
	if !has(checks.CheckBadPrint, "2026-10-19 08:58", "2026-10-19 09:00") {
		t.Error("10-19 08:58 bad print (row 2020662) did not pause")
	}
	if !has(checks.CheckBadPrint, "2026-10-19 09:19", "2026-10-19 09:20") {
		t.Error("10-19 09:19 bad print (row 2020732) did not pause")
	}
	if !has(checks.CheckCrossFrame, "2026-10-27 08:35", "2026-10-27 08:46") {
		t.Error("10-27 08:30 restart-truncated 5m (row 2045884) did not pause")
	}
	if !has(checks.CheckLookback, "2026-10-26 17:30", "2026-10-26 20:30") {
		t.Error("the 10-26 17:00-21:00 CT 1m hole did not pause the indicator lookback")
	}
	if len(spans[checks.CheckContractRoll]) != 0 {
		t.Errorf("contract_roll paused on a single-contract corpus: %v", spans[checks.CheckContractRoll])
	}
}
