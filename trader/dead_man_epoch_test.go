package trader

import (
	"context"
	"net"
	"testing"
	"time"

	"vl/store"

	ntwire "vl/provider/ninjatrader"
	nttrader "vl/trader/ninjatrader"
)

// ── release #22 OPS-SAFETY item 6 (EXECUTION F-3) ───────────────────────────
//
// Verified at base from the live log (read-only): the two forced link drops at
// 00:21:35 / 00:22:43 on 10-14 produced dead-man DOWN/UP/RESUMED lines for
// trader hoang only (vl_2026-10-13.log 53405 / 53767 / 53791). Trader 12345,
// holding #650, logged none: its watchdog compared ONE boolean once per
// decision cycle, and both the drop and the reconnect fell between samples.

// deadManLoopback is a real TCP server + TCPTrader with one dialed AddOn.
func deadManLoopback(t *testing.T) (*AutoTrader, *ntwire.TCPServer, *nttrader.TCPTrader, net.Conn) {
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
	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	waitAddonRegistered(t, s)
	broker := nttrader.NewTCPTrader(s, "MNQ", "Sim101")
	s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
	broker.StartCloseSync(at.id, "fixture", "ninjatrader", st)
	broker.SetOpenOrdersSource(at.ledgerOpenOrders) // production wiring (auto_trader.go): the reconcile probe reads it
	at.trader = broker
	at.config.NinjaTraderSymbol = "MNQ"
	return at, s, broker, conn
}

// reconnectUnseen closes the AddOn link and dials a new one, waiting until the
// server holds the NEW connection — all between two watchdog samples.
func reconnectUnseen(t *testing.T, s *ntwire.TCPServer, broker *nttrader.TCPTrader, old net.Conn) {
	t.Helper()
	rec0, _, _, _ := broker.MaintenanceView()
	_ = old.Close()
	for deadline := time.Now().Add(5 * time.Second); s.IsConnected(); {
		if time.Now().After(deadline) {
			t.Fatal("fixture: the server never saw the old link close")
		}
		time.Sleep(time.Millisecond)
	}
	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	waitAddonRegistered(t, s)
	for deadline := time.Now().Add(5 * time.Second); ; {
		if rec, _, _, _ := broker.MaintenanceView(); rec.AcceptSeq > rec0.AcceptSeq {
			// The AddOn's connect burst re-sends positions on the new link.
			s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture: the new link never got a new accept sequence")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestDeadManWatchdogSeesAReconnectBetweenTwoSamples — the runCycle driver:
// a disconnect + reconnect between two samples must block NEW entries until a
// clean reconciliation, exactly like a reconnect it saw.
func TestDeadManWatchdogSeesAReconnectBetweenTwoSamples(t *testing.T) {
	at, s, broker, conn := deadManLoopback(t)
	at.driveDeadManWatchdog()
	if at.deadMan.entriesBlocked() {
		t.Fatal("precondition: a live, never-dropped link must not block entries")
	}
	reconnectUnseen(t, s, broker, conn)
	if ev := at.driveDeadManWatchdog(); ev != wdReconnected {
		t.Fatalf("a link replaced between two samples must read as a reconnect, got event %d", ev)
	}
	if !at.deadMan.entriesBlocked() {
		t.Fatal("NEW entries must be BLOCKED after an unseen link gap, until a clean reconciliation")
	}
	if refusal, refused := at.admitEntry(admitIntent{Path: admitDecision, Symbol: "MNQ", Action: "open_long", Now: time.Now()}); !refused || refusal == "" {
		t.Fatalf("the admission chain must refuse a NEW entry after the unseen gap, got refused=%v %q", refused, refusal)
	}
	// The NEXT sample reconciles (never on the reconnect sample itself).
	if ev := at.driveDeadManWatchdog(); ev != wdResumed || at.deadMan.entriesBlocked() {
		t.Fatalf("a clean reconciliation on the next sample must resume entries, got event %d blocked=%v", ev, at.deadMan.entriesBlocked())
	}
}

// TestMonitorTickDrivesTheDeadManWatchdog — every trader's 60 s monitor beat
// samples the watchdog, so a trader whose decision cycle is blocked (or not
// cycling while it holds a position) still sees the gap.
func TestMonitorTickDrivesTheDeadManWatchdog(t *testing.T) {
	at, s, broker, conn := deadManLoopback(t)
	at.monitorTick(time.Now())
	if at.deadMan.entriesBlocked() {
		t.Fatal("precondition: a live link must not block entries")
	}
	reconnectUnseen(t, s, broker, conn)
	at.monitorTick(time.Now())
	if !at.deadMan.entriesBlocked() {
		t.Fatal("monitorTick must sample the dead-man watchdog: a link gap between two beats must block NEW entries")
	}
}
