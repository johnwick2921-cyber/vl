package updaterworker

// release.go — W-ONE-BUTTON M4 (3b-B, unit U3): turn a LOCAL release archive
// (3a's <release_id>.tar.gz, copied into NOFX_RELEASE_INBOX by the owner — no
// network code exists here) into a release directory in the layout
// internal/activation's Resolve reads, and record a verdict ONLY after every
// check has passed (brief C6/C7/C8/C9, CTO ruling 1790259689740).
//
// The archive (release.yml: `tar -C /tmp/stage -czf <id>.tar.gz .`) carries
// the staged allow-list (vl-bin, web/dist/…, deploy/RELEASE, ninjascript/…,
// LICENSE, optional updater binaries and calendar) plus, at its root, the
// signed pair manifest.json + manifest.json.sig. FetchRelease:
//
//  1. refuses at once if a verdict for the release id already exists
//     (written once, immutable — only an operator removing it re-opens the id);
//  2. extracts into a private staging dir inside the release root, refusing
//     every entry that is absolute, home-relative, carries "..", a backslash
//     or any non-canonical spelling, is a symlink / hard link / device / fifo /
//     anything but a regular file or directory, is setuid/setgid/sticky, or
//     repeats; writes go through os.Root, so even a missed spelling cannot
//     leave the staging dir;
//  3. verifies the SSHSIG over the signed manifest bytes (sshsig.go);
//  4. parses the signed manifest: release_id must be the one asked for,
//     source_sha 40 lowercase hex, artifacts[] non-null and non-empty, each a
//     canonical path with a 64-hex sha256 and a byte count, no duplicates, and
//     none under a name the materialized layout owns — so the 0-byte
//     manifest.json self-entry today's release.yml produces is refused (C8);
//  5. re-hashes EVERY artifacts[] entry (size and sha256) and refuses any
//     extra, missing or changed file — only the signed pair itself is exempt;
//  6. requires the layout: vl-bin, web/dist/index.html and deploy/RELEASE
//     listed, vl-bin owner-executable, deploy/RELEASE naming source_sha;
//  7. materializes activation's layout in the staging dir: RELEASE (a copy of
//     deploy/RELEASE), manifest.json = {source_sha, binary_md5 (computed),
//     signature_verdict "sshsig:release:SHA256:…"}, and the signed pair moved
//     byte-identical to signed/manifest.json + signed/manifest.json.sig;
//  8. re-proves the materialized tree with the SAME functions the job's
//     downloaded/verified states call (RehashRelease, ReverifyRelease);
//  9. renames the staging dir to <release_root>/<source_sha> (refused if that
//     exists — it may be the running release);
//  10. writes <data>/updater/verdicts/<release_id>.json (0600, dirs 0700,
//     no-clobber link) — the updaterjob.Verdict the app-side reader
//     (updaterjob.ReadVerdict) accepts; this package is its ONLY writer. If
//     THAT fails, the just-renamed release dir is removed. A KILL between 9
//     and 10 cannot run that cleanup: it leaves a verdict-less dir carrying
//     this fetch's pending marker (written before the rename), and the NEXT
//     FetchRelease quarantines it under the release-root lock before doing
//     anything else (D8b). A verdict-less dir is never read as verified, and
//     a dir without our marker is never touched ("never overwritten").
//
// Any refusal removes the staging dir; nothing outside it is ever written
// before step 9.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"vl/internal/updaterjob"
	"vl/internal/updaterwire"
)

// Refusal classes; each wraps the detail so a test can prove WHICH check refused.
var (
	ErrFetchConfig      = errors.New("release: fetch configuration refused")
	ErrArchive          = errors.New("release: archive is unreadable or over a limit")
	ErrUnsafeEntry      = errors.New("release: unsafe archive entry")
	ErrManifest         = errors.New("release: signed manifest is malformed or disagrees")
	ErrArtifactMismatch = errors.New("release: files do not match the signed manifest")
	ErrLayout           = errors.New("release: release layout is incomplete or inconsistent")
	ErrReleaseDirExists = errors.New("release: the release directory already exists")
	ErrVerdictExists    = errors.New("release: a verdict already exists for this release id")
	ErrVerdictWrite     = errors.New("release: the verdict could not be written")
)

const (
	// Limits: an archive over any of them is refused, never truncated.
	MaxArchiveEntries = 20000
	MaxReleaseBytes   = 2 << 30 // total uncompressed bytes of regular files
	MaxManifestBytes  = 8 << 20
	maxTrailingBytes  = 1 << 20 // tar record padding after the end-of-archive blocks
	maxPathBytes      = 1024
	maxElementBytes   = 255

	// The signed pair at the archive root (release.yml signs manifest.json there).
	signedManifestName = "manifest.json"
	signedSigName      = "manifest.json.sig"
	// The materialized layout (activation.Resolve: <dir>/{vl-bin|nofx-bin,
	// web/dist, RELEASE, manifest.json}) plus the signed pair kept under
	// signed/. A release dir holds EXACTLY ONE binary (R5 removes the vl
	// name).
	signedDir      = "signed"
	activationMfst = "manifest.json"
	releaseMarker  = "RELEASE"
	archiveMarker  = "deploy/RELEASE"
	binaryNameVL   = "vl-bin"
	binaryName     = "nofx-bin"
	distIndex      = "web/dist/index.html"
	stagingPrefix  = ".fetch-"
)

