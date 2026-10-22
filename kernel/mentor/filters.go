package mentor

import (
	"strings"

	"vl/market"
)

// Filters is the direction-and-context gate set (PLAN v1 §4). Every filter is
// pure: bars/levels in, a verdict out.

// ── 5-minute trigger line (§5.1) [D3.4 p1 @ 04:02–13:33] ─────────────────────
// BUY trigger = a 5m candle breaks the previous candle's HIGH; SELL = breaks
// the previous LOW [@ 04:02, 08:04]. A line is drawn at the BROKEN extreme,
// WICK INCLUDED [@ 04:28]. Entries only on the trigger side [@ 10:21]. The
// line moves ONLY on a reversal, once [@ 11:48–13:33]. Between two opposing
// lines → no trade [@ 16:38].

type TriggerLine struct {
	Dir   Side    // allowed direction ("" before the first break)
	Price float64 // the broken extreme (wick included)

	// B2: persistence — the last PROCESSED bucket (clock-aligned open) and
	// its bar. TriggerTick processes only newer buckets; the forming bucket
	// may fire a break once and is never re-applied.
	LastBucket int64
	LastBar    market.Kline

	// MovedAt is the bucket open of the bar that last MOVED the line (the
	// first draw or a reversal). The 1h line counts only when it moved at or
	// after the 4h line's last move — an earlier 1h trigger is ignored, the
	// 1h is "silent" [D4.4 p1 @18:36; p2 @03:10] (D4.4-05, item 19).
	MovedAt int64
}

// TriggerTick advances the trigger state over the closed buckets newer than
// LastBucket (B2). The trigger candle need not close [@ 08:19]; a bucket that
// exists (even forming) may fire a break, is committed once, and is never
// re-applied. Buckets are compared with the previous bucket of the SAME
// timeframe that EXISTS — "the previous candle" on his chart [D3.4 p1 @04:02,
// @08:04], never "the previous clock bucket" (CTO ruling 2026-10-04, [B]): the
// first candle after the 16:00–17:00 CT halt, a weekend, a holiday or a data
// hole is compared with the candle before the gap, so a Sunday-open break is
// not missed. tfMin is kept for the callers' signature.
func TriggerTick(prev TriggerLine, bars []market.Kline, tfMin int, cfg Config) TriggerLine {
	if !cfg.Enabled || len(bars) == 0 {
		return prev
	}
	next := prev
	for i, b := range bars {
		if b.OpenTime <= next.LastBucket {
			continue // a committed bucket
		}
		last := next.LastBar
		if last.OpenTime > 0 && b.OpenTime > last.OpenTime {
			next = applyBreak(next, last, b)
		}
		// Only NON-tail buckets commit. The LAST bucket is the forming one: it
		// is re-evaluated every tick with its growing extremes, so an intrabar
		// break fires at the MINUTE it happens — never from the bucket open
		// (replay-audit look-ahead (a) — the Go side must not register early),
		// and never lost (the break is idempotent: same-direction re-breaks do
		// not move the line, B3).
		if i < len(bars)-1 {
			next.LastBar = b
			next.LastBucket = b.OpenTime
		}
	}
	return next
}

// TriggerReplay1m advances a trigger line over a closed 1m series EXACTLY as
// the live path does (release #22 LINES-RESTART): the live bot runs one Tick
// per closed 1m bar, and each Tick hands TriggerTick the tf buckets as they
// stand at THAT minute — the forming bucket with the extremes it had so far.
// So the ORDER in which a bucket broke the prior high and low decides the line
// [D3.4 p1 @08:19 "the trigger candle need NOT close"; p2 @18:49 "keep the one
// price is beyond NOW"]. The seed used to read history bucket-complete
// (TriggerTick over barsTF of the whole series): a bucket that broke both
// extremes and closed inside moved nothing, while live had flipped at the
// first break — so every restart could hand the bot a different line (the
// 10-13 18:57 boot re-seeded the 4h at 30741.25 since 01:00 while the running
// bot held 30774.25 since 05:00).
//
// The replay calls the SAME TriggerTick, minute by minute, with the window
// the live path would have aggregated: [the last complete bucket, the forming
// bucket]. Committed buckets are skipped by TriggerTick's own watermark, so
// the result equals a live evaluator that ticked every bar. bars1m must be
// closed bars in time order (the caller cuts anything not closed by now).
func TriggerReplay1m(prev TriggerLine, bars1m []market.Kline, tfMin int, cfg Config) TriggerLine {
	return TriggerReplay1mFrozen(prev, bars1m, tfMin, cfg, nil)
}

