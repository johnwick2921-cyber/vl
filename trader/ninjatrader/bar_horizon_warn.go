package ninjatrader

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vl/kernel"
	"vl/logger"
	ntwire "vl/provider/ninjatrader"
	"vl/telemetry"
)

// ── THE BAR-HORIZON WARN (wave BARS HORIZON, 2026-09-09) ─────────────────────
//
// One observation point, at the bridge, for all 57 consumer call sites of
// market.FuturesBarsProvider. It RECORDS and WARNS. It never refuses, never
// degrades, never changes a returned slice (A10/A24 — this wave adds no gate).
//
// Three arms, and the third is the one the live instance needed:
//   SHORT  served < requested — the two 12000-bar callers against a 2500 ring
//   HOLED  open-market intervals missing INSIDE the served span
//   EMPTY  nothing came back
// On 2026-09-09 13:18:13 CT the read that armed order 133 was served 2000 of
// 2000 — SHORT was silent. HOLED is what speaks for that read.

// barHorizonWarnWindow re-arms one key after this long. Measured justification:
// the planner cadence on 2026-09-09 was 32m/10m/37m between reads
// (planner_read_facts ids 64->67), the monitor tick is 60s, and 57 consumer
// sites hit this bridge per cycle — an undeduped warn writes thousands of lines
// into a log file that was already 511 MB for that date.
// barHorizonWarnWindow is the dedupe window per (symbol, tf, why). FIVE
// minutes (101 D3(b), 2026-09-16): the running rev had produced 8,258 🕳 SHORT
// lines since boot because the key also carried the caller, the requested
// count and the served count — served grows by one every bar, so the key was
// unique per call and the window suppressed almost nothing.
const barHorizonWarnWindow = 5 * time.Minute

// barHorizonBootGrace suppresses the EMPTY arm for this long after the bridge is
// wired, because between the Go boot and the AddOn's replay EVERY site is served
// 0 bars. Measured 2026-09-09: PID 438 up 13:02:25 CT, first non-zero backfill
// 13:06:48 CT — ~4.5 minutes, ~40 EMPTY warns without a grace. The grace lifts on
// the backfill OR on this timer, so a TCP server that never comes up still gets
// one loud line rather than permanent silence.
const barHorizonBootGrace = 10 * time.Minute

type barHorizonWarnState struct {
	firstAt    time.Time
	lastAt     time.Time
	suppressed int
	// callers aggregates who noticed the condition inside the window, so the
	// one line that speaks can name all of them (D3(b)).
	callers map[string]int
}

var (
	barHorizonMu     sync.Mutex
	barHorizonSeen   = map[string]*barHorizonWarnState{}
	barHorizonDayMs  int64
	barHorizonGrace  time.Time
	barHorizonArmed  bool
	barHorizonLanded bool
)

// armBarHorizonGrace starts the EMPTY-arm boot grace. Called once, where the
// bridge is wired — that IS the entry point, so it owns the clock (A28).
func armBarHorizonGrace(now time.Time) {
	barHorizonMu.Lock()
	barHorizonGrace, barHorizonArmed, barHorizonLanded = now, true, false
	barHorizonMu.Unlock()
	barHorizonBeyondMu.Lock() // a boot: the once-per-boot INFO lines may speak again
	barHorizonBeyondSaid = map[string]bool{}
	barHorizonBeyondMu.Unlock()
}

// noteBarHorizonBackfillLanded lifts the EMPTY grace the moment the AddOn's
// replay has been flushed — the same seam bar_persist_wire.go already uses to
// stop a boot line reporting a cold cache.
func noteBarHorizonBackfillLanded() {
	barHorizonMu.Lock()
	barHorizonLanded = true
	barHorizonMu.Unlock()
}

// resetBarHorizonWarnsForSessionDay clears the dedupe state at a CME session-day
// rollover, so a condition that persisted yesterday is news again today. Called
// from barHorizonClaim on every rollover (its production call site).
func resetBarHorizonWarnsForSessionDay(sessionDayMs int64) {
	barHorizonMu.Lock()
	barHorizonSeen = map[string]*barHorizonWarnState{}
	barHorizonDayMs = sessionDayMs
	barHorizonMu.Unlock()
}

