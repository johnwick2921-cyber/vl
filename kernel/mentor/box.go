package mentor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"vl/kernel"
	"vl/market"
)

// §4.1 — FTGH / FTGL boxes [D3.2 p1 @ 01:21–04:25; D3.2 p2 @ 03:45–09:14],
// with the BOX RULING parts 1+2 (CTO 2026-10-03, verified against the course
// frame D3.3 FTGH/FTGL part1_06-25.jpg):
//
//   - Pair by ROLE — lows with lows (floor), highs with highs (ceiling) —
//     with NO price tolerance. Item 12 (CTO ruling 23:06Z): "the FAILURE
//     defines the box" — the partner is the next CONFIRMED two-sided swing
//     AFTER the extreme that FAILS to exceed it (a higher low / lower
//     high); the trade is the 3rd touch ("lần thứ 3 mới vô lệnh"). An
//     earlier adjacent decline bar is never the partner.
//   - "Mình lấy cái râu thấp nhất… của cái đáy trước đó… kéo lên tới… cái
//     BODY của cái đáy GẦN NHẤT" [Cách vẽ box @ 02:16–02:41] — FTGL: from
//     the LOWEST WICK of the extreme low up to the BODY of the later
//     higher low.
//   - "râu ở cái đỉnh cao nhất, và cái BODY của cái đỉnh GẦN NHẤT"
//     [@ 03:29] — FTGH: from the HIGHEST WICK of the extreme high down to
//     the BODY of the later lower high.
//   - Role detection [B]: a rolling 3-candle extreme — a low is a candle
//     whose low is the min of the last three lows; a high, the max of the
//     last three highs. This reproduces BOTH golden boxes on the 13-Sep
//     frame (±1 pt).
//   - Draw at once and EXTEND RIGHT. The box is reused indefinitely
//     [D3.2 p2 @ 06:21]. Valid by construction — no 3rd-touch TEST for the
//     box [@ 03:45]; the 3rd-touch rule belongs to the TRADE [@ 04:29],
//     exposed as Box.Touches.
//   - Deleted at the end of the day [D4.1 p2 @ 02:39–03:09 (end of day only)].
//     (An escaped BODY does NOT delete the box — see B4; the earlier "escape
//     deletes" gloss was stale.)
//   - "NEVER trade inside the box" [D3.2 p1 @ 06:59] → InsideAnyBox.
//   - "Draw TWO zones, never a third." [D3.2 p2 @ 09:14] → one floor +
//     one ceiling.
//   - TF: 1m REGULAR by default (the course frame outranks the 2025
//     extras); "2m" regular and "5mha" (5m Heikin Ashi) stay as knobs,
//     default off.

// BoxKind is which failure the zone names.
type BoxKind string

const (
	// FTGH — failure to go higher (ceiling; green→red).
	FTGH BoxKind = "ftgh"
	// FTGL — failure to go lower (floor; red→green).
	FTGL BoxKind = "ftgl"
)

// Level kinds for DS-103's location gate (box edges are one of the four
// setup locations).
const (
	KindFTGHEdge LevelKind = "ftgh_edge"
	KindFTGLEdge LevelKind = "ftgl_edge"
)

// Box is one drawn FTGH/FTGL zone. Never redrawn: once built, the edges do
// not move. Escaped boxes are deleted (dropped from the set).
type Box struct {
	Kind   BoxKind
	Top    float64 // FTGH: highest wick of the earlier high · FTGL: body of the nearest low
	Bottom float64 // FTGH: body of the nearest high · FTGL: lowest wick of the earlier low
	Key    string  // stable id "ftgh:<top>:<bottom>"
	// Touches counts closed candles whose wick reached an edge since the
	// box formed — the entry layer requires the THIRD touch [@ 04:29].
	Touches int
	// FormedAt is the bar index the box was drawn (extend-right origin):
	// the partner's confirmation bar (seq[nearest].idx + 1) — the box is
	// born when bottom/high 2's confirming bar closes, so neither pairing
	// candle nor its confirmation is ever walked as a return (item 12
	// ruling 23:06Z, folding B10 T1 + T3).
	FormedAt int
	// FormedAtMs is the OpenTime of the formation bar — the STABLE anchor
	// (UR-FIX U1): the live window slides (the provider returns the last
	// 2000 bars), so FormedAt the index shifts by 1 each new 1m bar. Every
	// tick the latched box re-resolves its index from FormedAtMs.
	FormedAtMs int64
	// Flipped marks a box whose BODY escaped (a closed candle's whole body
	// outside on the far side) — D14 "Uno Reverse" [slide 17; X2 @02:36–
	// 03:25]: "Kháng cự bị phá → sẽ thành hỗ trợ khi backtest. Hỗ trợ bị
	// phá → sẽ thành kháng cự khi backtest" — a broken FTGH becomes SUPPORT
	// and a broken FTGL becomes RESISTANCE. Returns are then evaluated from
	// the NEW side with the same reject rule and the same gates (HTF, ORB,
	// ping-pong). The edges do not move; only the role flips.
	Flipped bool
	// TF is the frame this box was DRAWN on when it differs from the
	// configured base frame — "2m" for a box drawn while the exec-frame
	// schedule trades the 2m (m410, Exec2mAfter30m). "" = the base frame
	// (Config.Box.TF): every box before m410, and every box while the knob is
	// OFF, so a knob-OFF run carries no new state. A "2m" box keeps FormedAt
	// as a 1m-window index (the walk and the D14 escape read the 1m), and its
	// touches count on closed 2m candles.
	TF string
}

