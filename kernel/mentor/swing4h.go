package mentor

import (
	"log"
	"strconv"
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
//  3. "Wait for a LITERAL touch. 'KHÔNG ĐƯỢC GẦN ĐỤNG'" [D5.2 p2 @ 18:36–18:41] — the
//     only setup where a near-touch is not accepted. R8: the line placement
//     offset (5–10 pts) sits TOWARD the approaching price — the touch band
//     is on the approach side only.
//  4. "First touching candle is the reference; 2 candles of leeway allowed
//     here only" [D5.2 p1 @15:21–15:48] — the 4h level's touch window is the
//     FIRST TWO touching 5m candles: the entry may be armed off either of
//     them, and if either of them CLOSES beyond the level there is NO order
//     (cancel any resting arm, spend the level). SWING4H-only; every other
//     setup keeps first-touch = 1 candle. The ISB recovery window after a
//     through-close is NOT this leeway — R34 keeps it open until the 4h flip.
//  5. Two cases [p3 @ 21:30, corrected §3]:
//   - Touch, closes BACK on the approach side (a REJECT) → stop order
//     beyond that candle. "Stop (clean rejection): ~30 points above the
//     line" [table]. R8: NO buffer — the order sits tight on the 5m
//     candle's extreme.
//   - Touch, closes THROUGH → CANCEL (invalid for this approach). "If it
//     then closes back, wait for a 5-MINUTE INSIDE BAR" — the ISB entry
//     has the "stop RIGHT AT the level" [table].
//  6. R8: a stop of 30–60 is allowed; ~100 → DO NOT ENTER [table]. The
//     swing is EXEMPT from the ISB stop ceiling (mentor_isb_max_pts, default 25;
//     the injector must not apply
//     it to SWING4H).
//  7. Targets [table]: first = "EMA 34 on the 5-minute" (or the 50-pt
//     fallback); BE at +1R [D1.2 p1 @12:34–13:12, @15:56–15:59 — the general
//     1:1 maintenance]; hold ONE 4h candle [D5.2 p1 @06:03–06:20].
//  8. One setup per approach: no new one until price has left the line by
//     at least the stop distance and comes back. R8: each new 4h candle
//     re-bases the line and the "used" approach resets.
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
	LineOffsetPts     float64 // placement offset TOWARD the approach; default 5 [D5.2 p2 @22:24–23:05: 5–10]
	StopBeyondLinePts float64 // clean-rejection stop distance; default 30 (R8: 30–60 allowed)
	MaxStopPts        float64 // ~100-pt ceiling; NOT used as a skip in swingRejectIntent (no refusal; waits on mentor Q43), its default is the SIZING ceiling the trader reads in mentorSwingCeiling (wide-stop 2-MNQ cap, mentorSwingWideStopCap): the mentor softened “Đừng vô” to “take 1-2 MNQ and sit” [D5.2 p2 @21:25–21:43]; the 2-MNQ tier sizes the risk down, the stop is never widened
	// MinStopPts is the degenerate-geometry floor (W26 P2-4): a swing stop
	// NARROWER than this is refused. 0 = OFF (the course states no minimum —
	// only a 100-pt ceiling and "a stop near 100 → don't enter" [06-SIZING]).
	// R5: new behaviour behind a knob that defaults OFF.
	MinStopPts        float64
	EntryBufferPts    float64 // NO buffer — order sits tight on the candle; default 0 [D5.2 p2 @05:02–05:12]
	TargetEMA5mPeriod int     // first target EMA period on 5m; default 34
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
}

