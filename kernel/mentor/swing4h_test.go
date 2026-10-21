package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// §8 swing tests, built from the source quotes [D5.2 p1–p3, table] with the
// corrected §3 (reject = close back on the approach side; through = cancel)
// and R8 (offset toward the approach, no entry buffer).
// Bars are synthetic 5m candles in fixed CT (−5); the 4h buckets are
// anchored at 17:00 CT and the line is the EMA 34 of CLOSED buckets.

// The tape: three closed 4h buckets with closes 10000, 11000, 12000 → the
// line is EMA 34 ≈ 10168.16. The closed-bucket bars stay FAR from the line
// (they never touch it); the touch happens only in the current bucket.
func mk5m(t *testing.T, day, hour, minute int, o, h, l, c float64) market.Kline {
	t.Helper()
	loc := time.FixedZone("CT", -5*3600)
	ot := time.Date(2026, 9, day, hour, minute, 0, 0, loc)
	return market.Kline{OpenTime: ot.UnixMilli(), Open: o, High: h, Low: l, Close: c, CloseTime: ot.UnixMilli() + 4*60_000}
}

func swingTape(t *testing.T, cur []market.Kline) []market.Kline {
	t.Helper()
	closed := []market.Kline{
		mk5m(t, 14, 17, 5, 9999, 10000, 9998, 10000),   // bucket 17:00 close 10000
		mk5m(t, 14, 21, 5, 10999, 11000, 10998, 11000), // bucket 21:00 close 11000
		mk5m(t, 15, 1, 5, 11999, 12000, 11998, 12000),  // bucket 01:00 close 12000
	}
	return append(closed, cur...)
}

const swingLineGolden = 10168.16 // EMA 34 of 10000/11000/12000 ≈ 10168.1633// TestSwing4hRejectShort — resistance: price came from BELOW, the first
// touching 5m candle closes BACK below the line → sell stop tight on the
// reference candle's low (R8: no buffer), stop ~30 pts above the line
// [D5.2 p3 @ 21:30 corrected; table "Stop (clean rejection)"; R8].
func TestSwing4hRejectShort(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),     // prev: below the line
		mk5m(t, 15, 5, 5, 10160, 10175, 10155, 10160), // touches, closes back below (reject)
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	out := SwingTick(s, bars, cfg, cur[1].OpenTime+60_000)
	if len(out) != 1 {
		t.Fatalf("intents = %d, want exactly the reject stop entry; got %+v", len(out), out)
	}
	in := out[0]
	if in.Action != PlaceStopEntry || in.Side != SideShort {
		t.Fatalf("intent = %+v, want a SHORT stop entry", in)
	}
	if abs(in.Price-10155) > 0.01 { // ref low, tight (R8: no buffer)
		t.Fatalf("entry = %.2f, want 10155 (sell stop tight on the rejecting candle)", in.Price)
	}
	if abs(in.Stop-(swingLineGolden+30)) > 0.01 {
		t.Fatalf("stop = %.2f, want %.2f (30 pts above the line)", in.Stop, swingLineGolden+30)
	}
}

// TestSwing4hRejectLong — support: came from above, closes back above →
// buy stop tight on the reference candle's high, stop 30 below the line.
func TestSwing4hRejectLong(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10200, 10210, 10195, 10205), // prev: above the line
		mk5m(t, 15, 5, 5, 10165, 10175, 10155, 10175), // touches, closes back above (reject)
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	out := SwingTick(s, bars, cfg, cur[1].OpenTime+60_000)
	if len(out) != 1 || out[0].Side != SideLong {
		t.Fatalf("intents = %+v, want one LONG stop entry", out)
	}
	in := out[0]
	if abs(in.Price-10175) > 0.01 { // ref high, tight (R8: no buffer)
		t.Fatalf("entry = %.2f, want 10175 (buy stop tight on the rejecting candle)", in.Price)
	}
	if abs(in.Stop-(swingLineGolden-30)) > 0.01 {
		t.Fatalf("stop = %.2f, want %.2f (30 pts below the line)", in.Stop, swingLineGolden-30)
	}
}

