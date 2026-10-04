package updateauth

import (
	"strconv"
	"strings"
	"testing"
)

// ── M3 census-repair verify #3 (N1/N2): directives the COMPILER reads ──────
//
// Rule 5 read comments for ONE compiler directive (//go:linkname). A cgo
// preamble is a comment too, and it reads files at COMPILE time: GNU as's
// .incbin, C23 #embed and #cgo LDFLAGS all pull a file into the binary with
// no Go string literal, no updateauth import, no crypto/hmac and no
// LoadDeviceKey. The verifier's N2 below built, vetted and minted a MAC the
// production VerifyMAC accepted with every census green. Each probe here is
// the verifier's file WORD FOR WORD, planted in a synthetic module (t.TempDir
// — never the real tree, and no key file exists anywhere: the census is
// syntactic) and judged by the PRODUCTION census function updateAuthOffenders,
// the one TestUpdateAuthCensus runs over the real tree.

// N2 — verifier probe zz_vcc3_incbin.go, verbatim.
const probeN2Incbin = `package main

// VERIFIER PROBE N2 (census repair; scratch copy only — never a worktree).
// A cgo preamble is a COMMENT: the census never reads it for anything. GNU
// as's .incbin pulls the key file into the binary at compile time (the C
// compiler runs in the package directory), with no Go string literal, no
// updateauth import, no crypto/hmac and no LoadDeviceKey. The module has no
// cgo today.

/*
__asm__(".section .rodata\n.global vcc3key\n.balign 8\nvcc3key:\n.incbin \"data/upd" "ater/dev" "ice.k" "ey\"\n.previous\n");
extern const unsigned char vcc3key[32];
*/
import "C"

import (
	"encoding/hex"
	"strconv"
	"unsafe"

	"github.com/golang-jwt/jwt/v5"
)

func vcc3Mint(releaseID, jobID string, expiresAt int64) (string, error) {
	k := C.GoBytes(unsafe.Pointer(&C.vcc3key[0]), 32)
	sig, err := jwt.SigningMethodHS256.Sign("vl-update-install/v1|"+releaseID+"|"+jobID+"|"+strconv.FormatInt(expiresAt, 10), k)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sig), nil
}
`

// PIN (verify #3 N2): `import "C"` is refused in ANY non-test file of the
// module (none exists today). No cgo at all is the fail-closed rule: .incbin
// takes ../ and absolute paths, so the preamble reaches the data dir from any
// package, and the preamble's C is not something this census can read.
func TestUpdateAuthCensusRefusesCgo(t *testing.T) {
	root := mintBase(t)
	const rel = "zz_vcc3_incbin.go"
	mintWrite(t, root, rel, probeN2Incbin)
	requirePrefixes(t, mintOffenders(t, root), rel+`: imports "C"`)

	// the same import in a non-root package, under an alias, and in a file a
	// build tag excludes (the walk ignores build tags: it can only over-report)
	for rel, body := range map[string]string{
		"kernel/zz_cgo.go":        "package kernel\n\n// #include <stdlib.h>\nimport \"C\"\n",
		"api/zz_cgo_alias.go":     "package api\n\nimport (\n\t\"fmt\"\n\tc \"C\"\n)\n\nvar _ = fmt.Sprint\nvar _ = c.int(0)\n",
		"trader/zz_cgo_tagged.go": "//go:build never\n\npackage trader\n\nimport \"C\"\n",
	} {
		t.Run(rel, func(t *testing.T) {
			root := mintBase(t)
			mintWrite(t, root, rel, body)
			requirePrefixes(t, mintOffenders(t, root), rel+`: imports "C"`)
		})
	}

	// controls: prose naming the import in a comment or a string, and cgo in a
	// _test.go file (not walked: a test file is never linked into the app
	// binary — and the toolchain refuses cgo in tests anyway), stay clean
	root = mintBase(t)
	mintWrite(t, root, "kernel/doc.go", "// Package kernel never writes import \"C\"; cgo would read files at compile time.\npackage kernel\n\nconst note = `import \"C\"`\n")
	mintWrite(t, root, "kernel/zz_cgo_test.go", "package kernel\n\n// #include <stdlib.h>\nimport \"C\"\n")
	if off := mintOffenders(t, root); len(off) != 0 {
		t.Fatalf("prose and a test file must stay clean:\n%s", strings.Join(off, "\n"))
	}
}

