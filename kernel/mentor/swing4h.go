package mentor

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vl/kernel"
	"vl/market"
)

// §8 — THE SWING SETUP, 4-hour EMA 34 → 5-minute [D5.2 p1 @04:36], with the corrected
// §3 (reject = close back on the APPROACH side; close through = wrong way)
// and the RULES-FIX v3 corrections (R8):
//
//  1. "On the 4-hour, draw the line at EMA 34 BEFORE price gets there"
//     [D5.2 p1 @ 04:36, 11:04]. The line is the EMA 34 known at the START of
//     the current 4h bar: closed buckets only, and the line FLIPS when a 4h
//     candle finishes — "do not wait for a retest" [p3 @ 12:30].
//  2. "Switch STRAIGHT DOWN TO THE 5-MINUTE — not the 1-minute" [p1 @ 05:30].
//  3. The touch is LITERAL (m463, D5.2-R12): "Không, phải đụng nha, không được
//     gần đụng. Bắt buộc là phải đụng" [D5.2 p2 @09:12–09:27, @18:27–18:48;
//     "đụng ít đụng nhiều gì vẫn phải đụng" @13:56–14:05]. The candle's range
//     must reach the line itself; a wick into it counts (R13). The old 5-pt
//     "placement offset toward the approach" (LineOffsetPts) is GONE — it armed
//     trade 707 off a candle 1.28 pts short of the line. The 5–10 pt "trừ hao"
//     of [p2 @21:41–22:22] (R15) is the mentor's drawing tolerance on an
//     unzoomed 4H chart, not a touch allowance: the bot's line is the exact
//     EMA. A near miss inside the removed band is NAMED (swing_near_touch).
//  4. Only the FIRST touching candle counts, with 2 candles of leeway (m463,
//     D5.2-R19/R20): "lúc nào cũng phải lấy cây nến đầu tiên. Đừng bao giờ lấy
//     cây nến thứ 2" [p1 @13:23–13:40]; "em sẽ du di là 2 cây nến" [p1
//     @15:37–15:51]. The 4h candle's touch window is its first LeewayCandles
//     touching 5m candles: the entry may be armed off either; once the window
//     is spent, NO further plain reject in that 4h candle however far price
//     travels in between (swing_not_first_touch). The old "left the line by
//     the stop distance → re-arm" guard (ClearLongAt/ClearShortAt) is gone —
//     it is what re-armed trades 709/710.
//  5. Two cases [D5.2 p3 @19:15–21:56] (R21/R22):
//   - Touch, closes BACK on the approach side (a REJECT) → stop order on that
//     candle's extreme, stop ~30 beyond the line (R18/R21/R28).
//   - A close BEYOND the line — the touching candle's own close, any later
//     close of the window, a gap over the line — makes the level INVALID for
//     the rest of the 4h candle: "không chạm vô nữa, không đụng vô nữa"
//     (R22/R23). Then ONLY a 5m inside bar back on the trade side may enter
//     (R24, no time limit R25), stop AT the line (R29); every reject-shaped
//     touch is refused (swing_level_invalid). Candle colour does not matter
//     (R26, [p3 @06:24–06:52] "không quan trọng màu").
//  6. Stop of ~100 → "đừng vô" (m463, D5.2-R31, REVERSES m186): a swing whose
//     stop is ABOVE SkipStopPts (knob mentor_swing_max_stop_pts, default 90 —
//     his own example is 91.75 [p2 @19:34–21:43]) is refused (swing_max_stop),
//     on the reject and the inside-bar path alike. 30–40 / 50–60 are fine. The
//     swing stays EXEMPT from the ISB ceiling (mentor_isb_max_pts, 25).
//  7. Targets [table]: first = "EMA 34 on the 5-minute" (or the 50-pt
//     fallback); BE at +1R [D1.2 p1 @12:34–13:12, @15:56–15:59 — the general
//     1:1 maintenance]; hold ONE 4h candle [D5.2 p1 @06:03–06:20].
//  8. The 4h candle closes → the instance is over (R27): the line re-bases,
//     the window resets, and an unfilled order expires at the candle's close —
//     the MARKET close when the CME shuts inside the candle (13:00 CT bucket →
//     16:00), never inside the break (trade 678, swingExpiry).
//  9. Direction is the APPROACH SIDE to the 4H EMA 34 line, not the 5m trend
//     (R7/R35, [D5.2 p3 @15:04–18:25, @22:21–24:24]): price below the line =
//     resistance → short, above = support → long; no 5m-trend filter. The
//     EMA's SLOPE is NOT read: R7's worked cases are the aligned ones ("price
//     below a FALLING EMA 34"), and the course shows no case of price below
//     a RISING EMA, so a counter-slope approach trades by its side like any
//     other (swingApproach) — an open question for the mentor, not a rule.
//
// Hard invariant: the stop must sit on the correct side of the entry, else
// no intent is emitted — logged and counted (swingInvalidStops).

// Swing-specific intents (the package's Action set is extended here).
const (
	// ActionMoveStopBE asks the injector to move the stop to break-even.
	ActionMoveStopBE Action = "move_stop_to_break_even"
	// ActionClosePosition asks the injector to close the position.
	ActionClosePosition Action = "close_position"
)

// SwingCfg holds the §8 knobs.
type SwingCfg struct {
	EMAPeriod         int     // 34
	StopBeyondLinePts float64 // clean-rejection stop distance; default 30 (R8: 30–60 allowed)
	// MaxStopPts is the SIZING ceiling (risk_control.mentor_tuning.swing_max_stop_pts,
	// default 100) the trader reads in mentorSwingCeiling: a swing stop >= it
	// goes out at <= 2 MNQ. It is never a skip — SkipStopPts is (m463). With the
	// default skip 90 a stop >= 100 is refused before sizing; the size cap only
	// bites when the owner raises mentor_swing_max_stop_pts above it.
	MaxStopPts float64
	// MinStopPts is the degenerate-geometry floor (W26 P2-4): a swing stop
	// NARROWER than this is refused. 0 = OFF (the course states no minimum —
	// only a 100-pt ceiling and "a stop near 100 → don't enter" [06-SIZING]).
	// R5: new behaviour behind a knob that defaults OFF.
	MinStopPts        float64
	EntryBufferPts    float64 // NO buffer — order sits tight on the candle; default 0 [D5.2 p2 @05:02–05:12]
	TargetEMA5mPeriod int     // first target EMA period on 5m; default 34
	// SkipStopPts (m463, D5.2-R31) — knob mentor_swing_max_stop_pts: a swing
	// (reject or inside bar) whose stop is ABOVE this is refused, NAMED
	// swing_max_stop: "100 điểm quá nhiều rủi ro… đừng vô" [D5.2 p2 @19:34–21:43]
	// (his example 91.75 pts); "30-40, 50-60 ok, vô 2 contracts được". Default
	// 90; ≤ 0 resolves to 90 (no OFF). REVERSES m186 (owner order 2026-11-01
	// "full of it", after being shown the quote).
	SkipStopPts float64
	// LeewayCandles is the reference-leeway knob [D5.2 p1 @15:21–15:48]: the
	// first OR second touching candle may be the reference (the 4h level's
	// touch window = the first LeewayCandles touching 5m candles). It is NOT
	// the ISB window — R34 rules that the ISB watch after a through-close
	// stays open until the 4h flip (no candle limit).
	LeewayCandles int
	Hold4hBars    int // hold to the close of the N-th 4h candle AFTER the entry candle (SwingHoldDeadline); default 1 [D5.2 p1 @06:03–06:20]
	// Respects5mZone — gate swing entries on the 5m trigger-line zone.
	// Default false: §8 is a self-contained 4h → 5m procedure; nothing in
	// D5.2 ties it to the 5m trigger lines. [C] not stated in the method —
	// DS-103 wires the gate at the call site.
	Respects5mZone bool
	// ISBCloseBackRequired (m466 P4, G63 D5.2-R26) — knob
	// mentor_swing_isb_close_back [D5.2-R26 p3 @04:49–05:09]: false =
	// not_required (the default) — after
	// an invalid cross the 5m inside bar ALONE is the setup, even while candle 1
	// and the inside bar still close beyond the line; the entry must sit on the
	// trade side of the line and the stop stays AT the line (R29). "chị vô đúng
	// mà… còn nếu chị đợi đóng ngược lại cũng được, không thành vấn đề, nhưng
	// chị đợi hơi lâu" [D5.2 p3 @04:49–05:09]; "tùy góc nhìn" [p3 @07:42–08:36].
	// true = required: candle 1 AND the inside bar must both close back (the
	// pre-#61 rule, R24 read strictly).
	ISBCloseBackRequired bool
	// ISBCancelOnCloseThrough (m466 P4, G66 D5.2-R25) — knob
	// mentor_swing_isb_keep, INVERTED so the zero value is the course: false =
	// keep ON (the default) — a resting INSIDE-BAR order is NOT cancelled by a
	// later 5m close through the line; it rests until it fills, the 4h candle
	// carrying the line closes (R27, the order's expiry) or the day/EOD rules
	// end it: "Dù nó có xuống 1 cây nữa, nó trồi được 5 cây nữa. Để cái cây
	// inside bar này cho em" [D5.2 p2 @10:23–10:51]. The close-through cancel
	// stays on the REJECT order (R22). true = keep OFF (the pre-#61 behaviour:
	// any resting swing order dies on a close through the line).
	ISBCancelOnCloseThrough bool
}

