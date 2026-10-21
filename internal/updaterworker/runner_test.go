package updaterworker

import (
	"strings"
	"testing"
	"time"

	"vl/internal/updaterjob"
)

// runToEnd installs and drives the job; a park is resumed once (after the
// owner's F5: the AddOn now runs the manifest's build).
func (r *rig) runToEnd(t *testing.T) updaterjob.Job {
	t.Helper()
	r.install()
	if err := r.drive(); err != nil {
		t.Fatalf("drive: %v", err)
	}
	if j := r.job(); j.State == updaterjob.StateNT8Updated && j.Phase == updaterjob.PhaseDone {
		r.f5()
		if resp := r.w.Handle(resumeRequest(boxJobID)); !resp.OK {
			t.Fatalf("resume: %+v", resp)
		}
		if err := r.drive(); err != nil {
			t.Fatalf("drive after resume: %v", err)
		}
	}
	return r.job()
}

// f5 plays the owner's attended AddOn update: minutes pass, the AddOn now
// runs the release's build.
func (r *rig) f5() {
	r.clock.Advance(3 * time.Minute)
	r.mu.Lock()
	r.addonBuild = r.manifestBuild
	r.addonSeq++ // F5 + NT8 restart: the AddOn reconnects on a NEW connection
	r.mu.Unlock()
}

// driveActive drives the worker's CURRENT active job (a catch-up job created
// by maybeStartCatchup, whose id is not the fixed boxJobID).
func (r *rig) driveActive() error {
	r.w.mu.Lock()
	id := r.w.active
	r.w.mu.Unlock()
	if id == "" {
		return nil
	}
	return r.w.drive(r.t.Context(), id)
}

// runCatchup starts a catch-up job (when owed) and drives it to its end.
func (r *rig) runCatchup(t *testing.T) (started bool, j updaterjob.Job) {
	t.Helper()
	ok, err := r.w.maybeStartCatchup(t.Context())
	if err != nil {
		t.Fatalf("maybeStartCatchup: %v", err)
	}
	if !ok {
		return false, updaterjob.Job{}
	}
	r.w.mu.Lock()
	id := r.w.active
	r.w.mu.Unlock()
	if id == "" {
		t.Fatal("no active job after maybeStartCatchup")
	}
	if err := r.w.drive(t.Context(), id); err != nil {
		t.Fatalf("drive catch-up: %v", err)
	}
	k, err := updaterjob.Read(r.data, id)
	if err != nil {
		t.Fatal(err)
	}
	return true, k
}

// PIN (dispatch §4(2)): every transition is persisted BEFORE its side
// effect. Each fake side effect re-reads the job file and records a violation
// unless its own state is on disk "started" (and, after the hold write, this
// job's hold is on disk). Three paths cover every effect: the happy path
// (nt8_skipped), the one-click AddOn path (nt8_updated → build+restart), and a
// Watch that never proves the new build (rollback).
func TestEveryTransitionPersistsBeforeItsSideEffect(t *testing.T) {
	for _, c := range []struct {
		name  string
		opts  []rigOpt
		setup func(r *rig)
		final updaterjob.State
		calls []string
	}{
		{name: "happy path, AddOn unchanged", final: updaterjob.StateComplete,
			calls: []string{"rehash", "resolve", "stage", "hold_write", "backup", "snapshot", "activate", "watch", "hold_clear"}},
		{name: "C# changed: one-click AddOn build+restart", opts: []rigOpt{withCSChanged()}, final: updaterjob.StateComplete,
			calls: []string{"rehash", "resolve", "stage", "hold_write", "backup", "snapshot", "activate", "watch", "hold_clear"}},
		{name: "Watch never proves the release: rollback", setup: func(r *rig) { r.watchFail[boxNew] = true }, final: updaterjob.StateRolledBack,
			calls: []string{"rehash", "resolve", "stage", "hold_write", "backup", "snapshot", "activate", "watch", "rollback", "watch", "hold_clear"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, c.opts...)
			if c.setup != nil {
				c.setup(r)
			}
			j := r.runToEnd(t)
			r.noViolations(t)
			if j.State != c.final || j.Phase != updaterjob.PhaseDone {
				t.Fatalf("job ended %s/%s (error %q, blocker %q), want %s/done\nhistory %v", j.State, j.Phase, j.Error, j.Blocker, c.final, states(j))
			}
			if got := strings.Join(r.calls, ","); got != strings.Join(c.calls, ",") {
				t.Fatalf("side effects ran %s\nwant             %s", got, strings.Join(c.calls, ","))
			}
			if r.hold().Present {
				t.Fatalf("the hold survives a finished job: %+v", r.hold())
			}
			t.Logf("history: %s", strings.Join(states(j), " → "))
			t.Logf("receipts: %s", strings.Join(receiptSteps(j), ", "))
		})
	}
}

// A rolled-back job keeps the error that sent it back (M5 renders it).
func TestRolledBackKeepsWhyItRolledBack(t *testing.T) {
	r := newRig(t)
	r.watchFail[boxNew] = true
	j := r.runToEnd(t)
	if j.State != updaterjob.StateRolledBack || !strings.Contains(j.Error, "not proven within") {
		t.Fatalf("rolled_back with error %q", j.Error)
	}
}
