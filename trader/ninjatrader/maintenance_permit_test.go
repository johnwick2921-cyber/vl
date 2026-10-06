package ninjatrader

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── W-ONE-BUTTON M2 site 4 — the broker-layer entry permit ─────────────────
//
// The hold is enforced at the three ENTRY functions (placeEntry,
// PlaceLimitEntry, PlaceStopEntry) and NEVER at the
// protective / management paths (PlaceProtectiveStop, CancelOrder,
// ModifyBracket, MoveStopToBreakeven, CloseLong/CloseShort) — CTO correction C1.

// allFramesServer is a started loopback server whose client records EVERY
// frame type the server writes, and proves the highest capability floor the
// entry/protective paths require (so no refusal below is a build refusal).
func allFramesServer(t *testing.T) (*ntwire.TCPServer, chan ntwire.FrameType) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "mp.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
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
	frames := make(chan ntwire.FrameType, 64)
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			frames <- env.Type
		}
	}()
	top := ntwire.MinAddonBuildPictureHtf // the highest floor any path below needs
	if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: top}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200 && !ntwire.FarSideProven(s.FarSideBuildID(), top); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !ntwire.FarSideProven(s.FarSideBuildID(), top) {
		t.Fatal("fixture: far-side build never proven")
	}
	drain(frames)
	return s, frames
}

func drain(c chan ntwire.FrameType) {
	for {
		select {
		case <-c:
		default:
			return
		}
	}
}

