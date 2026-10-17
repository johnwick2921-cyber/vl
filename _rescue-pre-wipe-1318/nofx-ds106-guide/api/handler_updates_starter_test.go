package api

// W-ONE-BUTTON M4 3b-B U5b (c) — knob ON, install hands off over the worker
// socket, at the production call site: the gin install route through the
// real gate, the production verdict (updaterworker.FetchRelease), and a REAL
// wireserver listening on <data>/updater/worker.sock in the temp data dir.
// Accepted: OK with state "requested", or — a re-send — OK with the job's
// own state as its job file (updaterjob's production writer) records it for
// THIS release. Anything else, or no worker, is M3's 503 — including a
// re-send whose job file the worker's production writer has walked to a
// TERMINAL state (complete, cancelled) that the worker echoes: a finished
// job is never "accepted" again (U5b verifier D1, mutant M4).

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"vl/internal/updaterjob"
	"vl/internal/updaterwire"
)

func TestInstallHandsOffOverTheSocket(t *testing.T) {
	const installUnavailable = `{"error":"installer unavailable"}`
	cases := []struct {
		name      string
		worker    bool
		answer    func(g0 string) (bool, string) // g0: the grant's job id
		jobFile   string                         // "" none; else the release id the job file names
		walk      []updaterjob.State             // nil: writeTestJob (downloaded/done); else walkTestJob through these states
		wantCode  int
		wantCalls int
	}{
		{"worker accepts: requested", true, func(string) (bool, string) { return true, "requested" }, "", nil, http.StatusAccepted, 1},
		{"worker refuses", true, func(string) (bool, string) { return false, "busy" }, "", nil, http.StatusServiceUnavailable, 1},
		{"worker says another state, no job file", true, func(string) (bool, string) { return true, "downloaded" }, "", nil, http.StatusServiceUnavailable, 1},
		{"re-send: the job's own state, same release", true, func(string) (bool, string) { return true, "downloaded" }, updRelease, nil, http.StatusAccepted, 1},
		{"re-send: a state the job file does not hold", true, func(string) (bool, string) { return true, "gate_ok" }, updRelease, nil, http.StatusServiceUnavailable, 1},
		{"re-send: the job file names another release", true, func(string) (bool, string) { return true, "downloaded" }, "v2026.09.24-7", nil, http.StatusServiceUnavailable, 1},
		{"re-send: the job file walked to complete, the worker echoes it", true, func(string) (bool, string) { return true, "complete" }, updRelease, completeWalk, http.StatusServiceUnavailable, 1},
		{"re-send: the job file walked to cancelled, the worker echoes it", true, func(string) (bool, string) { return true, "cancelled" }, updRelease, []updaterjob.State{updaterjob.StateCancelled}, http.StatusServiceUnavailable, 1},
		{"no worker", false, nil, "", nil, http.StatusServiceUnavailable, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(updaterKnobEnv, "1")
			e := newUpdEnv(t)
			fetchTestRelease(t, e.dataDir, updRelease)
			g := e.grant(updRelease)
			switch {
			case c.jobFile != "" && c.walk != nil:
				walkTestJob(t, e.dataDir, g.JobID, c.jobFile, c.walk...)
			case c.jobFile != "":
				writeTestJob(t, e.dataDir, g.JobID, c.jobFile)
			}
			var w *testWorker
			if c.worker {
				w = startTestWorker(t, e.dataDir, func(string, string) (bool, string) { return c.answer(g.JobID) })
			}
			resp := e.do("POST", "/api/updates/install", grantBody(g))
			if resp.Code != c.wantCode {
				t.Fatalf("install = %d %s, want %d", resp.Code, resp.Body.String(), c.wantCode)
			}
			switch c.wantCode {
			case http.StatusAccepted:
				var body map[string]any
				if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil || len(body) != 1 || body["job_id"] != g.JobID {
					t.Fatalf("202 body = %s, want exactly {\"job_id\":%q}", resp.Body.String(), g.JobID)
				}
			case http.StatusServiceUnavailable:
				if resp.Body.String() != installUnavailable {
					t.Fatalf("503 body = %s, want M3's %s", resp.Body.String(), installUnavailable)
				}
			}
			if w != nil {
				installs, other := w.calls()
				if len(installs) != c.wantCalls || len(other) != 0 {
					t.Fatalf("the worker saw installs=%v other verbs=%v, want %d install frame(s) and nothing else", installs, other, c.wantCalls)
				}
				if installs[0] != (updaterwire.InstallPayload{ReleaseID: updRelease, JobID: g.JobID}) {
					t.Fatalf("the worker got %+v, want exactly the grant's {release_id %s, job_id %s}", installs[0], updRelease, g.JobID)
				}
			}
		})
	}
}

// completeWalk is the success path from requested to complete (the AddOn
// decision taken as nt8_skipped).
var completeWalk = []updaterjob.State{
	updaterjob.StateDownloaded, updaterjob.StateVerified, updaterjob.StatePreflightOK,
	updaterjob.StateMaintenanceHeld, updaterjob.StateDrainedAcked, updaterjob.StateGateOK,
	updaterjob.StateBackupDone, updaterjob.StateNT8Skipped, updaterjob.StateActivated,
	updaterjob.StateBooted, updaterjob.StateBootVerified, updaterjob.StateWorkerSwapped,
	updaterjob.StateComplete,
}

