// Plan 4.11 — account_balance receive-path integration test.
//
// Spins a real TCPServer + MockTCPClient, has the mock write an
// account_balance frame, and asserts the server's AccountState() reflects it.
// Proves the END-TO-END Go receive path for the real-balance frame (read →
// decode → store → accessor) without requiring NT8. Mirrors
// tcp_server_bars_test.go.
package ninjatrader

import (
	"context"
	"testing"
	"time"
)

func TestTCPServer_AccountBalanceReceivePath(t *testing.T) {
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	client := NewMockTCPClient(addr, 50*time.Millisecond)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client Start: %v", err)
	}
	defer client.Stop()
	waitForConnected(t, srv, 2*time.Second)

	// Before any frame, AccountState reports not-yet-received.
	if _, ok := srv.AccountState(); ok {
		t.Fatal("AccountState ok=true before any account_balance frame")
	}

	want := AccountBalancePayload{
		Account:        "Sim101",
		CashValue:      102345.67,
		BuyingPower:    409382.68,
		RealizedPnL:    125.00,
		UnrealizedPnL:  -42.50,
		NetLiquidation: 102303.17,
	}
	if err := writeFromMock(client, FrameAccountBalance, want); err != nil {
		t.Fatalf("write account_balance: %v", err)
	}

	// Poll until the drain/read loop applies it.
	deadline := time.Now().Add(2 * time.Second)
	var got AccountBalancePayload
	var ok bool
	for time.Now().Before(deadline) {
		if got, ok = srv.AccountState(); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ok {
		t.Fatal("AccountState never became valid after account_balance frame")
	}
	if got.Account != want.Account || got.CashValue != want.CashValue ||
		got.BuyingPower != want.BuyingPower || got.RealizedPnL != want.RealizedPnL ||
		got.UnrealizedPnL != want.UnrealizedPnL || got.NetLiquidation != want.NetLiquidation {
		t.Errorf("AccountState = %+v, want %+v", got, want)
	}
}
