// WAVE B REPAIR (2026-09-05) — the CALLER-LEVEL pin.
//
// WHY THIS FILE EXISTS. The first cut of Wave B proved its guard as a pure
// function and proved its wiring with a source grep. Three independent reviewers
// then mutated the call site — forcing the verdict to REST, deleting the
// unknown branch, cancelling on ignorance — and the entire suite stayed green,
// because nothing in this repository executed the placement decision. That is
// the same shape as the defect the wave exists to fix: every layer reported what
// it INTENDED, and nothing read back what was actually done.
//
// So this file drives the decision AND the dispatch, with the casing the STORE
// hands back, and asserts what reached the ledger and what reached the wire.

package trader

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// --- fakes -----------------------------------------------------------------

type placeCall struct {
	symbol  string
	side    string
	qty     float64
	trigger float64
	sl, tp  float64
	leg1Qty int
	leg1TP  float64
}

type fakePlacer struct {
	calls          []placeCall
	stopLimitCalls int
	sid            string
	err            error
	afterRegister  func()
}

func (f *fakePlacer) PlaceStopEntry(symbol, side string, quantity float64, stopPx, sl, tp float64, leg1Qty int, leg1TP float64, beforeSend ...func(string) error) (string, error) {
	f.calls = append(f.calls, placeCall{symbol, side, quantity, stopPx, sl, tp, leg1Qty, leg1TP})
	if f.err != nil {
		return "", f.err
	}
	if f.sid == "" {
		f.sid = "sid-fake"
	}
	for _, register := range beforeSend {
		if err := register(f.sid); err != nil {
			return "", err
		}
	}
	if f.afterRegister != nil {
		f.afterRegister()
	}
	return f.sid, nil
}

// PlaceStopEntryWithLimit records the SAME call through the limit variant (PR B,
// 2026-10-03): the routing pin asserts which variant the knob selected.
func (f *fakePlacer) PlaceStopEntryWithLimit(symbol, side string, quantity float64, stopPx, sl, tp float64, leg1Qty int, leg1TP float64, beforeSend ...func(string) error) (string, error) {
	f.calls = append(f.calls, placeCall{symbol, side, quantity, stopPx, sl, tp, leg1Qty, leg1TP})
	f.stopLimitCalls++
	if f.err != nil {
		return "", f.err
	}
	if f.sid == "" {
		f.sid = "sid-fake"
	}
	for _, register := range beforeSend {
		if err := register(f.sid); err != nil {
			return "", err
		}
	}
	if f.afterRegister != nil {
		f.afterRegister()
	}
	return f.sid, nil
}

type stateWrite struct {
	id     int64
	state  string
	reason string
}

type fakeLedger struct {
	states  []stateWrite
	signals map[int64]string
}

func (f *fakeLedger) SetState(id int64, state, reason string) error {
	f.states = append(f.states, stateWrite{id, state, reason})
	return nil
}

func (f *fakeLedger) BeginPlacement(id int64, signalID string) error {
	if f.signals == nil {
		f.signals = map[int64]string{}
	}
	f.signals[id] = signalID
	f.states = append(f.states, stateWrite{id, "place_pending", ""})
	return nil
}

// compile-time proof that the seams are the PRODUCTION types' own shape.
var _ stopEntryPlacer = (*ntTrader.TCPTrader)(nil)
var _ armStateWriter = (*store.ArmedOrderStore)(nil)

// freeSlot is the cancel-confirmation guard verdict these Wave B cases run
// under: the slot is empty at the broker, so the stop-side guard remains the
// thing under test. TestStopEntryRefusedWhenSlotIsLiveAtTheBroker covers the
// other verdict.
func freeSlot() slotVerdict { return slotVerdict{Action: slotFree, Why: "test: slot free"} }

const testTick = 0.25

func testOffset() float64 { return 2 * testTick } // STOP_ENTRY_OFFSET_TICKS default

// --- the decision, over the casing the store actually returns ---------------

