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

// §8 — THE SWING SETUP, 4-hour EMA 34 → 5-minute [D5.2], with the corrected
// §3 (reject = close back on the APPROACH side; close through = wrong way)
// and the RULES-FIX v3 corrections (R8):
//
//  1. "On the 4-hour, draw the line at EMA 34 BEFORE price gets there"
//     [D5.2 p1 @ 04:36, 11:04]. The line is the EMA 34 known at the START of
//     the current 4h bar: closed buckets only, and the line FLIPS when a 4h
//     candle finishes — "do not wait for a retest" [p3 @ 12:30].
//  2. "Switch STRAIGHT DOWN TO THE 5-MINUTE — not the 1-minute" [p1 @ 05:30].
//  3. "Wait for a LITERAL touch. 'KHÔNG ĐƯỢC GẦN ĐỤNG'" [p2 @ 09:15] — the
//     only setup where a near-touch is not accepted. R8: the line placement
//     offset (5–10 pts) sits TOWARD the approaching price — the touch band
//     is on the approach side only.
//  4. "First touching candle is the reference; 2 candles of leeway allowed
//     here only" [p1 @ 15:21] — implemented as the ISB window below.
//  5. Two cases [p3 @ 21:30, corrected §3]:
//   - Touch, closes BACK on the approach side (a REJECT) → stop order
//     beyond that candle. "Stop (clean rejection): ~30 points above the
//     line" [table]. R8: NO buffer — the order sits tight on the 5m
//     candle's extreme.
//   - Touch, closes THROUGH → CANCEL (invalid for this approach). "If it
//     then closes back, wait for a 5-MINUTE INSIDE BAR" — the ISB entry
//     has the "stop RIGHT AT the level" [table].
//  6. R8: a stop of 30–60 is allowed; ~100 → DO NOT ENTER [table]. The
//     swing is EXEMPT from the 25-pt ceiling (the injector must not apply
//     it to SWING4H).
//  7. Targets [table]: first = "EMA 34 on the 5-minute" (or the 50-pt
//     fallback); BE at +1R; hold to the close of the 2nd 4h candle after
//     entry (the method does not state the hold length — knob [C]).
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
	LineOffsetPts     float64 // placement offset TOWARD the approach; default 5 (R8: 5–10)
	StopBeyondLinePts float64 // clean-rejection stop distance; default 30 (R8: 30–60 allowed)
	MaxStopPts        float64 // ≥ this → skip; default 100 [table]
	EntryBufferPts    float64 // R8: NO buffer — order sits tight on the candle; default 0
	TargetEMA5mPeriod int     // first target EMA period on 5m; default 34
	// LeewayCandles is the reference-leeway knob (R26, P2): the first OR
	// second touching candle may be the reference. It is NOT the ISB window —
	// R34 rules that the ISB watch after a through-close stays open until the
	// 4h flip (no candle limit). Currently unused by the swing tick.
	LeewayCandles int
	Hold4hBars    int // hold to the close of the N-th 4h candle after entry; default 2 [C]
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
		MaxStopPts:        100,
		EntryBufferPts:    0, // R8: no buffer
		TargetEMA5mPeriod: 34,
		LeewayCandles:     2,
		Hold4hBars:        2,
	}
}

// swingInvalidStops counts the hard-invariant violations (log + counter).
var swingInvalidStops atomic.Int64

// SwingInvalidStops exposes the counter for tests and telemetry.
func SwingInvalidStops() int64 { return swingInvalidStops.Load() }

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
}

type swingTouch struct {
	RefBar   market.Kline // the first touching 5m candle
	Approach Side         // price came from this side: SideLong = from above
	Through  bool         // the touch closed THROUGH the line
}

