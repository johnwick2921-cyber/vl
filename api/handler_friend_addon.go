package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// handler_friend_addon.go — W-FRIEND-HOSTED (CTO 2026-10-21 00:15): the friend
// dashboard banner reads GET /api/friend/addon-status (friend mode only) to tell
// whether the CONNECTED NinjaTrader AddOn (its hello build_id) is older than the
// latest built addon (the _dist manifest's addon_build). Field names are the
// contract DS-106's banner consumes: connected_build, latest_build, outdated.

// friendConnectedBuildResolver returns the connected AddOn's hello build_id
// ("" when NT8 is not connected). It is a var so tests can pin the connected
// build without spinning a real TCP server; production reads FarSideBuildID.
var friendConnectedBuildResolver = func(s *Server) string {
	if tcp := s.futuresTCPServer(); tcp != nil {
		return tcp.FarSideBuildID()
	}
	return ""
}

// handleFriendAddonStatus serves GET /api/friend/addon-status. Registered ONLY
// in friend mode (the owner's bot 404s), no auth: the friend app and the
// dashboard banner read it before login.
func (s *Server) handleFriendAddonStatus(c *gin.Context) {
	connected := friendConnectedBuildResolver(s)
	latest := s.latestFriendAddonBuild()
	_, _, outdated := friendAddonStatus(connected, latest)
	c.JSON(http.StatusOK, gin.H{
		"connected_build": connected,
		"latest_build":    latest,
		"outdated":        outdated,
	})
}

// latestFriendAddonBuild reads addon_build from the active friend dist manifest;
// "" when there is no dist or no manifest (a friend bot that never had a build).
func (s *Server) latestFriendAddonBuild() string {
	dir := s.resolveFriendDistDir()
	if dir == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return ""
	}
	var m struct {
		AddonBuild string `json:"addon_build"`
	}
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	return m.AddonBuild
}

// friendAddonStatus is the ONE canonicalizer for "is the connected AddOn
// outdated" (K4): outdated is true only when BOTH build ids are known, both are
// date-prefixed build ids, and the connected id is OLDER than the latest under
// the R17 (date, suffix-length, suffix) ordering. A missing side, a non-date id,
// or a connected id that is >= latest reads NOT outdated — never a fabricated
// "update available", never a downgrade.
func friendAddonStatus(connected, latest string) (connectedBuild, latestBuild string, outdated bool) {
	comparable := datePrefixedBuildID(connected) && datePrefixedBuildID(latest)
	return connected, latest, comparable && buildIDLess(connected, latest)
}

// buildIDLess orders two date-prefixed build ids by (date, suffix-length,
// suffix) — R17. Plain lexical broke once the suffix grew to two digits:
// "2026-10-18-9" vs "2026-10-18-10" compares '9' > '1', so the 2-digit build
// read OLDER. Callers must already have checked datePrefixedBuildID.
func buildIDLess(a, b string) bool {
	if a[:10] != b[:10] {
		return a[:10] < b[:10]
	}
	as, bs := a[10:], b[10:]
	if len(as) != len(bs) {
		return len(as) < len(bs)
	}
	return as < bs
}

// datePrefixedBuildID reports whether b is a date-prefixed build id: it starts
// with YYYY-MM-DD (the AddOn VL_BUILD_ID form, e.g. "2026-10-18-a"). Anything
// else — empty, a bare number, a timestamp without dashes — is not comparable.
func datePrefixedBuildID(b string) bool {
	if len(b) < 10 {
		return false
	}
	for i := 0; i < 10; i++ {
		switch i {
		case 4, 7:
			if b[i] != '-' {
				return false
			}
		default:
			if b[i] < '0' || b[i] > '9' {
				return false
			}
		}
	}
	return true
}
