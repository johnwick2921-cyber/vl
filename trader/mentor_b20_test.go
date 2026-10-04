package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
)

// ── B20 CONFLUENCE UPGRADE — trader half (FIXES.md B20, D3.4 p3 @09:17–12:59)
//
// "a later flip to the trade's side upgrades it to confluence (hold ≥1:2,
// exit C)" — the emit is DS-103's kernel; these pins cover the trader half:
// the pure switch, the handler (exit branch → C, size NEVER re-read) and the
// placement-side branch registration the upgrade switches.

func TestMentorConfluenceUpgradeModePure(t *testing.T) {
	cases := []struct {
		name, current, wantMode, wantWhy string
	}{
		{"B upgrades to C", "B", "C", "exit B → C"},
		{"A upgrades to C", "A", "C", "exit A → C"},
		{"C is already confluence", "C", "C", "already confluence"},
		{"no position is a no-op", "", "", "no open mentor position"},
		{"the swing never upgrades", "swing", "swing", "holds by the 4h"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mode, why := mentorConfluenceUpgradeMode(c.current)
			if mode != c.wantMode {
				t.Fatalf("mode %q, want %q (why %q)", mode, c.wantMode, why)
			}
			if !strings.Contains(why, c.wantWhy) {
				t.Fatalf("why %q does not contain %q", why, c.wantWhy)
			}
		})
	}
}

// TestMentorConfluenceUpgradeHandler: the upgrade switches the registered exit
// branch to C and NEVER re-runs the size table (the size-unchanged half of the
// rule). Mutant: dropping the setMentorExitMode call leaves the branch B → RED.
func TestMentorConfluenceUpgradeHandler(t *testing.T) {
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.setMentorExitMode("long", "B")

	sized := false
	mentorSizeForHook = func() { sized = true }
	t.Cleanup(func() { mentorSizeForHook = nil })

	in := mentor.Intent{Action: mentorActionConfluenceUpgrade, Side: mentor.SideLong,
		Reason: "trigger flipped to the long side [B20, D3.4 p3 @09:17]"}
	at.mentorConfluenceUpgrade(in)

	if got := at.mentorExitMode("long"); got != "C" {
		t.Fatalf("the upgrade must switch the branch to C, got %q", got)
	}
	if sized {
		t.Fatalf("the upgrade must never re-run the size table")
	}
	if got := MentorCountSnapshot()["exit_upgrade_c"]; got != 1 {
		t.Fatalf("the upgrade must be counted once, got %d", got)
	}

	// a second upgrade on an already-C position is a counted no-op, never a
	// second exit_upgrade_c.
	at.mentorConfluenceUpgrade(in)
	if got := MentorCountSnapshot()["exit_upgrade_c"]; got != 1 {
		t.Fatalf("an already-confluence position must not double-count, got %d", got)
	}
	if got := MentorCountSnapshot()["exit_upgrade_noop"]; got != 1 {
		t.Fatalf("the no-op must be counted once, got %d", got)
	}

	// an upgrade naming a side with no open position is ignored + counted.
	sh := in
	sh.Side = mentor.SideShort
	at.mentorConfluenceUpgrade(sh)
	if got := MentorCountSnapshot()["exit_upgrade_no_position"]; got != 1 {
		t.Fatalf("a no-position upgrade must be counted once, got %d", got)
	}
	if got := at.mentorExitMode("short"); got != "" {
		t.Fatalf("a no-position upgrade must not fabricate a branch, got %q", got)
	}
}

// TestMentorPlacementRegistersExitBranch: the entry-time fork (B for a plain
// ISB) is registered per open position — the state a later upgrade switches.
func TestMentorPlacementRegistersExitBranch(t *testing.T) {
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireMentorPlacementSeams(t)
	ct := kernel.CTLocation()
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, ct) }
	t.Cleanup(func() { mentorNowSource = nil })

	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12}
	choice := mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}

	var placed int
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed++ }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })
	mentorLatestPriceSource = func() (float64, bool) { return 20999.50, true }
	t.Cleanup(func() { mentorLatestPriceSource = nil })

	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 1 {
		t.Fatalf("a safe-price ISB entry must reach the placement path, placed=%d", placed)
	}
	if got := at.mentorExitMode("long"); got != "B" {
		t.Fatalf("an ISB entry forks B at entry and registers it, got %q", got)
	}
}
