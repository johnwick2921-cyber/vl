// Package ninjatrader — Plan 4.4 Stage 2 bar cache.
//
// Holds the latest N OHLCV bars per (symbol, timeframe) received over the
// TCP wire from the C# AddOn (frames bars_historical + bar_update). Thread-
// safe and bounded; read by the kernel (Stage 3 decision feed) and the
// chart relay (Stage 4) via Get().
//
// Backpressure invariant: writes from the socket read loop go through a
// bounded channel into a dedicated drain goroutine, which then calls into
// this cache. The cache itself uses a brief Lock() per write and an RLock
// + copy-on-read for snapshots, so reads never block writes for long.
package ninjatrader

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultBarCacheMaxBars is the per-(symbol, timeframe) ring-buffer
// capacity. 2500 (was 1024) retains the deeper re-seed window
// (defaultAutoBarsBack=2000) so a backfilled feed-down gap stays visible
// in the chart instead of being trimmed off; still covers EMA200 + ATR14
// with wide headroom.
const DefaultBarCacheMaxBars = 2500

// BarCache stores the most recent bars per (symbol, timeframe). Writes
// from bars_historical SEED the slice; writes from bar_update UPSERT,
// replacing the bar at the same t (in-progress bar) or appending new bars
// (capped at MaxBars — oldest bars are dropped FIFO once the ring fills).
type BarCache struct {
	mu      sync.RWMutex
	bars    map[string][]Bar // key: "SYMBOL|TIMEFRAME"
	maxBars int
	// dropped counts NT8 empty-minute placeholder bars refused at ingest.
	dropped int64
	// W4/D21: the AddOn's boundary frame carries the just-closed bar AFTER
	// the new forming one, so it arrives with a smaller timestamp. finalisations
	// counts those accepted as a close of a bar the ring already holds;
	// outOfOrderDrops counts every other backwards bar, still refused.
	finalisations   int64
	outOfOrderDrops int64
	// BAR-SOURCE WAVE: per key, whether a live bar has been seen this process
	// (the scale-mismatch check runs once, on the first), and every mismatch
	// detected, for the boot line.
	liveSeen map[string]bool
	// scaleSkips records, per key, a scale check that could not be made because
	// the only reference was not adjacent to the live bar (101 D1' rule 2).
	scaleSkips map[string]ScaleCheckSkip
	mismatches map[string]ScaleMismatch
	// seedOffScale is set when the CURRENT seed for a key was found on another
	// scale, and cleared by the next SeedHistorical. mismatches keeps every
	// detection for the boot line; this answers "is the seed I hold bad NOW".
	seedOffScale map[string]bool
	// lastLive is, per key, WHEN this process last received a LIVE bar frame
	// that the ring accepted (bar_update, never a historical seed). It is the
	// time.Now() of Upsert, so it carries Go's MONOTONIC reading: an age taken
	// with time.Since is immune to wall-clock steps (release #22 OPS-SAFETY —
	// the stale gate judged bar age on the wall clock alone and failed OPEN on
	// a backward step). Read via LastLiveReceipt.
	lastLive map[string]time.Time
	// GAP-BACKFILL (2026-10-14): seeds judged by same-minute overlap with live
	// bars (bar_overlap_verdict.go) — agreed sets the verdict, disagreed leaves
	// the key armed. Read via OverlapVerdictCounts.
	overlapAgreed, overlapDisagreed int64
	// overlapJudgeUntil — per SYMBOL, until when a replay is judged by
	// same-minute overlap: armed only by the gap-backfill re-request
	// (armOverlapVerdict), so every other replay keeps the first-live-bar rule.
	overlapJudgeUntil map[string]time.Time
	// lastReconnectUnixNano (WAVE 26) — the wall-clock (UnixNano) instant of the
	// most recent AddOn (re)connect/hello. The stale-print guard
	// (bar_stale_print.go) is armed for stalePrintGuardWindow after it.
	lastReconnectUnixNano atomic.Int64
	// staleQuarantined (W26 F2/P2) — per (symbol,tf) key, the OPEN-stamp Ts the
	// STALE-PRINT guard quarantined. ONLY these stay quarantined on a later
	// "clean" update of the same T. A bar the SCALE-BREAK guard marked mixed is
	// deliberately NOT in this set: a genuine roll minute must be re-promotable
	// when the live side proves correct, so the keep-quarantined freeze must
	// never apply to every mixed bar.
	staleQuarantined map[string]map[int64]struct{}
}

