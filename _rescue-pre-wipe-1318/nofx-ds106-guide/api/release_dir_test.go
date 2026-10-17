package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// L4: the knob is OFF by default and OFF must be byte-identical to before.
// This is the test that makes "additive" a fact rather than an intention.
func TestUnsetReleaseDirServesExactlyTheOldPath(t *testing.T) {
	t.Setenv("NOFX_RELEASE_DIR", "")
	resetReleaseDirForTest()
	if got := ResolvedDistDir(); got != UIDistDir {
		t.Fatalf("ResolvedDistDir() = %q with the knob unset, want the historical %q", got, UIDistDir)
	}
	if got := CurrentReleaseDir(); got != "" {
		t.Fatalf("CurrentReleaseDir() = %q with the knob unset, want empty", got)
	}
}

func TestSetReleaseDirServesFromTheActiveRelease(t *testing.T) {
	root := t.TempDir()
	t.Setenv("NOFX_RELEASE_DIR", root)
	resetReleaseDirForTest()
	want := filepath.Join(root, "current", UIDistDir)
	if got := ResolvedDistDir(); got != want {
		t.Fatalf("ResolvedDistDir() = %q, want %q", got, want)
	}
}

// Resolved ONCE: a path that can change under a running process is a path two
// parts of the same boot will read differently, and then nothing agrees about
// what is being served.
func TestReleaseDirIsResolvedOnceAndDoesNotMoveUnderTheProcess(t *testing.T) {
	first := t.TempDir()
	t.Setenv("NOFX_RELEASE_DIR", first)
	resetReleaseDirForTest()
	before := ResolvedDistDir()

	// Change the environment the way a careless caller might.
	if err := os.Setenv("NOFX_RELEASE_DIR", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if after := ResolvedDistDir(); after != before {
		t.Fatalf("the resolved dist moved under the process: %q -> %q", before, after)
	}
}

// The 🗂 line's golden, in BOTH states. It is a separate line precisely so the
// 🖥 golden stays byte-identical when the knob is off.
func TestReleaseDirBootLineGoldenUnset(t *testing.T) {
	t.Setenv("NOFX_RELEASE_DIR", "")
	resetReleaseDirForTest()
	want := "release-dir: n/a — versioned runtimes off; serving web/dist and reading deploy/RELEASE"
	if got := ReleaseDirBootLine(); got != want {
		t.Fatalf("unset golden drifted:\n got: %s\nwant: %s", got, want)
	}
}

func TestReleaseDirBootLineGoldenSet(t *testing.T) {
	t.Setenv("NOFX_RELEASE_DIR", "/srv/nofx/releases")
	resetReleaseDirForTest()
	want := "release-dir: /srv/nofx/releases/current — serving /srv/nofx/releases/current/web/dist and reading /srv/nofx/releases/current/RELEASE"
	if got := ReleaseDirBootLine(); got != want {
		t.Fatalf("set golden drifted:\n got: %s\nwant: %s", got, want)
	}
}

// An unset knob must never print an empty gap that reads as a missing field.
func TestReleaseDirBootLineNeverPrintsAnEmptyValue(t *testing.T) {
	t.Setenv("NOFX_RELEASE_DIR", "")
	resetReleaseDirForTest()
	line := ReleaseDirBootLine()
	if !strings.Contains(line, "n/a") {
		t.Fatalf("an unknowable value must be NAMED n/a, got: %s", line)
	}
	if strings.Contains(line, ": —") || strings.Contains(line, "  ") {
		t.Fatalf("the line has an empty gap where a value belongs: %q", line)
	}
}
