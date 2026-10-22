package updaterworker

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"vl/internal/updaterjob"
)

// ── m355 build-id ordering (never-downgrade) ────────────────────────────────

func TestBuildIDPartsAndCompare(t *testing.T) {
	cases := []struct {
		id                  string
		wantY, wantM, wantD int
		wantSuffix          string
		ok                  bool
	}{
		{"2026-10-21-c", 2026, 10, 21, "c", true},
		{"2026-10-21", 2026, 10, 21, "", true},
		{"2026-10-21-aa", 2026, 10, 21, "aa", true},
		{"2026-1-21-c", 0, 0, 0, "", false},   // not zero-padded
		{"2026-10-21-c!", 0, 0, 0, "", false}, // bad suffix char
		{"2026-10-21-", 0, 0, 0, "", false},   // trailing dash, empty suffix
		{"2026-13-21-c", 0, 0, 0, "", false},  // month out of range
		{"n/a", 0, 0, 0, "", false},
		{"", 0, 0, 0, "", false},
	}
	for _, c := range cases {
		y, m, d, suffix, ok := buildIDParts(c.id)
		if ok != c.ok || (ok && (y != c.wantY || m != c.wantM || d != c.wantD || suffix != c.wantSuffix)) {
			t.Errorf("buildIDParts(%q) = (%d,%d,%d,%q,%v), want (%d,%d,%d,%q,%v)", c.id, y, m, d, suffix, ok, c.wantY, c.wantM, c.wantD, c.wantSuffix, c.ok)
		}
	}
}

func TestCompareBuildIDOrdersDatesThenSuffix(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"2026-10-21-c", "2026-10-18-a", 1, true},   // later day
		{"2026-10-18-a", "2026-10-21-c", -1, true},  // earlier day
		{"2026-10-21-c", "2026-10-21-c", 0, true},   // equal
		{"2026-10-21-b", "2026-10-21-a", 1, true},   // same day, later suffix
		{"2026-10-21-a", "2026-10-21-aa", -1, true}, // "-aa" follows "-z"-style: longer is later
		{"2026-10-21", "2026-10-21-a", -1, true},    // no suffix is older
		{"2026-10-21-a", "2026-10-21", 1, true},
		{"2026-10-21-c", "n/a", 0, false}, // unrankable
		{"x", "2026-10-21-c", 0, false},
	}
	for _, c := range cases {
		got, ok := compareBuildID(c.a, c.b)
		if got != c.want || ok != c.ok {
			t.Errorf("compareBuildID(%q,%q) = (%d,%v), want (%d,%v)", c.a, c.b, got, ok, c.want, c.ok)
		}
		if c.ok {
			if n := buildIDNewer(c.a, c.b); n != (c.want > 0) {
				t.Errorf("buildIDNewer(%q,%q) = %v, want %v", c.a, c.b, n, c.want > 0)
			}
		} else if buildIDNewer(c.a, c.b) {
			t.Errorf("buildIDNewer(%q,%q) must be false when unrankable", c.a, c.b)
		}
	}
}

// ── m355 verdict-by-source-sha (the verified installed release) ─────────────

func writeVerdictFile(t *testing.T, dataDir, releaseID, sha, dir string) {
	t.Helper()
	vdir := filepath.Join(dataDir, "updater", "verdicts")
	if err := os.MkdirAll(vdir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"schema":1,"release_id":` + strconv.Quote(releaseID) + `,"source_sha":` + strconv.Quote(sha) +
		`,"release_dir":` + strconv.Quote(dir) + `,"signer":"release","signer_fingerprint":"SHA256:` +
		strings.Repeat("A", 42) + `0","hashalg":"sha512","manifest_sha256":"` + strings.Repeat("ab", 32) +
		`","artifacts":5,"verified_at":"2026-10-21T00:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(vdir, releaseID+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// verdictForSourceSHA returns the verdict whose source_sha matches; ok=false
// when none does. MUTANT: match on release_dir base instead of source_sha →
// a mismatched verdict is returned → RED.
func TestVerdictForSourceSHA(t *testing.T) {
	dataDir := t.TempDir()
	writeVerdictFile(t, dataDir, "v1.0.0", strings.Repeat("a1", 20), filepath.Join(dataDir, "releases", strings.Repeat("a1", 20)))
	writeVerdictFile(t, dataDir, "v2.0.0", strings.Repeat("b2", 20), filepath.Join(dataDir, "releases", strings.Repeat("b2", 20)))

	v, ok, err := verdictForSourceSHA(dataDir, strings.Repeat("b2", 20))
	if err != nil || !ok {
		t.Fatalf("verdictForSourceSHA(b2…) = (%+v,%v,%v), want found", v, ok, err)
	}
	if v.ReleaseID != "v2.0.0" || v.SourceSHA != strings.Repeat("b2", 20) {
		t.Fatalf("returned the wrong verdict: %+v", v)
	}
	if _, ok, err := verdictForSourceSHA(dataDir, strings.Repeat("c3", 20)); err != nil || ok {
		t.Fatalf("verdictForSourceSHA(c3…) = ok=%v err=%v, want not-found", ok, err)
	}
	// An unreadable verdict (wrong mode) is skipped, never an error.
	writeVerdictFile(t, dataDir, "v3.0.0", strings.Repeat("d4", 20), filepath.Join(dataDir, "releases", strings.Repeat("d4", 20)))
	if err := os.Chmod(filepath.Join(dataDir, "updater", "verdicts", "v3.0.0.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := verdictForSourceSHA(dataDir, strings.Repeat("d4", 20)); err != nil || ok {
		t.Fatalf("an unreadable verdict must be skipped: ok=%v err=%v", ok, err)
	}
}

// PIN (m355, rule 2 + 3): the catch-up's expected build is the VERIFIED
// artifact's manifest build, persisted at creation — never oldInstallBuildID().
// MUTANT: wantAddonBuild returns oldInstallBuildID() for a catch-up → the
// stale-install build leaks through → RED.
func TestWantAddonBuildForCatchupIsThePersistedExpectedBuild(t *testing.T) {
	j := updaterjob.Job{Catchup: true, Addon: &updaterjob.AddonState{ExpectedBuild: "2026-10-21-c"}}
	w := &Worker{cfg: Config{Target: Target{InstallDir: t.TempDir()}}}
	// The install tree declares a DIFFERENT (stale) build; it must be ignored.
	if err := os.MkdirAll(filepath.Join(w.cfg.Target.InstallDir, "ninjascript"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.cfg.Target.InstallDir, "ninjascript", "VLTraderTCPClient.cs"),
		[]byte(`private const string VL_BUILD_ID = "2026-10-18-a";`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := w.wantAddonBuild(j); got != "2026-10-21-c" {
		t.Fatalf("wantAddonBuild(catchup) = %q, want the persisted expected build 2026-10-21-c", got)
	}
}