// NewBarCache constructs an empty cache. maxBars <= 0 uses
// DefaultBarCacheMaxBars.
func NewBarCache(maxBars int) *BarCache {
	if maxBars <= 0 {
		maxBars = DefaultBarCacheMaxBars
	}
	return &BarCache{
		bars:             make(map[string][]Bar),
		staleQuarantined: make(map[string]map[int64]struct{}),
		maxBars:          maxBars,
	}
}

// markStaleQuarantined records that the STALE-PRINT guard (not the scale-break
// guard) quarantined this (key, open-stamp T). It is the ONLY path into the
// keep-quarantined freeze.
func (c *BarCache) markStaleQuarantined(key string, t int64) {
	if c == nil {
		return
	}
	set := c.staleQuarantined[key]
	if set == nil {
		set = make(map[int64]struct{})
		c.staleQuarantined[key] = set
	}
	set[t] = struct{}{}
}

// isStaleQuarantined reports whether this (key, open-stamp T) was quarantined by
// the stale-print guard. A scale-break mixed bar is NOT in the set.
func (c *BarCache) isStaleQuarantined(key string, t int64) bool {
	if c == nil {
		return false
	}
	_, ok := c.staleQuarantined[key][t]
	return ok
}

// pruneStaleQuarantined drops set entries whose T is no longer in the ring, so
// the set stays bounded by the ring when a seed replaces or trims it.
func (c *BarCache) pruneStaleQuarantined(key string, ring []Bar) {
	set := c.staleQuarantined[key]
	if len(set) == 0 {
		return
	}
	present := make(map[int64]bool, len(ring))
	for _, b := range ring {
		present[b.T] = true
	}
	for t := range set {
		if !present[t] {
			delete(set, t)
		}
	}
	if len(set) == 0 {
		delete(c.staleQuarantined, key)
	}
}

// NO SYNTHETIC BARS, EVER (P0 2026-08-17).
//
// NinjaTrader's own minute store keeps EMPTY-MINUTE PLACEHOLDER records, and its
// bar builder materialises each one as a bar with open==high==low==close (the
// .ncd file's base price) and volume 0. Whenever a declared-open session has no
// real ticks, NT8 therefore hands us a flat line rather than a gap.
//
// That is exactly what the owner saw. After the AddOn watchdog livelock
// (7aa521a1) forced NT8 to re-fetch Friday 2026-08-14 into an all-placeholder
// file, /api/klines returned 959 of 1500 one-minute bars with O=H=L=C=30147.50
// and volume 0, contiguous from 00:02 to 16:00 CT — a 16-hour horizontal line
// across the chart where TradingView (and this chart, before) would simply skip
// to the next real candle.
//
// Every hop we own is a verbatim pass-through, so nothing in our code invents
// these bars — but nothing rejected them either, and they reach the chart AND
// the kernel's detectors. A bar with no volume and no range carries no
// information by construction; the only thing it can do is lie. Drop it at
// ingest, which is the one place that protects both consumers at once.
//
// The test is deliberately narrow: BOTH zero volume AND zero range. A real but
// illiquid minute that still printed a range is kept, and so is a zero-volume
// bar that somehow carries one.
func isPlaceholderBar(b Bar) bool {
	return b.V == 0 && b.H == b.L && b.O == b.C && b.O == b.H
}

// dropPlaceholderBars returns bars with NT8's empty-minute placeholders removed,
// plus how many were dropped. It allocates only when something is actually
// dropped, so the healthy path stays free.
func dropPlaceholderBars(bars []Bar) ([]Bar, int) {
	bad := 0
	for i := range bars {
		if isPlaceholderBar(bars[i]) {
			bad++
		}
	}
	if bad == 0 {
		return bars, 0
	}
	out := make([]Bar, 0, len(bars)-bad)
	for i := range bars {
		if !isPlaceholderBar(bars[i]) {
			out = append(out, bars[i])
		}
	}
	return out, bad
}

// DroppedPlaceholders reports how many NT8 empty-minute placeholder bars this
// cache has refused, so the condition is observable instead of silent.
func (c *BarCache) DroppedPlaceholders() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.dropped
}

