package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"vl/manager"
	"vl/store"
)

// W-FRIEND-HOSTED (CTO 2026-10-21 00:15): GET /api/friend/addon-status answers
// {connected_build, latest_build, outdated} in friend mode only.

func newAddonStatusServer(t *testing.T, friend bool, manifestAddonBuild string, connectedBuild string) *Server {
	t.Helper()
	prevMode := friendModeResolver
	prevBuild := friendConnectedBuildResolver
	prevDir := friendManifestDir
	t.Cleanup(func() {
		friendModeResolver = prevMode
		friendConnectedBuildResolver = prevBuild
		friendManifestDir = prevDir
	})
	friendModeResolver = func() bool { return friend }
	friendConnectedBuildResolver = func(*Server) string { return connectedBuild }

	if manifestAddonBuild != "" {
		dir := t.TempDir()
		payload, _ := json.Marshal(map[string]string{"addon_build": manifestAddonBuild, "app_version": "1.0"})
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), payload, 0o600); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
		friendManifestDir = dir
	} else {
		friendManifestDir = ""
	}

	st, err := store.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewServer(manager.NewTraderManager(), st, nil, "127.0.0.1", 0)
}

func getAddonStatus(t *testing.T, s *Server) (int, map[string]interface{}) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/friend/addon-status", nil)
	s.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		return w.Code, nil
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v (%s)", err, w.Body.String())
	}
	return w.Code, body
}

func TestFriendAddonStatusOwner404(t *testing.T) {
	s := newAddonStatusServer(t, false, "b", "a")
	code, _ := getAddonStatus(t, s)
	if code != http.StatusNotFound {
		t.Fatalf("owner mode = %d, want 404 (route not registered)", code)
	}
}

func TestFriendAddonStatusNoConnectionNoManifest(t *testing.T) {
	s := newAddonStatusServer(t, true, "", "")
	code, body := getAddonStatus(t, s)
	if code != http.StatusOK {
		t.Fatalf("friend mode = %d, want 200", code)
	}
	if body["connected_build"] != "" || body["latest_build"] != "" || body["outdated"] != false {
		t.Fatalf("got %v, want all-empty + outdated=false", body)
	}
}

func TestFriendAddonStatusOutdated(t *testing.T) {
	s := newAddonStatusServer(t, true, "2026-10-20-b", "2026-10-18-a")
	_, body := getAddonStatus(t, s)
	if body["connected_build"] != "2026-10-18-a" || body["latest_build"] != "2026-10-20-b" || body["outdated"] != true {
		t.Fatalf("got %v, want connected=old latest=new outdated=true", body)
	}
}

func TestFriendAddonStatusCurrent(t *testing.T) {
	s := newAddonStatusServer(t, true, "2026-10-20-b", "2026-10-20-b")
	_, body := getAddonStatus(t, s)
	if body["outdated"] != false {
		t.Fatalf("connected == latest must be outdated=false, got %v", body)
	}
}

func TestFriendAddonStatusMissingConnectedNotOutdated(t *testing.T) {
	// NT8 closed (connected "") with a known latest build → NOT outdated (no
	// fabricated "update available"). The mutant that drops the non-empty guard
	// turns this RED.
	s := newAddonStatusServer(t, true, "2026-10-20-b", "")
	_, body := getAddonStatus(t, s)
	if body["outdated"] != false {
		t.Fatalf("missing connected build must be outdated=false, got %v", body)
	}
}

func TestFriendAddonStatusMissingLatestNotOutdated(t *testing.T) {
	s := newAddonStatusServer(t, true, "", "2026-10-18-a")
	_, body := getAddonStatus(t, s)
	if body["outdated"] != false {
		t.Fatalf("missing latest build must be outdated=false, got %v", body)
	}
}

func TestFriendAddonStatusPure(t *testing.T) {
	cases := []struct {
		connected, latest string
		wantOutdated      bool
	}{
		{"", "", false},
		{"", "b", false},
		{"a", "", false},
		{"a", "a", false},
		{"a", "b", true},
	}
	for _, c := range cases {
		_, _, got := friendAddonStatus(c.connected, c.latest)
		if got != c.wantOutdated {
			t.Errorf("friendAddonStatus(%q,%q).outdated = %v, want %v", c.connected, c.latest, got, c.wantOutdated)
		}
	}
}
