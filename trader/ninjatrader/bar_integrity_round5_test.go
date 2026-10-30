package ninjatrader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
)

// m445 round 5 (DEF-A) — "clears by itself" must be a promise that holds. For a
// MENTOR verdict a hole that takes RTH minutes does not stop pausing when it
// leaves the 1500-minute window: the key-level leg picks it up until the minutes
// are measured filled or it leaves the 90-day reach. The instant the verdict
// prints (the refusal text, the owner push, /api/health clears_at) must be the
// one at which the pause REALLY ends.
func TestMentorRTHHoleClearsAtIsWhenTheMentorPauseReallyEnds(t *testing.T) {
	armIntegrity(t)
	at := func(mo time.Month, d, h, m int) time.Time {
		return time.Date(2026, mo, d, h, m, 0, 0, kernel.CTLocation())
	}
	holeTo := at(10, 27, 9, 30)
	t.Cleanup(SetBarHolesWithHolesForTest(at(10, 1, 17, 0).UnixMilli(), at(10, 29, 10, 0).UnixMilli(), []TestHole{
		{Symbol: "MNQ", Contract: "MNQ 12-26", FromMs: at(10, 27, 9, 0).UnixMilli(), ToMs: holeTo.UnixMilli()}, // RTH
	}))
	now := at(10, 27, 12, 0)
	v := IntegrityVerdictForMentor("MNQ", IntegrityIntradayOpenMin, now)
	if !v.Blocked || len(v.Holes) != 1 {
		t.Fatalf("the RTH hole blocks the mentor verdict: %+v", v)
	}
	promised := time.UnixMilli(v.Holes[0].ClearsAtMs)
	if want := holeTo.Add(IntegritySeedReachDays * 24 * time.Hour); !promised.Equal(want) {
		t.Fatalf("the promised instant must be the end of the 90-day key-level reach %s, not the earlier window exit: got %s",
			kernel.TableTimeCT(want), kernel.TableTimeCT(promised))
	}
	// days after the hole left the 1500-minute window, still blocked — and the
	// promise printed THEN is the same instant
	for _, later := range []time.Time{at(10, 29, 12, 0), at(10, 31, 12, 0), at(12, 1, 12, 0)} {
		l := IntegrityVerdictForMentor("MNQ", IntegrityIntradayOpenMin, later)
		if !l.Blocked || len(l.Holes) != 1 || l.Holes[0].ClearsAtMs != v.Holes[0].ClearsAtMs {
			t.Fatalf("at %s the promise must not move and the pause must stand: %+v", kernel.TableTimeCT(later), l)
		}
	}
	// the printed instant is exact: blocked a minute before, clear at it
	if b := IntegrityVerdictForMentor("MNQ", IntegrityIntradayOpenMin, promised.Add(-time.Minute)); !b.Blocked {
		t.Fatalf("one minute before the promised instant the pause stands: %+v", b)
	}
	if a := IntegrityVerdictForMentor("MNQ", IntegrityIntradayOpenMin, promised); a.Blocked && a.Class == IntegrityClassHole {
		t.Fatalf("at the promised instant the pause is over: %+v", a)
	}
	// the non-mentor verdict is unaffected: it still clears at the window exit
	nv := IntegrityVerdictFor("MNQ", IntegrityIntradayOpenMin, now)
	if !nv.Blocked || nv.Holes[0].ClearsAtMs >= promised.UnixMilli() {
		t.Fatalf("a non-mentor verdict clears at the window exit, long before the 90-day instant: %+v", nv)
	}
}

// DEF-B — the scope names what the gate refuses. An RTH hole older than the
// 1500-minute window and inside the 12000 one pauses the AI / planner path (deep)
// AND the mentor (key-level): no entry path is left, so the scope is all_entries
// and the banner must not say the mentor setups trade normally.
func TestScopeNamesEveryBlockedLegAndNeverUnderclaims(t *testing.T) {
	armIntegrity(t)
	at := func(mo time.Month, d, h, m int) time.Time {
		return time.Date(2026, mo, d, h, m, 0, 0, kernel.CTLocation())
	}
	hole := []TestHole{{Symbol: "MNQ", Contract: "MNQ 12-26", FromMs: at(10, 27, 9, 0).UnixMilli(), ToMs: at(10, 27, 9, 30).UnixMilli()}}
	t.Cleanup(SetBarHolesWithHolesForTest(at(10, 1, 17, 0).UnixMilli(), at(10, 31, 12, 0).UnixMilli(), hole))

	for _, now := range []time.Time{at(10, 29, 12, 0), at(10, 31, 12, 0)} {
		if m := IntegrityVerdictForMentor("MNQ", IntegrityIntradayOpenMin, now); !m.Blocked {
			t.Fatalf("setup %s: the mentor verdict must be blocked: %+v", kernel.TableTimeCT(now), m)
		}
		views, armed := TradingPausedSnapshot(now)
		if !armed || len(views) != 1 || !views[0].Paused {
			t.Fatalf("%s: the hole pauses: %+v", kernel.TableTimeCT(now), views)
		}
		v := views[0]
		if v.Scope != PausedScopeAll {
			t.Fatalf("%s: mentor AND deep legs are blocked, no path is left: scope = %q, want %q (%+v)", kernel.TableTimeCT(now), v.Scope, PausedScopeAll, v)
		}
		has := map[string]bool{}
		for _, s := range v.Scopes {
			has[s] = true
		}
		if !has[PausedScopeMentor] || !has[PausedScopeDeep] {
			t.Fatalf("%s: scopes must name every blocked leg, mentor_entries included: %v", kernel.TableTimeCT(now), v.Scopes)
		}
		// the printed clear time is the one the mentor leg really ends at
		if len(v.Reasons) != 1 || len(v.Reasons[0].ClearsAt) != 1 || !strings.Contains(v.Reasons[0].ClearsAt[0], "01-25 08:30 CT") {
			t.Fatalf("%s: the health reason must print the instant the pause really ends (01-25 08:30 CT): %+v", kernel.TableTimeCT(now), v.Reasons)
		}
	}
}

