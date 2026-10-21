package updaterwire

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"vl/internal/censuswalk"
)

// ── M4 3b-B U2: a resume is built ONLY by the attended updater CLI ─────────
//
// resume continues a job the worker parked for an attended step (the owner's
// F5 at nt8_updated); dispatch §0: it "resumes only on an attended
// `vl-updater resume <job>`". The app never sends one: the API side hands
// off an install and reads status, nothing else. So the names that BUILD a
// resume request may appear in exactly two directories:
//
//   - internal/updaterwire — the wire package, which defines them;
//   - cmd/vl-updater     — the CLI the owner types the job id into.
//
// Both are EXACT directories, never prefixes (CTO ruling 1790258770876:
// census admissions extend by exact names): cmd/vl-updaterx, a
// subdirectory of cmd/vl-updater and internal/updaterwire/wireserver are
// all outside.
//
// "Build" is judged by NAME, fail closed: any reference to NewResume,
// VerbResume or ResumePayload of the wire package (under its own name, a
// renamed import or a dot import), and any string literal that spells a
// resume frame ("verb":"resume"). ResumePayload is in the set because a
// resume Request validates only with a non-nil *ResumePayload — naming the
// type is the one other way to build one. A READ of the verb counts too: a
// worker-side handler dispatches on the payload pointer (r.Resume != nil),
// which Validate makes equivalent, and names none of these.

// resumeBuilders are the wire package's names that build a resume request.
var resumeBuilders = map[string]bool{"NewResume": true, "VerbResume": true, "ResumePayload": true}

// resumeAdmittedDirs are the EXACT module-relative directories whose non-test
// files may name a resume builder, each with its reason.
var resumeAdmittedDirs = map[string]string{
	resumeWireDir: "the wire package defines the resume verb",
	resumeCLIDir:  "the attended `vl-updater resume <job>` CLI (M4 3b-B dispatch §0/§3) — the only sender",
}

// resumeWireDir is where the builders must be DEFINED; the census checks they
// are, so a rename cannot leave it judging names that no longer exist.
const resumeWireDir = "internal/updaterwire"

// resumeCLIDir is admitted ONLY for package main: a command cannot be
// imported, so nothing else can reach a builder through it (U2 verifier
// defect 3). Any other package name there is an offender, and the file is
// judged like any file outside.
const resumeCLIDir = "cmd/vl-updater"

// resumeFrameRe matches a hand-spelled resume frame (or a fragment of one)
// inside a string literal. Case-insensitive, because json.Unmarshal into a
// Request matches "Verb" as readily as "verb" (U2 verifier defect 2).
var resumeFrameRe = regexp.MustCompile(`(?i)"verb"\s*:\s*"resume`)

// resumeValueRe is the same rule for a decoded JSON "verb" value.
var resumeValueRe = regexp.MustCompile(`(?i)^resume`)

// spellsResume reports whether a string literal's (Go-unquoted) value spells a
// resume frame: the raw fragment rule, or — when the value is JSON — a "verb"
// key whose value DECODES to resume anywhere in it, or a "resume" key (the
// payload json.Unmarshal would put in Request.Resume). JSON escapes (a
// unicode escape inside the key or the value) are what DecodeRequest reads,
// not what a regex sees (U2 verifier defect 1).
func spellsResume(s string) bool {
	if resumeFrameRe.MatchString(s) {
		return true
	}
	var v any
	if json.Unmarshal([]byte(s), &v) != nil {
		return false
	}
	return jsonSpellsResume(v)
}

// jsonSpellsResume walks a decoded JSON value; a string inside it is judged
// again, so a frame encoded inside a JSON string is seen too.
func jsonSpellsResume(v any) bool {
	switch v := v.(type) {
	case map[string]any:
		for k, e := range v {
			if s, ok := e.(string); ok && strings.EqualFold(k, "verb") && resumeValueRe.MatchString(s) {
				return true
			}
			if strings.EqualFold(k, "resume") {
				return true
			}
			if jsonSpellsResume(e) {
				return true
			}
		}
	case []any:
		for _, e := range v {
			if jsonSpellsResume(e) {
				return true
			}
		}
	case string:
		return spellsResume(v)
	}
	return false
}

const (
	resumeWriteReason   = "writes a Resume field"
	resumePointerReason = "takes a pointer to an updaterwire.Request"
	resumeShapeReason   = "declares a Resume field (a Request shape)"
	resumeTypeReason    = "declares a type from updaterwire.Request"
	resumeConvertReason = "converts to an updaterwire.Request"
)

// throughResume reports whether e reaches a Resume field (X.Resume, or
// anything below it such as X.Resume.JobID) — the shape a write to it has.
func throughResume(e ast.Expr) bool {
	for {
		switch x := e.(type) {
		case *ast.SelectorExpr:
			if x.Sel.Name == "Resume" {
				return true
			}
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		default:
			return false
		}
	}
}

