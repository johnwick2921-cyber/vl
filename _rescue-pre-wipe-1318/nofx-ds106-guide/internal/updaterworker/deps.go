// Package updaterworker is the M4 updater WORKER (wave 3b-B, unit U4): the
// process that owns ONE durable update job at a time, drives it through the
// state table internal/updaterjob owns, holds trading through the swap with the
// installation hold file, and answers the M3 socket (four verbs).
//
// It is the census-admitted worker side:
//
//   - hold.go is the ONE file in the module, beside the operator CLI, that
//     writes or clears the installation hold (store/maintenance_hold_writers_test.go
//     admits internal/updaterworker/hold.go by name). The worker hold carries
//     Owner "updater" and its job id and NEVER sets withdraw_entries (owner
//     rule: the updater never cancels orders).
//   - the trading app never links this package (TestTradingAppNeverLinksTheUpdaterWorkerSide).
//   - the worker never mints: it reads the app's own views on loopback with
//     the operator's $NOFX_CUTOVER_TOKEN, which is never logged or persisted.
//
// Every side effect is behind a narrow interface (this file) so the whole state
// machine is driven, in tests and in the §5 dry run, against fakes — never the
// live binary, never systemd, never :8080:
//
//	Library     the activation library (103's internal/activation at #201 head
//	            afd60391) — mirrored here EXACTLY; the real adapter is
//	            library_activation.go (NewActivationLibrary), one-line
//	            delegations, parity-pinned by TestAdapterReceiptParity
//	Reverifier  the release re-proof (U3's updaterjob.ReadVerdict /
//	            RehashRelease / ReverifyRelease) — the production adapter is
//	            reverifier.go (NewReleaseReverifier)
//	AppReader   the running bot's loopback views (app_http.go is the real one)
//	Host        clock, sleeps, the main-tree lock check, build info, /proc
//
// Nothing here is reachable from the app, and nothing runs unless an operator
// starts `vl-updater serve` (L4: no worker, no job file = today).
package updaterworker

import (
	"context"
	"errors"
	"time"

	"vl/internal/updaterjob"
)

// ── the activation library seam (CTO 1790261377377: the LANDED #201 shape) ──

// Release, Identity and Receipt are the activation library's types, mirrored
// by internal/updaterjob field for field (names, types, order): activation's
// Receipt JSON tags are pinned by updaterjob's TestReceiptTagsMatchActivationGolden;
// activation's Release and Identity carry no tags (the job file's are
// updaterjob's). Identical field lists make the adapter a plain Go struct
// conversion in both directions — activation.Release(r), updaterjob.Receipt(rc)
// — which the compiler refuses the day either side drifts.
type (
	Release  = updaterjob.Release
	Identity = updaterjob.Identity
	Receipt  = updaterjob.Receipt
)

// WatchOpts mirrors activation.WatchOpts at #201 head afd60391 exactly (field
// names, types, order), so the adapter converts it directly.
type WatchOpts struct {
	// LogPath is the file the RUNNING process writes (named by its boot date).
	LogPath string
	// HealthURL is asked for the revision it is serving.
	HealthURL string
	// Since is the persisted kill instant: a boot line older than it belongs to
	// a previous boot. Zero means "now" — only for a caller that just restarted.
	Since time.Time
	// Within bounds the wait; zero means 90s.
	Within time.Duration
}

// Library is the activation library as the worker calls it — the #201 shape
// at afd60391, method for method (TestLibrarySeamMirrorsTheLandedActivationShape
// pins the rendered signatures). Every call is a step the worker has ALREADY
// persisted as started (TestEveryTransitionPersistsBeforeItsSideEffect).
type Library interface {
	// Resolve reads <dir>/{vl-bin,web/dist,RELEASE,manifest.json}.
	Resolve(dir string) (Release, error)
	// Stage proves rel's binary is the release it claims (vcs stamps + md5).
	Stage(rel Release) (Receipt, error)
	// Backup takes an online, integrity-checked copy of the database to dest.
	Backup(dbPath, dest string) (Receipt, error)
	// Snapshot copies the install's three halves to dest
	// (<dest>/vl-bin, <dest>/web/dist, <dest>/RELEASE).
	Snapshot(install Release, dest string) (Receipt, error)
	// Activate installs rel's halves into prev's paths (prev = the install),
	// then kills the process id names and returns the NEW identity.
	Activate(rel, prev Release, id Identity) (Identity, Receipt, error)
	// Watch proves the process now running is rel (boot line after Since +
	// health), within opts.Within.
	Watch(rel Release, id Identity, opts WatchOpts) (Receipt, error)
	// RollbackTo restores prev's three halves into install, then kills id.
	// It does NOT watch: the caller persists, then watches the OLD sha.
	RollbackTo(prev, install Release, id Identity) (Identity, Receipt, error)
	// CurrentIdentity is the unit's MainPID + /proc/<pid>/stat field 22.
	CurrentIdentity() (Identity, error)
}

