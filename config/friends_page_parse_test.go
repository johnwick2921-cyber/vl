package config

import "testing"

// m386 — the ONE parser for VL_FRIENDS_PAGE: "1" or "true" (trimmed,
// case-insensitive for "true"), anything else — including "" — is false.

func TestFriendsPageEnabled(t *testing.T) {
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
	}
	for _, c := range cases {
		t.Run("VL_FRIENDS_PAGE="+c.v, func(t *testing.T) {
			t.Setenv("VL_FRIENDS_PAGE", c.v)
			if got := FriendsPageEnabled(); got != c.want {
				t.Fatalf("FriendsPageEnabled() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestInitSetsFriendsPage(t *testing.T) {
	prev := global
	t.Cleanup(func() { global = prev })

	t.Setenv("VL_FRIENDS_PAGE", "1")
	Init()
	if !Get().FriendsPage {
		t.Fatal("VL_FRIENDS_PAGE=1 must set FriendsPage at Init")
	}

	t.Setenv("VL_FRIENDS_PAGE", "")
	Init()
	if Get().FriendsPage {
		t.Fatal("VL_FRIENDS_PAGE unset must leave FriendsPage false at Init")
	}
}
