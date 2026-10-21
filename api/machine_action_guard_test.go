package api

// P2-8 (rescan OPS-SECURITY-FEED "Telegram machine token can trade"; CTO fold
// on owner-identity 2026-10-14): a machine token may READ and REDUCE risk,
// never add it. Driven at the PRODUCTION router with the token the
// production bot mints (agent.GenerateBotToken → mustBotToken) — the bot now
// acts for the box owner, so before this guard its token could start the
// owner's traders, rewrite strategies and place test trades.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vl/auth"
	"vl/store"
	"vl/telegram/agent"
	"vl/telemetry"

	"github.com/gin-gonic/gin"
)

func mtRefusedCount(method, route string) int {
	_, table := telemetry.GateBlockSnapshot()
	return table[""][MachineTokenRefusedGate+":"+method+" "+route]
}

func mtIsRefusal(w *httptest.ResponseRecorder) bool {
	return w.Code == http.StatusForbidden && strings.Contains(w.Body.String(), machineActionRefusedMsg)
}

// mtSeedOwnerTrader: one trader row owned by the owner (the enrolled admin).
func mtSeedOwnerTrader(t *testing.T, e *updEnv) string {
	t.Helper()
	const id = "mt-owner-trader"
	if err := e.st.Trader().Create(&store.Trader{ID: id, UserID: updAdminID, Name: "hoang", AIModelID: "m", ExchangeID: "x", InitialBalance: 1000}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMachineTokenReadsAndReducesRiskButNeverAddsIt(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	t.Cleanup(telemetry.ResetGateBlocksForTest)
	e := newUpdEnv(t)
	bot := mustBotToken(t)
	tid := mtSeedOwnerTrader(t, e)

	// (a) A read: 200, and it sees the owner's trader.
	if w := credCall(t, e, "GET", "/api/my-traders", bot, ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), tid) {
		t.Fatalf("(a) the bot token on GET /api/my-traders = %d %s — want 200 with the owner's trader", w.Code, w.Body.String())
	}

	// (b) Risk-reducing actions pass the guard: the bot gets exactly what the
	// owner's own session gets from the handler (here the trader is not
	// loaded in the manager, so both read the handler's own answer).
	for _, p := range []struct{ method, path, body string }{
		{"POST", "/api/traders/" + tid + "/stop", ""},
		{"POST", "/api/traders/" + tid + "/close-position", `{"symbol":"MNQ","side":"LONG"}`},
		{"POST", "/api/traders/" + tid + "/pause", `{"minutes":30}`},
	} {
		wb := credCall(t, e, p.method, p.path, bot, p.body)
		wu := credCall(t, e, p.method, p.path, e.tok, p.body)
		if mtIsRefusal(wb) {
			t.Fatalf("(b) the bot token on %s %s was refused (%d %s) — stopping/closing only takes risk off", p.method, p.path, wb.Code, wb.Body.String())
		}
		if wb.Code != wu.Code || wb.Body.String() != wu.Body.String() {
			t.Fatalf("(b) %s %s: bot %d %s ≠ owner session %d %s — the guard must pass the bot to the same handler answer",
				p.method, p.path, wb.Code, wb.Body.String(), wu.Code, wu.Body.String())
		}
	}

	// (c) Risk-INCREASING and config routes: 403 with the one message, and
	// one count per refusal, by route.
	for _, p := range []struct{ method, path, route, body string }{
		{"POST", "/api/traders/" + tid + "/start", "/api/traders/:id/start", ""},
		{"POST", "/api/debug/nt-test-trade", "/api/debug/nt-test-trade", `{"trader_id":"` + tid + `","side":"buy","qty":1}`},
		{"POST", "/api/armed/test-arm", "/api/armed/test-arm", `{"trader_id":"` + tid + `"}`},
		{"PUT", "/api/strategies/s-1", "/api/strategies/:id", `{"name":"bot rewrite"}`},
		{"POST", "/api/traders", "/api/traders", `{"name":"bot-made"}`},
		{"POST", "/api/traders/" + tid + "/resume", "/api/traders/:id/resume", ""},
	} {
		w := credCall(t, e, p.method, p.path, bot, p.body)
		var body struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != http.StatusForbidden || body.Error != machineActionRefusedMsg {
			t.Fatalf("(c) the bot token on %s %s = %d %s — want 403 %q", p.method, p.path, w.Code, w.Body.String(), machineActionRefusedMsg)
		}
		if n := mtRefusedCount(p.method, p.route); n != 1 {
			t.Fatalf("(c) %s %s refusal counted %d times, want 1", p.method, p.route, n)
		}
	}
	// The owner's own session still reaches those handlers (only machines are refused).
	for _, p := range []struct{ method, path, body string }{
		{"POST", "/api/traders/" + tid + "/start", ""},
		{"PUT", "/api/strategies/s-1", `{"name":"owner rewrite"}`},
	} {
		if w := credCall(t, e, p.method, p.path, e.tok, p.body); mtIsRefusal(w) {
			t.Fatalf("the owner's session on %s %s was refused by the machine guard: %d %s", p.method, p.path, w.Code, w.Body.String())
		}
	}
}

// (d) A NEW mutating route — registered here, under authMiddleware, the way
// agent_routes.go registers its own — is refused to machine tokens by
// default: nobody listed it.
func TestMachineTokenIsRefusedOnANewMutatingRouteByDefault(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	t.Cleanup(telemetry.ResetGateBlocksForTest)
	e := newUpdEnv(t)
	ran := 0
	e.s.router.POST("/api/zz-new-knob", e.s.authMiddleware(), func(c *gin.Context) {
		ran++
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	gate, err := auth.GenerateScopedJWT(updAdminID, updAdminEmail, auth.ScopeGateJWT)
	if err != nil {
		t.Fatal(err)
	}
	for name, tok := range map[string]string{"bot token (scope=telegram)": mustBotToken(t), "gate-jwt token": gate, "bot@internal, no scope": mintMapJWT(t, ownerClaims("bot@internal"))} {
		if w := credCall(t, e, "POST", "/api/zz-new-knob", tok, `{}`); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), machineActionRefusedMsg) {
			t.Fatalf("%s on a NEW mutating route = %d %s — want 403 by default", name, w.Code, w.Body.String())
		}
	}
	if ran != 0 {
		t.Fatalf("the new route's handler ran %d times for machine tokens", ran)
	}
	if n := mtRefusedCount("POST", "/api/zz-new-knob"); n != 3 {
		t.Fatalf("new-route refusals counted %d, want 3", n)
	}
	if w := credCall(t, e, "POST", "/api/zz-new-knob", e.tok, `{}`); w.Code != http.StatusOK || ran != 1 {
		t.Fatalf("the owner's session on the new route = %d (ran %d) — want 200", w.Code, ran)
	}
}

// Census over the router's REAL route list: every protected route a machine
// token can change state on is refused unless it is a listed read or
// risk-reducing action. Protected = answers 401 with no token (authMiddleware);
// GETs are never called (some stream).
func TestMachineActionGuardCensusOverTheRouter(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	t.Cleanup(telemetry.ResetGateBlocksForTest)
	e := newUpdEnv(t)
	bot := mustBotToken(t)
	refused := map[string]bool{}
	for _, r := range e.s.router.Routes() {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || machineDenied(r.Path) {
			continue
		}
		if machineActionAllowed(r.Method, r.Path) {
			continue
		}
		path := concretePath(r.Path)
		if w := credCall(t, e, r.Method, path, "", "{}"); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "Missing Authorization header") {
			continue // public route: authMiddleware never runs there
		}
		w := credCall(t, e, r.Method, path, bot, "{}")
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), machineActionRefusedMsg) {
			t.Fatalf("%s %s with the bot token = %d %s — a state-changing route that is not a listed risk-reducing action must refuse machine tokens", r.Method, r.Path, w.Code, w.Body.String())
		}
		refused[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"POST /api/traders/:id/start", "POST /api/traders/:id/resume", "POST /api/traders", "PUT /api/traders/:id",
		"DELETE /api/traders/:id", "POST /api/strategies", "PUT /api/strategies/:id", "POST /api/strategies/:id/activate",
		"POST /api/exchanges", "PUT /api/models", "POST /api/plan/approve", "POST /api/plan/reset",
		"POST /api/armed/test-arm", "POST /api/debug/nt-test-trade", "POST /api/risk/clear-freeze",
		"POST /api/plan/session-registry", "POST /api/crypto/decrypt",
	} {
		if !refused[want] {
			t.Fatalf("census never refused %s — is it still registered, or was it allow-listed?", want)
		}
	}
	if len(refused) < 40 {
		t.Fatalf("census refused only %d routes — a walk that refused almost nothing proves nothing", len(refused))
	}
}