// TestSwing4hOffsetTowardApproach — R8: the placement offset sits TOWARD
// the approaching price. A candle that reaches only the far side of the old
// symmetric band (line−offset < low) has NOT reached the placed line and
// produces nothing; one that reaches line−offset does.
func TestSwing4hOffsetTowardApproach(t *testing.T) {
	cfg := DefaultSwingCfg()
	loc := time.FixedZone("CT", -5*3600)
	closed := []market.Kline{
		mk5m(t, 14, 17, 5, 9999, 10000, 9998, 10000),
		mk5m(t, 14, 21, 5, 10999, 11000, 10998, 11000),
		mk5m(t, 15, 1, 5, 11999, 12000, 11998, 12000),
	}
	now := closed[len(closed)-1].OpenTime + 60_000
	line, _, ok := swingLine(closed, now, cfg, loc)
	if !ok {
		t.Fatal("line setup failed")
	}
	step := int64(5 * 60_000)
	prev := mk5m(t, 15, 1, 6, line-40, line-39, line-41, line-40) // below the line, at 01:06 (the tick start)
	short1 := market.Kline{OpenTime: now + step, Open: line - 5, High: line - 4, Low: line - 3, Close: line - 4, CloseTime: now + step + 4*60_000}
	short2 := market.Kline{OpenTime: now + 2*step, Open: line - 6, High: line - 4, Low: line - 6, Close: line - 5, CloseTime: now + 2*step + 4*60_000}
	bars := append(closed, prev, short1, short2)
	s := &SwingState{}
	out := SwingTick(s, bars, cfg, short2.OpenTime+60_000)
	if len(out) != 1 {
		t.Fatalf("intents = %d, want exactly the one reaching touch; got %+v", len(out), out)
	}
	if out[0].Side != SideShort {
		t.Fatalf("intent = %+v, want a short entry from the reaching candle", out[0])
	}
	if abs(out[0].Price-(line-6)) > 0.01 {
		t.Fatalf("entry = %.2f, want %.2f — the reaching candle's low, tight (R8)", out[0].Price, line-6)
	}
}

// TestSwing4hThroughCancelsThenISB — touch closes THROUGH → CANCEL; if it
// then closes back within the leeway, a 5m inside bar enters with the stop
// AT the level [D5.2 p3 @ 21:30, 21:56; table "Stop (inside-bar entry)"].
func TestSwing4hThroughCancelsThenISB(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),      // prev: below
		mk5m(t, 15, 5, 5, 10155, 10220, 10150, 10220),  // touches, closes THROUGH (above)
		mk5m(t, 15, 5, 10, 10175, 10180, 10150, 10160), // closes back below; ISB of the touch candle
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	out := SwingTick(s, bars, cfg, cur[2].OpenTime+60_000)
	if len(out) != 1 {
		t.Fatalf("intents = %d, want just the ISB entry (no empty-ArmID cancel for a through-close with nothing resting); got %+v", len(out), out)
	}
	in := out[0]
	if in.Action != PlaceStopEntry || in.Side != SideShort {
		t.Fatalf("ISB intent = %+v, want a SHORT stop entry", in)
	}
	if abs(in.Stop-swingLineGolden) > 0.01 { // RIGHT AT the level
		t.Fatalf("ISB stop = %.2f, want %.2f (AT the level)", in.Stop, swingLineGolden)
	}
	if abs(in.Price-10150) > 0.01 { // ISB candle low, tight (R8)
		t.Fatalf("ISB entry = %.2f, want 10150", in.Price)
	}
}

