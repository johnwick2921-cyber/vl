package addonbuild

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// The ps1's exit code decides: a script that printed both markers and then failed (the
// selftest's exit code is the script's) is not ok.
func TestCompilerProbeHonoursThePs1ExitCode(t *testing.T) {
	useTempWindowsTools(t)
	cfg := Config{
		DocsDir: mkNT8(t, t.TempDir(), "alice", true), PowerShellExe: "powershell.exe",
		Run: func(context.Context, string, ...string) ([]byte, error) {
			return []byte("VL_ADDON_PROBE=ok\nVL_SELFTEST_OK\n"), errors.New("exit status 1")
		},
	}
	if ok, detail := CompilerProbe(context.Background(), cfg); ok {
		t.Fatalf("CompilerProbe = ok (%q) on a ps1 that exited 1", detail)
	}
}

// Both callers hand the script the SAME two folders: without -CustomDir the probe has no
// csproj and no NinjaTrader.Custom.dll (the script guards that in normal mode only), and
// without -NT8Bin an NT8 outside C:\Program Files is never looked at.
func TestProbeAndBuildPassTheSameTwoFoldersToThePs1(t *testing.T) {
	useTempWindowsTools(t)
	src, work := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "x.cs"), []byte("// x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []string
	cfg := Config{
		DocsDir: mkNT8(t, t.TempDir(), "alice", true), Exe: "/mnt/d/Apps/NT8/bin/NinjaTrader.exe",
		PowerShellExe: "powershell.exe", SourceDir: src, WorkDir: work,
		Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			got = args
			if out := filepath.Join(work, "ps1-out"); isDir(out) {
				return nil, os.WriteFile(filepath.Join(out, "VL.AddOn.dll"), []byte("fake"), 0o644)
			}
			return []byte("VL_ADDON_PROBE=ok\nVL_SELFTEST_OK\n"), nil
		},
	}
	check := func(who string) {
		t.Helper()
		want := wslToWindows(filepath.Join(cfg.DocsDir, "bin", "Custom"))
		if d := argAfter(got, "-CustomDir"); d != want {
			t.Errorf("%s: -CustomDir %q, want %q (args %q)", who, d, want, got)
		}
		if b := argAfter(got, "-NT8Bin"); b != `D:\Apps\NT8\bin` {
			t.Errorf(`%s: -NT8Bin %q, want D:\Apps\NT8\bin (args %q)`, who, b, got)
		}
	}
	if ok, detail := CompilerProbe(context.Background(), cfg); !ok {
		t.Fatalf("CompilerProbe: %s", detail)
	}
	check("CompilerProbe")
	got = nil
	if res, err := Build(context.Background(), cfg); err != nil || res.BuiltBy != BuiltByRoslynPs1 {
		t.Fatalf("Build = %+v, %v", res, err)
	}
	check("Build")
}
