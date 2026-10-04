package ninjatrader

import (
	"encoding/json"
	"errors"
	"testing"
)

// PARTIAL-CLOSE (2026-10-03) — the capability gate: Go refuses to SEND
// reduce_position to a peer that never advertised it, and the hello flag is
// what proves it (dispatch item 3).

func TestSendReducePositionRefusesWithoutTheCapability(t *testing.T) {
	s := NewTCPServer(nil)
	if s.ReducePositionSupported() {
		t.Fatalf("a fresh server must not believe an unproven capability")
	}
	err := s.SendReducePosition(ReducePositionPayload{Symbol: "MNQ", Side: "long", Quantity: 3, ClientID: "rx-1"})
	if !errors.Is(err, ErrReduceUnsupported) {
		t.Fatalf("want ErrReduceUnsupported before any send, got %v", err)
	}
}

func TestSendReducePositionProceedsPastTheGateWhenAdvertised(t *testing.T) {
	s := NewTCPServer(nil)
	s.farSideReduce.Store(true)
	// The capability gate passes; the next refusal is the missing connection —
	// proving the frame was not blocked for capability reasons.
	err := s.SendReducePosition(ReducePositionPayload{Symbol: "MNQ", Side: "long", Quantity: 3, ClientID: "rx-1"})
	if err == nil || errors.Is(err, ErrReduceUnsupported) {
		t.Fatalf("want the connection error past the gate, got %v", err)
	}
}

func TestHelloPayloadCarriesTheReduceFlag(t *testing.T) {
	// The flag is optional on the wire: an older AddOn's hello (no field)
	// parses to false — fail-closed, capability by receipt.
	old := `{"protocol_version":3,"source":"vltrader-addon","build_id":"2026-09-30-m22"}`
	var hp HelloPayload
	if err := json.Unmarshal([]byte(old), &hp); err != nil {
		t.Fatalf("old hello must parse: %v", err)
	}
	if hp.ReducePosition {
		t.Fatalf("an AddOn that never advertised reduce_position must read false")
	}
	newh := `{"protocol_version":3,"source":"vltrader-addon","build_id":"2026-10-03-c1","reduce_position":true}`
	if err := json.Unmarshal([]byte(newh), &hp); err != nil {
		t.Fatalf("new hello must parse: %v", err)
	}
	if !hp.ReducePosition {
		t.Fatalf("the advertised capability must be received")
	}
}

func TestReduceFillPayloadRoundtrip(t *testing.T) {
	raw := `{"client_id":"rx-1","symbol":"MNQ","side":"long","quantity":3,"fill_price":100.25,"remaining":2,"account":"Sim101"}`
	var p ReduceFillPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("reduce_fill must parse: %v", err)
	}
	if p.Quantity != 3 || p.Remaining != 2 || p.FillPrice != 100.25 {
		t.Fatalf("the fill and the REMAINING quantity must ride the frame, got %+v", p)
	}
}
