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
// This census reads the SOURCE of the WHOLE MODULE (m471 L3-4: it used to read
// trader/ only, so manager/'s boot_duplicate_symbol_account P0 - a trader not
// started at boot - was neither pushed nor listed and CI stayed green). A P0
// alert is raised three ways, and each is found:
//   - an emitAlert call whose level is "P0" (or not a literal, so it may be P0):
//     pushed when ownerPushKind lists its kind;
//   - a store.AlertDB{Level: "P0", Kind: ...} literal written anywhere else
//     (straight through st.Alert().Emit, or a row handed to maybeOwnerPush):
//     pushed when the function that builds it also calls ownerpush.Push (or
//     maybeOwnerPush) and the kind is classed P0;
//   - an Alert().Emit call whose argument is not such a literal: a failure
//     (the census cannot read its kind) unless it is emitAlert itself.
// Every kind found is pushed or listed below in notPushed with the reason it is
// not. A new P0 kind in neither fails CI.
//
// MUTANT (shown RED): remove "ai-payment" from ownerPushKind -> "P0 kind
// \"ai-payment\" (trader/auto_trader_ai402.go) is neither pushed to the owner
// nor listed in notPushed" -> RED. MUTANT 2 (m471): drop the ownerpush.Push in
// manager's refuseBootDuplicate -> "P0 kind \"boot_duplicate_symbol_account\"
// (manager/trader_manager.go) is neither pushed ..." -> RED.

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
	kind, file string // file is module-relative
	line       int
	maybe      bool // level is not a literal: P0 on some path
	viaEmit    bool // an emitAlert call (pushed iff ownerPushKind)
	pushedHere bool // a direct row: the writing function also calls ownerpush.Push / maybeOwnerPush
}

// censusSkipDirs are not part of the bot process's Go source.
var censusSkipDirs = map[string]bool{"web": true, "node_modules": true, "vendor": true, "testdata": true}

// censusNotTheBot are Go files that build an AlertDB the census cannot read and
// that never run in the bot process, with WHY.
var censusNotTheBot = map[string]string{
	"cmd/sandbox-seed/main.go": "a separate dev binary that seeds demo alert rows into a sandbox DB; never runs in the bot",
}

// censusRawEmitOK are the functions allowed to pass a non-literal row to
// Alert().Emit: emitAlert itself (every caller of it is censused).
var censusRawEmitOK = map[string]bool{"trader/auto_trader_alerts.go:emitAlert": true}

// isOwnerPushCall reports an ownerpush.Push call, or a call of the trader's
// maybeOwnerPush (which ends in ownerpush.Push for a pushed kind).
func isOwnerPushCall(n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name == "maybeOwnerPush" {
		return true
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && sel.Sel.Name == "Push" && x.Name == "ownerpush"
}

// p0EmitSites parses every non-test .go file of the module under root and
// returns each P0 alert site (see the census comment). A kind must be a string
// literal or a package-level string constant; anything else is a failure,
// because the census cannot read it. dirs collects the module-relative
// directories parsed, so the caller can prove the walk reached them.
func p0EmitSites(t *testing.T, root string, dirs map[string]bool) []p0Site {
	t.Helper()
	type pkgFiles struct {
		files  []*ast.File
		paths  []string
		consts map[string]string
	}
	fset := token.NewFileSet()
	byDir := map[string]*pkgFiles{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || censusSkipDirs[d.Name()]) {
				return filepath.SkipDir
			}
			if path != root {
				if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
					return filepath.SkipDir // a nested module is not this binary
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		dir := filepath.Dir(path)
		if rel, rerr := filepath.Rel(root, dir); rerr == nil {
			dirs[filepath.ToSlash(rel)] = true
		}
		pf := byDir[dir]
		if pf == nil {
			pf = &pkgFiles{consts: map[string]string{}}
			byDir[dir] = pf
		}
		pf.files = append(pf.files, f)
		pf.paths = append(pf.paths, path)
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
		strOf := func(e ast.Expr) (string, bool) {
			switch k := e.(type) {
			case *ast.BasicLit:
				if k.Kind != token.STRING {
					return "", false
				}
				s, uerr := strconv.Unquote(k.Value)
				return s, uerr == nil
			case *ast.Ident:
				s, ok := pf.consts[k.Name]
				return s, ok
			}
			return "", false
		}
		for fi, f := range pf.files {
			rel, _ := filepath.Rel(root, pf.paths[fi])
			rel = filepath.ToSlash(rel)
			if _, skip := censusNotTheBot[rel]; skip {
				continue
			}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				fnKey := rel + ":" + fn.Name.Name
				pushes := false
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if isOwnerPushCall(n) {
						pushes = true
					}
					return !pushes
				})
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					switch x := n.(type) {
					case *ast.CallExpr:
						sel, ok := x.Fun.(*ast.SelectorExpr)
						if !ok {
							return true
						}
						pos := fset.Position(x.Pos())
						switch sel.Sel.Name {
						case "emitAlert":
							if len(x.Args) < 2 {
								return true
							}
							maybe := false
							if lv, ok := x.Args[0].(*ast.BasicLit); ok {
								if lv.Value != `"P0"` {
									return true
								}
							} else {
								maybe = true // a level computed at run time can be P0
							}
							kind, ok := strOf(x.Args[1])
							if !ok || kind == "" {
								t.Errorf("%s:%d: emitAlert kind is not a string literal or a package string constant - the census cannot read it", rel, pos.Line)
								return true
							}
							sites = append(sites, p0Site{kind: kind, file: rel, line: pos.Line, maybe: maybe, viaEmit: true})
						case "Emit":
							inner, ok := sel.X.(*ast.CallExpr)
							if !ok {
								return true
							}
							if is, ok := inner.Fun.(*ast.SelectorExpr); !ok || is.Sel.Name != "Alert" || len(x.Args) != 1 {
								return true
							}
							arg := x.Args[0]
							if u, ok := arg.(*ast.UnaryExpr); ok && u.Op == token.AND {
								arg = u.X
							}
							if _, ok := arg.(*ast.CompositeLit); !ok && !censusRawEmitOK[fnKey] {
								t.Errorf("%s:%d: Alert().Emit is passed a row the census cannot read (not an AlertDB literal) - write the literal at the call, or raise it through emitAlert", rel, pos.Line)
							}
						}
					case *ast.CompositeLit:
						typ := ""
						switch tt := x.Type.(type) {
						case *ast.SelectorExpr:
							typ = tt.Sel.Name
						case *ast.Ident:
							typ = tt.Name
						}
						if typ != "AlertDB" || censusRawEmitOK[fnKey] {
							return true
						}
						var levelExpr, kindExpr ast.Expr
						for _, e := range x.Elts {
							kv, ok := e.(*ast.KeyValueExpr)
							if !ok {
								continue
							}
							if k, ok := kv.Key.(*ast.Ident); ok {
								switch k.Name {
								case "Level":
									levelExpr = kv.Value
								case "Kind":
									kindExpr = kv.Value
								}
							}
						}
						if levelExpr == nil {
							return true // no level: a query model or a lookup key, never a raised row
						}
						maybe := false
						if lv, ok := strOf(levelExpr); ok {
							if lv != "P0" {
								return true
							}
						} else {
							maybe = true
						}
						pos := fset.Position(x.Pos())
						kind := ""
						if kindExpr != nil {
							kind, _ = strOf(kindExpr)
						}
						if kind == "" {
							t.Errorf("%s:%d: a P0 AlertDB literal whose Kind the census cannot read (not a string literal or a package string constant)", rel, pos.Line)
							return true
						}
						sites = append(sites, p0Site{kind: kind, file: rel, line: pos.Line, maybe: maybe, pushedHere: pushes})
					}
					return true
				})
			}
		}
	}
	return sites
}