// Swing ISB close-back knob values (m466 P4), canonicalized where they enter
// (canon 28) by NormalizeSwingISBCloseBack.
const (
	SwingISBCloseBackNotRequired = "not_required" // default: the inside bar alone [D5.2-R26 p3 @04:49–05:09]
	SwingISBCloseBackRequired    = "required"     // pre-#61: candle 1 + the inside bar close back
)

// NormalizeSwingISBCloseBack canonicalizes mentor_swing_isb_close_back: "" →
// not_required (the course default); ok=false = an unknown value, which fails
// closed to not_required (the course) and is counted by the caller.
func NormalizeSwingISBCloseBack(v string) (mode string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", SwingISBCloseBackNotRequired:
		return SwingISBCloseBackNotRequired, true
	case SwingISBCloseBackRequired:
		return SwingISBCloseBackRequired, true
	}
	return SwingISBCloseBackNotRequired, false
}

// DefaultSwingCfg returns the §8 defaults.
func DefaultSwingCfg() SwingCfg {
	return SwingCfg{
		EMAPeriod:         34,
		StopBeyondLinePts: 30,
		MaxStopPts:        100, // the SIZING ceiling (<= 2 MNQ), never a skip
		MinStopPts:        0,   // W26 P2-4: OFF by default (R5)
		EntryBufferPts:    0,   // no buffer [D5.2 p2 @05:02–05:12]
		TargetEMA5mPeriod: 34,
		SkipStopPts:       90, // m463 D5.2-R31: refuse a swing stop > 90 [p2 @19:34–21:43]
		LeewayCandles:     2,
		Hold4hBars:        1,
	}
}

// swingInvalidStops counts the hard-invariant violations (log + counter).
var swingInvalidStops atomic.Int64

// swingMinStopRefused counts a swing skipped for a stop narrower than
// MinStopPts (W26 P2-4; OFF = 0 by default, so this stays 0 today).
var swingMinStopRefused atomic.Int64

// SwingInvalidStops exposes the counter for tests and telemetry.
func SwingInvalidStops() int64 { return swingInvalidStops.Load() }

// SwingMinStopRefused exposes the min-stop counter (W26 P2-4).
func SwingMinStopRefused() int64 { return swingMinStopRefused.Load() }

// swingMaxStopRefused counts a swing refused for a stop above SkipStopPts
// (m463, D5.2-R31).
var swingMaxStopRefused atomic.Int64

// SwingMaxStopRefused exposes the R31 counter for tests and telemetry.
func SwingMaxStopRefused() int64 { return swingMaxStopRefused.Load() }

// lateDaySwingTargetCap is the late-day swing target ceiling: the top of the
// mentor's 20–30 [D5.2 p2 @07:21] (CTO ruling m331 B3; was 25). A code
// constant, not a stored knob — every live trader resolves to it.
const lateDaySwingTargetCap = 30.0

// swingLateDayRRRefused counts a late-day swing refused (reason
// swing_late_day_rr): only the risk ≤ 0 backstop — a wide stop is never
// refused, its target stays at 1:1.
var swingLateDayRRRefused atomic.Int64

// SwingLateDayRRRefused exposes the counter for tests and telemetry.
func SwingLateDayRRRefused() int64 { return swingLateDayRRRefused.Load() }

// lastSessionBucket returns the OPEN (ms) of the LAST 4h trading bucket of the
// CME session containing now — 13:00 CT on a normal day (the 12:00 stub
// re-anchored to 13:00); the bucket holding the shortened close on an
// early-close day. [derived: D5.2 p2 @07:00–07:58 "market sắp đóng cửa" → the
// last 4h bucket]
func lastSessionBucket(now int64) int64 {
	st := kernel.SessionStateAt(time.UnixMilli(now))
	var closeMs int64
	if st.Class == kernel.SessionShortened && st.HasClose {
		closeMs = st.Close.UnixMilli()
	} else {
		start := kernel.CMESessionDayStart(time.UnixMilli(now))
		closeMs = start.AddDate(0, 0, 1).Add(-time.Hour).UnixMilli()
	}
	return kernel.FourHBucketStart(closeMs, nil)
}

// lateDaySwingCut applies the late-day swing target cut (CTO final ruling
// m331 B3). Two course constraints: cut the target to 20–30 pts near the
// close [D5.2 p2 @07:21], but 1:1 is mandatory in every situation
// [03-CHECKLIST.md:160; DAY-5.md:117 "big stop → keep 1:1"]. So inside the
// LAST 4h bucket the target distance becomes max(risk, min(dist, 30)): the cut
// goes only as far as the 1:1 floor allows. A distance already below risk is
// returned unchanged for the existing non-late 1:1 gate. The ONLY refusal is
// risk ≤ 0 (backstop). target/cut are PRICES; the cap applies to |target−entry|.
func lateDaySwingCut(entryBucket, now int64, side Side, entry, target, risk float64) (cut float64, late, ok bool) {
	if entryBucket != lastSessionBucket(now) {
		return target, false, true
	}
	if risk <= 0 {
		return target, true, false
	}
	dist := abs(target - entry)
	if dist < risk {
		return target, true, true
	}
	cutDist := dist
	if cutDist > lateDaySwingTargetCap {
		cutDist = lateDaySwingTargetCap
	}
	if cutDist < risk {
		cutDist = risk // 1:1 floor
	}
	if side == SideShort {
		return entry - cutDist, true, true
	}
	return entry + cutDist, true, true
}

// SwingState is everything the swing machine carries between ticks. It is
// rebuildable: replaying the same 5m series from a zero state reproduces it.
type SwingState struct {
	Line        float64 // the 4h EMA 34 in force
	BucketStart int64   // ms, the 4h bucket the line belongs to
	LastBarTime int64   // ms, last closed 5m bar processed
	FirstTouch  *swingTouch
	EmaCount    int            // 4h closes consumed by Line — ≥ FourHEMA34Min means warm (seed path)
	Pending     *swingPosition // S1: the resting stop order, not yet filled
	Pos         *swingPosition // S1: the FILLED swing leg (BE/hold management)
	// LineStale (release #22, F-3): a feed gap longer than the live slice
	// left 4h closes the evaluator cannot read, so the warm line is no longer
	// the canonical EMA. SWING4H entries are refused (counted) until a
	// restart reseeds the line from the store — never the cold slice value.
	LineStale bool `json:"line_stale,omitempty"`
	// HoledBuckets (m456): starts (ms) of the open-session 4h buckets with no
	// bars that made the SEED mark the line stale. Display only.
	HoledBuckets []int64 `json:"-"`
	// Folded is the number of 4h closes the last flip folded (1 on contiguous
	// ticks; >1 after a feed gap) — read by runSwing for the change line.
	Folded int `json:"-"`
	// refused (m463) holds the swing's NAMED refusals of this tick; runSwing
	// drains it into the evaluator's refusal ledger + a note (never silent).
	refused []SwingRefusal
}

// SwingRefusal is one named swing refusal: Name is the ledger key, Why the
// note line (bar time, prices and the D5.2 rule ids).
type SwingRefusal struct {
	Name string
	Why  string
}

// Named swing refusals (m463) — the ledger keys runSwing counts.
const (
	SwingRefusedNearTouch     = "swing_near_touch"
	SwingRefusedNotFirstTouch = "swing_not_first_touch"
	SwingRefusedLevelInvalid  = "swing_level_invalid"
	SwingRefusedMaxStop       = "swing_max_stop"
	// SwingRefusedPositionOpen — an inside bar at an invalid level while this
	// level's swing position is still open: one setup per level [D5.2-R19/R23
	// p1 @13:23–13:40, p3 @19:15–21:56].
	SwingRefusedPositionOpen = "swing_position_open"
	// SwingRefusedCandleClosed — a setup on a 5m bar whose 4h candle had
	// already closed when the tick read it (the candle's last bar is first seen
	// at the 4h close): the instance is over [D5.2-R27 p3 @12:16–13:18].
	SwingRefusedCandleClosed = "swing_candle_closed"
	// SwingRefusedISBSpent — an inside bar at an invalid level whose inside-bar
	// order already FILLED in this 4h candle: the level gave its trade [D5.2-R19/R23 p1 @13:23–13:40, p3 @19:15–21:56].
	SwingRefusedISBSpent = "swing_isb_spent"
	// SwingRefusedISBEntryBeyondLine (m466 P4, G63 D5.2-R26) — under
	// mentor_swing_isb_close_back not_required an inside bar may form while
	// price is still beyond the line, but its entry must sit on the TRADE side
	// of the line ("cái điểm entry ở inside bar, basically nó vẫn nằm trên line"
	// [D5.2 p3 @07:42–08:36]) — the stop is AT the line (R29). An inside bar
	// whose stop order would rest beyond the line is refused, NAMED; it does not
	// spend the level (R25: a later inside bar may enter).
	SwingRefusedISBEntryBeyondLine = "swing_isb_entry_beyond_line"
)

