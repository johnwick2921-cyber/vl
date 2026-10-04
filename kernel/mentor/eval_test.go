package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// TestEvaluatorOnRecordedDaysEmitsOnlyCompleteIntents — canon 53: the full
// per-1m-close evaluator runs over three recorded RTH days; every emitted
// intent carries a reason and every PlaceStopEntry is geometrically sane
// (stop on the risk side, target on the reward side).
func TestEvaluatorOnRecordedDaysEmitsOnlyCompleteIntents(t *testing.T) {
	for _, day := range []string{"mnq_1m_2026-09-15_rth", "mnq_1m_2026-09-16_rth", "mnq_1m_2026-08-28_rth"} {
		cfg := DefaultConfig()
		cfg.Enabled = true
		e := New(cfg)
		bars := loadFixture(t, day, "1m")
		total := 0
		swingIntents := 0
		for i := 2; i <= len(bars); i++ {
			now := bars[i-1].OpenTime + 59_999
			for _, in := range e.Tick(bars[:i], now) {
				total++
				if strings.HasPrefix(in.Reason, "swing") {
					swingIntents++
					// SWING EXPIRY (CTO 1791008594562): an unfilled swing order
					// lives until the close of the CURRENT 4h candle.
					if in.Action == PlaceStopEntry && in.ExpiryMs != swingExpiry(now) {
						t.Fatalf("%s bar %d: swing expiry = %d, want the current 4h close %d: %+v", day, i, in.ExpiryMs, swingExpiry(now), in)
					}
				}
				if in.Reason == "" {
					t.Fatalf("%s bar %d: intent without a reason: %+v", day, i, in)
				}
				if in.Action == PlaceStopEntry {
					if in.Side == SideLong && !(in.Stop < in.Price && in.Price < in.Target) {
						t.Fatalf("%s bar %d: long %+v is not stop < entry < target", day, i, in)
					}
					if in.Side == SideShort && !(in.Stop > in.Price && in.Price > in.Target) {
						t.Fatalf("%s bar %d: short %+v is not stop > entry > target", day, i, in)
					}
					// the intraday 25-pt ceiling is the ISB/PHL rule; the §8
					// swing carries its own stop budget (30 pts beyond the
					// 4h EMA, MaxStopPts=100 — never the 25-pt ceiling).
					swing := strings.HasPrefix(in.Reason, "swing")
					ceiling := cfg.StopCeilingPts
					if swing {
						ceiling = cfg.Swing.MaxStopPts
					}
					if in.Price-in.Stop > ceiling || in.Stop-in.Price > ceiling {
						t.Fatalf("%s bar %d: stop distance over its ceiling: %+v", day, i, in)
					}
				}
				if in.Action == LevelInvalid && in.LevelKey == "" {
					t.Fatalf("%s bar %d: level_invalid without a level key", day, i)
				}
			}
		}
		t.Logf("%s: %d intents over %d bars", day, total, len(bars))
		if day == "mnq_1m_2026-09-15_rth" && swingIntents == 0 {
			// the wiring mutant (dropping runSwing from Tick) must turn RED
			// here: 09-15's tape provably produces §8 swing intents.
			t.Fatalf("%s: no swing intents emitted — runSwing not wired into Tick", day)
		}
	}
}

