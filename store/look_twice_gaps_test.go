package store

import (
	"fmt"
	"path/filepath"
	"testing"
)

// ── LOOK TWICE, the mutants no test saw (full test of 9acafc81f) ─────────────
// Each case is one filter of OwnDeathFor / OverCloseFor, named by its mutant id in
// fulltest-71/C1-table.log. Both decide orders: a wrong own death holds a naked
// position for 60 s, a missed one lets a close be sent on a dead position.

type gapRows struct {
	t  *testing.T
	st *Store
	n  int
}

func newGapRows(t *testing.T) *gapRows {
	t.Helper()
	st, err := New(filepath.Join(t.TempDir(), "gaps.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &gapRows{t: t, st: st}
}

type gapRow struct {
	trader, account, side, status string
	entryQty, qty                 float64
	entry, exit                   int64
}

func (g *gapRows) row(sym string, r gapRow) int64 {
	g.t.Helper()
	g.n++
	p := &TraderPosition{TraderID: r.trader, ExchangeType: "ninjatrader", ExchangePositionID: fmt.Sprint("gap-", g.n), Symbol: sym,
		Side: r.side, Quantity: r.qty, EntryQuantity: r.entryQty, EntryPrice: 100, EntryTime: r.entry, ExitTime: r.exit,
		EntryOrderID: fmt.Sprint("e-", g.n), Leverage: 1, Status: r.status, Source: "t", Account: r.account}
	if err := g.st.GormDB().Create(p).Error; err != nil {
		g.t.Fatal(err)
	}
	// exactly these values, whatever a create hook or a column default made of a zero
	if err := g.st.GormDB().Model(&TraderPosition{}).Where("id = ?", p.ID).Updates(map[string]any{
		"status": r.status, "entry_quantity": r.entryQty, "quantity": r.qty, "entry_time": r.entry, "exit_time": r.exit, "account": r.account}).Error; err != nil {
		g.t.Fatal(err)
	}
	return p.ID
}

func (g *gapRows) receipt(sym string, r NT8ExitReceipt) NT8ExitReceipt {
	g.t.Helper()
	g.n++
	r.Symbol = sym
	if r.ID == "" {
		r.ID = fmt.Sprintf("r%03d", g.n)
	}
	if r.Account == "" {
		r.Account = "Sim101"
	}
	if r.Side == "" {
		r.Side = "SHORT"
	}
	if err := g.st.GormDB().Create(&r).Error; err != nil {
		g.t.Fatal(err)
	}
	return r
}

func TestOwnDeathFilters(t *testing.T) {
	g := newGapRows(t)
	const me = "t1"
	ours := gapRow{me, "Sim101", "SHORT", "CLOSED", 2, 2, 1, 100}
	newer := func(r gapRow) gapRow { r.exit = 900; return r }
	for name, c := range map[string]struct {
		with func(sym string)
		open bool // want OpenRow
	}{
		"E06 an open blank-account row of another trader":     {func(s string) { g.row(s, gapRow{"t2", "", "SHORT", "OPEN", 1, 1, 1, 0}) }, false},
		"E06 an open blank-account row of ours (control)":     {func(s string) { g.row(s, gapRow{me, "", "SHORT", "OPEN", 1, 1, 1, 0}) }, true},
		"E06 an open row of another trader here (control)":    {func(s string) { g.row(s, gapRow{"t2", "Sim101", "SHORT", "OPEN", 1, 1, 1, 0}) }, true},
		"E11 an open row of the other side":                   {func(s string) { g.row(s, gapRow{me, "Sim101", "LONG", "OPEN", 1, 1, 1, 0}) }, false},
		"E11 an open row of another account":                  {func(s string) { g.row(s, gapRow{me, "Sim102", "SHORT", "OPEN", 1, 1, 1, 0}) }, false},
		"E07 a newer closed row of another account":           {func(s string) { g.row(s, newer(gapRow{me, "Sim102", "SHORT", "CLOSED", 9, 9, 1, 0})) }, false},
		"E08 an older closed row":                             {func(s string) { g.row(s, gapRow{me, "Sim101", "SHORT", "CLOSED", 9, 9, 1, 50}) }, false},
		"E09 a newer closed row of another trader":            {func(s string) { g.row(s, newer(gapRow{"t2", "Sim101", "SHORT", "CLOSED", 9, 9, 1, 0})) }, false},
		"E10 a newer closed row of the other side":            {func(s string) { g.row(s, newer(gapRow{me, "Sim101", "LONG", "CLOSED", 9, 9, 1, 0})) }, false},
		"a newer closed row of another symbol":                {func(s string) { g.row(s+"x", newer(gapRow{me, "Sim101", "SHORT", "CLOSED", 9, 9, 1, 0})) }, false},
		"E08 a newer row with the same exit (the id decides)": {func(string) {}, false},
	} {
		t.Run(name, func(t *testing.T) {
			sym := fmt.Sprint("S", g.n)
			if name[:3] == "E08" { // an older row, written BEFORE ours as it happened
				g.row(sym, gapRow{me, "Sim101", "SHORT", "CLOSED", 9, 9, 1, ours.exit})
			}
			id := g.row(sym, ours)
			g.receipt(sym, NT8ExitReceipt{PositionID: id, Applied: true, Quantity: 1})
			g.receipt(sym, NT8ExitReceipt{PositionID: id, Applied: true, Quantity: 1})
			g.receipt(sym, NT8ExitReceipt{PositionID: id, Applied: false, Quantity: 5}) // E12
			c.with(sym)
			d, err := g.st.Position().OwnDeathFor(me, "Sim101", sym, "short")
			if err != nil {
				t.Fatal(err)
			}
			if c.open {
				if !d.OpenRow || d.ByReceipts() {
					t.Fatalf("an open row is there: %+v", d)
				}
				return
			}
			if d.OpenRow || d.Last == nil || d.Last.ID != id || d.ReceiptQty != 2 || d.EntryQty != 2 || !d.ByReceipts() {
				t.Fatalf("want row %d dead by its own 2 of 2: %+v", id, d)
			}
		})
	}

	// E13 + a blank account: an old row's original size is its quantity.
	id := g.row("OLD", gapRow{me, "", "SHORT", "CLOSED", 0, 3, 1, 100})
	g.receipt("OLD", NT8ExitReceipt{PositionID: id, Applied: true, Quantity: 3})
	if d, err := g.st.Position().OwnDeathFor(me, "Sim101", "OLD", "SHORT"); err != nil || d.Last == nil || d.EntryQty != 3 || !d.ByReceipts() {
		t.Fatalf("E13: %+v %v", d, err)
	}
	// E12 the other way round: an unapplied receipt does not make a death.
	id = g.row("UNA", gapRow{me, "Sim101", "SHORT", "CLOSED", 3, 3, 1, 100})
	g.receipt("UNA", NT8ExitReceipt{PositionID: id, Applied: true, Quantity: 1})
	g.receipt("UNA", NT8ExitReceipt{PositionID: id, Applied: false, Quantity: 2})
	if d, err := g.st.Position().OwnDeathFor(me, "Sim101", "UNA", "SHORT"); err != nil || d.ReceiptQty != 1 || d.ByReceipts() {
		t.Fatalf("E12: %+v %v", d, err)
	}
	// E04: no size and no receipts is no evidence, not "0 of 0 closed".
	if (OwnDeath{Last: &TraderPosition{}}).ByReceipts() {
		t.Fatal("E04: a row of unknown size with no receipts is not dead by receipts")
	}
}

func TestOverCloseFilters(t *testing.T) {
	g := newGapRows(t)
	const me = "t1"
	base := gapRow{me, "Sim101", "SHORT", "CLOSED", 1, 1, 1_000_000, 1_500_000}
	get := func(r NT8ExitReceipt) OverClose {
		t.Helper()
		oc, err := g.st.Position().OverCloseFor(me, r, 10_000)
		if err != nil {
			t.Fatal(err)
		}
		return oc
	}
	// setup is one row of 1, closed by 1, and one unapplied receipt of 1: excess 1.
	setup := func(sym string) (int64, NT8ExitReceipt) {
		id := g.row(sym, base)
		g.receipt(sym, NT8ExitReceipt{PositionID: id, Applied: true, Quantity: 1, ExitMs: 1_500_000})
		return id, g.receipt(sym, NT8ExitReceipt{ID: sym + "-u", Quantity: 1, Price: 100, ExitMs: 1_500_500})
	}
	for name, other := range map[string]gapRow{
		"F02 a row entered after the exit":   {me, "Sim101", "SHORT", "OPEN", 5, 5, 1_600_000, 0},
		"F03 a later row of the other side":  {me, "Sim101", "LONG", "CLOSED", 5, 5, 1_200_000, 1_300_000},
		"F05 a later row of another account": {me, "Sim102", "SHORT", "CLOSED", 5, 5, 1_200_000, 1_300_000},
		"a later row of another trader":      {"t2", "Sim101", "SHORT", "CLOSED", 5, 5, 1_200_000, 1_300_000},
	} {
		sym := "A" + name[:3]
		id, u := setup(sym)
		g.row(sym, other)
		if oc := get(u); oc.Row == nil || oc.Row.ID != id || oc.Excess != 1 || oc.AvgPx != 100 {
			t.Errorf("%s: %+v", name, oc)
		}
	}

	// F06: a part-closed OPEN row with no entry_quantity holds a residual, not its size.
	id := g.row("F06", gapRow{me, "Sim101", "SHORT", "OPEN", 0, 1, 1_000_000, 0})
	g.receipt("F06", NT8ExitReceipt{PositionID: id, Applied: true, Quantity: 2, ExitMs: 1_500_000})
	if oc := get(g.receipt("F06", NT8ExitReceipt{Quantity: 1, Price: 100, ExitMs: 1_500_500})); oc.Row != nil || oc.Excess != 0 {
		t.Errorf("F06: nothing is proven by a residual: %+v", oc)
	}
	// … a CLOSED one's quantity is its size (control).
	id = g.row("F06c", gapRow{me, "", "SHORT", "CLOSED", 0, 1, 1_000_000, 1_500_000})
	g.receipt("F06c", NT8ExitReceipt{PositionID: id, Applied: true, Quantity: 1, ExitMs: 1_500_000})
	if oc := get(g.receipt("F06c", NT8ExitReceipt{Quantity: 1, Price: 100, ExitMs: 1_500_500})); oc.Row == nil || oc.Entry != 1 || oc.Excess != 1 {
		t.Errorf("F06 control: %+v", oc)
	}

	// F07: an unapplied receipt that names the row is counted as pending, not twice.
	id = g.row("F07", base)
	g.receipt("F07", NT8ExitReceipt{PositionID: id, Applied: true, Quantity: 1, ExitMs: 1_500_000})
	if oc := get(g.receipt("F07", NT8ExitReceipt{PositionID: id, Quantity: 1, Price: 100, ExitMs: 1_500_500})); oc.Applied != 1 || oc.Pending != 1 || oc.Excess != 1 {
		t.Errorf("F07: %+v", oc)
	}

	// F09: an unapplied receipt from before the row belongs to an earlier position,
	// however close in time.
	id = g.row("F09", gapRow{me, "Sim101", "SHORT", "CLOSED", 1, 1, 1_499_000, 1_500_000})
	g.receipt("F09", NT8ExitReceipt{ID: "F09-0", Quantity: 1, Price: 50, ExitMs: 1_498_000})
	g.receipt("F09", NT8ExitReceipt{PositionID: id, Applied: true, Quantity: 1, ExitMs: 1_500_000})
	if oc := get(g.receipt("F09", NT8ExitReceipt{ID: "F09-1", Quantity: 1, Price: 100, ExitMs: 1_500_500})); oc.Pending != 1 || oc.Excess != 1 || oc.FirstID != "F09-1" {
		t.Errorf("F09: %+v", oc)
	}

	// F12: the APPLIED receipts alone over-close the row: what the pending one adds
	// cannot price contracts it did not close.
	id = g.row("F12", base)
	g.receipt("F12", NT8ExitReceipt{PositionID: id, Applied: true, Quantity: 2, ExitMs: 1_500_000})
	if oc := get(g.receipt("F12", NT8ExitReceipt{Quantity: 1, Price: 100, ExitMs: 1_500_500})); oc.Excess != 2 || oc.AvgPx != 0 || oc.FirstID != "" {
		t.Errorf("F12: %+v", oc)
	}

	// F13: two pending receipts, one of them still closed the row: the LAST price
	// is the reverse's.
	g.row("F13", base)
	g.receipt("F13", NT8ExitReceipt{ID: "F13-0", Quantity: 1, Price: 100, ExitMs: 1_500_000})
	if oc := get(g.receipt("F13", NT8ExitReceipt{ID: "F13-1", Quantity: 1, Price: 101, ExitMs: 1_500_006})); oc.Excess != 1 || oc.AvgPx != 101 || oc.FirstID != "F13-0" {
		t.Errorf("F13: %+v", oc)
	}

	// F20: not stored is nil, not an error (the watch ends instead of waiting 70 s).
	if r, err := g.st.Position().ExitReceipt("no-such"); r != nil || err != nil {
		t.Errorf("F20: %+v %v", r, err)
	}
}