// TestSwing4hISBWatchOpenUntilFlip — R34 [D5.2 p2 @10:30]: after a through-close
// the ISB watch stays open until the 4h flip, NOT time-limited to LeewayCandles
// (2). An inside bar that closes back FOUR candles after the through-close still
// enters, with the stop AT the level.
func TestSwing4hISBWatchOpenUntilFlip(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10200, 10220, 10190, 10210),  // prev close above the line (Long approach)
		mk5m(t, 15, 5, 5, 10200, 10200, 10140, 10150),  // touches, closes THROUGH (below) → invalid
		mk5m(t, 15, 5, 10, 10150, 10160, 10140, 10145), // below, no ISB (candle 1)
		mk5m(t, 15, 5, 15, 10145, 10155, 10135, 10140), // below, no ISB (candle 2 — the old window ends)
		mk5m(t, 15, 5, 20, 10180, 10190, 10120, 10130), // wide, below (candle 3)
		mk5m(t, 15, 5, 25, 10130, 10185, 10125, 10180), // closes back above, ISB of candle 3 → LONG entry
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	out := SwingTick(s, bars, cfg, cur[5].OpenTime+60_000)
	if len(out) != 1 {
		t.Fatalf("intents = %d, want just the ISB entry (the watch is not 2-candle limited; no empty-ArmID cancel); got %+v", len(out), out)
	}
	in := out[0]
	if in.Action != PlaceStopEntry || in.Side != SideLong {
		t.Fatalf("ISB intent = %+v, want a LONG stop entry (support close-back)", in)
	}
	if abs(in.Stop-swingLineGolden) > 0.01 {
		t.Fatalf("ISB stop = %.2f, want %.2f (AT the level)", in.Stop, swingLineGolden)
	}
	if abs(in.Price-10185) > 0.01 {
		t.Fatalf("ISB entry = %.2f, want 10185 (the ISB candle's high, tight R8)", in.Price)
	}
	// R43 (item 24, CTO merge pin): the ISB-after-close-through entry carries
	// the swing first target max(1R, 5m EMA34); this short tape has no warm EMA,
	// so it is exactly 1R beyond the entry.
	if want := in.Price + (in.Price - in.Stop); abs(in.Target-want) > 0.01 {
		t.Fatalf("ISB target = %.2f, want %.2f (1R: TARGET 1-1 TRƯỚC [D5.2 p2 @11:17])", in.Target, want)
	}
}

// TestSwing4hRejectTargetIsOneRFloor — item 24 (R43): the swing first target is
// max(1R, the 5m EMA 34) [D5.2 p2 @11:00 "TARGET 1-1 TRƯỚC"]. The old 50-pt
// fallback made a swing with a >50-pt stop carry a target SMALLER than its stop
// and the trader's R9 refused it. With a short tape (no EMA) the target is 1R,
// never the 50-pt fallback.
func TestSwing4hRejectTargetIsOneRFloor(t *testing.T) {
	cfg := DefaultSwingCfg()
	// ref.Low 10143 sits ~25 pts below the line → stop ~55 pts > 50: the old
	// 50-pt fallback target was smaller than the stop and R9 refused the swing.
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10145, 10150, 10140, 10145), // prev below the line
		mk5m(t, 15, 5, 5, 10150, 10165, 10143, 10145), // touches, closes back below (reject)
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	out := SwingTick(s, bars, cfg, cur[1].OpenTime+60_000)
	if len(out) != 1 || out[0].Side != SideShort {
		t.Fatalf("intents = %+v, want one SHORT reject stop entry", out)
	}
	in := out[0]
	risk := abs(in.Stop - in.Price)
	if risk <= 50 {
		t.Fatalf("fixture: the stop must exceed 50 pts to exercise the regression (risk=%.2f)", risk)
	}
	if d := abs(in.Target - in.Price); d < risk {
		t.Fatalf("target distance %.2f must be ≥ risk %.2f — R9 must never refuse a swing for a nearer target", d, risk)
	}
	if abs(in.Target-(in.Price-risk)) > 0.01 {
		t.Fatalf("target = %.2f, want 1R = %.2f (never the old 50-pt fallback)", in.Target, in.Price-risk)
	}
}

// TestSwingFirstTarget — the pure max(1R, 5m EMA 34) rule: the EMA wins only
// when it sits FURTHER than 1R on the profitable side; nearer, absent or
// wrong-side EMA falls back to 1R.
func TestSwingFirstTarget(t *testing.T) {
	cases := []struct {
		name        string
		side        Side
		entry, stop float64
		ema         float64
		want        float64
	}{
		{"long ema absent", SideLong, 100, 90, 0, 110},
		{"long ema nearer than 1R", SideLong, 100, 90, 105, 110},
		{"long ema further than 1R", SideLong, 100, 90, 120, 120},
		{"long ema wrong side", SideLong, 100, 90, 95, 110},
		{"short ema absent", SideShort, 100, 110, 0, 90},
		{"short ema nearer than 1R", SideShort, 100, 110, 95, 90},
		{"short ema further than 1R", SideShort, 100, 110, 80, 80},
		{"short ema wrong side", SideShort, 100, 110, 105, 90},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := swingFirstTarget(tc.side, tc.entry, tc.stop, tc.ema); abs(got-tc.want) > 1e-9 {
				t.Fatalf("swingFirstTarget(%s, entry %.0f, stop %.0f, ema %.0f) = %.2f, want %.2f",
					tc.side, tc.entry, tc.stop, tc.ema, got, tc.want)
			}
		})
	}
}