// AllPairs enumerates every (symbol, timeframe) pair currently held — the
// boot-backfill entry point for bar persistence.
// Finalisations reports how many times a live bar's close reached the cache
// via the AddOn's boundary re-emit (W4/D21). READ, never assumed.
func (c *BarCache) Finalisations() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.finalisations
}

// OutOfOrderDrops reports backwards bars refused because the ring has never
// held their timestamp — a genuine misordering, not a close.
func (c *BarCache) OutOfOrderDrops() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.outOfOrderDrops
}

func (c *BarCache) AllPairs() [][2]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([][2]string, 0, len(c.bars))
	for k := range c.bars {
		parts := strings.SplitN(k, "|", 2)
		if len(parts) == 2 {
			out = append(out, [2]string{parts[0], parts[1]})
		}
	}
	return out
}

// ── CANONICAL TIME CONTRACT (2026-08-19, chart-timestamp dispatch) ──────────
//
//	A Bar's T in THIS CACHE is the bar's OPEN time, epoch ms UTC.
//
// NT8 stamps bars at their period END (NinjaScript Bars.GetTime(i) is the
// close; proven live: a forming 5m bar covering 01:30–01:35 CT arrives stamped
// 01:35 while the clock reads 01:31). The C# AddOn forwards that close stamp
// verbatim, and this cache used to store it unchanged while every reader —
// barsToKlines, /api/klines, the SSE relay, the kernel detectors, the charts —
// treated T as the OPEN. Net effect: every bar was labelled one full period
// late everywhere downstream.
//
// The conversion happens HERE, once, at ingest, because every consumer reads
// this cache (the SSE relay polls it; REST serves it; the kernel bridges it).
// Converting in any single reader would leave the twins wrong (the multi-
// instance defect class). The C# side and its LastEmittedTimeUtcMs dedup
// cursor stay in close-stamp domain untouched — no wire change.
//
// Side effect worth naming: C2's clockDriftMs and the clock-health line both
// compute "feed now" as newestT + interval. Under close stamps that OVERSHOT
// the true close by one interval; with open stamps it lands exactly on NT8's
// own stamp again — a strict accuracy improvement, thresholds untouched.
func openStampBars(bars []Bar, timeframe string) []Bar {
	dur := timeframeMs(timeframe)
	if dur <= 0 || len(bars) == 0 {
		return bars
	}
	out := make([]Bar, len(bars))
	for i, b := range bars {
		b.T -= dur
		out[i] = b
	}
	return out
}

// OpenStampBars (exported, BAR-TRUTH 2026-08-28) applies the canonical
// close-stamp → open-stamp conversion ONCE for every reader. The persistence
// path must use it too — historical replay frames arrive close-stamped, and
// without it the DB rows landed at T+1m (2499/2500 common-window mismatches).
func OpenStampBars(bars []Bar, timeframe string) []Bar {
	return openStampBars(bars, timeframe)
}

// timeframeMs mirrors the coded TF vocabulary (bars_market_bridge.go keeps the
// kernel-side twin; both fall back to 1m).
func timeframeMs(timeframe string) int64 {
	switch timeframe {
	case "1m":
		return 60_000
	case "2m": // parity with kernel.TFDurationMs
		return 120_000
	case "3m":
		return 180_000
	case "5m":
		return 300_000
	case "15m":
		return 900_000
	case "30m":
		return 1_800_000
	case "1h":
		return 3_600_000
	case "2h":
		return 7_200_000
	case "4h":
		return 14_400_000
	case "6h": // C11 (2026-08-25) — previously fell through to 1m
		return 21_600_000
	case "8h":
		return 28_800_000
	case "12h":
		return 43_200_000
	case "1d", "1D":
		return 86_400_000
	case "3d": // C11 — previously fell through to 1m
		return 259_200_000
	case "1w", "1W":
		return 604_800_000
	default:
		return 60_000
	}
}

