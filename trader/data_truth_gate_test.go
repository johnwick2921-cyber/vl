package trader

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	"vl/internal/ownerpush"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── DATA-TRUTH GATE (release #55 integrator: m446 + m447 -> m445's ONE verdict) ──
//
// Every pin drives the PRODUCTION call site (admitEntry via the AI decision
// executor and the manual door, the mentor authoring seam, the mentor armed
// pass, the SIM test seams, the debug door) with a REGISTERED datatruth check
// that blocks - not a fake of the verdict. Exits are driven the same way and must
// never ask.
//
// MUTANTS (each shown RED in the report):
//   W1  dataIntegrityVerdictAt drops the dataTruthVerdictAt fold  -> every refusal pin RED
//   W2  dataIntegrityAdmitRefusal returns early when !integrityApplies (the pre-#55 shape)
//                                                               -> AI / manual pins RED
//   W3  dataTruthReasons ignores the swing4h scope               -> scope pin RED
//   W4  dataTruthBeat never evaluates                           -> episode-without-entry pin RED
//   W5  wireDataTruthBoot skips Require / UseOwnerPush           -> boot wiring pins RED
//   W6  dataIntegrityBeat asks dataIntegrityVerdictAt (the folded one) -> no-withdraw pin RED

// truthFake is one registered check the pins steer.
type truthFake struct {
	blocked atomic.Bool
	calls   atomic.Int64
	why     string
}

func (f *truthFake) fn(symbol string, now time.Time) (bool, string, error) {
	f.calls.Add(1)
	if f.blocked.Load() {
		return true, f.why, nil
	}
	return false, "", nil
}

// armDataTruthGate turns the trader-side gate on over a clean datatruth registry
// and registers `name` as a steerable check.
func armDataTruthGate(t *testing.T, name string) *truthFake {
	t.Helper()
	datatruth.Reset()
	prevWired := dataTruthWired.Swap(true)
	prevAt := dataTruthWiredAt.Swap(time.Now().UnixMilli())
	prevReady := dataTruthReadyFn
	dataTruthReadyFn = func() bool { return true }
	t.Cleanup(func() {
		datatruth.Reset()
		dataTruthWired.Store(prevWired)
		dataTruthWiredAt.Store(prevAt)
		dataTruthReadyFn = prevReady
	})
	f := &truthFake{why: "2 suspect 1m bar(s): 10-19 08:58 close 82 pt away"}
	datatruth.Register(name, f.fn)
	return f
}

// An entry through the AI decision executor and the manual door is refused by a
// blocking data-truth check, named and counted under data_truth; the SAME call
// proceeds the moment the check measures clear.
func TestDataTruthRefusesEntriesThroughAdmitEntry(t *testing.T) {
	f := armDataTruthGate(t, checks.CheckBadPrint)
	f.blocked.Store(true)
	at, _ := pauseTrader(t)
	at.id = "datatruth-ai-1"
	for _, action := range []string{"open_long", "open_short"} {
		before := gateBlocks(at.id, integrityClassDataTruth)
		rec := &store.DecisionAction{Action: action, Symbol: "MNQ"}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s was NOT refused at the gate: it ran on past admitChain (panic %v)", action, r)
				}
			}()
			if err := at.executeDecisionWithRecord(&kernel.Decision{Action: action, Symbol: "MNQ"}, rec); err != nil {
				t.Fatalf("%s: unexpected error %v", action, err)
			}
		}()
		if rec.Success || !strings.HasPrefix(rec.Error, "data_truth: ") ||
			!strings.Contains(rec.Error, "bad_print: 2 suspect 1m bar(s)") {
			t.Fatalf("%s must be refused by name at executeDecisionWithRecord: %q", action, rec.Error)
		}
		if got := gateBlocks(at.id, integrityClassDataTruth); got != before+1 {
			t.Fatalf("%s: the refusal must be counted under %s once: %d -> %d", action, integrityClassDataTruth, before, got)
		}
	}
	if reason, refused := at.AdmitManualEntryBracketAt("MNQ", "open_long", 0, 0, time.Now()); !refused || !strings.HasPrefix(reason, "data_truth: ") {
		t.Fatalf("the agent-chat door must be refused by data truth: refused=%v reason=%q", refused, reason)
	}
	// resume is on a MEASURED clear: the check says clear -> the gate lets the entry on to the next gate
	f.blocked.Store(false)
	if reason, refused := at.AdmitManualEntryBracketAt("MNQ", "open_long", 0, 0, time.Now()); refused && strings.HasPrefix(reason, "data_truth") {
		t.Fatalf("a measured-clear check must not refuse: %q", reason)
	}
}

