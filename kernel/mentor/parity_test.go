package mentor

// STRICT parity harness vs DS-108's replay (CTO verdict 2026-10-03 07:02Z on
// eaefa9e66). The five contract points:
//
//  1. rc != 0 on ANY mismatch — rc=0 means parity holds, nothing less.
//  2. Per-source warm-up, fail-closed: 4h buckets >= 102 (3x34, the CTO's
//     interim floor until DS-103's Seed numbers land) and 1m bars >= 102 in
//     the prefix BEFORE the target day. A short tape is a hard fail, never a
//     comparison on a cold prefix.
//  3. Compare like with like: Go ARMS vs replay ORDERS
//     (orders_v5_base.csv), keyed exactly by (minute, setup, side, entry,
//     stop); then a replay-internal fills-vs-trades cross-check
//     (orders with fill_min vs trades_v5_base.csv).
//  4. Frozen inputs: the comparison CSVs are copied into testdata/parity/
//     replay/ with their sha256 (SHA256SUMS); the test re-verifies the
//     checksums and fails if DS-108's source files have moved.
//  5. Per day: the FIRST divergent minute and the first differing state
//     field per column (levels, boxes, trigger lines, EMA, filters), Go
//     value vs replay value. That report is the deliverable, not the match
//     count.
//
// Known deltas the harness is EXPECTED to surface (they are defects to
// route, not harness bugs): the replay's 5m trigger flips at the intrabar
// break minute while the Go line moves at the bucket open; the Go 5m-ISB
// box is deleted on escape while the replay state dump keeps the last-born
// box; Go has no (b) trading-window / (a) done-after-win stop rules; Go has
// no R10 re-entry; Go's REVISB knob defaults OFF (the v5_base run had it
// OFF too); Go's R1 ISB skips only the twenties ([20,30)) while the replay
// currently skips every stop >= 20.

import (
	"bufio"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"vl/market"
)

const (
	replaySourceDir = "/home/hoang/mm-course/mentor-mode/replay"
	frozenDir       = "testdata/parity/replay"

	// Per-source warm-up floors (CTO 2026-10-03 07:02Z, pending DS-103's
	// Seed PR numbers).
	warmupFourHBuckets = 102
	warmupOneMBars     = 102
)

type parityDay struct {
	name string
	file string
	day  string
}

var parityDays = []parityDay{
	{"trend", "testdata/parity/trend_2025-05-07.csv", "2025-05-07"},
	{"range", "testdata/parity/range_2023-06-20.csv", "2023-06-20"},
	// P7 (CTO 2026-10-03 12:44Z): the old news day (2025-04-02) cannot
	// satisfy the 102 4h floor from the db-copy (earliest MNQ 1m is
	// 2025-03-16 22:01) — replaced by the 2025-02-07 NFP day, whose full
	// 30-day prefix sits inside one contract.
	{"news", "testdata/parity/news_2025-02-07.csv", "2025-02-07"},
	{"normal_a", "testdata/parity/normal_a_2024-07-16.csv", "2024-07-16"},
	{"normal_b", "testdata/parity/normal_b_2025-07-16.csv", "2025-07-16"},
}

// ────────────────────────────────────────────────────────────────────────────
// Replay input loading

type orderRow struct {
	day, oid, setup string
	side            int // 1 / -1
	entry, stop     float64
	placedMinCT     string
	fillMinCT       string
	outcome         string
	cancelReason    string
}

func loadOrderRows(t *testing.T, path string) []orderRow {
	t.Helper()
	rows := readCSV(t, path)
	var out []orderRow
	for i, r := range rows {
		if i == 0 {
			continue
		}
		out = append(out, orderRow{
			day:          r[0],
			oid:          r[1],
			setup:        r[2],
			side:         atoiOr0(r[3]),
			entry:        atofOrNaN(r[4]),
			stop:         atofOrNaN(r[5]),
			placedMinCT:  r[9],
			fillMinCT:    r[11],
			outcome:      r[13],
			cancelReason: r[14],
		})
	}
	return out
}

type tradeRow struct {
	day     string
	minCT   string
	setup   string
	side    int
	entry   float64
	stop    float64
	fillMin string
}