var (
	sha40Re  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Artifact is one artifacts[] entry of 3a's manifest (manifest.sh).
type Artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

// SignedManifest is what the worker reads from the SIGNED manifest bytes.
// It is only ever returned by a call that verified or re-hashed those bytes.
type SignedManifest struct {
	ReleaseID    string
	SourceSHA    string
	Artifacts    []Artifact
	AddonBuildID string // addon.build_id verbatim ("n/a" passes through; "" = absent)
	SHA256       string // sha256 of the signed manifest bytes
}

// FetchConfig is everything FetchRelease reads; it reads no environment.
type FetchConfig struct {
	Archive        string               // the local <release_id>.tar.gz (a path, never a URL)
	ReleaseID      string               // the id asked for; the signed manifest must say the same
	ReleaseRoot    string               // NOFX_RELEASE_DIR: absolute, a real dir, owner-only writable
	AllowedSigners string               // <installDir>/deploy/release_allowed_signers
	DataDir        string               // the installation's data dir (absolute)
	Now            func() time.Time     // nil ⇒ time.Now
	Logf           func(string, ...any) // nil ⇒ no warning line (a verdict-link dir-fsync warning)
}

// activationManifest is activation.Manifest's JSON, exactly.
type activationManifest struct {
	SourceSHA string `json:"source_sha"`
	BinaryMD5 string `json:"binary_md5"`
	Signature string `json:"signature_verdict"`
}

// fetchAfterRename is a TEST SEAM: it runs between step 9's rename and step
// 10's verdict link (D8b's kill point). A no-op in production.
var fetchAfterRename = func() {}

// FetchRelease materializes and verifies a local release archive and writes
// its verdict (see the file comment for the exact order).
func FetchRelease(cfg FetchConfig) (updaterjob.Verdict, error) {
	// 1. the verdict comes first: nothing else is looked at for an id that has one
	vpath, err := updaterjob.VerdictPath(cfg.DataDir, cfg.ReleaseID)
	if err != nil {
		return updaterjob.Verdict{}, fmt.Errorf("%w: %w", ErrFetchConfig, err)
	}
	if _, err := os.Lstat(vpath); err == nil {
		return updaterjob.Verdict{}, fmt.Errorf("%w: %s (written once; remove it by hand to re-fetch)", ErrVerdictExists, vpath)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return updaterjob.Verdict{}, fmt.Errorf("%w: cannot tell whether %s exists: %w", ErrVerdictExists, vpath, err)
	}
	if err := checkRealDir(cfg.DataDir, false); err != nil {
		return updaterjob.Verdict{}, fmt.Errorf("%w: data dir: %w", ErrFetchConfig, err)
	}
	if !filepath.IsAbs(cfg.ReleaseRoot) {
		return updaterjob.Verdict{}, fmt.Errorf("%w: release root %q is not absolute", ErrFetchConfig, cfg.ReleaseRoot)
	}
	if err := checkRealDir(cfg.ReleaseRoot, true); err != nil {
		return updaterjob.Verdict{}, fmt.Errorf("%w: release root: %w", ErrFetchConfig, err)
	}
	if cfg.AllowedSigners == "" {
		return updaterjob.Verdict{}, fmt.Errorf("%w: no allowed-signers path", ErrFetchConfig)
	}
	if fi, err := os.Lstat(cfg.Archive); err != nil || !fi.Mode().IsRegular() {
		return updaterjob.Verdict{}, fmt.Errorf("%w: archive %q is not a regular file (%v)", ErrFetchConfig, cfg.Archive, err)
	}
	now := time.Now
	if cfg.Now != nil {
		now = cfg.Now
	}
	// D8b (CTO 1790279155144 (4)): hold the release root for the WHOLE fetch
	// (through the rename and the verdict link below), and first recover an
	// interrupted fetch: a release dir carrying this package's own pending
	// marker and no verdict is quarantined (renamed, never deleted), so a kill
	// between rename and link can no longer block its sha forever. A dir with
	// no marker is not ours and still refuses below ("never overwritten").
	unlock, err := LockReleaseRoot(cfg.ReleaseRoot)
	if err != nil {
		return updaterjob.Verdict{}, fmt.Errorf("%w: %w", ErrFetchConfig, err)
	}
	defer unlock()
	if _, err := quarantineLocked(cfg.ReleaseRoot, cfg.DataDir, now()); err != nil {
		return updaterjob.Verdict{}, fmt.Errorf("%w: interrupted-fetch recovery: %w", ErrFetchConfig, err)
	}

	// 2. extract into a private staging dir inside the release root
	staging, err := os.MkdirTemp(cfg.ReleaseRoot, stagingPrefix+cfg.ReleaseID+"-")
	if err != nil {
		return updaterjob.Verdict{}, fmt.Errorf("%w: staging dir: %w", ErrFetchConfig, err)
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := extractArchive(cfg.Archive, staging); err != nil {
		return updaterjob.Verdict{}, err
	}
	root, err := os.OpenRoot(staging)
	if err != nil {
		return updaterjob.Verdict{}, fmt.Errorf("%w: %w", ErrArchive, err)
	}
	defer root.Close()

	// 3. the signature over the signed bytes
	mb, err := readRegular(root, signedManifestName, MaxManifestBytes)
	if err != nil {
		return updaterjob.Verdict{}, fmt.Errorf("%w: %w", ErrManifest, err)
	}
	sb, err := readRegular(root, signedSigName, MaxSignatureBytes)
	if err != nil {
		return updaterjob.Verdict{}, fmt.Errorf("%w: %w", ErrManifest, err)
	}
	sv, err := VerifySSHSIG(mb, sb, cfg.AllowedSigners)
	if err != nil {
		return updaterjob.Verdict{}, err
	}

	// 4. the manifest
	m, err := parseSignedManifest(mb)
	if err != nil {
		return updaterjob.Verdict{}, err
	}
	if m.ReleaseID != cfg.ReleaseID {
		return updaterjob.Verdict{}, fmt.Errorf("%w: the signed manifest is release %q, not the %q asked for", ErrManifest, m.ReleaseID, cfg.ReleaseID)
	}
	final := filepath.Join(cfg.ReleaseRoot, m.SourceSHA)
	if _, err := os.Lstat(final); !errors.Is(err, fs.ErrNotExist) {
		return updaterjob.Verdict{}, fmt.Errorf("%w: %s (it may be the running release; never overwritten) (lstat: %v)", ErrReleaseDirExists, final, err)
	}

	// 5. every artifact, and nothing else (the signed pair alone is exempt)
	if err := rehashTree(root, m.Artifacts, map[string]bool{signedManifestName: true, signedSigName: true}); err != nil {
		return updaterjob.Verdict{}, err
	}

	// 6. the layout the activation needs
	if err := checkArchiveLayout(root, m); err != nil {
		return updaterjob.Verdict{}, err
	}

	// 7. materialize activation's layout
	if err := materialize(root, m, sv); err != nil {
		return updaterjob.Verdict{}, err
	}
	if err := syncDir(staging); err != nil {
		return updaterjob.Verdict{}, fmt.Errorf("%w: %w", ErrArchive, err)
	}

	// 8. the verdict this fetch will write, and the job's own re-proofs of it
	// over what was just materialized (still the staging dir)
	v := updaterjob.Verdict{
		Schema:            updaterjob.VerdictSchema,
		ReleaseID:         m.ReleaseID,
		SourceSHA:         m.SourceSHA,
		ReleaseDir:        final,
		Signer:            sv.Principal,
		SignerFingerprint: sv.Fingerprint,
		HashAlg:           sv.HashAlg,
		ManifestSHA256:    m.SHA256,
		Artifacts:         len(m.Artifacts),
		VerifiedAt:        now().UTC().Format(time.RFC3339Nano),
	}
	if err := v.Check(); err != nil {
		return updaterjob.Verdict{}, fmt.Errorf("%w: %w", ErrVerdictWrite, err)
	}
	if _, err := rehashDir(staging, v); err != nil {
		return updaterjob.Verdict{}, err
	}
	if _, _, err := reverifyDir(staging, cfg.AllowedSigners, v); err != nil {
		return updaterjob.Verdict{}, err
	}

	// 9. into place
	if _, err := os.Lstat(final); !errors.Is(err, fs.ErrNotExist) {
		return updaterjob.Verdict{}, fmt.Errorf("%w: %s appeared during the fetch", ErrReleaseDirExists, final)
	}
	if err := MarkFetchPending(cfg.ReleaseRoot, m.SourceSHA); err != nil {
		return updaterjob.Verdict{}, fmt.Errorf("%w: pending marker: %w", ErrFetchConfig, err)
	}
	if err := os.Rename(staging, final); err != nil {
		_ = ClearFetchPending(cfg.ReleaseRoot, m.SourceSHA)
		return updaterjob.Verdict{}, fmt.Errorf("%w: %s: %w", ErrReleaseDirExists, final, err)
	}
	renamed = true
	_ = syncDir(cfg.ReleaseRoot)
	fetchAfterRename()

	// 10. the verdict — last, and only now. A verdict-link dir-fsync failure
	// is a WARNING, not a refusal (#206 review fold): the verdict's content
	// was fsynced before the link and the link itself is atomic, so both the
	// verdict and the release dir exist — refusing here would RemoveAll the
	// release dir and leave a published verdict naming nothing (the old
	// behaviour: operator told "refused", app-side gate reports verified,
	// every re-fetch refused with ErrVerdictExists until a hand delete).
	warn, err := writeVerdict(cfg.DataDir, vpath, v)
	if err != nil {
		// no release dir survives without its verdict
		_ = os.RemoveAll(final)
		_ = syncDir(cfg.ReleaseRoot)
		_ = ClearFetchPending(cfg.ReleaseRoot, m.SourceSHA)
		return updaterjob.Verdict{}, err
	}
	if warn != nil && cfg.Logf != nil {
		cfg.Logf("verdict %s: %v", vpath, warn)
	}
	// The verdict is written: a marker that survives this clear names a
	// finished fetch, and the next recovery removes it (a verdict names the dir).
	_ = ClearFetchPending(cfg.ReleaseRoot, m.SourceSHA)
	return v, nil
}

// RehashRelease is the job's `downloaded` re-proof of a verdict (brief §3.7
// Releases.Rehash) over the release dir it names: the verdict passes Check,
// the signed manifest is the one the verdict recorded (manifest_sha256) and
// says the verdict's release_id, source_sha and artifact count, every
// artifacts[] entry re-hashes, no file exists beyond artifacts[] and the four
// the materialization owns, RELEASE is a byte-identical copy of
// deploy/RELEASE naming source_sha, and the activation manifest names
// source_sha and the binary's current md5. Returns the number of artifacts
// re-hashed.
func RehashRelease(v updaterjob.Verdict) (int, error) {
	if err := v.Check(); err != nil {
		return 0, err
	}
	return rehashDir(v.ReleaseDir, v)
}

// rehashDir is RehashRelease over dir — FetchRelease's staging dir before
// the rename, v.ReleaseDir after it.
func rehashDir(releaseDir string, v updaterjob.Verdict) (int, error) {
	if !sha256Re.MatchString(v.ManifestSHA256) {
		return 0, fmt.Errorf("%w: no expected manifest_sha256 to re-hash against", ErrManifest)
	}
	root, err := openReleaseRoot(releaseDir)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	mb, err := readRegular(root, path.Join(signedDir, signedManifestName), MaxManifestBytes)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrManifest, err)
	}
	if got := sha256Hex(mb); got != v.ManifestSHA256 {
		return 0, fmt.Errorf("%w: signed/manifest.json hashes to %s, the verdict's manifest_sha256 is %s", ErrManifest, got, v.ManifestSHA256)
	}
	m, err := parseSignedManifest(mb)
	if err != nil {
		return 0, err
	}
	if err := manifestIsTheVerdicts(m, v); err != nil {
		return 0, err
	}
	exempt := map[string]bool{
		activationMfst: true, releaseMarker: true,
		path.Join(signedDir, signedManifestName): true, path.Join(signedDir, signedSigName): true,
	}
	if err := rehashTree(root, m.Artifacts, exempt); err != nil {
		return 0, err
	}
	if err := checkArchiveLayout(root, m); err != nil {
		return 0, err
	}
	rel, err := readRegular(root, releaseMarker, 4096)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrLayout, releaseMarker, err)
	}
	dep, _ := readRegular(root, archiveMarker, 4096)
	if !bytes.Equal(rel, dep) {
		return 0, fmt.Errorf("%w: %s (%q) is not a copy of %s (%q)", ErrLayout, releaseMarker, bytes.TrimSpace(rel), archiveMarker, bytes.TrimSpace(dep))
	}
	am, err := readActivationManifest(root)
	if err != nil {
		return 0, err
	}
	if am.SourceSHA != m.SourceSHA {
		return 0, fmt.Errorf("%w: manifest.json source_sha %q, the signed manifest's is %q", ErrLayout, am.SourceSHA, m.SourceSHA)
	}
	bin, err := binaryInRoot(root)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", ErrLayout, err)
	}
	sum, err := md5Of(root, bin)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrLayout, bin, err)
	}
	if am.BinaryMD5 != sum {
		return 0, fmt.Errorf("%w: manifest.json binary_md5 %q, %s hashes to %s", ErrLayout, am.BinaryMD5, bin, sum)
	}
	return len(m.Artifacts), nil
}

