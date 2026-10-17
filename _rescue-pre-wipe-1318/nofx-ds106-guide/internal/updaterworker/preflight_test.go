package updaterworker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"vl/internal/updaterjob"
)

// Every preflight refusal (brief row 4, C19, C20, C22) ends the job REFUSED
// with NO hold written and nothing changed — the owner's calendar file is
// byte-identical afterwards (it is never overwritten).
func TestPreflightRefusalsNeverHold(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(r *rig)
		want  string
	}{
		{"C22: not flat before the hold", func(r *rig) { r.flat = false }, "addon_census_prehold"},
		{"C19: the main-tree lock is held by another session", func(r *rig) { r.lockHolder = "other-lane" }, "main-tree lock is not held by this job (C19: held by \"other-lane\")"},
		{"C20: the calendar differs", func(r *rig) {
			writeFile(r.t, filepath.Join(r.inst, calendarFile), `[{"time":"2026-10-01T12:30:00Z","title":"owner edit"}]`+"\n")
		}, "calendar_static_t1.json differs"},
		// U4 re-verify note 4 (mutant M18b): the release carries a calendar
		// the install lacks — it cannot be compared, so it refuses (and the
		// worker never copies the template itself)
		{"C20: the release has a calendar the install lacks", func(r *rig) {
			if err := os.Remove(filepath.Join(r.inst, calendarFile)); err != nil {
				r.t.Fatal(err)
			}
		}, "cannot be compared with the release's"},
		{"the install binary is dirty", func(r *rig) {
			writeFile(r.t, filepath.Join(r.inst, "nofx-bin"), "NOFXBIN rev="+boxOld+" modified=true\n")
		}, "modified=true"},
		{"the RELEASE marker names another build", func(r *rig) {
			writeFile(r.t, filepath.Join(r.inst, "deploy", "RELEASE"), strings.Repeat("c3", 20)+"\n")
		}, "RELEASE marker names"},
		{"the unit runs another binary", func(r *rig) { r.exe = "/opt/other/nofx-bin (deleted)" }, "not the install's"},
		{"health serves another build", func(r *rig) { r.running = strings.Repeat("d4", 20) }, "the app serves"},
		{"no bot database", func(r *rig) { os.Remove(filepath.Join(r.data, "data.db")) }, "no bot database"},
		{"the release IS the install", func(r *rig) {
			writeFile(r.t, filepath.Join(r.inst, "nofx-bin"), binaryBody(boxNew))
		}, "already this release"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			c.setup(r)
			cal, _ := os.ReadFile(filepath.Join(r.inst, calendarFile))
			r.install()
			if err := r.drive(); err != nil {
				t.Fatal(err)
			}
			j := r.job()
			if j.State != updaterjob.StateRefused || !strings.Contains(j.Error, c.want) {
				t.Fatalf("job %s (error %q), want refused because %q", j.State, j.Error, c.want)
			}
			if r.callCount("hold_write") != 0 || r.hold().Present {
				t.Fatal("a refused preflight wrote a hold")
			}
			if after, _ := os.ReadFile(filepath.Join(r.inst, calendarFile)); string(after) != string(cal) {
				t.Fatal("the owner's calendar file was changed")
			}
		})
	}
}

// The preflight leg list must demand the PRE-HOLD census leg, never the drain
// one: addon_census can never pass before a hold exists (the wire only sends
// maintenance frames while held), so demanding it refuses every install on a
// fresh bot process. (#206 review fold.)
func TestPreflightFlatLegsUseThePreholdCensus(t *testing.T) {
	found := false
	for _, n := range preflightFlatLegs {
		if n == "addon_census" {
			t.Fatalf("preflight must not demand the drain census leg: %v", preflightFlatLegs)
		}
		found = found || n == "addon_census_prehold"
	}
	if !found {
		t.Fatalf("preflight must demand the prehold census leg: %v", preflightFlatLegs)
	}
}

// C22 on a FRESH never-held connection (the AddOn sends no maintenance frame
// before any hold): preflight must PASS when the box is otherwise flat — the
// normal production path, since every activation restarts the bot. The job
// then holds and drain (which needs a real ack) fails: recovery_needed with
// the hold kept, but NOT the preflight refusal the old addon_census demand
// produced. (#206 review fold.)
func TestPreflightPassesOnANeverHeldConnection(t *testing.T) {
	r := newRig(t)
	r.addonConnected = false // no wire ack at all: this connection never held
	r.install()
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	j := r.job()
	if j.State == updaterjob.StateRefused {
		t.Fatalf("preflight refused a flat never-held connection: %q", j.Error)
	}
	if strings.Contains(j.Error, "addon_census") {
		t.Fatalf("the census must not block preflight on a never-held connection: %q", j.Error)
	}
	if r.callCount("hold_write") != 1 {
		t.Fatalf("preflight must pass and write the hold; hold_write ran %d times (job %s)", r.callCount("hold_write"), j.State)
	}
}

