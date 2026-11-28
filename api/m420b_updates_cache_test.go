package api

import (
	"encoding/json"
	"testing"
	"time"

	"vl/internal/updaterwire"
	"vl/internal/updaterwire/wireserver"
)

// m420b/B4 — GET /api/updates affirms update_available / latest_tag only from a
// FRESH cached check, and install_state only when the worker answered.

func startStatusCheckWorker(t *testing.T, dataDir string, check, status func() updaterwire.Response) {
	t.Helper()
	path, err := updaterwire.SocketPath(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := wireserver.Listen(path, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = ln.Serve(func(req updaterwire.Request) updaterwire.Response {
			switch {
			case req.Verb == updaterwire.VerbCheck && req.Check != nil:
				return check()
			case req.Verb == updaterwire.VerbStatus && req.Status != nil:
				return status()
			}
			return updaterwire.Response{OK: false, Error: "unexpected verb"}
		})
	}()
	t.Cleanup(func() { _ = ln.Close(); <-done })
}

func getUpdatesStatus(t *testing.T, e *updEnv) map[string]any {
	t.Helper()
	w := e.do("GET", "/api/updates", "")
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestM420bUpdatesStatusAffirmsFreshCheckAndInstallState(t *testing.T) {
	t.Setenv(updaterKnobEnv, "1")
	e := newUpdEnv(t)
	state := "idle"
	startStatusCheckWorker(t, e.dataDir,
		func() updaterwire.Response {
			return updaterwire.Response{OK: true, State: "verified_ready", Detail: `{"available":true,"ready":true,"tag":"v2026.10.25.1"}`}
		},
		func() updaterwire.Response { return updaterwire.Response{OK: true, State: state} })

	// Nothing cached yet: the verdict keys are ABSENT (Unknown), never guessed.
	m := getUpdatesStatus(t, e)
	if _, has := m["update_available"]; has {
		t.Fatalf("no check yet, update_available must be absent: %v", m)
	}
	if m["install_state"] != "idle" {
		t.Fatalf("install_state must be read from the worker, got %v", m["install_state"])
	}

	// The manual check (production route) fills the cache.
	e.do("POST", "/api/updates/check", "{}")
	m = getUpdatesStatus(t, e)
	if m["update_available"] != true || m["latest_tag"] != "v2026.10.25.1" {
		t.Fatalf("fresh check must be affirmed: %v", m)
	}
	state = "busy"
	if m = getUpdatesStatus(t, e); m["install_state"] != "installing" {
		t.Fatalf("busy worker must read installing, got %v", m["install_state"])
	}

	// A cache older than 15 minutes is omitted again.
	e.s.updCheck.mu.Lock()
	e.s.updCheck.at = time.Now().Add(-16 * time.Minute)
	e.s.updCheck.mu.Unlock()
	m = getUpdatesStatus(t, e)
	if _, has := m["update_available"]; has {
		t.Fatalf("stale cache must be omitted: %v", m)
	}
	if _, has := m["latest_tag"]; has {
		t.Fatalf("stale cache must omit latest_tag: %v", m)
	}
}

func TestM420bBackgroundCheckFillsCacheAndHonoursRateLimit(t *testing.T) {
	e := newUpdEnv(t)
	calls := 0
	resp := updaterwire.Response{OK: true, State: "up_to_date", Detail: `{"tag":"v1"}`}
	startStatusCheckWorker(t, e.dataDir,
		func() updaterwire.Response { calls++; return resp },
		func() updaterwire.Response { return updaterwire.Response{OK: true, State: "idle"} })

	e.s.runBackgroundUpdateCheck(time.Now())
	if avail, tag, ok := e.s.updCheck.fresh(time.Now()); !ok || avail || tag != "v1" {
		t.Fatalf("background beat must cache up_to_date: %v %q %v", avail, tag, ok)
	}
	resp = updaterwire.Response{OK: true, State: "rate_limited"}
	e.s.runBackgroundUpdateCheck(time.Now())
	if calls != 2 {
		t.Fatalf("precondition: two relayed checks, got %d", calls)
	}
	e.s.runBackgroundUpdateCheck(time.Now())
	e.s.runBackgroundUpdateCheck(time.Now().Add(10 * time.Minute))
	if calls != 2 {
		t.Fatalf("after rate_limited the loop must stay silent, got %d calls", calls)
	}
	e.s.runBackgroundUpdateCheck(time.Now().Add(31 * time.Minute))
	if calls != 3 {
		t.Fatalf("after the backoff the loop resumes, got %d calls", calls)
	}
}
