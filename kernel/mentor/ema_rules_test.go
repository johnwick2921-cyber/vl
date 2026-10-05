package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// TestEMARoomRule — E1: the EMA34 setup refuses for room exactly like a level
// (reward < RoomMultiple x risk). The EMA34 rides the shared PHL/PLH path, so
// this pins the room rule at the EMA call shape.
func TestEMARoomRule(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	ema := Level{Key: string(KindEMA34), Kind: KindEMA34, Price: 100}

	tr := Touch{
		LevelKey:       ema.Key,
		Outcome:        TouchReject,
		ApproachedFrom: SideLong, // came from above: support → LONG (buy stop)
		RefBar:         market.Kline{High: 101, Low: 100},
	}

	// Old high 107.5: target 102.5 (shy 5) → reward 0.5 < 2x risk 2 → refuse.
	if in, ok, reason := PHLPLHR2(tr, Level{Price: 107.5}, 0, 10, 0, cfg); ok {
		t.Fatalf("EMA room rule did not refuse (T < R): %+v", in)
	} else if reason == "" {
		t.Fatal("empty refusal reason")
	}

	// Old high 112: target 107 (shy 5) → reward 5 ≥ 2x risk 2 (4) → pass.
	if _, ok, reason := PHLPLHR2(tr, Level{Price: 112}, 0, 10, 0, cfg); !ok {
		t.Fatalf("EMA with room refused: %s", reason)
	}
}

// TestEMAVisitReclassifies — E3: per-visit references apply to the EMA too (the
// line moves, so a visit ends on one closed candle not touching it).
func TestEMAVisitReclassifies(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	lvl := Level{Key: string(KindEMA34), Kind: KindEMA34, Price: 100}
	var tr Touch

	prev := market.Kline{Close: 99}
	bar1 := market.Kline{Open: 99.4, High: 100.2, Low: 99.3, Close: 99.5}
	visitTick(&tr, lvl, prev, bar1, cfg)
	if tr.Outcome != TouchReject {
		t.Fatalf("visit 1 outcome = %q, want reject", tr.Outcome)
	}
	// non-touching candle ends the visit
	bar2 := market.Kline{Open: 99.5, High: 99.8, Low: 99.2, Close: 99.6}
	visitTick(&tr, lvl, bar1, bar2, cfg)
	if tr.Outcome != TouchNone {
		t.Fatalf("EMA visit must end on a non-touching candle, got %q", tr.Outcome)
	}
	// the next touching candle is the new reference
	bar3 := market.Kline{Open: 99.6, High: 100.1, Low: 99.5, Close: 99.4}
	visitTick(&tr, lvl, bar2, bar3, cfg)
	if tr.Outcome != TouchReject || tr.RefBar != bar3 {
		t.Fatalf("EMA visit 2 did not reclassify with the new reference")
	}
}

// TestEmaLossTick — E2 (CTO 12:50:13Z): the block is a LOSS at the line. The
// entry must FILL first; only then does a stop touch block. Pinned cases:
// (a) filled, then stopped → blocked until a departure; (b) never filled
// before the expiry → NOT blocked; (c) filled, then target first → NOT blocked.
func TestEmaLossTick(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LossDeparturePts = 20 // B22 (CTO 20:29:07Z): the knob default is OFF — E2's numeric departure is pinned here explicitly
	e := New(cfg)

	armLong := func(entry, stop, target, expiry float64) {
		e.State.EmaPendingSide = SideLong
		e.State.EmaPendingEntry = entry
		e.State.EmaPendingStop = stop
		e.State.EmaPendingTarget = target
		e.State.EmaPendingExpiry = int64(expiry)
		e.State.EmaPendingFilled = false
	}

	// (a) fill then stop: candle 1 reaches the entry; candle 2 hits the stop.
	armLong(102, 98, 108, 200_000)
	emaLossTick(e, 100, market.Kline{High: 102.5, Low: 101, CloseTime: 100_000}, 100_000)
	if e.State.EmaPendingSide == "" || !e.State.EmaPendingFilled {
		t.Fatal("touching the entry must fill the pending setup")
	}
	emaLossTick(e, 100, market.Kline{High: 101.5, Low: 97.5, CloseTime: 101_000}, 101_000)
	if !e.State.EmaBlocked {
		t.Fatal("(a) filled then stopped must block the EMA line")
	}
	// R-a: the loss candle is never its own departure, however far its close.
	emaLossTick(e, 100, market.Kline{High: 120, Low: 119, Close: 120, CloseTime: 101_000}, 101_000)
	if !e.State.EmaBlocked {
		t.Fatal("R-a: the loss candle must not lift the block")
	}
	// Departure (loss_departure_pts 20): a LATER candle close 110 (12 pts) is
	// not far enough.
	emaLossTick(e, 100, market.Kline{High: 111, Low: 109, Close: 110, CloseTime: 102_000}, 102_000)
	if !e.State.EmaBlocked {
		t.Fatal("12 pts from the loss price must not lift the block")
	}
	// 22 pts: the departure lifts the block.
	emaLossTick(e, 100, market.Kline{High: 121, Low: 119, Close: 120, CloseTime: 103_000}, 103_000)
	if e.State.EmaBlocked {
		t.Fatal("departure (>=20 pts after the loss candle) must lift the EMA block")
	}

	// (b) never filled: the stop trades but the entry never did, then the
	// order expires — no position, no loss.
	armLong(102, 98, 108, 200_000)
	emaLossTick(e, 100, market.Kline{High: 100.9, Low: 97.5}, 100_000)
	if e.State.EmaBlocked || e.State.EmaPendingSide == "" {
		t.Fatal("(b) a stop touch without a fill is not a loss")
	}
	emaLossTick(e, 100, market.Kline{High: 100, Low: 99}, 250_000)
	if e.State.EmaBlocked || e.State.EmaPendingSide != "" {
		t.Fatal("(b) expiry of an unfilled order must NOT block")
	}

	// (c) filled, then target first: a win, not a loss.
	armLong(102, 98, 108, 200_000)
	emaLossTick(e, 100, market.Kline{High: 102.5, Low: 101}, 100_000)
	emaLossTick(e, 100, market.Kline{High: 108.5, Low: 103}, 101_000)
	if e.State.EmaBlocked || e.State.EmaPendingSide != "" {
		t.Fatal("(c) target first must clear the setup without blocking")
	}
}