// resumeWrites reports how f (a file, or inside the wire package one
// declaration's body with dot=true) fills a Resume field without naming a
// builder (U2 verifier defect 2); local and dot are f's names for the wire
// package.
//
//   - Every file: a composite key Resume, &X.Resume, an assignment to
//     X.Resume, or anything below it. Fail closed: Resume is a field of the
//     wire Request alone today, so an unrelated one is renamed, not admitted.
//   - Files importing the wire package: an unkeyed Request literal (its last
//     element IS Resume), and any pointer to a Request — *Request, &Request{},
//     new(Request), or &x for a name that holds one (declared with the type,
//     or assigned a Request literal or any call into the wire package) —
//     because a decoder needs a pointer to fill Resume, and nothing in the
//     wire API takes one (Client.Do and the worker's Handler take values).
//
// A Request can also be ASSEMBLED from a shape without any of those
// (TestResumeCensusSeesEveryRequestShape): a read of r.Resume gives a
// *ResumePayload whose type is never spelled, and a struct with the same
// fields becomes a Request by conversion — or, unnamed, by plain assignment.
// So, too:
//
//   - Every file: a struct type declaring a field named Resume (named or
//     embedded) — struct identity needs that exact name. Fail closed, as
//     above.
//   - Files importing the wire package: a type declared from Request (alias
//     or defined), a conversion to Request, and an unkeyed literal whose type
//     is ELIDED inside a literal whose type mentions Request
//     ([]Request{{v, nil, nil, nil, p}}).
//
// READS are not judged: r.Resume != nil and r.Resume.JobID stay admitted.
func resumeWrites(f ast.Node, local map[string]bool, dot bool) []string {
	isRequest := func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.SelectorExpr:
			id, ok := x.X.(*ast.Ident)
			return ok && local[id.Name] && x.Sel.Name == "Request"
		case *ast.Ident:
			return dot && x.Name == "Request"
		}
		return false
	}
	fromWire := func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.CompositeLit:
			return isRequest(x.Type)
		case *ast.CallExpr:
			switch fn := x.Fun.(type) {
			case *ast.SelectorExpr:
				id, ok := fn.X.(*ast.Ident)
				return ok && local[id.Name]
			case *ast.Ident: // a dot import makes the wire package's calls bare
				return dot && ast.IsExported(fn.Name)
			}
		}
		return false
	}
	mentionsRequest := func(t ast.Expr) bool {
		hit := false
		ast.Inspect(t, func(n ast.Node) bool {
			if e, ok := n.(ast.Expr); ok && isRequest(e) {
				hit = true
			}
			return !hit
		})
		return hit
	}
	// elidedUnkeyed reports an unkeyed literal, at any depth, whose type is
	// elided (keys too: a map may be keyed by a Request).
	var elidedUnkeyed func(elts []ast.Expr) bool
	elidedUnkeyed = func(elts []ast.Expr) bool {
		for _, e := range elts {
			if kv, ok := e.(*ast.KeyValueExpr); ok {
				if elidedUnkeyed([]ast.Expr{kv.Key}) {
					return true
				}
				e = kv.Value
			}
			cl, ok := e.(*ast.CompositeLit)
			if !ok || cl.Type != nil {
				continue
			}
			if len(cl.Elts) > 0 {
				if _, keyed := cl.Elts[0].(*ast.KeyValueExpr); !keyed {
					return true
				}
			}
			if elidedUnkeyed(cl.Elts) {
				return true
			}
		}
		return false
	}
	holds := map[string]bool{} // names that hold a wire Request (scope-blind, fail closed)
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Field:
			if isRequest(x.Type) {
				for _, nm := range x.Names {
					holds[nm.Name] = true
				}
			}
		case *ast.ValueSpec:
			for i, nm := range x.Names {
				if (x.Type != nil && isRequest(x.Type)) || (i < len(x.Values) && fromWire(x.Values[i])) ||
					(i == 0 && len(x.Values) == 1 && fromWire(x.Values[0])) {
					holds[nm.Name] = true
				}
			}
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				nm, ok := l.(*ast.Ident)
				if ok && ((len(x.Rhs) == len(x.Lhs) && fromWire(x.Rhs[i])) || (i == 0 && len(x.Rhs) == 1 && fromWire(x.Rhs[0]))) {
					holds[nm.Name] = true
				}
			}
		}
		return true
	})
	found := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.KeyValueExpr:
			if id, ok := x.Key.(*ast.Ident); ok && id.Name == "Resume" {
				found[resumeWriteReason] = true
			}
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				if throughResume(l) {
					found[resumeWriteReason] = true
				}
			}
		case *ast.UnaryExpr:
			if x.Op != token.AND {
				break
			}
			if throughResume(x.X) {
				found[resumeWriteReason] = true
			}
			switch y := x.X.(type) {
			case *ast.Ident:
				if holds[y.Name] {
					found[resumePointerReason] = true
				}
			case *ast.CompositeLit:
				if isRequest(y.Type) {
					found[resumePointerReason] = true
				}
			}
		case *ast.StarExpr:
			if isRequest(x.X) {
				found[resumePointerReason] = true
			}
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 && isRequest(x.Args[0]) {
				found[resumePointerReason] = true
			}
			if len(x.Args) == 1 && isRequest(ast.Unparen(x.Fun)) {
				found[resumeConvertReason] = true
			}
		case *ast.CompositeLit:
			if isRequest(x.Type) && len(x.Elts) > 0 {
				if _, keyed := x.Elts[0].(*ast.KeyValueExpr); !keyed {
					found[resumeWriteReason] = true
				}
			}
			if x.Type != nil && !isRequest(x.Type) && mentionsRequest(x.Type) && elidedUnkeyed(x.Elts) {
				found[resumeWriteReason] = true
			}
		case *ast.StructType:
			for _, fl := range x.Fields.List {
				names := fl.Names
				if len(names) == 0 { // embedded: the field is named after its type
					if id := embeddedName(fl.Type); id != nil {
						names = []*ast.Ident{id}
					}
				}
				for _, nm := range names {
					if nm.Name == "Resume" {
						found[resumeShapeReason] = true
					}
				}
			}
		case *ast.TypeSpec:
			if isRequest(ast.Unparen(x.Type)) {
				found[resumeTypeReason] = true
			}
		}
		return true
	})
	out := make([]string, 0, len(found))
	for r := range found {
		out = append(out, r)
	}
	return out
}

// embeddedName is the field name an embedded field takes: its type's name
// (T, *T, pkg.T, T[X]).
func embeddedName(e ast.Expr) *ast.Ident {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.SelectorExpr:
			return x.Sel
		case *ast.Ident:
			return x
		default:
			return nil
		}
	}
}

// wireDecl is one top-level declaration of the wire package: its pin name
// (a method as Recv.Name) and the nodes of its body — never its own defining
// name, so NewResume's declaration is judged by what NewResume contains.
type wireDecl struct {
	name  string
	parts []ast.Node
}

func wireDeclsOf(d ast.Decl) []wireDecl {
	switch d := d.(type) {
	case *ast.FuncDecl:
		name, parts := d.Name.Name, []ast.Node{d.Type}
		if d.Recv != nil {
			parts = append(parts, d.Recv)
			if len(d.Recv.List) == 1 {
				name = recvTypeName(d.Recv.List[0].Type) + "." + name
			}
		}
		if d.Body != nil {
			parts = append(parts, d.Body)
		}
		return []wireDecl{{name, parts}}
	case *ast.GenDecl:
		var out []wireDecl
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *ast.TypeSpec:
				// A blank-named copy of the spec: the TypeSpec rule of
				// resumeWrites sees `type Req = Request` / `type Req Request`,
				// and the defining name itself is never judged.
				out = append(out, wireDecl{s.Name.Name, []ast.Node{&ast.TypeSpec{
					Name: ast.NewIdent("_"), TypeParams: s.TypeParams, Assign: s.Assign, Type: s.Type,
				}}})
			case *ast.ValueSpec:
				var parts []ast.Node
				if s.Type != nil {
					parts = append(parts, s.Type)
				}
				for _, v := range s.Values {
					parts = append(parts, v)
				}
				for _, n := range s.Names {
					out = append(out, wireDecl{n.Name, parts})
				}
			}
		}
		return out
	}
	return nil
}

func recvTypeName(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.Ident:
			return x.Name
		default:
			return "?"
		}
	}
}

