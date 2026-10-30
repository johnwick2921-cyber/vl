package mentor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"vl/kernel"
	"vl/market"
)

// Seed minima — the EXACT warm-ups the mentor sources need (CTO P0
// 1791009358785, binding point 3 — DS-108's replay uses the same numbers).
const (
	// FourHEMA34Min is the ABSOLUTE minimum for the 4h EMA 34: 34 closed 4h
	// candles is the EMA's FIRST value, not a warmed-up one (CTO 07:00).
	FourHEMA34Min = 34
	// FourHEMA34Warmup is DS-103's STATED warm-up — the floor the fail-closed
	// gate actually reads. 3×34, the CTO's safe default (a barely-formed EMA
	// must never trade). Seed() refuses below it and the boot line prints it.
	FourHEMA34Warmup = 102
	// OneMEMA34Min / OneMEMA34Warmup: the same split for the 1m EMA 34.
	OneMEMA34Min    = 34
	OneMEMA34Warmup = 102
	// OneHRTHLevelsMin: the 1H RTH key-level set needs at least 2 candles
	// (one colour change) — otherwise there is nothing to draw. Full stored
	// history is used (F7: no lookback cap).
	OneHRTHLevelsMin = 2
	// Closed15mMin: R6's 15m conflict reads CLOSED 15m candles — at least one.
	Closed15mMin = 1
)

// seedDepth is one evaluator's per-source history depth: measured from the
// stored bars at Seed, then advanced by the evaluator's own state as bars
// arrive (advanceDepth), so the fail-closed gate can clear without a restart.
type seedDepth struct {
	fourH int // closed 4h candles (17:00 CT anchor)
	oneM  int // closed 1m bars
	oneH  int // 1H RTH level-set candles
	today int // 1 once a bar of the current session exists
	m15   int // closed 15m candles
}

// seedDepthOf measures every source's depth from the stored 1m bars.
func seedDepthOf(bars1m []market.Kline, now int64) seedDepth {
	// P1 (CTO 12:44:08Z / 12:47:11Z): ONE bar source — every higher timeframe
	// is AGGREGATED FROM 1m; the store's native 1h is never read. The LEVEL
	// walk uses the RTH-filtered 1h aggregation; the 4h EMA uses the ALL-HOURS
	// 1h aggregation (keyLevel1HBars drops non-RTH candles, which would gut the
	// Globex 4h buckets).
	d := seedDepth{
		fourH: len(fourHClosedBuckets(barsTF(bars1m, 60), now)),
		oneM:  closedCount(bars1m, now),
		oneH:  len(keyLevel1HBars(bars1m)),
		m15:   len(closedBucketsTF(bars1m, 15, now)),
	}
	// Today's session: at least one 1m bar whose trading-day key (17:00 CT
	// flip, EPOCH RULING: real UTC ms through America/Chicago) matches the
	// current session's key — covers both the overnight and the RTH half.
	key := sessionKeyCT(now)
	for _, b := range bars1m {
		if sessionKeyCT(b.OpenTime) == key {
			d.today = 1
			break
		}
	}
	return d
}

// missingFromDepth names every source below its warm-up. The floors are the
// constants above and are never lowered; the strings are the ones the
// injector's mentorSourcesMissing expects.
func missingFromDepth(d seedDepth) []string {
	var missing []string
	// 4h EMA 34: 4h buckets from the 1h history, 17:00 CT anchor, closed only.
	// The gate reads the WARM-UP (102), not the bare minimum (34).
	if d.fourH < FourHEMA34Warmup {
		missing = append(missing, fmt.Sprintf("bar history depth: 4h EMA34 warm-up (%d/%d 4h candles)", d.fourH, FourHEMA34Warmup))
	}
	// 1m EMA 34.
	if d.oneM < OneMEMA34Warmup {
		missing = append(missing, fmt.Sprintf("bar history depth: 1m EMA34 warm-up (%d/%d 1m bars)", d.oneM, OneMEMA34Warmup))
	}
	// 1H RTH level set.
	if d.oneH < OneHRTHLevelsMin {
		missing = append(missing, fmt.Sprintf("bar history depth: 1H RTH level history (%d candles)", d.oneH))
	}
	if d.today == 0 {
		missing = append(missing, "bar history depth: today's session")
	}
	// Closed 15m candle.
	if d.m15 < Closed15mMin {
		missing = append(missing, fmt.Sprintf("bar history depth: closed 15m candle (%d)", d.m15))
	}
	return missing
}

// SeedMissing checks the per-source history depth at boot. Every short source
// is named (the same strings the injector's mentorSourcesMissing expects).
func SeedMissing(bars1m []market.Kline, now int64) []string {
	return missingFromDepth(seedDepthOf(bars1m, now))
}

// sessionKeyCT returns the trading-day key (17:00 CT flip, EPOCH RULING:
// real UTC ms through America/Chicago) a bar belongs to — "today's
// session" in SeedMissing is membership in THIS key, not a calendar-day
// window.
func sessionKeyCT(ms int64) string {
	return tradingDayKey(time.UnixMilli(ms).In(ctime()))
}

// SeedDepths reports each source's seeded depth, keyed by the names the
// injector's per-source refusal prints (mentorDepthRequirements). The values
// are the SAME numbers SeedMissing compares against its floors — one source
// of truth for the seed depth and the refusal line.
func SeedDepths(bars1m, bars1h []market.Kline, now int64) map[string]int {
	// P3-1 (ENGINE-AUDIT-R2 DS-104): "today session" is the 17:00 CT Globex
	// session — the SAME definition seedDepthOf (the fail-closed gate) reads —
	// not the calendar day. One definition for one name, so the boot depth
	// line and the gate can never disagree on what "today" means.
	todayN := 0
	key := sessionKeyCT(now)
	for _, b := range bars1m {
		if sessionKeyCT(b.OpenTime) == key {
			todayN = 1
			break
		}
	}
	return map[string]int{
		"4h EMA34":      len(fourHClosedBuckets(bars1h, now)),
		"1m EMA34":      closedCount(bars1m, now),
		"1h level set":  len(keyLevel1HBars(bars1h)),
		"today session": todayN,
		"closed 15m":    len(closedBucketsTF(bars1m, 15, now)),
	}
}

