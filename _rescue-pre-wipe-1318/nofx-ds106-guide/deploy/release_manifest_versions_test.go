package deploy

// W-PR-B fold [10] — the manifest header promises "nothing is defaulted to a
// plausible value", yet nt8.min_version defaulted to 8.1.2.1, max_tested_version
// to 'n/a' and updater_min_version to 0.0.0 — a fabricated "tested range" for
// an updater that trusts the manifest. A version nobody measured is null
// (A24: absent ≠ a guess). The only honest sources are explicit env values.
//
// Class-250 probe: THIS TEST IS THE CALLER — it runs the REAL manifest.sh
// against a staged fixture. The mutation that proves it: reinstating the
// ${NT8_MIN:-8.1.2.1} / ${NT8_MAX_TESTED:-n/a} / ${UPDATER_MIN:-0.0.0}
// literals fails the null assertions below.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// manifestFixture stages what manifest.sh demands: an artifact, and a
// deploy/RELEASE marker that AGREES with the source sha (P3).
func manifestFixture(t *testing.T) (stage, sha string) {
	t.Helper()
	stage = t.TempDir()
	if err := os.WriteFile(filepath.Join(stage, "nofx-bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha = strings.Repeat("c", 40)
	if err := os.MkdirAll(filepath.Join(stage, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "deploy/RELEASE"), []byte(sha+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return stage, sha
}

// versionlessEnv strips the three version vars so the child sees the true
// "nobody provided them" state — no duplicates, no empty-string ambiguity.
func versionlessEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "NT8_MIN=") || strings.HasPrefix(kv, "NT8_MAX_TESTED=") || strings.HasPrefix(kv, "UPDATER_MIN=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func TestManifestNeverDefaultsAFabricatedTestedRange(t *testing.T) {
	stage, sha := manifestFixture(t)

	run := func(env []string) string {
		t.Helper()
		cmd := exec.Command("bash", "deploy/release/manifest.sh", stage, sha, "v9.9.9")
		cmd.Dir = repoRoot(t)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("manifest failed: %v\n%s", err, out)
		}
		return string(out)
	}

	// no env ⇒ null, never a guessed tested range
	out := run(versionlessEnv())
	for _, want := range []string{`"min_version": null`, `"max_tested_version": null`, `"updater_min_version": null`} {
		if !strings.Contains(out, want) {
			t.Fatalf("an unmeasured version must render null; missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"min_version": "8.1.2.1"`) || strings.Contains(out, `"max_tested_version": "n/a"`) || strings.Contains(out, `"updater_min_version": "0.0.0"`) {
		t.Fatalf("fabricated defaults leaked into the manifest:\n%s", out)
	}

	// explicit env ⇒ passed through verbatim (the one honest source)
	out2 := run(append(versionlessEnv(),
		"NT8_MIN=8.1.2.1", "NT8_MAX_TESTED=8.1.2.1", "UPDATER_MIN=1.4.0"))
	for _, want := range []string{`"min_version": "8.1.2.1"`, `"max_tested_version": "8.1.2.1"`, `"updater_min_version": "1.4.0"`} {
		if !strings.Contains(out2, want) {
			t.Fatalf("an explicitly provided version must render verbatim; missing %q in:\n%s", want, out2)
		}
	}
}
