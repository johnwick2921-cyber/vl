package trader

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
)

// RELEASE #22 CANCEL-SETTLE — review-1014 EXECUTION.md F-1/F-2/F-7 and
// PLANNER.md F2, re-verified at base 9accae11f before any fix:
//
//   - row 363 (data.db, read-only): state_reason carries "(ack timeout —
//     unconfirmed, settlement pass owns it)" after NT8's "✕ armed S1 cancelled
//     in NT8" (vl_2026-10-13.log:64700, 01:47:29 CT), cancel_attempts=8, settled
//     only at 02:05:00 by snapshot 110695;
//   - vl_2026-10-13.log:64701/65144/65539/65921/66218/66300/66322/66330 — the
//     UNACKED WARN for signal 7e614fac, every pass for 17 minutes.
//
// The drain missed NT8's report because the ordered-execution worker (W117 F2)
// owns the frame: the drain's advisory copy is a no-op (OrderedOwned), and the
// worker applies it only after the post-update order_snapshot. The drain only
// re-read the row when ANOTHER frame arrived, so it timed out on a row the
// worker had already written 'cancelled' — then re-sent the cancel and moved
// the row back to cancel_pending.

// r22ReplayRig is a REAL TCPServer + REAL ordered worker
// (installNTOrderedExecutions, the production install) + a fake AddOn that
// answers the first cancel with NT8's real frame order — order_update(s), THEN
// the order_snapshot the worker waits for — and answers nothing to a re-sent
// cancel (NT8 no longer has the order).
type r22ReplayRig struct {
	at      *AutoTrader
	st      *store.Store
	ledger  *store.ArmedOrderStore
	cancels atomic.Int32
	rowID   int64
	sig     string
}

