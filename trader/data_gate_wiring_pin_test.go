package trader

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	"vl/internal/ownerpush"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── m452 (release #55): THE PRODUCTION WIRING OF THE DATA GATES, PINNED ─────
//
// The m445/m446/m447 gates were pinned FUNCTION by function, and every pin called
// the function directly. Nothing pinned that production CALLS it. The #55 review
// removed each call in turn and the whole suite stayed green:
//
//	W1  wireDataTruthBoot() in NewAutoTrader      -> dataTruthWired never set, the
//	    m446/m447 leg inert, ALL ten checks stop gating entries, no log, no counter
//	W2  at.dataTruthBeat(now) in monitorTick       -> no episode / push until an
//	    entry happens to ask
//	W3  ntTrader.WireDataTruthChecks(...) in bar_persist_wire.go (pinned in
//	    trader/ninjatrader/data_gate_wiring_pin_test.go)
//	W4  at.registerBrokerAgreement() in Run         -> a comment-out passed the
//	    strings.Index pin
//
// These pins read the SOURCE through go/parser, so a call that is commented out,
// moved under an `if`, or deleted fails - the text-level pins accepted a comment.
// Where a behaviour can be driven (the beat in monitorTick, the wire re-check, the
// bounded beats) the pin drives the production function instead.
//
// MUTANTS (each shown RED in the m452 report):
//   M1  delete / comment out wireDataTruthBoot() in NewAutoTrader      -> TestNewAutoTraderSwitchesTheDataTruthGateOnUnderNoGuard
//   M2  move it back under `if st != nil`                              -> the same test
//   M3  delete runDataGateBeats(now) from monitorTick                  -> TestMonitorTickRunsTheDataTruthBeat..., ...BeatsLastAndBounded
//   M4  put dataTruthBeat back at the head of monitorTick              -> TestMonitorTickRunsTheDataGateBeatsAfterEveryGuard
//   M5  comment out at.registerBrokerAgreement() in Run                -> TestRunRegistersTheBrokerAgreementAfterTheReconcileStarts
//   M6  drop the wire re-check in openEntryWithRecord                  -> TestEntryIsRefusedAtTheWireWhenTheVerdictTurns
//   M7  reference a verdict from an exit-side function                 -> TestDataVerdictIsConsultedOnlyByTheEntryChain

// liveCall is one non-comment call site found in a function body.
type liveCall struct {
	pos    token.Pos
	line   int
	nested []string // kinds of enclosing statements: "if", "for", "switch", "func-lit"
}

// liveCalls returns every call to `callee` (a bare name, or the Sel of a selector)
// inside funcName (a FuncDecl.Name) of file. Comments are not in the AST, so a
// commented-out call is not found.
func liveCalls(t *testing.T, file, funcName, callee string) []liveCall {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	fs := token.NewFileSet()
	af, err := parser.ParseFile(fs, file, b, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	var out []liveCall
	for _, d := range af.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != funcName || fd.Body == nil {
			continue
		}
		var stack []ast.Node
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch e := c.Fun.(type) {
			case *ast.Ident:
				name = e.Name
			case *ast.SelectorExpr:
				name = e.Sel.Name
			}
			if name != callee {
				return true
			}
			lc := liveCall{pos: c.Pos(), line: fs.Position(c.Pos()).Line}
			for _, s := range stack[:len(stack)-1] {
				switch s.(type) {
				case *ast.IfStmt:
					lc.nested = append(lc.nested, "if")
				case *ast.ForStmt, *ast.RangeStmt:
					lc.nested = append(lc.nested, "for")
				case *ast.SwitchStmt, *ast.TypeSwitchStmt:
					lc.nested = append(lc.nested, "switch")
				case *ast.FuncLit:
					lc.nested = append(lc.nested, "func-lit")
				}
			}
			out = append(out, lc)
			return true
		})
	}
	return out
}

