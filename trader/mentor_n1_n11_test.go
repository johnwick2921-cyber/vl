package trader

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

// ── N1 + N11 (DS-104, PIPELINE-TRACE) ──────────────────────────────────────
// N1: the evaluator's ArmSeq restarts at 0 after a reload/restart, so a bare
// ArmID ("isb-1") collides with a TERMINAL ledger row from the prior
// evaluator/process and UpsertArm silently no-ops it. The scenario is now
// epoch-prefixed, and an authored row id of 0 refuses + logs.
// N11: the scan and the event goroutine both tick the evaluator; mentorEvalOnce
// serializes them with one mutex.

// TestMentorArmIDUniqueAcrossRestarts (N1): a terminal row for an OLD
// process/evaluator must not swallow a NEW arm with the same evaluator ArmID.
// Mutant: drop the epoch prefix (Scenario: armID) → the new arm is refused
// (row id 0) and this turns RED.
func TestMentorArmIDUniqueAcrossRestarts(t *testing.T) {
	mentorLiveMu.Lock()
	mentorLiveArms = map[string]mentorLiveArm{}
	mentorLiveMu.Unlock()
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	ledger := st.ArmedOrders()

	// An OLD terminal row from a prior process (authored with the OLD
	// unprefixed scenario, before N1) that never reached the broker.
	old := store.ArmedOrderDB{
		TraderID: at.id, PlanID: mentorPlanID(at.id), Version: 1, Session: "MENTOR",
		Scenario: "isb-1", LegIndex: 0, Side: "LONG",
		State: store.StateCancelled, EntryPx: 100, StopPx: 99, TargetPx: 102,
		Kind: "stop_entry",
	}
	if err := ledger.DB().Create(&old).Error; err != nil {
		t.Fatal(err)
	}

	// A reload/restart bumps the epoch, then the SAME evaluator ArmID arrives.
	at.mentorArmEpoch = time.Now().UnixNano()
	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-1", Setup: "ISB",
		Side: mentor.SideLong, Price: 200, Stop: 199, Target: 204, StopPts: 1, TargetPts: 4,
		ExpiryMs: time.Now().UnixMilli() + 60_000}
	at.mentorArmIntent(in, mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}, 1000, 1100, "B", 0)

	arm, ok := at.mentorLiveArmFor("isb-1")
	if !ok || arm.RowID == 0 || arm.RowID == old.ID {
		t.Fatalf("the new arm must be authored on its own row after a reset, got %+v ok=%v (old row %d)", arm, ok, old.ID)
	}
	var row store.ArmedOrderDB
	if err := ledger.DB().First(&row, arm.RowID).Error; err != nil {
		t.Fatalf("the new arm row must exist: %v", err)
	}
	if row.State != store.StateArmed || row.Scenario == old.Scenario {
		t.Fatalf("new arm state=%q scenario=%q, want armed on a distinct scenario", row.State, row.Scenario)
	}
}

// TestMentorReloadSeedPathBumpsEpoch (N1 at the production call site): a
// reloaded evaluator restarts ArmSeq at 0, so the new "isb-1" must not collide
// with the prior run's TERMINAL ledger row. Unlike
// TestMentorArmIDUniqueAcrossRestarts (which sets at.mentorArmEpoch directly),
// this drives the epoch through mentorSeedAtStart — the production seed path a
// real reload takes. Mutant (CTO M3): hardcode `at.mentorArmEpoch = 1` in the
// seed path → both runs mint scenario "isb-1-1" → UpsertArm hits the terminal
// row (same version, no signal) and no-ops (row id 0) → this turns RED.
func TestMentorReloadSeedPathBumpsEpoch(t *testing.T) {
	mentorLiveMu.Lock()
	mentorLiveArms = map[string]mentorLiveArm{}
	mentorLiveMu.Unlock()
	cfg := store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}}
	_, st := resetTrader(t, cfg)

	// RUN 1 — a trader is seeded through the production path (mentorSeedAtStart,
	// exactly what construction does), which sets the arm epoch.
	at1 := &AutoTrader{id: "trader-1", store: st, exchange: "ninjatrader"}
	at1.config.StrategyConfig = &cfg
	at1.mentorSeedAtStart()
	epoch1 := at1.mentorArmEpoch
	if epoch1 == 0 {
		t.Fatal("the production seed path must set a non-zero arm epoch")
	}

	// The prior run's TERMINAL row for this ArmID under THIS run's scenario —
	// what a previous process (same epoch) would have left behind.
	scenario1 := fmt.Sprintf("isb-1-%d", epoch1)
	old := store.ArmedOrderDB{
		TraderID: at1.id, PlanID: mentorPlanID(at1.id), Version: 1, Session: "MENTOR",
		Scenario: scenario1, LegIndex: 0, Side: "LONG",
		State: store.StateCancelled, EntryPx: 100, StopPx: 99, TargetPx: 102,
		Kind: "stop_entry",
	}
	if err := st.ArmedOrders().DB().Create(&old).Error; err != nil {
		t.Fatal(err)
	}

	// RUN 2 (reload) — a NEW trader over the SAME store, seeded the same way.
	// The epoch MUST move, or the new isb-1 re-hits scenario1 and dies on the
	// terminal row.
	at2 := &AutoTrader{id: "trader-1", store: st, exchange: "ninjatrader"}
	at2.config.StrategyConfig = &cfg
	time.Sleep(time.Millisecond) // two seeds must land on different wall-clock nanos
	at2.mentorSeedAtStart()
	if at2.mentorArmEpoch == epoch1 {
		t.Fatalf("a reload through mentorSeedAtStart must bump the arm epoch (got %d twice)", epoch1)
	}

	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-1", Setup: "ISB",
		Side: mentor.SideLong, Price: 200, Stop: 199, Target: 204, StopPts: 1, TargetPts: 4,
		ExpiryMs: time.Now().UnixMilli() + 60_000}
	wireMentorPlacementSeams(t, at2)
	at2.mentorArmIntent(in, mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}, 1000, 1100, "B", 0)

	arm, ok := at2.mentorLiveArmFor("isb-1")
	if !ok || arm.RowID == 0 || arm.RowID == old.ID {
		t.Fatalf("the reloaded arm must be authored on its OWN new row, got %+v ok=%v (old row %d)", arm, ok, old.ID)
	}
	var row store.ArmedOrderDB
	if err := st.ArmedOrders().DB().First(&row, arm.RowID).Error; err != nil {
		t.Fatalf("the new arm row must exist: %v", err)
	}
	if row.State != store.StateArmed || row.Scenario == old.Scenario {
		t.Fatalf("reloaded arm state=%q scenario=%q, want armed on a fresh scenario", row.State, row.Scenario)
	}
}

