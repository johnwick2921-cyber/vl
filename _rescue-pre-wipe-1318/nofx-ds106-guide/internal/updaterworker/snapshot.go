package updaterworker

import (
	"fmt"
	"io/fs"
	"path/filepath"
)

// ── the job-scoped snapshot of the install (C4 as ruled) ───────────────────
//
// Activate overwrites the install's three halves in place, so BEFORE it the
// worker copies them out with the library's Snapshot to
// <BackupRoot>/<job>/install/ — 103's layout at #201 afd60391:
// <dest>/vl-bin, <dest>/web/dist, <dest>/RELEASE (not deploy/RELEASE: U1
// verifier note 11) — and proves the copy before trusting it: the binary's
// vcs stamps are the install's clean sha, and every half is byte-identical to
// the install's. The rollback is RollbackTo(snapshot, install, id).

// snapshotRelease addresses a snapshot dir as a Release (sha = the install's;
// binName is the install's basename, kept as-is — a snapshot of a vl-bin
// install restores to vl-bin). // R5 removes the nofx name.
func snapshotRelease(dir, sha, binName string) Release {
	return Release{
		Dir:         dir,
		SHA:         sha,
		Binary:      filepath.Join(dir, binName),
		Dist:        filepath.Join(dir, "web", "dist"),
		ReleaseFile: filepath.Join(dir, "RELEASE"),
	}
}

// verifySnapshot is nil only when snap is a faithful copy of install.
func (w *Worker) verifySnapshot(snap, install Release) error {
	rev, mod, err := w.host.BuildInfo(snap.Binary)
	if err != nil {
		return fmt.Errorf("snapshot binary: %w", err)
	}
	if rev != install.SHA || mod != "false" {
		return fmt.Errorf("snapshot binary is %s (modified=%s), not the install's clean %s", rev, orNA(mod), install.SHA)
	}
	for _, p := range [][2]string{{snap.Binary, install.Binary}, {snap.ReleaseFile, install.ReleaseFile}} {
		a, err := sha256File(p[0])
		if err != nil {
			return fmt.Errorf("snapshot: %w", err)
		}
		b, err := sha256File(p[1])
		if err != nil {
			return fmt.Errorf("install: %w", err)
		}
		if a != b {
			return fmt.Errorf("snapshot %s differs from the install's %s", p[0], p[1])
		}
	}
	a, err := treeHashes(snap.Dist)
	if err != nil {
		return fmt.Errorf("snapshot dist: %w", err)
	}
	b, err := treeHashes(install.Dist)
	if err != nil {
		return fmt.Errorf("install dist: %w", err)
	}
	if len(b) == 0 {
		return fmt.Errorf("install dist %s is empty", install.Dist)
	}
	if !equalSets(a, b) {
		return fmt.Errorf("snapshot dist differs from the install's (%d vs %d files)", len(a), len(b))
	}
	return nil
}

// treeHashes is rel path → sha256 for every regular file under dir; anything
// but a directory or a regular file (a symlink, a device) refuses.
func treeHashes(dir string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", p)
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		sum, err := sha256File(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = sum
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