// ReverifyRelease is the job's `verified` re-proof of a verdict (brief §3.7
// Releases.Reverify): the verdict passes Check, the signed pair under signed/
// in the release dir it names verifies against the allowed-signers file NOW,
// the signed manifest IS the one the verdict recorded (manifest_sha256,
// release_id, source_sha) and was signed by the key it recorded
// (signer_fingerprint), and the activation manifest's signature_verdict is
// exactly this verification's verdict.
func ReverifyRelease(v updaterjob.Verdict, allowedSigners string) (SignedManifest, SignatureVerdict, error) {
	if err := v.Check(); err != nil {
		return SignedManifest{}, SignatureVerdict{}, err
	}
	return reverifyDir(v.ReleaseDir, allowedSigners, v)
}

// reverifyDir is ReverifyRelease over dir (see rehashDir).
func reverifyDir(releaseDir, allowedSigners string, v updaterjob.Verdict) (SignedManifest, SignatureVerdict, error) {
	root, err := openReleaseRoot(releaseDir)
	if err != nil {
		return SignedManifest{}, SignatureVerdict{}, err
	}
	defer root.Close()
	mb, err := readRegular(root, path.Join(signedDir, signedManifestName), MaxManifestBytes)
	if err != nil {
		return SignedManifest{}, SignatureVerdict{}, fmt.Errorf("%w: %w", ErrManifest, err)
	}
	sb, err := readRegular(root, path.Join(signedDir, signedSigName), MaxSignatureBytes)
	if err != nil {
		return SignedManifest{}, SignatureVerdict{}, fmt.Errorf("%w: %w", ErrManifest, err)
	}
	sv, err := VerifySSHSIG(mb, sb, allowedSigners)
	if err != nil {
		return SignedManifest{}, SignatureVerdict{}, err
	}
	m, err := parseSignedManifest(mb)
	if err != nil {
		return SignedManifest{}, SignatureVerdict{}, err
	}
	if err := manifestIsTheVerdicts(m, v); err != nil {
		return SignedManifest{}, SignatureVerdict{}, err
	}
	if sv.Fingerprint != v.SignerFingerprint {
		return SignedManifest{}, SignatureVerdict{}, fmt.Errorf("%w: signed by %s; the verdict records %s", ErrManifest, sv.Fingerprint, v.SignerFingerprint)
	}
	am, err := readActivationManifest(root)
	if err != nil {
		return SignedManifest{}, SignatureVerdict{}, err
	}
	if am.Signature != sv.String() || am.SourceSHA != m.SourceSHA {
		return SignedManifest{}, SignatureVerdict{}, fmt.Errorf("%w: manifest.json says %q for %q; the signature verifies as %q for %q",
			ErrLayout, am.Signature, am.SourceSHA, sv.String(), m.SourceSHA)
	}
	return m, sv, nil
}

