package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// m445 review D1. The api package must never reach a broker entry method
// itself: every order an API route puts on the wire goes through an AutoTrader
// seam that crosses the data-integrity verdict (admitEntry, TestArmPlace,
// TestArmPlaceStop, DebugNTTestTrade). POST /api/debug/nt-test-trade used to
// call TCPTrader.DebugPlaceTestTrade directly — a MARKET entry with no check,
// reachable on the production instance. This pin fails the build if any api
// production file names a broker entry method again.
func TestAPIHasNoDirectBrokerEntryDoor(t *testing.T) {
	banned := map[string]bool{
		"OpenLong": true, "OpenShort": true, "OpenWithBracket": true,
		"PlaceLimitEntry": true, "PlaceStopEntry": true, "SendSignal": true,
		"DebugPlaceTestTrade": true, "PlaceLimitEntryWithStop": true,
	}
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) < 10 {
		t.Fatalf("fixture: api production files not found (%d, %v)", len(files), err)
	}
	scanned := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		scanned++
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, f, nil, 0)
		if perr != nil {
			t.Fatalf("%s: %v", f, perr)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && banned[sel.Sel.Name] {
				t.Errorf("%s: %s reaches the broker entry method %s directly — route it through an AutoTrader seam that crosses the data-integrity verdict",
					f, fset.Position(sel.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
	if scanned < 10 {
		t.Fatalf("fixture: only %d api production files scanned", scanned)
	}
}