// DefaultSwingCfg returns the §8 defaults.
func DefaultSwingCfg() SwingCfg {
	return SwingCfg{
		EMAPeriod:         34,
		LineOffsetPts:     5,
		StopBeyondLinePts: 30,
		MaxStopPts:        100, // ~100-pt ceiling, no skip (m186 D5.2 @21:25-21:43)
		MinStopPts:        0,   // W26 P2-4: OFF by default (R5)
		EntryBufferPts:    0,   // no buffer [D5.2 p2 @05:02–05:12]
		TargetEMA5mPeriod: 34,
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
	Line         float64 // the 4h EMA 34 in force
	BucketStart  int64   // ms, the 4h bucket the line belongs to
	LastBarTime  int64   // ms, last closed 5m bar processed
	FirstTouch   *swingTouch
	EmaCount     int            // 4h closes consumed by Line — ≥ FourHEMA34Min means warm (seed path)
	ClearLongAt  float64        // price ≤ this re-arms longs (line − stop)
	ClearShortAt float64        // price ≥ this re-arms shorts (line + stop)
	Pending      *swingPosition // S1: the resting stop order, not yet filled
	Pos          *swingPosition // S1: the FILLED swing leg (BE/hold management)
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
}

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
		s.FirstTouch = nil
		s.ClearLongAt, s.ClearShortAt = 0, 0
	}
	if warm {
		// The seeded recurrence has full history; the local slice is only a fallback.
		line = s.Line
	}

	for i := 1; i < len(bars5m); i++ {
		b := bars5m[i]
		if b.OpenTime <= s.LastBarTime || b.CloseTime == 0 {
			continue
		}
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
		if s.Pending != nil && closesThroughPendingLine(s.Pending.Side, b.Close, line) {
			out = append(out, Intent{
				Action: CancelArm,
				ArmID:  s.Pending.ArmID,
				Reason: "swing: a later 5m close cut through the line — cancel the resting order [D5.2 p1 @15:48–16:24]",
			})
			s.Pending = nil
		}
		prev := bars5m[i-1]
		// one-setup-per-approach re-arm: price left the line by the stop
		// distance and has room to come back.
		if b.High >= line+cfg.StopBeyondLinePts {
			s.ClearShortAt = 0
		}
		if b.Low <= line-cfg.StopBeyondLinePts {
			s.ClearLongAt = 0
		}
		// R32 + R34 [D5.2 p2 @20:48, @10:30] — once a touch closed THROUGH the
		// line the level is INVALID for this whole 4h candle. Only an inside
		// bar may trade it now (never a normal reject / PHL / PLH again), and
		// the ISB watch stays open until the 4h flip — it is NOT time-limited
		// to LeewayCandles. The flip (bucketStart change above) resets it.
		if s.FirstTouch != nil && s.FirstTouch.Through {
			// closes back after the through-close → the 5m INSIDE BAR entry
			// with the stop AT the level [D5.2 p1 @ 21:43–21:49].
			//
			// Release #52 W5 (A7): the pair is "candle 1 closes back, THEN wait
			// one more 5m beat for the inside bar" [D5.2 p1 @21:25-21:40 "đợi
			// thêm 1 nhịp 5 phút ... cây nến INSA 305 phút cho lệnh short"], and
			// candle 1's COLOUR sets the side [D1.4 p1 @09:20-09:36 "nếu màu
			// xanh thì long, màu đỏ thì short"]. So prev (candle 1) must itself
			// have closed back across the line, must carry the swing side's
			// colour (red for a short, green for a long), and b is the inside
			// bar; the close-back bar can no longer be its own inside bar.
			if !s.FirstTouch.ISBFired &&
				closedBack(s.FirstTouch.Approach, prev.Close, line) &&
				ISBDirection(prev) == s.FirstTouch.Approach &&
				closedBack(s.FirstTouch.Approach, b.Close, line) && IsISB(prev, b) {
				if in, eok := swingISBIntent(s.FirstTouch, b, line, cfg); eok {
					// A6: keep Through latched; only mark the ISB spent.
					s.FirstTouch.ISBFired = true
					// R43 (item 24): the swing's first target = max(1R, the
					// warmed 5m EMA34) — "TARGET 1-1 TRƯỚC" [D5.2 p2 @11:17].
					in.Target = swingFirstTarget(in.Side, in.Price, in.Stop, swingTargetEMA(bars5m, cfg))
					if cut, late, ok := lateDaySwingCut(s.BucketStart, b.OpenTime, in.Side, in.Price, in.Target, abs(in.Stop-in.Price)); late {
						if !ok {
							swingLateDayRRRefused.Add(1)
							continue
						}
						in.Target = cut
					}
					in.ArmID = "swing-" + strconv.FormatInt(b.OpenTime, 10)
					s.openPosition(in, now)
					out = append(out, in)
					continue
				}
			}
			continue
		}
		approach, hasApproach := swingApproach(prev.Close, line)
		if !hasApproach {
			continue // tie: prev closed exactly on the line — no approach yet
		}
		if !touchesLineApproach(b, line, cfg.LineOffsetPts, approach) {
			continue
		}
		// Leeway window [D5.2 p1 @15:21–15:48]: the 4h level's touch window is
		// the FIRST LeewayCandles touching 5m candles (default 2) instead of
		// one. The entry may be armed off any of them, so the one-setup-per-
		// approach distance guard is waived while the window is open. Once the
		// window is spent, the normal first-touch / spent rules apply.
		inWindow := s.FirstTouch != nil && !s.FirstTouch.Through &&
			s.FirstTouch.Touches < cfg.LeewayCandles
		// Release #54 (P2-3, trade 707): the "not yet re-armed" guard is a
		// guard on ARMING a new reject — it must never swallow a close THROUGH
		// the line. It used to run first, so a touching 5m bar that closed
		// through was skipped without latching Through; the next bar then read
		// the other side of the line as a fresh approach and armed a normal
		// reject at a level the mentor declares invalid for the whole 4h candle
		// [D5.2 p2 @20:41–21:04, @20:48, @10:30; D5.2 p3 @21:30]. The through
		// branch below therefore runs BEFORE the guard; the guard still applies
		// to the reject path.
		if closedBack(approach, b.Close, line) && !inWindow {
			if approach == SideLong && s.ClearLongAt != 0 && b.Low > s.ClearLongAt {
				continue // not yet re-armed
			}
			if approach == SideShort && s.ClearShortAt != 0 && b.High < s.ClearShortAt {
				continue // not yet re-armed
			}
		}
		if !closedBack(approach, b.Close, line) {

			// closes THROUGH → CANCEL [D5.2 p3 @ 21:30] (corrected §3). A
			// touching candle in the leeway window that closes beyond the level
			// spends it: NO order, cancel any resting arm [D5.2 p1 @15:21–15:48].

			// S1: name the resting swing arm so the cancel reaches the right
			// order, and clear the pending — a cancelled order must never
			// grow into a phantom position. A through-close with NOTHING
			// resting must not emit an ArmID "" cancel (FIX-MENTOR-PHANTOM-ARM).
			if s.Pending != nil {
				out = append(out, Intent{
					Action: CancelArm,
					ArmID:  s.Pending.ArmID,
					Reason: "swing: touch closed through the line — cancel, invalid for this approach [D5.2 p3 @ 21:30, corrected section 3]",
				})
				s.Pending = nil
			}
			touches := 1
			if s.FirstTouch != nil && !s.FirstTouch.Through {
				touches = s.FirstTouch.Touches + 1
			}
			s.FirstTouch = &swingTouch{RefBar: b, Approach: approach, Through: true, Touches: touches}
			continue
		}
		// A second touching candle inside the window may re-arm (supersede) only
		// while NO position owns the level — if the first arm already filled on
		// this candle, the position is open and there is nothing to supersede.
		if inWindow && s.Pos != nil {
			continue
		}
		in, eok := swingRejectIntent(approach, b, line, cfg)
		if !eok {
			s.FirstTouch = nil
			continue
		}
		in.Target = swingFirstTarget(in.Side, in.Price, in.Stop, swingTargetEMA(bars5m, cfg))
		if cut, late, ok := lateDaySwingCut(s.BucketStart, b.OpenTime, in.Side, in.Price, in.Target, abs(in.Stop-in.Price)); late {
			if !ok {
				swingLateDayRRRefused.Add(1)
				s.FirstTouch = nil
				continue
			}
			in.Target = cut
		}
		in.ArmID = "swing-" + strconv.FormatInt(b.OpenTime, 10)
		// A later touching candle inside the window supersedes the earlier arm:
		// the entry re-bases on THIS candle's extreme — either of the first two
		// touching candles may be the reference [D5.2 p1 @15:21–15:48].
		if s.Pending != nil {
			out = append(out, Intent{
				Action: CancelArm,
				ArmID:  s.Pending.ArmID,
				Reason: "swing: re-armed off a later touching candle in the leeway window — supersede the first arm [D5.2 p1 @15:21–15:48]",
			})
			s.Pending = nil
		}
		if approach == SideLong {
			s.ClearLongAt = line - cfg.StopBeyondLinePts
		} else {
			s.ClearShortAt = line + cfg.StopBeyondLinePts
		}
		touches := 1
		if s.FirstTouch != nil && !s.FirstTouch.Through {
			touches = s.FirstTouch.Touches + 1
		}
		s.FirstTouch = &swingTouch{RefBar: b, Approach: approach, Touches: touches}
		s.openPosition(in, now)
		out = append(out, in)
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

// touchesLineApproach is the LITERAL touch of the placed line (R8): the
// placement offset (5–10 pts) sits TOWARD the approaching price, so the
// line is drawn at line−offset for a below-approach (resistance) and at
// line+offset for an above-approach (support); the candle must reach the
// placed line — "KHÔNG ĐƯỢC GẦN ĐỤNG" [D5.2 p2 @ 18:36–18:41].
func touchesLineApproach(b market.Kline, line, offset float64, approach Side) bool {
	if approach == SideShort { // price comes from below → line at line−offset
		return b.Low <= line-offset && b.High >= line-offset
	}
	return b.Low <= line+offset && b.High >= line+offset
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
// reference candle's extreme (R8: no buffer), stop 30 pts beyond the line.
// Refused only when the hard invariant (stop on the correct side of the entry)
// breaks, or the stop is narrower than MinStopPts (OFF by default). MaxStopPts
// is NOT used as a skip here (release #54, doc truth; the trader reads its
// default as the sizing ceiling, mentorSwingCeiling -> the 2-MNQ wide-stop
// cap): a stop of ~100 pts is NOT refused today — m186 softened the course's "dung vo" to "take 1-2 MNQ
// and sit" [D5.2 p2 @21:25–21:43], and the 100-pt ceiling waits on mentor Q43
// (trade 709/710, stop 102.25). The swing ISB (stop AT the line) is exempt by
// the same ruling.
func swingRejectIntent(approach Side, ref market.Kline, line float64, cfg SwingCfg) (Intent, bool) {
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
	// m186 D5.2: NO ~100-pt skip — the mentor softened “Đừng vô” to “take 1-2 MNQ and sit” [D5.2 p2 @21:25–21:43]; the 2-MNQ tier sizes the risk down, the stop is never widened.
	if cfg.MinStopPts > 0 && abs(in.Stop-in.Price) < cfg.MinStopPts {
		swingMinStopRefused.Add(1)
		return Intent{}, false
	}
	if in.Side == SideLong && in.Stop >= in.Price || in.Side == SideShort && in.Stop <= in.Price {
		swingInvalidStops.Add(1)
		log.Printf("swing4h: hard invariant broken — %s entry %.2f stop %.2f refused",
			in.Side, in.Price, in.Stop)
		return Intent{}, false
	}
	in.Reason = "swing §8: reject touch, stop order tight on the reference candle, stop 30 pts beyond the 4h EMA 34 [D5.2 p3 @ 21:30 corrected, table, R8]"
	return in, true
}

// swingISBIntent builds the inside-bar entry after a through-close: stop AT
// the level, "even if it feels big" [table]. ref is the INSIDE-BAR candle.
func swingISBIntent(t *swingTouch, ref market.Kline, line float64, cfg SwingCfg) (Intent, bool) {
	var in Intent
	if t.Approach == SideShort {
		in = Intent{Action: PlaceStopEntry, Setup: "SWING4H", Side: SideShort, Price: ref.Low - cfg.EntryBufferPts, Stop: line}
	} else {
		in = Intent{Action: PlaceStopEntry, Setup: "SWING4H", Side: SideLong, Price: ref.High + cfg.EntryBufferPts, Stop: line}
	}
	// m186 D5.2: NO ~100-pt skip — the mentor softened “Đừng vô” to “take 1-2 MNQ and sit” [D5.2 p2 @21:25–21:43]; the 2-MNQ tier sizes the risk down, the stop is never widened.
	if cfg.MinStopPts > 0 && abs(in.Stop-in.Price) < cfg.MinStopPts {
		swingMinStopRefused.Add(1)
		return Intent{}, false
	}
	if in.Side == SideLong && in.Stop >= in.Price || in.Side == SideShort && in.Stop <= in.Price {
		swingInvalidStops.Add(1)
		log.Printf("swing4h: hard invariant broken — ISB entry %s %.2f stop %.2f refused", in.Side, in.Price, in.Stop)
		return Intent{}, false
	}
	in.Reason = "swing §8: through-close, then closes back → 5m inside bar with the stop AT the level [D5.2 p1 @ 21:43–21:49, table]"
	return in, true
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
func holedFourHBuckets(bars []market.Kline, barMs, from, to int64) (holes []holedBucket, present int) {
	i := 0
	for i < len(bars) && bars[i].OpenTime < from {
		i++
	}
	for s := from; s < to; {
		e := s + fourHMs
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
			if open, ok := kernel.OpenIntervalsBetween(covered, e, 60_000, 1000); !ok || open >= swingHoleMinOpenMinutes {
				holes = append(holes, holedBucket{Start: s, Folded: present})
			}
		}
		if any {
			present++
		}
		nxt := fourHBucketStart(e, ctime())
		if nxt <= s {
			nxt = e
		}
		s = nxt
	}
	return holes, present
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
