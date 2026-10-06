package ninjatrader

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// REVIEW-313 M6 + M19 pin, at the production call site: the frame flag must
// CARRY the knob. With the c2 floor proven, PlaceStopEntryWithLimit must emit
// stop_limit:true with order_type stop_entry (M6 — setting the flag to false
// would silently send a stop-MARKET with the whole suite green), and
// PlaceStopEntry must emit the frame with the key ABSENT (M19 — the knob-off
// wire is byte-identical to today; an omitempty regression would send
// stop_limit:false on every stop-market). Both read the RAW frame bytes: a
// decoded bool cannot tell "absent" from "false".
func TestStopLimitWireFlagCarriesTheKnob(t *testing.T) {
	s, rawFrames := rawSignalServer(t, ntwire.MinAddonBuildStopLimit)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "wire-flag-pin"

	// M6: the limit variant on a c2 AddOn carries stop_limit:true.
	if _, err := tr.PlaceStopEntryWithLimit("MNQ", "long", 1, 100, 99, 102, 0, 0); err != nil {
		t.Fatalf("place with limit failed: %v", err)
	}
	raw := readRawFrame(t, rawFrames)
	if !bytes.Contains(raw, []byte(`"stop_limit":true`)) {
		t.Fatalf("the stop-limit frame must carry stop_limit:true, got %s", raw)
	}
	var p ntwire.SignalPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("decode signal frame: %v", err)
	}
	if p.OrderType != "stop_entry" {
		t.Fatalf("order_type=%q, want stop_entry", p.OrderType)
	}
	if !p.StopLimit {
		t.Fatalf("StopLimit decoded false on the limit variant frame")
	}

	// M19: the market variant sends the key ABSENT — the knob-off wire.
	// (short side: the B3 guard dedupes the identical long key within the
	// window, and a second long would be dropped as the first's duplicate.)
	if _, err := tr.PlaceStopEntry("MNQ", "short", 1, 100, 102, 98, 0, 0); err != nil {
		t.Fatalf("place stop entry failed: %v", err)
	}
	raw2 := readRawFrame(t, rawFrames)
	if bytes.Contains(raw2, []byte("stop_limit")) {
		t.Fatalf("the stop-market frame must not carry a stop_limit key, got %s", raw2)
	}
}

// rawSignalServer proves the given build and returns the RAW signal frame
// payload bytes as they left the server. One client: the reader and the
// prover are the same conn, so the frames the server writes back are exactly
// this test's signal frames.
func rawSignalServer(t *testing.T, buildID string) (*ntwire.TCPServer, chan []byte) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "wf.db"))
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
	raw := make(chan []byte, 8)
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			if env.Type != ntwire.FrameSignal {
				continue
			}
			raw <- append([]byte(nil), env.Payload...)
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
	return s, raw
}

func readRawFrame(t *testing.T, raw chan []byte) []byte {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case p := <-raw:
			return p
		case <-deadline:
			t.Fatal("no signal frame reached the wire")
		}
	}
}
