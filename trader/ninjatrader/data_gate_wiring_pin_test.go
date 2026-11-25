package ninjatrader

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"
)

// m452 (release #55) - THE PRODUCTION SWITCH-ON OF THE m446 CHECKS, PINNED.
//
// The existing boot pin (datatruth_boot_test.go) calls WireDataTruthChecks
// directly, so deleting the ONE call in bar_persist_wire.go left every test green:
// the ready edge was never reached, and entries blocked after the 5 min grace with
// "not wired" on every symbol - fail-closed, but only discoverable in production.
// This reads the SOURCE through go/parser (a commented-out call is not in the
// AST) and pins that the call is live, inside the bar-backfill goroutine closure
// of WireBarPersistence, and that it comes after the server is bound.
//
// MUTANTS (shown RED in the m452 report):
//   M7  delete / comment out WireDataTruthChecks(bh, server) in bar_persist_wire.go
//   M8  delete startDataTruthWarmer() from WireDataTruthChecks

func liveCallPos(t *testing.T, file, funcName, callee string) (found bool, pos token.Pos, line int, inFuncLit bool) {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	fs := token.NewFileSet()
	af, err := parser.ParseFile(fs, file, b, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	for _, d := range af.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != funcName || fd.Body == nil {
			continue
		}
		var stack []ast.Node
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch e := c.Fun.(type) {
			case *ast.Ident:
				name = e.Name
			case *ast.SelectorExpr:
				name = e.Sel.Name
			}
			if name == callee && !found {
				found, pos, line = true, c.Pos(), fs.Position(c.Pos()).Line
				for _, s := range stack[:len(stack)-1] {
					if _, ok := s.(*ast.FuncLit); ok {
						inFuncLit = true
					}
				}
			}
			return true
		})
	}
	return
}

func TestWireBarPersistenceSwitchesTheDataTruthChecksOn(t *testing.T) {
	ok, pos, line, lit := liveCallPos(t, "bar_persist_wire.go", "WireBarPersistence", "WireDataTruthChecks")
	if !ok {
		t.Fatal("WireBarPersistence must call WireDataTruthChecks(bh, server) as live code: without it the eight m446 checks are never registered and DataTruthReady never flips (every entry blocks 'not wired' after the boot grace)")
	}
	if !lit {
		t.Fatalf("WireDataTruthChecks at bar_persist_wire.go:%d must run in the bar-backfill goroutine closure, where the TCP server exists - not on the caller's goroutine before the server is up", line)
	}
	// it comes after the server is bound for the hole tracker, in the same closure
	ok2, bindPos, _, _ := liveCallPos(t, "bar_persist_wire.go", "WireBarPersistence", "Store")
	if !ok2 || pos < bindPos {
		t.Fatalf("WireDataTruthChecks (line %d) must come after barHolesServer.Store(server): the server it reads from must be bound first", line)
	}
}

func TestWireDataTruthChecksStartsTheWarmer(t *testing.T) {
	ok, _, _, _ := liveCallPos(t, "datatruth_boot.go", "WireDataTruthChecks", "startDataTruthWarmer")
	if !ok {
		t.Fatal("WireDataTruthChecks must start the minute-edge warmer (live call): without it the first asker of every minute - an entry path, or the armed pass under armedPassMu - pays the ~100 ms store read")
	}
	if ok, _, _, _ := liveCallPos(t, "datatruth_boot.go", "startDataTruthWarmer", "WarmSwingInputs"); !ok {
		t.Fatal("the warmer must call checks.WarmSwingInputs")
	}
}