type swingTouch struct {
	RefBar   market.Kline // the first touching 5m candle
	Approach Side         // price came from this side: SideLong = from above
	Through  bool         // the touch closed THROUGH the line
	// ISBFired latches that the inside-bar entry of this invalid level has
	// been armed (release #52 W5, A6). Through stays set after it, so a later
	// wick-touch-and-close-back in the same 4h candle can never re-arm a normal
	// reject at a level the mentor declared invalid; the latch also stops the
	// ISB itself from re-arming every bar. Cleared with FirstTouch at the 4h
	// flip. [D5.2 p2 @20:41-21:04 "cái level đó nó trở thành Invalid rồi thì
	// mình sẽ chỉ đi kiếm Insaba"]
	ISBFired bool `json:"isb_fired,omitempty"`
	// Touches counts the touching 5m candles this window has consumed [D5.2 p1
	// @15:21–15:48]: the 4h level's touch window is the first LeewayCandles
	// touching candles, and the entry may be armed off any of them.
	Touches int
}

type swingPosition struct {
	Side        Side
	Entry       float64
	Stop        float64
	Target      float64
	Risk        float64
	EntryBucket int64
	ArmID       string
	// MovedBE is TRUE only once the EXECUTOR reported the break-even APPLIED
	// (Evaluator.SwingBEApplied) — release #54 (N3): it used to be set at emit,
	// so a refused or failed move was never retried and the swing sat on its
	// full stop for the whole hold. A rebuilt position carries the broker truth.
	MovedBE bool
	// beEarned latches the first 5m CLOSE at/through +1R: from then on the BE
	// intent is re-emitted (once per closed 5m bar) until the executor reports it
	// applied or the position ended. Earning is a fact about the tape; applying
	// is a fact about the broker.
	beEarned bool
	// beTriedBar / closeTriedBar are the OPEN stamps of the last 5m bar a BE /
	// hold-close intent was emitted on — the retry runs once per closed 5m bar,
	// not on every 1m tick of the same bar.
	beTriedBar    int64
	closeTriedBar int64
	ExpiryMs      int64 // S1: the resting order's expiry (close of the current 4h candle)
	// ISB (m466 P4, G66 D5.2-R25) marks the INSIDE-BAR order of an invalid
	// level (vs the reject order): with mentor_swing_isb_keep ON (default) a
	// later 5m close through the line does not cancel it. Not serialized: the
	// state digests of the pinned tapes stay byte-identical under the pre-#61
	// knob values, and a resting order never survives a restart (only a FILLED
	// swing is rebuilt, from the broker — RebuildSwingPos).
	ISB bool `json:"-"`
}

// fourHBucketStart returns the start (ms) of the 4h bucket a bar belongs to.
// The ANCHOR itself lives in kernel.FourHBucketStart (one definition, shared
// with the planner's 4h candle table); this wrapper only adds the per-bar
// cache. The minute-keyed cache keeps the per-bar cost to a map lookup
// (barsTF(240) buckets every bar on every tick — without it the time.Date path
// made the 30-day replay 16x slower: 27s -> 437s). The key is the bar's UTC
// minute, so distinct buckets can never collide (a 17:00 CT bucket may straddle
// two UTC 4h spans).
var fourHBucketCache sync.Map // utcMinute -> bucket start ms

func fourHBucketStart(ot int64, loc *time.Location) int64 {
	if loc == nil {
		loc = ctime()
	}
	key := ot / 60_000
	if v, ok := fourHBucketCache.Load(key); ok {
		return v.(int64)
	}
	res := kernel.FourHBucketStart(ot, loc)
	fourHBucketCache.Store(key, res)
	return res
}

// swingLine computes the 4h EMA 34 known at the START of the current 4h bar:
// the EMA over closed 4h buckets (17:00 CT anchored), built from the closed
// 5m series.
func swingLine(bars5m []market.Kline, now int64, cfg SwingCfg, loc *time.Location) (line float64, bucketStart int64, ok bool) {
	if loc == nil {
		loc = ctime()
	}
	cur := fourHBucketStart(now, loc)
	var closes []float64
	var last int64
	for _, b := range bars5m {
		bs := fourHBucketStart(b.OpenTime, loc)
		if bs >= cur {
			continue // the current bucket is still open — never counted
		}
		if last == bs {
			closes[len(closes)-1] = b.Close // keep the bucket's last close
			continue
		}
		closes = append(closes, b.Close)
		last = bs
	}
	if len(closes) < 2 {
		return 0, cur, false
	}
	kbars := make([]market.Kline, len(closes))
	for i, c := range closes {
		kbars[i] = market.Kline{Close: c}
	}
	return emaValue(kbars, cfg.EMAPeriod), cur, true
}

// bucketClosesBetween returns, in time order, the close of every 4h bucket
// with a start in [from, to) that has bars in the slice (the last 5m bar of
// each) — the closes a warm line must fold on a flip. covered=false when the
// slice starts AFTER the from bucket, or a bucket in range was OPEN CME time
// with no bars (m456): buckets before the slice may have traded and cannot be
// read, so the caller must fail closed (release #22, F-3: one
// close per flip lost every bucket that opened and closed between two ticks).
func bucketClosesBetween(bars5m []market.Kline, from, to int64) (closes []float64, covered bool) {
	if len(bars5m) == 0 {
		return nil, false
	}
	if fourHBucketStart(bars5m[0].OpenTime, ctime()) > from {
		return nil, false
	}
	// m456: a bucket in [from, to) with no bars (or a missing tail) during an
	// OPEN session is a data hole, not a weekend: its fold is lost and the
	// caller must fail closed exactly like an unreadable bucket.
	if holes, _ := holedFourHBuckets(bars5m, 5*60_000, from, to); len(holes) > 0 {
		return nil, false
	}
	last := int64(-1)
	for _, b := range bars5m {
		bs := fourHBucketStart(b.OpenTime, ctime())
		if bs < from || bs >= to {
			continue
		}
		if bs == last {
			closes[len(closes)-1] = b.Close
			continue
		}
		closes = append(closes, b.Close)
		last = bs
	}
	return closes, true
}

// bucketClose returns the close of the closed 4h bucket starting at start
// (the last 5m bar inside it), 0 when absent.
func bucketClose(bars5m []market.Kline, start int64) float64 {
	var out float64
	for _, b := range bars5m {
		if fourHBucketStart(b.OpenTime, ctime()) == start {
			out = b.Close
		}
	}
	return out
}

