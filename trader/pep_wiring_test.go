package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/pep"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── PEP MODE, TRADER WIRING: pins through the PRODUCTION call sites ─────────
//
// Every pin below drives the real event pass (pepEventPassAt) over a REAL
// TCPTrader, a real TCP server (the fake AddOn reads the frames) and a real
// store - the same fixture the chat-door and armed-executor pins use. Each pin
// names the mutant that turns it RED.

func pepCfgOf(style string, contracts int) store.PepConfig {
	en, n := true, contracts
	return store.PepConfig{Enabled: &en, Style: style, Contracts: &n}
}

// newPepWire is a chat-door wire harness whose strategy is a pep strategy.
func newPepWire(t *testing.T, p store.PepConfig) *chatDoorWire {
	t.Helper()
	w := newChatDoorWire(t, store.RiskControlConfig{})
	cfg := store.StrategyConfig{Pep: &p}
	w.at.config.StrategyConfig = &cfg
	w.at.config.NinjaTraderSymbol = "MNQ"
	// the executor reads the account balance for its position-value ratio check;
	// the AddOn streams one in production
	w.s.SeedAccountBalanceForTest("Sim101", ntwire.AccountBalancePayload{Account: "Sim101", CashValue: 50000, BuyingPower: 50000, NetLiquidation: 50000})
	return w
}

const pepDay = "2026-10-29" // a Thursday, CDT

// pepFeed serves bars as the 1m ring.
func pepFeed(t *testing.T, bars []market.Kline) {
	t.Helper()
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(sym, tf string, n int) []market.Kline {
		if tf != "1m" {
			return nil
		}
		return tailOf(bars, n)
	}
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
}

// pepLive fixes the live price the entry path reads.
func pepLive(t *testing.T, px float64) {
	t.Helper()
	prev := openEntryMarketRead
	openEntryMarketRead = func(string, string) (*market.Data, error) { return &market.Data{CurrentPrice: px}, nil }
	t.Cleanup(func() { openEntryMarketRead = prev })
}

type pepBar struct{ o, h, l, c float64 }

// pepBarsA builds the style-A fixture: a flat 25020 tape from 07:00 CT, the
// 08:30-08:34 box 25000..25040 (size 40), plus the named later bars, ending at
// (excluding) upTo. Every bar is FINAL.
func pepBarsA(t *testing.T, date, upTo string, over map[string]pepBar) []market.Kline {
	t.Helper()
	box := map[string]pepBar{
		"08:30": {25010, 25040, 25000, 25020},
		"08:31": {25020, 25035, 25005, 25030},
		"08:32": {25030, 25036, 25010, 25015},
		"08:33": {25015, 25030, 25008, 25025},
		"08:34": {25025, 25034, 25012, 25020},
	}
	for k, v := range over {
		box[k] = v
	}
	start := ctTimeAt(t, date, "07:00").UnixMilli()
	end := ctTimeAt(t, date, upTo).UnixMilli()
	var out []market.Kline
	for ms := start; ms < end; ms += 60_000 {
		hh := time.UnixMilli(ms).In(ctTimeAt(t, date, "07:00").Location()).Format("15:04")
		b, ok := box[hh]
		if !ok {
			b = pepBar{25020, 25020, 25020, 25020}
		}
		out = append(out, market.Kline{OpenTime: ms, CloseTime: ms + 59_999, Open: b.o, High: b.h, Low: b.l, Close: b.c, Volume: 1, Final: true})
	}
	return out
}

// pepBreakUp is the day's long break: the 08:37 bar trades through the box high.
var pepBreakUp = map[string]pepBar{"08:37": {25030, 25041.25, 25029, 25040}}

func pepNow(t *testing.T) time.Time { return ctTimeAt(t, pepDay, "08:38").Add(2 * time.Second) }

func waitSignal(t *testing.T, w *chatDoorWire) ntwire.SignalPayload {
	t.Helper()
	select {
	case p := <-w.sigs:
		return p
	case <-time.After(3 * time.Second):
		t.Fatal("no entry reached the wire")
	}
	return ntwire.SignalPayload{}
}