func TestSwingTickWarmLineIsAuthoritativeWithinBucket(t *testing.T) {
	cfg := DefaultSwingCfg()
	bars := []market.Kline{
		mk5m(t, 14, 1, 5, 1000, 1000, 1000, 1000),
		mk5m(t, 14, 5, 5, 1600, 1600, 1600, 1600),
		mk5m(t, 14, 9, 0, 950, 1100, 900, 1000),
		mk5m(t, 14, 9, 5, 1000, 1050, 950, 1000),
	}
	now := bars[3].OpenTime + 60_000
	localLine, bucketStart, ok := swingLine(bars, now, cfg, ctime())
	if !ok || localLine <= bars[3].Close || localLine-bars[3].Low >= cfg.MaxStopPts {
		t.Fatalf("test setup local line %.2f, want a line above close with a valid stop distance", localLine)
	}

	s := &SwingState{
		Line:        950,
		BucketStart: bucketStart,
		LastBarTime: bars[2].OpenTime,
		EmaCount:    FourHEMA34Min,
		FirstTouch:  &swingTouch{Approach: SideShort, Through: true},
	}
	out := SwingTick(s, bars, cfg, now)
	if len(out) != 0 {
		t.Fatalf("warm line %.2f must govern the same-bucket close; local line %.2f emitted %+v", s.Line, localLine, out)
	}
	if s.Line != 950 {
		t.Fatalf("warm line changed within its bucket: got %.2f, want 950", s.Line)
	}
}

// TestSwing4hStopCeiling100 — R8: 30–60 is allowed, ~100 → DO NOT ENTER
// [table].
func TestSwing4hStopCeiling100(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),
		mk5m(t, 15, 5, 5, 10055, 10175, 10050, 10060), // touches, reject, entry 10050 → spread ~148.2
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	out := SwingTick(s, bars, cfg, cur[1].OpenTime+60_000)
	if len(out) != 0 {
		t.Fatalf("stop ≥ 100 must skip; got %+v", out)
	}
}

// TestSwing4hHardInvariantCounter — the stop must sit on the correct side of
// the entry, else no intent and the counter ticks (log + counter).
func TestSwing4hHardInvariantCounter(t *testing.T) {
	cfg := DefaultSwingCfg()
	before := SwingInvalidStops()
	// line 155, long approach: entry = 120 (tight), stop = 155 − 30 = 125 →
	// the stop sits ABOVE the entry — the invariant breaks.
	if _, ok := swingRejectIntent(SideLong, market.Kline{High: 120, Low: 119}, 155, cfg); ok {
		t.Fatal("invariant violation must be refused")
	}
	if SwingInvalidStops() != before+1 {
		t.Fatalf("counter = %d, want %d (one refused intent logged)", SwingInvalidStops(), before+1)
	}
}

// TestSwing4hOneSetupPerApproach — one setup per approach: no new one until
// price has left the line by at least the stop distance and comes back. The
// return path crosses through (cancel) and marks the level INVALID (R32
// [D5.2 p2 @20:48]): the later below-line touch is REFUSED — an invalid level
// may only trade an inside bar, never a normal reject, for the rest of the 4h
// candle.
func TestSwing4hOneSetupPerApproach(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),      // prev below
		mk5m(t, 15, 5, 5, 10160, 10175, 10155, 10160),  // first setup (short)
		mk5m(t, 15, 5, 10, 10160, 10175, 10155, 10160), // same approach again — blocked
		mk5m(t, 15, 5, 15, 10190, 10210, 10190, 10205), // leaves the line by ≥ 30 (entirely above)
		mk5m(t, 15, 5, 20, 10200, 10200, 10150, 10160), // crosses back through → cancel, level INVALID
		mk5m(t, 15, 5, 25, 10195, 10205, 10150, 10155), // below the line — no ISB close-back
		mk5m(t, 15, 5, 30, 10195, 10205, 10150, 10155), // below the line — no ISB close-back
		mk5m(t, 15, 5, 35, 10060, 10100, 10050, 10060), // back below without touching
		mk5m(t, 15, 5, 40, 10160, 10175, 10155, 10160), // below-line touch → REFUSED (invalid level)
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	out := SwingTick(s, bars, cfg, cur[8].OpenTime+60_000)
	entries, cancels := 0, 0
	for _, in := range out {
		switch in.Action {
		case PlaceStopEntry:
			entries++
		case CancelArm:
			cancels++
		}
	}
	if entries != 1 {
		t.Fatalf("stop entries = %d, want 1 (the invalid level refuses the later below-line touch); got %+v", entries, out)
	}
	if cancels != 0 {
		t.Fatalf("cancels = %d, want 0 (the through-cross back has nothing resting — no empty-ArmID cancel); got %+v", cancels, out)
	}
}

