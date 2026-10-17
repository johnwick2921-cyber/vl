package updaterworker

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"

	"vl/internal/updaterjob"
	"vl/internal/updaterwire"
	"vl/internal/updaterwire/wireserver"
)

// serve puts the rig's worker behind the REAL socket (wireserver.Listen on
// <data>/updater/<socket>, Serve with w.Handle) and returns a dialled client.
func (r *rig) serve(t *testing.T) *updaterwire.Client {
	t.Helper()
	path, err := updaterwire.SocketPath(r.data)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := wireserver.Listen(path, func(format string, a ...any) { r.cfg.Logf(format, a...) })
	if err != nil {
		t.Fatal(err)
	}
	go ln.Serve(r.w.Handle)
	t.Cleanup(func() { ln.Close() })
	c, err := updaterwire.DialWorker(r.data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func do(t *testing.T, c *updaterwire.Client, req updaterwire.Request) updaterwire.Response {
	t.Helper()
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", req.Verb, err)
	}
	return resp
}

// PIN (#206 note socket.go:181): a stopped worker's runner never acts on a
// resume wake, so the verb must refuse — the old code answered ok("resuming")
// although nothing would happen until a restart.
func TestSocketResumeRefusesWhileStopped(t *testing.T) {
	r := newRig(t)
	r.install()
	c := r.serve(t)
	// the runner hit a persist failure: the job stays and the worker stops
	r.w.mu.Lock()
	r.w.stopped = "recovery_needed: " + boxJobID
	r.w.mu.Unlock()
	resp := do(t, c, resumeRequest(boxJobID))
	if resp.OK || resp.Error != "recovery needed" {
		t.Fatalf("resume while stopped = %+v, want the recovery-needed refusal", resp)
	}
}

// PIN (dispatch §3, §4(10), C14 as ruled): over the REAL socket, the four
// verbs are authenticated by the worker's own job file — a job id that is not
// the active job is refused, a cancel past the boundary (maintenance_held on)
// is refused and changes nothing, a resume outside nt8_updated/done is
// refused, and an unknown verb never reaches the handler. (A foreign peer uid
// is refused per connection by wireserver before a byte is read — M3's own
// pin, server_test.go.)
func TestSocketRefusesJobMismatchCancelPastBoundaryResumeOutsideNT8Updated(t *testing.T) {
	r := newRig(t)
	c := r.serve(t)
	must := func(req updaterwire.Request, ok bool, want string) {
		t.Helper()
		resp := do(t, c, req)
		got := resp.State
		if !resp.OK {
			got = resp.Error
		}
		if resp.OK != ok || got != want {
			t.Fatalf("%s %+v → %+v, want ok=%v %q", req.Verb, req, resp, ok, want)
		}
	}
	must(updaterwire.NewStatus(""), true, StatusIdle)
	must(updaterwire.NewStatus(boxJobID), false, "unknown job")
	must(updaterwire.NewInstall("v9.9.9", boxJobID), false, "release not verified")
	must(updaterwire.NewInstall(boxReleaseID, boxJobID), true, "requested")
	must(updaterwire.NewInstall(boxReleaseID, boxJobID), true, "requested") // an idempotent re-send answers its state
	must(updaterwire.NewInstall(boxReleaseID, "job-u4-0002abcd"), false, "busy")
	must(updaterwire.NewStatus(boxJobID), true, "requested")
	must(updaterwire.NewStatus(""), true, StatusBusy)
	// a job file that is not the active job
	other, _ := updaterjob.New("job-u4-0002abcd", boxReleaseID, r.clock.Now())
	if err := updaterjob.Write(r.data, other); err != nil {
		t.Fatal(err)
	}
	must(updaterwire.NewCancelBeforeBoundary("job-u4-0002abcd"), false, "job mismatch")
	must(resumeRequest("job-u4-0002abcd"), false, "job mismatch")
	must(updaterwire.NewCancelBeforeBoundary("job-u4-0009abcd"), false, "unknown job")
	must(resumeRequest(boxJobID), false, "not resumable") // requested is not the park

	// past the boundary: the job holds (crash the runner at drained_acked)
	r.w.crash = func(q string) {
		if q == "drained_acked/started" {
			panic(crashPanic{q})
		}
	}
	r.runCrashing(t)
	before := r.job()
	must(updaterwire.NewCancelBeforeBoundary(boxJobID), false, "past boundary")
	must(resumeRequest(boxJobID), false, "not resumable")
	if after := r.job(); len(after.Transitions) != len(before.Transitions) || after.State != updaterjob.StateDrainedAcked {
		t.Fatalf("a refused verb changed the job: %s → %s", before.State, after.State)
	}
	if s, _ := ReadHoldFor(r.data, boxJobID); s != HoldOurs {
		t.Fatalf("a refused cancel dropped the hold (%s)", s)
	}

	// an unknown verb is refused by the wire before the handler
	path, _ := updaterwire.SocketPath(r.data)
	raw, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetDeadline(time.Now().Add(5 * time.Second))
	raw.Write([]byte(`{"v":1,"verb":"start_install","payload":{"job_id":"` + boxJobID + `"}}` + "\n"))
	line, _ := bufio.NewReader(raw).ReadString('\n')
	if !strings.Contains(line, `"rejected"`) {
		t.Fatalf("unknown verb answered %q", line)
	}
}

// The cancel boundary under the job mutex: a cancel that lands while the
// preflight runs (before maintenance_held is written) wins — the runner's
// next write re-reads the file, finds cancelled, and stops. No hold is
// ever written.
func TestCancelDuringPreflightNeverHolds(t *testing.T) {
	r := newRig(t)
	c := r.serve(t)
	r.install()
	r.w.crash = func(q string) {
		if q == "preflight_ok/effect" {
			if resp := do(t, c, updaterwire.NewCancelBeforeBoundary(boxJobID)); !resp.OK || resp.State != "cancelled" {
				t.Errorf("cancel during preflight = %+v", resp)
			}
		}
	}
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	j := r.job()
	if j.State != updaterjob.StateCancelled {
		t.Fatalf("job %s, want cancelled\n%v", j.State, states(j))
	}
	if r.callCount("hold_write") != 0 || r.hold().Present {
		t.Fatal("a cancelled job wrote a hold")
	}
	if resp := do(t, c, updaterwire.NewStatus("")); resp.State != StatusIdle {
		t.Fatalf("after cancel: %+v", resp)
	}
}