func (w *chatDoorWire) noSignal(t *testing.T, what string) {
	t.Helper()
	select {
	case p := <-w.sigs:
		t.Fatalf("%s: an entry reached the wire: %+v", what, p)
	case <-time.After(300 * time.Millisecond):
	}
}

// ── ENTRY: ONE market order with its native bracket, 6 stays 6 ──────────────
//
// MUTANTS (each shown RED):
//   - resolveMaxContracts without the pep exception  -> the 0B clamp turns 6 into 2
//     (the quantity on the wire is 2, and the sizing guard refuses: no signal).
//   - pepMarkSending/claim removed from the send     -> TestPepOneTradePerDay*.
func TestPepEntryIsOneBracketedMarketOrderAndSixStaysSix(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 6))
	pepFeed(t, pepBarsA(t, pepDay, "08:38", pepBreakUp))
	pepLive(t, 25041)
	if got := w.at.resolveMaxContracts(); got != 6 {
		t.Fatalf("resolveMaxContracts = %d, want 6 (the pep exception: the 2-contract 0B clamp must not apply)", got)
	}

	if !w.at.pepEventPassAt(pepNow(t)) {
		t.Fatal("the pep event pass did not evaluate")
	}
	p := waitSignal(t, w)
	if p.Side != "long" || p.Quantity != 6 {
		t.Fatalf("entry %+v, want long x6", p)
	}
	// stop = live - full box (40), target = live + full box, on the tick grid
	if p.StopLoss != 25001 || p.TakeProfit != 25081 {
		t.Fatalf("bracket SL %.2f TP %.2f, want SL 25001 TP 25081 (live 25041 -/+ the 40 pt box)", p.StopLoss, p.TakeProfit)
	}
	// ONE order: no second frame of any kind follows
	w.noSignal(t, "after the one entry")

	// the durable record: one row, in the sending state, with the bracket sent
	ds, err := w.st.PepDay().Day(w.at.id, pepDay)
	if err != nil || ds.Counted != 1 || ds.Last == nil || ds.Last.State != store.PepDaySending {
		t.Fatalf("day record %+v err %v, want one 'sending' row", ds, err)
	}
	if ds.Last.Stop != 25001 || ds.Last.Target != 25081 || ds.Last.Contracts != 6 || ds.Last.Side != "long" {
		t.Fatalf("day row %+v", ds.Last)
	}
}

// 6 stays 6 on the shared sizing seam, and an AI/mentor trader keeps its clamp.
func TestPepMaxContractsExceptionOnlyForPep(t *testing.T) {
	pepT := &AutoTrader{id: "p", exchange: "ninjatrader"}
	c := pepCfgOf("A", 6)
	pepT.config.StrategyConfig = &store.StrategyConfig{Pep: &c}
	if got := pepT.resolveMaxContracts(); got != 6 {
		t.Fatalf("pep: resolveMaxContracts = %d, want 6", got)
	}
	over := pepCfgOf("A", 25) // above the hard ceiling: the resolver clamps to 10
	pepT.config.StrategyConfig = &store.StrategyConfig{Pep: &over}
	if got := pepT.resolveMaxContracts(); got != 10 {
		t.Fatalf("pep 25 saved: resolveMaxContracts = %d, want the hard ceiling 10", got)
	}
	ai := &AutoTrader{id: "a", exchange: "ninjatrader"}
	ai.config.StrategyConfig = &store.StrategyConfig{}
	untouched := kernel.ResolveMaxContracts(0, int(maxFuturesContracts)) // the 0B venue default through the Stage-A cap
	if got := ai.resolveMaxContracts(); got != untouched || got >= 6 {
		t.Fatalf("AI trader: resolveMaxContracts = %d, want the untouched %d", got, untouched)
	}
	off := pepCfgOf("A", 6)
	f := false
	off.Enabled = &f
	ai.config.StrategyConfig = &store.StrategyConfig{Pep: &off}
	if got := ai.resolveMaxContracts(); got != untouched {
		t.Fatalf("pep saved OFF: resolveMaxContracts = %d, want the untouched %d", got, untouched)
	}
}

