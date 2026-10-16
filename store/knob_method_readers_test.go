package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"vl/internal/censuswalk"
)

// ── CLEANUP BATCH 2, B1 — THE REGISTRY'S FIELD GREP MISSED METHOD READERS ─────
//
// Six wake knobs rendered "no known reader — pending verification" in the
// settings panel while being fully wired: each is read through a DayPlanConfig
// ACCESSOR METHOD (WakeOn15mZoneEnabled(), WakeMinIntervalMinutes(), …) and the
// 2026-09-03 sweep grepped for the FIELD. The registry's own note said a
// method-based reader would not appear. This is the method-level detector that
// note asked for, as code: for every candidate-unverified knob, find the struct
// field behind its json leaf, find methods on the owning type whose body reads
// that field, and find production call sites of those methods. A candidate with
// a method reader is misclassified, and this pin names the reader.

// knobFieldOwner walks StrategyConfig by reflection and returns, for a json
// leaf, every (ownerTypeName, GoFieldName) that carries it.
func knobFieldOwners(leaf string) [][2]string {
	var out [][2]string
	seen := map[string]bool{}
	var walk func(t reflect.Type, depth int)
	walk = func(t reflect.Type, depth int) {
		if depth > 6 || t == nil {
			return
		}
		for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || seen[t.Name()] {
			return
		}
		seen[t.Name()] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			ft := f.Type
			for ft.Kind() == reflect.Ptr || ft.Kind() == reflect.Slice {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				walk(ft, depth+1)
				continue
			}
			if tag == leaf {
				out = append(out, [2]string{t.Name(), f.Name})
			}
		}
	}
	walk(reflect.TypeOf(StrategyConfig{}), 0)
	return out
}

// repoGoFiles lists every non-test .go file under the module root via
// censuswalk.NonTestGoFiles (CLASS 258: skip names apply only as direct
// children of the module root).
func repoGoFiles(t *testing.T, rootArg string) (root string, files []string) {
	t.Helper()
	root = rootArg
	fs, err := censuswalk.NonTestGoFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		files = append(files, f.Path)
	}
	return root, files
}

// receiverTypeName returns the bare type name of a method receiver ("*DayPlanConfig" → "DayPlanConfig").
func receiverTypeName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	switch t := fd.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// methodReadsField reports whether a method body contains <recv>.<field>.
func methodReadsField(fd *ast.FuncDecl, field string) bool {
	if fd.Body == nil || fd.Recv == nil || len(fd.Recv.List) == 0 || len(fd.Recv.List[0].Names) == 0 {
		return false
	}
	recv := fd.Recv.List[0].Names[0].Name
	found := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == recv && sel.Sel.Name == field {
				found = true
			}
		}
		return !found
	})
	return found
}

// MethodReadersOfKnob is the detector. It returns "file:line" production call
// sites of accessor methods that read the knob's field, plus the accessor
// names, or nothing.
func methodReadersOfKnob(t *testing.T, leaf string) (accessors []string, callSites []string) {
	t.Helper()
	owners := knobFieldOwners(leaf)
	if len(owners) == 0 {
		return nil, nil
	}
	root, files := repoGoFiles(t, "..")
	fset := token.NewFileSet()
	parsed := map[string]*ast.File{}
	for _, f := range files {
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			continue
		}
		parsed[f] = af
	}
	// pass 1: accessor methods on the owning type that read the field
	acc := map[string]bool{}
	for _, af := range parsed {
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil {
				continue
			}
			for _, o := range owners {
				if receiverTypeName(fd) == o[0] && methodReadsField(fd, o[1]) {
					acc[fd.Name.Name] = true
				}
			}
		}
	}
	if len(acc) == 0 {
		return nil, nil
	}
	// pass 2: production call sites via go/types — the selector's selection
	// must resolve to the accessor *types.Func on the owner type in
	// vl/store; package store itself is excluded (the call chain must leave
	// store/), and method expressions / method values count (SelectorExpr
	// references, not only calls). CTO CENSUS-GUARDS 1790306266164 [31].
	var anames []string
	for a := range acc {
		anames = append(anames, a)
	}
	sort.Strings(anames)
	callSites = typedAccessorCallSites(t, root, anames, owners)
	for a := range acc {
		accessors = append(accessors, a)
	}
	sort.Strings(accessors)
	sort.Strings(callSites)
	return accessors, callSites
}

