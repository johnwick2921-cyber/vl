package agent

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"vl/mcp"
	"vl/store"
)

// R1 — the custom-URL model client built in loadAIClientFromStoreUser must ride
// security.SafeHTTPClient, so a loopback target AND a redirect-to-loopback are
// both refused at the very call site (not just at config-write time).

func TestR1CustomURLModelClientBlocksLoopbackAndRedirect(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agent-r1.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	// A provider that is NOT in the registry so selection falls through to the
	// custom-URL branch (the one that used to build a bare http.Client).
	loopback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer loopback.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, loopback.URL, http.StatusFound)
	}))
	defer redirector.Close()

	if _, err := st.AIModel().CreateEntry("default", "myproxy", "proxy", true, "sk-test", loopback.URL, "mymodel"); err != nil {
		t.Fatalf("create model entry: %v", err)
	}

	a := New(nil, st, DefaultConfig(), slog.Default())
	client, _, ok := a.loadAIClientFromStoreUser("default")
	if !ok {
		t.Fatalf("model selection should succeed")
	}

	base, isBase := client.(*mcp.Client)
	if !isBase {
		t.Fatalf("custom-URL branch must return *mcp.Client, got %T", client)
	}
	if base.HTTPClient == nil {
		t.Fatalf("client has no HTTPClient")
	}

	// Loopback target: the dial must be refused before any bytes move.
	if resp, err := base.HTTPClient.Get(loopback.URL); err == nil {
		resp.Body.Close()
		t.Fatalf("loopback target must be refused, got HTTP %d", resp.StatusCode)
	}

	// Redirect-to-loopback: the redirect guard must refuse the hop.
	if resp, err := base.HTTPClient.Get(redirector.URL); err == nil {
		resp.Body.Close()
		t.Fatalf("redirect-to-loopback must be refused, got HTTP %d", resp.StatusCode)
	}
}
