package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
)

// TestM472A6UnsetWindowIsWholeRTHBand — m472 A6. The mentor trading window is
// OFF when unset: the black RTH band on his 5m chart (08:30 to about 15:15 CT)
// is the window he trades [Oct D5.2 @ 01:00-01:20]
// (frame "/home/hoang/mm-course/oct/scenes/Ngày 5.2 Cách vẽ và trade tại major levels/s_00004.jpg",
// notes OctD5.2-R2). The 30-minute 08:30-09:00 window is only a time-poor
// student's example [Oct D4.1 @ 12:52-13:00] (s_00761.jpg), so it is not a
// default. September D1.2 p1 @23:49-24:21 taught 08:30 + 60 min; October
// supersedes it. A stored 60 (strategy "mentosss") stays 60, a stored -1 stays -1.
// What stops entries after RTH is NOT the window but the EOD-flat session gate
// (mentorEODFlatAuthorRefusal), proven here at 15:30 CT on the same unset strategy.
func TestM472A6UnsetWindowIsWholeRTHBand(t *testing.T) {
	ct := kernel.CTLocation()
	day := func(h, m int) time.Time { return time.Date(2026, 10, 23, h, m, 0, 0, ct) }
	setMentorNowSource(func() time.Time { return day(10, 30) })
	t.Cleanup(func() { setMentorNowSource(nil) })
	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong, Setup: "ISB"}

	unset := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	if _, minutes := unset.mentorWindowKnobs(); minutes != -1 {
		t.Fatalf("unset window minutes = %d, want -1 (window off)", minutes)
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
	// what stops entries after RTH is still in force (session / EOD flat).
	if refuse, _ := unset.mentorEODFlatAuthorRefusal(in, day(15, 30)); !refuse {
		t.Fatal("15:30 CT must still be refused by the EOD-flat session gate on an unset strategy")
	}
	if refuse, why := unset.mentorEODFlatAuthorRefusal(in, day(10, 30)); refuse {
		t.Fatalf("10:30 CT must not be refused as EOD flat: %s", why)
	}
}
