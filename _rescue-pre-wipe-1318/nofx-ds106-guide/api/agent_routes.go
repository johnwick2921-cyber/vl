package api

import (
	"vl/agent"

	"github.com/gin-gonic/gin"
)

// RegisterAgentHandler registers NOFXi agent API routes on the main router.
// Chat endpoint requires authentication; market data endpoints are public.
func (s *Server) RegisterAgentHandler(h *agent.WebHandler) {
	// Chat requires auth — can trigger trades and access account data
	s.router.POST("/api/agent/chat", s.authMiddleware(), func(c *gin.Context) {
		isAdmin := c.GetString("user_id") == "admin"
		ctx := agent.WithStoreUserID(c.Request.Context(), c.GetString("user_id"))
		ctx = agent.WithSessionPolicy(ctx, agent.SessionPolicy{
			Authenticated:           true,
			IsAdmin:                 isAdmin,
			CanExecuteTrade:         true,
			CanViewSensitiveSecrets: false,
		})
		req := c.Request.WithContext(ctx)
		h.HandleChat(c.Writer, req)
	})
	s.router.POST("/api/agent/chat/stream", s.authMiddleware(), func(c *gin.Context) {
		isAdmin := c.GetString("user_id") == "admin"
		ctx := agent.WithStoreUserID(c.Request.Context(), c.GetString("user_id"))
		ctx = agent.WithSessionPolicy(ctx, agent.SessionPolicy{
			Authenticated:           true,
			IsAdmin:                 isAdmin,
			CanExecuteTrade:         true,
			CanViewSensitiveSecrets: false,
		})
		req := c.Request.WithContext(ctx)
		h.HandleChatStream(c.Writer, req)
	})
	// Public endpoints — read-only agent health
	s.router.GET("/api/agent/health", gin.WrapF(h.HandleHealth))
}
