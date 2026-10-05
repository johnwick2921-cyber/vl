package mentor

import (
	"testing"

	"vl/market"
)

// R7 tests — 00-METHOD D5.4 (RULES-FIX-v3): the reverse ISB at EMA 9, behind
// its own knob (default OFF, L4). In an uptrend an ISB pointing SHORT at the
// EMA 9 → LONG buy stop above that ISB candle's high; the downtrend mirror;
// the stop is the other side of the ISB candle plus the buffer (method silent;
// the knob supplies it). Cancel-if-unfilled rides the evaluator's stacking.

func reverseCfg() Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ISBReverseEMA9Enabled = true
	return cfg
}

func TestReverseISBAtEMA9KnobOff(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ISBReverseEMA9Enabled = false // explicitly off (the default is ON since R-C)
	prev := market.Kline{Open: 106, Close: 100, High: 106.5, Low: 99.5}
	cur := market.Kline{Open: 102, Close: 104, High: 104.5, Low: 99.5}
	if in, ok, _ := ReverseISBAtEMA9(prev, cur, 101, SideLong, cfg); ok || in.Action != "" {
		t.Fatalf("knob off must emit nothing; got %+v ok=%v [D5.4; L4]", in, ok)
	}
}

// OWNER RULING 2026-10-04 R-C: the reverse ISB at EMA 9 is ON by default.
func TestReverseISBAtEMA9DefaultIsOn(t *testing.T) {
	if !DefaultConfig().ISBReverseEMA9Enabled {
		t.Fatal("DefaultConfig().ISBReverseEMA9Enabled must be true (owner ruling 2026-10-04 R-C)")
	}
}

func TestReverseISBAtEMA9LongAndShort(t *testing.T) {
	cfg := reverseCfg()
	// uptrend, ISB points SHORT (red candle 1), EMA 9 inside the ISB range
	redPrev := market.Kline{Open: 106, Close: 100, High: 106.5, Low: 99.5}
	cur := market.Kline{Open: 102, Close: 104, High: 104.5, Low: 99.5}
	in, ok, reason := ReverseISBAtEMA9(redPrev, cur, 101, SideLong, cfg)
	if !ok || in.Side != SideLong || in.Price != 104.5+cfg.ISBBufferPts || in.Stop != 99.5-cfg.ISBBufferPts {
		t.Fatalf("uptrend reverse: %+v ok=%v reason=%q — want long buy stop at the ISB high + buffer, stop other side minus buffer [D5.4, D1.4 p1 @ 22:26–23:26]", in, ok, reason)
	}
	// downtrend, ISB points LONG (green candle 1)
	greenPrev := market.Kline{Open: 100, Close: 106, High: 106.5, Low: 99.5}
	in, ok, _ = ReverseISBAtEMA9(greenPrev, cur, 101, SideShort, cfg)
	if !ok || in.Side != SideShort || in.Price != 99.5-cfg.ISBBufferPts || in.Stop != 104.5+cfg.ISBBufferPts {
		t.Fatalf("downtrend reverse: %+v ok=%v — want short sell stop at the ISB low − buffer [D5.4, D1.4 p1 @ 22:26–23:26]", in, ok)
	}
}

func TestReverseISBAtEMA9Refusals(t *testing.T) {
	cfg := reverseCfg()
	redPrev := market.Kline{Open: 106, Close: 100, High: 106.5, Low: 99.5}
	cur := market.Kline{Open: 102, Close: 104, High: 104.5, Low: 99.5}
	// ISB already points WITH the trend → nothing
	if _, ok, _ := ReverseISBAtEMA9(redPrev, cur, 101, SideShort, cfg); ok {
		t.Fatal("an ISB pointing with the trend is not a reverse [D5.4]")
	}
	// price never reached the EMA 9 (loose touch required)
	if _, ok, _ := ReverseISBAtEMA9(redPrev, cur, 120, SideLong, cfg); ok {
		t.Fatal("no EMA 9 touch, no reverse [D5.4]")
	}
	// not an ISB at all
	notISB := market.Kline{Open: 102, Close: 108, High: 108.5, Low: 100}
	if _, ok, _ := ReverseISBAtEMA9(redPrev, notISB, 101, SideLong, cfg); ok {
		t.Fatal("a non-ISB pair is not a reverse [D5.4]")
	}
}
