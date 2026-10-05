package ninjatrader

import (
	"os"
	"strings"
	"testing"
)

// ── REVIEW-SPLIT-2 Go-side code pins ───────────────────────────────────────
// The AddOn is pinned in package trader (split_cs_pin_test.go). These pin the
// Go half of the same wire contract: the position_close receipt identity and
// the leg1_tp tick-rounding.

// TestSplitGoPositionCloseLegInIdentity pins P1-2: the close receipt identity
// includes p.Leg, so two same-ms stop exits (leg 2 then leg 1) hash to
// different identities and both apply. Mutant: drop p.Leg from the slice → RED.
func TestSplitGoPositionCloseLegInIdentity(t *testing.T) {
	b, err := os.ReadFile("close_sync.go")
	if err != nil {
		t.Fatalf("cannot read close_sync.go: %v", err)
	}
	src := string(b)
	if !strings.Contains(src, `identity, _ := json.Marshal([]any{p.Account, symbol, side, p.SignalID, leg, p.Leg, p.Seq, qty, exitMs})`) {
		t.Error("close identity lost p.Leg — two same-ms leg exits would collide")
	}
}

// TestSplitGoPositionClosePayloadLeg pins P1-2: the wire payload struct carries
// the leg field the C# AddOn sends. Mutant: drop Leg from PositionClosePayload → RED.
func TestSplitGoPositionClosePayloadLeg(t *testing.T) {
	b, err := os.ReadFile("../../provider/ninjatrader/tcp_framing.go")
	if err != nil {
		t.Fatalf("cannot read tcp_framing.go: %v", err)
	}
	src := string(b)
	if !strings.Contains(src, `Leg int `+"`"+`json:"leg,omitempty"`+"`") {
		t.Error("PositionClosePayload lost its leg field")
	}
}

// TestSplitGoLeg1TPTickRounded pins P2-1: leg1_tp is rounded to the tick grid
// (nearest, like a target) before it rides the frame. Mutant: send raw leg1TP → RED.
func TestSplitGoLeg1TPTickRounded(t *testing.T) {
	b, err := os.ReadFile("tcp_trader.go")
	if err != nil {
		t.Fatalf("cannot read tcp_trader.go: %v", err)
	}
	src := string(b)
	if !strings.Contains(src, "leg1TP = RoundToTick(leg1TP, tick)") {
		t.Error("leg1_tp is no longer tick-rounded on the wire")
	}
}
