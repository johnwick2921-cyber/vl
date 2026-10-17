package updaterjob

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"vl/internal/updaterwire"
)

// Layout under the installation's data dir (the one internal/installpath
// resolves for the bot, the hold CLI and the wire):
//
//	<dataDir>/updater/            0700 (updaterwire.UpdaterDirName)
//	  jobs/                       0700
//	    <job_id>.json             0600  one job, written ONLY by Write
//	    .jobs.lock                0600  flock held by every Write
//	    .job-*.tmp                      a write in flight (or a crash's leftover; never read)
const (
	jobsDirName  = "jobs"
	jobFileExt   = ".json"
	jobsLockName = ".jobs.lock"
	tmpPattern   = ".job-*.tmp"
)

var (
	// ErrNotFound: no job file for the id (or no jobs dir at all).
	ErrNotFound = errors.New("updaterjob: no such job")
	// ErrBadJobID: the id fails updaterwire.ValidJobID — nothing is joined.
	ErrBadJobID = errors.New("updaterjob: invalid job id")
	// ErrBadDataDir: empty or relative data dir.
	ErrBadDataDir = errors.New("updaterjob: data dir must be an absolute path")
	// ErrUnsafe: a symlink, a non-regular file, a foreign owner, a mode
	// looser than 0600/0700, or a file larger than MaxFileBytes.
	ErrUnsafe = errors.New("updaterjob: unsafe job file")
	// ErrRewrite: a Write that would not extend the job on disk (a changed
	// identity field, a rewritten history or receipt, a finished job edited,
	// a file created mid-flight).
	ErrRewrite = errors.New("updaterjob: write does not extend the job on disk")
)

// Seams (tests only): the owner lookup, and the three durability calls whose
// ORDER is the crash-safety claim (TestWriteIsTmpFsyncRenameAndPrivate).
var (
	geteuid = os.Geteuid
	// ownerOf reads a job file's owner (tests make one file read as another
	// uid's without root; the DIRS go through updaterwire's own lookup).
	ownerOf = func(fi fs.FileInfo) (uint32, bool) {
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return 0, false
		}
		return st.Uid, true
	}
	fsyncFile  = func(f *os.File) error { return f.Sync() }
	renameFile = os.Rename
	fsyncDir   = func(dir string) error {
		d, err := os.Open(dir)
		if err != nil {
			return err
		}
		defer d.Close()
		return d.Sync()
	}
)

// JobsDir is <dataDir>/updater/jobs.
func JobsDir(dataDir string) (string, error) {
	if dataDir == "" || !filepath.IsAbs(dataDir) {
		return "", ErrBadDataDir
	}
	return filepath.Join(filepath.Clean(dataDir), updaterwire.UpdaterDirName, jobsDirName), nil
}

// Path is <dataDir>/updater/jobs/<job_id>.json for a valid job id only
// (updaterwire.ValidJobID — the ONE id rule the wire and the app share); a
// forged id is refused before any join, and the joined path is re-checked to
// be a direct child of the jobs dir.
func Path(dataDir, jobID string) (string, error) {
	dir, err := JobsDir(dataDir)
	if err != nil {
		return "", err
	}
	if !updaterwire.ValidJobID(jobID) {
		return "", fmt.Errorf("%w: %q", ErrBadJobID, clip(jobID))
	}
	name := jobID + jobFileExt
	p := filepath.Join(dir, name)
	if filepath.Dir(p) != dir || filepath.Base(p) != name {
		return "", fmt.Errorf("%w: %q", ErrBadJobID, clip(jobID))
	}
	return p, nil
}

// privateDirs checks <dataDir>/updater and its jobs dir: real directories,
// owned by this uid, no group/other bits (updaterwire.CheckPrivateDir). With
// create, missing ones are made 0700 first (never MkdirAll: the data dir must
// already exist), and every dir Mkdir actually creates is made durable by an
// fsync of its PARENT before anything is written into it — if that fsync
// fails the empty dir is removed again, so the next write re-creates and
// re-syncs it; an existing loose/foreign/symlinked one is refused, never
// repaired. Without create, a missing one is ErrNotFound and nothing is made.
func privateDirs(dataDir string, create bool) (string, error) {
	jobs, err := JobsDir(dataDir)
	if err != nil {
		return "", err
	}
	for _, d := range []string{filepath.Dir(jobs), jobs} {
		if create {
			err := os.Mkdir(d, 0o700)
			switch {
			case err == nil:
				if err := fsyncDir(filepath.Dir(d)); err != nil {
					_ = os.Remove(d)
					return "", err
				}
			case !errors.Is(err, fs.ErrExist):
				return "", err
			}
		}
		if err := updaterwire.CheckPrivateDir(d, geteuid()); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return "", fmt.Errorf("%w: %v", ErrNotFound, err)
			}
			return "", fmt.Errorf("%w: %v", ErrUnsafe, err)
		}
	}
	return jobs, nil
}

