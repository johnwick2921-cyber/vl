package updaterworker

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"vl/internal/installpath"
	"vl/internal/updaterjob"
)

// reproofInstall is a temp installation (its .env names no DB_PATH) whose
// deploy/release_allowed_signers is r's, with a release fetched by the REAL
// FetchRelease into a release root beside it. The Target is the production
// resolver's (ResolveTarget), so the verdict lands in the data dir the
// adapter reads.
func reproofInstall(t *testing.T, r testRelease) (Target, Verdict) {
	t.Helper()
	t.Setenv("DB_PATH", "x")
	os.Unsetenv("DB_PATH")
	base := t.TempDir()
	inst := filepath.Join(base, "install")
	writeFile(t, filepath.Join(inst, "data", "data.db"), "SQLite format 3\x00")
	signers, err := os.ReadFile(r.signers)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ReleaseAllowedSignersPath(inst), string(signers))
	tg, err := ResolveTarget(inst)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "releases")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	fv, err := FetchRelease(FetchConfig{Archive: r.archive, ReleaseID: testReleaseID, ReleaseRoot: root,
		AllowedSigners: ReleaseAllowedSignersPath(inst), DataDir: tg.DataDir, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatalf("fixture fetch: %v", err)
	}
	setReleaseRoot(t, root) // the operator's NOFX_RELEASE_DIR is the root it was fetched into
	return tg, mirrorVerdict(fv)
}

// setReleaseRoot sets NOFX_RELEASE_DIR ("" unsets it) and resets the one
// release-dir resolver (a sync.Once in production).
func setReleaseRoot(t *testing.T, root string) {
	t.Helper()
	t.Setenv("NOFX_RELEASE_DIR", root)
	installpath.ResetReleaseDirForTest()
	t.Cleanup(installpath.ResetReleaseDirForTest)
}

