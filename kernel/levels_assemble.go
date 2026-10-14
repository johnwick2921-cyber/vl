package kernel

import (
	"fmt"
	"math"
	"strings"
	"time"

	"vl/logger"
	"vl/market"
)

// P1.7 — assemble every detector → confluence scorer → the KEY LEVELS prompt
// block. One entry point the decision loop calls (mirror of FormatSVPLine +
// BuildSVPProfile). Pure: no LLM, no DB. Warms forward with the bar cache; the
// durable session-profile store (P1.3) feeds nPOC via extraLevels.

// BuildKeyLevelsBlock assembles the structural map from `bars`, scores it, and
// renders the executor prompt block. Returns "" when there is nothing to show
// (no closed bars / no in-band levels) so the caller injects nothing.
// proximityK is the resolved day-trade lock width (≤0 → spec constant 1.5).
func BuildKeyLevelsBlock(traderID string, bars []market.Kline, reg SessionRegistry, symbol string, maxLevels int, now time.Time, proximityK float64, extraLevels ...DetectedLevel) string {
	return BuildKeyLevelsBlockOpts(traderID, bars, reg, symbol, maxLevels, now, proximityK, false, "", extraLevels...)
}

// BuildKeyLevelsBlockOpts (grading audit §4.6/4.7 + 1h wave, 2026-08-25) —
// BuildKeyLevelsBlock with the seat_1h_zone guarantee and the minGrade floor
// applied. The executor KEY LEVELS block must obey the SAME rules as the
// planner table (one-sided tables and sub-min rows reached the executor before
// this).
func BuildKeyLevelsBlockOpts(traderID string, bars []market.Kline, reg SessionRegistry, symbol string, maxLevels int, now time.Time, proximityK float64, seat1HZone bool, minGrade string, extraLevels ...DetectedLevel) string {
	return BuildKeyLevelsBlockOptsBand(traderID, bars, reg, symbol, maxLevels, now, proximityK, seat1HZone, minGrade, 0, extraLevels...)
}

// BuildKeyLevelsBlockOptsBand is BuildKeyLevelsBlockOpts with an explicit
// confluence band (points); ≤0 → the default 0.10 × dATR.
func BuildKeyLevelsBlockOptsBand(traderID string, bars []market.Kline, reg SessionRegistry, symbol string, maxLevels int, now time.Time, proximityK float64, seat1HZone bool, minGrade string, confBandPts float64, extraLevels ...DetectedLevel) string {
	scored, price, _ := AssembleScoredLevelsMinGradeBand(traderID, bars, reg, symbol, maxLevels, nil, HTFScoreMultiplier, now, proximityK, minGrade, confBandPts, extraLevels...)
	if price <= 0 {
		return ""
	}
	if seat1HZone {
		scored = Seat1HZone(scored, maxLevels)
	}
	block := RenderKeyLevelsBlock(scored, price)

	// W3 (2026-09-09) — the MERGED map, the entry shortlist in reachability
	// order, and any projections beyond the mapped range, rendered BELOW the
	// existing table rather than replacing it: the seated table is what the
	// scorer produced and stays exactly as it was (the score is untouched this
	// wave), while the map block is what the owner's card shows.
	//
	// ATR5m is the same series the confirm/stop path uses (StaleConfirmATR5m);
	// when it cannot be computed the ATR column prints n/a rather than 0.
	atr5m := StaleConfirmATR5m(bars)
	cs := BuildMapWithProjections(scored, mapProjectionsFor(scored, bars, symbol, price, now), price, atr5m, MapCandidateOpts{})
	if mb := RenderMapBlock(cs, price); mb != "" {
		block += "\n" + mb
	}
	// The per-read counterpart of the D7 boot line: real numbers, COUNTED from
	// the map that was actually built (canon 35 — counters record, never infer).
	logger.Infof("%s", MapReadLine(CountMap(len(scored), cs)))
	return block
}

