package trader

import (
	"testing"
	"time"
	nt "vl/provider/ninjatrader"
	nttrader "vl/trader/ninjatrader"
)

func TestOrderBookDisplayScopesAndDatesReceipt(t *testing.T) {
	t.Setenv("NT8_ORDER_SNAPSHOT_SECS", "30")
	server := nt.NewTCPServer(nil)
	at := &AutoTrader{trader: nttrader.NewTCPTrader(server, "MNQ", "audit-sim"), config: AutoTraderConfig{NinjaTraderSymbol: "MNQ"}}
	now := time.Date(2026, 9, 8, 3, 0, 0, 0, time.UTC)
	if got := at.OrderBookForDisplayAt(now); got.Reason == "" || got.ReceivedAtMs != 0 {
		t.Fatal("missing book became computed")
	}
	server.OrderSnapshots().PutAt(nt.OrderSnapshotPayload{Account: "other-sim", Orders: []nt.NT8Order{{Symbol: "MNQ", Name: "wrong-account"}}}, now)
	if got := at.OrderBookForDisplayAt(now); got.Reason == "" {
		t.Fatal("another account answered")
	}
	received := now.Add(-21 * time.Second)
	server.OrderSnapshots().PutAt(nt.OrderSnapshotPayload{Account: "audit-sim", BuildID: "2026-09-07-h1", EmittedMs: now.Add(time.Hour).UnixMilli(), Orders: []nt.NT8Order{{Symbol: "MNQ", Name: "right"}, {Symbol: "ES", Name: "wrong-symbol"}}}, received)
	got := at.OrderBookForDisplayAt(now)
	if got.Reason != "" || got.ReceivedAtMs != received.UnixMilli() || got.AgeMs != 21000 || len(got.Orders) != 1 || got.Orders[0].Name != "right" {
		t.Fatalf("wrong source: %+v", got)
	}
	if got := at.OrderBookForDisplayAt(now.Add(time.Minute)); got.Reason == "" || len(got.Orders) != 0 {
		t.Fatal("stale book accepted")
	}
	if got := at.OrderBookForDisplayAt(received.Add(-time.Second)); got.Reason == "" || len(got.Orders) != 0 {
		t.Fatal("future receipt accepted")
	}
}
