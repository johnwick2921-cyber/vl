package mentor

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// m410 — the FTGH/FTGL boxes are drawn on the frame the trader TRADES: "Anh
// chị trade khung nào anh chị là vẽ khung đó … Nếu như anh chị trade khung 1
// phút, anh chị vẽ ở bên khung 1 phút. Anh chị trade khung 2 phút, anh chị vẽ
// khung 2 phút … Đừng có thích khung nào vẽ khung đó dùm" [D4.1 p1
// @22:57–23:21]. The exec schedule (Exec2mAfter30m, exec2mReads2m) trades the
// 1m from 08:30 to 09:00 CT and the 2m afterwards, so a box drawn at 10:00 is a
// 2m box and one drawn at 08:45 a 1m box. The escape read stays the 1m body
// close [D4.1 p1 @20:38–20:45].

// m410Tape is a deterministic wavy 1m tape from `startMin` (CT minute-of-day)
// for n bars on 2026-09-15 — swings on the 1m differ from swings on the 2m.
func m410Tape(startMin, n int) []market.Kline {
	t0 := time.Date(2026, time.September, 15, startMin/60, startMin%60, 0, 0, ctime()).UnixMilli()
	out := make([]market.Kline, 0, n)
	prev := 20000.0
	for i := 0; i < n; i++ {
		c := 20000 + 9*math.Sin(float64(i)/4.0) + 3.1*math.Sin(float64(i)*1.9)
		o := prev
		h := math.Max(o, c) + 0.75 + 0.5*math.Abs(math.Sin(float64(i)*0.7))
		l := math.Min(o, c) - 0.75 - 0.5*math.Abs(math.Cos(float64(i)*1.3))
		out = append(out, market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_999, Open: o, High: h, Low: l, Close: c})
		prev = c
	}
	return out
}

func m410Cfg(knob bool) Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.Exec2mAfter30m = knob
	return cfg
}

// m410Latch ticks the evaluator bar by bar and returns, per role, the box at
// the tick it FIRST latched plus the closed-bar prefix length of that tick.
func m410Latch(cfg Config, bars []market.Kline) (map[string]Box, map[string]int) {
	e := New(cfg)
	first := map[string]Box{}
	at := map[string]int{}
	for k := 3; k <= len(bars); k++ {
		e.Tick(bars[:k], BarCloseInstant(bars[k-1]))
		for role, b := range e.State.LatchedBoxes {
			if _, ok := first[role]; !ok {
				first[role] = b
				at[role] = k
			}
		}
	}
	return first, at
}

// oracleKey is the box the pure builder draws on `tf` over the same closed
// prefix, for the role.
func m410Oracle(bars []market.Kline, tf string, role string) (Box, bool) {
	cfg := DefaultBoxCfg()
	cfg.TF = tf
	now := time.UnixMilli(BarCloseInstant(bars[len(bars)-1])).In(ctime())
	for _, b := range BoxesBuild(bars, cfg, now) {
		if string(b.Kind) == role {
			return b, true
		}
	}
	return Box{}, false
}

// TestM410BoxDrawnOn2mAt1000 — a 10:00 CT box is built from CLOSED 2m
// candles: the latched box is tagged "2m", its edges equal the pure 2m build
// over the same prefix and differ from the 1m build (control: the tape does
// exercise the frame). Mutant (force the 1m frame) → RED.
func TestM410BoxDrawnOn2mAt1000(t *testing.T) {
	bars := m410Tape(9*60+30, 70) // 09:30 → 10:40 CT: every tick is on the 2m schedule
	first, at := m410Latch(m410Cfg(true), bars)
	if len(first) == 0 {
		t.Fatal("no box latched on the tape — fixture is vacuous")
	}
	diff := 0
	for role, b := range first {
		prefix := bars[:at[role]]
		if m := prefix[len(prefix)-1]; rthMinuteOf(m.OpenTime) < 9*60 {
			t.Fatalf("%s latched at a pre-09:00 bar — fixture broken", role)
		}
		if b.TF != "2m" {
			t.Fatalf("%s box latched after 09:00 CT has TF %q, want \"2m\" (drawn on the traded frame)", role, b.TF)
		}
		want, ok := m410Oracle(prefix, "2m", role)
		if !ok {
			t.Fatalf("%s: the pure 2m build over the same prefix draws no box", role)
		}
		if b.Key != want.Key {
			t.Fatalf("%s box key %q != pure 2m build %q", role, b.Key, want.Key)
		}
		if one, ok := m410Oracle(prefix, "1m", role); !ok || one.Key != b.Key {
			diff++
		}
	}
	if diff == 0 {
		t.Fatal("control: the 1m build equals the 2m build on every role — the tape cannot tell the frames apart")
	}
}