// mapProjectionsFor assembles D5's projected references for one read. Each
// producer returns nothing when its inputs are absent, so an unavailable source
// contributes no row rather than a fabricated one (A24).
func mapProjectionsFor(scored []ScoredLevel, bars []market.Kline, symbol string, price float64, now time.Time) []MapCandidate {
	if len(scored) == 0 {
		return nil
	}
	lo, hi := scored[0].Price, scored[0].Price
	for _, s := range scored {
		if s.Price < lo {
			lo = s.Price
		}
		if s.Price > hi {
			hi = s.Price
		}
	}
	var out []MapCandidate
	// (a) prior-week extremes from the DAILY source — never the 1m ring.
	out = append(out, ProjectPriorWeekExtremes(DailyBarsFor(symbol, MapDailyBarCount), now)...)
	// (b) round numbers beyond the mapped range.
	dATR := DailyRangeProxy(bars, now)
	if dATR > 0 {
		out = append(out, ProjectRoundNumbersBeyond(lo, hi, MapRoundNumberStep, dATR)...)
	}
	// (c) the ATR-projected session extreme, from the session's own open.
	if cb := closedBars(bars, now); len(cb) > 0 && dATR > 0 {
		out = append(out, ProjectSessionExtreme(cb[0].Open, dATR)...)
	}
	// (d) the measured move of the mapped range once price has broken it.
	out = append(out, ProjectMeasuredMove(lo, hi, price)...)
	return out
}

// AssembleScoredLevels runs every detector, scores them, and returns the graded
// TOP-N levels + the reference price + the daily-ATR used. Shared by the executor
// KEY LEVELS block (P1.7) and the planner input package (P3.3). Returns
// (nil, 0, 0) when there are no closed bars. proximityK is the resolved day-trade
// lock width (≤0 → spec constant 1.5) and threads into BOTH the round-number
// generator and the scorer — H1/H2: the config must govern which levels are
// generated AND which are seated, not just the activation-window paths.
func AssembleScoredLevels(traderID string, bars []market.Kline, reg SessionRegistry, symbol string, maxLevels int, now time.Time, proximityK float64, extraLevels ...DetectedLevel) (scored []ScoredLevel, price, dATR float64) {
	return AssembleScoredLevelsBand(traderID, bars, reg, symbol, maxLevels, now, proximityK, 0, extraLevels...)
}

// AssembleScoredLevelsBand is AssembleScoredLevels with an explicit confluence
// band (points); ≤0 → the default 0.10 × dATR.
func AssembleScoredLevelsBand(traderID string, bars []market.Kline, reg SessionRegistry, symbol string, maxLevels int, now time.Time, proximityK float64, confBandPts float64, extraLevels ...DetectedLevel) (scored []ScoredLevel, price, dATR float64) {
	cb := closedBars(bars, now)
	if len(cb) == 0 {
		return nil, 0, 0
	}
	price = cb[len(cb)-1].Close
	if price <= 0 {
		return nil, 0, 0
	}
	dATR = DailyRangeProxy(bars, now)
	if dATR <= 0 {
		dATR = 0.008 * price // fallback until the map warms
	}
	atr := market.ExportCalculateATR(cb, 14)
	if atr <= 0 {
		atr = dATR / 20
	}
	tick := market.FuturesTickSize(symbol)
	if tick <= 0 {
		tick = 0.25
	}
	tol := 3 * tick

	var all []DetectedLevel
	all = append(all, ExtractMultiDayLevels(bars, reg, now)...)
	all = append(all, RoundNumberLevels(price, dATR, proximityK)...)
	all = append(all, OpeningRangeLevels(bars, reg, now)...)
	all = append(all, GapLevels(bars, atr, 1.0, now)...)
	all = append(all, EqualHighsLows(bars, tol, now)...)
	all = append(all, SupplyDemandZones(bars, atr, now)...)
	all = append(all, FairValueGaps(bars, fvgMinGapPoints(symbol), now)...)
	all = append(all, OrderBlocks(bars, atr, now)...)
	all = append(all, VolumeLevels(bars, now)...) // Pack B (2026-08-26) — volume family
	// Level-truth wave (2026-08-27) — recent 5m/15m fractal swings (T3).
	all = append(all, SwingPointLevels(bars, now)...)
	all = append(all, extraLevels...) // nPOC etc. from the durable store (P1.3)
	CaptureIdentityContext(all, symbol, AISVPBarInterval)
	// S4 (mega-research 2026-08-26) — nPOC is emitted twice (in-kernel 120-bin
	// POC + store-fed SVP-row POC; prices can differ by >1pt). Dedupe on
	// (kind, price within 1 tick) BEFORE scoring so one POC = one seat.
	all = dedupeSameKind(all)

	// W11b — persisted level-state (freshness A→B→C, consumed) now surfaces: the
	// trader installs LevelStateProvider over store.LevelStateStore. Nil provider →
	// all-fresh (byte-identical to the pre-W11b output the goldens capture).
	scored = ScoreLevelsBand(all, price, dATR, levelFreshnessFn(traderID, symbol, now), maxLevels, proximityK, confBandPts)
	return scored, price, dATR
}

