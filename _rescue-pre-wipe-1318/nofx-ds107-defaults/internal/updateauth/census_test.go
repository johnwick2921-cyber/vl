package updateauth

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"nofx/internal/censuswalk"
)

// ── W-ONE-BUTTON M3 census: who may touch the enrollment, and who may mint ──
//
// Scans every non-test Go file in the module (the ONE root-only walk,
// internal/censuswalk — M3 fold M5):
//
//  1. the enrollment/seen file names are spelled ONLY in paths.go — judged on
//     every string literal AND every constant-folded concatenation run
//     ("device"+".key", a const + "_ids.json", dir + "/ad" + "min.json"),
//     and the key file's name may not be spelled even in FRAGMENTS a
//     variable could join (a path element "device" / "device.*" / "*.key");
//     a directive the compiler reads is code too, so no //go:embed pattern
//     may even be able to MATCH the updater dir or the key file (rule 5).
//     "Spelled" means in the Go source this walk reads: a prose comment is
//     not code, and a non-Go file (.s, .syso, .swig) is not read at all —
//     see WHAT THIS CANNOT PROVE;
//  2. only EXACT importers may import this package — the update handler, the
//     Server wiring, the attended CLI's one file, and the M4 worker package
//     by its exact directory (never a prefix: "internal/updater" also covered
//     the APP-linked internal/updaterwire — red-team 3 #1(b)); never a dot or
//     blank import, never the same file importing it twice, and EVERY name a
//     file imports it under is resolved before a reference is judged
//     (verifier D1: a second name hid every reference through the first);
//  3. every exported identifier an outside file references is CLASSIFIED:
//     restricted ones (enroll, mint, load the key/admin, consume, verify,
//     and every enrollment path helper) are admitted per FILE; open ones
//     (types, errors, limits, validators) to any admitted importer; an
//     unclassified one is refused until someone classifies it (CTO ruling
//     Q1(a): nothing API-side mints a MAC; M3 spec: no API creates or
//     resets the enrollment — the /updates gate, admitted by FILE, READS
//     admin.json and device.key on every request, and nothing else API-side
//     may (PR #200 review #19));
//  4. crypto/hmac is imported ONLY by this package among the packages that
//     reach the updater data dir (an updater/installpath/holdcli import, a
//     data-dir or updater-dir helper, or a path element "updater") — a MAC
//     minted beside the key's directory is a mint, whatever it calls;
//  5. no //go:linkname anywhere in non-test code (verifier D2: the directive
//     binds a local name to updateauth.ComputeMAC with no import at all, and
//     a mode-0 parse never saw the comment). Assembly is not a second route:
//     go1.25.13 refuses an .s file's call to another package's Go function
//     ("relocation target … not defined for ABI0"; the <ABIInternal>
//     selector is "only permitted when compiling runtime") [A, probed
//     2026-09-24], so without a linkname it cannot reach ComputeMAC.
//     Nor any `import "C"` (census-repair verify #3 N2): a cgo preamble is C
//     this census cannot read, and .incbin / #embed / #cgo LDFLAGS read a
//     file at COMPILE time from any package (../ and absolute paths too) —
//     the verifier's N2 compiled the key in with every census green. The
//     module has no cgo [A: go list CgoFiles empty over ./...]; none is
//     admitted, under any import name. And //go:embed, which reads a file
//     under the package directory at COMPILE time (verify #3 N1a/N1b: a
//     root-package embed of data/updater/device.key, then the glob
//     data/upd*r/dev*, each minted with every census green): none in the
//     module-root package, whose directory holds the default data dir; and,
//     in EVERY package, no pattern with an element that can match "updater"
//     or "device.key" (path.Match, after "all:", in bare, "quoted" and
//     `raw` spellings) — the data dir is the directory of DB_PATH anchored on
//     the bot's WorkingDirectory, the checkout it is built in, so
//     DB_PATH=kernel/data.db would put the key in reach of package kernel. An
//     argument list that cannot be parsed is refused. The module's real
//     embeds (kernel/, branding/, agent/) match neither;
//  6. the LOADED device key is used only to verify (verifier D3: the file
//     admitted LoadDeviceKey minted a grant through golang-jwt's HS256 —
//     rule 4 sees only crypto/hmac). In every file that references
//     LoadDeviceKey, the call is bound `key, err := …LoadDeviceKey(dir)` and
//     the key variable appears ONLY as the first argument of
//     updateauth.VerifyMAC, of a LoadAdmin result's PasswordStillBound (the
//     H1 belt's constant-time check), or of the builtin clear — whatever the
//     primitive (crypto/hmac, a JWT signer, hand-rolled SHA-256), the key
//     must be NAMED to reach it (keyFlowOffenders). The ADMISSION is by name,
//     so it is exactly as sound as the resolution of the four names it
//     trusts — the import name at VerifyMAC, the import name a LoadAdmin
//     binding was called through, that binding, and clear. Each is admitted
//     only when NO declaration in the declaration re-binds it — every site
//     go/types declares an identifier, a generic method's receiver type
//     parameters included — and, for clear, none in the package block
//     either. Census-repair verify P1 (a receiver type parameter named
//     clear) and P2 (the import name shadowed before a LoadAdmin binding)
//     were two forms the name check did not read, and each admitted a real
//     mint; the claim is now CHECKED against go/types over every (trusted
//     name × declaration form) by
//     TestUpdateAuthKeyFlowAdmissionMatchesTheCompilersResolution.
//
// Fail-closed side effects, named: the fragment rule refuses ANY literal
// path element that is exactly "device", starts "device." or ends ".key"
// module-wide (a future "server.key" or a JSON field literally "device"
// trips it and must be spelled another way); rule 6 judges by NAME, so an
// unrelated variable sharing the key's name in the same function is
// reported, and a trusted name re-declared ANYWHERE in the declaration —
// even in a scope the admitted call is not in — refuses that call; rule 5
// refuses any //go:embed in the root package whatever its pattern, and
// anywhere a pattern element such as `*`, `*.key` or `u*` that COULD match
// "updater" / "device.key" (spell it narrower: `*.json`, a literal file), and
// any import of "C" even in a file a build tag excludes.
//
// WHAT THIS CANNOT PROVE (M3 fold M4 — stated, not implied): it is a
// syntactic census over the NON-TEST .go FILES of the walk — identifiers,
// imports, the comments the compiler reads (//go:linkname, //go:embed, a cgo
// preamble behind import "C") and constant strings. These pass:
//   - RUN TIME: a file that builds the key's path at run time (fmt.Sprintf
//     with a non-literal, byte arithmetic, a directory listing) or from a
//     name it re-binds to a second constant (constantStrings folds a name to
//     one value), and reads the file itself; one that receives the key bytes
//     or a path through an interface or a function value handed to it by an
//     admitted file; one that reaches the updater dir through a package the
//     census does not relate to it — rule 6 binds only the key LoadDeviceKey
//     returns.
//   - COMPILE TIME, in the tree: a data dir configured (DB_PATH) strictly
//     BELOW a package directory, reached by a //go:embed pattern that names
//     an ANCESTOR of it — a directory pattern embeds its whole subtree
//     (DB_PATH=kernel/st/x.db with //go:embed st) [B: the embed spec; not
//     probed]. Rule 5 covers the default data dir (no embed in the root
//     package) and a data dir AT a package directory (no element that can
//     match "updater" / "device.key"), not a deeper one. And source this walk
//     never opens: an assembly file's #include (cmd/asm preprocesses .s
//     files), a .syso object the linker takes as-is, SWIG files
//     (.swig/.swigcxx, which go build turns into cgo), and every third-party
//     module — the module cache, a vendor/ dir, a go.mod replace pointing
//     outside the tree, a go.work use/replace (the toolchain reads a go.work
//     beside or above the module automatically) [B].
//   - BUILD TIME, outside the source: go generate (it writes source before
//     the build), -toolexec, -ldflags -X (sets a string variable from the
//     build command line), -overlay, GOFLAGS or go.env — none of it is in a
//     .go file [B].
//   - BY HAND: a key file copied into the source tree under another name
//     (then any embed, or a literal of its bytes, carries it), or a .go file
//     placed inside the data dir itself [B].
//   - TEST FILES: a _test.go file is outside the walk by design — it is
//     never linked into the app binary [A: rule 5's controls].
//
// The app process runs as the same uid that owns device.key, so nothing but
// review and this tripwire stops app code from reading the key: the census is
// a BELT against a careless future lane, never a boundary against a
// determined one. What it makes fail loudly is the direct spelling in the Go
// source it reads (a literal, a constant run, a fragment, an embed pattern)
// and the likely drift: a helper reused, a prefix admission, a second import
// name, a linkname, a cgo preamble, an embed in the root package, a MAC
// beside the key, a different HMAC over the loaded key.
var (
	// updateAuthImporterFiles may import the package (exact files).
	updateAuthImporterFiles = map[string]bool{
		"api/handler_updates.go":                 true,
		"api/server.go":                          true,
		"internal/updaterbootstrap/bootstrap.go": true,
	}
	// updateAuthImporterPackages may import the package (exact directories —
	// never a prefix). The M4 worker gets no restricted identifier.
	updateAuthImporterPackages = map[string]bool{
		"internal/updaterworker": true,
	}
	// updateAuthRestricted: identifier → the ONLY files outside the package
	// that may reference it. An empty set = nobody outside.
	updateAuthRestricted = map[string]map[string]bool{
		"Enroll":        {"internal/updaterbootstrap/bootstrap.go": true},
		"Authorize":     {"internal/updaterbootstrap/bootstrap.go": true},
		"ComputeMAC":    {},
		"Message":       {},
		"LoadDeviceKey": {"api/handler_updates.go": true},
		"LoadAdmin":     {"api/handler_updates.go": true, "internal/updaterbootstrap/bootstrap.go": true}, // red-4 #6: the attended CLI reads the incumbent for the REPLACE line
		"VerifyMAC":     {"api/handler_updates.go": true},
		"Consume":       {"api/handler_updates.go": true},
		"NoteExpired":   {"api/handler_updates.go": true}, // red-3 #2: the handler raises the clock floor after VerifyMAC
		"Dir":           {"internal/updaterbootstrap/bootstrap.go": true},
		"AdminPath":     {"internal/updaterbootstrap/bootstrap.go": true},
		"DeviceKeyPath": {"internal/updaterbootstrap/bootstrap.go": true},
		"SeenPath":      {},
	}
	// updateAuthOpen: identifiers any admitted importer may reference.
	updateAuthOpen = map[string]bool{
		"Admin": true, "Grant": true, "Manifest": true, "Verifier": true, "StubVerifier": true,
		"ErrAlreadyEnrolled": true, "ErrBadClock": true, "ErrExpired": true, "ErrMalformed": true,
		"ErrNoDataDir": true, "ErrNoVerifiedManifest": true, "ErrNotEnrolled": true, "ErrPrunedReplay": true,
		"ErrReplay": true, "ErrSeenCorrupt": true, "ErrSeenFull": true, "ErrUnsafe": true,
		"DeviceKeyLen": true, "MaxAuthorizationWindow": true, "MaxEmailLen": true, "MaxJobIDLen": true,
		"MaxReleaseIDLen": true, "MaxSeenEntries": true, "MaxUserIDLen": true, "SeenRetention": true,
		"ValidReleaseID": true, "ValidJobID": true, "NewJobID": true, "ParseInstallRequest": true, "CheckExpiry": true,
	}
	// updaterAreaImports: importing one of these (or a subpackage) reaches the
	// updater data dir.
	updaterAreaImports = []string{
		"internal/updateauth", "internal/updaterwire", "internal/updaterworker",
		"internal/updaterbootstrap", "internal/installpath", "internal/holdcli",
	}
	// updaterAreaIdents: referencing one of these names reaches the data dir
	// or the updater dir.
	updaterAreaIdents = map[string]bool{
		"MaintenanceDataDir": true, "UpdaterDirName": true, "SocketPath": true,
		"SocketPathFor": true, "MaintenanceHoldPath": true, "DataDirFor": true,
	}
)