// barHorizonInBootGrace answers with the CALLER's clock (A28).
func barHorizonInBootGrace(now time.Time) bool {
	barHorizonMu.Lock()
	defer barHorizonMu.Unlock()
	if !barHorizonArmed || barHorizonLanded {
		return false
	}
	return now.Sub(barHorizonGrace) < barHorizonBootGrace
}

// barHorizonClaim is the dedupe. The key is deliberately NOT the rendered line:
// that carries an age which changes on every call and would suppress nothing.
// Returns whether to emit, how many were eaten since, and when the run started.
func barHorizonClaim(key, caller string, now time.Time) (bool, int, time.Time, map[string]int) {
	day := kernel.CMESessionDayStart(now).UnixMilli()
	barHorizonMu.Lock()
	stale := day != barHorizonDayMs
	barHorizonMu.Unlock()
	if stale {
		// The CME session-day rollover: a condition that persisted yesterday is
		// news again today. This is resetBarHorizonWarnsForSessionDay's
		// production call site (A29).
		resetBarHorizonWarnsForSessionDay(day)
	}
	barHorizonMu.Lock()
	st, ok := barHorizonSeen[key]
	if !ok {
		barHorizonSeen[key] = &barHorizonWarnState{firstAt: now, lastAt: now, callers: map[string]int{caller: 1}}
		barHorizonMu.Unlock()
		return true, 0, now, map[string]int{caller: 1}
	}
	if now.Sub(st.firstAt) >= barHorizonWarnWindow {
		eaten, since := st.suppressed, st.firstAt
		// the re-armed line names who was talking during the window it closes
		who := st.callers
		who[caller]++
		st.firstAt, st.lastAt, st.suppressed = now, now, 0
		st.callers = map[string]int{caller: 1}
		barHorizonMu.Unlock()
		return true, eaten, since, who
	}
	st.suppressed++
	st.lastAt = now
	if st.callers == nil {
		st.callers = map[string]int{}
	}
	st.callers[caller]++
	barHorizonMu.Unlock()
	return false, 0, time.Time{}, nil
}

// barHorizonCallersTxt renders the aggregated callers deterministically.
func barHorizonCallersTxt(who map[string]int) string {
	if len(who) == 0 {
		return "callers=[]"
	}
	names := make([]string, 0, len(who))
	for n := range who {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		if who[n] > 1 {
			parts = append(parts, fmt.Sprintf("%s×%d", n, who[n]))
		} else {
			parts = append(parts, n)
		}
	}
	return "callers=[" + strings.Join(parts, " ") + "]"
}

