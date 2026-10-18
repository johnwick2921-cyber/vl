package main

// sshsig.go — B10.2: a self-contained PORT of the release signature check
// (internal/updaterworker/sshsig.go VerifySSHSIG) that compiles for Windows.
// The friend app cannot import that package: it transitively pulls Unix-only
// syscalls (syscall.Stat_t / Flock / Kill) that fail GOOS=windows. The app
// therefore embeds the friend-distribution PUBLIC key in the binary and
// verifies the signed manifest against those BYTES — no trust-anchor file, no
// uid/mode checks, nothing an attacker can rewrite on the friend's PC.
//
// The policy is byte-for-byte the same as sshsig.go:113 (stricter than
// ssh-keygen, never looser): armor BEGIN/END, magic "SSHSIG" v1 with nothing
// trailing, plain ssh-ed25519 key, namespace "release", empty reserved field,
// hash "sha512", the signing key byte-equal to an embedded key listed for
// principal "release" (exact principal only — never a pattern, any negation
// vetoes the line), then ed25519 over "SSHSIG" ‖ str(ns) ‖ str("") ‖
// str("sha512") ‖ str(SHA-512(message)) (PROTOCOL.sshsig).

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

const (
	friendSigPrincipal = "release" // allowed-signers principal (B10.2: signed -I release)
	friendSigNamespace = "release" // SSHSIG namespace (signed -n release)
	friendSigHashAlg   = "sha512"

	friendSSHSIGMagic   = "SSHSIG"
	friendSSHSIGVersion = 1
	friendSSHEd25519    = "ssh-ed25519"
	friendArmorBegin    = "-----BEGIN SSH SIGNATURE-----"
	friendArmorEnd      = "-----END SSH SIGNATURE-----"

	friendMaxSignersBytes = 64 << 10
	friendMaxSigBytes     = 64 << 10
)

var (
	errFriendNoKeys        = errors.New("sshsig: no embedded friend-distribution key")
	errFriendSignersBad    = errors.New("sshsig: embedded allowed-signers content is malformed")
	errFriendSigFormat     = errors.New("sshsig: signature is malformed")
	errFriendSigKeyType    = errors.New("sshsig: signing key is not a plain ssh-ed25519 key")
	errFriendSigNS         = errors.New("sshsig: wrong namespace")
	errFriendSigHashAlg    = errors.New("sshsig: hash algorithm is not sha512")
	errFriendSigPrincipal  = errors.New("sshsig: no embedded ssh-ed25519 key for principal release")
	errFriendSigForeignKey = errors.New("sshsig: signing key is not an embedded friend-distribution key")
	errFriendSigInvalid    = errors.New("sshsig: signature does not verify")
)

