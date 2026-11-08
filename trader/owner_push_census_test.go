package trader

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"vl/internal/ownerpush"
	"vl/store"
)

// m469 — THE P0 CENSUS. On 11-03..11-06 a trader's AI answered 402 for 2.5
// days; the "ai-payment" P0 was written to the alert center on the first 402
// and never reached the owner, because ownerPushKind did not list it. Nothing
// failed: a P0 that is not pushed looks exactly like a P0 that is.
//
// This census reads the SOURCE: every emitAlert call under trader/ (non-test
// files) whose level is "P0" - or is not a literal, so it may be P0 - names a
// kind that is either pushed to the owner (ownerPushKind) or listed below in
// notPushed with the reason it is not. A new P0 kind in neither fails CI.
//
// MUTANT (shown RED): remove "ai-payment" from ownerPushKind -> "P0 kind
// \"ai-payment\" (trader/auto_trader_ai402.go) is neither pushed to the owner
// nor listed in notPushed" -> RED.

// notPushed is every P0 kind that stays in the alert center only, with WHY.
// A reason is a sentence the owner could read; "OPEN" marks a kind whose push
// is an owner decision not yet taken (m469 left it as it was).
var notPushed = map[string]string{
	"plan-died": "plan content, not ops: a session's plan died. 4 rows to 11-07. Pinned in-app only by " +
		"TestOwnerPushKindsAreExactlyTheOpsP0Set; the m469 plan leaves it unpushed until the owner asks.",
	"read-fail": "plan content, not ops: the planner's read failed closed. 53 rows to 11-07 - too noisy to page; " +
		"pinned unpushed by run_owner_alert_episode_test.go; the m469 plan leaves it unpushed until the owner asks.",
	"regime-dark": "plan content (P0 only when the plan was built half-blind): a decision-quality note on one plan, " +
		"same family as plan-died.",
	"halt": "the consecutive-loss halt is the owner's own risk rule working as set (no new entries until the next " +
		"session), not a fault; one per session day in the alert center.",
	"approval": "approval_required is a mode the owner turns on himself (default OFF); the entry waits for HIS " +
		"approval in the app, which is where he approves it.",
	"boot-integrity": "the boot message the owner is pushed at every start already says when trading is refused " +
		"at boot (ownerpush.BootMessage, refused=true).",
	"stop-move-not-applied-twice": "the second miss of an episode whose first miss was already pushed as " +
		"stop-move-not-applied (mentor_exit_drive.go, same signal and leg).",
	"calendar-slice-missing": "OPEN: fail-closed (the session is blacked out around the fallback times, so " +
		"nothing unsafe trades) and it repeats daily until the feed is fixed; 15 rows to 11-07. Owner decision.",
	"mentor-restart-rebuild": "OPEN: once per position at a restart (manage by hand); the owner has just " +
		"restarted the bot. Owner decision.",
	"mentor-swing-restart": "OPEN: once per swing at a restart (manage by hand); the owner has just restarted " +
		"the bot. Owner decision.",
	"broker_book": "OPEN: arm placement is refused while the NT8 order book is dark (fail closed); FEED DOWN " +
		"covers a dead NT8 only when bars stop too. Owner decision.",
	"ordered_owner_conflict": "OPEN: raised when a trader is started onto an account another trader already " +
		"owns - the start is refused while the owner is in the app. Owner decision.",
	"pep_mode_conflict": "OPEN: pep mode is OFF on every trader; the conflict refuses the start while the " +
		"owner is in the app. Owner decision with the pep wave.",
	"pep_flat_failed": "OPEN: pep mode is OFF on every trader (owner 10-30); a pep position still open after " +
		"the flat attempts. Owner decision with the pep wave.",
}

type p0Site struct {
	kind, file string
	line       int
	maybe      bool // level is not a literal: P0 on some path
}

