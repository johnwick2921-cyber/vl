package updaterworker

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"vl/internal/updaterworker/releasefixture"
)

// ── SSHSIG, cross-checked against the REAL ssh-keygen ─────────────────────────
//
// Every signature here is made by `ssh-keygen -Y sign` (the tool release.yml
// signs with), and every case also asks `ssh-keygen -Y verify -I release -n
// release` (release.yml's own verification form) for its opinion, so a
// refusal is shown to be the one the test name claims and not a fixture that
// was broken some other way. Where our policy is deliberately STRICTER than
// the tool (sha256 hashalg, a wildcard principal), the test proves the tool
// accepts the fixture and we refuse it.

// The ssh-keygen helpers live in internal/updaterworker/releasefixture
// (moved there unchanged by U4N); these are their package-local names.

// sshKeygen returns the real tool or skips the test with the reason.
func sshKeygen(t *testing.T) string { t.Helper(); return releasefixture.SSHKeygen(t) }

// keygenEnv keeps an ssh-agent and the user's ~/.ssh out of every run.
func keygenEnv(t *testing.T) []string { return releasefixture.KeygenEnv(t) }

func runKeygen(t *testing.T, args ...string) []byte {
	t.Helper()
	return releasefixture.RunKeygen(t, args...)
}

type testSigner struct {
	priv string // the private half's file (a temp dir; never the repo)
	pub  string // "ssh-ed25519 AAAA… comment" — the .pub line
}

func (s testSigner) fixture() releasefixture.Signer {
	return releasefixture.Signer{Priv: s.priv, Pub: s.pub}
}

func newTestSigner(t *testing.T, dir, name string) testSigner {
	t.Helper()
	s := releasefixture.NewSigner(t, dir, name)
	return testSigner{priv: s.Priv, pub: s.Pub}
}

// fingerprint is `ssh-keygen -l -E sha256 -f <pub>`'s second field.
func (s testSigner) fingerprint(t *testing.T) string {
	t.Helper()
	return s.fixture().Fingerprint(t)
}

// sign signs the file at msgPath with `ssh-keygen -Y sign` and returns the
// armored signature it wrote to msgPath.sig.
func (s testSigner) sign(t *testing.T, msgPath, namespace string, extra ...string) []byte {
	t.Helper()
	return s.fixture().Sign(t, msgPath, namespace, extra...)
}

func writeAllowedSigners(t *testing.T, dir string, lines ...string) string {
	t.Helper()
	return releasefixture.WriteAllowedSigners(t, dir, lines...)
}

