package ninjatrader

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/censuswalk"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// PIN 5 — the warn is DEDUPED and every detection is COUNTED.
//
// Why a dedupe at all, by measurement: the planner cadence on 2026-09-09 was
// 32m/10m/37m between reads (planner_read_facts ids 64->65->66->67), the monitor
// tick is 60s, and 57 consumer call sites hit this bridge per cycle. The
// 2026-09-09 log file is 511 MB. An undeduped warn writes thousands of lines.
//
// Why the counter is OUTSIDE the dedupe: counters record, never infer (class
// 35). A dedupe that hides 699 of 700 detections must not also hide the rate.
func TestShortReadWarnIsDedupedAndCounted(t *testing.T) {
	resetBarHorizonWarnsForSessionDay(0)
	get := bhWarnCapture(t)
	cache := ntwire.NewBarCache(2500)
	bhSeed(cache, "MNQ", "1m", bhCT(2026, time.September, 8, 9, 0), 1000)

	// ONE call site for every read below: resolveBarHorizonCaller names the
	// frame, and the frame is part of the dedupe key — two different lines in
	// this test would be two different keys and nothing would ever suppress.
	read := func(at time.Time) { barsFromCache(cache, "MNQ", "1m", 2500, at) } // m452: inside the ring, so a SHORT is a real one

	base := telemetry.BarHorizonCounts()["short"]
	// The live arm-133 instant, to the second, so the "since" below is a
	// RESOLVED clock rather than a literal.
	// 101 D3(b) (2026-09-16): the window is FIVE minutes, pinned as a policy
	// value here rather than derived from the constant (class 93). 280 reads
	// one second apart = 4m39s, inside one window.
	if barHorizonWarnWindow != 5*time.Minute {
		t.Fatalf("barHorizonWarnWindow = %s, want 5m (101 D3(b))", barHorizonWarnWindow)
	}
	start := bhCT(2026, time.September, 9, 13, 18).Add(13 * time.Second)
	for i := 0; i < 280; i++ {
		read(start.Add(time.Duration(i) * time.Second))
	}
	if n := bhCountLines(bhFilter(get(), "bar_horizon_warn_test.go"), "bar horizon"); n != 1 {
		t.Fatalf("%d emitted lines inside the five-minute window, want 1", n)
	}
	if got := telemetry.BarHorizonCounts()["short"] - base; got != 280 {
		t.Fatalf("BarHorizonCounts()[\"short\"] rose by %d, want 280 — every DETECTION is counted, not only the emitted one", got)
	}

	// Past the re-arm window the key speaks again, and it says how many it ate.
	read(start.Add(6 * time.Minute))
	lines := bhFilter(get(), "bar_horizon_warn_test.go")
	if n := bhCountLines(lines, "bar horizon"); n != 2 {
		t.Fatalf("after the window re-armed: %d emitted lines, want 2: %v", n, lines)
	}
	if bhCountLines(lines, "suppressed=279 since ") != 1 {
		t.Fatalf("the re-armed line must name what it suppressed and since when: %v", lines)
	}
	if bhCountLines(lines, "suppressed=279 since 13:18:13 CT") != 1 {
		t.Fatalf("the 'since' must be a RESOLVED clock, not a literal or a duration: %v", lines)
	}

	// Build up a fresh suppression run, then cross into the next CME session
	// day. The rollover must CLEAR the state, so the next line carries no
	// "suppressed=" at all — without the rollover the window path would emit
	// the same line WITH suppressed=5, which is what this discriminates.
	for i := 1; i <= 5; i++ { // inside the window the 6-minute read opened
		read(start.Add(6*time.Minute + time.Duration(i)*time.Second))
	}
	read(start.Add(24 * time.Hour))
	lines = bhFilter(get(), "bar_horizon_warn_test.go")
	if n := bhCountLines(lines, "bar horizon"); n != 3 {
		t.Fatalf("after the session-day rollover: %d emitted lines, want 3: %v", n, lines)
	}
	if n := bhCountRe(lines, suppressedFiveRe); n != 0 {
		t.Fatalf("the CME session-day rollover did not clear the suppression state: %v", lines)
	}
}

