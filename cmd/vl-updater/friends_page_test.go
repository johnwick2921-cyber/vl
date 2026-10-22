package main

import (
	"path/filepath"
	"testing"
)

// m386 — the updater's friend sweep is skipped unless VL_FRIENDS_PAGE=1. The
// production seam is friendRegistryPathIfEnabled (not a copied parser): on →
// the non-empty friends.json path; off/unset → "" (updateFriends then takes the
// "no friend surface configured" branch).

func TestFriendRegistryPathIfEnabled(t *testing.T) {
	t.Setenv("VL_FRIENDS_ROOT", filepath.Join(t.TempDir(), "root"))

	t.Setenv("VL_FRIENDS_PAGE", "1")
	if got := friendRegistryPathIfEnabled(); got == "" {
		t.Fatal("VL_FRIENDS_PAGE=1 must give a non-empty registry path")
	} else if filepath.Base(got) != "friends.json" {
		t.Fatalf("path = %q, want it to end in friends.json", got)
	}

	t.Setenv("VL_FRIENDS_PAGE", "true")
	if got := friendRegistryPathIfEnabled(); got == "" {
		t.Fatal("VL_FRIENDS_PAGE=true must give a non-empty registry path")
	}

	for _, off := range []string{"", "0", "false"} {
		t.Setenv("VL_FRIENDS_PAGE", off)
		if got := friendRegistryPathIfEnabled(); got != "" {
			t.Fatalf("VL_FRIENDS_PAGE=%q must give \"\" (sweep skipped), got %q", off, got)
		}
	}
}
