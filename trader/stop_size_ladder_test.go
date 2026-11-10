package trader

import (
	"fmt"
	"strings"
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// Stop-to-size LADDER — trader half (owner order 2026-11-10: "stop loss can
// adjust contract to fit like mentor said ... cause some day it go big").
// Course numbers (October wins over September):
//   - ISB ~12 pts → size 5; ISB 20–30 pts → size 3 or no trade [OctD2.2-R73 @01:12:32];
//   - ~40 pts → do not trade; if you do, only 1 MNQ [OctD2.3-R47 @48:12, R48 @49:38];
//   - stop too big → the ONLY fix is reduce size, never a bigger target [OctD2.2-R49 @40:47].
// D4.1-R12 ("inside bar mà nó hai mươi mấy điểm… đừng vô" [D4.1 p1
// @05:41–06:08]) refused every ISB above 20; the owner ordered size-down over
// refusal, so with the ladder ON the ISB 20–40 stop is SIZED, not refused.

// ladderIntent builds a long entry intent with the given stop (pts) and a
// target 3R out (R9 never bites), the geometry the production path reads.
func ladderIntent(setup string, stopPts float64) mentor.Intent {
	price := 29000.0
	return mentor.Intent{Action: mentor.PlaceStopLimitEntry, Side: mentor.SideLong, Setup: setup,
		Price: price, Stop: price - stopPts, Target: price + 3*stopPts, ExecTFMin: 1}
}

// ladderRun runs the PRODUCTION entry path's gate + size: the resolver the ONE
// entry path uses (mentorEntryGateInputs), the R8/R9 rule gate
// (mentorRuleGate) and the sizing call site (mentorSizeFor). Returns the
// refusal ("" when sized) and the size choice.
func ladderRun(t *testing.T, at *AutoTrader, in mentor.Intent) (string, mentorSizeChoice) {
	t.Helper()
	extra := at.mentorEntryGateInputs(in, mentorExtraFor(in, false))
	if why := mentorRuleGate(in, extra); why != "" {
		return why, mentorSizeChoice{}
	}
	got, err := at.mentorSizeFor(in, extra)
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	return "", got
}

// TestStopLadderProductionCallSites — the course numbers through the
// production gate + size table, default knobs (ladder ON, 1 MNQ, max 40).
// RED before the change: ISB 25/35 and PHL 28 / BOX 38 are refused by R8.
func TestStopLadderProductionCallSites(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	for _, tc := range []struct {
		name     string
		setup    string
		stop     float64
		want     int
		wantTier string
		cite     string
	}{
		{"ISB 12 → 5 (base) [OctD2.2-R73]", "ISB", 12, 5, "base", ""},
		{"ISB 25 → 3 [OctD2.2-R73]", "ISB", 25, 3, "stop_ladder_reduced", "OctD2.2-R73"},
		{"ISB 30 → 3 (top of the 20–30 band)", "ISB", 30, 3, "stop_ladder_reduced", "OctD2.2-R73"},
		{"ISB 35 → 1 [OctD2.3-R47 @48:12]", "ISB", 35, 1, "stop_ladder_wide", "OctD2.3-R47"},
		{"ISB 40 → 1 (at the max)", "ISB", 40, 1, "stop_ladder_wide", "OctD2.3-R47"},
		{"PHL 28 → 3", "PHL", 28, 3, "stop_ladder_reduced", "OctD2.2-R73"},
		{"PLH 22 → 3 (the pre-ladder 20–25 band keeps its own tier)", "PLH", 22, 3, "reduced", ""},
		{"BOX 38 → 1", "BOX", 38, 1, "stop_ladder_wide", "OctD2.3-R47"},
		{"BOX 15 → 5 (unchanged)", "BOX", 15, 5, "base", ""},
	} {
		why, got := ladderRun(t, at, ladderIntent(tc.setup, tc.stop))
		if why != "" {
			t.Fatalf("%s: refused %q, want %d contracts", tc.name, why, tc.want)
		}
		if got.Contracts != tc.want || got.Tier != tc.wantTier {
			t.Fatalf("%s: got %d (%s — %s), want %d (%s)", tc.name, got.Contracts, got.Tier, got.Why, tc.want, tc.wantTier)
		}
		if tc.cite != "" {
			// the size line names the ladder tier, the stop and the citation
			if !strings.Contains(got.Why, tc.cite) || !strings.Contains(got.Why, "mentor_stop_size_ladder") ||
				!strings.Contains(got.Why, fmt.Sprintf("stop %.1f pts", tc.stop)) {
				t.Fatalf("%s: Why %q must name the tier knob, the stop and %s", tc.name, got.Why, tc.cite)
			}
		}
	}
	// 45 pts > mentor_wide_stop_max_pts 40 → refused at R8 [OctD2.3-R47/R48].
	for _, setup := range []string{"ISB", "PHL", "BOX"} {
		why, _ := ladderRun(t, at, ladderIntent(setup, 45))
		if !strings.HasPrefix(why, "R8:") || !strings.Contains(why, "mentor_wide_stop_max_pts") {
			t.Fatalf("%s 45: refusal %q, want R8 naming mentor_wide_stop_max_pts", setup, why)
		}
	}
	// SWING4H keeps its own tiers: a 35-pt swing is 2 MNQ (the swing base).
	sw := ladderIntent("SWING4H", 35)
	if why, got := ladderRun(t, at, sw); why != "" || got.Contracts != 2 || got.Tier != "swing4h" {
		t.Fatalf("SWING4H 35: why=%q got %d (%s), want 2 (swing4h) — the ladder never touches the swing", why, got.Contracts, got.Tier)
	}
	// The ladder never touches the target [OctD2.2-R49].
	in := ladderIntent("ISB", 35)
	tgt := in.Target
	ladderRun(t, at, in)
	if in.Target != tgt {
		t.Fatal("the ladder changed the target")
	}
}

// TestStopLadderOffIsToday — mentor_stop_size_ladder=false → the bot behaves
// EXACTLY as before the ladder: a 25-pt ISB refused by R8 at the 20-pt
// ceiling with the same string, a 28-pt PHL refused at 25, a 22-pt PHL sized
// by the pre-ladder band, a 12-pt ISB base.
func TestStopLadderOffIsToday(t *testing.T) {
	off := false
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorStopSizeLadder: &off})
	if why, _ := ladderRun(t, at, ladderIntent("ISB", 25)); why != "R8: stop 25.0 pts above the 20-pt ceiling — skip (the swing is the only exemption)" {
		t.Fatalf("ladder OFF ISB 25: %q, want the pre-ladder R8 refusal", why)
	}
	if why, _ := ladderRun(t, at, ladderIntent("PHL", 28)); why != "R8: stop 28.0 pts above the 25-pt ceiling — skip (the swing is the only exemption)" {
		t.Fatalf("ladder OFF PHL 28: %q, want the pre-ladder R8 refusal", why)
	}
	if why, got := ladderRun(t, at, ladderIntent("PHL", 22)); why != "" || got.Contracts != 3 || got.Tier != "reduced" {
		t.Fatalf("ladder OFF PHL 22: why=%q got %d (%s), want 3 (reduced)", why, got.Contracts, got.Tier)
	}
	if why, got := ladderRun(t, at, ladderIntent("ISB", 12)); why != "" || got.Contracts != 5 || got.Tier != "base" {
		t.Fatalf("ladder OFF ISB 12: why=%q got %d (%s), want 5 (base)", why, got.Contracts, got.Tier)
	}
	// A stored mentor_isb_max_pts 25 keeps its pre-ladder meaning when OFF:
	// a 24-pt ISB is the reduced band (3), a 26 refused.
	at25 := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorStopSizeLadder: &off, MentorISBMaxPts: 25})
	if why, got := ladderRun(t, at25, ladderIntent("ISB", 24)); why != "" || got.Contracts != 3 || got.Tier != "reduced" {
		t.Fatalf("ladder OFF isb_max 25, ISB 24: why=%q got %d (%s), want 3 (reduced)", why, got.Contracts, got.Tier)
	}
	if why, _ := ladderRun(t, at25, ladderIntent("ISB", 26)); !strings.HasPrefix(why, "R8: stop 26.0 pts above the 25-pt ceiling") {
		t.Fatalf("ladder OFF isb_max 25, ISB 26: %q, want R8 at 25", why)
	}
}