// Seed warms the evaluator state from the STORED bars (1m + 1h; the 4h series
// is built from 1h, session-anchored at 17:00 CT). It returns the missing
// sources ("" names are never returned) — while any are missing the evaluator
// REFUSES entries (fail-closed: never trade on a cold EMA or truncated
// levels). Seeding is deterministic: the same bars rebuild the same state.
func Seed(e *Evaluator, bars1m []market.Kline, now int64) []string {
	return seed(e, bars1m, keyLevel1HBars(bars1m), now)
}

// SeedFull is Seed with the 1H RTH key-level walk fed from a PRE-STITCHED
// full-history series (KEYLEVEL-FULL-HISTORY, release #10). FIX 3: bars1m is
// now the STITCHED, back-adjusted 1m series (same gap + cut as full1HRTH), NOT
// the current contract's 1m — so the 4h EMA 34, the #435 seeded 5m/4h/1h
// trigger lines and the depth seam ALL run on the same full history. The 1m
// EMA 34/9 tail is unchanged by construction (the stitched tail IS the
// current contract's recent bars). full1HRTH overrides ONLY the 1H RTH
// key-level walk (levels + BODY-close deletion). The trader builds both from
// the same RollStitcher result.
func SeedFull(e *Evaluator, bars1m []market.Kline, full1HRTH []market.Kline, now int64) []string {
	return seed(e, bars1m, full1HRTH, now)
}

func seed(e *Evaluator, bars1m []market.Kline, full1HRTH []market.Kline, now int64) []string {
	// Defensive copy: the 15m depth check filters in place over the caller's
	// backing array.
	bars1m = append([]market.Kline(nil), bars1m...)
	// P1 (CTO 12:44:08Z / 12:47:11Z): the higher timeframes are AGGREGATED
	// FROM 1m (the store's native 1h is never read for the EMAs/4h). The level
	// walk is the RTH-filtered 1h aggregation — or the full stitched history
	// when SeedFull was called; the 4h EMA uses the all-hours aggregation.
	e.seeded = true
	e.depth = seedDepthOf(bars1m, now)
	e.depth.oneH = len(full1HRTH) // full stitched history when SeedFull called
	e.depth4hBucket = fourHBucketStart(now, ctime())
	e.missing = missingFromDepth(e.depth)
	e.depthMet = ""

	e.seedHistoryState(bars1m, full1HRTH, now, false)

	// S2: the swing must never replay OLD touches at boot. Stamp the newest
	// closed 5m bar as the watermark, so the first tick walks only NEW bars —
	// a 25h tape containing an old touch stays silent instead of turning the
	// old touch into a live order (00-METHOD.md §8: "Wait for a LITERAL
	// touch" [D5.2 p2 @ 18:36–18:41] — the touch is observed live, never reconstructed).
	if b5 := closedBuckets(bars1m, now, e.Cfg); len(b5) > 0 {
		e.State.Swing.LastBarTime = b5[len(b5)-1].OpenTime
	}

	// N2 (release #23 MENTOR-SEED-STATE): the FTGH/FTGL latch is REPLAYED from
	// the 17:00 CT roll minute by minute, exactly as a continuous bot latched
	// it — a restart never redraws a box the running bot already drew
	// [D4.1 p2 @02:39–02:57 "không vẽ cái box khác… y nguyên đó tới cuối ngày"].
	// P2-N2 (mentor rescan): every latched box is then WATERMARKED at the newest
	// closed bar, so the first tick evaluates only NEW returns — the returns
	// the running bot already judged are history, never re-emitted (the same
	// watermark idea as S2 above).
	e.seedBoxLatch(closedOnly(bars1m, now), now)

	e.seedLine = e.SeedLine(0)
	return e.missing
}