// BoxCfg holds the §4.1 knobs.
type BoxCfg struct {
	// TF is the box timeframe: "1m" (default, the course frame), "2m"
	// (2025 extra), "5mha" (5m Heikin Ashi, 2025 extra). Default off for
	// the non-course options.
	TF string
	// TouchBandPts: a wick within this of an edge counts as a touch;
	// default 0.25.
	TouchBandPts float64
}

// DefaultBoxCfg returns the ruling defaults: 1m regular.
func DefaultBoxCfg() BoxCfg {
	return BoxCfg{TF: "1m", TouchBandPts: 0.25}
}

// swingPairAt is one detected role extreme with its defining bar index.
type swingPairAt struct {
	kind  kernel.LevelKind
	price float64
	idx   int
}

// HeikinAshi converts regular OHLC bars to Heikin Ashi (the 2025-extra box
// TF option): HAclose = (O+H+L+C)/4; HAopen = (prev HAopen + prev HAclose)/2
// (first HAopen = (O+C)/2); HAhigh = max(H, HAopen, HAclose);
// HAlow = min(L, HAopen, HAclose).
func HeikinAshi(bars []market.Kline) []market.Kline {
	out := make([]market.Kline, 0, len(bars))
	var prevOpen, prevClose float64
	for i, b := range bars {
		haClose := (b.Open + b.High + b.Low + b.Close) / 4
		haOpen := (b.Open + b.Close) / 2
		if i > 0 {
			haOpen = (prevOpen + prevClose) / 2
		}
		haHigh := b.High
		if haOpen > haHigh {
			haHigh = haOpen
		}
		if haClose > haHigh {
			haHigh = haClose
		}
		haLow := b.Low
		if haOpen < haLow {
			haLow = haOpen
		}
		if haClose < haLow {
			haLow = haClose
		}
		out = append(out, market.Kline{OpenTime: b.OpenTime, Open: haOpen, High: haHigh, Low: haLow, Close: haClose, CloseTime: b.CloseTime})
		prevOpen, prevClose = haOpen, haClose
	}
	return out
}

// barsForTF resolves the box timeframe from the 1m series: "1m" passes
// through; "2m" aggregates; "5mha" aggregates 5m then converts to Heikin
// Ashi.
func barsForTF(bars []market.Kline, tf string) []market.Kline {
	switch tf {
	case "2m":
		return dropFormingTail(bars, barsTF(bars, 2), 2)
	case "5mha":
		return HeikinAshi(barsTF(bars, 5))
	default:
		return bars
	}
}

// dropFormingTail drops the last aggregate bucket when the 1m series ends
// before that bucket's close — a half-formed candle must never be a swing, a
// confirming bar or a partner (m410: boxes read CLOSED 2m candles only).
func dropFormingTail(src, agg []market.Kline, tfMin int) []market.Kline {
	if len(src) == 0 || len(agg) == 0 {
		return agg
	}
	if src[len(src)-1].OpenTime+60_000 < agg[len(agg)-1].CloseTime+1 {
		return agg[:len(agg)-1]
	}
	return agg
}

// refreshBox re-resolves a latched box's window index, touch count and D14
// flip from its stable time anchor. A base-frame box (TF "") reads
// barsForTF(bars, cfg.TF) exactly as before m410. A "2m" box (m410) counts
// touches on closed 2m candles, but its FormedAt is the 1m index of the
// formation bucket's LAST minute and the D14 escape reads the 1m body close
// ("box escape = 1m body close" [D4.1 p1 @20:38–20:45]). Absent anchor → fail
// closed: the last known values stand.
func refreshBox(bars []market.Kline, b *Box, cfg BoxCfg) {
	if b.TF != "2m" {
		tfBars := barsForTF(bars, cfg.TF)
		if idx, ok := resolveBarIndex(tfBars, b.FormedAtMs); ok {
			b.Touches = countBoxTouches(tfBars, *b, idx, cfg)
			b.Flipped = escaped(tfBars, *b, idx)
			b.FormedAt = idx
		}
		return
	}
	tf2 := barsForTF(bars, "2m")
	i2, ok2 := resolveBarIndex(tf2, b.FormedAtMs)
	i1, ok1 := resolveBarIndex(bars, b.FormedAtMs)
	if !ok2 || !ok1 || i1+1 >= len(bars) {
		return
	}
	b.Touches = countBoxTouches(tf2, *b, i2, cfg)
	b.FormedAt = i1 + 1
	b.Flipped = escaped(bars, *b, b.FormedAt)
}

