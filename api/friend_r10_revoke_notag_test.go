package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R10 — revoke with no stored device id AND no device carrying tag:<name>
// returns a distinct surfaced message (200 + warning), never a 500 retry loop.

func TestR10RevokeNoTagDeviceDistinct(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	root := os.Getenv("VL_FRIENDS_ROOT")
	writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"x","state":"active","created_at":"2026-10-18T00:00:00Z","tailscale_device_id":""}]`)
	if err := os.MkdirAll(filepath.Join(root, "tom"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFriendScript(t, e, "remove.sh", `#!/bin/bash
set -euo pipefail
mkdir -p "$VL_FRIENDS_ROOT/trash"
mv "$VL_FRIENDS_ROOT/$1" "$VL_FRIENDS_ROOT/trash/$1"
`)

	// Tailscale API: an EMPTY device list (no device carries tag:tom).
	tsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"devices":[]}`))
			return
		}
		t.Errorf("DELETE should not be attempted when no tag device exists")
		w.WriteHeader(http.StatusNotFound)
	}))
	defer tsSrv.Close()
	oldBase := tailscaleAPIBase
	tailscaleAPIBase = tsSrv.URL
	t.Cleanup(func() { tailscaleAPIBase = oldBase })
	t.Setenv("TAILSCALE_API_KEY", "fake-key")

	w := friendCall(t, e, "POST", "/api/owner/friends/tom/revoke", "", e.tok)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke with no tag device = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "no Tailscale device carries tag:tom") {
		t.Fatalf("expected the distinct no-tag message, got: %s", body)
	}
	if strings.Contains(body, "revoked locally but") {
		t.Fatalf("must not be the 500 retry message: %s", body)
	}
}