// SwingTick advances the swing machine over the closed 5m bars since the
// last tick and returns the intents. Pure in (state, bars): the same tape
// rebuilds the same state.
func SwingTick(s *SwingState, bars5m []market.Kline, cfg SwingCfg, now int64) []Intent {
	if cfg.EMAPeriod <= 0 || cfg.StopBeyondLinePts <= 0 {
		cfg = DefaultSwingCfg()
	}
	loc := ctime()
	var out []Intent
	if s == nil {
		s = &SwingState{}
	}
	// S1: a resting swing order that reached its expiry without filling is
	// dropped — it can never grow into a phantom position that later closes
	// a different mentor trade (00-METHOD.md §8: "Enter on the 5-minute,
	// HOLD according to the 4-hour" [p1 @ 05:48] — the hold applies only to
	// a FILLED entry).
	//
	// m437 (release #53): the expiry is bucket end - 1ms, and the tick that
	// finalises the bucket's LAST 5m bar runs at now = that bar's CloseTime+1 =
	// the bucket end. Dropping Pending on `now` alone threw the order away
	// BEFORE the bar walk reached that last bar, yet the NT8 order is live until
	// its expiry — a trade-through on the last bar is a REAL fill that was then
	// never tracked (no BE, no hold-close). So the expiry is judged against the
	// BARS: the order is dropped only once its final bar has been walked (here,
	// for a carried-over expiry) or when the walk reaches a bar that OPENS after
	// the expiry (in the loop below, before that bar's fill check).
	s.expireSwingPending(now)
	warm := s.EmaCount >= FourHEMA34Min
	line, bucketStart, ok := swingLine(bars5m, now, cfg, loc)
	var prevBucket int64 // the 4h candle in force before this tick's flip (0 = no flip / no prior candle)
	if !ok {
		// Seeded warm line: the local slice is too short to recompute the 4h
		// EMA — KEEP the seeded line, never wipe a warm line to 0 (P0 seed).
		if warm {
			return out
		}
		s.Line, s.BucketStart = 0, bucketStart
		return out
	}
	if bucketStart != s.BucketStart {
		// rv59 (release #59 review, course-swing P2): the tick that first sees a
		// 4h candle's LAST 5m bar runs at now = that candle's end (m437), so the
		// bucket has already rolled. Those bars belong to the candle that just
		// closed: they are walked FIRST, against THAT candle's line and touch
		// window (the fill check, the R31 cancel of its resting order, the R22
		// through-close latch, every named refusal), and only then does the line
		// flip. A setup on them arms nothing — the order could only be placed at
		// the 4h close, when the instance is over (R27, swing_candle_closed). The
		// old order flipped first and read the closing bar against the NEXT
		// candle's line with a fresh window, arming a bar that never touched its
		// own line into the next candle.
		// Bars of 4h candles that opened AND closed unseen (a feed gap of 4h+)
		// are not walked for entries at all — the line in force was never
		// theirs, and their setups are as over as the closing candle's; they
		// still get the fill / cancel checks, and the skip is NAMED once.
		prevBucket = s.BucketStart
		if prevBucket != 0 && s.Line != 0 {
			for i := 1; i < len(bars5m); i++ {
				b := bars5m[i]
				if b.OpenTime <= s.LastBarTime || b.CloseTime == 0 {
					continue
				}
				if fourHBucketStart(b.OpenTime, loc) != prevBucket {
					break
				}
				out = append(out, s.stepSwingBar(bars5m, i, s.Line, cfg, now, swingBarClosedCandle)...)
			}
		}
		// A 4h candle finished: FLIP to the new line, do not wait for a
		// retest [D5.2 p3 @ 12:30]; the "used" approach resets (R8).
		// P0: a warm line continues the SAME EMA recurrence incrementally
		// (exact parity with a full-history rebuild) instead of a cold
		// recompute from the ~2-day slice.
		s.Folded = 0
		if warm {
			// F-3 (release #22): fold EVERY closed 4h bucket since the last
			// flip that has bars in the slice — not only the bucket the line
			// was in. A feed gap of 4h+ while the process stays up used to
			// drop the buckets that opened and closed unseen (+39.8 pts).
			// Buckets with no bars during a CLOSED session (a weekend, the
			// halt, a holiday) were never candles and fold nothing, exactly
			// like the seed's closed-bucket series. A bucket with no bars
			// while the session was OPEN is a data hole (m456): covered=false.
			closes, covered := bucketClosesBetween(bars5m, s.BucketStart, bucketStart)
			if !covered {
				// The slice no longer reaches the last folded bucket, or an
				// open-session bucket has no bars (m456): the missing closes
				// are unknowable here. Fail closed — keep the
				// last canonical value, refuse swing entries until a reseed.
				s.LineStale = true
			} else {
				k := 2.0 / float64(cfg.EMAPeriod+1)
				for _, nc := range closes {
					s.Line = s.Line + k*(nc-s.Line)
					s.EmaCount++
				}
				s.Folded = len(closes)
			}
		} else {
			s.Line = line
		}
		s.BucketStart = bucketStart
		s.FirstTouch = nil // R27: a new 4h candle → a new first-touch window
	}
	if warm {
		// The seeded recurrence has full history; the local slice is only a fallback.
		line = s.Line
	}

	unseen := 0
	for i := 1; i < len(bars5m); i++ {
		b := bars5m[i]
		if b.OpenTime <= s.LastBarTime || b.CloseTime == 0 {
			continue
		}
		mode := swingBarLive
		if prevBucket != 0 && fourHBucketStart(b.OpenTime, loc) < s.BucketStart {
			mode = swingBarUnseenCandle
			unseen++
		}
		out = append(out, s.stepSwingBar(bars5m, i, line, cfg, now, mode)...)
	}
	if unseen > 0 {
		s.refuse(SwingRefusedCandleClosed, "%d 5m bars of 4h candles that closed before this tick could read them (a feed gap, or the backlog before the first readable line) were not walked for swing entries — their instances are over [D5.2-R27 p3 @12:16–13:18]", unseen)
	}
	// m437: the expiry drop runs AFTER the walk — the bucket's last bar has now
	// had its fill check, so an order that did not fill is retired cleanly.
	s.expireSwingPending(now)
	// S1: BE/hold management runs AFTER the bar walk, so a fill that happened
	// THIS tick is managed against the fill bar's close — not one bar late.
	if s.Pos != nil {
		out = append(out, manageSwingPosition(s, bars5m, cfg)...)
	}
	return out
}

// swingBarMode says how stepSwingBar reads a bar (rv59).
type swingBarMode int

const (
	swingBarLive         swingBarMode = iota // the bar's 4h candle is the one in force
	swingBarClosedCandle                     // the closing candle's own bar, read at its close: walked, arms nothing
	swingBarUnseenCandle                     // a 4h candle that opened and closed unseen: fill/cancel checks only
)

// stepSwingBar consumes ONE closed 5m bar (bars5m[i]) against the line of the
// 4h candle it belongs to: the resting order's expiry / fill / R31 cancel, then
// the entry rules (walkSwingBar) per mode.
func (s *SwingState) stepSwingBar(bars5m []market.Kline, i int, line float64, cfg SwingCfg, now int64, mode swingBarMode) []Intent {
	var out []Intent
	b := bars5m[i]
	s.LastBarTime = b.OpenTime
	// m437: a bar that OPENS after the order's expiry can never fill it
	// (the order was cancelled at the expiry); every bar up to and
	// including the bucket's last 5m bar still gets the fill check below.
	if s.Pending != nil && s.Pending.ExpiryMs > 0 && b.OpenTime >= s.Pending.ExpiryMs {
		s.Pending = nil
	}
	// S1: the swing position opens only on the FILL of its own resting
	// order — the first bar that trades through the entry fills it.
	if s.Pending != nil && fillsSwingEntry(s.Pending, b) {
		s.Pos = s.Pending
		s.Pos.MovedBE = false
		s.Pending = nil
	}
	// R31 [D5.2 p1 @15:48–16:24 "1 cây nến sau đó đóng ở trên level…
	// không đặt lệnh"]: while a reject order rests, any later 5m close
	// THROUGH the line kills it — the level went invalid, so the stop
	// order must not keep holding the account slot against a line price
	// has already cut through. The fill check above wins: a filled order
	// is a position, not a resting order to cancel.
	//
	// m466 P4 (G66 D5.2-R25, knob mentor_swing_isb_keep, default ON): the
	// cancel is the REJECT order's rule (R22). An INSIDE-BAR order rests through
	// a later close beyond the line — "Dù nó có xuống 1 cây nữa, nó trồi được 5
	// cây nữa. Để cái cây inside bar này cho em" [D5.2 p2 @10:23–10:51] — until
	// it fills, the 4h candle carrying the line closes (R27: the expiry check
	// above) or the day/EOD rules end it. Keep OFF = the pre-#61 cancel.
	keepISB := s.Pending != nil && s.Pending.ISB && !cfg.ISBCancelOnCloseThrough
	if s.Pending != nil && !keepISB && closesThroughPendingLine(s.Pending.Side, b.Close, line) {
		out = append(out, Intent{
			Action: CancelArm,
			ArmID:  s.Pending.ArmID,
			Reason: "swing: a later 5m close cut through the line — cancel the resting order [D5.2 p1 @15:48–16:24]",
		})
		s.Pending = nil
		// rv59 (course-swing P2): an INSIDE-BAR order that dies unfilled does not
		// spend the level — the way back has no time limit [D5.2-R25 p1
		// @20:31–21:57 "xuất hiện inside bar, tôi vô lệnh"], so the next inside
		// bar of this 4h candle may enter. Only a FILL spends it (one setup per
		// level, D5.2-R19/R23). Under a Through latch the only order that can
		// rest is the inside bar's (the latch cancelled the reject order).
		if ft := s.FirstTouch; ft != nil && ft.Through && ft.ISBFired {
			ft.ISBFired = false
		}
	}
	if mode == swingBarUnseenCandle {
		return out
	}
	prev := bars5m[i-1]
	return append(out, s.walkSwingBar(prev, b, line, bars5m, cfg, now, mode == swingBarClosedCandle)...)
}