// boxEscapeOpenMs returns the OpenTime of the first closed candle after
// formation whose whole BODY closed outside the box — read on the box's own
// frame for a base-frame box and on the 1m for a "2m" box (the escape stays a
// 1m body close). ok=false when absent or not escaped.
func boxEscapeOpenMs(bars []market.Kline, old Box, baseCfg BoxCfg) (int64, bool) {
	if old.TF == "2m" {
		i1, ok := resolveBarIndex(bars, old.FormedAtMs)
		if !ok || i1+1 >= len(bars) {
			return 0, false
		}
		esc, ok := boxEscapeIndex(bars, old, i1+1)
		if !ok {
			return 0, false
		}
		return bars[esc].OpenTime, true
	}
	tfBars := barsForTF(bars, baseCfg.TF)
	idx, ok := resolveBarIndex(tfBars, old.FormedAtMs)
	if !ok {
		return 0, false
	}
	esc, ok := boxEscapeIndex(tfBars, old, idx)
	if !ok {
		return 0, false
	}
	return tfBars[esc].OpenTime, true
}

// swings3 is the role detector [B]: a rolling 3-candle extreme — candle i is
// a LOW when its low is the min of the last three lows, a HIGH when its high
// is the max of the last three highs. LEFT-only, deliberately: the NEAREST
// pairing swing keeps no right-side confirmation (B10 T1 ruling 21:00Z);
// only the EXTREME is confirmed, by swingConfirmed, at the pairing site.
func swings3(bars []market.Kline) []swingPairAt {
	var seq []swingPairAt
	for i := 2; i < len(bars); i++ {
		if bars[i].CloseTime == 0 {
			continue
		}
		lo, hi := bars[i-2].Low, bars[i-2].High
		for j := i - 1; j <= i; j++ {
			if bars[j].Low < lo {
				lo = bars[j].Low
			}
			if bars[j].High > hi {
				hi = bars[j].High
			}
		}
		switch {
		case bars[i].Low == lo && bars[i].Low < bars[i-1].Low && bars[i].Low < bars[i-2].Low:
			seq = append(seq, swingPairAt{kind: kernel.KindSWGL, price: bars[i].Low, idx: i})
		case bars[i].High == hi && bars[i].High > bars[i-1].High && bars[i].High > bars[i-2].High:
			seq = append(seq, swingPairAt{kind: kernel.KindSWGH, price: bars[i].High, idx: i})
		}
	}
	return seq
}

// swingConfirmed is the B10 T1 fractal right side (CTO ruling 21:00Z): a
// swing low is confirmed only when the NEXT closed bar has a HIGHER low (the
// spike failed to go lower); a swing high, only when the NEXT closed bar has
// a LOWER high. A one-way tape never confirms, so no swing and no box.
// Item 12 (CTO ruling 23:06Z) applies it to BOTH pairing swings — the
// EXTREME and its partner.
func swingConfirmed(bars []market.Kline, s swingPairAt) bool {
	if s.idx+1 >= len(bars) || bars[s.idx+1].CloseTime == 0 {
		return false
	}
	if s.kind == kernel.KindSWGL {
		return bars[s.idx+1].Low > s.price
	}
	return bars[s.idx+1].High < s.price
}

