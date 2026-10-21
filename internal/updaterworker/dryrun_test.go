package updaterworker

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"vl/internal/updaterjob"
	"vl/internal/updaterwire"
)

// waitStop polls the job file (real time; the runner's own waits are on the
// fake clock) until the job is finished or parked.
func (r *rig) waitStop(t *testing.T) updaterjob.Job {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if j, err := updaterjob.Read(r.data, boxJobID); err == nil {
			if updaterjob.Finished(j.State, j.Phase) || (j.State == updaterjob.StateNT8Updated && j.Phase == updaterjob.PhaseDone && j.Blocker != "") {
				r.w.mu.Lock()
				running := r.w.running
				r.w.mu.Unlock()
				if !running {
					return j
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the job never stopped: %v", states(r.job()))
	return updaterjob.Job{}
}

func (r *rig) report(t *testing.T, path string, c *updaterwire.Client, j updaterjob.Job) {
	t.Helper()
	t.Logf("[%s] states: %s", path, strings.Join(states(j), " → "))
	for i, rc := range j.Receipts {
		ev := make([]string, 0, len(rc.Evidence))
		for k, v := range rc.Evidence {
			ev = append(ev, k+"="+v)
		}
		sort.Strings(ev)
		ok := "ok"
		if !rc.OK {
			ok = "FAIL " + rc.Err
		}
		t.Logf("[%s]   receipt %2d %-15s %s  {%s}", path, i, rc.Step, ok, strings.Join(ev, " "))
	}
	s, st := ReadHoldFor(r.data, boxJobID)
	status := do(t, c, updaterwire.NewStatus(""))
	t.Logf("[%s] final %s/%s · hold %s · worker status %s · error %q · recovery_reason %q · calls %v",
		path, j.State, j.Phase, holdSummary(s, st), status.State, j.Error, j.RecoveryReason, r.calls)
}

// §5 DRY RUN: the whole state machine, driven by the REAL runner goroutine
// (Start), commanded over the REAL socket (wireserver + Dial), writing the
// REAL job file and the REAL hold file, reading the app through the REAL
// loopback reader (HTTPApp over an httptest app), against the FAKE library —
// never the live binary, systemd, :8080 or a live data dir. Five paths:
// happy, AddOn park + attended resume, Watch-timeout rollback, rollback that
// fails (recovery_needed), cancel before the boundary.
func TestDryRunWholeStateMachine(t *testing.T) {
	for _, p := range []struct {
		name  string
		opts  []rigOpt
		setup func(r *rig, c *updaterwire.Client)
		final updaterjob.State
	}{
		{name: "happy", final: updaterjob.StateComplete},
		{name: "park+resume", opts: []rigOpt{withCSChanged()}, final: updaterjob.StateComplete},
		{name: "watch-timeout rollback", setup: func(r *rig, _ *updaterwire.Client) { r.watchFail[boxNew] = true }, final: updaterjob.StateRolledBack},
		{name: "rollback fails", setup: func(r *rig, _ *updaterwire.Client) { r.watchFail[boxNew], r.rollbackFail = true, true }, final: updaterjob.StateRecoveryNeeded},
		// #206 review fold: the binary and RELEASE halves restore fine, the dist
		// half fails — the relaunch boots the old binary serving the FAILED
		// release's bundle. Only the dist proof can see it; before the fold
		// this ended rolled_back with the hold cleared.
		{name: "rollback fails at restore dist", setup: func(r *rig, _ *updaterwire.Client) { r.watchFail[boxNew], r.rollbackDistFail = true, true }, final: updaterjob.StateRecoveryNeeded},
		{name: "cancel", setup: func(r *rig, c *updaterwire.Client) {
			r.w.crash = func(q string) {
				if q == "preflight_ok/started" {
					if resp := do(t, c, updaterwire.NewCancelBeforeBoundary(boxJobID)); resp.State != "cancelled" {
						t.Errorf("cancel = %+v", resp)
					}
				}
			}
		}, final: updaterjob.StateCancelled},
	} {
		t.Run(p.name, func(t *testing.T) {
			r := newRig(t, p.opts...)
			c := r.serve(t)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			rep, err := r.w.Start(ctx)
			if err != nil || rep.Active != "" {
				t.Fatalf("Start = %+v, %v", rep, err)
			}
			if p.setup != nil {
				p.setup(r, c)
			}
			if resp := do(t, c, updaterwire.NewInstall(boxReleaseID, boxJobID)); !resp.OK || resp.State != "requested" {
				t.Fatalf("install = %+v", resp)
			}
			j := r.waitStop(t)
			if j.State == updaterjob.StateNT8Updated {
				t.Logf("[%s] parked: blocker %q · worker status %s", p.name, j.Blocker, do(t, c, updaterwire.NewStatus("")).State)
				r.f5()
				if resp := do(t, c, resumeRequest(boxJobID)); !resp.OK || resp.State != "resuming" {
					t.Fatalf("resume = %+v", resp)
				}
				for deadline := time.Now().Add(30 * time.Second); r.job().ResumedAt == nil; time.Sleep(5 * time.Millisecond) {
					if time.Now().After(deadline) {
						t.Fatalf("the runner never took the resume: %v", states(r.job()))
					}
				}
				j = r.waitStop(t)
			}
			r.report(t, p.name, c, j)
			r.noViolations(t)
			if j.State != p.final || j.Phase != updaterjob.PhaseDone {
				t.Fatalf("ended %s/%s, want %s/done", j.State, j.Phase, p.final)
			}
			if p.name == "rollback fails at restore dist" && !strings.Contains(j.Error, "served UI") {
				t.Fatalf("the dist half must be the proven failure: %q", j.Error)
			}
			if p.final == updaterjob.StateRecoveryNeeded {
				if s, _ := ReadHoldFor(r.data, boxJobID); s != HoldOurs {
					t.Fatalf("recovery_needed without the hold kept (%s)", s)
				}
				if resp := do(t, c, updaterwire.NewInstall(boxReleaseID, "job-u4-0005abcd")); resp.Error != "recovery needed" {
					t.Fatalf("install after recovery = %+v", resp)
				}
				for _, ln := range strings.Split(strings.TrimSpace(RecoveryText(j, r.cfg.Target)), "\n") {
					t.Logf("[%s] recovery | %s", p.name, ln)
				}
			} else if r.hold().Present {
				t.Fatalf("hold left behind: %+v", r.hold())
			}
			if p.name == "happy" {
				b, _ := json.MarshalIndent(updaterjob.View(j), "", "  ")
				t.Logf("[happy] API view (M5):\n%s", b)
				t.Logf("[happy] job file: %s", filepath.Join(r.data, "updater", "jobs", boxJobID+".json"))
			}
		})
	}
}

// PIN: the cutover token reaches NOTHING the worker writes — no job file, no
// receipt evidence, no error or blocker text, no backup or snapshot file, no
// log line — on the happy path, the rollback path, a 401 before the hold
// (refused) and a 401 after it (a blocker, then reverted_clean — self-recovery
// releases the hold and lock, nothing was installed). The reader itself never
// prints it (%v, %+v, %#v).
func TestNoTokenEverReachesTheJobFileOrEvidence(t *testing.T) {
	for _, p := range []struct {
		name  string
		setup func(r *rig)
		final updaterjob.State
	}{
		{name: "happy", final: updaterjob.StateComplete},
		{name: "rollback", setup: func(r *rig) { r.watchFail[boxNew] = true }, final: updaterjob.StateRolledBack},
		{name: "401 in preflight", setup: func(r *rig) { r.badToken = true }, final: updaterjob.StateRefused},
		{name: "401 after the hold", setup: func(r *rig) {
			r.w.crash = func(q string) {
				if q == "drained_acked/started" {
					r.mu.Lock()
					r.badToken = true
					r.mu.Unlock()
				}
			}
		}, final: updaterjob.StateRevertedClean},
		// verifier D5 / mutant A36: an app that answers non-200 with the
		// request's own Authorization header in its body — the worker's error
		// (a blocker, a receipt, the recovery reason) never carries the body
		{name: "500 echoing the header in preflight", setup: func(r *rig) { r.echo500 = true }, final: updaterjob.StateRefused},
		{name: "500 echoing the header after the hold", setup: func(r *rig) {
			r.w.crash = func(q string) {
				if q == "drained_acked/started" {
					r.mu.Lock()
					r.echo500 = true
					r.mu.Unlock()
				}
			}
		}, final: updaterjob.StateRevertedClean},
	} {
		t.Run(p.name, func(t *testing.T) {
			r := newRig(t)
			if p.setup != nil {
				p.setup(r)
			}
			j := r.runToEnd(t)
			if j.State != p.final {
				t.Fatalf("ended %s (error %q), want %s", j.State, j.Error, p.final)
			}
			if p.name == "401 in preflight" {
				if !strings.Contains(j.Error, "401") {
					t.Fatalf("a 401 in preflight is not named: %q", j.Error)
				}
				// refused AT ONCE: the token proof failing is not a blocker to wait out
				var started, ended time.Time
				for _, tr := range j.Transitions {
					if tr.State == updaterjob.StatePreflightOK && tr.Phase == updaterjob.PhaseStarted {
						started = tr.At
					}
					if tr.State == updaterjob.StateRefused {
						ended = tr.At
					}
				}
				if d := ended.Sub(started); d >= time.Minute {
					t.Fatalf("a 401 in preflight waited %s before refusing", d)
				}
			}
			if p.name == "401 after the hold" && !strings.Contains(j.Error, "401") {
				t.Fatalf("a 401 after the hold is not the blocker that stopped it: %q", j.Error)
			}
			haystack := allBytes(t, filepath.Dir(r.inst))
			haystack["worker log"] = []byte(r.log.String())
			jb, _ := json.Marshal(j)
			haystack["job (decoded)"] = jb
			haystack["reader %v"] = []byte(fmt.Sprintf("%v %+v %#v %s", r.app, r.app, r.app, r.app))
			for _, k := range sortedKeys(haystack) {
				if strings.Contains(string(haystack[k]), boxToken) || strings.Contains(string(haystack[k]), "Bearer ") {
					t.Fatalf("the cutover token reached %s", k)
				}
			}
			if len(haystack) < 10 {
				t.Fatalf("scanned only %d files", len(haystack))
			}
		})
	}
}
