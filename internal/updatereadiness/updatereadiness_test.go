package updatereadiness

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
// item stays ok.
func okRunner() Runner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "powershell.exe" {
			for _, a := range args {
				if a == "-Probe" {
					return []byte("VL_ADDON_PROBE=ok\nVL_SELFTEST_OK\n"), nil
				}
			}
		}
		return nil, nil
	}
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
	items := Check(context.Background(), Config{DocsDir: docs, Run: okRunner()})
	if len(items) != 5 {
		t.Fatalf("got %d items, want 5: %+v", len(items), items)
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

func TestCheckConnectOnStartupOff(t *testing.T) {
	docs := buildDocs(t)
	writeCompiler(t, docs)
	writeConfig(t, docs, `<ConnectOptions>
  <TradovateOptions>
    <Name>funded</Name>
    <ConnectOnStartup>false</ConnectOnStartup>
  </TradovateOptions>
</ConnectOptions>`)
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