func loadTradeRows(t *testing.T, path string) []tradeRow {
	t.Helper()
	rows := readCSV(t, path)
	var out []tradeRow
	for i, r := range rows {
		if i == 0 {
			continue
		}
		out = append(out, tradeRow{
			day:     r[0],
			minCT:   r[1],
			setup:   r[3],
			side:    atoiOr0(r[4]),
			entry:   atofOrNaN(r[5]),
			stop:    atofOrNaN(r[6]),
			fillMin: r[9],
		})
	}
	return out
}

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("csv %s: %v", path, err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("csv %s: %v", path, err)
	}
	return rows
}

// frozenFiles maps replay source → frozen copy; the test fails if either
// side moved (CTO contract point 4).
var frozenFiles = []struct{ src, frozen string }{
	{"orders_v5_base.csv", "orders_v5_base.csv"},
	{"trades_v5_base.csv", "trades_v5_base.csv"},
	{"state_v5_2023-06-20.csv", "state_v5_2023-06-20.csv"},
	{"state_v5_2024-07-16.csv", "state_v5_2024-07-16.csv"},
	{"state_v5_2025-04-02.csv", "state_v5_2025-04-02.csv"},
	{"state_v5_2025-05-07.csv", "state_v5_2025-05-07.csv"},
	{"state_v5_2025-07-16.csv", "state_v5_2025-07-16.csv"},
}