// walkSwingBar runs the D5.2 entry rules on one closed 5m bar b (prev = the
// bar before it) against the 4h line in force, after the fill / resting-order
// checks. It returns the bar's intents; every refusal it makes is NAMED in
// s.refused (m463 — never silent). candleClosed: b's 4h candle closed before
// this tick read it — a setup on b is refused (swing_candle_closed, R27).
func (s *SwingState) walkSwingBar(prev, b market.Kline, line float64, bars5m []market.Kline, cfg SwingCfg, now int64, candleClosed bool) []Intent {
	var out []Intent
	closedRefuse := func(what string, in Intent) {
		s.refuse(SwingRefusedCandleClosed, "%s CT %s %s %.2f off the 4h line %.2f: this 5m bar is the last of its 4h candle, read at the 4h close — the instance is over, nothing arms into the next candle [D5.2-R27 p3 @12:16–13:18]",
			swingCT(b.OpenTime), what, in.Side, in.Price, line)
	}
	cancelPending := func(reason string) {
		if s.Pending != nil {
			out = append(out, Intent{Action: CancelArm, ArmID: s.Pending.ArmID, Reason: reason})
			s.Pending = nil
		}
	}
	// D5.2-R22 [p3 @19:15–21:56; p1 @15:48–16:24]: once this 4h candle's touch
	// window exists, ANY 5m close beyond the line — the window's own touching
	// candle, a later bar, a gap over the line — makes the level INVALID for the
	// rest of the 4h candle. The old code latched only a TOUCHING through-close
	// read against the previous close, so a gap over the line (or a tie close on
	// the line) left the window open and the next bar from the other side armed
	// a reject in the opposite direction.
	if ft := s.FirstTouch; ft != nil && !ft.Through && !closedBack(ft.Approach, b.Close, line) {
		cancelPending("swing: a 5m close beyond the line — the level is invalid for this 4h candle, cancel [D5.2-R22 p3 @19:15–21:56]")
		s.FirstTouch = &swingTouch{RefBar: b, Approach: ft.Approach, Through: true, Touches: ft.Touches + 1}
		return out
	}
	// D5.2-R22..R25 — the level is INVALID: only the 5m inside bar may enter.
	if ft := s.FirstTouch; ft != nil && ft.Through {
		// R24/R26 [p1 @20:31–21:43 "đợi thêm 1 nhịp 5 phút"; p3 @06:24–06:52
		// "không quan trọng màu"]: candle 1 (prev) closed back on the trade side,
		// THEN b is its inside bar, also closing back. A6/A7 kept (the latched
		// invalid marker; the close-back bar is never its own inside bar); the
		// D1.4 colour clause (ISBDirection(prev) == Approach) is REMOVED — the
		// swing's direction is the 4H line's, not candle 1's colour.
		//
		// m466 P4 (G63 D5.2-R26, knob mentor_swing_isb_close_back): the two
		// close-back clauses are the REQUIRED option only. not_required (the
		// default) — the inside bar ALONE is the setup, even while price still
		// closes beyond the line: "Chị vô đúng mà… còn nếu chị đợi đóng ngược lại
		// cũng được, không thành vấn đề, nhưng chị đợi hơi lâu" [D5.2 p3
		// @04:49–05:09]; "tùy góc nhìn" [p3 @07:42–08:36]. Its entry must still
		// sit on the trade side of the line (checked below, named
		// swing_isb_entry_beyond_line), and the stop stays AT the line (R29).
		isInside := swingInsideBar(prev, b)
		if cfg.ISBCloseBackRequired {
			isInside = isInside && closedBack(ft.Approach, prev.Close, line) &&
				closedBack(ft.Approach, b.Close, line)
		}
		if isInside && ft.ISBFired {
			// rv59: an inside bar is never read as a reject shape (the
			// "only a 5m inside bar may enter" text on a bar that IS one).
			// The level's inside-bar order FILLED and that leg has ended →
			// the level gave its trade, NAMED swing_isb_spent. While this
			// level's order rests or its position is open, the level's setup
			// is live — that was named once when it latched (or it is the
			// inside bar's own trade); nothing more to refuse.
			if s.Pos == nil && s.Pending == nil {
				s.refuse(SwingRefusedISBSpent, "%s CT inside bar at the invalid 4h line %.2f: this level's inside-bar entry already filled in this 4h candle — one setup per level [D5.2-R19/R23 p1 @13:23–13:40, p3 @19:15–21:56]",
					swingCT(b.OpenTime), line)
			}
			return out
		}
		if isInside {
			// One setup per level [D5.2-R19/R23 p1 @13:23–13:40; p3
			// @19:15–21:56]: a close beyond the line ALSO latches Through after
			// this level's reject order FILLED. A second arm here would rest
			// beside the open leg and its fill would replace s.Pos (the first
			// leg's BE/hold tracking lost). Refused, NAMED, and the inside bar
			// is spent — the level already gave its trade.
			if s.Pos != nil || s.Pending != nil {
				open := s.Pending
				if s.Pos != nil {
					open = s.Pos
				}
				ft.ISBFired = true
				s.refuse(SwingRefusedPositionOpen, "%s CT inside bar at the invalid 4h line %.2f: the swing %s (%s %.2f) of this level is still open — one setup per level [D5.2-R19/R23 p1 @13:23–13:40, p3 @19:15–21:56]",
					swingCT(b.OpenTime), line, open.ArmID, open.Side, open.Entry)
				return out
			}
			if entry, onSide := swingISBEntryOnTradeSide(ft.Approach, b, line, cfg); !onSide {
				// m466 P4 (G63): only reachable under not_required — a closed-back
				// inside bar always reaches past the line. Not spent (R25).
				s.refuse(SwingRefusedISBEntryBeyondLine, "%s CT inside bar at the invalid 4h line %.2f: its %s stop order %.2f would rest BEYOND the line, where the stop AT the line cannot sit — \"cái điểm entry ở inside bar, basically nó vẫn nằm trên line\" [D5.2-R26/R29 p3 @07:42–08:36]",
					swingCT(b.OpenTime), line, ft.Approach, entry)
				return out
			}
			in, ok, why := swingISBIntent(ft, b, line, cfg)
			if !ok {
				// Not spent: R25 (no time limit) lets a later inside bar with a
				// stop inside the rules enter.
				s.refuse(why.Name, "%s CT inside bar: %s", swingCT(b.OpenTime), why.Why)
				return out
			}
			in.Target = swingFirstTarget(in.Side, in.Price, in.Stop, swingTargetEMA(bars5m, cfg))
			if cut, late, lok := lateDaySwingCut(s.BucketStart, b.OpenTime, in.Side, in.Price, in.Target, abs(in.Stop-in.Price)); late {
				if !lok {
					swingLateDayRRRefused.Add(1)
					s.refuse("swing_late_day_rr", "%s CT inside bar: late-day 1:1 cut impossible (risk %.2f)", swingCT(b.OpenTime), abs(in.Stop-in.Price))
					return out
				}
				in.Target = cut
			}
			if candleClosed {
				closedRefuse("inside bar", in)
				return out
			}
			ft.ISBFired = true // A6: Through stays latched; the ISB is spent
			in.ArmID = "swing-" + strconv.FormatInt(b.OpenTime, 10)
			s.openPosition(in, now)
			s.Pending.ISB = true // m466 P4 (G66): the inside-bar order (keep rule)
			return append(out, in)
		}
		// A reject-shaped touch at the invalid level (either side): refused,
		// NAMED — the 707 / 709 / 710 shape.
		if approach, ok := swingApproach(prev.Close, line); ok && closedBack(approach, b.Close, line) {
			if touchesLine(b, line) || nearTouch(b, line, approach) {
				s.refuse(SwingRefusedLevelInvalid, "%s CT %s reject shape at the 4h line %.2f, which a 5m close went through earlier in this 4h candle (%s CT) — invalid, only a 5m inside bar may enter [D5.2-R22/R23/R24 p3 @19:15–21:56, p1 @16:24–19:12]%s",
					swingCT(b.OpenTime), approach, line, swingCT(ft.RefBar.OpenTime), swingRefuseExtras(b, line, approach, cfg))
			}
		}
		return out
	}
	approach, hasApproach := swingApproach(prev.Close, line)
	if !hasApproach {
		return out // tie: prev closed exactly on the line — no approach yet
	}
	// D5.2-R12/R13: LITERAL touch — the candle's range must reach the line.
	if !touchesLine(b, line) {
		if closedBack(approach, b.Close, line) && nearTouch(b, line, approach) {
			gap := line - b.High
			if approach == SideLong {
				gap = b.Low - line
			}
			s.refuse(SwingRefusedNearTouch, "%s CT %s candle stopped %.2f pts short of the 4h line %.2f — not a touch: \"bắt buộc phải đụng, không được gần đụng\" [D5.2-R12 p2 @09:12–09:27]",
				swingCT(b.OpenTime), approach, gap, line)
		}
		return out
	}
	if !closedBack(approach, b.Close, line) {
		// The first touching candle closes THROUGH → INVALID, no order (R22).
		cancelPending("swing: touch closed through the line — cancel, the level is invalid for this 4h candle [D5.2-R22 p3 @19:15–21:56]")
		s.FirstTouch = &swingTouch{RefBar: b, Approach: approach, Through: true, Touches: 1}
		return out
	}
	// A reject: the first (or, within the leeway, the second) touching candle.
	touches := 1
	if ft := s.FirstTouch; ft != nil {
		if ft.Touches >= cfg.LeewayCandles {
			s.refuse(SwingRefusedNotFirstTouch, "%s CT %s reject at the 4h line %.2f is touching candle #%d of this 4h candle — only the first (2 of leeway) counts [D5.2-R19/R20 p1 @13:23–13:40, @15:37–15:51]",
				swingCT(b.OpenTime), approach, line, ft.Touches+1)
			ft.Touches++
			return out
		}
		touches = ft.Touches + 1
		// The second window candle supersedes only while NO position owns the
		// level — if the first arm already filled, there is nothing to re-base.
		if s.Pos != nil {
			ft.Touches = touches
			return out
		}
	}
	in, ok, why := swingRejectIntent(approach, b, line, cfg)
	if !ok {
		s.refuse(why.Name, "%s CT %s reject: %s", swingCT(b.OpenTime), approach, why.Why)
		s.FirstTouch = &swingTouch{RefBar: b, Approach: approach, Touches: touches} // the touch is spent
		return out
	}
	in.Target = swingFirstTarget(in.Side, in.Price, in.Stop, swingTargetEMA(bars5m, cfg))
	if cut, late, lok := lateDaySwingCut(s.BucketStart, b.OpenTime, in.Side, in.Price, in.Target, abs(in.Stop-in.Price)); late {
		if !lok {
			swingLateDayRRRefused.Add(1)
			s.refuse("swing_late_day_rr", "%s CT %s reject: late-day 1:1 cut impossible (risk %.2f)", swingCT(b.OpenTime), approach, abs(in.Stop-in.Price))
			s.FirstTouch = &swingTouch{RefBar: b, Approach: approach, Touches: touches}
			return out
		}
		in.Target = cut
	}
	if candleClosed {
		closedRefuse(string(approach)+" reject", in)
		s.FirstTouch = &swingTouch{RefBar: b, Approach: approach, Touches: touches}
		return out
	}
	in.ArmID = "swing-" + strconv.FormatInt(b.OpenTime, 10)
	// A later touching candle inside the window supersedes the earlier arm: the
	// entry re-bases on THIS candle's extreme [D5.2 p1 @15:21–15:48].
	cancelPending("swing: re-armed off a later touching candle in the leeway window — supersede the first arm [D5.2 p1 @15:21–15:48]")
	s.FirstTouch = &swingTouch{RefBar: b, Approach: approach, Touches: touches}
	s.openPosition(in, now)
	return append(out, in)
}