// ── PASS 2 IS go/types, NOT NAME MATCHING ─────────────────────────────────────
//
// The old pass counted every CallExpr whose selector NAME was an accessor, in
// every file. That let (a) an accessor calling an accessor inside
// store/strategy.go keep a knob "live" with no reader outside the package,
// (b) a same-named method on any other receiver count, and (c) method
// expressions — trader/effective_settings.go:842 — pass unseen. The typed
// pass resolves each selector through the toolchain and counts only
// selections whose *types.Func IS the accessor method on the owner type
// inside vl/store; package store is excluded and references count, not
// only calls. go list and go/types failures are FATAL here — the pass never
// degrades to name matching.

// readerTypeContext is the one-per-root typed fixture: the toolchain's
// package list (go list -e -json -deps -export over every walked dir), the
// export-data importer that makes vl/store resolve to ONE package object
// across all type-checks, and the parsed + type-checked caches. Shared by
// every methodReadersOfKnob call so the cost is paid once per test process.
type readerTypeContext struct {
	root     string
	fset     *token.FileSet
	imp      types.Importer
	pkgFiles map[string][]string // importPath -> absolute GoFiles (as go list reports them)
	asts     map[string]*ast.File
	checked  map[string]*types.Info
}

var (
	readerTypeCtxMu sync.Mutex
	readerTypeCtxs  = map[string]*readerTypeContext{}
)

// readerTypeContextFor builds (once) the typed fixture for root.
func readerTypeContextFor(t *testing.T, root string) *readerTypeContext {
	t.Helper()
	absRoot, aerr := filepath.Abs(root)
	if aerr != nil {
		t.Fatalf("abs(%s): %v", root, aerr)
	}
	root = absRoot
	readerTypeCtxMu.Lock()
	defer readerTypeCtxMu.Unlock()
	if c, ok := readerTypeCtxs[root]; ok {
		return c
	}
	fs, err := censuswalk.NonTestGoFiles(root)
	if err != nil {
		t.Fatalf("censuswalk: %v", err)
	}
	dirs := map[string]bool{}
	for _, f := range fs {
		dirs[filepath.Dir(f.Path)] = true
	}
	patterns := make([]string, 0, len(dirs))
	for d := range dirs {
		rel, rerr := filepath.Rel(root, d)
		if rerr != nil {
			t.Fatalf("rel(%s, %s): %v", root, d, rerr)
		}
		patterns = append(patterns, "./"+filepath.ToSlash(rel))
	}
	sort.Strings(patterns)
	cmd := exec.Command("go", append([]string{"list", "-e", "-json", "-deps", "-export", "--"}, patterns...)...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list over the walked dirs failed: %v\n%s", err, tailOf(string(out), 2000))
	}
	pkgFiles := map[string][]string{}
	exportOf := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p struct {
			ImportPath string
			Dir        string
			Export     string
			GoFiles    []string
			Error      *struct {
				Err string
			}
		}
		if derr := dec.Decode(&p); derr != nil {
			t.Fatalf("decode go list output: %v", derr)
		}
		if p.Error != nil {
			// Build constraints exclude every file on this platform (the two
			// research harnesses under docs/). Not compiled, so it cannot be
			// a production reader — skipped, not degraded.
			continue
		}
		if p.Export != "" {
			exportOf[p.ImportPath] = p.Export
		}
		if dirs[p.Dir] && len(p.GoFiles) > 0 {
			files := make([]string, 0, len(p.GoFiles))
			for _, f := range p.GoFiles {
				files = append(files, filepath.Join(p.Dir, f))
			}
			pkgFiles[p.ImportPath] = files
		}
	}
	fset := token.NewFileSet()
	lookup := func(path string) (io.ReadCloser, error) {
		e, ok := exportOf[path]
		if !ok {
			return nil, fmt.Errorf("no export data for %q — not a dependency of the walked packages", path)
		}
		return os.Open(e)
	}
	imp := importer.ForCompiler(fset, "gc", lookup)
	c := &readerTypeContext{
		root:     root,
		fset:     fset,
		imp:      imp,
		pkgFiles: pkgFiles,
		asts:     map[string]*ast.File{},
		checked:  map[string]*types.Info{},
	}
	for _, files := range pkgFiles {
		for _, f := range files {
			af, perr := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
			if perr != nil {
				t.Fatalf("parse %s: %v", f, perr)
			}
			c.asts[f] = af
		}
	}
	readerTypeCtxs[root] = c
	return c
}

