package trader

import (
	"strings"
	"testing"
	"time"

	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// ── REVIEW ROUND 1 PINS (fix/m458-pep-trader) ───────────────────────────────

// DEFECT 1: a pep entry that reached the wire was logged "REFUSED" and saved as
// a failed decision, because executeDecisionWithRecordAt never sets Success on an
// open (the AI loop's caller does) and pepSendEntry read rec.Success.
// MUTANT: drop the `rec.Success = true` in pepSendEntry -> the saved decision
// reads success=false, the log says REFUSED, lastBlock is not reset -> RED.
func TestPepSentEntryIsRecordedAsASuccess(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	pepFeed(t, pepBarsA(t, pepDay, "08:38", pepBreakUp))
	pepLive(t, 25041)
	logBuf := captureTraderLog(t)
	w.at.pep.lastBlock = "pep_stale_block" // a success must reset it
	w.at.pepEventPassAt(pepNow(t))
	p := waitSignal(t, w)
	if p.Side != "long" || p.Quantity != 3 {
		t.Fatalf("entry %+v, want long x3", p)
	}
	recs, err := w.st.Decision().GetLatestRecords(w.at.id, 5)
	if err != nil || len(recs) != 1 {
		t.Fatalf("saved decisions %v err %v, want exactly one", recs, err)
	}
	r := recs[0]
	if !r.Success || r.ErrorMessage != "" || len(r.Decisions) != 1 || !r.Decisions[0].Success || r.Decisions[0].Error != "" {
		t.Fatalf("the sent entry is recorded as %+v (action %+v), want success with no error", r, r.Decisions)
	}
	out := logBuf.String()
	if strings.Contains(out, "REFUSED") {
		t.Fatalf("a sent pep entry must never be logged REFUSED:\n%s", out)
	}
	if !strings.Contains(out, "📈 pep A: open_long MNQ x3 SL 25001.00 TP 25081.00") {
		t.Fatalf("the success line is missing:\n%s", out)
	}
	if w.at.pep.lastBlock != "" {
		t.Fatalf("lastBlock = %q after a successful send, want reset", w.at.pep.lastBlock)
	}
}

// P3-1: an empty ring is not silent. The evaluator's named pep_no_bars block is
// counted ONCE per change and pep-truth shows it, not the previous pass's state.
// MUTANT: restore `if len(bars)==0 { return false }` in pepEventPassAt -> RED.
func TestPepEmptyRingIsCountedOnceAndShown(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	w := newPepWire(t, pepCfgOf("A", 3))
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return nil }
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	for i := 0; i < 3; i++ {
		if !w.at.pepEventPassAt(pepNow(t).Add(time.Duration(i) * time.Second)) {
			t.Fatalf("pass %d did not evaluate an empty ring", i)
		}
	}
	if n := gateBlocks(w.at.id, "pep_no_bars"); n != 1 {
		t.Fatalf("pep_no_bars counted %d times over three passes, want exactly 1 (once per change)", n)
	}
	snap := w.at.PepTruthSnapshot(pepNow(t))
	found := false
	for _, r := range snap.Refusals {
		if r.Code == "pep_no_bars" {
			found = true
		}
	}
	if !found || snap.Signal == "traded" || snap.Signal == "signal" {
		t.Fatalf("pep-truth signal %q refusals %v, want the named pep_no_bars", snap.Signal, snap.Refusals)
	}
	// a nil provider reads the same way
	market.FuturesBarsProvider = nil
	if !w.at.pepEventPassAt(pepNow(t)) {
		t.Fatal("a nil provider must evaluate as an empty ring, not return silently")
	}
}

