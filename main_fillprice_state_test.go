package main

import (
	"go/ast"
	"go/token"
	"strconv"
	"testing"
)

// m55r3-h2 (release #55 round-3 review, tests-are-real lens, defect 2): deleting
// `trader.SetFillPriceStatePath(cfg.DBPath + ".fillprice.json")` from main.go
// passed every suite - the m451 restart pin calls SetFillPriceStatePath itself,
// so it proves the mechanism and not that main arms it. Without the call the
// fill_price strikes and the owner's acknowledgement are memory-only and the next
// kill -9 deploy silently lifts a standing pause.
//
// The state path is set by an UNCONDITIONAL top-level statement of main(), with
// the DB path beside which the file lives, AFTER the os.Args DB-path override and
// BEFORE the traders load (their start runs the data-truth boot wiring, which
// reads the path through trader.RegisterFillPrice).
//
// MUTANT: delete the SetFillPriceStatePath line from main.go, or nest it in an
// `if`, or move it below LoadTradersFromStore -> RED.
func TestMainSetsTheFillPriceStatePathBeforeTradersLoad(t *testing.T) {
	stmts := mainTopLevel(t)
	set, load, override := -1, -1, -1
	for i, st := range stmts {
		if c := directCall(st); c != nil && isSel(c.Fun, "trader", "SetFillPriceStatePath") && len(c.Args) == 1 {
			if be, ok := c.Args[0].(*ast.BinaryExpr); ok && be.Op == token.ADD && isSel(be.X, "cfg", "DBPath") {
				if lit, ok := be.Y.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if v, err := strconv.Unquote(lit.Value); err == nil && v == ".fillprice.json" {
						set = i
					}
				}
			}
		}
		if load < 0 && containsCall(st, "traderManager", "LoadTradersFromStore") {
			load = i
		}
		ast.Inspect(st, func(m ast.Node) bool {
			if as, ok := m.(*ast.AssignStmt); ok && len(as.Lhs) == 1 && isSel(as.Lhs[0], "cfg", "DBPath") {
				if ix, ok := as.Rhs[0].(*ast.IndexExpr); ok && isSel(ix.X, "os", "Args") {
					override = i
				}
			}
			return true
		})
	}
	if set < 0 {
		t.Fatal(`main() must call trader.SetFillPriceStatePath(cfg.DBPath + ".fillprice.json") as an unconditional TOP-LEVEL statement: without it the fill_price strikes and the owner's ack are memory-only and a kill -9 deploy lifts a standing pause`)
	}
	if load < 0 || override < 0 {
		t.Fatalf("anchors missing: load=%d override=%d", load, override)
	}
	if !(override < set && set < load) {
		t.Fatalf("order must be: DB path override (stmt %d) < SetFillPriceStatePath (stmt %d) < LoadTradersFromStore (stmt %d)", override, set, load)
	}
}
