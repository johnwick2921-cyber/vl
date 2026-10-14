package api

import (
	"vl/store"

	"github.com/gin-gonic/gin"
)

// requireOwner (N-1, verify/0926-system; owner-identity 2026-10-14) — true
// only for a NON-machine JWT whose user is the box OWNER as store.ResolveOwner
// resolves it (the one resolver the Telegram bot uses too). It used to be
// users[0] — the first-created account — on the promise that "registration
// closes after the first user", which is false on the live box: the first
// account is a test account that owns no trader. Fail closed: nil claims, a
// machine token, no store, or NO resolved owner (split ownership, the traders'
// user missing, no users, a store error) all refuse; the unresolved case is
// logged and counted (gate owner_unresolved).
func (s *Server) requireOwner(c *gin.Context) bool {
	cl := authClaimsFrom(c)
	if cl == nil || cl.UserID == "" || cl.IsMachine() {
		return false
	}
	if s.store == nil {
		return false
	}
	o := s.store.ResolveOwner()
	if !o.Resolved() {
		store.RecordOwnerRefusal("owner-only route "+c.Request.Method+" "+c.FullPath(), o)
		return false
	}
	return o.UserID == cl.UserID
}

// apiTradingMode is a seam kept for tests: the build is futures-only (C2), so
// it always reports "futures".
var apiTradingMode = func() string {
	return "futures"
}

// tradingModeIsFutures reports whether this build is running the futures
