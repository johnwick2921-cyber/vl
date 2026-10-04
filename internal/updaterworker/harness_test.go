package updaterworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"vl/internal/updaterjob"
	"vl/internal/updaterwire"
	"vl/store"
)

// ── the U4 test box ─────────────────────────────────────────────────────────
//
// One simulated machine behind every seam, so the REAL runner, the REAL job
// file (updaterjob.Write/Read), the REAL hold file (store via hold.go) and the
// REAL loopback reader (HTTPApp over an httptest app) are exercised together:
//
//	fakeLib   the activation library: installs by copying real files into a
//	          temp install, "kills" by changing the identity, and writes the
//	          boot line the bot would write into the temp data log
//	fakeRel   the release re-proof (verdict + rehash + reverify)
//	fakeHost  a clock that only moves when the runner sleeps (no wall-clock
//	          test: none can fall into the CME daily break)
//	the app   /api/health, /api/maintenance, /api/installation-gate and / —
//	          computed from the hold file ON DISK and the box's state
//
// Every side-effect call re-reads the job file and records a VIOLATION unless
// its own state is on disk "started" (TestEveryTransitionPersistsBeforeItsSideEffect),
// and every call after the hold write also requires this job's hold on disk.

const (
	boxToken     = "tok-u4-never-anywhere-7f3a9c41e2" // VL_CUTOVER_TOKEN in every rig
	boxReleaseID = "v1.2.0"
	boxJobID     = "job-u4-0001abcd"
	boxOldBuild  = "2026-09-23-m21"
	boxNewBuild  = "2026-09-25-m22"
)

