package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"nofx/internal/censuswalk"
)

// ── M3 FOLD-M3-A — the users-table WRITER CENSUS (CTO 1790239512054) ───────
//
// auth/retire.go reads users.updated_at as the account's credential epoch:
// every session token issued at or before it is refused on every protected
// route. That reading is only as true as the set of code that writes the
// users table. A new writer that touches the row — a gorm Save, an Updates
// with a struct, a raw UPDATE, a migration default — silently retires (or
// un-retires) every session, and nothing else in the tree would notice.
//
// So this census walks every non-test .go file in the module (the ruled
// roots store/, api/, telegram/, cmd/ and main.go must each be walked; the
// rest of the module is walked too, which can only over-report) and finds
// every write to the users table:
//
//   - gorm: a write verb (Create, CreateInBatches, Save, Update, Updates,
//     UpdateColumn, UpdateColumns, Delete, FirstOrCreate) whose argument, or
//     whose Model(...)/Table(...) receiver chain, names the users model —
//     &User{}, &store.User{} (any import alias), new(User), a variable
//     declared or assigned as User / *User / []User, Table("users"). A bare
//     User in ANY package counts: gorm names a struct User's table "users"
//     whatever package declares it.
//   - schema: AutoMigrate or a Migrator verb on the users model.
//   - raw SQL: a string literal that writes the table (UPDATE users, DELETE
//     FROM users, INSERT [OR …] INTO users, REPLACE INTO users, ALTER TABLE
//     users, DROP TABLE users, TRUNCATE users).
//   - a call to a UserStore method that is itself a writer (derived to a
//     fixpoint), through Store.User(), a variable holding it, or the
//     UserStore receiver.
//
// and pins the (file, function, kind) set EXACTLY. Widening it is a reviewed
// act, not a drive-by line in another PR. The users.updated_at writers among
// them (the retire epoch's sources): UpdatePassword (the credential change),
// Create / EnsureAdmin / handleRegister (gorm stamps updated_at ==
// created_at — CredentialEpoch reads that as "never changed"), and
// initTables (the Postgres ALTER … updated_at DEFAULT CURRENT_TIMESTAMP and
// AutoMigrate: a row that predates the column gets whatever the migration
// gives it — the legacy phantom epoch, pinned by FOLD-M3-B in api/). The
// deletes (DeleteAll, handleResetAccount) remove rows; a token whose row is
// gone is refused everywhere (api tokenRetirement).
//
// WHAT THIS CANNOT SEE (named, not chased — M3 ha2 verify defect 1; the
// census is a syntactic BELT, and a writer it misses can only move the epoch
// FORWARD, which retires sessions early: fail closed, never a bypass). Each
// compiles and plants clean [A, planted 2026-09-24 by the ha2 verifier; a
// type-aware go/packages cross-check found exactly this census's sites at
// HEAD, and the 23 sites it could not decide statically touch no users row]:
//   - a User-typed STRUCT FIELD written through (db.Save(&b.u));
//   - SQL built at run time (Exec(fmt.Sprintf("UPDATE %s …", tbl))), a
//     schema-qualified table (UPDATE main.users) or UPDATE OR IGNORE users;
//   - Table(constIdent) — a table named through a constant, not a literal;
//   - a generic or `any` wrapper (save(db, v any)) called with a User;
//   - another model whose TableName() returns "users", or a type alias
//     (type Account = User);
//   - a method value (save := db.Save; save(&u));
//   - a UserStore built directly (store.NewUserStore(db).UpdatePassword) or
//     held in a struct field, not reached through Store.User().
//
// The directory-skip gap (a writer under api/.hidden, _x, x/testdata/y) WAS
// closed: the walk is internal/censuswalk (root-only skips), pinned by
// TestUsersWriterCensusSeesNestedSkipNamedDirs.
var reviewedUsersTableWriters = []string{
	// PR #200 F5 (reviewed 2026-09-24): resetPasswordLockedOutAdvice, the 410
	// body of POST /api/reset-password. The app never executes it: it is the
	// statement a locked-out OWNER runs by hand, and it DOES move updated_at
	// — deliberately, so every pre-reset session retires
	// (TestResetPasswordAdviceRetiresPreResetSessionsOnSQLite runs it as
	// served). Listed because the census's question is "what can move the
	// credential epoch", and this can.
	"api/handler_user.go · (package level) · raw SQL UPDATE users",
	"api/handler_user.go · (*Server).handleChangePassword · calls UserStore.UpdatePassword",
	"api/handler_user.go · (*Server).handleRegister · calls UserStore.Create",
	"api/handler_user.go · (*Server).handleResetAccount · gorm Delete",
	"store/store.go · (*Store).initTables · calls UserStore.initTables",
	"store/user.go · (*UserStore).Create · gorm Create",
	"store/user.go · (*UserStore).DeleteAll · gorm Delete",
	"store/user.go · (*UserStore).EnsureAdmin · calls UserStore.Create",
	"store/user.go · (*UserStore).UpdatePassword · gorm Updates",
	"store/user.go · (*UserStore).initTables · raw SQL ALTER TABLE users",
	"store/user.go · (*UserStore).initTables · schema AutoMigrate",
}