// TestDecideStopEntryOverStoredCasing — the store canonicalizes armed_orders.Side
// to UPPERCASE at the write (store/armed_orders.go:181, class 28, owner ruling
// 2026-09-03), so "LONG"/"SHORT" is what ListNonTerminal hands the placement
// branch. Until this repair the branch compared it to the lowercase literal
// "long": every LONG stop entry got the SHORT trigger, and the guard then read
// that mis-signed trigger with long semantics.
func TestDecideStopEntryOverStoredCasing(t *testing.T) {
	for _, c := range []struct {
		name        string
		side        string
		entry       float64
		price       float64
		wantSide    string
		wantTrigger float64
		wantAction  stopEntryAction
		wantVerdict stopGuardVerdict
	}{
		// A BUY STOP SITS ABOVE THE LEVEL. entry+offset, never entry−offset.
		{"stored LONG rests below trigger", "LONG", 29610.00, 29590.25, "long", 29610.50, stopEntryPlace, stopGuardRest},
		{"stored LONG already through", "LONG", 29610.00, 29612.25, "long", 29610.50, stopEntryCancel, stopGuardThrough},
		{"stored LONG exact touch fires", "LONG", 29610.00, 29610.50, "long", 29610.50, stopEntryCancel, stopGuardThrough},
		// A SELL STOP SITS BELOW THE LEVEL. Live entry_px 29591.02 (armed_orders
		// ids 38..102) — NOT tick-aligned, which is what makes the rounding real.
		{"stored SHORT rests above trigger", "SHORT", 29591.02, 29650.00, "short", 29590.50, stopEntryPlace, stopGuardRest},
		{"stored SHORT already through (09-04 id 38 price)", "SHORT", 29591.02, 29515.25, "short", 29590.50, stopEntryCancel, stopGuardThrough},
		// lowercase rows (pre-2026-09-03) must behave identically.
		{"legacy lowercase long", "long", 29610.00, 29590.25, "long", 29610.50, stopEntryPlace, stopGuardRest},
		{"legacy lowercase short", "short", 29591.02, 29515.25, "short", 29590.50, stopEntryCancel, stopGuardThrough},
		{"surrounding space", "  LONG ", 29610.00, 29590.25, "long", 29610.50, stopEntryPlace, stopGuardRest},
		// UNEVALUABLE INPUTS — none of them may reach cancel or place.
		{"no price", "LONG", 29610.00, 0, "long", 29610.50, stopEntryNoop, stopGuardUnknown},
		{"negative price", "SHORT", 29591.02, -1, "short", 29590.50, stopEntryNoop, stopGuardUnknown},
		{"no authored entry", "LONG", 0, 29500.00, "long", 0, stopEntryNoop, stopGuardUnknown},
		{"unrecognised side", "BUY", 29610.00, 29590.25, "buy", 0, stopEntryNoop, stopGuardUnknown},
		{"empty side", "   ", 29610.00, 29590.25, "", 0, stopEntryNoop, stopGuardUnknown},
	} {
		d := decideStopEntry(c.side, c.entry, testOffset(), testTick, c.price)
		if d.Side != c.wantSide {
			t.Errorf("%s: wire side %q, want %q — an uppercase side reaches a C# ternary that reads `side == \"long\" ? Buy : SellShort`", c.name, d.Side, c.wantSide)
		}
		if d.Trigger != c.wantTrigger {
			t.Errorf("%s: trigger %.4f, want %.4f", c.name, d.Trigger, c.wantTrigger)
		}
		if d.Verdict != c.wantVerdict {
			t.Errorf("%s: verdict %v, want %v (%s)", c.name, d.Verdict, c.wantVerdict, d.Why)
		}
		if d.Action != c.wantAction {
			t.Errorf("%s: action %v, want %v (%s)", c.name, d.Action, c.wantAction, d.Why)
		}
	}
}

// TestDecideStopEntryJudgesTheNumberItSends — the trigger is rounded to the tick
// BEFORE the guard reads it, because PlaceStopEntry rounds it before the wire
// carries it. Judging the UNROUNDED value let the guard call "resting" an order
// the broker receives at the market.
//
// The live number makes it concrete. armed_orders ids 38..102 carry entry_px
// 29591.02 — not tick-aligned. A BUY stop 2 ticks beyond it is 29591.52, which
// the wire rounds to 29591.50. With the market at 29591.50 the unrounded
// judgement answers REST (29591.50 < 29591.52) and the branch sends a buy stop
// whose trigger is exactly the market: it fires on arrival, at whatever the
// market is, which is the entire failure mode this guard exists to prevent.
// Rounded first, the same inputs answer THROUGH and the arm is cancelled.
func TestDecideStopEntryJudgesTheNumberItSends(t *testing.T) {
	const liveEntry = 29591.02 // armed_orders ids 38..102, NY/S2 2026-09-04
	const market = 29591.50    // a real tick, and exactly the rounded trigger
	d := decideStopEntry("LONG", liveEntry, testOffset(), testTick, market)
	if d.Trigger != ntTrader.RoundToTick(d.Trigger, testTick) {
		t.Fatalf("the judged trigger %.4f is not on a tick boundary — the wire will round it and judge≠sent", d.Trigger)
	}
	if d.Trigger != 29591.50 {
		t.Fatalf("trigger %.4f, want 29591.50 (entry+2t rounded to the MNQ tick)", d.Trigger)
	}
	if d.Action != stopEntryCancel {
		t.Fatalf("a buy stop whose SENT trigger equals the market must be cancelled, got %v (%s)", d.Action, d.Why)
	}
	// The value that USED to be judged still disagrees — otherwise this pin has
	// stopped testing the rounding at all.
	if v, _ := stopEntryGuardVerdict("long", liveEntry+testOffset(), market); v != stopGuardRest {
		t.Fatalf("the unrounded trigger no longer disagrees: %v — the pin is vacuous", v)
	}
}