var (
	boxOld = strings.Repeat("a1", 20)
	boxNew = strings.Repeat("b2", 20)
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type box struct {
	t     *testing.T
	mu    sync.Mutex
	clock *fakeClock

	inst, data, relDir, backupRoot string

	binName    string // the install binary basename (vl-bin or vl-bin — R5 removes the vl form)
	relBinName string // the release dir's binary basename (defaults to binName)

	id      Identity // the unit's MainPID identity
	running string   // the sha the running process serves

	// the fake lock (WORKER-TAKES-THE-LOCK): lockHolder is the current
	// holder ("" free); the counters pin the acquire/release edges.
	lockHolder          string
	lockAcquireCalls    int
	lockAcquireSessions []string
	lockReleaseCalls    int
	lockAcquireErr      error

	// knobs
	flat             bool
	addonConnected   bool
	pollHook         func() // run at every fake-host sleep boundary (planner-wait tests)
	addonBuild       string // what the running AddOn reports
	manifestBuild    string // the signed manifest's addon.build_id
	refuseBoot       map[string]bool
	watchFail        map[string]bool
	rollbackFail     bool
	rollbackDistFail bool // RollbackTo restores binary+RELEASE, fails at dist, no kill of its own
	badToken         bool // the app refuses the token (401)
	verdictMissing   bool
	holdWriteLies    bool          // the hold write lands on disk, then errs (U1 item 9)
	holdWriteFails   bool          // the hold write errs before anything lands
	ackStale         bool          // the AddOn's last ack is 20 s old
	ackJob           string        // the AddOn acks this job id instead of the hold's
	addonSeq         uint64        // the AddOn's connection accept_seq (a reconnect = a new seq)
	exe              string        // /proc/<MainPID>/exe, when not the install's binary
	healthRev        string        // /api/health serves this revision instead of the running sha
	ackLag           time.Duration // the AddOn's last ack is this much older than the 5 s tick (age stays consistent)
	wallStep         time.Duration // a wall-clock step: shifts the RENDERED received time only; AgeMs stays monotonic
	maintJob         string        // /api/maintenance names this job instead of the hold's
	echo500          bool          // the authed endpoints answer 500 echoing the request's Authorization header
	noCS             bool          // the signed manifest lists no ninjascript/*.cs
	reverifyTamper   func(*ReleaseFacts)

	// UPDATER-NT8-CLOSED knobs
	addonDownSince time.Duration // >0: the AddOn is disconnected this long (stamped on the fake server's record); the ack is nil and the fake gate attaches its nt8_absent view
	absentInflight bool          // nt8_absent leg: an entry send holds a permit
	absentQueued   int           // nt8_absent leg: queued_signals
	absentPlanner  bool          // nt8_absent leg: a planner read is in flight
	absentSim      bool          // nt8_absent leg: the bound account is SIM-tradeable
	absentDbOpen   bool          // nt8_absent leg: an OPEN trader_positions row (the CTO P1 leg)
	cutoverStale   bool          // P-D ruling item 3: the trader_cutover leg fails with the
	// production "working_orders: snapshot stale" shape when the
	// AddOn is gone (tonight's job 66383c7c preflight blocker)
	plannerInFlight bool // F2: the planner_in_flight leg FAILS with the production
	// "waiting for the AI plan (started hh:mm:ss)" detail
	absentOverride  bool // attach the view with the flags below instead of the computed ones (the K_elig mutant probe)
	absentEligible  bool
	absentReady     bool
	absentFlapAt    int // with absentOverride: from gate-read N onward the view flaps to eligible=false, ready=true (the gate must refuse — K_elig would let it through)
	absentViewReads int // count of gate-view reads for the flap knob

	calls       []string
	violations  []string
	watchOpts   []WatchOpts
	watchSHAs   []string
	rollbackArg [][2]Release
	activateIDs []Identity
	rollbackIDs []Identity
	rollbackAtt []int   // the job's attempts on disk at each RollbackTo call
	ackAges     []int64 // every ack age the worker observed, in order
}

// rig is one worker on one box.
type rig struct {
	*box
	cfg Config
	app *HTTPApp
	srv *httptest.Server
	w   *Worker
	log *strings.Builder
}

type rigOpt func(*box)

// withBinary renames the box's install and release binaries from vl-bin to
// binName (the R1a dual-binary predictor tests; R5 removes this option).
func withBinary(binName string) rigOpt {
	return func(b *box) {
		for _, dir := range []string{b.inst, b.relDir} {
			old, nu := filepath.Join(dir, "vl-bin"), filepath.Join(dir, binName)
			if err := os.Rename(old, nu); err == nil {
				_ = os.Chmod(nu, 0o755)
			}
		}
		b.binName = binName
		b.relBinName = binName
	}
}

// withReleaseBinary renames ONLY the release's binary (the R2-like rollback
// test: a vl release activating onto a vl install).
func withReleaseBinary(binName string) rigOpt {
	return func(b *box) {
		old, nu := filepath.Join(b.relDir, "vl-bin"), filepath.Join(b.relDir, binName)
		if err := os.Rename(old, nu); err == nil {
			_ = os.Chmod(nu, 0o755)
		}
		b.relBinName = binName
	}
}

func withCSChanged() rigOpt {
	return func(b *box) {
		writeFile(b.t, filepath.Join(b.relDir, "ninjascript", "VLTrader.cs"), "// C# v2\n")
		b.manifestBuild = boxNewBuild
	}
}

func newRig(t *testing.T, opts ...rigOpt) *rig {
	t.Helper()
	t.Setenv(CutoverTokenEnv, boxToken)
	// a SHORT root: <root>/vl/data/updater/<socket> must fit sun_path (107)
	root, err := os.MkdirTemp("", "u4-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	b := &box{
		t: t, clock: &fakeClock{t: time.Date(2026, 9, 24, 10, 0, 0, 0, time.Local)},
		inst: filepath.Join(root, "vl"), backupRoot: filepath.Join(root, "vl-backups", "updater"),
		binName: "vl-bin",
		id:      Identity{PID: 4242, StartTicks: 1000}, running: boxOld,
		flat: true, addonConnected: true, addonBuild: boxOldBuild, manifestBuild: boxOldBuild,
		absentSim:  true,
		refuseBoot: map[string]bool{}, watchFail: map[string]bool{},
	}
	b.data = filepath.Join(b.inst, "data")
	b.relDir = filepath.Join(root, "releases", boxNew)
	// the running install (a checkout at boxOld)
	writeFile(t, filepath.Join(b.inst, b.binName), binaryBody(boxOld))
	writeFile(t, filepath.Join(b.inst, "deploy", "RELEASE"), boxOld+"\n")
	writeFile(t, filepath.Join(b.inst, "web", "dist", "index.html"), "<html>old</html>\n")
	writeFile(t, filepath.Join(b.inst, "web", "dist", "assets", "app.js"), "old();\n")
	writeFile(t, filepath.Join(b.inst, calendarFile), "[]\n")
	writeFile(t, filepath.Join(b.inst, "ninjascript", "VLTrader.cs"), "// C# v1\n")
	writeFile(t, filepath.Join(b.data, "data.db"), "SQLite format 3\x00 fake\n")
	// the materialized release (activation layout)
	writeFile(t, filepath.Join(b.relDir, b.binName), binaryBody(boxNew))
	writeFile(t, filepath.Join(b.relDir, "RELEASE"), boxNew+"\n")
	writeFile(t, filepath.Join(b.relDir, "web", "dist", "index.html"), "<html>new</html>\n")
	writeFile(t, filepath.Join(b.relDir, calendarFile), "[]\n")
	writeFile(t, filepath.Join(b.relDir, "ninjascript", "VLTrader.cs"), "// C# v1\n")
	writeFile(t, filepath.Join(b.relDir, "manifest.json"), fmt.Sprintf(`{"source_sha":%q,"binary_md5":"","signature_verdict":"sshsig:release:SHA256:fake"}`, boxNew))
	for _, o := range opts {
		o(b)
	}
	if b.relBinName == "" {
		b.relBinName = b.binName
	}

	r := &rig{box: b, log: &strings.Builder{}}
	var app *HTTPApp
	r.srv = httptest.NewServer(http.HandlerFunc(b.serveApp))
	t.Cleanup(r.srv.Close)
	port := r.srv.Listener.Addr().(*net.TCPAddr).Port
	r.cfg = Config{
		Target: Target{InstallDir: b.inst, DBFile: filepath.Join(b.data, "data.db"), DataDir: b.data, LogDir: b.data,
			Port: port, Source: "test"},
		BackupRoot: b.backupRoot,
		Budgets:    DefaultBudgets(),
	}
	var logMu sync.Mutex
	r.cfg.Logf = func(format string, a ...any) {
		logMu.Lock()
		defer logMu.Unlock()
		fmt.Fprintf(r.log, format+"\n", a...)
	}
	app, err = NewHTTPApp(r.cfg.Target.BaseURL())
	if err != nil {
		t.Fatal(err)
	}
	r.app = app
	r.w = r.newWorker()

	// the hold writer seams: the REAL store functions, observed at the instant
	// of the call (persist-before) and optionally made to fail AFTER the write
	origW, origC := writeMaintenanceHold, clearMaintenanceHold
	t.Cleanup(func() { writeMaintenanceHold, clearMaintenanceHold = origW, origC })
	writeMaintenanceHold = func(d string, h store.MaintenanceHold) error {
		b.expect("hold_write", false, updaterjob.StateMaintenanceHeld)
		if b.holdWriteFails {
			return errors.New("open .hold-*.tmp: no space left on device")
		}
		if err := origW(d, h); err != nil {
			return err
		}
		if b.holdWriteLies {
			return errors.New("fsync: input/output error (after the rename)")
		}
		return nil
	}
	clearMaintenanceHold = func(d, job string) error {
		// a re-run clear (crash after the first) finds it already absent:
		// ours or absent, never another's
		b.expect("hold_clear", false, updaterjob.StateComplete, updaterjob.StateRolledBack)
		if s, _ := ReadHoldFor(b.data, boxJobID); s != HoldOurs && s != HoldAbsent {
			b.violate("hold_clear with a %s hold on disk", s)
		}
		return origC(d, job)
	}
	return r
}

// newWorker is a FRESH worker over the same dirs, box and app (a restart).
func (r *rig) newWorker() *Worker {
	w, err := New(r.cfg, Deps{Lib: &fakeLib{r.box}, App: r.app, Rel: &fakeRel{r.box}, Host: &fakeHost{r.box}})
	if err != nil {
		r.t.Fatal(err)
	}
	return w
}

// install is the install verb at the production handler.
func (r *rig) install() {
	r.t.Helper()
	if resp := r.w.Handle(updaterwire.NewInstall(boxReleaseID, boxJobID)); !resp.OK || resp.State != "requested" {
		r.t.Fatalf("install verb: %+v", resp)
	}
}

// drive runs the job to its next stop on the calling goroutine.
func (r *rig) drive() error {
	return r.w.drive(context.Background(), boxJobID)
}

func (r *rig) job() updaterjob.Job {
	r.t.Helper()
	j, err := updaterjob.Read(r.data, boxJobID)
	if err != nil {
		r.t.Fatal(err)
	}
	return j
}

func (r *rig) hold() store.MaintenanceHoldState { return store.ReadMaintenanceHold(r.data) }

// states is the job's history as "state/phase" strings.
func states(j updaterjob.Job) []string {
	out := make([]string, len(j.Transitions))
	for i, t := range j.Transitions {
		out[i] = string(t.State) + "/" + string(t.Phase)
	}
	return out
}

func receiptSteps(j updaterjob.Job) []string {
	out := make([]string, len(j.Receipts))
	for i, rc := range j.Receipts {
		s := rc.Step
		if !rc.OK {
			s += "(fail)"
		}
		out[i] = s
	}
	return out
}

// expect records a call and checks the job on disk: its own state, started;
// with afterHold, this job's hold must be on disk too.
func (b *box) expect(call string, afterHold bool, want ...updaterjob.State) {
	b.mu.Lock()
	b.calls = append(b.calls, call)
	b.mu.Unlock()
	j, err := updaterjob.Read(b.data, boxJobID)
	if err != nil {
		b.violate("%s: job unreadable: %v", call, err)
		return
	}
	okState := false
	for _, s := range want {
		okState = okState || j.State == s
	}
	if !okState || j.Phase != updaterjob.PhaseStarted {
		b.violate("%s ran while the job on disk is %s/%s (want %v/started)", call, j.State, j.Phase, want)
	}
	if afterHold {
		if s, _ := ReadHoldFor(b.data, boxJobID); s != HoldOurs {
			b.violate("%s ran at %s without this job's hold on disk (%s)", call, j.State, s)
		}
	}
}

func (b *box) violate(format string, a ...any) {
	b.mu.Lock()
	b.violations = append(b.violations, fmt.Sprintf(format, a...))
	b.mu.Unlock()
}

func (b *box) noViolations(t *testing.T) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.violations) > 0 {
		t.Fatalf("persist-before / hold violations:\n  %s", strings.Join(b.violations, "\n  "))
	}
}

