package updaterworker

import (
	"context"
	"os"
	"os/exec"
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

// repointFixed points the fixed-path var for tool at p and returns a restore.
func repointFixed(tool, p string) func() {
	switch tool {
	case "powershell.exe":
		prev := windowsPowerShellWSL
		windowsPowerShellWSL = p
		return func() { windowsPowerShellWSL = prev }
	case "tasklist.exe":
		prev := windowsTasklistWSL
		windowsTasklistWSL = p
		return func() { windowsTasklistWSL = prev }
	case "dotnet.exe":
		prev := windowsDotnetWSL
		windowsDotnetWSL = p
		return func() { windowsDotnetWSL = prev }
	}
	panic("unknown tool " + tool)
}

func TestExecWindowsResolvesBareToolViaFixedPath(t *testing.T) {
	// tasklist.exe is essential: deciding whether the old NinjaTrader.exe has
	// fully exited reads it, so it must resolve on a Linux-only PATH too.
	for _, tool := range []string{"powershell.exe", "tasklist.exe", "dotnet.exe"} {
		t.Run(tool, func(t *testing.T) {
			dir := t.TempDir()
			stub := writeToolStub(t, dir, tool, "from-fixed-stub")
			t.Cleanup(repointFixed(tool, stub))
			t.Setenv("PATH", "/usr/bin:/bin") // Linux-only: no Windows dirs
			out, err := execWindows(context.Background(), tool, "arg")
			if err != nil {
				t.Fatalf("execWindows(%s): %v", tool, err)
			}
			if !strings.Contains(string(out), "from-fixed-stub") {
				t.Fatalf("execWindows(%s) must run the fixed-path stub, output %q", tool, out)
			}
		})
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

// W-UPDATER-WINPATH CI fix: the rig must put HERMETIC powershell.exe/dotnet.exe
// on PATH (never /mnt/c), so addonbuild.Build's production resolveWindowsExe
// finds them via LookPath on a Linux box with no Windows mount.
func TestHarnessWindowsToolsAreHermetic(t *testing.T) {
	newRig(t)
	for _, tool := range []string{"powershell.exe", "dotnet.exe"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("hermetic %s not on PATH after newRig: %v", tool, err)
		}
		if strings.Contains(p, "/mnt/c/") {
			t.Fatalf("%s resolved to a real Windows path %q — the harness must use its hermetic stub", tool, p)
		}
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