// reachesResume reports whether a wire-package declaration body names a
// builder (bare, as the package itself spells it), spells a resume frame, or
// writes a Resume field / takes a pointer to a Request (resumeWrites, with
// the package's own names read as a dot import would read them).
func reachesResume(parts []ast.Node) bool {
	for _, p := range parts {
		hit := false
		ast.Inspect(p, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				hit = hit || resumeBuilders[x.Name]
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					s, err := strconv.Unquote(x.Value)
					hit = hit || (err == nil && spellsResume(s))
				}
			}
			return !hit
		})
		if hit || len(resumeWrites(p, nil, true)) > 0 {
			return true
		}
	}
	return false
}

// resumeWireDecls is the EXACT set of top-level declarations of
// resumeWireDir (methods as Recv.Name) whose bodies reach a resume builder:
// name one, spell a resume frame, or write a Resume field. The wire directory
// is admitted wholesale, so without this pin a new exported wrapper there
// (func Continue(j string) Request { return NewResume(j) }) would hand the
// app a resume under a name no census judges (U2 verifier defect 4). A new
// member is an offender; a removed or renamed one fails the real-tree census.
var resumeWireDecls = map[string]bool{
	"Verbs":            true, // the fixed verb list
	"Verb.Known":       true, // the fixed verb set
	"Request":          true, // the Resume *ResumePayload field
	"NewResume":        true, // THE builder (the CLI's)
	"ResumeWorker":     true, // W-ADDON-ONE-CLICK: the page Retry's single non-CLI sender (api calls this, never NewResume)
	"payloadKeys":      true, // resume's payload allow-list
	"DecodeRequest":    true, // decodes a resume frame via NewResume
	"Request.Validate": true, // checks the resume payload
	"EncodeRequest":    true, // encodes the resume payload
}

type resumeCensus struct {
	offenders    []string
	scanned      int
	defined      map[string]bool // builder names declared at top level of resumeWireDir
	wireMentions map[string]bool // top-level decls of resumeWireDir that reach a builder
}

// resumeBuilderCensus scans every non-test .go file under root (the ONE
// root-only walk, internal/censuswalk) and reports each file outside
// resumeAdmittedDirs that names a resume builder of <module>/internal/updaterwire
// or spells a resume frame.
func resumeBuilderCensus(root string) (resumeCensus, error) {
	c := resumeCensus{defined: map[string]bool{}, wireMentions: map[string]bool{}}
	module, err := censuswalk.ModulePath(root)
	if err != nil {
		return c, err
	}
	wirePath := module + "/" + resumeWireDir
	files, err := censuswalk.NonTestGoFiles(root)
	if err != nil {
		return c, err
	}
	hits := map[string]bool{}
	for _, file := range files {
		rel := file.Rel
		f, perr := parser.ParseFile(token.NewFileSet(), file.Path, nil, parser.SkipObjectResolution)
		if perr != nil {
			hits[rel+": cannot be parsed"] = true
			continue
		}
		c.scanned++
		dir := path.Dir(rel)
		if dir == resumeWireDir {
			for _, d := range f.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					if d.Recv == nil && resumeBuilders[d.Name.Name] {
						c.defined[d.Name.Name] = true
					}
				case *ast.GenDecl:
					for _, s := range d.Specs {
						switch s := s.(type) {
						case *ast.TypeSpec:
							if resumeBuilders[s.Name.Name] {
								c.defined[s.Name.Name] = true
							}
						case *ast.ValueSpec:
							for _, n := range s.Names {
								if resumeBuilders[n.Name] {
									c.defined[n.Name] = true
								}
							}
						}
					}
				}
				for _, wd := range wireDeclsOf(d) {
					if !reachesResume(wd.parts) {
						continue
					}
					c.wireMentions[wd.name] = true
					if !resumeWireDecls[wd.name] {
						hits[rel+": declares "+wd.name+", which reaches a resume builder outside the pinned set (resumeWireDecls)"] = true
					}
				}
			}
		}
		if dir == resumeCLIDir && f.Name.Name != "main" {
			hits[rel+": package "+f.Name.Name+" (the CLI directory admits only package main)"] = true
		} else if _, admitted := resumeAdmittedDirs[dir]; admitted {
			continue
		}
		local := map[string]bool{} // this file's names for the wire package
		dot := false
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil || p != wirePath {
				continue
			}
			switch {
			case imp.Name == nil:
				local[path.Base(wirePath)] = true
			case imp.Name.Name == ".":
				dot = true
			case imp.Name.Name != "_":
				local[imp.Name.Name] = true
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && local[id.Name] && resumeBuilders[x.Sel.Name] {
					hits[rel+": names updaterwire."+x.Sel.Name] = true
				}
			case *ast.Ident:
				if dot && resumeBuilders[x.Name] {
					hits[rel+": names updaterwire."+x.Name] = true
				}
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					if s, err := strconv.Unquote(x.Value); err == nil && spellsResume(s) {
						hits[rel+": spells a resume frame"] = true
					}
				}
			}
			return true
		})
		for _, why := range resumeWrites(f, local, dot) {
			hits[rel+": "+why] = true
		}
	}
	for h := range hits {
		c.offenders = append(c.offenders, h)
	}
	sort.Strings(c.offenders)
	return c, nil
}

// PIN (M4 3b-B U2, dispatch §0/§3): over the REAL tree, nothing but the wire
// package and cmd/vl-updater names a resume builder or spells a resume
// frame. At this head cmd/vl-updater does not exist yet, so this proves no
// OTHER package references them; the admitted set is that exact directory.
//
// The name carries "Census" so the standard gate
// (go test -run 'Census|Guard|Walk|Link') runs this real-tree scan; it keeps
// the brief's name as its prefix, so -run TestOnlyTheUpdaterCLIBuildsAResume
// (the name wire.go cites) still selects it.
func TestOnlyTheUpdaterCLIBuildsAResumeCensus(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	c, err := resumeBuilderCensus(root)
	if err != nil {
		t.Fatal(err)
	}
	if c.scanned < 100 {
		t.Fatalf("census scanned only %d files", c.scanned)
	}
	for name := range resumeBuilders {
		if !c.defined[name] {
			t.Fatalf("%s is not declared in %s — the census would judge a name that no longer exists (update resumeBuilders with the rename)", name, resumeWireDir)
		}
	}
	for name := range resumeWireDecls {
		if !c.wireMentions[name] {
			t.Fatalf("%s is pinned in resumeWireDecls but no longer reaches a resume builder in %s (reached: %v) — update the pin with the rename or removal", name, resumeWireDir, c.wireMentions)
		}
	}
	if len(c.offenders) > 0 {
		t.Fatalf("a resume is built only by the attended updater CLI (cmd/vl-updater); the app never sends one:\n%s", strings.Join(c.offenders, "\n"))
	}
	if _, err := os.Stat(filepath.Join(root, "cmd", "vl-updater")); err != nil {
		t.Logf("cmd/vl-updater absent at this head (%v): no package in the tree builds a resume", err)
	}
}

