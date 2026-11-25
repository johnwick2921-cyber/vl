package pep

import (
	"testing"

	"vl/store"
)

// OWNER RULING 2026-10-30 ("keep default E 6"): the shipped defaults are style E
// with 6 contracts; the E box is 2 minutes and the A box 5 minutes. The numbers
// are LITERALS here on purpose: this fails if ANY layer drifts, not only if two
// layers drift apart (TestDefaultConfigEqualsResolvedStoreDefaults covers the
// second case). Production path: store.ResolvePep(nil) -> FromSettings, and the
// stored-block path with a pep block that leaves the leaves unsaved.
//
// Mutant: store.PepStyleDefault "E" -> "A", PepContractsDefault 6 -> 3,
// PepEBoxMinutesDefault 2 -> 5, DefaultConfig().Style StyleA -> RED.
func TestShippedDefaultsAreStyleE6Contracts(t *testing.T) {
	if store.PepStyleDefault != "E" || store.PepContractsDefault != 6 {
		t.Fatalf("store shipped defaults = %q / %d, want E / 6", store.PepStyleDefault, store.PepContractsDefault)
	}
	if DefaultContracts != 6 || DefaultConfig().Style != StyleE || DefaultConfig().Contracts != 6 {
		t.Fatalf("kernel/pep DefaultConfig = %s / %d, want E / 6", DefaultConfig().Style, DefaultConfig().Contracts)
	}
	if store.PepEBoxMinutesDefault != 2 || store.PepABoxMinutesDefault != 5 {
		t.Fatalf("box defaults E %d A %d, want 2 / 5", store.PepEBoxMinutesDefault, store.PepABoxMinutesDefault)
	}
	on := true
	for name, c := range map[string]*store.StrategyConfig{
		"nil config":       nil,
		"no pep block":     {},
		"empty pep":        {Pep: &store.PepConfig{}},
		"on, nothing else": {Pep: &store.PepConfig{Enabled: &on}},
	} {
		r := store.ResolvePep(c)
		if r.Style != "E" || r.Contracts != 6 || r.E.BoxMinutes != 2 || r.A.BoxMinutes != 5 {
			t.Fatalf("%s: resolved %s / %d / E box %d / A box %d, want E / 6 / 2 / 5", name, r.Style, r.Contracts, r.E.BoxMinutes, r.A.BoxMinutes)
		}
		cfg := ConfigOf(c)
		if cfg.Style != StyleE || cfg.Contracts != 6 || cfg.E.BoxMinutes != 2 || cfg.A.BoxMinutes != 5 {
			t.Fatalf("%s: evaluator config %s / %d / E box %d / A box %d, want E / 6 / 2 / 5", name, cfg.Style, cfg.Contracts, cfg.E.BoxMinutes, cfg.A.BoxMinutes)
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("%s: shipped config invalid: %v", name, err)
		}
	}
	// A junk style runs the shipped default (E), named as a clamp.
	r := store.ResolvePep(&store.StrategyConfig{Pep: &store.PepConfig{Style: "Z"}})
	if r.Style != "E" || r.Clamps["pep_style"] == "" {
		t.Fatalf("junk style: %s clamps %v", r.Style, r.Clamps)
	}
}
