package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/store"
)

// ── RELEASE #25 COURSE-EXITS (lane mentor-course-exits) — trader call-site pins ──
//
// Named mutants (each anchored to ONE site, run RED then restored; see the PR):
//   X3 mentor_exit_drive.go  `at.mentorResonanceRunnerTP(nt, p, p.RunnerTarget)` deleted
//      → TestCX3ResonanceRunnerTPIsSentOnTheRunnerLeg RED.
//   X4 mentor_trail_r22.go   `at.mentorBEBeforePartialStep(nt, p, c)` deleted
//      → TestCX4BEBeforeThePartialAtHalfway RED.
//   X5 mentor_trail_r22.go   `if !mentorISBC3PartialAnyProfit(at.mentorRiskControl()) {` → `if false {`
//      → TestCX5ISBCandle3PartialWaitsFor1RWhenOff RED.
//   X2 mentor_actions.go     the mentorSwingRunnerLegs hunk deleted
//      → TestCX2SwingRunnerSplitsAtTheFirstTarget RED.

func cxBool(v bool) *bool { return &v }

// TestCX3ResonanceRunnerTPIsSentOnTheRunnerLeg — D2.4 p1 @01:58–02:09 "70-80% là
// nó sẽ phá luôn cái đỉnh bên tay trái" + @02:24–02:47 "để stop loss ở break
// even, cho nó chạy": a same-direction ISB within 3 candles of a PLH fill arms
// mode A — both stops to BE, and (knob ON, the default) the RUNNER's TP moves
// out to the level beyond the old extreme through a REAL modify_bracket on wire
// leg 2; leg 1's +1R TP is never touched. It was logged "WOULD set TP …
// UNWIRED" and never sent. Knob OFF: logged only, no frame.
func TestCX3ResonanceRunnerTPIsSentOnTheRunnerLeg(t *testing.T) {
	const beyond = 30680.00 // the level beyond the old low (short) the evaluator stamped
	for _, on := range []bool{true, false} {
		rc := store.RiskControlConfig{}
		if !on {
			rc.MentorResonanceRunnerTPOldHigh = cxBool(false)
		}
		b := r22Fixture(t, rc)
		b.positions("short", 3)
		b.book(r22FullBook(r22Stop)...)
		mentorRunnerTargets.Store(mentorRunnerTargetKey(b.at.id, "isb-r22"), beyond)
		t.Cleanup(func() { mentorRunnerTargets.Delete(mentorRunnerTargetKey(b.at.id, "isb-r22")) })
		b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
		// the fill candle closes (BarsSinceFill 1), nothing reached
		b.candle(r22Minute(0), 30742.25, 30744.00, 30738.00, 30739.00)
		b.at.mentorArmResonanceOnISB("short")
		if lp := b.at.mentorLivePos[r22Sig]; lp == nil || lp.Pos.Mode != "A-resonance" {
			t.Fatalf("on=%v: the resonance must arm mode A: %+v", on, lp)
		}
		mods := b.waitMods(1)
		if on {
			if len(mods) != 1 || mods[0].SignalID != r22Sig || mods[0].Leg != 2 || mods[0].NewTakeProfit != beyond || mods[0].NewStopLoss != 0 {
				t.Fatalf("knob ON: want ONE modify_bracket {leg 2, TP %.2f, no stop}; got %+v", beyond, mods)
			}
			if n := b.at.mentorCountSnapshot()["resonance_runner_tp_sent"]; n != 1 {
				t.Fatalf("resonance_runner_tp_sent = %d, want 1", n)
			}
			if tp := b.at.mentorLivePos[r22Sig].Legs[1].TP; tp != beyond {
				t.Fatalf("the runner's TP belief must follow the sent modify: %.2f", tp)
			}
		} else {
			if len(mods) != 0 {
				t.Fatalf("knob OFF: no modify frame (logged only); got %+v", mods)
			}
			if n := b.at.mentorCountSnapshot()["modify_bracket_resonance_logged"]; n != 1 {
				t.Fatalf("knob OFF: modify_bracket_resonance_logged = %d, want 1", n)
			}
		}
		for _, m := range mods {
			if m.Leg == 1 {
				t.Fatalf("leg 1's +1R TP must never be modified by the resonance (R-RES): %+v", mods)
			}
		}
	}
}