// Every admitted PATH crosses the fold: planner arm, mentor arm, Picture. (The
// window per path is pinned beside the m445 gate; here the fold is what is
// proven.)
func TestDataTruthRefusesEveryAdmitPath(t *testing.T) {
	f := armDataTruthGate(t, checks.CheckCrossFrame)
	f.blocked.Store(true)
	f.why = "1 frame mismatch(es): 5m 08:34 low 56 pt off"
	at, _ := pauseTrader(t)
	at.id = "datatruth-paths-1"
	at.isRunning = true
	at.config.StrategyConfig = &store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	now := time.Now()
	for name, in := range map[string]admitIntent{
		"planner arm":  {Path: admitArm},
		"mentor arm":   {Path: admitArm, MentorArm: true},
		"mentor swing": {Path: admitArm, MentorArm: true, Swing: true},
		"picture":      {Path: admitPicture},
		"agent":        {Path: admitAgent},
		"ai decision":  {Path: admitDecision},
	} {
		in.Symbol, in.Action, in.Now, in.Key = "MNQ", "open_long", now, "k"
		reason, refused := at.admitEntry(in)
		if !refused || !strings.HasPrefix(reason, "data_truth: ") || !strings.Contains(reason, "cross_frame") {
			t.Fatalf("%s must be refused by the data-truth fold: refused=%v %q", name, refused, reason)
		}
	}
}

// The mentor AUTHORING seam and the mentor ARMED PASS both cross the fold.
func TestDataTruthRefusesMentorAuthoringAndTheArmedPass(t *testing.T) {
	f := armDataTruthGate(t, checks.CheckLookback)
	f.blocked.Store(true)
	f.why = "ema34 1m: 3 missing bars weigh 4.2 pt"

	// authoring
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.exchange = "ninjatrader"
	now := mentorStaleBlockClock(9, 0)
	setMentorNowSource(func() time.Time { return now })
	t.Cleanup(func() { setMentorNowSource(nil) })
	wireMentorPlacementSeams(t, at)
	mentorStaleBlockFeed(t, now, 30*time.Second)
	var placed []mentor.Intent
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed = append(placed, i) }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })
	before := gateBlocks(at.id, integrityClassDataTruth)
	at.mentorPlaceIntent(mentorStaleBlockIntent(), mentorSizeChoice{Contracts: 1, Tier: "base"}, 1000, 1100)
	if len(placed) != 0 {
		t.Fatalf("a failing data-truth check must NOT author a mentor arm, %d reached the recorder", len(placed))
	}
	if at.mentorCountSnapshot()["placement_refused_data_integrity"] != 1 || gateBlocks(at.id, integrityClassDataTruth) != before+1 {
		t.Fatalf("the refusal must be counted: %v", at.mentorCountSnapshot())
	}
	f.blocked.Store(false)
	at.mentorPlaceIntent(mentorStaleBlockIntent(), mentorSizeChoice{Contracts: 1, Tier: "base"}, 1000, 1100)
	if len(placed) != 1 {
		t.Fatalf("a measured-clear check must let the SAME intent author, placed %d", len(placed))
	}

	// armed pass
	f.blocked.Store(true)
	pnow := time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC)
	pat, _, signals, _ := shadowWireHarnessAt(t, store.StrategyConfig{}, pnow)
	mentorStaleBlockFeed(t, pnow, 30*time.Second)
	staleStepReceipt(t, time.Second)
	pat.config.StrategyConfig.RiskControl.MentorMode = true
	b := gateBlocks(pat.id, integrityClassDataTruth)
	pat.mentorOnlyPlacementPass(pnow, nil)
	if got := gateBlocks(pat.id, integrityClassDataTruth); got != b+1 {
		t.Fatalf("the mentor armed pass must refuse on a failing data-truth check and count it: %d -> %d", b, got)
	}
	select {
	case s := <-signals:
		t.Fatalf("the pass sent %+v through a failing data-truth check", s)
	case <-time.After(200 * time.Millisecond):
	}
}