// P3-2: pep admission asks runningNow. A pass already in flight when Stop lands
// must not place an entry. MUTANT: delete the admitPep runningNow refusal -> the
// long x3 reaches the wire -> RED. Control: the same pass on a running trader
// enters (TestPepEntryIsOneBracketedMarketOrderAndSixStaysSix).
func TestPepNotRunningRefusesTheEntryAndConsumesNothing(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	w := newPepWire(t, pepCfgOf("A", 3))
	w.at.isRunningMutex.Lock()
	w.at.isRunning = false
	w.at.isRunningMutex.Unlock()
	pepFeed(t, pepBarsA(t, pepDay, "08:38", pepBreakUp))
	pepLive(t, 25041)
	w.at.pepEventPassAt(pepNow(t))
	w.noSignal(t, "a NOT-running pep trader")
	if n := gateBlocks(w.at.id, "trader_stopped"); n < 1 {
		t.Fatalf("trader_stopped counted %d, want >= 1", n)
	}
	ds, err := w.st.PepDay().Day(w.at.id, pepDay)
	if err != nil || ds.Counted != 0 {
		t.Fatalf("a refusal that sent nothing must not burn the day's trade: %+v err %v", ds, err)
	}
}

// P3-3: the thesis-death exit never closes a pep position. CONTROL: the same
// fixture with pep OFF closes it (so the fixture proves the exit is live).
// MUTANT: `modeOwnsExits() && false` in maybeExitOnThesisDeath -> the pep leg
// sends a close_position -> RED.
func TestPepThesisDeathExitNeverTouchesAPepPosition(t *testing.T) {
	tune := func(withPep bool) func(*store.StrategyConfig) {
		return func(c *store.StrategyConfig) {
			c.DayPlan.PlanEnabled = false // pep and the day plan are exclusive
			c.DayPlan.PlannerExitOnThesisDeath = "all"
			if withPep {
				pc := pepCfgOf("A", 3)
				c.Pep = &pc
			}
		}
	}
	ctl := withdrawFixture(t, "pep-td-ctl", tune(false))
	pk2SeedPlannerPosition(t, ctl, "dormant", "i", "", "")
	ctl.at.monitorTick(ctl.now.Add(time.Minute))
	ctl.w.waitFor(t, "the control's thesis-death exit", func(fr wdFrame) bool { return fr.Type == ntwire.FrameClosePosition })

	f := withdrawFixture(t, "pep-td", tune(true))
	if !f.at.pepEnabled() {
		t.Fatal("fixture: the trader must be in pep mode")
	}
	pk2SeedPlannerPosition(t, f, "dormant", "i", "", "")
	f.at.monitorTick(f.now.Add(time.Minute))
	f.w.flush(t)
	if n := dlClosePositionFrames(f.w); n != 0 {
		t.Fatalf("the thesis-death exit closed a pep position (%d close frames)", n)
	}
}

// P3-3 (second guard): breakeven_trigger_r (the R rule) is an AI-era trigger; a
// pep trader keeps the points rule. CONTROL: the same position on an AI trader
// fires on the R rule. MUTANT: `modeOwnsExits() && false` in breakevenTriggerFor
// -> the pep trader fires on R -> RED.
func TestPepBreakevenTriggerKeepsThePointsRule(t *testing.T) {
	on := true
	mk := func(pepOn bool) (*AutoTrader, *store.Store) {
		cfg := store.StrategyConfig{}
		cfg.RiskControl.BreakevenEnabled = &on
		cfg.RiskControl.BreakevenTriggerPoints = 40
		cfg.RiskControl.BreakevenTriggerR = 1
		if pepOn {
			pc := pepCfgOf("A", 3)
			cfg.Pep = &pc
		}
		at, st := resetTrader(t, cfg)
		p := &store.TraderPosition{TraderID: at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG", Quantity: 1, EntryQuantity: 1,
			EntryPrice: 100, Status: "OPEN", Source: "armed_entry", EntryOrderID: "sig-be", EntryTime: 1, CreatedAt: 1, UpdatedAt: 1}
		if err := st.GormDB().Create(p).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := st.TradeExcursions().Open(store.TradeExcursion{PositionID: p.ID, Side: "LONG", EntryPx: 100, StopPxInitial: 90, TargetPx: 130, Size: 1, Source: "live"}); err != nil {
			t.Fatal(err)
		}
		return at, st
	}
	ai, _ := mk(false)
	if fire, _ := ai.breakevenTriggerFor("MNQ", "LONG", 100, 111); !fire {
		t.Fatal("control: an AI trader at +11 pts on a 10 pt initial risk fires the 1R rule")
	}
	pp, _ := mk(true)
	if !pp.pepEnabled() {
		t.Fatal("fixture: the trader must be in pep mode")
	}
	if fire, _ := pp.breakevenTriggerFor("MNQ", "LONG", 100, 111); fire {
		t.Fatal("a pep trader must keep the points rule (40 pts): +11 must not fire")
	}
}

