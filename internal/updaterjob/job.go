package updaterjob

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"time"

	"vl/internal/updaterwire"
)

// SchemaVersion is the job file's "schema". A reader refuses any other value.
const SchemaVersion = 1

// MaxJobAge is the 30-minute rule (dispatch §2, C21 as ruled): a job older
// than this in any unfinished state is marked recovery_needed at worker start
// and at the resume verb, and is never silently resumed. The age is measured
// from created_at, not from the last transition.
const MaxJobAge = 30 * time.Minute

// MaxAttempts caps the runs of one state's step (brief §3.4.3): a state whose
// attempts reach it goes to recovery_needed.
const MaxAttempts = 3

// Limits the reader enforces and the writer refuses to exceed, so the writer
// can never persist a file its own reader rejects.
const (
	MaxFileBytes   = 1 << 20
	MaxReceipts    = 64
	MaxTransitions = 64
)

// ErrCorrupt wraps every refusal of a job whose content is not what this
// package writes (unknown or re-cased keys, a history the state table does
// not allow, a bad id, …). A reader treats it like an unreadable file.
var ErrCorrupt = errors.New("updaterjob: corrupt job")

// Job is the on-disk job (<dataDir>/updater/jobs/<job_id>.json, brief §3.2).
//
// Always present: schema, job_id, release_id, state, phase, attempts,
// created_at, updated_at, transitions, receipts ([] at creation — computed
// empty, not unknown). Everything else is ABSENT until the worker has read it
// (L7): a pointer or an omitted string, never a zero standing in for a value.
//
// Never present: requested_by and grant (C14 as ruled — absent, never
// fabricated), any MAC, key, token, e-mail or account name
// (TestJobFileCarriesNoSecret walks the type).
type Job struct {
	Schema    int       `json:"schema"`
	JobID     string    `json:"job_id"`
	ReleaseID string    `json:"release_id"`
	SourceSHA string    `json:"source_sha,omitempty"` // after verified is done
	State     State     `json:"state"`
	Phase     Phase     `json:"phase"`
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Blocker is the verbatim reason the current state is waiting (a gate
	// leg's "name: detail", the AddOn park). Cleared on every state change.
	Blocker string `json:"blocker,omitempty"`
	// Error is the last step error, verbatim.
	Error       string       `json:"error,omitempty"`
	Transitions []Transition `json:"transitions"`
	Receipts    []Receipt    `json:"receipts"`

	Release    *Release `json:"release,omitempty"`  // the release being installed (after verified)
	Install    *Release `json:"install,omitempty"`  // the running install's halves (preflight)
	Snapshot   *Release `json:"snapshot,omitempty"` // the job-scoped copy of the install halves (backup_done)
	BackupPath string   `json:"backup_path,omitempty"`

	IdentityBefore   *Identity `json:"identity_before,omitempty"`   // read just before activate
	IdentityAfter    *Identity `json:"identity_after,omitempty"`    // what activate started
	IdentityRollback *Identity `json:"identity_rollback,omitempty"` // read just before rollback

	// The activation's boot proof inputs, persisted BEFORE the kill: the log
	// the new process will write, the offset to scan from, and the kill
	// instant Watch takes as its since (CTO 1790259689740: 103's Watch gains
	// a since = the persisted kill instant).
	LogPath    string     `json:"log_path,omitempty"`
	LogOffset  *int64     `json:"log_offset,omitempty"`
	WatchSince *time.Time `json:"watch_since,omitempty"`
	// The same three for the rollback's Watch on the OLD sha.
	RollbackLogPath    string     `json:"rollback_log_path,omitempty"`
	RollbackLogOffset  *int64     `json:"rollback_log_offset,omitempty"`
	RollbackWatchSince *time.Time `json:"rollback_watch_since,omitempty"`

	NT8 *NT8Decision `json:"nt8,omitempty"`

	// Addon is the one-click AddOn flow state (W-ADDON-ONE-CLICK), set after
	// nt8_updated and carried through addon_built/placed/restarted/reconnected.
	Addon *AddonState `json:"addon,omitempty"`

	// DrainPath is how the drain passed: "" (the normal ack/census path) or
	// "nt8_absent" (UPDATER-NT8-CLOSED: the link was down >= 60s and every
	// ledger leg passed on its own evidence). LinkDownSince is the server's
	// disconnect stamp when the absent path was taken.
	DrainPath     string `json:"drain_path,omitempty"`
	LinkDownSince string `json:"link_down_since,omitempty"`

	ResumedAt       *time.Time `json:"resumed_at,omitempty"`        // the attended resume from nt8_updated
	LastGoodReceipt *int       `json:"last_good_receipt,omitempty"` // index into receipts (recovery_needed)
	RecoveryReason  string     `json:"recovery_reason,omitempty"`
}

// Transition is one persisted (state, phase) with its time. The list is the
// job's whole history; a reader replays it through CheckMove/CheckFinish.
type Transition struct {
	State State     `json:"state"`
	Phase Phase     `json:"phase"`
	At    time.Time `json:"at"`
	// Receipts is how many receipts the job held when this transition was
	// recorded — a RECORDED count, never inferred (canon 35) — so "a receipt
	// was appended since this step started" survives a Read: a STARTED step
	// becomes DONE only with more receipts than its started transition
	// recorded (Finish, Validate).
	Receipts int `json:"receipts"`
}

// Receipt mirrors activation.Receipt field for field — names, types, order
// and JSON tags (TestReceiptTagsMatchActivationGolden) — so the worker can
// convert one into the other directly and the job file carries the library's
// receipt field for field. Not byte for byte: AddReceipt and Write
// normalize every receipt time to UTC without a monotonic reading (norm) —
// the same instant, possibly a different rendering than the library's.
type Receipt struct {
	Step      string            `json:"step"`
	StartedAt time.Time         `json:"started_at"`
	EndedAt   time.Time         `json:"ended_at"`
	OK        bool              `json:"ok"`
	Evidence  map[string]string `json:"evidence,omitempty"`
	Err       string            `json:"err,omitempty"`
}

// Release mirrors activation.Release's fields (names, types, order; the
// library's struct has no JSON tags, these are the job file's).
type Release struct {
	Dir          string `json:"dir"`
	SHA          string `json:"sha"`
	Binary       string `json:"binary"`
	Dist         string `json:"dist"`
	ReleaseFile  string `json:"release_file"`
	ManifestPath string `json:"manifest_path,omitempty"` // absent for the install and the snapshot
}

// Identity mirrors activation.Identity (pid + /proc/<pid>/stat field 22).
type Identity struct {
	PID        int    `json:"pid"`
	StartTicks uint64 `json:"start_ticks"`
}

// NT8Decision is the AddOn decision as READ (C12 as ruled): the values the
// worker compared, never a file date.
type NT8Decision struct {
	Decision        string `json:"decision"`         // "skipped" | "updated"
	Reason          string `json:"reason,omitempty"` // why "updated" (ack absent, build differs, C# changed, read failed)
	ManifestBuildID string `json:"manifest_build_id,omitempty"`
	AckedBuildID    string `json:"acked_build_id,omitempty"`
	AckedAt         string `json:"acked_at,omitempty"`       // addon_ack.received, verbatim
	AckAcceptSeq    uint64 `json:"ack_accept_seq,omitempty"` // the connection's accept_seq at the park — a new seq proves a new connection
	CSUnchanged     *bool  `json:"cs_unchanged,omitempty"`
	// OldInstallBuildID (W26 PART C) is the VL_BUILD_ID the install's OWN
	// ninjascript declares — "the build the OLD install shipped". It is READ at
	// the park and persists to the resume, where it distinguishes the #206 hazard
	// (build id NOT bumped: old install build == manifest build) from the
	// already-compiled case (bumped: the AddOn can only run the new build if it
	// was F5'd). Empty when unreadable — the park stays the safe side.
	OldInstallBuildID string `json:"old_install_build_id,omitempty"`
	// Absent marks the nt8_absent decision (UPDATER-NT8-CLOSED): NT8 was
	// closed at the decision, so no ack was read — the decision rests on the
	// signed manifest vs the install's ninjascript/*.cs content alone.
	// F5Owed records that the AddOn must be F5'd at the next NT8 start (the
	// release's C# differs from the install's); the job does NOT park — NT8
	// is closed, so there is nobody to F5 now.
	Absent bool `json:"absent,omitempty"`
	F5Owed bool `json:"f5_owed,omitempty"`
}

// AddonState persists the one-click AddOn flow's inputs across its steps, so a
// resume mid-flow (or DS-102's rollback) knows exactly what was built, placed
// and restarted. W-ADDON-ONE-CLICK.
type AddonState struct {
	// DllPath is the built VL.AddOn.dll (WSL path) and Sha256 its hex digest.
	DllPath string `json:"dll_path,omitempty"`
	Sha256  string `json:"sha256,omitempty"`
	// BuiltBy records which builder produced the DLL: "roslyn-ps1" (the no-SDK
	// build-addon.ps1 via NT8's built-in Roslyn) or "dotnet" (the SDK fallback).
	BuiltBy string `json:"built_by,omitempty"`
	// MigrationPending is set when the AddOn place is DEFERRED to the restart
	// step: NT8 was OPEN and NinjaTrader.Custom.dll still held the AddOn type,
	// so the Custom.dll rebuild + AddOn place run while NT8 is closed (release
	// #35 FOLD 2), right after the restart's stop and before its start.
	MigrationPending bool `json:"migration_pending,omitempty"`
	// BackupDir is where Place MOVED the previous DLL ("" when there was none).
	BackupDir string `json:"backup_dir,omitempty"`
	// Restarted records that nt8_restart actually restarted NT8 (so rollback
	// knows to restart it again with the old DLL restored).
	Restarted bool `json:"restarted,omitempty"`
	// OldAcceptSeq is the AddOn connection's accept_seq before the restart — a
	// NEW accept_seq proves a new connection on reconnect.
	OldAcceptSeq uint64 `json:"old_accept_seq,omitempty"`
}

// NT8 decisions.
const (
	NT8Skipped = "skipped"
	NT8Updated = "updated"
)

// ErrBadReleaseID: the release id fails updaterwire.ValidReleaseID.
var ErrBadReleaseID = errors.New("updaterjob: invalid release id")

// ErrNoReceipt: Finish on a step that has appended no receipt since it
// started (U1 verifier defect 2).
var ErrNoReceipt = errors.New("updaterjob: a step is done only with its receipt")

// ErrAttemptsExhausted: the current state's step has run MaxAttempts times.
var ErrAttemptsExhausted = errors.New("updaterjob: attempts exhausted")

// norm is the one time form the file carries: UTC, no monotonic reading.
func norm(t time.Time) time.Time { return t.UTC().Round(0) }

func normPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	n := norm(*t)
	return &n
}

