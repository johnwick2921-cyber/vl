package mentor

import (
	"testing"

	"vl/market"
)

// TestISBIdentifiedByBodyOnly — RULES-FIX-v3 R1 [D1.4 p1 @ 06:13–08:47]: the
// ISB candle's BODY must sit inside the PREVIOUS candle's FULL range, wicks
// included ("Inside có nghĩa là cái BODY của cây nến đó phải nằm BÊN TRONG cây
// nến trend"); the ISB candle's wicks and colour are free.
func TestISBIdentifiedByBodyOnly(t *testing.T) {
	prev := market.Kline{Open: 100, Close: 105, High: 107, Low: 98}
	// body inside the full range, wicks far outside → ISB (wicks ignored)
	cur := market.Kline{Open: 101, Close: 104, High: 110, Low: 90}
	if !IsISB(prev, cur) {
		t.Fatal("body inside the full range is an ISB regardless of its own wicks")
	}
	// body pokes ABOVE the previous HIGH → not an ISB
	cur2 := market.Kline{Open: 103, Close: 108, High: 108.5, Low: 99}
	if IsISB(prev, cur2) {
		t.Fatal("body beyond the previous FULL range is NOT an ISB")
	}
	// body below the previous LOW → not an ISB
	cur3 := market.Kline{Open: 99, Close: 97, High: 99.5, Low: 96}
	if IsISB(prev, cur3) {
		t.Fatal("body below the previous low is NOT an ISB")
	}
	// equal range → ISB (resting in place)
	cur4 := market.Kline{Open: 100, Close: 105, High: 105.5, Low: 99.5}
	if !IsISB(prev, cur4) {
		t.Fatal("equal body is an ISB")
	}
}

// TestISBOrdersWickInclusiveWithBuffer — R1 [D1.4 p1 @ 10:33–11:25, 22:22–22:30,
// 24:41–24:55]: ENTRY at the ISB candle's OWN extreme, stop at the opposite
// extreme, 1.5-pt buffer BOTH sides outward, order type STOP-LIMIT with
// limit = the stop (trigger) price.
func TestISBOrdersWickInclusiveWithBuffer(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	c := market.Kline{High: 100.5, Low: 98.25}
	long, short := ISBOrders(c, cfg)
	if long.Price != 102.0 || long.Stop != 96.75 || long.Limit != 102.0 {
		t.Fatalf("long = %+v, want price 102.0 stop 96.75 limit 102.0", long)
	}
	if short.Price != 96.75 || short.Stop != 102.0 || short.Limit != 96.75 {
		t.Fatalf("short = %+v, want price 96.75 stop 102.0 limit 96.75", short)
	}
	if long.Action != PlaceStopLimitEntry || short.Action != PlaceStopLimitEntry {
		t.Fatalf("actions = %q/%q, want stop-limit both sides", long.Action, short.Action)
	}
}

// TestISBDirectionAndStopLimitOrder — R1 [D1.4 p1 @ 09:20–10:20]: direction is
// the COLOUR OF CANDLE 1 (green → long, red → short); the emitted order is the
// single stop-limit on that side, and a twenties stop is skipped.
func TestISBDirectionAndStopLimitOrder(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	green := market.Kline{Open: 100, Close: 106, High: 106.5, Low: 99.5}
	red := market.Kline{Open: 106, Close: 100, High: 106.5, Low: 99.5}
	cur := market.Kline{Open: 102, Close: 104, High: 106.0, Low: 100.0}
	if ISBDirection(green) != SideLong || ISBDirection(red) != SideShort {
		t.Fatalf("direction = %q/%q, want long/short", ISBDirection(green), ISBDirection(red))
	}
	dir, in, ok, _ := ISBStopLimitOrder(green, cur, cfg)
	if !ok || dir != SideLong || in.Side != SideLong || in.Action != PlaceStopLimitEntry {
		t.Fatalf("order = %+v ok=%v dir=%q, want a long stop-limit", in, ok, dir)
	}
	dir, _, ok, _ = ISBStopLimitOrder(red, cur, cfg)
	if !ok || dir != SideShort {
		t.Fatalf("short side order: ok=%v dir=%q", ok, dir)
	}
	// a candle whose stop lands in the twenties is skipped, citing D4.1
	wide := market.Kline{Open: 102, Close: 104, High: 112.0, Low: 93.0}
	if _, _, ok, reason := ISBStopLimitOrder(green, wide, cfg); ok || reason == "" {
		t.Fatalf("twenties stop: ok=%v reason=%q, want skip with a cited reason", ok, reason)
	}
}

// TestISBStopVerdict — RULES-FIX-v3 R1 [D4.1 p1 @ 05:41]: NO fixed 5–6-pt stop
// and NO ceiling for the ISB; the ONLY skip is a stop in the twenties.
func TestISBStopVerdict(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cases := []struct {
		name   string
		span   float64
		wantOK bool
	}{
		{"small 4pt stop", 1.0, true},        // 1 + 3 buffer = 4 — taken (no minimum)
		{"5pt stop", 2.0, true},              // 2 + 3 = 5
		{"6pt stop", 3.0, true},              // 3 + 3 = 6
		{"twenties", 17.0, false},            // 17 + 3 = 20 — the only skip
		{"thirties not skipped", 28.0, true}, // 31 — only the twenties skip (no ceiling)
	}
	for _, c := range cases {
		stop, ok, _ := ISBStopVerdict(market.Kline{High: 100 + c.span/2, Low: 100 - c.span/2}, cfg)
		if ok != c.wantOK {
			t.Fatalf("%s: ok=%v (stop %.2f), want %v", c.name, ok, stop, c.wantOK)
		}
	}
}

