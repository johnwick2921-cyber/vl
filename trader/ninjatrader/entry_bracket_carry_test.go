package ninjatrader

import (
	"reflect"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
)

// ── W1b FOLD-3 — the entry carries its OWN bracket; the maps learn it only
// when the entry may be on the wire ──────────────────────────────────────────
//
// The shared (symbol, side) SL/TP maps are what MoveStopToBreakeven's widen ban
// reads as the live stop. A market entry that wrote them BEFORE its send and
// was then refused left a bracket no order carries there.

func bracketMaps(tr *TCPTrader) (map[string]float64, map[string]float64) {
	return tr.EntryBracketMapsForTest()
}

// Every refusal before the send leaves the maps byte-identical; the live
// bracket seeded first is what the widen ban keeps judging against.
func TestOpenWithBracketRefusedLeavesTheMapsByteIdentical(t *testing.T) {
	s, frames := allFramesServer(t)
	cases := []struct {
		name   string
		tr     func() *TCPTrader
		stop   float64
		marker string
	}{
		{"no bound account", func() *TCPTrader { return NewTCPTrader(s, "MNQ") }, 29050, "no bound account"},
		{"not a tradeable account", func() *TCPTrader { return NewTCPTrader(s, "MNQ", "Live-Unknown") }, 29050, "not tradeable"},
		{"the maintenance permit", func() *TCPTrader {
			tr := NewTCPTrader(s, "MNQ", "Sim101")
			tr.SetEntryPermit(refusePermit)
			return tr
		}, 29050, "maintenance hold"},
		{"the one entry latch", func() *TCPTrader {
			tr := NewTCPTrader(s, "MNQ", "Sim101")
			tr.SetEntryLatchSource(&EntryLatchSource{
				Book: func(_ time.Time, _ map[string]bool) EntryLatchBookVerdict {
					return EntryLatchBookVerdict{Detail: "fixture: book stale"}
				},
				Ledgers: func(_ map[string]bool) ([]string, error) { return nil, nil },
			})
			return tr
		}, 29050, "one_entry_latch"},
		{"an incomplete bracket", func() *TCPTrader { return NewTCPTrader(s, "MNQ", "Sim101") }, 0, "must be called before"},
		// The hold's provably-UNSENT drop (W1b FOLD-3 repair): SendSignal itself
		// refuses with ErrEntryHeld (the hold landed before the flush, so
		// sendAttempted is false). Every rail before the send passed; only the
		// latchSent guard keeps this bracket out of the maps.
		{"the hold's provably-unsent drop", func() *TCPTrader {
			hs := ntwire.NewTCPServer(nil)
			hs.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
			hs.SetEntryHoldCheck(func() bool { return true })
			return NewTCPTrader(hs, "MNQ", "Sim101")
		}, 29050, ntwire.ErrEntryHeld.Error()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := c.tr()
			_ = tr.SetStopLoss("MNQ", "long", 1, 29000) // the live position's stop
			_ = tr.SetTakeProfit("MNQ", "long", 1, 29300)
			stops, targets := bracketMaps(tr)
			_, err := tr.OpenWithBracket("MNQ", "long", 1, c.stop, 29200)
			if err == nil || !strings.Contains(err.Error(), c.marker) {
				t.Fatalf("fixture: want a refusal naming %q, got %v", c.marker, err)
			}
			if waitFrame(frames, ntwire.FrameSignal, 100*time.Millisecond) {
				t.Fatal("a refused entry reached the wire")
			}
			s2, t2 := bracketMaps(tr)
			if !reflect.DeepEqual(s2, stops) || !reflect.DeepEqual(t2, targets) {
				t.Fatalf("a refused entry must leave the maps byte-identical:\n stops   %v → %v\n targets %v → %v", stops, s2, targets, t2)
			}
		})
	}
}