// TestM410BoxDrawnOn1mAt0845 — inside the RTH-open +30 minutes the exec frame
// is the 1m, and so are the boxes: untagged (base frame), edges equal to the
// pure 1m build.
func TestM410BoxDrawnOn1mAt0845(t *testing.T) {
	bars := m410Tape(8*60+30, 30) // 08:30 → 09:00 CT
	first, at := m410Latch(m410Cfg(true), bars)
	if len(first) == 0 {
		t.Fatal("no box latched on the 08:30–09:00 tape — fixture is vacuous")
	}
	for role, b := range first {
		prefix := bars[:at[role]]
		if b.TF != "" {
			t.Fatalf("%s box latched inside 08:30–09:00 CT has TF %q, want the base 1m frame (\"\")", role, b.TF)
		}
		want, ok := m410Oracle(prefix, "1m", role)
		if !ok || want.Key != b.Key {
			t.Fatalf("%s box key %q != pure 1m build %q (ok=%v)", role, b.Key, want.Key, ok)
		}
	}
}

// TestM410KnobOffBoxesStayOn1m — Exec2mAfter30m OFF: the 10:00 box is the 1m
// build, untagged: byte-identical to before m410.
func TestM410KnobOffBoxesStayOn1m(t *testing.T) {
	bars := m410Tape(9*60+30, 70)
	first, at := m410Latch(m410Cfg(false), bars)
	if len(first) == 0 {
		t.Fatal("no box latched — fixture is vacuous")
	}
	for role, b := range first {
		if b.TF != "" {
			t.Fatalf("knob OFF: %s box TF %q, want \"\"", role, b.TF)
		}
		want, ok := m410Oracle(bars[:at[role]], "1m", role)
		if !ok || want.Key != b.Key {
			t.Fatalf("knob OFF: %s box key %q != 1m build %q", role, b.Key, want.Key)
		}
	}
}

// TestM410TwoMinuteBoxIgnoresFormingBucket — a box never reads a half-formed
// 2m candle: a prefix ending on the FIRST minute of a 2m bucket draws the same
// boxes as the prefix without that minute.
func TestM410TwoMinuteBoxIgnoresFormingBucket(t *testing.T) {
	bars := m410Tape(9*60+30, 41) // odd length: ends on the first minute of a 2m bucket
	cfg := DefaultBoxCfg()
	cfg.TF = "2m"
	now := time.UnixMilli(BarCloseInstant(bars[len(bars)-1])).In(ctime())
	a := BoxesBuild(bars, cfg, now)
	b := BoxesBuild(bars[:len(bars)-1], cfg, now)
	if len(a) != len(b) {
		t.Fatalf("a forming 2m bucket changed the box set: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Key != b[i].Key || a[i].FormedAtMs != b[i].FormedAtMs {
			t.Fatalf("a forming 2m bucket changed box %d: %+v vs %+v", i, a[i], b[i])
		}
	}
}

// TestM410EscapeStaysOneMinuteBodyClose — a 2m-drawn box's D14 escape is
// still the 1m BODY close ("Break là một cây nến một phút ... nó đóng bên
// ngoài" [D4.1 p1 @20:38-20:45]): a lone 1m body above the top flips the box
// even though the enclosing 2m candle's body straddles the edge. Mutant (read
// the escape on the 2m) gives RED.
func TestM410EscapeStaysOneMinuteBodyClose(t *testing.T) {
	t0 := time.Date(2026, time.September, 15, 10, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 99, 100, 98, 99), mk(1, 99, 100, 98, 99), // formation bucket
		mk(2, 99, 99.5, 98.5, 99.5), mk(3, 100.5, 101.5, 100.2, 101), // 1m body 100.5 to 101 above top 100; 2m body 99 to 101 straddles
		mk(4, 99, 99.5, 98.5, 99), mk(5, 99, 99.5, 98.5, 99),
	}
	b := Box{Kind: FTGH, Top: 100, Bottom: 98, TF: "2m", FormedAtMs: bars[0].OpenTime}
	refreshBox(bars, &b, DefaultBoxCfg())
	if b.FormedAt != 1 {
		t.Fatalf("FormedAt = %d, want 1 (the formation bucket's last 1m)", b.FormedAt)
	}
	if !b.Flipped {
		t.Fatal("a 1m body close above the top did not flip the 2m-drawn box: the escape must stay on the 1m")
	}
	if tf2 := barsForTF(bars, "2m"); escaped(tf2, b, 0) {
		t.Fatal("control: the 2m candle body straddles the top; the 2m read must NOT call it an escape")
	}
}