// N1a — verifier probe zz_vcc2_embed_literal.go, verbatim: the module-root
// package embeds the key by its literal path and mints through golang-jwt's
// HS256.
const probeN1aEmbedLiteral = `package main

// VERIFIER PROBE (census repair; scratch copy only — never a worktree).
// The census reads comments ONLY for //go:linkname (rule 5). //go:embed is the
// sibling directive: it reads a file under the root package's directory at
// COMPILE time. The data dir (data/updater/device.key) lives under the module
// root, so the root package main — the trading app — can embed the key with
// no string literal, no updateauth import, no crypto/hmac and no LoadDeviceKey,
// and mint through golang-jwt's HS256 (probe V2's primitive). The glob keeps
// even the directive from spelling "device.key".

import (
	_ "embed"
	"encoding/hex"
	"strconv"

	"github.com/golang-jwt/jwt/v5"
)

//go:embed data/updater/device.key
var vcc2EmbeddedKey []byte

func vcc2EmbedMint(releaseID, jobID string, expiresAt int64) (string, error) {
	sig, err := jwt.SigningMethodHS256.Sign("vl-update-install/v1|"+releaseID+"|"+jobID+"|"+strconv.FormatInt(expiresAt, 10), vcc2EmbeddedKey)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sig), nil
}
`

// N1b — verifier probe zz_vcc2_embed.go, verbatim: the same file with the
// glob data/upd*r/dev*, so the directive never spells the file name.
const probeN1bEmbedGlob = `package main

// VERIFIER PROBE (census repair; scratch copy only — never a worktree).
// The census reads comments ONLY for //go:linkname (rule 5). //go:embed is the
// sibling directive: it reads a file under the root package's directory at
// COMPILE time. The data dir (data/updater/device.key) lives under the module
// root, so the root package main — the trading app — can embed the key with
// no string literal, no updateauth import, no crypto/hmac and no LoadDeviceKey,
// and mint through golang-jwt's HS256 (probe V2's primitive). The glob keeps
// even the directive from spelling "device.key".

import (
	_ "embed"
	"encoding/hex"
	"strconv"

	"github.com/golang-jwt/jwt/v5"
)

//go:embed data/upd*r/dev*
var vcc2EmbeddedKey []byte

func vcc2EmbedMint(releaseID, jobID string, expiresAt int64) (string, error) {
	sig, err := jwt.SigningMethodHS256.Sign("vl-update-install/v1|"+releaseID+"|"+jobID+"|"+strconv.FormatInt(expiresAt, 10), vcc2EmbeddedKey)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sig), nil
}
`

// PIN (verify #3 N1a, N1b): no //go:embed in the module-root package. Its
// directory holds the default data dir (installpath.DefaultDBPath
// "data/data.db" → <root>/data/updater/device.key), and //go:embed reads a
// file under the package directory at COMPILE time — neither a literal nor a
// glob ever reaches a string the census folds. None exists today [A: go list
// EmbedPatterns over ./... names only vl/agent, vl/branding and
// vl/kernel]. Both probes also trip the pattern rule (the next pin), so a
// revert of either rule turns this pin red.
func TestUpdateAuthCensusRefusesRootPackageEmbed(t *testing.T) {
	for rel, c := range map[string]struct{ body, pattern string }{
		"zz_vcc2_embed_literal.go": {probeN1aEmbedLiteral, "data/updater/device.key"},
		"zz_vcc2_embed.go":         {probeN1bEmbedGlob, "data/upd*r/dev*"},
	} {
		t.Run(rel, func(t *testing.T) {
			root := mintBase(t)
			mintWrite(t, root, rel, c.body)
			requirePrefixes(t, mintOffenders(t, root),
				rel+": //go:embed in the module-root package",
				rel+": //go:embed pattern "+strconv.Quote(c.pattern)+" can match the enrollment")
		})
	}
}

