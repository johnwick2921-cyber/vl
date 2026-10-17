package kernel

import (
	"os"
	"strings"
	"testing"
)

// W-EXEC-TRUTH W3 (h) — the planner prompt follows the resolved
// entry_policy_default, rendered through BuildPlannerPrompt (the production
// builder; trader/auto_trader_planner.go fills the three PlannerInput fields
// from the store resolvers).

// LEGACY byte-identity: "legacy" renders the pre-W3 prompt exactly — the
// golden is the knob_prune planner prompt as it stood at the W3 base
// (e74fce17), copied before any W3 prompt edit.
func TestW3PlannerPromptLegacyPolicyByteIdentical(t *testing.T) {
	want, err := os.ReadFile("testdata/knob_prune/planner_prompt_legacy_policy.txt")
	if err != nil {
		t.Fatal(err)
	}
	got := BuildPlannerPrompt(PlannerInput{MaxLevels: 8, ScenarioCap: 3, EntryPolicyDefault: EntryPolicyDefaultLegacy})
	if got != string(want) {
		t.Fatalf("entry_policy_default=legacy must render the pre-W3 prompt byte-identically:\n%s", firstDiff(string(want), got))
	}
}

// The ZERO PlannerInput renders the SHIPPED default (market_in_zone, 10 pt,
// 3 min) — what production renders for a strategy that saved nothing.
func TestW3PlannerPromptZeroValueIsTheShippedDefault(t *testing.T) {
	zero := BuildPlannerPrompt(PlannerInput{MaxLevels: 8, ScenarioCap: 3})
	explicit := BuildPlannerPrompt(PlannerInput{MaxLevels: 8, ScenarioCap: 3, EntryPolicyDefault: EntryPolicyMarketInZone, ZoneMaxPts: 10, MinHoldMin: 3})
	if zero != explicit {
		t.Fatalf("the zero value must render market_in_zone/10/3:\n%s", firstDiff(explicit, zero))
	}
	if !strings.Contains(zero, EntryPolicyPromptMarker) {
		t.Fatal("the shipped default must render the ENTRY POLICY sentence")
	}
	// The knob values are READ into the sentence.
	p := BuildPlannerPrompt(PlannerInput{MaxLevels: 8, ScenarioCap: 3, ZoneMaxPts: 6.5, MinHoldMin: 4})
	for _, frag := range []string{"be at most 6.5 points wide", "an armed time_hold holds at least 4 minutes"} {
		if !strings.Contains(p, frag) {
			t.Errorf("resolved knob not rendered: %q", frag)
		}
	}
}

// The two deleted sentences are gone under market_in_zone, and every sibling
// that said the old law says the new one.
func TestW3PlannerPromptMarketInZoneDeletesTheOldLaw(t *testing.T) {
	p := BuildPlannerPrompt(PlannerInput{MaxLevels: 8, ScenarioCap: 3, EntryPolicyDefault: EntryPolicyMarketInZone})
	for _, gone := range []string{
		"a RESTING ORDER IS THE ONLY WAY INTO THE MARKET",
		"entry_mode=immediate is AI-path ONLY",
		"breakout_retest stays a normal AI play",
		"immediate-mode waterfall plays stay on the AI path",
		"OMIT arm{} and let the AI path take it",
		"NEVER arm acceptance or a raw sweep",
		"NOT armable at all",
		"reclaim→stop_entry",
		"hold|acceptance|breakout_retest NEVER arm",
	} {
		if strings.Contains(p, gone) {
			t.Errorf("market_in_zone prompt still says %q", gone)
		}
	}
	for _, kept := range []string{
		"ARMS FOLLOW THE BIAS: Every scenario in the plan's bias direction",
		"a LIMIT at the FAR edge of your economics.entry_zone",
		"entry_mode=immediate ARMS under the ENTRY POLICY",
		"must arm SINGLE",
	} {
		if !strings.Contains(p, kept) {
			t.Errorf("market_in_zone prompt missing %q", kept)
		}
	}
	// Exactly ONE entry-policy sentence.
	if n := strings.Count(p, "ENTRY POLICY ("); n != 1 {
		t.Fatalf("want ONE ENTRY POLICY sentence, got %d", n)
	}
}

// planned_order = the legacy text + its one sentence (the policy only stamps
// where legacy already arms).
func TestW3PlannerPromptPlannedOrderIsLegacyPlusOneSentence(t *testing.T) {
	legacy := BuildPlannerPrompt(PlannerInput{MaxLevels: 8, ScenarioCap: 3, EntryPolicyDefault: EntryPolicyDefaultLegacy})
	po := BuildPlannerPrompt(PlannerInput{MaxLevels: 8, ScenarioCap: 3, EntryPolicyDefault: EntryPolicyPlannedOrder})
	sentence := resolvePromptEntryPolicy(EntryPolicyPlannedOrder, 0, 0).entryPolicySentence()
	if sentence == "" || strings.Count(po, sentence) != 1 {
		t.Fatalf("planned_order must render its sentence once")
	}
	if strings.Replace(po, sentence, "", 1) != legacy {
		t.Fatal("planned_order minus its sentence must equal the legacy prompt")
	}
}

// Class 38: the contract holds under EVERY policy's rendering, and the boot
// guard judges all three.
func TestW3PromptContractsHoldUnderEveryPolicy(t *testing.T) {
	for _, pol := range []string{"", EntryPolicyMarketInZone, EntryPolicyPlannedOrder, EntryPolicyDefaultLegacy} {
		for _, wf := range []bool{false, true} {
			p := BuildPlannerPrompt(PlannerInput{MaxLevels: 8, ScenarioCap: 3, EntryPolicyDefault: pol, WriteFeasibilityOn: wf})
			if err := ValidatePromptContracts(p); err != nil {
				t.Errorf("policy %q writeFeas=%v: %v", pol, wf, err)
			}
		}
	}
	if line := PromptContractBootLine(); !strings.Contains(line, "all stated in prompt") {
		t.Fatalf("boot guard: %s", line)
	}
}

// RED: a prompt that carries the ENTRY POLICY marker but not its law fails the
// guard (the Gate is live), and a market_in_zone prompt that still carried the
// deleted legacy sentence would not rescue a missing ENTRY POLICY fragment.
func TestW3EntryPolicyRowHasTeeth(t *testing.T) {
	legacy := plannerOutputContract(8, 3, true, true, true)
	if err := ValidatePromptContracts(legacy + " " + EntryPolicyPromptMarker); err == nil {
		t.Fatal("the marker without the market_in_zone law must fail the class-38 guard")
	}
	miz := plannerOutputContractFor(8, 3, true, true, true, resolvePromptEntryPolicy(EntryPolicyMarketInZone, 0, 0))
	for _, frag := range []string{"the zone must contain arm.entry", "an armed time_hold holds at least", "entry_mode=pullback or entry_mode=immediate"} {
		if err := ValidatePromptContracts(strings.ReplaceAll(miz, frag, "")); err == nil {
			t.Errorf("dropping %q from the market_in_zone prompt must fail the guard", frag)
		}
	}
}