// keygenVerify is release.yml's verification, verbatim in form.
func keygenVerify(t *testing.T, signers string, sig, msg []byte) error {
	t.Helper()
	sigPath := filepath.Join(t.TempDir(), "m.sig")
	if err := os.WriteFile(sigPath, sig, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sshKeygen(t), "-Y", "verify", "-f", signers, "-I", "release", "-n", "release", "-s", sigPath)
	cmd.Stdin = bytes.NewReader(msg)
	cmd.Env = keygenEnv(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

type sigFixture struct {
	dir     string
	signer  testSigner
	msg     []byte
	msgPath string
	signers string // lists signer under principal "release" (README form, comment included)
}

func newSigFixture(t *testing.T) sigFixture {
	t.Helper()
	sshKeygen(t)
	dir := t.TempDir()
	s := newTestSigner(t, dir, "signer")
	msg := []byte(`{"release_id":"v0.0.1-test","source_sha":"` + strings.Repeat("ab", 20) + `","artifacts":[]}` + "\n")
	msgPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(msgPath, msg, 0o644); err != nil {
		t.Fatal(err)
	}
	return sigFixture{dir: dir, signer: s, msg: msg, msgPath: msgPath, signers: writeAllowedSigners(t, dir, "release "+s.pub)}
}

func TestSSHSIGVerifiesAnSshKeygenSignature(t *testing.T) {
	f := newSigFixture(t)
	sig := f.signer.sign(t, f.msgPath, "release")
	if err := keygenVerify(t, f.signers, sig, f.msg); err != nil {
		t.Fatalf("fixture: the real ssh-keygen refuses its own signature: %v", err)
	}
	fp := f.signer.fingerprint(t)
	// README form ("release <keytype> <base64> <comment>"), plus the shapes a
	// hand-kept file has: comments, blank lines, another principal's line,
	// and a principal LIST that contains release.
	other := newTestSigner(t, f.dir, "other")
	busy := writeAllowedSigners(t, f.dir,
		"# vl release trust anchor",
		"",
		"ci "+other.pub,
		"ci,release "+f.signer.pub,
	)
	for name, signers := range map[string]string{"readme form": f.signers, "busy file": busy} {
		t.Run(name, func(t *testing.T) {
			if err := keygenVerify(t, signers, sig, f.msg); err != nil {
				t.Fatalf("fixture: ssh-keygen refuses %s: %v", name, err)
			}
			v, err := VerifySSHSIG(f.msg, sig, signers)
			if err != nil {
				t.Fatalf("VerifySSHSIG refused a signature ssh-keygen accepts: %v", err)
			}
			if v.Fingerprint != fp {
				t.Fatalf("fingerprint = %q, ssh-keygen -l says %q", v.Fingerprint, fp)
			}
			if v.Principal != "release" || v.Namespace != "release" || v.HashAlg != "sha512" {
				t.Fatalf("verdict = %+v", v)
			}
			if got, want := v.String(), "sshsig:release:"+fp; got != want {
				t.Fatalf("signature_verdict = %q, want %q", got, want)
			}
		})
	}
}

// refuseBoth asserts ssh-keygen's opinion (toolAccepts) and that ours refuses
// with want.
func refuseBoth(t *testing.T, signers string, sig, msg []byte, toolAccepts bool, want error) {
	t.Helper()
	kerr := keygenVerify(t, signers, sig, msg)
	if toolAccepts && kerr != nil {
		t.Fatalf("fixture: ssh-keygen was expected to ACCEPT (our policy is the stricter one) but refused: %v", kerr)
	}
	if !toolAccepts && kerr == nil {
		t.Fatalf("fixture: ssh-keygen ACCEPTS this signature — the fixture is not broken the way the test claims")
	}
	v, err := VerifySSHSIG(msg, sig, signers)
	if err == nil {
		t.Fatalf("VerifySSHSIG ACCEPTED (verdict %q); want %v", v.String(), want)
	}
	if !errors.Is(err, want) {
		t.Fatalf("VerifySSHSIG refused with %v; want %v", err, want)
	}
}

func TestSSHSIGRefusesWrongNamespace(t *testing.T) {
	f := newSigFixture(t)
	sig := f.signer.sign(t, f.msgPath, "file") // a real key, the wrong namespace
	refuseBoth(t, f.signers, sig, f.msg, false, ErrSigNamespace)
}

func TestSSHSIGRefusesWrongPrincipal(t *testing.T) {
	f := newSigFixture(t)
	sig := f.signer.sign(t, f.msgPath, "release")
	t.Run("the key listed under another principal", func(t *testing.T) {
		refuseBoth(t, writeAllowedSigners(t, f.dir, "ci "+f.signer.pub), sig, f.msg, false, ErrSigPrincipal)
	})
	t.Run("a negated principal", func(t *testing.T) {
		refuseBoth(t, writeAllowedSigners(t, f.dir, "!release "+f.signer.pub), sig, f.msg, false, ErrSigPrincipal)
	})
	// A list that names release AND negates a pattern matching it: ssh-keygen
	// refuses (a matching negation wins); so must we — never looser (verifier D1).
	for _, list := range []string{"release,!release", "release,!*", "release,!rel*"} {
		t.Run("a list that names and negates release: "+list, func(t *testing.T) {
			refuseBoth(t, writeAllowedSigners(t, f.dir, list+" "+f.signer.pub), sig, f.msg, false, ErrSigPrincipal)
		})
	}
	// Any negation on the line admits nothing here, even one that cannot match
	// release (ssh-keygen honours "!foo,release"; we do not implement patterns).
	t.Run("a list with a negation ssh-keygen honours", func(t *testing.T) {
		refuseBoth(t, writeAllowedSigners(t, f.dir, "!foo,release "+f.signer.pub), sig, f.msg, true, ErrSigPrincipal)
	})
	// ssh-keygen matches principals as PATTERNS; we match the exact name only.
	t.Run("a wildcard principal ssh-keygen honours", func(t *testing.T) {
		refuseBoth(t, writeAllowedSigners(t, f.dir, "rel* "+f.signer.pub), sig, f.msg, true, ErrSigPrincipal)
	})
	// ssh-keygen separates the fields of an allowed-signers line with space,
	// tab, CR and LF; ours with SPACE and TAB only (CTO ruling 1790279155144
	// (1)); every other character — ASCII VT/FF, Unicode NBSP/NEL — is part of
	// the field it sits in, for both. A parser that
	// split on Unicode white space would admit each of these lines, which the
	// tool refuses: a divergence from the reference, never a looseness we keep.
	keyType, keyRest, ok := strings.Cut(f.signer.pub, " ")
	if !ok {
		t.Fatalf("fixture: .pub line %q has no space", f.signer.pub)
	}
	for _, tc := range []struct {
		name, line  string
		toolAccepts bool
		want        error
	}{
		// ssh-keygen (OpenSSH 9.6p1) refuses all six: "Could not verify
		// signature." (after "<file>:1: invalid key" for the keytype/key VT).
		{"a VT separator", "release\v" + f.signer.pub, false, ErrSigPrincipal},
		{"an FF separator", "release\f" + f.signer.pub, false, ErrSigPrincipal},
		{"a U+00A0 NBSP separator", "release\u00a0" + f.signer.pub, false, ErrSigPrincipal},
		{"a U+0085 NEL separator", "release\u0085" + f.signer.pub, false, ErrSigPrincipal},
		{"a leading VT", "\vrelease " + f.signer.pub, false, ErrSigPrincipal},
		// "ssh-ed25519\vAAAA…" is one field: a key type that is not
		// ssh-ed25519, so the line admits nothing.
		{"a VT between keytype and key", "release " + keyType + "\v" + keyRest, false, ErrSigPrincipal},
		// The one place the ruled tokenizer is STRICTER than the tool:
		// ssh-keygen also ends the principal field at a CR (its strdelimw set
		// is " \t\r\n"); ours does not, so the principal reads
		// "release\rssh-…" and admits nothing. Refusing what the tool accepts
		// is the permitted direction; accepting what it refuses is not.
		{"a CR separator ssh-keygen honours", "release\r" + f.signer.pub, true, ErrSigPrincipal},
		// A LEADING CR (verifier f13 defect 1): ssh-keygen skips only leading
		// space/tab, then ends the first field at the CR — an EMPTY principal
		// — and refuses (OpenSSH 9.6p1, rc 255 "Could not verify signature.").
		// Trimming CR from the FRONT of the line would admit it.
		{"a leading CR", "\rrelease " + f.signer.pub, false, ErrSigPrincipal},
		{"a leading CR then a space", "\r release " + f.signer.pub, false, ErrSigPrincipal},
		{"a leading space then a CR", " \rrelease " + f.signer.pub, false, ErrSigPrincipal},
	} {
		t.Run("a non space/tab separator: "+tc.name, func(t *testing.T) {
			refuseBoth(t, writeAllowedSigners(t, f.dir, tc.line), sig, f.msg, tc.toolAccepts, tc.want)
		})
	}
}

// PIN (#206 note sshsig.go:245): the trust anchor is accepted only when owned
// by this uid or root — a foreign uid can rewrite the anchor at will, so the
// mode check alone proves nothing about who controls the trusted keys. chown
// to another uid needs root, so the judgment is pinned directly and the
// WIRING through the signersAnchorUID seam (anchorUID in production).
func TestAllowedSignersAnchorMustBeOursOrRoot(t *testing.T) {
	euid := os.Geteuid()
	for _, tc := range []struct {
		uid  uint32
		good bool
	}{
		{uint32(euid), true},
		{0, true}, // a root-owned 0644 anchor is legitimate hardening
		{uint32(euid) + 1000, false},
		{12345, false},
	} {
		if why := anchorOwnerBad(tc.uid, euid); (why == "") != tc.good {
			t.Errorf("anchorOwnerBad(%d, %d) = %q, good=%v", tc.uid, euid, why, tc.good)
		}
	}
	f := newSigFixture(t)
	sig := f.signer.sign(t, f.msgPath, "release")
	prev := signersAnchorUID
	t.Cleanup(func() { signersAnchorUID = prev })
	signersAnchorUID = func(os.FileInfo) (uint32, bool) { return 54321, true }
	if _, err := VerifySSHSIG(f.msg, sig, f.signers); !errors.Is(err, ErrAllowedSignersUnsafe) || !strings.Contains(err.Error(), "uid 54321") {
		t.Fatalf("a foreign-owned anchor = %v, want ErrAllowedSignersUnsafe naming uid 54321", err)
	}
	signersAnchorUID = func(os.FileInfo) (uint32, bool) { return 0, true }
	if _, err := VerifySSHSIG(f.msg, sig, f.signers); err != nil {
		t.Fatalf("a root-owned anchor is legitimate hardening: %v", err)
	}
}

// acceptLikeTool asserts ssh-keygen ACCEPTS the fixture and so do we.
func acceptLikeTool(t *testing.T, signers string, sig, msg []byte) {
	t.Helper()
	if err := keygenVerify(t, signers, sig, msg); err != nil {
		t.Fatalf("fixture: ssh-keygen refuses: %v", err)
	}
	v, err := VerifySSHSIG(msg, sig, signers)
	if err != nil {
		t.Fatalf("VerifySSHSIG refused a signature ssh-keygen accepts: %v", err)
	}
	if v.Principal != "release" {
		t.Fatalf("verdict = %+v", v)
	}
}

// TestSSHSIGPrincipalQuotingMatchesSshKeygen is the P0 differential: the
// principal field of an allowed-signers line is tokenized the way OpenSSH
// 9.6p1 strdelimw tokenizes it (quotes drop, quoted content kept verbatim —
// commas inside quotes do NOT split the principal list), so we never trust a
// key ssh-keygen treats as part of a principal name. Every toolAccepts value
// was measured on this box's ssh-keygen (OpenSSH 9.6p1), 2026-09-25.
func TestSSHSIGPrincipalQuotingMatchesSshKeygen(t *testing.T) {
	f := newSigFixture(t)
	sig := f.signer.sign(t, f.msgPath, "release")
	for _, tc := range []struct {
		name        string
		line        string
		toolAccepts bool
		want        error // when toolAccepts is false: the error ours must give
	}{
		// ssh-keygen ACCEPTS all of these (strdelimw drops ONE quote pair and
		// the principal list splits on commas only outside quotes):
		{"a quoted principal", `"release" ` + f.signer.pub, true, nil},
		{"a quoted list naming release", `"release,evil" ` + f.signer.pub, true, nil},
		{"a quote that opens mid-field", `a,"release" ` + f.signer.pub, true, nil},
		{"a quote pair around the middle", `re"lease" ` + f.signer.pub, true, nil},
		{"a quoted suffix in the list", `release,"x,y" ` + f.signer.pub, true, nil},
		// ssh-keygen REFUSES all of these — and so must we:
		{"a quote pair before the comma", `"x,y"z,release ` + f.signer.pub, false, ErrSigPrincipal},
		{"a quote pair before a suffix", `"rel"ease ` + f.signer.pub, false, ErrSigPrincipal},
		{"a quoted suffix after release", `release"evil" ` + f.signer.pub, false, ErrSigPrincipal},
		{"an escaped principal", `rele\ase ` + f.signer.pub, false, ErrSigPrincipal},
		{"a quoted principal then an option fragment", `"release",evil ` + f.signer.pub, false, ErrAllowedSignersBad},
		{"a space inside the quotes", `"release evil" ` + f.signer.pub, false, ErrSigPrincipal},
		{"a quote pair before the list split", `"rel",ease ` + f.signer.pub, false, ErrSigPrincipal},
		{"an empty quoted principal", `"" ` + f.signer.pub, false, ErrSigPrincipal},
		{"quoted text then the keytype field", `"x,y"release ` + f.signer.pub, false, ErrSigPrincipal},
		{"an unterminated quote", `"release ` + f.signer.pub, false, ErrAllowedSignersBad},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signers := writeAllowedSigners(t, f.dir, tc.line)
			if tc.toolAccepts {
				acceptLikeTool(t, signers, sig, f.msg)
				return
			}
			refuseBoth(t, signers, sig, f.msg, false, tc.want)
		})
	}
}