// New is the job the install verb writes: requested/done, attempts 0, no
// receipts. It refuses an id the wire would refuse.
func New(jobID, releaseID string, now time.Time) (Job, error) {
	if !updaterwire.ValidJobID(jobID) {
		return Job{}, fmt.Errorf("%w: %q", ErrBadJobID, clip(jobID))
	}
	if !updaterwire.ValidReleaseID(releaseID) {
		return Job{}, fmt.Errorf("%w: %q", ErrBadReleaseID, clip(releaseID))
	}
	if now.IsZero() {
		return Job{}, errors.New("updaterjob: New needs a clock reading")
	}
	now = norm(now)
	return Job{
		Schema:      SchemaVersion,
		JobID:       jobID,
		ReleaseID:   releaseID,
		State:       StateRequested,
		Phase:       PhaseDone,
		CreatedAt:   now,
		UpdatedAt:   now,
		Transitions: []Transition{{State: StateRequested, Phase: PhaseDone, At: now, Receipts: 0}},
		Receipts:    []Receipt{},
	}, nil
}

// Enter moves the job to state `to` (CheckMove), in EntryPhase(to), with
// attempts 1 for a state whose effect is about to run (0 for one entered
// done), clears the blocker (a blocker belongs to the state it blocked), and
// appends the transition. The caller persists (Write) BEFORE it performs the
// new state's side effect. A refused move leaves the job unchanged.
func (j *Job) Enter(to State, now time.Time) error {
	if err := CheckMove(j.State, j.Phase, to); err != nil {
		return err
	}
	if len(j.Transitions) >= MaxTransitions {
		return fmt.Errorf("%w: more than %d transitions", ErrCorrupt, MaxTransitions)
	}
	now = norm(now)
	ph := EntryPhase(to)
	j.State, j.Phase = to, ph
	j.Attempts = 0
	if ph == PhaseStarted {
		j.Attempts = 1
	}
	j.Blocker = ""
	j.UpdatedAt = now
	j.Transitions = append(append([]Transition(nil), j.Transitions...), Transition{State: to, Phase: ph, At: now, Receipts: len(j.Receipts)})
	return nil
}