// seedHistoryState builds every HISTORY-DERIVED piece of the evaluator state
// from the stored bars: the 1H RTH key levels and their BODY-close deletion,
// the 1m EMA 34/9, the swing's 4h EMA 34 line, and the 5m/4h/1h trigger lines.
// Seed runs it once at boot; ReseedState re-runs it after a history refill
// (N1, release #23) — reset=true starts the key-level deletion and the trigger
// lines from scratch instead of continuing the boot evaluator's empty state.
// Nothing here touches a latch, a position, an arm, a leg budget or a visit
// counter.
func (e *Evaluator) seedHistoryState(bars1m []market.Kline, full1HRTH []market.Kline, now int64, reset bool) {
	// 1H RTH key levels from the FULL closed stored history (F7, no cap).
	candles1h := full1HRTH
	if n := len(candles1h); n > 0 && keyLevel1HCandleCloseTime(candles1h[n-1].OpenTime) > now {
		candles1h = candles1h[:n-1]
	}
	e.State.Seed1HBars = candles1h // the deletion check needs the FULL closed series
	e.State.SeedLevels = keyLevelsFromCandles(candles1h, e.Cfg.KeyLevelPrunePts)
	// The boot seed adds to the evaluator's (empty) deletion set; a re-seed
	// RECOMPUTES it — a level the refilled 1H history no longer deletes is live
	// again, and every deletion the running bot made is re-derived from the
	// same closed candles (the set is a pure function of SeedLevels + candles).
	deleted := e.State.DeletedLevels
	if reset || deleted == nil {
		deleted = map[string]bool{}
	}
	if n := len(candles1h); n > 0 {
		e.State.Seed1HWatermark = candles1h[n-1].OpenTime
		// The colour the incremental walk continues from — without it the first
		// extension compares against false and draws a phantom level whenever
		// the last seeded candle was green.
		e.State.Seed1HLastColour = candleColour(candles1h[n-1])
		// KEY-LEVEL RULING (KEYLEVEL-FULL-HISTORY, release #10): the BODY-close
		// deletion is computed ONCE over the FULL closed history at seed, then
		// advanced incrementally per tick past this watermark. The forming
		// candle was already dropped above, so candles1h is all-closed.
		for _, l := range e.State.SeedLevels {
			if levelDeletedBy1HBody(l, candles1h, now) {
				deleted[l.Key] = true
			}
		}
		e.State.Seed1HDeletionWatermark = candles1h[n-1].OpenTime
	}
	e.State.DeletedLevels = deleted

	// 1m EMA 34/9 from the closed 1m history (full recompute at seed, once).
	var closes []float64
	var last market.Kline
	for _, b := range bars1m {
		if b.CloseTime > now {
			continue
		}
		closes = append(closes, b.Close)
		last = b
	}
	if len(closes) > 0 {
		e.State.EMA34 = emaFromCloses(closes, e.Cfg.EMAPeriod34)
		e.State.EMA9 = emaFromCloses(closes, e.Cfg.EMAPeriod9)
		e.State.Seed1mWatermark = last.CloseTime
		e.State.EMA34Warmed = true
	}
	// M401: the 2m EMA 34/9 over the closed 2m buckets of the same history
	// (only while the schedule knob is ON; a re-seed recomputes it from scratch).
	e.State.EMA34x2, e.State.EMA9x2, e.State.Seed2mWatermark, e.State.EMA2mWarmed = 0, 0, 0, false
	if e.Cfg.Exec2mAfter30m && len(closes) > 0 {
		var closed1m []market.Kline
		for _, b := range bars1m {
			if b.CloseTime <= now {
				closed1m = append(closed1m, b)
			}
		}
		// Formed only: at least EMAPeriod34 closed 2m candles of history.
		if b2 := closedBucketsTF(closed1m, 2, now); len(b2) > 0 && len(b2) >= e.Cfg.EMAPeriod34 {
			c2 := make([]float64, len(b2))
			for i, b := range b2 {
				c2[i] = b.Close
			}
			e.State.EMA34x2 = emaFromCloses(c2, e.Cfg.EMAPeriod34)
			e.State.EMA9x2 = emaFromCloses(c2, e.Cfg.EMAPeriod9)
			e.State.Seed2mWatermark = b2[len(b2)-1].CloseTime
			e.State.EMA2mWarmed = true
		}
	}

	// The swing's 4h EMA 34 line: the last 34 closed 4h candles (17:00 anchor).
	// The swing's 4h EMA line: the canonical recurrence over ALL closed 4h
	// candles (17:00 anchor), built from the ALL-HOURS 1h aggregation of the 1m
	// history (P1). The seed and every incremental step continue the SAME
	// recurrence, so seed+ticks == full rebuild exactly. 34 closed candles are
	// the warm-up MINIMUM (SeedMissing), not a cap — never re-window.
	bars1h := barsTF(withSeamBars(bars1m, e.seamBars, now), 60)
	if b4 := fourHClosedBuckets(bars1h, now); len(b4) > 0 {
		closes4 := make([]float64, 0, len(b4))
		for _, b := range b4 {
			closes4 = append(closes4, b.Close)
		}
		e.State.Swing.Line = emaFromCloses(closes4, e.Cfg.Swing.EMAPeriod)
		e.State.Swing.BucketStart = fourHBucketStart(now, ctime())
		e.State.Swing.EmaCount = len(closes4)
		// The line is the canonical recurrence over the stored closes again —
		// the F-3 stale mark ("until a restart reseeds the line from the
		// store") is exactly what this read cures. EXCEPT (m456): a 4h bucket
		// that was OPEN CME time but has no bars (a 1m hole the replay window
		// never reached: 10-26 17:00-21:57 CT, +0.42 pt on 10-28, +10.8 pt on
		// 10-27) was skipped as if it were a weekend, so the recurrence is
		// missing a fold. Mark it stale (SWING4H refused, counted) until a
		// refill + re-seed closes the hole; holes older than the horizon have
		// decayed to nothing and do not count.
		holes, present := holedFourHBuckets(bars1h, 3_600_000, fourHBucketStart(b4[0].OpenTime, ctime()), fourHBucketStart(now, ctime()))
		e.State.Swing.HoledBuckets = recentHoledBuckets(holes, present, swingHoleHorizonBuckets(e.Cfg.Swing.EMAPeriod))
		e.State.Swing.LineStale = len(e.State.Swing.HoledBuckets) > 0
	}

	// F2 (release #10) + LINES-RESTART (release #22): seed the 5m trigger line
	// and the 4h/1h HTF trigger lines from the SAME closed 1m history by
	// REPLAYING it minute by minute through the live TriggerTick
	// (TriggerReplay1m) — never bucket-complete. A bucket read whole cannot
	// see which extreme broke first; live reads it intrabar, so a restart used
	// to change the line (P1-1: the HTF verdict differed in 20 of 276 reseed
	// samples on 10-06→10-08). The replay leaves the forming bucket
	// uncommitted, exactly as the live path does after its last tick, so the
	// first tick consumes only NEWER bars (no double-advance).
	closed1m := closedOnly(bars1m, now)
	trig, fourH, oneH := e.State.Trigger, e.State.HTF.FourH, e.State.HTF.OneH
	if reset {
		// A re-seed replays the WHOLE refilled history from an empty line —
		// continuing the old line would keep the very bucket the hole cut short
		// (TriggerTick never re-reads a bucket at or below LastBucket).
		trig, fourH, oneH = TriggerLine{}, TriggerLine{}, TriggerLine{}
	}
	e.State.Trigger = TriggerReplay1m(trig, closed1m, 5, e.Cfg)
	// Print windows: the SAME freeze the tick path uses. N6 (release #23):
	// the windows are absolute instants, and the caller hands the seed the
	// print windows of EVERY day the replayed history covers (the trader reads
	// each day's stored calendar) — a running bot froze each day's print candle
	// on that day, so the replay freezes it too. A historical bar at a
	// different absolute time never matches a window.
	if e.Cfg.NewsBarsExcluded {
		// CE-4 reworked: freeze the pre-print trigger read; the bars still
		// build their whole 1h/4h candles (no deletion).
		frozen := func(openMs int64) bool { return newsBarFrozen(openMs, e.Cfg) }
		e.State.HTF.FourH = TriggerReplay1mFrozen(fourH, closed1m, 240, e.Cfg, frozen)
		e.State.HTF.OneH = TriggerReplay1mFrozen(oneH, closed1m, 60, e.Cfg, frozen)
	} else {
		htfBars := htfFeedBars(closed1m, e.Cfg.PrintWindows)
		e.State.HTF.FourH = TriggerReplay1m(fourH, htfBars, 240, e.Cfg)
		e.State.HTF.OneH = TriggerReplay1m(oneH, htfBars, 60, e.Cfg)
	}
}