// The SIM test seams are entries too.
func TestDataTruthRefusesTheSIMTestSeams(t *testing.T) {
	t.Setenv("ARMED_TEST_SEAM", "on")
	f := armDataTruthGate(t, checks.CheckContractRoll)
	f.blocked.Store(true)
	f.why = "two contracts in one decision window"
	now := time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC)
	at, _, signals, _ := shadowWireHarnessAt(t, store.StrategyConfig{}, now)
	if _, err := at.TestArmPlace("long", 29600, 29590, 29630); err == nil || !strings.Contains(err.Error(), "data_truth") {
		t.Fatalf("TestArmPlace must be refused by data truth: %v", err)
	}
	if _, err := at.TestArmPlaceStop("long", 29600, 29590, 29630); err == nil || !strings.Contains(err.Error(), "data_truth") {
		t.Fatalf("TestArmPlaceStop must be refused by data truth: %v", err)
	}
	if _, err := at.DebugNTTestTrade("long"); err == nil || !strings.Contains(err.Error(), "data_truth") {
		t.Fatalf("the debug test-trade door must be refused by data truth: %v", err)
	}
	select {
	case s := <-signals:
		t.Fatalf("a SIM test order reached the wire through a failing check: %+v", s)
	case <-time.After(200 * time.Millisecond):
	}
}

// EXITS NEVER CONSULT IT: a close through the decision executor and the manual
// door is not refused, and the check is not even evaluated; the monitor beat's
// m445 episode machinery (which also withdraws resting entries) does not see it.
func TestDataTruthIsEntryOnly(t *testing.T) {
	f := armDataTruthGate(t, checks.CheckBadPrint)
	f.blocked.Store(true)
	at, _ := pauseTrader(t)
	at.id = "datatruth-exit-1"
	for _, action := range []string{"close_long", "close_short"} {
		rec := &store.DecisionAction{Action: action, Symbol: "MNQ"}
		func() {
			defer func() { _ = recover() }() // no broker in this fixture: the close may panic past the gate, which is the point
			_ = at.executeDecisionWithRecord(&kernel.Decision{Action: action, Symbol: "MNQ"}, rec)
		}()
		if strings.HasPrefix(rec.Error, "data_truth") || strings.HasPrefix(rec.Error, "data_integrity") {
			t.Fatalf("%s must never be refused for data: %q", action, rec.Error)
		}
		if _, refused := at.AdmitManualEntryBracketAt("MNQ", action, 0, 0, time.Now()); refused {
			t.Fatalf("the manual door must not refuse %s for data", action)
		}
	}
	if n := f.calls.Load(); n != 0 {
		t.Fatalf("an exit must not even evaluate the data-truth checks, evaluated %d times", n)
	}
	// the m445 beat and withdraw decision are NOT driven by a data-truth block
	// (it pauses NEW entries only; resting orders are left alone)
	armIntegrityGate(t, &intFake{blockedFor: func(int) bool { return false }}) // m445 clear: only data truth blocks
	if reason, _ := at.integrityWithdrawReason(time.Now()); reason != "" {
		t.Fatalf("a data-truth block must not withdraw resting entries: %q", reason)
	}
	at.dataIntegrityBeat(time.Now())
	if at.integrity.active {
		t.Fatal("a data-truth block must not start the m445 pause episode (it has its own episode and alert)")
	}
	if got := f.calls.Load(); got != 0 {
		t.Fatalf("the m445 withdraw / beat decisions must not evaluate data-truth checks (evaluated %d)", got)
	}
}