// TestStopLadderKnobs — the owner's knobs are honoured: wide contracts 2,
// wide max 50 (45 sized 2 instead of refused); a stored isb_max above the
// wide max is never narrowed; the ladder only LOWERS (an owner reduced knob
// of 1 stays 1 in the 20–30 band, a base of 2 stays 2 at 35 with wide 3).
func TestStopLadderKnobs(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorWideStopContracts: 2, MentorWideStopMaxPts: 50})
	if why, got := ladderRun(t, at, ladderIntent("ISB", 45)); why != "" || got.Contracts != 2 || got.Tier != "stop_ladder_wide" {
		t.Fatalf("wide 2 / max 50, ISB 45: why=%q got %d (%s), want 2 (stop_ladder_wide)", why, got.Contracts, got.Tier)
	}
	if why, _ := ladderRun(t, at, ladderIntent("PHL", 51)); !strings.HasPrefix(why, "R8:") {
		t.Fatalf("max 50, PHL 51: %q, want R8", why)
	}
	atHi := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorISBMaxPts: 60})
	if why, got := ladderRun(t, atHi, ladderIntent("ISB", 55)); why != "" || got.Contracts != 1 {
		t.Fatalf("stored isb_max 60 + ladder ON, ISB 55: why=%q got %d, want 1 (never narrowed)", why, got.Contracts)
	}
	atLow := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorBaseContracts: 2, MentorWideStopContracts: 3})
	if why, got := ladderRun(t, atLow, ladderIntent("ISB", 35)); why != "" || got.Contracts != 2 || got.Tier != "base" {
		t.Fatalf("base 2 / wide 3, ISB 35: why=%q got %d (%s), want 2 (base — the ladder never raises)", why, got.Contracts, got.Tier)
	}
}

