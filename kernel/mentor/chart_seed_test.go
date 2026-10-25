package mentor

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// m422 (C5) pins. Production call sites: Evaluator.Tick (what the seed must
// never change) and Evaluator.SeedChartDisplay (the boot display seed the
// trader calls after the other boot seeds).

// TestSeedChartDisplayNeverAuthors — pin (a). Two evaluators seeded the same
// way over the real 10-13/10-14 tape; one also runs SeedChartDisplay. Three
// hours of Ticks later every tick's intents AND the full State JSON are
// byte-identical. Mutant: SeedChartDisplay calling seededLevels (advances the
// 1m/1H watermarks) or latchBoxes / carryIntradayLines / trackTrendlines → RED.
func TestSeedChartDisplayNeverAuthors(t *testing.T) {
	all := loadSeedStateFixture(t)
	full := ssTape(all, true, 1<<62)
	i0 := -1
	for i, b := range full {
		if b.OpenTime >= ssRoll {
			i0 = i
			break
		}
	}
	if i0 < 1 {
		t.Fatal("the fixture does not cover the 10-13 17:00 roll")
	}
	seedNow := BarCloseInstant(full[i0-1])
	plain := ssSeed(full, seedNow, ssCPI)
	seeded := ssSeed(full, seedNow, ssCPI)
	seeded.SeedChartDisplay(lrWindow(full, i0-1), seedNow)
	if len(seeded.State.ChartLevels) == 0 || len(seeded.State.ChartEMA) == 0 || seeded.State.ChartSeededAt != seedNow {
		t.Fatalf("the seed drew nothing: levels=%d ema=%d at=%d", len(seeded.State.ChartLevels), len(seeded.State.ChartEMA), seeded.State.ChartSeededAt)
	}
	if len(seeded.State.Levels) != 0 {
		t.Fatalf("State.Levels must stay empty (limits R1 reads empty as no Tick yet): %d", len(seeded.State.Levels))
	}
	ticks := 0
	for i := i0; i < len(full) && ticks < 180; i++ {
		now := BarCloseInstant(full[i])
		plain.Cfg.PrintWindows, seeded.Cfg.PrintWindows = ssCPI, ssCPI
		a := plain.Tick(lrWindow(full, i), now)
		b := seeded.Tick(lrWindow(full, i), now)
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if string(ja) != string(jb) {
			t.Fatalf("tick %d (%s CT): intents differ with the display seed:\n%s\n%s", ticks, lrCT(time.UnixMilli(full[i].OpenTime)), ja, jb)
		}
		sa, _ := json.Marshal(plain.State)
		sb, _ := json.Marshal(seeded.State)
		if string(sa) != string(sb) {
			t.Fatalf("tick %d (%s CT): State JSON differs with the display seed", ticks, lrCT(time.UnixMilli(full[i].OpenTime)))
		}
		if seeded.State.ChartSeededAt != 0 {
			t.Fatalf("tick %d: the first real Tick must clear ChartSeededAt", ticks)
		}
		ticks++
	}
	if ticks < 180 {
		t.Fatalf("fixture too short: %d ticks", ticks)
	}
}

// TestChartDisplayFieldsAreReadOnlyByDisplayCode — pin (b). Nothing that
// trades may read State.ChartLevels / ChartTrendlines / ChartEMA. AST scan of
// every non-test .go file in the repo: a selector of those names is allowed
// only (1) as an assignment TARGET in eval.go / chart_seed.go (the writers) or
// (2) anywhere in the display readers arm_snapshot.go and
// trader/mentor_truth_panel.go. Mutant: a read in any other file → RED.
func TestChartDisplayFieldsAreReadOnlyByDisplayCode(t *testing.T) {
	names := map[string]bool{"ChartLevels": true, "ChartTrendlines": true, "ChartEMA": true}
	writers := map[string]bool{"kernel/mentor/eval.go": true, "kernel/mentor/chart_seed.go": true}
	readers := map[string]bool{"kernel/mentor/arm_snapshot.go": true, "trader/mentor_truth_panel.go": true}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var bad []string
	scanned := 0
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "web", "vendor", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return nil
		}
		scanned++
		if readers[rel] {
			return nil
		}
		lhs := map[*ast.SelectorExpr]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			if as, ok := n.(*ast.AssignStmt); ok {
				for _, l := range as.Lhs {
					if se, ok := l.(*ast.SelectorExpr); ok {
						lhs[se] = true
					}
				}
			}
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			se, ok := n.(*ast.SelectorExpr)
			if !ok || !names[se.Sel.Name] {
				return true
			}
			if writers[rel] && lhs[se] {
				return true
			}
			bad = append(bad, rel+":"+strconv.Itoa(fset.Position(se.Pos()).Line)+" "+se.Sel.Name)
			return true
		})
		return nil
	})
	if scanned < 50 {
		t.Fatalf("lint lost its teeth: scanned only %d files from %s", scanned, root)
	}
	if len(bad) > 0 {
		t.Fatalf("display-only fields read outside the allow-list (they must never feed trading):\n%s", strings.Join(bad, "\n"))
	}
}