// macPackageHome is the ONE package that may compute or verify a MAC beside
// the updater dir.
const macPackageHome = "internal/updateauth"

func TestUpdateAuthCensus(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	offenders, scanned, err := updateAuthOffenders(root)
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 100 {
		t.Fatalf("scan saw only %d files — the walk is not covering the module", scanned)
	}
	if len(offenders) > 0 {
		t.Fatalf("update-authorization census:\n%s", strings.Join(offenders, "\n"))
	}
}

// The admission tables are pinned: widening any of them is a reviewed act,
// and every name in them is a real exported identifier of this package (a
// typo would admit nothing and restrict nothing).
func TestUpdateAuthCensusTablesArePinned(t *testing.T) {
	keys := func(m map[string]bool) string {
		var out []string
		for k, v := range m {
			if v {
				out = append(out, k)
			}
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	if got, want := keys(updateAuthImporterFiles), "api/handler_updates.go,api/server.go,internal/updaterbootstrap/bootstrap.go"; got != want {
		t.Fatalf("importer files = %s, want exactly %s", got, want)
	}
	if got, want := keys(updateAuthImporterPackages), "internal/updaterworker"; got != want {
		t.Fatalf("importer packages = %s, want exactly %s", got, want)
	}
	var restricted []string
	for name, files := range updateAuthRestricted {
		restricted = append(restricted, name+"="+keys(files))
	}
	sort.Strings(restricted)
	want := "AdminPath=internal/updaterbootstrap/bootstrap.go;Authorize=internal/updaterbootstrap/bootstrap.go;ComputeMAC=;" +
		"Consume=api/handler_updates.go;DeviceKeyPath=internal/updaterbootstrap/bootstrap.go;Dir=internal/updaterbootstrap/bootstrap.go;" +
		"Enroll=internal/updaterbootstrap/bootstrap.go;LoadAdmin=api/handler_updates.go,internal/updaterbootstrap/bootstrap.go;LoadDeviceKey=api/handler_updates.go;" +
		"Message=;NoteExpired=api/handler_updates.go;SeenPath=;VerifyMAC=api/handler_updates.go"
	if got := strings.Join(restricted, ";"); got != want {
		t.Fatalf("restricted identifiers =\n%s\nwant exactly\n%s", got, want)
	}
	// every classified name exists; nothing is both restricted and open
	exported := map[string]bool{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			switch x := d.(type) {
			case *ast.FuncDecl:
				if x.Recv == nil {
					exported[x.Name.Name] = x.Name.IsExported()
				}
			case *ast.GenDecl:
				for _, s := range x.Specs {
					switch sp := s.(type) {
					case *ast.TypeSpec:
						exported[sp.Name.Name] = sp.Name.IsExported()
					case *ast.ValueSpec:
						for _, n := range sp.Names {
							exported[n.Name] = n.IsExported()
						}
					}
				}
			}
		}
	}
	for name := range updateAuthRestricted {
		if !exported[name] {
			t.Errorf("restricted %s is not an exported identifier of updateauth", name)
		}
		if updateAuthOpen[name] {
			t.Errorf("%s is both restricted and open", name)
		}
	}
	for name := range updateAuthOpen {
		if !exported[name] {
			t.Errorf("open %s is not an exported identifier of updateauth", name)
		}
	}
}

// updateAuthOffenders is the census over every non-test .go file under root.
func updateAuthOffenders(root string) (offenders []string, scanned int, err error) {
	module, err := censuswalk.ModulePath(root)
	if err != nil {
		return nil, 0, err
	}
	const literalHome = "internal/updateauth/paths.go"
	fileNames := []string{adminFileName, deviceKeyName, seenFileName, enrollLockName, seenLockName}
	importers := func(rel string) bool {
		return updateAuthImporterFiles[rel] || updateAuthImporterPackages[path.Dir(rel)]
	}
	type pkgFacts struct {
		hmacFiles  []string
		updaterWhy string          // first reason the package reaches the updater dir; "" = none
		topNames   map[string]bool // every package-level name any file of the package declares
	}
	pkgs := map[string]*pkgFacts{}
	// 6. files that load the device key, judged once every file of their
	// package is parsed (a package-level `clear` may sit in another file)
	type keyHolder struct {
		rel, dir string
		fset     *token.FileSet
		f        *ast.File
		aliases  map[string]bool
	}
	var holders []keyHolder
	seen := map[string]bool{}
	offend := func(line string) {
		if !seen[line] {
			seen[line] = true
			offenders = append(offenders, line)
		}
	}

	files, err := censuswalk.NonTestGoFiles(root)
	if err != nil {
		return nil, 0, err
	}
	for _, file := range files {
		rel := file.Rel
		// ParseComments: directives live in comments (verifier D2 — mode 0
		// dropped them, so a //go:linkname was invisible).
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, file.Path, nil, parser.ParseComments)
		if perr != nil {
			offend(rel + ": cannot be parsed, so it cannot be checked (" + perr.Error() + ")")
			continue
		}
		scanned++

		// 5. //go:linkname binds a local name to ANY package's symbol
		// (nofx/internal/updateauth.ComputeMAC included) with no import, no
		// selector and no restricted identifier, so nothing above could see
		// it. None is admitted in non-test code; the module has none.
		dir := path.Dir(rel)
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if strings.HasPrefix(c.Text, "//go:linkname") {
					offend(rel + ": //go:linkname — binds a symbol of another package past every rule of this census; none is admitted")
				}
				// //go:embed reads a file under the package directory at
				// COMPILE time — no literal, no import, no call (verify #3
				// N1a/N1b), so it is judged here or nowhere.
				args, isEmbed := embedDirectiveArgs(c.Text)
				if !isEmbed {
					continue
				}
				if dir == "." {
					offend(rel + ": //go:embed in the module-root package — its directory holds the default data dir (data/updater/…), so any pattern there can compile the enrollment into the binary; none is admitted")
				}
				patterns, perr := parseEmbedPatterns(args)
				if perr != nil {
					offend(rel + ": //go:embed arguments cannot be parsed, so they cannot be checked (" + perr.Error() + ")")
					continue
				}
				for _, p := range patterns {
					if name, hit := embedPatternReachesEnrollment(p); hit {
						offend(rel + ": //go:embed pattern " + strconv.Quote(p) + " can match the enrollment (" + strconv.Quote(name) + ") — a data dir configured at this package's directory (DB_PATH) puts it in reach; spell the pattern so no element can match \"updater\" or the key file")
					}
				}
			}
		}
		facts := pkgs[dir]
		if facts == nil {
			facts = &pkgFacts{topNames: map[string]bool{}}
			pkgs[dir] = facts
		}
		for _, d := range f.Decls {
			switch x := d.(type) {
			case *ast.FuncDecl:
				if x.Recv == nil {
					facts.topNames[x.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, s := range x.Specs {
					switch sp := s.(type) {
					case *ast.ValueSpec:
						for _, n := range sp.Names {
							facts.topNames[n.Name] = true
						}
					case *ast.TypeSpec:
						facts.topNames[sp.Name.Name] = true
					}
				}
			}
		}
		reach := func(why string) {
			if facts.updaterWhy == "" {
				facts.updaterWhy = rel + " " + why
			}
		}

		// 2. imports; 4. crypto/hmac and updater-area imports. EVERY name the
		// file imports this package under is resolved (verifier D1: one
		// variable overwritten per import let a second name — `ua` beside
		// `updateauth` — hide every reference through the first).
		aliases := map[string]bool{}
		imported := 0
		for _, im := range f.Imports {
			ip, _ := strconv.Unquote(im.Path.Value)
			// 5. cgo (verify #3 N2): the preamble is C the census cannot read,
			// and .incbin / #embed / #cgo LDFLAGS read files at COMPILE time
			// from any package (../ and absolute paths included). The module
			// has none; none is admitted, under any import name.
			if ip == "C" {
				offend(rel + `: imports "C" — a cgo preamble reads files at compile time (.incbin, #embed, #cgo LDFLAGS) past every rule of this census; the module has no cgo and none is admitted`)
			}
			if ip == "crypto/hmac" {
				facts.hmacFiles = append(facts.hmacFiles, rel)
			}
			for _, a := range updaterAreaImports {
				if ip == module+"/"+a || strings.HasPrefix(ip, module+"/"+a+"/") {
					reach("imports " + ip)
				}
			}
			if ip != module+"/internal/updateauth" {
				continue
			}
			if !importers(rel) {
				offend(rel + ": imports nofx/internal/updateauth")
			}
			if imported++; imported == 2 {
				offend(rel + ": imports nofx/internal/updateauth more than once")
			}
			alias := "updateauth"
			if im.Name != nil {
				alias = im.Name.Name
				if alias == "." || alias == "_" {
					offend(rel + ": " + alias + "-imports nofx/internal/updateauth")
					continue
				}
			}
			aliases[alias] = true
		}

		// 1. literals and constant-folded runs; 4. a path element "updater"
		for _, v := range constantStrings(f) {
			if rel != literalHome {
				for _, name := range fileNames {
					if strings.Contains(v, name) {
						offend(rel + ": spells " + name)
					}
				}
			}
			for _, e := range strings.FieldsFunc(v, func(r rune) bool { return r == '/' || r == '\\' }) {
				if rel != literalHome && (e == "device" || strings.HasPrefix(e, "device.") || strings.HasSuffix(e, ".key")) {
					offend(rel + ": spells a fragment of device.key (" + strconv.Quote(e) + ")")
				}
				if e == updaterDirName {
					reach("names the path element \"updater\"")
				}
			}
		}

		holdsKey := false
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident: // 4. data-dir / updater-dir helpers (bare, or a selector's Sel)
				if updaterAreaIdents[x.Name] {
					reach("references " + x.Name)
				}
			case *ast.SelectorExpr: // 3. classified references (any reference, not only calls)
				if id, ok := x.X.(*ast.Ident); ok && aliases[id.Name] {
					name := x.Sel.Name
					holdsKey = holdsKey || name == "LoadDeviceKey"
					if allowed, restricted := updateAuthRestricted[name]; restricted {
						if !allowed[rel] {
							offend(rel + ": references updateauth." + name)
						}
					} else if !updateAuthOpen[name] {
						offend(rel + ": references unclassified updateauth." + name + " — classify it in the census tables")
					}
				}
			}
			return true
		})
		if holdsKey && dir != macPackageHome {
			holders = append(holders, keyHolder{rel: rel, dir: dir, fset: fset, f: f, aliases: aliases})
		}
	}

	// 6. the loaded device key is used only to verify
	for _, h := range holders {
		for _, line := range keyFlowOffenders(h.rel, h.fset, h.f, h.aliases, pkgs[h.dir].topNames["clear"]) {
			offend(line)
		}
	}

	// 4. crypto/hmac beside the updater dir, per package
	var dirs []string
	for d := range pkgs {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		facts := pkgs[d]
		if len(facts.hmacFiles) == 0 || facts.updaterWhy == "" || d == macPackageHome {
			continue
		}
		ip := module
		if d != "." {
			ip = module + "/" + d
		}
		for _, h := range facts.hmacFiles {
			offend(h + ": imports crypto/hmac in package " + ip + ", which references the updater data dir (" + facts.updaterWhy + ") — only " + macPackageHome + " computes a MAC there")
		}
	}
	return offenders, scanned, nil
}