// TestCX4BEBeforeThePartialAtHalfway — D1.2 p1 @12:41–13:12 "kéo stop … về
// break even là 10 … ở mức 1-1" (entry 10, target 20, price 15). Knob OFF (the
// default, owner ruling R3): a close past half-way to +1R with NO partial moves
// NOTHING. Knob ON: that close sends BOTH legs' stops to break-even.
func TestCX4BEBeforeThePartialAtHalfway(t *testing.T) {
	for _, on := range []bool{false, true} {
		rc := store.RiskControlConfig{MentorBEBeforePartial: cxBool(on)} // course-defaults-30: the default is ON, so OFF is explicit
		b := r22Fixture(t, rc)
		b.positions("short", 3)
		b.book(r22FullBook(r22Stop)...)
		b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
		// R = 18 → half-way to +1R = 30733.25. Close 30732.00: past half-way,
		// short of +1R (30724.25), no receipt.
		b.candle(r22Minute(0), 30742.25, 30743.00, 30731.00, 30732.00)
		got := b.waitMoves(2)
		if !on {
			if len(got) != 0 {
				t.Fatalf("knob OFF (R3): nothing moves before the confirmed partial; got %+v", got)
			}
			continue
		}
		if len(got) != 2 {
			t.Fatalf("knob ON: want 2 move_stop frames (leg 1 + runner) to BE; got %+v", got)
		}
		legs := map[int]bool{}
		for _, m := range got {
			if m.NewStopLoss != r22Entry || m.SignalID != r22Sig {
				t.Fatalf("knob ON: every move must be to BE %.2f: %+v", r22Entry, got)
			}
			legs[m.Leg] = true
		}
		if !legs[1] || !legs[2] {
			t.Fatalf("knob ON: both legs (1 and 2) must move: %+v", got)
		}
		if n := b.at.mentorCountSnapshot()["be_before_partial"]; n != 1 {
			t.Fatalf("be_before_partial = %d, want 1", n)
		}
	}
}

// TestCX4BEBeforePartialWaitsShortOfHalfway — a close short of half-way moves
// nothing even with the knob ON (the rule is "half-way", never the fill).
func TestCX4BEBeforePartialWaitsShortOfHalfway(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{MentorBEBeforePartial: cxBool(true)})
	b.positions("short", 3)
	b.book(r22FullBook(r22Stop)...)
	b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
	b.candle(r22Minute(0), 30742.25, 30743.00, 30734.00, 30736.00) // +0.35R
	if got := b.waitMoves(0); len(got) != 0 {
		t.Fatalf("a close short of half-way must move nothing; got %+v", got)
	}
}

