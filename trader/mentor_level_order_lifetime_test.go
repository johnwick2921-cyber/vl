package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── D2-44 (item 11) — level-order lifetime: a LEVEL ("lvl-") arm RESTS until
// the RTH window end (15:00 CT) or a close-through, never the next 1m candle;
// the trader-side cancel/expiry clears the evaluator's LevelArms so the level
// can re-emit on the next valid touch. ─────────────────────────────────────────

// mentorLifetimeClock is a fixed CT instant helper for the lifetime tests.
func mentorLifetimeClock(h, m int) time.Time {
	return time.Date(2026, 9, 23, h, m, 0, 0, kernel.CTLocation())
}

// (FIX A) a "lvl-" arm expires at the next 15:00 CT; the ISB stays one candle;
// an intent-carried expiry always wins.
func TestMentorIntentExpiryLevelRestsTo1500(t *testing.T) {
	barClose := mentorLifetimeClock(10, 30).UnixMilli()
	want := mentorLifetimeClock(15, 0).UnixMilli()

	if got := mentorIntentExpiry(mentor.Intent{ArmID: "lvl-3", Setup: "PHL"}, barClose); got != want {
		t.Fatalf("level arm expiry = %d, want %d (15:00 CT)", got, want)
	}
	if got := mentorIntentExpiry(mentor.Intent{ArmID: "isb-1", Setup: "ISB"}, barClose); got != barClose+60_000 {
		t.Fatalf("ISB expiry = %d, want %d (next 1m candle)", got, barClose+60_000)
	}
	if got := mentorIntentExpiry(mentor.Intent{ArmID: "lvl-4", ExpiryMs: 12345}, barClose); got != 12345 {
		t.Fatalf("intent-carried expiry must win: got %d, want 12345", got)
	}
	// After 15:00 CT the level expiry fail-safes to tomorrow's 15:00 (guard only
	// — placement past RTH is refused upstream).
	late := mentorLifetimeClock(16, 0).UnixMilli()
	if got := mentorIntentExpiry(mentor.Intent{ArmID: "lvl-5"}, late); got != mentorLifetimeClock(15, 0).AddDate(0, 0, 1).UnixMilli() {
		t.Fatalf("after-15:00 level expiry = %d, want tomorrow 15:00 CT", got)
	}
}

// (FIX B) the injector cancel clears the evaluator's LevelArms entry — under
// the N11 mutex in production (mentorDispatchIntent is reached from
// mentorEvalOnce); the lock-free half is correct because the caller holds the
// mutex.
func TestMentorCancelArmClearsEvaluatorLevelArm(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	at.mentorEval = mentor.New(at.mentorEvaluatorConfig())
	at.mentorEval.State.LevelArms = map[string]mentor.LevelArm{
		"L": {ArmID: "lvl-9", Side: mentor.SideLong, LevelPrice: 29385},
	}
	now := time.Now()
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "lvl-9",
		Side: "long", EntryPx: 29392, StopPx: 29385, TargetPx: 29430, Kind: "stop_entry", Condition: "PHL",
		State: store.StateArmed, Origin: store.ArmOriginMentor, ExpiryMs: now.UnixMilli() + 60_000}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	mentorRegisterLiveArm("lvl-9", row.ID, "long", 29392)

	at.mentorDispatchIntent(mentor.Intent{Action: mentor.CancelArm, ArmID: "lvl-9", Reason: "test close-through"}, mentorTierInputs{}, 1000, 1100)

	if len(at.mentorEval.State.LevelArms) != 0 {
		t.Fatalf("the injector cancel must clear the evaluator LevelArms; got %+v", at.mentorEval.State.LevelArms)
	}
}

// (FIX B trap) the tick reconcile clears a LevelArms entry whose ledger row is
// no longer resting — the N12 expiry sweep's terminal write is reconciled on
// the same tick. Mutant: drop the mentorReconcileLevelArms call → RED.
func TestMentorTickReconcilesTerminalLevelArm(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	at.mentorEval = mentor.New(at.mentorEvaluatorConfig())
	at.mentorEval.State.LevelArms = map[string]mentor.LevelArm{
		"L": {ArmID: "lvl-9", Side: mentor.SideLong, LevelPrice: 29385},
	}
	// A level arm the TRADER already expired/cancelled (terminal), but the
	// evaluator still believes rests.
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "lvl-9",
		Side: "long", EntryPx: 29392, StopPx: 29385, TargetPx: 29430, Kind: "stop_entry", Condition: "PHL",
		State: store.StateCancelled, Origin: store.ArmOriginMentor, ExpiryMs: time.Now().UnixMilli()}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	now := mentorLifetimeClock(10, 30)
	// Bars close ABOVE the resting level (29385) so the evaluator's OWN
	// close-through sweep (levelArmCancels) does not clear the entry — only the
	// tick reconcile may.
	bars := []market.Kline{
		{OpenTime: now.Add(-2 * time.Minute).UnixMilli(), CloseTime: now.Add(-2*time.Minute).UnixMilli() + 59_999, Open: 29390, High: 29395, Low: 29388, Close: 29393, Final: true},
		{OpenTime: now.Add(-time.Minute).UnixMilli(), CloseTime: now.Add(-time.Minute).UnixMilli() + 59_999, Open: 29393, High: 29398, Low: 29391, Close: 29396, Final: true},
	}

	at.mentorEvalOnce(bars)

	if len(at.mentorEval.State.LevelArms) != 0 {
		t.Fatalf("the tick must reconcile the terminal level arm out of LevelArms; got %+v", at.mentorEval.State.LevelArms)
	}
}