// Finish marks the current STARTED state DONE (CheckFinish) and appends the
// transition. The caller has appended the step's receipt first: Finish
// refuses (ErrNoReceipt) unless a receipt was added since the state's
// started transition — a receipt of an earlier step never finishes this one.
func (j *Job) Finish(now time.Time) error {
	if err := CheckFinish(j.State, j.Phase); err != nil {
		return err
	}
	if len(j.Transitions) == 0 {
		return fmt.Errorf("%w: no history", ErrCorrupt)
	}
	if started := j.Transitions[len(j.Transitions)-1].Receipts; len(j.Receipts) <= started {
		return fmt.Errorf("%w: %s holds %d receipts, as many as when it started", ErrNoReceipt, j.State, len(j.Receipts))
	}
	if len(j.Transitions) >= MaxTransitions {
		return fmt.Errorf("%w: more than %d transitions", ErrCorrupt, MaxTransitions)
	}
	now = norm(now)
	j.Phase = PhaseDone
	j.UpdatedAt = now
	j.Transitions = append(append([]Transition(nil), j.Transitions...), Transition{State: j.State, Phase: PhaseDone, At: now, Receipts: len(j.Receipts)})
	return nil
}

// Retry counts one more run of the current STARTED state's step (a resume
// re-running it). It refuses past MaxAttempts: the caller moves the job to
// recovery_needed instead.
func (j *Job) Retry(now time.Time) error {
	if j.Phase != PhaseStarted {
		return fmt.Errorf("%w: only a started step is retried (%s/%s)", ErrForbiddenEdge, j.State, j.Phase)
	}
	if j.Attempts >= MaxAttempts {
		return fmt.Errorf("%w: %s ran %d times", ErrAttemptsExhausted, j.State, j.Attempts)
	}
	j.Attempts++
	j.UpdatedAt = norm(now)
	return nil
}