// SeedTickWindow1m is the 1m window the live tick hands Tick (the trader's
// mentorBars1mDepth — a trader pin keeps the two equal). The seed's box-latch
// replay rebuilds each past minute's box scan over the SAME window.
const SeedTickWindow1m = 1500

// seedBoxLatch (N2, release #23) replays the FTGH/FTGL latch from the 17:00 CT
// session roll, one 1m bar at a time, through the SAME BoxesBuild the tick runs
// on the SAME window the tick would have had at that minute: the first box a
// role produced is the box the running bot latched [D4.1 p2 @02:39–02:57], so a
// restart at any instant reproduces it instead of redrawing from whatever the
// scan picks NOW (10-14: a continuous bot held FTGH 30792.50–30805.75 / FTGL
// 30767.00–30801.00; the 08:22 restart drew 30878.00–30891.50 / 30498.00–
// 30586.75). The replay stops once both roles are latched — a latched role
// never changes — so a normal day costs a few dozen scans, a day where one role
// never forms costs one scan per minute of the session.
//
// Every latched box is then WATERMARKED (P2-N2, mentor rescan): BoxRefs[key] =
// the newest closed bar, so the first live tick walks only returns that close
// AFTER the seed — the running bot already judged every older one (10-14
// 08:24: the first tick walked six historical FTGH returns and set
// SchoolOneSide from them).
func (e *Evaluator) seedBoxLatch(closed1m []market.Kline, now int64) {
	day := tradingDayKey(time.UnixMilli(now).In(ctime()))
	e.State.LatchedBoxesDay = day
	e.State.LatchedBoxes = nil
	if !e.Cfg.Enabled || len(closed1m) == 0 {
		return
	}
	cfg := e.Cfg.Box
	if cfg.TF == "" {
		cfg = DefaultBoxCfg()
	}
	// The first bar a tick of THIS trading day evaluated: the tick's clock is
	// BarCloseInstant(bar), so the day key is read at that instant.
	first := sort.Search(len(closed1m), func(i int) bool {
		return tradingDayKey(time.UnixMilli(BarCloseInstant(closed1m[i])).In(ctime())) >= day
	})
	latched := map[string]Box{}
	post := []Box{}
	for j := first; j < len(closed1m); j++ {
		at := BarCloseInstant(closed1m[j])
		if tradingDayKey(time.UnixMilli(at).In(ctime())) != day {
			continue
		}
		lo := j + 1 - SeedTickWindow1m
		// On the 1m TF the scan reads only TODAY's swings, and a swing needs its
		// two left neighbours: bars before first-2 can never change the result,
		// so the window is cut there (same boxes, a fraction of the work). The
		// aggregated TFs (2m, 5m Heikin Ashi) depend on the window's start and
		// keep the live window exactly.
		// m410: a 2m box frame needs 2 left 2m neighbours (4 1m bars) and a
		// window that starts on a 2m bucket, so the cut keeps 8 bars of margin.
		cut := first - 2
		if e.Cfg.Exec2mAfter30m {
			cut = first - 8
		}
		if cfg.TF == "1m" && lo < cut {
			lo = cut
		}
		if lo < 0 {
			lo = 0
		}
		if e.Cfg.Exec2mAfter30m {
			// m410: a 2m box frame needs the window to START on a 2m bucket.
			for lo > 0 && bucketOpen(closed1m[lo].OpenTime, 2) != closed1m[lo].OpenTime {
				lo--
			}
		}
		w := closed1m[lo : j+1]
		build := e.boxFrameCfg(at) // m410: the frame this tick draws boxes on
		// Once both roles are latched the first box never changes, so the
		// full BoxesBuild scan is skipped — only the B52 new-zone check below
		// still needs the window.
		// m429 R4: with the fresh-RTH knob ON a role latched in the evening can
		// still be REPLACED by a box that forms in RTH, so the scan must reach
		// RTH — it builds from the RTH slice exactly as the tick does
		// (boxBuildBars), never the full window, and applies the SAME replace
		// rule (freshRTHReplaces) the tick's latchBoxes applies.
		rthScan := e.Cfg.CourseExits.BoxFreshRTHPair && boxRTHWindow(at, e.Cfg) && eveningLatched(latched, at)
		if len(latched) < 2 || rthScan {
			for _, b := range BoxesBuild(boxBuildBars(w, at, e.Cfg), build, time.UnixMilli(at)) {
				if build.TF != cfg.TF {
					b.TF = build.TF
				}
				old, ok := latched[string(b.Kind)]
				if !ok {
					latched[string(b.Kind)] = b
				} else if e.freshRTHReplaces(old, b) {
					latched[string(b.Kind)] = b
					post = dropPreRTHPost(post, b)
				}
			}
		}
		// B52 (release #29): replay the new-zone latch too — once a role's
		// active box escaped, the next same-role box OUTSIDE the old range is
		// latched, exactly as the tick does. The replay runs to the end of the
		// day (escapes happen long after the first boxes form).
		if e.Cfg.BoxNewZoneAfterEscape {
			for _, kind := range []BoxKind{FTGL, FTGH} {
				active, ok := activeBoxOfPair(latched, post, kind)
				if !ok {
					continue
				}
				if _, escapedYet := boxEscapeOpenMs(w, active, cfg); !escapedYet {
					continue
				}
				nb := boxAfterEscapeBuildFrame(w, build, cfg, active)
				if nb == nil || postHasBox(post, nb.Key) {
					continue
				}
				post = append(post, *nb)
			}
		} else if len(latched) >= 2 && !e.freshRTHPending(latched, at) {
			// The pre-B52 fast path: stop once both roles are latched — a
			// latched role never changes, so a normal day costs a few dozen
			// scans, a day where one role never forms costs one scan per minute.
			// (m429: not while an evening box can still be replaced in RTH.)
			break
		}
	}
	if len(latched) == 0 {
		return
	}
	e.State.LatchedBoxes = latched
	e.State.PostEscapeBoxes = post
	if e.State.BoxRefs == nil {
		e.State.BoxRefs = map[string]int64{}
	}
	newest := closed1m[len(closed1m)-1].OpenTime
	for _, b := range latched {
		if e.State.BoxRefs[b.Key] < newest {
			e.State.BoxRefs[b.Key] = newest
		}
	}
	for _, b := range post {
		if e.State.BoxRefs[b.Key] < newest {
			e.State.BoxRefs[b.Key] = newest
		}
	}
}

