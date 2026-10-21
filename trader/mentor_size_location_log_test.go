package trader

import (
	"strings"
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// TestMentorSizeLogNamesTheLocation — TRENDLINE-THIRD-TOUCH: the size line
// ("🧑‍🏫 mentor size: …") carries the place the setup was taken at, read from
// the intent's AnchorKey, so a trade's location is in the log. A PHL at a
// support trendline logs "at a location: trendline long"; a plain ISB (no
// anchor) says so. RED at origin/dev (the line had no location).
func TestMentorSizeLogNamesTheLocation(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	buf := captureTraderLog(t)

	phl := mentor.Intent{
		Action: mentor.PlaceStopEntry, Side: mentor.SideLong, Setup: "PHL",
		Price: 21000, Stop: 20992, Target: 21016, StopPts: 8, TargetPts: 16,
		AnchorKey: "trendline:long:1789480920000:1789481280000", Anchor: 20995,
	}
	if _, err := at.mentorSizeFor(phl, mentorExtraFor(phl, false)); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, "mentor size:") || !strings.Contains(out, "at a location: trendline long") {
		t.Fatalf("size line does not name the trendline location:\n%s", out)
	}

	buf.Reset()
	isb := isbIntent("")
	if _, err := at.mentorSizeFor(isb, mentorExtraFor(isb, false)); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, "mentor size:") || !strings.Contains(out, "no location anchor") {
		t.Fatalf("an ISB with no anchor must say so:\n%s", out)
	}
}
