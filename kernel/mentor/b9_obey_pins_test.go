package mentor

import (
	"math"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// B9 [D5.1 p1 @16:24, @19:11–20:07] (38dc7b6e1): every setup obeys the same
// day/HTF gates — box trades obey the 4h/1h direction, the day-off and the
// spent cap; ISBs obey the spent cap; PHL/PLH already did. These pins drive
// the Tick call site and assert the VALUES (cap distance, refusal key), so a
// gate dropped from any one path turns exactly one pin RED.

const b9CapPts = 15.0

// b9BoxFixture is the FTGL reject-return tape (one LONG return, two box
// entries) with the HTF and the day latch preset.
func b9BoxFixture(htf HTF, verdict DayVerdict) (*Evaluator, []market.Kline, int64) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0 // no EMA34 location line: the seeded level is the only target
	cfg.RoomMultiple = 0.05
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 102, 99, 101),
		mk(2, 101, 103, 95, 96),   // bottom 1: the extreme low 95
		mk(3, 96.5, 97, 96, 96.8), // confirms the extreme
		mk(4, 96.8, 97, 96.2, 96.8),
		mk(5, 96.8, 97, 95.5, 96),   // bottom 2: later confirmed higher low
		mk(6, 96.2, 97, 95.8, 96.8), // confirms bottom 2
		mk(7, 96.5, 97.1, 95.9, 97), // FTGL reject return → LONG
	}
	e := New(cfg)
	now := bars[7].OpenTime + 59_999
	// One far key level above: the box target is the next level beyond the
	// entry, so the uncapped target sits well past the 15-pt cap.
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = []Level{{Key: "far", Kind: KindKeyLevel, Price: 125}}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	// Freeze the 5m trigger BELOW the entry so the trigger-retest level does
	// not sit between the entry and the far key level — the box target must
	// read the seeded far level (125), not a trigger line drawn from the tape.
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 80, LastBucket: t0 + 5*60_000}
	e.State.HTF = htf
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: verdict}
	return e, bars, now
}

func b9BoxEntries(ins []Intent) []Intent {
	var out []Intent
	for _, in := range ins {
		if strings.HasPrefix(in.Reason, "box edge return") {
			out = append(out, in)
		}
	}
	return out
}

func b9Dist(in Intent) float64 {
	return abs(in.Target - in.Price)
}

// (a) a LONG box entry against a SHORT 4h is refused, named, zero entries.
func TestB9BoxAgainst4hRefused(t *testing.T) {
	e, bars, now := b9BoxFixture(HTF{FourH: TriggerLine{Dir: SideShort, Price: 99}}, DayTrade)
	if got := b9BoxEntries(e.Tick(bars, now)); len(got) != 0 {
		t.Fatalf("box LONG vs 4h SHORT emitted %d entries, want 0: %+v", len(got), got)
	}
	if e.State.Refusals["box_htf_side_mismatch"] == 0 {
		t.Fatalf("ledger = %v, want box_htf_side_mismatch counted", e.State.Refusals)
	}
	// Control: the same tape with the 4h LONG emits, so the refusal above is
	// the direction gate and not a dead fixture.
	e2, bars2, now2 := b9BoxFixture(HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}, DayTrade)
	if got := b9BoxEntries(e2.Tick(bars2, now2)); len(got) == 0 {
		t.Fatalf("control (4h LONG) emitted no box entries: ledger %v", e2.State.Refusals)
	}
}

// (b1) a box trade on a DayOff day is refused before any entry is built.
func TestB9BoxOnDayOffRefused(t *testing.T) {
	e, bars, now := b9BoxFixture(HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}, DayOff)
	if got := b9BoxEntries(e.Tick(bars, now)); len(got) != 0 {
		t.Fatalf("day-off tick emitted %d box entries, want 0: %+v", len(got), got)
	}
	if e.State.Refusals["box_day_off"] == 0 {
		t.Fatalf("ledger = %v, want box_day_off counted", e.State.Refusals)
	}
}

// (b2) a box trade on a DaySpent day is CAPPED at the 15-pt target — the
// VALUE is asserted (the old pin only counted entries, so dropping the cap
// stayed green). The control proves the uncapped target is farther than 15.
func TestB9BoxOnSpentDayCapsTarget(t *testing.T) {
	htf := HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}
	ctl, cbars, cnow := b9BoxFixture(htf, DayTrade)
	uncapped := b9BoxEntries(ctl.Tick(cbars, cnow))
	if len(uncapped) == 0 {
		t.Fatalf("control (trade day) emitted no box entries: ledger %v", ctl.State.Refusals)
	}
	wide := false
	for _, in := range uncapped {
		if b9Dist(in) > b9CapPts+1e-9 {
			wide = true
		}
	}
	if !wide {
		t.Fatalf("control targets %+v are all within %.0f pts — the cap pin could not discriminate", uncapped, b9CapPts)
	}

	e, bars, now := b9BoxFixture(htf, DaySpent)
	got := b9BoxEntries(e.Tick(bars, now))
	if len(got) == 0 {
		t.Fatalf("spent-day tick emitted no box entries (cap, not refusal): ledger %v", e.State.Refusals)
	}
	for _, in := range got {
		if in.Target == 0 {
			t.Fatalf("spent-day box entry has no target: %+v", in)
		}
		if b9Dist(in) > b9CapPts+1e-9 {
			t.Fatalf("spent-day box target %.2f is %.2f pts from entry %.2f — must be capped at %.0f",
				in.Target, b9Dist(in), in.Price, b9CapPts)
		}
		if !in.SpentDay {
			t.Fatalf("spent-day box entry not stamped SpentDay: %+v", in)
		}
	}
}

// (d) PHL at the Tick call site: on a spent day the target is CAPPED at 15
// pts; on a DayOff day the entry is refused (phl_day_off), zero entries.
func TestB9PHLSpentDayCappedDayOffRefused(t *testing.T) {
	oldHighs := []Level{{Key: "old-high:130", Kind: KindOldExtreme, Price: 130}}

	ctl, cbars, cnow, _ := b14Fixture(oldHighs, 99.5)
	cins := ctl.Tick(cbars, cnow)
	var base *Intent
	for i := range cins {
		if cins[i].Action == PlaceStopEntry {
			base = &cins[i]
		}
	}
	if base == nil || b9Dist(*base) <= b9CapPts {
		t.Fatalf("control PHL entry = %+v (ledger %v): need an entry with a target beyond %.0f pts", base, ctl.State.Refusals, b9CapPts)
	}

	e, bars, now, _ := b14Fixture(oldHighs, 99.5)
	e.State.Day.Verdict = DaySpent
	var got *Intent
	ins := e.Tick(bars, now)
	for i := range ins {
		if ins[i].Action == PlaceStopEntry {
			got = &ins[i]
		}
	}
	if got == nil {
		t.Fatalf("spent-day PHL emitted no entry (cap, not refusal): ledger %v", e.State.Refusals)
	}
	if b9Dist(*got) > b9CapPts+1e-9 {
		t.Fatalf("spent-day PHL target %.2f is %.2f pts from entry %.2f — must be capped at %.0f",
			got.Target, b9Dist(*got), got.Price, b9CapPts)
	}

	off, obars, onow, _ := b14Fixture(oldHighs, 99.5)
	off.State.Day.Verdict = DayOff
	if n := b14Entries(off.Tick(obars, onow)); n != 0 {
		t.Fatalf("day-off PHL emitted %d entries, want 0", n)
	}
	if off.State.Refusals["phl_day_off"] == 0 {
		t.Fatalf("ledger = %v, want phl_day_off counted", off.State.Refusals)
	}
}
