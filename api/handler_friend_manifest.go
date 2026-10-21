package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"vl/config"
)

// handler_friend_manifest.go — B2 contract + B5 S9: the friend bot serves the
// signed friend manifest and the artifacts it points at; the friend app verifies
// everything before installing. Served ONLY in friend mode (VL_FRIEND_INSTANCE=1);
// the owner's bot answers 404 on every /api/friend/* route.

// The build (DS-108 deploy/friend/build.sh) drops each build into
// /home/hoang/vl-friends/_dist/<build>/ and writes the build name into
// _dist/LATEST. The friend bot's WorkingDirectory is /home/hoang/vl-friends/<name>/
// (B2), so "../_dist" is the shared dist root. A LATEST pointer file (never a
// symlink swap) avoids any deletion when a new build lands: old builds persist.
var (
	friendDistRoot    = filepath.Join("..", "_dist")
	friendManifestDir string // test override; empty = resolve from _dist/LATEST
)

// resolveFriendDistDir returns the active dist dir (the LATEST build), or "".
func (s *Server) resolveFriendDistDir() string {
	if friendManifestDir != "" {
		return friendManifestDir // test override
	}
	b, err := os.ReadFile(filepath.Join(friendDistRoot, "LATEST"))
	if err != nil {
		return ""
	}
	return filepath.Join(friendDistRoot, strings.TrimSpace(string(b)))
}

// friendModeOn reports friend mode via the ONE parser (config.FriendInstanceEnabled,
// S21) so this file does not depend on the config plumbing DS-102 owns.
func friendModeOn() bool {
	return config.FriendInstanceEnabled()
}

// handleFriendManifest serves GET /api/friend/manifest — the manifest JSON.
func (s *Server) handleFriendManifest(c *gin.Context) {
	if !friendModeOn() {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	dir := s.resolveFriendDistDir()
	if dir == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no friend dist"})
		return
	}
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no friend manifest"})
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", b)
}

// handleFriendManifestSig serves GET /api/friend/manifest.sig — the armored
// SSHSIG over the manifest.
func (s *Server) handleFriendManifestSig(c *gin.Context) {
	if !friendModeOn() {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	dir := s.resolveFriendDistDir()
	if dir == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no friend dist"})
		return
	}
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json.sig"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no friend manifest signature"})
		return
	}
	c.Data(http.StatusOK, "text/plain; charset=utf-8", b)
}

// handleFriendArtifact serves GET /api/friend/artifact/:name. Only the two real
// artifacts exist; anything else is refused (the name is whitelisted, never used
// as a path fragment).
func (s *Server) handleFriendArtifact(c *gin.Context) {
	if !friendModeOn() {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	dir := s.resolveFriendDistDir()
	if dir == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no friend dist"})
		return
	}
	name := c.Param("name")
	var fn string
	switch name {
	case "addon":
		fn = "VL.AddOn.dll"
	case "app":
		fn = "vl-friend.exe"
	default:
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.File(filepath.Join(dir, fn))
}
