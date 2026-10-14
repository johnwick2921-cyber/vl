package api

import (
	"net/http"

	"vl/auth"
	"vl/logger"
	"vl/telemetry"

	"github.com/gin-gonic/gin"
)

// ── P2-8 (rescan OPS-SECURITY-FEED, CTO fold on owner-identity 2026-10-14) ──
// A MACHINE token (auth.Claims.IsMachine: the Telegram bot's scope=telegram
// token, gate-jwt's, any scope claim, or bot@internal) may READ and REDUCE
// risk — never add it.
//
// Until owner-identity the bot acted for a test account that owned nothing,
// so its token could reach no trader. Now it acts for the box owner, and
// every protected route outside machineDeniedRoutes would have answered it:
// start a trader, create one, rewrite a strategy, arm a plan. This guard,
// run in authMiddleware for every machine token, is DENY BY DEFAULT over the
// router's real routes:
//   - a GET/HEAD is a read: admitted (machineDeniedRoutes still refuses the
//     GETs it names first);
//   - a POST that only reads (machineReadPostRoutes) is admitted;
//   - a risk-REDUCING action (machineRiskReducingRoutes: stop/pause a
//     trader, close one position, flatten a trader) is admitted;
//   - EVERY other method/route — including any route added later — is
//     refused 403 with machineActionRefusedMsg, logged, and counted under
//     gate "machine_token_refused:<METHOD> <route>" (trader "").
// Both lists are exact (method, gin FullPath) pairs and every entry must be
// a registered route (TestMachineAllowListNamesOnlyRegisteredRoutes). There
// is no route that cancels an order, so none is listed.
//
// The cutover-worker token keeps its own stricter two-route allow-list,
// checked first in authMiddleware.

// machineActionRefusedMsg is the 403 body a refused machine token receives.
const machineActionRefusedMsg = "the bot may read and reduce risk, not add it"

// MachineTokenRefusedGate prefixes the gate-block counter of one refusal.
const MachineTokenRefusedGate = "machine_token_refused"

// machineReadPostRoutes are POST routes that only READ: their handlers
// change no trading state, config or knob.
var machineReadPostRoutes = []string{
	"POST /api/equity-history-batch",      // batch read of equity history
	"POST /api/v1/bars/stream-ticket",     // a short-lived ticket for the read-only bar stream
	"POST /api/strategies/preview-prompt", // renders a prompt from a config in memory; saves nothing
}

// machineRiskReducingRoutes are the only state-changing routes a machine
// token may reach: each one can only take risk OFF.
var machineRiskReducingRoutes = []string{
	"POST /api/traders/:id/stop",           // halt a trader
	"POST /api/traders/:id/pause",          // refuse NEW entries for a while
	"POST /api/traders/:id/close-position", // close one open position
	"POST /api/risk/force-flat",            // flatten a trader
}

// machineActionAllowed reports whether a machine token may run method on the
// route pattern fullPath. Deny by default: anything not a read or a listed
// risk-reducing action is refused, so a NEW route is refused until someone
// lists it here on purpose.
func machineActionAllowed(method, fullPath string) bool {
	if fullPath == "" {
		return false
	}
	if method == http.MethodGet || method == http.MethodHead {
		return true
	}
	key := method + " " + fullPath
	for _, r := range machineReadPostRoutes {
		if r == key {
			return true
		}
	}
	for _, r := range machineRiskReducingRoutes {
		if r == key {
			return true
		}
	}
	return false
}

// machineActionRefuse answers one refused machine-token request: 403 with
// the one message, a WARN naming the route and scope (never the token), and
// one gate-block count for that route.
func machineActionRefuse(c *gin.Context, cl *auth.Claims) {
	route := c.Request.Method + " " + c.FullPath()
	telemetry.IncGateBlock("", MachineTokenRefusedGate+":"+route)
	scope := ""
	if cl != nil {
		scope = cl.Scope
	}
	logger.Warnf("🔒 [machine] refused %s from %s (scope=%q): %s", route, c.ClientIP(), scope, machineActionRefusedMsg)
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": machineActionRefusedMsg})
}
