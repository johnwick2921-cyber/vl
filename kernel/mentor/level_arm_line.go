package mentor

import "strings"

// G17 D2.3-R34 (m466 P2, release #61) — a resting level order on a MOVING line
// is cancelled by a close through the line's CURRENT price.
//
// Course: "Mình làm gì? Mình cancel. Cancel. Chẳng làm gì khác hết" — a later
// candle closes below the level [D2.3 p1 @23:35–24:03].
//
// Before #61 levelArmCancels compared the close with LevelArm.LevelPrice,
// frozen at placement. A sloped trendline moves ~0.45 pt/min, so twenty
// minutes later a rising support stood ~9 pts above its placement price and a
// close under the CURRENT line (but over the frozen one) left the buy stop
// resting; and a line that DIED (a 5m close through, D2.3) simply left the
// level set while its order rested on.
//
// levelArmLinePrice answers, for the cancel sweep, the price the close is
// compared with:
//   - a TRENDLINE key reads the line's value at the checked candle's close
//     (Trendline.priceAt(chkCloseTime)) from this tick's live lines; a line no
//     longer among them (dead, or dropped by the today filter) is GONE → the
//     order is cancelled (level_arm_line_gone);
//   - an EMA 34 key (the 1m/2m EMA 34 or the EMA 34 location line) reads this
//     tick's line price from the level set;
//   - every other key — key levels, old extremes, box edges, gap edges, the
//     5m trigger retest (a step line: a reversal is a different line, not a
//     drift) and the D4.3 wick zone (fixed edges with their own death rule) —
//     keeps the placement price, unchanged.
//
// Knob LevelArmLineCancel "current_line" (default) | "placement_price" (the
// pre-#61 frozen read, byte-identical).

// levelArmLineCurrent reports whether the G17 read is on ("" reads as on).
func levelArmLineCurrent(cfg Config) bool {
	return cfg.LevelArmLineCancel != LevelArmLineCancelPlacement
}

// isCurrentLineKey: the moving lines whose order follows the line's current
// price — the trendline and the EMA 34 keys.
func isCurrentLineKey(key string) bool {
	return key == string(KindEMA34) || key == string(KindEMA34HTF) ||
		strings.HasPrefix(key, string(KindTrendline)+":")
}

// levelArmLinePrice returns the price a resting arm's close-through is read
// against and whether its line is gone. lines are this tick's LIVE trendlines
// (dead ones already dropped); levels is this tick's level set.
func levelArmLinePrice(cfg Config, arm LevelArm, key string, levels []Level, lines []Trendline, chkCloseTime int64) (price float64, gone bool) {
	if !levelArmLineCurrent(cfg) || !isCurrentLineKey(key) {
		return arm.LevelPrice, false
	}
	if strings.HasPrefix(key, string(KindTrendline)+":") {
		for _, tl := range lines {
			if tl.key() == key && !tl.Dead {
				return tl.priceAt(chkCloseTime), false
			}
		}
		return arm.LevelPrice, true
	}
	for _, l := range levels {
		if l.Key == key && l.Price != 0 {
			return l.Price, false
		}
	}
	return arm.LevelPrice, false // the EMA line not read this tick: the placement price stands
}