func TestSSHSIGRefusesForeignKey(t *testing.T) {
	f := newSigFixture(t)
	foreign := newTestSigner(t, f.dir, "foreign")
	sig := foreign.sign(t, f.msgPath, "release") // right namespace, right hash, a key nobody listed
	refuseBoth(t, f.signers, sig, f.msg, false, ErrSigForeignKey)
}

func TestSSHSIGRefusesSha256Hashalg(t *testing.T) {
	f := newSigFixture(t)
	sig := f.signer.sign(t, f.msgPath, "release", "-O", "hashalg=sha256")
	refuseBoth(t, f.signers, sig, f.msg, true, ErrSigHashAlg)
}

func TestSSHSIGRefusesTamperedManifest(t *testing.T) {
	f := newSigFixture(t)
	sig := f.signer.sign(t, f.msgPath, "release")
	for name, msg := range map[string][]byte{
		"one byte changed":  bytes.Replace(f.msg, []byte("v0.0.1-test"), []byte("v0.0.2-test"), 1),
		"a byte appended":   append(append([]byte(nil), f.msg...), ' '),
		"the newline gone":  bytes.TrimSuffix(f.msg, []byte("\n")),
		"an empty manifest": {},
	} {
		t.Run(name, func(t *testing.T) {
			refuseBoth(t, f.signers, sig, msg, false, ErrSigInvalid)
		})
	}
}

