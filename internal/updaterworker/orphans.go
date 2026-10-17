package updaterworker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vl/internal/updaterwire"
)

// ── D8b (CTO 1790279155144 (4), 1790280128238): the interrupted fetch ──────
//
// The attended fetch renames its staging dir to <release_root>/<source_sha>
// and only THEN links <data>/updater/verdicts/<release_id>.json (the verdict
// is written last, on purpose). A kill between those two steps leaves a
// materialized release dir that no verdict names. Such a dir is never
// activated — every path to activation starts from a verdict (the install
// verb refuses "release not verified"; downloaded/verified re-read it) — but
// it blocked every later fetch of that sha at "the release directory already
// exists (it may be the running release)".
//
// PROVENANCE, not absence. A <sha> dir no verdict names is not proof of an
// interrupted fetch: U3's accepted rule is that an existing release dir is
// NEVER overwritten (it may be the running release, placed by other means),
// and TestVerdictWrittenOnlyAfterEveryCheck pins that refusal. So the fetch
// leaves its own mark: MarkFetchPending writes <release_root>/.pending-<sha>
// (fsynced) immediately BEFORE its rename, and ClearFetchPending removes it
// after the verdict link (or after the fetch removed its own dir on a failed
// link). The recovery (quarantineLocked, which the fetch runs FIRST under the
// release-root lock) acts ONLY on a marker:
//
//	marker + <sha> dir + no verdict names it → the interrupted fetch's: the dir
//	                                           is quarantined (renamed to
//	                                           .orphan-<sha>-<unix>, bytes kept,
//	                                           never deleted), marker removed
//	marker + <sha> dir + a verdict names it  → the fetch finished; marker removed
//	marker, no <sha> dir                     → killed before its rename; marker removed
//	<sha> dir, no marker                     → not the fetch's: untouched (the
//	                                           fetch then refuses "never overwritten")
//
// The lock is an exclusive flock on the release root DIRECTORY (no lock file:
// a refused fetch leaves the release root empty); non-blocking — a fetch in
// flight makes the recovery refuse. Any verdict that cannot be read refuses
// the whole recovery (it cannot then prove a dir is unreferenced).

// ErrFetchInFlight: another fetch holds the release root's lock.
var ErrFetchInFlight = errors.New("updaterworker: a fetch is in flight (the release root lock is held)")

const (
	orphanPrefix       = ".orphan-"
	fetchPendingPrefix = ".pending-"
	verdictsSubdir     = "verdicts"
	maxVerdictRead     = 64 << 10
)

// LockReleaseRoot flocks the release root directory exclusively without
// waiting. The fetch holds it from before its rename until after its verdict
// link, so the recovery below never races a live fetch.
func LockReleaseRoot(releaseRoot string) (func(), error) {
	if releaseRoot == "" || !filepath.IsAbs(releaseRoot) {
		return nil, errors.New("updaterworker: the release root must be an absolute path")
	}
	f, err := os.OpenFile(releaseRoot, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrFetchInFlight
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

func pendingMarker(releaseRoot, sha string) (string, error) {
	if !isSHA40(sha) {
		return "", fmt.Errorf("updaterworker: %q is not a release sha", sha)
	}
	return filepath.Join(releaseRoot, fetchPendingPrefix+sha), nil
}

// MarkFetchPending records, durably, that a fetch is about to rename its
// staging dir to <release_root>/<sha>. The fetch calls it (holding the lock)
// immediately before the rename.
func MarkFetchPending(releaseRoot, sha string) error {
	p, err := pendingMarker(releaseRoot, sha)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return syncDirPath(releaseRoot)
}

// ClearFetchPending removes the marker (idempotent when absent). The fetch
// calls it after its verdict link, or after removing its own dir on a failed
// link.
func ClearFetchPending(releaseRoot, sha string) error {
	p, err := pendingMarker(releaseRoot, sha)
	if err != nil {
		return err
	}
	//homeguard:allow — one fetch-pending marker file under releaseRoot, resolved by pendingMarker
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDirPath(releaseRoot)
}

// QuarantineInterruptedFetches takes the lock and runs the recovery; it
// returns the quarantined dirs' new names.
func QuarantineInterruptedFetches(releaseRoot, dataDir string, now time.Time) ([]string, error) {
	unlock, err := LockReleaseRoot(releaseRoot)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return quarantineLocked(releaseRoot, dataDir, now)
}

// quarantineLocked is the recovery for a caller that ALREADY holds the
// release-root lock — the fetch itself (at the U3 fold: FetchRelease takes
// the lock, runs this, and keeps the lock through its rename and verdict
// link, so no recovery can ever see its own in-between state).
func quarantineLocked(releaseRoot, dataDir string, now time.Time) ([]string, error) {
	named, err := verdictReleaseDirs(dataDir)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(releaseRoot)
	if err != nil {
		return nil, err
	}
	var moved []string
	for _, e := range ents {
		sha, ok := strings.CutPrefix(e.Name(), fetchPendingPrefix)
		if !ok || !isSHA40(sha) || !e.Type().IsRegular() {
			continue
		}
		dir := filepath.Join(releaseRoot, sha)
		fi, err := os.Lstat(dir)
		switch {
		case errors.Is(err, os.ErrNotExist):
			// killed before its rename: nothing landed
		case err != nil:
			return moved, err
		case fi.IsDir() && !named[dir]:
			to := filepath.Join(releaseRoot, orphanPrefix+sha+"-"+strconv.FormatInt(now.Unix(), 10))
			if err := os.Rename(dir, to); err != nil {
				return moved, fmt.Errorf("updaterworker: quarantine %s: %w", dir, err)
			}
			moved = append(moved, to)
		}
		// named by a verdict (the fetch finished), or not a dir: leave it
		if err := ClearFetchPending(releaseRoot, sha); err != nil {
			return moved, err
		}
	}
	return moved, nil
}

// verdictReleaseDirs is the set of release_dir values every verdict names.
// Any verdict that cannot be read or parsed refuses the whole set.
func verdictReleaseDirs(dataDir string) (map[string]bool, error) {
	if dataDir == "" || !filepath.IsAbs(dataDir) {
		return nil, errors.New("updaterworker: the data dir must be an absolute path")
	}
	dir := filepath.Join(dataDir, updaterwire.UpdaterDirName, verdictsSubdir)
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	var names []string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		p := filepath.Join(dir, n)
		f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return nil, fmt.Errorf("updaterworker: verdict %s: %w", n, err)
		}
		b, err := io.ReadAll(io.LimitReader(f, maxVerdictRead))
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("updaterworker: verdict %s: %w", n, err)
		}
		var v struct {
			ReleaseDir *string `json:"release_dir"`
		}
		if err := json.Unmarshal(b, &v); err != nil || v.ReleaseDir == nil || !filepath.IsAbs(*v.ReleaseDir) {
			return nil, fmt.Errorf("updaterworker: verdict %s names no absolute release_dir — refusing to judge any release dir unreferenced", n)
		}
		out[filepath.Clean(*v.ReleaseDir)] = true
	}
	return out, nil
}

func syncDirPath(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