// TestTickSourcesAgree — decideStopEntry rounds with market.FuturesTickSize and
// PlaceStopEntry re-rounds with ninjatrader.InstrumentTickSize. Judge-equals-sent
// holds only while those two agree for the roots this bot trades.
func TestTickSourcesAgree(t *testing.T) {
	for _, root := range []string{"NQ", "MNQ", "ES", "MES", "RTY", "M2K", "YM", "MYM"} {
		a, b := market.FuturesTickSize(root), ntTrader.InstrumentTickSize(root)
		if a != b || a <= 0 {
			t.Errorf("%s: market.FuturesTickSize=%v vs ninjatrader.InstrumentTickSize=%v — the executor would judge a trigger the wire re-rounds", root, a, b)
		}
	}
}

// --- the dispatch: what reaches the ledger and what reaches the wire --------

func armRow(id int64, side string, entry float64) store.ArmedOrderDB {
	return store.ArmedOrderDB{
		ID: id, TraderID: "t1", PlanID: "2026-09-05:NY:t1", Version: 3,
		Scenario: "S2", Side: side, Kind: "stop_entry",
		EntryPx: entry, StopPx: entry + 40, TargetPx: entry - 120,
		State: "armed",
	}
}

// TestPlaceOneStopEntryDispatch — A29 / D3. THE pin the first cut was missing:
// it executes the branch that decides cancel vs place vs leave-alone. Every
// mutation the reviewers ran against the old call site turns one of these red.
func TestPlaceOneStopEntryDispatch(t *testing.T) {
	now := time.Date(2026, 9, 5, 10, 5, 0, 0, time.UTC)

	t.Run("through cancels and never places", func(t *testing.T) {
		at := &AutoTrader{id: "t1"}
		pl, led := &fakePlacer{}, &fakeLedger{}
		r := armRow(38, "SHORT", 29591.02)
		d := decideStopEntry(r.Side, r.EntryPx, testOffset(), testTick, 29515.25)
		at.placeOneStopEntry(pl, led, r, d, 29515.25, now, freeSlot())
		if len(pl.calls) != 0 {
			t.Fatalf("an already-through stop reached the wire: %+v", pl.calls)
		}
		if len(led.states) != 1 || led.states[0].state != "cancelled" || led.states[0].id != 38 {
			t.Fatalf("want exactly one cancel of row 38, got %+v", led.states)
		}
		if !strings.Contains(led.states[0].reason, "accepted through (stop side)") ||
			!strings.Contains(led.states[0].reason, "never placed") {
			t.Errorf("the cancel reason must name the guard and say it was never placed: %q", led.states[0].reason)
		}
	})

	t.Run("rest places exactly one, with the folded side and the judged trigger", func(t *testing.T) {
		at := &AutoTrader{id: "t1"}
		pl, led := &fakePlacer{sid: "sig-1"}, &fakeLedger{}
		r := armRow(200, "LONG", 29610.00) // the STORED casing
		d := decideStopEntry(r.Side, r.EntryPx, testOffset(), testTick, 29590.25)
		at.placeOneStopEntry(pl, led, r, d, 29590.25, now, freeSlot())
		if len(pl.calls) != 1 {
			t.Fatalf("want exactly one placement, got %d", len(pl.calls))
		}
		c := pl.calls[0]
		if c.side != "long" {
			t.Errorf("the wire carried side %q — the AddOn reads `side == \"long\" ? Buy : SellShort`, so anything else submits a live SELL", c.side)
		}
		if c.trigger != 29610.50 {
			t.Errorf("a BUY stop must sit ABOVE the level: trigger %.2f, want 29610.50", c.trigger)
		}
		if c.trigger != d.Trigger {
			t.Errorf("the trigger sent (%.4f) is not the trigger judged (%.4f)", c.trigger, d.Trigger)
		}
		if c.qty != 1 || c.sl != r.StopPx || c.tp != r.TargetPx {
			t.Errorf("bracket/qty not forwarded intact: %+v", c)
		}
		if led.signals[200] != "sig-1" {
			t.Errorf("the signal id was not recorded: %v", led.signals)
		}
		if len(led.states) != 1 || led.states[0].state != "place_pending" {
			t.Fatalf("want exactly one pending transition, got %+v", led.states)
		}
	})

	t.Run("unknown leaves the arm exactly as it is", func(t *testing.T) {
		for _, c := range []struct {
			name  string
			side  string
			entry float64
			price float64
		}{
			{"no price", "LONG", 29610.00, 0},
			{"no authored entry", "LONG", 0, 29500.00},
			{"unrecognised side", "BUY", 29610.00, 29590.25},
		} {
			at := &AutoTrader{id: "t1"}
			pl, led := &fakePlacer{}, &fakeLedger{}
			r := armRow(300, c.side, c.entry)
			d := decideStopEntry(r.Side, r.EntryPx, testOffset(), testTick, c.price)
			at.placeOneStopEntry(pl, led, r, d, c.price, now, freeSlot())
			if len(pl.calls) != 0 {
				t.Errorf("%s: an unadjudicated arm reached the wire: %+v", c.name, pl.calls)
			}
			if len(led.states) != 0 {
				t.Errorf("%s: an unadjudicated arm was written to (cancel-on-ignorance): %+v", c.name, led.states)
			}
		}
	})

	t.Run("a build refusal never touches the ledger", func(t *testing.T) {
		at := &AutoTrader{id: "t1"}
		pl := &fakePlacer{err: fmt.Errorf("refusing stop-entry: %w", ntwire.ErrAddonBuildTooOld)}
		led := &fakeLedger{}
		r := armRow(400, "SHORT", 29591.02)
		d := decideStopEntry(r.Side, r.EntryPx, testOffset(), testTick, 29650.00)
		if d.Action != stopEntryPlace {
			t.Fatalf("fixture no longer reaches the wire: %v (%s)", d.Action, d.Why)
		}
		at.placeOneStopEntry(pl, led, r, d, 29650.00, now, freeSlot())
		if len(pl.calls) != 1 {
			t.Fatalf("the refusal must happen AT the wire call, got %d calls", len(pl.calls))
		}
		if len(led.states) != 0 || len(led.signals) != 0 {
			t.Fatalf("a refused stop entry must stay armed for the next cycle: %+v %v", led.states, led.signals)
		}
	})

	t.Run("a transport failure also leaves the arm alone", func(t *testing.T) {
		at := &AutoTrader{id: "t1"}
		pl := &fakePlacer{err: errors.New("send armed signal: broken pipe")}
		led := &fakeLedger{}
		r := armRow(500, "SHORT", 29591.02)
		d := decideStopEntry(r.Side, r.EntryPx, testOffset(), testTick, 29650.00)
		at.placeOneStopEntry(pl, led, r, d, 29650.00, now, freeSlot())
		if len(led.states) != 0 {
			t.Fatalf("a failed send must not move the ledger: %+v", led.states)
		}
	})
}

