package installpath

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// VL_RELEASE_DIR turns the install into VERSIONED runtimes:
//
//      VL_RELEASE_DIR/<sha>/{vl-bin,web/dist,RELEASE,manifest.json}
//      VL_RELEASE_DIR/current -> <sha>
//
// with `current` a symlink, so an activation or a rollback moves all three
// halves at once and can never leave a mixed install.
//
// THIS IS THE ONE RESOLVER, for the same reason the data directory has one.
// The first version of this feature read os.Getenv in TWO places: api resolved
// it once behind a sync.Once, kernel re-read it on every call. Those two can
// DISAGREE — the api half keeps the value it latched while the kernel half
// picks up a later one — so the process would serve a bundle from one release
// while judging its boot integrity against another, and each half would be
// internally consistent and quietly describing a different install. The whole
// point of resolving once is defeated by resolving once per package.
//
// UNSET is the default and means today's behaviour, byte for byte.
var (
	releaseDirOnce sync.Once
	releaseDirVal  string
)

// ReleaseDir returns the configured release root, or "" when unset. Resolved
// ONCE per process: a path that can change under a running process is a path
// two parts of the same boot will read differently.
func ReleaseDir() string {
	releaseDirOnce.Do(func() {
		releaseDirVal = strings.TrimSpace(os.Getenv("VL_RELEASE_DIR")) // R5: VL_ only
	})
	return releaseDirVal
}

// CurrentReleaseDir is VL_RELEASE_DIR/current, or "" when unset.
func CurrentReleaseDir() string {
	root := ReleaseDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "current")
}

// ReleaseMarkerPaths lists, in priority order, the files that may declare which
// revision this process is supposed to be running.
//
// Unset returns exactly one path — "deploy/RELEASE" — so the behaviour and the
// boot line are byte-identical to what they have always been. Set, the ACTIVE
// release's own marker is consulted first: deploy/RELEASE in the working
// directory belongs to whatever tree the process was launched from, and under
// a versioned install that tree answers for a DIFFERENT build.
func ReleaseMarkerPaths() []string {
	cur := CurrentReleaseDir()
	if cur == "" {
		return []string{"deploy/RELEASE"}
	}
	return []string{filepath.Join(cur, "RELEASE"), "deploy/RELEASE"}
}

// ResetReleaseDirForTest lets a test vary the environment. Production never
// calls it: the whole point of the sync.Once is that the value cannot move
// under a running process.
func ResetReleaseDirForTest() {
	releaseDirOnce = sync.Once{}
	releaseDirVal = ""
}