// A sizing that is not EXACTLY the owner's contract count is refused, never
// silently shrunk or grown: a stale clamp (here, a max_contracts_per_order of 2
// would not apply; the mutant is a drifting notional) sends nothing.
func TestPepSizingDriftIsRefusedNotShrunk(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 6))
	pepFeed(t, pepBarsA(t, pepDay, "08:38", pepBreakUp))
	pepLive(t, 25041)
	// the executor re-reads the market at a price 40% away from the one the
	// notional was built from: notional/(price x pv) rounds to 4, not 6
	calls := 0
	prev := openEntryMarketRead
	openEntryMarketRead = func(string, string) (*market.Data, error) {
		calls++
		if calls == 1 { // the pep wire check's read
			return &market.Data{CurrentPrice: 25041}, nil
		}
		return &market.Data{CurrentPrice: 25041 * 1.5}, nil // the executor's read
	}
	t.Cleanup(func() { openEntryMarketRead = prev })
	before := gateBlocks(w.at.id, "pep_entry_failed")
	w.at.pepEventPassAt(pepNow(t))
	w.noSignal(t, "a sizing drift")
	if gateBlocks(w.at.id, "pep_entry_failed") != before+1 {
		t.Fatal("the refused drift must be counted as pep_entry_failed")
	}
	// the claim never reached the wire: it was released, the day is not burned
	ds, _ := w.st.PepDay().Day(w.at.id, pepDay)
	if ds.Counted != 0 {
		t.Fatalf("a claim that never reached the wire must be released, counted=%d", ds.Counted)
	}
}

// ── ONE TRADE PER DAY, DURABLE ──────────────────────────────────────────────
//
// MUTANT: pepEvalOnce passes Trades: 0 (ignores the record) -> the fresh trader
// object (a restart) sends a SECOND entry -> RED.
func TestPepOneTradePerDayAcrossARestart(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	pepFeed(t, pepBarsA(t, pepDay, "08:38", pepBreakUp))
	pepLive(t, 25041)
	w.at.pepEventPassAt(pepNow(t))
	waitSignal(t, w)

	// RESTART: a brand-new AutoTrader object (fresh pep runtime, empty memory)
	// over the same database and the same broker.
	cfg := *w.at.config.StrategyConfig
	re := &AutoTrader{id: w.at.id, exchange: "ninjatrader", store: w.st, trader: w.nt}
	re.config.StrategyConfig = &cfg
	re.config.NinjaTraderSymbol = "MNQ"
	re.positionFirstSeenTime = map[string]int64{}
	re.pepEventPassAt(pepNow(t).Add(30 * time.Second)) // inside the 2-minute sending window
	// three minutes on, the sending window is over: the restarted trader READS the
	// day's trade from the database and the evaluator answers the named cap
	// refusal (not merely a downstream layer's refusal)
	re.pepEventPassAt(pepNow(t).Add(3 * time.Minute))
	re.pep.truthMu.Lock()
	rs := re.pep.truth.res
	re.pep.truthMu.Unlock()
	if !rs.HasCode(pep.CodeMaxTrades) || rs.Diag.TradesTaken != 1 {
		t.Fatalf("the restarted trader must read 1 trade taken from the database: trades=%d refusals=%+v", rs.Diag.TradesTaken, rs.Refusals)
	}
	w.noSignal(t, "the restarted trader, same signal still on the tape")

	// the cap is the setting: 2 allowed -> the restarted trader may take a 2nd
	two := pepCfgOf("A", 3)
	n := 2
	two.MaxTradesPerDay = &n
	cfg2 := store.StrategyConfig{Pep: &two}
	re2 := &AutoTrader{id: w.at.id, exchange: "ninjatrader", store: w.st, trader: w.nt}
	re2.config.StrategyConfig = &cfg2
	re2.config.NinjaTraderSymbol = "MNQ"
	re2.positionFirstSeenTime = map[string]int64{}
	// style A takes the first break only (the evaluator's rule), so even a cap
	// of 2 sends nothing more for the same break; the durable count is what the
	// evaluator saw (Trades=1).
	re2.pepEventPassAt(pepNow(t).Add(120 * time.Second))
	w.noSignal(t, "style A takes the first break only")
}

