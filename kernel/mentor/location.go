package mentor

import (
	"fmt"
	"strings"

	"vl/market"
)

// LocationVerdict implements the LOCATION GATE (fold item 1, owner ruling:
// setups happen ONLY at important levels).
//
// TEST-ONLY (c52 W12 S18): this function has NO production caller — only
// location_test.go and isb_ruling_test.go call it. Production decides a PHL/PLH
// location through levelIsLocation (eval.go, the touch loop) and the swing
// through its own level-touch read. The gate covers PHL/PLH, NOT the ISB: the
// 2026-10-03 owner ruling made the ISB un-location-gated ("inside bar lúc nào
// cũng có thể take risk" [D4.1 p1 @05:19–05:34]; see eval.go, the ISB branch).
// Kept as the executable spec of the location rules; do not delete it.
// A setup's reference candle must touch one of:
//
//   - a key level (§4.3);
//   - the EMA 34 of the trading chart — R3: the 1m EMA 34 for intraday setups
//     [D5.4 @08:12–08:40] (knob EMALocationTFMinutes, default 1;
//     other TFs 5/15/60/240 allowed for experimentation; the 4h EMA 34 is ONLY
//     the §8 swing's, never this gate [D5.2 p1 @ 05:30–05:43]);
//   - the 5m trigger-line retest [D3.4 p2 @ 20:51];
//   - (FTGH/FTGL box edges are out of v1 — boxes are not built).
//
// An old high/low ALONE is not a location: it counts only when it coincides
// within LocationCoincidePts (±2) of a key level. EMA 9 is a reverse-ISB
// location only (§10) — never a location for ISB/PHL/PLH in v1.
//
// levels may be the plain level set; the verdict recomputes the EMA34 line
// from the bar history so a moving line is judged at its CURRENT value.
func LocationVerdict(ref market.Kline, levels []Level, trigger TriggerLine, bars []market.Kline, cfg Config) (ok bool, where string) {
	if !cfg.Enabled {
		return false, ""
	}
	if len(bars) > 0 && cfg.EMALocationTFMinutes > 0 {
		v := emaValue(barsTF(bars, cfg.EMALocationTFMinutes), cfg.EMAPeriod34)
		if touchesPrice(ref, v, cfg.TouchBandPts) {
			return true, fmt.Sprintf("ema34_%dm", cfg.EMALocationTFMinutes)
		}
	}
	for _, l := range levels {
		switch l.Kind {
		case KindKeyLevel:
			if touchesPrice(ref, l.Price, cfg.TouchBandPts) {
				return true, "key_level"
			}
		case KindOldExtreme:
			// only when it coincides ±2 pts with a key level
			if coincidesWithKeyLevel(l, levels) && touchesPrice(ref, l.Price, cfg.TouchBandPts) {
				return true, "old_extreme@key_level"
			}
		case KindTriggerRetest:
			if touchesPrice(ref, l.Price, cfg.TouchBandPts) {
				return true, "trigger_retest"
			}
		case KindFTGHEdge, KindFTGLEdge:
			// BOX RULING part 2: a box edge is a location, used again and again.
			if touchesPrice(ref, l.Price, cfg.TouchBandPts) {
				return true, "box_edge"
			}
		case KindTrendline:
			// X9 slide 27 + DAY-3 row 27: a VALID trendline (3rd touch passed)
			// is a location. The level's Price is the line's current value.
			if touchesPrice(ref, l.Price, cfg.TouchBandPts) {
				return true, "trendline"
			}
		}
		// KindEMA9 is deliberately NOT a location (reverse-ISB only, §10).
	}
	if trigger.Price != 0 && trigger.Dir != "" && touchesPrice(ref, trigger.Price, cfg.TouchBandPts) {
		return true, "trigger_line"
	}
	return false, ""
}

// LocationOf names the place a setup was taken at, read from its G2 AnchorKey
// (the touched level's key, or the box key): "trendline long", "key_level",
// "ftgl", "ema34_htf", … — the trader's size log prints it so a trade's
// location is on the record. A trendline also names its side (support =
// long, resistance = short). "" = the setup carries no anchor (a plain ISB).
func LocationOf(anchorKey string) string {
	if anchorKey == "" {
		return ""
	}
	kind, rest, _ := strings.Cut(anchorKey, ":")
	if kind == string(KindTrendline) {
		side, _, _ := strings.Cut(rest, ":")
		return kind + " " + side
	}
	return kind
}

// LocationCoincidePts is the ±2-pt coincidence for an old high/low next to a
// key level (fold item 1).
const LocationCoincidePts = 2.0

func coincidesWithKeyLevel(l Level, levels []Level) bool {
	for _, k := range levels {
		if k.Kind == KindKeyLevel && abs(k.Price-l.Price) <= LocationCoincidePts {
			return true
		}
	}
	return false
}

// touchesPrice reports a literal touch: the candle's range reaches the price
// within the band (§3 step 1).
func touchesPrice(ref market.Kline, price, band float64) bool {
	return ref.High >= price-band && ref.Low <= price+band
}

// levelIsLocation reports whether a touched LEVEL itself is a valid entry
// location (PHL/PLH at it): key levels, the EMA34-HTF line, the trigger retest
// — or an old extreme coinciding ±2 pts with a key level. A bare old high/low
// is NOT a location (fold item 1).
func levelIsLocation(lvl Level, levels []Level) bool {
	switch lvl.Kind {
	case KindKeyLevel, KindEMA34HTF, KindTriggerRetest, KindFTGHEdge, KindFTGLEdge, KindTrendline:
		return true
	case KindOldExtreme:
		return coincidesWithKeyLevel(lvl, levels)
	}
	return false
}

// EMALocationLevel returns the location-gate EMA 34 line as a Level (for the
// evaluator's PHL/PLH entry-level check, where the touched level itself must
// be a location). R3: default tf = 1m (intraday chart).
func EMALocationLevel(bars []market.Kline, cfg Config) (Level, bool) {
	return EMALocationLevelAt(bars, cfg, 0)
}

// EMALocationLevelAt is EMALocationLevel on the M401 execution-frame schedule
// (emaScheduleTF; now == 0 = no clock, the configured TF as-is).
func EMALocationLevelAt(bars []market.Kline, cfg Config, now int64) (Level, bool) {
	if !cfg.Enabled || len(bars) == 0 || cfg.EMALocationTFMinutes <= 0 {
		return Level{}, false
	}
	tfMin := emaScheduleTF(cfg, cfg.EMALocationTFMinutes, now)
	tf := barsTF(bars, tfMin)
	if tfMin != cfg.EMALocationTFMinutes {
		tf = closedBucketsTF(bars, tfMin, now)
		if len(tf) == 0 || len(tf) < cfg.EMAPeriod34 {
			tf = barsTF(bars, cfg.EMALocationTFMinutes) // 2m line not formed yet: the 1m line stands
		}
	}
	v := emaValue(tf, cfg.EMAPeriod34)
	return Level{
		Key:   string(KindEMA34HTF),
		Kind:  KindEMA34HTF,
		Price: v,
	}, true
}