// PIN 5b — the EMPTY arm is GRACED across the boot window.
//
// Measured on 2026-09-09: PID 438 started 13:02:25 CT and the first non-zero
// backfill logged at 13:06:48 CT — a ~4.5 minute window in which every one of
// the 57 consumer sites is served 0 bars. Without a grace that is ~40 EMPTY
// warns at every boot. The grace lifts on the backfill OR on a timer, so a TCP
// server that never comes up still gets one loud line (pin the FALLBACK, not
// only the hook).
func TestEmptyArmIsGracedUntilBackfillOrTimeout(t *testing.T) {
	resetBarHorizonWarnsForSessionDay(0)
	get := bhWarnCapture(t)
	cache := ntwire.NewBarCache(2500) // seeded with NOTHING
	boot := bhCT(2026, time.September, 9, 13, 2)
	armBarHorizonGrace(boot)

	barsFromCache(cache, "MNQ", "1m", 2000, boot.Add(1*time.Minute))
	if n := bhCountLines(bhFilter(get(), "bar_horizon_warn_test.go"), "bar horizon"); n != 0 {
		t.Fatalf("EMPTY warned %d times inside the boot grace, want 0: %v", n, bhFilter(get(), "bar_horizon_warn_test.go"))
	}

	// The fallback timer, with the hook never firing.
	barsFromCache(cache, "MNQ", "1m", 2000, boot.Add(barHorizonBootGrace+time.Minute))
	lines := bhFilter(get(), "bar_horizon_warn_test.go")
	if bhCountLines(lines, "bar horizon", "EMPTY") != 1 {
		t.Fatalf("after the grace fallback: want 1 EMPTY warn, got %v", lines)
	}
	if bhCountLines(lines, "served=0") != 1 || bhCountLines(lines, "gaps=UNKNOWN") != 1 {
		t.Fatalf("the EMPTY line must say served=0 and gaps=UNKNOWN (never a plausible zero, A24): %v", lines)
	}

	// And the hook path: a fresh grace, lifted by the backfill landing.
	resetBarHorizonWarnsForSessionDay(0)
	get2 := bhWarnCapture(t)
	armBarHorizonGrace(boot)
	noteBarHorizonBackfillLanded()
	barsFromCache(cache, "MNQ", "1m", 2000, boot.Add(1*time.Minute))
	if bhCountLines(get2(), "bar horizon", "EMPTY") != 1 {
		t.Fatalf("after the backfill landed the EMPTY arm must speak immediately: %v", get2())
	}
}

// PIN 5c — A29. Every new function has a production call site, named.
func TestBarHorizonHasProductionCallSites(t *testing.T) {
	for fn, wantIn := range map[string]string{
		"barsFromCache(":                     "trader/ninjatrader/bars_market_bridge.go",
		"barHorizonWarn(":                    "trader/ninjatrader/bars_market_bridge.go",
		"kernel.HorizonOf(":                  "trader/ninjatrader/bars_market_bridge.go",
		"armBarHorizonGrace(":                "trader/ninjatrader/bars_market_bridge.go",
		"noteBarHorizonBackfillLanded(":      "trader/ninjatrader/bar_persist_wire.go",
		"barHorizonBootLine(":                "trader/ninjatrader/bar_persist_wire.go",
		"telemetry.IncBarHorizon(":           "trader/ninjatrader/bar_horizon_warn.go",
		"resetBarHorizonWarnsForSessionDay(": "trader/ninjatrader/bar_horizon_warn.go",
		"cache.MaxBars()":                    "trader/ninjatrader/bars_market_bridge.go",
		// Added in review 2026-09-09: the counter had FIVE arms and no reader.
		"telemetry.BarHorizonCounts(": "trader/ninjatrader/bar_horizon_warn.go",
		"barHorizonCountsTxt(":        "trader/ninjatrader/bar_horizon_warn.go",
	} {
		n, where := prodCallSites(t, repoRoot(t), fn)
		if n == 0 {
			t.Fatalf("%s: 0 production call sites — a new function with no caller is not shipped (A29)", fn)
		}
		if !strings.Contains(strings.Join(where, " "), wantIn) {
			t.Fatalf("%s: production call sites %v, want one in %s", fn, where, wantIn)
		}
	}
}

// prodCallSites counts NON-TEST .go files under the repo root that contain
// `needle`, and returns their repo-relative paths. Precedent for source-scanning
// wiring pins in this repo: kernel/wiring_riskforceflat_pin_test.go,
// trader/wiring_gate_test.go, researchsnapshot/wiring_test.go.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return root
}