// PIN (U4N item A): the production Reverifier — the constructor
// cmd/vl-updater's newReverifier calls — re-proves a release the REAL fetch
// verified: Verdict is updaterjob.ReadVerdict's file, field for field; Rehash
// re-hashes every artifact; Reverify re-verifies the SSHSIG against the
// INSTALL's deploy/release_allowed_signers and returns the signed manifest's
// facts (release, source, manifest sha256, signer, addon build, artifacts).
func TestReleaseReverifierReprovesAFetchedRelease(t *testing.T) {
	r := buildRelease(t, releaseOpts{})
	tg, want := reproofInstall(t, r)
	rv, err := NewReleaseReverifier(tg)
	if err != nil {
		t.Fatalf("NewReleaseReverifier: %v", err)
	}
	v, err := rv.Verdict(testReleaseID)
	if err != nil || v != want {
		t.Fatalf("Verdict = %+v, %v\nwant the verdict file %+v", v, err, want)
	}
	if n, err := rv.Rehash(v); err != nil || n != v.Artifacts || n == 0 {
		t.Fatalf("Rehash = %d, %v; want %d artifacts", n, err, v.Artifacts)
	}
	f, err := rv.Reverify(v)
	if err != nil {
		t.Fatalf("Reverify: %v", err)
	}
	idx, err := os.ReadFile(filepath.Join(v.ReleaseDir, "web", "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	isum := sha256.Sum256(idx)
	if f.ReleaseID != testReleaseID || f.SourceSHA != testSHA || f.ManifestSHA256 != v.ManifestSHA256 ||
		f.SignerFingerprint != r.fp || f.AddonBuildID != testBuildID || len(f.Artifacts) != v.Artifacts ||
		f.Artifacts["web/dist/index.html"] != hex.EncodeToString(isum[:]) || f.Artifacts["ninjascript/VLTraderTCPClient.cs"] == "" {
		t.Fatalf("Reverify facts = %+v\nwant release %s source %s manifest %s signer %s build %s, %d artifacts incl. index.html %x",
			f, testReleaseID, testSHA, v.ManifestSHA256, r.fp, testBuildID, v.Artifacts, isum)
	}
}

// PIN (U4N item A, C7): the adapter refuses — every call, nothing cached —
// an absent verdict, an absent or foreign trust anchor in the INSTALL tree,
// and a verdict it is handed that the release does not prove (it re-proves
// the verdict it is GIVEN, never one it re-reads).
func TestReleaseReverifierRefuses(t *testing.T) {
	r := buildRelease(t, releaseOpts{})
	foreign := newTestSigner(t, t.TempDir(), "foreign")
	for name, c := range map[string]struct {
		tamper func(t *testing.T, tg Target, v *Verdict)
		call   func(rv Reverifier, v Verdict) error
		want   error
	}{
		"no verdict for the release id": {nil, func(rv Reverifier, v Verdict) error {
			_, err := rv.Verdict("v9.9.9-none")
			return err
		}, fs.ErrNotExist},
		"the install has no allowed-signers file": {func(t *testing.T, tg Target, v *Verdict) {
			if err := os.Remove(ReleaseAllowedSignersPath(tg.InstallDir)); err != nil {
				t.Fatal(err)
			}
		}, reverify, ErrNoAllowedSigners},
		// U4F defect 3: the directory holding the trust anchor must be a
		// REAL directory — a symlinked <install>/deploy is never followed,
		// even to a file that would verify (probe P4).
		"the install's deploy/ is a symlink to a copy of itself": {func(t *testing.T, tg Target, v *Verdict) {
			symlinkDeployElsewhere(t, tg.InstallDir)
		}, reverify, ErrAllowedSignersUnsafe},
		// U4F defect 6 (fail-closed default for the CTO): the verdict's
		// release dir must be <the CURRENT resolved release root>/<source
		// sha> — Verdict refuses otherwise, so no step resolves a release
		// from a root the operator no longer names (probe P7).
		"NOFX_RELEASE_DIR is unset": {func(t *testing.T, tg Target, v *Verdict) {
			setReleaseRoot(t, "")
		}, verdict, ErrReleaseRoot},
		"NOFX_RELEASE_DIR now names another root": {func(t *testing.T, tg Target, v *Verdict) {
			other := filepath.Join(t.TempDir(), "other-root")
			if err := os.Mkdir(other, 0o755); err != nil {
				t.Fatal(err)
			}
			setReleaseRoot(t, other)
		}, verdict, ErrReleaseRoot},
		"NOFX_RELEASE_DIR names the same root through a symlink": {func(t *testing.T, tg Target, v *Verdict) {
			link := filepath.Join(t.TempDir(), "rootlink")
			if err := os.Symlink(filepath.Dir(v.ReleaseDir), link); err != nil {
				t.Fatal(err)
			}
			setReleaseRoot(t, link)
		}, verdict, ErrReleaseRoot},
		"the install's allowed-signers names another key": {func(t *testing.T, tg Target, v *Verdict) {
			writeFile(t, ReleaseAllowedSignersPath(tg.InstallDir), "release "+foreign.pub+"\n")
		}, reverify, ErrSigForeignKey},
		"the verdict handed in records another manifest (reverify)": {func(t *testing.T, tg Target, v *Verdict) {
			v.ManifestSHA256 = strings.Repeat("b", 64)
		}, reverify, ErrManifest},
		"the verdict handed in records another signer": {func(t *testing.T, tg Target, v *Verdict) {
			v.SignerFingerprint = foreign.fingerprint(t)
		}, reverify, ErrManifest},
		"the verdict handed in records another manifest (rehash)": {func(t *testing.T, tg Target, v *Verdict) {
			v.ManifestSHA256 = strings.Repeat("b", 64)
		}, func(rv Reverifier, v Verdict) error { _, err := rv.Rehash(v); return err }, ErrManifest},
	} {
		t.Run(name, func(t *testing.T) {
			tg, v := reproofInstall(t, r)
			rv, err := NewReleaseReverifier(tg)
			if err != nil {
				t.Fatalf("NewReleaseReverifier: %v", err)
			}
			if c.tamper != nil {
				c.tamper(t, tg, &v)
			}
			if err := c.call(rv, v); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}

func reverify(rv Reverifier, v Verdict) error { _, err := rv.Reverify(v); return err }

func verdict(rv Reverifier, v Verdict) error { _, err := rv.Verdict(v.ReleaseID); return err }

// PIN (U4N item A): the worker's Verdict mirror IS the verdict file — the
// same fields, in the same order, of the same types, with the same json tags.
// A field updaterjob.Verdict gains or loses breaks the ONE mapping's struct
// conversion at compile time; a tag the conversion would silently ignore
// fails here.
func TestVerdictMirrorIsTheVerdictFile(t *testing.T) {
	mt, ft := reflect.TypeOf(Verdict{}), reflect.TypeOf(updaterjob.Verdict{})
	if mt.NumField() != ft.NumField() {
		t.Fatalf("the mirror carries %d fields, the verdict file %d", mt.NumField(), ft.NumField())
	}
	for i := 0; i < ft.NumField(); i++ {
		m, f := mt.Field(i), ft.Field(i)
		if m.Name != f.Name || m.Type != f.Type || m.Tag.Get("json") != f.Tag.Get("json") {
			t.Fatalf("field %d: mirror %s %s `json:%q`, verdict file %s %s `json:%q`", i, m.Name, m.Type, m.Tag.Get("json"), f.Name, f.Type, f.Tag.Get("json"))
		}
	}
	// the mapping round-trips a fully populated verdict both ways
	fv := updaterjob.Verdict{Schema: 1, ReleaseID: "v1", SourceSHA: testSHA, ReleaseDir: "/r/" + testSHA, Signer: "release",
		SignerFingerprint: "SHA256:x", HashAlg: "sha512", ManifestSHA256: strings.Repeat("a", 64), Artifacts: 3, VerifiedAt: "t"}
	if got := mirrorVerdict(fv).file(); got != fv {
		t.Fatalf("round trip = %+v, want %+v", got, fv)
	}
}

// symlinkDeployElsewhere moves <install>/deploy OUT of the install and leaves a
// symlink to it in its place: the file it points at is the same trust anchor,
// so only a refusal of the symlinked directory can fail the re-proof.
func symlinkDeployElsewhere(t *testing.T, installDir string) {
	t.Helper()
	deploy := filepath.Join(installDir, "deploy")
	elsewhere := filepath.Join(t.TempDir(), "deploy-elsewhere")
	if err := os.Rename(deploy, elsewhere); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, deploy); err != nil {
		t.Fatal(err)
	}
}