// verifySSHSIG verifies an armored SSHSIG signature over message against the
// EMBEDDED allowed-signers content (a single "release ssh-ed25519 <b64>"
// line). It returns the signer's OpenSSH fingerprint on success.
func verifySSHSIG(message, armored, allowedSigners []byte) (string, error) {
	keys, err := friendSignerKeys(allowedSigners)
	if err != nil {
		return "", err
	}
	blob, err := dearmorFriendSSHSIG(armored)
	if err != nil {
		return "", err
	}
	r := &friendSSHReader{b: blob}
	magic, ok := r.raw(len(friendSSHSIGMagic))
	if !ok || string(magic) != friendSSHSIGMagic {
		return "", fmt.Errorf("%w: magic is not %q", errFriendSigFormat, friendSSHSIGMagic)
	}
	version, ok := r.u32()
	if !ok || version != friendSSHSIGVersion {
		return "", fmt.Errorf("%w: version %d, want %d", errFriendSigFormat, version, friendSSHSIGVersion)
	}
	pubBlob, ok1 := r.str()
	namespace, ok2 := r.str()
	reserved, ok3 := r.str()
	hashAlg, ok4 := r.str()
	sigBlob, ok5 := r.str()
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
		return "", fmt.Errorf("%w: truncated signature object", errFriendSigFormat)
	}
	if len(r.b) != 0 {
		return "", fmt.Errorf("%w: %d byte(s) of trailing data", errFriendSigFormat, len(r.b))
	}
	pub, err := parseFriendEd25519Blob(pubBlob)
	if err != nil {
		return "", err
	}
	if string(namespace) != friendSigNamespace {
		return "", fmt.Errorf("%w: %q, want %q", errFriendSigNS, namespace, friendSigNamespace)
	}
	if len(reserved) != 0 {
		return "", fmt.Errorf("%w: reserved field is not empty", errFriendSigFormat)
	}
	if string(hashAlg) != friendSigHashAlg {
		return "", fmt.Errorf("%w: %q", errFriendSigHashAlg, hashAlg)
	}
	sr := &friendSSHReader{b: sigBlob}
	sigType, okT := sr.str()
	rawSig, okS := sr.str()
	if !okT || !okS || len(sr.b) != 0 || string(sigType) != friendSSHEd25519 || len(rawSig) != ed25519.SignatureSize {
		return "", fmt.Errorf("%w: signature is not one ssh-ed25519 signature", errFriendSigFormat)
	}
	if len(keys) == 0 {
		return "", errFriendSigPrincipal
	}
	listed := false
	for _, k := range keys {
		listed = listed || bytes.Equal(k, pubBlob)
	}
	if !listed {
		return "", fmt.Errorf("%w: %s is not an embedded friend-distribution key",
			errFriendSigForeignKey, friendKeyFingerprint(pubBlob))
	}
	digest := sha512.Sum512(message)
	var signed []byte
	signed = append(signed, friendSSHSIGMagic...)
	signed = appendFriendSSHString(signed, []byte(friendSigNamespace))
	signed = appendFriendSSHString(signed, nil)
	signed = appendFriendSSHString(signed, []byte(friendSigHashAlg))
	signed = appendFriendSSHString(signed, digest[:])
	if !ed25519.Verify(pub, signed, rawSig) {
		return "", errFriendSigInvalid
	}
	return friendKeyFingerprint(pubBlob), nil
}

