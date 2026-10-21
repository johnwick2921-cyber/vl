package deploy

// U7b (DS-101): the friend unit must (a) force friend mode via
// Environment=VL_FRIEND_INSTANCE=1, (b) fail fast if its .env is not readable,
// and (c) wait — bounded by TimeoutStartSec=120 — until API_SERVER_HOST is
// assigned to a local interface before the bot starts. Before this fix a friend
// bot could start before its tunnel IP existed and die on a bind error (or run
// in non-friend mode).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFriendServiceHardening(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "friends", "vl-friend@.service"))
	if err != nil {
		t.Fatalf("read service file: %v", err)
	}
	s := string(b)

	for _, want := range []string{
		"Environment=VL_FRIEND_INSTANCE=1",
		"ExecStartPre=/usr/bin/test -r /home/hoang/vl-friends/%i/.env",
		"TimeoutStartSec=120",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("service file missing %q", want)
		}
	}

	// The wait ExecStartPre must poll `ip -o addr` for API_SERVER_HOST.
	if !strings.Contains(s, "API_SERVER_HOST") || !strings.Contains(s, "ip -o addr show") {
		t.Errorf("service file missing the API_SERVER_HOST wait (ip -o addr) ExecStartPre")
	}

	// Extract the /bin/bash -c payload and prove it is syntactically valid bash.
	payload := extractServiceExecStartPreBash(s)
	if payload == "" {
		t.Fatalf("no wait ExecStartPre found")
	}
	// %i is a systemd specifier; substitute a dummy instance so bash sees a
	// concrete path.
	payload = strings.ReplaceAll(payload, "%i", "tom")
	if err := bashSyntaxCheck(payload); err != nil {
		t.Errorf("wait ExecStartPre is not valid bash: %v\npayload: %s", err, payload)
	}
}

// extractServiceExecStartPreBash returns the single-quoted argument of the
// /bin/bash -c ExecStartPre that waits on API_SERVER_HOST.
func extractServiceExecStartPreBash(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "ExecStartPre=/bin/bash -c") && strings.Contains(line, "API_SERVER_HOST") {
			start := strings.Index(line, "'")
			end := strings.LastIndex(line, "'")
			if start >= 0 && end > start {
				return line[start+1 : end]
			}
		}
	}
	return ""
}

func bashSyntaxCheck(payload string) error {
	cmd := exec.Command("bash", "-n", "-c", payload)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bash -n failed: %v\n%s", err, out)
	}
	return nil
}
