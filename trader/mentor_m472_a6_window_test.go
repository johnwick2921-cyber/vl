package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
)

// TestM472A6UnsetWindowIsRTHBand — m472 A6 (review round 2). An UNSET mentor
// trading window is the RTH band: the black band on his 5m chart (08:30 to about
// 15:15 CT = 405 minutes, screen-read) is the band he trades [Oct D5.2 @ 01:00-01:20]
// (frame "/home/hoang/mm-course/oct/scenes/Ngày 5.2 Cách vẽ và trade tại major levels/s_00004.jpg",
// notes OctD5.2-R2). The 30-minute 08:30-09:00 window is only a time-poor
// student's example [Oct D4.1 @ 12:52-13:00] (s_00761.jpg), so it is not a
// default. September D1.2 p1 @23:49-24:21 taught 08:30 + 60 min; October
// supersedes it. A stored 60 (strategy "mentosss") stays 60, a stored -1 stays -1.
// What stops entries after RTH is NOT the window but the EOD-flat session gate
// (mentorEODFlatAuthorRefusal), proven here at 15:30 CT on the same unset strategy.
func TestM472A6UnsetWindowIsRTHBand(t *testing.T) {
	ct := kernel.CTLocation()
	day := func(h, m int) time.Time { return time.Date(2026, 10, 23, h, m, 0, 0, ct) }
	setMentorNowSource(func() time.Time { return day(10, 30) })
	t.Cleanup(func() { setMentorNowSource(nil) })
	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong, Setup: "ISB"}

	unset := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	if start, minutes := unset.mentorWindowKnobs(); start != "08:30" || minutes != 405 {
		t.Fatalf("unset window = %s/%d, want 08:30/405 (the RTH band to 15:15 CT)", start, minutes)
	}
	if refuse, why := unset.mentorWindowGate(in); refuse {
		t.Fatalf("an unset strategy must be able to enter at 10:30 CT: %s", why)
	}
	// the owner's saved 60 is untouched.
	sixty := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorWindowMinutes: 60})
	if _, minutes := sixty.mentorWindowKnobs(); minutes != 60 {
		t.Fatalf("stored 60 = %d, want 60", minutes)
	}
	if refuse, _ := sixty.mentorWindowGate(in); !refuse {
		t.Fatal("a stored 08:30+60 must still refuse 10:30 CT")
	}
	off := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorWindowMinutes: -1})
	if _, minutes := off.mentorWindowKnobs(); minutes != -1 {
		t.Fatalf("stored -1 = %d, want -1", minutes)
	}
	// outside the band the window itself refuses (overnight is NOT open by default).
	for _, h := range [][2]int{{3, 0}, {8, 29}, {15, 15}, {17, 5}, {23, 30}} {
		setMentorNowSource(func() time.Time { return day(h[0], h[1]) })
		if refuse, _ := unset.mentorWindowGate(in); !refuse {
			t.Errorf("%02d:%02d CT: an unset strategy must be refused outside the RTH band", h[0], h[1])
		}
	}
	setMentorNowSource(func() time.Time { return day(10, 30) })
	// an explicit stored -1 is the owner's opt-out and stays "no window".
	if refuse, _ := off.mentorWindowGate(in); refuse {
		t.Fatal("stored -1 must remain no window at 10:30")
	}
	// what stops entries after RTH is still in force (session / EOD flat).
	if refuse, _ := unset.mentorEODFlatAuthorRefusal(in, day(15, 30)); !refuse {
		t.Fatal("15:30 CT must still be refused by the EOD-flat session gate on an unset strategy")
	}
	if refuse, why := unset.mentorEODFlatAuthorRefusal(in, day(10, 30)); refuse {
		t.Fatalf("10:30 CT must not be refused as EOD flat: %s", why)
	}
}

// TestM472A6UnsetWindowOvernightRefused (review round 2, replaces the round-1
// pin that recorded overnight as OPEN). An UNSET window refuses every hour
// outside 08:30 to 15:15 CT [Oct D5.2 @ 01:00-01:20, OctD5.2-R2]; the EOD-flat
// gate still binds inside the band from 14:45. An explicit -1 (owner opt-out)
// keeps the round-1 behaviour: the window never refuses.
func TestM472A6UnsetWindowOvernightRefused(t *testing.T) {
	ct := kernel.CTLocation()
	at := func(h, m int) time.Time { return time.Date(2026, 10, 26, h, m, 0, 0, ct) } // Monday
	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong, Setup: "ISB"}
	unset := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	optOut := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorWindowMinutes: -1})
	t.Cleanup(func() { setMentorNowSource(nil) })
	for _, c := range []struct {
		h, m          int
		windowRefuses bool
		eodRefuses    bool
	}{{3, 0, true, false}, {7, 0, true, false}, {8, 29, true, false}, {8, 30, false, false}, {10, 30, false, false}, {14, 45, false, true}, {15, 10, false, true}, {15, 15, true, true}, {16, 0, true, true}, {17, 5, true, false}, {19, 0, true, false}, {23, 30, true, false}} {
		setMentorNowSource(func() time.Time { return at(c.h, c.m) })
		if refuse, _ := unset.mentorWindowGate(in); refuse != c.windowRefuses {
			t.Errorf("%02d:%02d unset window refuses = %v, want %v", c.h, c.m, refuse, c.windowRefuses)
		}
		if refuse, why := optOut.mentorWindowGate(in); refuse {
			t.Errorf("%02d:%02d stored -1 must never refuse: %s", c.h, c.m, why)
		}
		if refuse, _ := unset.mentorEODFlatAuthorRefusal(in, at(c.h, c.m)); refuse != c.eodRefuses {
			t.Errorf("%02d:%02d CT EOD-flat refusal = %v, want %v", c.h, c.m, refuse, c.eodRefuses)
		}
	}
}
