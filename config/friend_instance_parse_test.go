package config

import "testing"

// S21 — the ONE parser for VL_FRIEND_INSTANCE: "1" or "true" (trimmed,
// case-insensitive), anything else false.
func TestFriendInstanceEnabled(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{" True ", true},
		{"", false},
		{"0", false},
		{"false", false},
		{"yes", false},
		{"on", false},
		{"2", false},
	}
	for _, c := range cases {
		t.Run("VL_FRIEND_INSTANCE="+c.v, func(t *testing.T) {
			t.Setenv("VL_FRIEND_INSTANCE", c.v)
			if got := FriendInstanceEnabled(); got != c.want {
				t.Fatalf("FriendInstanceEnabled() = %v, want %v", got, c.want)
			}
		})
	}
}
