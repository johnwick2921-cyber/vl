package addonbuild

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkNT8(t *testing.T, root, user string, withCustom bool) string {
	t.Helper()
	docs := filepath.Join(root, user, "Documents", "NinjaTrader 8")
	if withCustom {
		if err := os.MkdirAll(filepath.Join(docs, "bin", "Custom"), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.MkdirAll(docs, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return docs
}

func TestDetect(t *testing.T) {
	orig := nt8DocsGlob
	t.Cleanup(func() { nt8DocsGlob = orig })

	t.Run("single", func(t *testing.T) {
		root := t.TempDir()
		alice := mkNT8(t, root, "alice", true)
		mkNT8(t, root, "bob", false)
		nt8DocsGlob = filepath.Join(root, "*", "Documents", "NinjaTrader 8")
		got, err := Detect()
		if err != nil {
			t.Fatalf("Detect = %v, want nil", err)
		}
		if got != alice {
			t.Fatalf("Detect = %q, want %q", got, alice)
		}
	})

	t.Run("multiple", func(t *testing.T) {
		root := t.TempDir()
		mkNT8(t, root, "alice", true)
		mkNT8(t, root, "bob", true)
		nt8DocsGlob = filepath.Join(root, "*", "Documents", "NinjaTrader 8")
		_, err := Detect()
		if err == nil || !strings.Contains(err.Error(), "multiple") {
			t.Fatalf("Detect err = %v, want multiple-candidates error", err)
		}
	})

	t.Run("none", func(t *testing.T) {
		root := t.TempDir()
		mkNT8(t, root, "alice", false)
		mkNT8(t, root, "bob", false)
		nt8DocsGlob = filepath.Join(root, "*", "Documents", "NinjaTrader 8")
		_, err := Detect()
		if err == nil || !strings.Contains(err.Error(), "no Documents/NinjaTrader 8") {
			t.Fatalf("Detect err = %v, want no-candidate error", err)
		}
	})
}

func TestBuild(t *testing.T) {
	root := t.TempDir()
	docsDir := mkNT8(t, root, "alice", true)
	srcDir := filepath.Join(root, "ninjascript")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "VLTraderTCPClient.cs"), []byte("// fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(root, "build")

	var gotArgs []string
	cfg := Config{
		DocsDir:       docsDir,
		Exe:           "/mnt/c/Program Files/NinjaTrader 8/bin/NinjaTrader.exe",
		DotnetExe:     "dotnet.exe",
		PowerShellExe: "powershell.exe",
		SourceDir:     srcDir,
		WorkDir:       workDir,
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			gotArgs = append([]string{name}, args...)
			// Simulate the no-SDK ps1 build succeeding: write the DLL to the
			// ps1 -Out dir (workDir/ps1-out).
			out := filepath.Join(workDir, "ps1-out")
			if err := os.MkdirAll(out, 0o755); err != nil {
				return nil, err
			}
			return []byte("VL_ADDON_DLL=x\nVL_ADDON_SHA256=y\n"), os.WriteFile(filepath.Join(out, "VL.AddOn.dll"), []byte("fake dll"), 0o644)
		},
	}

	res, err := Build(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Build err = %v", err)
	}
	wantDll := filepath.Join(workDir, "ps1-out", "VL.AddOn.dll")
	if res.DllPath != wantDll {
		t.Fatalf("dll = %q, want %q", res.DllPath, wantDll)
	}
	if res.BuiltBy != BuiltByRoslynPs1 {
		t.Fatalf("BuiltBy = %q, want %q", res.BuiltBy, BuiltByRoslynPs1)
	}
	wantSum, err := sha256File(wantDll)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sha256 != wantSum {
		t.Fatalf("sum = %q, want %q", res.Sha256, wantSum)
	}

	if len(gotArgs) == 0 || gotArgs[0] != "powershell.exe" {
		t.Fatalf("Run got %v, want powershell.exe -File ... build-addon.ps1", gotArgs)
	}
	// The ps1 must be written into the work dir and invoked with the right
	// reference dirs.
	ps1, err := os.ReadFile(filepath.Join(workDir, "build-addon.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if string(ps1) != BuildPs1() {
		t.Fatalf("written ps1 drifted from the embedded source")
	}
}

// TestBuildFallsBackToDotnet: when the ps1 path fails, Build falls back to the
// dotnet.exe net48 csproj and records BuiltBy=dotnet.
func TestBuildFallsBackToDotnet(t *testing.T) {
	root := t.TempDir()
	docsDir := mkNT8(t, root, "alice", true)
	srcDir := filepath.Join(root, "ninjascript")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "VLTraderTCPClient.cs"), []byte("// fake"), 0o644)
	workDir := filepath.Join(root, "build")

	var calls []string
	cfg := Config{
		DocsDir:       docsDir,
		Exe:           "/mnt/c/Program Files/NinjaTrader 8/bin/NinjaTrader.exe",
		DotnetExe:     "dotnet.exe",
		PowerShellExe: "powershell.exe",
		SourceDir:     srcDir,
		WorkDir:       workDir,
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name)
			if strings.Contains(name, "powershell") {
				return nil, errors.New("ps1 failed")
			}
			// Simulate a successful dotnet build writing the DLL.
			out := filepath.Join(workDir, "out")
			if err := os.MkdirAll(out, 0o755); err != nil {
				return nil, err
			}
			return nil, os.WriteFile(filepath.Join(out, "VL.AddOn.dll"), []byte("fake dll"), 0o644)
		},
	}

	res, err := Build(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Build err = %v", err)
	}
	wantDll := filepath.Join(workDir, "out", "VL.AddOn.dll")
	if res.DllPath != wantDll || res.BuiltBy != BuiltByDotnet {
		t.Fatalf("res = %+v, want dll %q built_by %q", res, wantDll, BuiltByDotnet)
	}

	// Both builders were attempted, ps1 first then dotnet.
	if len(calls) != 2 || !strings.Contains(calls[0], "powershell") || !strings.Contains(calls[1], "dotnet") {
		t.Fatalf("Run calls = %v, want powershell first then dotnet", calls)
	}

	proj, err := os.ReadFile(filepath.Join(workDir, "VL.AddOn.csproj"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`C:\Program Files\NinjaTrader 8\bin\NinjaTrader.Core.dll`, // Exe-derived, WSL→Windows
		`wsl.localhost`,          // SourceDir in the WSL fs → UNC
		`NinjaTrader.Custom.dll`, // from DocsDir/bin/Custom
		`NinjaTrader.Vendor.dll`,
		`Newtonsoft.Json.dll`,
	} {
		if !strings.Contains(string(proj), want) {
			t.Errorf("csproj missing %q:\n%s", want, proj)
		}
	}
}

