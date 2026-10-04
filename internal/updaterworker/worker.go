package updaterworker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vl/internal/updaterjob"
	"vl/safe"
)

// Budgets are the step budgets (brief §3.1 "constants, not knobs"; OQ-2 as
// ruled). They are fields only so a test can shrink them; every wait is a
// Host.Sleep, never the wall clock.
type Budgets struct {
	PreflightFlat time.Duration // the pre-hold flat probe (C22): pass once within it, else refused
	Drain         time.Duration // drained_acked (C11), else recovery_needed with the hold kept
	Gate          time.Duration // gate_ok (R-q), else recovery_needed with the hold kept
	Reprove       time.Duration // R-i: ready re-proven before backup, nt8 and activate
	Watch         time.Duration // activation.Watch's within
	PostBootAck   time.Duration // boot_verified: the AddOn acks the new process (OQ-4)
	Poll          time.Duration // every re-read of the app
	IdentityRetry time.Duration // CurrentIdentity before a rollback
	HoldClear     time.Duration // complete/rolled_back: retry the clear this long
	PlannerWait   time.Duration // F2: a live AI-plan read is a benign blocker; wait
	// up to this long for it (preflight, drain, gate) instead of
	// failing on the step budget. Expiry: preflight refused (no
	// hold); drain/gate RELEASE the hold and refuse.
}

// DefaultBudgets are the ruled values.
func DefaultBudgets() Budgets {
	return Budgets{
		PreflightFlat: 10 * time.Minute,
		Drain:         10 * time.Minute,
		Gate:          10 * time.Minute,
		Reprove:       2 * time.Minute,
		Watch:         90 * time.Second,
		PostBootAck:   120 * time.Second,
		Poll:          2 * time.Second,
		IdentityRetry: 15 * time.Second,
		HoldClear:     30 * time.Second,
		PlannerWait:   15 * time.Minute,
	}
}

// Config is one worker's installation and budgets.
type Config struct {
	Target Target
	// BackupRoot is ~/vl-backups/updater: <BackupRoot>/<job>/data.db (the DB
	// backup) and <BackupRoot>/<job>/install/ (the snapshot of the install's
	// three halves the rollback restores). Absolute, outside the install.
	BackupRoot string
	Budgets    Budgets
	// Logf is the worker's log (stderr in serve). Nothing it is handed ever
	// carries the cutover token (TestNoTokenEverReachesTheJobFileOrEvidence).
	Logf func(format string, args ...any)
}

// Worker owns ONE durable update job at a time.
type Worker struct {
	cfg  Config
	lib  Library
	app  AppReader
	rel  Reverifier
	host Host

	// mu is THE job mutex: every job-file write (runner and socket) happens
	// under it, re-reading the file first, so a cancel and the runner's next
	// transition can never both land (brief §3.3 cancel boundary).
	mu      sync.Mutex
	active  string          // the one unfinished job ("" = idle)
	running bool            // the runner is inside drive()
	stopped string          // why the worker stopped (recovery_needed, a persist failure); install refused until restart (OQ-3)
	resume  map[string]bool // attended resume signals, consumed by the runner
	wake    chan struct{}

	// swapPending is set by the swap step once the worker binary was replaced
	// on disk. It does NOT exit the worker: the job must first persist the
	// swap receipt and run its terminal effects (hold + lock release).
	swapPending atomic.Bool
	// swapDone closes exactly once, when a job that swapped the worker binary
	// has FINISHED (finished()). cmd/vl-updater serve exits on it so systemd
	// restarts the worker on the new binary (the unit's Restart= policy
	// decides — see the PR body). Closing it any earlier lets the process
	// exit before the swap receipt and the complete transition are written,
	// and the restarted worker then re-runs worker_swapped until the attempts
	// cap sends the job to recovery_needed (job de4cf900, 2026-10-04).
	swapDone chan struct{}

	// crash is a TEST SEAM: called at every boundary with a point name
	// ("<state>/started", "<state>/effect", "<state>/done"); a test panics in
	// it to play a crash there. nil in production.
	crash func(point string)
}