// TestEmaSetupAllowed — E2 block + E4 crossing knob on the seam.
func TestEmaSetupAllowed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EmaMaxCross30m = 4
	e := New(cfg)
	ema := Level{Key: string(KindEMA34), Kind: KindEMA34, Price: 100}

	// non-EMA levels are never gated here.
	if !emaSetupAllowed(e, keyLevel(100), nil, cfg) {
		t.Fatal("key levels must pass the EMA seam")
	}

	// crossing noise: 4 sign changes in the last 30 bars.
	bars := []market.Kline{
		{Close: 101}, {Close: 99}, {Close: 101}, {Close: 99}, {Close: 100.5},
	}
	if emaSetupAllowed(e, ema, bars, cfg) {
		t.Fatalf("cross count %d must refuse at knob 4", emaCross30(bars, 100))
	}

	// knob OFF: the same tape is allowed.
	off := cfg
	off.EmaMaxCross30m = 0
	if !emaSetupAllowed(e, ema, bars, off) {
		t.Fatal("knob OFF must allow the EMA setup")
	}

	// loss block wins regardless of the knob.
	e.State.EmaBlocked = true
	if emaSetupAllowed(e, ema, bars, off) {
		t.Fatal("a blocked EMA must refuse even with the knob off")
	}
}

// TestEmaCross30Window: only the last 30 candles count.
func TestEmaCross30Window(t *testing.T) {
	var bars []market.Kline
	for i := 0; i < 40; i++ {
		c := 100.0
		if i%2 == 1 {
			c = 99.0
		}
		bars = append(bars, market.Kline{Close: c})
	}
	if got := emaCross30(bars, 99.5); got != 30 {
		t.Fatalf("cross count = %d, want 30 (window cap)", got)
	}
}

// TestEmaCrossingGatesTradedLine — item 16 (CTO 23:49Z) Tick pin through the
// production PHL path: the gate must sit on the line that TRADES
// (KindEMA34HTF, the location-gate line), default ON. A reject touch at the
// EMA34HTF line with crossing noise (close crossed the line >= EmaMaxCross30m
// over the last 30m) emits NO entry. Mutant (isEMA34 = KindEMA34 only) → RED:
// the same touch emits a PHL.
func TestEmaCrossingGatesTradedLine(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0 // no auto EMA location line: the HTF line is seeded below
	cfg.RoomMultiple = 2
	cfg.PHLMinCandlesFromExtreme = 2
	cfg.PHLTargetShyPts = 0
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	// EmaMaxCross30m stays at its item-16 default (2 = ON).

	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 101, 101.5, 101, 101.3),
		mk(1, 101, 101.4, 100, 100.5),
		mk(2, 100, 101, 98, 99),        // the left structural low 98 (D2-28 prior)
		mk(3, 99.5, 130, 129.5, 129.7), // the far old high 130 — the PHL target
		// crossing noise: closes 101/99/101/99 cross the EMA 100 four times.
		// Lows are flat 99 so no second swing low forms (no FTGL box), and the
		// 9-bar tape has only ONE closed 5m bucket → no 5m/15m ISB boxes.
		mk(4, 100.5, 101.5, 99, 101),
		mk(5, 99.5, 100.5, 99, 99),
		mk(6, 100.5, 101.5, 99, 101),
		mk(7, 99.5, 100.5, 99, 99),
	}
	// the reject reference: low touches 100, closes above → long reject.
	refBar := mk(8, 100.1, 100.8, 99.6, 100.5)
	bars = append(bars, refBar)
	now := bars[len(bars)-1].OpenTime + 59_999

	if n := emaCross30(bars, 100); n < cfg.EmaMaxCross30m {
		t.Fatalf("fixture: emaCross30 = %d, want >= %d (the gate must fire)", n, cfg.EmaMaxCross30m)
	}

	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = []Level{
		{Key: string(KindEMA34HTF), Kind: KindEMA34HTF, Price: 100},
		{Key: "old-high:130", Kind: KindOldExtreme, Price: 130},
	}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	e.State.Touches = map[string]Touch{
		string(KindEMA34HTF): {LevelKey: string(KindEMA34HTF), Outcome: TouchReject, ApproachedFrom: SideLong, RefBar: refBar, PriceAtTouch: 100},
	}

	ins := e.Tick(bars, now)
	for _, in := range ins {
		if in.Action == PlaceStopEntry {
			t.Fatalf("an EMA34HTF setup with crossing noise must be gated OFF (item 16); got %+v", in)
		}
	}
}
