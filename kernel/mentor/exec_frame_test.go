package mentor

import (
	"testing"

	"vl/market"
)

// ── EXTRAS-ALIGN item 3 (kernel half) — the trail frame follows the entry ──
//
// Named mutant (anchored to ONE site, run RED then restored):
//   E3k drop the stamp — eval.go ISB emit `chosen.ExecTFMin = execTFMin` →
//      `chosen.ExecTFMin = 0` → TestExec2mISBIntentCarriesItsFrame RED.

// TestExec2mISBIntentCarriesItsFrame — item 3, the kernel half: with the
// X5-10 exec-2m knob ON at 09:04 CT the ISB is read on the closed 2m candles
// and the intent carries ExecTFMin = 2, so the trader trails its runner on
// the 2m ("trade khung nào thì gồng theo khung đó" [X8 @18:02–18:14]). Knob
// OFF: the 1m read, ExecTFMin 0 — byte-identical intents.
func TestExec2mISBIntentCarriesItsFrame(t *testing.T) {
	base := auditMs(2026, 9, 15, 8, 58, 0)
	mk := func(i int, o, h, l, c float64) market.Kline {
		ot := base + int64(i)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 121, 122, 95, 120.5), // head: keeps the EMA target above the entry; its deep wick keeps the 5m trigger long
		mk(1, 121, 121.5, 120, 121),
		mk(2, 99, 104, 98.5, 103), // 2m candle A (09:00–09:01): green 99→106, range 98.5–106
		mk(3, 103, 106, 102, 106),
		mk(4, 101, 102.1, 100.9, 101.5), // 2m candle B (09:02–09:03): body 101–102 inside A
		mk(5, 101.5, 102, 101, 102),
	}
	now := bars[len(bars)-1].CloseTime + 1 // 09:04 CT
	isbs := func(cfg Config) []Intent {
		var out []Intent
		for _, in := range newISBEval(cfg).Tick(bars, now) {
			if (in.Action == PlaceStopLimitEntry || in.Action == PlaceStopEntry) && in.Setup == "ISB" {
				out = append(out, in)
			}
		}
		return out
	}
	// The room rule is OFF here (RoomMultiple 0): this pin is about the frame
	// stamp, and the fixture's own swing high at 106 sits inside the room.
	off := DefaultConfig()
	off.Enabled = true
	off.RoomMultiple = 0
	off.Exec2mAfter30m = false
	gotOff := isbs(off)
	if len(gotOff) == 0 {
		t.Fatal("fixture: knob OFF, the 1m read must emit the ISB")
	}
	for _, in := range gotOff {
		if in.ExecTFMin != 0 {
			t.Fatalf("knob OFF: a 1m ISB must carry no frame stamp (byte-identical), got ExecTFMin=%d", in.ExecTFMin)
		}
	}
	on := DefaultConfig()
	on.Enabled = true
	on.RoomMultiple = 0
	on.Exec2mAfter30m = true
	gotOn := isbs(on)
	if len(gotOn) == 0 {
		t.Fatal("fixture: knob ON at 09:04, the 2m read must emit the ISB")
	}
	for _, in := range gotOn {
		if in.ExecTFMin != 2 {
			t.Fatalf("knob ON: the 2m-read ISB must carry ExecTFMin=2, got %d (%+v)", in.ExecTFMin, in)
		}
	}
}

// TestClosedCandleTFIsTheExecReadsBucket — the trail candle is the SAME 2m
// bucket the exec-2m read builds, and only once it has closed.
func TestClosedCandleTFIsTheExecReadsBucket(t *testing.T) {
	base := auditMs(2026, 9, 15, 10, 0, 0) // an even minute
	mk := func(i int, o, h, l, c float64) market.Kline {
		ot := base + int64(i)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c, Final: true}
	}
	bars := []market.Kline{mk(0, 100, 103, 99, 102), mk(1, 102, 104, 101, 103), mk(2, 103, 105, 102.5, 104)}
	if c, ok := ClosedCandleTF(bars[:2], 2); !ok || c.High != 104 || c.Low != 99 || c.OpenTime != base {
		t.Fatalf("the 10:00 2m candle closes with the 10:01 bar: got %+v ok=%v", c, ok)
	}
	if _, ok := ClosedCandleTF(bars, 2); ok {
		t.Fatal("the 10:02 bar only half-fills its 2m candle — nothing closed on it")
	}
	if c, ok := ClosedCandleTF(bars, 1); !ok || c.OpenTime != bars[2].OpenTime {
		t.Fatalf("tf 1 is the newest 1m bar itself: %+v", c)
	}
}