// AddReceipt appends a receipt (at most MaxReceipts).
func (j *Job) AddReceipt(r Receipt, now time.Time) error {
	if r.Step == "" {
		return fmt.Errorf("%w: a receipt without a step", ErrCorrupt)
	}
	if len(j.Receipts) >= MaxReceipts {
		return fmt.Errorf("%w: more than %d receipts", ErrCorrupt, MaxReceipts)
	}
	j.Receipts = append(append([]Receipt(nil), j.Receipts...), normReceipt(r))
	j.UpdatedAt = norm(now)
	return nil
}

func normReceipt(r Receipt) Receipt {
	r.StartedAt, r.EndedAt = norm(r.StartedAt), norm(r.EndedAt)
	if len(r.Evidence) == 0 {
		r.Evidence = nil
	} else {
		ev := make(map[string]string, len(r.Evidence))
		for k, v := range r.Evidence {
			ev[k] = v
		}
		r.Evidence = ev
	}
	return r
}

// normalize is the form Write persists: every time UTC without a monotonic
// reading, nil lists as [] (computed-empty), copies of every slice/map.
func normalize(j Job) Job {
	j.CreatedAt, j.UpdatedAt = norm(j.CreatedAt), norm(j.UpdatedAt)
	tr := make([]Transition, len(j.Transitions))
	for i, t := range j.Transitions {
		t.At = norm(t.At)
		tr[i] = t
	}
	j.Transitions = tr
	rc := make([]Receipt, len(j.Receipts))
	for i, r := range j.Receipts {
		rc[i] = normReceipt(r)
	}
	j.Receipts = rc
	j.WatchSince, j.RollbackWatchSince, j.ResumedAt = normPtr(j.WatchSince), normPtr(j.RollbackWatchSince), normPtr(j.ResumedAt)
	return j
}

