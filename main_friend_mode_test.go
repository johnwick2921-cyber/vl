package main

import (
	"strings"
	"testing"

	"vl/config"
)

// W-FRIEND-HOSTED Segment 2 (DS-102) — the friend bind rail and the boot line.
//
// The 0.0.0.0 / :: / empty refusal is the safety check the mutant test removes:
// deleting any one branch of isAllInterfacesHost must turn a case here RED.

func TestValidateFriendBindRefusesAllInterfacesAndEmpty(t *testing.T) {
	cases := []struct {
		name       string
		apiHost    string
		ntListen   string
		wantRefuse bool
	}{
		{"ok specific hosts", "100.64.0.2", "100.64.0.2:36981", false},
		{"api all v4", "0.0.0.0", "100.64.0.2:36981", true},
		{"api all v6", "::", "100.64.0.2:36981", true},
		{"api empty", "", "100.64.0.2:36981", true},
		{"nt all v4", "100.64.0.2", "0.0.0.0:36981", true},
		{"nt all v6", "100.64.0.2", "[::]:36981", true},
		{"nt empty", "100.64.0.2", "", true},
		{"nt bare all v6", "100.64.0.2", "::", true},
		{"nt loopback ok", "127.0.0.1", "127.0.0.1:36974", false},
		{"api hostname refused (U11)", "friendbox", "100.64.0.2:36981", true},
		{"nt hostname refused (U11)", "100.64.0.2", "friendbox:36981", true},
		{"nt v6 literal ok", "100.64.0.2", "[fd00::2]:36981", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateFriendBind(c.apiHost, c.ntListen)
			if c.wantRefuse && err == nil {
				t.Fatalf("validateFriendBind(%q, %q) = nil, want refusal", c.apiHost, c.ntListen)
			}
			if !c.wantRefuse && err != nil {
				t.Fatalf("validateFriendBind(%q, %q) = %v, want nil", c.apiHost, c.ntListen, err)
			}
		})
	}
}

func TestIsAllInterfacesHost(t *testing.T) {
	cases := []struct {
		h    string
		want bool
	}{
		{"", true},
		{"0.0.0.0", true},
		{"::", true},
		{"  ::  ", true},
		{"127.0.0.1", false},
		{"::1", false},
		{"100.64.0.2", false},
		{"friendbox", false},
	}
	for _, c := range cases {
		if got := isAllInterfacesHost(c.h); got != c.want {
			t.Errorf("isAllInterfacesHost(%q) = %v, want %v", c.h, got, c.want)
		}
	}
}

func TestFriendHardGuarantees(t *testing.T) {
	custom := &config.Config{JWTSecret: "custom-friend-secret-0123456789abcdef"}
	def := &config.Config{JWTSecret: config.InsecureDefaultJWTSecret}

	cases := []struct {
		name string
		cfg  *config.Config
		key  string
		want string // "" = ok
	}{
		{"all set", custom, "abc123", ""},
		{"empty key", custom, "  ", "NT_CONNECT_KEY"},
		{"default jwt", def, "abc123", "JWT_SECRET"},
		{"nil cfg", nil, "abc123", "JWT_SECRET"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := friendHardGuarantees(c.cfg, c.key)
			if c.want == "" && got != "" {
				t.Fatalf("friendHardGuarantees = %q, want ok", got)
			}
			if c.want != "" && !strings.Contains(got, c.want) {
				t.Fatalf("friendHardGuarantees = %q, want it to name %q", got, c.want)
			}
		})
	}
}

func TestFriendBootLineReadsEveryField(t *testing.T) {
	got := friendBootLine("tom", "100.64.0.2", 18081, "100.64.0.2:36981")
	want := "👥 friend instance tom · web 100.64.0.2:18081 · nt 100.64.0.2:36981"
	if got != want {
		t.Fatalf("friendBootLine = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, "👥 friend instance tom") {
		t.Fatalf("boot line must name the friend: %q", got)
	}
}