// manifestIsTheVerdicts refuses a signed manifest that is not the one the
// verdict recorded: its bytes (manifest_sha256), release_id, source_sha and
// artifact count must all be the verdict's (verifier D6).
func manifestIsTheVerdicts(m SignedManifest, v updaterjob.Verdict) error {
	if m.SHA256 != v.ManifestSHA256 || m.ReleaseID != v.ReleaseID || m.SourceSHA != v.SourceSHA || len(m.Artifacts) != v.Artifacts {
		return fmt.Errorf("%w: the signed manifest (sha256 %s) is release %q, source_sha %s, %d artifacts; the verdict records %s, %q, %s, %d",
			ErrManifest, m.SHA256, m.ReleaseID, m.SourceSHA, len(m.Artifacts), v.ManifestSHA256, v.ReleaseID, v.SourceSHA, v.Artifacts)
	}
	return nil
}

// ── extraction ────────────────────────────────────────────────────────────────

// extractArchive unpacks a gzip'd tar into dir (a fresh, empty staging dir).
func extractArchive(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%w: %s is not gzip: %w", ErrArchive, archive, err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}
	defer root.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	var total int64
	for entries := 0; ; entries++ {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrArchive, err)
		}
		if entries >= MaxArchiveEntries {
			return fmt.Errorf("%w: more than %d entries", ErrArchive, MaxArchiveEntries)
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if hdr.Typeflag == tar.TypeDir {
			name = strings.TrimSuffix(name, "/")
		}
		if name == "" || name == "." {
			if hdr.Typeflag != tar.TypeDir {
				return fmt.Errorf("%w: %q: the archive root is not a directory", ErrUnsafeEntry, hdr.Name)
			}
			continue
		}
		if err := checkRelPath(name); err != nil {
			return fmt.Errorf("%w: %q: %v", ErrUnsafeEntry, hdr.Name, err)
		}
		if seen[name] {
			return fmt.Errorf("%w: %q: appears twice", ErrUnsafeEntry, hdr.Name)
		}
		seen[name] = true
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o755); err != nil {
				return fmt.Errorf("%w: %q: %w", ErrArchive, hdr.Name, err)
			}
		case tar.TypeReg:
			if hdr.Mode&0o7000 != 0 {
				return fmt.Errorf("%w: %q: setuid/setgid/sticky mode %#o", ErrUnsafeEntry, hdr.Name, hdr.Mode)
			}
			if hdr.Size < 0 || hdr.Size > MaxReleaseBytes-total {
				return fmt.Errorf("%w: %q: the release would exceed %d bytes", ErrArchive, hdr.Name, int64(MaxReleaseBytes))
			}
			total += hdr.Size
			if d := path.Dir(name); d != "." {
				if err := root.MkdirAll(d, 0o755); err != nil {
					return fmt.Errorf("%w: %q: %w", ErrArchive, hdr.Name, err)
				}
			}
			mode := fs.FileMode(0o644)
			if hdr.Mode&0o111 != 0 {
				mode = 0o755
			}
			if err := writeEntry(root, name, mode, tr, hdr.Size); err != nil {
				return fmt.Errorf("%w: %q: %w", ErrArchive, hdr.Name, err)
			}
		default:
			return fmt.Errorf("%w: %q: type %q is not a regular file or directory (symlink, hard link, device, fifo, …)", ErrUnsafeEntry, hdr.Name, string(rune(hdr.Typeflag)))
		}
	}
	// After the end-of-archive blocks only zero padding may follow. Draining
	// the stream to its end is also what makes gzip check its CRC and length.
	rest, err := io.ReadAll(io.LimitReader(gz, maxTrailingBytes+1))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}
	if len(rest) > maxTrailingBytes || bytes.IndexFunc(rest, func(r rune) bool { return r != 0 }) >= 0 {
		return fmt.Errorf("%w: data after the end of the tar archive", ErrArchive)
	}
	return nil
}

