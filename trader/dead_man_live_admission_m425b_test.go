package trader

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// m425b S5: admission must read the LIVE link + connection epoch, not only the
// dead-man state cached by the last ~60s cycle. The pins drive the production
// chain (admitEntry) with the watchdog NEVER stepped after the baseline.
// MUTANT: call at.deadMan.entriesBlocked() in admitChain again → the post-drop
// and post-reconnect admissions pass → RED.

type m425bLink struct {
	s    *ntwire.TCPServer
	conn net.Conn
}

func (l *m425bLink) dial(t *testing.T) {
	t.Helper()
	c, err := net.Dial("tcp", l.s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	l.conn = c
	waitAddonRegistered(t, l.s)
}

func (l *m425bLink) drop(t *testing.T) {
	t.Helper()
	_ = l.conn.Close()
	for deadline := time.Now().Add(3 * time.Second); l.s.IsConnected() && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if l.s.IsConnected() {
		t.Fatal("fixture: the server never saw the link drop")
	}
}

func m425bRig(t *testing.T) (*AutoTrader, *m425bLink) {
	t.Helper()
	at, st := resetTrader(t, store.StrategyConfig{})
	withMaintenanceDir(t)
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })
	l := &m425bLink{s: s}
	l.dial(t)
	broker := nttrader.NewTCPTrader(s, "MNQ", "Sim101")
	at.trader = broker
	at.exchange = "ninjatrader"
	_ = st
	return at, l
}

func m425bAdmit(at *AutoTrader) string {
	rec, _ := at.admitEntry(admitIntent{Path: admitArm, Symbol: "MNQ", Action: "open_long", Key: "m425b"})
	return rec
}

func TestM425bAdmissionSeesLiveLinkDrop(t *testing.T) {
	at, l := m425bRig(t)
	at.driveDeadManWatchdog() // baseline sample: link up, epoch recorded, state live
	if at.deadMan.entriesBlocked() {
		t.Fatal("fixture: watchdog must be live with the link up")
	}
	if rec := m425bAdmit(at); strings.Contains(rec, "dead_man") {
		t.Fatalf("control: a healthy link must not refuse on dead_man, got %q", rec)
	}
	l.drop(t) // NO watchdog step: the cached state still reads live
	if at.deadMan.entriesBlocked() {
		t.Fatal("fixture: the cached state must still be stale-live")
	}
	if rec := m425bAdmit(at); !strings.Contains(rec, "dead_man_watchdog") {
		t.Fatalf("a dropped link must refuse at admission even before the 60s cycle, got %q", rec)
	}
}

func TestM425bAdmissionSeesReplacedLinkEpoch(t *testing.T) {
	at, l := m425bRig(t)
	at.driveDeadManWatchdog()
	l.drop(t)
	l.dial(t) // drop AND reconnect inside the cycle: connected now, new epoch
	if at.deadMan.entriesBlocked() {
		t.Fatal("fixture: the cached state must still be stale-live")
	}
	if rec := m425bAdmit(at); !strings.Contains(rec, "dead_man_watchdog") {
		t.Fatalf("a link replaced between samples must refuse at admission, got %q", rec)
	}
}

// m427/8 — the haveEpoch guard in entriesBlockedLive. A FRESH watchdog has never
// sampled an epoch (w.epoch is 0, haveEpoch false): the first live epoch it is
// shown (7) is the baseline, not a "replaced link". Without the guard every
// entry was refused as dead_man from boot until the first ~60 s cycle.
// Positive control: once an epoch has been sampled, a different one blocks.
// Mutant: `if w.haveEpoch && epoch != w.epoch` -> `if epoch != w.epoch` -> RED.
func TestM427FreshWatchdogDoesNotBlockOnTheFirstEpoch(t *testing.T) {
	w := &deadManWatchdog{}
	if w.entriesBlockedLive(true, true, 7, true) {
		t.Fatal("a fresh watchdog (no epoch sampled yet) refused entries on the first live epoch 7")
	}
	// the first cycle samples epoch 7
	w.stepEpoch(true, 7, true, time.Now().UnixMilli(), func() bool { return true })
	if w.entriesBlockedLive(true, true, 7, true) {
		t.Fatal("same epoch after the baseline must not block")
	}
	if !w.entriesBlockedLive(true, true, 8, true) {
		t.Fatal("a different epoch after the baseline is a replaced link and must block")
	}
}
