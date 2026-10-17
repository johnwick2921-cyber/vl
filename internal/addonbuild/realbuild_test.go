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

// TestRealBuildProof is an opt-in integration proof that the net48 recipe
// actually compiles the release's ninjascript/*.cs with the box's dotnet.exe.
// Set ADDONBUILD_REAL_PROOF=1 to run. It writes only to a worker-made dir under
// %LOCALAPPDATA%\Temp and never touches the real NT8 folder.
func TestRealBuildProof(t *testing.T) {
	if os.Getenv("ADDONBUILD_REAL_PROOF") != "1" {
		t.Skip("set ADDONBUILD_REAL_PROOF=1 to run the real dotnet.exe build proof")
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
	dll, sha, err := Build(ctx, cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Logf("BUILT %s sha256=%s", dll, sha)
}
