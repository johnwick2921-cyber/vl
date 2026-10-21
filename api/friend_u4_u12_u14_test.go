package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestU12AddRefusesBadTunnelIP(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	prev := tunnelIPOK
	tunnelIPOK = func() (string, bool) { return "", false }
	t.Cleanup(func() { tunnelIPOK = prev })

	w := friendCall(t, e, "POST", "/api/owner/friends", `{"name":"tom"}`, e.tok)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("add with bad TUNNEL_IP = %d, want 400 (%s)", w.Code, w.Body.String())
	}
}

func TestU12RotateRefusesBadTunnelIP(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"x","state":"active","created_at":"2026-10-18T00:00:00Z","tailscale_device_id":""}]`)
	prev := tunnelIPOK
	tunnelIPOK = func() (string, bool) { return "9.9.9.9", false }
	t.Cleanup(func() { tunnelIPOK = prev })

	w := friendCall(t, e, "POST", "/api/owner/friends/tom/rotate", "", e.tok)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("rotate with bad TUNNEL_IP = %d, want 400 (%s)", w.Code, w.Body.String())
	}
}

func TestU14RevokedRowRefusesActions(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"x","state":"revoked","created_at":"2026-10-18T00:00:00Z","tailscale_device_id":""}]`)

	for _, act := range []string{"pause", "resume", "rotate"} {
		w := friendCall(t, e, "POST", "/api/owner/friends/tom/"+act, "", e.tok)
		if w.Code != http.StatusConflict {
			t.Errorf("%s on a revoked row = %d, want 409", act, w.Code)
		}
	}
}

func TestU4RevokeTagLookupWhenNoDeviceID(t *testing.T) {
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

	var mu sync.Mutex
	var gotList, gotDelete string
	tsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if r.Method == http.MethodGet {
			gotList = r.URL.Path
		} else {
			gotDelete = r.URL.Path
		}
		mu.Unlock()
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"devices":[{"id":"dev-tom-1","name":"tom","tags":["tag:tom"]}]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer tsSrv.Close()
	oldBase := tailscaleAPIBase
	tailscaleAPIBase = tsSrv.URL
	t.Cleanup(func() { tailscaleAPIBase = oldBase })
	t.Setenv("TAILSCALE_API_KEY", "fake-key")

	w := friendCall(t, e, "POST", "/api/owner/friends/tom/revoke", "", e.tok)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	mu.Lock()
	list, del := gotList, gotDelete
	mu.Unlock()
	if list != "/api/v2/tailnet/-/devices" {
		t.Fatalf("tag lookup path = %q, want /api/v2/tailnet/-/devices", list)
	}
	if del != "/api/v2/device/dev-tom-1" {
		t.Fatalf("device delete path = %q, want /api/v2/device/dev-tom-1", del)
	}
	var resp struct {
		Tailscale string `json:"tailscale"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Tailscale != "deleted" {
		t.Fatalf("tailscale = %q, want deleted", resp.Tailscale)
	}
}

func TestU4RevokeAlreadyRevokedSkipsRemove(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"x","state":"revoked","created_at":"2026-10-18T00:00:00Z","tailscale_device_id":"dev123"}]`)
	// No tom/ folder — remove.sh must NOT run (it would fail / move the root).

	tsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer tsSrv.Close()
	oldBase := tailscaleAPIBase
	tailscaleAPIBase = tsSrv.URL
	t.Cleanup(func() { tailscaleAPIBase = oldBase })
	t.Setenv("TAILSCALE_API_KEY", "fake-key")

	w := friendCall(t, e, "POST", "/api/owner/friends/tom/revoke", "", e.tok)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke on revoked row = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	// The folder was never touched (there was none); the tailscale step ran.
	var resp struct {
		Tailscale string `json:"tailscale"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Tailscale != "deleted" {
		t.Fatalf("tailscale = %q, want deleted", resp.Tailscale)
	}
	if strings.Contains(w.Body.String(), "revoked locally") {
		t.Fatalf("unexpected non-200 message: %s", w.Body.String())
	}
}