// tailOf returns the last n bytes of s (for LOUD error tails).
func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// storeAccessorFuncs resolves each accessor name to its *types.Func on the
// owner type inside vl/store. Identity is by OBJECT, not name — so the
// same-named method on any other type never matches.
func storeAccessorFuncs(t *testing.T, c *readerTypeContext, accessors []string, owners [][2]string) map[*types.Func]bool {
	t.Helper()
	storePkg, err := c.imp.Import("vl/store")
	if err != nil {
		t.Fatalf("import vl/store: %v", err)
	}
	out := map[*types.Func]bool{}
	for _, o := range owners {
		obj := storePkg.Scope().Lookup(o[0])
		if obj == nil {
			t.Fatalf("owner type %s not found in the vl/store scope — pass 1 and go/types disagree", o[0])
		}
		tname, ok := obj.(*types.TypeName)
		if !ok {
			t.Fatalf("owner %s is %T, not a type name — pass 1 and go/types disagree", o[0], obj)
		}
		named, ok := tname.Type().(*types.Named)
		if !ok {
			t.Fatalf("owner %s is %T, not a named type — pass 1 and go/types disagree", o[0], tname.Type())
		}
		for _, an := range accessors {
			idx := -1
			for i := 0; i < named.NumMethods(); i++ {
				if named.Method(i).Name() == an {
					idx = i
					break
				}
			}
			if idx < 0 {
				t.Fatalf("accessor %s is not a method of %s in vl/store — pass 1 and go/types disagree", an, o[0])
			}
			out[named.Method(idx)] = true
		}
	}
	return out
}

// checkPackage type-checks one package once per root (from the go list
// GoFiles — the platform's real file set, build tags applied) and caches the
// Selections Info. Any type error is FATAL.
func (c *readerTypeContext) checkPackage(t *testing.T, importPath string) *types.Info {
	t.Helper()
	if info, ok := c.checked[importPath]; ok {
		return info
	}
	parsed := make([]*ast.File, 0, len(c.pkgFiles[importPath]))
	for _, f := range c.pkgFiles[importPath] {
		af, err := parser.ParseFile(c.fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		parsed = append(parsed, af)
	}
	info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
	cfg := &types.Config{Importer: c.imp}
	if _, err := cfg.Check(importPath, c.fset, parsed, info); err != nil {
		t.Fatalf("type-check %s failed: %v", importPath, err)
	}
	c.checked[importPath] = info
	return info
}

// typedAccessorCallSites returns every production reference to the accessors
// whose selection resolves to the accessor method itself on the owner type in
// vl/store. package store is excluded — the call chain must leave store/ —
// and a SelectorExpr counts whether it is a call, a method value or a method
// expression. go list and go/types failures are fatal (no name fallback).
func typedAccessorCallSites(t *testing.T, root string, accessors []string, owners [][2]string) []string {
	t.Helper()
	c := readerTypeContextFor(t, root)
	funcs := storeAccessorFuncs(t, c, accessors, owners)
	nameSet := map[string]bool{}
	for _, a := range accessors {
		nameSet[a] = true
	}
	var sites []string
	for importPath, files := range c.pkgFiles {
		if importPath == "vl/store" {
			continue
		}
		mentions := false
		for _, f := range files {
			af := c.asts[f]
			if af == nil {
				t.Fatalf("no parsed AST for %s", f)
			}
			ast.Inspect(af, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && nameSet[sel.Sel.Name] {
					mentions = true
				}
				return !mentions
			})
			if mentions {
				break
			}
		}
		if !mentions {
			continue
		}
		info := c.checkPackage(t, importPath)
		for sel, seln := range info.Selections {
			fn, ok := seln.Obj().(*types.Func)
			if !ok || !funcs[fn] {
				continue
			}
			pos := c.fset.Position(sel.Pos())
			rel, rerr := filepath.Rel(c.root, pos.Filename)
			if rerr != nil {
				t.Fatalf("rel(%s, %s): %v", c.root, pos.Filename, rerr)
			}
			sites = append(sites, fmt.Sprintf("%s:%d", filepath.ToSlash(rel), pos.Line))
		}
	}
	sort.Strings(sites)
	return sites
}

