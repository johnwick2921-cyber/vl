package mentor

import (
	"vl/market"
)

// Filters is the direction-and-context gate set (PLAN v1 §4). Every filter is
// pure: bars/levels in, a verdict out.

// ── 5-minute trigger line [§5.1, D3.4 p1 @ 04:02–13:33] ─────────────────────
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

// applyBreak runs the §5.1 break logic for one bucket against the previous
// adjacent bucket (B3: every reversal moves the line ONCE — later breaks in
// the SAME direction never move it; the NEXT reversal moves it again).
func applyBreak(next TriggerLine, before, cur market.Kline) TriggerLine {
	brokeHigh := cur.High > before.High
	brokeLow := cur.Low < before.Low
	if brokeHigh && brokeLow {
		// broke BOTH: draw both lines, keep the one price is NOW beyond
		// [D3.4 p2 @ 18:49].
		if cur.Close > before.High {
			brokeLow = false
		} else {
			brokeHigh = false
		}
	}
	switch {
	case next.Dir == "": // the FIRST break draws the line [@ 19:48]
		if brokeHigh {
			next.Dir, next.Price = SideLong, before.High
		} else if brokeLow {
			next.Dir, next.Price = SideShort, before.Low
		}
	case next.Dir == SideLong && brokeLow: // reversal: move the line once
		// B1 (10-03 ruling): ONE line, moved on a reversal — the old line is
		// gone, not kept as a second line [D3.4 p1 @11:11–12:40].
		next.Dir, next.Price = SideShort, before.Low
	case next.Dir == SideShort && brokeHigh: // reversal: move the line once
		next.Dir, next.Price = SideLong, before.High
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

// ── mid-range [§12, D3.2 p2 @ 08:34; D3.4 p3 @ 01:44] ───────────────────────
// The middle of any range: between two levels → PHL/PLH banned, ISB only.

// MidRange reports price sitting between two levels, each within
// cfg.RangeGapPts of price (RangeGapPts 0 disables the filter).
func MidRange(levels []Level, price float64, cfg Config) bool {
	if cfg.RangeGapPts <= 0 {
		return false
	}
	above, below := false, false
	for _, l := range levels {
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
// middle of a range only the ISB survives [D3.2 p2 @ 08:34].
func SetupPermittedVerdict(kind string, levels []Level, price float64, cfg Config) (ok bool, reason string) {
	if !MidRange(levels, price, cfg) {
		return true, ""
	}
	if kind == "ISB" {
		return true, ""
	}
	return false, "mid-range: PHL/PLH banned, inside bar only [D3.2 p2 @ 08:34]"
}