type swingPosition struct {
	Side        Side
	Entry       float64
	Stop        float64
	Target      float64
	Risk        float64
	EntryBucket int64
	ArmID       string
	MovedBE     bool
	ExpiryMs    int64 // S1: the resting order's expiry (close of the current 4h candle)
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
	if s.Pending != nil && s.Pending.ExpiryMs > 0 && now >= s.Pending.ExpiryMs {
		s.Pending = nil
	}
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
		if warm {
			if nc := bucketClose(bars5m, s.BucketStart); nc != 0 {
				k := 2.0 / float64(cfg.EMAPeriod+1)
				s.Line = s.Line + k*(nc-s.Line)
				s.EmaCount++
			} else {
				s.Line = line
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
			// with the stop AT the level [D5.2 p3 @ 21:56].
			if closedBack(s.FirstTouch.Approach, b.Close, line) && IsISB(prev, b) {
				if in, eok := swingISBIntent(s.FirstTouch, b, line, cfg); eok {
					s.FirstTouch = nil
					// R43 (item 24): the swing's first target = max(1R, the
					// warmed 5m EMA34) — "TARGET 1-1 TRƯỚC" [D5.2 p2 @11:17].
					in.Target = swingFirstTarget(in.Side, in.Price, in.Stop, swingTargetEMA(bars5m, cfg))
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
		if approach == SideLong && s.ClearLongAt != 0 && b.Low > s.ClearLongAt {
			continue // not yet re-armed
		}
		if approach == SideShort && s.ClearShortAt != 0 && b.High < s.ClearShortAt {
			continue // not yet re-armed
		}
		if !closedBack(approach, b.Close, line) {
			// closes THROUGH → CANCEL [D5.2 p3 @ 21:30, corrected §3].
			// S1: name the resting swing arm so the cancel reaches the right
			// order, and clear the pending — a cancelled order must never
			// grow into a phantom position. A through-close with NOTHING
			// resting must not emit an ArmID "" cancel (FIX-MENTOR-PHANTOM-ARM).
			if s.Pending != nil {
				out = append(out, Intent{
					Action: CancelArm,
					ArmID:  s.Pending.ArmID,
					Reason: "swing: touch closed through the line — cancel, invalid for this approach [D5.2 p3 @ 21:30, corrected §3]",
				})
				s.Pending = nil
			}
			s.FirstTouch = &swingTouch{RefBar: b, Approach: approach, Through: true}
			continue
		}
		in, eok := swingRejectIntent(approach, b, line, cfg)
		if !eok {
			s.FirstTouch = nil
			continue
		}
		in.Target = swingFirstTarget(in.Side, in.Price, in.Stop, swingTargetEMA(bars5m, cfg))
		in.ArmID = "swing-" + strconv.FormatInt(b.OpenTime, 10)
		if approach == SideLong {
			s.ClearLongAt = line - cfg.StopBeyondLinePts
		} else {
			s.ClearShortAt = line + cfg.StopBeyondLinePts
		}
		s.FirstTouch = &swingTouch{RefBar: b, Approach: approach}
		s.openPosition(in, now)
		out = append(out, in)
	}
	// S1: BE/hold management runs AFTER the bar walk, so a fill that happened
	// THIS tick is managed against the fill bar's close — not one bar late.
	if s.Pos != nil {
		out = append(out, manageSwingPosition(s, bars5m, cfg)...)
	}
	return out
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
	p.Risk = in.Stop - in.Price
	if in.Side == SideShort {
		p.Risk = in.Price - in.Stop
	}
	s.Pending = p
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
// placed line — "KHÔNG ĐƯỢC GẦN ĐỤNG" [D5.2 p2 @ 09:15].
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
// Refused when the stop distance reaches MaxStopPts or the hard invariant
// (stop on the correct side of the entry) breaks.
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
	if abs(in.Stop-in.Price) >= cfg.MaxStopPts {
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
	if abs(in.Stop-in.Price) >= cfg.MaxStopPts {
		return Intent{}, false
	}
	if in.Side == SideLong && in.Stop >= in.Price || in.Side == SideShort && in.Stop <= in.Price {
		swingInvalidStops.Add(1)
		log.Printf("swing4h: hard invariant broken — ISB entry %s %.2f stop %.2f refused", in.Side, in.Price, in.Stop)
		return Intent{}, false
	}
	in.Reason = "swing §8: through-close, then closes back → 5m inside bar with the stop AT the level [D5.2 p3 @ 21:56, table]"
	return in, true
}

// manageSwingPosition emits the BE and hold-close intents for an open swing.
func manageSwingPosition(s *SwingState, bars []market.Kline, cfg SwingCfg) []Intent {
	p := s.Pos
	if p == nil || len(bars) == 0 {
		return nil
	}
	cur := bars[len(bars)-1]
	var out []Intent
	if !p.MovedBE {
		if p.Side == SideLong && cur.Close >= p.Entry+p.Risk || p.Side == SideShort && cur.Close <= p.Entry-p.Risk {
			out = append(out, Intent{Action: ActionMoveStopBE, ArmID: p.ArmID,
				Reason: "swing §8: +1R reached — stop to break-even"})
			p.MovedBE = true
		}
	}
	holdMs := p.EntryBucket + int64(cfg.Hold4hBars)*4*3600_000
	if cur.OpenTime >= holdMs {
		out = append(out, Intent{Action: ActionClosePosition, ArmID: p.ArmID,
			Reason: "swing §8: hold to the close of the 2nd 4h candle after entry"})
		s.Pos = nil
	}
	return out
}
