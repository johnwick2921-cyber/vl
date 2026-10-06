package trader

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// mentoredTrader builds a minimal AutoTrader with the given risk-control
// config — enough for the mentor mode switch surfaces.
func mentoredTrader(t *testing.T, rc store.RiskControlConfig) *AutoTrader {
	t.Helper()
	return &AutoTrader{
		id: "t-mentor",
		config: AutoTraderConfig{
			StrategyConfig: &store.StrategyConfig{RiskControl: rc},
		},
	}
}

// TestMentorContractsForSizeTable pins EVERY tier at the production call site
// (mentorContractsFor). A size tier off by one — the CTO's mutant — fails this.
func TestMentorContractsForSizeTable(t *testing.T) {
	base, conf, big, reduced, swing4h, spentCap, mx := 5, 10, 20, 3, 3, 2, 20
	cases := []struct {
		name string
		in   mentorTierInputs
		want mentorSizeChoice
	}{
		{"base ISB", mentorTierInputs{Setup: "ISB", StopPts: 5.75, TargetPts: 11.5}, mentorSizeChoice{5, "base", ""}},
		{"base PHL", mentorTierInputs{Setup: "PHL", StopPts: 11.25, TargetPts: 22.5}, mentorSizeChoice{5, "base", ""}},
		{"confluence", mentorTierInputs{Setup: "PHL", StopPts: 11.25, Confluence: true}, mentorSizeChoice{10, "confluence", ""}},
		{"big", mentorTierInputs{Setup: "PHL", StopPts: 12, TargetPts: 35, RoomMultiple: 2.5, Confluence: true, HTFAgree: true}, mentorSizeChoice{20, "big", ""}},
		{"big needs 4h AND 1h", mentorTierInputs{Setup: "PHL", StopPts: 12, TargetPts: 35, RoomMultiple: 2.5, Confluence: true, HTFAgree: false}, mentorSizeChoice{10, "confluence", ""}},
		{"big needs room", mentorTierInputs{Setup: "PHL", StopPts: 19.5, TargetPts: 35, RoomMultiple: 1.5, Confluence: true, HTFAgree: true}, mentorSizeChoice{10, "confluence", ""}},
		{"big needs target", mentorTierInputs{Setup: "PHL", StopPts: 12, TargetPts: 29, RoomMultiple: 2.5, Confluence: true, HTFAgree: true}, mentorSizeChoice{10, "confluence", ""}},
		{"stop in the twenties", mentorTierInputs{Setup: "PLH", StopPts: 23, TargetPts: 46}, mentorSizeChoice{3, "reduced", ""}},
		{"stop at 20 boundary", mentorTierInputs{Setup: "PLH", StopPts: 20, TargetPts: 40}, mentorSizeChoice{3, "reduced", ""}},
		{"stop below twenties", mentorTierInputs{Setup: "PLH", StopPts: 19.5, TargetPts: 40}, mentorSizeChoice{5, "base", ""}},
		// B12 (CTO 1791041016051): the stop-in-the-twenties cut wins over
		// confluence sizing — 3, never 10/20.
		{"twenties cut beats confluence", mentorTierInputs{Setup: "PHL", StopPts: 22, TargetPts: 30, Confluence: true}, mentorSizeChoice{3, "reduced", ""}},
		{"twenties cut beats big", mentorTierInputs{Setup: "PHL", StopPts: 22, TargetPts: 35, RoomMultiple: 2.5, Confluence: true, HTFAgree: true}, mentorSizeChoice{3, "reduced", ""}},
		{"spent day no longer overrides twenties", mentorTierInputs{Setup: "PLH", StopPts: 23, TargetPts: 46, SpentDay: true}, mentorSizeChoice{3, "reduced", ""}},
		{"spent day sizes base normally", mentorTierInputs{Setup: "PLH", StopPts: 11.25, TargetPts: 22.5, SpentDay: true}, mentorSizeChoice{5, "base", ""}},
		{"SWING4H", mentorTierInputs{Setup: "SWING4H", StopPts: 12, TargetPts: 24}, mentorSizeChoice{3, "swing4h", ""}},
		{"hard cap", mentorTierInputs{Setup: "PHL", StopPts: 12, TargetPts: 35, RoomMultiple: 2.5, Confluence: true, HTFAgree: true}, mentorSizeChoice{20, "big", ""}},
		// S9 (D5.2 p2 @05:21–05:57, S15 ruling): a strong day cuts the SWING
		// only — "nhưng chỉ cùng 4 giờ". Other setups keep their tiers.
		{"strong day does NOT cut big (swing-only)", mentorTierInputs{Setup: "PHL", StopPts: 12, TargetPts: 35, RoomMultiple: 2.5, Confluence: true, HTFAgree: true, StrongDay: true}, mentorSizeChoice{20, "big", ""}},
		{"strong day does NOT cut base (swing-only)", mentorTierInputs{Setup: "ISB", StopPts: 5.75, TargetPts: 11.5, StrongDay: true}, mentorSizeChoice{5, "base", ""}},
		{"strong day cuts the SWING to 2", mentorTierInputs{Setup: "SWING4H", StopPts: 30, StrongDay: true}, mentorSizeChoice{2, "strong_day", ""}},
		// ISB at an old high/low → reduce size, tier 3 (owner ruling 00:1x CT,
		// D4.1 p1 written rule 2).
		{"ISB at old extreme → 3", mentorTierInputs{Setup: "ISB", StopPts: 5.75, TargetPts: 11.5, ISBOldExtreme: true}, mentorSizeChoice{3, "isb_old_extreme", ""}},
		{"old extreme beats confluence", mentorTierInputs{Setup: "ISB", StopPts: 11.25, Confluence: true, ISBOldExtreme: true}, mentorSizeChoice{3, "isb_old_extreme", ""}},
		{"strong day does not beat old extreme (swing-only)", mentorTierInputs{Setup: "ISB", StopPts: 5.75, TargetPts: 11.5, StrongDay: true, ISBOldExtreme: true}, mentorSizeChoice{3, "isb_old_extreme", ""}},
		{"spent day no longer overrides old extreme", mentorTierInputs{Setup: "ISB", StopPts: 5.75, TargetPts: 11.5, SpentDay: true, ISBOldExtreme: true}, mentorSizeChoice{3, "isb_old_extreme", ""}},
		// ISB in a range → reduce size, tier 3 (written rule 3, D4.1 p1
		// @08:05/09:40) — same rank as the old-extreme reduction.
		{"ISB in range → 3", mentorTierInputs{Setup: "ISB", StopPts: 5.75, TargetPts: 11.5, ISBInRange: true}, mentorSizeChoice{3, "isb_in_range", ""}},
		{"in range beats confluence", mentorTierInputs{Setup: "ISB", StopPts: 11.25, Confluence: true, ISBInRange: true}, mentorSizeChoice{3, "isb_in_range", ""}},
		{"strong day does not beat in range (swing-only)", mentorTierInputs{Setup: "ISB", StopPts: 5.75, TargetPts: 11.5, StrongDay: true, ISBInRange: true}, mentorSizeChoice{3, "isb_in_range", ""}},
		{"spent day no longer overrides in range", mentorTierInputs{Setup: "ISB", StopPts: 5.75, TargetPts: 11.5, SpentDay: true, ISBInRange: true}, mentorSizeChoice{3, "isb_in_range", ""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := mentorContractsFor(c.in, base, conf, big, reduced, swing4h, spentCap, mx)
			if err != nil {
				t.Fatalf("mentorContractsFor: %v", err)
			}
			if got.Contracts != c.want.Contracts || got.Tier != c.want.Tier {
				t.Fatalf("got (%d, %s, %q), want (%d, %s)", got.Contracts, got.Tier, got.Why, c.want.Contracts, c.want.Tier)
			}
		})
	}
}

