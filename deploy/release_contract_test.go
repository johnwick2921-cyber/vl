package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// W-ONE-BUTTON WAVE 3a — the release workflow's guarantees, pinned.
//
// A release workflow is a thing nobody reads again until the day it matters,
// and by then the person reading it is under pressure. These tests state what
// it must do in words, so a change that quietly removes a guard fails here
// instead of shipping a package with a secret in it.

func repoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", rel))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return string(b)
}

// The trigger is the whole safety story: a release is cut from a TAG that a
// human approved, never from a push to a branch.
func TestReleaseWorkflowTriggersOnTagAndNeverOnPushToDev(t *testing.T) {
	y := repoFile(t, ".github/workflows/release.yml")
	if !strings.Contains(y, "tags:") || !strings.Contains(y, "v*") {
		t.Fatalf("release.yml must trigger on a v* TAG")
	}
	for _, forbidden := range []string{"branches:", "- dev", "pull_request:"} {
		if strings.Contains(y, forbidden) {
			t.Fatalf("release.yml must NEVER trigger on %q — a release is cut from an approved tag", forbidden)
		}
	}
	if !strings.Contains(y, "environment:") || !strings.Contains(y, "release") {
		t.Fatalf("release.yml must run in the protected 'release' Environment (owner = required reviewer)")
	}
}

// The binary must be provably built from clean, tagged source.
func TestReleaseWorkflowEnforcesCleanVcsStampAndTheGuideRev(t *testing.T) {
	y := repoFile(t, ".github/workflows/release.yml")
	if !strings.Contains(y, "vcs.modified") {
		t.Fatalf("release.yml must enforce vcs.modified=false — a dirty build is not a release")
	}
	if !strings.Contains(y, "VITE_GUIDE_BUILT_REV") {
		t.Fatalf("release.yml must pass VITE_GUIDE_BUILT_REV so the guide cannot lie about the binary")
	}
}

// The artifact repository is an OPEN OWNER DECISION. The fail-closed default is
// THIS repo; the partner repo must never be reachable by accident.
func TestReleaseWorkflowDefaultsToThisRepoAndNeverThePartner(t *testing.T) {
	y := repoFile(t, ".github/workflows/release.yml")
	if !strings.Contains(y, "RELEASE_REPO") {
		t.Fatalf("the artifact target must be ONE variable, RELEASE_REPO, so the owner changes it in one line")
	}
	if !strings.Contains(y, "johnwick2921-cyber/vl") {
		t.Fatalf("RELEASE_REPO must DEFAULT to this repo")
	}
	if strings.Contains(y, "vlautoagenttraderv1") {
		t.Fatalf("the partner repo must never appear in the release workflow")
	}
}

// --- executable proofs: the scripts must REFUSE, not warn ---

func runScript(t *testing.T, script string, args ...string) (string, error) {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", script))
	if err != nil {
		t.Fatal(err)
	}
	// A MISSING script exits 127, which is also non-zero — so without this a
	// "it refused" assertion would be satisfied by "it was not there". That is
	// the shape that has bitten this wave twice.
	if st, serr := os.Stat(p); serr != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("%s must exist and be executable before its refusals mean anything (%v)", script, serr)
	}
	out, err := exec.Command("bash", append([]string{p}, args...)...).CombinedOutput()
	return string(out), err
}

// A package carrying a secret is rejected. This is the one that matters most:
// everything else costs a rebuild, this costs a credential.
func TestSecretScanRejectsASecretInTheStagedTree(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vl-bin"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The deny-list entry that has actually bitten this repo before.
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DEEPSEEK_API_KEY=sk-live-should-never-ship\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runScript(t, "deploy/release/secret-scan.sh", dir)
	if err == nil {
		t.Fatalf("a staged tree containing .env MUST be rejected; scan exited 0.\n%s", out)
	}
	if !strings.Contains(out, ".env") {
		t.Fatalf("the refusal must NAME the offending path, got:\n%s", out)
	}
}

func TestSecretScanAcceptsACleanTree(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vl-bin"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := runScript(t, "deploy/release/secret-scan.sh", dir); err != nil {
		t.Fatalf("a clean tree must pass, got error %v:\n%s", err, out)
	}
}