// TestSwing4hFlipOnNewBucket — when a 4h candle finishes the line FLIPS to
// the new one and pending work dies: "do not wait for a retest" [D5.2 p3
// @ 12:30].
func TestSwing4hFlipOnNewBucket(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),
		mk5m(t, 15, 5, 5, 10155, 10220, 10150, 10220), // through-close → pending
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	SwingTick(s, bars, cfg, cur[1].OpenTime+60_000)
	if s.FirstTouch == nil || !s.FirstTouch.Through {
		t.Fatalf("setup: pending touch not armed: %+v", s.FirstTouch)
	}
	// advance into the NEXT 4h bucket (09:00), one more closed bucket close
	next := append(bars, mk5m(t, 15, 9, 5, 12999, 13000, 12998, 13000))
	SwingTick(s, next, cfg, next[len(next)-1].OpenTime+60_000)
	if s.FirstTouch != nil {
		t.Fatal("the flip must drop the pending touch on the old line")
	}
	if s.Line == 0 || s.BucketStart != fourHBucketStart(next[len(next)-1].OpenTime+60_000, nil) {
		t.Fatal("the line must flip to the new bucket")
	}
}

// TestSwing4hBEAndHold — BE at +1R, then hold ONE 4h candle
// [D5.2 p1 @06:03–06:20]. Ticks are incremental: the BE/hold management
// runs at the START of a tick over the position's book.
func TestSwing4hBEAndHold(t *testing.T) {
	cfg := DefaultSwingCfg()
	entryBars := swingTape(t, []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),     // prev below
		mk5m(t, 15, 5, 5, 10160, 10175, 10155, 10160), // short entry 10155, risk ≈ 43.16
	})
	s := &SwingState{}
	out := SwingTick(s, entryBars, cfg, entryBars[len(entryBars)-1].OpenTime+60_000)
	entries := 0
	for _, in := range out {
		if in.Action == PlaceStopEntry {
			entries++
		}
	}
	if entries != 1 {
		t.Fatalf("setup entries = %d, want 1; got %+v", entries, out)
	}
	// next tick: a bar closing at −1R (≤ 10111.84) → BE
	beBars := append(entryBars, mk5m(t, 15, 5, 10, 10095, 10100, 10090, 10095))
	out = SwingTick(s, beBars, cfg, beBars[len(beBars)-1].OpenTime+60_000)
	var be bool
	for _, in := range out {
		if in.Action == ActionMoveStopBE {
			be = true
		}
	}
	if !be {
		t.Fatalf("no BE intent at +1R; got %+v", out)
	}
	// D5.2 p1 @06:03–06:20: hold ONE 4h candle. Entry bucket is 05:00, so the
	// hold limit is 09:00 — a bar BEFORE 09:00 must NOT close, and the bar
	// at/after 09:00 must close. MUTANT: Hold4hBars=2 → the 09:05 bar does not
	// close (holdMs 13:00) → RED.
	before := append(beBars, mk5m(t, 15, 8, 55, 10095, 10100, 10090, 10095))
	out2 := SwingTick(s, before, cfg, before[len(before)-1].OpenTime+60_000)
	for _, in := range out2 {
		if in.Action == ActionClosePosition {
			t.Fatalf("must NOT close before the 1st-4h-candle hold limit (08:55 < 09:00); got %+v", out2)
		}
	}
	atClose := append(before, mk5m(t, 15, 9, 5, 10095, 10100, 10090, 10095))
	out3 := SwingTick(s, atClose, cfg, atClose[len(atClose)-1].OpenTime+60_000)
	var closed bool
	for _, in := range out3 {
		if in.Action == ActionClosePosition {
			closed = true
		}
	}
	if !closed {
		t.Fatalf("no close at the ONE-4h-candle hold limit (09:05 ≥ 09:00); got %+v", out3)
	}
}