// BoxesBuild is the pure, deterministic box scan: role extremes pair with
// the NEAREST same-role extreme (in time, on the non-broken side) with NO
// tolerance; escaped boxes are dropped; only boxes of the current trading
// day survive. The same history always rebuilds the same set — a drawn box
// is never redrawn. At most one floor and one ceiling ("two zones").
func BoxesBuild(bars []market.Kline, cfg BoxCfg, now time.Time) []Box {
	if cfg.TF == "" {
		cfg = DefaultBoxCfg()
	}
	tfBars := barsForTF(bars, cfg.TF)
	seq := swings3(tfBars)
	today := tradingDayKey(now.In(ctime()))
	// B10 T2 (CTO 2026-10-03): pick the extreme among TODAY's swings only.
	// The slice is 1500 1m bars since A9; pairing against a yesterday extreme
	// and dropping the box at the day check left a day whose low sits above
	// yesterday's with no floor at all.
	var seqToday []swingPairAt
	for _, s := range seq {
		if tradingDayKey(time.UnixMilli(tfBars[s.idx].OpenTime).In(ctime())) == today {
			seqToday = append(seqToday, s)
		}
	}
	seq = seqToday
	var out []Box
	for _, role := range []kernel.LevelKind{kernel.KindSWGL, kernel.KindSWGH} {
		extreme := -1
		for i, s := range seq {
			if s.kind != role {
				continue
			}
			// B10 T1 (CTO ruling 21:00Z): only a CONFIRMED swing can be the
			// extreme — the next closed bar failed to break it. A one-way
			// tape has no confirmed swing, so no extreme and no box.
			if !swingConfirmed(tfBars, s) {
				continue
			}
			if extreme < 0 {
				extreme = i
				continue
			}
			if role == kernel.KindSWGL && s.price < seq[extreme].price {
				extreme = i
			}
			if role == kernel.KindSWGH && s.price > seq[extreme].price {
				extreme = i
			}
		}
		if extreme < 0 {
			continue
		}
		nearest := -1
		for i, s := range seq {
			// item 12 (CTO ruling 23:06Z, DAY-3 row 12): "the FAILURE
			// defines the box" — bottom/high 2 comes AFTER the extreme and
			// FAILS to exceed it, and the trade is the 3rd touch ("lần thứ
			// 3 mới vô lệnh"). The partner is the next CONFIRMED two-sided
			// swing AFTER the extreme that does not break it — a higher low
			// / lower high. An EARLIER adjacent decline bar is NEVER the
			// partner.
			if i == extreme || s.kind != role {
				continue
			}
			if s.idx <= seq[extreme].idx {
				continue // bottom/high 2 is LATER, never the previous bar
			}
			if !swingConfirmed(tfBars, s) {
				continue
			}
			// the nearest did not break the extreme
			if role == kernel.KindSWGL && s.price < seq[extreme].price {
				continue
			}
			if role == kernel.KindSWGH && s.price > seq[extreme].price {
				continue
			}
			if nearest < 0 {
				nearest = i
				continue
			}
			di := seq[i].idx - seq[extreme].idx
			if di < 0 {
				di = -di
			}
			dn := seq[nearest].idx - seq[extreme].idx
			if dn < 0 {
				dn = -dn
			}
			if di < dn {
				nearest = i
			}
		}
		if nearest < 0 {
			continue
		}
		b := boxFromPair(tfBars, seq[extreme], seq[nearest])
		if b == nil {
			continue
		}
		b.Touches = countBoxTouches(tfBars, *b, seq[extreme].idx, cfg)
		// B4 (10-03 ruling, PLAN.md: "NEVER deleted during the session… delete
		// at the end of the day" [D4.1 p2 @02:39–03:09]): an escaped body does
		// NOT delete the box — v3's "delete on escape" mixed in the 5m ISB box
		// rule and killed every box trade. The today-only candidate filter
		// above is the box's only death.
		b.Key = "ftgh:" + fnum(b.Top) + ":" + fnum(b.Bottom)
		if b.Kind == FTGL {
			b.Key = "ftgl:" + fnum(b.Top) + ":" + fnum(b.Bottom)
		}
		// item 12 (CTO ruling 23:06Z): FormedAt = bottom/high 2's
		// CONFIRMATION — the box is born when the partner's confirming bar
		// closes. The partner is always the LATER swing now, so this is
		// seq[nearest].idx + 1; the partner candle itself (bottom/high 2)
		// is a formation candle and is never walked as a return.
		b.FormedAt = seq[nearest].idx + 1
		b.FormedAtMs = tfBars[b.FormedAt].OpenTime // UR-FIX U1: the stable anchor
		// D14 "Uno Reverse" [slide 17; X2 @02:36–03:25]: a BODY escape flips
		// the role — a broken FTGH becomes support, a broken FTGL becomes
		// resistance. The box is NOT deleted (B4); it stays and trades the
		// new side with the same reject rule and gates.
		b.Flipped = escaped(tfBars, *b, b.FormedAt)
		out = append(out, *b)
	}
	return out
}

