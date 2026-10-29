package trader

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
)

// ── DATA-INTEGRITY GATE (m445), TRADER SIDE — PRODUCTION CALL SITES ─────────
//
// Owner, 2026-10-29: "this is a financial product, not a game — nothing
// related to data loss can continue to trade." Every path that can open risk
// is driven here through the call site production uses, with the hole
// tracker's verdict fed through integrityVerdictFn (the tracker itself, the
// real calendar and the real 10-26 data are pinned in
// trader/ninjatrader/bar_integrity_test.go).
//
// PATH → PRODUCTION CALL SITE PINNED HERE
//   AI decision            executeDecisionWithRecord        TestIntegrityGateRefusesAIDecision
//   agent-chat entry       AdmitManualEntryBracketAt        TestIntegrityGateRefusesAgentChatEntry
//   planner arm (limit)    runArmedPlacementAt              TestIntegrityGateKeepsAPlannerArmOffTheWire
//   mentor arm (row)       admitEntry(arm+MentorArm)        TestIntegrityGateWindowPerPath
//   mentor SWING4H row     admitEntry(arm+MentorArm+Swing)  TestIntegrityGateWindowPerPath
//   Picture HTF            admitEntry(picture)              TestIntegrityGateWindowPerPath
//   mentor authoring       mentorPlaceIntent                TestIntegrityGateRefusesMentorAuthoring
//   mentor armed pass      mentorOnlyPlacementPass          TestIntegrityGateRefusesTheMentorArmedPass
//   SIM test seams         TestArmPlace / TestArmPlaceStop  TestIntegrityGateRefusesTheSIMTestSeams
//   resting entries        monitorTick -> withdraw          TestIntegrityWithdrawsRestingEntries...
//
// MUTANTS (each shown RED in the wave report):
//   T1  drop the dataIntegrityAdmitRefusal call in admitChain  -> AI / agent / arm / window tests RED
//   T2  drop the dataIntegrityVerdictAt call in mentorPlaceIntent -> authoring RED
//   T3  drop it in mentorOnlyPlacementPass                     -> armed-pass RED
//   T4  integrityWindowFor always returns the deep window      -> TestIntegrityGateWindowPerPath RED
//   T5  withdrawEntriesIfDue ignores integrityWithdrawReason   -> withdraw RED
//   T6  dataIntegrityBeat never pushes                         -> episode RED
//   T7  the SIM seams skip dataIntegrityTestSeamRefusal        -> seams RED

// intFake feeds the tracker's verdict and records which window each ask used.
type intFake struct {
	blockedFor     func(openMin int) bool
	class          string
	asked          []int
	askedKeyLevels []bool
	clearsAtMs     int64 // when set, the fake hole carries this clear-by-itself instant
}

func holeVerdictFor(class string) ntTrader.IntegrityVerdict {
	if class == ntTrader.IntegrityClassUnknown {
		return ntTrader.IntegrityVerdict{Armed: true, Blocked: true, Class: class,
			Unknown: "the boot hole scan has not judged MNQ yet this process"}
	}
	return ntTrader.IntegrityVerdict{Armed: true, Blocked: true, Class: ntTrader.IntegrityClassHole,
		Holes: []ntTrader.IntegrityHole{{Symbol: "MNQ", Contract: "MNQ 12-26", State: "refilling", Missing: 297,
			Window: "10-26 17:00–21:57 CT"}}}
}

// armIntegrityGate arms the gate (the NT8 bridge is "wired") and installs the
// fake verdict. blockedFor nil = always blocked.
// seedMeasured marks a mentor trader's seeded series as MEASURED (complete), the
// state production reaches after mentorSeedAtStart. #55 m449: an unmeasured seed
// is BLOCKED, so a test that fakes the tracker and wants only the tracker's leg
// under test must say the seed leg is clear. The unmeasured case itself is
// pinned in data_integrity_seed_unmeasured_test.go.
func seedMeasured(at *AutoTrader) {
	at.seedSeries.mu.Lock()
	at.seedSeries.judged = true
	at.seedSeries.mu.Unlock()
}

func armIntegrityGate(t *testing.T, f *intFake) {
	t.Helper()
	if f.class == "" {
		f.class = ntTrader.IntegrityClassHole
	}
	t.Cleanup(ntTrader.SetIntegrityArmedForTest(true))
	prev := integrityVerdictFn
	integrityVerdictFn = func(symbol string, openMin int, now time.Time, keyLevels bool) ntTrader.IntegrityVerdict {
		f.asked = append(f.asked, openMin)
		f.askedKeyLevels = append(f.askedKeyLevels, keyLevels)
		if f.blockedFor != nil && !f.blockedFor(openMin) {
			return ntTrader.IntegrityVerdict{Armed: true}
		}
		v := holeVerdictFor(f.class)
		if f.clearsAtMs > 0 && len(v.Holes) > 0 {
			v.Holes[0].ClearsAtMs = f.clearsAtMs
		}
		return v
	}
	t.Cleanup(func() { integrityVerdictFn = prev })
}

