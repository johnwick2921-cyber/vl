package security

import (
	"strings"
	"testing"
	"time"
)

// R2 — SafeHTTPClient hardening: Tailscale CGNAT blocked, friend-mode redirects
// stay https, and the dial-time guard refuses a private host before any bytes.

func TestR2BlocksCGNAT(t *testing.T) {
	if err := ValidateURL("http://100.64.0.1"); err == nil {
		t.Fatalf("ValidateURL must refuse Tailscale CGNAT 100.64.0.1")
	}

	client := SafeHTTPClient(2 * time.Second)
	if resp, err := client.Get("http://100.64.0.1"); err == nil {
		resp.Body.Close()
		t.Fatalf("SafeHTTPClient.Get must refuse CGNAT, got HTTP %d", resp.StatusCode)
	}
}

func TestR2FriendModeRedirectRequiresHTTPS(t *testing.T) {
	// SafeHTTPClient refuses loopback DIALS, so an httptest redirector on
	// 127.0.0.1 can never be reached by it — pin the redirect GUARD directly at
	// the validator the CheckRedirect calls.
	prev := FriendModeEnabled
	FriendModeEnabled = func() bool { return true }
	t.Cleanup(func() { FriendModeEnabled = prev })

	err := redirectValidator("http://127.0.0.1:1")
	if err == nil {
		t.Fatalf("friend-mode redirect to http must be refused")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "https") {
		t.Fatalf("friend-mode redirect error should cite https, got: %v", err)
	}
}

func TestR2OwnerModeRedirectStillBlocksPrivate(t *testing.T) {
	prev := FriendModeEnabled
	FriendModeEnabled = func() bool { return false }
	t.Cleanup(func() { FriendModeEnabled = prev })

	err := redirectValidator("http://127.0.0.1:1")
	if err == nil {
		t.Fatalf("owner-mode redirect to loopback must still be blocked")
	}
	// The owner's guard blocks the private target, not the http scheme.
	if strings.Contains(strings.ToLower(err.Error()), "scheme must be https") {
		t.Fatalf("owner-mode should not cite https (lenient scheme), got: %v", err)
	}
}

func TestR2DialRefusesPrivateHost(t *testing.T) {
	// The rebind/TOCTOU guard lives at DialContext: a host that resolves to a
	// private IP is refused before the dial. localhost resolves to loopback.
	client := SafeHTTPClient(2 * time.Second)
	if resp, err := client.Get("http://localhost"); err == nil {
		resp.Body.Close()
		t.Fatalf("dial to localhost must be refused, got HTTP %d", resp.StatusCode)
	}
}