// SeedHistorical MERGES the provided bars into the cache for (symbol,
// timeframe). Called when the C# AddOn emits a bars_historical frame: the
// initial seed after a fresh BarsRequest, a Go-reconnect re-seed (N3), OR a
// data-feed reconnect recreate.
//
// MERGE, not replace: a re-seed can legitimately carry FEWER bars than the
// cache already holds — e.g. a reconnect recreate whose BarsRequest only loaded
// the current session, or an empty cursor-deduped frame. Replacing on such a
// frame WIPED the deeper history we'd accumulated (the Phase-0 reconnect
// regression: the chart collapsed to "today only"). Merging makes the persistent
// ring durable — it never shrinks on a thinner re-seed, accumulates real depth
// across sessions, survives reconnects, and lets a future deeper load fill in
// older bars in front. Union is by bar time T; incoming wins on overlap
// (freshest OHLCV for that bar); ascending order preserved; tail-capped at
// maxBars. Both inputs are assumed ascending by T (protocol contract
// vltrader_tcp_PROTOCOL.md §6).
func (c *BarCache) SeedHistorical(symbol, timeframe string, bars []Bar) {
	if symbol == "" || timeframe == "" {
		return
	}
	bars, bad := dropPlaceholderBars(bars)
	bars = openStampBars(bars, timeframe)         // close-stamp → OPEN-stamp, once, for every reader
	bars = stampSource(bars, BarSourceHistorical) // BAR-SOURCE WAVE: a replay is a replay
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropped += int64(bad)
	key := barKey(symbol, timeframe)
	existing := c.bars[key]
	// A ZERO-BAR REPLAY MUST NOT RE-ARM THE SCALE CHECK (101 D1', 2026-09-16).
	// On 09-16 the feed flapped five times; each reconnect's BarsRequest ran
	// while the feed was down and delivered nothing. The re-arm below used to
	// run BEFORE this return, so an empty frame re-armed the check without
	// giving it a new reference — and the next live bar was judged against the
	// boot replay's last bar from eleven hours earlier. 146.75 pts of
	// overnight move read as a scale break; 1,999 5m and 1,832 1m historical
	// bars were dropped; the 5m horizon went from 285h to 11h for the life of
	// the process. Nothing to judge means nothing to arm.
	if len(bars) == 0 && len(existing) > 0 {
		return
	}
	// Every NON-EMPTY replay re-arms the scale check: the FIRST live bar after
	// THIS seed is compared against it. Per-process arming would let a
	// mid-session reconnect re-seed on a different scale and never be caught —
	// a mutation survived on exactly that gap.
	if c.liveSeen != nil {
		delete(c.liveSeen, key)
	}
	if c.seedOffScale != nil {
		delete(c.seedOffScale, key)
	}
	if c.scaleSkips != nil {
		delete(c.scaleSkips, key)
	}
	if len(existing) == 0 {
		// First seed for this key — copy the tail (detaches from caller's array).
		if len(bars) > c.maxBars {
			bars = bars[len(bars)-c.maxBars:]
		}
		stored := make([]Bar, len(bars))
		copy(stored, bars)
		c.bars[key] = stored
		delete(c.staleQuarantined, key) // fresh ring, fresh keep-quarantined set
		return
	}
	if len(bars) == 0 {
		// Empty re-seed (cursor-deduped reconnect frame) — keep what we have.
		return
	}
	// A REPLAY NEVER OVERWRITES A LIVE BAR (bar-source wave). mergeBarsByTime
	// said "incoming is freshest"; for a replay landing on minutes that already
	// traded live, that put a back-adjusted bar over a real one.
	merged := mergeSeedKeepingLive(existing, bars)
	if len(merged) > c.maxBars {
		merged = merged[len(merged)-c.maxBars:]
	}
	c.bars[key] = merged
	c.pruneStaleQuarantined(key, merged)
	// GAP-BACKFILL (2026-10-14): a replay the gap re-request ASKED FOR lands
	// on minutes that already traded live; it is judged on THOSE minutes, now
	// — the first-live-bar check can never find an adjacent reference for it
	// (bar_overlap_verdict.go). Agreement sets the verdict the replay-hold
	// waits for; disagreement changes nothing. Any other replay is untouched.
	if until, armed := c.overlapJudgeUntil[symbol]; !armed || time.Now().After(until) {
		return
	}
	if pairs, agree := overlapScaleVerdict(symbol, existing, bars); pairs > 0 {
		if agree {
			if c.liveSeen == nil {
				c.liveSeen = make(map[string]bool)
			}
			c.liveSeen[key] = true
			c.overlapAgreed++
		} else {
			c.overlapDisagreed++
		}
	}
}

