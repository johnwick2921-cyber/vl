package updatereadiness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func buildDocs(t *testing.T) string {
	t.Helper()
	docs := filepath.Join(t.TempDir(), "NinjaTrader 8")
	for _, d := range []string{"bin/Custom/AddOns"} {
		if err := os.MkdirAll(filepath.Join(docs, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return docs
}

func writeConfig(t *testing.T, docs, xml string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(docs, "Config.xml"), []byte(xml), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeCompiler(t *testing.T, docs string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(docs, "bin", "Microsoft.CodeAnalysis.CSharp.dll"), []byte("PE"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// okRunner fakes the Windows seam: the -Probe call (the no-SDK Roslyn compiler
// check) reports ok; every other command succeeds, so the "PowerShell reachable"
// item stays ok. It matches the RESOLVED tool by basename: CompilerProbe resolves
// a bare "powershell.exe" to an absolute path before invoking Run
// (W-UPDATER-WINPATH), while the "PowerShell reachable" probe still reaches Run
// with the bare name — basename matches both.
func okRunner() Runner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if filepath.Base(name) == "powershell.exe" {
			for _, a := range args {
				if a == "-Probe" {
					return []byte("VL_ADDON_PROBE=ok\nVL_SELFTEST_OK\n"), nil
				}
			}
		}
		return nil, nil
	}
}

// powershellFixture writes a hermetic executable named powershell.exe and
// prepends its dir to PATH, so resolveWindowsExe's LookPath finds IT (never a
// real Windows binary) on any box — including CI, where no /mnt/c files exist.
func powershellFixture(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "powershell.exe"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func find(items []Item, name string) *Item {
	for i := range items {
		if items[i].Item == name {
			return &items[i]
		}
	}
	return nil
}

const tradovateOn = `<ConnectOptions>
  <TradovateOptions>
    <TypeName>NinjaTrader.Cbi.TradovateOptions</TypeName>
    <Name>funded</Name>
    <ConnectOnStartup>true</ConnectOnStartup>
  </TradovateOptions>
  <KinetickOptions>
    <Name>Kinetick</Name>
    <ConnectOnStartup>false</ConnectOnStartup>
  </KinetickOptions>
</ConnectOptions>`

func TestCheckAllGreen(t *testing.T) {
	docs := buildDocs(t)
	writeCompiler(t, docs)
	writeConfig(t, docs, tradovateOn)
	powershellFixture(t)
	items := Check(context.Background(), Config{DocsDir: docs, Run: okRunner()})
	if len(items) != 7 {
		t.Fatalf("got %d items, want 7: %+v", len(items), items)
	}
	for _, name := range []string{"NT8 Documents folder", "NT8 Roslyn compiler", "PowerShell reachable", "Connect on startup", "One-time AddOn migration"} {
		it := find(items, name)
		if it == nil || !it.OK {
			t.Fatalf("%s not ok: %+v", name, it)
		}
	}
	if comp := find(items, "NT8 Roslyn compiler"); comp == nil || comp.Detail == "" {
		t.Fatalf("compiler detail should carry the FileVersion, got %+v", comp)
	}
	// The two release-#35 items are present even with nil seams: accounts
	// fails closed, the addon-build comparison is informational.
	if it := find(items, "NinjaTrader accounts known"); it == nil || it.OK {
		t.Fatalf("nil accounts seam must fail closed: %+v", it)
	}
	if it := find(items, "AddOn build in NT8 vs release"); it == nil || !it.OK {
		t.Fatalf("nil addon-build seam must be informational-ok: %+v", it)
	}
}

func TestCheckReportsMissingDocsDir(t *testing.T) {
	items := Check(context.Background(), Config{Detect: func() (string, error) {
		return "", os.ErrNotExist
	}, Run: func(ctx context.Context, name string, args ...string) ([]byte, error) { return nil, nil }})
	if it := find(items, "NT8 Documents folder"); it == nil || it.OK {
		t.Fatalf("missing docs dir must be not-ok: %+v", it)
	}
	// powershell is probed independently even when the docs dir is absent.
	if it := find(items, "PowerShell reachable"); it == nil || !it.OK {
		t.Fatalf("powershell must still be probed when the docs dir is missing: %+v", it)
	}
}

func TestCheckMissingCompiler(t *testing.T) {
	docs := buildDocs(t)
	writeConfig(t, docs, tradovateOn)
	// A runner that never reports VL_ADDON_PROBE=ok: the no-SDK ps1 path does
	// not work, so the compiler item must read not-ok (GAP 2: true only when
	// the ps1 path works — file presence alone is no longer enough).
	items := Check(context.Background(), Config{DocsDir: docs, Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, nil
	}})
	it := find(items, "NT8 Roslyn compiler")
	if it == nil || it.OK {
		t.Fatalf("compiler must be not-ok when the no-SDK probe does not report ok: %+v", it)
	}
}

// W-UPDATER-WINPATH: on a Linux-only box with no Windows host (no powershell.exe
// on PATH AND no fixed WSL mount), the compiler probe must fail closed with a
// detail that names the unresolved tool — never pass, never panic.
func TestCheckCompilerFailsClosedWithoutWindowsHost(t *testing.T) {
	docs := buildDocs(t)
	writeCompiler(t, docs)
	writeConfig(t, docs, tradovateOn)
	t.Setenv("PATH", "/usr/bin:/bin") // no powershell.exe anywhere
	items := Check(context.Background(), Config{
		DocsDir:       docs,
		Run:           okRunner(), // never reached for the compiler item: resolution fails first
		PowerShellWSL: "/nonexistent/powershell.exe",
	})
	it := find(items, "NT8 Roslyn compiler")
	if it == nil || it.OK {
		t.Fatalf("compiler must fail closed with no Windows host: %+v", it)
	}
	if !strings.Contains(it.Detail, "powershell.exe") || !strings.Contains(it.Detail, "/nonexistent/powershell.exe") {
		t.Fatalf("detail should name the unresolved tool and the fixed path: %+v", it)
	}
}

func TestCheckConnectOnStartupOff(t *testing.T) {
	docs := buildDocs(t)
	writeCompiler(t, docs)
	writeConfig(t, docs, `<ConnectOptions>
  <TradovateOptions>
    <Name>funded</Name>
    <ConnectOnStartup>false</ConnectOnStartup>
  </TradovateOptions>
</ConnectOptions>`)
	powershellFixture(t)
	items := Check(context.Background(), Config{DocsDir: docs, Run: okRunner()})
	it := find(items, "Connect on startup")
	if it == nil || it.OK {
		t.Fatalf("a Tradovate connection with ConnectOnStartup=false must be not-ok: %+v", it)
	}
	if it.Detail != "funded=off" {
		t.Fatalf("detail = %q, want the connection name and state", it.Detail)
	}
}

func TestCheckMigrationPending(t *testing.T) {
	docs := buildDocs(t)
	writeCompiler(t, docs)
	writeConfig(t, docs, tradovateOn)
	if err := os.WriteFile(filepath.Join(docs, "bin", "Custom", "AddOns", "VLTrader.cs"), []byte("//"), 0o600); err != nil {
		t.Fatal(err)
	}
	powershellFixture(t)
	items := Check(context.Background(), Config{DocsDir: docs, Run: okRunner()})
	it := find(items, "One-time AddOn migration")
	if it == nil || it.OK {
		t.Fatalf("VL*.cs still present must be not-ok (one F5 owed): %+v", it)
	}
}

func TestCheckPowershellUnreachable(t *testing.T) {
	docs := buildDocs(t)
	writeCompiler(t, docs)
	writeConfig(t, docs, tradovateOn)
	items := Check(context.Background(), Config{DocsDir: docs, Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, os.ErrNotExist
	}})
	it := find(items, "PowerShell reachable")
	if it == nil || it.OK {
		t.Fatalf("an unreachable powershell must be not-ok: %+v", it)
	}
}

func TestParseConnections(t *testing.T) {
	conns := parseConnections(tradovateOn)
	if len(conns) != 2 {
		t.Fatalf("got %d connections, want 2: %+v", len(conns), conns)
	}
	if conns[0].block != "TradovateOptions" || conns[0].name != "funded" || conns[0].csu != "true" {
		t.Fatalf("first connection = %+v, want TradovateOptions/funded/true", conns[0])
	}
	if conns[1].block != "KinetickOptions" || conns[1].csu != "false" {
		t.Fatalf("second connection = %+v, want KinetickOptions/false", conns[1])
	}
}

func TestWindowsPath(t *testing.T) {
	if got := windowsPath("/mnt/c/Users/hoang/Documents/NinjaTrader 8"); got != `C:\Users\hoang\Documents\NinjaTrader 8` {
		t.Fatalf("windowsPath = %q", got)
	}
}

// ── release #35: "NinjaTrader accounts known" ──────────────────────────────

func accountsSeam(info AccountsInfo, err error) func(context.Context) (AccountsInfo, error) {
	return func(context.Context) (AccountsInfo, error) { return info, err }
}

func TestCheckAccountsLiveSIM(t *testing.T) {
	it := checkAccounts(context.Background(), Config{Accounts: accountsSeam(AccountsInfo{
		Live:     true,
		Accounts: []Account{{Name: "SimAccount1", IsSim: true}, {Name: "Sim101", IsSim: true}},
	}, nil)})
	if !it.OK {
		t.Fatalf("two live SIM accounts must be ok: %+v", it)
	}
	if it.Detail != "live — SimAccount1=on, Sim101=on" {
		t.Fatalf("detail = %q, want the live list", it.Detail)
	}
}

func TestCheckAccountsPersistedLastSeen(t *testing.T) {
	seen := time.Date(2026, 10, 17, 9, 50, 0, 0, time.UTC)
	it := checkAccounts(context.Background(), Config{Accounts: accountsSeam(AccountsInfo{
		Live:     false,
		SeenAt:   seen,
		Accounts: []Account{{Name: "Sim101", IsSim: true}},
	}, nil)})
	if !it.OK {
		t.Fatalf("a persisted SIM list must be ok: %+v", it)
	}
	// The detail must carry the last-seen clock, not "live" (DS-108's list).
	if !strings.Contains(it.Detail, "last seen ") || strings.Contains(it.Detail, "live") {
		t.Fatalf("detail = %q, want 'last seen hh:mm'", it.Detail)
	}
}

func TestCheckAccountsNoneKnown(t *testing.T) {
	it := checkAccounts(context.Background(), Config{Accounts: accountsSeam(AccountsInfo{}, nil)})
	if it.OK {
		t.Fatalf("an empty account list must be not-ok (fail closed): %+v", it)
	}
}

func TestCheckAccountsError(t *testing.T) {
	it := checkAccounts(context.Background(), Config{Accounts: accountsSeam(AccountsInfo{}, os.ErrNotExist)})
	if it.OK {
		t.Fatalf("a read error must be not-ok (fail closed): %+v", it)
	}
}

func TestCheckAccountsLiveAccountRefused(t *testing.T) {
	it := checkAccounts(context.Background(), Config{Accounts: accountsSeam(AccountsInfo{
		Live:     true,
		Accounts: []Account{{Name: "Sim101", IsSim: true}, {Name: "PropAcct", IsSim: false}},
	}, nil)})
	if it.OK {
		t.Fatalf("a non-SIM account must refuse the gate: %+v", it)
	}
	if !strings.Contains(it.Fix, "PropAcct") {
		t.Fatalf("fix = %q, want it to name the non-SIM account", it.Fix)
	}
}

// ── release #35: "AddOn build in NT8 vs release" (informational) ───────────

func TestCheckAddOnBuildMatch(t *testing.T) {
	it := checkAddOnBuild(context.Background(), Config{AddOnBuild: func(context.Context) (AddOnBuildInfo, error) {
		return AddOnBuildInfo{Running: "b1", Release: "b1"}, nil
	}})
	if !it.OK || !strings.Contains(it.Detail, "matches the release") {
		t.Fatalf("a matching build must be ok with the match detail: %+v", it)
	}
}

func TestCheckAddOnBuildMismatchReconciles(t *testing.T) {
	it := checkAddOnBuild(context.Background(), Config{AddOnBuild: func(context.Context) (AddOnBuildInfo, error) {
		return AddOnBuildInfo{Running: "old", Release: "new"}, nil
	}})
	if !it.OK || !strings.Contains(it.Detail, "rebuilds + restarts") {
		t.Fatalf("a mismatch must be ok (the walk-away rebuilds): %+v", it)
	}
}

func TestCheckAddOnBuildClosed(t *testing.T) {
	it := checkAddOnBuild(context.Background(), Config{AddOnBuild: func(context.Context) (AddOnBuildInfo, error) {
		return AddOnBuildInfo{}, nil
	}})
	if !it.OK || !strings.Contains(it.Detail, "closed") {
		t.Fatalf("NT8 closed must be ok with the closed detail: %+v", it)
	}
}

func TestCheckAddOnBuildReleaseUnknown(t *testing.T) {
	it := checkAddOnBuild(context.Background(), Config{AddOnBuild: func(context.Context) (AddOnBuildInfo, error) {
		return AddOnBuildInfo{Running: "b1"}, nil
	}})
	if !it.OK || !strings.Contains(it.Detail, "compared during the update") {
		t.Fatalf("a running-only build must be ok and defer the release side: %+v", it)
	}
}
