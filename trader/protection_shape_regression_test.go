package trader

import (
	"testing"
	nt "vl/provider/ninjatrader"
)

// F12 (port of #117 52134afc) — A "-sl" NAME IS INTENT, NOT COVERAGE.
// A bracket named like our stop suffix is protective only when its shape (a
// stop order acting against the position) agrees; a named order whose type or
// action contradicts the position side is NOT protection, and a named order
// with unreadable shape is UNKNOWN, never coverage.
func TestProtectionRequiresOrderShapeDespiteStopName(t *testing.T) {
	for _, tc := range []struct {
		name, action, typ string
		want              protectionAction
	}{
		{"wrong side", "BuyToCover", "StopMarket", protectionPlace},
		{"limit named stop", "Sell", "Limit", protectionPlace},
		{"unknown action", "", "StopMarket", protectionUnknown},
		{"unknown type", "Sell", "", protectionUnknown},
		{"correct closing stop", "Sell", "StopMarket", protectionOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := nt.NT8Order{Symbol: "MNQ", Name: "fixture-sl", Action: tc.action, Type: tc.typ, State: "Accepted", Quantity: 1, StopPrice: 29500}
			v := adjudicateProtection("MNQ", "LONG", 1, []nt.NT8Order{o}, true, nil, 29500)
			if v.Action != tc.want {
				t.Fatalf("action=%s want=%s: %s", v.Action, tc.want, v.Why)
			}
		})
	}
}