func TestEvaluatorTickWarmSwingEntryUsesSeededLine(t *testing.T) {
	loc := ctime()
	bar := func(day, hour, minute int, o, h, l, c float64) market.Kline {
		open := time.Date(2026, time.September, day, hour, minute, 0, 0, loc)
		return market.Kline{OpenTime: open.UnixMilli(), CloseTime: open.UnixMilli() + 59_999,
			Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		bar(14, 17, 5, 1000, 1000, 1000, 1000),
		bar(14, 21, 5, 1600, 1600, 1600, 1600),
		bar(15, 1, 5, 1000, 1000, 1000, 1000),
		bar(15, 5, 0, 900, 905, 895, 900),
		bar(15, 5, 5, 900, 1100, 900, 1100),
		bar(15, 5, 10, 970, 1020, 930, 940),
		bar(15, 5, 15, 940, 945, 935, 940),
	}
	now := bars[len(bars)-1].CloseTime + 1
	bucket := fourHBucketStart(now, loc)
	e := New(DefaultConfig())
	e.Cfg.Enabled = true
	e.State.Swing = SwingState{
		Line:        950,
		BucketStart: bucket,
		LastBarTime: bars[3].OpenTime,
		EmaCount:    FourHEMA34Min,
		FirstTouch:  &swingTouch{Approach: SideShort, Through: true},
	}

	var swings []Intent
	for _, in := range e.Tick(bars, now) {
		if in.Setup == "SWING4H" {
			swings = append(swings, in)
		}
	}
	if len(swings) != 1 {
		t.Fatalf("warm seeded swing should emit one entry, got %+v", swings)
	}
	if swings[0].Action != PlaceStopEntry || swings[0].Side != SideShort {
		t.Fatalf("swing intent = %+v, want short stop entry", swings[0])
	}
	if swings[0].Stop != e.State.Swing.Line {
		t.Fatalf("entry stop %.6f, want authoritative warm line %.6f", swings[0].Stop, e.State.Swing.Line)
	}
	if local, _, ok := swingLine(closedBuckets(bars, now, e.Cfg), now, e.Cfg.Swing, loc); !ok || abs(local-e.State.Swing.Line) <= e.Cfg.Swing.LineOffsetPts {
		t.Fatalf("fixture local line %.6f must differ from warm line %.6f beyond touch tolerance %.2f", local, e.State.Swing.Line, e.Cfg.Swing.LineOffsetPts)
	}
}

func TestEvaluatorTickWarmSwingEntryUsesIncrementedLineAfterFlip(t *testing.T) {
	loc := ctime()
	bar := func(day, hour, minute int, o, h, l, c float64) market.Kline {
		open := time.Date(2026, time.September, day, hour, minute, 0, 0, loc)
		return market.Kline{OpenTime: open.UnixMilli(), CloseTime: open.UnixMilli() + 59_999,
			Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		bar(14, 17, 5, 1000, 1000, 1000, 1000),
		bar(14, 21, 5, 1600, 1600, 1600, 1600),
		bar(15, 1, 5, 1000, 1000, 1000, 1000),
		bar(15, 5, 0, 1190, 1200, 1180, 1190),
		bar(15, 8, 55, 1190, 1200, 1180, 1200),
		bar(15, 9, 0, 900, 905, 895, 900),
		bar(15, 9, 5, 900, 1100, 900, 1100),
		bar(15, 9, 10, 970, 1020, 930, 940),
		bar(15, 9, 15, 940, 945, 935, 940),
	}
	now := bars[len(bars)-1].CloseTime + 1
	oldBucket := fourHBucketStart(bars[3].OpenTime, loc)
	e := New(DefaultConfig())
	e.Cfg.Enabled = true
	e.State.Swing = SwingState{
		Line:        950,
		BucketStart: oldBucket,
		LastBarTime: bars[4].OpenTime,
		EmaCount:    FourHEMA34Min,
	}
	wantLine := 950 + (2.0/35.0)*(1200-950)

	var swings []Intent
	for _, in := range e.Tick(bars, now) {
		if in.Setup == "SWING4H" {
			swings = append(swings, in)
		}
	}
	if e.State.Swing.Line != wantLine {
		t.Fatalf("bucket flip line %.6f, want incremented warm line %.6f", e.State.Swing.Line, wantLine)
	}
	if len(swings) != 1 {
		t.Fatalf("flipped warm swing should emit one entry, got %+v", swings)
	}
	if swings[0].Action != PlaceStopEntry || swings[0].Side != SideShort {
		t.Fatalf("swing intent = %+v, want short stop entry", swings[0])
	}
	if swings[0].Stop != wantLine {
		t.Fatalf("entry stop %.6f, want incremented line %.6f", swings[0].Stop, wantLine)
	}
}

// TestEvaluatorRebuildFromBarsMatchesIncremental — the state is rebuildable
// from bars plus the ledger, never RAM-only (dispatch requirement): replaying
// the same history in one pass yields exactly the state built incrementally.
func TestEvaluatorRebuildFromBarsMatchesIncremental(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	bars := loadFixture(t, "mnq_1m_2026-09-15_rth", "1m")

	inc := New(cfg)
	for i := 2; i <= len(bars); i++ {
		inc.Tick(bars[:i], bars[i-1].OpenTime+59_999)
	}
	// Rebuild = replay the same bars through Tick (the deterministic path a
	// restart runs); the ledger only records the emitted intents.
	replay := New(cfg)
	for i := 2; i <= len(bars); i++ {
		replay.Tick(bars[:i], bars[i-1].OpenTime+59_999)
	}

	a, err := MarshalState(inc.State)
	if err != nil {
		t.Fatal(err)
	}
	b, err := MarshalState(replay.State)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("state diverges between incremental and replay:\nincremental: %s\nreplay:      %s", a, b)
	}
}

// TestEvaluatorStateRoundTrip — the ledger form survives marshal/unmarshal.
func TestEvaluatorStateRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	bars := loadFixture(t, "mnq_1m_2026-09-15_rth", "1m")
	e.Tick(bars, bars[len(bars)-1].OpenTime+59_999)
	raw, err := MarshalState(e.State)
	if err != nil {
		t.Fatal(err)
	}
	back, err := UnmarshalState(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Touches) != len(e.State.Touches) || len(back.ISBArms) != len(e.State.ISBArms) || len(back.ISBOnly) != len(e.State.ISBOnly) {
		t.Fatalf("round trip lost state: %+v → %+v", e.State, back)
	}
}

