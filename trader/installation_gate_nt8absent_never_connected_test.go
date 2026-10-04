package trader

// UPDATER-NT8-CLOSED — a bot that booted with NT8 CLOSED and has never seen the
// AddOn connect (the 2026-10-04 07:27 install, job de4cf900).
//
// Before: the nt8_absent verdict measured the link-down from the closing
// connection's disconnect stamp, so a never-connected bot had no stamp, was
// never eligible, and preflight fell to the connected-world legs: trader_cutover
// leg 2 (api_positions) failed "NT8 account positions unknown: no account
// snapshot or confirmed entry fill" and the owner had to open NT8.
//
// Now: "no AddOn connection since the listener came up" is the measured link-down
// start, so the absent path becomes eligible after nt8AbsentMinLinkDown of bot
// uptime. Every other absent leg (hold, drained, no queued, no planner read,
// terminal-only ledger, SIM-only accounts, db_open_positions) is unchanged.
//
// Production call sites: InstallationGateStatus over the REAL wire view
// (installationWireView is NOT stubbed here), a REAL started TCPServer, and the
// REAL CutoverGateStatus legs (installationTraderCutover is NOT stubbed here).

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// newNeverConnectedFixture: hold "job-g", a started server (listener up for
// `uptime`, no AddOn ever connected), one bound Sim101 account, flat ledger.
func newNeverConnectedFixture(t *testing.T, uptime time.Duration) (*gateFixture, *ntwire.TCPServer) {
	t.Helper()
	dir := withMaintenanceDir(t)
	at, st := resetTrader(t, store.StrategyConfig{})
	at.id = "gate-t1"
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })
	s.SetListeningSinceForTest(time.Now().Add(-uptime))
	at.trader = ntTrader.NewTCPTrader(s, "MNQ", "Sim101")
	setHold(t, dir, "job-g")
	return &gateFixture{dir: dir, st: st, loaded: map[string]*AutoTrader{at.id: at}}, s
}

func traderCutoverFailure(g InstallationGate) (string, bool) {
	for _, l := range g.Legs {
		if strings.HasPrefix(l.Name, "trader_cutover:") && !l.Pass {
			return l.Detail, true
		}
	}
	return "", false
}

// The 07:27 shape: bot up 90 s, NT8 never connected.
//   - the normal gate is NOT ready and its trader_cutover leg fails exactly as
//     the owner saw (api_positions … NT8 account positions unknown) — the
//     reason preflight used to refuse;
//   - the absent path is now eligible and ready.
//
// Mutant: remove the never-connected seed (LinkDownSince's AcceptSeq==0 branch)
// → HaveDisconnectedAt stays false → this test goes RED.
func TestInstallationGateNt8AbsentNeverConnectedAfterUptimeIsEligible(t *testing.T) {
	f, _ := newNeverConnectedFixture(t, 90*time.Second)
	g := f.run()
	if g.Ready {
		t.Fatalf("the normal gate must not be READY with no AddOn connected: %+v", g.Legs)
	}
	detail, failed := traderCutoverFailure(g)
	if !failed || !strings.Contains(detail, "api_positions") || !strings.Contains(detail, "NT8 account positions unknown") {
		t.Fatalf("fixture: the normal trader_cutover leg must fail as at 07:27 (api_positions / NT8 account positions unknown), got %q (failed=%v)", detail, failed)
	}
	a := g.NT8Absent
	if a == nil {
		t.Fatal("nt8_absent missing although an NT8 trader exists")
	}
	if !a.Eligible {
		t.Fatalf("a bot up 90 s that never saw the AddOn connect must be absent-eligible: %+v", a)
	}
	if !a.Ready {
		t.Fatalf("every absent leg must pass in the never-connected shape: %+v", a.Legs)
	}
	if a.LinkDownSince == "" {
		t.Fatal("link_down_since must be recorded (the job file names it)")
	}
	if _, err := time.Parse(time.RFC3339Nano, a.LinkDownSince); err != nil {
		t.Fatalf("link_down_since %q is not an RFC3339 stamp: %v", a.LinkDownSince, err)
	}
}

// Uptime below the window: not eligible, legs ABSENT (not []), as before.
func TestInstallationGateNt8AbsentNeverConnectedBelowWindowNotEligible(t *testing.T) {
	f, _ := newNeverConnectedFixture(t, 30*time.Second)
	g := f.run()
	if g.NT8Absent == nil || g.NT8Absent.Eligible {
		t.Fatalf("a bot up 30 s must not be absent-eligible: %+v", g.NT8Absent)
	}
	if g.NT8Absent.Legs != nil {
		t.Fatalf("an uncomputed leg list must be ABSENT, not []: %+v", g.NT8Absent.Legs)
	}
}

// The seed never relaxes the other legs: a hold-less never-connected bot is not
// ready (the absent legs still run on their own evidence).
func TestInstallationGateNt8AbsentNeverConnectedStillNeedsTheHold(t *testing.T) {
	f, _ := newNeverConnectedFixture(t, 90*time.Second)
	if err := store.ClearMaintenanceHold(f.dir, "job-g"); err != nil {
		t.Fatal(err)
	}
	a := f.run().NT8Absent
	if a == nil || !a.Eligible {
		t.Fatalf("eligibility is the link measurement only: %+v", a)
	}
	mustFailAbsent(t, a, "hold", "no hold")
}

// Connected-then-lost keeps today's rule: the measured disconnect, NOT the
// (older) listener stamp. A bot up 10 minutes whose AddOn JUST dropped is not
// eligible until the link has been down 60 s.
func TestInstallationGateNt8AbsentConnectedThenLostKeepsTheDisconnectStamp(t *testing.T) {
	f, s := newNeverConnectedFixture(t, 10*time.Minute)
	c, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	waitAddonRegistered(t, s)
	if g := f.run(); g.NT8Absent == nil || g.NT8Absent.Eligible {
		t.Fatalf("a connected AddOn must not be eligible: %+v", g.NT8Absent)
	}
	_ = c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for s.IsConnected() {
		if time.Now().After(deadline) {
			t.Fatal("fixture: the server never noticed the AddOn closing")
		}
		time.Sleep(time.Millisecond)
	}
	var g InstallationGate
	for time.Now().Before(deadline) {
		g = f.run()
		if g.NT8Absent != nil && g.NT8Absent.LinkDownSince != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if g.NT8Absent == nil || g.NT8Absent.LinkDownSince == "" {
		t.Fatalf("the closed connection must carry a measured link-down start: %+v", g.NT8Absent)
	}
	if g.NT8Absent.Eligible {
		t.Fatalf("the AddOn dropped a moment ago: the 10-minute listener stamp must not make it eligible: %+v", g.NT8Absent)
	}
	since, err := time.Parse(time.RFC3339Nano, g.NT8Absent.LinkDownSince)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(since) > 30*time.Second {
		t.Fatalf("link_down_since %v is the old listener stamp, not the disconnect", since)
	}
}
