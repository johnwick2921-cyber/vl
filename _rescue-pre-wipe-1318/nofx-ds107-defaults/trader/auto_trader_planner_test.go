package trader

import (
	"testing"

	"nofx/mcp"
)

func TestResolvePlannerModelID(t *testing.T) {
	// Empty binding → primary.
	if id, prim := resolvePlannerModelID("", "deepseek"); id != "deepseek" || !prim {
		t.Fatalf("empty → primary: got %q/%v", id, prim)
	}
	if id, prim := resolvePlannerModelID("   ", "deepseek"); id != "deepseek" || !prim {
		t.Fatalf("whitespace → primary: got %q/%v", id, prim)
	}
	// Explicit binding → the pinned ID, not the primary.
	if id, prim := resolvePlannerModelID("deepseek-reasoner", "deepseek"); id != "deepseek-reasoner" || prim {
		t.Fatalf("pinned: got %q/%v want deepseek-reasoner/false", id, prim)
	}
}

func TestResolvePlannerClientEmptyUsesPrimary(t *testing.T) {
	// W2 — empty planner_model → primary client, but the stamped id is the EXACT
	// model, NEVER the provider alias (§125). "deepseek" pins to "deepseek-v4-pro".
	at := mkTrader("ninjatrader", boolp(true), "5m")
	at.aiModel = "deepseek"
	_, id := at.resolvePlannerClient()
	if mcp.IsProviderAlias(id) || id == "" {
		t.Fatalf("empty binding must resolve an EXACT model id (not an alias), got %q", id)
	}
	if id != mcp.DefaultModelForAlias("deepseek") {
		t.Fatalf("primary 'deepseek' should pin to %q, got %q", mcp.DefaultModelForAlias("deepseek"), id)
	}
	// A set planner_model resolves an exact, non-empty id too.
	at.config.StrategyConfig.DayPlan.PlannerModel = "deepseek"
	_, id2 := at.resolvePlannerClient()
	if id2 == "" || mcp.IsProviderAlias(id2) {
		t.Fatalf("resolved planner model id must be an exact non-empty string, got %q", id2)
	}
}