// TestISBStackingArithmetic — the verified stacking rule (CTO 2026-10-03 [A],
// D4.2 p2 @ 08:21–09:01, @ 16:02–16:06; D5.1 p2 @ 04:00–04:08): the reference is
// the FIRST inside-bar candle (I1 = stack candle 1). Candles 2 and 3 inside I1
// → KEEP; candle 4 not filling → CANCEL. `inside` counts the FOLLOWING candles
// that stayed inside (1 = stack candle 2). The §14 tiny-stop exception is NOT
// built.
func TestISBStackingArithmetic(t *testing.T) {
	if got := ISBStackAdvice(1); got != "hold" {
		t.Fatalf("stack candle 2 = %q, want hold", got)
	}
	if got := ISBStackAdvice(2); got != "hold" {
		t.Fatalf("stack candle 3 = %q, want hold", got)
	}
	if got := ISBStackAdvice(3); got != "cancel" {
		t.Fatalf("stack candle 4 = %q, want cancel", got)
	}
}

// TestISBStackTickHoldsTwoCancelsAtFour — the arm counter from the quotes:
// keep through candles 2 and 3, cancel at candle 4 [D5.1 p2 @ 04:00–04:08].
func TestISBStackTickHoldsTwoCancelsAtFour(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	first := market.Kline{Open: 100, Close: 105, High: 106, Low: 99}
	inside := market.Kline{Open: 101, Close: 104, High: 105.5, Low: 99.5}
	arm := &ISBArm{FirstBar: first}
	for n := 1; n <= 4; n++ {
		got := ISBStackTick(arm, inside, cfg)
		switch n {
		case 1, 2:
			if len(got) != 0 {
				t.Fatalf("stack candle %d = %+v, want hold (no intents)", n+1, got)
			}
		case 3:
			if len(got) != 1 || got[0].Action != CancelArm {
				t.Fatalf("stack candle 4 = %+v, want cancel [D5.1 p2 @ 04:00–04:08]", got)
			}
		}
	}
}

// TestISBStackTickContainmentIsAgainstI1NotTheMother — the verified wording:
// "cái BODY của cây nến đó nó vẫn nằm bên trong CÂY NẾN INSABA ĐẦU TIÊN"
// [D4.2 p2 @ 08:21–09:01]. A candle whose body is inside the MOTHER's full
// range but OUTSIDE I1 cancels AT ONCE.
func TestISBStackTickContainmentIsAgainstI1NotTheMother(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	// mother is WIDE (high 120 / low 80); I1 is much narrower.
	first := market.Kline{Open: 100, Close: 105, High: 106, Low: 99}
	// body 107..109: inside the mother's full range (80..120) but above I1.
	leavesI1 := market.Kline{Open: 107, Close: 109, High: 109.5, Low: 106.5}
	arm := &ISBArm{FirstBar: first}
	got := ISBStackTick(arm, leavesI1, cfg)
	if len(got) != 1 || got[0].Action != CancelArm {
		t.Fatalf("body outside I1 = %+v, want an immediate cancel [D4.2 p2 @ 08:21–09:01]", got)
	}
	if arm.Inside != -1 {
		t.Fatalf("inside = %d, want the arm over", arm.Inside)
	}
	// the same candle against a WIDER I1 is a hold, not a cancel: the
	// containment is I1's own range, nothing else.
	wideFirst := market.Kline{Open: 100, Close: 105, High: 120, Low: 80}
	arm2 := &ISBArm{FirstBar: wideFirst}
	if got := ISBStackTick(arm2, leavesI1, cfg); len(got) != 0 {
		t.Fatalf("inside a wide I1 = %+v, want hold", got)
	}
}

// TestISBOnRecordedTape — canon 53: every body-inside-bar on the recorded
// 2026-09-15 RTH day produces orders whose stop verdict matches the knobs,
// and the tape contains a sane number of ISBs.
func TestISBOnRecordedTape(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	bars := loadFixture(t, "mnq_1m_2026-09-15_rth", "1m")
	isbs := 0
	for i := 1; i < len(bars); i++ {
		if !IsISB(bars[i-1], bars[i]) {
			continue
		}
		isbs++
		long, short := ISBOrders(bars[i], cfg)
		if long.Stop >= long.Price || short.Stop <= short.Price {
			t.Fatalf("bar %d: inverted ISB orders %+v %+v", i, long, short)
		}
	}
	if isbs < 20 {
		t.Fatalf("recorded day has %d ISBs — tape too thin", isbs)
	}
	t.Logf("recorded day: %d body-ISBs on %d bars", isbs, len(bars))
}
