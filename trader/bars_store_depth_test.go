package trader

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/censuswalk"
	"vl/kernel"
	"vl/market"
	"vl/store"
)

// ── D2 PINS (wave BARS HORIZON, 2026-09-09) ─────────────────────────────────
//
// THREE CALL SITES WERE UNREACHABLE BY CONSTRUCTION, NOT BY MARKET CONDITIONS.
// The ring ceiling is DefaultBarCacheMaxBars = 2500 (bar_cache.go:24):
//
//	trader/auto_trader_planner.go  1m × 12000 = 200.0 h  vs 41.7 h ceiling
//	trader/auto_trader_weekly.go   1m × 12000 = 200.0 h  vs 41.7 h ceiling
//	trader/auto_trader_planner.go  5m ×  3000 = 250.0 h  vs 208.3 h ceiling
//
// No market condition can satisfy any of them. THE STORE IS THE HORIZON; THE
// RING IS THE CACHE (owner ruling 2026-09-09) — measured 2026-09-09, the bars
// table holds MNQ 1m 20,043 rows back to 2026-08-19 CT (21 days).

func d2Bars(startCT time.Time, n int, step time.Duration) []market.Kline {
	out := make([]market.Kline, 0, n)
	for i := 0; i < n; i++ {
		ts := startCT.Add(time.Duration(i) * step)
		o := 29000 + float64(i)
		out = append(out, market.Kline{OpenTime: ts.UnixMilli(), Open: o, High: o + 2, Low: o - 2, Close: o + 1, Volume: 7})
	}
	return out
}

// PIN D2-A — THE STORE FILLS THE OLDER END, AND NEVER OVERWRITES A LIVE BAR.
func TestStoreDepthPrependsOlderAndNeverReplacesRing(t *testing.T) {
	now := time.Date(2026, 9, 9, 13, 18, 0, 0, kernel.CTLocation())
	ringStart := now.Add(-100 * time.Minute)
	ring := d2Bars(ringStart, 100, time.Minute)
	// The store holds 400 bars covering the SAME window and 300 minutes before
	// it — and its overlapping copies carry a different Close, so a merge that
	// preferred the store would be visible.
	stored := d2Bars(ringStart.Add(-300*time.Minute), 400, time.Minute)
	for i := range stored {
		stored[i].Close = -1
	}

	out := barsWithStoreDepthFrom(ring, func(int) ([]market.Kline, error) { return stored, nil },
		"MNQ", "1m", 400, now)

	if len(out) != 400 {
		t.Fatalf("served %d bars, want 400 (300 from the store + 100 from the ring)", len(out))
	}
	for i := 1; i < len(out); i++ {
		if out[i].OpenTime <= out[i-1].OpenTime {
			t.Fatalf("result is not strictly ascending at %d (%d <= %d)", i, out[i].OpenTime, out[i-1].OpenTime)
		}
	}
	// Every RING bar must survive byte-identically — the ring is the live tape.
	tail := out[len(out)-len(ring):]
	for i := range ring {
		if tail[i] != ring[i] {
			t.Fatalf("ring bar %d was replaced by a stored one: got %+v want %+v", i, tail[i], ring[i])
		}
	}
	if out[0].OpenTime != stored[0].OpenTime {
		t.Fatalf("oldest served bar is %d, want the store's oldest %d", out[0].OpenTime, stored[0].OpenTime)
	}
}

// PIN D2-B — AN EMPTY RING IS NOT SUBSTITUTED BY THE STORE.
//
// The store is a DEPTH source, never a stand-in for a live feed. If the ring is
// empty the feed is down or the seed has not landed, and handing the planner
// 12,000 stale bars would let it write a plan on a dead tape — the exact
// failure "no NT8 → no decisions" exists to prevent.
func TestEmptyRingIsNeverSubstitutedByTheStore(t *testing.T) {
	now := time.Date(2026, 9, 9, 13, 18, 0, 0, kernel.CTLocation())
	stored := d2Bars(now.Add(-500*time.Minute), 500, time.Minute)
	read := 0
	out := barsWithStoreDepthFrom(nil, func(int) ([]market.Kline, error) { read++; return stored, nil },
		"MNQ", "1m", 500, now)
	if len(out) != 0 {
		t.Fatalf("an EMPTY ring was backfilled from the store with %d bars — the store must never substitute for the live feed", len(out))
	}
	if read != 0 {
		t.Fatalf("the store was read %d time(s) for an empty ring", read)
	}
}

