package updaterjob

// verdict.go — W-ONE-BUTTON M4 (3b-B, U3 repair D3): the release verdict file
// <dataDir>/updater/verdicts/<release_id>.json (brief §3.2), READ here.
//
// Why here: the worker's `fetch` (internal/updaterworker.FetchRelease) writes
// the verdict once, after every check has passed, but the API's install gate
// (U5b, brief §3.8: updaterjob.ReadVerdict(trader.MaintenanceDataDir(), id))
// must read it, and the trading app may not link vl/internal/updaterworker
// (store/maintenance_hold_writers_test.go, forbiddenWorkerPackages). This
// package is app-linkable and imports only the standard library and
// internal/updaterwire.
//
// READ-ONLY ON PURPOSE. Nothing in this file creates, links, renames, chmods
// or removes a file, and no OTHER file in this package names the verdict path
// (TestVerdictFileHasNoWriter pins both, over every non-test file). The ONE writer stays
// in the worker package, which the app cannot link: a writer here would let
// app code mint the evidence the install gate trusts. (Brief §3.1 listed a
// WriteVerdict here; it is deliberately kept worker-side — fail-closed.)

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"vl/internal/updaterwire"
)

const (
	// VerdictSchema is the verdict file's "schema". A reader refuses any other value.
	VerdictSchema = 1
	// VerdictSigner and VerdictHashAlg are the only values a verdict may carry
	// (the worker's SSHSIG policy: principal "release", hash sha512).
	VerdictSigner  = "release"
	VerdictHashAlg = "sha512"
	// MaxVerdictBytes caps a verdict file; a larger one is refused, never truncated.
	MaxVerdictBytes = 64 << 10

	verdictsDirName = "verdicts"
)

var (
	// ErrVerdictPath: the data dir or the release id cannot name a verdict.
	ErrVerdictPath = errors.New("updaterjob: verdict path refused")
	// ErrVerdict: the verdict file is absent, unsafe, unreadable or malformed.
	// An absent file also wraps fs.ErrNotExist.
	ErrVerdict = errors.New("updaterjob: verdict file is absent, unreadable or malformed")
)

var (
	verdictSHA40Re  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	verdictSHA256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// OpenSSH's SHA256 key fingerprint: "SHA256:" + unpadded base64 of 32 bytes.
	verdictFingerprintRe = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{42}[AEIMQUYcgkosw048]$`)
)

// Verdict is the verdict file (brief §3.2): exactly these keys.
type Verdict struct {
	Schema            int    `json:"schema"`
	ReleaseID         string `json:"release_id"`
	SourceSHA         string `json:"source_sha"`
	ReleaseDir        string `json:"release_dir"`
	Signer            string `json:"signer"`
	SignerFingerprint string `json:"signer_fingerprint"`
	HashAlg           string `json:"hashalg"`
	ManifestSHA256    string `json:"manifest_sha256"`
	Artifacts         int    `json:"artifacts"`
	VerifiedAt        string `json:"verified_at"` // RFC3339Nano, UTC
}

// VerdictPath is <dataDir>/updater/verdicts/<releaseID>.json. It touches no
// filesystem; it refuses a relative data dir and any id the wire refuses.
func VerdictPath(dataDir, releaseID string) (string, error) {
	if dataDir == "" || !filepath.IsAbs(dataDir) {
		return "", fmt.Errorf("%w: data dir %q is not absolute", ErrVerdictPath, dataDir)
	}
	if !updaterwire.ValidReleaseID(releaseID) {
		return "", fmt.Errorf("%w: release id %q is not a valid release id", ErrVerdictPath, releaseID)
	}
	return filepath.Join(filepath.Clean(dataDir), updaterwire.UpdaterDirName, verdictsDirName, releaseID+".json"), nil
}

// Check reports whether every field is computed and in range (never an empty
// stand-in). The worker checks a verdict before writing it; ReadVerdict
// checks it after reading. release_dir must be the clean absolute path of a
// directory NAMED for source_sha (<VL_RELEASE_DIR>/<source_sha>) — never
// "/", never another release's dir (verifier D5).
func (v Verdict) Check() error {
	if v.Schema != VerdictSchema || !updaterwire.ValidReleaseID(v.ReleaseID) || !verdictSHA40Re.MatchString(v.SourceSHA) ||
		v.Signer != VerdictSigner || !verdictFingerprintRe.MatchString(v.SignerFingerprint) ||
		v.HashAlg != VerdictHashAlg || !verdictSHA256Re.MatchString(v.ManifestSHA256) || v.Artifacts <= 0 {
		return fmt.Errorf("%w: a field is absent or out of range: %+v", ErrVerdict, v)
	}
	if !filepath.IsAbs(v.ReleaseDir) || filepath.Clean(v.ReleaseDir) != v.ReleaseDir || filepath.Base(v.ReleaseDir) != v.SourceSHA {
		return fmt.Errorf("%w: release_dir %q is not the clean absolute path of a directory named %s", ErrVerdict, v.ReleaseDir, v.SourceSHA)
	}
	if _, err := time.Parse(time.RFC3339Nano, v.VerifiedAt); err != nil {
		return fmt.Errorf("%w: verified_at: %w", ErrVerdict, err)
	}
	return nil
}

// ReadVerdict reads and validates the verdict for releaseID: a private
// regular file, exactly ONE JSON object with exactly the schema's keys, the id
// asked for, and every field computed. The file is opened once (O_NOFOLLOW:
// a symlink is refused; O_NONBLOCK: a FIFO cannot block) and judged on that
// descriptor. An absent file is ErrVerdict wrapping fs.ErrNotExist.
func ReadVerdict(dataDir, releaseID string) (Verdict, error) {
	p, err := VerdictPath(dataDir, releaseID)
	if err != nil {
		return Verdict{}, err
	}
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return Verdict{}, fmt.Errorf("%w: %w", ErrVerdict, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return Verdict{}, fmt.Errorf("%w: %w", ErrVerdict, err)
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 {
		return Verdict{}, fmt.Errorf("%w: %s is not a private regular file (%v)", ErrVerdict, p, fi.Mode())
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxVerdictBytes+1))
	if err != nil || len(b) > MaxVerdictBytes {
		return Verdict{}, fmt.Errorf("%w: %s unreadable or oversized (%v)", ErrVerdict, p, err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var v Verdict
	if err := dec.Decode(&v); err != nil {
		return Verdict{}, fmt.Errorf("%w: %s: %w", ErrVerdict, p, err)
	}
	// one object, then only whitespace (verifier D4)
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return Verdict{}, fmt.Errorf("%w: %s: data after the JSON object (%v)", ErrVerdict, p, err)
	}
	if v.ReleaseID != releaseID {
		return Verdict{}, fmt.Errorf("%w: %s names release %q", ErrVerdict, p, v.ReleaseID)
	}
	if err := v.Check(); err != nil {
		return Verdict{}, fmt.Errorf("%s: %w", p, err)
	}
	return v, nil
}
