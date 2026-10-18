package main

import (
	"strings"
	"testing"
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