// TestMentorArmRowZeroRefuses (N1 fail-safe): when UpsertArm authors no row
// (a same-scenario terminal-row collision leaves id 0), the arm is REFUSED +
// counted + logged — never registered as a phantom row 0. Mutant: disable the
// row-0 check (`if false && row.ID == 0`) → this goes RED.
func TestMentorArmRowZeroRefuses(t *testing.T) {
	mentorLiveMu.Lock()
	mentorLiveArms = map[string]mentorLiveArm{}
	mentorLiveMu.Unlock()
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	ledger := st.ArmedOrders()
	at.resetMentorCountersForTest()

	// Pin the epoch and seed a TERMINAL no-signal row for THIS construction's
	// scenario, so UpsertArm finds it and no-ops (returns nil, id 0).
	at.mentorArmEpoch = time.Now().UnixNano()
	scenario := fmt.Sprintf("isb-1-%d", at.mentorArmEpoch)
	old := store.ArmedOrderDB{
		TraderID: at.id, PlanID: mentorPlanID(at.id), Version: 1, Session: "MENTOR",
		Scenario: scenario, LegIndex: 0, Side: "LONG",
		State: store.StateCancelled, EntryPx: 100, StopPx: 99, TargetPx: 102,
		Kind: "stop_entry",
	}
	if err := ledger.DB().Create(&old).Error; err != nil {
		t.Fatal(err)
	}

	// NO bump: the new arm reuses the same epoch → same scenario → collision.
	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-1", Setup: "ISB",
		Side: mentor.SideLong, Price: 200, Stop: 199, Target: 204, StopPts: 1, TargetPts: 4,
		ExpiryMs: time.Now().UnixMilli() + 60_000}
	at.mentorArmIntent(in, mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}, 1000, 1100, "B", 0)

	if _, ok := at.mentorLiveArmFor("isb-1"); ok {
		t.Fatal("a row-0 arm must NOT be registered")
	}
	if c := at.mentorCountSnapshot()["placement_refused_arm_id_zero"]; c != 1 {
		t.Fatalf("placement_refused_arm_id_zero = %d, want 1", c)
	}
	var n int64
	if err := ledger.DB().Model(&store.ArmedOrderDB{}).Where("scenario = ? AND state = ?", scenario, store.StateArmed).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a collision must not mint a fresh armed row, got %d", n)
	}
}

// TestMentorEvalOnceSerializedAcrossGoroutines (N11): the scan loop and the
// event loop both tick the evaluator. mentorEvalOnce must serialize them —
// under -race this test flags the concurrent evaluator map write the moment the
// mutex is dropped.
func TestMentorEvalOnceSerializedAcrossGoroutines(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	open := int64(1_760_000_000_000)
	mkBars := func(offset int64) []market.Kline {
		out := make([]market.Kline, 40)
		for i := range out {
			o := open + offset + int64(i)*60_000
			out[i] = market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: 100, High: 101, Low: 99, Close: 100.5, Final: true}
		}
		return out
	}
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline { return mkBars(0) }
	t.Cleanup(func() { market.FuturesBarsProvider = nil })

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				at.mentorEvalOnce(mkBars(int64(i) * 60_000))
			}
		}(g)
	}
	wg.Wait()
}
