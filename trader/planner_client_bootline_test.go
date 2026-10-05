package trader

import (
	"strings"

	"testing"
	"vl/mcp"
)

// Class 37 (C7), RENAMED by R2 (2026-09-02) — the per-trader boot line carries
// the RESOLVED client config. It is now titled "executor client" because it
// describes the NON-STREAM paths; the stream policy has its own 🛰 line and the
// two used to be indistinguishable while reporting different retry counts.
func TestClass37PlannerClientBootLineWording(t *testing.T) {
	line := plannerClientBootLine("8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek", 30, 1200, 600, 2, 2, 65536)
	for _, want := range []string{
		"🛰 executor client:",
		"provider_row=8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek",
		"idle=30s",
		"total=1200s",
		"http_ceiling=600s (non-stream paths only",
		"retries=2 backoff=2s",
		"cap=65536",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("boot line missing %q: %s", want, line)
		}
	}
	if strings.Contains(strings.ToLower(line), "sk-") {
		t.Fatalf("boot line must never carry a key fragment: %s", line)
	}
}

func TestClass37PlannerStreamTotalResolvedInTrader(t *testing.T) {
	t.Setenv("AI_PLAN_TOTAL_DEADLINE_SECS", "")
	t.Setenv("AI_PLAN_STREAM_IDLE_SECS", "")
	if got := plannerStreamTotal().Seconds(); got != 1200 {
		t.Fatalf("plannerStreamTotal = %.0fs, want 1200", got)
	}
	if plannerStreamTotal() <= plannerStreamIdle() {
		t.Fatalf("total must exceed idle")
	}
}

// CLASS 46 — the class-41 stream-policy boot line and its fixture are GONE.
// Both asserted the same string literals (watchdog_log=on, keepalive=30s,
// serialize_executor=off, resend_identical=on), so the pair could only ever
// agree with each other, never with reality. The replacement lives in
// mcp/planner_policy_test.go, where every field is compared against the
// function that enforces it.

// R2 — the two 🛰 lines must be distinguishable. Before this both began
// "🛰 planner client:" and reported different retry numbers (2 vs 3) for
// different paths, with no way to tell which governed what.
func TestR2ExecutorAndPlannerLinesAreDistinct(t *testing.T) {
	exec := plannerClientBootLine("row-1", 30, 1200, 600, 2, 2, 65536)
	stream := mcp.PlannerClientPolicyLine()
	if !strings.HasPrefix(exec, "🛰 executor client:") {
		t.Fatalf("the non-stream line must name its own path:\n%s", exec)
	}
	if !strings.HasPrefix(stream, "🛰 planner client:") {
		t.Fatalf("the stream line keeps the planner name:\n%s", stream)
	}
	if strings.HasPrefix(exec, strings.SplitN(stream, ":", 2)[0]+":") {
		t.Fatal("the two lines still share a prefix — a reader cannot tell them apart")
	}
	// The executor line states the paths it governs, and its values are read.
	for _, want := range []string{"retries=2", "backoff=2s", "http_ceiling=600s", "non-stream paths only"} {
		if !strings.Contains(exec, want) {
			t.Fatalf("executor line missing %q:\n%s", want, exec)
		}
	}
}