// W1/M1/M2 - the switch-on of the data-truth gate is a live call in NewAutoTrader,
// UNDER NO `if`. It used to sit inside `if st != nil` (a NinjaTrader trader built
// without a store left dataTruthWired false: the m446/m447 leg silently inert,
// fail OPEN); it may sit under the exchange `switch` case only.
func TestNewAutoTraderSwitchesTheDataTruthGateOnUnderNoGuard(t *testing.T) {
	calls := liveCalls(t, "auto_trader.go", "NewAutoTrader", "wireDataTruthBoot")
	if len(calls) != 1 {
		t.Fatalf("NewAutoTrader must call wireDataTruthBoot() exactly once as live code (a comment is not a call), found %d", len(calls))
	}
	for _, k := range calls[0].nested {
		if k == "if" || k == "for" || k == "func-lit" {
			t.Fatalf("wireDataTruthBoot() at auto_trader.go:%d sits under a %q - the gate must switch on for EVERY NinjaTrader trader, store or not (a store-less trader must reach the fail-closed stubs, not an inert gate)", calls[0].line, k)
		}
	}
	// ...and it is the ninjatrader case: the call comes after the NT8 construction
	// and is not the crypto path's.
	nt := liveCalls(t, "auto_trader.go", "NewAutoTrader", "NewTraderFromEnv")
	if len(nt) == 0 || calls[0].pos < nt[0].pos {
		t.Fatalf("wireDataTruthBoot() must follow ntTrader.NewTraderFromEnv in the ninjatrader case")
	}
}

// W4/M5 - registerBrokerAgreement is a LIVE call in Run, after the reconcile
// starts (the weak text pin accepted `//MUT at.registerBrokerAgreement()`).
func TestRunRegistersTheBrokerAgreementAfterTheReconcileStarts(t *testing.T) {
	reg := liveCalls(t, "auto_trader.go", "Run", "registerBrokerAgreement")
	rec := liveCalls(t, "auto_trader.go", "Run", "StartPositionReconcile")
	if len(reg) != 1 || len(rec) != 1 {
		t.Fatalf("Run must call StartPositionReconcile and registerBrokerAgreement once each as live code (found reconcile=%d register=%d)", len(rec), len(reg))
	}
	if reg[0].pos < rec[0].pos {
		t.Fatalf("registerBrokerAgreement (line %d) must come after StartPositionReconcile (line %d)", reg[0].line, rec[0].line)
	}
	if reg[0].line-rec[0].line > 30 {
		t.Fatalf("registerBrokerAgreement is %d lines after the reconcile start - it must follow it directly", reg[0].line-rec[0].line)
	}
	if un := liveCalls(t, "auto_trader.go", "teardownRunStarted", "unregisterBrokerAgreement"); len(un) != 1 {
		t.Fatalf("teardownRunStarted must unregister the trader (live call), found %d", len(un))
	}
}

// W2 (source half)/M4 - monitorTick calls the beats LAST: after every guard, and
// only through the bounded runner. Neither beat is called directly.
func TestMonitorTickRunsTheDataGateBeatsAfterEveryGuard(t *testing.T) {
	run := liveCalls(t, "auto_trader_risk.go", "monitorTick", "runDataGateBeats")
	if len(run) != 1 {
		t.Fatalf("monitorTick must call runDataGateBeats exactly once as live code, found %d", len(run))
	}
	for _, direct := range []string{"dataIntegrityBeat", "dataTruthBeat"} {
		if c := liveCalls(t, "auto_trader_risk.go", "monitorTick", direct); len(c) != 0 {
			t.Fatalf("monitorTick calls %s directly (line %d): the beats run only through the bounded runner, after the guards", direct, c[0].line)
		}
	}
	for _, guard := range []string{"checkPositionDrawdown", "reconcileProtectionAt", "dailyLossFlattenAt", "enforceEODFlat", "mentorSweepsOnBeat", "withdrawEntriesIfDue"} {
		g := liveCalls(t, "auto_trader_risk.go", "monitorTick", guard)
		if len(g) != 1 {
			t.Fatalf("monitorTick must call %s once as live code, found %d", guard, len(g))
		}
		if g[0].pos > run[0].pos {
			t.Fatalf("%s (line %d) runs AFTER the data-gate beats (line %d): protection, flatten and EOD must never wait on them", guard, g[0].line, run[0].line)
		}
	}
	// the runner itself calls both beats
	for _, beat := range []string{"dataIntegrityBeat", "dataTruthBeat"} {
		if c := liveCalls(t, "data_beats_bounded.go", "runDataGateBeats", beat); len(c) != 1 {
			t.Fatalf("runDataGateBeats must call %s once as live code, found %d", beat, len(c))
		}
	}
}