func TestIntegrityWindowsEqualTheCodeTheyDescribe(t *testing.T) {
	if ntTrader.IntegrityIntradayOpenMin != mentorBars1mDepth {
		t.Fatalf("the intraday window (%d) must equal the 1m depth the mentor reads (mentorBars1mDepth = %d)", ntTrader.IntegrityIntradayOpenMin, mentorBars1mDepth)
	}
	// round 4 (D2): the non-mentor window is the WIDEST 1m reader among those
	// paths — the planner's candle tape — not a 4h-candle count (their 4h is a
	// NATIVE frame; a 1m hole cannot corrupt it).
	if ntTrader.IntegrityDeepOpenMin != plannerCandleTapeBars {
		t.Fatalf("the deep window must equal the planner's candle tape (plannerCandleTapeBars = %d), the widest 1m reader of the non-mentor paths; got %d", plannerCandleTapeBars, ntTrader.IntegrityDeepOpenMin)
	}
	if ntTrader.IntegrityDeepOpenMin < structureMapBarsAsk {
		t.Fatalf("the deep window must contain the structure map's 1m rebuild depth (structureMapBarsAsk = %d)", structureMapBarsAsk)
	}
	// SWING4H's EMA is gated on, and computed over, the evaluator's warm-up
	// history: the window must be the mentor's own constant, not a copy of it.
	if want := (mentor.FourHEMA34Warmup + 1) * 240; ntTrader.IntegritySwingOpenMin != want {
		t.Fatalf("the swing window must be (mentor.FourHEMA34Warmup=%d + 1) x 240 = %d open minutes, got %d", mentor.FourHEMA34Warmup, want, ntTrader.IntegritySwingOpenMin)
	}
	if ntTrader.IntegritySwingOpenMin < ntTrader.IntegrityDeepOpenMin {
		t.Fatal("the swing window must contain the deep one")
	}
	if ntTrader.IntegrityDeepOpenMin < kernel.AISVPBarCount {
		t.Fatalf("the deep window must contain the AI path's 1m depth (%d)", kernel.AISVPBarCount)
	}
	if kernel.HTFVetoNativeTF != "4h" {
		t.Fatalf("the deep window's 4h reasoning assumes the HTF veto reads %q", "4h")
	}
}

// ── the single admission chain: AI decision, agent door ────────────────────

func TestIntegrityGateRefusesAIDecision(t *testing.T) {
	f := &intFake{}
	armIntegrityGate(t, f)
	at, _ := pauseTrader(t)
	at.id = "integrity-ai-1"
	for _, action := range []string{"open_long", "open_short"} {
		before := gateBlocks(at.id, ntTrader.IntegrityClassHole)
		rec, err := &store.DecisionAction{Action: action, Symbol: "MNQ"}, error(nil)
		func() {
			// with the gate removed the decision runs on into a fixture that has
			// no broker and panics: report that as the pin failing, not as a
			// crashed package (review round 2, reviewer A)
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s was NOT refused at the gate: it ran on past admitChain (panic %v)", action, r)
				}
			}()
			err = at.executeDecisionWithRecord(&kernel.Decision{Action: action, Symbol: "MNQ"}, rec)
		}()
		if err != nil || rec.Success || !strings.HasPrefix(rec.Error, "data_integrity: ") ||
			!strings.Contains(rec.Error, "10-26 17:00–21:57 CT") || !strings.Contains(rec.Error, ntTrader.IntegrityAction) {
			t.Fatalf("%s must be refused by name at executeDecisionWithRecord (err=%v rec=%q)", action, err, rec.Error)
		}
		if got := gateBlocks(at.id, ntTrader.IntegrityClassHole); got != before+1 {
			t.Fatalf("%s: the refusal must be counted under %s once: %d -> %d", action, ntTrader.IntegrityClassHole, before, got)
		}
	}
	if len(f.asked) == 0 || f.asked[0] != ntTrader.IntegrityDeepOpenMin {
		t.Fatalf("the AI path reads the 4h structure: it must ask the DEEP window, asked %v", f.asked)
	}
	// A CLOSE is never admitted by this chain: it must NOT be refused for data.
	rec := &store.DecisionAction{Action: "close_long", Symbol: "MNQ"}
	func() {
		defer func() { _ = recover() }() // no broker in this fixture: the close may panic past the gate, which is the point
		_ = at.executeDecisionWithRecord(&kernel.Decision{Action: "close_long", Symbol: "MNQ"}, rec)
	}()
	if strings.HasPrefix(rec.Error, "data_integrity") {
		t.Fatalf("a CLOSE must never be refused for missing data: %q", rec.Error)
	}
}