// FAIL CLOSED: a record that cannot be read or written sends nothing.
func TestPepDayRecordUnreadableFailsClosed(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	pepFeed(t, pepBarsA(t, pepDay, "08:38", pepBreakUp))
	pepLive(t, 25041)
	if err := w.st.Close(); err != nil {
		t.Fatal(err)
	}
	before := gateBlocks(w.at.id, "pep_day_unreadable")
	w.at.pepEventPassAt(pepNow(t))
	w.noSignal(t, "an unreadable one-trade-per-day record")
	if gateBlocks(w.at.id, "pep_day_unreadable") != before+1 {
		t.Fatal("the unreadable record must be counted as pep_day_unreadable")
	}
}

// The claim is written BEFORE the send: at the moment the entry reaches the
// wire the durable row already exists (state sending). MUTANT: move the claim
// after the send -> the row is missing when the frame is read.
func TestPepClaimIsWrittenBeforeTheSend(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	pepFeed(t, pepBarsA(t, pepDay, "08:38", pepBreakUp))
	pepLive(t, 25041)
	// read the record from inside the market read the executor performs before
	// the send, and again when the frame is read on the wire
	w.at.pepEventPassAt(pepNow(t))
	waitSignal(t, w)
	ds, err := w.st.PepDay().Day(w.at.id, pepDay)
	if err != nil || ds.Last == nil || ds.Last.ClaimedAt == 0 || ds.Last.UpdatedAt < ds.Last.ClaimedAt {
		t.Fatalf("the claim row must carry its claim time before the wire: %+v err %v", ds.Last, err)
	}
	if ds.Last.SignalID == "" || !strings.HasPrefix(ds.Last.SignalID, "A|"+pepDay+"|long|") {
		t.Fatalf("signal id %q", ds.Last.SignalID)
	}
}

// An ADMISSION refusal (here the maintenance hold) happens before the claim: it
// consumes nothing, and the SAME signal is entered on the next pass once the
// hold clears. (A refusal AFTER the claim and before the wire releases the slot:
// TestPepSizingDriftIsRefusedNotShrunk. A refusal after the wire stays consumed.)
func TestPepAdmissionRefusalConsumesNothing(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	dir := withMaintenanceDir(t) // after the harness (it installs its own): this one is the live hold dir
	pepFeed(t, pepBarsA(t, pepDay, "08:38", pepBreakUp))
	pepLive(t, 25041)
	setHold(t, dir, "job-pep") // the maintenance hold refuses at the admission chain
	before := gateBlocks(w.at.id, "maintenance_hold")
	w.at.pepEventPassAt(pepNow(t))
	w.noSignal(t, "an entry during the maintenance hold")
	if gateBlocks(w.at.id, "maintenance_hold") <= before {
		t.Fatal("the maintenance hold refusal must be counted (KEPT in the pep branch of the chain)")
	}
	ds, _ := w.st.PepDay().Day(w.at.id, pepDay)
	if ds.Counted != 0 {
		t.Fatalf("an admission refusal sent nothing and must not consume the day (counted %d)", ds.Counted)
	}
	// the hold clears: the SAME signal is entered on the next pass
	if err := store.ClearMaintenanceHold(dir, "job-pep"); err != nil {
		t.Fatal(err)
	}
	w.at.pepEventPassAt(pepNow(t).Add(10 * time.Second))
	waitSignal(t, w)
}

// ── THE WIRE CHECK on the rounded prices ────────────────────────────────────

