// Package releasefixture builds REAL release archives for tests: 3a's own
// deploy/release/package.sh stages the allow-list, deploy/release/manifest.sh
// writes the manifest, the REAL `ssh-keygen -Y sign -n release` signs it, and
// GNU `tar -C stage -czf` packs it — the way release.yml makes a release.
//
// TEST SUPPORT ONLY. It is the fixture internal/updaterworker's release tests
// (buildRelease, the SSHSIG signers) have always used, moved here unchanged so
// the cmd/vl-updater tests can drive `vl-updater fetch` end to end over
// the SAME archive (unit U4N). No production package imports it:
// cmd/vl-updater's TestTheUpdaterBinaryNeverLinksTheReleaseFixture pins
// that the binary's dependency graph does not contain it.
package releasefixture

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// BuildID is the VL_BUILD_ID the fixture's ninjascript source names (and so
// the signed manifest's addon.build_id).
const BuildID = "2026-09-24-u3"

// ZeroSelfEntry is the artifacts[] entry release.yml:172's redirect makes the
// manifest list for itself: the shell creates manifest.json (0 bytes, the
// sha256 of nothing) before find runs.
const ZeroSelfEntry = `{"path":"manifest.json","sha256":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","bytes":0}`

// SSHKeygen returns the real tool or skips the test with the reason.
func SSHKeygen(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen is absent on this box — the SSHSIG tests sign and cross-verify with the REAL tool (OpenSSH >= 8.1), never a Go re-implementation")
	}
	return p
}

// KeygenEnv keeps an ssh-agent and the user's ~/.ssh out of every run.
func KeygenEnv(t *testing.T) []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "LC_ALL=C"}
}

// RunKeygen runs ssh-keygen with args and fails the test on an error.
func RunKeygen(t *testing.T, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(SSHKeygen(t), args...)
	cmd.Env = KeygenEnv(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ssh-keygen %v: %v\n%s", args, err, out)
	}
	return out
}

// Signer is an ed25519 key pair made by ssh-keygen in a temp dir.
type Signer struct {
	Priv string // the private half's file (a temp dir; never the repo)
	Pub  string // "ssh-ed25519 AAAA… comment" — the .pub line
}

// NewSigner makes a key pair <dir>/<name>{,.pub}.
func NewSigner(t *testing.T, dir, name string) Signer {
	t.Helper()
	priv := filepath.Join(dir, name)
	RunKeygen(t, "-q", "-t", "ed25519", "-N", "", "-C", "nofx-test-"+name, "-f", priv)
	pub, err := os.ReadFile(priv + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	return Signer{Priv: priv, Pub: strings.TrimSpace(string(pub))}
}

// Fingerprint is `ssh-keygen -l -E sha256 -f <pub>`'s second field.
func (s Signer) Fingerprint(t *testing.T) string {
	t.Helper()
	f := strings.Fields(string(RunKeygen(t, "-l", "-E", "sha256", "-f", s.Priv+".pub")))
	if len(f) < 2 || !strings.HasPrefix(f[1], "SHA256:") {
		t.Fatalf("ssh-keygen -l printed %q", f)
	}
	return f[1]
}

// Sign signs the file at msgPath with `ssh-keygen -Y sign` and returns the
// armored signature it wrote to msgPath.sig.
func (s Signer) Sign(t *testing.T, msgPath, namespace string, extra ...string) []byte {
	t.Helper()
	_ = os.Remove(msgPath + ".sig")
	args := append([]string{"-Y", "sign", "-f", s.Priv, "-n", namespace}, extra...)
	RunKeygen(t, append(args, msgPath)...)
	sig, err := os.ReadFile(msgPath + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

// WriteAllowedSigners writes an allowed-signers file of lines into dir under a
// fresh name and returns its path.
func WriteAllowedSigners(t *testing.T, dir string, lines ...string) string {
	t.Helper()
	p := filepath.Join(dir, fmt.Sprintf("allowed_signers_%d", len(lines)))
	for i := 0; ; i++ {
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			break
		}
		p = filepath.Join(dir, fmt.Sprintf("allowed_signers_%d_%d", len(lines), i))
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// RepoRoot is the module root, found by walking up from the test's cwd to the
// directory holding go.mod and deploy/release/package.sh.
func RepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "deploy", "release", "package.sh")); err != nil {
				t.Fatalf("repo root %s has no deploy/release/package.sh: %v", dir, err)
			}
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			t.Fatal("no go.mod above the test's directory")
		}
		dir = up
	}
}

// WriteFiles writes rel → body under root (0644, dirs 0755).
func WriteFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// ReleaseSource is a repo tree with exactly what package.sh requires, plus a
// STALE deploy/RELEASE that package.sh must not ship. ReleaseSourceBinary
// names the binary (vl-bin or nofx-bin — R5 removes the nofx form).
func ReleaseSource(t *testing.T, withIndex bool) string {
	return ReleaseSourceBinary(t, withIndex, "nofx-bin")
}