func TestIntegrityGateRefusesAgentChatEntry(t *testing.T) {
	f := &intFake{}
	armIntegrityGate(t, f)
	at, _ := pauseTrader(t)
	at.id = "integrity-agent-1"
	reason, refused := at.AdmitManualEntryBracketAt("MNQ", "open_long", 29590, 29630, time.Now())
	if !refused || !strings.HasPrefix(reason, "data_integrity: ") {
		t.Fatalf("an agent-chat entry must be refused for missing data: refused=%v %q", refused, reason)
	}
	_, err := at.OpenManualEntryAt("MNQ", "open_short", 1, 1, 29630, 29590, time.Now())
	var mr *ManualEntryRefusal
	if err == nil || !errors.As(err, &mr) || !strings.HasPrefix(mr.Reason, "data_integrity: ") {
		t.Fatalf("OpenManualEntry (the door) must refuse with a ManualEntryRefusal naming the data: %v", err)
	}
}

// ── the window each path reads ─────────────────────────────────────────────

func TestIntegrityGateWindowPerPath(t *testing.T) {
	cases := []struct {
		name string
		in   admitIntent
		want int
	}{
		{"mentor intraday arm", admitIntent{Path: admitArm, MentorArm: true}, ntTrader.IntegrityIntradayOpenMin},
		{"mentor SWING4H arm", admitIntent{Path: admitArm, MentorArm: true, Swing: true}, ntTrader.IntegritySwingOpenMin},
		{"planner arm", admitIntent{Path: admitArm}, ntTrader.IntegrityDeepOpenMin},
		{"AI decision", admitIntent{Path: admitDecision}, ntTrader.IntegrityDeepOpenMin},
		{"agent chat", admitIntent{Path: admitAgent}, ntTrader.IntegrityDeepOpenMin},
		{"Picture HTF", admitIntent{Path: admitPicture}, ntTrader.IntegrityDeepOpenMin},
	}
	for _, c := range cases {
		if got := integrityWindowFor(c.in); got != c.want {
			t.Fatalf("%s: window = %d open minutes, want %d", c.name, got, c.want)
		}
	}

	// And at the production chain: a hole inside the DEEP window only blocks
	// the paths that read it (the swing window contains the deep one); an
	// intraday mentor arm still places.
	f := &intFake{blockedFor: func(openMin int) bool { return openMin >= ntTrader.IntegrityDeepOpenMin }}
	armIntegrityGate(t, f)
	at, _ := pauseTrader(t)
	at.id = "integrity-window-1"
	now := time.Now()
	refuse := func(in admitIntent) string {
		in.Symbol, in.Action, in.Now, in.Key = "MNQ", "open_long", now, "k"
		// the data gate is the third step of the chain on this fixture: any
		// refusal that is NOT the data one means the data gate let it pass
		reason, refused := at.admitEntry(in)
		if refused && strings.HasPrefix(reason, "data_integrity: ") {
			return reason
		}
		return ""
	}
	if r := refuse(admitIntent{Path: admitArm, MentorArm: true}); r != "" {
		t.Fatalf("a hole older than the intraday window must NOT block an intraday mentor arm: %q", r)
	}
	if r := refuse(admitIntent{Path: admitArm, MentorArm: true, Swing: true}); r == "" {
		t.Fatal("the same hole IS inside the SWING4H window and must block the swing arm")
	}
	if r := refuse(admitIntent{Path: admitArm}); r == "" {
		t.Fatal("a planner arm reads the deep window and must be blocked")
	}
	// Picture needs the running + Day Plan checks that precede the data gate
	at.isRunning = true
	at.config.StrategyConfig = &store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	if r := refuse(admitIntent{Path: admitPicture}); r == "" {
		t.Fatal("a Picture HTF entry must be blocked by a hole in the deep window")
	}
}

// ── planner arm at the real wire: the row stays armed, nothing is sent ──────

