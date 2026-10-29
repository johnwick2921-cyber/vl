package ninjatrader

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"vl/kernel"
	"vl/logger"
	ntwire "vl/provider/ninjatrader"
	"vl/telemetry"
)

// ── BARS HORIZON pins (2026-09-09) ───────────────────────────────────────────
//
// These drive the PRODUCTION function barsFromCache, not a copy of it (class
// 86). wireFuturesBarsProvider's closure is a one-line delegate to it.
//
// A28: every clock here is STATED. Nothing calls time.Now().

// bhWarnCapture attaches the repo's WARN+ sink and returns a getter. The sink is
// process-global (logger/db_sink.go:40-45, newest wins), so every log-capture
// pin MUST restore a no-op on cleanup and must not run t.Parallel().
func bhWarnCapture(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	logger.AttachDBSink(func(_ int64, level, _, _, message, _ string) {
		mu.Lock()
		lines = append(lines, level+" "+message)
		mu.Unlock()
	})
	t.Cleanup(func() { logger.AttachDBSink(func(int64, string, string, string, string, string) {}) })
	return func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), lines...) }
}

// bhSeed puts n contiguous 1m bars into the cache, oldest first. Each bar
// carries distinct OHLC and non-zero volume so the cache's placeholder filter
// (NO SYNTHETIC BARS, 2026-08-17) keeps all of them.
func bhSeed(c *ntwire.BarCache, symbol, tf string, startCT time.Time, n int) {
	step := int64(60_000)
	bars := make([]ntwire.Bar, 0, n)
	for i := 0; i < n; i++ {
		p := 29000 + float64(i)*0.25
		bars = append(bars, ntwire.Bar{T: startCT.UnixMilli() + int64(i)*step, O: p, H: p + 1, L: p - 1, C: p + 0.5, V: 10})
	}
	c.SeedHistorical(symbol, tf, bars)
}

func bhCT(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, kernel.CTLocation())
}

func bhCountLines(lines []string, subs ...string) int {
	n := 0
	for _, l := range lines {
		ok := true
		for _, s := range subs {
			if !strings.Contains(l, s) {
				ok = false
				break
			}
		}
		if ok {
			n++
		}
	}
	return n
}

// suppressedFiveRe is the rollover discriminator: the 5-read fresh run before
// the rollover would show as "suppressed=5" in the rollover line if the state
// had not cleared. `[^0-9]` bounds it: the cumulative totals line can reach
// "suppressed=591" under -count=N, and "suppressed=5" is a prefix of it — the
// exact-5 form never matches a 5xx total.
var suppressedFiveRe = regexp.MustCompile(`suppressed=5[^0-9]`)

// suppressedSinceRe is the re-armed dedupe marker: "suppressed=N since T".
var suppressedSinceRe = regexp.MustCompile(`suppressed=[0-9]+ since `)

// bhFilter returns only the lines containing sub — a test's own caller frames.
// Under -count=N with -race, prior tests' goroutines can still emit warn lines
// into the shared sink while a later test captures; every assert must scope to
// the caller strings THIS test drives, never to the whole sink.
func bhFilter(lines []string, sub string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.Contains(l, sub) {
			out = append(out, l)
		}
	}
	return out
}

// bhCountRe counts lines matching the regexp exactly. The cumulative
// "totals(since boot)" counters keep rising under -count=N, so a substring
// assert like "suppressed=5" would match "suppressed=591" inside a totals line
// (the -count=5 flake DS-108 reproduced). The re-armed dedupe marker is
// `suppressed=<digits> since ` — matched exactly, never by the totals.
func bhCountRe(lines []string, re *regexp.Regexp) int {
	n := 0
	for _, l := range lines {
		if re.MatchString(l) {
			n++
		}
	}
	return n
}