// Absent ⇒ refuse (brief C6): no release installs until the owner commits
// deploy/release_allowed_signers.
func TestSSHSIGRefusesAnAbsentOrUnsafeAllowedSignersFile(t *testing.T) {
	f := newSigFixture(t)
	sig := f.signer.sign(t, f.msgPath, "release")
	absent := filepath.Join(f.dir, "deploy", "release_allowed_signers")
	if _, err := VerifySSHSIG(f.msg, sig, absent); !errors.Is(err, ErrNoAllowedSigners) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("absent allowed-signers: err = %v, want ErrNoAllowedSigners wrapping fs.ErrNotExist", err)
	}
	if got := ReleaseAllowedSignersPath("/srv/vl"); got != "/srv/vl/deploy/release_allowed_signers" {
		t.Fatalf("ReleaseAllowedSignersPath = %q", got)
	}
	link := filepath.Join(f.dir, "signers-link")
	if err := os.Symlink(f.signers, link); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifySSHSIG(f.msg, sig, link); !errors.Is(err, ErrAllowedSignersUnsafe) {
		t.Fatalf("symlinked allowed-signers: err = %v, want ErrAllowedSignersUnsafe", err)
	}
	loose := writeAllowedSigners(t, f.dir, "release "+f.signer.pub, "# loose")
	if err := os.Chmod(loose, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifySSHSIG(f.msg, sig, loose); !errors.Is(err, ErrAllowedSignersUnsafe) {
		t.Fatalf("group/world-writable allowed-signers: err = %v, want ErrAllowedSignersUnsafe", err)
	}
	// An option on the release line (namespaces=, valid-before=, cert-authority)
	// is a restriction ssh-keygen would enforce; one we do not implement is
	// refused rather than ignored.
	opt := writeAllowedSigners(t, f.dir, `release namespaces="release" `+f.signer.pub)
	if err := keygenVerify(t, opt, sig, f.msg); err != nil {
		t.Fatalf("fixture: ssh-keygen refuses the optioned line: %v", err)
	}
	if _, err := VerifySSHSIG(f.msg, sig, opt); !errors.Is(err, ErrAllowedSignersBad) {
		t.Fatalf("optioned release line: err = %v, want ErrAllowedSignersBad", err)
	}
	bad := writeAllowedSigners(t, f.dir, "release ssh-ed25519 not-base64!!", "#")
	if _, err := VerifySSHSIG(f.msg, sig, bad); !errors.Is(err, ErrAllowedSignersBad) {
		t.Fatalf("undecodable release key: err = %v, want ErrAllowedSignersBad", err)
	}
}

