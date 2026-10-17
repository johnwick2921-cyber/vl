package updaterjob

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The verdict reader at THIS package's API — the call U5b's API verifier
// makes (brief §3.8: updaterjob.ReadVerdict(trader.MaintenanceDataDir(), id)),
// in the package the trading app may link. The worker's FetchRelease (the
// one writer, internal/updaterworker) is pinned at its own call sites; here
// the file is planted exactly as that writer lays it down: <data>/updater and
// verdicts/ 0700, the file 0600, json.MarshalIndent(v, "", "  ") + "\n".

const vdSHA = "0123456789abcdef0123456789abcdef01234567"

// vdGood is a verdict the worker's writer would write (every field computed).
func vdGood() Verdict {
	sum := sha256.Sum256([]byte("release signing key"))
	return Verdict{
		Schema:            VerdictSchema,
		ReleaseID:         "v1.2.0",
		SourceSHA:         vdSHA,
		ReleaseDir:        "/srv/nofx-releases/" + vdSHA,
		Signer:            VerdictSigner,
		SignerFingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]),
		HashAlg:           VerdictHashAlg,
		ManifestSHA256:    strings.Repeat("ab", 32),
		Artifacts:         7,
		VerifiedAt:        "2026-09-24T18:02:11.123456789Z",
	}
}

