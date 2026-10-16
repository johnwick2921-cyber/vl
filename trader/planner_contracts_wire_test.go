// dispatch-31 fold (PR #564, CTO 2026-10-16): the PLANNER's REAL entries are
// LIMIT / market_in_zone, not stop — the first cut only wired the split on the
// stop path, so the wire still sent qty 1 / leg1 0 while the ledger said 4 = 2+2.
// These are the PRODUCTION-CALL-SITE pins through the real TCPTrader and a
// loopback AddOn (canon 53: parity tests exercise production call sites, never a
// fake placer). Each drives the authoring pass + placement and asserts the WIRE
// frame AND the ledger row agree — ledger = wire.

package trader

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// plannerSplitZoneScenario is a market_in_zone reject long at 100 whose target
// chain names a first target (target_chain[0] = 105) below the arm's final
// target 110 — the split the authoring stamp must produce (fold-564-b D5: leg 1
// at the plan's FIRST TARGET, not the first obstacle).
func plannerSplitZoneScenario(id string) kernel.PlanScenario {
	sc := zoneScenario(id, kernel.EntryPolicyMarketInZone, zone, false)
	sc.TargetChain = []float64{105, 110}
	fo := 105.0
	sc.Economics.FirstObstacle = &kernel.ScenarioObstacle{Price: &fo, Level: "L1", Response: "reduce"}
	return sc
}

// TestGate564_ZonePlannerArmWiresFourAsTwoPlusTwo — the planner's ACTUAL entry
// path (market_in_zone, zone_placement.go). With planner_contracts=4 and the
// size clamp raised (max_contracts_per_order=4 + STAGE_A_CONTRACT_CAP=4), the
// authoring stamp must reach the wire as qty 4, leg1_qty 2, leg1_tp 105 — and
// the ledger row must record exactly that. Mutants M564-1 (drop the authoring
// stamp), M564-2 (stamp Picture arms) and M564-3 (first obstacle → 0) all flip
// this test RED.
func TestGate564_ZonePlannerArmWiresFourAsTwoPlusTwo(t *testing.T) {
	t.Setenv("STAGE_A_CONTRACT_CAP", "4")
	r := newZoneRig(t, "g564-zone", zoneDoc(plannerSplitZoneScenario("S1")))
	r.at.config.StrategyConfig.RiskControl.PlannerContracts = 4
	r.at.config.StrategyConfig.RiskControl.MaxContractsPerOrder = 4
	// The zone rig proves no far-side build by default; prove the split-legs
	// build so the LIMIT path sends the two-OCO-pair frame (the live AddOn
	// 2026-10-14-a proves it).
	if err := ntwire.WriteFrame(r.conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: ntwire.MinAddonBuildSplitLegs}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for r.srv.FarSideBuildID() == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	sig := r.placeWorking(100.5)
	if sig.Quantity != 4 || sig.Leg1Qty != 2 || sig.Leg1TP != 105 {
		t.Fatalf("zone planner arm wired qty=%d leg1=%d@%.2f, want 4/2/105 (4 = 2 + 2 at the first obstacle)",
			sig.Quantity, sig.Leg1Qty, sig.Leg1TP)
	}
	row := r.row("S1")
	if row.Contracts == nil || *row.Contracts != 4 || row.Leg1Qty == nil || *row.Leg1Qty != 2 || row.Leg1TP != 105 {
		t.Fatalf("ledger must equal the wire (4 = 2 + 2 at 105): %+v", row)
	}
}

// TestGate564_ZonePlannerArmClampedToResolveMaxContracts — the SAME rig with the
// always-on clamp left at its defaults (max_contracts_per_order unset, Stage-A
// cap 1) must send the single 1-contract arm even though planner_contracts=4:
// the knob never bypasses resolveMaxContracts.
func TestGate564_ZonePlannerArmClampedToResolveMaxContracts(t *testing.T) {
	r := newZoneRig(t, "g564-clamp", zoneDoc(plannerSplitZoneScenario("S1")))
	r.at.config.StrategyConfig.RiskControl.PlannerContracts = 4
	// max_contracts_per_order unset → the venue default 2, Stage-A cap 1 → 1.
	sig := r.placeWorking(100.5)
	if sig.Quantity != 1 || sig.Leg1Qty != 0 || sig.Leg1TP != 0 {
		t.Fatalf("the knob must not bypass the always-on clamp: wired qty=%d leg1=%d@%.2f, want 1/0/0",
			sig.Quantity, sig.Leg1Qty, sig.Leg1TP)
	}
	row := r.row("S1")
	if row.Contracts != nil || row.Leg1Qty != nil || row.Leg1TP != 0 {
		t.Fatalf("a clamp to 1 must leave the row unstamped (today's single-contract arm): %+v", row)
	}
}