// AssembleScoredLevelsMinGrade (grading audit §4.6/4.7, 2026-08-25) is
// AssembleScoredLevels with a minGrade floor: the scorer runs on a 2× pool so
// a sub-floor row filtered OUT can be replaced by an in-band same-side
// candidate — seatBothSides re-balances AFTER the filter, so the minGrade cut
// can never leave the executor/planner table one-sided when candidates exist.
// Empty minGrade → byte-identical to AssembleScoredLevels.
func AssembleScoredLevelsMinGrade(traderID string, bars []market.Kline, reg SessionRegistry, symbol string, maxLevels int, htfSeats *int, htfMult float64, now time.Time, proximityK float64, minGrade string, extraLevels ...DetectedLevel) (scored []ScoredLevel, price, dATR float64) {
	return AssembleScoredLevelsMinGradeBand(traderID, bars, reg, symbol, maxLevels, htfSeats, htfMult, now, proximityK, minGrade, 0, extraLevels...)
}

// AssembleScoredLevelsMinGradeBand is AssembleScoredLevelsMinGrade with an
// explicit confluence band (points); ≤0 → the default 0.10 × dATR.
func AssembleScoredLevelsMinGradeBand(traderID string, bars []market.Kline, reg SessionRegistry, symbol string, maxLevels int, htfSeats *int, htfMult float64, now time.Time, proximityK float64, minGrade string, confBandPts float64, extraLevels ...DetectedLevel) (scored []ScoredLevel, price, dATR float64) {
	cb := closedBars(bars, now)
	if len(cb) == 0 {
		return nil, 0, 0
	}
	price = cb[len(cb)-1].Close
	if price <= 0 {
		return nil, 0, 0
	}
	dATR = DailyRangeProxy(bars, now)
	if dATR <= 0 {
		dATR = 0.008 * price // fallback until the map warms
	}
	atr := market.ExportCalculateATR(cb, 14)
	if atr <= 0 {
		atr = dATR / 20
	}
	tick := market.FuturesTickSize(symbol)
	if tick <= 0 {
		tick = 0.25
	}
	tol := 3 * tick

	var all []DetectedLevel
	all = append(all, ExtractMultiDayLevels(bars, reg, now)...)
	all = append(all, RoundNumberLevels(price, dATR, proximityK)...)
	all = append(all, OpeningRangeLevels(bars, reg, now)...)
	all = append(all, GapLevels(bars, atr, 1.0, now)...)
	all = append(all, EqualHighsLows(bars, tol, now)...)
	all = append(all, SupplyDemandZones(bars, atr, now)...)
	all = append(all, FairValueGaps(bars, fvgMinGapPoints(symbol), now)...)
	all = append(all, OrderBlocks(bars, atr, now)...)
	all = append(all, VolumeLevels(bars, now)...) // Pack B (2026-08-26) — volume family
	// Level-truth wave (2026-08-27) — recent 5m/15m fractal swings (T3).
	all = append(all, SwingPointLevels(bars, now)...)
	all = append(all, extraLevels...) // nPOC etc. from the durable store (P1.3)
	// S4 — same dedupe as AssembleScoredLevels (one POC = one seat).
	CaptureIdentityContext(all, symbol, AISVPBarInterval)
	all = dedupeSameKind(all)

	scored, _ = ScoreLevelsMinGradeFullSeats(all, price, dATR, levelFreshnessFn(traderID, symbol, now), maxLevels, proximityK, minGrade, htfSeats, htfMult, confBandPts)
	return scored, price, dATR
}

// AssembleScoredLevelsFullMinGrade (level-truth wave, 2026-08-27) is
// AssembleScoredLevelsMinGrade returning the graded PRE-SEAT pool alongside the
// seated result. The planner write site records EVERY pool grade into its
// machine-grade stamp map, so a level the model copies from the prompt that
// LOST the seat race (a far nPOC, a carried swing) still gets stamped — the
// stamp-gap regression fix (256/795 rows unstamped).
func AssembleScoredLevelsFullMinGrade(traderID string, bars []market.Kline, reg SessionRegistry, symbol string, maxLevels int, htfSeats *int, htfMult float64, now time.Time, proximityK float64, minGrade string, extraLevels ...DetectedLevel) (seated, pool []ScoredLevel, price, dATR float64) {
	return AssembleScoredLevelsFullMinGradeBand(traderID, bars, reg, symbol, maxLevels, htfSeats, htfMult, now, proximityK, minGrade, 0, extraLevels...)
}

