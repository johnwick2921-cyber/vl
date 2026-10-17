package installpath

import (
	"os"
	"path/filepath"
	"testing"
)

// THE BUG THIS PINS: the first version of the feature read the environment in
// TWO packages — api latched it behind a sync.Once, kernel called os.Getenv on
// every use. Those disagree the moment the environment changes, and then the
// process serves a bundle from one release while judging its boot integrity
// against another, each half internally consistent and quietly describing a
// different install. Resolving "once" per package is not resolving once.
//
// Both consumers now come through the functions below, so a test here is a
// test of both by construction.
func TestEveryConsumerSeesTheSameReleaseDirAfterTheEnvironmentMoves(t *testing.T) {
	first := t.TempDir()
	t.Setenv("NOFX_RELEASE_DIR", first)
	ResetReleaseDirForTest()

	root := ReleaseDir()
	cur := CurrentReleaseDir()
	marks := ReleaseMarkerPaths()
	dist := filepath.Join(cur, "web/dist") // what api derives

	// Move the environment the way a careless caller (or a restarted unit
	// manager) might.
	if err := os.Setenv("NOFX_RELEASE_DIR", t.TempDir()); err != nil {
		t.Fatal(err)
	}

	if got := ReleaseDir(); got != root {
		t.Fatalf("ReleaseDir moved under the process: %q -> %q", root, got)
	}
	if got := CurrentReleaseDir(); got != cur {
		t.Fatalf("CurrentReleaseDir moved: %q -> %q", cur, got)
	}
	got := ReleaseMarkerPaths()
	if len(got) != len(marks) || got[0] != marks[0] {
		t.Fatalf("ReleaseMarkerPaths moved: %v -> %v", marks, got)
	}
	// The two halves must still describe the SAME release: the marker the
	// kernel reads and the dist the api serves come from one directory.
	if filepath.Dir(got[0]) != filepath.Dir(filepath.Dir(dist)) {
		t.Fatalf("the marker (%s) and the bundle (%s) are in DIFFERENT releases", got[0], dist)
	}
}

// L4: unset is the default and must be exactly what it always was.
func TestUnsetIsExactlyTheHistoricalSinglePath(t *testing.T) {
	t.Setenv("NOFX_RELEASE_DIR", "")
	ResetReleaseDirForTest()
	if got := ReleaseDir(); got != "" {
		t.Fatalf("ReleaseDir() = %q with the knob unset, want empty", got)
	}
	if got := CurrentReleaseDir(); got != "" {
		t.Fatalf("CurrentReleaseDir() = %q, want empty", got)
	}
	got := ReleaseMarkerPaths()
	if len(got) != 1 || got[0] != "deploy/RELEASE" {
		t.Fatalf("unset must resolve to exactly [deploy/RELEASE], got %v", got)
	}
}

func TestSetPrefersTheActiveReleasesOwnMarker(t *testing.T) {
	root := t.TempDir()
	t.Setenv("NOFX_RELEASE_DIR", root)
	ResetReleaseDirForTest()
	got := ReleaseMarkerPaths()
	if len(got) != 2 {
		t.Fatalf("want the active marker and the fallback, got %v", got)
	}
	if want := filepath.Join(root, "current", "RELEASE"); got[0] != want {
		t.Fatalf("first path = %q, want the ACTIVE release's marker %q", got[0], want)
	}
	if got[1] != "deploy/RELEASE" {
		t.Fatalf("fallback = %q, want deploy/RELEASE", got[1])
	}
}