// resumeCensusWireGo is the synthetic module's wire package: the three
// builders, and the Request field a write can fill.
const resumeCensusWireGo = "package updaterwire\n\ntype Verb string\n\nconst VerbResume Verb = \"resume\"\n\n" +
	"type ResumePayload struct{ JobID string }\n\ntype Request struct {\n\tVerb   Verb\n\tResume *ResumePayload\n}\n\n" +
	"func NewResume(j string) Request { return Request{Verb: VerbResume, Resume: &ResumePayload{JobID: j}} }\n"

// resumeCensusOf plants files (module-relative path → body) in a synthetic
// module (t.TempDir, never the real tree) beside resumeCensusWireGo, and runs
// the PRODUCTION census on it. A planted internal/updaterwire/wire.go
// replaces the synthetic one.
func resumeCensusOf(t *testing.T, files map[string]string) resumeCensus {
	t.Helper()
	root := t.TempDir()
	all := map[string]string{
		"go.mod":                       "module vl\n\ngo 1.25\n",
		"internal/updaterwire/wire.go": resumeCensusWireGo,
	}
	for r, b := range files {
		all[r] = b
	}
	for r, b := range all {
		p := filepath.Join(root, filepath.FromSlash(r))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c, err := resumeBuilderCensus(root)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// resumeCensusImp is a one-file body importing the wire package as name
// ("" = its own name, "." = dot import).
func resumeCensusImp(pkg, name, use string) string {
	return "package " + pkg + "\n\nimport " + name + " \"vl/internal/updaterwire\"\n\n" + use + "\n"
}

// resumeJSONU spells a JSON unicode escape (backslash, u, four hex) at run
// time, so no editor or tool that decodes escapes can quietly turn a case
// back into the raw form the regex already sees — the cases guard it.
func resumeJSONU(hex string) string { return `\` + "u" + hex }

// wantResumeOffenders fails unless the census judged exactly want (reasons,
// each prefixed with rel) — no more, no fewer.
func wantResumeOffenders(t *testing.T, c resumeCensus, rel string, want ...string) {
	t.Helper()
	full := make([]string, 0, len(want))
	for _, w := range want {
		full = append(full, rel+": "+w)
	}
	sort.Strings(full)
	if strings.Join(c.offenders, "\n") != strings.Join(full, "\n") {
		t.Fatalf("census offenders = %v, want exactly %v", c.offenders, full)
	}
}

// PIN: the census sees every way to name a builder, and admits only the two
// exact directories. Each case plants ONE file in a synthetic module
// (t.TempDir, never the real tree) and is judged by the PRODUCTION census
// function; each has its expected single offender, and the positive controls
// have none.
func TestResumeBuilderCensusSeesEveryForm(t *testing.T) {
	census := func(t *testing.T, rel, body string) resumeCensus {
		t.Helper()
		files := map[string]string{}
		if rel != "" {
			files[rel] = body
		}
		return resumeCensusOf(t, files)
	}
	imp := resumeCensusImp
	ctor := `var _ = updaterwire.NewResume("job-0001abcd")`

	// the wire package alone: no offender, all three builders seen as defined
	if c := census(t, "", ""); len(c.offenders) != 0 || len(c.defined) != 3 {
		t.Fatalf("definer only: offenders=%v defined=%v", c.offenders, c.defined)
	}

	// positive controls: the admitted directories, and a handler that
	// dispatches on the payload pointer
	for _, ok := range []struct{ name, rel, body string }{
		{"the CLI", "cmd/vl-updater/main.go", imp("main", "", "func main() {\n\t_ = updaterwire.NewResume(\"job-0001abcd\")\n\t_ = updaterwire.Request{Verb: updaterwire.VerbResume, Resume: &updaterwire.ResumePayload{}}\n}")},
		{"the CLI, a second file", "cmd/vl-updater/resume.go", imp("main", "uw", "const frame = `{\"v\":1,\"verb\":\"resume\",\"payload\":{}}`\n\nvar _ = uw.VerbResume")},
		{"the wire package itself, a pinned declaration", "internal/updaterwire/more.go", "package updaterwire\n\nfunc EncodeRequest(r Request) ([]byte, error) {\n\tif r.Verb == VerbResume {\n\t\treturn []byte(`{\"verb\":\"resume\"}`), nil\n\t}\n\treturn nil, nil\n}\n"},
		{"a handler reading the payload pointer", "internal/updaterworker/socket.go", imp("updaterworker", "", "func isResume(r updaterwire.Request) bool { return r.Resume != nil }")},
		{"an unrelated resume word", "agent/x.go", "package agent\n\nvar words = []string{\"resume\", \"continue\"}\n"},
	} {
		t.Run("admitted "+ok.name, func(t *testing.T) {
			if c := census(t, ok.rel, ok.body); len(c.offenders) != 0 {
				t.Fatalf("%s must be admitted, census offenders = %v", ok.rel, c.offenders)
			}
		})
	}

	cases := []struct{ name, rel, body, want string }{
		{"the app, constructor", "api/resume.go", imp("api", "", ctor), "names updaterwire.NewResume"},
		{"the app, verb const", "api/resume.go", imp("api", "", "var _ = updaterwire.Request{Verb: updaterwire.VerbResume}"), "names updaterwire.VerbResume"},
		{"the app, payload type", "api/resume.go", imp("api", "", "var _ = new(updaterwire.ResumePayload)"), "names updaterwire.ResumePayload"},
		{"a read of the verb", "internal/updaterworker/socket.go", imp("updaterworker", "", "func isResume(r updaterwire.Request) bool { return r.Verb == updaterwire.VerbResume }"), "names updaterwire.VerbResume"},
		{"renamed import", "api/resume.go", imp("api", "uw", `var _ = uw.NewResume("job-0001abcd")`), "names updaterwire.NewResume"},
		{"dot import", "api/resume.go", imp("api", ".", `var _ = NewResume("job-0001abcd")`), "names updaterwire.NewResume"},
		{"hand-spelled frame", "api/resume.go", "package api\n\nconst f = `{\"v\":1, \"verb\" : \"resume\",\"payload\":{\"job_id\":\"job-0001abcd\"}}`\n", "spells a resume frame"},
		{"escaped frame", "api/resume.go", "package api\n\nconst f = \"{\\\"verb\\\":\\\"resume\\\"}\"\n", "spells a resume frame"},
		{"name-prefix sibling of the CLI", "cmd/vl-updaterx/main.go", imp("main", "", ctor), "names updaterwire.NewResume"},
		{"subdirectory of the CLI", "cmd/vl-updater/sub/x.go", imp("sub", "", ctor), "names updaterwire.NewResume"},
		{"the worker-side wire subpackage", "internal/updaterwire/wireserver/x.go", imp("wireserver", "", ctor), "names updaterwire.NewResume"},
		{"the trading app", "trader/x.go", imp("trader", "", ctor), "names updaterwire.NewResume"},
	}
	for _, dir := range censuswalk.NestedProbeDirs() {
		cases = append(cases, struct{ name, rel, body, want string }{
			"nested " + dir, dir + "/resume.go", imp(censuswalk.PackageName(dir), "", ctor), "names updaterwire.NewResume",
		})
	}

	// U2 verifier defect 1: frames with NO raw `"verb":"resume"` in them that
	// the PRODUCTION codec still decodes to exactly NewResume(job) — JSON
	// escapes are what DecodeRequest reads, not what a regex sees. The codec
	// proof first, so no case here is a strawman.
	wantFrame, err := EncodeRequest(NewResume("job-0001abcd"))
	if err != nil {
		t.Fatal(err)
	}
	jsonU := resumeJSONU
	for _, f := range []struct{ name, frame string }{
		{"JSON-escaped verb value", `{"v":1,"verb":"` + jsonU("0072") + `esume","payload":{"job_id":"job-0001abcd"}}`},
		{"JSON-escaped verb key", `{"v":1,"v` + jsonU("0065") + `rb":"resume","payload":{"job_id":"job-0001abcd"}}`},
	} {
		if strings.Contains(f.frame, `"verb":"resume"`) {
			t.Fatalf("%s is not escaped, the case proves nothing: %s", f.name, f.frame)
		}
		r, err := DecodeRequest([]byte(f.frame))
		got, eerr := EncodeRequest(r)
		if err != nil || eerr != nil || string(got) != string(wantFrame) {
			t.Fatalf("%s: the codec must read %s as a resume (decode %v, encode %v, frame %q)", f.name, f.frame, err, eerr, got)
		}
		cases = append(cases, struct{ name, rel, body, want string }{
			f.name, "api/resume.go", "package api\n\nconst f = `" + f.frame + "`\n", "spells a resume frame",
		})
	}
	inner, err := json.Marshal(`{"v` + jsonU("0065") + `rb":"resume"}`)
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct{ name, rel, body, want string }{
		"escaped frame inside a JSON string", "api/resume.go", "package api\n\nconst f = `{\"frames\":[" + string(inner) + "]}`\n", "spells a resume frame",
	})
	for _, ok := range []struct{ name, rel, body string }{
		{"JSON naming another verb", "agent/x.go", "package agent\n\nconst f = `{\"v\":1,\"verb\":\"status\",\"payload\":{}}`\n"},
		{"a receipt step called resume", "internal/updaterworker/receipt.go", "package updaterworker\n\nconst f = `{\"step\":\"resume\",\"resumed_at\":\"2026-09-24T10:00:00Z\"}`\n"},
	} {
		t.Run("admitted "+ok.name, func(t *testing.T) {
			if c := census(t, ok.rel, ok.body); len(c.offenders) != 0 {
				t.Fatalf("%s must be admitted, census offenders = %v", ok.rel, c.offenders)
			}
		})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := census(t, tc.rel, tc.body)
			if want := tc.rel + ": " + tc.want; len(c.offenders) != 1 || c.offenders[0] != want {
				t.Fatalf("census offenders = %v, want exactly [%s]", c.offenders, want)
			}
		})
	}
}

