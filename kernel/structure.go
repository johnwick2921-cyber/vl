package kernel

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"vl/market"
)

// G2 (regime wave, 2026-08-21) — PURE STRUCTURE DETECTORS: fractal swings →
// HH/HL/LH/LL → TRENDING_UP / TRENDING_DOWN / RANGING, plus BOS / CHoCH /
// SWEEP events. MSS = CHoCH + displacement (body ≥ MSS-body-ATR × ATR).
// No gating by itself — G1 (HTF veto), G4 (transition stand-down) and
// G8 (watcher hooks) consume its output.
//
// Spec source: the dispatch cites docs/research/plan-card/Implementation/v3
// (NQ-calibrated swing filters) + the FULL-SPEC vocabulary. Those research
// files are NOT present in this repo (found-not-fixed), so the calibration
// constants below are env-tunable per the dispatch's config discipline and
// the HH/HL/LH/LL vocabulary follows docs/market-regime-classification-en.md
// §6.4 ("Swing Structure — HH/HL/LH/LL sequence — determine trend structure").

const (
	DefaultStructureSwingK      = 2    // fractal window: bar must exceed k bars each side
	DefaultStructureMinSwingATR = 0.25 // swing move must be ≥ this × ATR vs the prior opposite swing
	DefaultStructureMSSBodyATR  = 1.5  // MSS displacement: CHoCH bar body ≥ this × ATR
	DefaultStructureEventWindow = 4    // event history kept in the snapshot (periods)
	// DefaultStructureOneWayATR (F-4, planner-touch-fills 2026-10-14) — a close
	// this many ATR beyond the last swing extreme, with no completed 3-swing set
	// (Trend RANGING), is a ONE-WAY move. 1.0 = the repo's own displacement unit
	// for a broken level (BD_MIN_DISP_ATR default 1.0, breakdown_continue.go).
	DefaultStructureOneWayATR = 1.0
)

