package trader

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── W1b FOLD-3 — A REFUSED SEND NEVER LEAVES ITS BRACKET IN THE MAPS ────────
//
// The CME open set the shared (symbol, side) SL/TP maps BEFORE the send
// (SetStopLoss / SetTakeProfit → OpenLong reads them). A send the broker then
// refused (the maintenance permit, the one entry latch, an unbound account)
// left the refused entry's stop in the map, and MoveStopToBreakeven reads that
// map as the live stop for the widen ban (tcp_trader.go MoveStopToBreakeven):
// a legitimate breakeven tighten against the REAL stop could be refused as a
// widen. Driven at the chat door (OpenManualEntryAt, the function the agent's
// executeTradeWith calls), over a real TCPTrader, a real TCP server and a real
// store (canon 53); the maps are read through the broker's own copy.

func TestRefusedChatOpenLeavesTheBracketMapsByteIdentical(t *testing.T) {
	cases := []struct {
		name   string
		refuse func(w *chatDoorWire)
		marker string // what the broker's refusal names
	}{
		{"the maintenance permit", func(w *chatDoorWire) {
			w.nt.SetEntryPermit(func() (func(), bool) { return nil, false })
		}, "maintenance"},
		{"the one entry latch", func(w *chatDoorWire) {
			w.nt.SetEntryLatchSource(&nttrader.EntryLatchSource{
				Book: func(_ time.Time, _ map[string]bool) nttrader.EntryLatchBookVerdict {
					return nttrader.EntryLatchBookVerdict{Detail: "book stale (fixture)"}
				},
				Ledgers: func(_ map[string]bool) ([]string, error) { return nil, nil },
			})
		}, "one_entry_latch"},
		{"an unbound account", func(w *chatDoorWire) {
			w.nt = nttrader.NewTCPTrader(w.s, "MNQ") // no bound account
			w.at.trader = w.nt
		}, "no bound account"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newChatDoorWire(t, store.RiskControlConfig{})
			stop, target := chatBracket(t, "open_long")
			c.refuse(w)
			// The live position's bracket an earlier entry left (the real stop
			// the breakeven move is judged against), ten points wider than the
			// chat's own on both legs — so any leak changes the maps.
			_ = w.nt.SetStopLoss("MNQ", "LONG", 1, stop-10)
			_ = w.nt.SetTakeProfit("MNQ", "LONG", 1, target+10)
			stopsBefore, targetsBefore := w.nt.EntryBracketMapsForTest()

			_, err := w.at.OpenManualEntryAt("MNQ", "open_long", 1, 1, stop, target, chatDoorMidday)
			if err == nil || !strings.Contains(err.Error(), c.marker) {
				t.Fatalf("fixture: the broker must refuse the admitted chat entry (%s), got %v", c.marker, err)
			}
			var ref *ManualEntryRefusal
			if errors.As(err, &ref) {
				t.Fatalf("a broker refusal is not an admission refusal: %v", err)
			}
			w.nothingSent(t, c.name)

			stopsAfter, targetsAfter := w.nt.EntryBracketMapsForTest()
			if !reflect.DeepEqual(stopsAfter, stopsBefore) || !reflect.DeepEqual(targetsAfter, targetsBefore) {
				t.Fatalf("a refused chat open must leave the (symbol, side) SL/TP maps byte-identical:\n stops   %v → %v\n targets %v → %v",
					stopsBefore, stopsAfter, targetsBefore, targetsAfter)
			}
		})
	}
}

// The positive half: a SENT chat entry's bracket is what the maps hold after
// the send — the stop the breakeven move is judged against is the stop the
// entry carried on the wire.
func TestSentChatOpenLeavesItsOwnBracketInTheMaps(t *testing.T) {
	w := newChatDoorWire(t, store.RiskControlConfig{})
	stop, target := chatBracket(t, "open_long")
	if _, err := w.at.OpenManualEntryAt("MNQ", "open_long", 1, 1, stop, target, chatDoorMidday); err != nil {
		t.Fatalf("an admitted chat entry must send: %v", err)
	}
	select {
	case sig := <-w.sigs:
		if sig.StopLoss != stop || sig.TakeProfit != target {
			t.Fatalf("the wire must carry the chat entry's own bracket: %+v (want SL %.2f TP %.2f)", sig, stop, target)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no signal frame reached the wire")
	}
	stops, targets := w.nt.EntryBracketMapsForTest()
	if stops["MNQ:LONG"] != stop || targets["MNQ:LONG"] != target {
		t.Fatalf("a sent entry's bracket must be in the maps: stops=%v targets=%v (want %.2f / %.2f)", stops, targets, stop, target)
	}
}
