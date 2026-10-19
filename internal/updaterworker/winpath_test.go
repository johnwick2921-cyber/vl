package updaterworker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeToolStub writes an executable that prints out on stdout — a harmless
// stand-in for powershell.exe / tasklist.exe. Never a real Windows binary.
func writeToolStub(t *testing.T, dir, name, out string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho \""+out+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// W-UPDATER-WINPATH: execWindows is the common runner for powershell.exe /
// tasklist.exe. These tests call the PRODUCTION execWindows with the fixed-path
// vars repointed at harmless stubs — never a real Windows binary. A Linux-only
// PATH simulates the user unit (systemd does not inherit appendWindowsPath).

func TestExecWindowsResolvesBareToolViaFixedPath(t *testing.T) {
	dir := t.TempDir()
	stub := writeToolStub(t, dir, "powershell.exe", "from-fixed-stub")
	prev := windowsPowerShellWSL
	windowsPowerShellWSL = stub
	t.Cleanup(func() { windowsPowerShellWSL = prev })
	t.Setenv("PATH", "/usr/bin:/bin") // Linux-only: no powershell.exe on PATH
	out, err := execWindows(context.Background(), "powershell.exe", "-NoProfile", "-Command", "hi")
	if err != nil {
		t.Fatalf("execWindows: %v", err)
	}
	if !strings.Contains(string(out), "from-fixed-stub") {
		t.Fatalf("execWindows must run the fixed-path stub, output %q", out)
	}
}

func TestExecWindowsPreservesExplicitPath(t *testing.T) {
	dir := t.TempDir()
	fixedStub := writeToolStub(t, dir, "powershell.exe", "from-fixed-stub")
	explicit := writeToolStub(t, dir, "mytool.exe", "from-explicit-stub")
	prev := windowsPowerShellWSL
	windowsPowerShellWSL = fixedStub
	t.Cleanup(func() { windowsPowerShellWSL = prev })
	t.Setenv("PATH", "/usr/bin:/bin")
	out, err := execWindows(context.Background(), explicit, "arg")
	if err != nil {
		t.Fatalf("execWindows: %v", err)
	}
	if !strings.Contains(string(out), "from-explicit-stub") {
		t.Fatalf("an explicit path must be used verbatim, output %q", out)
	}
	if strings.Contains(string(out), "from-fixed-stub") {
		t.Fatalf("an explicit path must NOT be remapped to the fixed location")
	}
}

func TestExecWindowsPathLookupWinsOverFixedPath(t *testing.T) {
	dir := t.TempDir()
	writeToolStub(t, dir, "powershell.exe", "from-path-stub")
	fixedStub := writeToolStub(t, dir, "fixed-powershell.exe", "from-fixed-stub")
	prev := windowsPowerShellWSL
	windowsPowerShellWSL = fixedStub
	t.Cleanup(func() { windowsPowerShellWSL = prev })
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin:/bin")
	out, err := execWindows(context.Background(), "powershell.exe")
	if err != nil {
		t.Fatalf("execWindows: %v", err)
	}
	if !strings.Contains(string(out), "from-path-stub") {
		t.Fatalf("a bare name found on PATH must be used, output %q", out)
	}
	if strings.Contains(string(out), "from-fixed-stub") {
		t.Fatalf("PATH lookup must win over the fixed location")
	}
}

func TestExecWindowsReportsResolutionError(t *testing.T) {
	prev := windowsPowerShellWSL
	windowsPowerShellWSL = "/nonexistent/fixed/powershell.exe"
	t.Cleanup(func() { windowsPowerShellWSL = prev })
	t.Setenv("PATH", "/usr/bin:/bin")
	_, err := execWindows(context.Background(), "powershell.exe", "-NoProfile")
	if err == nil {
		t.Fatal("execWindows must report the unresolved tool")
	}
	if !strings.Contains(err.Error(), "powershell.exe") || !strings.Contains(err.Error(), "/nonexistent/fixed/powershell.exe") {
		t.Fatalf("the error must name the bare name and the fixed path: %v", err)
	}
}