func writeEntry(root *os.Root, name string, mode fs.FileMode, r io.Reader, size int64) error {
	out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(r, size))
	if err == nil && n != size {
		err = fmt.Errorf("short entry: %d of %d bytes", n, size)
	}
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = root.Chmod(name, mode) // the umask narrowed the create mode
	}
	return err
}

// checkRelPath refuses anything but a canonical, local, slash-separated path —
// the same shapes check-archive-paths.sh refuses at packaging, and more.
func checkRelPath(p string) error {
	switch {
	case p == "":
		return errors.New("empty path")
	case len(p) > maxPathBytes:
		return fmt.Errorf("path longer than %d bytes", maxPathBytes)
	case strings.ContainsAny(p, "\x00\n\r\\"):
		return errors.New("NUL, newline or backslash in the path")
	case strings.HasPrefix(p, "/"):
		return errors.New("absolute path")
	case strings.HasPrefix(p, "~"):
		return errors.New("home-relative path")
	case p == ".." || strings.HasPrefix(p, "../") || strings.HasSuffix(p, "/..") || strings.Contains(p, "/../"):
		return errors.New("path escapes the root (..)")
	case path.Clean(p) != p:
		return errors.New("path is not canonical (., //, trailing /)")
	case !filepath.IsLocal(p):
		return errors.New("path is not local")
	}
	for _, e := range strings.Split(p, "/") {
		if len(e) > maxElementBytes {
			return fmt.Errorf("path element longer than %d bytes", maxElementBytes)
		}
	}
	return nil
}