// A SENT entry puts its own bracket on the wire and in the maps; a second,
// B3-refused entry on the same key leaves the FIRST one's bracket in place.
func TestOpenWithBracketSentRecordsItsOwnBracketAndABlockedRepeatDoesNot(t *testing.T) {
	s, frames := allFramesServer(t)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	_ = tr.SetStopLoss("MNQ", "long", 1, 28000) // a stale map value the send must NOT use
	_ = tr.SetTakeProfit("MNQ", "long", 1, 30000)
	if _, err := tr.OpenWithBracket("MNQ", "long", 1, 29000, 29200); err != nil {
		t.Fatalf("the entry must send: %v", err)
	}
	if !waitFrame(frames, ntwire.FrameSignal, time.Second) {
		t.Fatal("no signal frame reached the wire")
	}
	stops, targets := bracketMaps(tr)
	if stops["MNQ:LONG"] != 29000 || targets["MNQ:LONG"] != 29200 {
		t.Fatalf("a sent entry's own bracket must be in the maps: stops=%v targets=%v", stops, targets)
	}
	_, err := tr.OpenWithBracket("MNQ", "long", 1, 29050, 29250)
	if err == nil || !strings.Contains(err.Error(), "duplicate order dropped") {
		t.Fatalf("fixture: the repeat must be dropped by B3, got %v", err)
	}
	s2, t2 := bracketMaps(tr)
	if !reflect.DeepEqual(s2, stops) || !reflect.DeepEqual(t2, targets) {
		t.Fatalf("a B3-dropped repeat must leave the first entry's bracket:\n stops   %v → %v\n targets %v → %v", stops, s2, targets, t2)
	}
}

// The harm the ruling names: a refused entry's tighter stop left in the map
// made a legitimate breakeven tighten against the REAL stop read as a widen.
func TestRefusedEntryNeverTurnsABreakevenTightenIntoAWiden(t *testing.T) {
	s := ntwire.NewTCPServer(nil)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	_ = tr.SetStopLoss("MNQ", "long", 1, 29900) // the real resting stop
	tr.mu.Lock()
	tr.lastEntrySignalID = "sig-live"
	tr.mu.Unlock()
	tr.SetEntryPermit(refusePermit)
	if _, err := tr.OpenWithBracket("MNQ", "long", 1, 29950, 30100); err == nil {
		t.Fatal("fixture: the entry must be refused by the permit")
	}
	// 29920 tightens the real 29900 stop; it is below the refused entry's 29950.
	err := tr.MoveStopToBreakeven("long", 29920)
	if err != nil && strings.Contains(err.Error(), "stop-widen ban") {
		t.Fatalf("a breakeven tighten against the real stop was refused as a widen (the refused entry's stop was left in the map): %v", err)
	}
}

// The debug test trade carries its own bracket too: refused (no bound
// account), it leaves the maps untouched.
func TestDebugTestTradeRefusedLeavesTheMapsByteIdentical(t *testing.T) {
	s := ntwire.NewTCPServer(nil)
	s.BarCache().SeedHistorical("MNQ", "5m", []ntwire.Bar{{T: time.Now().Add(-5 * time.Minute).Truncate(5 * time.Minute).UnixMilli(), O: 29100, H: 29110, L: 29090, C: 29100, V: 10}})
	tr := NewTCPTrader(s, "MNQ") // unbound
	_ = tr.SetStopLoss("MNQ", "long", 1, 29000)
	_ = tr.SetTakeProfit("MNQ", "long", 1, 29300)
	stops, targets := bracketMaps(tr)
	if _, err := tr.DebugPlaceTestTrade("long"); err == nil || !strings.Contains(err.Error(), "no bound account") {
		t.Fatalf("fixture: an unbound test trade must be refused, got %v", err)
	}
	s2, t2 := bracketMaps(tr)
	if !reflect.DeepEqual(s2, stops) || !reflect.DeepEqual(t2, targets) {
		t.Fatalf("a refused test trade must leave the maps byte-identical:\n stops   %v → %v\n targets %v → %v", stops, s2, targets, t2)
	}
}