// PIN (U2 verifier defect 2, probe.out H2/H3/H4): a resume Request is valid
// only with a non-nil Resume, so WRITING that field builds one without naming
// any builder. The census flags every write form outside the admitted set —
// a composite key Resume, an unkeyed wire Request literal, &X.Resume and an
// assignment to X.Resume (or below it) — and every pointer to a wire Request,
// because a decoder (json.Unmarshal, Decoder.Decode, any helper) needs one to
// fill Resume; and the frame rules are case-insensitive, as json.Unmarshal
// into a Request is. READS stay admitted: a worker-side handler dispatches on
// r.Resume != nil and reads r.Resume.JobID.
func TestResumeCensusSeesEveryResumeFieldWrite(t *testing.T) {
	const (
		write = "writes a Resume field"
		ptr   = "takes a pointer to an updaterwire.Request"
		frame = "spells a resume frame"
	)
	file := func(pkg, body string) string {
		return "package " + pkg + "\n\nimport (\n\t\"bytes\"\n\t\"encoding/json\"\n\n\t\"vl/internal/updaterwire\"\n)\n\n" +
			"var _, _ = bytes.NewReader, json.Unmarshal\n\n" + body + "\n"
	}
	for _, tc := range []struct {
		name, rel, body string
		want            []string
	}{
		// the verifier's probes
		{"H2 json into &r.Resume", "api/h2.go", file("api", "func f() ([]byte, error) {\n\tr := updaterwire.Request{Verb: \"resume\"}\n\t_ = json.Unmarshal([]byte(`{\"job_id\":\"job-0001abcd\"}`), &r.Resume)\n\treturn updaterwire.EncodeRequest(r)\n}"), []string{write}},
		{"H3 Verbs()[3] and &r.Resume", "api/h3.go", file("api", "func f() ([]byte, error) {\n\tr := updaterwire.Request{Verb: updaterwire.Verbs()[3]}\n\t_ = json.Unmarshal([]byte(`{\"job_id\":\"job-0001abcd\"}`), &r.Resume)\n\treturn updaterwire.EncodeRequest(r)\n}"), []string{write}},
		{"H4 Go-struct JSON into a whole Request", "api/h4.go", file("api", "func f() ([]byte, error) {\n\tvar r updaterwire.Request\n\t_ = json.Unmarshal([]byte(`{\"Verb\":\"resume\",\"Resume\":{\"job_id\":\"job-0001abcd\"}}`), &r)\n\treturn updaterwire.EncodeRequest(r)\n}"), []string{frame, ptr}},
		// every other write form
		{"composite key Resume", "api/w.go", file("api", "func f(r0 updaterwire.Request) updaterwire.Request {\n\treturn updaterwire.Request{Verb: r0.Verb, Resume: r0.Resume}\n}"), []string{write}},
		{"assignment to X.Resume", "api/w.go", file("api", "func f(r0 updaterwire.Request) (r updaterwire.Request) {\n\tr.Verb = r0.Verb\n\tr.Resume = r0.Resume\n\treturn r\n}"), []string{write}},
		{"assignment below X.Resume", "api/w.go", file("api", "func f(r updaterwire.Request) updaterwire.Request {\n\tr.Resume.JobID = \"job-0002abcd\"\n\treturn r\n}"), []string{write}},
		{"address below X.Resume", "api/w.go", file("api", "func f(b []byte, r updaterwire.Request) error {\n\treturn json.Unmarshal(b, &r.Resume.JobID)\n}"), []string{write}},
		{"unkeyed Request literal", "api/w.go", file("api", "func f(r0 updaterwire.Request) updaterwire.Request {\n\treturn updaterwire.Request{r0.Verb, nil, nil, nil, r0.Resume}\n}"), []string{write}},
		// the struct declaring the field is judged too since the shape rule
		// (TestResumeCensusSeesEveryRequestShape), so both reasons
		{"an unrelated Resume field (fail closed)", "agent/x.go", "package agent\n\ntype state struct{ Resume bool }\n\nfunc f(s *state) { s.Resume = true }\n", []string{write, resumeShapeReason}},
		// pointers to a wire Request
		{"a decoder into a runtime frame", "api/p.go", file("api", "func f(b []byte) (updaterwire.Request, error) {\n\tvar r updaterwire.Request\n\terr := json.NewDecoder(bytes.NewReader(b)).Decode(&r)\n\treturn r, err\n}"), []string{ptr}},
		{"a Request parameter's address", "api/p.go", file("api", "func f(b []byte, r updaterwire.Request) (updaterwire.Request, error) {\n\treturn r, json.Unmarshal(b, &r)\n}"), []string{ptr}},
		{"a wire constructor's result, addressed", "api/p.go", file("api", "func f(b []byte) (updaterwire.Request, error) {\n\tr := updaterwire.NewStatus(\"\")\n\treturn r, json.Unmarshal(b, &r)\n}"), []string{ptr}},
		{"a pointer through any()", "api/p.go", file("api", "func f(b []byte) (updaterwire.Request, error) {\n\tvar r updaterwire.Request\n\treturn r, json.Unmarshal(b, any(&r))\n}"), []string{ptr}},
		{"new(Request)", "api/p.go", file("api", "func f(b []byte) (updaterwire.Request, error) {\n\tp := new(updaterwire.Request)\n\treturn *p, json.Unmarshal(b, p)\n}"), []string{ptr}},
		{"&Request{}", "api/p.go", file("api", "func f(b []byte) error {\n\treturn json.Unmarshal(b, &updaterwire.Request{})\n}"), []string{ptr}},
		{"a *Request parameter", "api/p.go", file("api", "func f(b []byte, p *updaterwire.Request) error {\n\treturn json.Unmarshal(b, p)\n}"), []string{ptr}},
		{"renamed import", "api/p.go", resumeCensusImp("api", "uw", "func f(r uw.Request) *uw.Request { return &r }"), []string{ptr}},
		{"dot import", "api/p.go", resumeCensusImp("api", ".", "func f(r Request) *Request { return &r }"), []string{ptr}},
		// case-insensitive frame rules
		{"case-insensitive fragment", "api/f.go", "package api\n\nconst f = `, \"Verb\" : \"RESUME`\n", []string{frame}},
		{"an escaped, re-cased verb key", "api/f.go", "package api\n\nconst f = `{\"V" + resumeJSONU("0065") + "rb\":\"resume\"}`\n", []string{frame}},
		{"a resume payload key in JSON", "api/f.go", "package api\n\nconst f = `{\"Resume\":{\"job_id\":\"job-0001abcd\"}}`\n", []string{frame}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantResumeOffenders(t, resumeCensusOf(t, map[string]string{tc.rel: tc.body}), tc.rel, tc.want...)
		})
	}

	// READS stay admitted (the U4 socket dispatches on r.Resume != nil, never
	// on case VerbResume); so do the app's by-value send, a relay that only
	// decodes and re-encodes (outside any name census by design: the socket's
	// peer uid and the worker's job file are the gates), decoding something
	// else beside a Request, and the CLI, which may build freely.
	for _, ok := range []struct{ name, rel, body string }{
		{"reads of the payload", "internal/updaterworker/socket.go", file("updaterworker", "func jobOf(r updaterwire.Request) (string, bool) {\n\tif r.Resume != nil {\n\t\treturn r.Resume.JobID, true\n\t}\n\treturn \"\", false\n}")},
		{"the app sends by value", "api/updates.go", file("api", "func send(c *updaterwire.Client) (updaterwire.Response, error) {\n\treturn c.Do(updaterwire.NewInstall(\"rel\", \"job-0001abcd\"))\n}")},
		{"a relay", "api/relay.go", file("api", "func relay(b []byte) ([]byte, error) {\n\tr, err := updaterwire.DecodeRequest(b)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\treturn updaterwire.EncodeRequest(r)\n}")},
		{"decoding something else", "api/other.go", file("api", "func f(b []byte, r updaterwire.Request) (int, error) {\n\tvar n int\n\terr := json.Unmarshal(b, &n)\n\t_ = r.Verb\n\treturn n, err\n}")},
		{"the CLI", "cmd/vl-updater/main.go", file("main", "func main() {\n\tvar r updaterwire.Request\n\t_ = json.Unmarshal(nil, &r.Resume)\n\t_ = json.Unmarshal(nil, &r)\n\t_ = updaterwire.Request{Resume: r.Resume}\n}")},
	} {
		t.Run("admitted "+ok.name, func(t *testing.T) {
			wantResumeOffenders(t, resumeCensusOf(t, map[string]string{ok.rel: ok.body}), ok.rel)
		})
	}
}

