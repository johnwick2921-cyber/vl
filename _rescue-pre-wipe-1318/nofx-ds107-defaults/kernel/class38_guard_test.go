package kernel

import (
	"strings"
	"testing"
)

// CLASS 38 guards (2026-09-01):
//   F4 — the class-34 hint guard, extended from CONDITION tokens to EVERY
//        enum-valued token, checked against the legal set for ITS OWN field.
//        Row 78's hint ("2x5m legal ONLY here") passed the class-34 guard
//        because 2x5m is not a condition token; it is a death/flip rule token
//        named inside a confirm-field instruction.
//   F5 — the contract test: a validator restriction keyed by condition/field
//        MUST be stated in the rendered prompt. "Validator forbids X" without
//        "prompt says X is forbidden" fails the build.

// TestClass38HintGuardCatchesOutOfEnumToken is E2: the guard must reject the
// EXACT pre-fix text that produced rows 78 and 79, and must accept its fixed
// form. This is the regression that keeps the token vocabulary single.
func TestClass38HintGuardCatchesOutOfEnumToken(t *testing.T) {
	preFix := ValidatorHint{
		Site:      "entry_law.go breakdown_continue (pre-class-38 text)",
		Text:      "1 confirming close + displacement ≥ BD_MIN_DISP_ATR×ATR5m OR stop-entry (E7); 2x5m legal ONLY here",
		RuleField: HintFieldConfirmRule,
	}
	if err := validateHintTokens(preFix); err == nil {
		t.Error(`the guard accepted "2x5m" inside a confirm.rule hint — this is the exact text the model copied into confirm2.rule (rejected-prompt row 79)`)
	} else if !strings.Contains(err.Error(), "2x5m") {
		t.Errorf("guard error must name the offending token, got: %v", err)
	}

	fixed := ValidatorHint{
		Site:      "entry_law.go breakdown_continue (fixed)",
		Text:      "1 confirming close + displacement ≥ BD_MIN_DISP_ATR×ATR5m OR stop-entry (E7); 2x5m_close legal ONLY here",
		RuleField: HintFieldConfirmRule,
	}
	if err := validateHintTokens(fixed); err != nil {
		t.Errorf("the enum-form hint must pass, got: %v", err)
	}

	// A death/flip hint may legally say 2x5m — the token is only wrong in the
	// wrong field. The guard must be field-aware, not a blanket ban.
	deathFlip := ValidatorHint{
		Site:      "synthetic death/flip hint",
		Text:      "the death rule fires on 2x5m below the level (5m_close is the single-close variant)",
		RuleField: HintFieldConditionRule,
	}
	if err := validateHintTokens(deathFlip); err != nil {
		t.Errorf("2x5m is LEGAL in a death/flip hint — the guard must be field-scoped, got: %v", err)
	}

	// And the mirror: a confirm-enum token inside a death/flip hint is wrong.
	crossed := ValidatorHint{
		Site:      "synthetic crossed hint",
		Text:      "the death rule fires on 1x5m_close below the level",
		RuleField: HintFieldConditionRule,
	}
	if err := validateHintTokens(crossed); err == nil {
		t.Error("1x5m_close is not a death/flip rule (enum: 2x5m|5m_close) — the guard must reject it")
	}
}

// TestClass38LiveHintRegistryIsClean — every shipped hint, including the entry
// law Style strings the rejection quotes verbatim, passes the extended guard.
func TestClass38LiveHintRegistryIsClean(t *testing.T) {
	if err := ValidateValidatorHints(); err != nil {
		t.Fatalf("live validator-hint registry is BROKEN: %v", err)
	}
	// The entry law Styles must actually BE in the registry — they are the
	// site that produced row 78, and a guard that does not cover them is
	// theatre.
	sites := map[string]bool{}
	for _, h := range ValidatorHints() {
		sites[h.Site] = true
	}
	for _, cond := range KnownConditions() {
		if _, ok := EntryLawFor(cond); !ok {
			continue
		}
		want := "entry_law.go Style:" + cond
		if !sites[want] {
			t.Errorf("entry law Style for %q is quoted into rejections but is NOT registered as a validator hint (site %q missing) — the class-34/38 guard never sees it", cond, want)
		}
	}
}

