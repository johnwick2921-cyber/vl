package main

import (
	"go/ast"
	"go/token"
	"strconv"
	"testing"
)

// Release #55 round 3 (review 5, defect 2): TestM451FillPriceStrikesAndAckSurviveARestart
// calls trader.SetFillPriceStatePath itself, so it proves the mechanism and not
// that main() wires it - deleting the line from main.go left every suite green
// while a kill -9 deploy would silently lift a standing fill_price pause (the
// strikes and the owner's acknowledgement would be memory-only).
//
// The state-file path is set by an UNCONDITIONAL top-level statement of main(),
// over cfg.DBPath + ".fillprice.json", AFTER the os.Args DB-path override (the
// file lives beside the DB the bot really opens) and BEFORE the traders load
// (RegisterFillPrice reads it at the data-truth boot wiring, which runs when the
// first NinjaTrader trader is built inside LoadTradersFromStore).
//
// MUTANT: delete trader.SetFillPriceStatePath(...) from main.go -> RED.
func TestMainSetsTheFillPriceStatePathBeforeTradersLoad(t *testing.T) {
	stmts := mainTopLevel(t)
	set, load, override := -1, -1, -1
	for i, st := range stmts {
		if c := directCall(st); c != nil && isSel(c.Fun, "trader", "SetFillPriceStatePath") && len(c.Args) == 1 {
			if bin, ok := c.Args[0].(*ast.BinaryExpr); ok && bin.Op == token.ADD && isSel(bin.X, "cfg", "DBPath") {
				if lit, ok := bin.Y.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil && s == ".fillprice.json" {
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
		t.Fatal("main() must call trader.SetFillPriceStatePath(cfg.DBPath + \".fillprice.json\") as an unconditional top-level statement - without it the fill_price strikes and the owner's ack are memory-only and a kill -9 deploy lifts a standing pause")
	}
	if load < 0 || override < 0 {
		t.Fatalf("pin is stale: load=%d override=%d", load, override)
	}
	if !(override < set && set < load) {
		t.Fatalf("order wrong: want DB-path override (%d) < SetFillPriceStatePath (%d) < LoadTradersFromStore (%d)", override, set, load)
	}
}
