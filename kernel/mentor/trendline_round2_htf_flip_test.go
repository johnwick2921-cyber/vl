package mentor

import (
	"testing"
	"time"
)

// round-2 D3-G12 — a persistent 1H/4H trendline is never deleted. "Khung giờ lớn
// mình để luôn, mình không bao giờ xóa" [Oct D3.2 @ 08:27] (OctD3.2-R20): the 4H
// line from the July low is kept past its break; the candles under the broken
// line see it as resistance, "đụng resistance thì drop" [@ 14:18-14:47] (R29);
// R32 [@ 20:20] the Friday long at the 4H falling line. Same tape and break as the
// m428 R1 pin (htfPersistenceTape: a 1h support line cut by ONE 5m candle wholly
// below it), through the production htFTrendlines.

func round2HTFBreak(t *testing.T, flip bool) (live, broken []Trendline, e *Evaluator) {
	t.Helper()
	bars, _ := htfPersistenceTape()
	for i := 1210; i < 1215; i++ {
		bars[i].Open, bars[i].High, bars[i].Low, bars[i].Close = 96, 96, 95, 95.5
	}
	e = &Evaluator{}
	e.Cfg.HTFTrendlineFlip = flip
	e.htFTrendlines(bars[:1210], time.UnixMilli(BarCloseInstant(bars[1209])))
	live, broken = e.htFTrendlines(bars[:1216], time.UnixMilli(BarCloseInstant(bars[1215])))
	return live, broken, e
}

func TestRound2D3G12BrokenHTFLineIsKeptWithTheRoleFlipped(t *testing.T) {
	live, broken, e := round2HTFBreak(t, true)
	nBroken := 0
	for _, tl := range broken {
		if tl.TFMin == 60 {
			nBroken++
		}
	}
	if nBroken == 0 {
		t.Fatal("control: the 1h support line must be reported broken")
	}
	var flipped *Trendline
	for i, tl := range live {
		if _, ok := e.tlFlip[tl.key()]; tl.TFMin == 60 && ok {
			flipped = &live[i]
		}
	}
	if flipped == nil {
		t.Fatalf("a broken 1h line must stay as a flipped line, live = %+v", live)
	}
	if flipped.Side != SideShort {
		t.Fatalf("a broken SUPPORT must become RESISTANCE (Side short), got %s", flipped.Side)
	}
	if flipped.ValidAt != 0 {
		t.Fatal("a flipped line is a location again only after a retest touch (ValidAt 0 at the flip)")
	}
	// the next tick keeps it (never deleted) and does not re-flip it back.
	bars, _ := htfPersistenceTape()
	for i := 1210; i < 1215; i++ {
		bars[i].Open, bars[i].High, bars[i].Low, bars[i].Close = 96, 96, 95, 95.5
	}
	live2, _ := e.htFTrendlines(bars[:1218], time.UnixMilli(BarCloseInstant(bars[1217])))
	kept := false
	for _, tl := range live2 {
		if _, ok := e.tlFlip[tl.key()]; tl.TFMin == 60 && ok && tl.Side == SideShort {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("the flipped line vanished on the next tick: %+v", live2)
	}
}

func TestRound2D3G12KnobOffDeletesTheBrokenLine(t *testing.T) {
	live, _, _ := round2HTFBreak(t, false)
	for _, tl := range live {
		if tl.TFMin == 60 {
			t.Fatalf("knob off: a broken 1h line is deleted (m428 R3), got live %+v", tl)
		}
	}
}
