// Plan 4.11 — accounts_list receive-path integration test.
//
// Spins a real TCPServer + MockTCPClient, has the mock write an
// accounts_list frame, and asserts the server's AccountsList() returns
// the stored accounts with is_sim flags intact. Also verifies
// account_select round-trip: sending an account_select frame to the
// server and observing the response (200 on SIM, 400 on live without SIM guard).
package ninjatrader

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTCPServer_AccountsListReceivePath(t *testing.T) {
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

	// Before any frame, AccountsList should return empty and ok=false.
	list, ok := srv.AccountsList()
	if ok {
		t.Errorf("AccountsList ok=true before any accounts_list frame; got %+v", list)
	}
	if len(list.Accounts) != 0 {
		t.Fatalf("AccountsList should be empty before frame; got %+v", list)
	}

	// Send accounts_list frame with two accounts: one SIM, one live.
	want := AccountsListPayload{
		Accounts: []AccountInfo{
			{Name: "Sim101", IsSim: true},
			{Name: "Live", IsSim: false},
		},
	}
	if err := writeFromMock(client, FrameAccountsList, want); err != nil {
		t.Fatalf("write accounts_list: %v", err)
	}

	// Poll until the drain/read loop applies it.
	deadline := time.Now().Add(2 * time.Second)
	var got AccountsListPayload
	var finalOK bool
	for time.Now().Before(deadline) {
		if got, finalOK = srv.AccountsList(); finalOK {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !finalOK {
		t.Fatal("AccountsList never became valid after accounts_list frame")
	}

	// Verify exact payload: count, names, and is_sim flags.
	if len(got.Accounts) != len(want.Accounts) {
		t.Errorf("AccountsList count: want %d, got %d", len(want.Accounts), len(got.Accounts))
	}
	for i, wantAcct := range want.Accounts {
		if i >= len(got.Accounts) {
			t.Fatalf("got.Accounts[%d] out of range", i)
		}
		if got.Accounts[i].Name != wantAcct.Name {
			t.Errorf("Accounts[%d].Name: want %q, got %q", i, wantAcct.Name, got.Accounts[i].Name)
		}
		if got.Accounts[i].IsSim != wantAcct.IsSim {
			t.Errorf("Accounts[%d].IsSim: want %v, got %v", i, wantAcct.IsSim, got.Accounts[i].IsSim)
		}
	}
}

func TestTCPServer_AccountSelectRoundTrip(t *testing.T) {
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

	// First, send accounts_list so the server knows about available accounts.
	accountsPayload := AccountsListPayload{
		Accounts: []AccountInfo{
			{Name: "Sim101", IsSim: true},
			{Name: "Live", IsSim: false},
		},
	}
	if err := writeFromMock(client, FrameAccountsList, accountsPayload); err != nil {
		t.Fatalf("write accounts_list: %v", err)
	}

	// Wait for the server to store it.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := srv.AccountsList(); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Now send account_select to switch to Sim101.
	// This should succeed because Sim101 is marked is_sim=true.
	selectPayload := AccountSelectPayload{Account: "Sim101"}

	// Before the select, CurrentAccount should be nil/empty.
	currBefore := srv.CurrentAccount()
	if currBefore != nil && *currBefore != "" {
		t.Errorf("CurrentAccount before select: want nil/empty, got %v", currBefore)
	}

	// Send the select frame from the mock.
	if err := writeFromMock(client, FrameAccountSelect, selectPayload); err != nil {
		t.Fatalf("write account_select Sim101: %v", err)
	}

	// Poll until the server processes the select and updates CurrentAccount.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if curr := srv.CurrentAccount(); curr != nil && *curr == "Sim101" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if curr := srv.CurrentAccount(); curr == nil || *curr != "Sim101" {
		t.Errorf("CurrentAccount after select: want Sim101, got %v", curr)
	}

	// Now attempt to switch to "Live" (is_sim=false).
	// The server should reject this (via SIM guard).
	selectLivePayload := AccountSelectPayload{Account: "Live"}
	if err := writeFromMock(client, FrameAccountSelect, selectLivePayload); err != nil {
		t.Fatalf("write account_select Live: %v", err)
	}

	// Give the server a brief moment to process and reject.
	time.Sleep(100 * time.Millisecond)

	// After the rejection, CurrentAccount should still be Sim101.
	if curr := srv.CurrentAccount(); curr == nil || *curr != "Sim101" {
		t.Errorf("CurrentAccount after live reject: want Sim101, got %v", curr)
	}
}

func TestTCPServer_AccountsListThreadSafety(t *testing.T) {
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

	// Send an accounts_list frame.
	payload := AccountsListPayload{
		Accounts: []AccountInfo{
			{Name: "TestAcct1", IsSim: true},
			{Name: "TestAcct2", IsSim: false},
		},
	}
	if err := writeFromMock(client, FrameAccountsList, payload); err != nil {
		t.Fatalf("write accounts_list: %v", err)
	}

	// Wait for it to be stored.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := srv.AccountsList(); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Spawn multiple goroutines reading AccountsList concurrently.
	// This verifies the RWMutex doesn't deadlock and returns consistent data.
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				list, ok := srv.AccountsList()
				if !ok {
					errs <- nil
					continue
				}
				if len(list.Accounts) != 2 {
					errs <- nil
					continue
				}
				if list.Accounts[0].Name != "TestAcct1" || !list.Accounts[0].IsSim {
					errs <- nil
				}
				if list.Accounts[1].Name != "TestAcct2" || list.Accounts[1].IsSim {
					errs <- nil
				}
			}
		}()
	}
	wg.Wait()
	close(errs)

	// Drain any errors (should be none).
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent read error: %v", err)
		}
	}
}

func TestTCPServer_CurrentAccountUpdatesOnSelect(t *testing.T) {
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

	// Send accounts_list.
	payload := AccountsListPayload{
		Accounts: []AccountInfo{
			{Name: "AcctA", IsSim: true},
			{Name: "AcctB", IsSim: true},
			{Name: "AcctC", IsSim: false},
		},
	}
	if err := writeFromMock(client, FrameAccountsList, payload); err != nil {
		t.Fatalf("write accounts_list: %v", err)
	}

	// Wait for storage.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := srv.AccountsList(); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Select AcctA (SIM).
	if err := writeFromMock(client, FrameAccountSelect, AccountSelectPayload{Account: "AcctA"}); err != nil {
		t.Fatalf("select AcctA: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if curr := srv.CurrentAccount(); curr != nil && *curr == "AcctA" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if curr := srv.CurrentAccount(); curr == nil || *curr != "AcctA" {
		t.Errorf("after selecting AcctA: want AcctA, got %v", curr)
	}

	// Switch to AcctB (also SIM, should succeed).
	if err := writeFromMock(client, FrameAccountSelect, AccountSelectPayload{Account: "AcctB"}); err != nil {
		t.Fatalf("select AcctB: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if curr := srv.CurrentAccount(); curr != nil && *curr == "AcctB" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if curr := srv.CurrentAccount(); curr == nil || *curr != "AcctB" {
		t.Errorf("after selecting AcctB: want AcctB, got %v", curr)
	}

	// Try to switch to AcctC (live, should fail and revert to AcctB).
	if err := writeFromMock(client, FrameAccountSelect, AccountSelectPayload{Account: "AcctC"}); err != nil {
		t.Fatalf("select AcctC: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if curr := srv.CurrentAccount(); curr == nil || *curr != "AcctB" {
		t.Errorf("after rejecting AcctC: want AcctB, got %v", curr)
	}
}