// ── REVIEW-BACK (DS-105): knob-ON pins through the production Tick path ─────

// m410Double turns a 2m-shaped template into a 1m tape: every template bar
// becomes TWO identical 1m bars, so the 2m aggregate reproduces the template
// exactly while the 1m series has NO swing at all (a swing needs a strictly
// lower low / higher high than its left neighbour, and each 1m bar equals its
// predecessor). Any box drawn on this tape therefore proves the 2m frame.
func m410Double(tpl []market.Kline, startMin int) []market.Kline {
	t0 := time.Date(2026, time.September, 15, startMin/60, startMin%60, 0, 0, ctime()).UnixMilli()
	out := make([]market.Kline, 0, 2*len(tpl))
	for i, b := range tpl {
		for j := 0; j < 2; j++ {
			o := t0 + int64(2*i+j)*60_000
			out = append(out, market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: b.Open, High: b.High, Low: b.Low, Close: b.Close})
		}
	}
	return out
}

// m410TickAll ticks the production call site on every closed 1m bar with the
// production clock (BarCloseInstant).
func m410TickAll(e *Evaluator, bars []market.Kline) {
	for k := 3; k <= len(bars); k++ {
		e.Tick(bars[:k], BarCloseInstant(bars[k-1]))
	}
}

// TestM410B52NewZoneOnTwoMinuteFrame — pin 1: a 2m-drawn FTGL escapes (the
// 1m body close) and the NEXT zone is drawn on the frame of the current time
// (2m, B52). The 1m view of this tape has no swings, so the boxes can only
// come from the 2m frame. Mutant (force 1m) → RED.
func TestM410B52NewZoneOnTwoMinuteFrame(t *testing.T) {
	tpl, _ := b52Tape()
	bars := m410Double(tpl, 9*60+26) // 09:26 → 10:00 CT: all on the 2m schedule
	cfg := m410Cfg(true)
	e := New(cfg)
	m410TickAll(e, bars)
	first := e.State.LatchedBoxes["ftgl"]
	if first.Key != "ftgl:96.80:96.00" || first.TF != "2m" {
		t.Fatalf("first FTGL = %+v, want ftgl:96.80:96.00 drawn on the 2m", first)
	}
	nb, ok := b52LastBox(e.State.PostEscapeBoxes, FTGL)
	if !ok {
		t.Fatalf("no post-escape FTGL after the 1m-body escape — post = %+v", e.State.PostEscapeBoxes)
	}
	if nb.Bottom != 92 || nb.Top != 92.9 || nb.TF != "2m" {
		t.Fatalf("new zone = %+v, want [92, 92.9] drawn on the 2m (the frame of the current time)", nb)
	}
	if nb.Top > first.Bottom {
		t.Fatalf("new zone not outside the old floor: %+v", nb)
	}
}

// TestM410ISBBoxBanOnTwoMinuteBox — pin 2: an ISB box standing over a 2m-drawn
// box's return refuses the box entry (box_isb_ban); the control (no ISB box)
// trades it, so the ban is what removed the entry. Mutant (force 1m) → the
// control emits no entry → RED.
func TestM410ISBBoxBanOnTwoMinuteBox(t *testing.T) {
	tpl := []market.Kline{
		{Open: 100, High: 101, Low: 99, Close: 100},
		{Open: 100, High: 102, Low: 99, Close: 101},
		{Open: 101, High: 103, Low: 95, Close: 96},
		{Open: 96.5, High: 97, Low: 96, Close: 96.8},
		{Open: 96.8, High: 97, Low: 96.2, Close: 96.8},
		{Open: 96.8, High: 97, Low: 95.5, Close: 96},
		{Open: 96.2, High: 97, Low: 95.8, Close: 96.8},
		{Open: 96.5, High: 97.1, Low: 95.9, Close: 97},
	}
	bars := m410Double(tpl, 9*60+44) // 09:44 → 10:00 CT
	fixture := func() *Evaluator {
		cfg := m410Cfg(true)
		cfg.RoomMultiple = 0.05
		e := New(cfg)
		now := BarCloseInstant(bars[len(bars)-1])
		e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}
		e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
		return e
	}
	boxEntries := func(ins []Intent) int {
		n := 0
		for _, in := range ins {
			if strings.HasPrefix(in.Reason, "box edge return") {
				n++
			}
		}
		return n
	}
	run := func(e *Evaluator) (entries int) {
		for k := 3; k <= len(bars); k++ {
			entries += boxEntries(e.Tick(bars[:k], BarCloseInstant(bars[k-1])))
		}
		return entries
	}
	control := fixture()
	if n := run(control); n == 0 {
		t.Fatalf("control: the 2m-drawn box emitted no box-edge entry (latched %+v, refusals %v) — the pin would be vacuous", control.State.LatchedBoxes, control.State.Refusals)
	}
	if b := control.State.LatchedBoxes["ftgl"]; b.TF != "2m" {
		t.Fatalf("control box %+v is not 2m-drawn", b)
	}
	banned := fixture()
	banned.State.ISBBox = &ISBBox{High: 100, Low: 90}
	if n := run(banned); n != 0 {
		t.Fatalf("an ISB box over the 2m box's return still emitted %d box entries, want 0", n)
	}
	if banned.State.Refusals["box_isb_ban"] == 0 {
		t.Fatalf("ledger = %v, want box_isb_ban counted", banned.State.Refusals)
	}
}