// mergeBarsByTime merges two ascending-by-T bar slices into one ascending,
// time-deduped slice. On equal T, the bar from `incoming` wins (it is the
// freshest OHLCV for that bar boundary); bars present only in `existing`
// (older history a thinner re-seed didn't carry) are preserved. Linear
// two-pointer merge — both inputs must be ascending by T.
func mergeBarsByTime(existing, incoming []Bar) []Bar {
	out := make([]Bar, 0, len(existing)+len(incoming))
	i, j := 0, 0
	for i < len(existing) && j < len(incoming) {
		switch {
		case existing[i].T < incoming[j].T:
			out = append(out, existing[i])
			i++
		case existing[i].T > incoming[j].T:
			out = append(out, incoming[j])
			j++
		default: // same bar time — incoming is freshest
			out = append(out, incoming[j])
			i++
			j++
		}
	}
	for ; i < len(existing); i++ {
		out = append(out, existing[i])
	}
	for ; j < len(incoming); j++ {
		out = append(out, incoming[j])
	}
	return out
}

// Upsert merges streaming bar_update bars into the cache. For each input
// bar:
//   - If the t matches the last cached bar's t: REPLACE (in-progress bar
//     update — the current minute's bar is still forming).
//   - If the t is strictly greater than the last cached bar's t: APPEND
//     (a new bar boundary was crossed).
//   - If the t is strictly less than the last cached bar's t: IGNORE
//     (out-of-order — the protocol contract is ascending, but defend
//     against partial misalignment after reconnect).
//
// The ring is bounded: when the slice exceeds maxBars, the oldest bar is
// dropped (FIFO).
//
// The input slice may contain MULTIPLE bars (the NT8 multi-bar gotcha:
// a single tick can update MinIndex..MaxIndex). Each is applied in input
// order via the rules above.
func (c *BarCache) Upsert(symbol, timeframe string, bars []Bar) {
	if symbol == "" || timeframe == "" || len(bars) == 0 {
		return
	}
	bars, bad := dropPlaceholderBars(bars)
	bars = openStampBars(bars, timeframe)   // close-stamp → OPEN-stamp, once, for every reader
	bars = stampSource(bars, BarSourceLive) // BAR-SOURCE WAVE: the minute as it traded
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropped += int64(bad)
	if len(bars) == 0 {
		return // the whole update was placeholders
	}
	key := barKey(symbol, timeframe)
	now := time.Now()
	for i := range bars {
		// THE BOOT MINUTE IS NEVER A MIXED BAR — detected on the first live bar
		// after a historical seed; on a mismatch the seed is dropped and this
		// bar is labelled mixed (values untouched, A24).
		bars[i], _ = c.detectScaleMismatch(key, bars[i], now)
	}
	existing := c.bars[key]
	accepted := false
	for i := range bars {
		b := bars[i]
		// WAVE 26 — a live print that lands right after a (re)connect/hello can
		// carry the AddOn's RELOADED series (a day-old price). Quarantine it
		// (label mixed) when its close is impossible against both its own open
		// and the previous ring bar, so no reader takes it.
		var quarantined bool
		b, quarantined = c.quarantineStalePrint(symbol, b, prevBarBefore(existing, b.T))
		bars[i] = b
		// W26 F2/P2 — record ONLY the stale-print quarantine (not a scale-break
		// mixed bar) as the one that stays frozen on a later clean update.
		if quarantined {
			c.markStaleQuarantined(key, b.T)
		}
		if len(existing) == 0 {
			existing = append(existing, b)
			accepted = true
			continue
		}
		last := existing[len(existing)-1]
		switch {
		case b.T == last.T:
			// W26 F2: a minute the STALE-PRINT guard quarantined stays
			// quarantined — a later "clean" update of the same T (a live tick
			// that returns the close to the real price while the stale extreme
			// survives) must not re-promote it to live. A scale-break mixed bar
			// is NOT frozen (a real roll minute re-promotes on proof).
			if c.isStaleQuarantined(key, b.T) {
				b.Source = BarSourceMixed
			}
			existing[len(existing)-1] = b
			accepted = true
		case b.T > last.T:
			existing = append(existing, b)
			accepted = true
			if len(existing) > c.maxBars {
				// Drop the oldest bar; copy the tail forward in place
				// to keep the slice's backing array bounded.
				dropped := existing[0]
				copy(existing, existing[1:])
				existing = existing[:c.maxBars]
				// Prune the keep-quarantined set with the ring (bounded).
				delete(c.staleQuarantined[key], dropped.T)
			}
		default:
			// W4/D21: a bar's own CLOSE always arrives "backwards". The AddOn
			// builds the forming bars first and only then appends the
			// just-closed previous bar with final=true
			// (VLBarsSubscriptionManager.cs:518-533 then :539-551), so the
			// finalisation carries a strictly smaller timestamp than the bar
			// appended a moment earlier and used to land here and be dropped —
			// which is why no LIVE-closed bar was ever Final in the cache and
			// every Picture consumer, filtering on Final, saw only seed bars.
			//
			// Accept such a bar ONLY as the finalisation of a bar the ring
			// ALREADY HOLDS at that exact timestamp. It is never an insert, so
			// a genuine out-of-order bar — one for a time this ring has never
			// seen — is still refused exactly as before.
			if b.Final {
				if idx := indexOfBarT(existing, b.T); idx >= 0 {
					if !existing[idx].Final {
						// W26 F2: keep a stale-print-quarantined minute
						// quarantined on the finalisation path too.
						if c.isStaleQuarantined(key, b.T) {
							b.Source = BarSourceMixed
						}
						existing[idx] = b
						c.finalisations++
					}
					continue
				}
			}
			c.outOfOrderDrops++
		}
	}
	c.bars[key] = existing
	// Only a frame that moved the ring's newest bar (a forming-bar update or a
	// new bar) proves the feed is alive NOW. A finalisation of an older bar or
	// a refused out-of-order bar does not.
	if accepted {
		if c.lastLive == nil {
			c.lastLive = make(map[string]time.Time)
		}
		c.lastLive[key] = now
	}
}