func ReleaseSourceBinary(t *testing.T, withIndex bool, binary string) string {
	t.Helper()
	src := t.TempDir()
	files := map[string]string{
		binary:                                 "\x7fELF u3 stand-in binary\n",
		"LICENSE":                              "test licence\n",
		"ninjascript/vltrader_tcp_PROTOCOL.md": "protocol_version: 3\n",
		"ninjascript/VLTraderTCPClient.cs":           "public const string VL_BUILD_ID = \"" + BuildID + "\";\n",
		"web/dist/assets/app.js":               "console.log('u3')\n",
		"deploy/RELEASE":                       strings.Repeat("a", 40) + "\n",
	}
	if withIndex {
		files["web/dist/index.html"] = "<!doctype html><title>u3</title>\n"
	}
	WriteFiles(t, src, files)
	if err := os.Chmod(filepath.Join(src, binary), 0o755); err != nil {
		t.Fatal(err)
	}
	return src
}

// RunIn runs name args in dir with KeygenEnv and fails the test on an error.
func RunIn(t *testing.T, dir string, stdoutOnly bool, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = KeygenEnv(t)
	var out []byte
	var err error
	if stdoutOnly {
		out, err = cmd.Output()
	} else {
		out, err = cmd.CombinedOutput()
	}
	if err != nil {
		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		t.Fatalf("%s %v: %v\n%s%s", name, args, err, out, stderr)
	}
	return out
}

// Release is one built archive.
type Release struct {
	// RedirectRefused is set, and nothing else, when VerbatimRedirect was
	// asked and manifest.sh itself refused the redirect (its output).
	RedirectRefused string

	Archive  string // <work>/<release_id>.tar.gz
	Stage    string
	Signer   Signer
	Signers  string // allowed-signers: the signer under principal release
	FP       string
	Manifest []byte // the signed bytes
	Sig      []byte
}

// Opts vary the build (the zero value is release.yml's archive, except that
// the manifest is written OUTSIDE the stage — the C8 deviation routed to 3a).
type Opts struct {
	SelfEntry        bool // inject the 0-byte manifest.json self-entry release.yml:172's redirect produces
	VerbatimRedirect bool // release.yml:172 verbatim — manifest.sh redirected INTO the stage
	NoIndex          bool
	BeforeManifest   func(t *testing.T, stage string) // add to the stage before manifest.sh lists it
	EditManifest     func(b []byte) []byte            // rewrite the manifest BEFORE it is signed
	AfterSign        func(t *testing.T, stage string) // tamper after signing, before tar
}

// Build makes the archive <work>/<releaseID>.tar.gz of source sha.
func Build(t *testing.T, releaseID, sha string, o Opts) Release {
	t.Helper()
	SSHKeygen(t)
	root := RepoRoot(t)
	work := t.TempDir()
	stage := filepath.Join(work, "stage")
	RunIn(t, root, false, "bash", "deploy/release/package.sh", ReleaseSource(t, !o.NoIndex), stage, sha)
	manifestPath := filepath.Join(stage, "manifest.json")
	if o.BeforeManifest != nil {
		o.BeforeManifest(t, stage)
	}
	if o.VerbatimRedirect {
		cmd := exec.Command("bash", "-c", `bash deploy/release/manifest.sh "$1" "$2" "$3" > "$1/manifest.json"`, "_", stage, sha, releaseID)
		cmd.Dir = root
		cmd.Env = KeygenEnv(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			return Release{RedirectRefused: fmt.Sprintf("%v: %s", err, out)}
		}
	} else {
		out := RunIn(t, root, true, "bash", "deploy/release/manifest.sh", stage, sha, releaseID)
		if o.SelfEntry {
			out = InjectSelfEntry(t, out)
		}
		if o.EditManifest != nil {
			out = o.EditManifest(out)
		}
		if err := os.WriteFile(manifestPath, out, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	signer := NewSigner(t, work, "release-signer")
	sig := signer.Sign(t, manifestPath, "release")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if o.AfterSign != nil {
		o.AfterSign(t, stage)
	}
	archive := filepath.Join(work, releaseID+".tar.gz")
	RunIn(t, work, false, "tar", "-C", stage, "-czf", archive, ".")
	return Release{
		Archive: archive, Stage: stage, Signer: signer,
		Signers: WriteAllowedSigners(t, work, "release "+signer.Pub),
		FP:      signer.Fingerprint(t), Manifest: manifest, Sig: sig,
	}
}

// InjectSelfEntry puts ZeroSelfEntry first in manifest.sh's artifacts[].
func InjectSelfEntry(t *testing.T, manifest []byte) []byte {
	t.Helper()
	const open = `"artifacts": [`
	if bytes.Count(manifest, []byte(open)) != 1 {
		t.Fatalf("fixture: manifest.sh output has no single %q:\n%s", open, manifest)
	}
	return bytes.Replace(manifest, []byte(open), []byte(open+ZeroSelfEntry+","), 1)
}