// TestCX5ISBCandle3PartialWaitsFor1RWhenOff — the candle-3 partial at a close
// in profit short of +1R: nil (today's rule, the default) sends the leg-1 TP
// modify (TestR22Candle3PartialIsARealModify); false ("bán bớt ở 1:1", D2.2 p3
// @12:13) sends NOTHING, counted isb_partial_wait_1r.
func TestCX5ISBCandle3PartialWaitsFor1RWhenOff(t *testing.T) {
	if !mentorISBC3PartialAnyProfit(nil) || !mentorISBC3PartialAnyProfit(&store.RiskControlConfig{}) {
		t.Fatal("CX-5: an absent key must keep today's rule (the any-profit candle-3 partial)")
	}
	b := r22Fixture(t, store.RiskControlConfig{MentorISBC3PartialAnyProfit: cxBool(false)})
	b.positions("short", 3)
	b.book(r22FullBook(r22Stop)...)
	b.registerSplit(r22Sig, "short", "ISB", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
	b.candle(r22Minute(0), 30742.25, 30744.00, 30735.00, 30737.50)
	if mods := b.waitMods(0); len(mods) != 0 {
		t.Fatalf("knob false: no candle-3 modify; leg 1 keeps its +1R TP; got %+v", mods)
	}
	if n := b.at.mentorCountSnapshot()["isb_partial_wait_1r"]; n != 1 {
		t.Fatalf("isb_partial_wait_1r = %d, want 1", n)
	}
}

// TestCX2SwingRunnerSplitsAtTheFirstTarget — D5.2 p2 @06:19–06:27 "đừng vô 1
// MNQ, vô at least là 2 … để có đường bắn bớt": with mentor_swing_runner ON and
// mentor_swing4h_contracts 2 the PRODUCTION writer authors ONE row with leg 1 =
// 1 contract at the swing's first target (the intent's target) and the runner's
// TP beyond it. OFF (the default): today's single bracket. ON with 1 contract:
// single bracket, counted swing_runner_needs_2_contracts.
func TestCX2SwingRunnerSplitsAtTheFirstTarget(t *testing.T) {
	in := mentor.Intent{Action: mentor.PlaceStopEntry, ArmID: "swing-1", Setup: "SWING4H",
		Side: mentor.SideShort, Price: 10155, Stop: 10198.16, Target: 10103.5,
		ExpiryMs: time.Now().UnixMilli() + 60_000}
	read := func(st *store.Store) store.ArmedOrderDB {
		var rows []store.ArmedOrderDB
		if err := st.ArmedOrders().DB().Where("scenario LIKE ?", "%swing-1%").Find(&rows).Error; err != nil || len(rows) != 1 {
			t.Fatalf("want ONE swing row, got %d (%v)", len(rows), err)
		}
		return rows[0]
	}
	// OFF (explicit — the course-defaults-30 default is ON): single bracket.
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorSwingRunner: cxBool(false)}})
	at.mentorArmIntent(in, mentorSizeChoice{Contracts: 2, Tier: "swing4h", Why: "test"}, 1000, 1100, "swing", 0)
	if r := read(st); r.Leg1Qty != nil || r.TargetPx != in.Target {
		t.Fatalf("knob OFF: today's single bracket at the first target; got leg1=%v target %.2f", r.Leg1Qty, r.TargetPx)
	}
	// ON, 2 contracts: leg 1 = 1 at the first target, the runner beyond.
	at2, st2 := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorSwingRunner: cxBool(true)}})
	at2.mentorArmIntent(in, mentorSizeChoice{Contracts: 2, Tier: "swing4h", Why: "test"}, 1000, 1100, "swing", 0)
	r := read(st2)
	if r.Leg1Qty == nil || *r.Leg1Qty != 1 || r.Leg1TP != in.Target {
		t.Fatalf("knob ON: leg 1 must be 1 contract at the first target %.2f; got leg1=%v tp %.2f", in.Target, r.Leg1Qty, r.Leg1TP)
	}
	if want := in.Price - 103; r.TargetPx != want { // max(100, 2 × 51.5) = 103 below the entry
		t.Fatalf("knob ON: the runner TP must be %.2f; got %.2f", want, r.TargetPx)
	}
	// ON, 1 contract: single bracket, counted.
	at3, st3 := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorSwingRunner: cxBool(true)}})
	at3.mentorArmIntent(in, mentorSizeChoice{Contracts: 1, Tier: "swing4h", Why: "test"}, 1000, 1100, "swing", 0)
	if r := read(st3); r.Leg1Qty != nil {
		t.Fatalf("knob ON, 1 contract: a runner needs ≥ 2 — single bracket; got leg1=%v", r.Leg1Qty)
	}
	if n := at3.mentorCountSnapshot()["swing_runner_needs_2_contracts"]; n != 1 {
		t.Fatalf("swing_runner_needs_2_contracts = %d, want 1", n)
	}
}

