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
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
)

// Config is everything Build/Place need that the caller supplies. DocsDir,
// Exe, SourceDir and WorkDir are WSL paths (Exe may also be a Windows path,
// as the worker's addonExe returns). DotnetExe and PowerShellExe are passed to
// Run verbatim.
//
//	DocsDir       - Documents\NinjaTrader 8 directory (bin/Custom must exist)
//	Exe           - NinjaTrader.exe path (Core/Gui refs derive from its dir)
//	DotnetExe     - dotnet.exe fallback builder (passed to Run verbatim)
//	PowerShellExe - powershell.exe running build-addon.ps1 (the PRIMARY builder)
//	SourceDir     - release's ninjascript/*.cs directory
//	WorkDir       - worker-made build dir (NEVER the NT8 folder)
//	Run           - execs a subprocess so tests can substitute a fake
type Config struct {
	DocsDir       string
	Exe           string
	DotnetExe     string
	PowerShellExe string
	SourceDir     string
	WorkDir       string
	Run           func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// BuiltBy values: which builder produced VL.AddOn.dll.
const (
	BuiltByRoslynPs1 = "roslyn-ps1" // build-addon.ps1 via NT8's built-in Roslyn (the no-SDK PRIMARY path)
	BuiltByDotnet    = "dotnet"     // the dotnet.exe net48 csproj fallback
)

// Result is what Build returns: the built DLL (WSL path), its hex sha256, and
// which builder produced it.
type Result struct {
	DllPath string
	Sha256  string
	BuiltBy string
}

// Sentinel errors returned by this package.
var (
	ErrNoNT8Dirs       = errors.New("addonbuild: no Documents/NinjaTrader 8 with bin/Custom found")
	ErrMultipleNT8Dirs = errors.New("addonbuild: multiple Documents/NinjaTrader 8 candidates")
	ErrNoSourceFiles   = errors.New("addonbuild: no .cs files in source dir")
	ErrNoDocsDir       = errors.New("addonbuild: DocsDir empty or missing bin/Custom")
	ErrNoRun           = errors.New("addonbuild: Run is nil")
	ErrBuildNoDll      = errors.New("addonbuild: build produced no VL.AddOn.dll")
	ErrNoCustomDll     = errors.New("addonbuild: no NinjaTrader.Custom.dll to rebuild")
)

// nt8DocsGlob is the WSL glob Detect scans. A package var so tests can point it
// at a temp dir.
var nt8DocsGlob = "/mnt/c/Users/*/Documents/NinjaTrader 8"

// Windows tool locations (W-UPDATER-WINPATH). The vl-updater user unit's PATH
// has no /mnt/c entries (systemd does not inherit WSL's appendWindowsPath), so
// a bare powershell.exe / dotnet.exe name can miss even though the tool exists
// at its fixed Windows location. Vars so tests can point them at temp files.
var (
	windowsPowerShellWSL = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
	windowsDotnetWSL     = "/mnt/c/Program Files/dotnet/dotnet.exe"
)

// resolveWindowsExe resolves a Windows tool name to a runnable path. A path
// (contains a separator, or is absolute) is passed through untouched. A bare
// name is looked up on PATH first (an interactive WSL shell has the Windows
// dirs); when LookPath misses, the fixed WSL mount of the tool's Windows
// location is tried. The error names every path tried.
func resolveWindowsExe(name, fixedWSL string) (string, error) {
	if name == "" {
		return "", errors.New("empty tool name")
	}
	if strings.ContainsAny(name, `/\`) || filepath.IsAbs(name) {
		return name, nil // already a path — passed verbatim
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	if isFile(fixedWSL) {
		return fixedWSL, nil
	}
	return "", fmt.Errorf("%q: not found on PATH; %s: no such file", name, fixedWSL)
}

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
// returns the DLL path (WSL), its hex sha256, and which builder produced it.
//
// PRIMARY path (the no-SDK partner-proof method): the embedded build-addon.ps1
// is written into WorkDir and run through PowerShell, which loads
// Microsoft.CodeAnalysis.CSharp.dll (Roslyn) from NT8's own bin and compiles
// against the framework reference assemblies + the NinjaTrader.Custom.csproj
// reference mirror. The dotnet.exe net48 csproj is only a FALLBACK, used when
// the ps1 path fails, and the result records which path actually built it.
func Build(ctx context.Context, c Config) (Result, error) {
	if c.Run == nil {
		return Result{}, ErrNoRun
	}
	docsDir, err := filepath.Abs(c.DocsDir)
	if err != nil {
		return Result{}, fmt.Errorf("addonbuild: abs DocsDir: %w", err)
	}
	srcDir, err := filepath.Abs(c.SourceDir)
	if err != nil {
		return Result{}, fmt.Errorf("addonbuild: abs SourceDir: %w", err)
	}
	workDir, err := filepath.Abs(c.WorkDir)
	if err != nil {
		return Result{}, fmt.Errorf("addonbuild: abs WorkDir: %w", err)
	}
	if !isDir(filepath.Join(docsDir, "bin", "Custom")) {
		return Result{}, fmt.Errorf("%w: %q", ErrNoDocsDir, c.DocsDir)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("addonbuild: mkdir work dir: %w", err)
	}
	csFiles, _ := filepath.Glob(filepath.Join(srcDir, "*.cs"))
	if len(csFiles) == 0 {
		return Result{}, fmt.Errorf("%w: %q", ErrNoSourceFiles, c.SourceDir)
	}

	powerShell := c.PowerShellExe
	if powerShell == "" {
		powerShell = "powershell.exe"
	}
	dotnetExe := c.DotnetExe
	if dotnetExe == "" {
		dotnetExe = "dotnet.exe"
	}

	// PRIMARY: the embedded build-addon.ps1 via NT8's built-in Roslyn.
	ps1Dll, ps1Err := buildViaPs1(ctx, c, workDir, srcDir, docsDir, powerShell)
	if ps1Err == nil {
		return ps1Dll, nil
	}

	// FALLBACK: the dotnet.exe net48 csproj (needs the SDK).
	dotnetDll, dotnetErr := buildViaDotnet(ctx, c, workDir, srcDir, docsDir, dotnetExe)
	if dotnetErr == nil {
		return dotnetDll, nil
	}

	return Result{}, fmt.Errorf("addonbuild: Roslyn ps1 build failed: %v; dotnet fallback failed: %v", ps1Err, dotnetErr)
}

// buildViaPs1 writes the embedded build-addon.ps1 into WorkDir and runs it
// through PowerShell with -Probe off, then verifies the DLL it emitted. On
// success it returns Result{BuiltBy: BuiltByRoslynPs1}.
func buildViaPs1(ctx context.Context, c Config, workDir, srcDir, docsDir, powerShell string) (Result, error) {
	powerShell, err := resolveWindowsExe(powerShell, windowsPowerShellWSL)
	if err != nil {
		return Result{}, fmt.Errorf("addonbuild: powershell: %w", err)
	}
	ps1Path := filepath.Join(workDir, "build-addon.ps1")
	if err := os.WriteFile(ps1Path, []byte(BuildPs1()), 0o644); err != nil {
		return Result{}, fmt.Errorf("addonbuild: write ps1: %w", err)
	}

	outDir := filepath.Join(workDir, "ps1-out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("addonbuild: mkdir ps1 out: %w", err)
	}

	customWin := wslToWindows(filepath.Join(docsDir, "bin", "Custom"))
	args := []string{
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", wslToWindows(ps1Path),
		"-Src", wslToWindows(srcDir),
		"-Out", wslToWindows(outDir),
		"-CustomDir", customWin,
		"-NT8Bin", nt8BinWindows(c.Exe),
	}
	out, runErr := c.Run(ctx, powerShell, args...)
	if runErr != nil {
		return Result{}, fmt.Errorf("addonbuild: powershell ps1 build: %w: %s", runErr, clipBuildOut(out))
	}

	dll := filepath.Join(outDir, "VL.AddOn.dll")
	if !isFile(dll) {
		return Result{}, fmt.Errorf("addonbuild: the Roslyn ps1 build produced no VL.AddOn.dll: %s", clipBuildOut(out))
	}
	sum, err := sha256File(dll)
	if err != nil {
		return Result{}, fmt.Errorf("addonbuild: sha256: %w", err)
	}
	return Result{DllPath: dll, Sha256: sum, BuiltBy: BuiltByRoslynPs1}, nil
}

// buildViaDotnet renders the net48 csproj and builds it with dotnet.exe — the
// fallback that needs an SDK (absent on the partner box).
func buildViaDotnet(ctx context.Context, c Config, workDir, srcDir, docsDir, dotnetExe string) (Result, error) {
	dotnetExe, err := resolveWindowsExe(dotnetExe, windowsDotnetWSL)
	if err != nil {
		return Result{}, fmt.Errorf("addonbuild: dotnet: %w", err)
	}
	customDir := filepath.Join(docsDir, "bin", "Custom")

	proj := renderTemplate(csprojTemplate, map[string]string{
		"{{ADDON_SRC}}":  wslToWindows(srcDir),
		"{{NT8_CUSTOM}}": wslToWindows(customDir),
		"{{NT8_PF_BIN}}": nt8BinWindows(c.Exe),
	})
	projPath := filepath.Join(workDir, "VL.AddOn.csproj")
	if err := os.WriteFile(projPath, []byte(proj), 0o644); err != nil {
		return Result{}, fmt.Errorf("addonbuild: write csproj: %w", err)
	}

	outDir := filepath.Join(workDir, "out")
	args := []string{"build", wslToWindows(projPath), "-c", "Release", "-o", wslToWindows(outDir)}
	out, err := c.Run(ctx, dotnetExe, args...)
	if err != nil {
		return Result{}, fmt.Errorf("addonbuild: dotnet build: %w: %s", err, clipBuildOut(out))
	}

	dll := filepath.Join(outDir, "VL.AddOn.dll")
	if !isFile(dll) {
		return Result{}, fmt.Errorf("%w: %s", ErrBuildNoDll, dll)
	}
	sum, err := sha256File(dll)
	if err != nil {
		return Result{}, fmt.Errorf("addonbuild: sha256: %w", err)
	}
	return Result{DllPath: dll, Sha256: sum, BuiltBy: BuiltByDotnet}, nil
}

// nt8BinWindows returns the NinjaTrader bin directory as a Windows path,
// derived from Exe (which may itself be a Windows or WSL path) with the
// standard install location as the default.
func nt8BinWindows(exe string) string {
	if exe != "" {
		dir := filepath.Dir(exe)
		if dir != "." && dir != "" {
			return wslToWindows(dir)
		}
	}
	return `C:\Program Files\NinjaTrader 8\bin`
}

// RebuildCustom (release #35, the no-F5 rebuild) compiles NinjaTrader.Custom.dll
// from every <DocsDir>/bin/Custom/**/*.cs EXCEPT VL*.cs, through build-addon.ps1
// -Custom, which ALSO verifies the new DLL's public type list equals the old
// Custom.dll's minus the VL types. It returns the built DLL (WSL path under
// WorkDir/custom-out) and its sha on success; any compile error or type-list
// diff is an error (the caller then asks for F5 as the last resort). The old
// Custom.dll is only READ here — the caller MOVEs it to a backup via PlaceCustom.
func RebuildCustom(ctx context.Context, c Config) (Result, error) {
	if c.Run == nil {
		return Result{}, ErrNoRun
	}
	docsDir, err := filepath.Abs(c.DocsDir)
	if err != nil {
		return Result{}, fmt.Errorf("addonbuild: abs DocsDir: %w", err)
	}
	workDir, err := filepath.Abs(c.WorkDir)
	if err != nil {
		return Result{}, fmt.Errorf("addonbuild: abs WorkDir: %w", err)
	}
	customDir := filepath.Join(docsDir, "bin", "Custom")
	if !isDir(customDir) {
		return Result{}, fmt.Errorf("%w: %q", ErrNoDocsDir, c.DocsDir)
	}
	if !isFile(filepath.Join(customDir, "NinjaTrader.Custom.dll")) {
		return Result{}, fmt.Errorf("%w: %s", ErrNoCustomDll, filepath.Join(customDir, "NinjaTrader.Custom.dll"))
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("addonbuild: mkdir work dir: %w", err)
	}
	ps1Path := filepath.Join(workDir, "build-addon.ps1")
	if err := os.WriteFile(ps1Path, []byte(BuildPs1()), 0o644); err != nil {
		return Result{}, fmt.Errorf("addonbuild: write ps1: %w", err)
	}
	// The -Custom mode compiles VLCustomBuilder.cs (from $PSScriptRoot) into a
	// temp exe; it must sit next to the ps1.
	if err := os.WriteFile(filepath.Join(workDir, "VLCustomBuilder.cs"), []byte(BuilderCs()), 0o644); err != nil {
		return Result{}, fmt.Errorf("addonbuild: write VLCustomBuilder.cs: %w", err)
	}
	outDir := filepath.Join(workDir, "custom-out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("addonbuild: mkdir custom-out: %w", err)
	}

	powerShell := c.PowerShellExe
	if powerShell == "" {
		powerShell = "powershell.exe"
	}
	powerShell, err = resolveWindowsExe(powerShell, windowsPowerShellWSL)
	if err != nil {
		return Result{}, fmt.Errorf("addonbuild: powershell: %w", err)
	}
	args := []string{
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", wslToWindows(ps1Path),
		"-Custom",
		"-CustomDir", wslToWindows(customDir),
		"-Out", wslToWindows(outDir),
		"-NT8Bin", nt8BinWindows(c.Exe),
	}
	out, runErr := c.Run(ctx, powerShell, args...)
	outStr := string(out)
	switch {
	case strings.Contains(outStr, "VL_CUSTOM_TYPES_DIFF"):
		// Compile succeeded but the public type list is wrong: indicators,
		// strategies or Apex would be disturbed. Never accept it.
		return Result{}, fmt.Errorf("addonbuild: the rebuilt Custom.dll public type list differs: %s", clipBuildOut(out))
	case runErr != nil:
		return Result{}, fmt.Errorf("addonbuild: Custom.dll rebuild: %w: %s", runErr, clipBuildOut(out))
	case !strings.Contains(outStr, "VL_CUSTOM_TYPES_OK"):
		return Result{}, fmt.Errorf("addonbuild: the rebuilt Custom.dll was not accepted: %s", clipBuildOut(out))
	}

	dll := filepath.Join(outDir, "NinjaTrader.Custom.dll")
	if !isFile(dll) {
		return Result{}, fmt.Errorf("addonbuild: the Custom rebuild produced no NinjaTrader.Custom.dll: %s", clipBuildOut(out))
	}
	sum, err := sha256File(dll)
	if err != nil {
		return Result{}, fmt.Errorf("addonbuild: sha256: %w", err)
	}
	return Result{DllPath: dll, Sha256: sum, BuiltBy: BuiltByRoslynPs1}, nil
}

// PlaceCustom MOVEs the current bin/Custom/NinjaTrader.Custom.dll to
// <DocsDir>/vl-addon-backups/<jobID>/custom-rebuild/ and copies the rebuilt DLL
// in, verifying its sha256. On any failure after the backup move it MOVEs the
// backup back (the old Custom.dll is never left absent). It returns the backup
// directory.
//
// Acceptance (f) (release #35 FOLD 4): the 16 culture satellites
// (bin/Custom/<culture>/NinjaTrader.{Custom,Vendor}.resources.dll) and
// bin/Custom/NinjaTrader.Vendor.dll are OUT OF SCOPE - the place must leave
// them byte-identical. Their sha256 is snapshotted before the move and verified
// after; a difference refuses the place and restores the old Custom.dll.
func PlaceCustom(c Config, dll, sha, jobID string) (backupDir string, err error) {
	customDir := filepath.Join(c.DocsDir, "bin", "Custom")
	if !isDir(customDir) {
		return "", fmt.Errorf("%w: %q", ErrNoDocsDir, c.DocsDir)
	}
	destDll := filepath.Join(customDir, "NinjaTrader.Custom.dll")
	if !isFile(destDll) {
		return "", fmt.Errorf("%w: %s", ErrNoCustomDll, destDll)
	}

	before, err := satelliteFingerprint(customDir)
	if err != nil {
		return "", err
	}

	backupDir = filepath.Join(c.DocsDir, "vl-addon-backups", jobID, "custom-rebuild")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", fmt.Errorf("addonbuild: mkdir custom-rebuild backup: %w", err)
	}
	backup := filepath.Join(backupDir, "NinjaTrader.Custom.dll")
	if err := os.Rename(destDll, backup); err != nil {
		return "", fmt.Errorf("addonbuild: move old Custom.dll: %w", err)
	}
	restore := func() { _ = os.Rename(backup, destDll) }

	if err := copyFile(dll, destDll); err != nil {
		restore()
		return "", fmt.Errorf("addonbuild: copy rebuilt Custom.dll: %w (old restored)", err)
	}
	got, err := sha256File(destDll)
	if err != nil {
		restore()
		return "", fmt.Errorf("addonbuild: Custom.dll sha256: %w (old restored)", err)
	}
	if got != sha {
		restore()
		return "", fmt.Errorf("addonbuild: Custom.dll sha256 mismatch: got %s want %s (old restored)", got, sha)
	}

	// Test seam: simulate a place that disturbs a satellite or the Vendor dll.
	if placeHook != nil {
		placeHook(customDir)
	}

	after, err := satelliteFingerprint(customDir)
	if err != nil {
		restore()
		return "", fmt.Errorf("addonbuild: satellite re-hash: %w (old restored)", err)
	}
	if !reflect.DeepEqual(before, after) {
		restore()
		return "", fmt.Errorf("addonbuild: a satellite/Vendor file changed during the place (acceptance f): %s", satelliteDiff(before, after))
	}
	return backupDir, nil
}

// placeHook, non-nil only in tests, runs between the Custom.dll copy and the
// after-place satellite verification so a test can simulate a place that
// disturbs a satellite or the Vendor dll. Production code never sets it.
var placeHook func(customDir string)

// satelliteFingerprint returns the hex sha256 of every OUT-OF-SCOPE file the
// place must leave untouched (acceptance f): bin/Custom/<culture>/
// NinjaTrader.{Custom,Vendor}.resources.dll plus bin/Custom/NinjaTrader.Vendor.dll,
// keyed by path relative to customDir. Missing files are simply absent.
func satelliteFingerprint(customDir string) (map[string]string, error) {
	patterns := []string{
		filepath.Join(customDir, "*", "NinjaTrader.Custom.resources.dll"),
		filepath.Join(customDir, "*", "NinjaTrader.Vendor.resources.dll"),
		filepath.Join(customDir, "NinjaTrader.Vendor.dll"),
	}
	out := make(map[string]string)
	for _, pat := range patterns {
		matches, _ := filepath.Glob(pat)
		for _, m := range matches {
			if !isFile(m) {
				continue
			}
			sum, err := sha256File(m)
			if err != nil {
				return nil, fmt.Errorf("addonbuild: satellite sha256 %s: %w", m, err)
			}
			rel, _ := filepath.Rel(customDir, m)
			out[rel] = sum
		}
	}
	return out, nil
}

// satelliteDiff renders a compact before/after difference for the error path.
func satelliteDiff(before, after map[string]string) string {
	var diffs []string
	for k, v := range before {
		if after[k] != v {
			diffs = append(diffs, fmt.Sprintf("%s: %s -> %s", k, shortSha(v), shortSha(after[k])))
		}
	}
	for k, v := range after {
		if _, ok := before[k]; !ok {
			diffs = append(diffs, fmt.Sprintf("%s: absent -> %s", k, shortSha(v)))
		}
	}
	return strings.Join(diffs, "; ")
}

func shortSha(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// BuildPs1 returns the embedded build-addon.ps1 source — the single source of
// truth for the no-SDK build (deploy/addon/build-addon.ps1 is the byte-identical
// deploy copy; TestPs1ParityWithDeploy keeps them from drifting).
func BuildPs1() string { return buildPs1Script }

// BuilderCs returns the embedded VLCustomBuilder.cs source — the C# program
// build-addon.ps1 compiles and runs in -Custom mode to rebuild Custom.dll WITH
// its embedded resources (deploy/addon/VLCustomBuilder.cs is the byte-identical
// deploy copy; TestBuilderCsParityWithDeploy keeps them from drifting).
func BuilderCs() string { return builderCs }

// CompilerProbe proves the no-SDK ps1 path works on this box: it writes the
// embedded build-addon.ps1 (and its VLCustomBuilder.cs sidekick) to a
// worker-made temp dir and runs its -Probe mode, which loads Roslyn from NT8's
// bin, resolves its dependencies, compiles a trivial source against the full
// reference set, AND runs the builder's --selftest (FOLD 5 F1). ok is true only
// when the probe prints both VL_ADDON_PROBE=ok and VL_SELFTEST_OK. Nothing
// touches the real NT8 folder or starts NT8.
func CompilerProbe(ctx context.Context, c Config) (ok bool, detail string) {
	if c.Run == nil {
		return false, "no command runner"
	}
	powerShell := c.PowerShellExe
	if powerShell == "" {
		powerShell = "powershell.exe"
	}
	powerShell, err := resolveWindowsExe(powerShell, windowsPowerShellWSL)
	if err != nil {
		return false, err.Error()
	}
	dir, err := os.MkdirTemp("", "vl-addon-probe-")
	if err != nil {
		return false, err.Error()
	}
	ps1Path := filepath.Join(dir, "build-addon.ps1")
	if err := os.WriteFile(ps1Path, []byte(BuildPs1()), 0o644); err != nil {
		return false, err.Error()
	}
	if err := os.WriteFile(filepath.Join(dir, "VLCustomBuilder.cs"), []byte(BuilderCs()), 0o644); err != nil {
		return false, err.Error()
	}

	args := []string{
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", wslToWindows(ps1Path),
		"-Probe",
		"-NT8Bin", nt8BinWindows(c.Exe),
		"-CustomDir", wslToWindows(filepath.Join(c.DocsDir, "bin", "Custom")),
	}
	out, runErr := c.Run(ctx, powerShell, args...)
	tail := clipBuildOut(out)
	if runErr != nil {
		return false, "probe failed: " + runErr.Error() + "; " + tail
	}
	s := string(out)
	switch {
	case !strings.Contains(s, "VL_ADDON_PROBE=ok"):
		return false, "probe did not report ok; " + tail
	case !strings.Contains(s, "VL_SELFTEST_OK"):
		return false, "probe selftest did not report ok; " + tail
	}
	return true, strings.TrimSpace(tail)
}

// clipBuildOut trims subprocess output to a bounded tail for error messages.
func clipBuildOut(out []byte) string {
	s := strings.TrimSpace(string(out))
	const max = 2000
	if len(s) > max {
		s = "…" + s[len(s)-max:]
	}
	return s
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