// AssembleScoredLevelsFullMinGradeBand is AssembleScoredLevelsFullMinGrade with
// an explicit confluence band (points); ≤0 → the default 0.10 × dATR.
func AssembleScoredLevelsFullMinGradeBand(traderID string, bars []market.Kline, reg SessionRegistry, symbol string, maxLevels int, htfSeats *int, htfMult float64, now time.Time, proximityK float64, minGrade string, confBandPts float64, extraLevels ...DetectedLevel) (seated, pool []ScoredLevel, price, dATR float64) {
	seated, pool, price, dATR, _ = AssembleResearchLevelsBand(traderID, bars, reg, symbol, maxLevels, htfSeats, htfMult, now, proximityK, minGrade, confBandPts, extraLevels...)
	return
}

// AssembleResearchLevels returns the pre-deduplication universe as evidence;
// the trading outputs use the same detection, scoring and seating path.
func AssembleResearchLevels(traderID string, bars []market.Kline, reg SessionRegistry, symbol string, maxLevels int, htfSeats *int, htfMult float64, now time.Time, proximityK float64, minGrade string, extraLevels ...DetectedLevel) (seated, pool []ScoredLevel, price, dATR float64, raw []DetectedLevel) {
	return AssembleResearchLevelsBand(traderID, bars, reg, symbol, maxLevels, htfSeats, htfMult, now, proximityK, minGrade, 0, extraLevels...)
}

// AssembleResearchLevelsBand is AssembleResearchLevels with an explicit
// confluence band (points); ≤0 → the default 0.10 × dATR.
func AssembleResearchLevelsBand(traderID string, bars []market.Kline, reg SessionRegistry, symbol string, maxLevels int, htfSeats *int, htfMult float64, now time.Time, proximityK float64, minGrade string, confBandPts float64, extraLevels ...DetectedLevel) (seated, pool []ScoredLevel, price, dATR float64, raw []DetectedLevel) {
	cb := closedBars(bars, now)
	if len(cb) == 0 {
		return nil, nil, 0, 0, nil
	}
	price = cb[len(cb)-1].Close
	if price <= 0 {
		return nil, nil, 0, 0, nil
	}
	dATR = DailyRangeProxy(bars, now)
	if dATR <= 0 {
		dATR = 0.008 * price // fallback until the map warms
	}
	atr := market.ExportCalculateATR(cb, 14)
	if atr <= 0 {
		atr = dATR / 20
	}
	tick := market.FuturesTickSize(symbol)
	if tick <= 0 {
		tick = 0.25
	}
	tol := 3 * tick

	var all []DetectedLevel
	all = append(all, ExtractMultiDayLevels(bars, reg, now)...)
	all = append(all, RoundNumberLevels(price, dATR, proximityK)...)
	all = append(all, OpeningRangeLevels(bars, reg, now)...)
	all = append(all, GapLevels(bars, atr, 1.0, now)...)
	all = append(all, EqualHighsLows(bars, tol, now)...)
	all = append(all, SupplyDemandZones(bars, atr, now)...)
	all = append(all, FairValueGaps(bars, fvgMinGapPoints(symbol), now)...)
	all = append(all, OrderBlocks(bars, atr, now)...)
	all = append(all, VolumeLevels(bars, now)...) // Pack B (2026-08-26) — volume family
	// Level-truth wave (2026-08-27) — recent 5m/15m fractal swings (T3).
	all = append(all, SwingPointLevels(bars, now)...)
	all = append(all, extraLevels...) // nPOC etc. from the durable store (P1.3)
	CaptureIdentityContext(all, symbol, AISVPBarInterval)
	raw = researchLevels(all)
	all = dedupeSameKind(raw)

	seated, pool = ScoreLevelsMinGradeFullSeats(all, price, dATR, levelFreshnessFn(traderID, symbol, now), maxLevels, proximityK, minGrade, htfSeats, htfMult, confBandPts)
	return seated, pool, price, dATR, raw
}

