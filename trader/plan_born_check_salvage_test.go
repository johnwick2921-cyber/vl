package trader

import (
	"encoding/json"
	"testing"

	"vl/kernel"
)

// FIX-PLANNER (2026-09-25) item 1 — scenario-level born-dead salvage at the
// production call site (runPlannerReadCoreObserved → validateAuthoredScenariosAt).
// The breach fixture is the same row455/row452 tape as plan_born_check_writesite_test.go;
// "5m close above 31085.00" / "2x5m close above 31080.00" are PROVEN breached
// between read 01:30:27 and publish 01:51:47 (TestW2A2RefusesOnlyWithTheReadClock),
// and the conformant sentences above 31095/31100 are proven clean.

// RED (before the fix): this read is refused born-dead on attempt 1 and burns
// attempts — the surviving scenarios must publish instead, with S1 dropped.
func TestFpSalvageDropsBornDeadScenarioAndPublishesTheRest(t *testing.T) {
	_, bars, read, publish := loadW2WriteFixture(t)
	at := w2Trader(t, bars)
	cand := w2Candidate(t, []string{
		"5m close above 31085.00",   // breached between read and publish → born dead
		"5m close above 31095.00",   // clean
		"2x5m close above 31100.00", // clean
	}, nil)
	ver, lc, err, prompts := w2Run(at, "LONDON", "2026-09-23", read, publish, cand)
	if err != nil || ver != 1 || lc != "active" || len(prompts) != 1 {
		t.Fatalf("salvage must publish on attempt 1: ver=%d lc=%s calls=%d err=%v", ver, lc, len(prompts), err)
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-09-23", "LONDON")
	if row == nil {
		t.Fatal("no published row")
	}
	var pub kernel.PlanDoc
	if json.Unmarshal([]byte(row.Doc), &pub) != nil {
		t.Fatalf("row doc unparsable: %.120s", row.Doc)
	}
	ids := map[string]bool{}
	for _, s := range pub.Scenarios {
		ids[s.ID] = true
	}
	if len(pub.Scenarios) != 2 || ids["S1"] || !ids["S2"] || !ids["S3"] {
		t.Fatalf("S1 must be dropped and S2/S3 published: got %d scenarios %v", len(pub.Scenarios), ids)
	}
	if c, _ := at.store.PlanLivenessCounts(); c.BornDeadRefusals != 0 {
		t.Fatalf("a salvaged read is NOT a whole-read refusal: %+v", c)
	}
	if row.BornCheck == nil {
		t.Fatal("the born check record must still be stored")
	}
	var bc kernel.BornCheck
	if json.Unmarshal([]byte(*row.BornCheck), &bc) != nil {
		t.Fatalf("born check unparsable: %s", *row.BornCheck)
	}
	s1Judged := false
	for _, v := range bc.Verdicts {
		if v.Subject == "S1" && v.Outcome == "invalidated" {
			s1Judged = true
		}
	}
	if !s1Judged {
		t.Fatalf("the record must show S1 was judged invalidated: %s", *row.BornCheck)
	}
	// The 💀 drop is RECORDED, one event per dropped scenario.
	if c, _ := at.store.PlanLivenessCounts(); c.BornDeadDropped != 1 {
		t.Fatalf("exactly one born-dead dropped event must be recorded: %+v", c)
	}
}

// RED (before the fix): both scenarios die, attempt 2 dies again — today that
// still burns attempt 3. The fix: ONE fresh-tape repair, then fail-closed.
func TestFpAllDeadDiesAndBurnsOnlyOneFreshTapeRepair(t *testing.T) {
	_, bars, read, publish := loadW2WriteFixture(t)
	at := w2Trader(t, bars)
	dead := w2Candidate(t, []string{"5m close above 31085.00", "2x5m close above 31080.00"}, nil)
	_, lc, err, prompts := w2Run(at, "LONDON", "2026-09-23", read, publish, dead, dead)
	if err != nil || lc != "no_trade" || len(prompts) != 2 {
		t.Fatalf("all-dead read must die after ONE fresh-tape repair: lc=%s calls=%d err=%v", lc, len(prompts), err)
	}
	if c, _ := at.store.PlanLivenessCounts(); c.BornDeadRefusals != 2 {
		t.Fatalf("one counted refusal per burned attempt: %+v", c)
	}
}

// The attempt-3 skip rides the SAME planner_fresh_tape knob as A6: OFF
// reproduces today's blind retry byte-identically (three calls, three refusals).
func TestFpFreshTapeOffBurnsAttempt3AsToday(t *testing.T) {
	_, bars, read, publish := loadW2WriteFixture(t)
	at := w2Trader(t, bars)
	off := false
	at.config.StrategyConfig.DayPlan.PlannerFreshTape = &off
	dead := w2Candidate(t, []string{"5m close above 31085.00", "2x5m close above 31080.00"}, nil)
	_, lc, _, prompts := w2Run(at, "LONDON", "2026-09-23", read, publish, dead, dead)
	if lc != "no_trade" || len(prompts) != 3 {
		t.Fatalf("knob OFF must reproduce today's blind retry: lc=%s calls=%d", lc, len(prompts))
	}
}

// fpSalvageNoPublish asserts the whole-read-refusal outcome the three safety
// guards share: fail-closed, only the machine S0 no-trade marker present,
// nothing dropped/recorded as salvage.
func fpSalvageNoPublish(t *testing.T, at *AutoTrader, lc string) {
	t.Helper()
	if lc != "no_trade" {
		t.Fatalf("must fail-closed, got lifecycle %q", lc)
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-09-23", "LONDON")
	if row == nil {
		t.Fatal("no row written")
	}
	var pub kernel.PlanDoc
	if json.Unmarshal([]byte(row.Doc), &pub) != nil {
		t.Fatalf("row doc unparsable: %.120s", row.Doc)
	}
	for _, s := range pub.Scenarios {
		if s.ID != "S0" { // the fail-closed doc's machine marker (NoTradePlanDoc)
			t.Fatalf("no authored scenario may publish: row carries %s", s.ID)
		}
	}
	if c, _ := at.store.PlanLivenessCounts(); c.BornDeadDropped != 0 {
		t.Fatalf("no scenario may be dropped/recorded as salvage on a whole-read refusal: %+v", c)
	}
}

// fpMutantGuardTargets — one born-dead scenario (S1, breached between read and
// publish) AND one surviving scenario (S2): the exact shape the salvage would
// otherwise publish. Each guard test FAILS under its mutant in the salvage
// condition (see the CTO mutation run).
func fpMutantGuardTargets() []string {
	return []string{"5m close above 31085.00", "5m close above 31095.00"}
}

// (a) death{} met → whole-read refusal; RED under the mutant that removes
// `&& r.Death == nil` (the salvage would then publish the dead-thesis plan).
func TestFpSalvageNeverPublishesDeathMet(t *testing.T) {
	_, bars, read, publish := loadW2WriteFixture(t)
	at := w2Trader(t, bars)
	cand := w2Candidate(t, fpMutantGuardTargets(), func(m map[string]any) {
		m["death"] = map[string]any{"price": 31085.0, "side": "above", "rule": "5m_close"}
		m["death_condition"] = "5m close above 31085 ends the plan"
	})
	_, lc, err, prompts := w2Run(at, "LONDON", "2026-09-23", read, publish, cand, cand)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prompts) != 2 {
		t.Fatalf("death-met read dies after the one fresh-tape repair: calls=%d", len(prompts))
	}
	fpSalvageNoPublish(t, at, lc)
}

// (b) flip{} fired → whole-read refusal; RED under the mutant that removes
// `&& r.Flip == nil`. Parse-stage contracts: flip{price} must match a number in
// bias.flip_condition prose AND obey the flip-direction law — a SHORT bias
// flips to long only on a close ABOVE the line (which the fixture tape
// breaches), so the fixture uses a short bias.
func TestFpSalvageNeverPublishesFlipFired(t *testing.T) {
	_, bars, read, publish := loadW2WriteFixture(t)
	at := w2Trader(t, bars)
	cand := w2Candidate(t, fpMutantGuardTargets(), func(m map[string]any) {
		m["flip"] = map[string]any{"price": 31085.0, "side": "above", "rule": "5m_close"}
		b := m["bias"].(map[string]any)
		b["direction"] = "short"
		b["flip_condition"] = "5m close above 31085 → long"
	})
	_, lc, err, prompts := w2Run(at, "LONDON", "2026-09-23", read, publish, cand, cand)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prompts) != 2 {
		t.Fatalf("flip-fired read dies after the one fresh-tape repair: calls=%d", len(prompts))
	}
	fpSalvageNoPublish(t, at, lc)
}

// (c) a grammar refusal present beside a born-dead and a surviving scenario →
// whole-read refusal; RED under the mutant that removes `len(r.Grammar) == 0 &&`
// (the salvage would then publish the out-of-grammar sentence). The one-repair
// cap still applies (the check also carries the born-dead verdict), so the read
// dies after 2 calls.
func TestFpSalvageNeverPublishesGrammarRefusal(t *testing.T) {
	_, bars, read, publish := loadW2WriteFixture(t)
	at := w2Trader(t, bars)
	cand := w2Candidate(t, []string{
		"1m close below 31000.00", // outside the invalidation grammar → refusal
		"5m close above 31085.00", // born dead between read and publish
		"5m close above 31095.00", // survives
	}, nil)
	_, lc, err, prompts := w2Run(at, "LONDON", "2026-09-23", read, publish, cand, cand)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prompts) != 2 {
		t.Fatalf("the read dies after the one fresh-tape repair (born-dead class in the check): calls=%d", len(prompts))
	}
	fpSalvageNoPublish(t, at, lc)
}
