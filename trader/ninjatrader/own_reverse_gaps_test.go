package ninjatrader

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── LOOK TWICE, the mutants no test saw (full test of 9acafc81f): the ledger half
// of the own-reverse proof. Named by mutant id in fulltest-71/C1-table.log. A proof
// used on the wrong row moves money between the bot record (daily-loss gate) and
// the manual one.

func reverseTrader(t *testing.T) (*TCPTrader, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "rev.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewTCPTrader(ntwire.NewTCPServer(nil), "MNQ", "Sim101"), st
}

const revSeen = int64(1_700_000_000_000)

func revProof() OwnReverse {
	return OwnReverse{Symbol: "MNQ", Side: "LONG", Qty: 1, Avg: 27753.25, SeenMs: revSeen, Why: "our own close opened it"}
}

// adoptedRow is a reconcile adoption already flagged manual.
func adoptedRow(t *testing.T, st *store.Store, name string, change func(*store.TraderPosition)) *store.TraderPosition {
	t.Helper()
	p := &store.TraderPosition{TraderID: "t1", ExchangeType: "ninjatrader", ExchangePositionID: name, Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryQuantity: 1, EntryPrice: 27753.25, EntryTime: revSeen + 61_000, Leverage: 1, Status: "OPEN", Source: "reconcile",
		PlanID: store.PlanUnresolvable, Account: "Sim101", Origin: store.OriginManual}
	if change != nil {
		change(p)
	}
	if err := st.GormDB().Create(p).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.GormDB().Model(p).Update("origin", store.OriginManual).Error; err != nil {
		t.Fatal(err)
	}
	return p
}

// U02-U04, U16, U05-U07: NoteOwnReverse corrects THIS position's row only.
func TestNoteOwnReverseCorrectsOnlyItsOwnRow(t *testing.T) {
	for name, c := range map[string]struct {
		change func(*store.TraderPosition)
		fixed  bool
	}{
		"its own row":                    {nil, true},
		"within half a tick":             {func(p *store.TraderPosition) { p.EntryPrice += 0.125 }, true},
		"entered a moment before":        {func(p *store.TraderPosition) { p.EntryTime = revSeen - ownReverseClockSlack.Milliseconds() }, true},
		"U05 another trader's row":       {func(p *store.TraderPosition) { p.TraderID = "t2" }, false},
		"U06/U03 another size":           {func(p *store.TraderPosition) { p.Quantity, p.EntryQuantity = 2, 2 }, false},
		"U06/U04 another average":        {func(p *store.TraderPosition) { p.EntryPrice += 0.25 }, false},
		"U07 a row from long before":     {func(p *store.TraderPosition) { p.EntryTime = revSeen - ownReverseClockSlack.Milliseconds() - 1 }, false},
		"a row with its own entry order": {func(p *store.TraderPosition) { p.EntryOrderID = "e" }, false},
	} {
		tr, st := reverseTrader(t)
		p := adoptedRow(t, st, "a", c.change)
		id, err := tr.NoteOwnReverse(st, "t1", revProof())
		if err != nil || (id == p.ID) != c.fixed {
			t.Errorf("%s: corrected row %d (%v), want corrected %v", name, id, err, c.fixed)
		}
		var got store.TraderPosition
		if err := st.GormDB().First(&got, p.ID).Error; err != nil || store.IsManualOrigin(got.Origin) == c.fixed {
			t.Errorf("%s: origin %q (%v), want corrected %v", name, got.Origin, err, c.fixed)
		}
		// U09: a proof that has its row is used up; one that has none still waits for it.
		if left := len(tr.ownReverses); (left == 0) != c.fixed {
			t.Errorf("%s: %d proofs left after corrected=%v", name, left, c.fixed)
		}
	}
}