// Every allow-list entry names a registered route (no stale or misspelled
// entry silently widening nothing — or something else later).
func TestMachineAllowListNamesOnlyRegisteredRoutes(t *testing.T) {
	e := newUpdEnv(t)
	have := map[string]bool{}
	for _, r := range e.s.router.Routes() {
		have[r.Method+" "+r.Path] = true
	}
	for _, list := range [][]string{machineReadPostRoutes, machineRiskReducingRoutes} {
		for _, k := range list {
			if !have[k] {
				t.Fatalf("machine allow-list entry %q is not a registered route", k)
			}
		}
	}
}

// The agent's route list (GetAPIDocs) advertises no route its token is
// refused on, and keeps the risk-reducing ones.
func TestAgentRouteListAdvertisesOnlyWhatTheBotMayDo(t *testing.T) {
	_ = newUpdEnv(t)
	docs := GetAPIDocs()
	for _, line := range strings.Split(docs, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || !strings.HasPrefix(f[1], "/api/") {
			continue
		}
		switch f[0] {
		case "GET", "POST", "PUT", "DELETE":
		default:
			continue
		}
		if !machineActionAllowed(f[0], f[1]) {
			t.Fatalf("GetAPIDocs advertises %s %s, which the bot's token is refused on", f[0], f[1])
		}
	}
	for _, want := range []string{"/api/traders/:id/stop", "/api/traders/:id/close-position", "/api/risk/force-flat", "/api/my-traders"} {
		if !strings.Contains(docs, want) {
			t.Fatalf("GetAPIDocs lost the allowed route %s", want)
		}
	}
	// The production composition (telegram/bot.go → agent.BuildAgentPrompt)
	// no longer walks the agent through refused workflows, and states the rule.
	prompt := agent.BuildAgentPrompt(docs, updAdminEmail, updAdminID)
	for _, gone := range []string{"/api/traders/:id/start", "POST /api/strategies", "PUT /api/strategies", "POST /api/traders ", "PUT /api/models"} {
		if strings.Contains(prompt, gone) {
			t.Fatalf("the agent's system prompt still offers %q, which its token is refused on", gone)
		}
	}
	if !strings.Contains(prompt, machineActionRefusedMsg) {
		t.Fatal("the agent's system prompt does not state the read-and-reduce rule")
	}
}
