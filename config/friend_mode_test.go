package config

import (
	"testing"
)

// W-FRIEND-HOSTED Segment 2 (DS-102) — VL_FRIEND_INSTANCE / VL_FRIEND_NAME and
// the B2 name contract.

func TestValidFriendName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"tom", true},
		{"tom2", true},
		{"a0", true},
		{"abcdefghijklmnopqrst", true},   // 20 chars
		{"a", false},                     // 1 char
		{"abcdefghijklmnopqrstu", false}, // 21 chars
		{"Tom", false},                   // uppercase
		{"tom smith", false},             // space
		{"tom-smith", false},             // hyphen
		{"", false},
	}
	for _, c := range cases {
		if got := ValidFriendName(c.name); got != c.want {
			t.Errorf("ValidFriendName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIsFriendInstanceNilSafe(t *testing.T) {
	var c *Config
	if c.IsFriendInstance() {
		t.Fatal("nil *Config must not be a friend instance")
	}
}

func TestFriendInstanceEnvParse(t *testing.T) {
	prev := global
	t.Cleanup(func() { global = prev })

	t.Setenv("VL_FRIEND_INSTANCE", "1")
	t.Setenv("VL_FRIEND_NAME", " Tom Smith ")
	Init()
	c := Get()
	if !c.IsFriendInstance() {
		t.Fatal("VL_FRIEND_INSTANCE=1 must set FriendInstance")
	}
	if c.FriendName != "tom smith" {
		t.Fatalf("FriendName = %q, want lowercased+trimmed %q", c.FriendName, "tom smith")
	}
}

func TestFriendInstanceUnsetIsOwner(t *testing.T) {
	prev := global
	t.Cleanup(func() { global = prev })

	t.Setenv("VL_FRIEND_INSTANCE", "")
	t.Setenv("VL_FRIEND_NAME", "")
	Init()
	if Get().IsFriendInstance() {
		t.Fatal("VL_FRIEND_INSTANCE unset must leave FriendInstance false")
	}
}
