package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
	"vl/telemetry"
)

// ── m425a (release #50) — S2 + S3 call-site pins ────────────────────────────

// TestM425aEODFlatAuthoringRefused — S3. rows 447/448 (10-23): an ISB and a PLH
// were authored at 15:30:00.081 / 15:35:00.084 CT and cancelled ~10 ms later
// "session ended (EOD flat)". The authoring path now refuses with a NAMED
// refusal + the mentor_eod_flat gate-block counter, asking the SAME functions
// the admission gate / armed pass ask (forceFlatWindowAt, ActiveSession).
// Revert the mentorEODFlatAuthorRefusal call in mentorPlaceIntent → the arm is
// placed (recorder fires) → FAIL.
func TestM425aEODFlatAuthoringRefused(t *testing.T) {
	telemetry.ResetSetupStallsForTest()
	t.Setenv("MENTOR_PLACE", "1")
	// the window knob is disabled (-1: any hour) exactly as in production, where
	// the 15:30 arm passed the window gate.
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorWindowMinutes: -1})
	at.mentorEval = mentor.New(mentor.Config{})
	at.mentorEval.State.ISBArms["isb-22"] = mentor.ISBArm{Inside: 0, Side: mentor.SideLong}
	resetMentorLiveArms(t)
	wireMentorPlacementSeams(t, at)
	var placed []mentor.Intent
	mentorPlaceRecorderForTest = func(in mentor.Intent, _ int) { placed = append(placed, in) }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	// 15:30:00.081 CT on a Friday — the NY session is over.
	now := time.Date(2026, 10, 23, 15, 30, 0, 81_000_000, kernel.CTLocation())
	setMentorNowSource(func() time.Time { return now })
	t.Cleanup(func() { setMentorNowSource(nil) })
	mentorStaleBlockFeed(t, now, 0)
	before := m425aGateBlocks(at.id, "mentor_eod_flat")

	bar := now.UnixMilli()
	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-22", Side: mentor.SideLong, Setup: "ISB",
		Price: 21000, Stop: 20988, Target: 21024, StopPts: 12, RefBarMs: bar, ExpiryMs: bar + 60_000}
	at.mentorDispatchIntent(in, mentorTierInputs{}, bar, bar)

	if len(placed) != 0 {
		t.Fatalf("an arm must not be authored inside the EOD-flat window: %+v", placed)
	}
	if got := at.mentorCountSnapshot()["placement_refused_eod_flat"]; got != 1 {
		t.Fatalf("placement_refused_eod_flat = %d, want 1", got)
	}
	if after := m425aGateBlocks(at.id, "mentor_eod_flat"); after != before+1 {
		t.Fatalf("mentor_eod_flat gate-block counter %d -> %d, want +1", before, after)
	}
	if got := stallLastRefusal(at.id, "isb"); !strings.HasPrefix(got, "eod_flat:") {
		t.Fatalf("the stall record must name the refusal eod_flat, got %q", got)
	}
	if _, ok := at.mentorEval.State.ISBArms["isb-22"]; ok {
		t.Fatalf("the eod_flat refusal must also drop the evaluator's ISB arm")
	}
	// SWING4H is exempt, as at admission.
	if refuse, why := at.mentorEODFlatAuthorRefusal(mentor.Intent{Setup: "SWING4H"}, now); refuse {
		t.Fatalf("SWING4H must be exempt, refused: %s", why)
	}
	// control: mid-session (10:00 CT) is not an EOD refusal.
	mid := time.Date(2026, 10, 23, 10, 0, 0, 0, kernel.CTLocation())
	if refuse, why := at.mentorEODFlatAuthorRefusal(in, mid); refuse {
		t.Fatalf("10:00 CT must not be refused as EOD flat: %s", why)
	}
}