// TestBuildBothFail: ps1 fails AND the dotnet fallback fails — the error must
// name both, never silently pass.
func TestBuildBothFail(t *testing.T) {
	root := t.TempDir()
	docsDir := mkNT8(t, root, "alice", true)
	srcDir := filepath.Join(root, "ninjascript")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "x.cs"), []byte("// x"), 0o644)
	workDir := filepath.Join(root, "build")

	cfg := Config{
		DocsDir:       docsDir,
		Exe:           "/mnt/c/Program Files/NinjaTrader 8/bin/NinjaTrader.exe",
		DotnetExe:     "dotnet.exe",
		PowerShellExe: "powershell.exe",
		SourceDir:     srcDir,
		WorkDir:       workDir,
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, errors.New("boom")
		},
	}

	_, err := Build(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "ps1 build failed") || !strings.Contains(err.Error(), "dotnet fallback failed") {
		t.Fatalf("err = %v, want an error naming both the ps1 and dotnet failures", err)
	}
}

func TestBuildErrors(t *testing.T) {
	root := t.TempDir()
	docsDir := mkNT8(t, root, "alice", true)
	srcDir := filepath.Join(root, "ninjascript")
	os.MkdirAll(srcDir, 0o755)
	workDir := filepath.Join(root, "build")

	base := Config{
		DocsDir:   docsDir,
		Exe:       "/mnt/c/Program Files/NinjaTrader 8/bin/NinjaTrader.exe",
		DotnetExe: "dotnet.exe",
		SourceDir: srcDir,
		WorkDir:   workDir,
		Run:       func(ctx context.Context, name string, args ...string) ([]byte, error) { return nil, nil },
	}

	t.Run("nil run", func(t *testing.T) {
		c := base
		c.Run = nil
		if _, err := Build(context.Background(), c); err != ErrNoRun {
			t.Fatalf("err = %v, want ErrNoRun", err)
		}
	})

	t.Run("no source files", func(t *testing.T) {
		if _, err := Build(context.Background(), base); err == nil || !strings.Contains(err.Error(), "no .cs files") {
			t.Fatalf("err = %v, want no-source-files", err)
		}
	})

	t.Run("missing docs dir", func(t *testing.T) {
		c := base
		c.DocsDir = filepath.Join(root, "nope")
		if _, err := Build(context.Background(), c); err == nil || !strings.Contains(err.Error(), "DocsDir") {
			t.Fatalf("err = %v, want DocsDir error", err)
		}
	})

	t.Run("build produces no dll", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(srcDir, "x.cs"), []byte("// x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Build(context.Background(), base); err == nil || !strings.Contains(err.Error(), "no VL.AddOn.dll") {
			t.Fatalf("err = %v, want no-dll error", err)
		}
	})
}

