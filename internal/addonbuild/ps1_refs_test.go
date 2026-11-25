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

// NT8 8.1.9.0 (auto-update 2026-11-15) ships a NinjaTrader.Custom.csproj whose Core and
// Gui references are <ProjectReference> items with no HintPath. build-addon.ps1 mirrored
// HintPaths only: in hand runs on 8.1.9.0 the UNCHANGED #71 source fails with 388 errors
// while the readiness probe (an empty class) says ok. No AddOn build is recorded on this
// box after 2026-11-12 00:30 CT, so no update has met it.

func ps1InOrder(t *testing.T, parts ...string) {
	t.Helper()
	ps1, at := BuildPs1(), 0
	for _, p := range parts {
		i := strings.Index(ps1[at:], p)
		if i < 0 {
			t.Fatalf("build-addon.ps1: %q missing or out of order (want %q)", p, parts)
		}
		at += i + len(p)
	}
}

func TestPs1TakesCoreAndGuiFromTheInstalledNT8(t *testing.T) {
	// after the csproj mirror (Add-Ref is first-wins: a csproj that names them still wins),
	// before anything is compiled, and a missing one is a plain stop
	ps1InOrder(t, "Add-Ref $m.Groups[1].Value",
		"foreach ($n in @('NinjaTrader.Core.dll','NinjaTrader.Gui.dll')) {",
		"Add-Ref (Join-Path $NT8Bin $n)",
		"if (-not $refPaths.ContainsKey($n.ToLowerInvariant())) {",
		"Fail (",
		"# -- Compile ---")
}

// The probe must fail when the real build would: it compiles a type of NinjaTrader.Core.
func TestPs1ProbeCompilesAgainstNinjaTraderCore(t *testing.T) {
	ps1InOrder(t, "if ($Probe) {\n    # A type", "NinjaTrader.NinjaScript.AddOnBase", "NinjaTrader.Cbi.Account", "$asmName = '__VlAddonProbe'")
	if strings.Contains(BuildPs1(), "public static class __VlAddonProbe { }") {
		t.Error("the probe compiles an empty class: it says ok when every AddOn build fails")
	}
}

// Opt-in (ADDONBUILD_REAL_PROOF=1): the real ps1 against a csproj in the 8.1.9.0 shape, in
// a throwaway Windows folder. Reads the NT8 folders, writes nothing there.
func TestRealPs1ProjectReferenceOnlyCsproj(t *testing.T) {
	if os.Getenv("ADDONBUILD_REAL_PROOF") != "1" {
		t.Skip("set ADDONBUILD_REAL_PROOF=1 to run the real ps1 against a ProjectReference-only csproj")
	}
	docs, err := Detect()
	if err != nil {
		t.Fatal(err)
	}
	user := strings.SplitN(strings.TrimPrefix(docs, "/mnt/c/Users/"), "/", 2)[0]
	work, err := os.MkdirTemp(filepath.Join("/mnt/c/Users", user, "AppData", "Local", "Temp"), "vl-ps1refs-")
	if err != nil {
		t.Fatal(err)
	}
	const pf = "/mnt/c/Program Files/NinjaTrader 8/bin"
	cp := func(from, to string) {
		t.Helper()
		b, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(to, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	custom, src := filepath.Join(work, "custom"), filepath.Join(work, "src")
	for _, n := range []string{"NinjaTrader.Custom.dll", "NinjaTrader.Vendor.dll"} {
		cp(filepath.Join(docs, "bin", "Custom", n), filepath.Join(custom, n))
	}
	if err := os.WriteFile(filepath.Join(custom, "NinjaTrader.Custom.csproj"), []byte(
		`<Project><ItemGroup><ProjectReference Include="..\NinjaTrader.Core\NinjaTrader.Core.csproj" /><ProjectReference Include="..\NinjaTrader.Gui\NinjaTrader.Gui.csproj" /></ItemGroup></Project>`), 0o644); err != nil {
		t.Fatal(err)
	}
	cs, _ := filepath.Glob(filepath.Join("..", "..", "ninjascript", "*.cs"))
	for _, f := range cs {
		cp(f, filepath.Join(src, filepath.Base(f)))
	}
	ps1 := filepath.Join(work, "build-addon.ps1")
	if err := os.WriteFile(ps1, []byte(BuildPs1()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "VLCustomBuilder.cs"), []byte(BuilderCs()), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		out, err := exec.CommandContext(ctx, windowsPowerShellWSL, append([]string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
			"-File", wslToWindows(ps1), "-CustomDir", wslToWindows(custom)}, args...)...).CombinedOutput()
		t.Logf("ps1 %v err=%v\n%s", args, err, clipBuildOut(out))
		return string(out), err
	}
	out, err := run("-Src", wslToWindows(src), "-Out", wslToWindows(filepath.Join(work, "out")))
	if err != nil || !strings.Contains(out, "VL_ADDON_SHA256=") {
		t.Errorf("the real source does not build on a ProjectReference-only csproj: %v", err)
	}
	out, err = run("-Probe")
	if err != nil || !strings.Contains(out, "VL_ADDON_PROBE=ok") || !strings.Contains(out, "VL_SELFTEST_OK") {
		t.Errorf("the probe is red on a box that can build: %v", err)
	}
	// an NT8 bin folder that has the compiler but no Core: both modes stop and name it
	bare := filepath.Join(work, "bare")
	dlls, _ := filepath.Glob(filepath.Join(pf, "*.dll"))
	for _, d := range dlls {
		if n := filepath.Base(d); strings.HasPrefix(n, "Microsoft.CodeAnalysis") || strings.HasPrefix(n, "System.") {
			cp(d, filepath.Join(bare, n))
		}
	}
	for _, mode := range [][]string{{"-Probe"}, {"-Src", wslToWindows(src), "-Out", wslToWindows(filepath.Join(work, "out2"))}} {
		out, err = run(append(mode, "-NT8Bin", wslToWindows(bare))...)
		if err == nil || !strings.Contains(out, "NinjaTrader.Core.dll not found") {
			t.Errorf("%v without NinjaTrader.Core must stop and name it: %v", mode[0], err)
		}
	}
}
