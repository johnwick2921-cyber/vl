package api

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// handler_friend_manifest.go — B2 contract + B5 S9: the friend bot serves the
// signed friend manifest and the artifacts it points at; the friend app verifies
// everything before installing. Served ONLY in friend mode (VL_FRIEND_INSTANCE=1);
// the owner's bot answers 404 on every /api/friend/* route.

// friendManifestDir is where the release build drops the friend manifest, its
// signature and the artifacts. Tests point it at a temp dir.
var friendManifestDir = filepath.Join("deploy", "friend")

// friendModeOn reports friend mode. It reads the env flag directly so this file
// does not depend on the config plumbing DS-102 owns (the flag contract is
// env VL_FRIEND_INSTANCE=1 — B2).
func friendModeOn() bool {
	return os.Getenv("VL_FRIEND_INSTANCE") == "1"
}

// handleFriendManifest serves GET /api/friend/manifest — the manifest JSON.
func (s *Server) handleFriendManifest(c *gin.Context) {
	if !friendModeOn() {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	b, err := os.ReadFile(filepath.Join(friendManifestDir, "manifest.json"))
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
	b, err := os.ReadFile(filepath.Join(friendManifestDir, "manifest.json.sig"))
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
	c.File(filepath.Join(friendManifestDir, fn))
}
