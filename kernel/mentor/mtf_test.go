package mentor

import (
	"os"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// greenISB / redISB are a candle-1 + inside candle-2 pair whose candle-1
// colour sets the ISB direction (ISBDirection reads candle 1). cur's body
// sits inside prev's full range (wicks included) so IsISB is true.
func greenISB() (prev, cur market.Kline) {
	prev = market.Kline{Open: 100, Close: 110, High: 112, Low: 98}
	cur = market.Kline{Open: 104, Close: 106, High: 108, Low: 102}
	return
}

func redISB() (prev, cur market.Kline) {
	prev = market.Kline{Open: 110, Close: 100, High: 112, Low: 98}
	cur = market.Kline{Open: 104, Close: 106, High: 108, Low: 102}
	return
}

func TestMTFAligned(t *testing.T) {
	gp, gc := greenISB()
	rp, rc := redISB()

	cases := []struct {
		name    string
		bars5m  []market.Kline
		bars15m []market.Kline
		side    Side
		want    bool
	}{
		{"5m long + 15m long + side long → aligned", []market.Kline{gp, gc}, []market.Kline{gp, gc}, SideLong, true},
		{"5m long + 15m long + side short → NOT aligned", []market.Kline{gp, gc}, []market.Kline{gp, gc}, SideShort, false},
		{"5m long + 15m short → NOT aligned", []market.Kline{gp, gc}, []market.Kline{rp, rc}, SideLong, false},
		{"5m short + 15m short + side short → aligned", []market.Kline{rp, rc}, []market.Kline{rp, rc}, SideShort, true},
		{"short 5m history → false", []market.Kline{gp}, []market.Kline{gp, gc}, SideLong, false},
		{"short 15m history → false", []market.Kline{gp, gc}, []market.Kline{gp}, SideLong, false},
		{"empty side → false", []market.Kline{gp, gc}, []market.Kline{gp, gc}, "", false},
		{"doji candle 1 (5m) → no ISB → false", []market.Kline{{Open: 100, Close: 100, High: 102, Low: 98}, gc}, []market.Kline{gp, gc}, SideLong, false},
	}
	for _, c := range cases {
		if got := MTFAligned(c.bars5m, c.bars15m, c.side); got != c.want {
			t.Errorf("%s: MTFAligned = %v, want %v", c.name, got, c.want)
		}
	}
}

// C52 W1 B9 [D4.2 p1 @12:16–12:47, @14:30–14:52, @15:16–15:36]: MTF
// confluence is the 15m/5m/entry ISB agreement ALONE — neither D4.2 part names
// the 5m trigger line. The trigger (silent, opposite, or the price under the
// line) must not change the verdict.
func TestMTFConfluence(t *testing.T) {
	gp, gc := greenISB()
	rp, rc := redISB()

	longAligned5, longAligned15 := []market.Kline{gp, gc}, []market.Kline{gp, gc}
	mixed5, mixed15 := []market.Kline{gp, gc}, []market.Kline{rp, rc}

	cases := []struct {
		name    string
		side    Side
		bars5m  []market.Kline
		bars15m []market.Kline
		want    bool
	}{
		{"15m, 5m and the entry all long → confluence", SideLong, longAligned5, longAligned15, true},
		{"aligned long but the entry is short → no", SideShort, longAligned5, longAligned15, false},
		{"15m opposite the 5m → no", SideLong, mixed5, mixed15, false},
		{"short history → no", SideLong, []market.Kline{gp}, longAligned15, false},
	}
	for _, c := range cases {
		if got := MTFConfluence(c.side, c.bars5m, c.bars15m); got != c.want {
			t.Errorf("%s: MTFConfluence = %v, want %v", c.name, got, c.want)
		}
	}
}

// mtfAlignedLongTape builds a 1m tape on 2026-09-15 CT whose latest CLOSED 15m
// pair (09:00 green, 09:15 inside) and latest CLOSED 5m pair (09:20 green,
// 09:25 inside) are both long ISBs, ending in a long 1m ISB (09:28, 09:29).
// A head bar at 08:55 with a high close puts the EMA 34 target well beyond.
func mtfAlignedLongTape() []market.Kline {
	at := func(hh, mm int, o, h, l, c float64) market.Kline {
		ot := auditMs(2026, 9, 15, hh, mm, 0)
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	var out []market.Kline
	out = append(out, at(8, 55, 120, 121.5, 97, 120)) // head: EMA target beyond; wide so no 4h/1h line moves
	// 15m A, 09:00-09:14: green, wide (low 98, high 114).
	out = append(out, at(9, 0, 100, 114, 98, 104))
	for m := 1; m <= 13; m++ {
		out = append(out, at(9, m, 104, 105, 103, 104))
	}
	out = append(out, at(9, 14, 104, 105, 103, 112))
	// 15m B, 09:15-09:19: flat inside A's range (wicks included).
	for m := 15; m <= 19; m++ {
		out = append(out, at(9, m, 105, 106, 104, 105))
	}
	// 5m P, 09:20-09:24: GREEN (o 103 -> c 107, range 102..108).
	out = append(out, at(9, 20, 103, 104, 102, 104))
	out = append(out, at(9, 21, 104, 105, 103, 105))
	out = append(out, at(9, 22, 105, 106, 104, 105))
	out = append(out, at(9, 23, 105, 108, 104, 106))
	out = append(out, at(9, 24, 106, 107, 105, 107))
	// 5m C, 09:25-09:29: inside P's range [102, 108]; ends on a long 1m ISB.
	out = append(out, at(9, 25, 104, 105, 103.5, 104.5))
	out = append(out, at(9, 26, 104.5, 105.5, 104, 105))
	out = append(out, at(9, 27, 105, 105.5, 104.5, 105))
	out = append(out, at(9, 28, 104.5, 106.2, 104.2, 105.8)) // 1m candle 1: GREEN
	out = append(out, at(9, 29, 105.2, 105.9, 104.6, 105.5)) // 1m candle 2: inside candle 1
	return out
}

// C52 W1 B9 at the PRODUCTION call site (Evaluator.Tick, the ISB emit): with
// the 15m, 5m and 1m ISBs all long the intent is mode-C confluence whatever
// the 5m trigger line says — a buy line the entry price is still under is the
// old conjunct's refusal that survives the entry filters (an OPPOSITE line
// already refuses the ISB itself, isb_trigger_side_mismatch). Pre-fix the two
// "still under the buy line" cases emitted Confluence=false.
func TestMTFConfluenceIgnoresTheTriggerAtTheEmitSite(t *testing.T) {
	bars := mtfAlignedLongTape()
	if !MTFAligned(bars5mOf(t, bars), bars15mOf(t, bars), SideLong) {
		t.Fatal("fixture: the closed 15m/5m pairs must both be long ISBs")
	}
	cases := []struct {
		name string
		trig TriggerLine
	}{
		{"buy line ABOVE the entry (price still under it)", TriggerLine{Dir: SideLong, Price: 130}},
		{"buy line far above the entry", TriggerLine{Dir: SideLong, Price: 125}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Enabled = true
			cfg.RoomMultiple = 0 // this pin tests the confluence flag, not the room rule
			e := New(cfg)
			e.State.Trigger = c.trig
			e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
			e.State.ORB = ORB{Day: dayStartCT(auditMs(2026, 9, 15, 9, 0, 0)), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
			e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(auditMs(2026, 9, 15, 9, 0, 0)).In(ctime())), Verdict: DayTrade}
			last := bars[len(bars)-1]
			ins := e.Tick(bars, last.CloseTime+1)
			for _, in := range ins {
				if in.Setup == "ISB" && (in.Action == PlaceStopLimitEntry || in.Action == PlaceStopEntry) {
					if !in.Confluence {
						t.Fatalf("aligned 15m/5m/1m long ISB must be confluence under %s: %+v", c.name, in)
					}
					return
				}
			}
			t.Fatalf("no ISB intent emitted (refusals %v): %+v", e.State.Refusals, ins)
		})
	}
}

func bars5mOf(t *testing.T, bars []market.Kline) []market.Kline {
	t.Helper()
	e := New(DefaultConfig())
	return e.closedBucketsMemo(bars, bars[len(bars)-1].CloseTime+1)
}

func bars15mOf(t *testing.T, bars []market.Kline) []market.Kline {
	t.Helper()
	e := New(DefaultConfig())
	return e.closedBucketsTFMemo(bars, 15, bars[len(bars)-1].CloseTime+1)
}

// TestMTFConfluenceWiredAtTheEmitSites pins the production call sites: the ISB
// emit and the PHL/PLH emit must stamp the intent's Confluence from
// MTFConfluence (D4.2-06 part 1). Removing either stamp breaks this pin.
func TestMTFConfluenceWiredAtTheEmitSites(t *testing.T) {
	b, err := os.ReadFile("eval.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		"confluence := MTFConfluence(side, cb5, cb15)",
		"confluence := MTFConfluence(in.Side, cb5, cb15)",
		"cb5 := e.closedBucketsMemo(bars, now)",
		"cb15 := e.closedBucketsTFMemo(bars, 15, now)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("eval.go lost the D4.2-06 wiring: missing %q", want)
		}
	}
}
