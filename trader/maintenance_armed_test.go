package trader

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── W-ONE-BUTTON M2 site 2 — the armed placement path ──────────────────────
//
// A hold refuses each arm AT ITS SEND POINT (never an early return from the
// pass: the tail — stale-working reaper, order_update drain, placement
// confirmation, cancel settlement, boot reconcile — must keep running), and it
// never cancels an arm: the refused arm stays "armed" and places on the first
// pass after the hold clears.

func liveArmFixture(t *testing.T) (*AutoTrader, *store.Store, chan struct{ sid string }, time.Time) {
	t.Helper()
	now := time.Date(2026, time.September, 11, 15, 0, 0, 0, time.UTC)
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	oneSetupOff(&cfg)
	structuralTestPolicy(&cfg, .5)
	cfg.RiskControl.MinRiskRewardRatio = 2
	at, st, sigs, _ := shadowWireHarnessAt(t, cfg, now)
	live := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long", Conviction: "low", FlipCondition: "n/a"},
		Levels: []kernel.PlanLevel{{Price: 100, Label: "PDH", Grade: "A", Instruction: "fade"}},
		Scenarios: []kernel.PlanScenario{{ID: "S1", Trigger: "t", Condition: "reject", Direction: "long",
			TargetChain: []float64{110}, Invalid: "i", Quality: "B",
			Confirm: &kernel.PlanConfirm{Rule: "touch", RefPrice: 100, Side: "above"},
			Arm:     &kernel.PlanArmSpec{Enabled: true, Entry: 100, Stop: 95, Target: 110}},
		},
		NoTrade: []string{}, DeathCondition: "n/a",
	}
	structuralTestMap(&live, structuralTestZone{100, 95.5, 100, "PDH"}, structuralTestZone{110, 110, 111, "target"})
	blob, _ := json.Marshal(live)
	shadowPlanAtTime(t, at, st, string(blob), now)
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return shadowBarsNearAt(100, now) }
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	out := make(chan struct{ sid string }, 8)
	go func() {
		for s := range sigs {
			out <- struct{ sid string }{s.SignalID}
		}
	}()
	return at, st, out, now
}

func TestArmedLimitRefusedWhileHeldArmSurvivesThenPlacesAfterRelease(t *testing.T) {
	dir := withMaintenanceDir(t)
	at, st, sigs, now := liveArmFixture(t)
	setHold(t, dir, "job-armed")
	before := gateBlocks(at.id, "maintenance_hold")

	at.maybeManageArmedOrdersAt(nil, now)

	select {
	case s := <-sigs:
		t.Fatalf("an armed entry reached the wire while held (signal %s)", s.sid)
	case <-time.After(300 * time.Millisecond):
	}
	rows, err := st.ArmedOrders().ListNonTerminal(at.id)
	if err != nil {
		t.Fatal(err)
	}
	armed := 0
	for _, r := range rows {
		if r.State == store.StateArmed {
			armed++
		}
	}
	if armed == 0 {
		t.Fatalf("the hold must not cancel or advance the arm; non-terminal rows: %+v", rows)
	}
	if gateBlocks(at.id, "maintenance_hold") <= before {
		t.Fatal("the refused placement was not counted as a maintenance_hold gate block")
	}

	if err := store.ClearMaintenanceHold(dir, "job-armed"); err != nil {
		t.Fatal(err)
	}
	at.maybeManageArmedOrdersAt(nil, now.Add(time.Second))
	select {
	case s := <-sigs:
		if s.sid == "" {
			t.Fatal("empty signal id")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("after the hold cleared the surviving arm did not place")
	}
}

// placeOneStopEntry refuses at ENTRY while held (the hold may land after the
// pass snapshot): nothing reaches the placer, no ledger write, counted as
// maintenance_hold, and REPORTED so the caller neither latches "placed" nor
// cancels the plan's other arms.
func TestPlaceOneStopEntryRefusesAtEntryWhileHeld(t *testing.T) {
	dir := withMaintenanceDir(t)
	setHold(t, dir, "job-stop")
	at, _ := resetTrader(t, store.StrategyConfig{})
	at.id = "maint-armed-entry"
	pl := &fakePlacer{}
	led := &fakeLedger{}
	r := store.ArmedOrderDB{ID: 7, TraderID: at.id, PlanID: "2026-09-22:NY", Version: 1, Session: "NY", Scenario: "S1", Side: "LONG", EntryPx: 100, StopPx: 95, TargetPx: 110, Kind: "stop_entry"}
	d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
	if d.Action != stopEntryPlace {
		t.Fatalf("fixture must be an otherwise-placeable arm, got %q", d.Action)
	}
	before := gateBlocks(at.id, "maintenance_hold")
	if got := at.placeOneStopEntry(pl, led, r, d, 99, time.Now(), freeSlot()); got != stopPlaceHeld {
		t.Fatalf("placeOneStopEntry must report a maintenance-hold refusal to its caller, got outcome %d", got)
	}
	if len(pl.calls) != 0 || len(led.states) != 0 {
		t.Fatalf("held: nothing may reach the placer or the ledger (calls=%d states=%+v)", len(pl.calls), led.states)
	}
	if gateBlocks(at.id, "maintenance_hold") != before+1 {
		t.Fatal("the refusal must be counted as maintenance_hold")
	}
}

// The broker permit refused (hold landed between the entry check and the
// send): the same report, the same counter, no ledger write.
func TestPlaceOneStopEntryReportsABrokerPermitRefusal(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{})
	at.id = "maint-armed-race"
	pl := &fakePlacer{err: fmt.Errorf("wire: %w", ntTrader.ErrMaintenanceHold)}
	led := &fakeLedger{}
	r := store.ArmedOrderDB{ID: 7, TraderID: at.id, PlanID: "2026-09-22:NY", Version: 1, Session: "NY", Scenario: "S1", Side: "LONG", EntryPx: 100, StopPx: 95, TargetPx: 110, Kind: "stop_entry"}
	d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
	before := gateBlocks(at.id, "maintenance_hold")
	if got := at.placeOneStopEntry(pl, led, r, d, 99, time.Now(), freeSlot()); got != stopPlaceHeld {
		t.Fatalf("a broker-permit refusal must be reported to the caller as a hold refusal, got outcome %d", got)
	}
	if len(led.states) != 0 {
		t.Fatalf("no ledger write for a held entry: %+v", led.states)
	}
	if gateBlocks(at.id, "maintenance_hold") != before+1 {
		t.Fatal("the race refusal must be counted as maintenance_hold")
	}
	// every other outcome is unchanged: a plain failure is NOT a hold refusal
	pl2 := &fakePlacer{err: fmt.Errorf("boom")}
	if got := at.placeOneStopEntry(pl2, &fakeLedger{}, r, d, 99, time.Now(), freeSlot()); got == stopPlaceHeld {
		t.Fatal("a non-hold failure must not be reported as a hold refusal")
	}
}

