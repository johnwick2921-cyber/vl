package auth

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"nofx/internal/censuswalk"
)

// M3 verifier defect 4 — CTO ruling 1790243040753: ValidateJWT refuses a
// token whose iat is more than ClockLeeway (60 s) in the future, and the one
// leeway jwt v5 applies to iat, nbf and exp is bounded at 60 s. The
// production-router pins are api/token_clock_window_test.go; this is the
// parser-level table plus the "the server never mints a future iat" census.

func mintTimes(t *testing.T, iat, nbf, exp time.Time) string {
	t.Helper()
	rc := jwt.RegisteredClaims{}
	if !iat.IsZero() {
		rc.IssuedAt = jwt.NewNumericDate(iat)
	}
	if !nbf.IsZero() {
		rc.NotBefore = jwt.NewNumericDate(nbf)
	}
	if !exp.IsZero() {
		rc.ExpiresAt = jwt.NewNumericDate(exp)
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{UserID: "u-clock", Email: "clock@example.test", RegisteredClaims: rc}).SignedString(JWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestValidateJWTClockWindow(t *testing.T) {
	prev := JWTSecret
	SetJWTSecret("clock-window-test-secret-0123456789")
	t.Cleanup(func() { JWTSecret = prev })
	if ClockLeeway != 60*time.Second {
		t.Fatalf("ClockLeeway = %v — the ruling is 60 s", ClockLeeway)
	}
	now := time.Now()
	past, later := now.Add(-time.Minute), now.Add(time.Hour)
	cases := []struct {
		name          string
		iat, nbf, exp time.Time
		want          error // nil = admitted
	}{
		{"iat now+2min", now.Add(2 * time.Minute), past, later, jwt.ErrTokenUsedBeforeIssued},
		{"iat now+30s (leeway)", now.Add(30 * time.Second), past, later, nil},
		{"exp now-30s (leeway)", past, past, now.Add(-30 * time.Second), nil},
		{"exp now-2min", now.Add(-time.Hour), past, now.Add(-2 * time.Minute), jwt.ErrTokenExpired},
		{"nbf now+30s (leeway)", past, now.Add(30 * time.Second), later, nil},
		{"nbf now+2min", past, now.Add(2 * time.Minute), later, jwt.ErrTokenNotValidYet},
		// jwt v5 WithIssuedAt verifies iat only when PRESENT (validator.go
		// verifyIssuedAt required=false): a token with no iat passes the
		// parser; the H2 retire rule (RetiredBy(nil) = true) refuses it.
		{"no iat (present-only; H2 refuses it)", time.Time{}, past, later, nil},
	}
	for _, c := range cases {
		_, err := ValidateJWT(mintTimes(t, c.iat, c.nbf, c.exp))
		switch {
		case c.want == nil && err != nil:
			t.Errorf("%s: refused (%v), want admitted", c.name, err)
		case c.want != nil && !errors.Is(err, c.want):
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	if !RetiredBy(nil, now.Add(-time.Hour), now.Add(-time.Hour)) {
		t.Fatal("RetiredBy(nil iat) = false — the no-iat token the parser admits must be retired by H2")
	}
}

// The blacklist entry lives through exp + ClockLeeway — the last instant the
// parser can still admit the token.
func TestBlacklistEntryOutlivesExpByTheLeeway(t *testing.T) {
	now := time.Now()
	BlacklistToken("clock-window-inside", now.Add(-30*time.Second))
	if !IsTokenBlacklisted("clock-window-inside") {
		t.Fatal("a token logged out 30 s past exp is no longer blacklisted — the parser still admits it for 30 s more")
	}
	BlacklistToken("clock-window-beyond", now.Add(-2*time.Minute))
	if IsTokenBlacklisted("clock-window-beyond") {
		t.Fatal("an entry 2 min past exp is still held — the parser refuses that token on its own")
	}
}

// "The server never mints a future iat" (CTO ruling 1790243040753), as a
// structural census over the REAL tree (every non-test .go file, the walk of
// TestOnlyLoginAndRegisterMintUnscopedTokens):
//
//  1. jwt.NewWithClaims / jwt.New is called in production code ONLY in
//     auth/auth.go (signToken) — every minter (GenerateJWT, GenerateScopedJWT,
//     telegram/agent.GenerateBotToken, cmd/gate-jwt) goes through it;
//  2. in every production file that imports golang-jwt, every IssuedAt and
//     NotBefore — a composite-literal key or an assignment — is exactly
//     jwt.NewNumericDate(time.Now()): no offset, no variable, no other clock;
//     and no MapClaims "iat"/"nbf" key is written at all.
func TestServerNeverMintsAFutureIat(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	minters, iatSites, offenders, scanned, err := futureIatCensus(root)
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 200 {
		t.Fatalf("positive control: scanned only %d Go files — the walk did not cover the module", scanned)
	}
	if iatSites["auth/auth.go"] != 2 {
		t.Fatalf("positive control: auth/auth.go has %d IssuedAt/NotBefore sites, want 2 (signToken)", iatSites["auth/auth.go"])
	}
	var extra []string
	for rel, n := range minters {
		if rel != "auth/auth.go" || n != 1 {
			extra = append(extra, rel+"×"+strconv.Itoa(n))
		}
	}
	sort.Strings(extra)
	if minters["auth/auth.go"] != 1 || len(extra) > 0 {
		t.Errorf("jwt.NewWithClaims/jwt.New in production code must be exactly auth/auth.go×1 (signToken); got %v", extra)
	}
	sort.Strings(offenders)
	for _, o := range offenders {
		t.Errorf("a token minted with a non-now iat/nbf: %s", o)
	}
}

// futureIatCensus walks every non-test .go file of the module at root
// (internal/censuswalk: root-only skips — a minter in api/.hidden, _x or
// x/testdata/y is compiled and linked like any other, class 258) and returns
// the jwt.NewWithClaims/jwt.New call sites per file, the IssuedAt/NotBefore
// sites per file, and every site that is not exactly
// jwt.NewNumericDate(time.Now()).
func futureIatCensus(root string) (minters, iatSites map[string]int, offenders []string, scanned int, err error) {
	const jwtPath = "github.com/golang-jwt/jwt/v5"
	files, err := censuswalk.NonTestGoFiles(root)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	minters, iatSites = map[string]int{}, map[string]int{}
	fset := token.NewFileSet()
	for _, gf := range files {
		rel := gf.Rel
		f, err := parser.ParseFile(fset, gf.Path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, nil, nil, 0, err
		}
		scanned++
		jwtName, timeName := "", ""
		for _, im := range f.Imports {
			path, _ := strconv.Unquote(im.Path.Value)
			name := ""
			if im.Name != nil {
				name = im.Name.Name
			}
			switch path {
			case jwtPath:
				if name == "" {
					name = "jwt"
				}
				jwtName = name
			case "time":
				if name == "" {
					name = "time"
				}
				timeName = name
			}
		}
		if jwtName == "" {
			continue
		}
		if jwtName == "." || jwtName == "_" {
			offenders = append(offenders, rel+": golang-jwt imported as "+jwtName+" — the census cannot read it")
			continue
		}
		isSel := func(e ast.Expr, pkg, name string) bool {
			s, ok := e.(*ast.SelectorExpr)
			if !ok {
				return false
			}
			id, ok := s.X.(*ast.Ident)
			return ok && pkg != "" && id.Name == pkg && s.Sel.Name == name
		}
		// jwt.NewNumericDate(time.Now()) exactly.
		isNowDate := func(e ast.Expr) bool {
			c, ok := e.(*ast.CallExpr)
			if !ok || !isSel(c.Fun, jwtName, "NewNumericDate") || len(c.Args) != 1 {
				return false
			}
			a, ok := c.Args[0].(*ast.CallExpr)
			return ok && isSel(a.Fun, timeName, "Now") && len(a.Args) == 0
		}
		check := func(field string, v ast.Expr) {
			iatSites[rel]++
			if !isNowDate(v) {
				offenders = append(offenders, fset.Position(v.Pos()).String()+": "+field+" is not "+jwtName+".NewNumericDate("+timeName+".Now())")
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if isSel(x.Fun, jwtName, "NewWithClaims") || isSel(x.Fun, jwtName, "New") {
					minters[rel]++
				}
			case *ast.KeyValueExpr:
				if k, ok := x.Key.(*ast.Ident); ok && (k.Name == "IssuedAt" || k.Name == "NotBefore") {
					check(k.Name, x.Value)
				}
				if k, ok := x.Key.(*ast.BasicLit); ok && k.Kind == token.STRING {
					if s, _ := strconv.Unquote(k.Value); s == "iat" || s == "nbf" {
						offenders = append(offenders, fset.Position(k.Pos()).String()+": a MapClaims "+s+" key — mint through auth.signToken")
					}
				}
			case *ast.AssignStmt:
				for i, l := range x.Lhs {
					if s, ok := l.(*ast.SelectorExpr); ok && (s.Sel.Name == "IssuedAt" || s.Sel.Name == "NotBefore") && i < len(x.Rhs) {
						check(s.Sel.Name, x.Rhs[i])
					}
				}
			}
			return true
		})
	}
	return minters, iatSites, offenders, scanned, nil
}
