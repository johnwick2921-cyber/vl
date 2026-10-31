package trader

import (
	"testing"
	"time"

	"vl/store"
)

// m461 Custom style THROUGH THE WIRE (adapted from the lock/money reviewer's
// throwaway tests): production path pepEventPassAt -> admission -> the one
// market order -> the durable day row, with the stored block resolved by
// store.ResolvePep.

func pcF(f float64) *float64 { return &f }
func pcI(i int) *int         { return &i }

// Custom breakout, 4 contracts, SL 0.5 x box, TP 2 x box on the 40-pt box:
// ONE market order with the native bracket (live 25041: SL 25021, TP 25121),
// quantity 4, nothing else on the wire, and a day row Counted=1 Style=Custom.
// Mutant: ConfigOf ignores Custom.Entry (IsBreakout false) -> no entry -> RED.
func TestPepCustomBreakoutIsOneBracketedOrder(t *testing.T) {
	c := pepCfgOf("Custom", 4)
	c.StyleCustom = &store.PepStyleCustomConfig{EntryType: "breakout", BoxMinutes: pcI(5), SLFraction: pcF(0.5), TPFraction: pcF(2)}
	w := newPepWire(t, c)
	pepFeed(t, pepBarsA(t, pepDay, "08:38", pepBreakUp))
	pepLive(t, 25041)
	if !w.at.pepEventPassAt(pepNow(t)) {
		t.Fatal("no evaluation")
	}
	p := waitSignal(t, w)
	if p.Side != "long" || p.Quantity != 4 || p.StopLoss != 25021 || p.TakeProfit != 25121 {
		t.Fatalf("%+v", p)
	}
	w.noSignal(t, "after the one Custom entry")
	ds, err := w.st.PepDay().Day(w.at.id, pepDay)
	if err != nil || ds.Counted != 1 || ds.Last == nil || ds.Last.Style != "Custom" {
		t.Fatalf("day row %+v err %v", ds, err)
	}
}

// Custom ema_cross (the seeded defaults) and style E on the same tape place the
// SAME order. Mutant: seed Custom's SL fraction differently -> bracket differs.
func TestPepCustomEMACrossEqualsStyleE(t *testing.T) {
	var got [2]struct {
		side       string
		qty        int
		sl, tp     float64
		sentAtStep int
	}
	for i, style := range []string{"Custom", "E"} {
		w := newPepWire(t, pepCfgOf(style, 6))
		over := map[string]pepBar{"08:30": {25010, 25040, 25000, 25020}, "08:31": {25020, 25030, 25010, 25025}}
		bars := pepBarsA(t, pepDay, "09:30", over)
		loc := ctTimeAt(t, pepDay, "07:00").Location()
		for k := range bars {
			hh := time.UnixMilli(bars[k].OpenTime).In(loc).Format("15:04")
			switch {
			case hh == "08:32" || hh == "08:33" || hh == "08:34":
				bars[k].Open, bars[k].High, bars[k].Low, bars[k].Close = 25020, 25021, 25019, 25020
			case hh >= "08:35":
				bars[k].Open, bars[k].High, bars[k].Low, bars[k].Close = 25100, 25101, 25099, 25100
			}
		}
		pepFeed(t, bars)
		pepLive(t, 25100)
		sent := false
		for m := 0; m < 50 && !sent; m++ {
			w.at.pepEventPassAt(ctTimeAt(t, pepDay, "08:36").Add(time.Duration(m)*time.Minute + 2*time.Second))
			select {
			case p := <-w.sigs:
				got[i].side, got[i].qty, got[i].sl, got[i].tp, got[i].sentAtStep = p.Side, p.Quantity, p.StopLoss, p.TakeProfit, m
				sent = true
			case <-time.After(100 * time.Millisecond):
			}
		}
		if !sent {
			t.Fatalf("%s: no entry", style)
		}
	}
	if got[0] != got[1] || got[0].qty != 6 {
		t.Fatalf("Custom %+v != E %+v", got[0], got[1])
	}
}

