package updaterworker

import (
	"testing"
	"time"
)

// ── W-ADDON-ONE-CLICK e2e fakes (DS-103) ─────────────────────────────────────
//
// The fake NT8 models the restart seam: the AddOn drops off the wire on "stop"
// (the maintenance ack goes nil) and reconnects on "start" announcing the NEW
// build under a FRESH accept_seq. The real runner, job file, hold file and the
// loopback app are the production ones from the existing rig; only the NT8 wire,
// dotnet and powershell are faked.
//
// DS-103 e2e cases (PLAN-update-walk-away.md "Proof required before done"):
//   1. a changed AddOn completes with no human (no nt8_updated park);
//   2. a build failure → reverted_clean (hold + lock released, placed AddOn
//      restored, result message sent);
//   3. a reconnect timeout after activation → rolled_back (snapshot rollback +
//      old DLL restored + hold/lock released + result message sent);
//   4. a transitional connection → waits (15 s re-checks, 10 min bound) then
//      proceeds, never refuses;
//   5. NT8 closed → completes, DLL placed, nothing owed;
//   6. first install: VLTraderTCPClient still in Custom.dll (VL.AddOn.dll
//      absent) → refused, sources migrated, one-time F5 blocker.
// These are wired to the worker's addon_build/addon_place/nt8_restart/reconnect
// steps once DS-104's feat/addon-one-click-worker and DS-102's
// feat/update-self-recovery land. The fakes below are the spec-fixed half.

// nt8Stop models powershell Stop-Process: the AddOn disconnects (ack gone).
func (b *box) nt8Stop() {
	b.mu.Lock()
	b.addonConnected = false
	b.addonDownSince = time.Second
	b.mu.Unlock()
}

// nt8Start models powershell Start-Process + the AddOn reconnect: it announces
// newBuild under a bumped accept_seq (a reconnect is a NEW connection, never a
// replayed hello).
func (b *box) nt8Start(newBuild string) {
	b.mu.Lock()
	b.addonBuild = newBuild
	b.addonSeq++
	b.addonConnected = true
	b.addonDownSince = 0
	b.mu.Unlock()
}

// TestFakeNT8StopStartModelsTheReconnect pins the fake itself, so the e2e cases
// rest on a fake whose disconnect/reconnect is real to the maintenance ack the
// worker reads. Mutant: nt8Start not bumping accept_seq → RED.
func TestFakeNT8StopStartModelsTheReconnect(t *testing.T) {
	r := newRig(t)
	b := r.box

	before := b.ack()
	if before == nil || before.BuildID != boxOldBuild {
		t.Fatalf("precondition: connected old build, got %+v", before)
	}

	b.nt8Stop()
	if a := b.ack(); a != nil {
		t.Fatalf("after stop the ack must be nil (NT8 gone), got %+v", a)
	}

	b.nt8Start(boxNewBuild)
	after := b.ack()
	if after == nil {
		t.Fatal("after start the ack must be present")
	}
	if after.BuildID != boxNewBuild {
		t.Fatalf("after start build = %q, want %q", after.BuildID, boxNewBuild)
	}
	if after.AcceptSeq <= before.AcceptSeq {
		t.Fatalf("after start accept_seq = %d, want > %d (a reconnect is a NEW seq)", after.AcceptSeq, before.AcceptSeq)
	}
}
