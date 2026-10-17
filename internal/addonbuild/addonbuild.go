// Package addonbuild compiles the NinjaTrader 8 VL.AddOn.dll OUTSIDE NinjaTrader
// and stages it into the NT8 Custom folder, so a one-click update can ship the
// AddOn without a terminal, an agent, or an F5.
//
// W-ADDON-ONE-CLICK (owner order 2026-10-17): "update = one click, no terminal,
// no agent — and the NinjaTrader file too, for vl AND vltrader".
//
// THE ONE RULE: nothing in this package deletes. The old DLL and the old AddOn
// sources are MOVED to a backups directory; the build dir is a worker-made temp
// dir. Every test uses t.TempDir() fake dirs and a fake Run — no test ever
// touches the real NT8 folder or starts NT8.
package addonbuild

import "context"

// Config is everything Build/Place need that the caller supplies. Run executes a
// subprocess (dotnet.exe build, powershell.exe, …) so tests can substitute a
// fake; the real worker wires it to exec.CommandContext. DocsDir is the
// Documents\NinjaTrader 8 directory; Exe is the NinjaTrader.exe path (unused by
// Build/Place, kept for the worker's restart step); DotnetExe is dotnet.exe;
// SourceDir holds the release's ninjascript/*.cs; WorkDir is the worker-made
// build dir.
type Config struct {
	DocsDir   string
	Exe       string
	DotnetExe string
	SourceDir string
	WorkDir   string
	Run       func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Build compiles every <SourceDir>/*.cs into VL.AddOn.dll under WorkDir and
// returns the DLL path and its hex sha256. The net48 csproj is rendered from the
// template and its reference DLLs are resolved from the box's own NT8 install.
func Build(ctx context.Context, c Config) (dllPath string, sha256 string, err error) {
	return "", "", errNotImplemented
}

// Place MOVES the current bin\Custom\VL.AddOn.dll (if any) to
// <DocsDir>\vl-addon-backups\<jobID>\ — outside bin\Custom, so NT8 never loads
// it — copies the newly built DLL in, and verifies its sha256 matches sha.
// It returns the backup directory it created (or "" when there was nothing to
// back up).
func Place(c Config, dll, sha, jobID string) (backupDir string, err error) {
	return "", errNotImplemented
}

// Detect returns the single /mnt/c/Users/*/Documents/NinjaTrader 8 directory
// that has a bin/Custom folder. More than one candidate is an error naming them;
// zero is an error.
func Detect() (docsDir string, err error) {
	return "", errNotImplemented
}
