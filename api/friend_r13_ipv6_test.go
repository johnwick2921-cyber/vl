package api

import (
	"testing"
)

// R13 — tunnelIPOK rejects IPv6 (IPv4 only), so every URL join
// http://TUNNEL_IP:port stays a valid literal without brackets.

func TestR13TunnelIPOKRejectsIPv6(t *testing.T) {
	// Capture the production closure (the var holds it at package init) and run
	// it directly against an IPv6 loopback that IS assigned on this box — the
	// rejection must come from the R13 check, not from a missing interface.
	prod := tunnelIPOK
	defer func() { tunnelIPOK = prod }()

	t.Setenv("TUNNEL_IP", "::1")
	if _, ok := prod(); ok {
		t.Fatalf("tunnelIPOK must reject IPv6 ::1")
	}

	t.Setenv("TUNNEL_IP", "fd7a:115c:a1e0::1")
	if _, ok := prod(); ok {
		t.Fatalf("tunnelIPOK must reject IPv6 ULA")
	}
}