func prodCallSites(t *testing.T, root, needle string) (int, []string) {
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

// PIN D3-E — A29. The boot rehydrate is WIRED, and it uses the ring-safe door.
func TestRingRehydrateIsWiredAtBoot(t *testing.T) {
	for fn, wantIn := range map[string]string{
		"rehydrateRingFromStore(": "trader/ninjatrader/bar_persist_wire.go",
		"cache.RehydrateOlder(":   "trader/ninjatrader/bar_persist_wire.go",
		"bh.LastNBars(":           "trader/ninjatrader/bar_persist_wire.go",
	} {
		n, where := prodCallSites(t, repoRoot(t), fn)
		if n == 0 {
			t.Fatalf("%s: 0 production call sites — a new function with no caller is not shipped (A29)", fn)
		}
		if !strings.Contains(strings.Join(where, " "), wantIn) {
			t.Fatalf("%s: production call sites %v, want one in %s", fn, where, wantIn)
		}
	}
	// The rehydrate must NEVER reach for SeedHistorical: that door seeds a cold
	// key and lets the INCOMING (here: the store) win an overlap, which is
	// exactly what D3 must not do.
	b, err := os.ReadFile("bar_persist_wire.go")
	if err != nil {
		t.Fatalf("read bar_persist_wire.go: %v", err)
	}
	if strings.Contains(string(b), "SeedHistorical(") {
		t.Fatalf("bar_persist_wire.go calls SeedHistorical — the store must enter through RehydrateOlder, which refuses a cold key and never outranks a live bar")
	}
}

// PIN D3-L — OWNER CONDITION (a), 2026-09-09. THE BOOT REHYDRATE TOUCHES 1m
// AND NOTHING ELSE.
//
// THE DEFECT THIS CAUGHT (class 86, 2026-09-09): the first cut rehydrated EVERY
// (symbol, timeframe) pair and silently moved a LIVE regime input using NT8
// aggregates this repo had judged inconsistent with their own 1m constituents.
// The owner's 09-09 ruling required the depth to come from the 1m tape.
//
// RE-POINTED 2026-09-16 under the owner's direct ruling for dispatch 101 —
// "i want fuull data" — which supersedes condition (a): EVERY subscribed
// timeframe is rehydrated from the store's current-contract rows, with four
// guards, and the regime input is protected by condition (b) instead (the
// baseline is served from the 1m tail; the 5m ring is only its fallback —
// TestRegimeLabelUnchanged pins that it did not move). The pin keeps its name,
// its A29 check and its owner; what it asserts is the new selection.
// (was TestRehydrateSelectsOnly1mPairs — the class-86 pin; renamed to what it
// asserts after the [O] "i want fuull data" ruling of 2026-09-16, never deleted)
func TestRehydrateSelectsEveryPair(t *testing.T) {
	in := [][2]string{
		{"MNQ", "1m"}, {"MNQ", "5m"}, {"MNQ", "15m"}, {"MNQ", "1h"},
		{"MNQ", "4h"}, {"MNQ", "1d"}, {"MNQ", "1w"}, {"ES", "1m"}, {"ES", "5m"},
	}
	got := pairsToRehydrate(in)
	if len(got) != len(in) {
		t.Fatalf("selected %d pairs from %d, want EVERY subscribed timeframe [O 2026-09-16]: %v", len(got), len(in), got)
	}
	for i := range in {
		if got[i] != in[i] {
			t.Fatalf("the cache's order was not preserved at %d: %v", i, got)
		}
	}
	// A29 — the production loop consults it.
	if n, where := prodCallSites(t, repoRoot(t), "pairsToRehydrate("); n == 0 {
		t.Fatalf("pairsToRehydrate has 0 production call sites (A29) — the filter can be bypassed with the suite green (%v)", where)
	}
}

// GUARD (ii) — RING-SIDE, from vl-93's census: a store row is REPLAY-GRADE to
// this process whatever its stamp says. The store carries 09-26 rows stamped
// `live` by the migration and 12-26 rows stamped `live` by a closed-bar
// catch-up delivered as bar_update after a subscribe (1d rows from 09-02 at
// rowid 438391). Age alone distinguishes neither. So EVERY rehydrated row
// enters the ring as historical, never as sacred live — and the scale check,
// the merge and the persister all treat it as the replay it is.
func TestRehydratedRowsEnterTheRingAsHistorical(t *testing.T) {
	rows := []store.BarHistoryDB{
		{OpenTimeMs: 1_000_000, O: 1, H: 2, L: 0.5, C: 1.5, V: 1, Source: store.BarSourceLive},
		{OpenTimeMs: 1_060_000, O: 1, H: 2, L: 0.5, C: 1.5, V: 1, Source: store.BarSourceHistorical},
	}
	bars := rehydrateBarsFromRows(rows)
	for i, b := range bars {
		if b.Source != ntwire.BarSourceHistorical {
			t.Fatalf("row %d entered the ring as %q; a store row is replay-grade to this process (guard ii)", i, b.Source)
		}
	}
}

// GUARD (i) — after a confirmed scale-break drop, rows stamped `historical`
// are the rejected seed's kin and are excluded from that key's refill; on the
// boot path they were verified in a prior boot and are kept.
func TestPostDropRefillExcludesHistoricalRows(t *testing.T) {
	rows := []store.BarHistoryDB{
		{OpenTimeMs: 1_000_000, Source: store.BarSourceLive},
		{OpenTimeMs: 1_060_000, Source: store.BarSourceHistorical},
		{OpenTimeMs: 1_120_000, Source: store.BarSourceLive},
	}
	if got, imp := rehydrateRowsFor(rows, true); len(got) != 2 || imp != 0 {
		t.Fatalf("post-drop: want the 2 live rows only (0 imports), got %d (%d)", len(got), imp)
	}
	if got, imp := rehydrateRowsFor(rows, false); len(got) != 3 || imp != 0 {
		t.Fatalf("boot path: want all 3 rows (0 imports), got %d (%d)", len(got), imp)
	}
}

// GUARD (iii) IS REAL CODE, AT THE DOOR, ON BOTH PATHS — vl-93 objection 1
// (2026-09-16). The reader LastNBarsOn hands imports to its callers (its filter
// is mixed+off-scale only — pinned in store TestCurrentContractReaderReturns
// ImportsUnfiltered); the first cut of this wave printed
// "import=excluded-by-reader" on a boot line without a line of code behind it
// (class 82). End to end on a real store: 500 live + 5 import rows on 12-26 at
// NON-colliding open times (the bars PK has no contract; an import on an
// occupied slot is skipped) → the reader returns 505 → the door admits 500 and
// COUNTS the 5, so the boot line prints a number it read.
func TestRehydrateDoorExcludesImportsOnBothPaths(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "door.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	const fiveMin = int64(5 * 60 * 1000)
	base := int64(1_789_000_000_000)
	var imports, live []store.BarHistoryDB
	for i := 0; i < 5; i++ { // older than every live row: no slot collides
		imports = append(imports, store.BarHistoryDB{Symbol: "MNQ", TF: "5m", OpenTimeMs: base + int64(i)*fiveMin, O: 29290, H: 29300, L: 29280, C: 29295, V: 1, Contract: "MNQ 12-26", Source: store.BarSourceHistoricalImport})
	}
	for i := 0; i < 500; i++ {
		live = append(live, store.BarHistoryDB{Symbol: "MNQ", TF: "5m", OpenTimeMs: base + int64(100+i)*fiveMin, O: 29290, H: 29300, L: 29280, C: 29295, V: 1, Contract: "MNQ 12-26", Source: store.BarSourceLive})
	}
	if err := bh.InsertBars(live); err != nil {
		t.Fatal(err)
	}
	if ins, skip, err := bh.ImportBars(imports); err != nil || ins != 5 || skip != 0 {
		t.Fatalf("fixture imports inserted=%d skipped=%d err=%v, want 5/0", ins, skip, err)
	}
	rows, err := bh.LastNBarsOn("MNQ", "5m", "MNQ 12-26", 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 505 {
		t.Fatalf("premise: the reader must hand the door 505 rows (imports included), got %d", len(rows))
	}
	for _, reseeded := range []bool{false, true} {
		kept, excluded := rehydrateRowsFor(rows, reseeded)
		if len(kept) != 500 || excluded != 5 {
			t.Fatalf("reseeded=%v: door kept %d (want 500), counted import=%d (want 5)", reseeded, len(kept), excluded)
		}
		for _, r := range kept {
			if r.Source == store.BarSourceHistoricalImport {
				t.Fatalf("reseeded=%v: an import row (%d) got through the door", reseeded, r.OpenTimeMs)
			}
		}
	}
}

// PIN 5d — THE COUNTER IS READABLE ON THE RUNNING BOT.
//
// THE DEFECT THIS CATCHES: telemetry.BarHorizonCounts had zero production
// readers. Five arms were counted on every detection and nothing on the running
// bot could ever print them — a counter that records into a void, which is the
// opposite of "counters record, never infer" (class 35). There is no API
// surface in this wave's footprint, so the totals ride the one line that
// already survives the dedupe.
func TestBarHorizonWarnLineCarriesTheCounters(t *testing.T) {
	txt := barHorizonCountsTxt()
	for _, arm := range []string{"short=", "holed=", "empty=", "graced=", "suppressed="} {
		if !strings.Contains(txt, arm) {
			t.Fatalf("arm %q is counted but never printed: %q", arm, txt)
		}
	}
	before := telemetry.BarHorizonCounts()["short"]
	telemetry.IncBarHorizon("short")
	after := barHorizonCountsTxt()
	if !strings.Contains(after, fmt.Sprintf("short=%d", before+1)) {
		t.Fatalf("the rendered totals do not READ telemetry (A11): before=%d line=%q", before, after)
	}
}

// THE P0 LINE SAYS WHAT THE RING HOLDS UNDER [O] "i want fuull data": every tf
// refills from the store after a drop (3b), so "LIVE-ONLY … 1m-only, owner
// condition 2026-09-09" is a superseded literal on a non-1m break (class 82);
// and it says whether NT8 was re-asked (3a) — once per boot, then not.
func TestScaleBreakP0TextMatchesTheRefillAndTheReask(t *testing.T) {
	for _, tf := range []string{"1m", "5m", "1h"} {
		got := scaleBreakRefillTxt(tf, nil)
		if strings.Contains(got, "1m-only") || strings.Contains(got, "LIVE-ONLY") {
			t.Fatalf("%s: superseded 1m-only literal on the P0 line: %q", tf, got)
		}
		if !strings.Contains(got, "store") || !strings.Contains(got, "NT8 re-asked") {
			t.Fatalf("%s: want the store refill + the re-ask named, got %q", tf, got)
		}
	}
	spent := scaleBreakRefillTxt("5m", ntwire.ErrHistoryReplaySpent)
	if !strings.Contains(spent, "NT8 NOT re-asked") || !strings.Contains(spent, "restart the AddOn") {
		t.Fatalf("spent budget must be named with the fix, got %q", spent)
	}
	down := scaleBreakRefillTxt("5m", errors.New("tcp_server: feed down"))
	if !strings.Contains(down, "NT8 NOT re-asked") || !strings.Contains(down, "feed down") {
		t.Fatalf("a refused re-ask must carry its reason, got %q", down)
	}
}

// TestBarHorizonCensusSeesNestedSkipNamedDirs plants the needle in EVERY
// censuswalk.NestedProbeDirs directory of a synthetic module and asserts
// prodCallSites reports every one. With the old any-depth SkipDir the dirs
// named like a root skip were invisible (CLASS 258).
func TestBarHorizonCensusSeesNestedSkipNamedDirs(t *testing.T) {
	root := t.TempDir()
	dirs := censuswalk.NestedProbeDirs()
	for _, dir := range dirs {
		full := filepath.Join(root, filepath.FromSlash(dir))
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
		src := "package " + censuswalk.PackageName(dir) + "\n\nfunc offender() {\n\t_ = barHorizonWarn(0, 0)\n}\n"
		if err := os.WriteFile(filepath.Join(full, "offender.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, where := prodCallSites(t, root, "barHorizonWarn(")
	seen := map[string]bool{}
	for _, w := range where {
		seen[w] = true
	}
	var missed []string
	for _, dir := range dirs {
		if !seen[dir+"/offender.go"] {
			missed = append(missed, dir)
		}
	}
	if len(missed) > 0 {
		t.Fatalf("the bar-horizon census skipped %d of %d nested probe dirs (saw %d sites) — a skip by NAME at depth exempts compiled packages (CLASS 258):\n\t%s",
			len(missed), len(dirs), n, strings.Join(missed, "\n\t"))
	}
}