// plannerWireHarness is a minimal loopback-AddOn rig for the band-limit and
// stop-entry placement paths (runArmedPlacementAt over the real TCPTrader).
// proveSplit=false leaves the far-side build unproven, so the limit path's
// build-floor gate drops the split (the D2/D3 fixtures).
func plannerWireHarness(t *testing.T, cfg store.StrategyConfig, proveSplit bool) (*AutoTrader, *store.Store, chan ntwire.SignalPayload, func()) {
	t.Helper()
	at, st := resetTrader(t, cfg)
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	waitAddonRegistered(t, s)
	frames := make(chan ntwire.SignalPayload, 8)
	done := make(chan struct{})
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			if env.Type != ntwire.FrameSignal {
				continue
			}
			var p ntwire.SignalPayload
			if json.Unmarshal(env.Payload, &p) == nil {
				select {
				case frames <- p:
				case <-done:
					return
				}
			}
		}
	}()
	if proveSplit {
		if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: ntwire.MinAddonBuildSplitLegs}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		for s.FarSideBuildID() == "" && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
	}
	broker := nttrader.NewTCPTrader(s, "MNQ", "Sim101")
	s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
	broker.StartCloseSync(at.id, "fixture", "ninjatrader", st)
	at.trader = broker
	at.config.NinjaTraderSymbol = "MNQ"
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, rthInstant())
	cleanup := func() {
		close(done)
		_ = conn.Close()
		cancel()
		_ = s.Stop()
	}
	t.Cleanup(cleanup)
	return at, st, frames, cleanup
}

// TestGate564_BandLimitPlannerArmWiresFourAsTwoPlusTwo — the band LIMIT path
// (armed_executor.go, non-zone policy). A stamped planner row sends qty 4,
// leg 1 = 2 at the first obstacle through the real TCPTrader.
func TestGate564_BandLimitPlannerArmWiresFourAsTwoPlusTwo(t *testing.T) {
	t.Setenv("ARMED_TEST_SEAM", "on")
	at, st, frames, _ := plannerWireHarness(t, store.StrategyConfig{
		RiskControl: store.RiskControlConfig{PlannerContracts: 4, MaxContractsPerOrder: 4},
	}, true)
	t.Setenv("STAGE_A_CONTRACT_CAP", "4")
	ledger := st.ArmedOrders()
	four, two := 4, 2
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "g564-band", Scenario: "S1", Version: 1, State: "armed",
		Side: "long", Kind: "limit", Condition: "reclaim", EntryPx: 29600, StopPx: 29590, TargetPx: 29670,
		Contracts: &four, Leg1Qty: &two, Leg1TP: 29660,
	}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	at.runArmedPlacementAt([]market.Kline{{Close: 29601}}, rthInstant().Add(-time.Hour).UnixMilli(), rthInstant(),
		armAdmission{armAdmitKey("g564-band", "S1", 0): true})
	select {
	case p := <-frames:
		if p.Quantity != 4 || p.Leg1Qty != 2 || p.Leg1TP != 29660 {
			t.Fatalf("band limit planner arm wired qty=%d leg1=%d@%.2f, want 4/2/29660", p.Quantity, p.Leg1Qty, p.Leg1TP)
		}
	case <-time.After(time.Second):
		t.Fatal("band limit path did not send")
	}
}

// TestGate564_StopPlannerArmWiresFourAsTwoPlusTwo — the STOP path over the real
// TCPTrader (the first cut only tested this through a fakePlacer). The stamped
// row sends qty 4, leg 1 = 2 at the first obstacle on the wire.
func TestGate564_StopPlannerArmWiresFourAsTwoPlusTwo(t *testing.T) {
	t.Setenv("ARMED_TEST_SEAM", "on")
	t.Setenv("STOP_ENTRY_SEAM", "on")
	at, st, frames, _ := plannerWireHarness(t, store.StrategyConfig{
		RiskControl: store.RiskControlConfig{PlannerContracts: 4, MaxContractsPerOrder: 4},
	}, true)
	t.Setenv("STAGE_A_CONTRACT_CAP", "4")
	ledger := st.ArmedOrders()
	four, two := 4, 2
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "g564-stop", Scenario: "S1", Version: 1, State: "armed",
		Side: "long", Kind: "stop_entry", Condition: "reclaim", EntryPx: 29600, StopPx: 29590, TargetPx: 29670,
		Contracts: &four, Leg1Qty: &two, Leg1TP: 29660,
	}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	at.runArmedPlacementAt([]market.Kline{{Close: 29599}}, rthInstant().Add(-time.Hour).UnixMilli(), rthInstant(),
		armAdmission{armAdmitKey("g564-stop", "S1", 0): true})
	select {
	case p := <-frames:
		if p.Quantity != 4 || p.Leg1Qty != 2 || p.Leg1TP != 29660 {
			t.Fatalf("stop planner arm wired qty=%d leg1=%d@%.2f, want 4/2/29660", p.Quantity, p.Leg1Qty, p.Leg1TP)
		}
	case <-time.After(time.Second):
		t.Fatal("stop path did not send")
	}
}

