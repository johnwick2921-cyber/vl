package kernel

import (
	"strings"
	"testing"
)

// FIX-PLANNER-TRUTH (2) — the planner prompt must state the truth about the
// lunch band: the arm path enforces it too (refuses new placements and cancels
// resting arms), so plan_mode=strict does NOT leave the band unenforced.
func TestPlannerPromptStatesArmPathEnforcesLunchBand(t *testing.T) {
	p := BuildPlannerPrompt(PlannerInput{MaxLevels: 8, ScenarioCap: 3})
	if !strings.Contains(p, "enforced on BOTH paths — the arm path refuses new placements here and cancels resting arms") {
		t.Fatalf("the planner prompt must state the arm path enforces the lunch band:\n%s", p)
	}
	if strings.Contains(p, "no band predicate exists in the arm path") {
		t.Fatalf("the planner prompt must no longer claim no band predicate exists in the arm path")
	}
}