// P3-4: the load-time refusal marks the trader NOT running in the database.
// MUTANT: UpdateStatus(..., true) (or no write) in pepStartRefusal -> RED.
func TestPepConflictMarksTheTraderNotRunningInTheDatabase(t *testing.T) {
	st, err := store.New(t.TempDir() + "/conflict.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	at := modeTrader(true, true, false)
	at.id, at.store = "conflict-db", st
	if err := st.Trader().Create(&store.Trader{ID: at.id, UserID: at.userID, Name: "M", IsRunning: true, InitialBalance: 1}); err != nil {
		t.Fatal(err)
	}
	if err := at.pepStartRefusal(); err == nil {
		t.Fatal("pep + mentor must be refused")
	}
	got, err := st.Trader().Get(at.id)
	if err != nil || got == nil {
		t.Fatalf("trader row: %v %v", got, err)
	}
	if got.IsRunning {
		t.Fatal("the refused trader is still marked running in the database")
	}
}

// P3-4: the flat's first act is cancelling what rests (a stray armed order of
// this trader), THEN the flatten. MUTANT: delete the cancelArmedOrdersSync call
// in pepFlatLocked -> the stray row stays non-terminal -> RED.
func TestPepFlatCancelsWhatRestsBeforeFlattening(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	arm := &store.ArmedOrderDB{TraderID: w.at.id, PlanID: "stray", Version: 1, Session: "NY", Scenario: "S1", Side: "long",
		EntryPx: 100, StopPx: 90, TargetPx: 130, State: "armed"}
	if err := w.st.ArmedOrders().DB().Create(arm).Error; err != nil {
		t.Fatal(err)
	}
	if rows, _ := w.st.ArmedOrders().ListNonTerminal(w.at.id); len(rows) != 1 {
		t.Fatalf("fixture: want one non-terminal armed row, got %d", len(rows))
	}
	w.at.pepFlatBeat(ctTimeAt(t, pepDay, "15:00").Add(5 * time.Second))
	if rows, _ := w.st.ArmedOrders().ListNonTerminal(w.at.id); len(rows) != 0 {
		t.Fatalf("the flat left %d armed order(s) resting", len(rows))
	}
}

// P3-4: past the verify window an empty local table is just an empty table;
// inside it the beat still checks it against the broker (here the broker holds
// a long our table does not show: counted flat_claim_refused_broker_disagrees).
// MUTANT: drop the pepFlatVerifyWindow bound -> the 15:30 beat verifies too -> RED.
func TestPepFlatVerifyWindowIsBounded(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	w := newPepWire(t, pepCfgOf("A", 3))
	w.s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "long", Quantity: 3, AvgPrice: 25041}})
	const code = "flat_claim_refused_broker_disagrees"
	w.at.pepFlatBeat(ctTimeAt(t, pepDay, "15:05"))
	inside := gateBlocks(w.at.id, code)
	if inside < 1 {
		t.Fatalf("inside the window the beat must check the empty table against the broker (%s = %d)", code, inside)
	}
	w.at.pepFlatBeat(ctTimeAt(t, pepDay, "15:30"))
	if got := gateBlocks(w.at.id, code); got != inside {
		t.Fatalf("past the verify window the beat must not verify (%s %d -> %d)", code, inside, got)
	}
}