// ── the manifest ──────────────────────────────────────────────────────────────

// parseSignedManifest reads the fields the worker needs from 3a's manifest.
// Absent and null are refused (an uncomputed list is not an empty one).
func parseSignedManifest(b []byte) (SignedManifest, error) {
	var raw struct {
		ReleaseID *string `json:"release_id"`
		SourceSHA *string `json:"source_sha"`
		Artifacts *[]struct {
			Path   *string `json:"path"`
			SHA256 *string `json:"sha256"`
			Bytes  *int64  `json:"bytes"`
		} `json:"artifacts"`
		Addon *struct {
			BuildID *string `json:"build_id"`
		} `json:"addon"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return SignedManifest{}, fmt.Errorf("%w: %w", ErrManifest, err)
	}
	if raw.ReleaseID == nil || !updaterwire.ValidReleaseID(*raw.ReleaseID) {
		return SignedManifest{}, fmt.Errorf("%w: release_id is absent or not a valid release id", ErrManifest)
	}
	if raw.SourceSHA == nil || !sha40Re.MatchString(*raw.SourceSHA) {
		return SignedManifest{}, fmt.Errorf("%w: source_sha is absent or not 40 lowercase hex", ErrManifest)
	}
	if raw.Artifacts == nil || len(*raw.Artifacts) == 0 {
		return SignedManifest{}, fmt.Errorf("%w: artifacts is absent, null or empty", ErrManifest)
	}
	m := SignedManifest{ReleaseID: *raw.ReleaseID, SourceSHA: *raw.SourceSHA, SHA256: sha256Hex(b)}
	if raw.Addon != nil && raw.Addon.BuildID != nil {
		m.AddonBuildID = *raw.Addon.BuildID
	}
	seen := map[string]bool{}
	for i, a := range *raw.Artifacts {
		if a.Path == nil || a.SHA256 == nil || a.Bytes == nil {
			return SignedManifest{}, fmt.Errorf("%w: artifacts[%d] lacks path, sha256 or bytes", ErrManifest, i)
		}
		p := *a.Path
		if err := checkRelPath(p); err != nil {
			return SignedManifest{}, fmt.Errorf("%w: artifacts[%d] %q: %v", ErrManifest, i, p, err)
		}
		if p == signedManifestName {
			return SignedManifest{}, fmt.Errorf("%w: artifacts[%d]: the manifest lists itself (%q, %d bytes) — "+
				"release.yml redirects manifest.sh INTO the stage, so the shell creates a 0-byte manifest.json before "+
				"find runs and the signed file can never match it (brief C8); refused until 3a writes the manifest outside the stage",
				ErrManifest, i, p, *a.Bytes)
		}
		if p == signedSigName || p == releaseMarker || p == signedDir || strings.HasPrefix(p, signedDir+"/") {
			return SignedManifest{}, fmt.Errorf("%w: artifacts[%d] %q is a name the materialized layout owns", ErrManifest, i, p)
		}
		if seen[p] {
			return SignedManifest{}, fmt.Errorf("%w: artifacts lists %q twice", ErrManifest, p)
		}
		seen[p] = true
		if !sha256Re.MatchString(*a.SHA256) || *a.Bytes < 0 {
			return SignedManifest{}, fmt.Errorf("%w: artifacts[%d] %q: sha256 must be 64 lowercase hex and bytes >= 0", ErrManifest, i, p)
		}
		m.Artifacts = append(m.Artifacts, Artifact{Path: p, SHA256: *a.SHA256, Bytes: *a.Bytes})
	}
	return m, nil
}

// rehashTree proves the tree under root is exactly artifacts ∪ exempt: every
// entry a directory or a regular file, every regular file listed (or exempt),
// every listed file present with its byte count and sha256.
func rehashTree(root *os.Root, artifacts []Artifact, exempt map[string]bool) error {
	listed := map[string]Artifact{}
	for _, a := range artifacts {
		listed[a.Path] = a
	}
	var extra, odd []string
	present := map[string]bool{}
	err := fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		switch t := d.Type(); {
		case t.IsDir():
			return nil
		case !t.IsRegular():
			odd = append(odd, p+" ("+t.String()+")")
		case exempt[p]:
		case listed[p] == (Artifact{}):
			extra = append(extra, p)
		default:
			present[p] = true
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%w: walk: %w", ErrArtifactMismatch, err)
	}
	var missing, changed []string
	for _, a := range artifacts {
		if !present[a.Path] {
			missing = append(missing, a.Path)
			continue
		}
		size, sum, err := hashFile(root, a.Path)
		if err != nil {
			changed = append(changed, a.Path+" (unreadable: "+err.Error()+")")
			continue
		}
		if size != a.Bytes || sum != a.SHA256 {
			changed = append(changed, fmt.Sprintf("%s (%d bytes sha256 %s; the manifest says %d bytes %s)", a.Path, size, sum, a.Bytes, a.SHA256))
		}
	}
	if len(odd)+len(extra)+len(missing)+len(changed) == 0 {
		return nil
	}
	var parts []string
	for _, g := range []struct {
		what string
		list []string
	}{{"not a regular file", odd}, {"extra (not in artifacts[])", extra}, {"missing", missing}, {"changed", changed}} {
		if len(g.list) > 0 {
			sort.Strings(g.list)
			if len(g.list) > 10 {
				g.list = append(g.list[:10], fmt.Sprintf("… %d more", len(g.list)-10))
			}
			parts = append(parts, g.what+": "+strings.Join(g.list, ", "))
		}
	}
	return fmt.Errorf("%w: %s", ErrArtifactMismatch, strings.Join(parts, "; "))
}

// binaryInRoot returns the staged release's ONE binary name: vl-bin when
// present, else vl-bin; BOTH present is refused (the dir must hold EXACTLY
// ONE — R5 removes the vl branch). // R5 removes.
func binaryInRoot(root *os.Root) (string, error) {
	vl, vlErr := root.Lstat(binaryNameVL)
	nfx, nfxErr := root.Lstat(binaryName)
	vlOK := vlErr == nil && vl.Mode().IsRegular()
	nfxOK := nfxErr == nil && nfx.Mode().IsRegular()
	switch {
	case vlOK && nfxOK:
		return "", fmt.Errorf("holds BOTH vl-bin and nofx-bin; a release dir must hold exactly one")
	case vlOK:
		return binaryNameVL, nil
	default:
		// Neither present keeps the old reading (vl-bin); the layout
		// check fails on the missing binary, as it always did.
		return binaryName, nil
	}
}

// checkArchiveLayout requires what activation needs from the archive half.
func checkArchiveLayout(root *os.Root, m SignedManifest) error {
	listed := map[string]bool{}
	for _, a := range m.Artifacts {
		listed[a.Path] = true
	}
	bin, err := binaryInRoot(root)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrLayout, err)
	}
	for _, need := range []string{bin, distIndex, archiveMarker} {
		if !listed[need] {
			return fmt.Errorf("%w: the manifest lists no %s", ErrLayout, need)
		}
	}
	// the manifest hashes contents, not modes: a binary nobody can execute
	// would pass every hash and fail only after the activation had killed the bot
	if fi, err := root.Lstat(bin); err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o100 == 0 {
		return fmt.Errorf("%w: %s is not an owner-executable regular file (%v, %v)", ErrLayout, bin, fi, err)
	}
	marker, err := readRegular(root, archiveMarker, 4096)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrLayout, archiveMarker, err)
	}
	if got := string(bytes.TrimSpace(marker)); got != m.SourceSHA {
		return fmt.Errorf("%w: %s names %q, the signed manifest's source_sha is %q", ErrLayout, archiveMarker, got, m.SourceSHA)
	}
	return nil
}

// materialize writes activation's layout inside the staging root.
func materialize(root *os.Root, m SignedManifest, sv SignatureVerdict) error {
	if err := root.Mkdir(signedDir, 0o755); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrLayout, signedDir, err)
	}
	for _, n := range []string{signedManifestName, signedSigName} {
		if err := root.Rename(n, path.Join(signedDir, n)); err != nil {
			return fmt.Errorf("%w: keep %s under %s: %w", ErrLayout, n, signedDir, err)
		}
	}
	marker, err := readRegular(root, archiveMarker, 4096)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrLayout, archiveMarker, err)
	}
	if err := writeEntry(root, releaseMarker, 0o644, bytes.NewReader(marker), int64(len(marker))); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrLayout, releaseMarker, err)
	}
	bin, err := binaryInRoot(root)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrLayout, err)
	}
	sum, err := md5Of(root, bin)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrLayout, bin, err)
	}
	am, err := json.Marshal(activationManifest{SourceSHA: m.SourceSHA, BinaryMD5: sum, Signature: sv.String()})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrLayout, err)
	}
	am = append(am, '\n')
	if err := writeEntry(root, activationMfst, 0o644, bytes.NewReader(am), int64(len(am))); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrLayout, activationMfst, err)
	}
	return nil
}

func readActivationManifest(root *os.Root) (activationManifest, error) {
	b, err := readRegular(root, activationMfst, 4096)
	if err != nil {
		return activationManifest{}, fmt.Errorf("%w: %s: %w", ErrLayout, activationMfst, err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var am activationManifest
	if err := dec.Decode(&am); err != nil {
		return activationManifest{}, fmt.Errorf("%w: %s: %w", ErrLayout, activationMfst, err)
	}
	return am, nil
}

// ── the verdict file ──────────────────────────────────────────────────────────

// writeVerdict creates the verdict no-clobber: <data>/updater and its
// verdicts/ are private dirs of this uid (created 0700 when absent, refused
// when loose), the bytes are fsynced in a temp file, and link(2) publishes
// them only if no verdict exists — a second writer gets ErrVerdictExists.
// verdictDirSync is a TEST SEAM, syncDir in production: the directory fsync
// that makes the verdict LINK durable.
var verdictDirSync = syncDir

func writeVerdict(dataDir, vpath string, v updaterjob.Verdict) (warn, err error) {
	// never write a verdict the app-side reader would refuse
	if err := v.Check(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrVerdictWrite, err)
	}
	updaterDir := filepath.Join(filepath.Clean(dataDir), updaterwire.UpdaterDirName)
	vdir := filepath.Dir(vpath)
	for _, d := range []string{updaterDir, vdir} {
		if err := os.Mkdir(d, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("%w: %s: %w", ErrVerdictWrite, d, err)
		}
		if err := updaterwire.CheckPrivateDir(d, os.Geteuid()); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrVerdictWrite, err)
		}
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrVerdictWrite, err)
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(vdir, ".verdict-*")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrVerdictWrite, err)
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(b)
	if err == nil {
		err = tmp.Chmod(0o600)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrVerdictWrite, err)
	}
	if err := os.Link(tmp.Name(), vpath); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("%w: %s", ErrVerdictExists, vpath)
		}
		return nil, fmt.Errorf("%w: %w", ErrVerdictWrite, err)
	}
	if err := verdictDirSync(vdir); err != nil {
		// The verdict IS linked; only the link's durability fsync failed.
		// Success with a warning (the caller keeps the release dir).
		return fmt.Errorf("linked but the verdicts dir fsync failed: %w", err), nil
	}
	return nil, nil
}

// ── small helpers ─────────────────────────────────────────────────────────────

// checkRealDir: dir is a real directory (not a symlink); with ownerOnly, it
// is also owned by this uid and not writable by group or other (the release
// root holds binaries that will be executed).
func checkRealDir(dir string, ownerOnly bool) error {
	if dir == "" || !filepath.IsAbs(dir) {
		return fmt.Errorf("%q is not an absolute path", dir)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory (%s)", dir, fi.Mode().Type())
	}
	if !ownerOnly {
		return nil
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is writable by group or other (%#o)", dir, fi.Mode().Perm())
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("%s is not owned by this uid", dir)
	}
	return nil
}

func openReleaseRoot(releaseDir string) (*os.Root, error) {
	if err := checkRealDir(releaseDir, false); err != nil {
		return nil, fmt.Errorf("%w: release dir: %w", ErrLayout, err)
	}
	root, err := os.OpenRoot(releaseDir)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLayout, err)
	}
	return root, nil
}

// readRegular reads a regular file (never through a symlink) of at most max bytes.
func readRegular(root *os.Root, name string, max int64) ([]byte, error) {
	fi, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file (%s)", name, fi.Mode().Type())
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s is larger than %d bytes", name, max)
	}
	return b, nil
}

func hashFile(root *os.Root, name string) (int64, string, error) {
	f, err := root.Open(name)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func md5Of(root *os.Root, name string) (string, error) {
	f, err := root.Open(name)
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

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