// TestM410TwoMinuteBoxEdgesAreLocations — pin 3: after a tick at 10:00 CT the
// 2m box's FTGH/FTGL edges are in the evaluator's level set (the set the
// location gate and the chart read). Mutant (force 1m) → no box, no edges → RED.
func TestM410TwoMinuteBoxEdgesAreLocations(t *testing.T) {
	tpl, _ := b52Tape()
	bars := m410Double(tpl, 9*60+26)
	e := New(m410Cfg(true))
	m410TickAll(e, bars)
	if last := time.UnixMilli(bars[len(bars)-1].OpenTime).In(ctime()); last.Hour() != 9 || last.Minute() != 59 {
		t.Fatalf("fixture's last bar opens %s, want 09:59 (the 10:00 tick)", last.Format("15:04"))
	}
	box := e.State.LatchedBoxes["ftgl"]
	if box.TF != "2m" {
		t.Fatalf("latched FTGL %+v is not 2m-drawn", box)
	}
	var top, bottom bool
	for _, l := range e.State.ChartLevels {
		if l.Key == box.Key+":top" && l.Kind == KindFTGHEdge && l.Price == box.Top {
			top = true
		}
		if l.Key == box.Key+":bottom" && l.Kind == KindFTGLEdge && l.Price == box.Bottom {
			bottom = true
		}
	}
	if !top || !bottom {
		t.Fatalf("2m box edges missing from the level set (top=%v bottom=%v): %+v", top, bottom, e.State.ChartLevels)
	}
}

// TestM410RestartParityOnTwoMinuteFrame — pin 4: on the real 10-13/10-14 tape
// with Exec2mAfter30m ON (the production default) a restart (seed replay)
// reproduces the continuous bot's 2m-frame box latch at every sampled instant.
// It pins the seed-cut rule (8 bars of margin + a 2m-bucket-aligned window
// start). Mutant (cut at first-2, no alignment) → the restart redraws a box →
// RED.
func TestM410RestartParityOnTwoMinuteFrame(t *testing.T) {
	ssPin1mBoxes = false
	all := loadSeedStateFixture(t)
	full := ssTape(all, true, 1<<62)
	key := func(m map[string]Box) string {
		var parts []string
		for _, k := range []string{"ftgl", "ftgh"} {
			if b, ok := m[k]; ok {
				parts = append(parts, fmt.Sprintf("%s %.2f–%.2f formed %s tf=%s", k, b.Bottom, b.Top, lrCT(time.UnixMilli(b.FormedAtMs)), b.TF))
			}
		}
		return strings.Join(parts, " · ")
	}
	samples, twoM := 0, 0
	ssLive(t, full, 1791992400000, func(i int, live *Evaluator, _ []Intent) { // → 10:40 CT
		if (i % 13) != 0 {
			return
		}
		samples++
		re := ssSeed(full, BarCloseInstant(full[i]), ssCPI)
		if key(re.State.LatchedBoxes) != key(live.State.LatchedBoxes) {
			t.Fatalf("%s CT: restart latch [%s] ≠ continuous [%s]", lrCT(time.UnixMilli(full[i].OpenTime)), key(re.State.LatchedBoxes), key(live.State.LatchedBoxes))
		}
		for _, b := range live.State.LatchedBoxes {
			if b.TF == "2m" {
				twoM++
				break
			}
		}
	})
	if samples == 0 || twoM == 0 {
		t.Fatalf("vacuous: %d samples, %d with a 2m-drawn latched box", samples, twoM)
	}
}
