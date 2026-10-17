package telegram

import "testing"

// UpdateResultMessage must render the plan's exact plain words per terminal
// state, with an unknown state empty (nothing fabricated, nothing sent).
func TestUpdateResultMessage(t *testing.T) {
	cases := []struct {
		name                       string
		state, newTag, oldTag, why string
		want                       string
	}{
		{"done", "complete", "v2", "v1", "", "Update done — running v2"},
		{"rolled back", "rolled_back", "v2", "v1", "AddOn reconnect timed out",
			"Update could not finish — nothing changed, still running v1, trading normal. Reason: AddOn reconnect timed out"},
		{"reverted clean", "reverted_clean", "v2", "v1", "build failed",
			"Update could not finish — nothing changed, still running v1, trading normal. Reason: build failed"},
		{"refused", "refused", "v2", "v1", "not passed within 10m0s: sim_accounts",
			"Update could not finish — nothing changed, still running v1, trading normal. Reason: not passed within 10m0s: sim_accounts"},
		{"recovery needed", "recovery_needed", "v2", "v1", "rollback failed",
			"⚠️ Update needs your attention — the automatic recovery failed. Reason: rollback failed"},
		{"unknown state", "requested", "v2", "v1", "", ""},
	}
	for _, c := range cases {
		if got := UpdateResultMessage(c.state, c.newTag, c.oldTag, c.why); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