// TestMentorExtraForCopiesSpentDay — A5 (CTO 1791041016051): the spent-day
// flag rides the intent (stamped by the evaluator) and must reach the size
// table (which, per R09, no longer sizes the whole trade to 2 — the flag feeds
// the 15-pt stop cap and the runner cap). Dropping the SpentDay copy here
// would silently kill the R9 15-pt stop cap in production.
func TestMentorExtraForCopiesSpentDay(t *testing.T) {
	old := mentorConfluenceForIntent
	mentorConfluenceForIntent = func(in mentor.Intent) bool { return true }
	t.Cleanup(func() { mentorConfluenceForIntent = old })

	got := mentorExtraFor(mentor.Intent{SpentDay: true}, true)
	if !got.SpentDay || !got.StrongDay || !got.Confluence {
		t.Fatalf("mentorExtraFor = %+v, want all three flags set", got)
	}
	mentorConfluenceForIntent = func(in mentor.Intent) bool { return false }
	got = mentorExtraFor(mentor.Intent{SpentDay: false}, false)
	if got.SpentDay || got.StrongDay || got.Confluence {
		t.Fatalf("mentorExtraFor = %+v, want all three flags false", got)
	}
}

// TestMentorContractsKnobCap: the owner's per-strategy cap wins over the big
// tier (never above mentor_max_contracts).
func TestMentorContractsKnobCap(t *testing.T) {
	in := mentorTierInputs{Setup: "PHL", StopPts: 12, TargetPts: 35, RoomMultiple: 2.5, Confluence: true, HTFAgree: true}
	got, err := mentorContractsFor(in, 5, 10, 20, 3, 3, 2, 12)
	if err != nil {
		t.Fatal(err)
	}
	if got.Contracts != 12 || got.Tier != "big" {
		t.Fatalf("cap: got %d %s, want 12 big", got.Contracts, got.Tier)
	}
	if _, err := mentorContractsFor(in, 5, 10, 20, 3, 3, 2, 0); err == nil {
		t.Fatal("a non-positive max must refuse (fail-closed)")
	}
}

// TestMentorModeSwitchBothDirections: mentor ON gets the mentor ceiling (the
// 0B clamp does NOT apply); mentor OFF keeps the 2-contract AI clamp
// byte-identical (the mentor size must never leak into AI mode).
func TestMentorModeSwitchBothDirections(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	if !at.mentorEnabled() {
		t.Fatal("mentor_mode on must enable")
	}
	if mx, ok := at.mentorMaxContracts(); !ok || mx != mentorMaxContractsDefault {
		t.Fatalf("mentor max = %d %v, want %d true", mx, ok, mentorMaxContractsDefault)
	}
	if got := at.resolveMaxContracts(); got != mentorMaxContractsDefault {
		t.Fatalf("resolveMaxContracts in mentor mode = %d, want %d (the 0B clamp must not apply)", got, mentorMaxContractsDefault)
	}

	off := mentoredTrader(t, store.RiskControlConfig{})
	if off.mentorEnabled() {
		t.Fatal("mentor_mode off must disable (default)")
	}
	if _, ok := off.mentorMaxContracts(); ok {
		t.Fatal("mentor max must not apply with mentor_mode off")
	}
	if got := off.resolveMaxContracts(); got != 1 {
		t.Fatalf("AI-mode resolveMaxContracts = %d, want 1 (the 0B Stage-A size-1 cap, unchanged)", got)
	}
}

// TestMentorAIEntriesOffButMentorDecisionsPass: the mode switch suppresses
// AI opens only; mentor-sourced decisions and closes pass.
func TestMentorAIEntriesOffButMentorDecisionsPass(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	aiOpen := func(action string) *kernel.Decision {
		return &kernel.Decision{Action: action}
	}
	if r := at.mentorSuppressAIEntry(aiOpen("open_long")); r == "" {
		t.Fatal("an AI open must be refused in mentor mode")
	}
	if r := at.mentorSuppressAIEntry(aiOpen("close_short")); r != "" {
		t.Fatalf("a close must never be suppressed: %q", r)
	}
	m := aiOpen("open_long")
	m.MentorSourced = true
	if r := at.mentorSuppressAIEntry(m); r != "" {
		t.Fatalf("a mentor-sourced open must pass: %q", r)
	}
	off := mentoredTrader(t, store.RiskControlConfig{})
	if r := off.mentorSuppressAIEntry(aiOpen("open_long")); r != "" {
		t.Fatalf("AI mode must never suppress: %q", r)
	}
}

// TestMentorNotionalRoundTripsThroughTheExecutorSizing: the synthetic
// decision's notional must come back out of futuresOrderQuantity as exactly
// the mentor contract count at the mentor ceiling (the mutant that restores
// the 2-contract clamp in resolveMaxContracts fails this).
func TestMentorNotionalRoundTripsThroughTheExecutorSizing(t *testing.T) {
	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12}
	d := mentor.BuildDecision(in, "MNQ", 17)
	if !d.MentorSourced || d.EntryPrice != in.Price {
		t.Fatalf("decision not mentor-marked: %+v", d)
	}
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	q := futuresOrderQuantity(d.Symbol, d.PositionSizeUSD, in.Price, at.resolveMaxContracts())
	if math.Abs(q-17) > 1e-9 {
		t.Fatalf("mentor quantity = %f, want 17 (the 0B clamp must not touch mentor sizing)", q)
	}
}

// TestMentorSplitLegs (CTO ruling 2026-10-03 05:42Z): n → leg 1 = ceil(n/2)
// with its own TP; leg 2 = the runner. n = 1 → a single leg. runnerCap (spent
// day: 2) caps the runner. Parity: ceil(n/2) equals DS-108's replay half.
func TestMentorSplitLegs(t *testing.T) {
	cases := []struct {
		n, cap, want1, want2 int
	}{
		{0, 0, 0, 0},
		{1, 0, 1, 0},
		{2, 0, 1, 1},
		{3, 0, 2, 1},
		{4, 0, 2, 2},
		{5, 0, 3, 2},
		{20, 0, 10, 10},
		{5, 2, 3, 2}, // spent day: at most 2 run after leg 1
		{6, 2, 3, 2}, // the runner is capped, not the total
		{20, 2, 10, 2},
	}
	for _, c := range cases {
		l1, l2 := mentorSplitLegs(c.n, c.cap)
		if l1 != c.want1 || l2 != c.want2 {
			t.Fatalf("mentorSplitLegs(%d, %d) = (%d, %d), want (%d, %d)", c.n, c.cap, l1, l2, c.want1, c.want2)
		}
	}
}