// TestSwingRebuildPosResumesBEAndHold — FIX-SWING-RESTART: a FILLED swing
// restored from durable records via RebuildSwingPos resumes break-even at +1R
// and the hold-close at the 2nd 4h candle after entry. The BE/hold management
// runs only because RebuildSwingPos set Pos — a mutant that drops the restore
// leaves Pos nil and this turns RED.
func TestSwingRebuildPosResumesBEAndHold(t *testing.T) {
	cfg := DefaultSwingCfg()
	base := swingTape(t, nil) // 3 closed 4h buckets (17:00/21:00/01:00)
	s := &SwingState{}
	// establish the warm line + LastBarTime from the closed buckets
	now0 := base[len(base)-1].OpenTime + 60_000
	_ = SwingTick(s, base, cfg, now0)
	if s.Line == 0 {
		t.Fatal("fixture: the swing line must be established")
	}
	// a FILLED long swing the trader proved from durable records:
	// entry 10110, original stop 10050 (risk 60), entry in the 05:00 bucket.
	entryBar := mk5m(t, 15, 5, 0, 10100, 10120, 10090, 10110)
	entryBucket := fourHBucketStart(entryBar.OpenTime, nil)
	if !s.RebuildSwingPos(SwingRebuild{Side: SideLong, Entry: 10110, EntryStop: 10050, LiveStop: 10050, Target: 10250, EntryBucket: entryBucket, ArmID: "swing-rebuilt"}) {
		t.Fatal("RebuildSwingPos refused a valid swing")
	}
	// a 5m close at +1R (>= 10170) → break-even
	beBars := append(base, mk5m(t, 15, 5, 5, 10180, 10190, 10175, 10185))
	out := SwingTick(s, beBars, cfg, beBars[len(beBars)-1].OpenTime+60_000)
	var be bool
	for _, in := range out {
		if in.Action == ActionMoveStopBE {
			be = true
			if in.ArmID != "swing-rebuilt" {
				t.Fatalf("BE intent ArmID = %q, want swing-rebuilt", in.ArmID)
			}
		}
	}
	if !be {
		t.Fatalf("no BE intent after the rebuild; got %+v", out)
	}
	// advance past entryBucket + 2*4h (05:00 + 8h = 13:00) → hold-close
	late := append(beBars, mk5m(t, 15, 13, 5, 10180, 10190, 10175, 10185))
	out2 := SwingTick(s, late, cfg, late[len(late)-1].OpenTime+60_000)
	var closed bool
	for _, in := range out2 {
		if in.Action == ActionClosePosition {
			closed = true
		}
	}
	if !closed {
		t.Fatalf("no close at the hold limit after the rebuild; got %+v", out2)
	}
}

// TestSwingRebuildPosAlreadyAtBESendsNoSecondBE — FIX-SWING-RESTART fold M2: a
// swing whose live NT8 stop is already at entry (MovedBE true) must NOT get a
// second ActionMoveStopBE after a restart, even on a +1R close. A mutant that
// forces MovedBE false in RebuildSwingPos re-sends the BE and this turns RED.
func TestSwingRebuildPosAlreadyAtBESendsNoSecondBE(t *testing.T) {
	cfg := DefaultSwingCfg()
	base := swingTape(t, nil)
	s := &SwingState{}
	_ = SwingTick(s, base, cfg, base[len(base)-1].OpenTime+60_000)
	entryBar := mk5m(t, 15, 5, 0, 10100, 10120, 10090, 10110)
	if !s.RebuildSwingPos(SwingRebuild{Side: SideLong, Entry: 10110, EntryStop: 10050, LiveStop: 10110, Target: 10250, EntryBucket: fourHBucketStart(entryBar.OpenTime, nil), ArmID: "swing-be-done", MovedBE: true}) {
		t.Fatal("RebuildSwingPos refused a valid swing")
	}
	// a 5m close well past +1R (>= 10170) must NOT re-send the BE
	beBars := append(base, mk5m(t, 15, 5, 5, 10180, 10190, 10175, 10185))
	out := SwingTick(s, beBars, cfg, beBars[len(beBars)-1].OpenTime+60_000)
	for _, in := range out {
		if in.Action == ActionMoveStopBE {
			t.Fatalf("a swing already at break-even must not re-send MoveStopBE; got %+v", out)
		}
	}
}