// A CANDIDATE-UNVERIFIED KNOB HAS NO METHOD READER. If it has one, it is
// misclassified: the panel would tell the operator a wired switch does nothing.
func TestCandidateKnobsHaveNoMethodReaders(t *testing.T) {
	for _, e := range AllKnobs() {
		if e.Status != KnobCandidate {
			continue
		}
		acc, sites := methodReadersOfKnob(t, e.Path)
		if len(sites) > 0 {
			t.Errorf("knob %q is candidate-unverified but is READ through %v at %v — reclassify it live with those consumers", e.Path, acc, sites)
		}
	}
}

// freeResolverReaders finds production references (OUTSIDE store/) to a knob's
// free-function RESOLVER (Resolve<GoField>) and/or its config accessor
// (<GoField>Enabled). The field-level method pass misses these when the knob's
// field sits on a NESTED type and the accessor lives on the outer config type
// (htf_veto: field Regime.HTFVeto, accessor StrategyConfig.HTFVetoEnabled which
// calls the free function ResolveHTFVeto). A candidate knob that is referenced
// this way is live, not "no known reader".
func freeResolverReaders(t *testing.T, leaf string) []string {
	t.Helper()
	owners := knobFieldOwners(leaf)
	if len(owners) == 0 {
		return nil
	}
	names := map[string]bool{}
	for _, o := range owners {
		names[o[1]+"Enabled"] = true
		names["Resolve"+o[1]] = true
	}
	_, files := repoGoFiles(t, "..")
	var sites []string
	for _, f := range files {
		// The resolver/accessor DEFINITION lives in store/; a production
		// reader is a reference from OUTSIDE package store. repoGoFiles walks
		// from the module root (".."), so store/ files carry a ../store/ prefix.
		if strings.Contains(filepath.ToSlash(f), "/store/") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		line := 0
		for _, l := range strings.Split(string(b), "\n") {
			line++
			for n := range names {
				if strings.Contains(l, n) {
					sites = append(sites, fmt.Sprintf("%s:%d", strings.TrimPrefix(filepath.ToSlash(f), "../"), line))
					break
				}
			}
		}
	}
	sort.Strings(sites)
	return sites
}

// A CANDIDATE-UNVERIFIED KNOB HAS NO FREE-FUNCTION RESOLVER READER. The
// 2026-09-03 sweep grepped for the FIELD; a Resolve<Field> free function (or a
// <Field>Enabled accessor on the config type) is equally invisible to that
// grep. htf_veto is the proof: ResolveHTFVeto is read through
// StrategyConfig.HTFVetoEnabled, which trader/auto_trader_loop.go:424 and
// trader/armed_executor.go:3033 call. A candidate with such a reference is
// misclassified — reclassify it live.
func TestCandidateKnobsHaveNoFreeFunctionResolverReaders(t *testing.T) {
	for _, e := range AllKnobs() {
		if e.Status != KnobCandidate {
			continue
		}
		if sites := freeResolverReaders(t, e.Path); len(sites) > 0 {
			t.Errorf("knob %q is candidate-unverified but its resolver/accessor is referenced outside store/ at %v — reclassify it live with those consumers", e.Path, sites)
		}
	}
}

