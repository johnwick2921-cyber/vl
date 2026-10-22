package trader

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
)

// T7 (release #22, UI-GUIDE-TRUTH): the dashboard chart and the mentor card
// draw ONLY what /api/mentor/truth carries, and that payload was built from
// State.Levels — stored before the trigger lines, the EMA 34 location, the box
// edges and the trendlines were appended. These pins run the PRODUCTION path
// end to end: a real Evaluator.Tick on a real tape, then MentorTruthSnapshot.

// mentorChartTape is the kernel's trendline return tape (kernel/mentor
// trendline_test.go trendlineReturnTape): two confirmed swing lows (97 @2,
// 98.2 @8), a leave (102 @11) and a return that touches the line at bar 13 —
// the 3rd touch, so the support line is a LOCATION. Bar 8's tight body keeps
// the FTGL box top at 98.3, below the line, so the box does not swallow it.
func mentorChartTape() []market.Kline {
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, kernel.CTLocation()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	return []market.Kline{
		mk(0, 100, 101, 100, 100.5),
		mk(1, 100, 101, 99, 100.2),
		mk(2, 100, 101, 97, 100.4),
		mk(3, 100, 101, 98, 100.6),
		mk(4, 100, 101, 98.5, 100.8),
		mk(5, 100, 101, 98, 100.9),
		mk(6, 100, 101, 98.6, 100.5),
		mk(7, 100, 101, 98.9, 100.7),
		mk(8, 98.6, 101, 98.2, 98.3),
		mk(9, 99.5, 101, 99.0, 99.7),
		mk(10, 100, 101, 99.5, 100.5),
		mk(11, 100.5, 102, 100, 101.8),
		mk(12, 101.5, 101.6, 100.2, 100.4),
		mk(13, 100.4, 101.5, 99.1, 101.2),
	}
}

func TestMentorTruthSnapshotCarriesChartObjectsAfterTick(t *testing.T) {
	at := truthAT(t)
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	at.mentorEval = mentor.New(cfg)
	ev := at.mentorEval
	// The 4h trigger line and the ORB are evaluator state the tick carries
	// forward on this short tape; preset them the way a live session holds them.
	ev.State.HTF = mentor.HTF{FourH: mentor.TriggerLine{Dir: mentor.SideLong, Price: 90, MovedAt: 1}}

	bars := mentorChartTape()
	now := mentor.BarCloseInstant(bars[len(bars)-1])
	ev.Tick(bars, now)
	ev.State.ORB = mentor.ORB{Day: 1, High: 101, Low: 100, Drawn: true, Escaped: mentor.SideLong}
	ev.State.ISBBox = &mentor.ISBBox{High: 101, Low: 98.2, Dir: mentor.SideShort, AtTime: bars[5].OpenTime}

	p, ok := at.MentorTruthSnapshot(time.UnixMilli(now))
	if !ok {
		t.Fatalf("snapshot not ok")
	}
	kinds := map[string]int{}
	for _, lv := range p.Levels {
		kinds[lv.Kind]++
	}
	for _, want := range []string{"trendline", "htf_trigger"} {
		if kinds[want] == 0 {
			t.Errorf("panel levels lack %q after a real Tick — kinds=%v", want, kinds)
		}
	}
	if kinds["ftgh_edge"]+kinds["ftgl_edge"] == 0 {
		t.Errorf("panel levels lack the box edges after a real Tick — kinds=%v", kinds)
	}
	if p.Trendlines == nil || len(*p.Trendlines) == 0 {
		t.Fatalf("panel has no trendline anchors after a real Tick: %+v", p.Trendlines)
	}
	tl := (*p.Trendlines)[0]
	if tl.Side != "long" || tl.P0Px != 97 || tl.P1Px != 98.2 || !tl.InLevel || tl.NowMs != bars[13].OpenTime {
		t.Errorf("trendline view = %+v, want the support line 97→98.2 trading at bar 13", tl)
	}
	if !tl.Valid {
		t.Errorf("the 3rd-touch support line must be valid=true (m134): %+v", tl)
	}
	if p.EMASeries == nil || len(*p.EMASeries) == 0 {
		t.Errorf("ema_series must be sent after a real Tick (m134 curve)")
	} else if last := (*p.EMASeries)[len(*p.EMASeries)-1]; last.Ema9 <= 0 || last.Ema34 <= 0 {
		t.Errorf("ema_series tail = %+v, want positive ema9/ema34", last)
	}
	if p.ISBBoxes == nil || len(*p.ISBBoxes) != 1 || (*p.ISBBoxes)[0].Label != "ISB 5m" {
		t.Errorf("isb boxes = %+v, want the one 5m rest box", p.ISBBoxes)
	}
	if p.ORB == nil || p.ORB.High != 101 || p.ORB.Low != 100 || p.ORB.Status != "escaped long" {
		t.Errorf("orb = %+v, want 100–101 escaped long", p.ORB)
	}
	if p.Swing != nil {
		t.Errorf("swing = %+v, want ABSENT while the 4h EMA 34 is cold (no fabricated line)", p.Swing)
	}

	// The wire carries them (what the web reads).
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"trendlines":[`, `"isb_boxes":[`, `"orb":{`, `"kind":"trendline"`, `"valid":`, `"ema_series":[`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("payload lacks %s: %s", key, b)
		}
	}
}

// m134 — a line with two anchors but no 3rd touch yet is FORMING: it is sent
// with valid=false so the chart draws it thin/dashed "waiting for 3rd touch".
func TestMentorTruthSnapshotSendsFormingTrendlines(t *testing.T) {
	at := truthAT(t)
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	at.mentorEval = mentor.New(cfg)
	ev := at.mentorEval

	bars := mentorChartTape()
	// Tick through bar 10: P1 (bar 8) has formed the line, no 3rd touch yet and
	// no 5m close through — the line is FORMING (ValidAt==0, Dead==false).
	sub := bars[:11]
	now := mentor.BarCloseInstant(sub[len(sub)-1])
	ev.Tick(sub, now)

	p, ok := at.MentorTruthSnapshot(time.UnixMilli(now))
	if !ok {
		t.Fatalf("snapshot not ok")
	}
	if p.Trendlines == nil || len(*p.Trendlines) == 0 {
		t.Fatalf("a forming line must be sent: %+v", p.Trendlines)
	}
	tl := (*p.Trendlines)[0]
	if tl.Valid {
		t.Errorf("forming line reported valid=true, want valid=false: %+v", tl)
	}
	if tl.Side != "long" || tl.P0Px != 97 || tl.P1Px != 98.2 {
		t.Errorf("forming line = %+v, want the support line 97→98.2", tl)
	}
}

// Computing (no evaluator yet): the new objects are ABSENT, never `[]` — the
// same canon as Levels/Depth.
func TestMentorTruthSnapshotChartObjectsAbsentWhileComputing(t *testing.T) {
	at := truthAT(t)
	at.mentorEval = nil
	p, ok := at.MentorTruthSnapshot(time.Now())
	if !ok || !p.Computing {
		t.Fatalf("want computing, got ok=%v %+v", ok, p)
	}
	if p.Trendlines != nil || p.ISBBoxes != nil || p.ORB != nil || p.Swing != nil {
		t.Fatalf("chart objects must be absent while computing: %+v", p)
	}
}
