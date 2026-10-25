package mentor

import (
	"math"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// ── m425a S1/S2 — an entry dropped AFTER its arm registered leaves no arm ────
// (release #50). Every pin drives the PRODUCTION entry, e.Tick.

// m425aISBTape is the seed_test ISB tape: one long ISB placement fires on it.
func m425aISBTape() []market.Kline {
	return []market.Kline{
		rthBars(0, 121, 121.5, 120.5, 120),
		rthBars(1, 98, 106, 97, 105),
		rthBars(2, 103, 104, 99, 100),
		rthBars(3, 100, 101.5, 99.5, 101),
		rthBars(4, 100, 101.5, 99.5, 101),
		rthBars(5, 100, 101.5, 99.5, 101),
	}
}

func m425aEval(orbHigh, orbLow float64) *Evaluator {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0
	cfg.OrbGateEnabled = true
	e := New(cfg)
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.ORB = ORB{Day: dayStartCT(rthBars(0, 0, 0, 0, 0).OpenTime), High: orbHigh, Low: orbLow, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(rthBars(0, 0, 0, 0, 0).OpenTime).In(ctime())), Verdict: DayTrade}
	return e
}

func m425aRun(e *Evaluator, bars []market.Kline) (placements int) {
	for i := 2; i <= len(bars); i++ {
		for _, in := range e.Tick(bars[:i], bars[i-1].CloseTime+1) {
			if in.Action == PlaceStopLimitEntry || in.Action == PlaceStopEntry {
				placements++
			}
		}
	}
	return
}

// TestM425aORBFilteredISBLeavesNoArm — the named RED. The ORB gate drops the
// ISB AFTER ISBArms[id] registered; the arm used to stay, blocking the next
// same-side ISB (isb_arm_active) and later emitting ExtendArm/CancelArm for an
// order never authored. Revert the cleanup after the filter stage → FAIL.
func TestM425aORBFilteredISBLeavesNoArm(t *testing.T) {
	bars := m425aISBTape()
	if p := m425aRun(m425aEval(90, 85), bars); p != 1 {
		t.Fatalf("control: ORB-clear long must place the ISB, got %d placements", p)
	}
	e := m425aEval(200, 150) // the entry sits INSIDE the opening range → orbGateFilter drops the long ISB
	var dropAt int
	for i := 2; i <= len(bars); i++ {
		for _, in := range e.Tick(bars[:i], bars[i-1].CloseTime+1) {
			if in.Action == PlaceStopLimitEntry || in.Action == PlaceStopEntry {
				t.Fatalf("ORB-filtered ISB must not place: %+v", in)
			}
		}
		if n := len(e.State.ISBArms); n != 0 {
			t.Fatalf("tick %d: ORB-filtered ISB left %d phantom ISBArms: %+v", i, n, e.State.ISBArms)
		}
		if dropAt == 0 && (e.State.Refusals["orb_inside"] > 0 || e.State.Refusals["orb_wrong_side"] > 0) {
			dropAt = i
		}
	}
	if dropAt == 0 {
		t.Fatalf("the ISB was never reached by the ORB gate (refusals %v)", e.State.Refusals)
	}
	// isb_arm_active is the gate that suppresses the NEXT same-side ISB: with
	// no phantom arm it cannot fire.
	if isbArmActive(e.State.ISBArms, SideLong) {
		t.Fatalf("a phantom arm would suppress the next same-side ISB (isb_arm_active)")
	}
}

// m425aLevelFixture is the B6 PHL fixture (level_armgate_test): a LONG PHL
// stop-entry at 29393 off the touch of a level at 29385. kind picks the level
// kind (KindKeyLevel, or KindEMA34HTF — the EMA34 line that trades).
func m425aLevelFixture(t *testing.T, kind LevelKind, key string) (*Evaluator, []market.Kline, Intent) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PHLTargetShyPts = 6
	cfg.Exec2mAfter30m = false
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA34 = 0
	e.State.EMA9 = 0
	e.State.SeedLevels = []Level{{Key: "old-high:29431.75", Kind: KindOldExtreme, Role: OldHigh, Price: 29431.75}}
	e.State.SeedLevels = append(e.State.SeedLevels, Level{Key: key, Kind: kind, Price: 29385})
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 29300}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 29300}}
	bars := []market.Kline{
		rthBars(-1, 29340, 29420, 29335, 29415),
		rthBars(0, 29420, 29431.75, 29410, 29425),
		rthBars(1, 29405, 29408, 29395, 29402),
		rthBars(2, 29392, 29402, 29370, 29396),
		rthBars(3, 29390, 29392, 29385, 29391),
	}
	now := bars[4].CloseTime + 1
	e.State.ORB = ORB{Day: dayStartCT(now), High: 29300, Low: 29200, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	for _, in := range e.Tick(bars, now) {
		if in.Action == PlaceStopEntry && in.Setup == "PHL" && in.Side == SideLong && in.ArmID != "" {
			return e, bars, in
		}
	}
	t.Fatalf("precondition: no LONG PHL emitted off %s (refusals %v)", key, e.State.Refusals)
	return nil, nil, Intent{}
}

