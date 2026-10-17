package updateauth

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ── W-ONE-BUTTON M3 fold M4 (red-team 3 #1 + red-team 4 #2) ────────────────
//
// CTO ruling Q1(a): "nothing on the API side mints a MAC", and the M3 spec:
// "NO API creates, resets or reads" the enrollment. The census pinned only
// four names (Enroll, Authorize, ComputeMAC, LoadDeviceKey) and admitted any
// importer whose path began "internal/updater" — which covers the APP-linked
// internal/updaterwire. So, with the census green:
//   - red-team 3: an API file minted a MAC from the raw key file
//     (crypto/hmac + "device"+".key", no updateauth import at all), and a
//     worker file minted through the exported DeviceKeyPath + Message;
//   - red-team 4: the app-linked wire read device.key, deleted the seen store
//     and rewrote admin.json through DeviceKeyPath / SeenPath / AdminPath.
// Each probe below is the red team's file, planted in a synthetic module
// (t.TempDir — never the real tree) and judged by the PRODUCTION census
// function updateAuthOffenders. A clean module shaped like production is the
// positive control.

func mintWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mintBase is a synthetic module with production's legitimate shape: the
// literal home, the one MAC implementation, the API gate's admitted uses, the
// attended CLI's admitted uses, the data-dir resolver, and an exchange client
// that signs with crypto/hmac but never touches the updater dir.
func mintBase(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mintWrite(t, root, "go.mod", "module nofx\n\ngo 1.25\n")
	mintWrite(t, root, "internal/updateauth/paths.go", "package updateauth\n\nimport \"path/filepath\"\n\n"+
		"const (\n\tupdaterDirName = \"updater\"\n\tadminFileName = \"admin.json\"\n\tdeviceKeyName = \"device.key\"\n\tseenFileName = \"seen_job_ids.json\"\n)\n\n"+
		"func Dir(d string) string { return filepath.Join(d, updaterDirName) }\n"+
		"func AdminPath(d string) string { return filepath.Join(Dir(d), adminFileName) }\n"+
		"func DeviceKeyPath(d string) string { return filepath.Join(Dir(d), deviceKeyName) }\n"+
		"func SeenPath(d string) string { return filepath.Join(Dir(d), seenFileName) }\n")
	mintWrite(t, root, "internal/updateauth/mac.go", "package updateauth\n\nimport (\n\t\"crypto/hmac\"\n\t\"crypto/sha256\"\n)\n\n"+
		"func Message(r, j string, e int64) ([]byte, error) { return []byte(r + \"|\" + j), nil }\n"+
		"func ComputeMAC(k []byte, r, j string, e int64) (string, error) { m := hmac.New(sha256.New, k); _ = m; return \"\", nil }\n"+
		"func VerifyMAC(k []byte, r, j string, e int64, h string) bool { return hmac.Equal(nil, nil) }\n")
	mintWrite(t, root, "trader/maintenance_datadir.go", "package trader\n\nfunc MaintenanceDataDir() string { return \"/srv/nofx/data\" }\n")
	mintWrite(t, root, "api/handler_updates.go", "package api\n\nimport (\n\t\"nofx/internal/updateauth\"\n\t\"nofx/trader\"\n)\n\n"+
		"func gate(g updateauth.Grant) bool {\n\td := trader.MaintenanceDataDir()\n\tk, _ := updateauth.LoadDeviceKey(d)\n\t_, _ = updateauth.LoadAdmin(d)\n"+
		"\t_ = updateauth.CheckExpiry(g.ExpiresAt, nil)\n\t_ = updateauth.Consume(d, g.JobID, g.ExpiresAt, nil)\n\treturn updateauth.VerifyMAC(k, g.ReleaseID, g.JobID, g.ExpiresAt, g.HMAC)\n}\n")
	mintWrite(t, root, "api/server.go", "package api\n\nimport \"nofx/internal/updateauth\"\n\nvar v updateauth.Verifier = updateauth.StubVerifier{}\n")
	mintWrite(t, root, "internal/updaterbootstrap/bootstrap.go", "package updaterbootstrap\n\nimport \"nofx/internal/updateauth\"\n\n"+
		"func enroll(d string) { _ = updateauth.Enroll(d, \"u\", \"e\", nil, false); _ = updateauth.AdminPath(d); _ = updateauth.DeviceKeyPath(d); _ = updateauth.Dir(d) }\n"+
		"func authorize(d string) { _, _ = updateauth.Authorize(d, \"v1\", nil); _ = updateauth.ValidReleaseID(\"v1\"); _ = updateauth.MaxAuthorizationWindow }\n")
	mintWrite(t, root, "trader/okx/trader.go", "package okx\n\nimport (\n\t\"crypto/hmac\"\n\t\"crypto/sha256\"\n)\n\n"+
		"func sign(secret, msg string) []byte { m := hmac.New(sha256.New, []byte(secret)); m.Write([]byte(msg)); return m.Sum(nil) }\n")
	return root
}

func mintOffenders(t *testing.T, root string) []string {
	t.Helper()
	off, _, err := updateAuthOffenders(root)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(off)
	return off
}

// requirePrefixes fails unless, for every wanted prefix, some offender starts
// with it.
func requirePrefixes(t *testing.T, off []string, want ...string) {
	t.Helper()
	for _, w := range want {
		hit := false
		for _, o := range off {
			hit = hit || strings.HasPrefix(o, w)
		}
		if !hit {
			t.Fatalf("census offenders lack %q:\n%s", w, strings.Join(off, "\n"))
		}
	}
}

func TestUpdateAuthMintCensusCleanBaseline(t *testing.T) {
	if off := mintOffenders(t, mintBase(t)); len(off) != 0 {
		t.Fatalf("a module with production's legitimate shape must be clean:\n%s", strings.Join(off, "\n"))
	}
}

// PIN (ported RT3-1a, red-team 3's api/zz_redteam3_mint.go verbatim): an API
// file that never imports updateauth and never spells "device.key" as one
// literal mints a MAC from the raw key file.
func TestUpdateAuthCensusRefusesTheAPISideRawKeyMinter(t *testing.T) {
	root := mintBase(t)
	const rel = "api/zz_redteam3_mint.go"
	mintWrite(t, root, rel, `package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"nofx/trader"
)

func rt3MintFromTheAPI(releaseID, jobID string, expiresAt int64) (string, error) {
	key, err := os.ReadFile(filepath.Join(trader.MaintenanceDataDir(), "updater", "device"+".key"))
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	fmt.Fprintf(m, "%s|%s|%d", releaseID, jobID, expiresAt)
	return hex.EncodeToString(m.Sum(nil)), nil
}
`)
	requirePrefixes(t, mintOffenders(t, root),
		rel+": spells device.key",
		rel+": spells a fragment of device.key",
		rel+": imports crypto/hmac in package nofx/api, which references the updater data dir")
}

// PIN (ported RT3-1b, red-team 3's internal/updaterworker/zz_redteam3_mint.go
// verbatim): the CTO-refused option (c) — the worker minting the MAC itself
// through the exported DeviceKeyPath and Message.
func TestUpdateAuthCensusRefusesTheWorkerSideMinter(t *testing.T) {
	root := mintBase(t)
	const rel = "internal/updaterworker/zz_redteam3_mint.go"
	mintWrite(t, root, rel, `package updaterworker

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"

	"nofx/internal/updateauth"
)

func MintInstallMAC(dataDir, releaseID, jobID string, expiresAt int64) (string, error) {
	key, err := os.ReadFile(updateauth.DeviceKeyPath(dataDir))
	if err != nil {
		return "", err
	}
	msg, err := updateauth.Message(releaseID, jobID, expiresAt)
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	_, _ = m.Write(msg)
	return hex.EncodeToString(m.Sum(nil)), nil
}
`)
	requirePrefixes(t, mintOffenders(t, root),
		rel+": references updateauth.DeviceKeyPath",
		rel+": references updateauth.Message",
		rel+": imports crypto/hmac in package nofx/internal/updaterworker, which references the updater data dir")
}

// PIN (ported RT4-2a, red-team 4's internal/updaterwire/redteam_m3_red4_leak.go):
// the APP-linked wire package reads device.key, forgets every spent job id and
// rewrites admin.json through the exported path helpers.
func TestUpdateAuthCensusRefusesTheAppLinkedWireTouchingTheEnrollment(t *testing.T) {
	root := mintBase(t)
	const rel = "internal/updaterwire/redteam_m3_red4_leak.go"
	mintWrite(t, root, rel, `//go:build redteam_m3_red4

package updaterwire

import (
	"os"

	"nofx/internal/updateauth"
)

func RedTeamLeak(dataDir string) ([]byte, error) {
	key, err := os.ReadFile(updateauth.DeviceKeyPath(dataDir))
	_ = os.Remove(updateauth.SeenPath(dataDir))
	_ = os.WriteFile(updateauth.AdminPath(dataDir), []byte("{}\n"), 0o600)
	return key, err
}
`)
	requirePrefixes(t, mintOffenders(t, root),
		rel+": imports nofx/internal/updateauth",
		rel+": references updateauth.DeviceKeyPath",
		rel+": references updateauth.SeenPath",
		rel+": references updateauth.AdminPath")
}

// PIN (M4(c)): importer admission is EXACT. The worker package is admitted by
// its exact directory and gets no restricted identifier; a subpackage of it,
// the wire, a lookalike of the CLI's directory and a cmd/updater* binary are
// not admitted.
func TestUpdateAuthImporterAdmissionIsExact(t *testing.T) {
	const opener = "\n\nimport \"nofx/internal/updateauth\"\n\nvar _ = updateauth.ValidReleaseID\n"
	root := mintBase(t)
	mintWrite(t, root, "internal/updaterworker/job.go", "package updaterworker"+opener)
	if off := mintOffenders(t, root); len(off) != 0 {
		t.Fatalf("the worker package (exact dir) may import updateauth for its open identifiers:\n%s", strings.Join(off, "\n"))
	}
	for _, rel := range []string{
		"internal/updaterworker/sub/job.go",
		"internal/updaterwire/ids2.go",
		"internal/updaterbootstrapx/x.go",
		"internal/updaterbootstrap/extra.go",
		"cmd/updater-bootstrap/main.go",
		"cmd/updater-anything/main.go",
		"api/handler_updates_helper.go",
	} {
		t.Run(rel, func(t *testing.T) {
			root := mintBase(t)
			pkg := filepath.Base(filepath.Dir(rel))
			if strings.HasPrefix(rel, "cmd/") {
				pkg = "main"
			}
			mintWrite(t, root, rel, "package "+strings.ReplaceAll(pkg, "-", "")+opener)
			requirePrefixes(t, mintOffenders(t, root), rel+": imports nofx/internal/updateauth")
		})
	}
}

// PIN (M4(a)): the enrollment path helpers, Message and ComputeMAC are
// restricted per FILE even inside an admitted importer: the API gate may not
// resolve the key/admin/seen paths or build a MAC message, the CLI may not
// compute a MAC by hand, and an identifier nobody has classified is refused.
func TestUpdateAuthRestrictedIdentifiersInsideAdmittedFiles(t *testing.T) {
	for name, c := range map[string]struct{ rel, body, want string }{
		"API resolves the key path":   {"api/server.go", "package api\n\nimport \"nofx/internal/updateauth\"\n\nvar p = updateauth.DeviceKeyPath(\"/d\")\n", "api/server.go: references updateauth.DeviceKeyPath"},
		"API resolves the seen store": {"api/server.go", "package api\n\nimport \"nofx/internal/updateauth\"\n\nvar p = updateauth.SeenPath(\"/d\")\n", "api/server.go: references updateauth.SeenPath"},
		"API builds a MAC message":    {"api/server.go", "package api\n\nimport \"nofx/internal/updateauth\"\n\nvar m, _ = updateauth.Message(\"r\", \"j\", 1)\n", "api/server.go: references updateauth.Message"},
		"API enrolls":                 {"api/server.go", "package api\n\nimport \"nofx/internal/updateauth\"\n\nvar e = updateauth.Enroll\n", "api/server.go: references updateauth.Enroll"},
		"CLI computes a MAC":          {"internal/updaterbootstrap/bootstrap.go", "package updaterbootstrap\n\nimport \"nofx/internal/updateauth\"\n\nvar f = updateauth.ComputeMAC\n", "internal/updaterbootstrap/bootstrap.go: references updateauth.ComputeMAC"},
		"CLI loads the key":           {"internal/updaterbootstrap/bootstrap.go", "package updaterbootstrap\n\nimport \"nofx/internal/updateauth\"\n\nvar f = updateauth.LoadDeviceKey\n", "internal/updaterbootstrap/bootstrap.go: references updateauth.LoadDeviceKey"},
		"worker consumes a job id":    {"internal/updaterworker/job.go", "package updaterworker\n\nimport \"nofx/internal/updateauth\"\n\nvar f = updateauth.Consume\n", "internal/updaterworker/job.go: references updateauth.Consume"},
		"unclassified identifier":     {"api/server.go", "package api\n\nimport \"nofx/internal/updateauth\"\n\nvar f = updateauth.SomeNewHelper\n", "api/server.go: references unclassified updateauth.SomeNewHelper"},
		"aliased import":              {"api/server.go", "package api\n\nimport ua \"nofx/internal/updateauth\"\n\nvar p = ua.AdminPath(\"/d\")\n", "api/server.go: references updateauth.AdminPath"},
		// verifier D1 / probe V1: a SECOND import name hides the first one
		"second import name, worker mints (V1)": {"internal/updaterworker/mint.go", "package updaterworker\n\nimport (\n\t\"nofx/internal/updateauth\"\n\tua \"nofx/internal/updateauth\"\n)\n\nvar _ ua.Grant\n\nvar f = updateauth.ComputeMAC\n", "internal/updaterworker/mint.go: references updateauth.ComputeMAC"},
		"second import name, API gate":          {"api/handler_updates.go", "package api\n\nimport (\n\tua \"nofx/internal/updateauth\"\n\t\"nofx/internal/updateauth\"\n)\n\nvar _ updateauth.Grant\n\nvar p = ua.SeenPath(\"/d\")\n", "api/handler_updates.go: references updateauth.SeenPath"},
	} {
		t.Run(name, func(t *testing.T) {
			root := mintBase(t)
			mintWrite(t, root, c.rel, c.body)
			requirePrefixes(t, mintOffenders(t, root), c.want)
		})
	}
}

// PIN (M4(b)): the key file name spelled outside paths.go in any constant
// concatenation, in fragments a variable could join, and crypto/hmac in any
// package that reaches the updater data dir. Each case trips its rule in an
// otherwise-clean module; the exchange client (crypto/hmac, no updater
// reference) stays clean in every case.
func TestUpdateAuthCensusFoldsConcatenationAndFlagsMACPrimitives(t *testing.T) {
	for name, c := range map[string]struct{ rel, body, want string }{
		"split admin.json":           {"agent/x.go", "package agent\n\nvar p = \"/d/updater/ad\" + \"min.json\"\n", "agent/x.go: spells admin.json"},
		"const-folded seen store":    {"agent/x.go", "package agent\n\nconst a = \"seen_job\"\n\nvar p = a + \"_ids.json\"\n", "agent/x.go: spells seen_job_ids.json"},
		"run inside a variable join": {"agent/x.go", "package agent\n\nfunc p(d string) string { return d + \"/ad\" + \"min.json\" }\n", "agent/x.go: spells admin.json"},
		"device fragment":            {"agent/x.go", "package agent\n\nvar parts = []string{\"updater\", \"device\", \"key\"}\n", "agent/x.go: spells a fragment of device.key"},
		".key fragment":              {"agent/x.go", "package agent\n\nfunc p(d, n string) string { return d + \"/\" + n + \".key\" }\n", "agent/x.go: spells a fragment of device.key"},
		"hmac beside an updater dir": {"kernel/sig.go", "package kernel\n\nimport (\n\t\"crypto/hmac\"\n\t\"crypto/sha256\"\n)\n\nvar _ = hmac.New(sha256.New, nil)\n", "kernel/sig.go: imports crypto/hmac in package nofx/kernel, which references the updater data dir"},
		"hmac beside the wire const": {"agent/sig.go", "package agent\n\nimport (\n\t\"crypto/hmac\"\n\t\"crypto/sha256\"\n\n\t\"nofx/internal/updaterwire\"\n)\n\nvar _ = hmac.New(sha256.New, []byte(updaterwire.UpdaterDirName))\n", "agent/sig.go: imports crypto/hmac in package nofx/agent, which references the updater data dir"},
		"hmac in the wire":           {"internal/updaterwire/sig.go", "package updaterwire\n\nimport \"crypto/hmac\"\n\nvar _ = hmac.Equal\n", "internal/updaterwire/sig.go: imports crypto/hmac in package nofx/internal/updaterwire, which references the updater data dir"},
	} {
		t.Run(name, func(t *testing.T) {
			root := mintBase(t)
			if name == "hmac beside an updater dir" {
				// the updater-dir reference sits in ANOTHER file of the same package
				mintWrite(t, root, "kernel/paths.go", "package kernel\n\nimport \"path/filepath\"\n\nfunc dir(d string) string { return filepath.Join(d, \"updater\") }\n")
			}
			if name == "hmac in the wire" {
				mintWrite(t, root, "internal/updaterwire/paths.go", "package updaterwire\n\nconst UpdaterDirName = \"updater\"\n")
			}
			mintWrite(t, root, c.rel, c.body)
			off := mintOffenders(t, root)
			requirePrefixes(t, off, c.want)
			for _, o := range off {
				if strings.HasPrefix(o, "trader/okx/") {
					t.Fatalf("the exchange client (hmac, no updater reference) must stay clean: %s", o)
				}
			}
		})
	}
}

// PIN (M3 census repair, verifier D1 — probe V1 verbatim): the census tracked
// ONE import name per file, overwritten by each import of the package, so a
// second name (`ua "nofx/internal/updateauth"`) left every reference through
// the first unchecked. With the census green, the worker minted a MAC the
// production VerifyMAC accepts — red-team 3 #1(b), the CTO-refused option
// (c), back through one import line. Every import name now resolves, and a
// file that imports the package more than once is itself an offence.
func TestUpdateAuthCensusResolvesEveryImportName(t *testing.T) {
	root := mintBase(t)
	const rel = "internal/updaterworker/mint.go"
	mintWrite(t, root, rel, `package updaterworker

import (
	"os"

	"nofx/internal/updateauth"
	ua "nofx/internal/updateauth"
)

var _ ua.Grant

func Mint(dataDir, releaseID, jobID string, expiresAt int64) (string, error) {
	key, err := os.ReadFile(updateauth.DeviceKeyPath(dataDir))
	if err != nil {
		return "", err
	}
	return updateauth.ComputeMAC(key, releaseID, jobID, expiresAt)
}
`)
	requirePrefixes(t, mintOffenders(t, root),
		rel+": references updateauth.DeviceKeyPath",
		rel+": references updateauth.ComputeMAC",
		rel+": imports nofx/internal/updateauth more than once")
}

// PIN (M3 census repair, verifier D2 — probe V3 verbatim): the census parsed
// with mode 0, which drops comments, so a //go:linkname directive was
// invisible. V3 pulls updateauth.ComputeMAC into package kernel under a local
// name — no import of updateauth, no selector, no restricted identifier — and
// a main linking nofx/api printed the exact HMAC-SHA256 with the census green.
// Any //go:linkname in non-test code is now an offence (the module has none);
// the directive in a _test.go file is outside the walk (tests are not linked
// into the app), and a prose mention that is not a directive stays clean.
func TestUpdateAuthCensusRefusesLinkname(t *testing.T) {
	root := mintBase(t)
	const rel = "kernel/zz_v3_link.go"
	mintWrite(t, root, rel, `package kernel

import _ "unsafe"

//go:linkname v3ComputeMAC nofx/internal/updateauth.ComputeMAC
func v3ComputeMAC(key []byte, releaseID, jobID string, expiresAt int64) (string, error)

func V3Mint(key []byte, releaseID, jobID string, expiresAt int64) (string, error) {
	return v3ComputeMAC(key, releaseID, jobID, expiresAt)
}
`)
	requirePrefixes(t, mintOffenders(t, root), rel+": //go:linkname")

	// the push form, inside the package itself, is refused too
	root = mintBase(t)
	mintWrite(t, root, "internal/updateauth/push.go", "package updateauth\n\nimport _ \"unsafe\"\n\n//go:linkname computeMACForLink\nfunc computeMACForLink() {}\n")
	requirePrefixes(t, mintOffenders(t, root), "internal/updateauth/push.go: //go:linkname")

	// controls: a prose mention is not a directive; a test file is not walked
	root = mintBase(t)
	mintWrite(t, root, "kernel/doc.go", "// Package kernel never uses go:linkname (see // go:linkname in the census).\npackage kernel\n")
	mintWrite(t, root, "kernel/zz_link_test.go", "package kernel\n\nimport _ \"unsafe\"\n\n//go:linkname t nofx/internal/updateauth.ComputeMAC\nfunc t()\n")
	if off := mintOffenders(t, root); len(off) != 0 {
		t.Fatalf("prose and a test file must stay clean:\n%s", strings.Join(off, "\n"))
	}
}
