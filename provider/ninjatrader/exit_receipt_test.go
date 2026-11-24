// look-twice A (spec 2026-11-24 section 3 A) — the EXIT fence, the twin of
// entry_receipt_test.go. Every frame test goes over a real dialed socket
// through the production read loop; the receive clock is injected so each frame
// has its own instant.

package ninjatrader

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"vl/discipline"
)

type exitFenceRig struct {
	t      *testing.T
	srv    *TCPServer
	client *MockTCPClient
	nowMs  atomic.Int64
	seq    int
}

var exitFenceT0 = time.Date(2026, 11, 19, 12, 47, 30, 0, time.UTC)

func newExitFenceRig(t *testing.T) *exitFenceRig {
	t.Helper()
	r := &exitFenceRig{t: t, srv: NewTCPServer(nil)}
	r.srv.SetReceiptClockForTest(func() time.Time {
		return exitFenceT0.Add(time.Duration(r.nowMs.Load()) * time.Millisecond)
	})
	addr := freeEphemeralAddr(t)
	r.srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := r.srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = r.srv.Stop() })
	r.client = NewMockTCPClient(addr, 50*time.Millisecond)
	if err := r.client.Start(ctx); err != nil {
		t.Fatalf("client Start: %v", err)
	}
	t.Cleanup(func() { r.client.Stop() })
	waitForConnected(t, r.srv, 2*time.Second)
	return r
}

// send writes one frame at ms and waits until the read loop has PROCESSED it: a
// positions frame for a private marker account follows it on the same stream.
func (r *exitFenceRig) send(ms int64, ft FrameType, payload any) {
	r.t.Helper()
	r.nowMs.Store(ms)
	if err := writeFromMock(r.client, ft, payload); err != nil {
		r.t.Fatalf("write %s: %v", ft, err)
	}
	r.seq++
	mark := PositionsPayload{Account: "marker", Positions: []OpenPosition{{Symbol: "MARK", Side: "long", Quantity: r.seq}}}
	if err := writeFromMock(r.client, FramePositions, mark); err != nil {
		r.t.Fatalf("write marker: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if g, ok := r.srv.PositionsFor("marker"); ok && len(g) == 1 && g[0].Quantity == r.seq {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("frame %s at %d was never processed", ft, ms)
		}
		time.Sleep(time.Millisecond)
	}
}

func (r *exitFenceRig) ou(ms int64, name, state string, qty int) {
	r.t.Helper()
	sig := name
	for _, s := range exitLegSuffixes {
		if n := len(name) - len(s.suffix); n > 0 && name[n:] == s.suffix {
			sig = name[:n]
			break
		}
	}
	r.send(ms, FrameOrderUpdate, OrderUpdatePayload{SignalID: sig, OrderName: name, State: state,
		Quantity: qty, Symbol: "MNQ", Account: "Sim101"})
}

func (r *exitFenceRig) pc(ms int64, sig, reason string, leg, qty int, exitTime string) {
	r.t.Helper()
	r.send(ms, FramePositionClose, PositionClosePayload{SignalID: sig, Symbol: "MNQ", PositionSide: "short",
		ExitPrice: 27751.25, Quantity: qty, ExitReason: reason, ExitTime: exitTime, Leg: leg, Account: "Sim101"})
}

// want asserts the fence of Sim101/MNQ: ms, or -1 for "never stamped".
func (r *exitFenceRig) want(ms int64, why string) {
	r.t.Helper()
	got := r.srv.ExecutionReceiptsFor("Sim101", "MNQ").Exit
	if ms < 0 {
		if !got.IsZero() {
			r.t.Fatalf("%s: exit fence = +%dms, want never stamped", why, got.Sub(exitFenceT0).Milliseconds())
		}
		return
	}
	if want := exitFenceT0.Add(time.Duration(ms) * time.Millisecond); !got.Equal(want) {
		if got.IsZero() {
			r.t.Fatalf("%s: exit fence never stamped, want +%dms", why, ms)
		}
		r.t.Fatalf("%s: exit fence = +%dms, want +%dms", why, got.Sub(exitFenceT0).Milliseconds(), ms)
	}
}

// Every exit leg the AddOn names, filled or part-filled, stamps from order_update.
func TestExitFenceEveryLegName(t *testing.T) {
	r := newExitFenceRig(t)
	ms := int64(10)
	for _, suf := range []string{"-sl", "-sl2", "-sl3", "-tp", "-tp2", "-tp3", "-lx"} {
		for _, st := range []string{"partfilled", "filled"} {
			qty := 1
			if st == "filled" {
				qty = 2
			}
			r.ou(ms, "sig"+suf, st, qty)
			r.want(ms, suf+" "+st)
			ms += 10
		}
	}
}

