package trader

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"vl/store"
)

// UI-GUIDE-TRUTH (release #24): guide numbers pinned to the PRODUCTION
// resolvers that decide them — the value is read from a call, never a regex
// over Go source; only the guide sentence is matched as text.

func guideContent(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "web", "src", "guide", "content", name))
	if err != nil {
		t.Fatalf("read guide %s: %v", name, err)
	}
	return string(b)
}

func guideNumber(t *testing.T, src, pattern string) int {
	t.Helper()
	m := regexp.MustCompile(pattern).FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("guide sentence %q not found — re-anchor this pin", pattern)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// C6: planCard.ts said "default 4"; the resolver ships 2. Mutant: write
// "shipped default 4" back into the guide (or move ReplanCapDefault) → RED.
func TestGuideReplanCapIsTheResolverDefault(t *testing.T) {
	got, src := store.ResolveReplanCap(&store.DayPlanConfig{}, "NY")
	if src != store.SourceShippedDefault {
		t.Fatalf("fixture: an empty config resolved from %q, want the shipped default", src)
	}
	want := guideNumber(t, guideContent(t, "planCard.ts"),
		`shipped default (\d+) \(store/resolve_source\.go ReplanCapDefault\)`)
	if got != want {
		t.Fatalf("guide says the re-plan cap ships at %d; store.ResolveReplanCap returns %d", want, got)
	}
}

// B15: the Max levels card names the code default the planner resolves for a
// strategy with nothing saved (the owner's saved 12 is DB state, stated as
// such). Mutant: change the card's "8 (kernel.DefaultMaxLevels" → RED.
func TestGuideMaxLevelsIsTheResolverDefault(t *testing.T) {
	got, _, _, _, _ := resolveSessionPlanCfg(&store.DayPlanConfig{}, "NY")
	want := guideNumber(t, guideContent(t, "settings.ts"),
		`'(\d+) \(kernel\.DefaultMaxLevels, also the Studio default when nothing is saved\)'`)
	if got != want {
		t.Fatalf("guide says max levels defaults to %d; resolveSessionPlanCfg returns %d", want, got)
	}
	saved := 12
	if got, _, _, _, _ := resolveSessionPlanCfg(&store.DayPlanConfig{MaxLevels: saved}, "NY"); got != saved {
		t.Fatalf("a saved max_levels %d resolved to %d — the card says a saved value wins", saved, got)
	}
}