// dedupeSameKind collapses same-kind duplicates within 1 MNQ tick (register
// S4, mega-research 2026-08-26: the dual nPOC emission paths could seat one
// POC twice). First occurrence wins (deterministic — detector order, with
// store-fed extras appended last).
func dedupeSameKind(levels []DetectedLevel) []DetectedLevel {
	const dedupeTick = 0.25 // MNQ tick (the same constant Tier1ProximityTicks cites)
	out := make([]DetectedLevel, 0, len(levels))
	for _, l := range levels {
		dup := false
		for _, o := range out {
			// D2 — TIMEFRAME IS IDENTITY. The key is (kind, tf, price±tick), not
			// (kind, price±tick). A 1h order block and a 1d order block at one
			// price are two references that a trader reads differently, and
			// before this the second one vanished into the first with no record
			// — the survivor kept its own timeframe and the loser's existence
			// was simply lost. Cross-timeframe coincidence is a MERGE input
			// (D3), never a dedupe casualty. Same kind AND same timeframe still
			// collapses, which is what this dedupe was added for (register S4:
			// the dual nPOC paths could seat one POC twice).
			if o.Kind == l.Kind && o.TF == l.TF && math.Abs(o.Price-l.Price) <= dedupeTick {
				researchCut(l, fmt.Sprintf("same-kind duplicate of %s %.2f [%s]", o.Kind, o.Price, o.Label))
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, l)
		}
	}
	return out
}

// DetectHTFLevels (G2/G3, 2026-08-24) — per-TF swing/zone detection on the
// CONFIGURED higher timeframes, so real 1h/4h support/resistance enters the
// candidate pool (before this, every detector ran on the 1m slice only). Every
// output carries HTF=true: the scorer's ×1.2 multiplier and the P0.1
// standalone-zone rule then apply. TFs below 15m and "D" are skipped —
// intraday noise adds nothing, and the daily anchors are already covered by
// ExtractMultiDayLevels. Cluster tolerance scales with the TF's own ATR
// (3 ticks is meaningless on a 1h bar).
// HTFDetectionReport is what the per-timeframe pass RECORDS about itself, so
// the boot line and the observability log READ the detection rather than
// recomputing it (A11: a boot line is read, never literal). Counts and Skipped
// are disjoint: a timeframe appears in exactly one of them, so "produced
// nothing" and "was never read" stop being the same observation from outside.
type HTFDetectionReport struct {
	Requested []string          // the timeframes asked for, in order
	Counts    map[string]int    // tf -> levels emitted (0 is a real, computed zero)
	Skipped   map[string]string // tf -> why nothing was read from it
	Resolved  map[string]string // tf -> the volatility-derived params it actually used
}

func newHTFDetectionReport(timeframes []string) *HTFDetectionReport {
	return &HTFDetectionReport{
		Requested: append([]string(nil), timeframes...),
		Counts:    map[string]int{},
		Skipped:   map[string]string{},
		Resolved:  map[string]string{},
	}
}

// TFsWithLevels returns the timeframes that emitted at least one level, in the
// order they were requested — the boot line's `by tf` list.
func (r *HTFDetectionReport) TFsWithLevels() []string {
	if r == nil {
		return nil
	}
	var out []string
	for _, tf := range r.Requested {
		if r.Counts[tf] > 0 {
			out = append(out, tf)
		}
	}
	return out
}

// DetectHTFLevelsReport is DetectHTFLevels plus the record of what each
// timeframe did. DetectHTFLevels remains the level-only entry point so existing
// call sites are untouched.
func DetectHTFLevelsReport(fetch func(tf string, count int) []market.Kline, timeframes []string, symbol string, now time.Time) ([]DetectedLevel, *HTFDetectionReport) {
	return detectHTFLevels(fetch, timeframes, symbol, now)
}

func DetectHTFLevels(fetch func(tf string, count int) []market.Kline, timeframes []string, symbol string, now time.Time) []DetectedLevel {
	levels, _ := detectHTFLevels(fetch, timeframes, symbol, now)
	return levels
}

// DetectHTFLevelsExport is detectHTFLevels exported for read-only replay
// harnesses (S2/S4 measurement gate, 2026-09-16): the same detector the live
// path calls, over caller-supplied bars. No production path uses this entry.
func DetectHTFLevelsExport(fetch func(tf string, count int) []market.Kline, timeframes []string, symbol string, now time.Time) ([]DetectedLevel, *HTFDetectionReport) {
	return detectHTFLevels(fetch, timeframes, symbol, now)
}

