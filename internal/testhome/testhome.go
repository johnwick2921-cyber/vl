// Package testhome is the shared P0 test-isolation guard for every package
// whose tests can reach the REAL updater worker state under the process HOME
// (~/.config/vl-updater/*, ~/bin/vl-updater). A package installs it in its
// TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testhome.Guard(m)) }
//
// Guard snapshots the real paths before the tests and fails the run if ANY
// of them changed (created, deleted, content, or mtime) — the live box's
// worker credential and binary must survive every test run byte-identically.
// A test that forgets its HOME isolation trips the guard: every test that can
// reach these paths must set t.Setenv("HOME", t.TempDir()) (or run through a
// package seam that does).
//
// Born from TEST-WROTE-REAL-WORKER-ENV-2 (owner P0 2026-10-02): a root
// package test ran the real enroll CLI and rewrote the live
// ~/.config/vl-updater/env with a freshly minted test token. The earlier
// guard covered internal/updaterbootstrap only; this helper is the widened
// version any package can mount.
//
// WIDENED 2026-10-17 (owner order after the 2026-10-16 19:05–19:14 CDT wipe of
// /home/hoang, THE ONE RULE): every test package in the repo mounts this guard
// (internal/homeguard lint fails the build otherwise), and Run now
//
//  1. PREVENTS: points HOME, XDG_CONFIG_HOME, XDG_DATA_HOME, XDG_CACHE_HOME
//     and XDG_STATE_HOME at a fresh os.MkdirTemp folder for the whole test
//     process, pinning GOCACHE/GOMODCACHE/GOPATH to their real values first
//     so `go` subprocesses keep working — a test (or a script it runs) that
//     forgets its own isolation writes into the throwaway folder, never the
//     real home;
//  2. DETECTS: snapshots the REAL home (the passwd entry, which a stray
//     t.Setenv cannot move) — every top-level entry name plus the updater
//     paths byte-for-byte — and fails the run (rc 1) if any top-level entry
//     vanished, the home folder itself vanished, or an updater path changed.
package testhome

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"vl/internal/safepath"
)

type fileStamp struct {
	sha256 string
	mtime  int64
}

// watchedPaths are the real-HOME paths the guard snapshots: every file under
// ~/.config/vl-updater, and the installed worker binary ~/bin/vl-updater.
// Each rel path is resolved against the process HOME at run time.
var watchedPaths = []string{
	".config/vl-updater", // directory — every file inside is stamped
	"bin/vl-updater",     // file — stamped if it exists
	// 2026-10-17 (DS-106 adversarial review, hole 2): the dotfiles a test or a
	// child process with a cleared env would rewrite through the passwd home.
	".gitconfig",
	".bashrc",
	".profile",
	".config/go/env", // GOENV
	".ssh/authorized_keys",
	".ssh/config",
	".ssh/id_ed25519",
	".ssh/id_rsa",
}

// listedDirs are the real-home folders whose ENTRY NAMES are snapshotted
// (depth 1): a name that vanishes, or a NEW name a test wrote there through a
// literal path or the passwd home, fails the run (DS-106 hole 2).
var listedDirs = []string{"", ".config", "bin", ".ssh", ".local", ".local/share", ".local/state", ".local/bin", ".cache"} // .local/.cache: DS-106 round 2

// concurrentOK names the top-level entries OTHER processes on this box create
// during a run — any new top-level directory (worktrees, clones, trash) and
// shell history files. They never fail the run; everything else new does.
func concurrentOK(rel string, isDir bool) bool {
	if strings.Contains(rel, "/") {
		return false
	}
	if isDir {
		// Lanes create worktrees, sync clones and the trash folder at the top
		// of the home while suites run (2026-10-17: ~/partner-sync-1017 tripped
		// the guard mid-suite). A new top-level DIRECTORY is therefore not a
		// signal; a new top-level FILE, anything new in the listed config/bin/
		// ssh/local/cache folders, and every deletion still are.
		return true
	}
	switch rel {
	case ".bash_history", ".lesshst", ".viminfo", ".wget-hsts", ".python_history", ".node_repl_history", ".sudo_as_admin_successful":
		return true
	}
	return false
}

// Sig maps a rel path under the real HOME to its stamp. A missing file or
// dir reads as absent (no entry), never fabricated.
func Sig() map[string]fileStamp { return SigAt(RealHome()) }

// SigAt is Sig against an explicit home folder ("" → empty signature).
func SigAt(home string) map[string]fileStamp {
	sig := map[string]fileStamp{}
	if home == "" {
		return sig
	}
	var stamp func(rel string)
	stamp = func(rel string) {
		p := filepath.Join(home, filepath.FromSlash(rel))
		fi, err := os.Stat(p)
		if err != nil {
			return // absent: no entry
		}
		if fi.IsDir() {
			entries, err := os.ReadDir(p)
			if err != nil {
				return
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				stamp(rel + "/" + e.Name())
			}
			return
		}
		b, err := os.ReadFile(p)
		sum := "unreadable"
		if err == nil {
			h := sha256.Sum256(b)
			sum = hex.EncodeToString(h[:])
		}
		sig[rel] = fileStamp{sha256: sum, mtime: fi.ModTime().UnixNano()}
	}
	for _, rel := range watchedPaths {
		stamp(rel)
	}
	return sig
}

