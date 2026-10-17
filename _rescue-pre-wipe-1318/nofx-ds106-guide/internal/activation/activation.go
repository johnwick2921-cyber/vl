// Package activation performs the steps of a vl update: resolve a release,
// prove the binary it contains, back up the database, swap it in, watch the new
// process prove itself, and roll back if it does not.
//
// It is a LIBRARY, not a procedure. deploy/cutover.sh v6 had the right steps in
// the right order and no durable state, no per-step evidence, and no caller but
// a human; the M4 worker needs the same steps driven from a persisted job. So
// every step here returns a Receipt and the library NEVER writes job state —
// the caller persists the receipt BEFORE performing the side effect, which is
// what lets a crashed worker resume. Each step is idempotent at its own step: a
// second call after success is a no-op with OK=true and Evidence["already"]="true".
package activation

import (
	"crypto/md5"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Release is one versioned runtime on disk: NOFX_RELEASE_DIR/<sha>/.
// It is a DIRECTORY, never a set of sibling files — the `vl-bin.old.<sha>.<ts>`
// naming the v6 script used could collide and could not carry the dist or the
// RELEASE marker alongside the binary it belonged to.
type Release struct {
	Dir          string
	SHA          string
	Binary       string
	Dist         string
	ReleaseFile  string
	ManifestPath string
}

// Identity is the ONLY identity of a running vl process: its pid together
// with the start time from /proc/<pid>/stat field 22. A pid alone answers
// "does some process exist"; the question is always "is this the SAME process
// I measured". A recycled pid must be refused, never signalled.
type Identity struct {
	PID        int
	StartTicks uint64
}

// Receipt is what a step produces. Evidence carries the values the step
// actually READ — never a value it assumed, and never a fabricated empty
// (an uncomputed field is absent; a computed-empty one is present and empty).
type Receipt struct {
	Step      string            `json:"step"`
	StartedAt time.Time         `json:"started_at"`
	EndedAt   time.Time         `json:"ended_at"`
	OK        bool              `json:"ok"`
	Evidence  map[string]string `json:"evidence,omitempty"`
	Err       string            `json:"err,omitempty"`
}

// Manifest is the subset of a release manifest this package reads.
type Manifest struct {
	SourceSHA string `json:"source_sha"`
	BinaryMD5 string `json:"binary_md5"`
	Signature string `json:"signature_verdict"`
}

func newReceipt(step string) Receipt {
	return Receipt{Step: step, StartedAt: time.Now(), Evidence: map[string]string{}}
}

func (r Receipt) fail(err error) (Receipt, error) {
	r.EndedAt = time.Now()
	r.OK = false
	r.Err = err.Error()
	return r, err
}

func (r Receipt) done() (Receipt, error) {
	r.EndedAt = time.Now()
	r.OK = true
	return r, nil
}

// Resolve reads a release directory and refuses one that cannot be trusted.
// 3a's release workflow already verified the signature; this re-reads the
// verdict and the sha256s rather than assuming the earlier step ran — a
// guarantee nobody re-checks is a guarantee that silently lapses.
func Resolve(dir string) (Release, error) {
	if dir == "" {
		return Release{}, fmt.Errorf("release dir is empty")
	}
	bin, err := releaseBinaryName(dir)
	if err != nil {
		return Release{}, fmt.Errorf("release %s: %w", dir, err)
	}
	rel := Release{
		Dir:          dir,
		Binary:       filepath.Join(dir, bin),
		Dist:         filepath.Join(dir, "web", "dist"),
		ReleaseFile:  filepath.Join(dir, "RELEASE"),
		ManifestPath: filepath.Join(dir, "manifest.json"),
	}
	b, err := os.ReadFile(rel.ManifestPath)
	if err != nil {
		return Release{}, fmt.Errorf("release %s has no readable manifest.json: %w", dir, err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Release{}, fmt.Errorf("release %s has an unparseable manifest.json: %w", dir, err)
	}
	if m.SourceSHA == "" {
		return Release{}, fmt.Errorf("release %s: manifest names no source_sha", dir)
	}
	// A manifest with no signature verdict is NOT a manifest that passed: an
	// absent verdict and a failed one must not read the same (A24).
	if m.Signature == "" {
		return Release{}, fmt.Errorf("release %s: manifest carries no signature verdict; refusing rather than assuming it was verified", dir)
	}
	rel.SHA = m.SourceSHA
	return rel, nil
}

// releaseBinaryName returns the release dir's ONE binary name: vl-bin when
// present, else vl-bin; BOTH present is refused (the dir must hold EXACTLY
// ONE — R5 removes the vl branch when the rename lands). Neither present is
// refused too: a release without a binary would only fail later, after a kill.
func releaseBinaryName(dir string) (string, error) {
	vl, vlErr := os.Stat(filepath.Join(dir, "vl-bin"))
	nfx, nfxErr := os.Stat(filepath.Join(dir, "nofx-bin"))
	vlOK := vlErr == nil && vl.Mode().IsRegular()
	nfxOK := nfxErr == nil && nfx.Mode().IsRegular()
	switch {
	case vlOK && nfxOK:
		return "", fmt.Errorf("holds BOTH vl-bin and nofx-bin; a release dir must hold exactly one")
	case vlOK:
		return "vl-bin", nil
	default:
		// Neither present keeps the old reading (vl-bin); the activate
		// step fails on the missing binary, as it always did.
		return "nofx-bin", nil
	}
}

// Manifest re-reads the manifest for a resolved release.
func (r Release) Manifest() (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(r.ManifestPath)
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(b, &m)
}

// Stage proves the binary in a release IS the binary that release claims.
//
// It reads the build info out of the binary with debug/buildinfo rather than
// parsing `go version -m` output. The v6 script parsed that text and read the
// wrong field, because the output is TAB-separated and the parser had been
// written from memory of what it "looks like" — every cutover would have
// refused its own binary (CLASS 241). The library asks the toolchain instead.
func Stage(rel Release) (Receipt, error) {
	rc := newReceipt("stage")
	m, err := rel.Manifest()
	if err != nil {
		return rc.fail(fmt.Errorf("cannot read manifest: %w", err))
	}
	info, err := buildinfo.ReadFile(rel.Binary)
	if err != nil {
		return rc.fail(fmt.Errorf("cannot read build info from %s: %w", rel.Binary, err))
	}
	var rev, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	// CLASS 248: a binary with NO vcs stamps is not "the wrong binary" — it was
	// built somewhere Go does not stamp, and the cure is a different build, not
	// a different file. Go does not stamp a build made from a linked git
	// worktree, and -buildvcs=true does not error, it just stamps nothing. Told
	// "this is not the binary for that sha", an operator checks a sha that is
	// already correct and concludes the check is broken.
	if rev == "" {
		return rc.fail(fmt.Errorf(
			"%s carries NO vcs stamps at all, so its identity cannot be proven; "+
				"Go does not stamp a build from a linked git worktree — build from a clean clone or the main tree",
			rel.Binary))
	}
	rc.Evidence["vcs.revision"] = rev
	rc.Evidence["vcs.modified"] = modified
	rc.Evidence["manifest.source_sha"] = m.SourceSHA
	if rev != m.SourceSHA {
		return rc.fail(fmt.Errorf("%s is stamped, but with revision %s, not the manifest's %s", rel.Binary, rev, m.SourceSHA))
	}
	if modified != "false" {
		return rc.fail(fmt.Errorf("%s was built from a DIRTY tree (vcs.modified=%q)", rel.Binary, modified))
	}
	sum, err := fileMD5(rel.Binary)
	if err != nil {
		return rc.fail(fmt.Errorf("cannot checksum %s: %w", rel.Binary, err))
	}
	rc.Evidence["binary_md5"] = sum
	if m.BinaryMD5 != "" {
		rc.Evidence["manifest.binary_md5"] = m.BinaryMD5
		if sum != m.BinaryMD5 {
			return rc.fail(fmt.Errorf("%s hashes to %s, but the manifest names %s", rel.Binary, sum, m.BinaryMD5))
		}
	}
	return rc.done()
}

func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// statField22 reads the start time from a /proc/<pid>/stat line.
//
// THE PARSING TRAP: field 2 is the executable name in parentheses and MAY
// CONTAIN SPACES AND PARENTHESES ("(my prog (2))"), so splitting the line on
// whitespace and indexing shifts every later field. Everything after the LAST
// ')' is safe to split; field 22 is then the 20th of those.
func statField22(line string) (uint64, error) {
	i := strings.LastIndex(line, ")")
	if i < 0 {
		return 0, fmt.Errorf("stat line has no comm field")
	}
	rest := strings.Fields(line[i+1:])
	// After ')' the fields are 3,4,5,...; field 22 is index 22-3 = 19.
	const idx = 19
	if len(rest) <= idx {
		return 0, fmt.Errorf("stat line has %d fields after comm, need at least %d", len(rest), idx+1)
	}
	var v uint64
	if _, err := fmt.Sscanf(rest[idx], "%d", &v); err != nil {
		return 0, fmt.Errorf("stat field 22 %q is not a number: %w", rest[idx], err)
	}
	return v, nil
}

// failID is fail() for the steps that also return an Identity.
func (r Receipt) failID(err error) (Identity, Receipt, error) {
	rr, e := r.fail(err)
	return Identity{}, rr, e
}

// atomicCopy writes src to dst via a temp file in dst's OWN directory and a
// rename. The temp file must share a filesystem with the destination or the
// rename degrades to a copy and stops being atomic — which is the whole point:
// a half-written binary must never be observable at the destination path.
func atomicCopy(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".activate-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil { // durable before it is visible
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, st.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}

// atomicSwapDir puts src's CONTENTS at dst by staging a sibling directory and
// renaming it into place, so a reader never sees a half-populated dist.
func atomicSwapDir(src, dst string) error {
	staged := dst + ".activating"
	_ = os.RemoveAll(staged)
	if err := copyTree(src, staged); err != nil {
		return err
	}
	old := dst + ".replaced"
	_ = os.RemoveAll(old)
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			return err
		}
	}
	if err := os.Rename(staged, dst); err != nil {
		_ = os.Rename(old, dst) // put it back rather than leave nothing there
		return err
	}
	_ = os.RemoveAll(old)
	return nil
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, info.Mode().Perm())
	})
}