// Nothing that is not an exit FILL moves it.
func TestExitFenceNotAnExitFill(t *testing.T) {
	r := newExitFenceRig(t)
	r.ou(10, "sig", "filled", 3)       // the entry
	r.ou(20, "sig-sl", "working", 0)   // a resting stop
	r.ou(30, "sig-sl", "cancelled", 0) // a cancelled stop
	r.ou(40, "sig-tp", "filled", 0)    // no quantity
	r.ou(50, "Close", "filled", 3)     // fenced from position_close only
	r.ou(60, "-sl", "filled", 3)       // a suffix with no signal
	r.send(70, FrameOrderUpdate, OrderUpdatePayload{SignalID: "sig", OrderName: "sig-sl", State: "filled",
		Quantity: 3, Symbol: "MNQ", Account: "Sim101", CancelReport: true})
	// With the cancel-report regime ON the echo passes the read loop's drop.
	r.srv.noteExitOrderUpdate(OrderUpdatePayload{SignalID: "sig", OrderName: "sig-sl", State: "filled",
		Quantity: 3, Symbol: "MNQ", Account: "Sim101", CancelReport: true})
	r.send(75, FrameOrderUpdate, OrderUpdatePayload{SignalID: "sig", OrderName: "sig-sl", State: "filled",
		Quantity: 3, Symbol: "MES", Account: "Sim101"})
	r.send(76, FramePositionClose, PositionClosePayload{SignalID: "Close", Symbol: "MES", Quantity: 3,
		ExitReason: "manual", ExitTime: "t0", Account: "Sim101"})
	r.send(77, FramePositionClose, PositionClosePayload{SignalID: "Close", Symbol: "MNQ", Quantity: 0,
		ExitReason: "manual", ExitTime: "t0", Account: "Sim101"})
	r.send(80, FrameOrderUpdate, OrderUpdatePayload{SignalID: "sig", OrderName: "sig-sl", State: "filled",
		Quantity: 3, Symbol: "MNQ", Account: "SimAccount12"})
	r.send(90, FrameOrderUpdate, OrderUpdatePayload{SignalID: "sig", OrderName: "sig-sl", State: "filled",
		Quantity: 3, Symbol: "MNQ", Account: ""})
	r.want(-1, "no exit fill on Sim101/MNQ")
	if g := r.srv.ExecutionReceiptsFor("SimAccount12", "MNQ").Exit; g.IsZero() {
		t.Fatal("the other account's exit must fence the other account")
	}
	if g := r.srv.ExecutionReceiptsFor("SimAccount12", "ES").Exit; !g.IsZero() {
		t.Fatal("another symbol must not be fenced")
	}
}

// One fill, one stamp: the position_close of a counted fill does not move the
// fence past a positions frame that arrived between the two frames.
func TestExitFenceCloseOfSameFillDoesNotMove(t *testing.T) {
	for _, c := range []struct {
		name, order, reason string
		leg                 int
	}{
		{"single stop (leg 0)", "sig-sl", "sl", 0}, {"split leg 1", "sig-sl", "sl", 1},
		{"leg 2", "sig-sl2", "sl", 2}, {"leg 3 target", "sig-tp3", "tp", 3}, {"limit exit", "sig-lx", "limit", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newExitFenceRig(t)
			r.ou(100, c.order, "partfilled", 1)
			r.ou(110, c.order, "filled", 2)
			r.send(120, FramePositions, PositionsPayload{Account: "Sim101", Positions: []OpenPosition{}})
			r.pc(130, "sig", c.reason, c.leg, 2, "t1")
			r.want(110, "the close of the fill already stamped")
			if !r.srv.ExecutionReceiptsFor("Sim101", "MNQ").PositionsAfterFills() {
				t.Fatal("the flat frame between the two frames must stay newer than the fence")
			}
			r.pc(140, "sig", c.reason, c.leg, 2, "t1")
			r.ou(150, c.order, "filled", 2)
			r.ou(160, c.order, "partfilled", 1)
			r.want(110, "replays of both frames")
		})
	}
}

// Either frame alone is enough, and a late twin of a close-only fill is a replay.
func TestExitFenceEitherFrameAlone(t *testing.T) {
	r := newExitFenceRig(t)
	r.pc(100, "sig", "sl", 1, 2, "t1")
	r.want(100, "position_close alone")
	r.ou(110, "sig-sl", "filled", 2)
	r.want(100, "its own late order_update")
	r.ou(120, "sig-sl2", "partfilled", 1)
	r.want(120, "leg 2 part fill")
	// order_update "filled" lost: the close carries MORE than was counted.
	r.pc(130, "sig", "sl", 2, 2, "t2")
	r.want(130, "the close of an uncounted rest")
}

// "Close" and hand exits are fenced from position_close, once each.
func TestExitFenceCloseOrderName(t *testing.T) {
	r := newExitFenceRig(t)
	r.pc(100, "Close", "manual", 0, 3, "t1")
	r.want(100, "first Close")
	r.pc(110, "Close", "manual", 0, 3, "t1")
	r.want(100, "the same frame again")
	r.pc(120, "Close", "manual", 0, 3, "t2")
	r.want(120, "a second Close of the same size")
}

