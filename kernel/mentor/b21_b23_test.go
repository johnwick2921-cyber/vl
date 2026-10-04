package mentor

import (
	"math"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// ---------- B21: ping-pong candle cap ----------

// TestPingPongCandleCap — B21 (OPEN-NUMBERS Q5, CTO-verified D4.2 p2
// @05:17–06:37): gap > 50 is not enough — the largest 1m candle of the last
// PingPongCandleLookback closed bars must stay at or below
// PingPongCandleMaxPts ("nến tầm mười mấy điểm").
func TestPingPongCandleCap(t *testing.T) {
	cfg := DefaultConfig()
	floor := Box{Kind: FTGL, Top: 100, Bottom: 90, Key: "ftgl"}
	ceil := Box{Kind: FTGH, Top: 210, Bottom: 160, Key: "ftgh"} // gap 60

	mk := func(h, l float64) market.Kline { return market.Kline{High: h, Low: l} }
	fat := []market.Kline{mk(130, 105)} // 25-pt candle
	if ok, reason := pingPongVerdict([]Box{floor, ceil}, fat, 110, cfg); ok || reason != "ping_pong_candle_too_big" {
		t.Fatalf("25-pt candle in a 60-pt range must refuse, got ok=%v reason=%q", ok, reason)
	}
	thin := []market.Kline{mk(130, 112)} // 18-pt candle
	if ok, _ := pingPongVerdict([]Box{floor, ceil}, thin, 110, cfg); !ok {
		t.Fatal("18-pt candle in a 60-pt range must pass")
	}
	// The lookback is 30: a fat candle OLDER than the window does not block.
	old := make([]market.Kline, 31)
	old[0] = mk(130, 105)
	for i := 1; i < 31; i++ {
		old[i] = mk(120, 112)
	}
	if ok, _ := pingPongVerdict([]Box{floor, ceil}, old, 110, cfg); !ok {
		t.Fatal("a fat candle outside the 30-bar lookback must not block")
	}
	// Knob off: PingPongCandleMaxPts = 0 disables the cap.
	cfg.PingPongCandleMaxPts = 0
	if ok, _ := pingPongVerdict([]Box{floor, ceil}, fat, 110, cfg); !ok {
		t.Fatal("PingPongCandleMaxPts=0 must disable the candle cap")
	}
}

// ---------- B21: key-level pair ----------

// b21PairFixture builds the X4 tape: key level K at 99.5 (support), the next
// level above is the old high at 110 (gap 10.5). Bar 2 carries the prior
// swing low 98 (so the higher-low check passes) AND the old high 110. Bar 4
// is the variable candle: fat (range 10.7 > the 10.5 gap) or thin.
func b21PairFixture(fat bool) (*Evaluator, []market.Kline, int64) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0 // no EMA34 location line: the pair is K ↔ KL2
	cfg.RoomMultiple = 0.5
	cfg.PHLMinCandlesFromExtreme = 2
	cfg.PHLTargetShyPts = 0
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bar4h, bar4l := 102.4, 100.2
	if fat {
		bar4h, bar4l = 119.0, 100.2 // range 18.8 > the 18.5 pair gap
	}
	bars := []market.Kline{
		mk(0, 101, 101.5, 100.8, 101.3), // thin
		mk(1, 101, 101.4, 99.7, 100.5),  // thin, above K
		mk(2, 99, 115, 98, 114.8),       // swing low 98 + a 115 high (visit 1 of K: low 98 touches, close above → reject; its own PHL attempt fails the floor vs KL2 118)
		mk(3, 102, 118, 100.2, 117.8),   // the old high 118 (a tape swing — the PHL ladder reads tape extremes) + depart (no K touch) → visit 1 ends
		// bar 4 is the variable candle
		mk(4, 102, bar4h, bar4l, 102.2),
		mk(5, 102, 103, 99.5, 102.9), // visit 2: touch at K, reject → the PHL reference
	}
	now := bars[5].OpenTime + 59_999
	levels := []Level{
		{Key: "K", Kind: KindKeyLevel, Price: 99.5},
		{Key: "KL2", Kind: KindKeyLevel, Price: 118},            // the pair's second level: gap 18.5
		{Key: "old-high:118", Kind: KindOldExtreme, Price: 118}, // the PHL target (matched to the tape swing at bar 3)
	}
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = levels
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e, bars, now
}

func placeEntries(ins []Intent) int {
	n := 0
	for _, in := range ins {
		if in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry {
			n++
		}
	}
	return n
}

// TestB21KeyPairCandleTooBig — X4 (@06:31–08:52): a candle as big as the rank
// of the two levels ("một cái nến nó bằng cái rank của 2 cái level rồi thì
// ngồi im") — the touched level and the next level must sit farther apart
// than the largest 1m candle of the last 30. Fat → keypair_candle_too_big,
// thin → the PHL fires.
func TestB21KeyPairCandleTooBig(t *testing.T) {
	e, bars, now := b21PairFixture(true)
	ins := e.Tick(bars, now)
	// The keypair gate kills the PHL; an ISB on the same pair may still fire
	// (ISBs are not level-pair entries — the rule is about the level reject).
	for _, in := range ins {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && strings.HasPrefix(in.Reason, "PHL/PLH") {
			t.Fatalf("fat candle between K and the old high emitted a PHL entry: %+v", in)
		}
	}
	if e.State.Refusals["keypair_candle_too_big"] == 0 {
		t.Fatalf("ledger = %v, want keypair_candle_too_big counted", e.State.Refusals)
	}
	e2, bars2, now2 := b21PairFixture(false)
	ins2 := e2.Tick(bars2, now2)
	var phl int
	for _, in := range ins2 {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && strings.HasPrefix(in.Reason, "PHL/PLH") {
			phl++
		}
	}
	if phl != 1 {
		t.Fatalf("thin candles emitted %d PHL entries, want 1: %+v", phl, ins2)
	}
	if e2.State.Refusals["keypair_candle_too_big"] != 0 {
		t.Fatalf("ledger = %v, the thin pair must not refuse", e2.State.Refusals)
	}
}

