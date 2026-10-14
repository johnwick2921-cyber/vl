package trader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vl/kernel"
	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// T1 (release #22, UI-GUIDE-TRUTH): release #21 told the owner "a level order
// placed after 15:00 rests through to 15:00 the NEXT day". The evaluator stamps
// that expiry, but the TRADER's EOD flat cancels every intraday mentor arm the
// moment no session window is open (14:45–17:00 CT), so a level order never
// lives past 14:45 and one authored in the gap dies at birth (DB 256/282/283/343).
//
// These pins run the PRODUCTION armed pass on the SHIPPED session registry (no
// test registry written), then pin the guide sentence to that registry.

func seedLevelArm(t *testing.T, at *AutoTrader, ledger *store.ArmedOrderStore, scenario string, expiryMs int64) {
	t.Helper()
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: scenario, Side: "long", State: store.StateArmed,
		EntryPx: 29600, StopPx: 29590, TargetPx: 29620,
		Kind: "stop_entry", Condition: "PHL", ExpiryMs: expiryMs,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(1),
	}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
}

func TestMentorLevelArmLifetimeEndsAtTheEODFlat(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	for _, tc := range []struct {
		name      string
		h, m      int
		cancelled bool
	}{
		// Morning-stamped order, still before its own 15:00 expiry: the 14:45
		// NY close cancels it first.
		{"14:46 — past the NY flat, before the 15:00 stamp", 14, 46, true},
		// Authored inside the gap: its stamp is 15:00 TOMORROW, it dies now.
		{"15:30 — born in the 14:45–17:00 gap", 15, 30, true},
		// The 17:00 window opens (ASIA ships DISABLED and still counts): rests.
		{"17:05 — evening, a session window is open", 17, 5, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
			now := b3Clock(tc.h, tc.m)
			if _, ok := at.sessionRegistry(now).ActiveSession(now); ok == tc.cancelled {
				t.Fatalf("fixture: shipped registry active-session=%v at %02d:%02d", ok, tc.h, tc.m)
			}
			expiry := mentor.LevelArmExpiry(now.UnixMilli())
			if expiry <= now.UnixMilli() {
				t.Fatalf("fixture: level expiry %d not in the future", expiry)
			}
			seedLevelArm(t, at, ledger, "lvl-t1", expiry)

			at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})

			row := readArmRow(t, ledger, "lvl-t1")
			got := row.State == store.StateCancelled
			if got != tc.cancelled {
				t.Fatalf("level arm at %02d:%02d CT: cancelled=%v (state=%q reason=%q), want cancelled=%v",
					tc.h, tc.m, got, row.State, row.StateReason, tc.cancelled)
			}
			if tc.cancelled && !strings.Contains(row.StateReason, "session ended (EOD flat)") {
				t.Fatalf("cancel reason = %q, want the EOD-flat intraday mentor cancel", row.StateReason)
			}
		})
	}
}

// The guide's lifetime sentence names the registry's own times, never the
// false "15:00 the NEXT day". Mutant: restore the #21 sentence → RED.
func TestGuideMentorLevelLifetimeNamesTheEODFlat(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "web", "src", "guide", "content", "mentor.ts"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	i := strings.Index(src, "title: 'Entries become stop-LIMIT'")
	if i < 0 {
		t.Fatal("mentor.ts: the 'Entries become stop-LIMIT' card is gone — re-anchor this pin")
	}
	card := src[i:]
	if j := strings.Index(card, "cite:"); j > 0 {
		card = card[:j]
	}
	reg := kernel.DefaultSessionRegistry()
	var nyFlat, asiaOpen string
	for _, s := range reg.Sessions {
		switch s.Name {
		case kernel.SessionNY:
			nyFlat = s.FlatCT
		case kernel.SessionAsia:
			asiaOpen = s.WindowStartCT
		}
	}
	if nyFlat == "" || asiaOpen == "" {
		t.Fatalf("registry lost NY/ASIA: %+v", reg)
	}
	for _, want := range []string{
		"never lives past " + nyFlat + " CT",
		"from the NY close at " + nyFlat + " CT until the " + asiaOpen + " CT reopen",
		"rests until " + nyFlat + " CT the next trading day",
		"from 15:45 CT",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("guide lifetime card lacks %q (registry NY flat %s, ASIA open %s)", want, nyFlat, asiaOpen)
		}
	}
	for _, lie := range []string{"rests through to 15:00 the NEXT day", "RESTS until the NEXT 15:00 CT"} {
		if strings.Contains(card, lie) {
			t.Errorf("guide lifetime card still says %q — the EOD flat cancels it at %s", lie, nyFlat)
		}
	}
}
