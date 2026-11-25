package ninjatrader

import (
	"fmt"
	"testing"

	"vl/store"
)

// ── LOOK TWICE, the VALUES of its timing constants (ledger half) ─────────────
// Absolute numbers on purpose: a boundary computed from the constant follows it.

// ownReverseClockSlack is 10 s, ownReverseFirstSeen 2 min, ownReverseKeep 10 min.
// MUTANTS: 5 s / 20 s, 1 min / 4 min, 5 min / 20 min.
func TestLookTwiceTiming_OwnReverseProof(t *testing.T) {
	for name, c := range map[string]struct {
		after int64 // first sighting, ms after the receipt
		bot   bool
		left  int // proofs still waiting afterwards
	}{
		"10.1 s before the receipt":        {-10_100, false, 1},
		"9.9 s before the receipt":         {-9_900, true, 0},
		"119.9 s after the receipt":        {119_900, true, 0},
		"120.1 s after the receipt":        {120_100, false, 1},
		"599.9 s after it, still kept":     {599_900, false, 1},
		"600.1 s after it, forgotten":      {600_100, false, 0},
		"control: at the receipt's moment": {0, true, 0},
	} {
		tr, st := reverseTrader(t)
		if _, err := tr.NoteOwnReverse(nil, "t1", revProof()); err != nil {
			t.Fatal(err)
		}
		p := adoptedRow(t, st, "p", nil)
		if err := st.GormDB().Model(p).Update("origin", "").Error; err != nil {
			t.Fatal(err)
		}
		why := tr.flagManualUnlessOwnReverse(st, "t1", p.ID, "MNQ", "LONG", 1, 27753.25, revSeen+c.after)
		if (why != "") != c.bot || len(tr.ownReverses) != c.left {
			t.Errorf("%s: kept bot %v with %d proofs left, want %v with %d", name, why != "", len(tr.ownReverses), c.bot, c.left)
		}
	}
}

// The other reader of ownReverseClockSlack: the row's own entry time. MUTANTS: 5 s, 20 s.
func TestLookTwiceTiming_OwnReverseRowSlack(t *testing.T) {
	for before, fixed := range map[int64]bool{9_900: true, 10_100: false} {
		tr, st := reverseTrader(t)
		p := adoptedRow(t, st, fmt.Sprint("r", before), func(p *store.TraderPosition) { p.EntryTime = revSeen - before })
		id, err := tr.NoteOwnReverse(st, "t1", revProof())
		if err != nil || (id == p.ID) != fixed {
			t.Errorf("a row entered %d ms before the receipt: corrected %d (%v), want corrected %v", before, id, err, fixed)
		}
	}
}