// TestMentorLeg1TPForC: C sets leg 1's TP at ≥1:2 AT ENTRY.
func TestMentorLeg1TPForC(t *testing.T) {
	if got := mentorLeg1TPForC(100, 10, "long"); got != 120 {
		t.Fatalf("C leg-1 TP long = %.2f, want 120 (2R)", got)
	}
	if got := mentorLeg1TPForC(100, 10, "short"); got != 80 {
		t.Fatalf("C leg-1 TP short = %.2f, want 80 (2R)", got)
	}
}

// TestMentorApplyExitResult: a failed or unwired modify_bracket / move_stop is
// logged and counted; wired actions reach their wires per leg.
func TestMentorApplyExitResult(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	ResetMentorCountersForTest()
	// The unwired path needs moveStopWire nil — but it carries a PRODUCTION
	// default (exit_mechs_suspend.go:50), so this test must nil it explicitly
	// to exercise the unwired branch (pre-existing merged-HEAD breakage: the
	// first block read the production default as "wired" and counted
	// move_stop_failed instead of move_stop_unwired).
	oldMoveStopWire := moveStopWire
	moveStopWire = nil
	t.Cleanup(func() { moveStopWire = oldMoveStopWire })
	tp := 103.0

	// unwired: both actions counted, never sent.
	at.mentorApplyExitResult(&ntTrader.TCPTrader{}, "long", mentorExitResult{
		MoveStops: []string{"leg1", "leg2"}, NewStop: 100, ModifyTP: &tp,
	})
	snap := MentorCountSnapshot()
	if snap["move_stop_unwired"] != 2 || snap["modify_bracket_unwired"] != 1 {
		t.Fatalf("unwired actions must be counted (2 move_stop, 1 modify): %v", snap)
	}
	// wired: each leg moves and the TP modifies.
	ResetMentorCountersForTest()
	var moved []string
	var modLeg string
	var modTP float64
	moveStopWire = func(nt *ntTrader.TCPTrader, side string, newStop float64) error {
		moved = append(moved, side)
		return nil
	}
	modifyBracketWire = func(leg string, newTP float64) error { modLeg, modTP = leg, newTP; return nil }
	t.Cleanup(func() { moveStopWire = nil; modifyBracketWire = nil })
	at.mentorApplyExitResult(&ntTrader.TCPTrader{}, "long", mentorExitResult{
		MoveStops: []string{"leg1", "leg2"}, NewStop: 100, ModifyTP: &tp,
	})
	if len(moved) != 2 || modLeg != "leg1" || modTP != 103 {
		t.Fatalf("wired actions: moved=%v modLeg=%q modTP=%.2f", moved, modLeg, modTP)
	}
	// a failing modify is counted, not silent.
	ResetMentorCountersForTest()
	modifyBracketWire = func(leg string, newTP float64) error { return fmt.Errorf("boom") }
	at.mentorApplyExitResult(&ntTrader.TCPTrader{}, "long", mentorExitResult{ModifyTP: &tp})
	if got := MentorCountSnapshot()["modify_bracket_failed"]; got != 1 {
		t.Fatalf("the failed modify must be counted once, got %d", got)
	}
}

// TestMentorConfirmLegProtection: a leg whose protection cannot be confirmed
// on the next snapshot is FLATTENED (that leg only, fail-closed).
func TestMentorConfirmLegProtection(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	ResetMentorCountersForTest()
	var flattened []string
	flattenLegWire = func(leg string) error { flattened = append(flattened, leg); return nil }
	t.Cleanup(func() { flattenLegWire = nil })

	// no confirmation source → EVERY leg is flattened.
	at.mentorConfirmLegProtection([]string{"leg1", "leg2"})
	if len(flattened) != 2 {
		t.Fatalf("nil protection source must flatten both legs, flattened=%v", flattened)
	}
	snap := MentorCountSnapshot()
	if snap["leg_protection_lost_leg1"] != 1 || snap["leg_protection_lost_leg2"] != 1 {
		t.Fatalf("the lost protection must be counted per leg: %v", snap)
	}
	// leg1 confirmed, leg2 not → only leg2 flattens.
	ResetMentorCountersForTest()
	flattened = nil
	mentorLegProtectedSource = func(leg string) bool { return leg == "leg1" }
	t.Cleanup(func() { mentorLegProtectedSource = nil })
	at.mentorConfirmLegProtection([]string{"leg1", "leg2"})
	if len(flattened) != 1 || flattened[0] != "leg2" {
		t.Fatalf("only the unconfirmed leg flattens, flattened=%v", flattened)
	}
}