// PIN (verify #3, the configurable data dir): the data dir is the directory
// of the DB path (main.go's os.Args[1] if given — it overrides the config —
// else DB_PATH from the process env, else <install>/.env, else data/data.db),
// anchored on the bot's WorkingDirectory — the checkout the deploy builds in
// (deploy/vl.service WorkingDirectory=__VL_DIR__). So DB_PATH=kernel/data.db
// puts device.key at <root>/kernel/updater/device.key, in reach of a
// //go:embed in package kernel [A read: installpath.DataDir, main.go:68-74].
// Fail-closed and cheap: in EVERY package, a //go:embed pattern with an
// element that can match "updater" (the directory, or anything under it) or
// "device.key" is refused, in any spelling the toolchain accepts (bare,
// "quoted", `raw`, all:). What it cannot see — a pattern naming an ANCESTOR
// of a data dir configured further below the package (DB_PATH=kernel/st/x.db
// with //go:embed st) — is named in WHAT THIS CANNOT PROVE.
func TestUpdateAuthCensusRefusesEmbedPatternsThatCanMatchTheEnrollment(t *testing.T) {
	const body = "package kernel\n\nimport _ \"embed\"\n\n//go:embed %s\nvar k []byte\n"
	for _, c := range []struct{ args, pattern string }{
		{"updater/device.key", "updater/device.key"},
		{"upd*r/dev*", "upd*r/dev*"},
		{"updater", "updater"},
		{"all:updater", "all:updater"},
		{"*", "*"},
		{`"u[p]dater/dev*"`, "u[p]dater/dev*"},
		{"`upd?ter`", "upd?ter"},
		{"session_calendar.json */device.key", "*/device.key"},
		{"*.key", "*.key"},
		// a case-insensitive filesystem (NTFS, APFS default) resolves a
		// literal pattern by Lstat, so the spelling's case must not matter
		// (census-repair verify #3 note 3)
		{"UPDATER/DEVICE.KEY", "UPDATER/DEVICE.KEY"},
		{"Updater", "Updater"},
	} {
		t.Run(c.args, func(t *testing.T) {
			root := mintBase(t)
			const rel = "kernel/zz_embed.go"
			mintWrite(t, root, rel, strings.Replace(body, "%s", c.args, 1))
			requirePrefixes(t, mintOffenders(t, root), rel+": //go:embed pattern "+strconv.Quote(c.pattern)+" can match the enrollment")
		})
	}
	// an argument list the toolchain cannot parse is refused, not skipped
	root := mintBase(t)
	mintWrite(t, root, "kernel/zz_embed.go", strings.Replace(body, "%s", `"data/updater`, 1))
	requirePrefixes(t, mintOffenders(t, root), "kernel/zz_embed.go: //go:embed arguments cannot be parsed")
}

// Controls for both embed rules: the real tree's embeds (kernel/, branding/,
// agent/ — literal files, a list, a *.json glob) in their own packages; prose
// naming go:embed in a comment of the root package (not a directive: the
// toolchain reads "//go:embed" + space or tab only); and the N1a directive in
// a root _test.go file. Ruling for test files: a _test.go file is never
// linked into the app binary, so it is outside the walk, exactly as for
// //go:linkname.
func TestUpdateAuthCensusAdmitsOrdinaryEmbeds(t *testing.T) {
	root := mintBase(t)
	mintWrite(t, root, "kernel/zz_embeds.go", "package kernel\n\nimport _ \"embed\"\n\n//go:embed session_calendar.json\nvar cal []byte\n\n"+
		"//go:embed testdata/futures_mnq_empty.golden testdata/futures_mnq_plan.golden\nvar golden []byte\n")
	mintWrite(t, root, "branding/branding.go", "package branding\n\nimport _ \"embed\"\n\n//go:embed product.txt\nvar product string\n")
	mintWrite(t, root, "agent/skill_registry.go", "package agent\n\nimport \"embed\"\n\n//go:embed skills/*.json\nvar skills embed.FS\n")
	mintWrite(t, root, "main.go", "// Package main never uses go:embed: //go:embedded is prose, and so is\n// the census rule that refuses a //go:embed directive here.\npackage main\n\n"+
		"//go:embedded assets live in kernel/ — not a directive: no space or tab follows \"//go:embed\".\nfunc main() {}\n")
	mintWrite(t, root, "zz_vcc2_embed_test.go", probeN1aEmbedLiteral)
	if off := mintOffenders(t, root); len(off) != 0 {
		t.Fatalf("ordinary embeds, prose and a test file must stay clean:\n%s", strings.Join(off, "\n"))
	}
}