// refuse records one NAMED swing refusal (m463): runSwing drains them into the
// evaluator's refusal ledger and a note line.
func (s *SwingState) refuse(name, format string, args ...any) {
	s.refused = append(s.refused, SwingRefusal{Name: name, Why: name + ": " + fmt.Sprintf(format, args...)})
}

// TakeRefusals returns and clears the swing's named refusals.
func (s *SwingState) TakeRefusals() []SwingRefusal {
	r := s.refused
	s.refused = nil
	return r
}

// swingNearTouchBandPts is the width of the near-touch allowance m463 REMOVED
// (the old LineOffsetPts default). It is not a gate: a candle that comes this
// close without reaching the line is only NAMED (swing_near_touch) so the
// owner sees the literal-touch rule bite.
const swingNearTouchBandPts = 5.0

// touchesLine is the LITERAL touch (D5.2-R12/R13): the candle's range reaches
// the line — a wick into it counts.
func touchesLine(b market.Kline, line float64) bool {
	return b.Low <= line && b.High >= line
}

// nearTouch: the candle stopped short of the line on the approach side, by at
// most swingNearTouchBandPts.
func nearTouch(b market.Kline, line float64, approach Side) bool {
	if approach == SideShort { // came from below: the high falls short
		return b.High < line && line-b.High <= swingNearTouchBandPts
	}
	return b.Low > line && b.Low-line <= swingNearTouchBandPts
}

// swingRefuseExtras adds, to an invalid-level refusal, what ELSE the candle
// would have broken: a near miss (R12) and the reject stop it would have had
// against the 90-pt ceiling (R31).
func swingRefuseExtras(b market.Kline, line float64, approach Side, cfg SwingCfg) string {
	var x string
	if !touchesLine(b, line) {
		gap := line - b.High
		if approach == SideLong {
			gap = b.Low - line
		}
		x += fmt.Sprintf("; also no literal touch, %.2f pts short [D5.2-R12 p2 @09:12–09:27]", gap)
	}
	entry, stop := b.Low-cfg.EntryBufferPts, line+cfg.StopBeyondLinePts
	if approach == SideLong {
		entry, stop = b.High+cfg.EntryBufferPts, line-cfg.StopBeyondLinePts
	}
	if pts := abs(stop - entry); pts > swingSkipStopPts(cfg) {
		x += fmt.Sprintf("; the reject stop would be %.2f pts > %.0f [D5.2-R31 p2 @19:34–21:43]", pts, swingSkipStopPts(cfg))
	}
	return x
}

// swingSkipStopPts resolves SkipStopPts (≤ 0 → the 90 default; no OFF).
func swingSkipStopPts(cfg SwingCfg) float64 {
	if cfg.SkipStopPts > 0 {
		return cfg.SkipStopPts
	}
	return DefaultSwingCfg().SkipStopPts
}

// swingInsideBar is the swing's inside bar (D5.2-R24/R26): b's BODY inside
// prev's FULL range (the D1.4 geometry). Unlike IsISB, a doji candle 1 is NOT
// refused — IsISB refuses it because a doji has no colour and so no direction
// (D1.4 ruling P3), while the swing's direction comes from the 4H line and the
// course says the colour does not matter [D5.2 p3 @06:24–06:52].
func swingInsideBar(prev, b market.Kline) bool {
	return curHigh(b) <= prev.High && curLow(b) >= prev.Low
}

// swingCT renders a refusal note's bar time ("01-02 15:04"; every caller adds
// the " CT" label) through the kernel's single time source (TZ guard).
func swingCT(ms int64) string {
	return kernel.TableTimeCT(time.UnixMilli(ms))
}

// swingBarMs is the 5m bar length the swing machine walks.
const swingBarMs = 5 * 60_000

// expireSwingPending drops a resting swing order whose expiry has passed AND
// whose final bar has been walked (m437). The order is live at NT8 until its
// expiry (bucket end - 1ms), so the last 5m bar of the bucket — whose close is
// that same instant — can still fill it; a drop on the wall clock alone, before
// that bar's fill check, loses a real fill. LastBarTime is the open of the last
// bar the walk consumed, so "its close >= the expiry" means the order's final
// bar has had its fill check. An order whose final bar never arrives (a feed
// gap) lingers until the walk reaches a bar that opens after the expiry, which
// the loop turns into the drop — a late bar can never fill an expired order.
func (s *SwingState) expireSwingPending(now int64) {
	p := s.Pending
	if p == nil || p.ExpiryMs <= 0 || now < p.ExpiryMs {
		return
	}
	if s.LastBarTime+swingBarMs-1 >= p.ExpiryMs {
		s.Pending = nil
	}
}

// fillsSwingEntry reports whether a closed bar trades through the resting
// swing order's entry — the stop order fills when price reaches the trigger.
func fillsSwingEntry(p *swingPosition, b market.Kline) bool {
	if p == nil {
		return false
	}
	if p.Side == SideLong {
		return b.High >= p.Entry
	}
	return b.Low <= p.Entry
}

// closesThroughPendingLine reports whether a closed 5m bar cut through the
// line a resting swing order sits against [D5.2 p1 @15:48–16:24]: a short
// rests BELOW the resistance (a close above = through), a long rests ABOVE
// the support (a close below = through). Either invalidates the level.
func closesThroughPendingLine(side Side, close, line float64) bool {
	if side == SideShort {
		return close > line
	}
	return close < line
}

// openPosition records the RESTING swing order (not yet filled). S1: the
// swing position must open only on the FILL of its own ArmID — a resting,
// expired or cancelled order must never drive BE/hold management, or it
// would close a DIFFERENT mentor trade after the hold.
func (s *SwingState) openPosition(in Intent, now int64) {
	p := &swingPosition{Side: in.Side, Entry: in.Price, Stop: in.Stop, Target: in.Target, EntryBucket: s.BucketStart, ArmID: in.ArmID, ExpiryMs: swingExpiry(now)}
	// SWING-RISK (release #22): the ONE risk definition. It used to be
	// Stop−Price for a long and Price−Stop for a short — NEGATIVE for both — so
	// manageSwingPosition's "+1R" test passed on the fill bar itself.
	p.Risk = SwingRisk(in.Side, in.Price, in.Stop)
	s.Pending = p
}

// SwingRisk is THE swing risk: the distance from the entry to the stop the
// trade was entered with, on the LOSS side — long: entry − stop; short: stop −
// entry. Positive for a well-formed swing; ≤ 0 when the stop sits on the wrong
// side. One definition for both ways a swing position is made — the live fill
// (openPosition) and the restart rebuild (RebuildSwingPos) — so a swing reaches
// break-even at the same +1R whether or not the bot restarted in between.
func SwingRisk(side Side, entry, entryStop float64) float64 {
	if side == SideShort {
		return entryStop - entry
	}
	return entry - entryStop
}

// swingTargetEMA is the first target: the 5m EMA 34 [table]. R42 [D5.2 p2
// @01:32] — the EMA is warmed over the FULL closed 5m series, not the last 34
// bars seeded from a single close (the cold EMA was not the chart's 5m EMA 34).
func swingTargetEMA(bars5m []market.Kline, cfg SwingCfg) float64 {
	if len(bars5m) < cfg.TargetEMA5mPeriod {
		return 0
	}
	return emaValue(bars5m, cfg.TargetEMA5mPeriod)
}

// targetOnSide reports whether the EMA target sits on the profitable side of
// the entry (otherwise the 1R floor stands).
func targetOnSide(side Side, entry, ema float64) bool {
	if ema == 0 {
		return false
	}
	if side == SideLong {
		return ema > entry
	}
	return ema < entry
}

// swingFirstTarget is the swing's leg-1 target: max(1R, the warmed 5m EMA 34)
// on the profitable side [D5.2 p2 @11:00 "TARGET CỨ AT LEAST LÀ 1-1 TRƯỚC" —
// R43]. The 5m EMA 34 wins only when it sits FURTHER than 1R; an absent or
// wrong-side EMA falls back to 1R. The old 50-pt fallback is gone: the swing
// must never be refused because its target is nearer than its stop.
func swingFirstTarget(side Side, entry, stop, ema5m float64) float64 {
	risk := abs(stop - entry)
	t1 := entry + risk
	if side == SideShort {
		t1 = entry - risk
	}
	if ema5m == 0 || !targetOnSide(side, entry, ema5m) {
		return t1
	}
	if side == SideLong && ema5m > t1 {
		return ema5m
	}
	if side == SideShort && ema5m < t1 {
		return ema5m
	}
	return t1
}

// swingApproach is the approach side from the previous bar's close vs the
// line. A close EXACTLY on the line is a tie: no approach yet — wait for a
// bar that closes off the level (CTO 2026-10-03, mirror of touch.go's
// approachSide rule).
func swingApproach(prevClose, line float64) (Side, bool) {
	if prevClose == line {
		return "", false
	}
	if prevClose < line {
		return SideShort, true // came from below → resistance
	}
	return SideLong, true // came from above → support
}