// RealHome is the passwd home folder — the one the 2026-10-16 wipe took. It
// falls back to $HOME only when the passwd lookup fails, and is read before
// Run redirects HOME.
func RealHome() string {
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return filepath.Clean(u.HomeDir)
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Clean(h)
	}
	return ""
}

// realHomeFn is RealHome behind a seam so the guard's own tests can point it
// at a throwaway folder and prove a deletion trips rc 1. Production callers
// never touch it.
var realHomeFn = RealHome

// listing maps every entry name (depth 1) in each listedDirs folder of home
// to whether it is a directory. Missing folders contribute nothing.
func listing(home string) map[string]bool {
	out := map[string]bool{}
	if home == "" {
		return out
	}
	for _, d := range listedDirs {
		entries, err := os.ReadDir(filepath.Join(home, d))
		if err != nil {
			continue
		}
		for _, e := range entries {
			out[filepath.ToSlash(filepath.Join(d, e.Name()))] = e.IsDir()
		}
	}
	return out
}

// isolate points every home-derived variable at a fresh temp folder and
// returns that folder plus a restore func. The Go tool caches are pinned to
// their pre-redirect values first so `go` subprocesses do not rebuild the
// world or try to download a toolchain into the throwaway home.
func isolate(realHome string) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "vl-testhome-")
	if err != nil {
		return "", nil, err
	}
	saved := map[string]*string{}
	set := func(k, v string) {
		if _, done := saved[k]; !done {
			if old, ok := os.LookupEnv(k); ok {
				o := old
				saved[k] = &o
			} else {
				saved[k] = nil
			}
		}
		_ = os.Setenv(k, v)
	}
	keep := func(k, fallback string) {
		if os.Getenv(k) == "" && fallback != "" {
			set(k, fallback)
		}
	}
	if realHome != "" {
		cache := os.Getenv("XDG_CACHE_HOME")
		if cache == "" {
			cache = filepath.Join(realHome, ".cache")
		}
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			gopath = filepath.Join(realHome, "go")
		}
		keep("GOCACHE", filepath.Join(cache, "go-build"))
		keep("GOPATH", gopath)
		keep("GOMODCACHE", filepath.Join(gopath, "pkg", "mod"))
	}
	// A test that runs git commit needs an identity; the real ~/.gitconfig
	// is out of reach on purpose.
	keep("GIT_AUTHOR_NAME", "vl-test")
	keep("GIT_AUTHOR_EMAIL", "vl-test@example.invalid")
	keep("GIT_COMMITTER_NAME", "vl-test")
	keep("GIT_COMMITTER_EMAIL", "vl-test@example.invalid")
	set("HOME", tmp)
	set("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	set("XDG_DATA_HOME", filepath.Join(tmp, ".local", "share"))
	set("XDG_CACHE_HOME", filepath.Join(tmp, ".cache"))
	set("XDG_STATE_HOME", filepath.Join(tmp, ".local", "state"))
	restore := func() {
		for k, v := range saved {
			if v == nil {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, *v)
			}
		}
	}
	return tmp, restore, nil
}

// Guard runs the package tests under Run. Mount it in TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testhome.Guard(m)) }
func Guard(m *testing.M) int { return Run(m.Run) }

// Run is Guard for a TestMain that already wraps m.Run (testtmpfs.Run, a
// clock seam, …): os.Exit(testhome.Run(func() int { return testtmpfs.Run(m.Run) })).
func Run(run func() int) int {
	real := realHomeFn()
	before := SigAt(real)
	beforeList := listing(real)

	tmp, restore, err := isolate(real)
	if err != nil {
		fmt.Fprintf(os.Stderr, "TEST-ISOLATION GUARD: cannot create the throwaway HOME: %v\n", err)
		return 1
	}
	code := run()
	restore()
	// Only the folder this process made, only under os.TempDir() — the
	// one-rule exception (a temp dir created in this run).
	_ = safepath.RemoveAllTemp(tmp)

	var changed []string
	if real != "" {
		if _, err := os.Stat(real); err != nil {
			changed = append(changed, real+" — the real home folder itself is GONE")
		}
	}
	afterList := listing(real)
	for name := range beforeList {
		if _, ok := afterList[name]; !ok {
			changed = append(changed, "~/"+name+" DELETED during the tests (entry of the real home)")
		}
	}
	for name, isDir := range afterList {
		if _, ok := beforeList[name]; !ok && !concurrentOK(name, isDir) {
			changed = append(changed, "~/"+name+" CREATED during the tests (a test wrote the real home)")
		}
	}
	after := SigAt(real)
	for name, b := range before {
		if a, ok := after[name]; !ok {
			changed = append(changed, name+" DELETED during the tests")
		} else if a.sha256 != b.sha256 || a.mtime != b.mtime {
			changed = append(changed, name+" CHANGED during the tests")
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			changed = append(changed, name+" CREATED during the tests")
		}
	}
	sort.Strings(changed)
	if len(changed) > 0 {
		fmt.Fprintf(os.Stderr, "TEST-ISOLATION GUARD FAILED — the REAL home (%s) changed during the package tests:\n  %s\nTests run with HOME pointed at a throwaway folder; a test that reaches the real home through user.Current, a literal path or an unguarded delete is a P0 (owner order 2026-10-17, THE ONE RULE).\n", real, strings.Join(changed, "\n  "))
		return 1
	}
	return code
}