// barHorizonWarn counts every detection, then decides whether to speak.
// COUNT FIRST, ALWAYS (class 35): the counter must not inherit the dedupe.
func barHorizonWarn(now time.Time, h kernel.BarHorizon, symbol, tf, caller string, ringCap int) {
	// m452 (release #55) - TWO changes to what this function counts and says.
	//
	// (1) A DEPTH REQUEST BEYOND THE RING IS NOT A DEFECT. The two 12000-bar
	// callers (the planner candle tape, the weekly shadow) ask a 2500-bar ring for
	// five times what it can ever hold; the caller splices the rest from the store
	// (barsWithStoreDepth prints its own "bars depth" line with the post-splice
	// horizon). That read is SHORT on every single call by construction, and the
	// live #54 boot showed what a structural SHORT does to the signal: 21,953
	// "short" counts in 2.6 minutes (about 500x the #53 rate), a WARN class nobody
	// can act on, and real hole lines buried under it. So it is now its own arm,
	// "beyond_ring": counted (record, never infer), told ONCE per boot per
	// (symbol, tf) at INFO, never WARNed, never counted as "short". EMPTY and HOLED
	// are untouched and stay loud - a request beyond the ring that ALSO comes back
	// empty or with holes inside the served span is still a finding.
	//
	// (2) THE BOOT GRACE IS APPLIED BEFORE THE COUNTERS. Between the Go boot and the
	// AddOn's replay every site is served 0 bars; that is expected for ~4.5 minutes
	// and the grace exists so it is not reported - but the empty and short counters
	// were bumped BEFORE the grace check, so the grace silenced the line and not the
	// numbers. A graced read now counts as "graced" and nothing else.
	if h.Empty() && barHorizonInBootGrace(now) {
		if h.Why() != "" {
			telemetry.IncBarHorizon("graced")
		}
		return
	}
	beyondRing := h.Short() && !h.Empty() && ringCap > 0 && h.Requested > ringCap
	empty, short, holed := h.Empty(), h.Short() && !beyondRing, h.Holed()
	if beyondRing {
		telemetry.IncBarHorizon("beyond_ring")
		barHorizonNoteBeyondRing(symbol, tf, h, ringCap, caller)
	}
	why := barHorizonWhy(empty, short, holed)
	if why == "" {
		return
	}
	if empty {
		telemetry.IncBarHorizon("empty")
	}
	if short {
		telemetry.IncBarHorizon("short")
	}
	if holed {
		telemetry.IncBarHorizon("holed")
	}
	// THE KEY IS THE CONDITION, NOT THE OBSERVER. (symbol, tf, why) — never
	// the caller, never the counts. A ring that is short is one fact; the
	// callers that noticed are a list on the line, and the counts on the
	// line are the LATEST read, which is what the reader wants.
	key := strings.Join([]string{symbol, tf, why}, "|")
	emit, eaten, since, who := barHorizonClaim(key, caller, now)
	if !emit {
		telemetry.IncBarHorizon("suppressed")
		return
	}
	extra := ""
	if eaten > 0 {
		extra = fmt.Sprintf(" · suppressed=%d since %s", eaten, kernel.ClockCTSeconds(since))
	}
	// WARN, never INFO: INFO is journald-suppressed here and never reaches
	// log_events, so an INFO line lives only inside a log file that rotates on
	// process start. WARN ships through logger.AttachDBSink and is queryable.
	//
	// THE COUNTERS RIDE THE LINE (added in review, 2026-09-09). A29: before
	// this, telemetry.BarHorizonCounts had ZERO production readers — five arms
	// counted on the running bot that nobody could ever read, which is a
	// counter that records into a void. There is no API surface in this wave's
	// footprint, so the totals ship on the one line that already survives the
	// dedupe. They are READ from telemetry, never recomputed here (A11), and
	// they are process-lifetime totals, not a rate — no denominator is implied.
	logger.Warnf("🕳 bar horizon %s: %s %s · ring=%d %s%s · totals(since boot) %s",
		why, symbol, h.Line(), ringCap, barHorizonCallersTxt(who), extra, barHorizonCountsTxt())
}

// barHorizonWhy joins the arms the way kernel.BarHorizon.Why does, over the
// EFFECTIVE arms (a beyond-the-ring SHORT is not one of them).
func barHorizonWhy(empty, short, holed bool) string {
	var parts []string
	if empty {
		parts = append(parts, "EMPTY")
	}
	if short {
		parts = append(parts, "SHORT")
	}
	if holed {
		parts = append(parts, "HOLED")
	}
	return strings.Join(parts, "+")
}

// barHorizonInfof is the INFO writer of the once-per-boot lines; a var so the pins
// can read it (the repo's capture sink is WARN+ only).
var barHorizonInfof = logger.Infof

var (
	barHorizonBeyondMu   sync.Mutex
	barHorizonBeyondSaid = map[string]bool{}
)

// barHorizonNoteBeyondRing says, ONCE per boot per (symbol, tf), that a caller
// asks for more than the ring can hold. INFO: it is a fact about the call sites,
// not a fault; the per-read numbers live in the "beyond_ring" counter.
func barHorizonNoteBeyondRing(symbol, tf string, h kernel.BarHorizon, ringCap int, caller string) {
	k := symbol + "|" + tf
	barHorizonBeyondMu.Lock()
	said := barHorizonBeyondSaid[k]
	barHorizonBeyondSaid[k] = true
	barHorizonBeyondMu.Unlock()
	if said {
		return
	}
	barHorizonInfof("🕳 bar horizon: %s %s is asked for %d bars but the ring holds at most %d (served %d) - a depth request beyond the ring is spliced from the store by its caller, not a hole; counted under beyond_ring, said once per boot · first asker %s",
		symbol, tf, h.Requested, ringCap, h.Served, caller)
}

