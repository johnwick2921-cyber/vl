package kernel

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"vl/market"
)

// FVG ENTRY MODEL (dispatch 2026-08-26) — FVG becomes a first-class PLAY, not
// just a level type. Pure math end-to-end: displacement off a Tier-1 anchor
// leaves a fresh gap → retrace INTO the gap → invalid on close through the
// DISTAL edge → targets at the next liquidity. Every element is machine-
// computable and ADVISORY: the executor AI remains the judge (zero new gates).

// ── env thresholds (canon: no literals; citation per number) ────────────────

// FvgEntryMinDispATR is the minimum displacement-body size in 5m Wilder
// ATR(14) multiples (env FVG_ENTRY_MIN_DISP_ATR, default 1.5). Citation: the
// MSS/displacement research — impulses below ~1.5× the 5m ATR are noise
// legs, not initiative displacement; the entry-model spec pins the same floor.
func FvgEntryMinDispATR() float64 {
	if v := os.Getenv("FVG_ENTRY_MIN_DISP_ATR"); v != "" {
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && n > 0 {
			return n
		}
	}
	return 1.5
}

// FvgEntryCEWidthPts is the gap width above which the CE (midpoint) entry mode
// applies instead of the proximal edge (env FVG_CE_WIDTH_PTS, default 20).
// Citation: NQ gap sweet spot 20–80 pts — below 20 pts the gap is a thin edge
// zone (edge entry), above it the midpoint offers the higher-probability
// fill (1h+ fill rate ~70–80%).
func FvgEntryCEWidthPts() float64 {
	if v := os.Getenv("FVG_CE_WIDTH_PTS"); v != "" {
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && n > 0 {
			return n
		}
	}
	return 20
}

// FvgCe is the midpoint entry of a gap (computed, never model-written).
func FvgCe(lo, hi float64) float64 { return (lo + hi) / 2 }

// FreshFvg is ONE machine-verified candidate gap the planner may author an
// fvg_entry scenario from (level-truth wave b2, 2026-08-27). The write-site
// validator was rejecting model-declared gaps because the model was BLIND —
// it never saw the real candidates. This list is the cure.
type FreshFvg struct {
	Direction string  `json:"direction"` // long | short
	Lo        float64 `json:"lo"`
	Hi        float64 `json:"hi"`
	AgeBars   int     `json:"age_bars"` // bars since the gap's newest candle
	DispATR   float64 `json:"disp_atr"` // impulse body in 5m Wilder ATR(14)
}