func detectHTFLevels(fetch func(tf string, count int) []market.Kline, timeframes []string, symbol string, now time.Time) ([]DetectedLevel, *HTFDetectionReport) {
	rep := newHTFDetectionReport(timeframes)
	if fetch == nil || len(timeframes) == 0 {
		return nil, rep
	}
	tick := market.FuturesTickSize(symbol)
	if tick <= 0 {
		tick = 0.25
	}
	seen := map[string]bool{}
	var out []DetectedLevel
	for _, rawTF := range timeframes {
		tf := canonicalDetectionTF(rawTF)
		if tf == "" {
			continue
		}
		if seen[tf] {
			rep.Skipped[tf] = "requested more than once"
			continue
		}
		if !isHTFDetectionTF(tf) {
			// Not a silent drop: a timeframe outside the detection set is a
			// DECISION (sub-15m is intraday noise; the set is isHTFDetectionTF),
			// and a lane asking why 5m has no zones deserves the reason rather
			// than an empty list.
			rep.Skipped[tf] = "outside the HTF detection set"
			continue
		}
		seen[tf] = true
		cb := closedBars(fetch(tf, htfDetectionFetchBars), now)
		if len(cb) < htfMinClosedBars {
			// D1 — a timeframe with too few bars for the definition emits
			// NOTHING and says so. A level built from a partial window is worse
			// than no level: it looks identical to one built from a full one.
			rep.Skipped[tf] = fmt.Sprintf("only %d closed bars, need %d for the definition", len(cb), htfMinClosedBars)
			continue
		}
		// D1 — parameters stated in BARS stay in bars (the pivot width k, the
		// fetch depth); parameters stated in VOLATILITY are resolved against
		// THIS timeframe's own ATR and recorded, because 3 ticks means something
		// different on a 1w bar than on a 15m one.
		atr := market.ExportCalculateATR(cb, 14)
		if atr <= 0 {
			atr = 0.002 * cb[len(cb)-1].Close
		}
		tol := 3 * tick
		if alt := 0.15 * atr; alt > tol {
			tol = alt
		}
		rep.Resolved[tf] = fmt.Sprintf("bars=%d atr=%.2f tol=%.2f span=%s", len(cb), atr, tol, htfWindowSpan(cb))
		lookback := len(cb)
		before := len(out)
		for _, d := range htfDetectors {
			for _, l := range d.Run(cb, atr, tol, now) {
				out = append(out, tagHTFLevel(l, tf, lookback))
			}
		}
		// A computed zero, recorded as one — distinct from Skipped above.
		rep.Counts[tf] = len(out) - before
	}
	return out, rep
}

// htfDetectionFetchBars is the per-timeframe fetch depth. Stated in BARS (12e:
// a parameter in bars stays in bars across timeframes), so 1w reads 500 weeks
// where 15m reads 500 quarter-hours and the definition is unchanged.
const htfDetectionFetchBars = 500

// htfMinClosedBars is the floor every detector in this pass needs. It matches
// the strictest guard among them — EqualHighsLows returns nil below 5 closed
// bars — and exists so the LOOP can say why a timeframe emitted nothing instead
// of each detector silently returning an empty slice.
const htfMinClosedBars = 5

// htfWindowSpan describes the lookback window in calendar terms, so D4's three
// distinct facts (formation timeframe, lookback window, age at read) are each
// recorded rather than inferred from one another.
func htfWindowSpan(cb []market.Kline) string {
	if len(cb) == 0 {
		return "n/a"
	}
	first := time.UnixMilli(cb[0].OpenTime).In(chicago())
	last := time.UnixMilli(cb[len(cb)-1].OpenTime).In(chicago())
	return fmt.Sprintf("%s→%s", first.Format("2006-01-02"), last.Format("2006-01-02"))
}

