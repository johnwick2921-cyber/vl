package auth

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"nofx/internal/censuswalk"
)

// M3 red-team H1 census over the REAL tree: an UNSCOPED token (GenerateJWT —
// a user identity, admitted to the credential routes) is minted ONLY where a
// user proves their password or first creates the account: the login and
// register handlers in api/handler_user.go. Every other minting site — the
// Telegram bot (telegram/agent.GenerateBotToken), cmd/gate-jwt, anything
// added later — must mint a MACHINE token (GenerateScopedJWT), which
// authMiddleware denies on the credential, Telegram-config and update routes.
//
// Walk: every non-test .go file of the module, through internal/censuswalk —
// skip names apply at the module ROOT only. The go tool does NOT skip a
// package in api/.hidden, _x or x/testdata/y when it is imported: it compiles
// and links it, so a minter there is a minter (class 258; pinned by
// TestMintCensusesSeeNestedSkipNamedDirs).
func TestOnlyLoginAndRegisterMintUnscopedTokens(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]int{"api/handler_user.go": 2} // register + login
	seen, scanned, err := unscopedMintSites(root)
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 200 {
		t.Fatalf("positive control: scanned only %d Go files — the walk did not cover the module", scanned)
	}
	if seen["api/handler_user.go"] != allowed["api/handler_user.go"] {
		t.Fatalf("positive control: api/handler_user.go mints %d unscoped tokens, want %d (register + login)",
			seen["api/handler_user.go"], allowed["api/handler_user.go"])
	}
	var offenders []string
	for rel, n := range seen {
		if _, ok := allowed[rel]; !ok {
			offenders = append(offenders, rel)
			_ = n
		}
	}
	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Fatalf("unscoped user tokens minted outside login/register (must use GenerateScopedJWT): %v", offenders)
	}
}

// unscopedMintSites walks every non-test .go file of the module at root
// (internal/censuswalk: root-only skips) and counts, per file, the references
// to nofx/auth's GenerateJWT — through any import name, or bare under a
// dot-import / inside package auth. The declaration's own name is not a use.
func unscopedMintSites(root string) (seen map[string]int, scanned int, err error) {
	files, err := censuswalk.NonTestGoFiles(root)
	if err != nil {
		return nil, 0, err
	}
	seen = map[string]int{}
	fset := token.NewFileSet()
	for _, gf := range files {
		rel := gf.Rel
		f, err := parser.ParseFile(fset, gf.Path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, 0, err
		}
		scanned++
		// The names this file reaches package nofx/auth by (an alias counts;
		// a dot-import makes a bare GenerateJWT the auth one).
		aliases, dot := map[string]bool{}, f.Name.Name == "auth"
		for _, im := range f.Imports {
			if strings.Trim(im.Path.Value, `"`) != "nofx/auth" {
				continue
			}
			switch {
			case im.Name == nil:
				aliases["auth"] = true
			case im.Name.Name == ".":
				dot = true
			default:
				aliases[im.Name.Name] = true
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr: // auth.GenerateJWT (any reference, not only a call)
				if id, ok := x.X.(*ast.Ident); ok && x.Sel.Name == "GenerateJWT" && aliases[id.Name] {
					seen[rel]++
				}
				return false
			case *ast.FuncDecl: // the declaration's own name is not a use
				if x.Body != nil {
					ast.Inspect(x.Body, func(m ast.Node) bool {
						if id, ok := m.(*ast.Ident); ok && dot && id.Name == "GenerateJWT" {
							seen[rel]++
						}
						if s, ok := m.(*ast.SelectorExpr); ok {
							if id, ok := s.X.(*ast.Ident); ok && s.Sel.Name == "GenerateJWT" && aliases[id.Name] {
								seen[rel]++
							}
							return false
						}
						return true
					})
				}
				return false
			}
			return true
		})
	}
	return seen, scanned, nil
}
