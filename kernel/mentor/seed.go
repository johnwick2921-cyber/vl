package mentor

import (
	"fmt"
	"strings"
	"time"

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
	// Defensive copy: the 15m depth check filters in place over the caller's
	// backing array.
	bars1m = append([]market.Kline(nil), bars1m...)
	// P1 (CTO 12:44:08Z / 12:47:11Z): Seed takes 1m ONLY and builds 1h
	// (clock-aligned aggregation) and the 4h buckets (17:00 CT anchor) from it.
	// The level walk is RTH-filtered; the 4h EMA uses the all-hours aggregation.
	bars1h := keyLevel1HBars(bars1m)
	e.seeded = true
	e.depth = seedDepthOf(bars1m, now)
	e.depth4hBucket = fourHBucketStart(now, ctime())
	e.missing = missingFromDepth(e.depth)
	e.depthMet = ""

	// 1H RTH key levels from the FULL closed stored history (F7, no cap).
	candles1h := bars1h
	if n := len(candles1h); n > 0 && keyLevel1HCandleCloseTime(candles1h[n-1].OpenTime) > now {
		candles1h = candles1h[:n-1]
	}
	e.State.Seed1HBars = candles1h // the deletion check needs the FULL closed series
	e.State.SeedLevels = keyLevelsFromCandles(candles1h, e.Cfg.KeyLevelPrunePts)
	if n := len(candles1h); n > 0 {
		e.State.Seed1HWatermark = candles1h[n-1].OpenTime
		// The colour the incremental walk continues from — without it the first
		// extension compares against false and draws a phantom level whenever
		// the last seeded candle was green.
		e.State.Seed1HLastColour = candleColour(candles1h[n-1])
	}

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
	}

	// The swing's 4h EMA 34 line: the last 34 closed 4h candles (17:00 anchor).
	// The swing's 4h EMA line: the canonical recurrence over ALL closed 4h
	// candles (17:00 anchor), built from the ALL-HOURS 1h aggregation of the 1m
	// history (P1). The seed and every incremental step continue the SAME
	// recurrence, so seed+ticks == full rebuild exactly. 34 closed candles are
	// the warm-up MINIMUM (SeedMissing), not a cap — never re-window.
	if b4 := fourHClosedBuckets(barsTF(bars1m, 60), now); len(b4) > 0 {
		closes4 := make([]float64, 0, len(b4))
		for _, b := range b4 {
			closes4 = append(closes4, b.Close)
		}
		e.State.Swing.Line = emaFromCloses(closes4, e.Cfg.Swing.EMAPeriod)
		e.State.Swing.BucketStart = fourHBucketStart(now, ctime())
		e.State.Swing.EmaCount = len(closes4)
	}

	// S2: the swing must never replay OLD touches at boot. Stamp the newest
	// closed 5m bar as the watermark, so the first tick walks only NEW bars —
	// a 25h tape containing an old touch stays silent instead of turning the
	// old touch into a live order (00-METHOD.md §8: "Wait for a LITERAL
	// touch" [p2 @ 09:15] — the touch is observed live, never reconstructed).
	if b5 := closedBuckets(bars1m, now, e.Cfg); len(b5) > 0 {
		e.State.Swing.LastBarTime = b5[len(b5)-1].OpenTime
	}

	e.seedLine = SeedLine(e.State, bars1m, now)
	return e.missing
}

// SeedLine is the one boot/arm line: the seeded depth per source, n/a when
// unknown.
func SeedLine(s State, bars1m []market.Kline, now int64) string {
	parts := []string{"mentor seed:"}
	parts = append(parts, fmt.Sprintf("4h EMA34 %d/%d", len(fourHClosedBuckets(barsTF(bars1m, 60), now)), FourHEMA34Warmup))
	parts = append(parts, fmt.Sprintf("1m EMA34 %d/%d", closedCount(bars1m, now), OneMEMA34Warmup))
	parts = append(parts, fmt.Sprintf("1H RTH levels %d candles", len(keyLevel1HBars(bars1m))))
	parts = append(parts, fmt.Sprintf("levels %d", len(s.SeedLevels)))
	return strings.Join(parts, " ")
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