// The swing4h check judges the line only SWING4H entries read: it blocks the
// swing window and nothing else; every other check blocks every window.
func TestDataTruthSwing4hScopesToSwingEntries(t *testing.T) {
	f := armDataTruthGate(t, checks.CheckLookbackSwing4h)
	f.blocked.Store(true)
	f.why = "swing 4h EMA34 line disagrees by 6 pt"
	at, _ := pauseTrader(t)
	at.id = "datatruth-swing-1"
	now := time.Now()
	refuse := func(in admitIntent) bool {
		in.Symbol, in.Action, in.Now, in.Key = "MNQ", "open_long", now, "k"
		reason, refused := at.admitEntry(in)
		return refused && strings.HasPrefix(reason, "data_truth: ")
	}
	if refuse(admitIntent{Path: admitArm, MentorArm: true}) {
		t.Fatal("a swing4h-only failure must NOT block an intraday mentor arm")
	}
	if refuse(admitIntent{Path: admitArm}) {
		t.Fatal("a swing4h-only failure must NOT block a planner arm")
	}
	if !refuse(admitIntent{Path: admitArm, MentorArm: true, Swing: true}) {
		t.Fatal("a swing4h failure MUST block the SWING4H mentor arm")
	}
	// any other check blocks the intraday arm
	loss := &truthFake{why: "lost final 10-29 08:34"}
	loss.blocked.Store(true)
	datatruth.Register(checks.CheckIngestLoss, loss.fn)
	if !refuse(admitIntent{Path: admitArm, MentorArm: true}) {
		t.Fatal("ingest_loss must block the intraday mentor arm")
	}
}

// FAIL CLOSED. A required check that never registered is a stub that blocks -
// but only once the boot grace has passed (during it the verdict is a "starting"
// block and NOTHING is evaluated, pushed or counted as an episode).
func TestDataTruthUnwiredIsBlockedAndOnlyAlertsAfterTheBootGrace(t *testing.T) {
	armDataTruthGate(t, checks.CheckBadPrint)
	datatruth.Require(checks.CheckIngestLoss) // required, never registered -> stub
	dataTruthReadyFn = func() bool { return false }
	at, _ := pauseTrader(t)
	at.id = "datatruth-boot-1"
	var pushed []string
	datatruth.SetNotifier(func(kind, text string) { pushed = append(pushed, kind) })

	wiredAt := time.UnixMilli(dataTruthWiredAt.Load())
	v := at.dataIntegrityVerdictAt(ntTrader.IntegrityDeepOpenMin, wiredAt.Add(time.Minute), false)
	if !v.Blocked || v.Class != integrityClassDataTruth || !strings.Contains(v.Reason, "still starting") {
		t.Fatalf("inside the boot grace the verdict is a fail-closed 'starting': %+v", v)
	}
	if len(pushed) != 0 || datatruth.Snapshot()[checks.CheckIngestLoss].Episodes != 0 {
		t.Fatalf("nothing is alerted or episoded during the boot grace: pushed=%v", pushed)
	}
	v = at.dataIntegrityVerdictAt(ntTrader.IntegrityDeepOpenMin, wiredAt.Add(dataTruthBootGrace+time.Minute), false)
	if !v.Blocked || !strings.Contains(v.Reason, "ingest_loss") || !strings.Contains(v.Reason, "not wired") {
		t.Fatalf("past the grace the unwired required check blocks and is NAMED: %+v", v)
	}
	if len(pushed) != 1 || !strings.HasPrefix(pushed[0], "data_truth_ingest_loss|MNQ|") {
		t.Fatalf("past the grace the stub raises ONE episode alert: %v", pushed)
	}
}

