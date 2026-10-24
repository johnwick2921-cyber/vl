package mentor

import (
	"testing"

	"vl/market"
)

// TestBoxEntryCarriesItsDrawnFrame pins m417's box half on the production call
// site (boxEntryIntent): the BOX intent carries the box's DRAWN frame (Box.TF
// from m410) so the X8 trap-dodge and the runner trail read that frame
// [D4.1 "trade khung nào vẽ khung đó" @22:57–23:21]. A box drawn on the 2m
// (after 09:00 CT) → 2; a base-frame box (drawn on the 1m during 08:30–09:00,
// or the knob OFF) → 1; a 5mha box → 5.
// MUTANT: drop the ExecTFMin stamp in boxEntryIntent → RED.
func TestBoxEntryCarriesItsDrawnFrame(t *testing.T) {
	cfgBase := DefaultConfig()
	cfgBase.LocTriggerFilter = false
	cfgBase.PingPongCandleMaxPts = 0

	ref := market.Kline{OpenTime: 12_345_000, CloseTime: 12_345_000 + 59_999, High: 99, Low: 96, Close: 98.5}
	lvls := []Level{{Key: "old_extreme:110", Kind: KindOldExtreme, Price: 110}}

	cases := []struct {
		name string
		b    Box
		cfg  Config
		want int
	}{
		{"2m box drawn after 09:00 → 2", Box{Kind: FTGL, Top: 98, Bottom: 95, Key: "ftgl:98.00:95.00", TF: "2m"}, cfgBase, 2},
		{"base 1m box drawn 08:30–09:00 → 1", Box{Kind: FTGL, Top: 98, Bottom: 95, Key: "ftgl:98.00:95.00"}, cfgBase, 1},
		{"5mha box → 5", Box{Kind: FTGL, Top: 98, Bottom: 95, Key: "ftgl:98.00:95.00", TF: "5mha"}, cfgBase, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := boxEntryIntent(ref, tc.b, []Box{tc.b}, lvls, TriggerLine{}, nil, tc.cfg)
			if len(out) != 1 {
				t.Fatalf("boxEntryIntent = %d intents, want 1", len(out))
			}
			if out[0].ExecTFMin != tc.want {
				t.Fatalf("ExecTFMin=%d want %d", out[0].ExecTFMin, tc.want)
			}
		})
	}
}