// TestMentorExitRules (EXIT-SPEC-v3 on SPLIT LEGS): at +0.5R BOTH legs' stops
// arm BE; leg 1's +1R TP candle marks the runner's trail start (the bracket
// closes leg 1 — no wire action); the runner trails behind closed candles only
// AFTER that (no look-ahead); same-bar stop+level is worse (stop); a BE touch
// exits "be", a trailed-stop touch "trail"; trail off (video-8 knob) never
// moves the stop; C never moves the stop, even at +2R.
func TestMentorExitRules(t *testing.T) {
	long := mentorPosition{Symbol: "MNQ", Side: "long", Entry: 100, Stop: 90, R: 10, Leg1: 2, Leg2: 2, Mode: "B"}
	// +0.5R candle (high 105, low 95): arm BE for BOTH legs, no exit, no leg-1
	// crossing, no trail of THIS candle (its low 95 is below entry).
	res := mentorExitB(long, 104, 105, 95, true)
	if res.Exited || res.ExitPrice != 0 || res.ExitReason != "" || res.Leg1AtTarget {
		t.Fatalf("arming candle: %+v — must only arm BE", res)
	}
	if res.NewStop != long.Entry || len(res.MoveStops) != 2 {
		t.Fatalf("arming candle must move BOTH legs to BE 100: %+v", res)
	}
	long.ArmedBE = true
	long.Stop = long.Entry
	// +1R candle (high 110, low above BE): leg 1's TP is hit — the bracket
	// closes leg 1 (no wire action); the runner trails from the NEXT candle.
	res = mentorExitB(long, 109, 110, 100.5, true)
	if res.Exited || !res.Leg1AtTarget || res.ModifyTP != nil {
		t.Fatalf("leg-1-at-target candle: %+v", res)
	}
	if res.NewStop != long.Entry {
		t.Fatalf("the +1R candle must keep the stop at BE, got %.2f", res.NewStop)
	}
	long.Scaled = true
	// a candle that does NOT touch the BE stop trails the RUNNER behind its close.
	res = mentorExitB(long, 101.5, 102, 100.5, true)
	if res.Exited || res.NewStop != 100.5 {
		t.Fatalf("trail candle: %+v — want the runner trailed to the closed low 100.5", res)
	}
	if len(res.MoveStops) != 1 || res.MoveStops[0] != "leg2" {
		t.Fatalf("only the runner trails, got %v", res.MoveStops)
	}
	// a candle touching the trailed stop exits at the stop.
	long.Stop = 100.5
	res = mentorExitB(long, 101.4, 101.5, 100.4, true)
	if !res.Exited || res.ExitPrice != 100.5 || res.ExitReason != "trail" {
		t.Fatalf("trail exit: %+v — want exited at 100.5 trail", res)
	}
	// BE touch (after leg 1's target, never trailed): exits "be" at entry.
	be := mentorPosition{Symbol: "MNQ", Side: "long", Origin: "ISB", Entry: 100, Stop: 100, R: 10, Mode: "B", ArmedBE: true, Scaled: true}
	res = mentorExitB(be, 100.9, 101, 99.9, true)
	if !res.Exited || res.ExitPrice != 100 || res.ExitReason != "be" {
		t.Fatalf("BE exit: %+v — want exited at 100 be", res)
	}
	// trail off (video-8 knob): after leg 1's target the stop NEVER moves.
	off := mentorPosition{Symbol: "MNQ", Side: "long", Origin: "ISB", Entry: 100, Stop: 100, R: 10, Mode: "B", ArmedBE: true, Scaled: true}
	res = mentorExitB(off, 101.5, 102, 101, false)
	if res.Exited || res.NewStop != 100 || len(res.MoveStops) != 0 {
		t.Fatalf("trail off: %+v — want the stop pinned at BE 100", res)
	}
	// same-bar worse before arming (stop + halfR in one candle).
	pre := mentorPosition{Symbol: "MNQ", Side: "long", Entry: 100, Stop: 90, R: 10, Mode: "B"}
	res = mentorExitB(pre, 104, 105, 89, true)
	if !res.Exited || res.ExitPrice != 90 || res.ExitReason != "stop(worse-same-bar)" {
		t.Fatalf("same-bar worse: %+v", res)
	}
	// short side: BE arm at −0.5R for both legs; leg 1 at −1R; runner trails
	// over the high.
	short := mentorPosition{Symbol: "MNQ", Side: "short", Entry: 100, Stop: 110, R: 10, Mode: "B"}
	res = mentorExitB(short, 95, 96, 94.5, true)
	if res.Exited || res.NewStop != 100 || len(res.MoveStops) != 2 {
		t.Fatalf("short arming candle: %+v — want both stops at BE 100", res)
	}
	short.ArmedBE = true
	short.Stop = 100
	res = mentorExitB(short, 90, 96, 89.5, true)
	if res.Exited || !res.Leg1AtTarget {
		t.Fatalf("short leg-1 candle: %+v — want the −1R leg-1 mark", res)
	}
	short.Scaled = true
	res = mentorExitB(short, 98, 99.5, 98, true)
	if res.Exited || res.NewStop != 99.5 || len(res.MoveStops) != 1 || res.MoveStops[0] != "leg2" {
		t.Fatalf("short trail: %+v — want the runner trailed to the closed high 99.5", res)
	}
	// C: the stop never moves, even at +0.5R/+1R/+2R (leg 1's TP is set at
	// entry — mentorLeg1TPForC; nothing modifies it).
	c := mentorPosition{Symbol: "MNQ", Side: "short", Origin: "ISB", Entry: 100, Stop: 112, R: 12, Mode: "C"}
	for i := 0; i < 5; i++ {
		if _, why, exited := mentorExitC(c, 108, 109); exited || why != "" {
			t.Fatalf("C hold: exited=%v why=%q on a non-stop candle", exited, why)
		}
	}
	for _, cc := range [][2]float64{{100, 95}, {95, 90}, {90, 75}} {
		if _, why, exited := mentorExitC(c, cc[0], cc[1]); exited || why != "" {
			t.Fatalf("C must hold through +0.5R/+1R/+2R candles, exited=%v why=%q", exited, why)
		}
	}
	if px, why, exited := mentorExitC(c, 111, 113); !exited || px != 112 || why != "stop" {
		t.Fatalf("C stop: exited=%v px=%.2f why=%q", exited, px, why)
	}
}

// TestMentorBEHalfDistanceTarget (X1, CTO 1791031960407, PLAN Exits B
// D1.2 p1 @10:31–15:20): the B BE trigger is half the distance to the TRADE'S
// target (the intent's target) — +0.5R only when the trade target is 1:1; a
// 2R target arms BE at +1R. Mutant: half of leg 1's +1R TP instead → the
// 2R rows go RED.
func TestMentorBEHalfDistanceTarget(t *testing.T) {
	// trade target 1R (110) → BE at +0.5R.
	base := mentorPosition{Side: "long", Entry: 100, Stop: 90, R: 10, Target: 110, Mode: "B"}
	if got := mentorBEHalfDistance(base); got != 5 {
		t.Fatalf("half of a 1R trade target = 5, got %.2f", got)
	}
	if res := mentorExitB(base, 104, 105, 95, true); res.NewStop != 100 {
		t.Fatalf("1:1 target: BE must arm at +0.5R (105), got stop %.2f", res.NewStop)
	}
	// trade target 2R (120), leg 1 TP still 1R (110): BE arms at +1R (110),
	// NOT at half of leg 1's TP (+0.5R). A +0.9R candle (high 109) must NOT
	// arm — the leg-1-TP mutant arms at 105 and goes RED here.
	deep := mentorPosition{Side: "long", Entry: 100, Stop: 90, R: 10, Target: 120, Leg1TP: 110, Mode: "B"}
	if got := mentorBEHalfDistance(deep); got != 10 {
		t.Fatalf("half of a 2R trade target = 10, got %.2f", got)
	}
	if res := mentorExitB(deep, 104, 107, 95, true); res.NewStop != 90 {
		t.Fatalf("a +0.7R candle must NOT arm BE for a 2R trade target, stop %.2f", res.NewStop)
	}
	if res := mentorExitB(deep, 104, 109, 95, true); res.NewStop != 90 {
		t.Fatalf("a +0.9R candle must NOT arm BE for a 2R trade target (leg 1 TP is NOT the basis), stop %.2f", res.NewStop)
	}
	// +1R (high 110) arms both legs.
	if res := mentorExitB(deep, 106, 110, 95, true); res.NewStop != 100 || len(res.MoveStops) != 2 {
		t.Fatalf("half the distance to the 2R trade target must arm both legs, %+v", res)
	}
	// short mirrored: trade target 80 (2R) → BE at −1R.
	short := mentorPosition{Side: "short", Entry: 100, Stop: 110, R: 10, Target: 80, Leg1TP: 90, Mode: "B"}
	if got := mentorBEHalfDistance(short); got != -10 {
		t.Fatalf("short half-distance to a 2R trade target = -10, got %.2f", got)
	}
	if res := mentorExitB(short, 93, 95, 93.5, true); res.NewStop != 110 {
		t.Fatalf("a -0.45R candle must NOT arm BE for a 2R short trade target, stop %.2f", res.NewStop)
	}
	if res := mentorExitB(short, 92, 95, 89.5, true); res.NewStop != 100 {
		t.Fatalf("half the distance to the 2R short trade target must arm BE, %+v", res)
	}
}