// FreshFvgCandidates scans the closed bars with the SAME 3-candle gap test the
// write-site validator uses (bullish: low[i] > high[i+2] mirrored; session-break
// guard; gap floor ≥ max(2×tick, noise floor); displacement body ≥
// FvgEntryMinDispATR × ATR14(5m)) and returns every qualifying gap within
// FvgEntryLookbackBars, newest first. Empty → the planner is told to author NO
// fvg_entry scenario.
func FreshFvgCandidates(bars []market.Kline, symbol string, now time.Time) []FreshFvg {
	cb := closedBars(bars, now)
	if len(cb) < 3 {
		return nil
	}
	tick := market.FuturesTickSize(symbol)
	if tick <= 0 {
		tick = 0.25
	}
	fiveM := AggregateBars(cb, 5*60_000)
	atr5 := 0.0
	if len(fiveM) >= 15 {
		atr5 = market.ExportCalculateATR(fiveM, 14)
	}
	lookback := len(cb)
	if lookback > FvgEntryLookbackBars() {
		lookback = FvgEntryLookbackBars()
	}
	floor := math.Max(2*tick, fvgMinGapPoints(symbol))
	var out []FreshFvg
	for i := len(cb) - 3; i >= 0 && i >= len(cb)-lookback-3+1; i-- {
		if !fvgWindowContiguous(cb, i+2) {
			continue
		}
		// newest candle = cb[i+2] (the impulse); gap measured between cb[i]
		// (oldest of the three) and cb[i+2].
		var dir string
		var gapLo, gapHi float64
		if cb[i+2].Low > cb[i].High {
			dir, gapLo, gapHi = "long", cb[i].High, cb[i+2].Low
		} else if cb[i+2].High < cb[i].Low {
			dir, gapLo, gapHi = "short", cb[i+2].High, cb[i].Low
		} else {
			continue
		}
		if gapHi-gapLo < floor {
			continue
		}
		impulse := math.Abs(cb[i+2].Close - cb[i+2].Open)
		dispATR := 0.0
		if atr5 > 0 {
			dispATR = impulse / atr5
			if dispATR < FvgEntryMinDispATR() {
				continue
			}
		}
		out = append(out, FreshFvg{
			Direction: dir, Lo: gapLo, Hi: gapHi,
			AgeBars: len(cb) - 1 - (i + 2), DispATR: round2(dispATR),
		})
	}
	return out
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// HasFvgScenario reports whether the plan carries an fvg_entry scenario
// (the write site gates the bar re-verification on this).
func HasFvgScenario(d *PlanDoc) bool {
	if d == nil {
		return false
	}
	for _, s := range d.Scenarios {
		if strings.EqualFold(strings.TrimSpace(s.Condition), "fvg_entry") {
			return true
		}
	}
	return false
}

// ── write-time validation ───────────────────────────────────────────────────

// ValidateFvgEntryScenarios re-verifies every fvg_entry scenario against the
// STORED 1m bars (the planner may hallucinate or cite a stale gap). Checks,
// each with a citation comment:
//  1. the 3-candle relation (bullish: low[i] > high[i+2]; bearish mirrored)
//     exists within the last FvgEntryLookbackBars and matches the declared
//     gap within 2 ticks;
//  2. gap size ≥ max(2×tick, the existing FVG gap floor) — reuses
//     fvgMinGapPoints;
//  3. displacement body ≥ FvgEntryMinDispATR × Wilder ATR14(5m) (5m bars
//     aggregated from the 1m series);
//  4. origin_level exists in the seated table or the HTF section (labels);
//  5. entry_mode=ce requires the gap wider than FvgEntryCEWidthPts.
//
// Failure → error → the existing planner retry loop consumes it (fail-closed).
func ValidateFvgEntryScenarios(d *PlanDoc, bars []market.Kline, symbol string, originLabels map[string]bool, now time.Time) error {
	if d == nil {
		return nil
	}
	var fvgCount int
	for _, s := range d.Scenarios {
		if !strings.EqualFold(strings.TrimSpace(s.Condition), "fvg_entry") {
			continue
		}
		fvgCount++
		if s.Fvg == nil {
			return fmt.Errorf("scenario %s declares condition fvg_entry but has no fvg{} object", s.ID)
		}
		if err := validateOneFvgEntry(s, bars, symbol, originLabels, now); err != nil {
			return err
		}
	}
	_ = fvgCount
	return nil
}

func validateOneFvgEntry(s PlanScenario, bars []market.Kline, symbol string, originLabels map[string]bool, now time.Time) error {
	f := s.Fvg
	if f.Lo <= 0 || f.Hi <= 0 || f.Hi <= f.Lo {
		return fmt.Errorf("scenario %s fvg: lo/hi invalid (%.2f–%.2f)", s.ID, f.Lo, f.Hi)
	}
	tick := market.FuturesTickSize(symbol)
	if tick <= 0 {
		tick = 0.25
	}
	// (5) entry_mode consistency: ce only for wide gaps.
	if strings.EqualFold(f.EntryMode, "ce") && f.Hi-f.Lo <= FvgEntryCEWidthPts() {
		return fmt.Errorf("scenario %s fvg: entry_mode=ce requires a gap wider than %.0f pts (declared %.1f) — use edge",
			s.ID, FvgEntryCEWidthPts(), f.Hi-f.Lo)
	}
	if f.EntryMode != "edge" && f.EntryMode != "ce" {
		return fmt.Errorf("scenario %s fvg: entry_mode must be edge|ce, got %q", s.ID, f.EntryMode)
	}
	if strings.ToLower(f.Direction) != strings.ToLower(s.Direction) {
		return fmt.Errorf("scenario %s fvg: fvg.direction %q disagrees with scenario direction %q", s.ID, f.Direction, s.Direction)
	}
	// (1)+(2)+(3) — the 3-candle relation must exist RECENTLY and match.
	cb := closedBars(bars, now)
	if len(cb) < 3 {
		return fmt.Errorf("scenario %s fvg: no closed bars to verify the gap", s.ID)
	}
	tol := 2 * tick
	lookback := len(cb)
	if lookback > FvgEntryLookbackBars() {
		lookback = FvgEntryLookbackBars()
	}
	found := false
	var impulse float64
	for i := len(cb) - 3; i >= 0 && i >= len(cb)-lookback-3+1; i-- {
		// A6 — session-break guard (same rule as the detector): a triple that
		// straddles the halt/weekend is a phantom gap, never a valid entry.
		if !fvgWindowContiguous(cb, i+2) {
			continue
		}
		var gapLo, gapHi float64
		// Dispatch convention: index 0 = NEWEST candle, 2 = oldest.
		// bullish: low[0] > high[2] → the newest candle gapped UP away from the
		// two older ones (gap below); bearish mirrored. The impulse candle is
		// the NEWEST (cb[i+2]) — the one that displaced.
		if strings.EqualFold(s.Direction, "long") {
			if cb[i+2].Low <= cb[i].High {
				continue
			}
			gapLo, gapHi = cb[i].High, cb[i+2].Low
		} else {
			if cb[i+2].High >= cb[i].Low {
				continue
			}
			gapLo, gapHi = cb[i+2].High, cb[i].Low
		}
		if math.Abs(gapLo-f.Lo) > tol || math.Abs(gapHi-f.Hi) > tol {
			continue
		}
		// (2) gap floor: max(2×tick, the existing FVG noise floor).
		if gapHi-gapLo < math.Max(2*tick, fvgMinGapPoints(symbol)) {
			continue
		}
		// (3) displacement body (the impulse candle that left the gap).
		impulse = math.Abs(cb[i+2].Close - cb[i+2].Open)
		found = true
		break
	}
	if !found {
		return fmt.Errorf("scenario %s fvg: no fresh 3-candle gap %s matches the declared %.2f–%.2f (fake/stale gap)",
			s.ID, strings.ToLower(s.Direction), f.Lo, f.Hi)
	}
	// Displacement floor in 5m Wilder ATR(14) — aggregate 5m closes from 1m.
	fiveM := AggregateBars(cb, 5*60_000)
	if len(fiveM) < 15 {
		return fmt.Errorf("scenario %s fvg: insufficient 5m history for the displacement ATR check", s.ID)
	}
	atr5 := market.ExportCalculateATR(fiveM, 14)
	if atr5 <= 0 {
		return fmt.Errorf("scenario %s fvg: 5m ATR unavailable", s.ID)
	}
	if impulse < FvgEntryMinDispATR()*atr5 {
		return fmt.Errorf("scenario %s fvg: displacement body %.1f < %.1f×ATR5m (%.1f) — weak impulse, not a displacement",
			s.ID, impulse, FvgEntryMinDispATR(), atr5)
	}
	if f.DisplacementATR > 0 && math.Abs(f.DisplacementATR-impulse/atr5) > 0.75 {
		return fmt.Errorf("scenario %s fvg: declared displacement_atr %.2f disagrees with the computed %.2f",
			s.ID, f.DisplacementATR, impulse/atr5)
	}
	// (4) origin_level in the seated table or HTF section.
	if len(originLabels) > 0 && !originLabels[f.OriginLevel] {
		return fmt.Errorf("scenario %s fvg: origin_level %q is not in the seated table or the HTF section", s.ID, f.OriginLevel)
	}
	// CE recompute: the midpoint is computed, never trusted from the model.
	ce := FvgCe(f.Lo, f.Hi)
	if math.Abs(f.CE-ce) > 0.51 {
		return fmt.Errorf("scenario %s fvg: declared ce %.2f disagrees with the computed midpoint %.2f", s.ID, f.CE, ce)
	}
	return nil
}

// FvgEntryLookbackBars bounds the gap search window (env, default 40 1m bars):
// a gap older than this is STALE — the freshness ladder applies to the gap as
// a zone, so a long-gone impulse is not an entry.
func FvgEntryLookbackBars() int {
	if v := os.Getenv("FVG_ENTRY_LOOKBACK_BARS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 40
}

// AggregateBars buckets a 1m series into bars of `bucketMs` width (open of the
// first, high/low, close of the last, summed volume). Used for the 5m ATR leg.
// Epoch-floored: a bar's bucket is OpenTime / bucketMs * bucketMs.
func AggregateBars(bars []market.Kline, bucketMs int64) []market.Kline {
	if bucketMs <= 0 {
		return nil
	}
	return aggregateBarsBy(bars, bucketMs, func(openMs int64) int64 {
		return openMs / bucketMs * bucketMs
	})
}

// aggregateBarsBy is the ONE aggregation loop, shared by AggregateBars (epoch
// floor) and Aggregate4HCT (17:00 CT anchor) — so the 4h anchor is a bucket-key
// function, never a second copy of the OHLC merge.
func aggregateBarsBy(bars []market.Kline, bucketMs int64, key func(openMs int64) int64) []market.Kline {
	if len(bars) == 0 {
		return nil
	}
	var out []market.Kline
	for _, b := range bars {
		bucket := key(b.OpenTime)
		if len(out) == 0 || out[len(out)-1].OpenTime != bucket {
			out = append(out, market.Kline{OpenTime: bucket, Open: b.Open, High: b.High, Low: b.Low, Close: b.Close, Volume: b.Volume})
			continue
		}
		last := &out[len(out)-1]
		if b.High > last.High {
			last.High = b.High
		}
		if b.Low < last.Low {
			last.Low = b.Low
		}
		last.Close = b.Close
		last.Volume += b.Volume
	}
	return out
}

// ── live confirm state (executor advisory + card) ───────────────────────────

// FvgEntryVerdict is the per-cycle machine state of one fvg_entry scenario.
type FvgEntryVerdict struct {
	State       string // IN_ZONE | ABOVE | BELOW | FILLED_INVALID
	TouchNumber int    // 1st/2nd/3rd+ retrace into the gap since plan birth
	Met         bool   // in-zone per entry_mode (edge: in zone; ce: near CE)
	Detail      string
}

// EvaluateFvgEntry computes the live state. The gap IS the band (reuses the
// touch/confirm machinery semantics): the distal edge is the invalidation
// side (long → lo, short → hi); a CLOSE through the distal edge on the
// decision TF (1m close for the line) → FILLED_INVALID.
func EvaluateFvgEntry(bars []market.Kline, f *PlanFvgEntry, sinceMs, nowMs int64) FvgEntryVerdict {
	v := FvgEntryVerdict{State: "ABOVE"}
	if f == nil || f.Lo <= 0 || f.Hi <= 0 {
		return v
	}
	cb := closedBarsAt(bars, nowMs)
	if len(cb) == 0 {
		return v
	}
	close := cb[len(cb)-1].Close
	long := strings.EqualFold(f.Direction, "long")
	distal := f.Lo
	if !long {
		distal = f.Hi
	}
	// Touch numbering: distinct entries INTO the zone since plan birth.
	since := BarsSince(bars, sinceMs)
	inZone := false
	for _, b := range since {
		in := b.Low <= f.Hi && b.High >= f.Lo
		if in && !inZone {
			v.TouchNumber++
		}
		inZone = in
	}
	switch {
	case (long && close <= distal) || (!long && close >= distal):
		v.State = "FILLED_INVALID"
		v.Detail = fmt.Sprintf("closed through distal %.2f on the decision TF", distal)
	case close >= f.Lo && close <= f.Hi:
		v.State = "IN_ZONE"
		ce := FvgCe(f.Lo, f.Hi)
		if strings.EqualFold(f.EntryMode, "ce") {
			// ce mode: MET near the midpoint (band = max(2 ticks, 10% of width)).
			band := math.Max(0.5, (f.Hi-f.Lo)*0.10)
			v.Met = math.Abs(close-ce) <= band
			v.Detail = fmt.Sprintf("CE %.2f (%.1f away%s)", ce, math.Abs(close-ce), map[bool]string{true: " — MET", false: ""}[v.Met])
		} else {
			v.Met = true
			v.Detail = fmt.Sprintf("in zone %.2f–%.2f — MET (edge mode)", f.Lo, f.Hi)
		}
	case close > f.Hi:
		v.State = "ABOVE"
		v.Detail = fmt.Sprintf("price %.2f above the gap — waiting for the retrace", close)
	case close < f.Lo:
		v.State = "BELOW"
		v.Detail = fmt.Sprintf("price %.2f below the gap — waiting for the retrace", close)
	}
	return v
}

func closedBarsAt(bars []market.Kline, nowMs int64) []market.Kline {
	out := make([]market.Kline, 0, len(bars))
	for _, b := range bars {
		if b.CloseTime < nowMs {
			out = append(out, b)
		}
	}
	return out
}

// RenderFvgEntryLines renders the T3-style live advisory per fvg_entry
// scenario: "S2 fvg_entry: gap 29641.00–29652.00 (CE 29646.50) — price IN
// ZONE · touch #2". Facts only; the AI judges.
func RenderFvgEntryLines(doc PlanDoc, bars []market.Kline, sinceMs, nowMs int64) string {
	var b strings.Builder
	for _, s := range doc.Scenarios {
		if !strings.EqualFold(strings.TrimSpace(s.Condition), "fvg_entry") || s.Fvg == nil {
			continue
		}
		v := EvaluateFvgEntry(bars, s.Fvg, sinceMs, nowMs)
		touch := ""
		if v.TouchNumber > 0 {
			touch = fmt.Sprintf(" · touch #%d", v.TouchNumber)
		}
		b.WriteString(fmt.Sprintf("  %s fvg_entry: gap %.2f–%.2f (CE %.2f, mode %s) — price %s%s (%s)\n",
			s.ID, s.Fvg.Lo, s.Fvg.Hi, FvgCe(s.Fvg.Lo, s.Fvg.Hi), s.Fvg.EntryMode, v.State, touch, v.Detail))
	}
	if b.Len() == 0 {
		return ""
	}
	return "Machine-computed FVG entries (advisory — you remain the judge):\n" + b.String()
}

// FvgScenarioStates is the card-facing per-scenario state list (API → FE chip).
type FvgScenarioState struct {
	ID    string  `json:"id"`
	Lo    float64 `json:"fvg_lo"`
	Hi    float64 `json:"fvg_hi"`
	CE    float64 `json:"ce"`
	Mode  string  `json:"entry_mode"`
	State string  `json:"state"`
	Touch int     `json:"touch_number"`
	Met   bool    `json:"met"`
}

// FvgScenarioStatesFor builds the card payload for one plan doc.
func FvgScenarioStatesFor(doc PlanDoc, bars []market.Kline, sinceMs, nowMs int64) []FvgScenarioState {
	var out []FvgScenarioState
	for _, s := range doc.Scenarios {
		if !strings.EqualFold(strings.TrimSpace(s.Condition), "fvg_entry") || s.Fvg == nil {
			continue
		}
		v := EvaluateFvgEntry(bars, s.Fvg, sinceMs, nowMs)
		out = append(out, FvgScenarioState{
			ID: s.ID, Lo: s.Fvg.Lo, Hi: s.Fvg.Hi, CE: FvgCe(s.Fvg.Lo, s.Fvg.Hi),
			Mode: s.Fvg.EntryMode, State: v.State, Touch: v.TouchNumber, Met: v.Met,
		})
	}
	return out
}