// p0EmitSites parses every non-test .go file under root and returns each
// emitAlert call whose level is "P0" or not a literal. A kind must be a string
// literal or a package-level string constant; anything else is a failure,
// because the census cannot read it.
func p0EmitSites(t *testing.T, root string) []p0Site {
	t.Helper()
	type pkgFiles struct {
		files  []*ast.File
		consts map[string]string
	}
	fset := token.NewFileSet()
	byDir := map[string]*pkgFiles{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		dir := filepath.Dir(path)
		pf := byDir[dir]
		if pf == nil {
			pf = &pkgFiles{consts: map[string]string{}}
			byDir[dir] = pf
		}
		pf.files = append(pf.files, f)
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if s, uerr := strconv.Unquote(lit.Value); uerr == nil {
							pf.consts[name.Name] = s
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("census walk: %v", err)
	}
	var sites []p0Site
	for _, pf := range byDir {
		for _, f := range pf.files {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "emitAlert" || len(call.Args) < 2 {
					return true
				}
				pos := fset.Position(call.Pos())
				maybe := false
				switch lv := call.Args[0].(type) {
				case *ast.BasicLit:
					if lv.Value != `"P0"` {
						return true
					}
				default:
					maybe = true // a level computed at run time can be P0
				}
				var kind string
				switch k := call.Args[1].(type) {
				case *ast.BasicLit:
					kind, _ = strconv.Unquote(k.Value)
				case *ast.Ident:
					kind = pf.consts[k.Name]
				}
				if kind == "" {
					t.Errorf("%s:%d: emitAlert kind is not a string literal or a package string constant - the census cannot read it", pos.Filename, pos.Line)
					return true
				}
				sites = append(sites, p0Site{kind: kind, file: pos.Filename, line: pos.Line, maybe: maybe})
				return true
			})
		}
	}
	return sites
}

func TestOwnerPushP0Census(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sites := p0EmitSites(t, wd)
	// the parse itself must have found the production sites (a walk that reads
	// nothing would pass every rule below)
	if len(sites) < 25 {
		t.Fatalf("census found only %d P0 emitAlert sites - the walk is broken", len(sites))
	}
	kinds := map[string]string{} // kind -> first site
	for _, s := range sites {
		if _, ok := kinds[s.kind]; !ok {
			kinds[s.kind] = filepath.Base(s.file)
		}
	}
	for _, must := range []string{"ai-payment", "ai-failing", "feed-down", "trader_panic", "goroutine_panic", "daily-loss", "untracked-position", alertKindProtectionUnverified} {
		if _, ok := kinds[must]; !ok {
			t.Fatalf("census did not find the %q P0 site - the walk is broken", must)
		}
	}
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		pushed := ownerPushKind("P0", k)
		why, listed := notPushed[k]
		switch {
		case !pushed && !listed:
			t.Errorf("P0 kind %q (trader/%s) is neither pushed to the owner nor listed in notPushed - push it (ownerPushKind) or list it with the reason", k, kinds[k])
		case pushed && listed:
			t.Errorf("P0 kind %q is pushed AND listed in notPushed (%q) - drop the stale entry", k, why)
		case listed && strings.TrimSpace(why) == "":
			t.Errorf("P0 kind %q is listed in notPushed with no reason", k)
		}
		if !pushed {
			continue
		}
		// pushed => an ops kind: emitAlert writes no row (so pushes nothing) for
		// a trader without a day plan unless the kind is an ops kind.
		if !isOpsAlertKind(k) {
			t.Errorf("pushed P0 kind %q is not an ops kind - a trader with no day plan (every mentor trader) never writes the row, so it is never pushed", k)
		}
		// pushed => classed P0 by the channel: never dropped by an outage, never
		// held back by the all-kinds backstop.
		key := ownerPushKey(&store.AlertDB{Kind: k, TraderID: "t1", EventID: "e"})
		if !ownerpush.IsP0Key(key) {
			t.Errorf("pushed P0 kind %q is not classed P0 by ownerpush (key %q): an outage would discard it after 90 minutes", k, key)
		}
	}
	for k := range notPushed {
		if _, ok := kinds[k]; !ok {
			t.Errorf("notPushed lists %q but no P0 emitAlert site raises it any more - drop the stale entry", k)
		}
	}
}
