package ninjatrader

import (
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
)

// TestMoveStopForSignalKeyedBySignalID — the mentor exit-drive's signal-keyed
// move: two legs of one mentor intent share a side but carry distinct signal
// ids, so each move is keyed by signal id and each widen ban is PER SIGNAL (the
// side-keyed stopLoss must never cross-contaminate leg 1's stop into the
// runner's widen check). [A] pins the wire frame the AddOn reads.
func TestMoveStopForSignalKeyedBySignalID(t *testing.T) {
	s, st, conn, moves := moveStopServer(t)

	if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: ntwire.MinAddonBuildStopSlot}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for s.FarSideBuildID() == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.StartCloseSync("signal-keyed-trader", "fixture", "ninjatrader", st)

	// leg 1 moves first.
	if err := tr.MoveStopForSignal("leg1", "long", 29600); err != nil {
		t.Fatalf("leg1 move: %v", err)
	}
	// The runner moves INDEPENDENTLY to a tighter stop that sits BELOW leg 1's:
	// under a side-keyed widen ban this would be refused (29590 < 29600).
	if err := tr.MoveStopForSignal("runner", "long", 29590); err != nil {
		t.Fatalf("runner independent move refused by cross-leg widen ban: %v", err)
	}
	// The runner's OWN widen ban still holds: 29580 < 29590 widens.
	if err := tr.MoveStopForSignal("runner", "long", 29580); err == nil {
		t.Fatal("runner widen (29590 → 29580) was NOT refused")
	}

	got := []ntwire.MoveStopPayload{}
	for i := 0; i < 2; i++ {
		select {
		case p := <-moves:
			got = append(got, p)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for move_stop %d/2 (got %d)", i+1, len(got))
		}
	}
	if len(got) != 2 {
		t.Fatalf("move_stop frames = %d, want 2 (the widen was never sent)", len(got))
	}
	if got[0].SignalID != "leg1" || got[1].SignalID != "runner" {
		t.Fatalf("signal ids = %q,%q, want leg1,runner", got[0].SignalID, got[1].SignalID)
	}
	if got[0].NewStopLoss != 29600 || got[1].NewStopLoss != 29590 {
		t.Fatalf("stops = %.2f,%.2f, want 29600,29590", got[0].NewStopLoss, got[1].NewStopLoss)
	}
}
