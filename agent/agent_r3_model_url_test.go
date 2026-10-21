package agent

import (
	"strings"
	"testing"
)

// R3 — the agent's model-config validation must enforce the friend https-only
// rule through the ONE helper (security.ValidateModelURL). The owner's box
// keeps the lenient http-allowed behavior.

func TestR3ModelConfigValidatorFriendModeRequiresHTTPS(t *testing.T) {
	cases := []struct {
		name     string
		friend   string
		url      string
		wantErr  bool
		contains string
	}{
		{"friend http refused", "1", "http://api.deepseek.com", true, "https"},
		{"friend https ok", "1", "https://api.deepseek.com", false, ""},
		{"owner http allowed", "", "http://api.deepseek.com", false, ""},
		{"owner http loopback refused", "", "http://127.0.0.1:1", true, "127.0.0.1"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("VL_FRIEND_INSTANCE", c.friend)
			v := modelConfigValidator{provider: "deepseek", customAPIURL: c.url, modelID: "deepseek"}
			err := v.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() url=%q friend=%q: err=%v, wantErr=%v", c.url, c.friend, err, c.wantErr)
			}
			if c.wantErr && !strings.Contains(err.Error(), c.contains) {
				t.Fatalf("error %q should contain %q", err.Error(), c.contains)
			}
		})
	}
}
