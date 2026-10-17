package api

import (
	"path/filepath"

	"vl/internal/installpath"
)

// The release-dir resolver lives in internal/installpath — the ONE resolver,
// beside the data-directory one, for the same reason: two packages each
// reading the environment can disagree with each other, and then the process
// serves a bundle from one release while judging its integrity against
// another. These are thin pass-throughs so api's call sites keep reading
// naturally.

// ReleaseDir returns the configured release root, or "" when unset.
func ReleaseDir() string { return installpath.ReleaseDir() }

// CurrentReleaseDir is NOFX_RELEASE_DIR/current, or "" when unset.
func CurrentReleaseDir() string { return installpath.CurrentReleaseDir() }

// ResolvedDistDir is where the served bundle actually lives: under the active
// release when NOFX_RELEASE_DIR is set, else the working-directory-relative
// path this process has always used.
func ResolvedDistDir() string {
	cur := installpath.CurrentReleaseDir()
	if cur == "" {
		return UIDistDir
	}
	return filepath.Join(cur, UIDistDir)
}

func resetReleaseDirForTest() { installpath.ResetReleaseDirForTest() }

// ReleaseDirBootLine reports which versioned runtime this process resolved, as
// its OWN line rather than a field appended to an existing one.
//
// WHY A SEPARATE LINE: §3 asked for both "unset ⇒ byte-identical boot lines"
// and "the boot line prints the resolved dir, n/a when unset". Appending an
// n/a segment to the 🖥 line would change its bytes and break every golden
// that reads it, so the two requirements are only compatible if the new value
// gets its own line. Unset still prints — an absent knob is a FACT worth
// stating, and a reader who sees no line at all cannot tell "unset" from
// "this build does not have the feature".
//
// The value is READ from the resolver, never assumed, and an unset knob prints
// the literal n/a rather than an empty gap that looks like a missing field.
func ReleaseDirBootLine() string {
	root := ReleaseDir()
	if root == "" {
		return "release-dir: n/a — versioned runtimes off; serving " + UIDistDir + " and reading deploy/RELEASE"
	}
	return "release-dir: " + CurrentReleaseDir() +
		" — serving " + ResolvedDistDir() +
		" and reading " + CurrentReleaseDir() + "/RELEASE"
}