// TestClass38PromptContractsAllStated is E3: every enumerated validator
// restriction is stated in the rendered prompt.
func TestClass38PromptContractsAllStated(t *testing.T) {
	prompt := plannerOutputContract(8, 5, true, true, false)
	onPrompt := plannerOutputContract(8, 5, true, true, true) // knob ON (write-time feasibility clause rendered)
	if err := ValidatePromptContracts(prompt); err != nil {
		t.Fatalf("a validator restriction is NOT stated in the prompt (class 38): %v", err)
	}
	if err := ValidatePromptContracts(onPrompt); err != nil {
		t.Fatalf("a validator restriction is NOT stated in the ON rendering (class 38): %v", err)
	}
	// W3: the market_in_zone rendering (the shipped default) is judged too.
	mizOn := class38MizRendering(true)
	if err := ValidatePromptContracts(class38MizRendering(false)); err != nil {
		t.Fatalf("a validator restriction is NOT stated in the market_in_zone rendering (class 38): %v", err)
	}
	if err := ValidatePromptContracts(mizOn); err != nil {
		t.Fatalf("a validator restriction is NOT stated in the market_in_zone ON rendering (class 38): %v", err)
	}
	if len(PromptContracts()) < 10 {
		t.Errorf("the contract registry has only %d rows — the C5 enumeration found more condition-keyed restrictions than that", len(PromptContracts()))
	}
	// Every row must carry the validator site it mirrors, so a reader can go
	// from the prompt sentence to the branch that enforces it.
	for _, c := range PromptContracts() {
		if strings.TrimSpace(c.Site) == "" {
			t.Errorf("contract %q has no validator site", c.Rule)
		}
		if len(c.MustAppear) == 0 {
			t.Errorf("contract %q states nothing — an empty contract always passes", c.Rule)
		}
		// A gate-gated row must actually be RENDERED somewhere (the ON
		// rendering) — otherwise the gate would make it silently dead.
		if c.Gate != "" {
			rendered := onPrompt
			if c.Gate == EntryPolicyPromptMarker {
				rendered = mizOn // W3: the ENTRY POLICY row renders under market_in_zone
			}
			for _, frag := range c.MustAppear {
				if !strings.Contains(rendered, frag) {
					t.Errorf("gated contract %q: fragment %q absent from the ON rendering — the row is dead", c.Rule, frag)
				}
			}
		}
	}
}

// TestClass38ContractTestFailsWhenPromptDropsARule is E3's teeth: remove a
// restriction's sentence from the prompt and the guard must fail. Without this
// the contract test could pass vacuously.
func TestClass38ContractTestFailsWhenPromptDropsARule(t *testing.T) {
	prompt := plannerOutputContract(8, 5, true, true, false)
	onPrompt := plannerOutputContract(8, 5, true, true, true) // knob ON
	for _, c := range PromptContracts() {
		// Gated rows apply only to the rendering that carries their sentence.
		applicable := prompt
		if c.Gate != "" {
			applicable = onPrompt
		}
		if c.Gate == EntryPolicyPromptMarker {
			applicable = class38MizRendering(true) // W3: the ENTRY POLICY row
		}
		mutilated := applicable
		for _, frag := range c.MustAppear {
			mutilated = strings.ReplaceAll(mutilated, frag, "")
		}
		if mutilated == applicable {
			t.Errorf("contract %q: none of its fragments were present to remove — the fragments do not match the live prompt", c.Rule)
			continue
		}
		if err := ValidatePromptContracts(mutilated); err == nil {
			t.Errorf("contract %q: removing its sentence from the prompt did NOT fail the guard", c.Rule)
		}
	}
}

// class38MizRendering is the output contract under the market_in_zone entry
// policy (W3) — the shipped default's rendering.
func class38MizRendering(writeFeas bool) string {
	return plannerOutputContractFor(8, 5, true, true, writeFeas, resolvePromptEntryPolicy(EntryPolicyMarketInZone, 0, 0))
}
