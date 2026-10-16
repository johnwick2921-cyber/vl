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

// F-7 (W32 mentor-knob-truth): mentor_spent_day_contracts is a DEAD knob — the
// spent-day runner cap is the constant mentorSpentDayRunnerCap = 2, never this
// field. The registry must say ineffective with no consumer, so the owner is
// not shown a knob that does anything. Mutant: marking it live → RED.
func TestMentorSpentDayContractsIsIneffective(t *testing.T) {
	e, ok := LookupKnob("mentor_spent_day_contracts")
	if !ok {
		t.Fatal("mentor_spent_day_contracts missing from the registry")
	}
	if e.Status != KnobIneffective {
		t.Fatalf("mentor_spent_day_contracts status = %v, want ineffective (dead — the spent-day runner cap is the constant)", e.Status)
	}
	if len(e.Consumers) != 0 {
		t.Fatalf("mentor_spent_day_contracts consumers = %v, want none", e.Consumers)
	}
	if e.Note == "" {
		t.Fatal("mentor_spent_day_contracts must carry the reason it is dead")
	}
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