func TestIntegrityGateKeepsAPlannerArmOffTheWire(t *testing.T) {
	real := time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC) // Mon 10:00 CT, CME open
	t.Cleanup(kernel.ClockSampleForTest(kernel.NewSteppedClock(real).Sample))
	at, st, signals, _ := shadowWireHarnessAt(t, store.StrategyConfig{}, real)
	mentorStaleBlockFeed(t, real, 30*time.Second) // a FRESH feed: the stale leg is not what refuses
	staleStepReceipt(t, time.Second)
	row, admitted := armStaleRow(t, at, st)
	f := &intFake{}
	armIntegrityGate(t, f)
	before := gateBlocks(at.id, ntTrader.IntegrityClassHole)

	for i := 0; i < 3; i++ { // three passes: one episode of refusals
		at.runArmedPlacementAt([]market.Kline{{Close: 29600}}, real.Add(-time.Hour).UnixMilli(), real.Add(time.Duration(i)*time.Second), admitted)
	}
	if got := armStaleRowState(t, st, row); got.State != store.StateArmed || got.SignalID != "" {
		t.Fatalf("a hole in the data window must keep the planner arm OFF the wire; row = %s signal %q", got.State, got.SignalID)
	}
	select {
	case s := <-signals:
		t.Fatalf("an arm reached the wire through a data hole: %+v", s)
	case <-time.After(300 * time.Millisecond):
	}
	if got := gateBlocks(at.id, ntTrader.IntegrityClassHole); got != before+1 {
		t.Fatalf("the refusal is counted ONCE per episode as %s: %d -> %d", ntTrader.IntegrityClassHole, before, got)
	}

	// Positive control: the hole is measured filled -> the SAME row places.
	f.blockedFor = func(int) bool { return false }
	at.runArmedPlacementAt([]market.Kline{{Close: 29600}}, real.Add(-time.Hour).UnixMilli(), real.Add(5*time.Second), admitted)
	if got := armStaleRowState(t, st, row); got.SignalID == "" {
		t.Fatalf("with the data complete the arm must place; row = %s", got.State)
	}
}

// The STOP-ENTRY send site (placeOneStopEntry — the route every mentor row
// takes) is a separate wire call from the limit one; it asks armAdmitted too.
// The fake AddOn proves its build first (a stop entry is refused at the wire
// until the far side is proven), so the positive control is real.
func TestIntegrityGateKeepsAStopEntryArmOffTheWire(t *testing.T) {
	t.Setenv("STOP_ENTRY_SEAM", "on")
	f := withdrawFixture(t, "int-stop-entry", nil)
	build := addonBuildIDFromSource()
	if build == "" {
		t.Fatal("fixture: the AddOn build id could not be read from the C# source")
	}
	for _, w := range []struct {
		ft ntwire.FrameType
		p  any
	}{
		{ntwire.FrameHello, ntwire.HelloPayload{ProtocolVersion: ntwire.ProtocolVersion, Source: "vltrader-addon", BuildID: build}},
		{ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: build}},
	} {
		if err := ntwire.WriteFrame(f.w.conn, w.ft, w.p); err != nil {
			t.Fatal(err)
		}
	}
	for deadline := time.Now().Add(2 * time.Second); !ntwire.FarSideProven(f.w.srv.FarSideBuildID(), build) && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if !ntwire.FarSideProven(f.w.srv.FarSideBuildID(), build) {
		t.Fatal("fixture: far-side build never proven")
	}
	row := store.ArmedOrderDB{TraderID: f.at.id, PlanID: "stop-arm", Version: 1, Scenario: "S1", State: store.StateArmed,
		Side: "long", Kind: "stop_entry", Condition: "reclaim", EntryPx: 101, StopPx: 95, TargetPx: 110}
	if err := f.st.ArmedOrders().UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	admitted := armAdmission{armAdmitKey(row.PlanID, "S1", 0): true}
	bars := []market.Kline{{Close: 100}}
	fk := &intFake{}
	armIntegrityGate(t, fk)

	f.at.runArmedPlacementAt(bars, f.now.Add(-time.Hour).UnixMilli(), f.now, admitted)
	f.w.flush(t)
	if got := f.rowByID(t, row.ID); got.State != store.StateArmed || got.SignalID != "" {
		t.Fatalf("a data hole must keep the STOP-ENTRY arm off the wire; row = %s signal %q", got.State, got.SignalID)
	}
	for _, fr := range f.w.all() {
		if fr.Type == ntwire.FrameSignal {
			t.Fatalf("a stop-entry reached the wire through a data hole: %+v", fr)
		}
	}
	// control: the data is complete -> the SAME row reaches the wire
	fk.blockedFor = func(int) bool { return false }
	f.at.runArmedPlacementAt(bars, f.now.Add(-time.Hour).UnixMilli(), f.now.Add(5*time.Second), admitted)
	f.w.waitFor(t, "the stop-entry signal once the data is complete", func(fr wdFrame) bool { return fr.Type == ntwire.FrameSignal })
	if got := f.rowByID(t, row.ID); got.SignalID == "" {
		t.Fatalf("with the data complete the stop-entry must place (control); row = %s", got.State)
	}
}