func newR22ReplayRig(t *testing.T, traderID string) *r22ReplayRig {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "r22.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := ntwire.NewTCPServer(nil)
	srv.SetAddrForTest("127.0.0.1:0")
	srv.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	c, err := net.Dial("tcp", srv.ListenAddrForTest().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	waitAddonRegistered(t, srv)
	t.Cleanup(func() { _ = c.Close() })
	if err := ntwire.WriteFrame(c, ntwire.FrameHello, ntwire.HelloPayload{ProtocolVersion: ntwire.ProtocolVersion, Source: "vltrader-addon"}); err != nil {
		t.Fatal(err)
	}
	nt := ntTrader.NewTCPTrader(srv, "MNQ", "Sim101")
	rig := &r22ReplayRig{st: st, ledger: st.ArmedOrders(), sig: "7e614fac-0875-467a-a80b-6541113d269d"}
	rig.at = &AutoTrader{id: traderID, name: "12345-replay", store: st, exchange: "ninjatrader", trader: nt, exchangeID: "ex"}
	t.Cleanup(func() { armedSubs.Delete(traderID) })
	// Row 363's shape: ASIA v5 S1 SHORT limit 30815, a placed plan row.
	row := store.ArmedOrderDB{TraderID: traderID, PlanID: "2026-10-13:ASIA:" + traderID, Version: 5, Session: "ASIA",
		Scenario: "S1", Side: "SHORT", EntryPx: 30815, StopPx: 30854.97, TargetPx: 30725.67, State: store.StateWorking, SignalID: rig.sig}
	if err := rig.ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	rig.rowID = row.ID
	if err := rig.at.installNTOrderedExecutions(nt); err != nil {
		t.Fatalf("install ordered executions: %v", err)
	}
	t.Cleanup(func() {
		rig.at.orderedExecMu.Lock()
		if rig.at.orderedExecUnreg != nil {
			rig.at.orderedExecUnreg()
		}
		rig.at.orderedExecMu.Unlock()
	})
	go func() {
		for {
			env, err := ntwire.ReadFrame(c)
			if err != nil {
				return
			}
			if env.Type != ntwire.FrameCancelOrder {
				continue
			}
			var p ntwire.CancelOrderPayload
			if json.Unmarshal(env.Payload, &p) != nil || p.SignalID != rig.sig {
				continue
			}
			if rig.cancels.Add(1) != 1 {
				continue // NT8 no longer has the order: there is nothing to report
			}
			// snapshot 110624: CancelSubmitted, then Cancelled — order_update(s)
			// FIRST, the order_snapshot after (VLTraderTCPClient.cs ~1948/~1955).
			_ = ntwire.WriteFrame(c, ntwire.FrameOrderUpdate, ntwire.OrderUpdatePayload{SignalID: rig.sig, State: "cancelpending", Symbol: "MNQ", Account: "Sim101", Quantity: 1})
			_ = ntwire.WriteFrame(c, ntwire.FrameOrderUpdate, ntwire.OrderUpdatePayload{SignalID: rig.sig, State: "cancelled", Symbol: "MNQ", Account: "Sim101", Quantity: 1})
			time.Sleep(150 * time.Millisecond)
			_ = ntwire.WriteFrame(c, ntwire.FrameOrderSnapshot, ntwire.OrderSnapshotPayload{Account: "Sim101", BuildID: "test", Orders: []ntwire.NT8Order{}})
		}
	}()
	return rig
}

func r22SettledOffDrain(t *testing.T, st *store.Store) int {
	t.Helper()
	n, err := store.SystemCounter(st, syncCancelSettledOffDrainKey)
	if err != nil {
		t.Fatalf("read %s: %v", syncCancelSettledOffDrainKey, err)
	}
	return n
}

func r22TerminalRefusals(trader string) int {
	_, table := telemetry.GateBlockSnapshot()
	return table[trader][store.GateRequestCancelTerminal]
}

// F-1 — the #363 replay at the production call site (the dormant-plan sync
// cancel, armed_executor.go "plan lifecycle" branch → cancelArmedOrdersSyncNonMentor
// → the REAL TCP cancel + the REAL advisory stream). NT8 confirms; the row
// stays 'cancelled'; the cancel is sent ONCE; the drain counts it as settled
// (not UNACKED); the settled-off-drain counter records it; and the
// installation gate's ledger_exposure leg no longer sees the row (item 4).
func TestR22Replay363ConfirmedCancelStaysCancelled(t *testing.T) {
	t.Setenv("ARMED_CANCEL_ACK_TIMEOUT_MS", "500")
	telemetry.ResetGateBlocksForTest()
	t.Cleanup(telemetry.ResetGateBlocksForTest)
	rig := newR22ReplayRig(t, "r22-replay-363")
	before := r22SettledOffDrain(t, rig.st)

	n, unacked := rig.at.cancelArmedOrdersSyncNonMentor(`plan lifecycle "dormant"`)

	time.Sleep(300 * time.Millisecond) // a late re-send would land at the fake by now
	state, _ := rig.ledger.StateOf(rig.rowID)
	t.Logf("replay: state=%s cancels_sent=%d cancelled=%d unacked=%d", state, rig.cancels.Load(), n, unacked)
	if state != store.StateCancelled {
		t.Fatalf("NT8 confirmed the cancel, the ledger says %q — a confirmed cancel was resurrected (row 363)", state)
	}
	if got := rig.cancels.Load(); got != 1 {
		t.Fatalf("the cancel went out %d times — a cancel was re-sent for an order NT8 no longer had", got)
	}
	if n != 1 || unacked != 0 {
		t.Fatalf("(cancelled, unacked) = (%d, %d), want (1, 0) — a settled row was reported UNACKED", n, unacked)
	}
	if got := r22SettledOffDrain(t, rig.st) - before; got != 1 {
		t.Fatalf("%s moved by %d, want +1 — the settlement learned off the drain must be counted", syncCancelSettledOffDrainKey, got)
	}
	if got := r22TerminalRefusals(rig.at.id); got != 0 {
		t.Fatalf("the timeout branch asked the store to re-open a settled row %d time(s) — it must honour the state it read", got)
	}
	var row store.ArmedOrderDB
	if err := rig.ledger.DB().First(&row, rig.rowID).Error; err != nil {
		t.Fatal(err)
	}
	// #526 ruling (release #24): the sync sweep now stamps the bot's own
	// request AT THE SEND (store.StampCancelRequested), so the slot re-arms as
	// a bot cancel. The pin's subject is unchanged: the F-1 timeout branch must
	// never re-open the settled row through RequestCancel (attempts stay 0).
	if row.CancelAttempts != 0 || row.CancelRequestedAtMs <= 0 {
		t.Fatalf("a confirmed cancel carries the at-send request stamp and no RequestCancel attempt: attempts=%d requested_at=%d reason=%q", row.CancelAttempts, row.CancelRequestedAtMs, row.StateReason)
	}
	// Item 4 — the updater's preflight reads this exact leg.
	g := InstallationGateStatus(nil, rig.st)
	leg, ok := legOf(g, "ledger_exposure")
	if !ok || !leg.Pass {
		t.Fatalf("a row whose cancel NT8 confirmed must not count as exposure: %+v", leg)
	}
}

// F-1 — the post-drain read is honoured: the advisory stream is unavailable
// (src returns nil — a dead subscription the consumer self-heals next cycle),
// the ordered worker applies NT8's 'cancelled' while the cancel is in flight,
// and the timeout branch reads the row terminal. It must count it settled and
// never ask RequestCancel to re-open it. Anchors both mutants: the bare
// RequestCancel restored (refusal counter + UNACKED) and the guard dropped.
func TestR22SyncCancelTimeoutHonoursTheStateItRead(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	t.Cleanup(telemetry.ResetGateBlocksForTest)
	at, st := resetTrader(t, store.StrategyConfig{})
	ledger := st.ArmedOrders()
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "2026-10-13:ASIA:" + at.id, Version: 5, Session: "ASIA",
		Scenario: "S1", Side: "SHORT", EntryPx: 30815, StopPx: 30854.97, TargetPx: 30725.67, State: store.StateWorking, SignalID: "sig-r22-seam"}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	sends := 0
	at.armedSyncSeam = &armedSyncSeam{
		Cancel: func(sid string) error {
			sends++
			if sends == 1 {
				// The ordered worker's application of NT8's report (the closure
				// installNTOrderedExecutions registers calls exactly this).
				at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: sid, State: "cancelled", Symbol: "MNQ", Account: "Sim101", Quantity: 1}, ledger)
			}
			return nil
		},
		Stream:  func() <-chan ntwire.OrderUpdatePayload { return nil },
		Timeout: 50 * time.Millisecond,
	}
	before := r22SettledOffDrain(t, st)

	n, unacked := at.cancelArmedOrdersSync("session close — EOD flat")

	state, _ := ledger.StateOf(row.ID)
	if state != store.StateCancelled {
		t.Fatalf("the row NT8 cancelled reads %q after the timeout branch", state)
	}
	if n != 1 || unacked != 0 {
		t.Fatalf("(cancelled, unacked) = (%d, %d), want (1, 0)", n, unacked)
	}
	if got := r22TerminalRefusals(at.id); got != 0 {
		t.Fatalf("the timeout branch called RequestCancel on a row it had read terminal (%d refusal(s)) — it must honour the state it read", got)
	}
	if sends != 1 {
		t.Fatalf("cancel sent %d times, want 1", sends)
	}
	if got := r22SettledOffDrain(t, st) - before; got != 1 {
		t.Fatalf("%s moved by %d, want +1", syncCancelSettledOffDrainKey, got)
	}
}