// structureOneWayATR resolves STRUCTURE_ONEWAY_ATR (default 1.0; 0 or a bad
// value → the default; the boot regime ledger prints the resolved value).
func structureOneWayATR() float64 {
	if v := os.Getenv("STRUCTURE_ONEWAY_ATR"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return DefaultStructureOneWayATR
}

// StructureOneWayATR exports the resolved one-way multiple (boot ledger, guide).
func StructureOneWayATR() float64 { return structureOneWayATR() }

// StructureOneWay is the evidence of a one-way move on a TF whose 3-swing
// trend is RANGING: the newest CLOSED bar closed beyond the last swing extreme
// by at least Mult × ATR — a break of structure the swing set has not caught
// up with (a crash makes no new fractal low until it pauses).
type StructureOneWay struct {
	Dir     string  `json:"dir"`     // up | down
	Close   float64 `json:"close"`   // the newest closed bar's close
	Extreme float64 `json:"extreme"` // the swing high/low it closed beyond
	ATR     float64 `json:"atr"`
	Mult    float64 `json:"mult"`
	TimeMs  int64   `json:"time_ms"` // that bar's close instant
}

// StructureTFs are the per-TF detectors the wave builds (2.1).
var StructureTFs = []string{"5m", "15m", "1h"}

// BarsHoleWarning reports the LATEST HOLE inside the last 6 hours of a bar
// series (m134/m149): a gap longer than one bar interval between consecutive
// bars, counting only the CME-OPEN missing minutes. A pure session break
// (16:00→17:00 CT, a weekend) has no open minute missing and does NOT warn;
// the forming tail never trips it. Returns "" when the series is contiguous or
// every hole is older than 6 h (a stale hole must not warn for hours).
func BarsHoleWarning(bars []market.Kline, tfMin int) string {
	if tfMin <= 0 || len(bars) < 2 {
		return ""
	}
	step := int64(tfMin) * 60_000
	newest := bars[len(bars)-1].OpenTime
	sixH := int64(6 * 60 * 60 * 1000)
	for i := len(bars) - 1; i >= 1; i-- {
		d := bars[i].OpenTime - bars[i-1].OpenTime
		if d <= step {
			continue
		}
		if bars[i].OpenTime < newest-sixH {
			break // every older gap is outside the 6 h window
		}
		prev := bars[i-1].OpenTime
		next := bars[i].OpenTime
		open := 0
		for m := prev + step; m < next; m += step {
			if IsCMEOpen(time.UnixMilli(m)) {
				open++
			}
		}
		if open == 0 {
			continue // a pure session break — no open minute missing
		}
		return fmt.Sprintf("gap %s → %s (%d open minute(s) missing)", ClockCT(time.UnixMilli(prev)), ClockCT(time.UnixMilli(next)), open)
	}
	return ""
}

// structureTFMinutes is the bar length table for the structure detectors
// (5m/15m/1h only — the acceptance resolver is rule-scoped and would fold 1h
// into its 5m default).
func structureTFMinutes(tf string) int {
	switch strings.TrimSpace(tf) {
	case "15m":
		return 15
	case "1h":
		return 60
	default:
		return 5
	}
}

// structureClockCT renders a swing/event instant as "15:04 CT" (full times
// stay labelled; the per-TF prompt line is already CT-labelled).
func structureClockCT(ms int64) string {
	return ClockCT(time.UnixMilli(ms))
}

// structureSwingK resolves STRUCTURE_SWING_K (default 2).
func structureSwingK() int {
	if v := os.Getenv("STRUCTURE_SWING_K"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
	}
	return DefaultStructureSwingK
}

// structureMinSwingATR resolves STRUCTURE_MIN_SWING_ATR (default 0.25).
func structureMinSwingATR() float64 {
	if v := os.Getenv("STRUCTURE_MIN_SWING_ATR"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return DefaultStructureMinSwingATR
}

// structureMSSBodyATR resolves STRUCTURE_MSS_BODY_ATR (default 1.5).
func structureMSSBodyATR() float64 {
	if v := os.Getenv("STRUCTURE_MSS_BODY_ATR"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return DefaultStructureMSSBodyATR
}

// SwingRef is one confirmed fractal swing.
type SwingRef struct {
	Kind   string  `json:"kind"` // HH | HL | LH | LL
	Price  float64 `json:"price"`
	TimeMs int64   `json:"time_ms"` // bar close instant
}

// StructureEvent is a machine-detected break event on one TF.
type StructureEvent struct {
	Type     string  `json:"type"` // BOS | CHoCH | MSS | SWEEP
	Dir      string  `json:"dir"`  // up | down
	Price    float64 `json:"price"`
	TimeMs   int64   `json:"time_ms"`   // triggering bar close
	RefPrice float64 `json:"ref_price"` // the swing extreme that broke
	Detail   string  `json:"detail"`
}

// StructureState is the per-TF snapshot G1/G4/G8 read.
type StructureState struct {
	Trend      string           `json:"trend"` // TRENDING_UP | TRENDING_DOWN | RANGING
	Swing      *SwingRef        `json:"swing,omitempty"`
	LastEvents []StructureEvent `json:"last_events,omitempty"`
	// Atr (A3 min-SL gate, 2026-08-26) — ATR(14) Wilder on THIS TF's closed bars,
	// computed by the same simpleATR14 the swing engine already uses. Exposed so
	// the entry gate can size the minimum stop distance against the real 5m ATR
	// instead of a hardcoded width (research: 15/27 week losers stopped-too-tight).
	Atr float64 `json:"atr,omitempty"`
	// OneWay (F-4) — set ONLY when Trend is RANGING and the newest closed bar
	// broke the last swing extreme by ≥ STRUCTURE_ONEWAY_ATR × ATR. Trend itself
	// is unchanged for its 21 production readers; ONLY the HTF veto reads the
	// evidence (StructureState.VetoTrend, kernel/htf_veto.go).
	OneWay *StructureOneWay `json:"one_way,omitempty"`
}

type swing struct {
	kind   string
	price  float64
	timeMs int64
	high   bool
}

// simpleATR14 computes the classic ATR(14) on closed bars with WILDER
// smoothing — the variant the research's nautilus ATR and vl/market's
// calculateATR both use. Conformance audit C-ATR1 (2026-08-22): this file
// previously used a plain SMA, which read ~43% low on the 08-21 15m series
// and silently loosened the min-swing and MSS-displacement thresholds.

// SimpleATR14 exports the ATR helper for replays: join bars+atr by timestamp
// via store.BarHistoryStore.BarsBetween, then recompute the same value here.
func SimpleATR14(highs, lows, closes []float64) float64 { return simpleATR14(highs, lows, closes) }

func simpleATR14(highs, lows, closes []float64) float64 {
	if len(closes) == 0 {
		return 0
	}
	if len(closes) == 1 {
		return highs[0] - lows[0]
	}
	trs := make([]float64, len(closes))
	trs[0] = highs[0] - lows[0]
	for i := 1; i < len(closes); i++ {
		tr := highs[i] - lows[i]
		if hc := highs[i] - closes[i-1]; hc > tr {
			tr = hc
		}
		if lc := closes[i-1] - lows[i]; lc > tr {
			tr = lc
		}
		trs[i] = tr
	}
	n := 14
	if len(trs) <= n {
		sum := 0.0
		for i := 1; i < len(trs); i++ {
			sum += trs[i]
		}
		return sum / float64(len(trs)-1)
	}
	sum := 0.0
	for i := 1; i <= n; i++ {
		sum += trs[i]
	}
	atr := sum / float64(n)
	for i := n + 1; i < len(trs); i++ {
		atr = (atr*float64(n-1) + trs[i]) / float64(n)
	}
	return atr
}

// fractalSwings is the swing pass of ComputeStructureState, extracted (S1
// structure layer, 2026-09-16) so the D/4h/1h structure map runs the SAME
// detector: fractal extremes (window k), alternating high/low, min-move
// filtered against the prior opposite swing, then labelled HH/HL/LH/LL
// against the previous same-type swing. Pure; oldest first.
func fractalSwings(closed []market.KlineBar, iv int64, atr float64) []swing {
	n := len(closed)
	if n == 0 {
		return nil
	}
	highs := make([]float64, n)
	lows := make([]float64, n)
	for i, b := range closed {
		highs[i] = b.High
		lows[i] = b.Low
	}
	k := structureSwingK()
	swings := make([]swing, 0, 16)
	for i := k; i < n-k; i++ {
		isHigh, isLow := true, true
		for j := i - k; j <= i+k; j++ {
			if j == i {
				continue
			}
			if highs[j] >= highs[i] {
				isHigh = false
			}
			if lows[j] <= lows[i] {
				isLow = false
			}
		}
		if !isHigh && !isLow {
			continue
		}
		price, hi := highs[i], true
		if isLow {
			price, hi = lows[i], false
		}
		t := closed[i].Time + iv
		if len(swings) > 0 && swings[len(swings)-1].high == hi {
			// Same-side extreme: keep the more extreme one.
			if (hi && price <= swings[len(swings)-1].price) || (!hi && price >= swings[len(swings)-1].price) {
				continue
			}
			swings[len(swings)-1] = swing{price: price, timeMs: t, high: hi}
			continue
		}
		// Min-move significance vs the prior opposite swing.
		if len(swings) > 0 {
			move := price - swings[len(swings)-1].price
			if move < 0 {
				move = -move
			}
			if atr > 0 && move < structureMinSwingATR()*atr {
				continue
			}
		}
		swings = append(swings, swing{price: price, timeMs: t, high: hi})
	}
	// 2. label HH/HL/LH/LL against the previous same-type swing.
	for i := range swings {
		prev := -1
		for j := i - 1; j >= 0; j-- {
			if swings[j].high == swings[i].high {
				prev = j
				break
			}
		}
		switch {
		case swings[i].high && prev < 0:
			swings[i].kind = "HH"
		case swings[i].high && swings[i].price > swings[prev].price:
			swings[i].kind = "HH"
		case swings[i].high:
			swings[i].kind = "LH"
		case !swings[i].high && prev < 0:
			swings[i].kind = "LL"
		case !swings[i].high && swings[i].price > swings[prev].price:
			swings[i].kind = "HL"
		default:
			swings[i].kind = "LL"
		}
	}

	return swings
}

// ComputeStructureState runs the swing engine + event detectors on one TF's
// kline series. tfMinutes names the interval; nowMs is the evaluation instant;
// only bars CLOSED at nowMs participate (repo bar convention: a bar closes at
// Time + interval). atr ≤ 0 → computed from the series itself.
func ComputeStructureState(klines []market.KlineBar, tfMinutes int, atr float64, nowMs int64) StructureState {
	iv := int64(tfMinutes) * 60_000
	closed := make([]market.KlineBar, 0, len(klines))
	for _, b := range klines {
		if b.Time+iv <= nowMs {
			closed = append(closed, b)
		}
	}
	out := StructureState{Trend: "RANGING"}
	n := len(closed)
	if n == 0 {
		return out
	}
	highs := make([]float64, n)
	lows := make([]float64, n)
	closes := make([]float64, n)
	for i, b := range closed {
		highs[i] = b.High
		lows[i] = b.Low
		closes[i] = b.Close
	}
	if atr <= 0 {
		atr = simpleATR14(highs, lows, closes)
	}
	out.Atr = atr // A3 (2026-08-26) — per-TF ATR(14) Wilder for the min-SL gate

	swings := fractalSwings(closed, iv, atr)
	if len(swings) == 0 {
		return out
	}

	// 3. trend: 3-swing confirmation (the dispatch's standard) — the last two
	// swing PAIRS compared same-type: newest high > previous high AND newest low
	// > previous low → TRENDING_UP; both descending → TRENDING_DOWN; anything
	// else, incl. fewer than 4 confirmed swings, is RANGING (fail-open:
	// unconfirmed never vetoes).
	if len(swings) >= 4 {
		a, b, c, d := swings[len(swings)-4], swings[len(swings)-3], swings[len(swings)-2], swings[len(swings)-1]
		if a.high && b.high == false && c.high && d.high == false {
			if d.price > b.price && c.price > a.price {
				out.Trend = "TRENDING_UP"
			} else if d.price < b.price && c.price < a.price {
				out.Trend = "TRENDING_DOWN"
			}
		} else if a.high == false && b.high && c.high == false && d.high {
			if c.price > a.price && d.price > b.price {
				out.Trend = "TRENDING_UP"
			} else if c.price < a.price && d.price < b.price {
				out.Trend = "TRENDING_DOWN"
			}
		}
	}
	newest := swings[len(swings)-1]
	out.Swing = &SwingRef{Kind: newest.kind, Price: newest.price, TimeMs: newest.timeMs}

	// 4. events vs the last high/low swing extremes.
	lastHigh, lastLow := newest, newest
	for i := len(swings) - 1; i >= 0; i-- {
		if swings[i].high {
			lastHigh = swings[i]
			break
		}
	}
	for i := len(swings) - 1; i >= 0; i-- {
		if !swings[i].high {
			lastLow = swings[i]
			break
		}
	}
	// F-4 — a BOS without a completed 3-swing set: on a RANGING TF, the newest
	// closed bar closing ≥ k×ATR beyond the last swing extreme is a one-way
	// move (the 10-14 07:00–09:00 shape: a 400-pt fall makes no new fractal
	// low until it pauses). Recorded as evidence; Trend stays RANGING.
	if out.Trend == "RANGING" && atr > 0 {
		lc, k := closed[n-1], structureOneWayATR()
		t := lc.Time + iv
		if !lastLow.high && t > lastLow.timeMs && lc.Close <= lastLow.price-k*atr {
			out.OneWay = &StructureOneWay{Dir: "down", Close: lc.Close, Extreme: lastLow.price, ATR: atr, Mult: k, TimeMs: t}
		} else if lastHigh.high && t > lastHigh.timeMs && lc.Close >= lastHigh.price+k*atr {
			out.OneWay = &StructureOneWay{Dir: "up", Close: lc.Close, Extreme: lastHigh.price, ATR: atr, Mult: k, TimeMs: t}
		}
	}
	var events []StructureEvent
	// Track one BOS per side per run (first break past the extreme).
	bosUpSeen, bosDownSeen := false, false
	for i := n - 1; i >= 0; i-- {
		b := closed[i]
		t := b.Time + iv
		if t <= newest.timeMs {
			break // only bars AFTER the newest confirmed swing can break it
		}
		switch out.Trend {
		case "TRENDING_UP":
			if b.Close > lastHigh.price && !bosUpSeen {
				bosUpSeen = true
				events = append(events, StructureEvent{Type: "BOS", Dir: "up", Price: b.Close, TimeMs: t,
					RefPrice: lastHigh.price, Detail: fmt.Sprintf("BOS-up %.2f @%s", b.Close, structureClockCT(t))})
			}
			if b.Close < lastLow.price {
				body := b.Close - b.Open
				if body < 0 {
					body = -body
				}
				ev := StructureEvent{Type: "CHoCH", Dir: "down", Price: b.Close, TimeMs: t,
					RefPrice: lastLow.price, Detail: fmt.Sprintf("CHoCH-down %.2f @%s", b.Close, structureClockCT(t))}
				if atr > 0 && body >= structureMSSBodyATR()*atr {
					ev.Type = "MSS"
					ev.Detail = fmt.Sprintf("MSS-down %.2f @%s (body %.2f ≥ %.1f×ATR)", b.Close, structureClockCT(t), body, structureMSSBodyATR())
				}
				events = append(events, ev)
			}
			if b.High > lastHigh.price && b.Close <= lastHigh.price {
				events = append(events, StructureEvent{Type: "SWEEP", Dir: "up", Price: b.High, TimeMs: t,
					RefPrice: lastHigh.price, Detail: fmt.Sprintf("SWEEP of high %.2f (wick %.2f) @%s", lastHigh.price, b.High, structureClockCT(t))})
			}
		case "TRENDING_DOWN":
			if b.Close < lastLow.price && !bosDownSeen {
				bosDownSeen = true
				events = append(events, StructureEvent{Type: "BOS", Dir: "down", Price: b.Close, TimeMs: t,
					RefPrice: lastLow.price, Detail: fmt.Sprintf("BOS-down %.2f @%s", b.Close, structureClockCT(t))})
			}
			if b.Close > lastHigh.price {
				body := b.Close - b.Open
				if body < 0 {
					body = -body
				}
				ev := StructureEvent{Type: "CHoCH", Dir: "up", Price: b.Close, TimeMs: t,
					RefPrice: lastHigh.price, Detail: fmt.Sprintf("CHoCH-up %.2f @%s", b.Close, structureClockCT(t))}
				if atr > 0 && body >= structureMSSBodyATR()*atr {
					ev.Type = "MSS"
					ev.Detail = fmt.Sprintf("MSS-up %.2f @%s (body %.2f ≥ %.1f×ATR)", b.Close, structureClockCT(t), body, structureMSSBodyATR())
				}
				events = append(events, ev)
			}
			if b.Low < lastLow.price && b.Close >= lastLow.price {
				events = append(events, StructureEvent{Type: "SWEEP", Dir: "down", Price: b.Low, TimeMs: t,
					RefPrice: lastLow.price, Detail: fmt.Sprintf("SWEEP of low %.2f (wick %.2f) @%s", lastLow.price, b.Low, structureClockCT(t))})
			}
		default: // RANGING: only liquidity sweeps of the range extremes
			if b.High > lastHigh.price && b.Close <= lastHigh.price {
				events = append(events, StructureEvent{Type: "SWEEP", Dir: "up", Price: b.High, TimeMs: t,
					RefPrice: lastHigh.price, Detail: fmt.Sprintf("SWEEP of high %.2f (wick %.2f) @%s", lastHigh.price, b.High, structureClockCT(t))})
			}
			if b.Low < lastLow.price && b.Close >= lastLow.price {
				events = append(events, StructureEvent{Type: "SWEEP", Dir: "down", Price: b.Low, TimeMs: t,
					RefPrice: lastLow.price, Detail: fmt.Sprintf("SWEEP of low %.2f (wick %.2f) @%s", lastLow.price, b.Low, structureClockCT(t))})
			}
		}
	}
	// newest-first, bounded to the recent window
	cutoff := nowMs - int64(DefaultStructureEventWindow)*iv
	kept := make([]StructureEvent, 0, 3)
	for i := len(events) - 1; i >= 0 && len(kept) < 3; i-- {
		if events[i].TimeMs >= cutoff {
			kept = append(kept, events[i])
		}
	}
	out.LastEvents = kept
	return out
}

// StructureSnapshot computes the per-TF structure from the 1m cache (the SAME
// series every other futures consumer reads) aggregated to 5m/15m/1h.
func StructureSnapshot(bars1m []market.Kline, nowMs int64) map[string]StructureState {
	snap := make(map[string]StructureState, len(StructureTFs))
	for _, tf := range StructureTFs {
		minutes := structureTFMinutes(tf)
		agg := StructureAggregateToMinutes(bars1m, minutes)
		kb := make([]market.KlineBar, 0, len(agg))
		for _, k := range agg {
			kb = append(kb, market.KlineBar{Time: k.OpenTime, Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume})
		}
		state := ComputeStructureState(kb, minutes, 0, nowMs)
		if len(kb) > 0 {
			snap[tf] = state
		}
	}
	// m149: a bars hole is NOT injected here as a fake StructureState — the
	// caller carries it as its own Context field (ctx.StructureGap), so the
	// persisted structure map stays per-TF truth.
	return snap
}

// HTFVetoNativeTF is the higher timeframe the HTF veto's cross / 4h modes read
// from the NATIVE bar series (F-4): the 1m cache holds ~2,000 bars (≈8 4h
// bars) — far too few for a 4h swing set — so the 4h state is computed from
// the bar cache's own 4h series, the same series the planner's structure map
// reads (trader/structure_map_wire.go).
const HTFVetoNativeTF = "4h"

// HTFVetoNativeBarsAsk is how many native 4h bars the veto asks for (the ring
// holds up to 2,500).
const HTFVetoNativeBarsAsk = 2500

// WithNativeHTFStructure adds the 4h state computed from NATIVE 4h bars to a
// structure snapshot (F-4). Before this, StructureSnapshot built 5m/15m/1h
// only, so HTF_VETO_MODE=cross (LIVE since 2026-08-28) and =4h could never
// find snap["4h"] and NEVER vetoed — zero "htf_veto:" refusals in every log
// since the switch. No bars → the key stays ABSENT (never fabricated) and
// the veto fails open loudly for that TF.
func WithNativeHTFStructure(snap map[string]StructureState, bars4h []market.Kline, nowMs int64) map[string]StructureState {
	if len(bars4h) == 0 {
		return snap
	}
	if snap == nil {
		snap = map[string]StructureState{}
	}
	kb := make([]market.KlineBar, 0, len(bars4h))
	for _, k := range bars4h {
		kb = append(kb, market.KlineBar{Time: k.OpenTime, Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume})
	}
	st := ComputeStructureState(kb, 240, 0, nowMs)
	if st.Atr <= 0 && st.Swing == nil {
		return snap // no CLOSED 4h bar at nowMs: absent, not a fabricated RANGING
	}
	snap[HTFVetoNativeTF] = st
	return snap
}

// StructureSnapshotForVeto is the cycle snapshot every consumer of the HTF
// veto reads: StructureSnapshot(1m cache) + the native 4h state (F-4).
func StructureSnapshotForVeto(symbol string, bars1m []market.Kline, nowMs int64) map[string]StructureState {
	snap := StructureSnapshot(bars1m, nowMs)
	if market.FuturesBarsProvider != nil {
		snap = WithNativeHTFStructure(snap, market.FuturesBarsProvider(symbol, HTFVetoNativeTF, HTFVetoNativeBarsAsk), nowMs)
	}
	return snap
}

// StructurePromptLine renders the dispatch's advisory one-liner for the
// executor prompt: "STRUCTURE 15m: TRENDING_DOWN (LL 29346 @10:15) · 1h:
// RANGING · last event: CHoCH-up 15m @29470.25 10:45".
func StructurePromptLine(snap map[string]StructureState) string {
	if len(snap) == 0 {
		return ""
	}
	parts := make([]string, 0, 3)
	for _, tf := range StructureTFs {
		st, ok := snap[tf]
		if !ok {
			parts = append(parts, tf+": n/a")
			continue
		}
		label := tf + ": " + st.Trend
		if st.Swing != nil {
			label += fmt.Sprintf(" (%s %.2f @%s)", st.Swing.Kind, st.Swing.Price, structureClockCT(st.Swing.TimeMs))
		}
		parts = append(parts, label)
	}
	var lastEv *StructureEvent
	var lastTF string
	for _, tf := range StructureTFs {
		st, ok := snap[tf]
		if !ok {
			continue
		}
		for i := range st.LastEvents {
			if lastEv == nil || st.LastEvents[i].TimeMs > lastEv.TimeMs {
				e := st.LastEvents[i]
				lastEv = &e
				lastTF = tf
			}
		}
	}
	line := "STRUCTURE " + strings.Join(parts, " · ")
	if lastEv != nil {
		line += fmt.Sprintf(" · last event: %s %s @%s", lastEv.Type+"-"+lastEv.Dir, lastTF, structureClockCT(lastEv.TimeMs))
	}
	return line
}
