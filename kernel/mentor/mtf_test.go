package mentor

import (
	"os"
	"strings"
	"testing"

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

func TestMTFConfluence(t *testing.T) {
	gp, gc := greenISB()
	rp, rc := redISB()

	longAligned5, longAligned15 := []market.Kline{gp, gc}, []market.Kline{gp, gc}
	mixed5, mixed15 := []market.Kline{gp, gc}, []market.Kline{rp, rc}

	buyTrig := TriggerLine{Dir: SideLong, Price: 99}
	sellTrig := TriggerLine{Dir: SideShort, Price: 111}
	silent := TriggerLine{}

	cases := []struct {
		name    string
		side    Side
		trig    TriggerLine
		price   float64
		bars5m  []market.Kline
		bars15m []market.Kline
		want    bool
	}{
		{"aligned + buy trigger agrees (long above line) → true", SideLong, buyTrig, 103, longAligned5, longAligned15, true},
		{"aligned + sell trigger opposite → false", SideLong, sellTrig, 103, longAligned5, longAligned15, false},
		{"aligned + silent trigger → false", SideLong, silent, 103, longAligned5, longAligned15, false},
		{"NOT aligned (15m short) + trigger agrees → false", SideLong, buyTrig, 103, mixed5, mixed15, false},
		{"aligned but price below the buy line → false", SideLong, buyTrig, 98, longAligned5, longAligned15, false},
	}
	for _, c := range cases {
		if got := MTFConfluence(c.side, c.trig, c.price, c.bars5m, c.bars15m); got != c.want {
			t.Errorf("%s: MTFConfluence = %v, want %v", c.name, got, c.want)
		}
	}
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
		"chosen.Confluence = MTFConfluence(side, e.State.Trigger, chosen.Price, cb5, cb15)",
		"in.Confluence = MTFConfluence(in.Side, e.State.Trigger, in.Price, cb5, cb15)",
		"cb5 := closedBuckets(bars, now, e.Cfg)",
		"cb15 := closedBucketsTF(bars, 15, now)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("eval.go lost the D4.2-06 wiring: missing %q", want)
		}
	}
}
