package trader

import (
	"strings"
	"testing"

	"vl/kernel/mentor"
)

// TestAnswers1112ReviewSwingSkipMessageQuotesTheEightyLine — answers-1112
// review (P3): the trader's R31 backstop prints the resolved ceiling (80 since
// Q13) and must quote the line that states 80 ("7-80 điểm … cứ vô 1-2" [Sept
// D5.2 p2 @05:25-05:33]), not only the 100-pt "đừng vô" line beside it.
func TestAnswers1112ReviewSwingSkipMessageQuotesTheEightyLine(t *testing.T) {
	skip := mentorCourseExitsResolve(nil, nil).SwingMaxStopPts
	in := mentor.Intent{Setup: "SWING4H", Side: mentor.SideShort, Price: 28123.50, Stop: 28123.50 + 85}
	why := mentorRuleGate(in, mentorTierInputs{Setup: "SWING4H", StopPts: 85, SwingSkipStopPts: skip})
	if !strings.HasPrefix(why, "R31: swing stop 85.00 pts above the 80-pt ceiling") {
		t.Fatalf("an 85-pt swing stop must be refused against the 80-pt default: %q", why)
	}
	if !strings.Contains(why, "[D5.2 p2 @05:25-05:33]") || !strings.Contains(why, "7-80 điểm") {
		t.Fatalf("the R31 refusal must quote the line that states 80 [D5.2 p2 @05:25-05:33]: %q", why)
	}
}