// PIN D2-C — A RING THAT ALREADY SERVES THE ASK IS NOT SECOND-GUESSED.
func TestSatisfiedRingSkipsTheStoreEntirely(t *testing.T) {
	now := time.Date(2026, 9, 9, 13, 18, 0, 0, kernel.CTLocation())
	ring := d2Bars(now.Add(-200*time.Minute), 200, time.Minute)
	read := 0
	out := barsWithStoreDepthFrom(ring, func(int) ([]market.Kline, error) { read++; return nil, nil },
		"MNQ", "1m", 200, now)
	if read != 0 {
		t.Fatalf("the store was read %d time(s) although the ring already served 200 of 200", read)
	}
	if len(out) != len(ring) {
		t.Fatalf("served %d, want the ring's %d unchanged", len(out), len(ring))
	}
}

// PIN D2-D — A STORE READ THAT FAILS WARNS AND DEGRADES TO THE RING (A10).
func TestStoreReadFailureFallsBackToTheRing(t *testing.T) {
	now := time.Date(2026, 9, 9, 13, 18, 0, 0, kernel.CTLocation())
	ring := d2Bars(now.Add(-100*time.Minute), 100, time.Minute)
	out := barsWithStoreDepthFrom(ring, func(int) ([]market.Kline, error) { return nil, errors.New("db locked") },
		"MNQ", "1m", 400, now)
	if len(out) != len(ring) {
		t.Fatalf("a failed store read served %d bars, want the ring's %d — it must never blank or block (A10)", len(out), len(ring))
	}
}

// PIN D2-E — THE RV BASELINE STOPS PROMISING 20 DAYS IT WAS NEVER FED.
//
// WHAT THIS PIN IS, AND WHAT IT IS NOT (relabeled in review, 2026-09-09). It is
// the ESTIMATOR PARITY anchor: it proves RVBaselineFrom5mDays returns the same
// VALUE as the pre-wave RVBaselineFrom5m for the same slice, and that the day
// count it reports is the count it was FED and not the count it was ASKED.
// It builds BOTH sides' inputs itself, so BY CONSTRUCTION it cannot see the
// production INPUT move — that is the class-53 trap, and it is not a defect
// here as long as nobody cites this pin as evidence that the rendered value is
// unchanged. It was cited that way, and the claim was false. The CALL-SITE
// golden is TestRegimeLabelUnchangedWhenTheWindowDoesNotChange
// (trader/regime_input_window_test.go), owner condition (d).
//
// RVBaselineFrom5m(min5Long, 20, 5) filled the struct field RVBaseline20d from
// about 7 complete session-days. The value is honest internally (incomplete
// days are dropped, and it returns ok=false below minDays) but nothing carried
// the actual day count to the reader, and the ask above it — 5m × 3000 = 250.0
// h against a 208.3 h ring — could never have delivered 20 days anyway.
func TestRVBaselineCarriesItsActualDayCount(t *testing.T) {
	// Seven complete session-days of 5m bars.
	var min5 []market.Kline
	start := time.Date(2026, 8, 31, 17, 0, 0, 0, kernel.CTLocation())
	for d := 0; d < 7; d++ {
		day := start.AddDate(0, 0, d)
		for i := 0; i < 280; i++ {
			ts := day.Add(time.Duration(i) * 5 * time.Minute)
			o := 29000 + float64(i%20)
			min5 = append(min5, market.Kline{OpenTime: ts.UnixMilli(), Open: o, High: o + 3, Low: o - 3, Close: o + 1, Volume: 5})
		}
	}
	base, days, ok := kernel.RVBaselineFrom5mDays(min5, 20, 5)
	if !ok {
		t.Fatalf("baseline not computed from 7 complete days")
	}
	// EXACT, not a range. The first version asserted 1..20 and a mutation that
	// returned maxDays (20 — the ASK) instead of len(perDay) passed green: the
	// wave's entire point is that the fed count and the asked count DIFFER, so
	// a pin that tolerates the ask cannot see the defect (class 89).
	if days != 7 {
		t.Fatalf("complete-day count = %d, want exactly 7 (the fixture builds 7 complete session-days)", days)
	}
	if days >= 20 {
		t.Fatalf("the reported count (%d) equals the ASK — it must report what was FED", days)
	}
	// The value itself must be byte-identical to the pre-wave helper.
	old, oldOK := kernel.RVBaselineFrom5m(min5, 20, 5)
	if !oldOK || old != base {
		t.Fatalf("RVBaselineFrom5mDays changed the COMPUTED value: %v vs %v — D2 may only move the input or the name", base, old)
	}
	// The regime the model reads must NAME the window it was actually fed.
	r := kernel.ComputeRegime(kernel.RegimeInputs{
		Price: 29000, Min5Bars: min5, RVBaseline: base, RVBaselineDays: days,
	})
	if !strings.Contains(r.Render(), fmt.Sprintf("%d complete session-days", days)) {
		t.Errorf("the regime line does not name the baseline's real window (%d days): %s", days, r.Render())
	}
	if strings.Contains(r.Render(), "of-normal") {
		t.Errorf("the regime line still says \"of-normal\" — a baseline with no stated window: %s", r.Render())
	}
}