// latchBoxes pins the first box per role per trading day (D4.1-22):
// BoxesBuild re-picks the MOST extreme confirmed swing every tick, so a later
// higher high would rebuild the FTGH with a new key and drop the first box.
// The course says no — "không vẽ cái box khác… y nguyên đó tới cuối ngày"
// [D4.1 p2 @02:39–02:57]: the first box drawn for a role stands to the end of
// the day. Only the touch count keeps advancing (recomputed from the full
// history against the LATCHED edges), so a latched box never freezes its
// touches and never changes its key.
//
// N3 (release #23): the OUTPUT is the latch, never the fresh scan. `fresh`
// only latches a role that is not latched yet; EVERY latched role is refreshed
// on EVERY tick, with its touches and D14 flip refreshed from the window. It
// used to iterate `fresh` alone, so a latched box vanished on every tick whose
// scan lacked its role (10-14: the latched FTGL was absent on 50 ticks) — its
// edges stopped being locations and the interior ban stopped applying. B52
// (release #29, fold-551) narrows the output to the LIVE set (≤2 zones): the
// newest new-zone box + the zone it escaped from, or the first boxes before any
// escape.
func (e *Evaluator) latchBoxes(fresh []Box, bars []market.Kline, cfg BoxCfg) []Box {
	if e.State.LatchedBoxes == nil {
		e.State.LatchedBoxes = map[string]Box{}
	}
	for _, b := range fresh {
		role := string(b.Kind)
		if _, ok := e.State.LatchedBoxes[role]; ok {
			continue
		}
		e.State.LatchedBoxes[role] = b
		e.note("box latched %s %.2f–%.2f (formed %s CT) — stands to the 17:00 CT session roll, never redrawn [D4.1 p2 @02:39]", strings.ToUpper(role), b.Bottom, b.Top, kernel.TableTimeCT(time.UnixMilli(b.FormedAtMs)))
	}
	// Refresh the first boxes (touch count + D14 flip + window index).
	for _, kind := range []BoxKind{FTGL, FTGH} {
		role := string(kind)
		latched, ok := e.State.LatchedBoxes[role]
		if !ok {
			continue
		}
		// UR-FIX U1: the latched box re-resolves its formation index from the
		// TIME anchor every tick (the live window slides), then refreshes BOTH
		// the touch count AND the D14 flip. Absent anchor (the formation bar
		// scrolled out of the window) → fail closed: keep the last known
		// values, never recompute on a stale index.
		refreshBox(bars, &latched, cfg)
		e.State.LatchedBoxes[role] = latched // write the refreshed copy back
	}
	// B52 (release #29): after a standing box escapes, latch ONE new same-role
	// box outside the old range, per further escape. The old box keeps its
	// flipped role (D14); the new box is that role's active box.
	if e.Cfg.BoxNewZoneAfterEscape {
		e.latchPostEscapeBoxes(bars, cfg, e.boxBuildCfg(cfg))
	}
	// CTO ruling (fold-551): AT MOST TWO ZONES LIVE. After a new-zone latch the
	// live set is the NEW zone + the zone it escaped from (flipped); every
	// older zone (the old opposite box, earlier flipped boxes) retires — no
	// ladder ("đừng vẽ vùng thứ 3… rối lắm" [D3.2 p2 @09:47–09:51]).
	return liveBoxes(e.State.LatchedBoxes, e.State.PostEscapeBoxes)
}

// liveBoxes is the LIVE box set under the 2-zone ruling (fold-551): before any
// escape the first boxes (FTGL then FTGH); after a new-zone latch exactly the
// newest zone plus the zone it escaped from (the immediately-previous same-role
// post-escape box, else that role's first box). Every other box is retired from
// the live reads (inside_any_box / near_box / between-boxes). The ladder is
// still kept in State.PostEscapeBoxes for the replay and the latch counter.
func liveBoxes(latched map[string]Box, post []Box) []Box {
	if len(post) == 0 {
		out := make([]Box, 0, len(latched))
		for _, kind := range []BoxKind{FTGL, FTGH} {
			if b, ok := latched[string(kind)]; ok {
				out = append(out, b)
			}
		}
		return out
	}
	newest := post[len(post)-1]
	out := []Box{newest}
	for i := len(post) - 2; i >= 0; i-- {
		if post[i].Kind == newest.Kind {
			return append(out, post[i])
		}
	}
	if b, ok := latched[string(newest.Kind)]; ok {
		out = append(out, b)
	}
	return out
}

// latchPostEscapeBoxes (B52) refreshes the post-escape boxes and latches a new
// one for any role whose ACTIVE box has escaped. The active box is the newest
// post-escape box of that role, else the first-latched box. One box per escape:
// once latched, the new box is the active box and blocks another until IT
// escapes. Dedup by Key so a replay of the same tick never double-latches.
func (e *Evaluator) latchPostEscapeBoxes(bars []market.Kline, cfg, buildCfg BoxCfg) {
	// Refresh the post-escape boxes first (touches + D14 flip + window index).
	for i := range e.State.PostEscapeBoxes {
		refreshBox(bars, &e.State.PostEscapeBoxes[i], cfg)
	}
	for _, kind := range []BoxKind{FTGL, FTGH} {
		active, ok := e.activeBoxOf(kind)
		if !ok {
			continue
		}
		// F551-1(c): the seed skips an absent anchor — the tick must too (never
		// escape-check on a stale FormedAt).
		if _, escapedYet := boxEscapeOpenMs(bars, active, cfg); !escapedYet {
			continue
		}
		nb := boxAfterEscapeBuildFrame(bars, buildCfg, cfg, active)
		if nb == nil {
			continue
		}
		if e.hasPostEscapeBox(nb.Key) {
			continue
		}
		e.State.PostEscapeBoxes = append(e.State.PostEscapeBoxes, *nb)
		e.note("box new zone after escape: %s %.2f–%.2f (formed %s CT) — new zone outside the escaped %s %.2f–%.2f [B52 D3.4 p3 recovered2 @00:00–00:21]",
			strings.ToUpper(string(nb.Kind)), nb.Bottom, nb.Top,
			kernel.TableTimeCT(time.UnixMilli(nb.FormedAtMs)),
			strings.ToUpper(string(active.Kind)), active.Bottom, active.Top)
	}
}

// activeBoxOf returns the role's newest post-escape box, else its first-latched
// box. ok=false when the role has no box at all yet.
func (e *Evaluator) activeBoxOf(kind BoxKind) (Box, bool) {
	return activeBoxOfPair(e.State.LatchedBoxes, e.State.PostEscapeBoxes, kind)
}

