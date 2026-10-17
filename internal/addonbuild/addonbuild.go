// Package addonbuild compiles the NinjaTrader 8 VL.AddOn.dll OUTSIDE NinjaTrader
// and stages it into the NT8 Custom folder, so a one-click update can ship the
// AddOn without a terminal, an agent, or an F5.
//
// W-ADDON-ONE-CLICK (owner order 2026-10-17): "update = one click, no terminal,
// no agent - and the NinjaTrader file too, for vl AND vltrader".
//
// THE ONE RULE: nothing in this package deletes. The old DLL is MOVED to a
// backups directory; the build dir is a worker-made dir. Every test uses fake
// dirs and a fake Run - no test ever touches the real NT8 folder or starts NT8.
//
// PATH CONTRACT: every Config path is a WSL path (what Detect returns, e.g.
// /mnt/c/Users/<u>/Documents/NinjaTrader 8). Build converts them to Windows
// paths when rendering the csproj, because the csproj and the -o dir are
// consumed by dotnet.exe (a Windows process) via Run. Place and Detect operate
// purely on WSL paths.
package addonbuild

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is everything Build/Place need that the caller supplies. All path
// fields are WSL paths.
//
//	DocsDir   - Documents\NinjaTrader 8 directory (bin/Custom must exist)
//	Exe       - NinjaTrader.exe path (Core/Gui refs derive from its dir)
//	DotnetExe - dotnet.exe to build with (passed to Run verbatim)
//	SourceDir - release's ninjascript/*.cs directory
//	WorkDir   - worker-made build dir (NEVER the NT8 folder)
//	Run       - execs a subprocess so tests can substitute a fake
type Config struct {
	DocsDir   string
	Exe       string
	DotnetExe string
	SourceDir string
	WorkDir   string
	Run       func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Sentinel errors returned by this package.
var (
	ErrNoNT8Dirs       = errors.New("addonbuild: no Documents/NinjaTrader 8 with bin/Custom found")
	ErrMultipleNT8Dirs = errors.New("addonbuild: multiple Documents/NinjaTrader 8 candidates")
	ErrNoSourceFiles   = errors.New("addonbuild: no .cs files in source dir")
	ErrNoDocsDir       = errors.New("addonbuild: DocsDir empty or missing bin/Custom")
	ErrNoRun           = errors.New("addonbuild: Run is nil")
	ErrBuildNoDll      = errors.New("addonbuild: build produced no VL.AddOn.dll")
)

// nt8DocsGlob is the WSL glob Detect scans. A package var so tests can point it
// at a temp dir.
var nt8DocsGlob = "/mnt/c/Users/*/Documents/NinjaTrader 8"

// Detect returns the single Documents/NinjaTrader 8 directory (WSL path) that
// has a bin/Custom folder. More than one candidate is an error naming them;
// zero is an error.
func Detect() (docsDir string, err error) {
	matches, _ := filepath.Glob(nt8DocsGlob)
	var candidates []string
	for _, m := range matches {
		if isDir(filepath.Join(m, "bin", "Custom")) {
			candidates = append(candidates, m)
		}
	}
	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("%w: glob %q matched %d dir(s), none with bin/Custom", ErrNoNT8Dirs, nt8DocsGlob, len(matches))
	case 1:
		return candidates[0], nil
	default:
		return "", fmt.Errorf("%w: %s", ErrMultipleNT8Dirs, strings.Join(candidates, ", "))
	}
}