// TestSwingRebuildPosHoldBoundary — FIX-SWING-RESTART fold M3, m186 D5.2: the
// hold-close fires at the END of ONE 4h candle (bucket+4h). The entry bar sits
// LATE in its 4h bucket (08:55 = 3h55m into the 05:00 bucket), so the bucket
// math — not a round number — decides the boundary: no close on a bar before
// 09:00, close ON the first bar at/after 09:00. A mutant that reverts
// Hold4hBars to 2 moves the boundary to 13:00 and misses the 09:00 close → RED.
func TestSwingRebuildPosHoldBoundary(t *testing.T) {
	cfg := DefaultSwingCfg()
	base := swingTape(t, nil)
	s := &SwingState{}
	_ = SwingTick(s, base, cfg, base[len(base)-1].OpenTime+60_000)
	// 08:55 is 3h55m into the 05:00 bucket — the entry bucket is 05:00, NOT 08:00.
	entryBar := mk5m(t, 15, 8, 55, 10100, 10120, 10090, 10110)
	entryBucket := fourHBucketStart(entryBar.OpenTime, nil)
	if entryBucket != mk5m(t, 15, 5, 0, 0, 0, 0, 0).OpenTime {
		t.Fatalf("08:55 must resolve to the 05:00 bucket, got %d", entryBucket)
	}
	if !s.RebuildSwingPos(SwingRebuild{Side: SideLong, Entry: 10110, EntryStop: 10050, LiveStop: 10050, Target: 10250, EntryBucket: entryBucket, ArmID: "swing-late"}) {
		t.Fatal("RebuildSwingPos refused a valid swing")
	}
	// a 5m bar BEFORE the boundary (holdMs = 05:00 + 1×4h = 09:00): 08:55
	preBars := append(base, mk5m(t, 15, 8, 55, 10130, 10140, 10125, 10135))
	out := SwingTick(s, preBars, cfg, preBars[len(preBars)-1].OpenTime+60_000)
	for _, in := range out {
		if in.Action == ActionClosePosition {
			t.Fatalf("no hold-close before the ONE-candle boundary (08:55 < 09:00); got %+v", out)
		}
	}
	// the FIRST bar at/after the boundary: 09:00 → close
	postBars := append(preBars, mk5m(t, 15, 9, 0, 10130, 10140, 10125, 10135))
	out2 := SwingTick(s, postBars, cfg, postBars[len(postBars)-1].OpenTime+60_000)
	var closed bool
	for _, in := range out2 {
		if in.Action == ActionClosePosition {
			closed = true
		}
	}
	if !closed {
		t.Fatalf("hold-close must fire on the first bar at/after the ONE-candle boundary (09:00); got %+v", out2)
	}
}