// closedBack reports the corrected §3 reject: the close is BACK on the side
// price came FROM. Came from above (support, long) → close above the line;
// came from below (resistance, short) → close below.
func closedBack(approach Side, close, line float64) bool {
	if approach == SideLong {
		return close > line
	}
	return close < line
}

// swingRejectIntent builds the clean-rejection stop order: tight on the
// reference candle's extreme (no buffer), stop 30 pts beyond the line
// [D5.2-R18/R21/R28 p1 @13:44–14:05]. Refused — with the refusal it names — when the stop is
// ABOVE SkipStopPts (m463, D5.2-R31 "đừng vô": the entry is the candle's
// extreme, so a tall reject candle makes the stop far wider than 30 — trades
// 709/710 were 102.25), narrower than MinStopPts (OFF by default), or on the
// wrong side of the entry (the hard invariant).
func swingRejectIntent(approach Side, ref market.Kline, line float64, cfg SwingCfg) (Intent, bool, SwingRefusal) {
	var in Intent
	if approach == SideShort { // resistance: came from below → sell stop
		in = Intent{
			Action: PlaceStopEntry,
			Setup:  "SWING4H",
			Side:   SideShort,
			Price:  ref.Low - cfg.EntryBufferPts,
			Stop:   line + cfg.StopBeyondLinePts,
		}
	} else {
		in = Intent{
			Action: PlaceStopEntry,
			Setup:  "SWING4H",
			Side:   SideLong,
			Price:  ref.High + cfg.EntryBufferPts,
			Stop:   line - cfg.StopBeyondLinePts,
		}
	}
	if why, bad := swingStopRefusal(in, cfg); bad {
		return Intent{}, false, why
	}
	in.Reason = "swing §8: reject touch, stop order tight on the reference candle, stop 30 pts beyond the 4h EMA 34 [D5.2-R18/R21/R28 p1 @13:44–14:05, p3 @17:32–18:01]"
	return in, true, SwingRefusal{}
}

// swingISBIntent builds the inside-bar entry after the level went invalid:
// stop AT the level, "even if it feels big" [D5.2-R29 p3 @05:14–05:57] — but a
// stop above SkipStopPts is still "đừng vô" (R31: his own example, the 17 Jun
// inside-bar long with the stop at the level 91.75 pts away, is the one he
// rejects [p2 @18:49–21:40]). ref is the INSIDE-BAR candle.
func swingISBIntent(t *swingTouch, ref market.Kline, line float64, cfg SwingCfg) (Intent, bool, SwingRefusal) {
	var in Intent
	if t.Approach == SideShort {
		in = Intent{Action: PlaceStopEntry, Setup: "SWING4H", Side: SideShort, Price: ref.Low - cfg.EntryBufferPts, Stop: line}
	} else {
		in = Intent{Action: PlaceStopEntry, Setup: "SWING4H", Side: SideLong, Price: ref.High + cfg.EntryBufferPts, Stop: line}
	}
	if why, bad := swingStopRefusal(in, cfg); bad {
		return Intent{}, false, why
	}
	if cfg.ISBCloseBackRequired {
		// pre-#61 text, byte-identical (the pinned tapes replay under required).
		in.Reason = "swing §8: the level went invalid, then closes back → 5m inside bar with the stop AT the level [D5.2-R24/R29 p1 @20:31–21:57, p3 @05:14–05:57]"
	} else {
		in.Reason = "swing §8: the level went invalid → 5m inside bar, no close back needed (mentor_swing_isb_close_back=not_required) — stop AT the level [D5.2-R24/R26/R29 p1 @20:31–21:57, p3 @04:49–05:57]"
	}
	return in, true, SwingRefusal{}
}

// swingISBEntryOnTradeSide (m466 P4, G63 D5.2-R26) reports the inside-bar
// stop order's entry and whether it sits strictly on the TRADE side of the line
// (above it for a long, below it for a short), where a stop AT the line (R29)
// can protect it. Same entry price as swingISBIntent.
func swingISBEntryOnTradeSide(approach Side, ref market.Kline, line float64, cfg SwingCfg) (float64, bool) {
	if approach == SideShort {
		e := ref.Low - cfg.EntryBufferPts
		return e, e < line
	}
	e := ref.High + cfg.EntryBufferPts
	return e, e > line
}

// swingStopRefusal is the ONE stop check both swing entries run, in order:
// the R31 skip ceiling, the MinStopPts floor, the hard invariant.
func swingStopRefusal(in Intent, cfg SwingCfg) (SwingRefusal, bool) {
	pts := abs(in.Stop - in.Price)
	if skip := swingSkipStopPts(cfg); pts > skip+1e-6 { // 1e-6: float noise of the unrounded line, never a tick (0.25)
		swingMaxStopRefused.Add(1)
		return SwingRefusal{Name: SwingRefusedMaxStop, Why: fmt.Sprintf(
			"%s entry %.2f stop %.2f = %.2f pts > the %.0f-pt ceiling (mentor_swing_max_stop_pts) — \"100 điểm quá nhiều rủi ro\", đừng vô [D5.2-R31 p2 @19:34–21:43]; m186 reversed by owner order 2026-11-01",
			in.Side, in.Price, in.Stop, pts, skip)}, true
	}
	if cfg.MinStopPts > 0 && pts < cfg.MinStopPts {
		swingMinStopRefused.Add(1)
		return SwingRefusal{Name: "swing_min_stop", Why: fmt.Sprintf("stop %.2f pts < the %.2f-pt floor (swing_min_stop_pts)", pts, cfg.MinStopPts)}, true
	}
	if in.Side == SideLong && in.Stop >= in.Price || in.Side == SideShort && in.Stop <= in.Price {
		swingInvalidStops.Add(1)
		log.Printf("swing4h: hard invariant broken — %s entry %.2f stop %.2f refused", in.Side, in.Price, in.Stop)
		return SwingRefusal{Name: "swing_invalid_stop", Why: fmt.Sprintf("%s entry %.2f stop %.2f — the stop is on the wrong side of the entry", in.Side, in.Price, in.Stop)}, true
	}
	return SwingRefusal{}, false
}

// SwingHoldDeadline is the open-stamp (ms) of the first 5m bar at/after which
// the swing's hold is over: the CLOSE of the n-th 4h candle AFTER the entry
// candle (m332 B4). The entry candle itself is never one of the n — a swing
// entered late in its bucket must not be closed minutes later. Candles are
// stepped through the real 17:00 CT-anchored buckets (kernel.FourHBucketStart),
// never entryBucket + n*4h: the 13:00 bucket is cut by the 16:00-17:00 halt,
// and a bucket that opens while the CME is closed (weekend, holiday) is not a
// candle — it is skipped, so a Friday swing holds through the first CANDLES of
// the new week, not an empty Friday-evening bucket. n < 1 counts as 1. The
// deadline is capped at the next market close (never held through the halt or
// a weekend) — see swingHold.
func SwingHoldDeadline(entryBucket int64, n int) int64 {
	d, _ := swingHold(entryBucket, n)
	return d
}

// swingHold returns the hold deadline and whether it was CAPPED at a market
// close. A swing is NEVER carried through a market close (daily halt, weekend,
// holiday): if the CME closes before the stepped candle end, the deadline is
// that close (the first closed minute, found with kernel.IsCMEOpen), and the
// position is flattened on the last bar before it.
func swingHold(entryBucket int64, n int) (int64, bool) {
	if n < 1 {
		n = 1
	}
	b := entryBucket
	for step := 0; step < n+1; step++ {
		b = nextTradingFourHBucket(b)
	}
	for t := entryBucket; t < b; t += 5 * 60_000 {
		if !kernel.IsCMEOpen(time.UnixMilli(t)) {
			return t, true
		}
	}
	return b, false
}

// nextTradingFourHBucket returns the start of the first 4h bucket after b that
// opens while the CME is open (bounded; falls back to b+4h).
func nextTradingFourHBucket(b int64) int64 {
	const fourH = 4 * 3600_000
	cur := b
	for i := 0; i < 128; i++ {
		// probe past the boundary: a bucket is 4h except around DST/early closes
		next := fourHBucketStart(cur+fourH, ctime())
		for next <= cur {
			next = fourHBucketStart(next+30*60_000, ctime())
		}
		cur = next
		if kernel.IsCMEOpen(time.UnixMilli(cur)) {
			return cur
		}
	}
	return b + fourH
}