// Build compiles every <SourceDir>/*.cs into VL.AddOn.dll under WorkDir and
// returns the DLL path (WSL) and its hex sha256. The net48 csproj is rendered
// from the embedded template with Windows paths; reference DLLs resolve from the
// box's own NT8 install (Custom/Vendor from DocsDir, Core/Gui/Newtonsoft from
// the Exe's Program Files bin).
func Build(ctx context.Context, c Config) (dllPath string, sha256 string, err error) {
	if c.Run == nil {
		return "", "", ErrNoRun
	}
	if c.DocsDir == "" || !isDir(filepath.Join(c.DocsDir, "bin", "Custom")) {
		return "", "", fmt.Errorf("%w: %q", ErrNoDocsDir, c.DocsDir)
	}
	if err := os.MkdirAll(c.WorkDir, 0o755); err != nil {
		return "", "", fmt.Errorf("addonbuild: mkdir work dir: %w", err)
	}
	csFiles, _ := filepath.Glob(filepath.Join(c.SourceDir, "*.cs"))
	if len(csFiles) == 0 {
		return "", "", fmt.Errorf("%w: %q", ErrNoSourceFiles, c.SourceDir)
	}

	customDir := filepath.Join(c.DocsDir, "bin", "Custom")
	pfBin := filepath.Dir(c.Exe)
	if pfBin == "." || pfBin == "" {
		pfBin = "/mnt/c/Program Files/NinjaTrader 8/bin"
	}

	proj := renderTemplate(csprojTemplate, map[string]string{
		"{{ADDON_SRC}}":  wslToWindows(c.SourceDir),
		"{{NT8_CUSTOM}}": wslToWindows(customDir),
		"{{NT8_PF_BIN}}": wslToWindows(pfBin),
	})
	projPath := filepath.Join(c.WorkDir, "VL.AddOn.csproj")
	if err := os.WriteFile(projPath, []byte(proj), 0o644); err != nil {
		return "", "", fmt.Errorf("addonbuild: write csproj: %w", err)
	}

	outDir := filepath.Join(c.WorkDir, "out")
	args := []string{"build", wslToWindows(projPath), "-c", "Release", "-o", wslToWindows(outDir)}
	if _, err := c.Run(ctx, c.DotnetExe, args...); err != nil {
		return "", "", fmt.Errorf("addonbuild: dotnet build: %w", err)
	}

	dll := filepath.Join(outDir, "VL.AddOn.dll")
	if !isFile(dll) {
		return "", "", fmt.Errorf("%w: %s", ErrBuildNoDll, dll)
	}
	sum, err := sha256File(dll)
	if err != nil {
		return "", "", fmt.Errorf("addonbuild: sha256: %w", err)
	}
	return dll, sum, nil
}

// Place MOVES the current bin\Custom\VL.AddOn.dll (if any) to
// <DocsDir>\vl-addon-backups\<jobID>\ - outside bin\Custom, so NT8 never loads
// it - copies the newly built DLL in, and verifies its sha256 matches sha. It
// returns the backup directory it created (or "" when there was nothing to back
// up). On a sha mismatch the old DLL stays safe in the backup dir; the worker
// must not let NT8 start until it restores or replaces the dest DLL.
func Place(c Config, dll, sha, jobID string) (backupDir string, err error) {
	destDir := filepath.Join(c.DocsDir, "bin", "Custom")
	if !isDir(destDir) {
		return "", fmt.Errorf("%w: %q", ErrNoDocsDir, c.DocsDir)
	}
	destDll := filepath.Join(destDir, "VL.AddOn.dll")

	backupDir = ""
	if isFile(destDll) {
		backupDir = filepath.Join(c.DocsDir, "vl-addon-backups", jobID)
		if err := os.MkdirAll(backupDir, 0o755); err != nil {
			return "", fmt.Errorf("addonbuild: mkdir backup: %w", err)
		}
		if err := os.Rename(destDll, filepath.Join(backupDir, "VL.AddOn.dll")); err != nil {
			return "", fmt.Errorf("addonbuild: move old dll: %w", err)
		}
	}

	if err := copyFile(dll, destDll); err != nil {
		return "", fmt.Errorf("addonbuild: copy new dll: %w", err)
	}
	got, err := sha256File(destDll)
	if err != nil {
		return "", fmt.Errorf("addonbuild: sha256: %w", err)
	}
	if got != sha {
		return "", fmt.Errorf("addonbuild: sha256 mismatch: got %s want %s", got, sha)
	}
	return backupDir, nil
}