// New builds a worker. Nothing runs until Start.
func New(cfg Config, d Deps) (*Worker, error) {
	if d.Lib == nil || d.App == nil || d.Rel == nil || d.Host == nil {
		return nil, errors.New("updaterworker: every dependency is required")
	}
	t := cfg.Target
	for name, p := range map[string]string{"install dir": t.InstallDir, "data dir": t.DataDir, "db file": t.DBFile, "log dir": t.LogDir, "backup root": cfg.BackupRoot} {
		if p == "" || !filepath.IsAbs(p) {
			return nil, fmt.Errorf("updaterworker: %s must be an absolute path", name)
		}
	}
	// the ONE containment check (PathWithin: path ELEMENTS on RESOLVED
	// paths): a backup root named through a symlink to the install is inside
	// it (U4F, the containment class); the root may not exist yet, so its
	// deepest existing ancestor is what gets resolved, and a dangling symlink
	// on the way refuses (U4F verify note 4)
	inside, err := PathWithin(cfg.BackupRoot, t.InstallDir)
	if err != nil {
		return nil, fmt.Errorf("updaterworker: the backup root %s cannot be checked against the install: %w", cfg.BackupRoot, err)
	}
	if inside {
		return nil, fmt.Errorf("updaterworker: the backup root %s is inside the install %s — a snapshot must survive the install it restores", cfg.BackupRoot, t.InstallDir)
	}
	if t.Port <= 0 || t.Port > 65535 {
		return nil, errors.New("updaterworker: target has no API port")
	}
	if cfg.Budgets == (Budgets{}) {
		cfg.Budgets = DefaultBudgets()
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	return &Worker{cfg: cfg, lib: d.Lib, app: d.App, rel: d.Rel, host: d.Host, resume: map[string]bool{}, wake: make(chan struct{}, 1), swapDone: make(chan struct{})}, nil
}

func (w *Worker) dataDir() string { return w.cfg.Target.DataDir }

func (w *Worker) logf(format string, args ...any) { w.cfg.Logf(format, args...) }

// boundary is the crash seam.
func (w *Worker) boundary(point string) {
	if w.crash != nil {
		w.crash(point)
	}
}

// errMoved: the job on disk is no longer at the (state, phase) the caller
// acted from — a cancel landed first. The caller stops; the next read sees it.
var errMoved = errors.New("updaterworker: the job moved under the runner")

// update is the ONLY job-file writer for an existing job: under the job
// mutex it re-reads the file, refuses unless the job is still at (state,
// phase), applies fn and writes. It returns what it wrote.
func (w *Worker) update(id string, state updaterjob.State, phase updaterjob.Phase, fn func(j *updaterjob.Job) error) (updaterjob.Job, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.updateLocked(id, state, phase, fn)
}

func (w *Worker) updateLocked(id string, state updaterjob.State, phase updaterjob.Phase, fn func(j *updaterjob.Job) error) (updaterjob.Job, error) {
	j, err := updaterjob.Read(w.dataDir(), id)
	if err != nil {
		return updaterjob.Job{}, err
	}
	if j.State != state || j.Phase != phase {
		return j, fmt.Errorf("%w: at %s/%s, not %s/%s", errMoved, j.State, j.Phase, state, phase)
	}
	if err := fn(&j); err != nil {
		return j, err
	}
	if err := updaterjob.Write(w.dataDir(), j); err != nil {
		return j, fmt.Errorf("persist %s: %w", id, err)
	}
	return j, nil
}

// now is the clock for a job's next record: never earlier than its last
// transition (a wall clock stepped back must not write a history that runs
// backwards — U1 fold: transition times are non-decreasing).
func (w *Worker) now(j updaterjob.Job) time.Time {
	t := w.host.Now()
	if n := len(j.Transitions); n > 0 && t.Before(j.Transitions[n-1].At) {
		return j.Transitions[n-1].At
	}
	if t.Before(j.UpdatedAt) {
		return j.UpdatedAt
	}
	return t
}

// Start is the start sweep (brief §3.4) and then the runner. It refuses when
// more than one unfinished job is on disk (only a bug makes two), and marks
// every unfinished job older than 30 minutes recovery_needed — never resumed
// (C21). It returns the start line's facts for the caller to print.
func (w *Worker) Start(ctx context.Context) (StartReport, error) {
	rep, err := w.sweep()
	if err != nil {
		return rep, err
	}
	safe.GoNamed("updater-worker-run", func() { w.run(ctx) })
	if rep.Active != "" {
		w.signal()
	}
	return rep, nil
}

// StartReport is what the start sweep found (printed READ, n/a when absent).
type StartReport struct {
	Active         string   // the unfinished job the runner resumes
	Recovery       []string // every recovery_needed job on disk (OQ-3: the start line lists them)
	StaleAtStart   []string // jobs this sweep moved to recovery_needed
	FinishedOthers int
}

func (w *Worker) sweep() (StartReport, error) {
	var rep StartReport
	jobs, skipped, err := updaterjob.ListTolerant(w.dataDir())
	if err != nil {
		return rep, fmt.Errorf("updaterworker: start sweep: %w", err)
	}
	for _, id := range skipped {
		// a TERMINAL job written by an older released worker may no longer
		// validate; it is skipped, never fatal (worker-self-update P0).
		w.logf("updater: start sweep: WARN terminal job %s is no longer validatable and was skipped", id)
	}
	var unfinished []updaterjob.Job
	for _, j := range jobs {
		switch {
		case j.State == updaterjob.StateRecoveryNeeded:
			rep.Recovery = append(rep.Recovery, j.JobID)
		case updaterjob.Finished(j.State, j.Phase):
			rep.FinishedOthers++
		case updaterjob.Stale(j, w.host.Now()):
			if err := w.toRecovery(j, "stale at start: older than 30 minutes in "+string(j.State)+"/"+string(j.Phase)+" — never resumed (C21)"); err != nil {
				return rep, err
			}
			rep.StaleAtStart = append(rep.StaleAtStart, j.JobID)
			rep.Recovery = append(rep.Recovery, j.JobID)
		default:
			unfinished = append(unfinished, j)
		}
	}
	if len(unfinished) > 1 {
		ids := make([]string, len(unfinished))
		for i, j := range unfinished {
			ids[i] = j.JobID
		}
		return rep, fmt.Errorf("updaterworker: %d unfinished jobs on disk (%s) — one at a time; refusing to start", len(ids), strings.Join(ids, ", "))
	}
	if len(unfinished) == 1 {
		w.mu.Lock()
		w.active = unfinished[0].JobID
		w.mu.Unlock()
		rep.Active = unfinished[0].JobID
	}
	if len(rep.StaleAtStart) > 0 {
		w.mu.Lock()
		w.stopped = "recovery_needed: " + strings.Join(rep.StaleAtStart, ", ")
		w.mu.Unlock()
	}
	return rep, nil
}

// toRecovery moves j (read from disk) to recovery_needed with a reason and
// the last OK receipt, under the job mutex. The hold is never touched.
func (w *Worker) toRecovery(j updaterjob.Job, reason string) error {
	_, err := w.update(j.JobID, j.State, j.Phase, func(k *updaterjob.Job) error {
		markRecovery(k, reason)
		return k.Enter(updaterjob.StateRecoveryNeeded, w.now(*k))
	})
	return err
}

// markRecovery sets the recovery fields: the reason, and the index of the
// last receipt that is OK (U1 fold: last_good_receipt only ever names an OK
// receipt; absent when there is none).
func markRecovery(j *updaterjob.Job, reason string) {
	j.RecoveryReason = reason
	j.LastGoodReceipt = nil
	for i := len(j.Receipts) - 1; i >= 0; i-- {
		if j.Receipts[i].OK {
			n := i
			j.LastGoodReceipt = &n
			break
		}
	}
}

func (w *Worker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// run is the runner goroutine: one job at a time, driven until it finishes,
// parks or stops. A persist failure stops the worker (fail closed: a job
// whose next transition cannot be written must not take its side effect).
func (w *Worker) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		}
		w.mu.Lock()
		id, stopped := w.active, w.stopped
		if id != "" && stopped == "" {
			w.running = true
		}
		w.mu.Unlock()
		if id == "" || stopped != "" {
			continue
		}
		err := w.drive(ctx, id)
		w.mu.Lock()
		w.running = false
		if err != nil && ctx.Err() == nil && w.stopped == "" {
			w.stopped = "runner error: " + err.Error()
		}
		w.mu.Unlock()
		if err != nil && ctx.Err() == nil {
			w.logf("updater: job %s: runner stopped: %v", id, err)
		}
	}
}
