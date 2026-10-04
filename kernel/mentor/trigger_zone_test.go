package mentor

import (
	"testing"

	"vl/market"
)

// TestTriggerBoxZoneVerdict — B1 (10-03 ruling, D3.4 p1 @16:38–17:19): the
// no-trade zone sits between an FTGL below and the BUY trigger line above
// (mirror: an FTGH above and the SELL line below): "khỏi đánh, đợi nó thoát
// ra khỏi 2 cái". This replaces the misread "between two opposing trigger
// lines" band — there is only ONE line, moved on a reversal.
func TestTriggerBoxZoneVerdict(t *testing.T) {
	// long trigger @100, an FTGL below with top 95: the zone is [95, 100].
	buy := TriggerLine{Dir: SideLong, Price: 100}
	ftgl := Box{Kind: FTGL, Top: 95, Bottom: 90, Key: "ftgl:95:90"}
	if ok, _ := triggerBoxZoneVerdict(buy, []Box{ftgl}, 97); ok {
		t.Fatal("price between the FTGL and the buy line must be no-trade")
	}
	if ok, _ := triggerBoxZoneVerdict(buy, []Box{ftgl}, 95); ok {
		t.Fatal("price exactly on the FTGL edge of the zone is still no-trade")
	}
	if ok, _ := triggerBoxZoneVerdict(buy, []Box{ftgl}, 100); ok {
		t.Fatal("price exactly on the buy line is still no-trade")
	}
	if ok, _ := triggerBoxZoneVerdict(buy, []Box{ftgl}, 101); !ok {
		t.Fatal("escaped above the buy line is not in the zone")
	}
	if ok, _ := triggerBoxZoneVerdict(buy, []Box{ftgl}, 94); !ok {
		t.Fatal("escaped below the FTGL is not in the zone")
	}

	// short trigger @97, an FTGH above with bottom 100: the zone is [97, 100].
	sell := TriggerLine{Dir: SideShort, Price: 97}
	ftgh := Box{Kind: FTGH, Top: 104, Bottom: 100, Key: "ftgh:104:100"}
	if ok, _ := triggerBoxZoneVerdict(sell, []Box{ftgh}, 99); ok {
		t.Fatal("price between the sell line and the FTGH must be no-trade")
	}
	if ok, _ := triggerBoxZoneVerdict(sell, []Box{ftgh}, 100); ok {
		t.Fatal("price exactly on the FTGH edge of the zone is still no-trade")
	}
	if ok, _ := triggerBoxZoneVerdict(sell, []Box{ftgh}, 97); ok {
		t.Fatal("price exactly on the sell line is still no-trade")
	}
	if ok, _ := triggerBoxZoneVerdict(sell, []Box{ftgh}, 96); !ok {
		t.Fatal("escaped below the sell line is not in the zone")
	}
	if ok, _ := triggerBoxZoneVerdict(sell, []Box{ftgh}, 101); !ok {
		t.Fatal("escaped above the FTGH is not in the zone")
	}

	// without a box on the trigger side there is NO zone at all (the old
	// two-line band banned a region unconditionally — B1 removes that).
	if ok, _ := triggerBoxZoneVerdict(sell, nil, 99); !ok {
		t.Fatal("with no FTGH there is no zone")
	}
	if ok, _ := triggerBoxZoneVerdict(sell, []Box{ftgl}, 99); !ok {
		t.Fatal("an FTGL does not build a zone below a SELL line")
	}
}

// TestTriggerBoxZoneFilter — the Tick sweep: a place entry inside the zone is
// dropped with the named refusal; swing and zone-free entries pass.
func TestTriggerBoxZoneFilter(t *testing.T) {
	buy := TriggerLine{Dir: SideLong, Price: 100}
	ftgl := Box{Kind: FTGL, Top: 95, Bottom: 90}
	in := []Intent{
		{Action: PlaceStopEntry, Side: SideLong, Price: 97, Reason: "PHL test"},
		{Action: PlaceStopLimitEntry, Side: SideLong, Price: 96, Reason: "ISB test"},
		{Action: PlaceStopEntry, Side: SideLong, Price: 102, Reason: "PHL outside"},
		{Action: PlaceStopEntry, Side: SideLong, Price: 98, Reason: "swing §8: reject touch"},
		{Action: CancelArm, Reason: "cancel"},
	}
	out, refusals := triggerBoxZoneFilter(in, buy, []Box{ftgl})
	if len(out) != 3 {
		t.Fatalf("kept = %+v, want 3 (zone-free entry + swing + cancel)", out)
	}
	if len(refusals) != 2 {
		t.Fatalf("refusals = %v, want 2", refusals)
	}
	for _, r := range refusals {
		if r != "trigger_ftgl_buy_zone" {
			t.Fatalf("refusal = %q, want trigger_ftgl_buy_zone", r)
		}
	}
	for _, in := range out {
		if in.Action == PlaceStopEntry && in.Price == 97 {
			t.Fatal("zone entry survived")
		}
	}
}

// TestTriggerLineOneLineOnly — B1: a reversal moves the line; there is never a
// second line in the state.
func TestTriggerLineOneLineOnly(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	tl := TriggerLine{Dir: SideLong, Price: 100, LastBucket: b5(0, 0, 0, 0).OpenTime, LastBar: b5(0, 102, 101, 102)}
	got := TriggerTick(tl, []market.Kline{b5(5, 99, 97, 97)}, 5, cfg)
	if got.Dir != SideShort || got.Price != 101 {
		t.Fatalf("reversal must move the line to sell @ the broken low: %+v", got)
	}
}