// PIN D2-F — A29. The new depth path and the day-count helper are WIRED.
func TestD2WiredAtTheThreeCallSites(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	for fn, wantIn := range map[string]string{
		"barsWithStoreDepth(":     "trader/auto_trader_planner.go",
		"barsWithStoreDepthFrom(": "trader/bars_store_depth.go",
		// ROLL WAVE — the depth path reads the CONTRACT-FILTERED form. The bare
		// LastNBars( no longer appears here (and the reader-filter lint fails
		// if it ever does); the wiring this pins is that the store read exists
		// on the depth path at all.
		"LastNBarsOn(":                "trader/bars_store_depth.go",
		"RVBaselineFrom5mDays(":       "trader/regime_input_window.go",
		"rvBaselineFallback5mBarsAsk": "trader/auto_trader_planner.go",
		"at.barsWithStoreDepth(at.fu": "trader/auto_trader_weekly.go",
	} {
		n, where := d2ProdCallSites(t, root, fn)
		if n == 0 {
			t.Errorf("%s: 0 production call sites (A29)", fn)
			continue
		}
		if !strings.Contains(strings.Join(where, " "), wantIn) {
			t.Errorf("%s: production call sites %v, want one in %s", fn, where, wantIn)
		}
	}
	// The three unreachable asks are GONE.
	for _, gone := range []string{`"1m", 12000`, `"5m", 3000`} {
		n, where := d2ProdCallSites(t, root, gone)
		if n > 0 {
			t.Errorf("an ask above the ring ceiling survives: %s in %v", gone, where)
		}
	}
}

