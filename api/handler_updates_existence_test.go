package api

// PR #200 review #15 (CTO 1790252194343): the handler_updates.go header
// comment claimed the agent "must never learn these routes exist". F1 only
// keeps them out of the route LIST the agent is handed (pinned by
// TestAgentRouteListOmitsAccountBotConfigAndUpdateRoutes); their existence is
// observable to anyone who probes: a registered /api/updates* method+path
// answers the gate's uniform 403, an unregistered path answers 404. This pin
// makes the corrected comment a checked statement about the PRODUCTION router
// (canon 53) — if the gate's answer ever changes, the comment must too.

import (
	"net/http"
	"testing"
)

func TestUpdatesRouteExistenceIsObservable(t *testing.T) {
	e := newUpdEnv(t)
	for name, tok := range map[string]string{"no token": "", "the Telegram bot's token": mustBotToken(t)} {
		for _, p := range []struct{ method, path string }{
			{"GET", "/api/updates"}, {"POST", "/api/updates/check"}, {"POST", "/api/updates/install"},
			{"GET", "/api/updates/readiness"}, {"GET", "/api/updates/jobs/0123456789abcdef"}, {"GET", "/api/updates/jobs/0123456789abcdef/receipt"},
		} {
			if w := credCall(t, e, p.method, p.path, tok, ""); w.Code != http.StatusForbidden || w.Body.String() != forbiddenBody {
				t.Fatalf("%s: registered %s %s = %d %s — the comment says it answers the gate's 403", name, p.method, p.path, w.Code, w.Body.String())
			}
		}
		for _, p := range []struct{ method, path string }{
			{"GET", "/api/updatez"}, {"GET", "/api/updates/nonexistent"}, {"DELETE", "/api/updates"}, {"POST", "/api/updates/jobs/0123456789abcdef"},
		} {
			if w := credCall(t, e, p.method, p.path, tok, ""); w.Code != http.StatusNotFound {
				t.Fatalf("%s: unregistered %s %s = %d %s — want 404 (the other half of the existence oracle)", name, p.method, p.path, w.Code, w.Body.String())
			}
		}
	}
}
