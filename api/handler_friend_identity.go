package api

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// W-FRIEND-HOSTED (Sonnet reviewer #2, S14): GET /api/friend/identity answers
// the SHA-256 of this instance's NT_CONNECT_KEY — never the key itself — so the
// friend app can prove it is talking to ITS bot, not some other friend's.
// Registered ONLY in friend mode (the owner's bot 404s); no auth (the app calls
// it before login).
func (s *Server) handleFriendIdentity(c *gin.Context) {
	key := strings.TrimSpace(os.Getenv("NT_CONNECT_KEY"))
	c.JSON(http.StatusOK, gin.H{"key_sha256": sha256Hex(key)})
}
