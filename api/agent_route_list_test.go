package api

// M3 red-team H1, third layer: the route list the Telegram LLM agent is given
// (GetAPIDocs, rendered into its system prompt by telegram/agent.
// BuildAgentPrompt — the production composition, telegram/bot.go) no longer
// advertises the account-management routes, the bot's own config routes, or
// the updater. The routes themselves stay registered (the web UI uses them);
// the server-side denials (credential_guard.go) are what enforce — an LLM can
// guess a path it was not told — this only stops handing it the map.

import (
	"net/http"
	"strings"
	"testing"

	"vl/telegram/agent"
)

// agentHiddenProbes: every path the agent's route list must not contain,
// with one registered route for each (proof the route is KEPT).
var agentHiddenProbes = []struct{ method, path string }{
	{"PUT", "/api/user/password"},
	{"POST", "/api/reset-account"},
	{"POST", "/api/reset-password"},
	{"POST", "/api/login"},
	{"POST", "/api/register"},
	{"POST", "/api/logout"},
	{"GET", "/api/telegram"},
	{"POST", "/api/telegram/model"},
	{"DELETE", "/api/telegram/binding"},
	{"GET", "/api/updates"},
}

func TestAgentRouteListOmitsAccountBotConfigAndUpdateRoutes(t *testing.T) {
	e := newUpdEnv(t) // NewServer → setupRoutes populates the registry
	docs := GetAPIDocs()
	prompt := agent.BuildAgentPrompt(docs, updAdminEmail, updAdminID)
	// P2-8 (owner-identity fold): the bot may read and reduce risk, so its
	// list keeps stop, not start (start is refused to machine tokens).
	for _, want := range []string{"/api/my-traders", "/api/traders/:id/stop", "/api/strategies"} {
		if !strings.Contains(docs, want) || !strings.Contains(prompt, want) {
			t.Fatalf("positive control: the agent's route list lost the ordinary route %s", want)
		}
	}
	for _, p := range agentHiddenProbes {
		for name, text := range map[string]string{"GetAPIDocs": docs, "BuildAgentPrompt": prompt} {
			for _, line := range strings.Split(text, "\n") {
				f := strings.Fields(line)
				if len(f) >= 2 && (f[1] == p.path || strings.HasPrefix(f[1], p.path+"/")) {
					t.Fatalf("%s advertises %s to the Telegram agent: %q", name, p.path, strings.TrimSpace(line))
				}
			}
		}
	}
	// The routes themselves are kept.
	have := map[string]bool{}
	for _, r := range e.s.router.Routes() {
		have[r.Method+" "+r.Path] = true
	}
	for _, p := range agentHiddenProbes {
		if !have[p.method+" "+p.path] {
			t.Fatalf("%s %s is no longer registered — only its advertisement was to go", p.method, p.path)
		}
	}
	// And the owner still reaches one of them (the web UI's login).
	if _, code := credLogin(t, e, updAdminEmail, updAdminPass); code != http.StatusOK {
		t.Fatalf("login = %d", code)
	}
}
