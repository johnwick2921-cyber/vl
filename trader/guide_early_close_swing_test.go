package trader

// m471 L1 review finding 1 (GUIDE CONTENT LAW). The shortened-day fix made the
// SWING4H position and arm NOT exempt from a shortened day's flat, and moved
// the mentor's last entry to early close − 15 (12:00 on a 12:15 day). The
// guide kept saying the opposite in several places — status.ts's SHORTENED
// paragraph ended "SWING4H positions and swing arms keep their own exemption"
// (false on the closed-market path the same paragraph describes), and the
// tradingDay EOD-flat card, the mentor lifetime card, the FAQ, the glossary
// and the Window-length knob stated the swing exemption / 15:45 cutoff with no
// shortened-day exception.
//
// These pins read the times from the PRODUCTION functions (earlyCloseFlatDue,
// mentorPastDailyHaltCutoff) and drive the PRODUCTION closed-market path
// (retireBookAfterEarlyCloseAt → enforceEODFlatAt) with a swing on the book,
// then hold the guide's words to what that code did.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
)

// shortenedDayGuideTimes returns the 11-27 flat and mentor cutoff as "HH:MM",
// read minute by minute from the production gates.
func shortenedDayGuideTimes(t *testing.T, at *AutoTrader) (flat, cutoff string) {
	t.Helper()
	day := ctDay(2026, 11, 27)
	for m := 0; m < 17*60 && (flat == "" || cutoff == ""); m++ {
		now := day.Add(time.Duration(m) * time.Minute)
		if flat == "" {
			if hhmm, due := earlyCloseFlatDue(now); due {
				flat = hhmm
			}
		}
		if cutoff == "" {
			if _, blocked := at.mentorPastDailyHaltCutoff(now); blocked {
				cutoff = fmt.Sprintf("%02d:%02d", m/60, m%60)
			}
		}
	}
	if flat == "" || cutoff == "" {
		t.Fatalf("fixture: 2026-11-27 must be a shortened day with a flat and a mentor cutoff (flat=%q cutoff=%q)", flat, cutoff)
	}
	return flat, cutoff
}

// guideLine returns the one source line of a guide content file that holds
// anchor (every guide string is a single-line literal).
func guideLine(t *testing.T, file, anchor string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "web", "src", "guide", "content", file))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, anchor) {
			return l
		}
	}
	t.Fatalf("%s: anchor %q is gone — re-anchor this pin", file, anchor)
	return ""
}

var guideSentenceEnd = regexp.MustCompile(`[.!?](\s|'|$)`)

// unqualifiedSwingExemptions returns every sentence that grants the SWING4H an
// exemption from the flat with no shortened-day / ordinary-day qualifier.
func unqualifiedSwingExemptions(line string) []string {
	var bad []string
	for _, s := range guideSentenceEnd.Split(line, -1) {
		low := strings.ToLower(s)
		if !strings.Contains(low, "swing") {
			continue
		}
		if !strings.Contains(low, "exempt") && !strings.Contains(low, "except the swing4h") {
			continue
		}
		if strings.Contains(low, "not exempt") || strings.Contains(low, "ordinary") || strings.Contains(low, "shortened") {
			continue
		}
		bad = append(bad, strings.TrimSpace(s))
	}
	return bad
}

func TestGuideShortenedDaySwingAndMentorCutoffMatchTheBinary(t *testing.T) {
	// Production, closed-market path: 11-27 12:20 CT is past the 12:15 early
	// close. retireBookAfterEarlyCloseAt must flatten the swing and cancel its
	// arm — the swing is NOT exempt on this path.
	day := ctDay(2026, 11, 27)
	sig := "sig-swing-guide-1127"
	acks := make(chan ntwire.OrderUpdatePayload, 4)
	at, rt := swingFixture(t, hm(day, 11, 0, 0), sig, acks)
	now := hm(day, 12, 20, 0)
	setTraderClock(t, now)
	go func() {
		time.Sleep(20 * time.Millisecond)
		acks <- ntwire.OrderUpdatePayload{SignalID: sig, State: "cancelled"}
	}()
	at.retireBookAfterEarlyCloseAt(now)
	if !hasEvent(rt, "close_long:MNQ") || !hasEvent(rt, "cancel:"+sig) {
		t.Fatalf("premise: 11-27 12:20 closed path must retire the SWING4H position and arm, wire=%v", rt.snapshot())
	}

	flat, cutoff := shortenedDayGuideTimes(t, at)

	type pin struct {
		file, anchor string
		mustHave     []string // the production times the entry must name
	}
	for _, p := range []pin{
		{"status.ts", "a TRADING day: reads fire, arms are allowed", []string{flat, cutoff}},
		{"tradingDay.ts", "For a Day-Plan or mentor trader, every resting entry is cancelled first", []string{"shortened", flat, cutoff}},
		{"mentor.ts", "ON TOP of every expiry, the EOD flat cancels every INTRADAY mentor order", []string{"shortened", flat, cutoff}},
		{"mentor.ts", `The "Window length" box offers`, []string{cutoff}},
		{"faq.ts", "EOD flat, and only for a Day-Plan or mentor trader", []string{"shortened", flat}},
		{"glossary.ts", "The session-end flat: cancel resting entries", []string{"shortened", flat}},
	} {
		line := guideLine(t, p.file, p.anchor)
		for _, s := range unqualifiedSwingExemptions(line) {
			t.Errorf("%s (%q): %q grants the SWING4H a flat exemption with no shortened-day exception — on 11-27 the binary flattens the swing (closed path at 12:20 retired it; flat %s CT)", p.file, p.anchor, s, flat)
		}
		for _, want := range p.mustHave {
			if !strings.Contains(strings.ToLower(line), strings.ToLower(want)) {
				t.Errorf("%s (%q): lacks %q (production shortened-day flat %s CT, mentor cutoff %s CT)", p.file, p.anchor, want, flat, cutoff)
			}
		}
	}
}
