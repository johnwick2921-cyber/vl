package api

import (
	"testing"
)

// R14 — (a) the receipt dedup key persists across a restart, and (b) the
// monitor starts at boot (owner only), not on the first page read.

func TestR14FriendAlertDedupPersistsAcrossRestart(t *testing.T) {
	e := newUpdEnv(t)
	resetFriendAlertStateForTest()
	defer resetFriendAlertStateForTest()

	prev := latestFriendJobReceipts
	latestFriendJobReceipts = func() []friendJobReceipt {
		return []friendJobReceipt{{Name: "tom", State: "restarted", Rev: "r1"}}
	}
	t.Cleanup(func() { latestFriendJobReceipts = prev })

	msgs := e.s.relayFriendReceipts()
	if len(msgs) != 1 {
		t.Fatalf("first relay = %d msgs, want 1", len(msgs))
	}
	if got := loadFriendAlertDedup()["tom"]; got == "" {
		t.Fatalf("dedup key was not persisted for tom")
	}

	// Simulate a restart: clear in-memory state, hydrate from disk, relay again.
	resetFriendAlertStateForTest()
	e.s.hydrateFriendAlertDedup()
	msgs2 := e.s.relayFriendReceipts()
	if len(msgs2) != 0 {
		t.Fatalf("re-relay after restart = %d msgs, want 0 (deduped)", len(msgs2))
	}
}

func TestR14StartFriendMonitorOwnerOnly(t *testing.T) {
	e := newUpdEnv(t)
	resetFriendMonitorForTest()
	defer resetFriendMonitorForTest()

	// Friend mode: no-op.
	t.Setenv("VL_FRIEND_INSTANCE", "1")
	e.s.StartFriendMonitor()
	if friendAlertStop != nil {
		t.Fatalf("friend mode must NOT start the monitor")
	}

	// Owner mode: starts.
	t.Setenv("VL_FRIEND_INSTANCE", "")
	resetFriendMonitorForTest()
	e.s.StartFriendMonitor()
	if friendAlertStop == nil {
		t.Fatalf("owner mode must start the monitor at boot")
	}
}
