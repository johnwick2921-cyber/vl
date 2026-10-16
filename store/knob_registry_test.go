package store

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// E1 — REGISTRY COMPLETENESS. Every schema field reflection can reach must
// carry a classification; a "live" one must name a consumer. Adding a field and
// leaving it unclassified FAILS THE BUILD, which is the point.
func TestKnobRegistryIsComplete(t *testing.T) {
	fields := EnumerateSchemaKnobs()
	if len(fields) < 40 {
		t.Fatalf("reflection found only %d fields — the enumerator is broken, not the registry", len(fields))
	}
	var missing, liveNoConsumer []string
	for _, p := range fields {
		e, ok := LookupKnob(p)
		if !ok {
			missing = append(missing, p)
			continue
		}
		if e.Status == KnobLive && len(e.Consumers) == 0 {
			liveNoConsumer = append(liveNoConsumer, p)
		}
	}
	sort.Strings(missing)
	sort.Strings(liveNoConsumer)
	if len(missing) > 0 {
		show := missing
		if len(show) > 10 {
			show = show[:10]
		}
		t.Errorf("%d schema field(s) unclassified — a setting nobody classified may not take effect:\n  %s",
			len(missing), strings.Join(show, "\n  "))
	}
	if len(liveNoConsumer) > 0 {
		t.Errorf("%d knob(s) marked LIVE with no consumer — the audit's dead-knob signature:\n  %s",
			len(liveNoConsumer), strings.Join(liveNoConsumer, "\n  "))
	}
	t.Logf("registry: %d reflected fields, all classified · %s", len(fields), KnobRegistryBootLine())
}

// The audit's dead knobs must NOT be classified live — the registry is only
// useful if it tells the truth about the fifteen that produced it.
func TestRegistryDoesNotCallTheAuditsDeadKnobsLive(t *testing.T) {
	for _, p := range AuditDeadKnobs2026_09_03 {
		e, ok := LookupKnob(p)
		if !ok {
			t.Errorf("%s: the audit named it and the registry does not know it", p)
			continue
		}
		if e.Status == KnobLive {
			t.Errorf("%s: registry says LIVE but the audit proved it cannot take effect", p)
		}
		// An audit-dead knob is 'ineffective' (read, no effect) — never
		// 'candidate', which means nobody has checked yet.
		if e.Status == KnobCandidate {
			t.Errorf("%s: the audit CHECKED this one — it is ineffective, not unverified", p)
		}
		if e.Note == "" {
			t.Errorf("%s: a non-live knob must carry the REASON", p)
		}
	}
}

// The boot line is counted from the registry, never typed. W1 (f): schema= is
// READ from the production enumeration (not a literal), and env-shadows — a
// counter nothing writes — reads n/a instead of a fabricated 0 (L7).
func TestKnobBootLineIsCounted(t *testing.T) {
	line := KnobRegistryBootLine() // the call main.go logs as "⚙ %s"
	schema := fmt.Sprintf("settings: schema=%d ", len(EnumerateSchemaKnobs()))
	envShadows := "env-shadows=n/a (not counted)"
	if s := KnobStatusSummary(); s.EnvShadows != nil {
		envShadows = fmt.Sprintf("env-shadows=%d", *s.EnvShadows)
	}
	// The ruling's two labels must BOTH appear — conflating them is the defect.
	for _, want := range []string{schema, "classified=", "live=", "ineffective=", "candidate-unverified=", envShadows} {
		if !strings.Contains(line, want) {
			t.Errorf("boot line missing %q: %s", want, line)
		}
	}
	if strings.Contains(line, "env-shadows=0") {
		t.Errorf("env-shadows printed 0 from a counter with no writer: %s", line)
	}
	if strings.Contains(line, "UNCLASSIFIED") {
		t.Errorf("the registry has unclassified fields: %s", line)
	}
	t.Logf("boot: %s", line)
}

// Release #32 (fix/risk-knob-truth) — item 5 and item 6 pin the registry to the
// B1 audit's truth. These are DATA assertions: the four crypto-era size toggles
// are ineffective with NO consumer, and the method-level recheck of the five
// CANDIDATE knobs promotes exactly the two whose free-function resolvers have a
// production caller, citing the consuming function by name.
func TestRelease32KnobTruth(t *testing.T) {
	// Item 5 — no consumer, KnobIneffective, with a reason. No UI exists for
	// these; the status must not pretend a reader exists.
	ineffective := map[string]string{
		"min_position_size":    "min_position_size",
		"max_margin_usage":     "max_margin_usage",
		"max_contracts_enabled": "max_contracts_enabled",
		"notional_cap_enabled":  "notional_cap_enabled",
	}
	for p, key := range ineffective {
		e, ok := LookupKnob(key)
		if !ok {
			t.Errorf("%s: registry does not know it", p)
			continue
		}
		if e.Status != KnobIneffective {
			t.Errorf("%s: status = %v, want KnobIneffective", p, e.Status)
		}
		if len(e.Consumers) != 0 {
			t.Errorf("%s: consumers = %v, want none", p, e.Consumers)
		}
		if e.Note == "" {
			t.Errorf("%s: an ineffective knob must carry the REASON", p)
		}
	}

	// Item 6 — the free-function resolver recheck. These two look Candidate to
	// a FIELD grep but their resolvers are consumed by the admission clock.
	live := map[string][]string{
		"last_entry_offset_min": {
			"store/strategy.go:LastEntryOffsetFor (resolver)",
			"trader/auto_trader_clock.go:383 entryBlockedByLastEntryAt",
			"trader/auto_trader_pause.go:154",
		},
		"eod_flat_offset_min": {
			"store/strategy.go:EODFlatOffsetFor (resolver)",
			"trader/auto_trader_clock.go:1091 eodSessionFlatAt",
			"trader/auto_trader_pause.go:158",
		},
	}
	for key, want := range live {
		e, ok := LookupKnob(key)
		if !ok {
			t.Errorf("%s: registry does not know it", key)
			continue
		}
		if e.Status != KnobLive {
			t.Errorf("%s: status = %v, want KnobLive (its resolver has a production caller)", key, e.Status)
		}
		for _, w := range want {
			found := false
			for _, c := range e.Consumers {
				if strings.Contains(c, w) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: consumers %v missing %q", key, e.Consumers, w)
			}
		}
	}

	// The three that have no production reader stay Candidate — the recheck
	// verified, it did not find a reader to promote.
	for _, key := range []string{"context_limit", "fixed_overhead", "longer_count"} {
		e, ok := LookupKnob(key)
		if !ok {
			t.Errorf("%s: registry does not know it", key)
			continue
		}
		if e.Status != KnobCandidate {
			t.Errorf("%s: status = %v, want KnobCandidate (no production reader found by the recheck)", key, e.Status)
		}
	}
}