// waitFrame waits (bounded) for a frame of type want; other types are skipped.
func waitFrame(c chan ntwire.FrameType, want ntwire.FrameType, d time.Duration) bool {
	deadline := time.After(d)
	for {
		select {
		case f := <-c:
			if f == want {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func refusePermit() (func(), bool) { return nil, false }

func armedTrader(t *testing.T, s *ntwire.TCPServer) *TCPTrader {
	t.Helper()
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	_ = tr.SetStopLoss("MNQ", "long", 1, 29000)
	_ = tr.SetTakeProfit("MNQ", "long", 1, 29200)
	return tr
}

// All three entry functions refuse while the permit refuses: the error wraps
// ErrMaintenanceHold, the ledger callback (beforeSend) never runs, and no
// signal frame reaches the wire.
func TestEntryPermitRefusedBlocksAllFourEntryFunctions(t *testing.T) {
	s, frames := allFramesServer(t)
	tr := armedTrader(t, s)
	tr.SetEntryPermit(refusePermit)
	var stamped atomic.Int32
	stamp := func(string) error { stamped.Add(1); return nil }

	calls := map[string]func() error{
		"placeEntry(OpenLong)": func() error { _, err := tr.OpenLong("MNQ", 1, 1); return err },
		"PlaceLimitEntry": func() error {
			_, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200, stamp)
			return err
		},
		"PlaceStopEntry": func() error {
			_, err := tr.PlaceStopEntry("MNQ", "long", 1, 29150, 29000, 29300, 0, 0, stamp)
			return err
		},
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, ErrMaintenanceHold) {
			t.Fatalf("%s: want an ErrMaintenanceHold refusal, got %v", name, err)
		}
	}
	if stamped.Load() != 0 {
		t.Fatalf("beforeSend ran %d time(s) under a refused permit — a ledger row was moved for an entry that never sent", stamped.Load())
	}
	if waitFrame(frames, ntwire.FrameSignal, 150*time.Millisecond) {
		t.Fatal("a signal frame reached the wire under a refused permit")
	}
}

// The refusal sits BEFORE the B3 dedupe guard: once the hold lifts, the very
// same entry is admitted (a refusal that consumed the dedupe slot would drop
// the legitimate retry inside the same bar).
func TestEntryPermitRefusalDoesNotConsumeTheDedupeSlot(t *testing.T) {
	s, frames := allFramesServer(t)
	tr := armedTrader(t, s)
	tr.SetEntryPermit(refusePermit)
	if _, err := tr.OpenLong("MNQ", 1, 1); !errors.Is(err, ErrMaintenanceHold) {
		t.Fatalf("held: %v", err)
	}
	tr.SetEntryPermit(func() (func(), bool) { return func() {}, true })
	if _, err := tr.OpenLong("MNQ", 1, 1); err != nil {
		t.Fatalf("after the hold lifts the same entry must be admitted, got %v", err)
	}
	if !waitFrame(frames, ntwire.FrameSignal, 2*time.Second) {
		t.Fatal("the admitted entry never reached the wire")
	}
}

// The permit spans the wire write: by the time release runs, the signal frame
// is already on the wire (so Hold, waiting for the release, waits for the send).
func TestEntryPermitIsHeldAcrossTheWireWrite(t *testing.T) {
	s, frames := allFramesServer(t)
	tr := armedTrader(t, s)
	var sawFrameBeforeRelease atomic.Bool
	var released atomic.Bool
	tr.SetEntryPermit(func() (func(), bool) {
		return func() {
			sawFrameBeforeRelease.Store(waitFrame(frames, ntwire.FrameSignal, 2*time.Second))
			released.Store(true)
		}, true
	})
	if _, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200); err != nil {
		t.Fatal(err)
	}
	if !released.Load() {
		t.Fatal("the permit was never released")
	}
	if !sawFrameBeforeRelease.Load() {
		t.Fatal("the permit was released before the signal frame was written")
	}
}

// Protection, management and exits are NEVER held (CTO C1): with the permit
// refusing, each still reaches the wire with its own frame type.
func TestEntryPermitDoesNotGateProtectionManagementOrExits(t *testing.T) {
	s, frames := allFramesServer(t)
	tr := armedTrader(t, s)
	tr.SetEntryPermit(refusePermit)
	tr.mu.Lock()
	tr.lastEntrySignalID = "sig-open-1" // an open entry to manage
	tr.mu.Unlock()

	steps := []struct {
		name string
		call func() error
		want ntwire.FrameType
	}{
		{"PlaceProtectiveStop", func() error { return tr.PlaceProtectiveStop("MNQ", "long", 1, 29000, "sig-open-1", "test") }, ntwire.FramePlaceProtectiveStop},
		{"CancelOrder", func() error { return tr.CancelOrder("sig-open-1") }, ntwire.FrameCancelOrder},
		{"ModifyBracket", func() error { return tr.ModifyBracket("sig-open-1", 29010, 29190) }, ntwire.FrameModifyBracket},
		{"MoveStopToBreakeven", func() error { return tr.MoveStopToBreakeven("long", 29050) }, ntwire.FrameMoveStop},
		{"CloseLong", func() error { _, err := tr.CloseLong("MNQ", 1); return err }, ntwire.FrameClosePosition},
	}
	for _, st := range steps {
		err := st.call()
		if errors.Is(err, ErrMaintenanceHold) {
			t.Fatalf("%s was refused by the maintenance hold — protection/management/exits must stay open", st.name)
		}
		if err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		if !waitFrame(frames, st.want, 2*time.Second) {
			t.Fatalf("%s: its frame never reached the wire while held", st.name)
		}
	}
}

// Unwired permit (standalone TCPTrader, every pre-existing fixture) = allow.
func TestUnwiredEntryPermitAllows(t *testing.T) {
	s, frames := allFramesServer(t)
	tr := armedTrader(t, s)
	if _, err := tr.OpenLong("MNQ", 1, 1); err != nil {
		t.Fatalf("unwired permit must allow: %v", err)
	}
	if !waitFrame(frames, ntwire.FrameSignal, 2*time.Second) {
		t.Fatal("unwired: the entry never reached the wire")
	}
}

// M2.1 (review 3 F11): the permit refusal sits BEFORE the B3 dedupe in EVERY
// entry function, not only placeEntry: once the hold lifts, the very same
// entry is admitted (a refusal that consumed the dedupe slot would drop the
// legitimate retry). Mutation: PlaceLimitEntry's permit moved after B3.
func TestEntryPermitRefusalNeverConsumesTheDedupeSlotInAnyEntryFunction(t *testing.T) {
	calls := map[string]func(tr *TCPTrader) error{
		"placeEntry(OpenLong)": func(tr *TCPTrader) error { _, err := tr.OpenLong("MNQ", 1, 1); return err },
		"PlaceLimitEntry": func(tr *TCPTrader) error {
			_, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200)
			return err
		},
		"PlaceStopEntry": func(tr *TCPTrader) error {
			_, err := tr.PlaceStopEntry("MNQ", "long", 1, 29150, 29000, 29300, 0, 0)
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			s, frames := allFramesServer(t)
			tr := armedTrader(t, s)
			tr.SetEntryPermit(refusePermit)
			if err := call(tr); !errors.Is(err, ErrMaintenanceHold) {
				t.Fatalf("held: want ErrMaintenanceHold, got %v", err)
			}
			tr.SetEntryPermit(func() (func(), bool) { return func() {}, true })
			if err := call(tr); err != nil {
				t.Fatalf("after the hold lifts the same entry must be admitted, got %v", err)
			}
			if !waitFrame(frames, ntwire.FrameSignal, 2*time.Second) {
				t.Fatal("the admitted entry never reached the wire")
			}
		})
	}
}
