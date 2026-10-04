package mentor

import (
	"strings"
	"testing"
)

// TestConflictSuppressesPHLLevelLoop — D4.2-03: the PHL/PLH level loop is wrapped
// in `if !conflict`; during a standing 15m/5m conflict a level reject that would
// otherwise emit must emit NO entry. Mutant: `if !conflict` → `if true` → RED.
func TestConflictSuppressesPHLLevelLoop(t *testing.T) {
	// Sanity: the fixture emits a PHL when there is NO conflict.
	e0, bars, now := b21PairFixture(false)
	ins0 := e0.Tick(bars, now)
	var phl int
	for _, in := range ins0 {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && strings.HasPrefix(in.Reason, "PHL/PLH") {
			phl++
		}
	}
	if phl != 1 {
		t.Fatalf("fixture must emit exactly one PHL without a conflict, got %d: %+v", phl, ins0)
	}

	// A standing conflict: 5m box short vs 15m box long. Both boxes sit ABOVE
	// the close (Low == the close 102.9, High 110), so they do not escape and
	// their inside-bans do not fire — the conflict wrap is the ONLY thing that
	// suppresses the PHL. Mutant `if true` lets the PHL emit → RED.
	e, bars, now := b21PairFixture(false)
	e.State.ISBBox = &ISBBox{High: 110, Low: 102.9, Dir: SideShort, AtTime: bars[0].OpenTime}
	e.State.ISBBox15m = &ISBBox{High: 110, Low: 102.9, Dir: SideLong, AtTime: bars[0].OpenTime}
	ins := e.Tick(bars, now)
	for _, in := range ins {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && strings.HasPrefix(in.Reason, "PHL/PLH") {
			t.Fatalf("conflict must suppress the PHL level loop, got %+v", in)
		}
	}
}