// TestMentorResonanceFork (EXIT-SPEC-v3 A on SPLIT LEGS): a PHL/PLH fill
// followed by an ISB in the SAME direction within 3 candles flips the position
// to A — both stops to BE at that moment and leg 1's TP modified OUT to the
// runner's target (no 1:1 scale-out). Then A NEVER trails (candles that would
// trail in B are ignored); it exits only on a BE touch.
func TestMentorResonanceFork(t *testing.T) {
	ResetMentorCountersForTest()
	pos := &mentorPosition{Symbol: "MNQ", Side: "long", Origin: "PHL", Entry: 100, Stop: 90, Target: 130, R: 10, Leg1TP: 110, Mode: "B"}
	armed, modifyTP, modifyLeg2TP := mentorMaybeArmResonance(pos, "long", 2, 140)
	if !armed {
		t.Fatal("ISB same side within 3 candles of a PHL fill must arm resonance")
	}
	// R-RES (CTO 1791040643329): the resonance STILL takes the 1:1 partial —
	// leg 1's +1R TP STAYS resting (no modify). Mutant: moving leg 1's TP to
	// the runner's target (the old X2) → RED.
	if modifyTP != 0 {
		t.Fatalf("the resonance flip must NOT modify leg 1's TP (R-RES), got modifyTP %.2f", modifyTP)
	}
	// D2-49: the runner's target goes BEYOND the old high (140 > 130).
	if modifyLeg2TP != 140 || pos.Target != 140 {
		t.Fatalf("mode A must move the runner's TP beyond the old high: modifyLeg2TP=%.2f target=%.2f", modifyLeg2TP, pos.Target)
	}
	if pos.Leg1TP != 110 {
		t.Fatalf("after the flip leg 1's +1R TP must be UNCHANGED (110), got %.2f", pos.Leg1TP)
	}
	if pos.Mode != "A-resonance" || !pos.ArmedBE || pos.Stop != pos.Entry {
		t.Fatalf("resonance arm: mode=%s armed=%v stop=%.2f — want A-resonance, BE stop 100", pos.Mode, pos.ArmedBE, pos.Stop)
	}
	if got := MentorCountSnapshot()["resonance_armed"]; got != 1 {
		t.Fatalf("resonance_armed must be counted once, got %d", got)
	}
	// A holds through candles that would trail in B (+1R, +2R highs).
	for _, cc := range [][2]float64{{105, 101}, {110, 101}, {120, 102}} {
		if px, why, exited := mentorExitA(*pos, cc[1], cc[0]); exited || px != 0 || why != "" {
			t.Fatalf("A must hold a %v candle: exited=%v px=%.2f why=%q", cc, exited, px, why)
		}
	}
	// only the BE touch exits, at entry, with the resonance reason.
	if px, why, exited := mentorExitA(*pos, 99.5, 102); !exited || px != 100 || why != "resonance_be" {
		t.Fatalf("A BE exit: exited=%v px=%.2f why=%q — want exited at 100 resonance_be", exited, px, why)
	}
	// refused arms: too late (4th candle), wrong side, non-PHL/PLH origin, the
	// fill bar itself, already-armed.
	if armed, _, _ := mentorMaybeArmResonance(pos, "long", 4, 0); armed {
		t.Fatal("an ISB 4 candles after the fill must NOT arm resonance")
	}
	if armed, _, _ := mentorMaybeArmResonance(&mentorPosition{Origin: "PHL", Mode: "B"}, "short", 2, 0); armed {
		t.Fatal("an ISB against the position must NOT arm resonance")
	}
	if armed, _, _ := mentorMaybeArmResonance(&mentorPosition{Origin: "PHL", Mode: "B"}, "long", 0, 0); armed {
		t.Fatal("the fill bar itself must NOT arm resonance (within 3 candles AFTER the fill)")
	}
	if armed, _, _ := mentorMaybeArmResonance(&mentorPosition{Origin: "ISB", Mode: "B"}, "long", 2, 0); armed {
		t.Fatal("a non-PHL/PLH origin must NOT arm resonance")
	}
	if armed, _, _ := mentorMaybeArmResonance(&mentorPosition{Origin: "PLH", Mode: "A-resonance"}, "long", 2, 0); armed {
		t.Fatal("an already-resonant position must NOT re-arm")
	}
}

// TestMentorTrailTFKnob: the B candle trail runs on 1m (default) and 30s/45s;
// off = the video-8 legacy (superseded); an unknown value refuses to trail
// fail-closed and is counted.
func TestMentorTrailTFKnob(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	if got := at.mentorTrailTF(); got != "1m" {
		t.Fatalf("unset knob → %q, want the 1m default", got)
	}
	for _, tc := range []struct{ in, want string }{
		{"1m", "1m"}, {"30s", "30s"}, {"45s", "45s"}, {"off", "off"}, {" OFF ", "off"},
	} {
		at.config.StrategyConfig.RiskControl.MentorTrailTF = tc.in
		if got := at.mentorTrailTF(); got != tc.want {
			t.Fatalf("trail_tf %q → %q, want %q", tc.in, got, tc.want)
		}
	}
	ResetMentorCountersForTest()
	at.config.StrategyConfig.RiskControl.MentorTrailTF = "garbage"
	if got := at.mentorTrailTF(); got != "off" {
		t.Fatalf("bad trail_tf → %q, want off (fail-closed, no trail)", got)
	}
	if got := MentorCountSnapshot()["trail_tf_bad_value"]; got != 1 {
		t.Fatalf("the bad trail_tf must be counted once, got %d", got)
	}
	if !mentorTrailEnabled("1m") || mentorTrailEnabled("off") {
		t.Fatal("mentorTrailEnabled: 1m must trail, off must not")
	}
}

// TestMentorExitMechSuspensionAppliesToAIOnly: EXIT_MECHS_SUSPENDED (0B)
// suspends the AI mechanisms but never the mentor stop moves — the same last
// hop, two different gates. The dead mentorMoveStop path (rel10) was removed;
// the LIVE mentor stop moves are mentorMoveLegStop (signal-keyed move_stop) and
// mentorMoveStopBE (moveStopWire) — neither calls exitMechSuspendedRefuse, so
// the AI suspension never leaks into the mentor path. This test pins that the
// AI half still refuses when suspended.
func TestMentorExitMechSuspensionAppliesToAIOnly(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	ResetExitMechSuspendNoticeForTest()

	if !at.exitMechSuspendedRefuse("be40", "test trigger") {
		t.Fatal("the AI suspension must STILL apply to the AI mechanisms")
	}
}

