package updateauth

import (
	"os"
	"path/filepath"
	"testing"

	"nofx/internal/censuswalk"
)

// PIN (M3 fold M5, ported red-team 4 probe RT4-1c): the update-authorization
// census walks every compiled package, including one in a directory the old
// walk skipped by NAME below the root (api/web, internal/node_modules/p,
// api/.hidden, x/testdata/y, _x, …). The planted file is red-team 4's
// api/web minter — it enrolls an arbitrary identity and mints a MAC — placed
// in a synthetic module (t.TempDir, never the real tree) and judged by the
// PRODUCTION census function.
func TestUpdateAuthCensusSeesNestedSkipNamedDirs(t *testing.T) {
	const minter = "\n\nimport (\n\t\"time\"\n\n\t\"nofx/internal/updateauth\"\n)\n\n" +
		"func RedTeamMint(dataDir string) (string, error) {\n" +
		"\tif err := updateauth.Enroll(dataDir, \"attacker-id\", \"attacker@example.test\", time.Now(), true); err != nil {\n\t\treturn \"\", err\n\t}\n" +
		"\tkey, err := updateauth.LoadDeviceKey(dataDir)\n\tif err != nil {\n\t\treturn \"\", err\n\t}\n" +
		"\tg, err := updateauth.Authorize(dataDir, \"v1\", time.Now())\n\tif err != nil {\n\t\treturn \"\", err\n\t}\n" +
		"\treturn updateauth.ComputeMAC(key, g.ReleaseID, g.JobID, g.ExpiresAt)\n}\n"
	plant := func(t *testing.T, rel, pkg string) []string {
		t.Helper()
		root := t.TempDir()
		for r, body := range map[string]string{
			"go.mod":                       "module nofx\n\ngo 1.25\n",
			"internal/updateauth/paths.go": "package updateauth\n",
			"api/handler_updates.go":       "package api\n",
			rel:                            "package " + pkg + minter,
		} {
			p := filepath.Join(root, filepath.FromSlash(r))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		off, _, err := updateAuthOffenders(root)
		if err != nil {
			t.Fatal(err)
		}
		return off
	}
	want := func(rel string) []string {
		return []string{rel + ": imports nofx/internal/updateauth", rel + ": references updateauth.Enroll",
			rel + ": references updateauth.LoadDeviceKey", rel + ": references updateauth.Authorize", rel + ": references updateauth.ComputeMAC"}
	}
	check := func(t *testing.T, rel string, off []string) {
		t.Helper()
		got := map[string]bool{}
		for _, o := range off {
			got[o] = true
		}
		for _, w := range want(rel) {
			if !got[w] {
				t.Fatalf("%s enrolls and mints from the app side; census offenders %v lack %q", rel, off, w)
			}
		}
	}
	// positive control: the same file in an ordinary dir
	check(t, "api/redteam_mint.go", plant(t, "api/redteam_mint.go", "api"))
	for _, dir := range censuswalk.NestedProbeDirs() {
		t.Run(dir, func(t *testing.T) {
			rel := dir + "/redteam_mint.go"
			check(t, rel, plant(t, rel, censuswalk.PackageName(dir)))
		})
	}
}
