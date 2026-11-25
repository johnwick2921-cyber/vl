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

// The probe is ok only with exit 0 AND both markers: a script that printed both and then
// failed (the selftest's exit code is the script's) is not ok, and neither is an exit 0
// that printed only one of them.
func TestCompilerProbeHonoursThePs1ExitCode(t *testing.T) {
	useTempWindowsTools(t)
	const compiled, selftest = "VL_ADDON_PROBE=ok\n", "VL_SELFTEST_OK\n"
	for name, c := range map[string]struct {
		out  string
		err  error
		want bool
	}{
		"both markers, exit 0":           {compiled + selftest, nil, true},
		"both markers, exit 1":           {compiled + selftest, errors.New("exit status 1"), false},
		"only VL_ADDON_PROBE=ok, exit 0": {compiled, nil, false},
		"only VL_SELFTEST_OK, exit 0":    {selftest, nil, false},
		"no marker, exit 0":              {"", nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := Config{
				DocsDir: mkNT8(t, t.TempDir(), "alice", true), PowerShellExe: "powershell.exe",
				Run: func(context.Context, string, ...string) ([]byte, error) { return []byte(c.out), c.err },
			}
			if ok, detail := CompilerProbe(context.Background(), cfg); ok != c.want {
				t.Fatalf("CompilerProbe = %v (%q), want %v", ok, detail, c.want)
			}
		})
	}
}

// For the SAME Config, CompilerProbe and Build hand the script the same two folders:
// without -CustomDir the probe has no csproj and no NinjaTrader.Custom.dll (the script
// guards that in normal mode only), and without -NT8Bin an NT8 outside C:\Program Files
// is never looked at. It does NOT say that production gives them the same Config: the
// readiness item sets no Exe, so it looks in C:\Program Files only, and has its own
// docs-folder detection (updatereadiness, TestReadinessProbeLooksOnlyInTheDefaultFolders);
// the updater honours VL_NT8_EXE and VL_NT8_DOCS_DIR.
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
