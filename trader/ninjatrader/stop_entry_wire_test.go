package ninjatrader

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ENTRY-MECHANICS E7 (2026-08-30) — stop-entry orders. Far-side frame law:
// the stop_entry frame is PROVEN by receiving it on the loopback TCP server
// (the same harness the move_stop wire proof uses) — order_type=stop_entry,
// stop_price = the rounded trigger, identity stamp intact. The C# AddOn's
// parse of the new fields is additive (old Go never sends it); the real NT8
// far-side proof is the D-rule cutover gate.

func stopEntryServer(t *testing.T) (*ntwire.TCPServer, *store.Store, net.Conn, chan ntwire.SignalPayload) {
	st, err := store.New(filepath.Join(t.TempDir(), "se.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	// The tradeable guard needs Sim101 registered as SIM.
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel(); _ = st.Close() })

	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	frames := make(chan ntwire.SignalPayload, 4)
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			if env.Type != ntwire.FrameSignal {
				continue
			}
			var p ntwire.SignalPayload
			if json.Unmarshal(env.Payload, &p) == nil {
				frames <- p
			}
		}
	}()
	return s, st, conn, frames
}

// TestPlaceStopEntryFrameOnLoopback — twin long/short: the stop-entry signal
// frame arrives with order_type=stop_entry + stop_price and the identity stamp.
func TestPlaceStopEntryFrameOnLoopback(t *testing.T) {
	for _, tc := range []struct {
		side    string
		trigger float64
		sl      float64
		tp      float64
	}{
		{"long", 29670.50, 29650.00, 29720.00},
		{"short", 29400.25, 29420.75, 29350.00},
	} {
		s, st, conn, frames := stopEntryServer(t)

		// Capability handshake: the far side proves it will BUILD the order
		// correctly by reporting its build id on the heartbeat. The floor moved
		// from FarSideBuildE7 (parse-proven, 2026-08-30) to MinAddonBuildStopSlot
		// (slot-proven, WAVE B 2026-09-05) — seeded BY IMPORT, never as a literal
		// copy of the constant (A24).
		if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: ntwire.MinAddonBuildStopSlot}); err != nil {
			t.Fatalf("write heartbeat: %v", err)
		}
		for i := 0; i < 100 && !ntwire.FarSideProven(s.FarSideBuildID(), ntwire.MinAddonBuildStopSlot); i++ {
			time.Sleep(10 * time.Millisecond)
		}

		tr := NewTCPTrader(s, "MNQ", "Sim101")
		tr.mu.Lock()
		tr.st = st
		tr.mu.Unlock()

		var sid string
		var err error
		for i := 0; i < 50; i++ {
			sid, err = tr.PlaceStopEntry("MNQ", tc.side, 1, tc.trigger, tc.sl, tc.tp, 0, 0)
			if err == nil || !strings.Contains(err.Error(), "no NT client connected") {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err != nil {
			t.Fatalf("%s: place stop-entry failed: %v", tc.side, err)
		}
		select {
		case p := <-frames:
			if p.OrderType != "stop_entry" {
				t.Fatalf("%s: order_type=%q, want stop_entry", tc.side, p.OrderType)
			}
			if p.StopPrice != tc.trigger {
				t.Fatalf("%s: stop_price=%.2f, want %.2f", tc.side, p.StopPrice, tc.trigger)
			}
			// E1 — the trigger travels in stop_price and the limit slot is EMPTY,
			// on both sides. The AddOn selects its CreateOrder arguments from
			// exactly these two fields, so a trigger that leaked into limit_price
			// would rebuild the 2026-09-04 defect from the Go end.
			if p.LimitPrice != 0 {
				t.Fatalf("%s: limit_price=%.2f on a stop entry, want 0", tc.side, p.LimitPrice)
			}
			if p.SignalID != sid {
				t.Fatalf("%s: signal_id mismatch", tc.side)
			}
			if p.Account != "Sim101" || p.TraderID != tr.traderID {
				t.Fatalf("%s: identity stamp missing: acct=%q trader=%q", tc.side, p.Account, p.TraderID)
			}
			if p.StopLoss != tc.sl || p.TakeProfit != tc.tp {
				t.Fatalf("%s: bracket wrong: SL %.2f TP %.2f", tc.side, p.StopLoss, p.TakeProfit)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s: no stop_entry frame arrived on the loopback", tc.side)
		}
	}
}

// TestPlaceStopEntryRefusedWithoutFarSideBuild — the capability handshake:
// before the far side reports a build_id ≥ MinAddonBuildStopSlot, NO stop_entry frame
// may leave the wire (the 2026-08-30 incident: an old AddOn executed the frame
// as MARKET).
func TestPlaceStopEntryRefusedWithoutFarSideBuild(t *testing.T) {
	s, st, conn, frames := stopEntryServer(t)
	_ = conn

	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.mu.Lock()
	tr.st = st
	tr.mu.Unlock()

	var err error
	for i := 0; i < 50; i++ {
		_, err = tr.PlaceStopEntry("MNQ", "short", 1, 28700.00, 28850.00, 28500.00, 0, 0)
		if err == nil || !strings.Contains(err.Error(), "no NT client connected") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err == nil {
		t.Fatal("stop-entry was sent without a far-side build id — the capability gate is broken")
	}
	// TRACEABILITY PIN (B-rules, FIX-P1A): NO build id is "far side not proven
	// since reconnect" — the ErrFarSideNotProven sentinel, never
	// ErrAddonBuildTooOld (that one requires a REPORTED old build). A mutation
	// reverting the reason text or the sentinel fails these assertions.
	if !errors.Is(err, ntwire.ErrFarSideNotProven) {
		t.Fatalf("a no-proof refusal must wrap ErrFarSideNotProven: %v", err)
	}
	if errors.Is(err, ntwire.ErrAddonBuildTooOld) {
		t.Fatalf("a no-proof refusal must NOT read as addon-build-too-old: %v", err)
	}
	if !strings.Contains(err.Error(), "far side not proven since reconnect") {
		t.Fatalf("wrong refusal: %v", err)
	}

	// No signal frame may have reached the wire.
	select {
	case p := <-frames:
		t.Fatalf("a frame leaked to the wire despite the gate: %+v", p)
	case <-time.After(200 * time.Millisecond):
		// expected silence
	}

	// An OLD build id (pre-E7) is equally refused.
	if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: "2026-08-20"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100 && s.FarSideBuildID() == ""; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	for i := 0; i < 50; i++ {
		_, err = tr.PlaceStopEntry("MNQ", "short", 1, 28700.00, 28850.00, 28500.00, 0, 0)
		if err == nil || !strings.Contains(err.Error(), "no NT client connected") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err == nil || !strings.Contains(err.Error(), "does not prove stop_entry support") {
		t.Fatalf("old build id should refuse stop-entry: %v", err)
	}
}

// TestStopEntryFrameIsAdditiveJSON — a pre-E7 Go binary never emits the new
// fields; the limit frame must remain byte-shape-identical apart from its own
// fields (backward-compat proof).
func TestStopEntryFrameIsAdditiveJSON(t *testing.T) {
	lim := ntwire.SignalPayload{Symbol: "MNQ", Side: "long", Quantity: 1,
		Entry: 100.25, StopLoss: 99.00, TakeProfit: 102.00, SignalID: "s1",
		OrderType: "limit", LimitPrice: 100.25}
	b1, _ := json.Marshal(lim)
	var m1 map[string]any
	_ = json.Unmarshal(b1, &m1)
	if _, has := m1["stop_price"]; has {
		t.Fatal("a limit frame must NOT carry stop_price")
	}
	if _, has := m1["limit_price"]; !has {
		t.Fatal("limit frame must carry limit_price")
	}
	se := lim
	se.OrderType = "stop_entry"
	se.StopPrice = 101.00
	se.LimitPrice = 0
	b2, _ := json.Marshal(se)
	var m2 map[string]any
	_ = json.Unmarshal(b2, &m2)
	if m2["order_type"] != "stop_entry" || m2["stop_price"] != 101.00 {
		t.Fatalf("stop_entry frame malformed: %s", b2)
	}
}

// TestStopEntryRefusedOnPreStopSlotBuild — WAVE B / E6. FarSideBuildE7
// (2026-08-30) proved only that the AddOn PARSED a stop_entry frame; it did NOT
// prove the trigger reached NT8's stopPrice slot. Every stop entry that build
// family ever sent went out as `Limit price=<trigger> Stop price=0` — 22 of 22
// lifetime submissions, 0 fills. An AddOn reporting a build below the stop-slot
// minimum must be REFUSED, never sent a frame it will mis-execute.
func TestStopEntryRefusedOnPreStopSlotBuild(t *testing.T) {
	s, st, conn, frames := stopEntryServer(t)

	if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: ntwire.FarSideBuildE7}); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}
	for i := 0; i < 100 && s.FarSideBuildID() == ""; i++ {
		time.Sleep(10 * time.Millisecond)
	}

	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.mu.Lock()
	tr.st = st
	tr.mu.Unlock()

	var err error
	for i := 0; i < 50; i++ {
		_, err = tr.PlaceStopEntry("MNQ", "short", 1, 29590.50, 29650.00, 29481.50, 0, 0)
		if err == nil || !strings.Contains(err.Error(), "no NT client connected") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err == nil {
		t.Fatal("a stop entry was sent to an AddOn that predates the stop-slot fix — it would go out with a ZERO trigger")
	}
	if !strings.Contains(err.Error(), "predates the stop-slot fix") {
		t.Fatalf("refusal must name the reason: %v", err)
	}
	if !strings.Contains(err.Error(), "build_id="+ntwire.FarSideBuildE7) {
		t.Fatalf("refusal must name the received build id: %v", err)
	}
	// THE SENTINEL IS THE CONTRACT, not the prose. armed_executor.go branches on
	// errors.Is(perr, ntwire.ErrAddonBuildTooOld) to count a build refusal apart
	// from a transport failure and to dedupe it; a %v instead of %w in the wrap
	// silently drops the whole counting path and leaves every armed leg
	// re-logging every cycle for the entire go-first window, with
	// /api/risk/gate-blocks reading zero.
	if !errors.Is(err, ntwire.ErrAddonBuildTooOld) {
		t.Fatalf("the refusal does not wrap ErrAddonBuildTooOld — the caller cannot count it: %v", err)
	}
	select {
	case p := <-frames:
		t.Fatalf("a frame leaked to the wire despite the stop-slot gate: %+v", p)
	case <-time.After(200 * time.Millisecond):
	}
}