// W2 (behaviour half)/M3 - the PRODUCTION beat (monitorTick) raises the
// data-truth episode and its owner push with NO entry attempted.
func TestMonitorTickRunsTheDataTruthBeatAndRaisesTheEpisode(t *testing.T) {
	f := armDataTruthGate(t, checks.CheckBadPrint)
	f.blocked.Store(true)
	var mu sync.Mutex
	var pushed []string
	datatruth.SetNotifier(func(kind, text string) { mu.Lock(); pushed = append(pushed, kind); mu.Unlock() })
	at, _ := feedTrader(t)
	at.id = "datatruth-tick-1"
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 30_000, Final: true}})
	at.monitorTick(now)
	mu.Lock()
	n := len(pushed)
	mu.Unlock()
	if n != 1 || !datatruth.Snapshot()[checks.CheckBadPrint].Blocked {
		t.Fatalf("one monitorTick must raise the bad_print episode (pushed=%v snapshot=%+v): the 60 s beat is what alerts a bad print when nobody is entering", pushed, datatruth.Snapshot()[checks.CheckBadPrint])
	}
	f.blocked.Store(false)
	at.monitorTick(now.Add(time.Minute))
	if datatruth.Snapshot()[checks.CheckBadPrint].Blocked {
		t.Fatal("a measured clear on the next beat must close the episode")
	}
}

// M3/M4 (bounded) - a stuck data check never holds monitorTick: the beat returns
// within the budget, the episode work finishes on its own goroutine, and a later
// beat does not stack a second run behind the stuck one.
func TestMonitorTickRunsTheDataGateBeatsBoundedAndNeverStacks(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int64
	datatruth.Reset()
	prevWired := dataTruthWired.Swap(true)
	prevAt := dataTruthWiredAt.Swap(time.Now().UnixMilli())
	prevReady := dataTruthReadyFn
	prevBudget := dataBeatBudget
	dataTruthReadyFn = func() bool { return true }
	dataBeatBudget = 80 * time.Millisecond
	t.Cleanup(func() {
		close(release) // let the stuck beat finish before the registry is reset
		time.Sleep(50 * time.Millisecond)
		datatruth.Reset()
		dataTruthWired.Store(prevWired)
		dataTruthWiredAt.Store(prevAt)
		dataTruthReadyFn = prevReady
		dataBeatBudget = prevBudget
	})
	datatruth.Register(checks.CheckBadPrint, func(string, time.Time) (bool, string, error) {
		calls.Add(1)
		<-release // a hung store read
		return false, "", nil
	})
	at, _ := feedTrader(t)
	at.id = "datatruth-bounded-1"
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 30_000, Final: true}})

	tickDone := make(chan struct{})
	go func() { at.monitorTick(now); close(tickDone) }()
	select {
	case <-tickDone:
	case <-time.After(2 * time.Second):
		t.Fatal("monitorTick is stuck behind a hung data check: the beats are not bounded")
	}
	// the next minute's beat finds the stuck one still in flight and skips it
	at.monitorTick(now.Add(time.Minute))
	at.monitorTick(now.Add(2 * time.Minute))
	time.Sleep(50 * time.Millisecond)
	if c := calls.Load(); c != 1 {
		t.Fatalf("%d concurrent data-truth evaluations from 3 beats behind one stuck read, want exactly 1 (no stacking)", c)
	}
}