// TestSwingNeverFillsSendsNoManage (S1 pin, production call site SwingTick):
// a swing intent that never fills must never send MoveStopBE / ClosePosition —
// the hold close is a phantom that would flatten a DIFFERENT mentor position on
// the same side. Mutant: openPosition at emit again (s.Pos = p) → the hold
// close fires and this turns RED.
// TestSwingNeverFillsSendsNoManage (S1 pin, production call site SwingTick):
// a swing intent that never fills must never send MoveStopBE / ClosePosition —
// the hold close is a phantom that would flatten a DIFFERENT mentor position on
// the same side. It also pins that the position opens only on a FILL: the
// no-fill bar between emit and the hold boundary must leave s.Pos nil. Mutants:
// open at emit again (s.Pos = p) OR promote without a fill check → the hold
// close fires and this turns RED.
func TestSwingNeverFillsSendsNoManage(t *testing.T) {
	cfg := DefaultSwingCfg()
	cfg.Hold4hBars = 1 // hold to the close of the 1st 4h candle after entry
	entryBars := swingTape(t, []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),     // prev below
		mk5m(t, 15, 5, 5, 10160, 10175, 10155, 10160), // short entry 10155 (resting, not filled)
	})
	s := &SwingState{}
	out := SwingTick(s, entryBars, cfg, entryBars[len(entryBars)-1].OpenTime+60_000)
	entries := 0
	for _, in := range out {
		if in.Action == PlaceStopEntry {
			entries++
		}
	}
	if entries != 1 {
		t.Fatalf("setup entries = %d, want 1; got %+v", entries, out)
	}
	if s.Pending == nil || s.Pos != nil {
		t.Fatalf("the swing must rest PENDING before any fill: pending=%v pos=%v", s.Pending, s.Pos)
	}
	// A no-fill bar INSIDE the same 4h bucket (before the expiry): low 10156
	// never trades through the 10155 sell stop, high 10160 never touches the
	// line. A promote-without-fill mutant opens s.Pos here.
	rest := append(entryBars, mk5m(t, 15, 5, 10, 10158, 10160, 10156, 10158))
	out2 := SwingTick(s, rest, cfg, rest[len(rest)-1].OpenTime+60_000)
	for _, in := range out2 {
		if in.Action == ActionMoveStopBE || in.Action == ActionClosePosition {
			t.Fatalf("an unfilled swing must never send %s; got %+v", in.Action, out2)
		}
	}
	if s.Pos != nil {
		t.Fatalf("a bar that does not trade through the entry must not open a position, got %+v", s.Pos)
	}
	// The hold boundary (Hold4hBars=1 → the 05:00 bucket holds to 09:00): the
	// doji at 10156 neither fills the sell stop nor touches the flipped line.
	late := append(rest, mk5m(t, 15, 9, 5, 10156, 10156, 10156, 10156))
	out3 := SwingTick(s, late, cfg, late[len(late)-1].OpenTime+60_000)
	for _, in := range out3 {
		if in.Action == ActionMoveStopBE || in.Action == ActionClosePosition {
			t.Fatalf("an unfilled swing must never send %s across the hold; got %+v", in.Action, out3)
		}
	}
	if s.Pos != nil {
		t.Fatalf("the unfilled swing must not open a position across the hold, got %+v", s.Pos)
	}
}

// TestSwingMinStopKnobDefaultOff (W26 P2-4) — the degenerate-geometry floor is
// OFF by default (R5): the course states no minimum, only the 100-pt ceiling and
// "a stop near 100 → don't enter".
func TestSwingMinStopKnobDefaultOff(t *testing.T) {
	if DefaultSwingCfg().MinStopPts != 0 {
		t.Fatalf("MinStopPts must default 0 (OFF), got %v", DefaultSwingCfg().MinStopPts)
	}
}

// TestSwingMinStopRefusesDegenerate (W26 P2-4) — with the knob ON, a clean-
// rejection stop narrower than the floor is refused; with the knob OFF (0) the
// same geometry still emits (today's behaviour). Mutant: delete the
// `if cfg.MinStopPts > 0 && abs(...)` check in swingRejectIntent → this test
// goes RED on the ON half.
func TestSwingMinStopRefusesDegenerate(t *testing.T) {
	ref := mk5m(t, 15, 5, 5, 10160, 10175, 10155, 10160) // closes back below the line
	line := swingLineGolden                              // ≈ 10168.16

	// OFF (0): the stop ≈ line+30 − ref.Low ≈ 38 pts emits.
	off, ok := swingRejectIntent(SideShort, ref, line, DefaultSwingCfg())
	if !ok {
		t.Fatalf("MinStopPts=0 must not refuse (OFF = today), got %+v", off)
	}

	// ON (50): a ~38-pt stop is below the floor → refused.
	on := DefaultSwingCfg()
	on.MinStopPts = 50
	if _, ok := swingRejectIntent(SideShort, ref, line, on); ok {
		t.Fatalf("MinStopPts=50 must refuse a stop narrower than 50 pts")
	}
	if SwingMinStopRefused() == 0 {
		t.Fatalf("the min-stop refusal must be counted")
	}
}
