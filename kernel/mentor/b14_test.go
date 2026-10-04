package mentor

import (
	"math"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// b14Fixture builds the B14 tape + evaluator presets. The tape carries a
// prior swing LOW at 98 (bar 2), a far old high at 130 (bar 3) and a reject
// touch at the key level K (bar 5). The seeded old-extreme levels are
// preset by the caller (the SwingPointLevels path is empty on a 6-bar tape,
// so the level set is exactly the preset).
func b14Fixture(oldHighLevels []Level, refLow float64) (*Evaluator, []market.Kline, int64, string) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0 // no EMA34 location line: the pair is K ↔ the old high
	cfg.RoomMultiple = 2
	cfg.PHLMinCandlesFromExtreme = 2
	cfg.PHLTargetShyPts = 0
	cfg.ISBBufferPts = 0
	// LocTriggerFilter off: the 5m trigger the 97-low tape draws is SHORT
	// and would refuse the long PHL at the B2 gate before the higher-low
	// check — the knob exists exactly to switch that filter off for level
	// rejects (CTO 13:20:22Z), and B14 tests the swing/target rules.
	cfg.LocTriggerFilter = false
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 101, 101.5, 101, 101.3),
		mk(1, 101, 101.4, 100, 100.5),
		mk(2, 100, 106.2, 98, 99),      // SWGL 98 @2 (the tape's prior low) + SWGH 106.2 @2 (the near old high)
		mk(3, 99.5, 130, 129.5, 129.7), // SWGH 130 @3 — the far old high (thin: a candle as big as the pair gap must not trigger B21 X4)
		mk(4, 104.5, 105.4, 104.2, 104.8),
		mk(5, 102, 104.2, refLow, 103.9), // the reject-touch reference (green, keeps the 5m trigger long)
	}
	now := bars[5].OpenTime + 59_999
	levels := []Level{
		{Key: "K", Kind: KindKeyLevel, Price: refLow},
	}
	levels = append(levels, oldHighLevels...)
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = levels
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	e.State.Touches = map[string]Touch{
		"K": {LevelKey: "K", Outcome: TouchReject, ApproachedFrom: SideLong, RefBar: bars[5], PriceAtTouch: refLow},
	}
	return e, bars, now, "K"
}

func b14Entries(ins []Intent) int {
	n := 0
	for _, in := range ins {
		if in.Action == PlaceStopEntry {
			n++
		}
	}
	return n
}

// TestB14bNearestOldHighOnly — B14b [D2.2 p2 @03:07–03:14]: the target is the
// NEAREST old extreme on the trade side; a failed nearest high is a SKIP,
// never a farther old high. Nearest old high 106.2 fails the 1:1 floor
// (reward 2 < risk 4.7); the farther 130 would pass — the old loop emitted
// it, the new one refuses with phl_target_below_floor.
func TestB14bNearestOldHighOnly(t *testing.T) {
	oldHighs := []Level{
		{Key: "old-high:130", Kind: KindOldExtreme, Price: 130},
		{Key: "old-high:106.2", Kind: KindOldExtreme, Price: 106.2},
	}
	e, bars, now, _ := b14Fixture(oldHighs, 99.5)
	ins := e.Tick(bars, now)
	if n := b14Entries(ins); n != 0 {
		t.Fatalf("failed nearest high emitted %d entries, want 0: %+v", n, ins)
	}
	if e.State.Refusals["phl_target_below_floor"] == 0 {
		t.Fatalf("ledger = %v, want phl_target_below_floor counted", e.State.Refusals)
	}
}

// TestB14aPriorSwingFromTape — B14a [D2.2 p3 @04:06]: the higher-low check
// reads the nearest PRIOR same-role swing of the TAPE ("đối chiếu với cái
// đáy bên tay trái"), not the old-extreme level set. The stop (97) is not
// higher than the tape's prior low (98) → refused, named. The old read
// (priorSameRole over the level set, which holds only HIGHs here) skipped
// the check and emitted the far high.
func TestB14aPriorSwingFromTape(t *testing.T) {
	oldHighs := []Level{
		{Key: "old-high:130", Kind: KindOldExtreme, Price: 130},
	}
	e, bars, now, _ := b14Fixture(oldHighs, 97)
	ins := e.Tick(bars, now)
	if n := b14Entries(ins); n != 0 {
		t.Fatalf("not-a-higher-low touch emitted %d entries, want 0: %+v", n, ins)
	}
	if e.State.Refusals["phl_not_higher_low"] == 0 {
		t.Fatalf("ledger = %v, want phl_not_higher_low counted", e.State.Refusals)
	}
	// Sanity: the same tape with a genuinely higher low emits (the floor
	// still fails vs 130? entry 104.2, stop 99.5, target 130 → passes).
	e2, bars2, now2, _ := b14Fixture(oldHighs, 99.5)
	ins2 := e2.Tick(bars2, now2)
	if n := b14Entries(ins2); n != 1 {
		t.Fatalf("higher-low touch emitted %d entries, want 1: %+v", n, ins2)
	}
	for _, in := range ins2 {
		if in.Action == PlaceStopEntry && !strings.HasPrefix(in.Reason, "PHL/PLH") {
			t.Fatalf("entry reason = %q, want the PHL path", in.Reason)
		}
	}
}