func TestPepWireCheck(t *testing.T) {
	in := pep.Intent{Side: pep.SideLong, Entry: 25040, StopDist: 40, TargetDist: 40}
	ok := pepWireCheck(in, 25041, 0.25)
	if ok.Class != "" || ok.Stop != 25001 || ok.Target != 25081 {
		t.Fatalf("clean long: %+v", ok)
	}
	// short mirrors
	sh := pepWireCheck(pep.Intent{Side: pep.SideShort, Entry: 25000, StopDist: 40, TargetDist: 40}, 24999.5, 0.25)
	if sh.Class != "" || sh.Stop != 25039.5 || sh.Target != 24959.5 {
		t.Fatalf("clean short: %+v", sh)
	}
	// slippage: 25 pts from the reference on a 40 pt stop (> 50%)
	if w := pepWireCheck(in, 25065, 0.25); w.Class != "pep_slippage" {
		t.Fatalf("slippage: %+v", w)
	}
	// no usable price
	if w := pepWireCheck(in, 0, 0.25); w.Class != "pep_no_live_price" {
		t.Fatalf("no price: %+v", w)
	}
	// the stop is rounded OUTWARD (away): a 1.10 pt stop on a 0.25 grid is 1.25
	// (the evaluator already rounds; the wire must never tighten it)
	odd := pepWireCheck(pep.Intent{Side: pep.SideLong, Entry: 100, StopDist: 1.1, TargetDist: 1.1}, 100, 0.25)
	if odd.Stop != 98.75 {
		t.Fatalf("long stop must round DOWN (away) to 98.75, got %+v", odd)
	}
	// reward:risk distorted by rounding (a 0.3 pt box on a 0.25 grid: stop up to
	// 0.5, target down to 0.25 -> 0.5:1 against an intended 1:1)
	if w := pepWireCheck(pep.Intent{Side: pep.SideLong, Entry: 100, StopDist: 0.3, TargetDist: 0.3}, 100, 0.25); w.Class != "pep_rr_distorted" {
		t.Fatalf("rr distortion: %+v", w)
	}
	// a collapsed bracket
	if w := pepWireCheck(pep.Intent{Side: pep.SideLong, Entry: 100, StopDist: 0.1, TargetDist: 0.1}, 100, 0.25); w.Class == "" {
		t.Fatalf("collapsed bracket must be refused: %+v", w)
	}
}

// The wire check is on the PRODUCTION path: a price that has run away since the
// signal sends nothing and is counted.
func TestPepSlippageRefusesAtTheProductionPath(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	pepFeed(t, pepBarsA(t, pepDay, "08:38", pepBreakUp))
	pepLive(t, 25100) // 60 pts above the 25040 reference on a 40 pt stop
	before := gateBlocks(w.at.id, "pep_slippage")
	w.at.pepEventPassAt(pepNow(t))
	w.noSignal(t, "a runaway price")
	if gateBlocks(w.at.id, "pep_slippage") != before+1 {
		t.Fatal("the slippage refusal must be counted as pep_slippage")
	}
	// repeated passes count the SAME refusal once (not once per bar)
	w.at.pepEventPassAt(pepNow(t).Add(5 * time.Second))
	if gateBlocks(w.at.id, "pep_slippage") != before+1 {
		t.Fatal("the same refusal on a later pass must not be re-counted")
	}
}

// ── NO BE / TRAIL / DRAWDOWN CLOSE ON A PEP FILL ────────────────────────────
//
// MUTANTS: modeOwnsExits() -> mentorEnabled() at each site makes the matching
// pin RED (the AI-era mover moves/closes a pep position).
func pepExitRig() *AutoTrader {
	c := pepCfgOf("A", 3)
	return &AutoTrader{
		id: "pep-exit", exchange: "ninjatrader", trader: &ntTrader.TCPTrader{},
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{
			Pep: &c,
			RiskControl: store.RiskControlConfig{
				BreakevenEnabled: bePtr(true), BreakevenTriggerPoints: 40,
				TrailingEnabled: bePtr(true), TrailingArm: TrailArmImmediate, TrailingATRMult: 2, TrailingATRPeriod: 14,
			},
		}},
	}
}

