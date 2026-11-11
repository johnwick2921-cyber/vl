package mentor

import "testing"

// TestM472Exec1mAllDayDefault — m472 A1 (OctD4.2-R28). The October course
// overrides the September "2m after the first 30 minutes" schedule: "Trade
// khung 1 phut, chu khong la khung 2 phut. Khung 2 phut no quet banh xac"
// [Oct D4.2 @ 22:56-23:46]; repeated [Oct D5.4 @ 03:09-03:24], [Oct D5.5 @
// 09:16-09:20]; EMA 34 on the 1m only [Oct D5.3 @ 02:00]. Frame:
// /home/hoang/mm-course/oct/scenes/Ngày 4.2 Setup gồng lời và cách move stoploss/s_01389.jpg
// (TradingView 1m chart, "1m" selected).
//
// The pin uses the production DefaultConfig at 09:15 CT (past the old 09:00
// switch): the ISB fixture is a long ISB on the 1m and a doji candle 1 on the
// 2m, so the 1m read EMITS and the 2m read does not. The execution frame, the
// EMA 9/34 read and the box frame must all be the 1m.
func TestM472Exec1mAllDayDefault(t *testing.T) {
	now := auditMs(2026, 9, 15, 9, 15, 0)
	bars := x5BarsAt(now)

	cfg := DefaultConfig()
	cfg.Enabled = true
	if cfg.Exec2mAfter30m {
		t.Errorf("DefaultConfig().Exec2mAfter30m must be false [Oct D4.2 @ 22:56-23:46]")
	}
	e := newISBEval(cfg)
	if got := x5_10Emits(e, bars, now); got != 1 {
		t.Fatalf("09:15 CT default config must read the ISB on the 1m (emit 1), got %d", got)
	}
	if e.State.EMAFrame == 2 {
		t.Fatalf("EMA 9/34 must be read on the 1m at 09:15 CT, State.EMAFrame=%d", e.State.EMAFrame)
	}
	if tf := emaScheduleTF(cfg, 1, now); tf != 1 {
		t.Fatalf("emaScheduleTF at 09:15 CT default = %d, want 1", tf)
	}
	if _, _, frame := e.State.EMAUsed(); frame != "1m" {
		t.Fatalf("EMAUsed frame = %q, want 1m", frame)
	}
}

// TestM472Exec1mScheduleTable pins the default-config EMA frame across the day
// (review round 1): 1m at every hour, so a flip of the default is caught away
// from 09:15 too. The evening-slow 5m schedule is unchanged and is not asserted
// here (it is gated by its own knob, off in DefaultConfig).
func TestM472Exec1mScheduleTable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	for _, hm := range [][2]int{{8, 45}, {9, 15}, {14, 55}, {18, 0}, {3, 30}} {
		now := auditMs(2026, 9, 15, hm[0], hm[1], 0)
		if tf := emaScheduleTF(cfg, 1, now); tf != 1 {
			t.Errorf("%02d:%02d CT default emaScheduleTF = %d, want 1", hm[0], hm[1], tf)
		}
	}
}

// legacy2mCfg is DefaultConfig with the SEPTEMBER 2m-after-09:00 schedule
// switched back ON. m472 A1 flipped the default to the 1m all day [Oct D4.2 @
// 22:56-23:46]; fixtures, goldens and pins that exercise the 2m execution-frame
// code (kept in place behind the knob) or were recorded on the old default build
// their config through this so they keep covering that path.
func legacy2mCfg() Config {
	c := DefaultConfig()
	c.Exec2mAfter30m = true
	// m472 A2: these goldens were recorded on the pre-October build, whose
	// trendline default was "fourth" (Sept D2.3-R7); October is "third" [Oct D3.2 @ 31:17].
	c.TrendlineEntryTouch = TrendlineEntryFourth
	// m472 A3: likewise the PHL offsets were 0.50/0.50 (Sept D2.2-R10); October is 1/1 [Oct D2.3 @ 46:20-47:20].
	c.PHLEntryBufferPts, c.PHLStopBufferPts = 0.5, 0.5
	c.ISBStackTinyStopPts = 12.5 // oct round 2 G2: these goldens ran the September tiny-stop hold
	c.ISBTargetFillClose = false // oct round 2 D2-G3: these goldens ran the September level-target rule
	c.TriggerFromOpen = false    // oct round 2 G4: these goldens read the trigger over the whole history
	c.TriggerEscalation = true   // oct round 2 G3: these goldens ran the September 15m/30m trigger escalation
	c.HTFGateNewsOnly = false    // oct round 2 D2-G7: these goldens ran the m462 all-session 4h/1h gate
	return c
}