// ── the release re-proof seam (U3; the adapter is reverifier.go) ────────────

// Verdict mirrors U3's verdict file (updaterjob.Verdict, <data>/updater/verdicts/<release_id>.json,
// written by the attended `vl-updater fetch`) field for field: names, types,
// order AND json tags. The ONE mapping between the two is reverifier.go's
// mirrorVerdict / Verdict.file — a plain Go struct conversion the compiler
// refuses the day the field lists drift; a conversion ignores tags, so
// TestVerdictMirrorIsTheVerdictFile pins those.
type Verdict struct {
	Schema            int    `json:"schema"`
	ReleaseID         string `json:"release_id"`
	SourceSHA         string `json:"source_sha"`
	ReleaseDir        string `json:"release_dir"`
	Signer            string `json:"signer"`
	SignerFingerprint string `json:"signer_fingerprint"`
	HashAlg           string `json:"hashalg"`
	ManifestSHA256    string `json:"manifest_sha256"`
	Artifacts         int    `json:"artifacts"`
	VerifiedAt        string `json:"verified_at"` // RFC3339Nano, UTC
}

// ReleaseFacts is what a re-verification proved from the SIGNED manifest.
type ReleaseFacts struct {
	ReleaseID         string
	SourceSHA         string
	ManifestSHA256    string
	SignerFingerprint string
	// AddonBuildID is manifest addon.build_id verbatim ("" absent, "n/a" passes
	// through — the nt8 rule refuses both).
	AddonBuildID string
	// Artifacts is the signed manifest's artifacts[] as path → sha256 (the
	// adapter copies U3's SignedManifest.Artifacts). The nt8 rule reads the
	// ninjascript/*.cs entries (C12) and the boot check reads
	// web/dist/index.html (OQ-6 R-b) from HERE — the signed bytes, re-verified
	// at the step that reads them — never from a file's name or date.
	Artifacts map[string]string
}

// Reverifier re-proves a release the attended fetch verified. The job's
// downloaded and verified states call it; it never trusts the verdict file
// alone (a guarantee nobody re-checks silently lapses). The production adapter
// (reverifier.go, NewReleaseReverifier) delegates to U3's functions.
type Reverifier interface {
	// Verdict reads the verdict file (updaterjob.ReadVerdict).
	Verdict(releaseID string) (Verdict, error)
	// Rehash re-hashes every signed artifact of the materialized release
	// (U3 RehashRelease(v)); returns the count.
	Rehash(v Verdict) (int, error)
	// Reverify re-verifies the SSHSIG over the signed manifest against the
	// installation's allowed-signers file NOW (U3 ReverifyRelease) and binds
	// the result to the verdict: the adapter refuses unless the signed
	// manifest's sha256 and source_sha are the verdict's (U3 verifier defect 6).
	Reverify(v Verdict) (ReleaseFacts, error)
}

// ── the running app, read on loopback ───────────────────────────────────────

// ErrUnauthorized is a 401 from the app: the cutover token was refused. In
// preflight it refuses the job; after the hold it is a blocker.
var ErrUnauthorized = errors.New("updaterworker: the app refused the cutover token (401)")