// PIN 1 — a read served fewer bars than it asked for, with the ask INSIDE the
// ring, WARNS and names the consumer. (m452: an ask BEYOND the ring - the two
// 12000-bar sites against a 2500-bar ring - is structural, spliced from the store
// by its caller, and has its own pins below: it must NOT warn.)
func TestBridgeWarnsWhenServedIsShortOfRequested(t *testing.T) {
	resetBarHorizonWarnsForSessionDay(0)
	get := bhWarnCapture(t)
	cache := ntwire.NewBarCache(2500)
	bhSeed(cache, "MNQ", "1m", bhCT(2026, time.September, 8, 9, 0), 1000)
	now := bhCT(2026, time.September, 9, 13, 18)

	out := barsFromCache(cache, "MNQ", "1m", 2500, now)

	if len(out) != 1000 {
		t.Fatalf("returned %d klines, want 1000 — the warn must change nothing that is RETURNED (A10)", len(out))
	}
	lines := bhFilter(get(), "bar_horizon_bridge_test.go")
	if n := bhCountLines(lines, "bar horizon"); n != 1 {
		t.Fatalf("want 1 bar-horizon warn, got %d captured lines (asked=2500 served=1000): %v", n, lines)
	}
	for _, want := range []string{"asked=2500", "served=1000", "ring=2500", "SHORT", "MNQ", "callers=["} {
		if bhCountLines(lines, want) != 1 {
			t.Fatalf("warn line missing %q: %v", want, lines)
		}
	}
	// The caller must be THIS test's file, not the bridge's own closure.
	if bhCountLines(lines, "callers=[ninjatrader/bar_horizon_bridge_test.go:") != 1 {
		t.Fatalf("warn line does not name this test as the caller: %v", lines)
	}
}

// PIN 1-m452 — THE PRODUCTION CALL SITE (barsFromCache) for a depth request
// BEYOND the ring: no WARN, no "short" count, one INFO per (symbol, tf) per boot,
// a "beyond_ring" count per read, and the returned slice unchanged. Mutant: drop
// the beyondRing branch in barHorizonWarn -> the WARN and the short count return.
func TestBridgeDepthBeyondTheRingIsInfoOncePerBootNotAWarn(t *testing.T) {
	resetBarHorizonWarnsForSessionDay(0)
	armBarHorizonGrace(bhCT(2026, time.September, 9, 8, 0)) // a boot: clears the once-per-boot memory
	noteBarHorizonBackfillLanded()
	get := bhWarnCapture(t)
	var infoMu sync.Mutex
	var infos []string
	prevInfof := barHorizonInfof
	barHorizonInfof = func(format string, args ...interface{}) {
		infoMu.Lock()
		infos = append(infos, fmt.Sprintf(format, args...))
		infoMu.Unlock()
	}
	t.Cleanup(func() { barHorizonInfof = prevInfof })
	cache := ntwire.NewBarCache(2500)
	bhSeed(cache, "MNQ", "1m", bhCT(2026, time.September, 8, 9, 0), 2500)
	now := bhCT(2026, time.September, 9, 13, 18)
	baseShort := telemetry.BarHorizonCounts()["short"]
	baseBeyond := telemetry.BarHorizonCounts()["beyond_ring"]

	for i := 0; i < 50; i++ { // 50 reads: the planner tape, the weekly shadow, every cycle
		if out := barsFromCache(cache, "MNQ", "1m", 12000, now.Add(time.Duration(i)*time.Second)); len(out) != 2500 {
			t.Fatalf("returned %d klines, want 2500 - the observation must change nothing that is RETURNED (A10)", len(out))
		}
	}
	lines := bhFilter(get(), "bar_horizon_bridge_test.go")
	if n := bhCountLines(lines, "bar horizon"); n != 0 {
		t.Fatalf("a depth request beyond the ring WARNed %d time(s): %v", n, lines)
	}
	infoMu.Lock()
	gotInfos := append([]string(nil), infos...)
	infoMu.Unlock()
	if n := bhCountLines(gotInfos, "beyond the ring", "MNQ 1m"); n != 1 || len(gotInfos) != 1 {
		t.Fatalf("want exactly ONE INFO line per boot for MNQ 1m, got %d: %v", len(gotInfos), gotInfos)
	}
	if got := telemetry.BarHorizonCounts()["short"] - baseShort; got != 0 {
		t.Fatalf("a beyond-the-ring ask must not count as short: short rose by %d", got)
	}
	if got := telemetry.BarHorizonCounts()["beyond_ring"] - baseBeyond; got != 50 {
		t.Fatalf("beyond_ring rose by %d, want 50 - every read is counted, only the line is once", got)
	}
}