func (b *box) callCount(name string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, c := range b.calls {
		if c == name {
			n++
		}
	}
	return n
}

// ── the fake activation library ─────────────────────────────────────────────

type fakeLib struct{ b *box }

func (f *fakeLib) rc(step string, err error, ev map[string]string) Receipt {
	now := f.b.clock.Now()
	r := Receipt{Step: step, StartedAt: now, EndedAt: now, OK: err == nil, Evidence: ev}
	if err != nil {
		r.Err = err.Error()
	}
	return r
}

func (f *fakeLib) Resolve(dir string) (Release, error) {
	f.b.expect("resolve", false, updaterjob.StateVerified)
	var m struct {
		SourceSHA string `json:"source_sha"`
		Signature string `json:"signature_verdict"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return Release{}, err
	}
	if err := json.Unmarshal(raw, &m); err != nil || m.SourceSHA == "" || m.Signature == "" {
		return Release{}, fmt.Errorf("manifest refused")
	}
	return Release{Dir: dir, SHA: m.SourceSHA, Binary: filepath.Join(dir, f.b.relBinName), Dist: filepath.Join(dir, "web", "dist"),
		ReleaseFile: filepath.Join(dir, "RELEASE"), ManifestPath: filepath.Join(dir, "manifest.json")}, nil
}

func (f *fakeLib) Stage(rel Release) (Receipt, error) {
	f.b.expect("stage", false, updaterjob.StatePreflightOK)
	return f.rc("stage", nil, map[string]string{"vcs.revision": rel.SHA, "vcs.modified": "false"}), nil
}

func (f *fakeLib) Backup(dbPath, dest string) (Receipt, error) {
	f.b.expect("backup", true, updaterjob.StateBackupDone)
	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		return f.rc("backup", nil, map[string]string{"already": "true"}), nil
	}
	if err := copyFile(dbPath, dest); err != nil {
		return f.rc("backup", err, nil), err
	}
	return f.rc("backup", nil, map[string]string{"integrity_check": "ok"}), nil
}

func (f *fakeLib) Snapshot(install Release, dest string) (Receipt, error) {
	f.b.expect("snapshot", true, updaterjob.StateBackupDone)
	out := snapshotRelease(dest, install.SHA, filepath.Base(install.Binary)) // snapshot keeps the install's basename
	for _, p := range [][2]string{{install.Binary, out.Binary}, {install.ReleaseFile, out.ReleaseFile}} {
		if err := copyFile(p[0], p[1]); err != nil {
			return f.rc("snapshot", err, nil), err
		}
	}
	if err := copyTree(install.Dist, out.Dist); err != nil {
		return f.rc("snapshot", err, nil), err
	}
	return f.rc("snapshot", nil, map[string]string{"captured": "binary,dist,RELEASE"}), nil
}

// installHalves copies rel's three halves into dst's paths.
func installHalves(rel, dst Release) error {
	if err := copyFile(rel.Binary, dst.Binary); err != nil {
		return err
	}
	if err := copyFile(rel.ReleaseFile, dst.ReleaseFile); err != nil {
		return err
	}
	if err := os.RemoveAll(dst.Dist); err != nil {
		return err
	}
	return copyTree(rel.Dist, dst.Dist)
}

// kill plays the identity-guarded SIGKILL + systemd relaunch: the new process
// runs whatever binary is installed. The boot LINE is written later, on the
// first Watch — R1a predictor pin: the log the worker predicts must be ABSENT
// while Activate runs, exactly as on the box (the new process writes its boot
// line only after it relaunches).
func (b *box) kill(id Identity) (Identity, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if id != b.id {
		return Identity{}, fmt.Errorf("pid %d is no longer the process this identity names — refusing to signal", id.PID)
	}
	b.id = Identity{PID: b.id.PID + 100, StartTicks: b.id.StartTicks + 7}
	sha, _ := parseBinaryBody(b.instBinary())
	b.running = sha
	b.clock.Advance(6 * time.Second) // RestartSec=5 + boot
	return b.id, nil
}

// instBinary is the install's binary: vl-bin when present, else vl-bin
// (R5 removes the vl branch) — the same rule target.InstallBinaryPath uses.
func (b *box) instBinary() string {
	if _, err := os.Stat(filepath.Join(b.inst, "vl-bin")); err == nil {
		return filepath.Join(b.inst, "vl-bin")
	}
	return filepath.Join(b.inst, "vl-bin")
}

// writeBootLine appends the relaunched process's boot line to logPath — called
// from Watch (AFTER Activate/RollbackTo returned), never from kill.
func (b *box) writeBootLine(logPath string) error {
	b.mu.Lock()
	sha, status, level, pid := b.running, "OK", "INFO", b.id.PID
	if b.refuseBoot[sha] {
		status, level = "REFUSED", "ERRO" // main.go logs the refused line at ERROR
	}
	now := b.clock.Now().In(time.Local)
	b.mu.Unlock()
	line := fmt.Sprintf("%s [%s] main/main.go:322 🔐 BOOT INTEGRITY %s — rev %s · pid %d · built 2026-09-24T00:00:00Z · expected %s · goldens PASS\n",
		now.Format("01-02 15:04:05"), level, status, sha[:12], pid, sha[:12])
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

func (f *fakeLib) Activate(rel, prev Release, id Identity) (Identity, Receipt, error) {
	f.b.expect("activate", true, updaterjob.StateActivated)
	if j, err := updaterjob.Read(f.b.data, boxJobID); err == nil {
		if j.IdentityBefore == nil || *j.IdentityBefore != id || j.WatchSince == nil || j.LogOffset == nil {
			f.b.violate("activate(%v) without that identity and the boot-watch inputs persisted first", id)
		}
	}
	f.b.mu.Lock()
	f.b.activateIDs = append(f.b.activateIDs, id)
	f.b.mu.Unlock()
	if err := installHalves(rel, prev); err != nil {
		return Identity{}, f.rc("activate", err, nil), err
	}
	next, err := f.b.kill(id)
	if err != nil {
		return Identity{}, f.rc("activate", err, nil), err
	}
	return next, f.rc("activate", nil, map[string]string{"release": rel.SHA, "prev": prev.SHA, "killed": "SIGKILL"}), nil
}

func (f *fakeLib) Watch(rel Release, id Identity, opts WatchOpts) (Receipt, error) {
	f.b.expect("watch", true, updaterjob.StateBooted, updaterjob.StateRollingBack)
	// The relaunched process's boot line lands only after the Activate (or
	// RollbackTo) returned — write it now, to the path the worker predicted.
	if err := f.b.writeBootLine(opts.LogPath); err != nil {
		return f.rc("watch", err, nil), err
	}
	f.b.mu.Lock()
	f.b.watchOpts = append(f.b.watchOpts, opts)
	f.b.watchSHAs = append(f.b.watchSHAs, rel.SHA)
	fail, running := f.b.watchFail[rel.SHA], f.b.running
	f.b.mu.Unlock()
	ev := map[string]string{"expect_sha": rel.SHA}
	if fail || running != rel.SHA {
		f.b.clock.Advance(opts.Within)
		err := fmt.Errorf("not proven within %s: no boot line for %s", opts.Within, rel.SHA)
		return f.rc("watch", err, ev), err
	}
	ev["boot_line"] = "found after the kill"
	return f.rc("watch", nil, ev), nil
}

func (f *fakeLib) RollbackTo(prev, install Release, id Identity) (Identity, Receipt, error) {
	f.b.expect("rollback", true, updaterjob.StateRollingBack)
	if j, err := updaterjob.Read(f.b.data, boxJobID); err == nil {
		f.b.mu.Lock()
		f.b.rollbackAtt = append(f.b.rollbackAtt, j.Attempts)
		f.b.mu.Unlock()
	}
	f.b.mu.Lock()
	f.b.rollbackIDs = append(f.b.rollbackIDs, id)
	f.b.rollbackArg = append(f.b.rollbackArg, [2]Release{prev, install})
	fail, distFail := f.b.rollbackFail, f.b.rollbackDistFail
	f.b.mu.Unlock()
	if fail {
		err := errors.New("restore binary: rename: read-only file system")
		return Identity{}, f.rc("rollback", err, map[string]string{"restore": prev.SHA}), err
	}
	if distFail {
		// The #206 review fold scenario: the binary and RELEASE are restored,
		// the dist restore FAILS, and the systemd unit relaunches onto the
		// restored old binary before RollbackTo returns (its own kill never
		// runs — activation.RollbackTo errors out of atomicSwapDir). The
		// install now serves the FAILED release's bundle on the old binary.
		if err := copyFile(prev.Binary, install.Binary); err != nil {
			return Identity{}, f.rc("rollback", err, nil), err
		}
		if err := copyFile(prev.ReleaseFile, install.ReleaseFile); err != nil {
			return Identity{}, f.rc("rollback", err, nil), err
		}
		next, err := f.b.kill(id) // Restart=on-failure relaunch
		if err != nil {
			return Identity{}, f.rc("rollback", err, nil), err
		}
		err = errors.New("restore dist: injected partial restore")
		return next, f.rc("rollback", err, map[string]string{"restore": prev.SHA}), err
	}
	if err := installHalves(prev, install); err != nil {
		return Identity{}, f.rc("rollback", err, nil), err
	}
	next, err := f.b.kill(id)
	if err != nil {
		err = fmt.Errorf("ROLLBACK FAILED — %w", err)
		return Identity{}, f.rc("rollback", err, nil), err
	}
	return next, f.rc("rollback", nil, map[string]string{"restore": prev.SHA, "restored": "binary,dist,RELEASE"}), nil
}

func (f *fakeLib) CurrentIdentity() (Identity, error) {
	f.b.mu.Lock()
	defer f.b.mu.Unlock()
	return f.b.id, nil
}

// ── the fake release re-proof ───────────────────────────────────────────────

type fakeRel struct{ b *box }

func (f *fakeRel) Verdict(releaseID string) (Verdict, error) {
	if f.b.verdictMissing || releaseID != boxReleaseID {
		return Verdict{}, fmt.Errorf("no verdict for %q", releaseID)
	}
	return Verdict{ReleaseID: releaseID, SourceSHA: boxNew, ReleaseDir: f.b.relDir, ManifestSHA256: strings.Repeat("ab", 32),
		SignerFingerprint: "SHA256:fakeReleaseKey", Artifacts: 5}, nil
}

func (f *fakeRel) Rehash(v Verdict) (int, error) {
	f.b.expect("rehash", false, updaterjob.StateDownloaded)
	return v.Artifacts, nil
}

func (f *fakeRel) Reverify(v Verdict) (ReleaseFacts, error) {
	arts := map[string]string{}
	for _, rel := range []string{"web/dist/index.html", "ninjascript/VLTrader.cs", calendarFile, f.b.relBinName} {
		if f.b.noCS && strings.HasPrefix(rel, "ninjascript/") {
			continue
		}
		sum, err := sha256File(filepath.Join(f.b.relDir, filepath.FromSlash(rel)))
		if err != nil {
			return ReleaseFacts{}, err
		}
		arts[rel] = sum
	}
	facts := ReleaseFacts{ReleaseID: v.ReleaseID, SourceSHA: v.SourceSHA, ManifestSHA256: v.ManifestSHA256,
		SignerFingerprint: v.SignerFingerprint, AddonBuildID: f.b.manifestBuild, Artifacts: arts}
	if f.b.reverifyTamper != nil {
		f.b.reverifyTamper(&facts)
	}
	return facts, nil
}

// ── the fake host ───────────────────────────────────────────────────────────

type fakeHost struct{ b *box }

func (h *fakeHost) Now() time.Time { return h.b.clock.Now() }

func (h *fakeHost) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h.b.clock.Advance(d)
	if h.b.pollHook != nil {
		h.b.pollHook()
	}
	return nil
}

func (h *fakeHost) LockHolder() (string, error) {
	h.b.mu.Lock()
	defer h.b.mu.Unlock()
	return h.b.lockHolder, nil
}

// LockAcquire simulates `vl-lock.sh acquire`. Free (lockHolder "") takes it
// as our session; held refuses naming the holder; an error flag fails it.
func (h *fakeHost) LockAcquire(session, task string, minutes int) (bool, string, error) {
	h.b.mu.Lock()
	defer h.b.mu.Unlock()
	h.b.lockAcquireCalls++
	h.b.lockAcquireSessions = append(h.b.lockAcquireSessions, session)
	if h.b.lockAcquireErr != nil {
		return false, "", h.b.lockAcquireErr
	}
	if h.b.lockHolder == "" {
		h.b.lockHolder = session
		return true, "", nil
	}
	return false, h.b.lockHolder, nil
}

// LockRelease simulates `vl-lock.sh release` (only the holder may release).
func (h *fakeHost) LockRelease(session string) error {
	h.b.mu.Lock()
	defer h.b.mu.Unlock()
	h.b.lockReleaseCalls++
	if h.b.lockHolder == "" {
		return nil // the script's no-lock no-op
	}
	if h.b.lockHolder != session {
		return fmt.Errorf("release refused: '%s' is not the holder ('%s')", session, h.b.lockHolder)
	}
	h.b.lockHolder = ""
	return nil
}

func (h *fakeHost) BuildInfo(binary string) (string, string, error) {
	sha, mod := parseBinaryBody(binary)
	if sha == "" {
		return "", "", fmt.Errorf("%s: no build info", binary)
	}
	return sha, mod, nil
}

func (h *fakeHost) ExeOf(pid int) (string, error) {
	h.b.mu.Lock()
	defer h.b.mu.Unlock()
	if pid != h.b.id.PID {
		return "", fmt.Errorf("no such process %d", pid)
	}
	if h.b.exe != "" {
		return h.b.exe, nil
	}
	return h.b.instBinary(), nil // R5 removes: vl-bin when the install carries it
}

// ── the app (httptest) ──────────────────────────────────────────────────────

func (b *box) serveApp(w http.ResponseWriter, r *http.Request) {
	auth := func() bool {
		b.mu.Lock()
		bad := b.badToken
		b.mu.Unlock()
		b.mu.Lock()
		echo := b.echo500
		b.mu.Unlock()
		if echo {
			http.Error(w, "internal error: request carried "+r.Header.Get("Authorization"), http.StatusInternalServerError)
			return false
		}
		if bad || r.Header.Get("Authorization") != "Bearer "+boxToken {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return false
		}
		return true
	}
	switch r.URL.Path {
	case "/api/health":
		b.mu.Lock()
		rev := b.running[:12]
		if b.healthRev != "" {
			rev = b.healthRev
		}
		b.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "time": "x", "revision": rev})
	case "/api/maintenance":
		if auth() {
			json.NewEncoder(w).Encode(b.maintenanceView())
		}
	case "/api/installation-gate":
		if auth() {
			json.NewEncoder(w).Encode(b.gateView())
		}
	case "/":
		body, err := os.ReadFile(filepath.Join(b.inst, "web", "dist", "index.html"))
		if err != nil {
			http.Error(w, "no ui", http.StatusServiceUnavailable)
			return
		}
		w.Write(body)
	default:
		http.NotFound(w, r)
	}
}

// ack is the AddOn's current maintenance_ack: it answers the hold ON DISK,
// resent every 5 s (received = the last 5 s tick; age = now − received).
func (b *box) ack() *AckView {
	b.mu.Lock()
	connected, build, stale, ackJob, lag, wall, seq := b.addonConnected, b.addonBuild, b.ackStale, b.ackJob, b.ackLag, b.wallStep, b.addonSeq
	down := b.addonDownSince
	b.mu.Unlock()
	if !connected || down > 0 {
		return nil
	}
	st := store.ReadMaintenanceHold(b.data)
	now := b.clock.Now().UTC()
	recv := now.Truncate(5 * time.Second).Add(-lag)
	// wallStep shifts ONLY the rendered wall time (the thing the old code
	// compared); AgeMs keeps measuring the monotonic age of the SAME ack.
	a := &AckView{Received: recv.Add(wall).Format(time.RFC3339Nano), AgeMs: now.Sub(recv).Milliseconds(), BuildID: build, AcceptSeq: seq}
	b.mu.Lock()
	b.ackAges = append(b.ackAges, a.AgeMs) // the age sequence the worker OBSERVED
	b.mu.Unlock()
	if st.Held && !st.Corrupt {
		a.Held, a.JobID = true, st.Hold.JobID
	}
	if stale {
		a.AgeMs = 20000
	}
	if ackJob != "" {
		a.JobID = ackJob
	}
	return a
}

func (b *box) maintenanceView() MaintenanceView {
	st := store.ReadMaintenanceHold(b.data)
	v := MaintenanceView{State: "clear", Drained: true, AddonAck: b.ack()}
	if st.Held {
		job, since := st.Hold.JobID, st.Hold.Since
		v.Held, v.State, v.JobID, v.Since = true, "held", &job, &since
	}
	b.mu.Lock()
	if b.maintJob != "" {
		mj := b.maintJob
		v.JobID = &mj
	}
	b.mu.Unlock()
	return v
}

func (b *box) gateView() GateView {
	st := store.ReadMaintenanceHold(b.data)
	b.mu.Lock()
	flat, plannerHeld := b.flat, b.plannerInFlight
	b.mu.Unlock()
	a := b.ack()
	job := "n/a"
	if st.Held {
		job = st.Hold.JobID
	}
	// The prehold census leg (trader/installation_gate.go): a never-held
	// connection (no ack) PASSES — the other flat legs vouch for it; an ack
	// that exists must be fresh AND flat — except a STALE FLAT census, which
	// passes exactly like no census (prehold-stale-census, owner order
	// 2026-10-02 10:15 CT): a stale census is never stronger evidence than no
	// census, so the second install of one bot process needs no restart.
	preholdCensusPass, preholdCensusDetail := true, "no census — never held"
	if a != nil {
		switch {
		case a.AgeMs > ackMaxAgeMs && flat:
			preholdCensusPass, preholdCensusDetail = true, fmt.Sprintf("census is %d ms old (from an earlier hold) — flat, treated as no census", a.AgeMs)
		case a.AgeMs > ackMaxAgeMs:
			preholdCensusPass, preholdCensusDetail = false, fmt.Sprintf("census ack is %d ms old (max %d) and not flat", a.AgeMs, ackMaxAgeMs)
		default:
			preholdCensusPass, preholdCensusDetail = flat, fmt.Sprintf("flat=%v", flat)
		}
	}
	censusAge := "n/a"
	if a != nil {
		censusAge = fmt.Sprintf("%dms", a.AgeMs)
	}
	legs := []GateLeg{
		{Name: "hold", Pass: st.Held, Detail: "held=" + fmt.Sprint(st.Held)},
		{Name: "go_drained", Pass: st.Held, Detail: "barrier engaged"},
		{Name: "in_flight_sends", Pass: true, Detail: "0"},
		{Name: "queued_signals", Pass: true, Detail: "0"},
		{Name: "planner_in_flight", Pass: !plannerHeld, Detail: func() string {
			if plannerHeld {
				return "waiting for the AI plan (started 12:03:05)"
			}
			return "none"
		}()},
		{Name: "traders_nt8", Pass: true, Detail: "1 NT8 trader"},
		{Name: "addon_ack", Pass: st.Held && a != nil && a.Held && a.JobID == job, Detail: "ack"},
		{Name: "addon_census", Pass: flat, Detail: fmt.Sprintf("age=%s — flat=%v", censusAge, flat)},
		{Name: "addon_census_prehold", Pass: preholdCensusPass, Detail: preholdCensusDetail},
		{Name: "ledger_exposure", Pass: flat, Detail: "arms"},
		{Name: "trader_cutover:t1", Pass: flat && !b.cutoverStale, Detail: func() string {
			if b.cutoverStale {
				return "working_orders: snapshot stale (AddOn down)"
			}
			return "legs 1,2,4"
		}()},
	}
	ready := true
	for _, l := range legs {
		ready = ready && l.Pass
	}
	gv := GateView{Ready: ready, JobID: job, Legs: legs, Traders: []string{"t1"}, Note: "test"}
	// UPDATER-NT8-CLOSED: the fake gate mirrors trader/installation_gate.go —
	// the nt8_absent view is ATTACHED whenever an NT8 trader exists (also when
	// the link is up: eligible=false, ready=false), and its flags are computed
	// from the knobs; the override probes the worker's own eligibility check.
	b.mu.Lock()
	downSince, inflight, queued, planner, sim, dbOpen := b.addonDownSince, b.absentInflight, b.absentQueued, b.absentPlanner, b.absentSim, b.absentDbOpen
	override, eligible, ready := b.absentOverride, b.absentEligible, b.absentReady
	if override && b.absentFlapAt > 0 {
		b.absentViewReads++
		if b.absentViewReads >= b.absentFlapAt {
			eligible, ready = false, true // flap: eligible is gone, ready lies
		}
	}
	b.mu.Unlock()
	abs := &NT8AbsentView{}
	if override {
		abs.Eligible, abs.Ready = eligible, ready
		if ready {
			abs.LinkDownSince = "2026-09-28T04:00:00Z"
		}
	} else if downSince > 0 {
		downAt := b.clock.Now().Add(-downSince)
		abs.Eligible = downSince >= 60*time.Second
		abs.LinkDownSince = downAt.UTC().Format(time.RFC3339Nano)
	}
	if abs.Eligible || ready {
		absLegs := []GateLeg{
			{Name: "hold", Pass: st.Held, Detail: "held=" + fmt.Sprint(st.Held)},
			{Name: "go_drained", Pass: st.Held, Detail: "barrier engaged"},
			{Name: "in_flight_sends", Pass: !inflight, Detail: fmt.Sprintf("in_flight_sends=%v", inflight)},
			{Name: "queued_signals", Pass: queued == 0, Detail: fmt.Sprintf("queued_signals=%d", queued)},
			{Name: "planner_in_flight", Pass: !planner, Detail: "none"},
			{Name: "ledger_exposure", Pass: flat, Detail: fmt.Sprintf("flat=%v", flat)},
			{Name: "sim_accounts", Pass: sim, Detail: "Sim101 tradeable"},
			{Name: "db_open_positions", Pass: !dbOpen, Detail: "0 open rows"},
		}
		abs.Ready = true
		for _, l := range absLegs {
			abs.Ready = abs.Ready && l.Pass
		}
		abs.Legs = absLegs
	}
	gv.NT8Absent = abs
	return gv
}

// ── files ───────────────────────────────────────────────────────────────────

func binaryBody(sha string) string { return "VLBIN rev=" + sha + " modified=false\n" }

func parseBinaryBody(p string) (sha, modified string) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", ""
	}
	for _, f := range strings.Fields(string(b)) {
		if v, ok := strings.CutPrefix(f, "rev="); ok {
			sha = v
		}
		if v, ok := strings.CutPrefix(f, "modified="); ok {
			modified = v
		}
	}
	return sha, modified
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o755)
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if fi.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		return copyFile(p, filepath.Join(dst, rel))
	})
}

// allBytes is every file under the dirs, concatenated (for leak scans).
func allBytes(t *testing.T, dirs ...string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, d := range dirs {
		filepath.Walk(d, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() || fi.Mode()&os.ModeSocket != 0 {
				return nil
			}
			if b, err := os.ReadFile(p); err == nil {
				out[p] = b
			}
			return nil
		})
	}
	return out
}

func sortedKeys(m map[string][]byte) []string {
	k := make([]string, 0, len(m))
	for p := range m {
		k = append(k, p)
	}
	sort.Strings(k)
	return k
}

// resumeRequest is the attended CLI's frame (a _test.go file is outside the
// resume census by design: it is never linked into a binary).
func resumeRequest(jobID string) updaterwire.Request { return updaterwire.NewResume(jobID) }

func updaterwireInstallOf(jobID string) updaterwire.Request {
	return updaterwire.NewInstall(boxReleaseID, jobID)
}

func updaterwireStatusOf(jobID string) updaterwire.Request { return updaterwire.NewStatus(jobID) }