// The monitor beat keeps the datatruth episode (and so its owner alert and the
// /api/health block) current with NO entry attempted.
func TestDataTruthBeatRaisesTheEpisodeWithoutAnEntry(t *testing.T) {
	f := armDataTruthGate(t, checks.CheckBadPrint)
	f.blocked.Store(true)
	var pushed []string
	datatruth.SetNotifier(func(kind, text string) { pushed = append(pushed, kind+"|"+text) })
	at, _ := pauseTrader(t)
	at.id = "datatruth-beat-1"
	now := time.Now()
	at.dataTruthBeat(now)
	at.dataTruthBeat(now.Add(time.Minute))
	if len(pushed) != 1 || !strings.Contains(pushed[0], "bad_print") {
		t.Fatalf("the beat must raise the episode once, with no entry attempted: %v", pushed)
	}
	if !datatruth.Snapshot()[checks.CheckBadPrint].Blocked {
		t.Fatal("the episode must show blocked in the health snapshot")
	}
	f.blocked.Store(false)
	at.dataTruthBeat(now.Add(2 * time.Minute))
	if datatruth.Snapshot()[checks.CheckBadPrint].Blocked {
		t.Fatal("a measured clear must close the episode")
	}
}

// BOOT WIRING (the trader half): the required set is declared (stubs that block),
// the two account-side m447 checks are registered, and the alert goes to the
// owner-push path.
func TestWireDataTruthBootDeclaresTheRequiredSetAndRegistersTheAccountChecks(t *testing.T) {
	datatruth.Reset()
	prevWired := dataTruthWired.Load()
	t.Cleanup(func() { datatruth.Reset(); dataTruthWired.Store(prevWired) })
	var mu sync.Mutex
	var sent []string
	pusher := ownerpush.NewE(func(text string) error { mu.Lock(); sent = append(sent, text); mu.Unlock(); return nil })
	t.Cleanup(ownerpush.Use(pusher))
	doWireDataTruthBoot()
	if !dataTruthWired.Load() {
		t.Fatal("the boot wiring must switch the gate on")
	}
	snap := datatruth.Snapshot()
	for _, n := range dataTruthRequired() {
		if _, ok := snap[n]; !ok {
			t.Fatalf("required check %q is not declared after the boot wiring", n)
		}
	}
	// the eight m446 checks are stubs until ntTrader.WireDataTruthChecks lands them: they block
	for _, n := range []string{checks.CheckBadPrint, checks.CheckCrossFrame, checks.CheckLookback, checks.CheckLookbackSwing4h,
		checks.CheckLookbackLevels, checks.CheckLookbackSession, checks.CheckIngestLoss, checks.CheckContractRoll} {
		blocked, reasons := datatruth.Evaluate("MNQ", time.Now())
		if !blocked {
			t.Fatalf("with %s unwired the symbol must be blocked (fail closed)", n)
		}
		found := false
		for _, r := range reasons {
			if r.Check == n && strings.Contains(r.Detail, "not wired") {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s must block as 'not wired': %+v", n, reasons)
		}
	}
	// the alert reaches the owner-push path (datatruth.SetNotifier -> ownerpush.Push)
	pusher.Flush()
	mu.Lock()
	n := len(sent)
	first := ""
	if n > 0 {
		first = sent[0]
	}
	mu.Unlock()
	if n == 0 || !strings.Contains(first, "DATA TRUTH: new entries PAUSED for MNQ") {
		t.Fatalf("the episode alert must reach ownerpush.Push, sent=%d %q", n, first)
	}
	if len(dataTruthRequired()) != 10 {
		t.Fatalf("10 checks are required (8 m446 + broker_agreement + fill_price), got %d", len(dataTruthRequired()))
	}
}
