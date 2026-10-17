package updaterwire

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vl/internal/censuswalk"
	"vl/internal/installpath"
)

func TestSocketPathResolvesViaInstallpath(t *testing.T) {
	inst := t.TempDir()
	t.Setenv("DB_PATH", "")
	os.Unsetenv("DB_PATH")
	// default: <install>/data/updater/worker.sock
	got, err := SocketPathFor(inst)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(installpath.DataDir(inst, installpath.DefaultDBPath), "updater", "worker.sock")
	if got != want {
		t.Fatalf("SocketPathFor = %q, want %q", got, want)
	}
	// DB_PATH from <install>/.env moves the data dir — and the socket with it
	if err := os.WriteFile(filepath.Join(inst, ".env"), []byte("DB_PATH=var/db/bot.db\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = SocketPathFor(inst)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(inst, "var", "db", "updater", "worker.sock"); got != want {
		t.Fatalf("SocketPathFor with .env DB_PATH = %q, want %q", got, want)
	}
	// the process environment wins over .env, exactly as for the bot
	t.Setenv("DB_PATH", "/abs/elsewhere/x.db")
	got, _ = SocketPathFor(inst)
	if got != "/abs/elsewhere/updater/worker.sock" {
		t.Fatalf("env DB_PATH: %q", got)
	}
}

func TestSocketPathRefusesRelativeEmptyOrTooLong(t *testing.T) {
	for _, d := range []string{"", "data", "./data", "../data"} {
		if _, err := SocketPath(d); err == nil {
			t.Fatalf("SocketPath(%q) must refuse a non-absolute data dir", d)
		}
	}
	long := "/" + strings.Repeat("d", 120)
	if _, err := SocketPath(long); err == nil {
		t.Fatal("a socket path over the 107-byte sun_path limit must be refused, not truncated")
	}
	if p, err := SocketPath("/srv/nofx/data"); err != nil || p != "/srv/nofx/data/updater/worker.sock" {
		t.Fatalf("positive control: %q %v", p, err)
	}
}

// The socket filename literal lives in ONE file (as "hold.json" lives only in
// store/maintenance_hold.go): nothing else can name — and so unlink, chmod or
// squat — the worker socket without going through SocketPath.
func TestWorkerSocketLiteralIsConfined(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	offenders, scanned, err := workerSocketLiteralOffenders(root)
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 100 {
		t.Fatalf("scan saw only %d files", scanned)
	}
	if len(offenders) > 0 {
		t.Fatalf("the worker socket may be named only by internal/updaterwire/paths.go:\n%s", strings.Join(offenders, "\n"))
	}
}

// workerSocketLiteralOffenders scans every non-test .go file under root (the
// ONE root-only walk, internal/censuswalk — M3 fold M5) for the socket
// filename literal outside internal/updaterwire/paths.go.
func workerSocketLiteralOffenders(root string) (offenders []string, scanned int, err error) {
	files, err := censuswalk.NonTestGoFiles(root)
	if err != nil {
		return nil, 0, err
	}
	for _, file := range files {
		rel := file.Rel
		f, perr := parser.ParseFile(token.NewFileSet(), file.Path, nil, 0)
		if perr != nil {
			offenders = append(offenders, rel+": cannot be parsed")
			continue
		}
		scanned++
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && strings.Contains(lit.Value, "worker.sock") && rel != "internal/updaterwire/paths.go" {
				offenders = append(offenders, rel+": names the worker socket (\"worker.sock\")")
			}
			return true
		})
	}
	return offenders, scanned, nil
}