func TestPepNoAutoBreakevenOnAPepFill(t *testing.T) {
	t.Setenv("EXIT_MECHS_SUSPENDED", "0")
	at := pepExitRig()
	moved := false
	old := moveStopWire
	moveStopWire = func(*ntTrader.TCPTrader, string, float64) error { moved = true; return nil }
	t.Cleanup(func() { moveStopWire = old })
	resetAutoBEMentorBlockedCountForTest()
	get := warnPlusCapture(t)
	at.maybeMoveStopToBreakeven("MNQ", "LONG", 31023.50, 31068.50) // +45 >= 40: the trigger WOULD fire
	if moved {
		t.Fatal("pep mode: the AI-era auto-breakeven must NOT move the stop")
	}
	if AutoBEMentorBlockedCount() != 1 {
		t.Fatalf("blocked counter = %d, want 1", AutoBEMentorBlockedCount())
	}
	if !hasLine(get(), "auto-breakeven: not applied — pep mode ON") {
		t.Fatalf("the WARN must name pep mode, got %v", get())
	}
}

func TestPepNoTrailingOnAPepFill(t *testing.T) {
	t.Setenv("EXIT_MECHS_SUSPENDED", "0") // seam open: removing the gate must make this MOVE (the RED)
	restore := a3WireBars()
	defer restore()
	var movedTo []float64
	old := moveStopWire
	moveStopWire = func(_ *ntTrader.TCPTrader, _ string, newStop float64) error {
		movedTo = append(movedTo, newStop)
		return nil
	}
	t.Cleanup(func() { moveStopWire = old })

	at := pepExitRig()
	at.maybeTrailStop("MNQ", "LONG", 29600, 29700)
	if len(movedTo) != 0 {
		t.Fatalf("pep mode: the AI-era trail must NOT move the stop, moved to %v", movedTo)
	}
	if len(at.trailStates) != 0 {
		t.Fatalf("pep mode: the trail must create no state, got %d", len(at.trailStates))
	}
	// control: the SAME armed trail config with pep OFF moves the stop once, so
	// the pin above cannot pass on an empty trail path
	ctl := pepExitRig()
	f := false
	ctl.config.StrategyConfig.Pep.Enabled = &f
	ctl.maybeTrailStop("MNQ", "LONG", 29600, 29700)
	if len(movedTo) != 1 {
		t.Fatalf("control (pep OFF): the armed trail must move the stop once, got %v", movedTo)
	}
}

func TestPepNoDrawdownEmergencyCloseOnAPepFill(t *testing.T) {
	rt := &recordingTrader{MockTrader: &MockTrader{positions: []map[string]interface{}{
		{"symbol": "MNQ", "side": "LONG", "entryPrice": 100.0, "markPrice": 100.6, "positionAmt": 1.0, "leverage": 10.0},
	}}}
	c := pepCfgOf("A", 3)
	at := &AutoTrader{
		id: "pep-dd", trader: rt,
		config:       AutoTraderConfig{StrategyConfig: &store.StrategyConfig{Pep: &c}},
		peakPnLCache: map[string]float64{"MNQ_LONG": 10.0},
	}
	resetDrawdownMentorBlockedCountForTest()
	get := warnPlusCapture(t)
	at.checkPositionDrawdown()
	if rt.closedLong || rt.closedShort {
		t.Fatal("pep mode: the drawdown emergency close must NOT close the position")
	}
	if !hasLine(get(), "drawdown emergency close: not applied — pep mode ON") {
		t.Fatalf("the WARN must name pep mode, got %v", get())
	}
	// control: pep OFF keeps the close byte-identical
	off := &AutoTrader{
		id: "ai-dd", trader: &recordingTrader{MockTrader: &MockTrader{positions: rt.positions}},
		config:       AutoTraderConfig{StrategyConfig: &store.StrategyConfig{}},
		peakPnLCache: map[string]float64{"MNQ_LONG": 10.0},
	}
	off.checkPositionDrawdown()
	if !off.trader.(*recordingTrader).closedLong {
		t.Fatal("control: an AI trader's drawdown close must still fire")
	}
}