// vdWorkerBytes is the worker writer's exact byte form.
func vdWorkerBytes(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

// vdPlant writes b as <dd>/updater/verdicts/<id>.json with the writer's dirs
// (0700) and the given file mode; it returns the path.
func vdPlant(t *testing.T, dd, id string, b []byte, mode os.FileMode) string {
	t.Helper()
	vdir := filepath.Join(dd, "updater", "verdicts")
	if err := os.MkdirAll(vdir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(vdir, id+".json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

// vdRefused asserts ReadVerdict refuses with ErrVerdict and returns nothing.
func vdRefused(t *testing.T, name, dd, id string) error {
	t.Helper()
	got, err := ReadVerdict(dd, id)
	if !errors.Is(err, ErrVerdict) {
		t.Errorf("%s: ReadVerdict = %+v, %v — want ErrVerdict", name, got, err)
	}
	if got != (Verdict{}) {
		t.Errorf("%s: a refused read returned a verdict: %+v", name, got)
	}
	return err
}

// TestVerifierReadsTheVerdictFile: the verifier's read of the file the worker
// wrote returns exactly that verdict; an absent file is ErrVerdict wrapping
// fs.ErrNotExist and the read creates nothing; a file naming another release
// is refused; VerdictPath is <data>/updater/verdicts/<id>.json for a valid id
// only and touches no filesystem.
func TestVerifierReadsTheVerdictFile(t *testing.T) {
	dd := t.TempDir()
	// absent — no updater dir at all, then no verdicts dir, then no file
	for _, pre := range []string{"", "updater", "updater/verdicts"} {
		if pre != "" {
			if err := os.MkdirAll(filepath.Join(dd, pre), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		before := tree(t, dd)
		got, err := ReadVerdict(dd, "v1.2.0")
		if !errors.Is(err, ErrVerdict) || !errors.Is(err, fs.ErrNotExist) || got != (Verdict{}) {
			t.Errorf("absent verdict (dirs %q): ReadVerdict = %+v, %v — want ErrVerdict wrapping fs.ErrNotExist", pre, got, err)
		}
		if after := tree(t, dd); !reflect.DeepEqual(before, after) {
			t.Errorf("a read of an absent verdict changed the tree: %v → %v", before, after)
		}
	}
	// present, in the writer's exact form
	want := vdGood()
	vdPlant(t, dd, want.ReleaseID, vdWorkerBytes(t, want), 0o600)
	if got, err := ReadVerdict(dd, want.ReleaseID); err != nil || got != want {
		t.Fatalf("ReadVerdict of the worker's verdict = %+v, %v; want %+v", got, err, want)
	}
	// only whitespace after the object is still one object (the writer ends
	// with "\n"; an editor's extra blank lines change nothing)
	ws := t.TempDir()
	vdPlant(t, ws, want.ReleaseID, append(vdWorkerBytes(t, want), "\n \t\r\n"...), 0o600)
	if got, err := ReadVerdict(ws, want.ReleaseID); err != nil || got != want {
		t.Errorf("trailing whitespace: ReadVerdict = %+v, %v; want the verdict", got, err)
	}
	// the file for v1.2.0 names another release
	other := want
	other.ReleaseID = "v1.2.1"
	od := t.TempDir()
	vdPlant(t, od, "v1.2.0", vdWorkerBytes(t, other), 0o600)
	vdRefused(t, "the file names another release", od, "v1.2.0")

	// VerdictPath
	if p, err := VerdictPath(dd, "v1.2.0"); err != nil || p != filepath.Join(dd, "updater", "verdicts", "v1.2.0.json") {
		t.Errorf("VerdictPath = %q, %v", p, err)
	}
	fresh := t.TempDir()
	before := tree(t, fresh)
	for _, c := range []struct{ dir, id string }{
		{"", "v1.2.0"}, {"data", "v1.2.0"}, {"./data", "v1.2.0"},
		{fresh, ""}, {fresh, ".."}, {fresh, "../v1.2.0"}, {fresh, "v1/../../x"}, {fresh, "v1.2.0/x"},
		{fresh, "/v1.2.0"}, {fresh, ".v1"}, {fresh, "-v1"}, {fresh, "v1..2"}, {fresh, "v1.2.0\x00"},
		{fresh, "v1 2"}, {fresh, "v1|2"}, {fresh, strings.Repeat("a", 65)},
	} {
		if p, err := VerdictPath(c.dir, c.id); !errors.Is(err, ErrVerdictPath) {
			t.Errorf("VerdictPath(%q, %q) = %q, %v — want ErrVerdictPath", c.dir, c.id, p, err)
		}
		if got, err := ReadVerdict(c.dir, c.id); !errors.Is(err, ErrVerdictPath) || got != (Verdict{}) {
			t.Errorf("ReadVerdict(%q, %q) = %+v, %v — want ErrVerdictPath", c.dir, c.id, got, err)
		}
	}
	if after := tree(t, fresh); !reflect.DeepEqual(before, after) {
		t.Errorf("refused paths changed the tree: %v → %v", before, after)
	}
}

// TestVerdictIsTheWorkersExactShape: the app-side type carries U3's verdict
// shape field for field — names, Go types, JSON tags, order — taken from
// ReleaseVerdict at f0e757fd (internal/updaterworker/release.go:134-148), so
// the verdict the worker writes is the verdict the app reads; and the file
// it writes is byte-for-byte this golden. A key the shape does not have is
// refused, and a key it has but the file lacks reads as absent (refused),
// never as a zero standing in for a value.
func TestVerdictIsTheWorkersExactShape(t *testing.T) {
	want := []string{
		"Schema int schema",
		"ReleaseID string release_id",
		"SourceSHA string source_sha",
		"ReleaseDir string release_dir",
		"Signer string signer",
		"SignerFingerprint string signer_fingerprint",
		"HashAlg string hashalg",
		"ManifestSHA256 string manifest_sha256",
		"Artifacts int artifacts",
		"VerifiedAt string verified_at",
	}
	rt := reflect.TypeOf(Verdict{})
	var got []string
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		got = append(got, f.Name+" "+f.Type.String()+" "+f.Tag.Get("json"))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Verdict fields =\n  %s\nwant U3's ReleaseVerdict shape\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	golden := `{
  "schema": 1,
  "release_id": "v1.2.0",
  "source_sha": "0123456789abcdef0123456789abcdef01234567",
  "release_dir": "/srv/nofx-releases/0123456789abcdef0123456789abcdef01234567",
  "signer": "release",
  "signer_fingerprint": "` + vdGood().SignerFingerprint + `",
  "hashalg": "sha512",
  "manifest_sha256": "` + strings.Repeat("ab", 32) + `",
  "artifacts": 7,
  "verified_at": "2026-09-24T18:02:11.123456789Z"
}
`
	if b := vdWorkerBytes(t, vdGood()); string(b) != golden {
		t.Errorf("the worker's verdict bytes =\n%s\nwant\n%s", b, golden)
	}
	// an extra key (a field another schema would add) is refused
	var m map[string]any
	if err := json.Unmarshal(vdWorkerBytes(t, vdGood()), &m); err != nil {
		t.Fatal(err)
	}
	for _, extra := range []string{"signature_verdict", "binary_md5", "grant"} {
		mm := map[string]any{}
		for k, v := range m {
			mm[k] = v
		}
		mm[extra] = "x"
		dd := t.TempDir()
		vdPlant(t, dd, "v1.2.0", vdWorkerBytes(t, mm), 0o600)
		vdRefused(t, "extra key "+extra, dd, "v1.2.0")
	}
	// every key absent in turn: refused (absent is never a zero value)
	for k := range m {
		mm := map[string]any{}
		for kk, v := range m {
			if kk != k {
				mm[kk] = v
			}
		}
		dd := t.TempDir()
		vdPlant(t, dd, "v1.2.0", vdWorkerBytes(t, mm), 0o600)
		vdRefused(t, "key "+k+" absent", dd, "v1.2.0")
	}
}

// TestReadVerdictTakesExactlyOneObject (U3 verifier defect 4): the verdict
// file is ONE JSON object and then only whitespace; anything after the
// object — garbage, a second object, a scalar, a stray bracket — is refused
// (the second Decode must be io.EOF), never ignored.
func TestReadVerdictTakesExactlyOneObject(t *testing.T) {
	obj := vdWorkerBytes(t, vdGood())
	for name, tail := range map[string]string{
		"trailing garbage":           " trailing garbage",
		"a second object":            string(obj),
		"a second, different object": `{"schema":1,"release_id":"v9.9.9"}`,
		"a trailing null":            "null",
		"a trailing number":          " 1",
		"a stray bracket":            "]",
		"a stray brace":              "}",
	} {
		dd := t.TempDir()
		vdPlant(t, dd, "v1.2.0", append(append([]byte(nil), obj...), tail...), 0o600)
		vdRefused(t, name, dd, "v1.2.0")
	}
}

// TestReadVerdictReleaseDirIsNamedForItsSHA (U3 verifier defect 5):
// release_dir is the clean absolute path of a directory NAMED source_sha
// (<NOFX_RELEASE_DIR>/<source_sha>, filepath.Base == source_sha) — never
// the filesystem root, never another release's dir, never an unclean or
// relative spelling of the right one.
func TestReadVerdictReleaseDirIsNamedForItsSHA(t *testing.T) {
	for name, dir := range map[string]string{
		"the filesystem root":               "/",
		"another release's dir":             "/srv/nofx-releases/89abcdef0123456789abcdef0123456789abcdef",
		"the release root itself":           "/srv/nofx-releases",
		"a dir named for the sha plus more": "/srv/nofx-releases/" + vdSHA + ".old",
		"a trailing slash (not clean)":      "/srv/nofx-releases/" + vdSHA + "/",
		"a dot-dot spelling (not clean)":    "/srv/other/../nofx-releases/" + vdSHA,
		"a doubled slash (not clean)":       "/srv//nofx-releases/" + vdSHA,
		"relative":                          "srv/nofx-releases/" + vdSHA,
		"empty":                             "",
		"the sha as a parent, not the leaf": "/srv/" + vdSHA + "/current",
	} {
		v := vdGood()
		v.ReleaseDir = dir
		dd := t.TempDir()
		vdPlant(t, dd, v.ReleaseID, vdWorkerBytes(t, v), 0o600)
		vdRefused(t, name, dd, v.ReleaseID)
		if err := v.Check(); !errors.Is(err, ErrVerdict) {
			t.Errorf("%s: Check = %v, want ErrVerdict (the writer must refuse it too)", name, err)
		}
	}
	// positive control: the release's own dir, anywhere absolute
	for _, dir := range []string{"/srv/nofx-releases/" + vdSHA, "/" + vdSHA} {
		v := vdGood()
		v.ReleaseDir = dir
		dd := t.TempDir()
		vdPlant(t, dd, v.ReleaseID, vdWorkerBytes(t, v), 0o600)
		if got, err := ReadVerdict(dd, v.ReleaseID); err != nil || got != v {
			t.Errorf("release_dir %q: ReadVerdict = %+v, %v", dir, got, err)
		}
	}
}

// TestReadVerdictRefusesAnyFieldOutOfRange: every field is computed and in
// range, or the verdict is refused — never an empty stand-in.
func TestReadVerdictRefusesAnyFieldOutOfRange(t *testing.T) {
	for name, edit := range map[string]func(v *Verdict){
		"schema 0":                        func(v *Verdict) { v.Schema = 0 },
		"schema 2":                        func(v *Verdict) { v.Schema = 2 },
		"source_sha upper case":           func(v *Verdict) { v.SourceSHA = strings.ToUpper(vdSHA); v.ReleaseDir = "/r/" + v.SourceSHA },
		"source_sha 39 hex":               func(v *Verdict) { v.SourceSHA = vdSHA[:39]; v.ReleaseDir = "/r/" + v.SourceSHA },
		"signer empty":                    func(v *Verdict) { v.Signer = "" },
		"signer another principal":        func(v *Verdict) { v.Signer = "root" },
		"fingerprint only the prefix":     func(v *Verdict) { v.SignerFingerprint = "SHA256:" },
		"fingerprint padded":              func(v *Verdict) { v.SignerFingerprint += "=" },
		"fingerprint MD5 form":            func(v *Verdict) { v.SignerFingerprint = "MD5:" + v.SignerFingerprint[len("SHA256:"):] },
		"fingerprint one char short":      func(v *Verdict) { v.SignerFingerprint = v.SignerFingerprint[:len(v.SignerFingerprint)-1] },
		"hashalg sha256":                  func(v *Verdict) { v.HashAlg = "sha256" },
		"manifest_sha256 63 hex":          func(v *Verdict) { v.ManifestSHA256 = v.ManifestSHA256[:63] },
		"manifest_sha256 upper case":      func(v *Verdict) { v.ManifestSHA256 = strings.ToUpper(v.ManifestSHA256) },
		"artifacts 0":                     func(v *Verdict) { v.Artifacts = 0 },
		"artifacts negative":              func(v *Verdict) { v.Artifacts = -1 },
		"verified_at empty":               func(v *Verdict) { v.VerifiedAt = "" },
		"verified_at not RFC3339":         func(v *Verdict) { v.VerifiedAt = "2026-09-24 18:02:11" },
		"release_id empty in the file":    func(v *Verdict) { v.ReleaseID = "" },
		"release_id re-cased in the file": func(v *Verdict) { v.ReleaseID = "V1.2.0" },
	} {
		v := vdGood()
		edit(&v)
		dd := t.TempDir()
		vdPlant(t, dd, "v1.2.0", vdWorkerBytes(t, v), 0o600)
		vdRefused(t, name, dd, "v1.2.0")
	}
	if err := vdGood().Check(); err != nil {
		t.Fatalf("control: Check(the good verdict) = %v", err)
	}
}

// TestReadVerdictRefusesUnsafeFiles: the verdict is a private regular file,
// read through no symlink, bounded in size; a FIFO or a directory at its
// path is refused at once (it never blocks the API).
func TestReadVerdictRefusesUnsafeFiles(t *testing.T) {
	good := vdWorkerBytes(t, vdGood())
	// group- or other-accessible
	for _, mode := range []os.FileMode{0o640, 0o604, 0o660, 0o644} {
		dd := t.TempDir()
		vdPlant(t, dd, "v1.2.0", good, mode)
		vdRefused(t, "mode "+mode.String(), dd, "v1.2.0")
	}
	// a symlink to a good, private verdict
	dd := t.TempDir()
	real := vdPlant(t, dd, "v1.2.1", good, 0o600) // a real file elsewhere in the dir
	link := filepath.Join(filepath.Dir(real), "v1.2.0.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := vdRefused(t, "a symlink", dd, "v1.2.0"); err != nil && !errors.Is(err, syscall.ELOOP) {
		t.Errorf("a symlink: refused by %v, want the no-follow open's ELOOP", err)
	}
	// a directory at the path
	dd = t.TempDir()
	if err := os.MkdirAll(filepath.Join(dd, "updater", "verdicts", "v1.2.0.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := vdRefused(t, "a directory", dd, "v1.2.0"); err != nil && !strings.Contains(err.Error(), "not a private regular file") {
		t.Errorf("a directory: refused by %v, want the regular-file rule", err)
	}
	// a FIFO: refused promptly by the regular-file rule, never a blocked read
	dd = t.TempDir()
	vdir := filepath.Join(dd, "updater", "verdicts")
	if err := os.MkdirAll(vdir, 0o700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(vdir, "v1.2.0.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := ReadVerdict(dd, "v1.2.0"); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrVerdict) || !strings.Contains(err.Error(), "not a private regular file") {
			t.Errorf("a FIFO: ReadVerdict = %v, want ErrVerdict by the regular-file rule", err)
		}
	case <-time.After(3 * time.Second):
		t.Errorf("a FIFO: ReadVerdict blocked for 3 s")
		// unblock the reader so the test binary can exit
		if w, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			w.Close()
		}
		<-done
	}
	// oversized: a good object padded with whitespace past MaxVerdictBytes
	// (so only the size cap can refuse it)
	dd = t.TempDir()
	big := append(append([]byte(nil), good...), strings.Repeat(" ", MaxVerdictBytes)...)
	vdPlant(t, dd, "v1.2.0", big, 0o600)
	vdRefused(t, "oversized", dd, "v1.2.0")
	// control: exactly at the cap reads
	dd = t.TempDir()
	atCap := append(append([]byte(nil), good...), strings.Repeat(" ", MaxVerdictBytes-len(good))...)
	vdPlant(t, dd, "v1.2.0", atCap, 0o600)
	if got, err := ReadVerdict(dd, "v1.2.0"); err != nil || got != vdGood() {
		t.Errorf("a verdict exactly MaxVerdictBytes long: ReadVerdict = %+v, %v", got, err)
	}
}
