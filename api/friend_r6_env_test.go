package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R6 — the Friends page drives `systemctl --user`, which needs the user-bus env
// a SYSTEM unit lacks. Pin the env at the PRODUCTION systemctlUser call site by
// running it against a fake systemctl that dumps its environment.

func TestR6SystemctlUserCarriesUserBusEnv(t *testing.T) {
	// This test must run the REAL default systemctlUser (not a friendTestEnv
	// fake). The var holds the production closure at package init; save/restore
	// it so a concurrent test's override never leaks.
	prev := systemctlUser
	defer func() { systemctlUser = prev }()

	dump := filepath.Join(t.TempDir(), "env.dump")
	bin := t.TempDir()
	script := filepath.Join(bin, "systemctl")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nenv > \"$ENV_DUMP\"\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake systemctl: %v", err)
	}
	t.Setenv("ENV_DUMP", dump)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", "") // absent
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")

	if err := systemctlUser("show-environment", ""); err != nil {
		t.Fatalf("systemctlUser: %v", err)
	}
	b, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("read env dump: %v", err)
	}
	s := string(b)
	uid := os.Getuid()
	for _, want := range []string{
		fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", uid),
		fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", uid),
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("env missing %q — full env:\n%s", want, s)
		}
	}

	if got := FriendControlSelfCheck(); got != "✓" {
		t.Fatalf("self-check with a reachable fake bus = %q, want ✓", got)
	}
}