// THE METHOD-READER CHAIN MUST LEAVE package store (CTO CENSUS-GUARDS
// 1790306266164 [31]). store/strategy.go:1624 (c.WakeOnLevelEventsEnabled()
// inside WakeOnHTFOrderBlocks) and :1699 kept six of the seven wake knobs
// "live" under the old name-only pass even with every trader/ reader deleted
// — an intra-store accessor call is not a production reader. Every returned
// site must be outside store/, and a method expression is a reader too: the
// old CallExpr-only pass did not see trader/effective_settings.go:842
// ((*store.DayPlanConfig).WakeMinIntervalMinutes).
func TestKnobMethodReaderSitesLeaveStorePackage(t *testing.T) {
	for _, leaf := range []string{"wake_on_level_events", "wake_min_interval_min", "wake_on_15m_zone", "wake_on_htf_ob", "wake_on_htf_zone", "wake_on_ifvg", "wake_on_seated_invalidation"} {
		_, sites := methodReadersOfKnob(t, leaf)
		if len(sites) == 0 {
			t.Fatalf("%s: no call sites outside package store — the chain does not leave store/", leaf)
		}
		for _, s := range sites {
			if strings.HasPrefix(s, "store/") {
				t.Errorf("%s: site %s is inside package store — an intra-store accessor call is not a production reader", leaf, s)
			}
		}
	}
	_, sites := methodReadersOfKnob(t, "wake_min_interval_min")
	for _, s := range sites {
		if strings.HasPrefix(s, "trader/effective_settings.go:") {
			return
		}
	}
	t.Errorf("wake_min_interval_min: no site in trader/effective_settings.go — the method-expression binding is not seen")
}

// THE MATCH IS TIED TO THE RECEIVER TYPE (CTO CENSUS-GUARDS 1790306266164
// [31]). A same-named method on a foreign receiver is not the knob's reader;
// a method expression on the owner type is. Synthetic module — the owners and
// accessors are passed in exactly the shapes pass 1 produces (owner type name
// + accessor names) through the same typed pass methodReadersOfKnob calls.
func TestKnobMethodReaderSitesAreReceiverTyped(t *testing.T) {
	root := t.TempDir()
	writeTreeFile(t, root, "go.mod", "module vl\n\ngo 1.25.13\n")
	writeTreeFile(t, root, "store/store.go", `package store

type DayPlanConfig struct{ WakeOnIfvgFlag bool }

func (c *DayPlanConfig) WakeOnIfvg() bool { return c.WakeOnIfvgFlag }
func (c *DayPlanConfig) helper() bool     { return c.WakeOnIfvg() }
`)
	writeTreeFile(t, root, "elsewhere/fake.go", `package elsewhere

import "vl/store"

type Fake struct{}

func (f *Fake) WakeOnIfvg() bool       { return false }
func UseFake(f *Fake) bool             { return f.WakeOnIfvg() }
func Real(c *store.DayPlanConfig) bool { return c.WakeOnIfvg() }
func AsExpr() func(*store.DayPlanConfig) bool { return (*store.DayPlanConfig).WakeOnIfvg }
`)
	want := []string{
		"elsewhere/fake.go:9",  // Real — a call on the owner type
		"elsewhere/fake.go:10", // AsExpr — a method expression on the owner type
	}
	sites := typedAccessorCallSites(t, root, []string{"WakeOnIfvg"}, [][2]string{{"DayPlanConfig", "WakeOnIfvg"}})
	got := map[string]bool{}
	for _, s := range sites {
		got[s] = true
	}
	for _, s := range sites {
		matched := false
		for _, w := range want {
			if s == w {
				matched = true
			}
		}
		if !matched {
			t.Errorf("unexpected site %s — a same-named foreign receiver or an intra-store call is not the knob's reader", s)
		}
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing site %s", w)
		}
	}
}

