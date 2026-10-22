package updaterworker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"vl/internal/updaterjob"
)

// ── W-FRIEND-HOSTED Segment 5: the friend sweep (flat → restart → boot) ────
//
// The sweep is advisory and never fails the owner's install. The four
// required outcomes, driven through the fakeHost friend seams:
//
//	flat → restarted   (boot OK)
//	open → deferred    (never flat within the budget)
//	failed → restored  (first boot fails, the restore restart recovers)
//	paused → skipped   (never restarted)
//
// MUTANT (spec): ignoring the flat check must make the deferred test RED —
// a friend that is NOT flat would be restarted instead of deferred.

// friendRig builds a worker whose friend surface is configured to a temp
// registry, with the given friend rows.
func friendRig(t *testing.T, rows []friendRecord) (*rig, string) {
	t.Helper()
	root := t.TempDir()
	registryPath := filepath.Join(root, "friends.json")
	if rows != nil {
		b, err := json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(registryPath, b, 0o600); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(registryPath, []byte("[]"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := newRig(t)
	r.cfg.FriendRegistry = registryPath
	r.cfg.FriendHost = "100.64.0.1"
	r.cfg.Budgets.FriendFlat = 30 * time.Second
	r.cfg.Budgets.FriendPoll = time.Second
	r.cfg.Budgets.Watch = 5 * time.Second
	r.w = r.newWorker()
	r.w.notifier = func(text string) {
		r.box.mu.Lock()
		r.box.friendNotifications = append(r.box.friendNotifications, text)
		r.box.mu.Unlock()
	}
	return r, registryPath
}

func TestUpdateFriendsFlatRestarts(t *testing.T) {
	r, _ := friendRig(t, []friendRecord{
		{Name: "tom", WebPort: 18081, State: "active"},
	})
	r.box.friendWebPorts = map[string]int{"tom": 18081}
	r.box.friendFlat = map[string]bool{"tom": true}
	r.box.friendBootResults = map[string][]bool{"tom": {true}}
	r.box.friendBootRev = map[string]string{"tom": "f8faa0bcb96a"}

	receipts, results := r.w.updateFriends(context.Background(), strings40())
	if len(results) != 1 || results[0].State != "restarted" || results[0].Rev != "f8faa0bcb96a" {
		t.Fatalf("results = %+v, want one restarted tom", results)
	}
	if got := r.box.friendRestartCalls["tom"]; got != 1 {
		t.Fatalf("tom restarts = %d, want 1", got)
	}
	if len(receipts) < 2 {
		t.Fatalf("receipts = %d, want the sweep receipt + the friend receipt", len(receipts))
	}
}

func TestUpdateFriendsOpenDefers(t *testing.T) {
	r, _ := friendRig(t, []friendRecord{
		{Name: "tom", WebPort: 18081, State: "active"},
	})
	r.box.friendWebPorts = map[string]int{"tom": 18081}
	r.box.friendFlat = map[string]bool{"tom": false} // never flat

	_, results := r.w.updateFriends(context.Background(), strings40())
	if len(results) != 1 || results[0].State != "deferred" {
		t.Fatalf("results = %+v, want one deferred tom", results)
	}
	if got := r.box.friendRestartCalls["tom"]; got != 0 {
		t.Fatalf("tom restarts = %d, want 0 (never flat → never restarted)", got)
	}
	// The owner is notified of the defer.
	if len(r.box.friendNotifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(r.box.friendNotifications))
	}
}

func TestUpdateFriendsFailedBootRestores(t *testing.T) {
	r, _ := friendRig(t, []friendRecord{
		{Name: "tom", WebPort: 18081, State: "active"},
	})
	r.box.friendWebPorts = map[string]int{"tom": 18081}
	r.box.friendFlat = map[string]bool{"tom": true}
	// First restart's boot FAILS, the restore restart's boot succeeds.
	r.box.friendBootResults = map[string][]bool{"tom": {false, true}}
	r.box.friendBootRev = map[string]string{"tom": "f8faa0bcb96a"}

	_, results := r.w.updateFriends(context.Background(), strings40())
	if len(results) != 1 || results[0].State != "restored-restart" {
		t.Fatalf("results = %+v, want one restored-restart tom", results)
	}
	if got := r.box.friendRestartCalls["tom"]; got != 2 {
		t.Fatalf("tom restarts = %d, want 2 (the restart + the restore)", got)
	}
}

func TestUpdateFriendsPausedSkips(t *testing.T) {
	r, _ := friendRig(t, []friendRecord{
		{Name: "tom", WebPort: 18081, State: "paused"},
	})
	r.box.friendWebPorts = map[string]int{"tom": 18081}

	_, results := r.w.updateFriends(context.Background(), strings40())
	if len(results) != 1 || results[0].State != "skipped" {
		t.Fatalf("results = %+v, want one skipped tom", results)
	}
	if got := r.box.friendRestartCalls["tom"]; got != 0 {
		t.Fatalf("tom restarts = %d, want 0 (paused never restarts)", got)
	}
}

// TestUpdateFriendsR11TradersRule pins the CTO R11 rule: restart when
// flat==true OR the reply carries traders==0 (a zero-trader instance manages
// no position). An older bot omitting "traders" falls back to flat alone.
func TestUpdateFriendsR11TradersRule(t *testing.T) {
	// traders=0, flat=false → RESTARTED (zero traders are safe to move).
	r, _ := friendRig(t, []friendRecord{{Name: "tom", WebPort: 18081, State: "active"}})
	r.box.friendWebPorts = map[string]int{"tom": 18081}
	r.box.friendFlat = map[string]bool{"tom": false}
	r.box.friendHasTraders = map[string]bool{"tom": true}
	r.box.friendTraders = map[string]int{"tom": 0}
	r.box.friendBootResults = map[string][]bool{"tom": {true}}
	r.box.friendBootRev = map[string]string{"tom": "f8faa0bcb96a"}
	_, results := r.w.updateFriends(context.Background(), strings40())
	if len(results) != 1 || results[0].State != "restarted" {
		t.Fatalf("traders=0 → results = %+v, want one restarted tom", results)
	}

	// traders=1, flat=false → DEFERRED (a real trader, not flat).
	r, _ = friendRig(t, []friendRecord{{Name: "tom", WebPort: 18081, State: "active"}})
	r.box.friendWebPorts = map[string]int{"tom": 18081}
	r.box.friendFlat = map[string]bool{"tom": false}
	r.box.friendHasTraders = map[string]bool{"tom": true}
	r.box.friendTraders = map[string]int{"tom": 1}
	_, results = r.w.updateFriends(context.Background(), strings40())
	if len(results) != 1 || results[0].State != "deferred" {
		t.Fatalf("traders=1 flat=false → results = %+v, want one deferred tom", results)
	}
	if got := r.box.friendRestartCalls["tom"]; got != 0 {
		t.Fatalf("traders=1 flat=false: tom restarts = %d, want 0", got)
	}

	// "traders" MISSING, flat=false → DEFERRED (older bot: flat alone).
	r, _ = friendRig(t, []friendRecord{{Name: "tom", WebPort: 18081, State: "active"}})
	r.box.friendWebPorts = map[string]int{"tom": 18081}
	r.box.friendFlat = map[string]bool{"tom": false}
	_, results = r.w.updateFriends(context.Background(), strings40())
	if len(results) != 1 || results[0].State != "deferred" {
		t.Fatalf("missing traders, flat=false → results = %+v, want one deferred tom", results)
	}
	if got := r.box.friendRestartCalls["tom"]; got != 0 {
		t.Fatalf("missing traders, flat=false: tom restarts = %d, want 0", got)
	}
}

func TestUpdateFriendsNoRegistryNoSweep(t *testing.T) {
	r := newRig(t)
	r.cfg.FriendRegistry = ""
	r.cfg.FriendHost = "100.64.0.1"
	r.w = r.newWorker()
	receipts, results := r.w.updateFriends(context.Background(), strings40())
	if receipts != nil || results != nil {
		t.Fatalf("no friend surface → no sweep, got receipts=%d results=%d", len(receipts), len(results))
	}
}

func TestUpdateFriendsRegistryReadErrorIsAdvisory(t *testing.T) {
	r, registryPath := friendRig(t, nil)
	// Corrupt the registry: the sweep records + notifies, never errors, and
	// (R8) persists a synthetic failed entry so api/friend_alert.go relays it.
	if err := os.WriteFile(registryPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipts, results := r.w.updateFriends(context.Background(), strings40())
	if len(receipts) == 0 {
		t.Fatalf("corrupt registry → a receipt, got %d", len(receipts))
	}
	if len(results) != 1 || results[0].Name != "*" || results[0].State != "failed" || results[0].Note != "friend sweep disabled: registry unreadable" {
		t.Fatalf("corrupt registry → synthetic {name:*, state:failed} entry, got %+v", results)
	}
	if len(r.box.friendNotifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(r.box.friendNotifications))
	}
}

// TestUpdateFriendsNoTunnelIPNotifies pins U13 + R8: a registry WITH active
// friends but no TUNNEL_IP (FriendHost) must NOT vanish silently — the owner
// gets one notification, a receipt naming the reason, AND a synthetic
// friends[] entry {name:"*", state:"failed"} that api/friend_alert.go relays
// to the owner's Telegram (a notify alone only logs).
func TestUpdateFriendsNoTunnelIPNotifies(t *testing.T) {
	r, _ := friendRig(t, []friendRecord{
		{Name: "tom", WebPort: 18081, State: "active"},
	})
	r.w.cfg.FriendHost = "" // TUNNEL_IP unset

	receipts, results := r.w.updateFriends(context.Background(), strings40())
	if len(results) != 1 {
		t.Fatalf("results = %+v, want one synthetic failed entry", results)
	}
	if results[0].Name != "*" || results[0].State != "failed" || results[0].Note != "friend sweep disabled: TUNNEL_IP unset" {
		t.Fatalf("synthetic entry = %+v, want {name:*, state:failed, note:\"friend sweep disabled: TUNNEL_IP unset\"}", results[0])
	}
	if len(receipts) != 1 {
		t.Fatalf("receipts = %d, want exactly one", len(receipts))
	}
	if receipts[0].Step != "friend sweep disabled: TUNNEL_IP unset" {
		t.Fatalf("receipt step = %q, want %q", receipts[0].Step, "friend sweep disabled: TUNNEL_IP unset")
	}
	if len(r.box.friendNotifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(r.box.friendNotifications))
	}
	if got := r.box.friendRestartCalls["tom"]; got != 0 {
		t.Fatalf("tom restarts = %d, want 0 (sweep disabled, never restarted)", got)
	}
}

// TestUpdateFriendsNoTunnelIPNoActiveFriends pins the U13 boundary: an unset
// TUNNEL_IP with NO active friends stays silent (nothing to sweep), matching
// the no-registry path.
func TestUpdateFriendsNoTunnelIPNoActiveFriends(t *testing.T) {
	r, _ := friendRig(t, []friendRecord{
		{Name: "tom", WebPort: 18081, State: "paused"},
	})
	r.w.cfg.FriendHost = ""
	receipts, results := r.w.updateFriends(context.Background(), strings40())
	if receipts != nil || results != nil {
		t.Fatalf("no active friends + no TUNNEL_IP → nothing, got receipts=%d results=%d", len(receipts), len(results))
	}
}

// strings40 is a 40-char lowercase hex sha for the friend boot check.
func strings40() string { return "0123456789abcdef0123456789abcdef01234567" }

// TestOSHostFriendFlat drives the PRODUCTION FriendFlat adapter (the real
// OSHost HTTP + JSON path) against an httptest server: flat:true with a
// traders count, flat:false with a traders count, a 404 (friend mode off)
// fail-closes to not-flat, and a reply WITHOUT "traders" (an older bot) is
// detected (HasTraders false, R11).
func TestOSHostFriendFlat(t *testing.T) {
	var mu sync.Mutex
	flat := true
	traders := 0
	hasTraders := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"status":"ok","revision":"f8faa0bcb96a"}`)
		case "/api/friend/flat":
			mu.Lock()
			defer mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if hasTraders {
				fmt.Fprintf(w, `{"flat":%t,"traders":%d,"at":%d}`, flat, traders, time.Now().UnixMilli())
			} else {
				fmt.Fprintf(w, `{"flat":%t,"at":%d}`, flat, time.Now().UnixMilli())
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	h := OSHost{}

	v, err := h.FriendFlat(context.Background(), srv.URL)
	if err != nil || !v.Flat || !v.HasTraders || v.Traders != 0 {
		t.Fatalf("flat:true traders:0 → (%+v, %v), want Flat HasTraders Traders=0", v, err)
	}
	mu.Lock()
	flat = false
	traders = 1
	mu.Unlock()
	v, err = h.FriendFlat(context.Background(), srv.URL)
	if err != nil || v.Flat || !v.HasTraders || v.Traders != 1 {
		t.Fatalf("flat:false traders:1 → (%+v, %v), want !Flat HasTraders Traders=1", v, err)
	}
	// A server without /api/friend/flat (friend mode off) fails closed.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			fmt.Fprint(w, `{"status":"ok"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv2.Close()
	v, err = h.FriendFlat(context.Background(), srv2.URL)
	if err != nil || v.Flat || v.HasTraders {
		t.Fatalf("missing /api/friend/flat → (%+v, %v), want zero view, nil", v, err)
	}
	// An OLDER bot reply without "traders" is detected.
	mu.Lock()
	hasTraders = false
	flat = false
	mu.Unlock()
	v, err = h.FriendFlat(context.Background(), srv.URL)
	if err != nil || v.Flat || v.HasTraders {
		t.Fatalf("missing traders field → (%+v, %v), want !Flat !HasTraders", v, err)
	}
}

// TestCatchupCompleteNeverRestartsFriends pins the 2026-10-21 18:31:08
// catch-up bug at the production call site (stepReleaseHold): a completed
// catch-up job (no Release) restarted every friend and then failed the boot
// check with an empty sha. MUTANT: drop `!j.Catchup && j.Release != nil` from
// the gate in steps.go (the updateFriends sha guard is mutated out too) and
// this goes RED.
func TestCatchupCompleteNeverRestartsFriends(t *testing.T) {
	r, _ := friendRig(t, []friendRecord{{Name: "tom", WebPort: 18081, State: "active"}})
	r.box.friendWebPorts = map[string]int{"tom": 18081}
	r.box.friendFlat = map[string]bool{"tom": true}
	r.box.friendBootResults = map[string][]bool{"tom": {true}}
	j, err := updaterjob.NewCatchup("catchup-pin1", r.w.host.Now())
	if err != nil {
		t.Fatal(err)
	}
	j.State = updaterjob.StateComplete
	res := r.w.stepReleaseHold(context.Background(), j)
	if got := r.box.friendRestartCalls["tom"]; got != 0 {
		t.Fatalf("catch-up complete restarted tom %d times, want 0 (res.err=%v)", got, res.err)
	}
	if res.set != nil {
		t.Fatalf("catch-up must not persist friends[]")
	}
}

// TestUpdateFriendsEmptySHANeverRestarts pins the defense layer.
func TestUpdateFriendsEmptySHANeverRestarts(t *testing.T) {
	r, _ := friendRig(t, []friendRecord{{Name: "tom", WebPort: 18081, State: "active"}})
	r.box.friendWebPorts = map[string]int{"tom": 18081}
	r.box.friendFlat = map[string]bool{"tom": true}
	_, results := r.w.updateFriends(context.Background(), "")
	if got := r.box.friendRestartCalls["tom"]; got != 0 {
		t.Fatalf("restarted with empty sha %d times", got)
	}
	if len(results) != 1 || results[0].State != "failed" {
		t.Fatalf("results = %+v", results)
	}
}
