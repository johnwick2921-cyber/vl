package mentor

import (
	"strings"
	"testing"
)

// answers-1112 review (2026-11-12) — the confluence PHL/PLH room at the new
// phl_room_multiple default.
//
// roomRefusal asks reward >= roomMultiple × Leg1RiskMultiple(confluence) × risk.
// A confluence PHL's leg 1 is 2R ("at least là risk reward 1-2 … trường hợp
// đặc biệt, cộng hưởng" [Sept D3.4 p3 @07:38-08:09, @10:32-11:05]), so at the
// October default 1 the confluence room is 1 × 2R = 2R — the report's final
// setting ("A confluence PHL still needs 2R through Leg1RiskMultiple(confluence)
// = 2, which matches Sept D3.4", REPORT-mentor-answers-1112.md Q2). At the
// revert value 2 it is 2 × 2R = 4R (today before answers-1112). The guide,
// registry and mentor.go comment said "still asks 2x its 2R leg 1" (= 4R); the
// review caught the mismatch and this test pins the real numbers at the
// production core (phlPLHR2, the function the level path calls with
// confluence = e.mtfConfluence(...)).
//
// Geometry: the WE10 reference candle (entry 28,980, stop 28,988.75, risk 8.75).
func TestAnswers1112ReviewConfluencePHLRoomIsTwoR(t *testing.T) {
	run := func(cfg Config, obstacle float64, confluence bool) (Intent, bool, string) {
		levels := []Level{{Key: "old-low-line", Kind: KindKeyLevel, Price: obstacle}}
		return phlPLHR2(we10Touch(), Level{Kind: KindOldExtreme, Role: OldLow, Price: 28_946}, 0, 3, 0, levels, cfg, confluence)
	}
	rev := answersCfg()
	rev.PHLRoomMultiple = 2 // revert: today before answers-1112

	// 28,958: reward 22.00 = 2.51R — inside 4R, past 2R.
	if _, ok, why := run(answersCfg(), 28_958, true); !ok {
		t.Fatalf("default (1): a confluence PLH with 2.51R room must pass the 1 x 2R = 2R room [Sept D3.4 p3 \"at least 1-2\"]; refused %q", why)
	}
	if _, ok, why := run(rev, 28_958, true); ok || !strings.HasPrefix(why, "room: reward 22.00 pts < 35.00 pts") {
		t.Fatalf("revert (2): the confluence room must be 2 x 2R = 4R (35.00 pts), as today; ok=%v why=%q", ok, why)
	}
	// 28,965: reward 15.00 = 1.71R — the non-confluence WE10 trade passes, the
	// confluence one is short of its 2R leg 1 and is refused at the default.
	if _, ok, why := run(answersCfg(), 28_965, false); !ok {
		t.Fatalf("default: the non-confluence WE10 (1.71R) must pass; refused %q", why)
	}
	if _, ok, why := run(answersCfg(), 28_965, true); ok || !strings.HasPrefix(why, "room: reward 15.00 pts < 17.50 pts (1.00x the leg-1 2R take-profit)") {
		t.Fatalf("default: a confluence PLH at 1.71R must be refused room against 2R (17.50 pts); ok=%v why=%q", ok, why)
	}
}