// eveningLatched reports whether any latched box formed before the RTH open of
// the CT day `at` falls on (a candidate for the m429 fresh-RTH replacement).
func eveningLatched(latched map[string]Box, at int64) bool {
	open := rthOpenMs(at)
	for _, b := range latched {
		if b.FormedAtMs < open {
			return true
		}
	}
	return false
}

// freshRTHPending (m429) reports whether the seed replay must keep scanning
// past "both roles latched": the knob is ON and a latched box is still an
// evening box that today's RTH (not yet over at `at`) can replace.
func (e *Evaluator) freshRTHPending(latched map[string]Box, at int64) bool {
	if !e.Cfg.CourseExits.BoxFreshRTHPair || !eveningLatched(latched, at) {
		return false
	}
	t := time.UnixMilli(at).In(ctime())
	return t.Hour()*60+t.Minute() < 17*60 // before the 17:00 roll the RTH may still produce a box
}

// SeedBoxLine is the boot line for the replayed box latch — every field READ
// from the seeded latch (a role not latched today prints "none").
func (e *Evaluator) SeedBoxLine() string {
	part := func(k BoxKind) string {
		b, ok := e.State.LatchedBoxes[string(k)]
		if !ok {
			return fmt.Sprintf("%s none", strings.ToUpper(string(k)))
		}
		return fmt.Sprintf("%s %.2f–%.2f (formed %s CT)", strings.ToUpper(string(k)), b.Bottom, b.Top, kernel.TableTimeCT(time.UnixMilli(b.FormedAtMs)))
	}
	day := e.State.LatchedBoxesDay
	if day == "" {
		day = "n/a"
	}
	return fmt.Sprintf("mentor seed: box latch replayed from the 17:00 CT roll for trading day %s: %s · %s — stands to the session roll, never redrawn [D4.1 p2 @02:39]", day, part(FTGH), part(FTGL))
}

// ReseedSnapshot is the history-derived state a re-seed may change, READ from
// the evaluator.
type ReseedSnapshot struct {
	FourH, OneH, FiveM TriggerLine
	EMA34, EMA9        float64
	EMAFrame           string // "1m" or "2m": the frame EMA34/EMA9 were read on
	SwingLine          float64
	SwingCount         int
	SwingStale         bool
	KeyLive, KeyDel    int
	Bars1m             int64 // Seed1mWatermark: the newest closed 1m bar folded in
}

// ReseedReport is the before → after of one state-only re-seed.
type ReseedReport struct {
	Before, After ReseedSnapshot
	Bars          int // closed 1m bars the re-seed read
	Refused       string
}

func (e *Evaluator) reseedSnapshot() ReseedSnapshot {
	live, del := e.keyLevelCounts()
	ema9, ema34, frame := e.State.EMAUsed()
	return ReseedSnapshot{
		FourH: e.State.HTF.FourH, OneH: e.State.HTF.OneH, FiveM: e.State.Trigger,
		EMA34: ema34, EMA9: ema9, EMAFrame: frame,
		SwingLine: e.State.Swing.Line, SwingCount: e.State.Swing.EmaCount, SwingStale: e.State.Swing.LineStale,
		KeyLive: live, KeyDel: del,
		Bars1m: e.State.Seed1mWatermark,
	}
}

// Changed reports whether the re-seed moved anything a trade reads.
func (r ReseedReport) Changed() bool {
	b, a := r.Before, r.After
	return b.FourH.Dir != a.FourH.Dir || b.FourH.Price != a.FourH.Price || b.FourH.MovedAt != a.FourH.MovedAt ||
		b.OneH.Dir != a.OneH.Dir || b.OneH.Price != a.OneH.Price || b.OneH.MovedAt != a.OneH.MovedAt ||
		b.FiveM.Dir != a.FiveM.Dir || b.FiveM.Price != a.FiveM.Price || b.FiveM.MovedAt != a.FiveM.MovedAt ||
		b.EMA34 != a.EMA34 || b.EMA9 != a.EMA9 || b.SwingLine != a.SwingLine || b.SwingStale != a.SwingStale ||
		b.KeyLive != a.KeyLive || b.KeyDel != a.KeyDel
}

