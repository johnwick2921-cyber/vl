package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestSwingApproachTieIsNoApproach — a prev bar closing EXACTLY on the line
// is a tie: no approach yet, wait for a bar that closes off the level (CTO
// 2026-10-03, mirror of touch.go's approachSide rule).
func TestSwingApproachTieIsNoApproach(t *testing.T) {
	line := 10168.16
	if _, ok := swingApproach(line, line); ok {
		t.Fatal("prev close exactly on the line must be a tie (no approach)")
	}
	if side, ok := swingApproach(line-1, line); !ok || side != SideShort {
		t.Fatalf("below the line: side=%q ok=%v, want short", side, ok)
	}
	if side, ok := swingApproach(line+1, line); !ok || side != SideLong {
		t.Fatalf("above the line: side=%q ok=%v, want long", side, ok)
	}
}

// TestSwing4hTieTouchProducesNothing — tape-level: a bar closing exactly on
// the line arms a through (cancel + leeway); after the leeway expires, a
// touch whose PREVIOUS bar closed exactly on the line is a tie — no intent.
func TestSwing4hTieTouchProducesNothing(t *testing.T) {
	cfg := DefaultSwingCfg()
	// the line over the three closed buckets is deterministic — compute it
	loc := time.FixedZone("CT", -5*3600)
	closed := []market.Kline{
		mk5m(t, 14, 17, 5, 9999, 10000, 9998, 10000),
		mk5m(t, 14, 21, 5, 10999, 11000, 10998, 11000),
		mk5m(t, 15, 1, 5, 11999, 12000, 11998, 12000),
	}
	now := closed[len(closed)-1].OpenTime + 60_000
	line, _, ok := swingLine(closed, now, cfg, loc)
	if !ok {
		t.Fatal("line setup failed")
	}
	step := int64(5 * 60_000)
	mk := func(ot int64, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: ot, Open: o, High: h, Low: l, Close: c, CloseTime: ot + 4*60_000}
	}
	a := mk(now, line-20, line+10, line-20, line)            // closes ON the line → through → cancel, leeway 2
	c1 := mk(now+step, line+12, line+14, line+11, line+13)   // leeway 1; body ABOVE a's full range → not an ISB of A (RULES-FIX-v3: ISB = body inside the FULL range)
	c2 := mk(now+2*step, line+2, line+2, line, line)         // leeway 0; closes ON the line again
	touch := mk(now+3*step, line-1, line+5, line-20, line-1) // touch whose prev closed exactly on the line
	bars := append(closed, a, c1, c2, touch)
	s := &SwingState{}
	out := SwingTick(s, bars, cfg, touch.OpenTime+60_000)
	// nothing: bar A's through-close has no resting order (no empty-ArmID
	// cancel), and the tie touch produced nothing.
	if len(out) != 0 {
		t.Fatalf("tie touch must produce nothing: got %+v", out)
	}
}