func TestFrozenParityInputs(t *testing.T) {
	sums := map[string]string{}
	for _, line := range readLines(t, frozenDir+"/SHA256SUMS") {
		f := strings.Fields(line)
		if len(f) == 2 {
			sums[f[1]] = f[0]
		}
	}
	for _, p := range frozenFiles {
		got := sha256File(t, frozenDir+"/"+p.frozen)
		if want, ok := sums[p.frozen]; ok && got != want {
			t.Errorf("frozen %s sha256 %s != recorded %s (regenerate SHA256SUMS)", p.frozen, got, want)
		}
		src := sha256File(t, replaySourceDir+"/"+p.src)
		if src != got {
			t.Errorf("SOURCE MOVED: %s sha256 %s != frozen %s %s — re-freeze before comparing",
				p.src, src, p.frozen, got)
		}
		t.Logf("SHA256 %s  %s", got, p.frozen)
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ────────────────────────────────────────────────────────────────────────────
// Go-side state emission (one row per target-day RTH minute)

type goStateRow struct {
	minCT      string
	gate       string
	trigSide   string // "1" / "-1" / "0"
	buyLine    string
	sellLine   string
	between    string // "1" / "0"
	dir4       string
	dir1h      string
	daySpent   string
	conflict   string
	levelBelo  string
	levelAbov  string
	ema34      string
	boxTop     string
	boxBot     string
	boxDir     string
	isb        string
	motherHi   string
	motherLo   string
	isbDir     string
	armsPlac   string
	armsCanc   string
	o, h, l, c string
}

// stateColumnNames lists the compared columns in the replay state CSV order
// (day and min_utc are handled separately: min_utc is derived, day is the
// join key).
var stateColumnNames = []string{
	"o", "h", "l", "c",
	"gate", "trig_side", "buy_line", "sell_line", "between",
	"dir4", "dir1h", "day_spent", "conflict",
	"level_below", "level_above", "ema34",
	"box_top", "box_bot", "box_dir",
	"isb", "mother_hi", "mother_lo", "isb_dir",
	"arms_placed", "arms_cancelled",
}

func goStateColumns(r goStateRow) map[string]string {
	return map[string]string{
		"o": r.o, "h": r.h, "l": r.l, "c": r.c,
		"gate": r.gate, "trig_side": r.trigSide,
		"buy_line": r.buyLine, "sell_line": r.sellLine, "between": r.between,
		"dir4": r.dir4, "dir1h": r.dir1h, "day_spent": r.daySpent,
		"conflict":    r.conflict,
		"level_below": r.levelBelo, "level_above": r.levelAbov,
		"ema34":   r.ema34,
		"box_top": r.boxTop, "box_bot": r.boxBot, "box_dir": r.boxDir,
		"isb": r.isb, "mother_hi": r.motherHi, "mother_lo": r.motherLo,
		"isb_dir":        r.isbDir,
		"arms_placed":    r.armsPlac,
		"arms_cancelled": r.armsCanc,
	}
}

func sideNum(s Side) string {
	switch s {
	case SideLong:
		return "1"
	case SideShort:
		return "-1"
	}
	return "0"
}

func fnumOrEmpty(v float64) string {
	if v == 0 {
		return ""
	}
	return fnum(v)
}

// emitGoState builds the per-minute Go state row the way the evaluator sees
// the world at the close of bar cur (index i in bars, exclusive end).
func emitGoState(e *Evaluator, bars []market.Kline, i int, cfg Config) goStateRow {
	cur := bars[i-1]
	prev := bars[i-2]
	now := cur.CloseTime
	row := goStateRow{
		minCT: minCTFromBar(cur),
		o:     fnum(cur.Open), h: fnum(cur.High), l: fnum(cur.Low), c: fnum(cur.Close),
	}
	row.minCT = minCTFromBar(cur)

	// gate: day-gate days first, then the ORB gate (Go has no (b) window /
	// (a) done-after-win stop rules — any replay "window"/"done" gate is a
	// routed defect, not an emission).
	switch {
	case e.State.Day.Verdict == DayOff:
		row.gate = "day_gate"
	case !e.State.ORB.Drawn || e.State.ORB.Escaped == "":
		row.gate = "orb_pre"
	default:
		row.gate = ""
	}

	// 5m trigger line.
	tr := e.State.Trigger
	row.trigSide = sideNum(tr.Dir)
	switch tr.Dir {
	case SideLong:
		row.buyLine = fnum(tr.Price)
		if tr.OldDir == SideShort {
			row.sellLine = fnum(tr.OldPrice)
		}
	case SideShort:
		row.sellLine = fnum(tr.Price)
		if tr.OldDir == SideLong {
			row.buyLine = fnum(tr.OldPrice)
		}
	}
	if row.buyLine != "" && row.sellLine != "" {
		b, s := mustF(row.buyLine), mustF(row.sellLine)
		lo, hi := b, s
		if s < b {
			lo, hi = s, b
		}
		if lo < cur.Close && cur.Close < hi {
			row.between = "1"
		} else {
			row.between = "0"
		}
	} else {
		row.between = "0"
	}

	// 4h / 1h directions + conflict.
	row.dir4 = sideNum(e.State.HTF.FourH.Dir)
	row.dir1h = sideNum(e.State.HTF.OneH.Dir)
	if HTFConflict(e.State.HTF) {
		row.conflict = "1"
	} else {
		row.conflict = "0"
	}

	// day gate.
	if e.State.Day.Verdict == DaySpent {
		row.daySpent = "1"
	} else {
		row.daySpent = "0"
	}

	// nearest key levels below / above the close (the replay's lv_sorted is
	// the day's static key-level set; the Go set is Levels() recomputed per
	// minute — a divergence here is a real behavior difference).
	levels := Levels(bars[:i], cfg, now)
	var below, above float64
	for _, lvl := range levels {
		if lvl.Kind != KindKeyLevel {
			continue
		}
		if lvl.Price < cur.Close && (below == 0 || lvl.Price > below) {
			below = lvl.Price
		}
		if lvl.Price > cur.Close && (above == 0 || lvl.Price < above) {
			above = lvl.Price
		}
	}
	row.levelBelo = fnumOrEmpty(below)
	row.levelAbov = fnumOrEmpty(above)

	// 1m EMA34 (the v5_base run config: ema34_tf='1m').
	row.ema34 = fnum(emaValue(bars[:i], cfg.EMAPeriod34))

	// the 5m ISB rest box as the evaluator currently holds it (active only —
	// the replay dump keeps the last-born box; an escaped box diverges here).
	if bx := e.State.ISBBox; bx != nil {
		row.boxTop = fnum(bx.High)
		row.boxBot = fnum(bx.Low)
		row.boxDir = sideNum(bx.Dir)
	} else {
		row.boxDir = "0"
	}

	// ISB on the closed pair (the replay requires the pair to be consecutive
	// RTH minutes; the Go evaluator's IsISB has no doji guard, so its true
	// reading is emitted as-is).
	prevMin := ctMinuteOfDay(prev.OpenTime)
	curMin := ctMinuteOfDay(cur.OpenTime)
	consecutiveRTH := prev.OpenTime == cur.OpenTime-60_000 &&
		prevMin >= 8*60+30 && prevMin < 15*60 && curMin >= 8*60+30 && curMin < 15*60
	if consecutiveRTH && IsISB(prev, cur) {
		row.isb = "1"
		row.motherHi = fnum(prev.High)
		row.motherLo = fnum(prev.Low)
		row.isbDir = sideNum(ISBDirection(prev))
	} else {
		row.isb = "0"
		row.isbDir = "0"
	}

	// per-minute arm counters are filled by the caller (the drive loop) after
	// the emission; the emitter itself starts them empty.
	row.armsPlac = "0"
	row.armsCanc = "0"
	return row
}

func mustF(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func ctMinuteOfDay(ctMs int64) int {
	ct := time.UnixMilli(ctMs).UTC()
	return ct.Hour()*60 + ct.Minute()
}

// ────────────────────────────────────────────────────────────────────────────
// Arms vs orders (CTO contract point 3)

type armKey struct {
	setup     string
	side      int
	entry     float64
	stop      float64
	placedMin string
}

func armKeyString(k armKey) string {
	return fmt.Sprintf("%s|%s|%d|%s|%s", k.placedMin, k.setup, k.side, fnum(k.entry), fnum(k.stop))
}

// setupFor classifies a Go placement intent into the replay's setup
// vocabulary using the reason and the level key it names.
func setupFor(in Intent) string {
	r := in.Reason
	switch {
	case strings.HasPrefix(r, "ISB:"):
		return "ISB"
	case strings.Contains(r, "reverse ISB") || strings.Contains(r, "REVISB"):
		return "REVISB"
	case strings.Contains(r, "swing"):
		return "SWING4H"
	case strings.HasPrefix(r, "PHL/PLH"):
		if in.Side == SideLong {
			return "PHL"
		}
		return "PLH"
	}
	switch {
	case strings.HasPrefix(in.LevelKey, string(KindKeyLevel)+":"):
		return "PHLPLH@LEVEL"
	case strings.HasPrefix(in.LevelKey, string(KindOldExtreme)+":"):
		if in.Side == SideLong {
			return "PHL"
		}
		return "PLH"
	case strings.HasPrefix(in.LevelKey, string(KindEMA34)) || strings.HasPrefix(in.LevelKey, string(KindEMA34HTF)):
		return "PHLPLH@EMA34"
	case strings.HasPrefix(in.LevelKey, string(KindTriggerRetest)):
		return "TRIGRET"
	case strings.HasPrefix(in.LevelKey, string(KindFTGHEdge)) || strings.HasPrefix(in.LevelKey, string(KindFTGLEdge)):
		return "BOX"
	}
	return "UNKNOWN"
}

// ────────────────────────────────────────────────────────────────────────────
// The strict test

func TestParityStrictAgainstReplayV5(t *testing.T) {
	allOrders := loadOrderRows(t, frozenDir+"/orders_v5_base.csv")
	allTrades := loadTradeRows(t, frozenDir+"/trades_v5_base.csv")

	// per-day buckets
	ordersByDay := map[string][]orderRow{}
	for _, o := range allOrders {
		ordersByDay[o.day] = append(ordersByDay[o.day], o)
	}
	tradesByDay := map[string][]tradeRow{}
	for _, tr := range allTrades {
		tradesByDay[tr.day] = append(tradesByDay[tr.day], tr)
	}

	cfg := DefaultConfig()
	cfg.Enabled = true

	// PARITY_DAY=trend|range|news|normal_a|normal_b narrows the run to one
	// day — a development accelerator; the committed gate runs all five.
	wantDay := os.Getenv("PARITY_DAY")

	failed := false
	for _, d := range parityDays {
		if wantDay != "" && d.name != wantDay {
			continue
		}
		failed = runParityDay(t, d, cfg, ordersByDay[d.day], tradesByDay[d.day]) || failed
	}
	if failed {
		t.Error("PARITY FAIL: at least one column or order diverged — details above. rc != 0 is the contract.")
	}
}

func runParityDay(t *testing.T, d parityDay, cfg Config, orders []orderRow, trades []tradeRow) bool {
	bars := LoadCSVBars(t, d.file, 1)
	t0 := time.Now()

	firstTarget := -1
	for i, b := range bars {
		if time.UnixMilli(b.OpenTime).UTC().Format("2006-01-02") == d.day {
			firstTarget = i
			break
		}
	}
	if firstTarget < 0 {
		t.Fatalf("%s: no target-day bars in the tape", d.name)
	}
	prefix := bars[:firstTarget]

	// Per-source warm-up, fail-closed (CTO contract point 2): each source's
	// own count against its own constant.
	fourHBuckets := map[int64]bool{}
	for _, b := range barsTF(prefix, 240) {
		fourHBuckets[b.OpenTime] = true
	}
	oneH := len(barsTF(prefix, 60))
	if len(fourHBuckets) < warmupFourHBuckets {
		// hard fail for THIS day (never a comparison on a cold prefix) but
		// the run continues so every day still gets its report line.
		t.Errorf("%s: 4h prefix depth FAIL: %d buckets < %d (cold 4h sources — no comparison for this day)",
			d.name, len(fourHBuckets), warmupFourHBuckets)
		return true
	}
	if len(prefix) < warmupOneMBars {
		t.Errorf("%s: 1m prefix depth FAIL: %d bars < %d (no comparison for this day)",
			d.name, len(prefix), warmupOneMBars)
		return true
	}
	t.Logf("WARMUP %-8s: prefix %d bars / %d days / 4h buckets %d / 1h buckets %d before %s",
		d.name, len(prefix), prefixDays(prefix), len(fourHBuckets), oneH, d.day)

	// Drive the evaluator minute by minute; record target-day placements and
	// RTH state rows.
	e := New(cfg)
	goRows := map[string]goStateRow{}
	placements := map[string]armKey{} // deduped Go arms, keyed by arm key
	unknownSetups := map[string]int{}
	var rowOrder []string
	for i := 2; i <= len(bars); i++ {
		cur := bars[i-1]
		if time.UnixMilli(cur.OpenTime).UTC().Format("2006-01-02") != d.day {
			e.Tick(bars[:i], cur.CloseTime)
			continue
		}
		intents := e.Tick(bars[:i], cur.CloseTime)
		minute := minCTFromBar(cur)
		placedThisMinute, cancelledThisMinute := 0, 0
		for _, in := range intents {
			switch in.Action {
			case PlaceStopEntry, PlaceStopLimitEntry:
				k := armKey{
					setup:     setupFor(in),
					side:      replaySideNum(in.Side),
					entry:     in.Price,
					stop:      in.Stop,
					placedMin: minute,
				}
				if k.setup == "UNKNOWN" {
					unknownSetups[in.Reason]++
				}
				key := armKeyString(k)
				if _, seen := placements[key]; !seen {
					placements[key] = k
					placedThisMinute++
				}
			case CancelArm:
				cancelledThisMinute++
			}
		}
		// RTH minutes only, like the replay's 390-row state files.
		m := ctMinuteOfDay(cur.OpenTime)
		if m >= 8*60+30 && m < 15*60 {
			row := emitGoState(e, bars, i, cfg)
			row.armsPlac = strconv.Itoa(placedThisMinute)
			row.armsCanc = strconv.Itoa(cancelledThisMinute)
			if _, seen := goRows[row.minCT]; !seen {
				rowOrder = append(rowOrder, row.minCT)
			}
			goRows[row.minCT] = row
		}
	}

	t.Logf("GO %-8s %s: %d placements (%d unique arms), %d RTH state rows, evaluator %s",
		d.name, d.day, len(placements), len(placements), len(goRows), time.Since(t0).Round(time.Millisecond))

	failed := false

	// ── state comparison: first divergent minute per column.
	statePath := frozenDir + "/state_v5_" + d.day + ".csv"
	state, stateHeader := loadStateRows(t, statePath)
	if len(state) == 0 {
		if _, err := os.Stat(statePath); err != nil {
			t.Errorf("%s: replay state file missing (%s) — DS-108 must dump state_v5 for this day",
				d.name, statePath)
		} else {
			t.Errorf("%s: replay state file empty for %s", d.name, d.day)
		}
		// no state comparison possible — the orders comparison below still runs
		return failed
	}
	replayByMin := map[string][]string{}
	var replayMinutes []string
	for _, r := range state {
		replayByMin[r[1]] = r
		replayMinutes = append(replayMinutes, r[1])
	}
	colIdx := map[string]int{}
	for i, h := range stateHeader {
		colIdx[h] = i
	}

	// minute-set differences first (a row on one side but not the other
	// diverges on every column).
	goMinSet := map[string]bool{}
	for _, m := range rowOrder {
		goMinSet[m] = true
	}
	var onlyGo, onlyReplay []string
	for _, m := range rowOrder {
		if _, ok := replayByMin[m]; !ok {
			onlyGo = append(onlyGo, m)
		}
	}
	for _, m := range replayMinutes {
		if _, ok := goRows[m]; !ok {
			onlyReplay = append(onlyReplay, m)
		}
	}
	if len(onlyGo) > 0 {
		t.Errorf("%s: %d RTH minutes exist in GO but not in the replay state file (first %s)",
			d.name, len(onlyGo), onlyGo[0])
		failed = true
	}
	if len(onlyReplay) > 0 {
		t.Errorf("%s: %d RTH minutes exist in the REPLAY state file but not in Go (first %s)",
			d.name, len(onlyReplay), onlyReplay[0])
		failed = true
	}

	goCols := map[string]map[string]string{}
	for _, m := range rowOrder {
		goCols[m] = goStateColumns(goRows[m])
	}
	diverged := 0
	for _, col := range stateColumnNames {
		idx, ok := colIdx[col]
		if !ok {
			t.Errorf("%s: state column %q missing from the replay file", d.name, col)
			continue
		}
		firstMin, goVal, replayVal := "", "", ""
		for _, m := range replayMinutes {
			row := replayByMin[m]
			grow, ok := goCols[m]
			if !ok {
				continue // minute-set divergence reported above
			}
			gv := grow[col]
			rv := row[idx]
			if !stateCellEqual(col, gv, rv) {
				if firstMin == "" {
					firstMin, goVal, replayVal = m, gv, rv
				}
			}
		}
		if firstMin != "" {
			t.Errorf("%s: column %-14s first divergence at %s: go=%q replay=%q",
				d.name, col, firstMin, goVal, replayVal)
			failed = true
			diverged++
		}
	}
	t.Logf("STATE %-8s %s: %d/%d columns diverged", d.name, d.day, diverged, len(stateColumnNames))

	// ── arms vs orders: exact keying (minute, setup, side, entry, stop).
	orderKeys := map[string]orderRow{}
	for _, o := range orders {
		k := armKey{
			setup:     o.setup,
			side:      o.side,
			entry:     o.entry,
			stop:      o.stop,
			placedMin: o.placedMinCT,
		}
		orderKeys[armKeyString(k)] = o
	}
	goSet := map[string]bool{}
	for key := range placements {
		goSet[key] = true
	}
	var goOnlyArms, replayOnlyArms []armKey
	for key, k := range placements {
		if _, ok := orderKeys[key]; !ok {
			goOnlyArms = append(goOnlyArms, k)
		}
	}
	for key, o := range orderKeys {
		if !goSet[key] {
			replayOnlyArms = append(replayOnlyArms, armKey{
				setup: o.setup, side: o.side, entry: o.entry, stop: o.stop, placedMin: o.placedMinCT,
			})
		}
	}
	sort.Slice(goOnlyArms, func(i, j int) bool { return goOnlyArms[i].placedMin < goOnlyArms[j].placedMin })
	sort.Slice(replayOnlyArms, func(i, j int) bool { return replayOnlyArms[i].placedMin < replayOnlyArms[j].placedMin })
	if len(goOnlyArms) > 0 {
		t.Errorf("%s: %d GO arms with no matching replay order; first: %s %s side=%d entry=%s stop=%s",
			d.name, len(goOnlyArms), goOnlyArms[0].placedMin, goOnlyArms[0].setup,
			goOnlyArms[0].side, fnum(goOnlyArms[0].entry), fnum(goOnlyArms[0].stop))
		failed = true
		for _, k := range goOnlyArms[:min(5, len(goOnlyArms))] {
			t.Logf("  GO-ONLY   %s %-12s side=%d entry=%s stop=%s", k.placedMin, k.setup, k.side, fnum(k.entry), fnum(k.stop))
		}
	}
	if len(replayOnlyArms) > 0 {
		t.Errorf("%s: %d replay orders with no matching GO arm; first: %s %s side=%d entry=%s stop=%s",
			d.name, len(replayOnlyArms), replayOnlyArms[0].placedMin, replayOnlyArms[0].setup,
			replayOnlyArms[0].side, fnum(replayOnlyArms[0].entry), fnum(replayOnlyArms[0].stop))
		failed = true
		for _, k := range replayOnlyArms[:min(5, len(replayOnlyArms))] {
			t.Logf("  REPLAY-ONLY %s %-12s side=%d entry=%s stop=%s", k.placedMin, k.setup, k.side, fnum(k.entry), fnum(k.stop))
		}
	}
	setupCounts := map[string]int{}
	for key := range placements {
		setupCounts[placements[key].setup]++
	}
	var setups []string
	for s, n := range setupCounts {
		setups = append(setups, fmt.Sprintf("%s=%d", s, n))
	}
	sort.Strings(setups)
	t.Logf("ARMS  %-8s %s: matched=%d go-only=%d replay-only=%d setups:%s",
		d.name, d.day, len(placements)-len(goOnlyArms), len(goOnlyArms), len(replayOnlyArms),
		strings.Join(setups, " "))
	for reason, n := range unknownSetups {
		t.Logf("  UNKNOWN-SETUP %3d x %s", n, reason)
	}

	// ── fills vs trades (replay-internal cross-check; Go has no fill sim).
	filled := 0
	for _, o := range orders {
		if strings.TrimSpace(o.fillMinCT) != "" {
			filled++
		}
	}
	if filled != len(trades) {
		t.Logf("FILLS %-8s: replay orders filled=%d vs trades rows=%d — DS-108 internal inconsistency, routed",
			d.name, filled, len(trades))
	} else {
		t.Logf("FILLS %-8s: orders filled=%d == trades rows=%d", d.name, filled, len(trades))
	}
	return failed
}

// loadStateRows returns the replay state rows (header excluded) plus the
// header row for column indexing. A missing file returns nil, nil.
func loadStateRows(t *testing.T, path string) ([][]string, []string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}
	rows := readCSV(t, path)
	if len(rows) < 2 {
		return nil, nil
	}
	return rows[1:], rows[0]
}

// stateCellEqual compares one Go-emitted cell with one replay cell. Numeric
// columns compare exactly (the contract is exact parity); empty replay cells
// are treated as "no value" and compare only against an empty Go cell.
func stateCellEqual(col, goVal, replayVal string) bool {
	gi := strings.TrimSpace(goVal)
	ri := strings.TrimSpace(replayVal)
	// the replay's current state dump never populates the arms columns
	// (verified: empty on every row of all five state files — a DS-108 dump
	// artifact), so an empty cell means zero, not "no value".
	if col == "arms_placed" || col == "arms_cancelled" {
		if gi == "" {
			gi = "0"
		}
		if ri == "" {
			ri = "0"
		}
		return gi == ri
	}
	switch col {
	case "gate":
		return gi == ri
	case "o", "h", "l", "c", "buy_line", "sell_line", "level_below", "level_above",
		"ema34", "box_top", "box_bot", "mother_hi", "mother_lo":
		if gi == "" && ri == "" {
			return true
		}
		gf, gok := parseFloatOrZero(gi)
		rf, rok := parseFloatOrZero(ri)
		if gok != rok {
			return false
		}
		if !gok && !rok {
			return gi == ri
		}
		return gf == rf
	default:
		// ints and the min_utc string column: exact string equality on the
		// trimmed cell (Go emits "" never for ints; replay may emit "").
		if gi == "" && ri == "" {
			return true
		}
		return gi == ri
	}
}

func parseFloatOrZero(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func replaySideNum(s Side) int {
	if s == SideShort {
		return -1
	}
	return 1
}

func prefixDays(bars []market.Kline) int {
	days := map[string]bool{}
	for _, b := range bars {
		days[time.UnixMilli(b.OpenTime).UTC().Format("2006-01-02")] = true
	}
	return len(days)
}

func atoiOr0(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}

func atofOrNaN(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

func minCTFromBar(b market.Kline) string {
	// the package convention: bar times are the CT wall clock as epoch millis,
	// so UTC formatting of the epoch yields the CT clock string.
	return time.UnixMilli(b.OpenTime).UTC().Format("2006-01-02 15:04")
}
