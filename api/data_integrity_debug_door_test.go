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
	// m452: the list used to name PlaceLimitEntryWithStop, which does not exist,
	// and miss the two real stop/split entry placers - so a route calling
	// nt.PlaceStopEntryWithLimit(...) or nt.PlaceLimitEntryWithSplit(...) passed.
	// Every name here is now checked against the broker's declared methods
	// (below), so a stale or invented name fails instead of silently banning
	// nothing; and the scan covers agent/, which holds the chat door's trader
	// interface (it declares OpenLong/OpenShort and must never CALL them).
	banned := map[string]bool{
		"OpenLong": true, "OpenShort": true, "OpenWithBracket": true,
		"PlaceLimitEntry": true, "PlaceLimitEntryWithSplit": true,
		"PlaceStopEntry": true, "PlaceStopEntryWithLimit": true,
		"SendSignal": true, "DebugPlaceTestTrade": true,
		"placeEntry": true, "placeEntryWith": true, "placeLimitEntry": true, "placeStopEntry": true,
	}
	declared := declaredBrokerMethods(t, "../trader/ninjatrader/tcp_trader.go")
	for name := range banned {
		if name == "SendSignal" { // a TCPServer method, not a TCPTrader one
			continue
		}
		if !declared[name] {
			t.Errorf("banned name %q is not a method declared in trader/ninjatrader/tcp_trader.go: the ban covers nothing (fix the list)", name)
		}
	}
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) < 10 {
		t.Fatalf("fixture: api production files not found (%d, %v)", len(files), err)
	}
	agentFiles, aerr := filepath.Glob("../agent/*.go")
	if aerr != nil || len(agentFiles) < 5 {
		t.Fatalf("fixture: agent production files not found (%d, %v)", len(agentFiles), aerr)
	}
	files = append(files, agentFiles...)
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

// declaredBrokerMethods lists the method names declared on *TCPTrader in file.
func declaredBrokerMethods(t *testing.T, file string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	out := map[string]bool{}
	for _, d := range af.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil {
			out[fd.Name.Name] = true
		}
	}
	if len(out) < 20 {
		t.Fatalf("fixture: only %d methods parsed from %s", len(out), file)
	}
	return out
}