// A path that escapes the extraction root is rejected before it is written.
// The workflow must refuse with an INSTRUCTION when the owner has not yet
// created the release keypair, rather than a file-not-found from ssh-keygen.
func TestReleaseWorkflowRefusesWithInstructionsWhenThePublicKeyIsMissing(t *testing.T) {
	y := repoFile(t, ".github/workflows/release.yml")
	// NOTE: this used to assert deploy/release.pub. That was the BROKEN form —
	// ssh-keygen -Y verify reads its -f file as allowed-signers, so a bare
	// public key never verifies (P1-a, proven by
	// TestReleaseSignatureVerifiesOnlyWithAnAllowedSignersFile). The assertion
	// was pinning the defect, which is why it had to move with the fix.
	if !strings.Contains(y, "deploy/release_allowed_signers") {
		t.Fatalf("the workflow must verify against the COMMITTED allowed-signers file")
	}
	if !strings.Contains(y, "RELEASE_SIGNING_KEY") {
		t.Fatalf("the private half must come from the release Environment secret")
	}
	if !strings.Contains(y, "deploy/release/README.md") {
		t.Fatalf("the refusal must point at the one-time owner instructions")
	}
}

// A rollback pair is advertised tested ONLY when the compat job proved it.
func TestReleaseWorkflowNeverFabricatesARollbackPair(t *testing.T) {
	y := repoFile(t, ".github/workflows/release.yml")
	if strings.Contains(y, "ROLLBACK_PAIRS: '[]'") {
		t.Fatalf("ROLLBACK_PAIRS is now computed by the dbcompat job; a literal would assert a pair nobody proved")
	}
	// NOTE: this previously asserted that `needs.dbcompat` was ABSENT, because
	// the job did not exist yet and a reference to a missing job parses but
	// fails at run time. The job exists now, so the assertion inverts: the
	// pairs must come FROM it rather than from a literal.
	if !strings.Contains(y, "needs.dbcompat.outputs.rollback_pairs") {
		t.Fatalf("ROLLBACK_PAIRS must be COMPUTED by the dbcompat job, not written as a literal")
	}
	if !strings.Contains(y, "dbcompat:") {
		t.Fatalf("the dbcompat job must exist in the same workflow")
	}
}

func TestPackagerRejectsAnArchivePathThatEscapesTheRoot(t *testing.T) {
	out, err := runScript(t, "deploy/release/check-archive-paths.sh", "../../etc/passwd")
	if err == nil {
		t.Fatalf("a path escaping the root MUST be rejected; exited 0.\n%s", out)
	}
	if out2, err2 := runScript(t, "deploy/release/check-archive-paths.sh", "web/dist/index.html"); err2 != nil {
		t.Fatalf("an ordinary relative path must be accepted, got %v:\n%s", err2, out2)
	}
}

