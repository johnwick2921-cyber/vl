package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// R1 [D3.2 p2 @ 06:25]: every return trades, not only the first. Each NEW
// visit — price outside the box on the approach side, then a touch — gets
// its own reference candle. The first return is the third touch overall
// [D3.2 p1 @ 04:29]. Fact 4 [D3.2 p1 @ 21:04–21:33]: the touch is checked
// FIRST — a candle that touches AND closes outside on the approach side is
// the REJECT candle, the trade reference itself, and must count.
func TestBoxReturnBarsEveryVisit(t *testing.T) {
	cfg := DefaultBoxCfg()
	b := Box{Kind: FTGH, Top: 105, Bottom: 102}
	bars := []market.Kline{
		// the formation candle (formedAt = 0); closed INSIDE, so the B10 T1
		// seeding rule (spell state from the FormedAt candle's close) keeps
		// outside=false for the walk.
		{Close: 104},
		{High: 105.1, Low: 102.5, Close: 104, CloseTime: 1}, // touch, no outside first → not a visit
		{High: 101, Low: 99, Close: 100.5, CloseTime: 2},    // close below 102 → outside
		{High: 105.0, Low: 100, Close: 101.5, CloseTime: 3}, // REJECT: touch + close below 102 → visit 1, the trade reference
		{High: 105.2, Low: 103, Close: 104, CloseTime: 4},   // touch after the reject's outside close → visit 2 (cancel)
		{High: 100, Low: 98.5, Close: 99.5, CloseTime: 5},   // outside again
		{High: 105.1, Low: 99, Close: 104.5, CloseTime: 6},  // visit 3 (touch, close inside = cancel)
	}
	rs := BoxReturnBars(bars, b, 0, cfg)
	if len(rs) != 3 {
		t.Fatalf("returns = %d, want 3 (every return trades, per visit)", len(rs))
	}
	if rs[0].N != 1 || rs[0].RefBar != 3 {
		t.Fatalf("return 1 = %+v, want N=1 RefBar=3 (the reject candle is the reference)", rs[0])
	}
	if rs[1].N != 2 || rs[1].RefBar != 4 {
		t.Fatalf("return 2 = %+v, want N=2 RefBar=4 — later returns must not be skipped [D3.2 p2 @ 06:25]", rs[1])
	}
	if rs[2].N != 3 || rs[2].RefBar != 6 {
		t.Fatalf("return 3 = %+v, want N=3 RefBar=6", rs[2])
	}
	if !BoxReturnReject(b, bars[3]) {
		t.Fatal("a touch closing below the FTGH is a REJECT (trade reference) [D3.2 p1 @ 21:04–21:33]")
	}
	if BoxReturnReject(b, bars[4]) || BoxReturnReject(b, bars[6]) {
		t.Fatal("a touch closing inside the box is a CANCEL, not a reject")
	}
}

// TestBoxFullAndIncrementalWalksAgree — B10 T1 FOLD (CTO 21:16Z): ONE
// seeding rule for both walks. For every box on the fixture tapes the full
// walk (BoxReturnBars from FormedAt) and the live walk (BoxReturnBarsFrom
// from FormedAt+1) must agree visit-for-visit. Mutant (re-implement the
// full walk with outside=false instead of delegating) → RED.
func TestBoxFullAndIncrementalWalksAgree(t *testing.T) {
	cfg := DefaultBoxCfg()
	tapes := []string{
		"mnq_1m_2026-09-13_boxframe",
		"mnq_1m_2026-09-15_rth",
		"mnq_1m_2026-08-28_rth",
	}
	boxesSeen := 0
	for _, name := range tapes {
		bars := loadFixture(t, name, "1m")
		now := time.UnixMilli(bars[len(bars)-1].OpenTime + 60_000)
		for _, b := range BoxesBuild(bars, cfg, now) {
			boxesSeen++
			full := BoxReturnBars(bars, b, b.FormedAt, cfg)
			incr := BoxReturnBarsFrom(bars, b, b.FormedAt+1, cfg)
			if len(full) != len(incr) {
				t.Fatalf("%s %s: full walk %d returns, incremental %d", name, b.Key, len(full), len(incr))
			}
			for i := range full {
				if full[i] != incr[i] {
					t.Fatalf("%s %s: return %d differs: full %+v vs incremental %+v",
						name, b.Key, i, full[i], incr[i])
				}
			}
		}
	}
	if boxesSeen == 0 {
		t.Fatal("no boxes built on any fixture tape — the pin tests nothing")
	}
}

// B3 [D3.4 p3 @ 07:38–08:22]: confluence = FTGL/FTGH entry + the 5m trigger
// agrees — NO key-level condition (10-03 ruling). LONG = FTGL (support);
// SHORT = FTGH. Feeds DS-102's exit-C / size-10.
func TestConfluenceVerdict(t *testing.T) {
	b := Box{Kind: FTGL, Top: 29020, Bottom: 29000} // support box
	buy := TriggerLine{Dir: SideLong, Price: 28990} // buy line under the entry → agrees
	sell := TriggerLine{Dir: SideShort, Price: 29100}

	if f := ConfluenceVerdict(b, SideLong, buy); !f.On || f.Side != SideLong {
		t.Fatalf("FTGL + buy trigger = %+v, want On Long", f)
	}
	if f := ConfluenceVerdict(b, SideLong, sell); f.On {
		t.Fatal("sell trigger must not confluence a long")
	}
	if f := ConfluenceVerdict(b, SideLong, TriggerLine{}); f.On {
		t.Fatal("no trigger line can never agree — fail closed")
	}
	// B3: the key levels are IRRELEVANT — none, far or inside all confluence
	// the same FTGL + buy pair.
	far := []Level{{Kind: KindKeyLevel, Price: 28950}}
	inside := []Level{{Kind: KindKeyLevel, Price: 29010}}
	_ = far
	_ = inside
	if f := ConfluenceVerdict(b, SideLong, buy); !f.On {
		t.Fatal("an FTGL + buy trigger must confluence with NO key level at all")
	}
	// SHORT mirror: FTGH + sell trigger.
	c := Box{Kind: FTGH, Top: 29100, Bottom: 29080}
	if f := ConfluenceVerdict(c, SideShort, sell); !f.On || f.Side != SideShort {
		t.Fatalf("FTGH + sell trigger = %+v, want On Short", f)
	}
	if f := ConfluenceVerdict(c, SideLong, buy); f.On {
		t.Fatal("a long against an FTGH must not confluence")
	}
	// Wrong box kind for the side.
	if f := ConfluenceVerdict(b, SideShort, sell); f.On {
		t.Fatal("SHORT needs an FTGH, not an FTGL")
	}
}
