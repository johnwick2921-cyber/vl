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