// keyFlowOffenders is rule 6 over one file that references
// updateauth.LoadDeviceKey (through any of its import names): per top-level
// declaration,
//   - every reference to LoadDeviceKey is the whole right-hand side of a `:=`
//     whose first name is a real identifier — the key variable (a function
//     value, `=`, `var`, `_` or an inline use is refused);
//   - every other appearance of a key variable's name anywhere in the
//     declaration is the FIRST argument of exactly one of:
//     <import name>.VerifyMAC(key, …), with the import name not re-declared
//     in the declaration; <admin>.PasswordStillBound(key, …), where <admin>
//     is the `:=` result of <import name>.LoadAdmin with THAT import name
//     not re-declared in the declaration either (census-repair verify P2: a
//     fake bound after a shadowing `updateauth := …` received the key), the
//     call sits inside that binding's scope after it, and the name is
//     declared nowhere else in the declaration; or clear(key) with clear the
//     builtin (declared neither in the declaration — a generic method's
//     receiver type parameters included, census-repair verify P1 — nor at
//     the package's top level).
//
// It is judged by NAME, not by type. Where it REFUSES by name it
// over-reports: a second variable that happens to share the key's name, a
// struct-literal field spelled like it, a key re-bound in a nested scope,
// or a trusted name re-declared in a scope the admitted call is not in.
// Where it ADMITS by name it is only as sound as its resolution of the four
// trusted names (the import name at VerifyMAC, the import name behind a
// LoadAdmin binding, that binding, clear) — which is why the earlier "it can
// only over-report" was FALSE: P1 and P2 were each a declaration form the
// resolution did not read. declaredOther now counts every site go/types
// declares an identifier — checked by
// TestUpdateAuthKeyFlowAdmissionMatchesTheCompilersResolution. Labels have
// their own namespace; an import cannot re-bind a trusted name at an
// admitted call in a file that compiles (`import clear "…"` makes clear(key)
// "use of package clear not in selector", and a second package under the
// updateauth name "updateauth redeclared in this block" — go1.25.13, probed;
// a second import of updateauth itself is rule 2's).
func keyFlowOffenders(rel string, fset *token.FileSet, f *ast.File, aliases map[string]bool, packageDeclaresClear bool) []string {
	var out []string
	at := func(n ast.Node) string { return " (line " + strconv.Itoa(fset.Position(n.Pos()).Line) + ")" }
	for _, decl := range f.Decls {
		parent := map[ast.Node]ast.Node{}
		var stack []ast.Node
		ast.Inspect(decl, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if len(stack) > 0 {
				parent[n] = stack[len(stack)-1]
			}
			stack = append(stack, n)
			return true
		})
		// boundBy: the key variable when sel is the callee of the whole RHS of
		// `name, … := sel(…)`; nil otherwise.
		boundBy := func(sel *ast.SelectorExpr) (*ast.Ident, *ast.AssignStmt) {
			call, ok := parent[sel].(*ast.CallExpr)
			if !ok || call.Fun != sel {
				return nil, nil
			}
			as, ok := parent[call].(*ast.AssignStmt)
			if !ok || as.Tok != token.DEFINE || len(as.Rhs) != 1 || as.Rhs[0] != call || len(as.Lhs) == 0 {
				return nil, nil
			}
			id, ok := as.Lhs[0].(*ast.Ident)
			if !ok || id.Name == "_" {
				return nil, nil
			}
			return id, as
		}
		keyBinds := map[*ast.Ident]bool{}
		keyNames := map[string]bool{}
		adminBinds := map[*ast.Ident]*ast.AssignStmt{}
		adminVia := map[*ast.Ident]string{} // the import name each LoadAdmin binding was called through
		ast.Inspect(decl, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || !aliases[x.Name] {
				return true
			}
			switch sel.Sel.Name {
			case "LoadDeviceKey":
				id, _ := boundBy(sel)
				if id == nil {
					out = append(out, rel+": updateauth.LoadDeviceKey must be bound as `key, err := updateauth.LoadDeviceKey(dir)` and the key used only to verify"+at(sel))
					return true
				}
				keyBinds[id], keyNames[id.Name] = true, true
			case "LoadAdmin":
				if id, as := boundBy(sel); id != nil {
					adminBinds[id] = as
					adminVia[id] = sel.X.(*ast.Ident).Name
				}
			}
			return true
		})
		if len(keyNames) == 0 {
			continue
		}
		// declaredOther: name is declared (or assigned) in the declaration by
		// something other than the idents in except.
		declaredOther := func(name string, except func(*ast.Ident) bool) bool {
			found := false
			check := func(id *ast.Ident) {
				if id != nil && id.Name == name && (except == nil || !except(id)) {
					found = true
				}
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.AssignStmt:
					for _, l := range x.Lhs {
						if id, ok := l.(*ast.Ident); ok {
							check(id)
						}
					}
				case *ast.ValueSpec:
					for _, id := range x.Names {
						check(id)
					}
				case *ast.Field:
					for _, id := range x.Names {
						check(id)
					}
				case *ast.RangeStmt:
					if k, ok := x.Key.(*ast.Ident); ok {
						check(k)
					}
					if v, ok := x.Value.(*ast.Ident); ok {
						check(v)
					}
				case *ast.TypeSpec:
					check(x.Name)
				case *ast.FuncDecl:
					check(x.Name)
					for _, id := range receiverTypeParams(x) {
						check(id)
					}
				}
				return true
			})
			return found
		}
		// inScopeOf: use lies after the `:=` and inside the block it declares in.
		inScopeOf := func(use ast.Node, as *ast.AssignStmt) bool {
			scope := parent[as]
			switch s := scope.(type) {
			case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
			case *ast.IfStmt:
				if s.Init != as {
					return false
				}
			case *ast.SwitchStmt:
				if s.Init != as {
					return false
				}
			case *ast.TypeSwitchStmt:
				if s.Init != as {
					return false
				}
			case *ast.ForStmt:
				if s.Init != as {
					return false
				}
			default:
				return false
			}
			return use.Pos() > as.End() && use.End() <= scope.End()
		}
		admitted := func(id *ast.Ident) bool {
			call, ok := parent[id].(*ast.CallExpr)
			if !ok || len(call.Args) == 0 || call.Args[0] != id {
				return false
			}
			switch fn := call.Fun.(type) {
			case *ast.Ident: // the builtin clear, and nothing that shadows it
				return fn.Name == "clear" && len(call.Args) == 1 && !packageDeclaresClear && !declaredOther("clear", nil)
			case *ast.SelectorExpr:
				x, ok := fn.X.(*ast.Ident)
				if !ok {
					return false
				}
				if fn.Sel.Name == "VerifyMAC" {
					return aliases[x.Name] && !declaredOther(x.Name, nil)
				}
				if fn.Sel.Name != "PasswordStillBound" {
					return false
				}
				for b, as := range adminBinds {
					if b.Name == x.Name && inScopeOf(call, as) && !declaredOther(adminVia[b], nil) &&
						!declaredOther(x.Name, func(d *ast.Ident) bool { _, isBind := adminBinds[d]; return isBind && d.Name == b.Name }) {
						return true
					}
				}
			}
			return false
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || !keyNames[id.Name] || keyBinds[id] {
				return true
			}
			if s, ok := parent[id].(*ast.SelectorExpr); ok && s.Sel == id {
				return true // a field or method name, never the variable
			}
			if !admitted(id) {
				out = append(out, rel+": the loaded device key "+strconv.Quote(id.Name)+" is used outside updateauth.VerifyMAC / <LoadAdmin result>.PasswordStillBound / clear"+at(id))
			}
			return true
		})
	}
	return out
}