// TestCX2SplitSwingHoldCloseNeverOverCloses — a split swing whose leg 1 already
// took the first target closes only what the broker still holds; an unreadable
// book refuses (the stop protects). A single-bracket swing is unchanged.
func TestCX2SplitSwingHoldCloseNeverOverCloses(t *testing.T) {
	at := &AutoTrader{}
	old := mentorDriveOpenQty
	t.Cleanup(func() { mentorDriveOpenQty = old })
	split := store.ArmedOrderDB{Leg1Qty: store.IntPtr(1)}
	mentorDriveOpenQty = func(*AutoTrader) (map[string]float64, bool) { return map[string]float64{"short": 1}, true }
	if q, ok := at.mentorSwingCloseQty(split, "short", 2); !ok || q != 1 {
		t.Fatalf("split swing after leg 1: close 1 (the broker's open qty), got %v %v", q, ok)
	}
	mentorDriveOpenQty = func(*AutoTrader) (map[string]float64, bool) { return nil, false }
	if _, ok := at.mentorSwingCloseQty(split, "short", 2); ok {
		t.Fatal("an unreadable broker book must refuse the split swing close (never an over-close)")
	}
	if q, ok := at.mentorSwingCloseQty(store.ArmedOrderDB{}, "short", 2); !ok || q != 2 {
		t.Fatalf("single-bracket swing: unchanged qty, got %v %v", q, ok)
	}
}

// TestCXKnobDefaultsAndBootLine — every absent key resolves to its knob-rule
// default, onto the evaluator config, and the boot line READS them per trader.
func TestCXKnobDefaultsAndBootLine(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	cfg := at.mentorEvaluatorConfig()
	d := mentor.DefaultCourseExitsCfg()
	if cfg.CourseExits != d {
		t.Fatalf("absent keys must resolve to the kernel defaults: %+v vs %+v", cfg.CourseExits, d)
	}
	line := at.mentorCourseExitsBootLine()
	for _, want := range []string{"swing_ema34_is_first_target=ON", "swing_runner=ON (swing contracts 2)", "resonance_runner_tp_old_high=ON",
		"be_before_partial=ON", "isb_c3_partial_any_profit=ON", "reverse_isb_as_phl=ON", "ema9_visit_cap=shared",
		"isb_max_pts=25", "keypair_candle_ratio=1", "box_fresh_rth_pair=OFF", "box_max_height_pts=off"} {
		if !strings.Contains(line, want) {
			t.Fatalf("boot line %q missing %q", line, want)
		}
	}
	// stored values reach the evaluator and the line (READ, never literal).
	at2, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true,
		MentorSwingEMA34IsFirstTarget: cxBool(false), MentorReverseISBAsPHL: cxBool(true), MentorEMA9VisitCap: "exempt",
		MentorISBMaxPts: 26, MentorKeypairCandleRatio: 3, MentorBoxFreshRTHPair: cxBool(true), MentorBoxMaxHeightPts: 40}})
	c2 := at2.mentorEvaluatorConfig().CourseExits
	if c2.SwingEMA34IsFirstTarget || !c2.ReverseISBAsPHL || c2.EMA9VisitCap != "exempt" || c2.ISBMaxStopPts != 26 ||
		c2.KeypairCandleRatio != 3 || !c2.BoxFreshRTHPair || c2.BoxMaxHeightPts != 40 {
		t.Fatalf("stored knobs must reach the evaluator: %+v", c2)
	}
	if l2 := at2.mentorCourseExitsBootLine(); !strings.Contains(l2, "swing_ema34_is_first_target=OFF") || !strings.Contains(l2, "isb_max_pts=26") {
		t.Fatalf("the boot line must READ the stored values: %q", l2)
	}
	// a bad value fails closed and is counted.
	at3, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorEMA9VisitCap: "bogus", MentorKeypairCandleRatio: -2}})
	c3 := at3.mentorEvaluatorConfig().CourseExits
	if c3.EMA9VisitCap != "shared" || c3.KeypairCandleRatio != 1 {
		t.Fatalf("bad values must fail closed to the defaults: %+v", c3)
	}
	snap := at3.mentorCountSnapshot()
	if snap["ema9_visit_cap_bad_value"] == 0 || snap["keypair_candle_ratio_bad_value"] == 0 {
		t.Fatalf("bad values must be counted: %v", snap)
	}
}