func clip(s string) string {
	if len(s) > 64 {
		return s[:64]
	}
	return s
}

var sha40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Validate is every rule the reader and the writer enforce on content: the
// ids, the schema, a known state and phase, the attempts range, and a
// history that is a legal walk of the state table from requested/done at
// created_at to the current (state, phase).
func (j Job) Validate() error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrCorrupt, fmt.Sprintf(format, a...))
	}
	if j.Schema != SchemaVersion {
		return bad("schema %d, want %d", j.Schema, SchemaVersion)
	}
	if !updaterwire.ValidJobID(j.JobID) {
		return fmt.Errorf("%w: %w: %q", ErrCorrupt, ErrBadJobID, clip(j.JobID))
	}
	if !updaterwire.ValidReleaseID(j.ReleaseID) {
		return fmt.Errorf("%w: %w: %q", ErrCorrupt, ErrBadReleaseID, clip(j.ReleaseID))
	}
	if j.SourceSHA != "" && !sha40.MatchString(j.SourceSHA) {
		return bad("source_sha is not 40 lowercase hex")
	}
	row, ok := rowByState[j.State]
	if !ok {
		return bad("unknown state %q", clip(string(j.State)))
	}
	if j.Phase != PhaseStarted && j.Phase != PhaseDone {
		return bad("unknown phase %q", clip(string(j.Phase)))
	}
	if row.Effect == EffectNone && j.Phase != PhaseDone {
		return bad("%s has no side effect and cannot be started", j.State)
	}
	lo := 0
	if j.Phase == PhaseStarted {
		lo = 1
	}
	if j.Attempts < lo || j.Attempts > MaxAttempts {
		return bad("attempts %d out of range for %s/%s", j.Attempts, j.State, j.Phase)
	}
	if j.CreatedAt.IsZero() || j.UpdatedAt.IsZero() {
		return bad("created_at/updated_at missing")
	}
	if j.Transitions == nil || j.Receipts == nil {
		return bad("transitions and receipts are lists, never null")
	}
	if n := len(j.Transitions); n == 0 || n > MaxTransitions {
		return bad("%d transitions", n)
	}
	if len(j.Receipts) > MaxReceipts {
		return bad("%d receipts", len(j.Receipts))
	}
	first := j.Transitions[0]
	if first.State != StateRequested || first.Phase != PhaseDone || !first.At.Equal(j.CreatedAt) {
		return bad("history does not start at requested/done at created_at")
	}
	sawActivated := false
	var parkDone, leftPark *time.Time // the nt8_updated park: done, and the move to activated
	var nt8State State                // the AddOn branch the history entered, if any
	for i := 1; i < len(j.Transitions); i++ {
		p, c := j.Transitions[i-1], j.Transitions[i]
		if c.At.IsZero() {
			return bad("transition %d has no time", i)
		}
		if c.At.Before(p.At) {
			return bad("transition %d is earlier than transition %d", i, i-1)
		}
		// receipt counts are recorded, so they never run backwards: two
		// finished steps can never share one receipt
		if c.Receipts < p.Receipts {
			return bad("transition %d records %d receipts, fewer than the %d before it", i, c.Receipts, p.Receipts)
		}
		if c.State == p.State && c.Phase == PhaseDone {
			if err := CheckFinish(p.State, p.Phase); err != nil {
				return bad("transition %d: %v", i, err)
			}
			if c.Receipts <= p.Receipts {
				return bad("transition %d: %s is done with no receipt since it started", i, c.State)
			}
			if c.State == StateNT8Updated {
				parkDone = &c.At
			}
			continue
		}
		// LEGACY EDGE (worker-self-update, P0 2026-10-02): every released
		// worker before the worker_swapped state wrote boot_verified/done →
		// complete. History validation must accept every transition any
		// released version could have written — this edge stays legal for
		// REPLAY only; CheckMove (the live table) still forbids it.
		legacyBootToComplete := p.State == StateBootVerified && p.Phase == PhaseDone && c.State == StateComplete
		// LEGACY EDGE (W-ADDON-ONE-CLICK, 2026-10-17): every released worker
		// before this wave left the nt8_updated park directly for activated
		// (the attended F5 resume). Replay accepts it; the live table does not.
		legacyParkLeave := p.State == StateNT8Updated && p.Phase == PhaseDone && c.State == StateActivated
		if !legacyBootToComplete && !legacyParkLeave {
			if err := CheckMove(p.State, p.Phase, c.State); err != nil {
				return bad("transition %d: %v", i, err)
			}
		}
		if c.Phase != EntryPhase(c.State) {
			return bad("transition %d enters %s %s, want %s", i, c.State, c.Phase, EntryPhase(c.State))
		}
		if c.State == StateNT8Skipped || c.State == StateNT8Updated {
			nt8State = c.State
		}
		if c.State == StateActivated {
			sawActivated = true
			if p.State == StateNT8Updated {
				leftPark = &c.At
			}
		}
	}
	last := j.Transitions[len(j.Transitions)-1]
	if last.State != j.State || last.Phase != j.Phase {
		return bad("history ends at %s/%s, the job says %s/%s", last.State, last.Phase, j.State, j.Phase)
	}
	// updated_at is never before the last transition — so never before
	// created_at, the first transition's time (times only run forward)
	if j.UpdatedAt.Before(last.At) {
		return bad("updated_at is earlier than the last transition")
	}
	if last.Receipts > len(j.Receipts) {
		return bad("the history records %d receipts, the file holds %d", last.Receipts, len(j.Receipts))
	}
	// The attended park: the job leaves nt8_updated only on a resume recorded
	// inside the park (after it was done, no later than the move out), and a
	// resume time exists only on a job that parked.
	if j.ResumedAt != nil {
		switch {
		case parkDone == nil:
			return bad("resumed_at on a job that never parked at nt8_updated")
		case !j.ResumedAt.After(*parkDone):
			return bad("resumed_at is not after the nt8_updated park")
		case leftPark != nil && j.ResumedAt.After(*leftPark):
			return bad("resumed_at is after the job left the nt8_updated park")
		}
	}
	if leftPark != nil && j.ResumedAt == nil {
		return bad("left the nt8_updated park for activated with no attended resume")
	}
	for i, r := range j.Receipts {
		if r.Step == "" {
			return bad("receipt %d has no step", i)
		}
	}
	for name, r := range map[string]*Release{"release": j.Release, "install": j.Install, "snapshot": j.Snapshot} {
		if r != nil {
			if err := r.validate(); err != nil {
				return bad("%s: %v", name, err)
			}
		}
	}
	// One release sha: a present release IS source_sha; the snapshot is a copy
	// of the install (equal shas); neither is the build being installed.
	if j.Release != nil && j.Release.SHA != j.SourceSHA {
		return bad("release.sha is not source_sha")
	}
	if j.Install != nil && j.Snapshot != nil && j.Install.SHA != j.Snapshot.SHA {
		return bad("snapshot.sha is not install.sha")
	}
	for name, r := range map[string]*Release{"install": j.Install, "snapshot": j.Snapshot} {
		if r != nil && j.SourceSHA != "" && r.SHA == j.SourceSHA {
			return bad("%s.sha is the release being installed", name)
		}
	}
	for name, id := range map[string]*Identity{"identity_before": j.IdentityBefore, "identity_after": j.IdentityAfter, "identity_rollback": j.IdentityRollback} {
		if id != nil && id.PID <= 0 {
			return bad("%s: pid %d", name, id.PID)
		}
	}
	for name, p := range map[string]string{"backup_path": j.BackupPath, "log_path": j.LogPath, "rollback_log_path": j.RollbackLogPath} {
		if p != "" && !filepath.IsAbs(p) {
			return bad("%s is not absolute", name)
		}
	}
	for name, o := range map[string]*int64{"log_offset": j.LogOffset, "rollback_log_offset": j.RollbackLogOffset} {
		if o != nil && *o < 0 {
			return bad("%s is negative", name)
		}
	}
	if j.NT8 != nil && j.NT8.Decision != NT8Skipped && j.NT8.Decision != NT8Updated {
		return bad("nt8 decision %q", clip(j.NT8.Decision))
	}
	// the AddOn branch the history took carries its own decision
	if nt8State != "" {
		want := NT8Skipped
		if nt8State == StateNT8Updated {
			want = NT8Updated
		}
		if j.NT8 == nil || j.NT8.Decision != want {
			return bad("%s without nt8.decision %q", nt8State, want)
		}
	}
	if j.LastGoodReceipt != nil && (*j.LastGoodReceipt < 0 || *j.LastGoodReceipt >= len(j.Receipts)) {
		return bad("last_good_receipt %d out of range", *j.LastGoodReceipt)
	}
	if j.LastGoodReceipt != nil && !j.Receipts[*j.LastGoodReceipt].OK {
		return bad("last_good_receipt %d names a failed receipt", *j.LastGoodReceipt)
	}
	// The kill is never persisted without what a rollback needs: the release
	// being installed, the install it replaces, the job-scoped snapshot to
	// restore, the DB backup, and the identity it was aimed at.
	if sawActivated && (j.Release == nil || j.Install == nil || j.Snapshot == nil || j.BackupPath == "" || j.IdentityBefore == nil) {
		return bad("activated without its rollback inputs (release, install, snapshot, backup_path, identity_before)")
	}
	return nil
}