// The market_in_zone send site (placeZoneRow — the third wire call) asks
// armAdmitted too: a data hole keeps the zone row armed, one limit once the
// data is complete.
func TestIntegrityGateKeepsAZoneRowOffTheWire(t *testing.T) {
	r := newZoneRig(t, "m445-zone", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.setTape(zoneTape(100.0, r.now, 0))
	fk := &intFake{}
	armIntegrityGate(t, fk)
	before := gateBlocks(r.at.id, ntTrader.IntegrityClassHole)
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("a data hole: nothing may reach the wire, got %d frame(s)", len(sigs))
	}
	if row := r.row("S1"); row.State != store.StateArmed || !strings.HasPrefix(row.LastVerdict, "refused: data_integrity") {
		t.Fatalf("the hole must not advance the zone row and its last verdict must name the data: %+v", row)
	}
	if gateBlocks(r.at.id, ntTrader.IntegrityClassHole) <= before {
		t.Fatal("the zone refusal must count the data class")
	}
	fk.blockedFor = func(int) bool { return false }
	r.at.maybeManageArmedOrdersAt(nil, r.now.Add(time.Second))
	if sigs, _ := r.drain(); len(sigs) != 1 {
		t.Fatalf("with the data complete the zone row must place once (control), got %d", len(sigs))
	}
}

// ── the mentor's two seams ─────────────────────────────────────────────────

func TestIntegrityGateRefusesMentorAuthoring(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.exchange = "ninjatrader"
	seedMeasured(at)
	now := mentorStaleBlockClock(9, 0)
	setMentorNowSource(func() time.Time { return now })
	t.Cleanup(func() { setMentorNowSource(nil) })
	wireMentorPlacementSeams(t, at)
	mentorStaleBlockFeed(t, now, 30*time.Second) // fresh
	f := &intFake{}
	armIntegrityGate(t, f)

	var placed []mentor.Intent
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed = append(placed, i) }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	before := gateBlocks(at.id, ntTrader.IntegrityClassHole)
	at.mentorPlaceIntent(mentorStaleBlockIntent(), mentorSizeChoice{Contracts: 1, Tier: "base"}, 1000, 1100)
	if len(placed) != 0 {
		t.Fatalf("a hole in the intraday window must NOT author an arm, %d reached the recorder", len(placed))
	}
	if at.mentorCountSnapshot()["placement_refused_data_integrity"] != 1 || gateBlocks(at.id, ntTrader.IntegrityClassHole) != before+1 {
		t.Fatalf("the refusal must be counted (mentor funnel + gate-block table): %v", at.mentorCountSnapshot())
	}
	if len(f.asked) == 0 || f.asked[0] != ntTrader.IntegrityIntradayOpenMin {
		t.Fatalf("an intraday setup asks the INTRADAY window, asked %v", f.asked)
	}

	// a SWING4H intent asks the swing window
	f.asked = nil
	f.blockedFor = func(openMin int) bool { return openMin == ntTrader.IntegritySwingOpenMin }
	swing := mentorStaleBlockIntent()
	swing.Setup, swing.ArmID = "SWING4H", "swing-1"
	at.mentorPlaceIntent(swing, mentorSizeChoice{Contracts: 1, Tier: "base"}, 1000, 1100)
	if len(placed) != 0 || len(f.asked) == 0 || f.asked[0] != ntTrader.IntegritySwingOpenMin {
		t.Fatalf("SWING4H must be refused by the swing window (placed %d, asked %v)", len(placed), f.asked)
	}
	// ...and an intraday intent on the same deep-only hole PROCEEDS
	at.mentorPlaceIntent(mentorStaleBlockIntent(), mentorSizeChoice{Contracts: 1, Tier: "base"}, 1000, 1100)
	if len(placed) != 1 {
		t.Fatalf("an intraday setup must still author when only the deep window has the hole, placed %d", len(placed))
	}
}

func TestIntegrityGateRefusesTheMentorArmedPass(t *testing.T) {
	now := time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC)
	at, _, signals, _ := shadowWireHarnessAt(t, store.StrategyConfig{}, now)
	mentorStaleBlockFeed(t, now, 30*time.Second)
	staleStepReceipt(t, time.Second)
	at.config.StrategyConfig.RiskControl.MentorMode = true
	f := &intFake{}
	armIntegrityGate(t, f)
	before := gateBlocks(at.id, ntTrader.IntegrityClassHole)
	at.mentorOnlyPlacementPass(now, nil)
	if got := gateBlocks(at.id, ntTrader.IntegrityClassHole); got != before+1 {
		t.Fatalf("the mentor armed pass must refuse for missing data and count it: %d -> %d", before, got)
	}
	if len(f.asked) == 0 || f.asked[0] != ntTrader.IntegrityIntradayOpenMin {
		t.Fatalf("the pass-level ask is the INTRADAY window, asked %v", f.asked)
	}
	select {
	case s := <-signals:
		t.Fatalf("the pass sent %+v through a hole", s)
	case <-time.After(200 * time.Millisecond):
	}
}