// TestMentorRuleGateR8R9: the injector-side R8/R9 gate (RULES-FIX v3).
// SWING4H: 30–60 allowed, ~100 refused, EXEMPT from the 25-pt ceiling.
// R9: target never smaller than stop; spent-day cap 15 skips a stop over 15.
func TestMentorRuleGateR8R9(t *testing.T) {
	if why := mentorRuleGate(mentor.Intent{Setup: "SWING4H", Side: "long", Price: 100, Stop: 60, StopPts: 40, TargetPts: 80}, mentorTierInputs{}); why != "" {
		t.Fatalf("SWING4H 40-pt stop must pass (30–60 allowed): %q", why)
	}
	if why := mentorRuleGate(mentor.Intent{Setup: "SWING4H", Side: "long", Price: 100, Stop: 40, StopPts: 60, TargetPts: 120}, mentorTierInputs{}); why != "" {
		t.Fatalf("SWING4H 60-pt stop must pass: %q", why)
	}
	if why := mentorRuleGate(mentor.Intent{Setup: "SWING4H", Side: "short", Price: 100, Stop: 200, StopPts: 100, TargetPts: 200}, mentorTierInputs{}); why == "" {
		t.Fatal("SWING4H ~100-pt stop must be refused (R8)")
	}
	// the swing is the ONLY exemption from the 25-pt ceiling
	if why := mentorRuleGate(mentor.Intent{Setup: "PHL", Side: "long", Price: 100, Stop: 70, Target: 130, StopPts: 30, TargetPts: 30}, mentorTierInputs{}); why == "" {
		t.Fatal("a non-swing stop over 25 must be refused (R8 ceiling)")
	}
	// R9: target never smaller than stop
	if why := mentorRuleGate(mentor.Intent{Setup: "PHL", Side: "long", Price: 100, Stop: 88, Target: 98, StopPts: 12, TargetPts: 2}, mentorTierInputs{}); why == "" {
		t.Fatal("a target smaller than the stop must be refused (R9)")
	}
	if why := mentorRuleGate(mentor.Intent{Setup: "PHL", Side: "long", Price: 100, Stop: 88, Target: 112, StopPts: 12, TargetPts: 12}, mentorTierInputs{}); why != "" {
		t.Fatalf("target ≥ stop must pass: %q", why)
	}
	// R9 spent-day cap: stop over 15 skips
	if why := mentorRuleGate(mentor.Intent{Setup: "PHL", Side: "long", Price: 100, Stop: 82, Target: 136, StopPts: 18, TargetPts: 36}, mentorTierInputs{SpentDay: true}); why == "" {
		t.Fatal("a spent day must skip any stop over 15 (R9 cap)")
	}
	if why := mentorRuleGate(mentor.Intent{Setup: "PHL", Side: "long", Price: 100, Stop: 88, Target: 124, StopPts: 12, TargetPts: 24}, mentorTierInputs{SpentDay: true}); why != "" {
		t.Fatalf("a spent day with a 12-pt stop must pass: %q", why)
	}
	// R13: DayOff does not stop the swing, so the spent-day cap must not
	// either — a SWING4H with a >15-pt stop trades through a spent day.
	if why := mentorRuleGate(mentor.Intent{Setup: "SWING4H", Side: "long", Price: 100, Stop: 60, StopPts: 40, TargetPts: 80}, mentorTierInputs{SpentDay: true}); why != "" {
		t.Fatalf("a spent day must NOT refuse the swing (R13): %q", why)
	}
	// a targetless intraday entry is BAD GEOMETRY now (CTO 1791058631982) —
	// the kernel always tags the ISB target, so a zero target is a bug
	if why := mentorRuleGate(mentor.Intent{Setup: "ISB", Side: "long", Price: 100, Stop: 94, StopPts: 5.75}, mentorTierInputs{}); why == "" || !strings.HasPrefix(why, "bad geometry") {
		t.Fatalf("a targetless ISB must be refused as bad geometry, got %q", why)
	}
}

// TestMentorRuleGateBadGeometry (CTO 1791058631982): before any sizing, every
// non-swing entry must have Stop > 0, Target > 0 and the right sides (long:
// stop < entry < target; short: target < entry < stop); the swing needs the
// stop side only (no fixed target). The mutant (gate dropped) turns the
// Target-0 pin RED — with the old fallback it would size BIG (20).
func TestMentorRuleGateBadGeometry(t *testing.T) {
	// a Target-0 confluence intent is refused, never big
	if why := mentorRuleGate(mentor.Intent{Setup: "PHL", Side: "long", Price: 100, Stop: 90, Target: 0}, mentorTierInputs{Confluence: true}); why == "" || !strings.HasPrefix(why, "bad geometry") {
		t.Fatalf("a Target-0 confluence intent must be refused as bad geometry, got %q", why)
	}
	// an inverted long is refused
	if why := mentorRuleGate(mentor.Intent{Setup: "PHL", Side: "long", Price: 100, Stop: 110, Target: 120}, mentorTierInputs{}); why == "" || !strings.HasPrefix(why, "bad geometry") {
		t.Fatalf("an inverted long must be refused, got %q", why)
	}
	// a valid long, a valid short and a valid targetless swing still pass
	if why := mentorRuleGate(mentor.Intent{Setup: "PHL", Side: "long", Price: 100, Stop: 90, Target: 120}, mentorTierInputs{}); why != "" {
		t.Fatalf("a valid long must pass: %q", why)
	}
	if why := mentorRuleGate(mentor.Intent{Setup: "PHL", Side: "short", Price: 100, Stop: 110, Target: 80}, mentorTierInputs{}); why != "" {
		t.Fatalf("a valid short must pass: %q", why)
	}
	if why := mentorRuleGate(mentor.Intent{Setup: "SWING4H", Side: "long", Price: 100, Stop: 60}, mentorTierInputs{}); why != "" {
		t.Fatalf("a targetless swing with a correct stop side must pass: %q", why)
	}
	if why := mentorRuleGate(mentor.Intent{Setup: "SWING4H", Side: "long", Price: 100, Stop: 110}, mentorTierInputs{}); why == "" || !strings.HasPrefix(why, "bad geometry") {
		t.Fatalf("a swing with the stop on the wrong side must be refused, got %q", why)
	}
}

// TestMentorRuleGateDefenceInDepth (CTO 1791058442006): the gate reads the
// GEOMETRY (abs(Price−Stop), abs(Target−Price)), never a bare intent field an
// emit site forgot to set, and an entry with no Setup tag is refused
// fail-closed with the reason "untagged setup". The mutants (bare fields, or
// the refusal dropped) turn RED here.
func TestMentorRuleGateDefenceInDepth(t *testing.T) {
	if why := mentorRuleGate(mentor.Intent{Price: 100, Stop: 90, Target: 120}, mentorTierInputs{}); why == "" || !strings.HasPrefix(why, "untagged setup") {
		t.Fatalf("an untagged setup must be refused with 'untagged setup', got %q", why)
	}
	if why := mentorRuleGate(mentor.Intent{Setup: "PHL", Side: "long", Price: 100, Stop: 78, Target: 130}, mentorTierInputs{}); why != "" {
		t.Fatalf("geometry stop 22 / target 30 must pass: %q", why)
	}
	if why := mentorRuleGate(mentor.Intent{Setup: "PHL", Side: "long", Price: 100, Stop: 88, Target: 101}, mentorTierInputs{}); why == "" || !strings.HasPrefix(why, "R9") {
		t.Fatalf("geometry target 1 < stop 12 must be refused R9, got %q", why)
	}
	if why := mentorRuleGate(mentor.Intent{Setup: "PHL", Side: "long", Price: 100, Stop: 70, Target: 140}, mentorTierInputs{}); why == "" || !strings.HasPrefix(why, "R8") {
		t.Fatalf("geometry stop 30 must hit the R8 ceiling, got %q", why)
	}
}