// Custom's max-box cap refuses a 40-pt box (pep_box_too_wide); A ignores the
// same stored cap. Custom's stored flat time 12:30 is FlatMin 750.
// Mutant: lock block applied to Custom (or the cap read under A) -> RED.
func TestPepCustomMaxBoxAndFlatTimeAreReadAIgnoresThem(t *testing.T) {
	cc := pepCfgOf("Custom", 4)
	cc.StyleCustom = &store.PepStyleCustomConfig{EntryType: "breakout", BoxMinutes: pcI(5)}
	cc.MaxBoxPoints = pcF(30)
	w := newPepWire(t, cc)
	pepFeed(t, pepBarsA(t, pepDay, "08:38", pepBreakUp))
	pepLive(t, 25041)
	w.at.pepEventPassAt(pepNow(t))
	w.noSignal(t, "Custom max box 30 vs a 40-pt box")
	found := false
	for _, r := range w.at.PepTruthSnapshot(pepNow(t)).Refusals {
		if r.Code == "pep_box_too_wide" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want pep_box_too_wide, got %+v", w.at.PepTruthSnapshot(pepNow(t)).Refusals)
	}

	ca := pepCfgOf("A", 4)
	ca.MaxBoxPoints = pcF(30)
	wa := newPepWire(t, ca)
	pepFeed(t, pepBarsA(t, pepDay, "08:38", pepBreakUp))
	pepLive(t, 25041)
	wa.at.pepEventPassAt(pepNow(t))
	waitSignal(t, wa)

	cf := pepCfgOf("Custom", 4)
	cf.FlatTimeCT = "12:30"
	wf := newPepWire(t, cf)
	if got := wf.at.pepConfigAt(pepNow(t)); got.FlatMin != 750 {
		t.Fatalf("Custom flat 12:30 -> FlatMin %d, want 750", got.FlatMin)
	}
}

// Reviewer P2: a stored tick of 1.0 under style E must not change the PDF
// stop (40.25 box -> SL 20.25 on the 0.25 tick, not 21); under Custom it is read.
// Also the stored signal age is ignored under A / E.
// Mutant: remove pep_tick_size from PepLockedLeaves / the TickSize reset -> RED.
func TestPepStoredTickAndSignalAgeIgnoredUnderAEReadUnderCustom(t *testing.T) {
	for _, style := range []string{"A", "E"} {
		c := pepCfgOf(style, 6)
		c.TickSize = pcF(1.0)
		c.SignalMaxAgeMin = pcI(9)
		w := newPepWire(t, c)
		got := w.at.pepConfigAt(pepNow(t))
		if got.TickSize != 0.25 || got.SignalMaxAgeMin != 2 {
			t.Fatalf("style %s: tick %v age %d, want 0.25 / 2", style, got.TickSize, got.SignalMaxAgeMin)
		}
	}
	cc := pepCfgOf("Custom", 6)
	cc.TickSize = pcF(1.0)
	cc.SignalMaxAgeMin = pcI(9)
	got := newPepWire(t, cc).at.pepConfigAt(pepNow(t))
	if got.TickSize != 1.0 || got.SignalMaxAgeMin != 9 {
		t.Fatalf("Custom must read them: tick %v age %d", got.TickSize, got.SignalMaxAgeMin)
	}
}

// m461: under style A every stored number except contracts is ignored. A
// strategy that stored a different box start, box length, SL/TP fraction,
// rounding, flat time, max box and trades/day still sends the PDF bracket
// (full box both ways, 6 contracts) and resolves flat 15:00 / 1 trade / no
// max box. Folded from the m461 money reviewer's probe.
func TestPepLockedAIgnoresEveryStoredNumberButContracts(t *testing.T) {
	c := pepCfgOf("A", 6)
	c.FlatTimeCT = "10:00"
	c.MaxBoxPoints = fp(5)
	c.MaxTradesPerDay = ip(5)
	c.BoxStartCT = "08:00"
	c.SLRounding = "nearest"
	c.StyleA = &store.PepStyleAConfig{BoxMinutes: ip(1), SLFraction: fp(0.1), TPFraction: fp(3)}
	c.StyleCustom = &store.PepStyleCustomConfig{SLFraction: fp(0.3)}
	w := newPepWire(t, c)
	pepFeed(t, pepBarsA(t, pepDay, "08:38", pepBreakUp))
	pepLive(t, 25041)
	if !w.at.pepEventPassAt(pepNow(t)) {
		t.Fatal("no eval")
	}
	p := waitSignal(t, w)
	if p.Quantity != 6 || p.StopLoss != 25001 || p.TakeProfit != 25081 {
		t.Fatalf("A must send the PDF bracket qty 6 SL 25001 TP 25081, got %+v", p)
	}
	if got := w.at.pepConfigAt(pepNow(t)); got.FlatMin != 900 || got.MaxTradesPerDay != 1 || got.MaxBoxPoints != 0 {
		t.Fatalf("A must resolve flat 15:00 / 1 trade / no max box, got %+v", got)
	}
}