// PIN (U2 verifier defect 3, probe2.out G1): cmd/vl-updater is admitted
// because a main package cannot be imported. Any other package name there
// could be (nu "vl/cmd/vl-updater"), wrapping a builder for the app, so
// the file is an offender itself and is judged like any file outside.
func TestResumeCensusAdmitsOnlyPackageMainInTheCLIDir(t *testing.T) {
	const notMain = "package vlupdater (the CLI directory admits only package main)"
	t.Run("G1: an importable wrapper the app calls", func(t *testing.T) {
		c := resumeCensusOf(t, map[string]string{
			"cmd/vl-updater/lib.go": "package vlupdater\n\nimport \"vl/internal/updaterwire\"\n\nfunc R(j string) updaterwire.Request { return updaterwire.NewResume(j) }\n",
			"api/x.go":                "package api\n\nimport nu \"vl/cmd/vl-updater\"\n\nvar _ = nu.R(\"job-0001abcd\")\n",
		})
		wantResumeOffenders(t, c, "cmd/vl-updater/lib.go", notMain, "names updaterwire.NewResume")
	})
	t.Run("a non-main package naming no builder", func(t *testing.T) {
		c := resumeCensusOf(t, map[string]string{"cmd/vl-updater/lib.go": "package vlupdater\n\nfunc R() {}\n"})
		wantResumeOffenders(t, c, "cmd/vl-updater/lib.go", notMain)
	})
	t.Run("admitted package main", func(t *testing.T) {
		c := resumeCensusOf(t, map[string]string{"cmd/vl-updater/main.go": resumeCensusImp("main", "", "func main() { _ = updaterwire.NewResume(\"job-0001abcd\") }")})
		wantResumeOffenders(t, c, "cmd/vl-updater/main.go")
	})
}