// LastLiveReceipt returns when this process last received an accepted LIVE
// bar frame for (symbol, timeframe), as a time.Time carrying the monotonic
// clock reading (so time.Since on it ignores wall-clock steps). ok=false: no
// live frame received since the process started (a historical seed alone
// never counts — it is a replay, not evidence the feed is alive).
func (c *BarCache) LastLiveReceipt(symbol, timeframe string) (time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.lastLive[barKey(symbol, timeframe)]
	return t, ok
}

// indexOfBarT returns the index of the bar stamped t, or -1. The ring is
// ascending and a finalisation targets the newest bars, so the scan runs
// backwards and stops as soon as it passes t.
func indexOfBarT(bars []Bar, t int64) int {
	for i := len(bars) - 1; i >= 0; i-- {
		if bars[i].T == t {
			return i
		}
		if bars[i].T < t {
			return -1
		}
	}
	return -1
}

// Get returns a SNAPSHOT (defensive copy) of the cached bars for
// (symbol, timeframe). The returned slice is safe for the caller to hold
// or mutate without affecting the cache. Returns nil if no bars are
// cached yet.
//
// The RLock is held only for the duration of the slice copy, which is
// microseconds. Readers never block writers for long.
func (c *BarCache) Get(symbol, timeframe string) []Bar {
	c.mu.RLock()
	defer c.mu.RUnlock()
	src, ok := c.bars[barKey(symbol, timeframe)]
	if !ok || len(src) == 0 {
		return nil
	}
	out := make([]Bar, len(src))
	copy(out, src)
	return out
}

// Count returns the number of cached bars for (symbol, timeframe). Useful
// for tests and metrics.
func (c *BarCache) Count(symbol, timeframe string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.bars[barKey(symbol, timeframe)])
}

// MaxBars is this cache's ring capacity — READ, not assumed.
// DefaultBarCacheMaxBars is the DEFAULT; NewBarCache accepts any value, so a
// line that names the ring must ask the cache rather than the constant (A11).
func (c *BarCache) MaxBars() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.maxBars
}

// Keys returns the list of currently-populated (symbol, timeframe) pairs.
// Returned in unspecified order. Useful for diagnostics + the Stage 4
// chart relay enumerating its outbound subscriptions.
func (c *BarCache) Keys() [][2]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([][2]string, 0, len(c.bars))
	for k := range c.bars {
		s, t, ok := splitBarKey(k)
		if !ok {
			continue
		}
		out = append(out, [2]string{s, t})
	}
	return out
}