// activeBoxOfPair is the pure form of activeBoxOf, shared with the seed replay.
func activeBoxOfPair(latched map[string]Box, post []Box, kind BoxKind) (Box, bool) {
	for i := len(post) - 1; i >= 0; i-- {
		if post[i].Kind == kind {
			return post[i], true
		}
	}
	if b, ok := latched[string(kind)]; ok {
		return b, true
	}
	return Box{}, false
}

// hasPostEscapeBox reports whether a post-escape box with the given Key is
// already latched.
func (e *Evaluator) hasPostEscapeBox(key string) bool {
	return postHasBox(e.State.PostEscapeBoxes, key)
}

// postHasBox is the pure form of hasPostEscapeBox, shared with the seed replay.
func postHasBox(post []Box, key string) bool {
	for _, b := range post {
		if b.Key == key {
			return true
		}
	}
	return false
}

// resolveBarIndex returns the index of the bar with the given OpenTime in the
// CURRENT window (UR-FIX U1/U2): the stable time anchor is resolved to the
// sliding window's index each tick. ok=false when the bar has scrolled out —
// callers fail closed.
func resolveBarIndex(bars []market.Kline, openTimeMs int64) (int, bool) {
	if openTimeMs == 0 || len(bars) == 0 {
		return 0, false
	}
	for i, b := range bars {
		if b.OpenTime == openTimeMs {
			return i, true
		}
	}
	return 0, false
}

// resolveBarIndexAtOrBefore returns the index of the NEWEST bar opened at or
// before openTimeMs in the current window — the BoxRefs walk watermark (P2-N2,
// release #23). An exact-match read restarted the walk at the box's formation
// whenever the watermark minute was missing from the window (a ring hole, or a
// seed watermark the ring never held) and so re-judged every old return; a
// watermark OLDER than the whole window (slid out) is still ok=false, and the
// walk restarts at formation exactly as before.
func resolveBarIndexAtOrBefore(bars []market.Kline, openTimeMs int64) (int, bool) {
	if openTimeMs == 0 || len(bars) == 0 || bars[0].OpenTime > openTimeMs {
		return 0, false
	}
	i := sort.Search(len(bars), func(i int) bool { return bars[i].OpenTime > openTimeMs })
	return i - 1, true
}

// boxFromPair builds the zone from the extreme and its nearest same-role
// swing: FTGH: [nearest high's upper body edge, extreme's highest wick];
// FTGL: [extreme's lowest wick, nearest low's lower body edge].
// nil when the zone is degenerate.
func boxFromPair(bars []market.Kline, s1, s2 swingPairAt) *Box {
	b1, b2 := bars[s1.idx], bars[s2.idx]
	if s1.kind == kernel.KindSWGH {
		top := b1.High // highest wick of the extreme high
		body := b2.Open
		if b2.Close > body {
			body = b2.Close // nearest high's upper body edge
		}
		if top <= body {
			return nil
		}
		return &Box{Kind: FTGH, Top: top, Bottom: body}
	}
	bottom := b1.Low // lowest wick of the extreme low
	body := b2.Open
	if b2.Close < body {
		body = b2.Close // nearest low's lower body edge
	}
	if body <= bottom {
		return nil
	}
	return &Box{Kind: FTGL, Top: body, Bottom: bottom}
}

// escaped reports whether a closed candle AFTER formation had its whole
// body outside the zone — "ESCAPE = the candle's BODY outside (whole candle
// better)" [D3.2 p1 @ 19:05].
func escaped(bars []market.Kline, b Box, formedAt int) bool {
	for _, c := range bars[formedAt+1:] {
		if c.CloseTime == 0 {
			continue
		}
		switch b.Kind {
		case FTGH:
			if c.Open > b.Top && c.Close > b.Top {
				return true
			}
		case FTGL:
			if c.Open < b.Bottom && c.Close < b.Bottom {
				return true
			}
		}
	}
	return false
}

// boxEscapeIndex returns the index of the FIRST closed candle after formation
// whose whole BODY closed outside the box on the far side (the escape that
// triggers B52). ok=false when the box has not escaped yet.
func boxEscapeIndex(bars []market.Kline, b Box, formedAt int) (int, bool) {
	for i := formedAt + 1; i < len(bars); i++ {
		c := bars[i]
		if c.CloseTime == 0 {
			continue
		}
		switch b.Kind {
		case FTGH:
			if c.Open > b.Top && c.Close > b.Top {
				return i, true
			}
		case FTGL:
			if c.Open < b.Bottom && c.Close < b.Bottom {
				return i, true
			}
		}
	}
	return 0, false
}