// AppReader reads the running bot's own views. The real one (app_http.go)
// sends GETs to 127.0.0.1 with Bearer $NOFX_CUTOVER_TOKEN; it never writes.
type AppReader interface {
	// Health is GET /api/health's "revision" (12 chars on the live bot).
	Health(ctx context.Context) (string, error)
	// Maintenance is GET /api/maintenance.
	Maintenance(ctx context.Context) (MaintenanceView, error)
	// InstallationGate is GET /api/installation-gate.
	InstallationGate(ctx context.Context) (GateView, error)
	// Index is GET / (the served UI shell, OQ-6 R-b).
	Index(ctx context.Context) ([]byte, error)
}

// MaintenanceView mirrors trader.MaintenanceStatusView (GET /api/maintenance);
// TestAppViewsMirrorTheTraderJSON marshals the trader types into it.
type MaintenanceView struct {
	Held          bool     `json:"held"`
	State         string   `json:"state"`
	JobID         *string  `json:"job_id"`
	Since         *string  `json:"since"`
	Reason        string   `json:"reason,omitempty"`
	InFlightSends int64    `json:"in_flight_sends"`
	Drained       bool     `json:"drained"`
	AddonAck      *AckView `json:"addon_ack"`
}

// AckView mirrors trader.MaintenanceAckView.
type AckView struct {
	Received       string `json:"received"`
	AgeMs          int64  `json:"age_ms"`
	Held           bool   `json:"held"`
	JobID          string `json:"job_id"`
	QueuedCommands int    `json:"queued_commands"`
	BuildID        string `json:"build_id"`
	AcceptSeq      uint64 `json:"accept_seq"`
}

// GateView mirrors trader.InstallationGate (GET /api/installation-gate).
type GateView struct {
	Ready     bool           `json:"ready"`
	JobID     string         `json:"job_id"`
	Legs      []GateLeg      `json:"legs"`
	Traders   []string       `json:"traders"`
	Note      string         `json:"note"`
	NT8Absent *NT8AbsentView `json:"nt8_absent"`
}

// NT8AbsentView mirrors trader.NT8AbsentView (UPDATER-NT8-CLOSED): the
// nt8_absent verdict. Legs are ABSENT when not eligible — an uncomputed leg
// list is absent, never [] (canon 49/53).
type NT8AbsentView struct {
	Eligible      bool      `json:"eligible"`
	Ready         bool      `json:"ready"`
	LinkDownSince string    `json:"link_down_since"`
	Legs          []GateLeg `json:"legs"`
}

// GateLeg mirrors trader.InstallationGateLeg.
type GateLeg struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
	Source string `json:"source"`
}

// ── the machine ─────────────────────────────────────────────────────────────

// Host is the machine the worker runs on. Every wait goes through it, so no
// test sleeps on the wall clock (and none can fall into the CME daily break).
type Host interface {
	// Now is the wall clock (job times are persisted UTC).
	Now() time.Time
	// Sleep waits d or until ctx ends.
	Sleep(ctx context.Context, d time.Duration) error
	// LockAcquire runs `bash <LockScript> acquire <session> <task>
	// <minutes>`: the same atomic acquire humans use. acquired=true when
	// rc 0 (the lock is now ours). When rc 1 (held), holder names the
	// current holder when the script's status line parses. The worker
	// never acquires a held/stale lock and never clears an incomplete one.
	LockAcquire(session, task string, minutes int) (acquired bool, holder string, err error)
	// LockHolder reports the current lock holder: "" when free; the named
	// session when held (fresh OR stale — both name their holder). An
	// incomplete or abandoned-incomplete lock is an error: the worker
	// never proceeds on a lock it cannot positively attribute.
	LockHolder() (holder string, err error)
	// LockRelease runs `bash <LockScript> release <session>` — only the
	// holder may release. A no-lock release is a no-op success (the script
	// prints "no lock" with rc 0).
	LockRelease(session string) error
	// BuildInfo reads vcs.revision and vcs.modified from a binary.
	BuildInfo(binary string) (revision, modified string, err error)
	// ExeOf is /proc/<pid>/exe.
	ExeOf(pid int) (string, error)
}

// Deps is everything New needs besides the Config.
type Deps struct {
	Lib  Library
	App  AppReader
	Rel  Reverifier
	Host Host
}

// ErrNotWired: a production dependency whose adapter has not landed yet. serve
// refuses to start with it (L4: no worker runs until every piece is real).
var ErrNotWired = errors.New("updaterworker: not wired yet")