// The stale-feed leg is part of the SAME verdict at the mentor seams: it
// answers first, with its own class, exactly as before the data legs existed.
func TestIntegrityVerdictFoldsTheStaleLeg(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.exchange = "ninjatrader"
	now := mentorStaleBlockClock(9, 0)
	mentorStaleBlockFeed(t, now, 3*time.Minute) // stale
	f := &intFake{}
	armIntegrityGate(t, f)
	v := at.dataIntegrityVerdictAt(ntTrader.IntegrityIntradayOpenMin, now, true)
	if !v.Blocked || v.Class != "stale_data" || v.isData() {
		t.Fatalf("a stale feed is the verdict's stale leg: %+v", v)
	}
	if len(f.asked) != 0 {
		t.Fatalf("the stale leg answers before the tracker is asked: %v", f.asked)
	}
	mentorStaleBlockFeed(t, now, 20*time.Second) // fresh -> the data leg speaks
	v = at.dataIntegrityVerdictAt(ntTrader.IntegrityIntradayOpenMin, now, true)
	if !v.Blocked || v.Class != ntTrader.IntegrityClassHole || !v.isData() {
		t.Fatalf("with a fresh feed the hole leg blocks: %+v", v)
	}
	// unarmed -> inert
	t.Cleanup(ntTrader.SetIntegrityArmedForTest(false))
	if v := at.dataIntegrityVerdictAt(ntTrader.IntegrityIntradayOpenMin, now, true); v.Blocked {
		t.Fatalf("without the NT8 bridge the data legs are inert: %+v", v)
	}
}

// The SIM test seams put a real order on the wire outside admitEntry.
func TestIntegrityGateRefusesTheSIMTestSeams(t *testing.T) {
	t.Setenv("ARMED_TEST_SEAM", "on")
	now := time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC)
	at, _, signals, _ := shadowWireHarnessAt(t, store.StrategyConfig{}, now)
	f := &intFake{}
	armIntegrityGate(t, f)
	if _, err := at.TestArmPlace("long", 29600, 29590, 29630); err == nil || !strings.Contains(err.Error(), "data_integrity") {
		t.Fatalf("TestArmPlace must be refused for missing data: %v", err)
	}
	if _, err := at.TestArmPlaceStop("long", 29600, 29590, 29630); err == nil || !strings.Contains(err.Error(), "data_integrity") {
		t.Fatalf("TestArmPlaceStop must be refused for missing data: %v", err)
	}
	select {
	case s := <-signals:
		t.Fatalf("a SIM test order reached the wire through a hole: %+v", s)
	case <-time.After(200 * time.Millisecond):
	}
}

// ── resting entries are withdrawn, with a named reason; exits are untouched ──

func TestIntegrityWithdrawsRestingEntriesWithANamedReason(t *testing.T) {
	f := withdrawFixture(t, "wd-integrity", nil)
	row, sid := f.placeResting(t)
	t1 := f.now.Add(time.Minute)
	fk := &intFake{}
	armIntegrityGate(t, fk)
	f.setLiveBook(t1, wdEntryOrder(sid))
	want := WithdrawReasonPrefix + "data_integrity: " + integrityHoleReason("MNQ", holeVerdictFor(ntTrader.IntegrityClassHole))
	assertWithdrawnThenConfirmed(t, f, row, sid, want, t1)
	if !strings.Contains(want, "10-26 17:00–21:57 CT") {
		t.Fatalf("the ledger reason must name the hole window: %q", want)
	}
}

func TestIntegrityWithdrawScopeAndGrace(t *testing.T) {
	now := time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC)

	// non-mentor trader: a deep-window hole withdraws every resting entry
	at, _ := pauseTrader(t)
	fk := &intFake{}
	armIntegrityGate(t, fk)
	if r, swingOnly := at.integrityWithdrawReason(now); r == "" || swingOnly {
		t.Fatalf("non-mentor: withdraw everything: %q swingOnly=%v", r, swingOnly)
	}

	// mentor trader, hole in the DEEP window only -> SWING rows only (an
	// intraday arm on a clean intraday window must not be cancelled every beat)
	mt := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	mt.exchange = "ninjatrader"
	fk.blockedFor = func(openMin int) bool { return openMin == ntTrader.IntegritySwingOpenMin }
	if r, swingOnly := mt.integrityWithdrawReason(now); r == "" || !swingOnly {
		t.Fatalf("mentor, deep-only hole: swing rows only: %q swingOnly=%v", r, swingOnly)
	}
	// hole inside the intraday window -> everything
	fk.blockedFor = nil
	if r, swingOnly := mt.integrityWithdrawReason(now); r == "" || swingOnly {
		t.Fatalf("mentor, intraday hole: withdraw all: %q swingOnly=%v", r, swingOnly)
	}

	// an UNKNOWN detector never withdraws inside the grace (a restart must not
	// churn the ledger), and does once it has stood past it
	fk.class = ntTrader.IntegrityClassUnknown
	if r, _ := at.integrityWithdrawReason(now); r != "" {
		t.Fatalf("unknown inside the grace must not withdraw: %q", r)
	}
	at.integrity.mu.Lock()
	at.integrity.unknownSince = now.Add(-integrityUnknownGrace - time.Second)
	at.integrity.mu.Unlock()
	if r, _ := at.integrityWithdrawReason(now); r == "" {
		t.Fatal("unknown past the grace must withdraw")
	}
}