// TestGate564_PictureArmNeverStampedByPlannerKnob — a MACHINE (Picture) arm is
// never sized by planner_contracts, even with the knob at 4 and the clamp
// raised. Mutant M564-2 (authoring call site passes `machine` = false → Picture
// arms get stamped) flips this RED: the Picture entry would wire qty 4 instead
// of the 1-lot limit D22 pins.
func TestGate564_PictureArmNeverStampedByPlannerKnob(t *testing.T) {
	t.Setenv("STAGE_A_CONTRACT_CAP", "4")
	r, epoch := newPicRig(t, "g564-pic", func(cfg *store.StrategyConfig) {
		cfg.RiskControl.PlannerContracts = 4
		cfg.RiskControl.MaxContractsPerOrder = 4
	})
	picPlan(r, picScenario("P1", "opp-pic", r.now, epoch, picDefault))
	picPass(r, 0, 101.95)
	picPass(r, time.Second, 101.95)
	sigs, _ := r.drain()
	if len(sigs) != 1 {
		t.Fatalf("one Picture entry frame, got %d", len(sigs))
	}
	if sigs[0].Quantity != 1 || sigs[0].Leg1Qty != 0 || sigs[0].Leg1TP != 0 {
		t.Fatalf("a Picture arm must never be stamped by planner_contracts: qty=%d leg1=%d@%.2f, want 1/0/0",
			sigs[0].Quantity, sigs[0].Leg1Qty, sigs[0].Leg1TP)
	}
}

// plannerStampedRow is a placeable band-limit planner row the authoring stamp
// would have produced for planner_contracts=4 (first target 29660).
func plannerStampedRow(at *AutoTrader) store.ArmedOrderDB {
	four, two := 4, 2
	return store.ArmedOrderDB{
		TraderID: at.id, PlanID: "g564-reclamp", Scenario: "S1", Version: 1, State: "armed",
		Side: "long", Kind: "limit", Condition: "reclaim", EntryPx: 29600, StopPx: 29590, TargetPx: 29670,
		Contracts: &four, Leg1Qty: &two, Leg1TP: 29660,
	}
}

// TestRegate564_KnobLoweredWhileArmedWiresOne — D1 probe 1: a row stamped 4
// while the knob was 4 must RE-CLAMP to 1 at send once the owner lowers
// planner_contracts to 1 (the stale stamp never bypasses the cap).
func TestRegate564_KnobLoweredWhileArmedWiresOne(t *testing.T) {
	t.Setenv("ARMED_TEST_SEAM", "on")
	t.Setenv("STAGE_A_CONTRACT_CAP", "4")
	at, st, frames, _ := plannerWireHarness(t, store.StrategyConfig{
		RiskControl: store.RiskControlConfig{PlannerContracts: 4, MaxContractsPerOrder: 4},
	}, true)
	ledger := st.ArmedOrders()
	row := plannerStampedRow(at)
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	at.config.StrategyConfig.RiskControl.PlannerContracts = 1 // the owner lowers the knob while the arm waits
	at.runArmedPlacementAt([]market.Kline{{Close: 29601}}, rthInstant().Add(-time.Hour).UnixMilli(), rthInstant(),
		armAdmission{armAdmitKey("g564-reclamp", "S1", 0): true})
	select {
	case p := <-frames:
		if p.Quantity != 1 || p.Leg1Qty != 0 || p.Leg1TP != 0 {
			t.Fatalf("a lowered knob must re-clamp at send: wired qty=%d leg1=%d@%.2f, want 1/0/0",
				p.Quantity, p.Leg1Qty, p.Leg1TP)
		}
	case <-time.After(time.Second):
		t.Fatal("limit path did not send")
	}
}

