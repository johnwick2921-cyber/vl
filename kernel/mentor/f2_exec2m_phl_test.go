package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// F2 (W-MENTOR-TRANSCRIPT-TRUTH): the X5-10 exec-2m schedule applies to EVERY
// setup the transcript names — the ISB pair AND PHL/PLH. "Mình trade khung 2
// phút … potential high low" [X8 @15:52–15:57]; after the first 30 minutes of
// RTH he trades the 2m and trails on the frame he entered ("trade khung nào
// thì gồng theo khung đó" [X8 @18:02–18:14]). This pins the PHL half at the
// production call site (Evaluator.Tick): with the knob ON past 09:00 CT the
// reject-touch classification reads the CLOSED 2m candle (so the entry
// geometry is the 2m candle's) and the intent carries ExecTFMin = 2; the knob
// OFF reads the 1m (ExecTFMin 0, byte-identical intents).
//
// Named mutant: eval.go PHL emit `in.ExecTFMin = execTFMin` → `0` →
// TestF2Exec2mPHLCarriesItsFrame RED.

// f2PHLBars builds the 6-bar tape 09:00–09:05 CT (base + 0…5 minutes). 2m
// buckets: [base,base+2m) = bars 0-1 (the structural left low 97.8),
// [base+2m,base+4m) = bars 2-3 (the old high 120), [base+4m,base+6m) = bars
// 4-5 (the reject touch of L=100: low 100, close 100.5 back above). On the 1m
// the touch candle is bar 5 (high 102, low 100); on the 2m it is the last
// closed 2m candle (high 105.4, low 100) — so the two reads emit the PHL at
// DIFFERENT entry prices, proving the frame actually switched, not just a
// stamp.
func f2PHLBars(base int64) ([]market.Kline, int64) {
	mk := func(i int, o, h, l, c float64) market.Kline {
		ot := base + int64(i)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 98, 98.5, 97.8, 98.4), // swing low 97.8 — the structural left low
		mk(1, 98.4, 98.6, 97.9, 98.5),
		mk(2, 98.5, 120, 98.3, 119.5), // old high 120
		mk(3, 119.5, 119.7, 104, 105),
		mk(4, 105, 105.4, 101, 101.4),
		mk(5, 101.4, 102, 100, 100.5), // reject touch of L=100
	}
	return bars, bars[5].CloseTime + 1
}

// f2PHLEval builds a seeded evaluator over the tape with the key level L=100
// and the two old extremes.
func f2PHLEval(cfg Config, bars []market.Kline, now int64) *Evaluator {
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA34 = 0
	e.State.EMA9 = 0
	e.State.SeedLevels = []Level{
		{Key: "L", Kind: KindKeyLevel, Price: 100},
		{Key: "old-low:97.8", Kind: KindOldExtreme, Role: OldLow, Price: 97.8},
		{Key: "old-high:120", Kind: KindOldExtreme, Role: OldHigh, Price: 120},
	}
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 95}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 95}}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e
}

// f2PHLCfg is the frame-pin config: the room rule and the target-shy are off
// so the pin reads the frame stamp and the entry geometry alone.
func f2PHLCfg() Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.EMALocationTFMinutes = 0
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.RoomMultiple = 0
	cfg.PHLTargetShyPts = 0
	cfg.PHLMinCandlesFromExtreme = 2
	cfg.LocTriggerFilter = false
	cfg.NearBoxRoomMultiple = 0
	return cfg
}

func f2PHLEmits(cfg Config, bars []market.Kline, now int64) []Intent {
	var out []Intent
	for _, in := range f2PHLEval(cfg, bars, now).Tick(bars, now) {
		if in.Action == PlaceStopEntry && in.Setup == "PHL" && in.Side == SideLong {
			out = append(out, in)
		}
	}
	return out
}

func TestF2Exec2mPHLCarriesItsFrame(t *testing.T) {
	bars, now := f2PHLBars(auditMs(2026, 9, 15, 9, 0, 0)) // 09:00–09:05 CT

	// OFF (explicit): the 1m read — entry at bar 5's high 102 + 1.0 buffer,
	// no frame stamp.
	off := f2PHLCfg()
	off.Exec2mAfter30m = false
	gotOff := f2PHLEmits(off, bars, now)
	if len(gotOff) == 0 {
		t.Fatal("fixture: knob OFF, the 1m read must emit the PHL")
	}
	if gotOff[0].Price != 103 || gotOff[0].ExecTFMin != 0 {
		t.Fatalf("knob OFF: the 1m PHL must be entry 103 (high 102 + 1.0) with ExecTFMin 0, got Price=%.2f ExecTFMin=%d (%+v)", gotOff[0].Price, gotOff[0].ExecTFMin, gotOff[0])
	}

	// ON at 09:05: the 2m read — entry at the closed 2m candle's high 105.4 +
	// 1.0, stamped ExecTFMin 2.
	on := f2PHLCfg()
	on.Exec2mAfter30m = true
	gotOn := f2PHLEmits(on, bars, now)
	if len(gotOn) == 0 {
		t.Fatal("fixture: knob ON past 09:00, the 2m read must emit the PHL")
	}
	if gotOn[0].Price != 106.4 || gotOn[0].ExecTFMin != 2 {
		t.Fatalf("knob ON: the 2m PHL must be entry 106.4 (high 105.4 + 1.0) with ExecTFMin 2, got Price=%.2f ExecTFMin=%d (%+v)", gotOn[0].Price, gotOn[0].ExecTFMin, gotOn[0])
	}
}

// TestF2Exec2mPHLScheduleAtTick — the schedule boundary for PHL/PLH: with the
// knob ON, a PHL inside the RTH open +30 window (08:45 CT) is STILL the 1m
// read ("Trong dòng 30 phút đầu giờ em vẫn sẽ trade khung 1 phút" [X5
// @00:41–01:14; X11 @17:08–17:25]) — the 2m read starts only at/after 09:00.
func TestF2Exec2mPHLScheduleAtTick(t *testing.T) {
	bars, now := f2PHLBars(auditMs(2026, 9, 15, 8, 44, 0)) // 08:44–08:49 CT

	cfg := f2PHLCfg()
	cfg.Exec2mAfter30m = true
	got := f2PHLEmits(cfg, bars, now)
	if len(got) == 0 {
		t.Fatal("fixture: knob ON inside the RTH open +30 window, the 1m read must emit the PHL")
	}
	if got[0].Price != 103 || got[0].ExecTFMin != 0 {
		t.Fatalf("knob ON before 09:00 CT: the PHL must stay the 1m read (entry 103, ExecTFMin 0), got Price=%.2f ExecTFMin=%d (%+v)", got[0].Price, got[0].ExecTFMin, got[0])
	}
}