// boxAfterEscapeBuild (B52, release #29) builds ONE new same-role box from
// confirmed swings that form STRICTLY AFTER the standing box escaped, whose
// zone lies ENTIRELY OUTSIDE the old box's range — a new FTGL below the old
// floor, a new FTGH above the old ceiling. "khi nào nó rớt xuống đây, nó bật
// ngược lên, nó tạo cái failure to go lower dưới này nè… thì anh chị mới có
// được cái range giữa cái này và cái này" [D3.4 p3 recovered2 @00:00–00:21];
// "Đừng vẽ vùng thứ 3… Rối lắm" [D3.2 p2 @09:47–09:54] — a zone inside the
// range is refused. The construction is the SAME rule as BoxesBuild (two
// same-role swings, the second fails to break the first; wick of the first to
// the nearest body of the second). Returns nil when no such box exists yet.
func boxAfterEscapeBuild(bars []market.Kline, cfg BoxCfg, old Box) *Box {
	return boxAfterEscapeBuildFrame(bars, cfg, cfg, old)
}

// boxAfterEscapeBuildFrame is boxAfterEscapeBuild with the two frames named:
// buildCfg is the frame the NEW box is drawn on (m410: the 2m while the exec
// schedule trades it), baseCfg the configured base frame the OLD box was read
// on when it carries no TF. With buildCfg == baseCfg it is byte-identical to
// the pre-m410 build.
func boxAfterEscapeBuildFrame(bars []market.Kline, buildCfg, baseCfg BoxCfg, old Box) *Box {
	cfg := buildCfg
	tfBars := barsForTF(bars, cfg.TF)
	escMs, ok := boxEscapeOpenMs(bars, old, baseCfg)
	if !ok {
		return nil
	}
	esc, ok := resolveBarIndex(tfBars, escMs)
	if !ok {
		// the escaping 1m bar sits inside a 2m bucket: the swing must open
		// strictly AFTER it — the first bucket opening after escMs.
		esc = sort.Search(len(tfBars), func(i int) bool { return tfBars[i].OpenTime > escMs }) - 1
	}
	role := kernel.KindSWGL
	if old.Kind == FTGH {
		role = kernel.KindSWGH
	}
	seq := swings3(tfBars)
	extreme := -1
	for i, s := range seq {
		if s.idx <= esc || s.kind != role {
			continue
		}
		if !swingConfirmed(tfBars, s) {
			continue
		}
		if extreme < 0 {
			extreme = i
			continue
		}
		if role == kernel.KindSWGL && s.price < seq[extreme].price {
			extreme = i
		}
		if role == kernel.KindSWGH && s.price > seq[extreme].price {
			extreme = i
		}
	}
	if extreme < 0 {
		return nil
	}
	nearest := -1
	for i, s := range seq {
		if i == extreme || s.kind != role {
			continue
		}
		if s.idx <= seq[extreme].idx {
			continue
		}
		if !swingConfirmed(tfBars, s) {
			continue
		}
		if role == kernel.KindSWGL && s.price < seq[extreme].price {
			continue
		}
		if role == kernel.KindSWGH && s.price > seq[extreme].price {
			continue
		}
		if nearest < 0 {
			nearest = i
			continue
		}
		di := seq[i].idx - seq[extreme].idx
		if di < 0 {
			di = -di
		}
		dn := seq[nearest].idx - seq[extreme].idx
		if dn < 0 {
			dn = -dn
		}
		if di < dn {
			nearest = i
		}
	}
	if nearest < 0 {
		return nil
	}
	b := boxFromPair(tfBars, seq[extreme], seq[nearest])
	if b == nil {
		return nil
	}
	// Outside the old range only — never a zone inside the standing range.
	if old.Kind == FTGL && b.Top > old.Bottom {
		return nil
	}
	if old.Kind == FTGH && b.Bottom < old.Top {
		return nil
	}
	b.Touches = countBoxTouches(tfBars, *b, seq[extreme].idx, cfg)
	b.Key = "ftgh:" + fnum(b.Top) + ":" + fnum(b.Bottom)
	if b.Kind == FTGL {
		b.Key = "ftgl:" + fnum(b.Top) + ":" + fnum(b.Bottom)
	}
	b.FormedAt = seq[nearest].idx + 1
	b.FormedAtMs = tfBars[b.FormedAt].OpenTime
	b.Flipped = escaped(tfBars, *b, b.FormedAt)
	if buildCfg.TF != baseCfg.TF && buildCfg.TF == "2m" {
		b.TF = "2m"
		refreshBox(bars, b, baseCfg) // FormedAt/Flipped onto the 1m window
	}
	return b
}

// countBoxTouches counts closed candles after formation whose wick reached
// an edge — the third touch is the trade [D3.2 p1 @ 04:29].
func countBoxTouches(bars []market.Kline, b Box, formedAt int, cfg BoxCfg) int {
	n := 0
	for _, c := range bars[formedAt+1:] {
		if c.CloseTime == 0 {
			continue
		}
		if touchesEdge(b, c, cfg) {
			n++
		}
	}
	return n
}

// touchesEdge reports a wick touch of either edge within TouchBandPts.
func touchesEdge(b Box, c market.Kline, cfg BoxCfg) bool {
	return touchesEdgeKind(b.Kind, b, c, cfg)
}