// TestRegate564_ClampLoweredWhileArmedWiresOne — D1 probe 2: reverting the
// Stage-A cap (STAGE_A_CONTRACT_CAP unset → 1) while a 4-lot row waits must
// re-clamp to 1 at send.
func TestRegate564_ClampLoweredWhileArmedWiresOne(t *testing.T) {
	t.Setenv("ARMED_TEST_SEAM", "on")
	t.Setenv("STAGE_A_CONTRACT_CAP", "4")
	at, st, frames, _ := plannerWireHarness(t, store.StrategyConfig{
		RiskControl: store.RiskControlConfig{PlannerContracts: 4, MaxContractsPerOrder: 4},
	}, true)
	ledger := st.ArmedOrders()
	row := plannerStampedRow(at)
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("STAGE_A_CONTRACT_CAP"); err != nil {
		t.Fatal(err)
	}
	at.runArmedPlacementAt([]market.Kline{{Close: 29601}}, rthInstant().Add(-time.Hour).UnixMilli(), rthInstant(),
		armAdmission{armAdmitKey("g564-reclamp", "S1", 0): true})
	select {
	case p := <-frames:
		if p.Quantity != 1 || p.Leg1Qty != 0 || p.Leg1TP != 0 {
			t.Fatalf("a reverted Stage-A cap must re-clamp at send: wired qty=%d leg1=%d@%.2f, want 1/0/0",
				p.Quantity, p.Leg1Qty, p.Leg1TP)
		}
	case <-time.After(time.Second):
		t.Fatal("limit path did not send")
	}
}

// TestRegate564_LedgerEqualsWireWhenSplitDropped — D2: when the limit sender
// drops the split (no far-side build proof), the wire carries one 4-lot bracket
// and the LEDGER must record that (leg1 cleared), never the authored 2 @ 29660.
func TestRegate564_LedgerEqualsWireWhenSplitDropped(t *testing.T) {
	t.Setenv("ARMED_TEST_SEAM", "on")
	t.Setenv("STAGE_A_CONTRACT_CAP", "4")
	at, st, frames, _ := plannerWireHarness(t, store.StrategyConfig{
		RiskControl: store.RiskControlConfig{PlannerContracts: 4, MaxContractsPerOrder: 4},
	}, false) // no split-legs proof → the sender drops the split
	ledger := st.ArmedOrders()
	row := plannerStampedRow(at)
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	at.runArmedPlacementAt([]market.Kline{{Close: 29601}}, rthInstant().Add(-time.Hour).UnixMilli(), rthInstant(),
		armAdmission{armAdmitKey("g564-reclamp", "S1", 0): true})
	select {
	case p := <-frames:
		if p.Quantity != 4 || p.Leg1Qty != 0 || p.Leg1TP != 0 {
			t.Fatalf("an unproven build must drop the split: wired qty=%d leg1=%d@%.2f, want 4/0/0",
				p.Quantity, p.Leg1Qty, p.Leg1TP)
		}
	case <-time.After(time.Second):
		t.Fatal("limit path did not send")
	}
	var got store.ArmedOrderDB
	if err := ledger.DB().First(&got, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Leg1Qty != nil && *got.Leg1Qty != 0 {
		t.Fatalf("ledger must equal the wire: row leg1_qty = %d, want 0 (the split was dropped)", *got.Leg1Qty)
	}
	if got.Leg1TP != 0 {
		t.Fatalf("ledger must equal the wire: row leg1_tp = %.2f, want 0 (the split was dropped)", got.Leg1TP)
	}
	if got.Contracts == nil || *got.Contracts != 4 {
		t.Fatalf("the total 4 must survive: %+v", got.Contracts)
	}
}

// TestRegate564_EffectiveLineShowsClampedSize — D4: with planner_contracts=4
// and the always-on clamp at 1 (max_contracts_per_order unset, Stage-A 1), the
// Studio effective line must read 1 with a clamp origin, never 4 "saved value".
func TestRegate564_EffectiveLineShowsClampedSize(t *testing.T) {
	raw := `{"strategy_type":"ai_trading","ai_config":{"risk_control":{"planner_contracts":4}},"day_plan":{"plan_enabled":true}}`
	rows := effRowsFor(t, raw, "ninjatrader", "NY")
	row, ok := rows[rcPath+"planner_contracts"]
	if !ok {
		t.Fatal("the planner_contracts effective row is missing")
	}
	if jsonVal(t, row.Effective) != float64(1) {
		t.Fatalf("planner_contracts effective = %v, want 1 (min(knob 4, resolveMaxContracts 1))", row.Effective)
	}
	if !strings.Contains(strings.ToLower(row.Origin), "clamp") && !strings.Contains(strings.ToLower(row.Origin), "stage") {
		t.Fatalf("planner_contracts origin = %q, want the clamp as the binding source", row.Origin)
	}
}