// Line renders the before → after, READ values only (a field the re-seed did
// not change prints once, "unchanged").
func (r ReseedReport) Line() string {
	if r.Refused != "" {
		return "REFUSED — " + r.Refused + " (state kept as it was)"
	}
	b, a := r.Before, r.After
	trig := func(name string, x, y TriggerLine) string {
		f := func(t TriggerLine) string {
			if t.Dir == "" {
				return "none"
			}
			since := "n/a"
			if t.MovedAt > 0 {
				since = kernel.TableTimeCT(time.UnixMilli(t.MovedAt)) + " CT"
			}
			return fmt.Sprintf("%s @%.2f since %s", t.Dir, t.Price, since)
		}
		if x.Dir == y.Dir && x.Price == y.Price && x.MovedAt == y.MovedAt {
			return fmt.Sprintf("%s %s (unchanged)", name, f(y))
		}
		return fmt.Sprintf("%s %s → %s", name, f(x), f(y))
	}
	num := func(name string, x, y float64) string {
		if x == y {
			return fmt.Sprintf("%s %.2f (unchanged)", name, y)
		}
		return fmt.Sprintf("%s %.2f → %.2f (Δ%+.2f)", name, x, y, y-x)
	}
	parts := []string{
		trig("4h trigger", b.FourH, a.FourH),
		trig("1h trigger", b.OneH, a.OneH),
		trig("5m trigger", b.FiveM, a.FiveM),
		num(a.EMAFrame+" EMA34", b.EMA34, a.EMA34),
		num(a.EMAFrame+" EMA9", b.EMA9, a.EMA9),
		num("swing 4h EMA34 line", b.SwingLine, a.SwingLine),
	}
	if b.SwingStale != a.SwingStale {
		parts = append(parts, fmt.Sprintf("swing line stale %v → %v", b.SwingStale, a.SwingStale))
	}
	if b.KeyLive != a.KeyLive || b.KeyDel != a.KeyDel {
		parts = append(parts, fmt.Sprintf("key levels %d live · %d deleted → %d live · %d deleted", b.KeyLive, b.KeyDel, a.KeyLive, a.KeyDel))
	} else {
		parts = append(parts, fmt.Sprintf("key levels %d live · %d deleted (unchanged)", a.KeyLive, a.KeyDel))
	}
	verdict := "UNCHANGED"
	if r.Changed() {
		verdict = "CHANGED"
	}
	return fmt.Sprintf("%s over %d closed 1m bar(s): %s", verdict, r.Bars, strings.Join(parts, " · "))
}

// ReseedState (N1, release #23) re-runs the HISTORY-DERIVED half of the seed —
// the 5m/4h/1h trigger lines, the swing's 4h EMA 34 line, the 1m EMA 34/9 and
// the 1H RTH key levels with their BODY-close deletion — over a store read that
// now holds bars the boot seed could not see (the AddOn's history replay is
// released to the store AFTER the trader seeds, and a bar hole can be refilled
// at any time). Everything a live tick ACTED on is kept: the box latch and its
// watermarks, the day latch, the ORB, every arm, leg, loss box, position, touch
// classification and visit counter. A store read with no closed 1m bar is
// refused (the state stays as it was) — never a wipe. full1HRTH nil = the
// current-contract walk (Seed's fallback).
func ReseedState(e *Evaluator, bars1m, full1HRTH []market.Kline, now int64) ReseedReport {
	rep := ReseedReport{Before: e.reseedSnapshot()}
	closed := closedCount(bars1m, now)
	rep.Bars = closed
	if !e.seeded {
		rep.Refused = "the evaluator was never seeded"
		rep.After = rep.Before
		return rep
	}
	if closed == 0 {
		rep.Refused = "the store read returned no closed 1m bar"
		rep.After = rep.Before
		return rep
	}
	if len(full1HRTH) == 0 {
		full1HRTH = keyLevel1HBars(bars1m)
	}
	e.seedHistoryState(bars1m, full1HRTH, now, true)
	// The depth only ever grows: a refilled store can clear a short source,
	// never re-open one.
	d := seedDepthOf(bars1m, now)
	d.oneH = len(full1HRTH)
	if d.fourH > e.depth.fourH {
		e.depth.fourH = d.fourH
	}
	if d.oneM > e.depth.oneM {
		e.depth.oneM = d.oneM
	}
	if d.oneH > e.depth.oneH {
		e.depth.oneH = d.oneH
	}
	if d.today > e.depth.today {
		e.depth.today = d.today
	}
	if d.m15 > e.depth.m15 {
		e.depth.m15 = d.m15
	}
	if len(e.missing) > 0 {
		if m := missingFromDepth(e.depth); len(m) == 0 {
			e.missing = nil
			e.depthMet = depthMetLine
		} else {
			e.missing = m
		}
	}
	rep.After = e.reseedSnapshot()
	return rep
}

// SeedLine is the one boot/arm line: the seeded depth per source — read from
// the evaluator's OWN seeded depth (e.depth, the SAME numbers the fail-closed
// gate reads and Depths() serves), never recomputed from a caller-supplied bar
// series — plus the seeded trigger directions (F2: the lines are warmed from
// history now, so the line says what they are — not "none until the first
// tick"). contracts is the number of contracts stitched into the full history
// (0 omits the count; the trader passes len(contracts)).
func (e *Evaluator) SeedLine(contracts int) string {
	parts := []string{"mentor seed:"}
	parts = append(parts, fmt.Sprintf("4h EMA34 %d/%d", e.depth.fourH, FourHEMA34Warmup))
	if e.State.Swing.LineStale && len(e.State.Swing.HoledBuckets) > 0 {
		// m456: say it on the boot line, not only in the refusal counter.
		starts := make([]string, 0, len(e.State.Swing.HoledBuckets))
		for _, ms := range e.State.Swing.HoledBuckets {
			starts = append(starts, kernel.FormatCT(time.UnixMilli(ms)))
		}
		parts = append(parts, fmt.Sprintf("swing line STALE: no bars in open 4h bucket %s", strings.Join(starts, ", ")))
	}
	parts = append(parts, fmt.Sprintf("1m EMA34 %d/%d", e.depth.oneM, OneMEMA34Warmup))
	parts = append(parts, fmt.Sprintf("1H RTH levels %d candles", e.depth.oneH))
	// r22 (P3, "92 levels"): the count is READ from the seeded set split by the
	// 1H BODY-close deletion — the old "levels N" printed len(SeedLevels), which
	// INCLUDES deleted levels (10-13 08:49 boot: "levels 92" with 27 live).
	live, deleted := e.keyLevelCounts()
	parts = append(parts, fmt.Sprintf("key levels %d live · %d deleted", live, deleted))
	if contracts > 0 {
		parts = append(parts, fmt.Sprintf("contracts %d", contracts))
	}
	parts = append(parts, triggerLinePart("4h", e.State.HTF.FourH))
	parts = append(parts, triggerLinePart("1h", e.State.HTF.OneH))
	parts = append(parts, triggerLinePart("5m", e.State.Trigger))
	return strings.Join(parts, " ")
}

