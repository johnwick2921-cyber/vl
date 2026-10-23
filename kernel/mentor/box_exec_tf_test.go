package mentor

import (
	"math"
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