// ── the episode: one WARN-equivalent + one push, one resume ────────────────

func TestIntegrityEpisodePushesOnceAndAnnouncesTheResume(t *testing.T) {
	rec, flush := installPush(t)
	at, _ := feedTrader(t)
	at.name = "Mentor A"
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 30_000, Final: true}})
	fk := &intFake{}
	armIntegrityGate(t, fk)
	epBefore := telemetryEpisodes(ntTrader.IntegrityClassHole)

	at.monitorTick(now)
	at.monitorTick(now.Add(time.Minute))
	at.monitorTick(now.Add(2 * time.Minute)) // the pause goes on: still ONE message
	flush()
	msgs := rec.all()
	if n := countContaining(msgs, "TRADING PAUSED"); n != 1 {
		t.Fatalf("exactly one pause push per episode, got %d: %q", n, msgs)
	}
	p := msgs[0]
	for _, w := range []string{"Mentor A", "MNQ", "10-26 17:00–21:57 CT", ntTrader.IntegrityAction, "exits, stops and protection are unaffected"} {
		if !strings.Contains(p, w) {
			t.Fatalf("the pause push must carry %q: %q", w, p)
		}
	}
	if got := telemetryEpisodes(ntTrader.IntegrityClassHole); got != epBefore+1 {
		t.Fatalf("one episode counted per reason: %d -> %d", epBefore, got)
	}

	// measured filled -> ONE resume
	fk.blockedFor = func(int) bool { return false }
	at.monitorTick(now.Add(3 * time.Minute))
	at.monitorTick(now.Add(4 * time.Minute))
	flush()
	msgs = rec.all()
	if n := countContaining(msgs, "TRADING RESUMED"); n != 1 {
		t.Fatalf("exactly one resume push, got %d: %q", n, msgs)
	}
	if n := countContaining(msgs, "TRADING PAUSED"); n != 1 {
		t.Fatalf("no second pause after the resume: %q", msgs)
	}

	// a NEW hole is a new episode: pushed again
	fk.blockedFor = nil
	at.monitorTick(now.Add(10 * time.Minute))
	flush()
	if n := countContaining(rec.all(), "TRADING PAUSED"); n != 2 {
		t.Fatalf("a new episode pushes again, got %d", n)
	}
}

// An UNKNOWN detector is told only after it has stood past the grace, and not
// while CME is closed.
func TestIntegrityUnknownIsPushedOnlyAfterTheGrace(t *testing.T) {
	rec, flush := installPush(t)
	at, _ := feedTrader(t)
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 30_000, Final: true}})
	fk := &intFake{class: ntTrader.IntegrityClassUnknown}
	armIntegrityGate(t, fk)
	at.dataIntegrityBeat(now)
	at.dataIntegrityBeat(now.Add(time.Minute))
	flush()
	if len(rec.all()) != 0 {
		t.Fatalf("a fresh unknown (a normal boot) must not push: %q", rec.all())
	}
	at.dataIntegrityBeat(now.Add(integrityUnknownGrace + time.Minute))
	flush()
	if n := countContaining(rec.all(), "TRADING PAUSED"); n != 1 {
		t.Fatalf("an unknown past the grace is told once: %q", rec.all())
	}
}

// A paused trader never blocks protection: a position's stop/BE move path does
// not cross admitChain. The pin is structural — closes/exits are not admitted.
func TestIntegrityGateIsEntryOnly(t *testing.T) {
	f := &intFake{}
	armIntegrityGate(t, f)
	at, _ := pauseTrader(t)
	at.id = "integrity-entryonly"
	// admitEntry is the ONLY caller of the gate; a manual CLOSE door does not call it
	if _, refused := at.AdmitManualEntryBracketAt("MNQ", "close_long", 0, 0, time.Now()); refused {
		t.Fatal("a close action is not an entry and must never be refused by the data gate")
	}
	if len(f.asked) != 0 {
		t.Fatalf("a close must not even ask the verdict: %v", f.asked)
	}
}

func telemetryEpisodes(class string) int64 {
	return telemetry.DataIntegrityCountsSnapshot().Episodes[class]
}

// ── review round 1, D1: the debug test-trade door ───────────────────────────