// TestMentorSizeForUsesGeometry (CTO 1791058442006): the tier inputs are the
// geometry, never bare intent fields — a zero-StopPts/TargetPts intent still
// sizes from Price/Stop/Target. The mutant (bare in.StopPts/in.TargetPts)
// sizes this as base 5 instead of the twenties cut → RED.
func TestMentorSizeForUsesGeometry(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	in := mentor.Intent{Setup: "PHL", Price: 100, Stop: 78, Target: 130} // stop 22, target 30 — the twenties cut
	choice, err := at.mentorSizeFor(in, mentorTierInputs{})
	if err != nil {
		t.Fatal(err)
	}
	if choice.Contracts != 3 || choice.Tier != "reduced" {
		t.Fatalf("geometry sizing = %+v, want reduced 3 (a 22-pt stop, no confluence)", choice)
	}
}

// TestMentorBars1mDepthP0A6 (CTO 1791058624275): both mentor 1m call sites
// ask 1500 bars — enough for the §7 Globex window (17:00→08:30 CT, 930 bars)
// plus the RTH day to 15:00 CT (1320 total). The old ask started mid-window:
// past the 08:30 freeze the run window holds ZERO of its bars and the day
// gate reads DayNotMeasured every day (pinned here via GlobexRun on the same
// tape). The mutant — putting the old depth back — turns RED on the ask pin.
func TestMentorBars1mDepthP0A6(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	old := market.FuturesBarsProvider
	gotN := 0
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline {
		if symbol == "MNQ" && tf == "1m" {
			gotN = n
		}
		return nil
	}
	t.Cleanup(func() { market.FuturesBarsProvider = old })
	at.mentorTick(nil)
	if gotN != 1500 {
		t.Fatalf("mentorTick asks %d 1m bars, want 1500", gotN)
	}
	gotN = 0
	at.mentorEventPassAt(time.Now())
	if gotN != 1500 {
		t.Fatalf("mentorEventPassAt asks %d 1m bars, want 1500", gotN)
	}

	// The window the depth must cover: a full tape 17:00 CT → 15:00 CT the
	// next day (1320 bars). The full tape measures the Globex run; the last
	// 240 bars (the old ask) hold no window bar past the freeze → not
	// measured.
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		loc = time.FixedZone("CST6", -6*3600)
	}
	day := time.Date(2026, 9, 14, 17, 0, 0, 0, loc)
	bars := make([]market.Kline, 0, 1320)
	for i := 0; i < 1320; i++ {
		ot := day.Add(time.Duration(i) * time.Minute).UnixMilli()
		h, l := 20000.0, 19990.0
		if i%2 == 0 {
			h = 20350.0
		} else {
			l = 19950.0
		}
		bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, High: h, Low: l})
	}
	now := bars[len(bars)-1].CloseTime // 15:00 CT — the frozen 17:00→08:30 window applies
	if run, ok := mentor.GlobexRun(bars, now, nil); !ok || run <= 0 {
		t.Fatalf("the full 1320-bar tape must measure the Globex run, got ok=%v run=%.1f", ok, run)
	}
	if _, ok := mentor.GlobexRun(bars[len(bars)-240:], now, nil); ok {
		t.Fatal("the old depth must read the day as not-measured past the freeze (zero window bars)")
	}
}

// TestMentorSpentDayClamp: §7 — at most 2 contracts running on a spent day.
func TestMentorSpentDayClamp(t *testing.T) {
	if got := mentorSpentDayClamp(20, 2); got != 2 {
		t.Fatalf("spent-day clamp 20->%d, want 2", got)
	}
	if got := mentorSpentDayClamp(1, 2); got != 1 {
		t.Fatalf("spent-day clamp 1->%d, want 1 (never up-size)", got)
	}
}

// TestMentorSizeForCountsAndLogsTier: the AutoTrader entry point counts the
// tier ("which tier and why") — a silent size choice fails this.
func TestMentorSizeForCountsAndLogsTier(t *testing.T) {
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12}
	choice, err := at.mentorSizeFor(in, mentorTierInputs{})
	if err != nil {
		t.Fatal(err)
	}
	if choice.Contracts != mentorBaseContractsDefault || choice.Tier != "base" {
		t.Fatalf("choice = %+v", choice)
	}
	if got := MentorCountSnapshot()["tier_base"]; got != 1 {
		t.Fatalf("the tier must be counted once, got %d", got)
	}
	if choice.Why == "" {
		t.Fatal("the tier must carry its why")
	}
}

// TestMentorExitFork pins the A/B/C fork AT ENTRY (CTO 1791029620038): one
// branch per case. Mutants: confluence ignored in the fork (C case RED),
// mentorLeg1TPForC dropped (C leg-1 TP RED).
func TestMentorExitFork(t *testing.T) {
	cases := []struct {
		name       string
		in         mentor.Intent
		confluence bool
		wantMode   string
		wantTP     float64
	}{
		{"swing holds by the 4h", mentor.Intent{Setup: "SWING4H", Side: mentor.SideLong, Price: 100, Stop: 90}, false, "swing", 0},
		{"normal B ISB", mentor.Intent{Setup: "ISB", Side: mentor.SideShort, Price: 100, Stop: 106, StopPts: 6}, false, "B", 0},
		{"PHL starts as B with the resonance watch", mentor.Intent{Setup: "PHL", Side: mentor.SideLong, Price: 100, Stop: 95, StopPts: 5}, false, "B", 0},
		{"C confluence long holds 1:2", mentor.Intent{Setup: "PHL", Side: mentor.SideLong, Price: 100, Stop: 95, StopPts: 5}, true, "C", 110},
		{"C confluence short holds 1:2", mentor.Intent{Setup: "PLH", Side: mentor.SideShort, Price: 100, Stop: 105, StopPts: 5}, true, "C", 90},
		{"C geometry risk when StopPts unset", mentor.Intent{Setup: "ISB", Side: mentor.SideLong, Price: 100, Stop: 92}, true, "C", 116},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, tp, why := mentorExitFork(tc.in, tc.confluence)
			if mode != tc.wantMode || tp != tc.wantTP {
				t.Fatalf("fork = (%s, %.2f) want (%s, %.2f); why=%q", mode, tp, tc.wantMode, tc.wantTP, why)
			}
		})
	}

	// the A branch: a PHL fill starting as B flips to A on a same-direction
	// ISB within 3 candles — the REST's stop to BE, no trail, leg 1's +1R TP
	// unchanged (R-RES).
	pos := &mentorPosition{
		Origin: "PHL", Side: "long", Entry: 100, Stop: 95, Target: 112, R: 5,
		Mode: "B", Leg1TP: 105,
	}
	armed, modTP, modLeg2 := mentorMaybeArmResonance(pos, "long", 2, 120)
	if !armed || pos.Mode != "A-resonance" || pos.Stop != pos.Entry || modTP != 0 || pos.Leg1TP != 105 || modLeg2 != 120 || pos.Target != 120 {
		t.Fatalf("the A flip must arm and move the runner beyond the old high: armed=%v mode=%s stop=%.2f modTP=%.2f leg1TP=%.2f modLeg2=%.2f target=%.2f", armed, pos.Mode, pos.Stop, modTP, pos.Leg1TP, modLeg2, pos.Target)
	}
}