// prehold-stale-census (owner order 2026-10-02 10:15 CT): the worker reaches
// the hold step when the gate's prehold leg passes on a stale flat census —
// the held:false release ack left by the PREVIOUS install's hold on this
// process's connection, which the wire never refreshes. The rig's gateView
// MIRRORS the production rule (the real leg is an unexported closure inside
// trader.InstallationGateStatus, so the rig cannot call it without dragging
// the bot's store/TCP fixtures into the worker package); the PRODUCTION rule
// is pinned by the trader package's own
// TestInstallationGatePreholdCensusStale* tests — under CTO mutant B (the
// production fix reverted) THOSE go RED while this test stays green, which is
// exactly its job: it proves the worker flows, not the rule.
func TestPreflightStaleFlatCensusReachesTheHold(t *testing.T) {
	r := newRig(t)
	r.ackStale = true // 20 s old — past the 15 s freshness max; the census itself is flat
	r.install()
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	j := r.job()
	if j.State == updaterjob.StateRefused {
		t.Fatalf("preflight refused a stale flat census: %q", j.Error)
	}
	if strings.Contains(j.Error, "addon_census_prehold") {
		t.Fatalf("the stale flat census must not block preflight: %q", j.Error)
	}
	if r.callCount("hold_write") != 1 {
		t.Fatalf("preflight must pass and write the hold; hold_write ran %d times (job %s)", r.callCount("hold_write"), j.State)
	}
}

// brief §3.4.3: a step that keeps crashing is retried at most MaxAttempts
// times in all, then recovery_needed (the hold kept) — never a crash loop.
func TestAStepThatKeepsCrashingIsRecoveryNeeded(t *testing.T) {
	r := newRig(t)
	crashAt := func(q string) {
		if q == "gate_ok/started" {
			panic(crashPanic{q})
		}
	}
	r.w.crash = crashAt
	r.runCrashing(t)
	for i := 0; i < updaterjob.MaxAttempts; i++ {
		r.w = r.newWorker()
		r.w.crash = crashAt
		if _, err := r.w.sweep(); err != nil {
			t.Fatal(err)
		}
		r.runCrashing(t)
	}
	j := r.job()
	if j.State != updaterjob.StateRecoveryNeeded || !strings.Contains(j.RecoveryReason, "attempts cap") {
		t.Fatalf("job %s (reason %q) after %d crashes in one step", j.State, j.RecoveryReason, updaterjob.MaxAttempts+1)
	}
	if s, _ := ReadHoldFor(r.data, boxJobID); s != HoldOurs {
		t.Fatalf("hold %s", s)
	}
}

// OQ-3 as ruled: recovery_needed refuses install until the worker restarts;
// the (attended) restart is the acknowledgement: the start sweep lists the
// job and the fresh worker accepts a new install.
func TestRestartAcknowledgesRecovery(t *testing.T) {
	r := newRig(t)
	r.watchFail[boxNew], r.rollbackFail = true, true
	if j := r.runToEnd(t); j.State != updaterjob.StateRecoveryNeeded {
		t.Fatalf("job %s", j.State)
	}
	r.w = r.newWorker()
	rep, err := r.w.sweep()
	if err != nil || len(rep.Recovery) != 1 || rep.Recovery[0] != boxJobID || rep.Active != "" {
		t.Fatalf("start sweep = %+v, %v", rep, err)
	}
	// the operator restored and cleared the hold (recovery text), then restarted
	if err := ReleaseJob(r.data, boxJobID); err != nil {
		t.Fatal(err)
	}
	if resp := r.w.Handle(updaterwireInstallOf("job-u4-0006abcd")); !resp.OK || resp.State != "requested" {
		t.Fatalf("install after the restart = %+v", resp)
	}
}

// The reader talks to loopback only, sends the Bearer only on the two
// authenticated views (never to /api/health or /), never follows a redirect
// (the Bearer would ride it), and needs the token from the environment.
func TestHTTPAppIsLoopbackOnly(t *testing.T) {
	t.Setenv(CutoverTokenEnv, boxToken)
	for _, base := range []string{"http://10.0.0.2:8080", "https://127.0.0.1:8080", "http://example.test", "http://127.0.0.1:8080/api"} {
		if _, err := NewHTTPApp(base); err == nil {
			t.Fatalf("NewHTTPApp(%q) accepted", base)
		}
	}
	var mu sync.Mutex
	seen := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path] = r.Header.Get("Authorization")
		mu.Unlock()
		switch r.URL.Path {
		case "/api/health":
			w.Write([]byte(`{"status":"ok","revision":"a1a1a1a1a1a1"}`))
		case "/api/maintenance":
			http.Redirect(w, r, "http://127.0.0.1:1/elsewhere", http.StatusFound)
		case "/api/installation-gate":
			w.Write([]byte(`{"ready":true,"job_id":"n/a","legs":[]}`))
		case "/":
			w.Write([]byte("<html>"))
		}
	}))
	defer srv.Close()
	a, err := NewHTTPApp(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if h, err := a.Health(ctx); err != nil || h != "a1a1a1a1a1a1" {
		t.Fatalf("health %q %v", h, err)
	}
	if _, err := a.InstallationGate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Maintenance(ctx); err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("a redirect was followed or accepted: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen["/api/health"] != "" || seen["/"] != "" || seen["/api/installation-gate"] != "Bearer "+boxToken || seen["/api/maintenance"] != "Bearer "+boxToken {
		t.Fatalf("Authorization sent per path: %q", seen)
	}
	if _, ok := seen["/elsewhere"]; ok {
		t.Fatal("the redirect target was contacted")
	}
	os.Unsetenv(CutoverTokenEnv)
	if _, err := NewHTTPApp(srv.URL); err != ErrNoToken {
		t.Fatalf("no token: %v", err)
	}
}