// readSafe reads p only when it is a regular file (never through a symlink:
// O_NOFOLLOW; never a FIFO or device: checked on the OPENED descriptor), mode
// no looser than 0600, owned by the effective uid, and at most MaxFileBytes —
// internal/updateauth's readPrivateFile rules, re-stated here because this
// package may not import updateauth (brief C2).
func readSafe(p string) ([]byte, error) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil, fmt.Errorf("%w: %s", ErrNotFound, filepath.Base(p))
		case errors.Is(err, syscall.ELOOP):
			return nil, fmt.Errorf("%w: %s is a symlink", ErrUnsafe, filepath.Base(p))
		}
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrUnsafe, filepath.Base(p))
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%w: %s mode %04o is looser than 0600", ErrUnsafe, filepath.Base(p), fi.Mode().Perm())
	}
	if uid, ok := ownerOf(fi); !ok || int(uid) != geteuid() {
		return nil, fmt.Errorf("%w: %s is not owned by this uid", ErrUnsafe, filepath.Base(p))
	}
	if fi.Size() > MaxFileBytes {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrUnsafe, filepath.Base(p), MaxFileBytes)
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxFileBytes {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrUnsafe, filepath.Base(p), MaxFileBytes)
	}
	return b, nil
}

// encode is the writer's exact byte form: two-space indented JSON + "\n".
func encode(j Job) ([]byte, error) {
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// canonical returns the fixed point of decode∘encode: one round trip turns
// every string into valid UTF-8, after which encode(decode(b)) == b — so the
// reader's byte-exact check never refuses a file this writer wrote.
func canonical(j Job) ([]byte, Job, error) {
	b, err := encode(j)
	if err != nil {
		return nil, Job{}, err
	}
	var k Job
	if err := json.Unmarshal(b, &k); err != nil {
		return nil, Job{}, err
	}
	b, err = encode(k)
	return b, k, err
}

// decode is the strict reader: exactly one JSON object of this schema's keys
// (unknown keys refused), nothing after it, byte-identical to what the writer
// would write for it — which also refuses a re-cased key, a duplicate key, a
// reordered or re-indented file and a non-canonical number — then Validate,
// then the file-name binding.
func decode(b []byte, jobID string) (Job, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var j Job
	if err := dec.Decode(&j); err != nil {
		return Job{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Job{}, fmt.Errorf("%w: data after the job object", ErrCorrupt)
	}
	again, err := encode(j)
	if err != nil || !bytes.Equal(again, b) {
		return Job{}, fmt.Errorf("%w: not the writer's canonical form", ErrCorrupt)
	}
	if err := j.Validate(); err != nil {
		return Job{}, err
	}
	if j.JobID != jobID {
		return Job{}, fmt.Errorf("%w: file %s holds job %q", ErrCorrupt, jobID, clip(j.JobID))
	}
	return j, nil
}

func lockJobs(jobs string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(jobs, jobsLockName), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("updaterjob: lock: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// Write persists j atomically and durably, and only when it EXTENDS the job
// on disk:
//
//   - content first, before any filesystem access: a valid id, Validate (the
//     history is a legal walk of the state table), at most MaxFileBytes;
//   - a new file is born only as New's job (requested/done, no receipts);
//   - an existing file must be this package's (readSafe + the strict
//     decoder), and the new job keeps its schema/job_id/release_id/
//     created_at, keeps every earlier transition and receipt byte for byte,
//     never unsets or changes source_sha/release/install/snapshot/
//     backup_path once set, and a finished job is never edited (an identical
//     re-write is a no-op);
//   - then temp file in the jobs dir (0600 from birth) → write → fsync →
//     chmod 0600 → rename over the target → fsync the dir, all under
//     jobs/.jobs.lock. A reader sees the old job or the new job, never a
//     partial one. Any error — including the dir fsync — is returned: the
//     caller must not perform the side effect it was persisting for.
func Write(dataDir string, j Job) error {
	if _, err := JobsDir(dataDir); err != nil {
		return err
	}
	if !updaterwire.ValidJobID(j.JobID) {
		return fmt.Errorf("%w: %q", ErrBadJobID, clip(j.JobID))
	}
	j = normalize(j)
	if err := j.Validate(); err != nil {
		return err
	}
	b, j, err := canonical(j)
	if err != nil {
		return fmt.Errorf("updaterjob: encode: %w", err)
	}
	if len(b) > MaxFileBytes {
		return fmt.Errorf("%w: job %s encodes to %d bytes (max %d)", ErrCorrupt, j.JobID, len(b), MaxFileBytes)
	}
	target, err := Path(dataDir, j.JobID)
	if err != nil {
		return err
	}
	jobs, err := privateDirs(dataDir, true)
	if err != nil {
		return err
	}
	unlock, err := lockJobs(jobs)
	if err != nil {
		return err
	}
	defer unlock()
	old, err := readSafe(target)
	switch {
	case errors.Is(err, ErrNotFound):
		if len(j.Transitions) != 1 || len(j.Receipts) != 0 {
			return fmt.Errorf("%w: job %s has no file; a job file is born at requested", ErrRewrite, j.JobID)
		}
	case err != nil:
		return err
	default:
		oj, err := decode(old, j.JobID)
		if err != nil {
			return fmt.Errorf("existing job file: %w", err)
		}
		if err := extends(oj, old, j, b); err != nil {
			return err
		}
	}
	return writeAtomic(jobs, target, b)
}

// extends is nil when nj (encoded nb) is a legal successor of the job on
// disk (oj, encoded ob).
func extends(oj Job, ob []byte, nj Job, nb []byte) error {
	if bytes.Equal(ob, nb) {
		return nil
	}
	rw := func(why string) error { return fmt.Errorf("%w: job %s: %s", ErrRewrite, nj.JobID, why) }
	if Finished(oj.State, oj.Phase) {
		return rw("the job is finished (" + string(oj.State) + ")")
	}
	if oj.Schema != nj.Schema || oj.JobID != nj.JobID || oj.ReleaseID != nj.ReleaseID || !oj.CreatedAt.Equal(nj.CreatedAt) {
		return rw("schema, job_id, release_id and created_at never change")
	}
	if len(nj.Transitions) < len(oj.Transitions) || len(nj.Receipts) < len(oj.Receipts) {
		return rw("history and receipts are append-only")
	}
	same := func(a, b any) bool {
		x, _ := json.Marshal(a)
		y, _ := json.Marshal(b)
		return bytes.Equal(x, y)
	}
	for i := range oj.Transitions {
		if !same(oj.Transitions[i], nj.Transitions[i]) {
			return rw(fmt.Sprintf("transition %d rewritten", i))
		}
	}
	for i := range oj.Receipts {
		if !same(oj.Receipts[i], nj.Receipts[i]) {
			return rw(fmt.Sprintf("receipt %d rewritten", i))
		}
	}
	for name, pair := range map[string][2]any{
		"source_sha":  {oj.SourceSHA, nj.SourceSHA},
		"release":     {oj.Release, nj.Release},
		"install":     {oj.Install, nj.Install},
		"snapshot":    {oj.Snapshot, nj.Snapshot},
		"backup_path": {oj.BackupPath, nj.BackupPath},
	} {
		if unset(pair[0]) {
			continue
		}
		if !same(pair[0], pair[1]) {
			return rw(name + " is write-once")
		}
	}
	return nil
}

func unset(v any) bool {
	switch x := v.(type) {
	case string:
		return x == ""
	case *Release:
		return x == nil
	}
	return false
}

func writeAtomic(dir, target string, b []byte) error {
	f, err := os.CreateTemp(dir, tmpPattern)
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after a successful rename
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := fsyncFile(f); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	if err := renameFile(tmp, target); err != nil {
		return err
	}
	return fsyncDir(dir)
}

// Read loads one job: a valid id, private dirs, a safe regular file, the
// strict decoder, and the file-name binding. No job file (or no jobs dir) is
// ErrNotFound and creates nothing.
func Read(dataDir, jobID string) (Job, error) {
	p, err := Path(dataDir, jobID)
	if err != nil {
		return Job{}, err
	}
	if _, err := privateDirs(dataDir, false); err != nil {
		return Job{}, err
	}
	b, err := readSafe(p)
	if err != nil {
		return Job{}, err
	}
	return decode(b, jobID)
}

// List reads every job file, oldest first (created_at, then job_id). A
// missing jobs dir is an empty list; a write's temp file and the lock file
// are skipped; any other entry, or any job that does not read, is an error
// (fail closed — the worker's start sweep must see every job or none).
func List(dataDir string) ([]Job, error) {
	jobs, err := privateDirs(dataDir, false)
	if errors.Is(err, ErrNotFound) {
		return []Job{}, nil
	}
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(jobs)
	if err != nil {
		return nil, err
	}
	out := []Job{}
	for _, e := range ents {
		name := e.Name()
		if name == jobsLockName || (strings.HasPrefix(name, ".job-") && strings.HasSuffix(name, ".tmp")) {
			continue
		}
		id, ok := strings.CutSuffix(name, jobFileExt)
		if !ok || !updaterwire.ValidJobID(id) {
			return nil, fmt.Errorf("%w: unexpected entry %q in the jobs dir", ErrUnsafe, clip(name))
		}
		j, err := Read(dataDir, id)
		if err != nil {
			return nil, fmt.Errorf("job %s: %w", id, err)
		}
		out = append(out, j)
	}
	sort.Slice(out, func(a, b int) bool {
		if !out[a].CreatedAt.Equal(out[b].CreatedAt) {
			return out[a].CreatedAt.Before(out[b].CreatedAt)
		}
		return out[a].JobID < out[b].JobID
	})
	return out, nil
}

// ListTolerant is List for the start sweep (worker-self-update P0
// 2026-10-02): a TERMINAL job (complete/refused/rolled_back) that no longer
// validates — a job written by an older released worker whose edges the table
// has since changed — is skipped and its id returned in skippedTerminal so
// the caller can WARN and continue. A NON-terminal unreadable job still
// errors (and names the job). Everything else is exactly List.
func ListTolerant(dataDir string) (jobs []Job, skippedTerminal []string, err error) {
	jobsDir, err := privateDirs(dataDir, false)
	if errors.Is(err, ErrNotFound) {
		return []Job{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	ents, err := os.ReadDir(jobsDir)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range ents {
		name := e.Name()
		if name == jobsLockName || (strings.HasPrefix(name, ".job-") && strings.HasSuffix(name, ".tmp")) {
			continue
		}
		id, ok := strings.CutSuffix(name, jobFileExt)
		if !ok || !updaterwire.ValidJobID(id) {
			return nil, nil, fmt.Errorf("%w: unexpected entry %q in the jobs dir", ErrUnsafe, clip(name))
		}
		j, err := Read(dataDir, id)
		if err == nil {
			jobs = append(jobs, j)
			continue
		}
		if peeked, peekErr := peekState(jobsDir, name); peekErr == nil && isTerminalJobState(peeked) {
			skippedTerminal = append(skippedTerminal, id)
			continue
		}
		return nil, nil, fmt.Errorf("job %s: %w", id, err)
	}
	sort.Slice(jobs, func(a, b int) bool {
		if !jobs[a].CreatedAt.Equal(jobs[b].CreatedAt) {
			return jobs[a].CreatedAt.Before(jobs[b].CreatedAt)
		}
		return jobs[a].JobID < jobs[b].JobID
	})
	return jobs, skippedTerminal, nil
}

// peekState reads only the "state" field of a job file (tolerant of every
// other field) so the sweep can classify a job it cannot fully validate.
func peekState(jobsDir, name string) (State, error) {
	raw, err := os.ReadFile(filepath.Join(jobsDir, name))
	if err != nil {
		return "", err
	}
	var p struct {
		State State `json:"state"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", err
	}
	return p.State, nil
}

// isTerminalJobState reports the terminal states a sweep may skip: complete,
// refused, rolled_back, reverted_clean (worker-self-update P0: the CTO's named
// set, plus the self-recovery terminal W-ADDON-ONE-CLICK adds).
func isTerminalJobState(s State) bool {
	return s == StateComplete || s == StateRefused || s == StateRolledBack || s == StateRevertedClean
}