// F-1 — the store refusal reaches the executor as a refusal, not a failed
// write: armLifecycleWrite must NOT fail the slot closed for it (a latch there
// would refuse the next placement on that plan/scenario/leg until some other
// write cleared it).
func TestR22TerminalRefusalDoesNotLatchTheSlot(t *testing.T) {
	resetArmedLedgerFailuresForTest()
	t.Cleanup(resetArmedLedgerFailuresForTest)
	at, st := resetTrader(t, store.StrategyConfig{})
	ledger := st.ArmedOrders()
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "2026-10-14:LONDON:" + at.id, Version: 1, Session: "LONDON",
		Scenario: "S1", Side: "LONG", EntryPx: 100, StopPx: 95, TargetPx: 110, State: store.StateCancelled, SignalID: "sig-r22-latch"}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	err := ledger.RequestCancel(row.ID, "gate changed", time.Now().UnixMilli())
	if !errors.Is(err, store.ErrArmCancelTerminal) {
		t.Fatalf("want the typed terminal refusal, got %v", err)
	}
	at.armLifecycleWrite("request_cancel(gate_changed)", row, err)
	if armSlotBlockedForPlacement(row) {
		t.Fatal("a refused terminal cancel latched the slot fail-closed — it is a refusal, not a failed write")
	}
}

// F-2 — NT8's positive per-order 'cancelled' report releases the one-entry
// latch's queued-entry marker at the order_update branch itself (N7: a
// positive cancel only). Before the fix the marker survived until the C8 45 s
// stale sweep, which then printed "NT8 never confirmed" for an entry NT8 had
// positively cancelled (vl_2026-10-13.log:41352→41359, 41955→41965,
// 42661→42662, 42803→42804, 42867→42868; the 17:50:00 queued_entry refusal of
// 404e5dbd at :22692 after its 17:47:00 cancel at :22631).
func TestR22OrderUpdateCancelledForgetsThePendingEntry(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	nt := at.armedTrader()
	sid, err := nt.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200)
	if err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	if !nt.HasPendingEntry(sid) {
		t.Fatal("precondition: the sent entry must be pending")
	}
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "2026-10-13:LONDON:" + at.id, Version: 1, Session: "LONDON", Scenario: "isb-1",
		Side: "LONG", EntryPx: 29100, StopPx: 29000, TargetPx: 29200, State: store.StateWorking, SignalID: sid}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	// The ordered worker's call (OrderedOwned is false on the worker's copy).
	at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: sid, State: "cancelled", Symbol: "MNQ", Account: "Sim101", Quantity: 1}, ledger)
	if state, _ := ledger.StateOf(row.ID); state != store.StateCancelled {
		t.Fatalf("precondition: the report must settle the row, got %q", state)
	}
	if nt.HasPendingEntry(sid) {
		t.Fatal("NT8 positively reported the entry cancelled, yet its queued-entry marker survives — the latch refuses the next arm until the 45 s sweep")
	}
}

// F-2 negative half (N7 "positive cancel only"): a cancel REQUEST is not a
// cancel — RequestCancel alone must leave the marker in place.
func TestR22CancelRequestAloneKeepsThePendingEntry(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	nt := at.armedTrader()
	sid, err := nt.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200)
	if err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "2026-10-13:LONDON:" + at.id, Version: 1, Session: "LONDON", Scenario: "isb-2",
		Side: "LONG", EntryPx: 29100, StopPx: 29000, TargetPx: 29200, State: store.StateWorking, SignalID: sid}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := ledger.RequestCancel(row.ID, "gate changed", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: sid, State: "cancelpending", Symbol: "MNQ", Account: "Sim101", Quantity: 1}, ledger)
	if !nt.HasPendingEntry(sid) {
		t.Fatal("an unconfirmed cancel cleared the queued-entry marker — the latch must stay fail-closed until NT8 reports")
	}
}