// P1-a — the signature verify must use an ALLOWED_SIGNERS file, not a bare
// public key. `ssh-keygen -Y verify -f <file>` parses <file> as
// "<principal> <keytype> <base64> [comment]". A bare .pub starts with the
// KEYTYPE, so ssh-keygen reads "ssh-ed25519" as the principal and -I release
// then matches nothing. This test signs with a REAL local keypair and proves
// both directions, so the fix is not taken on faith.
func TestReleaseSignatureVerifiesOnlyWithAnAllowedSignersFile(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}
	dir := t.TempDir()
	key := filepath.Join(dir, "k")
	if out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "vl-release-test", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("keygen: %v\n%s", err, out)
	}
	msg := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(msg, []byte(`{"release_id":"vtest"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("ssh-keygen", "-Y", "sign", "-f", key, "-n", "release", msg).CombinedOutput(); err != nil {
		t.Fatalf("sign: %v\n%s", err, out)
	}
	sig := msg + ".sig"

	verify := func(signersFile string) error {
		c := exec.Command("ssh-keygen", "-Y", "verify", "-f", signersFile, "-I", "release", "-n", "release", "-s", sig)
		in, _ := os.Open(msg)
		defer in.Close()
		c.Stdin = in
		_, err := c.CombinedOutput()
		return err
	}

	// The WRONG form the workflow used to carry: the bare public key.
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(dir, "bare.pub")
	if err := os.WriteFile(bare, pub, 0o644); err != nil {
		t.Fatal(err)
	}
	if verify(bare) == nil {
		t.Fatalf("a BARE .pub must NOT verify — if it does, this test proves nothing about the fix")
	}

	// The correct form: "<principal> <keytype> <base64> [comment]".
	allowed := filepath.Join(dir, "allowed_signers")
	if err := os.WriteFile(allowed, []byte("release "+string(pub)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verify(allowed); err != nil {
		t.Fatalf("the allowed_signers form MUST verify: %v", err)
	}
}

// ...and the workflow and README must actually use that form.
func TestReleaseWorkflowUsesTheAllowedSignersFile(t *testing.T) {
	y := repoFile(t, ".github/workflows/release.yml")
	if !strings.Contains(y, "release_allowed_signers") {
		t.Fatalf("verify must use deploy/release_allowed_signers, not a bare .pub")
	}
	if strings.Contains(y, "-f deploy/release.pub") {
		t.Fatalf("the bare-.pub verify form is broken and must not appear")
	}
	r := repoFile(t, "deploy/release/README.md")
	if !strings.Contains(r, "release_allowed_signers") {
		t.Fatalf("the owner instructions must produce the allowed_signers file")
	}
}

// P1-b — the job that holds the signing key must not run an unpinned remote
// script, and an absent scanner must FAIL the job rather than silently
// downgrade it to the deny-list.
func TestReleaseWorkflowDoesNotPipeAnUnpinnedRemoteScriptIntoShell(t *testing.T) {
	y := repoFile(t, ".github/workflows/release.yml")
	if strings.Contains(y, "install.sh | sh") || strings.Contains(y, "| sh -s") {
		t.Fatalf("a job holding RELEASE_SIGNING_KEY must not pipe a remote script into a shell")
	}
	if strings.Contains(y, "master/scripts") {
		t.Fatalf("no fetching from a MOVING branch in the signing job")
	}
	if !strings.Contains(y, "GITLEAKS_REQUIRED") {
		t.Fatalf("gitleaks must be REQUIRED in CI — an absent scanner is not a pass")
	}
}

// P2-b — deploy/RELEASE must be WRITTEN from the source sha, never copied. The
// checked-in file is the marker the BOOT procedure wrote for the PREVIOUS
// boot, so a copy ships a sha that disagrees with the binary beside it.
func TestPackagerWritesTheReleaseMarkerFromTheSourceSha(t *testing.T) {
	src := t.TempDir()
	for _, p := range []string{"vl-bin", "LICENSE", "ninjascript/x.cs", "ninjascript/vltrader_tcp_PROTOCOL.md", "web/dist/index.html"} {
		full := filepath.Join(src, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A STALE marker in the source tree — the previous boot's sha.
	if err := os.MkdirAll(filepath.Join(src, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := strings.Repeat("a", 40)
	if err := os.WriteFile(filepath.Join(src, "deploy/RELEASE"), []byte(stale+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stage := filepath.Join(t.TempDir(), "stage")
	want := strings.Repeat("b", 40)
	if out, err := runScript(t, "deploy/release/package.sh", src, stage, want); err != nil {
		t.Fatalf("package failed: %v\n%s", err, out)
	}
	got, err := os.ReadFile(filepath.Join(stage, "deploy/RELEASE"))
	if err != nil {
		t.Fatalf("staged RELEASE missing: %v", err)
	}
	if strings.TrimSpace(string(got)) == stale {
		t.Fatalf("the STALE checked-in marker was copied — it must be written from the source sha")
	}
	if strings.TrimSpace(string(got)) != want {
		t.Fatalf("staged RELEASE = %q, want the source sha %q", strings.TrimSpace(string(got)), want)
	}
	// No sha at all must be refused rather than defaulted.
	if out, err := runScript(t, "deploy/release/package.sh", src, filepath.Join(t.TempDir(), "s2")); err == nil {
		t.Fatalf("packaging without a source sha must be REFUSED:\n%s", out)
	}
}

// P2-c — an empty COMPUTED list is []; an UNCOMPUTED one is null. A reader that
// cannot tell them apart treats an unrun job as a proven-empty result.
func TestManifestRendersUncomputedListsAsNullNotEmpty(t *testing.T) {
	stage := t.TempDir()
	if err := os.WriteFile(filepath.Join(stage, "vl-bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// P3 made manifest.sh require a staged deploy/RELEASE that AGREES with the
	// source sha. This fixture predates that and staged none — the guard
	// correctly refused it. The fixture is completed rather than the guard
	// relaxed.
	sha := strings.Repeat("c", 40)
	if err := os.MkdirAll(filepath.Join(stage, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "deploy/RELEASE"), []byte(sha+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runScript(t, "deploy/release/manifest.sh", stage, sha, "v9.9.9")
	if err != nil {
		t.Fatalf("manifest failed: %v\n%s", err, out)
	}
	for _, field := range []string{`"upgrade_pairs": null`, `"rollback_pairs": null`, `"capabilities_required": null`} {
		if !strings.Contains(out, field) {
			t.Fatalf("an UNCOMPUTED list must render null, missing %q in:\n%s", field, out)
		}
	}
	if out2, err2 := runScript(t, "deploy/release/manifest.sh", stage, "not40hex", "v9.9.9"); err2 == nil {
		t.Fatalf("a source sha that is not 40 hex must be REFUSED:\n%s", out2)
	}
}

// The rollback step that nobody runs must be the one this job insists on: OLD
// binary booting the MIGRATED database. A forward migration that drops a
// column the old binary still SELECTs fails exactly there, and only there.
func TestDbCompatProvesTheRollbackDirectionNotJustTheUpgrade(t *testing.T) {
	sh := repoFile(t, "deploy/release/db-compat.sh")
	if !strings.Contains(sh, "3-old-boots-migrated-db") {
		t.Fatalf("db-compat must boot the OLD binary against the MIGRATED database — that is the rollback")
	}
	for _, step := range []string{"1-old-creates-fresh", "2-new-migrates-forward"} {
		if !strings.Contains(sh, step) {
			t.Fatalf("missing ordered step %q", step)
		}
	}
	// The verdict must not be taken from the process exit: with no NT8 in CI a
	// binary is not expected to stay up, so a green exit proves nothing.
	if !strings.Contains(sh, "sqlite_master") {
		t.Fatalf("the verdict must be read off the DATABASE (table count), not the process")
	}
	if !strings.Contains(sh, "NOT PROVEN") {
		t.Fatalf("an unproven pair must say so; the caller advertises tested:false")
	}
}

// P3 — the packaged marker and the manifest must agree, enforced in CODE and
// not only by a test, so the guarantee survives outside the suite.
func TestManifestRefusesWhenTheStagedReleaseDisagreesWithTheSourceSha(t *testing.T) {
	stage := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stage, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "vl-bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("b", 40)
	if err := os.WriteFile(filepath.Join(stage, "deploy/RELEASE"), []byte(strings.Repeat("a", 40)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runScript(t, "deploy/release/manifest.sh", stage, want, "v1"); err == nil {
		t.Fatalf("a staged RELEASE that disagrees with the source sha must be REFUSED:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(stage, "deploy/RELEASE"), []byte(want+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runScript(t, "deploy/release/manifest.sh", stage, want, "v1"); err != nil {
		t.Fatalf("an agreeing RELEASE must pass: %v\n%s", err, out)
	}
}

// The db-compat job mints throwaway keys. They live in its WORK dir, which the
// packager never reads — but "never reads" is a claim, so it is asserted: a
// staged tree must contain neither key, under any name.
func TestStagedTreeNeverContainsTheEphemeralBootSecrets(t *testing.T) {
	src := t.TempDir()
	for _, p := range []string{"vl-bin", "LICENSE", "ninjascript/x.cs", "ninjascript/vltrader_tcp_PROTOCOL.md", "web/dist/index.html"} {
		full := filepath.Join(src, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Plant both shapes in the SOURCE tree; the allow-list must not carry them.
	if err := os.WriteFile(filepath.Join(src, "rsa.pem"), []byte("-----BEGIN PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(t.TempDir(), "stage")
	if out, err := runScript(t, "deploy/release/package.sh", src, stage, strings.Repeat("d", 40)); err != nil {
		t.Fatalf("package failed: %v\n%s", err, out)
	}
	_ = filepath.Walk(stage, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		base := filepath.Base(p)
		if base == "rsa.pem" || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") {
			t.Fatalf("an ephemeral boot secret reached the stage: %s", p)
		}
		return nil
	})
	// And the scan agrees independently.
	if out, err := runScript(t, "deploy/release/secret-scan.sh", stage); err != nil {
		t.Fatalf("a correctly staged tree must pass the scan: %v\n%s", err, out)
	}
}

// The throwaway keys must not outlive the run, by ANY exit path.
func TestDbCompatShredsItsEphemeralKeysOnExit(t *testing.T) {
	sh := repoFile(t, "deploy/release/db-compat.sh")
	if !strings.Contains(sh, "trap cleanup_boot_secrets EXIT INT TERM") {
		t.Fatalf("the ephemeral keys must be shredded on EXIT, INT and TERM — not only on the happy path")
	}
	if !strings.Contains(sh, "shred -u") {
		t.Fatalf("the key file must be shredded, not merely unlinked")
	}
}

// The negative proof must run in CI, not only as a unit re-implementation.
func TestReleaseWorkflowProvesAProdBuildWithoutTheRevFails(t *testing.T) {
	y := repoFile(t, ".github/workflows/release.yml")
	if !strings.Contains(y, "VITE_GUIDE_BUILT_REV= npm run build") {
		t.Fatalf("CI must prove the NEGATIVE: a prod build with no rev must fail")
	}
	if !strings.Contains(y, "the guard is gone") {
		t.Fatalf("the negative step must fail loudly when the build unexpectedly succeeds")
	}
}

// W-ONE-BUTTON M4 — cutover.sh must be a BOOT PROCEDURE, not a restart with a
// false safety net. These pin the three P1 defects the CTO found in v1.
func TestCutoverInstallsTheNewBinaryItWasGiven(t *testing.T) {
	sh := repoFile(t, "deploy/cutover.sh")
	// P1-a: v1 took no <new-bin> at all. It backed up whatever was already
	// installed, so either nothing changed or the "backup" WAS the new binary
	// and rollback restored the thing being rolled back from.
	if !strings.Contains(sh, "NEW_BIN=") {
		t.Fatalf("cutover must take the new binary as an argument")
	}
	// MOVED WITH THE CHANGE (CLASS 239). These used to assert that cutover.sh
	// ITSELF greps `vcs.revision=$NEW_SHA` out of `go version -m` and stages a
	// `vl-bin.new`. v7 delegates both to cmd/vl-activate, so the shell no
	// longer contains those strings — and asserting them would now be pinning
	// the OLD implementation rather than the guarantee.
	//
	// The guarantee did not weaken, it moved somewhere better tested:
	// internal/activation.Stage reads the build info with debug/buildinfo
	// (no text to misparse — the v6 awk read the wrong field because
	// `go version -m` is TAB-separated) and refuses on revision, dirty tree or
	// md5 mismatch; TestStageRefuses* mutation-prove each refusal. The atomic
	// install is atomicCopy (temp in the destination's own directory + rename),
	// pinned by TestActivateInstallsAllThreeHalvesBeforeKilling.
	//
	// What this test can still guarantee is that the shell DELEGATES rather
	// than growing a second implementation, which is the drift v7 exists to end.
	if !strings.Contains(sh, "vl-activate") {
		t.Fatalf("cutover.sh must delegate the proof to cmd/vl-activate, not reimplement it")
	}
	if !strings.Contains(sh, "verify -release") {
		t.Fatalf("cutover.sh must PROVE the new binary (vl-activate verify) before anything is touched")
	}
}

func TestCutoverRefusesWithoutAPassingInstallationGate(t *testing.T) {
	sh := repoFile(t, "deploy/cutover.sh")
	// P1-b: v1 SIGKILLed the trader with no check for an open position, a
	// non-terminal armed row, or an in-flight send (class 33 legs 1-5).
	// Finding [1] (preboot 4c05158b): /api/cutover-gate answers for ONE trader
	// (newest created_at), so the old script gated every OTHER trader out of
	// existence before the kill. The fold: /api/installation-gate, whose legs
	// the script REQUIRES by name — and whose overall "ready" verdict it must
	// NEVER trust (addon_census can never pass on a never-held bot).
	if !strings.Contains(sh, "VL_CUTOVER_TOKEN") || !strings.Contains(sh, "cutover gate needs a token") {
		t.Fatalf("no token must REFUSE, and the token must never be a command-line argument")
	}
	if !strings.Contains(sh, "/api/installation-gate") {
		t.Fatalf("the installation gate must be asked before the kill")
	}
	if strings.Contains(sh, "/api/cutover-gate") {
		t.Fatalf("the one-trader cutover gate must NOT be consulted — it cannot fail for any trader but the newest (finding [1])")
	}
	if !strings.Contains(sh, "require_legs 'trader_cutover:*'") ||
		!strings.Contains(sh, "require_legs 'ledger_exposure'") ||
		!strings.Contains(sh, "require_legs 'planner_in_flight'") ||
		!strings.Contains(sh, "require_legs 'traders_nt8'") {
		t.Fatalf("every trader's cutover legs + ledger_exposure + planner_in_flight + traders_nt8 must be REQUIRED by name")
	}
	if !strings.Contains(sh, "addon_census_prehold") {
		t.Fatalf("addon_census_prehold must be required whenever the payload has it (#206 adds the leg)")
	}
	if !strings.Contains(sh, "NEVER trusted") {
		t.Fatalf("the overall ready verdict must be declared untrusted, or a green gate hides a failing non-required leg")
	}
	if !strings.Contains(sh, "failing installation-gate legs:") {
		t.Fatalf("a failing required leg must refuse the cutover and be named")
	}
}

// interpolationRefused returns the offending header form when sh interpolates
// a cutover token into a curl Authorization header, else "". ANY expansion
// that names CUTOVER_TOKEN is refused (RENAME-R5: the VL_ form and any future name,
// and any future name — the token must never ride argv).
func interpolationRefused(sh string) string {
	re := regexp.MustCompile(`Authorization: Bearer \$\{[A-Z0-9_]*CUTOVER_TOKEN\}`)
	return re.FindString(sh)
}

func TestCutoverTokenNeverRidesAProcessArgv(t *testing.T) {
	sh := repoFile(t, "deploy/cutover.sh")
	// Findings [25]/[29]: the token was interpolated into curl's -H header, i.e.
	// argv, readable by any UID via ps//proc/<pid>/cmdline for the call's
	// lifetime — while the script's own refusal text says "never pass it on the
	// command line". The fold: a 0600 header file, curl -H @file, removed on
	// every exit path.
	if m := interpolationRefused(sh); m != "" {
		t.Fatalf("the token must never be interpolated into curl's argv — it rides a header FILE (%s)", m)
	}
	if !strings.Contains(sh, `-H "@$TOKEN_HDR"`) {
		t.Fatalf("curl must receive the header via -H @file")
	}
	if !strings.Contains(sh, "umask 077") {
		t.Fatalf("the token header file must be written 0600")
	}
	if !strings.Contains(sh, `rm -f "${TOKEN_HDR:-}"`) {
		t.Fatalf("the token header file must be removed on every exit path (trap)")
	}
}

// RENAME-R5: the refusal covers every CUTOVER_TOKEN name — narrowing it back
// to one literal form must fail THIS test (the mutant list's item 11).
func TestCutoverTokenInterpolationRefusalCoversAnyCUTOVER_TOKENName(t *testing.T) {
	for _, line := range []string{
		"curl -H \"Authorization: Bearer ${VL_CUTOVER_TOKEN}\" http://x",
		"curl -H \"Authorization: Bearer ${SOME_FUTURE_CUTOVER_TOKEN}\" http://x",
		"curl -H \"Authorization: Bearer ${SOME_OTHER_CUTOVER_TOKEN}\" http://x",
	} {
		if interpolationRefused(line) == "" {
			t.Fatalf("the interpolation %q must be refused", line)
		}
	}
	if interpolationRefused("curl -H \"Authorization: Bearer ${TOKEN}\" http://x") != "" {
		t.Fatalf("a non-CUTOVER_TOKEN expansion is not this refusal's business")
	}
}

func TestCutoverNeverInstructsRollbackForAPreInstallFailure(t *testing.T) {
	sh := repoFile(t, "deploy/cutover.sh")
	// Finding [24]: v6 ran `cp ... || { rollback; die }` — a staging failure
	// BEFORE anything was live invoked rollback(), which SIGKILLs a healthy
	// bot and re-proves the old rev for a cutover that never started. The
	// plan must split the failure space: before anything moved, REFUSE with
	// no restart and NO rollback; only after the install began may the
	// rollback command be named.
	if strings.Contains(sh, "on ANY failure: vl-activate rollback") {
		t.Fatalf("a pre-install failure must NOT route to rollback — nothing was touched, the healthy bot must not be restarted (finding [24])")
	}
	if !strings.Contains(sh, "NO rollback runs") {
		t.Fatalf("the plan must say a pre-install failure REFUSES with NO rollback")
	}
	if !strings.Contains(sh, "failure AFTER vl-activate began installing") {
		t.Fatalf("rollback must be named only for a failure AFTER the install began")
	}
}

func TestCutoverRollbackRestartsAndProvesTheOldRev(t *testing.T) {
	sh := repoFile(t, "deploy/cutover.sh")
	// P1-c: after a failed boot the RUNNING process is the NEW binary, so a
	// files-only rollback leaves the bad build serving while printing
	// "restart the unit". The canon requires a TESTED auto-rollback.
	// MOVED WITH THE CHANGE (CLASS 239). v7 does not implement rollback in
	// bash any more, so ROLLBACK OK / wait_boot / the RELEASE write are no
	// longer strings in this file. Asserting them would pin the old shell.
	//
	// The guarantee moved and got STRONGER: internal/activation.Rollback and
	// RollbackTo restore all three halves (binary, dist AND the RELEASE
	// marker — "restore the binary" leaves the UI serving the new bundle and
	// the marker claiming the new sha), refuse to signal a recycled pid, and
	// return the identity the caller then proves with Watch. Pinned by
	// TestRollbackRestoresTheDistAndTheMarkerNotJustTheBinary and
	// TestRollbackRefusesToSignalARecycledPID, both mutation-proven.
	//
	// v7 must still TELL the operator how to roll back, or the procedure is
	// only in someone's head.
	if !strings.Contains(sh, "rollback -prev") {
		t.Fatalf("cutover.sh must name the rollback command, or the procedure exists only in someone's head")
	}
	if !strings.Contains(sh, "--dry-run") {
		t.Fatalf("a procedure nobody has executed is not TESTED; --dry-run is how it gets exercised")
	}
}

// TestGuideRevIsRefusedByTheBUILDNotByAModuleScopeThrow pins the enforcement
// POINT, not the message. The first version of this guarantee was a `throw` at
// module scope in web/src/guide/types.ts; Vite never executes the module while
// building, so `VITE_GUIDE_BUILT_REV= npx vite build` exited 0 and the throw
// shipped INTO the bundle to fire on page load — a white screen on the trading
// UI instead of a refused build.
//
// This test cannot run a Vite build, so it is NOT the proof: the proof is the
// RED/GREEN pair recorded in the commit (empty rev exited 0 before, exits 1
// after). What this pin CAN do is fail if the build-time gate is ever deleted
// or silently moved back into application code, which is how the defect got in.
func TestGuideRevIsRefusedByTheBUILDNotByAModuleScopeThrow(t *testing.T) {
	cfg := repoFile(t, "web/vite.config.ts")
	if !strings.Contains(cfg, "guide-built-rev-is-a-build-input") {
		t.Fatalf("the build-time gate plugin is gone from vite.config.ts — a module-scope guard cannot refuse a build")
	}
	if !strings.Contains(cfg, "apply: 'build'") {
		t.Fatalf("the gate must apply to the BUILD; a dev-only plugin refuses nothing that ships")
	}
	if !strings.Contains(cfg, "VITE_GUIDE_BUILT_REV") {
		t.Fatalf("the gate must read VITE_GUIDE_BUILT_REV")
	}
	ts := repoFile(t, "web/src/guide/types.ts")
	if strings.Contains(ts, "throw new Error") {
		t.Fatalf("src/guide/types.ts throws again at module scope: that ships a crash into the bundle instead of failing the build")
	}
	if !strings.Contains(ts, "'unknown'") {
		t.Fatalf("an unusable rev must degrade to the honest 'unknown' at runtime, never a real-looking sha and never a crash")
	}
}

// TestCutoverDistinguishesAnUnstampedBinaryFromAWrongOne pins CLASS 248: two
// causes must not share one refusal. A binary built in a linked git worktree
// carries NO vcs stamps (proven 2026-09-24: `go build` and `-buildvcs=true`
// both produced zero vcs.* entries in a worktree, while a clean clone of the
// same commit produced vcs.revision + vcs.modified=false). Every lane builds
// in a worktree, so telling that operator "it is not the binary for this sha"
// sends them to check a sha that is already correct — and a guard that looks
// broken gets deleted mid-boot.
func TestCutoverDistinguishesAnUnstampedBinaryFromAWrongOne(t *testing.T) {
	sh := repoFile(t, "deploy/cutover.sh")
	// MOVED WITH THE CHANGE (CLASS 239). Both refusals now live in
	// internal/activation.Stage, which cutover.sh reaches through
	// `vl-activate verify`. The DISTINCTION is the guarantee — an unstamped
	// binary and a wrong-revision binary send the operator to different
	// places, and a refusal that names the wrong cause sends them to fix
	// something that is not broken — so it is pinned where it now lives:
	// TestStageRefusesABinaryWithNoVCSStampsAndSaysWhy asserts cause AND cure,
	// and TestStageRefusesAStampedBinaryWithADifferentRevision asserts the
	// wrong-revision path does NOT reuse the unstamped wording.
	lib := repoFile(t, "internal/activation/activation.go")
	if !strings.Contains(lib, "carries NO vcs stamps at all") {
		t.Fatalf("the library must refuse an UNSTAMPED binary with its own message")
	}
	if !strings.Contains(lib, "linked git worktree") {
		t.Fatalf("the unstamped refusal must name the CAUSE (a worktree build), not just the symptom")
	}
	if !strings.Contains(lib, "clean clone") {
		t.Fatalf("the unstamped refusal must name the CURE (build from a clean clone)")
	}
	if !strings.Contains(lib, "is stamped, but with revision") {
		t.Fatalf("a stamped-but-wrong binary must get a DIFFERENT message than an unstamped one")
	}
	if !strings.Contains(sh, "vl-activate") {
		t.Fatalf("cutover.sh must reach those refusals by delegating, not by reimplementing them")
	}
}

// F2: the manifest's protocol_version must come from the CODE.
//
// It used to grep the first `protocol_version` out of the protocol MARKDOWN,
// and the first match in that file is a JSON EXAMPLE showing 2 — while the
// shipped wire has been 3 since the symbol-tagged-fills generation. The
// manifest reported a protocol version the release does not speak, to an
// updater that trusts the manifest for compatibility. The line directly above
// it in the same script reads the AddOn build id "from the AddOn source rather
// than restated here (L7: READ, never literal)"; this one read prose.
func TestManifestReadsTheProtocolVersionFromCodeNotDocumentation(t *testing.T) {
	m := repoFile(t, "deploy/release/manifest.sh")
	if strings.Contains(m, "vltrader_tcp_PROTOCOL.md 2>/dev/null | head -1") {
		t.Fatal("protocol_version is still scraped from the markdown, whose first match is an example")
	}
	if !strings.Contains(m, "PROTOCOL_VERSION") || !strings.Contains(m, "tcp_framing.go") {
		t.Fatal("protocol_version must be read from the C# constant and the Go constant")
	}
	if !strings.Contains(m, "disagree; they ship in lockstep or not at all") {
		t.Fatal("a disagreement between the two constants must be a REFUSAL, not a preference")
	}
	// And the two constants must actually agree right now.
	cs := repoFile(t, "ninjascript/VLTraderTCPClient.cs")
	gofile := repoFile(t, "provider/ninjatrader/tcp_framing.go")
	csV := regexp.MustCompile(`PROTOCOL_VERSION[^=]*=\s*(\d+)`).FindStringSubmatch(cs)
	goV := regexp.MustCompile(`const ProtocolVersion\s*=\s*(\d+)`).FindStringSubmatch(gofile)
	if csV == nil || goV == nil {
		t.Fatalf("cannot read the constants: cs=%v go=%v", csV, goV)
	}
	if csV[1] != goV[1] {
		t.Fatalf("PROTOCOL_VERSION %s != ProtocolVersion %s — they ship in lockstep", csV[1], goV[1])
	}
}

// F2: a manifest that lists ITSELF at 0 bytes is describing a placeholder. The
// entry would carry the sha256 of the empty string, which matches nothing that
// ships.
func TestManifestRefusesAZeroByteSelfEntry(t *testing.T) {
	m := repoFile(t, "deploy/release/manifest.sh")
	if !strings.Contains(m, "the staged manifest.json is 0 bytes") {
		t.Fatal("a 0-byte manifest self-entry must be REFUSED, not emitted")
	}
}

// F3: owner-editable data is not a program artifact. Activation installs the
// binary, the bundle and the marker — and must NOT install this, or every
// update silently discards the owner's edits.
func TestManifestSeparatesOwnerDataFromProgramArtifacts(t *testing.T) {
	m := repoFile(t, "deploy/release/manifest.sh")
	if !strings.Contains(m, `"owner_data"`) {
		t.Fatal("the manifest must distinguish owner-editable data from program artifacts")
	}
	if !strings.Contains(m, "calendar_static_t1.json") {
		t.Fatal("calendar_static_t1.json must be classified, not left implicit")
	}
	if !strings.Contains(m, "template-only") {
		t.Fatal("owner data must be marked as shipped-as-template, never installed over an existing file")
	}
}

// D1-FOLD (DS-105): a post-R1b archive that holds ONLY the vl updater binaries
// must still stage them — OPTIONAL accepts both name pairs (R5 removes the
// old-name pair). Dropping the vl entries must fail THIS test.
func TestPackagerStagesAnArchiveHoldingOnlyVlUpdaterBinaries(t *testing.T) {
	src := t.TempDir()
	for _, p := range []string{"vl-bin", "LICENSE", "ninjascript/x.cs", "ninjascript/vltrader_tcp_PROTOCOL.md", "web/dist/index.html",
		"updater/vl-updater", "updater/vl-updater-bootstrap"} {
		full := filepath.Join(src, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(src, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(t.TempDir(), "stage")
	if out, err := runScript(t, "deploy/release/package.sh", src, stage, strings.Repeat("e", 40)); err != nil {
		t.Fatalf("package failed: %v\n%s", err, out)
	}
	for _, want := range []string{"updater/vl-updater", "updater/vl-updater-bootstrap"} {
		if _, err := os.Stat(filepath.Join(stage, want)); err != nil {
			t.Fatalf("the archive dropped %s: %v", want, err)
		}
	}
}