// TestMentorExitForkAtPlacement pins the fork WIRED at the placement call
// site: the fork is computed before the recorder seam, so the branch counter
// is observable. Mutant: the fork call removed from mentorPlaceIntent → the
// counter stays 0 and this test goes RED.
func TestMentorExitForkAtPlacement(t *testing.T) {
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireMentorPlacementSeams(t)
	ct := time.FixedZone("CT", -5*3600)
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, ct) }
	t.Cleanup(func() { mentorNowSource = nil })
	mentorLatestPriceSource = func() (float64, bool) { return 99, true }
	t.Cleanup(func() { mentorLatestPriceSource = nil })
	placed := 0
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed++ }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	// C: the stub flag is set — the fork must land on C.
	mentorConfluenceForIntent = func(in mentor.Intent) bool { return true }
	t.Cleanup(func() { mentorConfluenceForIntent = nil })
	at.mentorPlaceIntent(mentor.Intent{
		Action: mentor.PlaceStopEntry, Setup: "PHL", Side: mentor.SideLong,
		Price: 100, Stop: 95, Target: 112, StopPts: 5,
	}, mentorSizeChoice{Contracts: 10, Tier: "confluence"}, 0, 0)
	if placed != 1 {
		t.Fatalf("placement recorder must fire once, got %d", placed)
	}
	if got := MentorCountSnapshot()["exit_fork_C"]; got != 1 {
		t.Fatalf("exit_fork_C counter = %d, want 1", got)
	}

	// B: the seam stays bound but returns false → a normal ISB lands on B.
	mentorConfluenceForIntent = func(in mentor.Intent) bool { return false }
	mentorLatestPriceSource = func() (float64, bool) { return 101, true }
	at.mentorPlaceIntent(mentor.Intent{
		Action: mentor.PlaceStopEntry, Setup: "ISB", Side: mentor.SideShort,
		Price: 100, Stop: 106, Target: 88, StopPts: 6,
	}, mentorSizeChoice{Contracts: 5, Tier: "base"}, 0, 0)
	if got := MentorCountSnapshot()["exit_fork_B"]; got != 1 {
		t.Fatalf("exit_fork_B counter = %d, want 1", got)
	}
}

// TestMentorKnobRoutingDefaults pins the five knob-routing resolvers (CTO
// 1791033257041) at their ruled defaults and the evaluator config wiring.
// Mutant: any default flipped (e.g. leg budget false) → RED.
func TestMentorKnobRoutingDefaults(t *testing.T) {
	if !mentorLegBudgetEnabled(nil) || !mentorLocationTriggerFilter(nil) {
		t.Fatal("leg budget and the location trigger filter default ON")
	}
	if v := mentorLegResetOn(nil); v != "close" {
		t.Fatalf("leg reset default = %q, want close", v)
	}
	if v := mentorLvlRevisitMinPts(nil); v != 0 {
		t.Fatalf("lvl revisit default = %.2f, want 0", v)
	}
	if v := mentorEmaMaxCross30m(nil); v != 2 {
		t.Fatalf("ema max cross default = %d, want 2 (ON, most conservative — item 16)", v)
	}
	if v := mentorLossDeparturePts(nil); v != 0 {
		t.Fatalf("loss departure fallback default = %.2f, want 0 (OFF)", v)
	}

	f := false
	rc := &store.RiskControlConfig{
		MentorLegBudgetEnabled:      &f,
		MentorLegResetOn:            "touch",
		MentorLvlRevisitMinPts:      3,
		MentorEmaMaxCross30m:        4,
		MentorLocationTriggerFilter: &f,
		MentorLossDeparturePts:      25,
	}
	if mentorLegBudgetEnabled(rc) || mentorLocationTriggerFilter(rc) {
		t.Fatal("explicit false must turn the ON-default knobs OFF")
	}
	if v := mentorLegResetOn(rc); v != "touch" {
		t.Fatalf("leg reset = %q, want touch", v)
	}
	if v := mentorLvlRevisitMinPts(rc); v != 3 {
		t.Fatalf("lvl revisit = %.2f, want 3", v)
	}
	if v := mentorEmaMaxCross30m(rc); v != 4 {
		t.Fatalf("ema max cross = %d, want 4", v)
	}
	if v := mentorLossDeparturePts(rc); v != 25 {
		t.Fatalf("loss departure = %.2f, want 25", v)
	}
	if v := mentorLossDeparturePts(&store.RiskControlConfig{MentorLossDeparturePts: 0}); v != 0 {
		t.Fatalf("an unset departure fallback must stay OFF, got %.2f", v)
	}
	if v := mentorLossDeparturePts(&store.RiskControlConfig{MentorLossDeparturePts: -1}); v != 0 {
		t.Fatalf("a non-positive departure fallback must stay OFF, got %.2f", v)
	}

	// a bad leg reset value fails closed to the default and is counted.
	ResetMentorCountersForTest()
	bad := &store.RiskControlConfig{MentorLegResetOn: "garbage"}
	if v := mentorLegResetOn(bad); v != "close" {
		t.Fatalf("bad leg reset = %q, want close (fail closed)", v)
	}
	if got := MentorCountSnapshot()["leg_reset_on_bad_value"]; got != 1 {
		t.Fatalf("leg_reset_on_bad_value counter = %d, want 1", got)
	}

	// The evaluator config builder carries explicit strategy knobs, including
	// B22's opt-in numeric fallback.
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true,
		MentorLvlRevisitMinPts: 3, MentorEmaMaxCross30m: 4, MentorLossDeparturePts: 25})
	cfg := at.mentorEvaluatorConfig()
	if !cfg.Enabled || cfg.LvlRevisitMinPts != 3 || cfg.EmaMaxCross30m != 4 || cfg.LossDeparturePts != 25 {
		t.Fatalf("evaluator config: enabled=%v revisit=%.2f cross=%d loss_departure=%.2f — want true/3/4/25",
			cfg.Enabled, cfg.LvlRevisitMinPts, cfg.EmaMaxCross30m, cfg.LossDeparturePts)
	}
	naked := (&AutoTrader{id: "t-naked"}).mentorEvaluatorConfig()
	if !naked.Enabled || naked.LvlRevisitMinPts != 0 || naked.EmaMaxCross30m != 2 || naked.LossDeparturePts != 0 {
		t.Fatalf("no strategy → evaluator defaults: enabled=%v revisit=%.2f cross=%d loss_departure=%.2f — want true/0/2/0 (cross default ON, item 16)",
			naked.Enabled, naked.LvlRevisitMinPts, naked.EmaMaxCross30m, naked.LossDeparturePts)
	}
}

// TestMentorExpiryGuard pins the F3 fail-closed guard (CTO 1791035117415): a
// mentor arm without an expiry is refused with a named reason. Mutant: the
// guard inverted (a zero expiry passes) → RED.
func TestMentorExpiryGuard(t *testing.T) {
	if refuse, why := mentorExpiryGuard(0); !refuse || !textHas(why, "without an expiry") {
		t.Fatalf("a zero expiry must refuse with a named reason: refuse=%v why=%q", refuse, why)
	}
	if refuse, _ := mentorExpiryGuard(-1); !refuse {
		t.Fatal("a negative expiry must refuse")
	}
	if refuse, why := mentorExpiryGuard(60_000); refuse || why != "" {
		t.Fatalf("a positive expiry must pass: refuse=%v why=%q", refuse, why)
	}
}