// PurgeSymbol removes EVERY timeframe's cached bars for a symbol (P5.3 clean
// teardown after bars_unsubscribe — a removed symbol must not leave stale data
// for the chart/API to read). Case-insensitive on the symbol half of the key.
// Returns the number of (symbol|timeframe) entries removed.
func (c *BarCache) PurgeSymbol(symbol string) int {
	if symbol == "" {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := 0
	for k := range c.bars {
		s, _, ok := splitBarKey(k)
		if ok && strings.EqualFold(s, symbol) {
			delete(c.bars, k)
			delete(c.lastLive, k)
			removed++
		}
	}
	return removed
}

// barKey encodes (symbol, timeframe) as a single map key. We deliberately
// keep the encoding simple (single delimiter) and document it so the
// splitBarKey reverse is unambiguous. Symbols + timeframes are bounded
// strings without "|" so the delimiter is safe.
func barKey(symbol, timeframe string) string {
	return symbol + "|" + timeframe
}

// splitBarKey reverses barKey. Returns ok=false if the format is
// unexpected (defensive — should only be reachable if external code
// mutated the cache directly).
func splitBarKey(k string) (symbol, timeframe string, ok bool) {
	for i := 0; i < len(k); i++ {
		if k[i] == '|' {
			return k[:i], k[i+1:], true
		}
	}
	return "", "", false
}

// RehydrateOlder EXTENDS an already-live ring BACKWARDS with older bars — the
// D3 half of the BARS HORIZON wave (2026-09-09, owner-authorised).
//
// THE PROBLEM: a Go restart drops the ring to the AddOn's 2000-bar seed
// (defaultAutoBarsBack, tcp_server.go) while the persisted `bars` table holds
// 21 days. SeedHistorical merges WITHIN a process, so the ring climbs 2000 →
// 2500 across a session, but nothing rehydrates it from the store — every
// restart shortened the horizon again, silently.
//
// IT IS NOT SeedHistorical, AND THE DIFFERENCE IS DELIBERATE:
//
//   - A COLD KEY IS A NO-OP. SeedHistorical seeds an empty key; this refuses
//     to. An empty ring means the feed is down or the AddOn replay has not
//     landed, and filling it from the store would make a dead feed look alive
//     to every reader downstream. The store DEEPENS a live tape; it never
//     stands in for one.
//   - EXISTING WINS ON OVERLAP. SeedHistorical lets `incoming` win because
//     incoming is the freshest wire OHLCV; here `incoming` is the STORE, which
//     is by definition not fresher than the live ring. Only bars strictly
//     OLDER than the ring's oldest are taken, so no live or forming bar can be
//     replaced by a stored one.
//
// Placeholder bars are refused at this door exactly as at every other (NO
// SYNTHETIC BARS, EVER — isPlaceholderBar). Nothing is invented, interpolated
// or carried forward: only bars the caller actually handed over are stored.
//
// Returns how many bars were ACTUALLY added, so the boot line can report a
// resolved count rather than an intention (A11).
func (c *BarCache) RehydrateOlder(symbol, timeframe string, bars []Bar) int {
	if c == nil || symbol == "" || timeframe == "" || len(bars) == 0 {
		return 0
	}
	bars, bad := dropPlaceholderBars(bars)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropped += int64(bad)
	key := barKey(symbol, timeframe)
	existing := c.bars[key]
	if len(existing) == 0 {
		return 0 // COLD KEY — the store never substitutes for the live feed.
	}
	oldest := existing[0].T
	older := make([]Bar, 0, len(bars))
	for _, b := range bars {
		if b.T < oldest {
			older = append(older, b)
		}
	}
	if len(older) == 0 {
		return 0
	}
	// EXISTING is `incoming` here, so the live ring wins every overlap. The
	// filter above already removed every overlapping bar, so this is a plain
	// ascending splice — mergeBarsByTime is reused rather than re-derived so
	// the ordering rule has exactly one implementation.
	merged := mergeBarsByTime(older, existing)
	if len(merged) > c.maxBars {
		// Trim the OLDEST. The live tail is never the part that goes.
		merged = merged[len(merged)-c.maxBars:]
	}
	added := len(merged) - len(existing)
	if added < 0 {
		added = 0
	}
	c.bars[key] = merged
	return added
}
