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
// outdated": outdated is true only when BOTH builds are known (non-empty) and
// differ. A missing connected build (NT8 closed) or a missing latest build (no
// dist yet) reads NOT outdated — never a fabricated "update available".
func friendAddonStatus(connected, latest string) (connectedBuild, latestBuild string, outdated bool) {
	return connected, latest, connected != "" && latest != "" && connected != latest
}