// AI-era closes are refused on a pep position (the cycle skips the AI anyway;
// this is the door for any other producer of a close decision).
func TestPepRefusesAnAIClose(t *testing.T) {
	c := pepCfgOf("A", 3)
	at := &AutoTrader{id: "pep-close", exchange: "ninjatrader", positionMentorOwned: map[string]bool{}}
	at.config.StrategyConfig = &store.StrategyConfig{Pep: &c}
	d := &kernel.Decision{Action: "close_long", Symbol: "MNQ"}
	if r := at.pepSuppressAIClose(d); r == "" {
		t.Fatal("a pep trader must refuse an AI close decision")
	}
	d.PepSourced = true
	if r := at.pepSuppressAIClose(d); r != "" {
		t.Fatalf("pep's own close passes: %q", r)
	}
	ai := &AutoTrader{id: "ai", exchange: "ninjatrader"}
	ai.config.StrategyConfig = &store.StrategyConfig{}
	if r := ai.pepSuppressAIClose(&kernel.Decision{Action: "close_long", Symbol: "MNQ"}); r != "" {
		t.Fatalf("AI trader: byte-identical, got %q", r)
	}
}

// ── THE 15:00 FLAT ──────────────────────────────────────────────────────────
//
// MUTANT 1: pepFlatBeat reading 14:45 (885) instead of the setting -> the 14:50
// leg sends a flatten -> RED.
// MUTANT 2: pepFlatBeat never called from the monitor beat / returns early past
// the flat time -> the 15:00 leg sends nothing -> RED.
func TestPepFlatsAt1500WithAnOpenPositionAndNeverEarly(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	entry := ctTimeAt(t, pepDay, "09:10").UnixMilli()
	if err := w.st.Position().Create(&store.TraderPosition{
		TraderID: w.at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG",
		Quantity: 3, EntryPrice: 25041, Status: "OPEN", EntryTime: entry, CreatedAt: entry, UpdatedAt: entry,
	}); err != nil {
		t.Fatal(err)
	}
	// the SESSION flat (14:45 CT) never acts on a pep trader
	if w.at.enforceEODFlatAt(ctTimeAt(t, pepDay, "14:50")) {
		t.Fatal("the session EOD flat must not act on a pep trader")
	}
	w.at.pepFlatBeat(ctTimeAt(t, pepDay, "14:50"))
	w.at.pepFlatBeat(ctTimeAt(t, pepDay, "14:59").Add(30 * time.Second))
	select {
	case <-w.cls:
		t.Fatal("pep flattened BEFORE its flat time")
	case <-time.After(300 * time.Millisecond):
	}
	// 15:00:05 CT: the flat is sent through the trader close path
	w.at.pepFlatBeat(ctTimeAt(t, pepDay, "15:00").Add(5 * time.Second))
	select {
	case <-w.cls:
	case <-time.After(3 * time.Second):
		t.Fatal("pep did not flatten the open position at 15:00 CT")
	}
}

// A position carried over from an earlier CT day is flattened at once.
func TestPepFlatsACarriedOverPosition(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	old := ctTimeAt(t, "2026-10-28", "10:00").UnixMilli()
	if err := w.st.Position().Create(&store.TraderPosition{
		TraderID: w.at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG",
		Quantity: 3, EntryPrice: 25041, Status: "OPEN", EntryTime: old, CreatedAt: old, UpdatedAt: old,
	}); err != nil {
		t.Fatal(err)
	}
	w.at.pepFlatBeat(ctTimeAt(t, pepDay, "08:20"))
	select {
	case <-w.cls:
	case <-time.After(3 * time.Second):
		t.Fatal("a position from an earlier CT day must be flattened")
	}
}

// ── TRUTH ───────────────────────────────────────────────────────────────────