// writeTreeFile writes a file under the synthetic module root.
func writeTreeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// THE WAKE KNOBS ARE READ THROUGH THEIR ACCESSORS, AND THE REGISTRY SAYS SO.
// W-KNOB-PRUNE (2026-09-18): the five legacy per-class switches are FOLDED
// (read only for the mapping in WakeOnLevelEventsEnabled / WakeOnHTFOrderBlocks),
// wake_min_interval_min is FOLDED (constant unless stored) and the single
// wake_on_level_events switch is LIVE. The detector must still find a
// production call site for each, and the registry's Consumers must name one —
// so the entry is a reader, not a claim (A24).
func TestWakeKnobsAreLiveThroughTheirAccessors(t *testing.T) {
	want := map[string]KnobStatus{
		"wake_on_level_events":        KnobLive,
		"wake_min_interval_min":       KnobFolded,
		"wake_on_15m_zone":            KnobFolded,
		"wake_on_htf_ob":              KnobFolded,
		"wake_on_htf_zone":            KnobFolded,
		"wake_on_ifvg":                KnobFolded,
		"wake_on_seated_invalidation": KnobFolded,
	}
	for _, leaf := range []string{"wake_on_level_events", "wake_min_interval_min", "wake_on_15m_zone", "wake_on_htf_ob", "wake_on_htf_zone", "wake_on_ifvg", "wake_on_seated_invalidation"} {
		e, ok := LookupKnob(leaf)
		if !ok {
			t.Fatalf("%s not in the registry", leaf)
		}
		acc, sites := methodReadersOfKnob(t, leaf)
		if len(sites) == 0 {
			t.Fatalf("%s: the detector found no method reader — the wiring this pin exists for is gone", leaf)
		}
		if e.Status != want[leaf] {
			t.Errorf("%s: status %q, want %q — read via %v at %v", leaf, e.Status, want[leaf], acc, sites)
		}
		named := false
		for _, c := range e.Consumers {
			for _, s := range sites {
				if c == s {
					named = true
				}
			}
		}
		if !named {
			t.Errorf("%s: registry Consumers %v name none of the detector's call sites %v", leaf, e.Consumers, sites)
		}
		if e.UILabel() == "no known reader — pending verification" {
			t.Errorf("%s renders %q", leaf, e.UILabel())
		}
	}
}

// TestKnobMethodReadersSeeNestedSkipNamedDirs plants a .go file in EVERY
// censuswalk.NestedProbeDirs directory of a synthetic module and asserts
// repoGoFiles (the walk the reader census uses) covers every one. With the old
// any-depth SkipDir the dirs named like a root skip were invisible (CLASS 258).
func TestKnobMethodReadersSeeNestedSkipNamedDirs(t *testing.T) {
	root := t.TempDir()
	dirs := censuswalk.NestedProbeDirs()
	for _, dir := range dirs {
		full := filepath.Join(root, filepath.FromSlash(dir))
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
		src := "package " + censuswalk.PackageName(dir) + "\n"
		if err := os.WriteFile(filepath.Join(full, "probe.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, files := repoGoFiles(t, root)
	seen := map[string]bool{}
	for _, f := range files {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			t.Fatal(err)
		}
		seen[filepath.ToSlash(rel)] = true
	}
	var missed []string
	for _, dir := range dirs {
		if !seen[dir+"/probe.go"] {
			missed = append(missed, dir)
		}
	}
	if len(missed) > 0 {
		t.Fatalf("the knob-reader walk skipped %d of %d nested probe dirs — a skip by NAME at depth exempts compiled packages (CLASS 258):\n\t%s",
			len(missed), len(dirs), strings.Join(missed, "\n\t"))
	}
}