// verifyWithin runs VerifySSHSIG and fails the test if it has not returned
// within 10 s; unblock is called first so a stuck open can end.
func verifyWithin(t *testing.T, msg, sig []byte, signers string, unblock func()) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := VerifySSHSIG(msg, sig, signers)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		if unblock != nil {
			unblock()
		}
		<-done
		t.Fatalf("VerifySSHSIG blocked for 10 s opening the trust anchor %s", signers)
		return nil
	}
}

// unblockFifo opens a FIFO for writing (non-blocking: a reader is waiting)
// and closes it, which ends a reader stuck in open(2).
func unblockFifo(p string) func() {
	return func() {
		if w, err := os.OpenFile(p, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = w.Close()
		}
	}
}

// The trust anchor is judged and read as ONE file (verifier D7): a swap in
// the window between the checks and the read must not change which keys are
// trusted, nor hang the worker. The seam runs in that window; the file that
// was checked lists only the real signer, while every swapped-in file lists
// the FOREIGN key that signed the message.
func TestSSHSIGReadsTheTrustAnchorItChecked(t *testing.T) {
	f := newSigFixture(t)
	foreign := newTestSigner(t, f.dir, "foreign")
	sig := foreign.sign(t, f.msgPath, "release")
	t.Cleanup(func() { signersCheckedHook = nil })
	for name, c := range map[string]struct {
		swap func(t *testing.T, anchor string) (unblock func())
		want error
	}{
		"a regular file renamed over it": {func(t *testing.T, anchor string) func() {
			evil := writeAllowedSigners(t, t.TempDir(), "release "+foreign.pub)
			if err := os.Rename(evil, anchor); err != nil {
				t.Fatal(err)
			}
			return nil
		}, ErrSigForeignKey},
		"a symlink to a loose file renamed over it": {func(t *testing.T, anchor string) func() {
			d := t.TempDir()
			evil := writeAllowedSigners(t, d, "release "+foreign.pub)
			if err := os.Chmod(evil, 0o666); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(d, "link")
			if err := os.Symlink(evil, link); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(link, anchor); err != nil {
				t.Fatal(err)
			}
			return nil
		}, ErrSigForeignKey},
		"a FIFO renamed over it": {func(t *testing.T, anchor string) func() {
			fifo := filepath.Join(t.TempDir(), "fifo")
			if err := syscall.Mkfifo(fifo, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(fifo, anchor); err != nil {
				t.Fatal(err)
			}
			return unblockFifo(anchor)
		}, ErrSigForeignKey},
	} {
		t.Run(name, func(t *testing.T) {
			anchor := writeAllowedSigners(t, t.TempDir(), "release "+f.signer.pub)
			var unblock func()
			signersCheckedHook = func(p string) {
				if p == anchor {
					unblock = c.swap(t, anchor)
				}
			}
			err := verifyWithin(t, f.msg, sig, anchor, func() {
				if unblock != nil {
					unblock()
				}
			})
			signersCheckedHook = nil
			if err == nil {
				t.Fatalf("VerifySSHSIG ACCEPTED a foreign signature through a trust anchor swapped after its checks (%s)", name)
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("%s: err = %v, want %v (the keys of the file that was CHECKED)", name, err, c.want)
			}
		})
	}
	// control: the swapped-in regular file, read on its own, does admit the
	// foreign key — so the refusal above is the swap being ignored, not a fixture
	evil := writeAllowedSigners(t, t.TempDir(), "release "+foreign.pub)
	if _, err := VerifySSHSIG(f.msg, sig, evil); err != nil {
		t.Fatalf("control: the foreign allowed-signers file is refused on its own: %v", err)
	}
}

// A FIFO at the trust anchor's path is refused, and never blocks the open.
func TestSSHSIGRefusesAFifoTrustAnchorWithoutBlocking(t *testing.T) {
	f := newSigFixture(t)
	sig := f.signer.sign(t, f.msgPath, "release")
	fifo := filepath.Join(t.TempDir(), "release_allowed_signers")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyWithin(t, f.msg, sig, fifo, unblockFifo(fifo)); !errors.Is(err, ErrAllowedSignersUnsafe) {
		t.Fatalf("FIFO allowed-signers: err = %v, want ErrAllowedSignersUnsafe", err)
	}
}

// ── envelope mutations of a REAL signature ───────────────────────────────────

func sshString(b []byte) []byte {
	out := binary.BigEndian.AppendUint32(nil, uint32(len(b)))
	return append(out, b...)
}

type sigFields struct {
	magic     []byte
	version   uint32
	pub, ns   []byte
	reserved  []byte
	hash, sig []byte
	trailing  []byte
}

func decodeRealSig(t *testing.T, armored []byte) sigFields {
	t.Helper()
	s := strings.TrimSpace(string(armored))
	s = strings.TrimPrefix(s, "-----BEGIN SSH SIGNATURE-----")
	s = strings.TrimSuffix(s, "-----END SSH SIGNATURE-----")
	blob, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(s, "\n", ""))
	if err != nil {
		t.Fatal(err)
	}
	var f sigFields
	f.magic, blob = blob[:6], blob[6:]
	f.version, blob = binary.BigEndian.Uint32(blob), blob[4:]
	next := func() []byte {
		n := binary.BigEndian.Uint32(blob)
		v := blob[4 : 4+n]
		blob = blob[4+n:]
		return v
	}
	f.pub, f.ns, f.reserved, f.hash, f.sig = next(), next(), next(), next(), next()
	f.trailing = blob
	return f
}