// TriggerReplay1mFrozen is TriggerReplay1m with a CE-4 freeze: when `frozen`
// reports a 1m bar's open time, that bar still builds its bucket (the candle
// stays WHOLE) but fires no break — the trigger read holds its pre-print
// values until the freeze releases. frozen nil = no freeze (TriggerReplay1m).
func TriggerReplay1mFrozen(prev TriggerLine, bars1m []market.Kline, tfMin int, cfg Config, frozen func(openMs int64) bool) TriggerLine {
	if !cfg.Enabled || len(bars1m) == 0 || tfMin <= 1 {
		return prev
	}
	ms := int64(tfMin) * 60_000
	next := prev
	var done, cur market.Kline
	haveDone, haveCur := false, false
	window := make([]market.Kline, 0, 2)
	for _, b := range bars1m {
		bucket := bucketOpen(b.OpenTime, tfMin)
		if !haveCur || bucket != cur.OpenTime {
			if haveCur {
				done, haveDone = cur, true
			}
			// Built exactly like barsTF: the first bar's fields, re-stamped
			// to the bucket; then High/Low/Close/Volume extended per bar.
			cur = b
			cur.OpenTime = bucket
			cur.CloseTime = bucket + ms - 1
			haveCur = true
		} else {
			if b.High > cur.High {
				cur.High = b.High
			}
			if b.Low < cur.Low {
				cur.Low = b.Low
			}
			cur.Close = b.Close
			cur.Volume += b.Volume
		}
		if frozen != nil && frozen(b.OpenTime) {
			continue // the bar builds the bucket, but fires no break
		}
		window = window[:0]
		if haveDone {
			window = append(window, done)
		}
		window = append(window, cur)
		next = TriggerTick(next, window, tfMin, cfg)
	}
	return next
}

// applyBreak runs the §5.1 break logic for one bucket against the previous
// adjacent bucket (B3: every reversal moves the line ONCE — later breaks in
// the SAME direction never move it; the NEXT reversal moves it again).
func applyBreak(next TriggerLine, before, cur market.Kline) TriggerLine {
	brokeHigh := cur.High > before.High
	brokeLow := cur.Low < before.Low
	if brokeHigh && brokeLow {
		// broke BOTH: draw both lines, keep the one price is NOW beyond
		// [D3.4 p2 @ 18:49]. A close INSIDE the prior range is beyond
		// neither extreme: the bar moves nothing and the next bar decides
		// (CTO parity ruling 2026-10-04 Q2).
		switch {
		case cur.Close > before.High:
			brokeLow = false
		case cur.Close < before.Low:
			brokeHigh = false
		default:
			return next
		}
	}
	moved := false
	switch {
	case next.Dir == "": // the FIRST break draws the line [@ 19:48]
		if brokeHigh {
			next.Dir, next.Price = SideLong, before.High
			moved = true
		} else if brokeLow {
			next.Dir, next.Price = SideShort, before.Low
			moved = true
		}
	case next.Dir == SideLong && brokeLow: // reversal: move the line once
		// B1 (10-03 ruling): ONE line, moved on a reversal — the old line is
		// gone, not kept as a second line [D3.4 p1 @11:11–12:40].
		next.Dir, next.Price = SideShort, before.Low
		moved = true
	case next.Dir == SideShort && brokeHigh: // reversal: move the line once
		next.Dir, next.Price = SideLong, before.High
		moved = true
	}
	if moved {
		// MovedAt = the bucket that fired the move (item 19: the 1h line
		// resets when the 4h moves again) [D4.4 p1 @18:36; p2 @03:10].
		next.MovedAt = cur.OpenTime
	}
	// same-direction breaks (including repeats) never move the line
	return next
}

// TriggerVerdict filters an entry by the trigger line: allowed side only.
// B1 (10-03 ruling): there is ONE line, moved on a reversal — the no-trade
// zone "between two trigger lines" was a misread; the real zone sits between
// an FTGL and the buy line (mirror: FTGH and the sell line) and lives in
// triggerBoxZoneVerdict (eval.go), where the boxes are known.
func TriggerVerdict(t TriggerLine, price float64) (ok bool, side Side, reason string) {
	if t.Dir == "" {
		return true, "", ""
	}
	if t.Dir == SideLong && price < t.Price {
		return false, "", "below the buy trigger line — do nothing [D3.4 p1 @ 06:22]"
	}
	if t.Dir == SideShort && price > t.Price {
		return false, "", "above the sell trigger line — do nothing [D3.4 p1 @ 06:22]"
	}
	return true, t.Dir, ""
}