// TFBootLine is W-TF's boot posture. Every field is READ from its resolver; the
// per-read counts are n/a because no detection has run yet, exactly as W3's map
// line does (A11: a field the process cannot know yet prints n/a, never a
// plausible zero).
//
// `planner tfs` is LABELLED per-trader rather than printed, because this process
// serves several traders and each reads its own planner_timeframes — the same
// reason the map line labels `cap` per-trader instead of naming one value.
func TFBootLine(defaultSet []string, detectors int) string {
	return fmt.Sprintf(
		"🗺 tf: detection-set=[%s] · detectors=%d (%s) · detectors×tfs=%d · "+
			"planner tfs=per-trader (planner_timeframes; D→1d) · "+
			"levels=n/a (by tf: n/a) · cross-tf merged=n/a · "+
			"htf-weight=%.1f(const, no strategy loaded) · daily/weekly tier=4h[I]",
		strings.Join(defaultSet, ","), detectors, strings.Join(HTFDetectorNames(), ","),
		detectors*len(defaultSet), HTFScoreMultiplier)
}

// TFReadLine is the per-read counterpart: the same fields with the numbers the
// detection pass actually produced. Skipped timeframes are printed with their
// REASON, so "1w found nothing" and "1w was never read" stay distinguishable on
// the one line an operator looks at.
func TFReadLine(rep *HTFDetectionReport, htfMult float64) string {
	if rep == nil {
		return "🗺 tf: no detection report"
	}
	total := 0
	var byTF []string
	for _, tf := range rep.Requested {
		tf = canonicalDetectionTF(tf)
		if n, ok := rep.Counts[tf]; ok {
			total += n
			byTF = append(byTF, fmt.Sprintf("%s=%d", tf, n))
		}
	}
	var skipped []string
	for _, tf := range rep.Requested {
		tf = canonicalDetectionTF(tf)
		if why, ok := rep.Skipped[tf]; ok {
			skipped = append(skipped, fmt.Sprintf("%s(%s)", tf, why))
		}
	}
	skip := "none"
	if len(skipped) > 0 {
		skip = strings.Join(skipped, " ")
	}
	list := "none"
	if len(byTF) > 0 {
		list = strings.Join(byTF, " ")
	}
	return fmt.Sprintf("🗺 tf read: levels=%d (by tf: %s) · skipped=%s · htf-weight=%.1f",
		total, list, skip, htfMult)
}

// htfDetectors is the per-timeframe detector set, as a table rather than four
// inline loops, so "how many detectors run per timeframe" is a value the boot
// line can READ instead of a number someone types next to it (A11/A24). The
// DEFINITIONS are untouched — this wave changes which timeframes they run on,
// never what any of them looks for (A31).
//
// Every detector receives the SAME family definition on every timeframe (round
// 12, 12e: hold the family definition constant initially). What differs per
// timeframe is only the volatility-derived tolerance, resolved from that
// timeframe's own ATR and recorded in the report.
var htfDetectors = []struct {
	Name string
	Run  func(cb []market.Kline, atr, tol float64, now time.Time) []DetectedLevel
}{
	{"equal-highs-lows", func(cb []market.Kline, _, tol float64, now time.Time) []DetectedLevel {
		return EqualHighsLows(cb, tol, now)
	}},
	{"supply-demand", func(cb []market.Kline, atr, _ float64, now time.Time) []DetectedLevel {
		return SupplyDemandZones(cb, atr, now)
	}},
	{"fair-value-gaps", func(cb []market.Kline, atr, _ float64, now time.Time) []DetectedLevel {
		return FairValueGaps(cb, atr, now)
	}},
	{"order-blocks", func(cb []market.Kline, atr, _ float64, now time.Time) []DetectedLevel {
		return OrderBlocks(cb, atr, now)
	}},
}

// HTFDetectorCount is what the boot line reads.
func HTFDetectorCount() int { return len(htfDetectors) }

// HTFDetectorNames lists the detector families, for the Guide and the report.
func HTFDetectorNames() []string {
	out := make([]string, 0, len(htfDetectors))
	for _, d := range htfDetectors {
		out = append(out, d.Name)
	}
	return out
}

// canonicalDetectionTF resolves a CONFIGURED timeframe string to the one the
// store and every tier table speak.
//
// This is the join that was missing. The bound strategy's planner_timeframes
// has read ["D","4h","1h","15m","5m"] since the default was written
// (store/strategy.go:1407) — the owner's configuration has been ASKING for
// daily structure all along. The detection gate only ever knew "1d", so "D"
// fell through the `!isHTFDetectionTF(tf)` branch and was dropped in silence,
// which is why 0 of 297 stored plans carry a daily level while the config names
// one first.
//
// Canonicalising HERE, where the value enters detection, follows the repo's
// canonical-casing law (checklist 28: one canonicaliser per identifier, called
// where the value ENTERS). The alias set is deliberately small — only the forms
// the day-plan config actually uses.
func canonicalDetectionTF(tf string) string {
	t := strings.ToLower(strings.TrimSpace(tf))
	switch t {
	case "d", "1day", "daily":
		return "1d"
	case "w", "1week", "weekly":
		return "1w"
	}
	return t
}

