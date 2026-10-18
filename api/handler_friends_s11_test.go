package api

// B5 S11 PROOF (DS-105): Revoke completes end to end within 10 s, wired at the
// PRODUCTION handler path (POST /api/owner/friends/:name/revoke →
// handleOwnerFriendAction, case "revoke").
//
// S11: "Revoke works within 10 s: the unit is stopped, the Tailscale device
// removed, the key invalid, the folder moved (not deleted)."
//
// The four facts, each pinned:
//
//	unit stopped      — the fake remove.sh records the systemctl --user stop
//	                    (the real remove.sh does it in production mode)
//	Tailscale called  — tailscaleAPIBase is pointed at a fake server that
//	                    records the DELETE for the device id
//	key invalid       — the friend's .env (which carried the old key) is MOVED
//	                    away with the folder, and the unit is stopped, so a
//	                    hello with the old key has no listener and no key to
//	                    match. The refusal itself is pinned by
//	                    provider/ninjatrader TestConnectKeyWrongAndMissingRefused.
//	folder moved      — $ROOT/<name> is gone and its .env survives in trash
//
// The whole revoke is bounded: the wall clock is measured around the handler.

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestB5S11RevokeEndToEndWithin10s(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)

	root := os.Getenv("VL_FRIENDS_ROOT")
	oldKey := strings.Repeat("ab", 32) // 64 hex, the friend's connect key
	sum := sha256.Sum256([]byte(oldKey))
	keySHA := hex.EncodeToString(sum[:])

	// A provisioned friend: an active registry row + a folder holding its .env.
	writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"`+keySHA+`","state":"active","created_at":"2026-10-18T00:00:00Z","tailscale_device_id":"dev123"}]`)
	if err := os.MkdirAll(filepath.Join(root, "tom"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tom", ".env"),
		[]byte("NT_CONNECT_KEY="+oldKey+"\nAPI_SERVER_PORT=18081\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A fake remove.sh that models the PRODUCTION one: it records the unit
	// stop, MOVES the folder to trash (never deletes), and marks the registry
	// entry revoked. The handler calls it as `remove.sh <name>` (no --root),
	// so it resolves the friend folder from VL_FRIENDS_ROOT.
	writeFriendScript(t, e, "remove.sh", `#!/bin/bash
set -euo pipefail
echo stopped > "$VL_FRIENDS_ROOT/.stopped-$1"
mkdir -p "$VL_FRIENDS_ROOT/trash"
mv "$VL_FRIENDS_ROOT/$1" "$VL_FRIENDS_ROOT/trash/$1"
python3 - "$VL_FRIENDS_ROOT/friends.json" "$1" <<'PY'
import json, sys
reg, name = sys.argv[1:3]
rows = json.load(open(reg))
for r in rows:
    if r.get("name") == name:
        r["state"] = "revoked"
json.dump(rows, open(reg, "w"), indent=2)
PY
echo moved
`)

	// A fake Tailscale API that records the device delete. The handler's key is
	// set, so tailscaleDeleteDevice really makes the call (never a silent skip).
	var mu sync.Mutex
	var gotPath, gotAuth string
	tsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		if r.Method != http.MethodDelete {
			t.Errorf("tailscale delete method = %s, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer tsSrv.Close()
	oldBase := tailscaleAPIBase
	tailscaleAPIBase = tsSrv.URL
	t.Cleanup(func() { tailscaleAPIBase = oldBase })
	t.Setenv("TAILSCALE_API_KEY", "fake-key")

	start := time.Now()
	w := friendCall(t, e, "POST", "/api/owner/friends/tom/revoke", "", e.tok)
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("revoke: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	if elapsed > 10*time.Second {
		t.Fatalf("revoke took %s — S11 requires ≤ 10 s", elapsed)
	}

	// 1. The unit was stopped (the fake remove.sh recorded it).
	if _, err := os.Stat(filepath.Join(root, ".stopped-tom")); err != nil {
		t.Fatalf("remove.sh must record the unit stop: %v", err)
	}

	// 2. The Tailscale device delete was called for dev123 with the key auth.
	mu.Lock()
	path, auth := gotPath, gotAuth
	mu.Unlock()
	if path != "/api/v2/device/dev123" {
		t.Fatalf("tailscale delete path = %q, want /api/v2/device/dev123", path)
	}
	if auth != basicAuth("fake-key", "") {
		t.Fatalf("tailscale delete auth = %q, want the key basic auth", auth)
	}

	// 3. The key is invalid: the folder (and its .env carrying the old key) is
	// gone from the friend root, and the registry no longer marks tom active —
	// a hello with the old key has no listener and no matching key. The hello
	// refusal itself is pinned by TestConnectKeyWrongAndMissingRefused.
	if _, err := os.Stat(filepath.Join(root, "tom")); !os.IsNotExist(err) {
		t.Fatalf("friend folder must be moved away: stat err = %v", err)
	}

	// 4. The folder was MOVED, never deleted: the .env with the old key
	// survives in trash.
	movedEnv := filepath.Join(root, "trash", "tom", ".env")
	b, err := os.ReadFile(movedEnv)
	if err != nil {
		t.Fatalf("the moved .env must survive in trash (never deleted): %v", err)
	}
	if !strings.Contains(string(b), "NT_CONNECT_KEY="+oldKey) {
		t.Fatalf("the moved .env must still carry the old key (it died with the folder): %s", string(b))
	}

	// The registry entry is revoked (the key's SHA is no longer authoritative).
	reg := filepath.Join(root, "friends.json")
	rb, err := os.ReadFile(reg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rb), `"state": "revoked"`) {
		t.Fatalf("registry must mark tom revoked: %s", string(rb))
	}
}

func basicAuth(user, pass string) string {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth(user, pass)
	return req.Header.Get("Authorization")
}