// ---------- B23: level visit cap ----------

// TestB23LevelVisitCap — B23 (OPEN-NUMBERS Q3, CTO-verified D1.3 p1
// @10:32–11:43 "knock knock"): a level trades its first 3 visits of the day;
// the 4th and later refuse, named and counted.
func TestB23LevelVisitCap(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.KeyLevelTFMinutes = 1
	cfg.LocTriggerFilter = false
	cfg.EMALocationTFMinutes = 0 // the cap must count K only
	cfg.LevelMaxVisits = 3
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 98, 99, 98, 98.5),   // approach from below
		mk(1, 99, 101, 99, 99.5),  // visit 1: touches 100, closes below → reject
		mk(2, 99, 99.6, 99, 99.5), // depart: no touch → visit 1 ends
		mk(3, 99, 101, 99, 99.5),  // visit 2
		mk(4, 99, 99.6, 99, 99.5), // depart
		mk(5, 99, 101, 99, 99.5),  // visit 3
		mk(6, 99, 99.6, 99, 99.5), // depart
		mk(7, 99, 101, 99, 99.5),  // visit 4 → the cap
	}
	now := bars[7].OpenTime + 59_999
	levels := []Level{{Key: "K", Kind: KindKeyLevel, Price: 100}}
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = levels
	e.State.ORB = ORB{Day: dayStartCT(now), High: 130, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideShort, Price: 200}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	for i := 2; i <= len(bars); i++ {
		ins := e.Tick(bars[:i], bars[i-1].CloseTime+1)
		t.Logf("tick %d: visits=%v refusals=%v intents=%d", i, e.State.Visits, e.State.Refusals, len(ins))
	}
	if e.State.Visits["K"] != 4 {
		t.Fatalf("Visits[K] = %d, want 4", e.State.Visits["K"])
	}
	if e.State.Refusals["level_visit_cap"] != 1 {
		t.Fatalf("ledger = %v, want level_visit_cap counted exactly once", e.State.Refusals)
	}
}

// ---------- B20: school-1 flip upgrade ----------

// TestB20SchoolOneFlipUpgrade — B20 (OPEN-NUMBERS Q2, CTO-verified D3.4 p3
// @09:17–12:59): school 1 takes the key-level reject WITHOUT the 5m trigger
// agreeing; when the trigger later flips to the trade's side the evaluator
// emits ConfluenceUpgrade exactly ONCE.
func TestB20SchoolOneFlipUpgrade(t *testing.T) {
	oldHighs := []Level{{Key: "old-high:130", Kind: KindOldExtreme, Price: 130}}
	e, bars, now, _ := b14Fixture(oldHighs, 99.5)
	if e.Cfg.TriggerSchool != 1 {
		t.Fatalf("TriggerSchool default = %d, want 1", e.Cfg.TriggerSchool)
	}
	ins := e.Tick(bars, now)
	if n := placeEntries(ins); n != 1 {
		t.Fatalf("school-1 level reject emitted %d entries, want 1: %+v", n, ins)
	}
	if e.State.SchoolOneSide != SideLong || e.State.SchoolOneUpgraded {
		t.Fatalf("after a school-1 entry without agreement: SchoolOneSide=%q upgraded=%v, want long/false", e.State.SchoolOneSide, e.State.SchoolOneUpgraded)
	}
	// The 5m trigger flips LONG (preset; no new closed 5m bucket on these
	// extra bars, so TriggerTick leaves it untouched).
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 95}
	bars2 := append(append([]market.Kline{}, bars...), market.Kline{
		OpenTime: bars[len(bars)-1].OpenTime + 60_000, CloseTime: bars[len(bars)-1].CloseTime + 60_000,
		Open: 103, High: 104.5, Low: 103, Close: 104.4,
	})
	now2 := bars2[len(bars2)-1].CloseTime + 1
	ins2 := e.Tick(bars2, now2)
	var upgrades int
	for _, in := range ins2 {
		if in.Action == ConfluenceUpgrade {
			upgrades++
			if in.Side != SideLong {
				t.Fatalf("upgrade side = %q, want long", in.Side)
			}
		}
	}
	if upgrades != 1 {
		t.Fatalf("flip tick emitted %d upgrades, want 1: %+v", upgrades, ins2)
	}
	if !e.State.SchoolOneUpgraded {
		t.Fatal("SchoolOneUpgraded must latch true after the flip")
	}
	// A third tick must NOT re-emit (the once-guard).
	bars3 := append(append([]market.Kline{}, bars2...), market.Kline{
		OpenTime: bars2[len(bars2)-1].OpenTime + 60_000, CloseTime: bars2[len(bars2)-1].CloseTime + 60_000,
		Open: 104, High: 104.8, Low: 104, Close: 104.6,
	})
	ins3 := e.Tick(bars3, bars3[len(bars3)-1].CloseTime+1)
	for _, in := range ins3 {
		if in.Action == ConfluenceUpgrade {
			t.Fatalf("the upgrade re-emitted on the next tick: %+v", in)
		}
	}
	_ = strings.HasPrefix // keep strings imported if unused paths change
}
