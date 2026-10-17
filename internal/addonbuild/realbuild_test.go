package addonbuild

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRealBuildProof is an opt-in integration proof that the PRIMARY no-SDK
// path (build-addon.ps1 via NT8's built-in Roslyn) really compiles the
// release's ninjascript/*.cs into VL.AddOn.dll on this box, falling back to
// dotnet.exe only if the ps1 fails. Set ADDONBUILD_REAL_PROOF=1 to run. It
// writes only to a worker-made dir under %LOCALAPPDATA%\Temp and never touches
// the real NT8 folder.
func TestRealBuildProof(t *testing.T) {
	if os.Getenv("ADDONBUILD_REAL_PROOF") != "1" {
		t.Skip("set ADDONBUILD_REAL_PROOF=1 to run the real no-SDK ps1 build proof")
	}
	docsDir, err := Detect()
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	// docsDir = /mnt/c/Users/<u>/Documents/NinjaTrader 8
	rel := strings.TrimPrefix(docsDir, "/mnt/c/Users/")
	winUser := strings.SplitN(rel, "/", 2)[0]

	srcDir := filepath.Join("..", "..", "ninjascript")
	workDir := filepath.Join("/mnt/c/Users", winUser, "AppData", "Local", "Temp", "vl-addon-build", "proof")

	cfg := Config{
		DocsDir:   docsDir,
		Exe:       "/mnt/c/Program Files/NinjaTrader 8/bin/NinjaTrader.exe",
		DotnetExe: "dotnet.exe",
		SourceDir: srcDir,
		WorkDir:   workDir,
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			out, err := cmd.CombinedOutput()
			t.Logf("$ %s %s\n%s", name, strings.Join(args, " "), out)
			return out, err
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	res, err := Build(ctx, cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Logf("BUILT %s sha256=%s built_by=%s", res.DllPath, res.Sha256, res.BuiltBy)
}