// U02-U04, U16: the match itself.
func TestOwnReverseIs(t *testing.T) {
	r := revProof()
	for name, c := range map[string]struct {
		sym, side string
		qty, avg  float64
		want      bool
	}{
		"same":               {"MNQ", "long", 1, 27753.25, true},
		"half a tick":        {"MNQ", "LONG", 1, 27753.125, true},
		"U16 another symbol": {"MES", "LONG", 1, 27753.25, false},
		"U02 other side":     {"MNQ", "SHORT", 1, 27753.25, false},
		"U03 size":           {"MNQ", "LONG", 2, 27753.25, false},
		"U04 average":        {"MNQ", "LONG", 1, 27753.50, false},
	} {
		if got := r.is(c.sym, c.side, c.qty, c.avg); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// U01, U11, U12, U13: what flagManualUnlessOwnReverse makes of the proofs.
func TestFlagManualUnlessOwnReverse(t *testing.T) {
	tr, st := reverseTrader(t)
	for i := 0; i < 2; i++ { // U01: two receipts of one reverse prove it twice
		if _, err := tr.NoteOwnReverse(st, "t1", revProof()); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(tr.ownReverses); n != 1 {
		t.Fatalf("U01: one reverse = one record, got %d", n)
	}
	n := 0
	ask := func(firstSeen int64) (bot bool) {
		t.Helper()
		n++
		p := adoptedRow(t, st, fmt.Sprint("f", n), func(p *store.TraderPosition) { p.Status = "CLOSED" })
		if err := st.GormDB().Model(p).Updates(map[string]any{"status": "OPEN", "origin": ""}).Error; err != nil {
			t.Fatal(err)
		}
		why := tr.flagManualUnlessOwnReverse(st, "t1", p.ID, "MNQ", "LONG", 1, 27753.25, firstSeen)
		var got store.TraderPosition
		if err := st.GormDB().First(&got, p.ID).Error; err != nil {
			t.Fatal(err)
		}
		if store.IsManualOrigin(got.Origin) != (why == "") {
			t.Fatalf("origin %q but why %q", got.Origin, why)
		}
		return why != ""
	}
	if ask(revSeen - ownReverseClockSlack.Milliseconds() - 1) {
		t.Fatal("U11: a position first seen long before the receipt is not the reverse")
	}
	if ask(revSeen + ownReverseFirstSeen.Milliseconds() + 1) {
		t.Fatal("a position first seen long after the receipt is not the reverse")
	}
	if !ask(revSeen) {
		t.Fatal("control: the reverse itself is kept bot")
	}
	if ask(revSeen) {
		t.Fatal("U12: one proof, one row")
	}
	// U13: an unused proof is forgotten after ownReverseKeep.
	if _, err := tr.NoteOwnReverse(nil, "t1", revProof()); err != nil {
		t.Fatal(err)
	}
	ask(revSeen + ownReverseKeep.Milliseconds())
	if len(tr.ownReverses) != 1 {
		t.Fatal("control: still kept")
	}
	ask(revSeen + ownReverseKeep.Milliseconds() + 1)
	if len(tr.ownReverses) != 0 {
		t.Fatal("U13: an old proof must not wait for ever")
	}
}

// U17-U20: only a later ENTRY fill of ours, there, explains a position.
func TestEntryFillSince(t *testing.T) {
	tr, _ := reverseTrader(t)
	for name, c := range map[string]struct {
		f    recentFill
		want bool
	}{
		"ours, then":            {recentFill{SignalID: "e", Symbol: "MNQ", Account: "Sim101", Side: "LONG", TimeMs: 1000}, true},
		"no symbol, no account": {recentFill{SignalID: "e", Side: "LONG", TimeMs: 1000}, true},
		"U17 earlier":           {recentFill{SignalID: "e", Symbol: "MNQ", Account: "Sim101", Side: "LONG", TimeMs: 999}, false},
		"U18 other side":        {recentFill{SignalID: "e", Symbol: "MNQ", Account: "Sim101", Side: "SHORT", TimeMs: 1000}, false},
		"U19 other symbol":      {recentFill{SignalID: "e", Symbol: "MES", Account: "Sim101", Side: "LONG", TimeMs: 1000}, false},
		"U20 other account":     {recentFill{SignalID: "e", Symbol: "MNQ", Account: "Sim102", Side: "LONG", TimeMs: 1000}, false},
	} {
		tr.mu.Lock()
		tr.recentFills = []recentFill{c.f}
		tr.mu.Unlock()
		if _, ok := tr.EntryFillSince("MNQ", "LONG", 1000); ok != c.want {
			t.Errorf("%s: %v, want %v", name, ok, c.want)
		}
	}
}

// U26 / U30: a withheld close leaves the entry's fill correlation as it was, and
// never puts an older entry's back over a newer one's.
func TestCloseAllIfKeepsTheEntryCorrelation(t *testing.T) {
	sig := func(tr *TCPTrader, set string) string {
		tr.mu.Lock()
		defer tr.mu.Unlock()
		if set != "" {
			tr.lastEntrySignalID = set
		}
		return tr.lastEntrySignalID
	}
	tr, _ := reverseTrader(t)
	sig(tr, "entry-1")
	err := tr.CloseAllIf("long", func() error { return fmt.Errorf("%w: flat", ntwire.ErrSendWithheld) })
	if !errors.Is(err, ntwire.ErrSendWithheld) || sig(tr, "") != "entry-1" {
		t.Fatalf("U26: err %v, correlation %q", err, sig(tr, ""))
	}
	err = tr.CloseAllIf("long", func() error { sig(tr, "entry-2"); return fmt.Errorf("%w: flat", ntwire.ErrSendWithheld) })
	if !errors.Is(err, ntwire.ErrSendWithheld) || sig(tr, "") != "entry-2" {
		t.Fatalf("U30: err %v, correlation %q", err, sig(tr, ""))
	}
	if err = tr.CloseAllIf("long", func() error { return nil }); err == nil || errors.Is(err, ntwire.ErrSendWithheld) || sig(tr, "") != "" {
		t.Fatalf("control: a close that failed for another reason drops it as before: err %v, correlation %q", err, sig(tr, ""))
	}
}
