package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CLASS 250. VITE_GUIDE_BUILT_REV is a REQUIRED build input: web/vite.config.ts
// refuses a production build without it. It shipped with a NEGATIVE proof (the
// refusal fires) and no POSITIVE one (every producer of the artifact supplies
// it), so the first producer to run — CI — failed on five checks.
//
// A guard is only as good as the census of its producers. This test IS that
// census, and it fails when a new producer appears without the input, which is
// the only way the next person to add one finds out before CI does.
//
// It deliberately does not try to parse YAML or Dockerfiles: it asks, for every
// place that runs a production frontend build, whether the input is supplied
// near enough to reach it (a step's `env:`, an `ARG`/`ENV` above the `RUN`, or
// on the command line itself).

// scanLookback is how far above an invocation the input may be declared: a
// step's env: block or an ARG/ENV pair sits within a few lines, never 25.
const scanLookback = 25

const guideRevVar = "VITE_GUIDE_BUILT_REV"

// notAProducer lists files that CONTAIN the string "npm run build" without ever
// running one. Each entry says why, because an unexplained exemption is how a
// real producer gets waved through later (CLASS 242's lesson about exemptions).
var notAProducer = map[string]string{
	// Emits advice text into a PR comment; the string is documentation for a
	// human, not a build this repo performs.
	".github/workflows/pr-checks-comment.yml": "generates comment text, runs no build",
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Dir(wd) // deploy/ -> repo root
}

func TestEveryProductionFrontendBuildSuppliesTheGuideRev(t *testing.T) {
	root := repoRoot(t)
	var scanned, missing []string

	walk := func(dir string, keep func(string) bool) {
		_ = filepath.Walk(filepath.Join(root, dir), func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !keep(p) {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			if why, skip := notAProducer[filepath.ToSlash(rel)]; skip {
				t.Logf("skipping %s — %s", rel, why)
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			lines := strings.Split(string(b), "\n")
			for i, ln := range lines {
				if !strings.Contains(ln, "npm run build") {
					continue
				}
				// A comment mentioning the command is not an invocation of it.
				// (This test failed on its OWN explanatory comments first.)
				if strings.HasPrefix(strings.TrimSpace(ln), "#") {
					continue
				}
				scanned = append(scanned, fmt.Sprintf("%s:%d", rel, i+1))
				lo := i - scanLookback
				if lo < 0 {
					lo = 0
				}
				if strings.Contains(strings.Join(lines[lo:i+1], "\n"), guideRevVar) {
					continue
				}
				missing = append(missing, fmt.Sprintf("%s:%d  %s", rel, i+1, strings.TrimSpace(ln)))
			}
			return nil
		})
	}

	walk(".github/workflows", func(p string) bool { return strings.HasSuffix(p, ".yml") })
	walk("docker", func(p string) bool { return strings.Contains(filepath.Base(p), "Dockerfile") })
	walk("deploy", func(p string) bool { return strings.HasSuffix(p, ".sh") })

	if len(scanned) == 0 {
		t.Fatal("scanned no production build call sites at all — the census is looking in the wrong place")
	}
	if len(missing) > 0 {
		t.Fatalf("%d production frontend build(s) do not supply %s (of %d scanned):\n  %s\n\n"+
			"Each must get it from the step's env:, an ARG/ENV above the RUN, or the command line. "+
			"web/vite.config.ts REFUSES a production build without it, so an unsupplied producer fails at build time.",
			len(missing), guideRevVar, len(scanned), strings.Join(missing, "\n  "))
	}
}

// TestComposeSuppliesTheGuideRevToTheFrontendImage covers the producer that
// runs no `npm run build` itself: compose builds Dockerfile.frontend, so the
// input has to cross the build-args boundary or the image is built unstamped.
func TestComposeSuppliesTheGuideRevToTheFrontendImage(t *testing.T) {
	root := repoRoot(t)
	hits, _ := filepath.Glob(filepath.Join(root, "docker-compose*.yml"))
	if len(hits) == 0 {
		t.Skip("no docker-compose files in this tree")
	}
	var bad []string
	for _, p := range hits {
		rel, _ := filepath.Rel(root, p)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		s := string(b)
		if !strings.Contains(s, "Dockerfile.frontend") {
			continue
		}
		if !strings.Contains(s, guideRevVar) {
			bad = append(bad, rel)
		}
	}
	if len(bad) > 0 {
		t.Fatalf("%d compose file(s) build Dockerfile.frontend without passing %s — they would build a "+
			"guide whose revision cannot be checked, or fail at build time:\n  %s",
			len(bad), guideRevVar, strings.Join(bad, "\n  "))
	}
}

// TestWorkflowsThatBuildTheFrontendImagePassTheBuildArg is the third boundary:
// a workflow can build the frontend image without ever typing "npm run build".
func TestWorkflowsThatBuildTheFrontendImagePassTheBuildArg(t *testing.T) {
	root := repoRoot(t)
	var bad []string
	hits, _ := filepath.Glob(filepath.Join(root, ".github/workflows/*.yml"))
	for _, p := range hits {
		rel, _ := filepath.Rel(root, p)
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s := string(b)
		if !strings.Contains(s, "Dockerfile.frontend") {
			continue
		}
		if !strings.Contains(s, guideRevVar) {
			bad = append(bad, rel)
		}
	}
	if len(bad) > 0 {
		t.Fatalf("%d workflow(s) build Dockerfile.frontend without a %s build-arg:\n  %s",
			len(bad), guideRevVar, strings.Join(bad, "\n  "))
	}
}
