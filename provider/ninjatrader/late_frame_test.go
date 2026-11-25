package ninjatrader

import (
	"testing"
	"time"
)

// Each case is one sequence of positions frames and entry fills; want is Unproven
// for MNQ SHORT after the last step. MUTANTS named per case.
func TestPositionRegrowIsUnprovenUntilExplainedOrRepeated(t *testing.T) {
	type step struct {
		after time.Duration // since the step before
		frame []OpenPosition
		entry string // an entry fill of this signal (frame is nil then)
	}
	short := func(q int) OpenPosition {
		return OpenPosition{Symbol: "MNQ", Side: "short", Quantity: q, AvgPrice: 100}
	}
	long := func(q int) OpenPosition {
		return OpenPosition{Symbol: "MNQ", Side: "long", Quantity: q, AvgPrice: 100}
	}
	f := func(after time.Duration, ps ...OpenPosition) step {
		return step{after: after, frame: append([]OpenPosition{}, ps...)}
	}
	fill := func(after time.Duration, sig string) step { return step{after: after, entry: sig} }
	const ms, sec = time.Millisecond, time.Second
	for name, c := range map[string]struct {
		steps []step
		want  bool
	}{
		"the first frame of an account": {[]step{f(0, short(1))}, false},
		"unchanged":                     {[]step{f(0, short(1)), f(30*sec, short(1))}, false},
		"smaller":                       {[]step{f(0, short(3)), f(sec, short(1))}, false},
		"flat":                          {[]step{f(0, short(3)), f(sec)}, false},
		"flat, then late":               {[]step{f(0, short(1)), f(sec), f(ms, short(1))}, true},
		"smaller, then late":            {[]step{f(0, short(3)), f(sec, short(1)), f(ms, short(3))}, true},
		"late, then smaller again":      {[]step{f(0, short(3)), f(sec, short(1)), f(ms, short(3)), f(30*sec, short(1))}, false},
		"flat and two late ones, the second smaller": {[]step{f(0, short(3)), f(sec), f(ms, short(3)), f(ms, short(1))}, true},
		"repeated too soon":                          {[]step{f(0, short(1)), f(sec), f(ms, short(1)), f(positionRegrowProof-ms, short(1))}, true},
		"repeated":                                   {[]step{f(0, short(1)), f(sec), f(ms, short(1)), f(positionRegrowProof, short(1))}, false},
		"repeated, flat, late again":                 {[]step{f(0), f(sec, short(1)), f(30*sec, short(1)), f(sec), f(ms, short(1))}, true},
		"a size that changes is not a repeat":        {[]step{f(0), f(sec, short(1)), f(30*sec, short(2))}, true},
		"a repeat counts from the changed size":      {[]step{f(0), f(sec, short(1)), f(30*sec, short(2)), f(sec, short(2))}, true},
		"a hand add":                                 {[]step{f(0, short(1)), f(sec, short(2))}, true},
		"a hand add, repeated":                       {[]step{f(0, short(1)), f(sec, short(2)), f(30*sec, short(2))}, false},
		"an entry after the flat frame":              {[]step{f(0), fill(sec, "a"), f(ms, short(1))}, false},
		"an entry after its own frame":               {[]step{f(0), f(sec, short(1)), fill(ms, "a")}, false},
		"NT8's Close is not an entry":                {[]step{f(0, short(1)), f(sec), fill(ms, "Close"), f(ms, short(1))}, true},
		"an entry before the flat frame":             {[]step{f(0, short(1)), fill(sec, "a"), f(ms), f(ms, short(1))}, true},
		"the entry of a closed trade":                {[]step{f(0), fill(sec, "a"), f(ms, short(1)), f(60 * sec), f(ms, short(1))}, true},
		"its frame beat the entry, then it closed":   {[]step{f(0), f(sec, short(3)), fill(ms, "a"), f(60*sec, short(1)), f(ms, short(3))}, true},
		"the other side was entered":                 {[]step{f(0, short(1)), f(sec), fill(ms, "a"), f(ms, long(1)), f(ms, short(1))}, true},
		"the other side says nothing of this one":    {[]step{f(0, short(1), long(1)), f(sec, short(1)), f(ms, short(1), long(1))}, false},
		"two months in the other order":              {[]step{f(0, short(2), short(1)), f(sec, short(1), short(2))}, false},
		"two months of one side are one size":        {[]step{f(0, short(1), short(2)), f(sec, short(2), short(1))}, false},
		"one of two months came back":                {[]step{f(0, short(1), short(2)), f(sec, short(1)), f(ms, short(1), short(2))}, true},
	} {
		s := NewTCPServer(nil)
		now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
		s.SetReceiptClockForTest(func() time.Time { return now })
		for _, st := range c.steps {
			now = now.Add(st.after)
			if st.entry != "" {
				s.NoteEntryExecution("MNQ", "Sim101", st.entry, 1)
				continue
			}
			s.SeedPositionsForTestAt("Sim101", st.frame, now)
		}
		if got := s.ProtectionEvidenceFor("Sim101", "MNQ").Unproven("mnq", "SHORT"); got != c.want {
			t.Errorf("%s: Unproven = %v, want %v", name, got, c.want)
		}
		if s.ProtectionEvidenceFor("Sim102", "MNQ").Unproven("MNQ", "SHORT") {
			t.Errorf("%s: another account is not this one", name)
		}
	}
}

// The PRODUCTION read loop keeps the levels, and they survive a reconnect.
func TestPositionRegrowThroughTheReadLoop(t *testing.T) {
	s, dial := probeServer(t)
	c := dial()
	frame := func(ps ...OpenPosition) {
		t.Helper()
		if err := WriteFrame(c, FramePositions, PositionsPayload{Account: "Sim101", Positions: append([]OpenPosition{}, ps...)}); err != nil {
			t.Fatal(err)
		}
		probeSeen(t, s, c)
	}
	one := OpenPosition{Symbol: "MNQ", Side: "short", Quantity: 1, AvgPrice: 100}
	unproven := func() bool { return s.ProtectionEvidenceFor("Sim101", "MNQ").Unproven("MNQ", "SHORT") }
	frame(one)
	frame()
	frame(one)
	if !unproven() {
		t.Fatal("flat, then Short 1 with no entry fill: unproven")
	}
	_ = c.Close()
	probeWait(t, "the disconnect", func() bool { return !s.IsConnected() })
	c = dial()
	frame(one)
	if !unproven() {
		t.Fatal("a reconnect proves nothing")
	}
	now := time.Now().Add(positionRegrowProof)
	s.SetReceiptClockForTest(func() time.Time { return now })
	frame(one)
	if unproven() {
		t.Fatal("a later frame repeats it: proven")
	}
}
