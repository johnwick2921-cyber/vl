package api

import (
	"testing"
)

// friend_manifest_routes_test.go — B5 S12(b): the three /api/friend/* routes
// DS-108 owns must NOT be registered on the owner's bot (friend mode off); they
// exist only in friend mode. The handler-level 404 stays as defense in depth.
// MUTANT: move any of the three api.GET lines back outside the `if s.friendMode`
// gate → TestFriendManifestRoutesNotRegisteredOnOwnerBot goes RED.

func friendRouteSet(s *Server) map[string]bool {
	m := map[string]bool{}
	for _, r := range s.router.Routes() {
		m[r.Method+" "+r.Path] = true
	}
	return m
}

var friendManifestRoutePaths = []string{
	"GET /api/friend/manifest",
	"GET /api/friend/manifest.sig",
	"GET /api/friend/artifact/:name",
}

func TestFriendManifestRoutesNotRegisteredOnOwnerBot(t *testing.T) {
	s := newFriendFlatServer(t, false, nil) // owner mode
	have := friendRouteSet(s)
	for _, p := range friendManifestRoutePaths {
		if have[p] {
			t.Fatalf("owner bot registered %s — B5 S12(b): friend routes must not exist on the owner's bot", p)
		}
	}
}

func TestFriendManifestRoutesRegisteredInFriendMode(t *testing.T) {
	s := newFriendFlatServer(t, true, nil) // friend mode
	have := friendRouteSet(s)
	for _, p := range friendManifestRoutePaths {
		if !have[p] {
			t.Fatalf("friend bot is missing %s", p)
		}
	}
}
