package ninjatrader

import (
	"context"
	"net"
	"testing"
	"time"
)

// Release 2 (AddOn 2026-11-24-a) adds two ack STRINGS and one order_snapshot REASON and
// changes no frame. This is the Go half of that promise; it uses no helper newer than
// #71, so the same file runs on the installed bot's source.
func TestAddonSettleGateFramesChangeNothingInGo(t *testing.T) {
	s := NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })
	c, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	go func() {
		for {
			if _, err := ReadFrame(c); err != nil {
				return
			}
		}
	}()
	wait := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); !ok(); time.Sleep(time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	wait("connect", s.IsConnected)
	for _, acks := range []string{"place_protective_stop_flat", "place_protective_stop_opposite"} {
		if err := WriteFrame(c, FrameAck, AckPayload{Acks: acks}); err != nil {
			t.Fatal(err)
		}
	}
	const acct = "SimA"
	if err := WriteFrame(c, FrameOrderSnapshot, OrderSnapshotPayload{
		Account: acct, BuildID: ExpectedAddonBuild, EmittedMs: time.Now().UnixMilli(), Reason: "lx_working",
		Orders: []NT8Order{{OrderID: "1", Name: "c1-lx", Action: "buy", Type: "limit", Quantity: 1, State: "Working", Symbol: "MNQ"}},
	}); err != nil {
		t.Fatal(err)
	}
	wait("the snapshot", func() bool { _, ok := s.OrderSnapshots().Latest(acct); return ok })
	p, _ := s.OrderSnapshots().Latest(acct)
	if p.Reason != "lx_working" || len(p.WorkingOrders()) != 1 {
		t.Fatalf("lx_working snapshot = %+v", p)
	}
	// The acks came first on the same ordered stream: they have been processed, and the
	// connection is still the live one.
	if !s.IsConnected() {
		t.Fatal("an unknown ack string cost the connection")
	}
}