// isHTFDetectionTF lists the timeframes DetectHTFLevels runs on.
//
// W-TF (2026-09-10, owner ruling): the daily family — 1d, 3d, 1w — joins the
// set. Before this, the gate stopped at 12h on the reasoning that "the daily
// anchors are already covered by ExtractMultiDayLevels"; that covers PDH/PDL
// and the prior-week extremes, not a daily SWING, ZONE, ORDER BLOCK or FVG.
// Measured on 2026-09-10: 0 of 297 stored plans carried a level from any daily
// timeframe, while the store holds 1d n=1902 back to 2019-05-02 and 1w n=384
// back to 2019-04-26.
//
// Sub-15m stays OUT and the original reason stands: intraday noise adds nothing
// to higher-timeframe structure, and base swing detection already serves 5m and
// 15m. Widening this set downward is a decision, not an omission — TestE1c pins
// it so the next lane has to mean it.
func isHTFDetectionTF(tf string) bool {
	for _, t := range HTFDetectionTFs {
		if t == tf {
			return true
		}
	}
	return false
}

// HTFDetectionTFs is the ORDERED single source for which timeframes the
// per-timeframe pass runs on. The membership test above ranges over it and every
// caller that needs a default set reads it, so a timeframe added here reaches
// the gate and every call site in the same second.
//
// This shape is deliberate. Checklist class 107: centralising a hand-typed list
// is a behaviour change wherever the lists differed, and the remedy is a single
// SOURCE, never a second list beside the predicate — a []string sitting next to
// a hardcoded switch diverges exactly as readily as a switch and a SQL literal.
var HTFDetectionTFs = []string{"15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w"}

// DefaultHTFDetectionTFs is the set a caller uses when it has no configured
// list of its own — one rung per scale rather than all eleven, because a caller
// on a per-decision path pays a fetch and four detector passes per timeframe.
// Callers WITH a configured list (the planner reads planner_timeframes) pass
// theirs; this is not a competing default for them.
var DefaultHTFDetectionTFs = []string{"15m", "1h", "4h", "1d", "1w"}

// tagHTFLevel marks a detected level with its HTF origin + a TF-suffixed label
// ("EQH·1h", "Demand·4h") so the ranked table and the card show provenance.
// Also sets the structured TF field the v3 zone tiers grade on.
func tagHTFLevel(l DetectedLevel, tf string, lookbackBars int) DetectedLevel {
	l.HTF = true
	l.TF = tf
	l.LookbackBars = lookbackBars
	l.Label = l.Label + "·" + tf
	return l
}

// DailyRangeProxy estimates the typical daily range from intraday bars by
// averaging each COMPLETED CME session-day's range (the developing day is
// skipped). 0 when no completed day is present — the caller falls back.
// Improves as the cache / durable store warms forward.
// S1 (mega-research 2026-08-26) — RENAMED from DailyATRProxy: this is a
// session-day H−L range mean, NOT an ATR. The name now says so.
func DailyRangeProxy(bars []market.Kline, now time.Time) float64 {
	cb := closedBars(bars, now)
	if len(cb) == 0 {
		return 0
	}
	type dayRange struct{ hi, lo float64 }
	days := map[string]*dayRange{}
	for _, b := range cb {
		key := CMESessionDayKey(time.UnixMilli(b.OpenTime))
		d := days[key]
		if d == nil {
			d = &dayRange{hi: math.Inf(-1), lo: math.Inf(1)}
			days[key] = d
		}
		d.hi = math.Max(d.hi, b.High)
		d.lo = math.Min(d.lo, b.Low)
	}
	nowFut := CMESessionDayKey(now)
	var sum float64
	var n int
	for key, d := range days {
		if key == nowFut {
			continue // developing day is not a completed range
		}
		if d.hi > d.lo {
			sum += d.hi - d.lo
			n++
		}
	}
	if n > 0 {
		return sum / float64(n)
	}
	// Fallback: the developing day's range so far.
	if d := days[nowFut]; d != nil && d.hi > d.lo {
		return d.hi - d.lo
	}
	return 0
}
