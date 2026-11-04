package trader

import (
	"strings"
	"testing"
	"time"

	"vl/calendar"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
)

// m466 P3 (release #61, owner order 2026-11-02 "fix everything the video check
// found") — the trader half: the ISB ceiling 20 at R8 (G23 D4.1-R12) with PHL/PLH
// keeping 25, the print window's PrintAtMs (G58 D4.4-R6), the three knobs end
// to end, and the done-after-win nil → ON pin (G8 D1.2-R34).

// TestM61RuleGateISBTwentyPHLTwentyFive — G23: "nếu như một cái setup inside bar
// mà nó hai mươi mấy điểm, làm ơn giúp em đừng vô" [D4.1 p1 @05:41–06:08]. The
// D3.4-R28 example (sell stop 29,034.25, stop 29,056.75 — 22.50 pts) is refused
// at R8 as an ISB; the SAME geometry tagged PHL keeps the D3.3-R1 25 ceiling
// ("hơn 20, 25 đổ lại ok" [D3.3 p1 @01:59–02:09]) and passes. A stored 25
// keeps the ISB.
// MUTANT: R8 reads the ISB ceiling for every non-swing setup → the PHL is
// refused → RED.
func TestM61RuleGateISBTwentyPHLTwentyFive(t *testing.T) {
	in := func(setup string) mentor.Intent {
		return mentor.Intent{Action: mentor.PlaceStopLimitEntry, Setup: setup, Side: mentor.SideShort,
			Price: 29034.25, Stop: 29056.75, Target: 28980}
	}
	if why := mentorRuleGate(in("ISB"), mentorTierInputs{}); !strings.HasPrefix(why, "R8") {
		t.Fatalf("ISB 22.50 pts at the default ceiling: %q, want an R8 refusal", why)
	}
	if why := mentorRuleGate(in("ISB"), mentorTierInputs{ISBMaxStopPts: 20}); !strings.HasPrefix(why, "R8") {
		t.Fatalf("ISB 22.50 pts at the resolved 20: %q, want R8", why)
	}
	if why := mentorRuleGate(in("ISB"), mentorTierInputs{ISBMaxStopPts: 25}); why != "" {
		t.Fatalf("a stored 25 must keep the 22.50 ISB: %q", why)
	}
	for _, s := range []string{"PHL", "PLH", "BOX"} {
		if why := mentorRuleGate(in(s), mentorTierInputs{ISBMaxStopPts: 20}); why != "" {
			t.Fatalf("%s 22.50 pts must keep the D3.3-R1 25 ceiling: %q", s, why)
		}
	}
	over := in("PHL")
	over.Stop = 29060 // 25.75 pts
	if why := mentorRuleGate(over, mentorTierInputs{ISBMaxStopPts: 20}); !strings.HasPrefix(why, "R8") {
		t.Fatalf("PHL 25.75 pts: %q, want R8 (D3.3-R1 > 25 skip)", why)
	}
	twenty := in("ISB")
	twenty.Stop = 29054.25 // exactly 20.00 — his own "20 points" [D4.1 p1 @02:39–03:20]
	if why := mentorRuleGate(twenty, mentorTierInputs{ISBMaxStopPts: 20}); why != "" {
		t.Fatalf("ISB 20.00 pts must pass: %q", why)
	}

	// The production resolver: unset → 20 (the kernel default, read not
	// retyped); a stored 25 wins.
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	if got := mentorCourseExitsResolve(at, at.mentorRiskControl()).ISBMaxPts; got != 20 {
		t.Fatalf("mentor_isb_max_pts unset resolves to %g, want 20", got)
	}
	at25 := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorISBMaxPts: 25})
	if got := mentorCourseExitsResolve(at25, at25.mentorRiskControl()).ISBMaxPts; got != 25 {
		t.Fatalf("stored 25 resolves to %g, want 25 (stored values win)", got)
	}
}

// TestM61SizeBandKeepsPHLReduced — B12: the 20–25 reduced band stays for
// PHL/PLH/BOX (D3.3-R4 "stop loss tới hai mấy điểm luôn thì mình giảm size"
// [D3.3 p1 @01:09–02:24]); for the ISB it is unreachable at default (R8 refuses
// first) and reachable only with a stored mentor_isb_max_pts above 20.
// MUTANT: band top = the ISB ceiling for every setup → the PHL sizes base → RED.
func TestM61SizeBandKeepsPHLReduced(t *testing.T) {
	const base, conf, big, reduced, swing4h, spent, mx = 5, 5, 5, 3, 2, 2, 20
	for _, c := range []struct {
		name string
		in   mentorTierInputs
		want string
	}{
		{"PHL 22.50 at default", mentorTierInputs{Setup: "PHL", StopPts: 22.5, ISBMaxStopPts: 20}, "reduced"},
		{"BOX 22.50 at default", mentorTierInputs{Setup: "BOX", StopPts: 22.5}, "reduced"},
		{"ISB 22.50 with a stored 25", mentorTierInputs{Setup: "ISB", StopPts: 22.5, ISBMaxStopPts: 25}, "reduced"},
		{"ISB 18 at default", mentorTierInputs{Setup: "ISB", StopPts: 18, ISBMaxStopPts: 20}, "base"},
	} {
		got, err := mentorContractsFor(c.in, base, conf, big, reduced, swing4h, spent, mx)
		if err != nil {
			t.Fatal(err)
		}
		if got.Tier != c.want {
			t.Fatalf("%s: tier %s (%d), want %s — %s", c.name, got.Tier, got.Contracts, c.want, got.Why)
		}
	}
}