func (f sigFields) armor() []byte {
	var blob []byte
	blob = append(blob, f.magic...)
	blob = binary.BigEndian.AppendUint32(blob, f.version)
	for _, v := range [][]byte{f.pub, f.ns, f.reserved, f.hash, f.sig} {
		blob = append(blob, sshString(v)...)
	}
	blob = append(blob, f.trailing...)
	b64 := base64.StdEncoding.EncodeToString(blob)
	var sb strings.Builder
	sb.WriteString("-----BEGIN SSH SIGNATURE-----\n")
	for len(b64) > 70 {
		sb.WriteString(b64[:70] + "\n")
		b64 = b64[70:]
	}
	sb.WriteString(b64 + "\n-----END SSH SIGNATURE-----\n")
	return []byte(sb.String())
}

func TestSSHSIGRefusesMalformedEnvelopes(t *testing.T) {
	f := newSigFixture(t)
	real := f.signer.sign(t, f.msgPath, "release")
	base := decodeRealSig(t, real)
	// control: the re-armoring is faithful, so each mutant differs ONLY by its mutation
	if _, err := VerifySSHSIG(f.msg, base.armor(), f.signers); err != nil {
		t.Fatalf("control: the faithfully re-armored real signature is refused: %v", err)
	}
	certType := sshString([]byte("ssh-ed25519-cert-v01@openssh.com"))
	for name, c := range map[string]struct {
		armored []byte
		want    error
	}{
		"bad magic":          {func() []byte { m := base; m.magic = []byte("SSHSIH"); return m.armor() }(), ErrSigFormat},
		"version 2":          {func() []byte { m := base; m.version = 2; return m.armor() }(), ErrSigFormat},
		"trailing data":      {func() []byte { m := base; m.trailing = []byte{0}; return m.armor() }(), ErrSigFormat},
		"non-empty reserved": {func() []byte { m := base; m.reserved = []byte("x"); return m.armor() }(), ErrSigFormat},
		"certificate key type": {func() []byte {
			m := base
			m.pub = append(certType, base.pub[len(sshString([]byte("ssh-ed25519"))):]...)
			return m.armor()
		}(), ErrSigKeyType},
		"no armor":          {[]byte(strings.SplitN(string(real), "\n", 2)[1]), ErrSigFormat},
		"text before armor": {append([]byte("x\n"), real...), ErrSigFormat},
		"no end line":       {[]byte(strings.Replace(string(real), "-----END SSH SIGNATURE-----", "", 1)), ErrSigFormat},
		// The END marker glued to the last base64 chars (#206 review fold):
		// ssh-keygen's dearmor wants "\n-----END …" and refuses with 'missing
		// footer'; the bare-marker search used to accept this shape.
		"END glued to the base64": {func() []byte {
			return []byte(strings.Replace(string(real), "\n"+armorEnd, armorEnd, 1))
		}(), ErrSigFormat},
		"text after armor": {append(append([]byte(nil), real...), []byte("junk\n")...), ErrSigFormat},
		"empty":            {nil, ErrSigFormat},
		"oversized":        {bytes.Repeat([]byte("A"), MaxSignatureBytes+1), ErrSigFormat},
	} {
		t.Run(name, func(t *testing.T) {
			v, err := VerifySSHSIG(f.msg, c.armored, f.signers)
			if err == nil {
				t.Fatalf("ACCEPTED a %s envelope (verdict %q)", name, v.String())
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("%s: err = %v, want %v", name, err, c.want)
			}
		})
	}
	// The differential for the glued-END case: the real ssh-keygen refuses
	// the same bytes ('missing footer'), so the refusal matches the tool.
	t.Run("the tool refuses the glued END", func(t *testing.T) {
		glued := []byte(strings.Replace(string(real), "\n"+armorEnd, armorEnd, 1))
		if err := keygenVerify(t, f.signers, glued, f.msg); err == nil {
			t.Fatal("fixture: ssh-keygen ACCEPTS the glued END — the case is not the refusal it claims")
		}
	})
}