// The mentor leg alone (the hole is older than the deep window, so only the
// key-level leg and SWING4H hold it) is mentor_entries, and widest-scope ranks
// the legs without losing one.
func TestWidestPausedScopeRanksTheLegs(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{PausedScopeSwing}, PausedScopeSwing},
		{[]string{PausedScopeDeep, PausedScopeSwing}, PausedScopeDeep},
		{[]string{PausedScopeMentor, PausedScopeSwing}, PausedScopeMentor},
		{[]string{PausedScopeMentor, PausedScopeDeep}, PausedScopeAll},
		{[]string{PausedScopeDeep, PausedScopeMentor, PausedScopeSwing}, PausedScopeAll},
		{[]string{PausedScopeAll}, PausedScopeAll},
	}
	for _, c := range cases {
		if got := WidestPausedScope(c.in); got != c.want {
			t.Errorf("WidestPausedScope(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMentorLegAloneIsMentorEntries(t *testing.T) {
	armIntegrity(t)
	at := func(mo time.Month, d, h, m int) time.Time {
		return time.Date(2026, mo, d, h, m, 0, 0, kernel.CTLocation())
	}
	// 10-12 (Mon) 09:00-09:30 CT: older than the 12000 window at 10-29, inside 24720 and the 90-day reach
	t.Cleanup(SetBarHolesWithHolesForTest(at(10, 1, 17, 0).UnixMilli(), at(10, 29, 12, 0).UnixMilli(), []TestHole{
		{Symbol: "MNQ", Contract: "MNQ 12-26", FromMs: at(10, 12, 9, 0).UnixMilli(), ToMs: at(10, 12, 9, 30).UnixMilli()},
	}))
	views, _ := TradingPausedSnapshot(at(10, 29, 12, 0))
	if len(views) != 1 || views[0].Scope != PausedScopeMentor {
		t.Fatalf("an RTH hole beyond the AI/planner window pauses the mentor (and SWING4H) only: %+v", views)
	}
}

// Release #55 round 3 (review 4, D1): the data-truth scopes rank like the others,
// and the per-check mapping follows what the gate really obeys.
func TestWidestPausedScopeRanksTheDataTruthScopes(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{PausedScopeAI}, PausedScopeAI},
		{[]string{PausedScopeAI, PausedScopeSwing}, PausedScopeDeep},
		{[]string{PausedScopeMentor, PausedScopeAI}, PausedScopeAll},
		{[]string{PausedScopeTraders}, PausedScopeTraders},
		// a trader/account block next to ANY narrower block is "mixed": neither single
		// scope sentence is true of both (re-review P2)
		{[]string{PausedScopeTraders, PausedScopeSwing}, PausedScopeMixed},
		{[]string{PausedScopeTraders, PausedScopeMentor}, PausedScopeMixed},
		{[]string{PausedScopeTraders, PausedScopeAI}, PausedScopeMixed},
		{[]string{PausedScopeTraders, PausedScopeDeep}, PausedScopeMixed},
		{[]string{PausedScopeTraders, PausedScopeAll}, PausedScopeAll},
	}
	for _, c := range cases {
		if got := WidestPausedScope(c.in); got != c.want {
			t.Errorf("WidestPausedScope(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDataTruthPausedScopeMapping(t *testing.T) {
	cases := []struct {
		check           string
		readers, scopes []string
		want            string
	}{
		{"bad_print", nil, nil, PausedScopeAll},
		{"cross_frame", []string{"mentor"}, nil, PausedScopeMentor},
		{"cross_frame", []string{"ai"}, nil, PausedScopeAI},
		{"cross_frame", []string{"ai", "mentor"}, nil, PausedScopeAll},
		{"cross_frame", []string{"somebody_new"}, nil, PausedScopeAll},
		{"indicator_lookback_swing4h", nil, nil, PausedScopeSwing},
		{"broker_agreement", nil, []string{"acct:SIM101"}, PausedScopeTraders},
	}
	for _, c := range cases {
		if got := DataTruthPausedScope(c.check, c.readers, c.scopes); got != c.want {
			t.Errorf("DataTruthPausedScope(%s,%v,%v) = %q, want %q", c.check, c.readers, c.scopes, got, c.want)
		}
	}
}
