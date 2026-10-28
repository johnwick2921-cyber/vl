package mentor

import (
	"fmt"
	"math"
	"time"

	"vl/kernel"
	"vl/market"
)

// m428 R2 (owner ruling 2026-10-25, Q2): two same-role swing wicks at (nearly)
// the SAME price are a horizontal SUPPORT (two lows) or RESISTANCE (two highs),
// NEVER a trendline — "a trendline must have an angle". "Near" is the day's own
// measure: half the average high-low range of the last 20 closed candles of the
// traded frame (flatNearPts) — quiet day ~4-5 pts, run day 10-20 pts.
//
// The flat pair joins the level set as an OLD HIGH / OLD LOW of its role
// (Kind KindOldExtreme, Role OldLow for a support, OldHigh for a resistance), so
// every trading rule that reads an old high/low — location, touch, PHL/PLH,
// target ladder, ISB-at-old-extreme flag, role fit — reads it with the SAME
// semantics by construction; only its Key (flatSupportTag / flatResistanceTag)
// and its chart label ("support" / "resistance") tell it apart. The level sits
// at the LATER swing's price (a wick that really traded) and is alive until a
// later closed 1m candle trades beyond it — exactly the old-extreme break rule
// (oldExtremeBroken).
const (
	flatSupportTag    = "flat_support"
	flatResistanceTag = "flat_resistance"
)

// flatSRLevels returns the day's flat support / resistance levels: per role the
// ONE nearest unbroken flat pair (an unbroken support sits below price, so the
// nearest is the highest; mirror for resistance). Pairs are any two of today's
// confirmed same-role swings at least 5 frame bars apart, within the near
// distance of each other (read at the later swing), with no candle between them
// piercing the pair's outer wick by more than the touch band.
func flatSRLevels(bars []market.Kline, cfg Config, now int64) []Level {
	if len(bars) < 2 {
		return nil
	}
	nowT := time.UnixMilli(now)
	fb, _ := execBuildFrame(bars, nowT, cfg.Exec2mAfter30m && exec2mReads2m(now))
	if len(fb) < 5 {
		return nil
	}
	today := tradingDayKey(nowT.In(ctime()))
	tape := bars[:len(bars)-1]
	var out []Level
	for _, role := range []kernel.LevelKind{kernel.KindSWGL, kernel.KindSWGH} {
		isHigh := role == kernel.KindSWGH
		var pts []swingPairAt
		for _, s := range swings3(fb) {
			if s.kind == role && swingConfirmed(fb, s) &&
				tradingDayKey(time.UnixMilli(fb[s.idx].OpenTime).In(ctime())) == today {
				pts = append(pts, s)
			}
		}
		var best Level
		for j := 1; j < len(pts); j++ {
			p1 := pts[j]
			near := flatNearPts(fb, p1.idx)
			confirmedAt := fb[p1.idx+1].CloseTime
			if oldExtremeBroken(tape, p1.price, confirmedAt, isHigh) {
				continue
			}
			for k := 0; k < j; k++ {
				p0 := pts[k]
				// the 5-bar floor is OURS, not the course (c52 W9 B7-R19; same constant as newSlopedLine)
				if p1.idx-p0.idx < 5 || math.Abs(p1.price-p0.price) > near || flatPierced(fb, p0, p1, isHigh) {
					continue
				}
				tag, roleName := flatSupportTag, OldLow
				if isHigh {
					tag, roleName = flatResistanceTag, OldHigh
				}
				l := Level{
					Key:    fmt.Sprintf("%s:%s:%.2f", KindOldExtreme, tag, p1.price),
					Kind:   KindOldExtreme,
					Price:  p1.price,
					AtTime: confirmedAt,
					Role:   roleName,
				}
				nearer := best.Kind == "" || (isHigh && l.Price < best.Price) || (!isHigh && l.Price > best.Price) ||
					l.Price == best.Price && l.AtTime > best.AtTime
				if nearer {
					best = l
				}
				break // one pair per later swing is enough
			}
		}
		if best.Kind != "" {
			out = append(out, best)
		}
	}
	return out
}

// flatPierced reports a candle between the two anchors that went beyond the
// pair's outer wick by more than the touch band — price cut through the level
// between its own two touches, so the pair is no clean flat line.
func flatPierced(fb []market.Kline, p0, p1 swingPairAt, isHigh bool) bool {
	for i := p0.idx + 1; i < p1.idx && i < len(fb); i++ {
		if isHigh && fb[i].High > math.Max(p0.price, p1.price)+trendlineTouchBandPts {
			return true
		}
		if !isHigh && fb[i].Low < math.Min(p0.price, p1.price)-trendlineTouchBandPts {
			return true
		}
	}
	return false
}

// withFlatSR folds the flat levels into the level set as OLD HIGH / OLD LOW
// candidates: the course keeps ONE nearest unbroken old high and ONE nearest
// unbroken old low (swingOldExtremeLevels), so a flat resistance/support
// REPLACES the pivot-picked one of its role when it is the nearer (a support: the
// higher; a resistance: the lower; a tie takes the flat tag) and is otherwise not
// a separate level. Never adds a third old extreme: the r22 "one pair" pins and
// every touch / refusal count that reads the old extremes keep their shape.
func withFlatSR(levels []Level, bars []market.Kline, cfg Config, now int64) []Level {
	for _, f := range flatSRLevels(bars, cfg, now) {
		at := -1
		for i, l := range levels {
			if l.Kind == KindOldExtreme && l.Role == f.Role {
				at = i
				break
			}
		}
		switch {
		case at < 0:
			levels = append(levels, f)
		case f.Role == OldLow && f.Price >= levels[at].Price, f.Role == OldHigh && f.Price <= levels[at].Price:
			levels[at] = f
		}
	}
	return levels
}

// FlatNearScaleForTest sets the R2 near-distance multiplier and returns the
// function that restores it (like kernel.ClockSampleForTest, for tests in other
// packages whose synthetic tapes have candles a few points tall and swings a
// point or two apart). Production never calls it.
func FlatNearScaleForTest(scale float64) (restore func()) {
	old := flatNearScale
	flatNearScale = scale
	return func() { flatNearScale = old }
}