// PIN (U2 verifier defect 4, probe2.out G2): the wire directory is admitted
// wholesale, so the census pins WHICH of its top-level declarations reach a
// resume builder (resumeWireDecls). A new one — an exported wrapper the app
// could call, a method, an init, a blank var, a frame constant, a function
// that only writes Resume — is an offender naming the declaration; a pinned
// name in any file of the package is admitted, and so is a declaration that
// reaches no builder.
func TestResumeCensusPinsTheWirePackagesResumeDeclarations(t *testing.T) {
	// the synthetic wire package: its type field and its constructor body
	if c := resumeCensusOf(t, nil); strings.Join(sortedKeys(c.wireMentions), " ") != "NewResume Request" {
		t.Errorf("synthetic wire package reaches a builder from %v, want [NewResume Request]", sortedKeys(c.wireMentions))
	}
	const more = "internal/updaterwire/more.go"
	for _, tc := range []struct{ name, body, decl string }{
		{"G2: an exported wrapper the app calls", "func Continue(j string) Request { return NewResume(j) }", "Continue"},
		{"a method", "func (r Request) AsResume(j string) Request { return NewResume(j) }", "Request.AsResume"},
		{"a pointer-receiver method writing Resume", "func (r *Request) Park(j string) { r.Resume = &ResumePayload{JobID: j} }", "Request.Park"},
		{"a wrapper that only writes Resume", "func Continue(j string) (r Request) {\n\tr.Verb = Verbs()[3]\n\t_ = json.Unmarshal([]byte(`{\"job_id\":\"`+j+`\"}`), &r.Resume)\n\treturn r\n}", "Continue"},
		{"a wrapper that only spells a frame", "func Continue(j string) (Request, error) {\n\treturn DecodeRequest([]byte(`{\"v\":1,\"verb\":\"resume\",\"payload\":{\"job_id\":\"` + j + `\"}}`))\n}", "Continue"},
		{"an init", "func init() { _ = NewResume(\"job-0001abcd\") }", "init"},
		{"a blank var", "var _ = NewResume(\"job-0001abcd\")", "_"},
		{"a frame constant", "const resumeFrame = `{\"verb\":\"resume\"}`", "resumeFrame"},
		{"a type alias of the payload", "type Payload = ResumePayload", "Payload"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := "package updaterwire\n\nimport \"encoding/json\"\n\nvar _ = json.Unmarshal\n\n" + tc.body + "\n"
			wantResumeOffenders(t, resumeCensusOf(t, map[string]string{more: body}), more,
				"declares "+tc.decl+", which reaches a resume builder outside the pinned set (resumeWireDecls)")
		})
	}
	for _, ok := range []struct{ name, body string }{
		{"a pinned name in another file", "func DecodeRequest(b []byte) (Request, error) { return NewResume(string(b)), nil }"},
		{"a pinned method in another file", "func (v Verb) Known() bool { return v == VerbResume }"},
		{"a declaration that reaches no builder", "func Helper(r Request) bool { return r.Resume != nil }"},
	} {
		t.Run("admitted "+ok.name, func(t *testing.T) {
			wantResumeOffenders(t, resumeCensusOf(t, map[string]string{more: "package updaterwire\n\n" + ok.body + "\n"}), more)
		})
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// PIN (U2 verifier note 6, kept STRICT): the U4 worker socket dispatches a
// resume on the payload pointer (req.Resume != nil, then req.Resume.JobID) —
// admitted; a `case updaterwire.VerbResume:` outside the admitted set names a
// builder and is an offender. Validate makes the two dispatches equivalent,
// so the strict form costs the worker nothing.
func TestResumeCensusU4SocketDispatchesOnThePayloadPointer(t *testing.T) {
	const rel = "internal/updaterworker/socket.go"
	handler := func(dispatch string) string {
		return resumeCensusImp("updaterworker", "", "type Worker struct{}\n\nfunc (w *Worker) resume(job string) updaterwire.Response { return updaterwire.Response{OK: true, State: \"resuming\"} }\n\n"+
			"func (w *Worker) Handle(req updaterwire.Request) updaterwire.Response {\n"+dispatch+"\treturn updaterwire.RejectedResponse\n}")
	}
	t.Run("admitted: on the payload pointer", func(t *testing.T) {
		c := resumeCensusOf(t, map[string]string{rel: handler("\tswitch {\n\tcase req.Status != nil:\n\t\treturn updaterwire.Response{OK: true, State: \"idle\"}\n\tcase req.Resume != nil:\n\t\treturn w.resume(req.Resume.JobID)\n\t}\n")})
		wantResumeOffenders(t, c, rel)
	})
	t.Run("offender: case VerbResume", func(t *testing.T) {
		c := resumeCensusOf(t, map[string]string{rel: handler("\tswitch req.Verb {\n\tcase updaterwire.VerbResume:\n\t\treturn w.resume(req.Resume.JobID)\n\t}\n")})
		wantResumeOffenders(t, c, rel, "names updaterwire.VerbResume")
	})
}

// PIN (U2 fold 2, resumed builder's residual probe — residual-red-before.out
// H7/H8/H9/H10/H12): a resume Request can be ASSEMBLED with no builder name,
// no frame literal, no Resume write and no pointer to a Request. A READ of
// r0.Resume (admitted) yields a nil *ResumePayload whose type is never
// spelled; json.Unmarshal(&p) fills it; then the Request is built from a
// SHAPE: a local alias or defined type of Request (unkeyed literal, or
// converted back), an unkeyed literal whose type is elided inside a []Request
// literal, or a generic struct with a `Resume P` field that is converted — or,
// being unnamed, simply ASSIGNED (no conversion at all). Each form compiles
// and encodes to exactly the NewResume frame (proven in the probe). So,
// outside the admitted set:
//   - a struct type declaring a field named Resume (every file, fail closed,
//     like the Resume key rule: struct identity needs that exact field name);
//   - a type declared from the wire Request (alias or defined);
//   - a conversion to the wire Request;
//   - an unkeyed literal whose type is elided inside a literal whose type
//     mentions the wire Request.
func TestResumeCensusSeesEveryRequestShape(t *testing.T) {
	const (
		write   = "writes a Resume field"
		shape   = "declares a Resume field (a Request shape)"
		typ     = "declares a type from updaterwire.Request"
		convert = "converts to an updaterwire.Request"
	)
	file := func(body string) string {
		return "package api\n\nimport (\n\t\"encoding/json\"\n\n\t\"vl/internal/updaterwire\"\n)\n\nvar _ = json.Unmarshal\n\n" + body + "\n"
	}
	fill := "\tp := r0.Resume\n\t_ = json.Unmarshal([]byte(`{\"job_id\":\"`+job+`\"}`), &p)\n"
	build := func(pre, ret string) string {
		return file(pre + "func Build(r0 updaterwire.Request, job string) updaterwire.Request {\n" + fill + "\treturn " + ret + "\n}")
	}
	fields := "\tVerb updaterwire.Verb\n\tResume P\n"
	for _, tc := range []struct {
		name, body string
		want       []string
	}{
		// the probes (their real-tree plants are residual-red-before.out)
		{"H7 a generic shape, converted", build("type shape[P any] struct {\n"+fields+"}\n\nfunc shapeOf[P any](v updaterwire.Verb, p P) shape[P] { return shape[P]{v, p} }\n\n", "updaterwire.Request(shapeOf(updaterwire.Verbs()[3], p))"), []string{shape, convert}},
		{"H8 an alias of Request, unkeyed", build("type req = updaterwire.Request\n\n", "req{updaterwire.Verbs()[3], nil, nil, nil, p}"), []string{typ}},
		{"H9 an elided unkeyed Request in a slice", build("", "[]updaterwire.Request{{updaterwire.Verbs()[3], nil, nil, nil, p}}[0]"), []string{write}},
		{"H10 a defined type from Request, converted", build("type req updaterwire.Request\n\n", "updaterwire.Request(req{updaterwire.Verbs()[3], nil, nil, nil, p})"), []string{typ, convert}},
		{"H12 an unnamed generic shape, assigned", build("func shapeOf[P any](v updaterwire.Verb, p P) struct {\n"+fields+"} {\n\treturn struct {\n"+fields+"\t}{v, p}\n}\n\n", "shapeOf(updaterwire.Verbs()[3], p)"), []string{shape}},
		// each rule alone
		{"any conversion to Request", file("func f(r updaterwire.Request) updaterwire.Request { return (updaterwire.Request)(r) }"), []string{convert}},
		{"an elided unkeyed Request in a map", file("func f(r updaterwire.Request) map[int]updaterwire.Request {\n\treturn map[int]updaterwire.Request{0: {r.Verb, nil, nil, nil, r.Resume}}\n}"), []string{write}},
		{"an elided unkeyed Request as a map key", file("func f(r updaterwire.Request) map[updaterwire.Request]bool {\n\treturn map[updaterwire.Request]bool{{r.Verb, nil, nil, nil, r.Resume}: true}\n}"), []string{write}},
		{"an elided unkeyed Request inside an elided KEYED map", file("func f(r updaterwire.Request) []map[string]updaterwire.Request {\n\treturn []map[string]updaterwire.Request{{\"a\": {r.Verb, nil, nil, nil, r.Resume}}}\n}"), []string{write}},
		{"an elided unkeyed Request two levels down", file("func f(r updaterwire.Request) [][]updaterwire.Request {\n\treturn [][]updaterwire.Request{{{r.Verb, nil, nil, nil, r.Resume}}}\n}"), []string{write}},
		{"an embedded Resume field", "package agent\n\ntype Resume struct{}\n\ntype shape struct {\n\tVerb string\n\t*Resume\n}\n", []string{shape}},
		{"an unrelated Resume field (fail closed)", "package agent\n\ntype state struct{ Resume bool }\n", []string{shape}},
		{"renamed import", resumeCensusImp("api", "uw", "type req = uw.Request"), []string{typ}},
		{"dot import", resumeCensusImp("api", ".", "func f(r Request) Request { return Request(r) }"), []string{convert}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantResumeOffenders(t, resumeCensusOf(t, map[string]string{"api/s.go": strings.Replace(tc.body, "package agent", "package api", 1)}), "api/s.go", tc.want...)
		})
	}
	for _, ok := range []struct{ name, body string }{
		{"a slice of constructed Requests", file("func f() []updaterwire.Request {\n\treturn []updaterwire.Request{updaterwire.NewStatus(\"\"), updaterwire.NewInstall(\"rel\", \"job-0001abcd\")}\n}")},
		{"an elided KEYED Request without Resume", file("func f() []updaterwire.Request {\n\treturn []updaterwire.Request{{Verb: updaterwire.VerbStatus, Status: &updaterwire.StatusPayload{}}}\n}")},
		{"conversion to another wire type", file("func f(s string) updaterwire.Verb { return updaterwire.Verb(s) }")},
		{"an alias of another wire type", file("type resp = updaterwire.Response\n\nvar _ resp")},
		{"a receipt field that is not exactly Resume", file("type receipt struct {\n\tResumedAt string\n\tResumeJob string\n}\n\nvar _ receipt")},
		{"a parameter named Resume", file("func f(Resume int) int { return Resume }")},
		{"an elided unkeyed literal of an unrelated type", file("var _ = [][2]int{{1, 2}}")},
	} {
		t.Run("admitted "+ok.name, func(t *testing.T) {
			wantResumeOffenders(t, resumeCensusOf(t, map[string]string{"api/s.go": ok.body}), "api/s.go")
		})
	}
}

