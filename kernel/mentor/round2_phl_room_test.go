package mentor

import (
	"fmt"
	"testing"
	"time"
)

// D2-G2 (oct round 2) — October takes the Oct D2.3 PHL/PLH at 1:1..1.8R, not
// the 2R of Sept D5.3 METHOD s7 [OctD2.3-R31 @ 27:33 "tăng lên 1,2 ... 1,1
// bây giờ là dĩ vãng"; WE7 risk 15.75 / reward 18 = 1.14R and WE10 tool R:R
// 1.71 were both taken]. PHLRoomMultiple (knob phl_room_multiple) is the room
// the PHL/PLH path asks: default 1 (the 1:1 floor stays), 2 = the September
// behaviour. Each case is the mentor's own rung on the real stored bars,
// through the production rollingPHLTick at DefaultConfig (NOT the cfg
// override TestPHL1CourseWorkedExamples uses).
func TestD2G2PHLRoomOctoberOneToOne(t *testing.T) {
	cases := []struct {
		name         string
		file         string
		d            int
		side         Side
		fromH, fromM int
		rungH, rungM int
		entry        float64
	}{
		{"WE6 10-27", "phl1_mnq_1m_20261027_0730ct.csv", 27, SideLong, 9, 30, 9, 44, 29645.00},
		{"WE7 10-30", "phl1_mnq_1m_20261030_1120ct.csv", 30, SideShort, 13, 15, 13, 25, 28316.25},
		{"WE10 10-29", "phl1_mnq_1m_20261029_1800ct.csv", 29, SideShort, 19, 40, 19, 52, 28980.00},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bars := phl1Tape(t, c.file)
			from := phl1At(t, bars, 2026, time.October, c.d, c.fromH, c.fromM)
			rung := phl1At(t, bars, 2026, time.October, c.d, c.rungH, c.rungM)
			at := fmt.Sprintf("%02d:%02d", c.rungH, c.rungM)

			// October default: the rung arms.
			e := phl1Evaluator(DefaultConfig(), c.side, bars[from].OpenTime, bars[rung].Close)
			got := phl1Find(phl1RunRolling(e, bars, from, rung), at)
			if len(got.places) != 1 || !m464Approx(got.places[0].Price, c.entry) {
				t.Fatalf("%s: default config must arm the mentor's %s rung %.2f; got %+v refused=%v", c.name, at, c.entry, got.places, got.refuse)
			}

			// Knob at 2 = September: refused rolling_room, nothing armed.
			cfg := DefaultConfig()
			cfg.PHLRoomMultiple = 2
			e2 := phl1Evaluator(cfg, c.side, bars[from].OpenTime, bars[rung].Close)
			got2 := phl1Find(phl1RunRolling(e2, bars, from, rung), at)
			if len(got2.places) != 0 {
				t.Fatalf("%s: phl_room_multiple=2 must restore the 2R refusal, got %+v", c.name, got2.places)
			}
			found := false
			for _, r := range got2.refuse {
				if r == "rolling_room" {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s: phl_room_multiple=2 must name rolling_room, got %v", c.name, got2.refuse)
			}
		})
	}
}
