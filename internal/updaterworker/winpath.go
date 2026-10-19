package updaterworker

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Windows tool locations (W-UPDATER-WINPATH). The vl-updater user unit's PATH
// has no /mnt/c entries (systemd does not inherit WSL's appendWindowsPath), so
// a bare powershell.exe / tasklist.exe / dotnet.exe name can miss even though
// the tool exists at its fixed Windows location. The nt8 restart (powershell)
// and the tasklist probe exec those bare names through execWindows — the
// common runner. Vars so tests can point them at temp stub executables (never
// the real Windows binaries).
var (
	windowsPowerShellWSL = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
	windowsTasklistWSL   = "/mnt/c/Windows/System32/tasklist.exe"
	windowsDotnetWSL     = "/mnt/c/Program Files/dotnet/dotnet.exe"
)

// windowsToolFixedPath returns the fixed WSL mount for a bare Windows tool name
// the runner may be asked to exec. Read at call time so tests can repoint the
// vars without re-initializing a map.
func windowsToolFixedPath(name string) (string, bool) {
	switch name {
	case "powershell.exe":
		return windowsPowerShellWSL, true
	case "tasklist.exe":
		return windowsTasklistWSL, true
	case "dotnet.exe":
		return windowsDotnetWSL, true
	}
	return "", false
}

// resolveWindowsExe resolves a Windows tool name to a runnable path. A path
// (contains a separator, or is absolute) is passed through untouched — an
// explicit configured path is never remapped. A bare name is looked up on PATH
// first (an interactive WSL shell has the Windows dirs); when LookPath misses,
// the fixed WSL mount is tried. The error names the bare name and the fixed
// path it tried.
func resolveWindowsExe(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("updaterworker: empty windows tool name")
	}
	if strings.ContainsAny(name, `/\`) || filepath.IsAbs(name) {
		return name, nil
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	if fixed, ok := windowsToolFixedPath(name); ok {
		if isRegularFile(fixed) {
			return fixed, nil
		}
		return "", fmt.Errorf("updaterworker: %q not found on PATH and %s does not exist", name, fixed)
	}
	return "", fmt.Errorf("updaterworker: %q not found on PATH", name)
}