// PIN (the shape rules inside the wire package): the wire directory is
// admitted wholesale, so a Request shape declared THERE — an exported alias or
// defined type of Request, a conversion to it, a struct with a Resume field,
// an elided unkeyed Request — would hand the app a way to assemble a resume
// the app-side rules no longer see (updaterwire.Req{v, …, p}). Each reaches a
// builder and, being outside resumeWireDecls, is an offender naming it.
func TestResumeCensusPinsWireRequestShapes(t *testing.T) {
	const more = "internal/updaterwire/more.go"
	for _, tc := range []struct{ name, body, decl string }{
		{"an alias of Request", "type Req = Request", "Req"},
		{"a defined type from Request", "type Req Request", "Req"},
		{"a conversion to Request", "func Same(r Request) Request { return Request(r) }", "Same"},
		{"a generic shape with a Resume field", "type Shape[P any] struct {\n\tVerb   Verb\n\tResume P\n}", "Shape"},
		{"an elided unkeyed Request", "func Many(r Request) []Request { return []Request{{r.Verb, r.Resume}} }", "Many"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantResumeOffenders(t, resumeCensusOf(t, map[string]string{more: "package updaterwire\n\n" + tc.body + "\n"}), more,
				"declares "+tc.decl+", which reaches a resume builder outside the pinned set (resumeWireDecls)")
		})
	}
	t.Run("admitted an alias of another wire type", func(t *testing.T) {
		wantResumeOffenders(t, resumeCensusOf(t, map[string]string{more: "package updaterwire\n\ntype Word = Verb\n"}), more)
	})
}