// A beyond-the-ring ask that ALSO has a hole inside the served span stays LOUD:
// the HOLED arm is untouched.
func TestBridgeDepthBeyondTheRingStillWarnsOnARealHole(t *testing.T) {
	resetBarHorizonWarnsForSessionDay(0)
	noteBarHorizonBackfillLanded()
	get := bhWarnCapture(t)
	cache := ntwire.NewBarCache(2500)
	bhSeed(cache, "MNQ", "1m", bhCT(2026, time.September, 9, 8, 30), 60)
	bhSeed(cache, "MNQ", "1m", bhCT(2026, time.September, 9, 10, 50), 60) // an 80-minute open-market hole
	barsFromCache(cache, "MNQ", "1m", 12000, bhCT(2026, time.September, 9, 12, 0))
	lines := bhFilter(get(), "bar_horizon_bridge_test.go")
	if bhCountLines(lines, "bar horizon", "HOLED", "gaps=80") != 1 {
		t.Fatalf("a real hole under a beyond-the-ring ask must WARN as HOLED: %v", lines)
	}
	if bhCountLines(lines, "SHORT") != 0 {
		t.Fatalf("the structural SHORT must not be named on the hole line: %v", lines)
	}
}

// m452 - the boot grace silences the NUMBERS too. Live #54: 21,953 short/empty
// counts in the first 2.6 minutes (about 500x the #53 rate) because the counters
// were bumped before the grace check.
func TestEmptyInBootGraceCountsOnlyGraced(t *testing.T) {
	resetBarHorizonWarnsForSessionDay(0)
	boot := bhCT(2026, time.September, 9, 13, 2)
	armBarHorizonGrace(boot)
	cache := ntwire.NewBarCache(2500) // nothing seeded: the AddOn's replay has not landed
	c0 := telemetry.BarHorizonCounts()
	for i := 0; i < 30; i++ {
		barsFromCache(cache, "MNQ", "1m", 2000, boot.Add(time.Duration(i)*time.Second))
	}
	c1 := telemetry.BarHorizonCounts()
	if d := c1["graced"] - c0["graced"]; d != 30 {
		t.Fatalf("graced rose by %d, want 30", d)
	}
	for _, k := range []string{"empty", "short", "holed", "beyond_ring"} {
		if d := c1[k] - c0[k]; d != 0 {
			t.Fatalf("%s rose by %d inside the boot grace, want 0 (only graced counts there)", k, d)
		}
	}
}