// A reused "-sl" name (place_protective_stop) is a new fill: from order_update
// once its own working state was seen, and from position_close when it was not.
func TestExitFenceReusedStopName(t *testing.T) {
	r := newExitFenceRig(t)
	r.ou(100, "sig-sl", "filled", 2)
	r.pc(105, "sig", "sl", 0, 2, "t1")
	r.ou(200, "sig-sl", "working", 0)
	r.want(100, "placing the replacement is not a fill")
	r.ou(300, "sig-sl", "filled", 1)
	r.want(300, "the replacement's smaller fill")
	r.pc(305, "sig", "sl", 0, 1, "t2")
	r.want(300, "the replacement's close pairs with it")
	// two same-name stops live at once (10-16): the AddOn drops the second
	// order_update, so position_close is the only evidence.
	r.pc(400, "sig", "sl", 0, 1, "t3")
	r.want(400, "a second close with its own exit_time")
	// a part-filled, then cancelled order is over too.
	r.ou(500, "sig-sl2", "partfilled", 1)
	r.ou(510, "sig-sl2", "cancelled", 1)
	r.ou(520, "sig-sl2", "working", 0)
	r.ou(530, "sig-sl2", "partfilled", 1)
	r.want(530, "the replacement of a part-filled, cancelled stop")
}

// A close that did not arrive must not swallow a much later fill of the name.
func TestExitFencePairWindow(t *testing.T) {
	r := newExitFenceRig(t)
	r.ou(0, "sig-sl", "filled", 2)
	r.pc(20_000, "sig", "sl", 0, 2, "t9")
	r.want(20_000, "past the pair window the close stamps")
	r.ou(30_000, "sig-tp", "filled", 2)
	r.pc(30_001, "sig", "tp", 0, 1, "t10")
	r.want(30_001, "a close of another size is another fill, even inside the window")
}

// A close whose identity does not verify is not RECORDED, but it is a fill.
func TestExitFenceStampsBeforeVerifyInbound(t *testing.T) {
	discipline.ResetFreezeForTest()
	t.Cleanup(discipline.ResetFreezeForTest)
	r := newExitFenceRig(t)
	seq := r.srv.assignSeqRegister("t-fence-a", "Sim101", "sig")
	r.send(100, FramePositionClose, PositionClosePayload{SignalID: "sig", Symbol: "MNQ", PositionSide: "short",
		Quantity: 2, ExitReason: "sl", ExitTime: "t1", Account: "Sim101", TraderID: "t-somebody-else", Seq: seq})
	r.want(100, "a close refused by the identity check")
	if _, frozen := discipline.IsFrozen("t-fence-a"); !frozen {
		t.Fatal("the fixture must really be an identity mismatch")
	}
	if _, pending := r.srv.lookupPending(seq, "sig"); !pending {
		t.Fatal("a mismatched close must still be refused (its op was retired)")
	}
}

// The fence must not leak into the readers that serve NT8 truth today, and it
// survives a new adapter/connection because it lives on the server.
func TestExitFenceLeavesPositionsReadersAlone(t *testing.T) {
	r := newExitFenceRig(t)
	r.send(100, FramePositions, PositionsPayload{Account: "Sim101",
		Positions: []OpenPosition{{Symbol: "MNQ", Side: "short", Quantity: 1, AvgPrice: 27733}}})
	r.ou(200, "sig-sl", "filled", 1)
	g, received, entry, ok := r.srv.PositionsForExecutionReceipt("Sim101", "MNQ")
	if !ok || len(g) != 1 || !entry.IsZero() || !received.Equal(exitFenceT0.Add(100*time.Millisecond)) {
		t.Fatalf("PositionsForExecutionReceipt changed: %+v received=%v entry=%v ok=%v", g, received, entry, ok)
	}
	e := r.srv.ExecutionReceiptsFor("Sim101", "MNQ")
	if e.PositionsAfterFills() {
		t.Fatal("a positions frame older than the exit fill must not read as settled")
	}
	r.send(200, FramePositions, PositionsPayload{Account: "Sim101"})
	if r.srv.ExecutionReceiptsFor("Sim101", "MNQ").PositionsAfterFills() {
		t.Fatal("the SAME instant is not proof of after")
	}
	r.send(201, FramePositions, PositionsPayload{Account: "Sim101"})
	if !r.srv.ExecutionReceiptsFor("Sim101", "MNQ").PositionsAfterFills() {
		t.Fatal("a newer frame settles it")
	}
	r.ou(300, "sig9", "filled", 1) // a real entry
	if r.srv.ExecutionReceiptsFor("Sim101", "MNQ").PositionsAfterFills() {
		t.Fatal("the entry fence counts too")
	}
	if (ExecutionReceipts{}).PositionsAfterFills() || r.srv.ExecutionReceiptsFor("", "MNQ").PositionsAfterFills() {
		t.Fatal("no positions frame is never settled")
	}
}