func (r Release) validate() error {
	if !sha40.MatchString(r.SHA) {
		return errors.New("sha is not 40 lowercase hex")
	}
	for name, p := range map[string]string{"dir": r.Dir, "binary": r.Binary, "dist": r.Dist, "release_file": r.ReleaseFile} {
		if p == "" || !filepath.IsAbs(p) {
			return fmt.Errorf("%s is not an absolute path", name)
		}
	}
	if r.ManifestPath != "" && !filepath.IsAbs(r.ManifestPath) {
		return errors.New("manifest_path is not an absolute path")
	}
	return nil
}

// Stale is the 30-minute rule as a pure function of the job and an injected
// clock: an UNFINISHED job whose age (now − created_at) exceeds MaxJobAge.
// A zero created_at, or one AFTER now (the wall clock stepped back past the
// job's birth, so its age is unknowable), is stale too — fail closed: the
// job goes to recovery_needed (attended) rather than being resumed on a guess.
// complete/started and rolled_back/started are unfinished (their hold clear
// has not run), so they are subject to the rule like any other.
func Stale(j Job, now time.Time) bool {
	if Finished(j.State, j.Phase) {
		return false
	}
	if j.CreatedAt.IsZero() {
		return true
	}
	age := now.Sub(j.CreatedAt)
	return age < 0 || age > MaxJobAge
}