// TestPlacementRefusalKeyCannotAliasTheArmGate — the placement dedupe key and
// the arm-gate key share at.armRefusalLast. The placement key was 0-based while
// every gate key is 1-based, so placement(leg_index=1) was byte-identical to
// gate(li=0) for the same plan/version/scenario: two writers alternating under
// one key make armRefusalChanged answer true every cycle for BOTH, which
// re-increments the durable per-session counter per cycle instead of once per
// distinct arm-spec.
func TestPlacementRefusalKeyCannotAliasTheArmGate(t *testing.T) {
	const plan, ver, sc = "2026-09-05:NY:t1", 3, "S2"
	gateKey := func(li int) string {
		return plan + ":" + itoa(ver) + ":" + sc + ":leg" + itoa(li+1)
	}
	at := &AutoTrader{id: "t1"}
	for _, legIndex := range []int{0, 1, 2} {
		pl, led := &fakePlacer{}, &fakeLedger{}
		r := armRow(600+int64(legIndex), "LONG", 29610.00)
		r.LegIndex = legIndex
		before := len(at.armRefusalLast)
		d := decideStopEntry(r.Side, r.EntryPx, testOffset(), testTick, 0) // unknown → keyed refusal
		at.placeOneStopEntry(pl, led, r, d, 0, time.Now(), freeSlot())
		if len(at.armRefusalLast) != before+1 {
			t.Fatalf("leg %d: the refusal was not keyed (before=%d after=%d)", legIndex, before, len(at.armRefusalLast))
		}
		for k := range at.armRefusalLast {
			for _, li := range []int{0, 1, 2} {
				if k == gateKey(li) {
					t.Fatalf("placement key %q is byte-identical to the arm-gate key for leg %d", k, li)
				}
			}
		}
	}
}

func itoa(i int) string { return fmt.Sprintf("%d", i) }