// barHorizonCountsTxt renders telemetry.BarHorizonCounts in a stable arm order
// so two log lines can be diffed. Every value is READ; none is inferred.
func barHorizonCountsTxt() string {
	c := telemetry.BarHorizonCounts()
	parts := make([]string, 0, len(c))
	for _, k := range []string{"short", "holed", "empty", "graced", "beyond_ring", "suppressed"} {
		parts = append(parts, fmt.Sprintf("%s=%d", k, c[k]))
	}
	return strings.Join(parts, " ")
}

// resolveBarHorizonCaller names the frame that asked for the bars.
//
// HONEST LIMITATION, stated rather than papered over: SEVEN sites call the
// provider on BEHALF of someone else — trader/bars_store_depth.go's
// barsWithStoreDepth (added by D2, and now the caller named on the two 1m sites
// and the RV baseline's fallback read), trader/desk_facts.go's deskBars, and the
// closure indirections in auto_trader_planner.go, auto_trader_wake_levels.go,
// auto_trader_weekly.go and kernel/engine_analysis.go. For those the name is the
// WRAPPER, not the ultimate consumer. It is NOT mitigated by walking N frames to
// guess: a heuristic that confidently names the wrong function is worse than an
// honest wrapper name, and the wrapper name is unique and greppable. The only
// frames skipped are this wave's OWN shims.
//
// PAIR THE TWO LINES WHEN YOU READ A D2 SITE (added in review, 2026-09-09).
// barsWithStoreDepth asks the ring for 12,000 1m bars, which the 2,500-bar ring
// can never serve, so this 🕳 SHORT line fires on EVERY planner read and every
// weekly shadow — and is then satisfied one layer up by the store splice, which
// prints its own "📚 bars depth: … ring <horizon> → store-deepened <horizon>"
// line with the post-splice horizon. The SHORT line describes the RING; the 📚
// line describes what the CALLER actually received. Neither is suppressed:
// suppressing the ring's line would hide a genuinely dead feed on the day the
// store also fails.
func resolveBarHorizonCaller() string {
	var pcs [16]uintptr
	n := runtime.Callers(2, pcs[:])
	if n == 0 {
		return "UNKNOWN (no stack)"
	}
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		base := filepath.Base(f.File)
		if base != "bars_market_bridge.go" && base != "bar_horizon_warn.go" {
			return shortCallerPath(f.File) + ":" + strconv.Itoa(f.Line)
		}
		if !more {
			break
		}
	}
	return "UNKNOWN (no frame outside the bridge)"
}

// shortCallerPath keeps the last two path segments — enough to grep, short
// enough for a log line.
func shortCallerPath(p string) string {
	dir, file := filepath.Split(p)
	return filepath.Join(filepath.Base(filepath.Clean(dir)), file)
}

// barHorizonBootLine reports what the ring ACTUALLY holds per (symbol, tf),
// every field READ from the cache rather than from DefaultBarCacheMaxBars or
// defaultAutoBarsBack (A11). Printed after the backfill lands, never before —
// the boot 📊 line once "reported own1m for every TF on a cold cache".
func barHorizonBootLine(cache *ntwire.BarCache, now time.Time) string {
	if cache == nil {
		return "🕳 bar horizon: cache UNKNOWN (no TCP server) · depths UNKNOWN"
	}
	keys := cache.Keys()
	depths := make([]string, 0, len(keys))
	for _, k := range keys {
		depths = append(depths, fmt.Sprintf("%s %s=%d", k[0], k[1], cache.Count(k[0], k[1])))
	}
	sort.Strings(depths)
	txt := strings.Join(depths, " · ")
	if txt == "" {
		txt = "none cached yet"
	}
	return fmt.Sprintf("🕳 bar horizon: ring=%d · warn window=%s · boot grace=%s · at %s · depths %s",
		cache.MaxBars(), barHorizonWarnWindow, barHorizonBootGrace,
		kernel.ClockCTSeconds(now), txt)
}
