//go:build windows

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestShortcutScriptPowerShellParses — B1: the generated script must PARSE as
// real PowerShell (the owner's box rejected the old "};else{" split). This runs
// only on Windows, where powershell is available.
func TestShortcutScriptPowerShellParses(t *testing.T) {
	ps := shortcutScript()
	check := "$null = [scriptblock]::Create($args[0]); 'ok'"
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", check, ps).CombinedOutput()
	if err != nil {
		t.Fatalf("shortcut script does not parse as PowerShell: %v\n%s\nscript:\n%s", err, strings.TrimSpace(string(out)), ps)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("shortcut script parse check failed: %s", strings.TrimSpace(string(out)))
	}
}
