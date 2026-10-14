package trader

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vl/internal/updaterjob"
	"vl/kernel"
	"vl/logger"
	"vl/telemetry"
)

// ── INSTALL PENDING — freeze NEW entries while an update job is in preflight ─
//
// Release #22 OPS-SAFETY. The updater's preflight waits (up to 10 min, 15 while
// a plan read runs) for the bot to be FLAT before it writes the maintenance
// hold. Nothing told the bot a job was waiting, so the bot kept placing: on
// 10-14 job 30a8bbf6 sat in preflight_ok/started 01:39:53→01:49:53 CT, trader
// 12345 placed armed S1 at 01:44:00 (vl_2026-10-13.log 64591), the cancel was
// never acked, and the job was refused on ledger_exposure.
//
// There is no updater→bot push channel before the hold (the worker only READS
// the bot's /api/installation-gate and /api/maintenance, and the Updates page
// reads the gate too, so a gate read cannot mean "a job is waiting"). So the
// bot READS the job files the updater already writes (<data>/updater/jobs/
// *.json, read-only, never written here) and refuses NEW entries while any job
// is in a pre-hold state: requested, downloaded, verified or preflight_ok.
//
// It auto-clears when the job leaves those states (refused, cancelled — or
// maintenance_held, where the hold itself takes over) and, as a timeout, once
// the job is older than updaterjob.MaxJobAge (the updater's own 30-minute
// rule) — so a crashed worker can never freeze entries forever. Exits,
// protection, closes and plan reads are never frozen: this is an ENTRY gate in
// the one admission chain only.
//
// A job file that cannot be read or parsed is skipped, WARNed once per file
// version and counted (install_pending_unreadable): the maintenance hold stays
// the hard barrier, so an unreadable job file costs at worst a refused install
// (today's behaviour), never a bot frozen on a file it cannot read.

// installPendingStates are the pre-hold states: a job here is about to hold.
var installPendingStates = map[updaterjob.State]bool{
	updaterjob.StateRequested:   true,
	updaterjob.StateDownloaded:  true,
	updaterjob.StateVerified:    true,
	updaterjob.StatePreflightOK: true,
}