// W3-adjacent: the data verdict is consulted ONLY by the entry chain. A census of
// every non-test function in trader/ that references a verdict function; a NEW
// caller (say in an exit, protection or flatten path) fails here and has to be
// added to the allowlist on purpose, in review. This is the cheap guard the
// review asked for in place of driving every exit under a blocked verdict.
func TestDataVerdictIsConsultedOnlyByTheEntryChain(t *testing.T) {
	banned := map[string]bool{
		"dataIntegrityVerdictAt": true, "integrityVerdictAt": true, "dataTruthVerdictAt": true,
		"dataTruthReasons": true, "dataIntegrityAdmitRefusal": true, "dataIntegrityTestSeamRefusal": true,
		"integrityWithdrawReason": true, "seedSeriesVerdict": true,
	}
	// file:function -> why it may ask
	allowed := map[string]string{
		"armed_executor.go:mentorOnlyPlacementPass":           "mentor armed-pass placement (entry)",
		"armed_executor.go:TestArmPlace":                      "SIM test-arm seam (an entry)",
		"armed_executor.go:TestArmPlaceStop":                  "SIM test-arm seam (an entry)",
		"auto_trader_orders.go:openEntryWithRecord":           "m452 wire re-check (entry)",
		"data_integrity_gate.go:dataIntegrityVerdictAt":       "THE verdict",
		"data_integrity_gate.go:integrityVerdictAt":           "the m445 leg",
		"data_integrity_gate.go:dataIntegrityAdmitRefusal":    "admitChain's gate (entry)",
		"data_integrity_gate.go:dataIntegrityTestSeamRefusal": "SIM test seam (an entry)",
		"data_integrity_gate.go:DebugNTTestTrade":             "debug MARKET entry door",
		"data_integrity_gate.go:integrityWithdrawReason":      "withdraws RESTING ENTRIES only",
		"data_integrity_gate.go:dataIntegrityBeat":            "the entry-pause episode (acts on nothing but entries)",
		"data_integrity_seed.go:HealthSeedSeriesPause":        "health display of the seed leg",
		"data_truth_gate.go:dataTruthVerdictAt":               "the m446/m447 fold",
		"data_truth_gate.go:dataTruthBeat":                    "the data-truth episode (acts on nothing)",
		"entry_admission.go:admitChain":                       "THE entry chain",
		"mentor_tick.go:mentorPlaceIntent":                    "mentor arm authoring (entry)",
		"withdraw.go:withdrawEntriesIfDue":                    "withdraws RESTING ENTRIES only",
	}
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fs := token.NewFileSet()
	var found []string
	for _, de := range files {
		n := de.Name()
		if de.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(fs, n, b, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			hit := false
			ast.Inspect(fd.Body, func(x ast.Node) bool {
				if c, ok := x.(*ast.CallExpr); ok {
					switch e := c.Fun.(type) {
					case *ast.Ident:
						hit = hit || banned[e.Name]
					case *ast.SelectorExpr:
						hit = hit || banned[e.Sel.Name]
					}
				}
				return true
			})
			if hit {
				found = append(found, n+":"+fd.Name.Name)
			}
		}
	}
	sort.Strings(found)
	for _, f := range found {
		if _, ok := allowed[f]; !ok {
			t.Errorf("%s asks the data verdict but is not on the entry-chain allowlist: exits, stops, BE/trail, protection, flatten and reconciliation must NEVER consult it (owner principle: pause NEW entries only). If this is an entry seam, add it to the allowlist with its reason", f)
		}
	}
	// the allowlist is also a census: a listed seam that no longer asks has been
	// unwired (an entry path that stopped consulting the verdict fails OPEN)
	have := map[string]bool{}
	for _, f := range found {
		have[f] = true
	}
	for f := range allowed {
		if !have[f] {
			t.Errorf("allowlisted %s no longer references a data verdict: the seam was unwired (fail OPEN) or renamed", f)
		}
	}
}

