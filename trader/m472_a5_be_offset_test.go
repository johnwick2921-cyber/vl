package trader

import (
	"math"
	"testing"

	"vl/store"
)

// m472 A5 — break-even "a little past" the entry.
// [Oct D4.2 @ 13:00-13:25] BREAK EVEN: entry at 10, the stop goes to 11 (one
// step past, on the profit side). Frame:
// /home/hoang/mm-course/oct/scenes/Ngày 4.2 Setup gồng lời và cách move stoploss/s_00781.jpg
// The course never says how many ticks, so be_offset_ticks defaults to 0 (OFF).

func m472Int(v int) *int { return &v }

// TestM472A5BEOffsetPrice — the pure price rule on the MNQ 0.25 grid.
func TestM472A5BEOffsetPrice(t *testing.T) {
	const tick = 0.25
	cases := []struct {
		name  string
		side  string
		entry float64
		ticks int
		want  float64
	}{
		{"0 ticks long = today", "long", 100.00, 0, 100.00},
		{"0 ticks short = today", "short", 100.00, 0, 100.00},
		{"2 ticks long", "long", 100.00, 2, 100.50},
		{"2 ticks short", "short", 100.00, 2, 99.50},
		{"off-grid long rounds up then offsets", "long", 100.10, 1, 100.50},
		{"off-grid short rounds down then offsets", "short", 100.10, 1, 99.75},
	}
	for _, c := range cases {
		be := mentorBEPastEntry(c.side, mentorBEStopPrice(c.side, c.entry, tick), tick, c.ticks)
		if math.Abs(be-c.want) > 1e-9 {
			t.Errorf("%s: got %.4f want %.4f", c.name, be, c.want)
		}
	}
}

// TestM472A5BEOffsetKnobReader — nil / out of range fail closed to OFF.
func TestM472A5BEOffsetKnobReader(t *testing.T) {
	rc := func(v *int) *store.RiskControlConfig {
		return &store.RiskControlConfig{MentorTuning: &store.MentorTuning{BEOffsetTicks: v}}
	}
	at, _ := resetTrader(t, store.StrategyConfig{})
	for _, c := range []struct {
		rc   *store.RiskControlConfig
		want int
	}{{nil, 0}, {&store.RiskControlConfig{}, 0}, {rc(nil), 0}, {rc(m472Int(2)), 2}, {rc(m472Int(-1)), 0}, {rc(m472Int(21)), 0}} {
		if got := mentorBEOffsetTicks(c.rc); got != c.want {
			t.Errorf("reader = %d, want %d", got, c.want)
		}
		if c.rc != nil {
			if got := mentorTuningResolve(at, c.rc).BEOffsetTicks; got != c.want {
				t.Errorf("resolver = %d, want %d", got, c.want)
			}
		}
	}
}

// TestM472A5BEMoveThroughProductionPath — the production BE-before-partial
// move (TestCX4 fixture: short, entry r22Entry) sends the stop 2 ticks past the
// entry (entry-0.50) with the knob at 2, and exactly the entry at 0 / absent.
func TestM472A5BEMoveThroughProductionPath(t *testing.T) {
	for _, ticks := range []*int{nil, m472Int(0), m472Int(2)} {
		rc := store.RiskControlConfig{MentorBEBeforePartial: cxBool(true), MentorTuning: &store.MentorTuning{BEOffsetTicks: ticks}}
		b := r22Fixture(t, rc)
		b.positions("short", 3)
		b.book(r22FullBook(r22Stop)...)
		b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
		b.candle(r22Minute(0), 30742.25, 30743.00, 30731.00, 30732.00)
		want := r22Entry
		if ticks != nil && *ticks == 2 {
			want = r22Entry - 0.50
		}
		got := b.waitMoves(2)
		if len(got) != 2 {
			t.Fatalf("ticks=%v: want 2 BE moves, got %+v", ticks, got)
		}
		for _, m := range got {
			if m.NewStopLoss != want {
				t.Fatalf("ticks=%v: BE stop %.2f, want %.2f (%+v)", ticks, m.NewStopLoss, want, got)
			}
		}
	}
}
