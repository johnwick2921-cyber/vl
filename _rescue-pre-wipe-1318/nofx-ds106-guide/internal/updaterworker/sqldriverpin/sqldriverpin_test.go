// Package sqldriverpin holds the D4 pins, in a test-only package that links
// NOTHING of the worker set on purpose: the failure they guard is an init-time
// panic ("sql: Register called twice for driver sqlite"), and a pin living in
// a test binary that links the worker set would die of that panic before it
// could say which binary links which two drivers. From here the toolchain is
// asked (and the real binary is built and run), and the answer names them.
package sqldriverpin_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// The two packages that register the database/sql driver "sqlite". The repo's
// ONE registration site is store/sqlitedriver: modernc in the default build,
// glebarez under -tags cgofree — never both in one binary.
const (
	modernc  = "modernc.org/sqlite"
	glebarez = "github.com/glebarez/go-sqlite"
)

// PIN (D4, CTO ruling 1790302009885): no binary linking the worker set
// registers the "sqlite" driver twice. internal/activation (#201) blank-
// imported glebarez/go-sqlite beside store/sqlitedriver's modernc; any binary
// linking both — the updater linking vl/store (the hold) AND activation (the
// adapter) — panicked at init. For each binary the worker set reaches, the
// toolchain's own dependency list must not carry both registrations (and must
// carry one: a probe that sees neither proves nothing).
//
// The in-process half — this runs inside the updaterworker test binary itself
// — is updaterworker's TestTheWorkerTestBinaryRegistersSQLiteOnce.
func TestNoBinaryLinkingTheWorkerSetRegistersADuplicateSQLDriver(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root %s: %v", root, err)
	}
	for _, c := range []struct {
		binary string
		args   []string
		tags   string
		want   string // the ONE driver the build tag selects
	}{
		{"the vl-updater binary (./cmd/vl-updater)", []string{"list", "-deps", "-f", "{{.ImportPath}}", "./cmd/vl-updater"}, "", modernc},
		{"the vl-updater binary under -tags cgofree", []string{"list", "-deps", "-f", "{{.ImportPath}}", "./cmd/vl-updater"}, "cgofree", glebarez},
		{"the updaterworker test binary (./internal/updaterworker)", []string{"list", "-deps", "-test", "-f", "{{.ImportPath}}", "./internal/updaterworker"}, "", modernc},
		{"the updaterworker test binary under -tags cgofree", []string{"list", "-deps", "-test", "-f", "{{.ImportPath}}", "./internal/updaterworker"}, "cgofree", glebarez},
		{"the api test binary (./api)", []string{"list", "-deps", "-test", "-f", "{{.ImportPath}}", "./api"}, "", modernc},
		{"the api test binary under -tags cgofree", []string{"list", "-deps", "-test", "-f", "{{.ImportPath}}", "./api"}, "cgofree", glebarez},
	} {
		t.Run(c.binary, func(t *testing.T) {
			deps := goList(t, root, c.tags, c.args...)
			var drivers []string
			for _, d := range []string{modernc, glebarez} {
				if deps[d] {
					drivers = append(drivers, d)
				}
			}
			sort.Strings(drivers)
			switch {
			case len(drivers) == 0:
				t.Fatalf("%s links neither %s nor %s — the probe is not seeing the binary (%d deps)", c.binary, modernc, glebarez, len(deps))
			case len(drivers) == 2:
				t.Fatalf("%s links BOTH %s — two registrations of the database/sql driver \"sqlite\"; it panics at init "+
					"(\"sql: Register called twice for driver sqlite\"). Every package imports nofx/store/sqlitedriver, never a driver.",
					c.binary, strings.Join(drivers, " AND "))
			case drivers[0] != c.want:
				// one driver, the wrong one for this tag: a build that flipped
				// store/sqlitedriver's backend choice would slip past the
				// not-both check and still panic in any binary that links the
				// other registration.
				t.Fatalf("%s links %s — under this build the ONE registration must be %s", c.binary, drivers[0], c.want)
			}
		})
	}
}

// goList runs `go <args>` (goCommand) under the build tags and returns the
// import paths it prints.
func goList(t *testing.T, root string, tags string, args ...string) map[string]bool {
	t.Helper()
	if tags != "" {
		// -tags is a flag of the subcommand, not of go itself
		args = append([]string{args[0], "-tags", tags}, args[1:]...)
	}
	cmd := goCommand(root, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	deps := map[string]bool{}
	for _, ln := range strings.Split(string(out), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			deps[ln] = true
		}
	}
	return deps
}

// PIN (D4, the build smoke): the vl-updater binary, built exactly as an
// operator builds it (go build -o <dir>/vl-updater ./cmd/vl-updater), gets
// through init and answers its no-argument usage: exit 2, the usage line on
// stderr, nothing on stdout, no panic. No argument means run() prints the
// usage BEFORE it resolves an install, reads an env or opens anything — the
// run touches nothing (cmd/vl-updater/main.go run: len(rest) == 0).
func TestUpdaterBinaryInitsWithoutPanic(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "vl-updater")
	goBuild(t, root, "build", "-o", bin, "./cmd/vl-updater")
	cmd := exec.Command(bin)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()} // no token, no install, no inherited knobs
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	var ee *exec.ExitError
	code := 0
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running %s: %v", bin, err)
	}
	const usage = "usage: vl-updater [--install-dir d] serve | fetch <release_id> | status [<job>] | resume <job> | recovery <job>\n"
	if strings.Contains(stderr.String(), "panic:") || code != 2 || stderr.String() != usage || stdout.Len() != 0 {
		t.Fatalf("the built vl-updater did not init cleanly: exit %d\nstdout %q\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
}

// goBuild runs the go command in the census environment and fails on an error.
func goBuild(t *testing.T, root string, args ...string) {
	t.Helper()
	if out, err := goCommand(root, args...).CombinedOutput(); err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// goCommand is the toolchain that built this test (GOTOOLCHAIN=local), else
// the go on PATH, offline with a read-only go.mod — internal/censuswalk's
// census environment (its goCommand is unexported).
func goCommand(root string, args ...string) *exec.Cmd {
	bin := filepath.Join(runtime.GOROOT(), "bin", "go")
	env := append(os.Environ(), "GOPROXY=off", "GOFLAGS=-mod=readonly", "GOWORK=off")
	if _, err := os.Stat(bin); err == nil {
		env = append(env, "GOTOOLCHAIN=local")
	} else {
		bin = "go"
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir, cmd.Env = root, env
	return cmd
}