// receiverTypeParams returns the type parameters a generic method's receiver
// DECLARES: `func (r *T[K, clear]) m()` declares K and clear for the whole
// method inside an index expression of the receiver type — not in any
// ast.Field, so a Field walk never sees them (census-repair verify P1:
// `func (vccP1Box[clear]) …` made clear(key) a conversion that carried the
// key out). go/types' unpackRecv reads unparen, an optional *, unparen, then
// the indices; parentheses and stars are unwrapped here in any order and
// depth (a superset). A non-identifier index does not compile ("receiver
// type parameter … must be an identifier") and declares nothing.
func receiverTypeParams(fd *ast.FuncDecl) []*ast.Ident {
	if fd.Recv == nil {
		return nil
	}
	var out []*ast.Ident
	for _, fld := range fd.Recv.List {
		t := fld.Type
		for {
			if p, ok := t.(*ast.ParenExpr); ok {
				t = p.X
				continue
			}
			if st, ok := t.(*ast.StarExpr); ok {
				t = st.X
				continue
			}
			break
		}
		var indices []ast.Expr
		switch x := t.(type) {
		case *ast.IndexExpr:
			indices = []ast.Expr{x.Index}
		case *ast.IndexListExpr:
			indices = x.Indices
		}
		for _, e := range indices {
			if id, ok := e.(*ast.Ident); ok {
				out = append(out, id)
			}
		}
	}
	return out
}

