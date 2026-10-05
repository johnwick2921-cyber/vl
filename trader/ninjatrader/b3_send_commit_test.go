package ninjatrader

import (
	"errors"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/telemetry"
)

// W1b E12(b) — THE B3 DEDUPE SLOT IS RECORDED ONLY BY A REAL SEND.
//
// orderGuard.admit recorded the key at ADMIT time, before the ledger
// registration (beforeSend), the SL/TP precondition and the send. A refusal
// AFTER B3 therefore burned the 55 s window: the legitimate retry of an
// attempt that never reached the wire was refused as a "duplicate" of it.
// And the armed paths discarded B3's reason and never counted the refusal,
// so "Duplicate order dropped" never showed an armed refusal.
//
// Production call sites: PlaceLimitEntry / PlaceStopEntry / OpenLong over the
// real loopback (stopEntryServer); the latch is unwired (allows everything),
// so B3 is the only guard that can refuse the retry.

func noFrame(t *testing.T, frames chan ntwire.SignalPayload, what string) {
	t.Helper()
	select {
	case p := <-frames:
		t.Fatalf("%s: a frame left the wire: %+v", what, p)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestB3UnsentLimitRefusalDoesNotConsumeSlot(t *testing.T) {
	s, _, _, frames := stopEntryServer(t)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	// Attempt 1: B3 admits, then the ledger registration refuses — nothing sent.
	if _, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29600, 29590, 29630, func(string) error { return errors.New("fixture ledger unavailable") }); err == nil {
		t.Fatal("registration failure ignored")
	}
	noFrame(t, frames, "unregistered attempt")
	// Attempt 2, the same order a moment later with a working registration.
	sid, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29600, 29590, 29630, func(string) error { return nil })
	if err != nil {
		t.Fatalf("retry of an attempt that never reached the wire was refused: %v", err)
	}
	if p := awaitFrame(t, frames, "retry"); p.SignalID != sid {
		t.Fatalf("retry frame signal %q, want %q", p.SignalID, sid)
	}
}

func TestB3UnsentStopEntryRefusalDoesNotConsumeSlot(t *testing.T) {
	s, _, conn, frames := stopEntryServer(t)
	if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: ntwire.MinAddonBuildStopSlot}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100 && !ntwire.FarSideProven(s.FarSideBuildID(), ntwire.MinAddonBuildStopSlot); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	if _, err := tr.PlaceStopEntry("MNQ", "short", 1, 29590, 29610, 29550, 0, 0, func(string) error { return errors.New("fixture ledger unavailable") }); err == nil {
		t.Fatal("registration failure ignored")
	}
	noFrame(t, frames, "unregistered stop-entry")
	if _, err := tr.PlaceStopEntry("MNQ", "short", 1, 29590, 29610, 29550, 0, 0); err != nil {
		t.Fatalf("retry of an unsent stop-entry was refused: %v", err)
	}
	awaitFrame(t, frames, "stop-entry retry")
}

func TestB3UnsentMarketRefusalDoesNotConsumeSlot(t *testing.T) {
	s, _, _, frames := stopEntryServer(t)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	// Attempt 1: B3 admits, then the SL/TP precondition refuses — nothing sent.
	if _, err := tr.OpenLong("MNQ", 1, 1); err == nil || !strings.Contains(err.Error(), "SetStopLoss") {
		t.Fatalf("want the SL/TP precondition refusal, got %v", err)
	}
	noFrame(t, frames, "bracketless market entry")
	_ = tr.SetStopLoss("MNQ", "long", 1, 29575)
	_ = tr.SetTakeProfit("MNQ", "long", 1, 29650)
	if _, err := tr.OpenLong("MNQ", 1, 1); err != nil {
		t.Fatalf("retry of an unsent market entry was refused: %v", err)
	}
	awaitFrame(t, frames, "market retry")
}

func gateBlocks(gate string) int {
	_, table := telemetry.GateBlockSnapshot()
	return table[""][gate]
}

// A REAL duplicate is still refused — and on the armed paths it is now named
// and counted like the market path's (b3_order_dedup in the process-wide ""
// bucket), so the Gate-blocks panel can see it.
func TestB3ArmedDuplicateRefusedNamedAndCounted(t *testing.T) {
	s, _, conn, frames := stopEntryServer(t)
	if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: ntwire.MinAddonBuildStopSlot}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100 && !ntwire.FarSideProven(s.FarSideBuildID(), ntwire.MinAddonBuildStopSlot); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	for _, kind := range []string{"limit", "stop_entry"} {
		send := func(beforeSend ...func(string) error) (string, error) {
			if kind == "stop_entry" {
				return tr.PlaceStopEntry("MNQ", "long", 1, 29600, 29575, 29650, 0, 0, beforeSend...)
			}
			return tr.PlaceLimitEntry("MNQ", "long", 1, 29600, 29575, 29650, beforeSend...)
		}
		if _, err := send(); err != nil {
			t.Fatalf("%s: first send refused: %v", kind, err)
		}
		awaitFrame(t, frames, kind+" first")
		before := gateBlocks("b3_order_dedup")
		_, err := send()
		if err == nil {
			t.Fatalf("%s: an identical order inside the window was sent twice", kind)
		}
		noFrame(t, frames, kind+" duplicate")
		if !strings.Contains(err.Error(), "duplicate order dropped") {
			t.Errorf("%s: refusal must carry B3's reason, got %q", kind, err)
		}
		if got := gateBlocks("b3_order_dedup") - before; got != 1 {
			t.Errorf("%s: armed B3 refusal counted %d times under b3_order_dedup, want 1", kind, got)
		}
	}
}

// A REAL duplicate MARKET entry is still refused (W1b E12 verifier defect 2).
// placeEntry records its B3 slot through `b3Sent = latchSent`; without that
// assignment a sent market entry never records its key and a double-fired
// OpenLong passes B3. Two OpenLong calls over the real loopback: the second is
// "duplicate order dropped", sends no frame, and counts b3_order_dedup once.
func TestB3MarketDuplicateRefusedAndCounted(t *testing.T) {
	s, _, _, frames := stopEntryServer(t)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	_ = tr.SetStopLoss("MNQ", "long", 1, 29575)
	_ = tr.SetTakeProfit("MNQ", "long", 1, 29650)
	if _, err := tr.OpenLong("MNQ", 1, 1); err != nil {
		t.Fatalf("first market entry refused: %v", err)
	}
	awaitFrame(t, frames, "market first")
	before := gateBlocks("b3_order_dedup")
	_, err := tr.OpenLong("MNQ", 1, 1)
	if err == nil {
		t.Fatal("an identical market entry inside the window was sent twice")
	}
	noFrame(t, frames, "market duplicate")
	if !strings.Contains(err.Error(), "duplicate order dropped") {
		t.Errorf("market: refusal must carry B3's reason, got %q", err)
	}
	if got := gateBlocks("b3_order_dedup") - before; got != 1 {
		t.Errorf("market: B3 duplicate counted %d times under b3_order_dedup, want 1", got)
	}
}