// PIN 1b — the OTHER direction, per the acceptance: a satisfied read is SILENT.
func TestBridgeSilentWhenReadIsSatisfied(t *testing.T) {
	resetBarHorizonWarnsForSessionDay(0)
	get := bhWarnCapture(t)
	cache := ntwire.NewBarCache(2500)
	bhSeed(cache, "MNQ", "1m", bhCT(2026, time.September, 8, 9, 0), 2500)
	now := bhCT(2026, time.September, 9, 13, 18)

	out := barsFromCache(cache, "MNQ", "1m", 2000, now)

	if len(out) != 2000 {
		t.Fatalf("returned %d klines, want 2000 (tail truncation unchanged)", len(out))
	}
	// The TAIL, not the head: truncation keeps the NEWEST bars. Pinned because
	// the observation must not be able to change what is returned (A10).
	cached := cache.Get("MNQ", "1m")
	if out[len(out)-1].OpenTime != cached[len(cached)-1].T {
		t.Fatalf("newest returned bar %d, want the cache's newest %d — truncation must keep the TAIL",
			out[len(out)-1].OpenTime, cached[len(cached)-1].T)
	}
	if out[0].OpenTime != cached[len(cached)-2000].T {
		t.Fatalf("oldest returned bar %d, want %d", out[0].OpenTime, cached[len(cached)-2000].T)
	}
	if n := bhCountLines(bhFilter(get(), "bar_horizon_bridge_test.go"), "bar horizon"); n != 0 {
		t.Fatalf("a satisfied, contiguous read emitted %d bar-horizon warns, want 0: %v", n, bhFilter(get(), "bar_horizon_bridge_test.go"))
	}
}

// PIN 1c — a HOLED read that is NOT short still warns. This is the 2026-09-09
// 13:18:13 CT case: 2000 of 2000 served, 696 open-market minutes missing inside
// them. A served-count check alone is silent here.
func TestBridgeWarnsOnAHoledWindowThatIsNotShort(t *testing.T) {
	resetBarHorizonWarnsForSessionDay(0)
	get := bhWarnCapture(t)
	cache := ntwire.NewBarCache(2500)
	// Two contiguous runs with an 80-minute open-market hole between them.
	bhSeed(cache, "MNQ", "1m", bhCT(2026, time.September, 9, 8, 30), 60)
	bhSeed(cache, "MNQ", "1m", bhCT(2026, time.September, 9, 10, 50), 60)
	now := bhCT(2026, time.September, 9, 12, 0)

	out := barsFromCache(cache, "MNQ", "1m", 120, now)

	if len(out) != 120 {
		t.Fatalf("returned %d klines, want 120", len(out))
	}
	lines := bhFilter(get(), "bar_horizon_bridge_test.go")
	if bhCountLines(lines, "bar horizon", "HOLED", "gaps=80") != 1 {
		t.Fatalf("want one HOLED warn with gaps=80 (served==asked==120), got: %v", lines)
	}
	if bhCountLines(lines, "SHORT") != 0 {
		t.Fatalf("SHORT fired on a fully-served read: %v", lines)
	}
}

// PIN 6 — every warn and boot-line field is READ from the object that enforces
// it, never a literal (A11).
func TestWarnFieldsAreResolvedNotLiteral(t *testing.T) {
	resetBarHorizonWarnsForSessionDay(0)
	get := bhWarnCapture(t)
	cache := ntwire.NewBarCache(1234) // deliberately NOT DefaultBarCacheMaxBars
	bhSeed(cache, "MNQ", "1m", bhCT(2026, time.September, 9, 9, 0), 100)
	now := bhCT(2026, time.September, 9, 10, 39)

	barsFromCache(cache, "MNQ", "1m", 500, now)

	lines := bhFilter(get(), "bar_horizon_bridge_test.go")
	for _, want := range []string{"ring=1234", "asked=500", "served=100"} {
		if bhCountLines(lines, want) != 1 {
			t.Fatalf("warn line missing %q: %v", want, lines)
		}
	}
	if bhCountLines(lines, "2500") != 0 {
		t.Fatalf("warn line contains 2500 (the DEFAULT ring), want the cache's own 1234 (A11): %v", lines)
	}

	boot := barHorizonBootLine(cache, now)
	if !strings.Contains(boot, "ring=1234") || !strings.Contains(boot, "MNQ 1m=100") {
		t.Fatalf("boot line %q must report ring=1234 and MNQ 1m=100, both READ from the cache (A11)", boot)
	}
	if strings.Contains(boot, "2500") || strings.Contains(boot, "=2000") {
		t.Fatalf("boot line %q contains a literal depth", boot)
	}
	_ = fmt.Sprint()
}