func TestPlace(t *testing.T) {
	root := t.TempDir()
	docsDir := mkNT8(t, root, "alice", true)
	destDll := filepath.Join(docsDir, "bin", "Custom", "VL.AddOn.dll")
	if err := os.WriteFile(destDll, []byte("old dll"), 0o644); err != nil {
		t.Fatal(err)
	}

	newDll := filepath.Join(root, "new", "VL.AddOn.dll")
	os.MkdirAll(filepath.Dir(newDll), 0o755)
	if err := os.WriteFile(newDll, []byte("new dll"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha, err := sha256File(newDll)
	if err != nil {
		t.Fatal(err)
	}

	backup, err := Place(Config{DocsDir: docsDir}, newDll, sha, "job-1")
	if err != nil {
		t.Fatalf("Place err = %v", err)
	}
	wantBackup := filepath.Join(docsDir, "vl-addon-backups", "job-1")
	if backup != wantBackup {
		t.Fatalf("backup = %q, want %q", backup, wantBackup)
	}
	// Old DLL moved to the backup, new DLL in place.
	old, err := os.ReadFile(filepath.Join(wantBackup, "VL.AddOn.dll"))
	if err != nil {
		t.Fatal(err)
	}
	if string(old) != "old dll" {
		t.Fatalf("backup content = %q, want old dll", old)
	}
	cur, err := os.ReadFile(destDll)
	if err != nil {
		t.Fatal(err)
	}
	if string(cur) != "new dll" {
		t.Fatalf("dest content = %q, want new dll", cur)
	}
}

func TestPlaceNoExistingDll(t *testing.T) {
	root := t.TempDir()
	docsDir := mkNT8(t, root, "alice", true)
	newDll := filepath.Join(root, "new", "VL.AddOn.dll")
	os.MkdirAll(filepath.Dir(newDll), 0o755)
	os.WriteFile(newDll, []byte("new dll"), 0o644)
	sha, _ := sha256File(newDll)

	backup, err := Place(Config{DocsDir: docsDir}, newDll, sha, "job-2")
	if err != nil {
		t.Fatalf("Place err = %v", err)
	}
	if backup != "" {
		t.Fatalf("backup = %q, want empty (nothing to back up)", backup)
	}
}

func TestPlaceShaMismatch(t *testing.T) {
	root := t.TempDir()
	docsDir := mkNT8(t, root, "alice", true)
	newDll := filepath.Join(root, "new", "VL.AddOn.dll")
	os.MkdirAll(filepath.Dir(newDll), 0o755)
	os.WriteFile(newDll, []byte("new dll"), 0o644)

	if _, err := Place(Config{DocsDir: docsDir}, newDll, "deadbeef", "job-3"); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("err = %v, want sha256 mismatch", err)
	}
}

func TestWslToWindows(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	cases := []struct{ in, want string }{
		{"/mnt/c/Users/hoang/Documents/NinjaTrader 8", `C:\Users\hoang\Documents\NinjaTrader 8`},
		{"/mnt/c/", `C:\`},
		{"/home/u/repo/ninjascript", `\\wsl.localhost\Ubuntu\home\u\repo\ninjascript`},
		{"", ""},
		// Already-Windows paths pass through unchanged (addonExe returns a
		// Windows path in production).
		{`C:\Program Files\NinjaTrader 8\bin`, `C:\Program Files\NinjaTrader 8\bin`},
		{`\\wsl.localhost\Ubuntu\tmp\x`, `\\wsl.localhost\Ubuntu\tmp\x`},
	}
	for _, c := range cases {
		if got := wslToWindows(c.in); got != c.want {
			t.Errorf("wslToWindows(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTemplateParityWithDeploy(t *testing.T) {
	deploy, err := os.ReadFile(filepath.Join("..", "..", "deploy", "addon", "VL.AddOn.csproj.tmpl"))
	if err != nil {
		t.Fatalf("read deploy template: %v", err)
	}
	if string(deploy) != csprojTemplate {
		t.Fatalf("deploy/addon/VL.AddOn.csproj.tmpl drifted from embedded csproj.tmpl (parity law)")
	}
}

func TestPs1ParityWithDeploy(t *testing.T) {
	deploy, err := os.ReadFile(filepath.Join("..", "..", "deploy", "addon", "build-addon.ps1"))
	if err != nil {
		t.Fatalf("read deploy ps1: %v", err)
	}
	if string(deploy) != buildPs1Script {
		t.Fatalf("deploy/addon/build-addon.ps1 drifted from embedded build-addon.ps1 (parity law)")
	}
}

func TestCompilerProbe(t *testing.T) {
	root := t.TempDir()
	docsDir := mkNT8(t, root, "alice", true)

	t.Run("probe ok", func(t *testing.T) {
		var gotArgs []string
		cfg := Config{
			DocsDir:       docsDir,
			PowerShellExe: "powershell.exe",
			Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				gotArgs = append([]string{name}, args...)
				return []byte("VL_ADDON_PROBE=ok\n"), nil
			},
		}
		ok, detail := CompilerProbe(context.Background(), cfg)
		if !ok {
			t.Fatalf("CompilerProbe = %v (%q), want ok", ok, detail)
		}
		if len(gotArgs) == 0 || gotArgs[0] != "powershell.exe" {
			t.Fatalf("Run got %v, want powershell.exe -Probe", gotArgs)
		}
		// The -Probe flag and the reference dirs must be passed.
		found := false
		for _, a := range gotArgs {
			if a == "-Probe" {
				found = true
			}
		}
		if !found {
			t.Fatalf("probe args missing -Probe: %v", gotArgs)
		}
	})

	t.Run("probe fails", func(t *testing.T) {
		cfg := Config{
			DocsDir:       docsDir,
			PowerShellExe: "powershell.exe",
			Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return []byte("ERROR failed to load Roslyn\n"), errors.New("exit 1")
			},
		}
		ok, detail := CompilerProbe(context.Background(), cfg)
		if ok {
			t.Fatalf("CompilerProbe = ok, want not-ok (probe failed)")
		}
		if !strings.Contains(detail, "failed to load Roslyn") {
			t.Fatalf("detail = %q, want the probe diagnostics", detail)
		}
	})

	t.Run("nil run", func(t *testing.T) {
		ok, _ := CompilerProbe(context.Background(), Config{DocsDir: docsDir})
		if ok {
			t.Fatalf("CompilerProbe with nil Run must be not-ok")
		}
	})
}
