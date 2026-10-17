package updaterworker

// releaseroot.go — W-ONE-BUTTON M4 (3b-B, unit U4F): the ONE check of the
// release root (NOFX_RELEASE_DIR, read through installpath's one resolver)
// against the installation. `vl-updater fetch` materializes under it and the
// re-proof adapter refuses a verdict that is not under it, so both call this
// and nothing else.
//
// Containment is decided on RESOLVED paths and on path ELEMENTS:
//   - the root must be its own resolved path (filepath.EvalSymlinks): a symlink
//     anywhere in it is refused, because text that looks outside the install
//     can still point inside (verifier U4N defect 2, probe P2);
//   - the install dir is resolved too, so an install named through a symlink
//     cannot make a root inside the real install look outside;
//   - the root is outside the install only when the relative path IS ".." or
//     starts with "../" — never a string prefix: "<install>/..rel" is a
//     directory inside the install whose name starts with ".." (defect 1).
//
// The last two are the module's ONE containment check, PathWithin
// (containment.go, U4F verify note 5).

import (
	"errors"
	"fmt"
	"path/filepath"

	"vl/internal/installpath"
)

// ErrReleaseRoot is every refusal of the release root.
var ErrReleaseRoot = errors.New("release root refused")

// ReleaseRoot returns the configured release root, resolved, after checking it
// against the installation at installDir. It refuses an unset or relative
// NOFX_RELEASE_DIR, one that is not its own resolved path, and one that is the
// install or inside it.
func ReleaseRoot(installDir string) (string, error) {
	root := installpath.ReleaseDir()
	if root == "" {
		return "", fmt.Errorf("%w: NOFX_RELEASE_DIR is not set (the release root the release is materialized under)", ErrReleaseRoot)
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("%w: NOFX_RELEASE_DIR=%q must be an absolute path", ErrReleaseRoot, root)
	}
	root = filepath.Clean(root)
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("%w: NOFX_RELEASE_DIR=%s cannot be resolved: %w", ErrReleaseRoot, root, err)
	}
	if realRoot != root {
		return "", fmt.Errorf("%w: NOFX_RELEASE_DIR=%s is not its own resolved path (it resolves to %s) — name the real directory, with no symlink in it", ErrReleaseRoot, root, realRoot)
	}
	// The install must itself resolve (U4G defect 4, restored): a
	// non-existent or dangling install is refused here — containment against
	// an install that does not exist is not "outside", it is unknown.
	realInstall, err := filepath.EvalSymlinks(installDir)
	if err != nil {
		return "", fmt.Errorf("%w: the install %s cannot be resolved: %w", ErrReleaseRoot, installDir, err)
	}
	inside, err := PathWithin(realRoot, realInstall)
	if err != nil {
		return "", fmt.Errorf("%w: NOFX_RELEASE_DIR=%s cannot be checked against the install %s: %w", ErrReleaseRoot, root, installDir, err)
	}
	if inside {
		return "", fmt.Errorf("%w: NOFX_RELEASE_DIR=%s must be outside the install %s", ErrReleaseRoot, root, installDir)
	}
	return realRoot, nil
}