func m425aPHLCount(ints []Intent) (n int) {
	for _, in := range ints {
		if in.Action == PlaceStopEntry && strings.HasPrefix(in.Reason, "PHL") {
			n++
		}
	}
	return
}

// TestM425aRefusedPHLNotReEmitted — S2. The trader refuses the PHL (the window
// gate, 10-23 04:25:59 CT) and the dispatch drops the arm. The touch stays
// TouchReject, so the level used to re-emit EVERY minute for the rest of the
// visit (37 times). A trader-side refusal now ends that touch for that level.
// Revert the RefusedLevelTouch skip in the touch loop → the next tick
// re-emits → FAIL.
func TestM425aRefusedPHLNotReEmitted(t *testing.T) {
	e, bars, in := m425aLevelFixture(t, KindKeyLevel, "L")
	e.DropArm(in.ArmID) // exactly what mentorDispatchEntry's deferred guard calls
	if len(e.State.LevelArms) != 0 {
		t.Fatalf("a refused level intent must leave no LevelArms entry: %+v", e.State.LevelArms)
	}
	// the next minutes: price stays near the level, the touch (RefBar) is unchanged.
	cur := bars
	for i := 4; i <= 6; i++ {
		b := rthBars(i, 29386, 29392, 29380, 29390)
		cur = append(append([]market.Kline{}, cur...), b)
		e.State.ISBBox, e.State.ISBBox15m, e.State.ISBBox30m = nil, nil, nil // isolate the re-emit gate from the 5m box ban
		if got := m425aPHLCount(e.Tick(cur, b.CloseTime+1)); got != 0 {
			t.Fatalf("minute %d: the refused PHL touch was re-emitted (%d) — a trader-side refusal must end the touch", i, got)
		}
	}
}

// m425aEMAFixture is the ema_pending_expiry_test fixture: a real EMA34HTF PHL
// emitted by e.Tick, with EmaPending* recorded.
func m425aEMAFixture(t *testing.T) (*Evaluator, []market.Kline, Intent) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	cfg.RoomMultiple = 2
	cfg.PHLMinCandlesFromExtreme = 2
	cfg.PHLTargetShyPts = 0
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	cfg.EmaMaxCross30m = 0
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 101, 101.5, 101, 101.3), mk(1, 101, 101.4, 100, 100.5), mk(2, 100, 101, 98, 99),
		mk(3, 99.5, 130, 129.5, 129.7), mk(4, 100.5, 101.5, 99, 101), mk(5, 99.5, 100.5, 99, 99),
		mk(6, 100.5, 101.5, 99, 101), mk(7, 99.5, 100.5, 99, 99),
	}
	refBar := mk(8, 100.1, 100.8, 99.6, 100.5)
	bars = append(bars, refBar)
	now := refBar.OpenTime + 59_999
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = []Level{
		{Key: string(KindEMA34HTF), Kind: KindEMA34HTF, Price: 100},
		{Key: "old-high:130", Kind: KindOldExtreme, Role: OldHigh, Price: 130},
	}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	e.State.Touches = map[string]Touch{
		string(KindEMA34HTF): {LevelKey: string(KindEMA34HTF), Outcome: TouchReject, ApproachedFrom: SideLong, RefBar: refBar, PriceAtTouch: 100},
	}
	for _, in := range e.Tick(bars, now) {
		if in.Action == PlaceStopEntry && in.ArmID != "" {
			return e, bars, in
		}
	}
	t.Fatalf("precondition: the EMA34HTF setup must emit (refusals %v)", e.State.Refusals)
	return nil, nil, Intent{}
}