// walkTestJob writes a job with updaterjob's PRODUCTION writer and walks it
// through `to` the way the worker's runner does: Enter → Write (persisted
// before the effect) and, for a state with a side effect, a receipt → Finish
// → Write. It reads the file back and asserts it holds the last state, and
// that the state is terminal when the caller walked to one.
func walkTestJob(t *testing.T, dataDir, jobID, releaseID string, to ...updaterjob.State) updaterjob.Job {
	t.Helper()
	now := time.Date(2026, 9, 24, 18, 2, 11, 0, time.UTC)
	j, err := updaterjob.New(jobID, releaseID, now)
	if err != nil {
		t.Fatalf("updaterjob.New: %v", err)
	}
	if err := updaterjob.Write(dataDir, j); err != nil {
		t.Fatalf("updaterjob.Write (requested): %v", err)
	}
	// What each step reads or writes, persisted with its state the way the
	// worker does (Validate refuses an nt8_* state without its decision and
	// an activated job without its rollback inputs).
	base := t.TempDir()
	half := func(dir, sha string) *updaterjob.Release {
		return &updaterjob.Release{Dir: dir, SHA: sha, Binary: dir + "/nofx", Dist: dir + "/web/dist", ReleaseFile: dir + "/deploy/RELEASE"}
	}
	const newSHA, oldSHA = "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"
	for i, s := range to {
		now = now.Add(time.Second)
		if err := j.Enter(s, now); err != nil {
			t.Fatalf("step %d: Enter %s: %v", i, s, err)
		}
		switch s {
		case updaterjob.StatePreflightOK:
			j.SourceSHA, j.Release = newSHA, half(base+"/releases/"+releaseID, newSHA)
			j.Install = half(base+"/install", oldSHA)
		case updaterjob.StateBackupDone:
			j.Snapshot, j.BackupPath = half(base+"/snapshot", oldSHA), base+"/backup.db"
		case updaterjob.StateNT8Skipped:
			j.NT8 = &updaterjob.NT8Decision{Decision: updaterjob.NT8Skipped}
		case updaterjob.StateActivated:
			j.IdentityBefore = &updaterjob.Identity{PID: 4242, StartTicks: 1}
		}
		if err := updaterjob.Write(dataDir, j); err != nil {
			t.Fatalf("step %d: updaterjob.Write (%s/%s): %v", i, j.State, j.Phase, err)
		}
		if j.Phase != updaterjob.PhaseStarted {
			continue
		}
		r := updaterjob.Receipt{Step: string(s), StartedAt: now, EndedAt: now.Add(time.Second), OK: true}
		now = now.Add(time.Second)
		if err := j.AddReceipt(r, now); err != nil {
			t.Fatalf("step %d: AddReceipt %s: %v", i, s, err)
		}
		if err := j.Finish(now); err != nil {
			t.Fatalf("step %d: Finish %s: %v", i, s, err)
		}
		if err := updaterjob.Write(dataDir, j); err != nil {
			t.Fatalf("step %d: updaterjob.Write (%s/done): %v", i, s, err)
		}
	}
	got, err := updaterjob.Read(dataDir, jobID)
	if err != nil {
		t.Fatalf("updaterjob.Read after the production writer: %v", err)
	}
	if last := to[len(to)-1]; got.State != last || got.ReleaseID != releaseID {
		t.Fatalf("job file holds %s for %q, want %s for %q", got.State, got.ReleaseID, last, releaseID)
	}
	if !updaterjob.Finished(got.State, got.Phase) {
		t.Fatalf("job file holds %s/%s: the walk did not finish a terminal state", got.State, got.Phase)
	}
	return got
}

// With the knob ON the status reports what the server now holds — the four
// keys (the M3 key-set plus worker_listening), install enabled because a real
// verifier and a starter are wired, and worker_listening MEASURED: the route
// dials the worker socket at request time with a 250 ms bound, so with no
// worker running it is false (a measured value, never inferred — CTO ruling
// on #206).
func TestUpdatesStatusWithTheKnobOn(t *testing.T) {
	t.Setenv(updaterKnobEnv, "1")
	e := newUpdEnv(t)
	w := e.do("GET", "/api/updates", "")
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "enrolled,install_enabled,manifest_verifier,worker_listening" {
		t.Fatalf("knob ON: GET /api/updates keys = %v", keys)
	}
	if m["enrolled"] != true || m["install_enabled"] != true || m["manifest_verifier"] != "configured" {
		t.Fatalf("knob ON: GET /api/updates = %v", m)
	}
	if m["worker_listening"] != false {
		t.Fatalf("no worker is listening in this test, yet worker_listening = %v (a measured value, never inferred)", m["worker_listening"])
	}
}

// The true half of the ruling: a worker socket that accepts the bounded dial
// makes worker_listening true — the route measured it at request time.
func TestUpdatesStatusWorkerListeningTrueWithAWorker(t *testing.T) {
	t.Setenv(updaterKnobEnv, "1")
	e := newUpdEnv(t)
	path, err := updaterwire.SocketPath(e.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil { // CheckSocketFile: owner-only perms
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close(); os.Remove(path) })
	go func() { // the worker's listener: accept the probe and let it hang up
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			c.Close() // a frameless close is a clean EOF for the prober
		}
	}()
	w := e.do("GET", "/api/updates", "")
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["worker_listening"] != true {
		t.Fatalf("a worker is listening, yet worker_listening = %v: %s", m["worker_listening"], w.Body.String())
	}
	if m["install_enabled"] != true {
		t.Fatalf("install_enabled = %v alongside the listening worker", m["install_enabled"])
	}
}