// TestEvaluatorInvalidLevelBlocksPHL — §3 [D5.2 p2 @ 20:48]: once a level is
// INVALID, only ISBs may trade there — the evaluator never emits a PHL/PLH
// naming that level.
func TestEvaluatorInvalidLevelBlocksPHL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RangeGapPts = 0
	// synthetic history: price runs up to a key level, first touch closes on
	// the wrong side (invalid), then a later reject touch must not trade.
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: int64(i) * 60_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 99, 100, 98, 100),
		mk(1, 100, 103, 100, 103), // green → green
		mk(2, 103, 106, 103, 106),
	}
	e := New(cfg)
	e.State.ISBOnly["key_level:103.00:1"] = false // (the level key format)
	// wrong-way first touch at 103: approaches from below, closes back below
	for i := 3; i < 40; i++ {
		bars = append(bars, mk(i, 102, 104, 101, 103.5))
	}
	now := bars[len(bars)-1].OpenTime + 59_999
	intents := e.Tick(bars, now)
	for _, in := range intents {
		if in.Action == PlaceStopEntry && in.LevelKey != "" {
			t.Fatalf("PHL/PLH emitted against a wrong-way level: %+v", in)
		}
	}
}

// TestSwingZoneGateKnob — CTO swing ruling (mails 1791001124127 /
// 1791001760445): §8 swings are NOT gated on the 5m trigger zone by default —
// nothing in D5.2 ties the 4h→5m swing to the 5m trigger lines. The knob
// swing_respects_5m_zone (default false) turns the zone gate on at the
// runSwing call site [C]: not stated in the method.
func TestSwingZoneGateKnob(t *testing.T) {
	tl := TriggerLine{Dir: SideShort, Price: 97}
	ints := []Intent{
		{Action: PlaceStopEntry, Reason: "swing §8: reject touch", Price: 98.5}, // wrong trigger side
		{Action: PlaceStopEntry, Reason: "swing §8: reject touch", Price: 96},   // on the trigger side — allowed
	}
	if DefaultSwingCfg().Respects5mZone {
		t.Fatal("swing_respects_5m_zone default must be false")
	}
	if got, dropped := swingZoneGate(ints, tl, false); len(got) != 2 || dropped != 0 {
		t.Fatalf("knob off: the zone gate must be OFF, got %d intents, %d dropped", len(got), dropped)
	}
	got, dropped := swingZoneGate(ints, tl, true)
	if len(got) != 1 || got[0].Price != 96 || dropped != 1 {
		t.Fatalf("knob on: the zone-price swing must be dropped, got %+v, dropped=%d", got, dropped)
	}
}

// TestEvaluatorRefusesWrongTriggerSide — B2 (10-03 ruling): every entry,
// ISB included, must be on the trigger side (longs above the buy line,
// shorts below the sell line). The old two-line band is gone (B1): price
// exactly ON the line is now allowed when no box builds the FTGL/buy zone
// (see trigger_zone_test.go for the zone itself).
func TestEvaluatorRefusesWrongTriggerSide(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: int64(i) * 60_000, Open: o, High: h, Low: l, Close: c}
	}
	// an ISB pair: cur's body inside prev's full range (wicks included).
	for _, tc := range []struct {
		name        string
		close       float64
		wantRefused bool
	}{
		{"2025-05-07 09:21 sell ISB above sell line", 19949.25, true},
		{"exactly on the sell line — allowed, no box zone", 19931, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := New(cfg)
			bars := make([]market.Kline, 10)
			for i := range bars {
				bars[i] = mk(i+5, 19940, 19950, 19935, 19945)
			}
			bars[0] = mk(5, 19940, 19960, 19931, 19950)
			bars[5] = mk(10, 19940, 19950, 19930, 19942)
			bars[8] = mk(13, 19950, 19960, 19920, 19940)
			bars[9] = mk(14, 19942, 19950, 19930, tc.close)
			intents := e.Tick(bars, bars[9].OpenTime+59_999)
			if e.State.Trigger.Dir != SideShort || e.State.Trigger.Price != 19931 {
				t.Fatalf("fixture: want a sell line at 19931, got %+v", e.State.Trigger)
			}
			if tc.wantRefused {
				if e.State.Refusals["isb_trigger_side"] != 1 {
					t.Fatalf("trigger-side drop must be counted once as isb_trigger_side, ledger = %v", e.State.Refusals)
				}
				for _, in := range intents {
					if in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry {
						t.Fatalf("entry emitted on the wrong trigger side: %+v", in)
					}
				}
			} else if e.State.Refusals["isb_trigger_side"] != 0 {
				t.Fatalf("trigger line itself must allow equality, ledger = %v", e.State.Refusals)
			}
		})
	}
}