// embedDirectiveArgs reports whether a comment is a //go:embed directive and
// returns the text after "//go:embed". The toolchain reads "//go:embed"
// followed by a space (cmd/compile noder, which also takes the bare form) or a
// tab (go/build findEmbed) — go1.25.13 [A read]; this accepts all three, a
// superset. Prose ("// uses go:embed", "//go:embedded") is not a directive.
func embedDirectiveArgs(text string) (string, bool) {
	const d = "//go:embed"
	if !strings.HasPrefix(text, d) {
		return "", false
	}
	rest := text[len(d):]
	if rest == "" || rest[0] == ' ' || rest[0] == '\t' {
		return rest, true
	}
	return "", false
}

// parseEmbedPatterns splits //go:embed arguments exactly as go/build's
// parseGoEmbed does: space-separated bare patterns, "double-quoted" Go
// strings and `back-quoted` strings. A malformed list is an error (the caller
// refuses it: what cannot be parsed cannot be checked).
func parseEmbedPatterns(args string) ([]string, error) {
	var out []string
	for args = strings.TrimLeftFunc(args, unicode.IsSpace); args != ""; args = strings.TrimLeftFunc(args, unicode.IsSpace) {
		var p string
		switch args[0] {
		case '`':
			q, rest, ok := strings.Cut(args[1:], "`")
			if !ok {
				return nil, errors.New("unterminated `")
			}
			p, args = q, rest
		case '"':
			i := 1
			for ; i < len(args) && args[i] != '"'; i++ {
				if args[i] == '\\' {
					i++
				}
			}
			if i >= len(args) {
				return nil, errors.New("unterminated \"")
			}
			q, err := strconv.Unquote(args[:i+1])
			if err != nil {
				return nil, err
			}
			p, args = q, args[i+1:]
		default:
			i := strings.IndexFunc(args, unicode.IsSpace)
			if i < 0 {
				i = len(args)
			}
			p, args = args[:i], args[i:]
		}
		if args != "" {
			if r, _ := utf8.DecodeRuneInString(args); !unicode.IsSpace(r) {
				return nil, errors.New("pattern not followed by a space: " + args)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// embedPatternReachesEnrollment reports whether any element of a //go:embed
// pattern (after an "all:" prefix) can match the updater dir name or the key
// file name. The key sits at <dataDir>/updater/device.key and a pattern
// matching a directory embeds its whole subtree, so from a package directory
// that IS the data dir every pattern that reaches the key has an element
// matching "updater" (or, from a package placed inside the updater dir,
// "device.key"). Judged per element, at any position — fail closed: `*`
// matches both and is refused wherever it stands. A malformed element is
// refused too.
func embedPatternReachesEnrollment(pattern string) (string, bool) {
	for _, e := range strings.Split(strings.TrimPrefix(pattern, "all:"), "/") {
		// Case-folded: on a case-insensitive filesystem the toolchain resolves
		// a literal pattern by Lstat, so UPDATER/DEVICE.KEY reaches the key.
		// Lowering a class ([A-Z]) can only widen the match — fail closed.
		e = strings.ToLower(e)
		for _, name := range []string{updaterDirName, deviceKeyName} {
			ok, err := path.Match(e, name)
			if err != nil {
				return "malformed element " + strconv.Quote(e), true
			}
			if ok {
				return name, true
			}
		}
	}
	return "", false
}

// constantStrings returns every string the file spells as a constant: each
// string literal, and each run of adjacent constant operands in every
// maximal `+` chain (so "device"+".key", a file-local const + "_ids.json" and
// the "/ad"+"min.json" inside dir+"/ad"+"min.json" all fold). Names bound in
// the file to a foldable string (const, var, :=, =) fold too — by NAME, not
// by scope, and each name to ONE value: the last binding the fold passes
// saw. That is NOT "can only over-report" (the earlier claim, false like
// rule 6's — census-repair verify #3): `a := "ad"`, then `{ a := "zz" }`,
// then dir + "/" + a + "min.json" folds to "/zzmin.json" and the admin.json
// spelling is missed [A, probed 2026-09-24: the same file with one binding
// of a IS reported]. A name re-bound to a second constant is therefore in
// WHAT THIS CANNOT PROVE, beside run-time construction.
func constantStrings(f *ast.File) []string {
	env := map[string]string{}
	bind := func(names []*ast.Ident, values []ast.Expr) bool {
		changed := false
		if len(names) != len(values) {
			return false
		}
		for i, n := range names {
			if v, ok := foldString(values[i], env); ok {
				if old, had := env[n.Name]; !had || old != v {
					env[n.Name] = v
					changed = true
				}
			}
		}
		return changed
	}
	for pass := 0; pass < 4; pass++ {
		changed := false
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.ValueSpec:
				changed = bind(x.Names, x.Values) || changed
			case *ast.AssignStmt:
				var names []*ast.Ident
				for _, l := range x.Lhs {
					id, ok := l.(*ast.Ident)
					if !ok {
						return true
					}
					names = append(names, id)
				}
				changed = bind(names, x.Rhs) || changed
			}
			return true
		})
		if !changed {
			break
		}
	}
	var out []string
	inner := map[*ast.BinaryExpr]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if b, ok := n.(*ast.BinaryExpr); ok && b.Op == token.ADD {
			for _, side := range []ast.Expr{b.X, b.Y} {
				if c, ok := unparen(side).(*ast.BinaryExpr); ok && c.Op == token.ADD {
					inner[c] = true
				}
			}
		}
		return true
	})
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				if v, err := strconv.Unquote(x.Value); err == nil {
					out = append(out, v)
				}
			}
		case *ast.BinaryExpr:
			if x.Op != token.ADD || inner[x] {
				return true
			}
			run, have := "", false
			for _, op := range flattenAdd(x) {
				if v, ok := foldString(op, env); ok {
					run, have = run+v, true
					continue
				}
				if have {
					out = append(out, run)
				}
				run, have = "", false
			}
			if have {
				out = append(out, run)
			}
		}
		return true
	})
	return out
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

func flattenAdd(e ast.Expr) []ast.Expr {
	if b, ok := unparen(e).(*ast.BinaryExpr); ok && b.Op == token.ADD {
		return append(flattenAdd(b.X), flattenAdd(b.Y)...)
	}
	return []ast.Expr{e}
}

func foldString(e ast.Expr, env map[string]string) (string, bool) {
	switch x := unparen(e).(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			v, err := strconv.Unquote(x.Value)
			return v, err == nil
		}
	case *ast.Ident:
		v, ok := env[x.Name]
		return v, ok
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			a, ok1 := foldString(x.X, env)
			b, ok2 := foldString(x.Y, env)
			if ok1 && ok2 {
				return a + b, true
			}
		}
	}
	return "", false
}
