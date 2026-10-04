package ninjatrader

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// M10 pin (REVIEW-309 r2, PR B 2026-10-03): the stop-limit build floor. An
// AddOn that proves the stop-SLOT floor but not the stop-LIMIT floor would
// build StopMarket when handed stop_limit=true — fail closed. Remove the floor
// check in placeStopEntry and this test turns RED twice over: the error is no
// longer ErrAddonBuildTooOld AND a signal frame reaches the wire.
func TestStopLimitFloorRefusesAnAddOnBelowIt(t *testing.T) {
	s, frames := allFramesServer(t) // proves 2026-09-20-p1: ≥ stop-slot, < stop-limit
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "floor-pin"
	_, err := tr.PlaceStopEntryWithLimit("MNQ", "long", 1, 100, 99, 102)
	if !errors.Is(err, ntwire.ErrAddonBuildTooOld) {
		t.Fatalf("a stop-limit on an AddOn below the floor must refuse with ErrAddonBuildTooOld, got %v", err)
	}
	// The bind emits account_register frames; the ONLY frame that proves the
	// floor leaked is a signal frame.
	deadline := time.After(200 * time.Millisecond)
	for {
		select {
		case f := <-frames:
			if f == ntwire.FrameSignal {
				t.Fatalf("the floor refusal must not reach the wire, got a signal frame")
			}
		case <-deadline:
			return
		}
	}
}

// dialServer opens a fresh loopback client connection to a started server.
func dialServer(t *testing.T, s *ntwire.TCPServer) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

// proveBuildServer is a loopback server whose far side proves EXACTLY the
// given build id — unlike allFramesServer, which proves the highest floor and
// would hide every value below it.
func proveBuildServer(t *testing.T, buildID string) (*ntwire.TCPServer, chan ntwire.FrameType) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "pb.db"))
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
	if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: buildID}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200 && !ntwire.FarSideProven(s.FarSideBuildID(), buildID); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !ntwire.FarSideProven(s.FarSideBuildID(), buildID) {
		t.Fatal("fixture: far-side build never proven")
	}
	drain(frames)
	return s, frames
}

// REVIEW-313 M5 pin: the floor VALUE, not just its presence. The realistic
// below-floor AddOn is #312's own c1 build — the owner F5'd #312 but not #313
// — and a floor lowered to c1 keeps every existing test green while that c1
// AddOn builds StopMarket on a stop_limit=true frame. c1 must be REFUSED and
// c2 must be SENT.
func TestStopLimitFloorValueRefusesCancelReportBuild(t *testing.T) {
	for _, tc := range []struct {
		name        string
		build       string
		wantRefused bool
	}{
		{"c1_cancel_report_build_refused", ntwire.MinAddonBuildCancelReport, true},
		{"c2_stop_limit_build_sent", ntwire.MinAddonBuildStopLimit, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, frames := proveBuildServer(t, tc.build)
			tr := NewTCPTrader(s, "MNQ", "Sim101")
			tr.traderID = "floor-value-pin"
			_, err := tr.PlaceStopEntryWithLimit("MNQ", "long", 1, 100, 99, 102)
			if tc.wantRefused {
				if !errors.Is(err, ntwire.ErrAddonBuildTooOld) {
					t.Fatalf("a stop-limit on the c1 (cancel-report) AddOn must refuse with ErrAddonBuildTooOld, got %v", err)
				}
				deadline := time.After(200 * time.Millisecond)
			refuseDrain:
				for {
					select {
					case f := <-frames:
						if f == ntwire.FrameSignal {
							t.Fatalf("the c1 floor refusal must not reach the wire, got a signal frame")
						}
					case <-deadline:
						break refuseDrain
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("a stop-limit on the c2 AddOn must be sent, got %v", err)
			}
			deadline := time.After(500 * time.Millisecond)
			for {
				select {
				case f := <-frames:
					if f == ntwire.FrameSignal {
						return
					}
				case <-deadline:
					t.Fatal("no signal frame reached the wire for the c2 AddOn")
				}
			}
		})
	}
}
