package ninjatrader

import (
	"errors"
	"testing"

	ntwire "vl/provider/ninjatrader"
)

// PARTIAL-CLOSE (2026-10-03) — the typed sender refuses BEFORE the send when
// the far side never advertised the capability (dispatch item 3).

func TestReducePositionRefusesUnsupportedBeforeSend(t *testing.T) {
	tr := &TCPTrader{}
	if err := tr.ReducePosition("long", 3, "rx-1"); !errors.Is(err, ntwire.ErrReduceUnsupported) {
		t.Fatalf("want ErrReduceUnsupported, got %v", err)
	}
}

func TestCancelBracketLegRefusesAnUnknownLeg(t *testing.T) {
	tr := &TCPTrader{}
	if err := tr.CancelBracketLeg("sig-1", "xx"); err == nil {
		t.Fatalf("an unknown leg must be refused")
	}
	if err := tr.CancelBracketLeg("sig-1", "SL"); err == nil {
		// Upper-case resolves to sl, but with no bound server the call must
		// still fail — and NOT with the unknown-leg error.
		t.Fatalf("a leg cancel without a bound server must error, got nil")
	}
}