// PIN D2-G — THE SPLICE NEVER SERVES MORE THAN IT WAS ASKED FOR.
//
// THE DEFECT THIS CATCHES (found by a reviewer's mutation, 2026-09-09): the
// tail cap at the end of barsWithStoreDepthFrom could be deleted and the whole
// trader package stayed green, because D2-A's fixture picks an n large enough
// that the cap never engages. Without it `older` (bounded by n) plus the ring
// can reach 2n, and the TAPE line one layer up would then read "14000 1m bars
// HELD of 12000 requested" — a disclosure that contradicts itself, which is the
// exact class this wave exists to remove.
func TestStoreDepthNeverServesMoreThanAsked(t *testing.T) {
	now := time.Date(2026, 9, 9, 13, 18, 0, 0, kernel.CTLocation())
	ringStart := now.Add(-100 * time.Minute)
	ring := d2Bars(ringStart, 100, time.Minute)                         // 100 live bars
	stored := d2Bars(ringStart.Add(-300*time.Minute), 300, time.Minute) // 300 older bars
	const n = 250                                                       // < 300 + 100

	out := barsWithStoreDepthFrom(ring, func(int) ([]market.Kline, error) { return stored, nil },
		"MNQ", "1m", n, now)

	if len(out) != n {
		t.Fatalf("served %d bars for an ask of %d — the splice over-serves", len(out), n)
	}
	// And what survives must be the NEWEST n, live tail included.
	if out[len(out)-1].OpenTime != ring[len(ring)-1].OpenTime {
		t.Fatalf("the live tail was trimmed: newest served %d, newest ring %d", out[len(out)-1].OpenTime, ring[len(ring)-1].OpenTime)
	}
	wantOldest := ring[len(ring)-1].OpenTime - int64(n-1)*60_000
	if out[0].OpenTime != wantOldest {
		t.Fatalf("the trim kept the wrong end: oldest served %d, want %d (the newest %d bars)", out[0].OpenTime, wantOldest, n)
	}
}

// F1 (2026-09-14) — the API/dashboard seam must degrade to the ring whenever it
// cannot answer: nil store, or no contract named, or a store with nothing to
// add. The splice itself is pinned by D2-A..D2-G; these guard the NEW surface.
func TestBarsWithStoreDepthSeamDegradesWhenUnanswerable(t *testing.T) {
	now := time.Date(2026, 9, 14, 10, 0, 0, 0, kernel.CTLocation())
	ring := d2Bars(now.Add(-100*time.Minute), 100, time.Minute)

	if out := BarsWithStoreDepth(ring, nil, "MNQ 12-26", "MNQ", "1m", 200, now); len(out) != 100 {
		t.Fatalf("a nil store must serve the ring alone: %d bars", len(out))
	}

	st, err := store.New(filepath.Join(t.TempDir(), "seam.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if err := st.BarHistory().Migrate(); err != nil {
		t.Fatal(err)
	}
	// A real store but NO contract named — UNKNOWN is not "everything" (A24).
	if out := BarsWithStoreDepth(ring, st, "   ", "MNQ", "1m", 200, now); len(out) != 100 {
		t.Fatalf("an unnamed contract must serve the ring alone: %d bars", len(out))
	}
	// A real store + a named contract, but the table has no bars — ring alone.
	if out := BarsWithStoreDepth(ring, st, "MNQ 12-26", "MNQ", "1m", 200, now); len(out) != 100 {
		t.Fatalf("an empty store must serve the ring alone: %d bars", len(out))
	}
}

func d2ProdCallSites(t *testing.T, root, needle string) (int, []string) {
	t.Helper()
	n := 0
	var where []string
	files, werr := censuswalk.NonTestGoFiles(root)
	if werr != nil {
		t.Fatalf("census walk: %v", werr)
	}
	for _, cf := range files {
		b, rerr := os.ReadFile(cf.Path)
		if rerr != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, needle) && !strings.HasPrefix(strings.TrimSpace(line), "//") {
				if strings.HasPrefix(strings.TrimSpace(line), "func ") {
					continue
				}
				n++
				where = append(where, cf.Rel)
				break
			}
		}
	}
	return n, where
}

