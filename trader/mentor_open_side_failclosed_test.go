package trader

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
)

// look-twice D (spec 2026-11-24 section 3 D). The production open-side seam
// answered "" when the position read FAILED, and the never-add / no-flip gate
// reads "" as "nothing is open": an unreadable account lifted the gate. The
// refusal is driven through the production wiring and the placement call site.
//
// MUTANT: return "" on the read error again → the entry is placed → RED.
func TestMentorOpenSideReadErrorRefusesTheEntry(t *testing.T) {
	for _, c := range []struct {
		name      string
		fake      fakeMentorPositionReader
		placed    int
		counter   string
		setup     string
		side      mentor.Side
		openSweep bool
	}{
		{"read error, ISB long", fakeMentorPositionReader{err: errors.New("NT8 account positions unknown")}, 0, "add_open_side_unresolved", "ISB", mentor.SideLong, true},
		{"read error, short box", fakeMentorPositionReader{err: errors.New("NT8 account positions unknown")}, 0, "add_open_side_unresolved", "BOX", mentor.SideShort, true},
		{"held, side unreadable", fakeMentorPositionReader{positions: []map[string]interface{}{{"symbol": "MNQ", "positionAmt": 2.0}}}, 0, "add_open_side_unresolved", "ISB", mentor.SideLong, true},
		{"held short after an unreadable row", fakeMentorPositionReader{positions: []map[string]interface{}{{"side": "?", "positionAmt": 2.0}, {"side": "SHORT", "positionAmt": -1.0}}}, 0, "mentor_no_flip", "ISB", mentor.SideLong, true},
		{"no size on an unreadable row is not a position", fakeMentorPositionReader{positions: []map[string]interface{}{{"symbol": "MNQ", "side": ""}}}, 1, "", "ISB", mentor.SideLong, false},
		{"read OK and flat still places", fakeMentorPositionReader{}, 1, "", "ISB", mentor.SideLong, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
			at.trader = &c.fake
			at.mentorWireProductionSeams()
			t.Cleanup(func() { mentorPlaceRecorderForTest = nil; setMentorNowSource(nil) })
			production := at.mentorOpenSideSource
			wireMentorPlacementSeams(t, at)
			at.mentorOpenSideSource = production
			ct := kernel.CTLocation()
			setMentorNowSource(func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, ct) })
			placed := 0
			mentorPlaceRecorderForTest = func(mentor.Intent, int) { placed++ }

			in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: c.side,
				Price: 21000, Stop: 20988, Target: 21024, Setup: c.setup, StopPts: 12}
			if c.side == mentor.SideShort {
				in.Stop, in.Target = 21012, 20976
			}
			at.mentorPlaceIntent(in, mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}, 1000, 1100)
			if placed != c.placed {
				t.Fatalf("placed = %d, want %d (counters %v)", placed, c.placed, at.mentorCountSnapshot())
			}
			if c.counter != "" {
				if got := at.mentorCountSnapshot()[c.counter]; got != 1 {
					t.Fatalf("%s = %d, want 1 (counters %v)", c.counter, got, at.mentorCountSnapshot())
				}
			}
			if c.counter == "add_open_side_unresolved" {
				if at.mentorCountSnapshot()["mentor_no_flip"] != 0 {
					t.Fatal("an unreadable account is not a flip: it must be named for what it is")
				}
				if refuse, why := at.mentorAddGate(in); !refuse || !strings.Contains(why, "could not be read") {
					t.Fatalf("the refusal must say the read failed: refuse=%v why=%q", refuse, why)
				}
			}
			// The day-stop sweep's read is UNCHANGED: it already counted an
			// unreadable list as open through openPositionCount.
			if got := at.mentorOpenPositionActive(); got != c.openSweep {
				t.Fatalf("mentorOpenPositionActive = %v, want %v", got, c.openSweep)
			}
		})
	}
}