// InstallPendingView is GET /api/maintenance's install_pending block. Pending
// is always present (computed false ≠ absent); the job fields only when true.
type InstallPendingView struct {
	Pending   bool   `json:"pending"`
	JobID     string `json:"job_id,omitempty"`
	State     string `json:"state,omitempty"`
	Phase     string `json:"phase,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	ClearsBy  string `json:"clears_by,omitempty"` // created_at + MaxJobAge (the timeout)
	Unread    int    `json:"unread,omitempty"`    // job files skipped as unreadable this read
}

type installJobPeek struct {
	JobID     string           `json:"job_id"`
	State     updaterjob.State `json:"state"`
	Phase     updaterjob.Phase `json:"phase"`
	CreatedAt time.Time        `json:"created_at"`
}

// installPendingCacheTTL bounds how often the jobs dir is re-read (the gate is
// asked on every placement pass and every Picture frame).
const installPendingCacheTTL = 2 * time.Second

var installPending struct {
	mu        sync.Mutex
	dir       string
	readAt    time.Time // monotonic
	dirMod    time.Time
	view      InstallPendingView
	lastJob   string // the job the current episode named ("" = no episode)
	warned    map[string]bool
	episodes  int64
	nowFn     func() time.Time // test clock seam; nil = time.Now
}

func installPendingNow() time.Time {
	if installPending.nowFn != nil {
		return installPending.nowFn()
	}
	return time.Now()
}

// resetInstallPendingForTest clears the cache and episode state (tests only).
func resetInstallPendingForTest() {
	installPending.mu.Lock()
	defer installPending.mu.Unlock()
	installPending.dir, installPending.readAt, installPending.dirMod = "", time.Time{}, time.Time{}
	installPending.view, installPending.lastJob, installPending.warned = InstallPendingView{}, "", nil
	installPending.nowFn = nil
}

// InstallPendingStatus reads (cached ≤ 2 s, re-read at once when the jobs
// dir changes) whether an update job is in a pre-hold state.
func InstallPendingStatus() InstallPendingView {
	dataDir := MaintenanceDataDir()
	if dataDir == "" {
		return InstallPendingView{}
	}
	jobsDir, err := updaterjob.JobsDir(dataDir)
	if err != nil {
		return InstallPendingView{}
	}
	now := installPendingNow()
	var dirMod time.Time
	if fi, err := os.Stat(jobsDir); err == nil {
		dirMod = fi.ModTime()
	}
	ip := &installPending
	ip.mu.Lock()
	defer ip.mu.Unlock()
	if ip.dir == jobsDir && !ip.readAt.IsZero() && dirMod.Equal(ip.dirMod) && time.Since(ip.readAt) < installPendingCacheTTL {
		return ip.view
	}
	v := readInstallPending(jobsDir, now)
	ip.dir, ip.readAt, ip.dirMod, ip.view = jobsDir, time.Now(), dirMod, v
	// Episode edges, logged once each (L7: every value READ from the file).
	switch {
	case v.Pending && ip.lastJob != v.JobID:
		ip.lastJob = v.JobID
		ip.episodes++
		telemetry.IncGateBlock("", "install_pending_episode")
		logger.Warnf("⏳ install pending: update job %s is %s/%s (created %s) — NEW entries FROZEN until the job ends or %s; exits, protection and plan reads continue.",
			v.JobID, v.State, v.Phase, v.CreatedAt, v.ClearsBy)
	case !v.Pending && ip.lastJob != "":
		logger.Infof("⏳ install pending CLEARED: update job %s left the pre-hold states (or passed its %s timeout) — NEW entries unfrozen (a maintenance hold, if written, still refuses them).",
			ip.lastJob, updaterjob.MaxJobAge)
		ip.lastJob = ""
	}
	return v
}

// readInstallPending scans the jobs dir once. Called with installPending.mu held.
func readInstallPending(jobsDir string, now time.Time) InstallPendingView {
	ents, err := os.ReadDir(jobsDir)
	if err != nil {
		return InstallPendingView{} // no jobs dir: nothing pending
	}
	var best *installJobPeek
	unread := 0
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue // the lock file and a write's .job-*.tmp are never read
		}
		p := filepath.Join(jobsDir, name)
		j, err := peekInstallJob(p)
		if err != nil {
			unread++
			key := name
			if fi, serr := os.Stat(p); serr == nil {
				key = name + "@" + fi.ModTime().UTC().Format(time.RFC3339Nano)
			}
			if installPending.warned == nil {
				installPending.warned = map[string]bool{}
			}
			if !installPending.warned[key] {
				installPending.warned[key] = true
				telemetry.IncGateBlock("", "install_pending_unreadable")
				logger.Warnf("⏳ install pending: job file %s unreadable (%v) — skipped; the maintenance hold stays the barrier.", name, err)
			}
			continue
		}
		if !installPendingStates[j.State] {
			continue
		}
		if j.CreatedAt.IsZero() {
			continue
		}
		// The updater's own 30-minute rule is the timeout. A created_at in the
		// future (the wall clock stepped back past it) stays pending: its age
		// is unknowable, and the job's own end clears it.
		if age := now.Sub(j.CreatedAt); age > updaterjob.MaxJobAge {
			continue
		}
		if best == nil || j.CreatedAt.After(best.CreatedAt) {
			jj := j
			best = &jj
		}
	}
	if best == nil {
		return InstallPendingView{Unread: unread}
	}
	return InstallPendingView{
		Pending: true, JobID: best.JobID, State: string(best.State), Phase: string(best.Phase),
		CreatedAt: kernel.FormatCT(best.CreatedAt),
		ClearsBy:  kernel.FormatCT(best.CreatedAt.Add(updaterjob.MaxJobAge)),
		Unread:    unread,
	}
}

func peekInstallJob(p string) (installJobPeek, error) {
	f, err := os.Open(p)
	if err != nil {
		return installJobPeek{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, updaterjob.MaxFileBytes+1))
	if err != nil {
		return installJobPeek{}, err
	}
	if len(b) > updaterjob.MaxFileBytes {
		return installJobPeek{}, fmt.Errorf("larger than %d bytes", updaterjob.MaxFileBytes)
	}
	var j installJobPeek
	if err := json.Unmarshal(b, &j); err != nil {
		return installJobPeek{}, err
	}
	if j.State == "" {
		return installJobPeek{}, fmt.Errorf("no state")
	}
	return j, nil
}

// InstallPending is the gate predicate: (reason, true) while an update job is
// in a pre-hold state.
func InstallPending() (string, bool) {
	v := InstallPendingStatus()
	if !v.Pending {
		return "", false
	}
	return fmt.Sprintf("install pending: update job %s is %s/%s (created %s); entries unfreeze when it ends or by %s",
		v.JobID, v.State, v.Phase, v.CreatedAt, v.ClearsBy), true
}