// friendSignerKeys parses the embedded allowed-signers content (a byte copy of
// what an allowed_signers file would contain) and returns the key blob of every
// ssh-ed25519 key listed for the principal "release". Line parsing is identical
// to sshsig.go releaseSignerKeys, minus the trust-anchor file checks.
func friendSignerKeys(content []byte) ([][]byte, error) {
	if len(content) > friendMaxSignersBytes {
		return nil, fmt.Errorf("%w: larger than %d bytes", errFriendSignersBad, friendMaxSignersBytes)
	}
	var keys [][]byte
	for i, line := range strings.Split(string(content), "\n") {
		line = strings.TrimLeft(strings.TrimRight(line, " \t\r\n"), " \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		principals, rest, ok := friendKeygenPrincipalsToken(line)
		if !ok {
			return nil, fmt.Errorf("%w: line %d: unterminated quote in the principals field", errFriendSignersBad, i+1)
		}
		fields := strings.FieldsFunc(rest, friendSignersSep)
		forRelease, negated := false, false
		for _, p := range strings.Split(principals, ",") {
			forRelease = forRelease || p == friendSigPrincipal
			negated = negated || strings.HasPrefix(p, "!")
		}
		if !forRelease || negated {
			continue
		}
		if len(fields) < 2 {
			return nil, fmt.Errorf("%w: line %d: a release line needs <principals> <keytype> <base64>", errFriendSignersBad, i+1)
		}
		if !friendLooksLikeKeyType(fields[0]) {
			return nil, fmt.Errorf("%w: line %d: options on a release line are not supported (%q)", errFriendSignersBad, i+1, fields[0])
		}
		if fields[0] != friendSSHEd25519 {
			continue
		}
		blob, err := base64.StdEncoding.DecodeString(fields[1])
		if err != nil {
			return nil, fmt.Errorf("%w: line %d: key is not base64: %v", errFriendSignersBad, i+1, err)
		}
		if _, err := parseFriendEd25519Blob(blob); err != nil {
			return nil, fmt.Errorf("%w: line %d: %v", errFriendSignersBad, i+1, err)
		}
		keys = append(keys, blob)
	}
	return keys, nil
}

func friendSignersSep(r rune) bool { return r == ' ' || r == '\t' }

// friendKeygenPrincipalsToken is the same OpenSSH 9.6p1 strdelimw semantics as
// sshsig.go sshKeygenPrincipalsToken.
func friendKeygenPrincipalsToken(line string) (principals, rest string, ok bool) {
	idx := strings.IndexAny(line, " \t\"")
	if idx < 0 {
		return line, "", true
	}
	if line[idx] != '"' {
		return line[:idx], strings.TrimLeft(line[idx:], " \t"), true
	}
	body := line[idx+1:]
	closeIdx := strings.IndexByte(body, '"')
	if closeIdx < 0 {
		return "", "", false
	}
	principals = line[:idx] + body[:closeIdx]
	rest = strings.TrimLeft(body[closeIdx+1:], " \t")
	return principals, rest, true
}

func friendLooksLikeKeyType(s string) bool {
	for _, p := range []string{"ssh-", "ecdsa-", "sk-", "rsa-", "x509v3-"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// parseFriendEd25519Blob returns the 32-byte key of a plain ssh-ed25519 public
// key blob: string("ssh-ed25519") ‖ string(key), nothing after.
func parseFriendEd25519Blob(blob []byte) (ed25519.PublicKey, error) {
	r := &friendSSHReader{b: blob}
	kt, ok1 := r.str()
	k, ok2 := r.str()
	if !ok1 || !ok2 || len(r.b) != 0 {
		return nil, fmt.Errorf("%w: public key blob is malformed", errFriendSigFormat)
	}
	if string(kt) != friendSSHEd25519 {
		return nil, fmt.Errorf("%w: %q", errFriendSigKeyType, kt)
	}
	if len(k) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: ed25519 key is %d bytes", errFriendSigFormat, len(k))
	}
	return ed25519.PublicKey(k), nil
}

func friendKeyFingerprint(blob []byte) string {
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// dearmorFriendSSHSIG strips the armor ssh-keygen -Y sign writes.
func dearmorFriendSSHSIG(armored []byte) ([]byte, error) {
	if len(armored) > friendMaxSigBytes {
		return nil, fmt.Errorf("%w: larger than %d bytes", errFriendSigFormat, friendMaxSigBytes)
	}
	s := string(armored)
	if !strings.HasPrefix(s, friendArmorBegin+"\n") {
		return nil, fmt.Errorf("%w: does not begin with %q", errFriendSigFormat, friendArmorBegin)
	}
	s = s[len(friendArmorBegin)+1:]
	needle := "\n" + friendArmorEnd
	end := strings.Index(s, needle)
	if end < 0 {
		return nil, fmt.Errorf("%w: no %q line", errFriendSigFormat, friendArmorEnd)
	}
	marker := end + 1
	if strings.TrimSpace(s[marker+len(friendArmorEnd):]) != "" {
		return nil, fmt.Errorf("%w: text after %q", errFriendSigFormat, friendArmorEnd)
	}
	body := strings.ReplaceAll(s[:end], "\n", "")
	blob, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, fmt.Errorf("%w: armor body: %v", errFriendSigFormat, err)
	}
	return blob, nil
}

// friendSSHReader reads the SSH wire encoding (RFC 4251 §5).
type friendSSHReader struct{ b []byte }

func (r *friendSSHReader) raw(n int) ([]byte, bool) {
	if n < 0 || len(r.b) < n {
		return nil, false
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v, true
}

func (r *friendSSHReader) u32() (uint32, bool) {
	v, ok := r.raw(4)
	if !ok {
		return 0, false
	}
	return binary.BigEndian.Uint32(v), true
}

func (r *friendSSHReader) str() ([]byte, bool) {
	n, ok := r.u32()
	if !ok || uint64(n) > uint64(len(r.b)) {
		return nil, false
	}
	return r.raw(int(n))
}

func appendFriendSSHString(dst, v []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(v)))
	return append(dst, v...)
}