// TestM425aRefusedEMA34BooksNoLoss — S2 tail. The EMA34 level sets EmaPending*
// at emit, BEFORE the injector can refuse; the refused intent was never an
// order, so price later running through its entry and "stop" must NOT book
// EmaBlocked (ema_rules.go:87-99). Revert the EmaPendingArmID retraction →
// EmaBlocked → FAIL.
func TestM425aRefusedEMA34BooksNoLoss(t *testing.T) {
	e, bars, in := m425aEMAFixture(t)
	if e.State.EmaPendingSide == "" {
		t.Fatalf("precondition: the EMA34 emit must set EmaPending*")
	}
	e.DropArm(in.ArmID) // the dispatch's deferred guard on a trader-side refusal
	if e.State.EmaPendingSide != "" {
		t.Fatalf("a refused EMA34 intent must not leave an EmaPending: side=%q", e.State.EmaPendingSide)
	}
	last := bars[len(bars)-1]
	mk := func(i int, o, h, l, c float64) market.Kline {
		ot := last.OpenTime + int64(i)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	// price runs through the entry, then through the stop, of the never-placed order.
	up := mk(1, in.Price-0.5, in.Price+1, in.Price-0.6, in.Price+0.5)
	dn := mk(2, in.Price, in.Price, in.Stop-2, in.Stop-1)
	cur := append(append([]market.Kline{}, bars...), up)
	e.Tick(cur, up.CloseTime+1)
	cur = append(cur, dn)
	e.Tick(cur, dn.CloseTime+1)
	if e.State.EmaBlocked {
		t.Fatalf("phantom EmaBlocked booked from a refused (never-placed) EMA34 intent")
	}
}

// m426 (review of m425a S2): the refusal binds to ONE touch (its reference bar).
// A refusal recorded for an EARLIER touch A of the same level key must not
// silence the current touch B — B emits normally. Mutant: `ref == tr.RefBar.CloseTime`
// -> `ref != 0` in the touch loop (any refusal silences the level forever) -> FAIL.
func TestM426NewTouchAfterRefusalStillEmits(t *testing.T) {
	e, bars, in := m425aLevelFixture(t, KindKeyLevel, "L")
	e.DropArm(in.ArmID)
	// pretend touch A (an older reference bar) was the refused one
	for k, ref := range e.State.RefusedLevelTouch {
		e.State.RefusedLevelTouch[k] = ref - 60_000
	}
	if len(e.State.RefusedLevelTouch) == 0 {
		t.Fatal("precondition: DropArm recorded no refusal")
	}
	e.State.ISBBox, e.State.ISBBox15m, e.State.ISBBox30m = nil, nil, nil
	if got := m425aPHLCount(e.Tick(bars, bars[len(bars)-1].CloseTime+1)); got == 0 {
		t.Fatal("a refusal of an earlier touch silenced a NEW touch of the same level")
	}
}

// m427/9 — the ROLLING PHL order through DropArm. The trader refuses the rolling
// pullback order: (a) RollingPHL is zeroed (no phantom roll left to block the
// next same-side setup or to emit a CancelArm for an order never authored),
// (b) NO RefusedLevelTouch is recorded for the rolling key — the rolling order
// re-arms on a NEW reference candle by design — even when a Touches row for the
// key exists with a reference bar, and (c) the next reference candle re-arms.
// Mutants: drop `|| key == rollingLevelKey` in DropArm -> (b) RED;
// `if e.State.RollingPHL.ArmID == armID` -> `if false` in unregisterArm -> (a) RED.
func TestM427DropArmOnTheRollingPHLZeroesItAndNeverMarksARefusedTouch(t *testing.T) {
	e, bars, _ := rollingFixture()
	placed := rollingPlaceEntries(e.Tick(bars, bars[5].OpenTime+59_999))
	if len(placed) != 1 {
		t.Fatalf("control: the rolling order must place once, got %d", len(placed))
	}
	armID := placed[0].ArmID
	if e.State.RollingPHL.ArmID != armID || e.State.LevelArms[rollingLevelKey].ArmID != armID {
		t.Fatalf("control: the rolling arm must be registered: %+v", e.State.RollingPHL)
	}
	// A touch row under the rolling key with a reference bar: the shape that
	// would make the S2 refusal mark fire if the exemption were gone.
	if e.State.Touches == nil {
		e.State.Touches = map[string]Touch{}
	}
	e.State.Touches[rollingLevelKey] = Touch{Outcome: TouchReject, RefBar: bars[5], ApproachedFrom: SideLong}

	e.DropArm(armID)

	if e.State.RollingPHL != (RollingPHL{}) {
		t.Fatalf("(a) a refused rolling order left a phantom RollingPHL: %+v", e.State.RollingPHL)
	}
	if _, ok := e.State.LevelArms[rollingLevelKey]; ok {
		t.Fatal("the rolling LevelArms entry must be gone")
	}
	if _, ok := e.State.RefusedLevelTouch[rollingLevelKey]; ok {
		t.Fatalf("(b) a refused rolling order must NOT record RefusedLevelTouch (it re-arms by design): %+v", e.State.RefusedLevelTouch)
	}

	// (c) a NEW reference candle re-arms. (The synthetic Touches row only probes the
	// exemption above; a live touch-reject would own the entry, so drop it first.)
	delete(e.State.Touches, rollingLevelKey)
	t0 := bars[0].OpenTime
	next := append(append([]market.Kline{}, bars...), market.Kline{OpenTime: t0 + 6*60_000, CloseTime: t0 + 6*60_000 + 59_000, Open: 113, High: 114, Low: 108, Close: 109})
	ins := e.Tick(next, next[6].OpenTime+59_999)
	for _, in := range ins {
		if in.Action == CancelArm && in.ArmID == armID {
			t.Fatalf("a CancelArm was emitted for the dropped (never authored) rolling order %s", armID)
		}
	}
	re := rollingPlaceEntries(ins)
	if len(re) != 1 || re[0].ArmID == armID {
		t.Fatalf("(c) the next reference candle must re-arm the rolling order under a NEW id, got %+v", re)
	}
}