// POST /api/debug/nt-test-trade places a MARKET entry on the SIM wire and
// "bypasses the AI + risk gate" by design. It reached the broker through
// TCPTrader.DebugPlaceTestTrade with no data check. Its only door is now
// AutoTrader.DebugNTTestTrade (the api handler calls nothing else —
// api/data_integrity_debug_door_test.go): through a hole nothing reaches the
// wire; with the data complete (control) exactly one entry frame does.
func TestIntegrityGateRefusesTheDebugTestTradeDoor(t *testing.T) {
	now := time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC)
	at, _, signals, _ := shadowWireHarnessAt(t, store.StrategyConfig{}, now)
	nt := at.armedTrader()
	if nt == nil {
		t.Fatal("fixture: no TCPTrader bound")
	}
	nt.BarCache().SeedHistorical("MNQ", "5m", []ntwire.Bar{{T: now.Add(-5 * time.Minute).UnixMilli(), O: 29600, H: 29610, L: 29590, C: 29600, V: 10, Final: true}})
	f := &intFake{}
	armIntegrityGate(t, f)
	if _, err := at.DebugNTTestTrade("long"); err == nil || !strings.Contains(err.Error(), "data_integrity") {
		t.Fatalf("the debug test trade must be refused through a hole: %v", err)
	}
	if _, err := at.DebugNTTestTrade("short"); err == nil || !strings.Contains(err.Error(), "data_integrity") {
		t.Fatalf("the debug test trade (short) must be refused through a hole: %v", err)
	}
	select {
	case s := <-signals:
		t.Fatalf("a debug test trade reached the wire through a hole: %+v", s)
	case <-time.After(200 * time.Millisecond):
	}
	// CONTROL: the data complete -> the very same call reaches the wire.
	f.blockedFor = func(int) bool { return false }
	if _, err := at.DebugNTTestTrade("long"); err != nil && strings.Contains(err.Error(), "data_integrity") {
		t.Fatalf("control: with the data complete the door must open: %v", err)
	}
	select {
	case s := <-signals:
		if s.Symbol == "" {
			t.Fatalf("control: an empty frame reached the wire: %+v", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("control: with the data complete the debug test trade must send its entry frame (the pin would otherwise pass on a door that is always shut)")
	}
}

// m445 round 4 (D4) — a resume that is only the stretch LEAVING the window must
// not say the bars came back. The episode ends either on a measured fill (the
// tracker holds nothing for the symbol) or by ageing out (the tracker still lists
// the hole, unfilled): the owner push and the log say which.
func TestIntegrityResumeTextSaysWhetherTheBarsCameBack(t *testing.T) {
	judged := time.Date(2026, 10, 28, 9, 0, 0, 0, kernel.CTLocation())
	ctMs := func(d, h, m int) int64 {
		return time.Date(2026, 10, d, h, m, 0, 0, kernel.CTLocation()).UnixMilli()
	}
	for _, tc := range []struct {
		name    string
		holes   []ntTrader.TestHole
		must    []string
		mustNot []string
	}{
		{"measured filled", []ntTrader.TestHole{{Symbol: "MNQ", Contract: "MNQ 12-26"}},
			[]string{"complete again (measured filled)"}, []string{"NOT refilled", "aged out"}},
		{"aged out, never refilled", []ntTrader.TestHole{{Symbol: "MNQ", Contract: "MNQ 12-26", FromMs: ctMs(14, 3, 0), ToMs: ctMs(14, 3, 30)}},
			[]string{"no missing bars inside the data window any more", "NOT refilled", "aged out", "10-14 03:00–03:30 CT"}, []string{"complete again", "measured filled"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, flush := installPush(t)
			at, _ := feedTrader(t)
			at.name = "Mentor A"
			now := cmeOpenNow(t)
			withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 30_000, Final: true}})
			fk := &intFake{}
			armIntegrityGate(t, fk)
			t.Cleanup(ntTrader.SetBarHolesWithHolesForTest(judged.UnixMilli()-1e9, judged.UnixMilli(), tc.holes))
			at.monitorTick(now)
			fk.blockedFor = func(int) bool { return false }
			at.monitorTick(now.Add(time.Minute))
			flush()
			var resumed string
			for _, m := range rec.all() {
				if strings.Contains(m, "TRADING RESUMED") {
					resumed = m
				}
			}
			if resumed == "" {
				t.Fatalf("no resume push: %q", rec.all())
			}
			for _, w := range tc.must {
				if !strings.Contains(resumed, w) {
					t.Fatalf("the resume push must say %q: %q", w, resumed)
				}
			}
			for _, w := range tc.mustNot {
				if strings.Contains(resumed, w) {
					t.Fatalf("the resume push must NOT say %q: %q", w, resumed)
				}
			}
		})
	}
}
