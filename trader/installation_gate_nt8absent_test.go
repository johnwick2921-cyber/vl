package trader

// UPDATER-NT8-CLOSED — the nt8_absent gate verdict (owner ruling 22:1x CT
// 09-27: NT8 closed = the safest moment to update). Production call site:
// InstallationGateStatus over the real leg closures. Each case breaks exactly
// one thing and names the absent leg that must fail. The mutations the CTO
// will run — drop the 60s window, drop each ledger leg, drop the revoke, drop
// the SIM check — must each fail one of these.

import (
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// newAbsentFixture: NT8 link down 90 s, hold "job-g", barrier engaged, one
// bound Sim101 account, flat ledger, no planner reads, no queued signals.
func newAbsentFixture(t *testing.T) (*gateFixture, *ntwire.TCPServer) {
	t.Helper()
	dir := withMaintenanceDir(t)
	at, st := resetTrader(t, store.StrategyConfig{})
	at.id = "gate-t1"
	s := ntwire.NewTCPServer(nil)
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	at.trader = ntTrader.NewTCPTrader(s, "MNQ", "Sim101")
	setHold(t, dir, "job-g")
	// F35 — the absent view's sim_accounts leg vouches from the PERSISTED
	// last frame (the wire is disconnected by definition), so the fixture
	// seeds the persisted row the ready shape needs.
	if err := st.NTAccountsLast().Upsert([]store.NTAccountLast{
		{Name: "Sim101", IsSim: true, Connection: "SimulationConnection", ReceivedAtMs: time.Now().UnixMilli()},
	}); err != nil {
		t.Fatal(err)
	}
	f := &gateFixture{dir: dir, st: st, loaded: map[string]*AutoTrader{at.id: at},
		wire: installationWire{
			Connected:          false,
			Rec:                ntwire.ConnectionRecord{AcceptSeq: 3, RemotePort: 50123},
			HasAck:             false,
			DisconnectedAt:     time.Now().Add(-90 * time.Second),
			HaveDisconnectedAt: true,
		}}
	prevW, prevC := installationWireView, installationTraderCutover
	installationWireView = func([]*AutoTrader) (installationWire, bool) { return f.wire, true }
	installationTraderCutover = func(*AutoTrader) []CutoverLeg {
		return []CutoverLeg{{N: 1, Name: "db_open_positions", Pass: true}, {N: 2, Name: "api_positions", Pass: true}, {N: 4, Name: "working_orders", Pass: true}}
	}
	t.Cleanup(func() { installationWireView, installationTraderCutover = prevW, prevC })
	return f, s
}

func absentLegOf(a *NT8AbsentView, name string) (InstallationGateLeg, bool) {
	if a == nil {
		return InstallationGateLeg{}, false
	}
	for _, l := range a.Legs {
		if l.Name == name {
			return l, true
		}
	}
	return InstallationGateLeg{}, false
}

func mustFailAbsent(t *testing.T, a *NT8AbsentView, name, want string) {
	t.Helper()
	if a == nil {
		t.Fatalf("nt8_absent missing entirely")
	}
	if a.Ready {
		t.Fatalf("nt8_absent READY although leg %s must fail: %+v", name, a.Legs)
	}
	l, ok := absentLegOf(a, name)
	if !ok {
		t.Fatalf("absent leg %s missing: %+v", name, a.Legs)
	}
	if l.Pass {
		t.Fatalf("absent leg %s passed; want fail (%s)", name, l.Detail)
	}
	if want != "" && !strings.Contains(l.Detail, want) {
		t.Fatalf("absent leg %s detail %q does not say %q", name, l.Detail, want)
	}
}

func TestInstallationGateNt8AbsentReadyWhenEveryLegPasses(t *testing.T) {
	f, _ := newAbsentFixture(t)
	g := f.run()
	if g.NT8Absent == nil {
		t.Fatal("nt8_absent missing when an NT8 trader exists")
	}
	a := g.NT8Absent
	if !a.Eligible || !a.Ready {
		t.Fatalf("absent view must be eligible and ready: %+v", a)
	}
	if a.LinkDownSince == "" {
		t.Fatal("link_down_since must be recorded")
	}
	for _, name := range []string{"hold", "go_drained", "in_flight_sends", "queued_signals", "planner_in_flight", "ledger_exposure", "sim_accounts", "db_open_positions"} {
		l, ok := absentLegOf(a, name)
		if !ok {
			t.Errorf("absent leg %s missing", name)
			continue
		}
		if l.Source == "" {
			t.Errorf("absent leg %s quotes no source", name)
		}
		if !l.Pass {
			t.Errorf("absent leg %s failed: %s", name, l.Detail)
		}
	}
	// the NORMAL verdict stays not-ready: with the AddOn down there is no ack
	if g.Ready {
		t.Fatalf("the normal gate must not be READY with the AddOn disconnected")
	}
}

func TestInstallationGateNt8AbsentLinkDownBelowWindowNotEligible(t *testing.T) {
	f, _ := newAbsentFixture(t)
	f.wire.DisconnectedAt = time.Now().Add(-30 * time.Second)
	g := f.run()
	if g.NT8Absent == nil || g.NT8Absent.Eligible {
		t.Fatalf("link down 30s must not be eligible: %+v", g.NT8Absent)
	}
	if g.NT8Absent.Legs != nil {
		t.Fatalf("an uncomputed leg list must be ABSENT, not []: %+v", g.NT8Absent.Legs)
	}
}

func TestInstallationGateNt8AbsentConnectedIsNotEligible(t *testing.T) {
	f, _ := newAbsentFixture(t)
	// a reconnect REVOKES the verdict (the accept clears the stamp) — never
	// grandfathered
	f.wire.Connected = true
	f.wire.Rec.Ack = goodCensusAck("job-g")
	f.wire.HasAck = true
	f.wire.AckAge = time.Second
	g := f.run()
	if g.NT8Absent == nil || g.NT8Absent.Eligible {
		t.Fatalf("a connected AddOn must not be eligible: %+v", g.NT8Absent)
	}
}

func TestInstallationGateNt8AbsentNoDisconnectStampNotEligible(t *testing.T) {
	f, _ := newAbsentFixture(t)
	f.wire.HaveDisconnectedAt = false // never measured → fail closed
	g := f.run()
	if g.NT8Absent == nil || g.NT8Absent.Eligible {
		t.Fatalf("without the server's disconnect stamp the verdict must not be eligible: %+v", g.NT8Absent)
	}
}

func TestInstallationGateNt8AbsentEachLedgerLegRefuses(t *testing.T) {
	t.Run("no hold", func(t *testing.T) {
		f, _ := newAbsentFixture(t)
		if err := store.ClearMaintenanceHold(f.dir, "job-g"); err != nil {
			t.Fatal(err)
		}
		mustFailAbsent(t, f.run().NT8Absent, "hold", "no hold")
	})
	t.Run("an entry send holds a permit", func(t *testing.T) {
		f, _ := newAbsentFixture(t)
		if err := store.ClearMaintenanceHold(f.dir, "job-g"); err != nil {
			t.Fatal(err)
		}
		release, ok := MaintenanceEntryPermit()
		if !ok {
			t.Fatal("fixture: permit refused before the hold")
		}
		defer release()
		setHold(t, f.dir, "job-g")
		g := f.run()
		mustFailAbsent(t, g.NT8Absent, "go_drained", "permit")
		mustFailAbsent(t, g.NT8Absent, "in_flight_sends", "in_flight_sends=1")
	})
	t.Run("signals queued on the wire", func(t *testing.T) {
		f, _ := newAbsentFixture(t)
		f.wire.Queued = 3
		mustFailAbsent(t, f.run().NT8Absent, "queued_signals", "queued_signals=3")
	})
	t.Run("a planner read is in flight", func(t *testing.T) {
		f, _ := newAbsentFixture(t)
		plannerReadInFlight.Store("gate-t1", true)
		defer plannerReadInFlight.Delete("gate-t1")
		mustFailAbsent(t, f.run().NT8Absent, "planner_in_flight", "plannerReadInFlight[gate-t1]")
	})
	t.Run("a picture send is pending", func(t *testing.T) {
		f, _ := newAbsentFixture(t)
		if _, _, err := f.st.PictureHtfClaim(&store.PictureHtfOpportunityDB{OppKey: "opp-1", TraderID: "someone", Stage: "confirmed"}); err != nil {
			t.Fatal(err)
		}
		if won, err := f.st.PictureHtfClaimSubmission("opp-1", "picture-htf-1"); err != nil || !won {
			t.Fatal(err)
		}
		mustFailAbsent(t, f.run().NT8Absent, "ledger_exposure", "picture")
	})
	t.Run("bound account is not SIM", func(t *testing.T) {
		f, _ := newAbsentFixture(t)
		// F35 — the absent view vouches from the PERSISTED frame, so the
		// non-SIM mutation must flip the persisted row (a non-SIM row stays
		// non-SIM) rather than the live list the wire no longer has.
		if err := f.st.NTAccountsLast().Upsert([]store.NTAccountLast{
			{Name: "Sim101", IsSim: false, Connection: "LiveConnection", ReceivedAtMs: time.Now().UnixMilli()},
		}); err != nil {
			t.Fatal(err)
		}
		mustFailAbsent(t, f.run().NT8Absent, "sim_accounts", "not SIM")
	})
	t.Run("the server vouches for no account", func(t *testing.T) {
		f, s := newAbsentFixture(t)
		// F35 — disconnected + no persisted row for the bound name = nothing
		// vouches for it (fail-closed), the persisted mirror of the old
		// "the live list vouches for no account" case: rebind to an account
		// name the last frame never carried.
		at := f.loaded["gate-t1"]
		at.trader = ntTrader.NewTCPTrader(s, "MNQ", "Sim999")
		mustFailAbsent(t, f.run().NT8Absent, "sim_accounts", "no persisted account")
	})
	t.Run("a trader bound to no account", func(t *testing.T) {
		f, s := newAbsentFixture(t)
		// rebind the fixture's trader with an empty bound account
		at := f.loaded["gate-t1"]
		at.trader = ntTrader.NewTCPTrader(s, "MNQ", "")
		mustFailAbsent(t, f.run().NT8Absent, "sim_accounts", "bound to no account")
	})
}

// P1 (CTO 1790571298302): the absent verdict must carry a position leg — the
// SAME store read CutoverGateStatus leg 1 uses. An OPEN trader_positions row
// with NT8 absent ≥60s and every other leg green fails the verdict and NAMES
// the row; a closed row leaves it ready.
// The READY verdict must NAME the db_open_positions leg. A mutant that drops
// the leg from absentLegs (and its closure, to keep the build) must fail
// here even when every remaining leg passes — the leg's presence is pinned,
// not just its behavior.
func TestInstallationGateNt8AbsentLegsIncludeDbOpenPositions(t *testing.T) {
	f, _ := newAbsentFixture(t)
	a := f.run().NT8Absent
	if a == nil || !a.Eligible || !a.Ready {
		t.Fatalf("fixture must be ready: %+v", a)
	}
	if _, ok := absentLegOf(a, "db_open_positions"); !ok {
		t.Fatalf("the ready verdict must name the db_open_positions leg: %+v", a.Legs)
	}
}

func TestInstallationGateNt8AbsentOpenPositionRefuses(t *testing.T) {
	f, _ := newAbsentFixture(t)
	if err := f.st.Position().CreateOpenPosition(&store.TraderPosition{TraderID: "gate-t1", Symbol: "MNQ", Side: "long"}); err != nil {
		t.Fatal(err)
	}
	a := f.run().NT8Absent
	mustFailAbsent(t, a, "db_open_positions", "open#")
	l, _ := absentLegOf(a, "db_open_positions")
	for _, want := range []string{"gate-t1", "MNQ", "long"} {
		if !strings.Contains(l.Detail, want) {
			t.Fatalf("the leg must name the row's trader, symbol and side: %q", l.Detail)
		}
	}
}

func TestInstallationGateNt8AbsentClosedRowStaysReady(t *testing.T) {
	f, _ := newAbsentFixture(t)
	if err := f.st.Position().CreateOpenPosition(&store.TraderPosition{TraderID: "gate-t1", Symbol: "MNQ", Side: "long"}); err != nil {
		t.Fatal(err)
	}
	rows, err := f.st.Position().GetOpenPositions("gate-t1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("fixture: %v rows=%d", err, len(rows))
	}
	if _, err := f.st.Position().ClosePosition(rows[0].ID, rows[0].EntryPrice, "test", 0, 0, "sync"); err != nil {
		t.Fatal(err)
	}
	g := f.run()
	if g.NT8Absent == nil || !g.NT8Absent.Eligible || !g.NT8Absent.Ready {
		t.Fatalf("a closed row must leave the absent verdict ready: %+v", g.NT8Absent)
	}
	l, ok := absentLegOf(g.NT8Absent, "db_open_positions")
	if !ok || !l.Pass {
		t.Fatalf("db_open_positions %+v", l)
	}
}