// M6 - THE VERDICT IS ASKED AGAIN AT THE WIRE. The agent-chat entry is admitted at
// the head of OpenManualEntryAt; the market read (and, on the AI path, the reconcile
// flatten await of up to 35 s) sits between that and the send. The verdict turns
// INSIDE that gap (the market-read seam flips it): the admitted entry must be
// refused at openEntryWithRecord, name its class, count it, and send nothing. The
// control leg proves the same call goes out when the verdict stays clear.
func TestEntryIsRefusedAtTheWireWhenTheVerdictTurns(t *testing.T) {
	for _, turns := range []bool{false, true} {
		name := "control: the verdict stays clear, the entry is sent"
		if turns {
			name = "the verdict turns after admission, the entry is refused at the wire"
		}
		t.Run(name, func(t *testing.T) {
			w := newChatDoorWire(t, store.RiskControlConfig{})
			stop, target := chatBracket(t, "open_long")
			var turned atomic.Bool
			armIntegrityGate(t, &intFake{blockedFor: func(int) bool { return turned.Load() }})
			prevRead := openEntryMarketRead
			openEntryMarketRead = func(sym, ex string) (*market.Data, error) {
				turned.Store(turns) // the gap: admission already said yes
				return prevRead(sym, ex)
			}
			t.Cleanup(func() { openEntryMarketRead = prevRead })
			before := gateBlocks(w.at.id, ntTrader.IntegrityClassHole)

			_, err := w.at.OpenManualEntryAt("MNQ", "open_long", 1, 1, stop, target, chatDoorMidday)
			if !turns {
				if err != nil {
					t.Fatalf("control: a clear verdict must let the admitted entry through: %v", err)
				}
				select {
				case <-w.sigs: // the entry reached the wire
				case <-time.After(2 * time.Second):
					t.Fatal("control: the clear entry never reached the wire")
				}
				return
			}
			if err == nil || !strings.HasPrefix(err.Error(), "data_integrity: ") {
				t.Fatalf("an entry whose verdict turned between admission and the send must be refused at the wire with the data_integrity class, got %v", err)
			}
			if got := gateBlocks(w.at.id, ntTrader.IntegrityClassHole); got != before+1 {
				t.Fatalf("the wire refusal must be counted under %s once: %d -> %d", ntTrader.IntegrityClassHole, before, got)
			}
			w.nothingSent(t, "wire re-check")
		})
	}
}

// m452 - THE PAUSE PUSHES ARE PER TRADER. ownerpush budgets 3 pushes per hour per
// KIND (the text before the first "|"); "data-paused|<id>|ts" put every trader on
// one MNQ hole in ONE bucket, so a fourth trader's pause (or any trader's widen /
// narrow / resume) was held. FOUR traders pause on the same hole through the
// production beat (dataIntegrityBeat) and all four owner pushes must be sent.
// MUTANT: ownerPushPausedKey back to "data-paused|%s|%d" -> only 3 sent -> RED.
func TestPausePushesAreBudgetedPerTraderNotPerKind(t *testing.T) {
	rec, flush := installPush(t)
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 30_000, Final: true}})
	armIntegrityGate(t, &intFake{})
	for i, id := range []string{"mentor-a", "mentor-b", "mentor-c", "mentor-d"} {
		at, _ := feedTrader(t)
		at.id = id
		at.name = "Trader " + string(rune('A'+i))
		at.dataIntegrityBeat(now)
	}
	flush()
	if n := countContaining(rec.all(), "TRADING PAUSED"); n != 4 {
		t.Fatalf("4 traders paused on one hole, %d pause pushes were sent (the budget is per trader, 3/hour each): %q", n, rec.all())
	}
	if k := ownerPushPausedKey("mentor-a", now); k == ownerPushPausedKey("mentor-b", now) || strings.SplitN(k, "|", 2)[0] == strings.SplitN(ownerPushPausedKey("mentor-b", now), "|", 2)[0] {
		t.Fatalf("the pause push KIND (text before the first '|') must differ per trader: %q", k)
	}
	if r := ownerPushResumedKey(ownerPushPausedKey("mentor-a", now)); !strings.HasPrefix(r, "data-resumed-mentor-a|") {
		t.Fatalf("the resume key must keep the trader in its kind: %q", r)
	}
}

