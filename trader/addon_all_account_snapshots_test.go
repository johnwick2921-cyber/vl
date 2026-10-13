package trader

import (
	"os"
	"strings"
	"testing"
)

// REL19-ADDON — the AddOn must stream the working-order book for EVERY target
// account (all SIM accounts + any account Go declared bound via account_register),
// not just the active one. The repo has no C# harness (NinjaScript compiles only
// inside NT8 on the Windows host), so these are SOURCE pins — the same shape as
// wave_b_addon_slot_test.go: they prove the shipped .cs text, and the far-side
// build gate proves the DLL NT8 actually loaded. A mutant that reverts the C#
// to active-account-only turns the matching pin RED.

const addonSnapshotSourcePath = "../ninjascript/VLTraderTCPClient.cs"

func addonSnapshotSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(addonSnapshotSourcePath)
	if err != nil {
		t.Fatalf("cannot read the AddOn source at %s: %v", addonSnapshotSourcePath, err)
	}
	return string(b)
}

// TestAddonSendAllOrderSnapshotsEnumeratesAccounts pins the all-account
// enumeration: the method exists, walks Account.All (not just `account`), keeps
// the SIM filter, and sends one frame per target account.
func TestAddonSendAllOrderSnapshotsEnumeratesAccounts(t *testing.T) {
	src := addonSnapshotSource(t)
	if !strings.Contains(src, "private void SendAllOrderSnapshots(string reason)") {
		t.Fatal("SendAllOrderSnapshots method is gone")
	}
	if !strings.Contains(src, "foreach (var a in Account.All)") {
		t.Fatal("SendAllOrderSnapshots must enumerate Account.All (every account), not just the active one")
	}
	if !strings.Contains(src, "bool target = IsSimAccount(a);") {
		t.Fatal("SendAllOrderSnapshots lost the SIM filter")
	}
	if !strings.Contains(src, `if (target) SendOrderSnapshot(a, reason ?? "periodic");`) {
		t.Fatal("SendAllOrderSnapshots must send one frame per target account")
	}
}

// TestAddonPeriodicSnapshotIsAllAccounts — mutant: periodic back to active-only
// (SendOrderSnapshot(account, "periodic")) → RED.
func TestAddonPeriodicSnapshotIsAllAccounts(t *testing.T) {
	src := addonSnapshotSource(t)
	if strings.Contains(src, `try { SendOrderSnapshot(account, "periodic"); }`) {
		t.Fatal("the periodic heartbeat still snapshots only the active account — a non-active-account trader reads an empty book between order events")
	}
	if !strings.Contains(src, `try { SendAllOrderSnapshots("periodic"); }`) {
		t.Fatal(`the periodic heartbeat must call SendAllOrderSnapshots("periodic")`)
	}
}

// TestAddonConnectAndRegisterTriggerAllAccountSnapshots — mutants: drop the
// account_register trigger, or skip non-active accounts on connect → RED.
func TestAddonConnectAndRegisterTriggerAllAccountSnapshots(t *testing.T) {
	src := addonSnapshotSource(t)
	if !strings.Contains(src, `SendAllOrderSnapshots("connect");`) {
		t.Fatal(`the connect path must snapshot every account: missing SendAllOrderSnapshots("connect")`)
	}
	if !strings.Contains(src, `SendAllOrderSnapshots("account_register");`) {
		t.Fatal(`account_register must trigger an all-account snapshot: missing SendAllOrderSnapshots("account_register")`)
	}
}