// keyLevelCounts splits the seeded key levels into live and deleted (a closed
// 1H candle's BODY closed through it — KEY-LEVEL ruling 5b). live + deleted ==
// len(SeedLevels).
func (e *Evaluator) keyLevelCounts() (live, deleted int) {
	for _, l := range e.State.SeedLevels {
		if e.State.DeletedLevels[l.Key] {
			deleted++
		} else {
			live++
		}
	}
	return live, deleted
}

// triggerLinePart renders one seeded trigger line for the boot line, every
// field READ from the seeded state (release #22 LINES-RESTART: the restart
// line prints the direction, the line price AND the "since" — the bucket that
// last moved it — for all three lines, so two boot lines can be compared
// without guessing): "<name> trigger <dir> @<price> since <bucket CT>". An
// unset direction is "none"; an unknown since/price prints n/a.
func triggerLinePart(name string, t TriggerLine) string {
	dir := string(t.Dir)
	if dir == "" {
		dir = "none"
	}
	px, since := "n/a", "n/a"
	if t.Dir != "" && t.Price != 0 {
		px = fmt.Sprintf("%.2f", t.Price)
	}
	if t.MovedAt > 0 {
		since = time.UnixMilli(t.MovedAt).In(ctime()).Format("2006-01-02T15:04")
	}
	return fmt.Sprintf("%s trigger %s @%s since %s", name, dir, px, since)
}

// depthMetLine is the ONE line logged when the seeded depth floors are met
// after boot (the evaluator stops refusing entries).
const depthMetLine = "mentor seed: depth met — entries allowed"

// advanceDepth moves every source's depth forward from the evaluator's own
// state as bars arrive, and clears e.missing once every source meets its
// warm-up. Called every Tick on a seeded evaluator (O(1) in the steady state).
// The floors are never lowered; a source only ever gains depth.
func (e *Evaluator) advanceDepth(bars []market.Kline, now int64) {
	// 4h: each observed 4h-bucket rollover closes one more 4h candle
	// (fourHClosedBuckets counts the buckets before the current one).
	if bs := fourHBucketStart(now, ctime()); bs > e.depth4hBucket {
		if e.depth4hBucket != 0 {
			e.depth.fourH++
		}
		e.depth4hBucket = bs
	}
	// 1H RTH level set: Seed1HBars grows as 1H candles close.
	if n := len(e.State.Seed1HBars); n > e.depth.oneH {
		e.depth.oneH = n
	}
	// today's session / closed 15m only need to be observed once.
	if e.depth.today == 0 && len(bars) > 0 && sessionKeyCT(bars[len(bars)-1].OpenTime) == sessionKeyCT(now) {
		e.depth.today = 1
	}
	if e.depth.m15 < Closed15mMin {
		if n := len(closedBucketsTF(bars, 15, now)); n > e.depth.m15 {
			e.depth.m15 = n
		}
	}
	// 1m: counted where the EMA consumes the bars (seededLevels).
	if len(e.missing) > 0 {
		if m := missingFromDepth(e.depth); len(m) == 0 {
			e.missing = nil
			e.depthMet = depthMetLine
		} else {
			e.missing = m
		}
	}
}

// Depths reports the live per-source depth under the names of SeedDepths /
// the injector's mentorDepthRequirements; nil when the evaluator is unseeded.
func (e *Evaluator) Depths() map[string]int {
	if !e.seeded {
		return nil
	}
	return map[string]int{
		"4h EMA34":      e.depth.fourH,
		"1m EMA34":      e.depth.oneM,
		"1h level set":  e.depth.oneH,
		"today session": e.depth.today,
		"closed 15m":    e.depth.m15,
	}
}

// TakeDepthMet returns the one-shot "depth met" line the first time the
// seeded floors are met after boot, then "" — the caller logs it once.
func (e *Evaluator) TakeDepthMet() string {
	l := e.depthMet
	e.depthMet = ""
	return l
}

// DepthMet returns the live depth line WITHOUT consuming it (read-only, for the
// dashboard's "what trades" panel). The tick's mentorRefreshDepths consumes it
// via TakeDepthMet; a read path must never steal that line.
func (e *Evaluator) DepthMet() string {
	return e.depthMet
}

// SourcesMissing reports the seeded-but-missing sources (nil when unseeded or
// complete).
func (e *Evaluator) SourcesMissing() []string {
	return e.missing
}

func fourHClosedBuckets(bars1h []market.Kline, now int64) []market.Kline {
	var out []market.Kline
	var cur *market.Kline
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	key := int64(0)
	first := true
	for _, b := range bars1h {
		k := fourHBucketStart(b.OpenTime, ctime())
		if first {
			key = k
			first = false
		}
		if k != key {
			flush()
			key = k
		}
		c := b
		cur = &c
	}
	flush()
	// Drop every bucket that is still open at now: the bucket containing now
	// (start equality) and any bucket whose recorded close lies at/past now.
	// A loop, not one drop — after removing a bucket past now, the new last
	// bucket is often the one CONTAINING now and must go too.
	for len(out) > 0 {
		last := out[len(out)-1]
		if last.CloseTime >= now ||
			fourHBucketStart(last.OpenTime, ctime()) == fourHBucketStart(now, ctime()) {
			out = out[:len(out)-1]
			continue
		}
		break
	}
	return out
}