// Release #55 round 3 (review 5, defect 1): the boot pin looped over the eight
// m446 names only, and the m447/m451 tests call RegisterBrokerTruthChecks() /
// RegisterFillPrice() themselves - so deleting RegisterBrokerTruthChecks() from
// doWireDataTruthBoot left the full ./trader suite green while broker_agreement
// and fill_price stayed "not wired" stubs forever: a silent total halt of every
// NT8 trader's NEW entries once the boot grace ends, found only in production.
//
// Driven through the PRODUCTION boot body (doWireDataTruthBoot, not the Register
// helpers): after it, both account-side checks must be REAL checks, and neither
// may answer Evaluate with a "not wired" stub reason.
//
// MUTANT: delete RegisterBrokerTruthChecks() from doWireDataTruthBoot -> RED.
func TestDoWireDataTruthBootRegistersTheTwoAccountChecksForReal(t *testing.T) {
	datatruth.Reset()
	prevWired := dataTruthWired.Load()
	t.Cleanup(func() { datatruth.Reset(); dataTruthWired.Store(prevWired) })
	t.Cleanup(ownerpush.Use(ownerpush.NewE(func(string) error { return nil })))
	doWireDataTruthBoot()
	for _, n := range []string{CheckBrokerAgreement, checks.CheckFillPrice} {
		if !datatruth.Wired(n) {
			t.Fatalf("%s is still a Require stub after doWireDataTruthBoot: RegisterBrokerTruthChecks is not called from the boot wiring, so every NT8 trader's entries block forever once the grace ends", n)
		}
	}
	_, reasons := datatruth.Evaluate("MNQ", time.Now())
	for _, r := range reasons {
		if (r.Check == CheckBrokerAgreement || r.Check == checks.CheckFillPrice) && strings.Contains(r.Detail, "not wired") {
			t.Fatalf("%s reports a stub after the boot wiring: %+v", r.Check, r)
		}
	}
}

// Release #55 round 3 (review 4, D6): the fill_price pause text carries the API's
// own address, read at the PRODUCTION registration (RegisterFillPrice, reached
// from doWireDataTruthBoot) - not a bare relative path.
//
// MUTANT: delete checks.SetAckBaseURL(...) from RegisterFillPrice -> RED.
func TestRegisterFillPriceSetsTheAcknowledgementAddressFromTheConfig(t *testing.T) {
	prev := checks.AckBaseURL()
	t.Cleanup(func() { checks.SetAckBaseURL(prev) })
	checks.SetAckBaseURL("http://placeholder.invalid:1")
	prevGuard := checks.SetDefaultFills(checks.NewFillGuard(checks.DefaultFillParams(), nil))
	t.Cleanup(func() { checks.SetDefaultFills(prevGuard); datatruth.Reset() })
	RegisterFillPrice()
	want := ackBaseURLFromConfig()
	if got := checks.AckBaseURL(); got != want || !strings.HasPrefix(got, "http://") || strings.Contains(got, "invalid") {
		t.Fatalf("RegisterFillPrice must set the ack address from the config: got %q want %q", got, want)
	}
}
