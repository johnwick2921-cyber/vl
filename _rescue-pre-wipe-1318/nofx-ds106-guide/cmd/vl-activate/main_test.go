package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildCLI compiles the command under test. The CLI's contract is about its
// EXIT CODE and its STDOUT, which only a real process has.
func buildCLI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "nofx-activate")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

type receipt struct {
	Step     string            `json:"step"`
	OK       bool              `json:"ok"`
	Evidence map[string]string `json:"evidence"`
	Err      string            `json:"err"`
}

func runCLI(t *testing.T, bin string, args ...string) (receipt, int, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run: %v", err)
		}
	}
	var r receipt
	if s := strings.TrimSpace(stdout.String()); s != "" {
		if err := json.Unmarshal([]byte(s), &r); err != nil {
			t.Fatalf("stdout is not one JSON receipt: %v\n%s", err, s)
		}
	}
	return r, code, stderr.String()
}

// A refusal must still PRINT its receipt. The evidence is most valuable
// exactly when the step failed, and a caller piping stdout should not have to
// choose between the exit code and the reason.
func TestCLIPrintsAReceiptAndExitsNonZeroOnARefusal(t *testing.T) {
	bin := buildCLI(t)
	r, code, stderr := runCLI(t, bin, "verify", "-release", filepath.Join(t.TempDir(), "nope"))
	if code == 0 {
		t.Fatal("CLI exited 0 on a release it could not resolve")
	}
	if r.Step != "verify" {
		t.Fatalf("receipt step = %q, want verify — the refusal printed no receipt", r.Step)
	}
	if r.OK {
		t.Fatal("receipt says OK on a refusal")
	}
	if r.Err == "" {
		t.Fatal("receipt carries no reason")
	}
	if stderr == "" {
		t.Fatal("nothing on stderr for a human")
	}
}

// The manifest refusal must survive the CLI boundary: a release whose manifest
// carries no signature verdict is refused, and the reason says so.
func TestCLIRefusesAReleaseWhoseManifestHasNoSignatureVerdict(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"source_sha":"`+strings.Repeat("a", 40)+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	r, code, _ := runCLI(t, bin, "verify", "-release", dir)
	if code == 0 {
		t.Fatal("CLI accepted a manifest with no signature verdict")
	}
	if !strings.Contains(r.Err, "no signature verdict") {
		t.Fatalf("reason does not name the cause: %q", r.Err)
	}
}

func TestCLIBackupPrintsTheIntegrityEvidence(t *testing.T) {
	bin := buildCLI(t)
	// A database the CLI can actually copy.
	src := filepath.Join(t.TempDir(), "data.db")
	mk := exec.Command("go", "run", "-", src)
	mk.Stdin = strings.NewReader(`package main
import ("database/sql"; "os"; _ "github.com/glebarez/go-sqlite")
func main(){ db,_ := sql.Open("sqlite", os.Args[1]); db.Exec("create table t(a int)"); db.Close() }`)
	if out, err := mk.CombinedOutput(); err != nil {
		t.Skipf("cannot make a fixture database here: %v\n%s", err, out)
	}
	dest := filepath.Join(t.TempDir(), "copy.db")
	r, code, stderr := runCLI(t, bin, "backup", "-db", src, "-dest", dest)
	if code != 0 {
		t.Fatalf("backup failed: %s", stderr)
	}
	if r.Evidence["integrity_check"] != "ok" {
		t.Fatalf("receipt must carry the verification it performed, got %+v", r.Evidence)
	}
}

func TestCLIRejectsAnUnknownSubcommand(t *testing.T) {
	bin := buildCLI(t)
	_, code, stderr := runCLI(t, bin, "frobnicate")
	if code == 0 {
		t.Fatal("CLI exited 0 on an unknown subcommand")
	}
	if !strings.Contains(stderr, "unknown subcommand") && !strings.Contains(stderr, "nofx-activate") {
		t.Fatalf("stderr does not help the caller: %q", stderr)
	}
}
