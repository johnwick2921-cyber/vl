package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
)

// The 4h EMA 34 feeds only the swing line, so a store with 94 of the 102 closed
// 4h candles blocks SWING4H entries only: the intraday entries (ISB/PHL/box)
// pass the placement gate. Any other missing source blocks every entry.
func TestMentorSeedScopePlacementGate(t *testing.T) {
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireMentorPlacementSeams(t)
	ct := kernel.CTLocation()
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, ct) }
	t.Cleanup(func() { mentorNowSource = nil })

	depth := func(h4, m1 int) {
		mentorSourceDepthSource = func(name string) (int, bool) {
			switch name {
			case "4h EMA34":
				return h4, true
			case "1m EMA34":
				return m1, true
			}
			return 9999, true
		}
	}
	isb := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12}
	swing := isb
	swing.Setup = "SWING4H"
	choice := mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}
	var placed []string
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed = append(placed, i.Setup) }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	// 94/102 4h, everything else met: ISB placed, SWING4H refused.
	depth(94, 9999)
	at.mentorPlaceIntent(isb, choice, 1000, 1100)
	at.mentorPlaceIntent(swing, choice, 1000, 1100)
	if len(placed) != 1 || placed[0] != "ISB" {
		t.Fatalf("94/102 4h: want only the ISB placed, got %v", placed)
	}
	if got := MentorCountSnapshot()["mentor_sources_missing"]; got != 1 {
		t.Fatalf("the swing refusal must be counted mentor_sources_missing once, got %d", got)
	}
	if b := at.mentorSourcesBlocking("ISB"); len(b) != 0 {
		t.Fatalf("a short 4h EMA must not block the ISB: %v", b)
	}
	if b := strings.Join(at.mentorSourcesBlocking("SWING4H"), ", "); !textHas(b, "4h EMA34 (94/102)") {
		t.Fatalf("the swing must be blocked by the 4h warm-up: %q", b)
	}

	// 1m EMA 34 short: every entry refused (the 4h is met).
	placed = nil
	depth(9999, 50)
	at.mentorPlaceIntent(isb, choice, 1000, 1100)
	at.mentorPlaceIntent(swing, choice, 1000, 1100)
	if len(placed) != 0 {
		t.Fatalf("1m EMA34 short: every entry must be refused, got %v", placed)
	}

	// 102 reached: the swing is placed.
	depth(102, 9999)
	at.mentorPlaceIntent(swing, choice, 1000, 1100)
	if len(placed) != 1 || placed[0] != "SWING4H" {
		t.Fatalf("102 closed 4h candles: the swing must be placed, got %v", placed)
	}
}

// The boot/seed line prints every missing source and which entry kinds it blocks.
func TestMentorSeedRefusalLineNamesWhatItBlocks(t *testing.T) {
	m4h := "bar history depth: 4h EMA34 warm-up (94/102 4h candles)"
	m1m := "bar history depth: 1m EMA34 warm-up (50/102 1m bars)"

	only4h := mentorSeedRefusalLine([]string{m4h})
	for _, want := range []string{"SWING4H entries only (intraday entries allowed)", m4h, "[blocks SWING4H entries only]"} {
		if !strings.Contains(only4h, want) {
			t.Fatalf("4h-only line %q missing %q", only4h, want)
		}
	}
	both := mentorSeedRefusalLine([]string{m4h, m1m})
	for _, want := range []string{"REFUSING ALL entries", m4h, m1m, "[blocks ALL entries]"} {
		if !strings.Contains(both, want) {
			t.Fatalf("4h+1m line %q missing %q", both, want)
		}
	}
	if strings.Contains(both, "intraday entries allowed") {
		t.Fatalf("with a non-4h source missing the line must not say intraday entries are allowed: %q", both)
	}
}
