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
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "isb-1", LegIndex: 0, Side: "LONG",
		State: store.StateCancelled, EntryPx: 100, StopPx: 99, TargetPx: 102,
		Kind: "stop_entry",
	}
	if err := ledger.DB().Create(&old).Error; err != nil {
		t.Fatal(err)
	}

	// A reload/restart bumps the epoch, then the SAME evaluator ArmID arrives.
	bumpMentorArmEpoch()
	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-1", Setup: "ISB",
		Side: mentor.SideLong, Price: 200, Stop: 199, Target: 204, StopPts: 1, TargetPts: 4,
		ExpiryMs: time.Now().UnixMilli() + 60_000}
	at.mentorArmIntent(in, mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}, 1000, 1100, "B", 0)

	arm, ok := mentorLiveArmFor("isb-1")
	if !ok || len(arm.RowIDs) == 0 || arm.RowIDs[0] == old.ID {
		t.Fatalf("the new arm must be authored on its own row after a reset, got %+v ok=%v (old row %d)", arm, ok, old.ID)
	}
	var row store.ArmedOrderDB
	if err := ledger.DB().First(&row, arm.RowIDs[0]).Error; err != nil {
		t.Fatalf("the new arm row must exist: %v", err)
	}
	if row.State != store.StateArmed || row.Scenario == old.Scenario {
		t.Fatalf("new arm state=%q scenario=%q, want armed on a distinct scenario", row.State, row.Scenario)
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
	ResetMentorCountersForTest()

	// Pin the epoch and seed a TERMINAL no-signal row for THIS construction's
	// scenario, so UpsertArm finds it and no-ops (returns nil, id 0).
	bumpMentorArmEpoch()
	scenario := fmt.Sprintf("isb-1-%d", mentorArmEpoch.Load())
	old := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
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

	if _, ok := mentorLiveArmFor("isb-1"); ok {
		t.Fatal("a row-0 arm must NOT be registered")
	}
	if c := MentorCountSnapshot()["placement_refused_arm_id_zero"]; c != 1 {
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