// THE PLANNER DOOR IS NT8-ONLY (CTO ruling under the owner's delegation,
// 2026-09-16). storeBarReader is the ONE reader behind barsWithStoreDepth —
// the planner's 12,000-bar 1m tape and the weekly reader's. Measured that day
// on data/data.db: MNQ 12-26 1m held 2,873 live + 25 replay + 426
// historical_import rows, all inside the newest 12,000 the shared reader hands
// out, so 426 imported bars were in the planner's tape. Real store, imports
// NEWER than the live rows (they would be the first rows served), the ring
// empty of them: the reader must serve zero.
func TestPlannerStoreReaderServesNoImportRows(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "tape.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	const oneMin = int64(60_000)
	base := int64(1_789_000_000_000)
	var live, imports []store.BarHistoryDB
	for i := 0; i < 500; i++ {
		live = append(live, store.BarHistoryDB{Symbol: "MNQ", TF: "1m", OpenTimeMs: base + int64(i)*oneMin, O: 29290, H: 29300, L: 29280, C: 29295, V: 1, Contract: "MNQ 12-26", Source: store.BarSourceLive})
	}
	for i := 0; i < 5; i++ {
		imports = append(imports, store.BarHistoryDB{Symbol: "MNQ", TF: "1m", OpenTimeMs: base + int64(600+i)*oneMin, O: 29290, H: 29300, L: 29280, C: 29295, V: 1, Contract: "MNQ 12-26", Source: store.BarSourceHistoricalImport})
	}
	if err := bh.InsertBars(live); err != nil {
		t.Fatal(err)
	}
	if ins, skip, err := bh.ImportBars(imports); err != nil || ins != 5 || skip != 0 {
		t.Fatalf("fixture imports inserted=%d skipped=%d err=%v, want 5/0 (class 128)", ins, skip, err)
	}
	at := &AutoTrader{store: st}
	read := at.storeBarReader("MNQ", "1m")
	got, err := read(12000)
	if err != nil {
		t.Fatalf("storeBarReader: %v", err)
	}
	if len(got) != 500 {
		t.Fatalf("planner reader served %d rows, want 500 (the 5 imports excluded)", len(got))
	}
	for _, k := range got {
		if k.OpenTime >= base+600*oneMin {
			t.Fatalf("an import row (%d) reached the planner tape", k.OpenTime)
		}
	}
	// the exported seam that documents itself as "the same splice the planner uses"
	out := BarsWithStoreDepth(nil, st, "MNQ 12-26", "MNQ", "1m", 12000, time.UnixMilli(base+700*oneMin))
	_ = out // an EMPTY ring is never backfilled — the seam's own pin; the reader behind it is what this test names
}