func TestUsersTableWriterCensus(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	sites, walked, err := usersTableWriters(root)
	if err != nil {
		t.Fatal(err)
	}
	// The ruled roots must each have been walked — a census that walked
	// nothing (wrong root, a skipped dir) would otherwise pass vacuously.
	for _, want := range []string{"store", "api", "telegram", "cmd", "main.go"} {
		if walked[want] == 0 {
			t.Fatalf("census never walked %s (walked %v) — it is not covering the ruled roots", want, walked)
		}
	}
	want := append([]string(nil), reviewedUsersTableWriters...)
	sort.Strings(want)
	if strings.Join(sites, "\n") != strings.Join(want, "\n") {
		t.Fatalf("users-table writers at HEAD != the reviewed set.\n  unexpected: %v\n  missing:    %v\n(a new writer can move users.updated_at — the credential epoch every session is judged by, auth/retire.go; review it, then pin it here)",
			censusMinus(sites, want), censusMinus(want, sites))
	}
}

// The census is proved on a synthetic module: the clean tree reports only
// its two store writers (positive control); each planted writer shape — in
// every ruled root, through an import alias, through a new UserStore method
// and its caller — is reported with its (file, function); reads, other
// models and prose naming "users" are not.
func TestUsersWriterCensusCatchesEveryWriterShape(t *testing.T) {
	write := func(root, rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const storeUser = "package store\n\nimport \"gorm.io/gorm\"\n\n" +
		"type User struct{ ID, Email string }\n\ntype UserStore struct{ db *gorm.DB }\n\n" +
		"func (s *UserStore) Create(user *User) error { return s.db.Create(user).Error }\n\n" +
		"func (s *UserStore) GetByID(id string) (*User, error) {\n\tvar u User\n\treturn &u, s.db.Where(\"id = ?\", id).First(&u).Error\n}\n\n" +
		"func (s *UserStore) Count() (n int64) { s.db.Model(&User{}).Count(&n); return }\n\n" +
		"func (s *UserStore) UpdatePassword(id, h string) error {\n\treturn s.db.Model(&User{}).Where(\"id = ?\", id).Updates(map[string]any{\"password_hash\": h}).Error\n}\n"
	const storeStore = "package store\n\ntype Store struct{ u *UserStore }\n\nfunc (s *Store) User() *UserStore { return s.u }\n"
	base := func() string {
		root := t.TempDir()
		write(root, "go.mod", "module nofx\n\ngo 1.25\n")
		write(root, "store/user.go", storeUser)
		write(root, "store/store.go", storeStore)
		write(root, "api/handler.go", "package api\n\nimport \"nofx/store\"\n\n"+
			"func who(st *store.Store) { u, _ := st.User().GetByID(\"x\"); _ = u; _ = st.User().Count() }\n\n"+
			"var help = \"DESTRUCTIVE — delete all users/traders/strategies\"\n")
		write(root, "telegram/bot.go", "package telegram\n\nimport \"nofx/store\"\n\n"+
			"func other(db interface{ Create(any) any }) { db.Create(&store.Trader{}) }\n")
		write(root, "cmd/tool/main.go", "package main\n\nfunc main() {}\n")
		write(root, "main.go", "package main\n\nfunc main() {}\n")
		return root
	}
	clean := []string{
		"store/user.go · (*UserStore).Create · gorm Create",
		"store/user.go · (*UserStore).UpdatePassword · gorm Updates",
	}
	sites, walked, err := usersTableWriters(base())
	if err != nil || strings.Join(sites, "\n") != strings.Join(clean, "\n") {
		t.Fatalf("clean synthetic module: sites=%v err=%v — want exactly %v", sites, err, clean)
	}
	for _, r := range []string{"store", "api", "telegram", "cmd", "main.go"} {
		if walked[r] == 0 {
			t.Fatalf("synthetic module: %s not walked (%v)", r, walked)
		}
	}
	for name, c := range map[string]struct{ rel, body, want string }{
		"gorm Model(&store.User{}).Update in api": {"api/x.go",
			"package api\n\nimport \"nofx/store\"\n\nfunc setEmail(db dbish) { db.Model(&store.User{}).Where(\"id = ?\", 1).Update(\"email\", \"e\") }\n",
			"api/x.go · setEmail · gorm Update"},
		"gorm Table(\"users\").Updates in telegram": {"telegram/x.go",
			"package telegram\n\nfunc touch(db dbish) { db.Table(\"users\").Where(\"id = ?\", 1).Updates(map[string]any{\"updated_at\": 1}) }\n",
			"telegram/x.go · touch · gorm Updates"},
		"raw UPDATE users in cmd": {"cmd/tool/fix.go",
			"package main\n\nfunc fix(db dbish) { db.Exec(`UPDATE users SET updated_at = ?`, 1) }\n",
			"cmd/tool/fix.go · fix · raw SQL UPDATE users"},
		"raw DELETE FROM users in main.go": {"main.go",
			"package main\n\nfunc main() {}\n\nfunc wipe(db dbish) { db.Exec(\"delete from users\") }\n",
			"main.go · wipe · raw SQL DELETE FROM users"},
		"raw INSERT OR REPLACE INTO users": {"api/y.go",
			"package api\n\nconst q = \"INSERT OR REPLACE INTO users (id) VALUES (?)\"\n",
			"api/y.go · (package level) · raw SQL INSERT OR REPLACE INTO users"},
		"db.Save(&user) on a store.User value": {"api/z.go",
			"package api\n\nimport \"nofx/store\"\n\nfunc resave(db dbish) {\n\tvar user store.User\n\tdb.Save(&user)\n}\n",
			"api/z.go · resave · gorm Save"},
		"Save on a User parameter": {"telegram/p.go",
			"package telegram\n\nimport \"nofx/store\"\n\nfunc keep(db dbish, u *store.User) { db.Save(u) }\n",
			"telegram/p.go · keep · gorm Save"},
		"Delete(&store.User{}) inside a closure": {"api/reset.go",
			"package api\n\nimport \"nofx/store\"\n\ntype Server struct{}\n\nfunc (s *Server) reset(tx func(func(db dbish) error) error) {\n\t_ = tx(func(db dbish) error { db.Delete(&store.User{}); return nil })\n}\n",
			"api/reset.go · (*Server).reset · gorm Delete"},
		"an import alias": {"api/alias.go",
			"package api\n\nimport st \"nofx/store\"\n\nfunc mk(db dbish) { db.Create(&st.User{ID: \"x\"}) }\n",
			"api/alias.go · mk · gorm Create"},
		"a new caller of UpdatePassword": {"telegram/pw.go",
			"package telegram\n\nimport \"nofx/store\"\n\nfunc rotate(st *store.Store) { _ = st.User().UpdatePassword(\"id\", \"h\") }\n",
			"telegram/pw.go · rotate · calls UserStore.UpdatePassword"},
		"a caller through a UserStore variable": {"cmd/tool/reg.go",
			"package main\n\nimport \"nofx/store\"\n\nfunc reg(st *store.Store) {\n\tus := st.User()\n\t_ = us.Create(nil)\n}\n",
			"cmd/tool/reg.go · reg · calls UserStore.Create"},
		"a new UserStore writer method": {"store/user_touch.go",
			"package store\n\nfunc (s *UserStore) Touch(id string) error { return s.db.Model(&User{}).Where(\"id = ?\", id).UpdateColumn(\"updated_at\", 1).Error }\n",
			"store/user_touch.go · (*UserStore).Touch · gorm UpdateColumn"},
		"schema AutoMigrate on the users model": {"store/mig.go",
			"package store\n\nfunc (s *Store) mig(db dbish) { db.AutoMigrate(&User{}) }\n",
			"store/mig.go · (*Store).mig · schema AutoMigrate"},
		"new(User) through FirstOrCreate": {"store/foc.go",
			"package store\n\nfunc seed(db dbish) { db.FirstOrCreate(new(User)) }\n",
			"store/foc.go · seed · gorm FirstOrCreate"},
		"fetch through the UserStore, then Save": {"api/fetch.go",
			"package api\n\nimport \"nofx/store\"\n\ntype Server struct{ store *store.Store; db dbish }\n\nfunc (s *Server) rename(id string) {\n\tu, err := s.store.User().GetByID(id)\n\t_ = err\n\tu.Email = \"x\"\n\ts.db.Save(u)\n}\n",
			"api/fetch.go · (*Server).rename · gorm Save"},
		"range over GetAll, then Updates by Model(&u)": {"telegram/all.go",
			"package telegram\n\nimport \"nofx/store\"\n\nfunc stamp(st *store.Store, db dbish) {\n\tusers, _ := st.User().GetAll()\n\tfor _, u := range users {\n\t\tdb.Model(&u).Updates(map[string]any{\"updated_at\": 1})\n\t}\n}\n",
			"telegram/all.go · stamp · gorm Updates"},
		"index into GetAll, then Save": {"cmd/tool/first.go",
			"package main\n\nimport \"nofx/store\"\n\nfunc first(st *store.Store, db dbish) {\n\tusers, _ := st.User().GetAll()\n\tu := users[0]\n\tdb.Save(&u)\n}\n",
			"cmd/tool/first.go · first · gorm Save"},
		"a bare User declared by another package": {"agent/u.go",
			"package agent\n\ntype User struct{ ID string }\n\nfunc save(db dbish) { db.Save(&User{}) }\n",
			"agent/u.go · save · gorm Save"},
	} {
		t.Run(name, func(t *testing.T) {
			root := base()
			write(root, c.rel, c.body)
			sites, _, err := usersTableWriters(root)
			if err != nil {
				t.Fatal(err)
			}
			hit := false
			for _, s := range sites {
				hit = hit || s == c.want
			}
			if !hit {
				t.Fatalf("census missed the planted writer: sites = %v, want one == %q", sites, c.want)
			}
		})
	}
	// A new UserStore writer method makes its CALLERS writers too (fixpoint).
	root := base()
	write(root, "store/user_touch.go", "package store\n\nfunc (s *UserStore) Touch(id string) error { return s.db.Exec(\"UPDATE users SET updated_at = 1 WHERE id = ?\", id).Error }\n")
	write(root, "api/touch.go", "package api\n\nimport \"nofx/store\"\n\nfunc poke(st *store.Store) { _ = st.User().Touch(\"id\") }\n")
	sites, _, err = usersTableWriters(root)
	if err != nil {
		t.Fatal(err)
	}
	if !censusHas(sites, "api/touch.go · poke · calls UserStore.Touch") || !censusHas(sites, "store/user_touch.go · (*UserStore).Touch · raw SQL UPDATE users") {
		t.Fatalf("a new writer method and its caller: sites = %v", sites)
	}
	// Negative controls: reads, another model's writes and prose that names
	// the table stay out (the clean base already carries one of each).
	root = base()
	write(root, "api/reads.go", "package api\n\nimport \"nofx/store\"\n\n"+
		"func reads(db dbish, st *store.Store) {\n\tvar us []store.User\n\tdb.Model(&store.User{}).Where(\"x\").Find(&us)\n\tdb.Delete(&store.Trader{})\n\t_ = \"failed to delete users: %w\"\n\t_, _ = st.User().GetByID(\"x\")\n}\n")
	if sites, _, _ := usersTableWriters(root); strings.Join(sites, "\n") != strings.Join(clean, "\n") {
		t.Fatalf("reads / other models / prose were reported as users writers: %v", sites)
	}
	// An unparseable file is reported, never skipped silently.
	root = base()
	write(root, "api/broken.go", "package api\n\nfunc (\n")
	if sites, _, _ := usersTableWriters(root); !censusHasPrefix(sites, "api/broken.go · cannot be parsed") {
		t.Fatalf("an unparseable file must be reported: %v", sites)
	}
}

var (
	gormWriteVerbs = map[string]bool{
		"Create": true, "CreateInBatches": true, "Save": true, "Update": true, "Updates": true,
		"UpdateColumn": true, "UpdateColumns": true, "Delete": true, "FirstOrCreate": true,
	}
	migratorVerbs = map[string]bool{
		"CreateTable": true, "DropTable": true, "AddColumn": true, "DropColumn": true,
		"AlterColumn": true, "RenameColumn": true, "RenameTable": true,
	}
	usersWriteSQL = regexp.MustCompile("(?is)\\b(update|delete\\s+from|insert\\s+(?:or\\s+\\w+\\s+)?into|replace\\s+into|alter\\s+table|drop\\s+table(?:\\s+if\\s+exists)?|truncate(?:\\s+table)?)\\s+[\"'`]?users(?:[\"'`\\s(;]|$)")
)

type usersCensusFile struct {
	rel     string
	f       *ast.File
	inStore bool            // package store: a bare User / UserStore is the store's
	aliases map[string]bool // the names this file reaches nofx/store by
}

// usersTableWriters returns every users-table write site under root as
// sorted, de-duplicated "file · function · kind" strings (unparseable files
// are reported as sites too, never skipped), and how many files it walked
// under each top-level root (a root-level file counts under its own name).
func usersTableWriters(root string) (sites []string, walked map[string]int, err error) {
	walked = map[string]int{}
	var files []usersCensusFile
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			sites = append(sites, s)
		}
	}
	// The ONE census walk (internal/censuswalk): skip names apply at the
	// module ROOT only — a package in api/.hidden, _x or x/testdata/y is
	// compiled and linked like any other (M5 class; pinned by
	// TestUsersWriterCensusSeesNestedSkipNamedDirs).
	goFiles, err := censuswalk.NonTestGoFiles(root)
	if err != nil {
		return nil, nil, err
	}
	for _, gf := range goFiles {
		rel := gf.Rel
		walked[strings.SplitN(rel, "/", 2)[0]]++
		f, perr := parser.ParseFile(token.NewFileSet(), gf.Path, nil, parser.SkipObjectResolution)
		if perr != nil {
			add(rel + " · cannot be parsed, so it cannot be checked (" + perr.Error() + ")")
			continue
		}
		cf := usersCensusFile{rel: rel, f: f, inStore: f.Name.Name == "store", aliases: map[string]bool{}}
		for _, im := range f.Imports {
			if ip, _ := strconv.Unquote(im.Path.Value); ip == "nofx/store" {
				switch {
				case im.Name == nil:
					cf.aliases["store"] = true
				case im.Name.Name == ".":
					cf.inStore = true
				default:
					cf.aliases[im.Name.Name] = true
				}
			}
		}
		files = append(files, cf)
	}
	// UserStore writer methods, derived to a fixpoint: a UserStore method is
	// a writer when it writes directly or calls another UserStore writer.
	writers := map[string]bool{}
	var found []string
	for i := 0; i < 16; i++ {
		found = nil
		next := map[string]bool{}
		for _, cf := range files {
			for _, s := range censusFile(cf, writers) {
				found = append(found, s.String())
				if s.userStoreMethod != "" {
					next[s.userStoreMethod] = true
				}
			}
		}
		if len(next) == len(writers) {
			break
		}
		writers = next
	}
	for _, s := range found {
		add(s)
	}
	sort.Strings(sites)
	return sites, walked, nil
}