// TestM425aRefusedPHLEndsTouchKeepsReason — S2. A PHL the trader refuses
// (window gate, 10-23 04:25:59 CT) ends that touch for the level in the
// evaluator (RefusedLevelTouch, so it is not re-emitted every minute) and the
// stall record keeps the ORIGINAL reason, not a later stale_intent.
// Revert the RefusedLevelTouch bookkeeping in Evaluator.DropArm → FAIL.
func TestM425aRefusedPHLEndsTouchKeepsReason(t *testing.T) {
	telemetry.ResetSetupStallsForTest()
	t.Setenv("MENTOR_PLACE", "1")
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true}) // default window 08:30 +60m
	at.mentorEval = mentor.New(mentor.Config{})
	const key = "L"
	refClose := int64(1_792_000_000_000 + 59_999)
	at.mentorEval.State.Touches = map[string]mentor.Touch{key: {LevelKey: key, Outcome: mentor.TouchReject}}
	tr := at.mentorEval.State.Touches[key]
	tr.RefBar.CloseTime = refClose
	at.mentorEval.State.Touches[key] = tr
	at.mentorEval.State.LevelArms = map[string]mentor.LevelArm{key: {ArmID: "lvl-7", Side: mentor.SideLong, LevelPrice: 21000, PlacedAt: refClose}}
	resetMentorLiveArms(t)
	wireMentorPlacementSeams(t, at)
	now := time.Date(2026, 10, 23, 4, 25, 59, 0, kernel.CTLocation())
	setMentorNowSource(func() time.Time { return now })
	t.Cleanup(func() { setMentorNowSource(nil) })
	mentorStaleBlockFeed(t, now, 0)

	in := mentor.Intent{Action: mentor.PlaceStopEntry, ArmID: "lvl-7", Side: mentor.SideLong, Setup: "PHL",
		Price: 21000, Stop: 20988, Target: 21024, StopPts: 12, RefBarMs: now.UnixMilli(), AnchorKey: key}
	at.mentorDispatchIntent(in, mentorTierInputs{}, now.UnixMilli(), now.UnixMilli())

	if got := stallLastRefusal(at.id, "lvl"); !strings.HasPrefix(got, "window:") {
		t.Fatalf("the stall record must keep the ORIGINAL refusal (window), got %q", got)
	}
	if got := at.mentorEval.State.RefusedLevelTouch[key]; got != refClose {
		t.Fatalf("the refused touch must be recorded for the level (ref bar %d), got %v", refClose, at.mentorEval.State.RefusedLevelTouch)
	}
	if len(at.mentorEval.State.LevelArms) != 0 {
		t.Fatalf("the refused level arm must be unregistered: %+v", at.mentorEval.State.LevelArms)
	}
}

func m425aGateBlocks(traderID, gate string) int {
	_, tbl := telemetry.GateBlockSnapshot()
	return tbl[traderID][gate]
}

// m426 (review of m425a S3): authoring and the armed pass agree in BOTH plan
// states. The armed pass cancels "session ended (EOD flat)" only when no AI day
// plan is active, so in the 14:45-17:00 gap an arm is refused with no plan and
// authored with one. Mutant: drop the ActivePlanFor early return -> the plan
// state is refused too -> FAIL.
func TestM426EODFlatAuthoringMirrorsArmedPassPlanCondition(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorWindowMinutes: -1})
	now := time.Date(2026, 10, 23, 15, 30, 0, 0, kernel.CTLocation())
	in := mentor.Intent{Setup: "ISB"}
	t.Cleanup(func() { kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{}) })

	kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{ActivePlan: func(string) *kernel.ActivePlan { return nil }})
	if refuse, _ := at.mentorEODFlatAuthorRefusal(in, now); !refuse {
		t.Fatal("no active plan + session ended: the armed pass cancels, so authoring must refuse")
	}
	kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{ActivePlan: func(string) *kernel.ActivePlan {
		return &kernel.ActivePlan{Session: "NY"}
	}})
	if refuse, why := at.mentorEODFlatAuthorRefusal(in, now); refuse {
		t.Fatalf("active plan: the armed pass keeps the arm, so authoring must not refuse: %s", why)
	}
}