// TestStopLadderTableOrder — the bare size table (zero value = ladder ON):
// the ladder caps every earlier tier (old extreme, 2m) and never the swing.
func TestStopLadderTableOrder(t *testing.T) {
	const base, conf, big, reduced, swing4h, spent, mx = 5, 5, 5, 3, 2, 2, 20
	for _, c := range []struct {
		name     string
		in       mentorTierInputs
		want     int
		wantTier string
	}{
		{"old-extreme ISB 35 → 1", mentorTierInputs{Setup: "ISB", StopPts: 35, ISBOldExtreme: true}, 1, "stop_ladder_wide"},
		{"2m PHL 33 → 1", mentorTierInputs{Setup: "PHL", StopPts: 33, ExecTFMin: 2}, 1, "stop_ladder_wide"},
		{"2m PHL 25 → 3 (exec_2m_reduce already 3)", mentorTierInputs{Setup: "PHL", StopPts: 25, ExecTFMin: 2}, 3, "exec_2m_reduce"},
		{"confluence ISB 26 → 3", mentorTierInputs{Setup: "ISB", StopPts: 26, Confluence: true}, 3, "stop_ladder_reduced"},
		{"SWING4H 35 → swing tier", mentorTierInputs{Setup: "SWING4H", StopPts: 35}, 2, "swing4h"},
		{"ladder OFF ISB 35 → base (the table alone; R8 refuses first in production)", mentorTierInputs{Setup: "ISB", StopPts: 35, StopLadderOff: true}, 5, "base"},
	} {
		got, err := mentorContractsFor(c.in, base, conf, big, reduced, swing4h, spent, mx)
		if err != nil {
			t.Fatal(err)
		}
		if got.Contracts != c.want || got.Tier != c.wantTier {
			t.Fatalf("%s: got %d (%s — %s), want %d (%s)", c.name, got.Contracts, got.Tier, got.Why, c.want, c.wantTier)
		}
	}
}