func TestPepTruthSnapshot(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 6))
	pepFeed(t, pepBarsA(t, pepDay, "08:38", pepBreakUp))
	pepLive(t, 25041)
	now := pepNow(t)
	w.at.pepEventPassAt(now)
	waitSignal(t, w)
	tr := w.at.PepTruthSnapshot(now.Add(time.Minute))
	if !tr.Enabled || tr.Mode != "pep" || tr.Style != "A" || tr.Contracts != 6 {
		t.Fatalf("truth %+v", tr)
	}
	if tr.Box.High != 25040 || tr.Box.Low != 25000 || tr.Box.Size != 40 || !tr.Box.Drawn {
		t.Fatalf("box %+v", tr.Box)
	}
	if tr.TradesToday != 1 || tr.Stop != 25001 || tr.Target != 25081 || tr.Side != "long" {
		t.Fatalf("trade facts %+v", tr)
	}
	// 15:00 CT minus the snapshot instant (08:39:02 CT)
	want := int64(ctTimeAt(t, pepDay, "15:00").Sub(now.Add(time.Minute)) / time.Second)
	if tr.FlatTimeCT != "15:00" || tr.FlatCountdownSec != want {
		t.Fatalf("flat %q countdown %d, want 15:00 / %d", tr.FlatTimeCT, tr.FlatCountdownSec, want)
	}
	// off: the card hides itself
	off := &AutoTrader{id: "x", exchange: "ninjatrader"}
	off.config.StrategyConfig = &store.StrategyConfig{}
	if o := off.PepTruthSnapshot(now); o.Enabled || o.Mode != "ai" {
		t.Fatalf("an AI trader's card: %+v", o)
	}
}

// ── pep NEVER flattens an orphan ────────────────────────────────────────────
//
// The AI path's reconcile-before-open flattens a broker position no ledger row
// explains before it opens. pep refuses instead: it sends nothing on top of a
// position it cannot explain and flattens nothing it did not open.
// MUTANT: reconcileBeforeOpenNTOrPep delegating to reconcileBeforeOpenNTReport
// for a pep decision -> a close frame is sent -> RED.
func TestPepNeverFlattensAnOrphan(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	w.s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "long", Quantity: 2, AvgPrice: 25000}})
	// the inner step, directly
	d := &kernel.Decision{Symbol: "MNQ", Action: "open_long", PepSourced: true, PepContracts: 3}
	flattened, err := w.at.reconcileBeforeOpenNTOrPep(d, "long")
	if err == nil || flattened || !strings.Contains(err.Error(), "pep_broker_not_flat") {
		t.Fatalf("flattened=%v err=%v, want a pep_broker_not_flat refusal and no flatten", flattened, err)
	}
	select {
	case <-w.cls:
		t.Fatal("pep flattened an orphan position")
	case <-time.After(300 * time.Millisecond):
	}
	// and through the production event pass: nothing is sent, the refusal is counted
	pepFeed(t, pepBarsA(t, pepDay, "08:38", pepBreakUp))
	pepLive(t, 25041)
	before := gateBlocks(w.at.id, "pep_broker_not_flat")
	w.at.pepEventPassAt(pepNow(t))
	w.noSignal(t, "an entry on top of a broker position")
	select {
	case <-w.cls:
		t.Fatal("the event pass flattened an orphan position")
	case <-time.After(200 * time.Millisecond):
	}
	if gateBlocks(w.at.id, "pep_broker_not_flat") != before+1 {
		t.Fatal("the refusal must be counted as pep_broker_not_flat")
	}
}

// The pep reader of the wire: a PepSourced decision whose claim was never made
// (a producer that skipped the chain) is refused at the wire, nothing sent.
func TestPepWireRefusesAnUnclaimedEntry(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	d := &kernel.Decision{Symbol: "MNQ", Action: "open_long", StopLoss: 25001, TakeProfit: 25081, PepSourced: true, PepContracts: 3}
	if err := w.at.pepMarkSending(d); err == nil || !strings.Contains(err.Error(), "pep_no_claim") {
		t.Fatalf("an unclaimed entry must be refused at the wire: %v", err)
	}
	if r := w.at.pepClaimBeforeSend(d, pepNow(t)); !strings.HasPrefix(r, "pep_no_pending_claim") {
		t.Fatalf("a claim with no recorded signal must be refused: %q", r)
	}
	w.noSignal(t, "an unclaimed entry")
}