// TestD2CensusSeesNestedSkipNamedDirs plants the d2 needle in EVERY
// censuswalk.NestedProbeDirs directory of a synthetic module and asserts
// d2ProdCallSites reports every one. With the old any-depth SkipDir the dirs
// named like a root skip were invisible (CLASS 258).
func TestD2CensusSeesNestedSkipNamedDirs(t *testing.T) {
	root := t.TempDir()
	dirs := censuswalk.NestedProbeDirs()
	for _, dir := range dirs {
		full := filepath.Join(root, filepath.FromSlash(dir))
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
		src := "package " + censuswalk.PackageName(dir) + "\n\nfunc offender() {\n\t_ = barsWithStoreDepthFrom(0, 0)\n}\n"
		if err := os.WriteFile(filepath.Join(full, "offender.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, where := d2ProdCallSites(t, root, "barsWithStoreDepthFrom(")
	seen := map[string]bool{}
	for _, w := range where {
		seen[filepath.ToSlash(w)] = true
	}
	var missed []string
	for _, dir := range dirs {
		if !seen[dir+"/offender.go"] {
			missed = append(missed, dir)
		}
	}
	if len(missed) > 0 {
		t.Fatalf("the d2 census skipped %d of %d nested probe dirs (saw %d sites) — a skip by NAME at depth exempts compiled packages (CLASS 258):\n\t%s",
			len(missed), len(dirs), n, strings.Join(missed, "\n\t"))
	}
}

// PIN m326 — the served 4h series must never carry the NT8-native 16:00-close
// stub. A legacy row stored at 12:00 CT (the 4h 13:00 bucket one hour early, by
// a pre-m220/m134 binary) re-anchors +1h on READ, and when the corrected 13:00
// row is ALSO present the two collapse to ONE bar. MUTANT: drop the re-anchor
// (ot = r.OpenTimeMs in storeRowsToKlines) → the 12:00 bar is served alongside
// the 13:00 → RED.
func TestStoreRowsToKlinesReanchorsTheLegacy4hStub(t *testing.T) {
	loc := kernel.CTLocation()
	ms := func(y int, mo time.Month, d, h, m int) int64 {
		return time.Date(y, mo, d, h, m, 0, 0, loc).UnixMilli()
	}
	// Same trade date: the legacy stub (12:00) and the corrected bucket (13:00)
	// with IDENTICAL OHLC (the stub is an exact copy of the 13:00 bucket), plus
	// the neighbours 09:00 and 17:00.
	rows := []store.BarHistoryDB{
		{Symbol: "MNQ", TF: "4h", Contract: "MNQ 12-26", OpenTimeMs: ms(2026, 10, 20, 9, 0), O: 100, H: 102, L: 98, C: 101, Source: store.BarSourceHistorical},
		{Symbol: "MNQ", TF: "4h", Contract: "MNQ 12-26", OpenTimeMs: ms(2026, 10, 20, 12, 0), O: 101, H: 105, L: 100, C: 104, Source: store.BarSourceLive},
		{Symbol: "MNQ", TF: "4h", Contract: "MNQ 12-26", OpenTimeMs: ms(2026, 10, 20, 13, 0), O: 101, H: 105, L: 100, C: 104, Source: store.BarSourceLive},
		{Symbol: "MNQ", TF: "4h", Contract: "MNQ 12-26", OpenTimeMs: ms(2026, 10, 20, 17, 0), O: 104, H: 106, L: 103, C: 105, Source: store.BarSourceLive},
	}
	got := storeRowsToKlines(rows, "4h")
	wantHours := []int{9, 13, 17}
	if len(got) != len(wantHours) {
		t.Fatalf("served %d bars, want %d (the 12:00 stub must collapse into 13:00): %+v", len(got), len(wantHours), got)
	}
	for i, k := range got {
		if h := time.UnixMilli(k.OpenTime).In(loc).Hour(); h != wantHours[i] {
			t.Fatalf("bar %d opens at %s CT, want hour %d — the served series must be 17,21,01,05,09,13 only",
				i, time.UnixMilli(k.OpenTime).In(loc).Format("15:04"), wantHours[i])
		}
	}
	// The corrected 13:00 OHLC survives the collapse (it is the later stamp).
	if got[1].Open != 101 || got[1].Close != 104 {
		t.Fatalf("the 13:00 bucket must keep the corrected OHLC, got %+v", got[1])
	}
}

// PIN m326 (legacy-only form) — a store that holds ONLY the legacy stub (no
// corrected partner, the pre-fix old-contract case) still serves 13:00, never
// 12:00.
func TestStoreRowsToKlinesReanchorsALoneLegacyStub(t *testing.T) {
	loc := kernel.CTLocation()
	ms := func(h, m int) int64 { return time.Date(2026, 10, 20, h, m, 0, 0, loc).UnixMilli() }
	rows := []store.BarHistoryDB{
		{Symbol: "MNQ", TF: "4h", Contract: "MNQ 09-26", OpenTimeMs: ms(12, 0), O: 101, H: 105, L: 100, C: 104, Source: store.BarSourceLive},
	}
	got := storeRowsToKlines(rows, "4h")
	if len(got) != 1 {
		t.Fatalf("served %d bars, want 1", len(got))
	}
	if h := time.UnixMilli(got[0].OpenTime).In(loc).Hour(); h != 13 {
		t.Fatalf("a lone legacy stub served at %s CT, want 13:00", time.UnixMilli(got[0].OpenTime).In(loc).Format("15:04"))
	}
	if got[0].OpenTime != ms(13, 0) {
		t.Fatalf("re-anchored open = %d, want %d (13:00 CT)", got[0].OpenTime, ms(13, 0))
	}
}