// X8TrapDodge reports the X8 trap-dodge EXIT [X8 "Cách né trap & Setup gồng lời"
// part1 @07:31–08:07]: the 2m candle closed THROUGH the EMA34 against the 5m
// trigger trend while the 5m trigger swept against the position — the breakout
// is a FAKE and the position must exit, not sit ("thì out lại tại đó chứ đừng
// ngồi hô").
//
//	long:  the 5m trigger is on the SELL side (a candle swept the prior low) AND
//	       the 2m candle closed ABOVE the EMA34 → exit the long.
//	short: the 5m trigger is on the BUY side (a candle swept the prior high) AND
//	       the 2m candle closed BELOW the EMA34 → exit the short.
//
// A 2m close through the EMA34 ALONE never fires — the transcript: a close
// under the EMA34 does not by itself allow a short [X8 @08:17–08:48]; the 5m
// sweep is the trigger. ema34/close2m <= 0 or an empty trigger = no read, no
// exit (fail-closed).
func X8TrapDodge(side string, triggerDir Side, ema34, close2m float64) bool {
	if ema34 <= 0 || close2m <= 0 || triggerDir == "" {
		return false
	}
	if strings.EqualFold(side, "short") {
		return triggerDir == SideLong && close2m < ema34
	}
	return triggerDir == SideShort && close2m > ema34
}

// ── 15m/5m ISB conflict [§5.3, D4.2 p1 @ 13:59; §12] ────────────────────────

// 2–3 adjacent 5m candles churning and going nowhere = a 15m inside bar
// [@ 00:00]. Same direction → trade; OPPOSITE directions → DO NOT TRADE AT ALL
// [@ 14:35].

// ISBConflictVerdict — B8 (10-03 fix, D4.2 p1 @14:24–14:52; D5.1 p2
// @01:55): the 15m read is the REAL 15m TF (aggregated CLOSED buckets), not
// three 5m bars standing in for it — he says "mình nhắm theo khung 15 phút…
// đánh theo khung 15 phút thật sự". A live 5m ISB and a live 15m ISB with
// OPPOSITE directions → no trade at all [D4.2 p1 @ 14:35 "làm ơn đừng trade
// luôn… 2 khung giờ lớn đang ngược chiều nhau"]. Both directions are the
// INSIDE candle's colour (candle 1; ISBDirection), doji = no ISB at all.
// The conflict is ISB-path only — its call site runs inside the IsISB gate.
func ISBConflictVerdict(bars5m, bars15m []market.Kline) bool {
	if len(bars5m) < 2 || len(bars15m) < 2 {
		return false
	}
	p5, c5 := bars5m[len(bars5m)-2], bars5m[len(bars5m)-1]
	p15, c15 := bars15m[len(bars15m)-2], bars15m[len(bars15m)-1]
	if !IsISB(p5, c5) || !IsISB(p15, c15) {
		return false
	}
	return ISBDirection(p5) != ISBDirection(p15)
}

// ── mid-range (§12) [D3.2 p2 @ 08:34; D3.4 p3 @ 01:44] ───────────────────────
// The middle of any range: between two levels → PHL/PLH banned, ISB only.

// MidRange reports price sitting between two levels, each within
// cfg.RangeGapPts of price (RangeGapPts 0 disables the filter).
func MidRange(levels []Level, price float64, cfg Config) bool {
	if cfg.RangeGapPts <= 0 {
		return false
	}
	above, below := false, false
	for _, l := range levels {
		// REL-5 audit #1: target-only levels (4h trigger, wick microscalp) and
		// trendlines are never range boundaries — they are locations or target
		// ladders, not the key levels a mid-range sits between.
		if l.Kind == KindHTFTrigger || l.Kind == KindWickMicroscalp || l.Kind == KindTrendline || l.Kind == KindTrendlineTarget {
			continue
		}
		d := l.Price - price
		switch {
		case d > 0 && d <= cfg.RangeGapPts:
			above = true
		case d < 0 && -d <= cfg.RangeGapPts:
			below = true
		}
	}
	return above && below
}

// SetupPermittedVerdict gates a setup kind by the mid-range filter: in the
// middle of a range only the ISB survives [D3.2 p2 @ 08:34; D2.2 p3 @14:03–16:03
// "Ở giữa rank không được đánh potential high low, chỉ được đánh insaba"].
func SetupPermittedVerdict(kind string, levels []Level, price float64, cfg Config) (ok bool, reason string) {
	if !MidRange(levels, price, cfg) {
		return true, ""
	}
	if kind == "ISB" {
		return true, ""
	}
	return false, "mid-range: PHL/PLH banned, inside bar only [D3.2 p2 @ 08:34]"
}