type usersWriteSite struct {
	file, fn, kind  string
	userStoreMethod string // the UserStore method this site sits in ("" = none)
}

func (s usersWriteSite) String() string { return s.file + " · " + s.fn + " · " + s.kind }

func censusFile(cf usersCensusFile, writers map[string]bool) []usersWriteSite {
	var out []usersWriteSite
	isUserType := func(e ast.Expr) bool {
		for {
			switch x := e.(type) {
			case *ast.StarExpr:
				e = x.X
				continue
			case *ast.ArrayType:
				e = x.Elt
				continue
			case *ast.ParenExpr:
				e = x.X
				continue
			case *ast.Ident:
				return x.Name == "User" // a bare User in any package: gorm names its table "users"
			case *ast.SelectorExpr:
				id, ok := x.X.(*ast.Ident)
				return ok && cf.aliases[id.Name] && x.Sel.Name == "User"
			}
			return false
		}
	}
	isUserStoreType := func(e ast.Expr) bool {
		if s, ok := e.(*ast.StarExpr); ok {
			e = s.X
		}
		switch x := e.(type) {
		case *ast.Ident:
			return cf.inStore && x.Name == "UserStore"
		case *ast.SelectorExpr:
			id, ok := x.X.(*ast.Ident)
			return ok && cf.aliases[id.Name] && x.Sel.Name == "UserStore"
		}
		return false
	}
	for _, decl := range cf.f.Decls {
		fnName, recvStore, recvName := "(package level)", false, ""
		userVars, storeVars := map[string]bool{}, map[string]bool{}
		var node ast.Node = decl
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fnName = fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) == 1 {
				rt := fd.Recv.List[0].Type
				star := ""
				if s, ok := rt.(*ast.StarExpr); ok {
					star, rt = "*", s.X
				}
				if id, ok := rt.(*ast.Ident); ok {
					fnName = "(" + star + id.Name + ")." + fd.Name.Name
				}
				if isUserStoreType(fd.Recv.List[0].Type) && len(fd.Recv.List[0].Names) == 1 {
					recvStore, recvName = true, fd.Recv.List[0].Names[0].Name
				}
			}
		}
		isUserStoreValue := func(e ast.Expr) bool {
			switch x := e.(type) {
			case *ast.CallExpr: // anything.User() — Store.User returns the UserStore
				sel, ok := x.Fun.(*ast.SelectorExpr)
				return ok && sel.Sel.Name == "User" && len(x.Args) == 0
			case *ast.Ident:
				return storeVars[x.Name] || (recvStore && x.Name == recvName)
			}
			return false
		}
		var isUserValue func(e ast.Expr) bool
		isUserValue = func(e ast.Expr) bool {
			switch x := e.(type) {
			case *ast.UnaryExpr:
				return x.Op == token.AND && isUserValue(x.X)
			case *ast.StarExpr:
				return isUserValue(x.X)
			case *ast.ParenExpr:
				return isUserValue(x.X)
			case *ast.IndexExpr: // users[0]
				return isUserValue(x.X)
			case *ast.CompositeLit:
				return x.Type != nil && isUserType(x.Type)
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 {
					return isUserType(x.Args[0])
				}
				// what a UserStore method returns (GetByID, GetAll, …) is
				// read as a User: fetch-modify-Save is the likeliest new writer
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok && isUserStoreValue(sel.X) {
					return true
				}
			case *ast.Ident:
				return userVars[x.Name]
			}
			return false
		}
		// Local typing, scope-insensitive (over-approximates): parameters,
		// var declarations and assignments that hold a User or a UserStore.
		ast.Inspect(node, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncType:
				if x.Params != nil {
					for _, fl := range x.Params.List {
						for _, nm := range fl.Names {
							if isUserType(fl.Type) {
								userVars[nm.Name] = true
							}
							if isUserStoreType(fl.Type) {
								storeVars[nm.Name] = true
							}
						}
					}
				}
			case *ast.ValueSpec:
				for i, nm := range x.Names {
					if x.Type != nil && isUserType(x.Type) {
						userVars[nm.Name] = true
					}
					if x.Type != nil && isUserStoreType(x.Type) {
						storeVars[nm.Name] = true
					}
					if i < len(x.Values) && isUserValue(x.Values[i]) {
						userVars[nm.Name] = true
					}
					if i < len(x.Values) && isUserStoreValue(x.Values[i]) {
						storeVars[nm.Name] = true
					}
				}
			case *ast.AssignStmt:
				for i, l := range x.Lhs {
					id, ok := l.(*ast.Ident)
					if !ok {
						continue
					}
					var r ast.Expr
					if len(x.Rhs) == len(x.Lhs) {
						r = x.Rhs[i]
					} else if len(x.Rhs) == 1 {
						r = x.Rhs[0] // u, err := st.User().GetByID(id)
					}
					if r != nil && isUserValue(r) {
						userVars[id.Name] = true
					}
					if r != nil && len(x.Rhs) == len(x.Lhs) && isUserStoreValue(r) {
						storeVars[id.Name] = true
					}
				}
			case *ast.RangeStmt: // for _, u := range users
				if isUserValue(x.X) {
					for _, kv := range []ast.Expr{x.Key, x.Value} {
						if id, ok := kv.(*ast.Ident); ok {
							userVars[id.Name] = true
						}
					}
				}
			}
			return true
		})
		chainNamesUsers := func(e ast.Expr) bool {
			for {
				switch x := e.(type) {
				case *ast.ParenExpr:
					e = x.X
					continue
				case *ast.SelectorExpr:
					e = x.X
					continue
				case *ast.CallExpr:
					sel, ok := x.Fun.(*ast.SelectorExpr)
					if !ok {
						return false
					}
					if sel.Sel.Name == "Model" && len(x.Args) == 1 && isUserValue(x.Args[0]) {
						return true
					}
					if sel.Sel.Name == "Table" && len(x.Args) >= 1 {
						if lit, ok := x.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							v, _ := strconv.Unquote(lit.Value)
							if f := strings.Fields(strings.ToLower(v)); len(f) > 0 && strings.Trim(f[0], "\"`") == "users" {
								return true
							}
						}
					}
					e = sel.X
					continue
				}
				return false
			}
		}
		site := func(kind string) {
			s := usersWriteSite{file: cf.rel, fn: fnName, kind: kind}
			if recvStore {
				if fd, ok := decl.(*ast.FuncDecl); ok {
					s.userStoreMethod = fd.Name.Name
				}
			}
			out = append(out, s)
		}
		ast.Inspect(node, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					v, _ := strconv.Unquote(x.Value)
					if m := usersWriteSQL.FindStringSubmatch(v); m != nil {
						site("raw SQL " + strings.ToUpper(strings.Join(strings.Fields(m[1]), " ")) + " users")
					}
				}
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				verb := sel.Sel.Name
				if isUserStoreValue(sel.X) {
					if writers[verb] {
						site("calls UserStore." + verb)
					}
					return true
				}
				argUser := false
				for _, a := range x.Args {
					argUser = argUser || isUserValue(a)
				}
				switch {
				case gormWriteVerbs[verb] && (argUser || chainNamesUsers(sel.X)):
					site("gorm " + verb)
				case verb == "AutoMigrate" && argUser:
					site("schema AutoMigrate")
				case migratorVerbs[verb] && (argUser || (len(x.Args) > 0 && isUsersLit(x.Args[0]))):
					site("schema Migrator." + verb)
				}
			}
			return true
		})
	}
	return out
}

func isUsersLit(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	v, _ := strconv.Unquote(lit.Value)
	return strings.EqualFold(strings.TrimSpace(v), "users")
}

func censusMinus(a, b []string) []string {
	in := map[string]bool{}
	for _, x := range b {
		in[x] = true
	}
	var out []string
	for _, x := range a {
		if !in[x] {
			out = append(out, x)
		}
	}
	return out
}

func censusHas(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func censusHasPrefix(xs []string, p string) bool {
	for _, x := range xs {
		if strings.HasPrefix(x, p) {
			return true
		}
	}
	return false
}