// manageSwingPosition emits the BE and hold-close intents for an open swing.
//
// Release #54 (N3): neither intent is single-shot any more. The position is
// kept until the EXECUTOR reports the outcome back (SwingBEApplied /
// SwingEnded): an unapplied BE or hold-close is re-emitted once per closed 5m
// bar. Before, MovedBE=true / Pos=nil were set at emit, so a refusal (a
// transient book, a row not yet filled, a rejected stop) was final — the swing
// sat on its full stop, or was carried past the 4h rule, and the executor's
// log line promised "retried next 4h read" while nothing ever retried.
func manageSwingPosition(s *SwingState, bars []market.Kline, cfg SwingCfg) []Intent {
	p := s.Pos
	if p == nil || len(bars) == 0 {
		return nil
	}
	cur := bars[len(bars)-1]
	var out []Intent
	holdMs, capped := swingHold(p.EntryBucket, cfg.Hold4hBars)
	// A capped deadline is a market close: the last bar BEFORE it (whose own
	// close lands on the halt) is the one that flattens — there is no bar at it.
	holdOver := cur.OpenTime >= holdMs || (capped && cur.OpenTime+5*60_000 >= holdMs)
	if !p.MovedBE && !holdOver {
		// m233: the BE at +1R on a CLOSED 5m candle is the GENERAL 1:1 rule
		// [D1.2 p1 @12:34–13:12, @15:56–15:59 "Bắt buộc … maintain risk reward
		// 1-1"] — it applies to every trade, the swing included (D5.2 adds no
		// exception). A 5m CLOSE at/through +1R earns the break-even; once
		// earned it stays owed until the executor reports it applied.
		if p.Side == SideLong && cur.Close >= p.Entry+p.Risk || p.Side == SideShort && cur.Close <= p.Entry-p.Risk {
			p.beEarned = true
		}
		if p.beEarned && p.beTriedBar != cur.OpenTime {
			p.beTriedBar = cur.OpenTime
			out = append(out, Intent{Action: ActionMoveStopBE, ArmID: p.ArmID,
				Reason: "swing: +1R reached on a 5m close — stop to break-even [D1.2 p1 @12:34–13:12, @15:56–15:59]"})
		}
	}
	if holdOver && p.closeTriedBar != cur.OpenTime {
		p.closeTriedBar = cur.OpenTime
		out = append(out, Intent{Action: ActionClosePosition, ArmID: p.ArmID,
			Reason: "swing §8: hold ONE 4h candle [D5.2 p1 @06:03–06:20]"})
	}
	return out
}

// BEApplied records the executor's receipt that the break-even stop is on the
// swing's position (release #54, N3). A receipt for another arm is ignored.
func (s *SwingState) BEApplied(armID string) bool {
	if s == nil || s.Pos == nil || armID == "" || s.Pos.ArmID != armID {
		return false
	}
	s.Pos.MovedBE = true
	return true
}

// Ended records that the swing's position no longer exists — its stop, its
// target or its hold-close filled, or the executor proved the account holds no
// position carrying this arm's signal (release #54, N2). It clears Pos so a
// finished swing can never emit a BE or a close against a LATER trade. A
// receipt for another arm is ignored; the report says whether it cleared.
func (s *SwingState) Ended(armID string) bool {
	if s == nil || s.Pos == nil || armID == "" || s.Pos.ArmID != armID {
		return false
	}
	s.Pos = nil
	return true
}

// SwingRebuild is the durable record of a FILLED swing the restart path
// restores from: the armed row's entry and ENTRY stop (the stop the trade was
// entered with — the risk is measured from it, never from a stop the broker
// has since moved), NT8's live stop, the 4h bucket of the swing's 5m bar, the
// arm id, and whether the live stop already sits at break-even.
type SwingRebuild struct {
	Side        Side
	Entry       float64
	EntryStop   float64
	LiveStop    float64
	Target      float64
	EntryBucket int64
	ArmID       string
	MovedBE     bool
}

// RebuildSwingPos (FIX-SWING-RESTART) restores the FILLED swing leg from
// durable records (the armed row, the open position and NT8's live stop) so
// manageSwingPosition resumes break-even + hold-close across a restart. Every
// field is caller-proven; the kernel invents nothing. The risk is SwingRisk —
// the SAME definition the live fill uses (SWING-RISK, release #22). A
// degenerate position (unknown side, entry unreadable, or a risk ≤ 0) is
// refused (false) so the caller leaves the stop as it is at NT8 — never a
// guessed stop or a guessed close.
func (s *SwingState) RebuildSwingPos(r SwingRebuild) bool {
	if r.Side != SideLong && r.Side != SideShort {
		return false
	}
	risk := SwingRisk(r.Side, r.Entry, r.EntryStop)
	if r.Entry <= 0 || risk <= 0 {
		return false
	}
	s.Pos = &swingPosition{
		Side:        r.Side,
		Entry:       r.Entry,
		Stop:        r.LiveStop,
		Target:      r.Target,
		Risk:        risk,
		EntryBucket: r.EntryBucket,
		ArmID:       r.ArmID,
		MovedBE:     r.MovedBE,
	}
	return true
}

// swingHoleHorizonBuckets is how far back (in FOLDED 4h closes) a missing
// open-session bucket still matters to the line. A lost fold leaves an error of
// k(1-k)^n * (close - ema) with k = 2/(period+1): at 2*period folds (~11
// trading days) that is below 0.06 pt even for a 50-pt close-to-line distance,
// so an older hole is not worth wedging every swing entry until a restart.
func swingHoleHorizonBuckets(period int) int {
	if period <= 0 {
		period = DefaultSwingCfg().EMAPeriod
	}
	return 2 * period
}

// swingHoleMinOpenMinutes is the least OPEN CME time a 4h bucket may leave
// uncovered at its tail before it counts as a data hole. One hour, because the
// bars read here are 1h (seed) or 5m (flip): a quiet minute or two with no
// print is not a hole, a missing hour of an open session is.
const swingHoleMinOpenMinutes = 60

const fourHMs = int64(4 * time.Hour / time.Millisecond)

// holedBucket is a 4h bucket whose tail was open CME time with no bars.
type holedBucket struct {
	Start  int64 // ms, bucket start
	Folded int   // present (folded) buckets BEFORE it: where the lost fold belonged
}

// holedFourHBuckets walks the 17:00-CT-anchored 4h buckets from bucket start
// `from` up to (excluding) `to` and returns every bucket whose bars stop short
// of the bucket end while the CME session was OPEN for at least
// swingHoleMinOpenMinutes of the uncovered tail, a bucket with no bars at all
// included. The close the EMA folds for a bucket is its LAST bar's close, so a
// missing tail is a wrong close and a missing bucket is a missing fold. The
// seed's fourHClosedBuckets and bucketClosesBetween both skip such a bucket as
// if it were a weekend ("never candles"); only the CME calendar (the same
// kernel.IsCMEOpen the trading gate reads) can tell a halt, a weekend or a
// holiday from a data hole. barMs is the bar duration of `bars` (ascending).
// present is the number of buckets that had at least one bar.
//
// Bucket edges come from fourHBucketStart, the SAME function the fold uses
// (m456 defect 1): a bucket is [s, fourHBucketEnd(s)), which is shorter than
// 4h where the 17:00 CT anchor cuts it (1h on the fall-back Saturday, 3h on the
// spring-forward one). Walking a flat s+4h read such a bucket as 4h long and
// counted the next session's open hours as its uncovered tail.
//
// A roll seam is not special-cased here (m456 defect 2): the seed hands this
// scan, and the fold, a series that already holds every minute any contract of
// the stitch stores (withSeamBars), so a presence test on `bars` is the whole
// test.
func holedFourHBuckets(bars []market.Kline, barMs, from, to int64) (holes []holedBucket, present int) {
	i := 0
	for i < len(bars) && bars[i].OpenTime < from {
		i++
	}
	for s := from; s < to; {
		e := fourHBucketEnd(s)
		covered, any := s, false
		for i < len(bars) && bars[i].OpenTime < e {
			any = true
			if end := bars[i].OpenTime + barMs; end > covered {
				covered = end
			}
			i++
		}
		if covered > e {
			covered = e
		}
		if covered < e {
			open, ok := kernel.OpenIntervalsBetween(covered, e, 60_000, 1000)
			if !ok || open >= swingHoleMinOpenMinutes {
				holes = append(holes, holedBucket{Start: s, Folded: present})
			}
		}
		if any {
			present++
		}
		s = e
	}
	return holes, present
}

// fourHBucketEnd is the exclusive end of the 4h bucket starting at s: the start
// of the bucket the instant s+4h falls in. Equal to s+4h except where the 17:00
// CT anchor cuts the bucket short (DST change days), so it is the same edge the
// fold (fourHBucketStart) puts between two buckets. Always > s.
func fourHBucketEnd(s int64) int64 {
	if e := fourHBucketStart(s+fourHMs, ctime()); e > s {
		return e
	}
	return s + fourHMs
}

// recentHoledBuckets keeps the holes still worth failing closed for: those with
// at most `horizon` folded closes after them.
func recentHoledBuckets(holes []holedBucket, present, horizon int) []int64 {
	var out []int64
	for _, h := range holes {
		if present-h.Folded <= horizon {
			out = append(out, h.Start)
		}
	}
	return out
}

// withSeamBars is bars1m plus the stitch's seam bars (Evaluator.SetSeamBars)
// that closed by now, in open-time order, skipping a minute bars1m already has.
// With no seam bars it returns bars1m itself, so a non-stitched seed is
// untouched. This is the single place the swing line's 1m source is widened to
// every minute any contract of a roll stitch stores.
func withSeamBars(bars1m, seam []market.Kline, now int64) []market.Kline {
	if len(seam) == 0 || len(bars1m) == 0 {
		return bars1m
	}
	out := make([]market.Kline, 0, len(bars1m)+len(seam))
	j := 0
	for _, b := range bars1m {
		for j < len(seam) && seam[j].OpenTime < b.OpenTime {
			if seam[j].CloseTime <= now {
				out = append(out, seam[j])
			}
			j++
		}
		if j < len(seam) && seam[j].OpenTime == b.OpenTime {
			j++
		}
		out = append(out, b)
	}
	for ; j < len(seam); j++ {
		if seam[j].CloseTime <= now {
			out = append(out, seam[j])
		}
	}
	return out
}