// TestM61PrintWindowCarriesPrintAt — G58: the window builder stamps the print
// minute so the kernel can start the 1h/4h read 30 minutes before it; the
// order-hold window itself stays −10m/+5m.
// MUTANT: drop PrintAtMs in mentorNewsPrintWindowsFromEvents → RED.
func TestM61PrintWindowCarriesPrintAt(t *testing.T) {
	loc := kernel.CTLocation()
	at := time.Date(2026, 9, 11, 7, 30, 0, 0, loc)
	ws := mentorNewsPrintWindowsFromEvents([]calendar.Event{{Title: "CPI m/m", Impact: calendar.T1, Time: at}})
	if len(ws) != 1 {
		t.Fatalf("windows %v, want one", ws)
	}
	w := ws[0]
	if w.PrintAtMs != at.UnixMilli() {
		t.Fatalf("PrintAtMs %d, want the 07:30 print %d", w.PrintAtMs, at.UnixMilli())
	}
	if w.FromMs != at.Add(-10*time.Minute).UnixMilli() || w.ToMs != at.Add(5*time.Minute).UnixMilli() {
		t.Fatalf("the hold window moved: %d–%d", w.FromMs, w.ToMs)
	}
}

// TestM61KnobsResolveAndApply — the three knobs end to end (store → resolver →
// evaluator config → boot line): unset = the course; a stored value wins; a bad
// one fails closed to the default and is counted.
// MUTANT: applyMentorCourseExits not writing a field → RED.
func TestM61KnobsResolveAndApply(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	cfg := mentor.DefaultConfig()
	cfg.CourseExits.Open5mBreak, cfg.CourseExits.NewsWaitMode, cfg.CourseExits.NewsReadLeadMin = "x", "x", -1
	applyMentorCourseExits(at, &cfg, at.mentorRiskControl())
	if cfg.CourseExits.Open5mBreak != mentor.Open5mBreak1mClose || cfg.CourseExits.NewsWaitMode != mentor.NewsWaitBoth || cfg.CourseExits.NewsReadLeadMin != 30 {
		t.Fatalf("defaults: %q %q %d, want 1m_close both 30", cfg.CourseExits.Open5mBreak, cfg.CourseExits.NewsWaitMode, cfg.CourseExits.NewsReadLeadMin)
	}
	zero := 0
	atS := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorOpen5mBreak: "5m_close", MentorNewsWaitMode: " 4H_ONLY ", MentorNewsReadLeadMin: &zero})
	applyMentorCourseExits(atS, &cfg, atS.mentorRiskControl())
	if cfg.CourseExits.Open5mBreak != mentor.Open5mBreak5mClose || cfg.CourseExits.NewsWaitMode != mentor.NewsWait4hOnly || cfg.CourseExits.NewsReadLeadMin != 0 {
		t.Fatalf("stored: %q %q %d, want 5m_close 4h_only 0", cfg.CourseExits.Open5mBreak, cfg.CourseExits.NewsWaitMode, cfg.CourseExits.NewsReadLeadMin)
	}
	bad := 500
	atB := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorOpen5mBreak: "15m", MentorNewsWaitMode: "1h_only", MentorNewsReadLeadMin: &bad})
	applyMentorCourseExits(atB, &cfg, atB.mentorRiskControl())
	if cfg.CourseExits.Open5mBreak != mentor.Open5mBreak1mClose || cfg.CourseExits.NewsWaitMode != mentor.NewsWaitBoth || cfg.CourseExits.NewsReadLeadMin != 30 {
		t.Fatalf("bad values must fail closed to the defaults: %q %q %d", cfg.CourseExits.Open5mBreak, cfg.CourseExits.NewsWaitMode, cfg.CourseExits.NewsReadLeadMin)
	}
	snap := atB.mentorCountSnapshot()
	for _, k := range []string{"open5m_break_bad_value", "news_wait_mode_bad_value", "news_read_lead_min_bad_value"} {
		if snap[k] == 0 {
			t.Fatalf("bad value not counted under %s: %v", k, snap)
		}
	}
	line := atS.mentorCourseExitsBootLine()
	for _, want := range []string{"open5m_break=5m_close", "news_wait_mode=4h_only", "news_read_lead_min=0", "isb_max_pts=20"} {
		if !strings.Contains(line, want) {
			t.Fatalf("boot line missing %q: %s", want, line)
		}
	}
}

// TestM61DoneAfterWinNilOn — G8 D1.2-R34 (doc fix only): nil → ON, the course
// default ("làm ơn đóng máy lại" [D1.2 p2 @05:28–06:27]); m186 reverted the
// 2026-10-16 OFF. The comments at the switch now say so.
func TestM61DoneAfterWinNilOn(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	if !at.mentorDoneAfterWinEnabled() {
		t.Fatal("mentor_done_after_win nil must read ON")
	}
	off := false
	if mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorDoneAfterWin: &off}).mentorDoneAfterWinEnabled() {
		t.Fatal("an explicit false must read OFF")
	}
}