// The caller consults that report BEFORE latching placedThisPass and before
// cancelOtherArmsInPlan (a hold refusal must never cancel the plan's arms).
func TestArmedStopCallerSkipsTheLatchOnAHoldRefusal(t *testing.T) {
	b, err := os.ReadFile("armed_executor.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	i := strings.Index(src, "func (at *AutoTrader) runArmedPlacementAtFiltered(")
	j := strings.Index(src[i:], "\n}\n")
	body := src[i : i+j]
	call := strings.Index(body, "if at.placeOneStopEntry(")
	latch := strings.Index(body, "placedThisPass = true\n\t\t\t\tplacedGroup = r.EntryGroup\n\t\t\t\tat.cancelOtherArmsInPlan(ledger, rows, r, now)\n\t\t\t\tcontinue")
	if call < 0 {
		t.Fatal("runArmedPlacementAtFiltered must branch on placeOneStopEntry's hold report (`if at.placeOneStopEntry(`)")
	}
	if latch < 0 || call > latch {
		t.Fatalf("the hold branch (%d) must come before the stop path's latch + sibling cancel (%d)", call, latch)
	}
}

// The limit path's broker-permit refusal (hold landed after the pass snapshot):
// the arm stays "armed", the refusal is counted as maintenance_hold, nothing
// reaches the wire, and it is not logged as a placement failure.
func TestArmedLimitBrokerPermitRefusalLeavesTheArmArmed(t *testing.T) {
	at, st, sigs, now := liveArmFixture(t) // no hold file: the pass reads "not held"
	nt := at.armedTrader()
	if nt == nil {
		t.Fatal("fixture: no TCPTrader")
	}
	nt.SetEntryPermit(func() (func(), bool) { return nil, false })
	before := gateBlocks(at.id, "maintenance_hold")

	at.maybeManageArmedOrdersAt(nil, now)

	select {
	case s := <-sigs:
		t.Fatalf("an entry reached the wire under a refused broker permit (signal %s)", s.sid)
	case <-time.After(300 * time.Millisecond):
	}
	rows, err := st.ArmedOrders().ListNonTerminal(at.id)
	if err != nil {
		t.Fatal(err)
	}
	armed := 0
	for _, r := range rows {
		if r.State == store.StateArmed {
			armed++
		}
	}
	if armed == 0 {
		t.Fatalf("a permit refusal must leave the arm armed; rows: %+v", rows)
	}
	if gateBlocks(at.id, "maintenance_hold") <= before {
		t.Fatal("the broker-permit refusal was not counted as maintenance_hold")
	}
}

// M2.1 (review 3 F3, CTO: first after F4) — the permit-then-hold race. The
// pass read "not held" and the permit was granted, but the hold landed before
// the queue flush: the entry is DROPPED (ErrEntryHeld) and the drop sink
// retires the place_pending row as never sent. The log must say exactly that —
// not "the arm stays armed and places after the update", which was a lie.
func TestArmedEntryDroppedAfterItsPermitSaysTheArmWasRetired(t *testing.T) {
	withMaintenanceDir(t) // configured, no hold FILE: the pass reads "not held"
	at, st, _, now := liveArmFixture(t)
	nt := at.armedTrader()
	nt.SetEntryPermit(func() (func(), bool) { return func() {}, true }) // permit granted
	nt.SetEntryHoldCheck(func() bool { return true })                   // ...then the hold is seen at the flush
	nt.SetDroppedEntrySink(at.id, at.onMaintenanceDroppedEntry)
	logs := captureTraderLog(t)

	at.maybeManageArmedOrdersAt(nil, now)

	rows, err := st.ArmedOrders().ListNonTerminal(at.id)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.State == store.StatePlacePending {
			t.Fatalf("the dropped entry's row must be settled, not left place_pending: %+v", r)
		}
	}
	out := logs.String()
	if !strings.Contains(out, "never sent") {
		t.Fatalf("fixture: the queue drop must have settled the row as never sent; log:\n%s", out)
	}
	if strings.Contains(out, "stays armed") {
		t.Fatalf("the arm was retired by the drop — the log must not claim it stays armed; log:\n%s", out)
	}
	if !strings.Contains(out, "retired") {
		t.Fatalf("the refusal line must say the arm was retired (never sent); log:\n%s", out)
	}
}
