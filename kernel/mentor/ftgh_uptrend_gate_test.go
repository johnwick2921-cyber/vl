package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// TestBoxReturnGatedAgainst5mTrend pins D4.3-04 — no FTGH short in a 5m
// uptrend, no FTGL long in a 5m downtrend: "từ trái qua phải TREND ĐANG TĂNG.
// [Vẽ] FAILURE TO GO HIGHER SAO ĐÁNH?" [D4.3 @08:53–09:06]. The box's CURRENT
// role (D14 flip) sets the side. Mutant: disable the trend gate → the uptrend
// case no longer refuses box_against_5m_trend → RED.
func TestBoxReturnGatedAgainst5mTrend(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.RoomMultiple = 0.05
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	// FTGH ceiling [106, 104]: extreme high 106 @2 (confirmed by bar 3), the
	// later lower high 105 @6 is the partner (confirmed by bar 7). bar 8
	// closes below the box (approach side); bar 9 is the reject return — it
	// touches the top 106 and closes back below the bottom 104 → SHORT entry.
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 102, 99, 101),
		mk(2, 101, 106, 100, 104),    // swing high @2 (106) — the extreme
		mk(3, 103, 104, 102, 103),    // confirms bar 2 (104 < 106)
		mk(4, 101, 102, 99.5, 100.5), // swing low @4 (99.5) — the short target level
		mk(5, 103, 104, 102, 103),
		mk(6, 104, 105, 103, 104),   // swing high @6 (105) — the lower-high partner
		mk(7, 104, 104.5, 103, 104), // confirms bar 6 (104.5 < 105)
		mk(8, 103.5, 104.5, 103, 103.5),
		mk(9, 103.5, 106, 103, 103.8), // the reject return (touch 106, close < 104)
	}
	now := bars[9].OpenTime + 59_999

	// Non-vacuous control: the tape really builds the FTGH and the last bar is
	// a reject return that would trade SHORT.
	boxes := BoxesBuild(bars, cfg.Box, time.UnixMilli(now))
	if len(boxes) != 1 || boxes[0].Kind != FTGH || boxes[0].Top != 106 || boxes[0].Bottom != 104 {
		t.Fatalf("fixture: want one FTGH [106,104], got %+v", boxes)
	}
	var returns []BoxReturn
	for _, b := range boxes {
		returns = append(returns, BoxReturnBarsFrom(bars, b, b.FormedAt+1, DefaultBoxCfg())...)
	}
	if len(returns) != 1 || !BoxReturnReject(boxes[0], bars[returns[0].RefBar]) {
		t.Fatalf("fixture: want one reject return, got %+v", returns)
	}

	seed := func(trig TriggerLine) *Evaluator {
		e := New(cfg)
		e.State.Trigger = trig
		// ORB escaped SHORT so a short box entry survives the §7 gate.
		e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideShort}
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideShort, Price: 93}}
		e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
		return e
	}

	// With the 5m trend UP, the FTGH SHORT return is refused by the trend gate.
	eUp := seed(TriggerLine{Dir: SideLong, Price: 100, LastBucket: 1 << 62})
	insUp := eUp.Tick(bars, now)
	if eUp.State.Refusals["box_against_5m_trend"] != 1 {
		t.Fatalf("uptrend: want box_against_5m_trend once, ledger=%v", eUp.State.Refusals)
	}
	for _, in := range insUp {
		if strings.HasPrefix(in.Reason, "box edge return") && in.Side == SideShort {
			t.Fatalf("uptrend: an FTGH short must not emit: %+v", in)
		}
	}

	// CONTROL (no 5m trend read): the trend gate must not fire — the same
	// reject return is left to the remaining gates (it needs no trend read).
	eNone := seed(TriggerLine{LastBucket: 1 << 62})
	eNone.Tick(bars, now)
	if eNone.State.Refusals["box_against_5m_trend"] != 0 {
		t.Fatalf("no trend read must not trip the trend gate, ledger=%v", eNone.State.Refusals)
	}
}
