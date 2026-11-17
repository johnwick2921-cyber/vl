package trader

import (
	"testing"

	"vl/market"
	"vl/store"
)

// EX-13 trader half: mentorClosedBarsFor (the closed-bar filter every mentor
// reader of the bar cache uses — the event pass, the 2-minute scan, the strong-
// day 5m read, the chart seed). The 11-16 boot batch ended in the 19:12 bar,
// final=true, emitted 19:12:45 — fifteen seconds before its 19:13:00 close.

const ex13T1912 = int64(1794877920000) // 2026-11-16 19:12 CT

func ex13Bar(open, emitted int64, final bool) market.Kline {
	return market.Kline{OpenTime: open, CloseTime: open + 59_999, Open: 1, High: 2, Low: 0.5, Close: 1.5, Final: final, EmittedAt: emitted}
}

func ex13Trader(guard *bool) *AutoTrader {
	sc := &store.StrategyConfig{}
	sc.RiskControl.MentorTuning = &store.MentorTuning{MentorBarCloseGuard: guard}
	return &AutoTrader{id: "ex13", config: AutoTraderConfig{ID: "ex13", StrategyConfig: sc}}
}

func TestEX13ClosedBarsDropAFinalTailEmittedBeforeItsClose(t *testing.T) {
	batch := ex13T1912 + 45_000
	bars := []market.Kline{
		ex13Bar(ex13T1912-120_000, batch, true),
		ex13Bar(ex13T1912-60_000, batch, true),
		ex13Bar(ex13T1912, batch, true), // the forming 19:12 bar labelled final
	}
	at := ex13Trader(nil)
	got := at.mentorClosedBarsFor(bars, "1m")
	if len(got) != 2 || got[len(got)-1].OpenTime != ex13T1912-60_000 {
		t.Fatalf("default: the final-labelled 19:12 tail (close 19:13:00, NT8 clock 19:12:45) must be dropped; got %d bars", len(got))
	}
	if n := at.mentorCountSnapshot()["bar_final_before_close"]; n != 1 {
		t.Fatalf("bar_final_before_close = %d, want 1", n)
	}
	// A second pass over the same cache: still dropped, counted once per bar.
	at.mentorClosedBarsFor(bars, "1m")
	if n := at.mentorCountSnapshot()["bar_final_before_close"]; n != 1 {
		t.Fatalf("bar_final_before_close = %d after a second pass, want 1 (once per bar)", n)
	}

	off := false
	if got := ex13Trader(&off).mentorClosedBarsFor(bars, "1m"); len(got) != 3 {
		t.Fatalf("knob false must revert (Final alone): got %d bars, want 3", len(got))
	}
}

func TestEX13ClosedBarsKeepTrulyClosedTails(t *testing.T) {
	at := ex13Trader(nil)
	for _, c := range []struct {
		name string
		bars []market.Kline
		want int
	}{
		{"tail final, emitted after its close (boundary finalisation)", []market.Kline{
			ex13Bar(ex13T1912-60_000, ex13T1912+100, true),
			ex13Bar(ex13T1912, ex13T1912+60_000+300, true),
		}, 2},
		{"tail final, emitted exactly at its close", []market.Kline{
			ex13Bar(ex13T1912, ex13T1912+60_000, true),
		}, 1},
		{"tail final, own stamp early but a newer NT8 clock reading in the window", []market.Kline{
			ex13Bar(ex13T1912-60_000, ex13T1912+60_000+500, true),
			ex13Bar(ex13T1912, ex13T1912+59_000, true),
		}, 2},
		{"no emission stamp anywhere (store / backfill bars): Final alone", []market.Kline{
			ex13Bar(ex13T1912-60_000, 0, true),
			ex13Bar(ex13T1912, 0, true),
		}, 2},
		{"forming tail (final=false) dropped as before", []market.Kline{
			ex13Bar(ex13T1912-60_000, ex13T1912+200, true),
			ex13Bar(ex13T1912, ex13T1912+200, false),
		}, 1},
	} {
		if got := at.mentorClosedBarsFor(c.bars, "1m"); len(got) != c.want {
			t.Errorf("%s: got %d bars, want %d", c.name, len(got), c.want)
		}
	}
	if n := at.mentorCountSnapshot()["bar_final_before_close"]; n != 0 {
		t.Errorf("bar_final_before_close = %d on truly closed tails, want 0", n)
	}
}

func TestEX13ClosedBars5mTail(t *testing.T) {
	open := ex13T1912 - 120_000 // 19:10 5m bar, closes 19:15
	b := market.Kline{OpenTime: open, CloseTime: open + 299_999, Final: true, EmittedAt: ex13T1912 + 45_000}
	if got := ex13Trader(nil).mentorClosedBarsFor([]market.Kline{b}, "5m"); len(got) != 0 {
		t.Fatalf("a final-labelled 5m tail emitted 19:12:45 (close 19:15) must be dropped")
	}
}