// closedOnly returns the bars closed by now (CloseTime <= now — the same
// cut the seed's 1m EMA uses), in their original order.
func closedOnly(bars []market.Kline, now int64) []market.Kline {
	out := make([]market.Kline, 0, len(bars))
	for _, b := range bars {
		if b.CloseTime <= now {
			out = append(out, b)
		}
	}
	return out
}

func closedCount(bars []market.Kline, now int64) int {
	n := 0
	for _, b := range bars {
		if b.CloseTime <= now {
			n++
		}
	}
	return n
}

// closedBucketsTF aggregates to tfMin and drops the still-forming tail bucket.
// Shared by the X5-10 2m ISB read and the 15m/30m box reads.
//
// CONTRACT (FU-3): `now` must be an instant STRICTLY AFTER the newest closed
// 1m bar's close — in the Tick path that is BarCloseInstant(last) =
// last.CloseTime + 1 (see eval.go BarCloseInstant); at seed it is the wall
// clock (also after the newest closed bar). A bucket whose recorded close
// equals the newest closed bar's close is KEPT (its CloseTime < now); only a
// bucket whose close is still in the FUTURE is dropped. A caller passing
// last.CloseTime VERBATIM (no +1) would over-drop the just-completed bucket.
func closedBucketsTF(bars []market.Kline, tfMin int, now int64) []market.Kline {
	agg := barsTF(bars, tfMin)
	if len(agg) > 0 && agg[len(agg)-1].CloseTime >= now {
		agg = agg[:len(agg)-1]
	}
	return agg
}

// emaFromCloses computes the EMA over closes (period P: k = 2/(P+1)).
func emaFromCloses(closes []float64, period int) float64 {
	if len(closes) == 0 || period <= 0 {
		return 0
	}
	k := 2.0 / float64(period+1)
	ema := closes[0]
	for _, c := range closes[1:] {
		ema = ema + k*(c-ema)
	}
	return ema
}

// keyLevelsFromCandles runs the colour-change walk + prune over already
// bucketed 1H RTH candles (the same rules as KeyLevels, on a candle series).
func keyLevelsFromCandles(candles []market.Kline, prunePts float64) []Level {
	var cand []Level
	for i := 1; i < len(candles); i++ {
		if candleColour(candles[i-1]) == candleColour(candles[i]) {
			continue
		}
		cand = append(cand, Level{
			Key:    keyLevelKey(candles[i]),
			Kind:   KindKeyLevel,
			Price:  candles[i].Open,
			AtTime: candles[i].OpenTime,
		})
	}
	var kept []Level
	for i := len(cand) - 1; i >= 0; i-- {
		l := cand[i]
		drop := false
		for _, k := range kept {
			if abs(l.Price-k.Price) < prunePts {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, l)
		}
	}
	out := make([]Level, len(kept))
	for i, l := range kept {
		out[len(kept)-1-i] = l
	}
	return out
}

// keyLevelsAppend continues the colour-change walk as today's 1H candles close:
// a colour change draws a level at the candle OPEN; the prune then drops any
// older kept level within prunePts (newest-first, F7 semantics).
func keyLevelsAppend(levels []Level, candle market.Kline, lastColour bool, prunePts float64) ([]Level, bool) {
	colour := candleColour(candle)
	if colour != lastColour {
		lvl := Level{
			Key:    keyLevelKey(candle),
			Kind:   KindKeyLevel,
			Price:  candle.Open,
			AtTime: candle.OpenTime,
		}
		var kept []Level
		for _, old := range levels {
			if abs(old.Price-lvl.Price) >= prunePts {
				kept = append(kept, old)
			}
		}
		kept = append(kept, lvl)
		return kept, colour
	}
	return levels, lastColour
}

// missing4hPrefix names the one source that feeds ONLY the §8 swing line.
const missing4hPrefix = "bar history depth: 4h EMA34"

// missingScope classifies the missing sources. The 4h EMA 34 feeds only the
// swing line (State.Swing.Line); intraday setups read the 1m EMA 34 and the 1H
// level set. So a short 4h EMA blocks SWING4H entries only; every other
// missing source (1m EMA 34, 1H RTH levels, today's session, closed 15m)
// blocks ALL entries.
func missingScope(missing []string) (blockAll, blockSwing bool) {
	for _, m := range missing {
		if strings.HasPrefix(m, missing4hPrefix) {
			blockSwing = true
		} else {
			blockAll = true
		}
	}
	return blockAll, blockSwing
}

// scopedFailClosedFilter is failClosedFilter with the per-source scope: with
// any non-4h source missing every entry is dropped (seed_missing_source); with
// only the 4h EMA short, only SWING4H entries are dropped
// (seed_missing_4h_ema_warmup) and the intraday entries pass. Cancels always
// pass.
func scopedFailClosedFilter(out []Intent, missing []string) (kept []Intent, refusals []string) {
	blockAll, blockSwing := missingScope(missing)
	kept = out[:0]
	for _, in := range out {
		switch in.Action {
		case PlaceStopEntry, PlaceStopLimitEntry:
			if blockAll {
				refusals = append(refusals, "seed_missing_source")
				continue
			}
			if blockSwing && strings.EqualFold(in.Setup, "SWING4H") {
				refusals = append(refusals, "seed_missing_4h_ema_warmup")
				continue
			}
		}
		kept = append(kept, in)
	}
	return kept, refusals
}

// failClosedFilter drops every entry intent while any seeded source is missing
// (cancels survive — an open arm must stay closable). Unseeded evaluators never
// call it: legacy behaviour is untouched.
func failClosedFilter(out []Intent) (kept []Intent, refusals []string) {
	kept = out[:0]
	for _, in := range out {
		switch in.Action {
		case PlaceStopEntry, PlaceStopLimitEntry:
			refusals = append(refusals, "seed_missing_source")
			continue
		}
		kept = append(kept, in)
	}
	return kept, refusals
}