// touchesEdgeKind is touchesEdge with the role made explicit — D14 "Uno
// Reverse": the return walk evaluates the box's CURRENT role (a flipped FTGH
// touches like an FTGL), while the third-touch count keeps the original.
func touchesEdgeKind(k BoxKind, b Box, c market.Kline, cfg BoxCfg) bool {
	switch k {
	case FTGH:
		if abs(c.High-b.Top) <= cfg.TouchBandPts {
			return true
		}
		return c.High >= b.Bottom && c.Low <= b.Bottom+cfg.TouchBandPts
	case FTGL:
		if abs(c.Low-b.Bottom) <= cfg.TouchBandPts {
			return true
		}
		return c.Low <= b.Top && c.High >= b.Top-cfg.TouchBandPts
	}
	return false
}

// BoxEdgeLocations exports each live box's edges as locations for DS-103's
// location gate (the box edge is one of the four setup locations).
func BoxEdgeLocations(boxes []Box) []Level {
	var out []Level
	for _, b := range boxes {
		out = append(out,
			Level{Key: b.Key + ":top", Kind: KindFTGHEdge, Price: b.Top},
			Level{Key: b.Key + ":bottom", Kind: KindFTGLEdge, Price: b.Bottom},
		)
	}
	return out
}

// InsideAnyBox is the §12 ban: "NEVER trade inside the box — 'hoàn toàn
// không' — neither the candle nor your entry point" [D3.2 p1 @ 06:59].
func InsideAnyBox(boxes []Box, price float64) bool {
	for _, b := range boxes {
		if price > b.Bottom && price < b.Top {
			return true
		}
	}
	return false
}

// nearBoxRefusal implements Day-3 row 24 [D3.2 p1 @ 21:53–23:08]: a setup
// too close to a box is refused. The nearest box edge STRICTLY in the trade
// direction (long → the closest edge ABOVE; short → the closest edge BELOW)
// must be at least roomMultiple × risk points away, else the setup is "sát
// box" and skipped. Row 25 [@ 23:14–24:03]: "nằm ở GIỮA 2 box" — exempt ONLY
// between two DIFFERENT boxes: one wholly above (Bottom > price) AND another
// wholly below (Top < price). A price inside a single box is NOT exempt. No
// edge in the trade direction → allowed. roomMultiple <= 0 or risk <= 0
// disables the check.
func nearBoxRefusal(boxes []Box, price float64, side Side, roomMultiple, risk float64) (refuse bool, why string) {
	if roomMultiple <= 0 || risk <= 0 {
		return false, ""
	}
	wholeAbove, wholeBelow := false, false
	nearest := 0.0
	found := false
	for _, b := range boxes {
		if b.Bottom > price {
			wholeAbove = true // a box lies wholly above the entry
		}
		if b.Top < price {
			wholeBelow = true // a box lies wholly below the entry
		}
		for _, edge := range []float64{b.Top, b.Bottom} {
			var dist float64
			switch side {
			case SideLong:
				if edge > price {
					dist = edge - price
				} else {
					continue
				}
			case SideShort:
				if edge < price {
					dist = price - edge
				} else {
					continue
				}
			default:
				continue
			}
			if !found || dist < nearest {
				nearest, found = dist, true
			}
		}
	}
	if wholeAbove && wholeBelow {
		return false, "" // row 25: between two DIFFERENT boxes — exempt
	}
	if !found {
		return false, ""
	}
	if nearest < roomMultiple*risk {
		return true, fmt.Sprintf("near box: nearest box edge %.2f pts < %.2f pts room (%gx risk)", nearest, roomMultiple*risk, roomMultiple)
	}
	return false, ""
}

// structureTrend5m is the CE-8 trend read (release #25, knob
// d43_trend_source "structure"): the 5m swing STRUCTURE read left to right —
// "Nhìn từ bên trái qua bên phải… Đỉnh cao, đỉnh thấp hơn, đỉnh thấp hơn — thì
// nó đang là xu hướng gì — Giảm" [D4.3 @12:43–12:54]. It takes the last two
// CONFIRMED swing highs and the last two confirmed swing lows on the closed 5m
// candles (swings3 + swingConfirmed, the box machinery's detector): higher
// high AND higher low → long; lower high AND lower low → short; anything else
// (mixed, or fewer than two of each) → "" = no trend, so no box return is
// refused for being against it.
func structureTrend5m(closed5m []market.Kline) Side {
	var highs, lows []float64
	for _, sw := range swings3(closed5m) {
		if !swingConfirmed(closed5m, sw) {
			continue
		}
		if sw.kind == kernel.KindSWGH {
			highs = append(highs, sw.price)
		} else {
			lows = append(lows, sw.price)
		}
	}
	if len(highs) < 2 || len(lows) < 2 {
		return ""
	}
	h1, h2 := highs[len(highs)-2], highs[len(highs)-1]
	l1, l2 := lows[len(lows)-2], lows[len(lows)-1]
	switch {
	case h2 > h1 && l2 > l1:
		return SideLong
	case h2 < h1 && l2 < l1:
		return SideShort
	}
	return ""
}