func TestOwnerPushP0Census(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(wd) // this file lives in <module>/trader
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("census root %s is not the module root: %v", root, err)
	}
	dirs := map[string]bool{}
	sites := p0EmitSites(t, root, dirs)
	// the parse itself must have reached the module (a walk that reads nothing
	// would pass every rule below)
	for _, d := range []string{".", "trader", "manager", "internal/datatruth", "internal/datareport", "internal/ownerpush"} {
		if !dirs[d] {
			t.Fatalf("census did not parse %s/ - the walk is broken", d)
		}
	}
	if len(sites) < 25 {
		t.Fatalf("census found only %d P0 sites - the walk is broken", len(sites))
	}
	type kindInfo struct {
		file    string
		pushed  bool
		viaEmit bool
	}
	kinds := map[string]*kindInfo{}
	for _, s := range sites {
		pushed := false
		if s.viaEmit {
			pushed = ownerPushKind("P0", s.kind)
		} else {
			pushed = s.pushedHere && ownerpush.IsP0Key(s.kind+"|t1|e")
		}
		ki := kinds[s.kind]
		if ki == nil {
			kinds[s.kind] = &kindInfo{file: s.file, pushed: pushed, viaEmit: s.viaEmit}
			continue
		}
		// one site of a kind that does not push is enough to lose it
		ki.pushed = ki.pushed && pushed
		ki.viaEmit = ki.viaEmit || s.viaEmit
	}
	for _, must := range []string{"ai-payment", "ai-failing", "feed-down", "trader_panic", "goroutine_panic", "daily-loss", "untracked-position", alertKindProtectionUnverified, "boot_duplicate_symbol_account"} {
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
		ki := kinds[k]
		why, listed := notPushed[k]
		switch {
		case !ki.pushed && !listed:
			t.Errorf("P0 kind %q (%s) is neither pushed to the owner nor listed in notPushed - push it (ownerPushKind, or ownerpush.Push where the row is written) or list it with the reason", k, ki.file)
		case ki.pushed && listed:
			t.Errorf("P0 kind %q is pushed AND listed in notPushed (%q) - drop the stale entry", k, why)
		case listed && strings.TrimSpace(why) == "":
			t.Errorf("P0 kind %q is listed in notPushed with no reason", k)
		}
		if !ki.pushed || !ki.viaEmit {
			continue
		}
		// pushed via emitAlert => an ops kind: emitAlert writes no row (so pushes
		// nothing) for a trader without a day plan unless the kind is an ops kind.
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
			t.Errorf("notPushed lists %q but no P0 alert site raises it any more - drop the stale entry", k)
		}
	}
}
